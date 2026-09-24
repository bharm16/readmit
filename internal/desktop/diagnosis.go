package desktop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/diagnose"
	"github.com/bharm16/readmit/internal/findingreview"
	"github.com/bharm16/readmit/internal/operation"
)

// MaxDiagnosisFindings bounds one window of findings, matching MaxComparisonRows.
const MaxDiagnosisFindings = 200

// DiagnosisRequest names the verified case, the configuration the diagnosis
// runs under, and the new directory entry the report is written into.
// Config names a workspace entry declaring readmit-diagnose-config/v1;
// Builtin selects one of the three built-in configurations when Config is
// empty ("siu", "lifecycle", "order"). Nothing is chosen implicitly.
type DiagnosisRequest struct {
	Workspace string `json:"workspace"`
	Case      string `json:"case"`
	Identity  string `json:"identity"`
	Config    string `json:"config,omitzero"`
	Builtin   string `json:"builtin,omitzero"`
	Output    string `json:"output,omitzero"` // read by RunDiagnosis alone
	Offset    int    `json:"offset"`
}

// Diagnosis is one report windowed for the panes. Every count is the
// engine's own; a window can never read as the whole of it. CaseIdentity is
// the identity of the case the report was run over, as the report records it,
// so a retained report of another case is never read as the open case's.
type Diagnosis struct {
	Case         string                 `json:"case"`
	Config       string                 `json:"config,omitzero"`
	ReportSHA256 string                 `json:"report_sha256"`
	CaseIdentity string                 `json:"case_identity"`
	Schema       string                 `json:"schema"`
	Profile      string                 `json:"profile"`
	Ruleset      string                 `json:"ruleset"`
	Rules        []string               `json:"rules"`
	Window       diagnose.Window        `json:"window"`
	Scope        string                 `json:"scope"`
	NoFindings   string                 `json:"no_findings,omitzero"`
	Offset       int                    `json:"offset"`
	Total        int                    `json:"total"`
	Findings     []diagnose.Finding     `json:"findings"`
	Unsupported  []diagnose.Unsupported `json:"unsupported"`
}

// DiagnosisResult carries one state. Diagnosis is present whenever a report
// was produced or read, including when the window holds no finding, because
// the counts and the scope sentence are the answer in that case.
type DiagnosisResult struct {
	State     State      `json:"state"`
	Reason    string     `json:"reason,omitzero"`
	Output    string     `json:"output,omitzero"`
	Diagnosis *Diagnosis `json:"diagnosis,omitzero"`
}

func (r *DiagnosisResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// digest is the identity every derived document is bound to here: the SHA-256
// of the exact bytes as they sit in the workspace, in the same hexadecimal
// form the command line reports.
func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// diagnosisConfig resolves the one configuration a diagnosis runs under: a
// workspace entry declaring readmit-diagnose-config/v1, or one of the three
// built-in configurations by name. Exactly one must be chosen — a diagnosis
// run from the window never gets a configuration nobody selected.
func diagnosisConfig(root, entry, builtin string) (diagnose.Config, refusal) {
	if entry != "" {
		data, declined := workspaceDocument(root, entry, maxDiagnoseConfigBytes, "the diagnosis configuration")
		if data == nil {
			return diagnose.Config{}, declined
		}
		config, err := diagnose.ParseConfig(data)
		if err != nil {
			return diagnose.Config{}, refusal{Failed, err.Error()}
		}
		return config, refusal{}
	}
	switch builtin {
	case "siu":
		return diagnose.DefaultConfig(), refusal{}
	case "lifecycle":
		return diagnose.LifecycleConfig(), refusal{}
	case "order":
		return diagnose.OrderConfig(), refusal{}
	}
	return diagnose.Config{}, refusal{Failed, "a diagnosis runs under one named configuration entry or one built-in selection: siu, lifecycle or order"}
}

// RunDiagnosis runs one supported diagnosis over the verified case and writes
// report.json and report.md into one new directory entry, exactly as
// `readmit diagnose` writes them. The report is bound to the case identity the
// window displayed, and an existing destination is refused, never overwritten.
// Like `readmit diagnose`, it reads existing evidence and needs no license
// term, so an expired license never gates it.
func (a *App) RunDiagnosis(request DiagnosisRequest) DiagnosisResult {
	return run(a, false, false, func(context.Context) DiagnosisResult {
		failure := func(reason string) DiagnosisResult {
			return DiagnosisResult{State: Failed, Reason: reason}
		}
		if request.Offset < 0 {
			return failure("a diagnosis window cannot begin before its first finding")
		}
		root, _, declined := openedCase(request.Workspace, request.Case, request.Identity)
		if root == "" {
			return DiagnosisResult{State: declined.state, Reason: declined.reason}
		}
		config, declined := diagnosisConfig(root, request.Config, request.Builtin)
		if declined.reason != "" {
			return DiagnosisResult{State: declined.state, Reason: declined.reason}
		}
		report, err := diagnose.Run(artifactpath.JoinReference(root, request.Case), config)
		if err != nil {
			return failure(err.Error())
		}
		jsonData, err := diagnose.JSON(report)
		if err != nil {
			return failure("cannot encode diagnosis report")
		}
		if artifactpath.EntryName(request.Output) != nil {
			return failure("a diagnosis is written to one new directory entry of the open workspace")
		}
		if err := operation.WriteReportDirectory(filepath.Join(root, request.Output), "diagnosis",
			operation.ReportFile{Name: "report.json", Data: jsonData},
			operation.ReportFile{Name: "report.md", Data: diagnose.Markdown(report)},
		); err != nil {
			return failure(err.Error())
		}
		result := windowedDiagnosis(request.Case, request.Config, digest(jsonData), report, request.Offset)
		result.Output = request.Output
		return result
	})
}

// OpenDiagnosisReport reads one retained diagnosis report directory of the
// open workspace with the same strict reader a review uses, and reports the
// identity a review must name.
func (a *App) OpenDiagnosisReport(workspace, entry string, offset int) DiagnosisResult {
	return run(a, false, false, func(context.Context) DiagnosisResult {
		failure := func(reason string) DiagnosisResult {
			return DiagnosisResult{State: Failed, Reason: reason}
		}
		if offset < 0 {
			return failure("a diagnosis window cannot begin before its first finding")
		}
		root, declined := resolveFolder(workspace)
		if root == "" {
			return DiagnosisResult{State: declined.state, Reason: declined.reason}
		}
		data, declined := diagnosisReportDocument(root, entry)
		if data == nil {
			return DiagnosisResult{State: declined.state, Reason: declined.reason}
		}
		report, err := findingreview.ParseReport(data)
		if err != nil {
			return failure(err.Error())
		}
		return windowedDiagnosis(entry, "", digest(data), report, offset)
	})
}

// diagnosisReportDocument reads the report.json of one retained diagnosis
// report directory, under the same byte bound a review reads it.
func diagnosisReportDocument(root, entry string) ([]byte, refusal) {
	dir, err := artifactpath.Child(root, entry)
	if err != nil {
		return nil, refusal{Failed, "a diagnosis report must be one directory entry of the open workspace"}
	}
	path := filepath.Join(dir, "report.json")
	info, err := os.Lstat(path)
	switch {
	case err != nil || !info.Mode().IsRegular():
		return nil, refusal{Failed, "a diagnosis report directory holds report.json as one regular file"}
	case info.Size() > findingreview.MaxReportBytes:
		return nil, refusal{Failed, "the diagnosis report is larger than this release reads"}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, refusal{Failed, "the diagnosis report could not be read"}
	}
	return data, refusal{}
}

// windowedDiagnosis lays one report out for the panes: the engine's own
// counts, scope and window, and the requested window of its findings.
func windowedDiagnosis(caseName, config, reportSHA256 string, report diagnose.Report, offset int) DiagnosisResult {
	start := min(offset, len(report.Findings))
	described := &Diagnosis{
		Case: caseName, Config: config, ReportSHA256: reportSHA256,
		CaseIdentity: report.CaseIdentity, Schema: report.Schema, Profile: report.Profile, Ruleset: report.Ruleset,
		Rules: report.Rules, Window: report.Window, Scope: report.Scope,
		NoFindings: report.NoFindings,
		Offset:     offset, Total: len(report.Findings),
		Findings:    report.Findings[start : start+min(MaxDiagnosisFindings, len(report.Findings)-start)],
		Unsupported: report.Unsupported,
	}
	if described.Rules == nil {
		described.Rules = []string{}
	}
	if described.Findings == nil {
		described.Findings = []diagnose.Finding{}
	}
	if described.Unsupported == nil {
		described.Unsupported = []diagnose.Unsupported{}
	}
	switch {
	case len(report.Findings) == 0:
		return DiagnosisResult{State: Empty, Reason: "this diagnosis produced no findings inside its declared scope", Diagnosis: described}
	case len(described.Findings) == 0:
		return DiagnosisResult{State: Empty, Reason: "this window begins past the last finding of this diagnosis", Diagnosis: described}
	}
	return DiagnosisResult{State: Completed, Diagnosis: described}
}

// GroupDiagnosesRequest names the cases whose findings are grouped by
// signature. Cases are entries of the open workspace.
type GroupDiagnosesRequest struct {
	Workspace string   `json:"workspace"`
	Cases     []string `json:"cases"`
	Config    string   `json:"config,omitzero"`
	Builtin   string   `json:"builtin,omitzero"`
	Offset    int      `json:"offset"`
}

// DiagnosisGroupsResult carries one grouped reading of several diagnoses. The
// group list is windowed by the request's offset; Cases keeps every complete
// unchanged diagnosis, exactly as the engine's own contract requires.
type DiagnosisGroupsResult struct {
	State       State                  `json:"state"`
	Reason      string                 `json:"reason,omitzero"`
	Offset      int                    `json:"offset"`
	Total       int                    `json:"total"`
	Groups      *diagnose.GroupsReport `json:"groups,omitzero"`
	CaseEntries map[string]string      `json:"case_entries,omitzero"`
}

func (r *DiagnosisGroupsResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// GroupDiagnoses re-evaluates the selected cases under one configuration and
// groups equal finding signatures, exactly as `readmit diagnose groups` does.
// It reads several cases, so it is interruptible; cancellation discards the
// view and never touches evidence.
func (a *App) GroupDiagnoses(request GroupDiagnosesRequest) DiagnosisGroupsResult {
	return run(a, true, false, func(ctx context.Context) DiagnosisGroupsResult {
		return groupDiagnoses(ctx, request)
	})
}

func groupDiagnoses(ctx context.Context, request GroupDiagnosesRequest) DiagnosisGroupsResult {
	failure := func(reason string) DiagnosisGroupsResult {
		return DiagnosisGroupsResult{State: Failed, Reason: reason}
	}
	if request.Offset < 0 {
		return failure("a grouping window cannot begin before its first group")
	}
	root, declined := resolveFolder(request.Workspace)
	if root == "" {
		return DiagnosisGroupsResult{State: declined.state, Reason: declined.reason}
	}
	config, declined := diagnosisConfig(root, request.Config, request.Builtin)
	if declined.reason != "" {
		return DiagnosisGroupsResult{State: declined.state, Reason: declined.reason}
	}
	paths := make([]string, 0, len(request.Cases))
	for _, name := range request.Cases {
		path, err := artifactpath.Child(root, name)
		if err != nil {
			return failure("every grouped case must be named by one directory entry of the open workspace")
		}
		paths = append(paths, path)
	}
	report, identities, err := diagnose.GroupCasesWithIdentities(ctx, paths, config)
	if err != nil {
		if ctx.Err() != nil {
			return DiagnosisGroupsResult{State: Cancelled, Reason: cancelledRefusal.reason}
		}
		return failure(err.Error())
	}
	entries := make(map[string]string, len(identities))
	for i, identity := range identities {
		entries[identity] = request.Cases[i]
	}
	return windowedDiagnosisGroups(report, request.Offset, entries)
}

// OpenDiagnosisGroupsReport reads a retained grouping through its own strict
// reader. Groupings are presentation artifacts, never single diagnoses or
// finding-review inputs. No case is re-read to guess an entry name from a
// retained report's identities.
func (a *App) OpenDiagnosisGroupsReport(workspace, entry string, offset int) DiagnosisGroupsResult {
	return run(a, false, false, func(context.Context) DiagnosisGroupsResult {
		if offset < 0 {
			return DiagnosisGroupsResult{State: Failed, Reason: "a grouping window cannot begin before its first group"}
		}
		root, declined := resolveFolder(workspace)
		if root == "" {
			return DiagnosisGroupsResult{State: declined.state, Reason: declined.reason}
		}
		dir, err := artifactpath.Child(root, entry)
		if err != nil {
			return DiagnosisGroupsResult{State: Failed, Reason: "a diagnosis grouping report must be one directory entry of the open workspace"}
		}
		path := filepath.Join(dir, "report.json")
		info, err := os.Lstat(path)
		switch {
		case err != nil || !info.Mode().IsRegular():
			return DiagnosisGroupsResult{State: Failed, Reason: "a diagnosis grouping report directory holds report.json as one regular file"}
		case info.Size() > diagnose.MaxGroupsReportBytes+1:
			return DiagnosisGroupsResult{State: Failed, Reason: "the diagnosis grouping report is larger than this release reads"}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return DiagnosisGroupsResult{State: Failed, Reason: "the diagnosis grouping report could not be read"}
		}
		report, err := diagnose.ParseGroups(data)
		if err != nil {
			return DiagnosisGroupsResult{State: Failed, Reason: err.Error()}
		}
		return windowedDiagnosisGroups(report, offset, nil)
	})
}

func windowedDiagnosisGroups(report diagnose.GroupsReport, offset int, entries map[string]string) DiagnosisGroupsResult {
	total := len(report.Groups)
	start := min(offset, total)
	report.Groups = report.Groups[start : start+min(MaxDiagnosisFindings, total-start)]
	return DiagnosisGroupsResult{State: Completed, Offset: offset, Total: total, Groups: &report, CaseEntries: entries}
}
