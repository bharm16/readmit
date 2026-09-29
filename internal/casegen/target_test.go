package casegen_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/casegen"
	"github.com/bharm16/readmit/internal/replay"
)

// The golden messages are written by hand, field by field, from the documented
// mapping; the generator must produce each byte for byte.
func TestGeneratedMessagesEqualHandAuthoredGoldenBytes(t *testing.T) {
	var golden struct {
		Schema   string `json:"schema"`
		Note     string `json:"note"`
		Messages []struct {
			File    string `json:"file"`
			Request string `json:"request"`
			Profile string `json:"profile"`
			Row     string `json:"row"`
			Variant string `json:"variant"`
			Ordinal int    `json:"ordinal"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(read(t, "golden/golden.json"), &golden, json.RejectUnknownMembers(true)); err != nil {
		t.Fatal(err)
	}
	if golden.Schema != "readmit-casegen-golden/v1" || len(golden.Messages) == 0 {
		t.Fatal("no golden messages")
	}
	for _, m := range golden.Messages {
		t.Run(m.File, func(t *testing.T) {
			request, profile, pack := owned(t, m.Request, m.Profile)
			g := generate(t, request, profile, pack)
			for i, c := range g.Record.Cases {
				if c.Row == m.Row && c.Variant == m.Variant {
					if got, want := g.Messages(i)[m.Ordinal-1], read(t, "golden/"+m.File); !bytes.Equal(got, want) {
						t.Fatalf("generated\n%q\nwant\n%q", got, want)
					}
					return
				}
			}
			t.Fatal("no such case")
		})
	}
}

// target is an independent receiving application: an MLLP listener that keeps
// its own appointment, visit and result ledgers from the raw bytes it
// receives. It is written from the HL7 message descriptions alone and shares
// nothing with the generator, the scenario lifecycle or readmit's reader: it
// splits by the delimiters MSH declares and answers AA or AR.
type target struct {
	listener net.Listener
	mu       sync.Mutex
	// snapshot is the site convention this target was configured with: a
	// modifying scheduling message states the whole resource list. Without it
	// the target reads HL7's action code/unique identifier mode.
	snapshot  bool
	resources map[string]map[string]string
	ledger    map[string]string
	results   map[string]string
	seen      map[string]bool
	done      sync.WaitGroup
}

func startTarget(t *testing.T, snapshot bool) *target {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &target{listener: l, snapshot: snapshot, resources: map[string]map[string]string{}, ledger: map[string]string{}, results: map[string]string{}, seen: map[string]bool{}}
	s.done.Add(1)
	go func() {
		defer s.done.Done()
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			s.done.Add(1)
			go func() {
				defer s.done.Done()
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(5 * time.Second))
				r := bufio.NewReader(c)
				for {
					frame, err := r.ReadBytes(0x1c)
					if err != nil {
						return
					}
					if _, err := r.ReadByte(); err != nil {
						return
					}
					raw := string(bytes.TrimPrefix(frame[:len(frame)-1], []byte{0x0b}))
					control, code := s.apply(raw)
					ack := "MSH|^~\\&|TARGET|LAB|READMIT|SYNTHETIC|20260101000000||ACK|A" + control + "|T|2.5.1\rMSA|" + code + "|" + control + "\r"
					if _, err := c.Write(append(append([]byte{0x0b}, ack...), 0x1c, '\r')); err != nil {
						return
					}
				}
			}()
		}
	}()
	t.Cleanup(func() { l.Close(); s.done.Wait() })
	return s
}

// message is one received message split by its own declared delimiters.
type message struct {
	segments map[string][][]string
	comp     string
	rep      string
}

func split(raw string) message {
	m := message{segments: map[string][][]string{}}
	fieldSep := raw[3:4]
	m.comp, m.rep = raw[4:5], raw[5:6]
	for _, line := range strings.Split(strings.TrimRight(raw, "\r"), "\r") {
		fields := strings.Split(line, fieldSep)
		if fields[0] == "MSH" {
			fields = append([]string{"MSH", fieldSep}, fields[1:]...)
		}
		m.segments[fields[0]] = append(m.segments[fields[0]], fields)
	}
	return m
}

// get is SEG-n.c of the first repetition of the first such segment, "" where
// absent.
func (m message) get(segment string, n, c int) string {
	list := m.segments[segment]
	if len(list) == 0 || n >= len(list[0]) {
		return ""
	}
	parts := strings.Split(strings.Split(list[0][n], m.rep)[0], m.comp)
	if c > len(parts) {
		return ""
	}
	return parts[c-1]
}

// resource is one received service or resource segment: its action code, the
// identifier that names it across messages and its filler status.
type resource struct{ action, id, status string }

// statusField is where each resource segment carries its filler status.
var statusField = map[string]int{"AIS": 10, "AIG": 14, "AIL": 12, "AIP": 12}

func (m message) resources() []resource {
	out := []resource{}
	for _, seg := range []string{"AIS", "AIG", "AIL", "AIP"} {
		for _, fields := range m.segments[seg] {
			r := resource{}
			if len(fields) > 2 {
				r.action = fields[2]
			}
			if len(fields) > 3 {
				r.id = strings.Split(fields[3], m.comp)[0]
			}
			if n := statusField[seg]; len(fields) > n {
				r.status = strings.Split(fields[n], m.comp)[0]
			}
			out = append(out, r)
		}
	}
	return out
}

// update applies an updating scheduling message to the resources an
// appointment holds, by resource identifier and status. In action code mode
// (HL7 v2.5.1 section 2.10.4.2; chapter 10 requires the segment action code
// on every updating or modifying event) RGS-2 must be present and each
// resource segment adds (A), updates (U) or deletes (D) the resource its
// identifier names; a resource not sent is unchanged, and a segment without a
// valid action code is refused. Under the snapshot convention the message's
// resources replace the held ones and carry no action code. In either mode an
// addition (S18) must book a resource not booked, and a cancellation of
// participation (S20) may only cancel a booked resource.
func (s *target) update(m message, event string, held map[string]string) (map[string]string, bool) {
	next := map[string]string{}
	if s.snapshot {
		changed := false
		for _, r := range m.resources() {
			if r.action != "" || r.status == "" {
				return nil, false
			}
			next[r.id] = r.status
			changed = changed || event == "S18" && r.status == "Booked" && held[r.id] != "Booked" ||
				event == "S20" && r.status == "Cancelled" && held[r.id] == "Booked"
		}
		if (event == "S18" || event == "S20") && !changed {
			return nil, false
		}
		return next, true
	}
	if m.get("RGS", 2, 1) == "" {
		return nil, false
	}
	for id, status := range held {
		next[id] = status
	}
	for _, r := range m.resources() {
		_, exists := next[r.id]
		switch {
		case r.status == "":
			return nil, false
		case event == "S18" && (r.action != "A" || r.status != "Booked"):
			return nil, false
		case event == "S20" && (r.action != "U" || next[r.id] != "Booked" || r.status != "Cancelled"):
			return nil, false
		case r.action == "A" && !exists:
			next[r.id] = r.status
		case r.action == "U" && exists:
			next[r.id] = r.status
		case r.action == "D" && exists:
			delete(next, r.id)
		default:
			return nil, false
		}
	}
	return next, true
}

// apply is the target's own business rule for one message: a message whose
// control ID it already applied is acknowledged and not applied again.
func (s *target) apply(raw string) (string, string) {
	m := split(raw)
	s.mu.Lock()
	defer s.mu.Unlock()
	control := m.get("MSH", 10, 1)
	if s.seen[control] {
		return control, "AA"
	}
	s.seen[control] = true
	kind, event := m.get("MSH", 9, 1), m.get("MSH", 9, 2)
	switch kind {
	case "SIU":
		key := m.get("SCH", 2, 1) + "@" + m.get("SCH", 2, 2)
		if m.get("SCH", 2, 1) == "" {
			return control, "AR"
		}
		state := strings.Fields(s.ledger[key])
		status := ""
		if len(state) > 0 {
			status = state[0]
		}
		start := m.get("TQ1", 7, 1)
		if len(start) < 8 || start[4:6] < "01" || start[4:6] > "12" {
			return control, "AR"
		}
		held := s.resources[key]
		next := ""
		switch {
		case event == "S12" && status == "":
			next = "booked"
			held = map[string]string{}
			for _, r := range m.resources() {
				if r.action != "" || r.status == "" {
					return control, "AR"
				}
				held[r.id] = r.status
			}
		case (event == "S13" || event == "S14" || event == "S18" || event == "S20") && status == "booked":
			next = "booked"
			updated, ok := s.update(m, event, held)
			if !ok {
				return control, "AR"
			}
			held = updated
		case event == "S15" && status == "booked":
			next = "cancelled"
		case event == "S26" && status == "booked":
			next = "noshow"
		default:
			return control, "AR"
		}
		s.resources[key] = held
		names := []string{}
		for id, st := range held {
			names = append(names, id+":"+st)
		}
		sort.Strings(names)
		s.ledger[key] = next + " " + start + " " + strings.Join(names, ",")
	case "ADT":
		patient := m.get("PID", 3, 1) + "@" + m.get("PID", 3, 4)
		if strings.HasPrefix(s.ledger[patient], "merged") {
			return control, "AR"
		}
		if event == "A40" {
			prior := m.get("MRG", 1, 1) + "@" + m.get("MRG", 1, 4)
			if prior == patient || strings.HasPrefix(s.ledger[prior], "merged") {
				return control, "AR"
			}
			s.ledger[prior] = "merged " + patient
			return control, "AA"
		}
		visit := m.get("PV1", 19, 1) + "@" + m.get("PV1", 19, 4)
		state := strings.Fields(s.ledger[visit])
		status := ""
		if len(state) > 0 {
			status = state[0]
		}
		next := map[string]map[string]string{
			"A04": {"": "preadmit"}, "A01": {"": "admitted", "preadmit": "admitted"}, "A02": {"admitted": "admitted"},
			"A08": {"preadmit": "preadmit", "admitted": "admitted", "discharged": "discharged"}, "A03": {"admitted": "discharged"},
			"A11": {"admitted": "cancelled"}, "A13": {"discharged": "admitted"},
		}[event][status]
		if next == "" || m.get("PV1", 19, 1) == "" {
			return control, "AR"
		}
		s.ledger[visit] = next + " " + m.get("PV1", 3, 1) + " " + m.get("PID", 5, 1) + "^" + m.get("PID", 5, 2)
	case "ORM":
		order := m.get("ORC", 2, 1) + "@" + m.get("ORC", 2, 2)
		next := map[string]map[string]string{"NW": {"": "ordered"}, "XO": {"ordered": "ordered"}, "CA": {"ordered": "cancelled"}}[m.get("ORC", 1, 1)][s.ledger[order]]
		if next == "" {
			return control, "AR"
		}
		s.ledger[order] = next
	case "ORU":
		filler := m.get("OBR", 3, 1)
		status := m.get("OBR", 25, 1)
		if filler == "" {
			return control, "AR"
		}
		previous := s.results[filler]
		if status == "C" && previous != "F" && previous != "C" {
			return control, "AR"
		}
		if status != "C" && (previous == "F" || previous == "C") {
			return control, "AR"
		}
		s.results[filler] = status
		s.ledger[filler] = status + " " + fmt.Sprint(len(m.segments["OBX"]))
	default:
		return control, "AR"
	}
	return control, "AA"
}

func (s *target) ledgerView() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]string{}
	for k, v := range s.ledger {
		out[k] = v
	}
	return out
}

// Each case is sent one phase at a time through the product's own replay
// send, and after each phase the target's ledger and answers are held to the
// hand-authored expectations before the next phase runs, so a cancellation
// can never hide the reschedule it follows.
func TestCasesReachAnIndependentTargetWithExternallyDefinedOutcomes(t *testing.T) {
	var expectations struct {
		Schema string `json:"schema"`
		Note   string `json:"note"`
		Cases  []struct {
			Request         string `json:"request"`
			Profile         string `json:"profile"`
			Variant         string `json:"variant"`
			ResourceUpdates string `json:"resource_updates,omitzero"`
			Phases          []struct {
				Phase  string            `json:"phase"`
				ACKs   []string          `json:"acks"`
				Ledger map[string]string `json:"ledger"`
			} `json:"phases"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(read(t, "target-expectations.json"), &expectations, json.RejectUnknownMembers(true)); err != nil {
		t.Fatal(err)
	}
	if expectations.Schema != "readmit-casegen-target-expectations/v1" {
		t.Fatal(expectations.Schema)
	}
	written := map[string]string{}
	records := map[string]casegen.Record{}
	for _, c := range expectations.Cases {
		if _, ok := written[c.Request]; ok {
			continue
		}
		request, profile, pack := owned(t, c.Request, c.Profile)
		dir := t.TempDir()
		w, err := generate(t, request, profile, pack).Write(context.Background(), dir, casegen.Placement{Record: "generation.json", Entry: func(c casegen.Case) string { return c.Row + "-" + c.Variant }})
		if err != nil {
			t.Fatal(err)
		}
		record, err := casegen.ReadRecord(w.Record)
		if err != nil {
			t.Fatal(err)
		}
		written[c.Request], records[c.Request] = dir, record
	}
	for _, c := range expectations.Cases {
		t.Run(c.Request+"/"+c.Variant, func(t *testing.T) {
			var generated casegen.Case
			for _, candidate := range records[c.Request].Cases {
				if candidate.Variant == c.Variant {
					generated = candidate
				}
			}
			if len(generated.Phases) != len(c.Phases) {
				t.Fatalf("%d phases generated, %d expected", len(generated.Phases), len(c.Phases))
			}
			s := startTarget(t, c.ResourceUpdates == "snapshot")
			destination := replay.Target{Schema: replay.TargetSchema, TestEndpoint: true, Address: s.listener.Addr().String(), Transport: "plain", ConnectTimeout: "2s", MessageTimeout: "3s", MaxACKBytes: 4096}
			for i, want := range c.Phases {
				phase := generated.Phases[i]
				if phase.ID != want.Phase {
					t.Fatalf("phase %d is %s, expected %s", i+1, phase.ID, want.Phase)
				}
				occurrences := []string{}
				for _, ordinal := range phase.Occurrences {
					occurrences = append(occurrences, generated.Occurrences[ordinal-1].CaseEvent)
				}
				// The target is held to each phase's bytes, not their timing:
				// the byte-only replay is chosen explicitly.
				plan, err := replay.PrepareSend(context.Background(), filepath.Join(written[c.Request], generated.Entry), destination, replay.Options{Occurrences: occurrences, IgnoreScenarioTiming: true}, replay.SendOptions{})
				if err != nil {
					t.Fatal(err)
				}
				run, err := replay.Send(context.Background(), plan, filepath.Join(t.TempDir(), "run"), replay.SendOptions{})
				if err != nil {
					t.Fatal(err)
				}
				acks := []string{}
				for _, e := range run.Events {
					acks = append(acks, e.ACK.Code)
				}
				if !slices.Equal(acks, want.ACKs) {
					t.Fatalf("phase %s answered %v, expected %v", phase.ID, acks, want.ACKs)
				}
				if got := s.ledgerView(); !mapsEqual(got, want.Ledger) {
					t.Fatalf("after phase %s the target holds %v, expected %v", phase.ID, got, want.Ledger)
				}
			}
		})
	}
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// Reusing a case's bytes never merges histories or executions. A later
// generation of the same content from another scenario revision, derived from
// the first and with a delayed variant, names the first generation's case
// bundles; each record keeps its own ancestry, revision and schedule, an
// intentional retransmission stays a retransmission, and every execution of
// the shared case is its own run.
func TestReusedCasesKeepTheirOwnHistoryAndExecutions(t *testing.T) {
	request, profile, pack := owned(t, "book-reschedule-cancel", "siu")
	request = edited(t, request, func(v map[string]any) {
		v["variants"] = []any{
			map[string]any{"id": "baseline", "polarity": "positive", "mutations": []any{}},
			map[string]any{"id": "duplicate-booking", "polarity": "negative", "mutations": []any{map[string]any{"op": "duplicate", "step": "book"}}},
		}
	})
	dir := t.TempDir()
	first, err := casegen.Generate(context.Background(), request, profile, pack, casegen.Options{Source: &casegen.Source{Item: "scenario-1", Revision: "1"}})
	if err != nil {
		t.Fatal(err)
	}
	a, err := first.Write(context.Background(), dir, casegen.Placement{Record: "first.json", Entry: func(c casegen.Case) string { return "first-" + c.Variant }})
	if err != nil {
		t.Fatal(err)
	}
	firstRecord, err := casegen.ReadRecord(a.Record)
	if err != nil {
		t.Fatal(err)
	}
	held := map[string]casegen.Held{}
	for i, c := range firstRecord.Cases {
		held[firstRecord.CaseKey(i)] = casegen.Held{Entry: c.Entry, Identity: c.Identity}
	}
	derived := edited(t, request, func(v map[string]any) {
		v["derived_from"] = first.Record.ContentIdentity
		v["variants"] = append(v["variants"].([]any), map[string]any{"id": "slow-booking", "polarity": "positive", "mutations": []any{map[string]any{"op": "delay", "step": "book", "after": "3s"}}})
	})
	second, err := casegen.Generate(context.Background(), derived, profile, pack, casegen.Options{Source: &casegen.Source{Item: "scenario-1", Revision: "2"}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := second.Write(context.Background(), dir, casegen.Placement{Record: "second.json", Entry: func(c casegen.Case) string { return "second-" + c.Variant }, Held: held})
	if err != nil {
		t.Fatal(err)
	}
	// One bundle per content: nothing of the second generation was written again.
	if b.Cases[0].Entry != "first-baseline" || b.Cases[1].Entry != "first-duplicate-booking" || b.Cases[2].Entry != "first-baseline" {
		t.Fatalf("entries %s %s %s", b.Cases[0].Entry, b.Cases[1].Entry, b.Cases[2].Entry)
	}
	if entries, _ := filepath.Glob(filepath.Join(dir, "second-*")); len(entries) != 0 {
		t.Fatalf("reused content was written again: %v", entries)
	}
	// The retransmission is still two messages in the shared bundle.
	duplicate, err := bundle.Open(filepath.Join(dir, "first-duplicate-booking"))
	if err != nil || len(duplicate.Events) != 4 || !b.Cases[1].Occurrences[1].Duplicate {
		t.Fatalf("an intentional duplicate collapsed: %v", err)
	}
	// Each record keeps its own history and schedule.
	secondRecord, err := casegen.ReadRecord(b.Record)
	if err != nil {
		t.Fatal(err)
	}
	if firstRecord.Ancestry.Source.Revision != "1" || secondRecord.Ancestry.Source.Revision != "2" || secondRecord.Ancestry.DerivedFrom != first.Record.ContentIdentity ||
		firstRecord.ContentIdentity == secondRecord.ContentIdentity || len(secondRecord.Ancestry.Variants) != 3 {
		t.Fatal("the generations' ancestry merged")
	}
	if secondRecord.Cases[2].Occurrences[0].Delay != "3s" || secondRecord.Cases[0].Occurrences[0].Delay != "0s" || secondRecord.Cases[2].Variant != "slow-booking" || secondRecord.Cases[2].Ledger[0].Mutation.Op != "delay" {
		t.Fatal("the shared case lost its variant's schedule or ledger")
	}
	if raw, err := os.ReadFile(filepath.Join(dir, "first.json")); err != nil || !bytes.Equal(raw, a.Record) {
		t.Fatal("the first generation's record changed")
	}
	// Two executions of the one shared case are two runs of that case.
	s := startTarget(t, false)
	destination := replay.Target{Schema: replay.TargetSchema, TestEndpoint: true, Address: s.listener.Addr().String(), Transport: "plain", ConnectTimeout: "2s", MessageTimeout: "3s", MaxACKBytes: 4096}
	runs := []*replay.Run{}
	for range 2 {
		plan, err := replay.PrepareSend(context.Background(), filepath.Join(dir, "first-baseline"), destination, replay.Options{Occurrences: []string{"s0001-e000001"}, IgnoreScenarioTiming: true}, replay.SendOptions{})
		if err != nil {
			t.Fatal(err)
		}
		run, err := replay.Send(context.Background(), plan, filepath.Join(t.TempDir(), "run"), replay.SendOptions{})
		if err != nil {
			t.Fatal(err)
		}
		runs = append(runs, run)
	}
	if runs[0].Identity == runs[1].Identity || runs[0].Manifest.SourceBundleIdentity != b.Cases[0].Identity || runs[1].Manifest.SourceBundleIdentity != b.Cases[0].Identity {
		t.Fatal("two executions of one case share an execution identity")
	}
}
