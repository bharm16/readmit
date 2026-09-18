// Package durablerun retains a local execution journal around the frozen test
// and replay artifacts. Opening a job is read-only recovery, never execution.
package durablerun

import (
	"bytes"
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

// RecoverySchema is the read-side classification of one job. It is a separate
// document beside readmit-job/v1, which gains no member.
const RecoverySchema = "readmit-run-recovery/v1"

// What recovery established about one planned occurrence. NotAttempted means no
// intent was ever synced for it, so a new run repeating it repeats work that
// never touched the receiver. Acknowledged means a matched ACK was recorded.
// Uncertain means an intent was synced and no acknowledged outcome followed:
// bytes may have reached the receiver, and the send is never repeated.
const (
	NotAttempted = "not_attempted"
	Acknowledged = "acknowledged"
	Uncertain    = "uncertain"
)

// Occurrence names one planned outbound occurrence and what is known about it.
type Occurrence struct {
	ID       string `json:"occurrence"`
	Delivery string `json:"delivery"`
}

// Lease states. Held: the lease document is present and the journal records no
// completion, so the writer may still hold the run's resources. Released: no
// lease document is present. Stale: the document is present but the journal
// recorded a terminal state, so nothing holds it and cleanup may remove it.
const (
	LeaseHeld     = "held"
	LeaseReleased = "released"
	LeaseStale    = "stale"
)

// Recovery is what one read of a job establishes. Terminal is true only when a
// complete terminal record was read and nothing follows it. SafeToRepeat is
// true only when the run recorded its completion and no intent was ever
// synced; ResumeRefusal states why otherwise.
type Recovery struct {
	Schema        string       `json:"schema"`
	Run           Summary      `json:"run"`
	Terminal      bool         `json:"terminal"`
	Occurrences   []Occurrence `json:"occurrences"`
	NotAttempted  int          `json:"not_attempted"`
	Acknowledged  int          `json:"acknowledged"`
	Uncertain     int          `json:"uncertain"`
	Lease         string       `json:"lease"`
	SafeToRepeat  bool         `json:"safe_to_repeat"`
	ResumeRefusal string       `json:"resume_refusal,omitzero"`
}

// LeaseSchema is the document a run holds beside its journal while it may be
// using its declared resources. It is the seam a scheduler admits against: a
// lease is held while it is present and the journal records no completion.
// Nothing in this release reads leases across jobs or refuses admission.
const LeaseSchema = "readmit-run-lease/v1"

// Resource kinds a run declares. An environment is the named nonproduction
// environment whose fixture state the run changes; an endpoint is the address
// the run connects to. A run without a named environment declares only the
// endpoint.
const (
	EnvironmentResource = "environment"
	EndpointResource    = "endpoint"
)

type Resource struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// Holder identifies the process that wrote the lease. It is not proof that the
// process is alive: recovery never decides that.
type Holder struct {
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
}

type Lease struct {
	Schema     string     `json:"schema"`
	Holder     Holder     `json:"holder"`
	DeadlineAt time.Time  `json:"deadline_at,omitzero"`
	Resources  []Resource `json:"resources"`
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

// writer's two sticky failures are distinct. failed means the journal can take
// no further record, so nothing after it is recorded. halted means an evidence
// write failed, so no further intent is accepted, while the journal may still
// record how the run stopped.
type writer struct {
	journalBytes int
	failed       error
	halted       error
	finished     bool
	root         *os.Root
	journal      evidenceFile
	sequence     int
	previous     string
	summary      Summary
}

// Start is one foreground execution. It requires a fresh destination and never
// resumes an existing job. Each selected payload and effective configuration is
// synced before execution; an intent is synced before each network write. A
// deadline on ctx is the run's deadline: reaching it stops new sends and is
// recorded as timed_out, with any in-flight delivery left uncertain.
func Start(ctx context.Context, specPath, output string) (Summary, error) {
	plan, err := testrunner.Prepare(specPath)
	if err != nil {
		return Summary{}, err
	}
	return start(ctx, plan, output)
}

func start(ctx context.Context, plan *testrunner.Plan, output string) (summary Summary, err error) {
	var w *writer
	defer func() {
		if err != nil && summary.Schema != "" && (w == nil || !w.finished) {
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
	if err = writeLease(ctx, root, plan); err != nil {
		return Summary{}, err
	}
	// The lease is released when this process stops, however it stops. A
	// crash leaves it, and recovery reports it held until the journal says
	// otherwise.
	defer root.Remove("lease.json")
	f, err := openEvidence(root, "journal.jsonl")
	if err != nil {
		return Summary{}, errors.New("cannot create durable journal")
	}
	defer f.Close()
	w = &writer{root: root, journal: f, previous: digest(raw), summary: Summary{Schema: Schema, State: Ready, StopReason: Ready, Planned: plan.Count()}}
	if err = w.append(entry{Kind: "ready"}); err != nil {
		return w.summary, err
	}
	// Directory entries must reach stable storage too, before any network effect.
	if err = SyncDirectory(root, "intended"); err != nil {
		return w.summary, err
	}
	if err = SyncDirectory(root, "."); err != nil {
		return w.summary, err
	}
	parent, err := os.OpenRoot(filepath.Dir(output))
	if err != nil {
		return w.summary, errors.New("cannot sync durable run parent")
	}
	err = SyncDirectory(parent, ".")
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
			if err := SyncDirectory(root, name); err != nil {
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
	w.finished = true
	// A retained execution error is a result, not an unstructured loss of
	// status. A failed evidence write is both: the journal says how the run
	// stopped, and the caller is told why.
	return w.summary, w.halted
}

func writeLease(ctx context.Context, root *os.Root, plan *testrunner.Plan) error {
	lease := Lease{Schema: LeaseSchema, Holder: Holder{PID: os.Getpid(), StartedAt: time.Now().UTC()}, Resources: []Resource{}}
	if deadline, ok := ctx.Deadline(); ok {
		lease.DeadlineAt = deadline.UTC()
	}
	if name := plan.Environment().Name; name != "" {
		lease.Resources = append(lease.Resources, Resource{Kind: EnvironmentResource, Name: name})
	}
	lease.Resources = append(lease.Resources, Resource{Kind: EndpointResource, Name: plan.Target().Address})
	raw, err := json.Marshal(lease, json.Deterministic(true))
	if err != nil || len(raw) > maxLease {
		return errors.New("cannot encode durable lease")
	}
	return write(root, "lease.json", raw)
}

// leaseState reads the lease beside a journal, if one is present. A lease that
// is present but unreadable is refused like any other changed evidence.
func leaseState(root *os.Root, terminal bool) (string, error) {
	if _, err := root.Lstat("lease.json"); err != nil {
		return LeaseReleased, nil
	}
	raw, err := read(root, "lease.json", maxLease)
	if err != nil {
		return "", err
	}
	var lease Lease
	if json.Unmarshal(raw, &lease, json.RejectUnknownMembers(true)) != nil || lease.Schema != LeaseSchema || lease.Holder.StartedAt.IsZero() || len(lease.Resources) == 0 {
		return "", errors.New("durable lease is invalid")
	}
	if terminal {
		return LeaseStale, nil
	}
	return LeaseHeld, nil
}

func (w *writer) BeforeSend(id string) error {
	if w.halted != nil {
		return w.halted
	}
	// Persist replay directory entries (and the decision) before a durable intent
	// can attest that the frozen source/intended files exist.
	for _, name := range []string{"result/run/payloads", "result/run", "result", "."} {
		if err := SyncDirectory(w.root, name); err != nil {
			return err
		}
	}
	// Set before attempting persistence: failure may leave only partial intent.
	w.summary.DeliveryUncertain = true
	err := w.append(entry{Kind: "intent", Occurrence: id})
	if errors.Is(err, errJournalLimit) {
		// The limit is checked before any byte is written, so no intent exists
		// and the send it would have preceded was never attempted.
		w.summary.DeliveryUncertain = false
	}
	return err
}
func (w *writer) Sent(id string, raw []byte) error {
	name := "sent/" + id + ".bin"
	if err := write(w.root, name, raw); err != nil {
		w.halted = err
		return err
	}
	if err := SyncDirectory(w.root, "sent"); err != nil {
		w.halted = err
		return err
	}
	return w.append(entry{Kind: "sent", Occurrence: id, Sent: &payload{name, len(raw), digest(raw)}})
}
func (w *writer) Recorded(event replay.Event) error {
	for _, name := range []string{"result/run/payloads", "result/run", "result"} {
		if err := SyncDirectory(w.root, name); err != nil {
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

// ResumeSchema is the output of a resume: which job it repeated, how many
// never-attempted occurrences that was, and the new job's own summary.
const ResumeSchema = "readmit-run-resume/v1"

type Resumption struct {
	Schema      string  `json:"schema"`
	ResumedFrom State   `json:"resumed_from"`
	Repeated    int     `json:"repeated"`
	Run         Summary `json:"run"`
}

// Resume executes the retained plan of a job again, into a new output. It is a
// deliberate action that repeats only never-attempted work: it refuses when
// the job recorded no completion, when any intent was synced without an
// acknowledged outcome, when any delivery was acknowledged, and when the spec
// at specPath no longer prepares the exact plan the job retained. An uncertain
// send is never replayed by this or any other path.
func Resume(ctx context.Context, job, specPath, output string) (Resumption, error) {
	recovery, doc, err := readJob(job)
	if err != nil {
		return Resumption{}, err
	}
	if !recovery.SafeToRepeat {
		return Resumption{}, errors.New("resume refused: " + recovery.ResumeRefusal)
	}
	plan, err := testrunner.Prepare(specPath)
	if err != nil {
		return Resumption{}, err
	}
	current, err := json.Marshal(plan.PinnedInputs(), json.Deterministic(true))
	retained, retainedErr := json.Marshal(doc.Inputs, json.Deterministic(true))
	if err != nil || retainedErr != nil || !bytes.Equal(current, retained) {
		return Resumption{}, errors.New("resume refused: the spec, source or configuration differs from the retained plan; resume repeats only the same work")
	}
	summary, err := start(ctx, plan, output)
	return Resumption{Schema: ResumeSchema, ResumedFrom: recovery.Run.StopReason, Repeated: recovery.NotAttempted, Run: summary}, err
}
