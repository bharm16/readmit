package main

import (
	"path"
	"reflect"
	"strings"
)

// names decides the TypeScript name of each Go type the facade reaches. A
// type of internal/desktop keeps its Go name; a type of another package takes
// that package's prefix, so two packages' Result types stay apart. The two
// tables hold only the exceptions: the prefix of a package whose types the
// window names differently, and the name of a single type whose TypeScript
// name the panels import under another spelling. A new type needs an entry
// only when the generator refuses a name two Go types would share.
type names struct {
	prefixes map[string]string // package import path, relative to the module, to its prefix
	types    map[string]string // Go type, as goName writes it, to its TypeScript name
	// vocabularies name the choices a panel offers for members Go carries as
	// plain strings, each by the Go constants that name them: the const block
	// that declares one constant, or exactly the constants listed.
	vocabularies map[string][]string
}

func (n names) of(t reflect.Type) string {
	if name, ok := n.types[goName(t)]; ok {
		return name
	}
	pkg := relative(t.PkgPath())
	if pkg == "internal/desktop" {
		return capitalized(t.Name())
	}
	prefix, ok := n.prefixes[pkg]
	if !ok {
		prefix = capitalized(path.Base(pkg))
	}
	return prefix + capitalized(t.Name())
}

func capitalized(name string) string {
	return strings.ToUpper(name[:1]) + name[1:]
}

// facadeNames is the naming the committed declarations use.
var facadeNames = names{
	prefixes: map[string]string{
		"desktop/hubadmin":          "HubAdmin",
		"internal/assertionauthor":  "AssertionAuthor",
		"internal/capturejournal":   "CaptureJournal",
		"internal/correlate":        "Correlation",
		"internal/customerrunner":   "CustomerRunner",
		"internal/diagnose":         "Diagnosis",
		"internal/diff":             "Normalization",
		"internal/durablerun":       "DurableRun",
		"internal/engineexport":     "EngineExport",
		"internal/evidencesource":   "EvidenceSource",
		"internal/exportreview":     "ExportReview",
		"internal/findingreview":    "Finding",
		"internal/fixturereset":     "Reset",
		"internal/hl7":              "HL7",
		"internal/importer":         "Import",
		"internal/localprofile":     "LocalProfile",
		"internal/observesource":    "ObservationSource",
		"internal/observewindow":    "ObservationWindow",
		"internal/operationguard":   "OperationGuard",
		"internal/profilepack":      "ProfilePack",
		"internal/profilepackage":   "ProfilePackage",
		"internal/profileversion":   "ProfileVersion",
		"internal/reduce":           "Reduction",
		"internal/runcompare":       "RunCompare",
		"internal/runqueue":         "RunQueue",
		"internal/sendpolicy":       "SendPolicy",
		"internal/sequenceanalysis": "SequenceAnalysis",
		"internal/testauthor":       "Test",
		"internal/testrunner":       "TestRunner",
	},
	types: map[string]string{
		"internal/assertionauthor.Clause":              "AssertionClause",
		"internal/assertionauthor.Draft":               "AssertionSetDraftDocument",
		"internal/bundle.EventKind":                    "OccurrenceKind",
		"internal/collection.Policy":                   "ReceiverPolicy",
		"internal/correlate.Reference":                 "CorrelationOccurrence",
		"internal/correlate.ReviewedView":              "CorrelationReviewView",
		"internal/correlate.Rules":                     "CorrelationRulesDocument",
		"internal/desktop.Case":                        "CaseEvidence",
		"internal/desktop.CommandID":                   "CommandId",
		"internal/desktop.Decision":                    "ReviewDecision",
		"internal/desktop.RegionID":                    "RegionId",
		"internal/desktop.Row":                         "GridRow",
		"internal/diagnose.Config":                     "DiagnoseConfig",
		"internal/diagnose.Namespace":                  "DiagnoseConfigNamespace",
		"internal/diff.NormalizationSummary":           "NormalizationSummary",
		"internal/diff.Summary":                        "ComparisonSummary",
		"internal/drift.Drift":                         "Drift",
		"internal/durablerun.State":                    "RunState",
		"internal/engineexport.Plan":                   "EnginePlan",
		"internal/evidencesource.Source":               "EvidenceSource",
		"internal/findingreview.Record":                "FindingReviewRecord",
		"internal/grid.Filter":                         "Filter",
		"internal/hl7.State":                           "FieldState",
		"internal/importer.Recipe":                     "MappingRecipe",
		"internal/index.Match":                         "FieldMatch",
		"internal/localprofile.Condition":              "Condition",
		"internal/localprofile.Field":                  "Field",
		"internal/localprofile.Profile":                "LocalProfile",
		"internal/localprofile.Resolution":             "LocalProfileResolution",
		"internal/localprofile.Segment":                "Segment",
		"internal/observesource.Source":                "ObservationSource",
		"internal/observewindow.Window":                "ObservationWindow",
		"internal/operation.CaptureObservationBinding": "CaptureObservationBinding",
		"internal/operation.InspectionRow":             "InspectionRow",
		"internal/operation.ObservationAbsenceSummary": "ObservationAbsenceSummary",
		"internal/operation.ObservationAdapterSupport": "ObservationAdapterSupport",
		"internal/profileversion.AssessedTest":         "AssessedTest",
		"internal/profileversion.Version":              "ProfileVersion",
		"internal/project.Status":                      "CaseStatus",
		"internal/redact.LiteralBinding":               "RedactSpecBinding",
		"internal/replay.Classification":               "TargetClassification",
		"internal/replay.Target":                       "Target",
		"internal/runcompare.Execution":                "ExecutionView",
		"internal/sendpolicy.Policy":                   "SendPolicy",
		"internal/suite.Exclusion":                     "SuiteExclusionDeclaration",
		"internal/suite.GateReport":                    "CIGateReport",
		"internal/testauthor.Draft":                    "TestDraftDocument",
	},
	vocabularies: map[string][]string{
		"CorpusPathKind":          {"internal/desktop.corpusFolderPath"},
		"ExplanationInputKind":    {"internal/desktop.runInput"},
		"GuideStepId":             {"internal/guide.StepSample"},
		"GuideTrialId":            {"internal/guide.StepBaseline", "internal/guide.StepPostFix"},
		"InspectFormat":           {"internal/operation.autoFormat"},
		"InspectTerminator":       {"internal/operation.autoTerminator"},
		"InspectionPathKind":      {"internal/desktop.inspectionFile"},
		"SyntheticPacketPathKind": {"internal/desktop.packetDestination"},
		"TestBoundary":            {"internal/testrunner.LedgerBoundary", "internal/testrunner.ACKBoundary"},
		"TestExpectationOperator": {"internal/testauthor.LedgerCount"},
		"TestStage":               {"internal/testauthor.StageName"},
		"TransformOperator":       {"internal/transform.DropOccurrence"},
	},
}
