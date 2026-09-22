package desktop

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/bharm16/readmit/internal/engine"
	"github.com/bharm16/readmit/internal/redact"
	"github.com/bharm16/readmit/internal/sharing"
)

// The privacy screens. This file connects the preparation → materialization →
// review → approval → export journey to the operations the command line runs —
// `redact`, `redact export` and `share` — and adds no engine of its own: a
// derived review is the existing redaction operation's fail-closed output, an
// exported packet is the existing export operation's freshly generated one, and
// a support summary is the existing share operation's value-free preview made
// into a directory only by an approval that names it exactly.
//
// Three rules hold across every method here. The private local-state directory
// that holds source linkage, surrogate mappings, date offsets and residual
// values is written where the operator named it and is never read back by this
// package — only the redaction and sharing operations themselves touch it, and
// only to bind it. An approval is a fresh explicit act over the exact identity
// the bytes on disk have now, checked by the same gates the command line uses;
// nothing here records one, restores one, or lets a draft stand in for one. And
// nothing is ever uploaded: every operation is local, an export is a directory
// written beside the evidence, and a declining is stated rather than implied —
// no external regression-equivalence claim exists to preserve, so every result
// carries this release's explicit refusal of one.

// privacyOperation is the operation name the privacy panels cancel through.
const privacyOperation = "privacy"

// supportOperation is the operation name the support panel cancels through.
const supportOperation = "support"

// privacyDocumentLimit and sharingPolicyLimit mirror the bounds the redaction
// and sharing operations apply to their own inputs, so a request is refused
// with a sentence about the entry before the operation is started.
const (
	privacyDocumentLimit = 1 << 20
	sharingPolicyLimit   = 4096
)

// DeclinedEquivalence is what every prepared disclosure states about external
// regression equivalence. This release declines the claim rather than making
// it: fixture proof never substitutes for evidence from the actual target.
const DeclinedEquivalence = "declined"

// privacyBoundaries is what the privacy screen states about the journey before
// anybody acts on one. Every sentence is a fact about this build.
var privacyBoundaries = []string{
	"A derived review is disclosure review of one prepared extract; it is never a regression-equivalent packet, a certification or a Safe Harbor determination.",
	"The private local-state directory keeps source linkage, surrogate mappings, date offsets and known residual values customer-local. The window never opens it; deliberate original-versus-derived inspection is the inspector over each case.",
	"Approval names the exact materialized identities and is a fresh explicit act: any edit, changed source or stale approval requires renewed review, and no draft or typed label can restore one.",
	"Exporting a file writes it beside the evidence; it is not an upload, and no operation here transmits anything or places a secret value in the window.",
}

// PrivacyReviewRequest names the four inputs `readmit redact` derives a review
// from — the case, the original specification, the explicit disclosure policy
// and the complete original-artifact inventory — each one entry of the open
// workspace, plus the fresh review and private local-state entries it writes.
// Empty names propose the next free generated name.
type PrivacyReviewRequest struct {
	Workspace  string `json:"workspace"`
	Case       string `json:"case"`
	Spec       string `json:"spec"`
	Policy     string `json:"policy"`
	Inventory  string `json:"inventory"`
	Output     string `json:"output,omitzero"`
	LocalState string `json:"local_state,omitzero"`
}

// PrivacyReviewOutcome is what one derivation produced. State is the review's
// own word: blocked while any surface is left unresolved — the normal first
// answer, and the explicit blocker list a reviewer works down — and
// ready-for-approval once the derived case and specification verified.
type PrivacyReviewOutcome struct {
	Review      string   `json:"review"`
	Private     string   `json:"private"`
	State       string   `json:"state"`
	Identity    string   `json:"identity,omitzero"`
	Findings    int      `json:"findings"`
	Unresolved  int      `json:"unresolved"`
	Establishes string   `json:"establishes,omitzero"`
	Limitations []string `json:"limitations"`
}

// PrivacyReviewResult carries one state. Outcome is present whenever the
// redaction operation returned, including a blocked review: the blockers are
// the answer, and they are read back through the same review reader as ever.
type PrivacyReviewResult struct {
	State   State                 `json:"state"`
	Reason  string                `json:"reason,omitzero"`
	Outcome *PrivacyReviewOutcome `json:"outcome,omitzero"`
}

func (r *PrivacyReviewResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// DeriveExportReview runs the existing redaction operation over the selected
// entries and writes the review and its separate private local-state directory
// as two new workspace entries. It is the command line's own fail-closed gate:
// no derived case exists while any surface is unresolved, and the private
// directory stays exactly where the request named it. It contacts nothing; the
// original proof it runs privately speaks only to fresh built-in fixture
// receivers on the loopback, and never to a configured endpoint.
func (a *App) DeriveExportReview(request PrivacyReviewRequest) PrivacyReviewResult {
	return runNamed[PrivacyReviewResult, *PrivacyReviewResult](a, privacyOperation, true, true, func(ctx context.Context) PrivacyReviewResult {
		root, declined := resolveFolder(request.Workspace)
		if root == "" {
			return PrivacyReviewResult{State: declined.state, Reason: declined.reason}
		}
		casePath, specPath, policyPath, inventoryPath, reason, ok := privacyInputs(root, request)
		if !ok {
			return PrivacyReviewResult{State: Failed, Reason: reason}
		}
		review, refused := destinationFor(root, request.Output, "review")
		if refused.state != "" {
			return PrivacyReviewResult{State: refused.state, Reason: refused.reason}
		}
		private, refused := destinationFor(root, request.LocalState, "review-private")
		if refused.state != "" {
			return PrivacyReviewResult{State: refused.state, Reason: refused.reason}
		}
		if review.Name == private.Name {
			return PrivacyReviewResult{State: Failed, Reason: "the review and its private local state must be separate directories"}
		}
		created, err := redact.Create(ctx, redact.Request{
			CasePath:      casePath,
			SpecPath:      specPath,
			PolicyPath:    policyPath,
			InventoryPath: inventoryPath,
			Output:        filepath.Join(root, review.Name),
			LocalState:    filepath.Join(root, private.Name),
		})
		if err != nil {
			state, reason := privacyRefusal(err)
			return PrivacyReviewResult{State: state, Reason: reason}
		}
		return PrivacyReviewResult{State: Completed, Outcome: privacyOutcome(review.Name, private.Name, created)}
	})
}

// privacyInputs resolves the four documents one derivation reads, each one
// entry of the open workspace: the case as one entry, the specification, the
// policy and the inventory as bounded regular files. Every resolution is the
// one a review read applies, so a request that cannot name its inputs is
// refused before the operation writes anything at all.
func privacyInputs(root string, request PrivacyReviewRequest) (casePath, specPath, policyPath, inventoryPath, reason string, ok bool) {
	if request.Case == "" || request.Spec == "" || request.Policy == "" || request.Inventory == "" {
		return "", "", "", "", "select the case, the original specification, the disclosure policy and the original-artifact inventory", false
	}
	casePath, err := runEntryPath(root, request.Case)
	if err != nil {
		return "", "", "", "", "the case must be one entry of the open workspace", false
	}
	var declined refusal
	specPath, declined = boundedEntryPath(root, request.Spec, privacyDocumentLimit, "the original specification")
	if declined.state != "" {
		return "", "", "", "", declined.reason, false
	}
	policyPath, declined = boundedEntryPath(root, request.Policy, privacyDocumentLimit, "the disclosure policy")
	if declined.state != "" {
		return "", "", "", "", declined.reason, false
	}
	inventoryPath, declined = boundedEntryPath(root, request.Inventory, privacyDocumentLimit, "the original-artifact inventory")
	if declined.state != "" {
		return "", "", "", "", declined.reason, false
	}
	return casePath, specPath, policyPath, inventoryPath, "", true
}

// boundedEntryPath validates one regular workspace entry through the same read
// a review read applies and returns the path the operation opens it from.
func boundedEntryPath(root, name string, limit int, what string) (string, refusal) {
	if _, declined := workspaceDocument(root, name, limit, what); declined.state != "" {
		return "", declined
	}
	path, err := runEntryPath(root, name)
	if err != nil {
		return "", refusal{Failed, what + " must be named by one entry of the open workspace"}
	}
	return path, refusal{}
}

// privacyOutcome projects one completed derivation for the shell, with the
// review's own counts and the statements a person needs before approving.
func privacyOutcome(review, private string, created *redact.Review) *PrivacyReviewOutcome {
	outcome := &PrivacyReviewOutcome{
		Review:      review,
		Private:     private,
		State:       created.State,
		Identity:    created.Identity,
		Findings:    len(created.Findings),
		Limitations: privacyBoundaries,
	}
	for _, finding := range created.Findings {
		if !finding.Resolved {
			outcome.Unresolved++
		}
	}
	if created.State == readyForApproval {
		outcome.Establishes = DisclosureReviewed
	}
	return outcome
}

// PrivacyExportRequest names one review of the open workspace, the private
// local-state entry its derivation wrote, the exact review identity the
// reviewer is approving, and the fresh packet entry the export generates. The
// approval is the same explicit act `readmit redact export --approve` is: it is
// checked against the identity the bytes on disk have now, and it is never
// recorded or restored by this window.
type PrivacyExportRequest struct {
	Workspace  string `json:"workspace"`
	Review     string `json:"review"`
	LocalState string `json:"local_state"`
	Approval   string `json:"approval"`
	Output     string `json:"output,omitzero"`
}

// PrivacyExportOutcome is one generated packet: the fresh fixture proof the
// export ran, the disclosure-reviewed extract the packet establishes, and this
// release's explicit decline of any external regression-equivalence claim.
type PrivacyExportOutcome struct {
	Packet           string   `json:"packet"`
	Identity         string   `json:"identity"`
	ApprovedReview   string   `json:"approved_review"`
	Files            int      `json:"files"`
	ProofBaseline    string   `json:"proof_baseline"`
	ProofPostfix     string   `json:"proof_postfix"`
	FailedAssertions []int    `json:"failed_assertions"`
	Establishes      string   `json:"establishes"`
	Equivalence      string   `json:"external_equivalence"`
	Limitations      []string `json:"limitations"`
}

// PrivacyExportResult carries one state. Outcome is present only when every
// gate passed and the generated packet verified; a refused approval or a failed
// proof leaves nothing behind but the private attempt the operation retains.
type PrivacyExportResult struct {
	State   State                 `json:"state"`
	Reason  string                `json:"reason,omitzero"`
	Outcome *PrivacyExportOutcome `json:"outcome,omitzero"`
}

func (r *PrivacyExportResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ExportDerivedPacket runs the existing export operation: it re-verifies the
// review, re-checks the private binding, reruns the derived specification
// against fresh built-in fixtures, and writes the freshly generated packet only
// after every gate passes. The command line exports free of operation
// admission, so this writes under none either. Nothing is transmitted; the only
// addresses the operation ever speaks to are the loopback fixture receivers it
// starts itself, and their configuration is inside the packet it writes.
func (a *App) ExportDerivedPacket(request PrivacyExportRequest) PrivacyExportResult {
	return runNamed[PrivacyExportResult, *PrivacyExportResult](a, privacyOperation, true, false, func(ctx context.Context) PrivacyExportResult {
		root, declined := resolveFolder(request.Workspace)
		if root == "" {
			return PrivacyExportResult{State: declined.state, Reason: declined.reason}
		}
		reviewPath, err := runEntryPath(root, request.Review)
		if err != nil {
			return PrivacyExportResult{State: Failed, Reason: "the export review must be one entry of the open workspace"}
		}
		privatePath, err := runEntryPath(root, request.LocalState)
		if err != nil {
			return PrivacyExportResult{State: Failed, Reason: "the private local state must be one entry of the open workspace"}
		}
		if request.Approval == "" {
			return PrivacyExportResult{State: Failed, Reason: "approve the exact review identity to export it; an unapproved review exports nothing"}
		}
		destination, refused := destinationFor(root, request.Output, "export")
		if refused.state != "" {
			return PrivacyExportResult{State: refused.state, Reason: refused.reason}
		}
		manifest, err := redact.Export(ctx, redact.ExportRequest{
			ReviewPath: reviewPath,
			LocalState: privatePath,
			Approval:   request.Approval,
			Output:     filepath.Join(root, destination.Name),
		})
		if err != nil {
			state, reason := privacyRefusal(err)
			return PrivacyExportResult{State: state, Reason: reason}
		}
		return PrivacyExportResult{State: Completed, Outcome: &PrivacyExportOutcome{
			Packet:           destination.Name,
			Identity:         sealedIdentity(filepath.Join(root, destination.Name)),
			ApprovedReview:   manifest.ApprovedReview,
			Files:            len(manifest.Files),
			ProofBaseline:    string(manifest.Proof.BaselineStatus),
			ProofPostfix:     string(manifest.Proof.PostfixStatus),
			FailedAssertions: manifest.Proof.FailedAssertions,
			Establishes:      DisclosureReviewed,
			Equivalence:      DeclinedEquivalence,
			Limitations:      privacyBoundaries,
		}}
	})
}

// sealedIdentity reads a generated packet's root completion marker back from
// disk, so what the window displays is the identity the sealed bytes record.
func sealedIdentity(packet string) string {
	raw, err := os.ReadFile(filepath.Join(packet, "identity.sha256"))
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(string(raw), "\n")
}

// privacyRefusal maps the redaction operations' own errors to one state and one
// sentence. Their diagnostics are operator-facing sentences that disclose no
// path and no value, so they carry unchanged; cancellation is named as itself.
func privacyRefusal(err error) (State, string) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return Cancelled, "the privacy operation was cancelled; any partial destination remains incomplete and cannot be verified as complete"
	}
	if errors.Is(err, engine.ErrUnsupportedVersion) {
		return Failed, "the evidence was evaluated by a version this release cannot read; it has not been changed"
	}
	return Failed, err.Error()
}

// SupportPolicyRequest authors one sharing policy into a new workspace entry.
// The closed destination vocabulary, the support switch and the byte bound are
// the sharing contract's own members; nothing else is writable about one.
type SupportPolicyRequest struct {
	Workspace    string   `json:"workspace"`
	Output       string   `json:"output"`
	Support      bool     `json:"support"`
	Destinations []string `json:"destinations"`
	MaxBytes     int      `json:"max_bytes"`
}

// SupportPolicy is one sharing policy as stored and as the share operation will
// apply it. A policy that denies support denies preparation: the preview is
// the refusal.
type SupportPolicy struct {
	Entry        string   `json:"entry"`
	Schema       string   `json:"schema"`
	Support      bool     `json:"support"`
	Destinations []string `json:"destinations"`
	MaxBytes     int      `json:"max_bytes"`
}

// SupportPolicyResult carries one state.
type SupportPolicyResult struct {
	State  State          `json:"state"`
	Reason string         `json:"reason,omitzero"`
	Policy *SupportPolicy `json:"policy,omitzero"`
}

func (r *SupportPolicyResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// SaveSharingPolicy writes one sharing policy through the sharing contract's
// own decoder, so a policy the operation would refuse is never written. The
// bytes are canonical, so the policy identity a preview binds is the identity
// of exactly what is on disk.
func (a *App) SaveSharingPolicy(request SupportPolicyRequest) SupportPolicyResult {
	return run(a, false, true, func(context.Context) SupportPolicyResult {
		root, declined := resolveFolder(request.Workspace)
		if root == "" {
			return SupportPolicyResult{State: declined.state, Reason: declined.reason}
		}
		policy := sharing.Policy{
			Schema:       sharing.PolicySchema,
			Support:      request.Support,
			Destinations: append([]string{}, request.Destinations...),
			MaxBytes:     request.MaxBytes,
		}
		encoded, err := json.Marshal(policy, json.Deterministic(true))
		if err != nil {
			return SupportPolicyResult{State: Failed, Reason: "the sharing policy could not be canonicalized"}
		}
		if _, err := sharing.DecodePolicy(encoded); err != nil {
			return SupportPolicyResult{State: Failed, Reason: sharingRefusalReason(request.Support, request.Destinations, request.MaxBytes)}
		}
		// The decoder reads the exact file bytes, so the stored policy carries
		// the trailing newline the way every canonical document here does.
		encoded = append(encoded, '\n')
		if _, err := sharing.DecodePolicy(encoded); err != nil {
			return SupportPolicyResult{State: Failed, Reason: "the sharing policy could not be written as canonical bytes"}
		}
		if err := writeWorkspaceEntry(root, request.Output, encoded); err != nil {
			if errors.Is(err, fs.ErrPermission) {
				return SupportPolicyResult{State: PermissionDenied, Reason: "this account cannot write into the open workspace"}
			}
			return SupportPolicyResult{State: Failed, Reason: err.Error()}
		}
		return SupportPolicyResult{State: Completed, Policy: &SupportPolicy{
			Entry: request.Output, Schema: policy.Schema, Support: policy.Support,
			Destinations: policy.Destinations, MaxBytes: policy.MaxBytes,
		}}
	})
}

// sharingRefusalReason names the one thing a refused policy request got wrong,
// in the order the decoder checks them, so a fix is a one-member change.
func sharingRefusalReason(support bool, destinations []string, maxBytes int) string {
	switch {
	case len(destinations) == 0 || len(destinations) > 2:
		return "a sharing policy names one or both supported destinations: local-file, customer-hub-download"
	case maxBytes < 1 || maxBytes > 65536:
		return "a sharing policy's byte bound must be between 1 and 65536"
	}
	for _, destination := range destinations {
		if destination != "local-file" && destination != "customer-hub-download" {
			return "a sharing policy's destinations admit only local-file and customer-hub-download"
		}
	}
	if !support {
		return "a policy that denies support denies preparation; the share preview is the refusal"
	}
	return "the sharing policy was refused by its own decoder"
}

// ReadSharingPolicy reads one sharing policy of the open workspace through the
// sharing contract's own decoder. It writes nothing.
func (a *App) ReadSharingPolicy(workspace, entry string) SupportPolicyResult {
	return run(a, false, false, func(context.Context) SupportPolicyResult {
		root, declined := resolveFolder(workspace)
		if root == "" {
			return SupportPolicyResult{State: declined.state, Reason: declined.reason}
		}
		data, declined := workspaceDocument(root, entry, sharingPolicyLimit, "the sharing policy")
		if declined.state != "" {
			return SupportPolicyResult{State: declined.state, Reason: declined.reason}
		}
		policy, err := sharing.DecodePolicy(data)
		if err != nil {
			return SupportPolicyResult{State: Failed, Reason: "that entry is not a sharing policy this release prepares with"}
		}
		return SupportPolicyResult{State: Completed, Policy: &SupportPolicy{
			Entry: entry, Schema: policy.Schema, Support: policy.Support,
			Destinations: policy.Destinations, MaxBytes: policy.MaxBytes,
		}}
	})
}

// SupportRequest names one value-free support summary to prepare: the source
// packet, portable review or complete derived review, the sharing policy that
// decides preparation, and — for a derived review only — the private
// local-state entry its derivation wrote, which the operation binds and never
// copies. Every member is one entry of the open workspace.
type SupportRequest struct {
	Workspace string `json:"workspace"`
	Source    string `json:"source"`
	Kind      string `json:"kind"`
	Private   string `json:"private,omitzero"`
	Policy    string `json:"policy"`
}

// SupportSummary is the prepared summary itself. Every member is a commitment,
// a closed outcome or fixed scope text: no free-form name, error, path,
// credential or message field exists to fill, which is why this view can show
// all of it — the preview is every byte the published bundle will hold.
type SupportSummary struct {
	SourceKind          string `json:"source_kind"`
	SourceIdentity      string `json:"source_identity"`
	InputCommitment     string `json:"input_commitment"`
	SpecIdentity        string `json:"spec_identity"`
	PolicyIdentity      string `json:"policy_identity"`
	Outcome             string `json:"outcome"`
	ExternalEquivalence string `json:"external_equivalence"`
	Scope               string `json:"scope"`
	Identity            string `json:"identity"`
	MaxBytes            int    `json:"max_bytes"`
	WithinPolicy        bool   `json:"within_policy"`
}

// SupportPreviewResult carries one state. Summary is present whenever the share
// operation prepared a candidate, so an exclusion — a policy that denies
// support, an unlisted source type, a source that does not verify — is a
// refusal with its own sentence rather than a partial success.
type SupportPreviewResult struct {
	State   State           `json:"state"`
	Reason  string          `json:"reason,omitzero"`
	Summary *SupportSummary `json:"summary,omitzero"`
}

func (r *SupportPreviewResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// supportExclusions is what a support summary never carries, stated rather
// than implied. The words are the share operation's own boundary.
var supportExclusions = []string{
	"No evidence payload: messages, source names, notes, specification text, run values, original reports, caches and logs are excluded.",
	"Arbitrary files and directories are refused rather than collected recursively.",
	"Byte commitments can still link related artifacts; review that metadata before sharing.",
}

// PreviewSupportSummary prepares the value-free support summary through the
// existing share operation and shows every byte it would publish, without
// writing anything. The sources and policy are read again by the publish that
// follows, so a changed input invalidates the preview through its identity.
func (a *App) PreviewSupportSummary(request SupportRequest) SupportPreviewResult {
	return run(a, false, false, func(ctx context.Context) SupportPreviewResult {
		root, declined := resolveFolder(request.Workspace)
		if root == "" {
			return SupportPreviewResult{State: declined.state, Reason: declined.reason}
		}
		candidate, policy, reason, ok := a.supportCandidate(root, request, ctx)
		if !ok {
			return SupportPreviewResult{State: Failed, Reason: reason}
		}
		return SupportPreviewResult{State: Completed, Summary: supportSummaryView(candidate, policy)}
	})
}

// supportCandidate resolves the request's entries and prepares one candidate
// through the share operation. The refusals here name the entry that was wrong
// so a fix is a selection; the operation's own refusals stay the operation's.
func (a *App) supportCandidate(root string, request SupportRequest, ctx context.Context) (*sharing.Candidate, sharing.Policy, string, bool) {
	var zero sharing.Policy
	sourcePath, err := runEntryPath(root, request.Source)
	if err != nil {
		return nil, zero, "the source must be one entry of the open workspace", false
	}
	if request.Kind == "derived-review" && request.Private == "" {
		return nil, zero, "a derived review's summary needs the private local-state entry its derivation wrote; it is bound, never copied", false
	}
	privatePath := ""
	if request.Private != "" {
		if privatePath, err = runEntryPath(root, request.Private); err != nil {
			return nil, zero, "the private local state must be one entry of the open workspace", false
		}
	}
	policyData, declined := workspaceDocument(root, request.Policy, sharingPolicyLimit, "the sharing policy")
	if declined.state != "" {
		return nil, zero, declined.reason, false
	}
	policy, err := sharing.DecodePolicy(policyData)
	if err != nil {
		return nil, zero, "that entry is not a sharing policy this release prepares with", false
	}
	candidate, err := sharing.Prepare(ctx, sharing.Request{
		Source:  sourcePath,
		Kind:    request.Kind,
		Private: privatePath,
		Policy:  filepath.Join(root, request.Policy),
	})
	if err != nil {
		if ctx.Err() != nil {
			return nil, zero, cancelledRefusal.reason, false
		}
		return nil, zero, "the share operation refused this preparation: the source kind, the policy's support decision, the destination bound or a source that does not verify", false
	}
	return candidate, policy, "", true
}

// supportSummaryView projects one prepared candidate, with the identity the
// publish will require and the policy bound the summary sits within. The
// summary's own members are read back from its canonical bytes, so what the
// window draws is exactly what the bundle will hold.
func supportSummaryView(candidate *sharing.Candidate, policy sharing.Policy) *SupportSummary {
	var summary sharing.Summary
	json.Unmarshal(candidate.Bytes(), &summary, json.RejectUnknownMembers(true))
	return &SupportSummary{
		SourceKind:          summary.SourceKind,
		SourceIdentity:      summary.SourceIdentity,
		InputCommitment:     summary.InputCommitment,
		SpecIdentity:        summary.SpecIdentity,
		PolicyIdentity:      summary.PolicyIdentity,
		Outcome:             summary.Outcome,
		ExternalEquivalence: summary.ExternalEquivalence,
		Scope:               summary.Scope,
		Identity:            candidate.Identity(),
		MaxBytes:            policy.MaxBytes,
		WithinPolicy:        len(candidate.Bytes()) <= policy.MaxBytes,
	}
}

// SupportPublishRequest adds the two members publishing needs: the exact
// preview identity the reviewer is approving and the fresh local directory the
// bundle is written into. The approval is a fresh explicit act bound to the
// summary identity; it is never recorded here and never restored by anything.
type SupportPublishRequest struct {
	Workspace string `json:"workspace"`
	Source    string `json:"source"`
	Kind      string `json:"kind"`
	Private   string `json:"private,omitzero"`
	Policy    string `json:"policy"`
	Approval  string `json:"approval"`
	Output    string `json:"output,omitzero"`
}

// SupportPublishOutcome is one published local bundle: the directory, the
// summary identity inside it, the closed file set, and the statements about
// what the bundle never carried and what publishing never did.
type SupportPublishOutcome struct {
	Bundle      string   `json:"bundle"`
	Identity    string   `json:"identity"`
	Files       []string `json:"files"`
	Exclusions  []string `json:"exclusions"`
	NoUpload    string   `json:"no_upload"`
	Limitations []string `json:"limitations"`
}

// SupportPublishResult carries one state. Outcome is present only when the
// completion marker was written; an interrupted or refused publication leaves a
// directory with no marker, which every reader refuses.
type SupportPublishResult struct {
	State   State                  `json:"state"`
	Reason  string                 `json:"reason,omitzero"`
	Outcome *SupportPublishOutcome `json:"outcome,omitzero"`
}

func (r *SupportPublishResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// PublishSupportSummary regenerates the summary from the current sources and
// policy through the existing share operation and writes the reviewed bundle
// only under an approval naming the exact identity the regeneration produced.
// A stale approval is a refusal, never a warning. The local directory is the
// only thing written; there is no automatic upload path of any kind.
func (a *App) PublishSupportSummary(request SupportPublishRequest) SupportPublishResult {
	return runNamed[SupportPublishResult, *SupportPublishResult](a, supportOperation, true, false, func(ctx context.Context) SupportPublishResult {
		root, declined := resolveFolder(request.Workspace)
		if root == "" {
			return SupportPublishResult{State: declined.state, Reason: declined.reason}
		}
		candidate, _, reason, ok := a.supportCandidate(root, SupportRequest{
			Workspace: request.Workspace, Source: request.Source, Kind: request.Kind,
			Private: request.Private, Policy: request.Policy,
		}, ctx)
		if !ok {
			return SupportPublishResult{State: Failed, Reason: reason}
		}
		if ctx.Err() != nil {
			return SupportPublishResult{State: Cancelled, Reason: cancelledRefusal.reason}
		}
		if request.Approval == "" {
			return SupportPublishResult{State: Failed, Reason: "approve the exact preview identity to publish it; an unreviewed summary publishes nothing"}
		}
		if candidate.Identity() != request.Approval {
			return SupportPublishResult{State: Failed, Reason: "this approval does not name the summary the current sources and policy produce; review the current preview again and approve the identity it displays"}
		}
		// The destination is the native local folder a person chose, or one new
		// generated entry of the open workspace. Either way the share operation
		// reserves it: a destination inside the source or the private linkage
		// is refused there, whatever was displayed.
		destination := filepath.Base(request.Output)
		output := filepath.Join(root, destination)
		if filepath.IsAbs(request.Output) {
			destination, output = filepath.Base(request.Output), request.Output
		} else {
			chosen, refused := destinationFor(root, request.Output, "support")
			if refused.state != "" {
				return SupportPublishResult{State: refused.state, Reason: refused.reason}
			}
			destination, output = chosen.Name, filepath.Join(root, chosen.Name)
		}
		if err := candidate.Publish(ctx, request.Approval, output); err != nil {
			if ctx.Err() != nil {
				return SupportPublishResult{State: Cancelled, Reason: "the publication was cancelled; any partial directory has no completion marker and is never resumed"}
			}
			return SupportPublishResult{State: Failed, Reason: "the publication was refused; an incomplete directory has no completion marker and recovery is a new destination with a fresh review"}
		}
		return SupportPublishResult{State: Completed, Outcome: &SupportPublishOutcome{
			Bundle:      destination,
			Identity:    request.Approval,
			Files:       []string{"support.json", "event.json", "identity.sha256"},
			Exclusions:  supportExclusions,
			NoUpload:    "Publishing this bundle wrote a local directory. Exporting a file is not uploading it: nothing here transmits, and the customer hub's authenticated review workflow is a separate deliberate connection.",
			Limitations: privacyBoundaries,
		}}
	})
}

// ChooseSupportExportPath presents the host's native folder dialog for the new
// folder a support bundle is published into. The choice is a destination only;
// choosing it publishes nothing and contacts nothing.
func (a *App) ChooseSupportExportPath() PacketPathResult {
	return run(a, true, false, func(ctx context.Context) PacketPathResult {
		folder, declined := a.chooseFolder(ctx, "Choose a new folder for the reviewed support bundle")
		if folder == "" {
			return PacketPathResult{State: declined.state, Reason: declined.reason}
		}
		return PacketPathResult{State: Completed, Path: folder}
	})
}

// VerifySupportBundle verifies one support bundle of the open workspace
// offline, through the same reader `readmit share verify` runs, independently
// of its source. A missing, extra, changed or malformed member is refused;
// a verified identity means bundle integrity and nothing else.
func (a *App) VerifySupportBundle(workspace, entry string) SupportPreviewResult {
	return run(a, false, false, func(context.Context) SupportPreviewResult {
		root, declined := resolveFolder(workspace)
		if root == "" {
			return SupportPreviewResult{State: declined.state, Reason: declined.reason}
		}
		path, err := runEntryPath(root, entry)
		if err != nil {
			return SupportPreviewResult{State: Failed, Reason: "a support bundle must be named by one entry of the open workspace"}
		}
		summary, err := sharing.Open(path)
		if err != nil {
			return SupportPreviewResult{State: Failed, Reason: "this directory is not a complete support bundle this release verifies; a bundle missing, holding or hiding anything beyond its three members is refused"}
		}
		encoded, err := json.Marshal(summary, json.Deterministic(true))
		if err != nil {
			return SupportPreviewResult{State: Failed, Reason: "the verified summary could not be read back"}
		}
		return SupportPreviewResult{State: Completed, Summary: &SupportSummary{
			SourceKind: summary.SourceKind, SourceIdentity: summary.SourceIdentity,
			InputCommitment: summary.InputCommitment, SpecIdentity: summary.SpecIdentity,
			PolicyIdentity: summary.PolicyIdentity, Outcome: summary.Outcome,
			ExternalEquivalence: summary.ExternalEquivalence, Scope: summary.Scope,
			Identity: sharing.Digest(encoded),
		}}
	})
}
