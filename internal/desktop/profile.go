package desktop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/localprofile"
	"github.com/bharm16/readmit/internal/profilelibrary"
	"github.com/bharm16/readmit/internal/profilepack"
	"github.com/bharm16/readmit/internal/profilepackage"
	"github.com/bharm16/readmit/internal/profileversion"
)

// ProfilePackResult reports what one profile pack declares about its
// provenance, exact version and four support levels.
type ProfilePackResult struct {
	State      State                   `json:"state"`
	Reason     string                  `json:"reason,omitzero"`
	Pack       *profilepack.Identity   `json:"pack,omitzero"`
	Provenance *profilepack.Provenance `json:"provenance,omitzero"`
	Coverage   []profilepack.Coverage  `json:"coverage,omitzero"`
	Bundleable bool                    `json:"bundleable"`
}

func (r *ProfilePackResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ProfileLibraryEntry reports one pack the library holds with its recorded provenance.
type ProfileLibraryEntry struct {
	Pack       profilepack.Identity   `json:"pack"`
	Provenance profilepack.Provenance `json:"provenance"`
}

// ProfileLibraryRow reports one published combination of the support matrix.
type ProfileLibraryRow struct {
	HL7Version string               `json:"hl7_version"`
	Family     string               `json:"family"`
	Parse      profilepack.Outcome  `json:"parse"`
	Labels     profilepack.Outcome  `json:"labels"`
	Structural profilepack.Outcome  `json:"structural"`
	Workflow   profilepack.Outcome  `json:"workflow"`
	Pack       profilepack.Identity `json:"pack"`
}

// ProfileLibraryResult reports what a profile library directory publishes
// across all 28 version/family combinations.
type ProfileLibraryResult struct {
	State      State                 `json:"state"`
	Reason     string                `json:"reason,omitzero"`
	Entries    []ProfileLibraryEntry `json:"entries,omitzero"`
	Matrix     []ProfileLibraryRow   `json:"matrix,omitzero"`
	Bundleable bool                  `json:"bundleable"`
}

func (r *ProfileLibraryResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ProfileAssessment carries comparison results and tests affected by changes.
type ProfileAssessment struct {
	Comparison profileversion.Comparison     `json:"comparison"`
	Tests      []profileversion.AssessedTest `json:"tests"`
}

// ProfileValidateRequest carries a profile document to validate and an optional pack to resolve against.
type ProfileValidateRequest struct {
	Workspace string `json:"workspace"`
	Document  string `json:"document"`
	Pack      string `json:"pack,omitzero"`
}

// ProfileSaveRequest saves a canonical profile revision and its seal.
type ProfileSaveRequest struct {
	Workspace  string `json:"workspace"`
	Document   string `json:"document"`
	Output     string `json:"output"`
	SealOutput string `json:"seal_output,omitzero"`
}

// LocalProfileResult carries the decoded profile, its resolution against the pinned pack,
// and its version seal.
type LocalProfileResult struct {
	State      State                    `json:"state"`
	Reason     string                   `json:"reason,omitzero"`
	Document   string                   `json:"document,omitzero"`
	Output     string                   `json:"output,omitzero"`
	SealOutput string                   `json:"seal_output,omitzero"`
	Profile    *localprofile.Profile    `json:"profile,omitzero"`
	Resolution *localprofile.Resolution `json:"resolution,omitzero"`
	Seal       *profileversion.Version  `json:"seal,omitzero"`
}

func (r *LocalProfileResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ProfileCompareRequest compares two profile documents or files and optionally assesses impacted tests.
type ProfileCompareRequest struct {
	Workspace  string `json:"workspace"`
	From       string `json:"from"`
	To         string `json:"to"`
	References string `json:"references,omitzero"`
}

// ProfileCompareResult carries differences between two profile versions and impacted tests.
type ProfileCompareResult struct {
	State      State                      `json:"state"`
	Reason     string                     `json:"reason,omitzero"`
	Comparison *profileversion.Comparison `json:"comparison,omitzero"`
	Assessment *ProfileAssessment         `json:"assessment,omitzero"`
}

func (r *ProfileCompareResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// ProfileUpgradePinRequest upgrades one saved test pin to a new profile version.
type ProfileUpgradePinRequest struct {
	Workspace  string             `json:"workspace"`
	References string             `json:"references"`
	Test       string             `json:"test"`
	WasPin     profileversion.Pin `json:"was_pin"`
	NowPin     profileversion.Pin `json:"now_pin"`
	Output     string             `json:"output"`
}

// ProfileUpgradePinResult reports the updated references index.
type ProfileUpgradePinResult struct {
	State      State                      `json:"state"`
	Reason     string                     `json:"reason,omitzero"`
	Output     string                     `json:"output,omitzero"`
	References *profileversion.References `json:"references,omitzero"`
}

func (r *ProfileUpgradePinResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// ProfilePackageExportRequest exports an existing profile, pack, version and origin into a package.
type ProfilePackageExportRequest struct {
	Workspace string `json:"workspace"`
	Profile   string `json:"profile"`
	Pack      string `json:"pack"`
	Version   string `json:"version"`
	Origin    string `json:"origin"`
	Output    string `json:"output"`
	Reviewed  bool   `json:"reviewed"`
}

// ProfilePackageImportRequest imports a package into a new private directory.
type ProfilePackageImportRequest struct {
	Workspace string `json:"workspace"`
	Package   string `json:"package"`
	Output    string `json:"output"`
}

// ProfilePackageResult reports package operations, metadata, dependencies and rights.
type ProfilePackageResult struct {
	State      State                  `json:"state"`
	Reason     string                 `json:"reason,omitzero"`
	Output     string                 `json:"output,omitzero"`
	Origin     *profilepackage.Origin `json:"origin,omitzero"`
	Pack       *profilepack.Identity  `json:"pack,omitzero"`
	Profile    *localprofile.Identity `json:"profile,omitzero"`
	Version    *localprofile.Identity `json:"version,omitzero"`
	SHA256     string                 `json:"sha256,omitzero"`
	Conflict   string                 `json:"conflict,omitzero"`
	Dependency string                 `json:"dependency,omitzero"`
	Rights     string                 `json:"rights,omitzero"`
}

func (r *ProfilePackageResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// InspectProfilePack inspects a single profile pack entry in the workspace.
func (a *App) InspectProfilePack(workspace, entry string) ProfilePackResult {
	return run(a, false, false, func(context.Context) ProfilePackResult {
		root, declined := resolveFolder(workspace)
		if root == "" {
			return ProfilePackResult{State: declined.state, Reason: declined.reason}
		}
		data, declined := workspaceDocument(root, entry, profilepack.MaxPackBytes, "the profile pack")
		if data == nil {
			return ProfilePackResult{State: declined.state, Reason: declined.reason}
		}
		pack, err := profilepack.Decode(data)
		if err != nil {
			return ProfilePackResult{State: Failed, Reason: err.Error()}
		}
		id := pack.Identity
		prov := pack.Provenance
		return ProfilePackResult{
			State:      Completed,
			Pack:       &id,
			Provenance: &prov,
			Coverage:   pack.Coverage,
			Bundleable: prov.RightsReview.Status == "approved",
		}
	})
}

// OpenProfileLibrary opens a directory of profile packs as a library and returns its support matrix.
func (a *App) OpenProfileLibrary(workspace, directory string) ProfileLibraryResult {
	return run(a, false, false, func(context.Context) ProfileLibraryResult {
		root, declined := resolveFolder(workspace)
		if root == "" {
			return ProfileLibraryResult{State: declined.state, Reason: declined.reason}
		}
		dirPath := directory
		if !filepath.IsAbs(directory) {
			if directory == "" || directory == "." {
				dirPath = root
			} else {
				dirPath = filepath.Join(root, directory)
			}
		}
		lib, err := profilelibrary.Open(dirPath)
		if err != nil {
			return ProfileLibraryResult{State: Failed, Reason: err.Error()}
		}
		entries := make([]ProfileLibraryEntry, 0, len(lib.Entries()))
		for _, e := range lib.Entries() {
			entries = append(entries, ProfileLibraryEntry{
				Pack:       e.Pack,
				Provenance: e.Provenance,
			})
		}
		matrix := make([]ProfileLibraryRow, 0, len(lib.Matrix()))
		for _, r := range lib.Matrix() {
			matrix = append(matrix, ProfileLibraryRow{
				HL7Version: r.HL7Version,
				Family:     r.Family,
				Parse:      r.Parse,
				Labels:     r.Labels,
				Structural: r.Structural,
				Workflow:   r.Workflow,
				Pack:       r.Pack,
			})
		}
		return ProfileLibraryResult{
			State:      Completed,
			Entries:    entries,
			Matrix:     matrix,
			Bundleable: lib.Bundleable() == nil,
		}
	})
}

// OpenProfile reads an existing local profile from the workspace, resolves against its pack,
// and computes its canonical seal.
func (a *App) OpenProfile(workspace, entry, packEntry string) LocalProfileResult {
	return run(a, false, false, func(context.Context) LocalProfileResult {
		root, declined := resolveFolder(workspace)
		if root == "" {
			return LocalProfileResult{State: declined.state, Reason: declined.reason}
		}
		data, declined := workspaceDocument(root, entry, localprofile.MaxProfileBytes, "the local profile")
		if data == nil {
			return LocalProfileResult{State: declined.state, Reason: declined.reason}
		}
		profile, err := localprofile.Decode(data)
		if err != nil {
			return LocalProfileResult{State: Failed, Reason: err.Error()}
		}
		pack := a.findOrReadPack(root, packEntry, profile.Base.Pack)
		resolution := localprofile.Resolve(profile, pack)
		seal, err := profileversion.Seal(profile)
		if err != nil {
			return LocalProfileResult{State: Failed, Reason: err.Error()}
		}
		canonicalBytes, err := json.Marshal(profile, json.Deterministic(true), jsontext.WithIndent("  "))
		if err != nil {
			return LocalProfileResult{State: Failed, Reason: "profile could not be canonicalized"}
		}
		return LocalProfileResult{
			State:      Completed,
			Document:   string(canonicalBytes) + "\n",
			Profile:    &profile,
			Resolution: &resolution,
			Seal:       &seal,
		}
	})
}

// ValidateProfile checks a local profile document strictly through Go and resolves it against a pack.
func (a *App) ValidateProfile(request ProfileValidateRequest) LocalProfileResult {
	return run(a, false, false, func(context.Context) LocalProfileResult {
		root, _ := resolveFolder(request.Workspace)
		profile, err := localprofile.Decode([]byte(request.Document))
		if err != nil {
			return LocalProfileResult{State: Failed, Reason: err.Error()}
		}
		pack := a.findOrReadPack(root, request.Pack, profile.Base.Pack)
		resolution := localprofile.Resolve(profile, pack)
		seal, err := profileversion.Seal(profile)
		if err != nil {
			return LocalProfileResult{State: Failed, Reason: err.Error()}
		}
		canonicalBytes, err := json.Marshal(profile, json.Deterministic(true), jsontext.WithIndent("  "))
		if err != nil {
			return LocalProfileResult{State: Failed, Reason: "profile could not be canonicalized"}
		}
		return LocalProfileResult{
			State:      Completed,
			Document:   string(canonicalBytes) + "\n",
			Profile:    &profile,
			Resolution: &resolution,
			Seal:       &seal,
		}
	})
}

// SaveProfile saves a canonical profile revision and optionally its seal. It never mutates
// an existing approved profile.
func (a *App) SaveProfile(request ProfileSaveRequest) LocalProfileResult {
	return run(a, false, true, func(context.Context) LocalProfileResult {
		root, declined := resolveFolder(request.Workspace)
		if root == "" {
			return LocalProfileResult{State: declined.state, Reason: declined.reason}
		}
		profile, err := localprofile.Decode([]byte(request.Document))
		if err != nil {
			return LocalProfileResult{State: Failed, Reason: err.Error()}
		}
		seal, err := profileversion.Seal(profile)
		if err != nil {
			return LocalProfileResult{State: Failed, Reason: err.Error()}
		}
		canonicalBytes, err := json.Marshal(profile, json.Deterministic(true), jsontext.WithIndent("  "))
		if err != nil {
			return LocalProfileResult{State: Failed, Reason: "profile could not be canonicalized"}
		}
		canonicalDoc := append(canonicalBytes, '\n')

		// Immutability check: never mutate an approved profile or overwrite existing profile.
		if err := a.checkProfileImmutability(root, profile, request.Output); err != nil {
			return LocalProfileResult{State: Failed, Reason: err.Error()}
		}

		if err := writeWorkspaceEntry(root, request.Output, canonicalDoc); err != nil {
			if errors.Is(err, fs.ErrPermission) {
				return LocalProfileResult{State: PermissionDenied, Reason: "this account cannot write into the open workspace"}
			}
			return LocalProfileResult{State: Failed, Reason: err.Error()}
		}

		if request.SealOutput != "" {
			sealBytes, err := seal.Encode()
			if err != nil {
				return LocalProfileResult{State: Failed, Reason: "cannot encode profile version seal"}
			}
			if err := writeWorkspaceEntry(root, request.SealOutput, sealBytes); err != nil {
				if errors.Is(err, fs.ErrPermission) {
					return LocalProfileResult{State: PermissionDenied, Reason: "this account cannot write profile version seal into the open workspace"}
				}
				return LocalProfileResult{State: Failed, Reason: err.Error()}
			}
		}

		resolution := localprofile.Resolve(profile, a.findOrReadPack(root, "", profile.Base.Pack))
		return LocalProfileResult{
			State:      Completed,
			Document:   string(canonicalDoc),
			Output:     request.Output,
			SealOutput: request.SealOutput,
			Profile:    &profile,
			Resolution: &resolution,
			Seal:       &seal,
		}
	})
}

// CompareProfiles compares two profile versions and optionally assesses impacted tests.
func (a *App) CompareProfiles(request ProfileCompareRequest) ProfileCompareResult {
	return run(a, false, false, func(context.Context) ProfileCompareResult {
		root, _ := resolveFolder(request.Workspace)
		fromProfile, err := a.resolveProfileDoc(root, request.From)
		if err != nil {
			return ProfileCompareResult{State: Failed, Reason: "from profile: " + err.Error()}
		}
		toProfile, err := a.resolveProfileDoc(root, request.To)
		if err != nil {
			return ProfileCompareResult{State: Failed, Reason: "to profile: " + err.Error()}
		}
		comparison, err := profileversion.Compare(fromProfile, toProfile)
		if err != nil {
			return ProfileCompareResult{State: Failed, Reason: err.Error()}
		}
		result := ProfileCompareResult{State: Completed, Comparison: &comparison}
		if request.References != "" {
			refData, err := a.resolveReferencesData(root, request.References)
			if err != nil {
				return ProfileCompareResult{State: Failed, Reason: "references: " + err.Error()}
			}
			refs, err := profileversion.DecodeReferences(refData)
			if err != nil {
				return ProfileCompareResult{State: Failed, Reason: err.Error()}
			}
			assessment, err := profileversion.Assess(comparison, refs)
			if err != nil {
				return ProfileCompareResult{State: Failed, Reason: err.Error()}
			}
			result.Assessment = &ProfileAssessment{
				Comparison: assessment.Comparison,
				Tests:      assessment.Tests,
			}
		}
		return result
	})
}

// UpgradeProfilePin upgrades one saved test pin to a new profile version.
func (a *App) UpgradeProfilePin(request ProfileUpgradePinRequest) ProfileUpgradePinResult {
	return run(a, false, true, func(context.Context) ProfileUpgradePinResult {
		root, declined := resolveFolder(request.Workspace)
		if root == "" {
			return ProfileUpgradePinResult{State: declined.state, Reason: declined.reason}
		}
		refData, declined := workspaceDocument(root, request.References, profileversion.MaxReferencesBytes, "the references document")
		if refData == nil {
			return ProfileUpgradePinResult{State: declined.state, Reason: declined.reason}
		}
		refs, err := profileversion.DecodeReferences(refData)
		if err != nil {
			return ProfileUpgradePinResult{State: Failed, Reason: err.Error()}
		}
		upgraded, err := refs.Upgrade(request.Test, request.WasPin, request.NowPin)
		if err != nil {
			return ProfileUpgradePinResult{State: Failed, Reason: err.Error()}
		}
		encoded, err := upgraded.Encode()
		if err != nil {
			return ProfileUpgradePinResult{State: Failed, Reason: "cannot encode upgraded references"}
		}
		if err := writeWorkspaceEntry(root, request.Output, encoded, true); err != nil {
			if errors.Is(err, fs.ErrPermission) {
				return ProfileUpgradePinResult{State: PermissionDenied, Reason: "this account cannot write into the open workspace"}
			}
			return ProfileUpgradePinResult{State: Failed, Reason: err.Error()}
		}
		return ProfileUpgradePinResult{State: Completed, Output: request.Output, References: &upgraded}
	})
}

// ExportProfilePackage packages an existing profile, pack, version and origin.
func (a *App) ExportProfilePackage(request ProfilePackageExportRequest) ProfilePackageResult {
	return run(a, false, true, func(context.Context) ProfilePackageResult {
		if !request.Reviewed {
			return ProfilePackageResult{
				State:  Failed,
				Reason: "profile export requires confirming metadata and notices were reviewed for disclosure",
			}
		}
		root, declined := resolveFolder(request.Workspace)
		if root == "" {
			return ProfilePackageResult{State: declined.state, Reason: declined.reason}
		}
		p, declined := workspaceDocument(root, request.Profile, localprofile.MaxProfileBytes, "the local profile")
		if p == nil {
			return ProfilePackageResult{State: declined.state, Reason: declined.reason}
		}
		b, declined := workspaceDocument(root, request.Pack, profilepack.MaxPackBytes, "the profile pack")
		if b == nil {
			return ProfilePackageResult{State: declined.state, Reason: declined.reason}
		}
		v, declined := workspaceDocument(root, request.Version, profileversion.MaxVersionBytes, "the profile version seal")
		if v == nil {
			return ProfilePackageResult{State: declined.state, Reason: declined.reason}
		}
		o, declined := workspaceDocument(root, request.Origin, profilepackage.MaxOriginBytes, "the profile origin")
		if o == nil {
			return ProfilePackageResult{State: declined.state, Reason: declined.reason}
		}
		pkgBytes, err := profilepackage.Export(p, b, v, o)
		if err != nil {
			return ProfilePackageResult{State: Failed, Reason: err.Error()}
		}
		if err := writeWorkspaceEntry(root, request.Output, pkgBytes); err != nil {
			if errors.Is(err, fs.ErrPermission) {
				return ProfilePackageResult{State: PermissionDenied, Reason: "this account cannot write into the open workspace"}
			}
			return ProfilePackageResult{State: Failed, Reason: err.Error()}
		}
		decodedPkg, err := profilepackage.Decode(pkgBytes)
		if err != nil {
			return ProfilePackageResult{State: Failed, Reason: err.Error()}
		}
		docs, err := decodedPkg.Documents()
		if err != nil {
			return ProfilePackageResult{State: Failed, Reason: err.Error()}
		}
		origin, _ := profilepackage.DecodeOrigin(docs["origin.json"])
		prof, _ := localprofile.Decode(docs["profile.json"])
		pack, _ := profilepack.Decode(docs["pack.json"])
		ver, _ := profileversion.DecodeVersion(docs["version.json"])
		sha := sha256.Sum256(pkgBytes)
		packId := pack.Identity
		verId := ver.Profile
		return ProfilePackageResult{
			State:      Completed,
			Output:     request.Output,
			Origin:     &origin,
			Profile:    &prof.Identity,
			Pack:       &packId,
			Version:    &verId,
			SHA256:     hex.EncodeToString(sha[:]),
			Rights:     origin.ReviewReference,
			Dependency: packId.ID + " " + packId.Version,
		}
	})
}

// ImportProfilePackage unpacks a verified package into a new private directory.
func (a *App) ImportProfilePackage(request ProfilePackageImportRequest) ProfilePackageResult {
	return run(a, true, true, func(ctx context.Context) ProfilePackageResult {
		root, declined := resolveFolder(request.Workspace)
		if root == "" {
			return ProfilePackageResult{State: declined.state, Reason: declined.reason}
		}
		data, declined := workspaceDocument(root, request.Package, profilepackage.MaxBytes, "the profile package")
		if data == nil {
			return ProfilePackageResult{State: declined.state, Reason: declined.reason}
		}
		if err := artifactpath.EntryName(request.Output); err != nil {
			return ProfilePackageResult{State: Failed, Reason: "output directory must be one regular entry of the open workspace"}
		}
		destination := filepath.Join(root, request.Output)
		if err := profilepackage.Import(ctx, destination, data); err != nil {
			if errors.Is(err, fs.ErrPermission) {
				return ProfilePackageResult{State: PermissionDenied, Reason: "this account cannot write into the open workspace"}
			}
			return ProfilePackageResult{State: Failed, Reason: err.Error()}
		}
		return ProfilePackageResult{State: Completed, Output: request.Output}
	})
}

// InspectProfilePackage inspects a profile package without unpacking it to disk.
func (a *App) InspectProfilePackage(workspace, entry string) ProfilePackageResult {
	return run(a, false, false, func(context.Context) ProfilePackageResult {
		root, declined := resolveFolder(workspace)
		if root == "" {
			return ProfilePackageResult{State: declined.state, Reason: declined.reason}
		}
		data, declined := workspaceDocument(root, entry, profilepackage.MaxBytes, "the profile package")
		if data == nil {
			return ProfilePackageResult{State: declined.state, Reason: declined.reason}
		}
		decodedPkg, err := profilepackage.Decode(data)
		if err != nil {
			return ProfilePackageResult{State: Failed, Reason: err.Error()}
		}
		docs, err := decodedPkg.Documents()
		if err != nil {
			return ProfilePackageResult{State: Failed, Reason: err.Error()}
		}
		origin, err := profilepackage.DecodeOrigin(docs["origin.json"])
		if err != nil {
			return ProfilePackageResult{State: Failed, Reason: err.Error()}
		}
		prof, err := localprofile.Decode(docs["profile.json"])
		if err != nil {
			return ProfilePackageResult{State: Failed, Reason: err.Error()}
		}
		pack, err := profilepack.Decode(docs["pack.json"])
		if err != nil {
			return ProfilePackageResult{State: Failed, Reason: err.Error()}
		}
		ver, err := profileversion.DecodeVersion(docs["version.json"])
		if err != nil {
			return ProfilePackageResult{State: Failed, Reason: err.Error()}
		}
		sha := sha256.Sum256(data)
		packId := pack.Identity
		verId := ver.Profile
		conflict := ""
		suggestedDir := filepath.Join(root, "imported-"+prof.Identity.ID)
		if _, err := os.Lstat(suggestedDir); err == nil {
			conflict = "default import directory 'imported-" + prof.Identity.ID + "' already exists"
		}
		return ProfilePackageResult{
			State:      Completed,
			Output:     entry,
			Origin:     &origin,
			Profile:    &prof.Identity,
			Pack:       &packId,
			Version:    &verId,
			SHA256:     hex.EncodeToString(sha[:]),
			Rights:     origin.ReviewReference,
			Dependency: packId.ID + " " + packId.Version,
			Conflict:   conflict,
		}
	})
}

// Helper methods

func (a *App) findOrReadPack(root, packEntry string, pinned profilepack.Identity) profilepack.Pack {
	if root != "" && packEntry != "" {
		if data, declined := workspaceDocument(root, packEntry, profilepack.MaxPackBytes, "the profile pack"); data != nil && declined.state == "" {
			if pack, err := profilepack.Decode(data); err == nil {
				return pack
			}
		}
	}
	if root != "" && pinned.ID != "" {
		entries, err := os.ReadDir(root)
		if err == nil {
			for _, entry := range entries {
				if !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), ".json") {
					continue
				}
				if data, declined := workspaceDocument(root, entry.Name(), profilepack.MaxPackBytes, "the profile pack"); data != nil && declined.state == "" {
					if pack, err := profilepack.Decode(data); err == nil {
						if pack.Satisfies(pinned) == nil {
							return pack
						}
					}
				}
			}
		}
	}
	return profilepack.Pack{}
}

func (a *App) checkProfileImmutability(root string, profile localprofile.Profile, outputName string) error {
	if err := artifactpath.EntryName(outputName); err != nil {
		return errors.New("a profile is written to one regular entry of the open workspace")
	}
	destPath := filepath.Join(root, outputName)
	if _, err := os.Lstat(destPath); err == nil {
		return errors.New("cannot overwrite existing profile; approved profiles are immutable, save as a new revision")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, declined := workspaceDocument(root, entry.Name(), profileversion.MaxVersionBytes, "the version seal")
		if data != nil && declined.state == "" {
			if seal, err := profileversion.DecodeVersion(data); err == nil {
				if seal.Profile.ID == profile.Identity.ID && seal.Profile.Version == profile.Identity.Version {
					if err := seal.Verify(profile); err != nil {
						return errors.New("profile version " + profile.Identity.Version + " is already sealed with different content; increment version to save changes")
					}
				}
			}
		}
	}
	return nil
}

func (a *App) resolveProfileDoc(root, fileOrDoc string) (localprofile.Profile, error) {
	if root != "" && artifactpath.EntryName(fileOrDoc) == nil {
		data, declined := workspaceDocument(root, fileOrDoc, localprofile.MaxProfileBytes, "the profile")
		if data != nil && declined.state == "" {
			return localprofile.Decode(data)
		}
	}
	return localprofile.Decode([]byte(fileOrDoc))
}

func (a *App) resolveReferencesData(root, fileOrDoc string) ([]byte, error) {
	if root != "" && artifactpath.EntryName(fileOrDoc) == nil {
		data, declined := workspaceDocument(root, fileOrDoc, profileversion.MaxReferencesBytes, "the references document")
		if data != nil && declined.state == "" {
			return data, nil
		}
	}
	return []byte(fileOrDoc), nil
}

func writeWorkspaceEntry(root, name string, data []byte, allowOverwrite ...bool) error {
	if err := artifactpath.EntryName(name); err != nil {
		return errors.New("destination must be one regular entry of the open workspace")
	}
	dest, err := artifactpath.Destination(filepath.Join(root, name))
	if err != nil {
		return err
	}
	flags := os.O_WRONLY | os.O_CREATE
	overwrite := len(allowOverwrite) > 0 && allowOverwrite[0]
	if !overwrite {
		flags |= os.O_EXCL
	} else {
		flags |= os.O_TRUNC
	}
	file, err := os.OpenFile(dest, flags, 0600)
	if err != nil {
		if os.IsExist(err) {
			return errors.New("cannot create destination; file already exists in workspace")
		}
		if errors.Is(err, fs.ErrPermission) {
			return fs.ErrPermission
		}
		return errors.New("cannot create destination file")
	}
	_, writeErr := file.Write(data)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(dest)
		return errors.New("cannot write destination file")
	}
	return nil
}
