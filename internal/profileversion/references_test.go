package profileversion_test

import (
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/localprofile"
	"github.com/bharm16/readmit/internal/profileversion"
)

func references(t *testing.T) profileversion.References {
	t.Helper()
	decoded, err := profileversion.DecodeReferences(fixture(t, "profile-references.json"))
	if err != nil {
		t.Fatalf("decode the reference index: %v", err)
	}
	return decoded
}

func pinned(t *testing.T, index profileversion.References, test string) profileversion.Pin {
	t.Helper()
	for _, reference := range index.Tests {
		if reference.Test == test {
			return reference.Pinned
		}
	}
	t.Fatalf("the index records no saved test %s", test)
	return profileversion.Pin{}
}

// sealedPin is the pin a saved test records when it is written against the
// fixture profile at one version, taken from the seal rather than assembled, so
// a test cannot pin a document nothing sealed.
func sealedPin(t *testing.T, version string) profileversion.Pin {
	t.Helper()
	at := profile(t)
	at.Identity.Version = version
	sealed, err := profileversion.Seal(at)
	if err != nil {
		t.Fatalf("seal at version %s: %v", version, err)
	}
	return sealed.Pin()
}

func TestDecodeReferencesReadsTheFixtureAsWritten(t *testing.T) {
	index := references(t)
	if index.Schema != profileversion.ReferencesSchema {
		t.Fatalf("decoded schema %q", index.Schema)
	}
	if len(index.Tests) != 4 {
		t.Fatalf("decoded %d references", len(index.Tests))
	}
	reschedule := index.Tests[2]
	if reschedule.Test != "test-reschedule.json" || reschedule.Case != "test-case" {
		t.Fatalf("the reference is %+v", reschedule)
	}
	// The pin is the sealed version: the identity, and the checksum of the
	// document that identity stood for. The fixture pins the real one.
	if reschedule.Pinned != sealedPin(t, "1") {
		t.Fatalf("the pin is %+v", reschedule.Pinned)
	}
	if reschedule.Pinned.Identity() != (localprofile.Identity{ID: "fixture-local-siu", Version: "1"}) {
		t.Fatalf("the pinned identity is %+v", reschedule.Pinned.Identity())
	}
	// An index records what saved tests pin, whatever they pin: one profile's
	// versions and another profile entirely sit in the same document.
	if pinned(t, index, "test-other-interface.json").ID != "fixture-local-adt" {
		t.Fatal("the index does not record a reference to another profile")
	}

	written, err := index.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if string(written) != string(fixture(t, "profile-references.json")) {
		t.Fatalf("the index was written as\n%s", written)
	}
}

// TestUpgradeMovesOnePinAndNothingElse is the explicit-upgrade half of the
// delivery: a pin moves when somebody names the test, the version it is on and
// the version it is moved to, and the index that was upgraded from is untouched.
func TestUpgradeMovesOnePinAndNothingElse(t *testing.T) {
	index := references(t)
	from := sealedPin(t, "1")
	to := revisedPin(t)

	upgraded, err := index.Upgrade("test-reschedule.json", from, to)
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if pinned(t, upgraded, "test-reschedule.json") != to {
		t.Fatalf("the upgraded pin is %+v", pinned(t, upgraded, "test-reschedule.json"))
	}
	if pinned(t, index, "test-reschedule.json") != from {
		t.Fatal("upgrading changed the index it was called on")
	}
	// Every other saved test stays exactly where it was: an upgrade moves the
	// one pin it names.
	for _, other := range []string{"test-cancellation.json", "test-other-interface.json", "test-retired-window.json"} {
		if pinned(t, upgraded, other) != pinned(t, index, other) {
			t.Fatalf("upgrading one saved test moved %s", other)
		}
	}
	if _, err := upgraded.Encode(); err != nil {
		t.Fatalf("the upgraded index is not a document: %v", err)
	}
}

func TestUpgradeRefusesWhatWouldChangeAContractSilently(t *testing.T) {
	index := references(t)
	siu1 := sealedPin(t, "1")
	siu2 := revisedPin(t)
	// The same version, standing for a different document: the profile was
	// rewritten under a version a saved test already pins.
	rewritten := siu1
	rewritten.SHA256 = "0000000000000000000000000000000000000000000000000000000000000000"

	for _, refusal := range []struct {
		name     string
		test     string
		from, to profileversion.Pin
		says     string
	}{
		{"a saved test the index does not record", "test-absent.json", siu1, siu2, "records no saved test"},
		{"a saved test that is on another version", "test-retired-window.json", siu1, siu2, "pins fixture-local-siu version 3"},
		{"a saved test that pins another profile", "test-other-interface.json", siu1, siu2, "pins fixture-local-adt version 1"},
		{"the version the saved test already pins", "test-reschedule.json", siu1, siu1, "other than the one"},
		{"another profile rather than another version", "test-reschedule.json", siu1,
			profileversion.Pin{ID: "fixture-local-adt", Version: "1", SHA256: siu1.SHA256}, "are different profiles"},
		{"a version no local profile could carry", "test-reschedule.json", siu1,
			profileversion.Pin{ID: "fixture-local-siu", Version: "latest", SHA256: siu2.SHA256}, "begins with a digit"},
		{"a pin with no checksum", "test-reschedule.json", siu1,
			profileversion.Pin{ID: "fixture-local-siu", Version: "2"}, "64 lowercase hexadecimal digits"},
		{"a profile rewritten under the version it pins", "test-reschedule.json", rewritten, siu2, "carries a new version"},
	} {
		t.Run(refusal.name, func(t *testing.T) {
			upgraded, err := index.Upgrade(refusal.test, refusal.from, refusal.to)
			if err == nil {
				t.Fatalf("accepted the upgrade: %+v", upgraded)
			}
			if !strings.Contains(err.Error(), refusal.says) {
				t.Fatalf("the refusal does not say %q: %v", refusal.says, err)
			}
			if len(upgraded.Tests) != 0 {
				t.Fatal("a refused upgrade returned an index")
			}
		})
	}
}

func TestDecodeReferencesRefusesWhatItCannotStandBehind(t *testing.T) {
	for _, refusal := range []struct {
		name     string
		document []byte
	}{
		{"one saved test pinned twice", fixture(t, "profile-references-refused.json")},
		{"an unknown member", mutate(t, "profile-references.json", `"tests"`, `"profile": "fixture-local-siu", "tests"`)},
		{"an unknown member inside a reference", mutate(t, "profile-references.json", `"case": "other-case"`, `"case": "other-case", "suite": "regression"`)},
		{"a contract this release does not read", mutate(t, "profile-references.json", "readmit-profile-references/v1", "readmit-profile-references/v2")},
		{"no schema at all", edit(t, "profile-references.json", func(document map[string]any) { delete(document, "schema") })},
		{"no tests member", edit(t, "profile-references.json", func(document map[string]any) { delete(document, "tests") })},
		{"no saved test at all", edit(t, "profile-references.json", func(document map[string]any) { document["tests"] = []any{} })},
		{"a reference with no case", edit(t, "profile-references.json", func(document map[string]any) {
			delete(document["tests"].([]any)[0].(map[string]any), "case")
		})},
		{"a reference with no pin", edit(t, "profile-references.json", func(document map[string]any) {
			delete(document["tests"].([]any)[0].(map[string]any), "pinned")
		})},
		{"a pin that names a version range", edit(t, "profile-references.json", func(document map[string]any) {
			document["tests"].([]any)[0].(map[string]any)["pinned"].(map[string]any)["version"] = ">=1"
		})},
		{"a pin with no checksum", edit(t, "profile-references.json", func(document map[string]any) {
			delete(document["tests"].([]any)[0].(map[string]any)["pinned"].(map[string]any), "sha256")
		})},
		{"a pinned checksum that is not one", edit(t, "profile-references.json", func(document map[string]any) {
			document["tests"].([]any)[0].(map[string]any)["pinned"].(map[string]any)["sha256"] = "not-a-digest"
		})},
		{"a pin with a member the contract does not carry", edit(t, "profile-references.json", func(document map[string]any) {
			document["tests"].([]any)[0].(map[string]any)["pinned"].(map[string]any)["latest"] = true
		})},
		{"an abbreviated digest", edit(t, "profile-references.json", func(document map[string]any) {
			document["tests"].([]any)[0].(map[string]any)["sha256"] = "040860d8"
		})},
		{"an empty saved test path", edit(t, "profile-references.json", func(document map[string]any) {
			document["tests"].([]any)[0].(map[string]any)["test"] = ""
		})},
		{"a path that could drive the terminal it is shown on", edit(t, "profile-references.json", func(document map[string]any) {
			document["tests"].([]any)[0].(map[string]any)["case"] = "test-case[2J"
		})},
		{"a document past the size limit", append(make([]byte, profileversion.MaxReferencesBytes), fixture(t, "profile-references.json")...)},
		{"nothing at all", []byte(`{}`)},
		{"not JSON", []byte("readmit-profile-references/v1")},
	} {
		t.Run(refusal.name, func(t *testing.T) {
			if index, err := profileversion.DecodeReferences(refusal.document); err == nil {
				t.Fatalf("accepted %+v", index)
			}
		})
	}
}
