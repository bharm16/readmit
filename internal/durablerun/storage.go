package durablerun

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/testrunner"
)

const maxJournal = 32 << 20
const maxPlan = 4 << 20

func digest(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func write(root *os.Root, name string, b []byte) error {
	f, e := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return errors.New("cannot create durable evidence")
	}
	_, e = f.Write(b)
	if e == nil {
		e = f.Sync()
	}
	c := f.Close()
	if e != nil || c != nil {
		return errors.New("cannot sync durable evidence; partial evidence retained")
	}
	return nil
}
func (w *writer) append(e entry) error {
	if w.failed != nil {
		return w.failed
	}
	e.Sequence = w.sequence + 1
	e.Previous = w.previous
	e.At = time.Now().UTC()
	b, err := json.Marshal(e, json.Deterministic(true))
	if err != nil {
		return errors.New("cannot encode durable journal")
	}
	if w.journalBytes+len(b)+1 > maxJournal {
		w.failed = errors.New("durable journal reached its size limit; execution stopped")
		return w.failed
	}
	if _, err = w.journal.Write(append(b, '\n')); err == nil {
		err = w.journal.Sync()
	}
	if err != nil {
		w.failed = errors.New("cannot sync durable journal; execution stopped")
		return w.failed
	}
	w.journalBytes += len(b) + 1
	w.sequence++
	w.previous = digest(b)
	return nil
}
func read(root *os.Root, name string, limit int) ([]byte, error) {
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Size() > int64(limit) {
		return nil, errors.New("durable evidence must be a bounded regular file")
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, errors.New("cannot open durable evidence")
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("invalid durable evidence")
	}
	b, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil || len(b) > limit {
		return nil, errors.New("cannot read bounded durable evidence")
	}
	return b, nil
}
func verify(root *os.Root, p payload) error {
	if p.Size < 0 || p.Size > 64<<20 {
		return errors.New("invalid durable payload size")
	}
	b, err := read(root, p.Path, p.Size)
	if err != nil {
		return err
	}
	if len(b) != p.Size || digest(b) != p.SHA256 {
		return errors.New("durable payload changed")
	}
	return nil
}

// Open recovers retained evidence without changing files or opening a network
// connection. An unfinished journal proves no completion: interrupted means
// completion was not recorded, not that another process is known to be dead.
func Open(path string) (Summary, error) {
	bad := errors.New("durable journal is invalid or evidence changed")
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return Summary{}, bad
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return Summary{}, bad
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return Summary{}, bad
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return Summary{}, bad
	}
	defer root.Close()
	raw, err := read(root, "plan.json", maxPlan)
	if err != nil {
		return Summary{}, err
	}
	var doc planDocument
	if json.Unmarshal(raw, &doc, json.RejectUnknownMembers(true)) != nil || doc.Schema != Schema || doc.CreatedAt.IsZero() || len(doc.Inputs.Mappings) == 0 || len(doc.Inputs.Mappings) > replay.MaxMessages || len(doc.Inputs.Mappings) != len(doc.Payloads) {
		return Summary{}, bad
	}
	spec, err := testrunner.DecodeSpec(doc.Inputs.Spec)
	if err != nil || len(spec.Input.Messages) != len(doc.Payloads) {
		return Summary{}, bad
	}
	for i, p := range doc.Payloads {
		id := fmt.Sprintf("o%06d", i+1)
		if p.Path != "intended/"+id+".bin" || doc.Inputs.Mappings[i].OutboundOccurrence != id {
			return Summary{}, bad
		}
		if err = verify(root, p); err != nil {
			return Summary{}, err
		}
	}
	summary := Summary{Schema: Schema, State: Interrupted, StopReason: Interrupted, Planned: len(doc.Payloads), Recovered: true}
	journal, err := read(root, "journal.jsonl", maxJournal)
	if err != nil {
		return Summary{}, err
	}
	previous := digest(raw)
	pending := ""
	finished := false
	sequence := 0
	recordedEvents := []replay.Event{}
	sentRecorded := false
	lines := bytes.Split(journal, []byte{'\n'})
	truncated := len(lines[len(lines)-1]) > 0
	for _, line := range lines[:len(lines)-1] {
		sequence++
		var e entry
		if finished || json.Unmarshal(line, &e, json.RejectUnknownMembers(true)) != nil || e.Sequence != sequence || e.Previous != previous || e.At.IsZero() {
			return Summary{}, bad
		}
		previous = digest(line)
		if sequence == 1 && e.Kind != "ready" || sequence == 2 && e.Kind != "running" {
			return Summary{}, bad
		}
		switch e.Kind {
		case "ready":
			if sequence != 1 {
				return Summary{}, bad
			}
		case "running":
			if sequence != 2 {
				return Summary{}, bad
			}
		case "intent":
			if sequence < 3 || pending != "" || e.Occurrence != fmt.Sprintf("o%06d", summary.Recorded+1) {
				return Summary{}, bad
			}
			pending = e.Occurrence
			sentRecorded = false
			summary.DeliveryUncertain = true
		case "sent":
			if sentRecorded || pending == "" || e.Occurrence != pending || e.Sent == nil || e.Sent.Path != "sent/"+pending+".bin" {
				return Summary{}, bad
			}
			if err = verify(root, *e.Sent); err != nil {
				return Summary{}, err
			}
			sentRecorded = true
		case "recorded":
			if summary.Recorded >= summary.Planned || e.Event == nil || e.Occurrence != fmt.Sprintf("o%06d", summary.Recorded+1) || e.Event.OutboundOccurrence != e.Occurrence {
				return Summary{}, bad
			}
			ev := e.Event
			if ev.Intended.SHA256 != doc.Payloads[summary.Recorded].SHA256 || ev.Intended.Size != doc.Payloads[summary.Recorded].Size || ev.Source.SHA256 != doc.Inputs.Mappings[summary.Recorded].SourceSHA256 {
				return Summary{}, bad
			}
			recordedEvents = append(recordedEvents, *ev)
			for _, p := range []struct {
				name string
				data payload
			}{
				{"source", payload{ev.Source.Path, ev.Source.Size, ev.Source.SHA256}}, {"intended", payload{ev.Intended.Path, ev.Intended.Size, ev.Intended.SHA256}},
				{"sent", payload{ev.Sent.Path, ev.Sent.Size, ev.Sent.SHA256}}, {"received", payload{ev.Received.Path, ev.Received.Size, ev.Received.SHA256}},
			} {
				if p.data.Path != "payloads/"+e.Occurrence+"-"+p.name+".bin" {
					return Summary{}, bad
				}
				ref := p.data
				ref.Path = "result/run/" + ref.Path
				if err = verify(root, ref); err != nil {
					return Summary{}, err
				}
			}
			summary.Recorded++
			if ev.Delivery == "acknowledged" {
				summary.DeliveryUncertain = false
				pending = ""
			}
		case "finished":
			if e.Final == nil || !terminal(e.Final.StopReason) || e.Final.Planned != summary.Planned || e.Final.Recorded != summary.Recorded || e.Final.Schema != Schema || e.Final.DeliveryUncertain != summary.DeliveryUncertain {
				return Summary{}, bad
			}
			final := *e.Final
			if final.Recovered || final.JournalIncomplete {
				return Summary{}, bad
			}
			expected := final.StopReason
			if summary.DeliveryUncertain {
				expected = DeliveryUncertain
			}
			if final.State != expected {
				return Summary{}, bad
			}
			if final.ResultIdentity != "" {
				artifact, err := testrunner.Open(filepath.Join(path, "result"))
				if err != nil || artifact.Identity != final.ResultIdentity || artifact.Result.SpecIdentity != digest(doc.Inputs.Spec) || artifact.Result.InputBundleIdentity != doc.Inputs.SourceIdentity {
					return Summary{}, bad
				}
				if artifact.Result.Target == nil || *artifact.Result.Target != doc.Inputs.Target {
					return Summary{}, bad
				}
				if artifact.Run != nil {
					if len(recordedEvents) != len(artifact.Run.Events) {
						return Summary{}, bad
					}
					for i, ev := range recordedEvents {
						actual, _ := json.Marshal(artifact.Run.Events[i], json.Deterministic(true))
						recorded, _ := json.Marshal(ev, json.Deterministic(true))
						if !bytes.Equal(actual, recorded) {
							return Summary{}, bad
						}
					}
				}
				if final.StopReason == Passed && artifact.Result.Status != testrunner.Pass || final.StopReason == AssertionFailed && artifact.Result.Status != testrunner.AssertionFailure {
					return Summary{}, bad
				}
			} else if final.StopReason == Passed || final.StopReason == AssertionFailed {
				return Summary{}, bad
			}
			summary = final
			finished = true
		default:
			return Summary{}, bad
		}
	}
	if !finished {
		if summary.DeliveryUncertain {
			summary.State = DeliveryUncertain
		}
		summary.Recovered = true
	}
	if truncated {
		summary.JournalIncomplete = true
		summary.DeliveryUncertain = true
		summary.State = DeliveryUncertain
		summary.StopReason = Interrupted
		summary.Recovered = true
	}
	return summary, nil
}
func terminal(s State) bool {
	switch s {
	case Passed, AssertionFailed, ExecutionError, Cancelled, TimedOut:
		return true
	}
	return false
}
