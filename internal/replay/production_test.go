package replay_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/replay"
)

// productionTarget is one environment an operator recorded as production.
func productionTarget() replay.Target {
	config := target("127.0.0.1:2575")
	config.Schema, config.Name, config.Classification = replay.TargetSchemaV3, "prod-siu", replay.Production
	return config
}

// A configuration recording the production class is refused before a plan
// exists, so every path that holds a plan is refused with it: no command has to
// remember to ask, and nothing is created for a replay that cannot happen.
func TestPrepareRefusesAProductionClassifiedEnvironment(t *testing.T) {
	source := caseAt(t, request("BOOK"))
	config := productionTarget()
	plan, err := replay.Prepare(source, config, replay.Options{})
	if err == nil {
		t.Fatalf("prepared a plan for %d messages against a production-classified environment", plan.Count())
	}
	if !strings.Contains(err.Error(), "production-classified environment") {
		t.Fatalf("refused without naming the class: %v", err)
	}
	// The same configuration with a class that refuses nothing by itself
	// prepares, so the refusal is the recorded class and not the rest of it.
	for _, recorded := range []replay.Classification{replay.Nonproduction, replay.Unclassified} {
		config.Classification = recorded
		if _, err := replay.Prepare(source, config, replay.Options{}); err != nil {
			t.Fatalf("a %s environment was refused by the class rule: %v", recorded, err)
		}
	}
}

// The refusal survives the reader an operator's own file goes through, so it is
// not a property of one in-memory value a caller happened to build.
func TestReadTargetThenPrepareRefusesProduction(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "prod.json")
	if err := replay.WriteTarget(path, productionTarget()); err != nil {
		t.Fatal(err)
	}
	config, err := replay.ReadTarget(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := replay.Prepare(caseAt(t, request("BOOK")), config, replay.Options{}); err == nil {
		t.Fatal("a production-classified configuration read from a file prepared a replay")
	}
	// Without a plan there is nothing to execute, so no run directory is
	// reserved for a replay that was refused before it was planned.
	if _, err := replay.Send(context.Background(), nil, filepath.Join(directory, "run"), replay.SendOptions{}); err == nil {
		t.Fatal("executed without a plan")
	}
}
