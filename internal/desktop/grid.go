package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/grid"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/index"
	"github.com/bharm16/readmit/internal/operation"
)

// Row is one occurrence in the grid: where it is, what it is, and the observed
// time a filter compares against. Offset and Size are its byte span in the
// source that holds it, so a row names where the original bytes are.
//
// No message content is here. A grid displays evidence, and what it displays is
// positions, types and recorded times — never a field value, a message byte or
// an original source path. ObservedAt is absent where the case recorded none,
// which is why a time filter cannot keep that occurrence. Decoded says whether
// the case could decode this occurrence at all: one it could not carries no
// indexed field, so no question about a field can be answered about it.
type Row struct {
	ID         string           `json:"id"`
	SourceID   string           `json:"source_id"`
	Offset     int              `json:"offset"`
	Size       int              `json:"size"`
	Kind       bundle.EventKind `json:"kind"`
	Direction  bundle.Direction `json:"direction"`
	ObservedAt *time.Time       `json:"observed_at"`
	Decoded    bool             `json:"decoded"`
}

// Grid is one window over one filtered case, and everything a person needs to
// know about what is not in the window.
//
// Excluded is the number of occurrences the selected filter removed from this
// view. It is always present, so a filtered grid can never look like the whole
// case: a view that hides records without saying how many is the interface
// equivalent of a false negative. Undecided is an upper bound on what this
// filter could not settle — retained values the index kept only as a shortened
// prefix, counted over the whole case rather than over the occurrences the
// other axes kept. Undecodable is a fact about the case rather than about this
// view: occurrences nothing decoded, which carry no indexed field for any
// question to reach. Both over-state uncertainty rather than under-stating it.
type Grid struct {
	Case        string `json:"case"`
	Index       string `json:"index"`
	Identity    string `json:"identity"`
	Filter      string `json:"filter"`
	Offset      int    `json:"offset"`
	Limit       int    `json:"limit"`
	Total       int    `json:"total"`
	Matched     int    `json:"matched"`
	Excluded    int    `json:"excluded"`
	Undecided   int    `json:"undecided"`
	Undecodable int    `json:"undecodable"`
	Rows        []Row  `json:"rows"`
}

// GridResult carries one state. Grid is present whenever the case and its index
// were accepted, including when the window holds no row, because the counts are
// the answer in that case.
type GridResult struct {
	State  State  `json:"state"`
	Reason string `json:"reason,omitzero"`
	Grid   *Grid  `json:"grid,omitzero"`
}

func (r *GridResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

func (r refusal) grid() GridResult { return GridResult{State: r.state, Reason: r.reason} }

// IndexDetails describes what one index retains of one case, and its health.
type IndexDetails struct {
	IndexName      string     `json:"index_name"`
	CaseName       string     `json:"case_name"`
	Identity       string     `json:"identity"`
	Schema         string     `json:"schema"`
	Provenance     string     `json:"provenance"`
	Retention      string     `json:"retention"`
	RetainUntil    *time.Time `json:"retain_until,omitzero"`
	RetentionState string     `json:"retention_state"`
	Fields         []string   `json:"fields"`
	Sources        int        `json:"sources"`
	Records        int        `json:"records"`
	Decoded        int        `json:"decoded"`
	Undecodable    int        `json:"undecodable"`
	BuiltAt        time.Time  `json:"built_at"`
	Applicable     bool       `json:"applicable"`
	Stale          bool       `json:"stale,omitzero"`
	Damaged        bool       `json:"damaged,omitzero"`
	Expired        bool       `json:"expired,omitzero"`
	Unsupported    bool       `json:"unsupported,omitzero"`
}

// IndexResult carries one state and the described index when available.
type IndexResult struct {
	State  State         `json:"state"`
	Reason string        `json:"reason,omitzero"`
	Index  *IndexDetails `json:"index,omitzero"`
}

func (r *IndexResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

func (r refusal) indexResult() IndexResult { return IndexResult{State: r.state, Reason: r.reason} }

// BuildIndexRequest declares what one new index of a case retains.
type BuildIndexRequest struct {
	Workspace   string   `json:"workspace"`
	Case        string   `json:"case"`
	Identity    string   `json:"identity,omitzero"`
	Output      string   `json:"output"`
	Fields      []string `json:"fields"`
	Retention   string   `json:"retention"`
	RetainUntil string   `json:"retain_until"`
	Replace     bool     `json:"replace,omitzero"`
}

// BuildIndexResult carries one state and the newly built index.
type BuildIndexResult struct {
	State  State         `json:"state"`
	Reason string        `json:"reason,omitzero"`
	Index  *IndexDetails `json:"index,omitzero"`
}

func (r *BuildIndexResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

func (r refusal) buildIndex() BuildIndexResult {
	return BuildIndexResult{State: r.state, Reason: r.reason}
}

// OpenGrid renders one bounded window of one case through one index of it.
//
// Both are named by one entry of the open workspace. The case is verified by
// the same reader `readmit timeline` and OpenCase run, and the index is checked
// against that verified evidence and against its own declared retention before
// a single row is reported: a grid never rests on an index the evidence no
// longer supports, and never serves a view from one whose retention has ended.
// Every window re-reads and re-checks both for exactly that reason.
//
// The filter applied is whichever one SelectFilter selected, so moving from one
// case to another keeps the view a person set up. It runs to completion under
// the case reader's own limits once it starts, so it holds the operation slot
// but is not interruptible.
func (a *App) OpenGrid(workspace, name, indexName string, offset, limit int) GridResult {
	return run(a, false, false, func(context.Context) GridResult {
		return a.openGrid(workspace, name, indexName, offset, limit)
	})
}

func (a *App) openGrid(workspace, name, indexName string, offset, limit int) GridResult {
	root, declined := resolveFolder(workspace)
	if root == "" {
		return declined.grid()
	}
	casePath, err := artifactpath.Child(root, name)
	if err != nil {
		return GridResult{State: Failed, Reason: "a case must be named by one directory entry of the open workspace"}
	}
	// An index is one file of the open workspace, so the entry rule is checked
	// where it is owned and the entry itself is inspected without following a
	// symbolic link: a listing can never be used to reach a file outside the
	// folder the person opened.
	if err := artifactpath.EntryName(indexName); err != nil {
		return GridResult{State: Failed, Reason: "an index must be named by one entry of the open workspace"}
	}
	indexPath := artifactpath.JoinReference(root, indexName)
	if entry, err := os.Lstat(indexPath); err != nil || !entry.Mode().IsRegular() {
		return GridResult{State: Failed, Reason: "an index must be one regular file of the open workspace"}
	}
	opened, err := operation.OpenCase(casePath)
	if err != nil {
		return GridResult{State: Failed, Reason: err.Error()}
	}
	document, declined := openIndex(indexPath, opened)
	if declined.state != "" {
		return declined.grid()
	}
	saved, declined := a.savedFilters()
	if declined.state != "" {
		return declined.grid()
	}
	selected := saved.Find(saved.Selected)
	page, err := grid.Select(*document, time.Now().UTC(), selected, grid.Window{Offset: offset, Limit: limit})
	if err != nil {
		return GridResult{State: Failed, Reason: refusedQuery(err)}
	}
	return described(name, indexName, opened.Identity, saved.Selected, page)
}

// BuildIndex builds a new index or rebuilds an existing disposable index of one case.
func (a *App) BuildIndex(request BuildIndexRequest) BuildIndexResult {
	return run(a, true, true, func(ctx context.Context) BuildIndexResult {
		return a.buildIndex(ctx, request)
	})
}

func (a *App) buildIndex(ctx context.Context, request BuildIndexRequest) BuildIndexResult {
	fail := func(state State, reason string) BuildIndexResult {
		return BuildIndexResult{State: state, Reason: reason}
	}
	root, declined := resolveFolder(request.Workspace)
	if root == "" {
		return fail(declined.state, declined.reason)
	}
	casePath, err := artifactpath.Child(root, request.Case)
	if err != nil {
		return fail(Failed, "a case must be named by one directory entry of the open workspace")
	}
	outputName := strings.TrimSpace(request.Output)
	if outputName == "" {
		outputName = request.Case + ".index.json"
	}
	if err := artifactpath.EntryName(outputName); err != nil {
		return fail(Failed, "an index must be written to one new entry of the open workspace")
	}
	destination := artifactpath.JoinReference(root, outputName)

	if len(request.Fields) == 0 {
		return fail(Failed, "index build requires at least one field to retain")
	}
	if len(request.Fields) > index.MaxFields {
		return fail(Failed, "an index retains at most 16 fields")
	}
	policy := index.Policy{
		Fields:    make([]string, 0, len(request.Fields)),
		Retention: index.Retention(request.Retention),
	}
	for _, field := range request.Fields {
		sel, err := hl7.ParseSelector(field)
		if err != nil {
			return fail(Failed, "invalid field selector: "+err.Error())
		}
		policy.Fields = append(policy.Fields, sel.String())
	}
	if request.Retention == "" {
		return fail(Failed, "index build requires explicit retention: values, digests, or states")
	}
	until := strings.TrimSpace(request.RetainUntil)
	if until != "" && until != "indefinite" {
		instant, err := time.Parse(time.RFC3339, until)
		if err != nil {
			return fail(Failed, "a retention end is an RFC 3339 instant or indefinite")
		}
		utc := instant.UTC()
		policy.RetainUntil = &utc
	}
	if err := index.ValidatePolicy(policy); err != nil {
		return fail(Failed, err.Error())
	}

	opened, err := operation.OpenCase(casePath)
	if err != nil {
		return fail(Failed, err.Error())
	}
	if request.Identity != "" && opened.Identity != request.Identity {
		return fail(Failed, "case identity has changed since it was displayed")
	}

	if fi, err := os.Lstat(destination); err == nil {
		if !request.Replace {
			return fail(Failed, "an index at that destination already exists; choose another name or confirm replacement")
		}
		if !fi.Mode().IsRegular() {
			return fail(Failed, "an index destination must be a regular file")
		}
		if err := os.Remove(destination); err != nil {
			return fail(Failed, "cannot remove existing index for replacement")
		}
	}

	at := time.Now().UTC()
	document, err := index.Build(ctx, opened, policy, at)
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || (err != nil && strings.Contains(err.Error(), "cancelled")) {
		return fail(Cancelled, cancelledRefusal.reason)
	}
	if err != nil {
		return fail(Failed, err.Error())
	}

	if _, err := index.Write(destination, document); err != nil {
		return probeWriteFailure(root,
			"this account cannot write into the open workspace",
			"the index file could not be written in the open workspace").buildIndex()
	}

	details := indexDetails(outputName, document, opened, at)
	return BuildIndexResult{
		State: Completed,
		Index: &details,
	}
}

// DescribeIndex reports what one index retains of one case, or finds an applicable index in the workspace.
func (a *App) DescribeIndex(workspace, caseName, indexName string) IndexResult {
	return run(a, false, false, func(context.Context) IndexResult {
		return a.describeIndex(workspace, caseName, indexName)
	})
}

func (a *App) describeIndex(workspace, caseName, indexName string) IndexResult {
	root, declined := resolveFolder(workspace)
	if root == "" {
		return declined.indexResult()
	}
	casePath, err := artifactpath.Child(root, caseName)
	if err != nil {
		return IndexResult{State: Failed, Reason: "a case must be named by one directory entry of the open workspace"}
	}
	opened, err := operation.OpenCase(casePath)
	if err != nil {
		return IndexResult{State: Failed, Reason: err.Error()}
	}

	at := time.Now().UTC()

	if indexName != "" {
		return describeSpecificIndex(root, indexName, opened, at)
	}

	standardName := caseName + ".index.json"
	indexPath := filepath.Join(root, standardName)
	if info, err := os.Lstat(indexPath); err == nil && info.Mode().IsRegular() {
		res := describeSpecificIndex(root, standardName, opened, at)
		if res.State == Completed {
			return res
		}
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return IndexResult{State: Empty, Reason: "no index has been built for this case; build one to explore messages"}
	}

	var candidateResult *IndexResult
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == standardName {
			continue
		}
		if kind, ok := classify(root, entry.Name(), false); ok && kind == IndexArtifact {
			res := describeSpecificIndex(root, entry.Name(), opened, at)
			if res.State == Completed && res.Index != nil && res.Index.Applicable {
				return res
			}
			if candidateResult == nil && res.Index != nil {
				candidateResult = &res
			}
		}
	}

	if candidateResult != nil {
		return *candidateResult
	}

	if info, err := os.Lstat(indexPath); err == nil && info.Mode().IsRegular() {
		return describeSpecificIndex(root, standardName, opened, at)
	}

	return IndexResult{State: Empty, Reason: "no index has been built for this case; build one to explore messages"}
}

func describeSpecificIndex(root, indexName string, opened *bundle.Bundle, at time.Time) IndexResult {
	if err := artifactpath.EntryName(indexName); err != nil {
		return IndexResult{State: Failed, Reason: "an index must be named by one entry of the open workspace"}
	}
	indexPath := artifactpath.JoinReference(root, indexName)
	entry, err := os.Lstat(indexPath)
	if err != nil || !entry.Mode().IsRegular() {
		return IndexResult{State: Failed, Reason: "an index must be one regular file of the open workspace"}
	}

	document, err := index.Open(indexPath)
	switch {
	case errors.Is(err, index.ErrUnsupportedVersion):
		return IndexResult{
			State:  Failed,
			Reason: "the index was written under a contract version this release cannot read; build it again from this case",
			Index:  &IndexDetails{IndexName: indexName, Unsupported: true},
		}
	case errors.Is(err, index.ErrDamaged):
		return IndexResult{
			State:  Failed,
			Reason: "the index no longer matches what was written for it; the evidence is unchanged, build the index again",
			Index:  &IndexDetails{IndexName: indexName, Damaged: true},
		}
	case err != nil:
		if data, readErr := os.ReadFile(indexPath); readErr == nil {
			var decl struct {
				Schema string `json:"schema"`
			}
			_ = json.Unmarshal(data, &decl)
			if decl.Schema == index.Schema {
				return IndexResult{
					State:  Failed,
					Reason: "the index no longer matches what was written for it; the evidence is unchanged, build the index again",
					Index:  &IndexDetails{IndexName: indexName, Damaged: true},
				}
			}
		}
		return IndexResult{
			State:  Failed,
			Reason: "that entry is not an index this release reads; build one from this case",
		}
	}

	details := indexDetails(indexName, document, opened, at)
	if err := document.Describes(opened); err != nil {
		details.Applicable = false
		details.Stale = true
		return IndexResult{
			State:  Failed,
			Reason: "the index was built from different evidence than this case; build it again from this case",
			Index:  &details,
		}
	}
	if err := document.Usable(at); err != nil {
		details.Applicable = false
		details.Expired = true
		return IndexResult{
			State:  Failed,
			Reason: "the retention declared for this index has ended; build it again from this case, or delete it",
			Index:  &details,
		}
	}

	details.Applicable = true
	return IndexResult{
		State: Completed,
		Index: &details,
	}
}

func indexDetails(name string, doc index.Document, opened *bundle.Bundle, at time.Time) IndexDetails {
	decoded, undecodable := 0, 0
	for _, record := range doc.Records {
		if record.ParseError != "" {
			undecodable++
		} else {
			decoded++
		}
	}
	retentionState := "active"
	expired := false
	if doc.Policy.RetainUntil != nil {
		if doc.Usable(at) != nil {
			retentionState = "ended"
			expired = true
		}
	}
	stale := false
	if opened != nil && doc.Describes(opened) != nil {
		stale = true
	}
	return IndexDetails{
		IndexName:      name,
		CaseName:       doc.Case.Identity,
		Identity:       doc.Case.Identity,
		Schema:         doc.Schema,
		Provenance:     doc.Case.Provenance,
		Retention:      string(doc.Policy.Retention),
		RetainUntil:    doc.Policy.RetainUntil,
		RetentionState: retentionState,
		Fields:         doc.Policy.Fields,
		Sources:        len(doc.Case.Sources),
		Records:        len(doc.Records),
		Decoded:        decoded,
		Undecodable:    undecodable,
		BuiltAt:        doc.BuiltAt,
		Applicable:     !expired && !stale,
		Stale:          stale,
		Expired:        expired,
	}
}

// openIndex reads one index and refuses it the moment it disagrees with the
// verified case. A refused index never changes or blocks the evidence: every
// remedy below is to build the index again from the case it describes.
func openIndex(path string, opened *bundle.Bundle) (*index.Document, refusal) {
	document, err := index.Open(path)
	switch {
	case errors.Is(err, index.ErrUnsupportedVersion):
		return nil, refusal{Failed, "the index was written under a contract version this release cannot read; build it again from this case"}
	case errors.Is(err, index.ErrDamaged):
		return nil, refusal{Failed, "the index no longer matches what was written for it; the evidence is unchanged, build the index again"}
	case err != nil:
		return nil, refusal{Failed, "that entry is not an index this release reads; build one from this case"}
	}
	if err := document.Describes(opened); err != nil {
		return nil, refusal{Failed, "the index was built from different evidence than this case; build it again from this case"}
	}
	return &document, refusal{}
}

// refusedQuery separates the one refusal with a remedy a person can act on from
// every other reason a filter cannot be answered. It reports neither the term
// that was searched for nor the field that was asked about.
func refusedQuery(err error) string {
	if errors.Is(err, index.ErrExpired) {
		return "the retention declared for this index has ended; build it again from this case, or delete it"
	}
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "this index retains no values") ||
			strings.Contains(msg, "a digests index cannot answer contains") ||
			strings.Contains(msg, "that field is not retained by this index") {
			return msg
		}
	}
	return "this index cannot answer the selected filter; it retains only the fields and the form it was built with"
}

// described reports the window and the counts around it. A window holding no
// row is empty with the reason it is empty, and still carries the counts,
// because how many records were excluded is the answer in that case.
func described(name, indexName, identity, filter string, page grid.Page) GridResult {
	rows := make([]Row, 0, len(page.Rows))
	for _, record := range page.Rows {
		rows = append(rows, Row{
			ID: record.ID, SourceID: record.SourceID, Offset: record.Offset, Size: record.Size,
			Kind: record.Kind, Direction: record.Direction,
			ObservedAt: record.ObservedAt, Decoded: record.ParseError == "",
		})
	}
	described := &Grid{
		Case: name, Index: indexName, Identity: identity, Filter: filter,
		Offset: page.Offset, Limit: page.Limit, Total: page.Total, Matched: page.Matched,
		Excluded: page.Excluded, Undecided: page.Undecided, Undecodable: page.Undecodable, Rows: rows,
	}
	switch {
	case page.Matched == 0 && page.Total == 0:
		return GridResult{State: Empty, Reason: "this case holds no occurrence", Grid: described}
	case page.Matched == 0:
		return GridResult{State: Empty, Reason: "the selected filter excluded every occurrence of this case", Grid: described}
	case len(rows) == 0:
		return GridResult{State: Empty, Reason: "this window begins past the last occurrence the filter kept", Grid: described}
	}
	return GridResult{State: Completed, Grid: described}
}
