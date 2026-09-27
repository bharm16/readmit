package connectedtest

import (
	"fmt"
	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/testrunner"
)

// ConvertLegacy explicitly creates a new logical revision with exact ancestry.
// Checks are independently supplied; no legacy approval is copied or invented.
func ConvertLegacy(specPath, project, id, revision string, checks []byte, g Generation) (*Plan, error) {
	legacy, err := testrunner.Prepare(specPath)
	if err != nil {
		return nil, err
	}
	pinned := legacy.PinnedInputs()
	spec, err := testrunner.DecodeSpec(pinned.Spec)
	if err != nil {
		return nil, err
	}
	env := legacy.Environment()
	files := map[string][]byte{"legacy-spec.json": pinned.Spec, "checks.json": checks}
	ref := func(id, schema, file string) Reference {
		return Reference{Project: project, ID: id, Schema: schema, File: file, SHA256: Digest(files[file])}
	}
	ancestor := ref("legacy", testrunner.SpecSchema, "legacy-spec.json")
	d := Test{Schema: TestSchema, Project: project, ID: id, Revision: revision, Ancestry: &ancestor, Environment: Environment{Project: project, ID: "legacy-target", Revision: pinned.Target.Identity(), Name: env.Name, Classification: string(env.Classification), Endpoint: "legacy-target", TargetIdentity: pinned.Target.Identity(), AddressPolicyIdentity: Digest([]byte("readmit-legacy-loopback-policy/v1")), TLS: TLS{Mode: "plain"}, TargetRevision: TargetRevision{Provenance: "unknown"}}, Setup: Setup{Kind: "operator-declared", Isolation: "legacy-operator-declared", Instructions: spec.Setup.ResetInstructions}, Checks: ref("checks", assertion.Schema, "checks.json"), OperatorVersion: OperatorVersion, Limits: Limits{MaxSteps: 256, MaxBytes: MaxBytes, DeadlineMS: 300000}, Datasets: []Dataset{{ID: "acks", Kind: "v2-messages", Phase: "after", Source: "legacy-ack", Completion: Completion{Kind: "ack-responses", HorizonMS: 300000, MaxRecords: 256, MaxBytes: MaxBytes}}}, Bindings: Bindings{Observed: "acks"}}
	for i, m := range pinned.Mappings {
		b, err := legacy.Outbound(m.OutboundOccurrence)
		if err != nil {
			return nil, err
		}
		name := fmt.Sprintf("input-%d.hl7", i+1)
		files[name] = b
		step := fmt.Sprintf("step-%d", i+1)
		d.Steps = append(d.Steps, Step{ID: step, Endpoint: "legacy-target", V2: &V2Stimulus{Input: ref(step, "hl7", name), Occurrence: m.SourceOccurrence}})
	}
	raw, err := encode(d)
	if err != nil {
		return nil, err
	}
	p, err := Compile(raw, files, g)
	if err != nil {
		return nil, err
	}
	if err := matchLegacy(p, legacy); err != nil {
		return nil, err
	}
	return p, nil
}
