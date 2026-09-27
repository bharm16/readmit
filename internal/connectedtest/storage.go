package connectedtest

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"strings"

	"github.com/bharm16/readmit/internal/artifactdir"
)

var planFamily = artifactdir.Family{Layout: artifactdir.Layout{Noun: "connected plan", AllowedDirectories: []string{"dependencies", "inputs"}, RequiredFiles: []string{"plan.json", "test.json", "identity.sha256"}, AllowFile: func(string) bool { return true }, MaxFiles: 2048, MaxFileBytes: MaxBytes, MaxBytes: 4 * MaxBytes}, Seal: artifactdir.DirectoryHash(PlanSchema)}
var resultFamily = artifactdir.Family{Layout: artifactdir.Layout{Noun: "connected result", AllowedDirectories: []string{"plan", "plan/dependencies", "plan/inputs", "observations"}, Nested: []string{"legacy"}, RequiredFiles: []string{"result.json", "identity.sha256"}, AllowFile: func(string) bool { return true }, MaxFiles: 20000, MaxFileBytes: MaxBytes, MaxBytes: 256 << 20}, Seal: artifactdir.DirectoryHash(ResultSchema)}

func (p *Plan) Write(ctx context.Context, output string) error {
	_, err := artifactdir.Write(ctx, output, planFamilyFor(p.document.Schema), artifactdir.Durable, p.Files())
	return err
}
func OpenPlan(directory string) (*Plan, error) {
	files, err := artifactdir.Read(directory, planFamily.Layout)
	if err != nil {
		return nil, err
	}
	var declared struct {
		Schema string `json:"schema"`
	}
	if json.Unmarshal(files["plan.json"], &declared) != nil || declared.Schema != PlanSchema && declared.Schema != PlanSchemaV2 && declared.Schema != PlanSchemaV3 && declared.Schema != PhasePlanSchema {
		return nil, invalid
	}
	if !sealed(declared.Schema, files) {
		return nil, errors.New("connected plan identity mismatch")
	}
	delete(files, "identity.sha256")
	return readPlan(files)
}
func sealed(schema string, files map[string][]byte) bool {
	return strings.TrimSpace(string(files["identity.sha256"])) == artifactdir.Identity(schema, files)
}
func readPlan(files map[string][]byte) (*Plan, error) {
	var d PlanDocument
	if json.Unmarshal(files["plan.json"], &d, json.RejectUnknownMembers(true)) != nil || d.Schema != PlanSchema && d.Schema != PlanSchemaV2 && d.Schema != PlanSchemaV3 && d.Schema != PhasePlanSchema {
		return nil, invalid
	}
	supplied := map[string][]byte{}
	for _, r := range references(d.Test) {
		supplied[r.File] = files["dependencies/"+r.SHA256]
	}
	if ref := d.Test.Environment.TargetRevision.Evidence; ref != nil {
		var receipt RevisionEvidence
		if json.Unmarshal(supplied[ref.File], &receipt, json.RejectUnknownMembers(true)) != nil {
			return nil, invalid
		}
		supplied[receipt.Source.File] = files["dependencies/"+receipt.Source.SHA256]
	}
	p, err := compile(files["test.json"], supplied, d.Generation, d.Schema == PhasePlanSchema)
	if err != nil {
		return nil, err
	}
	if len(files) != len(p.files) {
		return nil, errors.New("unexpected connected plan members")
	}
	for n, b := range p.files {
		if !bytes.Equal(b, files[n]) {
			return nil, errors.New("connected plan does not reproduce from retained inputs")
		}
	}
	return p, nil
}
func references(d Test) []Reference {
	refs := []Reference{d.Checks}
	refs = append(refs, d.Profiles...)
	refs = append(refs, d.Environment.Grants...)
	for _, r := range []*Reference{d.Ancestry, d.Setup.Plan, d.Setup.Cleanup, d.Environment.TargetRevision.Evidence} {
		if r != nil {
			refs = append(refs, *r)
		}
	}
	for _, s := range d.Steps {
		if s.V2 != nil {
			refs = append(refs, s.V2.Input)
		}
		if s.FHIR != nil && s.FHIR.Body != nil {
			refs = append(refs, *s.FHIR.Body)
		}
	}
	for _, ds := range d.Datasets {
		if ds.Projection != nil {
			refs = append(refs, *ds.Projection)
		}
		if ds.Completion.Policy != nil {
			refs = append(refs, *ds.Completion.Policy)
		}
		if ds.Completion.Barrier != nil {
			refs = append(refs, *ds.Completion.Barrier)
		}
	}
	return refs
}

// PrepareDirectory reads only a bounded local input directory. Each authored
// reference must name a member; links, devices and traversal are refused.
func PrepareDirectory(directory string, g Generation) (*Plan, error) {
	layout := artifactdir.Layout{Noun: "connected inputs", RequiredFiles: []string{"test.json"}, AllowFile: func(string) bool { return true }, AllowDirectory: func(string) bool { return true }, MaxFiles: 2048, MaxFileBytes: MaxBytes, MaxBytes: 2 * MaxBytes}
	files, err := artifactdir.Read(directory, layout)
	if err != nil {
		return nil, err
	}
	return Compile(files["test.json"], files, g)
}

func planFamilyFor(schema string) artifactdir.Family {
	f := planFamily
	f.Seal = artifactdir.DirectoryHash(schema)
	return f
}
