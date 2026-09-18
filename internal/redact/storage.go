package redact

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

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

func relative(base, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return base + string(os.PathSeparator) + path
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

func writeFile(dir, name string, raw []byte) error {
	file, err := os.OpenFile(filepath.Join(dir, filepath.FromSlash(name)), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return errors.New("cannot create redaction artifact file")
	}
	_, err = file.Write(raw)
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		return errors.New("cannot write redaction artifact file; incomplete output retained")
	}
	return nil
}

func tree(dir string) (map[string][]byte, error) {
	dir, err := artifactpath.Directory(dir)
	if err != nil {
		return nil, err
	}

	files := map[string][]byte{}
	directories := []string{}
	total := 0
	err = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return errors.New("cannot inspect artifact directory")
		}
		if path == dir {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("artifact cannot contain symlinks")
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return errors.New("cannot resolve artifact relative path")
		}
		if len(rel) > 256 {
			return errors.New("artifact path exceeds limit")
		}
		if entry.IsDir() {
			if len(directories) >= 1024 {
				return errors.New("artifact exceeds directory limit")
			}
			directories = append(directories, filepath.ToSlash(rel))
			return nil
		}
		if len(files) >= maxPacketFiles {
			return errors.New("artifact exceeds file limit")
		}
		raw, err := readLocal(path, min(maxReviewBytes, maxPacketBytes-total))
		if err != nil {
			return err
		}
		total += len(raw)
		if total > maxPacketBytes {
			return errors.New("artifact exceeds byte limit")
		}
		files[filepath.ToSlash(rel)] = raw
		return nil
	})
	if err == nil {
		for _, dir := range directories {
			populated := false
			for name := range files {
				if strings.HasPrefix(name, dir+"/") {
					populated = true
					break
				}
			}
			if !populated {
				return nil, errors.New("unexpected empty artifact directory")
			}
		}
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
	h := sha256.New()
	h.Write([]byte(schema + "\n"))
	var size [8]byte
	for _, name := range fileNames(files) {
		if name == "identity.sha256" {
			continue
		}
		for _, raw := range [][]byte{[]byte(name), files[name]} {
			binary.BigEndian.PutUint64(size[:], uint64(len(raw)))
			h.Write(size[:])
			h.Write(raw)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
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
