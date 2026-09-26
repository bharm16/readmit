//go:build !windows

package desktop_test

// Some entries are held to the listing's rule by the reader that opens them
// rather than by the facade: the documents read through workspaceDocument, and
// the rules, analyses, indexes, baselines, suites, packets, packages and
// bundles whose own readers inspect the entry without following it. Each
// member below is handed a symbolic link out of the workspace and a link to
// one of its own entries. Each link leads to a real entry of the member's
// kind, which the member accepts when it is named directly, so only the
// reader's own check can refuse it. It must refuse it with its own sentence
// before reading through it, or, where it falls back rather than refusing,
// never read through it at all.

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/correlate"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/expectation"
	"github.com/bharm16/readmit/internal/guide"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/profileversion"
	"github.com/bharm16/readmit/internal/reduce"
	"github.com/bharm16/readmit/internal/suite"
	"github.com/bharm16/readmit/internal/testauthor"
	"github.com/bharm16/readmit/internal/testrunner"
	"github.com/bharm16/readmit/internal/transform"
)

// ownReader is one member whose own reader refuses a symbolic link: the entry
// of the workspace it accepts when named directly, and the sentences it
// refuses a link with. A member that falls back instead of refusing — reading
// the name itself as an inline document, or resolving without the pack it
// named — has no sentence, and is held to never reading through the link.
type ownReader struct {
	name, entry string
	reasons     []string
	call        func(entry string) refused
}

// fallsBack is the refusal of a member that falls back rather than refusing:
// none, so the member is held to never reading through the link instead.
var fallsBack []string

// refusesLinksToEntriesItAccepts copies each member's entry into a folder
// outside the workspace, plants beside the entry a link to that copy
// (link-NAME) and a link to the entry itself (alias-NAME), and holds every
// member to its own refusal of both. Nothing may be created in the workspace
// or changed outside it. Named directly afterwards, each entry is accepted, so
// the refusal was the reader's rule and nothing else.
func refusesLinksToEntriesItAccepts(t *testing.T, root string, members []ownReader) {
	t.Helper()
	outside := t.TempDir()
	confined := make([]confinedMember, 0, len(members))
	for _, member := range members {
		if _, err := os.Lstat(filepath.Join(root, "alias-"+member.entry)); os.IsNotExist(err) {
			copyEntry(t, filepath.Join(root, member.entry), filepath.Join(outside, member.entry))
			for link, target := range map[string]string{"link-" + member.entry: filepath.Join(outside, member.entry), "alias-" + member.entry: filepath.Join(root, member.entry)} {
				if err := os.Symlink(target, filepath.Join(root, link)); err != nil {
					t.Fatal(err)
				}
			}
		}
		if member.reasons != nil {
			confined = append(confined, confinedMember{member.name, member.reasons, map[string]string{
				"a symbolic link out of the workspace":         "link-" + member.entry,
				"a symbolic link to an entry of the workspace": "alias-" + member.entry,
			}, member.call})
		}
	}
	listed, before := entriesOf(t, root), bytesUnder(t, outside)
	refusesEveryEntry(t, confined)
	for _, member := range members {
		if member.reasons != nil {
			continue
		}
		targets := []string{filepath.Join(outside, member.entry), filepath.Join(root, member.entry)}
		for _, link := range []string{"link-" + member.entry, "alias-" + member.entry} {
			ageReads(t, targets...)
			answeredWithin(t, member.name+" with "+link, func() refused { return member.call(link) })
			for _, target := range targets {
				if readSince(t, target) {
					t.Errorf("%s with %s read through the link", member.name, link)
				}
			}
		}
	}
	if after := entriesOf(t, root); !reflect.DeepEqual(listed, after) {
		t.Fatalf("a refused link created an entry in the workspace: %v, was %v", after, listed)
	}
	if after := bytesUnder(t, outside); !reflect.DeepEqual(before, after) {
		t.Fatal("a refused link changed the folder outside the workspace")
	}
	for _, member := range members {
		call := member.name + " named directly"
		if got := answeredWithin(t, call, func() refused { return member.call(member.entry) }); got.state != desktop.Completed {
			t.Errorf("%s: %+v, so a link to it cannot show a refusal", call, got)
		}
	}
}

// notOneRegularFile is the sentence workspaceDocument refuses a link with,
// before it reads anything.
func notOneRegularFile(what string) []string {
	return []string{what + " must be one regular file of the open workspace"}
}

// The correlation rules and the sequence analysis a sequence and a correlation
// review read, and the two opened on their own in their editors.
func TestSequenceAndCorrelationReadersRefuseALinkToADocumentOfTheirKind(t *testing.T) {
	app, root, identity := sequenceWorkspace(t)
	writeDocument(t, root, "analysis.json", `{"schema":"readmit-sequence-analysis/v1","rules_sha256":"","case_identity":"`+identity+
		`","clock_tolerance_seconds":5,"windows":[{"source":"s0001","start":"2026-01-01T12:00:00Z","end":"2026-01-01T12:01:00Z","coverage":"partial"}],"retries":[],"downstream":[]}`)
	opened := app.OpenCorrelationReview(desktop.CorrelationReviewRequest{Workspace: root, Case: "incident", Identity: identity, Rules: seqRulesEntry})
	if opened.State != desktop.Completed || opened.View == nil || len(opened.View.Links) == 0 {
		t.Fatalf("correlation review: %+v", opened)
	}
	review := func(rules string) desktop.CorrelationReviewRequest {
		return desktop.CorrelationReviewRequest{Workspace: root, Case: "incident", Identity: identity, Rules: rules}
	}
	rules := []string{"correlation rules must be one regular file of the open workspace"}
	refusesLinksToEntriesItAccepts(t, root, []ownReader{
		{"OpenSequence(Rules)", seqRulesEntry, rules, func(entry string) refused {
			result := app.OpenSequence(sequenceRequest(root, identity, entry))
			return refused{result.State, result.Reason}
		}},
		{"OpenSequence(Analysis)", "analysis.json", []string{"sequence analysis must be one regular workspace file of at most 1 MiB"}, func(entry string) refused {
			request := sequenceRequest(root, identity, seqRulesEntry)
			request.Analysis = entry
			result := app.OpenSequence(request)
			return refused{result.State, result.Reason}
		}},
		{"OpenCorrelationReview(Rules)", seqRulesEntry, rules, func(entry string) refused {
			result := app.OpenCorrelationReview(review(entry))
			return refused{result.State, result.Reason}
		}},
		{"DecideCorrelation(Rules)", seqRulesEntry, rules, func(entry string) refused {
			request := review(entry)
			request.Mapping = opened.View.Mapping
			request.Decision = correlate.Decision{Action: "reject", Link: opened.View.Links[0].ID, Actor: "local analyst", Reason: "separate observation"}
			request.Output = "decided"
			result := app.DecideCorrelation(request)
			return refused{result.State, result.Reason}
		}},
		{"OpenCorrelationRules", seqRulesEntry, notOneRegularFile("the correlation rules document"), func(entry string) refused {
			result := app.OpenCorrelationRules(root, entry)
			return refused{result.State, result.Reason}
		}},
		{"OpenSequenceAnalysis", "analysis.json", notOneRegularFile("the sequence analysis declaration"), func(entry string) refused {
			result := app.OpenSequenceAnalysis(root, entry)
			return refused{result.State, result.Reason}
		}},
	})
}

// The rules, plan and profile pack a transformation is previewed against and
// a plan is authored against.
func TestTransformationReadersRefuseALinkToADocumentOfTheirKind(t *testing.T) {
	app, root, identity := transformWorkspace(t, `{"operator":"shift-dates/v1","shift":"24h"}`)
	writeDocument(t, root, "pack.json", fixture(t, "profile-pack.json"))
	plan := func(rules, profile, output string) desktop.TransformPlanRequest {
		return desktop.TransformPlanRequest{Workspace: root, Case: "incident", Identity: identity, Rules: rules, Profile: profile,
			Steps: []transform.Step{{Operator: transform.ShiftDates, Shift: "24h"}}, Output: output}
	}
	if saved := app.SaveTransformPlan(plan("rules.json", "pack.json", "pinned-plan.json")); saved.State != desktop.Completed {
		t.Fatalf("a plan pinning the pack: %+v", saved)
	}
	preview := func(rules, plan, profile string) refused {
		result := app.PreviewTransformation(desktop.TransformRequest{Workspace: root, Case: "incident", Identity: identity, Rules: rules, Plan: plan, Profile: profile})
		return refused{result.State, result.Reason}
	}
	refusesLinksToEntriesItAccepts(t, root, []ownReader{
		{"PreviewTransformation(Rules)", "rules.json", notOneRegularFile("the correlation rules document"), func(entry string) refused {
			return preview(entry, "plan.json", "")
		}},
		{"PreviewTransformation(Plan)", "plan.json", notOneRegularFile("the transformation plan"), func(entry string) refused {
			return preview("rules.json", entry, "")
		}},
		{"PreviewTransformation(Profile)", "pack.json", notOneRegularFile("the profile pack"), func(entry string) refused {
			return preview("rules.json", "pinned-plan.json", entry)
		}},
		{"SaveTransformPlan(Rules)", "rules.json", notOneRegularFile("the correlation rules document"), func(entry string) refused {
			result := app.SaveTransformPlan(plan(entry, "", "rules-plan.json"))
			return refused{result.State, result.Reason}
		}},
		{"SaveTransformPlan(Profile)", "pack.json", notOneRegularFile("the profile pack"), func(entry string) refused {
			result := app.SaveTransformPlan(plan("rules.json", entry, "profile-plan.json"))
			return refused{result.State, result.Reason}
		}},
		{"OpenTransformPlan", "plan.json", notOneRegularFile("the transformation plan"), func(entry string) refused {
			result := app.OpenTransformPlan(root, entry)
			return refused{result.State, result.Reason}
		}},
	})
}

// The configuration a diagnosis and a grouping run under, the configuration
// and finding decisions opened in their editors, and the normalization policy
// a comparison is read through.
func TestDiagnosisAndNormalizationReadersRefuseALinkToADocumentOfTheirKind(t *testing.T) {
	app, root, identity := diagnosisWorkspace(t)
	writeDocument(t, root, "diagnose.config.json",
		`{"schema":"readmit-diagnose-config/v1","profile":"readmit-siu-v1","ruleset":"readmit-siu-diagnosis/v1","rules":["ack.msa-outcome","ack.err-outcome"],"namespaces":[]}`)
	writeDocument(t, root, "verdicts.json", docFindingDecisions)
	writeDocument(t, root, "compare.policy.json", nrmPolicy)
	before := writeCase(t, root, "before", framed(cmpBookedBefore)+framed(cmpMovedBefore))
	writeCase(t, root, "after", framed(cmpBookedAfter)+framed(cmpInsertedAfter)+framed(cmpMovedAfter))
	configuration := notOneRegularFile("the diagnosis configuration")
	refusesLinksToEntriesItAccepts(t, root, []ownReader{
		{"RunDiagnosis(Config)", "diagnose.config.json", configuration, func(entry string) refused {
			result := app.RunDiagnosis(desktop.DiagnosisRequest{Workspace: root, Case: "acked", Identity: identity, Config: entry, Output: "diagnosed"})
			return refused{result.State, result.Reason}
		}},
		{"GroupDiagnoses(Config)", "diagnose.config.json", configuration, func(entry string) refused {
			result := app.GroupDiagnoses(desktop.GroupDiagnosesRequest{Workspace: root, Cases: []string{"acked"}, Config: entry})
			return refused{result.State, result.Reason}
		}},
		{"OpenDiagnoseConfig", "diagnose.config.json", configuration, func(entry string) refused {
			result := app.OpenDiagnoseConfig(root, entry)
			return refused{result.State, result.Reason}
		}},
		{"OpenFindingDecisions", "verdicts.json", notOneRegularFile("the finding decisions document"), func(entry string) refused {
			result := app.OpenFindingDecisions(root, entry)
			return refused{result.State, result.Reason}
		}},
		{"NormalizeCompare(Policy)", "compare.policy.json", notOneRegularFile("the normalization policy"), func(entry string) refused {
			request := normalizeRequest(root, before.Identity)
			request.Policy = entry
			result := app.NormalizeCompare(request)
			return refused{result.State, result.Reason}
		}},
		{"OpenNormalizationPolicy", "compare.policy.json", notOneRegularFile("the normalization policy"), func(entry string) refused {
			result := app.OpenNormalizationPolicy(root, entry)
			return refused{result.State, result.Reason}
		}},
	})
}

// The index a grid reads and the index a person names to describe.
func TestIndexReadersRefuseALinkToAnIndexOfTheCase(t *testing.T) {
	root := t.TempDir()
	app := workspaceApp(t)
	incident := writeCase(t, root, "incident", framed(gridBooking)+framed(gridAccepted))
	writeIndex(t, root, "incident.index.json", incident, nil)
	index := notOneRegularFile("an index")
	refusesLinksToEntriesItAccepts(t, root, []ownReader{
		{"OpenGrid(indexName)", "incident.index.json", index, func(entry string) refused {
			result := app.OpenGrid(root, "incident", entry, 0, 10)
			return refused{result.State, result.Reason}
		}},
		{"DescribeIndex(indexName)", "incident.index.json", index, func(entry string) refused {
			result := app.DescribeIndex(root, "incident", entry)
			return refused{result.State, result.Reason}
		}},
	})
}

// The correlation rules a reduction groups by, both when it is previewed and
// when it runs its trials against the acking peer.
func TestAReductionRefusesALinkToItsCorrelationRules(t *testing.T) {
	peer := newAckingPeer(t, "AA")
	root, _ := confinementWorkspaces(t, peer.address)
	app := workspaceApp(t)
	writeDocument(t, root, "rules.json", `{"schema":"readmit-correlation-rules/v1","authorities":[],"rules":[{"id":"message","operator":"control-id","scope":"source"}]}`)
	opened := app.OpenCase(root, "case")
	if opened.State != desktop.Completed || opened.Case == nil {
		t.Fatalf("open the case a reduction reduces: %+v", opened)
	}
	reduction := func(rules string) desktop.ReductionRequest {
		return desktop.ReductionRequest{Workspace: root, Case: "case", Identity: opened.Case.Identity,
			Spec: "booking.json", Rules: rules, Grouping: reduce.GroupByCorrelation, Assertions: []string{"ack"}, Trials: 4, Confirmations: 1,
			ResetPlan: "plan.json", Target: "target.json", Work: "reduction-work"}
	}
	rules := notOneRegularFile("the correlation rules document")
	refusesLinksToEntriesItAccepts(t, root, []ownReader{
		{"PreviewReduction(Rules)", "rules.json", rules, func(entry string) refused {
			result := app.PreviewReduction(reduction(entry))
			return refused{result.State, result.Reason}
		}},
		{"StartReduction(Rules)", "rules.json", rules, func(entry string) refused {
			result := app.StartReduction(reduction(entry))
			return refused{result.State, result.Reason}
		}},
	})
}

// Every document the profile panel reads — a pack, a local profile, a
// references index, a version seal, an origin and a package — and every
// scenario document and library the scenario panel reads by name.
func TestProfileAndScenarioReadersRefuseALinkToADocumentOfTheirKind(t *testing.T) {
	root := t.TempDir()
	app := workspaceApp(t)
	profile := fixture(t, "local-profile.json")
	for name, document := range map[string]string{
		"local-profile.json": profile, "profile-pack.json": fixture(t, "profile-pack.json"),
		"profile-version.json": fixture(t, "profile-version.json"), "profile-origin.json": fixture(t, "profile-origin.json"),
		"references.json": fixture(t, "profile-references.json"), "scenario.json": fixture(t, "scenario-siu.json"),
		"plan.json": fixture(t, "scenario-generator.json"), "library.json": fixture(t, "scenario-library.json"),
		"expectations.json": fixture(t, "scenario-expectations.json"),
		"profile-v2.json":   strings.Replace(profile, `"version": "1"`, `"version": "2"`, 1),
		// The profile scan reads every *.json entry, so the pack a profile names
		// here is under another name, where only naming it reads it.
		"pack.document": fixture(t, "profile-pack.json"),
	} {
		writeDocument(t, root, name, document)
	}
	exported := func(profile, pack, version, origin, output string) refused {
		result := app.ExportProfilePackage(desktop.ProfilePackageExportRequest{Workspace: root, Profile: profile, Pack: pack,
			Version: version, Origin: origin, Output: output, Reviewed: true})
		return refused{result.State, result.Reason}
	}
	if packaged := exported("local-profile.json", "profile-pack.json", "profile-version.json", "profile-origin.json", "package.json"); packaged.state != desktop.Completed {
		t.Fatalf("a package to import: %+v", packaged)
	}
	library := func(request desktop.ScenarioLibraryRequest) desktop.ScenarioLibraryRequest {
		request.Workspace, request.TemplateID, request.Profile = root, "siu-appointment-lifecycle", "readmit-siu-lifecycle-v1"
		return request
	}
	for _, version := range []string{"1", "2"} {
		if saved := app.SaveScenarioLibraryEntry(library(desktop.ScenarioLibraryRequest{Library: map[string]string{"1": "", "2": "versions.json"}[version],
			Output: "versions.json", TemplateVer: version, Plan: "plan.json"})); saved.State != desktop.Completed {
			t.Fatalf("a library holding version %s: %+v", version, saved)
		}
	}
	compare := func(from, to, references string) refused {
		result := app.CompareProfiles(desktop.ProfileCompareRequest{Workspace: root, From: from, To: to, References: references})
		return refused{result.State, result.Reason}
	}
	scenario := notOneRegularFile("the scenario document")
	refusesLinksToEntriesItAccepts(t, root, []ownReader{
		{"InspectProfilePack", "profile-pack.json", notOneRegularFile("the profile pack"), func(entry string) refused {
			result := app.InspectProfilePack(root, entry)
			return refused{result.State, result.Reason}
		}},
		{"OpenProfile(entry)", "local-profile.json", notOneRegularFile("the local profile"), func(entry string) refused {
			result := app.OpenProfile(root, entry, "pack.document")
			return refused{result.State, result.Reason}
		}},
		{"OpenProfile(packEntry)", "pack.document", fallsBack, func(entry string) refused {
			result := app.OpenProfile(root, "local-profile.json", entry)
			return refused{result.State, result.Reason}
		}},
		{"ValidateProfile(Pack)", "pack.document", notOneRegularFile("the profile pack"), func(entry string) refused {
			result := app.ValidateProfile(desktop.ProfileValidateRequest{Workspace: root, Document: profile, Pack: entry})
			return refused{result.State, result.Reason}
		}},
		{"CompareProfiles(From)", "local-profile.json", fallsBack, func(entry string) refused { return compare(entry, "profile-v2.json", "") }},
		{"CompareProfiles(To)", "profile-v2.json", fallsBack, func(entry string) refused { return compare("local-profile.json", entry, "") }},
		{"CompareProfiles(References)", "references.json", fallsBack, func(entry string) refused {
			return compare("local-profile.json", "profile-v2.json", entry)
		}},
		{"UpgradeProfilePin(References)", "references.json", notOneRegularFile("the references document"), func(entry string) refused {
			result := app.UpgradeProfilePin(desktop.ProfileUpgradePinRequest{Workspace: root, References: entry, Test: "test-reschedule.json",
				WasPin: profileversion.Pin{ID: "fixture-local-siu", Version: "1", SHA256: "e96a3350b728a78d063cf99afddeec3854d393682039ed4348fbc89057c55054"},
				NowPin: profileversion.Pin{ID: "fixture-local-siu", Version: "2", SHA256: "4444444444444444444444444444444444444444444444444444444444444444"},
				Output: "upgraded.json"})
			return refused{result.State, result.Reason}
		}},
		{"ExportProfilePackage(Profile)", "local-profile.json", notOneRegularFile("the local profile"), func(entry string) refused {
			return exported(entry, "profile-pack.json", "profile-version.json", "profile-origin.json", "package-profile.json")
		}},
		{"ExportProfilePackage(Pack)", "profile-pack.json", notOneRegularFile("the profile pack"), func(entry string) refused {
			return exported("local-profile.json", entry, "profile-version.json", "profile-origin.json", "package-pack.json")
		}},
		{"ExportProfilePackage(Version)", "profile-version.json", notOneRegularFile("the profile version seal"), func(entry string) refused {
			return exported("local-profile.json", "profile-pack.json", entry, "profile-origin.json", "package-version.json")
		}},
		{"ExportProfilePackage(Origin)", "profile-origin.json", notOneRegularFile("the profile origin"), func(entry string) refused {
			return exported("local-profile.json", "profile-pack.json", "profile-version.json", entry, "package-origin.json")
		}},
		{"ImportProfilePackage(Package)", "package.json", notOneRegularFile("the profile package"), func(entry string) refused {
			result := app.ImportProfilePackage(desktop.ProfilePackageImportRequest{Workspace: root, Package: entry, Output: "imported"})
			return refused{result.State, result.Reason}
		}},
		{"InspectProfilePackage", "package.json", notOneRegularFile("the profile package"), func(entry string) refused {
			result := app.InspectProfilePackage(root, entry)
			return refused{result.State, result.Reason}
		}},
		{"BindScenarioProfile(Entry)", "local-profile.json", notOneRegularFile("the local profile"), func(entry string) refused {
			result := app.BindScenarioProfile(desktop.ScenarioProfileBindRequest{Workspace: root, Entry: entry, PackEntry: "profile-pack.json"})
			return refused{result.State, result.Reason}
		}},
		{"PreviewScenario(Document)", "scenario.json", scenario, func(entry string) refused {
			result := app.PreviewScenario(desktop.ScenarioPreviewRequest{Workspace: root, Document: entry})
			return refused{result.State, result.Reason}
		}},
		{"OpenScenario", "scenario.json", scenario, func(entry string) refused {
			result := app.OpenScenario(root, entry)
			return refused{result.State, result.Reason}
		}},
		{"GenerateScenario(Document)", "plan.json", fallsBack, func(entry string) refused {
			result := app.GenerateScenario(desktop.ScenarioGenerateRequest{Workspace: root, Document: entry, OutputName: "generated"})
			return refused{result.State, result.Reason}
		}},
		{"OpenScenarioLibrary", "library.json", scenario, func(entry string) refused {
			result := app.OpenScenarioLibrary(root, entry)
			return refused{result.State, result.Reason}
		}},
		{"SaveScenarioLibraryEntry(Library)", "versions.json", scenario, func(entry string) refused {
			result := app.SaveScenarioLibraryEntry(library(desktop.ScenarioLibraryRequest{Library: entry, Output: "saved-library.json", TemplateVer: "3", Plan: "plan.json"}))
			return refused{result.State, result.Reason}
		}},
		{"SaveScenarioLibraryEntry(Plan)", "plan.json", fallsBack, func(entry string) refused {
			result := app.SaveScenarioLibraryEntry(library(desktop.ScenarioLibraryRequest{Output: "plan-library.json", TemplateVer: "1", Plan: entry}))
			return refused{result.State, result.Reason}
		}},
		{"CompareScenarioLibraryEntries(Library)", "versions.json", scenario, func(entry string) refused {
			result := app.CompareScenarioLibraryEntries(library(desktop.ScenarioLibraryRequest{Library: entry, TemplateVer: "1", Expectations: "2"}))
			return refused{result.State, result.Reason}
		}},
		{"CheckScenarioLibrary(Library)", "library.json", scenario, func(entry string) refused {
			result := app.CheckScenarioLibrary(desktop.ScenarioLibraryRequest{Workspace: root, Library: entry, Expectations: "expectations.json"})
			return refused{result.State, result.Reason}
		}},
		{"CheckScenarioLibrary(Expectations)", "expectations.json", scenario, func(entry string) refused {
			result := app.CheckScenarioLibrary(desktop.ScenarioLibraryRequest{Workspace: root, Library: "library.json", Expectations: entry})
			return refused{result.State, result.Reason}
		}},
		{"ExportScenarioLibrary(Library)", "library.json", scenario, func(entry string) refused {
			result := app.ExportScenarioLibrary(desktop.ScenarioLibraryRequest{Workspace: root, Library: entry, Output: "exported-library.json"})
			return refused{result.State, result.Reason}
		}},
	})
}

// Every suite document, release references document, prepared suite and
// coverage document the suite panels read; every specification, baseline,
// release and profile a baseline or a release is reviewed, approved or opened
// from; and the saved test and assertion set a person imports into an editor.
func TestSuiteBaselineAndImportReadersRefuseALinkToAnEntryOfTheirKind(t *testing.T) {
	app, root := releaseWorkspace(t)
	raw := []byte(read(t, filepath.Join(root, "booking.json")))
	first, err := expectation.Read(filepath.Join(root, "release-1.json"))
	if err != nil {
		t.Fatal(err)
	}
	changed := []byte(replaceOnce(t, string(raw), `"AA"`, `"AE"`))
	commitment, err := expectation.Review("booking", changed, []profileversion.Version{}, &first, false)
	if err != nil {
		t.Fatal(err)
	}
	second, err := expectation.Approve("booking", changed, []profileversion.Version{}, &first, commitment.Identity, "reviewer", "changed")
	if err != nil {
		t.Fatal(err)
	}
	if err = expectation.Save(filepath.Join(root, "release-2.json"), second); err != nil {
		t.Fatal(err)
	}
	for _, output := range []string{"earlier", "prepared"} {
		if prepared := app.PrepareSuite(desktop.SuitePrepareRequest{Workspace: root, Entry: "suite.json", Environment: "east", Output: output}); prepared.State != desktop.Completed {
			t.Fatalf("a prepared suite: %+v", prepared)
		}
	}
	if authored := app.SaveSuiteCoverage(desktop.SuiteCoverageSaveRequest{Workspace: root, Prepared: "prepared",
		Requirements: []suite.Requirement{{ID: "accept-booking", Jobs: []string{"booking-one"}}}, Output: "coverage.json"}); authored.State != desktop.Completed {
		t.Fatalf("a coverage document: %+v", authored)
	}
	promotion := func(entry, releases string) desktop.SuitePromotionRequest {
		return desktop.SuitePromotionRequest{Workspace: root, Entry: entry, Environment: "east", Releases: releases, Revision: "fixture-build-7"}
	}
	reviewed := app.ReviewSuitePromotion(promotion("suite.json", "releases.json"))
	if reviewed.State != desktop.Completed || reviewed.Review == nil {
		t.Fatalf("a promotion review: %+v", reviewed)
	}
	approve := func(entry, releases, output string) refused {
		review := promotion(entry, releases)
		result := app.ApproveSuitePromotion(desktop.SuitePromotionApproveRequest{Workspace: root, Entry: review.Entry, Environment: review.Environment,
			Releases: review.Releases, Revision: review.Revision, Reviewed: reviewed.Review.Identity(), Approver: "Local reviewer", Rationale: "reviewed", Output: output})
		return refused{result.State, result.Reason}
	}
	writeDocument(t, root, "profile.json", fixture(t, "local-profile.json"))
	writeDocument(t, root, "assertions.json", fixture(t, "assertion-set.json"))
	// A baseline and a release each reviewed, and approved once, so that
	// approving again and reviewing against the previous one are real.
	baseline := func(request desktop.BaselineRequest) desktop.BaselineRequest {
		request.Workspace, request.Approver, request.Rationale = root, "reviewer", "fixture"
		if request.Release {
			request.ReleaseID = "booking"
		}
		if review := app.ReviewBaseline(request); review.State == desktop.Completed {
			request.Review = review.Comparison.Identity
		}
		return request
	}
	for _, request := range []desktop.BaselineRequest{
		{Spec: "booking.json", Output: "baseline.json"},
		{Spec: "booking.json", Release: true, Profiles: []string{"profile.json"}, Output: "release-3.json"},
	} {
		if approved := app.ApproveBaseline(baseline(request)); approved.State != desktop.Completed {
			t.Fatalf("an approved baseline: %+v", approved)
		}
	}
	reviewBaseline := func(request desktop.BaselineRequest) refused {
		result := app.ReviewBaseline(baseline(request))
		return refused{result.State, result.Reason}
	}
	approveBaseline := func(request desktop.BaselineRequest, entry string, set func(*desktop.BaselineRequest, string)) refused {
		request = baseline(request)
		set(&request, entry)
		result := app.ApproveBaseline(request)
		return refused{result.State, result.Reason}
	}
	spec := func(r *desktop.BaselineRequest, entry string) { r.Spec = entry }
	previous := func(r *desktop.BaselineRequest, entry string) { r.Previous = entry }
	profiles := func(r *desktop.BaselineRequest, entry string) { r.Profiles = []string{entry} }
	impact := func(set func(*desktop.SuiteImpactRequest)) refused {
		request := desktop.SuiteImpactRequest{Workspace: root, Suite: "suite.json", Releases: "releases.json", From: "release-1.json", To: "release-2.json"}
		set(&request)
		result := app.ExpectationImpact(request)
		return refused{result.State, result.Reason}
	}
	coverage := func(prepared, requirements string, previous []string) refused {
		result := app.AssessSuiteCoverage(desktop.SuiteCoverageAssessRequest{Workspace: root, Prepared: prepared, Requirements: requirements, Previous: previous, At: "2026-09-19T00:00:00Z"})
		return refused{result.State, result.Reason}
	}
	suiteInput := []string{"suite input must be a bounded regular file"}
	baselineInput := []string{"baseline input must be a readable regular file, not a symlink"}
	prepared := []string{"the prepared suite must be one directory entry of the open workspace"}
	refusesLinksToEntriesItAccepts(t, root, []ownReader{
		{"OpenSuite", "suite.json", notOneRegularFile("the suite document"), func(entry string) refused {
			result := app.OpenSuite(root, entry)
			return refused{result.State, result.Reason}
		}},
		{"PreviewSuite(Entry)", "suite.json", notOneRegularFile("the suite document"), func(entry string) refused {
			result := app.PreviewSuite(desktop.SuitePreviewRequest{Workspace: root, Entry: entry, Environment: "east"})
			return refused{result.State, result.Reason}
		}},
		{"PreviewSuite(Releases)", "releases.json", notOneRegularFile("the suite release references"), func(entry string) refused {
			result := app.PreviewSuite(desktop.SuitePreviewRequest{Workspace: root, Entry: "suite.json", Environment: "east", Releases: entry})
			return refused{result.State, result.Reason}
		}},
		{"PrepareSuite(Entry)", "suite.json", suiteInput, func(entry string) refused {
			result := app.PrepareSuite(desktop.SuitePrepareRequest{Workspace: root, Entry: entry, Environment: "east", Output: "prepared-entry"})
			return refused{result.State, result.Reason}
		}},
		{"PrepareSuite(Releases)", "releases.json", suiteInput, func(entry string) refused {
			result := app.PrepareSuite(desktop.SuitePrepareRequest{Workspace: root, Entry: "suite.json", Environment: "east", Releases: entry, Output: "prepared-releases"})
			return refused{result.State, result.Reason}
		}},
		{"ReviewSuitePromotion(Entry)", "suite.json", suiteInput, func(entry string) refused {
			result := app.ReviewSuitePromotion(promotion(entry, "releases.json"))
			return refused{result.State, result.Reason}
		}},
		{"ReviewSuitePromotion(Releases)", "releases.json", suiteInput, func(entry string) refused {
			result := app.ReviewSuitePromotion(promotion("suite.json", entry))
			return refused{result.State, result.Reason}
		}},
		{"ApproveSuitePromotion(Entry)", "suite.json", suiteInput, func(entry string) refused {
			return approve(entry, "releases.json", "promotion-entry.json")
		}},
		{"ApproveSuitePromotion(Releases)", "releases.json", suiteInput, func(entry string) refused {
			return approve("suite.json", entry, "promotion-releases.json")
		}},
		{"SaveSuiteCoverage(Prepared)", "prepared", prepared, func(entry string) refused {
			result := app.SaveSuiteCoverage(desktop.SuiteCoverageSaveRequest{Workspace: root, Prepared: entry,
				Requirements: []suite.Requirement{{ID: "accept-booking", Jobs: []string{"booking-one"}}}, Output: "coverage-again.json"})
			return refused{result.State, result.Reason}
		}},
		{"AssessSuiteCoverage(Prepared)", "prepared", prepared, func(entry string) refused { return coverage(entry, "coverage.json", nil) }},
		{"AssessSuiteCoverage(Previous)", "earlier", prepared, func(entry string) refused {
			return coverage("prepared", "coverage.json", []string{entry})
		}},
		{"AssessSuiteCoverage(Requirements)", "coverage.json", notOneRegularFile("the suite coverage document"), func(entry string) refused {
			return coverage("prepared", entry, nil)
		}},
		{"ExpectationImpact(Suite)", "suite.json", suiteInput, func(entry string) refused {
			return impact(func(r *desktop.SuiteImpactRequest) { r.Suite = entry })
		}},
		{"ExpectationImpact(Releases)", "releases.json", suiteInput, func(entry string) refused {
			return impact(func(r *desktop.SuiteImpactRequest) { r.Releases = entry })
		}},
		{"ExpectationImpact(From)", "release-1.json", baselineInput, func(entry string) refused {
			return impact(func(r *desktop.SuiteImpactRequest) { r.From = entry })
		}},
		{"ExpectationImpact(To)", "release-2.json", baselineInput, func(entry string) refused {
			return impact(func(r *desktop.SuiteImpactRequest) { r.To = entry })
		}},
		{"ReviewBaseline(Spec)", "booking.json", baselineInput, func(entry string) refused {
			return reviewBaseline(desktop.BaselineRequest{Spec: entry})
		}},
		{"ReviewBaseline(Previous)", "baseline.json", baselineInput, func(entry string) refused {
			return reviewBaseline(desktop.BaselineRequest{Spec: "booking.json", Previous: entry})
		}},
		{"ApproveBaseline(Spec)", "booking.json", baselineInput, func(entry string) refused {
			return approveBaseline(desktop.BaselineRequest{Spec: "booking.json", Output: "baseline-spec.json"}, entry, spec)
		}},
		{"ApproveBaseline(Previous)", "baseline.json", baselineInput, func(entry string) refused {
			return approveBaseline(desktop.BaselineRequest{Spec: "booking.json", Previous: "baseline.json", Output: "baseline-previous.json"}, entry, previous)
		}},
		{"OpenBaseline(Previous)", "baseline.json", baselineInput, func(entry string) refused {
			result := app.OpenBaseline(desktop.BaselineRequest{Workspace: root, Previous: entry})
			return refused{result.State, result.Reason}
		}},
		{"ReviewBaseline(Spec) of a release", "booking.json", baselineInput, func(entry string) refused {
			return reviewBaseline(desktop.BaselineRequest{Release: true, Spec: entry})
		}},
		{"ReviewBaseline(Previous) of a release", "release-3.json", baselineInput, func(entry string) refused {
			return reviewBaseline(desktop.BaselineRequest{Release: true, Spec: "booking.json", Previous: entry})
		}},
		{"ReviewBaseline(Profiles) of a release", "profile.json", baselineInput, func(entry string) refused {
			return reviewBaseline(desktop.BaselineRequest{Release: true, Spec: "booking.json", Profiles: []string{entry}})
		}},
		{"ApproveBaseline(Spec) of a release", "booking.json", baselineInput, func(entry string) refused {
			return approveBaseline(desktop.BaselineRequest{Release: true, Spec: "booking.json", Output: "release-spec.json"}, entry, spec)
		}},
		{"ApproveBaseline(Previous) of a release", "release-3.json", baselineInput, func(entry string) refused {
			return approveBaseline(desktop.BaselineRequest{Release: true, Spec: "booking.json", Previous: "release-3.json", Profiles: []string{"profile.json"}, Output: "release-previous.json"}, entry, previous)
		}},
		{"ApproveBaseline(Profiles) of a release", "profile.json", baselineInput, func(entry string) refused {
			return approveBaseline(desktop.BaselineRequest{Release: true, Spec: "booking.json", Profiles: []string{"profile.json"}, Output: "release-profiles.json"}, entry, profiles)
		}},
		{"OpenBaseline(Previous) of a release", "release-3.json", baselineInput, func(entry string) refused {
			result := app.OpenBaseline(desktop.BaselineRequest{Workspace: root, Release: true, Previous: entry})
			return refused{result.State, result.Reason}
		}},
		{"PostHubReleaseReview(Entry)", "release-1.json", notOneRegularFile("the released expectation"), func(entry string) refused {
			return postedWithoutAHub(app.PostHubReleaseReview(desktop.HubReleaseReviewRequest{Project: "cardio-study", Workspace: root, Entry: entry,
				Kind: "approval", ID: "release-approval", Text: "reviewed"}))
		}},
		{"ImportTest", "booking.json", []string{"cannot read a bounded regular test spec"}, func(entry string) refused {
			result := app.ImportTest(root, entry)
			return refused{result.State, result.Reason}
		}},
		{"ImportAssertionSet", "assertions.json", []string{"that entry could not be read"}, func(entry string) refused {
			result := app.ImportAssertionSet(root, entry)
			return refused{result.State, result.Reason}
		}},
	})
}

// previewedPacket answers one packet preview as a refusal when the preview
// reports a problem with its inputs, since a preview states what it could not
// verify as a problem beside the rest of what it found, rather than failing.
func previewedPacket(app *desktop.App, request desktop.PacketRequest) refused {
	result := app.PreviewPacket(request)
	if result.State == desktop.Completed && result.Preview != nil && len(result.Preview.Problems) > 0 {
		return refused{desktop.Failed, strings.Join(result.Preview.Problems, "; ")}
	}
	return refused{result.State, result.Reason}
}

// Every retained folder a packet, a portable review, a transfer package or a
// run's own reader opens: the case and the executions a packet is previewed
// and assembled from, the packet a review is exported from and reopened, the
// package inspected, opened and discarded, and a run reopened and read for
// progress.
func TestEvidenceFolderReadersRefuseALinkToAFolderOfTheirKind(t *testing.T) {
	app := newApp(t, &chooser{})
	root, spec, baseline, current := packetWorkspace(t, app)
	assembled := app.AssemblePacket(packetRequest(root, spec, baseline, current))
	if assembled.State != desktop.Completed || assembled.Packet == nil {
		t.Fatalf("a packet: %+v", assembled)
	}
	packet := assembled.Packet.Entry
	if exported := app.ExportPacketReview(desktop.PacketExportRequest{Workspace: root, Packet: packet, Destination: filepath.Join(root, "review")}); exported.State != desktop.Completed {
		t.Fatalf("a portable review: %+v", exported)
	}
	if saved := app.SaveProtectionControl(desktop.ProtectionControlRequest{
		Workspace: root, Entry: "protection.json", Name: "lab-evidence", Storage: "os-volume-encryption",
		Command: keyProgram(t, "test-only-not-a-real-key-4f8c1d2e6b0a9357", ""), Arguments: []string{"find-generic-password", "-w", "-s", "readmit-lab-key"},
		MaxAge: "720h", Retain: "1h",
	}); saved.State != desktop.Completed {
		t.Fatalf("a protection control: %+v", saved)
	}
	writeDocument(t, root, "evidence.txt", "synthetic retained evidence bytes")
	for _, output := range []string{"transfer", "doomed"} {
		if packed := app.PackProtectedPackage(desktop.ProtectionPackRequest{Workspace: root, Entry: "protection.json", Control: "lab-evidence", Sources: []string{"evidence.txt"}, Output: output}); packed.State != desktop.Completed {
			t.Fatalf("a transfer package: %+v", packed)
		}
	}
	open := func(control, entry, output string) refused {
		result := app.OpenProtectedPackage(desktop.ProtectionOpenRequest{Workspace: root, Entry: "protection.json", Control: control, Package: entry, Output: output})
		return refused{result.State, result.Reason}
	}
	caseRefusal := []string{"the case is not a case bundle this release verifies"}
	runRefusal := []string{"the entry is not a retained execution this release verifies"}
	storage := []string{"report evidence must be bounded regular files without symlinks or empty directories"}
	directory := []string{"artifact must be a regular directory"}
	refusesLinksToEntriesItAccepts(t, root, []ownReader{
		{"PreviewPacket(Case)", "case", caseRefusal, func(entry string) refused {
			request := packetRequest(root, spec, baseline, current)
			request.Case = entry
			return previewedPacket(app, request)
		}},
		{"PreviewPacket(BaselineCase)", "case", caseRefusal, func(entry string) refused {
			request := packetRequest(root, spec, baseline, current)
			request.BaselineCase = entry
			return previewedPacket(app, request)
		}},
		{"PreviewPacket(Current)", current, runRefusal, func(entry string) refused {
			return previewedPacket(app, packetRequest(root, spec, baseline, entry))
		}},
		{"PreviewPacket(Baseline)", baseline, runRefusal, func(entry string) refused {
			return previewedPacket(app, packetRequest(root, spec, entry, current))
		}},
		{"AssemblePacket(Case)", "case", storage, func(entry string) refused {
			request := packetRequest(root, spec, baseline, current)
			request.Case = entry
			result := app.AssemblePacket(request)
			return refused{result.State, result.Reason}
		}},
		{"AssemblePacket(BaselineCase)", "case", storage, func(entry string) refused {
			request := packetRequest(root, spec, baseline, current)
			request.BaselineCase = entry
			result := app.AssemblePacket(request)
			return refused{result.State, result.Reason}
		}},
		{"AssemblePacket(Current)", current, storage, func(entry string) refused {
			result := app.AssemblePacket(packetRequest(root, spec, baseline, entry))
			return refused{result.State, result.Reason}
		}},
		{"AssemblePacket(Baseline)", baseline, storage, func(entry string) refused {
			result := app.AssemblePacket(packetRequest(root, spec, entry, current))
			return refused{result.State, result.Reason}
		}},
		{"OpenPacket", packet, directory, func(entry string) refused {
			result := app.OpenPacket(root, entry)
			return refused{result.State, result.Reason}
		}},
		{"ExportPacketReview(Packet)", packet, directory, func(entry string) refused {
			result := app.ExportPacketReview(desktop.PacketExportRequest{Workspace: root, Packet: entry, Destination: filepath.Join(root, "exported-"+entry)})
			return refused{result.State, result.Reason}
		}},
		{"OpenPacketReview", "review", directory, func(entry string) refused {
			result := app.OpenPacketReview(desktop.PacketReviewRequest{Workspace: root, Entry: entry})
			return refused{result.State, result.Reason}
		}},
		{"OpenRunEvidence", current, []string{"that entry is not a retained execution this release verifies"}, func(entry string) refused {
			result := app.OpenRunEvidence(desktop.RunEvidenceRequest{Workspace: root, Entry: entry})
			return refused{result.State, result.Reason}
		}},
		{"DurableRunProgress", current, []string{"the durable run could not be verified; partial evidence has not been changed"}, func(entry string) refused {
			result := app.DurableRunProgress(root, entry)
			return refused{result.State, result.Reason}
		}},
		{"InspectProtectedPackage", "transfer", []string{"this directory is not a transfer package this release inspects; its descriptor must be readable without a key"}, func(entry string) refused {
			result := app.InspectProtectedPackage(root, entry)
			return refused{result.State, result.Reason}
		}},
		{"OpenProtectedPackage(Package) under its own control", "transfer", []string{"this directory is not a transfer package this release opens"}, func(entry string) refused {
			return open("", entry, "opened-own-"+entry)
		}},
		{"OpenProtectedPackage(Package) under a named control", "transfer", []string{"a transfer package must be a regular directory"}, func(entry string) refused {
			return open("lab-evidence", entry, "opened-named-"+entry)
		}},
		{"DiscardProtectedPackage(Package)", "doomed", []string{"a transfer package must be a regular directory"}, func(entry string) refused {
			result := app.DiscardProtectedPackage(desktop.ProtectionDiscardRequest{Workspace: root, Package: entry, Override: true})
			return refused{result.State, result.Reason}
		}},
	})
}

// postedWithoutAHub answers a hub review posted with no hub connected as
// completed when that is all that refused it, which it is only once it has
// read and decoded the entry it names.
func postedWithoutAHub(result desktop.HubReviewsResult) refused {
	if result.State == desktop.Failed && result.Reason == "not connected to customer hub" {
		return refused{desktop.Completed, ""}
	}
	return refused{result.State, result.Reason}
}

// Every entry a privacy review is derived and exported from, the sharing
// policy and the source a support summary is prepared, published and posted
// from, and the support bundle verified and posted.
func TestPrivacyAndSupportReadersRefuseALinkToAnEntryOfTheirKind(t *testing.T) {
	app := workspaceApp(t)
	root, review, _ := readyReview(t, app)
	if saved := app.SaveSharingPolicy(desktop.SupportPolicyRequest{Workspace: root, Output: "sharing.json", Support: true,
		Destinations: []string{"local-file"}, MaxBytes: 4096}); saved.State != desktop.Completed {
		t.Fatalf("a sharing policy: %+v", saved)
	}
	support := func(source, policy string) desktop.SupportRequest {
		return desktop.SupportRequest{Workspace: root, Source: source, Kind: "derived-review", Private: review.Private, Policy: policy}
	}
	preview := app.PreviewSupportSummary(support(review.Review, "sharing.json"))
	if preview.State != desktop.Completed || preview.Summary == nil {
		t.Fatalf("a support summary: %+v", preview)
	}
	publish := func(source, policy, output string) refused {
		request := support(source, policy)
		result := app.PublishSupportSummary(desktop.SupportPublishRequest{Workspace: root, Source: request.Source, Kind: request.Kind,
			Private: request.Private, Policy: request.Policy, Approval: preview.Summary.Identity, Output: output})
		return refused{result.State, result.Reason}
	}
	if published := publish(review.Review, "sharing.json", "support"); published.state != desktop.Completed {
		t.Fatalf("a published support bundle: %+v", published)
	}
	derive := func(set func(*desktop.PrivacyReviewRequest), output string) refused {
		request := privacyRequest(root, "policy.json")
		request.Output, request.LocalState = output, output+"-private"
		set(&request)
		result := app.DeriveExportReview(request)
		return refused{result.State, result.Reason}
	}
	postSupport := func(entry, kind string) refused {
		return postedWithoutAHub(app.PostHubSupportReview(desktop.HubSupportReviewRequest{Project: "cardio-study", Workspace: root, Entry: entry,
			Kind: kind, ID: "sup-" + kind, Recipient: "reviewer@hospital.org"}))
	}
	sharingPolicy := notOneRegularFile("the sharing policy")
	published := []string{"the published support bundle must be one directory entry of the open workspace"}
	source := []string{"the share operation refused this preparation: the source kind, the policy's support decision, the destination bound or a source that does not verify"}
	refusesLinksToEntriesItAccepts(t, root, []ownReader{
		{"DeriveExportReview(Case)", "original.case", []string{"bundle must be a regular directory"}, func(entry string) refused {
			return derive(func(r *desktop.PrivacyReviewRequest) { r.Case = entry }, "derived-case")
		}},
		{"DeriveExportReview(Spec)", "spec.json", notOneRegularFile("the original specification"), func(entry string) refused {
			return derive(func(r *desktop.PrivacyReviewRequest) { r.Spec = entry }, "derived-spec")
		}},
		{"DeriveExportReview(Policy)", "policy.json", notOneRegularFile("the disclosure policy"), func(entry string) refused {
			return derive(func(r *desktop.PrivacyReviewRequest) { r.Policy = entry }, "derived-policy")
		}},
		{"DeriveExportReview(Inventory)", "inventory.json", notOneRegularFile("the original-artifact inventory"), func(entry string) refused {
			return derive(func(r *desktop.PrivacyReviewRequest) { r.Inventory = entry }, "derived-inventory")
		}},
		{"ExportDerivedPacket(Review)", review.Review, []string{"artifact must be a regular directory"}, func(entry string) refused {
			result := app.ExportDerivedPacket(desktop.PrivacyExportRequest{Workspace: root, Review: entry, LocalState: review.Private,
				Approval: review.Identity, Output: "exported-" + entry})
			return refused{result.State, result.Reason}
		}},
		{"ReadSharingPolicy", "sharing.json", sharingPolicy, func(entry string) refused {
			result := app.ReadSharingPolicy(root, entry)
			return refused{result.State, result.Reason}
		}},
		{"PreviewSupportSummary(Source)", review.Review, source, func(entry string) refused {
			result := app.PreviewSupportSummary(support(entry, "sharing.json"))
			return refused{result.State, result.Reason}
		}},
		{"PreviewSupportSummary(Policy)", "sharing.json", sharingPolicy, func(entry string) refused {
			result := app.PreviewSupportSummary(support(review.Review, entry))
			return refused{result.State, result.Reason}
		}},
		{"PublishSupportSummary(Source)", review.Review, source, func(entry string) refused {
			return publish(entry, "sharing.json", "support-source")
		}},
		{"PublishSupportSummary(Policy)", "sharing.json", sharingPolicy, func(entry string) refused {
			return publish(review.Review, entry, "support-policy")
		}},
		{"VerifySupportBundle", "support", []string{"this directory is not a complete support bundle this release verifies; a bundle missing, holding or hiding anything beyond its three members is refused"}, func(entry string) refused {
			result := app.VerifySupportBundle(root, entry)
			return refused{result.State, result.Reason}
		}},
		{"PostHubSupportReview(Entry) of a support policy", "sharing.json", sharingPolicy, func(entry string) refused {
			return postSupport(entry, "support-policy")
		}},
		{"PostHubSupportReview(Entry) of a support request", "support", published, func(entry string) refused {
			return postSupport(entry, "support-request")
		}},
		{"PostHubSupportReview(Entry) of a support approval", "support", published, func(entry string) refused {
			return postSupport(entry, "support-approval")
		}},
	})
}

// The target a test is authored against, which the workspace's own listing of
// targets offers, and the approval a comparison of two runs is read beside.
func TestAuthoringAndComparisonReadersRefuseALinkToAnEntryOfTheirKind(t *testing.T) {
	app, root := guided(t)
	root = resolved(t, root)
	spec := authorGuidedTest(t, app, root, "guided-test.json", 1)
	for output, trial := range map[string]string{"broken": guide.StepBaseline, "fixed": guide.StepPostFix} {
		if practiced := app.RunPractice(desktop.PracticeRequest{Workspace: root, Spec: spec, Trial: trial, Output: output}); practiced.State != desktop.Completed {
			t.Fatalf("a practice run: %+v", practiced)
		}
		if err := os.Rename(filepath.Join(root, output, "result"), filepath.Join(root, output+"-result")); err != nil {
			t.Fatal(err)
		}
	}
	baseline := desktop.BaselineRequest{Workspace: root, Spec: spec, Approver: "reviewer", Rationale: "fixture", Output: "approval.json"}
	if reviewed := app.ReviewBaseline(baseline); reviewed.State == desktop.Completed {
		baseline.Review = reviewed.Comparison.Identity
	}
	if approved := app.ApproveBaseline(baseline); approved.State != desktop.Completed {
		t.Fatalf("an approval: %+v", approved)
	}
	opened := app.OpenCase(root, guide.CaseName)
	if opened.State != desktop.Completed || opened.Case == nil {
		t.Fatalf("open the sample case: %+v", opened)
	}
	request := desktop.TestRequest{Workspace: root, Case: guide.CaseName, Identity: opened.Case.Identity}
	for _, answer := range []testauthor.Answer{
		{Stage: testauthor.StageName, Name: "Rescheduling updates the original appointment"},
		{Stage: testauthor.StageMessages, Messages: []string{"s0001-e000001", "s0001-e000002"}},
		{Stage: testauthor.StageTarget, Target: guide.TargetName},
		{Stage: testauthor.StageBoundary, Boundary: testrunner.LedgerBoundary},
		{Stage: testauthor.StageObservation, Observation: "practice-observation.json"},
		{Stage: testauthor.StageReset, Reset: "Start a fresh practice receiver with an empty ledger before each run."},
	} {
		request.Answer = answer
		authored := app.AuthorTest(request)
		if authored.Test == nil {
			t.Fatalf("%s: %+v", answer.Stage, authored)
		}
		request.Draft, request.Answer = authored.Test.Draft, testauthor.Answer{}
	}
	request.Suggest = &testauthor.SuggestionRequest{Result: "fixed-result", Ledger: true}
	proposed := app.SuggestExpectations(request)
	if proposed.State != desktop.Completed || proposed.Test == nil || proposed.Test.Suggestions == nil || len(proposed.Test.Suggestions.Suggestions) == 0 {
		t.Fatalf("proposed expectations: %+v", proposed)
	}
	set := proposed.Test.Suggestions
	review := &testauthor.Review{Result: "fixed-result", Identity: set.Origin.Identity,
		Decisions: []testauthor.Decision{{Suggestion: set.Suggestions[0].ID, Approved: true}}}
	// targeting names entry as the draft's target, or answers the target stage
	// with it.
	targeting := func(entry string, answer bool) desktop.TestRequest {
		named := request
		if answer {
			named.Answer = testauthor.Answer{Stage: testauthor.StageTarget, Target: entry}
		} else {
			named.Draft.Target = entry
		}
		return named
	}
	target := []string{"a target is one entry of the open workspace declaring a target configuration this release reads"}
	refusesLinksToEntriesItAccepts(t, root, []ownReader{
		{"AuthorTest(Answer.Target)", guide.TargetName, target, func(entry string) refused {
			named := targeting(entry, true)
			named.Suggest = nil
			result := app.AuthorTest(named)
			return refused{result.State, result.Reason}
		}},
		{"AuthorTest(Draft.Target)", guide.TargetName, target, func(entry string) refused {
			named := targeting(entry, false)
			named.Suggest = nil
			result := app.AuthorTest(named)
			return refused{result.State, result.Reason}
		}},
		{"SuggestExpectations(Draft.Target)", guide.TargetName, target, func(entry string) refused {
			result := app.SuggestExpectations(targeting(entry, false))
			return refused{result.State, result.Reason}
		}},
		{"SuggestExpectations(Suggest.Result)", "fixed-result", []string{"a reviewed run is one directory entry of the open workspace"}, func(entry string) refused {
			named := request
			named.Suggest = &testauthor.SuggestionRequest{Result: entry, Ledger: true}
			result := app.SuggestExpectations(named)
			return refused{result.State, result.Reason}
		}},
		{"ApproveExpectations(Draft.Target)", guide.TargetName, target, func(entry string) refused {
			named := targeting(entry, false)
			named.Review = review
			result := app.ApproveExpectations(named)
			return refused{result.State, result.Reason}
		}},
		{"SaveTest(Draft.Target)", guide.TargetName, target, func(entry string) refused {
			named := targeting(entry, false)
			named.Suggest = nil
			named.Draft.Expectations = []testauthor.Expectation{{ID: "one-appointment", Operator: testauthor.LedgerCount, Count: set.Suggestions[0].Count}}
			named.Output = "saved-" + filepath.Base(entry)
			result := app.SaveTest(named)
			return refused{result.State, result.Reason}
		}},
		{"CompareRuns(Approval)", "approval.json", []string{"comparison requires verified workspace results or durable runs, an optional valid approval, and distinct complete repeat samples"}, func(entry string) refused {
			result := app.CompareRuns(desktop.RunComparisonRequest{Workspace: root, Baseline: "broken-result", Current: "fixed-result", Approval: entry})
			return refused{result.State, result.Reason}
		}},
	})
}

// The cases a project registers as a revision and the case a revision is
// derived from, which the engine's own registration reads. (A case's own
// registration is a save of its details, of a case the catalog discovered,
// and the catalog never discovers a link as a case.)
func TestProjectRegistrationRefusesALinkToACaseOfTheProject(t *testing.T) {
	parent := t.TempDir()
	app := newApp(t, &chooser{folder: parent})
	root, _ := createdProject(t, app, parent)
	message := func(control string) string {
		return framed("MSH|^~\\&|Scheduling|Acme|EHR|Acme|20260101120000||SIU^S12|" + control + "|P|2.5.1\r")
	}
	for name, control := range map[string]string{"incident-4821": "1", "another": "2"} {
		writeCase(t, root, name, message(control))
	}
	// A revision is derived evidence, as a reproducer build writes it.
	for name, control := range map[string]string{"revised": "3", "revised-again": "4"} {
		if _, err := bundle.Write(filepath.Join(root, name), []bundle.Input{{Data: []byte(message(control)), Options: hl7.Options{Format: hl7.MLLP, Terminator: hl7.CR}}},
			bundle.Provenance{Mode: bundle.Derived, Derivation: "readmit-reproducer/v1"}); err != nil {
			t.Fatal(err)
		}
	}
	registerCase(t, root, "incident-4821", "Duplicate appointment after reschedule")
	projectCase := []string{"a case must be named by one directory entry of the project"}
	refusesLinksToEntriesItAccepts(t, root, []ownReader{
		{"RegisterRevision(Name)", "revised", projectCase, func(entry string) refused {
			result := app.RegisterRevision(desktop.RevisionRegistration{Workspace: root, Name: entry, Parent: "incident-4821"})
			return refused{result.State, result.Reason}
		}},
		{"RegisterRevision(Parent)", "incident-4821", projectCase, func(entry string) refused {
			result := app.RegisterRevision(desktop.RevisionRegistration{Workspace: root, Name: "revised-again", Parent: entry})
			return refused{result.State, result.Reason}
		}},
	})
}
