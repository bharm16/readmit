package casegen_test

import (
	"encoding/json/v2"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/casegen"
	"github.com/bharm16/readmit/internal/profileeval"
)

// resourceEncoding is testdata/casegen/resource-encoding.json: how each edition
// that generates S18 and S20 encodes a resource's participation, hand-authored
// from that edition's chapter 10 and Tables 0206, 0278 and 0354.
type resourceEncoding struct {
	Schema   string                     `json:"schema"`
	Note     string                     `json:"note"`
	Values   map[string]string          `json:"values"`
	Kinds    map[string]string          `json:"kinds"`
	Editions map[string]editionEncoding `json:"editions"`
}

type editionEncoding struct {
	Source        string                     `json:"source"`
	Structure     string                     `json:"structure"`
	MSH9Structure int                        `json:"msh9_structure"`
	RGSActionCode int                        `json:"rgs_action_code"`
	Segments      map[string]segmentEncoding `json:"segments"`
}

type segmentEncoding struct {
	ActionCode    int      `json:"action_code"`
	Identity      int      `json:"identity"`
	IdentityTypes []string `json:"identity_types"`
	// IdentityComponents are where the identity's code, its coding system or
	// assigning authority, and a person's name and identifier type codes sit.
	IdentityComponents map[string]int `json:"identity_components"`
	FillerStatus       int            `json:"filler_status"`
	FillerStatusTypes  []string       `json:"filler_status_types"`
}

func readResourceEncoding(t *testing.T) resourceEncoding {
	t.Helper()
	raw, err := os.ReadFile("../../testdata/casegen/resource-encoding.json")
	if err != nil {
		t.Fatal(err)
	}
	var e resourceEncoding
	if err := json.Unmarshal(raw, &e, json.RejectUnknownMembers(true)); err != nil || e.Schema != "readmit-casegen-resource-encoding/v1" {
		t.Fatalf("the resource encoding table: %v", err)
	}
	return e
}

// resourceRequest is the smallest workflow that books, cancels and replaces a
// resource of one kind beside one that never changes: the appointment books
// kept and old, S20 cancels old, and S18 adds new.
func resourceRequest(kind, mode string, profile, pack []byte, profileID, packID, packSchema string) []byte {
	person := map[string]any{"id": "S1", "family": "STAFF", "given": "", "authority": "N1", "id_type": "EI", "name_type": "L"}
	resource := func(id, identifier, initial string) map[string]any {
		return map[string]any{"id": id, "kind": "resource", "namespace": "L", "identifier": identifier, "appointment": "appt", "initial_state": initial}
	}
	binding := func(id string) map[string]any {
		b := map[string]any{"subject": id, "kind": kind, "text": "", "role": map[string]any{"code": "R1", "text": "", "system": "L"}}
		if kind == casegen.PersonnelResource {
			b["text"], b["identifier_type"], b["name_type"] = "STAFF", "EI", "L"
		}
		return b
	}
	document := map[string]any{"schema": "readmit-scenario/v2", "scenario": map[string]any{"id": "encoding-" + kind, "version": "1"},
		"profile": "readmit-siu-lifecycle-v2", "base_time": "2026-01-01T12:00:00Z",
		"subjects": []any{
			map[string]any{"id": "patient-a", "kind": "patient", "namespace": "N1", "identifier": "P1", "initial_state": "active"},
			map[string]any{"id": "appt", "kind": "appointment", "namespace": "N1", "identifier": "A1", "patient": "patient-a", "initial_state": "none"},
			resource("kept", "K1", "booked"), resource("old", "O1", "booked"), resource("new", "N1", "none")},
		"steps": []any{
			map[string]any{"id": "book", "event": "S12", "subject": "appt", "after": "0s", "expect": "accepted"},
			map[string]any{"id": "cancel-old", "event": "S20", "subject": "old", "after": "1m", "expect": "accepted"},
			map[string]any{"id": "add-new", "event": "S18", "subject": "new", "after": "2m", "expect": "accepted"}}}
	request := map[string]any{"schema": casegen.Schema, "generator_version": casegen.Version, "scenario": document,
		"profile": map[string]any{"schema": "readmit-local-profile/v1", "id": profileID, "version": "1", "sha256": sum(profile)},
		"pack":    map[string]any{"schema": packSchema, "id": packID, "version": "1", "sha256": sum(pack)},
		"seed":    0, "wire": map[string]any{"delimiters": `|^~\&`, "precision": "second", "offset": "+00:00", "processing_id": "T",
			"sending": map[string]any{"application": "RM", "facility": "SY"}, "receiving": map[string]any{"application": "RC", "facility": "SY"}, "resource_updates": mode},
		"bindings": map[string]any{"patients": []any{map[string]any{"subject": "patient-a", "identifier_type": "MR", "additional_identifiers": []any{}}},
			"visits": []any{}, "orders": []any{}, "edits": []any{},
			"appointments": []any{map[string]any{"subject": "appt", "placer": nil, "start_after": "24h", "duration_minutes": 30,
				"reason": map[string]any{"code": "R1", "text": "", "system": "L"}, "contact": person, "entered_by": person, "resources": []any{}}},
			"resources": []any{binding("kept"), binding("old"), binding("new")}},
		"rows":     []any{map[string]any{"id": "row", "family": "DOE", "given": "", "notes": []any{}, "charset": "UNICODE UTF-8"}},
		"variants": []any{map[string]any{"id": "baseline", "polarity": "positive", "mutations": []any{}}}}
	raw, err := json.Marshal(request)
	if err != nil {
		panic(err)
	}
	return raw
}

// resourceModes are the two resource update modes every edition is held in.
var resourceModes = []string{casegen.ActionCodeUpdates, casegen.SnapshotUpdates}

// checkResourceEncoding holds one edition's S18 and S20 messages to its
// hand-authored encoding. It first checks that the edition's own pack
// declares each position with the data type and table the table names, then,
// for every resource kind and both update modes, generates a booking, a
// cancellation of participation and a replacement, and reads each message with
// the independent target's own parser at the table's positions: the trigger
// and structure, RGS-2, each resource's identity, action code and filler
// status, a cancellation that stays a cancellation and never a deletion, the
// replacement's order, and the resource that never changes. It returns the
// number of assertions made. Evaluator findings are left as they are: a
// generated positive fails nothing, and an undecided verdict stays undecided.
func checkResourceEncoding(t *testing.T, version string, pack []byte, packID, packSchema string, table resourceEncoding) int {
	t.Helper()
	e, ok := table.Editions[version]
	if !ok {
		t.Fatalf("no hand-authored encoding for %s", version)
	}
	checks := 0
	check := func(ok bool, format string, args ...any) {
		t.Helper()
		checks++
		if !ok {
			t.Errorf(version+": "+format, args...)
		}
	}
	k, err := profileeval.DecodePack(pack)
	if err != nil {
		t.Fatal(err)
	}
	for _, structure := range []string{"SIU_S18", "SIU_S20"} {
		i := slices.IndexFunc(k.Messages, func(m profileeval.MessageRule) bool { return m.HL7Version == version && m.Structure == structure })
		check(i >= 0, "the pack declares no %s", structure)
		if i < 0 {
			continue
		}
		fields := map[string]map[int]profileeval.FieldRule{}
		for _, s := range k.Messages[i].Segments {
			fields[s.ID] = map[int]profileeval.FieldRule{}
			for _, f := range s.Fields {
				fields[s.ID][f.Position] = f
			}
		}
		rgs := fields["RGS"][e.RGSActionCode]
		check(rgs.DataType == "ID" && rgs.Table == "0206", "%s RGS-%d is %s/%s", structure, e.RGSActionCode, rgs.DataType, rgs.Table)
		for segment, s := range e.Segments {
			action, identity, status := fields[segment][s.ActionCode], fields[segment][s.Identity], fields[segment][s.FillerStatus]
			check(action.DataType == "ID" && action.Table == "0206", "%s %s-%d is %s/%s", structure, segment, s.ActionCode, action.DataType, action.Table)
			check(slices.Contains(s.IdentityTypes, identity.DataType), "%s %s-%d is %s", structure, segment, s.Identity, identity.DataType)
			check(slices.Contains(s.FillerStatusTypes, status.DataType) && status.Table == "0278", "%s %s-%d is %s/%s", structure, segment, s.FillerStatus, status.DataType, status.Table)
		}
	}
	v := table.Values
	type held struct{ action, id, status string }
	// Every resource is coded K1, O1 or N1 in the coding system L, and a
	// personnel resource declares the identifier type EI and the name type L.
	identity := map[string]string{"authority": "L"}
	personnel := map[string]string{"authority": "L", "name_type": "L", "identifier_type": "EI"}
	for kind, segment := range table.Kinds {
		s := e.Segments[segment]
		profileID := "encoding-" + strings.ReplaceAll(version, ".", "-")
		profile := localProfile(profileID, packID, version, "SIU")
		for _, mode := range resourceModes {
			name := fmt.Sprintf("%s %s %s", version, kind, mode)
			g, err := casegen.Generate(t.Context(), resourceRequest(kind, mode, profile, pack, profileID, packID, packSchema), profile, pack, casegen.Options{})
			if err != nil {
				t.Errorf("%s: %v", name, err)
				continue
			}
			c, messages := caseOf(t, g, "baseline")
			steps := []string{}
			for _, o := range c.Occurrences {
				steps = append(steps, o.Step+" "+o.Event)
			}
			check(slices.Equal(steps, []string{"book S12", "cancel-old S20", "add-new S18"}), "%s: the replacement's order is %v", name, steps)
			for _, support := range g.Record.Support {
				if support.Event == "S18" || support.Event == "S20" {
					check(support.Structure == e.Structure, "%s: %s is generated as %s", name, support.Event, support.Structure)
				}
			}
			for _, f := range c.Validation.Report.Findings {
				check(f.Outcome != "fail", "%s: a generated positive fails %s", name, f.Rule)
			}
			read := func(raw []byte) (message, []held) {
				m := split(string(raw))
				out := []held{}
				for other := range e.Segments {
					if other != segment {
						check(len(m.segments[other]) == 0, "%s: a %s resource was written as %s", name, kind, other)
					}
				}
				for _, fields := range m.segments[segment] {
					component := func(n, c int) string {
						if n < len(fields) {
							if parts := strings.Split(fields[n], m.comp); c <= len(parts) {
								return parts[c-1]
							}
						}
						return ""
					}
					at := func(n int) string { return component(n, 1) }
					want := identity
					if segment == "AIP" {
						want = personnel
					}
					for part, value := range want {
						got := component(s.Identity, s.IdentityComponents[part])
						check(got == value, "%s: %s-%d.%d (%s) is %q", name, segment, s.Identity, s.IdentityComponents[part], part, got)
					}
					out = append(out, held{at(s.ActionCode), component(s.Identity, s.IdentityComponents["code"]), at(s.FillerStatus)})
					check(at(s.ActionCode) != v["delete"] && at(s.FillerStatus) != v["deleted"], "%s: a resource was deleted", name)
				}
				return m, out
			}
			booked, cancelled := v["booked"], v["cancelled"]
			want := [][]held{{{"", "K1", booked}, {"", "O1", booked}}, {{v["update"], "O1", cancelled}}, {{v["add"], "N1", booked}}}
			rgs := []string{"", v["update"], v["update"]}
			if mode == casegen.SnapshotUpdates {
				want = [][]held{{{"", "K1", booked}, {"", "O1", booked}}, {{"", "K1", booked}, {"", "O1", cancelled}}, {{"", "K1", booked}, {"", "O1", cancelled}, {"", "N1", booked}}}
				rgs = []string{"", "", ""}
			}
			for i, event := range []string{"S12", "S20", "S18"} {
				m, got := read(messages[i])
				structure := m.get("MSH", 9, e.MSH9Structure)
				check(m.get("MSH", 9, 1) == "SIU" && m.get("MSH", 9, 2) == event && structure == e.Structure, "%s: message %d is %s^%s^%s", name, i+1, m.get("MSH", 9, 1), m.get("MSH", 9, 2), structure)
				check(m.get("RGS", e.RGSActionCode, 1) == rgs[i], "%s: %s RGS-%d is %q", name, event, e.RGSActionCode, m.get("RGS", e.RGSActionCode, 1))
				check(slices.Equal(got, want[i]), "%s: %s holds %+v, not %+v", name, event, got, want[i])
			}
		}
	}
	return checks
}

// The owned edition's S18 and S20 messages hold to its hand-authored encoding
// in every resource kind and both update modes. The pinned editions are held
// to theirs by the pack-backed qualification (TestPinnedGenerationMatrix).
func TestResourceEncodingHoldsInTheOwnedEdition(t *testing.T) {
	pack, err := os.ReadFile("../../testdata/casegen/owned/pack.json")
	if err != nil {
		t.Fatal(err)
	}
	checkResourceEncoding(t, "2.5.1", pack, "owned-casegen-2-5-1", "readmit-profile-pack/v5", readResourceEncoding(t))
}

// The hand-authored encoding covers exactly the editions the retained matrix
// generates S18 and S20 in, so no edition claims support without its
// resource-encoding evidence. It reads no pack.
func TestResourceEncodingCoversEveryEditionThatGeneratesResourceEvents(t *testing.T) {
	raw, err := os.ReadFile("../../docs/scenario-generation-matrix.json")
	if err != nil {
		t.Fatal(err)
	}
	var m generationMatrix
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	generated := map[string]bool{}
	for _, row := range m.Rows {
		if (row.Event == "S18" || row.Event == "S20") && row.Status == "generated" {
			generated[row.HL7Version] = true
		}
	}
	table := readResourceEncoding(t)
	for version := range table.Editions {
		if !generated[version] {
			t.Errorf("%s has an encoding but generates no S18 or S20", version)
		}
	}
	for version := range generated {
		if _, ok := table.Editions[version]; !ok {
			t.Errorf("%s generates S18 or S20 without a hand-authored encoding", version)
		}
	}
}
