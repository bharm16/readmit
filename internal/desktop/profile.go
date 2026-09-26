package desktop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/bharm16/readmit/internal/artifactdir"
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
	HL7Version string `json:"hl7_version"`
	Family     string `json:"family"`
	profilepack.Outcomes
	Pack profilepack.Identity `json:"pack"`
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
	// LaterPin is present when the assessment finds a test affected: the
	// exact pin that test may be updated to, or why it may not be.
	LaterPin *LaterProfilePin `json:"later_pin,omitzero"`
}

// LaterProfilePin is the exact pin of the later profile one comparison
// compared, sealed over the very document compared, or the reason no saved
// test can be pinned to it. Exactly one of the two is set.
type LaterProfilePin struct {
	Pin     *profileversion.Pin `json:"pin,omitzero"`
	Refusal string              `json:"refusal,omitzero"`
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
// Seal and Provenance are the version seal the package carries and the pinned
// pack's upstream provenance, reported by inspection and import.
type ProfilePackageResult struct {
	State      State                   `json:"state"`
	Reason     string                  `json:"reason,omitzero"`
	Output     string                  `json:"output,omitzero"`
	Origin     *profilepackage.Origin  `json:"origin,omitzero"`
	Pack       *profilepack.Identity   `json:"pack,omitzero"`
	Profile    *localprofile.Identity  `json:"profile,omitzero"`
	Version    *localprofile.Identity  `json:"version,omitzero"`
	Seal       *profileversion.Version `json:"seal,omitzero"`
	Provenance *profilepack.Provenance `json:"provenance,omitzero"`
	SHA256     string                  `json:"sha256,omitzero"`
	Conflict   string                  `json:"conflict,omitzero"`
	Dependency string                  `json:"dependency,omitzero"`
	Rights     string                  `json:"rights,omitzero"`
}

// profileImportOperation names a package import while it holds the slot, so
// the profile panel's cancel control stops exactly the import it started. It
// is local work that reaches no destination.
const profileImportOperation = "profile-import"

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
			Bundleable: pack.Bundleable() == nil,
		}
	})
}

// OpenProfileLibrary opens a directory of profile packs as a library and returns its support matrix.
// The directory is the open workspace itself, or one real folder of it.
func (a *App) OpenProfileLibrary(workspace, directory string) ProfileLibraryResult {
	return run(a, false, false, func(context.Context) ProfileLibraryResult {
		root, declined := resolveFolder(workspace)
		if root == "" {
			return ProfileLibraryResult{State: declined.state, Reason: declined.reason}
		}
		dirPath := root
		if directory != "" && directory != "." {
			folder, err := artifactpath.Child(root, directory)
			if err != nil {
				return ProfileLibraryResult{State: Failed, Reason: "a profile library must be the open workspace or one folder of it, never a symbolic link"}
			}
			dirPath = folder
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
				Outcomes:   r.Outcomes,
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
// and computes its canonical seal. The profile and the pack are each one entry
// of the workspace or one entry of one of its folders, such as the directory a
// package was imported into. The pack is chosen as validating and saving
// choose it (profilePack): a named pack must be one the pack reader accepts,
// and with none named the pinned pack is looked for among the workspace's own
// entries. A pack that is not the pinned one contributes nothing, and the
// resolution says so.
func (a *App) OpenProfile(workspace, entry, packEntry string) LocalProfileResult {
	return run(a, false, false, func(context.Context) LocalProfileResult {
		root, declined := resolveFolder(workspace)
		if root == "" {
			return LocalProfileResult{State: declined.state, Reason: declined.reason}
		}
		data, declined := folderDocument(root, entry, localprofile.MaxProfileBytes, "the local profile")
		if data == nil {
			return LocalProfileResult{State: declined.state, Reason: declined.reason}
		}
		profile, err := localprofile.Decode(data)
		if err != nil {
			return LocalProfileResult{State: Failed, Reason: err.Error()}
		}
		pack, declined := profilePack(root, packEntry, profile.Base.Pack)
		if declined.state != "" {
			return LocalProfileResult{State: declined.state, Reason: declined.reason}
		}
		resolution := localprofile.Resolve(profile, pack)
		seal, err := profileversion.Seal(profile)
		if err != nil {
			return LocalProfileResult{State: Failed, Reason: err.Error()}
		}
		_, document, err := localprofile.Canonical(profile)
		if err != nil {
			return LocalProfileResult{State: Failed, Reason: "profile could not be canonicalized"}
		}
		return LocalProfileResult{
			State:      Completed,
			Document:   string(document),
			Profile:    &profile,
			Resolution: &resolution,
			Seal:       &seal,
		}
	})
}

// ValidateProfile checks a local profile document strictly through Go and resolves it against a pack.
func (a *App) ValidateProfile(request ProfileValidateRequest) LocalProfileResult {
	return run(a, false, false, func(context.Context) LocalProfileResult {
		root, declined := resolveFolder(request.Workspace)
		if root == "" && request.Pack != "" {
			return LocalProfileResult{State: declined.state, Reason: declined.reason}
		}
		profile, err := localprofile.Decode([]byte(request.Document))
		if err != nil {
			return LocalProfileResult{State: Failed, Reason: err.Error()}
		}
		pack, declined := profilePack(root, request.Pack, profile.Base.Pack)
		if declined.state != "" {
			return LocalProfileResult{State: declined.state, Reason: declined.reason}
		}
		resolution := localprofile.Resolve(profile, pack)
		seal, err := profileversion.Seal(profile)
		if err != nil {
			return LocalProfileResult{State: Failed, Reason: err.Error()}
		}
		_, document, err := localprofile.Canonical(profile)
		if err != nil {
			return LocalProfileResult{State: Failed, Reason: "profile could not be canonicalized"}
		}
		return LocalProfileResult{
			State:      Completed,
			Document:   string(document),
			Profile:    &profile,
			Resolution: &resolution,
			Seal:       &seal,
		}
	})
}

// SaveProfile saves a canonical profile revision and optionally its seal. It never mutates
// an existing approved profile. Saving names no pack, so the profile is resolved
// against the pack among the workspace's own entries that satisfies its pin, as
// opening and validating resolve it with none named.
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
		_, canonicalDoc, err := localprofile.Canonical(profile)
		if err != nil {
			return LocalProfileResult{State: Failed, Reason: "profile could not be canonicalized"}
		}

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

		resolution := localprofile.Resolve(profile, profilelibrary.FindPinned(root, profile.Base.Pack))
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
		fromProfile, _, err := a.resolveProfileDoc(root, request.From)
		if err != nil {
			return ProfileCompareResult{State: Failed, Reason: "from profile: " + err.Error()}
		}
		toProfile, toFile, err := a.resolveProfileDoc(root, request.To)
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
			if len(assessment.Affected()) > 0 {
				result.LaterPin = laterProfilePin(toProfile, toFile)
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
// Exporting and importing a package copy reviewed documents without
// activating anything, and like `readmit profile export` and `import` they
// need no license term.
func (a *App) ExportProfilePackage(request ProfilePackageExportRequest) ProfilePackageResult {
	return run(a, false, false, func(context.Context) ProfilePackageResult {
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

// ImportProfilePackage unpacks a verified package into a new private directory
// through profilepackage.Import, the import `readmit profile import` performs,
// so both refuse the same packages in the same words. A completed import
// reports what the package carried. Nothing is activated: no project, saved
// test pin or open editor changes, and no message is evaluated. The import can
// be cancelled from the profile panel; a cancellation after the directory was
// created says the directory holds an incomplete import.
func (a *App) ImportProfilePackage(request ProfilePackageImportRequest) ProfilePackageResult {
	return runNamed[ProfilePackageResult, *ProfilePackageResult](a, profiles["ImportProfilePackage"], func(ctx context.Context) ProfilePackageResult {
		return importProfilePackage(ctx, request)
	})
}

func importProfilePackage(ctx context.Context, request ProfilePackageImportRequest) ProfilePackageResult {
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
	_, occupied := os.Lstat(destination)
	if err := profilepackage.Import(ctx, destination, data); err != nil {
		if errors.Is(err, context.Canceled) {
			// A cancellation can land before Import finds the name taken, so
			// only a directory that was not there before is its own.
			if _, statErr := os.Lstat(destination); statErr == nil && occupied != nil {
				return ProfilePackageResult{State: Cancelled, Reason: "the import was cancelled after " + request.Output + " was created; it holds an incomplete import with no package.json, so review and remove it and import again into a new directory"}
			}
			return ProfilePackageResult{State: Cancelled, Reason: "the import was cancelled before anything was written"}
		}
		if errors.Is(err, fs.ErrPermission) {
			return ProfilePackageResult{State: PermissionDenied, Reason: "this account cannot write into the open workspace"}
		}
		return ProfilePackageResult{State: Failed, Reason: err.Error()}
	}
	result, err := describePackage(data)
	if err != nil {
		return ProfilePackageResult{State: Failed, Reason: err.Error()}
	}
	result.Output = request.Output
	return result
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
		result, err := describePackage(data)
		if err != nil {
			return ProfilePackageResult{State: Failed, Reason: err.Error()}
		}
		result.Output = entry
		suggestedDir := filepath.Join(root, "imported-"+result.Profile.ID)
		if _, err := os.Lstat(suggestedDir); err == nil {
			result.Conflict = "default import directory 'imported-" + result.Profile.ID + "' already exists"
		}
		return result
	})
}

// Helper methods

// describePackage reports what one package carries once profilepackage.Decode
// verified it: the profile, the pack it pins with that pack's provenance, the
// version seal, the local origin, and the SHA-256 of the package bytes read.
// Inspection and import share it, so what the window shows before an import is
// what it shows after one.
func describePackage(data []byte) (ProfilePackageResult, error) {
	decodedPkg, err := profilepackage.Decode(data)
	if err != nil {
		return ProfilePackageResult{}, err
	}
	docs, err := decodedPkg.Documents()
	if err != nil {
		return ProfilePackageResult{}, err
	}
	origin, err := profilepackage.DecodeOrigin(docs["origin.json"])
	if err != nil {
		return ProfilePackageResult{}, err
	}
	prof, err := localprofile.Decode(docs["profile.json"])
	if err != nil {
		return ProfilePackageResult{}, err
	}
	pack, err := profilepack.Decode(docs["pack.json"])
	if err != nil {
		return ProfilePackageResult{}, err
	}
	ver, err := profileversion.DecodeVersion(docs["version.json"])
	if err != nil {
		return ProfilePackageResult{}, err
	}
	sha := sha256.Sum256(data)
	packId := pack.Identity
	verId := ver.Profile
	provenance := pack.Provenance
	return ProfilePackageResult{
		State:      Completed,
		Origin:     &origin,
		Profile:    &prof.Identity,
		Pack:       &packId,
		Version:    &verId,
		Seal:       &ver,
		Provenance: &provenance,
		SHA256:     hex.EncodeToString(sha[:]),
		Rights:     origin.ReviewReference,
		Dependency: packId.ID + " " + packId.Version,
	}, nil
}

// folderDocument reads one document named by an entry of the open workspace,
// or by one entry of one of its folders as `folder/entry`, the shape of a
// document an import wrote into its new directory. The folder must be a real
// folder, never a symbolic link, so a name cannot leave the workspace, and the
// entry is read as workspaceDocument reads one.
func folderDocument(root, name string, limit int, what string) ([]byte, refusal) {
	folder, entry, nested := strings.Cut(name, "/")
	if !nested {
		folder, entry = root, name
	} else {
		path, err := artifactpath.Child(root, folder)
		if err != nil {
			return nil, refusal{Failed, what + " must be one entry of the open workspace or of one of its folders"}
		}
		folder = path
	}
	return workspaceDocument(folder, entry, limit, what)
}

// profilePack is the pack a profile is resolved against, chosen by one rule
// for opening, validating and saving, so the three never choose differently
// for the same profile: the pack named, read as folderDocument reads one and
// refused in the pack reader's words; or, with none named, the pack among the
// open workspace's own entries that satisfies the profile's pin, as
// profilelibrary.FindPinned finds it, which is nothing when there is no
// workspace. A pack beside a profile in one of the workspace's folders answers
// when it is named.
func profilePack(root, named string, pin profilepack.Identity) (profilepack.Pack, refusal) {
	if named == "" {
		return profilelibrary.FindPinned(root, pin), refusal{}
	}
	data, declined := folderDocument(root, named, profilepack.MaxPackBytes, "the profile pack")
	if data == nil {
		return profilepack.Pack{}, declined
	}
	pack, err := profilepack.Decode(data)
	if err != nil {
		return profilepack.Pack{}, refusal{Failed, err.Error()}
	}
	return pack, refusal{}
}

// checkProfileImmutability refuses a destination that is not one new entry of
// the open workspace, then holds the profile to every version seal the
// workspace holds, through profileversion.VerifyFolder, which refuses a
// workspace it cannot list.
func (a *App) checkProfileImmutability(root string, profile localprofile.Profile, outputName string) error {
	if err := artifactpath.EntryName(outputName); err != nil {
		return errors.New("a profile is written to one regular entry of the open workspace")
	}
	destPath := filepath.Join(root, outputName)
	if _, err := os.Lstat(destPath); err == nil {
		return errors.New("cannot overwrite existing profile; approved profiles are immutable, save as a new revision")
	}
	return profileversion.VerifyFolder(root, profile)
}

// resolveProfileDoc reads a profile from a workspace file, or else as the
// document itself, and says whether it was a file.
func (a *App) resolveProfileDoc(root, fileOrDoc string) (localprofile.Profile, bool, error) {
	if root != "" && artifactpath.EntryName(fileOrDoc) == nil {
		data, declined := workspaceDocument(root, fileOrDoc, localprofile.MaxProfileBytes, "the profile")
		if data != nil && declined.state == "" {
			profile, err := localprofile.Decode(data)
			return profile, true, err
		}
	}
	profile, err := localprofile.Decode([]byte(fileOrDoc))
	return profile, false, err
}

// laterProfilePin decides the pin an affected test may move to: the seal over
// the later profile exactly as compared, so it can name no other profile or
// version than the comparison's. A later profile that is no file of the
// workspace is refused, since a pin must name a document a test can be held to.
func laterProfilePin(later localprofile.Profile, fromFile bool) *LaterProfilePin {
	if !fromFile {
		return &LaterProfilePin{Refusal: "the later profile is not a file of the open workspace, so no saved test can be pinned to it"}
	}
	seal, err := profileversion.Seal(later)
	if err != nil {
		return &LaterProfilePin{Refusal: "the later profile's version seal could not be made: " + err.Error()}
	}
	pin := seal.Pin()
	return &LaterProfilePin{Pin: &pin}
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

// writeWorkspaceEntry writes data as the workspace entry name. The entry must
// be new unless allowOverwrite is set; an overwrite replaces the entry through
// replaceDocument and never opens what is there, so a symbolic link, a hard
// link or a FIFO planted at the name is replaced rather than written through.
func writeWorkspaceEntry(root, name string, data []byte, allowOverwrite ...bool) error {
	if err := artifactpath.EntryName(name); err != nil {
		return errors.New("destination must be one regular entry of the open workspace")
	}
	dest, err := artifactpath.Destination(filepath.Join(root, name))
	if err != nil {
		return err
	}
	if len(allowOverwrite) > 0 && allowOverwrite[0] {
		err = replaceDocument(dest, data)
		switch {
		case err == nil:
			return nil
		case errors.Is(err, fs.ErrPermission):
			return fs.ErrPermission
		case errors.Is(err, fs.ErrExist):
			return errors.New("cannot replace destination file; an interrupted write is retained beside it")
		}
		return errors.New("cannot write destination file")
	}
	err = newWorkspaceEntry.Create(dest, data)
	switch {
	case errors.Is(err, errEntryUncreated) && errors.Is(err, fs.ErrExist):
		return errors.New("cannot create destination; file already exists in workspace")
	case errors.Is(err, errEntryUncreated) && errors.Is(err, fs.ErrPermission):
		return fs.ErrPermission
	}
	return err
}

// errEntryUncreated is a new workspace entry that could not be created.
var errEntryUncreated = errors.New("cannot create destination file")

// newWorkspaceEntry is how a save that never overwrites creates its entry,
// through the shared document store.
var newWorkspaceEntry = artifactdir.Document{
	Errors: artifactdir.DocumentErrors{
		Create: errEntryUncreated,
		Write:  errors.New("cannot write destination file"),
	},
}
