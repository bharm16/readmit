package connectedrun

import "strings"

// A retained lifecycle's on-disk layout, in one place. The report package
// names tampered evidence and inventories disclosure surfaces from this one
// classification, and navigates phases, steps and validations through the
// directories below, so a new retained file kind lands here once instead of
// in every consumer across packages.

// Area is which part of a retained lifecycle one file belongs to.
type Area string

const (
	// AreaSeal is identity.sha256: the lifecycle seal.
	AreaSeal Area = "seal"
	// AreaRecord is manifest.json, started.json, the phase verdict files and intents/.
	AreaRecord Area = "record"
	// AreaPlan is plan/ except dependencies/.
	AreaPlan Area = "plan"
	// AreaPlanDeps is plan/dependencies/.
	AreaPlanDeps Area = "plan-dependencies"
	// AreaSetup is preflight/, isolation/, transitions/, continuation/ and previous/.
	AreaSetup Area = "setup"
	// AreaPhase is phases/<id>/..., refined by Sub.
	AreaPhase Area = "phase"
	// AreaOther is anything else.
	AreaOther Area = "other"
)

// PhaseArea refines AreaPhase by the phase's subdirectory.
type PhaseArea string

const (
	PhasePlan               PhaseArea = "plan"
	PhaseSteps              PhaseArea = "steps"
	PhaseTransport          PhaseArea = "transport"
	PhaseObservations       PhaseArea = "observations"
	PhaseIntervals          PhaseArea = "intervals"
	PhaseValidations        PhaseArea = "validations"
	PhasePreflight          PhaseArea = "preflight"
	PhaseEvaluation         PhaseArea = "evaluation"
	PhaseEvaluationDatasets PhaseArea = "evaluation-datasets"
	PhaseManifest           PhaseArea = "manifest"
	PhaseOther              PhaseArea = "other"
)

// Location is where one file of a retained lifecycle sits. The qualifiers
// mark the exchange, sample, dataset and body shapes that cut across
// subdirectories; each consumer combines them with Sub for its own answer.
type Location struct {
	Area Area
	// Phase is the phase id when Area is AreaPhase.
	Phase string
	// Sub refines the phase directory when Area is AreaPhase.
	Sub PhaseArea
	// HTTP marks a retained HTTP exchange below the phase directory.
	HTTP bool
	// Samples marks retained samples below the phase directory.
	Samples bool
	// Dataset marks typed rows below the phase directory.
	Dataset bool
	// Binary marks a .bin body.
	Binary bool
}

// Locate maps a lifecycle-relative path to its retained location.
func Locate(rel string) Location {
	switch {
	case rel == "identity.sha256":
		return Location{Area: AreaSeal}
	case rel == "manifest.json" || rel == "started.json" || strings.HasPrefix(rel, "phase-") || strings.HasPrefix(rel, "intents/"):
		return Location{Area: AreaRecord}
	case strings.HasPrefix(rel, "plan/dependencies/"):
		return Location{Area: AreaPlanDeps}
	case strings.HasPrefix(rel, "plan/"):
		return Location{Area: AreaPlan}
	case strings.HasPrefix(rel, "preflight/") || strings.HasPrefix(rel, "isolation/") || strings.HasPrefix(rel, "transitions/") || strings.HasPrefix(rel, "continuation/") || strings.HasPrefix(rel, "previous/"):
		return Location{Area: AreaSetup}
	case strings.HasPrefix(rel, "phases/"):
		phase, rest, _ := strings.Cut(strings.TrimPrefix(rel, "phases/"), "/")
		loc := Location{Area: AreaPhase, Phase: phase, Sub: PhaseOther,
			HTTP: strings.Contains(rest, "/http/"), Samples: strings.Contains(rest, "/samples/"),
			Dataset: strings.Contains(rest, "/dataset"), Binary: strings.HasSuffix(rest, ".bin")}
		switch {
		case rest == "manifest.json" || rest == "started.json":
			loc.Sub = PhaseManifest
		case strings.HasPrefix(rest, "plan/"):
			loc.Sub = PhasePlan
		case strings.HasPrefix(rest, "validations/"):
			loc.Sub = PhaseValidations
		case strings.HasPrefix(rest, "transport/"):
			loc.Sub = PhaseTransport
		case strings.HasPrefix(rest, "observations/"):
			loc.Sub = PhaseObservations
		case strings.HasPrefix(rest, "intervals/"):
			loc.Sub = PhaseIntervals
		case strings.HasPrefix(rest, "preflight/"):
			loc.Sub = PhasePreflight
		case strings.HasPrefix(rest, "steps/"):
			loc.Sub = PhaseSteps
		case strings.HasPrefix(rest, "evaluation/datasets/"):
			loc.Sub = PhaseEvaluationDatasets
		case strings.HasPrefix(rest, "evaluation"):
			loc.Sub = PhaseEvaluation
		}
		return loc
	default:
		return Location{Area: AreaOther}
	}
}

// PhaseDir is the lifecycle-relative directory retaining one phase's evidence.
func PhaseDir(phase string) string { return "phases/" + phase }

// StepDir is the lifecycle-relative directory retaining one step's FHIR evidence.
func StepDir(phase, step string) string { return PhaseDir(phase) + "/steps/" + step }

// ValidationDir is the lifecycle-relative directory retaining one validation's evidence.
func ValidationDir(phase, check string) string { return PhaseDir(phase) + "/validations/" + check }
