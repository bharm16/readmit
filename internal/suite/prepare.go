package suite

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/expectation"
	"github.com/bharm16/readmit/internal/runqueue"
)

// Prepared is a new private directory containing ordinary test specs and a
// queue. It carries no secret values and authorizes no network operation.
type Prepared struct {
	Directory string
	Queue     runqueue.Plan
}

func read(path string, max int) ([]byte, error) {
	input := suiteInput
	input.MaxBytes = max
	return input.Read(path)
}

// suiteInput is how a suite input is read: never through a link, and never
// past its bound.
var suiteInput = artifactdir.Document{
	Refusals: artifactdir.DocumentRefusals{
		Irregular: errors.New("suite input must be a bounded regular file"),
		Open:      errors.New("cannot open suite input"),
		Changed:   errors.New("suite input changed while opening"),
		Read:      errors.New("cannot read bounded suite input"),
		Size:      errors.New("suite input must be a bounded regular file"),
	},
}

func retain(dir, name string, raw []byte) error {
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return errors.New("cannot create suite configuration")
	}
	n, err := f.Write(raw)
	if err == nil && n != len(raw) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = f.Sync()
	}
	closed := f.Close()
	if err != nil || closed != nil {
		return errors.New("cannot persist suite configuration")
	}
	return nil
}

// Request is one suite operation: the document at Path expanded against the
// declared Environment into the new directory Output, with optional approved
// released-expectation references, an approved environment promotion, and the
// document identity a preview fixed. The optional pins are the whole choice
// among the former ordinary, approved, promoted and pinned variants: every
// caller — the command line, CI and the desktop — states one request.
type Request struct {
	Path, Environment, Output string
	// References names a readmit-suite-releases/v1 sidecar; when set, every
	// template and expectation is verified against its exact released pins.
	References string
	// Promotion, with its exact identity and the revision assumption it was
	// reviewed under, executes an approved promotion. All four promotion
	// members stand or fall together and cannot be combined with Identity.
	Promotion, PromotionIdentity, Revision string
	// Identity pins the operation to the suite document a preview reported:
	// a document that no longer has it is refused with ErrChanged before the
	// output exists.
	Identity string
}

// Validate refuses a request whose optional pins are incomplete or mutually
// exclusive, before any suite is read.
func (r Request) Validate() error {
	if r.Path == "" || r.Environment == "" || r.Output == "" {
		return errors.New("a suite operation requires the suite path, environment and a new output directory")
	}
	if (r.Promotion == "" && (r.PromotionIdentity != "" || r.Revision != "")) || (r.Promotion != "" && (r.PromotionIdentity == "" || r.Revision == "" || r.References == "")) {
		return errors.New("a promoted suite requires its promotion, identity, revision assumption and released references together")
	}
	if r.Promotion != "" && r.Identity != "" {
		return errors.New("a promoted suite is pinned by its promotion identity, not by a suite document identity")
	}
	return nil
}

// Prepare resolves every row of the request's suite before publishing
// queue.json, writing the one expansion this package decides. It never sends.
// A failed preparation removes only its own new configuration directory; once
// returned, the directory is retained and every run creates separate evidence.
func Prepare(request Request) (Prepared, error) {
	if err := request.Validate(); err != nil {
		return Prepared{}, err
	}
	if request.Promotion != "" {
		return Prepared{}, errors.New("a promoted suite is executed through Run, never prepared")
	}
	return prepare(request.Path, request.Environment, request.Output, request.References, request.Identity)
}

// prepare compiles the suite at path. A non-empty identity pins it: the
// document read here, once, is the one compiled, and it must have that
// identity, so a document rewritten after a preview is refused before the
// output exists.
func prepare(path, environment, output, references, identity string) (prepared Prepared, err error) {
	raw, err := read(path, MaxBytes)
	if err != nil {
		return prepared, err
	}
	if identity != "" && Identity(raw) != identity {
		return prepared, ErrChanged
	}
	resolved, err := artifactpath.Resolve(path)
	if err != nil {
		return prepared, err
	}
	doc, err := Decode(raw)
	if err != nil {
		return prepared, err
	}
	var releases map[string]expectation.Release
	var referencesRaw []byte
	if references != "" {
		releases, referencesRaw, err = loadReleases(references, doc)
		if err != nil {
			return prepared, err
		}
	}
	var selected *Environment
	for i := range doc.Environments {
		if doc.Environments[i].ID == environment {
			selected = &doc.Environments[i]
		}
	}
	if selected == nil {
		return prepared, errors.New("suite does not declare the selected environment")
	}
	root := filepath.Dir(resolved)
	out, err := artifactpath.Destination(output)
	if err != nil {
		return prepared, err
	}
	if err = os.Mkdir(out, 0700); err != nil {
		return prepared, errors.New("suite output must be a new directory")
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.RemoveAll(out)
		}
	}()
	_, queue, compiled, err := expand(root, doc, *selected, releases)
	if err != nil {
		return prepared, err
	}
	for _, test := range doc.Tests {
		if release, approved := releases[test.ID]; approved {
			encoded, e := release.Encode()
			if e != nil {
				return prepared, e
			}
			if e := retain(out, "release-"+test.ID+".json", encoded); e != nil {
				return prepared, e
			}
		}
	}
	for _, job := range compiled {
		if e := retain(out, job.Name, job.Raw); e != nil {
			return prepared, e
		}
	}
	if references != "" {
		if err = retain(out, "release-references.json", referencesRaw); err != nil {
			return prepared, err
		}
	}
	if err = retainSelection(out, doc, *selected); err != nil {
		return prepared, err
	}
	if err = retain(out, "suite.json", raw); err != nil {
		return prepared, err
	}
	if err = os.Mkdir(filepath.Join(out, "runs"), 0700); err != nil {
		return prepared, errors.New("cannot create suite runs directory")
	}
	queueRaw, err := json.Marshal(queue, json.Deterministic(true))
	if err != nil {
		return prepared, err
	}
	if err = retain(out, "queue.json", queueRaw); err != nil {
		return prepared, err
	}
	complete = true
	return Prepared{Directory: out, Queue: queue}, nil
}

// Run expands the request's suite once and delegates every scheduling and
// send decision to runqueue. Existing output is refused, never resumed or
// resent. A request carrying Identity executes only while the document at
// Path still has it, checked on the bytes it then compiles, so there is no
// gap between the check and the read and ErrChanged refuses before the output
// is created or anything is sent; a request carrying the promotion members
// executes an approved promotion (and requires References).
func Run(ctx context.Context, request Request) (runqueue.Report, error) {
	if err := request.Validate(); err != nil {
		return runqueue.Report{}, err
	}
	if request.Promotion != "" {
		return runPromoted(ctx, request)
	}
	prepared, err := prepare(request.Path, request.Environment, request.Output, request.References, request.Identity)
	if err != nil {
		return runqueue.Report{}, err
	}
	return runPrepared(ctx, prepared, nil)
}

// Identity is the identity of one suite document: the SHA-256 of its exact
// bytes, as a promotion review records the suite it reviewed. A preview
// reports it, and a run pinned with it executes only a document that still
// has it.
func Identity(raw []byte) string { return promotionHash(raw) }

// ErrChanged refuses a pinned run whose suite document no longer has the
// identity it was pinned to.
var ErrChanged = errors.New("the suite differs from the identity it was pinned to")

func runPrepared(ctx context.Context, prepared Prepared, pins map[string]string) (runqueue.Report, error) {
	raw, err := json.Marshal(prepared.Queue, json.Deterministic(true))
	if err != nil {
		return runqueue.Report{}, err
	}
	report, err := runqueue.Run(ctx, runqueue.Request{PlanBytes: raw, PlanDirectory: prepared.Directory, Runs: filepath.Join(prepared.Directory, "runs"), ApprovedInputs: pins})
	if err != nil {
		return report, err
	}
	encoded, err := json.Marshal(report, json.Deterministic(true))
	if err != nil {
		return report, err
	}
	if err = retain(prepared.Directory, "report.json", encoded); err != nil {
		return report, err
	}
	return report, nil
}
