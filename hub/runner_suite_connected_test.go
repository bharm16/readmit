package hub_test

import (
	"context"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/connectedlab"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/customerrunner"
	"github.com/bharm16/readmit/internal/expectation"
	"github.com/bharm16/readmit/internal/operationguard"
	"github.com/bharm16/readmit/internal/runqueue"
	"github.com/bharm16/readmit/internal/suite"
	"github.com/bharm16/readmit/internal/testlicense"
)

func provisionConnectedSuite(t *testing.T, f *connectedHubFixture) (*connectedlab.Harness, customerrunner.Config, suite.ConnectedRequest, string) {
	t.Helper()
	h := connectedlab.New(t, "", connectedlab.Message{Step: "v2-book", Raw: "MSH|^~\\&|SENDER|LAB|ENGINE|LAB|20260101000000||SIU^S12|RUNNER-BOOK|P|2.5.1\rSCH|RUNNER-1||||||||||2026-05-01T09:00:00Z\r"})
	h.HorizonMS = 3000 // Actual mTLS authority/provider checks are inside the window.
	h.Lab.SetDownstream("encounter")
	query := "identifier=urn%3Areadmit-lab%3Aappointment%7CRUNNER-1"
	rows := h.Observe("v2", "encounters", "after", "Encounter", query, "authoritative-application-api", connectedlab.FieldColumn("key", "text", "", true, true, "identifier#0", "value"), connectedlab.FieldColumn("start", "datetime", "", false, true, "period", "start"))
	datasets := []connectedtest.Dataset{rows}
	nativeRows := h.Observe("native", "encounters", "after", "Encounter", "identifier=urn%3Areadmit-lab%3Aappointment%7CNATIVE-RUNNER-1", "authoritative-application-api", connectedlab.FieldColumn("key", "text", "", true, true, "identifier#0", "value"), connectedlab.FieldColumn("start", "datetime", "", false, true, "period", "start"))
	nativeDatasets := []connectedtest.Dataset{nativeRows}
	holds := true
	wire := h.RefJSON("control-echo", assertion.Schema, "control-echo.json", assertion.Set{Schema: assertion.Schema, Name: "Original control acknowledgement", Assertions: []assertion.Assertion{{ID: "control-echo", Operator: assertion.ValuesEqual,
		Subject: assertion.Subject{Pair: &assertion.PairRef{Left: assertion.FieldRef{Scope: "input", Message: h.Occurrences["v2-book"], Selector: "MSH-10"}, Right: assertion.FieldRef{Scope: "observed", Message: h.Occurrences["v2-book"], Selector: "MSA-2"}}}, Expected: assertion.Expected{Holds: &holds}}}})
	h.Compile(connectedtest.FlowTest{ID: "booking", Steps: []connectedtest.Step{h.V2Step("v2-book"), h.FHIRStep("create", "POST", "Appointment", `{"resourceType":"Appointment","identifier":[{"system":"urn:readmit-lab:appointment","value":"NATIVE-RUNNER-1"}],"status":"booked","start":"2026-05-02T10:30:00Z","participant":[{"status":"accepted"}]}`, connectedtest.FHIRHeaders{}, nil)}, Phases: []connectedtest.FlowPhase{
		{ID: "v2", Steps: []string{"v2-book"}, Datasets: datasets, Wire: &connectedtest.WireChecks{Set: wire, Observed: "transport-acks"}, Checks: h.Checks("v2", datasets, connectedlab.RowCount("one-encounter", "encounters", 1), connectedlab.Instant("start", "encounters", "start", "2026-05-01T09:00:00Z"))},
		{ID: "native", Steps: []string{"create"}, Datasets: nativeDatasets, Checks: h.Checks("native", nativeDatasets, connectedlab.RowCount("one-encounter", "encounters", 1), connectedlab.Instant("start", "encounters", "start", "2026-05-02T10:30:00Z"))}}})
	review, err := expectation.ReviewConnected(h.PlanPath)
	if err != nil {
		t.Fatal(err)
	}
	release, err := expectation.ApproveConnected(h.PlanPath, "", review.Identity(), "Reviewer", "Literal independent downstream count", filepath.Join(h.Root, "release.json"))
	if err != nil {
		t.Fatal(err)
	}
	document := suite.ConnectedDocument{Schema: suite.ConnectedSchema, ID: "regression", Owner: "interop", Tags: []string{}, Parallelism: 1,
		Tests:        []suite.ConnectedTest{{ID: "booking", Revision: "1", Definition: review.Definition, Release: "release.json", ReleaseIdentity: release.Identity(), After: []string{}, State: "enabled"}},
		Environments: []suite.ConnectedEnvironment{{ID: "qa", Bindings: []suite.ConnectedBinding{{Test: "booking", Plan: "flow-plan", PlanIdentity: h.Plan.Identity(), Config: "flow-config.json"}}}}}
	path := filepath.Join(h.Root, "suite.json")
	connectedlab.WriteJSON(t, path, document)
	promotionReview, err := suite.ReviewConnectedPromotion(path, "qa", "independent-lab")
	if err != nil {
		t.Fatal(err)
	}
	promotionPath := filepath.Join(h.Root, "promotion.json")
	promotion, err := suite.ApproveConnectedPromotion(path, "qa", "independent-lab", promotionReview.Identity(), "Operator", "Exact private QA binding", promotionPath)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := suite.PrepareConnected(suite.ConnectedRequest{Path: path, Environment: "qa", Output: filepath.Join(h.Root, "prepared")})
	if err != nil {
		t.Fatal(err)
	}
	capability, err := prepared.Capabilities.Identity()
	if err != nil {
		t.Fatal(err)
	}
	authorityPath := filepath.Join(h.Root, "authority.json")
	connectedlab.WriteJSON(t, authorityPath, customerrunner.ConnectedAuthority{Schema: customerrunner.ConnectedAuthoritySchema, Actor: "runner", Generation: "1", Input: prepared.Identity, Promotion: promotion.Identity(), Capabilities: capability,
		Operations: prepared.Operations(), IssuedAt: time.Now().Add(-time.Minute), Expires: time.Now().Add(time.Hour), MaxSeconds: 120, MaxOccurrences: 10})
	// The hub's real customer project is explicitly configured for this lab.
	for i := range f.policy.Grants {
		f.policy.Grants[i].Project = "lab"
	}
	for i := range f.policy.Tokens {
		f.policy.Tokens[i].Project = "lab"
	}
	writePolicy(t, f.accessPath, f.policy)
	f.grants.Runners[0].Project, f.grants.Runners[0].Environment = "lab", "test"
	f.grants.Runners[0].Capability, f.grants.Runners[0].MaxSeconds, f.grants.Runners[0].MaxJobs = capability, 120, 10
	f.writeGrants()
	c := f.runnerConfig()
	c.Project, c.Environment = "lab", "test"
	request := suite.ConnectedRequest{Path: path, Environment: "qa", Output: filepath.Join(h.Root, "execution"), Promotion: promotionPath, PromotionIdentity: promotion.Identity(), Revision: "independent-lab", Instance: "customer-occurrence"}
	return h, c, request, authorityPath
}

func executeCustomerConnectedSuite(t *testing.T, c customerrunner.Config, request suite.ConnectedRequest, authority string) (report runqueue.ConnectedReport, err error) {
	t.Helper()
	profile := operationguard.Profile{Name: "runner", Execution: operationguard.ExecuteEachJob}
	err = operationguard.New(testlicense.New(t)).Run(t.Context(), profile, func(ctx context.Context) error {
		var runErr error
		report, runErr = customerrunner.RunConnectedSuite(ctx, c, request, authority)
		return runErr
	})
	return report, err
}

func TestConnectedSuiteActualCustomerEnrollmentUsesFreshAuthorityAndRetainedOracle(t *testing.T) {
	f := newConnectedHubFixture(t)
	h, c, request, authority := provisionConnectedSuite(t, f)
	before := f.requests.Load()
	report, err := executeCustomerConnectedSuite(t, c, request, authority)
	if err != nil || report.ExitCode() != 0 || report.Executed != 1 || h.Lab.Creates.Load() != 2 || f.requests.Load() <= before {
		if len(report.Jobs) > 0 && report.Jobs[0].Flow != nil {
			t.Logf("actual child: %+v", *report.Jobs[0].Flow)
		}
		t.Fatalf("actual enrolled suite: %+v %v; target writes=%d hub calls=%d", report, err, h.Lab.Creates.Load(), f.requests.Load()-before)
	}
	// An already-claimed occurrence cannot be sent again even with a fresh output.
	retry := request
	retry.Output = filepath.Join(h.Root, "retry")
	if _, err := executeCustomerConnectedSuite(t, c, retry, authority); err == nil || h.Lab.Creates.Load() != 2 {
		t.Fatal("retained occurrence was silently retried", err)
	}
	h.Lab.Server().Close()
	f.server.Close()
	if err = os.Remove(c.Key.Arguments[0]); err != nil {
		t.Fatal(err)
	}
	opened, err := suite.OpenConnectedExecution(t.Context(), request.Output)
	if err != nil || opened.Report.ExitCode() != 0 {
		t.Fatal("offline proof depended on hub, target or credential provider", err)
	}
	bytes, _ := json.Marshal(opened.Report)
	if len(bytes) == 0 {
		t.Fatal("verified report absent")
	}
}
