package profileversion_test

import (
	"os"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/localprofile"
	"github.com/bharm16/readmit/internal/profileversion"
)

func seeds(f *testing.F, name string, extra ...string) string {
	f.Helper()
	data, err := os.ReadFile(fixtureRoot + name)
	if err != nil {
		f.Fatalf("read %s: %v", name, err)
	}
	f.Add(data)
	for _, seed := range extra {
		f.Add([]byte(seed))
	}
	return string(data)
}

// FuzzSealedProfileVersion exercises the sealed version reader. No input may
// panic, and no bytes at all may produce a version whose identity, document
// length or digest is outside what the contract allows, or one the writer
// cannot write back unchanged.
func FuzzSealedProfileVersion(f *testing.F) {
	document := seeds(f, "profile-version.json",
		`{"schema":"readmit-profile-version/v1"}`,
		`{}`)
	for _, seed := range []string{
		strings.Replace(document, `"version": "1"`, `"version": "latest"`, 1),
		strings.Replace(document, `"bytes": 3322`, `"bytes": -1`, 1),
		strings.Replace(document, "readmit-profile-version/v1", "readmit-profile-version/v2", 1),
		strings.Replace(document, `"content"`, `"signature": "", "content"`, 1),
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		version, err := profileversion.DecodeVersion(data)
		if err != nil {
			return
		}
		if version.Schema != profileversion.VersionSchema {
			t.Fatalf("accepted the contract %q", version.Schema)
		}
		if err := localprofile.ValidateIdentity("local profile", version.Profile); err != nil {
			t.Fatalf("accepted the identity %+v: %v", version.Profile, err)
		}
		if version.Content.Bytes <= 0 || version.Content.Bytes > localprofile.MaxProfileBytes {
			t.Fatalf("accepted the document length %d", version.Content.Bytes)
		}
		if len(version.Content.SHA256) != 64 || strings.ToLower(version.Content.SHA256) != version.Content.SHA256 {
			t.Fatalf("accepted the digest %q", version.Content.SHA256)
		}
		written, err := version.Encode()
		if err != nil {
			t.Fatalf("a decoded version could not be written: %v", err)
		}
		again, err := profileversion.DecodeVersion(written)
		if err != nil || again != version {
			t.Fatalf("a version written and read again is %+v: %v", again, err)
		}
	})
}

// FuzzProfileReferenceIndex exercises the reference index reader and the one
// operation that moves a pin. No input may panic, no bytes may produce an index
// recording one saved test twice or a pin no local profile could answer, and no
// upgrade may leave an index the reader would refuse, move a pin it was not
// asked to move, or change the index it was called on.
func FuzzProfileReferenceIndex(f *testing.F) {
	document := seeds(f, "profile-references.json",
		`{"schema":"readmit-profile-references/v1","tests":[]}`,
		`{}`)
	refused, err := os.ReadFile(fixtureRoot + "profile-references-refused.json")
	if err != nil {
		f.Fatalf("read the refused fixture: %v", err)
	}
	f.Add(refused)
	for _, seed := range []string{
		strings.Replace(document, `"version": "1"`, `"version": "*"`, 1),
		strings.Replace(document, `"sha256": "e96a3350b728a78d063cf99afddeec3854d393682039ed4348fbc89057c55054"`, `"sha256": "E96A3350"`, 1),
		strings.Replace(document, `"case": "test-case"`, `"case": ""`, 1),
		strings.Replace(document, "readmit-profile-references/v1", "readmit-profile-references/v2", 1),
		strings.Replace(document, `"tests"`, `"profile": "fixture-local-siu", "tests"`, 1),
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		index, err := profileversion.DecodeReferences(data)
		if err != nil {
			return
		}
		if index.Schema != profileversion.ReferencesSchema || len(index.Tests) == 0 {
			t.Fatalf("accepted %+v", index)
		}
		recorded := map[string]profileversion.Pin{}
		for _, reference := range index.Tests {
			if _, twice := recorded[reference.Test]; twice {
				t.Fatalf("accepted the saved test %s twice", reference.Test)
			}
			if err := reference.Pinned.Validate(); err != nil {
				t.Fatalf("accepted the pin %+v: %v", reference.Pinned, err)
			}
			recorded[reference.Test] = reference.Pinned
		}
		if written, err := index.Encode(); err != nil {
			t.Fatalf("a decoded index could not be written: %v", err)
		} else if _, err := profileversion.DecodeReferences(written); err != nil {
			t.Fatalf("an index written and read again was refused: %v", err)
		}

		// Upgrading moves the one pin it names and nothing else, and what it
		// returns is still a document.
		first := index.Tests[0]
		to := profileversion.Pin{ID: first.Pinned.ID, Version: first.Pinned.Version + ".1", SHA256: first.SHA256}
		upgraded, err := index.Upgrade(first.Test, first.Pinned, to)
		if err != nil {
			t.Fatalf("upgrade %s from %+v: %v", first.Test, first.Pinned, err)
		}
		if err := upgraded.Validate(); err != nil {
			t.Fatalf("an upgraded index is not a document: %v", err)
		}
		for _, reference := range upgraded.Tests {
			want := recorded[reference.Test]
			if reference.Test == first.Test {
				want = to
			}
			if reference.Pinned != want {
				t.Fatalf("upgrading %s left %s pinned to %+v", first.Test, reference.Test, reference.Pinned)
			}
		}
		for _, reference := range index.Tests {
			if reference.Pinned != recorded[reference.Test] {
				t.Fatalf("upgrading changed the index it was called on at %s", reference.Test)
			}
		}
	})
}

// FuzzProfileVersionComparison exercises sealing and comparing over local
// profile documents. No input may panic; sealing is a function of the profile,
// so the same profile always seals the same way and always verifies against its
// own seal; and no bytes may produce a comparison of a profile with itself, of
// two different profiles, or of one version standing for two documents.
func FuzzProfileVersionComparison(f *testing.F) {
	document := seeds(f, "local-profile.json",
		`{"schema":"readmit-local-profile/v1"}`,
		`{}`)
	for _, seed := range []string{
		strings.Replace(document, `"version": "1"`, `"version": "2"`, 1),
		strings.Replace(document, `"id": "fixture-local-siu"`, `"id": "fixture-local-adt"`, 1),
		strings.Replace(document, `"usage": "X"`, `"usage": "O"`, 1),
		strings.Replace(document, `"precision": "minute"`, `"precision": "second"`, 1),
	} {
		f.Add([]byte(seed))
	}
	base, err := localprofile.Decode([]byte(document))
	if err != nil {
		f.Fatalf("decode the local profile fixture: %v", err)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		candidate, err := localprofile.Decode(data)
		if err != nil {
			return
		}
		sealed, err := profileversion.Seal(candidate)
		if err != nil {
			// A canonical document past the size a reader accepts is refused
			// rather than sealed, and nothing downstream of a seal runs.
			return
		}
		// Verify seals the profile again and compares, so a seal that was not a
		// function of the profile alone is caught here.
		if err := sealed.Verify(candidate); err != nil {
			t.Fatalf("a profile did not verify against its own seal: %v", err)
		}
		if err := sealed.Pin().Validate(); err != nil {
			t.Fatalf("a sealed version yielded a pin no reference could record: %v", err)
		}
		if _, err := profileversion.Compare(candidate, candidate); err == nil {
			t.Fatal("a version compared with itself")
		}

		comparison, err := profileversion.Compare(base, candidate)
		if err != nil {
			return
		}
		if comparison.Profile != base.Identity.ID || comparison.Profile != candidate.Identity.ID {
			t.Fatalf("compared %+v", comparison)
		}
		if comparison.From == comparison.To {
			t.Fatalf("compared one version with itself: %+v", comparison)
		}
		for _, change := range comparison.Changes {
			if change.Detail == "" {
				t.Fatalf("a change says nothing: %+v", change)
			}
			if change.Part != profileversion.PartBase && change.Subject == "" {
				t.Fatalf("a change names nothing: %+v", change)
			}
		}
	})
}
