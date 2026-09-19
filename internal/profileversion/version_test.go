package profileversion_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"os"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/localprofile"
	"github.com/bharm16/readmit/internal/profileversion"
)

const fixtureRoot = "../../testdata/fixtures/"

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(fixtureRoot + name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return data
}

// profile is the local profile every test here versions: the fixture issue #46
// authored, read through its own reader so nothing assembled in Go can stand in
// for a document a reader would have refused.
func profile(t *testing.T) localprofile.Profile {
	t.Helper()
	decoded, err := localprofile.Decode(fixture(t, "local-profile.json"))
	if err != nil {
		t.Fatalf("decode the local profile fixture: %v", err)
	}
	return decoded
}

// edit decodes one fixture loosely, lets the test reshape it, and encodes it
// again, for refusals a single textual edit cannot express.
func edit(t *testing.T, name string, reshape func(document map[string]any)) []byte {
	t.Helper()
	var document map[string]any
	if err := json.Unmarshal(fixture(t, name), &document); err != nil {
		t.Fatalf("decode %s loosely: %v", name, err)
	}
	reshape(document)
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("encode edited %s: %v", name, err)
	}
	return data
}

// mutate applies one textual edit to a fixture and fails the test if the edit
// did not land, so a refusal is never proven against unchanged bytes.
func mutate(t *testing.T, name, old, new string) []byte {
	t.Helper()
	source := string(fixture(t, name))
	if !strings.Contains(source, old) {
		t.Fatalf("%s does not contain %q", name, old)
	}
	return []byte(strings.Replace(source, old, new, 1))
}

// TestSealRecordsTheCanonicalDocument is the immutability half of the delivery
// read as one assertion: a version is sealed over the bytes the profile's own
// writer produces, the sealed record is the shipped fixture byte for byte, and
// the same profile always seals the same way.
func TestSealRecordsTheCanonicalDocument(t *testing.T) {
	sealed, err := profileversion.Seal(profile(t))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if sealed.Schema != profileversion.VersionSchema {
		t.Fatalf("sealed schema %q", sealed.Schema)
	}
	if sealed.Profile != (localprofile.Identity{ID: "fixture-local-siu", Version: "1"}) {
		t.Fatalf("sealed identity %+v", sealed.Profile)
	}

	// The digest is over the canonical document, which for this fixture is the
	// fixture itself: issue #46 requires the fixture read and written again to
	// be the fixture byte for byte.
	sum := sha256.Sum256(fixture(t, "local-profile.json"))
	if sealed.Content.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("the seal digests something other than the canonical document: %s", sealed.Content.SHA256)
	}
	if sealed.Content.Bytes != len(fixture(t, "local-profile.json")) {
		t.Fatalf("sealed length %d", sealed.Content.Bytes)
	}

	written, err := sealed.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if string(written) != string(fixture(t, "profile-version.json")) {
		t.Fatalf("the sealed version was written as\n%s", written)
	}
	again, err := profileversion.DecodeVersion(written)
	if err != nil {
		t.Fatalf("decode what was written: %v", err)
	}
	if again != sealed {
		t.Fatalf("a sealed version read back differs: %+v", again)
	}
}

// TestSealIgnoresFormattingAndFollowsRules separates the two things a checksum
// could mean. Reindenting a document changes not one rule, so it must not
// change the version's digest; changing a rule must.
func TestSealIgnoresFormattingAndFollowsRules(t *testing.T) {
	original := profile(t)
	sealed, err := profileversion.Seal(original)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	compact, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("write the profile compactly: %v", err)
	}
	reformatted, err := localprofile.Decode(compact)
	if err != nil {
		t.Fatalf("decode the reformatted profile: %v", err)
	}
	reformattedSeal, err := profileversion.Seal(reformatted)
	if err != nil {
		t.Fatalf("seal the reformatted profile: %v", err)
	}
	if reformattedSeal != sealed {
		t.Fatalf("reformatting changed the sealed version to %+v", reformattedSeal)
	}
	if err := sealed.Verify(reformatted); err != nil {
		t.Fatalf("a reformatted profile did not verify: %v", err)
	}

	changed := relaxed(t, original)
	changedSeal, err := profileversion.Seal(changed)
	if err != nil {
		t.Fatalf("seal the changed profile: %v", err)
	}
	if changedSeal.Content == sealed.Content {
		t.Fatal("a changed rule did not change the sealed content")
	}
}

// TestVerifyRefusesAProfileThatChangedUnderItsVersion is the rule a saved test's
// pin rests on: a version stands for one document, so a profile edited without a
// new version does not verify against the version it still claims.
func TestVerifyRefusesAProfileThatChangedUnderItsVersion(t *testing.T) {
	sealed, err := profileversion.Seal(profile(t))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if err := sealed.Verify(profile(t)); err != nil {
		t.Fatalf("the sealed profile did not verify against itself: %v", err)
	}

	changed := relaxed(t, profile(t))
	err = sealed.Verify(changed)
	if err == nil {
		t.Fatal("a profile that changed under its version verified")
	}
	if !strings.Contains(err.Error(), "carries a new version") {
		t.Fatalf("the refusal does not say what to do: %v", err)
	}

	renamed := profile(t)
	renamed.Identity.Version = "2"
	if err := sealed.Verify(renamed); err == nil {
		t.Fatal("a different version verified against this seal")
	}

	// A profile a reader would have refused is never sealed and never verifies.
	incomplete := profile(t)
	incomplete.Segments = nil
	if _, err := profileversion.Seal(incomplete); err == nil {
		t.Fatal("a profile constraining nothing was sealed")
	}
	if err := sealed.Verify(incomplete); err == nil {
		t.Fatal("a profile constraining nothing verified")
	}
}

func TestDecodeVersionRefusesWhatItCannotStandBehind(t *testing.T) {
	for _, refusal := range []struct {
		name     string
		document []byte
	}{
		{"an unknown member", mutate(t, "profile-version.json", `"content"`, `"sealed_at": "2026-09-19", "content"`)},
		{"a contract this release does not read", mutate(t, "profile-version.json", "readmit-profile-version/v1", "readmit-profile-version/v2")},
		{"no schema at all", edit(t, "profile-version.json", func(document map[string]any) { delete(document, "schema") })},
		{"no profile identity", edit(t, "profile-version.json", func(document map[string]any) { delete(document, "profile") })},
		{"no content", edit(t, "profile-version.json", func(document map[string]any) { delete(document, "content") })},
		{"a content member the contract does not carry", edit(t, "profile-version.json", func(document map[string]any) {
			document["content"].(map[string]any)["algorithm"] = "sha256"
		})},
		{"a content record with no digest", edit(t, "profile-version.json", func(document map[string]any) {
			delete(document["content"].(map[string]any), "sha256")
		})},
		{"an abbreviated digest", edit(t, "profile-version.json", func(document map[string]any) {
			document["content"].(map[string]any)["sha256"] = "abc123"
		})},
		{"an uppercase digest", edit(t, "profile-version.json", func(document map[string]any) {
			content := document["content"].(map[string]any)
			content["sha256"] = strings.ToUpper(content["sha256"].(string))
		})},
		{"a document of no length", edit(t, "profile-version.json", func(document map[string]any) {
			document["content"].(map[string]any)["bytes"] = 0
		})},
		{"a document longer than a profile may be", edit(t, "profile-version.json", func(document map[string]any) {
			document["content"].(map[string]any)["bytes"] = localprofile.MaxProfileBytes + 1
		})},
		{"an identity no local profile could carry", edit(t, "profile-version.json", func(document map[string]any) {
			document["profile"].(map[string]any)["id"] = "Fixture-Local-SIU"
		})},
		{"a version that does not begin with a digit", edit(t, "profile-version.json", func(document map[string]any) {
			document["profile"].(map[string]any)["version"] = "v1"
		})},
		{"an identity member the contract does not carry", edit(t, "profile-version.json", func(document map[string]any) {
			document["profile"].(map[string]any)["latest"] = true
		})},
		{"a document past the size limit", append(make([]byte, profileversion.MaxVersionBytes), fixture(t, "profile-version.json")...)},
		{"nothing at all", []byte(`{}`)},
		{"not JSON", []byte("readmit-profile-version/v1")},
	} {
		t.Run(refusal.name, func(t *testing.T) {
			if version, err := profileversion.DecodeVersion(refusal.document); err == nil {
				t.Fatalf("accepted %+v", version)
			}
		})
	}
}

// relaxed returns the fixture profile with one rule changed, through the typed
// editor, so the changed profile is one a reader would also accept.
func relaxed(t *testing.T, original localprofile.Profile) localprofile.Profile {
	t.Helper()
	editor, err := localprofile.Open(original)
	if err != nil {
		t.Fatalf("open the profile: %v", err)
	}
	if err := editor.SetField("SCH", localprofile.Field{
		Position:    1,
		Name:        "Placer appointment number",
		Usage:       localprofile.UsageRequiredOrEmpty,
		Cardinality: &localprofile.Cardinality{Min: 0, Max: "1"},
		Type:        "EI",
	}); err != nil {
		t.Fatalf("relax SCH-1: %v", err)
	}
	return editor.Profile()
}

// TestSealRefusesADocumentNothingCouldReadBack is the size boundary of the
// seal. Indentation makes a canonical document longer than the compact bytes a
// reader may have been handed, so a profile a reader accepts can still write out
// past the size a reader accepts. A version stands for a document, so such a
// profile is refused rather than sealed over bytes nothing could read back.
func TestSealRefusesADocumentNothingCouldReadBack(t *testing.T) {
	name := strings.Repeat("n", 128)
	oversize := profile(t)
	oversize.Segments = nil
	for segment := range 42 {
		fields := make([]localprofile.Field, 0, 512)
		for position := 1; position <= 512; position++ {
			fields = append(fields, localprofile.Field{Position: position, Name: name, Usage: localprofile.UsageOptional})
		}
		oversize.Segments = append(oversize.Segments, localprofile.Segment{
			ID:     "Z" + string(rune('A'+segment/10)) + string(rune('0'+segment%10)),
			Fields: fields,
		})
	}
	if err := oversize.Validate(); err != nil {
		t.Fatalf("the profile is not one a reader would accept: %v", err)
	}
	_, err := profileversion.Seal(oversize)
	if err == nil {
		t.Fatal("a profile no reader could read back was sealed")
	}
	if !strings.Contains(err.Error(), "canonical document exceeds") {
		t.Fatalf("the refusal does not say why: %v", err)
	}
	later := oversize
	later.Identity.Version = "2"
	if _, err := profileversion.Compare(oversize, later); err == nil {
		t.Fatal("a profile no reader could read back was compared")
	}
}
