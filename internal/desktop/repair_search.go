package desktop

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"slices"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/index"
	"github.com/bharm16/readmit/internal/operation"
)

// RepairSearchRequest names the case whose search data is repaired. The
// index is the case's own, which the application finds: a request never
// names a file, so repair cannot be pointed at an unrelated index.
type RepairSearchRequest struct {
	Context RequestContext `json:"context"`
	Case    ItemRef        `json:"case"`
}

// RepairSearch rebuilds the derived search data of one case: the index that
// names the case's exact evidence, built again from the verified case under
// exactly the fields, retention form and retention end it already declared,
// so repair grants no new retention of patient data. The rebuilt index
// replaces the old one only once it is written whole, so a repair that stops
// or fails leaves the index it found.
//
// An index whose declared retention has ended is not rebuilt: that retention
// is over, and rebuilding would extend it. An index that cannot be read at
// all is not rebuilt either, because the fields and retention it was built
// under cannot be known from it; building one is the ordinary index path,
// under choices a person makes.
func (a *App) RepairSearch(request RepairSearchRequest) BuildIndexResult {
	return run(a, true, true, func(ctx context.Context) BuildIndexResult {
		return a.repairSearch(ctx, request)
	})
}

func (a *App) repairSearch(ctx context.Context, request RepairSearchRequest) BuildIndexResult {
	if request.Case.Kind != CaseItem {
		return BuildIndexResult{State: Failed, Reason: "search is repaired for one case"}
	}
	loaded, _, records, declined := a.scoped(ctx, request.Context, []ItemRef{request.Case})
	if loaded == nil {
		return declined.buildIndex()
	}
	entry := records[0].Entry
	casePath, err := artifactpath.Child(loaded.root, entry)
	if err != nil {
		return BuildIndexResult{State: Failed, Reason: "the case is not one folder of the project"}
	}
	opened, err := operation.OpenCase(casePath)
	if err != nil {
		return BuildIndexResult{State: Failed, Reason: err.Error()}
	}
	found := a.describeIndex(loaded.root, entry, "")
	if found.Index == nil {
		// An index that cannot be read names no case, so only the name the
		// case's index is built under tells it is this case's.
		if standard := describeSpecificIndex(loaded.root, entry+".index.json", opened, a.now().UTC()); standard.Index != nil {
			found = standard
		}
	}
	details := found.Index
	switch {
	case found.State == Empty || details == nil:
		return BuildIndexResult{State: Empty, Reason: "this case has no search data to repair"}
	case details.Damaged || details.Unsupported:
		return BuildIndexResult{State: Failed, Index: details,
			Reason: "this case's index cannot be read, so the fields and retention it was built under are not known; nothing was rebuilt"}
	case details.Identity != opened.Identity:
		return BuildIndexResult{State: Failed, Reason: "no index of this case's exact evidence was found; nothing was rebuilt"}
	case details.Expired:
		return BuildIndexResult{State: Failed, Index: details,
			Reason: "the retention declared for this case's index has ended; it is not rebuilt, because rebuilding would extend it"}
	}
	policy := index.Policy{Fields: slices.Clone(details.Fields), Retention: details.Retention}
	if details.RetainUntil != nil {
		until := *details.RetainUntil
		policy.RetainUntil = &until
	}
	at := a.now().UTC()
	document, err := index.Build(ctx, opened, policy, at)
	if ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return BuildIndexResult{State: Cancelled, Reason: cancelledRefusal.reason}
	}
	if err != nil {
		return BuildIndexResult{State: Failed, Reason: err.Error()}
	}
	destination := artifactpath.JoinReference(loaded.root, details.IndexName)
	raw := make([]byte, 8)
	rand.Read(raw)
	rebuilt, err := index.Write(artifactpath.JoinReference(loaded.root, ".readmit-repair-"+hex.EncodeToString(raw)), document)
	if err != nil {
		return probeWriteFailure(loaded.root,
			"this account cannot write into the project folder",
			"the rebuilt index could not be written; the index it found is unchanged").buildIndex()
	}
	// The index being replaced must still be this case's own at the moment
	// it is replaced; one changed in the meantime is left as it is.
	if existing, err := index.Open(destination); err == nil && existing.Case.Identity != opened.Identity {
		os.Remove(rebuilt)
		return BuildIndexResult{State: Failed, Reason: "the index changed while it was rebuilt; it is left as it is"}
	}
	if err := os.Rename(rebuilt, destination); err != nil {
		os.Remove(rebuilt)
		return BuildIndexResult{State: Failed, Reason: "the rebuilt index could not replace the one it repairs; that one is unchanged"}
	}
	repaired := indexDetails(details.IndexName, document, opened, at)
	return BuildIndexResult{State: Completed, Index: &repaired}
}
