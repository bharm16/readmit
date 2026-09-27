package fhirvalidator

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/networkaction"
)

var invalid = errors.New("invalid offline FHIR validation capability or request")
var packageID = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,127}$`)
var packageVersion = regexp.MustCompile(`^[0-9]+(\.[0-9]+){1,3}([+-][A-Za-z0-9.-]+)?$`)

// containedReference is a FHIR id after '#': a resource contained in the same
// definition, which R4 core's own example ValueSets name as a code system.
var containedReference = regexp.MustCompile(`^#[A-Za-z0-9.-]{1,64}$`)
var capabilityFamily = artifactdir.Family{Layout: artifactdir.Layout{Noun: "FHIR validation capability", RequiredFiles: []string{"manifest.json", "identity.sha256"}, AllowFile: func(n string) bool { return n == "manifest.json" || n == "identity.sha256" || assetPath(n) }, AllowDirectory: func(n string) bool {
	return n == "metadata" || n == "licenses" || strings.HasPrefix(n, "metadata/") || strings.HasPrefix(n, "licenses/")
}, MaxFiles: 514, MaxFileBytes: 8 << 20, MaxBytes: 64 << 20}, Seal: artifactdir.DirectoryHash(CapabilitySchema)}

type Capability struct {
	manifest Manifest
	identity string
	files    map[string][]byte
}

func (c *Capability) Identity() string { return c.identity }
func (c *Capability) Manifest() Manifest {
	var m Manifest
	_ = json.Unmarshal(encode(c.manifest), &m)
	return m
}
func (Capability) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "FHIR validation capability (private)")
}
func encode(v any) []byte      { b, _ := json.Marshal(v, json.Deterministic(true)); return b }
func digest(raw []byte) string { return networkaction.Digest(raw) }
func assetPath(p string) bool {
	return len(p) > 0 && len(p) < 256 && path.Clean(p) == p && !strings.ContainsAny(p, "\\\r\n:") && (strings.HasPrefix(p, "metadata/") || strings.HasPrefix(p, "licenses/")) && !strings.Contains(p, "/.")
}
func canonicalURL(raw string) bool { return inventoryURL(raw) && !strings.Contains(raw, "|") }

// Inventory preserves legacy package URI spellings (including literal '|')
// without promoting them to selectable profile arguments or network targets.
func inventoryURL(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && len(raw) > 0 && len(raw) <= 4096 && u.User == nil && u.Fragment == "" && !strings.ContainsAny(raw, "\r\n\\ ") && ((u.Scheme == "http" || u.Scheme == "https") && u.Host != "" || u.Scheme == "urn" && u.Opaque != "")
}
func key(p PackageRef) string { return p.ID + "#" + p.Version }
func validateManifest(m Manifest, assets map[string][]byte) error {
	if m.Schema != CapabilitySchema || m.Platform != "linux/arm64" || !strings.HasPrefix(m.Image, "sha256:") || !networkaction.ValidDigest(strings.TrimPrefix(m.Image, "sha256:")) || !strings.Contains(m.BaseImage, "@sha256:") || !networkaction.ValidDigest(strings.SplitN(m.BaseImage, "@sha256:", 2)[1]) || !networkaction.ValidDigest(m.LauncherSHA256) || len(m.Packages) < 1 || len(m.Packages) > 128 || len(m.Profiles) > 10000 || len(m.Terminology) > 10000 || len(m.Assets) < 1 || len(m.Assets) > 512 {
		return invalid
	}
	if m.Base.SHA256 != strings.SplitN(m.BaseImage, "@sha256:", 2)[1] {
		return invalid
	}
	for _, component := range []Component{m.Validator, m.Runtime, m.Base, m.Adapter.Compiler} {
		if len(component.Name) < 1 || len(component.Name) > 256 || len(component.Version) < 1 || len(component.Version) > 128 || !networkaction.ValidDigest(component.SHA256) || component.License == "" || len(component.License) > 256 || !assetPath(component.SBOM) {
			return invalid
		}
	}
	if !networkaction.ValidDigest(m.Adapter.SourceSHA256) || !networkaction.ValidDigest(m.Adapter.JarSHA256) {
		return invalid
	}
	packages := map[string]Package{}
	for _, p := range m.Packages {
		if !packageID.MatchString(p.ID) || !packageVersion.MatchString(p.Version) || !networkaction.ValidDigest(p.SHA256) || p.License == "" || len(p.License) > 256 || len(p.Dependencies) > 128 {
			return invalid
		}
		k := key(PackageRef{p.ID, p.Version})
		if _, ok := packages[k]; ok {
			return invalid
		}
		packages[k] = p
	}
	if core, ok := packages["hl7.fhir.r4.core#4.0.1"]; !ok || core.SHA256 != requiredRoots["hl7.fhir.r4.core#4.0.1"] {
		return invalid
	}
	visiting, visited := map[string]bool{}, map[string]bool{}
	var visit func(string) error
	visit = func(k string) error {
		if visiting[k] {
			return invalid
		}
		if visited[k] {
			return nil
		}
		p, ok := packages[k]
		if !ok {
			return invalid
		}
		visiting[k] = true
		seen := map[string]bool{}
		for _, d := range p.Dependencies {
			if !packageID.MatchString(d.ID) || !packageVersion.MatchString(d.Version) || seen[key(d)] {
				return invalid
			}
			seen[key(d)] = true
			if e := visit(key(d)); e != nil {
				return e
			}
		}
		visiting[k] = false
		visited[k] = true
		return nil
	}
	for k := range packages {
		if e := visit(k); e != nil {
			return e
		}
	}
	if len(m.ValidatorPackages) > 128 {
		return invalid
	}
	roots := map[string]bool{}
	for _, root := range m.ValidatorPackages {
		if _, ok := packages[key(root)]; !ok || roots[key(root)] {
			return invalid
		}
		roots[key(root)] = true
	}
	validCanonical := func(c Canonical) bool {
		_, ok := packages[key(c.Package)]
		return ok && canonicalURL(c.URL) && len(c.Version) <= 128 && !strings.ContainsAny(c.Version, "\r\n|\\") && networkaction.ValidDigest(c.SHA256)
	}
	profiles := map[string]bool{}
	for _, p := range m.Profiles {
		k := p.URL + "|" + p.Version
		if !validCanonical(p) || profiles[k] {
			return invalid
		}
		profiles[k] = true
	}
	terms := map[string]bool{}
	for _, t := range m.Terminology {
		k := t.Kind + "|" + t.Canonical.URL + "|" + t.Canonical.Version + "|" + key(t.Canonical.Package) + "|" + t.Canonical.SHA256
		_, knownPackage := packages[key(t.Canonical.Package)]
		if !knownPackage || !inventoryURL(t.Canonical.URL) || len(t.Canonical.Version) > 128 || strings.ContainsAny(t.Canonical.Version, "\r\n|\\") || !networkaction.ValidDigest(t.Canonical.SHA256) || terms[k] || !slices.Contains([]string{"CodeSystem", "ValueSet"}, t.Kind) || !slices.Contains([]string{"complete", "fragment", "not-present", "example", "supplement", "expansion", "compose", "unavailable"}, t.Content) || len(t.References) > 512 {
			return invalid
		}
		for _, r := range t.References {
			if !inventoryURL(r.URL) && !containedReference.MatchString(r.URL) || len(r.Version) > 128 || strings.ContainsAny(r.Version, "\r\n|\\") {
				return invalid
			}
		}
		terms[k] = true
	}
	names := map[string]bool{}
	total := 0
	for _, a := range m.Assets {
		raw, ok := assets[a.Path]
		if !assetPath(a.Path) || names[a.Path] || !ok || len(raw) > 8<<20 || int64(len(raw)) != a.Bytes || digest(raw) != a.SHA256 || !slices.Contains([]string{"sbom", "license", "package-inventory", "adapter-source", "adapter-binary"}, a.Role) {
			return invalid
		}
		names[a.Path] = true
		total += len(raw)
		if total > 64<<20 {
			return invalid
		}
		if a.Role == "sbom" {
			var doc map[string]any
			if !boundedJSON(raw) || json.Unmarshal(raw, &doc) != nil || doc == nil {
				return invalid
			}
		}
	}
	if len(assets) != len(names) {
		return invalid
	}
	for _, c := range []Component{m.Validator, m.Runtime, m.Base, m.Adapter.Compiler} {
		found := false
		for _, a := range m.Assets {
			if a.Path == c.SBOM && a.Role == "sbom" {
				found = true
			}
		}
		if !found {
			return invalid
		}
	}
	sources, binaries := 0, 0
	for _, a := range m.Assets {
		if a.Role == "adapter-source" && a.SHA256 == m.Adapter.SourceSHA256 {
			sources++
		}
		if a.Role == "adapter-binary" && a.SHA256 == m.Adapter.JarSHA256 {
			binaries++
		}
	}
	if sources != 1 || binaries != 1 {
		return invalid
	}
	return nil
}

// Stage is an explicit local/admin write. It installs only an already acquired
// immutable descriptor and inventories; it never downloads or runs a program.
func Stage(ctx context.Context, output string, m Manifest, assets map[string][]byte) (*Capability, error) {
	// Own a complete snapshot before validating or writing. Nested caller
	// slices must not change the facts identified by the staged seal.
	raw, err := json.Marshal(m, json.Deterministic(true))
	if err != nil {
		return nil, invalid
	}
	var snapshot Manifest
	if json.Unmarshal(raw, &snapshot, json.RejectUnknownMembers(true)) != nil {
		return nil, invalid
	}
	m = snapshot
	copied := make(map[string][]byte, len(assets))
	for name, data := range assets {
		copied[name] = bytes.Clone(data)
	}
	assets = copied
	if validateManifest(m, assets) != nil {
		return nil, invalid
	}
	m.Assets = slices.Clone(m.Assets)
	slices.SortFunc(m.Assets, func(a, b Asset) int { return strings.Compare(a.Path, b.Path) })
	files := map[string][]byte{"manifest.json": encode(m)}
	for name, raw := range assets {
		files[name] = bytes.Clone(raw)
	}
	w, e := artifactdir.Create(output, capabilityFamily, artifactdir.Durable)
	if e != nil {
		return nil, e
	}
	defer w.Close()
	dirs := map[string]bool{}
	for name := range assets {
		for dir := path.Dir(name); dir != "."; dir = path.Dir(dir) {
			dirs[dir] = true
		}
	}
	ordered := []string{}
	for d := range dirs {
		ordered = append(ordered, d)
	}
	slices.Sort(ordered)
	for _, d := range ordered {
		if w.Mkdir(d) != nil {
			return nil, invalid
		}
	}
	for name, raw := range files {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if w.WriteFile(name, raw) != nil {
			return nil, invalid
		}
	}
	identity, e := w.Seal(nil)
	if e != nil {
		return nil, e
	}
	return &Capability{manifest: m, identity: identity, files: files}, nil
}

// StageBuild seals the manifest and assets an administrator's build wrote to
// metadata, reading each named member inside that folder only.
func StageBuild(ctx context.Context, metadata, output string) (*Capability, error) {
	root, err := os.OpenRoot(metadata)
	if err != nil {
		return nil, invalid
	}
	defer root.Close()
	read := func(name string) ([]byte, error) {
		f, err := root.Open(filepath.FromSlash(name))
		if err != nil {
			return nil, err
		}
		defer f.Close()
		raw, err := io.ReadAll(io.LimitReader(f, 8<<20+1))
		if err == nil && len(raw) > 8<<20 {
			err = invalid
		}
		return raw, err
	}
	raw, err := read("manifest.json")
	var m Manifest
	if err != nil || json.Unmarshal(raw, &m, json.RejectUnknownMembers(true)) != nil || len(m.Assets) > 512 {
		return nil, invalid
	}
	assets := map[string][]byte{}
	for _, a := range m.Assets {
		if !assetPath(a.Path) {
			return nil, invalid
		}
		if assets[a.Path], err = read(a.Path); err != nil {
			return nil, invalid
		}
	}
	return Stage(ctx, output, m, assets)
}
func OpenCapability(directory string) (*Capability, error) {
	files, e := artifactdir.Read(directory, capabilityFamily.Layout)
	if e != nil {
		return nil, invalid
	}
	return verifyCapability(files)
}
func verifyCapability(files map[string][]byte) (*Capability, error) {
	if len(files) > capabilityFamily.Layout.MaxFiles {
		return nil, invalid
	}
	total := 0
	for name, raw := range files {
		if name != "manifest.json" && name != "identity.sha256" && !assetPath(name) || len(raw) > capabilityFamily.Layout.MaxFileBytes {
			return nil, invalid
		}
		total += len(raw)
	}
	if total > capabilityFamily.Layout.MaxBytes {
		return nil, invalid
	}
	identity := strings.TrimSpace(string(files["identity.sha256"]))
	if identity != artifactdir.Identity(CapabilitySchema, files) {
		return nil, invalid
	}
	var m Manifest
	if json.Unmarshal(files["manifest.json"], &m, json.RejectUnknownMembers(true)) != nil {
		return nil, invalid
	}
	assets := map[string][]byte{}
	for name, raw := range files {
		if name != "manifest.json" && name != "identity.sha256" {
			assets[name] = raw
		}
	}
	if validateManifest(m, assets) != nil {
		return nil, invalid
	}
	return &Capability{manifest: m, identity: identity, files: files}, nil
}
