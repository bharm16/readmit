package fhirr4

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/dataset"
)

type Acquisition struct {
	Kind           string            `json:"kind"`
	SourceIdentity string            `json:"source_identity"`
	StartedAt      time.Time         `json:"started_at"`
	CompletedAt    time.Time         `json:"completed_at"`
	Method         string            `json:"method,omitzero"`
	RequestURL     string            `json:"request_url,omitzero"`
	Status         int               `json:"status"`
	Headers        map[string]string `json:"safe_response_metadata"`
}
type Input struct {
	Role        string
	Context     Context
	Acquisition Acquisition
	Bytes       []byte
}
type Source struct {
	ID          string      `json:"id"`
	Role        string      `json:"role"`
	Context     Context     `json:"context"`
	Acquisition Acquisition `json:"acquisition"`
	Path        string      `json:"path"`
	SHA256      string      `json:"sha256"`
	Bytes       int         `json:"bytes"`
	State       string      `json:"state"`
	Resources   []Resource  `json:"resources"`
	Findings    []Finding   `json:"findings"`
}
type Manifest struct {
	Schema  string   `json:"schema"`
	Version string   `json:"version"`
	Sources []Source `json:"sources"`
}
type Evidence struct {
	manifest  Manifest
	files     map[string][]byte
	documents []*Document
	identity  string
}

func (e *Evidence) Identity() string   { return e.identity }
func (e *Evidence) Manifest() Manifest { return clone(e.manifest) }
func (e *Evidence) Document(source string) (*Document, error) {
	for i, s := range e.manifest.Sources {
		if s.ID == source && e.documents[i] != nil {
			return e.documents[i], nil
		}
	}
	return nil, invalid
}
func (e *Evidence) Bytes(source string) ([]byte, error) {
	for _, s := range e.manifest.Sources {
		if s.ID == source {
			return bytes.Clone(e.files[s.Path]), nil
		}
	}
	return nil, invalid
}

var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var payloadName = regexp.MustCompile(`^payload-[0-9]{4}\.bin$`)
var evidenceFamily = artifactdir.Family{Layout: artifactdir.Layout{Noun: "FHIR evidence", RequiredFiles: []string{"manifest.json", "identity.sha256"}, AllowFile: func(name string) bool {
	return name == "manifest.json" || name == "identity.sha256" || payloadName.MatchString(name)
}, MaxFiles: 10, MaxFileBytes: MaxBytes, MaxBytes: 96 << 20}, Seal: artifactdir.DirectoryHash(EvidenceSchema)}

func (a Acquisition) validate() error {
	if a.Kind != "file" && a.Kind != "http" && a.Kind != "synthetic" || !digestPattern.MatchString(a.SourceIdentity) || a.StartedAt.IsZero() || a.CompletedAt.Before(a.StartedAt) || len(a.Headers) > 8 || len(a.RequestURL) > 4096 {
		return invalid
	}
	if a.Kind == "http" {
		if a.RequestURL != "" {
			if u, err := url.Parse(a.RequestURL); err != nil || u.User != nil {
				return invalid
			}
		}
		if a.Status < 100 || a.Status > 599 {
			return invalid
		}
		switch a.Method {
		case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD":
		default:
			return invalid
		}
	} else if a.Method != "" || a.RequestURL != "" || a.Status != 0 || len(a.Headers) != 0 {
		return invalid
	}
	for key, value := range a.Headers {
		switch key {
		case "Content-Type", "Date", "Age", "ETag", "Last-Modified", "Location":
		default:
			return invalid
		}
		if key == "Location" {
			if u, err := url.Parse(value); err != nil || u.User != nil {
				return invalid
			}
		}
		if len(value) > 4096 || strings.ContainsAny(value, "\r\n") {
			return invalid
		}
	}
	return nil
}

// Retain builds evidence from already acquired bytes. Invalid JSON/FHIR stays
// retained as invalid evidence; no absent collection or workflow pass is inferred.
func Retain(ctx context.Context, inputs []Input) (*Evidence, error) {
	if len(inputs) < 1 || len(inputs) > 8 {
		return nil, invalid
	}
	e := &Evidence{manifest: Manifest{Schema: EvidenceSchema, Version: Version, Sources: []Source{}}, files: map[string][]byte{}, documents: []*Document{}}
	total, resources := 0, 0
	for i, input := range inputs {
		if input.Context.Version != Version || validateContext(input.Context) != nil || input.Acquisition.validate() != nil || len(input.Bytes) > MaxBytes {
			return nil, invalid
		}
		switch input.Role {
		case "resource", "bundle", "request", "response":
		default:
			return nil, invalid
		}
		total += len(input.Bytes)
		if total > 64<<20 || ctx.Err() != nil {
			return nil, invalid
		}
		source := Source{ID: fmt.Sprintf("s%04d", i+1), Role: input.Role, Context: input.Context, Acquisition: clone(input.Acquisition), Path: fmt.Sprintf("payload-%04d.bin", i+1), SHA256: dataset.Digest(input.Bytes), Bytes: len(input.Bytes), State: "parsed", Resources: []Resource{}, Findings: []Finding{}}
		document, err := Decode(ctx, input.Bytes, input.Context)
		if err != nil {
			source.State = "invalid"
			source.Findings = append(source.Findings, Finding{Code: "json-decode-refused", State: "invalid"})
			if len(input.Bytes) == 0 && (input.Role == "request" || input.Role == "response") && input.Acquisition.Kind == "http" {
				source.State = "empty-http-body"
				source.Findings = []Finding{}
			}
		} else {
			source.Resources = document.Resources()
			source.Findings = document.Findings()
			resources += len(source.Resources)
			for _, finding := range source.Findings {
				if finding.State == "invalid" {
					source.State = "invalid"
				}
			}
		}
		if input.Acquisition.Kind == "http" && (input.Acquisition.Status < 200 || input.Acquisition.Status >= 300) {
			source.State = "http-error"
		}
		if input.Role == "response" && input.Acquisition.Kind == "http" && len(input.Bytes) > 0 && (input.Acquisition.Status == 204 || input.Acquisition.Method == "HEAD") {
			source.State = "invalid-http-body"
		}
		if resources > MaxResources {
			return nil, invalid
		}
		e.manifest.Sources = append(e.manifest.Sources, source)
		e.documents = append(e.documents, document)
		e.files[source.Path] = bytes.Clone(input.Bytes)
	}
	e.files["manifest.json"] = canonical(e.manifest)
	if len(e.files["manifest.json"]) > MaxBytes {
		return nil, invalid
	}
	e.identity = artifactdir.Identity(EvidenceSchema, e.files)
	return e, nil
}
func (e *Evidence) Write(ctx context.Context, output string) error {
	if e == nil {
		return invalid
	}
	_, err := artifactdir.Write(ctx, output, evidenceFamily, artifactdir.Durable, e.files)
	return err
}
func Open(ctx context.Context, directory string) (*Evidence, error) {
	files, err := artifactdir.Read(directory, evidenceFamily.Layout)
	if err != nil {
		return nil, err
	}
	return verifyEvidence(ctx, files)
}
func verifyEvidence(ctx context.Context, files map[string][]byte) (*Evidence, error) {
	if len(files) > evidenceFamily.Layout.MaxFiles {
		return nil, invalid
	}
	total := 0
	for name, raw := range files {
		if !evidenceFamily.Layout.AllowFile(name) || len(raw) > evidenceFamily.Layout.MaxFileBytes {
			return nil, invalid
		}
		total += len(raw)
	}
	if total > evidenceFamily.Layout.MaxBytes {
		return nil, invalid
	}
	if strings.TrimSpace(string(files["identity.sha256"])) != artifactdir.Identity(EvidenceSchema, files) {
		return nil, invalid
	}
	var manifest Manifest
	if json.Unmarshal(files["manifest.json"], &manifest, json.RejectUnknownMembers(true)) != nil || manifest.Schema != EvidenceSchema || manifest.Version != Version {
		return nil, invalid
	}
	inputs := []Input{}
	for i, source := range manifest.Sources {
		if source.ID != fmt.Sprintf("s%04d", i+1) || source.Path != fmt.Sprintf("payload-%04d.bin", i+1) {
			return nil, invalid
		}
		raw, ok := files[source.Path]
		if !ok || len(raw) != source.Bytes || dataset.Digest(raw) != source.SHA256 {
			return nil, invalid
		}
		inputs = append(inputs, Input{Role: source.Role, Context: source.Context, Acquisition: source.Acquisition, Bytes: raw})
	}
	if len(files) != len(inputs)+2 {
		return nil, invalid
	}
	rebuilt, err := Retain(ctx, inputs)
	if err != nil || !bytes.Equal(rebuilt.files["manifest.json"], files["manifest.json"]) {
		return nil, invalid
	}
	return rebuilt, nil
}

// RetainProjection binds a typed projection to original evidence. Its reader
// re-projects the retained bytes; a resealed manifest cannot change values.
func (e *Evidence) RetainProjection(ctx context.Context, source string, binding dataset.Binding, p Projection, output string) (Dataset, error) {
	document, err := e.Document(source)
	if err == nil {
		for _, selected := range e.manifest.Sources {
			if selected.ID == source && (binding.Source != selected.Acquisition.SourceIdentity || selected.State != "parsed" || selected.Role == "request") {
				err = invalid
			}
		}
	}
	if err != nil {
		return Dataset{}, err
	}
	projected, err := document.Project(ctx, binding, p)
	if err != nil {
		return Dataset{}, err
	}
	record := projectionRecord{Schema: DatasetSchema, Evidence: e.Identity(), Source: source, Dataset: projected}
	w, err := artifactdir.Create(output, projectionFamily, artifactdir.Durable)
	if err != nil {
		return Dataset{}, err
	}
	defer w.Close()
	if err := e.Write(ctx, filepath.Join(output, "evidence")); err != nil {
		return Dataset{}, err
	}
	if err := w.WriteFile("projection.json", canonical(record)); err != nil {
		return Dataset{}, err
	}
	if _, err := w.Seal(nil); err != nil {
		return Dataset{}, err
	}
	return projected, nil
}

type projectionRecord struct {
	Schema   string  `json:"schema"`
	Evidence string  `json:"evidence_identity"`
	Source   string  `json:"source"`
	Dataset  Dataset `json:"dataset"`
}

var projectionFamily = artifactdir.Family{Layout: artifactdir.Layout{Noun: "FHIR dataset", Nested: []string{"evidence"}, RequiredFiles: []string{"projection.json", "identity.sha256"}, AllowFile: func(name string) bool { return name == "projection.json" || name == "identity.sha256" }, MaxFiles: 12, MaxFileBytes: 32 << 20, MaxBytes: 128 << 20}, Seal: artifactdir.DirectoryHash(DatasetSchema)}

func OpenProjection(ctx context.Context, directory string) (Dataset, error) {
	files, err := artifactdir.Read(directory, projectionFamily.Layout)
	if err != nil || strings.TrimSpace(string(files["identity.sha256"])) != artifactdir.Identity(DatasetSchema, files) {
		return Dataset{}, invalid
	}
	var record projectionRecord
	if json.Unmarshal(files["projection.json"], &record, json.RejectUnknownMembers(true)) != nil || record.Schema != DatasetSchema {
		return Dataset{}, invalid
	}
	nested := map[string][]byte{}
	for name, raw := range files {
		if strings.HasPrefix(name, "evidence/") {
			nested[strings.TrimPrefix(name, "evidence/")] = raw
		}
	}
	evidence, err := verifyEvidence(ctx, nested)
	if err != nil || evidence.Identity() != record.Evidence {
		return Dataset{}, invalid
	}
	for _, source := range evidence.manifest.Sources {
		if source.ID == record.Source && (record.Dataset.Binding.Source != source.Acquisition.SourceIdentity || source.State != "parsed" || source.Role == "request") {
			return Dataset{}, invalid
		}
	}
	document, err := evidence.Document(record.Source)
	if err != nil {
		return Dataset{}, err
	}
	projected, err := document.Project(ctx, record.Dataset.Binding, record.Dataset.Projection)
	if err != nil || !bytes.Equal(canonical(projected), canonical(record.Dataset)) {
		return Dataset{}, invalid
	}
	return projected, nil
}
