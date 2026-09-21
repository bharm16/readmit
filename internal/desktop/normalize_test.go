package desktop_test

import (
	"encoding/json/v2"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/diff"
)

// The comparison fixtures' one volatile position: every paired record's
// control ID was regenerated, so an ignore rule scoped to it is the policy a
// reader would actually author over this change.
const nrmPolicy = `{"schema":"readmit-normalization-policy/v1","rules":[{"id":"regenerated-control-id","selector":"MSH-10","operator":"ignore"}]}`

func normalizeRequest(root, identity string) desktop.NormalizeRequest {
	return desktop.NormalizeRequest{
		Workspace: root, Left: "before", Identity: identity, Right: "after",
		Policy: "compare.policy.json", Keys: []string{cmpKey},
		Offset: 0, Limit: desktop.MaxComparisonRows,
	}
}

// normalized runs one normalization and fails the test if the facade refused it.
func normalized(t *testing.T, app *desktop.App, request desktop.NormalizeRequest) *desktop.Normalization {
	t.Helper()
	result := app.NormalizeCompare(request)
	if result.State != desktop.Completed || result.Normalization == nil {
		t.Fatalf("the facade refused a normalization it supports: %+v", result)
	}
	return result.Normalization
}

// The whole delivery in one normalization: every difference the comparison
// found is listed beside what the policy did about it, a suppressed difference
// names the one rule that suppressed it, and the raw comparison over the same
// evidence still reports everything — nothing here edited it.
func TestNormalizingAComparisonShowsExactlyWhichDifferencesThePolicyHides(t *testing.T) {
	app, root, identity := comparisonWorkspace(t)
	writeDocument(t, root, "compare.policy.json", nrmPolicy)
	normalization := normalized(t, app, normalizeRequest(root, identity))

	if normalization.Report != diff.NormalizationSchema || normalization.PolicySchema != diff.PolicySchema {
		t.Fatalf("the normalization does not name its contracts: %+v", normalization)
	}
	if normalization.PolicySHA256 != sha256Of([]byte(nrmPolicy)) {
		t.Fatalf("the normalization does not name the exact policy revision it ran under: %s", normalization.PolicySHA256)
	}
	// Both pairs' control IDs were suppressed by the one rule; the moved
	// identifier was addressed by nothing and stays visible.
	if normalization.Summary.Suppressed != 2 || normalization.Summary.Unaddressed != 1 || normalization.Summary.Retained != 0 {
		t.Fatalf("the policy did not suppress exactly the two regenerated control IDs: %+v", normalization.Summary)
	}
	if normalization.Summary.Inserted != 1 {
		t.Fatalf("an inserted occurrence was concealed: %+v", normalization.Summary)
	}
	outcomes := map[string][]diff.Difference{}
	for _, difference := range normalization.Differences {
		outcomes[difference.Outcome] = append(outcomes[difference.Outcome], difference)
	}
	for _, suppressed := range outcomes["suppressed"] {
		if suppressed.Selector != "MSH[1]-10[1]" || suppressed.Rule != "regenerated-control-id" {
			t.Fatalf("a suppressed difference is not attributable to its rule: %+v", suppressed)
		}
	}
	if len(outcomes["unaddressed"]) != 1 || outcomes["unaddressed"][0].Selector != "PID[1]-3[2]" || outcomes["unaddressed"][0].Rule != "" {
		t.Fatalf("a difference no rule addressed was hidden or attributed: %+v", outcomes["unaddressed"])
	}
	// Every rule appears with its counts, even one that addressed nothing.
	if len(normalization.Rules) != 1 || normalization.Rules[0].Suppressed != 2 {
		t.Fatalf("the applied rule is not listed with its counts: %+v", normalization.Rules)
	}
	// The raw comparison over the same evidence is untouched by the policy.
	raw := compared(t, app, compareRequest(root, identity, "after", cmpKey))
	if raw.Rows[0].Fields[0].Selector != "MSH[1]-10[1]" {
		t.Fatalf("the raw comparison no longer reports what the policy suppressed: %+v", raw.Rows[0])
	}
}

// A rule that cannot read the values it was scoped to settles nothing:
// undecided is neither agreement nor a suppression, and the reason is a
// bounded word rather than the value.
func TestNormalizingReportsUndecidedAsNeitherAgreementNorSuppression(t *testing.T) {
	app, root, identity := comparisonWorkspace(t)
	writeDocument(t, root, "compare.policy.json",
		`{"schema":"readmit-normalization-policy/v1","rules":[{"id":"identifier-tolerance","selector":"PID-3[2]","operator":"numeric","tolerance":"1"}]}`)
	normalization := normalized(t, app, normalizeRequest(root, identity))
	if normalization.Summary.Undecided != 1 || normalization.Summary.Suppressed != 0 {
		t.Fatalf("a rule over unreadable values decided something: %+v", normalization.Summary)
	}
	var undecided *diff.Difference
	for i, difference := range normalization.Differences {
		if difference.Outcome == diff.Undecided {
			undecided = &normalization.Differences[i]
		}
	}
	if undecided == nil || undecided.Selector != "PID[1]-3[2]" || undecided.Reason != diff.NotNumeric {
		t.Fatalf("the undecided difference does not say why it was undecided: %+v", undecided)
	}
}

// The policy, the evidence and the window are each bound the way every other
// read is: a policy the reader refuses runs nothing, a stale identity is
// refused, and a window past the last difference is empty with its counts.
func TestNormalizingRefusesWhatItCannotBindAndWindowsWhatItCan(t *testing.T) {
	app, root, identity := comparisonWorkspace(t)
	writeDocument(t, root, "compare.policy.json", nrmPolicy)
	writeDocument(t, root, "broken.policy.json",
		`{"schema":"readmit-normalization-policy/v1","rules":[{"id":"a","selector":"MSH-7","operator":"ignore","precisoin":"hour"}]}`)

	stale := normalizeRequest(root, identity)
	stale.Identity = strings.Repeat("0", 64)
	if got := app.NormalizeCompare(stale); got.State != desktop.Failed || got.Normalization != nil {
		t.Fatalf("evidence that changed since it was displayed was normalized anyway: %+v", got)
	}
	broken := normalizeRequest(root, identity)
	broken.Policy = "broken.policy.json"
	if got := app.NormalizeCompare(broken); got.State != desktop.Failed {
		t.Fatalf("a policy with an unknown member scoped a comparison: %+v", got)
	}
	absent := normalizeRequest(root, identity)
	absent.Policy = "absent.json"
	if got := app.NormalizeCompare(absent); got.State != desktop.Failed ||
		!strings.Contains(got.Reason, "the normalization policy") {
		t.Fatalf("an absent policy was not refused by name: %+v", got)
	}
	unkeyed := normalizeRequest(root, identity)
	unkeyed.Keys = nil
	if got := app.NormalizeCompare(unkeyed); got.State != desktop.Failed ||
		!strings.Contains(got.Reason, "name the fields that identify one record") {
		t.Fatalf("collections that are not copies aligned without keys: %+v", got)
	}
	whole := normalized(t, app, normalizeRequest(root, identity))
	past := normalizeRequest(root, identity)
	past.Offset = whole.Total
	windowed := app.NormalizeCompare(past)
	if windowed.State != desktop.Empty || windowed.Normalization == nil ||
		windowed.Normalization.Total != whole.Total || windowed.Normalization.Summary != whole.Summary {
		t.Fatalf("a window past the last difference lost the counts: %+v", windowed)
	}
	if got := app.NormalizeCompare(desktop.NormalizeRequest{Workspace: root, Left: "before", Identity: identity, Right: "after", Policy: "compare.policy.json", Offset: 0, Limit: 0}); got.State != desktop.Failed {
		t.Fatalf("a window of no rows was accepted: %+v", got)
	}
	// No value crosses in any outcome, including the ones the policy retained.
	encoded, err := json.Marshal(app.NormalizeCompare(normalizeRequest(root, identity)))
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"CTL-1", "CTL-91", "MRN-2", "ALT-9", "DOE", "ROE"} {
		if strings.Contains(string(encoded), value) {
			t.Fatalf("the normalization disclosed the compared value %q", value)
		}
	}
}
