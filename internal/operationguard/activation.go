package operationguard

// An operation activation folder: the folder an administrator supplies, or
// has the window build from a received document, holding the signed document,
// the vendor trust document and one operation policy naming them, the clock
// state activation creates and, when a runner authority is selected, its
// admission record. It is not a new contract: the policy is the operation
// policy the command line admits work through, and the operation contract
// itself requires only the absolute paths the policy names. This computer's
// license folder shares the layout for the files it holds beside its store.
// Creating and renewing a folder is license management: neither requires an
// existing activation, admits work, or reads or writes evidence.

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/entitlement"
)

// The fixed names inside an activation folder and inside this computer's
// license folder. The signed document keeps the entitlement store's own name.
const (
	trustName      = "trust.json"
	policyName     = "operation-policy.json"
	stateName      = "clock.json"
	admissionsName = "admissions.json"
)

var (
	// ErrActivationFolder refuses a folder that does not exist, is not a
	// folder, or is a symbolic link.
	ErrActivationFolder = errors.New("choose an existing folder that is not a symbolic link")
	// ErrActivationOccupied refuses a folder that already holds one of an
	// activation's files; nothing in it is replaced.
	ErrActivationOccupied = errors.New("the chosen folder already holds activation files; choose a new empty folder")
	// ErrActivationWrite names an activation file that could not be written.
	// What was written before it is retained, never removed.
	ErrActivationWrite = errors.New("the activation files could not be created in the chosen folder; an interrupted attempt may be retained")
	// ErrEarlierFormat refuses to create an activation for a license in the
	// v1 format, which binds devices and admits no new work.
	ErrEarlierFormat = errors.New("this license is in an earlier format that lists licensed computers and cannot activate new work here; ask your vendor for a license in the current format")
	// ErrLaterIssueEarlierFormat refuses a v1 license as the later issue of
	// an activation.
	ErrLaterIssueEarlierFormat = errors.New("a later issue must be a license in the current format")
	// ErrActivationReleased refuses to renew a released activation in place.
	ErrActivationReleased = errors.New("this local activation was released; create a new activation folder for the new document")
	// ErrActivationNameTaken refuses a later issue whose name in the folder
	// already holds different bytes.
	ErrActivationNameTaken = errors.New("the activation folder already holds a different document under this name")
	// ErrPolicyUpdateRetained names an interrupted policy update retained
	// beside the policy; it is reported, never replaced.
	ErrPolicyUpdateRetained = errors.New("an interrupted operation policy update is retained beside the policy; recover it outside this application")
)

// ActivationPolicyIn is the operation policy inside an activation folder.
func ActivationPolicyIn(folder string) string { return filepath.Join(folder, policyName) }

// folderPolicy is the operation policy of a folder with this layout, for the
// author and device and the runner authority selected. An unused role is
// explicitly empty, as the policy contract requires, and a folder with no
// runner authority names no admission record.
func folderPolicy(folder, author, device, authority string) Policy {
	policy := Policy{
		Schema:      PolicySchema,
		Entitlement: filepath.Join(folder, entitlement.DocumentName),
		Trust:       filepath.Join(folder, trustName),
		State:       filepath.Join(folder, stateName),
		Author:      author,
		Device:      device,
		Authority:   authority,
	}
	if authority != "" {
		policy.Admissions = filepath.Join(folder, admissionsName)
	}
	return policy
}

// CreateActivation writes an activation folder from a received v2 license:
// the document and the trust document byte for byte, and one operation policy
// naming them with the author, device and runner authority selected from what
// the document itself assigns. The documents are verified again at the instant
// given, never assumed from an earlier verification. The folder must exist and
// hold none of an activation's files; nothing is overwritten. It does not
// activate: activation stays the separate explicit step. It returns the
// policy's path.
func CreateActivation(folder string, data, trustData []byte, author, device, authority string, at time.Time) (string, error) {
	if _, err := artifactpath.Directory(folder); err != nil {
		return "", ErrActivationFolder
	}
	received, err := VerifyDocuments(data, trustData, at)
	if err != nil {
		return "", err
	}
	if !received.OperationCapable {
		return "", ErrEarlierFormat
	}
	if err := received.selects(author, device, authority); err != nil {
		return "", err
	}
	for _, name := range []string{entitlement.DocumentName, trustName, policyName, stateName, admissionsName} {
		if _, err := os.Lstat(filepath.Join(folder, name)); err == nil {
			return "", ErrActivationOccupied
		}
	}
	policy := folderPolicy(folder, author, device, authority)
	encoded, err := EncodePolicy(policy)
	if err != nil {
		return "", err
	}
	path := ActivationPolicyIn(folder)
	for _, file := range []struct {
		path string
		data []byte
	}{{policy.Entitlement, data}, {policy.Trust, trustData}, {path, encoded}} {
		if err := writeNew(file.path, file.data); err != nil {
			return "", activationWriteRefusal(err)
		}
	}
	return path, nil
}

// RenewActivation installs a later issue in the activation folder whose
// policy is at path, verified against the trust document the policy names.
// It refuses a released activation, then orders the issue as the entitlement
// store orders a renewal — the same organization, a sequence later than both
// the installed document's and the one the clock state recorded — and refuses
// a transfer, a reissue that no longer assigns the policy's device to its
// author or no longer names its runner authority. The later issue is written
// beside the installed one under its own name, and the policy is then replaced
// atomically to name it. The clock state is never touched.
func RenewActivation(path string, data []byte, at time.Time) error {
	policy, err := readSelectedPolicy(path)
	if err != nil {
		return ErrUnavailable
	}
	trustData, err := readFile(policy.Trust)
	if err != nil {
		return ErrUnavailable
	}
	received, err := VerifyDocuments(data, trustData, at)
	if err != nil {
		return err
	}
	if !received.OperationCapable {
		return ErrLaterIssueEarlierFormat
	}
	installed, err := readFile(policy.Entitlement)
	if err != nil {
		return ErrUnavailable
	}
	current, err := entitlement.DecodeV2(installed)
	if err != nil {
		return err
	}
	// A released activation is refused before the issue is ordered, as a
	// released store is. The clock records the latest issue admitted here, so
	// the renewal must follow it as well as the installed document. An
	// activation never activated, or whose clock state no reader accepts, has
	// no clock to order by; the installed document alone orders its renewal,
	// and the guard admits no work until the clock is there.
	installedIssue := current.Entitlement
	if state, err := readState(policy.State); err == nil {
		if state.Released {
			return ErrActivationReleased
		}
		installedIssue.Sequence = max(installedIssue.Sequence, state.Sequence)
	}
	if err := received.verified.V2.Claims.Renews(installedIssue); err != nil {
		return err
	}
	if err := received.selects(policy.Author, policy.Device, policy.Authority); err != nil {
		return err
	}
	name := "entitlement-" + received.ID + "-" + strconv.Itoa(received.Sequence) + ".json"
	destination := filepath.Join(filepath.Dir(policy.Entitlement), name)
	if existing, err := readFile(destination); err == nil {
		// A retry of the same interrupted renewal may find the document
		// already written; only the exact same issue continues.
		if !bytes.Equal(existing, data) {
			return ErrActivationNameTaken
		}
	} else if err := writeNew(destination, data); err != nil {
		return activationWriteRefusal(err)
	}
	next := policy
	next.Entitlement = destination
	encoded, err := EncodePolicy(next)
	if err != nil {
		return err
	}
	return replacePolicy(path, encoded)
}

// activationWriteRefusal separates a name already taken in the folder from
// any other failure to write an activation file.
func activationWriteRefusal(err error) error {
	if errors.Is(err, fs.ErrExist) {
		return ErrActivationOccupied
	}
	return ErrActivationWrite
}

// replacePolicy replaces an operation policy atomically: the new policy is
// written in full beside it through an exclusive retained replacement and
// renamed over it, as the clock state is updated. A retained replacement is
// reported, never overwritten.
func replacePolicy(path string, data []byte) error {
	replacement, err := policyFile.Begin(path)
	if errors.Is(err, fs.ErrExist) {
		return ErrPolicyUpdateRetained
	}
	if err != nil {
		return err
	}
	if err := replacement.Write(data); err != nil {
		replacement.Abandon()
		return err
	}
	err = replacement.Commit()
	replacement.Close()
	return err
}

// policyFile is how an operation policy is replaced, through the shared
// document store. A replacement written in full but not renamed into place is
// retained.
var policyFile = artifactdir.Document{
	Errors: artifactdir.DocumentErrors{
		Destination: errPolicyUnwritten,
		Create:      errPolicyUnwritten,
		Write:       errPolicyUnwritten,
		Install:     errors.New("cannot install the operation policy replacement"),
		Sync:        errors.New("the operation policy was replaced but could not be confirmed against a power loss"),
	},
}

// errPolicyUnwritten is a policy replacement that could not be written.
var errPolicyUnwritten = errors.New("cannot write the operation policy replacement")
