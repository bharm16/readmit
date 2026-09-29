package casegen_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/casegen"
	"github.com/bharm16/readmit/internal/profileeval"
	"github.com/bharm16/readmit/internal/scenario"
)

// eventCase is one lifecycle event taken from the state it is first taken
// from, as the smallest scenario that sends it. Identifiers are short enough
// for every edition's field lengths, so the matrix states what an edition
// requires of the event rather than of fixture data.
type eventCase struct {
	lifecycle, family, event string
	initial                  string
}

var events = []eventCase{
	{"readmit-adt-lifecycle-v1", "ADT", "A04", "none"}, {"readmit-adt-lifecycle-v1", "ADT", "A01", "none"},
	{"readmit-adt-lifecycle-v1", "ADT", "A02", "admitted"}, {"readmit-adt-lifecycle-v1", "ADT", "A08", "admitted"},
	{"readmit-adt-lifecycle-v1", "ADT", "A03", "admitted"}, {"readmit-adt-lifecycle-v1", "ADT", "A11", "admitted"},
	{"readmit-adt-lifecycle-v1", "ADT", "A13", "discharged"}, {"readmit-adt-lifecycle-v1", "ADT", "A40", "active"},
	{"readmit-siu-lifecycle-v1", "SIU", "S12", "none"}, {"readmit-siu-lifecycle-v1", "SIU", "S13", "booked"},
	{"readmit-siu-lifecycle-v1", "SIU", "S14", "booked"}, {"readmit-siu-lifecycle-v1", "SIU", "S15", "booked"},
	{"readmit-siu-lifecycle-v1", "SIU", "S26", "booked"},
	{"readmit-siu-lifecycle-v2", "SIU", "S18", "none"}, {"readmit-siu-lifecycle-v2", "SIU", "S20", "booked"},
	{"readmit-orm-lifecycle-v1", "ORM", "ORM-NW", "none"}, {"readmit-orm-lifecycle-v1", "ORM", "ORM-XO", "ordered"},
	{"readmit-orm-lifecycle-v1", "ORM", "ORM-CA", "ordered"},
	{"readmit-oru-lifecycle-v1", "ORU", "ORU-P", "ordered"}, {"readmit-oru-lifecycle-v1", "ORU", "ORU-F", "ordered"},
	{"readmit-oru-lifecycle-v1", "ORU", "ORU-C", "final"},
}

var matrixVersions = []string{"2.3.1", "2.4", "2.5", "2.5.1", "2.6", "2.7.1", "2.8.2"}

func sum(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

// eventRequest is one event's request under the given profile and pack.
func eventRequest(e eventCase, profile, pack []byte, profileID, packID string, packSchema string) []byte {
	patient := map[string]any{"id": "patient-a", "kind": "patient", "namespace": "N1", "identifier": "P1", "initial_state": "active"}
	subjects := []any{patient}
	steps := []any{}
	bindings := map[string]any{"patients": []any{map[string]any{"subject": "patient-a", "identifier_type": "MR", "additional_identifiers": []any{}}},
		"visits": []any{}, "appointments": []any{}, "resources": []any{}, "orders": []any{}, "edits": []any{}}
	document := map[string]any{"schema": "readmit-scenario/v1", "scenario": map[string]any{"id": "matrix-" + strings.ToLower(e.event), "version": "1"},
		"profile": e.lifecycle, "base_time": "2026-01-01T12:00:00Z"}
	step := map[string]any{"id": "step", "event": e.event, "subject": "subject", "after": "0s", "expect": "accepted"}
	person := map[string]any{"id": "S1", "family": "STAFF", "given": "", "authority": "N1", "id_type": "EI", "name_type": "L"}
	switch e.family {
	case "ADT":
		if e.event == "A40" {
			subjects = append(subjects, map[string]any{"id": "patient-b", "kind": "patient", "namespace": "N1", "identifier": "P2", "initial_state": "active"})
			bindings["patients"] = append(bindings["patients"].([]any), map[string]any{"subject": "patient-b", "identifier_type": "MR", "additional_identifiers": []any{}})
			step["subject"], step["into"] = "patient-b", "patient-a"
			break
		}
		subjects = append(subjects, map[string]any{"id": "subject", "kind": "visit", "namespace": "N1", "identifier": "V1", "patient": "patient-a", "initial_state": e.initial})
		bindings["visits"] = []any{map[string]any{"subject": "subject", "class": "I", "identifier_type": "VN", "location": map[string]any{"point_of_care": "W1", "room": "1", "bed": "1"}}}
		if e.event == "A02" {
			bindings["edits"] = []any{map[string]any{"step": "step", "op": "relocate", "location": map[string]any{"point_of_care": "W2", "room": "2", "bed": "2"}}}
		}
	case "SIU":
		subjects = append(subjects, map[string]any{"id": "subject", "kind": "appointment", "namespace": "N1", "identifier": "A1", "patient": "patient-a", "initial_state": e.initial})
		bindings["appointments"] = []any{map[string]any{"subject": "subject", "placer": nil, "start_after": "24h", "duration_minutes": 30,
			"reason": map[string]any{"code": "R1", "text": "", "system": "L"}, "contact": person, "entered_by": person,
			"resources": []any{map[string]any{"id": "service", "kind": "service", "identifier": map[string]any{"code": "C1", "text": "", "system": "L"}, "role": map[string]any{"code": "R1", "text": "", "system": "L"}}}}}
		if e.lifecycle == string(scenario.SIUResourceLifecycle) {
			// The resource lifecycle's events name a resource subject of a
			// booked appointment: an addition starts from none, a
			// cancellation of participation from booked.
			document["schema"] = scenario.ResourceSchema
			subjects[1].(map[string]any)["initial_state"] = "booked"
			subjects = append(subjects, map[string]any{"id": "resource", "kind": "resource", "namespace": "L", "identifier": "G1", "appointment": "subject", "initial_state": e.initial})
			bindings["resources"] = []any{map[string]any{"subject": "resource", "kind": "general", "text": "", "role": map[string]any{"code": "R1", "text": "", "system": "L"}}}
			step["subject"] = "resource"
		}
	case "ORM", "ORU":
		document["schema"] = "readmit-order-scenario/v1"
		subjects = append(subjects, map[string]any{"id": "subject", "kind": "order", "namespace": "N1", "identifier": "O1", "patient": "patient-a", "initial_state": e.initial})
		document["orders"] = []any{map[string]any{"subject": "subject", "placer": map[string]any{"namespace": "PL", "identifier": "O1"}, "filler": map[string]any{"namespace": "FL", "identifier": "F1"}}}
		document["results"] = []any{}
		if e.family == "ORU" {
			status := strings.TrimPrefix(e.event, "ORU-")
			document["results"] = []any{map[string]any{"step": "step", "observations": []any{
				map[string]any{"code": "T1", "sub_id": "1", "value": "One", "status": status}, map[string]any{"code": "T1", "sub_id": "2", "value": "Two", "status": status}}}}
		}
		bindings["orders"] = []any{map[string]any{"subject": "subject", "service": map[string]any{"code": "S1", "text": "", "system": "L"}, "observation_system": "L"}}
	}
	document["subjects"], document["steps"] = subjects, append(steps, step)
	request := map[string]any{"schema": casegen.Schema, "generator_version": casegen.Version, "scenario": document,
		"profile": map[string]any{"schema": "readmit-local-profile/v1", "id": profileID, "version": "1", "sha256": sum(profile)},
		"pack":    map[string]any{"schema": packSchema, "id": packID, "version": "1", "sha256": sum(pack)},
		"seed":    0, "wire": map[string]any{"delimiters": `|^~\&`, "precision": "second", "offset": "+00:00", "processing_id": "T",
			"sending": map[string]any{"application": "RM", "facility": "SY"}, "receiving": map[string]any{"application": "RC", "facility": "SY"}, "resource_updates": "action-code"},
		"bindings": bindings, "rows": []any{map[string]any{"id": "row", "family": "DOE", "given": "", "notes": []any{}, "charset": "UNICODE UTF-8"}},
		"variants": []any{map[string]any{"id": "baseline", "polarity": "positive", "mutations": []any{}}}}
	raw, err := json.Marshal(request)
	if err != nil {
		panic(err)
	}
	return raw
}

// localProfile pins a pack for one family with one local rule every message
// meets: MSH-10 present.
func localProfile(id, packID, version, family string) []byte {
	return fmt.Appendf(nil, `{"schema":"readmit-local-profile/v1","profile":{"id":%q,"version":"1"},"base":{"pack":{"id":%q,"version":"1"},"hl7_version":%q,"family":%q},"segments":[{"id":"MSH","fields":[{"position":10,"usage":"R"}]}]}`, id, packID, version, family)
}

// matrixRow is one event's outcome under one edition's pack.
type matrixRow struct {
	HL7Version string   `json:"hl7_version"`
	Lifecycle  string   `json:"lifecycle"`
	Event      string   `json:"event"`
	Trigger    string   `json:"trigger"`
	Structure  string   `json:"structure,omitzero"`
	Status     string   `json:"status"`
	Verdict    string   `json:"verdict,omitzero"`
	Undecided  []string `json:"undecided_rules,omitzero"`
	Reason     string   `json:"reason,omitzero"`
}

func matrixOf(t *testing.T, version string, pack []byte, packID, packSchema string) []matrixRow {
	t.Helper()
	rows := []matrixRow{}
	for _, e := range events {
		profileID := "matrix-" + strings.ToLower(e.family)
		profile := localProfile(profileID, packID, version, e.family)
		g, err := casegen.Generate(context.Background(), eventRequest(e, profile, pack, profileID, packID, packSchema), profile, pack, casegen.Options{})
		row := matrixRow{HL7Version: version, Lifecycle: e.lifecycle, Event: e.event}
		var unsupported *casegen.UnsupportedError
		switch {
		case errors.As(err, &unsupported):
			s := unsupported.Support[0]
			row.Trigger, row.Status, row.Reason = s.Trigger, s.Status, s.Reason
		case err != nil:
			t.Fatalf("%s %s: %v", version, e.event, err)
		default:
			c := g.Record.Cases[0]
			row.Trigger, row.Structure, row.Status, row.Verdict = g.Record.Support[0].Trigger, g.Record.Support[0].Structure, "generated", c.Validation.Verdict
			seen := map[string]bool{}
			for _, f := range c.Validation.Report.Findings {
				if f.Outcome == "fail" {
					t.Fatalf("%s %s: a generated positive case fails %s", version, e.event, f.Rule)
				}
				if !seen[f.Rule] {
					seen[f.Rule] = true
					row.Undecided = append(row.Undecided, f.Rule)
				}
			}
			slices.Sort(row.Undecided)
		}
		rows = append(rows, row)
	}
	return rows
}

// The owned pack supports every event the lifecycle profiles declare, and the
// matrix harness generates and passes each one against it.
func TestOwnedPackGeneratesEveryDeclaredEvent(t *testing.T) {
	pack, err := os.ReadFile("../../testdata/casegen/owned/pack.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range matrixOf(t, "2.5.1", pack, "owned-casegen-2-5-1", "readmit-profile-pack/v5") {
		if row.Status != "generated" || row.Verdict != "pass" {
			t.Errorf("%s: %+v", row.Event, row)
		}
	}
}

// Opt-in: the packs built from HL7's own files are not distributed (#627). The
// retained matrix records, for every declared event in every edition, whether
// the edition's own pack supports it and how its generated positive message
// evaluates; an unsupported event is named with its reason, never passed.
func TestPinnedGenerationMatrix(t *testing.T) {
	dir := os.Getenv("READMIT_PROFILE_EXTRACTION")
	required := os.Getenv("READMIT_REQUIRE_PROFILE_EXTRACTION") != ""
	update := os.Getenv("READMIT_UPDATE_GENERATION_MATRIX") != ""
	switch {
	case required && update:
		t.Fatal("qualification never updates the retained matrix")
	case dir == "" && required:
		t.Fatal("qualification requires READMIT_PROFILE_EXTRACTION to name the pinned packs")
	case dir == "":
		t.Skip("set READMIT_PROFILE_EXTRACTION to the pinned offline extraction")
	}
	var receipt struct {
		Packs map[string]struct {
			SHA256 string `json:"sha256"`
		} `json:"packs"`
	}
	raw, err := os.ReadFile("../../docs/profile-extraction-v5-receipt.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &receipt); err != nil {
		t.Fatal(err)
	}
	got := generationMatrix{Schema: "readmit-scenario-generation-matrix/v1", Generator: casegen.Version, Packs: map[string]string{}, Rows: []matrixRow{}}
	packs := map[string][]byte{}
	for _, version := range matrixVersions {
		pack, err := os.ReadFile(filepath.Join(dir, "pack-"+version+".json"))
		if err != nil {
			t.Fatalf("pack %s is missing: %v", version, err)
		}
		if sum(pack) != receipt.Packs["pack-"+version+".json"].SHA256 {
			t.Fatalf("pack %s differs from the v5 receipt", version)
		}
		got.Packs[version], packs[version] = sum(pack), pack
		got.Rows = append(got.Rows, matrixOf(t, version, pack, "hl7-v2-"+strings.ReplaceAll(version, ".", "-"), "readmit-profile-pack/v5")...)
	}
	encoded, err := json.Marshal(got, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	if update {
		if err := os.WriteFile("../../docs/scenario-generation-matrix.json", append(indentJSON(t, encoded), '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	retained, err := os.ReadFile("../../docs/scenario-generation-matrix.json")
	if err != nil {
		t.Fatal(err)
	}
	var want generationMatrix
	if err := json.Unmarshal(retained, &want); err != nil {
		t.Fatal(err)
	}
	wantEncoded, _ := json.Marshal(want, json.Deterministic(true))
	if string(wantEncoded) != string(encoded) {
		t.Fatal("generation against the pinned packs differs from the retained matrix; rerun with READMIT_UPDATE_GENERATION_MATRIX=1 and review the change")
	}
	// Every edition that generates S18 and S20 holds to its hand-authored
	// resource encoding before anything is receipted.
	table := readResourceEncoding(t)
	encoding := resourceEncodingOutcome{Editions: []string{}, Kinds: len(table.Kinds), Modes: len(resourceModes)}
	for _, version := range matrixVersions {
		if _, ok := table.Editions[version]; ok {
			encoding.Editions = append(encoding.Editions, version)
			encoding.Assertions += checkResourceEncoding(t, version, packs[version], "hl7-v2-"+strings.ReplaceAll(version, ".", "-"), "readmit-profile-pack/v5", table)
		}
	}
	if t.Failed() {
		t.FailNow()
	}
	if path := os.Getenv("READMIT_GENERATION_QUALIFICATION_RECEIPT"); path != "" {
		writeQualificationReceipt(t, path, got, retained, encoding)
	}
}

// resourceEncodingOutcome is what the per-edition resource-encoding check
// covered: the editions, how many resource kinds and update modes in each, and
// the assertions made. Every one held, or no receipt is written.
type resourceEncodingOutcome struct {
	Editions   []string `json:"editions"`
	Kinds      int      `json:"kinds"`
	Modes      int      `json:"modes"`
	Assertions int      `json:"assertions"`
}

// writeQualificationReceipt records what a qualification run proved: the
// committed revision and tree it ran on, the exact packs by digest, the
// generator and evaluator versions, the command, the outcome counts and the
// resource-encoding check. It holds digests and counts only, never pack
// content. The outcome counts keep every undecided verdict undecided.
func writeQualificationReceipt(t *testing.T, path string, got generationMatrix, retained []byte, encoding resourceEncodingOutcome) {
	t.Helper()
	counts := map[string]int{}
	for _, row := range got.Rows {
		counts[row.Status]++
		if row.Verdict != "" {
			counts[row.Status+"-"+row.Verdict]++
		}
	}
	receipt := map[string]any{
		"schema": "readmit-scenario-generation-qualification/v2", "revision": os.Getenv("READMIT_QUALIFIED_REVISION"), "tree": os.Getenv("READMIT_QUALIFIED_TREE"),
		"generator_version": casegen.Version, "evaluator_operator": profileeval.UsageOperatorVersion, "go_version": runtime.Version(),
		"packs": got.Packs, "retained_matrix_sha256": sum(retained), "rows": len(got.Rows), "outcomes": counts,
		"command": "make qualify-generation-packs", "test": "go test -count=1 -short -tags readmit_nosync ./internal/casegen -run ^TestPinnedGenerationMatrix$",
		"resource_encoding": encoding,
		"result":            "generation against every pinned pack equals the retained matrix, and every edition that generates S18 and S20 holds to its hand-authored resource encoding",
	}
	encoded, err := json.Marshal(receipt, json.Deterministic(true), jsontext.WithIndent(" "))
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatalf("the receipt must be a new file: %v", err)
	}
	defer file.Close()
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		t.Fatal(err)
	}
}

type generationMatrix struct {
	Schema    string            `json:"schema"`
	Generator string            `json:"generator_version"`
	Packs     map[string]string `json:"packs"`
	Rows      []matrixRow       `json:"rows"`
}

func indentJSON(t *testing.T, raw []byte) []byte {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(v, json.Deterministic(true), jsontext.WithIndent(" "))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// The retained matrix covers every declared event in every edition, pins the
// reviewed packs, and states a reason for every unsupported event; a generated
// row never claims a failing verdict. It reads no pack, so it runs everywhere.
func TestRetainedGenerationMatrixCoversEveryEvent(t *testing.T) {
	raw, err := os.ReadFile("../../docs/scenario-generation-matrix.json")
	if err != nil {
		t.Fatal(err)
	}
	var m generationMatrix
	if err := json.Unmarshal(raw, &m, json.RejectUnknownMembers(true)); err != nil {
		t.Fatal(err)
	}
	var receipt struct {
		Packs map[string]struct {
			SHA256 string `json:"sha256"`
		} `json:"packs"`
	}
	raw, err = os.ReadFile("../../docs/profile-extraction-v5-receipt.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &receipt); err != nil {
		t.Fatal(err)
	}
	if m.Schema != "readmit-scenario-generation-matrix/v1" || m.Generator != casegen.Version || len(m.Rows) != len(matrixVersions)*len(events) {
		t.Fatalf("incomplete matrix: %d rows", len(m.Rows))
	}
	for _, version := range matrixVersions {
		if m.Packs[version] == "" || m.Packs[version] != receipt.Packs["pack-"+version+".json"].SHA256 {
			t.Fatalf("matrix pins another pack for %s", version)
		}
	}
	for i, row := range m.Rows {
		e := events[i%len(events)]
		if row.HL7Version != matrixVersions[i/len(events)] || row.Event != e.event || row.Lifecycle != e.lifecycle || row.Trigger == "" {
			t.Fatalf("row %d is not %s %s", i, matrixVersions[i/len(events)], e.event)
		}
		switch row.Status {
		case "generated":
			if row.Structure == "" || row.Reason != "" || row.Verdict != "pass" && row.Verdict != "undecided" || row.Verdict == "undecided" && len(row.Undecided) == 0 {
				t.Fatalf("%s %s overstates a generated event", row.HL7Version, row.Event)
			}
		case "unsupported":
			if row.Reason == "" || row.Structure != "" || row.Verdict != "" {
				t.Fatalf("%s %s is unsupported without a reason", row.HL7Version, row.Event)
			}
		default:
			t.Fatalf("%s %s has no status", row.HL7Version, row.Event)
		}
	}
}
