package desktop_test

import (
	"bytes"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/casegen"
	"github.com/bharm16/readmit/internal/cli"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/scenariogen"
	"github.com/bharm16/readmit/internal/testlicense"
)

func casegenFixture(t *testing.T, name string) []byte {
	t.Helper()
	return mustRead(t, filepath.Join("..", "..", "testdata", "casegen", name))
}

// generationProject is a project holding the owned SIU profile and pack and a
// saved scenario plan of the booking, reschedule and cancellation workflow.
func generationProject(t *testing.T, rows []scenariogen.Row, variants []scenariogen.Variant) (*desktop.App, desktop.RequestContext, desktop.ItemRef, desktop.ItemRef, desktop.CaseGenerationSettings) {
	t.Helper()
	app, context := namedProject(t)
	writeDocument(t, context.Project, "pack.json", string(casegenFixture(t, "owned/pack.json")))
	writeDocument(t, context.Project, "profile.json", string(casegenFixture(t, "owned/profile-siu.json")))
	profiles := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.ProfileItem})
	var profile desktop.ItemRef
	for _, item := range profiles.Page.Items {
		if item.Summary.Profile != nil && item.Summary.Profile.Form == "local-profile" {
			profile = item.Ref
		}
	}
	if profile.ID == "" {
		t.Fatalf("the local profile is not listed: %+v", profiles)
	}
	plan := scenariogen.Plan{Schema: scenariogen.Schema, GeneratorVersion: scenariogen.Version, Seed: 7, Template: casegenFixture(t, "scenario-book-reschedule-cancel.json"), Rows: rows, Variants: variants}
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.ScenarioItem, Draft: desktop.ItemDraft{Name: "Book, move, cancel", Scenario: &desktop.ScenarioDraft{Plan: plan}}, IntentID: "scenario-1"})
	if saved.Outcome != desktop.SavedOutcome {
		t.Fatalf("save: %+v", saved)
	}
	var request struct {
		Wire     casegen.Wire     `json:"wire"`
		Bindings casegen.Bindings `json:"bindings"`
	}
	if err := json.Unmarshal(casegenFixture(t, "request-book-reschedule-cancel.json"), &request, json.RejectUnknownMembers(false)); err != nil {
		t.Fatal(err)
	}
	return app, context, *saved.Saved, profile, desktop.CaseGenerationSettings{Wire: request.Wire, Bindings: request.Bindings, Variants: []casegen.Variant{}}
}

func TestGeneratedScenarioCasesAreRegisteredOnceAndMatchTheCommandLine(t *testing.T) {
	app, context, scenarioRef, profile, settings := generationProject(t,
		[]scenariogen.Row{{ID: "plain", PatientName: "DOE", Notes: []string{}, Encoding: "utf-8"}},
		[]scenariogen.Variant{{ID: "baseline", Mutations: []scenariogen.Mutation{}}})
	settings.Variants = []casegen.Variant{
		{ID: "cancel-before-reschedule", Polarity: "negative", Mutations: []casegen.Mutation{{Op: "move", Step: "cancel", Before: "reschedule"}}},
		{ID: "duplicate-booking", Polarity: "negative", Mutations: []casegen.Mutation{{Op: "duplicate", Step: "book"}}},
	}
	if idle := app.ScenarioCasesProgress(); idle.State != desktop.Empty {
		t.Fatalf("progress with nothing running: %+v", idle)
	}
	before := entries(t, context.Project)
	request := desktop.ScenarioCasesRequest{Context: context, Scenario: scenarioRef, Profile: profile, Settings: settings, IntentID: "cases-1"}
	generated := app.GenerateScenarioCases(request)
	if generated.State != desktop.Completed || generated.Replayed || len(generated.Cases) != 3 || generated.HL7Version != "2.5.1" || generated.Family != "SIU" || generated.Seed != 7 {
		t.Fatalf("generate: %+v", generated)
	}
	for i, want := range []struct{ variant, polarity string }{{"baseline", "positive"}, {"cancel-before-reschedule", "negative"}, {"duplicate-booking", "negative"}} {
		c := generated.Cases[i]
		if c.Variant != want.variant || c.Polarity != want.polarity || !c.Evaluated || c.Case.Kind != desktop.CaseItem {
			t.Fatalf("case %d: %+v", i, c)
		}
	}
	if generated.Cases[0].Verdict != "pass" || len(generated.Cases[0].Phases) != 3 || generated.Cases[2].Messages != 4 {
		t.Fatalf("baseline %+v, duplicate %+v", generated.Cases[0], generated.Cases[2])
	}
	written := entries(t, context.Project)
	fresh := slices.DeleteFunc(slices.Clone(written), func(name string) bool { return slices.Contains(before, name) || !strings.HasPrefix(name, "generated-") })
	if len(fresh) != 4 || !slices.Contains(fresh, generated.Record) {
		t.Fatalf("written %v", fresh)
	}
	for _, c := range generated.Cases {
		if !slices.Contains(fresh, c.Entry) {
			t.Fatalf("case %s is not a project entry", c.Entry)
		}
	}

	// The same submission, or another of the same inputs, answers the cases
	// already generated and writes nothing.
	for _, intent := range []string{"cases-1", "cases-2"} {
		request.IntentID = intent
		again := app.GenerateScenarioCases(request)
		if again.State != desktop.Completed || !again.Replayed || again.ContentIdentity != generated.ContentIdentity || !slices.EqualFunc(again.Cases, generated.Cases, func(a, b desktop.GeneratedCase) bool { return a.Case == b.Case }) {
			t.Fatalf("again: %+v", again)
		}
	}
	if now := entries(t, context.Project); !slices.Equal(now, written) {
		t.Fatalf("a repeated submission wrote %v", now)
	}

	// Each case is a registered synthetic case that names its scenario.
	cases := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.CaseItem})
	for _, c := range generated.Cases {
		found := false
		for _, item := range cases.Page.Items {
			if item.Ref.ID == c.Case.ID {
				found = item.Summary.Case != nil && item.Summary.Case.Registered && item.Summary.Case.Provenance == "synthetic" && item.Summary.Case.Scenario != nil && item.Summary.Case.Scenario.Ref == scenarioRef
			}
		}
		if !found {
			t.Fatalf("case %s is not a registered case of its scenario", c.Entry)
		}
	}

	// The command line generates the same cases from the request the window
	// recorded, under the same profile and pack.
	raw, err := os.ReadFile(filepath.Join(context.Project, generated.Record))
	if err != nil {
		t.Fatal(err)
	}
	record, err := casegen.ReadRecord(raw)
	if err != nil || record.Ancestry.Source == nil || record.Ancestry.Source.Revision != scenarioRef.Revision {
		t.Fatalf("%v %+v", err, record.Ancestry.Source)
	}
	requestBytes, err := json.Marshal(record.Request)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "request.json"), requestBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if err := cli.Execute("dev", []string{"--operation-policy", testlicense.New(t), "scenario", "generate-case", filepath.Join(dir, "request.json"),
		filepath.Join(context.Project, "profile.json"), filepath.Join(context.Project, "pack.json"), "--output", filepath.Join(dir, "cli")}, &stdout, &stderr); err != nil {
		t.Fatalf("%v %s", err, stderr.String())
	}
	cliRaw, err := os.ReadFile(filepath.Join(dir, "cli", "generation.json"))
	if err != nil {
		t.Fatal(err)
	}
	cliRecord, err := casegen.ReadRecord(cliRaw)
	if err != nil || cliRecord.Ancestry.Request != record.Ancestry.Request || len(cliRecord.Cases) != len(record.Cases) {
		t.Fatalf("the command line generated other cases: %v", err)
	}
	for i, c := range record.Cases {
		if cliRecord.Cases[i].Identity != c.Identity {
			t.Fatalf("case %s differs from the command line's", c.Entry)
		}
	}

	// A derived generation names an ancestor the project holds, and leaves it
	// as it was.
	request.IntentID, request.Settings.DerivedFrom = "cases-3", strings.Repeat("0", 64)
	if refused := app.GenerateScenarioCases(request); refused.State != desktop.Failed || !strings.Contains(refused.Reason, "derive from is not in the project") {
		t.Fatalf("an unknown ancestor: %+v", refused)
	}
	request.Settings.DerivedFrom = generated.ContentIdentity
	derived := app.GenerateScenarioCases(request)
	if derived.State != desktop.Completed || derived.Replayed || derived.ContentIdentity == generated.ContentIdentity {
		t.Fatalf("a derived generation: %+v", derived)
	}
	// Its cases say exactly what the ancestor's did, so they are the ancestor's
	// cases: only its record is new.
	for i, c := range derived.Cases {
		if c.Case != generated.Cases[i].Case || c.Entry != generated.Cases[i].Entry {
			t.Fatalf("a derived case was written again: %+v", c)
		}
	}
	if now := entries(t, context.Project); len(slices.DeleteFunc(slices.Clone(now), func(name string) bool { return slices.Contains(written, name) })) != 1 {
		t.Fatalf("a derived generation wrote %v", now)
	}
	if after, err := os.ReadFile(filepath.Join(context.Project, generated.Record)); err != nil || !bytes.Equal(after, raw) {
		t.Fatal("the ancestor's record changed")
	}
}

func TestScenarioCasesRefuseWhatTheyCannotCarry(t *testing.T) {
	app, context, scenarioRef, profile, settings := generationProject(t,
		[]scenariogen.Row{{ID: "one", PatientName: "DOE", Notes: []string{"a", "b"}, Encoding: "utf-8"}, {ID: "two", PatientName: "ROE", Notes: []string{"a"}, Encoding: "utf-8"}},
		[]scenariogen.Variant{{ID: "baseline", Mutations: []scenariogen.Mutation{}}, {ID: "no-notes", Mutations: []scenariogen.Mutation{{Op: "field", Step: "book", Field: "NTE-3", State: "empty"}}}})
	before := entries(t, context.Project)
	refused := app.GenerateScenarioCases(desktop.ScenarioCasesRequest{Context: context, Scenario: scenarioRef, Profile: profile, Settings: settings, IntentID: "cases-1"})
	if refused.State != desktop.Failed || len(refused.Unconvertible) != 1 || refused.Unconvertible[0].Path != "variants[1].mutations[0]" || len(refused.Cases) != 0 {
		t.Fatalf("an unconvertible clause: %+v", refused)
	}
	if missing := app.GenerateScenarioCases(desktop.ScenarioCasesRequest{Context: context, Scenario: scenarioRef, Profile: profile, Settings: settings}); missing.State != desktop.Failed {
		t.Fatalf("a submission with no identity: %+v", missing)
	}
	if wrong := app.GenerateScenarioCases(desktop.ScenarioCasesRequest{Context: context, Scenario: profile, Profile: profile, Settings: settings, IntentID: "cases-2"}); wrong.State != desktop.Failed {
		t.Fatalf("a profile generated as a scenario: %+v", wrong)
	}
	if now := entries(t, context.Project); !slices.Equal(now, before) {
		t.Fatalf("a refused generation wrote %v", now)
	}

	app, context, scenarioRef, _, settings = generationProject(t,
		[]scenariogen.Row{{ID: "plain", PatientName: "DOE", Notes: []string{}, Encoding: "utf-8"}}, []scenariogen.Variant{{ID: "baseline", Mutations: []scenariogen.Mutation{}}})
	writeDocument(t, context.Project, "profile-adt.json", string(casegenFixture(t, "owned/profile-adt.json")))
	profiles := app.ListCatalog(desktop.CatalogQuery{Context: context, Kind: desktop.ProfileItem})
	checked := false
	for _, item := range profiles.Page.Items {
		if item.Name == "owned-casegen-adt" {
			other := app.GenerateScenarioCases(desktop.ScenarioCasesRequest{Context: context, Scenario: scenarioRef, Profile: item.Ref, Settings: settings, IntentID: "cases-3"})
			if other.State != desktop.Failed || !strings.Contains(other.Reason, "constrains ADT") {
				t.Fatalf("a profile of another family: %+v", other)
			}
			checked = true
		}
	}
	if !checked {
		t.Fatal("the ADT profile is not listed")
	}
}

// A running generation reports its stage and counts, the panel's own cancel
// stops it, and it answers Cancelled without a generation record.
func TestCancellingScenarioCaseGenerationLeavesNoRecord(t *testing.T) {
	rows := []scenariogen.Row{}
	for i := range 8 {
		rows = append(rows, scenariogen.Row{ID: "row-" + string(rune('a'+i)), PatientName: "DOE", Notes: []string{}, Encoding: "utf-8"})
	}
	variants := []scenariogen.Variant{}
	for i := range 16 {
		variants = append(variants, scenariogen.Variant{ID: "variant-" + string(rune('a'+i)), Mutations: []scenariogen.Mutation{{Op: "delay", Step: "book", After: "1s"}}})
	}
	app, context, scenarioRef, profile, settings := generationProject(t, rows, variants)
	answered := make(chan desktop.ScenarioCasesResult, 1)
	go func() {
		answered <- app.GenerateScenarioCases(desktop.ScenarioCasesRequest{Context: context, Scenario: scenarioRef, Profile: profile, Settings: settings, IntentID: "cases-1"})
	}()
	deadline := time.Now().Add(30 * time.Second)
	for {
		if progress := app.ScenarioCasesProgress(); progress.State == desktop.Completed && progress.Progress.Done > 0 {
			if progress.Progress.Cases != 128 {
				t.Fatalf("progress %+v", progress.Progress)
			}
			break
		}
		select {
		case result := <-answered:
			t.Fatalf("the generation answered before it reported progress: %+v", result)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("the generation reported no progress")
		}
		time.Sleep(time.Millisecond)
	}
	app.Cancel("another-operation")
	app.Cancel("scenario-cases")
	cancelled := <-answered
	if cancelled.State != desktop.Cancelled || len(cancelled.Cases) != 0 || cancelled.Record != "" {
		t.Fatalf("a cancelled generation answered %+v", cancelled)
	}
	for _, name := range entries(t, context.Project) {
		if strings.HasSuffix(name, "-generation.json") {
			t.Fatalf("a cancelled generation installed %s", name)
		}
	}
	if progress := app.ScenarioCasesProgress(); progress.State != desktop.Empty {
		t.Fatalf("progress outlived the generation: %+v", progress)
	}
}
