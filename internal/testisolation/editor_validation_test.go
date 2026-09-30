package testisolation_test

import (
	"os"
	"testing"

	"github.com/bharm16/readmit/internal/testisolation"
)

func TestEditorValidationUsesIsolationCompilerWithoutTargetEffects(t *testing.T) {
	h := fixture(t, "reserved-namespace")
	registry, err := os.ReadFile(h.registry)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := os.ReadFile(h.policy)
	if err != nil {
		t.Fatal(err)
	}
	if err := testisolation.ValidateConfiguration(encode(h.contract), registry, policy, h.options); err != nil {
		t.Fatal(err)
	}
	h.target.mu.Lock()
	requests := h.target.requests
	h.target.mu.Unlock()
	if requests != 0 {
		t.Fatal("editor validation reached the target")
	}
	h.contract.Resources[0].Attributes["unregistered"] = "value"
	if err := testisolation.ValidateConfiguration(encode(h.contract), registry, policy, h.options); err == nil {
		t.Fatal("editor accepted a field the registered template does not authorize")
	}
}
