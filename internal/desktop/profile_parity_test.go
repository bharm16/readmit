package desktop_test

// Importing a profile package in the window is `readmit profile import`: the
// same verification and the same writer over the same bytes, accepted and
// refused for the same reasons, writing the same five documents into a new
// directory and activating nothing. Opening the profile an import wrote reads
// it with the local-profile reader, resolves it against the pack it pins and
// computes the seal `readmit profile export` verifies it against. The
// checked-in v1 package is one the v1 writer exported from the shipped
// fixtures, kept so that a later change to either reader cannot silently stop
// importing a package retained today; the identity of its canonical bytes is
// pinned.

import (
	"context"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/localprofile"
	"github.com/bharm16/readmit/internal/profilepack"
	"github.com/bharm16/readmit/internal/profilepackage"
	"github.com/bharm16/readmit/internal/profileversion"
)

// historicalPackageIdentity is the SHA-256 of the canonical bytes the
// checked-in v1 package was exported as; an import writes exactly those bytes
// as its package.json whatever line endings a checkout gave the file.
const historicalPackageIdentity = "e61c22223124301d0eeba86066f563dafd5590211a4ac35b3046eb640f012e39"

// importedDocuments is every document an import writes, package.json last.
var importedDocuments = []string{"origin.json", "pack.json", "package.json", "profile.json", "version.json"}

// importedLine is what `readmit profile import` prints when it completes.
const importedLine = "Imported verified profile metadata. No project changed, no tests repinned, no message conformance evaluated.\n"

// packageWorkspace is a workspace holding the four fixture documents and the
// package `readmit profile export` wrote from them, with a saved-test
// references index beside them that no import may touch.
func packageWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"local-profile.json", "profile-pack.json", "profile-version.json", "profile-origin.json", "profile-references.json"} {
		writeDocument(t, root, name, fixture(t, name))
	}
	at := func(name string) string { return filepath.Join(root, name) }
	if _, stderr, err := commandLine(t, "profile", "export", at("local-profile.json"), "--pack", at("profile-pack.json"),
		"--version", at("profile-version.json"), "--origin", at("profile-origin.json"), "--output", at("package.json"), "--reviewed"); err != nil {
		t.Fatalf("export the fixture package: %v %s", err, stderr)
	}
	return root
}

// importedAlike imports one package entry in the window and with the command
// line into two new directories and holds them to each other: both complete,
// the command prints exactly its line, both directories hold the same five
// documents byte for byte, and what the window reports is what those
// documents say.
func importedAlike(t *testing.T, app *desktop.App, root, entry, window, command string) desktop.ProfilePackageResult {
	t.Helper()
	result := app.ImportProfilePackage(desktop.ProfilePackageImportRequest{Workspace: root, Package: entry, Output: window})
	if result.State != desktop.Completed || result.Output != window {
		t.Fatalf("%s: the window did not import: %+v", entry, result)
	}
	stdout, stderr, err := commandLine(t, "profile", "import", filepath.Join(root, entry), "--output", filepath.Join(root, command))
	if err != nil || stderr != "" || stdout != importedLine {
		t.Fatalf("%s: the command line imported differently: %v %q %q", entry, err, stdout, stderr)
	}
	for _, dir := range []string{window, command} {
		if listed := entriesOf(t, filepath.Join(root, dir)); !slices.Equal(listed, importedDocuments) {
			t.Fatalf("%s: %s holds %v", entry, dir, listed)
		}
	}
	for _, name := range importedDocuments {
		if read(t, filepath.Join(root, window, name)) != read(t, filepath.Join(root, command, name)) {
			t.Fatalf("%s: the window's %s differs from the command line's", entry, name)
		}
	}
	written := func(name string) []byte { return mustRead(t, filepath.Join(root, command, name)) }
	seal, err := profileversion.DecodeVersion(written("version.json"))
	if err != nil {
		t.Fatal(err)
	}
	pack, err := profilepack.Decode(written("pack.json"))
	if err != nil {
		t.Fatal(err)
	}
	origin, err := profilepackage.DecodeOrigin(written("origin.json"))
	if err != nil {
		t.Fatal(err)
	}
	profile, err := localprofile.Decode(written("profile.json"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Seal == nil || *result.Seal != seal || result.Version == nil || *result.Version != seal.Profile {
		t.Fatalf("%s: the window reports the seal %+v, the import wrote %+v", entry, result.Seal, seal)
	}
	if result.Pack == nil || *result.Pack != pack.Identity || result.Dependency != pack.Identity.ID+" "+pack.Identity.Version ||
		result.Provenance == nil || !reflect.DeepEqual(*result.Provenance, pack.Provenance) {
		t.Fatalf("%s: the window reports the pack %+v %+v, the import wrote %+v", entry, result.Pack, result.Provenance, pack)
	}
	if result.Origin == nil || *result.Origin != origin || result.Rights != origin.ReviewReference {
		t.Fatalf("%s: the window reports the origin %+v, the import wrote %+v", entry, result.Origin, origin)
	}
	if result.Profile == nil || *result.Profile != profile.Identity || profile.Base.Pack != pack.Identity {
		t.Fatalf("%s: the window reports the profile %+v, the import wrote %+v", entry, result.Profile, profile.Identity)
	}
	if result.SHA256 != fileDigest(t, filepath.Join(root, entry)) {
		t.Fatalf("%s: the window names the package %s", entry, result.SHA256)
	}
	return result
}

// refusedImportAlike imports one package entry into one destination name in
// the window and with the command line, and holds both to the same refusal:
// the command reports the reason the window shows, and neither leaves the
// workspace other than it was.
func refusedImportAlike(t *testing.T, app *desktop.App, root, entry, destination string) {
	t.Helper()
	before := workspaceState(t, root)
	window := app.ImportProfilePackage(desktop.ProfilePackageImportRequest{Workspace: root, Package: entry, Output: destination})
	if window.State != desktop.Failed || window.Reason == "" || window.Seal != nil || window.Profile != nil {
		t.Fatalf("%s into %s: the window did not refuse: %+v", entry, destination, window)
	}
	_, stderr, err := commandLine(t, "profile", "import", filepath.Join(root, entry), "--output", filepath.Join(root, destination))
	if err == nil || stderr != "readmit: "+window.Reason+"\n" {
		t.Fatalf("%s into %s: the command line refused differently: %v %q, the window: %q", entry, destination, err, stderr, window.Reason)
	}
	if after := workspaceState(t, root); !maps.Equal(after, before) {
		t.Fatalf("%s into %s: a refused import changed the workspace", entry, destination)
	}
}

func TestTheWindowImportsAndRefusesTheProfilePackagesTheCommandLineDoes(t *testing.T) {
	app := workspaceApp(t)
	root := packageWorkspace(t)
	exported := read(t, filepath.Join(root, "package.json"))
	writeDocument(t, root, "historical.json", read(t, filepath.Join("testdata", "historical-profile-package-v1.json")))
	references := fileDigest(t, filepath.Join(root, "profile-references.json"))

	// A package the command line exported today and one retained before the
	// window could import it are accepted alike, and each reads back as
	// independently readable documents the command line re-exports into the
	// package that was imported.
	importedAlike(t, app, root, "package.json", "window-imported", "command-imported")
	historical := importedAlike(t, app, root, "historical.json", "window-historical", "command-historical")
	if fileDigest(t, filepath.Join(root, "window-historical", "package.json")) != historicalPackageIdentity ||
		historical.Profile.ID != "fixture-local-siu" || historical.Seal.Content.SHA256 != "e96a3350b728a78d063cf99afddeec3854d393682039ed4348fbc89057c55054" {
		t.Fatalf("the historical package reads as %+v", historical)
	}
	if read(t, filepath.Join(root, "window-imported", "profile.json")) != fixture(t, "local-profile.json") {
		t.Fatal("the imported profile is not the profile that was exported")
	}
	imported := func(name string) string { return filepath.Join(root, "window-imported", name) }
	if _, stderr, err := commandLine(t, "profile", "export", imported("profile.json"), "--pack", imported("pack.json"), "--version", imported("version.json"),
		"--origin", imported("origin.json"), "--output", filepath.Join(root, "re-exported.json"), "--reviewed"); err != nil {
		t.Fatalf("the command line cannot re-export what the window imported: %v %s", err, stderr)
	}
	if read(t, filepath.Join(root, "re-exported.json")) != exported {
		t.Fatal("re-exporting the window's import changed the package")
	}

	// A tampered package, an unsupported version and a contract either reader
	// refuses are refused alike, and nothing is written.
	for name, change := range map[string][2]string{
		"tampered-notice.json":     {"No external profile content is incorporated.", "External content is incorporated."},
		"tampered-rule.json":       {"Local medical record number", "Changed rule"},
		"tampered-seal.json":       {"e96a3350b728a78d063cf99afddeec3854d393682039ed4348fbc89057c55054", "0000000000000000000000000000000000000000000000000000000000000000"},
		"tampered-provenance.json": {"readmit fixture authors", "someone else"},
		"next-version.json":        {`"readmit-profile-package/v1"`, `"readmit-profile-package/v2"`},
		"next-pack.json":           {`"readmit-profile-pack/v1"`, `"readmit-profile-pack/v2"`},
		"unknown-member.json":      {`"schema":"readmit-profile-package/v1",`, `"schema":"readmit-profile-package/v1","payloads":[],`},
	} {
		writeDocument(t, root, name, replaceOnce(t, exported, change[0], change[1]))
		refusedImportAlike(t, app, root, name, "refused")
	}

	// A destination that is already there — the window's own import, a file,
	// or a link to a folder — is refused alike and left exactly as it was.
	writeDocument(t, root, "occupied-file", "held")
	destinations := []string{"window-imported", "command-imported", "occupied-file"}
	if err := os.Symlink(filepath.Join(root, "command-imported"), filepath.Join(root, "linked")); err == nil {
		destinations = append(destinations, "linked")
	}
	for _, destination := range destinations {
		refusedImportAlike(t, app, root, "package.json", destination)
	}

	// Nothing an import writes is activated: the saved-test pins beside the
	// packages are unchanged.
	if fileDigest(t, filepath.Join(root, "profile-references.json")) != references {
		t.Fatal("an import changed the saved-test references")
	}
}

// Cancelling an import before its directory exists writes nothing; cancelling
// it once a document is written says the directory holds an incomplete import,
// which no import resumes into, and an import into a new directory completes.
func TestACancelledProfileImportSaysWhatItLeft(t *testing.T) {
	root := packageWorkspace(t)
	request := desktop.ProfilePackageImportRequest{Workspace: root, Package: "package.json", Output: "partial"}

	early, cancel := context.WithCancel(context.Background())
	cancel()
	if result := desktop.ImportProfilePackageWithinForTest(early, request); result.State != desktop.Cancelled || result.Reason != "the import was cancelled before anything was written" || result.Seal != nil {
		t.Fatalf("a cancellation before the import began answered %+v", result)
	}
	if _, err := os.Lstat(filepath.Join(root, "partial")); !os.IsNotExist(err) {
		t.Fatal("a cancelled import created its directory")
	}

	written, stop := context.WithCancel(context.Background())
	defer stop()
	result := desktop.ImportProfilePackageWithinForTest(cancelOnceWritten{written, filepath.Join(root, "partial", "profile.json"), stop}, request)
	if result.State != desktop.Cancelled || result.Seal != nil ||
		result.Reason != "the import was cancelled after partial was created; it holds an incomplete import with no package.json, so review and remove it and import again into a new directory" {
		t.Fatalf("a cancellation after a document was written answered %+v", result)
	}
	if listed := entriesOf(t, filepath.Join(root, "partial")); slices.Contains(listed, "package.json") || !slices.Contains(listed, "profile.json") {
		t.Fatalf("the cancelled import left %v", listed)
	}
	// A cancellation that lands before the import finds its name taken
	// claims nothing that was already there.
	if occupied := desktop.ImportProfilePackageWithinForTest(early, request); occupied.State != desktop.Cancelled || occupied.Reason != "the import was cancelled before anything was written" {
		t.Fatalf("a cancellation into an occupied directory answered %+v", occupied)
	}
	app := workspaceApp(t)
	if again := app.ImportProfilePackage(request); again.State != desktop.Failed || !strings.Contains(again.Reason, "destination must be new") {
		t.Fatalf("an import resumed into the incomplete directory: %+v", again)
	}
	request.Output = "retried"
	if again := app.ImportProfilePackage(request); again.State != desktop.Completed {
		t.Fatalf("an import into a new directory after a cancellation: %+v", again)
	}
}

// cancelOnceWritten cancels the import at the first check after one of its
// documents exists, rather than wherever scheduling happens to put it.
type cancelOnceWritten struct {
	context.Context
	path   string
	cancel context.CancelFunc
}

func (c cancelOnceWritten) Err() error {
	if _, err := os.Stat(c.path); err == nil {
		c.cancel()
	}
	return c.Context.Err()
}

func TestAProfileOpenedInTheWindowCarriesTheSealTheCommandLineImported(t *testing.T) {
	app := workspaceApp(t)
	root := packageWorkspace(t)
	if _, stderr, err := commandLine(t, "profile", "import", filepath.Join(root, "package.json"), "--output", filepath.Join(root, "imported")); err != nil {
		t.Fatalf("import: %v %s", err, stderr)
	}
	writeDocument(t, root, "adt-pack.json", fixture(t, "profile-pack-adt.json"))
	writeDocument(t, root, "refused-pack.json", fixture(t, "profile-pack-refused.json"))
	writeDocument(t, root, "refused-profile.json", fixture(t, "local-profile-refused.json"))
	imported := evidenceDigest(t, filepath.Join(root, "imported"))
	sealed, err := profileversion.DecodeVersion(mustRead(t, filepath.Join(root, "imported", "version.json")))
	if err != nil {
		t.Fatal(err)
	}

	// Named beside it, or found beside it, the pinned pack resolves the
	// profile, and the seal computed from what was opened is the one the
	// import wrote.
	for _, pack := range []string{"imported/pack.json", ""} {
		opened := app.OpenProfile(root, "imported/profile.json", pack)
		if opened.State != desktop.Completed || opened.Seal == nil || *opened.Seal != sealed || opened.Resolution == nil || !opened.Resolution.Pinned {
			t.Fatalf("open with pack %q: %+v", pack, opened)
		}
		if opened.Document != read(t, filepath.Join(root, "imported", "profile.json")) {
			t.Fatalf("open with pack %q: the canonical document differs from the one imported", pack)
		}
		if slices.ContainsFunc(opened.Resolution.Findings, func(f localprofile.Finding) bool { return f.Kind == localprofile.FindingPackNotPinned }) {
			t.Fatalf("open with pack %q: %+v", pack, opened.Resolution.Findings)
		}
	}

	// The command line verifies the window's seal: a package exported from
	// the opened profile with the seal the window computed is the package
	// that was imported.
	opened := app.OpenProfile(root, "imported/profile.json", "imported/pack.json")
	seal, err := opened.Seal.Encode()
	if err != nil {
		t.Fatal(err)
	}
	writeDocument(t, root, "window-seal.json", string(seal))
	at := func(name string) string { return filepath.Join(root, name) }
	if _, stderr, err := commandLine(t, "profile", "export", at("imported/profile.json"), "--pack", at("imported/pack.json"), "--version", at("window-seal.json"),
		"--origin", at("imported/origin.json"), "--output", at("sealed.json"), "--reviewed"); err != nil {
		t.Fatalf("the command line refused the window's seal: %v %s", err, stderr)
	}
	if read(t, at("sealed.json")) != read(t, at("package.json")) {
		t.Fatal("the window's seal exported a different package")
	}

	// A pack that is not the pinned one is read for nothing, and says so.
	other := app.OpenProfile(root, "imported/profile.json", "adt-pack.json")
	if other.State != desktop.Completed || other.Resolution == nil || other.Resolution.Pinned || other.Seal == nil || *other.Seal != sealed ||
		!slices.ContainsFunc(other.Resolution.Findings, func(f localprofile.Finding) bool { return f.Kind == localprofile.FindingPackNotPinned }) {
		t.Fatalf("open against another pack: %+v", other)
	}

	// What neither the reader nor the workspace admits is refused.
	if err := os.Symlink(filepath.Join(root, "imported"), filepath.Join(root, "linked")); err != nil {
		t.Logf("no symbolic link on this platform: %v", err)
	}
	for _, refused := range []struct{ profile, pack, reason string }{
		{"missing.json", "", "the local profile must be one regular file of the open workspace"},
		{"imported/missing.json", "", "the local profile must be one regular file of the open workspace"},
		{"missing/profile.json", "", "the local profile must be one entry of the open workspace or of one of its folders"},
		{"linked/profile.json", "", "the local profile must be one entry of the open workspace or of one of its folders"},
		{"../imported/profile.json", "", "the local profile must be one entry of the open workspace or of one of its folders"},
		{"imported/profile.json/x", "", "the local profile must be named by one entry of the open workspace"},
		{"imported/profile.json", "imported/missing.json", "the profile pack must be one regular file of the open workspace"},
		{"imported/profile.json", "linked/pack.json", "the profile pack must be one entry of the open workspace or of one of its folders"},
		{"imported/profile.json", "refused-pack.json", ""},
		{"refused-profile.json", "imported/pack.json", ""},
	} {
		result := app.OpenProfile(root, refused.profile, refused.pack)
		if result.State != desktop.Failed || result.Reason == "" || result.Profile != nil || result.Seal != nil ||
			(refused.reason != "" && result.Reason != refused.reason) {
			t.Errorf("open %q with pack %q: %+v", refused.profile, refused.pack, result)
		}
	}
	if evidenceDigest(t, filepath.Join(root, "imported")) != imported {
		t.Fatal("opening the imported profile changed what was imported")
	}
}

// workspaceState is every entry under a folder by its relative path: a file by
// the SHA-256 of its bytes, a symbolic link by where it points, so a test can
// state that a refused import changed nothing, the links it was refused at
// included.
func workspaceState(t *testing.T, root string) map[string]string {
	t.Helper()
	state := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			state[relative] = "link to " + target
			return err
		}
		state[relative] = fileDigest(t, path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return state
}

// The profile panel's cancel names the operation the facade runs an import
// under, so it stops exactly that import and nothing another panel started.
func TestTheProfilePanelCancelsAnImportByTheNameItRunsUnder(t *testing.T) {
	source := read(t, filepath.Join("..", "..", "desktop", "frontend", "src", "ProfileEditor.tsx"))
	if !strings.Contains(source, `const PROFILE_IMPORT = "`+desktop.ProfileImportOperationForTest+`";`) ||
		!strings.Contains(source, "cancel(PROFILE_IMPORT)") {
		t.Fatalf("the profile panel does not cancel the import by the name %q", desktop.ProfileImportOperationForTest)
	}
}
