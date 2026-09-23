package bundle

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/collection"
	"github.com/bharm16/readmit/internal/engineexport"
	"github.com/bharm16/readmit/internal/hl7"
	"github.com/bharm16/readmit/internal/observation"
)

// Write creates a new bundle exclusively. The identity file is written last:
// interrupted writes lack a valid completion marker and cannot be opened.
func Write(path string, inputs []Input, provenance Provenance) (*Bundle, error) {
	return WriteContext(context.Background(), path, inputs, provenance)
}

// WriteContext is Write for an import a person can cancel. Writing one payload
// file after another is the longest step of an import, so the cancellation is
// observed between them; a cancelled write is retained incomplete and refused
// by every reader, the same as any other interrupted write.
func WriteContext(ctx context.Context, path string, inputs []Input, provenance Provenance) (*Bundle, error) {
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
	return writeBundle(ctx, path, b)
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
	return writeBundle(context.Background(), path, b)
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
	return writeBundle(context.Background(), path, b)
}

func writeBundle(ctx context.Context, path string, b *Bundle) (*Bundle, error) {
	files, err := encode(b)
	if err != nil {
		return nil, err
	}
	b.Identity, err = artifactdir.WriteContext(ctx, path, artifactdir.WriteOptions{Domain: b.Manifest.Schema, Directories: []string{"payloads"}}, files)
	if errors.Is(err, artifactdir.ErrCreateDirectory) {
		return nil, errors.New("cannot create bundle; destination must be new and parent writable")
	}
	if errors.Is(err, artifactdir.ErrCancelled) {
		return nil, errors.New("bundle write cancelled; any incomplete bundle is retained and refused")
	}
	if err != nil {
		return nil, errors.New("cannot write bundle; incomplete bundle retained")
	}
	return b, nil
}

// supported reports whether a manifest declares a case bundle contract this
// release reads. Describe and Open must accept exactly the same set, so a new
// contract version reaches both readers at once and a listing never disagrees
// with what opening the same directory would accept.
func supported(schema string) bool {
	return schema == Schema || schema == RecordedSchema || schema == DerivedSchema || schema == CollectedSchema || schema == EngineExportSchema
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
	if err := validateEngineManifest(data, manifest); err != nil {
		return Manifest{}, err
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
	if err := validateEngineManifest(files["manifest.json"], manifest); err != nil {
		return nil, err
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
	if manifest.Schema == EngineExportSchema {
		plan, err := engineexport.Decode(files["engine-export.json"])
		if err != nil {
			return nil, err
		}
		if err := attachEngineExport(b, plan, files["engine-container.bin"]); err != nil {
			return nil, err
		}
		b.Manifest.EngineExport = &Payload{Path: "engine-export.json", Size: len(files["engine-export.json"]), SHA256: digest(files["engine-export.json"])}
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
	if manifest.Schema == EngineExportSchema {
		extraFiles += 2
	}
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
	if b.EngineExport != nil {
		files["engine-export.json"], err = json.Marshal(b.EngineExport, json.Deterministic(true))
		if err != nil {
			return nil, err
		}
		files["engine-container.bin"] = bytes.Clone(b.engineContainer)
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
	return artifactdir.Read(path, artifactdir.Layout{
		Noun:               "bundle",
		AllowedDirectories: []string{"payloads"},
		RequiredFiles:      []string{"manifest.json", "events.jsonl", "correlations.jsonl", "identity.sha256"},
		AllowFile: func(name string) bool {
			return name == "manifest.json" || name == "events.jsonl" || name == "correlations.jsonl" || name == "identity.sha256" || name == "observation.json" || name == "collection.json" || name == "engine-export.json" || name == "engine-container.bin" || strings.HasPrefix(name, "payloads/")
		},
		MaxFiles:     MaxEvents + 6,
		MaxFileBytes: maxFileBytes,
		MaxBytes:     maxBundleBytes,
	})
}

// Identity hashes a domain prefix and length-delimited relative paths/contents
// in bytewise path order. The identity file itself is the only excluded file.
func identityFor(schema string, files map[string][]byte) string {
	return artifactdir.Identity(schema, files)
}

func sortedNames(files map[string][]byte) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// validateEngineManifest keeps the v5 extension out of every legacy reader,
// including manifest-only listings that do not authenticate evidence.
func validateEngineManifest(data []byte, manifest Manifest) error {
	if manifest.Schema != EngineExportSchema {
		var members map[string]any
		if json.Unmarshal(data, &members) != nil {
			return errors.New("invalid bundle manifest")
		}
		if _, exists := members["engine_export"]; exists {
			return errors.New("legacy case cannot carry engine export provenance")
		}
	}
	if manifest.Schema == EngineExportSchema {
		var v5 struct {
			Schema     string `json:"schema"`
			State      string `json:"state"`
			Provenance struct {
				Mode       Mode       `json:"mode"`
				ImportedAt *time.Time `json:"imported_at"`
			} `json:"provenance"`
			Sources      []Source `json:"sources"`
			EventCount   int      `json:"event_count"`
			EngineExport *Payload `json:"engine_export"`
		}
		if json.Unmarshal(data, &v5, json.RejectUnknownMembers(true)) != nil || v5.EngineExport == nil || v5.Provenance.Mode != Imported {
			return errors.New("invalid v5 engine export manifest")
		}
	}

	return nil
}
