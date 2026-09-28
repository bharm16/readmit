package desktop

import (
	"strings"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/collection"
	"github.com/bharm16/readmit/internal/diagnose"
	"github.com/bharm16/readmit/internal/fixturereset"
	"github.com/bharm16/readmit/internal/grid"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/importer"
	"github.com/bharm16/readmit/internal/localprofile"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/profilepack"
	"github.com/bharm16/readmit/internal/profileversion"
	"github.com/bharm16/readmit/internal/scenario"
	"github.com/bharm16/readmit/internal/scenariogen"
	"github.com/bharm16/readmit/internal/sequenceanalysis"
	"github.com/bharm16/readmit/internal/testauthor"
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
	// ACKPositions are the acknowledgement positions an ACK field check
	// addresses, in the order a picker offers them.
	ACKPositions []string `json:"ack_positions"`
	// Checks, Profiles and Scenarios are every value the library's editors
	// offer, as the readers of their documents accept them.
	Checks    CheckVocabulary    `json:"checks"`
	Profiles  ProfileVocabulary  `json:"profiles"`
	Scenarios ScenarioVocabulary `json:"scenarios"`
	// FixtureModes are the modes the built-in SIU fixture runs in.
	FixtureModes []observation.Mode `json:"fixture_modes"`
	// AffectedTestImpacts are what Affected tests says about one pinned test.
	AffectedTestImpacts []string `json:"affected_test_impacts"`
	// ObservationStarts are where a new observation source of each kind
	// starts when the editor switches to that kind.
	ObservationStarts []observesource.Source `json:"observation_starts"`
	// Coverage is every value the coverage editor offers.
	Coverage CoverageVocabulary `json:"coverage"`
}

// CoverageVocabulary is every value a coverage declaration's editor offers,
// as the sequence analysis reader accepts them: the coverage a source window
// is declared with, the bases a retry is declared on, and the most seconds of
// clock tolerance.
type CoverageVocabulary struct {
	DeclaredCoverages        []sequenceanalysis.DeclaredCoverage `json:"declared_coverages"`
	RetryBases               []sequenceanalysis.RetryBasis       `json:"retry_bases"`
	MaxClockToleranceSeconds int                                 `json:"max_clock_tolerance_seconds"`
}

// CheckVocabulary is every value a check's editor offers: the sixteen
// operators, the message and observation scopes, the quantifiers a per-record
// check takes and the states a field is in.
type CheckVocabulary struct {
	Operators     []assertion.Operator     `json:"operators"`
	MessageScopes []assertion.MessageScope `json:"message_scopes"`
	RecordScopes  []assertion.RecordScope  `json:"record_scopes"`
	Quantifiers   []assertion.Quantifier   `json:"quantifiers"`
	FieldStates   []hl7.State              `json:"field_states"`
}

// ProfileVocabulary is every value a local profile's editor offers.
type ProfileVocabulary struct {
	HL7Versions        []string                         `json:"hl7_versions"`
	Families           []string                         `json:"families"`
	Usages             []localprofile.Usage             `json:"usages"`
	DataTypes          []string                         `json:"data_types"`
	ConditionOperators []localprofile.ConditionOperator `json:"condition_operators"`
	Bindings           []localprofile.Binding           `json:"bindings"`
	UniversalIDTypes   []string                         `json:"universal_id_types"`
	Precisions         []localprofile.Precision         `json:"precisions"`
	TimeZoneRules      []localprofile.TimeZoneRule      `json:"timezone_rules"`
	Unbounded          string                           `json:"unbounded"`
	Origins            []localprofile.Origin            `json:"origins"`
	SupportOutcomes    []profilepack.Outcome            `json:"support_outcomes"`
}

// ScenarioVocabulary is every lifecycle family and event the generator
// implements, the named workflows a new scenario starts from, and the values
// a plan's rows and variants take.
type ScenarioVocabulary struct {
	Catalog          scenario.Catalog       `json:"catalog"`
	Templates        []ScenarioTemplate     `json:"templates"`
	Expectations     []scenario.Expectation `json:"expectations"`
	Encodings        []string               `json:"encodings"`
	GeneratorVersion string                 `json:"generator_version"`
	MaxSeed          uint64                 `json:"max_seed"`
}

// ReceiverFaultVocabulary is every fault action a step can declare, whether
// each waits before it acts, and the delay the window starts a waiting fault
// with.
type ReceiverFaultVocabulary struct {
	Actions        []collection.FaultAction `json:"actions"`
	DefaultDelayMS int                      `json:"default_delay_ms"`
}

// DiagnosisBuiltin is one built-in diagnosis configuration: the selection
// that names it, the name a person reads for it, and the profile and ruleset
// it runs.
type DiagnosisBuiltin struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
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
		builtins = append(builtins, DiagnosisBuiltin{ID: builtin.id, Name: profileName(config.Profile), Profile: config.Profile, Ruleset: config.Ruleset})
	}
	return Vocabulary{
		DiagnosisBuiltins: builtins,
		ImportPlan:        importer.Vocabulary(),
		ResetOperators:    fixturereset.Reviewed(),
		ObservationStarts: operation.ObservationStarts(),
		ReceiverFaults:    ReceiverFaultVocabulary{Actions: collection.FaultActions(), DefaultDelayMS: defaultFaultDelayMS},
		Bounds: WindowBounds{
			Grid: grid.MaxRows, Comparison: MaxComparisonRows, Review: MaxReviewFindings,
			Sequence: MaxSequenceEvents, Diagnosis: MaxDiagnosisFindings,
		},
		ACKPositions: testauthor.ACKPositions(),
		Checks: CheckVocabulary{
			Operators:     assertion.Operators(),
			MessageScopes: []assertion.MessageScope{assertion.InputMessages, assertion.ObservedMessages},
			RecordScopes:  []assertion.RecordScope{assertion.BeforeRecords, assertion.AfterRecords},
			Quantifiers:   []assertion.Quantifier{assertion.QuantifierEvery, assertion.QuantifierAny, assertion.QuantifierNone},
			FieldStates:   []hl7.State{hl7.Present, hl7.Empty, hl7.Null, hl7.Omitted},
		},
		Profiles: ProfileVocabulary{
			HL7Versions: profilepack.HL7Versions(), Families: profilepack.Families(), Usages: localprofile.Usages(),
			DataTypes: localprofile.DataTypes(), ConditionOperators: localprofile.ConditionOperators(), Bindings: localprofile.Bindings(),
			UniversalIDTypes: localprofile.UniversalIDTypes(), Precisions: localprofile.Precisions(), TimeZoneRules: localprofile.TimeZoneRules(),
			Unbounded: localprofile.Unbounded,
			Origins:   []localprofile.Origin{localprofile.OriginProfile, localprofile.OriginOverridden, localprofile.OriginLocal, localprofile.OriginUndeclared},
			SupportOutcomes: []profilepack.Outcome{profilepack.OutcomeSupported, profilepack.OutcomeUntested, profilepack.OutcomeUnsupported,
				profilepack.OutcomeUnknown},
		},
		Scenarios: ScenarioVocabulary{
			Catalog: scenario.SupportedCatalog(), Templates: scenarioTemplates(),
			Expectations: []scenario.Expectation{scenario.Accepted, scenario.Refused}, Encodings: []string{"utf-8", "iso-8859-1"},
			GeneratorVersion: scenariogen.Version, MaxSeed: maxWindowSeed,
		},
		FixtureModes: []observation.Mode{observation.Fixed, observation.Defective},
		Coverage: CoverageVocabulary{DeclaredCoverages: sequenceanalysis.DeclaredCoverages(), RetryBases: sequenceanalysis.RetryBases(),
			MaxClockToleranceSeconds: sequenceanalysis.MaxClockToleranceSeconds},
		AffectedTestImpacts: []string{string(profileversion.ImpactAffected), string(profileversion.ImpactUnaffected),
			string(profileversion.ImpactCurrent), string(profileversion.ImpactUnrelated), ImpactUnknown},
	}
}
