package profileeval_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
	"github.com/bharm16/readmit/internal/localprofile"
	"github.com/bharm16/readmit/internal/profileeval"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Opt-in: the packs built from HL7's own files stay outside the repository until
// their exact redistribution review is approved (#627). The normal suite uses
// owned fixtures.
func TestPinnedExtractionReadback(t *testing.T) {
	dir := os.Getenv("READMIT_PROFILE_EXTRACTION")
	if dir == "" {
		t.Skip("set READMIT_PROFILE_EXTRACTION to the offline build output")
	}
	var receipt struct {
		Packs map[string]struct {
			SHA256 string `json:"sha256"`
		} `json:"packs"`
		Notice string `json:"notice_sha256"`
	}
	recorded, err := os.ReadFile("../../docs/profile-extraction-v5-receipt.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(recorded, &receipt); err != nil {
		t.Fatal(err)
	}
	notice, err := os.ReadFile(filepath.Join(dir, "licenses", "hl7-ip-copyright-and-trademarks.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	if sum := sha256.Sum256(notice); hex.EncodeToString(sum[:]) != receipt.Notice {
		t.Fatal("notice differs from the receipt")
	}
	for _, version := range []string{"2.3.1", "2.4", "2.5", "2.5.1", "2.6", "2.7.1", "2.8.2"} {
		t.Run(version, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(dir, "pack-"+version+".json"))
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(raw)
			if hex.EncodeToString(sum[:]) != receipt.Packs["pack-"+version+".json"].SHA256 {
				t.Fatal("pack differs from pending review receipt")
			}
			p, err := profileeval.DecodePack(raw)
			if err != nil {
				t.Fatal(err)
			}
			if p.Schema != profileeval.PackSchemaV5 || len(p.Messages) == 0 {
				t.Fatalf("%s pack with %d messages", p.Schema, len(p.Messages))
			}
			if p.Metadata.Bundleable() == nil {
				t.Fatal("builder approved rights")
			}
		})
	}
}

// The selected fixtures are authored independently of normalized sequence trees.
// This level qualifies only minimum order/cardinality, never absent field/table
// constraints or another structure with the same family name.
func TestPinnedUpstreamGroupingMatrix(t *testing.T) {
	dir := os.Getenv("READMIT_PROFILE_EXTRACTION")
	if dir == "" {
		t.Skip("set READMIT_PROFILE_EXTRACTION to the pinned offline extraction")
	}
	for _, version := range []string{"2.3.1", "2.4", "2.5", "2.5.1", "2.6", "2.7.1", "2.8.2"} {
		for _, family := range []string{"ADT", "SIU", "ORM", "ORU"} {
			t.Run(version+"/"+family, func(t *testing.T) {
				packRaw, err := os.ReadFile(filepath.Join(dir, "pack-"+version+".json"))
				if err != nil {
					t.Fatal(err)
				}
				pack, err := profileeval.DecodePack(packRaw)
				if err != nil {
					t.Fatal(err)
				}
				local, _ := fixture(t)
				var definition localprofile.Profile
				if err = json.Unmarshal(local, &definition); err != nil {
					t.Fatal(err)
				}
				definition.Base.Pack = pack.Metadata.Identity
				definition.Base.HL7Version = version
				definition.Base.Family = family
				definition.Segments = []localprofile.Segment{{ID: "MSH", Fields: []localprofile.Field{{Position: 10, Usage: localprofile.UsageRequired}}}}
				profileRaw, err := json.Marshal(definition)
				if err != nil {
					t.Fatal(err)
				}
				for _, kind := range []string{"positive", "negative"} {
					raw, err := os.ReadFile(filepath.Join("../../testdata/profile-evaluation/upstream-grouping", version, family+"-"+kind+".hl7"))
					if err != nil {
						t.Fatal(err)
					}
					got, err := profileeval.Evaluate(context.Background(), profileRaw, packRaw, []profileeval.Occurrence{{ID: kind, Bytes: raw}}, profileeval.Options{})
					if err != nil {
						t.Fatal(err)
					}
					if family == "ORM" && (version == "2.7.1" || version == "2.8.2") {
						if got.BaseSupport != "unsupported" || got.Verdict == "pass" {
							t.Fatalf("borrowed unavailable ORM schema: %+v", got)
						}
						found := false
						for _, finding := range got.Findings {
							if finding.Rule == "base-message-structure-unavailable" {
								found = true
							}
						}
						if !found {
							t.Fatal("missing named unsupported finding")
						}
						continue
					}
					groupFailure := false
					for _, finding := range got.Findings {
						if finding.Rule == "segment-group-order-cardinality" && finding.Origin == "profile" {
							groupFailure = true
						}
					}
					if groupFailure != (kind == "negative") || got.BaseSupport != "evaluated" {
						t.Fatalf("%s grouping: %+v", kind, got)
					}
					if got.Verdict == "pass" {
						t.Fatal("minimal grouping evidence advertised as full conformance")
					}
				}
			})
		}
	}
}

// Each fixture is authored from the order-detail grammar: exactly one of the
// six order kinds opens the detail. It is checked against the exact built
// packs, whose choice is the one each version's HL7 schema declares.
// Versions whose schemas define no ORM_O01 never acquire one from a neighbour.
func TestPinnedOrderDetailChoice(t *testing.T) {
	dir := os.Getenv("READMIT_PROFILE_EXTRACTION")
	if dir == "" {
		t.Skip("set READMIT_PROFILE_EXTRACTION to the pinned offline extraction")
	}
	for _, version := range []string{"2.3.1", "2.4", "2.5", "2.5.1", "2.6", "2.7.1", "2.8.2"} {
		t.Run(version, func(t *testing.T) {
			packRaw, err := os.ReadFile(filepath.Join(dir, "pack-"+version+".json"))
			if err != nil {
				t.Fatal(err)
			}
			pack, err := profileeval.DecodePack(packRaw)
			if err != nil {
				t.Fatal(err)
			}
			if pack.Schema != profileeval.PackSchemaV5 {
				t.Fatal(pack.Schema)
			}
			choices := 0
			var count func([]profileeval.Node)
			count = func(nodes []profileeval.Node) {
				for _, n := range nodes {
					if n.Choice {
						choices++
					}
					count(n.Children)
				}
			}
			for _, m := range pack.Messages {
				count(m.Sequence)
			}
			if version == "2.7.1" || version == "2.8.2" {
				if choices != 0 {
					t.Fatal("borrowed a choice from another version")
				}
				return
			}
			if choices != 1 {
				t.Fatalf("want the one ORM_O01 order-detail choice, got %d", choices)
			}
			local, _ := fixture(t)
			var definition localprofile.Profile
			if err = json.Unmarshal(local, &definition); err != nil {
				t.Fatal(err)
			}
			definition.Base.Pack = pack.Metadata.Identity
			definition.Base.HL7Version = version
			definition.Base.Family = "ORM"
			definition.Segments = []localprofile.Segment{{ID: "MSH", Fields: []localprofile.Field{{Position: 10, Usage: localprofile.UsageRequired}}}}
			profileRaw, err := json.Marshal(definition)
			if err != nil {
				t.Fatal(err)
			}
			for _, tc := range []struct {
				name  string
				valid bool
			}{{"OBR", true}, {"RQD", true}, {"RQ1", true}, {"RXO", true}, {"ODS", true}, {"ODT", true}, {"mixed", false}, {"missing", false}} {
				t.Run(tc.name, func(t *testing.T) {
					raw, err := os.ReadFile(filepath.Join("../../testdata/profile-evaluation/upstream-grouping", version, "ORM-detail-"+tc.name+".hl7"))
					if err != nil {
						t.Fatal(err)
					}
					got, err := profileeval.Evaluate(context.Background(), profileRaw, packRaw, []profileeval.Occurrence{{ID: tc.name, Bytes: raw}}, profileeval.Options{})
					if err != nil {
						t.Fatal(err)
					}
					failed := false
					for _, finding := range got.Findings {
						if finding.Rule == "segment-group-order-cardinality" && finding.Origin == "profile" {
							failed = true
						}
					}
					if failed == tc.valid || got.BaseSupport != "evaluated" || got.Operator != profileeval.UsageOperatorVersion {
						t.Fatalf("order detail %s: %+v", tc.name, got)
					}
					if got.Verdict == "pass" {
						t.Fatal("order-detail grouping advertised as full conformance")
					}
				})
			}
		})
	}
}

// Opt-in: the packs built from HL7's schemas, its chapters and the reviewed
// conditions. The HD pairing below is the v2.8.2 Chapter 2A rule,
// independently restated here rather than read from the pack under test.
func TestPinnedStandardPacks(t *testing.T) {
	dir := os.Getenv("READMIT_PROFILE_EXTRACTION")
	if dir == "" {
		t.Skip("set READMIT_PROFILE_EXTRACTION to the offline build output")
	}
	packs := map[string][]byte{}
	for _, version := range []string{"2.3.1", "2.4", "2.5", "2.5.1", "2.6", "2.7.1", "2.8.2"} {
		raw, err := os.ReadFile(filepath.Join(dir, "pack-"+version+".json"))
		if err != nil {
			t.Fatal(err)
		}
		pack, err := profileeval.DecodePack(raw)
		if err != nil {
			t.Fatalf("%s: %v", version, err)
		}
		if pack.Schema != profileeval.PackSchemaV5 || pack.Metadata.Bundleable() == nil {
			t.Fatalf("%s: %s or rights approved by the builder", version, pack.Schema)
		}
		packs[version] = raw
	}
	evaluate := func(t *testing.T, version, body string) profileeval.Report {
		t.Helper()
		pack, _ := profileeval.DecodePack(packs[version])
		local, _ := fixture(t)
		var definition localprofile.Profile
		if err := json.Unmarshal(local, &definition); err != nil {
			t.Fatal(err)
		}
		definition.Base.Pack, definition.Base.HL7Version, definition.Base.Family = pack.Metadata.Identity, version, "ADT"
		definition.Segments = []localprofile.Segment{{ID: "MSH", Fields: []localprofile.Field{{Position: 10, Usage: localprofile.UsageRequired}}}}
		profile, err := json.Marshal(definition)
		if err != nil {
			t.Fatal(err)
		}
		raw := []byte("MSH|^~\\&|OWNED|LAB|||20260101120000||ADT^A01^ADT_A01|M1|P|" + version + "\rEVN|A01|20260101120000\r" + body + "PV1|1|I\r")
		got, err := profileeval.Evaluate(context.Background(), profile, packs[version], []profileeval.Occurrence{{ID: "one", Bytes: raw}}, profileeval.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if got.Operator != profileeval.UsageOperatorVersion || got.Verdict == "pass" && strings.Contains(body, "&1.2.3^") {
			t.Fatalf("unexpected report: %+v", got)
		}
		return got
	}
	has := func(r profileeval.Report, rule, selector string) bool {
		return slices.ContainsFunc(r.Findings, func(f profileeval.Finding) bool { return f.Rule == rule && f.Selector == selector })
	}
	t.Run("2.8.2 HD pairing", func(t *testing.T) {
		missingType := evaluate(t, "2.8.2", "PID|1||MRN1^^^LAB_A&1.2.3^MR||DOE^JANE\r")
		if !has(missingType, "required-conditional-component", "PID[1]-3[1].4.3") {
			t.Fatalf("universal identifier without its type passed: %+v", missingType.Findings)
		}
		paired := evaluate(t, "2.8.2", "PID|1||MRN1^^^LAB_A&1.2.3&ISO^MR||DOE^JANE\r")
		if has(paired, "required-conditional-component", "PID[1]-3[1].4.3") || has(paired, "required-conditional-component", "PID[1]-3[1].4.2") {
			t.Fatalf("a complete HD pair failed: %+v", paired.Findings)
		}
	})
	// Each expectation restates the edition's printed usage: PID-3 and PID-5
	// are R in every edition; PID-2 and OBR-5 are withdrawn (W) from 2.7; and
	// Chapter 7's OBR reprint marks OBR-5 X for results through 2.5.1, B in 2.6.
	message := func(t *testing.T, version, family, raw string) profileeval.Report {
		t.Helper()
		pack, _ := profileeval.DecodePack(packs[version])
		local, _ := fixture(t)
		var definition localprofile.Profile
		if err := json.Unmarshal(local, &definition); err != nil {
			t.Fatal(err)
		}
		definition.Base.Pack, definition.Base.HL7Version, definition.Base.Family = pack.Metadata.Identity, version, family
		definition.Segments = []localprofile.Segment{{ID: "MSH", Fields: []localprofile.Field{{Position: 10, Usage: localprofile.UsageRequired}}}}
		profile, err := json.Marshal(definition)
		if err != nil {
			t.Fatal(err)
		}
		got, err := profileeval.Evaluate(context.Background(), profile, packs[version], []profileeval.Occurrence{{ID: "one", Bytes: []byte(raw)}}, profileeval.Options{})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	for _, version := range []string{"2.3.1", "2.4", "2.5", "2.5.1", "2.6", "2.7.1", "2.8.2"} {
		t.Run(version+" printed usage", func(t *testing.T) {
			msh := "MSH|^~\\&|OWNED|LAB|||20260101120000||%s|M1|P|" + version + "\r"
			adt := fmt.Sprintf(msh, "ADT^A01^ADT_A01") + "EVN|A01|20260101120000\rPID|1|%s|%s||%s\rPV1|1|I\r"
			missing := message(t, version, "ADT", fmt.Sprintf(adt, "", "", ""))
			if !has(missing, "required-field", "PID[1]-3") || !has(missing, "required-field", "PID[1]-5") {
				t.Fatalf("PID-3 and PID-5 are required: %+v", missing.Findings)
			}
			withdrawn := message(t, version, "ADT", fmt.Sprintf(adt, "OLD1", "MRN1^^^LAB^MR", "DOE^JANE"))
			if has(withdrawn, "prohibited-field", "PID[1]-2") != (version == "2.7.1" || version == "2.8.2") {
				t.Fatalf("PID-2 is withdrawn only from 2.7: %+v", withdrawn.Findings)
			}
			oru := fmt.Sprintf(msh, "ORU^R01^ORU_R01") + "PID|1||MRN1^^^LAB^MR||DOE^JANE\rOBR|1|||CODE^Test^L|S\rOBX|1|ST|CODE^Result^L||value||||||F\r"
			results := message(t, version, "ORU", oru)
			if has(results, "prohibited-field", "OBR[1]-5") != (version != "2.6") {
				t.Fatalf("Chapter 7's OBR-5: %+v", results.Findings)
			}
		})
	}
	t.Run("2.3.1 components are unclassified", func(t *testing.T) {
		got := evaluate(t, "2.3.1", "PID|1||MRN1^^^LAB_A^MR||DOE^JANE\r")
		if got.Verdict == "pass" || !slices.ContainsFunc(got.Findings, func(f profileeval.Finding) bool { return strings.HasPrefix(f.Rule, "component-usage-unclassified-") }) {
			t.Fatalf("the edition defines no component usage, so it cannot pass: %+v", got)
		}
	})
}
