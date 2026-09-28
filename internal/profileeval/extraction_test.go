package profileeval_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"github.com/bharm16/readmit/internal/localprofile"
	"github.com/bharm16/readmit/internal/profileeval"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Opt-in: generated upstream material remains outside the repository until its
// exact redistribution review is approved. The normal suite uses owned fixtures.
func TestPinnedExtractionReadback(t *testing.T) {
	dir := os.Getenv("READMIT_PROFILE_EXTRACTION")
	if dir == "" {
		t.Skip("set READMIT_PROFILE_EXTRACTION to the offline extractor output")
	}
	var receipt struct {
		Packs map[string]struct {
			SHA256 string `json:"sha256"`
		} `json:"packs"`
		Notices map[string]struct {
			SHA256 string `json:"sha256"`
		} `json:"notices"`
	}
	recorded, err := os.ReadFile("../../docs/profile-extraction-v3-receipt.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(recorded, &receipt); err != nil {
		t.Fatal(err)
	}
	for path, notice := range receipt.Notices {
		raw, err := os.ReadFile(filepath.Join(dir, path))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(raw)
		if hex.EncodeToString(sum[:]) != notice.SHA256 {
			t.Fatalf("notice differs: %s", path)
		}
	}
	for _, version := range []string{"2.3.1", "2.4", "2.5", "2.5.1", "2.6", "2.7.1", "2.8.2"} {
		t.Run(version, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(dir, "pack-"+version+".json"))
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(raw)
			if hex.EncodeToString(sum[:]) != receipt.Packs["pack-"+version+".json"].SHA256 {
				t.Fatal("normalized pack differs from pending review receipt")
			}
			p, err := profileeval.DecodePack(raw)
			if err != nil {
				t.Fatal(err)
			}
			if len(p.Messages) == 0 {
				t.Fatal("empty normalized metadata")
			}
			if p.Metadata.Bundleable() == nil {
				t.Fatal("extractor approved rights")
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
// six order kinds opens the detail. It is checked against the exact extracted
// v4 packs, whose choice comes from the same version's HL7apy declaration.
// Versions without an ORM_O01 source never acquire one from a neighbour.
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
			if pack.Schema != profileeval.PackSchemaV4 {
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
					if failed == tc.valid || got.BaseSupport != "evaluated" || got.Operator != profileeval.ChoiceOperatorVersion {
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

// Opt-in: the v5 packs built from the pinned HL7 database export and the
// reviewed conditions stay outside the repository with the other withheld
// packs. The HD pairing below is the v2.8.2 Chapter 2A rule, independently
// restated here rather than read from the pack under test.
func TestPinnedDatabasePacks(t *testing.T) {
	dir := os.Getenv("READMIT_PROFILE_V5_EXTRACTION")
	if dir == "" {
		t.Skip("set READMIT_PROFILE_V5_EXTRACTION to the offline database build")
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
	t.Run("2.3.1 components are unclassified", func(t *testing.T) {
		got := evaluate(t, "2.3.1", "PID|1||MRN1^^^LAB_A^MR||DOE^JANE\r")
		if got.Verdict == "pass" || !slices.ContainsFunc(got.Findings, func(f profileeval.Finding) bool { return strings.HasPrefix(f.Rule, "component-usage-unclassified-") }) {
			t.Fatalf("the edition defines no component usage, so it cannot pass: %+v", got)
		}
	})
}
