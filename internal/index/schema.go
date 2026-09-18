package index

import (
	"bytes"
	"encoding/json/jsontext"
	"io"
	"strings"

	"github.com/bharm16/readmit/internal/bundle"
)

// DeclaresSchema recognizes a top-level readmit-index/ schema declaration as
// soon as its string token is complete. Malformed content after that declaration
// cannot turn derived patient data into an ordinary file eligible for copying.
// This is classification, not validation: Decode must still accept the complete
// document before its declarations or contents may be used.
//
// The probe consumes at most the canonical file bound (64 MiB), independently
// of the smaller readable-index bound. Callers must refuse files beyond their
// own supported file bound. Corruption before any readable schema declaration
// cannot establish that a file is an index; filenames are not proof either.
func DeclaresSchema(data []byte) bool {
	decoder := jsontext.NewDecoder(io.LimitReader(bytes.NewReader(data), bundle.MaxEvidenceBytes), jsontext.AllowDuplicateNames(true))
	if token, err := decoder.ReadToken(); err != nil || token.Kind() != '{' {
		return false
	}
	for decoder.PeekKind() != '}' {
		key, err := decoder.ReadToken()
		if err != nil || key.Kind() != '"' {
			return false
		}
		if key.String() == "schema" && decoder.PeekKind() == '"' {
			token, err := decoder.ReadToken()
			if err != nil {
				return false
			}
			if token.Kind() == '"' && strings.HasPrefix(token.String(), "readmit-index/") {
				return true
			}
			continue
		}
		if err := decoder.SkipValue(); err != nil {
			return false
		}
	}
	return false
}
