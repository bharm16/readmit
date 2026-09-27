package profileeval_test

import (
	"bytes"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/localprofile"
	"github.com/bharm16/readmit/internal/profileeval"
)

func adversarialComponentFixture(t *testing.T) (profileeval.ProfileV3, profileeval.PackV3, []byte) {
	t.Helper()
	read := func(name string) []byte {
		raw, err := os.ReadFile(filepath.Join("../../testdata/profile-evaluation/v3/2.5.1/SIU", name))
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	var profile profileeval.ProfileV3
	var pack profileeval.PackV3
	if err := json.Unmarshal(read("profile.json"), &profile); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(read("pack.json"), &pack); err != nil {
		t.Fatal(err)
	}
	// Exercise the same owned declarations as base metadata as well as local rules.
	if err := json.Unmarshal(encodeComponentFixture(t, profile.Datatypes), &pack.Datatypes); err != nil {
		t.Fatal(err)
	}
	return profile, pack, read("step-1.hl7")
}

func encodeComponentFixture(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestComponentDeclarationsRefuseAmbiguousAndInvalidDefinitions(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*[]profileeval.DatatypeRule)
	}{
		{"duplicate type", func(d *[]profileeval.DatatypeRule) { *d = append(*d, (*d)[0]) }},
		{"duplicate position", func(d *[]profileeval.DatatypeRule) {
			(*d)[0].Components = append((*d)[0].Components, (*d)[0].Components[0])
		}},
		{"unsorted positions", func(d *[]profileeval.DatatypeRule) { c := (*d)[0].Components; c[0], c[1] = c[1], c[0] }},
		{"no components", func(d *[]profileeval.DatatypeRule) { (*d)[0].Components = nil }},
		{"primitive shadow", func(d *[]profileeval.DatatypeRule) { (*d)[0].Name = "ST" }},
		{"required prohibited", func(d *[]profileeval.DatatypeRule) {
			(*d)[0].Components[0].Required = true
			(*d)[0].Components[0].Prohibited = true
		}},
		{"excess position", func(d *[]profileeval.DatatypeRule) { c := (*d)[0].Components; c[len(c)-1].Position = 65 }},
		{"duplicate codes", func(d *[]profileeval.DatatypeRule) { (*d)[0].Components[0].Codes = []string{"A", "A"} }},
	} {
		for _, side := range []string{"local", "base"} {
			t.Run(tc.name+"/"+side, func(t *testing.T) {
				profile, pack, input := adversarialComponentFixture(t)
				if side == "local" {
					tc.change(&profile.Datatypes)
				} else {
					tc.change(&pack.Datatypes)
				}
				p, b := encodeComponentFixture(t, profile), encodeComponentFixture(t, pack)
				originals := [][]byte{bytes.Clone(p), bytes.Clone(b), bytes.Clone(input)}
				if _, err := profileeval.Evaluate(t.Context(), p, b, []profileeval.Occurrence{{ID: "one", Bytes: input}}, profileeval.Options{}); err == nil {
					t.Fatal("ambiguous component contract was evaluated")
				}
				for i, raw := range [][]byte{p, b, input} {
					if !bytes.Equal(raw, originals[i]) {
						t.Fatal("refusal modified its input")
					}
				}
			})
		}
	}
}

func TestComponentSourceLimitationsStayUnsupportedThroughNestedFallback(t *testing.T) {
	for _, tc := range []struct {
		name, rule string
		change     func(*profileeval.PackV3)
	}{
		{"unknown referenced type", "datatype-MISSING", func(p *profileeval.PackV3) {
			for i := range p.Datatypes {
				if p.Datatypes[i].Name == "EI" {
					p.Datatypes[i].Components[0].DataType = "MISSING"
				}
			}
		}},
		{"indirect recursion reaches wire boundary", "datatype-wire-depth-EI", func(p *profileeval.PackV3) {
			for i := range p.Datatypes {
				switch p.Datatypes[i].Name {
				case "EI":
					p.Datatypes[i].Components[0].DataType = "HD"
				case "HD":
					p.Datatypes[i].Components[0].DataType = "EI"
				}
			}
		}},
		{"base component usage unavailable", "component-usage-unavailable-EI", func(p *profileeval.PackV3) {
			for i := range p.Datatypes {
				if p.Datatypes[i].Name == "EI" {
					p.Datatypes[i].UsageKnown = false
				}
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			profile, pack, input := adversarialComponentFixture(t)
			// The selected local profile falls back to base component declarations.
			profile.Datatypes = nil
			tc.change(&pack)
			p, b := encodeComponentFixture(t, profile), encodeComponentFixture(t, pack)
			before := bytes.Clone(input)
			report, err := profileeval.Evaluate(t.Context(), p, b, []profileeval.Occurrence{{ID: "one", Bytes: input}}, profileeval.Options{CompleteCapture: true})
			if err != nil {
				t.Fatal(err)
			}
			if report.Verdict != "undecided" || report.Operator != profileeval.ComponentOperatorVersion || len(report.Findings) > 64 {
				t.Fatalf("unsupported source became passing or unbounded: %+v", report)
			}
			found := false
			for _, finding := range report.Findings {
				if finding.Rule == tc.rule && finding.Outcome == "unsupported" {
					found = true
					if finding.Start < 0 || finding.End > len(input) || finding.Start >= finding.End {
						t.Fatalf("untraceable unsupported source: %+v", finding)
					}
				}
			}
			if !found {
				t.Fatalf("missing %s: %+v", tc.rule, report.Findings)
			}
			if !bytes.Equal(before, input) {
				t.Fatal("changed original evidence")
			}
		})
	}
}

func TestNestedComponentDecodingNeverPassesUnsafeBytes(t *testing.T) {
	for _, tc := range []struct {
		name, rule string
		change     func([]byte) []byte
	}{
		{"unknown nested escape", "unsupported_escape", func(raw []byte) []byte { return bytes.Replace(raw, []byte("^PLACER"), []byte("^PLA\\Q\\CER"), 1) }},
		{"invalid nested UTF8", "invalid_utf8", func(raw []byte) []byte {
			raw = bytes.Replace(raw, []byte("|2.5.1\r"), []byte("|2.5.1||||||UNICODE UTF-8\r"), 1)
			return bytes.Replace(raw, []byte("^PLACER"), []byte("^PLA\xffCER"), 1)
		}},
		{"unsupported declared encoding", "message-charset", func(raw []byte) []byte {
			return bytes.Replace(raw, []byte("|2.5.1\r"), []byte("|2.5.1||||||8859/1\r"), 1)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			profile, pack, input := adversarialComponentFixture(t)
			input = tc.change(input)
			p, b := encodeComponentFixture(t, profile), encodeComponentFixture(t, pack)
			before := [][]byte{bytes.Clone(p), bytes.Clone(b), bytes.Clone(input)}
			report, err := profileeval.Evaluate(t.Context(), p, b, []profileeval.Occurrence{{ID: "one", Bytes: input}}, profileeval.Options{CompleteCapture: true})
			if err != nil {
				t.Fatal(err)
			}
			if report.Verdict == "pass" || report.LocalVerdict == "pass" {
				t.Fatalf("unsafe bytes passed: %+v", report)
			}
			found := false
			for _, finding := range report.Findings {
				if finding.Rule == tc.rule && finding.Outcome == "unsupported" {
					found = true
					if finding.Occurrence != "one" || finding.Start < 0 || finding.End > len(input) || !strings.HasPrefix(finding.Selector, "SCH") && tc.rule != "message-charset" {
						t.Fatalf("wrong original source: %+v", finding)
					}
				}
			}
			if !found {
				t.Fatalf("missing named %s outcome: %+v", tc.rule, report)
			}
			for i, raw := range [][]byte{p, b, input} {
				if !bytes.Equal(raw, before[i]) {
					t.Fatal("evaluation repaired its input")
				}
			}
		})
	}
}

func TestComponentPatientAndVisitAuthoritiesKeepSeparateWorkflowState(t *testing.T) {
	read := func(name string) []byte {
		raw, err := os.ReadFile(filepath.Join("../../testdata/profile-evaluation/v3/2.5.1/ADT", name))
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	profile, pack := read("profile.json"), read("pack.json")
	for _, authority := range []string{"PATIENT-A", "VISIT-A"} {
		for _, complete := range []bool{false, true} {
			t.Run(authority+map[bool]string{false: "/partial", true: "/complete"}[complete], func(t *testing.T) {
				first, second := read("step-1.hl7"), read("step-2.hl7")
				control, err := profileeval.Evaluate(t.Context(), profile, pack, []profileeval.Occurrence{{ID: "first", Bytes: first}, {ID: "second", Bytes: second}}, profileeval.Options{CompleteCapture: complete})
				if err != nil {
					t.Fatal(err)
				}
				if control.Verdict != "pass" {
					t.Fatalf("unchanged namespace control failed: %+v", control)
				}
				second = bytes.Replace(second, []byte(authority), []byte(authority+"-OTHER"), 1)
				before := bytes.Clone(second)
				report, err := profileeval.Evaluate(t.Context(), profile, pack, []profileeval.Occurrence{{ID: "first", Bytes: first}, {ID: "second", Bytes: second}}, profileeval.Options{CompleteCapture: complete})
				if err != nil {
					t.Fatal(err)
				}
				want := "undecided"
				if complete {
					want = "fail"
				}
				if report.Verdict != want {
					t.Fatalf("borrowed %s authority state: %+v", authority, report)
				}
				found := false
				for _, finding := range report.Findings {
					if finding.Rule == "owned-adt-2-5-1-lifecycle:unobserved-prerequisite" && finding.Occurrence == "second" && finding.Outcome == want {
						found = true
					}
				}
				if !found || !bytes.Equal(before, second) {
					t.Fatal("missing independent namespace finding or changed original bytes")
				}
			})
		}
	}
}

func TestComponentSelectionUsesDeclaredDelimitersAndDatePrecision(t *testing.T) {
	profile, pack, input := adversarialComponentFixture(t)
	input = []byte(strings.NewReplacer("|", "*", "^", "$", "~", "%", "\\", "!", "&", "?").Replace(string(input)))
	input = bytes.Replace(input, []byte("APPOINTMENT$"), []byte("APPOINTMENT!S!A$"), 1)
	for _, tc := range []struct{ name, stamp, verdict string }{{"escaped delimiter with minute precision", "202601011200+0000", "pass"}, {"missing timezone", "202601011200", "fail"}, {"insufficient date precision", "20260101+0000", "fail"}} {
		t.Run(tc.name, func(t *testing.T) {
			raw := bytes.Replace(input, []byte("ZTM*202601011200+0000"), []byte("ZTM*"+tc.stamp), 1)
			before := bytes.Clone(raw)
			report, err := profileeval.Evaluate(t.Context(), encodeComponentFixture(t, profile), encodeComponentFixture(t, pack), []profileeval.Occurrence{{ID: "one", Bytes: raw}}, profileeval.Options{CompleteCapture: true})
			if err != nil {
				t.Fatal(err)
			}
			if report.Verdict != tc.verdict {
				t.Fatalf("declared delimiter/date handling: %+v", report)
			}
			if !bytes.Equal(before, raw) {
				t.Fatal("normalized original delimiters")
			}
		})
	}
}

func TestPrimitiveFieldsRejectUnescapedSubcomponentsOnlyUnderV3(t *testing.T) {
	for _, kind := range []string{"ST", "ID", "IS"} {
		for _, custom := range []bool{false, true} {
			for _, escaped := range []bool{false, true} {
				name := kind + map[bool]string{false: "/standard", true: "/custom"}[custom] + map[bool]string{false: "/raw", true: "/escaped"}[escaped]
				t.Run(name, func(t *testing.T) {
					local, pack := fixture(t)
					var definition localprofile.Profile
					if err := json.Unmarshal(local, &definition); err != nil {
						t.Fatal(err)
					}
					definition.Segments = []localprofile.Segment{{ID: "MSH", Fields: []localprofile.Field{{Position: 1, Usage: localprofile.UsageRequired, Type: "ST"}, {Position: 2, Usage: localprofile.UsageRequired, Type: "ST"}}}, {ID: "ZID", Fields: []localprofile.Field{{Position: 1, Usage: localprofile.UsageRequired, Type: localprofile.DataType(kind)}}}}
					value := "A&B"
					if escaped {
						value = "A\\T\\B"
					}
					raw := []byte("MSH|^~\\&|SYNTH|LAB|||20260101||SIU^S12|C1|P|2.5.1\rZID|" + value + "\r")
					if custom {
						raw = []byte(strings.NewReplacer("|", "*", "^", "$", "~", "%", "\\", "!", "&", "?").Replace(string(raw)))
					}
					before := bytes.Clone(raw)
					for _, version := range []string{"v1", "v2", "v3"} {
						var profile []byte
						switch version {
						case "v1":
							profile = encodeComponentFixture(t, definition)
						case "v2":
							profile = encodeComponentFixture(t, profileeval.ProfileV2{Schema: profileeval.ProfileSchema, Definition: definition})
						case "v3":
							profile = encodeComponentFixture(t, profileeval.ProfileV3{Schema: profileeval.ProfileSchemaV3, Definition: definition})
						}
						report, err := profileeval.Evaluate(t.Context(), profile, pack, []profileeval.Occurrence{{ID: "one", Bytes: raw}}, profileeval.Options{})
						if err != nil {
							t.Fatal(err)
						}
						want := "pass"
						if version == "v3" && !escaped {
							want = "fail"
						}
						if report.LocalVerdict != want {
							t.Fatalf("%s primitive field outcome=%s want=%s: %+v", version, report.LocalVerdict, want, report.Findings)
						}
						expectedOperator := profileeval.OperatorVersion
						if version == "v3" {
							expectedOperator = profileeval.ComponentOperatorVersion
						}
						if report.Operator != expectedOperator {
							t.Fatal("historical operator changed")
						}
						found := false
						for _, finding := range report.Findings {
							if finding.Rule == "primitive-components-"+kind && finding.Outcome == "fail" {
								found = true
								if finding.Selector != "ZID[1]-1[1]" || finding.Start < 0 || finding.End > len(raw) || finding.End <= finding.Start || !bytes.Equal(raw[finding.Start:finding.End], []byte(map[bool]string{false: "A&B", true: "A?B"}[custom])) {
									t.Fatalf("wrong primitive source: %+v", finding)
								}
							}
							if strings.HasPrefix(finding.Selector, "MSH") && finding.Outcome == "fail" {
								t.Fatalf("literal delimiters rejected: %+v", finding)
							}
						}
						if found != (version == "v3" && !escaped) {
							t.Fatal("wrong primitive delimiter finding")
						}
					}
					if !bytes.Equal(before, raw) {
						t.Fatal("changed original primitive bytes")
					}
				})
			}
		}
	}
}
