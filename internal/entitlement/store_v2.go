package entitlement

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
)

// StoreSchemaV2 is the activation record of a v2 entitlement store: the named
// author and the device this installation activated as. It is a separate
// contract because a v1 activation records a device and nothing about whose
// device it is; readmit-entitlement-store/v1 gains no member.
const StoreSchemaV2 = "readmit-entitlement-store/v2"

// ActivationV2 is the local half of a v2 entitlement: which named author this
// installation activated for, on which assigned device, when, and whether that
// activation has since been released. It records no evidence and nothing about
// the machine beyond the identifiers a person supplied.
type ActivationV2 struct {
	Schema   string    `json:"schema"`
	Author   string    `json:"author"`
	Device   string    `json:"device"`
	Imported time.Time `json:"imported"`
	Released time.Time `json:"released,omitzero"`
}

// StoreV2 is an installed v2 entitlement directory. It holds the same two
// files a v1 store holds, under the v2 contracts.
type StoreV2 struct {
	Root       string
	Activation ActivationV2
	Claims     ClaimsV2
	signed     []byte
}

// ImportV2 verifies a received v2 entitlement and installs it for one named
// author on one of that author's assigned devices. Like [Import] it checks
// authenticity and the assignment, never the clock.
func ImportV2(destination string, data []byte, trust Trust, author, device string, at time.Time) (*StoreV2, error) {
	grant, err := VerifyV2(data, trust)
	if err != nil {
		return nil, err
	}
	if err := grant.Assigned(author, device); err != nil {
		return nil, err
	}
	activation := ActivationV2{Schema: StoreSchemaV2, Author: author, Device: device, Imported: at}
	record, err := encodeActivationV2(activation)
	if err != nil {
		return nil, err
	}
	root, err := artifactpath.Destination(destination)
	if err != nil {
		return nil, err
	}
	if err := os.Mkdir(root, 0700); err != nil {
		return nil, errors.New("cannot create entitlement store; destination must be new and parent writable")
	}
	for _, file := range []struct {
		name string
		data []byte
	}{{DocumentName, data}, {ActivationName, record}} {
		if _, err := create(filepath.Join(root, file.name), file.data); err != nil {
			os.RemoveAll(root)
			return nil, err
		}
	}
	return &StoreV2{Root: root, Activation: activation, Claims: grant.Claims, signed: data}, nil
}

// OpenV2 reads an installed v2 entitlement directory without verifying it. A
// store written under v1 is reported as an unsupported version here, exactly
// as [Open] reports one written under v2; neither is migrated.
func OpenV2(path string) (*StoreV2, error) {
	root, err := artifactpath.Directory(path)
	if err != nil {
		return nil, errors.New("an entitlement store must be an existing directory that is not a symbolic link")
	}
	opened, err := os.OpenRoot(root)
	if err != nil {
		return nil, errors.New("cannot open entitlement store directory")
	}
	defer opened.Close()
	record, err := read(opened, ActivationName)
	if err != nil {
		return nil, err
	}
	activation, err := decodeActivationV2(record)
	if err != nil {
		return nil, err
	}
	signed, err := read(opened, DocumentName)
	if err != nil {
		return nil, err
	}
	document, err := DecodeV2(signed)
	if err != nil {
		return nil, err
	}
	return &StoreV2{Root: root, Activation: activation, Claims: document.Entitlement, signed: signed}, nil
}

// Grant verifies the installed entitlement for the author and device this
// installation activated as. A released activation asserts nothing.
func (s *StoreV2) Grant(trust Trust) (GrantV2, error) {
	if !s.Activation.Released.IsZero() {
		return GrantV2{}, ErrReleased
	}
	grant, err := VerifyV2(s.signed, trust)
	if err != nil {
		return GrantV2{}, err
	}
	if err := grant.Assigned(s.Activation.Author, s.Activation.Device); err != nil {
		return GrantV2{}, err
	}
	return grant, nil
}

// Export writes the installed entitlement to a new file, byte for byte as it
// was received, whatever the store's activation or term state.
func (s *StoreV2) Export(destination string) (string, error) {
	return create(destination, s.signed)
}

// Renew installs a later issue of the same organization's v2 entitlement that
// still assigns this device to this author. A reissue that moves the author
// to other devices, or the device to another author, is a transfer: it is
// refused here and imported where it applies.
func (s *StoreV2) Renew(data []byte, trust Trust) error {
	if !s.Activation.Released.IsZero() {
		return ErrReleased
	}
	grant, err := VerifyV2(data, trust)
	if err != nil {
		return err
	}
	if grant.Claims.Organization != s.Claims.Organization {
		return ErrDifferentOrganization
	}
	if grant.Claims.Sequence <= s.Claims.Sequence {
		return ErrSuperseded
	}
	if err := grant.Assigned(s.Activation.Author, s.Activation.Device); err != nil {
		return err
	}
	if err := install(s.Root, DocumentName, data); err != nil {
		return err
	}
	s.signed, s.Claims = data, grant.Claims
	return nil
}

// Release records that this installation handed its activation back so an
// administrator can have the device reassigned. It is the same local record,
// with the same limit, as a v1 release: restoring a copy of the directory
// restores the activation, and assignment is settled by the issuer's reissue.
func (s *StoreV2) Release(at time.Time) error {
	if !s.Activation.Released.IsZero() {
		return ErrReleased
	}
	released := s.Activation
	released.Released = at
	record, err := encodeActivationV2(released)
	if err != nil {
		return err
	}
	if err := install(s.Root, ActivationName, record); err != nil {
		return err
	}
	s.Activation = released
	return nil
}

func decodeActivationV2(data []byte) (ActivationV2, error) {
	activation, err := decode[ActivationV2](data, StoreSchemaV2, activationKind)
	if err != nil {
		return ActivationV2{}, err
	}
	if err := validateActivationV2(activation); err != nil {
		return ActivationV2{}, err
	}
	return activation, nil
}

func encodeActivationV2(activation ActivationV2) ([]byte, error) {
	if err := validateActivationV2(activation); err != nil {
		return nil, err
	}
	return encode(activation, activationKind)
}

func validateActivationV2(activation ActivationV2) error {
	if activation.Schema != StoreSchemaV2 {
		return ErrUnsupportedVersion
	}
	if err := identifier(activation.Author); err != nil {
		return errors.New("author identifier: " + err.Error())
	}
	if err := identifier(activation.Device); err != nil {
		return errors.New("device identifier: " + err.Error())
	}
	if err := instant(activation.Imported); err != nil {
		return errors.New("import time: " + err.Error())
	}
	if !activation.Released.IsZero() {
		if err := instant(activation.Released); err != nil {
			return errors.New("release time: " + err.Error())
		}
		if activation.Released.Before(activation.Imported) {
			return errors.New("an activation is released no earlier than it was imported")
		}
	}
	return nil
}
