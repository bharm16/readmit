package connectedrun_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/casegen"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/connectedtransport"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/observeinterval"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/testisolation"
)

// scheduleFixture is one case generation of the owned book-then-reschedule
// scenario, written beside the harness: a baseline and a variant that delays
// the reschedule by one second. The two variants' messages are the same bytes,
// so they share one case bundle; only the record says how each is sent.
type scheduleFixture struct {
	raw    []byte
	record casegen.Record
	entry  string
}

func generateSchedule(t *testing.T, root string, variants ...map[string]any) scheduleFixture {
	t.Helper()
	read := func(name string) []byte {
		raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "casegen", name))
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	var request map[string]any
	if err := json.Unmarshal(read("request-book-reschedule-cancel.json"), &request); err != nil {
		t.Fatal(err)
	}
	// The harness's target reads a booking and a reschedule, so the scenario
	// stops before its cancellation.
	scenario := request["scenario"].(map[string]any)
	scenario["steps"] = slices.DeleteFunc(scenario["steps"].([]any), func(s any) bool { return s.(map[string]any)["id"] == "cancel" })
	request["variants"] = append([]any{map[string]any{"id": "baseline", "polarity": "positive", "mutations": []any{}}}, anySlice(variants)...)
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	g, err := casegen.Generate(t.Context(), raw, read("owned/profile-siu.json"), read("owned/pack.json"), casegen.Options{})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "generated")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	written, err := g.Write(t.Context(), dir, casegen.Placement{Record: "generation.json", Entry: func(c casegen.Case) string { return c.Variant }})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range written.Cases[1:] {
		if c.Entry != written.Cases[0].Entry {
			t.Fatal("a delay that reorders nothing changed the case bytes")
		}
	}
	recordRaw := written.Record
	record, err := casegen.ReadRecord(recordRaw)
	if err != nil {
		t.Fatal(err)
	}
	return scheduleFixture{raw: recordRaw, record: record, entry: written.Cases[0].Entry}
}

func anySlice(v []map[string]any) []any {
	out := []any{}
	for _, m := range v {
		out = append(out, m)
	}
	return out
}

func delayVariant(id, after string) map[string]any {
	return map[string]any{"id": id, "polarity": "positive", "mutations": []any{map[string]any{"op": "delay", "step": "reschedule", "after": after}}}
}

// scheduledLifecycle is the harness's two-phase lifecycle sending the named
// variant's generated messages, unchanged, on that variant's schedule.
func scheduledLifecycle(t *testing.T, h *flowContractHarness, f scheduleFixture, variant string) (connectedtest.FlowTest, map[string][]byte, connectedrun.FlowConfig) {
	t.Helper()
	d, files := flowWireDependencies(t, h)
	var config connectedrun.FlowConfig
	flowContractRead(t, h.configPath, &config)
	c := f.record.Cases[slices.IndexFunc(f.record.Cases, func(c casegen.Case) bool { return c.Variant == variant })]
	if len(d.Steps) != len(c.Occurrences) || len(d.Phases) != len(c.Phases) {
		t.Fatalf("the harness lifecycle is not the generated case's shape: %d steps, %d phases", len(d.Steps), len(d.Phases))
	}
	for i := range d.Steps {
		o := c.Occurrences[i]
		payload, err := os.ReadFile(filepath.Join(h.root, "generated", f.entry, filepath.FromSlash(o.Payload)))
		if err != nil {
			t.Fatal(err)
		}
		name := fmt.Sprintf("generated-%d.hl7", o.Ordinal)
		files[name] = payload
		d.Steps[i].V2 = &connectedtest.V2Stimulus{Input: connectedtest.Reference{Project: "lab", ID: fmt.Sprintf("generated-%d", o.Ordinal), Schema: "hl7", File: name, SHA256: dataset.Digest(payload)}, Occurrence: o.CaseEvent, Assignments: []connectedtest.Assignment{}}
	}
	files["generation.json"] = f.raw
	d.Schema = connectedtest.ScheduledFlowTestSchema
	d.Schedule = &connectedtest.Schedule{Generation: connectedtest.Reference{Project: "lab", ID: "generation", Schema: casegen.RecordSchema, File: "generation.json", SHA256: dataset.Digest(f.raw)}, Row: c.Row, Variant: variant}
	for name, phase := range config.Phases {
		phase.Definition.Case = filepath.Join("generated", f.entry)
		config.Phases[name] = phase
	}
	return d, files, config
}

func installScheduled(t *testing.T, h *flowContractHarness, d connectedtest.FlowTest, files map[string][]byte, config connectedrun.FlowConfig, name string) *connectedtest.FlowPlan {
	t.Helper()
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	p, err := connectedtest.CompileFlow(raw, files, h.plan.Document().Generation)
	if err != nil {
		t.Fatal(err)
	}
	h.plan, h.planPath = p, filepath.Join(h.root, name+"-plan")
	if err = p.Write(t.Context(), h.planPath); err != nil {
		t.Fatal(err)
	}
	write(t, h.configPath, config)
	return p
}

// observedAt is the reschedule phase's latest retained observation of the
// target no later than at after its stimulus started, the armed baseline
// included: the status of the one appointment the target holds.
func observedAt(t *testing.T, output string, at time.Duration) (string, time.Duration) {
	t.Helper()
	path := filepath.Join(output, "phases", "reschedule", "intervals", "after")
	interval, err := observeinterval.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	var started, finished time.Time
	for _, r := range interval.Records {
		switch r.Kind {
		case "stimulus-started":
			started = r.RecordedAt
		case "stimulus-finished":
			finished = r.RecordedAt
		}
	}
	status := ""
	for _, r := range interval.Records {
		if r.Snapshot == "" || started.IsZero() || r.RecordedAt.After(started.Add(at)) {
			continue
		}
		snapshot, err := dataset.Open(t.Context(), filepath.Join(path, r.Snapshot))
		if err != nil {
			t.Fatal(err)
		}
		table := assertion.SnapshotTable(snapshot)
		column := slices.IndexFunc(table.Columns, func(c dataset.Column) bool { return c.Name == "status" })
		if column < 0 || len(table.Rows) != 1 {
			t.Fatalf("an observation without the one appointment: %+v", table)
		}
		status = table.Rows[0].Values[column].Text
	}
	return status, finished.Sub(started)
}

// A declared delay that changes no order changes what the connected engine's
// observation sees at the declared observation time. The baseline and the
// delayed variant of one generation send the same bytes in the same order
// through connectedrun.ExecuteFlow. The observation time is one second into
// the reschedule phase, when the delay ends: the baseline's target has moved
// the appointment by then, and the delayed variant's has not, because every
// snapshot recorded by then was taken before its send could begin. Both end
// moved and pass. The schedule comes from the
// generation record and the variant the lifecycle names, never from the case
// bytes the two share, so their reviewed plans differ.
func TestScheduledLifecycleSendsOnTheGenerationsDeclaredDelays(t *testing.T) {
	h := newFlowContractHarnessWithSource(t, true)
	f := generateSchedule(t, h.root, delayVariant("slow-reschedule", "1s"))
	const window = time.Second
	type run struct {
		result  connectedrun.FlowResult
		at      string
		lasted  time.Duration
		sent    [][]byte
		receipt connectedtransport.Evidence
	}
	execute := func(variant string) run {
		d, files, config := scheduledLifecycle(t, h, f, variant)
		installScheduled(t, h, d, files, config, variant)
		p := flowWirePrepare(t, h, variant+"-run")
		output := filepath.Join(h.root, variant+"-run")
		result, err := connectedrun.ExecuteFlow(t.Context(), p, output, testisolation.Confirmation{})
		if err != nil {
			t.Fatal(err)
		}
		if result.State != "complete" || result.Verdict != assertion.VerdictPass || result.Cleanup != "complete" {
			t.Fatalf("%s did not complete: %+v", variant, result)
		}
		r := run{result: result}
		r.at, r.lasted = observedAt(t, output, window)
		for _, phase := range []string{"booking", "reschedule"} {
			transport, err := replay.Open(filepath.Join(output, "phases", phase, "transport", "run"))
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range transport.Events {
				raw, err := transport.Raw(e.Sent)
				if err != nil || e.Delivery != "acknowledged" {
					t.Fatalf("%s: %v %+v", phase, err, e)
				}
				r.sent = append(r.sent, raw)
			}
		}
		if r.receipt, err = connectedtransport.OpenEvidence(filepath.Join(output, "phases", "reschedule", "transport")); err != nil {
			t.Fatal(err)
		}
		if reopened, err := connectedrun.OpenFlow(t.Context(), output); err != nil || reopened.Verdict != result.Verdict {
			t.Fatalf("the scheduled lifecycle did not reopen: %v", err)
		}
		return r
	}
	baseline, slow := execute("baseline"), execute("slow-reschedule")
	if baseline.at != "moved" || baseline.lasted >= time.Second {
		t.Fatalf("the undelayed reschedule was observed %q, its stimulus lasting %v", baseline.at, baseline.lasted)
	}
	if slow.at != "booked" || slow.lasted < time.Second {
		t.Fatalf("the delayed reschedule was observed %q at %v, its stimulus lasting %v", slow.at, window, slow.lasted)
	}
	if len(baseline.sent) != 2 || !slices.EqualFunc(baseline.sent, slow.sent, bytes.Equal) {
		t.Fatal("the delay changed the bytes or the order sent")
	}
	if baseline.result.Plan == slow.result.Plan || !slices.Equal(baseline.receipt.Schedule, []time.Duration{0}) || !slices.Equal(slow.receipt.Schedule, []time.Duration{time.Second}) {
		t.Fatalf("one case's two schedules were not two reviewed plans: %v %v", baseline.receipt.Schedule, slow.receipt.Schedule)
	}
}

// Waiting is part of the send, not a pause outside it: a cancellation during
// the wait sends nothing further and leaves no uncertain delivery, an
// authority withdrawn during the wait is checked when the wait ends and stops
// the send, and a schedule the remaining budget cannot hold is refused before
// the phase arms. None shortens the delay.
func TestScheduledLifecycleWaitKeepsCancellationAuthorityAndBudget(t *testing.T) {
	// Each case runs against its own target: a cancelled lifecycle leaves its
	// isolation lease for an operator to settle.
	slow := func(t *testing.T, after string) (*flowContractHarness, connectedrun.FlowConfig) {
		h := newFlowContractHarnessWithSource(t, true)
		f := generateSchedule(t, h.root, delayVariant("slow-reschedule", after))
		d, files, config := scheduledLifecycle(t, h, f, "slow-reschedule")
		installScheduled(t, h, d, files, config, "slow-reschedule")
		return h, config
	}
	// The reschedule's connection is open, and its one message waiting, once
	// the transport retains what the connection negotiated.
	waiting := func(t *testing.T, output string, then func()) {
		go func() {
			for t.Context().Err() == nil {
				if _, err := os.Stat(filepath.Join(output, "phases", "reschedule", "transport", "transport.json")); err == nil {
					then()
					return
				}
				time.Sleep(5 * time.Millisecond)
			}
		}()
	}

	t.Run("cancelled", func(t *testing.T) {
		h, _ := slow(t, "1s")
		before := h.fixture.target.received.Load()
		p := flowWirePrepare(t, h, "cancelled-run")
		output := filepath.Join(h.root, "cancelled-run")
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		waiting(t, output, cancel)
		result, err := connectedrun.ExecuteFlow(ctx, p, output, testisolation.Confirmation{})
		if err != nil {
			t.Fatal(err)
		}
		reschedule := result.Phases[1]
		if result.State != "cancelled" || reschedule.Steps[0].Outcome != "not-attempted" || reschedule.Steps[0].Uncertain || h.fixture.target.received.Load()-before != 1 {
			t.Fatalf("a cancelled wait sent or left uncertainty: %+v, %d sent", result, h.fixture.target.received.Load()-before)
		}
		if _, err := connectedrun.OpenFlow(t.Context(), output); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("authority withdrawn", func(t *testing.T) {
		h, config := slow(t, "1s")
		before := h.fixture.target.received.Load()
		p := flowWirePrepare(t, h, "withdrawn-run")
		output := filepath.Join(h.root, "withdrawn-run")
		send := config.Phases["reschedule"].Definition.Send
		waiting(t, output, func() {
			write(t, filepath.Join(h.root, send.Path), networkaction.RunnerGrant{Schema: networkaction.GrantSchema, Actor: send.Actor, Generation: send.Generation, Binding: p.Bindings()["reschedule:stimulus"], IssuedAt: time.Now().Add(-time.Minute), Expires: time.Now().Add(-time.Second)})
		})
		result, err := connectedrun.ExecuteFlow(t.Context(), p, output, testisolation.Confirmation{})
		if err != nil {
			t.Fatal(err)
		}
		if result.State != "uncertain" || result.Verdict == assertion.VerdictPass || h.fixture.target.received.Load()-before != 1 {
			t.Fatalf("a send whose authority ended during its wait: %+v, %d sent", result, h.fixture.target.received.Load()-before)
		}
		if _, err := connectedrun.OpenFlow(t.Context(), output); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("budget", func(t *testing.T) {
		h, _ := slow(t, "3s")
		before := h.fixture.target.received.Load()
		p := flowWirePrepare(t, h, "budget-run")
		output := filepath.Join(h.root, "budget-run")
		deadline := time.Now().Add(5 * time.Second)
		ctx, cancel := context.WithDeadline(t.Context(), deadline)
		defer cancel()
		// Once the booking phase is done, leave the reschedule phase less than
		// its three-second delay and one message's window.
		result, err := connectedrun.ExecuteFlow(ctx, p, output, testisolation.Confirmation{}, func(r connectedrun.FlowPhaseResult) {
			if r.ID == "booking" {
				time.Sleep(time.Until(deadline) - 2900*time.Millisecond)
			}
		})
		if err != nil {
			t.Fatal(err)
		}
		if result.Phases[0].State != "complete" || result.Phases[1].State != "incomplete" || result.Phases[1].Steps[0].Outcome != "not-attempted" || h.fixture.target.received.Load()-before != 1 {
			t.Fatalf("a delay the budget cannot hold: %+v, %d sent", result, h.fixture.target.received.Load()-before)
		}
		if _, err := os.Stat(filepath.Join(output, "phases", "reschedule")); !os.IsNotExist(err) {
			t.Fatalf("the refused phase armed or retained evidence: %v", err)
		}
	})
}

// A scheduled lifecycle names exactly one generated case and sends it
// unchanged; timing it cannot keep is refused when the plan is compiled, before
// any execution exists. A generated case is never sent without its schedule:
// an unscheduled lifecycle naming its bundle is refused when it is prepared.
func TestScheduledLifecycleRefusesWhatItCannotKeepBeforeExecution(t *testing.T) {
	h := newFlowContractHarnessWithSource(t, true)
	f := generateSchedule(t, h.root, delayVariant("slow-reschedule", "1s"), delayVariant("very-slow-reschedule", "30s"))
	compile := func(d connectedtest.FlowTest, files map[string][]byte) error {
		raw, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		_, err = connectedtest.CompileFlow(raw, files, h.plan.Document().Generation)
		return err
	}
	for name, change := range map[string]func(*connectedtest.FlowTest, map[string][]byte){
		"a delay past the deadline": func(d *connectedtest.FlowTest, _ map[string][]byte) { d.Schedule.Variant = "very-slow-reschedule" },
		"a case the record lacks":   func(d *connectedtest.FlowTest, _ map[string][]byte) { d.Schedule.Variant = "absent" },
		"changed bytes": func(d *connectedtest.FlowTest, files map[string][]byte) {
			changed := bytes.Replace(files[d.Steps[1].V2.Input.File], []byte("SIU^S13"), []byte("SIU^S14"), 1)
			files["changed.hl7"] = changed
			d.Steps[1].V2.Input.File, d.Steps[1].V2.Input.SHA256 = "changed.hl7", dataset.Digest(changed)
		},
		"another occurrence": func(d *connectedtest.FlowTest, _ map[string][]byte) {
			d.Steps[0].V2.Occurrence, d.Steps[1].V2.Occurrence = d.Steps[1].V2.Occurrence, d.Steps[0].V2.Occurrence
		},
		"one phase for two": func(d *connectedtest.FlowTest, _ map[string][]byte) {
			d.Phases[0].Steps = append(d.Phases[0].Steps, d.Phases[1].Steps...)
			d.Phases = d.Phases[:1]
		},
		"a v4 lifecycle naming a schedule": func(d *connectedtest.FlowTest, _ map[string][]byte) { d.Schema = connectedtest.FlowTestSchema },
		"a v6 lifecycle without one":       func(d *connectedtest.FlowTest, _ map[string][]byte) { d.Schedule = nil },
	} {
		t.Run(name, func(t *testing.T) {
			d, files, _ := scheduledLifecycle(t, h, f, "slow-reschedule")
			change(&d, files)
			if err := compile(d, files); err == nil {
				t.Fatal("compiled")
			} else if strings.Contains(name, "deadline") && !strings.Contains(err.Error(), "deadline") {
				t.Fatalf("refused for another reason: %v", err)
			}
		})
	}
	t.Run("a generated case without its schedule", func(t *testing.T) {
		d, files, config := scheduledLifecycle(t, h, f, "baseline")
		d.Schema, d.Schedule = connectedtest.FlowTestSchema, nil
		installScheduled(t, h, d, files, config, "unscheduled")
		if _, err := connectedrun.PrepareFlow(h.planPath, h.configPath, "unscheduled-run"); err == nil {
			t.Fatal("an unscheduled lifecycle prepared a generated case")
		}
	})
	if h.fixture.target.received.Load() != 0 {
		t.Fatal("compilation or preparation reached the target")
	}
}
