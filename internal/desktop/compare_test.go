package desktop_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/diff"
)

// One interface change, before and after. Two appointments are booked; in the
// working collection every control ID was regenerated, one appointment's
// *second* patient-identifier repetition changed while its first stayed as it
// was, and a third appointment was inserted between them. The filler order
// number is what identifies the same appointment on both sides, and it is
// declared rather than inferred.
const (
	cmpBookedBefore = "MSH|^~\\&|READMIT|TEST|RECV|LAB|20260101120000||SIU^S12|CTL-1|P|2.5.1\rSCH|PLACER-1^READMIT|FILLER-1^READMIT||||CHECKUP|ROUTINE\rPID|1||MRN-1^^^READMIT^MR||DOE^JANE\r"
	cmpMovedBefore  = "MSH|^~\\&|READMIT|TEST|RECV|LAB|20260101120100||SIU^S12|CTL-2|P|2.5.1\rSCH|PLACER-2^READMIT|FILLER-2^READMIT||||CHECKUP|ROUTINE\rPID|1||MRN-2^^^READMIT^MR~ALT-2^^^READMIT^AN||ROE^RICHARD\r"

	cmpBookedAfter   = "MSH|^~\\&|READMIT|TEST|RECV|LAB|20260101120000||SIU^S12|CTL-91|P|2.5.1\rSCH|PLACER-1^READMIT|FILLER-1^READMIT||||CHECKUP|ROUTINE\rPID|1||MRN-1^^^READMIT^MR||DOE^JANE\r"
	cmpInsertedAfter = "MSH|^~\\&|READMIT|TEST|RECV|LAB|20260101120030||SIU^S12|CTL-92|P|2.5.1\rSCH|PLACER-3^READMIT|FILLER-3^READMIT||||CHECKUP|ROUTINE\rPID|1||MRN-3^^^READMIT^MR||POE^PAT\r"
	cmpMovedAfter    = "MSH|^~\\&|READMIT|TEST|RECV|LAB|20260101120100||SIU^S12|CTL-93|P|2.5.1\rSCH|PLACER-2^READMIT|FILLER-2^READMIT||||CHECKUP|ROUTINE\rPID|1||MRN-2^^^READMIT^MR~ALT-9^^^READMIT^AN||ROE^RICHARD\r"

	// The same booking with its patient segment gone: a change in the segment
	// sequence rather than in a value.
	cmpBookedTrimmed = "MSH|^~\\&|READMIT|TEST|RECV|LAB|20260101120000||SIU^S12|CTL-91|P|2.5.1\rSCH|PLACER-1^READMIT|FILLER-1^READMIT||||CHECKUP|ROUTINE\r"

	// Two acknowledgements and nothing else. The window compares stored
	// messages, so a case holding only these has nothing inside the comparison.
	cmpAckOne = "MSH|^~\\&|RECV|LAB|READMIT|TEST|20260101120001||ACK^S12|CTL-501|P|2.5.1\rMSA|AA|CTL-1\r"
	cmpAckTwo = "MSH|^~\\&|RECV|LAB|READMIT|TEST|20260101120101||ACK^S12|CTL-502|P|2.5.1\rMSA|AA|CTL-2\r"

	cmpGarbage = "NOT-HL7-AT-ALL"

	// The filler order number identifies one appointment on both sides. The
	// message control ID deliberately does not: it was regenerated, and a
	// comparison that used it would pair nothing.
	cmpKey = "SCH-2.1"
)

// comparisonWorkspace is that change in an open workspace, with the app that
// reads it and the identity the window verified for the left collection.
func comparisonWorkspace(t *testing.T) (*desktop.App, string, string) {
	t.Helper()
	root := t.TempDir()
	state := t.TempDir()
	app := desktop.New(&chooser{}, filepath.Join(state, "recent.json"), filepath.Join(state, "filters.json"), filepath.Join(state, "session.json"))
	before := writeCase(t, root, "before", framed(cmpBookedBefore)+framed(cmpMovedBefore))
	writeCase(t, root, "after", framed(cmpBookedAfter)+framed(cmpInsertedAfter)+framed(cmpMovedAfter))
	return app, root, before.Identity
}

func compareRequest(root, identity, right string, keys ...string) desktop.CompareRequest {
	return desktop.CompareRequest{
		Workspace: root, Left: "before", Identity: identity, Right: right,
		Keys: keys, Offset: 0, Limit: desktop.MaxComparisonRows,
	}
}

// compared runs one comparison and fails the test if the facade refused it.
func compared(t *testing.T, app *desktop.App, request desktop.CompareRequest) *desktop.Comparison {
	t.Helper()
	result := app.Compare(request)
	if result.State != desktop.Completed || result.Comparison == nil {
		t.Fatalf("the facade refused a comparison it supports: %+v", result)
	}
	return result.Comparison
}

// The whole delivery in one comparison: the two collections align on a declared
// key, every regenerated control ID stays an ordinary field change, the
// appointment that moved names the position that moved, and the record only the
// right collection holds occupies a row of its own rather than shifting the
// rows after it.
func TestComparingTwoCollectionsPairsThemOnDeclaredKeysAndShowsWhatWasInserted(t *testing.T) {
	app, root, identity := comparisonWorkspace(t)
	comparison := compared(t, app, compareRequest(root, identity, "after", cmpKey))

	if comparison.Alignment != "declared-keys" || len(comparison.Keys) != 1 || comparison.Keys[0] != "SCH[1]-2[1].1" {
		t.Fatalf("the comparison did not align on the declared key in canonical form: %+v", comparison)
	}
	// The window says which versioned engine contract these rows came from and
	// which payloads were inside the comparison at all, so a comparison of
	// stored messages never reads as a comparison of everything a case holds.
	if comparison.Report != diff.Schema || comparison.Boundary != diff.Messages {
		t.Fatalf("the comparison does not name the contract and boundary it came from: %+v", comparison)
	}
	if comparison.Total != 3 || len(comparison.Rows) != 3 {
		t.Fatalf("two paired appointments and one inserted record are three rows: %+v", comparison.Rows)
	}
	kinds := []desktop.RowKind{desktop.PairedRow, desktop.PairedRow, desktop.InsertedRow}
	for i, row := range comparison.Rows {
		if row.Kind != kinds[i] || row.Position != i+1 {
			t.Fatalf("row %d is %q at position %d, not %q in order: %+v", i, row.Kind, row.Position, kinds[i], row)
		}
	}
	// Both panes render every row, so a row with nothing on one side says so
	// rather than being left out of that pane.
	inserted := comparison.Rows[2]
	if inserted.Left != nil || inserted.Right == nil {
		t.Fatalf("an inserted record did not leave the left pane empty: %+v", inserted)
	}
	if comparison.Summary.Inserted != 1 || comparison.Summary.Missing != 0 || comparison.Summary.Ambiguous != 0 {
		t.Fatalf("the counts do not describe one insertion and nothing else: %+v", comparison.Summary)
	}

	// The appointment whose patient identifier moved names the position that
	// moved and the regenerated control ID beside it, both as ordinary field
	// changes over the same selector grammar the inspector and a test spec use.
	moved := comparison.Rows[1]
	if moved.Status != "changed" || moved.Left == nil || moved.Right == nil {
		t.Fatalf("the moved appointment was not reported as a changed pair: %+v", moved)
	}
	changed := map[string]desktop.FieldDifference{}
	for _, field := range moved.Fields {
		changed[field.Selector] = field
	}
	if len(changed) != 2 {
		t.Fatalf("a regenerated control ID and a moved identifier are two field changes: %+v", moved.Fields)
	}
	// The repetition that changed is named, and the one beside it that did not
	// is not: repetitions are compared as the separate positions they are.
	if _, reported := changed["PID[1]-3[1]"]; reported {
		t.Fatalf("an unchanged first repetition was reported as a difference: %+v", moved.Fields)
	}
	for _, selector := range []string{"MSH[1]-10[1]", "PID[1]-3[2]"} {
		field, reported := changed[selector]
		if !reported || field.Status != "changed" {
			t.Fatalf("%s was not reported as a changed position: %+v", selector, moved.Fields)
		}
		if field.LeftState != "present" || field.RightState != "present" {
			t.Fatalf("%s lost the decoded state of either side: %+v", selector, field)
		}
		if field.Name == "" {
			t.Fatalf("%s carries no label from the bundled dictionary: %+v", selector, field)
		}
	}
	// The pair that only had its control ID regenerated is still a pair: an
	// identifier a system transformed is a field change, never an alignment.
	if booked := comparison.Rows[0]; booked.Status != "changed" || len(booked.Fields) != 1 || booked.Fields[0].Selector != "MSH[1]-10[1]" {
		t.Fatalf("a regenerated control ID alone was not the only change of the first pair: %+v", booked)
	}
}

// A comparison carries positions, states and counts. No message byte, field
// value or alignment-key value crosses this boundary: reading a value is the
// inspector, exactly as it is for the grid and for a reproducer.
func TestAComparisonCarriesNoMessageContent(t *testing.T) {
	app, root, identity := comparisonWorkspace(t)
	encoded, err := json.Marshal(compared(t, app, compareRequest(root, identity, "after", cmpKey)))
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"MRN-", "ALT-", "DOE", "JANE", "ROE", "RICHARD", "CTL-", "FILLER-", "PLACER-", "CHECKUP"} {
		if strings.Contains(string(encoded), leaked) {
			t.Fatalf("the comparison exposed message content %q: %s", leaked, encoded)
		}
	}
}

// Two copies of one verified case align by the occurrence identity the evidence
// already carries, so no key has to be declared to compare a copy with what it
// was copied from.
func TestCopiesOfOneCaseAlignWithoutDeclaredKeys(t *testing.T) {
	app, root, identity := comparisonWorkspace(t)
	writeCase(t, root, "copy", framed(cmpBookedBefore)+framed(cmpMovedBefore))

	comparison := compared(t, app, compareRequest(root, identity, "copy"))
	if comparison.Alignment != "source-occurrence" || len(comparison.Keys) != 0 {
		t.Fatalf("copies of one case were not aligned by their own occurrence identity: %+v", comparison)
	}
	if comparison.Summary.Paired != 2 || comparison.Summary.Changed != 0 || comparison.Summary.Unchanged != 2 {
		t.Fatalf("a copy differs from its original: %+v", comparison.Summary)
	}
	for _, row := range comparison.Rows {
		if row.Kind != desktop.PairedRow || row.Status != "unchanged" || len(row.Fields) != 0 {
			t.Fatalf("a copy produced a row that is not an unchanged pair: %+v", row)
		}
	}
}

// Two collections that are not copies of one another are not paired by
// guesswork. The refusal names what to do about it in the window's own words,
// without repeating a command-line option a person here cannot type.
func TestComparingUnrelatedCollectionsWithoutKeysIsRefusedWithARemedy(t *testing.T) {
	app, root, identity := comparisonWorkspace(t)
	refused := app.Compare(compareRequest(root, identity, "after"))
	if refused.State != desktop.Failed || refused.Comparison != nil {
		t.Fatalf("unrelated collections were compared with no declared key: %+v", refused)
	}
	if !strings.Contains(refused.Reason, "identify one record") {
		t.Fatalf("the refusal does not say what to declare: %q", refused.Reason)
	}
	if strings.Contains(refused.Reason, "--") {
		t.Fatalf("the window repeated a command-line option: %q", refused.Reason)
	}
	// Recovery: a refusal releases the slot and changes nothing, so declaring
	// the key and asking again is all it takes.
	if recovered := app.Compare(compareRequest(root, identity, "after", cmpKey)); recovered.State != desktop.Completed {
		t.Fatalf("a refused comparison left the facade unusable: %+v", recovered)
	}
}

// A key that names more than one record on a side settles nothing, and the
// comparison says so: every candidate of the group is its own row, on its own
// side, and none of them is paired with any other.
func TestADuplicatedKeyLeavesEveryCandidateAmbiguous(t *testing.T) {
	app, root, identity := comparisonWorkspace(t)
	duplicate := strings.Replace(cmpInsertedAfter, "FILLER-3", "FILLER-1", 1)
	writeCase(t, root, "duplicated", framed(cmpBookedAfter)+framed(duplicate)+framed(cmpMovedAfter))

	comparison := compared(t, app, compareRequest(root, identity, "duplicated", cmpKey))
	if comparison.Summary.Ambiguous != 1 || comparison.Summary.Paired != 1 {
		t.Fatalf("one duplicated key left one ambiguous group and one pair: %+v", comparison.Summary)
	}
	candidates := 0
	for _, row := range comparison.Rows {
		if row.Kind != desktop.AmbiguousRow {
			continue
		}
		candidates++
		if row.Group != 1 || row.Reason != "duplicate_key" {
			t.Fatalf("an ambiguous candidate does not name the group it belongs to: %+v", row)
		}
		if (row.Left == nil) == (row.Right == nil) {
			t.Fatalf("an ambiguous candidate was paired with another: %+v", row)
		}
	}
	if candidates != 3 {
		t.Fatalf("one left and two right candidates are three ambiguous rows, not %d: %+v", candidates, comparison.Rows)
	}
}

// Evidence nothing decoded is not compared and is not passed over. It keeps a
// row saying no key could place it, and it is listed as unsupported evidence,
// so a comparison can never read as complete when part of it was never read.
func TestOccurrencesNothingDecodedAreReportedRatherThanComparedAround(t *testing.T) {
	app, root, identity := comparisonWorkspace(t)
	writeCase(t, root, "damaged", framed(cmpBookedAfter)+framed(cmpGarbage)+framed(cmpMovedAfter))

	comparison := compared(t, app, compareRequest(root, identity, "damaged", cmpKey))
	unaligned := 0
	for _, row := range comparison.Rows {
		if row.Kind != desktop.UnalignedRow {
			continue
		}
		unaligned++
		if row.Left != nil || row.Right == nil || row.Reason != "payload_unavailable" {
			t.Fatalf("the undecoded occurrence was not reported on its own side with a reason: %+v", row)
		}
	}
	if unaligned != 1 || comparison.Summary.Unaligned != 1 {
		t.Fatalf("the undecoded occurrence did not keep a row of its own: %+v", comparison.Rows)
	}
	gaps := 0
	for _, gap := range comparison.Unsupported {
		if gap.Side == "right" && gap.Code == "unparsed" {
			gaps++
		}
	}
	if gaps != 1 {
		t.Fatalf("the undecoded occurrence was not listed as unsupported evidence: %+v", comparison.Unsupported)
	}
}

// A window of a comparison never reads as the whole of it: it says where it
// begins and how many rows the comparison holds, and a window past the last row
// is empty with the reason rather than a comparison that found nothing.
func TestAComparisonWindowNeverReadsAsTheWholeComparison(t *testing.T) {
	app, root, identity := comparisonWorkspace(t)

	first := compareRequest(root, identity, "after", cmpKey)
	first.Limit = 2
	page := compared(t, app, first)
	if len(page.Rows) != 2 || page.Total != 3 || page.Offset != 0 || page.Limit != 2 {
		t.Fatalf("a window of two rows did not state the size of the comparison: %+v", page)
	}
	if page.Rows[0].Position != 1 || page.Rows[1].Position != 2 {
		t.Fatalf("a windowed row lost its position in the comparison: %+v", page.Rows)
	}

	next := compareRequest(root, identity, "after", cmpKey)
	next.Offset, next.Limit = 2, 2
	rest := compared(t, app, next)
	if len(rest.Rows) != 1 || rest.Rows[0].Position != 3 || rest.Rows[0].Kind != desktop.InsertedRow {
		t.Fatalf("the last window does not continue the comparison: %+v", rest)
	}

	past := compareRequest(root, identity, "after", cmpKey)
	past.Offset = 3
	beyond := app.Compare(past)
	if beyond.State != desktop.Empty || beyond.Comparison == nil || beyond.Comparison.Total != 3 {
		t.Fatalf("a window past the last row did not stay a comparison of three rows: %+v", beyond)
	}
}

// Every refusal a comparison can make, made. None of them reads evidence it was
// not given, and none of them reports a path, a value or a host diagnostic.
func TestComparisonRefusesWhatItCannotRead(t *testing.T) {
	app, root, identity := comparisonWorkspace(t)
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("not evidence"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()

	refusals := map[string]desktop.CompareRequest{
		"a workspace that is not a folder":         {Workspace: filepath.Join(root, "notes.txt"), Left: "before", Identity: identity, Right: "after", Keys: []string{cmpKey}, Limit: 10},
		"a right collection outside the workspace": {Workspace: root, Left: "before", Identity: identity, Right: filepath.Join(outside, "elsewhere"), Keys: []string{cmpKey}, Limit: 10},
		"a right collection naming a parent":       {Workspace: root, Left: "before", Identity: identity, Right: "../after", Keys: []string{cmpKey}, Limit: 10},
		"an entry that is not a case bundle":       {Workspace: root, Left: "before", Identity: identity, Right: "notes.txt", Keys: []string{cmpKey}, Limit: 10},
		"a left collection that is not listed":     {Workspace: root, Left: "missing", Identity: identity, Right: "after", Keys: []string{cmpKey}, Limit: 10},
		"evidence that changed since it was shown": {Workspace: root, Left: "before", Identity: "0000", Right: "after", Keys: []string{cmpKey}, Limit: 10},
		"no identity to bind the comparison to":    {Workspace: root, Left: "before", Right: "after", Keys: []string{cmpKey}, Limit: 10},
		"a selector that is not a position":        {Workspace: root, Left: "before", Identity: identity, Right: "after", Keys: []string{"not a selector"}, Limit: 10},
		"a window beginning before the first row":  {Workspace: root, Left: "before", Identity: identity, Right: "after", Keys: []string{cmpKey}, Offset: -1, Limit: 10},
		"an unbounded window":                      {Workspace: root, Left: "before", Identity: identity, Right: "after", Keys: []string{cmpKey}, Limit: desktop.MaxComparisonRows + 1},
		"a window of no rows at all":               {Workspace: root, Left: "before", Identity: identity, Right: "after", Keys: []string{cmpKey}, Limit: 0},
	}
	for name, request := range refusals {
		t.Run(name, func(t *testing.T) {
			refused := app.Compare(request)
			if refused.State != desktop.Failed || refused.Comparison != nil {
				t.Fatalf("the facade compared %s: %+v", name, refused)
			}
			if refused.Reason == "" {
				t.Fatal("a refusal says nothing about itself")
			}
			for _, disclosed := range []string{root, outside, "MRN-", "FILLER-", "not a selector"} {
				if strings.Contains(refused.Reason, disclosed) {
					t.Fatalf("the refusal repeated %q: %q", disclosed, refused.Reason)
				}
			}
		})
	}
	// Recovery: every refusal above released the slot and changed nothing.
	if recovered := app.Compare(compareRequest(root, identity, "after", cmpKey)); recovered.State != desktop.Completed {
		t.Fatalf("a refused comparison left the facade unusable: %+v", recovered)
	}
}

// A comparison verifies two cases, so it holds the one operation slot for as
// long as it runs, and the slot is released whatever the outcome. Cancelling
// does not interrupt it, because it runs to completion under the readers' own
// limits once it starts.
func TestComparingHoldsTheSameOperationSlot(t *testing.T) {
	app, root, identity := comparisonWorkspace(t)

	reentrant := &chooser{folder: root}
	second := desktop.New(reentrant, filepath.Join(t.TempDir(), "recent.json"), filepath.Join(t.TempDir(), "filters.json"), filepath.Join(t.TempDir(), "session.json"))
	var concurrent desktop.CompareResult
	reentrant.before = func() { concurrent = second.Compare(compareRequest(root, identity, "after", cmpKey)) }
	if opened := second.SelectWorkspace(); opened.State != desktop.Completed {
		t.Fatalf("the first operation did not complete: %+v", opened)
	}
	if concurrent.State != desktop.Busy || concurrent.Comparison != nil {
		t.Fatalf("a comparison ran while another operation held the facade: %+v", concurrent)
	}

	app.Cancel()
	if uninterrupted := compared(t, app, compareRequest(root, identity, "after", cmpKey)); uninterrupted.Total != 3 {
		t.Fatalf("cancelling changed what a comparison reports: %+v", uninterrupted)
	}
	if recovered := app.Compare(compareRequest(root, identity, "after", cmpKey)); recovered.State != desktop.Completed {
		t.Fatalf("the facade stayed busy after its operation finished: %+v", recovered)
	}
}

// A record that lost a segment differs in its segment sequence, not only in the
// positions inside it, and a paired row names that separately: a segment that
// is no longer there is not the same statement as a value that changed.
func TestAPairedRowNamesTheSegmentSequenceThatDiffers(t *testing.T) {
	app, root, identity := comparisonWorkspace(t)
	writeCase(t, root, "trimmed", framed(cmpBookedTrimmed)+framed(cmpMovedAfter))

	comparison := compared(t, app, compareRequest(root, identity, "trimmed", cmpKey))
	trimmed := comparison.Rows[0]
	if trimmed.Kind != desktop.PairedRow || trimmed.Status != "changed" {
		t.Fatalf("the trimmed booking was not paired with the one it came from: %+v", trimmed)
	}
	if len(trimmed.Segments) != 1 {
		t.Fatalf("one removed segment is one difference in the segment sequence: %+v", trimmed.Segments)
	}
	if segment := trimmed.Segments[0]; segment.Position != 3 || segment.Left != "PID[1]" || segment.Right != "omitted" {
		t.Fatalf("the removed segment was not named where it was: %+v", segment)
	}
}

// Two collections whose every occurrence is outside the compared boundary hold
// nothing to compare, and the window says that rather than reporting a
// comparison that found no difference. How much was left outside is beside it,
// so an empty comparison can never read as two collections that agree.
func TestCollectionsHoldingNothingInsideTheComparisonAreEmptyRatherThanEqual(t *testing.T) {
	app, root, _ := comparisonWorkspace(t)
	acks := writeCase(t, root, "acks", framed(cmpAckOne)+framed(cmpAckTwo))
	writeCase(t, root, "acks-copy", framed(cmpAckOne)+framed(cmpAckTwo))

	result := app.Compare(desktop.CompareRequest{
		Workspace: root, Left: "acks", Identity: acks.Identity, Right: "acks-copy",
		Limit: desktop.MaxComparisonRows,
	})
	if result.State != desktop.Empty || result.Comparison == nil {
		t.Fatalf("two collections with nothing to compare were not reported as empty: %+v", result)
	}
	if !strings.Contains(result.Reason, "stored message") {
		t.Fatalf("the reason does not say what was missing: %q", result.Reason)
	}
	comparison := result.Comparison
	if comparison.Total != 0 || len(comparison.Rows) != 0 || comparison.Summary.Paired != 0 {
		t.Fatalf("a comparison of nothing produced rows: %+v", comparison)
	}
	if comparison.LeftSummary.Occurrences != 0 || comparison.LeftSummary.Excluded != 2 {
		t.Fatalf("the window did not say how much was outside this comparison: %+v", comparison.LeftSummary)
	}
	if comparison.RightSummary.Occurrences != 0 || comparison.RightSummary.Excluded != 2 {
		t.Fatalf("the window did not say how much was outside this comparison: %+v", comparison.RightSummary)
	}
}

// evidenceDigest is every byte of one artifact directory, by relative path and
// content, so a test can state that reading it changed nothing.
func evidenceDigest(t *testing.T, root string) string {
	t.Helper()
	sum := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum.Write([]byte(relative))
		sum.Write(content)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// A comparison reads. Both collections are byte-identical afterwards, whether
// the comparison completed or was refused, and nothing is written beside them.
func TestComparingChangesNeitherCollection(t *testing.T) {
	app, root, identity := comparisonWorkspace(t)
	before := map[string]string{"before": evidenceDigest(t, filepath.Join(root, "before")), "after": evidenceDigest(t, filepath.Join(root, "after"))}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}

	if compared(t, app, compareRequest(root, identity, "after", cmpKey)).Total != 3 {
		t.Fatal("the comparison this test reads did not run")
	}
	if refused := app.Compare(compareRequest(root, identity, "after")); refused.State != desktop.Failed {
		t.Fatalf("the refusal this test reads did not happen: %+v", refused)
	}

	for name, digest := range before {
		if evidenceDigest(t, filepath.Join(root, name)) != digest {
			t.Fatalf("comparing changed the %s collection", name)
		}
	}
	after, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(entries) {
		t.Fatalf("comparing wrote something into the workspace: %d entries became %d", len(entries), len(after))
	}
}
