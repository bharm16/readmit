package bundle_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/mllp"
	"github.com/bharm16/readmit/internal/observation"
)

func recorded(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "case")
	started := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	snapshot := observation.Snapshot{Schema: observation.Schema, Profile: observation.Profile, SessionID: "0123456789abcdef0123456789abcdef", Mode: observation.Fixed, Consistent: true, Processed: []observation.Occurrence{{OccurrenceID: "s0001-e000001", ControlID: "LISTEN-BOOK"}}, Records: []observation.Record{}}
	_, err := bundle.WriteRecorded(path, []bundle.Input{{Data: mllp.Frame(fixture(t, "listen-s12.hl7")), Options: hl7.Options{Format: hl7.MLLP}, Observations: map[int]bundle.Observation{1: {Direction: bundle.Inbound, ObservedAt: &started}}}}, started, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func rehashV2(t *testing.T, path string) {
	t.Helper()
	files := directoryFiles(t, path)
	delete(files, "identity.sha256")
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	var stream bytes.Buffer
	stream.WriteString("readmit-case/v2\n")
	for _, name := range names {
		binary.Write(&stream, binary.BigEndian, uint64(len(name)))
		stream.WriteString(name)
		binary.Write(&stream, binary.BigEndian, uint64(len(files[name])))
		stream.Write(files[name])
	}
	digest := sha256.Sum256(stream.Bytes())
	if err := os.WriteFile(filepath.Join(path, "identity.sha256"), []byte(hex.EncodeToString(digest[:])+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestRecordedSnapshotIsCoveredByIdentityAndEvidenceReferences(t *testing.T) {
	for _, tc := range []struct {
		name, from, to string
		rehash         bool
	}{
		{"identity", "\"consistent\":true", "\"consistent\":false", false},
		{"session", "0123456789abcdef0123456789abcdef", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", true},
		{"control", "LISTEN-BOOK", "OTHER-CONTROL", true},
		{"occurrence", "s0001-e000001", "s0001-e000003", true},
		{"unknown-member", "\"consistent\":true", "\"consistent\":true,\"typo\":true", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := recorded(t)
			file := filepath.Join(path, "observation.json")
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
				manifestData, _ := os.ReadFile(manifestFile)
				var manifest bundle.Manifest
				if err := json.Unmarshal(manifestData, &manifest); err != nil {
					t.Fatal(err)
				}
				digest := sha256.Sum256(data)
				manifest.Observation.Size = len(data)
				manifest.Observation.SHA256 = hex.EncodeToString(digest[:])
				encoded, _ := json.Marshal(manifest, json.Deterministic(true))
				if err := os.WriteFile(manifestFile, append(encoded, '\n'), 0600); err != nil {
					t.Fatal(err)
				}
				rehashV2(t, path)
			}
			if _, err := bundle.Open(path); err == nil {
				t.Fatal("accepted altered recorded observation")
			}
		})
	}
}

func TestV1StillRejectsV2FieldsEvenWhenNull(t *testing.T) {
	path, _ := write(t, []bundle.Input{{Path: "synthetic", Data: fixture(t, "listen-s12.hl7")}}, imported())
	file := filepath.Join(path, "manifest.json")
	original, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []struct{ from, to string }{
		{`"schema":`, `"observation":null,"schema":`},
		{`"provenance":{`, `"provenance":{"session_id":"",`},
		{`"provenance":{`, `"provenance":{"started_at":null,`},
	} {
		modified := bytes.Replace(original, []byte(change.from), []byte(change.to), 1)
		if err := os.WriteFile(file, modified, 0600); err != nil {
			t.Fatal(err)
		}
		reseal(t, path)
		if _, err := bundle.Open(path); err == nil {
			t.Fatal("v1 reader accepted a v2 member")
		}
	}
}
