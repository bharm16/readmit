package profileeval_test

import (
	"slices"
	"strconv"
	"testing"

	"github.com/bharm16/readmit/internal/localprofile"
	"github.com/bharm16/readmit/internal/profileeval"
	"github.com/bharm16/readmit/internal/profilepack"
)

// Acceptance is answered once, by the module: every schema it lists decodes
// past the version gate, and anything else is refused as an unsupported
// version. Callers ask AcceptsProfile and AcceptsPack instead of listing
// versions themselves, so a new version lands in one place.
func TestAcceptanceAnswersEveryReadableProfileAndPackVersion(t *testing.T) {
	for _, schema := range profileeval.ProfileSchemas() {
		if !profileeval.AcceptsProfile(schema) {
			t.Fatalf("a listed profile schema is not accepted: %s", schema)
		}
		raw := []byte(`{"schema":` + strconv.Quote(schema) + `}`)
		if _, err := profileeval.DecodeProfile(raw); err == nil || err.Error() == "unsupported local profile version" {
			t.Fatalf("a listed profile schema does not decode past the version gate: %s: %v", schema, err)
		}
	}
	for _, schema := range profileeval.PackSchemas() {
		if !profileeval.AcceptsPack(schema) {
			t.Fatalf("a listed pack schema is not accepted: %s", schema)
		}
		raw := []byte(`{"schema":` + strconv.Quote(schema) + `}`)
		if _, err := profileeval.DecodePack(raw); err == nil || err.Error() == "unsupported profile pack version" {
			t.Fatalf("a listed pack schema does not decode past the version gate: %s: %v", schema, err)
		}
	}
	for _, schema := range []string{localprofile.Schema, profileeval.ProfileSchema, profileeval.ProfileSchemaV3} {
		if !slices.Contains(profileeval.ProfileSchemas(), schema) {
			t.Fatalf("a readable profile schema is not listed: %s", schema)
		}
	}
	for _, schema := range []string{profilepack.Schema, profileeval.PackSchema, profileeval.PackSchemaV3, profileeval.PackSchemaV4, profileeval.PackSchemaV5} {
		if !slices.Contains(profileeval.PackSchemas(), schema) {
			t.Fatalf("a readable pack schema is not listed: %s", schema)
		}
	}
	if profileeval.AcceptsProfile("readmit-local-profile/v9") || profileeval.AcceptsPack("readmit-profile-pack/v9") {
		t.Fatal("a future schema version is accepted before any reader exists")
	}
	if _, err := profileeval.DecodeProfile([]byte(`{"schema":"readmit-local-profile/v9"}`)); err == nil || err.Error() != "unsupported local profile version" {
		t.Fatalf("an unreadable profile version was not refused by name: %v", err)
	}
	if _, err := profileeval.DecodePack([]byte(`{"schema":"readmit-profile-pack/v9"}`)); err == nil || err.Error() != "unsupported profile pack version" {
		t.Fatalf("an unreadable pack version was not refused by name: %v", err)
	}
}
