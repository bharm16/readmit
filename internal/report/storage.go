package report

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/bundle"
)

func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func encode(value any) ([]byte, error) {
	data, err := json.Marshal(value, json.Deterministic(true))
	if err != nil {
		return nil, errors.New("cannot encode report evidence")
	}
	return append(data, '\n'), nil
}

func reserve(output string) (string, error) {
	output, err := artifactpath.Destination(output)
	if err != nil {
		return "", err
	}
	if os.Mkdir(output, 0700) != nil {
		return "", errors.New("cannot create report output; destination must be new")
	}
	return output, nil
}

func writeFile(dir, name string, data []byte) error {
	file, err := os.OpenFile(filepath.Join(dir, filepath.FromSlash(name)), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("cannot create report evidence; incomplete output retained")
	}
	_, err = file.Write(data)
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		return errors.New("cannot write report evidence; incomplete output retained")
	}
	return nil
}

// readTree rejects links, devices, oversized input and unindexed empty directories.
// os.Root confines the bounded reads to the selected directory.
func readTree(dir string) (map[string][]byte, error) {
	return readTreeAllowEmptySent(dir, false)
}

// Retained durable jobs may have no sent bytes after a connection failure.
// Empty operational sent/ carries no evidence file and is omitted on copy;
// all other empty directories remain errors, as does sent/ in ordinary input.
func readTreeAllowEmptySent(dir string, durable bool) (map[string][]byte, error) {
	invalid := errors.New("report evidence must be bounded regular files without symlinks or empty directories")
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, invalid
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, invalid
	}
	defer root.Close()
	files, directories := make(map[string][]byte), make(map[string]bool)
	total, entries := 0, 0
	err = fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return invalid
		}
		if name == "." {
			return nil
		}
		entries++
		if entries > maxFiles*2 || len(name) > 200 || strings.Count(name, "/") > 5 || strings.Contains(name, "\\") {
			return invalid
		}
		info, err := entry.Info()
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return invalid
		}
		if info.IsDir() {
			directories[name] = false
			return nil
		}
		if !info.Mode().IsRegular() || info.Size() > maxFileBytes || len(files) >= maxFiles {
			return invalid
		}
		file, err := root.Open(name)
		if err != nil {
			return invalid
		}
		opened, err := file.Stat()
		if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
			file.Close()
			return invalid
		}
		data, err := io.ReadAll(io.LimitReader(file, maxFileBytes+1))
		closeErr := file.Close()
		total += len(data)
		if err != nil || closeErr != nil || len(data) > maxFileBytes || total > maxPacketBytes {
			return invalid
		}
		files[name] = data
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			directories[parent] = true
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for name, populated := range directories {
		if !populated && !(durable && name == "sent" && files["engine.json"] != nil) {
			return nil, invalid
		}
	}
	return files, nil
}

func index(files map[string][]byte) []bundle.Payload {
	names := make([]string, 0, len(files))
	for name := range files {
		if name != "manifest.json" && name != "identity.sha256" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	result := make([]bundle.Payload, 0, len(names))
	for _, name := range names {
		data := files[name]
		result = append(result, bundle.Payload{Path: name, Size: len(data), SHA256: digest(data)})
	}
	return result
}

func copyFiles(files map[string][]byte, prefix, output string) error {
	for name, data := range files {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		name = strings.TrimPrefix(name, prefix)
		if !fs.ValidPath(name) {
			return errors.New("invalid report evidence path")
		}
		if os.MkdirAll(filepath.Join(output, filepath.Dir(filepath.FromSlash(name))), 0700) != nil {
			return errors.New("cannot create report evidence directory")
		}
		if err := writeFile(output, name, data); err != nil {
			return err
		}
	}
	return nil
}
