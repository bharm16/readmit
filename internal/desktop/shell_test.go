package desktop_test

import (
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/project"
)

// appFile and stylesFile are the two frontend sources that decide what a person
// actually reaches with a keyboard and sees without colour. The shell contract
// below is data, so these checks only confirm the window renders that data
// rather than a second, divergent copy of it.
const (
	appFile    = "../../desktop/frontend/src/App.tsx"
	stylesFile = "../../desktop/frontend/src/styles.css"
)

func shell(t *testing.T) desktop.Shell {
	t.Helper()
	result := newApp(t, &chooser{}).Shell()
	if result.State != desktop.Completed || result.Shell == nil {
		t.Fatalf("the window has no description: %+v", result)
	}
	return *result.Shell
}

func read(t *testing.T, path string) string {
	t.Helper()
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(source)
}

// The investigation journey is open a workspace, move through what it holds,
// read the evidence, inspect one case, and check what stays on the machine.
// Focus moves in that order, which is the order the window renders.
func TestFocusOrderFollowsTheInvestigationJourney(t *testing.T) {
	journey := []string{"commands", "navigation", "evidence", "inspector", "privacy"}
	regions := shell(t).Regions
	ordered := make([]string, 0, len(regions))
	for _, region := range regions {
		if region.Label == "" {
			t.Errorf("region %q has no accessible name", region.ID)
		}
		ordered = append(ordered, region.ID)
	}
	if !slices.Equal(ordered, journey) {
		t.Fatalf("focus order %v is not the investigation journey %v", ordered, journey)
	}

	// The window renders the regions by walking that order, and declares no
	// positive tab index, so what a person tabs through is document order.
	source := read(t, appFile)
	if !strings.Contains(source, "regions.map(") {
		t.Errorf("%s does not render the regions in the order the facade declares", appFile)
	}
	for scale := 1; scale < 10; scale++ {
		if strings.Contains(source, "tabIndex={"+strconv.Itoa(scale)+"}") {
			t.Errorf("%s declares a positive tab index, which moves focus out of document order", appFile)
		}
	}
}

// A region a person cannot reach is not navigation. Every declared region is
// named by a command, and the palette that lists those commands has a key.
func TestEveryRegionIsReachableFromTheCommandPalette(t *testing.T) {
	described := shell(t)
	targeted := make(map[string]bool)
	identifiers := make(map[string]bool)
	for _, command := range described.Commands {
		if command.ID == "" || command.Title == "" {
			t.Errorf("command %+v cannot be listed or run", command)
		}
		if identifiers[command.ID] {
			t.Errorf("command %q is declared twice", command.ID)
		}
		identifiers[command.ID] = true
		if command.Region != "" {
			targeted[command.Region] = true
		}
	}
	for _, region := range described.Regions {
		if !targeted[region.ID] {
			t.Errorf("no command moves focus to region %q", region.ID)
		}
	}
	if !identifiers["command-palette"] || !identifiers["search-workspace"] {
		t.Fatalf("the palette and search are not themselves commands: %+v", described.Commands)
	}
	for _, command := range described.Commands {
		if command.ID == "command-palette" && command.Keys == "" {
			t.Fatal("the command palette has no key, so every other command needs a pointer")
		}
	}
	if command := slices.IndexFunc(described.Commands, func(c desktop.Command) bool {
		return c.Region != "" && !slices.ContainsFunc(described.Regions, func(r desktop.Region) bool { return r.ID == c.Region })
	}); command >= 0 {
		t.Fatalf("command %q moves focus to a region the window does not have", described.Commands[command].ID)
	}
}

// Colour is the one channel a person may not have. Every status the window can
// show carries its own word and its own shape, so none of them is told apart by
// hue: remove every colour and the six operation states, the three artifact
// kinds and the four registered case statuses all stay distinct.
func TestEveryStatusIsDistinguishableWithoutColour(t *testing.T) {
	described := shell(t)
	indicators := make(map[string]desktop.Indicator, len(described.Indicators))
	symbols := make(map[string]string, len(described.Indicators))
	labels := make(map[string]string, len(described.Indicators))
	for _, indicator := range described.Indicators {
		if indicator.Symbol == "" || indicator.Label == "" {
			t.Errorf("status %q has no shape or no word, so only colour tells it apart", indicator.Status)
		}
		if previous, taken := symbols[indicator.Symbol]; taken {
			t.Errorf("statuses %q and %q share the shape %q", previous, indicator.Status, indicator.Symbol)
		}
		if previous, taken := labels[indicator.Label]; taken {
			t.Errorf("statuses %q and %q share the word %q", previous, indicator.Status, indicator.Label)
		}
		symbols[indicator.Symbol] = indicator.Status
		labels[indicator.Label] = indicator.Status
		indicators[indicator.Status] = indicator
	}

	for _, state := range []desktop.State{desktop.Empty, desktop.Busy, desktop.Cancelled, desktop.Failed, desktop.PermissionDenied, desktop.Completed} {
		if _, declared := indicators[string(state)]; !declared {
			t.Errorf("operation state %q has no non-colour indicator", state)
		}
	}
	for _, kind := range []desktop.Kind{desktop.CaseArtifact, desktop.ProjectArtifact, desktop.UnsupportedArtifact} {
		if _, declared := indicators[string(kind)]; !declared {
			t.Errorf("artifact kind %q has no non-colour indicator", kind)
		}
	}
	for _, status := range []project.Status{project.StatusOpen, project.StatusInvestigating, project.StatusResolved, project.StatusClosed} {
		if _, declared := indicators[string(status)]; !declared {
			t.Errorf("registered case status %q has no non-colour indicator", status)
		}
	}

	// The window draws both channels. A shape alone depends on a font having
	// the glyph; the word does not, so the word is never the one left out.
	source := read(t, appFile)
	if !strings.Contains(source, ".symbol") || !strings.Contains(source, ".label") {
		t.Errorf("%s does not render the shape and the word of a status", appFile)
	}
}

// Text has to scale and the window has to follow the light and dark the person
// chose. Both are offered as declared choices rather than a single fixed one.
func TestTextScalesAndThemesAreOfferedAsChoices(t *testing.T) {
	described := shell(t)
	if !slices.Contains(described.Themes, "system") || !slices.Contains(described.Themes, "light") || !slices.Contains(described.Themes, "dark") {
		t.Fatalf("the window does not offer system, light and dark: %v", described.Themes)
	}
	if described.TextScales[0] != 100 {
		t.Fatalf("text scaling does not start at the unscaled size: %v", described.TextScales)
	}
	if !slices.IsSorted(described.TextScales) {
		t.Fatalf("text scales are not offered in order: %v", described.TextScales)
	}
	// Readable at double size is the requirement the shell has to meet, not a
	// nominal step that stops short of it.
	if described.TextScales[len(described.TextScales)-1] < 200 {
		t.Fatalf("text does not scale to twice its size: %v", described.TextScales)
	}

	styles := read(t, stylesFile)
	for _, token := range []string{"--text-scale", `[data-theme="light"]`, `[data-theme="dark"]`, "prefers-color-scheme"} {
		if !strings.Contains(styles, token) {
			t.Errorf("%s carries no %s, so a declared choice changes nothing", stylesFile, token)
		}
	}
}

// The panes are resizable with a keyboard, not only with a pointer. The
// separator is focusable and reports its position, which is what a person
// without a pointing device and an assistive technology both read.
func TestThePaneSeparatorIsOperableWithAKeyboard(t *testing.T) {
	source := read(t, appFile)
	for _, required := range []string{`role="separator"`, "aria-valuenow", "aria-valuemin", "aria-valuemax", "ArrowLeft", "ArrowRight", "tabIndex={0}"} {
		if !strings.Contains(source, required) {
			t.Errorf("%s gives the pane separator no %s, so the panes resize only with a pointer", appFile, required)
		}
	}
}

// The privacy status is a product fact made visible, so it has to be true. It
// names what is absent and everything that is written outside evidence.
func TestPrivacyStatusNamesWhatIsAbsentAndWhatIsKept(t *testing.T) {
	privacy := shell(t).Privacy
	if privacy.Statement == "" {
		t.Fatal("the window states nothing about what leaves the machine")
	}
	absent := strings.ToLower(strings.Join(privacy.Absent, "\n"))
	for _, claim := range []string{"telemetry", "crash", "update", "analytics"} {
		if !strings.Contains(absent, claim) {
			t.Errorf("the privacy status does not say %q is absent", claim)
		}
	}
	if len(privacy.Kept) == 0 {
		t.Fatal("the privacy status claims nothing is kept, but the recent folder list is")
	}
	kept := strings.ToLower(strings.Join(privacy.Kept, "\n"))
	if !strings.Contains(kept, "readmit-desktop-recent/v1") {
		t.Errorf("the privacy status does not name the one file the shell keeps: %v", privacy.Kept)
	}
}

// What the window claims about privacy has to hold in the sources that would
// break it. Nothing in the interface reaches a network or a browser store, so
// no evidence can be sent anywhere or left behind in the webview.
func TestTheInterfaceReachesNoNetworkAndNoBrowserStorage(t *testing.T) {
	entries, err := os.ReadDir("../../desktop/frontend/src")
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		found++
		source := read(t, "../../desktop/frontend/src/"+entry.Name())
		for _, egress := range []string{"fetch(", "XMLHttpRequest", "sendBeacon", "WebSocket", "EventSource", "localStorage", "sessionStorage", "indexedDB", "document.cookie", "https://", "http://"} {
			if strings.Contains(source, egress) {
				t.Errorf("%s uses %s, which the privacy status says the shell never does", entry.Name(), egress)
			}
		}
	}
	if found == 0 {
		t.Fatal("no frontend sources were checked")
	}
}

// The window's vocabulary is declared once, in Go. The bindings repeat every
// region, command and theme as a closed type, which is what makes the interface
// fail to compile when it stops handling one of them: a region with no content,
// a command with no action, or a theme it cannot apply is a type error rather
// than a control that quietly does nothing.
func TestFrontendBindingsDeclareTheWindowsVocabulary(t *testing.T) {
	described := shell(t)
	bindings := read(t, bindingsFile)
	declared := func(kind, value string) {
		if !strings.Contains(bindings, `"`+value+`"`) {
			t.Errorf("%s %q has no typed declaration in %s", kind, value, bindingsFile)
		}
	}
	for _, region := range described.Regions {
		declared("region", region.ID)
	}
	for _, command := range described.Commands {
		declared("command", command.ID)
	}
	for _, theme := range described.Themes {
		declared("theme", theme)
	}
	for _, indicator := range described.Indicators {
		declared("status", indicator.Status)
	}
	for _, kind := range []desktop.MatchKind{desktop.ArtifactMatch, desktop.RegisteredMatch} {
		declared("match kind", string(kind))
	}
}
