package protect

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/secret"
)

// Collect reads the bounded contents of the named paths under the local names a
// package records for them. A named file is recorded under its own base name; a
// named directory contributes its base name and the relative path beneath it,
// so every recorded name is a relative path that an open can write back out.
//
// This is deliberately not [secret.Collect], which keys files by the path the
// operator typed because a leakage scan reports the location a person named. A
// package records a name it must later create, so absolute paths and parent
// traversal cannot be recorded here at all. Entries that are not regular files
// are counted, never opened, so walking a tree cannot leave it, loop or block.
// Every bound is applied to what the tree declares before anything is read.
func Collect(roots []string) ([]Source, int, error) {
	type candidate struct{ name, path string }
	candidates := make([]candidate, 0, len(roots))
	notRead, total := 0, int64(0)
	// Every recorded name begins with the base name of the root it came from,
	// so distinct bases cannot produce a name that is a path prefix of another
	// — and a prefix is not merely a duplicate: a package holding both "a" and
	// "a/b" writes a file where an open must then create a directory, which
	// would leave a valid-looking package that can never be reopened. Within
	// one root a file and a directory cannot share a name, so the roots are
	// where this is decided. The per-name check behind it is a safety net.
	bases := make(map[string]struct{}, len(roots))
	taken := make(map[string]struct{})
	declare := func(name, path string, info os.FileInfo) error {
		if err := EntryName(name); err != nil {
			return err
		}
		if _, repeated := taken[name]; repeated {
			return errors.New("two paths to pack would be recorded under the same name")
		}
		if info.Size() > MaxEntryBytes {
			return errors.New("a file to pack exceeds the package's per-file size limit")
		}
		if len(candidates) >= MaxEntries || total+info.Size() > MaxPackageBytes {
			return errors.New("the paths to pack exceed the package's limits; pack them in parts")
		}
		total += info.Size()
		taken[name] = struct{}{}
		candidates = append(candidates, candidate{name: name, path: path})
		return nil
	}
	for _, root := range roots {
		resolved, err := artifactpath.Resolve(root)
		if err != nil {
			return nil, 0, errors.New("a path to pack could not be resolved")
		}
		info, err := os.Stat(resolved)
		if err != nil {
			return nil, 0, errors.New("a path to pack could not be read")
		}
		base := filepath.Base(resolved)
		if err := EntryName(base); err != nil {
			return nil, 0, err
		}
		if _, repeated := bases[base]; repeated {
			return nil, 0, errors.New("two paths to pack share a final name, so one would be recorded inside the other")
		}
		bases[base] = struct{}{}
		if !info.IsDir() {
			if !info.Mode().IsRegular() {
				notRead++
				continue
			}
			if err := declare(base, resolved, info); err != nil {
				return nil, 0, err
			}
			continue
		}
		walkErr := filepath.WalkDir(resolved, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return errors.New("a path to pack could not be read")
			}
			if entry.IsDir() {
				return nil
			}
			if !entry.Type().IsRegular() {
				notRead++
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return errors.New("a path to pack could not be read")
			}
			relative, err := filepath.Rel(resolved, path)
			if err != nil {
				return errors.New("a path to pack could not be read")
			}
			return declare(base+"/"+filepath.ToSlash(relative), path, info)
		})
		if walkErr != nil {
			return nil, 0, walkErr
		}
	}
	slices.SortFunc(candidates, func(a, b candidate) int { return strings.Compare(a.name, b.name) })
	sources := make([]Source, 0, len(candidates))
	for _, entry := range candidates {
		data, err := readLocal(entry.path, MaxEntryBytes)
		if err != nil {
			return nil, 0, errors.New("a file to pack could not be read")
		}
		sources = append(sources, Source{Name: entry.name, Data: data})
	}
	return sources, notRead, nil
}

// Pack writes a new encrypted transfer package and returns its descriptor.
//
// The sources are read, never modified: original evidence is immutable, so
// encryption produces a new artifact beside it rather than rewriting it. No
// plaintext temporary copy is written anywhere: every byte that leaves this
// function is ciphertext, written straight into the destination that
// artifactpath reserved, owner-only, and a package that cannot be completed is
// removed rather than left looking like one.
//
// The key is read from the operator's declared program for the duration of this
// call and is never written, logged or returned.
func Pack(ctx context.Context, control Control, sources []Source, notRead int, output string, at time.Time) (Package, error) {
	if err := validateControl(control); err != nil {
		return Package{}, err
	}
	if control.State != Active {
		return Package{}, errors.New("the protection control is retired; it opens the packages it wrote and writes no new one")
	}
	if len(sources) == 0 {
		return Package{}, errors.New("a transfer package must hold at least one file")
	}
	if notRead < 0 {
		return Package{}, errors.New("a transfer package cannot report a negative unread count")
	}
	identity := make([]byte, packageIDLen/2)
	salt := make([]byte, saltBytes)
	if _, err := rand.Read(identity); err != nil {
		return Package{}, errors.New("cannot generate a package identity")
	}
	if _, err := rand.Read(salt); err != nil {
		return Package{}, errors.New("cannot generate a derivation salt")
	}
	content, check, err := deriveFrom(ctx, control, salt)
	if err != nil {
		return Package{}, err
	}
	aead, err := sealer(content)
	if err != nil {
		return Package{}, err
	}
	descriptor := Package{
		Schema:     TransferSchema,
		Package:    hex.EncodeToString(identity),
		CreatedAt:  stamp(at),
		Control:    control.Name,
		Generation: control.Generation,
		Cipher:     Cipher,
		Derivation: Derivation,
		Salt:       salt,
		KeyCheck:   check,
		Entries:    make([]Entry, 0, len(sources)),
	}
	if until, declared := control.RetainUntil(at); declared {
		descriptor.RetainUntil = until
	}
	index := Index{Schema: IndexSchema, Package: descriptor.Package, Entries: make([]IndexEntry, 0, len(sources)), NotRead: notRead}
	ciphertext := make(map[string][]byte, len(sources)+1)
	for position, source := range sources {
		id := fmt.Sprintf("e%04d", position+1)
		entry, sealed, err := seal(aead, descriptor.Package, id, source.Data)
		if err != nil {
			return Package{}, err
		}
		descriptor.Entries = append(descriptor.Entries, entry)
		index.Entries = append(index.Entries, IndexEntry{ID: id, Name: source.Name, Bytes: int64(len(source.Data))})
		ciphertext[id] = sealed
	}
	if err := validateIndex(index); err != nil {
		return Package{}, err
	}
	encoded, err := json.Marshal(index, json.Deterministic(true))
	if err != nil {
		return Package{}, errors.New("cannot encode the transfer index")
	}
	sealedIndex, sealedIndexBytes, err := seal(aead, descriptor.Package, IndexEntryID, encoded)
	if err != nil {
		return Package{}, err
	}
	descriptor.Index = sealedIndex
	ciphertext[IndexEntryID] = sealedIndexBytes
	if err := validatePackage(descriptor); err != nil {
		return Package{}, err
	}
	manifest, err := json.Marshal(descriptor, json.Deterministic(true))
	if err != nil {
		return Package{}, errors.New("cannot encode the transfer descriptor")
	}
	manifest = append(manifest, '\n')
	root, err := artifactpath.Destination(output)
	if err != nil {
		return Package{}, err
	}
	if err := os.Mkdir(root, 0700); err != nil {
		return Package{}, errors.New("cannot create the transfer package; the destination must be new and writable")
	}
	write := func() error {
		for _, id := range slices.Sorted(maps.Keys(ciphertext)) {
			if err := writeOwnerOnly(filepath.Join(root, id+entrySuffix), ciphertext[id]); err != nil {
				return err
			}
		}
		return writeOwnerOnly(filepath.Join(root, DescriptorName), manifest)
	}
	if err := write(); err != nil {
		os.RemoveAll(root)
		return Package{}, err
	}
	return descriptor, nil
}

// Open decrypts a package into a new directory and returns what it held.
//
// The plaintext it writes is protected by the destination's own storage control
// and by an owner-only file mode, and by nothing else: opening a package ends
// the protection the package carried.
func Open(ctx context.Context, control Control, source, output string) (Package, Index, error) {
	descriptor, root, err := ReadPackage(source)
	if err != nil {
		return Package{}, Index{}, err
	}
	if err := validateControl(control); err != nil {
		return Package{}, Index{}, err
	}
	if descriptor.Control != control.Name {
		return Package{}, Index{}, errors.New("the package was written under a different protection control than the one named")
	}
	content, check, err := deriveFrom(ctx, control, descriptor.Salt)
	if err != nil {
		return Package{}, Index{}, err
	}
	if subtle.ConstantTimeCompare(check, descriptor.KeyCheck) != 1 {
		if descriptor.Generation != control.Generation {
			return Package{}, Index{}, errors.New("the package records an earlier key generation than this control now reads, and that key does not open it")
		}
		return Package{}, Index{}, errors.New("the package was not written with the key this control reads")
	}
	aead, err := sealer(content)
	if err != nil {
		return Package{}, Index{}, err
	}
	encoded, err := openEntry(aead, root, descriptor.Package, descriptor.Index)
	if err != nil {
		return Package{}, Index{}, err
	}
	index, err := DecodeIndex(encoded)
	if err != nil {
		return Package{}, Index{}, err
	}
	if index.Package != descriptor.Package {
		return Package{}, Index{}, errors.New("the package index belongs to a different package")
	}
	if len(index.Entries) != len(descriptor.Entries) {
		return Package{}, Index{}, errors.New("the package index does not describe the entries the descriptor declares")
	}
	files := make([][]byte, len(index.Entries))
	for position, entry := range index.Entries {
		if entry.ID != descriptor.Entries[position].ID {
			return Package{}, Index{}, errors.New("the package index does not describe the entries the descriptor declares")
		}
		data, err := openEntry(aead, root, descriptor.Package, descriptor.Entries[position])
		if err != nil {
			return Package{}, Index{}, err
		}
		if int64(len(data)) != entry.Bytes {
			return Package{}, Index{}, errors.New("a decrypted entry is not the size the package index records")
		}
		files[position] = data
	}
	destination, err := artifactpath.Destination(output)
	if err != nil {
		return Package{}, Index{}, err
	}
	if err := os.Mkdir(destination, 0700); err != nil {
		return Package{}, Index{}, errors.New("cannot create the output directory; the destination must be new and writable")
	}
	if err := writeTree(destination, index.Entries, files); err != nil {
		os.RemoveAll(destination)
		return Package{}, Index{}, err
	}
	return descriptor, index, nil
}

// ReadPackage reads and verifies one package descriptor without a key. It
// reports what the package declares about itself and never what it holds.
func ReadPackage(source string) (Package, string, error) {
	root, err := artifactpath.Directory(source)
	if err != nil {
		return Package{}, "", errors.New("a transfer package must be a regular directory")
	}
	data, err := readLocal(filepath.Join(root, DescriptorName), maxDescriptorSize)
	if err != nil {
		return Package{}, "", errors.New("the transfer package must hold a readable descriptor within its size limit")
	}
	descriptor, err := DecodePackage(data)
	if err != nil {
		return Package{}, "", err
	}
	return descriptor, root, nil
}

// Discard unlinks exactly the files a package declares and removes its
// directory. A directory holding anything the descriptor does not declare is
// refused before anything is removed, so this can only remove what readmit
// wrote. What removal does and does not establish is [DeletionLimitations].
//
// A package still inside its declared retention period is refused unless the
// caller overrides it, so a declared retention gates the destructive action
// rather than being reported after it. A package that declares no retention
// period is not gated, and the caller is told the state was never declared
// rather than that it was satisfied.
func Discard(source string, at time.Time, overrideRetention bool) (Package, int, error) {
	descriptor, root, err := ReadPackage(source)
	if err != nil {
		return Package{}, 0, err
	}
	if descriptor.Retention(at) == WithinRetention && !overrideRetention {
		return Package{}, 0, errors.New("the package is declared retained until " + descriptor.RetainUntil.UTC().Format(time.RFC3339) + "; nothing was removed")
	}
	declared := map[string]struct{}{DescriptorName: {}}
	for _, entry := range append([]Entry{descriptor.Index}, descriptor.Entries...) {
		declared[entry.ID+entrySuffix] = struct{}{}
	}
	present, err := os.ReadDir(root)
	if err != nil {
		return Package{}, 0, errors.New("the transfer package could not be read")
	}
	for _, entry := range present {
		if _, ok := declared[entry.Name()]; !ok || !entry.Type().IsRegular() {
			return Package{}, 0, errors.New("the package directory holds something the descriptor does not declare; nothing was removed")
		}
	}
	if len(present) != len(declared) {
		return Package{}, 0, errors.New("the package directory does not hold every file the descriptor declares; nothing was removed")
	}
	removed := 0
	for name := range declared {
		if err := os.Remove(filepath.Join(root, name)); err != nil {
			return Package{}, removed, errors.New("the transfer package was only partly removed")
		}
		removed++
	}
	if err := os.Remove(root); err != nil {
		return Package{}, removed, errors.New("the transfer package files were removed; its directory was not")
	}
	return descriptor, removed, nil
}

// Mask is what readmit prints wherever key material would otherwise appear. It
// is the one mask [secret.Mask] defines, because a key and a credential are the
// same kind of thing to every rendering in readmit: absent.
const Mask = secret.Mask

// ReadKey runs the control's declared program and returns what it printed, as a
// [secret.Value] that masks itself under every formatting verb and refuses to
// be serialized. It exists so a caller can establish that the declared store
// still answers for a control without encrypting anything; the material is
// never written, logged or recorded, here or anywhere else.
func ReadKey(ctx context.Context, control Control) (secret.Value, error) {
	if err := validateControl(control); err != nil {
		return secret.Value{}, err
	}
	material, err := control.Locator().Read(ctx)
	if err != nil {
		return secret.Value{}, errors.New("the key could not be read from its declared store")
	}
	if len(material.Expose()) < MinKeyBytes {
		return secret.Value{}, errors.New("the declared store returned fewer than " + strconv.Itoa(MinKeyBytes) + " bytes of key material")
	}
	return material, nil
}

// deriveFrom reads the key the control references and derives this package's
// content key and key check from it. The material is bounded, never written and
// never returned; readmit cannot overwrite the copy Go's allocator made of it,
// and the documentation says so rather than claiming the memory is scrubbed.
func deriveFrom(ctx context.Context, control Control, salt []byte) ([]byte, []byte, error) {
	if len(salt) != saltBytes {
		return nil, nil, errors.New("the transfer package declares a derivation input of the wrong length")
	}
	material, err := ReadKey(ctx, control)
	if err != nil {
		return nil, nil, err
	}
	content, err := hkdf.Key(sha256.New, material.Expose(), salt, TransferSchema+" content", keyBytes)
	if err != nil {
		return nil, nil, errors.New("cannot derive the package content key")
	}
	check, err := hkdf.Key(sha256.New, material.Expose(), salt, TransferSchema+" key-check", keyCheckBytes)
	if err != nil {
		return nil, nil, errors.New("cannot derive the package key check")
	}
	return content, check, nil
}

func sealer(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errors.New("cannot prepare the package cipher")
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, errors.New("cannot prepare the package cipher")
	}
	return aead, nil
}

// associated binds every entry to its contract version, its package and its
// entry identifier, so an entry cannot be moved between packages or renamed
// into another entry's place without failing authentication.
func associated(identity, id string) []byte {
	return []byte(TransferSchema + "\x00" + identity + "\x00" + id)
}

func seal(aead cipher.AEAD, identity, id string, plaintext []byte) (Entry, []byte, error) {
	if len(plaintext) > MaxEntryBytes {
		return Entry{}, nil, errors.New("a file to pack exceeds the package's per-file size limit")
	}
	nonce := make([]byte, nonceBytes)
	if _, err := rand.Read(nonce); err != nil {
		return Entry{}, nil, errors.New("cannot generate an entry nonce")
	}
	sealed := aead.Seal(nil, nonce, plaintext, associated(identity, id))
	digest := sha256.Sum256(sealed)
	return Entry{ID: id, Nonce: nonce, Bytes: int64(len(sealed)), SHA256: digest[:]}, sealed, nil
}

// openEntry reads one entry, checks it against the size and digest the
// descriptor records, then authenticates and decrypts it. The digest names a
// truncated or replaced file before any decryption is attempted; it is an
// integrity check over ciphertext and is not source authentication.
func openEntry(aead cipher.AEAD, root, identity string, entry Entry) ([]byte, error) {
	sealed, err := readLocal(filepath.Join(root, entry.ID+entrySuffix), MaxEntryBytes+16)
	if err != nil {
		return nil, errors.New("a package entry is missing or could not be read")
	}
	digest := sha256.Sum256(sealed)
	if int64(len(sealed)) != entry.Bytes || !bytes.Equal(digest[:], entry.SHA256) {
		return nil, errors.New("a package entry does not match the size and digest the descriptor records")
	}
	plaintext, err := aead.Open(nil, entry.Nonce, sealed, associated(identity, entry.ID))
	if err != nil {
		return nil, errors.New("a package entry failed authentication after the key check passed; the package was altered")
	}
	return plaintext, nil
}

// writeTree creates the decrypted files under names the package recorded. Every
// directory is created exclusively inside the destination that artifactpath
// reserved and this call created a moment ago, so nothing here can join its way
// out of it or reuse a directory it did not make.
func writeTree(destination string, entries []IndexEntry, files [][]byte) error {
	made := map[string]struct{}{"": {}}
	for position, entry := range entries {
		if err := EntryName(entry.Name); err != nil {
			return err
		}
		elements := strings.Split(entry.Name, "/")
		for depth := range elements[:len(elements)-1] {
			relative := strings.Join(elements[:depth+1], "/")
			if _, done := made[relative]; done {
				continue
			}
			if err := os.Mkdir(filepath.Join(destination, filepath.FromSlash(relative)), 0700); err != nil {
				return errors.New("cannot create a directory for a packed file")
			}
			made[relative] = struct{}{}
		}
		if err := writeOwnerOnly(filepath.Join(destination, filepath.FromSlash(entry.Name)), files[position]); err != nil {
			return err
		}
	}
	return nil
}

// writeOwnerOnly creates one new owner-only file. Creation is exclusive: an
// existing name is a failure, never an overwrite. On a filesystem without POSIX
// modes the mode is not an access control, which the documentation states.
func writeOwnerOnly(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("cannot create a package file; the destination must be new and writable")
	}
	_, writeErr := file.Write(data)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		return errors.New("cannot write a package file")
	}
	return nil
}

// readLocal reads one bounded regular file. It re-checks the opened file rather
// than trusting the earlier stat, so a path that changed underneath is refused.
// The first check is deliberately os.Lstat rather than os.Stat: a package
// descriptor or entry that is a symbolic link is refused rather than followed,
// because a package must be exactly the bytes its directory holds.
func readLocal(path string, limit int) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("input must be a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("cannot open input file")
	}
	info, err = file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, errors.New("input must be a regular file")
	}
	data, readErr := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(data) > limit {
		return nil, errors.New("input cannot be read within its size limit")
	}
	return data, nil
}
