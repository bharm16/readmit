package desktop

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/fieldvalues"
	"github.com/bharm16/readmit/internal/grid"
	"github.com/bharm16/readmit/internal/hl7"
)

const FieldValuesOperation = "field-values-read"
const fieldValueSnapshotLimit = 4

type FieldValueScope struct {
	Workspace string     `json:"workspace"`
	Case      string     `json:"case"`
	Identity  string     `json:"identity"`
	Query     grid.Query `json:"query"`
}
type FieldValuesRequest struct {
	Scope    FieldValueScope `json:"scope"`
	Selector string          `json:"selector"`
	Reveal   bool            `json:"reveal"`
	Snapshot string          `json:"snapshot,omitzero"`
	Offset   int             `json:"offset"`
	Limit    int             `json:"limit"`
}
type FieldValueBucket struct {
	ID       string `json:"id"`
	State    string `json:"state"`
	Value    string `json:"value,omitzero"`
	Hidden   bool   `json:"hidden"`
	Messages int    `json:"messages"`
}
type FieldValuesResult struct {
	State            State              `json:"state"`
	Reason           string             `json:"reason,omitzero"`
	Identity         string             `json:"identity"`
	ScopeIdentity    string             `json:"scope_identity"`
	Snapshot         string             `json:"snapshot"`
	Selector         string             `json:"selector"`
	Unit             string             `json:"unit"`
	Total            int                `json:"total"`
	Matched          int                `json:"matched"`
	ScopeUndecided   int                `json:"scope_undecided"`
	ScopeUndecodable int                `json:"scope_undecodable"`
	Scanned          int                `json:"scanned"`
	Complete         bool               `json:"complete"`
	ScanComplete     bool               `json:"scan_complete"`
	Revealed         bool               `json:"revealed"`
	Counts           fieldvalues.Counts `json:"counts"`
	Rows             []FieldValueBucket `json:"rows"`
	GroupCount       int                `json:"group_count"`
	Offset           int                `json:"offset"`
	Limit            int                `json:"limit"`
}

func (r *FieldValuesResult) refuse(state State, reason string) {
	*r = FieldValuesResult{State: state, Reason: reason, Rows: []FieldValueBucket{}}
}

type FieldValueOccurrencesRequest struct {
	Scope    FieldValueScope `json:"scope"`
	Selector string          `json:"selector"`
	Reveal   bool            `json:"reveal"`
	Snapshot string          `json:"snapshot"`
	Bucket   string          `json:"bucket"`
	Offset   int             `json:"offset"`
	Limit    int             `json:"limit"`
}
type FieldValueOccurrencesResult struct {
	State         State        `json:"state"`
	Reason        string       `json:"reason,omitzero"`
	Identity      string       `json:"identity"`
	ScopeIdentity string       `json:"scope_identity"`
	Snapshot      string       `json:"snapshot"`
	Bucket        string       `json:"bucket"`
	Rows          []MessageRow `json:"rows"`
	Total         int          `json:"total"`
	Offset        int          `json:"offset"`
	Limit         int          `json:"limit"`
}

func (r *FieldValueOccurrencesResult) refuse(state State, reason string) {
	*r = FieldValueOccurrencesResult{State: state, Reason: reason, Rows: []MessageRow{}}
}

type fieldValueMembership struct {
	ID          string
	Occurrences []string
}
type fieldValueSnapshot struct {
	ID, ScopeIdentity string
	Groups            []fieldValueMembership
}
type fieldValueSnapshots struct {
	sync.Mutex
	entries []fieldValueSnapshot
}

func opaqueFieldValueID() (string, error) {
	var token [24]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", errors.New("field count identity could not be generated")
	}
	return "fv:" + hex.EncodeToString(token[:]), nil
}
func (s *fieldValueSnapshots) put(scope string, groups []fieldvalues.Group) (fieldValueSnapshot, error) {
	id, err := opaqueFieldValueID()
	if err != nil {
		return fieldValueSnapshot{}, err
	}
	value := fieldValueSnapshot{ID: id, ScopeIdentity: scope, Groups: []fieldValueMembership{}}
	for _, group := range groups {
		id, err := opaqueFieldValueID()
		if err != nil {
			return fieldValueSnapshot{}, err
		}
		value.Groups = append(value.Groups, fieldValueMembership{ID: id, Occurrences: slices.Clone(group.Occurrences)})
	}
	s.Lock()
	defer s.Unlock()
	s.entries = append(s.entries, value)
	if len(s.entries) > fieldValueSnapshotLimit {
		s.entries = s.entries[len(s.entries)-fieldValueSnapshotLimit:]
	}
	return value, nil
}
func (s *fieldValueSnapshots) get(id, scope string) (fieldValueSnapshot, bool) {
	s.Lock()
	defer s.Unlock()
	for _, entry := range s.entries {
		if entry.ID == id && entry.ScopeIdentity == scope {
			return entry, true
		}
	}
	return fieldValueSnapshot{}, false
}

func fieldScopeIdentity(scope FieldValueScope, selector string, reveal bool, selected grid.Selection) string {
	ids := make([]string, 0, len(selected.Records))
	for _, record := range selected.Records {
		ids = append(ids, record.ID)
	}
	raw, _ := json.Marshal(struct {
		Scope                  FieldValueScope
		Selector               string
		Reveal                 bool
		Occurrences            []string
		Undecided, Undecodable int
	}{scope, selector, reveal, ids, selected.Undecided, selected.Undecodable})
	digest := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(digest[:])
}

type fieldScopeRead struct {
	source             *bundle.Bundle
	facts              messageFacts
	names              map[string]string
	selected           grid.Selection
	identity, selector string
	at                 time.Time
}

func readFieldScope(ctx context.Context, scope FieldValueScope, selector string, reveal bool) (fieldScopeRead, error) {
	selectedField, err := hl7.ParseSelector(selector)
	if err != nil {
		return fieldScopeRead{}, errors.New("choose one exact HL7 field or component selector")
	}
	if scope.Identity == "" {
		return fieldScopeRead{}, errors.New("field count scope requires the displayed source identity")
	}
	if err := grid.ValidateQuery(scope.Query); err != nil {
		return fieldScopeRead{}, err
	}
	root, source, declined := openedCase(scope.Workspace, scope.Case, scope.Identity)
	if root == "" {
		return fieldScopeRead{}, errors.New(declined.reason)
	}
	facts := readFacts(source)
	names := sourceNames(root, scope.Case, source)
	facts.names = names
	at := time.Now().UTC()
	document, _, err := queryIndex(ctx, root, source, scope.Query, at)
	if err != nil {
		return fieldScopeRead{}, err
	}
	selected, err := grid.SelectAllQuery(ctx, document, at, scope.Query, facts)
	if err != nil {
		return fieldScopeRead{}, err
	}
	return fieldScopeRead{source: source, facts: facts, names: names, selected: selected, identity: fieldScopeIdentity(scope, selectedField.String(), reveal, selected), selector: selectedField.String(), at: at}, nil
}

// ReadFieldValues counts the complete explicit capture/filter scope, then serves
// one bounded bucket page. Only memberships are cached behind random opaque
// handles. Source values never enter private state or the snapshot cache.
func (a *App) ReadFieldValues(request FieldValuesRequest) FieldValuesResult {
	return runNamedRead(a, profiles["ReadFieldValues"], func(ctx context.Context) FieldValuesResult {
		fail := func(err error) FieldValuesResult {
			result := FieldValuesResult{}
			state := Failed
			if errors.Is(err, context.Canceled) || ctx.Err() != nil {
				state = Cancelled
			}
			result.refuse(state, err.Error())
			return result
		}
		limit := request.Limit
		if limit == 0 {
			limit = 100
		}
		if request.Offset < 0 || limit < 1 || limit > 100 {
			return fail(errors.New("field value pages contain at most100 buckets and begin at a valid offset"))
		}
		scope, err := readFieldScope(ctx, request.Scope, request.Selector, request.Reveal)
		if err != nil {
			return fail(err)
		}
		ids := make([]string, 0, len(scope.selected.Records))
		for _, record := range scope.selected.Records {
			ids = append(ids, record.ID)
		}
		counted, err := fieldvalues.Count(ctx, scope.source, ids, scope.selector, request.Reveal)
		if err != nil {
			return fail(err)
		}
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		var snapshot fieldValueSnapshot
		if request.Snapshot == "" {
			snapshot, err = a.fieldValues.put(scope.identity, counted.Groups)
			if err != nil {
				return fail(err)
			}
		} else {
			var ok bool
			snapshot, ok = a.fieldValues.get(request.Snapshot, scope.identity)
			if !ok {
				return fail(errors.New("field count scope changed or expired; recount before paging"))
			}
			if len(snapshot.Groups) != len(counted.Groups) {
				return fail(errors.New("field count membership changed; recount this scope"))
			}
			for i, group := range counted.Groups {
				if !slices.Equal(group.Occurrences, snapshot.Groups[i].Occurrences) {
					return fail(errors.New("field count membership changed; recount this scope"))
				}
			}
		}
		if request.Offset > len(counted.Groups) {
			return fail(errors.New("field value page is outside its buckets"))
		}
		result := FieldValuesResult{State: Completed, Identity: scope.source.Identity, ScopeIdentity: scope.identity, Snapshot: snapshot.ID, Selector: scope.selector, Unit: "message occurrences; one value at the exact selector per occurrence", Total: scope.selected.Total, Matched: len(ids), Scanned: counted.Scanned, Complete: counted.Complete && scope.selected.Undecided == 0 && (len(request.Scope.Query.IndexedFields()) == 0 || scope.selected.Undecodable == 0), ScanComplete: counted.Scanned == len(ids), Revealed: request.Reveal, Counts: counted.Counts, Rows: []FieldValueBucket{}, GroupCount: len(counted.Groups), Offset: request.Offset, Limit: limit, ScopeUndecided: scope.selected.Undecided, ScopeUndecodable: scope.selected.Undecodable}
		for i := request.Offset; i < min(len(counted.Groups), request.Offset+limit); i++ {
			group := counted.Groups[i]
			result.Rows = append(result.Rows, FieldValueBucket{ID: snapshot.Groups[i].ID, State: group.State, Value: group.Value, Hidden: group.Hidden, Messages: len(group.Occurrences)})
		}
		return result
	})
}

// ReadFieldValueOccurrences re-verifies source and current complete query
// membership before resolving a bucket into its exact bounded occurrence page.
func (a *App) ReadFieldValueOccurrences(request FieldValueOccurrencesRequest) FieldValueOccurrencesResult {
	return runRead(a, false, func(ctx context.Context) FieldValueOccurrencesResult {
		fail := func(reason string) FieldValueOccurrencesResult {
			result := FieldValueOccurrencesResult{}
			result.refuse(Failed, reason)
			return result
		}
		limit := request.Limit
		if limit == 0 {
			limit = grid.MaxRows
		}
		if request.Offset < 0 || limit < 1 || limit > grid.MaxRows {
			return fail("message pages are bounded to200 occurrences")
		}
		scope, err := readFieldScope(ctx, request.Scope, request.Selector, request.Reveal)
		if err != nil {
			return fail(err.Error())
		}
		snapshot, ok := a.fieldValues.get(request.Snapshot, scope.identity)
		if !ok {
			return fail("field count scope changed or expired; recount before opening messages")
		}
		for _, group := range snapshot.Groups {
			if group.ID == request.Bucket {
				if request.Offset > len(group.Occurrences) {
					return fail("message page is outside this bucket")
				}
				ids := group.Occurrences[request.Offset:min(len(group.Occurrences), request.Offset+limit)]
				rows := []MessageRow{}
				if len(ids) > 0 {
					read := referencedMessages(ctx, scope.source, scope.facts, scope.names, ids, scope.at)
					if read.State != Completed && read.State != Empty {
						return fail(read.Reason)
					}
					rows = read.Rows
				}
				return FieldValueOccurrencesResult{State: Completed, Identity: scope.source.Identity, ScopeIdentity: scope.identity, Snapshot: snapshot.ID, Bucket: group.ID, Rows: rows, Total: len(group.Occurrences), Offset: request.Offset, Limit: limit}
			}
		}
		return fail("this bucket does not belong to the counted scope")
	})
}
