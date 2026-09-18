package testrunner

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
	"sort"
	"strings"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
	"github.com/bharm16/readmit/internal/replay"
)

func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func targetIdentity(target replay.TargetRecord) string {
	data, _ := encode(target)
	return digest(data)
}
func encode(value any) ([]byte, error) {
	data, err := json.Marshal(value, json.Deterministic(true))
	if err != nil {
		return nil, errors.New("cannot encode test evidence")
	}
	return append(data, '\n'), nil
}

func readLocal(path string, max int) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > int64(max) {
		return nil, errors.New("test input must be a bounded regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("cannot open test input")
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("test input must be a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(max)+1))
	if err != nil || len(data) > max {
		return nil, errors.New("cannot read bounded test input")
	}
	return data, nil
}

func reserve(output string, source os.FileInfo) (string, error) {
	var err error
	if source != nil {
		output, err = artifactpath.Outside(source, output)
		if err != nil {
			return "", err
		}
	} else {
		// Even malformed specs must not allow an error artifact to be placed
		// inside any finalized bundle. Resolve raw parents before cleaning .. .
		parent, leaf := filepath.Split(output)
		if leaf == "" || leaf == "." || leaf == ".." {
			return "", errors.New("test output requires a new directory")
		}
		if parent == "" {
			parent = "."
		}
		parent, err = filepath.EvalSymlinks(parent)
		if err != nil {
			return "", errors.New("cannot resolve test output parent")
		}
		parent, err = filepath.Abs(parent)
		if err != nil {
			return "", errors.New("cannot resolve test output parent")
		}
		output = filepath.Join(parent, leaf)
	}
	for current := filepath.Dir(output); ; current = filepath.Dir(current) {
		if _, err := os.Lstat(filepath.Join(current, "identity.sha256")); !os.IsNotExist(err) {
			return "", errors.New("test output must be outside immutable evidence")
		}
		if filepath.Dir(current) == current {
			break
		}
	}
	if err := os.Mkdir(output, 0700); err != nil {
		return "", errors.New("cannot create test output; destination must be new")
	}
	return output, nil
}

func retain(dir, path string, raw []byte) (*bundle.Payload, error) {
	file, err := os.OpenFile(filepath.Join(dir, path), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, errors.New("cannot create test evidence")
	}
	_, writeErr := file.Write(raw)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		return nil, errors.New("cannot write test evidence")
	}
	return &bundle.Payload{Path: path, Size: len(raw), SHA256: digest(raw)}, nil
}

func finish(dir string, result Result) (*Artifact, error) {
	data, err := encode(result)
	if err != nil || len(data) > maxResultBytes {
		return nil, errors.New("test result exceeds size limit")
	}
	if _, err := retain(dir, "result.json", data); err != nil {
		return nil, err
	}
	files, err := readDirectory(dir)
	if err != nil {
		return nil, err
	}
	identity := directoryIdentity(files)
	if _, err := retain(dir, "identity.sha256", []byte(identity+"\n")); err != nil {
		return nil, err
	}
	return Open(dir)
}

func readDirectory(dir string) (map[string][]byte, error) {
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("test result must be a regular directory")
	}
	files := make(map[string][]byte)
	total := 0
	err = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return errors.New("cannot inspect test evidence")
		}
		if path == dir {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return errors.New("invalid test evidence path")
		}
		rel = filepath.ToSlash(rel)
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("test evidence cannot contain symlinks")
		}
		if entry.IsDir() {
			if rel != "run" && rel != "run/payloads" {
				return errors.New("unexpected test evidence directory")
			}
			return nil
		}
		if len(files) >= 16016 {
			return errors.New("test result exceeds file limit")
		}
		data, err := readLocal(path, 16<<20)
		if err != nil {
			return err
		}
		total += len(data)
		if total > 128<<20 {
			return errors.New("test result exceeds byte limit")
		}
		files[rel] = data
		return nil
	})
	return files, err
}

func directoryIdentity(files map[string][]byte) string {
	h := sha256.New()
	_, _ = io.WriteString(h, Schema+"\n")
	paths := make([]string, 0, len(files))
	for path := range files {
		if path != "identity.sha256" {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	for _, path := range paths {
		for _, data := range [][]byte{[]byte(path), files[path]} {
			var size [8]byte
			binary.BigEndian.PutUint64(size[:], uint64(len(data)))
			_, _ = h.Write(size[:])
			_, _ = h.Write(data)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

func validDigest(value string) bool {
	data, err := hex.DecodeString(value)
	return err == nil && len(data) == sha256.Size && strings.ToLower(value) == value
}
