package desktop

// This computer's license, the way any software purchase is handled: one
// license per computer, activated from the file received at purchase or its
// pasted contents, verified here with no network, renewed in place by
// activating the renewed file, exported as a copy, and deactivated to move the
// seat to another computer. It is the same installed license the command line
// uses without being told where it is: `readmit license show` reports what
// this pane activated, and this pane reports what `readmit license import`
// installed. Everything here is license management, free of any activation,
// and nothing reads, gates or writes evidence. The words a person reads say
// license, activate, renew and deactivate, never a contract name.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bharm16/readmit/internal/operationguard"
)

// renewalWarning is how far ahead of expiry the pane asks for a renewal. It
// is the window's reminder, not a term: the signed document alone decides
// when the license ends and how long its grace lasts.
const renewalWarning = 30 * 24 * time.Hour

const maxPastedLicense = 1 << 20

// InstalledLicenseView is this computer's license in plain facts: who it is
// licensed to, what it includes, who and which computer it was activated for,
// its term decided from the local clock, and whether new work is admitted
// through it now. It carries no signature, key, path or contract name.
type InstalledLicenseView struct {
	Organization string `json:"organization"`
	Plan         string `json:"plan"`
	Sequence     int    `json:"sequence"`
	AuthorSeats  int    `json:"author_seats"`
	RunnerSlots  int    `json:"runner_slots"`
	Author       string `json:"author,omitzero"`
	Device       string `json:"device"`
	RunnerPool   string `json:"runner_pool,omitzero"`
	Starts       string `json:"starts"`
	Expires      string `json:"expires"`
	GraceEnds    string `json:"grace_ends"`
	Term         string `json:"term"`
	DaysLeft     int    `json:"days_left"`
	RenewSoon    bool   `json:"renew_soon"`
	Activated    string `json:"activated"`
	Deactivated  bool   `json:"deactivated"`
	// DeactivatedAt is when this computer was deactivated, when it was.
	DeactivatedAt string `json:"deactivated_at,omitzero"`
	// NewWork reports that new authoring and execution are admitted through
	// this license now; reading, verifying and exporting never need it.
	NewWork bool `json:"new_work"`
	// CurrentFormat is false for a license in the earlier format, which lists
	// licensed computers and admits no new work here.
	CurrentFormat bool `json:"current_format"`
}

// InstalledLicenseResult reports this computer's license: empty when none is
// activated, failed with a plain reason when it cannot be read or a change was
// refused. Outcome names what a completed change did.
type InstalledLicenseResult struct {
	State   State                 `json:"state"`
	Reason  string                `json:"reason,omitzero"`
	Outcome string                `json:"outcome,omitzero"`
	License *InstalledLicenseView `json:"license,omitzero"`
}

func (r *InstalledLicenseResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

// LicenseReviewRequest is a received license to check before activating it:
// its pasted contents, or nothing to choose the file natively. ChooseKeys asks
// for the vendor's verification keys file even when this computer already
// holds one, for a renewal signed after the vendor changed keys.
type LicenseReviewRequest struct {
	Contents   string `json:"contents,omitzero"`
	ChooseKeys bool   `json:"choose_keys"`
}

// LicenseReviewResult is one received license, verified here: what it
// declares, and whether activating it renews this computer's license in place.
// The chosen file and keys file are carried so activation continues from
// exactly what was checked.
type LicenseReviewResult struct {
	State       State                `json:"state"`
	Reason      string               `json:"reason,omitzero"`
	Entitlement string               `json:"entitlement,omitzero"`
	Trust       string               `json:"trust,omitzero"`
	Document    *LicenseDocumentView `json:"document,omitzero"`
	Renewal     bool                 `json:"renewal"`
}

func (r *LicenseReviewResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// LicenseActivateRequest activates a reviewed license on this computer: the
// file or pasted contents, the keys file if one was chosen, and who and which
// computer it is for (and, optionally, the runner pool tests from this
// computer count against), chosen from what the license itself assigns.
type LicenseActivateRequest struct {
	Entitlement string `json:"entitlement,omitzero"`
	Contents    string `json:"contents,omitzero"`
	Trust       string `json:"trust,omitzero"`
	Author      string `json:"author,omitzero"`
	Device      string `json:"device,omitzero"`
	Authority   string `json:"authority,omitzero"`
}

// NewWithInstalledLicense is the shell as it runs: NewWithOperationSelection
// plus this computer's license. When no activation folder selection exists,
// new work in the window is admitted through this computer's license, the one
// the command line uses. An unreadable selection is reported, not hidden by
// falling back to the installed license.
func NewWithInstalledLicense(chooser FolderChooser, documents ShellDocuments, license string) *App {
	a := NewWithOperationSelection(chooser, documents)
	a.licenseRoot = license
	if license != "" && a.operationPolicy == "" && a.operationRestoreRefusal == "" {
		policy := operationguard.InstalledPolicyIn(license)
		if _, err := os.Lstat(policy); err == nil {
			a.operationPolicy = policy
			a.operationGuard = operationguard.New(policy)
		}
	}
	return a
}

// DefaultInstalledLicensePath is where this computer's license lives, the
// place the command line reads it from too.
func DefaultInstalledLicensePath() (string, error) { return operationguard.InstalledLicensePath() }

const noLicenseLocation = "this computer's license has no place in this account's configuration folder"

// LicenseStatus reports this computer's license. It reads one small local
// folder and claims no operation slot, like the other status reads.
func (a *App) LicenseStatus() InstalledLicenseResult {
	if a.licenseRoot == "" {
		return InstalledLicenseResult{State: Failed, Reason: noLicenseLocation}
	}
	return a.installedLicense("")
}

func (a *App) installedLicense(outcome string) InstalledLicenseResult {
	at := time.Now().UTC().Truncate(time.Second)
	installed, err := operationguard.OpenInstalledLicense(a.licenseRoot, at)
	if errors.Is(err, operationguard.ErrNoLicense) {
		return InstalledLicenseResult{State: Empty, Reason: "no license is activated on this computer", Outcome: outcome}
	}
	if err != nil {
		return InstalledLicenseResult{State: Failed, Reason: licenseReason(err)}
	}
	view := &InstalledLicenseView{
		Organization: installed.Organization, Plan: installed.Plan, Sequence: installed.Sequence,
		AuthorSeats: installed.Seats, RunnerSlots: installed.RunnerInstances,
		Author: installed.Author, Device: installed.Device, RunnerPool: installed.Authority,
		Starts: licenseTime(installed.NotBefore), Expires: licenseTime(installed.Expires), GraceEnds: licenseTime(installed.GraceEnds),
		Term: string(installed.State), Activated: licenseTime(installed.Imported),
		Deactivated:   !installed.Released.IsZero() || installed.OperationReleased,
		CurrentFormat: installed.OperationCapable,
	}
	if !installed.Released.IsZero() {
		view.DeactivatedAt = licenseTime(installed.Released)
	}
	if left := installed.Expires.Sub(at); left > 0 {
		view.DaysLeft = int((left + 24*time.Hour - 1) / (24 * time.Hour))
		view.RenewSoon = left <= renewalWarning
	}
	view.NewWork = installed.OperationCapable && installed.Activated && !view.Deactivated &&
		(view.Term == "active" || view.Term == "grace")
	return InstalledLicenseResult{State: Completed, Outcome: outcome, License: view}
}

// ReviewLicense verifies a received license before it is activated: the file
// chosen natively, or the pasted contents, against the vendor verification
// keys this computer already holds or a keys file chosen natively. It writes
// nothing.
func (a *App) ReviewLicense(request LicenseReviewRequest) LicenseReviewResult {
	return run(a, true, false, func(ctx context.Context) LicenseReviewResult {
		if a.licenseRoot == "" {
			return LicenseReviewResult{State: Failed, Reason: noLicenseLocation}
		}
		var data []byte
		var entitlementPath string
		if strings.TrimSpace(request.Contents) != "" {
			if len(request.Contents) > maxPastedLicense {
				return LicenseReviewResult{State: Failed, Reason: "the pasted text is too long to be a license"}
			}
			data = []byte(request.Contents)
		} else {
			files, declined := a.chooseFiles(ctx, "Choose your license file", "License files", "*.json")
			if len(files) == 0 {
				return LicenseReviewResult{State: declined.state, Reason: declined.reason}
			}
			entitlementPath = files[0]
			var reason string
			if data, reason = readReceived(entitlementPath, "the license file cannot be read; choose it again"); reason != "" {
				return LicenseReviewResult{State: Failed, Reason: reason}
			}
		}
		trustData, trustPath, declined := a.verificationKeys(ctx, request.ChooseKeys)
		if declined.state != "" {
			return LicenseReviewResult{State: declined.state, Reason: declined.reason}
		}
		received, err := operationguard.VerifyDocuments(data, trustData, time.Now().UTC().Truncate(time.Second))
		if err != nil {
			return LicenseReviewResult{State: Failed, Reason: licenseReason(err), Trust: trustPath}
		}
		return LicenseReviewResult{
			State: Completed, Entitlement: entitlementPath, Trust: trustPath,
			Document: licenseDocumentView(received), Renewal: operationguard.RenewsInstalledLicense(a.licenseRoot, data),
		}
	})
}

// verificationKeys is the vendor trust document a received license is
// verified against: the one this computer's license holds, unless a keys file
// is asked for or none is held yet. A chosen path is returned so activation
// reads the same file again.
func (a *App) verificationKeys(ctx context.Context, choose bool) ([]byte, string, refusal) {
	if !choose {
		if held, err := operationguard.InstalledTrust(a.licenseRoot); err == nil {
			return held, "", refusal{}
		}
	}
	files, declined := a.chooseFiles(ctx, "Choose your vendor's verification keys file", "Key files", "*.json")
	if len(files) == 0 {
		return nil, "", declined
	}
	data, reason := readReceived(files[0], "the verification keys file cannot be read; choose it again")
	if reason != "" {
		return nil, "", refusal{Failed, reason}
	}
	return data, files[0], refusal{}
}

// readReceived reads one received file chosen by path, bounded and regular.
func readReceived(path, unreadable string) ([]byte, string) {
	if !filepath.IsAbs(path) {
		return nil, "choose the file by its full location"
	}
	data, err := operationguard.ReadDocument(path)
	if err != nil {
		return nil, unreadable
	}
	return data, ""
}

// ActivateLicense activates a received license on this computer, verified
// again at this moment. With a license already active here, a later issue of
// it renews it in place, exactly as `readmit license renew` does; otherwise it
// is installed as `readmit license import` installs this computer's license,
// setting a deactivated one aside. From then on new work in the window is
// admitted through it, as it is on the command line.
func (a *App) ActivateLicense(request LicenseActivateRequest) InstalledLicenseResult {
	return run(a, false, false, func(context.Context) InstalledLicenseResult {
		if a.licenseRoot == "" {
			return InstalledLicenseResult{State: Failed, Reason: noLicenseLocation}
		}
		var data []byte
		var reason string
		switch {
		case strings.TrimSpace(request.Contents) != "":
			if len(request.Contents) > maxPastedLicense {
				return InstalledLicenseResult{State: Failed, Reason: "the pasted text is too long to be a license"}
			}
			data = []byte(request.Contents)
		case request.Entitlement != "":
			if data, reason = readReceived(request.Entitlement, "the license file cannot be read; choose it again"); reason != "" {
				return InstalledLicenseResult{State: Failed, Reason: reason}
			}
		default:
			return InstalledLicenseResult{State: Failed, Reason: "choose your license file or paste its contents first"}
		}
		var trustData []byte
		if request.Trust != "" {
			if trustData, reason = readReceived(request.Trust, "the verification keys file cannot be read; choose it again"); reason != "" {
				return InstalledLicenseResult{State: Failed, Reason: reason}
			}
		}
		at := time.Now().UTC().Truncate(time.Second)
		outcome := "renewed"
		var err error
		if operationguard.RenewsInstalledLicense(a.licenseRoot, data) {
			err = operationguard.RenewInstalledLicense(a.licenseRoot, data, trustData)
		} else {
			outcome = "activated"
			if trustData == nil {
				if trustData, err = operationguard.InstalledTrust(a.licenseRoot); err != nil {
					return InstalledLicenseResult{State: Failed, Reason: "choose your vendor's verification keys file to activate this license"}
				}
			}
			err = operationguard.InstallLicense(a.licenseRoot, data, trustData, request.Author, request.Device, request.Authority, at)
		}
		if err != nil {
			return InstalledLicenseResult{State: Failed, Reason: licenseReason(err)}
		}
		policy := operationguard.InstalledPolicyIn(a.licenseRoot)
		if _, err := os.Lstat(policy); err == nil {
			if err := a.retainOperationPolicy(policy); err != nil {
				return InstalledLicenseResult{State: Failed, Reason: errRetainedSelection.Error()}
			}
		}
		return a.installedLicense(outcome)
	})
}

// DeactivateLicense deactivates this computer so its seat can be reissued for
// another one, exactly as `readmit license release` does: new work stops here
// and on the command line, and existing work stays readable, verifiable and
// exportable, and so does the license itself.
func (a *App) DeactivateLicense() InstalledLicenseResult {
	return run(a, false, false, func(context.Context) InstalledLicenseResult {
		if a.licenseRoot == "" {
			return InstalledLicenseResult{State: Failed, Reason: noLicenseLocation}
		}
		if err := operationguard.ReleaseInstalledLicense(a.licenseRoot, time.Now().UTC().Truncate(time.Second)); err != nil {
			return InstalledLicenseResult{State: Failed, Reason: licenseReason(err)}
		}
		return a.installedLicense("deactivated")
	})
}

// ExportInstalledLicense saves a copy of this computer's license file into a
// folder chosen natively, byte for byte as it was received, exactly as
// `readmit license export` writes it, whatever its term or activation state.
// The result names the resolved path actually written.
func (a *App) ExportInstalledLicense() LicenseExportResult {
	return run(a, true, false, func(ctx context.Context) LicenseExportResult {
		if a.licenseRoot == "" {
			return LicenseExportResult{State: Failed, Reason: noLicenseLocation}
		}
		id, err := operationguard.InstalledDocumentID(a.licenseRoot)
		if err != nil {
			return LicenseExportResult{State: Failed, Reason: licenseReason(err)}
		}
		folder, declined := a.chooseFolder(ctx, "Choose the folder to save a copy of this computer's license in")
		if folder == "" {
			return declined.licenseExport()
		}
		destination := filepath.Join(folder, id+".json")
		if _, err := os.Lstat(destination); err == nil {
			return LicenseExportResult{State: Failed, Reason: "the chosen folder already holds a file with this name; choose a different folder"}
		}
		written, err := operationguard.ExportInstalledLicense(a.licenseRoot, destination)
		if err != nil {
			return probeWriteFailure(folder,
				"this account cannot write into the chosen folder",
				"the license could not be saved into the chosen folder").licenseExport()
		}
		return LicenseExportResult{State: Completed, Document: id, Path: written}
	})
}

// licenseReason says why a license action was refused in the words a person
// buying software uses. It never names a contract, and it repeats nothing
// from the document.
func licenseReason(err error) string {
	for _, reason := range []struct {
		err  error
		text string
	}{
		{operationguard.ErrNoLicense, "no license is activated on this computer"},
		{operationguard.ErrLicenseInstalled, "this computer already has an active license; activate a renewal of it to replace it, or deactivate this computer before activating a different license"},
		{operationguard.ErrLicenseRetained, "an interrupted or earlier license is kept beside this computer's license; move it aside before activating again"},
		{operationguard.ErrLicenseUnreadable, "this computer's license cannot be read; move its folder aside before activating again"},
		{operationguard.ErrLicenseKeysMissing, "this computer's license cannot be checked without your vendor's verification keys; activate it again with the keys file"},
		{operationguard.ErrTrustUnreadable, "the verification keys file cannot be read; choose the keys file your vendor supplied"},
		{operationguard.ErrNoConfigurationDirectory, noLicenseLocation},
		{operationguard.ErrUnsupportedVersion, "this file is not a license this version of readmit can read"},
		{operationguard.ErrUnknownKey, "this license was not signed with your vendor's verification keys; if your vendor changed keys, choose their updated keys file"},
		{operationguard.ErrRetiredKey, "this license was signed after your vendor retired the key it names; ask your vendor to reissue it"},
		{operationguard.ErrRevokedKey, "this license was signed with a key your vendor withdrew; ask your vendor to reissue it"},
		{operationguard.ErrSignature, "this license file was changed after your vendor signed it, or does not match your vendor's keys"},
		{operationguard.ErrSuperseded, "this license is already activated here, or is older than the one activated on this computer"},
		{operationguard.ErrDifferentOrganization, "this license is for a different organization than the one activated on this computer; deactivate this computer before activating it"},
		{operationguard.ErrReleased, "this computer was deactivated; activate the license your vendor issued for it"},
		{operationguard.ErrAuthorNotNamed, "this license does not name that person"},
		{operationguard.ErrDeviceNotAssigned, "this license does not assign this computer to that person; a license moved to another computer is activated there"},
		{operationguard.ErrDeviceNotNamed, "this license does not name this computer; a license moved to another computer is activated there"},
		{operationguard.ErrAuthorityNotNamed, "this license does not include that runner pool"},
		{operationguard.ErrBusy, "another change to this computer's license is in progress or was interrupted; try again"},
		{operationguard.ErrUnavailable, "this computer's license could not be activated for new work: its activation state is missing or cannot be read"},
	} {
		if errors.Is(err, reason.err) {
			return reason.text
		}
	}
	return "this file cannot be used as a license here: it could not be read or verified"
}
