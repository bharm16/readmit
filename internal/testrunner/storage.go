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

// resultFamily is a test result: the spec it ran, the observations it read,
// the run it recorded under run/ and the verdict, sealed by the ADR-0002
// identity written last.
var resultFamily = artifactdir.Family{
	Layout: artifactdir.Layout{
		Noun:               "test result",
		AllowedDirectories: []string{"run", "run/payloads"},
		AllowFile:          func(string) bool { return true },
		MaxFiles:           16016,
		MaxFileBytes:       16 << 20,
		MaxBytes:           128 << 20,
	},
	Seal: artifactdir.DirectoryHash(Schema),
	Errors: artifactdir.Errors{
		Reserve: errors.New("cannot create test output; destination must be new and parent readable and writable"),
		Create:  errors.New("cannot create test evidence"),
		Write:   errors.New("cannot write test evidence"),
		Sync:    errors.New("cannot sync test result directory; the result was written in full but a power loss could still lose it"),
	},
}

func retain(result *artifactdir.Writer, path string, raw []byte) (*bundle.Payload, error) {
	if err := result.WriteFile(path, raw); err != nil {
		return nil, err
	}
	return &bundle.Payload{Path: path, Size: len(raw), SHA256: digest(raw)}, nil
}

// finish records the verdict and seals the result. The result is found through
// its own directory and its entry in the folder holding it; run/ syncs its own
// entries. A failure once the identity is written leaves a result that may
// open and says so.
func finish(writer *artifactdir.Writer, result Result) (*Artifact, error) {
	data, err := encode(result)
	if err != nil || len(data) > maxResultBytes {
		return nil, errors.New("test result exceeds size limit")
	}
	if _, err := retain(writer, "result.json", data); err != nil {
		return nil, err
	}
	if _, err := writer.Seal(nil); err != nil {
		return nil, err
	}
	return Open(writer.Path())
}

func readDirectory(dir string) (map[string][]byte, error) {
	return artifactdir.Read(dir, resultFamily.Layout)
}

func directoryIdentity(files map[string][]byte) string {
	return artifactdir.Identity(Schema, files)
}

func validDigest(value string) bool {
	data, err := hex.DecodeString(value)
	return err == nil && len(data) == sha256.Size && strings.ToLower(value) == value
}
