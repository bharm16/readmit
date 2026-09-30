package expectation

import (
	"encoding/json/v2"
	"errors"
	"strings"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/runnerprotocol"
	"github.com/bharm16/readmit/internal/testisolation"
)

const ConnectedSchema = "readmit-connected-test-release/v1"
const ConnectedReviewSchema = "readmit-connected-test-release-review/v1"

// ConnectedReview identifies the immutable test definition, apart from its
// explicitly environment-specific binding. It grants no execution authority.
type ConnectedReview struct {
	Schema     string `json:"schema"`
	ID         string `json:"id"`
	Revision   string `json:"revision"`
	Definition string `json:"definition"`
}

func (r ConnectedReview) Identity() string { return digest(r) }

// ConnectedRelease is a local approval, never a signature or a passing run.
// Changing an environment needs its own exact promotion and live authority.
type ConnectedRelease struct {
	Schema    string          `json:"schema"`
	Parent    string          `json:"parent"`
	Review    ConnectedReview `json:"review"`
	Reviewed  string          `json:"reviewed"`
	Approver  string          `json:"approver"`
	Rationale string          `json:"rationale"`
}

func (r ConnectedRelease) Identity() string { return digest(r) }

func (r *ConnectedReview) UnmarshalJSON(raw []byte) error {
	type plain ConnectedReview
	if runnerprotocol.Exact(raw, "schema", "id", "revision", "definition") != nil {
		return errors.New("invalid connected release review")
	}
	return json.Unmarshal(raw, (*plain)(r), json.RejectUnknownMembers(true))
}

func (r *ConnectedRelease) UnmarshalJSON(raw []byte) error {
	type plain ConnectedRelease
	if runnerprotocol.Exact(raw, "schema", "parent", "review", "reviewed", "approver", "rationale") != nil {
		return errors.New("invalid connected release")
	}
	return json.Unmarshal(raw, (*plain)(r), json.RejectUnknownMembers(true))
}

func DecodeConnected(raw []byte) (ConnectedRelease, error) {
	var r ConnectedRelease
	if len(raw) > MaxBytes || json.Unmarshal(raw, &r, json.RejectUnknownMembers(true)) != nil || r.Schema != ConnectedSchema ||
		r.Review.Schema != ConnectedReviewSchema || !identifier.MatchString(r.Review.ID) || !connectedText(r.Review.Revision, 256) ||
		!isDigest(r.Review.Definition) || r.Reviewed != r.Review.Identity() || r.Parent != "" && !isDigest(r.Parent) ||
		!connectedText(r.Approver, 256) || !connectedText(r.Rationale, 1024) {
		return ConnectedRelease{}, errors.New("invalid connected release approval")
	}
	return r, nil
}

func connectedText(v string, max int) bool {
	return strings.TrimSpace(v) != "" && len(v) <= max && !strings.ContainsAny(v, "\x00\r\n")
}

// ReviewConnected reads the sealed plan and its own dependencies. The only
// fields excluded from the definition are the declared environment, each
// server's base address and the isolation contract's environment binding.
// Requests, cases, checks, profiles, completion, isolation effects, capability
// baselines, generation and limits remain pinned. No value is resolved.
func ReviewConnected(path string) (ConnectedReview, error) {
	plan, err := connectedtest.OpenFlowPlan(path)
	if err != nil {
		return ConnectedReview{}, err
	}
	d := plan.Document()
	test := d.Test
	test.Environment = connectedtest.Environment{}
	for i := range test.Servers {
		test.Servers[i].Base = ""
	}
	var isolation testisolation.Contract
	if json.Unmarshal(plan.Dependency(test.Isolation), &isolation, json.RejectUnknownMembers(true)) != nil {
		return ConnectedReview{}, errors.New("connected release isolation cannot be verified")
	}
	isolation.Environment, isolation.Revision = "", ""
	test.Isolation.SHA256 = digest(isolation)
	definition := digest(struct {
		Test       connectedtest.FlowTest   `json:"test"`
		Generation connectedtest.Generation `json:"generation"`
	}{test, d.Generation})
	return ConnectedReview{Schema: ConnectedReviewSchema, ID: test.ID, Revision: test.Revision, Definition: definition}, nil
}

// ApproveConnected checks the exact reviewed definition again and publishes
// a new approval; a changed definition cannot inherit the reviewed identity.
func ApproveConnected(path, parent, reviewed, approver, rationale, output string) (ConnectedRelease, error) {
	review, err := ReviewConnected(path)
	if err != nil {
		return ConnectedRelease{}, err
	}
	if parent != "" {
		previousRaw, e := (artifactdir.Document{MaxBytes: MaxBytes}).Read(parent)
		if e != nil {
			return ConnectedRelease{}, e
		}
		previous, e := DecodeConnected(previousRaw)
		if e != nil || previous.Review.ID != review.ID || previous.Review.Revision == review.Revision && previous.Review.Definition != review.Definition {
			return ConnectedRelease{}, errors.New("a connected release cannot reuse a test revision for different expectations")
		}
		parent = previous.Identity()
	}
	r := ConnectedRelease{Schema: ConnectedSchema, Parent: parent, Review: review, Reviewed: reviewed, Approver: approver, Rationale: rationale}
	raw, err := json.Marshal(r, json.Deterministic(true))
	if err != nil {
		return ConnectedRelease{}, err
	}
	if _, err = DecodeConnected(raw); err != nil {
		return ConnectedRelease{}, err
	}
	if err = (artifactdir.Document{MaxBytes: MaxBytes}).Create(output, raw); err != nil {
		return ConnectedRelease{}, err
	}
	return r, nil
}
