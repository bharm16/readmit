package profileeval_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"os"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/localprofile"
	"github.com/bharm16/readmit/internal/profileeval"
)

// usagePack is an owned v5 pack. Its HD, CX and XTN declarations are authored
// by hand from HL7 v2.8.2 Chapter 2A (§2.A.33 HD, §2.A.14 CX, §2.A.91 XTN):
// the edition's usage codes, the conditions its narrative states, and its
// LEN/C.LEN columns. Only the positions a test needs are declared, and the
// closed code lists are owned test pins, not the edition's tables. ZFC is a
// fictional segment whose field condition exercises the same operator.
func usagePack(t *testing.T, change func(pack map[string]any)) (profile, pack []byte) {
	t.Helper()
	read := func(name string) map[string]any {
		raw, err := os.ReadFile("../../testdata/profile-evaluation/v3/2.8.2/ADT/" + name)
		if err != nil {
			t.Fatal(err)
		}
		var v map[string]any
		if err = json.Unmarshal(raw, &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	var definition localprofile.Profile
	raw, err := json.Marshal(read("profile.json")["definition"])
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &definition); err != nil {
		t.Fatal(err)
	}
	definition.Base.Pack.Version = "5"
	definition.Segments = []localprofile.Segment{{ID: "ZFC", Fields: []localprofile.Field{{Position: 9, Usage: localprofile.UsageOptional}}}}
	if profile, err = json.Marshal(definition); err != nil {
		t.Fatal(err)
	}
	valued := func(p int) map[string]any { return map[string]any{"valued": p} }
	notValued := func(p int) map[string]any { return map[string]any{"not_valued": p} }
	all := func(p ...map[string]any) map[string]any { return map[string]any{"all": p} }
	when := func(p map[string]any, then, otherwise string) map[string]any {
		return map[string]any{"when": p, "then": then, "else": otherwise}
	}
	length := func(state string, facets ...int) map[string]any {
		l := map[string]any{"state": state}
		switch state {
		case "normative":
			l["min"], l["max"], l["count"] = facets[0], facets[1], profileeval.CountEscapeContent
		case "conformance-only":
			l["conformance"], l["truncation"] = facets[0], "="
		}
		return l
	}
	component := func(position int, datatype, usage string, l map[string]any, extra map[string]any) map[string]any {
		c := map[string]any{"position": position, "datatype": datatype, "usage": usage, "required": false, "max_length": 0, "codes": []string{}, "length": l}
		for k, v := range extra {
			c[k] = v
		}
		return c
	}
	k := read("pack.json")
	k["schema"] = profileeval.PackSchemaV5
	k["metadata"].(map[string]any)["pack"].(map[string]any)["version"] = "5"
	k["datatypes"] = []any{
		map[string]any{"name": "HD", "usage_known": true, "components": []any{
			component(1, "IS", "O", length("conformance-only", 20), map[string]any{"table": "0300", "table_kind": "user"}),
			component(2, "ST", "C", length("conformance-only", 199), map[string]any{"condition": when(valued(3), "R", "O")}),
			component(3, "ID", "C", length("normative", 1, 6), map[string]any{"condition": when(valued(2), "R", "O"), "table": "0301", "table_kind": "hl7", "policy": "closed", "codes": []string{"ISO", "DNS", "UUID"}}),
		}},
		map[string]any{"name": "CX", "usage_known": true, "components": []any{
			component(1, "ST", "R", length("conformance-only", 15), nil),
			component(4, "HD", "C", length("not-assigned"), map[string]any{"condition": when(all(notValued(9), notValued(10)), "R", "O"), "table": "0363", "table_kind": "user"}),
			component(5, "ID", "R", length("normative", 2, 5), map[string]any{"table": "0203", "table_kind": "hl7", "policy": "extensible", "codes": []string{"MR", "PI"}}),
			component(9, "CWE", "C", length("not-assigned"), map[string]any{"condition": when(all(notValued(4), notValued(10)), "R", "O")}),
			component(10, "CWE", "C", length("not-assigned"), map[string]any{"condition": when(all(notValued(4), notValued(9)), "R", "O")}),
		}},
		map[string]any{"name": "XTN", "usage_known": true, "components": []any{
			component(1, "ST", "W", length("not-assigned"), nil),
			component(3, "ID", "R", length("normative", 2, 8), map[string]any{"table": "0202", "table_kind": "hl7", "policy": "closed", "codes": []string{"PH", "Internet"}}),
			component(4, "ST", "C", length("conformance-only", 199), nil),
			component(7, "SNM", "C", length("conformance-only", 9), map[string]any{"condition": when(all(notValued(4), notValued(12)), "R", "X")}),
			component(12, "ST", "C", length("conformance-only", 199), map[string]any{"condition": when(all(notValued(4), notValued(7)), "R", "X")}),
		}},
	}
	field := func(position int, datatype, usage string, extra map[string]any) map[string]any {
		f := map[string]any{"position": position, "required": false, "max_repetitions": 0, "datatype": datatype, "max_length": 0, "usage": usage, "length": length("not-assigned")}
		for k, v := range extra {
			f[k] = v
		}
		return f
	}
	k["messages"] = []any{map[string]any{"hl7_version": "2.8.2", "family": "ADT", "structure": "ADT_A01",
		"sequence": []any{
			map[string]any{"name": "header", "segment": "MSH", "min": 1, "max": "1"},
			map[string]any{"name": "patient", "segment": "PID", "min": 1, "max": "1"},
			map[string]any{"name": "fictional", "segment": "ZFC", "min": 0, "max": "1"},
		},
		"segments": []any{
			map[string]any{"id": "PID", "fields": []any{field(3, "CX", "R", nil), field(13, "XTN", "O", nil)}},
			map[string]any{"id": "ZFC", "fields": []any{
				field(1, "HD", "O", nil),
				field(2, "ST", "C", map[string]any{"condition": when(valued(1), "R", "X"), "length": length("normative", 1, 4)}),
			}},
		}}}
	if change != nil {
		change(k)
	}
	if pack, err = json.Marshal(k); err != nil {
		t.Fatal(err)
	}
	return profile, pack
}

func evaluateUsage(t *testing.T, profile, pack []byte, body string) profileeval.Report {
	t.Helper()
	header := "MSH|^~\\&|OWNED|LAB|||20260101120000||ADT^A01^ADT_A01|M1|P|2.8.2"
	if !utf8.ValidString(body) || strings.ContainsFunc(body, func(r rune) bool { return r > 127 }) {
		header += "||||||UNICODE UTF-8"
	}
	raw := []byte(header + "\r" + body)
	before := bytes.Clone(raw)
	got, err := profileeval.Evaluate(context.Background(), profile, pack, []profileeval.Occurrence{{ID: "one", Bytes: raw}}, profileeval.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, raw) {
		t.Fatal("evaluation modified evidence")
	}
	if got.Operator != profileeval.UsageOperatorVersion || got.Pack.Schema != profileeval.PackSchemaV5 {
		t.Fatalf("v5 pack not evaluated by its operator: %+v", got)
	}
	return got
}

func findingAt(r profileeval.Report, rule, selector string) *profileeval.Finding {
	for i, f := range r.Findings {
		if f.Rule == rule && (selector == "" || f.Selector == selector) {
			return &r.Findings[i]
		}
	}
	return nil
}

// The v2.8.2 HD narrative: "The second and third components must either both
// be valued (both non-null), or both be not valued (both null)."
func TestConditionalHDComponentsAreEvaluatedFromTheirPredicate(t *testing.T) {
	profile, pack := usagePack(t, nil)
	for _, tc := range []struct {
		name, hd, verdict, rule, selector string
	}{
		{"namespace only", "LAB_A", "pass", "", ""},
		{"universal identifier and its type", "LAB_A&1.2.3&ISO", "pass", "", ""},
		{"universal pair without namespace", "&1.2.3&ISO", "pass", "", ""},
		{"both universal components null", `LAB_A&""&""`, "pass", "", ""},
		{"universal identifier without its type", "LAB_A&1.2.3", "fail", "required-conditional-component", "PID[1]-3[1].4.3"},
		{"type without the universal identifier", "LAB_A&&ISO", "fail", "required-conditional-component", "PID[1]-3[1].4.2"},
		{"universal identifier with a null type", `LAB_A&1.2.3&""`, "fail", "required-conditional-component", "PID[1]-3[1].4.3"},
		{"type longer than its normative maximum", "LAB_A&1.2.3&ISOISOX", "fail", "component-length", "PID[1]-3[1].4.3"},
		{"type outside its closed pinned codes", "LAB_A&1.2.3&M", "fail", "component-code-set", "PID[1]-3[1].4.3"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := evaluateUsage(t, profile, pack, "PID|||MRN1^^^"+tc.hd+"^MR\r")
			if got.Verdict != tc.verdict {
				t.Fatalf("want %s: %+v", tc.verdict, got)
			}
			if tc.rule == "" {
				return
			}
			f := findingAt(got, tc.rule, tc.selector)
			if f == nil || f.Origin != "profile" || f.Outcome != "fail" {
				t.Fatalf("missing %s at %s: %+v", tc.rule, tc.selector, got.Findings)
			}
		})
	}
}

// The conditional component is evaluated in its own occurrence: an absent
// parent imposes nothing, and an unreadable operand is unknown, never false.
func TestConditionalUsageKeepsParentContextAndUnknownOperands(t *testing.T) {
	profile, pack := usagePack(t, nil)
	absent := evaluateUsage(t, profile, pack, "PID|||MRN1^^^^MR\r")
	if f := findingAt(absent, "required-conditional-component", "PID[1]-3[1].4"); absent.Verdict != "fail" || f == nil || f.State != "empty" {
		t.Fatalf("CX.4 is required when neither CX.9 nor CX.10 is valued: %+v", absent)
	}
	if slices.ContainsFunc(absent.Findings, func(f profileeval.Finding) bool { return strings.HasPrefix(f.Selector, "PID[1]-3[1].4.") }) {
		t.Fatalf("an absent HD evaluated its own components: %+v", absent.Findings)
	}
	// An unreadable sibling component makes its whole field unreadable, so
	// an unknown operand is exercised between fields: ZFC-2 depends on ZFC-1.
	unreadable := evaluateUsage(t, profile, pack, "PID|||MRN1^^^LAB_A&1.2.3&\\Zfoo\\^MR\r")
	if f := findingAt(unreadable, "unsupported_escape", "PID[1]-3[1]"); unreadable.Verdict == "pass" || f == nil {
		t.Fatalf("an unreadable identifier passed: %+v", unreadable)
	}
	unknown := evaluateUsage(t, profile, pack, "PID|||MRN1^^^LAB_A^MR\rZFC|LAB\\Zfoo\\|ABCD\r")
	if f := findingAt(unknown, "conditional-predicate-unknown-field", "ZFC[1]-2"); unknown.Verdict == "pass" || f == nil || f.Outcome != "undecided" {
		t.Fatalf("an unreadable ZFC-1 must leave ZFC-2's condition unknown: %+v", unknown)
	}
}

// XTN's exclusive address forms: "required when, and allowed only if" is a
// condition whose false branch prohibits the component. XTN.4's own sentence
// reads "or" where XTN.7 and XTN.12 read "and", so its predicate is left
// unrepresented rather than guessed, and it cannot pass.
func TestConditionalProhibitionAndUnrepresentedPredicates(t *testing.T) {
	profile, pack := usagePack(t, nil)
	for _, tc := range []struct {
		name, xtn, verdict, rule, selector string
	}{
		// XTN.4's unrepresented predicate keeps every XTN from passing.
		{"local number alone", "^^PH^^^^5551234", "undecided", "conditional-predicate-unrepresented-component", "PID[1]-13[1].4"},
		{"unformatted number alone", "^^PH^^^^^^^^^1-800-TEST", "undecided", "conditional-predicate-unrepresented-component", "PID[1]-13[1].4"},
		{"local and unformatted numbers", "^^PH^^^^5551234^^^^^1-800-TEST", "fail", "prohibited-conditional-component", "PID[1]-13[1].12"},
		{"no address form", "^^PH", "fail", "required-conditional-component", "PID[1]-13[1].7"},
		{"withdrawn telephone number", "555^^PH^^^^5551234", "fail", "prohibited-component", "PID[1]-13[1].1"},
		{"communication address", "^^Internet^a\\T\\b", "undecided", "conditional-predicate-unrepresented-component", "PID[1]-13[1].4"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := evaluateUsage(t, profile, pack, "PID|||MRN1^^^LAB_A^MR||||||||||"+tc.xtn+"\r")
			if got.Verdict != tc.verdict {
				t.Fatalf("want %s: %+v", tc.verdict, got)
			}
			if tc.rule != "" && findingAt(got, tc.rule, tc.selector) == nil {
				t.Fatalf("missing %s at %s: %+v", tc.rule, tc.selector, got.Findings)
			}
			if tc.verdict == "undecided" && slices.ContainsFunc(got.Findings, func(f profileeval.Finding) bool { return f.Outcome == "fail" }) {
				t.Fatalf("a valid address form failed: %+v", got.Findings)
			}
		})
	}
	// Every edition lets a site extend an HL7 table, so an HL7 table without
	// pinned values is no base constraint; an external table needs its pinned
	// vocabulary and stays a named gap.
	profile, pack = usagePack(t, func(k map[string]any) {
		c := k["datatypes"].([]any)[2].(map[string]any)["components"].([]any)[1].(map[string]any)
		c["codes"], c["policy"] = []string{}, "extensible"
	})
	got := evaluateUsage(t, profile, pack, "PID|||MRN1^^^LAB_A^MR||||||||||^^LCL^^^^5551234\r")
	if slices.ContainsFunc(got.Findings, func(f profileeval.Finding) bool { return f.Selector == "PID[1]-13[1].3" }) {
		t.Fatalf("an extensible HL7 table refused a local code: %+v", got.Findings)
	}
	profile, pack = usagePack(t, func(k map[string]any) {
		c := k["datatypes"].([]any)[2].(map[string]any)["components"].([]any)[1].(map[string]any)
		c["codes"], c["table_kind"] = []string{}, "external"
		delete(c, "policy")
	})
	got = evaluateUsage(t, profile, pack, "PID|||MRN1^^^LAB_A^MR||||||||||^^PH^^^^5551234\r")
	if f := findingAt(got, "component-terminology-external-unavailable-0202", "PID[1]-13[1].3"); got.Verdict != "undecided" || f == nil || f.Outcome != "unsupported" {
		t.Fatalf("an unpinned external table passed: %+v", got)
	}
}

func TestFieldConditionsUseTheirSegmentOccurrence(t *testing.T) {
	profile, pack := usagePack(t, nil)
	for _, tc := range []struct {
		name, zfc, verdict, rule, selector string
	}{
		{"neither field", "ZFC|\r", "pass", "", ""},
		{"condition holds", "ZFC|LAB_A|ABCD\r", "pass", "", ""},
		{"condition holds without the field", "ZFC|LAB_A\r", "fail", "required-conditional-field", "ZFC[1]-2"},
		{"condition false with the field", "ZFC||ABCD\r", "fail", "prohibited-conditional-field", "ZFC[1]-2"},
		{"condition false with a null field", "ZFC||\"\"\r", "fail", "prohibited-conditional-field", "ZFC[1]-2"},
		{"normative field maximum", "ZFC|LAB_A|ABCDE\r", "fail", "field-length", "ZFC[1]-2[1]"},
		// v2.8.2 section 2.7.1: an escape sequence's content counts, its
		// delimiters do not; lengths count characters, not bytes.
		{"escape content within the maximum", "ZFC|LAB_A|AB\\T\\C\r", "pass", "", ""},
		{"escape content beyond the maximum", "ZFC|LAB_A|ABC\\T\\D\r", "fail", "field-length", "ZFC[1]-2[1]"},
		{"multibyte characters within the maximum", "ZFC|LAB_A|ÄÖÜ\r", "pass", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := evaluateUsage(t, profile, pack, "PID|||MRN1^^^LAB_A^MR\r"+tc.zfc)
			if got.Verdict != tc.verdict {
				t.Fatalf("want %s: %+v", tc.verdict, got)
			}
			if tc.rule != "" && findingAt(got, tc.rule, tc.selector) == nil {
				t.Fatalf("missing %s at %s: %+v", tc.rule, tc.selector, got.Findings)
			}
		})
	}
}

// v2.3.1-2.6 count the encoded occurrence and state no escape rule, so a value
// that fits only when each escape sequence counts as one character is undecided.
func TestEncodedLengthCountIsBoundedWithoutAnEscapeRule(t *testing.T) {
	profile, pack := usagePack(t, func(k map[string]any) {
		zfc := k["messages"].([]any)[0].(map[string]any)["segments"].([]any)[1].(map[string]any)
		zfc["fields"].([]any)[1].(map[string]any)["length"].(map[string]any)["count"] = profileeval.CountEncoded
	})
	for _, tc := range []struct{ name, value, verdict, rule string }{
		{"plain within", "ABCD", "pass", ""},
		{"plain beyond", "ABCDE", "fail", "field-length"},
		{"escape counted either way", "AB\\T\\C", "undecided", "field-length-unit-undetermined"},
		{"escape beyond either way", "ABCD\\T\\", "fail", "field-length"},
		{"multibyte characters within", "ÄÖÜ", "pass", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := evaluateUsage(t, profile, pack, "PID|||MRN1^^^LAB_A^MR\rZFC|LAB_A|"+tc.value+"\r")
			if got.Verdict != tc.verdict || tc.rule != "" && findingAt(got, tc.rule, "ZFC[1]-2[1]") == nil {
				t.Fatalf("want %s %s: %+v", tc.verdict, tc.rule, got)
			}
		})
	}
}

// A source that cannot tell optional from conditional says so; it never lets
// a v5 evaluation pass. Unavailable length is a named gap, while a
// conformance length alone never fails a longer value.
func TestUnclassifiedUsageAndUnavailableLengthCannotPass(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
		rule   string
	}{
		{"unclassified component", func(k map[string]any) {
			hd := k["datatypes"].([]any)[0].(map[string]any)["components"].([]any)[0].(map[string]any)
			hd["usage"] = "unclassified"
		}, "component-usage-unclassified-HD"},
		{"unclassified field", func(k map[string]any) {
			pid := k["messages"].([]any)[0].(map[string]any)["segments"].([]any)[0].(map[string]any)["fields"].([]any)[1].(map[string]any)
			pid["usage"] = "unclassified"
		}, "base-field-usage-unclassified"},
		{"unavailable component length", func(k map[string]any) {
			hd := k["datatypes"].([]any)[0].(map[string]any)["components"].([]any)[0].(map[string]any)
			hd["length"] = map[string]any{"state": "unavailable"}
		}, "component-length-unavailable-HD"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			profile, pack := usagePack(t, tc.change)
			got := evaluateUsage(t, profile, pack, "PID|||MRN1^^^LAB_A^MR\r")
			if f := findingAt(got, tc.rule, ""); got.Verdict != "undecided" || f == nil || f.Outcome != "unsupported" {
				t.Fatalf("want undecided with %s: %+v", tc.rule, got)
			}
		})
	}
	profile, pack := usagePack(t, nil)
	long := strings.Repeat("N", 200)
	if got := evaluateUsage(t, profile, pack, "PID|||MRN1^^^"+long+"^MR\r"); got.Verdict != "pass" {
		t.Fatalf("a conformance length is not a maximum: %+v", got)
	}
	if got := evaluateUsage(t, profile, pack, "PID|||MRN1^^^LAB_A^ZZ\r"); got.Verdict != "pass" {
		t.Fatalf("an extensible HL7 table refused a site extension: %+v", got)
	}
	if got := evaluateUsage(t, profile, pack, "PID|||MRN1^^^LOCAL_DEPT_42^MR\r"); got.Verdict != "pass" {
		t.Fatalf("a user-defined table refused a local code: %+v", got)
	}
}

// Before v5 an upstream (0, 1) or false declaration meant "not required",
// which may be conditional. New evaluations report it instead of passing;
// a local profile's own optional declaration keeps its meaning.
func TestLegacyBaseDeclarationsCannotPassAConditionalAsOptional(t *testing.T) {
	_, v5 := usagePack(t, nil)
	var k map[string]any
	if err := json.Unmarshal(v5, &k); err != nil {
		t.Fatal(err)
	}
	k["schema"] = profileeval.PackSchemaV4
	legacy := func(item map[string]any) {
		usage := item["usage"]
		delete(item, "usage")
		delete(item, "condition")
		delete(item, "length")
		delete(item, "table_kind")
		delete(item, "policy")
		if _, component := item["codes"]; component {
			item["codes"] = []string{}
			item["prohibited"] = usage == "W"
		}
		item["required"] = usage == "R"
	}
	for _, d := range k["datatypes"].([]any) {
		for _, c := range d.(map[string]any)["components"].([]any) {
			legacy(c.(map[string]any))
		}
	}
	for _, s := range k["messages"].([]any)[0].(map[string]any)["segments"].([]any) {
		for _, f := range s.(map[string]any)["fields"].([]any) {
			legacy(f.(map[string]any))
		}
	}
	pack, err := json.Marshal(k)
	if err != nil {
		t.Fatal(err)
	}
	profile, _ := usagePack(t, nil)
	raw := []byte("MSH|^~\\&|OWNED|LAB|||20260101120000||ADT^A01^ADT_A01|M1|P|2.8.2\rPID|||MRN1^^^LAB_A&1.2.3^MR\r")
	got, err := profileeval.Evaluate(context.Background(), profile, pack, []profileeval.Occurrence{{ID: "one", Bytes: raw}}, profileeval.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Operator != profileeval.ChoiceOperatorVersion || got.Verdict == "pass" {
		t.Fatalf("a v4 pack passed an unclassified conditional: %+v", got)
	}
	for _, rule := range []string{"component-usage-unclassified-HD", "base-field-usage-unclassified"} {
		if f := findingAt(got, rule, ""); f == nil || f.Outcome != "unsupported" {
			t.Fatalf("missing %s: %+v", rule, got.Findings)
		}
	}
}

func TestV5MembersAreRefusedOutsideTheirContract(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
	}{
		{"condition without conditional usage", func(k map[string]any) {
			c := k["datatypes"].([]any)[0].(map[string]any)["components"].([]any)[0].(map[string]any)
			c["condition"] = map[string]any{"when": map[string]any{"valued": 2}, "then": "R", "else": "O"}
		}},
		{"predicate on itself", func(k map[string]any) {
			c := k["datatypes"].([]any)[0].(map[string]any)["components"].([]any)[1].(map[string]any)
			c["condition"] = map[string]any{"when": map[string]any{"valued": 2}, "then": "R", "else": "O"}
		}},
		{"predicate with two members", func(k map[string]any) {
			c := k["datatypes"].([]any)[0].(map[string]any)["components"].([]any)[1].(map[string]any)
			c["condition"] = map[string]any{"when": map[string]any{"valued": 3, "not_valued": 1}, "then": "R", "else": "O"}
		}},
		{"branch outside R, RE, O, X", func(k map[string]any) {
			c := k["datatypes"].([]any)[0].(map[string]any)["components"].([]any)[1].(map[string]any)
			c["condition"] = map[string]any{"when": map[string]any{"valued": 3}, "then": "W", "else": "O"}
		}},
		{"unknown usage code", func(k map[string]any) {
			k["datatypes"].([]any)[0].(map[string]any)["components"].([]any)[0].(map[string]any)["usage"] = "maybe"
		}},
		{"required boolean beside usage", func(k map[string]any) {
			k["datatypes"].([]any)[0].(map[string]any)["components"].([]any)[0].(map[string]any)["required"] = true
		}},
		{"missing length facets", func(k map[string]any) {
			delete(k["datatypes"].([]any)[0].(map[string]any)["components"].([]any)[0].(map[string]any), "length")
		}},
		{"normative length without a maximum", func(k map[string]any) {
			k["datatypes"].([]any)[0].(map[string]any)["components"].([]any)[0].(map[string]any)["length"] = map[string]any{"state": "normative", "min": 1}
		}},
		{"not-assigned length with a bound", func(k map[string]any) {
			k["datatypes"].([]any)[0].(map[string]any)["components"].([]any)[0].(map[string]any)["length"] = map[string]any{"state": "not-assigned", "max": 5}
		}},
		{"table without a kind", func(k map[string]any) {
			delete(k["datatypes"].([]any)[0].(map[string]any)["components"].([]any)[0].(map[string]any), "table_kind")
		}},
		{"HL7 table without a policy", func(k map[string]any) {
			delete(k["datatypes"].([]any)[1].(map[string]any)["components"].([]any)[2].(map[string]any), "policy")
		}},
		{"normative length without a count", func(k map[string]any) {
			k["datatypes"].([]any)[0].(map[string]any)["components"].([]any)[2].(map[string]any)["length"] = map[string]any{"state": "normative", "min": 1, "max": 6}
		}},
		{"codes on a user-defined table", func(k map[string]any) {
			c := k["datatypes"].([]any)[0].(map[string]any)["components"].([]any)[0].(map[string]any)
			c["codes"], c["policy"] = []string{"LAB_A"}, "closed"
		}},
		{"v5 members in a v4 pack", func(k map[string]any) { k["schema"] = profileeval.PackSchemaV4 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			profile, pack := usagePack(t, tc.change)
			raw := []byte("MSH|^~\\&|OWNED|LAB|||20260101120000||ADT^A01^ADT_A01|M1|P|2.8.2\rPID|||MRN1^^^LAB_A^MR\r")
			if _, err := profileeval.Evaluate(context.Background(), profile, pack, []profileeval.Occurrence{{ID: "one", Bytes: raw}}, profileeval.Options{}); err == nil {
				t.Fatal("invalid v5 declaration was evaluated")
			}
		})
	}
}

// Each predicate atom the edition texts need, on the fictional ZFC-3 whose
// condition a case supplies: required while the condition holds.
func TestPredicateAtomsReadTheirOwnEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, body, verdict string
		when                map[string]any
	}{
		{"value not in: holds", "ZFC|||||||||F", "fail", map[string]any{"value_not_in": map[string]any{"position": 9, "values": []string{"X"}}}},
		{"value not in: excluded value", "ZFC||||||||X", "pass", map[string]any{"value_not_in": map[string]any{"position": 8, "values": []string{"X"}}}},
		{"value in first component", "ZFC||||||||F^final", "fail", map[string]any{"value_in": map[string]any{"position": 8, "values": []string{"F"}}}},
		{"trigger event", "", "fail", map[string]any{"message": map[string]any{"part": "trigger", "in": []string{"A01"}}}},
		{"another trigger event", "", "pass", map[string]any{"message": map[string]any{"part": "trigger", "in": []string{"A21"}}}},
		{"unique other segment valued", "EVN|A01|20260101\rZFC|", "fail", map[string]any{"field": map[string]any{"segment": "EVN", "position": 2, "state": "valued"}}},
		{"absent other segment is not valued", "ZFC|", "pass", map[string]any{"field": map[string]any{"segment": "EVN", "position": 2, "state": "valued"}}},
		{"ambiguous other segment", "EVN|A01|1\rEVN|A01|2\rZFC|", "undecided", map[string]any{"field": map[string]any{"segment": "EVN", "position": 2, "state": "valued", "scope": "group"}}},
		{"repeated segment", "ZFC|\rZFC|", "fail", map[string]any{"repeated": "segment"}},
		{"single segment", "ZFC|", "pass", map[string]any{"repeated": "segment"}},
		{"information not in the message", "ZFC|", "undecided", map[string]any{"unknown": "required when known"}},
		{"any with a known true branch", "ZFC|||||||||X", "fail", map[string]any{"any": []any{map[string]any{"unknown": "site agreement"}, map[string]any{"valued": 9}}}},
		{"not", "ZFC|", "fail", map[string]any{"not": map[string]any{"valued": 9}}},
		{"sibling component empty", "ZFC||||||||^free text", "fail", map[string]any{"component": map[string]any{"position": 8, "component": 1, "state": "not_valued"}}},
		{"sibling component valued", "ZFC||||||||CODE^text", "pass", map[string]any{"component": map[string]any{"position": 8, "component": 1, "state": "not_valued"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			profile, pack := usagePack(t, func(k map[string]any) {
				messages := k["messages"].([]any)[0].(map[string]any)
				messages["sequence"] = []any{
					map[string]any{"name": "header", "segment": "MSH", "min": 1, "max": "1"},
					map[string]any{"name": "event", "segment": "EVN", "min": 0, "max": "*"},
					map[string]any{"name": "patient", "segment": "PID", "min": 1, "max": "1"},
					map[string]any{"name": "fictional", "segment": "ZFC", "min": 0, "max": "*"},
				}
				zfc := messages["segments"].([]any)[1].(map[string]any)
				field := map[string]any{"position": 3, "required": false, "max_repetitions": 0, "datatype": "ST", "max_length": 0, "usage": "C",
					"condition": map[string]any{"when": tc.when, "then": "R", "else": "O"}, "length": map[string]any{"state": "not-assigned"}}
				zfc["fields"] = append(zfc["fields"].([]any), field)
			})
			body := tc.body
			if body == "" {
				body = "ZFC|"
			}
			if !strings.HasPrefix(body, "EVN") {
				body = strings.Replace(body, "ZFC", "PID|||MRN1^^^LAB_A^MR\rZFC", 1)
			} else {
				body = strings.Replace(body, "ZFC", "PID|||MRN1^^^LAB_A^MR\rZFC", 1)
			}
			got := evaluateUsage(t, profile, pack, body+"\r")
			if got.Verdict != tc.verdict {
				t.Fatalf("want %s: %+v", tc.verdict, got)
			}
			if tc.verdict == "fail" && findingAt(got, "required-conditional-field", "ZFC[1]-3") == nil {
				t.Fatalf("missing ZFC-3 requirement: %+v", got.Findings)
			}
		})
	}
}

// A field's own table binding: user-defined tables never fail a site's code,
// HL7 tables are extensible, and an external vocabulary without its pinned
// values is a named gap.
func TestFieldTableBindings(t *testing.T) {
	for _, tc := range []struct {
		name, kind, value, verdict, rule string
	}{
		{"user-defined local code", "user", "LOCAL", "pass", ""},
		{"extensible HL7 table", "hl7", "ZZ", "pass", ""},
		{"external vocabulary not pinned", "external", "mg", "undecided", "field-terminology-external-unavailable-9999"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			profile, pack := usagePack(t, func(k map[string]any) {
				zfc := k["messages"].([]any)[0].(map[string]any)["segments"].([]any)[1].(map[string]any)
				field := map[string]any{"position": 4, "required": false, "max_repetitions": 0, "datatype": "ST", "max_length": 0, "usage": "O",
					"length": map[string]any{"state": "not-assigned"}, "table": "9999", "table_kind": tc.kind}
				if tc.kind == "hl7" {
					field["policy"] = "extensible"
				}
				zfc["fields"] = append(zfc["fields"].([]any), field)
			})
			got := evaluateUsage(t, profile, pack, "PID|||MRN1^^^LAB_A^MR\rZFC||||"+tc.value+"\r")
			if got.Verdict != tc.verdict || tc.rule != "" && findingAt(got, tc.rule, "ZFC[1]-4[1]") == nil {
				t.Fatalf("want %s %s: %+v", tc.verdict, tc.rule, got)
			}
		})
	}
}
