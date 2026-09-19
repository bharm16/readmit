package redact

import (
	"context"
	"encoding/json/v2"
	"errors"
	"path/filepath"
)

// ValidateDisclosure rechecks private input, policy, specification and source
// bindings without executing either a fixture or an external target. Its return
// value establishes review consistency only, never an authenticated approver.
func ValidateDisclosure(ctx context.Context, reviewPath, privatePath string) (*Review, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	review, err := OpenReview(reviewPath)
	if err != nil || review.State != "ready-for-approval" {
		return nil, errors.New("complete disclosure review required")
	}
	raw, err := readLocal(filepath.Join(privatePath, "state.json"), maxReviewBytes)
	if err != nil || digest(raw) != review.LocalStateCommitment {
		return nil, errors.New("private disclosure binding changed")
	}
	var local localState
	if json.Unmarshal(raw, &local, json.RejectUnknownMembers(true)) != nil || local.Schema != PrivateSchema {
		return nil, errors.New("invalid private disclosure binding")
	}
	if err := revalidate(local); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return review, nil
}
