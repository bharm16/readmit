package drift

import (
	"errors"
	"os"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/runresult"
)

// side is one half of a comparison: the report's own view of it, plus the
// retained records the comparison reads part by part. Those records stay here
// and never reach the report — a target address is read to say whether it
// changed and is not what is said.
type side struct {
	report          Side
	target          *replay.TargetRecord
	transformations []replay.Transformation
}

// openArtifact opens one artifact directory through runresult's evidence
// opener, which reads each artifact through that artifact's own verified
// reader. It opens no network connection and writes nothing.
func openArtifact(path string) (*runresult.Evidence, error) {
	directory, err := artifactpath.Resolve(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(directory)
	if err != nil {
		return nil, errors.New("cannot inspect drift input")
	}
	if !info.IsDir() {
		return nil, errors.New("a drift comparison reads a case, run, result or durable run directory; a message file retains no input identity, target, engine or rule of its own")
	}
	return runresult.OpenEvidence(path, "drift")
}

// kinds are the report's names for the evidence families a side may be.
var kinds = map[runresult.Family]string{
	runresult.CaseFamily:   CaseKind,
	runresult.RunFamily:    RunKind,
	runresult.ResultFamily: ResultKind,
	runresult.JobFamily:    JobKind,
}

// fromEvidence states what one opened artifact retains about each of the four
// causes. Every cause starts undeclared, so evidence that says nothing about a
// cause is reported as saying nothing rather than as agreeing. A case is
// evidence, not an execution: it declares the input it is and nothing about a
// target, an engine or a rule. A pin this build does not read is stated by
// its digest alone, which is still a fingerprint to compare.
func fromEvidence(opened *runresult.Evidence) *side {
	s := &side{report: Side{
		Kind:        kinds[opened.Family],
		Identity:    opened.Identity,
		Input:       InputSide{State: Undeclared, Transformations: []string{}},
		Target:      TargetSide{State: Undeclared, Revision: UnknownRevision},
		Environment: EnvironmentSide{State: Undeclared},
		Rule:        RuleSide{State: Undeclared},
	}}
	if input := opened.Input; input != nil {
		s.transformations = input.Transformations
		names := make([]string, 0, len(input.Transformations))
		for _, transformation := range input.Transformations {
			names = append(names, transformation.Name)
		}
		s.report.Input = InputSide{State: Declared, Identity: input.Identity, Transformations: names, RecordedChanges: input.Changes}
	}
	// The target fingerprint is the identity readmit-result/v1 records as
	// `target_identity`, so one target retained by a run and the same target
	// retained by a result fingerprint identically and compare across kinds.
	if target := opened.Target; target != nil {
		s.target = &target.Record
		s.report.Target = TargetSide{State: Declared, Fingerprint: target.Identity, Revision: UnknownRevision}
	}
	if pin := opened.Pin; pin != nil {
		if pin.Document == nil {
			s.report.Environment = EnvironmentSide{State: Unreadable, Fingerprint: pin.Digest}
			s.report.Rule = RuleSide{State: Unreadable, Fingerprint: pin.Digest}
		} else {
			s.report.Environment = EnvironmentSide{State: Declared, Fingerprint: pin.Digest, Engine: pin.Document.Engine, Spec: pin.Document.Spec}
			s.report.Rule = RuleSide{State: Declared, Fingerprint: pin.Digest, Profile: pin.Document.Profile, Resolution: resolution(pin.Document.Profile)}
		}
	}
	return s
}

// resolution reports whether this build holds the content a profile identity
// names. Only the profile this build implements resolves: no pack is extracted
// and no library is bundled in this release, so every other identity stands
// for content nothing here can read, however familiar the name looks.
func resolution(profile string) string {
	if profile == observation.Profile {
		return BundledProfile
	}
	return UnresolvedProfile
}
