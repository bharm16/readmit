package operationguard

// The window-facing license-management half of the operation boundary. The
// desktop facade reaches entitlement documents and runner admission records
// only through this package, never the entitlement package directly. A
// document or store of either version is read here through the entitlement
// package's one version-aware reader, the one the command tree calls
// directly, so every production surface uses the same readers, and nothing
// here issues, signs or gates a read. Everything in this file is license
// management in the sense the operation contract keeps free: none of it
// requires an existing activation.

import (
	"errors"
	"time"

	"github.com/bharm16/readmit/internal/entitlement"
)

// The entitlement shapes the window reports. They are re-exported so a caller
// needs no entitlement import of its own.
type (
	Assignment = entitlement.Assignment
	Authority  = entitlement.Authority
	Admission  = entitlement.Admission
	Capacity   = entitlement.Capacity
)

// ErrTrustUnreadable names a trust document none of this release's readers
// accepts, without repeating any of its bytes.
var ErrTrustUnreadable = errors.New("the trust document cannot be read here; select the vendor's verification keys file")

// ErrAdmissionsMissing names a runner admission record that activation has
// not created or that cannot be read.
var ErrAdmissionsMissing = errors.New("the runner admission record is missing or unreadable; activating the operation policy creates it")

// The ordering refusals a renewal reaches, re-exported so a caller reports
// the entitlement contract's own sentences without importing it.
var (
	ErrSuperseded            = entitlement.ErrSuperseded
	ErrDifferentOrganization = entitlement.ErrDifferentOrganization
)

// Received is the report of one entitlement document this machine verified:
// the document's own declarations plus the term state at the instant the
// caller supplied. A v1 document reports its bound devices and is not
// operation-capable; a v2 document reports its named assignments and runner
// authorities and is.
type Received struct {
	Version          string
	ID               string
	Organization     string
	Plan             string
	Sequence         int
	Issued           time.Time
	NotBefore        time.Time
	Expires          time.Time
	GraceDays        int
	GraceEnds        time.Time
	State            entitlement.State
	Seats            int
	DevicesPerSeat   int
	Devices          []string
	Assignments      []Assignment
	RunnerInstances  int
	Authorities      []Authority
	Capabilities     []string
	KeyID            string
	KeyStatus        entitlement.KeyStatus
	OperationCapable bool

	signed   entitlement.Signed
	verified entitlement.Verified
}

// Assigned reports whether the verified document assigns a device to a named
// author, by the entitlement contract's own rule.
func (r Received) Assigned(author, device string) error {
	if r.verified.V2 == nil {
		return entitlement.ErrAuthorNotNamed
	}
	return r.verified.V2.Assigned(author, device)
}

// RunnerAuthority reports the capacity the verified document grants to a
// named customer-controlled authority.
func (r Received) RunnerAuthority(id string) (Authority, error) {
	if r.verified.V2 == nil {
		return Authority{}, entitlement.ErrAuthorityNotNamed
	}
	return r.verified.V2.Authority(id)
}

// DocumentSummary is the installed-document fact a renewal orders itself by.
type DocumentSummary struct {
	ID           string
	Organization string
	Sequence     int
}

// ReadDocuments reads one received entitlement document and one trust document
// as regular files, bounded, and returns their exact bytes beside an error.
func ReadDocuments(entitlementPath, trustPath string) (data, trustData []byte, err error) {
	if data, err = readFile(entitlementPath); err != nil {
		return nil, nil, err
	}
	if trustData, err = readFile(trustPath); err != nil {
		return nil, nil, err
	}
	return data, trustData, nil
}

// VerifyDocuments verifies received bytes against received trust bytes and
// reports what the document declares. The entitlement reader decides, by the
// version the document declares; nothing is inferred and nothing about the
// machine is read.
func VerifyDocuments(data, trustData []byte, at time.Time) (Received, error) {
	trust, err := entitlement.DecodeTrust(trustData)
	if err != nil {
		return Received{}, ErrTrustUnreadable
	}
	signed, err := entitlement.ReadSigned(data)
	if err != nil {
		return Received{}, err
	}
	verified, err := signed.Verify(trust)
	if err != nil {
		return Received{}, err
	}
	if grant := verified.V2; grant != nil {
		return Received{
			Version: entitlement.SchemaV2, ID: grant.Claims.ID, Organization: grant.Claims.Organization, Plan: grant.Claims.Plan,
			Sequence: grant.Claims.Sequence, Issued: grant.Claims.Issued, NotBefore: grant.Claims.NotBefore,
			Expires: grant.Claims.Expires, GraceDays: grant.Claims.GraceDays, GraceEnds: grant.Claims.GraceEnds(),
			State: grant.StateAt(at), Seats: grant.Claims.Authors.Seats, DevicesPerSeat: grant.Claims.Authors.DevicesPerSeat,
			Assignments: grant.Claims.Authors.Assignments, RunnerInstances: grant.Claims.Runners.Instances,
			Authorities: grant.Claims.Runners.Authorities, Capabilities: grant.Claims.Capabilities,
			KeyID: grant.KeyID, KeyStatus: grant.KeyStatus, OperationCapable: true, signed: signed, verified: verified,
		}, nil
	}
	grant := verified.V1
	bound := make([]string, 0, len(grant.Claims.Scope.Devices))
	for _, device := range grant.Claims.Scope.Devices {
		bound = append(bound, device.ID)
	}
	return Received{
		Version: entitlement.Schema, ID: grant.Claims.ID, Organization: grant.Claims.Organization, Plan: grant.Claims.Plan,
		Sequence: grant.Claims.Sequence, Issued: grant.Claims.Issued, NotBefore: grant.Claims.NotBefore,
		Expires: grant.Claims.Expires, GraceDays: grant.Claims.GraceDays, GraceEnds: grant.Claims.GraceEnds(),
		State: grant.StateAt(at), Seats: grant.Claims.Scope.Seats, RunnerInstances: grant.Claims.Scope.Runners,
		Devices: bound, Capabilities: grant.Claims.Capabilities, KeyID: grant.KeyID, KeyStatus: grant.KeyStatus,
		signed: signed, verified: verified,
	}, nil
}

// DecodeInstalled summarizes an installed document for ordering. It decodes
// strictly without verifying: the policy's trust already decided the installed
// document, and a renewal's question is only whether the new issue is later.
func DecodeInstalled(data []byte) (DocumentSummary, error) {
	document, err := entitlement.DecodeV2(data)
	if err != nil {
		return DocumentSummary{}, err
	}
	return DocumentSummary{ID: document.Entitlement.ID, Organization: document.Entitlement.Organization, Sequence: document.Entitlement.Sequence}, nil
}

// RunnerReport is what one runner authority holds at an instant against the
// capacity the installed entitlement grants it.
type RunnerReport struct {
	Organization string
	Authority    string
	Capacity     Capacity
	At           time.Time
	Admissions   []Admission
}

// ReadRunnerReport reports the configured runner authority's held capacity.
// The policy decides everything: no authority configured, a missing record and
// a document that no longer verifies are each refused by name.
func ReadRunnerReport(path string, at time.Time) (RunnerReport, error) {
	p, grant, err := load(path)
	if err != nil {
		return RunnerReport{}, err
	}
	if p.Admissions == "" {
		return RunnerReport{}, ErrUnavailable
	}
	admissions, err := entitlement.OpenAdmissions(p.Admissions)
	if err != nil {
		return RunnerReport{}, ErrAdmissionsMissing
	}
	capacity, err := admissions.Capacity(grant, at)
	if err != nil {
		return RunnerReport{}, err
	}
	return RunnerReport{Organization: grant.Claims.Organization, Authority: admissions.Record.Authority, Capacity: capacity, At: at, Admissions: admissions.Record.Admissions}, nil
}

// SettleAdmission releases one admission — the instance handed its capacity
// back — or reconciles it, recording that an operator established the
// instance is no longer running. Both are settlement, never new paid work.
func SettleAdmission(path, instance string, reconcile bool, at time.Time) (RunnerReport, error) {
	p, grant, err := load(path)
	if err != nil {
		return RunnerReport{}, err
	}
	if p.Admissions == "" {
		return RunnerReport{}, ErrUnavailable
	}
	admissions, err := entitlement.OpenAdmissions(p.Admissions)
	if err != nil {
		return RunnerReport{}, ErrAdmissionsMissing
	}
	if reconcile {
		err = admissions.Reconcile(instance, at)
	} else {
		err = admissions.Release(instance, at)
	}
	if err != nil {
		return RunnerReport{}, err
	}
	capacity, err := admissions.Capacity(grant, at)
	if err != nil {
		return RunnerReport{}, err
	}
	return RunnerReport{Organization: grant.Claims.Organization, Authority: admissions.Record.Authority, Capacity: capacity, At: at, Admissions: admissions.Record.Admissions}, nil
}
