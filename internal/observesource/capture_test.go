package observesource_test

import (
	"context"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/observesource"
	"github.com/bharm16/readmit/internal/observewindow"
)

// Every message below is synthetic. Identifiers are invented tokens and no
// namespace, authority or person here refers to anything real.
const (
	downstreamBooking = "MSH|^~\\&|SCHEDULE|SITE-A|DOWNSTREAM|LAB|20260101120000+0000||SIU^S12|MSG-001|P|2.5.1\rSCH|APPT-001|FILLER-001\r"
	downstreamMove    = "MSH|^~\\&|SCHEDULE|SITE-A|DOWNSTREAM|LAB|20260101120100+0000||SIU^S13|MSG-002|P|2.5.1\rSCH|APPT-002|FILLER-002\r"
	downstreamNoKey   = "MSH|^~\\&|SCHEDULE|SITE-A|DOWNSTREAM|LAB|20260101120200+0000||SIU^S12|MSG-003|P|2.5.1\rSCH||FILLER-003\r"
	downstreamAck     = "MSH|^~\\&|DOWNSTREAM|LAB|SCHEDULE|SITE-A|20260101120001+0000||ACK|ACK-001|P|2.5.1\rMSA|AA|MSG-001\r"
)

const capturedWindow = `{
  "schema": "readmit-observation-window/v1",
  "source": {"kind": "downstream-capture", "identity": "scheduling-downstream", "scope": "appointments"},
  "watermark": {"kind": "none", "position": ""},
  "pre_existing_state": {"declaration": "declared-empty", "baseline_identity": ""},
  "completion": {"deadline": "5s", "quiet_period": "10ms", "stable_samples": 2, "max_records": 100, "max_samples": 64}
}`

const declaredCaptureSource = `{
  "schema": "readmit-observation-source/v2",
  "source": {"kind": "downstream-capture", "identity": "scheduling-downstream", "scope": "appointments"},
  "enabled": true,
  "freshness": {"max_age": "1h"},
  "extraction": null,
  "file": null,
  "http": null,
  "capture": {"path": "downstream.case", "kinds": ["message"], "record_key": "SCH-1.1", "max_occurrences": 100}
}`

// retainedEvidence reads back one read's own record, so a test checks what an
// operator would find beside the material rather than what the collector held
// in memory.
func retainedEvidence(t *testing.T, snapshot string, index int) observesource.Evidence {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(snapshot, "read-000"+strconv.Itoa(index), "read.json"))
	if err != nil {
		t.Fatal(err)
	}
	var record observesource.Evidence
	if err := json.Unmarshal(data, &record, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("a retained read is not one strict %s record: %v", observesource.EvidenceSchema, err)
	}
	return record
}

func framed(messages ...string) []byte {
	var wire []byte
	for _, message := range messages {
		wire = append(wire, 0x0b)
		wire = append(wire, message...)
		wire = append(wire, 0x1c, '\r')
	}
	return wire
}

// sealCaptureSpanning seals one downstream capture the way a receiver does: a
// recorded session that observed each occurrence at its own stated time.
func sealCaptureSpanning(t *testing.T, directory string, observed []time.Time, messages []string) string {
	t.Helper()
	path := filepath.Join(directory, "downstream.case")
	observations := make(map[int]bundle.Observation, len(messages))
	started := observed[0]
	for index := range messages {
		at := observed[index]
		observations[index+1] = bundle.Observation{Direction: bundle.Inbound, ObservedAt: &at}
		if at.Before(started) {
			started = at
		}
	}
	snapshot := observation.Snapshot{
		Schema: observation.Schema, Profile: observation.Profile,
		SessionID: strings.Repeat("b", 32), Mode: observation.Fixed, Consistent: true,
	}
	input := bundle.Input{Data: framed(messages...), Observations: observations}
	if _, err := bundle.WriteRecorded(path, []bundle.Input{input}, started, snapshot); err != nil {
		t.Fatal(err)
	}
	return path
}

// sealCaptureAt is the common case: every occurrence recorded at one time.
func sealCaptureAt(t *testing.T, directory string, observed time.Time, messages ...string) string {
	t.Helper()
	times := make([]time.Time, len(messages))
	for index := range times {
		times[index] = observed
	}
	return sealCaptureSpanning(t, directory, times, messages)
}

// declaredCapture writes one source document beside a sealed capture and reads
// it back through the same public reader a command uses.
func declaredCapture(t *testing.T, document string, observed time.Time, messages ...string) (observesource.Source, string, string) {
	t.Helper()
	directory := t.TempDir()
	capture := sealCaptureAt(t, directory, observed, messages...)
	source, err := observesource.ReadSource(write(t, directory, "source.json", document))
	if err != nil {
		t.Fatal(err)
	}
	return source, capture, filepath.Join(t.TempDir(), "snapshot")
}

// A downstream system is asked for nothing here. It received the HL7 it always
// received; readmit captured what was sent to it, and the observation reads
// that capture under a declared field mapping rather than a receipt anybody had
// to fabricate.
func TestADownstreamCaptureCompletesTheWindowAndBindsWhatTheRunProduced(t *testing.T) {
	source, capture, snapshot := declaredCapture(t, declaredCaptureSource, time.Now(), downstreamBooking, downstreamMove)
	completion := collect(t, source, window(t, capturedWindow), snapshot, "APPT-001", "APPT-404")
	if err := completion.Err(); err != nil {
		t.Fatalf("collection did not complete: %v", err)
	}
	if completion.RecordsObserved != 2 {
		t.Fatalf("records observed = %d, want 2", completion.RecordsObserved)
	}
	if err := completion.Correlated("APPT-001"); err != nil {
		t.Fatalf("what the run produced was not bound to the capture: %v", err)
	}
	// An occurrence the capture never held is unknown, and unknown is not a
	// pass: the window says it observed nothing for it rather than nothing.
	if err := completion.Correlated("APPT-404"); err == nil {
		t.Fatal("an unmatched occurrence was read as correlated")
	}
	// The snapshot names which retained evidence was read. Copying the case
	// would make a second original of the same messages, so the material is
	// the capture's own identity marker, byte for byte.
	retained, err := os.ReadFile(filepath.Join(snapshot, "read-0000", "identity.sha256"))
	if err != nil {
		t.Fatal(err)
	}
	marker, err := os.ReadFile(filepath.Join(capture, "identity.sha256"))
	if err != nil || string(retained) != string(marker) {
		t.Fatalf("the retained material does not name the capture that was read: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(snapshot, "read-0000"))
	if err != nil || len(entries) != 2 {
		t.Fatalf("one read retains its record and the identity of what it read: %v", entries)
	}
	for _, sample := range completion.Samples {
		if sample.EvidenceIdentity == "" {
			t.Fatal("an observation names the original material it read")
		}
	}
	// The record beside the read states how much retained evidence it covered,
	// from the capture's own manifest. A capture holding messages never reports
	// nothing: zero bytes is reserved for a capture that retained nothing.
	described, err := bundle.Describe(capture)
	if err != nil {
		t.Fatal(err)
	}
	covered := 0
	for _, source := range described.Sources {
		covered += source.Size
	}
	if covered == 0 {
		t.Fatal("this capture retained messages, so its manifest states their bytes")
	}
	if read := retainedEvidence(t, snapshot, 0); read.Bytes != covered || read.Records != 2 || read.Kind != observesource.DownstreamCapture {
		t.Fatalf("the retained read does not state what it covered: %+v, want %d bytes", read, covered)
	}
	// Reading a capture changes nothing about it: it is the canonical evidence
	// and the observation is a bounded read of it.
	if after, err := os.ReadFile(filepath.Join(capture, "identity.sha256")); err != nil || string(after) != string(marker) {
		t.Fatalf("the capture was not left exactly as it was found: %v", err)
	}
}

// An observed empty scope is evidence, and it is the only kind of emptiness
// that is: the capture was read in full, and it held no occurrence in scope.
func TestACaptureThatHeldNothingInScopeIsObservedRatherThanMissing(t *testing.T) {
	source, _, snapshot := declaredCapture(t, declaredCaptureSource, time.Now(), downstreamAck)
	completion := collect(t, source, window(t, capturedWindow), snapshot)
	if err := completion.AbsenceEvidence(); err != nil {
		t.Fatalf("an observed empty scope does not support an absence claim: %v", err)
	}
	if completion.RecordsObserved != 0 {
		t.Fatalf("records observed = %d, want 0", completion.RecordsObserved)
	}
}

// The declared scope decides what a record is. The same capture read for its
// acknowledgements and read for its messages is two different observations,
// and neither is inferred from the other.
func TestTheDeclaredScopeDecidesWhichOccurrencesAreRecords(t *testing.T) {
	acknowledgements := strings.Replace(strings.Replace(declaredCaptureSource,
		`"kinds": ["message"]`, `"kinds": ["ack"]`, 1), `"record_key": "SCH-1.1"`, `"record_key": "MSA-2"`, 1)
	for name, test := range map[string]struct {
		document string
		records  int
		produced string
	}{
		"messages":         {document: declaredCaptureSource, records: 2, produced: "APPT-001"},
		"acknowledgements": {document: acknowledgements, records: 1, produced: "MSG-001"},
	} {
		t.Run(name, func(t *testing.T) {
			source, _, snapshot := declaredCapture(t, test.document, time.Now(), downstreamBooking, downstreamMove, downstreamAck)
			completion := collect(t, source, window(t, capturedWindow), snapshot, test.produced)
			if err := completion.Err(); err != nil {
				t.Fatalf("collection did not complete: %v", err)
			}
			if completion.RecordsObserved != test.records {
				t.Fatalf("records observed = %d, want %d", completion.RecordsObserved, test.records)
			}
			if err := completion.Correlated(test.produced); err != nil {
				t.Fatalf("the declared key was not bound: %v", err)
			}
		})
	}
}

// The watermark is the same source-neutral declaration every other collector is
// held to: a capture whose newest recorded occurrence predates the window
// cannot describe it, however quiet it looks.
func TestACaptureRecordedBeforeTheWatermarkIsStaleRatherThanQuiet(t *testing.T) {
	opened := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	declared := strings.Replace(capturedWindow, `"kind": "none", "position": ""`,
		`"kind": "declared-position", "position": "`+opened+`"`, 1)
	source, _, snapshot := declaredCapture(t, declaredCaptureSource, time.Now(), downstreamBooking)
	completion := collect(t, source, window(t, declared), snapshot)
	if completion.Status != observewindow.Stale {
		t.Fatalf("status = %s, want stale", completion.Status)
	}
	if err := completion.AbsenceEvidence(); err == nil {
		t.Fatal("stale evidence supported an absence claim")
	}
}

// A capture is an ordered log, so the watermark is applied to both ends of what
// it covers. One that also holds occurrences from before the window returned
// evidence from before the window: counting them would attribute what was
// already there to this run, and dropping them silently would decide the count
// by a filter the retained record cannot show.
func TestACaptureReachingBackPastTheWatermarkIsStaleRatherThanCounted(t *testing.T) {
	now := time.Now()
	floor := now.Add(-30 * time.Minute)
	declared := strings.Replace(capturedWindow, `"kind": "none", "position": ""`,
		`"kind": "declared-position", "position": "`+floor.UTC().Format(time.RFC3339)+`"`, 1)
	for name, test := range map[string]struct {
		observed []time.Time
		want     observewindow.Status
		records  int
	}{
		"wholly inside the window": {
			observed: []time.Time{now.Add(-3 * time.Minute), now.Add(-time.Minute)},
			want:     observewindow.Complete, records: 2,
		},
		"straddling the watermark": {
			observed: []time.Time{now.Add(-time.Hour), now.Add(-time.Minute)},
			want:     observewindow.Stale,
		},
	} {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			sealCaptureSpanning(t, directory, test.observed, []string{downstreamBooking, downstreamMove})
			source, err := observesource.ReadSource(write(t, directory, "source.json", declaredCaptureSource))
			if err != nil {
				t.Fatal(err)
			}
			completion := collect(t, source, window(t, declared), filepath.Join(t.TempDir(), "snapshot"))
			if completion.Status != test.want {
				t.Fatalf("status = %s, want %s", completion.Status, test.want)
			}
			if completion.RecordsObserved != test.records {
				t.Fatalf("records observed = %d, want %d", completion.RecordsObserved, test.records)
			}
			if test.want != observewindow.Complete {
				if err := completion.AbsenceEvidence(); err == nil {
					t.Fatal("a capture reaching past the watermark supported an absence claim")
				}
			}
		})
	}
}

// Failed collection never becomes a passing absence assertion. Every row here
// observed zero records, and none of them observed that there are none.
func TestACaptureObservationNeverReadsFailedCollectionAsAbsence(t *testing.T) {
	for name, test := range map[string]struct {
		document string
		observed time.Time
		messages []string
		prepare  func(t *testing.T, capture string)
		want     observewindow.Status
	}{
		"a capture that was never sealed": {
			document: declaredCaptureSource, observed: time.Now(), messages: []string{downstreamBooking},
			prepare: func(t *testing.T, capture string) {
				if err := os.RemoveAll(capture); err != nil {
					t.Fatal(err)
				}
			},
			want: observewindow.Missing,
		},
		"a capture that does not verify": {
			document: declaredCaptureSource, observed: time.Now(), messages: []string{downstreamBooking},
			prepare: func(t *testing.T, capture string) {
				if err := os.WriteFile(filepath.Join(capture, "identity.sha256"), []byte(strings.Repeat("0", 64)+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
			},
			want: observewindow.Failed,
		},
		"a capture past the declared read bound": {
			document: strings.Replace(declaredCaptureSource, `"max_occurrences": 100`, `"max_occurrences": 1`, 1),
			observed: time.Now(), messages: []string{downstreamBooking, downstreamMove},
			want: observewindow.Truncated,
		},
		"a capture older than the declared freshness bound": {
			document: declaredCaptureSource, observed: time.Now().Add(-2 * time.Hour),
			messages: []string{downstreamBooking}, want: observewindow.Stale,
		},
		"an occurrence the capture could not parse": {
			document: declaredCaptureSource, observed: time.Now(), messages: []string{downstreamBooking, "NOT-HL7"},
			want: observewindow.Ambiguous,
		},
		"an occurrence in scope without the declared key": {
			document: declaredCaptureSource, observed: time.Now(),
			messages: []string{downstreamBooking, downstreamNoKey}, want: observewindow.Ambiguous,
		},
		"a disabled collector": {
			document: strings.Replace(declaredCaptureSource, `"enabled": true`, `"enabled": false`, 1),
			observed: time.Now(), messages: []string{downstreamBooking}, want: observewindow.Missing,
		},
		"a scope this collector was not pointed at": {
			document: strings.Replace(declaredCaptureSource, `"scope": "appointments"`, `"scope": "orders"`, 1),
			observed: time.Now(), messages: []string{downstreamBooking}, want: observewindow.Unsupported,
		},
	} {
		t.Run(name, func(t *testing.T) {
			source, capture, snapshot := declaredCapture(t, test.document, test.observed, test.messages...)
			if test.prepare != nil {
				test.prepare(t, capture)
			}
			completion := collect(t, source, window(t, capturedWindow), snapshot)
			if completion.Status != test.want {
				t.Fatalf("status = %s, want %s", completion.Status, test.want)
			}
			if err := completion.AbsenceEvidence(); err == nil {
				t.Fatalf("%s supported an absence claim", name)
			}
			if completion.RecordsObserved != 0 || len(completion.Correlations) != 0 {
				t.Fatalf("%s reported settled counts: %+v", name, completion)
			}
			if completion.Status.RunState() != "execution_error" {
				t.Fatalf("%s was not an execution error", name)
			}
		})
	}
}

// A capture that cannot date its own state is exactly as unreadable as a
// response stating neither an age nor a date. Unknown is not current.
func TestACaptureThatCannotDateItsStateIsAmbiguousRatherThanFresh(t *testing.T) {
	directory := t.TempDir()
	imported := time.Now()
	if _, err := bundle.Write(filepath.Join(directory, "downstream.case"),
		[]bundle.Input{{Path: "SYNTHETIC-FIXTURE", Data: framed(downstreamBooking)}},
		bundle.Provenance{Mode: bundle.Imported, ImportedAt: &imported}); err != nil {
		t.Fatal(err)
	}
	source, err := observesource.ReadSource(write(t, directory, "source.json", declaredCaptureSource))
	if err != nil {
		t.Fatal(err)
	}
	completion := collect(t, source, window(t, capturedWindow), filepath.Join(t.TempDir(), "snapshot"))
	if completion.Status != observewindow.Ambiguous {
		t.Fatalf("status = %s, want ambiguous", completion.Status)
	}
}

// Cancelling a capture observation is cancellation, not a reading that the
// downstream system received nothing. The stop reason and the verdict stay two
// different facts, exactly as they do for every other source.
func TestACancelledCaptureObservationIsCancellationRatherThanAnEmptyDownstream(t *testing.T) {
	// A quiet period this window can never reach inside its deadline keeps the
	// collector sampling until the caller cancels it.
	unsettled := strings.Replace(capturedWindow, `"quiet_period": "10ms"`, `"quiet_period": "4s"`, 1)
	source, _, snapshot := declaredCapture(t, declaredCaptureSource, time.Now(), downstreamBooking)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	completion, err := observesource.Collect(ctx, source, window(t, unsettled), observesource.Options{Snapshot: snapshot})
	if err != nil {
		t.Fatal(err)
	}
	if completion.Status != observewindow.Cancelled || completion.Status.RunState() != "cancelled" {
		t.Fatalf("status = %q, want cancelled", completion.Status)
	}
	if completion.RecordsObserved != 0 {
		t.Fatal("a cancelled window reported a settled count")
	}
	if err := completion.AbsenceEvidence(); err == nil {
		t.Fatal("a cancelled window supported an absence claim")
	}
}
