package desktop_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/diff"
	"github.com/bharm16/readmit/internal/findingreview"
)

// The five authored documents the window offers editors for, each as one valid
// example. Every open and save goes through the contract's own strict parser,
// so what these tests hold the facade to is that the parser is the same one
// the command line applies — not a second grammar.
const (
	docNormalizationPolicy = `{"schema":"readmit-normalization-policy/v1","rules":[{"id":"volatile-time","selector":"MSH-7","operator":"timestamp","precision":"hour"}]}`
	docDiagnoseConfig      = `{"schema":"readmit-diagnose-config/v1","profile":"readmit-siu-v1","ruleset":"readmit-siu-diagnosis/v1","rules":["message.duplicate-control-id"],"namespaces":[]}`
	docFindingDecisions    = `{"schema":"readmit-finding-decisions/v1","report_sha256":"` +
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" +
		`","decisions":[{"finding":"f000001","verdict":"confirmed","rationale":"the schedule keeps refusing this booking"}]}`
	docSequenceAnalysis = `{"schema":"readmit-sequence-analysis/v1","case_identity":"` +
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" +
		`","rules_sha256":"","clock_tolerance_seconds":5,"windows":[{"source":"s0001","start":"2026-01-01T12:00:00Z","end":"2026-01-01T12:01:00Z","coverage":"partial"}],"retries":[],"downstream":[]}`
)

func sha256Of(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Opening an authored document reads the entry through its contract's own
// parser and reports the digest of the exact bytes the workspace holds, which
// is the identity every derived view binds to.
func TestOpeningAuthoredDocumentsReportsTheirCanonicalFormAndExactIdentity(t *testing.T) {
	root := t.TempDir()
	app := workspaceApp(t)
	writeDocument(t, root, "correlate.rules.json", seqRules)
	writeDocument(t, root, "compare.policy.json", docNormalizationPolicy)
	writeDocument(t, root, "diagnose.config.json", docDiagnoseConfig)
	writeDocument(t, root, "verdicts.json", docFindingDecisions)
	writeDocument(t, root, "analysis.json", docSequenceAnalysis)

	rules := app.OpenCorrelationRules(root, "correlate.rules.json")
	if rules.State != desktop.Completed || rules.Rules == nil || len(rules.Rules.Rules) != 3 {
		t.Fatalf("correlation rules: %+v", rules)
	}
	if rules.SHA256 != sha256Of([]byte(seqRules)) {
		t.Fatalf("the rules identity is not the digest of the bytes the workspace holds: %s", rules.SHA256)
	}
	policy := app.OpenNormalizationPolicy(root, "compare.policy.json")
	if policy.State != desktop.Completed || policy.Policy == nil || len(policy.Policy.Rules) != 1 {
		t.Fatalf("normalization policy: %+v", policy)
	}
	if policy.SHA256 != sha256Of([]byte(docNormalizationPolicy)) {
		t.Fatalf("the policy identity is not the digest of the bytes the workspace holds: %s", policy.SHA256)
	}
	config := app.OpenDiagnoseConfig(root, "diagnose.config.json")
	if config.State != desktop.Completed || config.Config == nil || config.Config.Profile != "readmit-siu-v1" {
		t.Fatalf("diagnose config: %+v", config)
	}
	if config.SHA256 != sha256Of([]byte(docDiagnoseConfig)) {
		t.Fatalf("the configuration identity is not the digest of the bytes the workspace holds: %s", config.SHA256)
	}
	decisions := app.OpenFindingDecisions(root, "verdicts.json")
	if decisions.State != desktop.Completed || decisions.Decisions == nil || len(decisions.Decisions.Decisions) != 1 {
		t.Fatalf("finding decisions: %+v", decisions)
	}
	if decisions.SHA256 != sha256Of([]byte(docFindingDecisions)) {
		t.Fatalf("the decisions identity is not the digest of the bytes the workspace holds: %s", decisions.SHA256)
	}
	analysis := app.OpenSequenceAnalysis(root, "analysis.json")
	if analysis.State != desktop.Completed || analysis.Declaration == nil || len(analysis.Declaration.Windows) != 1 {
		t.Fatalf("sequence analysis: %+v", analysis)
	}
	if analysis.SHA256 != sha256Of([]byte(docSequenceAnalysis)) {
		t.Fatalf("the declaration identity is not the digest of the bytes the workspace holds: %s", analysis.SHA256)
	}
	// Every document is reported in one canonical form an editor holds, which
	// is itself a document the same parser accepts.
	if !strings.HasPrefix(policy.Document, "{\n") || !strings.HasSuffix(policy.Document, "}\n") {
		t.Fatalf("the policy was not canonicalized for the editor: %q", policy.Document)
	}
	if _, err := diff.DecodePolicy([]byte(policy.Document)); err != nil {
		t.Fatalf("the canonical policy is not a policy: %v", err)
	}
}

// A document the contract's own reader refuses is refused here with that
// reader's own sentence, and a misspelled member is a refusal rather than a
// rule that silently stopped applying.
func TestOpeningAuthoredDocumentsRefusesWhatTheirOwnReadersRefuse(t *testing.T) {
	root := t.TempDir()
	app := workspaceApp(t)
	writeDocument(t, root, "policy.json", `{"schema":"readmit-normalization-policy/v1","rules":[{"id":"a","selector":"MSH-7","operator":"ignore","precisoin":"hour"}]}`)
	writeDocument(t, root, "config.json", `{"schema":"readmit-diagnose-config/v2","profile":"p","ruleset":"r","rules":["x"],"namespaces":[]}`)
	writeDocument(t, root, "decisions.json", `{"schema":"readmit-finding-decisions/v1","report_sha256":"short","decisions":[]}`)

	if got := app.OpenNormalizationPolicy(root, "policy.json"); got.State != desktop.Failed || got.Policy != nil {
		t.Fatalf("a policy with an unknown member was accepted: %+v", got)
	}
	if got := app.OpenDiagnoseConfig(root, "config.json"); got.State != desktop.Failed || got.Config != nil {
		t.Fatalf("a configuration under another contract was accepted: %+v", got)
	}
	if got := app.OpenFindingDecisions(root, "decisions.json"); got.State != desktop.Failed || got.Decisions != nil {
		t.Fatalf("decisions naming no report identity were accepted: %+v", got)
	}
	if got := app.OpenCorrelationRules(root, "absent.json"); got.State != desktop.Failed ||
		!strings.Contains(got.Reason, "one regular file of the open workspace") {
		t.Fatalf("an absent entry was not refused by name: %+v", got)
	}
	if got := app.OpenNormalizationPolicy(root, filepath.Join("..", "policy.json")); got.State != desktop.Failed {
		t.Fatalf("a name outside the workspace was accepted: %+v", got)
	}
}

// Saving validates through the same parser, writes one canonical new entry,
// and reports the digest of exactly what it wrote, so an editor's save and a
// later open agree about both bytes and identity.
func TestSavingAnAuthoredDocumentWritesOneCanonicalNewEntry(t *testing.T) {
	root := t.TempDir()
	app := workspaceApp(t)
	saved := app.SaveNormalizationPolicy(desktop.RuleDocumentSaveRequest{
		Workspace: root, Document: docNormalizationPolicy, Output: "compare.policy.json",
	})
	if saved.State != desktop.Completed || saved.Output != "compare.policy.json" || saved.Policy == nil {
		t.Fatalf("save: %+v", saved)
	}
	written, err := os.ReadFile(filepath.Join(root, "compare.policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != saved.Document || saved.SHA256 != sha256Of(written) {
		t.Fatalf("the reported document and identity are not the entry that was written: %q %s", written, saved.SHA256)
	}
	reopened := app.OpenNormalizationPolicy(root, "compare.policy.json")
	if reopened.State != desktop.Completed || reopened.Document != saved.Document || reopened.SHA256 != saved.SHA256 {
		t.Fatalf("a saved policy does not reopen as itself: %+v", reopened)
	}
	// Two saves of one meaning are one byte sequence: reordered members and
	// different whitespace canonicalize to the same document.
	reordered := app.SaveNormalizationPolicy(desktop.RuleDocumentSaveRequest{
		Workspace: root,
		Document:  "{\n  \"rules\": [{\"operator\":\"timestamp\",\"precision\":\"hour\",\"id\":\"volatile-time\",\"selector\":\"MSH-7\"}],\n  \"schema\": \"readmit-normalization-policy/v1\"\n}",
		Output:    "compare.policy-2.json",
	})
	if reordered.State != desktop.Completed || reordered.Document != saved.Document {
		t.Fatalf("one meaning canonicalized to two documents: %+v", reordered)
	}
	// The other four contracts save through the same seam.
	if got := app.SaveCorrelationRules(desktop.RuleDocumentSaveRequest{Workspace: root, Document: seqRules, Output: "rules.json"}); got.State != desktop.Completed {
		t.Fatalf("save correlation rules: %+v", got)
	}
	if got := app.SaveSequenceAnalysis(desktop.RuleDocumentSaveRequest{Workspace: root, Document: docSequenceAnalysis, Output: "analysis.json"}); got.State != desktop.Completed {
		t.Fatalf("save sequence analysis: %+v", got)
	}
	config := app.SaveDiagnoseConfig(desktop.RuleDocumentSaveRequest{Workspace: root, Document: docDiagnoseConfig, Output: "config.json"})
	if config.State != desktop.Completed {
		t.Fatalf("save diagnose config: %+v", config)
	}
	writtenConfig, err := os.ReadFile(filepath.Join(root, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(writtenConfig) != config.Document || config.SHA256 != sha256Of(writtenConfig) {
		t.Fatalf("the reported configuration and identity are not the entry that was written: %q %s", writtenConfig, config.SHA256)
	}
	if reopened := app.OpenDiagnoseConfig(root, "config.json"); reopened.SHA256 != config.SHA256 || reopened.Document != config.Document {
		t.Fatalf("a saved configuration does not reopen as itself: %+v", reopened)
	}
	decisions := app.SaveFindingDecisions(desktop.RuleDocumentSaveRequest{Workspace: root, Document: docFindingDecisions, Output: "verdicts.json"})
	if decisions.State != desktop.Completed {
		t.Fatalf("save finding decisions: %+v", decisions)
	}
	// What was saved is what `readmit diagnose review` would read.
	writtenDecisions, err := os.ReadFile(filepath.Join(root, "verdicts.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := findingreview.ParseDecisions(writtenDecisions); err != nil {
		t.Fatalf("the saved decisions are not a decisions document: %v", err)
	}
}

// A save never overwrites: an entry already there is refused and retained
// exactly as it was, because a document a result was derived from must not be
// edited in place. The remedy is a new revision under a new name.
func TestSavingAnAuthoredDocumentRefusesToOverwriteAndRetainsWhatWasThere(t *testing.T) {
	root := t.TempDir()
	app := workspaceApp(t)
	writeDocument(t, root, "compare.policy.json", docNormalizationPolicy)
	refused := app.SaveNormalizationPolicy(desktop.RuleDocumentSaveRequest{
		Workspace: root,
		Document:  `{"schema":"readmit-normalization-policy/v1","rules":[{"id":"other","selector":"MSH-10","operator":"ignore"}]}`,
		Output:    "compare.policy.json",
	})
	if refused.State != desktop.Failed || !strings.Contains(refused.Reason, "already exists") {
		t.Fatalf("an existing entry was not refused by name: %+v", refused)
	}
	retained, err := os.ReadFile(filepath.Join(root, "compare.policy.json"))
	if err != nil || string(retained) != docNormalizationPolicy {
		t.Fatalf("a refused save changed the retained document: %q %v", retained, err)
	}
	// An invalid document is refused before anything is written.
	invalid := app.SaveDiagnoseConfig(desktop.RuleDocumentSaveRequest{
		Workspace: root, Document: `{"schema":"readmit-diagnose-config/v1"}`, Output: "config.json",
	})
	if invalid.State != desktop.Failed {
		t.Fatalf("an invalid configuration was saved: %+v", invalid)
	}
	if _, err := os.Lstat(filepath.Join(root, "config.json")); !os.IsNotExist(err) {
		t.Fatal("a refused save left an entry behind")
	}
}

// Saving is authoring, so it is admitted the way every other write is: a shell
// with no operation policy selected cannot save, and the refusal names the
// admission rather than the document.
func TestSavingAnAuthoredDocumentRequiresAdmission(t *testing.T) {
	root := t.TempDir()
	state := t.TempDir()
	app := desktop.New(&chooser{}, desktop.ShellDocuments{Folder: state})
	refused := app.SaveFindingDecisions(desktop.RuleDocumentSaveRequest{
		Workspace: root, Document: docFindingDecisions, Output: "verdicts.json",
	})
	if refused.State != desktop.PermissionDenied {
		t.Fatalf("an unadmitted save was not refused: %+v", refused)
	}
	if _, err := os.Lstat(filepath.Join(root, "verdicts.json")); !os.IsNotExist(err) {
		t.Fatal("an unadmitted save wrote an entry")
	}
	// Reading needs no admission: the same shell opens what is already there.
	writeDocument(t, root, "verdicts.json", docFindingDecisions)
	if got := app.OpenFindingDecisions(root, "verdicts.json"); got.State != desktop.Completed {
		t.Fatalf("an unadmitted shell could not read an authored document: %+v", got)
	}
}
