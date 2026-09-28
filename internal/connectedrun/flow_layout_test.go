package connectedrun_test

import (
	"reflect"
	"testing"

	"github.com/bharm16/readmit/internal/connectedrun"
)

// The retained layout is classified once, beside its writers: every part of
// a lifecycle maps to its area, a phase refines to its subdirectory, and the
// exchange, sample, dataset and body shapes mark themselves for the consumers
// that combine them. The report package names tamper evidence and inventories
// disclosure surfaces from this answer instead of re-deriving it.
func TestLocateMapsEveryPartOfTheRetainedLayout(t *testing.T) {
	phase := func(sub connectedrun.PhaseArea, qualifiers connectedrun.Location) connectedrun.Location {
		qualifiers.Area, qualifiers.Phase, qualifiers.Sub = connectedrun.AreaPhase, "book", sub
		return qualifiers
	}
	for rel, want := range map[string]connectedrun.Location{
		"identity.sha256":           {Area: connectedrun.AreaSeal},
		"manifest.json":             {Area: connectedrun.AreaRecord},
		"started.json":              {Area: connectedrun.AreaRecord},
		"phase-book.json":           {Area: connectedrun.AreaRecord},
		"intents/book.json":         {Area: connectedrun.AreaRecord},
		"plan/test.json":            {Area: connectedrun.AreaPlan},
		"plan/dependencies/abc":     {Area: connectedrun.AreaPlanDeps},
		"preflight/x.json":          {Area: connectedrun.AreaSetup},
		"isolation/result.json":     {Area: connectedrun.AreaSetup},
		"transitions/x.json":        {Area: connectedrun.AreaSetup},
		"continuation/x.json":       {Area: connectedrun.AreaSetup},
		"previous/x.json":           {Area: connectedrun.AreaSetup},
		"SUMMARY.md":                {Area: connectedrun.AreaOther},
		"intents":                   {Area: connectedrun.AreaOther},
		"plan":                      {Area: connectedrun.AreaOther},
		"phases":                    {Area: connectedrun.AreaOther},
		"phases/book":               phase(connectedrun.PhaseOther, connectedrun.Location{}),
		"phases/book/plan/x":        phase(connectedrun.PhasePlan, connectedrun.Location{}),
		"phases/book/manifest.json": phase(connectedrun.PhaseManifest, connectedrun.Location{}),
		"phases/book/started.json":  phase(connectedrun.PhaseManifest, connectedrun.Location{}),
		"phases/book/steps/create/response-0000.bin": phase(connectedrun.PhaseSteps, connectedrun.Location{Binary: true}),
		"phases/book/steps/create/http/request.json": phase(connectedrun.PhaseSteps, connectedrun.Location{HTTP: true}),
		"phases/book/transport/sent.mllp":            phase(connectedrun.PhaseTransport, connectedrun.Location{}),
		"phases/book/observations/o/dataset.json":    phase(connectedrun.PhaseObservations, connectedrun.Location{Dataset: true}),
		"phases/book/intervals/a/manifest.json":      phase(connectedrun.PhaseIntervals, connectedrun.Location{}),
		"phases/book/intervals/a/samples/0.json":     phase(connectedrun.PhaseIntervals, connectedrun.Location{Samples: true}),
		"phases/book/intervals/a/dataset/rows.json":  phase(connectedrun.PhaseIntervals, connectedrun.Location{Dataset: true}),
		"phases/book/validations/v/result.json":      phase(connectedrun.PhaseValidations, connectedrun.Location{}),
		"phases/book/preflight/cap.json":             phase(connectedrun.PhasePreflight, connectedrun.Location{}),
		"phases/book/evaluation.json":                phase(connectedrun.PhaseEvaluation, connectedrun.Location{}),
		"phases/book/evaluation/datasets/d.json":     phase(connectedrun.PhaseEvaluationDatasets, connectedrun.Location{Dataset: true}),
		"phases/book/notes.txt":                      phase(connectedrun.PhaseOther, connectedrun.Location{}),
	} {
		if got := connectedrun.Locate(rel); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %+v, want %+v", rel, got, want)
		}
	}
	if got := connectedrun.Locate("phases/other/x.json"); got.Phase != "other" {
		t.Errorf("the phase id is not carried: %+v", got)
	}
	if got := connectedrun.PhaseDir("book"); got != "phases/book" {
		t.Errorf("phase dir: %q", got)
	}
	if got := connectedrun.StepDir("book", "create"); got != "phases/book/steps/create" {
		t.Errorf("step dir: %q", got)
	}
	if got := connectedrun.ValidationDir("book", "v"); got != "phases/book/validations/v" {
		t.Errorf("validation dir: %q", got)
	}
}
