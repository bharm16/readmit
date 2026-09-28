package connectedtest

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/fhirobserve"
	"github.com/bharm16/readmit/internal/fixturereset"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/localprofile"
	"github.com/bharm16/readmit/internal/observeinterval"
	"github.com/bharm16/readmit/internal/profileeval"
	"github.com/bharm16/readmit/internal/profilepack"
	"github.com/bharm16/readmit/internal/profileversion"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"github.com/bharm16/readmit/internal/testrunner"
)

var identifier = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
var occurrence = regexp.MustCompile(`^s[0-9]{4}-e[0-9]{6}$`)
var hash = regexp.MustCompile(`^[0-9a-f]{64}$`)
var invalid = errors.New("invalid connected test contract")

// Compile has no ambient inputs, resolver, dialer, clock or secret provider.
// Dependencies are supplied bytes; even a network-shaped reference is refused.
func Compile(raw []byte, supplied map[string][]byte, generation Generation) (*Plan, error) {
	return compile(raw, supplied, generation, false)
}
func compile(raw []byte, supplied map[string][]byte, generation Generation, phase bool) (*Plan, error) {
	var d Test
	if len(raw) > MaxBytes || json.Unmarshal(raw, &d, json.RejectUnknownMembers(true)) != nil {
		return nil, invalid
	}
	if (d.Schema == PhaseTestSchema || d.Schema == PhaseTestSchemaV2) && !phase {
		return nil, invalid
	}
	fhirPhase := d.Schema == PhaseTestSchemaV2
	if !fhirPhase && (d.Servers != nil || d.Responses != nil || d.Validations != nil) {
		return nil, invalid
	}
	operatorVersion := OperatorVersion
	if d.Schema == TestSchemaV2 || intervalTest(d.Schema) {
		operatorVersion = OperatorVersionV2
	}
	if (d.Schema != TestSchema && d.Schema != TestSchemaV2 && !intervalTest(d.Schema)) || !identifier.MatchString(d.Project) || !identifier.MatchString(d.ID) || !short(d.Revision) || d.OperatorVersion != operatorVersion {
		return nil, invalid
	}
	if (d.Schema == TestSchemaV2 || intervalTest(d.Schema)) && (d.Checks.Schema != assertion.DatasetSchema || d.Bindings != (Bindings{})) {
		return nil, invalid
	}
	if d.Limits.MaxSteps < 1 || d.Limits.MaxSteps > 256 || d.Limits.MaxBytes < 1 || d.Limits.MaxBytes > MaxBytes || d.Limits.DeadlineMS < 1 || d.Limits.DeadlineMS > 3600000 || len(d.Steps) < 1 || len(d.Steps) > d.Limits.MaxSteps {
		return nil, invalid
	}
	base, err := time.Parse(time.RFC3339Nano, generation.BaseTime)
	if err != nil {
		return nil, errors.New("generation requires an explicit base instant")
	}
	if d.Schema == TestSchema {
		var fields struct {
			Datasets []map[string]jsontext.Value `json:"datasets"`
		}
		if json.Unmarshal(raw, &fields) != nil {
			return nil, invalid
		}
		for _, ds := range fields.Datasets {
			if _, ok := ds["namespace"]; ok {
				return nil, invalid
			}
			if _, ok := ds["projection"]; ok {
				return nil, invalid
			}
		}
	}
	if !intervalTest(d.Schema) {
		var fields struct {
			Datasets []struct {
				Completion map[string]jsontext.Value `json:"completion"`
			} `json:"datasets"`
		}
		if json.Unmarshal(raw, &fields) != nil {
			return nil, invalid
		}
		for _, ds := range fields.Datasets {
			if _, ok := ds.Completion["policy"]; ok {
				return nil, invalid
			}
		}
	}
	canonical, err := encode(d)
	if err != nil {
		return nil, err
	}
	planSchema := PlanSchema
	if d.Schema == TestSchemaV2 || intervalTest(d.Schema) {
		planSchema = PlanSchemaV2
	}
	if intervalTest(d.Schema) {
		planSchema = PlanSchemaV3
	}
	if d.Schema == PhaseTestSchema {
		planSchema = PhasePlanSchema
	}
	if fhirPhase {
		planSchema = PhasePlanSchemaV2
	}
	p := &Plan{document: PlanDocument{Schema: planSchema, TestIdentity: Digest(canonical), Test: d, Environment: d.Environment, Generation: generation, Resolution: map[string]string{}}, files: map[string][]byte{"test.json": canonical}}
	total := len(canonical)
	resolve := func(r Reference, schema string) ([]byte, error) {
		b, ok := supplied[r.File]
		if r.Project != d.Project || !identifier.MatchString(r.ID) || r.Schema != schema || !fs.ValidPath(r.File) || strings.Contains(r.File, "\\") || !hash.MatchString(r.SHA256) || !ok || len(b) > MaxBytes || Digest(b) != r.SHA256 {
			return nil, errors.New("unresolved, unsupported or cross-project dependency")
		}
		name := "dependencies/" + r.SHA256
		if _, ok := p.files[name]; !ok {
			total += len(b)
			if total > d.Limits.MaxBytes {
				return nil, errors.New("connected input byte limit")
			}
			p.files[name] = append([]byte(nil), b...)
		}
		return b, nil
	}
	env := d.Environment
	if env.Project != d.Project || !identifier.MatchString(env.ID) || !short(env.Revision) || !short(env.Name) || !identifier.MatchString(env.Endpoint) || !hash.MatchString(env.TargetIdentity) || !hash.MatchString(env.AddressPolicyIdentity) {
		return nil, invalid
	}
	if !slices.Contains([]string{"nonproduction", "unclassified", "test", "staging", "development", "unknown"}, env.Classification) {
		return nil, errors.New("connected test requires an explicit nonproduction classification or unknown")
	}
	if env.TLS.Mode != "plain" && env.TLS.Mode != "tls" && env.TLS.Mode != "mtls" || env.TLS.Mode == "plain" && env.TLS.ServerName != "" || env.TLS.Mode != "plain" && !short(env.TLS.ServerName) {
		return nil, invalid
	}
	rev := env.TargetRevision
	switch rev.Provenance {
	case "unknown":
		if rev.Value != "" || rev.Evidence != nil {
			return nil, invalid
		}
	case "operator-declared":
		if !short(rev.Value) || rev.Evidence != nil {
			return nil, invalid
		}
	case "independently-observed":
		if !short(rev.Value) || rev.Evidence == nil {
			return nil, invalid
		}
		raw, err := resolve(*rev.Evidence, RevisionEvidenceSchema)
		if err != nil {
			return nil, err
		}
		var receipt RevisionEvidence
		if json.Unmarshal(raw, &receipt, json.RejectUnknownMembers(true)) != nil || receipt.Schema != RevisionEvidenceSchema || receipt.TargetIdentity != env.TargetIdentity || receipt.AddressPolicyIdentity != env.AddressPolicyIdentity || receipt.ServerName != env.TLS.ServerName || receipt.Revision != rev.Value || !short(receipt.CollectorVersion) {
			return nil, errors.New("preflight revision binding mismatch")
		}
		if _, err := time.Parse(time.RFC3339Nano, receipt.ObservedAt); err != nil {
			return nil, invalid
		}
		if _, err := resolve(receipt.Source, "target-metadata"); err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("unsupported target revision provenance")
	}
	for _, ref := range env.Grants {
		if ref.Schema != sendpolicy.PolicySchema && ref.Schema != sendpolicy.ScopedPolicySchema {
			return nil, invalid
		}
		b, err := resolve(ref, ref.Schema)
		if err != nil {
			return nil, err
		}
		if ref.Schema == sendpolicy.ScopedPolicySchema {
			policy, err := sendpolicy.DecodeScopedPolicy(b)
			if err != nil || policy.Project != env.Project || policy.Environment != env.ID || policy.Revision != env.Revision {
				return nil, errors.New("scoped policy environment differs")
			}
		} else if _, err := sendpolicy.DecodePolicy(b); err != nil {
			return nil, err
		}
		if Digest(b) != env.AddressPolicyIdentity {
			return nil, errors.New("address policy pin mismatch")
		}
	}
	if !slices.Contains([]string{"operator-declared", "fixture-reset"}, d.Setup.Kind) || !short(d.Setup.Isolation) || !short(d.Setup.Instructions) || d.Setup.Kind == "fixture-reset" && d.Setup.Plan == nil {
		return nil, invalid
	}
	for _, ref := range []*Reference{d.Setup.Plan, d.Setup.Cleanup} {
		if ref != nil {
			b, err := resolve(*ref, fixturereset.PlanSchema)
			if err != nil {
				return nil, err
			}
			plan, err := fixturereset.DecodePlan(b)
			if err != nil {
				return nil, err
			}
			if plan.Environment != env.Name {
				return nil, errors.New("setup environment mismatch")
			}
		}
	}
	if d.Ancestry != nil {
		b, err := resolve(*d.Ancestry, testrunner.SpecSchema)
		if err != nil {
			return nil, err
		}
		if _, err := testrunner.DecodeSpec(b); err != nil {
			return nil, err
		}
	}
	for _, ref := range d.Profiles {
		b, err := resolve(ref, ref.Schema)
		if err != nil {
			return nil, err
		}
		switch ref.Schema {
		case profileeval.ProfileSchema, profileeval.ProfileSchemaV3:
			_, err = profileeval.DecodeProfile(b)
		case profileeval.PackSchema, profileeval.PackSchemaV3, profileeval.PackSchemaV4:
			_, err = profileeval.DecodePack(b)
		case localprofile.Schema:
			_, err = localprofile.Decode(b)
		case profilepack.Schema:
			_, err = profilepack.Decode(b)
		case profileversion.VersionSchema:
			_, err = profileversion.DecodeVersion(b)
		default:
			return nil, errors.New("unsupported profile pin")
		}
		if err != nil {
			return nil, err
		}
	}
	if len(d.Variables) > 256 {
		return nil, invalid
	}
	declared := map[string]bool{}
	runtime := map[string]bool{}
	for _, v := range d.Variables {
		if !identifier.MatchString(v.ID) || declared[v.ID] {
			return nil, invalid
		}
		declared[v.ID] = true
		switch v.Kind {
		case "response":
			// Bound only from an actual earlier response during execution.
			if !fhirPhase || v.Value != "" || v.Namespace != "" || v.OffsetMS != 0 {
				return nil, invalid
			}
			runtime[v.ID] = true
		case "literal":
			if !short(v.Value) || v.Namespace != "" || v.OffsetMS != 0 {
				return nil, invalid
			}
			p.document.Resolution[v.ID] = v.Value
		case "synthetic-id":
			if !identifier.MatchString(v.Namespace) || v.Value != "" || v.OffsetMS != 0 {
				return nil, invalid
			}
			stream := sha256.Sum256([]byte(v.Namespace + ":" + v.ID))
			rng := rand.NewPCG(generation.Seed, binary.BigEndian.Uint64(stream[:8]))
			p.document.Resolution[v.ID] = fmt.Sprintf("%s-%016x", v.Namespace, rng.Uint64())
		case "timestamp":
			if v.Value != "" || v.Namespace != "" || v.OffsetMS < -86400000 || v.OffsetMS > 86400000 {
				return nil, invalid
			}
			p.document.Resolution[v.ID] = base.Add(time.Duration(v.OffsetMS) * time.Millisecond).Format("20060102150405-0700")
		default:
			return nil, errors.New("unsupported generation operator")
		}
	}
	if fhirPhase {
		if err := compileServers(d, resolve); err != nil {
			return nil, err
		}
	}
	seen := map[string]bool{}
	occurrences := map[string]bool{}
	for _, s := range d.Steps {
		if s.Interaction != nil {
			if !fhirPhase || !identifier.MatchString(s.ID) || seen[s.ID] {
				return nil, invalid
			}
			seen[s.ID] = true
			if err := compileInteraction(p, d, s, runtime, resolve); err != nil {
				return nil, err
			}
			total += len(p.files["inputs/"+s.ID+".fhir"])
			if total > d.Limits.MaxBytes {
				return nil, errors.New("compiled input byte limit")
			}
			p.document.Effects = append(p.document.Effects, Effect{Step: s.ID, Kind: "fhir-interaction", Endpoint: s.Endpoint})
			continue
		}
		if !identifier.MatchString(s.ID) || seen[s.ID] || s.Endpoint != env.Endpoint || (s.V2 == nil) == (s.FHIR == nil) || fhirPhase && s.FHIR != nil {
			return nil, invalid
		}
		seen[s.ID] = true
		for _, k := range s.BusinessKeys {
			if !short(k.Namespace) || !short(k.Value) {
				return nil, invalid
			}
		}
		effect := "fhir-request"
		if s.V2 != nil {
			effect = "v2-send"
			if !occurrence.MatchString(s.V2.Occurrence) || d.Schema != PhaseTestSchema && d.Schema != PhaseTestSchemaV2 && occurrences[s.V2.Occurrence] {
				return nil, invalid
			}
			occurrences[s.V2.Occurrence] = true
			b, err := resolve(s.V2.Input, "hl7")
			if err != nil {
				return nil, err
			}

			doc, err := hl7.Parse(b, hl7.Options{})
			if err != nil || len(doc.Messages) != 1 {
				return nil, errors.New("stimulus must contain exactly one v2 message")
			}
			edits := []hl7.Edit{}
			for _, a := range s.V2.Assignments {
				value, ok := p.document.Resolution[a.Variable]
				selector, err := hl7.ParseSelector(a.Selector)
				if !ok || err != nil || strings.ContainsAny(value, "|^~\\&\r\n") {
					return nil, errors.New("invalid synthetic field assignment")
				}
				edits = append(edits, hl7.Edit{Selector: selector, Value: []byte(value)})
			}
			if len(edits) > 0 {
				rewritten, err := doc.Rewrite(0, edits, hl7.StandardDelimiters)
				if err != nil {
					return nil, err
				}
				b = rewritten.Bytes
			}
			total += len(b)
			if total > d.Limits.MaxBytes {
				return nil, errors.New("compiled input byte limit")
			}
			p.files["inputs/"+s.ID+".hl7"] = append([]byte(nil), b...)
		} else {
			r := s.FHIR
			u, err := url.Parse(r.RelativeURL)
			if r.Version != "4.0.1" || !slices.Contains([]string{"GET", "POST", "PUT", "DELETE", "PATCH"}, r.Method) || err != nil || u.IsAbs() || u.Host != "" || strings.HasPrefix(r.RelativeURL, "/") || !short(r.RelativeURL) || u.Fragment != "" || strings.Contains(r.RelativeURL, "..") {
				return nil, errors.New("unsupported FHIR request template")
			}
			if r.Body != nil {
				b, err := resolve(*r.Body, "fhir-r4-json")
				if err != nil {
					return nil, err
				}
				var body map[string]jsontext.Value
				if json.Unmarshal(b, &body) != nil || body["resourceType"] == nil {
					return nil, errors.New("invalid FHIR resource JSON")
				}
			}
		}
		p.document.Effects = append(p.document.Effects, Effect{Step: s.ID, Kind: effect, Endpoint: s.Endpoint})
	}
	for _, s := range d.Steps {
		deps := map[string]bool{}
		for _, a := range s.After {
			if !seen[a] || a == s.ID || deps[a] {
				return nil, errors.New("invalid step dependency")
			}
			deps[a] = true
		}
	}
	// Stable topological order preserves authored order for independent steps.
	done := map[string]bool{}
	for len(done) < len(d.Steps) {
		progress := false
		for _, s := range d.Steps {
			if done[s.ID] {
				continue
			}
			ready := true
			for _, a := range s.After {
				ready = ready && done[a]
			}
			if ready {
				done[s.ID] = true
				p.document.Order = append(p.document.Order, s.ID)
				progress = true
			}
		}
		if !progress {
			return nil, errors.New("cyclic step dependencies")
		}
	}
	datasets := map[string]Dataset{}
	if len(d.Datasets) > 256 {
		return nil, invalid
	}
	for _, ds := range d.Datasets {
		c := ds.Completion
		if !identifier.MatchString(ds.ID) || datasets[ds.ID].ID != "" || !slices.Contains([]string{"v2-messages", "record-keys", "fhir-resources", "typed-rows"}, ds.Kind) || !slices.Contains([]string{"before", "after"}, ds.Phase) || !short(ds.Source) || !(intervalTest(d.Schema) && slices.Contains([]string{"full-horizon", "processing-barrier"}, c.Kind) || !intervalTest(d.Schema) && slices.Contains([]string{"bounded-horizon", "ack-responses"}, c.Kind)) || c.HorizonMS < 1 || intervalTest(d.Schema) && c.HorizonMS > 300000 || c.Barrier != nil || c.MaxRecords < 1 || c.MaxRecords > 1000000 || c.MaxBytes < 1 || c.MaxBytes > MaxBytes {
			return nil, invalid
		}
		var definition observeinterval.Definition
		if intervalTest(d.Schema) {
			if c.Policy == nil || ds.Kind != "typed-rows" && !(fhirPhase && ds.Kind == "fhir-resources") {
				return nil, invalid
			}
			raw, err := resolve(*c.Policy, observeinterval.Schema)
			if err != nil {
				return nil, err
			}
			definition, err = observeinterval.Decode(raw)
			if err != nil || definition.Source != ds.Source || definition.Namespace != ds.Namespace || definition.HorizonMS != c.HorizonMS || definition.MaxRecords != c.MaxRecords || definition.MaxBytes != c.MaxBytes || (definition.Barrier != nil) != (c.Kind == "processing-barrier") {
				return nil, invalid
			}
		} else if c.Policy != nil || c.HorizonMS > d.Limits.DeadlineMS {
			return nil, invalid
		}
		if c.Kind == "ack-responses" && (ds.Source != "legacy-ack" || ds.Kind != "v2-messages" || ds.Phase != "after") {
			return nil, invalid
		}
		if ds.Kind == "typed-rows" {
			if d.Schema != TestSchemaV2 && !intervalTest(d.Schema) || !identifier.MatchString(ds.Namespace) || !hash.MatchString(ds.Source) || ds.Projection == nil || !intervalTest(d.Schema) && c.Kind != "bounded-horizon" {
				return nil, invalid
			}
			raw, err := resolve(*ds.Projection, dataset.ProjectionSchema)
			if err != nil {
				return nil, err
			}
			projection, err := dataset.DecodeProjection(raw)
			if err != nil || projection.Limits.MaxRows > c.MaxRecords || projection.Limits.MaxBytes > c.MaxBytes || !intervalTest(d.Schema) && projection.Limits.TimeoutMS > c.HorizonMS {
				return nil, invalid
			}
		} else if fhirPhase && ds.Kind == "fhir-resources" {
			if ds.Projection == nil {
				return nil, invalid
			}
			raw, err := resolve(*ds.Projection, fhirobserve.Schema)
			if err != nil {
				return nil, err
			}
			if _, err = compileObservation(d, ds, raw, definition, p.document.Resolution); err != nil {
				return nil, err
			}
		} else if ds.Namespace != "" || ds.Projection != nil {
			return nil, invalid
		}
		datasets[ds.ID] = ds
	}
	for _, b := range []struct{ id, kind, phase string }{{d.Bindings.Observed, "v2-messages", "after"}, {d.Bindings.Before, "record-keys", "before"}, {d.Bindings.After, "record-keys", "after"}} {
		if b.id != "" {
			ds, ok := datasets[b.id]
			if !ok || ds.Kind != b.kind || ds.Phase != b.phase {
				return nil, errors.New("incompatible dataset binding")
			}
		}
	}
	if d.Checks.Schema == assertion.DatasetSchema && (d.Schema == TestSchemaV2 || intervalTest(d.Schema)) {
		raw, err := resolve(d.Checks, assertion.DatasetSchema)
		if err != nil {
			return nil, err
		}
		set, err := assertion.DecodeDatasets(raw)
		if err != nil {
			return nil, err
		}
		if err := validateTypedBindings(set.Document(), d, p.files); err != nil {
			return nil, err
		}
		if err := validateFHIRChecks(d); err != nil {
			return nil, err
		}
	} else {
		checkBytes, err := resolve(d.Checks, assertion.Schema)
		if err != nil {
			return nil, err
		}
		checks, err := assertion.Decode(checkBytes)
		if err != nil {
			return nil, err
		}
		for _, a := range checks.Assertions {
			if err := validateBinding(a, d.Bindings, occurrences); err != nil {
				return nil, err
			}
		}
	}
	names := make([]string, 0, len(p.files))
	for n := range p.files {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		b := p.files[n]
		p.document.Members = append(p.document.Members, Member{Path: n, SHA256: Digest(b), Size: len(b)})
	}
	planBytes, err := encode(p.document)
	if err != nil {
		return nil, err
	}
	if len(planBytes) > MaxBytes {
		return nil, errors.New("compiled plan byte limit")
	}
	p.files["plan.json"] = planBytes
	p.identity = Digest(planBytes)
	return p, nil
}
func short(s string) bool {
	return strings.TrimSpace(s) != "" && len(s) <= 8192 && !strings.ContainsRune(s, 0)
}
func validateBinding(a assertion.Assertion, b Bindings, inputs map[string]bool) error {
	field := func(f assertion.FieldRef) error {
		if f.Scope == assertion.InputMessages && !inputs[f.Message] || f.Scope == assertion.ObservedMessages && b.Observed == "" {
			return errors.New("check refers to an unbound message scope")
		}
		return nil
	}
	record := func(s assertion.RecordScope) error {
		if s == assertion.BeforeRecords && b.Before == "" || s == assertion.AfterRecords && b.After == "" {
			return errors.New("check refers to an unbound dataset")
		}
		return nil
	}
	if a.When != nil {
		if err := field(a.When.Field); err != nil {
			return err
		}
	}
	s := a.Subject
	switch {
	case s.Field != nil:
		return field(*s.Field)
	case s.Pair != nil:
		if err := field(s.Pair.Left); err != nil {
			return err
		}
		return field(s.Pair.Right)
	case s.Collection != nil:
		return record(s.Collection.Scope)
	case s.Each != nil:
		return record(s.Each.Scope)
	case s.Transition != nil:
		if err := record(s.Transition.From); err != nil {
			return err
		}
		return record(s.Transition.To)
	}
	return invalid
}
