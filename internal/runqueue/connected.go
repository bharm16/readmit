package runqueue

import (
	"context"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/engine"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/runnerprotocol"
	"github.com/bharm16/readmit/internal/testisolation"
)

const ConnectedPlanSchema = "readmit-run-queue/v2"
const ConnectedReportSchema = "readmit-run-queue-report/v2"

// ConnectedPlan selects connected lifecycle inputs. It is never decoded by
// the legacy queue reader, whose specs and durable results are unchanged.
type ConnectedPlan struct {
	Schema      string         `json:"schema"`
	Parallelism int            `json:"parallelism"`
	Jobs        []ConnectedJob `json:"jobs"`
}

type ConnectedJob struct {
	ID           string   `json:"id"`
	Plan         string   `json:"plan"`
	PlanIdentity string   `json:"plan_identity"`
	Config       string   `json:"config"`
	Input        string   `json:"input"`
	After        []string `json:"after"`
	State        string   `json:"state"`
}

func (j *ConnectedJob) UnmarshalJSON(raw []byte) error {
	type plain ConnectedJob
	if runnerprotocol.Exact(raw, "id", "plan", "plan_identity", "config", "input", "after", "state") != nil {
		return errors.New("invalid connected queued job")
	}
	return json.Unmarshal(raw, (*plain)(j), json.RejectUnknownMembers(true))
}

func DecodeConnectedPlan(raw []byte) (ConnectedPlan, error) {
	var p ConnectedPlan
	if len(raw) > MaxPlanBytes || runnerprotocol.Exact(raw, "schema", "parallelism", "jobs") != nil ||
		json.Unmarshal(raw, &p, json.RejectUnknownMembers(true)) != nil || p.Schema != ConnectedPlanSchema {
		return p, errors.New("invalid connected queue")
	}
	legacy := Plan{Schema: PlanSchema, Parallelism: p.Parallelism}
	for _, j := range p.Jobs {
		if !filepath.IsLocal(j.Plan) || !filepath.IsAbs(j.Config) || filepath.Clean(j.Config) != j.Config ||
			!networkaction.ValidDigest(j.PlanIdentity) || !networkaction.ValidDigest(j.Input) || j.After == nil {
			return p, errors.New("invalid connected queue input pin")
		}
		switch j.State {
		case "enabled", "blocked", "skipped", "quarantined", "disabled", "unsupported":
		default:
			return p, errors.New("invalid connected queue declaration")
		}
		legacy.Jobs = append(legacy.Jobs, Job{ID: j.ID, Spec: j.ID + ".json", Isolation: SharedState, After: j.After})
	}
	// One rule decides queue ordering, cycle, identifier and concurrency bounds.
	if err := validate(legacy); err != nil {
		return p, err
	}
	return p, nil
}

type ConnectedJobReport struct {
	ID             string                   `json:"id"`
	Admission      Admission                `json:"admission"`
	Reason         string                   `json:"reason"`
	Resources      []durablerun.Resource    `json:"resources"`
	Flow           *connectedrun.FlowResult `json:"flow,omitzero"`
	ExecutionError bool                     `json:"execution_error,omitzero"`
}

type ConnectedReport struct {
	Schema      string               `json:"schema"`
	Parallelism int                  `json:"parallelism"`
	Jobs        []ConnectedJobReport `json:"jobs"`
	Executed    int                  `json:"executed"`
	StartFailed int                  `json:"start_failed"`
	Refused     int                  `json:"refused"`
	Skipped     int                  `json:"skipped"`
}

func (r ConnectedReport) ExitCode() int {
	if len(r.Jobs) == 0 {
		return 2
	}
	code := 0
	for _, job := range r.Jobs {
		if job.ExecutionError || job.Admission != Executed || job.Flow == nil || job.Flow.State != "complete" || job.Flow.Verdict == assertion.VerdictUndecided {
			return 2
		}
		if job.Flow.Verdict == assertion.VerdictFail {
			code = 1
		}
	}
	return code
}

// ConnectedExecution is the adapter onto a lifecycle. A customer runner wraps
// it with its live admitted authority; the scheduler never evaluates checks.
type ConnectedExecution func(context.Context, *connectedrun.PreparedFlow, string) (connectedrun.FlowResult, error)

type ConnectedRequest struct {
	PlanBytes     []byte
	PlanDirectory string
	Runs          string
	Instance      string
	Execute       ConnectedExecution
}

// ConnectedInstance binds one child to one occurrence and declared job. It is
// independent of a business identifier and cannot be borrowed by another slot.
func ConnectedInstance(occurrence, job string) string {
	return "j-" + networkaction.Digest([]byte(occurrence + "/" + job))[:40]
}

// RunConnected prepares every input and then uses the existing schedule's
// resource, dependency and cancellation decisions. Its results are actual
// connected results, never synthetic legacy durable-job summaries.
func RunConnected(ctx context.Context, request ConnectedRequest) (ConnectedReport, error) {
	plan, err := DecodeConnectedPlan(request.PlanBytes)
	if err != nil || !runnerprotocol.ID(request.Instance) {
		return ConnectedReport{}, errors.New("invalid connected queue execution")
	}
	queue := &schedule{states: make([]*state, len(plan.Jobs)), held: map[string]string{}, runs: request.Runs, owned: map[string]bool{}}
	byID := map[string]*state{}
	for index, job := range plan.Jobs {
		if _, err := os.Lstat(filepath.Join(request.Runs, job.ID)); !os.IsNotExist(err) {
			return ConnectedReport{}, errors.New("a connected queued output already exists or cannot be read")
		}
		instance := ConnectedInstance(request.Instance, job.ID)
		prepared, err := connectedrun.PrepareFlow(filepath.Join(request.PlanDirectory, job.Plan), job.Config, instance)
		if err != nil {
			return ConnectedReport{}, err
		}
		input, err := prepared.InputIdentity()
		if err != nil || input != job.Input {
			return ConnectedReport{}, errors.New("connected queued inputs changed since preparation")
		}
		snapshot, e := prepared.InputSnapshot()
		if e != nil {
			return ConnectedReport{}, e
		}
		registry, e := prepared.RegistrySnapshot()
		if e != nil {
			return ConnectedReport{}, e
		}
		configRaw, e := (artifactdir.Document{MaxBytes: 2 << 20}).Read(job.Config)
		if e != nil {
			return ConnectedReport{}, e
		}
		resources := []durablerun.Resource{}
		for _, resource := range prepared.Resources() {
			resources = append(resources, durablerun.Resource{Kind: "connected-state", Name: resource})
		}
		current := &state{job: Job{ID: job.ID, Isolation: SharedState, After: job.After}, output: filepath.Join(request.Runs, job.ID),
			connected: prepared, executeConnected: request.Execute, planIdentity: job.PlanIdentity, instance: instance,
			report: JobReport{ID: job.ID, Isolation: SharedState, Resources: resources}}
		current.verifyConnected = func(ctx context.Context, path string) error {
			return connectedrun.VerifyFlowInput(ctx, path, snapshot, configRaw, instance, registry)
		}
		if job.State != "enabled" {
			current.stop(Skipped, "the job is declared "+job.State)
		}
		queue.states[index], byID[job.ID], queue.owned[job.ID] = current, current, true
	}
	for _, current := range queue.states {
		for _, after := range current.job.After {
			current.after = append(current.after, byID[after])
		}
	}
	queue.run(ctx, plan.Parallelism)
	report := ConnectedReport{Schema: ConnectedReportSchema, Parallelism: plan.Parallelism, Jobs: make([]ConnectedJobReport, len(queue.states))}
	for index, current := range queue.states {
		if current.report.Admission == "" {
			current.stop(Skipped, "the queue stopped before this job started")
		}
		report.Jobs[index] = ConnectedJobReport{ID: current.job.ID, Admission: current.report.Admission, Reason: current.report.Reason, Resources: current.report.Resources, Flow: current.flow, ExecutionError: current.connectedExecutionError}
		switch current.report.Admission {
		case Executed:
			report.Executed++
		case Refused:
			report.Refused++
		case StartFailed:
			report.StartFailed++
		default:
			report.Skipped++
		}
	}
	return report, nil
}

func (s *state) startConnected(ctx context.Context) {
	execute := s.executeConnected
	if execute == nil {
		execute = func(ctx context.Context, p *connectedrun.PreparedFlow, output string) (connectedrun.FlowResult, error) {
			return connectedrun.ExecuteFlow(ctx, p, output, testisolation.Confirmation{})
		}
	}
	_, err := execute(ctx, s.connected, s.output)
	s.report.Admission = Executed
	s.connectedExecutionError = err != nil
	if err != nil {
		s.report.Reason = err.Error()
	}
	// A callback's return is never execution proof. Reopen the actual sealed
	// child, or inspect its interrupted evidence, through the lifecycle owner.
	flow, openErr := connectedrun.OpenFlow(context.WithoutCancel(ctx), s.output)
	if openErr != nil {
		flow, openErr = connectedrun.InspectFlow(context.WithoutCancel(ctx), s.output)
	}
	if openErr != nil || flow.Schema == "" || flow.Plan != s.planIdentity || flow.Instance != s.instance || flow.Engine != engine.Version() || s.verifyConnected == nil || s.verifyConnected(context.WithoutCancel(ctx), s.output) != nil {
		s.report.Admission = StartFailed
		s.report.Reason = "the actual connected child evidence cannot be verified"
		return
	}
	s.flow = &flow
	s.passed = !s.connectedExecutionError && flow.State == "complete" && flow.Verdict == assertion.VerdictPass
}
