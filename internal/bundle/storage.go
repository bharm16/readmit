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
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/collection"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/observation"
)

// Write creates a new bundle exclusively. The identity file is written last:
// interrupted writes lack a valid completion marker and cannot be opened.
func Write(path string, inputs []Input, provenance Provenance) (*Bundle, error) {
	if provenance.Mode == Recorded {
		return nil, errors.New("recorded sessions require WriteRecorded and an observation")
	}
	if provenance.Mode == Collected {
		return nil, errors.New("collected sessions require WriteCollected and a collection record")
	}
	b, err := build(inputs, provenance)
	if err != nil {
		return nil, err
	}
	return writeBundle(path, b)
}

// WriteRecorded preserves a receiver session and its final observation in v2.
// Default Write remains a v1 writer for imported and generated evidence.
func WriteRecorded(path string, inputs []Input, startedAt time.Time, snapshot observation.Snapshot) (*Bundle, error) {
	b, err := build(inputs, Provenance{Mode: Recorded, StartedAt: &startedAt, SessionID: snapshot.SessionID})
	if err != nil {
		return nil, err
	}
	if err := attachObservation(b, snapshot); err != nil {
		return nil, err
	}
	return writeBundle(path, b)
}

// WriteCollected preserves a generic receiver session and the record of what it
// collected in v4. Recorded fixture sessions keep their own v2 writer.
func WriteCollected(path string, inputs []Input, startedAt time.Time, record collection.Record) (*Bundle, error) {
	b, err := build(inputs, Provenance{Mode: Collected, StartedAt: &startedAt, SessionID: record.SessionID})
	if err != nil {
		return nil, err
	}
	if err := attachCollection(b, record); err != nil {
		return nil, err
	}
	return writeBundle(path, b)
}

func writeBundle(path string, b *Bundle) (*Bundle, error) {
	path, err := artifactpath.Destination(path)
	if err != nil {
		return nil, err
	}
	files, err := encode(b)
	if err != nil {
		return nil, err
	}
	b.Identity = identityFor(b.Manifest.Schema, files)
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

// supported reports whether a manifest declares a case bundle contract this
// release reads. Describe and Open must accept exactly the same set, so a new
// contract version reaches both readers at once and a listing never disagrees
// with what opening the same directory would accept.
func supported(schema string) bool {
	return schema == Schema || schema == RecordedSchema || schema == DerivedSchema || schema == CollectedSchema
}

// Describe reads only the manifest of a case bundle directory so a caller can
// list a folder without loading its evidence. It reports what the directory
// declares: it verifies neither completion, identity, payload hashes, nor any
// record. A described directory is not evidence until Open accepts it.
func Describe(path string) (Manifest, error) {
	root, err := os.OpenRoot(path)
	if err != nil {
		return Manifest{}, errors.New("cannot open bundle directory")
	}
	defer root.Close()
	file, err := root.Open("manifest.json")
	if err != nil {
		return Manifest{}, errors.New("cannot read bundle manifest")
	}
	info, statErr := file.Stat()
	if statErr != nil || !info.Mode().IsRegular() {
		file.Close()
		return Manifest{}, errors.New("bundle manifest must be a regular file")
	}
	data, readErr := io.ReadAll(io.LimitReader(file, maxFileBytes+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(data) > maxFileBytes {
		return Manifest{}, errors.New("cannot read bundle manifest")
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest, json.RejectUnknownMembers(true)); err != nil {
		return Manifest{}, errors.New("invalid bundle manifest")
	}
	if !supported(manifest.Schema) {
		return Manifest{}, errors.New("unsupported case bundle schema version")
	}
	return manifest, nil
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
	if !supported(manifest.Schema) {
		return nil, errors.New("unsupported case bundle schema version")
	}
	if manifest.Schema == RecordedSchema {
		// Do not relax v2 when adding the v3-only provenance member or the
		// v4-only collection member, even when they are explicitly null: a
		// typed decode would accept an explicit null as an omitted field.
		var members map[string]any
		if json.Unmarshal(files["manifest.json"], &members) != nil {
			return nil, errors.New("invalid v2 bundle manifest")
		}
		provenance, _ := members["provenance"].(map[string]any)
		if _, exists := provenance["derivation"]; exists {
			return nil, errors.New("invalid v2 bundle provenance")
		}
		if _, exists := members["collection"]; exists {
			return nil, errors.New("v2 manifest cannot carry a collection record")
		}
	}
	if manifest.Schema == CollectedSchema {
		var collected struct {
			Schema     string `json:"schema"`
			State      string `json:"state"`
			Provenance struct {
				Mode      Mode       `json:"mode"`
				StartedAt *time.Time `json:"started_at,omitzero"`
				SessionID string     `json:"session_id,omitzero"`
			} `json:"provenance"`
			Sources    []Source `json:"sources"`
			EventCount int      `json:"event_count"`
			Collection *Payload `json:"collection"`
		}
		if json.Unmarshal(files["manifest.json"], &collected, json.RejectUnknownMembers(true)) != nil {
			return nil, errors.New("invalid v4 bundle manifest")
		}
		var paths struct {
			Sources []map[string]any `json:"sources"`
		}
		if json.Unmarshal(files["manifest.json"], &paths) != nil {
			return nil, errors.New("invalid v4 sources")
		}
		for _, source := range paths.Sources {
			if _, exists := source["path"]; exists {
				return nil, errors.New("collected evidence cannot carry original source paths")
			}
		}
	}
	if manifest.Schema == DerivedSchema {
		var derived struct {
			Schema     string `json:"schema"`
			State      string `json:"state"`
			Provenance struct {
				Mode       Mode   `json:"mode"`
				Derivation string `json:"derivation"`
			} `json:"provenance"`
			Sources    []Source `json:"sources"`
			EventCount int      `json:"event_count"`
		}
		if json.Unmarshal(files["manifest.json"], &derived, json.RejectUnknownMembers(true)) != nil {
			return nil, errors.New("invalid v3 bundle manifest")
		}
		var paths struct {
			Sources []map[string]any `json:"sources"`
		}
		if json.Unmarshal(files["manifest.json"], &paths) != nil {
			return nil, errors.New("invalid v3 sources")
		}
		for _, source := range paths.Sources {
			if _, exists := source["path"]; exists {
				return nil, errors.New("derived evidence cannot carry original source paths")
			}
		}
	}
	if manifest.Schema == Schema {
		// v1 remains strict against fields first introduced in v2, including
		// explicit nulls that would otherwise decode to an omitted zero value.
		var legacy struct {
			Schema     string `json:"schema"`
			State      string `json:"state"`
			Provenance struct {
				Mode       Mode             `json:"mode"`
				ImportedAt *time.Time       `json:"imported_at,omitzero"`
				Generator  *GeneratorInputs `json:"generator,omitzero"`
			} `json:"provenance"`
			Sources    []Source `json:"sources"`
			EventCount int      `json:"event_count"`
		}
		if err := json.Unmarshal(files["manifest.json"], &legacy, json.RejectUnknownMembers(true)); err != nil {
			return nil, errors.New("invalid v1 bundle manifest")
		}
	}
	if manifest.Schema == Schema && (manifest.Provenance.Mode != Imported && manifest.Provenance.Mode != Generated || manifest.Observation != nil) || manifest.Schema == RecordedSchema && (manifest.Provenance.Mode != Recorded || manifest.Observation == nil) || manifest.Schema == DerivedSchema && (manifest.Provenance.Mode != Derived || manifest.Observation != nil) || manifest.Schema == CollectedSchema && (manifest.Provenance.Mode != Collected || manifest.Collection == nil || manifest.Observation != nil) {
		return nil, errors.New("case bundle schema does not match provenance, observation, and collection")
	}
	if manifest.State != "complete" {
		return nil, errors.New("bundle is incomplete")
	}
	marker := files["identity.sha256"]
	delete(files, "identity.sha256")
	id := identityFor(manifest.Schema, files)
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
	if manifest.Schema == RecordedSchema {
		snapshot, err := observation.Decode(files["observation.json"])
		if err != nil {
			return nil, err
		}
		if err := attachObservation(b, snapshot); err != nil {
			return nil, err
		}
		// Metadata refers to the exact stored JSON bytes, including whitespace.
		b.Manifest.Observation = &Payload{Path: "observation.json", Size: len(files["observation.json"]), SHA256: digest(files["observation.json"])}
	}
	if manifest.Schema == CollectedSchema {
		record, err := collection.Decode(files["collection.json"])
		if err != nil {
			return nil, err
		}
		if err := attachCollection(b, record); err != nil {
			return nil, err
		}
		b.Manifest.Collection = &Payload{Path: "collection.json", Size: len(files["collection.json"]), SHA256: digest(files["collection.json"])}
	}
	eventsMatch, err := sameRecords(events, b.Events)
	if err != nil {
		return nil, err
	}
	linksMatch, err := sameRecords(links, b.Correlations)
	if err != nil {
		return nil, err
	}
	if !sameJSON(manifest, b.Manifest) || !eventsMatch || !linksMatch {
		return nil, errors.New("bundle metadata or correlations disagree with evidence")
	}
	b.Identity = id
	return b, nil
}

func restoreInputs(manifest Manifest, events []Event, files map[string][]byte) ([]Input, error) {
	invalid := errors.New("invalid bundle source or occurrence layout")
	extraFiles := 3
	if manifest.Schema == RecordedSchema || manifest.Schema == CollectedSchema {
		extraFiles++
	}
	if len(manifest.Sources) == 0 && manifest.Schema != RecordedSchema && manifest.Schema != CollectedSchema || len(manifest.Sources) > MaxSources || len(events) == 0 && len(manifest.Sources) != 0 || len(events) > MaxEvents || manifest.EventCount != len(events) || len(files) != len(events)+extraFiles {
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
	if b.Observation != nil {
		files["observation.json"], err = observation.Encode(*b.Observation)
		if err != nil {
			return nil, err
		}
	}
	if b.Collection != nil {
		files["collection.json"], err = collection.Encode(*b.Collection)
		if err != nil {
			return nil, err
		}
	}
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

// Rebuilding can expand a small input into a large ambiguous-correlation graph.
// Compare records through the writer's incremental file budget, never by
// marshaling the complete reconstructed slice before checking its size.
func sameRecords[T any](stored, rebuilt []T) (bool, error) {
	expected, err := encodeLines(rebuilt)
	if err != nil {
		return false, err
	}
	actual, err := encodeLines(stored)
	if err != nil {
		return false, err
	}
	return bytes.Equal(actual, expected), nil
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
		if name != "manifest.json" && name != "events.jsonl" && name != "correlations.jsonl" && name != "identity.sha256" && name != "observation.json" && name != "collection.json" && !strings.HasPrefix(name, "payloads/") {
			return errors.New("unexpected bundle file")
		}
		if len(files) >= MaxEvents+5 {
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
func identityFor(schema string, files map[string][]byte) string {
	h := sha256.New()
	h.Write([]byte(schema + "\n"))
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
