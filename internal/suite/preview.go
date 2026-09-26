package suite

import (
	"encoding/json/v2"
	"errors"
	"slices"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/engine"
	"github.com/bharm16/readmit/internal/expectation"
	"github.com/bharm16/readmit/internal/runqueue"
	"github.com/bharm16/readmit/internal/testrunner"
)

// Expansion is the exact expansion of one suite against one environment,
// decided by the one expansion rule of this package, without writing
// anything. It is the preview a window shows before a configuration
// directory is created: the exact jobs, their effective inputs and bindings,
// the release pins in force and the resource serialization the queue will
// hold.
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

// ExpandedJob is one TEST-ROW job exactly as preparation compiles it. The
// paths are the effective local inputs after environment bindings and row
// references resolve; Sequence is the declared send order, which the template
// and the selected evidence have both been checked against.
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

// compiledJob is one expanded job's exact compiled specification: the name
// preparation retains it under and the canonical bytes preparation writes.
// The expansion has already validated them against the template, the
// bindings, the expectation overrides and the evidence send order.
type compiledJob struct {
	Name string
	Raw  []byte
}

// expand is the one expansion of a suite: the document, one selected
// environment and the released pins in force, to the ordered jobs and the
// queue that preparation compiles, preview shows and coverage and the gate
// compare against. It reads every template, binds every row, applies every
// expectation override, prepares every compiled job's exact inputs —
// including the send order the selected evidence supports — and refuses
// everything preparation refuses, writing nothing.
func expand(root string, doc Document, selected Environment, releases map[string]expectation.Release) (Expansion, runqueue.Plan, []compiledJob, error) {
	var expansion Expansion
	queue, err := doc.queue()
	if err != nil {
		return expansion, queue, nil, err
	}
	declared, err := doc.declare(selected.ID)
	if err != nil {
		return expansion, queue, nil, err
	}
	if len(declared) != len(queue.Jobs) {
		return expansion, queue, nil, errors.New("suite expansion is inconsistent with its queue")
	}
	expansion = Expansion{
		Suite:       ExpansionSuite{ID: doc.ID, Owner: doc.Owner, Tags: doc.Tags, Parallelism: doc.Parallelism},
		Environment: selected,
		Engine:      engine.Version(),
		Releases:    []ReleasePin{},
		Jobs:        make([]ExpandedJob, 0, len(declared)),
		Order:       previewOrder,
		Sharing:     previewOnly,
	}
	for _, test := range doc.Tests {
		if release, approved := releases[test.ID]; approved {
			expansion.Releases = append(expansion.Releases, ReleasePin{Test: test.ID, Identity: release.Identity()})
		}
	}
	compiled := make([]compiledJob, 0, len(declared))
	for i, declaration := range declared {
		release, approved := releases[declaration.Test]
		templateRaw, err := read(artifactpath.JoinReference(root, declaration.Spec), testrunner.MaxSpecBytes)
		if err != nil {
			return Expansion{}, queue, nil, err
		}
		if approved {
			if err = baselineSpec(templateRaw, release); err != nil {
				return Expansion{}, queue, nil, err
			}
		}
		spec, err := testrunner.DecodeSpec(templateRaw)
		if err != nil {
			return Expansion{}, queue, nil, err
		}
		if !slices.Equal(spec.Input.Messages, declaration.Sequence) {
			return Expansion{}, queue, nil, errors.New("suite sequence differs from its template")
		}
		spec.Input.Case = artifactpath.JoinReference(root, declaration.Case)
		spec.Target = artifactpath.JoinReference(root, declaration.Target)
		observation, err := bindObservation(spec.Observation.Boundary, declaration.Observation)
		if err != nil {
			return Expansion{}, queue, nil, err
		}
		if observation != "" {
			spec.Observation.Path = artifactpath.JoinReference(root, observation)
		}
		used := 0
		for a := range spec.Assertions {
			if expected, ok := declaration.Expected[spec.Assertions[a].ID]; ok {
				if approved {
					before, _ := json.Marshal(spec.Assertions[a].Expected, json.Deterministic(true))
					after, _ := json.Marshal(expected, json.Deterministic(true))
					if string(before) != string(after) {
						return Expansion{}, queue, nil, errors.New("suite row changes a released expectation; release a reviewed template for that row")
					}
				}
				spec.Assertions[a].Expected = expected
				used++
			}
		}
		if used != len(declaration.Expected) {
			return Expansion{}, queue, nil, errors.New("suite row names an assertion its template does not declare")
		}
		encoded, err := json.Marshal(spec, json.Deterministic(true))
		if err != nil {
			return Expansion{}, queue, nil, errors.New("cannot encode suite test")
		}
		if _, err = testrunner.DecodeSpec(encoded); err != nil {
			return Expansion{}, queue, nil, err
		}
		// The compiled bytes are prepared exactly as an execution would prepare
		// the retained file: a case or target that cannot verify, or a send
		// order the evidence does not support, refuses the whole expansion.
		plan, err := testrunner.PrepareSpec(encoded, root)
		if err != nil {
			return Expansion{}, queue, nil, err
		}
		mappings := plan.PinnedInputs().Mappings
		if len(mappings) != len(declaration.Sequence) {
			return Expansion{}, queue, nil, errors.New("suite sequence does not match selected evidence")
		}
		for m, mapping := range mappings {
			if mapping.SourceOccurrence != declaration.Sequence[m] {
				return Expansion{}, queue, nil, errors.New("suite sequence differs from evidence send order")
			}
		}
		if queue.Jobs[i].ID != declaration.Job || queue.Jobs[i].Isolation != declaration.Isolation || !slices.Equal(queue.Jobs[i].After, declaration.After) {
			return Expansion{}, queue, nil, errors.New("suite expansion is inconsistent with its queue")
		}
		if queue.Jobs[i].Isolation == "shared" {
			expansion.Sharing = previewShare
		}
		after := declaration.After
		if after == nil {
			after = []string{}
		}
		job := ExpandedJob{
			ID: declaration.Job, Test: declaration.Test, Row: declaration.Row, Spec: declaration.Spec,
			Case: spec.Input.Case, Target: spec.Target, Boundary: spec.Observation.Boundary,
			Observation: spec.Observation.Path, Isolation: string(declaration.Isolation),
			Sequence: declaration.Sequence, After: after,
		}
		if approved {
			job.Release = release.Identity()
		}
		expansion.Jobs = append(expansion.Jobs, job)
		compiled = append(compiled, compiledJob{Name: specName(job.ID), Raw: encoded})
	}
	return expansion, queue, compiled, nil
}

// Preview expands a suite against one environment exactly as preparation
// would, reading the templates and the selected evidence to check sequences,
// bindings and expectation overrides, and writes nothing. root is the folder
// the suite's relative references resolve from; references names a
// readmit-suite-releases/v1 sidecar whose pins are verified, or is empty for
// an ordinary suite.
func Preview(root string, doc Document, environment string, references string) (Expansion, error) {
	var selected *Environment
	for i := range doc.Environments {
		if doc.Environments[i].ID == environment {
			selected = &doc.Environments[i]
		}
	}
	if selected == nil {
		return Expansion{}, errors.New("suite does not declare the selected environment")
	}
	var releases map[string]expectation.Release
	if references != "" {
		loaded, _, err := loadReleases(references, doc)
		if err != nil {
			return Expansion{}, err
		}
		releases = loaded
	}
	expansion, _, _, err := expand(root, doc, *selected, releases)
	return expansion, err
}
