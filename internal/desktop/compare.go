package desktop

import (
	"errors"
	"strconv"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/diff"
	"github.com/bharm16/readmit/internal/hl7"
)

// MaxComparisonRows bounds one window of a comparison. Two collections align
// into one row list, and a window of that list is rendered; the whole list is
// never handed to the interface at once.
const MaxComparisonRows = 200

// RowKind is what one row of the panes is. The set is closed, so the interface
// can be held to it the way it is held to an occurrence kind or an artifact
// kind rather than to a bare string.
type RowKind string

// A row is one line of both panes, so every kind states what is on each side of
// it — including the side that holds nothing, which is the whole point of an
// inserted or a missing record.
const (
	// PairedRow has an occurrence on both sides, aligned by a known mapping or
	// by the declared keys.
	PairedRow RowKind = "paired"
	// MissingRow has an occurrence on the left and nothing on the right.
	MissingRow RowKind = "missing"
	// InsertedRow has an occurrence on the right and nothing on the left.
	InsertedRow RowKind = "inserted"
	// AmbiguousRow is one candidate of a group whose key is duplicated. Each
	// candidate is its own row on its own side: no candidate is paired with
	// another, because the evidence does not say which one it would be.
	AmbiguousRow RowKind = "ambiguous"
	// UnalignedRow is one occurrence no key could place, with the reason.
	UnalignedRow RowKind = "unaligned"
)

// FieldDifference is one compared position and what each side held there.
//
// No value is here. Selector is the canonical form of the shared field
// selector, so it is the same position the inspector addresses and the same
// string a test spec takes; LeftState and RightState are the decoded states the
// comparison distinguishes — present, empty, explicit null and omitted — which
// say that a position changed without saying what it changed to. Reading the
// bytes is the inspector, deliberately, exactly as it is for a reproducer.
//
// Status is `changed` where both sides were compared and differ, and
// `uncompared` where a side could not be decoded at all; an undecodable
// position is never reported as an equal one.
type FieldDifference struct {
	Selector   string    `json:"selector"`
	Name       string    `json:"name,omitzero"`
	Status     string    `json:"status"`
	LeftState  hl7.State `json:"left_state"`
	RightState hl7.State `json:"right_state"`
}

// ComparisonRow is one line of the two panes. Left and Right are the
// occurrences it holds, and either is absent where that side holds none, so a
// record inserted on one side occupies a row rather than shifting every row
// after it. Status is the pair's outcome, Reason is why an ambiguous or
// unaligned row was not paired, and Group numbers the candidates of one
// duplicated key so a person can see the whole group that was refused.
type ComparisonRow struct {
	Position int                  `json:"position"`
	Kind     RowKind              `json:"kind"`
	Status   string               `json:"status,omitzero"`
	Reason   string               `json:"reason,omitzero"`
	Group    int                  `json:"group,omitzero"`
	Left     *diff.Reference      `json:"left,omitzero"`
	Right    *diff.Reference      `json:"right,omitzero"`
	Fields   []FieldDifference    `json:"fields,omitzero"`
	Segments []diff.SegmentChange `json:"segments,omitzero"`
}

// Comparison is one comparison of two collections, rendered as rows both panes
// share. Every count is the engine's own: Summary says how many records were
// paired, changed, inserted, missing, ambiguous and unaligned over the whole
// comparison, and Total how many rows it produced, so a window of it can never
// read as the whole of it. Unsupported is every evidence gap the comparison
// found and did not compare around. Scope is the engine's statement of what a
// field comparison does and does not establish.
//
// Report is the versioned engine contract these rows were laid out from, and
// Boundary which payloads of each collection were inside the comparison at all,
// so a comparison of stored messages can never read as a comparison of
// everything the two collections hold. This result is itself a typed value the
// interface reads, not a stored document: nothing here is written anywhere.
type Comparison struct {
	Left         string             `json:"left"`
	Right        string             `json:"right"`
	Report       string             `json:"report"`
	Boundary     diff.Boundary      `json:"boundary"`
	LeftSummary  diff.InputSummary  `json:"left_summary"`
	RightSummary diff.InputSummary  `json:"right_summary"`
	Alignment    string             `json:"alignment"`
	Scope        string             `json:"scope"`
	Keys         []string           `json:"keys"`
	Fields       []string           `json:"fields"`
	Summary      diff.Summary       `json:"summary"`
	Offset       int                `json:"offset"`
	Limit        int                `json:"limit"`
	Total        int                `json:"total"`
	Rows         []ComparisonRow    `json:"rows"`
	Unsupported  []diff.Unsupported `json:"unsupported"`
}

// CompareRequest names the two collections and how they are aligned.
//
// Left is the case the window has verified and Identity is the identity it
// displayed for it, so a comparison is refused rather than run against evidence
// that changed since. Right is any other case of the open workspace. Keys are
// the field selectors that identify one record on both sides, needed whenever
// the two collections are not copies of one another; Fields narrows the
// comparison to named positions, and an empty list visits every field
// repetition both messages hold.
type CompareRequest struct {
	Workspace string   `json:"workspace"`
	Left      string   `json:"left"`
	Identity  string   `json:"identity"`
	Right     string   `json:"right"`
	Keys      []string `json:"keys"`
	Fields    []string `json:"fields"`
	Offset    int      `json:"offset"`
	Limit     int      `json:"limit"`
}

// CompareResult carries one state. Comparison is present whenever both
// collections were read and aligned, including when the window holds no row,
// because the counts are the answer in that case.
type CompareResult struct {
	State      State       `json:"state"`
	Reason     string      `json:"reason,omitzero"`
	Comparison *Comparison `json:"comparison,omitzero"`
}

func (r refusal) comparison() CompareResult {
	return CompareResult{State: r.state, Reason: r.reason}
}

// Compare aligns two collections of the open workspace and reports them as the
// rows of two panes.
//
// It decides nothing about either collection. The same engine the command line
// runs verifies both, applies the known source mapping when there is one and
// the declared keys otherwise, and reports what it could not settle — so what
// the window shows is exactly what `readmit diff` reports over the same
// evidence, laid out as rows rather than as a document. No ignore rule is
// applied here, so nothing a comparison found is suppressed before it is shown.
//
// It reads two verified artifacts under their readers' own limits and runs to
// completion once it starts, so it holds the operation slot but is not
// interruptible. Neither collection is changed, and no message byte or field
// value crosses this boundary.
func (a *App) Compare(request CompareRequest) CompareResult {
	release, claimed := a.claim()
	if !claimed {
		return busyRefusal.comparison()
	}
	defer release()
	if request.Offset < 0 || request.Limit < 1 || request.Limit > MaxComparisonRows {
		return CompareResult{State: Failed, Reason: "a comparison renders a window beginning at or after its first row, of between 1 and " + strconv.Itoa(MaxComparisonRows) + " rows"}
	}
	root, declined := resolveFolder(request.Workspace)
	if root == "" {
		return declined.comparison()
	}
	left, declined := comparedCase(root, request.Left)
	if left == "" {
		return declined.comparison()
	}
	right, declined := comparedCase(root, request.Right)
	if right == "" {
		return declined.comparison()
	}
	// The left collection is the one the window verified and displayed, so the
	// comparison is bound to that identity the way every other read of an open
	// case is. The right one was chosen here and is reported as it was read.
	opened, err := bundle.Open(left)
	if err != nil {
		return CompareResult{State: Failed, Reason: "the case could not be verified as complete, unmodified evidence"}
	}
	if request.Identity == "" || request.Identity != opened.Identity {
		return CompareResult{State: Failed, Reason: "the case identity changed; open the case again before comparing it"}
	}
	report, err := diff.Compare(
		diff.Input{Path: left},
		diff.Input{Path: right},
		diff.Options{Keys: request.Keys, Fields: request.Fields},
	)
	if err != nil {
		return CompareResult{State: Failed, Reason: refusedComparison(err)}
	}
	return windowed(request, report)
}

// comparedCase resolves one named entry of the open workspace and refuses
// anything that is not a case bundle this release reads. The window lists a run,
// a result or a report as an unsupported entry, so offering to compare one here
// would contradict the listing beside it.
func comparedCase(root, name string) (string, refusal) {
	path, err := artifactpath.Child(root, name)
	if err != nil {
		return "", refusal{Failed, "a compared collection must be named by one directory entry of the open workspace"}
	}
	if _, err := bundle.Describe(path); err != nil {
		return "", refusal{Failed, "a comparison reads two case bundles this release supports"}
	}
	return path, refusal{}
}

// refusedComparison separates the one refusal with a remedy a person acts on in
// the window from every other reason a comparison cannot be produced.
//
// Only the alignment refusal is reworded, and only because its own sentence
// names a command-line option a person here cannot type. Every other diagnostic
// the engine returns is a fixed sentence carrying no path, no file name and no
// value, and each one names a scope to narrow or evidence to rebuild, so it is
// reported as it is rather than replaced by a sentence that says less.
func refusedComparison(err error) string {
	if errors.Is(err, diff.ErrKeysRequired) {
		return "these collections are not copies of one another; name the fields that identify one record, such as MSH-10, to align them"
	}
	return err.Error()
}

// windowed turns one report into the rows both panes share and returns the
// requested window of them. A window holding no row is empty with the reason it
// is empty, and still carries the counts, because how much was left outside the
// comparison is the answer in that case.
func windowed(request CompareRequest, report diff.Report) CompareResult {
	all := comparisonRows(report)
	window := make([]ComparisonRow, 0, request.Limit)
	if request.Offset < len(all) {
		window = append(window, all[request.Offset:min(len(all), request.Offset+request.Limit)]...)
	}
	described := &Comparison{
		Left: request.Left, Right: request.Right,
		Report: report.Schema, Boundary: report.Boundary,
		LeftSummary: report.Left, RightSummary: report.Right,
		Alignment: report.Alignment, Scope: report.Scope,
		Keys: report.Keys, Fields: report.Fields, Summary: report.Summary,
		Offset: request.Offset, Limit: request.Limit, Total: len(all),
		Rows: window, Unsupported: report.Unsupported,
	}
	if described.Keys == nil {
		described.Keys = []string{}
	}
	if described.Fields == nil {
		described.Fields = []string{}
	}
	if described.Unsupported == nil {
		described.Unsupported = []diff.Unsupported{}
	}
	switch {
	case len(all) == 0:
		// Every occurrence both collections hold is outside the compared
		// boundary — a pair of cases holding only acknowledgements, say. The
		// excluded counts beside this say how much that was.
		return CompareResult{State: Empty, Reason: "neither collection holds a stored message inside this comparison", Comparison: described}
	case len(window) == 0:
		return CompareResult{State: Empty, Reason: "this window begins past the last row of this comparison", Comparison: described}
	}
	return CompareResult{State: Completed, Comparison: described}
}

// comparisonRows lays one report out as the lines both panes render: the
// records that were paired, then the ones the right collection does not hold,
// then the ones only it holds, then every candidate of a duplicated key, then
// every occurrence no key placed.
//
// The order is what the comparison found, and nothing else. It is not the order
// either collection recorded and it is not evidence of chronology: an alignment
// that implied one would be the hidden assumption a comparison exists to avoid.
func comparisonRows(report diff.Report) []ComparisonRow {
	rows := make([]ComparisonRow, 0, len(report.Pairs)+len(report.Missing)+len(report.Inserted)+len(report.Unaligned)+report.Summary.Ambiguous)
	for _, pair := range report.Pairs {
		rows = append(rows, ComparisonRow{
			Kind: PairedRow, Status: pair.Status,
			Left: reference(pair.Left), Right: reference(pair.Right),
			Fields: differences(pair.Fields), Segments: pair.Segments,
		})
	}
	for _, missing := range report.Missing {
		rows = append(rows, ComparisonRow{Kind: MissingRow, Left: reference(missing)})
	}
	for _, inserted := range report.Inserted {
		rows = append(rows, ComparisonRow{Kind: InsertedRow, Right: reference(inserted)})
	}
	for number, ambiguity := range report.Ambiguous {
		for _, candidate := range ambiguity.Left {
			rows = append(rows, ComparisonRow{Kind: AmbiguousRow, Reason: ambiguity.Reason, Group: number + 1, Left: reference(candidate)})
		}
		for _, candidate := range ambiguity.Right {
			rows = append(rows, ComparisonRow{Kind: AmbiguousRow, Reason: ambiguity.Reason, Group: number + 1, Right: reference(candidate)})
		}
	}
	for _, unaligned := range report.Unaligned {
		row := ComparisonRow{Kind: UnalignedRow, Reason: unaligned.Reason}
		if unaligned.Side == "left" {
			row.Left = reference(unaligned.Reference)
		} else {
			row.Right = reference(unaligned.Reference)
		}
		rows = append(rows, row)
	}
	for i := range rows {
		rows[i].Position = i + 1
	}
	return rows
}

func reference(of diff.Reference) *diff.Reference { return &of }

// differences drops the value side of every field the engine compared. A
// comparison run from the window never asks for values, so there is none to
// drop; taking only the members that carry no value keeps that a property of
// what crosses the boundary rather than of a flag somebody has to keep unset.
func differences(changes []diff.FieldChange) []FieldDifference {
	if len(changes) == 0 {
		return nil
	}
	fields := make([]FieldDifference, 0, len(changes))
	for _, change := range changes {
		fields = append(fields, FieldDifference{
			Selector:  change.Selector,
			Name:      change.Name,
			Status:    change.Status,
			LeftState: change.Left.State, RightState: change.Right.State,
		})
	}
	return fields
}
