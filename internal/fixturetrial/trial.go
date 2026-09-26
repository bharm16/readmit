// Package fixturetrial runs one test spec against a freshly bound built-in
// fixture and verifies that a retained acknowledgement is the canonical one
// that fixture answers with. It is the one home of "send this spec to the
// built-in fixture in this mode", so report trials, redaction proofs and the
// guided sample's practice runs cannot grow their own copies of the lifecycle
// or of the fixture's wire receipt.
package fixturetrial

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/receiver"
	"github.com/bharm16/readmit/internal/testrunner"
)

// trialFrameBytes is the largest frame one trial's fixture reads. Every trial
// sends the built-in fixture's own synthetic evidence, which stays far inside
// this bound.
const trialFrameBytes = 1 << 20

// resultName is the directory the sender retains its result under, inside the
// session directory.
const resultName = "result"

// specName and targetName are the executed configuration files of one session.
const (
	specName   = "spec.json"
	targetName = "target.json"
)

// Sender runs one spec against its target and retains the result. It is this
// module's one stall seam: tests stand in a sender whose own evidence storage
// stalls, or one that reports to an observer. The default is the ordinary test
// runner, writing at the trial's durability.
var Sender = func(ctx context.Context, specPath, output string, durability artifactdir.Durability) (*testrunner.Artifact, error) {
	return testrunner.RunWithDurability(ctx, specPath, output, durability)
}

// Session is one trial's session: a freshly bound loopback fixture and the
// paths its executed configuration and retained result take.
type Session struct {
	address string
	dir     string
}

// Address is the fixture's loopback endpoint. The target configuration a
// session writes must carry it, so the send reaches the fixture this trial
// bound and nothing else.
func (s Session) Address() string { return s.address }

// SpecPath and ResultPath are where the executed spec is written and where the
// sender retains its result.
func (s Session) SpecPath() string   { return s.dir + "/" + specName }
func (s Session) ResultPath() string { return s.dir + "/" + resultName }

// Trial is one run of a test spec against a freshly bound built-in fixture.
type Trial struct {
	// Mode is the built-in fixture behavior the spec runs against.
	Mode observation.Mode
	// Dir is the session directory. The executed target and spec are written
	// into it, the fixture installs its case and ledger beside them, and the
	// sender retains its result under "result".
	Dir string
	// CasePath and ObservationPath are where the fixture installs its case and
	// its live ledger. Both must be new.
	CasePath, ObservationPath string
	// Durability is what the fixture's case, its ledger and the retained result
	// are written with. A trial whose session directory its owner removes
	// before answering is Scratch; a session another process reads is Durable.
	Durability artifactdir.Durability
	// Budget bounds one trial end to end. The fixture waits this long for the
	// sender, which syncs the evidence it has just recorded before sending the
	// next message, and the send runs under this deadline.
	Budget time.Duration
	// Configure writes the executed target and spec into the session once the
	// fixture's address is bound. It is the caller's, because each keeps its
	// own document store, encoding and file layout; the target it writes must
	// carry Session.Address.
	Configure func(Session) error
	// Spec is the executed copy. The caller rebinds its case, target and
	// observation references onto the session before handing it over.
	Spec testrunner.Spec
}

// Outcome is what one trial left behind.
type Outcome struct {
	// Artifact is what the sender retained, nil when the send never completed.
	Artifact *testrunner.Artifact
	// SenderErr is the send's own failure. ServeErr is the fixture's, whose
	// evidence the send may already have been told about.
	SenderErr, ServeErr error
	// ConfigErr is a failure writing the executed configuration, before any
	// bytes moved.
	ConfigErr error
}

// Run sends one spec against a freshly bound built-in fixture in mode. It
// binds 127.0.0.1:0, installs the fixture in process, lets Configure write the
// executed target and spec, serves the fixture under the budget, sends, and
// then cancels and waits, so the fixture's case is finalized before the
// outcome is read. The fixture always runs in process: its live ledger is read
// back only by this process, and flushing it before each ACK would put the
// disk's latency inside the target's message timeout.
func Run(ctx context.Context, trial Trial) Outcome {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return Outcome{ServeErr: errors.New("cannot bind a loopback fixture")}
	}
	defer listener.Close()
	fixture, err := receiver.New(receiver.Config{
		Mode: trial.Mode, OutputPath: trial.CasePath, ObservationPath: trial.ObservationPath,
		MaxMessages: len(trial.Spec.Input.Messages), MaxFrameBytes: trialFrameBytes,
		IdleTimeout: trial.Budget, InProcess: true, Durability: trial.Durability,
	})
	if err != nil {
		return Outcome{ServeErr: err}
	}
	session := Session{address: listener.Addr().String(), dir: trial.Dir}
	if trial.Configure != nil {
		if err := trial.Configure(session); err != nil {
			return Outcome{ConfigErr: err}
		}
	}
	trialCtx, cancel := context.WithTimeout(ctx, trial.Budget)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := fixture.Serve(trialCtx, listener); done <- err }()
	// New has installed the empty observation before the sender can connect.
	artifact, senderErr := Sender(trialCtx, session.SpecPath(), session.ResultPath(), trial.Durability)
	cancel()
	serveErr := <-done
	return Outcome{Artifact: artifact, SenderErr: senderErr, ServeErr: serveErr}
}
