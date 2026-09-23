package testrunner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/bharm16/readmit/internal/artifactdir"
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
	output, err := artifactpath.Destination(output, source)
	if err != nil {
		return "", err
	}
	if err := os.Mkdir(output, 0700); err != nil {
		return "", errors.New("cannot create test output; destination must be new")
	}
	return output, nil
}

func retain(dir, path string, raw []byte, durability artifactdir.Durability) (*bundle.Payload, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, errors.New("cannot create test evidence")
	}
	defer root.Close()
	err = durability.WriteFile(root, path, raw)
	if errors.Is(err, artifactdir.ErrCreateFile) {
		return nil, errors.New("cannot create test evidence")
	}
	if err != nil {
		return nil, errors.New("cannot write test evidence")
	}
	return &bundle.Payload{Path: path, Size: len(raw), SHA256: digest(raw)}, nil
}

func finish(dir string, result Result, durability artifactdir.Durability) (*Artifact, error) {
	data, err := encode(result)
	if err != nil || len(data) > maxResultBytes {
		return nil, errors.New("test result exceeds size limit")
	}
	if _, err := retain(dir, "result.json", data, durability); err != nil {
		return nil, err
	}
	files, err := readDirectory(dir)
	if err != nil {
		return nil, err
	}
	identity := directoryIdentity(files)
	if _, err := retain(dir, "identity.sha256", []byte(identity+"\n"), durability); err != nil {
		return nil, err
	}
	return Open(dir)
}

func readDirectory(dir string) (map[string][]byte, error) {
	return artifactdir.Read(dir, artifactdir.Layout{
		Noun:               "test result",
		AllowedDirectories: []string{"run", "run/payloads"},
		AllowFile:          func(string) bool { return true },
		MaxFiles:           16016,
		MaxFileBytes:       16 << 20,
		MaxBytes:           128 << 20,
	})
}

func directoryIdentity(files map[string][]byte) string {
	return artifactdir.Identity(Schema, files)
}

func validDigest(value string) bool {
	data, err := hex.DecodeString(value)
	return err == nil && len(data) == sha256.Size && strings.ToLower(value) == value
}
