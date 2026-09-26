package cli

import (
	"strings"
	"testing"
)

func TestConnectedCommandsRequireExplicitGenerationAndSend(t *testing.T) {
	for _, args := range [][]string{{"connected", "prepare", "missing", "output"}, {"connected", "convert", "missing", "checks", "output"}, {"connected", "run", "missing", "legacy"}} {
		out, _, err := runInProcess(t, args...)
		if err == nil || out.Len() != 0 {
			t.Fatal("incomplete invocation started connected work")
		}
		if !strings.Contains(err.Error(), "requires") {
			t.Fatalf("expected explicit parameter refusal: %v", err)
		}
	}
}
