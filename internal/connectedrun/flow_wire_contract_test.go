package connectedrun_test

import (
	"bufio"
	"bytes"
	"encoding/json/v2"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/collection"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/observeinterval"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"github.com/bharm16/readmit/internal/testisolation"
)

func flowWirePtr[T any](v T) *T { return &v }

// Expectations come from the fictional stimulus and independent target protocol,
// never from the report returned by the evaluator under test.
func flowWireOperatorControls() assertion.Set {
	field := func(scope assertion.MessageScope, selector string) assertion.Subject {
		return assertion.Subject{Field: &assertion.FieldRef{Scope: scope, Message: "s0001-e000002", Selector: selector}}
	}
	collection := assertion.Subject{Collection: &assertion.RecordRef{Scope: "after"}}
	present := func(text string) assertion.Expected {
		return assertion.Expected{Field: &assertion.FieldValue{State: hl7.Present, Text: &text}}
	}
	cases := []struct {
		op                 assertion.Operator
		subject            assertion.Subject
		positive, negative assertion.Expected
	}{
		{assertion.FieldEquals, field("observed", "MSA-1"), present("AA"), present("AR")},
		{assertion.FieldNotEquals, field("observed", "MSA-1"), present("AR"), present("AA")},
		{assertion.FieldState, field("input", "OBX-5"), assertion.Expected{State: flowWirePtr(hl7.Present)}, assertion.Expected{State: flowWirePtr(hl7.Null)}},
		{assertion.TextMatches, field("observed", "MSA-2"), assertion.Expected{Pattern: flowWirePtr("^MOVE$")}, assertion.Expected{Pattern: flowWirePtr("^BOOK$")}},
		{assertion.NumericRange, field("input", "OBX-5"), assertion.Expected{Range: &assertion.Range{Min: "72.49", Max: "72.51"}}, assertion.Expected{Range: &assertion.Range{Min: "73", Max: "74"}}},
		{assertion.NumericTolerance, field("input", "OBX-5"), assertion.Expected{Tolerance: &assertion.Tolerance{Value: "72.5", Tolerance: "0"}}, assertion.Expected{Tolerance: &assertion.Tolerance{Value: "72", Tolerance: "0.49"}}},
		{assertion.DateWindow, field("input", "MSH-7"), assertion.Expected{Window: &assertion.Window{From: "20260101000000+0000", To: "20260101000002+0000"}}, assertion.Expected{Window: &assertion.Window{From: "20260102000000+0000", To: "20260102000002+0000"}}},
		{assertion.ValuesEqual, assertion.Subject{Pair: &assertion.PairRef{Left: assertion.FieldRef{Scope: "input", Message: "s0001-e000002", Selector: "MSH-10"}, Right: assertion.FieldRef{Scope: "observed", Message: "s0001-e000002", Selector: "MSA-2"}}}, assertion.Expected{Holds: flowWirePtr(true)}, assertion.Expected{Holds: flowWirePtr(false)}},
		{assertion.RecordCount, collection, assertion.Expected{Count: flowWirePtr(1)}, assertion.Expected{Count: flowWirePtr(2)}},
		{assertion.RecordsUnique, collection, assertion.Expected{Holds: flowWirePtr(true)}, assertion.Expected{Holds: flowWirePtr(false)}},
		{assertion.RecordsContain, collection, assertion.Expected{Keys: flowWirePtr([]string{"SAME"})}, assertion.Expected{Keys: flowWirePtr([]string{"OTHER"})}},
		{assertion.RecordsOrdered, collection, assertion.Expected{Keys: flowWirePtr([]string{"SAME"})}, assertion.Expected{Keys: flowWirePtr([]string{"OTHER", "SAME"})}},
		{assertion.RecordMultiplicity, collection, assertion.Expected{Multiplicity: &assertion.Multiplicity{Key: "SAME", Count: 1}}, assertion.Expected{Multiplicity: &assertion.Multiplicity{Key: "SAME", Count: 2}}},
		{assertion.RecordsAbsent, assertion.Subject{Collection: &assertion.RecordRef{Scope: "before"}}, assertion.Expected{Holds: flowWirePtr(false)}, assertion.Expected{Holds: flowWirePtr(true)}},
		{assertion.RecordKeyMatches, assertion.Subject{Each: &assertion.EachRef{Scope: "after", Quantifier: "every"}}, assertion.Expected{Pattern: flowWirePtr("^SAME$")}, assertion.Expected{Pattern: flowWirePtr("^OTHER$")}},
		{assertion.RecordsChanged, assertion.Subject{Transition: &assertion.TransitionRef{From: "before", To: "after"}}, assertion.Expected{Change: &assertion.Change{}}, assertion.Expected{Change: &assertion.Change{Added: 1}}},
	}
	set := assertion.Set{Schema: assertion.Schema, Name: "Independent wire operator controls"}
	for _, c := range cases {
		id := strings.ReplaceAll(string(c.op), "_", "-")
		set.Assertions = append(set.Assertions,
			assertion.Assertion{ID: "positive-" + id, Operator: c.op, Subject: c.subject, Expected: c.positive},
			assertion.Assertion{ID: "negative-" + id, Operator: c.op, Subject: c.subject, Expected: c.negative})
	}
	set.Assertions = append(set.Assertions,
		assertion.Assertion{ID: "conditional-skipped", Operator: assertion.FieldState, Subject: field("observed", "ERR-3"), When: &assertion.Condition{Field: assertion.FieldRef{Scope: "observed", Message: "s0001-e000002", Selector: "MSA-1"}, Equals: assertion.FieldValue{State: hl7.Present, Text: flowWirePtr("AR")}}, Expected: assertion.Expected{State: flowWirePtr(hl7.Present)}},
		assertion.Assertion{ID: "numeric-undecided", Operator: assertion.NumericRange, Subject: field("observed", "MSH-11"), Expected: assertion.Expected{Range: &assertion.Range{Min: "0", Max: "1"}}})
	return set
}

func flowWireDependencies(t *testing.T, h *flowContractHarness) (connectedtest.FlowTest, map[string][]byte) {
	t.Helper()
	d := h.plan.Document().Test
	files := map[string][]byte{}
	retain := func(ref connectedtest.Reference) {
		raw := h.plan.Dependency(ref)
		if raw == nil {
			for _, phase := range d.Phases {
				if found := h.plan.Phase(phase.ID).Files()["dependencies/"+ref.SHA256]; found != nil {
					raw = found
					break
				}
			}
		}
		if raw == nil {
			t.Fatalf("missing authored dependency %s", ref.File)
		}
		files[ref.File] = raw
	}
	retain(d.Isolation)
	for _, s := range d.Steps {
		retain(s.V2.Input)
	}
	for _, phase := range d.Phases {
		retain(phase.Checks)
		for _, ds := range phase.Datasets {
			retain(*ds.Projection)
			retain(*ds.Completion.Policy)
		}
	}
	return d, files
}

func flowWireReference(t *testing.T, files map[string][]byte, id, schema string, value any) connectedtest.Reference {
	t.Helper()
	raw, err := json.Marshal(value, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	name := id + ".json"
	files[name] = raw
	return connectedtest.Reference{Project: "lab", ID: id, Schema: schema, File: name, SHA256: dataset.Digest(raw)}
}

func flowWireInstall(t *testing.T, h *flowContractHarness, d connectedtest.FlowTest, files map[string][]byte, config connectedrun.FlowConfig) {
	t.Helper()
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	p, err := connectedtest.CompileFlow(raw, files, h.plan.Document().Generation)
	if err != nil {
		t.Fatal(err)
	}
	h.plan, h.planPath = p, filepath.Join(h.root, "wire-flow-plan")
	if err = p.Write(t.Context(), h.planPath); err != nil {
		t.Fatal(err)
	}
	write(t, h.configPath, config)
}

func flowWirePrepare(t *testing.T, h *flowContractHarness, instance string) *connectedrun.PreparedFlow {
	t.Helper()
	p, err := connectedrun.PrepareFlow(h.planPath, h.configPath, instance)
	if err != nil {
		t.Fatal(err)
	}
	var config connectedrun.FlowConfig
	flowContractRead(t, h.configPath, &config)
	for name, binding := range p.Bindings() {
		parts := strings.SplitN(name, ":", 2)
		var grant connectedrun.Grant
		if parts[0] == "isolation" {
			switch parts[1] {
			case "read":
				grant = config.Isolation.Read
			case "setup":
				grant = config.Isolation.Setup
			case "cleanup":
				grant = config.Isolation.Cleanup
			}
		} else if parts[1] == "stimulus" {
			grant = config.Phases[parts[0]].Definition.Send
		} else {
			grant = *config.Phases[parts[0]].Definition.Sources[strings.TrimPrefix(parts[1], "dataset:")].Grant
		}
		write(t, filepath.Join(h.root, grant.Path), networkaction.RunnerGrant{Schema: networkaction.GrantSchema, Actor: grant.Actor, Generation: grant.Generation, Binding: binding, IssuedAt: time.Now().Add(-time.Minute), Expires: time.Now().Add(time.Hour)})
	}
	return p
}

func TestFlowWireContractAllSixteenOperatorsUseExecutedOriginalOccurrences(t *testing.T) {
	h := newFlowContractHarnessWithSource(t, true)
	d, files := flowWireDependencies(t, h)
	var config connectedrun.FlowConfig
	flowContractRead(t, h.configPath, &config)
	wires := [][]byte{}
	for i := range d.Steps {
		s := &d.Steps[i]
		raw := bytes.Replace(files[s.V2.Input.File], []byte("||SIU^"), []byte("+0000||SIU^"), 1)
		raw = bytes.Replace(raw, []byte{28, 13}, append([]byte("OBX|1|NM|VALUE||72.50\r"), 28, 13), 1)
		files[s.V2.Input.File] = raw
		s.V2.Input.SHA256 = dataset.Digest(raw)
		wires = append(wires, raw)
	}
	// Separate source occurrences retain byte-identical authored MOVE stimuli.
	duplicate := d.Steps[1]
	duplicate.ID = "intentional-repeat"
	v2 := *duplicate.V2
	v2.Occurrence = "s0001-e000003"
	duplicate.V2 = &v2
	d.Steps = append(d.Steps, duplicate)
	d.Phases[1].Steps = append(d.Phases[1].Steps, duplicate.ID)
	wires = append(wires, bytes.Clone(wires[1]))
	now := time.Now()
	if _, err := bundle.Write(filepath.Join(h.root, "wire-case"), []bundle.Input{{Path: "owned-wire.mllp", Data: bytes.Join(wires, nil)}}, bundle.Provenance{Mode: bundle.Imported, ImportedAt: &now}); err != nil {
		t.Fatal(err)
	}
	for name, phase := range config.Phases {
		phase.Definition.Case = "wire-case"
		config.Phases[name] = phase
	}
	checks := flowWireOperatorControls()
	d.Phases[1].Wire = &connectedtest.WireChecks{Set: flowWireReference(t, files, "wire-controls", assertion.Schema, checks), Observed: "transport-acks", Before: &connectedtest.KeyBinding{Dataset: "before", Column: "key"}, After: &connectedtest.KeyBinding{Dataset: "after", Column: "key"}}
	flowWireInstall(t, h, d, files, config)
	p := flowWirePrepare(t, h, "wire-run")
	output := filepath.Join(h.root, "wire-run")
	result, err := connectedrun.ExecuteFlow(t.Context(), p, output, testisolation.Confirmation{})
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "complete" || result.Cleanup != "complete" || result.Verdict != assertion.VerdictFail || len(result.Phases) != 2 {
		t.Fatalf("operator controls did not complete: %+v", result)
	}
	phase := result.Phases[1]
	if phase.Wire == nil || phase.EvaluationError != "" || len(phase.Wire.Results) != 34 {
		t.Fatalf("missing complete wire denominator: %+v", phase)
	}
	operators := map[assertion.Operator]int{}
	for _, check := range phase.Wire.Results {
		want := assertion.OutcomePassed
		switch {
		case strings.HasPrefix(check.ID, "negative-"):
			want = assertion.OutcomeFailed
		case check.ID == "conditional-skipped":
			want = assertion.OutcomeSkipped
		case check.ID == "numeric-undecided":
			want = assertion.OutcomeUndecided
		}
		if check.Outcome != want {
			t.Errorf("%s: outcome %s, want %s", check.ID, check.Outcome, want)
		}
		if strings.HasPrefix(check.ID, "positive-") {
			operators[check.Operator]++
		}
	}
	if len(operators) != 16 {
		t.Fatalf("only %d operators exercised", len(operators))
	}
	run, err := replay.Open(filepath.Join(output, "phases", "reschedule", "transport", "run"))
	if err != nil {
		t.Fatal(err)
	}
	if len(run.Events) != 2 || run.Events[0].SourceOccurrence != "s0001-e000002" || run.Events[1].SourceOccurrence != "s0001-e000003" {
		t.Fatalf("original selected occurrences lost: %+v", run.Events)
	}
	for _, event := range run.Events {
		raw, e := run.Raw(event.Sent)
		if e != nil || !bytes.Equal(raw, wires[1]) {
			t.Fatal("authored duplicate transformed or not sent", e)
		}
	}
	if h.fixture.target.received.Load() != 3 {
		t.Fatal("selected duplicate omitted or extra input sent")
	}
	reopened, err := connectedrun.OpenFlow(t.Context(), output)
	if err != nil || !reflect.DeepEqual(reopened.Phases[1].Wire, phase.Wire) {
		t.Fatal("wire report did not reproduce from retained phase evidence", err)
	}
}

func TestFlowWireContractExplicitReorderingAndSameOriginalRepeat(t *testing.T) {
	h := newFlowContractHarnessWithSource(t, true)
	d, files := flowWireDependencies(t, h)
	var config connectedrun.FlowConfig
	flowContractRead(t, h.configPath, &config)

	// The original case has BOOK then MOVE. This phase deliberately selects
	// MOVE, BOOK, MOVE from those same two immutable source occurrences.
	book, move := d.Steps[0], d.Steps[1]
	repeat := move
	repeat.ID = "repeat-original-move"
	v2 := *move.V2
	repeat.V2 = &v2
	d.Steps = append(d.Steps, repeat)
	phase := d.Phases[1]
	phase.After = nil
	phase.Steps = []string{move.ID, book.ID, repeat.ID}
	var typed assertion.DatasetSetDocument
	if err := json.Unmarshal(files[phase.Checks.File], &typed); err != nil {
		t.Fatal(err)
	}
	for i := range typed.Assertions {
		if typed.Assertions[i].ID == "before-count" {
			typed.Assertions[i].Count = flowWirePtr(0)
		}
	}
	phase.Checks = flowWireReference(t, files, "ordered-typed-checks", assertion.DatasetSchema, typed)
	aliases := []string{"s9000-e000001", "s9000-e000002", "s9000-e000003"}
	sources := []string{"s0001-e000002", "s0001-e000001", "s0001-e000002"}
	controls := []string{"MOVE", "BOOK", "MOVE"}
	authored := [][]byte{files[move.V2.Input.File], files[book.V2.Input.File], files[move.V2.Input.File]}
	wire := assertion.Set{Schema: assertion.Schema, Name: "Original occurrence order with distinct ACK bindings"}
	acks := map[string]string{}
	for i, alias := range aliases {
		acks[alias] = phase.Steps[i]
		wire.Assertions = append(wire.Assertions,
			assertion.Assertion{
				ID: fmt.Sprintf("ack-%d", i+1), Operator: assertion.FieldEquals,
				Subject:  assertion.Subject{Field: &assertion.FieldRef{Scope: "observed", Message: alias, Selector: "MSA-2"}},
				Expected: assertion.Expected{Field: &assertion.FieldValue{State: hl7.Present, Text: flowWirePtr(controls[i])}},
			},
			assertion.Assertion{
				ID: fmt.Sprintf("original-%d", i+1), Operator: assertion.ValuesEqual,
				Subject: assertion.Subject{Pair: &assertion.PairRef{
					Left:  assertion.FieldRef{Scope: "input", Message: sources[i], Selector: "MSH-10"},
					Right: assertion.FieldRef{Scope: "observed", Message: alias, Selector: "MSA-2"},
				}},
				Expected: assertion.Expected{Holds: flowWirePtr(true)},
			})
	}
	phase.Wire = &connectedtest.WireChecks{
		Set:      flowWireReference(t, files, "ordered-wire-checks", assertion.Schema, wire),
		Observed: "transport-acks", Acknowledgements: acks,
	}
	d.Phases = []connectedtest.FlowPhase{phase}
	config.Phases = map[string]connectedrun.ConfigV2{phase.ID: config.Phases[phase.ID]}
	flowWireInstall(t, h, d, files, config)

	received := make(chan []byte, 8)
	originalOutput := h.fixture.target.getOutput()
	h.fixture.target.setOutput(func(n int, raw string) {
		originalOutput(n, raw)
		received <- []byte(raw)
	})
	p := flowWirePrepare(t, h, "ordered-run")
	output := filepath.Join(h.root, "ordered-run")
	result, err := connectedrun.ExecuteFlow(t.Context(), p, output, testisolation.Confirmation{})
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "complete" || result.Verdict != assertion.VerdictPass || result.Cleanup != "complete" || len(result.Phases) != 1 {
		t.Fatalf("explicit source sequence did not complete: %+v", result)
	}
	actual := result.Phases[0]
	if actual.EvaluationError != "" || actual.Wire == nil || len(actual.Wire.Results) != 6 {
		t.Fatalf("ACK aliases lost original occurrence bindings: %+v", actual)
	}
	for _, check := range actual.Wire.Results {
		if check.Outcome != assertion.OutcomePassed {
			t.Errorf("%s: original occurrence/ACK association = %s", check.ID, check.Outcome)
		}
	}
	if h.fixture.target.received.Load() != 3 {
		t.Fatal("selected original occurrence was deduplicated or extra traffic was sent")
	}
	run, err := replay.Open(filepath.Join(output, "phases", phase.ID, "transport", "run"))
	if err != nil {
		t.Fatal(err)
	}
	if len(run.Events) != 3 {
		t.Fatalf("attempt denominator = %d, want 3", len(run.Events))
	}
	outbound := map[string]bool{}
	for i, event := range run.Events {
		if event.SourceOccurrence != sources[i] || outbound[event.OutboundOccurrence] || event.Delivery != "acknowledged" {
			t.Fatalf("attempt %d lost explicit source/order identity: %+v", i+1, event)
		}
		outbound[event.OutboundOccurrence] = true
		sent, err := run.Raw(event.Sent)
		if err != nil || !bytes.Equal(sent, authored[i]) {
			t.Fatalf("attempt %d changed its original wire bytes: %v", i+1, err)
		}
		select {
		case raw := <-received:
			want := bytes.TrimSuffix(bytes.TrimPrefix(authored[i], []byte{11}), []byte{28, 13})
			if !bytes.Equal(raw, want) {
				t.Fatalf("target received attempt %d out of order or with changed bytes", i+1)
			}
		default:
			t.Fatalf("target did not witness attempt %d", i+1)
		}
	}
	select {
	case <-received:
		t.Fatal("target witnessed unexpected fourth attempt")
	default:
	}
	if err := os.Rename(filepath.Join(h.root, "case"), filepath.Join(h.root, "original-case-unavailable")); err != nil {
		t.Fatal(err)
	}
	reopened, err := connectedrun.OpenFlow(t.Context(), output)
	if err != nil || !reflect.DeepEqual(reopened.Phases[0].Wire, actual.Wire) {
		t.Fatal("exact occurrence sequence did not reopen without original case", err)
	}
}

func TestFlowWireContractCaptureMappingKeepsDuplicateOutputOccurrences(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutated bool
		copies  int
	}{
		{"correct-mapping", false, 2},
		{"wrong-mapping", true, 2},
		{"extra-duplicate", false, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newFlowContractHarnessWithSource(t, true)
			d, files := flowWireDependencies(t, h)
			d.Boundary = "engine-output"
			var config connectedrun.FlowConfig
			flowContractRead(t, h.configPath, &config)
			reserve, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			address := reserve.Addr().String()
			reserve.Close()
			_, port, _ := net.SplitHostPort(address)
			number, _ := strconv.Atoi(port)
			var policy sendpolicy.ScopedPolicy
			flowContractRead(t, filepath.Join(h.root, "policy.json"), &policy)
			policy.Rules = append(policy.Rules, sendpolicy.ScopeRule{Endpoint: "after", Operation: sendpolicy.CaptureListen, Port: number, Destinations: []string{"127.0.0.1/32"}, Selection: "single-address"})
			write(t, filepath.Join(h.root, "policy.json"), policy)
			policyRaw, err := os.ReadFile(filepath.Join(h.root, "policy.json"))
			if err != nil {
				t.Fatal(err)
			}
			d.Environment.AddressPolicyIdentity = dataset.Digest(policyRaw)
			source := observeinterval.CaptureSource{Schema: observeinterval.CaptureSourceSchema, Address: address, ReceiverPolicy: collection.Policy{Schema: collection.PolicySchemaV1, Name: "wire-output", SourceLabel: "independent-wire-output", Acknowledgement: collection.AckRule{Operator: collection.FixedCodeOperator, Code: "AA"}, AcceptedMessageTypes: collection.MessageTypeRule{Operator: collection.AnyMessageTypeRule, Values: []string{}}}, TimeoutMS: 10000, MaxFrameBytes: 4096, MaxBytes: 65536, MaxMessages: 10, MaxConnections: 2, MaxSessions: 10, RunSelector: "ZRN-1"}
			write(t, filepath.Join(h.root, "wire-capture.json"), source)
			sourceRaw, err := os.ReadFile(filepath.Join(h.root, "wire-capture.json"))
			if err != nil {
				t.Fatal(err)
			}
			projection := dataset.Projection{Schema: dataset.ProjectionSchema, ID: "wire-output", Format: "hl7", Order: "source", Columns: []dataset.Column{{Name: "key", Type: "text", Selector: "SCH-1", Key: true, Required: true}}, Limits: dataset.Limits{MaxRows: 10, MaxBytes: 65536, TimeoutMS: 1000}}
			phase := &d.Phases[1]
			for i := range phase.Datasets {
				ds := &phase.Datasets[i]
				if ds.ID != "after" {
					continue
				}
				ds.Source = dataset.Digest(sourceRaw)
				ref := flowWireReference(t, files, "wire-output", dataset.ProjectionSchema, projection)
				ds.Projection = &ref
				var interval observeinterval.Definition
				if err = json.Unmarshal(files[ds.Completion.Policy.File], &interval); err != nil {
					t.Fatal(err)
				}
				interval.Source = ds.Source
				interval.Mode = "stream"
				interval.Freshness = "ingress"
				window := flowWireReference(t, files, "wire-output-window", observeinterval.Schema, interval)
				ds.Completion.Policy = &window
			}
			var typed assertion.DatasetSetDocument
			if err = json.Unmarshal(files[phase.Checks.File], &typed); err != nil {
				t.Fatal(err)
			}
			typed.Assertions = []assertion.DatasetAssertion{{ID: "two-output-occurrences", Operator: "row-count", Subject: assertion.RowSelection{Dataset: "after"}, Count: flowWirePtr(2)}}
			for i := range typed.Bindings {
				if typed.Bindings[i].Name == "after" {
					typed.Bindings[i].Source = dataset.Digest(sourceRaw)
					typed.Bindings[i].ProjectionIdentity = projection.Identity()
				}
			}
			// This engine-output acceptance has no appointment-ledger
			// observation and no fixture ACK assertion. Only actual mapped
			// capture occurrences form the oracle.
			typed.Bindings = slices.DeleteFunc(typed.Bindings, func(b assertion.DatasetBinding) bool { return b.Name != "after" })
			phase.Datasets = slices.DeleteFunc(phase.Datasets, func(ds connectedtest.Dataset) bool { return ds.ID != "after" })
			phase.Checks = flowWireReference(t, files, "wire-capture-checks", assertion.DatasetSchema, typed)
			set := assertion.Set{Schema: assertion.Schema, Name: "Independent output mapping and multiplicity", Assertions: []assertion.Assertion{
				{ID: "original-reschedule-mapping", Operator: assertion.ValuesEqual, Subject: assertion.Subject{Pair: &assertion.PairRef{Left: assertion.FieldRef{Scope: "input", Message: "s0001-e000002", Selector: "SCH-1"}, Right: assertion.FieldRef{Scope: "observed", Message: "s0001-e000001", Selector: "SCH-1"}}}, Expected: assertion.Expected{Holds: flowWirePtr(true)}},
				{ID: "duplicate-output-retained", Operator: assertion.RecordMultiplicity, Subject: assertion.Subject{Collection: &assertion.RecordRef{Scope: "after"}}, Expected: assertion.Expected{Multiplicity: &assertion.Multiplicity{Key: "SAME", Count: 2}}},
				// The intervening collector ACK is occurrence 2; the second
				// original inbound frame keeps occurrence 3 after projection.
				{ID: "independent-output-not-ack", Operator: assertion.FieldEquals, Subject: assertion.Subject{Field: &assertion.FieldRef{Scope: "observed", Message: "s0001-e000003", Selector: "MSH-3"}}, Expected: assertion.Expected{Field: &assertion.FieldValue{State: hl7.Present, Text: flowWirePtr("MAPPER")}}},
			}}
			phase.Wire = &connectedtest.WireChecks{Set: flowWireReference(t, files, "wire-capture-set", assertion.Schema, set), Observed: "after", After: &connectedtest.KeyBinding{Dataset: "after", Column: "key"}}
			c := config.Phases[phase.ID]
			c.Definition.Sources["after"] = connectedrun.SourceSelection{Path: "wire-capture.json", Grant: &connectedrun.Grant{Path: "wire-capture-grant.json", Actor: "collector", Generation: "1"}}
			c.Definition.Sources = map[string]connectedrun.SourceSelection{"after": c.Definition.Sources["after"]}
			config.Phases = map[string]connectedrun.ConfigV2{phase.ID: c}
			phase.After = nil
			phase.When = nil
			chosen := d.Steps[1]
			chosen.After = nil
			d.Steps = []connectedtest.Step{chosen}
			d.Phases = []connectedtest.FlowPhase{*phase}

			// Only the independent mapping phase remains selected.
			flowWireInstall(t, h, d, files, config)
			originalOutput := h.fixture.target.getOutput()
			outputErrors := make(chan error, 1)
			h.fixture.target.setOutput(func(n int, raw string) {
				originalOutput(n, raw)
				if !strings.Contains(raw, "SIU^S13") {
					return
				}
				key := "SAME"
				if tc.mutated {
					key = "WRONG"
				}
				outputErrors <- flowWireSendMapped(address, "capture-run", key, tc.copies)
			})
			p := flowWirePrepare(t, h, "capture-run")
			output := filepath.Join(h.root, "capture-run")
			result, err := connectedrun.ExecuteFlow(t.Context(), p, output, testisolation.Confirmation{})
			if err != nil {
				t.Fatal(err)
			}
			select {
			case err = <-outputErrors:
				if err != nil {
					t.Fatal(err)
				}
			default:
				t.Fatal("independent mapper never ran")
			}
			want := assertion.VerdictPass
			if tc.mutated || tc.copies != 2 {
				want = assertion.VerdictFail
			}
			if result.State != "complete" || result.Cleanup != "complete" || result.Verdict != want || result.Phases[0].Wire == nil || result.Phases[0].EvaluationError != "" {
				t.Fatalf("captured wire mapping: %+v", result)
			}
			child, err := connectedrun.Open(t.Context(), filepath.Join(output, "phases", "reschedule"))
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := dataset.Open(t.Context(), filepath.Join(output, "phases", "reschedule", child.Observations["after"]))
			if err != nil {
				t.Fatal(err)
			}
			rows := snapshot.Document().Rows
			if len(rows) != tc.copies {
				t.Fatalf("capture deduplicated output occurrences: %+v", rows)
			}
			for i, row := range rows {
				if row.Provenance.SourceRecord != fmt.Sprintf("s0001-e%06d", 1+2*i) || !dataset.Equal(rows[0].Values[0], row.Values[0]) {
					t.Fatalf("capture lost original output occurrence: %+v", row)
				}
			}
			if result.Phases[0].Wire.Results[0].Outcome != map[bool]assertion.Outcome{false: assertion.OutcomePassed, true: assertion.OutcomeFailed}[tc.mutated] {
				t.Fatal("mapping result did not follow actual independent output")
			}
			wantMultiplicity := assertion.OutcomePassed
			if tc.mutated || tc.copies != 2 {
				wantMultiplicity = assertion.OutcomeFailed
			}
			if result.Phases[0].Wire.Results[1].Outcome != wantMultiplicity {
				t.Fatal("wire multiplicity ignored an independent extra output")
			}
			if _, err = connectedrun.OpenFlow(t.Context(), output); err != nil {
				t.Fatal("capture wire readback", err)
			}
		})
	}
}

// One independent connection emits byte-identical outputs and reads each
// real collector ACK. It uses no production mapping, ledger or ACK generator.
func flowWireSendMapped(address, run, key string, copies int) error {
	c, err := net.DialTimeout("tcp", address, 2*time.Second)
	if err != nil {
		return err
	}
	defer c.Close()
	if err = c.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		return err
	}
	r := bufio.NewReader(c)
	wire := "\x0bMSH|^~\\&|MAPPER|LAB|SINK|LAB|20260101000000+0000||SIU^S13|MAPPED|P|2.5.1\rSCH|" + key + "\rZRN|" + run + "\r\x1c\r"
	for i := 0; i < copies; i++ {
		if _, err = fmt.Fprint(c, wire); err != nil {
			return err
		}
		ack, e := r.ReadString(28)
		if e != nil {
			return e
		}
		end, e := r.ReadByte()
		if e != nil {
			return e
		}
		if end != 13 || !strings.Contains(ack, "MSA|AA|") {
			return fmt.Errorf("independent output was not acknowledged")
		}
	}
	return nil
}
