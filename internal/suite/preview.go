package suite

import (
	"encoding/json/v2"
	"errors"
	"slices"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/engine"
	"github.com/bharm16/readmit/internal/expectation"
	"github.com/bharm16/readmit/internal/testrunner"
)

// Expansion is the exact expansion of one suite against one environment,
// decided by the same rules prepare applies, without writing anything. It is
// the preview a window shows before a configuration directory is created: the
// exact jobs, their effective inputs and bindings, the release pins in force
// and the resource serialization the queue will hold.
type Expansion struct {
	Suite       ExpansionSuite `json:"suite"`
	Environment Environment    `json:"environment"`
	Engine      string         `json:"engine"`
	Releases    []ReleasePin   `json:"releases"`
	Jobs        []ExpandedJob  `json:"jobs"`
	Order       string         `json:"order"`
	Sharing     string         `json:"sharing"`
}

// ExpansionSuite restates the suite identity the expansion belongs to.
type ExpansionSuite struct {
	ID          string   `json:"id"`
	Owner       string   `json:"owner"`
	Tags        []string `json:"tags"`
	Parallelism int      `json:"parallelism"`
}

// ReleasePin names one suite test and the exact released expectation identity
// its template is held to. An ordinary suite carries none.
type ReleasePin struct {
	Test     string `json:"test"`
	Identity string `json:"identity"`
}

// ExpandedJob is one TEST-ROW job exactly as preparation would compile it. The
// paths are the effective local inputs after environment bindings and row
// references resolve; Sequence is the declared send order, which the template
// and the prepared evidence must both match.
type ExpandedJob struct {
	ID          string   `json:"id"`
	Test        string   `json:"test"`
	Row         string   `json:"row"`
	Spec        string   `json:"spec"`
	Case        string   `json:"case"`
	Target      string   `json:"target"`
	Boundary    string   `json:"boundary"`
	Observation string   `json:"observation,omitzero"`
	Isolation   string   `json:"isolation"`
	After       []string `json:"after"`
	Sequence    []string `json:"sequence"`
	Release     string   `json:"release,omitzero"`
}

const (
	previewOrder = "Jobs appear in the suite's declared test and row order. The selected input order is exact and is never reordered by preview, preparation or execution."
	previewShare = "Jobs declaring shared isolation hold the selected environment and the endpoint its target records for their whole run and are serialized against each other; isolated jobs may overlap within the declared parallelism."
	previewOnly  = "Every job declares isolated state and may overlap within the declared parallelism; no job holds the environment against another."
)

// Preview expands a suite against one environment exactly as preparation
// would, reading the templates to check sequences, bindings and expectation
// overrides, and writes nothing. root is the folder the suite's relative
// references resolve from; references names a readmit-suite-releases/v1
// sidecar whose pins are verified, or is empty for an ordinary suite.
func Preview(root string, doc Document, environment string, references string) (Expansion, error) {
	var expansion Expansion
	queue, err := doc.queue()
	if err != nil {
		return expansion, err
	}
	var selected *Environment
	for i := range doc.Environments {
		if doc.Environments[i].ID == environment {
			selected = &doc.Environments[i]
		}
	}
	if selected == nil {
		return expansion, errors.New("suite does not declare the selected environment")
	}
	var releases map[string]expectation.Release
	if references != "" {
		releases, _, err = loadReleases(references, doc)
		if err != nil {
			return expansion, err
		}
	}
	expansion = Expansion{
		Suite:       ExpansionSuite{ID: doc.ID, Owner: doc.Owner, Tags: doc.Tags, Parallelism: doc.Parallelism},
		Environment: *selected,
		Engine:      engine.Version(),
		Releases:    []ReleasePin{},
		Jobs:        []ExpandedJob{},
		Order:       previewOrder,
		Sharing:     previewOnly,
	}
	for _, test := range doc.Tests {
		if release, approved := releases[test.ID]; approved {
			expansion.Releases = append(expansion.Releases, ReleasePin{Test: test.ID, Identity: release.Identity()})
		}
		templateRaw, err := read(artifactpath.JoinReference(root, test.Spec), testrunner.MaxSpecBytes)
		if err != nil {
			return Expansion{}, err
		}
		if release, approved := releases[test.ID]; approved {
			if err = baselineSpec(templateRaw, release); err != nil {
				return Expansion{}, err
			}
		}
		for _, table := range doc.Tables {
			if table.ID != test.Table {
				continue
			}
			for _, row := range table.Rows {
				spec, err := testrunner.DecodeSpec(templateRaw)
				if err != nil {
					return Expansion{}, err
				}
				if !slices.Equal(spec.Input.Messages, test.Sequence) {
					return Expansion{}, errors.New("suite sequence differs from its template")
				}
				spec.Input.Case = artifactpath.JoinReference(root, row.Case)
				job := ExpandedJob{
					ID: test.ID + "-" + row.ID, Test: test.ID, Row: row.ID, Spec: test.Spec,
					Case: spec.Input.Case, Boundary: spec.Observation.Boundary,
					Isolation: string(test.Isolation), Sequence: test.Sequence, After: []string{},
				}
				for _, binding := range selected.Bindings {
					if binding.Parameter != test.Parameter {
						continue
					}
					job.Target = artifactpath.JoinReference(root, binding.Target)
					spec.Target = job.Target
					if spec.Observation.Boundary == testrunner.LedgerBoundary {
						if binding.Observation == "" {
							return Expansion{}, errors.New("ledger template requires an environment observation path")
						}
						job.Observation = artifactpath.JoinReference(root, binding.Observation)
						spec.Observation.Path = job.Observation
					} else if binding.Observation != "" {
						return Expansion{}, errors.New("ACK template cannot consume an observation binding")
					}
				}
				used := 0
				for i := range spec.Assertions {
					if expected, ok := row.Expected[spec.Assertions[i].ID]; ok {
						if _, approved := releases[test.ID]; approved {
							before, _ := json.Marshal(spec.Assertions[i].Expected, json.Deterministic(true))
							after, _ := json.Marshal(expected, json.Deterministic(true))
							if string(before) != string(after) {
								return Expansion{}, errors.New("suite row changes a released expectation; release a reviewed template for that row")
							}
						}
						spec.Assertions[i].Expected = expected
						used++
					}
				}
				if used != len(row.Expected) {
					return Expansion{}, errors.New("suite row names an assertion its template does not declare")
				}
				encoded, err := json.Marshal(spec, json.Deterministic(true))
				if err != nil {
					return Expansion{}, errors.New("cannot encode suite test")
				}
				if _, err = testrunner.DecodeSpec(encoded); err != nil {
					return Expansion{}, err
				}
				if release, approved := releases[test.ID]; approved {
					job.Release = release.Identity()
				}
				expansion.Jobs = append(expansion.Jobs, job)
			}
		}
	}
	if len(expansion.Jobs) != len(queue.Jobs) {
		return Expansion{}, errors.New("suite expansion is inconsistent with its queue")
	}
	for i, job := range queue.Jobs {
		if expansion.Jobs[i].ID != job.ID || expansion.Jobs[i].Isolation != string(job.Isolation) {
			return Expansion{}, errors.New("suite expansion is inconsistent with its queue")
		}
		expansion.Jobs[i].After = append(expansion.Jobs[i].After, job.After...)
		if job.Isolation == "shared" {
			expansion.Sharing = previewShare
		}
	}
	return expansion, nil
}
