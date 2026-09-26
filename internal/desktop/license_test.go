package desktop_test

// The license and commercial journey of the operation-access pane: verifying a
// received document, creating the local activation configuration without
// hand-authored JSON, renewing, exporting, settling runner capacity, and the
// customer-facing commercial destination. Every test asserts the refusals the
// contracts promise, and that the pane's operations stay license management:
// they never require an existing activation and never touch evidence.

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/entitlement"
	"github.com/bharm16/readmit/internal/operationguard"
)

// signing stands in for the vendor's issuer: a test-only key pair and the
// signed documents and trust stores it produces. No production key exists here.
type signing struct {
	public  ed25519.PublicKey
	private ed25519.PrivateKey
	trust   entitlement.Trust
}

func TestChooseLicenseFolderChecksTheNativeChoiceWithoutCreatingAnActivation(t *testing.T) {
	folder := t.TempDir()
	app := freshApp(t, &queueChooser{folders: []string{folder}}, "")
	if chosen := app.ChooseLicenseFolder(); chosen.State != desktop.Completed || chosen.Folder != folder {
		t.Fatalf("existing folder not chosen: %+v", chosen)
	}
	if entries, err := os.ReadDir(folder); err != nil || len(entries) != 0 {
		t.Fatalf("choosing wrote activation files: %v %v", entries, err)
	}
	link := filepath.Join(t.TempDir(), "activation-link")
	if err := os.Symlink(folder, link); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
	if chosen := freshApp(t, &queueChooser{folders: []string{link}}, "").ChooseLicenseFolder(); chosen.State != desktop.Failed || chosen.Folder != "" {
		t.Fatalf("symbolic-link folder chosen: %+v", chosen)
	}
	if chosen := freshApp(t, &queueChooser{}, "").ChooseLicenseFolder(); chosen.State != desktop.Cancelled || chosen.Folder != "" {
		t.Fatalf("cancelled native choice selected a folder: %+v", chosen)
	}
}

func TestChooseOperationPolicyAndClockResolutionReachTheCommandLineReaders(t *testing.T) {
	signer := newSigning(t)
	document := signer.document(t, claims(1, time.Now().UTC().Add(30*24*time.Hour)))
	entitlementPath, trustPath := writeReceived(t, "received", document, signer.trustBytes(t))
	folder := t.TempDir()
	selection := filepath.Join(t.TempDir(), "operations.json")
	creator := freshApp(t, &queueChooser{}, selection)
	if created := creator.CreateLicenseActivation(desktop.LicenseActivationRequest{
		Entitlement: entitlementPath, Trust: trustPath, Author: "alice", Device: "laptop", Folder: folder,
	}); created.State != desktop.Completed {
		t.Fatal(created)
	}
	if activated := creator.ActivateOperations(); activated.State != desktop.Completed {
		t.Fatal(activated)
	}
	app := freshApp(t, &queueChooser{folders: []string{folder}}, filepath.Join(t.TempDir(), "chosen.json"))
	if selected := app.ChooseOperationPolicy(); selected.State != desktop.Completed || !selected.Selected {
		t.Fatalf("native choice did not select policy: %+v", selected)
	}
	policyPath := filepath.Join(folder, "operation-policy.json")
	before, err := operationguard.Read(policyPath)
	if err != nil || app.OperationStatus().Clock == nil || *app.OperationStatus().Clock != before {
		t.Fatalf("selected policy disagrees with command-line reader: %v %+v", err, app.OperationStatus())
	}
	if declined := freshApp(t, &queueChooser{}, filepath.Join(t.TempDir(), "declined.json")).ChooseOperationPolicy(); declined.State != desktop.Cancelled {
		t.Fatalf("cancelled policy choice succeeded: %+v", declined)
	}

	policy, err := operationguard.DecodePolicy(mustRead(t, policyPath))
	if err != nil {
		t.Fatal(err)
	}
	clock := before
	clock.HighWater = time.Now().UTC().Truncate(time.Second).Add(time.Hour)
	clock.Rollback = true
	data, err := json.Marshal(clock)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(policy.State, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if refused := app.ResolveOperationClock(); refused.State != desktop.Failed || !strings.Contains(refused.Reason, "clock") {
		t.Fatalf("resolved while wall time was behind high-water: %+v", refused)
	}
	if same, err := operationguard.Read(policyPath); err != nil || !same.Rollback || !same.HighWater.Equal(clock.HighWater) {
		t.Fatalf("refused resolution changed clock: %v %+v", err, same)
	}
	clock.HighWater = time.Now().UTC().Truncate(time.Second).Add(-time.Minute)
	data, err = json.Marshal(clock)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(policy.State, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved := app.ResolveOperationClock()
	after, err := operationguard.Read(policyPath)
	if resolved.State != desktop.Completed || resolved.Clock == nil || err != nil || after.Rollback || *resolved.Clock != after || after.HighWater.Before(clock.HighWater) {
		t.Fatalf("facade resolution disagrees with command-line reader: %+v %v %+v", resolved, err, after)
	}
}

func newSigning(t *testing.T) *signing {
	t.Helper()
	public, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &signing{public: public, private: private, trust: entitlement.Trust{
		Schema: entitlement.TrustSchema,
		Keys:   []entitlement.Key{{ID: "vendor-test-2026a", Algorithm: entitlement.Algorithm, PublicKey: base64.StdEncoding.EncodeToString(public), Status: entitlement.KeyActive}},
	}}
}

func (s *signing) trustBytes(t *testing.T) []byte {
	t.Helper()
	data, err := entitlement.EncodeTrust(s.trust)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// claims are the purchased example scope every test signs variations of.
func claims(sequence int, expires time.Time) entitlement.ClaimsV2 {
	now := time.Now().UTC().Truncate(time.Second)
	return entitlement.ClaimsV2{
		ID: "ENT-0002", Organization: "example-hospital", Plan: "example-plan",
		Sequence: sequence, Issued: now.Add(-time.Hour), NotBefore: now.Add(-time.Hour), Expires: expires.Truncate(time.Second), GraceDays: 14,
		Authors: entitlement.Authors{Seats: 2, DevicesPerSeat: 2, Assignments: []entitlement.Assignment{
			{Author: "alice", Devices: []string{"desk", "laptop"}},
			{Author: "bob", Devices: []string{"laptop"}},
		}},
		Runners:      entitlement.Runners{Instances: 3, Authorities: []entitlement.Authority{{ID: "ci-pool", Instances: 1}, {ID: "local-runner", Instances: 2}}},
		Capabilities: []string{"author", "execute", "hub"},
	}
}

func (s *signing) document(t *testing.T, c entitlement.ClaimsV2) []byte {
	t.Helper()
	signed, err := entitlement.SignV2(c, s.trust.Keys[0].ID, s.private)
	if err != nil {
		t.Fatal(err)
	}
	data, err := entitlement.EncodeV2(signed)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// writeReceived places a received entitlement and trust document on disk the
// way the vendor's delivery channel does, and returns their absolute paths.
func writeReceived(t *testing.T, name string, document, trust []byte) (string, string) {
	t.Helper()
	dir := t.TempDir()
	entitlementPath := filepath.Join(dir, name+"-entitlement.json")
	trustPath := filepath.Join(dir, name+"-trust.json")
	if err := os.WriteFile(entitlementPath, document, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(trustPath, trust, 0o600); err != nil {
		t.Fatal(err)
	}
	return entitlementPath, trustPath
}

// queueChooser answers each native dialog in order: one folder per ChooseFolder
// call and one file batch per ChooseFiles call, so a multi-dialog journey is
// driven deterministically.
type queueChooser struct {
	folders []string
	batches [][]string
	err     error
}

func (c *queueChooser) ChooseFolder(string) (string, error) {
	if len(c.folders) == 0 {
		return "", nil
	}
	next := c.folders[0]
	c.folders = c.folders[1:]
	return next, c.err
}
func (c *queueChooser) ChooseFiles(string, string, string) ([]string, error) {
	if len(c.batches) == 0 {
		return nil, nil
	}
	next := c.batches[0]
	c.batches = c.batches[1:]
	return next, c.err
}

// freshApp wires a shell over fresh state files; a selection path also wires
// the retained commercial destination selection that lives beside it.
func freshApp(t *testing.T, chooser desktop.FolderChooser, selection string) *desktop.App {
	t.Helper()
	state := t.TempDir()
	if selection != "" {
		state = filepath.Dir(selection)
	}
	return desktop.NewWithOperationSelection(chooser, desktop.ShellDocuments{Folder: state})
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func mustTrust(t *testing.T, activationFolder string) entitlement.Trust {
	t.Helper()
	trust, err := entitlement.DecodeTrust(mustRead(t, filepath.Join(activationFolder, "trust.json")))
	if err != nil {
		t.Fatal(err)
	}
	return trust
}

// expireLease rewrites one admission's lease into the past, the state an
// interrupted instance's record is in once its lease ends without a release.
func expireLease(t *testing.T, activationFolder, instance string, until time.Time) {
	t.Helper()
	path := filepath.Join(activationFolder, "admissions.json")
	admissions, err := entitlement.OpenAdmissions(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := range admissions.Record.Admissions {
		if admissions.Record.Admissions[i].Instance == instance {
			admissions.Record.Admissions[i].Admitted = until.Add(-2 * time.Hour)
			admissions.Record.Admissions[i].LeaseUntil = until
		}
	}
	encoded, err := entitlement.EncodeAdmissions(admissions.Record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
}

// admitInstance records one admission directly in the authority's record, the
// way a runner host does, with the lease the caller declares.
func admitInstance(t *testing.T, activationFolder, instance string, at, until time.Time) error {
	t.Helper()
	admissions, err := entitlement.OpenAdmissions(filepath.Join(activationFolder, "admissions.json"))
	if err != nil {
		return err
	}
	grant, err := entitlement.VerifyV2(mustRead(t, filepath.Join(activationFolder, "entitlement.json")), mustTrust(t, activationFolder))
	if err != nil {
		return err
	}
	return admissions.Admit(grant, instance, at, until)
}

func TestVerifyLicenseDocumentReportsSignedFactsAndRefusesTampering(t *testing.T) {
	signer := newSigning(t)
	document := signer.document(t, claims(1, time.Now().UTC().Add(30*24*time.Hour)))
	entitlementPath, trustPath := writeReceived(t, "received", document, signer.trustBytes(t))

	app := freshApp(t, &queueChooser{batches: [][]string{{entitlementPath}, {trustPath}}}, "")
	result := app.VerifyLicenseDocument()
	if result.State != desktop.Completed || result.Document == nil {
		t.Fatal(result)
	}
	view := result.Document
	if view.Version != entitlement.SchemaV2 || view.ID != "ENT-0002" || view.Organization != "example-hospital" || view.Plan != "example-plan" || view.Sequence != 1 {
		t.Fatalf("declared facts not reported: %+v", view)
	}
	if view.State != string(entitlement.StateActive) || view.GraceDays != 14 {
		t.Fatalf("term not reported from the local clock: %+v", view)
	}
	if !view.OperationCapable || view.DevicesPerSeat != 2 || len(view.Assignments) != 2 || len(view.Authorities) != 2 || view.Seats != 2 || view.RunnerInstances != 3 {
		t.Fatalf("scope not reported: %+v", view)
	}
	if !slices.Equal(view.Capabilities, []string{"author", "execute", "hub"}) || view.KeyStatus != string(entitlement.KeyActive) {
		t.Fatalf("capabilities or key state not reported: %+v", view)
	}
	if result.Entitlement != entitlementPath || result.Trust != trustPath {
		t.Fatalf("the verified paths must be carried for the import step: %+v", result)
	}
	// The pane reports what the command line's own reader verifies: the same
	// document and trust bytes answer both.
	grant, err := entitlement.VerifyV2(document, signer.trust)
	if err != nil || grant.Claims.Organization != "example-hospital" || grant.Claims.Sequence != 1 || grant.KeyID != "vendor-test-2026a" {
		t.Fatalf("command-line reader disagrees: %v %+v", err, grant)
	}

	// A tampered document is refused with the entitlement reader's own sentence.
	tampered := []byte(strings.Replace(string(document), "example-hospital", "example-hospitaM", 1))
	tamperedPath, _ := writeReceived(t, "tampered", tampered, signer.trustBytes(t))
	app = freshApp(t, &queueChooser{batches: [][]string{{tamperedPath}, {trustPath}}}, "")
	if result = app.VerifyLicenseDocument(); result.State != desktop.Failed || result.Document != nil || result.Reason != entitlement.ErrSignature.Error() {
		t.Fatalf("tampering accepted: %+v", result)
	}

	// A trust store without the signing key refuses by name, and declining the
	// first dialog cancels without concluding anything.
	other := newSigning(t)
	otherTrustStore := entitlement.Trust{Schema: entitlement.TrustSchema, Keys: []entitlement.Key{{ID: "vendor-other-2026b", Algorithm: entitlement.Algorithm, PublicKey: base64.StdEncoding.EncodeToString(other.public), Status: entitlement.KeyActive}}}
	otherTrustBytes, err := entitlement.EncodeTrust(otherTrustStore)
	if err != nil {
		t.Fatal(err)
	}
	_, otherTrust := writeReceived(t, "other-trust", document, otherTrustBytes)
	app = freshApp(t, &queueChooser{batches: [][]string{{entitlementPath}, {otherTrust}}}, "")
	if result = app.VerifyLicenseDocument(); result.State != desktop.Failed || result.Reason != entitlement.ErrUnknownKey.Error() {
		t.Fatalf("untrusted key accepted: %+v", result)
	}
	app = freshApp(t, &queueChooser{batches: [][]string{{}, {trustPath}}}, "")
	if result = app.VerifyLicenseDocument(); result.State != desktop.Cancelled || result.Document != nil {
		t.Fatalf("declined dialog concluded something: %+v", result)
	}
}

func TestVerifyLicenseDocumentReportsV1AndExpiredTermsTruthfully(t *testing.T) {
	signer := newSigning(t)
	now := time.Now().UTC().Truncate(time.Second)
	// A v1 document verifies and is reported as such, but it cannot configure
	// named-author operation admission.
	v1, err := entitlement.Sign(entitlement.Claims{
		ID: "ENT-0001", Organization: "example-hospital", Plan: "example-plan", Sequence: 1,
		Issued: now.Add(-time.Hour), NotBefore: now.Add(-time.Hour), Expires: now.Add(24 * time.Hour), GraceDays: 0,
		Scope:        entitlement.Scope{Seats: 1, Runners: 1, Devices: []entitlement.Device{{ID: "workstation-a", Kind: entitlement.KindSeat}}},
		Capabilities: []string{"replay"},
	}, signer.trust.Keys[0].ID, signer.private)
	if err != nil {
		t.Fatal(err)
	}
	v1Bytes, err := entitlement.Encode(v1)
	if err != nil {
		t.Fatal(err)
	}
	v1Path, trustPath := writeReceived(t, "v1", v1Bytes, signer.trustBytes(t))
	app := freshApp(t, &queueChooser{batches: [][]string{{v1Path}, {trustPath}}}, "")
	result := app.VerifyLicenseDocument()
	if result.State != desktop.Completed || result.Document == nil || result.Document.Version != entitlement.Schema || result.Document.OperationCapable {
		t.Fatalf("v1 not reported truthfully: %+v", result)
	}
	if len(result.Document.Devices) != 1 || result.Document.Devices[0] != "workstation-a" || result.Document.State != string(entitlement.StateActive) || result.Document.Seats != 1 || result.Document.RunnerInstances != 1 {
		t.Fatalf("v1 scope not reported: %+v", result.Document)
	}
	if created := app.CreateLicenseActivation(desktop.LicenseActivationRequest{Entitlement: v1Path, Trust: trustPath, Author: "alice", Device: "laptop", Folder: t.TempDir()}); created.State != desktop.Failed || !strings.Contains(created.Reason, "earlier format") {
		t.Fatalf("v1 configured operation admission: %+v", created)
	}

	// An expired document still verifies; the term state is the report.
	expired := claims(1, now.Add(-2*24*time.Hour))
	expired.Issued, expired.NotBefore = now.Add(-40*24*time.Hour), now.Add(-40*24*time.Hour)
	expiredPath, _ := writeReceived(t, "expired", signer.document(t, expired), signer.trustBytes(t))
	app = freshApp(t, &queueChooser{batches: [][]string{{expiredPath}, {trustPath}}}, "")
	if result = app.VerifyLicenseDocument(); result.State != desktop.Completed || result.Document.State != string(entitlement.StateGrace) {
		t.Fatalf("expired term not reported: %+v", result)
	}
	noGrace := expired
	noGrace.GraceDays = 0
	noGracePath, _ := writeReceived(t, "no-grace", signer.document(t, noGrace), signer.trustBytes(t))
	app = freshApp(t, &queueChooser{batches: [][]string{{noGracePath}, {trustPath}}}, "")
	if result = app.VerifyLicenseDocument(); result.State != desktop.Completed || result.Document.State != string(entitlement.StateExpired) {
		t.Fatalf("ended term not reported: %+v", result)
	}
}

func TestCreateLicenseActivationConfiguresWithoutHandAuthoredJSON(t *testing.T) {
	signer := newSigning(t)
	document := signer.document(t, claims(1, time.Now().UTC().Add(30*24*time.Hour)))
	entitlementPath, trustPath := writeReceived(t, "received", document, signer.trustBytes(t))
	selection := filepath.Join(t.TempDir(), "operations.json")
	app := freshApp(t, &queueChooser{}, selection)

	folder := t.TempDir()
	request := desktop.LicenseActivationRequest{Entitlement: entitlementPath, Trust: trustPath, Author: "alice", Device: "laptop", Authority: "local-runner", Folder: folder}
	created := app.CreateLicenseActivation(request)
	if created.State != desktop.Completed || !created.Selected || created.Reason == "" {
		t.Fatalf("creation not reported as an explicit next step: %+v", created)
	}
	// The configuration exists and is exactly what the shared operation flow
	// reads; no JSON was hand-authored.
	for _, name := range []string{"entitlement.json", "trust.json", "operation-policy.json"} {
		if _, err := os.Stat(filepath.Join(folder, name)); err != nil {
			t.Fatalf("activation file missing: %s (%v)", name, err)
		}
	}
	if received := mustRead(t, filepath.Join(folder, "entitlement.json")); !slices.Equal(received, document) {
		t.Fatal("the installed document is not the received bytes")
	}
	// The selection persists: a new shell restores it without re-selection.
	next := freshApp(t, &queueChooser{}, selection)
	if status := next.OperationStatus(); !status.Selected {
		t.Fatalf("selection not retained: %+v", status)
	}
	if status := next.ActivateOperations(); status.State != desktop.Completed || status.Term != string(entitlement.StateActive) {
		t.Fatalf("activation through the created configuration: %+v", status)
	}
	// Permitted work is admitted; reads never needed it.
	if denied := next.SaveNoteItem(desktop.NoteSaveRequest{Context: desktop.RequestContext{Project: "absent"}, IntentID: "absent"}); denied.State == desktop.PermissionDenied {
		t.Fatal("created activation did not admit authoring", denied)
	}
	// The runner authority record exists once activated.
	runners := next.ShowRunnerAdmissions()
	if runners.State != desktop.Completed || runners.Authority != "local-runner" || runners.Instances != 2 || runners.Free != 2 || len(runners.Admissions) != 0 {
		t.Fatalf("runner capacity not shown: %+v", runners)
	}
}

func TestCreateLicenseActivationRefusesWrongAssignmentsAndOccupiedFolders(t *testing.T) {
	signer := newSigning(t)
	document := signer.document(t, claims(1, time.Now().UTC().Add(30*24*time.Hour)))
	entitlementPath, trustPath := writeReceived(t, "received", document, signer.trustBytes(t))
	app := freshApp(t, &queueChooser{}, "")

	cases := []struct {
		name    string
		request desktop.LicenseActivationRequest
		reason  string
	}{
		{"unassigned author", desktop.LicenseActivationRequest{Entitlement: entitlementPath, Trust: trustPath, Author: "carol", Device: "laptop", Folder: t.TempDir()}, entitlement.ErrAuthorNotNamed.Error()},
		{"device of another author", desktop.LicenseActivationRequest{Entitlement: entitlementPath, Trust: trustPath, Author: "alice", Device: "unknown-device", Folder: t.TempDir()}, entitlement.ErrDeviceNotAssigned.Error()},
		{"half an author/device pair", desktop.LicenseActivationRequest{Entitlement: entitlementPath, Trust: trustPath, Author: "alice", Folder: t.TempDir()}, "select the author and the device together"},
		{"unnamed authority", desktop.LicenseActivationRequest{Entitlement: entitlementPath, Trust: trustPath, Authority: "other-pool", Folder: t.TempDir()}, entitlement.ErrAuthorityNotNamed.Error()},
		{"relative path", desktop.LicenseActivationRequest{Entitlement: "entitlement.json", Trust: trustPath, Folder: t.TempDir()}, "select absolute paths for the received documents and the activation folder"},
	}
	for _, refusal := range cases {
		if created := app.CreateLicenseActivation(refusal.request); created.State != desktop.Failed || created.Reason != refusal.reason {
			t.Fatalf("%s accepted: %+v", refusal.name, created)
		}
	}
	// The document is re-verified at creation: a file swapped after the verify
	// step, here for one another key signed, never becomes an activation.
	other := newSigning(t)
	if err := os.WriteFile(entitlementPath, other.document(t, claims(1, time.Now().UTC().Add(30*24*time.Hour))), 0o600); err != nil {
		t.Fatal(err)
	}
	if created := app.CreateLicenseActivation(desktop.LicenseActivationRequest{Entitlement: entitlementPath, Trust: trustPath, Author: "alice", Device: "laptop", Folder: t.TempDir()}); created.State != desktop.Failed || created.Reason != entitlement.ErrSignature.Error() {
		t.Fatalf("swapped document activated: %+v", created)
	}
	if err := os.WriteFile(entitlementPath, document, 0o600); err != nil {
		t.Fatal(err)
	}
	// An occupied folder is never overwritten.
	folder := t.TempDir()
	if created := app.CreateLicenseActivation(desktop.LicenseActivationRequest{Entitlement: entitlementPath, Trust: trustPath, Author: "alice", Device: "laptop", Folder: folder}); created.State != desktop.Completed {
		t.Fatal(created)
	}
	if again := app.CreateLicenseActivation(desktop.LicenseActivationRequest{Entitlement: entitlementPath, Trust: trustPath, Author: "alice", Device: "laptop", Folder: folder}); again.State != desktop.Failed || !strings.Contains(again.Reason, "already holds") {
		t.Fatalf("occupied folder overwritten: %+v", again)
	}
}

// app2clock reads the retained clock state through the facade, for assertions
// that a step left it exactly as it was.
func app2clock(t *testing.T, selection string) operationguard.State {
	t.Helper()
	status := freshApp(t, &queueChooser{}, selection).OperationStatus()
	if status.State != desktop.Completed || status.Clock == nil {
		t.Fatalf("clock state unreadable: %+v", status)
	}
	return *status.Clock
}

func TestRenewLicenseDocumentInstallsLaterIssuesAndRefusesTransfers(t *testing.T) {
	signer := newSigning(t)
	now := time.Now().UTC().Truncate(time.Second)
	document := signer.document(t, claims(1, now.Add(30*24*time.Hour)))
	entitlementPath, trustPath := writeReceived(t, "received", document, signer.trustBytes(t))
	selection := filepath.Join(t.TempDir(), "operations.json")
	app := freshApp(t, &queueChooser{}, selection)
	folder := t.TempDir()
	if created := app.CreateLicenseActivation(desktop.LicenseActivationRequest{Entitlement: entitlementPath, Trust: trustPath, Author: "alice", Device: "laptop", Authority: "ci-pool", Folder: folder}); created.State != desktop.Completed {
		t.Fatal(created.State)
	}
	if activated := app.ActivateOperations(); activated.State != desktop.Completed {
		t.Fatal(activated)
	}

	// The one approved trial extension and a paid renewal are the same action
	// here: a later issue that still assigns this author and device.
	before := app2clock(t, selection)
	later := signer.document(t, claims(2, now.Add(60*24*time.Hour)))
	laterPath, _ := writeReceived(t, "later", later, signer.trustBytes(t))
	app2 := freshApp(t, &queueChooser{batches: [][]string{{laterPath}}}, selection)
	if renewed := app2.RenewLicenseDocument(); renewed.State != desktop.Completed || renewed.Term != string(entitlement.StateActive) {
		t.Fatalf("renewal refused: %+v", renewed)
	}
	// The retained clock was carried across the renewal untouched: renewal
	// installs a document and never rewrites local time state.
	after := app2clock(t, selection)
	if after.Sequence != before.Sequence || !after.HighWater.Equal(before.HighWater) || after.Released || after.Rollback != before.Rollback {
		t.Fatalf("clock changed across renewal: %+v then %+v", before, after)
	}

	refusals := []struct {
		name     string
		sequence int
		claims   func(entitlement.ClaimsV2) entitlement.ClaimsV2
		reason   string
	}{
		{"same sequence", 2, func(c entitlement.ClaimsV2) entitlement.ClaimsV2 { return c }, entitlement.ErrSuperseded.Error()},
		{"another organization", 3, func(c entitlement.ClaimsV2) entitlement.ClaimsV2 { c.Organization = "another-hospital"; return c }, entitlement.ErrDifferentOrganization.Error()},
		{"transfer to another author", 3, func(c entitlement.ClaimsV2) entitlement.ClaimsV2 {
			c.Authors.Assignments = []entitlement.Assignment{{Author: "bob", Devices: []string{"laptop"}}}
			return c
		}, entitlement.ErrAuthorNotNamed.Error()},
		{"authority dropped", 3, func(c entitlement.ClaimsV2) entitlement.ClaimsV2 {
			c.Runners.Authorities = []entitlement.Authority{{ID: "local-runner", Instances: 3}}
			return c
		}, entitlement.ErrAuthorityNotNamed.Error()},
	}
	for _, refusal := range refusals {
		c := claims(refusal.sequence, now.Add(90*24*time.Hour))
		c = refusal.claims(c)
		refusedPath, _ := writeReceived(t, "refused", signer.document(t, c), signer.trustBytes(t))
		refused := freshApp(t, &queueChooser{batches: [][]string{{refusedPath}}}, selection)
		if result := refused.RenewLicenseDocument(); result.State != desktop.Failed || result.Reason != refusal.reason {
			t.Fatalf("%s accepted: %+v", refusal.name, result)
		}
	}
	// No renewal path exists until a policy is selected.
	if result := freshApp(t, &queueChooser{}, "").RenewLicenseDocument(); result.State != desktop.Failed {
		t.Fatalf("renewal without selection: %+v", result)
	}
	// A released activation is not renewed in place; a new activation folder is.
	if released := app2.ReleaseOperations(); released.State != desktop.Completed {
		t.Fatal(released)
	}
	again := freshApp(t, &queueChooser{batches: [][]string{{laterPath}}}, selection)
	if result := again.RenewLicenseDocument(); result.State != desktop.Failed || !strings.Contains(result.Reason, "released") {
		t.Fatalf("released activation renewed: %+v", result)
	}
}

func TestExportLicenseDocumentWritesExactBytesAndNeverOverwrites(t *testing.T) {
	signer := newSigning(t)
	document := signer.document(t, claims(1, time.Now().UTC().Add(30*24*time.Hour)))
	entitlementPath, trustPath := writeReceived(t, "received", document, signer.trustBytes(t))
	selection := filepath.Join(t.TempDir(), "operations.json")
	app := freshApp(t, &queueChooser{}, selection)
	if created := app.CreateLicenseActivation(desktop.LicenseActivationRequest{Entitlement: entitlementPath, Trust: trustPath, Author: "alice", Device: "laptop", Folder: t.TempDir()}); created.State != desktop.Completed {
		t.Fatal(created.State)
	}
	destination := t.TempDir()
	app2 := freshApp(t, &queueChooser{folders: []string{destination}}, selection)
	exported := app2.ExportLicenseDocument()
	if exported.State != desktop.Completed || exported.Path == "" || exported.Document != "ENT-0002" {
		t.Fatalf("export refused: %+v", exported)
	}
	if written := mustRead(t, exported.Path); !slices.Equal(written, document) {
		t.Fatal("the exported document is not the installed bytes")
	}
	// A second export to the same destination refuses rather than overwriting.
	app3 := freshApp(t, &queueChooser{folders: []string{destination}}, selection)
	if again := app3.ExportLicenseDocument(); again.State != desktop.Failed || !strings.Contains(again.Reason, "already holds") {
		t.Fatalf("overwrite accepted: %+v", again)
	}
	// Without a selected policy there is nothing to export.
	if result := freshApp(t, &queueChooser{folders: []string{t.TempDir()}}, "").ExportLicenseDocument(); result.State != desktop.Failed {
		t.Fatalf("export without selection: %+v", result)
	}
}

func TestExportLicenseDocumentNamesResolvedDestinationThroughFolderLink(t *testing.T) {
	signer := newSigning(t)
	document := signer.document(t, claims(1, time.Now().UTC().Add(30*24*time.Hour)))
	entitlementPath, trustPath := writeReceived(t, "received", document, signer.trustBytes(t))
	selection := filepath.Join(t.TempDir(), "operations.json")
	app := freshApp(t, &queueChooser{}, selection)
	if created := app.CreateLicenseActivation(desktop.LicenseActivationRequest{Entitlement: entitlementPath, Trust: trustPath, Author: "alice", Device: "laptop", Folder: t.TempDir()}); created.State != desktop.Completed {
		t.Fatal(created)
	}

	parent := t.TempDir()
	target := filepath.Join(parent, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(parent, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
	folder := filepath.Join(link, "exports")
	if err := os.Mkdir(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	expected := filepath.Join(resolvedTarget, "exports", "ENT-0002.json")
	exported := freshApp(t, &queueChooser{folders: []string{folder}}, selection).ExportLicenseDocument()
	if exported.State != desktop.Completed || exported.Path != expected {
		t.Fatalf("export did not name the resolved destination %q: %+v", expected, exported)
	}
	if written := mustRead(t, expected); !slices.Equal(written, document) {
		t.Fatal("the exported document did not land in the link's target byte for byte")
	}
	if refused := freshApp(t, &queueChooser{folders: []string{link}}, selection).ExportLicenseDocument(); refused.State != desktop.Failed || refused.Path != "" {
		t.Fatalf("a chosen folder that is itself a symlink was accepted: %+v", refused)
	}
}

func TestRunnerAdmissionAdministrationShowsAndSettles(t *testing.T) {
	signer := newSigning(t)
	document := signer.document(t, claims(1, time.Now().UTC().Add(30*24*time.Hour)))
	entitlementPath, trustPath := writeReceived(t, "received", document, signer.trustBytes(t))
	selection := filepath.Join(t.TempDir(), "operations.json")
	app := freshApp(t, &queueChooser{}, selection)
	folder := t.TempDir()
	if created := app.CreateLicenseActivation(desktop.LicenseActivationRequest{Entitlement: entitlementPath, Trust: trustPath, Authority: "local-runner", Folder: folder}); created.State != desktop.Completed {
		t.Fatal(created.State)
	}
	if activated := app.ActivateOperations(); activated.State != desktop.Completed {
		t.Fatal(activated)
	}
	// The authority's granted capacity is reported before anything runs.
	runners := app.ShowRunnerAdmissions()
	if runners.State != desktop.Completed || runners.Organization != "example-hospital" || runners.Authority != "local-runner" || runners.Instances != 2 || runners.Free != 2 || len(runners.Admissions) != 0 {
		t.Fatalf("capacity not shown: %+v", runners)
	}
	// A runner host admits two instances; one goes stale when its lease ends
	// without a release. An operator settles a stale instance explicitly; an
	// unknown instance and a second settlement of the same one refuse by name.
	now := time.Now().UTC().Truncate(time.Second)
	if err := admitInstance(t, folder, "build-4821", now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := admitInstance(t, folder, "build-4822", now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	expireLease(t, folder, "build-4822", now.Add(-time.Minute))
	app2 := freshApp(t, &queueChooser{}, selection)
	runners = app2.ShowRunnerAdmissions()
	if runners.State != desktop.Completed || runners.Active != 1 || runners.Stale != 1 || runners.Free != 0 || len(runners.Admissions) != 2 {
		t.Fatalf("admissions not shown with their states: %+v", runners)
	}
	if settled := app2.SettleRunnerAdmission(desktop.RunnerSettleRequest{Instance: "build-4822", Reconcile: true}); settled.State != desktop.Completed || settled.Stale != 0 {
		t.Fatalf("stale instance not reconciled: %+v", settled)
	}
	if settled := app2.SettleRunnerAdmission(desktop.RunnerSettleRequest{Instance: "missing-instance"}); settled.State != desktop.Failed || settled.Reason != entitlement.ErrInstanceUnknown.Error() {
		t.Fatalf("unknown instance settled: %+v", settled)
	}
	if settled := app2.SettleRunnerAdmission(desktop.RunnerSettleRequest{Instance: "build-4822", Reconcile: true}); settled.State != desktop.Failed || settled.Reason != entitlement.ErrInstanceSettled.Error() {
		t.Fatalf("double settlement accepted: %+v", settled)
	}
	if settled := app2.SettleRunnerAdmission(desktop.RunnerSettleRequest{Instance: "build-4821"}); settled.State != desktop.Completed || settled.Active != 0 {
		t.Fatalf("active instance not released: %+v", settled)
	}
}

func TestCommercialDestinationIsAVisiblePrerequisiteUntilConfigured(t *testing.T) {
	selection := filepath.Join(t.TempDir(), "operations.json")
	app := freshApp(t, &queueChooser{}, selection)
	status := app.CommercialStatus()
	if status.State != desktop.Empty || status.Portal != "" || status.Reason == "" {
		t.Fatalf("unconfigured portal not a visible prerequisite: %+v", status)
	}
	// The destinations file is operator-supplied; this application invents no
	// URL of its own. A non-https destination, an unknown member and an
	// unknown environment label each refuse.
	dir := t.TempDir()
	files := map[string]string{
		"good.json":   `{"schema":"readmit-commercial-destinations/v1","environment":"sandbox","portal":"https://sandbox-portal.example.test/checkouts"}`,
		"http.json":   `{"schema":"readmit-commercial-destinations/v1","environment":"sandbox","portal":"http://insecure.example.test"}`,
		"member.json": `{"schema":"readmit-commercial-destinations/v1","environment":"sandbox","portal":"https://sandbox-portal.example.test","extra":true}`,
		"label.json":  `{"schema":"readmit-commercial-destinations/v1","environment":"internal","portal":"https://sandbox-portal.example.test"}`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"http.json", "member.json", "label.json"} {
		refused := freshApp(t, &queueChooser{batches: [][]string{{filepath.Join(dir, name)}}}, selection)
		if result := refused.ChooseCommercialDestinations(); result.State != desktop.Failed {
			t.Fatalf("%s accepted: %+v", name, result)
		}
	}
	chosen := freshApp(t, &queueChooser{batches: [][]string{{filepath.Join(dir, "good.json")}}}, selection)
	result := chosen.ChooseCommercialDestinations()
	if result.State != desktop.Completed || result.Environment != "sandbox" || result.Portal != "https://sandbox-portal.example.test/checkouts" {
		t.Fatalf("configured destination not reported: %+v", result)
	}
	if status = chosen.CommercialStatus(); status.State != desktop.Completed || status.Portal != "https://sandbox-portal.example.test/checkouts" {
		t.Fatalf("status after configuration: %+v", status)
	}
	// The selection is restored by a later window with a local read only: no
	// request is made, and the destination is exactly what the operator wrote.
	restored := freshApp(t, &queueChooser{}, selection)
	if status = restored.CommercialStatus(); status.State != desktop.Completed || status.Portal != "https://sandbox-portal.example.test/checkouts" || status.Environment != "sandbox" {
		t.Fatalf("destination selection not restored locally: %+v", status)
	}
	// A destination file that disappears is reported as the prerequisite it
	// is, never as a working portal.
	if err := os.Remove(filepath.Join(dir, "good.json")); err != nil {
		t.Fatal(err)
	}
	if status = restored.CommercialStatus(); status.State != desktop.Empty && status.State != desktop.Failed {
		t.Fatalf("vanished destination became a working portal: %+v", status)
	}
}

func TestLicenseManagementNeverGatesEvidenceOrRequiresActivation(t *testing.T) {
	signer := newSigning(t)
	document := signer.document(t, claims(1, time.Now().UTC().Add(30*24*time.Hour)))
	entitlementPath, trustPath := writeReceived(t, "received", document, signer.trustBytes(t))
	// A shell with no activation at all still manages licenses.
	app := freshApp(t, &queueChooser{batches: [][]string{{entitlementPath}, {trustPath}}}, "")
	if result := app.VerifyLicenseDocument(); result.State != desktop.Completed {
		t.Fatal(result)
	}
	if result := app.ExportLicenseDocument(); result.State != desktop.Failed {
		t.Fatalf("export without selection is not a refusal: %+v", result)
	}
	if result := app.ShowRunnerAdmissions(); result.State != desktop.Failed && result.State != desktop.Empty {
		t.Fatalf("runner administration without selection: %+v", result)
	}
	// Reads stay free throughout.
	if result := app.OpenWorkspace(t.TempDir()); result.State == desktop.PermissionDenied {
		t.Fatal("reading evidence was gated by license management", result)
	}
	if result := app.SaveNoteItem(desktop.NoteSaveRequest{Context: desktop.RequestContext{Project: "absent"}, IntentID: "absent"}); result.State != desktop.PermissionDenied {
		t.Fatal("authoring was admitted without an activation", result)
	}
}
