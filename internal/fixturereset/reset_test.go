package fixturereset

import (
	"context"
	"encoding/json/v2"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/environment"
	"github.com/bharm16/readmit/internal/replay"
	"github.com/bharm16/readmit/internal/sendpolicy"
)

const emptySnapshot = `{"schema":"readmit-observation/v1","profile":"readmit-siu-v1","session_id":"0123456789abcdef0123456789abcdef","mode":"fixed","processed":[],"consistent":true,"records":[]}`

// dirtySnapshot is the same session after one appointment was processed: a
// fixture that is up but has not been reset.
const dirtySnapshot = `{"schema":"readmit-observation/v1","profile":"readmit-siu-v1","session_id":"0123456789abcdef0123456789abcdef","mode":"fixed","processed":[{"occurrence_id":"s0001-e000001","control_id":"MSG00001"}],"consistent":true,"records":[{"record_id":"r000001","patient_id":{"value":"P1","namespace":"","universal_id":"","universal_id_type":""},"placer_id":{"value":"PL1","namespace":"","universal_id":"","universal_id_type":""},"filler_id":{"value":"FI1","namespace":"","universal_id":"","universal_id_type":""},"appointment_start":"20260103110000+0000"}]}`

// quiet accepts a connection and holds it without sending anything, which is
// what a receiver waiting to be sent to does. Nothing here leaves loopback.
func quiet(connection net.Conn) { io := make([]byte, 1); connection.Read(io) }

func endpoint(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer connection.Close()
				quiet(connection)
			}()
		}
	}()
	t.Cleanup(func() {
		listener.Close()
		<-done
	})
	return listener.Addr().String()
}

// closedEndpoint reserves a loopback port and releases it, so a dial to it is
// refused rather than reaching anything.
func closedEndpoint(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	return address
}

func target(address string) replay.Target {
	return replay.Target{
		Schema: replay.TargetSchemaV3, TestEndpoint: true, Name: "lab-siu",
		Classification: replay.Nonproduction, Address: address, Transport: "plain",
		ConnectTimeout: "2s", MessageTimeout: "100ms", MaxACKBytes: 4096,
	}
}

// planDirectory writes one plan directory holding the named files and returns
// its path. No test reaches outside the directory the test framework removes.
func planDirectory(t *testing.T, files map[string]string) string {
	t.Helper()
	directory := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return directory
}

func noResolution(context.Context, string) ([]netip.Addr, error) {
	return nil, net.ErrClosed
}

func TestRunConfirmsAReviewedResetAgainstANonproductionEnvironment(t *testing.T) {
	address := endpoint(t)
	document := planWith(confirmAction,
		`{"id":"empty-ledger","operator":"observation_empty","authority":"read_declared_file","instructions":"The fresh listener exports an empty ledger.","observation":"observation.json"}`,
		`{"id":"endpoint-quiet","operator":"endpoint_quiet","authority":"connect_approved_target","instructions":"The fixture accepts connections again."}`)
	directory := planDirectory(t, map[string]string{"observation.json": emptySnapshot})
	result := Run(t.Context(), Request{
		Target: target(address), PlanBytes: []byte(document), PlanDirectory: directory,
		Confirmed: []string{"stop-listener"},
	}, noResolution)
	if result.State != durablerun.Passed || result.Outcome != Confirmed || result.ExitCode() != 0 {
		t.Fatalf("a reset every action confirmed must pass: %+v", result)
	}
	if result.Schema != OutcomeSchema || result.Environment != "lab-siu" || result.Classification != replay.Nonproduction {
		t.Fatalf("the outcome must name the environment it was produced against: %+v", result)
	}
	// A loopback destination needs no policy document, and the reset asks the
	// same rule a send asks without ever requesting one.
	if result.Decision != sendpolicy.SendNotExplicit {
		t.Fatalf("the reset must record the send decision it was held to, got %q", result.Decision)
	}
	want := []ActionOutcome{
		{ID: "stop-listener", Operator: OperatorConfirms, Authority: NoAuthority, Outcome: Confirmed, Reason: OperatorConfirmed},
		{ID: "empty-ledger", Operator: ObservationEmpty, Authority: ReadDeclaredFile, Outcome: Confirmed, Reason: LedgerEmpty},
		{ID: "endpoint-quiet", Operator: EndpointQuiet, Authority: ConnectApprovedTarget, Outcome: Confirmed, Reason: EndpointReachable, Diagnosis: environment.Reachable},
	}
	if len(result.Actions) != len(want) {
		t.Fatalf("recorded %d actions, expected %d", len(result.Actions), len(want))
	}
	for i, action := range result.Actions {
		if action != want[i] {
			t.Errorf("action %d recorded %+v, expected %+v", i, action, want[i])
		}
	}
}

// TestRunRefusesEveryEnvironmentNobodyRecordedAsNonproduction is the least
// privilege boundary: a reset touches a named, recorded nonproduction
// environment or it touches nothing. A label is not proof, so the absence of
// one is certainly not.
func TestRunRefusesEveryEnvironmentNobodyRecordedAsNonproduction(t *testing.T) {
	for name, environment := range map[string]struct {
		classification replay.Classification
		reason         Reason
	}{
		"production":   {replay.Production, ProductionEnvironment},
		"unclassified": {replay.Unclassified, UnrecordedEnvironment},
		"unrecorded":   {"", UnrecordedEnvironment},
	} {
		configuration := target(closedEndpoint(t))
		configuration.Classification = environment.classification
		directory := planDirectory(t, map[string]string{"observation.json": emptySnapshot})
		result := Run(t.Context(), Request{
			Target: configuration, PlanBytes: []byte(planWith(confirmAction)), PlanDirectory: directory,
			Confirmed: []string{"stop-listener"},
		}, noResolution)
		if result.State != durablerun.ExecutionError || result.Outcome != Refused || result.Reason != environment.reason {
			t.Errorf("%s: expected a refused execution error naming %q, got %+v", name, environment.reason, result)
		}
		if len(result.Actions) != 0 {
			t.Errorf("%s: refused before any action, yet recorded %d", name, len(result.Actions))
		}
		if result.Decision != "" {
			t.Errorf("%s: a refused environment must not be resolved or dialled, yet a decision was reached", name)
		}
	}
}

func TestRunRefusesAPlanItCannotRunAsReviewed(t *testing.T) {
	unreviewed := planWith(`{"id":"wipe","operator":"run_shell","authority":"none","instructions":"rm -rf /var/lib/fixture"}`)
	directory := planDirectory(t, nil)
	result := Run(t.Context(), Request{
		Target: target(closedEndpoint(t)), PlanBytes: []byte(unreviewed), PlanDirectory: directory,
	}, noResolution)
	if result.State != durablerun.ExecutionError || result.Outcome != Refused || result.Reason != PlanRefused {
		t.Fatalf("an unreviewed action must be a named execution error: %+v", result)
	}
	if len(result.Actions) != 0 || result.PlanSHA256 == "" {
		t.Fatalf("a refused plan runs nothing and is still identified by its bytes: %+v", result)
	}
}

func TestRunRefusesAPlanWrittenForAnotherEnvironment(t *testing.T) {
	other := `{"schema":"readmit-reset-plan/v1","environment":"stage-siu","actions":[` + confirmAction + `]}`
	result := Run(t.Context(), Request{
		Target: target(closedEndpoint(t)), PlanBytes: []byte(other), PlanDirectory: planDirectory(t, nil),
		Confirmed: []string{"stop-listener"},
	}, noResolution)
	if result.State != durablerun.ExecutionError || result.Reason != PlanEnvironmentMismatch {
		t.Fatalf("a plan written for another environment must be refused: %+v", result)
	}
}

// TestRunRefusesAnApprovalThatWouldAssertAMachineResult keeps the operator's
// confirmation to the half a person actually performs. Confirming a machine
// action would let somebody hand readmit a result it is supposed to establish.
func TestRunRefusesAnApprovalThatWouldAssertAMachineResult(t *testing.T) {
	document := planWith(confirmAction,
		`{"id":"empty-ledger","operator":"observation_empty","authority":"read_declared_file","instructions":"x","observation":"observation.json"}`)
	directory := planDirectory(t, map[string]string{"observation.json": dirtySnapshot})
	for name, confirmed := range map[string][]string{
		"a machine action": {"stop-listener", "empty-ledger"},
		"an unknown id":    {"stop-listener", "restart-database"},
	} {
		result := Run(t.Context(), Request{
			Target: target(closedEndpoint(t)), PlanBytes: []byte(document), PlanDirectory: directory,
			Confirmed: confirmed,
		}, noResolution)
		if result.State != durablerun.ExecutionError || result.Reason != ApprovalNamesMachineAction {
			t.Errorf("%s: expected a refused execution error, got %+v", name, result)
		}
	}
}

// TestRunRefusesADestinationTheSendDecisionDenies goes through the approved
// -destination rule rather than around it: the one connection a reset may open
// is held to the decision a send is held to, and a reset never gets to open one
// a send would be denied.
func TestRunRefusesADestinationTheSendDecisionDenies(t *testing.T) {
	quietAction := `{"id":"endpoint-quiet","operator":"endpoint_quiet","authority":"connect_approved_target","instructions":"x"}`
	approved := sendpolicy.Policy{Schema: sendpolicy.PolicySchema, ApprovedDestinations: []string{"198.51.100.0/24"}}
	for name, selected := range map[string]struct {
		policy   *sendpolicy.Policy
		address  string
		resolve  sendpolicy.Resolver
		decision sendpolicy.Reason
	}{
		"a remote address with no policy selected":    {nil, "203.0.113.9:2575", noResolution, sendpolicy.PolicyRequired},
		"an address no approved destination contains": {&approved, "203.0.113.9:2575", noResolution, sendpolicy.UnapprovedDestination},
		"a name that does not resolve":                {&approved, "lab.example.invalid:2575", noResolution, sendpolicy.UnresolvableDestination},
		"a name resolving to several addresses": {&approved, "lab.example.invalid:2575", func(context.Context, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("198.51.100.7"), netip.MustParseAddr("198.51.100.8")}, nil
		}, sendpolicy.AmbiguousDestination},
	} {
		configuration := target(selected.address)
		result := Run(t.Context(), Request{
			Target: configuration, PlanBytes: []byte(planWith(quietAction)), PlanDirectory: planDirectory(t, nil),
			Policy: selected.policy,
		}, selected.resolve)
		if result.State != durablerun.ExecutionError || result.Outcome != Refused || result.Reason != DestinationRefused {
			t.Errorf("%s: expected a refused execution error, got %+v", name, result)
		}
		if result.Decision != selected.decision {
			t.Errorf("%s: expected the decision %q, got %q", name, selected.decision, result.Decision)
		}
		if len(result.Actions) != 0 {
			t.Errorf("%s: a denied destination is refused before any connection, yet %d actions ran", name, len(result.Actions))
		}
	}
}

// TestRunReportsAFixtureThatDidNotReset separates what readmit established from
// what it did not: a ledger that still holds a record is a failure, and a file
// it could not read as an observation leaves the reset merely unconfirmed.
// Both are execution errors; neither is a pass.
func TestRunReportsAFixtureThatDidNotReset(t *testing.T) {
	ledger := `{"id":"empty-ledger","operator":"observation_empty","authority":"read_declared_file","instructions":"x","observation":"observation.json"}`
	for name, expected := range map[string]struct {
		files   map[string]string
		outcome Outcome
		reason  Reason
	}{
		"a ledger the fixture did not clear": {map[string]string{"observation.json": dirtySnapshot}, Failed, LedgerNotEmpty},
		"an inconsistent snapshot":           {map[string]string{"observation.json": strings.Replace(emptySnapshot, `"consistent":true`, `"consistent":false`, 1)}, Failed, LedgerNotEmpty},
		"an observation that is not there":   {nil, Unconfirmed, ObservationUnreadable},
		"an observation of another contract": {map[string]string{"observation.json": `{"schema":"readmit-observation/v2"}`}, Unconfirmed, ObservationUnreadable},
		"an observation that is a directory": {nil, Unconfirmed, ObservationUnreadable},
	} {
		directory := planDirectory(t, expected.files)
		if name == "an observation that is a directory" {
			if err := os.Mkdir(filepath.Join(directory, "observation.json"), 0700); err != nil {
				t.Fatal(err)
			}
		}
		result := Run(t.Context(), Request{
			Target: target(closedEndpoint(t)), PlanBytes: []byte(planWith(ledger)), PlanDirectory: directory,
		}, noResolution)
		if result.State != durablerun.ExecutionError || result.Outcome != expected.outcome || result.Reason != expected.reason {
			t.Errorf("%s: expected %s (%s) as an execution error, got %+v", name, expected.outcome, expected.reason, result)
		}
	}
}

func TestRunReportsAnEndpointItCouldNotConfirmIsQuiet(t *testing.T) {
	quietAction := `{"id":"endpoint-quiet","operator":"endpoint_quiet","authority":"connect_approved_target","instructions":"x"}`
	result := Run(t.Context(), Request{
		Target: target(closedEndpoint(t)), PlanBytes: []byte(planWith(quietAction)), PlanDirectory: planDirectory(t, nil),
	}, noResolution)
	if result.State != durablerun.ExecutionError || result.Outcome != Unconfirmed || result.Reason != EndpointNotQuiet {
		t.Fatalf("an endpoint that was not reached leaves the reset unconfirmed: %+v", result)
	}
	if result.Actions[0].Diagnosis != environment.ConnectionRefused {
		t.Fatalf("the outcome must name the transport evidence behind it, got %q", result.Actions[0].Diagnosis)
	}
}

func TestRunRefusesAnEndpointConfigurationItCannotUse(t *testing.T) {
	quietAction := `{"id":"endpoint-quiet","operator":"endpoint_quiet","authority":"connect_approved_target","instructions":"x"}`
	configuration := target("127.0.0.1:2575")
	configuration.MessageTimeout = "not-a-duration"
	result := Run(t.Context(), Request{
		Target: configuration, PlanBytes: []byte(planWith(quietAction)), PlanDirectory: planDirectory(t, nil),
	}, noResolution)
	if result.State != durablerun.ExecutionError || result.Outcome != Refused || result.Reason != EndpointUnusable {
		t.Fatalf("an unusable configuration is a refused execution error: %+v", result)
	}
}

// TestRunStopsAtTheFirstStepItCouldNotConfirm records the rest of a plan as not
// attempted. An action nobody ran is never recorded as one that passed.
func TestRunStopsAtTheFirstStepItCouldNotConfirm(t *testing.T) {
	document := planWith(confirmAction,
		`{"id":"empty-ledger","operator":"observation_empty","authority":"read_declared_file","instructions":"x","observation":"observation.json"}`)
	directory := planDirectory(t, map[string]string{"observation.json": emptySnapshot})
	result := Run(t.Context(), Request{
		Target: target(closedEndpoint(t)), PlanBytes: []byte(document), PlanDirectory: directory,
	}, noResolution)
	if result.Outcome != Unconfirmed || result.Reason != AwaitingOperator || result.State != durablerun.ExecutionError {
		t.Fatalf("an unconfirmed operator step stops the reset: %+v", result)
	}
	if result.Actions[1].Outcome != NotAttempted || result.Actions[1].Reason != EarlierActionStopped {
		t.Fatalf("the remaining action must be recorded as not attempted: %+v", result.Actions[1])
	}
}

func TestRunRecordsCancellationAsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	directory := planDirectory(t, map[string]string{"observation.json": emptySnapshot})
	result := Run(ctx, Request{
		Target: target(closedEndpoint(t)), PlanBytes: []byte(planWith(confirmAction)), PlanDirectory: directory,
		Confirmed: []string{"stop-listener"},
	}, noResolution)
	if result.State != durablerun.Cancelled || result.Outcome != Cancelled || result.Reason != Interrupted {
		t.Fatalf("an interrupted reset is cancellation, not a pass: %+v", result)
	}
	if result.ExitCode() == 0 {
		t.Fatal("a cancelled reset must not exit 0")
	}
}

// TestNoResetOutcomeIsEverAnAssertionFailure is the ticket's second hard rule.
// Only a confirmed reset exits 0, and no reset outcome exits 1, because a
// fixture that did not reset is not evidence that an expectation was wrong.
func TestNoResetOutcomeIsEverAnAssertionFailure(t *testing.T) {
	for _, outcome := range []Outcome{Confirmed, Unconfirmed, Failed, Refused, Cancelled, NotAttempted} {
		settled := settle(Result{Outcome: outcome})
		if settled.State == durablerun.AssertionFailed || settled.ExitCode() == 1 {
			t.Errorf("%s settled as %s with exit code %d", outcome, settled.State, settled.ExitCode())
		}
		if (settled.State == durablerun.Passed) != (outcome == Confirmed) {
			t.Errorf("%s settled as %s; only a confirmed reset is a pass", outcome, settled.State)
		}
	}
}

// TestEncodedOutcomeCarriesItsVersionAndNothingPrivate keeps the retained
// document readable and shareable: closed vocabulary only, and no path, value
// or address from the environment it was produced against.
func TestEncodedOutcomeCarriesItsVersionAndNothingPrivate(t *testing.T) {
	address := endpoint(t)
	document := planWith(confirmAction,
		`{"id":"empty-ledger","operator":"observation_empty","authority":"read_declared_file","instructions":"Look in /srv/fixture/observation.json.","observation":"observation.json"}`)
	directory := planDirectory(t, map[string]string{"observation.json": emptySnapshot})
	result := Run(t.Context(), Request{
		Target: target(address), PlanBytes: []byte(document), PlanDirectory: directory,
		Confirmed: []string{"stop-listener"},
	}, noResolution)
	data, err := EncodeOutcome(result)
	if err != nil {
		t.Fatal(err)
	}
	var header struct {
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(data, &header); err != nil || header.Schema != OutcomeSchema {
		t.Fatalf("the retained outcome must declare %s: %v %q", OutcomeSchema, err, header.Schema)
	}
	for _, private := range []string{address, directory, "observation.json", "/srv/fixture", "Look in"} {
		if strings.Contains(string(data), private) {
			t.Errorf("the retained outcome echoed %q", private)
		}
	}
}
