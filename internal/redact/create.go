package redact

import (
	"context"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/exportreview"
	"github.com/bharm16/readmit/internal/testrunner"
)

// errUnsynced is a review or its private state whose directory entries could
// not be synced once both were complete.
var errUnsynced = errors.New("cannot sync redaction output; the review and its private state were written in full but a power loss could still lose them")

// privateFamily is a review's private state: the original proof and the
// transformation state the review commits to, written last.
var privateFamily = artifactdir.Family{
	Layout: artifactdir.Layout{
		Nested:    []string{"original-proof"},
		AllowFile: func(name string) bool { return name == "state.json" },
	},
	Seal: artifactdir.CompletionRecord("state.json", ""),
	Errors: artifactdir.Errors{
		Reserve: errors.New("cannot create private redaction state"),
		Create:  errCreateFile,
		Write:   errWriteFile,
		Sync:    errUnsynced,
	},
}

// reviewFamily is a disclosure review: the derived case and spec when every
// finding was handled, and the review itself, sealed by the ADR-0002 identity
// of all of them.
var reviewFamily = artifactdir.Family{
	Layout: artifactdir.Layout{
		Noun:         "redaction review",
		Nested:       []string{"case"},
		AllowFile:    regularReviewName,
		MaxFiles:     maxPacketFiles,
		MaxFileBytes: maxReviewBytes,
		MaxBytes:     maxPacketBytes,
	},
	Seal: artifactdir.DirectoryHash(ReviewSchema),
	Errors: artifactdir.Errors{
		Reserve: errors.New("cannot create redaction review"),
		Create:  errCreateFile,
		Write:   errWriteFile,
		Sync:    errUnsynced,
	},
}

// Create performs local review first. A fully handled case is tested privately
// against fresh built-in fixture sessions before its review becomes approvable.
// Blocked reviews contain findings only; no partially handled case is emitted.
//
// Create is the wiring of the three steps: prepare reads and verifies the
// request's files, derive turns them into derived evidence without touching
// disk or sockets, prove reaches the built-in fixture through [FixtureProver],
// and seal writes the review, the private state and the identity binding them.
func Create(ctx context.Context, request Request) (*Review, error) {
	return createWithProver(ctx, request, FixtureProver{})
}

// createWithProver is Create with the prove step's adapter chosen by the
// caller. Production wiring always hands it [FixtureProver]; tests stand in a
// fake, so no test swaps a package variable to reach this step.
func createWithProver(ctx context.Context, request Request, prover Prover) (*Review, error) {
	prepared, err := prepare(request)
	if err != nil {
		return nil, err
	}
	derived, err := derive(prepared.inputs)
	if err != nil {
		return nil, err
	}
	specBytes, err := encode(derived.Spec)
	if err != nil || len(specBytes) > testrunner.MaxSpecBytes {
		return nil, errors.New("derived spec exceeds limits")
	}
	return seal(ctx, request, prover, prepared, &derived, specBytes)
}

// prepared is what reading a request establishes before any derivation: the
// verified derive inputs and the commitment binding the exact reviewed bytes.
type prepared struct {
	inputs     deriveInputs
	commitment string
}

func prepare(request Request) (prepared, error) {
	fail := func(err error) (prepared, error) { return prepared{}, err }
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
	sources := []sourceReference{}
	for _, entry := range paths {
		path, err := artifactpath.Resolve(entry.path)
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
		sources = append(sources, sourceReference{Kind: entry.kind, Path: path, Identity: id})
	}
	policy, err := DecodePolicy(values["policy"])
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
	input, err := bundle.Open(artifactpath.JoinReference(filepath.Dir(resolved["spec"]), spec.Input.Case))
	if err != nil || input.Identity != source.Identity {
		return fail(errors.New("spec input must match the reviewed case"))
	}
	inventory, err := DecodeInventory(values["inventory"])
	if err != nil {
		return fail(err)
	}
	facts, err := verifyArtifacts(inventory, filepath.Dir(resolved["inventory"]), source.Identity)
	if err != nil {
		return fail(err)
	}
	commitment, _ := encode(Inputs{PolicySHA256: digest(values["policy"]), InventorySHA256: digest(values["inventory"]), SpecSHA256: digest(values["spec"])})
	return prepared{
		inputs: deriveInputs{
			Case: source, CasePath: request.CasePath, Spec: spec, Policy: policy,
			Inventory: inventory, Artifacts: facts, Sources: sources,
		},
		commitment: digest(commitment),
	}, nil
}

// seal writes the review and its private state: the derived case and spec when
// every finding was handled, the fresh fixture proof of the original case, the
// identity binding all of them, and the located findings when anything stayed
// unresolved. A proof or residual failure blocks the review rather than
// widening it, and the proof's own explanation is said to the caller only.
func seal(ctx context.Context, request Request, prover Prover, prepared prepared, derived *derivation, specBytes []byte) (*Review, error) {
	output, err := destination(request.Output, protectedPaths(derived.Local))
	if err != nil {
		return nil, err
	}
	private, err := destination(request.LocalState, protectedPaths(derived.Local))
	if err != nil {
		return nil, err
	}
	if output == private {
		return nil, errors.New("private mapping state and review must be separate directories")
	}
	review := &Review{Schema: ReviewSchema, State: "blocked", DataOrigin: "derived-testing-data", InputCommitment: prepared.commitment, RequiredFailures: slices.Clone(prepared.inputs.Policy.RequiredFailures), OriginalFailedAssertions: []int{}, Scope: exportreview.Scope, Residual: exportreview.Scan{Status: "not-run", Locations: []string{}, Limitations: "Unresolved findings block generation and scanning."}}
	// Encoding the complete private state also bounds retained mapping material
	// before reserving any destination. No mapping bytes enter a derived artifact.
	localBytes, err := encode(derived.Local)
	if err != nil || len(localBytes) > maxReviewBytes {
		return nil, errors.New("private transformation state exceeds limit")
	}
	state, err := artifactdir.Create(private, privateFamily, artifactdir.Durable)
	if err != nil {
		return nil, err
	}
	defer state.Close()
	if !hasUnresolved(derived.Findings) {
		proofDir := filepath.Join(private, "original-proof")
		proof, proveErr := prover.Prove(ctx, prepared.inputs.Spec, request.CasePath, proofDir, prepared.inputs.Policy.RequiredFailures)
		if proveErr != nil {
			review.OriginalProofFailure = proveErr.Error()
			if err := derived.finding("proof/original-assertions", "other-unique-identifiers", "original-fixture-proof-failed", "", false); err != nil {
				return nil, err
			}
		} else {
			derived.Local.OriginalProof = proof
			review.OriginalFailedAssertions = slices.Clone(proof.FailedAssertions)
			derived.Local.Sources = append(derived.Local.Sources, sourceReference{Kind: "original-baseline", Path: filepath.Join(proofDir, "baseline", "result"), Identity: proof.BaselineIdentity}, sourceReference{Kind: "original-postfix", Path: filepath.Join(proofDir, "postfix", "result"), Identity: proof.PostfixIdentity})
		}
	}
	// Revalidate after private execution, before deriving from the sealed snapshot.
	if err := revalidate(derived.Local); err != nil {
		return nil, err
	}
	written, err := artifactdir.Create(output, reviewFamily, artifactdir.Durable)
	if err != nil {
		return nil, err
	}
	defer written.Close()
	if !hasUnresolved(derived.Findings) {
		writtenCase, err := bundle.Write(filepath.Join(output, "case"), derived.Occurrences, bundle.Provenance{Mode: bundle.Derived, Derivation: "readmit-redact/v1"})
		if err != nil {
			return nil, err
		}
		if !reflect.DeepEqual(writtenCase.Correlations, prepared.inputs.Case.Correlations) {
			return nil, errors.New("transformation changed source correlations; no completed review produced")
		}
		review.DerivedIdentity = writtenCase.Identity
		review.DerivedSpecSHA256 = digest(specBytes)
		if err := written.WriteFile("spec.json", specBytes); err != nil {
			return nil, err
		}
		files, err := tree(output)
		if err != nil {
			return nil, err
		}
		review.Residual = exportreview.Residual(files, derived.Local.ResidualValues)
		for _, location := range review.Residual.Locations {
			if err := derived.finding(location, "other-unique-identifiers", "known-residual", "", false); err != nil {
				return nil, err
			}
		}
		if !hasUnresolved(derived.Findings) {
			review.State = "ready-for-approval"
		}
	}
	localBytes, err = encode(derived.Local)
	if err != nil || len(localBytes) > maxReviewBytes {
		return nil, errors.New("private transformation state exceeds limit")
	}
	review.LocalStateCommitment = digest(localBytes)
	review.Findings = derived.Findings
	review.Coverage, review.Uncovered = exportreview.Checklist(review.Findings)
	review.Policies = []string{}
	for name := range derived.Policies {
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
	if scan := exportreview.Residual(files, derived.Local.ResidualValues); review.State == "ready-for-approval" && scan.Status != "passed" {
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
	if _, err := state.Complete(localBytes); err != nil {
		return nil, err
	}
	if err := written.WriteFile("review.json", raw); err != nil {
		return nil, err
	}
	if review.Identity, err = written.Complete(nil); err != nil {
		return nil, err
	}
	// Export reads state.json and the original proof results through the
	// private folder, and the review through its own; each case, result and
	// run synced its own entries. So original-proof/, each folder and its
	// entry in the folder holding it are synced before the review is
	// reported. Every file is by then, so a failure leaves a review that may
	// open and says so.
	var proofDirectories []string
	if _, err := os.Lstat(filepath.Join(private, "original-proof")); err == nil {
		proofDirectories = []string{"original-proof"}
	}
	if err := state.SyncDirectories(proofDirectories...); err != nil {
		return nil, err
	}
	if err := state.Sync(); err != nil {
		return nil, err
	}
	if err := written.Sync(); err != nil {
		return nil, err
	}
	return review, nil
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
	path, err := artifactpath.Directory(path)
	if err != nil {
		return nil, err
	}
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
