package fhirrest

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/bharm16/readmit/internal/artifactdir"
)

// Evidence owns the same verified snapshot used for outcomes and projections.
// Accessors return detached bytes; callers never reopen an unverified path.
type Evidence struct {
	result   Result
	files    map[string][]byte
	identity string
}

func OpenEvidence(ctx context.Context, directory string) (*Evidence, error) {
	files, e := artifactdir.Read(directory, family.Layout)
	if e != nil {
		return nil, refused
	}
	result, e := verify(ctx, files)
	if e != nil {
		return nil, e
	}
	return &Evidence{result: result, files: files, identity: strings.TrimSpace(string(files["identity.sha256"]))}, nil
}
func (e *Evidence) Identity() string { return e.identity }
func (e *Evidence) Result() Result   { var r Result; _ = json.Unmarshal(encode(e.result), &r); return r }
func (e *Evidence) ResponseBytes(index int) ([]byte, bool) {
	if index < 0 || index >= len(e.result.Attempts) {
		return nil, false
	}
	name := e.result.Attempts[index].Response
	if name == "" {
		return nil, false
	}
	raw, ok := e.files[name]
	return bytes.Clone(raw), ok
}
func (e *Evidence) RequestBytes(index int) ([]byte, bool) {
	if index < 0 || index >= len(e.result.Attempts) {
		return nil, false
	}
	return bytes.Clone(e.result.Attempts[index].Request.HTTP.Body), true
}
func (Evidence) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "FHIR HTTP evidence (private)") }
func (Evidence) MarshalJSON() ([]byte, error) {
	return nil, errors.New("FHIR HTTP evidence requires explicit private access")
}
