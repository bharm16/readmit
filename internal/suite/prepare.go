package suite

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/expectation"
	"github.com/bharm16/readmit/internal/runqueue"
	"github.com/bharm16/readmit/internal/testrunner"
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

// Prepare resolves every row before publishing queue.json. It never sends.
// A failed preparation removes only its own new configuration directory; once
// returned, the directory is retained and every run creates separate evidence.
func Prepare(path, environment, output string) (prepared Prepared, err error) {
	return prepare(path, environment, output, "", "")
}

// PrepareApproved verifies an explicit pin for every template before expanding rows.
func PrepareApproved(path, environment, output, references string) (Prepared, error) {
	if references == "" {
		return Prepared{}, errors.New("released suite requires references")
	}
	return prepare(path, environment, output, references, "")
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
	queue, err := doc.queue()
	if err != nil {
		return prepared, err
	}
	for _, test := range doc.Tests {
		templateRaw, e := read(artifactpath.JoinReference(root, test.Spec), testrunner.MaxSpecBytes)
		if e != nil {
			return prepared, e
		}
		if r, approved := releases[test.ID]; approved {
			if e := baselineSpec(templateRaw, r); e != nil {
				return prepared, e
			}
			encoded, e := r.Encode()
			if e != nil {
				return prepared, e
			}
			if e := retain(out, "release-"+test.ID+".json", encoded); e != nil {
				return prepared, e
			}
		}
		for _, table := range doc.Tables {
			if table.ID != test.Table {
				continue
			}
			for _, row := range table.Rows {
				spec, e := testrunner.DecodeSpec(templateRaw)
				if e != nil {
					return prepared, e
				}
				if !slices.Equal(spec.Input.Messages, test.Sequence) {
					return prepared, errors.New("suite sequence differs from its template")
				}
				spec.Input.Case = artifactpath.JoinReference(root, row.Case)
				for _, binding := range selected.Bindings {
					if binding.Parameter == test.Parameter {
						spec.Target = artifactpath.JoinReference(root, binding.Target)
						if spec.Observation.Boundary == testrunner.LedgerBoundary {
							if binding.Observation == "" {
								return prepared, errors.New("ledger template requires an environment observation path")
							}
							spec.Observation.Path = artifactpath.JoinReference(root, binding.Observation)
						} else if binding.Observation != "" {
							return prepared, errors.New("ACK template cannot consume an observation binding")
						}
					}
				}
				used := 0
				for i := range spec.Assertions {
					if expected, ok := row.Expected[spec.Assertions[i].ID]; ok {
						if _, approved := releases[test.ID]; approved {
							before, _ := json.Marshal(spec.Assertions[i].Expected, json.Deterministic(true))
							after, _ := json.Marshal(expected, json.Deterministic(true))
							if string(before) != string(after) {
								return prepared, errors.New("suite row changes a released expectation; release a reviewed template for that row")
							}
						}
						spec.Assertions[i].Expected = expected
						used++
					}
				}
				if used != len(row.Expected) {
					return prepared, errors.New("suite row names an assertion its template does not declare")
				}
				encoded, e := json.Marshal(spec, json.Deterministic(true))
				if e != nil {
					return prepared, errors.New("cannot encode suite test")
				}
				if _, e = testrunner.DecodeSpec(encoded); e != nil {
					return prepared, e
				}
				name := test.ID + "-" + row.ID + ".json"
				if e = retain(out, name, encoded); e != nil {
					return prepared, e
				}
				plan, e := testrunner.Prepare(filepath.Join(out, name))
				if e != nil {
					return prepared, e
				}
				mappings := plan.PinnedInputs().Mappings
				if len(mappings) != len(test.Sequence) {
					return prepared, errors.New("suite sequence does not match selected evidence")
				}
				for i, mapping := range mappings {
					if mapping.SourceOccurrence != test.Sequence[i] {
						return prepared, errors.New("suite sequence differs from evidence send order")
					}
				}
			}
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

// Run expands one selected environment and delegates every scheduling and send
// decision to runqueue. Existing output is refused, never resumed or resent.
func Run(ctx context.Context, path, environment, output string) (runqueue.Report, error) {
	return run(ctx, path, environment, output, "", "")
}

func RunApproved(ctx context.Context, path, environment, output, references string) (runqueue.Report, error) {
	if references == "" {
		return runqueue.Report{}, errors.New("released suite requires references")
	}
	return run(ctx, path, environment, output, references, "")
}

// Identity is the identity of one suite document: the SHA-256 of its exact
// bytes, as a promotion review records the suite it reviewed. A preview reports
// it, and RunPinned executes only a document that still has it.
func Identity(raw []byte) string { return promotionHash(raw) }

// ErrChanged refuses a pinned run whose suite document no longer has the
// identity it was pinned to.
var ErrChanged = errors.New("the suite differs from the identity it was pinned to")

// RunPinned is Run — or RunApproved, when references are given — for a caller
// that previewed the suite: it executes only while the document at path still
// has identity, checked on the bytes it then compiles, so there is no gap
// between the check and the read. An empty or different identity refuses with
// ErrChanged before the output is created or anything is sent.
func RunPinned(ctx context.Context, path, environment, output, references, identity string) (runqueue.Report, error) {
	if identity == "" {
		return runqueue.Report{}, ErrChanged
	}
	return run(ctx, path, environment, output, references, identity)
}

func run(ctx context.Context, path, environment, output, references, identity string) (runqueue.Report, error) {
	prepared, err := prepare(path, environment, output, references, identity)
	if err != nil {
		return runqueue.Report{}, err
	}
	return runPrepared(ctx, prepared, nil)
}

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
