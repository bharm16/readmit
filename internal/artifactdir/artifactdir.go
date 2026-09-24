// Package artifactdir owns the filesystem mechanics shared by Readmit's
// immutable directory artifacts. It is the only writer of a sealed directory:
// a family of artifacts declares its layout, the rule its completion record is
// made by and the sentences its failures are reported in, and this package
// reserves the directory, writes and syncs every member, writes the completion
// record last and syncs every directory entry the artifact is found through,
// from a whole map of files (Write) or member by member (Create). It also owns
// bounded, confined reads of a declared layout and the ADR-0002 identity
// algorithm. Domain packages still own their schemas and semantic validation.
package artifactdir

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"
)

// Layout states the finite filesystem shape a domain reader accepts, and the
// shape a sealed family's writer may make.
type Layout struct {
	Noun               string
	AllowedDirectories []string
	// Nested names the directories that each hold a nested packet, such as a
	// case inside a review or a run inside a result: the directory and
	// everything below it are admitted by prefix, within this layout's
	// bounds, and the nested packet's own reader verifies what it holds.
	Nested        []string
	RequiredFiles []string
	AllowFile     func(string) bool
	// AllowDirectory admits a directory neither AllowedDirectories nor
	// Nested names, for a reader of an evidence tree whose directories are not
	// known in advance. Read asks it, and AllowFile, once for each directory
	// and file it meets, in the order it walks them.
	AllowDirectory func(string) bool
	// AllowEmpty, when set, refuses every directory that holds no file at any
	// depth unless it accepts it. It is asked once the whole artifact is read,
	// with every file read. When nil an admitted empty directory is accepted.
	AllowEmpty   func(directory string, files map[string][]byte) bool
	MaxFiles     int
	MaxFileBytes int
	MaxBytes     int
	// Refusals are the sentences a reader whose refusals predate Read reports
	// in place of Read's own.
	Refusals Refusals
}

// Refusals replace Read's own refusals. Each field left nil keeps Read's
// sentence for that refusal.
type Refusals struct {
	// Directory is a directory of the artifact that cannot be opened or
	// listed.
	Directory error
	// File is a file that cannot be opened or read.
	File error
	// Link is a symbolic link.
	Link error
	// Special is a device, pipe, socket or other entry that is neither a
	// regular file, a directory nor a link. When nil, Link is.
	Special error
	// Irregular is an opened file that is not the regular file listed.
	Irregular error
	// Files is a file past MaxFiles.
	Files error
	// Size is a file past MaxFileBytes, or contents past MaxBytes.
	Size error
	// Empty is a directory AllowEmpty refuses.
	Empty error
}

// nested reports whether name is one of the layout's nested packet
// directories or lies below one; strict excludes the directory itself.
func (l Layout) nested(name string, strict bool) bool {
	for _, prefix := range l.Nested {
		if !strict && name == prefix || strings.HasPrefix(name, prefix+"/") {
			return true
		}
	}
	return false
}

// admitsDirectory reports whether the layout admits the directory name.
func (l Layout) admitsDirectory(name string) bool {
	return slices.Contains(l.AllowedDirectories, name) || l.nested(name, false) || l.AllowDirectory != nil && l.AllowDirectory(name)
}

// admitsFile reports whether the layout admits the file name.
func (l Layout) admitsFile(name string) bool {
	return l.nested(name, true) || l.AllowFile != nil && l.AllowFile(name)
}

// Read returns every accepted file from one artifact directory. Directories
// and files must be explicitly admitted; symbolic links and special files are
// always refused, and a file replaced between being listed and being opened is
// refused rather than read. Domain packages validate the returned bytes.
func Read(directory string, layout Layout) (map[string][]byte, error) {
	noun := layout.Noun
	if noun == "" {
		noun = "artifact"
	}
	errText := func(message string) error { return errors.New(strings.ReplaceAll(message, "artifact", noun)) }
	refuse := func(declared error, message string) error {
		if declared != nil {
			return declared
		}
		return errText(message)
	}
	refusals := layout.Refusals
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errText("artifact must be a regular directory")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, refuse(refusals.Directory, "cannot open artifact directory")
	}
	defer root.Close()
	files := make(map[string][]byte)
	// directories records every directory read below the artifact and whether
	// any file lies below it, for AllowEmpty.
	directories := make(map[string]bool)
	total := 0
	var walk func(*os.Root, string) error
	walk = func(current *os.Root, prefix string) error {
		return fs.WalkDir(current.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return refuse(refusals.Directory, "cannot read artifact directory")
			}
			fullName := name
			if prefix != "" && name != "." {
				fullName = prefix + "/" + name
			}
			if entry.IsDir() {
				if name == "." {
					return nil
				}
				if !layout.admitsDirectory(fullName) {
					return errText("unexpected artifact directory")
				}
				directories[fullName] = false
				child, err := current.OpenRoot(name)
				if err != nil {
					return refuse(refusals.Directory, "cannot open artifact directory")
				}
				err = walk(child, fullName)
				closeErr := child.Close()
				if err != nil {
					return err
				}
				if closeErr != nil {
					return refuse(refusals.Directory, "cannot read artifact directory")
				}
				return fs.SkipDir
			}
			if entry.Type() != 0 {
				declared := refusals.Link
				if entry.Type()&fs.ModeSymlink == 0 && refusals.Special != nil {
					declared = refusals.Special
				}
				return refuse(declared, "artifact files must be regular files, never symlinks")
			}
			if !layout.admitsFile(fullName) {
				return errText("unexpected artifact file")
			}
			if layout.MaxFiles <= 0 || len(files) >= layout.MaxFiles {
				return refuse(refusals.Files, "artifact file limit exceeded")
			}
			listed, err := entry.Info()
			if err != nil {
				return refuse(refusals.File, "cannot open artifact file")
			}
			file, err := current.Open(name)
			if err != nil {
				return refuse(refusals.File, "cannot open artifact file")
			}
			info, statErr := file.Stat()
			if statErr != nil || !info.Mode().IsRegular() || !os.SameFile(listed, info) {
				file.Close()
				return refuse(refusals.Irregular, "artifact files must be regular files")
			}
			remaining := layout.MaxBytes - total
			limit := min(layout.MaxFileBytes, remaining)
			if limit < 0 {
				limit = 0
			}
			data, readErr := io.ReadAll(io.LimitReader(file, int64(limit)+1))
			closeErr := file.Close()
			if readErr != nil || closeErr != nil {
				return refuse(refusals.File, "cannot read artifact file")
			}
			total += len(data)
			if len(data) > layout.MaxFileBytes || total > layout.MaxBytes {
				return refuse(refusals.Size, "artifact contents exceed size limit")
			}
			files[fullName] = data
			for parent := path.Dir(fullName); parent != "."; parent = path.Dir(parent) {
				directories[parent] = true
			}
			return nil
		})
	}
	err = walk(root, "")
	if err != nil {
		return nil, err
	}
	if layout.AllowEmpty != nil {
		for name, populated := range directories {
			if !populated && !layout.AllowEmpty(name, files) {
				return nil, refuse(refusals.Empty, "unexpected empty artifact directory")
			}
		}
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
