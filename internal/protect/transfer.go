package protect

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
)

const (
	// TransferSchema and IndexSchema are the only transfer contracts this
	// release reads. Adding a member to either means a new version string with
	// a reader for every older version, never an added member here.
	TransferSchema = "readmit-transfer/v1"
	IndexSchema    = "readmit-transfer-index/v1"

	// Cipher and Derivation are the only algorithms this release accepts. A
	// package naming another one is refused: unsupported is not a pass, and
	// there is no negotiation that could be talked down to a weaker choice.
	Cipher     = "aes-256-gcm"
	Derivation = "hkdf-sha256"

	// DescriptorName is the one plaintext file of a package. It names no
	// packed file and holds no key material: a package's own index is
	// sensitive data, so it is encrypted like the content it describes.
	DescriptorName = "transfer.json"

	// IndexEntryID is the entry holding the encrypted index.
	IndexEntryID = "index"

	entrySuffix = ".bin"

	// MinKeyBytes is a length floor on the material the declared program
	// prints. readmit cannot measure entropy and does not claim to: this is a
	// length check, never a strength assessment.
	MinKeyBytes = 32

	saltBytes     = 32
	keyCheckBytes = 32
	keyBytes      = 32
	nonceBytes    = 12
	packageIDLen  = 32

	// A package is bounded before anything is read or written, so an oversized
	// input is refused and packed in parts rather than partly packed.
	MaxEntries        = 8192
	MaxEntryBytes     = 16 << 20
	MaxPackageBytes   = 256 << 20
	maxDescriptorSize = 8 << 20
	maxIndexSize      = 8 << 20
	maxEntryNameBytes = 1024
)

// ErrUnsupportedPackage reports a transfer package written under a contract
// version this release does not read.
var ErrUnsupportedPackage = errors.New("unsupported transfer package version")

// Limitations is what an encrypted transfer package establishes and what it
// does not. Every command that writes, opens, inspects or discards a package
// repeats it, because a control that is trusted past its reach is worse than
// no control at all.
const Limitations = "Encryption covers the content and the index of this package only, at rest and in transit, against someone who does not hold the key. It is not authentication of who wrote the package, not protection from a holder of the key, and not a control over the plaintext sources it was made from or the plaintext a recipient writes when opening it. The declared storage control is recorded as declared and is never verified by readmit. Removing a package unlinks it; it does not erase it from a solid-state device, a copy-on-write filesystem, a snapshot, a backup, a replica or a volume with wear levelling, and readmit cannot destroy a key it never held."

// DeletionLimitations is what discarding a package establishes. It is stated
// separately because deletion is the control operators most often over-read.
const DeletionLimitations = "Discarding unlinks the files this package declares and removes its directory. It does not overwrite the bytes, and it establishes nothing about copies: a solid-state device may retain the blocks until it reuses them, a copy-on-write filesystem and a snapshot keep the previous version, a backup or replica already holds its own copy, and a recipient who received the package still has it. Encryption is the control that outlives deletion, and only for as long as the key is controlled; readmit never held the key and cannot destroy it."

// Entry is one encrypted member of a package. The digest and size are an
// integrity check over the ciphertext that names a truncated or replaced file
// before decryption is attempted. A digest is not source authentication: the
// authenticated-encryption tag, and only it, ties an entry to the key.
type Entry struct {
	ID     string `json:"id"`
	Nonce  []byte `json:"nonce"`
	Bytes  int64  `json:"bytes"`
	SHA256 []byte `json:"sha256"`
}

// Package is the plaintext descriptor of an encrypted transfer package. It
// names the control whose key opens the package and nothing else about the
// evidence inside: no file name, no size of any packed file, no content.
type Package struct {
	Schema      string    `json:"schema"`
	Package     string    `json:"package"`
	CreatedAt   time.Time `json:"created_at"`
	Control     string    `json:"control"`
	Generation  int       `json:"generation"`
	Cipher      string    `json:"cipher"`
	Derivation  string    `json:"derivation"`
	Salt        []byte    `json:"salt"`
	KeyCheck    []byte    `json:"key_check"`
	RetainUntil time.Time `json:"retain_until,omitzero"`
	Index       Entry     `json:"index"`
	Entries     []Entry   `json:"entries"`
}

// Retention reports what the declared retention period says at a given time. A
// package that declares none is not-declared, never within-retention: an
// undeclared retention state is not a passing one.
func (p Package) Retention(now time.Time) RetentionState {
	if p.RetainUntil.IsZero() {
		return RetentionNotDeclared
	}
	if now.After(p.RetainUntil) {
		return PastRetention
	}
	return WithinRetention
}

// IndexEntry names one packed file. These are the sensitive names, and they
// live only inside the encrypted index.
type IndexEntry struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Bytes int64  `json:"bytes"`
}

// Index is the encrypted index of a package. NotRead counts the entries the
// pack deliberately did not read — a symbolic link, a device, a socket — so a
// package never implies it holds more than it does.
type Index struct {
	Schema  string       `json:"schema"`
	Package string       `json:"package"`
	Entries []IndexEntry `json:"entries"`
	NotRead int          `json:"entries_not_read"`
}

// Source is one file to pack, under the name the package records for it.
type Source struct {
	Name string
	Data []byte
}

// DecodePackage reads a transfer descriptor. Unknown members and unknown
// versions are errors; there is no migration and no repair.
func DecodePackage(data []byte) (Package, error) {
	if len(data) > maxDescriptorSize {
		return Package{}, errors.New("transfer descriptor exceeds its size limit")
	}
	var declared struct {
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(data, &declared); err != nil {
		return Package{}, errors.New("invalid transfer descriptor")
	}
	if declared.Schema != TransferSchema {
		return Package{}, ErrUnsupportedPackage
	}
	var descriptor Package
	if err := json.Unmarshal(data, &descriptor, json.RejectUnknownMembers(true)); err != nil {
		return Package{}, errors.New("invalid transfer descriptor")
	}
	if err := validatePackage(descriptor); err != nil {
		return Package{}, err
	}
	return descriptor, nil
}

// DecodeIndex reads a decrypted package index under the same strict contract.
func DecodeIndex(data []byte) (Index, error) {
	if len(data) > maxIndexSize {
		return Index{}, errors.New("transfer index exceeds its size limit")
	}
	var declared struct {
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(data, &declared); err != nil {
		return Index{}, errors.New("invalid transfer index")
	}
	if declared.Schema != IndexSchema {
		return Index{}, ErrUnsupportedPackage
	}
	var index Index
	if err := json.Unmarshal(data, &index, json.RejectUnknownMembers(true)); err != nil {
		return Index{}, errors.New("invalid transfer index")
	}
	if err := validateIndex(index); err != nil {
		return Index{}, err
	}
	return index, nil
}

func validatePackage(descriptor Package) error {
	if descriptor.Schema != TransferSchema {
		return ErrUnsupportedPackage
	}
	if err := packageID(descriptor.Package); err != nil {
		return err
	}
	if descriptor.Cipher != Cipher || descriptor.Derivation != Derivation {
		return errors.New("the transfer package declares an encryption this release does not read")
	}
	if len(descriptor.Salt) != saltBytes || len(descriptor.KeyCheck) != keyCheckBytes {
		return errors.New("the transfer package declares a derivation input of the wrong length")
	}
	if err := identifier(descriptor.Control); err != nil {
		return errors.New("transfer control name: " + err.Error())
	}
	if descriptor.Generation < 1 || descriptor.Generation > maxGeneration {
		return errors.New("transfer key generation: must be between 1 and " + strconv.Itoa(maxGeneration))
	}
	if descriptor.CreatedAt.IsZero() {
		return errors.New("transfer creation time: must be recorded")
	}
	if descriptor.Index.ID != IndexEntryID {
		return errors.New("the transfer package does not declare its index")
	}
	if len(descriptor.Entries) > MaxEntries {
		return errors.New("a transfer package holds at most " + strconv.Itoa(MaxEntries) + " entries")
	}
	total := int64(0)
	seen := make(map[string]struct{}, len(descriptor.Entries)+1)
	for _, entry := range append([]Entry{descriptor.Index}, descriptor.Entries...) {
		if err := validateEntry(entry); err != nil {
			return err
		}
		if _, repeated := seen[entry.ID]; repeated {
			return errors.New("the transfer package declares one entry twice")
		}
		seen[entry.ID] = struct{}{}
		total += entry.Bytes
		if total > MaxPackageBytes {
			return errors.New("the transfer package declares more than this release reads")
		}
	}
	for i := 1; i < len(descriptor.Entries); i++ {
		if descriptor.Entries[i-1].ID >= descriptor.Entries[i].ID {
			return errors.New("transfer entries must be uniquely identified and sorted")
		}
	}
	return nil
}

func validateEntry(entry Entry) error {
	if entry.ID != IndexEntryID {
		if len(entry.ID) != 5 || entry.ID[0] != 'e' {
			return errors.New("transfer entry identifier: must be the recorded form")
		}
		for _, r := range entry.ID[1:] {
			if r < '0' || r > '9' {
				return errors.New("transfer entry identifier: must be the recorded form")
			}
		}
	}
	if len(entry.Nonce) != nonceBytes || len(entry.SHA256) != sha256.Size {
		return errors.New("transfer entry: declares a nonce or digest of the wrong length")
	}
	// An AES-GCM ciphertext is the plaintext plus a 16-byte tag, so an empty
	// declared size cannot describe any entry this release wrote.
	if entry.Bytes < 16 || entry.Bytes > MaxEntryBytes+16 {
		return errors.New("transfer entry: declares a size outside this release's bounds")
	}
	return nil
}

func validateIndex(index Index) error {
	if index.Schema != IndexSchema {
		return ErrUnsupportedPackage
	}
	if err := packageID(index.Package); err != nil {
		return err
	}
	if len(index.Entries) > MaxEntries {
		return errors.New("a transfer index holds at most " + strconv.Itoa(MaxEntries) + " entries")
	}
	if index.NotRead < 0 {
		return errors.New("a transfer index cannot report a negative unread count")
	}
	seen := make(map[string]struct{}, len(index.Entries))
	for _, entry := range index.Entries {
		if err := EntryName(entry.Name); err != nil {
			return err
		}
		if entry.Bytes < 0 || entry.Bytes > MaxEntryBytes {
			return errors.New("transfer index entry: declares a size outside this release's bounds")
		}
		if _, repeated := seen[entry.Name]; repeated {
			return errors.New("the transfer index names one packed file twice")
		}
		seen[entry.Name] = struct{}{}
	}
	// A name that is a path prefix of another cannot be written back out: one
	// entry would be a file where the next needs a directory. readmit refuses
	// to pack such a set, but a package this release did not write is refused
	// here too, before an open creates anything, rather than part way through.
	for _, entry := range index.Entries {
		elements := strings.Split(entry.Name, "/")
		for depth := 1; depth < len(elements); depth++ {
			if _, clash := seen[strings.Join(elements[:depth], "/")]; clash {
				return errors.New("the transfer index names one packed file inside another")
			}
		}
	}
	return nil
}

func packageID(value string) error {
	if len(value) != packageIDLen {
		return errors.New("transfer package identifier: must be the recorded form")
	}
	if _, err := hex.DecodeString(value); err != nil {
		return errors.New("transfer package identifier: must be the recorded form")
	}
	return nil
}

// EntryName is the rule for a name a package records and an open writes back
// out. Every element is checked by artifactpath, which owns that rule, so a
// recorded name can never reach outside the directory the operator named.
func EntryName(name string) error {
	if name == "" || len(name) > maxEntryNameBytes {
		return errors.New("a packed name must be a relative path inside the package")
	}
	if !filepath.IsLocal(name) || strings.ContainsRune(name, '\\') {
		return errors.New("a packed name must be a relative path inside the package")
	}
	for _, element := range strings.Split(name, "/") {
		if err := artifactpath.EntryName(element); err != nil {
			return err
		}
	}
	return nil
}
