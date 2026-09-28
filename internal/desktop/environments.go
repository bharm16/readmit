package desktop

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/fixturereset"
	"github.com/bharm16/readmit/internal/observation"
	"github.com/bharm16/readmit/internal/operation"
	"github.com/bharm16/readmit/internal/sendpolicy"
	"github.com/bharm16/readmit/internal/suite"
	"github.com/bharm16/readmit/internal/testrunner"
)

// An environment is checked, its destinations evaluated and it is removed
// as the named object it is: every operation reads the revision the catalog
// names, never a file a window picked.

// EnvironmentCheckSchema is the contract of the latest explicit check the
// application retains for one environment.
const EnvironmentCheckSchema = "readmit-environment-check/v1"

// checksFolder is where the latest check of each environment is retained,
// inside the project's application area.
const checksFolder = "checks"

// maxCheckBytes bounds one retained check.
const maxCheckBytes = 64 << 10

// EnvironmentCheck is the latest explicit check of one environment: the
// revision it checked, when, and what it found. It is replaced by the next
// check and never read as a live connection.
type EnvironmentCheck struct {
	Schema    string               `json:"schema"`
	Item      string               `json:"item"`
	Revision  string               `json:"revision"`
	CheckedAt string               `json:"checked_at"`
	Outcome   string               `json:"outcome"`
	Reason    string               `json:"reason,omitzero"`
	Report    *EnvironmentReport   `json:"report"`
	Decision  *sendpolicy.Decision `json:"decision"`
}

var checkFile = artifactdir.Document{MaxBytes: maxCheckBytes}

func checkPath(root, item string) string {
	return filepath.Join(root, catalog.Folder, checksFolder, item+".json")
}

// readCheck reads the latest check retained for one environment.
func readCheck(root, item string) (EnvironmentCheck, error) {
	var check EnvironmentCheck
	if !catalog.ValidID(item) {
		return check, errors.New("not an application identity")
	}
	data, err := checkFile.Read(checkPath(root, item))
	if err != nil {
		return check, err
	}
	if err := json.Unmarshal(data, &check, json.RejectUnknownMembers(true)); err != nil || check.Schema != EnvironmentCheckSchema || check.Item != item {
		return EnvironmentCheck{}, errors.New("the environment's last check cannot be read")
	}
	return check, nil
}

// retainCheck replaces the latest check retained for one environment.
func retainCheck(root string, check EnvironmentCheck) error {
	data, err := encodeMember(check)
	if err != nil {
		return err
	}
	folder := filepath.Join(root, catalog.Folder, checksFolder)
	if err := os.MkdirAll(folder, 0o700); err != nil {
		return err
	}
	return checkFile.Replace(checkPath(root, check.Item), data)
}

// EnvironmentCheckResult answers one explicit check of a saved environment:
// the transport outcome and TLS details, the send decision a send there
// would be decided under, and when it was checked. A check that reached
// nothing still says why. Ref names the revision checked.
type EnvironmentCheckResult struct {
	State     State                `json:"state"`
	Reason    string               `json:"reason,omitzero"`
	Context   RequestContext       `json:"context"`
	Ref       *ItemRef             `json:"ref,omitzero"`
	Report    *EnvironmentReport   `json:"report,omitzero"`
	Decision  *sendpolicy.Decision `json:"decision,omitzero"`
	CheckedAt string               `json:"checked_at,omitzero"`
}

func (r *EnvironmentCheckResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// savedEnvironment reads one environment at the revision a request names,
// which must be its current one.
func (a *App) savedEnvironment(ctx context.Context, request ItemRequest) (*loadedCatalog, *CatalogItem, *environmentMembers, refusal) {
	if request.Ref.Kind != EnvironmentItem {
		return nil, nil, nil, refusal{Failed, "only an environment is checked"}
	}
	loaded, item, refused := a.catalogItem(ctx, request.Context, request.Ref, false)
	if loaded == nil {
		return nil, nil, nil, refusal{refused.State, refused.Reason}
	}
	if item.Availability != ItemAvailable {
		return nil, nil, nil, refusal{Failed, "this environment is " + string(item.Availability) + ": " + item.Reason}
	}
	if request.Ref.Revision != item.Ref.Revision {
		return nil, nil, nil, refusal{Failed, "the environment was saved again since it was shown; look at the saved version first"}
	}
	members, err := loaded.environmentOf(loaded.document.Items[loaded.document.Find(item.Ref.ID)])
	if err != nil {
		return nil, nil, nil, refusal{Failed, err.Error()}
	}
	return loaded, item, members, refusal{}
}

// CheckEnvironment connects to the saved environment at the revision shown,
// without sending a message, and decides its address under the environment's
// own send policy. It saves nothing of the environment: a person saves or
// discards an edit first. The result is retained as the environment's latest
// check, bound to the revision checked. Like `readmit target check`, it is
// admitted as execution.
func (a *App) CheckEnvironment(request ItemRequest) EnvironmentCheckResult {
	return runNamed[EnvironmentCheckResult, *EnvironmentCheckResult](a, profiles["CheckEnvironment"], func(ctx context.Context) EnvironmentCheckResult {
		result := EnvironmentCheckResult{Context: request.Context}
		loaded, item, members, declined := a.savedEnvironment(ctx, request)
		if loaded == nil {
			result.refuse(declined.state, declined.reason)
			return result
		}
		target, err := operation.ReadTarget(members.paths["target"])
		if err != nil {
			result.refuse(Failed, approvalReason(err))
			return result
		}
		a.reach(reachingTarget{ref: "environment:" + item.Ref.ID, name: item.Name, kind: ConnectionEnvironment, destination: target.Address})
		report, decision, err := operation.DiagnoseTarget(ctx, target, members.policy, sendpolicy.SystemResolver)
		checked := a.now()
		result.Report, result.Decision, result.CheckedAt = toEnvironmentReport(report), &decision, catalog.Stamp(checked)
		ref := item.Ref
		result.Ref = &ref
		record := EnvironmentCheck{Schema: EnvironmentCheckSchema, Item: item.Ref.ID, Revision: item.Ref.Revision, CheckedAt: result.CheckedAt,
			Outcome: string(report.Outcome), Report: result.Report, Decision: &decision}
		if err != nil {
			result.refuse(Failed, err.Error())
			record.Reason = err.Error()
		} else {
			result.State = Completed
		}
		if record.Outcome == "" {
			record.Outcome = "unusable"
		}
		if ctx.Err() != nil {
			result.refuse(Cancelled, "the check was stopped")
			return result
		}
		if retainErr := retainCheck(loaded.root, record); retainErr != nil && result.State == Completed {
			result.Reason = "the check completed and could not be retained as the environment's latest check"
		}
		return result
	})
}

// DestinationCheckRequest asks whether a proposed send to Address, under a
// Classification, would be allowed by an environment's saved send policy.
type DestinationCheckRequest struct {
	Context        RequestContext `json:"context"`
	Ref            ItemRef        `json:"ref"`
	Address        string         `json:"address"`
	Classification string         `json:"classification"`
}

// CheckEnvironmentDestination evaluates one proposed send against the
// environment's saved send policy, as a send would ask it. It may resolve a
// host name; it opens no connection and sends nothing. An environment with
// no policy has none to allow the send. A request that names no
// classification is decided under the one the environment records, never a
// nonproduction one assumed for it.
func (a *App) CheckEnvironmentDestination(request DestinationCheckRequest) SendPolicyEvalResult {
	return runNamed[SendPolicyEvalResult, *SendPolicyEvalResult](a, profiles["CheckEnvironmentDestination"], func(ctx context.Context) SendPolicyEvalResult {
		loaded, _, members, declined := a.savedEnvironment(ctx, ItemRequest{Context: request.Context, Ref: request.Ref})
		if loaded == nil {
			return SendPolicyEvalResult{State: declined.state, Reason: declined.reason}
		}
		classification := cmp.Or(request.Classification, string(members.target.Environment().Classification))
		decision := operation.EvaluateSendPolicy(ctx, members.policy, request.Address, classification, true, sendpolicy.SystemResolver)
		return SendPolicyEvalResult{State: Completed, Decision: &decision}
	})
}

// ReceiverSnapshot is one receiver observation of the project, the file a
// reset's empty-observation check reads, and when the receiver last wrote it.
// A readmit-observation/v1 document records no time of its own, so
// CollectedAt is the file's modification time, which each snapshot the
// receiver installs replaces; it is null when the file cannot say.
type ReceiverSnapshot struct {
	Entry       string  `json:"entry"`
	CollectedAt *string `json:"collected_at"`
}

// ReceiverSnapshotsResult lists the project's receiver observations.
type ReceiverSnapshotsResult struct {
	State     State              `json:"state"`
	Reason    string             `json:"reason,omitzero"`
	Context   RequestContext     `json:"context"`
	Snapshots []ReceiverSnapshot `json:"snapshots"`
}

func (r *ReceiverSnapshotsResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// ListReceiverSnapshots lists the entries of the open project that read as a
// receiver's readmit-observation/v1 ledger snapshot, most recently written
// first, for choosing the one a reset checks is empty. It is a read.
func (a *App) ListReceiverSnapshots(request ItemRequest) ReceiverSnapshotsResult {
	return run(a, false, false, func(ctx context.Context) ReceiverSnapshotsResult {
		result := ReceiverSnapshotsResult{Context: request.Context, Snapshots: []ReceiverSnapshot{}}
		root, declined := a.projectRoot(ctx, request.Context)
		if root == "" {
			result.refuse(declined.state, declined.reason)
			return result
		}
		entries, err := os.ReadDir(root)
		switch {
		case errors.Is(err, fs.ErrPermission):
			result.refuse(PermissionDenied, "this account cannot read the project folder")
			return result
		case err != nil:
			result.refuse(Failed, "the project folder cannot be read")
			return result
		}
		written := map[string]time.Time{}
		for _, entry := range entries {
			path := filepath.Join(root, entry.Name())
			if !entry.Type().IsRegular() || !declares(path, observation.Schema) {
				continue
			}
			data, err := boundedFile(path, observation.MaxBytes)
			if err != nil {
				continue
			}
			if _, err := observation.Decode(data); err == nil {
				snapshot := ReceiverSnapshot{Entry: entry.Name()}
				if info, err := entry.Info(); err == nil {
					written[entry.Name()] = info.ModTime()
					snapshot.CollectedAt = stampedTime(info.ModTime())
				}
				result.Snapshots = append(result.Snapshots, snapshot)
			}
		}
		slices.SortStableFunc(result.Snapshots, func(x, y ReceiverSnapshot) int {
			return cmp.Or(written[y.Entry].Compare(written[x.Entry]), cmp.Compare(x.Entry, y.Entry))
		})
		result.State = Completed
		if len(result.Snapshots) == 0 {
			result.State = Empty
		}
		return result
	})
}

// RemoveItemResult answers one removal. Referring names what still uses the
// object when the removal was refused.
type RemoveItemResult struct {
	State     State          `json:"state"`
	Reason    string         `json:"reason,omitzero"`
	Context   RequestContext `json:"context"`
	Referring []Referrer     `json:"referring"`
}

func (r *RemoveItemResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// RemoveItem removes a named environment or observation from the project:
// the catalog records that it was removed, so it is no longer listed or
// discovered again. It is refused, before anything is written, while a test
// or a suite binds any file of any of its revisions, or an environment links
// the observation; those are reassigned first. Its files stay exactly where
// they are, so every run, check and completion made with it stays readable.
func (a *App) RemoveItem(request ItemRequest) RemoveItemResult {
	return run(a, false, true, func(ctx context.Context) RemoveItemResult {
		result := RemoveItemResult{Context: request.Context, Referring: []Referrer{}}
		if request.Ref.Kind != EnvironmentItem && request.Ref.Kind != ObservationItem {
			result.refuse(Failed, "only an environment or an observation is removed here")
			return result
		}
		loaded, _, refused := a.catalogItem(ctx, request.Context, request.Ref, true)
		if loaded == nil {
			result.refuse(refused.State, refused.Reason)
			return result
		}
		record := loaded.document.Items[loaded.document.Find(request.Ref.ID)]
		if referring := loaded.referrers(record); len(referring) > 0 {
			result.Referring = referring
			result.refuse(Failed, "a test, a suite or an environment of this project still uses this object; reassign them first. Nothing was removed")
			return result
		}
		if _, err := loaded.store.Update(a.now(), func(document *catalog.Document) (bool, error) {
			at := document.Find(request.Ref.ID)
			if at < 0 {
				return false, catalog.ErrNoItem
			}
			document.Items[at].RemovedAt = catalog.Stamp(a.now())
			return true, nil
		}); err != nil {
			result.refuse(Failed, "the project's catalog could not record the removal; nothing was removed")
			return result
		}
		result.State = Completed
		return result
	})
}

// referrers are the project's objects that use one environment or
// observation: a test or a suite binding any file of any revision it was
// saved as, or the entry it was discovered at, and, for an observation, an
// environment whose current links name it or whose reset checks it is
// empty.
func (c *loadedCatalog) referrers(item catalog.Item) []Referrer {
	files := map[string]bool{}
	if item.Entry != "" {
		files[item.Entry] = true
	}
	for _, revision := range item.Revisions {
		for _, member := range revision.Members {
			files[member.Path] = true
		}
	}
	names := func(paths ...string) bool {
		for _, path := range paths {
			if path == "" {
				continue
			}
			clean := filepath.ToSlash(filepath.Clean(path))
			if filepath.IsAbs(path) {
				if relative, err := filepath.Rel(c.root, path); err == nil {
					clean = filepath.ToSlash(relative)
				}
			}
			if files[clean] {
				return true
			}
		}
		return false
	}
	referring := []Referrer{}
	for _, other := range c.document.Items {
		if other.ID == item.ID || c.removed(other) {
			continue
		}
		paths, availability, _ := c.backing(other)
		if availability != ItemAvailable {
			continue
		}
		uses := false
		switch other.Kind {
		case string(TestItem):
			if spec, err := testrunner.ReadSpec(paths[primaryRole(TestItem)]); err == nil {
				uses = names(spec.Target, spec.Observation.Path)
			}
		case string(SuiteItem):
			if data, err := boundedFile(paths[primaryRole(SuiteItem)], suite.MaxBytes); err == nil {
				if document, err := suite.Decode(data); err == nil {
					for _, environment := range document.Environments {
						for _, binding := range environment.Bindings {
							uses = uses || names(binding.Target, binding.Observation)
						}
					}
				}
			}
		case string(EnvironmentItem):
			if item.Kind == string(ObservationItem) {
				if path, held := paths["links"]; held {
					if links, err := readLinks(path); err == nil && links.Observation == item.ID {
						uses = true
					}
				}
				if path, held := paths["reset"]; held {
					if plan, err := operation.ReadResetPlan(path); err == nil {
						for _, action := range plan.Actions {
							uses = uses || action.Operator == fixturereset.CollectionEmpty && names(action.Observation)
						}
					}
				}
			}
		}
		if uses {
			referring = append(referring, Referrer{Ref: ItemRef{Kind: ItemKind(other.Kind), ID: other.ID, Revision: other.RevisionLabel()}, Name: c.read(other).Name})
		}
	}
	slices.SortFunc(referring, func(x, y Referrer) int {
		if x.Ref.ID < y.Ref.ID {
			return -1
		}
		if x.Ref.ID > y.Ref.ID {
			return 1
		}
		return 0
	})
	return referring
}
