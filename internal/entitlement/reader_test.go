package entitlement_test

// Either contract version goes through one reader: the version a document or
// a store declares chooses the reader that decides it, and the result says
// which one did. The rules both versions share are written here once, for
// both; the rules only one version has stay with that version's tests.

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/entitlement"
)

// contract is one entitlement version as the reader meets it: what a vendor
// signs under it and what an installation activates it for.
type contract struct {
	name   string
	schema string
	store  string
	id     string
	// author is who a store is activated for; a v1 entitlement names none.
	author string
	device string
	// issue signs one issue of this version's example purchase with the key
	// the trust store names, or with another vendor's key when foreign.
	issue func(t *testing.T, sequence int, organization string, foreign bool) []byte
}

// foreignKeyID names a signing key the test trust store does not name.
const foreignKeyID = "vendor-other-2026z"

// foreignKey is that key: another vendor's, or a forger's.
func foreignKey() ed25519.PrivateKey {
	return ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize))
}

func contracts() []contract {
	return []contract{
		{name: "v1", schema: entitlement.Schema, store: entitlement.StoreSchema, id: "ENT-0001", device: "ws-0413",
			issue: func(t *testing.T, sequence int, organization string, foreign bool) []byte {
				t.Helper()
				claims := testClaims()
				claims.Sequence, claims.Organization = sequence, organization
				if !foreign {
					return signed(t, claims)
				}
				document, err := entitlement.Sign(claims, foreignKeyID, foreignKey())
				if err != nil {
					t.Fatal(err)
				}
				data, err := entitlement.Encode(document)
				if err != nil {
					t.Fatal(err)
				}
				return data
			}},
		{name: "v2", schema: entitlement.SchemaV2, store: entitlement.StoreSchemaV2, id: "ENT-0002", author: "a.nguyen", device: "ws-0413",
			issue: func(t *testing.T, sequence int, organization string, foreign bool) []byte {
				t.Helper()
				claims := testClaimsV2()
				claims.Sequence, claims.Organization = sequence, organization
				if !foreign {
					return signedV2(t, claims)
				}
				document, err := entitlement.SignV2(claims, foreignKeyID, foreignKey())
				if err != nil {
					t.Fatal(err)
				}
				data, err := entitlement.EncodeV2(document)
				if err != nil {
					t.Fatal(err)
				}
				return data
			}},
	}
}

// other is the contract that is not c: the version c's documents and stores
// must never be read as.
func (c contract) other() contract {
	for _, candidate := range contracts() {
		if candidate.name != c.name {
			return candidate
		}
	}
	panic("one contract version only")
}

// tagged reports which version a reader result carries, and fails the test
// unless exactly one is set.
func tagged(t *testing.T, v1, v2 bool) string {
	t.Helper()
	switch {
	case v1 && !v2:
		return entitlement.Schema
	case v2 && !v1:
		return entitlement.SchemaV2
	}
	t.Fatalf("a reader result carries v1 %t and v2 %t", v1, v2)
	return ""
}

func (c contract) install(t *testing.T, trust entitlement.Trust, sequence int) (entitlement.Installed, []byte) {
	t.Helper()
	document := c.issue(t, sequence, "example-hospital", false)
	received, err := entitlement.ReadSigned(document)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	store, err := received.Import(filepath.Join(t.TempDir(), "entitlement"), trust, c.author, c.device, moment(2026, time.September, 19))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	return store, document
}

// Each version is verified, installed and reopened by its own reader, and the
// result is tagged with that version and no other.
func TestTheReaderDecidesEachVersionByItsOwnReader(t *testing.T) {
	trust := testTrust(t, entitlement.KeyActive, time.Time{})
	for _, c := range contracts() {
		t.Run(c.name, func(t *testing.T) {
			document := c.issue(t, 1, "example-hospital", false)
			received, err := entitlement.ReadSigned(document)
			if err != nil || received.Schema() != c.schema || received.NamesAuthors() != (c.author != "") {
				t.Fatalf("declared %q (names authors %t) %v, want %q", received.Schema(), received.NamesAuthors(), err, c.schema)
			}
			verified, err := received.Verify(trust)
			if err != nil {
				t.Fatalf("verify: %v", err)
			}
			if version := tagged(t, verified.V1 != nil, verified.V2 != nil); version != c.schema {
				t.Fatalf("verified as %s", version)
			}

			// A v1 entitlement names no authors and a v2 one activates only a
			// named author: neither is installed under the other's rule.
			if _, err := received.Import(filepath.Join(t.TempDir(), "misnamed"), trust, c.other().author, c.device, moment(2026, time.September, 19)); !errors.Is(err, entitlement.ErrAuthorNotNamed) {
				t.Fatalf("installed with the other version's author selection: %v", err)
			}
			root := filepath.Join(t.TempDir(), "entitlement")
			installed, err := received.Import(root, trust, c.author, c.device, moment(2026, time.September, 19))
			if err != nil {
				t.Fatalf("import: %v", err)
			}
			if version := tagged(t, installed.V1 != nil, installed.V2 != nil); version != c.schema {
				t.Fatalf("installed as %s", version)
			}
			reopened, err := entitlement.OpenInstalled(root)
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			if version := tagged(t, reopened.V1 != nil, reopened.V2 != nil); version != c.schema || reopened.Schema() != c.schema {
				t.Fatalf("reopened as %s (%s)", version, reopened.Schema())
			}
			if reopened.Root() != installed.Root() || reopened.Author() != c.author || reopened.Device() != c.device ||
				!reopened.Imported().Equal(moment(2026, time.September, 19)) || !reopened.Released().IsZero() {
				t.Fatalf("unexpected activation: %s %q %q %v %v", reopened.Root(), reopened.Author(), reopened.Device(), reopened.Imported(), reopened.Released())
			}
			granted, err := reopened.Grant(trust)
			if err != nil {
				t.Fatalf("grant: %v", err)
			}
			if version := tagged(t, granted.V1 != nil, granted.V2 != nil); version != c.schema {
				t.Fatalf("granted as %s", version)
			}
			exported := filepath.Join(t.TempDir(), "exported.json")
			if _, err := reopened.Export(exported); err != nil {
				t.Fatalf("export: %v", err)
			}
			if data, err := os.ReadFile(exported); err != nil || !bytes.Equal(data, document) {
				t.Fatalf("export did not write the imported bytes: %v", err)
			}
			if reopened.ID() != c.id {
				t.Fatalf("the installed document is named %q", reopened.ID())
			}
		})
	}
}

// A version no reader supports is refused as unsupported wherever the reader
// meets it: a document, an installation, a store. A document or store is never
// read under the other version's rules, even relabelled as that version.
func TestUnsupportedVersionsAreRefusedAndNeverMigrated(t *testing.T) {
	trust := testTrust(t, entitlement.KeyActive, time.Time{})
	for _, c := range contracts() {
		t.Run(c.name, func(t *testing.T) {
			document := string(c.issue(t, 1, "example-hospital", false))
			for _, relabelled := range []struct {
				schema string
				want   error
			}{{"readmit-entitlement/v9", entitlement.ErrUnsupportedVersion}, {c.other().schema, nil}} {
				received, err := entitlement.ReadSigned([]byte(strings.Replace(document, c.schema, relabelled.schema, 1)))
				if err != nil || received.Schema() != relabelled.schema || received.NamesAuthors() != (relabelled.schema == entitlement.SchemaV2) {
					t.Fatalf("declared %q (names authors %t) %v, want %q", received.Schema(), received.NamesAuthors(), err, relabelled.schema)
				}
				if _, err := received.Verify(trust); err == nil || (relabelled.want != nil && !errors.Is(err, relabelled.want)) {
					t.Errorf("%s relabelled %s verified: %v", c.name, relabelled.schema, err)
				}
				destination := filepath.Join(t.TempDir(), "relabelled")
				if _, err := received.Import(destination, trust, c.author, c.device, moment(2026, time.September, 19)); err == nil || (relabelled.want != nil && !errors.Is(err, relabelled.want)) {
					t.Errorf("%s relabelled %s installed: %v", c.name, relabelled.schema, err)
				}
				if _, err := os.Lstat(destination); !os.IsNotExist(err) {
					t.Errorf("a refused installation left %s behind", destination)
				}
			}

			installed, _ := c.install(t, trust, 1)
			record := filepath.Join(installed.Root(), entitlement.ActivationName)
			original, err := os.ReadFile(record)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(record, []byte(strings.Replace(string(original), c.store, "readmit-entitlement-store/v9", 1)), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := entitlement.OpenInstalled(installed.Root()); !errors.Is(err, entitlement.ErrUnsupportedVersion) {
				t.Errorf("a store of an unsupported version opened: %v", err)
			}
			// This version's activation beside the other version's document
			// is neither version's store.
			if err := os.WriteFile(record, original, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(installed.Root(), entitlement.DocumentName), c.other().issue(t, 1, "example-hospital", false), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := entitlement.OpenInstalled(installed.Root()); !errors.Is(err, entitlement.ErrUnsupportedVersion) {
				t.Errorf("a %s activation opened beside a %s document: %v", c.name, c.other().name, err)
			}
		})
	}
	for _, malformed := range [][]byte{[]byte("not json"), bytes.Repeat([]byte(" "), entitlement.MaxDocumentBytes+1)} {
		if _, err := entitlement.ReadSigned(malformed); err == nil {
			t.Errorf("a malformed document declared a version: %.16q", malformed)
		}
	}
}

// A released activation asserts nothing: it grants nothing, is not renewed or
// released again, and still exports what it was given. The release is kept.
func TestAReleasedActivationAssertsNothingAndStillExports(t *testing.T) {
	trust := testTrust(t, entitlement.KeyActive, time.Time{})
	for _, c := range contracts() {
		t.Run(c.name, func(t *testing.T) {
			installed, document := c.install(t, trust, 1)
			at := moment(2026, time.November, 1)
			if err := installed.Release(at); err != nil {
				t.Fatalf("release: %v", err)
			}
			reopened, err := entitlement.OpenInstalled(installed.Root())
			if err != nil || !reopened.Released().Equal(at) {
				t.Fatalf("the release was not kept: %v %v", reopened.Released(), err)
			}
			if _, err := reopened.Grant(trust); !errors.Is(err, entitlement.ErrReleased) {
				t.Errorf("a released activation still granted: %v", err)
			}
			if err := reopened.Renew(c.issue(t, 2, "example-hospital", false), trust); !errors.Is(err, entitlement.ErrReleased) {
				t.Errorf("a released activation was renewed: %v", err)
			}
			if err := reopened.Release(moment(2026, time.December, 1)); !errors.Is(err, entitlement.ErrReleased) {
				t.Errorf("an activation was released twice: %v", err)
			}
			exported := filepath.Join(t.TempDir(), "held.json")
			if _, err := reopened.Export(exported); err != nil {
				t.Fatalf("a released store refused to export: %v", err)
			}
			if data, err := os.ReadFile(exported); err != nil || !bytes.Equal(data, document) {
				t.Fatalf("a released store exported other bytes: %v", err)
			}
		})
	}
}

// A store follows one organization's lineage within its own version: a later
// issue renews it, and an issue of the other version, the installed issue or
// an earlier one, another organization's, and one signed by a key the trust
// store does not name are each refused by name and leave the store as it was.
func TestRenewalFollowsOneLineageWithinOneVersion(t *testing.T) {
	trust := testTrust(t, entitlement.KeyActive, time.Time{})
	for _, c := range contracts() {
		t.Run(c.name, func(t *testing.T) {
			installed, document := c.install(t, trust, 2)
			for _, refused := range []struct {
				name     string
				document []byte
				want     error
			}{
				{"the other version", c.other().issue(t, 3, "example-hospital", false), entitlement.ErrUnsupportedVersion},
				{"the installed issue", document, entitlement.ErrSuperseded},
				{"an earlier issue", c.issue(t, 1, "example-hospital", false), entitlement.ErrSuperseded},
				{"another organization", c.issue(t, 3, "other-hospital", false), entitlement.ErrDifferentOrganization},
				{"another vendor's key", c.issue(t, 3, "example-hospital", true), entitlement.ErrUnknownKey},
			} {
				if err := installed.Renew(refused.document, trust); !errors.Is(err, refused.want) {
					t.Errorf("%s: renewed: %v", refused.name, err)
				}
			}
			if kept, err := os.ReadFile(filepath.Join(installed.Root(), entitlement.DocumentName)); err != nil || !bytes.Equal(kept, document) {
				t.Fatalf("a refused renewal changed the installed document: %v", err)
			}

			later := c.issue(t, 3, "example-hospital", false)
			if err := installed.Renew(later, trust); err != nil {
				t.Fatalf("renew: %v", err)
			}
			reopened, err := entitlement.OpenInstalled(installed.Root())
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			granted, err := reopened.Grant(trust)
			if err != nil {
				t.Fatalf("grant: %v", err)
			}
			sequence := 0
			if granted.V2 != nil {
				sequence = granted.V2.Claims.Sequence
			} else {
				sequence = granted.V1.Claims.Sequence
			}
			if sequence != 3 {
				t.Fatalf("the renewal was not kept: sequence %d", sequence)
			}
		})
	}
}
