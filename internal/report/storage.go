package report

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"path"
	"slices"
	"sort"
	"strings"

	"github.com/bharm16/readmit/internal/artifactdir"
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

// outputErrors are the sentences a failed write of any report output is
// reported in.
var outputErrors = artifactdir.Errors{
	Reserve:   errors.New("cannot create report output; destination must be new and parent readable and writable"),
	Directory: errors.New("cannot create report evidence directory"),
	Create:    errors.New("cannot create report evidence; incomplete output retained"),
	Write:     errors.New("cannot write report evidence; incomplete output retained"),
	Sync:      errors.New("cannot sync report output; the output was written in full but a power loss could still lose it"),
}

// sealed is a report output sealed by the digest of its manifest, which
// indexes every other file: a packet, a retained packet, a portable review or
// a prepared rerun workspace. It holds its files, its own directories and the
// nested packets it copies whole.
func sealed(manifest, marker string, directories, nested []string, files ...string) artifactdir.Family {
	return artifactdir.Family{
		Layout: artifactdir.Layout{
			AllowedDirectories: directories,
			Nested:             nested,
			AllowFile:          func(name string) bool { return name == manifest || name == marker || slices.Contains(files, name) },
		},
		Seal:   artifactdir.ManifestHash("", manifest, marker),
		Errors: outputErrors,
	}
}

var (
	packetFamily      = sealed("manifest.json", "identity.sha256", []string{"profiles"}, []string{"reproducer", "baseline", "post-fix"}, "spec.json", "baseline-target.json", "post-fix-target.json", "diagnosis.json", "diagnosis.md", "profiles/receiver.json", "profiles/diagnosis.json", "profiles/diagnose-config.json", "diff.json", "diff.md", "history.json", "SUMMARY.md", "RERUN.md")
	retainedFamily    = sealed("manifest.json", "identity.sha256", nil, []string{"case", "current", "baseline", "baseline-case"}, "spec.json", "SUMMARY.md", "RERUN.md")
	reviewFamily      = sealed("manifest.json", "identity.sha256", nil, []string{"packet"}, "report.html", "report.md", "report.json", "junit.xml", "report.pdf")
	preparationFamily = sealed("preparation.json", "preparation.sha256", []string{"baseline", "post-fix", "reintroduced"}, []string{"reproducer"}, "target.json", "RERUN.md", "baseline/spec.json", "post-fix/spec.json", "reintroduced/spec.json")
	// trialFamily is one trial's execution workspace inside the workspace
	// Create removes before it answers: the case and spec it sends and the
	// fixture's target. It is scratch and writes no completion record.
	trialFamily = artifactdir.Family{
		Layout: artifactdir.Layout{
			Nested:    []string{"reproducer"},
			AllowFile: func(name string) bool { return name == "spec.json" || name == "target.json" },
		},
		Errors: artifactdir.Errors{
			Reserve: errors.New("cannot create report trial workspace"),
			Create:  outputErrors.Create,
			Write:   outputErrors.Write,
		},
	}
)

var errInvalidEvidence = errors.New("report evidence must be bounded regular files without symlinks or empty directories")

// readTree rejects links, devices, oversized input and unindexed empty directories.
// os.Root confines the bounded reads to the selected directory.
func readTree(dir string) (map[string][]byte, error) {
	return readTreeAllowEmptySent(dir, false)
}

// Retained durable jobs may have no sent bytes after a connection failure.
// Empty operational sent/ carries no evidence file and is omitted on copy;
// all other empty directories remain errors, as does sent/ in ordinary input.
func readTreeAllowEmptySent(dir string, durable bool) (map[string][]byte, error) {
	files, err := artifactdir.Read(dir, evidence(durable))
	if err != nil {
		return nil, errInvalidEvidence
	}
	return files, nil
}

// evidence is the one shape a report reads a folder of evidence as, whether a
// case, a result, a job or a packet: bounded regular files and the directories
// holding them, with short, shallow names, and no empty directory except a
// durable job's sent/. Every directory and file counts toward one entry bound.
func evidence(durable bool) artifactdir.Layout {
	entries := 0
	admit := func(name string) bool {
		entries++
		return entries <= maxFiles*2 && len(name) <= 200 && strings.Count(name, "/") <= 5 && !strings.Contains(name, "\\")
	}
	return artifactdir.Layout{
		AllowDirectory: admit,
		AllowFile:      admit,
		AllowEmpty: func(directory string, files map[string][]byte) bool {
			return durable && directory == "sent" && files["engine.json"] != nil
		},
		MaxFiles:     maxFiles,
		MaxFileBytes: maxFileBytes,
		MaxBytes:     maxPacketBytes,
	}
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

// copyFiles writes every file of files named below prefix into output, below
// into, with prefix replaced by it.
func copyFiles(output *artifactdir.Writer, files map[string][]byte, prefix, into string) error {
	for name, data := range files {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		name = strings.TrimPrefix(name, prefix)
		if !fs.ValidPath(name) {
			return errors.New("invalid report evidence path")
		}
		if err := output.WriteFile(path.Join(into, name), data); err != nil {
			return err
		}
	}
	return nil
}
