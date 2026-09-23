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
	"path/filepath"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/artifactpath"
)

var (
	ErrCreateDirectory = errors.New("cannot create artifact directory")
	ErrCreateFile      = errors.New("cannot create artifact file")
	ErrSyncFile        = errors.New("cannot sync artifact file; incomplete artifact retained")
	ErrCancelled       = errors.New("artifact write cancelled; any incomplete artifact is retained")
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
}

// Write creates one immutable directory artifact. Files are written in bytewise
// path order and synced; identity.sha256 is derived and written last as the
// completion marker. An interrupted write is deliberately retained incomplete.
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
	if err := os.Mkdir(path, 0700); err != nil {
		return "", fmt.Errorf("%w; destination must be new and parent writable", ErrCreateDirectory)
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return "", errors.New("cannot open new artifact directory")
	}
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
		if err := WriteFile(root, name, files[name]); err != nil {
			return "", err
		}
	}
	identity := ""
	completion := options.Completion
	if completion == nil {
		identity = Identity(options.Domain, files)
		completion = []byte(identity + "\n")
	}
	if err := WriteFile(root, "identity.sha256", completion); err != nil {
		return "", err
	}
	if err := SyncDirectory(root, "."); err != nil {
		return "", errors.New("cannot sync artifact directory; incomplete artifact retained")
	}
	return identity, nil
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
	if err := WriteFile(root, incompleteName, data); err != nil {
		return err
	}
	return root.Rename(incompleteName, finalName)
}

// WriteFile creates and syncs one new regular file below root. It never
// replaces an existing member and returns no successfully written partial as
// complete.
func WriteFile(root *os.Root, name string, data []byte) error {
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return ErrCreateFile
	}
	if err := WriteFileSync(file, data); err != nil {
		file.Close()
		return ErrSyncFile
	}
	if err := file.Close(); err != nil {
		return ErrSyncFile
	}
	return nil
}

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
