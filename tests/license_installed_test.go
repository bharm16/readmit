package tests

// This computer's license on the command line: installed once, used for new
// work without naming a policy, and read, renewed, exported and released by
// the same commands a script already runs against a named store, with the
// same reports and the same refusals.

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/bharm16/readmit/internal/entitlement"
	"github.com/bharm16/readmit/internal/operationguard"
)

// isolateInstalledLicense gives this test, and every executable it starts, a
// fresh account configuration directory, and returns where this computer's
// license lives inside it. It is never the developer's own.
func isolateInstalledLicense(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	root, err := operationguard.InstalledLicensePath()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(root, home) {
		t.Fatalf("the installed license is outside the isolated account: %s", root)
	}
	return root
}

// withoutPolicy runs the executable exactly as typed: no operation policy is
// named, so new work is admitted through this computer's license or not at all.
func withoutPolicy(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	cmd := exec.Command(binary, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// installableClaims is the example v2 purchase with the operation
// capabilities new work is admitted by.
func installableClaims(sequence int) entitlement.ClaimsV2 {
	claims := licenseClaimsV2()
	claims.Sequence = sequence
	claims.Capabilities = []string{"author", "execute", "hub"}
	return claims
}

// activationLine is the one report line that names when a store was
// activated, which two installations made seconds apart differ in.
var activationLine = regexp.MustCompile(`(?m)^Activation: imported \S+$`)

func TestThisComputersLicenseIsInstalledOnceAndUsedWithoutNamingIt(t *testing.T) {
	root := isolateInstalledLicense(t)
	trust := writeTrust(t, entitlement.KeyActive, time.Time{})
	file := writeEntitlementV2(t, "entitlement.json", installableClaims(1))
	project := filepath.Join(t.TempDir(), "licensed-project")

	// Nothing installed: new work is refused, reading is not, and the
	// commands that act on this computer's license say there is none.
	if _, stderr, err := withoutPolicy(t, "project", "init", "--title", "t", "--interface-version", "v1", "--output", project); err == nil || !strings.Contains(stderr, operationguard.ErrUnavailable.Error()) {
		t.Fatalf("unlicensed authoring: %v %s", err, stderr)
	}
	if stdout, _, err := withoutPolicy(t, "inspect", "../testdata/fixtures/adt-cr.hl7"); err != nil || !strings.Contains(stdout, "Messages: 1") {
		t.Fatalf("an unlicensed read was gated: %v %s", err, stdout)
	}
	for _, args := range [][]string{{"license", "show"}, {"license", "release"}, {"license", "export", "--output", filepath.Join(t.TempDir(), "x.json")}, {"license", "renew", file}} {
		if _, stderr, err := withoutPolicy(t, args...); err == nil || !strings.Contains(stderr, operationguard.ErrNoLicense.Error()) {
			t.Errorf("%v with nothing installed: %v %s", args, err, stderr)
		}
	}

	// `license import` without --output installs this computer's license:
	// the report is import's own, line for line what importing into a named
	// store reports.
	installed, stderr, err := withoutPolicy(t, "license", "import", file, "--trust", trust, "--author", "a.nguyen", "--device", "ws-0413")
	if err != nil || stderr != "" {
		t.Fatalf("install: %v %s", err, stderr)
	}
	named, stderr, err := withoutPolicy(t, "license", "import", file, "--trust", trust, "--author", "a.nguyen", "--device", "ws-0413", "--output", filepath.Join(t.TempDir(), "store"))
	if err != nil || stderr != "" {
		t.Fatalf("named import: %v %s", err, stderr)
	}
	if activationLine.ReplaceAllString(installed, "") != activationLine.ReplaceAllString(named, "") || !strings.HasPrefix(installed, "Entitlement installed: ENT-0002\n") {
		t.Fatalf("installing reports differently from importing:\n%s\n---\n%s", installed, named)
	}
	for _, name := range []string{"entitlement.json", "activation.json", "trust.json", "operation-policy.json", "clock.json"} {
		if _, err := os.Lstat(filepath.Join(root, name)); err != nil {
			t.Errorf("the installed license has no %s", name)
		}
	}
	if _, stderr, err := withoutPolicy(t, "license", "import", file, "--trust", trust, "--author", "a.nguyen", "--device", "ws-0413"); err == nil || !strings.Contains(stderr, operationguard.ErrLicenseInstalled.Error()) {
		t.Fatalf("a second installation: %v %s", err, stderr)
	}

	// `license show` without a store reports this computer's license, the
	// same report as naming its folder and trust store.
	shown, stderr, err := withoutPolicy(t, "license", "show")
	if err != nil || stderr != "" {
		t.Fatalf("show: %v %s", err, stderr)
	}
	byName, _, err := withoutPolicy(t, "license", "show", root, "--trust", filepath.Join(root, "trust.json"))
	if err != nil || shown != byName || !strings.Contains(shown, "Author: a.nguyen on ws-0413 (assigned)") {
		t.Fatalf("show without a store differs from show with it: %v\n%s\n---\n%s", err, shown, byName)
	}

	// New work is admitted without naming a policy.
	if _, stderr, err := withoutPolicy(t, "project", "init", "--title", "t", "--interface-version", "v1", "--output", project); err != nil {
		t.Fatalf("authoring under the installed license: %v %s", err, stderr)
	}
	status, stderr, err := withoutPolicy(t, "license", "operation", "status")
	if err != nil || !strings.Contains(status, `"organization":"example-hospital"`) {
		t.Fatalf("operation status of the installed license: %v %s %s", err, status, stderr)
	}

	// Renewal in place, with the store's own refusals.
	other := installableClaims(3)
	other.Organization = "another-hospital"
	transfer := installableClaims(3)
	transfer.Authors.Assignments = []entitlement.Assignment{{Author: "a.nguyen", Devices: []string{"lt-0091"}}}
	for _, c := range []struct {
		document string
		reason   error
	}{
		{file, entitlement.ErrSuperseded},
		{writeEntitlementV2(t, "other.json", other), entitlement.ErrDifferentOrganization},
		{writeEntitlementV2(t, "transfer.json", transfer), entitlement.ErrDeviceNotAssigned},
	} {
		if _, stderr, err := withoutPolicy(t, "license", "renew", c.document); err == nil || !strings.Contains(stderr, c.reason.Error()) {
			t.Errorf("renewal refused as %q: %v %s", c.reason, err, stderr)
		}
	}
	later := writeEntitlementV2(t, "later.json", installableClaims(2))
	renewed, stderr, err := withoutPolicy(t, "license", "renew", later)
	if err != nil || stderr != "" || !strings.HasPrefix(renewed, "Entitlement renewed: ENT-0002\n") || !strings.Contains(renewed, "Issue sequence: 2\n") {
		t.Fatalf("renew: %v %s\n%s", err, stderr, renewed)
	}
	if _, stderr, err := withoutPolicy(t, "project", "settings", project, "--title", "Renewed"); err != nil {
		t.Fatalf("authoring under the renewal: %v %s", err, stderr)
	}

	// Export writes the installed document byte for byte.
	exported := filepath.Join(t.TempDir(), "copy.json")
	if _, stderr, err := withoutPolicy(t, "license", "export", "--output", exported); err != nil {
		t.Fatalf("export: %v %s", err, stderr)
	}
	if want, _ := os.ReadFile(later); !bytes.Equal(mustRead(t, exported), want) {
		t.Fatal("the export is not the installed document's bytes")
	}

	// Releasing this computer stops new work here too, and everything that
	// reads or exports goes on.
	released, stderr, err := withoutPolicy(t, "license", "release")
	if err != nil || stderr != "" || !strings.HasPrefix(released, "Activation released: a.nguyen on ws-0413\n") {
		t.Fatalf("release: %v %s\n%s", err, stderr, released)
	}
	if _, stderr, err := withoutPolicy(t, "project", "settings", project, "--title", "After release"); err == nil || !strings.Contains(stderr, entitlement.ErrReleased.Error()) {
		t.Fatalf("authoring after release: %v %s", err, stderr)
	}
	for _, args := range [][]string{{"license", "show"}, {"license", "release"}, {"license", "renew", writeEntitlementV2(t, "fourth.json", installableClaims(4))}} {
		if _, stderr, err := withoutPolicy(t, args...); err == nil || !strings.Contains(stderr, entitlement.ErrReleased.Error()) {
			t.Errorf("%v after release: %v %s", args, err, stderr)
		}
	}
	if _, stderr, err := withoutPolicy(t, "license", "export", "--output", filepath.Join(t.TempDir(), "kept.json")); err != nil {
		t.Fatalf("a released license did not export: %v %s", err, stderr)
	}
	if stdout, _, err := withoutPolicy(t, "project", "show", project); err != nil || !strings.HasPrefix(stdout, "Project: Renewed\n") {
		t.Fatalf("reading after release: %v %s", err, stdout)
	}

	// The reissue for this device installs again; the released license is
	// set aside beside it, never deleted.
	reissue := writeEntitlementV2(t, "reissue.json", installableClaims(5))
	if _, stderr, err := withoutPolicy(t, "license", "import", reissue, "--trust", trust, "--author", "b.okafor", "--device", "ws-0512"); err != nil {
		t.Fatalf("reinstall after release: %v %s", err, stderr)
	}
	if aside, _ := filepath.Glob(root + ".released-*"); len(aside) != 1 {
		t.Fatalf("the released license was not set aside: %v", aside)
	}
	if stdout, _, err := withoutPolicy(t, "license", "show"); err != nil || !strings.Contains(stdout, "Author: b.okafor on ws-0512 (assigned)") {
		t.Fatalf("show after reinstall: %v %s", err, stdout)
	}
}

// A named store keeps every flag, report and exit status it had; naming this
// computer's license by its folder acts on it the way the unnamed form does,
// so its operation state follows a release.
func TestNamingThisComputersLicenseFolderReleasesItsOperationStateToo(t *testing.T) {
	root := isolateInstalledLicense(t)
	trust := writeTrust(t, entitlement.KeyActive, time.Time{})
	file := writeEntitlementV2(t, "entitlement.json", installableClaims(1))
	if _, stderr, err := withoutPolicy(t, "license", "import", file, "--trust", trust, "--author", "a.nguyen", "--device", "ws-0413"); err != nil {
		t.Fatalf("install: %v %s", err, stderr)
	}
	if _, stderr, err := withoutPolicy(t, "license", "release", root); err != nil {
		t.Fatalf("release by folder: %v %s", err, stderr)
	}
	if _, stderr, err := withoutPolicy(t, "project", "init", "--title", "t", "--interface-version", "v1", "--output", filepath.Join(t.TempDir(), "p")); err == nil || !strings.Contains(stderr, entitlement.ErrReleased.Error()) {
		t.Fatalf("authoring after releasing the folder by name: %v %s", err, stderr)
	}
	// The usage refusals of the named forms are unchanged.
	if _, stderr, err := withoutPolicy(t, "license", "show", root, "extra"); err == nil || exitCode(t, err) != exitRefused {
		t.Fatalf("two stores: %v %s", err, stderr)
	}
	if _, stderr, err := withoutPolicy(t, "license", "export", root); err == nil || exitCode(t, err) != exitRefused || !strings.Contains(stderr, "requires --output with a new file") {
		t.Fatalf("export without output: %v %s", err, stderr)
	}
}

// A license in the earlier format installs as this computer's license for the
// device it binds, reports as it always has, and admits no new work: v1 is
// never read as a named-author grant.
func TestAnEarlierFormatLicenseInstallsAndAdmitsNoNewWork(t *testing.T) {
	isolateInstalledLicense(t)
	trust := writeTrust(t, entitlement.KeyActive, time.Time{})
	file := writeEntitlement(t, "v1.json", licenseClaims())
	if _, stderr, err := withoutPolicy(t, "license", "import", file, "--trust", trust, "--author", "a.nguyen", "--device", "ws-0413"); err == nil || !strings.Contains(stderr, "--author applies to a v2 entitlement") {
		t.Fatalf("an author for a v1 license: %v %s", err, stderr)
	}
	installed, stderr, err := withoutPolicy(t, "license", "import", file, "--trust", trust, "--device", "ws-0413")
	if err != nil || stderr != "" || !strings.Contains(installed, "Document: readmit-entitlement/v1\n") {
		t.Fatalf("install: %v %s\n%s", err, stderr, installed)
	}
	if shown, _, err := withoutPolicy(t, "license", "show"); err != nil || !strings.Contains(shown, "Device: ws-0413 (seat)\n") {
		t.Fatalf("show: %v %s", err, shown)
	}
	if _, stderr, err := withoutPolicy(t, "project", "init", "--title", "t", "--interface-version", "v1", "--output", filepath.Join(t.TempDir(), "p")); err == nil || !strings.Contains(stderr, operationguard.ErrUnavailable.Error()) {
		t.Fatalf("a v1 license admitted new work: %v %s", err, stderr)
	}
}
