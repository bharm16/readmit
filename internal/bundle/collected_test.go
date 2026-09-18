package bundle_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/collection"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
)

const collectorPolicy = `{"schema":"readmit-receiver-policy/v1","name":"downstream-sink","source_label":"downstream-test-endpoint","acknowledgement":{"operator":"original-mode-fixed-code","code":"AA"},"accepted_message_types":{"operator":"any-message-type","values":[]}}`

func collectionRecord(t *testing.T) collection.Record {
	t.Helper()
	policy, err := collection.DecodePolicy([]byte(collectorPolicy))
	if err != nil {
		t.Fatal(err)
	}
	return collection.Record{
		Schema:                collection.Schema,
		SessionID:             "0123456789abcdef0123456789abcdef",
		Policy:                policy,
		ApplicationProcessing: collection.NoApplicationProcessing,
		Sessions:              []collection.Session{{SessionID: "c0001", SourceID: "s0001", Label: policy.SourceLabel}},
		Received: []collection.Received{{SessionID: "c0001", OccurrenceID: "s0001-e000001", ControlID: "LISTEN-BOOK", Mode: collection.OriginalMode,
			Accept:      collection.Stage{Code: collection.NotAcknowledged, Destination: collection.NoDestination},
			Application: collection.Stage{Code: "AA", ControlID: "READMITCOLLECT000001", Destination: collection.SameConnection}}},
	}
}

func collected(t *testing.T, record collection.Record) (string, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "case")
	started := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	input := bundle.Input{Data: mllp.Frame(fixture(t, "listen-s12.hl7")), Options: hl7.Options{Format: hl7.MLLP}, Observations: map[int]bundle.Observation{1: {Direction: bundle.Inbound, ObservedAt: &started}}}
	_, err := bundle.WriteCollected(path, []bundle.Input{input}, started, record)
	return path, err
}

func TestCollectedEvidenceHasDistinctVersionAndLabelledSessions(t *testing.T) {
	path, err := collected(t, collectionRecord(t))
	if err != nil {
		t.Fatal(err)
	}
	opened, err := bundle.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if opened.Manifest.Schema != "readmit-case/v4" || opened.Manifest.Provenance.Mode != bundle.Collected || opened.Manifest.Observation != nil {
		t.Fatalf("collected evidence reused another case version: %+v", opened.Manifest)
	}
	if opened.Collection == nil || opened.Collection.Sessions[0].Label != "downstream-test-endpoint" || opened.Manifest.Collection.Path != "collection.json" {
		t.Fatalf("collected evidence lost its explicit source label: %+v", opened.Collection)
	}
	if opened.Collection.ApplicationProcessing != collection.NoApplicationProcessing {
		t.Fatal("collected evidence claims application processing")
	}
	if opened.Manifest.Sources[0].Path != "" || opened.Events[0].ImportedAt != nil {
		t.Fatal("collected evidence fabricated source metadata")
	}
}

func TestCollectedReaderRejectsForeignMembersAndForgedRecords(t *testing.T) {
	for _, change := range []struct{ from, to string }{
		{`"provenance":{`, `"provenance":{"imported_at":null,`},
		{`"provenance":{`, `"provenance":{"generator":null,`},
		{`"provenance":{`, `"provenance":{"derivation":null,`},
		{`"schema":`, `"observation":null,"schema":`},
		{`"sources":[{`, `"sources":[{"path":"",`},
	} {
		path, err := collected(t, collectionRecord(t))
		if err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(path, "manifest.json")
		original, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, bytes.Replace(original, []byte(change.from), []byte(change.to), 1), 0600); err != nil {
			t.Fatal(err)
		}
		rehashCaseVersion(t, path, "readmit-case/v4")
		if _, err := bundle.Open(path); err == nil {
			t.Fatalf("v4 accepted %q after rehashing", change.to)
		}
	}
}

func TestCollectionRecordMustAgreeWithRetainedEvidence(t *testing.T) {
	for name, mutate := range map[string]func(r *collection.Record){
		"unknown occurrence":  func(r *collection.Record) { r.Received[0].OccurrenceID = "s0001-e000009" },
		"invented control ID": func(r *collection.Record) { r.Received[0].ControlID = "OTHER-CONTROL" },
		"unknown source":      func(r *collection.Record) { r.Sessions[0].SourceID = "s0002" },
		"anonymous message": func(r *collection.Record) {
			r.Received[0].ControlID = ""
			r.Received[0].Application = collection.Stage{Code: collection.NotAcknowledged, Destination: collection.NoDestination}
		},
	} {
		record := collectionRecord(t)
		mutate(&record)
		if _, err := collected(t, record); err == nil {
			t.Errorf("collected writer accepted %s", name)
		}
	}
}

func TestCollectedRecordIsCoveredByIdentityAndEvidenceReferences(t *testing.T) {
	for _, tc := range []struct {
		name, from, to string
		rehash         bool
	}{
		{"identity", "downstream-test-endpoint", "other-labelled-endpoint", false},
		{"session", "0123456789abcdef0123456789abcdef", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", true},
		{"control", "LISTEN-BOOK", "OTHER-CONTROL", true},
		{"occurrence", "s0001-e000001", "s0001-e000003", true},
		{"unknown-member", `"received":`, `"typo":true,"received":`, true},
		{"claimed processing", `"application_processing":"none"`, `"application_processing":"done"`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path, err := collected(t, collectionRecord(t))
			if err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(path, "collection.json")
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			data = bytes.Replace(data, []byte(tc.from), []byte(tc.to), 1)
			if err := os.WriteFile(file, data, 0600); err != nil {
				t.Fatal(err)
			}
			if tc.rehash {
				manifestFile := filepath.Join(path, "manifest.json")
				manifestData, err := os.ReadFile(manifestFile)
				if err != nil {
					t.Fatal(err)
				}
				var manifest bundle.Manifest
				if err := json.Unmarshal(manifestData, &manifest); err != nil {
					t.Fatal(err)
				}
				sum := sha256.Sum256(data)
				manifest.Collection.Size = len(data)
				manifest.Collection.SHA256 = hex.EncodeToString(sum[:])
				encoded, _ := json.Marshal(manifest, json.Deterministic(true))
				if err := os.WriteFile(manifestFile, append(encoded, '\n'), 0600); err != nil {
					t.Fatal(err)
				}
				rehashCaseVersion(t, path, "readmit-case/v4")
			}
			if _, err := bundle.Open(path); err == nil {
				t.Fatal("accepted an altered collection record")
			}
		})
	}
}

func TestLegacyCaseVersionsRejectCollectionMemberEvenNull(t *testing.T) {
	for _, version := range []string{"readmit-case/v1", "readmit-case/v2", "readmit-case/v3"} {
		t.Run(version, func(t *testing.T) {
			var path string
			switch version {
			case "readmit-case/v1":
				path, _ = write(t, []bundle.Input{{Path: "original", Data: fixture(t, "listen-s12.hl7")}}, imported())
			case "readmit-case/v2":
				path = recorded(t)
			default:
				path, _ = write(t, []bundle.Input{{Data: fixture(t, "listen-s12.hl7")}}, bundle.Provenance{Mode: bundle.Derived, Derivation: "readmit-redact/v1"})
			}
			file := filepath.Join(path, "manifest.json")
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			raw = bytes.Replace(raw, []byte(`"schema":`), []byte(`"collection":null,"schema":`), 1)
			if err := os.WriteFile(file, raw, 0600); err != nil {
				t.Fatal(err)
			}
			rehashCaseVersion(t, path, version)
			if _, err := bundle.Open(path); err == nil {
				t.Fatal("legacy schema silently gained a v4 member")
			}
		})
	}
}

// A readmit-case/v4 case sealed before enhanced acknowledgement workflows
// existed carries a readmit-collection/v1 record. The v4 reader must still open
// it, unchanged, rather than requiring the newer record version.
func TestCollectedEvidenceKeepsReadingTheEarlierCollectionRecord(t *testing.T) {
	record := collectionRecord(t)
	record.Schema = collection.SchemaV1
	record.Received[0].Application = collection.Stage{Code: "AA", Destination: collection.SameConnection}
	path, err := collected(t, record)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := bundle.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if opened.Manifest.Schema != "readmit-case/v4" || opened.Collection.Schema != collection.SchemaV1 {
		t.Fatalf("an earlier collection record was not read as itself: %s %s", opened.Manifest.Schema, opened.Collection.Schema)
	}
	sealed, err := os.ReadFile(filepath.Join(path, "collection.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, added := range []string{`"mode"`, `"accept"`, `"application"`} {
		if bytes.Contains(sealed, []byte(added)) {
			t.Errorf("the v1 record acquired the v2 member %s", added)
		}
	}
	entry := opened.Collection.Received[0]
	if entry.Mode != collection.OriginalMode || entry.Application.Code != "AA" || entry.Accept.Code != collection.NotAcknowledged {
		t.Fatalf("the v1 record was not read as one original-mode application acknowledgement: %+v", entry)
	}
}
