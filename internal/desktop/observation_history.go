package desktop

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/observewindow"
	"github.com/bharm16/readmit/internal/operation"
)

// An observation's history is the collections the project retained for its
// source, read and never repeated. A collection that did not complete —
// stopped at its deadline, truncated, stale, missing its source — observed
// nothing, so it is never reported as having found zero records.

// CollectionRow is one retained collection of an observation. Records is
// the settled count of a completed collection and null for every other
// status, which settled on none. Reason is why a collection that did not
// complete observed nothing.
type CollectionRow struct {
	Entry       string  `json:"entry"`
	ClosedAt    *string `json:"closed_at"`
	Status      string  `json:"status"`
	Trustworthy bool    `json:"trustworthy"`
	Records     *int    `json:"records"`
	Reason      string  `json:"reason,omitzero"`
}

// collectionRow is the row one retained completion reads as.
func collectionRow(entry string, completion observewindow.Completion) CollectionRow {
	row := CollectionRow{Entry: entry, ClosedAt: stampedTime(completion.ClosedAt), Status: string(completion.Status), Trustworthy: completion.Trustworthy()}
	if completion.Trustworthy() {
		records := completion.RecordsObserved
		row.Records = &records
	}
	if err := completion.Err(); err != nil {
		row.Reason = err.Error()
	}
	return row
}

// ObservationHistoryResult lists an observation's retained collections, the
// most recently closed first.
type ObservationHistoryResult struct {
	State       State           `json:"state"`
	Reason      string          `json:"reason,omitzero"`
	Context     RequestContext  `json:"context"`
	Collections []CollectionRow `json:"collections"`
}

func (r *ObservationHistoryResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// observationSource reads the source an available observation declares.
func (a *App) observationSource(ctx context.Context, request ItemRequest) (*loadedCatalog, *ObservationDraft, map[string]string, refusal) {
	if request.Ref.Kind != ObservationItem {
		return nil, nil, nil, refusal{Failed, "only an observation has collections"}
	}
	loaded, item, refused := a.catalogItem(ctx, request.Context, request.Ref, false)
	if loaded == nil {
		return nil, nil, nil, refusal{refused.State, refused.Reason}
	}
	if item.Availability != ItemAvailable {
		return nil, nil, nil, refusal{Failed, "this observation is " + string(item.Availability) + ": " + item.Reason}
	}
	record := loaded.document.Items[loaded.document.Find(item.Ref.ID)]
	draft, err := loaded.observationOf(record)
	if err != nil {
		return nil, nil, nil, refusal{Failed, err.Error()}
	}
	paths, _, _ := loaded.backing(record)
	return loaded, draft, paths, refusal{}
}

// ObservationHistory lists the collections the project retained for one
// observation's source. It is a read: nothing is collected.
func (a *App) ObservationHistory(request ItemRequest) ObservationHistoryResult {
	return run(a, false, false, func(ctx context.Context) ObservationHistoryResult {
		result := ObservationHistoryResult{Context: request.Context, Collections: []CollectionRow{}}
		loaded, draft, _, declined := a.observationSource(ctx, request)
		if loaded == nil {
			result.refuse(declined.state, declined.reason)
			return result
		}
		entries, err := os.ReadDir(loaded.root)
		if err != nil {
			result.refuse(Failed, "the project folder cannot be read")
			return result
		}
		for _, entry := range entries {
			path := filepath.Join(loaded.root, entry.Name())
			if !entry.Type().IsRegular() || !declares(path, observewindow.CompletionSchema) {
				continue
			}
			completion, err := observewindow.ReadCompletion(path)
			if err != nil || completion.Source != draft.Source.Observes {
				continue
			}
			result.Collections = append(result.Collections, collectionRow(entry.Name(), completion))
		}
		slices.SortStableFunc(result.Collections, func(x, y CollectionRow) int {
			return cmp.Or(cmp.Compare(stampOf(y.ClosedAt), stampOf(x.ClosedAt)), cmp.Compare(x.Entry, y.Entry))
		})
		result.State = Completed
		if len(result.Collections) == 0 {
			result.State = Empty
		}
		return result
	})
}

func stampOf(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// CompletionRequest names one retained collection of an observation.
type CompletionRequest struct {
	Context RequestContext `json:"context"`
	Ref     ItemRef        `json:"ref"`
	Entry   string         `json:"entry"`
}

// CompletionInspection is one retained collection read again: its row,
// when it opened, the samples it took and the run of identical observations
// it ended on, and whether the observation's current window bears it out.
// Supported is false, with its reason, for a collection made under a window
// since changed or one its own samples do not bear out.
type CompletionInspection struct {
	CollectionRow
	OpenedAt      *string `json:"opened_at"`
	Samples       int     `json:"samples"`
	StableSamples int     `json:"stable_samples"`
	QuietPeriod   string  `json:"quiet_period"`
	Supported     bool    `json:"supported"`
	Unsupported   string  `json:"unsupported,omitzero"`
}

// CompletionInspectionResult answers one inspection.
type CompletionInspectionResult struct {
	State      State                 `json:"state"`
	Reason     string                `json:"reason,omitzero"`
	Context    RequestContext        `json:"context"`
	Completion *CompletionInspection `json:"completion,omitzero"`
}

func (r *CompletionInspectionResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// InspectCompletion reads one retained collection of an observation again,
// against the observation's current window. It never collects.
func (a *App) InspectCompletion(request CompletionRequest) CompletionInspectionResult {
	return run(a, false, false, func(ctx context.Context) CompletionInspectionResult {
		result := CompletionInspectionResult{Context: request.Context}
		loaded, draft, paths, declined := a.observationSource(ctx, ItemRequest{Context: request.Context, Ref: request.Ref})
		if loaded == nil {
			result.refuse(declined.state, declined.reason)
			return result
		}
		path, err := artifactpath.File(loaded.root, request.Entry)
		if err != nil {
			result.refuse(Failed, "a collection is one entry of the project")
			return result
		}
		completion, err := observewindow.ReadCompletion(path)
		if err != nil {
			result.refuse(Failed, err.Error())
			return result
		}
		if completion.Source != draft.Source.Observes {
			result.refuse(Failed, "that entry is not a collection of this observation")
			return result
		}
		inspection := &CompletionInspection{CollectionRow: collectionRow(request.Entry, completion), OpenedAt: stampedTime(completion.OpenedAt),
			Samples: len(completion.Samples), StableSamples: completion.StableSamples, QuietPeriod: completion.QuietPeriod, Supported: true}
		if window, held := paths["window"]; held {
			if _, err := operation.ExplainObservation(path, window); errors.Is(err, operation.ErrCompletionUnsupported) {
				inspection.Supported, inspection.Unsupported = false, err.Error()
			} else if err != nil {
				result.refuse(Failed, err.Error())
				return result
			}
		}
		result.State, result.Completion = Completed, inspection
		return result
	})
}

// completionOutput is where one reviewed collection retains its completion
// and its snapshot: a fresh pair of project entries the application names.
func completionOutput(root string) (string, string, refusal) {
	for i := 1; i <= 999; i++ {
		completion, snapshot := fmt.Sprintf("observation-completion-%03d.json", i), fmt.Sprintf("observation-snapshot-%03d", i)
		free := true
		for _, entry := range []string{completion, snapshot} {
			if _, err := os.Lstat(filepath.Join(root, entry)); err == nil {
				free = false
			} else if !os.IsNotExist(err) {
				return "", "", probeReadFailure(root)
			}
		}
		if free {
			return completion, snapshot, refusal{}
		}
	}
	return "", "", refusal{Failed, "the project holds more collections than this release numbers"}
}

// memberDigest is the digest a revision recorded for one of its members, or
// the empty string for an object with no saved revision.
func memberDigest(item catalog.Item, role string) string {
	if current := item.Current(); current != nil {
		for _, member := range current.Members {
			if member.Role == role {
				return member.SHA256
			}
		}
	}
	return ""
}
