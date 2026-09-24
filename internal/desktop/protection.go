package desktop

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/engine"
	"github.com/bharm16/readmit/internal/protect"
)

// The protection screen. This file connects the protected-package journey to
// the operations the command line runs — `protect show`, `register`, `rotate`,
// `retire`, `pack`, `inspect`, `open` and `discard` — and adds no engine of its
// own: a control is a reference to a key readmit never holds, a package is the
// existing encrypted-transfer operation's output, and every gate is the
// operation's own.
//
// The rule every method here keeps is ADR-0006's: key material exists only
// inside the one operation that reads it, as a value that masks itself and
// refuses to be serialized. Nothing a method returns can carry a key — the
// views render the one mask the secret package defines, the locator arguments
// are counted rather than echoed, and a rotation is recorded only after the
// declared store answers for the control. Recipient and authority facts are the
// package's own: the control it was written under, the generation that was
// current, and the retention period it declares, shown beside what encryption
// does not establish.

// protectOperation is the operation name the protection panel cancels through.
const protectOperation = "protect"

// protectionLimitations is what an encrypted transfer package establishes, as
// the protect package states it to every command that touches one.
var protectionLimitations = []string{
	protect.Limitations,
	"Keys resolve only through the customer-managed references a control registers. readmit holds no key, cannot rotate or destroy one, and a lost key is a package that cannot be opened.",
	"Retirement is not revocation and deletion is not erasure: a recipient who already has a package keeps it, and no removal here reaches a snapshot, backup, replica or another machine.",
}

// ProtectionControl is one registered control as the window may show it. Key
// is always the mask — the key itself was never read to produce this view —
// and the locator is shown the way `protect show` shows it: the program's
// absolute path, and a count instead of the arguments, because an argument is
// the one place an operator could have put key material.
type ProtectionControl struct {
	Name             string `json:"name"`
	Storage          string `json:"storage"`
	State            string `json:"state"`
	Generation       int    `json:"generation"`
	RotatedAt        string `json:"rotated_at"`
	Rotation         string `json:"rotation"`
	MaxAge           string `json:"max_age,omitzero"`
	Retain           string `json:"retain,omitzero"`
	Command          string `json:"command"`
	LocatorArguments int    `json:"locator_arguments"`
	Key              string `json:"key"`
}

// ProtectionDocument is one protection document of the open workspace, as
// `protect show` reports it: every registered control, masked, with the
// document's own limits beside it.
type ProtectionDocument struct {
	Entry       string              `json:"entry"`
	Schema      string              `json:"schema"`
	Controls    []ProtectionControl `json:"controls"`
	Limitations []string            `json:"limitations"`
}

// ProtectionResult carries one state. Document is present whenever a readable
// protection document was read, including an empty one, so the screen's first
// step — registering the first control — is an ordinary completed state away.
type ProtectionResult struct {
	State    State               `json:"state"`
	Reason   string              `json:"reason,omitzero"`
	Entry    string              `json:"entry,omitzero"`
	Document *ProtectionDocument `json:"document,omitzero"`
}

func (r *ProtectionResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// protectionView projects one readable document. An absent entry is the empty
// document the first registration would write, never an error.
func protectionView(entry string, document protect.Document) *ProtectionDocument {
	view := &ProtectionDocument{
		Entry: entry, Schema: protect.Schema, Controls: []ProtectionControl{},
		Limitations: protectionLimitations,
	}
	now := time.Now()
	for _, control := range document.Controls {
		view.Controls = append(view.Controls, ProtectionControl{
			Name: control.Name, Storage: string(control.Storage), State: string(control.State),
			Generation: control.Generation, RotatedAt: control.RotatedAt.UTC().Format(time.RFC3339),
			Rotation: string(control.Rotation(now)), MaxAge: control.MaxAge, Retain: control.Retain,
			Command: control.Command, LocatorArguments: len(control.Arguments), Key: protect.Mask,
		})
	}
	return view
}

// protectionEntryFile resolves the workspace entry one protection document is
// read and written at. The document is data the operator declared, so it is
// read again on every operation rather than held anywhere. An absent entry is
// the empty document the first registration writes, so every operation here
// reads it that way, and one that declares another contract is reported, never
// replaced.
func protectionEntryFile(workspace, entry string) (string, protect.File, refusal) {
	root, declined := resolveFolder(workspace)
	if root == "" {
		return "", protect.File{}, declined
	}
	if entry == "" {
		return "", protect.File{}, refusal{Failed, "name the protection document entry this screen reads and writes"}
	}
	path, err := runEntryPath(root, entry)
	if err != nil {
		return "", protect.File{}, refusal{Failed, "the protection document must be one entry of the open workspace"}
	}
	// An absent entry is where the first document is written; one that exists
	// is read, so it is held to the rule every read entry is.
	if _, err := artifactpath.File(root, entry); err != nil {
		if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
			return "", protect.File{}, refusal{Failed, "the protection document must be one regular file of the open workspace, never a symbolic link"}
		}
	}
	return root, protect.File{Path: path, AbsentIsEmpty: true}, refusal{}
}

// ReadProtection reads one protection document of the open workspace and shows
// every registered control with the key masked. It runs no program, resolves
// no key and contacts nothing: this is the `protect show` of the window.
func (a *App) ReadProtection(workspace, entry string) ProtectionResult {
	return run(a, false, false, func(context.Context) ProtectionResult {
		_, file, declined := protectionEntryFile(workspace, entry)
		if declined.state != "" {
			return ProtectionResult{State: declined.state, Reason: declined.reason}
		}
		document, err := file.Read()
		if err != nil {
			return ProtectionResult{State: Failed, Reason: err.Error()}
		}
		return ProtectionResult{State: Completed, Entry: entry, Document: protectionView(entry, document)}
	})
}

// ProtectionControlRequest registers one control into the document entry named
// here, creating that document when it does not exist yet. The members are the
// ones `protect register` takes: a name, the declared at-rest storage control,
// and the reference to the key — the absolute path of the program that prints
// it and the locator arguments that select it — plus the rotation interval and
// the retention period packages written under it declare.
type ProtectionControlRequest struct {
	Workspace string   `json:"workspace"`
	Entry     string   `json:"entry"`
	Name      string   `json:"name"`
	Storage   string   `json:"storage"`
	Command   string   `json:"command"`
	Arguments []string `json:"arguments"`
	MaxAge    string   `json:"max_age,omitzero"`
	Retain    string   `json:"retain,omitzero"`
}

// SaveProtectionControl registers one control through the protect package's
// own validation and writes the document atomically. The key is never read to
// register a control: a control is a reference, and registering one proves
// nothing about the store behind it — the view says so rather than implying
// a verified property.
func (a *App) SaveProtectionControl(request ProtectionControlRequest) ProtectionResult {
	return run(a, false, true, func(context.Context) ProtectionResult {
		_, file, declined := protectionEntryFile(request.Workspace, request.Entry)
		if declined.state != "" {
			return ProtectionResult{State: declined.state, Reason: declined.reason}
		}
		updated, _, err := file.Register(protect.Control{
			Name:      request.Name,
			Storage:   protect.Storage(request.Storage),
			Command:   request.Command,
			Arguments: request.Arguments,
			MaxAge:    request.MaxAge,
			Retain:    request.Retain,
		}, time.Now())
		if err != nil {
			return ProtectionResult{State: Failed, Reason: err.Error()}
		}
		return ProtectionResult{State: Completed, Entry: request.Entry, Document: protectionView(request.Entry, updated)}
	})
}

// RotateProtectionControl records that the key behind a control was replaced
// in its own store, exactly as `protect rotate` does: the declared store must
// answer for the control before anything is recorded, and the material it
// printed is discarded, never written, logged or shown. readmit never read the
// previous key, so a recorded rotation is an assertion, not a verification.
func (a *App) RotateProtectionControl(workspace, entry, name string) ProtectionResult {
	return runNamed[ProtectionResult, *ProtectionResult](a, profiles["RotateProtectionControl"], func(ctx context.Context) ProtectionResult {
		_, file, declined := protectionEntryFile(workspace, entry)
		if declined.state != "" {
			return ProtectionResult{State: declined.state, Reason: declined.reason}
		}
		updated, _, err := file.Rotate(ctx, name, time.Now())
		if err != nil {
			if protect.StepOf(err) == protect.KeyStep && ctx.Err() != nil {
				return ProtectionResult{State: Cancelled, Reason: "the rotation was cancelled; the recorded rotation is unchanged"}
			}
			return ProtectionResult{State: Failed, Reason: err.Error()}
		}
		return ProtectionResult{State: Completed, Entry: entry, Document: protectionView(entry, updated)}
	})
}

// RetireProtectionControl stops a control writing new packages through the
// protect package's own state change. Retirement is not revocation, and the
// view the result carries states what that does not establish.
func (a *App) RetireProtectionControl(workspace, entry, name string) ProtectionResult {
	return run(a, false, true, func(context.Context) ProtectionResult {
		_, file, declined := protectionEntryFile(workspace, entry)
		if declined.state != "" {
			return ProtectionResult{State: declined.state, Reason: declined.reason}
		}
		updated, _, err := file.Retire(name)
		if err != nil {
			return ProtectionResult{State: Failed, Reason: err.Error()}
		}
		return ProtectionResult{State: Completed, Entry: entry, Document: protectionView(entry, updated)}
	})
}

// ProtectionPackRequest packs the named workspace entries — files or whole
// directories, each copied, never moved — into one new encrypted transfer
// package written under the named control of the protection document.
type ProtectionPackRequest struct {
	Workspace string   `json:"workspace"`
	Entry     string   `json:"entry"`
	Control   string   `json:"control"`
	Sources   []string `json:"sources"`
	Output    string   `json:"output,omitzero"`
}

// ProtectionPackage is one transfer package as its own descriptor declares it,
// without a key. The packed names and sizes are encrypted inside the package,
// so the only content fact this view can carry is the count the descriptor
// records; retention is the descriptor's declaration, reported as the state
// its own instant now stands in.
type ProtectionPackage struct {
	Entry       string `json:"entry"`
	Schema      string `json:"schema"`
	Package     string `json:"package"`
	Control     string `json:"control"`
	Generation  int    `json:"generation"`
	CreatedAt   string `json:"created_at"`
	Entries     int    `json:"entries"`
	NotRead     int    `json:"not_read,omitzero"`
	Retention   string `json:"retention"`
	RetainUntil string `json:"retain_until,omitzero"`
	Cipher      string `json:"cipher"`
	Derivation  string `json:"derivation"`
}

// ProtectionPackageResult carries one state. Package is present only when the
// descriptor was read and verified without a key.
type ProtectionPackageResult struct {
	State       State              `json:"state"`
	Reason      string             `json:"reason,omitzero"`
	Package     *ProtectionPackage `json:"package,omitzero"`
	Limitations []string           `json:"limitations"`
}

func (r *ProtectionPackageResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// packageView projects one descriptor without a key, with the retention state
// evaluated now exactly as `protect inspect` evaluates it.
func packageView(entry string, descriptor protect.Package, now time.Time, notRead int) *ProtectionPackage {
	view := &ProtectionPackage{
		Entry: entry, Schema: descriptor.Schema, Package: descriptor.Package,
		Control: descriptor.Control, Generation: descriptor.Generation,
		CreatedAt: descriptor.CreatedAt.UTC().Format(time.RFC3339),
		Entries:   len(descriptor.Entries), NotRead: notRead,
		Retention:  string(descriptor.Retention(now)),
		Cipher:     descriptor.Cipher,
		Derivation: descriptor.Derivation,
	}
	if !descriptor.RetainUntil.IsZero() {
		view.RetainUntil = descriptor.RetainUntil.UTC().Format(time.RFC3339)
	}
	return view
}

// PackProtectedPackage writes one encrypted transfer package through the
// existing pack operation: the control must be active, the key is read from
// its declared store for the duration of the operation alone, and no plaintext
// temporary copy is written anywhere. The sources are read, never modified,
// and each one is a workspace entry, so nothing outside the open folder is
// reached. A package that cannot be completed is removed rather than left
// looking like one.
func (a *App) PackProtectedPackage(request ProtectionPackRequest) ProtectionPackageResult {
	return runNamed[ProtectionPackageResult, *ProtectionPackageResult](a, profiles["PackProtectedPackage"], func(ctx context.Context) ProtectionPackageResult {
		root, file, declined := protectionEntryFile(request.Workspace, request.Entry)
		if declined.state != "" {
			return ProtectionPackageResult{State: declined.state, Reason: declined.reason}
		}
		if request.Control == "" || len(request.Sources) == 0 {
			return ProtectionPackageResult{State: Failed, Reason: "select the control to write under and at least one entry to pack"}
		}
		sources := make([]string, 0, len(request.Sources))
		for _, name := range request.Sources {
			path, err := artifactpath.File(root, name)
			if err != nil {
				path, err = artifactpath.Child(root, name)
			}
			if err != nil {
				return ProtectionPackageResult{State: Failed, Reason: "every entry to pack must be one regular file or folder of the open workspace, never a symbolic link"}
			}
			sources = append(sources, path)
		}
		destination, refused := destinationFor(root, request.Output, "protected")
		if refused.state != "" {
			return ProtectionPackageResult{State: refused.state, Reason: refused.reason}
		}
		descriptor, notRead, err := file.Pack(ctx, request.Control, sources, filepath.Join(root, destination.Name), time.Now())
		if err != nil {
			switch protect.StepOf(err) {
			case protect.SourcesStep:
				return ProtectionPackageResult{State: Failed, Reason: "the entries to pack exceed what one package of this release holds, or could not be read"}
			case protect.PackageStep:
				return packRefusal(err)
			}
			return ProtectionPackageResult{State: Failed, Reason: err.Error()}
		}
		return ProtectionPackageResult{State: Completed, Package: packageView(destination.Name, descriptor, time.Now(), notRead), Limitations: protectionLimitations}
	})
}

// packRefusal names what a failed pack means. The key's own diagnostics never
// carry material, and a missing key is an unavailable key rather than an empty
// one, so the sentences keep that distinction.
func packRefusal(err error) ProtectionPackageResult {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return ProtectionPackageResult{State: Cancelled, Reason: "the package was cancelled and was removed; nothing was left looking like a package"}
	}
	if errors.Is(err, engine.ErrUnsupportedVersion) {
		return ProtectionPackageResult{State: Failed, Reason: "the protection document was written by a version this release cannot read"}
	}
	return ProtectionPackageResult{State: Failed, Reason: "the key did not resolve from its declared store, or the package could not be written; a package that cannot be completed is removed"}
}

// InspectProtectedPackage reports what one transfer package of the open
// workspace declares about itself, without a key: the `protect inspect` of the
// window. It reads the descriptor alone; the packed names and sizes are inside
// the encrypted index and are not facts this view can carry.
func (a *App) InspectProtectedPackage(workspace, entry string) ProtectionPackageResult {
	return run(a, false, false, func(context.Context) ProtectionPackageResult {
		root, declined := resolveFolder(workspace)
		if root == "" {
			return ProtectionPackageResult{State: declined.state, Reason: declined.reason}
		}
		path, err := runEntryPath(root, entry)
		if err != nil {
			return ProtectionPackageResult{State: Failed, Reason: "a transfer package must be named by one entry of the open workspace"}
		}
		descriptor, _, err := protect.ReadPackage(path)
		if err != nil {
			if errors.Is(err, engine.ErrUnsupportedVersion) {
				return ProtectionPackageResult{State: Failed, Reason: "the package was written by a version this release cannot read"}
			}
			return ProtectionPackageResult{State: Failed, Reason: "this directory is not a transfer package this release inspects; its descriptor must be readable without a key"}
		}
		return ProtectionPackageResult{State: Completed, Package: packageView(entry, descriptor, time.Now(), 0), Limitations: protectionLimitations}
	})
}

// ProtectionOpenRequest opens one transfer package of the open workspace into
// a fresh workspace entry, under the named control of the protection document.
// The package's own control is used when none is named, and naming a different
// one than the package was written under is refused by the operation.
type ProtectionOpenRequest struct {
	Workspace string `json:"workspace"`
	Entry     string `json:"entry"`
	Control   string `json:"control,omitzero"`
	Package   string `json:"package"`
	Output    string `json:"output,omitzero"`
}

// OpenProtectedPackage decrypts one package through the existing open
// operation. The key is resolved only through the control's declared
// reference; a wrong key, a rotated-away key and a tampered package are each
// refused rather than decrypted. The plaintext it writes is protected by the
// destination's own storage control and an owner-only mode, and by nothing
// else: opening a package ends the protection the package carried, and the
// result says so.
func (a *App) OpenProtectedPackage(request ProtectionOpenRequest) ProtectionPackageResult {
	return runNamed[ProtectionPackageResult, *ProtectionPackageResult](a, profiles["OpenProtectedPackage"], func(ctx context.Context) ProtectionPackageResult {
		root, file, declined := protectionEntryFile(request.Workspace, request.Entry)
		if declined.state != "" {
			return ProtectionPackageResult{State: declined.state, Reason: declined.reason}
		}
		packagePath, err := runEntryPath(root, request.Package)
		if err != nil {
			return ProtectionPackageResult{State: Failed, Reason: "the transfer package must be one entry of the open workspace"}
		}
		destination, refused := destinationFor(root, request.Output, "opened")
		if refused.state != "" {
			return ProtectionPackageResult{State: refused.state, Reason: refused.reason}
		}
		descriptor, index, err := file.Open(ctx, request.Control, packagePath, filepath.Join(root, destination.Name))
		if err != nil {
			if protect.StepOf(err) == protect.DescriptorStep {
				return ProtectionPackageResult{State: Failed, Reason: "this directory is not a transfer package this release opens"}
			}
			state, reason := openRefusal(err)
			return ProtectionPackageResult{State: state, Reason: reason}
		}
		return ProtectionPackageResult{
			State:   Completed,
			Package: packageView(destination.Name, descriptor, time.Now(), index.NotRead),
			Limitations: append(append([]string{}, protectionLimitations...),
				"Opening a package ends the protection it carried: the decrypted output is protected by the destination's own storage control and an owner-only mode, and by nothing else."),
		}
	})
}

// openRefusal names what a failed open means. The protect operation's own
// diagnostics are operator-facing sentences that disclose no path and carry no
// material, so they carry unchanged — a rotated-away key is named as itself,
// never as a malfunction — and cancellation and unread versions are mapped to
// their states the way every other result here maps them.
func openRefusal(err error) (State, string) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return Cancelled, "the open was cancelled; the partial output was removed"
	}
	if errors.Is(err, engine.ErrUnsupportedVersion) {
		return Failed, "the package was written by a version this release cannot read"
	}
	return Failed, err.Error()
}

// ProtectionDiscardRequest discards one transfer package of the open
// workspace. A package still within its declared retention period is refused
// unless the reviewer overrides the declaration, and the result states what
// removal does not establish.
type ProtectionDiscardRequest struct {
	Workspace string `json:"workspace"`
	Package   string `json:"package"`
	Override  bool   `json:"override,omitzero"`
}

// ProtectionDiscardResult carries one state. Removed counts the files the
// descriptor declared and the discard unlinked.
type ProtectionDiscardResult struct {
	State       State    `json:"state"`
	Reason      string   `json:"reason,omitzero"`
	Removed     int      `json:"removed,omitzero"`
	Retention   string   `json:"retention,omitzero"`
	Overridden  bool     `json:"overridden,omitzero"`
	Limitations []string `json:"limitations,omitzero"`
}

func (r *ProtectionDiscardResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// DiscardProtectedPackage unlinks exactly the files a package declares through
// the existing discard operation, which refuses a directory holding anything
// the descriptor does not declare before removing anything. The declaration
// gates the destructive action: a package still within its declared retention
// period stays until the override is explicit, and one that declares no period
// is reported as never having declared one.
func (a *App) DiscardProtectedPackage(request ProtectionDiscardRequest) ProtectionDiscardResult {
	return run(a, false, false, func(context.Context) ProtectionDiscardResult {
		root, declined := resolveFolder(request.Workspace)
		if root == "" {
			return ProtectionDiscardResult{State: declined.state, Reason: declined.reason}
		}
		path, err := runEntryPath(root, request.Package)
		if err != nil {
			return ProtectionDiscardResult{State: Failed, Reason: "a transfer package must be named by one entry of the open workspace"}
		}
		descriptor, removed, err := protect.Discard(path, time.Now(), request.Override)
		if err != nil {
			if errors.Is(err, engine.ErrUnsupportedVersion) {
				return ProtectionDiscardResult{State: Failed, Reason: "the package was written by a version this release cannot read"}
			}
			return ProtectionDiscardResult{State: Failed, Reason: err.Error()}
		}
		return ProtectionDiscardResult{
			State:       Completed,
			Removed:     removed,
			Overridden:  request.Override,
			Retention:   string(descriptor.Retention(time.Now())),
			Limitations: []string{protect.DeletionLimitations},
		}
	})
}
