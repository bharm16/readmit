package desktop_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/grid"
	"github.com/bharm16/readmit/internal/index"
)

func fieldValueCapture(t *testing.T) (*desktop.App, string, desktop.FieldValueScope) {
	t.Helper()
	app, root, _ := gridWorkspace(t)
	var wire strings.Builder
	for i := 0; i < 600; i++ {
		tail := "ZPD|OWNED_A\r"
		switch {
		case i >= 580 && i < 590:
			tail = "ZPD|\r"
		case i >= 590 && i < 595:
			tail = "ZPD|\"\"\r"
		case i >= 595 && i < 598:
			tail = ""
		case i >= 598:
			tail = "ZPD|\\Q\\\r"
		case i >= 300:
			tail = "ZPD|OWNED_B\r"
		}
		wire.WriteString(framed(fmt.Sprintf("MSH|^~\\&|READMIT|TEST|RECV|LAB|20260101||ADT^A08|owned-%d|P|2.5.1\r", i) + tail))
	}
	source := writeCase(t, root, "large-field-scope", wire.String())
	return app, root, desktop.FieldValueScope{Workspace: root, Case: "large-field-scope", Identity: source.Identity, Query: grid.Query{}}
}

func TestFieldValuesCountFullScopeAndKeepHiddenBucketsOpaque(t *testing.T) {
	app, _, scope := fieldValueCapture(t)
	request := desktop.FieldValuesRequest{Scope: scope, Selector: "ZPD-1", Limit: 1}
	hidden := app.ReadFieldValues(request)
	if hidden.State != desktop.Completed || hidden.Total != 600 || hidden.Matched != 600 || hidden.Scanned != 600 || hidden.Complete || !hidden.ScanComplete || hidden.Counts.Present != 580 || hidden.Counts.Empty != 10 || hidden.Counts.Null != 5 || hidden.Counts.Omitted != 3 || hidden.Counts.Undecodable != 2 || hidden.GroupCount != 5 || len(hidden.Rows) != 1 {
		t.Fatalf("page-sized or merged state counts: %+v", hidden)
	}
	request.Snapshot = hidden.Snapshot
	request.Offset = 1
	request.Limit = 100
	page := app.ReadFieldValues(request)
	if page.State != desktop.Completed {
		t.Fatal(page)
	}
	var present desktop.FieldValueBucket
	for _, row := range append(hidden.Rows, page.Rows...) {
		if row.Value != "" || !strings.HasPrefix(row.ID, "fv:") {
			t.Fatalf("hidden source label or deterministic key: %+v", row)
		}
		if row.State == "present" {
			present = row
			if !row.Hidden || row.Messages != 580 {
				t.Fatalf("hidden distribution fingerprint: %+v", row)
			}
		}
	}
	members := app.ReadFieldValueOccurrences(desktop.FieldValueOccurrencesRequest{Scope: scope, Selector: "ZPD-1", Snapshot: hidden.Snapshot, Bucket: present.ID, Limit: 200})
	if members.State != desktop.Completed || members.Total != 580 || len(members.Rows) != 200 || members.Rows[0].ID != "s0001-e000001" || members.Rows[199].ID != "s0001-e000200" {
		t.Fatalf("not exact occurrence membership: %+v", members)
	}
	members2 := app.ReadFieldValueOccurrences(desktop.FieldValueOccurrencesRequest{Scope: scope, Selector: "ZPD-1", Snapshot: hidden.Snapshot, Bucket: present.ID, Offset: 400, Limit: 200})
	if members2.State != desktop.Completed || len(members2.Rows) != 180 || members2.Rows[179].ID != "s0001-e000580" {
		t.Fatalf("wrong complete bucket page: %+v", members2)
	}
	request.Snapshot = ""
	request.Offset = 0
	request.Reveal = true
	revealed := app.ReadFieldValues(request)
	counts := map[string]int{}
	for _, row := range revealed.Rows {
		counts[row.Value] = row.Messages
	}
	if revealed.State != desktop.Completed || counts["OWNED_A"] != 300 || counts["OWNED_B"] != 280 {
		t.Fatalf("revealed values differ from independent set: %+v", revealed)
	}
}

func TestFieldValueBucketRefusesChangedQuerySourceAndBorrowedSelector(t *testing.T) {
	app, root, scope := fieldValueCapture(t)
	counted := app.ReadFieldValues(desktop.FieldValuesRequest{Scope: scope, Selector: "ZPD-1"})
	if counted.State != desktop.Completed {
		t.Fatal(counted)
	}
	request := desktop.FieldValueOccurrencesRequest{Scope: scope, Selector: "ZPD-1", Snapshot: counted.Snapshot, Bucket: counted.Rows[0].ID}
	request.Scope.Query = grid.Query{Sources: []string{"missing-source"}}
	if result := app.ReadFieldValueOccurrences(request); result.State != desktop.Failed || len(result.Rows) != 0 {
		t.Fatalf("borrowed scope membership: %+v", result)
	}
	request.Scope = scope
	request.Selector = "ZPD-2"
	if result := app.ReadFieldValueOccurrences(request); result.State != desktop.Failed {
		t.Fatal("borrowed selector bucket")
	}
	sourcefile := filepath.Join(root, scope.Case, "payloads", "s0001-e000001.bin")
	raw, err := os.ReadFile(sourcefile)
	if err != nil {
		t.Fatal(err)
	}
	raw[10] = 'X'
	if err = os.WriteFile(sourcefile, raw, 0600); err != nil {
		t.Fatal(err)
	}
	request.Selector = "ZPD-1"
	if result := app.ReadFieldValueOccurrences(request); result.State != desktop.Failed || len(result.Rows) != 0 {
		t.Fatal("served count against changed source")
	}
}

func TestFieldValuesReportUndecidedFilterScopeInsteadOfACompleteCount(t *testing.T) {
	app, root, _ := gridWorkspace(t)
	source := writeCase(t, root, "uncertain-count", framed("MSH|^~\\&|A|B|C|D|20260101||ADT^A08|owned|P|2.5.1\rZPD|"+strings.Repeat("a", 140)+"TAIL\r"))
	result := app.ReadFieldValues(desktop.FieldValuesRequest{Scope: desktop.FieldValueScope{Workspace: root, Case: "uncertain-count", Identity: source.Identity, Query: grid.Query{Fields: []grid.FieldPredicate{{Selector: "ZPD[1]-1[1]", Match: index.Contains, Term: "TAIL", State: ""}}}}, Selector: "MSH-10"})
	if result.State != desktop.Completed || result.Complete || result.ScopeUndecided != 1 || result.Matched != 0 || result.Counts.Present != 0 {
		t.Fatalf("uncertain scope claimed complete: %+v", result)
	}
}

func TestFieldValuesOpaqueMembershipExpiresAndNeverBroadensRepeatedSelector(t *testing.T) {
	app, root, _ := gridWorkspace(t)
	source := writeCase(t, root, "repetition-scope", framed("MSH|^~\\&|A|B|C|D|20260101||ADT^A08|id|P|2.5.1\rZPD|FIRST~SECOND\r")+framed("MSH|^~\\&|A|B|C|D|20260101||ADT^A08|id2|P|2.5.1\rZPD|FIRST\r"))
	scope := desktop.FieldValueScope{Workspace: root, Case: "repetition-scope", Identity: source.Identity, Query: grid.Query{}}
	request := desktop.FieldValuesRequest{Scope: scope, Selector: "ZPD[1]-1[2]", Reveal: true}
	first := app.ReadFieldValues(request)
	if first.State != desktop.Completed || first.Counts.Present != 1 || first.Counts.Omitted != 1 || first.Matched != 2 || !first.Complete {
		t.Fatalf("all repetitions counted as messages: %+v", first)
	}
	found := false
	for _, row := range first.Rows {
		if row.Value == "SECOND" {
			found = true
			if row.Messages != 1 {
				t.Fatal("wrong count unit")
			}
		}
	}
	if !found {
		t.Fatal("explicit second repetition unavailable")
	}
	for range 4 {
		again := app.ReadFieldValues(request)
		if again.State != desktop.Completed || again.Snapshot == first.Snapshot {
			t.Fatal("bucket IDs are deterministic or not refreshed")
		}
	}
	stale := app.ReadFieldValueOccurrences(desktop.FieldValueOccurrencesRequest{Scope: scope, Selector: request.Selector, Reveal: true, Snapshot: first.Snapshot, Bucket: first.Rows[0].ID})
	if stale.State != desktop.Failed || len(stale.Rows) > 0 {
		t.Fatal("expired opaque membership was silently guessed")
	}
}

func TestFieldValuesGroupingBoundReportsUnresolvedValueRatherThanPrefixCount(t *testing.T) {
	app, root, _ := gridWorkspace(t)
	source := writeCase(t, root, "large-value-count", framed("MSH|^~\\&|A|B|C|D|20260101||ADT^A08|id|P|2.5.1\rZPD|"+strings.Repeat("x", 4097)+"\r"))
	scope := desktop.FieldValueScope{Workspace: root, Case: "large-value-count", Identity: source.Identity, Query: grid.Query{}}
	result := app.ReadFieldValues(desktop.FieldValuesRequest{Scope: scope, Selector: "ZPD-1", Reveal: true})
	if result.State != desktop.Completed || result.Complete || !result.ScanComplete || result.Counts.Undecided != 1 || len(result.Rows) != 1 || result.Rows[0].Value != "" || result.Rows[0].State != "undecided" {
		t.Fatalf("truncated prefix became a value group: %+v", result)
	}
}

func TestFieldValuesNamedCancellationReturnsNoPartialBucketsAndRecovers(t *testing.T) {
	app, _, scope := fieldValueCapture(t)
	request := desktop.FieldValuesRequest{Scope: scope, Selector: "ZPD-1", Reveal: true}
	finished := make(chan desktop.FieldValuesResult, 1)
	go func() { finished <- app.ReadFieldValues(request) }()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case result := <-finished:
			if result.State != desktop.Cancelled || len(result.Rows) != 0 || result.Complete || result.Snapshot != "" {
				t.Fatalf("cancelled partial buckets reported complete: %+v", result)
			}
			next := app.ReadFieldValues(request)
			if next.State != desktop.Completed || next.Counts.Present != 580 {
				t.Fatalf("count did not recover: %+v", next)
			}
			return
		case <-tick.C:
			app.Cancel(desktop.FieldValuesOperation)
		case <-deadline.C:
			t.Fatal("named field count did not stop within its bounded read")
		}
	}
}
