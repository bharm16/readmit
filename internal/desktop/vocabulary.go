package desktop

import (
	"strings"

	"github.com/bharm16/readmit/internal/collection"
	"github.com/bharm16/readmit/internal/diagnose"
	"github.com/bharm16/readmit/internal/fixturereset"
	"github.com/bharm16/readmit/internal/grid"
	"github.com/bharm16/readmit/internal/importer"
)

// Vocabulary is what the window offers a person to choose among, and the
// bounds it pages by, as the Go side that accepts them declares them. The
// window renders these rather than keeping a copy, so a choice it offers is
// always one the facade reads and a page it asks for is always one the facade
// answers.
type Vocabulary struct {
	// DiagnosisBuiltins are the built-in configurations a diagnosis can run
	// under, each named by the selection a diagnosis request makes.
	DiagnosisBuiltins []DiagnosisBuiltin `json:"diagnosis_builtins"`
	// ImportPlan is every value a readmit-import-plan/v1 member can declare.
	ImportPlan importer.PlanVocabulary `json:"import_plan"`
	// ResetOperators are the reviewed reset operators, each with the one
	// authority a plan records beside it.
	ResetOperators []fixturereset.Review `json:"reset_operators"`
	// ReceiverFaults are the controlled faults a responder policy's fault
	// step can declare, and the delay a waiting one starts with.
	ReceiverFaults ReceiverFaultVocabulary `json:"receiver_faults"`
	// Bounds are how many rows one window of each paged view asks for: the
	// most the facade answers in one window.
	Bounds WindowBounds `json:"bounds"`
}

// ReceiverFaultVocabulary is every fault action a step can declare, whether
// each waits before it acts, and the delay the window starts a waiting fault
// with.
type ReceiverFaultVocabulary struct {
	Actions        []collection.FaultAction `json:"actions"`
	DefaultDelayMS int                      `json:"default_delay_ms"`
}

// DiagnosisBuiltin is one built-in diagnosis configuration: the selection
// that names it and the profile and ruleset it runs.
type DiagnosisBuiltin struct {
	ID      string `json:"id"`
	Profile string `json:"profile"`
	Ruleset string `json:"ruleset"`
}

// WindowBounds are the facade's own bound on one window of each paged view.
type WindowBounds struct {
	Grid       int `json:"grid"`
	Comparison int `json:"comparison"`
	Review     int `json:"review"`
	Sequence   int `json:"sequence"`
	Diagnosis  int `json:"diagnosis"`
}

// diagnosisBuiltins is every built-in selection a diagnosis request may name,
// in the order the window offers them.
var diagnosisBuiltins = []struct {
	id     string
	config func() diagnose.Config
}{
	{"siu", diagnose.DefaultConfig},
	{"lifecycle", diagnose.LifecycleConfig},
	{"order", diagnose.OrderConfig},
}

// builtinConfig is the configuration one built-in selection runs under.
func builtinConfig(id string) (diagnose.Config, bool) {
	for _, builtin := range diagnosisBuiltins {
		if builtin.id == id {
			return builtin.config(), true
		}
	}
	return diagnose.Config{}, false
}

// builtinNames names every built-in selection, as a refusal lists them.
func builtinNames() string {
	names := make([]string, 0, len(diagnosisBuiltins))
	for _, builtin := range diagnosisBuiltins {
		names = append(names, builtin.id)
	}
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " or " + names[len(names)-1]
}

func vocabulary() Vocabulary {
	builtins := make([]DiagnosisBuiltin, 0, len(diagnosisBuiltins))
	for _, builtin := range diagnosisBuiltins {
		config := builtin.config()
		builtins = append(builtins, DiagnosisBuiltin{ID: builtin.id, Profile: config.Profile, Ruleset: config.Ruleset})
	}
	return Vocabulary{
		DiagnosisBuiltins: builtins,
		ImportPlan:        importer.Vocabulary(),
		ResetOperators:    fixturereset.Reviewed(),
		ReceiverFaults:    ReceiverFaultVocabulary{Actions: collection.FaultActions(), DefaultDelayMS: defaultFaultDelayMS},
		Bounds: WindowBounds{
			Grid: grid.MaxRows, Comparison: MaxComparisonRows, Review: MaxReviewFindings,
			Sequence: MaxSequenceEvents, Diagnosis: MaxDiagnosisFindings,
		},
	}
}
