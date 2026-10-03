// Package interfacespec retains authored interface specification associations.
// Descriptive documents never become executable profile clauses automatically.
package interfacespec

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/bharm16/readmit/internal/catalog"
	"github.com/bharm16/readmit/internal/strictdoc"
)

const Schema = "readmit-interface-spec/v1"
const MaxDocumentBytes = 64 << 10
const MaxDocuments = 16
const MaxBytes = 2 << 20

type ProfilePin struct {
	Item     string `json:"item"`
	Revision string `json:"revision"`
	Schema   string `json:"schema"`
	ID       string `json:"id"`
	Version  string `json:"version"`
	SHA256   string `json:"sha256"`
}
type Documentation struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Text   string `json:"text"`
}
type Document struct {
	Schema    string          `json:"schema"`
	Project   string          `json:"project"`
	Name      string          `json:"name"`
	Profile   ProfilePin      `json:"profile"`
	Documents []Documentation `json:"documents"`
}

func digest(raw []byte) string { value := sha256.Sum256(raw); return hex.EncodeToString(value[:]) }
func ValidDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	raw, err := hex.DecodeString(value)
	return err == nil && len(raw) == 32
}
func Validate(d Document) error {
	if d.Schema != Schema || !catalog.ValidID(d.Project) || !catalog.ValidName(d.Name) || !catalog.ValidID(d.Profile.Item) || !catalog.ValidToken(d.Profile.Revision) || !strings.HasPrefix(d.Profile.Schema, "readmit-local-profile/") || d.Profile.ID == "" || len(d.Profile.ID) > 200 || d.Profile.Version == "" || len(d.Profile.Version) > 200 || !ValidDigest(d.Profile.SHA256) || len(d.Documents) > MaxDocuments || d.Documents == nil {
		return errors.New("interface specification requires its project, exact profile revision and bounded document list")
	}
	seen := map[string]bool{}
	for _, doc := range d.Documents {
		if !catalog.ValidName(doc.Name) || !utf8.ValidString(doc.Text) || len(doc.Text) > MaxDocumentBytes || !ValidDigest(doc.SHA256) || digest([]byte(doc.Text)) != doc.SHA256 || seen[doc.SHA256] {
			return errors.New("specification documents need distinct byte identities and bounded UTF-8 content")
		}
		seen[doc.SHA256] = true
	}
	return nil
}
func Decode(raw []byte) (Document, error) {
	var d Document
	if err := (strictdoc.Document{Schema: Schema, MaxBytes: MaxBytes, Required: []string{"project", "name", "profile", "documents"}, Invalid: "invalid interface specification JSON", MustDeclare: "unsupported interface specification schema", TooLarge: "interface specification exceeds its byte bound", Requires: "interface specification requires every declared member"}).Decode(raw, &d); err != nil {
		return d, err
	}
	return d, Validate(d)
}
func Encode(d Document) ([]byte, error) {
	if err := Validate(d); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(d, json.Deterministic(true))
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxBytes {
		return nil, errors.New("encoded interface specification exceeds its byte bound")
	}
	return raw, nil
}
