package operationguard

// This computer's license: the one license installed for this account, used
// automatically by the application and by the command line. It is not a new
// contract. The folder holds an entitlement store exactly as `readmit license
// import --output` writes one (the signed document and its v1 or v2
// activation record), beside it the vendor trust document it was verified
// against and, for a license that can admit new work, the operation policy,
// clock state and runner admission record the operation guard already reads.
// So `readmit license show STORE` reads it as it reads any store, the
// operation guard admits work through its policy as it does any policy, and
// nothing here reads, gates or writes evidence.

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/entitlement"
)

var (
	// ErrNoLicense names a computer with nothing installed yet.
	ErrNoLicense = errors.New("no license is installed on this computer")
	// ErrLicenseInstalled refuses a second installation beside an active one.
	ErrLicenseInstalled = errors.New("this computer already has a license installed; renew it with a later issue, or release it before installing another")
	// ErrLicenseRetained names an interrupted installation, or a set-aside
	// license, retained where a new installation would go.
	ErrLicenseRetained = errors.New("an interrupted or set-aside license is retained beside this computer's license; move it aside before installing again")
	// ErrLicenseUnreadable names an installed folder no reader accepts. It is
	// reported, never repaired or replaced.
	ErrLicenseUnreadable = errors.New("this computer's license folder cannot be read; move it aside before installing again")
	// ErrLicenseKeysMissing names an installed license with no trust document
	// beside it, which cannot be verified here.
	ErrLicenseKeysMissing = errors.New("this computer's license has no verification keys beside it; install it again with the vendor trust document")
	// ErrNoConfigurationDirectory names an account whose configuration
	// directory cannot be located, so this computer's license has no place.
	ErrNoConfigurationDirectory = errors.New("cannot locate this account's configuration directory for the installed license")
)

// InstalledLicensePath is where this computer's license lives: a folder in
// this account's configuration directory, the same directory the application
// keeps its own shell state in. It is the only discovered location readmit
// uses, and it holds license state only, never evidence.
func InstalledLicensePath() (string, error) {
	root, err := os.UserConfigDir()
	if err != nil || !filepath.IsAbs(root) {
		return "", ErrNoConfigurationDirectory
	}
	return filepath.Join(root, "readmit", "license"), nil
}

// InstalledPolicy is the operation policy of this computer's license, which
// the command line admits new work through when no policy is named
// explicitly. It is empty when the configuration directory cannot be located,
// and naming it opens nothing: a missing file refuses new work exactly as an
// absent policy always has.
func InstalledPolicy() string {
	root, err := InstalledLicensePath()
	if err != nil {
		return ""
	}
	return InstalledPolicyIn(root)
}

// InstalledPolicyIn is the operation policy inside a license folder.
func InstalledPolicyIn(root string) string { return filepath.Join(root, policyName) }

// InstalledTrustIn is the trust document kept inside a license folder.
func InstalledTrustIn(root string) string { return filepath.Join(root, trustName) }

// The store refusals a person meets on this computer's license, re-exported
// so a caller can say them in its own words without importing the
// entitlement package.
var (
	ErrReleased           = entitlement.ErrReleased
	ErrUnsupportedVersion = entitlement.ErrUnsupportedVersion
	ErrUnknownKey         = entitlement.ErrUnknownKey
	ErrRetiredKey         = entitlement.ErrRetiredKey
	ErrRevokedKey         = entitlement.ErrRevokedKey
	ErrSignature          = entitlement.ErrSignature
	ErrAuthorNotNamed     = entitlement.ErrAuthorNotNamed
	ErrDeviceNotAssigned  = entitlement.ErrDeviceNotAssigned
	ErrDeviceNotNamed     = entitlement.ErrDeviceNotNamed
	ErrAuthorityNotNamed  = entitlement.ErrAuthorityNotNamed
)

// InstalledLicense is what this computer's license declares and what this
// computer did with it: the verified document's own facts at an instant, who
// and which device it was activated for, and whether that activation, and the
// operation state new work is admitted through, has been released.
type InstalledLicense struct {
	Received
	Root     string
	Author   string
	Device   string
	Imported time.Time
	Released time.Time
	// Authority is the runner authority this computer's work is admitted
	// against, when one was selected.
	Authority string
	// Activated reports that the operation state exists, so new work is
	// admitted through this license; OperationReleased that it was released.
	Activated         bool
	OperationReleased bool
}

// openInstalledStore opens the entitlement store the folder holds through the
// entitlement reader, which reads it under the version it was written in and
// migrates neither into the other.
func openInstalledStore(root string) (entitlement.Installed, error) {
	if _, err := os.Lstat(root); errors.Is(err, fs.ErrNotExist) {
		return entitlement.Installed{}, ErrNoLicense
	}
	return entitlement.OpenInstalled(root)
}

// OpenInstalledLicense reads and verifies this computer's license at an
// instant. It reads; it writes nothing, admits nothing and repairs nothing. A
// released license is still reported, because what was installed and when it
// was released are facts a person needs.
func OpenInstalledLicense(root string, at time.Time) (InstalledLicense, error) {
	store, err := openInstalledStore(root)
	if err != nil {
		return InstalledLicense{}, err
	}
	data, err := readFile(filepath.Join(store.Root(), entitlement.DocumentName))
	if err != nil {
		return InstalledLicense{}, ErrLicenseUnreadable
	}
	trustData, err := readFile(filepath.Join(store.Root(), trustName))
	if err != nil {
		return InstalledLicense{}, ErrLicenseKeysMissing
	}
	received, err := VerifyDocuments(data, trustData, at)
	if err != nil {
		return InstalledLicense{}, err
	}
	// An active license is verified for what it was activated as by the
	// store's own reader, the one `readmit license show` reports through; a
	// released one is still reported, as released.
	if store.Released().IsZero() {
		trust, err := entitlement.DecodeTrust(trustData)
		if err != nil {
			return InstalledLicense{}, ErrTrustUnreadable
		}
		if _, err := store.Grant(trust); err != nil {
			return InstalledLicense{}, err
		}
	}
	installed := InstalledLicense{
		Received: received, Root: store.Root(), Author: store.Author(), Device: store.Device(),
		Imported: store.Imported(), Released: store.Released(),
	}
	if store.V2 == nil {
		return installed, nil
	}
	policyData, err := readFile(filepath.Join(store.Root(), policyName))
	if err != nil {
		return installed, nil
	}
	policy, err := DecodePolicy(policyData)
	if err != nil {
		return installed, nil
	}
	installed.Authority = policy.Authority
	if state, err := readState(policy.State); err == nil {
		installed.Activated, installed.OperationReleased = true, state.Released
	}
	return installed, nil
}

// InstallLicense verifies a received license and installs it as this
// computer's license, for one device (and, under v2, the named author it is
// assigned to), exactly as `readmit license import` installs a store. A v2
// license is then activated for new work: its operation policy names the
// installed files, the selected runner authority if any, and the operation
// guard creates its clock state and runner record explicitly, as `readmit
// license operation activate` does.
//
// An active installed license is never replaced: a later issue of it is a
// renewal. A released one is set aside beside the new one, never deleted. An
// installation interrupted before its clock was created is finished only by
// the same document for the same author and device.
func InstallLicense(root string, data, trustData []byte, author, device, authority string, at time.Time) error {
	trust, err := entitlement.DecodeTrust(trustData)
	if err != nil {
		return ErrTrustUnreadable
	}
	received, err := VerifyDocuments(data, trustData, at)
	if err != nil {
		return err
	}
	// The assignment is decided before anything on disk changes, so a
	// document that does not name this computer never sets a released
	// license aside.
	if received.OperationCapable {
		if err := received.Assigned(author, device); err != nil {
			return err
		}
	} else if author != "" || authority != "" {
		return entitlement.ErrAuthorNotNamed
	} else if !slices.Contains(received.Devices, device) {
		return entitlement.ErrDeviceNotNamed
	}
	if authority != "" {
		if _, err := received.RunnerAuthority(authority); err != nil {
			return err
		}
	}
	// A retained interrupted installation is reported before anything is
	// set aside, so a refusal leaves the installed license where it was.
	staging := root + incompleteSuffix
	if _, err := os.Lstat(staging); !errors.Is(err, fs.ErrNotExist) {
		return ErrLicenseRetained
	}
	if _, err := os.Lstat(root); err == nil {
		if finished, err := setAsideOrFinish(root, data, author, device, at); err != nil || finished {
			return err
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return ErrLicenseUnreadable
	}
	if err := os.MkdirAll(filepath.Dir(root), 0o700); err != nil {
		return ErrNoConfigurationDirectory
	}
	staged, err := received.signed.Import(staging, trust, author, device, at)
	if err != nil {
		return err
	}
	stagedRoot := staged.Root()
	final := filepath.Join(filepath.Dir(stagedRoot), filepath.Base(root))
	abandon := func(err error) error { os.RemoveAll(stagedRoot); return err }
	if err := writeNew(filepath.Join(stagedRoot, trustName), trustData); err != nil {
		return abandon(err)
	}
	var policyPath string
	if received.OperationCapable {
		encoded, err := EncodePolicy(folderPolicy(final, author, device, authority))
		if err != nil {
			return abandon(err)
		}
		if err := writeNew(filepath.Join(stagedRoot, policyName), encoded); err != nil {
			return abandon(err)
		}
		policyPath = filepath.Join(final, policyName)
	}
	if err := os.Rename(stagedRoot, final); err != nil {
		return abandon(errors.New("cannot install this computer's license"))
	}
	if policyPath == "" {
		return nil
	}
	return Activate(policyPath)
}

// setAsideOrFinish decides what an existing installed folder means for a new
// installation: a released license is moved aside under the instant it was
// released, an interrupted installation of the same document is finished, and
// anything else is refused. A license whose operation state was released but
// whose store was not — a deactivation interrupted between the two, or `license
// operation release` — admits no new work already, so its store's release is
// recorded now and it is set aside like any released license. finished
// reports that the installation is now complete and nothing further is
// written.
func setAsideOrFinish(root string, data []byte, author, device string, at time.Time) (finished bool, err error) {
	current, err := openInstalledStore(root)
	if err != nil {
		return false, ErrLicenseUnreadable
	}
	if current.Released().IsZero() && operationReleased(current) {
		if err := current.Release(at); err != nil {
			return false, err
		}
	}
	if current.Released().IsZero() {
		policyPath := filepath.Join(current.Root(), policyName)
		installed, readErr := readFile(filepath.Join(current.Root(), entitlement.DocumentName))
		if current.V2 != nil && readErr == nil && bytes.Equal(installed, data) && current.Author() == author && current.Device() == device {
			if policy, err := readSelectedPolicy(policyPath); err == nil {
				if _, err := os.Lstat(policy.State); errors.Is(err, fs.ErrNotExist) {
					return true, Activate(policyPath)
				}
			}
		}
		return false, ErrLicenseInstalled
	}
	aside := current.Root() + ".released-" + current.Released().UTC().Format("20060102T150405Z")
	if _, err := os.Lstat(aside); !errors.Is(err, fs.ErrNotExist) {
		return false, ErrLicenseRetained
	}
	if err := os.Rename(current.Root(), aside); err != nil {
		return false, ErrLicenseRetained
	}
	return false, nil
}

func readSelectedPolicy(path string) (Policy, error) {
	data, err := readFile(path)
	if err != nil {
		return Policy{}, err
	}
	return DecodePolicy(data)
}

// RenewInstalledLicense installs a later issue of this computer's license in
// place, exactly as `readmit license renew` renews a store: the same
// organization, a later sequence, the same device (and author). Verification
// uses the trust document installed beside it, or a newly supplied one — a
// renewal signed after a key rotation — which then replaces it. The operation
// policy keeps naming the installed document, so new work is admitted under
// the renewal from the next admission on, and the clock state is untouched.
func RenewInstalledLicense(root string, data, trustData []byte) error {
	store, err := openInstalledStore(root)
	if err != nil {
		return err
	}
	installedTrust, err := readFile(filepath.Join(store.Root(), trustName))
	if err != nil {
		return ErrLicenseKeysMissing
	}
	chosen := installedTrust
	if trustData != nil {
		chosen = trustData
	}
	trust, err := entitlement.DecodeTrust(chosen)
	if err != nil {
		return ErrTrustUnreadable
	}
	if trustData == nil || bytes.Equal(trustData, installedTrust) {
		return store.Renew(data, trust)
	}
	// The new trust document is written in full beside the installed one
	// before the renewal and renamed over it after, so a refused renewal
	// leaves both as they were and a retained interrupted write is reported
	// before the license changes.
	replacement, err := trustFile.Begin(filepath.Join(store.Root(), trustName))
	if err != nil {
		return ErrLicenseRetained
	}
	if err := replacement.Write(trustData); err != nil {
		replacement.Abandon()
		return ErrLicenseRetained
	}
	if err := store.Renew(data, trust); err != nil {
		replacement.Abandon()
		return err
	}
	err = replacement.Commit()
	replacement.Close()
	return err
}

// trustFile is how the installed trust document is replaced by a renewal
// that brings new keys. A replacement installed after the renewal but not
// renamed into place is retained for the person to move.
var trustFile = artifactdir.Document{
	Errors: artifactdir.DocumentErrors{
		Install: errors.New("the renewal is installed but its verification keys could not be kept; move the retained keys file into place"),
		Sync:    errors.New("the renewal is installed but its verification keys could not be confirmed against a power loss"),
	},
}

// ReleaseInstalledLicense deactivates this computer: the operation state is
// released first, so new work stops even if the rest is interrupted, and then
// the store's activation, exactly as `readmit license release` records it.
// Releasing is a local record, not a proof to the vendor; the seat is
// reissued by the vendor, and the released license still exports.
func ReleaseInstalledLicense(root string, at time.Time) error {
	store, err := openInstalledStore(root)
	if err != nil {
		return err
	}
	if !store.Released().IsZero() {
		return entitlement.ErrReleased
	}
	if store.V2 != nil {
		policyPath := filepath.Join(store.Root(), policyName)
		if policy, err := readSelectedPolicy(policyPath); err == nil {
			if _, err := os.Lstat(policy.State); err == nil {
				// Clock state no reader accepts admits no work and is never
				// repaired here, so it does not hold the release back; a
				// retained or concurrent update does.
				if err := Release(policyPath); err != nil && !errors.Is(err, ErrUnavailable) {
					return err
				}
			}
		}
	}
	return store.Release(at)
}

// ExportInstalledLicense writes this computer's license document to a new
// file, byte for byte as it was received, exactly as `readmit license export`
// does, whatever its term or activation state. It returns the resolved path
// actually written.
func ExportInstalledLicense(root, destination string) (string, error) {
	store, err := openInstalledStore(root)
	if err != nil {
		return "", err
	}
	return store.Export(destination)
}

// operationReleased reports whether a v2 license's operation state records a
// release, whatever its store records.
func operationReleased(store entitlement.Installed) bool {
	if store.V2 == nil {
		return false
	}
	policy, err := readSelectedPolicy(filepath.Join(store.Root(), policyName))
	if err != nil {
		return false
	}
	state, err := readState(policy.State)
	return err == nil && state.Released
}

// RenewsInstalledLicense reports whether a received license renews this
// computer's license in place rather than installing a new one: a license of
// the same contract version is installed, it is not released, and its
// operation state, if it admits new work, exists and is not released. An
// interrupted installation is finished by installing, not renewed, and a
// license of another version is a different license, never a renewal.
func RenewsInstalledLicense(root string, data []byte) bool {
	store, err := openInstalledStore(root)
	if err != nil || !store.Released().IsZero() {
		return false
	}
	// A license that names authors never renews one that binds devices, nor
	// the reverse: each is renewed only by its own version's store.
	received, err := entitlement.ReadSigned(data)
	if err != nil || received.NamesAuthors() != (store.V2 != nil) {
		return false
	}
	if store.V2 == nil {
		return true
	}
	policy, err := readSelectedPolicy(filepath.Join(store.Root(), policyName))
	if err != nil {
		return false
	}
	state, err := readState(policy.State)
	return err == nil && !state.Released
}

// InstalledDocumentID names the installed document, read without verifying
// it, so an export is named even when nothing here can verify it any longer.
func InstalledDocumentID(root string) (string, error) {
	store, err := openInstalledStore(root)
	if err != nil {
		return "", err
	}
	return store.ID(), nil
}

// ReadDocument reads one received document as a regular file, bounded, and
// returns its exact bytes.
func ReadDocument(path string) ([]byte, error) { return readFile(path) }

// InstalledTrust returns the trust document installed beside this computer's
// license, the one renewals and pasted licenses are verified against.
func InstalledTrust(root string) ([]byte, error) {
	data, err := readFile(filepath.Join(root, trustName))
	if err != nil {
		return nil, ErrLicenseKeysMissing
	}
	return data, nil
}

const incompleteSuffix = ".incomplete"

// writeNew creates one new private file exclusively and never replaces one. A
// file it cannot create carries the cause behind its sentence, so a caller can
// tell a name already taken from a folder it cannot write.
func writeNew(path string, data []byte) error {
	return licenseFile.Create(path, data)
}

// licenseFile is how each file of this computer's license is created.
var licenseFile = artifactdir.Document{
	Errors: artifactdir.DocumentErrors{
		Create: errors.New("cannot create a file of this computer's license"),
		Write:  errors.New("cannot write a file of this computer's license"),
	},
}
