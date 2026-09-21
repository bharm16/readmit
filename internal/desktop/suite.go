package desktop

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/expectation"
	"github.com/bharm16/readmit/internal/runqueue"
	"github.com/bharm16/readmit/internal/suite"
)

// This file is the suite-management surface of the window: authoring suites
// through structured controls over the one canonical readmit-suite/v1
// contract, previewing the exact expansion preparation would compile, and
// connecting the expectation-release sidecar, coverage assessment and
// environment promotion review the command line already owns. Every operation
// calls the same internal/suite readers and writers `readmit suite` uses; the
// window adds no second suite language, and nothing here sends. Prepared
// execution belongs to the execution center, which continues from the private
// directories these operations retain.

// SuiteDraftSchema is the contract of the suite editor's draft content: the
// suite document an editor holds before it is stored, every member of which
// may still be empty. It is interpreted by this panel alone, through the same
// strict reader the suite contract applies, and a draft never claims to be a
// valid suite.
const SuiteDraftSchema = "readmit-suite-draft/v1"

// suiteDocument is the suite contract as the shared authoring seam works with
// it: the same strict decoder `readmit suite` reads, the suite's own byte
// bound, and its noun phrase.
var suiteDocument = ruleDocument[suite.Document]{"the suite document", suite.MaxBytes, suite.Decode}

// SuiteDocumentResult carries one suite: the canonical text the entry holds
// or the save wrote, the digest of those exact bytes, and the typed document
// the structured editor edits. SHA256 of a saved suite is the identity a
// coverage document and a promotion review bind to.
type SuiteDocumentResult struct {
	State    State           `json:"state"`
	Reason   string          `json:"reason,omitzero"`
	Document string          `json:"document,omitzero"`
	Output   string          `json:"output,omitzero"`
	SHA256   string          `json:"sha256,omitzero"`
	Suite    *suite.Document `json:"suite,omitzero"`
}

func (r *SuiteDocumentResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// OpenSuite reads one suite entry of the open workspace through the same
// strict reader `readmit suite` applies, so a suite the command line wrote
// opens with no clause dropped and no member invented. It writes nothing.
func (a *App) OpenSuite(workspace, entry string) SuiteDocumentResult {
	return run(a, false, false, func(context.Context) SuiteDocumentResult {
		document, sha, parsed, declined := suiteDocument.opened(workspace, entry)
		if parsed == nil {
			return SuiteDocumentResult{State: declined.state, Reason: declined.reason}
		}
		return SuiteDocumentResult{State: Completed, Document: document, SHA256: sha, Suite: parsed}
	})
}

// ValidateSuite decodes canonical JSON an operator pasted, exactly as a save
// would, and returns the typed document and canonical form. It is the expert
// import path; it writes nothing.
func (a *App) ValidateSuite(canonical string) SuiteDocumentResult {
	return run(a, false, false, func(context.Context) SuiteDocumentResult {
		parsed, err := suiteDocument.parse([]byte(canonical))
		if err != nil {
			return SuiteDocumentResult{State: Failed, Reason: err.Error()}
		}
		canonicalForm, err := canonicalDocument(parsed)
		if err != nil {
			return SuiteDocumentResult{State: Failed, Reason: "the suite document could not be canonicalized"}
		}
		return SuiteDocumentResult{State: Completed, Document: string(canonicalForm), SHA256: digest(canonicalForm), Suite: &parsed}
	})
}

// SaveSuite validates the editor's document through the suite's own reader
// and writes one canonical revision into a new entry of the open workspace.
// Versioning a suite is saving a new entry; an existing entry is never
// replaced and nothing rewrites the bytes another result was derived from.
func (a *App) SaveSuite(request RuleDocumentSaveRequest) SuiteDocumentResult {
	return run(a, false, true, func(context.Context) SuiteDocumentResult {
		document, sha, parsed, declined := suiteDocument.saved(request)
		if parsed == nil {
			return SuiteDocumentResult{State: declined.state, Reason: declined.reason}
		}
		return SuiteDocumentResult{State: Completed, Document: document, Output: request.Output, SHA256: sha, Suite: parsed}
	})
}

// SuitePreviewRequest names the suite to expand — canonical text an editor
// holds, or one entry of the open workspace — together with the declared
// environment to bind and, optionally, a readmit-suite-releases/v1 entry whose
// pins are verified. Exactly one of Document and Entry names the suite.
type SuitePreviewRequest struct {
	Workspace   string `json:"workspace"`
	Document    string `json:"document"`
	Entry       string `json:"entry"`
	Environment string `json:"environment"`
	Releases    string `json:"releases"`
}

// SuitePreviewResult carries the exact expansion: the jobs preparation would
// compile, their effective inputs and bindings, the release pins in force,
// and the resource serialization the queue holds. Nothing is written and
// nothing is sent.
type SuitePreviewResult struct {
	State     State            `json:"state"`
	Reason    string           `json:"reason,omitzero"`
	Expansion *suite.Expansion `json:"expansion,omitzero"`
}

func (r *SuitePreviewResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// PreviewSuite expands one suite against one environment exactly as
// preparation would, without creating a directory. The declared input order
// is stated and never changed; stateful tests are never silently
// parallelized. It reads the referenced templates and never opens a target.
func (a *App) PreviewSuite(request SuitePreviewRequest) SuitePreviewResult {
	return run(a, false, false, func(context.Context) SuitePreviewResult {
		root, declined := resolveFolder(request.Workspace)
		if root == "" {
			return SuitePreviewResult{State: declined.state, Reason: declined.reason}
		}
		if (request.Document == "") == (request.Entry == "") {
			return SuitePreviewResult{State: Failed, Reason: "name exactly one suite: the document being edited or one workspace entry"}
		}
		var parsed suite.Document
		if request.Document != "" {
			decoded, err := suiteDocument.parse([]byte(request.Document))
			if err != nil {
				return SuitePreviewResult{State: Failed, Reason: err.Error()}
			}
			parsed = decoded
		} else {
			data, declined := workspaceDocument(root, request.Entry, suite.MaxBytes, "the suite document")
			if data == nil {
				return SuitePreviewResult{State: declined.state, Reason: declined.reason}
			}
			decoded, err := suiteDocument.parse(data)
			if err != nil {
				return SuitePreviewResult{State: Failed, Reason: err.Error()}
			}
			parsed = decoded
		}
		references := ""
		if request.Releases != "" {
			data, declined := workspaceDocument(root, request.Releases, suite.MaxBytes, "the suite release references")
			if data == nil {
				return SuitePreviewResult{State: declined.state, Reason: declined.reason}
			}
			references = filepath.Join(root, request.Releases)
		}
		expansion, err := suite.Preview(root, parsed, request.Environment, references)
		if err != nil {
			return SuitePreviewResult{State: Failed, Reason: err.Error()}
		}
		return SuitePreviewResult{State: Completed, Expansion: &expansion}
	})
}

// SuitePrepareRequest compiles one saved suite entry against one declared
// environment into a new private directory entry of the open workspace. This
// is `readmit suite prepare`: it never sends, and execution is a separate
// explicit step in the execution center.
type SuitePrepareRequest struct {
	Workspace   string `json:"workspace"`
	Entry       string `json:"entry"`
	Environment string `json:"environment"`
	Releases    string `json:"releases"`
	Output      string `json:"output"`
}

// SuitePreparedResult carries the retained configuration directory and the
// queue plan it published. Nothing was sent.
type SuitePreparedResult struct {
	State     State          `json:"state"`
	Reason    string         `json:"reason,omitzero"`
	Directory string         `json:"directory,omitzero"`
	Queue     *runqueue.Plan `json:"queue,omitzero"`
}

func (r *SuitePreparedResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// PrepareSuite binds one saved suite to one declared environment and retains
// the compiled configuration in a new private directory, exactly as
// `readmit suite prepare` writes it. Existing output is refused, never
// resumed; no credential provider runs and nothing is sent.
func (a *App) PrepareSuite(request SuitePrepareRequest) SuitePreparedResult {
	return run(a, true, true, func(context.Context) SuitePreparedResult {
		root, declined := resolveFolder(request.Workspace)
		if root == "" {
			return SuitePreparedResult{State: declined.state, Reason: declined.reason}
		}
		if err := artifactpath.EntryName(request.Entry); err != nil {
			return SuitePreparedResult{State: Failed, Reason: "the suite must be named by one entry of the open workspace"}
		}
		if err := artifactpath.EntryName(request.Output); err != nil {
			return SuitePreparedResult{State: Failed, Reason: "a prepared suite is written to one new directory entry of the open workspace"}
		}
		references := ""
		if request.Releases != "" {
			if err := artifactpath.EntryName(request.Releases); err != nil {
				return SuitePreparedResult{State: Failed, Reason: "the suite release references must be named by one entry of the open workspace"}
			}
			references = filepath.Join(root, request.Releases)
		}
		path := filepath.Join(root, request.Entry)
		var prepared suite.Prepared
		var err error
		if references != "" {
			prepared, err = suite.PrepareApproved(path, request.Environment, filepath.Join(root, request.Output), references)
		} else {
			prepared, err = suite.Prepare(path, request.Environment, filepath.Join(root, request.Output))
		}
		if err != nil {
			return SuitePreparedResult{State: Failed, Reason: err.Error()}
		}
		return SuitePreparedResult{State: Completed, Directory: request.Output, Queue: &prepared.Queue}
	})
}

// SuiteCoverageSaveRequest authors the coverage document of one prepared
// suite directory: the suite digest and every specification pin come from the
// retained bytes, and only the requirements and exclusions are declarations.
type SuiteCoverageSaveRequest struct {
	Workspace    string              `json:"workspace"`
	Prepared     string              `json:"prepared"`
	Requirements []suite.Requirement `json:"requirements"`
	Exclusions   []suite.Exclusion   `json:"exclusions"`
	Output       string              `json:"output"`
}

// SuiteCoverageResult carries either the authored canonical document or the
// assessment the same strict reader reports. The report's denominator is the
// explicit declared one; a reduced selected set is never full coverage.
type SuiteCoverageResult struct {
	State    State                 `json:"state"`
	Reason   string                `json:"reason,omitzero"`
	Document string                `json:"document,omitzero"`
	Output   string                `json:"output,omitzero"`
	Report   *suite.CoverageReport `json:"report,omitzero"`
}

func (r *SuiteCoverageResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// SaveSuiteCoverage authors one coverage document from the operator's
// declarations and the exact retained bytes of a prepared suite directory,
// and writes it as one new entry of the open workspace. It changes no
// execution state and excludes nothing from execution.
func (a *App) SaveSuiteCoverage(request SuiteCoverageSaveRequest) SuiteCoverageResult {
	return run(a, false, true, func(context.Context) SuiteCoverageResult {
		root, declined := resolveFolder(request.Workspace)
		if root == "" {
			return SuiteCoverageResult{State: declined.state, Reason: declined.reason}
		}
		prepared, declined := suiteDirectory(root, request.Prepared)
		if prepared == "" {
			return SuiteCoverageResult{State: declined.state, Reason: declined.reason}
		}
		authored, err := suite.BuildCoverage(prepared, request.Requirements, request.Exclusions)
		if err != nil {
			return SuiteCoverageResult{State: Failed, Reason: err.Error()}
		}
		if err := writeWorkspaceEntry(root, request.Output, authored); err != nil {
			if errors.Is(err, fs.ErrPermission) {
				return SuiteCoverageResult{State: PermissionDenied, Reason: "this account cannot write into the open workspace"}
			}
			return SuiteCoverageResult{State: Failed, Reason: err.Error()}
		}
		return SuiteCoverageResult{State: Completed, Document: string(authored), Output: request.Output}
	})
}

// SuiteCoverageAssessRequest assesses one prepared suite directory against an
// explicit coverage document, with up to fifteen previous directories of the
// same suite and environment and an optional fixed assessment instant.
type SuiteCoverageAssessRequest struct {
	Workspace    string   `json:"workspace"`
	Prepared     string   `json:"prepared"`
	Requirements string   `json:"requirements"`
	Previous     []string `json:"previous"`
	At           string   `json:"at"`
}

// AssessSuiteCoverage reads retained suites only, exactly as `readmit suite
// coverage` does. Missing evidence is unknown, exclusions never pass, and an
// expired exclusion stays visible; nothing executes and nothing is sent.
func (a *App) AssessSuiteCoverage(request SuiteCoverageAssessRequest) SuiteCoverageResult {
	return runNamed[SuiteCoverageResult, *SuiteCoverageResult](a, "suite-coverage-assessment", true, false, func(ctx context.Context) SuiteCoverageResult {
		root, declined := resolveFolder(request.Workspace)
		if root == "" {
			return SuiteCoverageResult{State: declined.state, Reason: declined.reason}
		}
		prepared, declined := suiteDirectory(root, request.Prepared)
		if prepared == "" {
			return SuiteCoverageResult{State: declined.state, Reason: declined.reason}
		}
		data, declined := workspaceDocument(root, request.Requirements, suite.MaxBytes, "the suite coverage document")
		if data == nil {
			return SuiteCoverageResult{State: declined.state, Reason: declined.reason}
		}
		policy := filepath.Join(root, request.Requirements)
		previous := make([]string, 0, len(request.Previous))
		for _, name := range request.Previous {
			dir, declined := suiteDirectory(root, name)
			if dir == "" {
				return SuiteCoverageResult{State: declined.state, Reason: declined.reason}
			}
			previous = append(previous, dir)
		}
		now := time.Now().UTC()
		if request.At != "" {
			fixed, err := time.Parse(time.RFC3339, request.At)
			if err != nil {
				return SuiteCoverageResult{State: Failed, Reason: "the assessment instant must be an RFC3339 timestamp"}
			}
			now = fixed
		}
		report, err := suite.AssessCoverage(ctx, prepared, policy, previous, now)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return SuiteCoverageResult{State: Cancelled, Reason: "coverage assessment cancelled; retained evidence is unchanged"}
			}
			return SuiteCoverageResult{State: Failed, Reason: err.Error()}
		}
		return SuiteCoverageResult{State: Completed, Report: &report}
	})
}

// suiteDirectory resolves one retained prepared-suite directory entry of the
// open workspace, without following a symbolic link.
func suiteDirectory(root, name string) (string, refusal) {
	if err := artifactpath.EntryName(name); err != nil {
		return "", refusal{Failed, "the prepared suite must be named by one directory entry of the open workspace"}
	}
	path := filepath.Join(root, name)
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() {
		return "", refusal{Failed, "the prepared suite must be one directory entry of the open workspace"}
	}
	return path, refusal{}
}

// SuitePromotionRequest reviews one saved suite against one declared
// environment, its release sidecar and the operator's current revision
// assumption. Nothing is approved here and nothing is sent.
type SuitePromotionRequest struct {
	Workspace   string `json:"workspace"`
	Entry       string `json:"entry"`
	Environment string `json:"environment"`
	Releases    string `json:"releases"`
	Revision    string `json:"revision"`
}

// SuitePromotionApproveRequest records the explicit local approval of one
// reviewed promotion. Reviewed must name the review identity that was
// displayed; the engine re-reads every input and refuses a stale commitment.
type SuitePromotionApproveRequest struct {
	Workspace   string `json:"workspace"`
	Entry       string `json:"entry"`
	Environment string `json:"environment"`
	Releases    string `json:"releases"`
	Revision    string `json:"revision"`
	Reviewed    string `json:"reviewed"`
	Approver    string `json:"approver"`
	Rationale   string `json:"rationale"`
	Output      string `json:"output"`
}

// SuitePromotionResult carries the review's exact commitments — the suite and
// release digests, the revision assumption and every job pin — and, after an
// approval, the complete approval identity. An approval grants no send
// authority and proves nothing about the target's actual software.
type SuitePromotionResult struct {
	State    State                  `json:"state"`
	Reason   string                 `json:"reason,omitzero"`
	Review   *suite.PromotionReview `json:"review,omitzero"`
	Identity string                 `json:"identity,omitzero"`
	Output   string                 `json:"output,omitzero"`
}

func (r *SuitePromotionResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ReviewSuitePromotion compiles the suite into a private temporary directory
// and returns the exact commitments one approval would bind, exactly as
// `readmit suite review-promotion` does. No credential provider runs.
func (a *App) ReviewSuitePromotion(request SuitePromotionRequest) SuitePromotionResult {
	return run(a, false, false, func(context.Context) SuitePromotionResult {
		_, path, references, declined := promotionInputs(request.Workspace, request.Entry, request.Releases)
		if path == "" {
			return SuitePromotionResult{State: declined.state, Reason: declined.reason}
		}
		review, err := suite.ReviewPromotion(path, request.Environment, references, request.Revision)
		if err != nil {
			return SuitePromotionResult{State: Failed, Reason: err.Error()}
		}
		return SuitePromotionResult{State: Completed, Review: &review}
	})
}

// ApproveSuitePromotion records one explicit local approval of the reviewed
// promotion. Approval re-reads every input, so a changed suite, sidecar,
// environment or revision assumption refuses rather than approving stale
// commitments. It exclusively creates a new private file and grants no send
// authority.
func (a *App) ApproveSuitePromotion(request SuitePromotionApproveRequest) SuitePromotionResult {
	return run(a, false, true, func(context.Context) SuitePromotionResult {
		root, path, references, declined := promotionInputs(request.Workspace, request.Entry, request.Releases)
		if path == "" {
			return SuitePromotionResult{State: declined.state, Reason: declined.reason}
		}
		if err := artifactpath.EntryName(request.Output); err != nil {
			return SuitePromotionResult{State: Failed, Reason: "a promotion approval is written to one new entry of the open workspace"}
		}
		approval, err := suite.ApprovePromotion(path, request.Environment, references, request.Revision, request.Reviewed, request.Approver, request.Rationale, filepath.Join(root, request.Output))
		if err != nil {
			return SuitePromotionResult{State: Failed, Reason: err.Error()}
		}
		return SuitePromotionResult{State: Completed, Review: &approval.Review, Identity: approval.Identity(), Output: request.Output}
	})
}

// promotionInputs resolves the workspace root plus the suite entry and
// release sidecar a promotion review reads. The sidecar is required:
// promotion reviews exact approved expectations.
func promotionInputs(workspace, entry, releases string) (string, string, string, refusal) {
	root, declined := resolveFolder(workspace)
	if root == "" {
		return "", "", "", declined
	}
	if err := artifactpath.EntryName(entry); err != nil {
		return "", "", "", refusal{Failed, "the suite must be named by one entry of the open workspace"}
	}
	if err := artifactpath.EntryName(releases); err != nil {
		return "", "", "", refusal{Failed, "the release references must be named by one entry of the open workspace"}
	}
	return root, filepath.Join(root, entry), filepath.Join(root, releases), refusal{}
}

// SuiteReleasesResult carries one authored readmit-suite-releases/v1 sidecar:
// the canonical text, the entry it was written to, and the typed references.
type SuiteReleasesResult struct {
	State      State                    `json:"state"`
	Reason     string                   `json:"reason,omitzero"`
	Document   string                   `json:"document,omitzero"`
	Output     string                   `json:"output,omitzero"`
	References *suite.ReleaseReferences `json:"references,omitzero"`
}

func (r *SuiteReleasesResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// SaveSuiteReleases validates one release sidecar through the same reader
// `readmit suite prepare --releases` applies and writes it as one new entry.
// The exact release identities it pins are verified when the sidecar is used,
// never here by re-reading the releases.
func (a *App) SaveSuiteReleases(request RuleDocumentSaveRequest) SuiteReleasesResult {
	return run(a, false, true, func(context.Context) SuiteReleasesResult {
		root, declined := resolveFolder(request.Workspace)
		if root == "" {
			return SuiteReleasesResult{State: declined.state, Reason: declined.reason}
		}
		parsed, err := suite.DecodeReleases([]byte(request.Document))
		if err != nil {
			return SuiteReleasesResult{State: Failed, Reason: err.Error()}
		}
		// A sidecar is validated as written before the entry exists, because its
		// release paths resolve beside the sidecar once it is saved.
		canonical, err := canonicalDocument(parsed)
		if err != nil {
			return SuiteReleasesResult{State: Failed, Reason: "the suite release references could not be canonicalized"}
		}
		if err := writeWorkspaceEntry(root, request.Output, canonical); err != nil {
			if errors.Is(err, fs.ErrPermission) {
				return SuiteReleasesResult{State: PermissionDenied, Reason: "this account cannot write into the open workspace"}
			}
			return SuiteReleasesResult{State: Failed, Reason: err.Error()}
		}
		return SuiteReleasesResult{State: Completed, Document: string(canonical), Output: request.Output, References: &parsed}
	})
}

// SuiteImpactRequest names the suite, its sidecar and two retained releases.
// From must be the direct predecessor of To.
type SuiteImpactRequest struct {
	Workspace  string `json:"workspace"`
	Suite      string `json:"suite"`
	Releases   string `json:"releases"`
	From       string `json:"from"`
	To         string `json:"to"`
	ShowValues bool   `json:"show_values"`
}

// SuiteImpactResult carries the impact report: which suite tests pin the old
// release, which are already current, and the exact baseline and profile
// changes between the two. No pin is moved and no execution is assessed.
type SuiteImpactResult struct {
	State  State               `json:"state"`
	Reason string              `json:"reason,omitzero"`
	Impact *suite.ImpactReport `json:"impact,omitzero"`
}

func (r *SuiteImpactResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ExpectationImpact reports what one released template's successor would
// change for a saved suite, exactly as `readmit expectation impact` does. It
// reads only the named documents and moves no pin.
func (a *App) ExpectationImpact(request SuiteImpactRequest) SuiteImpactResult {
	return run(a, false, false, func(context.Context) SuiteImpactResult {
		root, declined := resolveFolder(request.Workspace)
		if root == "" {
			return SuiteImpactResult{State: declined.state, Reason: declined.reason}
		}
		for _, name := range []string{request.Suite, request.Releases, request.From, request.To} {
			if err := artifactpath.EntryName(name); err != nil {
				return SuiteImpactResult{State: Failed, Reason: "the suite, its release references and both releases must be named by entries of the open workspace"}
			}
		}
		from, err := expectation.Read(filepath.Join(root, request.From))
		if err != nil {
			return SuiteImpactResult{State: Failed, Reason: err.Error()}
		}
		to, err := expectation.Read(filepath.Join(root, request.To))
		if err != nil {
			return SuiteImpactResult{State: Failed, Reason: err.Error()}
		}
		report, err := suite.AssessReleases(filepath.Join(root, request.Suite), filepath.Join(root, request.Releases), from, to, request.ShowValues)
		if err != nil {
			return SuiteImpactResult{State: Failed, Reason: err.Error()}
		}
		return SuiteImpactResult{State: Completed, Impact: &report}
	})
}

// suiteDraft is the suite editor's draft content: the canonical suite text an
// editor holds and the entry it was opened from, either of which may still be
// empty. It is read by the panel that owns it and never claims to be valid.
type suiteDraft struct {
	Schema   string `json:"schema"`
	Entry    string `json:"entry,omitzero"`
	Document string `json:"document,omitzero"`
}

// validateSuiteDraft holds retained suite-editor work to its own contract:
// the draft names its schema and carries bounded text. Deciding whether that
// text is a suite stays with the suite reader, at preview and save.
func validateSuiteDraft(content []byte) error {
	var declared struct {
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(content, &declared); err != nil || declared.Schema != SuiteDraftSchema {
		return errors.New("suite draft content declares " + SuiteDraftSchema)
	}
	var draft suiteDraft
	if err := json.Unmarshal(content, &draft, json.RejectUnknownMembers(true)); err != nil {
		return errors.New("invalid suite draft content")
	}
	if len(draft.Document) > suite.MaxBytes {
		return fmt.Errorf("suite draft content exceeds %d bytes", suite.MaxBytes)
	}
	return nil
}
