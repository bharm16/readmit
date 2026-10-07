package desktop

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/hl7reference"
)

const referenceLibrarySchema = "readmit-hl7-reference-library/v1"
const maxReferenceLibraryBytes = 16384

var referenceEditions = hl7reference.Editions()

type ReferenceEdition struct {
	Edition  string `json:"edition"`
	Path     string `json:"path,omitzero"`
	Identity string `json:"identity,omitzero"`
}

type ReferenceLibraryResult struct {
	State    State              `json:"state"`
	Reason   string             `json:"reason,omitzero"`
	Editions []ReferenceEdition `json:"editions"`
}

func (r *ReferenceLibraryResult) refuse(state State, reason string) {
	r.State = state
	r.Reason = reason
	r.Editions = []ReferenceEdition{}
}

type referenceLibraryDocument struct {
	Schema   string             `json:"schema"`
	Editions []ReferenceEdition `json:"editions"`
}

func (a *App) referenceLibrary() ([]ReferenceEdition, error) {
	if a.bundledReferenceError != nil {
		return nil, a.bundledReferenceError
	}
	installed, err := a.installedReferenceLibrary()
	if err != nil {
		return nil, err
	}
	return a.withBundledReferences(installed), nil
}

func (a *App) withBundledReferences(installed []ReferenceEdition) []ReferenceEdition {
	entries := slices.Clone(a.bundledReferences)
	for _, entry := range installed {
		entries = slices.DeleteFunc(entries, func(builtIn ReferenceEdition) bool { return builtIn.Edition == entry.Edition })
		entries = append(entries, entry)
	}
	return entries
}

func (a *App) installedReferenceLibrary() ([]ReferenceEdition, error) {
	if !filepath.IsAbs(a.documents.Folder) {
		return []ReferenceEdition{}, nil
	}
	raw, err := a.documents.read(referenceLibraryName, maxReferenceLibraryBytes)
	if errors.Is(err, fs.ErrNotExist) {
		return []ReferenceEdition{}, nil
	}
	if err != nil {
		return nil, err
	}
	var d referenceLibraryDocument
	if json.Unmarshal(raw, &d, json.RejectUnknownMembers(true)) != nil || d.Schema != referenceLibrarySchema || len(d.Editions) > len(referenceEditions) {
		return nil, errNotADocument
	}
	seen := map[string]bool{}
	for _, entry := range d.Editions {
		if !slices.Contains(referenceEditions, entry.Edition) || seen[entry.Edition] || !filepath.IsAbs(entry.Path) || len(entry.Path) > 4096 || (!strings.HasPrefix(entry.Identity, "sha256:") || !validExportDigest(strings.TrimPrefix(entry.Identity, "sha256:"))) {
			return nil, errNotADocument
		}
		seen[entry.Edition] = true
	}
	return d.Editions, nil
}

func referenceLibraryResult(entries []ReferenceEdition) ReferenceLibraryResult {
	result := ReferenceLibraryResult{State: Completed, Editions: make([]ReferenceEdition, 0, len(referenceEditions))}
	for _, edition := range referenceEditions {
		entry := ReferenceEdition{Edition: edition}
		for _, installed := range entries {
			if installed.Edition == edition {
				entry = installed
				break
			}
		}
		result.Editions = append(result.Editions, entry)
	}
	return result
}

// ReadReferenceLibrary lists app definitions and explicitly chosen custom pins.
// The selected catalog is verified again when used; listing never repins content.
func (a *App) ReadReferenceLibrary() ReferenceLibraryResult {
	a.referenceLibraryMu.Lock()
	defer a.referenceLibraryMu.Unlock()
	entries, err := a.referenceLibrary()
	if err != nil {
		return ReferenceLibraryResult{State: Failed, Reason: "The app's HL7 definitions cannot be read. Saved selections are unchanged.", Editions: []ReferenceEdition{}}
	}
	return referenceLibraryResult(entries)
}

func (a *App) installReferenceCatalogs(ctx context.Context, paths []string) ReferenceLibraryResult {
	a.referenceLibraryMu.Lock()
	defer a.referenceLibraryMu.Unlock()
	previous, err := a.installedReferenceLibrary()
	if !filepath.IsAbs(a.documents.Folder) {
		return ReferenceLibraryResult{State: Failed, Reason: "The local reference store is not configured.", Editions: []ReferenceEdition{}}
	}
	fail := func(reason string) ReferenceLibraryResult {
		return ReferenceLibraryResult{State: Failed, Reason: reason, Editions: referenceLibraryResult(a.withBundledReferences(previous)).Editions}
	}
	if err != nil {
		return fail("The installed reference library cannot be read. Its selections are unchanged.")
	}
	type candidate struct {
		entry ReferenceEdition
		raw   []byte
	}
	candidates := []candidate{}
	seen := map[string]bool{}
	for _, path := range paths {
		if ctx.Err() != nil {
			return ReferenceLibraryResult{State: Cancelled, Reason: "Reference installation stopped. The previous selection remains.", Editions: referenceLibraryResult(previous).Editions}
		}
		if !filepath.IsAbs(path) {
			return fail("Choose an absolute reference catalog path.")
		}
		raw, err := (artifactdir.Document{MaxBytes: hl7reference.MaxBytes}).Read(path)
		if err != nil {
			return fail("A library catalog is missing, linked, unreadable or too large.")
		}
		catalog, err := hl7reference.Decode(raw)
		if err != nil {
			return fail("A library catalog is invalid. The previous library is unchanged.")
		}
		edition := catalog.Edition()
		if !slices.Contains(referenceEditions, edition) || seen[edition] {
			return fail("The library requires one catalog per supported edition.")
		}
		seen[edition] = true
		identity := catalog.Summary().Identity
		owned := filepath.Join(a.documents.Folder, "reference-catalogs", strings.TrimPrefix(identity, "sha256:")+".json")
		candidates = append(candidates, candidate{ReferenceEdition{edition, owned, identity}, raw})
	}
	if len(candidates) == 0 {
		return fail("Choose a library containing reference catalogs.")
	}
	if err := os.MkdirAll(filepath.Join(a.documents.Folder, "reference-catalogs"), 0700); err != nil {
		return fail("The reference catalog store cannot be created.")
	}
	next := slices.Clone(previous)
	for _, item := range candidates {
		if ctx.Err() != nil {
			return ReferenceLibraryResult{State: Cancelled, Reason: "Reference installation stopped. The previous selection remains.", Editions: referenceLibraryResult(previous).Editions}
		}
		doc := artifactdir.Document{MaxBytes: hl7reference.MaxBytes}
		if existing, err := doc.Read(item.entry.Path); err == nil {
			checked, err := hl7reference.Decode(existing)
			if err != nil || checked.Summary().Identity != item.entry.Identity {
				return fail("An installed catalog changed. It cannot be replaced implicitly.")
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return fail("An installed catalog is unreadable.")
		} else if err := doc.Create(item.entry.Path, item.raw); err != nil {
			return fail("The reference catalog could not be retained.")
		}
		next = slices.DeleteFunc(next, func(e ReferenceEdition) bool { return e.Edition == item.entry.Edition })
		next = append(next, item.entry)
	}
	if ctx.Err() != nil {
		return ReferenceLibraryResult{State: Cancelled, Reason: "Reference installation stopped. The previous selection remains.", Editions: referenceLibraryResult(previous).Editions}
	}
	raw, err := json.Marshal(referenceLibraryDocument{referenceLibrarySchema, next}, json.Deterministic(true))
	if err != nil || len(raw) > maxReferenceLibraryBytes {
		return fail("The reference library exceeds its storage bound.")
	}
	if err := a.documents.write(referenceLibraryName, append(raw, '\n')); err != nil {
		return fail("The reference library could not be saved. Its previous selections remain in use.")
	}
	return referenceLibraryResult(a.withBundledReferences(next))
}

// InstallReferenceCatalog retains exact supplied catalog bytes in the local store.
func (a *App) InstallReferenceCatalog(path string) ReferenceLibraryResult {
	return run(a, true, false, func(ctx context.Context) ReferenceLibraryResult {
		return a.installReferenceCatalogs(ctx, []string{path})
	})
}

// InstallReferenceLibrary imports a bounded manifest and its regular catalog files.
// No network acquisition or reference text is written into evidence or release files.
func (a *App) InstallReferenceLibrary(folder string) ReferenceLibraryResult {
	return run(a, true, false, func(ctx context.Context) ReferenceLibraryResult { return a.installReferenceLibrary(ctx, folder) })
}

func (a *App) installReferenceLibrary(ctx context.Context, folder string) ReferenceLibraryResult {
	fail := func(reason string) ReferenceLibraryResult {
		return ReferenceLibraryResult{State: Failed, Reason: reason, Editions: []ReferenceEdition{}}
	}
	if !filepath.IsAbs(folder) {
		return fail("Choose an absolute reference library folder.")
	}
	raw, err := (artifactdir.Document{MaxBytes: maxReferenceLibraryBytes}).Read(filepath.Join(folder, "library.json"))
	if err != nil {
		return fail("The folder has no readable reference library manifest.")
	}
	var manifest struct {
		Schema   string   `json:"schema"`
		Catalogs []string `json:"catalogs"`
	}
	if json.Unmarshal(raw, &manifest, json.RejectUnknownMembers(true)) != nil || manifest.Schema != referenceLibrarySchema || len(manifest.Catalogs) < 1 || len(manifest.Catalogs) > len(referenceEditions) {
		return fail("The reference library manifest is invalid.")
	}
	paths := []string{}
	for _, name := range manifest.Catalogs {
		if name == "" || name != filepath.Base(name) || strings.ContainsAny(name, "/\\") || !strings.HasSuffix(name, ".json") {
			return fail("The reference library names a file outside its folder.")
		}
		paths = append(paths, filepath.Join(folder, name))
	}
	return a.installReferenceCatalogs(ctx, paths)
}

func (a *App) installedReference(edition string) (string, string, error) {
	a.referenceLibraryMu.Lock()
	defer a.referenceLibraryMu.Unlock()
	entries, err := a.referenceLibrary()
	if err != nil {
		return "", "", err
	}
	for _, entry := range entries {
		if entry.Edition == edition {
			return entry.Path, entry.Identity, nil
		}
	}
	return "", "", nil
}
