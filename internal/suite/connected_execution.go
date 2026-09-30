package suite

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/expectation"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/runnerprotocol"
	"github.com/bharm16/readmit/internal/runqueue"
)

const ConnectedExecutionSchema = "readmit-suite-execution/v2"

type ConnectedExecution struct {
	Identity    string
	Preparation connectedPreparation
	Document    ConnectedDocument
	Queue       runqueue.ConnectedPlan
	Report      runqueue.ConnectedReport
}

type connectedExecutionManifest struct {
	Schema   string                   `json:"schema"`
	Input    string                   `json:"input"`
	Instance string                   `json:"instance"`
	Report   runqueue.ConnectedReport `json:"report"`
}

var connectedExecutionFamily = artifactdir.Family{
	Layout: artifactdir.Layout{Noun: "connected suite execution", RequiredFiles: []string{"manifest.json", "identity.sha256"},
		Nested: []string{"prepared", "runs"}, AllowEmpty: func(n string, _ map[string][]byte) bool { return n == "runs" || strings.HasPrefix(n, "runs/") },
		MaxFiles: 400000, MaxFileBytes: 64 << 20, MaxBytes: 2 << 30,
		AllowFile: func(n string) bool { return n == "manifest.json" || n == "identity.sha256" }},
	Seal: artifactdir.DirectoryHash(ConnectedExecutionSchema),
}

// RunConnected is the suite service over the existing queue/lifecycle. Every
// new output is an independent occurrence; opening it never retries it.
func RunConnected(ctx context.Context, request ConnectedRequest) (runqueue.ConnectedReport, error) {
	if err := validateConnectedRequest(request); err != nil || !runnerprotocol.ID(request.Instance) {
		return runqueue.ConnectedReport{}, errors.New("connected suite execution requires exact inputs and a new occurrence")
	}
	// Preparation is private and passive. The executable output is reserved
	// separately only after the entire expansion is established.
	parent, err := os.MkdirTemp("", "readmit-connected-suite-")
	if err != nil {
		return runqueue.ConnectedReport{}, err
	}
	defer os.RemoveAll(parent)
	preview := request
	preview.Output = filepath.Join(parent, "prepared")
	prepared, err := PrepareConnected(preview)
	if err != nil {
		return runqueue.ConnectedReport{}, err
	}
	return ExecuteConnected(ctx, prepared, request.Instance, request.Output, request.Execute)
}

// ExecuteConnected consumes an immutable preparation. Customer runners call
// this after real enrollment and finite installed authority have been checked.
// The callback is an authority adapter; the queue verifies actual child proof.
func ExecuteConnected(ctx context.Context, prepared *ConnectedPrepared, instance, output string, execute runqueue.ConnectedExecution) (runqueue.ConnectedReport, error) {
	if prepared == nil || !runnerprotocol.ID(instance) {
		return runqueue.ConnectedReport{}, errors.New("invalid connected suite execution")
	}
	files, preparation, document, queue, err := openConnectedPreparation(prepared.Directory)
	if err != nil || preparation.Input != prepared.Identity {
		return runqueue.ConnectedReport{}, errors.New("connected suite preparation changed")
	}
	if err := prepared.CheckCapabilities(ctx); err != nil {
		// Capability failures retain a complete denominator without arming any
		// target or dropping requested checks from the declared suite.
		return retainConnectedRefusal(output, files, preparation, queue, instance, err)
	}
	w, err := artifactdir.Create(output, connectedExecutionFamily, artifactdir.Durable)
	if err != nil {
		return runqueue.ConnectedReport{}, err
	}
	defer w.Close()
	if err = copyConnectedPreparation(w, files); err != nil {
		return runqueue.ConnectedReport{}, err
	}
	if err = w.Mkdir("runs"); err != nil {
		return runqueue.ConnectedReport{}, err
	}
	raw, err := json.Marshal(queue, json.Deterministic(true))
	if err != nil {
		return runqueue.ConnectedReport{}, err
	}
	report, runErr := runqueue.RunConnected(ctx, runqueue.ConnectedRequest{PlanBytes: raw, PlanDirectory: filepath.Join(w.Path(), "prepared"), Runs: filepath.Join(w.Path(), "runs"), Instance: instance, Execute: execute})
	if runErr != nil {
		return report, runErr
	}
	_ = document
	if err = writeConnectedExecution(w, preparation.Input, instance, report); err != nil {
		return report, err
	}
	opened, err := OpenConnectedExecution(context.WithoutCancel(ctx), w.Path())
	if err != nil {
		return report, err
	}
	return opened.Report, nil
}

func retainConnectedRefusal(output string, files map[string][]byte, preparation connectedPreparation, queue runqueue.ConnectedPlan, instance string, cause error) (runqueue.ConnectedReport, error) {
	report := runqueue.ConnectedReport{Schema: runqueue.ConnectedReportSchema, Parallelism: queue.Parallelism, Jobs: []runqueue.ConnectedJobReport{}, Refused: len(queue.Jobs)}
	for _, job := range queue.Jobs {
		report.Jobs = append(report.Jobs, runqueue.ConnectedJobReport{ID: job.ID, Admission: runqueue.Refused, Reason: cause.Error(), Resources: []durablerun.Resource{}})
	}
	w, err := artifactdir.Create(output, connectedExecutionFamily, artifactdir.Durable)
	if err != nil {
		return report, err
	}
	defer w.Close()
	if err = copyConnectedPreparation(w, files); err != nil {
		return report, err
	}
	if err = w.Mkdir("runs"); err != nil {
		return report, err
	}
	return report, writeConnectedExecution(w, preparation.Input, instance, report)
}

func copyConnectedPreparation(w *artifactdir.Writer, files map[string][]byte) error {
	names := []string{}
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if err := w.WriteFile("prepared/"+name, files[name]); err != nil {
			return err
		}
	}
	return nil
}

func writeConnectedExecution(w *artifactdir.Writer, input, instance string, report runqueue.ConnectedReport) error {
	raw, err := json.Marshal(connectedExecutionManifest{Schema: ConnectedExecutionSchema, Input: input, Instance: instance, Report: report}, json.Deterministic(true))
	if err != nil {
		return err
	}
	if err = w.WriteFile("manifest.json", raw); err != nil {
		return err
	}
	_, err = w.Seal(nil)
	return err
}

func openConnectedPreparation(path string) (map[string][]byte, connectedPreparation, ConnectedDocument, runqueue.ConnectedPlan, error) {
	fail := errors.New("connected suite preparation provenance cannot be verified")
	files, err := artifactdir.Read(path, connectedPreparedFamily.Layout)
	if err != nil || strings.TrimSpace(string(files["identity.sha256"])) != artifactdir.Identity(ConnectedPreparedSchema, files) {
		return nil, connectedPreparation{}, ConnectedDocument{}, runqueue.ConnectedPlan{}, fail
	}
	var manifest connectedPreparation
	if runnerprotocol.Exact(files["manifest.json"], "schema", "suite", "environment", "input", "capabilities", "promotion", "revision_assumption") != nil ||
		json.Unmarshal(files["manifest.json"], &manifest, json.RejectUnknownMembers(true)) != nil || manifest.Schema != ConnectedPreparedSchema || manifest.Suite != Identity(files["suite.json"]) || manifest.Capabilities.Validate() != nil {
		return nil, manifest, ConnectedDocument{}, runqueue.ConnectedPlan{}, fail
	}
	doc, err := DecodeConnected(files["suite.json"])
	if err != nil {
		return nil, manifest, doc, runqueue.ConnectedPlan{}, fail
	}
	queue, err := runqueue.DecodeConnectedPlan(files["queue.json"])
	if err != nil || len(doc.Tests) != len(queue.Jobs) || doc.Parallelism != queue.Parallelism {
		return nil, manifest, doc, queue, fail
	}
	selected := slices.IndexFunc(doc.Environments, func(e ConnectedEnvironment) bool { return e.ID == manifest.Environment })
	if selected < 0 {
		return nil, manifest, doc, queue, fail
	}
	for i, test := range doc.Tests {
		job := queue.Jobs[i]
		at := slices.IndexFunc(doc.Environments[selected].Bindings, func(b ConnectedBinding) bool { return b.Test == test.ID })
		binding := doc.Environments[selected].Bindings[at]
		if job.ID != test.ID || job.State != test.State || !slices.Equal(job.After, test.After) || job.Plan != "plans/"+test.ID || job.PlanIdentity != binding.PlanIdentity {
			return nil, manifest, doc, queue, fail
		}
		planPath := filepath.Join(path, job.Plan)
		plan, err := connectedtest.OpenFlowPlan(planPath)
		if err != nil || plan.Identity() != job.PlanIdentity {
			return nil, manifest, doc, queue, fail
		}
		review, err := expectation.ReviewConnected(planPath)
		if err != nil || review.ID != test.ID || review.Revision != test.Revision || review.Definition != test.Definition {
			return nil, manifest, doc, queue, fail
		}
		release, err := expectation.DecodeConnected(files["releases/"+test.ID+".json"])
		if err != nil || release.Identity() != test.ReleaseIdentity || release.Review != review {
			return nil, manifest, doc, queue, fail
		}
		inputRaw := files["inputs/"+test.ID+".json"]
		var input struct {
			Plan          string                           `json:"plan"`
			Configuration string                           `json:"configuration"`
			Bindings      map[string]networkaction.Binding `json:"bindings"`
			Sources       map[string]string                `json:"sources"`
			Registry      string                           `json:"isolation_registry"`
			Policy        string                           `json:"isolation_policy"`
			Validation    string                           `json:"validation"`
		}
		if runnerprotocol.Exact(inputRaw, "plan", "configuration", "bindings", "sources", "isolation_registry", "isolation_policy", "validation") != nil ||
			json.Unmarshal(inputRaw, &input, json.RejectUnknownMembers(true)) != nil || networkaction.Digest(inputRaw) != job.Input || input.Plan != job.PlanIdentity ||
			input.Configuration != networkaction.Digest(files["configurations/"+test.ID+".json"]) || !validDigest(input.Registry) || !validDigest(input.Policy) ||
			input.Validation != "" && !validDigest(input.Validation) || len(input.Bindings) == 0 || input.Sources == nil {
			return nil, manifest, doc, queue, fail
		}
		if input.Registry != networkaction.Digest(files["registries/"+test.ID+".json"]) {
			return nil, manifest, doc, queue, fail
		}
	}
	want := identity(struct {
		Suite        string
		Environment  string
		Queue        runqueue.ConnectedPlan
		Capabilities runnerprotocol.Capabilities
	}{manifest.Suite, manifest.Environment, queue, manifest.Capabilities})
	if manifest.Input != want {
		return nil, manifest, doc, queue, fail
	}
	if raw, present := files["promotion.json"]; present {
		promotion, e := DecodeConnectedPromotion(raw)
		capability, capErr := manifest.Capabilities.Identity()
		expected := ConnectedPromotionReview{Schema: ConnectedPromotionReviewSchema, Suite: manifest.Suite, Environment: manifest.Environment, Revision: manifest.Revision, Input: manifest.Input, Capabilities: capability, Jobs: []CoverageSpecification{}}
		for _, job := range queue.Jobs {
			expected.Jobs = append(expected.Jobs, CoverageSpecification{Job: job.ID, SHA256: job.Input})
		}
		if e != nil || capErr != nil || promotion.Identity() != manifest.Promotion || promotion.Reviewed != expected.Identity() {
			return nil, manifest, doc, queue, fail
		}
	} else if manifest.Promotion != "" || manifest.Revision != "" {
		return nil, manifest, doc, queue, fail
	}
	return files, manifest, doc, queue, nil
}

// OpenConnectedExecution verifies parent, approved definitions and each child
// through passive readers. It neither invokes a provider nor starts a worker.
func OpenConnectedExecution(ctx context.Context, path string) (ConnectedExecution, error) {
	fail := errors.New("connected suite execution provenance cannot be verified")
	files, err := artifactdir.Read(path, connectedExecutionFamily.Layout)
	if err != nil || strings.TrimSpace(string(files["identity.sha256"])) != artifactdir.Identity(ConnectedExecutionSchema, files) {
		return ConnectedExecution{}, fail
	}
	_, preparation, doc, queue, err := openConnectedPreparation(filepath.Join(path, "prepared"))
	if err != nil {
		return ConnectedExecution{}, err
	}
	var manifest connectedExecutionManifest
	if runnerprotocol.Exact(files["manifest.json"], "schema", "input", "instance", "report") != nil || json.Unmarshal(files["manifest.json"], &manifest, json.RejectUnknownMembers(true)) != nil ||
		manifest.Schema != ConnectedExecutionSchema || manifest.Input != preparation.Input || !runnerprotocol.ID(manifest.Instance) || manifest.Report.Schema != runqueue.ConnectedReportSchema ||
		manifest.Report.Parallelism != queue.Parallelism || len(manifest.Report.Jobs) != len(queue.Jobs) {
		return ConnectedExecution{}, fail
	}
	counts := map[runqueue.Admission]int{}
	// No unreported child is allowed to disappear from the denominator.
	runs, err := os.Open(filepath.Join(path, "runs"))
	if err != nil {
		return ConnectedExecution{}, fail
	}
	entries, readErr := runs.ReadDir(65)
	runs.Close()
	known := map[string]bool{}
	for _, job := range queue.Jobs {
		known[job.ID] = true
	}
	if readErr != nil && readErr != io.EOF || len(entries) > 64 {
		return ConnectedExecution{}, fail
	}
	for _, entry := range entries {
		if !known[entry.Name()] || !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return ConnectedExecution{}, fail
		}
	}
	for i, job := range queue.Jobs {
		result := manifest.Report.Jobs[i]
		if result.ID != job.ID {
			return ConnectedExecution{}, fail
		}
		counts[result.Admission]++
		directory := filepath.Join(path, "runs", job.ID)
		if result.Flow == nil {
			if result.Admission == runqueue.Executed || result.Admission != runqueue.Refused && result.Admission != runqueue.Skipped && result.Admission != runqueue.StartFailed {
				return ConnectedExecution{}, fail
			}
			if _, err := os.Lstat(directory); !os.IsNotExist(err) {
				return ConnectedExecution{}, fail
			}
			continue
		}
		if result.Admission != runqueue.Executed {
			return ConnectedExecution{}, fail
		}
		flow, err := connectedrun.OpenFlow(ctx, directory)
		if err != nil {
			flow, err = connectedrun.InspectFlow(ctx, directory)
		}
		if err != nil || flow.Plan != job.PlanIdentity || flow.Instance != runqueue.ConnectedInstance(manifest.Instance, job.ID) || flow.Engine != preparation.Capabilities.Engine || identity(flow) != identity(*result.Flow) {
			return ConnectedExecution{}, fail
		}
		if err = connectedrun.VerifyFlowInput(ctx, directory, files["prepared/inputs/"+job.ID+".json"], files["prepared/configurations/"+job.ID+".json"], runqueue.ConnectedInstance(manifest.Instance, job.ID), files["prepared/registries/"+job.ID+".json"]); err != nil {
			return ConnectedExecution{}, err
		}
	}
	if counts[runqueue.Executed] != manifest.Report.Executed || counts[runqueue.Refused] != manifest.Report.Refused || counts[runqueue.Skipped] != manifest.Report.Skipped || counts[runqueue.StartFailed] != manifest.Report.StartFailed {
		return ConnectedExecution{}, fail
	}
	return ConnectedExecution{Identity: artifactdir.Identity(ConnectedExecutionSchema, files), Preparation: preparation, Document: doc, Queue: queue, Report: manifest.Report}, nil
}
