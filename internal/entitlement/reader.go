package entitlement

import (
	"encoding/json/v2"
	"errors"
	"time"
)

// This file is the one place a declared contract version chooses a reader.
// Every caller that accepts either version — the command line, and the
// operation guard the window reaches entitlements through — verifies a
// received document, installs one and opens an installed store here, and
// renders from the version-tagged result. Each version keeps its own decoder,
// verifier and store contract: a v1 document or store is decided by the v1
// reader and a v2 one by the v2 reader, a version neither reads is refused as
// unsupported, and nothing is migrated or read under the other version's rules.

// Signed is one signed entitlement document as this machine received it, not
// yet verified: its exact bytes and the contract version they declare. Reading
// it decides nothing but which reader applies; [Signed.Verify] and
// [Signed.Import] hand the bytes to that reader, which verifies them.
type Signed struct {
	schema string
	data   []byte
}

// ReadSigned reads the contract version a received document declares, without
// reading anything else of it. It accepts nothing: a version no reader
// supports is refused when the document is verified or installed.
func ReadSigned(data []byte) (Signed, error) {
	if len(data) > MaxDocumentBytes {
		return Signed{}, errors.New(documentKind + " exceeds its size limit")
	}
	var declared struct {
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(data, &declared); err != nil {
		return Signed{}, errors.New("invalid " + documentKind)
	}
	return Signed{schema: declared.Schema, data: data}, nil
}

// Schema is the contract version the document declares, whether or not a
// reader in this release supports it.
func (s Signed) Schema() string { return s.schema }

// NamesAuthors reports whether the declared version activates named authors
// on their assigned devices, as v2 does, rather than binding devices, as v1
// does. A version no reader supports names none, and is refused as unsupported
// when it is verified or installed.
func (s Signed) NamesAuthors() bool { return s.schema == SchemaV2 }

// Verified is one document verified by the reader of the version it declares.
// Exactly one of V1 and V2 is set, and which one is set is the version: a
// caller renders the grant of that version and never infers one from the other.
type Verified struct {
	V1 *Grant
	V2 *GrantV2
}

// Verify verifies the document by its own version's verifier, against the
// trust store the caller selected.
func (s Signed) Verify(trust Trust) (Verified, error) {
	switch s.schema {
	case Schema:
		grant, err := Verify(s.data, trust)
		if err != nil {
			return Verified{}, err
		}
		return Verified{V1: &grant}, nil
	case SchemaV2:
		grant, err := VerifyV2(s.data, trust)
		if err != nil {
			return Verified{}, err
		}
		return Verified{V2: &grant}, nil
	}
	return Verified{}, ErrUnsupportedVersion
}

// Import verifies the document and installs it in a new directory as its own
// version's store: for a device the document binds under v1, and for a named
// author on one of their assigned devices under v2. A v1 entitlement names no
// authors, so naming one is refused rather than ignored.
func (s Signed) Import(destination string, trust Trust, author, device string, at time.Time) (Installed, error) {
	switch s.schema {
	case Schema:
		if author != "" {
			return Installed{}, ErrAuthorNotNamed
		}
		store, err := Import(destination, s.data, trust, device, at)
		if err != nil {
			return Installed{}, err
		}
		return Installed{V1: store}, nil
	case SchemaV2:
		store, err := ImportV2(destination, s.data, trust, author, device, at)
		if err != nil {
			return Installed{}, err
		}
		return Installed{V2: store}, nil
	}
	return Installed{}, ErrUnsupportedVersion
}

// Installed is an installed entitlement store of either version. Exactly one
// of V1 and V2 is set, and which one is set is the version of both its signed
// document and its activation record; its methods act through that version's
// own store.
type Installed struct {
	V1 *Store
	V2 *StoreV2
}

// OpenInstalled reads an installed entitlement directory with the reader of
// the version it was written under, without verifying it. A store neither
// reader accepts is reported as that reader's refusal, never migrated.
func OpenInstalled(path string) (Installed, error) {
	v1, err := Open(path)
	if err == nil {
		return Installed{V1: v1}, nil
	}
	if !errors.Is(err, ErrUnsupportedVersion) {
		return Installed{}, err
	}
	v2, err := OpenV2(path)
	if err != nil {
		return Installed{}, err
	}
	return Installed{V2: v2}, nil
}

// Schema is the contract version of the installed document.
func (s Installed) Schema() string {
	if s.V2 != nil {
		return SchemaV2
	}
	return Schema
}

// Root is the store's resolved directory.
func (s Installed) Root() string {
	if s.V2 != nil {
		return s.V2.Root
	}
	return s.V1.Root
}

// ID names the installed document, read without verifying it.
func (s Installed) ID() string {
	if s.V2 != nil {
		return s.V2.Claims.ID
	}
	return s.V1.Claims.ID
}

// Author is the named author the store was activated for. A v1 store names
// none, so it is empty.
func (s Installed) Author() string {
	if s.V2 != nil {
		return s.V2.Activation.Author
	}
	return ""
}

// Device is the device the store was activated as.
func (s Installed) Device() string {
	if s.V2 != nil {
		return s.V2.Activation.Device
	}
	return s.V1.Activation.Device
}

// Imported is when the store was activated.
func (s Installed) Imported() time.Time {
	if s.V2 != nil {
		return s.V2.Activation.Imported
	}
	return s.V1.Activation.Imported
}

// Released is when the activation was released, or zero while it holds.
func (s Installed) Released() time.Time {
	if s.V2 != nil {
		return s.V2.Activation.Released
	}
	return s.V1.Activation.Released
}

// Grant verifies the installed document for what the store was activated as,
// by its own version's rule. A released activation asserts nothing.
func (s Installed) Grant(trust Trust) (Verified, error) {
	if s.V2 != nil {
		grant, err := s.V2.Grant(trust)
		if err != nil {
			return Verified{}, err
		}
		return Verified{V2: &grant}, nil
	}
	grant, err := s.V1.Grant(trust)
	if err != nil {
		return Verified{}, err
	}
	return Verified{V1: &grant}, nil
}

// Renew installs a later issue of the same organization's entitlement, of the
// store's own version, for what the store was activated as. An issue of the
// other version is refused as unsupported: a store follows one lineage and is
// never renewed into another contract.
func (s Installed) Renew(data []byte, trust Trust) error {
	if s.V2 != nil {
		return s.V2.Renew(data, trust)
	}
	return s.V1.Renew(data, trust)
}

// Release records that this installation handed its activation back.
func (s Installed) Release(at time.Time) error {
	if s.V2 != nil {
		return s.V2.Release(at)
	}
	return s.V1.Release(at)
}

// Export writes the installed document to a new file, byte for byte as it was
// received, whatever the store's activation or term state, and returns the
// resolved path actually written.
func (s Installed) Export(destination string) (string, error) {
	if s.V2 != nil {
		return s.V2.Export(destination)
	}
	return s.V1.Export(destination)
}
