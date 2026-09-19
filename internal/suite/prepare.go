package suite

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"

	"github.com/bharm16/readmit/internal/artifactpath"
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
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > int64(max) {
		return nil, errors.New("suite input must be a bounded regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("cannot open suite input")
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return nil, errors.New("suite input changed while opening")
	}
	raw, err := io.ReadAll(io.LimitReader(f, int64(max)+1))
	if err != nil || len(raw) > max {
		return nil, errors.New("cannot read bounded suite input")
	}
	return raw, nil
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
	raw, err := read(path, MaxBytes)
	if err != nil {
		return prepared, err
	}
	resolved, err := artifactpath.Resolve(path)
	if err != nil {
		return prepared, err
	}
	doc, err := Decode(raw)
	if err != nil {
		return prepared, err
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
	prepared, err := Prepare(path, environment, output)
	if err != nil {
		return runqueue.Report{}, err
	}
	raw, err := json.Marshal(prepared.Queue, json.Deterministic(true))
	if err != nil {
		return runqueue.Report{}, err
	}
	report, err := runqueue.Run(ctx, runqueue.Request{PlanBytes: raw, PlanDirectory: prepared.Directory, Runs: filepath.Join(prepared.Directory, "runs")})
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
