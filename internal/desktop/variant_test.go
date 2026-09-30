package desktop_test

import (
	"bytes"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/diff"
	"github.com/bharm16/readmit/internal/project"
	"github.com/bharm16/readmit/internal/reproducer"
	"github.com/bharm16/readmit/internal/transform"
)

// bookingVariant is the incident's booking and its acknowledgement, whose
// entries are t000001 (booking) and t000002 (acknowledgement).
func bookingVariant(t *testing.T, incident desktop.ItemRef, identity string, steps ...desktop.VariantSequenceStep) desktop.VariantDraft {
	t.Helper()
	plan, err := reproducer.NewPlan(identity)
	if err != nil {
		t.Fatal(err)
	}
	plan.Steps = append(plan.Steps,
		reproducer.Step{Operator: reproducer.SelectOccurrence, Occurrence: repBookingID},
		reproducer.Step{Operator: reproducer.IncludeAcknowledgements},
		reproducer.Step{Operator: reproducer.SetField, Occurrence: repBookingID, Selector: "PID-5.1", Value: "ROE"})
	draft := desktop.VariantDraft{Source: incident, Plan: plan}
	if len(steps) > 0 {
		draft.Transform = &desktop.VariantTransform{Steps: steps}
	}
	return draft
}

// A variant's sequence changes are resolved exactly as Save builds them, and
// Save publishes them as one derived case registered under the
// transformation that wrote it. The case it came from is not touched.
func TestAVariantWithSequenceChangesIsOneDerivedCaseOfItsSource(t *testing.T) {
	app, context, incident, plan := variantProject(t)
	root := context.Project
	before := bytesUnder(t, filepath.Join(root, "incident"))
	draft := bookingVariant(t, incident, plan.Case,
		desktop.VariantSequenceStep{Step: transform.Step{Operator: transform.ShiftDates, Shift: "24h"}},
		desktop.VariantSequenceStep{Step: transform.Step{Operator: transform.DuplicateOccurrence, Entry: "t000001"}, Occurrence: repBookingID})

	resolved := app.ResolveVariant(desktop.VariantRequest{Context: context, Draft: draft, Reveal: true})
	if resolved.State != desktop.Completed || resolved.Variant == nil {
		t.Fatalf("resolve: %+v", resolved)
	}
	view := resolved.Variant
	included := []string{}
	for _, message := range view.Messages {
		if message.Included {
			included = append(included, message.Message.ID+"="+message.Reason)
		}
	}
	if len(view.Messages) != 3 || strings.Join(included, " ") != repBookingID+"=selected "+repAcceptedID+"=acknowledgement" {
		t.Fatalf("included messages: %+v", view.Messages)
	}
	order := []string{}
	for _, entry := range view.Sequence {
		order = append(order, entry.Occurrence)
	}
	if strings.Join(order, " ") != repBookingID+" "+repBookingID+" "+repAcceptedID || !view.Sequence[1].Copy {
		t.Fatalf("sequence: %+v", view.Sequence)
	}
	edited, shifted := false, 0
	for _, change := range view.Changes {
		switch {
		case change.Operator == reproducer.SetField:
			edited = change.Selector == "PID[1]-5[1].1" && change.AfterValue != nil && *change.AfterValue == "ROE" && change.BeforeValue != nil && *change.BeforeValue == "DOE"
		case change.Operator == transform.ShiftDates:
			shifted++
			if change.BeforeValue == nil || change.AfterValue == nil || *change.BeforeValue == *change.AfterValue {
				t.Fatalf("a shifted value was not read before and after: %+v", change)
			}
		}
	}
	if !edited || shifted == 0 || len(view.Blocking) != 0 {
		t.Fatalf("changes: %+v blocking %+v", view.Changes, view.Blocking)
	}
	if hidden := app.ResolveVariant(desktop.VariantRequest{Context: context, Draft: draft}); hidden.Variant == nil ||
		slices.ContainsFunc(hidden.Variant.Changes, func(change desktop.VariantChange) bool { return change.BeforeValue != nil || change.AfterValue != nil }) {
		t.Fatalf("values were read without being asked for: %+v", hidden)
	}

	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.VariantItem,
		Draft: desktop.ItemDraft{Name: "Booking twice", Variant: &draft}, IntentID: "variant-sequence"})
	if saved.Outcome != desktop.SavedOutcome || saved.Saved == nil {
		t.Fatalf("save: %+v", saved)
	}
	revisions, err := project.ReadRevisions(root)
	if err != nil || len(revisions.Revisions) != 1 || revisions.Revisions[0].Operation.Name != transform.Derivation || revisions.Revisions[0].Operation.Parent != "incident" {
		t.Fatalf("the registered revision: %+v %v", revisions, err)
	}
	opened := app.OpenCase(root, revisions.Revisions[0].Name)
	if opened.Case == nil || opened.Case.Occurrences != 3 || opened.Case.Messages != 2 {
		t.Fatalf("the saved variant's case: %+v", opened)
	}
	if after := bytesUnder(t, filepath.Join(root, "incident")); !maps.EqualFunc(after, before, bytes.Equal) {
		t.Fatal("saving a variant changed the case it came from")
	}
}

// A change pinned to a message the included messages no longer hold in that
// place is refused rather than moved onto another message, and an excluded
// entry that breaks a relation the variant still holds part of blocks Save.
// Neither writes anything.
func TestAVariantNeverRetargetsAChangeOrSavesABrokenRelation(t *testing.T) {
	app, context, incident, plan := variantProject(t)
	root := context.Project
	held := entries(t, root)
	stale := bookingVariant(t, incident, plan.Case,
		desktop.VariantSequenceStep{Step: transform.Step{Operator: transform.DuplicateOccurrence, Entry: "t000002"}, Occurrence: repBookingID})
	refused := app.ResolveVariant(desktop.VariantRequest{Context: context, Draft: stale})
	if refused.State != desktop.Failed || len(refused.Problems) != 1 || refused.Problems[0].Field != "variant.transform.steps[0]" {
		t.Fatalf("a retargeted change: %+v", refused)
	}
	broken := bookingVariant(t, incident, plan.Case,
		desktop.VariantSequenceStep{Step: transform.Step{Operator: transform.DropOccurrence, Entry: "t000002"}, Occurrence: repAcceptedID})
	resolved := app.ResolveVariant(desktop.VariantRequest{Context: context, Draft: broken})
	if resolved.Variant == nil || len(resolved.Variant.Blocking) != 1 || resolved.Variant.Blocking[0].Code != transform.SeveredByDrop {
		t.Fatalf("a broken relation: %+v", resolved)
	}
	for name, draft := range map[string]desktop.VariantDraft{"stale": stale, "broken": broken} {
		saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.VariantItem,
			Draft: desktop.ItemDraft{Name: "Refused " + name, Variant: &draft}, IntentID: "refused-" + name})
		if saved.Outcome != desktop.InvalidOutcome || len(saved.Problems) == 0 {
			t.Fatalf("%s: %+v", name, saved)
		}
	}
	if now := entries(t, root); !slices.Equal(now, held) {
		t.Fatalf("a refused variant wrote %v", now)
	}
}

// A case and its variant compare field by field under keys a person chose,
// with every message only one side holds kept as its own row; a named
// normalization policy suppresses what it addresses from the presentation
// while the original differences stay available, and the variant side
// carries its lineage and plan.
func TestCasesCompareUnderANamedPolicyAndKeepTheirOriginalDifferences(t *testing.T) {
	app, context, incident, plan := variantProject(t)
	root := context.Project
	draft := bookingVariant(t, incident, plan.Case)
	saved := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.VariantItem,
		Draft: desktop.ItemDraft{Name: "Renamed booking", Variant: &draft}, IntentID: "compare-variant"})
	if saved.Saved == nil {
		t.Fatalf("save: %+v", saved)
	}
	variant := *saved.Saved
	unkeyed := app.CompareCases(desktop.CaseComparisonRequest{Context: context, Current: incident, Other: variant, Limit: 50})
	if unkeyed.State != desktop.Failed || !strings.Contains(unkeyed.Reason, "MSH-10") {
		t.Fatalf("a comparison guessed its keys: %+v", unkeyed)
	}
	// What the variant was made from does not wait for keys.
	if refused := unkeyed.Comparison; refused == nil || len(refused.Rows) != 0 || len(refused.Lineage) != 1 || len(refused.Lineage[0].Steps) != len(plan.Steps)+2 {
		t.Fatalf("a refused comparison left out the variant's lineage: %+v", unkeyed.Comparison)
	}
	compared := app.CompareCases(desktop.CaseComparisonRequest{Context: context, Current: incident, Other: variant, Keys: []string{"MSH-10"}, Limit: 50})
	if compared.State != desktop.Completed || compared.Comparison == nil {
		t.Fatalf("compare: %+v", compared)
	}
	kinds := map[desktop.RowKind][]string{}
	for _, row := range compared.Comparison.Rows {
		kinds[row.Kind] = append(kinds[row.Kind], row.Field)
		if row.EarlierValue != nil || row.LaterValue != nil {
			t.Fatalf("values were shown without being asked for: %+v", row)
		}
	}
	if !slices.Contains(kinds[desktop.PairedRow], "PID[1]-5[1]") || len(kinds[desktop.MissingRow]) != 1 {
		t.Fatalf("rows: %+v", compared.Comparison.Rows)
	}
	if lineage := compared.Comparison.Lineage; len(lineage) != 1 || lineage[0].Source == nil || lineage[0].Source.ID != incident.ID ||
		len(lineage[0].Included) != 2 || len(lineage[0].Steps) != len(plan.Steps)+2 {
		t.Fatalf("lineage: %+v", compared.Comparison.Lineage)
	}
	policy := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.NormalizationPolicyItem, IntentID: "policy",
		Draft: desktop.ItemDraft{Name: "Ignore names", NormalizationPolicy: &diff.Policy{Rules: []diff.Rule{{Selector: "PID-5", Operator: diff.IgnoreOperator}}}}})
	if policy.Saved == nil {
		t.Fatalf("policy: %+v", policy)
	}
	refusedPolicy := app.SaveItem(desktop.SaveItemRequest{Context: context, Kind: desktop.NormalizationPolicyItem, IntentID: "bad-policy",
		Draft: desktop.ItemDraft{Name: "Bad", NormalizationPolicy: &diff.Policy{Rules: []diff.Rule{{Selector: "MSH-7", Operator: diff.TimestampOperator}}}}})
	if refusedPolicy.Outcome != desktop.InvalidOutcome || refusedPolicy.Problems[0].Field != "normalization_policy.rules[0]" {
		t.Fatalf("a timestamp rule without precision: %+v", refusedPolicy)
	}
	normalized := app.CompareCases(desktop.CaseComparisonRequest{Context: context, Current: incident, Other: variant, Keys: []string{"MSH-10"}, Policy: policy.Saved, Limit: 50})
	if normalized.Comparison == nil || normalized.Comparison.Suppressed != 1 ||
		slices.ContainsFunc(normalized.Comparison.Rows, func(row desktop.CaseComparisonRow) bool { return row.Field == "PID[1]-5[1]" }) {
		t.Fatalf("normalized: %+v", normalized)
	}
	original := app.CompareCases(desktop.CaseComparisonRequest{Context: context, Current: incident, Other: variant, Keys: []string{"MSH-10"}, Policy: policy.Saved,
		Original: true, Reveal: true, Limit: 50})
	at := slices.IndexFunc(original.Comparison.Rows, func(row desktop.CaseComparisonRow) bool { return row.Field == "PID[1]-5[1]" })
	if at < 0 || original.Comparison.Rows[at].Outcome != diff.Suppressed || original.Comparison.Rows[at].EarlierValue == nil {
		t.Fatalf("original differences: %+v", original.Comparison)
	}
	if listed := listed(t, app, root, desktop.NormalizationPolicyItem)["Ignore names"]; listed.Summary.NormalizationPolicy == nil || listed.Summary.NormalizationPolicy.Rules != 1 {
		t.Fatalf("the saved policy: %+v", listed)
	}
}
