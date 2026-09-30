package connectedrun_test

import (
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/assertion"
	"github.com/bharm16/readmit/internal/connectedlab"
	"github.com/bharm16/readmit/internal/connectedrun"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/testisolation"
)

func TestRetainedFlowInputVerifiesRealFHIRProofAfterOriginalInputsDisappear(t *testing.T) {
	h := connectedlab.New(t, "")
	h.Lab.SetDownstream("encounter")
	h.Compile(nativeBooking(h))
	p := h.Prepare("actual-input")
	snapshot, err := p.InputSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	registry, err := p.RegistrySnapshot()
	if err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(h.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	result, output := h.Run("actual-input")
	if result.State != "complete" || result.Verdict != assertion.VerdictPass || h.Lab.Creates.Load() != 3 {
		t.Fatal("genuine target proof did not pass", result.State, result.Verdict)
	}
	if err := connectedrun.VerifyFlowInput(t.Context(), output, snapshot, config, "actual-input", registry); err != nil {
		t.Fatal("actual runtime inputs did not verify", err)
	}
	// A second genuine preparation uses a different separately registered
	// credential generation, with the same immutable authored plan. Its valid
	// snapshot must not qualify the old executed lifecycle.
	var otherRegistry testisolation.Registry
	if err := json.Unmarshal(registry, &otherRegistry); err != nil {
		t.Fatal(err)
	}
	otherRegistry.Adapters[0].Read.Reference.Generation = "2"
	changedRegistry, _ := json.Marshal(otherRegistry, json.Deterministic(true))
	registryPath := filepath.Join(h.Root, "registry.json")
	if err := os.WriteFile(registryPath, changedRegistry, 0600); err != nil {
		t.Fatal(err)
	}
	other, err := connectedrun.PrepareFlow(h.PlanPath, h.ConfigPath, "other-input")
	if err != nil {
		t.Fatal("second actual configuration did not prepare", err)
	}
	otherSnapshot, err := other.InputSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if err := connectedrun.VerifyFlowInput(t.Context(), output, otherSnapshot, config, "actual-input", changedRegistry); !errors.Is(err, connectedrun.ErrFlowInput) {
		t.Fatal("old genuine lifecycle qualified different registered credentials", err)
	}
	for _, original := range []string{h.PlanPath, h.ConfigPath, filepath.Join(h.Root, "registry.json"), filepath.Join(h.Root, "policy.json")} {
		if err := os.RemoveAll(original); err != nil {
			t.Fatal(err)
		}
	}
	h.Lab.Server().Close()
	if err := connectedrun.VerifyFlowInput(t.Context(), output, snapshot, config, "actual-input", registry); err != nil {
		t.Fatal("offline verifier depended on originals", err)
	}
	if err := connectedrun.VerifyFlowInput(t.Context(), output, snapshot, config, "another-occurrence", registry); !errors.Is(err, connectedrun.ErrFlowInput) {
		t.Fatal("a different occurrence's proof was accepted", err)
	}
	var input struct {
		Plan          string                           `json:"plan"`
		Configuration string                           `json:"configuration"`
		Bindings      map[string]networkaction.Binding `json:"bindings"`
		Sources       map[string]string                `json:"sources"`
		Registry      string                           `json:"isolation_registry"`
		Policy        string                           `json:"isolation_policy"`
		Validation    string                           `json:"validation"`
	}
	if err := json.Unmarshal(snapshot, &input); err != nil {
		t.Fatal(err)
	}
	for key, binding := range input.Bindings {
		if key == "book:step:create" {
			binding.Credentials = networkaction.Digest([]byte("different-authorized-role"))
			input.Bindings[key] = binding
		}
	}
	transplanted, _ := json.Marshal(input, json.Deterministic(true))
	if err := connectedrun.VerifyFlowInput(t.Context(), output, transplanted, config, "actual-input", registry); !errors.Is(err, connectedrun.ErrFlowInput) {
		t.Fatal("actual proof transplanted into different approved credentials", err)
	}
}
