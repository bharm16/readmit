package upgrade_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/engine"
	"github.com/bharm16/readmit/internal/project"
	"github.com/bharm16/readmit/internal/upgrade"
)

// packageName is the one package every staged candidate in these tests holds.
const packageName = "readmit-desktop_9.9.9_x64.msi"

// staged writes one candidate directory: the package bytes, and the manifest
// recording them as literal JSON rather than as a document encoded from this
// package's own types, so what is read here is the shape a separate writer
// produces. Named members replace the intact ones; a member mapped to the
// empty string is left out altogether.
func staged(t *testing.T, payload []byte, replaced map[string]string) string {
	t.Helper()
	directory := filepath.Join(t.TempDir(), "staged")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if payload != nil {
		if err := os.WriteFile(filepath.Join(directory, packageName), payload, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	sum := sha256.Sum256(payload)
	members := map[string]string{
		"schema":                  `"` + upgrade.CandidateSchema + `"`,
		"version":                 `"9.9.9"`,
		"os":                      `"` + runtime.GOOS + `"`,
		"arch":                    `"` + runtime.GOARCH + `"`,
		"signed_for_distribution": `true`,
		"packages":                `[{"name":"` + packageName + `","format":"msi","sha256":"` + hex.EncodeToString(sum[:]) + `"}]`,
	}
	order := []string{"schema", "version", "os", "arch", "signed_for_distribution", "packages"}
	for name, value := range replaced {
		if _, known := members[name]; !known {
			order = append(order, name)
		}
		members[name] = value
	}
	var written []string
	for _, name := range order {
		if members[name] != "" {
			written = append(written, `"`+name+`":`+members[name])
		}
	}
	document := "{" + strings.Join(written, ",") + "}"
	if err := os.WriteFile(filepath.Join(directory, upgrade.CandidateDocumentName), []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	return directory
}

func check(t *testing.T, directory string, projects, runs []string) (upgrade.Plan, error) {
	t.Helper()
	return upgrade.Check(context.Background(), directory, projects, runs)
}

// A staged candidate this machine can be upgraded from is read whole: the
// manifest, the package files it records, and nothing else beside them.
func TestAnIntactStagedCandidateIsReady(t *testing.T) {
	payload := []byte("a package this test does not otherwise interpret")
	plan, err := check(t, staged(t, payload, nil), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Schema != upgrade.PlanSchema || plan.State != upgrade.Ready || plan.Refusal() != nil {
		t.Fatalf("%+v", plan)
	}
	if plan.Installed != engine.Version() || plan.Candidate != "9.9.9" {
		t.Fatalf("the plan named neither build it compared: %+v", plan)
	}
	if len(plan.Staged) != 1 || plan.Staged[0].State != upgrade.Intact || plan.Staged[0].Format != "msi" {
		t.Fatalf("%+v", plan.Staged)
	}
	document, err := upgrade.Encode(plan)
	if err != nil {
		t.Fatal(err)
	}
	var again upgrade.Plan
	if err := json.Unmarshal(document, &again, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("the plan does not read back under its own contract: %v", err)
	}
	if again.State != plan.State || len(again.Staged) != 1 || again.Staged[0] != plan.Staged[0] {
		t.Fatalf("%+v", again)
	}
}

// Each of these is a candidate this release refuses to read at all: a contract
// version it does not read, or that contract written in a way its writer never
// produces.
func TestAnUnreadableCandidateIsRefusedByName(t *testing.T) {
	digest := strings.Repeat("a", 64)
	for name, unreadable := range map[string]struct {
		replaced map[string]string
		want     error
	}{
		"a later contract version":                    {map[string]string{"schema": `"readmit-desktop-package/v2"`}, upgrade.ErrUnsupportedVersion},
		"a member this contract does not hold":        {map[string]string{"channel": `"stable"`}, nil},
		"an omitted member":                           {map[string]string{"signed_for_distribution": ""}, nil},
		"a build identity no package format holds":    {map[string]string{"version": `"not a version"`}, nil},
		"no package at all":                           {map[string]string{"packages": `[]`}, nil},
		"a package named as a path":                   {map[string]string{"packages": `[{"name":"../elsewhere.msi","format":"msi","sha256":"` + digest + `"}]`}, nil},
		"a package format this release does not know": {map[string]string{"packages": `[{"name":"x.rpm","format":"rpm","sha256":"` + digest + `"}]`}, nil},
		"a digest that is not one":                    {map[string]string{"packages": `[{"name":"x.msi","format":"msi","sha256":"NOTADIGEST"}]`}, nil},
		"one package recorded twice":                  {map[string]string{"packages": `[{"name":"x.msi","format":"msi","sha256":"` + digest + `"},{"name":"x.msi","format":"msi","sha256":"` + digest + `"}]`}, nil},
		"a target this release does not build for":    {map[string]string{"arch": `"riscv64"`}, nil},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := check(t, staged(t, []byte("package"), unreadable.replaced), nil, nil)
			if err == nil {
				t.Fatal("read a candidate this release does not read")
			}
			if unreadable.want != nil && !errors.Is(err, unreadable.want) {
				t.Fatalf("reported %v rather than the version it declares", err)
			}
		})
	}
}

// A file beside the packages that the manifest does not record is how the
// wrong installer gets run, so it refuses the candidate rather than being read
// as far as it agreed.
func TestAnUnrecordedFileRefusesTheWholeCandidate(t *testing.T) {
	directory := staged(t, []byte("package"), nil)
	if err := os.WriteFile(filepath.Join(directory, "readmit-desktop_9.9.8_x64.msi"), []byte("older"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := check(t, directory, nil, nil); err == nil {
		t.Fatal("an unrecorded package was accepted beside a recorded one")
	}
}

// A package whose bytes are not the recorded bytes, and one the staged
// directory never received, are each reported as what they are and refuse the
// upgrade. Neither is reported as intact.
func TestAStagedPackageIsReportedAsWhatItIs(t *testing.T) {
	directory := staged(t, []byte("package"), nil)
	if err := os.WriteFile(filepath.Join(directory, packageName), []byte("a partly written download"), 0o600); err != nil {
		t.Fatal(err)
	}
	plan, err := check(t, directory, nil, nil)
	if err != nil || plan.Staged[0].State != upgrade.Altered || plan.State != upgrade.Refused || plan.StagedIntact() {
		t.Fatalf("%+v %v", plan, err)
	}
	if err := os.Remove(filepath.Join(directory, packageName)); err != nil {
		t.Fatal(err)
	}
	plan, err = check(t, directory, nil, nil)
	if err != nil || plan.Staged[0].State != upgrade.Absent || plan.State != upgrade.Refused {
		t.Fatalf("%+v %v", plan, err)
	}
}

// A candidate built for another machine is refused, and so is a development
// preview recording that it is not signed for distribution — whatever else is
// true of it, and without that refusal making its intact packages read as
// anything else.
func TestACandidateThisMachineIsNotUpgradedFromIsRefused(t *testing.T) {
	elsewhere := `"linux"`
	if runtime.GOOS == "linux" {
		elsewhere = `"windows"`
	}
	for name, refused := range map[string]map[string]string{
		"built for another operating system": {"os": elsewhere},
		"not signed for distribution":        {"signed_for_distribution": `false`},
	} {
		t.Run(name, func(t *testing.T) {
			plan, err := check(t, staged(t, []byte("package"), refused), nil, nil)
			if err != nil || plan.State != upgrade.Refused || plan.Refusal() == nil {
				t.Fatalf("%+v %v", plan, err)
			}
			if !plan.StagedIntact() {
				t.Fatal("a refused candidate misreported an intact package")
			}
		})
	}
}

// The identity of the build running the check is what the candidate is
// compared against, and a candidate naming that identity is not an upgrade of
// it. An unstamped build reports `dev`, which no package version carries, so
// the comparison is reached through an identity the reader accepts.
func TestTheBuildRunningTheCheckIsNotAnUpgradeOfItself(t *testing.T) {
	plan := upgrade.Plan{
		Installed: "9.9.9", Candidate: "9.9.9",
		OS: runtime.GOOS, Arch: runtime.GOARCH, SignedForDistribution: true,
	}
	if plan.Refusal() == nil {
		t.Fatal("the build already running the check was reported as an upgrade of itself")
	}
}

// A project whose documents this release reads is readable; one holding a
// document version it does not read is unreadable, and reviewing either
// changes not one byte of it.
func TestReviewingAProjectReadsItAndChangesNothing(t *testing.T) {
	directory := staged(t, []byte("package"), nil)
	root := filepath.Join(t.TempDir(), "workspace")
	if _, err := project.Create(root, project.Document{Schema: project.Schema, Settings: project.Settings{Title: "Investigation"}, InterfaceVersions: []string{"v1"}}); err != nil {
		t.Fatal(err)
	}
	before := inventory(t, root)
	plan, err := check(t, directory, []string{root}, nil)
	if err != nil || plan.State != upgrade.Ready {
		t.Fatalf("%+v %v", plan, err)
	}
	if len(plan.Retained) != 1 || plan.Retained[0] != (upgrade.Retained{Name: "workspace", Kind: upgrade.ProjectKind, State: upgrade.Readable}) {
		t.Fatalf("%+v", plan.Retained)
	}
	if err := os.WriteFile(filepath.Join(root, "later.json"), []byte(`{"schema":"readmit-index/v99"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	plan, err = check(t, directory, []string{root}, nil)
	if err != nil || plan.Retained[0].State != upgrade.Unreadable || plan.State != upgrade.Refused {
		t.Fatalf("%+v %v", plan, err)
	}
	after := inventory(t, root)
	delete(after, "later.json")
	if len(after) != len(before) {
		t.Fatal("the review added or removed a file of the project it reviewed")
	}
	for name, digest := range before {
		if after[name] != digest {
			t.Fatal("the review rewrote a file of the project it reviewed")
		}
	}
}

// A run's retained engine pin decides what a later build does with it. A spec
// contract this release does not read is `unsupported`, which is distinct from
// a directory retaining no readable pin at all: an upgrade that read one as
// the other would report evidence as damaged when it was only newer.
func TestReviewingARunSeparatesANewerPinFromADamagedOne(t *testing.T) {
	directory := staged(t, []byte("package"), nil)
	job := filepath.Join(t.TempDir(), "job")
	if err := os.MkdirAll(job, 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(pin engine.Pin) {
		t.Helper()
		raw, err := engine.Encode(pin)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(job, "engine.json"), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	current := engine.Current("readmit-test/v1")
	write(current)
	plan, err := check(t, directory, nil, []string{job})
	if err != nil || plan.Retained[0] != (upgrade.Retained{Name: "job", Kind: upgrade.RunKind, State: upgrade.Readable}) {
		t.Fatalf("%+v %v", plan, err)
	}

	write(engine.Pin{Schema: engine.Schema, Engine: "0.9.0-alpha.7", Spec: "readmit-test/v2", Profile: current.Profile})
	plan, err = check(t, directory, nil, []string{job})
	if err != nil || plan.Retained[0].State != upgrade.Unsupported || plan.State != upgrade.Refused {
		t.Fatalf("%+v %v", plan, err)
	}

	if err := os.WriteFile(filepath.Join(job, "engine.json"), []byte("{not a pin"), 0o600); err != nil {
		t.Fatal(err)
	}
	plan, err = check(t, directory, nil, []string{job})
	if err != nil || plan.Retained[0].State != upgrade.Unreadable {
		t.Fatalf("%+v %v", plan, err)
	}
}

// A cancelled check answers with the cancellation rather than with a plan over
// part of what it was given.
func TestACancelledCheckReportsNoPlan(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	root := filepath.Join(t.TempDir(), "workspace")
	if _, err := project.Create(root, project.Document{Schema: project.Schema, Settings: project.Settings{Title: "Investigation"}, InterfaceVersions: []string{"v1"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := upgrade.Check(ctx, staged(t, []byte("package"), nil), []string{root}, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled check reported %v", err)
	}
}

func inventory(t *testing.T, root string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		files[entry.Name()] = hex.EncodeToString(sum[:])
	}
	return files
}
