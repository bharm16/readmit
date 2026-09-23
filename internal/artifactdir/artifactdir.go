// Package artifactdir owns the filesystem mechanics shared by Readmit's
// immutable directory artifacts. Domain packages still own their schemas and
// semantic validation; this package owns exclusive synced writes, bounded
// regular-file reads, layout confinement, and the ADR-0002 identity algorithm.
package artifactdir

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/artifactpath"
)

var (
	ErrCreateDirectory = errors.New("cannot create artifact directory")
	ErrCreateFile      = errors.New("cannot create artifact file")
	ErrSyncFile        = errors.New("cannot sync artifact file; incomplete artifact retained")
	// ErrSyncDirectory is a directory sync that failed once the completion
	// record was written. Every file, that record among them, is written and
	// synced, so the artifact may already open; what is not confirmed is that
	// every name it is found through survives a power loss.
	ErrSyncDirectory = errors.New("cannot sync artifact directory; the artifact was written in full but a power loss could still lose it")
	ErrCancelled     = errors.New("artifact write cancelled; any incomplete artifact is retained")
)

// Layout states the finite filesystem shape a domain reader accepts.
type Layout struct {
	Noun               string
	AllowedDirectories []string
	RequiredFiles      []string
	AllowFile          func(string) bool
	MaxFiles           int
	MaxFileBytes       int
	MaxBytes           int
}

type WriteOptions struct {
	Domain      string
	Directories []string
	// Completion supplies an established domain-specific identity marker.
	// When nil, Write computes the ADR-0002 directory identity from Domain.
	Completion []byte
	// Durability is Durable unless the caller states that the artifact is
	// Scratch.
	Durability Durability
}

// Durability is the explicit choice a writer makes about flushing what it
// creates to the device. The zero value is Durable, so a writer that states
// nothing keeps the shared discipline.
type Durability uint8

const (
	// Durable syncs every file before its handle closes and, through Write,
	// every directory naming one before the write answers.
	Durable Durability = iota
	// Scratch creates the same bytes through the same exclusive creates and
	// renames, and syncs nothing. It is only for a throwaway workspace that
	// its owner removes before it answers and keeps nothing from except
	// through a Durable write, so a power loss can cost only work that was
	// never going to be kept.
	Scratch
)

// Write creates one immutable directory artifact. Files are written in bytewise
// path order and synced; identity.sha256 is derived and written last as the
// completion marker. It answers only once every directory naming one of them,
// or the artifact itself, is synced too. An interrupted write is deliberately
// retained incomplete; one whose directories cannot be synced after the marker
// answers ErrSyncDirectory, since what it retained is complete.
func Write(path string, options WriteOptions, files map[string][]byte) (string, error) {
	return WriteContext(context.Background(), path, options, files)
}

// WriteContext is Write for work a person can cancel. A write is one synced
// file after another, so the cancellation is observed between files: one
// arriving before the destination exists creates nothing, and one arriving
// later stops the write with what it wrote retained incomplete, exactly like
// any other interrupted write. Neither ever writes the completion marker.
func WriteContext(ctx context.Context, path string, options WriteOptions, files map[string][]byte) (string, error) {
	if _, exists := files["identity.sha256"]; exists {
		return "", errors.New("artifact files cannot supply their own completion marker")
	}
	if ctx.Err() != nil {
		return "", ErrCancelled
	}
	path, err := artifactpath.Destination(path)
	if err != nil {
		return "", err
	}
	parent, root, err := Reserve(path)
	if err != nil {
		return "", err
	}
	defer parent.Close()
	defer root.Close()
	for _, name := range options.Directories {
		if name == "" || name == "." || filepath.IsAbs(name) {
			return "", errors.New("invalid artifact directory name")
		}
		if err := root.Mkdir(filepath.ToSlash(name), 0700); err != nil {
			return "", errors.New("cannot create artifact member directory")
		}
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if ctx.Err() != nil {
			return "", ErrCancelled
		}
		if err := options.Durability.WriteFile(root, name, files[name]); err != nil {
			return "", err
		}
	}
	identity := ""
	completion := options.Completion
	if completion == nil {
		identity = Identity(options.Domain, files)
		completion = []byte(identity + "\n")
	}
	if err := options.Durability.WriteFile(root, "identity.sha256", completion); err != nil {
		return "", err
	}
	if err := options.Durability.SyncEntries(root, parent, options.Directories); err != nil {
		return "", ErrSyncDirectory
	}
	return identity, nil
}

// Reserve creates the new directory at path, already resolved by
// artifactpath, for an artifact its writer builds, and opens it. The folder
// that holds it is opened first and returned open, because SyncEntries syncs
// it last: a folder the writer cannot open is refused before anything is
// created, and the directory is made inside that opened folder, so the folder
// synced is the one naming it. The caller closes both.
func Reserve(path string) (parent, root *os.Root, err error) {
	parent, err = os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, nil, fmt.Errorf("%w; destination must be new and parent readable and writable", ErrCreateDirectory)
	}
	if err := parent.Mkdir(filepath.Base(path), 0700); err != nil {
		parent.Close()
		return nil, nil, fmt.Errorf("%w; destination must be new and parent writable", ErrCreateDirectory)
	}
	root, err = parent.OpenRoot(filepath.Base(path))
	if err != nil {
		parent.Close()
		return nil, nil, errors.New("cannot open new artifact directory")
	}
	return parent, root, nil
}

// SyncDirectory syncs the directory name below root, so the names it holds
// survive a power loss. Every directory sync a writer makes goes through it,
// so the platform rule is stated once: on Windows it syncs nothing.
func SyncDirectory(root *os.Root, name string) error {
	return syncDirectory(root, name)
}

// syncDirectory is the directory sync every writer makes. It is the seam
// package tests take to see which directories a write syncs, and when.
var syncDirectory = flushDirectory

// Directories lists, in bytewise order, every directory below an artifact that
// holds one of files: the member directories SyncEntries syncs for an artifact
// written file by file.
func Directories(files map[string][]byte) []string {
	seen := make(map[string]bool)
	for name := range files {
		for directory := path.Dir(name); directory != "."; directory = path.Dir(directory) {
			seen[directory] = true
		}
	}
	directories := make([]string, 0, len(seen))
	for directory := range seen {
		directories = append(directories, directory)
	}
	slices.Sort(directories)
	return directories
}

// SyncEntries syncs every directory holding a name the artifact is found
// through: each member directory, the artifact directory and the folder that
// holds it. A write reports success only after all of them, so no name it made
// is lost to a power loss after it answered. Write makes this sync itself; a
// writer that builds an artifact file by file, with root and parent from
// Reserve, makes it once its completion record is written, and reports a
// failure as ErrSyncDirectory does.
func SyncEntries(root, parent *os.Root, directories []string) error {
	return Durable.SyncEntries(root, parent, directories)
}

// SyncEntries is the package SyncEntries unless d is Scratch, which syncs
// nothing.
func (d Durability) SyncEntries(root, parent *os.Root, directories []string) error {
	if d == Scratch {
		return nil
	}
	for _, name := range directories {
		if err := syncDirectory(root, filepath.ToSlash(name)); err != nil {
			return err
		}
	}
	if err := syncDirectory(root, "."); err != nil {
		return err
	}
	return syncDirectory(parent, ".")
}

// DurableFile is what one durable write needs from the file it writes: a full
// write and a sync. *os.File satisfies it, and so does a size-limited stand-in
// for a full disk, which is the seam package tests use.
type DurableFile interface {
	io.Writer
	Sync() error
}

// WriteFileSync writes data to an already-open file with the shared durable
// discipline: one full write checked for a short write, then Sync. Close
// remains the caller's, so a caller that holds a new file open across earlier
// steps — a replacement created before the bytes it will hold are known —
// still finishes through the same rule.
func WriteFileSync(file DurableFile, data []byte) error {
	n, err := file.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = file.Sync()
	}
	return err
}

// Publish writes data to incompleteName and renames it onto finalName inside
// root only after every byte is synced, so finalName never holds a partial
// record. The incomplete file is a new file written once, the same way
// WriteFile writes a member, and it must not already exist. A failure retains
// the incomplete file; a caller whose policy is to remove it removes it
// itself.
func Publish(root *os.Root, incompleteName, finalName string, data []byte) error {
	return Durable.Publish(root, incompleteName, finalName, data)
}

// Publish is the package Publish, syncing the incomplete file unless d is
// Scratch.
func (d Durability) Publish(root *os.Root, incompleteName, finalName string, data []byte) error {
	if err := d.WriteFile(root, incompleteName, data); err != nil {
		return err
	}
	return root.Rename(incompleteName, finalName)
}

// WriteFile creates and syncs one new regular file below root. It never
// replaces an existing member and returns no successfully written partial as
// complete.
func WriteFile(root *os.Root, name string, data []byte) error {
	return Durable.WriteFile(root, name, data)
}

// WriteFile is the package WriteFile, syncing the file unless d is Scratch.
// A Scratch file is still created exclusively and checked for a short write.
func (d Durability) WriteFile(root *os.Root, name string, data []byte) error {
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return ErrCreateFile
	}
	if err := WriteFileSync(seamedFile{file, d}, data); err != nil {
		file.Close()
		return ErrSyncFile
	}
	if err := file.Close(); err != nil {
		return ErrSyncFile
	}
	return nil
}

// Sync flushes a file its writer made unless d is Scratch. It is the same sync
// WriteFile makes, for a writer that holds its own file open, such as an
// append-only log.
func (d Durability) Sync(file *os.File) error {
	if d == Scratch {
		return nil
	}
	return syncFile(file)
}

// syncFile is the file sync every Durable WriteFile and Sync makes. It is the
// seam tests take to see which files a writer built on them syncs, and with
// what bytes.
var syncFile = (*os.File).Sync

// seamedFile is a file WriteFile is writing, synced as its durability says.
type seamedFile struct {
	*os.File
	durability Durability
}

func (f seamedFile) Sync() error { return f.durability.Sync(f.File) }

// Read returns every accepted file from one artifact directory. Directories
// and files must be explicitly admitted; symbolic links and special files are
// always refused. Domain packages validate the returned bytes.
func Read(path string, layout Layout) (map[string][]byte, error) {
	noun := layout.Noun
	if noun == "" {
		noun = "artifact"
	}
	errText := func(message string) error { return errors.New(strings.ReplaceAll(message, "artifact", noun)) }
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errText("artifact must be a regular directory")
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, errText("cannot open artifact directory")
	}
	defer root.Close()
	allowedDirectories := make(map[string]bool, len(layout.AllowedDirectories)+1)
	allowedDirectories["."] = true
	for _, name := range layout.AllowedDirectories {
		allowedDirectories[name] = true
	}
	files := make(map[string][]byte)
	total := 0
	var walk func(*os.Root, string) error
	walk = func(current *os.Root, prefix string) error {
		return fs.WalkDir(current.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return errText("cannot read artifact directory")
			}
			fullName := name
			if prefix != "" && name != "." {
				fullName = prefix + "/" + name
			}
			if entry.IsDir() {
				if name == "." {
					return nil
				}
				if !allowedDirectories[fullName] {
					return errText("unexpected artifact directory")
				}
				child, err := current.OpenRoot(name)
				if err != nil {
					return errText("cannot open artifact directory")
				}
				err = walk(child, fullName)
				closeErr := child.Close()
				if err != nil {
					return err
				}
				if closeErr != nil {
					return errText("cannot read artifact directory")
				}
				return fs.SkipDir
			}
			if entry.Type() != 0 {
				return errText("artifact files must be regular files, never symlinks")
			}
			if layout.AllowFile == nil || !layout.AllowFile(fullName) {
				return errText("unexpected artifact file")
			}
			if layout.MaxFiles <= 0 || len(files) >= layout.MaxFiles {
				return errText("artifact file limit exceeded")
			}
			file, err := current.Open(name)
			if err != nil {
				return errText("cannot open artifact file")
			}
			info, statErr := file.Stat()
			if statErr != nil || !info.Mode().IsRegular() {
				file.Close()
				return errText("artifact files must be regular files")
			}
			remaining := layout.MaxBytes - total
			limit := min(layout.MaxFileBytes, remaining)
			if limit < 0 {
				limit = 0
			}
			data, readErr := io.ReadAll(io.LimitReader(file, int64(limit)+1))
			closeErr := file.Close()
			if readErr != nil || closeErr != nil {
				return errText("cannot read artifact file")
			}
			total += len(data)
			if len(data) > layout.MaxFileBytes || total > layout.MaxBytes {
				return errText("artifact contents exceed size limit")
			}
			files[fullName] = data
			return nil
		})
	}
	err = walk(root, "")
	if err != nil {
		return nil, err
	}
	for _, name := range layout.RequiredFiles {
		if _, ok := files[name]; !ok {
			return nil, errText("artifact is incomplete: required file missing")
		}
	}
	return files, nil
}

// Identity hashes a domain prefix and sorted, length-delimited relative names
// and contents. The completion marker itself is never identity input.
func Identity(domain string, files map[string][]byte) string {
	names := make([]string, 0, len(files))
	for name := range files {
		if name != "identity.sha256" {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	hash := sha256.New()
	hash.Write([]byte(domain + "\n"))
	var size [8]byte
	for _, name := range names {
		binary.BigEndian.PutUint64(size[:], uint64(len(name)))
		hash.Write(size[:])
		hash.Write([]byte(name))
		binary.BigEndian.PutUint64(size[:], uint64(len(files[name])))
		hash.Write(size[:])
		hash.Write(files[name])
	}
	return hex.EncodeToString(hash.Sum(nil))
}
