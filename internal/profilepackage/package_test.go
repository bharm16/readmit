package profilepackage_test

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"github.com/bharm16/readmit/internal/profilepack"
	"github.com/bharm16/readmit/internal/profilepackage"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, e := os.ReadFile("../../testdata/fixtures/" + name)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func origin() []byte {
	return []byte(`{"schema":"readmit-profile-origin/v1","source_format":"readmit-local-profile/v1","source":"Independent fixture","revision":"1","license":"LicenseRef-Local","notice":"Local fixture permission text","mapping_limitations":"No external mapping; local rules only","review_reference":"fixture-review"}`)
}
func TestRoundTripPreservesVersionAndProvenance(t *testing.T) {
	b, e := profilepackage.Export(fixture(t, "local-profile.json"), fixture(t, "profile-pack.json"), fixture(t, "profile-version.json"), origin())
	if e != nil {
		t.Fatal(e)
	}
	p, e := profilepackage.Decode(b)
	if e != nil {
		t.Fatal(e)
	}
	files, e := p.Documents()
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Contains(files["origin.json"], []byte("Local fixture permission text")) {
		t.Fatal("notice lost")
	}
	again, e := profilepackage.Export(files["profile.json"], files["pack.json"], files["version.json"], files["origin.json"])
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(b, again) {
		t.Fatal("round trip changed package")
	}
	if !bytes.Equal(files["profile.json"], fixture(t, "local-profile.json")) {
		t.Fatal("profile rules changed")
	}
}

func TestImportRefusesChangedAndUnsupportedContracts(t *testing.T) {
	b, e := profilepackage.Export(fixture(t, "local-profile.json"), fixture(t, "profile-pack.json"), fixture(t, "profile-version.json"), origin())
	if e != nil {
		t.Fatal(e)
	}
	b, e = json.Marshal(jsontext.Value(b), jsontext.WithIndent("  "))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = profilepackage.Decode(b); e != nil {
		t.Fatal("reformatting changed integrity:", e)
	}

	for name, pair := range map[string][2]string{
		"package version":      {"readmit-profile-package/v1", "readmit-profile-package/v2"},
		"profile version":      {"readmit-local-profile/v1", "readmit-local-profile/v2"},
		"pack version":         {"readmit-profile-pack/v1", "readmit-profile-pack/v2"},
		"seal version":         {"readmit-profile-version/v1", "readmit-profile-version/v2"},
		"origin version":       {"readmit-profile-origin/v1", "readmit-profile-origin/v2"},
		"operator":             {"value_in", "eval"},
		"provenance integrity": {"Local fixture permission text", "A changed notice"},
		"pack integrity":       {"Placer Appointment ID", "Changed label"},
		"profile integrity":    {"Local medical record number", "Changed rule"},
		"nested unknown":       {`"source_format":`, `"script": "private-sentinel", "source_format":`},
		"duplicate":            {`"sha256":`, `"sha256": "bad", "sha256":`},
		"missing origin":       {`"origin": {`, `"origin": null, "discarded": {`},
		"unknown member":       {`"schema": "readmit-profile-package/v1",`, `"schema": "readmit-profile-package/v1", "payloads": [],`},
	} {
		t.Run(name, func(t *testing.T) {
			bad := bytes.Replace(b, []byte(pair[0]), []byte(pair[1]), 1)
			if bytes.Equal(b, bad) {
				t.Fatal("ineffective mutation")
			}
			if _, err := profilepackage.Decode(bad); err == nil {
				t.Fatal("accepted altered contract")
			}
		})
	}
}
func TestExportDoesNotRepairSealsOrPins(t *testing.T) {
	profile := fixture(t, "local-profile.json")
	pack := fixture(t, "profile-pack.json")
	version := fixture(t, "profile-version.json")
	for name, p := range map[string][]byte{"changed rule": bytes.Replace(profile, []byte("Local medical record number"), []byte("Altered field"), 1), "changed pin": bytes.Replace(profile, []byte(`"fixture-siu"`), []byte(`"other-pack"`), 1)} {
		t.Run(name, func(t *testing.T) {
			if _, err := profilepackage.Export(p, pack, version, origin()); err == nil {
				t.Fatal("export repaired changed profile")
			}
		})
	}
	if _, err := profilepackage.Export(profile, fixture(t, "profile-pack-adt.json"), version, origin()); err == nil {
		t.Fatal("accepted another pack")
	}
	if _, err := (profilepackage.Package{}).Documents(); err == nil {
		t.Fatal("zero package exported")
	}
}

func TestImportCancellationRefusalAndRecovery(t *testing.T) {
	b, e := profilepackage.Export(fixture(t, "local-profile.json"), fixture(t, "profile-pack.json"), fixture(t, "profile-version.json"), origin())
	if e != nil {
		t.Fatal(e)
	}
	dst := filepath.Join(t.TempDir(), "imported")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := profilepackage.Import(ctx, dst, b); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatal("cancelled import wrote output")
	}
	bad := bytes.Replace(b, []byte("Local fixture permission text"), []byte("Changed notice"), 1)
	if err := profilepackage.Import(context.Background(), dst, bad); err == nil {
		t.Fatal("tampered package imported")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatal("refused import wrote output")
	}
	if err := profilepackage.Import(context.Background(), dst, b); err != nil {
		t.Fatal(err)
	}
	saved, e := os.ReadFile(filepath.Join(dst, "package.json"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = profilepackage.Decode(saved); e != nil {
		t.Fatal(e)
	}
	if e = profilepackage.Import(context.Background(), dst, b); e == nil {
		t.Fatal("replaced import")
	}
	// Destinations under finalized evidence, including physical symlink aliases,
	// are never used even when no existing document would be overwritten.
	evidence := t.TempDir()
	if e = os.WriteFile(filepath.Join(evidence, "identity.sha256"), []byte("retained"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = profilepackage.Import(context.Background(), filepath.Join(evidence, "profile"), b); e == nil {
		t.Fatal("wrote into immutable evidence")
	}
	if runtime.GOOS != "windows" {
		info, e := os.Stat(filepath.Join(dst, "origin.json"))
		if e != nil || info.Mode().Perm() != 0600 {
			t.Fatal("origin not private")
		}
	}
}
func TestExportRetainsExplicitUnsupportedLevelsAndManualMapping(t *testing.T) {
	o := bytes.Replace(origin(), []byte(`"source_format":"readmit-local-profile/v1"`), []byte(`"source_format":"manual-external-mapping"`), 1)
	b, e := profilepackage.Export(fixture(t, "local-profile.json"), bytes.Replace(fixture(t, "profile-pack.json"), []byte(`"approved"`), []byte(`"pending"`), 1), fixture(t, "profile-version.json"), o)
	if e != nil {
		t.Fatal(e)
	}
	p, e := profilepackage.Decode(b)
	if e != nil {
		t.Fatal(e)
	}
	files, e := p.Documents()
	if e != nil {
		t.Fatal(e)
	}
	pack, e := profilepack.Decode(files["pack.json"])
	if e != nil {
		t.Fatal(e)
	}
	if pack.Provenance.RightsReview.Status != profilepack.ReviewPending {
		t.Fatal("pending review promoted")
	}
	if pack.Support("2.5.1", "SIU", profilepack.LevelStructural).Passing() {
		t.Fatal("structural support promoted")
	}
	if !bytes.Contains(files["origin.json"], []byte("manual-external-mapping")) {
		t.Fatal("mapping provenance lost")
	}
}
func FuzzDecode(f *testing.F) {
	read := func(name string) []byte {
		data, err := os.ReadFile("../../testdata/fixtures/" + name)
		if err != nil {
			f.Fatal(err)
		}
		return data
	}
	seed, err := profilepackage.Export(read("local-profile.json"), read("profile-pack.json"), read("profile-version.json"), origin())
	if err != nil {
		f.Fatal(err)
	}
	f.Add(seed)

	f.Add([]byte(`{"schema":"readmit-profile-package/v1"}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		p, err := profilepackage.Decode(data)
		if err != nil {
			return
		}
		files, err := p.Documents()
		if err != nil {
			t.Fatal(err)
		}
		b, err := profilepackage.Export(files["profile.json"], files["pack.json"], files["version.json"], files["origin.json"])
		if err != nil {
			t.Fatal(err)
		}
		if _, err = profilepackage.Decode(b); err != nil {
			t.Fatal(err)
		}
	})
}

func TestNearLimitMetadataPackageRemainsImportable(t *testing.T) {
	pack, err := profilepack.Decode(fixture(t, "profile-pack.json"))
	if err != nil {
		t.Fatal(err)
	}
	segments := map[string]map[int]string{}
	for i := 0; i < 29; i++ {
		fields := map[int]string{}
		count := 999
		if i == 28 {
			count = 20
		}
		for j := 1; j <= count; j++ {
			fields[j] = strings.Repeat("x", 128)
		}
		segments[fmt.Sprintf("Z%c%c", 'A'+i/26, 'A'+i%26)] = fields
	}
	pack.Labels[0].Segments = segments
	b, err := json.Marshal(pack, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		t.Fatal(err)
	}
	if len(b) >= profilepack.MaxPackBytes {
		t.Fatal("boundary fixture is too large")
	}
	exported, err := profilepackage.Export(fixture(t, "local-profile.json"), b, fixture(t, "profile-version.json"), origin())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = profilepackage.Decode(exported); err != nil {
		t.Fatal("exported metadata is not importable:", err)
	}
}

// Cancellation is injected at the context boundary when a real output file
// exists, rather than relying on scheduling or counting internal calls.
type cancelAfterFile struct {
	context.Context
	path   string
	cancel context.CancelFunc
}

func (c cancelAfterFile) Err() error {
	if _, err := os.Stat(c.path); err == nil {
		c.cancel()
	}
	return c.Context.Err()
}
func TestPartialImportRemainsUnactivatedAndRetryUsesNewDestination(t *testing.T) {
	b, err := profilepackage.Export(fixture(t, "local-profile.json"), fixture(t, "profile-pack.json"), fixture(t, "profile-version.json"), origin())
	if err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	partial := filepath.Join(parent, "partial")
	fresh := filepath.Join(parent, "fresh")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err = profilepackage.Import(cancelAfterFile{ctx, filepath.Join(partial, "profile.json"), cancel}, partial, b)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("mid-import cancellation: %v", err)
	}
	if _, err = os.Stat(filepath.Join(partial, "package.json")); !os.IsNotExist(err) {
		t.Fatal("partial import has a completion record")
	}
	if err = profilepackage.Import(context.Background(), partial, b); err == nil {
		t.Fatal("resumed into partial directory")
	}
	if err = profilepackage.Import(context.Background(), fresh, b); err != nil {
		t.Fatal(err)
	}
}

// The filesystem fault is injected when the public context is checked after a
// real constituent was written; no private writer or filesystem is mocked.
type interfereAfterFile struct {
	context.Context
	trigger, obstruction string
}

func (c interfereAfterFile) Err() error {
	if _, err := os.Stat(c.trigger); err == nil {
		_ = os.Mkdir(c.obstruction, 0700)
	}
	return c.Context.Err()
}
func TestImportWriteFailureRetainsIncompleteDirectory(t *testing.T) {
	b, err := profilepackage.Export(fixture(t, "local-profile.json"), fixture(t, "profile-pack.json"), fixture(t, "profile-version.json"), origin())
	if err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	partial := filepath.Join(parent, "partial")
	ctx := interfereAfterFile{context.Background(), filepath.Join(partial, "profile.json"), filepath.Join(partial, "pack.json")}
	if err = profilepackage.Import(ctx, partial, b); err == nil {
		t.Fatal("write obstruction ignored")
	}
	if _, err = os.Stat(filepath.Join(partial, "package.json")); !os.IsNotExist(err) {
		t.Fatal("failed import has completion record")
	}
	if err = profilepackage.Import(context.Background(), partial, b); err == nil {
		t.Fatal("reused incomplete import")
	}
	if err = profilepackage.Import(context.Background(), filepath.Join(parent, "retry"), b); err != nil {
		t.Fatal(err)
	}
}
