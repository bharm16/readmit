package connectedrun_test

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/connectedlab"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/engine"
	"github.com/bharm16/readmit/internal/fhirvalidator"
	"github.com/bharm16/readmit/internal/localprofile"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/profileeval"
	"github.com/bharm16/readmit/internal/profilepack"
	"github.com/bharm16/readmit/internal/runnerprotocol"
)

func TestFlowAdmissionInputPinsExcludeExecutionInstanceAndGrantValues(t *testing.T) {
	h := newFlowContractHarnessWithSource(t, true)
	a, err := connectedrun.PrepareFlow(h.planPath, h.configPath, "first-instance")
	if err != nil {
		t.Fatal(err)
	}
	b, err := connectedrun.PrepareFlow(h.planPath, h.configPath, "second-instance")
	if err != nil {
		t.Fatal(err)
	}
	first, err := a.InputIdentity()
	if err != nil || !networkaction.ValidDigest(first) {
		t.Fatal("input metadata was unavailable", err)
	}
	second, err := b.InputIdentity()
	if err != nil || first != second {
		t.Fatal("execution instance changed reviewed input pin", err)
	}
	if err = os.WriteFile(filepath.Join(h.root, "read-isolation-grant.json"), []byte("grant revoked"), 0600); err != nil {
		t.Fatal(err)
	}
	afterGrant, err := a.InputIdentity()
	if err != nil || first != afterGrant {
		t.Fatal("grant value entered read-only input metadata", err)
	}
	h.fixture.mu.Lock()
	defer h.fixture.mu.Unlock()
	if h.fixture.requests != 0 || h.fixture.snapshotReads.Load() != 0 || h.fixture.target.received.Load() != 0 {
		t.Fatal("input metadata contacted a target")
	}
}

func TestFlowAdmissionRequiredValidationRefusesBeforeStimulus(t *testing.T) {
	for _, mode := range []string{"missing", "none"} {
		t.Run(mode, func(t *testing.T) {
			h := connectedlab.New(t, "")
			if mode == "none" {
				h.EnableValidation("")
				h.Validation.Engine = "none"
			}
			flow := nativeBooking(h)
			flow.Phases[0].Validations = []connectedtest.ValidationCheck{connectedlab.Validate("returned-resource", "create", 2000)}
			h.Compile(flow)
			p, err := connectedrun.PrepareFlow(h.PlanPath, h.ConfigPath, "required-validation")
			if err != nil {
				t.Fatal(err)
			}
			_, err = p.Capabilities()
			var status fhirvalidator.Status
			if !errors.As(err, &status) || status.State != "worker-missing" || status.Requirement == "" {
				t.Fatal("required validation disappeared from admission", err)
			}
			err = p.CheckCapabilities(t.Context())
			if !errors.As(err, &status) || status.State != "worker-missing" {
				t.Fatal("required validator was not refused before setup", err)
			}
			if h.Lab.Writes.Load() != 0 || h.Lab.Creates.Load() != 0 || h.Lab.Tokens.Load() != 0 {
				t.Fatal("validator refusal performed target or credential effects")
			}
		})
	}
}

func TestFlowAdmissionCapabilitiesNameOnlySelectedContractsAndCollectors(t *testing.T) {
	h := newFlowContractHarnessWithSource(t, true)
	p, err := connectedrun.PrepareFlow(h.planPath, h.configPath, "capability-instance")
	if err != nil {
		t.Fatal(err)
	}
	c, err := p.Capabilities()
	if err != nil || c.Validate() != nil || c.Schema != runnerprotocol.CapabilitiesSchema || c.Engine != engine.Version() {
		t.Fatal("selected capability metadata was invalid", err)
	}
	for _, pin := range []runnerprotocol.CapabilityPin{
		{Kind: "contract", ID: "readmit-execution-plan", Version: "v4"},
		{Kind: "contract", ID: "readmit-connected-phase-plan", Version: "v1"},
		{Kind: "contract", ID: "readmit-dataset-assertion-set", Version: "v1"},
		{Kind: "contract", ID: "readmit-observation-source", Version: "v1"},
		{Kind: "collector", ID: "http-api", Version: engine.Version()},
	} {
		if !slices.Contains(c.Pins, pin) {
			t.Errorf("missing selected capability %+v", pin)
		}
	}
	for _, pin := range c.Pins {
		if pin.Kind == "smart" || pin.Kind == "validator" || pin.Kind == "validator-worker" || pin.ID == "fhir-resources" || pin.ID == "postgresql" {
			t.Errorf("unselected capability advertised: %+v", pin)
		}
	}
	h.fixture.mu.Lock()
	defer h.fixture.mu.Unlock()
	if h.fixture.requests != 0 || h.fixture.snapshotReads.Load() != 0 || h.fixture.target.received.Load() != 0 {
		t.Fatal("capability metadata contacted a target")
	}
}

func TestFlowAdmissionFHIRCapabilitiesPinSelectedSMARTWithoutRequiringUnusedValidator(t *testing.T) {
	h := connectedlab.New(t, "")
	h.EnableSMART()
	h.Compile(nativeBooking(h))
	p, err := connectedrun.PrepareFlow(h.PlanPath, h.ConfigPath, "smart-admission")
	if err != nil {
		t.Fatal(err)
	}
	c, err := p.Capabilities()
	if err != nil {
		t.Fatal(err)
	}
	for _, pin := range []runnerprotocol.CapabilityPin{
		{Kind: "contract", ID: "readmit-execution-plan", Version: "v5"},
		{Kind: "contract", ID: "readmit-connected-phase-plan", Version: "v2"},
		{Kind: "collector", ID: "fhir-resources", Version: engine.Version()},
		{Kind: "smart", ID: "backend-services/ES384", Version: "2.2"},
	} {
		if !slices.Contains(c.Pins, pin) {
			t.Errorf("missing selected FHIR capability %+v", pin)
		}
	}
	if err = p.CheckCapabilities(t.Context()); err != nil {
		t.Fatal("unrequested validator blocked a FHIR suite", err)
	}
	if h.Lab.Writes.Load() != 0 || h.Lab.Tokens.Load() != 0 {
		t.Fatal("FHIR metadata resolved credentials or changed target state")
	}
}

func TestFlowAdmissionValidatorDescriptorDoesNotProveInstalledWorker(t *testing.T) {
	h := connectedlab.New(t, "")
	h.EnableValidation(filepath.Join(t.TempDir(), "missing-engine.sock"))
	flow := nativeBooking(h)
	flow.Phases[0].Validations = []connectedtest.ValidationCheck{connectedlab.Validate("returned-resource", "create", 2000)}
	h.Compile(flow)
	p, err := connectedrun.PrepareFlow(h.PlanPath, h.ConfigPath, "validator-admission")
	if err != nil {
		t.Fatal(err)
	}
	c, err := p.Capabilities()
	if err != nil {
		t.Fatal("qualified pinned descriptor was not available", err)
	}
	for _, pin := range []runnerprotocol.CapabilityPin{
		{Kind: "validator", ID: "hl7-validator", Version: "6.10.4", SHA256: "1106b9d58f9e363e47bea7c4fc065841e5fc91fe9d062775c3bfdd212bd653cc"},
		{Kind: "profile", ID: "hl7.fhir.r4.core", Version: "4.0.1", SHA256: "ebd7731df7d36b5b7d39d5fb6c9d77b44bb7fe5742f1a2e87f164738c3289d44"},
		{Kind: "validator-worker", ID: "readmit-fhir-worker", Version: "v1", SHA256: strings.TrimPrefix(connectedlab.ValidatorImage, "sha256:")},
	} {
		if !slices.Contains(c.Pins, pin) {
			t.Errorf("missing exact selected validator dependency %+v", pin)
		}
	}
	err = p.CheckCapabilities(t.Context())
	var status fhirvalidator.Status
	if !errors.As(err, &status) || status.State != "worker-missing" || status.Requirement == "" {
		t.Fatal("descriptor falsely proved an installed worker", err)
	}
	if h.Lab.Writes.Load() != 0 || h.Lab.Creates.Load() != 0 || h.Lab.Tokens.Load() != 0 {
		t.Fatal("worker refusal reached the target or credential provider")
	}
}

func TestFlowAdmissionResourceDomainsDoNotChangeWithAuthoredPlanOrOccurrence(t *testing.T) {
	h := newFlowContractHarnessWithSource(t, true)
	p, err := connectedrun.PrepareFlow(h.planPath, h.configPath, "resource-instance")
	if err != nil {
		t.Fatal(err)
	}
	first := p.Resources()
	if len(first) == 0 {
		t.Fatal("connected suite had no state domains")
	}
	for _, resource := range first {
		if !networkaction.ValidDigest(resource) {
			t.Fatal("a resource value escaped its digest")
		}
	}
	d, files := flowWireDependencies(t, h)
	d.ID = "another-authored-test"
	var config connectedrun.FlowConfig
	flowContractRead(t, h.configPath, &config)
	flowWireInstall(t, h, d, files, config)
	other, err := connectedrun.PrepareFlow(h.planPath, h.configPath, "another-occurrence")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(first, other.Resources()) {
		t.Fatal("different test or occurrence evaded an existing target domain")
	}
}

func TestFlowAdmissionSnapshotRetainsExactConfigurationAndDetectsDrift(t *testing.T) {
	h := newFlowContractHarnessWithSource(t, true)
	p, err := connectedrun.PrepareFlow(h.planPath, h.configPath, "snapshot-instance")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := p.InputSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	identity, err := p.InputIdentity()
	if err != nil || identity != networkaction.Digest(snapshot) {
		t.Fatal("offline metadata did not identify the admitted input", err)
	}
	raw, err := os.ReadFile(h.configPath)
	if err != nil || os.WriteFile(h.configPath, append(raw, '\n'), 0600) != nil {
		t.Fatal("could not change selected configuration", err)
	}
	if _, err = p.InputSnapshot(); err == nil {
		t.Fatal("prepared metadata accepted rewritten configuration bytes")
	}
	fresh, err := connectedrun.PrepareFlow(h.planPath, h.configPath, "snapshot-instance")
	if err != nil {
		t.Fatal(err)
	}
	changed, err := fresh.InputIdentity()
	if err != nil || changed == identity {
		t.Fatal("exact configuration change did not require a new approved pin", err)
	}
}

func TestFlowAdmissionProfilePinsUseTheSelectedProfileAndPackOperator(t *testing.T) {
	h := newFlowContractHarnessWithSource(t, true)
	d, files := flowWireDependencies(t, h)
	raw, err := os.ReadFile("../../testdata/fixtures/local-profile.json")
	if err != nil {
		t.Fatal(err)
	}
	profile, err := localprofile.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = json.Marshal(profileeval.ProfileV3{Schema: profileeval.ProfileSchemaV3, Definition: profile})
	if err != nil {
		t.Fatal(err)
	}
	d.Profiles = append(d.Profiles, flowWireReference(t, files, "selected-profile", profileeval.ProfileSchemaV3, jsontext.Value(raw)))
	raw, err = os.ReadFile("../../testdata/fixtures/profile-pack.json")
	if err != nil {
		t.Fatal(err)
	}
	d.Profiles = append(d.Profiles, flowWireReference(t, files, "selected-pack", profilepack.Schema, jsontext.Value(raw)))
	var config connectedrun.FlowConfig
	flowContractRead(t, h.configPath, &config)
	flowWireInstall(t, h, d, files, config)
	p, err := connectedrun.PrepareFlow(h.planPath, h.configPath, "profile-pins")
	if err != nil {
		t.Fatal(err)
	}
	c, err := p.Capabilities()
	if err != nil {
		t.Fatal(err)
	}
	want := runnerprotocol.CapabilityPin{Kind: "contract", ID: "readmit-profile-evaluator", Version: "v2"}
	if !slices.Contains(c.Pins, want) {
		t.Fatal("component profile paired with legacy pack lost its selected operator")
	}
	if slices.Contains(c.Pins, runnerprotocol.CapabilityPin{Kind: "contract", ID: "readmit-profile-evaluator", Version: "v1"}) {
		t.Fatal("unselected evaluator version entered the capability requirement")
	}
}
