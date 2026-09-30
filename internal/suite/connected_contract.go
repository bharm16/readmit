package suite

import (
	"encoding/json/v2"
	"errors"

	"github.com/bharm16/readmit/internal/runqueue"
)

// ConnectedSchema is a separate suite language over pinned connected plans.
// The v1 reader and its templates retain their exact historical meaning.
const ConnectedSchema = "readmit-suite/v2"

type ConnectedDocument struct {
	Schema       string                 `json:"schema"`
	ID           string                 `json:"id"`
	Owner        string                 `json:"owner"`
	Tags         []string               `json:"tags"`
	Parallelism  int                    `json:"parallelism"`
	Tests        []ConnectedTest        `json:"tests"`
	Environments []ConnectedEnvironment `json:"environments"`
}

// ConnectedTest pins immutable authored expectations separately from each
// environment's compiled plan and customer-local runtime configuration.
type ConnectedTest struct {
	ID              string   `json:"id"`
	Revision        string   `json:"revision"`
	Definition      string   `json:"definition"`
	Release         string   `json:"release"`
	ReleaseIdentity string   `json:"release_identity"`
	After           []string `json:"after"`
	State           string   `json:"state"`
}

type ConnectedEnvironment struct {
	ID       string             `json:"id"`
	Bindings []ConnectedBinding `json:"bindings"`
}

type ConnectedBinding struct {
	Test         string `json:"test"`
	Plan         string `json:"plan"`
	PlanIdentity string `json:"plan_identity"`
	Config       string `json:"config"`
}

func (v *ConnectedDocument) UnmarshalJSON(raw []byte) error {
	type plain ConnectedDocument
	return required(raw, (*plain)(v), "schema", "id", "owner", "tags", "parallelism", "tests", "environments")
}

func (v *ConnectedTest) UnmarshalJSON(raw []byte) error {
	type plain ConnectedTest
	return required(raw, (*plain)(v), "id", "revision", "definition", "release", "release_identity", "after", "state")
}

func (v *ConnectedEnvironment) UnmarshalJSON(raw []byte) error {
	type plain ConnectedEnvironment
	return required(raw, (*plain)(v), "id", "bindings")
}

func (v *ConnectedBinding) UnmarshalJSON(raw []byte) error {
	type plain ConnectedBinding
	return required(raw, (*plain)(v), "test", "plan", "plan_identity", "config")
}

func DecodeConnected(raw []byte) (ConnectedDocument, error) {
	var d ConnectedDocument
	if len(raw) > MaxBytes || json.Unmarshal(raw, &d, json.RejectUnknownMembers(true)) != nil || d.Schema != ConnectedSchema ||
		!identifier.MatchString(d.ID) || !text(d.Owner, 256) || !tags(d.Tags) || len(d.Tests) < 1 || len(d.Tests) > 64 || len(d.Environments) < 1 || len(d.Environments) > 32 {
		return ConnectedDocument{}, errors.New("invalid connected suite declarations")
	}
	declared := map[string]bool{}
	queue := runqueue.Plan{Schema: runqueue.PlanSchema, Parallelism: d.Parallelism}
	for _, test := range d.Tests {
		if !identifier.MatchString(test.ID) || declared[test.ID] || !text(test.Revision, 256) || !validDigest(test.Definition) ||
			!local(test.Release) || !validDigest(test.ReleaseIdentity) || test.After == nil || !connectedState(test.State) {
			return ConnectedDocument{}, errors.New("invalid connected suite test pin")
		}
		declared[test.ID] = true
		queue.Jobs = append(queue.Jobs, runqueue.Job{ID: test.ID, Spec: test.ID + ".json", Isolation: runqueue.SharedState, After: test.After})
	}
	encoded, err := json.Marshal(queue)
	if err != nil {
		return ConnectedDocument{}, err
	}
	// The existing scheduler owns job bounds, order and dependency validation.
	if _, err = runqueue.DecodePlan(encoded); err != nil {
		return ConnectedDocument{}, err
	}
	environments := map[string]bool{}
	for _, environment := range d.Environments {
		if !identifier.MatchString(environment.ID) || environments[environment.ID] || len(environment.Bindings) != len(d.Tests) {
			return ConnectedDocument{}, errors.New("invalid connected suite environment")
		}
		environments[environment.ID] = true
		bound := map[string]bool{}
		for _, binding := range environment.Bindings {
			if !declared[binding.Test] || bound[binding.Test] || !local(binding.Plan) || !local(binding.Config) || !validDigest(binding.PlanIdentity) {
				return ConnectedDocument{}, errors.New("invalid connected suite environment binding")
			}
			bound[binding.Test] = true
		}
	}
	return d, nil
}

func connectedState(state string) bool {
	switch state {
	case "enabled", "blocked", "skipped", "quarantined", "disabled", "unsupported":
		return true
	}
	return false
}
