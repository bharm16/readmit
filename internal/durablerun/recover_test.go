package durablerun_test

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/durablerun"
)

func journalBytes(t *testing.T, out string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(out, "journal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func slowTarget(t *testing.T, spec string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(spec), "target.json"))
	if err != nil {
		t.Fatal(err)
	}
	var target map[string]any
	if err = json.Unmarshal(raw, &target); err != nil {
		t.Fatal(err)
	}
	target["message_timeout"] = "1m"
	raw, _ = json.Marshal(target)
	if err = os.WriteFile(filepath.Join(filepath.Dir(spec), "target.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestDeadlineReachedAfterSendIsTimedOutAndUncertain(t *testing.T) {
	address, _ := peer(t, "")
	spec, out := setup(t, address)
	slowTarget(t, spec)
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	got, err := durablerun.Start(ctx, spec, out)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != durablerun.DeliveryUncertain || got.StopReason != durablerun.TimedOut || !got.DeliveryUncertain {
		t.Fatalf("%+v", got)
	}
	recovery, err := durablerun.Recover(out)
	if err != nil {
		t.Fatal(err)
	}
	if !recovery.Terminal || recovery.Run != got || recovery.Uncertain != 1 || recovery.SafeToRepeat || recovery.Lease != durablerun.LeaseReleased || recovery.Occurrences[0].ID != "o000001" {
		t.Fatalf("%+v", recovery)
	}
	if _, err := os.Lstat(filepath.Join(out, "lease.json")); err == nil {
		t.Fatal("a stopped writer left its lease held")
	}
}

func TestDeadlineBeforeFirstSendIsTimedOutAndSafeToRepeat(t *testing.T) {
	address, received := peer(t, "AA")
	spec, out := setup(t, address)
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	got, err := durablerun.Start(ctx, spec, out)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != durablerun.TimedOut || got.DeliveryUncertain {
		t.Fatalf("%+v", got)
	}
	select {
	case <-received:
		t.Fatal("sent after the deadline")
	default:
	}
	recovery, err := durablerun.Recover(out)
	if err != nil {
		t.Fatal(err)
	}
	if !recovery.Terminal || recovery.NotAttempted != 1 || !recovery.SafeToRepeat || recovery.ResumeRefusal != "" || recovery.Lease != durablerun.LeaseReleased {
		t.Fatalf("%+v", recovery)
	}
	// Resume is the one path that executes again, and it repeats only work no
	// intent was ever synced for, into a new output, after proving the spec
	// still prepares the retained plan.
	before := journalBytes(t, out)
	resumed := filepath.Join(filepath.Dir(out), "resumed")
	got2, err := durablerun.Resume(context.Background(), out, spec, resumed)
	if err != nil {
		t.Fatal(err)
	}
	if got2.Schema != durablerun.ResumeSchema || got2.ResumedFrom != durablerun.TimedOut || got2.Repeated != 1 || got2.Run.State != durablerun.Passed || got2.Run.Recorded != 1 {
		t.Fatalf("%+v", got2)
	}
	if !bytes.Equal(before, journalBytes(t, out)) {
		t.Fatal("resume wrote into the job it resumed")
	}
	if reopened, err := durablerun.Open(resumed); err != nil || reopened.State != durablerun.Passed {
		t.Fatalf("%+v %v", reopened, err)
	}
	if _, err := durablerun.Resume(context.Background(), out, spec, resumed); err == nil {
		t.Fatal("resume reused an existing output")
	}
}

func TestResumeRefusesAcknowledgedWorkAndAChangedPlan(t *testing.T) {
	address, _ := peer(t, "AA")
	spec, out := setup(t, address)
	if _, err := durablerun.Start(context.Background(), spec, out); err != nil {
		t.Fatal(err)
	}
	recovery, err := durablerun.Recover(out)
	if err != nil {
		t.Fatal(err)
	}
	if !recovery.Terminal || recovery.Acknowledged != 1 || recovery.Occurrences[0].Delivery != durablerun.Acknowledged || recovery.SafeToRepeat || recovery.ResumeRefusal != "a delivery was acknowledged; a send is never repeated, and this release executes a spec whole" {
		t.Fatalf("%+v", recovery)
	}
	resumed := filepath.Join(filepath.Dir(out), "resumed")
	if _, err := durablerun.Resume(context.Background(), out, spec, resumed); err == nil || err.Error() != "resume refused: "+recovery.ResumeRefusal {
		t.Fatalf("%v", err)
	}
	// A run that never sent is repeatable only as the same work.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	spec2, out2 := setup(t, address)
	if _, err := durablerun.Start(ctx, spec2, out2); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(spec2)
	changed := bytes.Replace(raw, []byte(`"name":"ACK"`), []byte(`"name":"ACK2"`), 1)
	if bytes.Equal(raw, changed) {
		t.Fatal("fixture spec did not change")
	}
	if err := os.WriteFile(spec2, changed, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := durablerun.Resume(context.Background(), out2, spec2, resumed); err == nil || err.Error() != "resume refused: the spec, source or configuration differs from the retained plan; resume repeats only the same work" {
		t.Fatalf("%v", err)
	}
	if _, err := os.Lstat(resumed); err == nil {
		t.Fatal("a refused resume created an output")
	}
}

func TestTornTrailingRecordIsNeitherTerminalNorRepeatable(t *testing.T) {
	address, _ := peer(t, "AA")
	spec, out := setup(t, address)
	if _, err := durablerun.Start(context.Background(), spec, out); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(out, "journal.jsonl"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`{"sequence":4,"kind":"in`)
	f.Close()
	recovery, err := durablerun.Recover(out)
	if err != nil {
		t.Fatal(err)
	}
	if recovery.Terminal || !recovery.Run.JournalIncomplete || recovery.Run.State != durablerun.DeliveryUncertain || recovery.Acknowledged != 1 || recovery.SafeToRepeat || recovery.ResumeRefusal != "completion was not recorded; the writer may still be running" {
		t.Fatalf("%+v", recovery)
	}
	if _, err := durablerun.Clean(out); err == nil {
		t.Fatal("cleanup acted on a journal whose completion is unproven")
	}
}

func TestCleanupRemovesOnlyAStaleLease(t *testing.T) {
	address, _ := peer(t, "AA")
	spec, out := setup(t, address)
	if _, err := durablerun.Start(context.Background(), spec, out); err != nil {
		t.Fatal(err)
	}
	want := []string{"intended", "journal.jsonl", "plan.json", "result", "result.decision.json", "sent"}
	cleanup, err := durablerun.Clean(out)
	if err != nil || cleanup.Schema != durablerun.CleanupSchema || len(cleanup.Removed) != 0 || cleanup.Run.State != durablerun.Passed {
		t.Fatalf("%+v %v", cleanup, err)
	}
	if got, _ := json.Marshal(cleanup.Retained); string(got) != `["intended","journal.jsonl","plan.json","result","result.decision.json","sent"]` {
		t.Fatalf("retained %s, want %v", got, want)
	}
	// A writer that stopped after its terminal record but before releasing.
	lease := durablerun.Lease{Schema: durablerun.LeaseSchema, Holder: durablerun.Holder{PID: 1, StartedAt: time.Now().UTC()}, Resources: []durablerun.Resource{{Kind: durablerun.EndpointResource, Name: address}}}
	raw, _ := json.Marshal(lease)
	if err := os.WriteFile(filepath.Join(out, "lease.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	recovery, err := durablerun.Recover(out)
	if err != nil || recovery.Lease != durablerun.LeaseStale {
		t.Fatalf("%+v %v", recovery, err)
	}
	// A foreign entry means nothing in the directory is known to be safe.
	if err := os.WriteFile(filepath.Join(out, "notes.txt"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := durablerun.Clean(out); err == nil || err.Error() != "durable run holds an entry this release did not write; nothing was removed" {
		t.Fatalf("%v", err)
	}
	if _, err := os.Lstat(filepath.Join(out, "lease.json")); err != nil {
		t.Fatal("a refused cleanup removed the lease")
	}
	os.Remove(filepath.Join(out, "notes.txt"))
	before := journalBytes(t, out)
	cleanup, err = durablerun.Clean(out)
	if err != nil || len(cleanup.Removed) != 1 || cleanup.Removed[0] != "lease.json" {
		t.Fatalf("%+v %v", cleanup, err)
	}
	if _, err := os.Lstat(filepath.Join(out, "lease.json")); err == nil {
		t.Fatal("stale lease retained")
	}
	if !bytes.Equal(before, journalBytes(t, out)) {
		t.Fatal("cleanup changed the journal")
	}
	for _, name := range want {
		if _, err := os.Lstat(filepath.Join(out, name)); err != nil {
			t.Fatalf("cleanup removed evidence %s", name)
		}
	}
	if recovery, err = durablerun.Recover(out); err != nil || recovery.Lease != durablerun.LeaseReleased || recovery.Run.State != durablerun.Passed {
		t.Fatalf("%+v %v", recovery, err)
	}
	// An unreadable lease is changed evidence, not a released one.
	if err := os.WriteFile(filepath.Join(out, "lease.json"), []byte(`{"schema":"readmit-run-lease/v1","extra":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := durablerun.Open(out); err == nil || err.Error() != "durable lease is invalid" {
		t.Fatalf("%v", err)
	}
}
