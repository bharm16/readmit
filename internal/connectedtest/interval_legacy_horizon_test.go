package connectedtest_test

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/bharm16/readmit/internal/connectedtest"
)

func TestLegacyConnectedPlanHorizonsRetainTheirOneHourContract(t *testing.T) {
	for _, version := range []string{"v1", "v2"} {
		for _, horizon := range []int64{300001, 1800000, 3600000} {
			t.Run(fmt.Sprintf("%s/%d", version, horizon), func(t *testing.T) {
				var d connectedtest.Test
				var files map[string][]byte
				schema := connectedtest.PlanSchema
				if version == "v1" {
					raw, supplied := example(t)
					files = supplied
					if err := json.Unmarshal(raw, &d); err != nil {
						t.Fatal(err)
					}
				} else {
					d, files = intervalDefinition(t)
					d.Schema = connectedtest.TestSchemaV2
					d.Datasets[0].Completion.Kind = "bounded-horizon"
					d.Datasets[0].Completion.Policy = nil
					schema = connectedtest.PlanSchemaV2
				}
				d.Datasets[0].Completion.HorizonMS = horizon
				d.Limits.DeadlineMS = 3600000
				raw, err := json.Marshal(d)
				if err != nil {
					t.Fatal(err)
				}
				plan, err := connectedtest.Compile(raw, files, connectedtest.Generation{BaseTime: "2026-01-01T00:00:00Z"})
				if err != nil {
					t.Fatal("previously valid legacy horizon refused", err)
				}
				path := filepath.Join(t.TempDir(), "plan")
				if err = plan.Write(context.Background(), path); err != nil {
					t.Fatal(err)
				}
				reopened, err := connectedtest.OpenPlan(path)
				if err != nil || reopened.Identity() != plan.Identity() || reopened.Document().Schema != schema || reopened.Document().Test.Datasets[0].Completion.HorizonMS != horizon {
					t.Fatal("legacy horizon changed on reopen", err)
				}
				// Existing schemas still require coverage to fit their runner deadline.
				d.Limits.DeadlineMS = horizon - 1
				raw, _ = json.Marshal(d)
				if _, err = connectedtest.Compile(raw, files, plan.Document().Generation); err == nil {
					t.Fatal("legacy horizon longer than deadline accepted")
				}
			})
		}
	}
}
