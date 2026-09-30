package desktop

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/redact"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/report"
	"github.com/bharm16/readmit/internal/reportshare"
	"github.com/bharm16/readmit/internal/runresult"
	"github.com/bharm16/readmit/internal/testrunner"
)

// A template that declares rerun-derived-tests/v1 requires a real check run
// before the derived messages a share holds are shared: the derived case is
// sent, as a separately reviewed run (#555), to the environment the report's
// original run reached, and must reproduce the original phase. Preparing the
// check derives the export review it runs on (readmit redact), from the
// report's retained case and specification under the draft's policy and an
// inventory the backend assembles from the retained runs, with the project's
// sharing key, so the derived case is exactly the messages the share
// previews. Nothing is sent by preparing it, and a share stays blocked until
// a retained run of that derived case matched the phase.

// PrepareShareCheckAction derives the export review a share's check runs on.
const PrepareShareCheckAction ActionID = "report.prepare-check"

func init() {
	actionPolicies[PrepareShareCheckAction] = actionPolicy{consent: DeriveConsent, requirements: []ReviewRequirement{InventoryDeclarationRequirement},
		review: slot{}, perform: slot{profile: "DeriveExportReview"}, bind: bindShareCheck, execute: executeShareCheck}
}

// shareCheckInputs are the exact documents a share's check is derived from.
type shareCheckInputs struct {
	dir        string
	policy     []byte
	inventory  []byte
	spec       []byte
	commitment string
	phase      string
	display    []redact.OriginalArtifact
	current    *runresult.Result
}

// shareCheckInputs assembles what the check of one share draft derives
// from: the draft's policy, the retained runs as the original-artifact
// inventory, and the retained specification bound to the retained case.
func (c *loadedCatalog) shareCheckInputs(backing reportBacking, options ReportShareOptions) (*shareCheckInputs, error) {
	var template *redact.Policy
	if options.Template != "" {
		data, declined := workspaceDocument(c.root, options.Template, privacyDocumentLimit, "the template")
		if declined.state != "" {
			return nil, errors.New(declined.reason)
		}
		policy, err := redact.DecodePolicy(data)
		if err != nil {
			return nil, err
		}
		template = &policy
	}
	policy, err := reportshare.EffectivePolicy(template, options.Overrides)
	if err != nil || policy == nil {
		return nil, errors.New("a check needs a template")
	}
	policyBytes, err := canonicalRedactDocument(policy)
	if err != nil {
		return nil, err
	}
	if _, err := redact.DecodePolicy(policyBytes); err != nil {
		return nil, err
	}
	current, err := runresult.Open(filepath.Join(backing.packetDir, "current"))
	if err != nil || current.Artifact == nil || current.Spec == nil {
		return nil, errors.New("the report's retained run cannot be read")
	}
	in := &shareCheckInputs{current: current, policy: policyBytes}
	switch current.Artifact.Result.Status {
	case testrunner.Pass:
		in.phase = "pass"
	case testrunner.AssertionFailure:
		in.phase = "failure"
	default:
		return nil, errors.New("a check reproduces a run that passed or failed its checks; this run did neither")
	}
	artifacts := []redact.OriginalArtifact{{Kind: "result", Path: current.ResultPath}}
	in.display = []redact.OriginalArtifact{{Kind: "result", Path: "current"}}
	if backing.packet.Manifest.Baseline != nil && backing.packet.Manifest.Baseline.CaseIdentity == backing.packet.Manifest.Current.CaseIdentity {
		if baseline, err := runresult.Open(filepath.Join(backing.packetDir, "baseline")); err == nil && baseline.Artifact != nil {
			artifacts = append(artifacts, redact.OriginalArtifact{Kind: "result", Path: baseline.ResultPath})
			in.display = append(in.display, redact.OriginalArtifact{Kind: "result", Path: "comparison"})
		}
	}
	if in.inventory, err = canonicalRedactDocument(redact.Inventory{Schema: redact.InventorySchema, Complete: true, Artifacts: artifacts, ResidualValues: []string{}}); err != nil {
		return nil, err
	}
	spec := *current.Spec
	spec.Input.Case = filepath.Join(backing.packetDir, "case")
	if in.spec, err = json.Marshal(spec, json.Deterministic(true)); err != nil {
		return nil, err
	}
	in.spec = append(in.spec, '\n')
	in.commitment = redact.InputCommitment(in.policy, in.inventory, in.spec)
	in.dir = filepath.Join(c.root, catalog.Folder, "sharing", "check-"+in.commitment[:24])
	return in, nil
}

// shareCheckRun is the specification a check sends: the derived review's
// own, bound to its derived case and to the environment the original run
// reached.
func shareCheckRun(root, review string) string {
	sum := digestOf([]byte(review))
	return filepath.Join(root, catalog.Folder, "sharing", "run-"+sum[:24], "spec.json")
}

// originalTarget is the environment target of the project whose record is
// the one the original run retained.
func (c *loadedCatalog) originalTarget(record *replay.TargetRecord) string {
	if record == nil {
		return ""
	}
	for _, item := range c.document.Items {
		if item.Kind != string(EnvironmentItem) || c.removed(item) {
			continue
		}
		for i := len(item.Revisions) - 1; i >= 0; i-- {
			paths, availability, _ := c.revisionBacking(item, strconv.Itoa(item.Revisions[i].Number))
			if availability != ItemAvailable || paths["target"] == "" {
				continue
			}
			target, err := replay.ReadTarget(paths["target"])
			if err != nil {
				continue
			}
			if held, err := replay.Record(target); err == nil && reflect.DeepEqual(held, *record) {
				return paths["target"]
			}
		}
	}
	return ""
}

// shareRunCheck is whether the check a share needs has actual retained
// evidence: an export review derived from exactly this draft's inputs whose
// derived case holds exactly the shared messages, and a retained run of that
// derived case that matched the original phase.
func (c *loadedCatalog) shareRunCheck(backing reportBacking, bound *reportShareBinding) ShareRunCheck {
	in, err := c.shareCheckInputs(backing, bound.options)
	if err != nil {
		return ShareRunCheck{Reason: "the check cannot be prepared: " + err.Error()}
	}
	review, entry := c.shareCheckReview(in, bound.share)
	if review == nil {
		return ShareRunCheck{Reason: "run the check the template requires"}
	}
	runs, err := projectEntries(c.root, catalog.MaxItems, func(name string) bool { return strings.HasPrefix(name, "reexecution-") })
	if err != nil {
		return ShareRunCheck{Reason: err.Error()}
	}
	for _, name := range runs {
		opened, err := runresult.Open(filepath.Join(c.root, name))
		if err != nil || opened.Artifact == nil || opened.Artifact.Result.InputBundleIdentity != review.DerivedIdentity {
			continue
		}
		// A run counts only at the target the original run reached.
		sameTarget := reflect.DeepEqual(opened.Artifact.Result.Target, in.current.Artifact.Result.Target)
		if usable, _ := opened.Usable(); usable && sameTarget && redact.PhaseMatches(opened.Artifact, in.phase, review.RequiredFailures) {
			_, private, err := c.exportReview(filepath.Join(c.root, entry))
			if err != nil {
				return ShareRunCheck{Reason: err.Error()}
			}
			bound.derived = &derivedTest{review: filepath.Join(c.root, entry), private: filepath.Join(c.root, private.entry), identity: review.Identity}
			return ShareRunCheck{Satisfied: true}
		}
	}
	return ShareRunCheck{Reason: "the check has not run, or its run did not reproduce the original result"}
}

// shareCheckReview is the export review of the project derived from exactly
// these inputs whose derived case holds exactly the shared messages.
func (c *loadedCatalog) shareCheckReview(in *shareCheckInputs, share *reportshare.Share) (*redact.Review, string) {
	reviews, err := projectEntries(c.root, catalog.MaxItems, func(name string) bool {
		return strings.HasPrefix(name, "review-") && !strings.HasPrefix(name, "review-private")
	})
	if err != nil {
		return nil, ""
	}
	for _, name := range reviews {
		review, err := redact.OpenReview(filepath.Join(c.root, name))
		if err != nil || review.InputCommitment != in.commitment || review.State != "ready-for-approval" {
			continue
		}
		derived, err := bundle.Open(filepath.Join(c.root, name, "case"))
		if err != nil {
			continue
		}
		same := len(share.Derived) > 0
		for id, data := range share.Derived {
			raw, err := derived.Raw(id)
			same = same && err == nil && bytes.Equal(raw, data)
		}
		if same {
			return review, name
		}
	}
	return nil, ""
}

// derivedTest is the proven derived test a checked share holds: the review
// its check ran on, whose export (readmit redact export) is written beside
// the report.
type derivedTest struct {
	review, private, identity string
}

// derivedTestName is the folder a checked share's derived test is written as.
const derivedTestName = "Derived test"

// shareCheckBinding is what preparing a check writes.
type shareCheckBinding struct {
	root, packetDir string
	inputs          *shareCheckInputs
	review, private string
	target          string
	key             []byte
}

// ShareCheckView is what preparing a check derives from and writes.
type ShareCheckView struct {
	Report    string               `json:"report"`
	Phase     string               `json:"phase"`
	Inventory InventoryDeclaration `json:"inventory"`
	Messages  int                  `json:"messages"`
}

func bindShareCheck(a *App, ctx context.Context, request PrepareActionRequest, held bool) (*boundAction, refusal) {
	if len(request.Items) != 1 || request.Items[0].Kind != ReportItem || request.ReportShare == nil {
		return nil, refusal{Failed, "a check is prepared for one report's share"}
	}
	loaded, items, records, declined := a.scoped(ctx, request.Context, request.Items)
	if loaded == nil {
		return nil, declined
	}
	backing, err := loaded.reportBacking(records[0], "")
	if err != nil {
		return nil, refusal{Failed, err.Error()}
	}
	in, err := loaded.shareCheckInputs(backing, *request.ReportShare)
	if err != nil {
		return nil, refusal{Failed, "the check cannot be prepared: " + err.Error()}
	}
	key, err := shareKey(loaded.root)
	if err != nil {
		return nil, refusal{Failed, "the project's sharing key cannot be read or created"}
	}
	review, refused := destinationFor(loaded.root, "", "review")
	if refused.state != "" {
		return nil, refused
	}
	private, refused := destinationFor(loaded.root, "", "review-private")
	if refused.state != "" {
		return nil, refused
	}
	target := loaded.originalTarget(in.current.Artifact.Result.Target)
	ready, reason := review.Fresh && private.Fresh, destinationsReason(review, private)
	if target == "" && ready {
		ready, reason = false, "no environment of the project is the one the original run reached; a check runs there"
	}
	messages := 0
	if doc, err := report.BuildDocument(ctx, backing.packetDir, backing.packet, backing.authored); err == nil && request.ReportShare.Contents.Messages {
		messages = len(doc.Messages)
	}
	declaration := InventoryDeclaration{Entry: backing.authored.Title, Artifacts: in.display, ResidualValues: 0, Digest: digestOf(in.inventory)}
	bound := &shareCheckBinding{root: loaded.root, packetDir: backing.packetDir, inputs: in, review: review.Name, private: private.Name, target: target, key: key}
	return &boundAction{action: PrepareShareCheckAction, origin: request, shareCheck: bound,
		binding: binding(string(PrepareShareCheckAction), loaded.root, loaded.document.Project.ID, a.reviewer(), a.policyBinding(ctx, false, held),
			records[0].ID, backing.revision, backing.packet.Identity, in.commitment, review.Name, private.Name, target, fileDigest(target)),
		review: ActionReview{Items: items, Ready: ready, Refusal: reason, Destination: ReviewDestination{Output: review.Name},
			Derive:     &DeriveReviewView{Inventory: declaration, Review: review.Name, Private: private.Name},
			ShareCheck: &ShareCheckView{Report: backing.authored.Title, Phase: in.phase, Inventory: declaration, Messages: messages}}}, noRefusal
}

// ShareCheckOutcome is the check a window hands to the reviewed run: the
// derived export review, the report whose original run it reproduces, and
// the phase.
type ShareCheckOutcome struct {
	Review ItemRef `json:"review"`
	Packet ItemRef `json:"packet"`
	Phase  string  `json:"phase"`
}

func executeShareCheck(a *App, ctx context.Context, bound *boundAction, _ ReviewDecisions) ReviewedActionResult {
	check := bound.shareCheck
	in := check.inputs
	result := ReviewedActionResult{Outcome: ActionRefused}
	if err := os.MkdirAll(in.dir, 0o700); err != nil {
		result.refuse(Failed, "the check's inputs cannot be written")
		return result
	}
	for name, data := range map[string][]byte{"policy.json": in.policy, "inventory.json": in.inventory, "spec.json": in.spec} {
		path := filepath.Join(in.dir, name)
		if held, err := os.ReadFile(path); err == nil && bytes.Equal(held, data) {
			continue
		}
		os.Remove(path)
		if err := shellDocument.Create(path, data); err != nil {
			result.refuse(Failed, "the check's inputs cannot be written")
			return result
		}
	}
	created, err := redact.Create(ctx, redact.Request{CasePath: filepath.Join(check.packetDir, "case"), SpecPath: filepath.Join(in.dir, "spec.json"),
		PolicyPath: filepath.Join(in.dir, "policy.json"), InventoryPath: filepath.Join(in.dir, "inventory.json"),
		Output: filepath.Join(check.root, check.review), LocalState: filepath.Join(check.root, check.private), Key: check.key})
	if err != nil {
		state, reason := privacyRefusal(err)
		result.refuse(state, reason)
		if state == Cancelled {
			result.Outcome = ActionCancelled
		}
		return result
	}
	result.Derived = privacyOutcome(check.review, check.private, created)
	if created.State != "ready-for-approval" {
		result.refuse(Failed, "the derived messages still hold unresolved values; the check cannot run until the template handles them")
		return result
	}
	derived, err := testrunner.ReadSpec(filepath.Join(check.root, check.review, "spec.json"))
	if err != nil {
		result.refuse(Failed, "the derived test cannot be read back")
		return result
	}
	derived.Input.Case = filepath.Join(check.root, check.review, "case")
	derived.Target = check.target
	data, err := json.Marshal(derived, json.Deterministic(true))
	if err != nil {
		result.refuse(Failed, "the check's test cannot be written")
		return result
	}
	run := shareCheckRun(check.root, check.review)
	if err := os.MkdirAll(filepath.Dir(run), 0o700); err != nil || shellDocument.Create(run, append(data, '\n')) != nil {
		result.refuse(Failed, "the check's test cannot be written")
		return result
	}
	loaded, _ := a.loadCatalog(ctx, bound.origin.Context, false)
	if loaded == nil {
		result.refuse(Failed, "the project cannot be read")
		return result
	}
	index := loaded.document.ByEntry(string(ReportItem), check.review)
	if index < 0 {
		result.refuse(Failed, "the derived review is not listed in the project")
		return result
	}
	result.State, result.Outcome = Completed, ActionCompleted
	result.ShareCheck = &ShareCheckOutcome{Review: ItemRef{Kind: ReportItem, ID: loaded.document.Items[index].ID}, Packet: bound.origin.Items[0], Phase: in.phase}
	return result
}

// shareCheckSpec is the check test of an export review that preparing a
// share's check wrote, or empty.
func shareCheckSpec(root, review string) string {
	path := shareCheckRun(root, review)
	if !regular(path) {
		return ""
	}
	return path
}
