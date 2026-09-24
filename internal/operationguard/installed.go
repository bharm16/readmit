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
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/entitlement"
)

// The fixed names inside this computer's license folder. The signed document
// and the activation record keep the entitlement store's own names.
const (
	installedTrustName      = "trust.json"
	installedPolicyName     = "operation-policy.json"
	installedStateName      = "clock.json"
	installedAdmissionsName = "admissions.json"
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
func InstalledPolicyIn(root string) string { return filepath.Join(root, installedPolicyName) }

// InstalledTrustIn is the trust document kept inside a license folder.
func InstalledTrustIn(root string) string { return filepath.Join(root, installedTrustName) }

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

// installedStore is whichever entitlement store version the folder holds; the
// v1 reader and the v2 reader decide, and neither is migrated into the other.
type installedStore struct {
	v1 *entitlement.Store
	v2 *entitlement.StoreV2
}

func openInstalledStore(root string) (installedStore, error) {
	if _, err := os.Lstat(root); errors.Is(err, fs.ErrNotExist) {
		return installedStore{}, ErrNoLicense
	}
	v1, err := entitlement.Open(root)
	if err == nil {
		return installedStore{v1: v1}, nil
	}
	if !errors.Is(err, entitlement.ErrUnsupportedVersion) {
		return installedStore{}, err
	}
	v2, err := entitlement.OpenV2(root)
	if err != nil {
		return installedStore{}, err
	}
	return installedStore{v2: v2}, nil
}

func (s installedStore) root() string {
	if s.v2 != nil {
		return s.v2.Root
	}
	return s.v1.Root
}

func (s installedStore) author() string {
	if s.v2 != nil {
		return s.v2.Activation.Author
	}
	return ""
}

func (s installedStore) device() string {
	if s.v2 != nil {
		return s.v2.Activation.Device
	}
	return s.v1.Activation.Device
}

func (s installedStore) imported() time.Time {
	if s.v2 != nil {
		return s.v2.Activation.Imported
	}
	return s.v1.Activation.Imported
}

func (s installedStore) released() time.Time {
	if s.v2 != nil {
		return s.v2.Activation.Released
	}
	return s.v1.Activation.Released
}

func (s installedStore) grant(trust entitlement.Trust) error {
	if s.v2 != nil {
		_, err := s.v2.Grant(trust)
		return err
	}
	_, err := s.v1.Grant(trust)
	return err
}

func (s installedStore) renew(data []byte, trust entitlement.Trust) error {
	if s.v2 != nil {
		return s.v2.Renew(data, trust)
	}
	return s.v1.Renew(data, trust)
}

func (s installedStore) release(at time.Time) error {
	if s.v2 != nil {
		return s.v2.Release(at)
	}
	return s.v1.Release(at)
}

func (s installedStore) export(destination string) (string, error) {
	if s.v2 != nil {
		return s.v2.Export(destination)
	}
	return s.v1.Export(destination)
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
	data, err := readFile(filepath.Join(store.root(), entitlement.DocumentName))
	if err != nil {
		return InstalledLicense{}, ErrLicenseUnreadable
	}
	trustData, err := readFile(filepath.Join(store.root(), installedTrustName))
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
	if store.released().IsZero() {
		trust, err := entitlement.DecodeTrust(trustData)
		if err != nil {
			return InstalledLicense{}, ErrTrustUnreadable
		}
		if err := store.grant(trust); err != nil {
			return InstalledLicense{}, err
		}
	}
	installed := InstalledLicense{
		Received: received, Root: store.root(), Author: store.author(), Device: store.device(),
		Imported: store.imported(), Released: store.released(),
	}
	if store.v2 == nil {
		return installed, nil
	}
	policyData, err := readFile(filepath.Join(store.root(), installedPolicyName))
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
	var stagedRoot string
	if received.OperationCapable {
		store, err := entitlement.ImportV2(staging, data, trust, author, device, at)
		if err != nil {
			return err
		}
		stagedRoot = store.Root
	} else {
		store, err := entitlement.Import(staging, data, trust, device, at)
		if err != nil {
			return err
		}
		stagedRoot = store.Root
	}
	final := filepath.Join(filepath.Dir(stagedRoot), filepath.Base(root))
	abandon := func(err error) error { os.RemoveAll(stagedRoot); return err }
	if err := writeNew(filepath.Join(stagedRoot, installedTrustName), trustData); err != nil {
		return abandon(err)
	}
	var policyPath string
	if received.OperationCapable {
		policy := Policy{
			Schema:      PolicySchema,
			Entitlement: filepath.Join(final, entitlement.DocumentName),
			Trust:       filepath.Join(final, installedTrustName),
			State:       filepath.Join(final, installedStateName),
			Author:      author,
			Device:      device,
			Authority:   authority,
		}
		if authority != "" {
			policy.Admissions = filepath.Join(final, installedAdmissionsName)
		}
		encoded, err := EncodePolicy(policy)
		if err != nil {
			return abandon(err)
		}
		if err := writeNew(filepath.Join(stagedRoot, installedPolicyName), encoded); err != nil {
			return abandon(err)
		}
		policyPath = filepath.Join(final, installedPolicyName)
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
	if current.released().IsZero() && operationReleased(current) {
		if err := current.release(at); err != nil {
			return false, err
		}
	}
	if current.released().IsZero() {
		policyPath := filepath.Join(current.root(), installedPolicyName)
		installed, readErr := readFile(filepath.Join(current.root(), entitlement.DocumentName))
		if current.v2 != nil && readErr == nil && bytes.Equal(installed, data) && current.author() == author && current.device() == device {
			if policy, err := readSelectedPolicy(policyPath); err == nil {
				if _, err := os.Lstat(policy.State); errors.Is(err, fs.ErrNotExist) {
					return true, Activate(policyPath)
				}
			}
		}
		return false, ErrLicenseInstalled
	}
	aside := current.root() + ".released-" + current.released().UTC().Format("20060102T150405Z")
	if _, err := os.Lstat(aside); !errors.Is(err, fs.ErrNotExist) {
		return false, ErrLicenseRetained
	}
	if err := os.Rename(current.root(), aside); err != nil {
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
	installedTrust, err := readFile(filepath.Join(store.root(), installedTrustName))
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
		return store.renew(data, trust)
	}
	// The new trust document is written in full beside the installed one
	// before the renewal and renamed over it after, so a refused renewal
	// leaves both as they were and a retained interrupted write is reported
	// before the license changes.
	path := filepath.Join(store.root(), installedTrustName)
	staged := path + incompleteSuffix
	if err := writeNew(staged, trustData); err != nil {
		return ErrLicenseRetained
	}
	if err := store.renew(data, trust); err != nil {
		os.Remove(staged)
		return err
	}
	if err := os.Rename(staged, path); err != nil {
		return errors.New("the renewal is installed but its verification keys could not be kept; move the retained keys file into place")
	}
	return nil
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
	if !store.released().IsZero() {
		return entitlement.ErrReleased
	}
	if store.v2 != nil {
		policyPath := filepath.Join(store.root(), installedPolicyName)
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
	return store.release(at)
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
	return store.export(destination)
}

// operationReleased reports whether a v2 license's operation state records a
// release, whatever its store records.
func operationReleased(store installedStore) bool {
	if store.v2 == nil {
		return false
	}
	policy, err := readSelectedPolicy(filepath.Join(store.root(), installedPolicyName))
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
	if err != nil || !store.released().IsZero() {
		return false
	}
	version, err := entitlement.DeclaredVersion(data)
	if err != nil || (version == entitlement.SchemaV2) != (store.v2 != nil) {
		return false
	}
	if store.v2 == nil {
		return true
	}
	policy, err := readSelectedPolicy(filepath.Join(store.root(), installedPolicyName))
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
	if store.v2 != nil {
		return store.v2.Claims.ID, nil
	}
	return store.v1.Claims.ID, nil
}

// ReadDocument reads one received document as a regular file, bounded, and
// returns its exact bytes.
func ReadDocument(path string) ([]byte, error) { return readFile(path) }

// InstalledTrust returns the trust document installed beside this computer's
// license, the one renewals and pasted licenses are verified against.
func InstalledTrust(root string) ([]byte, error) {
	data, err := readFile(filepath.Join(root, installedTrustName))
	if err != nil {
		return nil, ErrLicenseKeysMissing
	}
	return data, nil
}

const incompleteSuffix = ".incomplete"

// writeNew creates one new private file exclusively and never replaces one.
func writeNew(path string, data []byte) error {
	destination, err := artifactpath.Destination(path)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return errors.New("cannot create a file of this computer's license")
	}
	writeErr := artifactdir.WriteFileSync(file, data)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(destination)
		return errors.New("cannot write a file of this computer's license")
	}
	return nil
}
