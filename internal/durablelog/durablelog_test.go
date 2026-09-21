package durablelog

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"strings"
	"testing"
	"time"
)

type testRecord struct {
	Envelope
	Kind       string `json:"kind"`
	Occurrence string `json:"occurrence,omitzero"`
	Final      *int   `json:"final,omitzero"`
}

// legacyRecord is the shape the two journals declared before this package
// existed: the envelope fields declared out by hand ahead of the record's own.
// The bytes a record marshals to must not change because the kernel does.
type legacyRecord struct {
	Sequence   int       `json:"sequence"`
	Previous   string    `json:"previous"`
	At         time.Time `json:"at"`
	Kind       string    `json:"kind"`
	Occurrence string    `json:"occurrence,omitzero"`
	Final      *int      `json:"final,omitzero"`
}

func TestRecordBytesMatchTheLegacyShape(t *testing.T) {
	at := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	prev := strings.Repeat("a", 64)
	stamped := testRecord{Kind: "intent", Occurrence: "o000001"}
	stamped.Sequence, stamped.Previous, stamped.At = 7, prev, at
	embedded, err := json.Marshal(stamped, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := json.Marshal(legacyRecord{Sequence: 7, Previous: prev, At: at, Kind: "intent", Occurrence: "o000001"}, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(embedded, legacy) {
		t.Fatalf("embedded envelope changed the bytes:\n embedded: %s\n   legacy: %s", embedded, legacy)
	}
}

type limitedFile struct {
	data   []byte
	n      int // bytes accepted per Write before reporting a short write
	synced bool
}

func (f *limitedFile) Write(b []byte) (int, error) {
	if f.n > 0 && len(b) > f.n {
		f.data = append(f.data, b[:f.n]...)
		return f.n, nil
	}
	f.data = append(f.data, b...)
	return len(b), nil
}
func (f *limitedFile) Sync() error { f.synced = true; return nil }

func TestAppendStampsTheChainAndSyncs(t *testing.T) {
	file := &limitedFile{}
	w := NewWriter(file, "chain-start", 1<<20, Messages{
		Limit:  errors.New("limit"),
		Sync:   errors.New("sync"),
		Encode: errors.New("encode"),
	})
	if err := w.Append(&testRecord{Kind: "ready"}); err != nil {
		t.Fatal(err)
	}
	if !file.synced {
		t.Fatal("append did not sync before returning")
	}
	line := string(bytes.TrimRight(file.data, "\n"))
	if !strings.HasPrefix(line, `{"sequence":1,"previous":"chain-start","at":"`) {
		t.Fatalf("record does not open with the stamped envelope: %s", line)
	}
	if w.Sequence() != 1 || w.Err() != nil {
		t.Fatalf("sequence %d, err %v", w.Sequence(), w.Err())
	}
	// A second record chains to the first, not to the plan digest.
	if err := w.Append(&testRecord{Kind: "running"}); err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(file.data, []byte{'\n'})
	var second testRecord
	if err := json.Unmarshal(lines[1], &second); err != nil {
		t.Fatal(err)
	}
	if second.Sequence != 2 || second.Previous != Digest(bytes.TrimRight(lines[0], "\n")) {
		t.Fatalf("second record does not chain to the first: %s", lines[1])
	}
}

func TestAppendIsRefusedPastTheDeclaredLimit(t *testing.T) {
	file := &limitedFile{}
	limit := errors.New("the journal is full")
	w := NewWriter(file, "start", 8, Messages{Limit: limit, Sync: errors.New("sync"), Encode: errors.New("encode")})
	if err := w.Append(&testRecord{Kind: "ready"}); !errors.Is(err, limit) {
		t.Fatalf("want the caller's limit error, got %v", err)
	}
	// The refused record's bytes were never written...
	if len(file.data) != 0 {
		t.Fatalf("refused record wrote %d bytes", len(file.data))
	}
	// ...and the refusal is sticky.
	if err := w.Append(&testRecord{Kind: "running"}); !errors.Is(err, limit) {
		t.Fatalf("sticky refusal changed: %v", err)
	}
	if w.Sequence() != 0 {
		t.Fatalf("sequence advanced past a refused record: %d", w.Sequence())
	}
}

func TestShortWriteIsASyncFailure(t *testing.T) {
	file := &limitedFile{n: 4}
	sync := errors.New("cannot sync")
	w := NewWriter(file, "start", 1<<20, Messages{Limit: errors.New("limit"), Sync: sync, Encode: errors.New("encode")})
	if err := w.Append(&testRecord{Kind: "ready"}); !errors.Is(err, sync) {
		t.Fatalf("want the caller's sync error, got %v", err)
	}
	if err := w.Append(&testRecord{Kind: "running"}); !errors.Is(err, sync) {
		t.Fatal("failure is not sticky")
	}
}

type brokenFile struct{}

func (brokenFile) Write([]byte) (int, error) { return 0, errors.New("disk gone") }
func (brokenFile) Sync() error               { return nil }

func TestFailedWriteIsSticky(t *testing.T) {
	sync := errors.New("cannot sync")
	w := NewWriter(brokenFile{}, "start", 1<<20, Messages{Limit: errors.New("limit"), Sync: sync, Encode: errors.New("encode")})
	if err := w.Append(&testRecord{Kind: "ready"}); !errors.Is(err, sync) {
		t.Fatalf("want the caller's sync error, got %v", err)
	}
	if w.Sequence() != 0 {
		t.Fatalf("sequence advanced past a failed record: %d", w.Sequence())
	}
}

func TestScanVerifiesTheChainAndReportsTornTail(t *testing.T) {
	corruptErr := errors.New("corrupt")
	file := &limitedFile{}
	w := NewWriter(file, "start", 1<<20, Messages{Limit: errors.New("limit"), Sync: errors.New("sync"), Encode: errors.New("encode")})
	for _, kind := range []string{"ready", "running", "intent"} {
		if err := w.Append(&testRecord{Kind: kind, Occurrence: "o000001"}); err != nil {
			t.Fatal(err)
		}
	}
	var seen []string
	truncated, err := Scan(file.data, "start", errors.New("corrupt"),
		func(line []byte) Record {
			var r testRecord
			if json.Unmarshal(line, &r) != nil {
				return nil
			}
			return &r
		},
		func(sequence int, r Record) error {
			seen = append(seen, r.(*testRecord).Kind)
			if sequence != len(seen) {
				t.Fatalf("visit sequence %d, want %d", sequence, len(seen))
			}
			return nil
		})
	if err != nil || truncated {
		t.Fatalf("scan of an intact journal: truncated=%v err=%v", truncated, err)
	}
	if len(seen) != 3 || seen[0] != "ready" || seen[2] != "intent" {
		t.Fatalf("visited %v", seen)
	}

	// A torn trailing record is reported, and the records before it are still
	// visited.
	torn := append(bytes.TrimRight(file.data, "\n"), '\n')
	torn = append(torn, `{"sequence":4,"previous":"x"`...)
	truncated, err = Scan(torn, "start", corruptErr,
		func(line []byte) Record {
			var r testRecord
			if json.Unmarshal(line, &r) != nil {
				return nil
			}
			return &r
		},
		func(sequence int, r Record) error { return nil })
	if err != nil || !truncated {
		t.Fatalf("torn tail: truncated=%v err=%v", truncated, err)
	}

	// A complete line that breaks the chain is corrupt, not torn.
	forged := append(bytes.TrimRight(file.data, "\n"), '\n')
	forged = append(forged, []byte(`{"sequence":4,"previous":"forged","at":"2026-09-20T12:00:00Z","kind":"sent"}`)...)
	forged = append(forged, '\n')
	truncated, err = Scan(forged, "start", corruptErr,
		func(line []byte) Record {
			var r testRecord
			if json.Unmarshal(line, &r) != nil {
				return nil
			}
			return &r
		},
		func(sequence int, r Record) error { return nil })
	if !errors.Is(err, corruptErr) {
		t.Fatalf("forged record: err=%v", err)
	}
	if truncated {
		t.Fatal("a forged complete line is not a torn tail")
	}

	// A line that does not decode is corrupt too.
	truncated, err = Scan([]byte("not json\n"), "start", corruptErr,
		func(line []byte) Record { return nil },
		func(sequence int, r Record) error { return nil })
	if err == nil {
		t.Fatal("undecodable line was accepted")
	}
	if truncated {
		t.Fatal("an undecodable complete line is not a torn tail")
	}
}

func TestScanVisitsEveryRecordBeforeTheVisitorRefuses(t *testing.T) {
	file := &limitedFile{}
	w := NewWriter(file, "start", 1<<20, Messages{Limit: errors.New("limit"), Sync: errors.New("sync"), Encode: errors.New("encode")})
	for _, kind := range []string{"ready", "running"} {
		if err := w.Append(&testRecord{Kind: kind}); err != nil {
			t.Fatal(err)
		}
	}
	refused := errors.New("refused")
	_, err := Scan(file.data, "start", errors.New("corrupt"),
		func(line []byte) Record {
			var r testRecord
			if json.Unmarshal(line, &r) != nil {
				return nil
			}
			return &r
		},
		func(sequence int, r Record) error { return refused })
	if !errors.Is(err, refused) {
		t.Fatalf("want the visitor's error, got %v", err)
	}
}
