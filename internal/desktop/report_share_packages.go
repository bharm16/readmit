package desktop

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/engine"
	"github.com/bharm16/readmit/internal/protect"
)

// Encrypted packages (#560) are a named list of the project's transfer
// packages, read from their descriptors without a key. Decrypt and Delete
// package are reviewed actions: Decrypt verifies and writes a fresh plaintext
// copy to a place chosen through the save dialog, opening or running nothing
// it holds; Delete unlinks the files the package declares, obeys its declared
// retention unless an explicit override with a reason is given, and states
// that it is not secure erasure. Neither reaches a key or anything remote.

const (
	// DecryptPackageAction writes a decrypted copy of one package.
	DecryptPackageAction ActionID = "package.decrypt"
	// DeletePackageAction unlinks the files one package declares.
	DeletePackageAction ActionID = "package.delete"
)

func init() {
	actionPolicies[DecryptPackageAction] = actionPolicy{consent: ExportConsent, review: slot{}, perform: slot{profile: "OpenProtectedPackage"},
		bind: bindPackageAction, execute: executePackageAction}
	actionPolicies[DeletePackageAction] = actionPolicy{consent: DeleteConsent, review: slot{}, perform: slot{writes: true},
		bind: bindPackageAction, execute: executePackageAction}
}

// EncryptedPackage is one transfer package of the project as its list shows
// it. Problem says why its descriptor cannot be read.
type EncryptedPackage struct {
	Entry       string `json:"entry"`
	CreatedAt   string `json:"created_at,omitzero"`
	Retention   string `json:"retention,omitzero"`
	RetainUntil string `json:"retain_until,omitzero"`
	Control     string `json:"control,omitzero"`
	Generation  int    `json:"generation,omitzero"`
	Entries     int    `json:"entries,omitzero"`
	Problem     string `json:"problem,omitzero"`
}

// EncryptedPackagesResult lists the project's transfer packages, newest
// first.
type EncryptedPackagesResult struct {
	State    State              `json:"state"`
	Reason   string             `json:"reason,omitzero"`
	Context  RequestContext     `json:"context"`
	Packages []EncryptedPackage `json:"packages"`
}

func (r *EncryptedPackagesResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// ListEncryptedPackages lists the project's transfer packages from their
// descriptors alone. It reads no key and writes nothing.
func (a *App) ListEncryptedPackages(request RequestContext) EncryptedPackagesResult {
	return runRead(a, false, func(ctx context.Context) EncryptedPackagesResult {
		result := EncryptedPackagesResult{Context: request, Packages: []EncryptedPackage{}}
		root, declined := a.projectRoot(ctx, request)
		if root == "" {
			result.refuse(declined.state, declined.reason)
			return result
		}
		names, err := projectEntries(root, MaxWorkspaceEntries, func(name string) bool {
			info, err := os.Lstat(filepath.Join(root, name))
			if err != nil || !info.IsDir() {
				return false
			}
			kind, ok := classify(root, name, true)
			return ok && kind == TransferPackageArtifact
		})
		if err != nil {
			result.refuse(Failed, err.Error())
			return result
		}
		now := a.now()
		for _, name := range names {
			result.Packages = append(result.Packages, packageRow(root, name, now))
		}
		slices.SortStableFunc(result.Packages, func(x, y EncryptedPackage) int { return strings.Compare(y.CreatedAt, x.CreatedAt) })
		result.State = Completed
		if len(result.Packages) == 0 {
			result.State = Empty
		}
		return result
	})
}

func packageRow(root, entry string, now time.Time) EncryptedPackage {
	row := EncryptedPackage{Entry: entry}
	descriptor, _, err := protect.ReadPackage(filepath.Join(root, entry))
	switch {
	case errors.Is(err, engine.ErrUnsupportedVersion):
		row.Problem = "written by a version this release cannot read"
		return row
	case err != nil:
		row.Problem = "its descriptor cannot be read without a key"
		return row
	}
	row.CreatedAt, row.Retention = descriptor.CreatedAt.UTC().Format(time.RFC3339), string(descriptor.Retention(now))
	if !descriptor.RetainUntil.IsZero() {
		row.RetainUntil = descriptor.RetainUntil.UTC().Format(time.RFC3339)
	}
	row.Control, row.Generation, row.Entries = descriptor.Control, descriptor.Generation, len(descriptor.Entries)
	return row
}

// PackageActionOptions name the package a Decrypt or Delete acts on: its
// entry, and for a Decrypt the place chosen for the copy; for a Delete,
// whether its declared retention is overridden.
type PackageActionOptions struct {
	Package     string `json:"package"`
	Destination string `json:"destination,omitzero"`
	Override    bool   `json:"override,omitzero"`
}

// PackageActionReview is what a Decrypt or Delete will do.
type PackageActionReview struct {
	Package     EncryptedPackage     `json:"package"`
	Destination ShareDestinationView `json:"destination"`
	Files       int                  `json:"files"`
	Override    bool                 `json:"override"`
	Limitations []string             `json:"limitations"`
}

// PackageActionOutcome is what a Decrypt or Delete did.
type PackageActionOutcome struct {
	Name    string `json:"name,omitzero"`
	Output  string `json:"output,omitzero"`
	Removed int    `json:"removed,omitzero"`
}

type packageBinding struct {
	root, path, destination string
	entry                   string
	control                 protect.File
	override                bool
}

func bindPackageAction(a *App, ctx context.Context, request PrepareActionRequest, _ bool) (*boundAction, refusal) {
	if request.Package == nil || request.Package.Package == "" {
		return nil, refusal{Failed, "a package action names one package"}
	}
	options := *request.Package
	root, declined := a.projectRoot(ctx, request.Context)
	if root == "" {
		return nil, declined
	}
	path, err := runEntryPath(root, options.Package)
	if err != nil {
		return nil, refusal{Failed, "the package must be one entry of the project"}
	}
	row := packageRow(root, options.Package, a.now())
	if row.Problem != "" {
		return nil, refusal{Failed, "this package " + row.Problem}
	}
	descriptor, _, _ := protect.ReadPackage(path)
	display := &PackageActionReview{Package: row, Files: len(descriptor.Entries) + 2, Override: options.Override, Limitations: []string{}}
	bound := &packageBinding{root: root, path: path, entry: options.Package, override: options.Override}
	parts := []string{string(request.Action), root, a.reviewer(), options.Package, fileDigest(filepath.Join(path, protect.DescriptorName)), row.Retention}
	ready, reason := true, ""
	if request.Action == DecryptPackageAction {
		control, declined := packageControl(root, descriptor.Control)
		if declined.state != "" {
			ready, reason = false, declined.reason
		}
		bound.control = control
		place, known := a.shareDestination(options.Destination)
		destination := place.path
		switch {
		case options.Destination == "" || !known || !place.folder:
			if ready {
				ready, reason = false, "choose where the decrypted copy is written"
			}
		default:
			display.Destination = ShareDestinationView{Kind: "local", Name: filepath.Base(destination), Location: filepath.Base(filepath.Dir(destination))}
			if _, err := os.Lstat(destination); !errors.Is(err, fs.ErrNotExist) && ready {
				ready, reason = false, "something is already there; choose a new name"
			}
			bound.destination = destination
			parts = append(parts, destination, fileDigest(control.Path))
		}
	} else {
		display.Limitations = []string{protect.DeletionLimitations}
		parts = append(parts, map[bool]string{true: "override", false: "declared"}[options.Override])
		if row.Retention == string(protect.WithinRetention) && !options.Override {
			ready, reason = false, "the package is retained until "+row.RetainUntil+"; override its retention to delete it"
		}
	}
	review := ActionReview{Items: []CatalogItem{}, Ready: ready, Refusal: reason, PackageAction: display, Destination: ReviewDestination{Output: display.Destination.Name}}
	if request.Action == DeletePackageAction && options.Override {
		review.Requirements = []ReviewRequirement{RationaleRequirement}
	}
	return &boundAction{action: request.Action, origin: request, packageAction: bound, binding: binding(parts...), review: review}, noRefusal
}

// packageControl is the protection document of the project that registers
// the control a package was written under.
func packageControl(root, name string) (protect.File, refusal) {
	for _, control := range listedControls(root) {
		if control.Control.Name == name {
			_, file, declined := protectionEntryFile(root, control.Entry)
			return file, declined
		}
	}
	return protect.File{}, refusal{Failed, "no encryption control of this project wrote this package"}
}

// listedControls are the controls every protection document of the project
// registers, retired ones included: a retired control still opens what it
// wrote.
func listedControls(root string) []ListedProtectionControl {
	listed := []ListedProtectionControl{}
	entries, err := os.ReadDir(root)
	if err != nil {
		return listed
	}
	for _, entry := range entries {
		name := entry.Name()
		if !entry.Type().IsRegular() || artifactpath.EntryName(name) != nil {
			continue
		}
		if kind, ok := classify(root, name, false); !ok || kind != ProtectionArtifact {
			continue
		}
		_, file, declined := protectionEntryFile(root, name)
		if declined.state != "" {
			continue
		}
		if document, err := file.Read(); err == nil {
			for _, control := range protectionView(name, document).Controls {
				listed = append(listed, ListedProtectionControl{Entry: name, Control: control})
			}
		}
	}
	return listed
}

func executePackageAction(a *App, ctx context.Context, bound *boundAction, decisions ReviewDecisions) ReviewedActionResult {
	result := ReviewedActionResult{Outcome: ActionRefused}
	pkg := bound.packageAction
	if bound.action == DecryptPackageAction {
		destination, err := artifactpath.Destination(pkg.destination)
		if err != nil {
			result.refuse(Failed, "the decrypted copy must be written outside retained evidence")
			return result
		}
		if _, _, err := pkg.control.Open(ctx, "", pkg.path, destination); err != nil {
			state, reason := openRefusal(err)
			result.refuse(state, reason)
			return result
		}
		result.State, result.Outcome = Completed, ActionCompleted
		result.PackageAction = &PackageActionOutcome{Name: filepath.Base(destination), Output: a.rememberShareOutput(destination)}
		return result
	}
	if pkg.override && strings.TrimSpace(decisions.Rationale) == "" {
		result.refuse(Failed, "say why the declared retention is overridden; nothing was removed")
		return result
	}
	_, removed, err := protect.Discard(pkg.path, a.now(), pkg.override)
	if err != nil {
		result.refuse(Failed, err.Error())
		if removed > 0 {
			result.PackageAction = &PackageActionOutcome{Removed: removed}
		}
		return result
	}
	result.State, result.Outcome = Completed, ActionCompleted
	result.PackageAction = &PackageActionOutcome{Name: pkg.entry, Removed: removed}
	return result
}
