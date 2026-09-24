// Package capturejournal retains a durable record of one multi-connection MLLP
// capture, so a capture interrupted by a crash, a power loss or a kill can be
// read back afterwards instead of disappearing with the process.
//
// It retains three things before the collector answers a peer: the exact bytes
// of every complete inbound frame, the intent to send each acknowledgement, and
// the completion of each send. Recovery reports what was received and which
// acknowledgements have an unknown effect. It never sends, resends or resumes:
// bytes already sent cannot be retracted, so an acknowledgement whose intent
// was synced and whose completion was not is retained as uncertain rather than
// repeated. An unfinished journal proves that finalization was not recorded; it
// never becomes a completed capture.
//
// It answers one question: what did this capture receive, and what is known
// about each acknowledgement it set out to send. It deliberately answers no
// other. In particular a stopped capture states that it stopped, never that
// what it did not capture did not happen; whether a capture's window can
// support a claim that something is absent is decided by internal/observewindow
// against a declared window, and is not restated here.
package capturejournal

import (
	"encoding/json/v2"
	"errors"
	"os"
	"regexp"
	"sync"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/durablelog"
	"github.com/bharm16/readmit/internal/durablerun"
)

// Schema is the contract this journal declares. It is a new artifact beside the
// case bundle and beside readmit-job/v1; neither of those changes here.
const Schema = "readmit-capture-journal/v1"

// Finalized is the terminal state of a capture that stopped in a controlled
// way. Every other state this package reports is durablerun's own vocabulary,
// which a capture shares rather than restates. A capture evaluates no
// assertion, so it never reports passed or assertion_failed: those are verdicts
// about a test, and a capture makes none.
const Finalized durablerun.State = "finalized"

// The acknowledgement stages a capture can report an uncertain send for, and
// the places one can be sent to. They are the collection record's own stage and
// destination names, carried here as written and bounded to the declared set,
// so a journal never records a destination the record could not express.
const (
	AcceptStage      = "accept"
	ApplicationStage = "application"

	SameConnection   = "same-connection"
	SeparateEndpoint = "separate-endpoint"
	NoDestination    = "none"
)

func declaredDestination(destination string) bool {
	return destination == SameConnection || destination == SeparateEndpoint || destination == NoDestination
}

const (
	maxJournalBytes  = 32 << 20
	maxPlanBytes     = 4 << 20
	maxFrameBytes    = 16 << 20
	maxRetainedBytes = 64 << 20
	maxControlBytes  = 1024
)

var (
	// The two identifier shapes below are the ones a case occurrence and a
	// collected connection already have. They are restated here rather than
	// imported, because a journal must read back on its own terms: it is the
	// artifact that survives when the case it was collecting never sealed.
	occurrencePattern = regexp.MustCompile(`^s[0-9]{4}-e[0-9]{6}$`)
	connectionPattern = regexp.MustCompile(`^c[0-9]{4}$`)
	identityPattern   = regexp.MustCompile(`^[0-9a-f]{32}$`)
	labelPattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	schemaPattern     = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}/v[0-9]{1,3}$`)
	digestPattern     = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Limits are the declared capacity this capture served under. They are recorded
// so a recovered capture states what it was allowed to hold, not only what it
// held.
type Limits struct {
	MaxConnections  int `json:"max_connections"`
	MaxSessions     int `json:"max_sessions"`
	MaxMessages     int `json:"max_messages"`
	MaxCaptureBytes int `json:"max_capture_bytes"`
	MaxFrameBytes   int `json:"max_frame_bytes"`
}

// Transport records whether the listener required TLS and whether it required a
// verified client certificate. It is what the capture was configured with; it
// names no file, no key and no certificate subject.
type Transport struct {
	TLS               bool `json:"tls"`
	ClientCertificate bool `json:"client_certificate"`
}

// Capture is the plan a journal opens with. It carries the policy's declared
// identity and a digest of its exact bytes, never the policy document itself
// and never the case destination, so a journal read on its own discloses no
// path and no configured endpoint.
type Capture struct {
	Schema       string    `json:"schema"`
	CreatedAt    time.Time `json:"created_at"`
	SessionID    string    `json:"session_id"`
	PolicyName   string    `json:"policy_name"`
	PolicySchema string    `json:"policy_schema"`
	PolicySHA256 string    `json:"policy_sha256"`
	Limits       Limits    `json:"limits"`
	Transport    Transport `json:"transport"`
}

// Summary is what one capture journal states about itself.
//
// Received counts complete inbound frames whose bytes are retained here.
// Acknowledged counts acknowledgement stages whose send completed. Unsent
// counts stages whose send was attempted and did not complete: that is a
// recorded failure, not an acknowledgement, and some of its bytes may still
// have reached the peer. Uncertain counts stages whose intent was synced and
// whose outcome was never recorded at all: their effect is unknown, and
// recovery never resolves one by sending again.
type Summary struct {
	Schema            string           `json:"schema"`
	State             durablerun.State `json:"state"`
	StopReason        durablerun.State `json:"stop_reason"`
	DeliveryUncertain bool             `json:"delivery_uncertain"`
	Received          int              `json:"received"`
	Acknowledged      int              `json:"acknowledged"`
	Unsent            int              `json:"unsent"`
	Uncertain         int              `json:"uncertain"`
	Recovered         bool             `json:"recovered"`
	JournalIncomplete bool             `json:"journal_incomplete"`
}

// ExitCode is 0 only for a capture that finalized in a controlled way. A
// capture evaluates no assertion, so it never reports the assertion-failure
// code; an interrupted, cancelled, failed or uncertain capture is 2.
func (s Summary) ExitCode() int {
	if s.State == Finalized {
		return 0
	}
	return 2
}

type payload struct {
	Path   string `json:"path"`
	Size   int    `json:"size"`
	SHA256 string `json:"sha256"`
}

type entry struct {
	durablelog.Envelope
	Kind        string   `json:"kind"`
	Session     string   `json:"session,omitzero"`
	Occurrence  string   `json:"occurrence,omitzero"`
	ControlID   string   `json:"control_id,omitzero"`
	Stage       string   `json:"stage,omitzero"`
	Destination string   `json:"destination,omitzero"`
	Frame       *payload `json:"frame,omitzero"`
	Sent        *int     `json:"sent,omitzero"`
	Final       *Summary `json:"final,omitzero"`
}

// Writer appends one capture's journal. Connections are served concurrently, so
// every append is serialized here: the chain a reader verifies is the order the
// records were actually synced in.
type Writer struct {
	mu       sync.Mutex
	path     string
	root     *os.Root
	journal  *os.File
	log      *durablelog.Writer
	retained int
	failed   error
	summary  Summary
}

func digest(b []byte) string { return durablelog.Digest(b) }

// Create reserves a new journal directory and records the capture plan and the
// two opening records before the caller binds a listener. The destination must
// be new: a journal is never appended to an existing one, because a second
// capture's records under one chain could not be told apart.
func Create(path string, capture Capture) (*Writer, error) {
	capture.Schema = Schema
	if err := capture.validate(); err != nil {
		return nil, err
	}
	path, err := reserve(path)
	if err != nil {
		return nil, err
	}
	if err := os.Mkdir(path, 0700); err != nil {
		return nil, errors.New("cannot create capture journal; destination must be new")
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, errors.New("cannot open capture journal")
	}
	if err := root.Mkdir("received", 0700); err != nil {
		root.Close()
		return nil, errors.New("cannot retain captured frames")
	}
	raw, err := json.Marshal(capture, json.Deterministic(true))
	if err != nil || len(raw) > maxPlanBytes {
		root.Close()
		return nil, errors.New("cannot encode bounded capture plan")
	}
	if err := writeFile(root, "capture.json", raw); err != nil {
		root.Close()
		return nil, err
	}
	file, err := root.OpenFile("journal.jsonl", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		root.Close()
		return nil, errors.New("cannot create capture journal file")
	}
	w := &Writer{path: path, root: root, journal: file,
		summary: Summary{Schema: Schema, State: durablerun.Ready, StopReason: durablerun.Ready}}
	w.log = durablelog.NewWriter(file, digest(raw), maxJournalBytes, durablelog.Messages{
		Limit:  errors.New("capture journal reached its size limit; capture stopped"),
		Sync:   errors.New("cannot sync capture journal; capture stopped"),
		Encode: errors.New("cannot encode capture journal record"),
	})
	for _, kind := range []string{"ready", "running"} {
		if err := w.append(entry{Kind: kind}); err != nil {
			w.Close()
			return nil, err
		}
	}
	// Directory entries reach stable storage before any byte is accepted, so a
	// crash cannot leave a journal whose own name was never persisted.
	if artifactdir.SyncDirectory(root, "received") != nil || artifactdir.SyncDirectory(root, ".") != nil {
		w.Close()
		return nil, durablerun.ErrSyncDirectory
	}
	w.summary.State, w.summary.StopReason = durablerun.Running, durablerun.Running
	return w, nil
}

// Received retains one complete inbound frame and records it. It returns only
// after both the bytes and the record are synced, so the collector can answer
// the peer knowing the frame it is answering survives the answer.
func (w *Writer) Received(session, occurrence, controlID string, raw []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.failed != nil {
		return w.failed
	}
	if !connectionPattern.MatchString(session) || !occurrencePattern.MatchString(occurrence) || len(controlID) > maxControlBytes {
		return w.stopLocked("a captured frame names its connection and its occurrence")
	}
	if len(raw) > maxFrameBytes || w.retained+len(raw) > maxRetainedBytes {
		w.failed = errors.New("capture journal reached its retained-frame limit; capture stopped")
		return w.failed
	}
	name := "received/" + occurrence + ".bin"
	if err := writeFile(w.root, name, raw); err != nil {
		w.failed = err
		return err
	}
	if artifactdir.SyncDirectory(w.root, "received") != nil {
		w.failed = durablerun.ErrSyncDirectory
		return durablerun.ErrSyncDirectory
	}
	w.retained += len(raw)
	if err := w.appendLocked(entry{Kind: "received", Session: session, Occurrence: occurrence, ControlID: controlID,
		Frame: &payload{Path: name, Size: len(raw), SHA256: digest(raw)}}); err != nil {
		return err
	}
	w.summary.Received++
	return nil
}

// Intent records that one acknowledgement stage is about to be written. It is
// synced before the write, so a process killed during the write leaves an
// intent with no completion, which recovery reports as uncertain.
func (w *Writer) Intent(session, occurrence, stage, destination string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !declaredDestination(destination) {
		return w.stopLocked("an acknowledgement is sent to a declared destination or to none")
	}
	if err := w.stageLocked(entry{Kind: "intent", Session: session, Occurrence: occurrence, Stage: stage, Destination: destination}); err != nil {
		return err
	}
	w.summary.Uncertain++
	w.summary.DeliveryUncertain = true
	return nil
}

// Sent records that an acknowledgement write completed, with the number of
// bytes the socket reported taking.
func (w *Writer) Sent(session, occurrence, stage string, sent int) error {
	return w.settle("sent", session, occurrence, stage, sent)
}

// Unsent records that an acknowledgement write was attempted and did not
// complete, with the number of bytes that did reach the socket. It resolves the
// intent, because what happened is known; it is never counted as an
// acknowledgement, because the stage was not answered. Bytes already sent
// cannot be retracted, so the count is retained rather than reported as zero.
func (w *Writer) Unsent(session, occurrence, stage string, sent int) error {
	return w.settle("unsent", session, occurrence, stage, sent)
}

func (w *Writer) settle(kind, session, occurrence, stage string, sent int) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if sent < 0 {
		return w.stopLocked("a settled acknowledgement records the bytes the socket took")
	}
	count := sent
	if err := w.stageLocked(entry{Kind: kind, Session: session, Occurrence: occurrence, Stage: stage, Sent: &count}); err != nil {
		return err
	}
	if kind == "sent" {
		w.summary.Acknowledged++
	} else {
		w.summary.Unsent++
	}
	w.summary.Uncertain--
	w.summary.DeliveryUncertain = w.summary.Uncertain > 0
	return nil
}

func (w *Writer) stageLocked(e entry) error {
	if w.failed != nil {
		return w.failed
	}
	if !connectionPattern.MatchString(e.Session) || !occurrencePattern.MatchString(e.Occurrence) || e.Stage != AcceptStage && e.Stage != ApplicationStage {
		return w.stopLocked("an acknowledgement record names its connection, occurrence and stage")
	}
	return w.appendLocked(e)
}

// stopLocked refuses something this journal cannot name, and makes that refusal
// sticky. A journal that was asked to record what it has no way to describe can
// no longer state what the capture did, which is the same standing as a record
// it could not sync: the capture stops rather than continuing unrecorded.
func (w *Writer) stopLocked(reason string) error {
	if w.failed == nil {
		w.failed = errors.New(reason)
	}
	return w.failed
}

// Finish records the terminal summary. The state it is given is where the
// capture stopped; an unresolved send still outranks it, because a stop that
// left an acknowledgement's effect unknown is not a clean finalization.
func (w *Writer) Finish(stop durablerun.State) Summary {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.summary.StopReason = stop
	w.summary.State = stop
	if w.summary.DeliveryUncertain {
		w.summary.State = durablerun.DeliveryUncertain
	}
	if w.failed != nil {
		w.summary.JournalIncomplete = true
		if w.summary.StopReason == Finalized {
			w.summary.StopReason = durablerun.ExecutionError
			w.summary.State = durablerun.ExecutionError
		}
		return w.summary
	}
	final := w.summary
	if err := w.appendLocked(entry{Kind: "finished", Final: &final}); err != nil {
		w.summary.JournalIncomplete = true
	}
	return w.summary
}

// Err is the first durability failure this journal hit, if any. It is sticky:
// once a record could not be synced, the journal no longer states what
// happened, and the capture stops rather than continuing unrecorded.
func (w *Writer) Err() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.failed
}

// Discard closes this journal and removes the directory it created. It is only
// for a capture that could not start at all: a journal that has recorded a
// frame is never removed, because what it holds may be the only surviving
// evidence of a message that arrived.
func (w *Writer) Discard() error {
	w.mu.Lock()
	recorded := w.summary.Received > 0 || w.log.Sequence() > 2
	w.mu.Unlock()
	if recorded {
		return errors.New("a capture journal that recorded a frame is never removed")
	}
	root := w.path
	if err := w.Close(); err != nil {
		return err
	}
	if err := os.RemoveAll(root); err != nil {
		return errors.New("cannot remove the capture journal that was never used")
	}
	return nil
}

func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	var closeErr error
	if w.journal != nil {
		closeErr = w.journal.Close()
		w.journal = nil
	}
	if w.root != nil {
		w.root.Close()
		w.root = nil
	}
	if closeErr != nil {
		return errors.New("cannot close capture journal")
	}
	return nil
}

func (w *Writer) append(e entry) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.appendLocked(e)
}

func (w *Writer) appendLocked(e entry) error {
	if w.failed != nil {
		return w.failed
	}
	if err := w.log.Append(&e); err != nil {
		// The log's refusals are already the journal's own words and are sticky
		// inside the log; recording them here stops the capture the same way a
		// record it could not name does.
		w.failed = err
	}
	return w.failed
}

func writeFile(root *os.Root, name string, data []byte) error {
	err := artifactdir.WriteFile(root, name, data)
	if errors.Is(err, artifactdir.ErrCreateFile) {
		return errors.New("cannot create capture evidence")
	}
	if err != nil {
		return errors.New("cannot sync capture evidence; partial evidence retained")
	}
	return nil
}
