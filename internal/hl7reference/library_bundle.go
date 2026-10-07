package hl7reference

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/artifactdir"
)

const BundleSchema = "readmit-hl7-reference-bundle/v1"
const maxBundleBytes = 128 << 20
const maxLibraryManifestBytes = 16 << 10

// Editions is the exact supported reference inventory, independent of evaluator
// qualification. Callers receive a copy so no window can change that inventory.
func Editions() []string {
	return []string{"2.9", "2.8.2", "2.8.1", "2.8", "2.7.1", "2.7", "2.6", "2.5.1", "2.5", "2.4", "2.3.1", "2.3", "2.2", "2.1"}
}

type BundleEntry struct {
	Edition string `json:"edition"`
	File    string `json:"file"`
	SHA256  string `json:"sha256"`
	Bytes   int64  `json:"bytes"`
}

type bundleManifest struct {
	Schema   string        `json:"schema"`
	Catalogs []BundleEntry `json:"catalogs"`
}

// Bundle is immutable application content. Each member is independently pinned,
// bounded and validated; neither an archive path nor a nearby edition is used.
type Bundle struct {
	entries []BundleEntry
	files   map[string]*zip.File
}

// BuildBundle runs at build time over the maintainer's complete source library.
// It preserves every catalog byte, including provenance and explicit gaps.
func BuildBundle(folder string, output io.Writer) error {
	raw, err := (artifactdir.Document{MaxBytes: maxLibraryManifestBytes}).Read(filepath.Join(folder, "library.json"))
	if err != nil {
		return errors.New("the build has no readable HL7 library manifest")
	}
	var library struct {
		Schema   string   `json:"schema"`
		Catalogs []string `json:"catalogs"`
	}
	if json.Unmarshal(raw, &library, json.RejectUnknownMembers(true)) != nil || library.Schema != "readmit-hl7-reference-library/v1" || len(library.Catalogs) != len(Editions()) {
		return errors.New("the desktop build requires all 14 HL7 editions")
	}
	archive := zip.NewWriter(output)
	manifest := bundleManifest{Schema: BundleSchema}
	seen := map[string]bool{}
	for _, name := range library.Catalogs {
		if name != filepath.Base(name) || strings.ContainsAny(name, "/\\") || !strings.HasSuffix(name, ".json") {
			return errors.New("the HL7 library names a file outside its folder")
		}
		raw, err := (artifactdir.Document{MaxBytes: MaxBytes}).Read(filepath.Join(folder, name))
		if err != nil {
			return errors.New("a build catalog is missing, linked or too large")
		}
		catalog, err := Decode(raw)
		if err != nil || !slices.Contains(Editions(), catalog.Edition()) || seen[catalog.Edition()] {
			return errors.New("the build library requires one valid catalog per HL7 edition")
		}
		seen[catalog.Edition()] = true
		entry := BundleEntry{catalog.Edition(), "hl7-" + catalog.Edition() + ".json", strings.TrimPrefix(catalog.Summary().Identity, "sha256:"), int64(len(raw))}
		manifest.Catalogs = append(manifest.Catalogs, entry)
		header := &zip.FileHeader{Name: entry.File, Method: zip.Deflate}
		header.SetMode(0600)
		member, err := archive.CreateHeader(header)
		if err != nil {
			return err
		}
		if _, err := member.Write(raw); err != nil {
			return err
		}
	}
	slices.SortFunc(manifest.Catalogs, func(a, b BundleEntry) int { return strings.Compare(a.Edition, b.Edition) })
	raw, err = json.Marshal(manifest, json.Deterministic(true))
	if err != nil {
		return err
	}
	header := &zip.FileHeader{Name: "manifest.json", Method: zip.Deflate}
	header.SetMode(0600)
	member, err := archive.CreateHeader(header)
	if err != nil {
		return err
	}
	if _, err = member.Write(raw); err != nil {
		return err
	}
	return archive.Close()
}

func OpenBundle(raw []byte) (*Bundle, error) {
	if len(raw) == 0 || len(raw) > maxBundleBytes {
		return nil, errors.New("the app's HL7 definitions are missing or exceed the bundle bound")
	}
	archive, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil || len(archive.File) != len(Editions())+1 {
		return nil, errors.New("the app's HL7 definition bundle is invalid")
	}
	files := map[string]*zip.File{}
	for _, file := range archive.File {
		if !file.Mode().IsRegular() || files[file.Name] != nil {
			return nil, errors.New("the HL7 bundle contains duplicate or non-regular members")
		}
		files[file.Name] = file
	}
	body, err := bundleMember(files["manifest.json"], maxLibraryManifestBytes)
	if err != nil {
		return nil, err
	}
	var manifest bundleManifest
	if json.Unmarshal(body, &manifest, json.RejectUnknownMembers(true)) != nil || manifest.Schema != BundleSchema || len(manifest.Catalogs) != len(Editions()) {
		return nil, errors.New("the app's HL7 definition manifest is invalid")
	}
	seen := map[string]bool{}
	for _, entry := range manifest.Catalogs {
		digest, err := hex.DecodeString(entry.SHA256)
		file := files[entry.File]
		if !slices.Contains(Editions(), entry.Edition) || seen[entry.Edition] || entry.File != "hl7-"+entry.Edition+".json" || err != nil || len(digest) != sha256.Size || entry.Bytes <= 0 || entry.Bytes > MaxBytes || file == nil || file.UncompressedSize64 != uint64(entry.Bytes) {
			return nil, errors.New("the HL7 bundle requires one bounded, pinned catalog per edition")
		}
		seen[entry.Edition] = true
	}
	return &Bundle{manifest.Catalogs, files}, nil
}

func bundleMember(file *zip.File, limit int64) ([]byte, error) {
	if file == nil || file.UncompressedSize64 > uint64(limit) {
		return nil, errors.New("an HL7 bundle member is missing or exceeds its bound")
	}
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	raw, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil || int64(len(raw)) > limit {
		return nil, errors.New("an HL7 bundle member could not be read within its bound")
	}
	return raw, nil
}

func (b *Bundle) Entries() []BundleEntry { return slices.Clone(b.entries) }

func (b *Bundle) catalogBytes(entry BundleEntry) ([]byte, error) {
	raw, err := bundleMember(b.files[entry.File], MaxBytes)
	if err != nil || int64(len(raw)) != entry.Bytes || fmt.Sprintf("%x", sha256.Sum256(raw)) != entry.SHA256 {
		return nil, errors.New("an app HL7 catalog differs from its bundled identity")
	}
	catalog, err := Decode(raw)
	if err != nil || catalog.Edition() != entry.Edition {
		return nil, errors.New("an app HL7 catalog does not describe its exact edition")
	}
	return raw, nil
}

// Verify is the packaging check. An empty or incomplete application cannot pass.
func (b *Bundle) Verify() error {
	for _, entry := range b.entries {
		if _, err := b.catalogBytes(entry); err != nil {
			return err
		}
	}
	return nil
}

// Retain materializes immutable app resources for the existing pinned file
// reader. It writes no user selection and never replaces changed cached bytes.
// Subsequent launches verify hashes without reparsing every edition.
func (b *Bundle) Retain(folder string) error {
	if !filepath.IsAbs(folder) {
		return errors.New("the app's HL7 definition store is unavailable")
	}
	if err := os.MkdirAll(folder, 0700); err != nil {
		return err
	}
	for _, entry := range b.entries {
		path := filepath.Join(folder, entry.SHA256+".json")
		document := artifactdir.Document{MaxBytes: MaxBytes}
		if existing, err := document.Read(path); err == nil {
			if int64(len(existing)) != entry.Bytes || fmt.Sprintf("%x", sha256.Sum256(existing)) != entry.SHA256 {
				return errors.New("a cached app HL7 catalog changed; no definition was replaced")
			}
			continue
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		raw, err := b.catalogBytes(entry)
		if err != nil {
			return err
		}
		if err := document.Create(path, raw); err != nil {
			return err
		}
	}
	return nil
}
