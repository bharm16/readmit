package connectedtest

import (
	"context"
	"errors"
	"github.com/bharm16/readmit/internal/profileeval"
)

// EvaluateProfiles evaluates generated inputs against exact retained profile and
// pack revisions. It is an explicit independent check service; it never changes
// historical execution-result/v1, its checks or its verdict.
func EvaluateProfiles(ctx context.Context, p *Plan, profileID, packID string, options profileeval.Options) (profileeval.Report, error) {
	if p == nil {
		return profileeval.Report{}, invalid
	}
	var profile, pack []byte
	for _, ref := range p.document.Test.Profiles {
		if ref.ID == profileID {
			if profile != nil {
				return profileeval.Report{}, errors.New("ambiguous profile pin")
			}
			profile = p.files["dependencies/"+ref.SHA256]
		}
		if ref.ID == packID {
			if pack != nil {
				return profileeval.Report{}, errors.New("ambiguous pack pin")
			}
			pack = p.files["dependencies/"+ref.SHA256]
		}
	}
	if profile == nil || pack == nil {
		return profileeval.Report{}, errors.New("profile checks require exact retained pins")
	}
	inputs := []profileeval.Occurrence{}
	for _, id := range p.document.Order {
		for _, step := range p.document.Test.Steps {
			if step.ID == id && step.V2 != nil {
				inputs = append(inputs, profileeval.Occurrence{ID: step.V2.Occurrence, Bytes: p.files["inputs/"+step.ID+".hl7"]})
			}
		}
	}
	return profileeval.Evaluate(ctx, profile, pack, inputs, options)
}
