package entitlement

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
)

const (
	// StoreSchema is the only activation record contract version this release
	// reads. The record is local installation state, never evidence.
	StoreSchema = "readmit-entitlement-store/v1"

	// DocumentName holds the signed entitlement exactly as it was received.
	// Export writes these bytes back, which is why an exported file still
	// verifies: it is the same file, not a re-encoding of the same claims.
	DocumentName = "entitlement.json"

	// ActivationName holds what this installation decided locally: which device
	// it activated as, when, and whether it has since released that activation.
	ActivationName = "activation.json"

	incompleteSuffix = ".incomplete"
)

// Activation is the local half of an entitlement. It records no evidence, no
// message content and nothing about the machine beyond the device identifier a
// person supplied, so an entitlement store is not sensitive data.
type Activation struct {
	Schema   string    `json:"schema"`
	Device   string    `json:"device"`
	Imported time.Time `json:"imported"`
	Released time.Time `json:"released,omitzero"`
}

// Store is an installed entitlement directory.
type Store struct {
	Root       string
	Activation Activation
	Claims     Claims
	signed     []byte
}

// Import verifies a received entitlement and installs it in a new directory.
// The destination must not exist, and the usual output policy refuses one
// reached through a symbolic link or inside retained evidence.
//
// Import checks authenticity and the device binding, never the clock: an
// entitlement whose term has already ended still installs, and [Store.Grant]
// reports that rather than the import refusing a document it cannot change.
func Import(destination string, data []byte, trust Trust, device string, at time.Time) (*Store, error) {
	grant, err := Verify(data, trust)
	if err != nil {
		return nil, err
	}
	if _, err := grant.Device(device); err != nil {
		return nil, err
	}
	activation := Activation{Schema: StoreSchema, Device: device, Imported: at}
	record, err := encodeActivation(activation)
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
	return &Store{Root: root, Activation: activation, Claims: grant.Claims, signed: data}, nil
}

// Open reads an installed entitlement directory. It reads; it does not verify,
// because verification needs the trust store the caller selected. Nothing here
// rewrites, migrates or repairs what a previous release installed.
func Open(path string) (*Store, error) {
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
	activation, err := decodeActivation(record)
	if err != nil {
		return nil, err
	}
	signed, err := read(opened, DocumentName)
	if err != nil {
		return nil, err
	}
	document, err := Decode(signed)
	if err != nil {
		return nil, err
	}
	return &Store{Root: root, Activation: activation, Claims: document.Entitlement, signed: signed}, nil
}

// Grant verifies the installed entitlement for the device this installation
// activated as. A released activation asserts nothing and is refused here.
func (s *Store) Grant(trust Trust) (Grant, error) {
	if !s.Activation.Released.IsZero() {
		return Grant{}, ErrReleased
	}
	grant, err := Verify(s.signed, trust)
	if err != nil {
		return Grant{}, err
	}
	if _, err := grant.Device(s.Activation.Device); err != nil {
		return Grant{}, err
	}
	return grant, nil
}

// Export writes the installed entitlement to a new file, byte for byte as it
// was received. A released or expired store still exports: the file belongs to
// the organization, and readmit never withholds what it was given.
func (s *Store) Export(destination string) (string, error) {
	return create(destination, s.signed)
}

// Renew installs a later issue of the same organization's entitlement for the
// same device: this is renewal, key rotation and a changed grace window. A
// device transfer is not a renewal — the reissued document names a different
// device, so it is refused here and imported on that device instead.
func (s *Store) Renew(data []byte, trust Trust) error {
	if !s.Activation.Released.IsZero() {
		return ErrReleased
	}
	grant, err := Verify(data, trust)
	if err != nil {
		return err
	}
	if grant.Claims.Organization != s.Claims.Organization {
		return ErrDifferentOrganization
	}
	if grant.Claims.Sequence <= s.Claims.Sequence {
		return ErrSuperseded
	}
	if _, err := grant.Device(s.Activation.Device); err != nil {
		return err
	}
	if err := install(s.Root, DocumentName, data); err != nil {
		return err
	}
	s.signed, s.Claims = data, grant.Claims
	return nil
}

// Release records that this installation handed its activation back, which is
// the local half of a device transfer. After it, the store grants nothing and
// the vendor can reissue the seat to another device.
//
// This is a local record, not a proof. An offline installation cannot show the
// vendor that it stopped using a seat, and restoring a copy of this directory
// restores the activation it held. Seat accounting is settled by the issuer
// when it reissues, not by this file.
func (s *Store) Release(at time.Time) error {
	if !s.Activation.Released.IsZero() {
		return ErrReleased
	}
	released := s.Activation
	released.Released = at
	record, err := encodeActivation(released)
	if err != nil {
		return err
	}
	if err := install(s.Root, ActivationName, record); err != nil {
		return err
	}
	s.Activation = released
	return nil
}

func decodeActivation(data []byte) (Activation, error) {
	activation, err := decode[Activation](data, StoreSchema, activationKind)
	if err != nil {
		return Activation{}, err
	}
	if err := validateActivation(activation); err != nil {
		return Activation{}, err
	}
	return activation, nil
}

func encodeActivation(activation Activation) ([]byte, error) {
	if err := validateActivation(activation); err != nil {
		return nil, err
	}
	return encode(activation, activationKind)
}

func validateActivation(activation Activation) error {
	if activation.Schema != StoreSchema {
		return ErrUnsupportedVersion
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

// write owns exclusive creation: an entitlement file never overwrites one, and
// a failed write leaves nothing behind for a later one to mistake for evidence
// of an interrupted replacement.
func write(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("cannot create the entitlement file; one of that name is already there and is never replaced")
	}
	_, writeErr := file.Write(data)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(path)
		return errors.New("cannot write the entitlement file")
	}
	return nil
}

// create reserves one new file and writes it. The reservation is resolved
// before the write and the resolved path is the one actually created, so an
// alias in the named path cannot place a file somewhere else. It is the one
// writer every entitlement file goes through.
func create(destination string, data []byte) (string, error) {
	path, err := artifactpath.Destination(destination)
	if err != nil {
		return "", err
	}
	if err := write(path, data); err != nil {
		return "", err
	}
	return path, nil
}

// install replaces one file of the store atomically: it is written in full
// beside the one already there and renamed over it, so a reader never observes
// a partial file and a failed write leaves the previous one exactly as it was.
// The incomplete file must not exist, so an interrupted write is retained and
// reported rather than overwritten, and the location is reserved again in case
// the store has since been moved inside retained evidence.
func install(root, name string, data []byte) error {
	incomplete, err := create(filepath.Join(root, name+incompleteSuffix), data)
	if err != nil {
		return err
	}
	if err := os.Rename(incomplete, filepath.Join(filepath.Dir(incomplete), name)); err != nil {
		os.Remove(incomplete)
		return errors.New("cannot replace the entitlement file")
	}
	return nil
}

func read(root *os.Root, name string) ([]byte, error) {
	file, err := root.Open(name)
	if err != nil {
		return nil, errors.New("entitlement store is missing a required file")
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, errors.New("entitlement store files must be regular files")
	}
	data, readErr := io.ReadAll(io.LimitReader(file, MaxDocumentBytes+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(data) > MaxDocumentBytes {
		return nil, errors.New("cannot read entitlement store file")
	}
	return data, nil
}
