package customerrunner

import (
	"context"
	"encoding/json/v2"
	"encoding/xml"
	"path/filepath"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/suite"
)

const ConnectedCISchema = "readmit-connected-suite-ci/v1"

type connectedCIManifest struct {
	Schema    string                    `json:"schema"`
	Execution string                    `json:"execution"`
	Summary   suite.CIReport            `json:"summary"`
	Refusal   *ConnectedRefusalMetadata `json:"refusal,omitzero"`
}

var connectedCIFamily = artifactdir.Family{Layout: artifactdir.Layout{Noun: "connected CI run", RequiredFiles: []string{"manifest.json", "ci.json", "junit.xml", "identity.sha256"}, Nested: []string{"execution"}, AllowEmpty: func(n string, _ map[string][]byte) bool { return strings.HasPrefix(n, "execution/") }, MaxFiles: 400000, MaxFileBytes: 64 << 20, MaxBytes: 2 << 30,
	AllowFile: func(n string) bool {
		return n == "manifest.json" || n == "ci.json" || n == "junit.xml" || n == "identity.sha256" || n == "ci-coverage.json"
	}}, Seal: artifactdir.DirectoryHash(ConnectedCISchema)}

// RunConnectedCI invokes the single enrolled suite path once. Only its fixed
// summaries are safe for CI logs; all linked actual proof remains private.
func RunConnectedCI(ctx context.Context, c Config, request suite.ConnectedRequest, authority, requirements string) suite.CIReport {
	result := suite.CIError()
	if raw, e := (artifactdir.Document{MaxBytes: suite.MaxBytes}).Read(request.Path); e == nil {
		if document, e := suite.DecodeConnected(raw); e == nil {
			result.Jobs = len(document.Tests)
		}
	}
	var coverageRaw []byte
	if requirements != "" {
		result.Coverage = "failed"
		var err error
		coverageRaw, err = (artifactdir.Document{MaxBytes: suite.MaxBytes}).Read(requirements)
		if err != nil {
			return result
		}
		if _, err = suite.DecodeConnectedCoverage(coverageRaw); err != nil {
			return result
		}
	}
	w, err := artifactdir.Create(request.Output, connectedCIFamily, artifactdir.Durable)
	if err != nil {
		return result
	}
	defer w.Close()
	request.Output = filepath.Join(w.Path(), "execution")
	report, runErr := RunConnectedSuite(ctx, c, request, authority)
	if report.Schema != "" {
		result.Jobs = len(report.Jobs)
		result.Executed = report.Executed
		result.Skipped = report.Skipped
		result.ExitCode = report.ExitCode()
	}
	if runErr != nil || ctx.Err() != nil || result.Jobs == 0 {
		result.ExitCode = 2
	}
	if coverageRaw != nil {
		if err = w.WriteFile("ci-coverage.json", coverageRaw); err != nil {
			return suite.CIError()
		}
		coverage, e := suite.AssessConnectedCoverage(context.WithoutCancel(ctx), request.Output, filepath.Join(w.Path(), "ci-coverage.json"), time.Now())
		eligible := e == nil && coverage.Passed == coverage.Denominator
		for _, job := range coverage.Jobs {
			eligible = eligible && job.Eligible
		}
		if eligible {
			result.Coverage = "passed"
		} else {
			result.ExitCode = 2
		}
	}
	result.State = "passed"
	if result.ExitCode == 1 {
		result.State = "failed"
	} else if result.ExitCode != 0 {
		result.State = "error"
	}
	identity := ""
	if execution, e := suite.OpenConnectedExecution(context.WithoutCancel(ctx), request.Output); e == nil {
		identity = execution.Identity
	}
	if result.ExitCode == 0 && identity == "" {
		result = suite.CIError()
	}
	encoded, e := json.Marshal(result, json.Deterministic(true))
	if e != nil || w.WriteFile("ci.json", encoded) != nil {
		return suite.CIError()
	}
	junit := connectedJUnit{Name: "readmit", Tests: 1, Case: connectedJUnitCase{Name: "saved-suite-gate", Class: "readmit"}}
	if result.ExitCode != 0 {
		junit.Failures = 1
		junit.Case.Failure = &connectedJUnitFailure{Message: "Suite gate did not pass; inspect retained evidence privately"}
	}
	encoded, e = xml.MarshalIndent(junit, "", "  ")
	if e != nil || w.WriteFile("junit.xml", append([]byte(xml.Header), encoded...)) != nil {
		return suite.CIError()
	}
	manifest := connectedCIManifest{Schema: ConnectedCISchema, Execution: identity, Summary: result}
	if runErr != nil {
		refusal := connectedRefusalMetadata(runErr)
		manifest.Refusal = &refusal
	}
	encoded, e = json.Marshal(manifest, json.Deterministic(true))
	if e != nil || w.WriteFile("manifest.json", encoded) != nil {
		return suite.CIError()
	}
	if _, e = w.Seal(nil); e != nil {
		return suite.CIError()
	}
	return result
}

// InspectConnectedCIRefusal exposes fixed actionable setup metadata only after
// the complete private envelope has verified, without reconnecting.
func InspectConnectedCIRefusal(ctx context.Context, directory string) (*ConnectedRefusalMetadata, error) {
	files, err := artifactdir.Read(directory, connectedCIFamily.Layout)
	if err != nil {
		return nil, err
	}
	if _, err = inspectConnectedCIFiles(ctx, directory, files); err != nil {
		return nil, err
	}
	var manifest connectedCIManifest
	if json.Unmarshal(files["manifest.json"], &manifest, json.RejectUnknownMembers(true)) != nil {
		return nil, ErrRefused
	}
	return manifest.Refusal, nil
}

// InspectConnectedCI checks linked evidence offline; an old safe aggregate
// alone can never substitute for a successful current execution.
func InspectConnectedCI(ctx context.Context, directory string) (suite.CIReport, error) {
	files, err := artifactdir.Read(directory, connectedCIFamily.Layout)
	if err != nil {
		return suite.CIError(), err
	}
	return inspectConnectedCIFiles(ctx, directory, files)
}

func inspectConnectedCIFiles(ctx context.Context, directory string, files map[string][]byte) (suite.CIReport, error) {
	if strings.TrimSpace(string(files["identity.sha256"])) != artifactdir.Identity(ConnectedCISchema, files) {
		return suite.CIError(), ErrRefused
	}
	var manifest connectedCIManifest
	if json.Unmarshal(files["manifest.json"], &manifest, json.RejectUnknownMembers(true)) != nil || manifest.Schema != ConnectedCISchema {
		return suite.CIError(), ErrRefused
	}
	if manifest.Refusal != nil && (manifest.Summary.ExitCode != 2 || !manifest.Refusal.valid()) {
		return suite.CIError(), ErrRefused
	}
	result, err := suite.DecodeCI(files["ci.json"])
	if err != nil {
		return result, err
	}
	a, _ := json.Marshal(result, json.Deterministic(true))
	b, _ := json.Marshal(manifest.Summary, json.Deterministic(true))
	if string(a) != string(b) {
		return suite.CIError(), ErrRefused
	}
	if manifest.Execution != "" {
		execution, e := suite.OpenConnectedExecution(ctx, filepath.Join(directory, "execution"))
		if e != nil || execution.Identity != manifest.Execution {
			return suite.CIError(), ErrRefused
		}
		if result.Jobs != len(execution.Report.Jobs) || result.Executed != execution.Report.Executed || result.Skipped != execution.Report.Skipped {
			return suite.CIError(), ErrRefused
		}
		if result.ExitCode < execution.Report.ExitCode() {
			return suite.CIError(), ErrRefused
		}
		if raw, present := files["ci-coverage.json"]; present {
			if _, e := suite.DecodeConnectedCoverage(raw); e != nil {
				return suite.CIError(), ErrRefused
			}
			coverage, e := suite.AssessConnectedCoverage(ctx, filepath.Join(directory, "execution"), filepath.Join(directory, "ci-coverage.json"), time.Now())
			eligible := e == nil && coverage.Passed == coverage.Denominator
			for _, job := range coverage.Jobs {
				eligible = eligible && job.Eligible
			}
			if eligible && result.Coverage != "passed" || !eligible && (result.Coverage != "failed" || result.ExitCode != 2) {
				return suite.CIError(), ErrRefused
			}
		} else if result.Coverage != "not_requested" {
			return suite.CIError(), ErrRefused
		}
	} else if result.ExitCode == 0 {
		return suite.CIError(), ErrRefused
	}
	return result, nil
}

type connectedJUnit struct {
	XMLName  xml.Name           `xml:"testsuite"`
	Name     string             `xml:"name,attr"`
	Tests    int                `xml:"tests,attr"`
	Failures int                `xml:"failures,attr"`
	Case     connectedJUnitCase `xml:"testcase"`
}
type connectedJUnitCase struct {
	Name    string                 `xml:"name,attr"`
	Class   string                 `xml:"classname,attr"`
	Failure *connectedJUnitFailure `xml:"failure,omitempty"`
}
type connectedJUnitFailure struct {
	Message string `xml:"message,attr"`
}
