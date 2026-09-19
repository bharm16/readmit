package runqueue

import "testing"

const oneJob = `{"schema":"readmit-run-queue/v1","parallelism":2,"jobs":[` +
	`{"id":"setup","spec":"setup.json","isolation":"shared"},` +
	`{"id":"booking","spec":"specs/booking.json","isolation":"shared","after":["setup"]}]}`

func TestDecodePlanReadsAQueueAsWritten(t *testing.T) {
	plan, err := DecodePlan([]byte(oneJob))
	if err != nil {
		t.Fatal(err)
	}
	if plan.Schema != PlanSchema || plan.Parallelism != 2 || len(plan.Jobs) != 2 {
		t.Fatalf("%+v", plan)
	}
	if plan.Jobs[0].ID != "setup" || plan.Jobs[0].Isolation != SharedState || len(plan.Jobs[0].After) != 0 {
		t.Fatalf("%+v", plan.Jobs[0])
	}
	if plan.Jobs[1].Spec != "specs/booking.json" || len(plan.Jobs[1].After) != 1 || plan.Jobs[1].After[0] != "setup" {
		t.Fatalf("%+v", plan.Jobs[1])
	}
}

// TestDecodePlanRefusesEveryQueueItCannotSchedule is the negative half of the
// contract. State isolation is never assumed, a document authored against a
// later contract is never read as though this one allowed it, and an order no
// job could ever start from is refused whole rather than partly executed.
func TestDecodePlanRefusesEveryQueueItCannotSchedule(t *testing.T) {
	for name, document := range map[string]string{
		"no schema":            `{"parallelism":1,"jobs":[{"id":"a","spec":"a.json","isolation":"shared"}]}`,
		"another schema":       `{"schema":"readmit-run-queue/v2","parallelism":1,"jobs":[{"id":"a","spec":"a.json","isolation":"shared"}]}`,
		"unknown member":       `{"schema":"readmit-run-queue/v1","parallelism":1,"retry":true,"jobs":[{"id":"a","spec":"a.json","isolation":"shared"}]}`,
		"unknown job member":   `{"schema":"readmit-run-queue/v1","parallelism":1,"jobs":[{"id":"a","spec":"a.json","isolation":"shared","command":"./reset.sh"}]}`,
		"no isolation":         `{"schema":"readmit-run-queue/v1","parallelism":1,"jobs":[{"id":"a","spec":"a.json"}]}`,
		"unknown isolation":    `{"schema":"readmit-run-queue/v1","parallelism":1,"jobs":[{"id":"a","spec":"a.json","isolation":"maybe"}]}`,
		"no jobs":              `{"schema":"readmit-run-queue/v1","parallelism":1,"jobs":[]}`,
		"no parallelism":       `{"schema":"readmit-run-queue/v1","jobs":[{"id":"a","spec":"a.json","isolation":"shared"}]}`,
		"negative parallelism": `{"schema":"readmit-run-queue/v1","parallelism":-1,"jobs":[{"id":"a","spec":"a.json","isolation":"shared"}]}`,
		"unbounded":            `{"schema":"readmit-run-queue/v1","parallelism":17,"jobs":[{"id":"a","spec":"a.json","isolation":"shared"}]}`,
		"repeated id":          `{"schema":"readmit-run-queue/v1","parallelism":1,"jobs":[{"id":"a","spec":"a.json","isolation":"shared"},{"id":"a","spec":"b.json","isolation":"shared"}]}`,
		"unreadable id":        `{"schema":"readmit-run-queue/v1","parallelism":1,"jobs":[{"id":"A/b","spec":"a.json","isolation":"shared"}]}`,
		"absolute spec":        `{"schema":"readmit-run-queue/v1","parallelism":1,"jobs":[{"id":"a","spec":"/etc/spec.json","isolation":"shared"}]}`,
		"escaping spec":        `{"schema":"readmit-run-queue/v1","parallelism":1,"jobs":[{"id":"a","spec":"../spec.json","isolation":"shared"}]}`,
		"undeclared after":     `{"schema":"readmit-run-queue/v1","parallelism":1,"jobs":[{"id":"a","spec":"a.json","isolation":"shared","after":["b"]}]}`,
		"self dependency":      `{"schema":"readmit-run-queue/v1","parallelism":1,"jobs":[{"id":"a","spec":"a.json","isolation":"shared","after":["a"]}]}`,
		"repeated dependency":  `{"schema":"readmit-run-queue/v1","parallelism":1,"jobs":[{"id":"a","spec":"a.json","isolation":"shared"},{"id":"b","spec":"b.json","isolation":"shared","after":["a","a"]}]}`,
		"cycle":                `{"schema":"readmit-run-queue/v1","parallelism":1,"jobs":[{"id":"a","spec":"a.json","isolation":"shared","after":["b"]},{"id":"b","spec":"b.json","isolation":"shared","after":["a"]}]}`,
	} {
		if _, err := DecodePlan([]byte(document)); err == nil {
			t.Errorf("%s was read as a schedulable queue", name)
		}
	}
	oversized := make([]byte, MaxPlanBytes+1)
	if _, err := DecodePlan(oversized); err == nil {
		t.Error("an oversized queue was read")
	}
}
