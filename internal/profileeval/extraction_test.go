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
	recorded, err := os.ReadFile("../../docs/profile-extraction-v2-receipt.json")
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
