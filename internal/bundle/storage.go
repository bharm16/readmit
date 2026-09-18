package bundle

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/hl7"
)

// Write creates a new bundle exclusively. The identity file is written last:
// interrupted writes lack a valid completion marker and cannot be opened.
func Write(path string, inputs []Input, provenance Provenance) (*Bundle, error) {
	b, err := build(inputs, provenance)
	if err != nil {
		return nil, err
	}
	files, err := encode(b)
	if err != nil {
		return nil, err
	}
	b.Identity = identity(files)
	if err := os.Mkdir(path, 0700); err != nil {
		return nil, errors.New("cannot create bundle; destination must be new and parent writable")
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, errors.New("cannot open new bundle directory")
	}
	defer root.Close()
	if err := root.Mkdir("payloads", 0700); err != nil {
		return nil, errors.New("cannot create payload directory")
	}
	for _, name := range sortedNames(files) {
		if err := writeFile(root, name, files[name]); err != nil {
			return nil, err
		}
	}
	if err := writeFile(root, "identity.sha256", []byte(b.Identity+"\n")); err != nil {
		return nil, err
	}
	return b, nil
}

func writeFile(root *os.Root, name string, data []byte) error {
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("cannot create bundle file; incomplete bundle retained")
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return errors.New("cannot write bundle file; incomplete bundle retained")
	}
	return nil
}

// Open verifies completion, identity, payload hashes, source coverage, and the
// metadata/correlations derived from the bytes before returning any evidence.
// Unknown schema versions and JSON members are errors; no migrations occur.
func Open(path string) (*Bundle, error) {
	files, err := readFiles(path)
	if err != nil {
		return nil, err
	}
	var manifest Manifest
	if err := json.Unmarshal(files["manifest.json"], &manifest, json.RejectUnknownMembers(true)); err != nil {
		return nil, errors.New("invalid bundle manifest")
	}
	if manifest.Schema != Schema {
		return nil, errors.New("unsupported case bundle schema version")
	}
	if manifest.State != "complete" {
		return nil, errors.New("bundle is incomplete")
	}
	marker := files["identity.sha256"]
	delete(files, "identity.sha256")
	id := identity(files)
	if string(marker) != id+"\n" {
		return nil, errors.New("bundle is incomplete or its identity does not match contents")
	}
	events, err := decodeLines[Event](files["events.jsonl"])
	if err != nil {
		return nil, err
	}
	links, err := decodeLines[Correlation](files["correlations.jsonl"])
	if err != nil {
		return nil, err
	}
	inputs, err := restoreInputs(manifest, events, files)
	if err != nil {
		return nil, err
	}
	b, err := build(inputs, manifest.Provenance)
	if err != nil {
		return nil, err
	}
	if !sameJSON(manifest, b.Manifest) || !sameJSON(events, b.Events) || !sameJSON(links, b.Correlations) {
		return nil, errors.New("bundle metadata or correlations disagree with evidence")
	}
	b.Identity = id
	return b, nil
}

func restoreInputs(manifest Manifest, events []Event, files map[string][]byte) ([]Input, error) {
	invalid := errors.New("invalid bundle source or occurrence layout")
	if len(manifest.Sources) == 0 || len(manifest.Sources) > MaxSources || len(events) == 0 || len(events) > MaxEvents || manifest.EventCount != len(events) || len(files) != len(events)+3 {
		return nil, invalid
	}
	var inputs []Input
	index, total := 0, 0
	for i, source := range manifest.Sources {
		if source.ID != fmt.Sprintf("s%04d", i+1) || source.Occurrences < 1 || source.Occurrences > len(events)-index {
			return nil, invalid
		}
		input := Input{Path: source.Path, Options: hl7.Options{Format: source.Format, Terminator: source.Terminator}, Observations: make(map[int]Observation)}
		for sequence := 1; sequence <= source.Occurrences; sequence++ {
			event := events[index]
			id := fmt.Sprintf("%s-e%06d", source.ID, sequence)
			name := "payloads/" + id + ".bin"
			raw, exists := files[name]
			if !exists || event.ID != id || event.SourceID != source.ID || event.Sequence != sequence || event.Offset != len(input.Data) || event.Payload.Path != name {
				return nil, invalid
			}
			total += len(raw)
			if len(raw) > MaxSourceBytes-len(input.Data) || total > MaxEvidenceBytes {
				return nil, errors.New("bundle evidence exceeds size limit")
			}
			input.Data = append(input.Data, raw...)
			input.Observations[sequence] = Observation{Direction: event.Direction, ObservedAt: event.ObservedAt}
			index++
		}
		inputs = append(inputs, input)
	}
	if index != len(events) {
		return nil, invalid
	}
	return inputs, nil
}

func encode(b *Bundle) (map[string][]byte, error) {
	manifest, err := json.Marshal(b.Manifest, json.Deterministic(true))
	if err != nil {
		return nil, errors.New("cannot encode bundle manifest")
	}
	files := map[string][]byte{"manifest.json": append(manifest, '\n')}
	files["events.jsonl"], err = encodeLines(b.Events)
	if err != nil {
		return nil, err
	}
	files["correlations.jsonl"], err = encodeLines(b.Correlations)
	if err != nil {
		return nil, err
	}
	for _, event := range b.Events {
		files[event.Payload.Path] = b.payloads[event.ID]
	}
	total := 65 // Reserve the completion marker's 64 hex characters and LF.
	for _, data := range files {
		total += len(data)
		if len(data) > maxFileBytes || total > maxBundleBytes {
			return nil, errors.New("bundle metadata or contents exceed size limit")
		}
	}
	return files, nil
}

func encodeLines[T any](values []T) ([]byte, error) {
	var data []byte
	for _, value := range values {
		line, err := json.Marshal(value, json.Deterministic(true))
		if err != nil {
			return nil, errors.New("cannot encode bundle records")
		}
		if len(line)+1 > maxFileBytes-len(data) {
			return nil, errors.New("bundle records exceed size limit")
		}
		data = append(data, line...)
		data = append(data, '\n')
	}
	return data, nil
}

func decodeLines[T any](data []byte) ([]T, error) {
	var values []T
	if len(data) == 0 {
		return values, nil
	}
	if data[len(data)-1] != '\n' {
		return nil, errors.New("incomplete bundle record file")
	}
	for line := range bytes.SplitSeq(data[:len(data)-1], []byte{'\n'}) {
		// A bundle can have up to one ACK link per event plus one gap per
		// message. Neither record file can exceed the occurrence limit.
		if len(values) >= MaxEvents {
			return nil, errors.New("bundle record limit exceeded")
		}
		var value T
		if err := json.Unmarshal(line, &value, json.RejectUnknownMembers(true)); err != nil {
			return nil, errors.New("invalid bundle record")
		}
		values = append(values, value)
	}
	return values, nil
}

func sameJSON(a, b any) bool {
	left, err := json.Marshal(a, json.Deterministic(true))
	if err != nil {
		return false
	}
	right, err := json.Marshal(b, json.Deterministic(true))
	return err == nil && bytes.Equal(left, right)
}

func readFiles(path string) (map[string][]byte, error) {
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, errors.New("cannot open bundle directory")
	}
	defer root.Close()
	files := make(map[string][]byte)
	total := 0
	err = fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return errors.New("cannot read bundle directory")
		}
		if entry.IsDir() {
			if name != "." && name != "payloads" {
				return errors.New("unexpected bundle directory")
			}
			return nil
		}
		if entry.Type() != 0 {
			return errors.New("bundle files must be regular files, never symlinks")
		}
		if name != "manifest.json" && name != "events.jsonl" && name != "correlations.jsonl" && name != "identity.sha256" && !strings.HasPrefix(name, "payloads/") {
			return errors.New("unexpected bundle file")
		}
		if len(files) >= MaxEvents+4 {
			return errors.New("bundle file limit exceeded")
		}
		f, err := root.Open(name)
		if err != nil {
			return errors.New("cannot read bundle file")
		}
		info, statErr := f.Stat()
		if statErr != nil || !info.Mode().IsRegular() {
			f.Close()
			return errors.New("bundle files must be regular files")
		}
		data, readErr := io.ReadAll(io.LimitReader(f, int64(min(maxFileBytes, maxBundleBytes-total))+1))
		closeErr := f.Close()
		if readErr != nil || closeErr != nil {
			return errors.New("cannot read bundle file")
		}
		total += len(data)
		if len(data) > maxFileBytes || total > maxBundleBytes {
			return errors.New("bundle contents exceed size limit")
		}
		files[name] = data
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, required := range []string{"manifest.json", "events.jsonl", "correlations.jsonl", "identity.sha256"} {
		if _, ok := files[required]; !ok {
			return nil, errors.New("bundle is incomplete: required file missing")
		}
	}
	return files, nil
}

// Identity hashes a domain prefix and length-delimited relative paths/contents
// in bytewise path order. The identity file itself is the only excluded file.
func identity(files map[string][]byte) string {
	h := sha256.New()
	h.Write([]byte(Schema + "\n"))
	var size [8]byte
	for _, name := range sortedNames(files) {
		binary.BigEndian.PutUint64(size[:], uint64(len(name)))
		h.Write(size[:])
		h.Write([]byte(name))
		binary.BigEndian.PutUint64(size[:], uint64(len(files[name])))
		h.Write(size[:])
		h.Write(files[name])
	}
	return hex.EncodeToString(h.Sum(nil))
}

func sortedNames(files map[string][]byte) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
