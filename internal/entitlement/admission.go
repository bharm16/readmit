package entitlement

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"github.com/bharm16/readmit/internal/artifactpath"
)

// A runner authority is a local record the organization keeps, not a vendor
// service. readmit-entitlement/v2 grants each named authority a number of
// execution instances it may hold active at once; the authority's admission
// record is where an instance is admitted before it starts, released when it
// finishes, and reconciled when it stopped without saying so. Every decision
// is made against the record on disk under an exclusive update, so two
// processes admitting at once cannot both take the last free instance.
//
// The record is exactly as authoritative as the file: a copy of it on another
// machine is a second authority with the same capacity, and readmit cannot
// tell. That is the stated limit of enforcement on disconnected copies; there
// is no vendor lease service, no call-home and no hardware binding behind it.
const (
	// AdmissionSchema is the runner admission record contract.
	AdmissionSchema = "readmit-runner-admission/v1"

	// MaxAdmissions bounds one record. Released and reconciled admissions are
	// retained rather than dropped, so a record that has admitted this many
	// instances is refused and a new record is started beside it.
	MaxAdmissions = 1024

	// MaxLease bounds how long one admission holds an instance before it is
	// stale. An instance that runs longer renews its lease; one that stops
	// renewing without releasing is what the bound exists to surface.
	MaxLease = 7 * 24 * time.Hour

	admissionKind = "runner admission record"
)

// The refusals an admission record can reach. Each names one reason.
var (
	ErrCapacityExhausted = errors.New("runner capacity is fully admitted; release an instance before admitting another")
	ErrCapacityUncertain = errors.New("runner capacity is held by an admission whose lease ended without release; reconcile it before admitting another instance")
	ErrInstanceAdmitted  = errors.New("this instance is already admitted")
	ErrInstanceUnknown   = errors.New("this instance is not admitted")
	ErrInstanceSettled   = errors.New("this admission was already released or reconciled")
	ErrLeaseTooLong      = errors.New("a lease holds an instance for at most " + strconv.Itoa(int(MaxLease.Hours()/24)) + " days")
	ErrAdmissionUpdate   = errors.New("another admission record update is in progress or was interrupted and retained; retry, or move the retained file aside")
)

// AdmissionState is where an admission stands at an instant.
type AdmissionState string

const (
	// AdmissionActive holds an instance whose lease has not ended.
	AdmissionActive AdmissionState = "active"
	// AdmissionStale holds an instance whose lease ended without a release.
	// The instance may still be running or may have stopped; the record
	// cannot tell, so the capacity stays held until someone reconciles it.
	AdmissionStale AdmissionState = "stale"
	// AdmissionReleased was handed back by the instance itself.
	AdmissionReleased AdmissionState = "released"
	// AdmissionReconciled was settled by an operator who established that the
	// instance is no longer running.
	AdmissionReconciled AdmissionState = "reconciled"
)

// Admission is one execution instance the authority admitted. Released and
// Reconciled are exclusive: an instance either handed its admission back or
// had it settled for it, never both.
type Admission struct {
	Instance   string    `json:"instance"`
	Admitted   time.Time `json:"admitted"`
	LeaseUntil time.Time `json:"lease_until"`
	Released   time.Time `json:"released,omitzero"`
	Reconciled time.Time `json:"reconciled,omitzero"`
}

// StateAt reports where an admission stands at an instant.
func (a Admission) StateAt(at time.Time) AdmissionState {
	switch {
	case !a.Released.IsZero():
		return AdmissionReleased
	case !a.Reconciled.IsZero():
		return AdmissionReconciled
	case at.Before(a.LeaseUntil):
		return AdmissionActive
	default:
		return AdmissionStale
	}
}

// AdmissionRecord is the file a runner authority keeps. It names the
// organization and the authority so a record cannot be applied under another
// organization's entitlement or another authority's capacity.
type AdmissionRecord struct {
	Schema       string      `json:"schema"`
	Organization string      `json:"organization"`
	Authority    string      `json:"authority"`
	Admissions   []Admission `json:"admissions"`
}

// Capacity is what an authority holds at an instant against what it was
// granted. Stale admissions hold capacity exactly as active ones do.
type Capacity struct {
	Instances int
	Active    int
	Stale     int
}

// Free is the number of instances the authority can still admit.
func (c Capacity) Free() int { return c.Instances - c.Active - c.Stale }

// Admissions is an opened admission record file.
type Admissions struct {
	Path   string
	Record AdmissionRecord
}

// CreateAdmissions starts an empty record for one authority the entitlement
// names, at a new file. The organization is taken from the grant so the record
// is bound to the lineage that granted the capacity.
func CreateAdmissions(destination string, grant GrantV2, authority string) (*Admissions, error) {
	if _, err := grant.Authority(authority); err != nil {
		return nil, err
	}
	record := AdmissionRecord{Schema: AdmissionSchema, Organization: grant.Claims.Organization, Authority: authority, Admissions: []Admission{}}
	data, err := EncodeAdmissions(record)
	if err != nil {
		return nil, err
	}
	path, err := create(destination, data)
	if err != nil {
		return nil, err
	}
	return &Admissions{Path: path, Record: record}, nil
}

// OpenAdmissions reads an admission record. It reads; it decides nothing.
func OpenAdmissions(path string) (*Admissions, error) {
	resolved, err := artifactpath.Resolve(path)
	if err != nil {
		return nil, errors.New("an admission record must be an existing file")
	}
	data, err := readAdmissions(resolved)
	if err != nil {
		return nil, err
	}
	record, err := DecodeAdmissions(data)
	if err != nil {
		return nil, err
	}
	return &Admissions{Path: resolved, Record: record}, nil
}

// Capacity reports what this authority holds at an instant against the
// capacity the grant assigns it. The grant must be the same organization's and
// must name this authority; otherwise the record says nothing about it.
func (a *Admissions) Capacity(grant GrantV2, at time.Time) (Capacity, error) {
	return capacity(a.Record, grant, at)
}

// Admit records one execution instance as active until a lease instant, if
// the authority has a free instance. A held instance is refused by name:
// fully admitted capacity asks for a release, and capacity held by a stale
// admission asks for reconciliation, so an interrupted instance's capacity is
// never silently reused.
//
// Admission is a new paid operation, so it is refused outside the term. An
// admission made inside the term is still renewed, released and reconciled
// after it: already-started bounded work finishes.
func (a *Admissions) Admit(grant GrantV2, instance string, at, until time.Time) error {
	if err := identifier(instance); err != nil {
		return errors.New("instance identifier: " + err.Error())
	}
	if err := lease(at, until); err != nil {
		return err
	}
	switch grant.StateAt(at) {
	case StateNotYetValid:
		return ErrNotYetValid
	case StateExpired:
		return ErrExpired
	}
	return a.update(func(record *AdmissionRecord) error {
		held, err := capacity(*record, grant, at)
		if err != nil {
			return err
		}
		if slices.ContainsFunc(record.Admissions, func(m Admission) bool { return m.Instance == instance }) {
			return ErrInstanceAdmitted
		}
		if len(record.Admissions) >= MaxAdmissions {
			return errors.New("an admission record holds at most " + strconv.Itoa(MaxAdmissions) + " admissions")
		}
		if held.Free() < 1 {
			if held.Stale > 0 {
				return ErrCapacityUncertain
			}
			return ErrCapacityExhausted
		}
		record.Admissions = append(record.Admissions, Admission{Instance: instance, Admitted: at, LeaseUntil: until})
		return nil
	})
}

// Renew extends an admission's lease. A stale admission is renewed too: the
// instance reporting in is exactly what resolves the uncertainty its silence
// created. A settled admission is not.
//
// Renewal follows the term as admission does, once the grace window has
// closed: an instance admitted in the term keeps the lease it already holds,
// which is at most MaxLease, and releases when it is done. That is what bounds
// already-started work after expiry rather than letting it renew forever.
func (a *Admissions) Renew(grant GrantV2, instance string, at, until time.Time) error {
	if err := lease(at, until); err != nil {
		return err
	}
	if grant.StateAt(at) == StateExpired {
		return ErrExpired
	}
	if _, err := capacity(a.Record, grant, at); err != nil {
		return err
	}
	return a.settle(instance, func(m *Admission) error {
		if !until.After(m.LeaseUntil) {
			return errors.New("a renewed lease ends after the current one")
		}
		m.LeaseUntil = until
		return nil
	})
}

// Release records that the instance finished, cancelled or otherwise stopped
// on its own account and handed its capacity back.
func (a *Admissions) Release(instance string, at time.Time) error {
	return a.settle(instance, func(m *Admission) error {
		if at.Before(m.Admitted) {
			return errors.New("an admission is released no earlier than it was admitted")
		}
		m.Released = at
		return nil
	})
}

// Reconcile records that an operator established the instance is no longer
// running and settles its admission for it. It is the recovery for an
// interrupted instance whose lease ended without a release, and for one an
// operator knows stopped before its lease ended; readmit records the decision
// and does not check the instance, because it cannot.
func (a *Admissions) Reconcile(instance string, at time.Time) error {
	return a.settle(instance, func(m *Admission) error {
		if at.Before(m.Admitted) {
			return errors.New("an admission is reconciled no earlier than it was admitted")
		}
		m.Reconciled = at
		return nil
	})
}

func (a *Admissions) settle(instance string, apply func(*Admission) error) error {
	if err := identifier(instance); err != nil {
		return errors.New("instance identifier: " + err.Error())
	}
	return a.update(func(record *AdmissionRecord) error {
		index := slices.IndexFunc(record.Admissions, func(m Admission) bool { return m.Instance == instance })
		if index < 0 {
			return ErrInstanceUnknown
		}
		admission := &record.Admissions[index]
		if !admission.Released.IsZero() || !admission.Reconciled.IsZero() {
			return ErrInstanceSettled
		}
		return apply(admission)
	})
}

// update is the one way a record changes. The replacement is created
// exclusively first, so a second updater is refused until this one is renamed
// into place; the current record is then read from disk, the decision made
// against it, and the result written in full and renamed over the original.
// A refused decision leaves the file untouched. An interrupted update is
// retained and reported rather than overwritten, as the entitlement store's is.
func (a *Admissions) update(apply func(*AdmissionRecord) error) error {
	incomplete, err := artifactpath.Destination(a.Path + incompleteSuffix)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(incomplete, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, fs.ErrExist) {
		return ErrAdmissionUpdate
	}
	if err != nil {
		return errors.New("cannot create the admission record update")
	}
	abandon := func(err error) error {
		file.Close()
		os.Remove(incomplete)
		return err
	}
	current, err := readAdmissions(a.Path)
	if err != nil {
		return abandon(err)
	}
	record, err := DecodeAdmissions(current)
	if err != nil {
		return abandon(err)
	}
	if record.Organization != a.Record.Organization || record.Authority != a.Record.Authority {
		return abandon(errors.New("the admission record on disk no longer names the same organization and authority"))
	}
	// The handle now holds what is on disk, whether or not the decision below
	// changes it, so a refused caller sees the record that refused it.
	a.Record = record
	record.Admissions = slices.Clone(record.Admissions)
	if err := apply(&record); err != nil {
		return abandon(err)
	}
	data, err := EncodeAdmissions(record)
	if err != nil {
		return abandon(err)
	}
	_, writeErr := file.Write(data)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(incomplete)
		return errors.New("cannot write the admission record")
	}
	if err := os.Rename(incomplete, a.Path); err != nil {
		os.Remove(incomplete)
		return errors.New("cannot replace the admission record")
	}
	a.Record = record
	return nil
}

func capacity(record AdmissionRecord, grant GrantV2, at time.Time) (Capacity, error) {
	if grant.Claims.Organization != record.Organization {
		return Capacity{}, ErrDifferentOrganization
	}
	authority, err := grant.Authority(record.Authority)
	if err != nil {
		return Capacity{}, err
	}
	held := Capacity{Instances: authority.Instances}
	for _, admission := range record.Admissions {
		switch admission.StateAt(at) {
		case AdmissionActive:
			held.Active++
		case AdmissionStale:
			held.Stale++
		}
	}
	return held, nil
}

func lease(at, until time.Time) error {
	if err := instant(at); err != nil {
		return errors.New("admission time: " + err.Error())
	}
	if err := instant(until); err != nil {
		return errors.New("lease end: " + err.Error())
	}
	if !until.After(at) {
		return errors.New("a lease ends after it begins")
	}
	if until.Sub(at) > MaxLease {
		return ErrLeaseTooLong
	}
	return nil
}

// DecodeAdmissions reads an admission record. Unknown members and unknown
// versions are errors; there is no migration and no repair.
func DecodeAdmissions(data []byte) (AdmissionRecord, error) {
	record, err := decode[AdmissionRecord](data, AdmissionSchema, admissionKind)
	if err != nil {
		return AdmissionRecord{}, err
	}
	if err := validateAdmissions(record); err != nil {
		return AdmissionRecord{}, err
	}
	return record, nil
}

// EncodeAdmissions writes a validated admission record deterministically.
func EncodeAdmissions(record AdmissionRecord) ([]byte, error) {
	if err := validateAdmissions(record); err != nil {
		return nil, err
	}
	return encode(record, admissionKind)
}

func validateAdmissions(record AdmissionRecord) error {
	if record.Schema != AdmissionSchema {
		return ErrUnsupportedVersion
	}
	if err := identifier(record.Organization); err != nil {
		return errors.New("organization: " + err.Error())
	}
	if err := identifier(record.Authority); err != nil {
		return errors.New("runner authority identifier: " + err.Error())
	}
	if record.Admissions == nil {
		return errors.New("an admission record lists its admissions")
	}
	if len(record.Admissions) > MaxAdmissions {
		return errors.New("an admission record holds at most " + strconv.Itoa(MaxAdmissions) + " admissions")
	}
	seen := make(map[string]bool, len(record.Admissions))
	for _, admission := range record.Admissions {
		if err := identifier(admission.Instance); err != nil {
			return errors.New("instance identifier: " + err.Error())
		}
		if seen[admission.Instance] {
			return errors.New("an instance is admitted at most once in a record")
		}
		seen[admission.Instance] = true
		for _, m := range []struct {
			member string
			value  time.Time
		}{{"admission time", admission.Admitted}, {"lease end", admission.LeaseUntil}} {
			if err := instant(m.value); err != nil {
				return errors.New(m.member + ": " + err.Error())
			}
		}
		// A renewed lease may end well past MaxLease after the admission; the
		// bound applies to each admission and renewal, not to their sum.
		if !admission.LeaseUntil.After(admission.Admitted) {
			return errors.New("a lease ends after it begins")
		}
		if !admission.Released.IsZero() && !admission.Reconciled.IsZero() {
			return errors.New("an admission is released or reconciled, never both")
		}
		for _, m := range []struct {
			member string
			value  time.Time
		}{{"release time", admission.Released}, {"reconciliation time", admission.Reconciled}} {
			if m.value.IsZero() {
				continue
			}
			if err := instant(m.value); err != nil {
				return errors.New(m.member + ": " + err.Error())
			}
			if m.value.Before(admission.Admitted) {
				return errors.New("an admission is settled no earlier than it was admitted")
			}
		}
	}
	return nil
}

// readAdmissions reads one record through the same bounded reader the
// entitlement store uses, rooted at the record's directory.
func readAdmissions(path string) ([]byte, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, errors.New("cannot open admission record directory")
	}
	defer root.Close()
	return read(root, filepath.Base(path))
}
