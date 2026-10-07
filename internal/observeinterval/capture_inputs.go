package observeinterval

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/dataset"
	"github.com/bharm16/readmit/internal/networkaction"
	"github.com/bharm16/readmit/internal/secret"
	"path/filepath"
	"slices"
	"strconv"
	"time"
)

// verifyCaptureInputs follows only explicitly selected public configuration.
// It is an execution/preparation guard. Offline decoding never follows paths.
func verifyCaptureInputs(s CaptureSource) error {
	for _, input := range s.Inputs {
		if !filepath.IsAbs(input.Path) {
			return invalid
		}
		data, err := (artifactdir.Document{MaxBytes: 4 << 20}).Read(input.Path)
		if err != nil {
			return invalid
		}
		if input.Role == "credential" {
			document, err := secret.Decode(data)
			if err != nil {
				return invalid
			}
			ref, err := secret.Bind(document, input.Credential, secret.MLLPEndpoint, s.Address)
			if err != nil || ref.Rotation(time.Now()) == secret.RotationOverdue || s.PrivateKey == nil || strconv.Itoa(ref.Generation) != s.PrivateKey.Generation || ref.Command != s.PrivateKey.Locator.Command || !slices.Equal(ref.Arguments, s.PrivateKey.Locator.Arguments) {
				return invalid
			}
			data, err = json.Marshal(ref, json.Deterministic(true))
			if err != nil {
				return invalid
			}
		} else if input.Role == "certificate" && !bytes.Equal(data, s.Certificate) || input.Role == "client-authorities" && !bytes.Equal(data, s.Authorities) {
			return invalid
		}
		if dataset.Digest(data) != input.SHA256 {
			return invalid
		}
	}
	return nil
}

type captureInputAuthority struct {
	authority networkaction.Authority
	source    CaptureSource
}

func (a captureInputAuthority) Check(ctx context.Context, binding networkaction.Binding) (networkaction.Actor, error) {
	if err := verifyCaptureInputs(a.source); err != nil {
		return networkaction.Actor{}, err
	}
	return a.authority.Check(ctx, binding)
}
