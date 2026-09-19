package suite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/durablerun"
	"github.com/bharm16/readmit/internal/runqueue"
)

const PromotionSchema = "readmit-suite-promotion/v1"
const PromotionReviewSchema = "readmit-suite-promotion-review/v1"

// PromotionReview commits the whole suite (including isolation/dependencies and
// row parameters), template approvals and each environment's actual input pins.
// Revision is an operator assertion about target software, never a live probe.
type PromotionReview struct {
	Schema      string                  `json:"schema"`
	Commitment  string                  `json:"identity"`
	Suite       string                  `json:"suite_sha256"`
	Releases    string                  `json:"releases_sha256"`
	Environment string                  `json:"environment"`
	Revision    string                  `json:"revision_assumption"`
	Jobs        []CoverageSpecification `json:"jobs"`
}
type Promotion struct {
	Schema    string          `json:"schema"`
	Review    PromotionReview `json:"review"`
	Reviewed  string          `json:"reviewed"`
	Approver  string          `json:"approver"`
	Rationale string          `json:"rationale"`
}

func (v *PromotionReview) UnmarshalJSON(raw []byte) error {
	type plain PromotionReview
	return required(raw, (*plain)(v), "schema", "identity", "suite_sha256", "releases_sha256", "environment", "revision_assumption", "jobs")
}
func (v *Promotion) UnmarshalJSON(raw []byte) error {
	type plain Promotion
	return required(raw, (*plain)(v), "schema", "review", "reviewed", "approver", "rationale")
}
func promotionHash(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
func identity(v any) string {
	raw, _ := json.Marshal(v, json.Deterministic(true))
	return promotionHash(raw)
}
func (r PromotionReview) Identity() string { r.Commitment = ""; return identity(r) }
func (p Promotion) Identity() string       { return identity(p) }
func validDigest(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && hex.EncodeToString(b) == s
}
func DecodePromotion(raw []byte) (Promotion, error) {
	var p Promotion
	if len(raw) > MaxBytes || json.Unmarshal(raw, &p, json.RejectUnknownMembers(true)) != nil || p.Schema != PromotionSchema || p.Review.Schema != PromotionReviewSchema || !validDigest(p.Review.Suite) || !validDigest(p.Review.Releases) || !identifier.MatchString(p.Review.Environment) || !text(p.Review.Revision, 256) || !text(p.Approver, 256) || !text(p.Rationale, 1024) || p.Review.Identity() != p.Reviewed || p.Review.Commitment != p.Reviewed || len(p.Review.Jobs) < 1 || len(p.Review.Jobs) > 64 {
		return Promotion{}, errors.New("invalid suite promotion approval")
	}
	seen := map[string]bool{}
	for _, j := range p.Review.Jobs {
		if !identifier.MatchString(j.Job) || seen[j.Job] || !validDigest(j.SHA256) {
			return Promotion{}, errors.New("invalid promotion job pin")
		}
		seen[j.Job] = true
	}
	return p, nil
}
func promotionReview(prepared Prepared, environment, revision string) (PromotionReview, error) {
	r := PromotionReview{Schema: PromotionReviewSchema, Environment: environment, Revision: revision, Jobs: []CoverageSpecification{}}
	if !text(revision, 256) {
		return r, errors.New("promotion requires an explicit target revision assumption")
	}
	raw, e := read(filepath.Join(prepared.Directory, "suite.json"), MaxBytes)
	if e != nil {
		return r, e
	}
	r.Suite = promotionHash(raw)
	raw, e = read(filepath.Join(prepared.Directory, "release-references.json"), MaxBytes)
	if e != nil {
		return r, e
	}
	r.Releases = promotionHash(raw)
	for _, job := range prepared.Queue.Jobs {
		plan, e := durablerun.Prepare(filepath.Join(prepared.Directory, job.Spec))
		if e != nil {
			return r, e
		}
		pin, e := plan.InputIdentity()
		if e != nil {
			return r, e
		}
		r.Jobs = append(r.Jobs, CoverageSpecification{Job: job.ID, SHA256: pin})
	}
	r.Commitment = r.Identity()
	return r, nil
}

// ReviewPromotion validates every parameter, release and target without sending.
// Its temporary owner-only compilation is removed on return.
func ReviewPromotion(path, environment, references, revision string) (PromotionReview, error) {
	dir, e := os.MkdirTemp("", "readmit-promotion-")
	if e != nil {
		return PromotionReview{}, errors.New("cannot create private promotion preview")
	}
	defer os.RemoveAll(dir)
	prepared, e := PrepareApproved(path, environment, filepath.Join(dir, "prepared"), references)
	if e != nil {
		return PromotionReview{}, e
	}
	return promotionReview(prepared, environment, revision)
}
func ApprovePromotion(path, environment, references, revision, reviewed, approver, rationale, output string) (Promotion, error) {
	review, e := ReviewPromotion(path, environment, references, revision)
	if e != nil {
		return Promotion{}, e
	}
	p := Promotion{Schema: PromotionSchema, Review: review, Reviewed: reviewed, Approver: approver, Rationale: rationale}
	raw, e := json.Marshal(p, json.Deterministic(true))
	if e != nil {
		return Promotion{}, e
	}
	if _, e = DecodePromotion(raw); e != nil {
		return Promotion{}, e
	}
	out, e := artifactpath.Destination(output)
	if e != nil {
		return Promotion{}, e
	}
	if e = retain(filepath.Dir(out), filepath.Base(out), raw); e != nil {
		return Promotion{}, e
	}
	return p, nil
}

// RunPromoted checks the externally selected approval identity, then checks the
// complete freshly compiled inputs. The queue rechecks all sealed execution
// plans before the first send. Existing destinations never resume uncertain work.
func RunPromoted(ctx context.Context, path, environment, output, references, approval, pinned, revision string) (runqueue.Report, error) {
	raw, e := read(approval, MaxBytes)
	if e != nil {
		return runqueue.Report{}, e
	}
	p, e := DecodePromotion(raw)
	if e != nil {
		return runqueue.Report{}, e
	}
	if !validDigest(pinned) || p.Identity() != pinned || p.Review.Environment != environment || p.Review.Revision != revision {
		return runqueue.Report{}, errors.New("promotion approval, environment or revision assumption differs")
	}
	prepared, e := PrepareApproved(path, environment, output, references)
	if e != nil {
		return runqueue.Report{}, e
	}
	review, e := promotionReview(prepared, environment, revision)
	if e != nil || review.Identity() != p.Reviewed {
		_ = os.RemoveAll(prepared.Directory)
		return runqueue.Report{}, errors.New("suite inputs changed since promotion approval")
	}
	if e = retain(prepared.Directory, "promotion.json", raw); e != nil {
		return runqueue.Report{}, e
	}
	pins := map[string]string{}
	for _, j := range review.Jobs {
		pins[j.Job] = j.SHA256
	}
	return runPrepared(ctx, prepared, pins)
}
