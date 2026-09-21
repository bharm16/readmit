package desktop

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"strconv"
	"strings"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/correlate"
	"github.com/bharm16/readmit/internal/exportreview"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/profilepack"
	"github.com/bharm16/readmit/internal/redact"
	"github.com/bharm16/readmit/internal/transform"
)

// MaxReviewFindings bounds one window of a review's located findings. A review
// holds up to the review reader's own fifty thousand of them, and a window of
// that list is rendered; the whole list is never handed to the interface.
const MaxReviewFindings = 200

// Decision is the reviewer's explicit decision over one export review, as its
// own named outcome. The set is closed, and neither refusal is folded into the
// other: a review nobody has decided yet, an incomplete review that cannot
// authorize disclosure whatever identity is named, an approval naming bytes
// that are not the ones on disk now, and an approval of exactly these bytes are
// four different answers.
type Decision string

const (
	// NotDecided: no approval was offered, so the reviewer has decided nothing.
	NotDecided Decision = "not-decided"
	// IncompleteReview: the review itself is blocked — it holds unresolved
	// findings, a residual hit, or no derived material at all. An incomplete
	// review cannot authorize disclosure, so the exact identity does not
	// approve it either.
	IncompleteReview Decision = "incomplete-review"
	// StaleApproval: the approval names a different review than the bytes this
	// read verified. Every input, policy, specification and output version is
	// bound into the review identity, so changing any of them leaves the old
	// approval naming a review that no longer exists.
	StaleApproval Decision = "stale-approval"
	// Approved: the reviewer approved exactly the identity this read computed
	// from the bytes that are there now.
	Approved Decision = "approved"
)

// DisclosureReviewed is what a ready review establishes, named rather than
// implied. It is a disclosure review of the bytes that would leave, and it is
// never a claim that the extract re-executes like the incident it came from.
const DisclosureReviewed = "disclosure-reviewed-extract"

// reviewBoundary is what this window states about a review before anybody acts
// on one. Every sentence is a fact about this build.
const reviewBoundary = "A review states what the declared policy did to the bytes of a derived extract. " +
	"A disclosure-reviewed extract is not a regression-equivalent packet: equivalence needs evidence from the " +
	"actual external target, and fixture proof never substitutes for it. Nothing here is uploaded, no private " +
	"source mapping or original value is read, and this window records no approval, performs no export and " +
	"claims no certification, Safe Harbor status or authentication of the source evidence."

// transformBoundary is what this window states about a transformation preview.
const transformBoundary = "A preview states what this plan would do to the sequence a replay sends. It writes " +
	"nothing into evidence and produces no case, no run and no derived bundle, so it is neither a disclosure " +
	"review nor an approval to share anything."

// ReviewSurface is one declared export surface and how much of it the policy
// settled. A surface is where the content is and what kind of content it is,
// both in the reporting engine's own words: Name is the first element of the
// location it wrote and Content its own word for what was found there, so the
// source filenames of a case never collapse into its messages and the run
// values of a retained artifact never collapse into its report text.
//
// Nothing is matched against a list kept here, so a surface a later policy
// introduces is inventoried the moment it appears rather than dropped for not
// being recognized, and every finding belongs to exactly one: the counts always
// sum to the whole inventory.
type ReviewSurface struct {
	Name       string `json:"name"`
	Content    string `json:"content"`
	Findings   int    `json:"findings"`
	Unresolved int    `json:"unresolved"`
}

// ReviewFinding is one located item of the inventory.
//
// No value is here, transformed or original. Location is where the item is,
// Class the checklist category the policy labelled it with, Reason the engine's
// own word for what it is, Policy the named operator that handled it, and
// Resolved whether it was handled at all. Reading a transformed value is the
// inspector over the derived case the review names, deliberately, exactly as it
// is for a comparison or a reproducer; an original value is never here at all.
type ReviewFinding struct {
	Surface  string `json:"surface"`
	Location string `json:"location"`
	Class    string `json:"class"`
	Reason   string `json:"reason"`
	Policy   string `json:"policy,omitzero"`
	Resolved bool   `json:"resolved"`
}

// Review is one export review of the open workspace, as the window reads it.
//
// Identity is what an approval must name, and it is recomputed from the bytes
// that are there now rather than read out of the review: the input commitment,
// the private state commitment, the derived case identity and the derived
// specification digest are all inside it, so changing any bound artifact
// changes it. Establishes is what a ready review establishes, named; Decision
// is the reviewer's own, and Boundary and Scope are the statements a person
// needs before acting on either.
//
// This result is a typed value the interface reads, not a stored document:
// nothing here is written anywhere, and no approval is retained.
type Review struct {
	Name                 string `json:"name"`
	Report               string `json:"report"`
	State                string `json:"state"`
	DataOrigin           string `json:"data_origin"`
	Identity             string `json:"identity"`
	InputCommitment      string `json:"input_commitment"`
	LocalStateCommitment string `json:"local_state_commitment"`
	DerivedIdentity      string `json:"derived_case_identity,omitzero"`
	DerivedSpecSHA256    string `json:"derived_spec_sha256,omitzero"`

	Policies                 []string                `json:"policies_applied"`
	Surfaces                 []ReviewSurface         `json:"surfaces"`
	Coverage                 []exportreview.Coverage `json:"coverage"`
	Uncovered                []string                `json:"uncovered_classes"`
	Residual                 exportreview.Scan       `json:"residual_scan"`
	RequiredFailures         []int                   `json:"required_failures"`
	OriginalFailedAssertions []int                   `json:"original_failed_assertions"`

	Establishes    string   `json:"establishes,omitzero"`
	Decision       Decision `json:"decision"`
	DecisionReason string   `json:"decision_reason"`

	Unresolved int             `json:"unresolved"`
	Offset     int             `json:"offset"`
	Limit      int             `json:"limit"`
	Total      int             `json:"total"`
	Findings   []ReviewFinding `json:"findings"`

	Scope    string `json:"scope"`
	Boundary string `json:"boundary"`
}

// ReviewRequest names one export review of the open workspace and, optionally,
// the review identity the reviewer is approving. Approve is the reviewer's
// explicit decision and nothing else: it is checked against the identity this
// read computed and is never written anywhere.
type ReviewRequest struct {
	Workspace string `json:"workspace"`
	Review    string `json:"review"`
	Approve   string `json:"approve,omitzero"`
	Offset    int    `json:"offset"`
	Limit     int    `json:"limit"`
}

// ReviewResult carries one state. Review is present whenever the review was
// verified, including when the window holds no finding, because the counts and
// the decision are the answer in that case.
type ReviewResult struct {
	State  State   `json:"state"`
	Reason string  `json:"reason,omitzero"`
	Review *Review `json:"review,omitzero"`
}

func (r *ReviewResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

func (r refusal) review() ReviewResult { return ReviewResult{State: r.state, Reason: r.reason} }

// Transformation is one plan previewed over one verified case: the documents it
// was read under, and the preview itself. The preview is the engine's own and
// records no value byte, before or after — a change is a position, an operator,
// the state the position was in and the relation a rename assigned.
type Transformation struct {
	Case     string            `json:"case"`
	Rules    string            `json:"rules"`
	Plan     string            `json:"plan"`
	Profile  string            `json:"profile,omitzero"`
	Preview  transform.Preview `json:"preview"`
	Boundary string            `json:"boundary"`
}

// TransformRequest names the case and the three documents a preview is read
// under, each as one entry of the open workspace. Identity binds the request to
// the evidence the window verified, exactly as a comparison does. Profile is
// the pack the plan pinned, required when it pins one and refused when it does
// not.
type TransformRequest struct {
	Workspace string `json:"workspace"`
	Case      string `json:"case"`
	Identity  string `json:"identity"`
	Rules     string `json:"rules"`
	Plan      string `json:"plan"`
	Profile   string `json:"profile,omitzero"`
}

// TransformResult carries one state. Transformation is present whenever the
// plan was previewed, including when it declares no step.
type TransformResult struct {
	State          State           `json:"state"`
	Reason         string          `json:"reason,omitzero"`
	Transformation *Transformation `json:"transformation,omitzero"`
}

func (r *TransformResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

func (r refusal) transformation() TransformResult {
	return TransformResult{State: r.state, Reason: r.reason}
}

// OpenReview reads one export review of the open workspace and reports its
// inventory, its coverage and the reviewer's decision.
//
// It decides nothing about the review. The same verified offline reader
// `readmit redact export` gates on opens it, which recomputes the review
// identity from the bytes that are there now and checks the derived case and
// the derived specification against what the review committed to — so a review
// whose input, policy, specification or output changed is either refused here
// or reports an identity the old approval does not name. The private state
// directory is never named, opened or read: this window sees what a recipient
// would see, and nothing else.
//
// It reads one verified artifact under that reader's own limits and runs to
// completion once it starts, so it holds the operation slot but is not
// interruptible. Nothing is written, nothing is exported, and no approval is
// retained.
func (a *App) OpenReview(request ReviewRequest) ReviewResult {
	return run(a, false, false, func(context.Context) ReviewResult {
		return a.openReview(request)
	})
}

func (a *App) openReview(request ReviewRequest) ReviewResult {
	if request.Offset < 0 || request.Limit < 1 || request.Limit > MaxReviewFindings {
		return ReviewResult{State: Failed, Reason: "a review renders a window beginning at or after its first finding, of between 1 and " + strconv.Itoa(MaxReviewFindings) + " findings"}
	}
	root, declined := resolveFolder(request.Workspace)
	if root == "" {
		return declined.review()
	}
	path, err := artifactpath.Child(root, request.Review)
	if err != nil {
		return ReviewResult{State: Failed, Reason: "an export review must be named by one directory entry of the open workspace"}
	}
	opened, err := redact.OpenReview(path)
	if err != nil {
		// The reader's own diagnostic separates a review this release cannot
		// read from one whose derived material disagrees with what it committed
		// to, and it names no path and no value, so it is reported as it is.
		return ReviewResult{State: Failed, Reason: err.Error()}
	}
	surfaces, unresolved := inventoried(opened.Findings)
	return reviewed(request, opened, surfaces, unresolved)
}

// PreviewTransformation reports what one transformation plan would do to the
// sequence a replay sends, over the case the window verified.
//
// Nothing about what a step means is decided here. The same engine
// `readmit transform` runs reads the plan, applies the declared correlation
// rules through `readmit correlate`, and answers with the sequence, every
// position it would rewrite, what happened to every declared relation and
// everything it left alone — so the panel shows exactly what the command line
// previews over the same evidence.
//
// It writes nothing at all: no case, no run and no derived bundle. It reads
// verified evidence under the case reader's own limits and runs to completion
// once it starts, so it holds the operation slot but is not interruptible.
func (a *App) PreviewTransformation(request TransformRequest) TransformResult {
	return run(a, false, false, func(context.Context) TransformResult {
		return a.previewTransformation(request)
	})
}

func (a *App) previewTransformation(request TransformRequest) TransformResult {
	root, _, declined := openedCase(request.Workspace, request.Case, request.Identity)
	if root == "" {
		return declined.transformation()
	}
	casePath := artifactpath.JoinReference(root, request.Case)
	declared, declined := workspaceDocument(root, request.Rules, correlate.MaxRulesBytes,
		"the correlation rules document")
	if declined.state != "" {
		return declined.transformation()
	}
	authored, declined := workspaceDocument(root, request.Plan, transform.MaxPlanBytes,
		"the transformation plan")
	if declined.state != "" {
		return declined.transformation()
	}
	pack, declined := pinnedPack(root, request.Profile)
	if declined.state != "" {
		return declined.transformation()
	}
	preview, err := operation.PreviewTransform(operation.TransformRequest{Case: casePath, Rules: declared, Plan: authored, Pack: pack})
	if err != nil {
		return TransformResult{State: Failed, Reason: err.Error()}
	}
	previewed := &Transformation{
		Case: request.Case, Rules: request.Rules, Plan: request.Plan,
		Profile: request.Profile, Preview: preview, Boundary: transformBoundary,
	}
	if len(preview.Plan.Steps) == 0 {
		return TransformResult{State: Empty, Reason: "this plan declares no step, so the sequence is the case's own", Transformation: previewed}
	}
	return TransformResult{State: Completed, Transformation: previewed}
}

// pinnedPack reads the profile pack the reviewer selected, or none at all. A
// plan that pins none is previewed against none, and the preview itself refuses
// a pack the plan did not pin rather than validating against whatever was
// named here.
func pinnedPack(root, name string) (*profilepack.Pack, refusal) {
	if name == "" {
		return nil, refusal{}
	}
	data, declined := workspaceDocument(root, name, profilepack.MaxPackBytes, "the profile pack")
	if declined.state != "" {
		return nil, declined
	}
	pack, err := profilepack.Decode(data)
	if err != nil {
		return nil, refusal{Failed, err.Error()}
	}
	return &pack, refusal{}
}

// workspaceDocument reads one declared document named by an entry of the open
// workspace. The entry is inspected without following a symbolic link, exactly
// as the sequence inspects a rules document and the grid inspects an index, so
// a listing can never be used to reach a file outside the folder the person
// opened. what is the document's own noun phrase, so every refusal below reads
// as a sentence about the document a person named rather than about documents
// in general.
func workspaceDocument(root, name string, limit int, what string) ([]byte, refusal) {
	if err := artifactpath.EntryName(name); err != nil {
		return nil, refusal{Failed, what + " must be named by one entry of the open workspace"}
	}
	path := artifactpath.JoinReference(root, name)
	entry, err := os.Lstat(path)
	switch {
	case err != nil || !entry.Mode().IsRegular():
		return nil, refusal{Failed, what + " must be one regular file of the open workspace"}
	case entry.Size() > int64(limit):
		return nil, refusal{Failed, what + " is larger than this release reads"}
	}
	declared, err := os.ReadFile(path)
	// A document this account cannot read is a different answer from one the
	// reader refused: there is nothing to fix in it, and writing it again is
	// not the remedy.
	if errors.Is(err, fs.ErrPermission) {
		return nil, refusal{PermissionDenied, "this account cannot read " + what}
	}
	if err != nil {
		return nil, refusal{Failed, what + " could not be read"}
	}
	return declared, refusal{}
}

// inventoried groups every located finding by the export surface it is on, in
// the order the review reported them, and counts what each surface left
// unresolved. The grouping is the engine's own location and its own word for
// what it found, so nothing here decides which surfaces exist.
func inventoried(findings []exportreview.Finding) ([]ReviewSurface, int) {
	surfaces := make([]ReviewSurface, 0, 8)
	at := map[ReviewSurface]int{}
	unresolved := 0
	for _, finding := range findings {
		key := ReviewSurface{Name: surfaceOf(finding.Location), Content: finding.Reason}
		position, known := at[key]
		if !known {
			position = len(surfaces)
			at[key] = position
			surfaces = append(surfaces, key)
		}
		surfaces[position].Findings++
		if !finding.Resolved {
			surfaces[position].Unresolved++
			unresolved++
		}
	}
	return surfaces, unresolved
}

// surfaceOf names the export surface one location is on. Locations are written
// with `/` by every reporting engine, on every platform, so the first element
// is the surface and a location with no separator is a surface of its own.
func surfaceOf(location string) string {
	if surface, _, found := strings.Cut(location, "/"); found {
		return surface
	}
	return location
}

// reviewed turns one verified review into the inventory the window draws and
// the requested window of its findings, and states the reviewer's decision.
func reviewed(request ReviewRequest, opened *redact.Review, surfaces []ReviewSurface, unresolved int) ReviewResult {
	all := make([]ReviewFinding, 0, len(opened.Findings))
	for _, finding := range opened.Findings {
		all = append(all, ReviewFinding{
			Surface: surfaceOf(finding.Location), Location: finding.Location,
			Class: finding.Class, Reason: finding.Reason,
			Policy: finding.Policy, Resolved: finding.Resolved,
		})
	}
	window := make([]ReviewFinding, 0, request.Limit)
	if request.Offset < len(all) {
		window = append(window, all[request.Offset:min(len(all), request.Offset+request.Limit)]...)
	}
	decision, because := decided(request.Approve, opened)
	described := &Review{
		Name: request.Review, Report: opened.Schema, State: opened.State,
		DataOrigin: opened.DataOrigin, Identity: opened.Identity,
		InputCommitment: opened.InputCommitment, LocalStateCommitment: opened.LocalStateCommitment,
		DerivedIdentity: opened.DerivedIdentity, DerivedSpecSHA256: opened.DerivedSpecSHA256,
		Policies: opened.Policies, Surfaces: surfaces,
		Coverage: opened.Coverage, Uncovered: opened.Uncovered, Residual: opened.Residual,
		RequiredFailures: opened.RequiredFailures, OriginalFailedAssertions: opened.OriginalFailedAssertions,
		Decision: decision, DecisionReason: because,
		Unresolved: unresolved, Offset: request.Offset, Limit: request.Limit,
		Total: len(all), Findings: window,
		Scope: opened.Scope, Boundary: reviewBoundary,
	}
	// A member the interface draws as a list is a list, never null, and only a
	// review that is ready establishes anything at all.
	for _, list := range []*[]string{&described.Policies, &described.Uncovered} {
		if *list == nil {
			*list = []string{}
		}
	}
	for _, list := range []*[]int{&described.RequiredFailures, &described.OriginalFailedAssertions} {
		if *list == nil {
			*list = []int{}
		}
	}
	if described.Residual.Locations == nil {
		described.Residual.Locations = []string{}
	}
	if opened.State == readyForApproval {
		described.Establishes = DisclosureReviewed
	}
	// A review holding no finding at all is refused by the reader that just
	// accepted this one, so an empty window here can only be a window that
	// begins past the last finding, and it keeps the counts beside it.
	if len(window) == 0 {
		return ReviewResult{State: Empty, Reason: "this window begins past the last finding of this review", Review: described}
	}
	return ReviewResult{State: Completed, Review: described}
}

// readyForApproval is the one state of the review contract that an approval can
// name. It is the reader's own word, matched rather than redefined.
const readyForApproval = "ready-for-approval"

// decided reports the reviewer's explicit decision over this review, as its own
// named outcome. An incomplete review is answered first and whatever identity
// was named, because a review holding unresolved findings or a residual hit
// cannot authorize disclosure at all — naming its exact bytes does not change
// that.
func decided(approval string, opened *redact.Review) (Decision, string) {
	switch {
	case opened.State != readyForApproval:
		return IncompleteReview, "this review is not complete; an incomplete review cannot authorize disclosure, whatever identity approves it"
	case approval == "":
		return NotDecided, "nobody has approved this review; approving it names the exact identity below"
	case approval != opened.Identity:
		return StaleApproval, "this approval names a different review than the bytes on disk now; the input, policy, specification or output changed, so review the current one again"
	}
	return Approved, "this approval names exactly the review that was read, which binds its input, policy, specification and output versions"
}
