package suite

import (
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/connectedtest"
	"github.com/bharm16/readmit/internal/runnerprotocol"
)

const ConnectedPromotionSchema = "readmit-suite-promotion/v2"
const ConnectedPromotionReviewSchema = "readmit-suite-promotion-review/v2"

type ConnectedPromotionReview struct {
	Schema       string                  `json:"schema"`
	Suite        string                  `json:"suite"`
	Environment  string                  `json:"environment"`
	Revision     string                  `json:"revision_assumption"`
	Input        string                  `json:"input"`
	Capabilities string                  `json:"capabilities"`
	Jobs         []CoverageSpecification `json:"jobs"`
}

func (r ConnectedPromotionReview) Identity() string { return identity(r) }

type ConnectedPromotion struct {
	Schema    string                   `json:"schema"`
	Review    ConnectedPromotionReview `json:"review"`
	Reviewed  string                   `json:"reviewed"`
	Approver  string                   `json:"approver"`
	Rationale string                   `json:"rationale"`
}

func (p ConnectedPromotion) Identity() string { return identity(p) }

func (r *ConnectedPromotionReview) UnmarshalJSON(raw []byte) error {
	type plain ConnectedPromotionReview
	return required(raw, (*plain)(r), "schema", "suite", "environment", "revision_assumption", "input", "capabilities", "jobs")
}

func (r *ConnectedPromotion) UnmarshalJSON(raw []byte) error {
	type plain ConnectedPromotion
	return required(raw, (*plain)(r), "schema", "review", "reviewed", "approver", "rationale")
}

func DecodeConnectedPromotion(raw []byte) (ConnectedPromotion, error) {
	var p ConnectedPromotion
	if len(raw) > MaxBytes || json.Unmarshal(raw, &p, json.RejectUnknownMembers(true)) != nil || p.Schema != ConnectedPromotionSchema ||
		p.Review.Schema != ConnectedPromotionReviewSchema || !validDigest(p.Review.Suite) || !validDigest(p.Review.Input) || !validDigest(p.Review.Capabilities) ||
		!identifier.MatchString(p.Review.Environment) || !text(p.Review.Revision, 256) || len(p.Review.Jobs) < 1 || len(p.Review.Jobs) > 64 ||
		p.Review.Identity() != p.Reviewed || !text(p.Approver, 256) || !text(p.Rationale, 1024) {
		return ConnectedPromotion{}, errors.New("invalid connected suite promotion")
	}
	seen := map[string]bool{}
	for _, job := range p.Review.Jobs {
		if !identifier.MatchString(job.Job) || !validDigest(job.SHA256) || seen[job.Job] {
			return ConnectedPromotion{}, errors.New("invalid connected promotion job pin")
		}
		seen[job.Job] = true
	}
	return p, nil
}

func (p *ConnectedPrepared) PromotionReview(environment, revision string) (ConnectedPromotionReview, error) {
	_, metadata, _, _, err := openConnectedPreparation(p.Directory)
	if err != nil || metadata.Environment != environment || !text(revision, 256) {
		return ConnectedPromotionReview{}, errors.New("invalid connected promotion environment or target assumption")
	}
	capability, err := p.Capabilities.Identity()
	if err != nil {
		return ConnectedPromotionReview{}, err
	}
	review := ConnectedPromotionReview{Schema: ConnectedPromotionReviewSchema, Suite: metadata.Suite, Environment: environment, Revision: revision, Input: p.Identity, Capabilities: capability, Jobs: []CoverageSpecification{}}
	for _, job := range p.Queue.Jobs {
		review.Jobs = append(review.Jobs, CoverageSpecification{Job: job.ID, SHA256: job.Input})
	}
	return review, nil
}

func ReviewConnectedPromotion(path, environment, revision string) (ConnectedPromotionReview, error) {
	dir, err := os.MkdirTemp("", "readmit-connected-promotion-")
	if err != nil {
		return ConnectedPromotionReview{}, err
	}
	defer os.RemoveAll(dir)
	p, err := PrepareConnected(ConnectedRequest{Path: path, Environment: environment, Output: filepath.Join(dir, "prepared")})
	if err != nil {
		return ConnectedPromotionReview{}, err
	}
	return p.PromotionReview(environment, revision)
}

func ApproveConnectedPromotion(path, environment, revision, reviewed, approver, rationale, output string) (ConnectedPromotion, error) {
	review, err := ReviewConnectedPromotion(path, environment, revision)
	if err != nil {
		return ConnectedPromotion{}, err
	}
	p := ConnectedPromotion{Schema: ConnectedPromotionSchema, Review: review, Reviewed: reviewed, Approver: approver, Rationale: rationale}
	raw, err := json.Marshal(p, json.Deterministic(true))
	if err != nil {
		return ConnectedPromotion{}, err
	}
	if _, err = DecodeConnectedPromotion(raw); err != nil {
		return ConnectedPromotion{}, err
	}
	if err = (artifactdir.Document{MaxBytes: MaxBytes}).Create(output, raw); err != nil {
		return ConnectedPromotion{}, err
	}
	return p, nil
}

// VerifyPromotion checks an externally installed approval and its exact pin
// against this expansion. Neither a passing run nor a pipeline can approve it.
func (p *ConnectedPrepared) VerifyPromotion(path, pin, environment, revision string) (ConnectedPromotion, error) {
	raw, err := read(path, MaxBytes)
	if err != nil {
		return ConnectedPromotion{}, err
	}
	promotion, err := DecodeConnectedPromotion(raw)
	if err != nil || promotion.Identity() != pin {
		return ConnectedPromotion{}, errors.New("connected promotion differs from its approved identity")
	}
	review, err := p.PromotionReview(environment, revision)
	if err != nil || identity(review) != identity(promotion.Review) {
		return ConnectedPromotion{}, errors.New("connected suite inputs changed since promotion approval")
	}
	return promotion, nil
}

// RuntimeScope is the actual scope every selected plan compiles against. The
// suite's named environment is an alias, not permission to route elsewhere.
func (p *ConnectedPrepared) RuntimeScope() (project, environment string, err error) {
	for _, job := range p.Queue.Jobs {
		plan, e := connectedtest.OpenFlowPlan(filepath.Join(p.Directory, job.Plan))
		if e != nil {
			return "", "", e
		}
		scope := plan.Document().Test.Environment
		if project != "" && (scope.Project != project || scope.ID != environment) {
			return "", "", errors.New("one customer runner cannot serve several declared execution scopes")
		}
		project, environment = scope.Project, scope.ID
	}
	if !runnerprotocol.ID(project) || !runnerprotocol.ID(environment) {
		return "", "", errors.New("connected suite has no execution scope")
	}
	return project, environment, nil
}
