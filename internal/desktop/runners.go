package desktop

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/customerrunner"
)

// Named runners (#564). A runner is an object of the project: the
// readmit-runner/v1 configuration its host reads, saved whole under the name a
// person gave it, with the project environment it serves. Its status is what
// the window last established about it by an actual read or admission, kept
// with when; a runner nothing has checked is Not checked.

// RunnerLinksSchema is the saved runner's assignment: the project
// environment it runs for.
const RunnerLinksSchema = "readmit-runner-links/v1"

type runnerLinks struct {
	Schema      string `json:"schema"`
	Environment string `json:"environment"`
}

// RunnerDraft is a whole runner as its editor holds it: every member of the
// readmit-runner/v1 configuration, and the project environment it is
// assigned to, empty for none.
type RunnerDraft struct {
	Hub          string               `json:"hub"`
	Project      string               `json:"project"`
	Environment  string               `json:"environment"`
	Root         string               `json:"root"`
	CA           string               `json:"ca"`
	Certificate  string               `json:"certificate"`
	Key          RunnerReferenceInput `json:"key"`
	Token        RunnerReferenceInput `json:"token"`
	UpdateKey    string               `json:"update_key"`
	UpdateEngine string               `json:"update_engine"`
	Assigned     string               `json:"assigned"`
}

func validateRunnerDraft(scope draftScope, draft ItemDraft) ([]catalog.Staged, *RunnerDraft, []FieldProblem) {
	if draft.Runner == nil {
		return nil, nil, []FieldProblem{{Field: "runner", Problem: "a runner is one runner configuration"}}
	}
	d := *draft.Runner
	if d.Key.Arguments == nil {
		d.Key.Arguments = []string{}
	}
	if d.Token.Arguments == nil {
		d.Token.Arguments = []string{}
	}
	problems := []FieldProblem{}
	config, declined := runnerConfigFrom(RunnerConfigRequest{Hub: d.Hub, Project: d.Project, Environment: d.Environment, Root: d.Root, CA: d.CA,
		Certificate: d.Certificate, Key: d.Key, Token: d.Token, UpdateKey: d.UpdateKey, UpdateEngine: d.UpdateEngine})
	if declined.reason != "" {
		problems = append(problems, FieldProblem{Field: "runner", Problem: declined.reason})
	}
	if d.Assigned != "" {
		at := -1
		if scope.loaded != nil {
			at = scope.loaded.document.Find(d.Assigned)
		}
		if at < 0 || scope.loaded.document.Items[at].Kind != string(EnvironmentItem) {
			problems = append(problems, FieldProblem{Field: "assigned", Problem: "choose one of this project's environments"})
		}
	}
	if len(problems) != 0 {
		return nil, nil, problems
	}
	document := runnerDocumentResult(config)
	staged := []catalog.Staged{{Role: string(RunnerItem), File: "runner.json", Data: []byte(document.Document)}}
	if d.Assigned != "" {
		links, _ := json.Marshal(runnerLinks{Schema: RunnerLinksSchema, Environment: d.Assigned}, json.Deterministic(true))
		staged = append(staged, catalog.Staged{Role: "links", File: "links.json", Data: links})
	}
	return staged, &d, nil
}

func verifyRunner(files map[string]string) error {
	_, _, err := readRunnerMembers(files)
	return err
}

func readRunnerMembers(files map[string]string) (customerrunner.Config, string, error) {
	data, err := boundedFile(files[string(RunnerItem)], runnerConfigBytes)
	if err != nil {
		return customerrunner.Config{}, "", err
	}
	config, err := customerrunner.DecodeConfig(data)
	if err != nil {
		return config, "", errors.New("the runner configuration could not be read through its own strict reader")
	}
	path, held := files["links"]
	if !held {
		return config, "", nil
	}
	raw, err := boundedFile(path, 4096)
	var links runnerLinks
	if err != nil || json.Unmarshal(raw, &links, json.RejectUnknownMembers(true)) != nil || links.Schema != RunnerLinksSchema || !catalog.ValidID(links.Environment) {
		return config, "", errors.New("the runner's environment assignment cannot be read")
	}
	return config, links.Environment, nil
}

// runnerDraftOf opens a saved runner as the draft its editor starts from.
func (c *loadedCatalog) runnerDraftOf(item catalog.Item) (*RunnerDraft, error) {
	paths, availability, reason := c.backing(item)
	if availability != ItemAvailable {
		return nil, errors.New(reason)
	}
	config, assigned, err := readRunnerMembers(paths)
	if err != nil {
		return nil, err
	}
	return &RunnerDraft{Hub: config.Hub, Project: config.Project, Environment: config.Environment, Root: config.Root, CA: config.CA, Certificate: config.Certificate,
		Key: RunnerReferenceInput{Command: config.Key.Command, Arguments: config.Key.Arguments}, Token: RunnerReferenceInput{Command: config.Token.Command, Arguments: config.Token.Arguments},
		UpdateKey: config.UpdateKey, UpdateEngine: config.UpdateEngine, Assigned: assigned}, nil
}

// The runner statuses, in the order a list puts the ones needing attention
// first.
const (
	RunnerAttention     = "attention"
	RunnerRefused       = "refused"
	RunnerOffline       = "offline"
	RunnerSetupRequired = "setup-required"
	RunnerNotChecked    = "not-checked"
	RunnerBusy          = "busy"
	RunnerAvailable     = "available"
)

var runnerStatusOrder = []string{RunnerAttention, RunnerRefused, RunnerOffline, RunnerSetupRequired, RunnerNotChecked, RunnerBusy, RunnerAvailable}

// runnerStatusSchema is the window's record of what it last established about
// each runner configuration it read, admitted or exported.
const (
	runnerStatusSchema = "readmit-desktop-runner-status/v1"
	maxRunnerStatus    = 256
)

type runnerStatusRecord struct {
	Config string    `json:"config"`
	State  string    `json:"state"`
	At     time.Time `json:"at"`
	Reason string    `json:"reason,omitzero"`
}

type runnerStatusDocument struct {
	Schema  string               `json:"schema"`
	Runners []runnerStatusRecord `json:"runners"`
}

func (a *App) runnerStatuses() map[string]runnerStatusRecord {
	out := map[string]runnerStatusRecord{}
	data, err := a.documents.read(runnerStatusName, 128<<10)
	var document runnerStatusDocument
	if err != nil || json.Unmarshal(data, &document, json.RejectUnknownMembers(true)) != nil || document.Schema != runnerStatusSchema {
		return out
	}
	for _, record := range document.Runners {
		if slices.Contains(runnerStatusOrder, record.State) && !record.At.IsZero() {
			out[record.Config] = record
		}
	}
	return out
}

// recordRunnerStatus keeps what an admission, a read or an export established
// about the configuration at path, with when. Failing to keep it changes
// nothing the operation did.
func (a *App) recordRunnerStatus(path, state, reason string) {
	a.runnerMu.Lock()
	defer a.runnerMu.Unlock()
	held := a.runnerStatuses()
	held[path] = runnerStatusRecord{Config: path, State: state, At: a.now().UTC(), Reason: reason}
	records := make([]runnerStatusRecord, 0, len(held))
	for _, record := range held {
		records = append(records, record)
	}
	slices.SortFunc(records, func(x, y runnerStatusRecord) int { return y.At.Compare(x.At) })
	if len(records) > maxRunnerStatus {
		records = records[:maxRunnerStatus]
	}
	data, err := json.Marshal(runnerStatusDocument{Schema: runnerStatusSchema, Runners: records}, json.Deterministic(true))
	if err == nil {
		_ = a.documents.write(runnerStatusName, data)
	}
}

// RunnerRow is one named runner of the project as the window last knew it.
type RunnerRow struct {
	Ref         ItemRef `json:"ref"`
	Name        string  `json:"name"`
	Config      string  `json:"config"`
	Environment string  `json:"environment"`
	// HubEnvironment is the environment the hub admits this runner for.
	HubEnvironment string `json:"hub_environment"`
	Assigned       string `json:"assigned,omitzero"`
	Hub            string `json:"hub"`
	Project        string `json:"project"`
	Root           string `json:"root"`
	Status         string `json:"status"`
	Reason         string `json:"reason,omitzero"`
	LastSeen       string `json:"last_seen,omitzero"`
	ActiveJobs     int    `json:"active_jobs"`
	Local          bool   `json:"local"`
}

// RunnerListResult is the project's named runners, needing attention first.
type RunnerListResult struct {
	State   State          `json:"state"`
	Reason  string         `json:"reason,omitzero"`
	Context RequestContext `json:"context"`
	Runners []RunnerRow    `json:"runners"`
}

func (r *RunnerListResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ListRunners lists the project's named runners with the status each one's
// actual state gives: a runner whose folder is on this machine is read now,
// and any other shows what its last admission or export established, dated.
func (a *App) ListRunners(request RequestContext) RunnerListResult {
	return runRead(a, false, func(ctx context.Context) RunnerListResult {
		result := RunnerListResult{Context: request, Runners: []RunnerRow{}}
		loaded, declined := a.loadCatalog(ctx, request, false)
		if loaded == nil {
			result.refuse(declined.state, declined.reason)
			return result
		}
		statuses := a.runnerStatuses()
		for _, item := range loaded.document.Items {
			if item.Kind != string(RunnerItem) || loaded.removed(item) {
				continue
			}
			paths, availability, reason := loaded.backing(item)
			row := RunnerRow{Ref: ItemRef{Kind: RunnerItem, ID: item.ID, Revision: item.RevisionLabel()}, Name: cmp.Or(item.Name, "Runner"), Status: RunnerNotChecked}
			if availability != ItemAvailable {
				row.Status, row.Reason = RunnerAttention, reason
				result.Runners = append(result.Runners, row)
				continue
			}
			config, assigned, err := readRunnerMembers(paths)
			if err != nil {
				row.Status, row.Reason = RunnerAttention, err.Error()
				result.Runners = append(result.Runners, row)
				continue
			}
			row.Config, row.Hub, row.Project, row.Root, row.Assigned = paths[string(RunnerItem)], hostOf(config.Hub), config.Project, config.Root, assigned
			row.Environment, row.HubEnvironment = config.Environment, config.Environment
			if at := loaded.document.Find(assigned); assigned != "" && at >= 0 {
				row.Environment = loaded.document.Items[at].Name
			}
			if record, held := statuses[row.Config]; held {
				row.Status, row.Reason, row.LastSeen = record.State, record.Reason, catalog.Stamp(record.At)
			}
			if _, err := os.Lstat(config.Root); err == nil {
				row.Local = true
				if health, err := customerrunner.Health(config.Root); err == nil {
					for _, job := range retainedRunnerJobs(config.Root) {
						if job.State == "" || job.DeliveryUncertain || job.JournalIncomplete {
							row.ActiveJobs++
						}
					}
					switch health.State {
					case "recovery_required":
						row.Status, row.Reason = RunnerAttention, "a retained job needs recovery"
					case "lease_current":
						row.Status = RunnerBusy
					}
				} else if !errors.Is(err, fs.ErrNotExist) {
					row.Status, row.Reason = RunnerAttention, "the runner folder cannot be read"
				}
			}
			result.Runners = append(result.Runners, row)
		}
		slices.SortStableFunc(result.Runners, func(x, y RunnerRow) int {
			return cmp.Or(cmp.Compare(slices.Index(runnerStatusOrder, x.Status), slices.Index(runnerStatusOrder, y.Status)),
				strings.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name)), strings.Compare(x.Ref.ID, y.Ref.ID))
		})
		result.State = Completed
		return result
	})
}

// ChooseRunnerPath presents the host's dialog for one path the runner, CI and
// gate tasks name: the runner's working folder ("working-folder"), a CA or
// client certificate ("ca-certificate", "client-certificate"), a credential
// reader program ("locator-program"), a staged update's manifest or program
// ("update-manifest", "update-program"), a job or the test it runs ("job",
// "spec"), a runner or schedule policy ("runner-policy", "schedule-policy"),
// a gate policy ("gate-policy"), or a retained CI results or gate snapshot
// folder ("ci-results", "gate-snapshot").
// Choosing reads and writes nothing.
func (a *App) ChooseRunnerPath(kind string) PathChoiceResult {
	return run(a, true, false, func(ctx context.Context) PathChoiceResult {
		folders := map[string]string{"working-folder": "Choose the working folder", "ci-results": "Import CI results", "gate-snapshot": "Choose the gate results"}
		if title, held := folders[kind]; held {
			folder, declined := a.chooseFolder(ctx, title)
			if folder == "" {
				return PathChoiceResult{State: declined.state, Reason: declined.reason}
			}
			return PathChoiceResult{State: Completed, Kind: kind, Paths: []string{folder}}
		}
		title, filter, pattern := "", "All files (*.*)", "*.*"
		switch kind {
		case caCertificateFile:
			title, filter, pattern = "Choose a CA certificate", "PEM certificates (*.pem *.crt)", "*.pem;*.crt"
		case clientCertificateFile:
			title, filter, pattern = "Choose a client certificate", "PEM certificates (*.pem *.crt)", "*.pem;*.crt"
		case locatorProgramFile:
			title = "Choose the credential reader"
		case "update-manifest":
			title, filter, pattern = "Choose the update manifest", "JSON manifests (*.json)", "*.json"
		case "update-program":
			title = "Choose the staged program"
		case "job":
			title, filter, pattern = "Choose the job", "JSON jobs (*.json)", "*.json"
		case "spec":
			title, filter, pattern = "Choose the test to run", "JSON specs (*.json)", "*.json"
		case "schedule-policy":
			title, filter, pattern = "Open policy", "JSON policies (*.json)", "*.json"
		case "runner-policy":
			title, filter, pattern = "Choose the runner policy", "JSON policies (*.json)", "*.json"
		case "gate-policy":
			title, filter, pattern = "Choose the gate policy", "JSON policies (*.json)", "*.json"
		default:
			return PathChoiceResult{State: Failed, Reason: "unknown runner path kind"}
		}
		files, declined := a.chooseFiles(ctx, title, filter, pattern)
		if len(files) == 0 {
			return PathChoiceResult{State: declined.state, Reason: declined.reason}
		}
		return PathChoiceResult{State: Completed, Kind: kind, Paths: files[:1]}
	})
}
