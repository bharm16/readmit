package durablerun

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/durablelog"
	"github.com/bharm16/readmit/internal/engine"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/testrunner"
)

const maxJournal = 32 << 20
const maxPlan = 4 << 20
const maxLease = 64 << 10

// maxResourceName bounds one resource a lease names. It is the widest of the
// two things a run declares: an environment name is bounded at 64 bytes by the
// target contract, and an endpoint address by that contract's own address rule.
const maxResourceName = 1 << 10

// evidenceFile is what one durable write needs from the file it writes. It is
// the one seam a size-limited stand-in for a full disk takes in tests; every
// evidence and journal write in this package goes through openEvidence.
type evidenceFile interface {
	io.Writer
	Sync() error
	Close() error
}

var (
	openEvidence = func(root *os.Root, name string) (evidenceFile, error) {
		return root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	}
	journalLimit = maxJournal
)

// errJournalLimit is returned before any byte of the refused record is written,
// so a send whose intent it refused was never attempted.
var errJournalLimit = errors.New("durable journal reached its size limit; execution stopped")

func digest(b []byte) string { return durablelog.Digest(b) }
func write(root *os.Root, name string, b []byte) error {
	f, e := openEvidence(root, name)
	if e != nil {
		return errors.New("cannot create durable evidence")
	}
	e = artifactdir.WriteFileSync(f, b)
	c := f.Close()
	if e != nil || c != nil {
		return errors.New("cannot sync durable evidence; partial evidence retained")
	}
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

// openJob resolves a job directory to its physical root before anything in it
// is trusted, so a symlink cannot point recovery at another directory's result.
func openJob(path string) (*os.Root, string, error) {
	bad := errors.New("durable journal is invalid or evidence changed")
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, "", bad
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return nil, "", bad
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return nil, "", bad
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, "", bad
	}
	return root, path, nil
}

// readPin reads the engine pin a run retained beside its plan. A directory
// that retains none is not a job this release wrote, and is refused rather
// than read as one whose evaluator is unknown.
func readPin(root *os.Root) (engine.Pin, error) {
	raw, err := read(root, "engine.json", engine.MaxPinBytes)
	if err != nil {
		return engine.Pin{}, errors.New("durable run retains no readable engine pin")
	}
	return engine.Decode(raw)
}

// Engine reports the pin a job retained whether or not this build evaluates
// what it names, so an operator reads why a run is unreadable rather than only
// that it is. It opens no network connection and changes no file.
func Engine(path string) (engine.Pin, error) {
	root, _, err := openJob(path)
	if err != nil {
		return engine.Pin{}, err
	}
	defer root.Close()
	return readPin(root)
}

// Open recovers retained evidence without changing files or opening a network
// connection. An unfinished journal proves no completion: interrupted means
// completion was not recorded, not that another process is known to be dead.
func Open(path string) (Summary, error) {
	recovery, _, err := readJob(path)
	if err != nil {
		return Summary{}, err
	}
	return recovery.Run, nil
}

// Recover is the same read as Open, reporting what it established about every
// planned occurrence, whether the run recorded its completion, whether a lease
// is still held, and whether a new run would repeat only never-attempted work.
func Recover(path string) (Recovery, error) {
	recovery, _, err := readJob(path)
	return recovery, err
}

func readJob(path string) (Recovery, planDocument, error) {
	bad := errors.New("durable journal is invalid or evidence changed")
	root, path, err := openJob(path)
	if err != nil {
		return Recovery{}, planDocument{}, err
	}
	defer root.Close()
	// The pin is read before anything is judged: a run evaluated under a spec
	// or profile version this release does not read is refused by name, not
	// reported as invalid evidence.
	pin, err := readPin(root)
	if err != nil {
		return Recovery{}, planDocument{}, err
	}
	if err = pin.Supported(); err != nil {
		return Recovery{}, planDocument{}, err
	}
	raw, err := read(root, "plan.json", maxPlan)
	if err != nil {
		return Recovery{}, planDocument{}, err
	}
	var doc planDocument
	if json.Unmarshal(raw, &doc, json.RejectUnknownMembers(true)) != nil || doc.Schema != Schema || doc.CreatedAt.IsZero() || len(doc.Inputs.Mappings) == 0 || len(doc.Inputs.Mappings) > replay.MaxMessages || len(doc.Inputs.Mappings) != len(doc.Payloads) {
		return Recovery{}, planDocument{}, bad
	}
	spec, err := testrunner.DecodeSpec(doc.Inputs.Spec)
	if err != nil || len(spec.Input.Messages) != len(doc.Payloads) {
		return Recovery{}, planDocument{}, bad
	}
	for i, p := range doc.Payloads {
		id := fmt.Sprintf("o%06d", i+1)
		if p.Path != "intended/"+id+".bin" || doc.Inputs.Mappings[i].OutboundOccurrence != id {
			return Recovery{}, planDocument{}, bad
		}
		if err = verify(root, p); err != nil {
			return Recovery{}, planDocument{}, err
		}
	}
	intended := make([]payload, len(doc.Payloads))
	sources := make([]string, len(doc.Inputs.Mappings))
	for i := range doc.Payloads {
		intended[i] = doc.Payloads[i]
		sources[i] = doc.Inputs.Mappings[i].SourceSHA256
	}
	proto := newProtocol(len(doc.Payloads), intended, sources)
	journal, err := read(root, "journal.jsonl", maxJournal)
	if err != nil {
		return Recovery{}, planDocument{}, err
	}
	truncated, err := durablelog.Scan(journal, digest(raw), bad,
		func(line []byte) durablelog.Record {
			var e entry
			if json.Unmarshal(line, &e, json.RejectUnknownMembers(true)) != nil {
				return nil
			}
			return &e
		},
		func(at int, r durablelog.Record) error {
			e := r.(*entry)
			switch e.Kind {
			case recordReady, recordRunning, recordIntent:
				if err := proto.apply(e); err != nil {
					return bad
				}
			case recordSent:
				if err := proto.apply(e); err != nil {
					return bad
				}
				if err := verify(root, *e.Sent); err != nil {
					return err
				}
			case recordRecorded:
				refs, err := proto.recorded(e)
				if err != nil {
					return bad
				}
				for _, declared := range refs {
					ref := declared
					ref.Path = "result/run/" + ref.Path
					if err := verify(root, ref); err != nil {
						return err
					}
				}
			case recordFinished:
				final, err := proto.finish(e)
				if err != nil {
					return bad
				}
				if final.ResultIdentity != "" {
					artifact, err := testrunner.Open(filepath.Join(path, "result"))
					if err != nil || artifact.Identity != final.ResultIdentity || artifact.Result.SpecIdentity != digest(doc.Inputs.Spec) || artifact.Result.InputBundleIdentity != doc.Inputs.SourceIdentity {
						return bad
					}
					if artifact.Result.Target == nil || *artifact.Result.Target != doc.Inputs.Target {
						return bad
					}
					if artifact.Run != nil {
						events := proto.recordedEvents
						if len(events) != len(artifact.Run.Events) {
							return bad
						}
						for i, ev := range events {
							actual, _ := json.Marshal(artifact.Run.Events[i], json.Deterministic(true))
							recorded, _ := json.Marshal(ev, json.Deterministic(true))
							if !bytes.Equal(actual, recorded) {
								return bad
							}
						}
					}
					if final.StopReason == Passed && artifact.Result.Status != testrunner.Pass || final.StopReason == AssertionFailed && artifact.Result.Status != testrunner.AssertionFailure {
						return bad
					}
				} else if final.StopReason == Passed || final.StopReason == AssertionFailed {
					return bad
				}
				proto.commit(final)
			default:
				return bad
			}
			return nil
		})
	if err != nil {
		return Recovery{}, planDocument{}, err
	}
	summary := proto.summary
	occurrences := proto.occurrences
	if !proto.finished {
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
		// A torn record is never assumed harmless: it may be an intent for the
		// next occurrence whose sync the crash interrupted.
		for i := range occurrences {
			if occurrences[i].Delivery == NotAttempted {
				occurrences[i].Delivery = Uncertain
				break
			}
		}
	}
	recovery := Recovery{Schema: RecoverySchema, Run: summary, Terminal: proto.finished && !truncated, Occurrences: occurrences}
	for _, o := range occurrences {
		switch o.Delivery {
		case Acknowledged:
			recovery.Acknowledged++
		case Uncertain:
			recovery.Uncertain++
		default:
			recovery.NotAttempted++
		}
	}
	recovery.Lease, err = leaseState(root, recovery.Terminal)
	if err != nil {
		return Recovery{}, planDocument{}, err
	}
	switch {
	case !recovery.Terminal:
		recovery.ResumeRefusal = "completion was not recorded; the writer may still be running"
	case recovery.Uncertain > 0:
		recovery.ResumeRefusal = "an intent was synced without an acknowledged outcome; that send is never repeated"
	case recovery.Acknowledged > 0:
		recovery.ResumeRefusal = "a delivery was acknowledged; a send is never repeated, and this release executes a spec whole"
	default:
		recovery.SafeToRepeat = true
	}
	return recovery, doc, nil
}
