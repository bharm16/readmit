package profileeval_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/profileeval"
)

func TestOwnedSemanticMatrix(t *testing.T) {
	var matrix struct {
		Schema string `json:"schema"`
		Rows   []struct {
			HL7Version    string            `json:"hl7_version"`
			Family        string            `json:"family"`
			Fixture       string            `json:"fixture"`
			Test          string            `json:"test"`
			Level         string            `json:"level"`
			Qualification string            `json:"qualification"`
			Artifacts     map[string]string `json:"artifacts"`
		} `json:"rows"`
	}
	b, err := os.ReadFile("../../docs/profile-semantic-fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &matrix, json.RejectUnknownMembers(true)); err != nil {
		t.Fatal(err)
	}
	if matrix.Schema != "readmit-profile-semantic-fixtures/v1" || len(matrix.Rows) != 28 {
		t.Fatal("incomplete matrix")
	}
	seen := map[string]bool{}
	for _, row := range matrix.Rows {
		key := row.HL7Version + "/" + row.Family
		if seen[key] {
			t.Fatal("duplicate cell")
		}
		seen[key] = true
		t.Run(key, func(t *testing.T) {
			root := filepath.Join("../..", row.Fixture)
			files := map[string][]byte{}
			for name, want := range row.Artifacts {
				b, err := os.ReadFile(filepath.Join(root, name))
				if err != nil {
					t.Fatal(err)
				}
				sum := sha256.Sum256(b)
				if hex.EncodeToString(sum[:]) != want {
					t.Fatalf("fixture provenance differs: %s", name)
				}
				files[name] = b
			}
			var cases []struct {
				Name            string   `json:"name"`
				Files           []string `json:"files"`
				CompleteCapture bool     `json:"complete_capture"`
				Verdict         string   `json:"verdict"`
			}
			if err := json.Unmarshal(files["expectations.json"], &cases, json.RejectUnknownMembers(true)); err != nil {
				t.Fatal(err)
			}
			if len(cases) != 6 {
				t.Fatal("missing positive/negative fixture")
			}
			for _, tc := range cases {
				t.Run(tc.Name, func(t *testing.T) {
					inputs := []profileeval.Occurrence{}
					for _, name := range tc.Files {
						inputs = append(inputs, profileeval.Occurrence{ID: name, Bytes: bytes.Clone(files[name])})
					}
					got, err := profileeval.Evaluate(context.Background(), files["profile.json"], files["pack.json"], inputs, profileeval.Options{CompleteCapture: tc.CompleteCapture})
					if err != nil {
						t.Fatal(err)
					}
					if got.Verdict != tc.Verdict || got.LocalVerdict != tc.Verdict {
						t.Fatalf("want %s got %+v", tc.Verdict, got)
					}
					if got.Operator != profileeval.ComponentOperatorVersion || got.BaseSupport != "evaluated" || got.WorkflowSupport != "evaluated" {
						t.Fatal(got)
					}
					for _, in := range inputs {
						if !bytes.Equal(in.Bytes, files[in.ID]) {
							t.Fatal("changed original fixture bytes")
						}
					}
				})
			}
		})
	}
}

func TestPublishedSourceMatrixBindsEverySelectedGroupingFixture(t *testing.T) {
	var matrix struct {
		Schema string `json:"schema"`
		Rows   []struct {
			Version      string `json:"hl7_version"`
			Family       string `json:"family"`
			Pack         string `json:"pack_sha256"`
			Rights       string `json:"rights_review"`
			Shipped      bool   `json:"shipped"`
			GroupSupport string `json:"upstream_grouping_support"`
			Fixtures     map[string]struct {
				Path   string `json:"path"`
				SHA256 string `json:"sha256"`
			} `json:"upstream_grouping_fixtures"`
		} `json:"rows"`
	}
	raw, err := os.ReadFile("../../docs/profile-evaluation-matrix.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &matrix); err != nil {
		t.Fatal(err)
	}
	var receipt struct {
		Packs map[string]struct {
			SHA256 string `json:"sha256"`
		} `json:"packs"`
	}
	raw, err = os.ReadFile("../../docs/profile-extraction-v2-receipt.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &receipt); err != nil {
		t.Fatal(err)
	}
	if matrix.Schema != "readmit-profile-evaluation-matrix/v2" || len(matrix.Rows) != 28 {
		t.Fatal("incomplete source matrix")
	}
	seen := map[string]bool{}
	for _, row := range matrix.Rows {
		key := row.Version + "/" + row.Family
		if seen[key] {
			t.Fatal("duplicate source cell")
		}
		seen[key] = true
		if row.Shipped || row.Rights != "pending" || row.Pack != receipt.Packs["pack-"+row.Version+".json"].SHA256 {
			t.Fatalf("unreviewed source gained distribution or moved pin: %s", key)
		}
		wanted := "tested-minimum-order-cardinality-only"
		if row.Family == "ORM" && (row.Version == "2.7.1" || row.Version == "2.8.2") {
			wanted = "schema-absent"
		}
		if row.GroupSupport != wanted {
			t.Fatalf("overstated grouping support: %s", key)
		}
		if len(row.Fixtures) != 2 {
			t.Fatal("missing grouping fixtures")
		}
		for _, kind := range []string{"positive", "negative"} {
			fixture, ok := row.Fixtures[kind]
			if !ok {
				t.Fatal(kind)
			}
			raw, err := os.ReadFile(filepath.Join("../..", fixture.Path))
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(raw)
			if hex.EncodeToString(sum[:]) != fixture.SHA256 {
				t.Fatal("fixture differs from source qualification matrix")
			}
		}
	}
}
