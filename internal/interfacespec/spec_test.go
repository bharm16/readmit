package interfacespec_test

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/bharm16/readmit/internal/interfacespec"
	"strings"
	"testing"
)

func TestSpecificationPinsProfileAndDocumentsAsSeparateNonExecutableProvenance(t *testing.T) {
	text := "Owned partner narrative, not an executable constraint."
	sum := sha256.Sum256([]byte(text))
	d := interfacespec.Document{Schema: interfacespec.Schema, Project: "0123456789abcdef01234567", Name: "Owned interface specification", Profile: interfacespec.ProfilePin{Item: "1123456789abcdef01234567", Revision: "1", Schema: "readmit-local-profile/v1", ID: "owned", Version: "1", SHA256: strings.Repeat("a", 64)}, Documents: []interfacespec.Documentation{{Name: "owned.md", Text: text, SHA256: hex.EncodeToString(sum[:])}}}
	raw, err := interfacespec.Encode(d)
	if err != nil {
		t.Fatal(err)
	}
	read, err := interfacespec.Decode(raw)
	if err != nil || read.Profile != d.Profile || read.Documents[0].Text != text {
		t.Fatalf("pins or prose changed: %+v %v", read, err)
	}
	invalid := d
	invalid.Documents = append(invalid.Documents, invalid.Documents[0])
	if _, err = interfacespec.Encode(invalid); err == nil {
		t.Fatal("duplicate documents accepted")
	}
	if _, err = interfacespec.Decode([]byte(strings.Replace(string(raw), `"name":"Owned interface specification"`, `"executable":null,"name":"Owned interface specification"`, 1))); err == nil {
		t.Fatal("unknown member became a constraint")
	}
	d.Documents[0].Text = "Changed"
	if _, err = interfacespec.Encode(d); err == nil {
		t.Fatal("changed document accepted under old digest")
	}
}
