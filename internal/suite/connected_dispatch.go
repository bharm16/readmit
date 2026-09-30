package suite

import (
	"context"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"

	"github.com/bharm16/readmit/internal/artifactdir"
)

const ConnectedDispatchSchema = "readmit-connected-suite-dispatch/v1"

// ConnectedDispatch is customer-installed typed execution data consumed by
// RD19's acknowledged scheduler. It is not a scheduler or a shell program.
type ConnectedDispatch struct {
	Schema            string `json:"schema"`
	Suite             string `json:"suite"`
	Environment       string `json:"environment"`
	Authority         string `json:"authority"`
	Promotion         string `json:"promotion"`
	PromotionIdentity string `json:"promotion_identity"`
	Revision          string `json:"revision_assumption"`
	Input             string `json:"input"`
}

func DecodeConnectedDispatch(raw []byte) (ConnectedDispatch, error) {
	var d ConnectedDispatch
	if len(raw) > MaxBytes || required(raw, &d, "schema", "suite", "environment", "authority", "promotion", "promotion_identity", "revision_assumption", "input") != nil ||
		d.Schema != ConnectedDispatchSchema || !identifier.MatchString(d.Environment) || !validDigest(d.Input) || !validDigest(d.PromotionIdentity) || !text(d.Revision, 256) {
		return d, errors.New("invalid installed connected suite dispatch")
	}
	for _, p := range []string{d.Suite, d.Authority, d.Promotion} {
		if !filepath.IsAbs(p) || filepath.Clean(p) != p {
			return d, errors.New("connected dispatch paths are exact customer-host paths")
		}
	}
	return d, nil
}

func ReadConnectedDispatch(path string) (ConnectedDispatch, error) {
	raw, err := read(path, MaxBytes)
	if err != nil {
		return ConnectedDispatch{}, err
	}
	return DecodeConnectedDispatch(raw)
}

// SaveConnectedDispatch publishes only after the entire promoted expansion
// was verified passively. Installing/issuing authority remains separate.
func SaveConnectedDispatch(request ConnectedRequest, authority, output string) (ConnectedDispatch, error) {
	dir, err := os.MkdirTemp("", "readmit-connected-dispatch-")
	if err != nil {
		return ConnectedDispatch{}, err
	}
	defer os.RemoveAll(dir)
	preview := request
	preview.Output = filepath.Join(dir, "prepared")
	p, err := PrepareConnected(preview)
	if err != nil {
		return ConnectedDispatch{}, err
	}
	if _, err = p.VerifyPromotion(request.Promotion, request.PromotionIdentity, request.Environment, request.Revision); err != nil {
		return ConnectedDispatch{}, err
	}
	d := ConnectedDispatch{Schema: ConnectedDispatchSchema, Suite: request.Path, Environment: request.Environment, Authority: authority, Promotion: request.Promotion, PromotionIdentity: request.PromotionIdentity, Revision: request.Revision, Input: p.Identity}
	raw, err := json.Marshal(d, json.Deterministic(true))
	if err != nil {
		return d, err
	}
	if _, err = DecodeConnectedDispatch(raw); err != nil {
		return d, err
	}
	if err = (artifactdir.Document{MaxBytes: MaxBytes}).Create(output, raw); err != nil {
		return d, err
	}
	return d, nil
}

func (d ConnectedDispatch) Prepare(ctx context.Context) (*ConnectedPrepared, func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, func() {}, err
	}
	dir, err := os.MkdirTemp("", "readmit-scheduled-connected-")
	if err != nil {
		return nil, func() {}, err
	}
	remove := func() { _ = os.RemoveAll(dir) }
	p, err := PrepareConnected(ConnectedRequest{Path: d.Suite, Environment: d.Environment, Output: filepath.Join(dir, "prepared"), Promotion: d.Promotion, PromotionIdentity: d.PromotionIdentity, Revision: d.Revision})
	if err != nil || p.Identity != d.Input {
		remove()
		return nil, func() {}, errors.New("installed connected dispatch inputs changed")
	}
	return p, remove, nil
}
