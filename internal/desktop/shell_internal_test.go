package desktop

import (
	"regexp"
	"runtime/debug"
	"testing"

	"github.com/bharm16/readmit/internal/engine"
)

// About names the build it runs: the version, the revision, commit time and
// modified flag Go stamped into the executable, and the release channel. A
// build without a version-control stamp, as a test binary is, names none of
// them rather than inventing one.
func TestShellNamesItsBuild(t *testing.T) {
	described := New(nil, ShellDocuments{Folder: t.TempDir()}).Shell().Shell
	build := described.Build
	if build.Version != engine.Version() || build.Version != described.Version || build.Channel != "Development preview, unsigned" {
		t.Fatalf("the window's build: %+v", build)
	}
	if build.Revision != "" && !regexp.MustCompile(`^[0-9a-f]{40,64}$`).MatchString(build.Revision) {
		t.Fatalf("a revision that is not a commit: %q", build.Revision)
	}

	stamped := buildOf(&debug.BuildInfo{Settings: []debug.BuildSetting{
		{Key: "vcs", Value: "git"},
		{Key: "vcs.revision", Value: "3527e801aa0b9d6f2c5e1c9f0f4f8e7d6c5b4a39"},
		{Key: "vcs.time", Value: "2026-09-27T12:00:00Z"},
		{Key: "vcs.modified", Value: "true"},
	}}, true)
	if stamped.Revision != "3527e801aa0b9d6f2c5e1c9f0f4f8e7d6c5b4a39" || stamped.BuiltAt != "2026-09-27T12:00:00Z" || !stamped.Modified ||
		stamped.Version != engine.Version() || stamped.Channel != releaseChannel {
		t.Fatalf("a stamped build: %+v", stamped)
	}
	if clean := buildOf(&debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.modified", Value: "false"}}}, true); clean.Modified {
		t.Fatalf("a clean build reads modified: %+v", clean)
	}
	if unread := buildOf(nil, false); unread.Revision != "" || unread.BuiltAt != "" || unread.Modified || unread.Channel != releaseChannel {
		t.Fatalf("a build with no build information: %+v", unread)
	}
}
