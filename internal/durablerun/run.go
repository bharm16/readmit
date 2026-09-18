// Package durablerun retains a local execution journal around the frozen test
// and replay artifacts. Opening a job is read-only recovery, never execution.
package durablerun

import (
	"context"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/testrunner"
)

const Schema = "readmit-job/v1"

type State string

const (
	Ready             State = "ready"
	Running           State = "running"
	Passed            State = "passed"
	AssertionFailed   State = "assertion_failed"
	ExecutionError    State = "execution_error"
	Cancelled         State = "cancelled"
	TimedOut          State = "timed_out"
	Interrupted       State = "interrupted"
	DeliveryUncertain State = "delivery_uncertain"
)

// Summary contains no message values or source paths. StopReason distinguishes
// why execution stopped from whether a delivery's effect is still unknown.
type Summary struct {
	Schema            string `json:"schema"`
	State             State  `json:"state"`
	StopReason        State  `json:"stop_reason"`
	DeliveryUncertain bool   `json:"delivery_uncertain"`
	Planned           int    `json:"planned"`
	Recorded          int    `json:"recorded"`
	ResultIdentity    string `json:"result_identity,omitzero"`
	Recovered         bool   `json:"recovered"`
	JournalIncomplete bool   `json:"journal_incomplete"`
}

func (s Summary) ExitCode() int {
	if s.State == Passed {
		return 0
	}
	if s.State == AssertionFailed {
		return 1
	}
	return 2
}

type planDocument struct {
	Schema    string                  `json:"schema"`
	CreatedAt time.Time               `json:"created_at"`
	Inputs    testrunner.PinnedInputs `json:"inputs"`
	Payloads  []payload               `json:"payloads"`
}
type payload struct {
	Path   string `json:"path"`
	Size   int    `json:"size"`
	SHA256 string `json:"sha256"`
}
type entry struct {
	Sequence   int           `json:"sequence"`
	Previous   string        `json:"previous"`
	At         time.Time     `json:"at"`
	Kind       string        `json:"kind"`
	Occurrence string        `json:"occurrence,omitzero"`
	Sent       *payload      `json:"sent,omitzero"`
	Event      *replay.Event `json:"event,omitzero"`
	Final      *Summary      `json:"final,omitzero"`
}

type writer struct {
	journalBytes int
	failed       error
	root         *os.Root
	journal      *os.File
	sequence     int
	previous     string
	summary      Summary
}

// Start is one foreground execution. It requires a fresh destination and never
// resumes an existing job. Each selected payload and effective configuration is
// synced before execution; an intent is synced before each network write.
func Start(ctx context.Context, specPath, output string) (summary Summary, err error) {
	defer func() {
		if err != nil && summary.Schema != "" {
			summary.StopReason = ExecutionError
			if errors.Is(ctx.Err(), context.Canceled) {
				summary.StopReason = Cancelled
			}
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				summary.StopReason = TimedOut
			}
			summary.State = summary.StopReason
			if summary.DeliveryUncertain {
				summary.State = DeliveryUncertain
			}
			summary.JournalIncomplete = true
		}
	}()
	plan, err := testrunner.Prepare(specPath)
	if err != nil {
		return Summary{}, err
	}
	output, err = plan.DurableDestination(output)
	if err != nil {
		return Summary{}, err
	}
	if err = os.Mkdir(output, 0700); err != nil {
		return Summary{}, errors.New("cannot create durable run; destination must be new")
	}
	root, err := os.OpenRoot(output)
	if err != nil {
		return Summary{}, errors.New("cannot open durable run")
	}
	defer root.Close()
	doc := planDocument{Schema: Schema, CreatedAt: time.Now().UTC(), Inputs: plan.PinnedInputs(), Payloads: []payload{}}
	if err = root.Mkdir("intended", 0700); err != nil {
		return Summary{}, errors.New("cannot retain durable plan")
	}
	if err = root.Mkdir("sent", 0700); err != nil {
		return Summary{}, errors.New("cannot retain durable evidence")
	}
	for _, m := range doc.Inputs.Mappings {
		raw, e := plan.Outbound(m.OutboundOccurrence)
		if e != nil {
			return Summary{}, e
		}
		name := "intended/" + m.OutboundOccurrence + ".bin"
		if err = write(root, name, raw); err != nil {
			return Summary{}, err
		}
		doc.Payloads = append(doc.Payloads, payload{name, len(raw), digest(raw)})
	}
	raw, err := json.Marshal(doc, json.Deterministic(true))
	if err != nil || len(raw) > maxPlan {
		return Summary{}, errors.New("cannot encode bounded durable plan")
	}
	if err = write(root, "plan.json", raw); err != nil {
		return Summary{}, err
	}
	f, err := root.OpenFile("journal.jsonl", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return Summary{}, errors.New("cannot create durable journal")
	}
	defer f.Close()
	w := &writer{root: root, journal: f, previous: digest(raw), summary: Summary{Schema: Schema, State: Ready, StopReason: Ready, Planned: plan.Count()}}
	if err = w.append(entry{Kind: "ready"}); err != nil {
		return w.summary, err
	}
	// Directory entries must reach stable storage too, before any network effect.
	if err = syncDirectory(root, "intended"); err != nil {
		return w.summary, err
	}
	if err = syncDirectory(root, "."); err != nil {
		return w.summary, err
	}
	parent, err := os.OpenRoot(filepath.Dir(output))
	if err != nil {
		return w.summary, errors.New("cannot sync durable run parent")
	}
	err = syncDirectory(parent, ".")
	parent.Close()
	if err != nil {
		return w.summary, err
	}
	w.summary.State = Running
	w.summary.StopReason = Running
	if err = w.append(entry{Kind: "running"}); err != nil {
		return w.summary, err
	}
	artifact, _ := testrunner.ExecuteObserved(ctx, plan, filepath.Join(output, "result"), w)
	stop := ExecutionError
	if artifact != nil {
		switch artifact.Result.Status {
		case testrunner.Pass:
			stop = Passed
		case testrunner.AssertionFailure:
			stop = AssertionFailed
		}
		for _, name := range []string{"result/run", "result", "."} {
			// Configuration refusal can produce a result without a replay directory.
			if name == "result/run" && artifact.Run == nil {
				continue
			}
			if err := syncDirectory(root, name); err != nil {
				return w.summary, err
			}
		}
		w.summary.ResultIdentity = artifact.Identity
		if artifact.Run != nil {
			for _, event := range artifact.Run.Events {
				if event.Outcome == replay.Cancelled {
					stop = Cancelled
				}
				if event.Outcome == replay.Timeout {
					stop = TimedOut
				}
			}
		}
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		stop = Cancelled
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		stop = TimedOut
	}
	w.summary.StopReason = stop
	w.summary.State = stop
	if w.summary.DeliveryUncertain {
		w.summary.State = DeliveryUncertain
	}
	if err = w.append(entry{Kind: "finished", Final: &w.summary}); err != nil {
		return w.summary, err
	}
	// A retained execution error is a result, not an unstructured loss of status.
	return w.summary, nil
}
func (w *writer) BeforeSend(id string) error {
	// Persist replay directory entries (and the decision) before a durable intent
	// can attest that the frozen source/intended files exist.
	for _, name := range []string{"result/run/payloads", "result/run", "result", "."} {
		if err := syncDirectory(w.root, name); err != nil {
			return err
		}
	}
	// Set before attempting persistence: failure may leave only partial intent.
	w.summary.DeliveryUncertain = true
	return w.append(entry{Kind: "intent", Occurrence: id})
}
func (w *writer) Sent(id string, raw []byte) error {
	name := "sent/" + id + ".bin"
	if err := write(w.root, name, raw); err != nil {
		return err
	}
	if err := syncDirectory(w.root, "sent"); err != nil {
		return err
	}
	return w.append(entry{Kind: "sent", Occurrence: id, Sent: &payload{name, len(raw), digest(raw)}})
}
func (w *writer) Recorded(event replay.Event) error {
	for _, name := range []string{"result/run/payloads", "result/run", "result"} {
		if err := syncDirectory(w.root, name); err != nil {
			return err
		}
	}
	if err := w.append(entry{Kind: "recorded", Occurrence: event.OutboundOccurrence, Event: &event}); err != nil {
		return err
	}
	w.summary.Recorded++
	// Transport halts after the first error; only a matched ACK resolves intent.
	if event.Delivery == "acknowledged" {
		w.summary.DeliveryUncertain = false
	}
	return nil
}
