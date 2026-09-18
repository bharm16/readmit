package redact

import (
	"context"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/exportreview"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/testrunner"
)

// Create performs local review first. A fully handled case is tested privately
// against fresh built-in fixture sessions before its review becomes approvable.
// Blocked reviews contain findings only; no partially handled case is emitted.
func Create(ctx context.Context, request Request) (*Review, error) {
	t, source, spec, inputDigests, err := prepare(request)
	if err != nil {
		return nil, err
	}
	inputs, err := t.transformCase(source)
	if err != nil {
		return nil, err
	}
	derivedSpec, err := t.transformSpec(spec)
	if err != nil {
		return nil, err
	}
	specBytes, err := encode(derivedSpec)
	if err != nil || len(specBytes) > testrunner.MaxSpecBytes {
		return nil, errors.New("derived spec exceeds limits")
	}
	output, err := destination(request.Output, protectedPaths(t.local))
	if err != nil {
		return nil, err
	}
	private, err := destination(request.LocalState, protectedPaths(t.local))
	if err != nil {
		return nil, err
	}
	if output == private {
		return nil, errors.New("private mapping state and review must be separate directories")
	}
	review := &Review{Schema: ReviewSchema, State: "blocked", DataOrigin: "derived-testing-data", InputCommitment: inputDigests, RequiredFailures: slices.Clone(t.policy.RequiredFailures), OriginalFailedAssertions: []int{}, Scope: exportreview.Scope, Residual: exportreview.Scan{Status: "not-run", Locations: []string{}, Limitations: "Unresolved findings block generation and scanning."}}
	// Encoding the complete private state also bounds retained mapping material
	// before reserving any destination. No mapping bytes enter a derived artifact.
	localBytes, err := encode(t.local)
	if err != nil || len(localBytes) > maxReviewBytes {
		return nil, errors.New("private transformation state exceeds limit")
	}
	if err := os.Mkdir(private, 0700); err != nil {
		return nil, errors.New("cannot create private redaction state")
	}
	if !hasUnresolved(t.findings) {
		proofDir := filepath.Join(private, "original-proof")
		proof, err := runProof(ctx, spec, request.CasePath, proofDir, t.policy.RequiredFailures)
		if err != nil {
			if err := t.finding("proof/original-assertions", "other-unique-identifiers", "original-fixture-proof-failed", "", false); err != nil {
				return nil, err
			}
		} else {
			t.local.OriginalProof = proof
			review.OriginalFailedAssertions = slices.Clone(proof.FailedAssertions)
			t.local.Sources = append(t.local.Sources, sourceReference{Kind: "original-baseline", Path: filepath.Join(proofDir, "baseline", "result"), Identity: proof.BaselineIdentity}, sourceReference{Kind: "original-postfix", Path: filepath.Join(proofDir, "postfix", "result"), Identity: proof.PostfixIdentity})
		}
	}
	// Revalidate after private execution, before deriving from the sealed snapshot.
	if err := revalidate(t.local); err != nil {
		return nil, err
	}
	if err := os.Mkdir(output, 0700); err != nil {
		return nil, errors.New("cannot create redaction review")
	}
	if !hasUnresolved(t.findings) {
		derived, err := bundle.Write(filepath.Join(output, "case"), inputs, bundle.Provenance{Mode: bundle.Derived, Derivation: "readmit-redact/v1"})
		if err != nil {
			return nil, err
		}
		if !reflect.DeepEqual(derived.Correlations, source.Correlations) {
			return nil, errors.New("transformation changed source correlations; no completed review produced")
		}
		review.DerivedIdentity = derived.Identity
		review.DerivedSpecSHA256 = digest(specBytes)
		if err := writeFile(output, "spec.json", specBytes); err != nil {
			return nil, err
		}
		files, err := tree(output)
		if err != nil {
			return nil, err
		}
		review.Residual = exportreview.Residual(files, t.local.ResidualValues)
		for _, location := range review.Residual.Locations {
			if err := t.finding(location, "other-unique-identifiers", "known-residual", "", false); err != nil {
				return nil, err
			}
		}
		if !hasUnresolved(t.findings) {
			review.State = "ready-for-approval"
		}
	}
	localBytes, err = encode(t.local)
	if err != nil || len(localBytes) > maxReviewBytes {
		return nil, errors.New("private transformation state exceeds limit")
	}
	review.LocalStateCommitment = digest(localBytes)
	review.Findings = t.findings
	review.Coverage, review.Uncovered = exportreview.Checklist(review.Findings)
	review.Policies = []string{}
	for name := range t.policies {
		review.Policies = append(review.Policies, name)
	}
	slices.Sort(review.Policies)
	raw, err := encode(review)
	if err != nil || len(raw) > maxReviewBytes {
		return nil, errors.New("review exceeds size limit")
	}
	files, err := tree(output)
	if err != nil {
		return nil, err
	}
	// Scan includes the public manifest itself. No scan result can clear a
	// structural or policy finding, and a hit cannot complete an approvable review.
	files["review.json"] = raw
	if scan := exportreview.Residual(files, t.local.ResidualValues); review.State == "ready-for-approval" && scan.Status != "passed" {
		if len(review.Findings)+len(scan.Locations) > maxFindings {
			return nil, errors.New("public review exceeds finding limit")
		}
		review.State = "blocked"
		review.Residual = scan
		for _, location := range scan.Locations {
			review.Findings = append(review.Findings, exportreview.Finding{Location: location, Class: "other-unique-identifiers", Reason: "known-residual", Resolved: false})
		}
		review.Coverage, review.Uncovered = exportreview.Checklist(review.Findings)
		raw, err = encode(review)
		if err != nil || len(raw) > maxReviewBytes {
			return nil, errors.New("public review exceeds size limit")
		}
		files["review.json"] = raw
	}
	if err := writeFile(private, "state.json", localBytes); err != nil {
		return nil, err
	}
	if err := writeFile(output, "review.json", raw); err != nil {
		return nil, err
	}
	review.Identity = identity(ReviewSchema, files)
	if err := writeFile(output, "identity.sha256", []byte(review.Identity+"\n")); err != nil {
		return nil, err
	}
	return review, nil
}

func prepare(request Request) (*transformer, *bundle.Bundle, testrunner.Spec, string, error) {
	t := &transformer{policies: map[string]bool{}, original: map[string]*hl7.Document{}, derived: map[string]*hl7.Document{}, local: localState{Schema: PrivateSchema, Sources: []sourceReference{}, Mappings: []mapping{}, Shifts: []shift{}, ResidualValues: [][]byte{}}}
	fail := func(err error) (*transformer, *bundle.Bundle, testrunner.Spec, string, error) {
		return nil, nil, testrunner.Spec{}, "", err
	}
	source, err := bundle.Open(request.CasePath)
	if err != nil {
		return fail(err)
	}
	if len(source.Events) > 256 || len(source.Manifest.Sources) > 64 {
		return fail(errors.New("redaction fixture proof v1 supports at most 256 occurrences and 64 sources"))
	}
	paths := []struct{ kind, path string }{{"case", request.CasePath}, {"spec", request.SpecPath}, {"policy", request.PolicyPath}, {"inventory", request.InventoryPath}}
	values := map[string][]byte{}
	resolved := map[string]string{}
	for _, entry := range paths {
		path, err := canonical(entry.path)
		if err != nil {
			return fail(err)
		}
		resolved[entry.kind] = path
		id := source.Identity
		if entry.kind != "case" {
			raw, err := readLocal(path, maxConfigBytes)
			if err != nil {
				return fail(err)
			}
			values[entry.kind] = raw
			id = digest(raw)
		}
		t.local.Sources = append(t.local.Sources, sourceReference{Kind: entry.kind, Path: path, Identity: id})
	}
	t.policy, err = DecodePolicy(values["policy"])
	if err != nil {
		return fail(err)
	}
	spec, err := testrunner.DecodeSpec(values["spec"])
	if err != nil {
		return fail(err)
	}
	if len(spec.Input.Messages) != 2 {
		return fail(errors.New("redaction fixture proof v1 requires exactly two selected messages"))
	}
	input, err := bundle.Open(relative(filepath.Dir(resolved["spec"]), spec.Input.Case))
	if err != nil || input.Identity != source.Identity {
		return fail(errors.New("spec input must match the reviewed case"))
	}
	inventory, err := decodeInventory(values["inventory"])
	if err != nil {
		return fail(err)
	}
	if err := t.reviewInventory(inventory, filepath.Dir(resolved["inventory"]), source.Identity); err != nil {
		return fail(err)
	}
	commitment, _ := encode(Inputs{PolicySHA256: digest(values["policy"]), InventorySHA256: digest(values["inventory"]), SpecSHA256: digest(values["spec"])})
	return t, source, spec, digest(commitment), nil
}

func hasUnresolved(findings []exportreview.Finding) bool {
	for _, finding := range findings {
		if !finding.Resolved {
			return true
		}
	}
	return false
}

func revalidate(local localState) error {
	if len(local.Sources) < 4 {
		return errors.New("private state lacks source linkage")
	}
	for i, kind := range []string{"case", "spec", "policy", "inventory"} {
		if local.Sources[i].Kind != kind {
			return errors.New("private source linkage has an unsupported layout")
		}
	}
	sourceIdentity := local.Sources[0].Identity
	for _, reference := range local.Sources {
		id := ""
		switch reference.Kind {
		case "case":
			source, err := bundle.Open(reference.Path)
			if err != nil {
				return err
			}
			id = source.Identity
		case "spec", "policy", "inventory":
			raw, err := readLocal(reference.Path, maxConfigBytes)
			if err != nil {
				return err
			}
			id = digest(raw)
		case "original-baseline", "original-postfix":
			result, err := testrunner.Open(reference.Path)
			if err != nil || result.Result.InputBundleIdentity != sourceIdentity {
				return errors.New("original proof changed")
			}
			if reference.Kind == "original-baseline" && (result.Result.Status != testrunner.AssertionFailure || !slices.Equal(failedAssertions(result), local.OriginalProof.FailedAssertions) || result.Identity != local.OriginalProof.BaselineIdentity) || reference.Kind == "original-postfix" && (result.Result.Status != testrunner.Pass || result.Identity != local.OriginalProof.PostfixIdentity) {
				return errors.New("private proof claims disagree with verified evidence")
			}
			originalSpec, err := testrunner.ReadSpec(local.Sources[1].Path)
			if err != nil || !sameAssertionContract(&originalSpec, result.Spec) {
				return errors.New("private proof changed the original assertion contract")
			}
			id = result.Identity
		default:
			var err error
			id, err = artifactIdentity(reference.Kind, reference.Path, sourceIdentity)
			if err != nil {
				return err
			}
		}
		if id != reference.Identity {
			return errors.New("reviewed source or policy changed; create a new review")
		}
	}
	return nil
}

func OpenReview(path string) (*Review, error) {
	files, err := tree(path)
	if err != nil {
		return nil, err
	}
	for name := range files {
		if !regularReviewName(name) {
			return nil, errors.New("unexpected review artifact")
		}
	}
	var review Review
	if json.Unmarshal(files["review.json"], &review, json.RejectUnknownMembers(true)) != nil {
		return nil, errors.New("invalid export review")
	}
	if err := validateReview(review); err != nil {
		return nil, err
	}
	review.Identity = identity(ReviewSchema, files)
	if string(files["identity.sha256"]) != review.Identity+"\n" {
		return nil, errors.New("incomplete or changed export review")
	}
	if review.DerivedIdentity != "" {
		derived, err := bundle.Open(filepath.Join(path, "case"))
		if err != nil || derived.Manifest.Provenance.Mode != bundle.Derived || derived.Identity != review.DerivedIdentity || digest(files["spec.json"]) != review.DerivedSpecSHA256 {
			return nil, errors.New("derived material disagrees with review")
		}
		if _, err := testrunner.DecodeSpec(files["spec.json"]); err != nil {
			return nil, err
		}
	} else if len(files) != 2 {
		return nil, errors.New("blocked review has unexpected evidence")
	}
	return &review, nil
}

// Standalone and embedded reviews have the same versioned contract. Byte
// identities bind their containers; they cannot substitute for these checks.
func validateReview(review Review) error {
	if review.Schema != ReviewSchema || review.DataOrigin != "derived-testing-data" || review.State != "blocked" && review.State != "ready-for-approval" {
		return errors.New("invalid export review")
	}
	coverage, uncovered := exportreview.Checklist(review.Findings)
	if len(review.Findings) == 0 || len(review.Findings) > maxFindings || review.Scope != exportreview.Scope || !reflect.DeepEqual(coverage, review.Coverage) || !slices.Equal(uncovered, review.Uncovered) {
		return errors.New("review coverage disagrees with located findings")
	}
	if review.State == "ready-for-approval" && (hasUnresolved(review.Findings) || review.DerivedIdentity == "" || review.Residual.Status != "passed" || len(review.RequiredFailures) == 0 || !slices.Equal(review.RequiredFailures, review.OriginalFailedAssertions)) {
		return errors.New("review readiness is unsupported")
	}
	return nil
}
