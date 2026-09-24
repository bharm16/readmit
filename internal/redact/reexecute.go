package redact

import (
	"context"
	"encoding/json/v2"
	"errors"
	"path/filepath"
	"reflect"
	"slices"

	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/report"
	"github.com/bharm16/readmit/internal/runresult"
	"github.com/bharm16/readmit/internal/testrunner"
)

// ReexecutionRequest selects one separately reset failure or pass execution.
// Paths remain customer-local; an original packet is never copied for sharing.
type ReexecutionRequest struct {
	ReviewPath, LocalState, Approval, OriginalPacket, SpecPath, Phase string
}

// Reexecution is a prepared, locally validated send. Preparation opens no network.
// Each execution needs its own authorization and fresh output directory.
type Reexecution struct {
	request   ReexecutionRequest
	prepared  *durablerun.Prepared
	binding   ReexecutionAssessment
	protected []string
}

// ReexecutionAssessment deliberately separates a match of selected criteria
// from external equivalence and from approval of the newly collected bytes.
// This is an output document, not an executable input or a disclosure approval.
type ReexecutionAssessment struct {
	Schema                 string `json:"schema"`
	ReviewIdentity         string `json:"review_identity"`
	OriginalPacketIdentity string `json:"original_packet_identity"`
	OriginalResultIdentity string `json:"original_result_identity"`
	DerivedCaseIdentity    string `json:"derived_case_identity"`
	ExecutionSpecIdentity  string `json:"execution_spec_identity"`
	Phase                  string `json:"phase"`
	ResultIdentity         string `json:"result_identity"`
	Criteria               string `json:"criteria"`
	ExternalEquivalence    string `json:"external_equivalence"`
	Disclosure             string `json:"disclosure"`
	Reason                 string `json:"reason"`
}

// Preview states no execution verdict and does not approve any newly acquired
// ACK, observation, metadata or historical target configuration for disclosure.
func (p *Reexecution) Preview() ReexecutionAssessment { return p.binding }

// PinnedInputs are the prepared inputs Execute would send: the rebound
// specification, the target configuration and the outbound mapping of each
// selected occurrence of the approved derived case. Reading them sends nothing.
func (p *Reexecution) PinnedInputs() testrunner.PinnedInputs { return p.prepared.PinnedInputs() }

func PrepareReexecution(ctx context.Context, request ReexecutionRequest) (*Reexecution, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if request.Approval == "" || request.OriginalPacket == "" || request.SpecPath == "" || (request.Phase != "failure" && request.Phase != "pass") {
		return nil, errors.New("reexecution requires exact review approval, retained original packet, spec and failure or pass phase")
	}
	review, err := OpenReview(request.ReviewPath)
	if err != nil {
		return nil, err
	}
	if request.Approval != review.Identity || review.State != "ready-for-approval" {
		return nil, errors.New("reexecution requires the exact complete disclosure review; changed artifacts require review again")
	}
	raw, err := readLocal(filepath.Join(request.LocalState, "state.json"), maxReviewBytes)
	if err != nil || digest(raw) != review.LocalStateCommitment {
		return nil, errors.New("private source linkage differs from approved review")
	}
	var local localState
	if json.Unmarshal(raw, &local, json.RejectUnknownMembers(true)) != nil || local.Schema != PrivateSchema {
		return nil, errors.New("invalid private source linkage")
	}
	if err := revalidate(local); err != nil {
		return nil, err
	}
	original, err := report.OpenRetained(ctx, request.OriginalPacket)
	if err != nil {
		return nil, err
	}
	run := original.Manifest.Current
	if run.CaseIdentity != local.Sources[0].Identity || run.JournalIncomplete || run.DeliveryUncertain || run.Status == string(testrunner.ExecutionError) {
		return nil, errors.New("original packet must contain a completed certain run of the reviewed original case")
	}
	originalPath := filepath.Join(request.OriginalPacket, "current")
	retained, err := runresult.Open(originalPath)
	if err != nil {
		return nil, err
	}
	if retained.Artifact == nil {
		return nil, errors.New("original packet retains no finalized result")
	}
	if usable, _ := retained.Usable(); !usable {
		return nil, errors.New("original packet must contain a completed certain run")
	}
	observed := retained.Artifact
	sourceSpec, err := testrunner.ReadSpec(local.Sources[1].Path)
	if err != nil || !sameAssertionContract(&sourceSpec, observed.Spec) || sourceSpec.Setup.InitialState != observed.Spec.Setup.InitialState {
		return nil, errors.New("actual original execution changed the reviewed failure/pass criteria")
	}
	if !phaseMatches(observed, request.Phase, review.RequiredFailures) {
		return nil, errors.New("actual original execution does not meet the selected failure/pass criteria")
	}
	approvedSpec, err := testrunner.ReadSpec(filepath.Join(request.ReviewPath, "spec.json"))
	if err != nil {
		return nil, err
	}
	prepared, err := durablerun.Prepare(request.SpecPath)
	if err != nil {
		return nil, err
	}
	inputs := prepared.PinnedInputs()
	spec, err := testrunner.DecodeSpec(inputs.Spec)
	if err != nil || inputs.SourceIdentity != review.DerivedIdentity || !sameAssertionContract(&approvedSpec, &spec) || spec.Setup.InitialState != approvedSpec.Setup.InitialState {
		return nil, errors.New("reexecution must preserve the reviewed derived case, message order, initial state and exact assertions")
	}
	// Configuration equality is necessary, not sufficient: it identifies neither
	// receiver software nor a reset. Those remain explicitly unproven.
	if observed.Result.Target == nil || !reflect.DeepEqual(*observed.Result.Target, inputs.Target) {
		return nil, errors.New("reexecution target configuration differs from the selected original execution")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &Reexecution{request: request, prepared: prepared, protected: append(protectedPaths(local), request.ReviewPath, request.LocalState, request.OriginalPacket, request.SpecPath), binding: ReexecutionAssessment{
		Schema: "readmit-reexecution-assessment/v1", ReviewIdentity: review.Identity,
		OriginalPacketIdentity: original.Identity, OriginalResultIdentity: observed.Identity,
		DerivedCaseIdentity: review.DerivedIdentity, ExecutionSpecIdentity: digest(inputs.Spec), Phase: request.Phase,
		Criteria: "not-executed", ExternalEquivalence: "declined", Disclosure: "customer-local-only-new-review-required",
		Reason: "No reexecution yet. Review approval does not authorize disclosure of new execution evidence.",
	}}, nil
}

// Execute performs one authorized send, never setup, reset, retries or resume.
// The durable job preserves cancellation and uncertain delivery for read-only recovery.
func (p *Reexecution) Execute(ctx context.Context, output string) (ReexecutionAssessment, error) {
	fresh, err := PrepareReexecution(ctx, p.request)
	if err != nil {
		return p.binding, err
	}
	if fresh.binding != p.binding {
		return p.binding, errors.New("reexecution inputs changed after preview; prepare and authorize again")
	}
	// Validate all protected inputs before any directory is created or byte sent.
	if _, err := destination(output, p.protected); err != nil {
		return p.binding, err
	}
	summary, err := p.prepared.Start(ctx, output)
	result := p.binding
	result.Criteria = "unavailable-or-unstable"
	result.Reason = "Incomplete, cancelled or uncertain execution cannot establish equivalence; reconcile delivery and reset before a separately authorized new attempt."
	if err != nil {
		return result, err
	}
	result.ResultIdentity = summary.ResultIdentity
	if summary.JournalIncomplete || summary.DeliveryUncertain || (summary.State != durablerun.Passed && summary.State != durablerun.AssertionFailed) {
		return result, nil
	}
	retained, err := runresult.Open(output)
	if err != nil {
		return result, err
	}
	if retained.Artifact == nil {
		return result, errors.New("reexecution retains no finalized result")
	}
	if usable, _ := retained.Usable(); !usable {
		return result, nil
	}
	artifact := retained.Artifact
	rechecked, err := PrepareReexecution(ctx, p.request)
	if err != nil || rechecked.binding != p.binding {
		return result, errors.New("review changed during execution; fresh review required")
	}
	if artifact.Result.SpecIdentity != result.ExecutionSpecIdentity || artifact.Result.InputBundleIdentity != result.DerivedCaseIdentity {
		return result, errors.New("retained reexecution differs from approved inputs")
	}
	review, err := OpenReview(p.request.ReviewPath)
	if err != nil {
		return result, err
	}
	if phaseMatches(artifact, p.request.Phase, review.RequiredFailures) {
		result.Criteria = "matched"
		result.Reason = "Selected criteria matched one actual retained reexecution. Target software, reset equivalence and repeat stability are unverified; fixture ledger or ACK-only evidence cannot establish external workflow equivalence."
	} else {
		result.Criteria = "changed"
		result.Reason = "Reexecution changed the selected failure set or full pass criteria; external equivalence is declined."
	}
	return result, nil
}

func phaseMatches(a *testrunner.Artifact, phase string, failures []int) bool {
	if phase == "pass" {
		return a.Result.Status == testrunner.Pass
	}
	return a.Result.Status == testrunner.AssertionFailure && slices.Equal(failedAssertions(a), failures)
}
