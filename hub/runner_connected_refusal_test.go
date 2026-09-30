package hub_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/customerrunner"
	"github.com/bharm16/readmit/internal/operationguard"
	"github.com/bharm16/readmit/internal/suite"
	"github.com/bharm16/readmit/internal/testlicense"
)

func TestConnectedCustomerRunnerWrongCapabilityRetainsActionablePrivateRefusalBeforeStimulus(t *testing.T) {
	f := newConnectedHubFixture(t)
	h, c, request, authority := provisionConnectedSuite(t, f)
	f.grants.Runners[0].Capability = strings.Repeat("c", 64)
	f.writeGrants()
	output := filepath.Join(h.Root, "wrong-capability-ci")
	request.Output = output
	profile := operationguard.Profile{Name: "runner", Execution: operationguard.ExecuteEachJob}
	var result suite.CIReport
	if e := operationguard.New(testlicense.New(t)).Run(t.Context(), profile, func(ctx context.Context) error {
		result = customerrunner.RunConnectedCI(ctx, c, request, authority, "")
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if result.ExitCode != 2 || result.Jobs != 1 || result.Executed != 0 || h.Lab.Creates.Load() != 0 {
		t.Fatalf("missing capability became success or lost denominator: %+v", result)
	}
	metadata, e := customerrunner.InspectConnectedCIRefusal(t.Context(), output)
	if e != nil || metadata == nil || metadata.Category != "capability-or-admission-denied" || len(metadata.Required) == 0 {
		t.Fatalf("private refusal not actionable: %+v %v", metadata, e)
	}
	if h.Lab.Creates.Load() != 0 {
		t.Fatal("passive refusal inspection stimulated target")
	}
}
