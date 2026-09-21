package observesource

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/observewindow"
)

// EncodeSource writes one validated source deterministically. Unused transports
// are emitted as explicit nulls for the schema version in force, so a document
// written here reads back through DecodeSource without repair.
func EncodeSource(s Source) ([]byte, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	var (
		data []byte
		err  error
	)
	switch s.Schema {
	case SchemaV1:
		data, err = json.Marshal(struct {
			Schema     string               `json:"schema"`
			Observes   observewindow.Source `json:"source"`
			Enabled    bool                 `json:"enabled"`
			Freshness  Freshness            `json:"freshness"`
			Extraction *Extraction          `json:"extraction"`
			File       *File                `json:"file"`
			HTTP       *HTTP                `json:"http"`
		}{Schema: s.Schema, Observes: s.Observes, Enabled: s.Enabled, Freshness: s.Freshness, Extraction: s.Extraction, File: s.File, HTTP: s.HTTP}, json.Deterministic(true))
	case Schema:
		data, err = json.Marshal(struct {
			Schema     string               `json:"schema"`
			Observes   observewindow.Source `json:"source"`
			Enabled    bool                 `json:"enabled"`
			Freshness  Freshness            `json:"freshness"`
			Extraction *Extraction          `json:"extraction"`
			File       *File                `json:"file"`
			HTTP       *HTTP                `json:"http"`
			Capture    *Capture             `json:"capture"`
		}{Schema: s.Schema, Observes: s.Observes, Enabled: s.Enabled, Freshness: s.Freshness, Extraction: s.Extraction, File: s.File, HTTP: s.HTTP, Capture: s.Capture}, json.Deterministic(true))
	case SchemaDatabase:
		data, err = json.Marshal(struct {
			Schema     string               `json:"schema"`
			Observes   observewindow.Source `json:"source"`
			Enabled    bool                 `json:"enabled"`
			Freshness  Freshness            `json:"freshness"`
			Extraction *Extraction          `json:"extraction"`
			File       *File                `json:"file"`
			HTTP       *HTTP                `json:"http"`
			Capture    *Capture             `json:"capture"`
			Database   *Database            `json:"database"`
		}{Schema: s.Schema, Observes: s.Observes, Enabled: s.Enabled, Freshness: s.Freshness, Extraction: s.Extraction, File: s.File, HTTP: s.HTTP, Capture: s.Capture, Database: s.Database}, json.Deterministic(true))
	default:
		return nil, ErrUnsupportedVersion
	}
	if err != nil {
		return nil, errors.New("cannot encode observation source")
	}
	return data, nil
}

// Identity names the canonical form of this declared source. It pins a saved
// configuration when binding to a test; it does not authenticate a file.
func (s Source) Identity() string {
	data, err := EncodeSource(s)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// WriteSource records one declared observation source. It validates before
// writing and renames an owner-only incomplete file onto the destination, so a
// reader never observes a partial document and a failed write leaves the
// previous one exactly as it was. Paths are written as declared; ReadSource
// resolves them against the document's own directory.
func WriteSource(path string, s Source) error {
	data, err := EncodeSource(s)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	destination, err := artifactpath.Destination(path)
	if err != nil {
		return errors.New("cannot write an observation source here")
	}
	incomplete, err := artifactpath.Destination(path + ".incomplete")
	if err != nil {
		return errors.New("cannot write an observation source here; an interrupted write may be retained beside it")
	}
	file, err := os.OpenFile(incomplete, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("cannot create the new observation source; an interrupted write is retained")
	}
	_, writeErr := file.Write(data)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(incomplete)
		return errors.New("cannot write the new observation source")
	}
	if err := os.Rename(incomplete, destination); err != nil {
		os.Remove(incomplete)
		return errors.New("cannot replace the observation source")
	}
	return nil
}

// DecodeSource reads one declared observation source exactly as written.
// Unknown members and unknown versions are errors; there is no migration and no
// repair, and a diagnostic names the declaration at fault without repeating the
// value that failed.
func DecodeSource(data []byte) (Source, error) {
	if len(data) > MaxSourceBytes {
		return Source{}, errors.New("observation source exceeds size limit")
	}
	var source Source
	if err := json.Unmarshal(data, &source); err != nil {
		if errors.Is(err, ErrUnsupportedVersion) {
			return Source{}, ErrUnsupportedVersion
		}
		return Source{}, errors.New("invalid observation source JSON")
	}
	if err := source.Validate(); err != nil {
		return Source{}, err
	}
	return source, nil
}

// ReadSource opens one declared observation source. Like every other input
// reader here it accepts a regular file only, and the export and certificate
// authority it names are resolved against the directory the document itself
// lives in, never the caller's working directory: the declared form is what the
// file holds, and this is what reading it means.
func ReadSource(path string) (Source, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return Source{}, errors.New("an observation source must be a readable regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return Source{}, errors.New("cannot open observation source file")
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return Source{}, errors.New("an observation source must be a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(MaxSourceBytes)+1))
	if err != nil {
		return Source{}, errors.New("cannot read observation source file")
	}
	if len(data) > MaxSourceBytes {
		return Source{}, errors.New("observation source exceeds size limit")
	}
	source, err := DecodeSource(data)
	if err != nil {
		return Source{}, err
	}
	resolved, err := artifactpath.Resolve(path)
	if err != nil {
		return Source{}, errors.New("cannot resolve the observation source document")
	}
	return anchor(source, filepath.Dir(resolved)), nil
}

// anchor resolves the paths a source declares against the directory the
// document itself lives in. It preserves raw traversal rather than cleaning it
// away, so a reference through a symbolic link names what the filesystem names.
func anchor(source Source, directory string) Source {
	if source.File != nil && source.File.Path != "" {
		export := *source.File
		export.Path = artifactpath.JoinReference(directory, export.Path)
		source.File = &export
	}
	if source.HTTP != nil && source.HTTP.CAFile != "" {
		endpoint := *source.HTTP
		endpoint.CAFile = artifactpath.JoinReference(directory, endpoint.CAFile)
		source.HTTP = &endpoint
	}
	if source.Capture != nil && source.Capture.Path != "" {
		capture := *source.Capture
		capture.Path = artifactpath.JoinReference(directory, capture.Path)
		source.Capture = &capture
	}
	if source.Database != nil && source.Database.CAFile != "" {
		d := *source.Database
		d.CAFile = artifactpath.JoinReference(directory, d.CAFile)
		source.Database = &d
	}
	return source
}
