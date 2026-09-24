package desktop

// The license half of the operation-access pane. Everything here is license
// management in the sense the operation contract keeps free: verifying a
// received document, creating or renewing the local activation configuration,
// exporting the installed document, settling runner capacity, and reporting
// the operator-supplied commercial destination. None of it requires an
// existing activation, gates a read, or issues anything: the application never
// signs an entitlement and never possesses a vendor key.

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"github.com/bharm16/readmit/internal/artifactdir"
	"github.com/bharm16/readmit/internal/artifactpath"
	"github.com/bharm16/readmit/internal/operationguard"
)

// The fixed names of the private activation folder the pane creates. They are
// this application's own layout inside a folder the operator chose; the
// operation contract itself only requires the absolute paths the policy names.
const (
	activationEntitlementName = "entitlement.json"
	activationTrustName       = "trust.json"
	activationPolicyName      = "operation-policy.json"
	activationStateName       = "clock.json"
	activationAdmissionsName  = "admissions.json"
)

// The commercial destinations contract is operator-supplied configuration for
// deliberate navigation only: the pane shows the destination and opens it when
// the person chooses to. This application reads it locally, contacts nothing,
// and invents no destination of its own.
const (
	commercialDestinationsSchema = "readmit-commercial-destinations/v1"
	commercialSelectionSchema    = "readmit-desktop-commercial-selection/v1"
	commercialPrerequisite       = "the commercial portal destination is not configured; choose the operator-supplied destinations file"
	commercialUndecodable        = "the destinations file cannot be read here; choose a valid commercial destinations file again"
)

var errRetainedSelection = errors.New("cannot retain operation policy selection")
var errRetainedPolicyUpdate = errors.New("an interrupted operation policy update is retained beside the policy; recover it outside this application")

// LicenseAssignmentView is one named author and the devices the issuer
// assigned to that person.
type LicenseAssignmentView struct {
	Author  string   `json:"author"`
	Devices []string `json:"devices"`
}

// LicenseAuthorityView is one customer-controlled runner authority and the
// execution instances it was granted.
type LicenseAuthorityView struct {
	ID        string `json:"id"`
	Instances int    `json:"instances"`
}

// LicenseDocumentView is what one verified entitlement declares: identifiers,
// dates and counts the document itself carries, plus the term state decided
// from the local clock. It renders no signature value and no path.
type LicenseDocumentView struct {
	Version          string                  `json:"version"`
	ID               string                  `json:"id"`
	Organization     string                  `json:"organization"`
	Plan             string                  `json:"plan"`
	Sequence         int                     `json:"sequence"`
	Issued           string                  `json:"issued,omitzero"`
	NotBefore        string                  `json:"not_before,omitzero"`
	Expires          string                  `json:"expires,omitzero"`
	GraceDays        int                     `json:"grace_days,omitzero"`
	GraceEnds        string                  `json:"grace_ends,omitzero"`
	State            string                  `json:"state,omitzero"`
	Seats            int                     `json:"seats,omitzero"`
	DevicesPerSeat   int                     `json:"devices_per_seat,omitzero"`
	Devices          []string                `json:"devices,omitzero"`
	Assignments      []LicenseAssignmentView `json:"assignments,omitzero"`
	RunnerInstances  int                     `json:"runner_instances,omitzero"`
	Authorities      []LicenseAuthorityView  `json:"authorities,omitzero"`
	Capabilities     []string                `json:"capabilities,omitzero"`
	KeyID            string                  `json:"key_id,omitzero"`
	KeyStatus        string                  `json:"key_status,omitzero"`
	OperationCapable bool                    `json:"operation_capable"`
}

// LicenseVerifyResult carries one verification of a received document. The
// paths are carried so the import step can continue from exactly what was
// verified; a state other than Completed never carries them.
type LicenseVerifyResult struct {
	State       State                `json:"state"`
	Reason      string               `json:"reason,omitzero"`
	Entitlement string               `json:"entitlement,omitzero"`
	Trust       string               `json:"trust,omitzero"`
	Document    *LicenseDocumentView `json:"document,omitzero"`
}

func (r *LicenseVerifyResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// LicenseFolderResult carries one native folder choice for a new activation.
type LicenseFolderResult struct {
	State  State  `json:"state"`
	Reason string `json:"reason,omitzero"`
	Folder string `json:"folder,omitzero"`
}

func (r *LicenseFolderResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// LicenseActivationRequest names the verified documents, the role selections
// made from what the document assigns, and the private folder the activation
// is created in. An unused author/device or authority selection is explicitly
// empty, exactly as the operation policy contract requires.
type LicenseActivationRequest struct {
	Entitlement string `json:"entitlement"`
	Trust       string `json:"trust"`
	Author      string `json:"author,omitzero"`
	Device      string `json:"device,omitzero"`
	Authority   string `json:"authority,omitzero"`
	Folder      string `json:"folder"`
}

// LicenseExportResult carries one byte-for-byte export of the installed
// entitlement document.
type LicenseExportResult struct {
	State    State  `json:"state"`
	Reason   string `json:"reason,omitzero"`
	Document string `json:"document,omitzero"`
	Path     string `json:"path,omitzero"`
}

func (r *LicenseExportResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// RunnerAdmissionView is one execution instance the authority holds, with the
// state decided from the local clock.
type RunnerAdmissionView struct {
	Instance   string `json:"instance"`
	Admitted   string `json:"admitted"`
	LeaseUntil string `json:"lease_until"`
	State      string `json:"state"`
}

// RunnerStatusResult reports what the configured runner authority holds
// against the capacity the installed entitlement grants it.
type RunnerStatusResult struct {
	State        State                 `json:"state"`
	Reason       string                `json:"reason,omitzero"`
	Organization string                `json:"organization,omitzero"`
	Authority    string                `json:"authority,omitzero"`
	Instances    int                   `json:"instances,omitzero"`
	Active       int                   `json:"active,omitzero"`
	Stale        int                   `json:"stale,omitzero"`
	Free         int                   `json:"free,omitzero"`
	Admissions   []RunnerAdmissionView `json:"admissions,omitzero"`
}

func (r *RunnerStatusResult) refuse(state State, reason string) { r.State, r.Reason = state, reason }

// RunnerSettleRequest settles one admission: released when the instance
// handed its admission back, reconciled when an operator established that it
// is no longer running.
type RunnerSettleRequest struct {
	Instance  string `json:"instance"`
	Reconcile bool   `json:"reconcile"`
}

// CommercialStatusResult reports the operator-supplied commercial portal
// destination: a visible prerequisite until it is configured, and a
// destination this application shows but never contacts on its own.
type CommercialStatusResult struct {
	State       State  `json:"state"`
	Reason      string `json:"reason,omitzero"`
	Environment string `json:"environment,omitzero"`
	Portal      string `json:"portal,omitzero"`
	ConfigPath  string `json:"config_path,omitzero"`
}

func (r *CommercialStatusResult) refuse(state State, reason string) {
	r.State, r.Reason = state, reason
}

type commercialDestinations struct {
	Schema      string `json:"schema"`
	Environment string `json:"environment"`
	Portal      string `json:"portal"`
}

type commercialSelection struct {
	Schema string `json:"schema"`
	Config string `json:"config"`
}

func licenseTime(value time.Time) string { return value.Format(time.RFC3339) }

// readSelectedPolicy reads and strict-decodes the selected operation policy.
func readSelectedPolicy(path string) (operationguard.Policy, error) {
	data, err := readOperationFile(path)
	if err != nil {
		return operationguard.Policy{}, err
	}
	return operationguard.DecodePolicy(data)
}

// createPrivateFile writes one new private file exclusively. An existing file
// is refused, never replaced.
func createPrivateFile(path string, data []byte) error {
	destination, err := artifactpath.Destination(path)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	writeErr := artifactdir.WriteFileSync(file, data)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(destination)
		return errors.New("cannot write the file")
	}
	return nil
}

// privateWriteRefusal separates a folder this account cannot write from other
// creation refusals, including an already occupied name.
func privateWriteRefusal(folder string, err error) refusal {
	if errors.Is(err, fs.ErrExist) {
		return refusal{Failed, "the chosen folder already holds activation files; choose a new empty folder"}
	}
	return probeWriteFailure(folder,
		"this account cannot create the activation files in the chosen folder",
		"the activation files could not be created in the chosen folder; an interrupted attempt may be retained")
}

// retainOperationPolicy persists an explicit policy selection and installs the
// guard for it, exactly as selecting a supplied folder does.
func (a *App) retainOperationPolicy(path string) error {
	a.operationMu.Lock()
	defer a.operationMu.Unlock()
	if a.operationSelectionPath != "" {
		encoded, err := json.Marshal(operationSelection{Schema: operationSelectionSchema, Policy: path})
		if err != nil || writeShellDocument(a.operationSelectionPath, append(encoded, '\n')) != nil {
			return errRetainedSelection
		}
	}
	a.operationPolicy = path
	a.operationGuard = operationguard.New(path)
	return nil
}

// installOperationPolicy points this shell at a policy file without rewriting
// the retained selection: a renewal replaced the policy's content, not its path.
func (a *App) installOperationPolicy(path string) {
	a.operationMu.Lock()
	defer a.operationMu.Unlock()
	a.operationPolicy = path
	a.operationGuard = operationguard.New(path)
}

// replacePolicyFile replaces the operation policy atomically through an
// exclusive retained replacement, mirroring the guard's own state updates.
func replacePolicyFile(path string, data []byte) error {
	destination, err := artifactpath.Destination(path + incompleteSuffix)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return errRetainedPolicyUpdate
	}
	if err != nil {
		return err
	}
	writeErr := artifactdir.WriteFileSync(file, data)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(destination)
		return errors.New("cannot write the operation policy replacement")
	}
	if err = os.Rename(destination, path); err != nil {
		return errors.New("cannot install the operation policy replacement")
	}
	return nil
}

// VerifyLicenseDocument verifies one received entitlement against a trust
// store, both chosen through native file dialogs. Verification is total and
// local: the v1 and v2 readers the command line uses decide, and the report is
// the document's own declarations plus the term state from the local clock.
func (a *App) VerifyLicenseDocument() LicenseVerifyResult {
	return run(a, true, false, func(ctx context.Context) LicenseVerifyResult {
		files, declined := a.chooseFiles(ctx, "Choose the received entitlement document", "JSON documents", "*.json")
		if len(files) == 0 {
			return LicenseVerifyResult{State: declined.state, Reason: declined.reason}
		}
		entitlementPath := files[0]
		files, declined = a.chooseFiles(ctx, "Choose the vendor trust document", "JSON documents", "*.json")
		if len(files) == 0 {
			return LicenseVerifyResult{State: declined.state, Reason: declined.reason}
		}
		trustPath := files[0]
		if !filepath.IsAbs(entitlementPath) || !filepath.IsAbs(trustPath) {
			return LicenseVerifyResult{State: Failed, Reason: "select absolute paths for the received documents"}
		}
		view, reason := verifyLicenseFiles(entitlementPath, trustPath, time.Now().UTC().Truncate(time.Second))
		if reason != "" {
			return LicenseVerifyResult{State: Failed, Reason: reason}
		}
		return LicenseVerifyResult{State: Completed, Entitlement: entitlementPath, Trust: trustPath, Document: view}
	})
}

// verifyLicenseFiles reads and verifies both documents through the shared
// operation boundary the command line uses. A non-empty reason is the
// refusal to report.
func verifyLicenseFiles(entitlementPath, trustPath string, at time.Time) (*LicenseDocumentView, string) {
	data, trustData, err := operationguard.ReadDocuments(entitlementPath, trustPath)
	if err != nil {
		return nil, operationguard.ErrUnavailable.Error()
	}
	received, err := operationguard.VerifyDocuments(data, trustData, at)
	if err != nil {
		return nil, err.Error()
	}
	return licenseDocumentView(received), ""
}

// licenseDocumentView is what one verified document declares, as the window
// reports it.
func licenseDocumentView(received operationguard.Received) *LicenseDocumentView {
	view := &LicenseDocumentView{
		Version: received.Version, ID: received.ID, Organization: received.Organization, Plan: received.Plan,
		Sequence: received.Sequence, Issued: licenseTime(received.Issued), NotBefore: licenseTime(received.NotBefore),
		Expires: licenseTime(received.Expires), GraceDays: received.GraceDays, GraceEnds: licenseTime(received.GraceEnds),
		State: string(received.State), Seats: received.Seats, DevicesPerSeat: received.DevicesPerSeat,
		Devices: received.Devices, RunnerInstances: received.RunnerInstances, Capabilities: received.Capabilities,
		KeyID: received.KeyID, KeyStatus: string(received.KeyStatus), OperationCapable: received.OperationCapable,
	}
	for _, assignment := range received.Assignments {
		view.Assignments = append(view.Assignments, LicenseAssignmentView{Author: assignment.Author, Devices: assignment.Devices})
	}
	for _, authority := range received.Authorities {
		view.Authorities = append(view.Authorities, LicenseAuthorityView{ID: authority.ID, Instances: authority.Instances})
	}
	return view
}

// ChooseLicenseFolder asks for the existing private folder a new activation is
// created in. It writes nothing; creation is the separate explicit action.
func (a *App) ChooseLicenseFolder() LicenseFolderResult {
	return run(a, true, false, func(ctx context.Context) LicenseFolderResult {
		folder, declined := a.chooseFolder(ctx, "Choose the private folder for the local license activation")
		if folder == "" {
			return LicenseFolderResult{State: declined.state, Reason: declined.reason}
		}
		if _, err := artifactpath.Directory(folder); err != nil {
			return LicenseFolderResult{State: Failed, Reason: "choose an existing folder that is not a symbolic link"}
		}
		return LicenseFolderResult{State: Completed, Folder: folder}
	})
}

// CreateLicenseActivation writes the local activation configuration the
// operator would otherwise hand-author: the received documents, and one
// operation policy naming the role selections the person made from what the
// document itself assigns. It verifies the documents again at creation, never
// overwrites an occupied folder, and never activates: activation stays the
// separate explicit action it is on the command line.
func (a *App) CreateLicenseActivation(request LicenseActivationRequest) OperationResult {
	return run(a, false, false, func(context.Context) OperationResult {
		if !filepath.IsAbs(request.Entitlement) || !filepath.IsAbs(request.Trust) || !filepath.IsAbs(request.Folder) {
			return OperationResult{State: Failed, Reason: "select absolute paths for the received documents and the activation folder"}
		}
		if (request.Author == "") != (request.Device == "") {
			return OperationResult{State: Failed, Reason: "select the author and the device together"}
		}
		if _, err := artifactpath.Directory(request.Folder); err != nil {
			return OperationResult{State: Failed, Reason: "choose an existing folder that is not a symbolic link"}
		}
		data, trustData, err := operationguard.ReadDocuments(request.Entitlement, request.Trust)
		if err != nil {
			return OperationResult{State: Failed, Reason: operationguard.ErrUnavailable.Error()}
		}
		// The documents are verified again at the moment of creation: what was
		// verified a moment ago is never assumed to still be there.
		received, err := operationguard.VerifyDocuments(data, trustData, time.Now().UTC().Truncate(time.Second))
		if err != nil {
			return OperationResult{State: Failed, Reason: err.Error()}
		}
		if !received.OperationCapable {
			return OperationResult{State: Failed, Reason: "this license is in an earlier format that lists licensed computers and cannot activate new work here; ask your vendor for a license in the current format"}
		}
		if request.Author != "" {
			if err = received.Assigned(request.Author, request.Device); err != nil {
				return OperationResult{State: Failed, Reason: err.Error()}
			}
		}
		if request.Authority != "" {
			if _, err = received.RunnerAuthority(request.Authority); err != nil {
				return OperationResult{State: Failed, Reason: err.Error()}
			}
		}
		for _, name := range []string{activationEntitlementName, activationTrustName, activationPolicyName, activationStateName, activationAdmissionsName} {
			if _, err := os.Lstat(filepath.Join(request.Folder, name)); err == nil {
				return OperationResult{State: Failed, Reason: "the chosen folder already holds activation files; choose a new empty folder"}
			}
		}
		policy := operationguard.Policy{
			Schema:      operationguard.PolicySchema,
			Entitlement: filepath.Join(request.Folder, activationEntitlementName),
			Trust:       filepath.Join(request.Folder, activationTrustName),
			State:       filepath.Join(request.Folder, activationStateName),
			Author:      request.Author,
			Device:      request.Device,
			Authority:   request.Authority,
		}
		if request.Authority != "" {
			policy.Admissions = filepath.Join(request.Folder, activationAdmissionsName)
		}
		encoded, err := operationguard.EncodePolicy(policy)
		if err != nil {
			return OperationResult{State: Failed, Reason: operationguard.ErrUnavailable.Error()}
		}
		for _, file := range []struct {
			name string
			data []byte
		}{{activationEntitlementName, data}, {activationTrustName, trustData}, {activationPolicyName, encoded}} {
			if err = createPrivateFile(filepath.Join(request.Folder, file.name), file.data); err != nil {
				return privateWriteRefusal(request.Folder, err).operation()
			}
		}
		if err = a.retainOperationPolicy(filepath.Join(request.Folder, activationPolicyName)); err != nil {
			return OperationResult{State: Failed, Reason: errRetainedSelection.Error()}
		}
		return OperationResult{State: Completed, Selected: true, Reason: "the local activation is created; activate it to admit licensed work"}
	})
}

func (r refusal) operation() OperationResult {
	return OperationResult{State: r.state, Reason: r.reason}
}

// RenewLicenseDocument installs one later issue of the installed entitlement,
// chosen through a native file dialog and verified against the same trust
// store. It refuses a transfer — a reissue that no longer assigns this
// device to this author, or no longer names the configured runner authority —
// and never rewrites the retained clock state.
func (a *App) RenewLicenseDocument() OperationResult {
	return run(a, true, false, func(ctx context.Context) OperationResult {
		_, path := a.selectedOperation()
		if path == "" {
			return OperationResult{State: Failed, Reason: operationguard.ErrUnavailable.Error()}
		}
		if a.installedSelected(path) {
			return a.renewSelectedInstalled(ctx, path)
		}
		policy, err := readSelectedPolicy(path)
		if err != nil {
			return OperationResult{State: Failed, Reason: operationguard.ErrUnavailable.Error()}
		}
		files, declined := a.chooseFiles(ctx, "Choose the later-issue entitlement document", "JSON documents", "*.json")
		if len(files) == 0 {
			return declined.operation()
		}
		if !filepath.IsAbs(files[0]) {
			return OperationResult{State: Failed, Reason: "select an absolute path for the received document"}
		}
		data, trustData, err := operationguard.ReadDocuments(files[0], policy.Trust)
		if err != nil {
			return OperationResult{State: Failed, Reason: operationguard.ErrUnavailable.Error()}
		}
		received, err := operationguard.VerifyDocuments(data, trustData, time.Now().UTC().Truncate(time.Second))
		if err != nil {
			return OperationResult{State: Failed, Reason: err.Error()}
		}
		if !received.OperationCapable {
			return OperationResult{State: Failed, Reason: "a later issue must be a license in the current format"}
		}
		installed, err := readOperationFile(policy.Entitlement)
		if err != nil {
			return OperationResult{State: Failed, Reason: operationguard.ErrUnavailable.Error()}
		}
		current, err := operationguard.DecodeInstalled(installed)
		if err != nil {
			return OperationResult{State: Failed, Reason: err.Error()}
		}
		if received.Organization != current.Organization {
			return OperationResult{State: Failed, Reason: operationguard.ErrDifferentOrganization.Error()}
		}
		// A released activation is not renewed in place: a new explicitly
		// created activation is the separate record the contract promises.
		if state, stateErr := operationguard.Read(path); stateErr == nil {
			if state.Released {
				return OperationResult{State: Failed, Reason: "this local activation was released; create a new activation folder for the new document"}
			}
			if received.Sequence <= state.Sequence {
				return OperationResult{State: Failed, Reason: operationguard.ErrSuperseded.Error()}
			}
		} else if !errors.Is(stateErr, operationguard.ErrUnavailable) {
			return OperationResult{State: Failed, Reason: stateErr.Error()}
		}
		if received.Sequence <= current.Sequence {
			return OperationResult{State: Failed, Reason: operationguard.ErrSuperseded.Error()}
		}
		if policy.Author != "" {
			if err = received.Assigned(policy.Author, policy.Device); err != nil {
				return OperationResult{State: Failed, Reason: err.Error()}
			}
		}
		if policy.Authority != "" {
			if _, err = received.RunnerAuthority(policy.Authority); err != nil {
				return OperationResult{State: Failed, Reason: err.Error()}
			}
		}
		name := "entitlement-" + received.ID + "-" + strconv.Itoa(received.Sequence) + ".json"
		destination := filepath.Join(filepath.Dir(policy.Entitlement), name)
		if existing, readErr := readOperationFile(destination); readErr == nil {
			// A retry of the same interrupted renewal may find the document
			// already written; only the exact same issue continues.
			if !slices.Equal(existing, data) {
				return OperationResult{State: Failed, Reason: "the activation folder already holds a different document under this name"}
			}
		} else if err = createPrivateFile(destination, data); err != nil {
			return privateWriteRefusal(filepath.Dir(policy.Entitlement), err).operation()
		}
		next := policy
		next.Entitlement = destination
		encoded, err := operationguard.EncodePolicy(next)
		if err != nil {
			return OperationResult{State: Failed, Reason: operationguard.ErrUnavailable.Error()}
		}
		if err = replacePolicyFile(path, encoded); err != nil {
			return OperationResult{State: Failed, Reason: err.Error()}
		}
		a.installOperationPolicy(path)
		if status := a.OperationStatus(); status.State == Completed {
			return status
		}
		return OperationResult{State: Completed, Selected: true, Reason: "the later issue is installed; activate it to admit licensed work"}
	})
}

// installedSelected reports whether the selected policy is this computer's
// license, whose document is renewed in place as its store is, never beside it.
func (a *App) installedSelected(path string) bool {
	return a.licenseRoot != "" && path == operationguard.InstalledPolicyIn(a.licenseRoot)
}

// renewSelectedInstalled renews this computer's license in place when it is
// the selected activation, through the same renewal `readmit license renew`
// performs, so its store and its operation policy keep naming one document.
func (a *App) renewSelectedInstalled(ctx context.Context, path string) OperationResult {
	files, declined := a.chooseFiles(ctx, "Choose the later-issue entitlement document", "JSON documents", "*.json")
	if len(files) == 0 {
		return declined.operation()
	}
	data, reason := readReceived(files[0], operationguard.ErrUnavailable.Error())
	if reason != "" {
		return OperationResult{State: Failed, Reason: reason}
	}
	if err := operationguard.RenewInstalledLicense(a.licenseRoot, data, nil); err != nil {
		return OperationResult{State: Failed, Reason: err.Error()}
	}
	a.installOperationPolicy(path)
	return a.OperationStatus()
}

// ExportLicenseDocument writes the installed entitlement document to a new
// file in a folder chosen through a native dialog, byte for byte as it was
// received, whatever the activation or term state is.
func (a *App) ExportLicenseDocument() LicenseExportResult {
	return run(a, true, false, func(ctx context.Context) LicenseExportResult {
		_, path := a.selectedOperation()
		if path == "" {
			return LicenseExportResult{State: Failed, Reason: operationguard.ErrUnavailable.Error()}
		}
		policy, err := readSelectedPolicy(path)
		if err != nil {
			return LicenseExportResult{State: Failed, Reason: operationguard.ErrUnavailable.Error()}
		}
		data, err := readOperationFile(policy.Entitlement)
		if err != nil {
			return LicenseExportResult{State: Failed, Reason: operationguard.ErrUnavailable.Error()}
		}
		document, err := operationguard.DecodeInstalled(data)
		if err != nil {
			return LicenseExportResult{State: Failed, Reason: err.Error()}
		}
		folder, declined := a.chooseFolder(ctx, "Choose the folder to export the entitlement into")
		if folder == "" {
			return declined.licenseExport()
		}
		if _, err = artifactpath.Directory(folder); err != nil {
			return LicenseExportResult{State: Failed, Reason: "choose an existing folder that is not a symbolic link"}
		}
		destination := filepath.Join(folder, document.ID+".json")
		if err = createPrivateFile(destination, data); err != nil {
			if errors.Is(err, fs.ErrExist) {
				return LicenseExportResult{State: Failed, Reason: "the destination folder already holds a file with this name; choose a different folder"}
			}
			return probeWriteFailure(folder,
				"this account cannot write into the chosen folder",
				"the entitlement could not be exported into the chosen folder").licenseExport()
		}
		return LicenseExportResult{State: Completed, Document: document.ID, Path: destination}
	})
}

func (r refusal) licenseExport() LicenseExportResult {
	return LicenseExportResult{State: r.state, Reason: r.reason}
}

// ShowRunnerAdmissions reports what the configured runner authority holds
// against the capacity the installed entitlement grants it. It decides
// nothing: a missing record says the activation has not created it.
func (a *App) ShowRunnerAdmissions() RunnerStatusResult {
	return run(a, false, false, func(context.Context) RunnerStatusResult {
		return a.runnerStatus(operationguard.ReadRunnerReport)
	})
}

// SettleRunnerAdmission releases one admission — the instance handed its
// capacity back — or reconciles it, recording that an operator established the
// instance is no longer running. Both are settlement, never new paid work.
func (a *App) SettleRunnerAdmission(request RunnerSettleRequest) RunnerStatusResult {
	return run(a, false, false, func(context.Context) RunnerStatusResult {
		settle := func(path string, at time.Time) (operationguard.RunnerReport, error) {
			return operationguard.SettleAdmission(path, request.Instance, request.Reconcile, at)
		}
		return a.runnerStatus(settle)
	})
}

// runnerStatus loads the selected policy, verifies the installed entitlement,
// applies one optional settlement, and reports the authority's capacity and
// every admission with its state at the local clock.
func (a *App) runnerStatus(settle func(path string, at time.Time) (operationguard.RunnerReport, error)) RunnerStatusResult {
	_, path := a.selectedOperation()
	if path == "" {
		return RunnerStatusResult{State: Failed, Reason: operationguard.ErrUnavailable.Error()}
	}
	policy, err := readSelectedPolicy(path)
	if err != nil {
		return RunnerStatusResult{State: Failed, Reason: operationguard.ErrUnavailable.Error()}
	}
	if policy.Admissions == "" {
		return RunnerStatusResult{State: Empty, Reason: "no runner authority is configured for this activation"}
	}
	report, err := settle(path, time.Now().UTC().Truncate(time.Second))
	if err != nil {
		return RunnerStatusResult{State: Failed, Reason: err.Error()}
	}
	status := RunnerStatusResult{
		State: Completed, Organization: report.Organization, Authority: report.Authority,
		Instances: report.Capacity.Instances, Active: report.Capacity.Active, Stale: report.Capacity.Stale, Free: report.Capacity.Free(),
	}
	for _, admission := range report.Admissions {
		status.Admissions = append(status.Admissions, RunnerAdmissionView{
			Instance: admission.Instance, Admitted: licenseTime(admission.Admitted), LeaseUntil: licenseTime(admission.LeaseUntil),
			State: string(admission.StateAt(report.At)),
		})
	}
	return status
}

// ChooseCommercialDestinations selects the operator-supplied destinations file
// through a native dialog and retains the selection locally. Selecting a file
// makes no request and contacts no service.
func (a *App) ChooseCommercialDestinations() CommercialStatusResult {
	return run(a, true, false, func(ctx context.Context) CommercialStatusResult {
		files, declined := a.chooseFiles(ctx, "Choose the commercial destinations file", "JSON documents", "*.json")
		if len(files) == 0 {
			return declined.commercial()
		}
		path := files[0]
		if !filepath.IsAbs(path) {
			return CommercialStatusResult{State: Failed, Reason: "select an absolute path for the destinations file"}
		}
		data, err := readOperationFile(path)
		if err != nil {
			return CommercialStatusResult{State: Failed, Reason: commercialUndecodable}
		}
		destinations, err := decodeCommercialDestinations(data)
		if err != nil {
			return CommercialStatusResult{State: Failed, Reason: commercialUndecodable}
		}
		a.commercialMu.Lock()
		defer a.commercialMu.Unlock()
		if a.commercialSelectionPath != "" {
			encoded, err := json.Marshal(commercialSelection{Schema: commercialSelectionSchema, Config: path})
			if err != nil || writeShellDocument(a.commercialSelectionPath, append(encoded, '\n')) != nil {
				return CommercialStatusResult{State: Failed, Reason: "cannot retain the commercial destinations selection"}
			}
		}
		a.commercialConfigPath = path
		return CommercialStatusResult{State: Completed, Environment: destinations.Environment, Portal: destinations.Portal, ConfigPath: path}
	})
}

// CommercialStatus reports the retained commercial destination. Until one is
// configured the portal is a visible prerequisite; the pane shows the
// destination and navigates only when the person deliberately chooses to.
func (a *App) CommercialStatus() CommercialStatusResult {
	return run(a, false, false, func(context.Context) CommercialStatusResult {
		a.commercialMu.Lock()
		path := a.commercialConfigPath
		a.commercialMu.Unlock()
		if path == "" {
			return CommercialStatusResult{State: Empty, Reason: commercialPrerequisite}
		}
		data, err := readOperationFile(path)
		if err != nil {
			return CommercialStatusResult{State: Empty, Reason: commercialPrerequisite}
		}
		destinations, err := decodeCommercialDestinations(data)
		if err != nil {
			return CommercialStatusResult{State: Failed, Reason: commercialUndecodable}
		}
		return CommercialStatusResult{State: Completed, Environment: destinations.Environment, Portal: destinations.Portal, ConfigPath: path}
	})
}

func (r refusal) commercial() CommercialStatusResult {
	return CommercialStatusResult{State: r.state, Reason: r.reason}
}

// decodeCommercialDestinations reads the operator-supplied destinations
// contract strictly: presence before strict decode, a closed environment
// label, and one https destination without credentials or a fragment.
func decodeCommercialDestinations(data []byte) (commercialDestinations, error) {
	var members map[string]jsontext.Value
	if json.Unmarshal(data, &members) != nil || members == nil {
		return commercialDestinations{}, errors.New("invalid destinations document")
	}
	for _, name := range []string{"schema", "environment", "portal"} {
		value, ok := members[name]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return commercialDestinations{}, errors.New("invalid destinations document")
		}
	}
	var destinations commercialDestinations
	if err := json.Unmarshal(data, &destinations, json.RejectUnknownMembers(true)); err != nil {
		return commercialDestinations{}, errors.New("invalid destinations document")
	}
	if destinations.Schema != commercialDestinationsSchema {
		return commercialDestinations{}, errors.New("unsupported destinations version")
	}
	if destinations.Environment != "sandbox" && destinations.Environment != "production" {
		return commercialDestinations{}, errors.New("environment must be sandbox or production")
	}
	if len(destinations.Portal) > 512 {
		return commercialDestinations{}, errors.New("portal destination is too long")
	}
	destination, err := url.Parse(destinations.Portal)
	if err != nil || destination.Scheme != "https" || destination.Host == "" || destination.User != nil || destination.Fragment != "" || destination.RawFragment != "" {
		return commercialDestinations{}, errors.New("portal destination must be https without credentials or a fragment")
	}
	return destinations, nil
}

// restoreCommercialSelection retains only an explicit prior destinations
// selection. Restoring reads one local file; it makes no request.
func (a *App) restoreCommercialSelection(selectionPath string) {
	a.commercialMu.Lock()
	defer a.commercialMu.Unlock()
	a.commercialSelectionPath = selectionPath
	data, err := readOperationFile(selectionPath)
	if err != nil {
		return
	}
	var selection commercialSelection
	if json.Unmarshal(data, &selection, json.RejectUnknownMembers(true)) != nil || selection.Schema != commercialSelectionSchema || !filepath.IsAbs(selection.Config) {
		return
	}
	a.commercialConfigPath = selection.Config
}
