package redact

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
)

func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func encode(value any) ([]byte, error) {
	raw, err := json.Marshal(value, json.Deterministic(true))
	if err != nil {
		return nil, errors.New("cannot encode redaction artifact")
	}
	return append(raw, '\n'), nil
}

func readLocal(path string, limit int) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > int64(limit) {
		return nil, errors.New("redaction input must be a bounded regular file, without symlinks")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("cannot read redaction input")
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("redaction input must be regular")
	}
	raw, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil || len(raw) > limit {
		return nil, errors.New("redaction input exceeds read limit")
	}
	return raw, nil
}

func destination(path string, protected []string) (string, error) {
	if len(protected) == 0 {
		return "", errors.New("artifact destination requires a source boundary")
	}
	sources := make([]os.FileInfo, 0, len(protected))
	for _, source := range protected {
		info, err := os.Stat(source)
		if err != nil {
			return "", errors.New("cannot inspect protected artifact")
		}
		sources = append(sources, info)
	}
	resolved, err := artifactpath.Destination(path, sources...)
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(resolved); !os.IsNotExist(err) {
		return "", errors.New("redaction destination must be new")
	}
	return resolved, nil
}

var (
	errCreateFile = errors.New("cannot create redaction artifact file")
	errWriteFile  = errors.New("cannot write redaction artifact file; incomplete output retained")
)

// tree reads every file of a redaction artifact or workspace: bounded regular
// files with bounded names, no empty directory and no link. It refuses in the
// sentences redaction has always used.
func tree(dir string) (map[string][]byte, error) {
	dir, err := artifactpath.Directory(dir)
	if err != nil {
		return nil, err
	}
	var refused error
	named := func(name string) bool {
		if len(name) > 256 {
			refused = errors.New("artifact path exceeds limit")
			return false
		}
		return true
	}
	directories := 0
	files, err := artifactdir.Read(dir, artifactdir.Layout{
		AllowDirectory: func(name string) bool {
			if !named(name) {
				return false
			}
			if directories >= 1024 {
				refused = errors.New("artifact exceeds directory limit")
				return false
			}
			directories++
			return true
		},
		AllowFile:    named,
		AllowEmpty:   func(string, map[string][]byte) bool { return false },
		MaxFiles:     maxPacketFiles,
		MaxFileBytes: maxReviewBytes,
		MaxBytes:     maxPacketBytes,
		Refusals: artifactdir.Refusals{
			Directory: errors.New("cannot inspect artifact directory"),
			File:      errors.New("cannot read redaction input"),
			Link:      errors.New("artifact cannot contain symlinks"),
			Special:   errors.New("redaction input must be a bounded regular file, without symlinks"),
			Irregular: errors.New("redaction input must be regular"),
			Files:     errors.New("artifact exceeds file limit"),
			Size:      errors.New("redaction input must be a bounded regular file, without symlinks"),
			Empty:     errors.New("unexpected empty artifact directory"),
		},
	})
	if refused != nil {
		return nil, refused
	}
	return files, err
}

func fileNames(files map[string][]byte) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func identity(schema string, files map[string][]byte) string {
	return artifactdir.Identity(schema, files)
}

func protectedPaths(local localState) []string {
	paths := []string{}
	for _, source := range local.Sources {
		paths = append(paths, source.Path)
	}
	return paths
}

func regularReviewName(name string) bool {
	return name == "review.json" || name == "identity.sha256" || name == "spec.json" || strings.HasPrefix(name, "case/")
}
