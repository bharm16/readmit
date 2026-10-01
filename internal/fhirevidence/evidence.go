// Package fhirevidence retains explicitly declared R4 sources. Its reader
// verifies one byte snapshot and uses fhirr4 and fhirrequest to interpret it;
// neither reading nor publication has network or executable authority.
package fhirevidence

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/fhirr4"
	"github.com/bharm16/readmit/internal/fhirrequest"
	"github.com/bharm16/readmit/internal/strictdoc"
)

const Schema = "readmit-fhir-evidence/v1"
const ManifestName = "fhir-evidence.json"
const SourceID = "fhir"
const SourceName = "source.json"

var invalid = errors.New("invalid or unsupported retained FHIR R4 evidence")
var digest = regexp.MustCompile(`^[a-f0-9]{64}$`)

type RequestDeclaration struct {
	Method  string              `json:"method"`
	URL     string              `json:"url"`
	Headers fhirrequest.Headers `json:"headers"`
}
type Declaration struct {
	SourceKind string              `json:"source_kind"`
	Context    fhirr4.Context      `json:"context"`
	Request    *RequestDeclaration `json:"request,omitzero"`
}
type Provenance struct {
	Mode       string     `json:"mode"`
	ImportedAt *time.Time `json:"imported_at,omitzero"`
	Derivation string     `json:"derivation,omitzero"`
	Parent     string     `json:"parent,omitzero"`
}
type Manifest struct {
	Schema      string      `json:"schema"`
	Declaration Declaration `json:"declaration"`
	Provenance  Provenance  `json:"provenance"`
	SHA256      string      `json:"sha256"`
	Size        int         `json:"size"`
	Resources   int         `json:"resources"`
}
type Artifact struct {
	Identity string
	Manifest Manifest
	Document *fhirr4.Document
	Request  *fhirrequest.Request
	raw      []byte
}

func (a *Artifact) Raw() []byte { return bytes.Clone(a.raw) }

var layout = artifactdir.Layout{Noun: "FHIR evidence", RequiredFiles: []string{ManifestName, SourceName, "identity.sha256"},
	AllowFile: func(name string) bool {
		return slices.Contains([]string{ManifestName, SourceName, "identity.sha256"}, name)
	},
	MaxFiles: 3, MaxFileBytes: fhirr4.MaxBytes, MaxBytes: fhirr4.MaxBytes + (64 << 10)}
var family = artifactdir.Family{Layout: layout, Seal: artifactdir.DirectoryHash(Schema), Incomplete: artifactdir.RemoveIncomplete}
var manifestDocument = strictdoc.Document{MaxBytes: 64 << 10, Schema: Schema, Required: []string{"declaration", "provenance", "sha256", "size", "resources"}, Invalid: invalid.Error(), TooLarge: invalid.Error(), MustDeclare: invalid.Error(), Requires: invalid.Error(), Unsupported: invalid}

// Interpret verifies a declaration against original bytes, without publishing.
func Interpret(ctx context.Context, declaration Declaration, raw []byte) (*fhirr4.Document, *fhirrequest.Request, error) {
	if declaration.Context.Version != fhirr4.Version || declaration.Context.MediaType != "application/fhir+json" || len(raw) > fhirr4.MaxBytes || !slices.Contains([]string{"resource", "bundle", "request"}, declaration.SourceKind) {
		return nil, nil, invalid
	}
	var request *fhirrequest.Request
	if declaration.SourceKind == "request" {
		if declaration.Request == nil {
			return nil, nil, invalid
		}
		r := declaration.Request
		contentType := ""
		if len(raw) > 0 {
			contentType = declaration.Context.MediaType
		}
		parsed, err := fhirrequest.Parse(declaration.Context.Base, r.Method, r.URL, contentType, raw, r.Headers)
		if err != nil {
			return nil, nil, err
		}
		request = &parsed
	} else if declaration.Request != nil {
		return nil, nil, invalid
	}
	if len(raw) == 0 {
		if request == nil {
			return nil, nil, invalid
		}
		return nil, request, nil
	}
	doc, err := fhirr4.Decode(ctx, raw, declaration.Context)
	if err != nil {
		return nil, nil, err
	}
	resources := doc.Resources()
	if len(resources) == 0 || resources[0].Type == "" {
		return nil, nil, invalid
	}
	if declaration.SourceKind == "bundle" && resources[0].Type != "Bundle" || declaration.SourceKind == "resource" && resources[0].Type == "Bundle" {
		return nil, nil, invalid
	}
	return doc, request, nil
}

func validProvenance(p Provenance) bool {
	if !slices.Contains([]string{"imported", "generated", "derived"}, p.Mode) || len(p.Derivation) > 256 || strings.ContainsAny(p.Derivation, "\r\n\x00") || p.ImportedAt != nil && (p.ImportedAt.IsZero() || p.Mode != "imported") {
		return false
	}
	if p.Mode == "derived" {
		return digest.MatchString(p.Parent) && p.Derivation != ""
	}
	return p.Parent == ""
}

// Create seals an immutable source and its exact declaration. The directory
// identity includes provenance and HTTP conditions, even for an empty body.
func Create(ctx context.Context, destination string, declaration Declaration, raw []byte, provenance Provenance) (*Artifact, error) {
	if !validProvenance(provenance) {
		return nil, invalid
	}
	doc, _, err := Interpret(ctx, declaration, raw)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	manifest := Manifest{Schema: Schema, Declaration: declaration, Provenance: provenance, SHA256: hex.EncodeToString(sum[:]), Size: len(raw)}
	if doc != nil {
		manifest.Resources = len(doc.Resources())
	}
	data, err := json.Marshal(manifest, json.Deterministic(true))
	if err != nil {
		return nil, err
	}
	if _, err := artifactdir.Write(ctx, destination, family, artifactdir.Durable, map[string][]byte{ManifestName: append(data, '\n'), SourceName: bytes.Clone(raw)}); err != nil {
		return nil, err
	}
	return Open(ctx, destination)
}

// Open verifies the seal, source digest, explicit format and resource census
// from the same captured snapshot before answering any interpretation.
func Open(ctx context.Context, directory string) (*Artifact, error) {
	files, err := artifactdir.Read(directory, layout)
	if err != nil {
		return nil, err
	}
	identity := strings.TrimSuffix(string(files["identity.sha256"]), "\n")
	if !digest.MatchString(identity) || identity != artifactdir.Identity(Schema, files) {
		return nil, invalid
	}
	var manifest Manifest
	if manifestDocument.Decode(files[ManifestName], &manifest) != nil || !validProvenance(manifest.Provenance) {
		return nil, invalid
	}
	raw := files[SourceName]
	sum := sha256.Sum256(raw)
	if manifest.Size != len(raw) || manifest.SHA256 != hex.EncodeToString(sum[:]) {
		return nil, invalid
	}
	doc, request, err := Interpret(ctx, manifest.Declaration, raw)
	if err != nil {
		return nil, err
	}
	count := 0
	if doc != nil {
		count = len(doc.Resources())
	}
	if count != manifest.Resources {
		return nil, invalid
	}
	return &Artifact{Identity: identity, Manifest: manifest, Document: doc, Request: request, raw: bytes.Clone(raw)}, nil
}

// Describe reads only the declaration for discovery. Open remains the verifier.
func Describe(directory string) (Manifest, error) {
	data, err := (artifactdir.Document{MaxBytes: 64 << 10}).Read(filepath.Join(directory, ManifestName))
	var manifest Manifest
	if err != nil {
		return manifest, err
	}
	if manifestDocument.Decode(data, &manifest) != nil || !validProvenance(manifest.Provenance) {
		return manifest, invalid
	}
	return manifest, nil
}
