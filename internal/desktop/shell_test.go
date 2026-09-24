package desktop_test

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/project"
)

// The window's focus order, statuses, commands and shortcuts are declared once,
// here in Go, and the interface renders that declaration. These tests own the
// declaration itself and confirm the interface still reads it rather than a
// second copy of its own. They read the interface sources; they do not render a
// window, so what they establish is that the declaration is coherent and that
// the interface is still wired to it. That every declared region and command
// has something on the other side is left to the frontend type check, where the
// two records are keyed by the declared identifiers.
const (
	frontendDirectory = "../../desktop/frontend/src"
	stylesFile        = frontendDirectory + "/styles.css"
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

// frontend is every interface source, so a check does not quietly stop applying
// when a part of the window moves to a file of its own.
func frontend(t *testing.T) string {
	t.Helper()
	entries, err := os.ReadDir(frontendDirectory)
	if err != nil {
		t.Fatal(err)
	}
	var sources strings.Builder
	for _, entry := range entries {
		if entry.Type().IsRegular() {
			sources.WriteString(read(t, filepath.Join(frontendDirectory, entry.Name())))
		}
	}
	if sources.Len() == 0 {
		t.Fatal("no interface sources were found")
	}
	return sources.String()
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

	source := frontend(t)
	// The window walks the declared order rather than one of its own, and every
	// declared region has an element, because the record holding them is keyed
	// by the declared identifiers and ReactElement does not admit nothing.
	if !strings.Contains(source, "regions.map(") {
		t.Error("the interface does not render the regions in the order the facade declares")
	}
	if !strings.Contains(source, "Record<RegionId, ReactElement>") {
		t.Error("region content is not keyed by the declared regions, so a region can be left out")
	}
	// No positive tab index, so the controls inside the regions are tabbed
	// through in document order, which is the order the regions are rendered.
	for index := 1; index < 10; index++ {
		if strings.Contains(source, "tabIndex={"+strconv.Itoa(index)+"}") {
			t.Errorf("the interface declares tab index %d, which moves focus out of document order", index)
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
	if command := slices.IndexFunc(described.Commands, func(c desktop.Command) bool {
		return c.Region != "" && !slices.ContainsFunc(described.Regions, func(r desktop.Region) bool { return r.ID == c.Region })
	}); command >= 0 {
		t.Fatalf("command %q moves focus to a region the window does not have", described.Commands[command].ID)
	}

	// Every command has an action behind it, for the same reason every region
	// has an element: the record is keyed by the declared identifiers.
	if !strings.Contains(frontend(t), "Record<CommandId, () => void>") {
		t.Error("command actions are not keyed by the declared commands, so a command can be left out")
	}
}

// A shortcut the window shows beside a command has to run that command. A key
// declared here and bound nowhere is a promise the window does not keep, and
// the palette prints it in full beside the command that ignores it.
func TestEveryDeclaredShortcutIsBound(t *testing.T) {
	source := strings.ToLower(frontend(t))
	bound := 0
	for _, command := range shell(t).Commands {
		if command.Keys == "" {
			continue
		}
		bound++
		// The last part of a combination is the key itself; the modifiers are
		// read from the event rather than matched by name.
		parts := strings.Split(strings.ToLower(command.Keys), "+")
		key := parts[len(parts)-1]
		// A key is bound where it is compared against, so a string literal that
		// merely shares its spelling does not satisfy this.
		if !strings.Contains(source, `case "`+key+`"`) && !strings.Contains(source, `=== "`+key+`"`) {
			t.Errorf("command %q shows the shortcut %q, but the interface tests no %q key", command.ID, command.Keys, key)
		}
	}
	if bound == 0 {
		t.Fatal("no command declares a shortcut, so nothing was checked")
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
	for _, kind := range []desktop.Kind{
		desktop.CaseArtifact, desktop.ProjectArtifact, desktop.RevisionsArtifact,
		desktop.ResultArtifact, desktop.JobArtifact, desktop.ReviewArtifact,
		desktop.IndexArtifact, desktop.TargetArtifact, desktop.RulesArtifact,
		desktop.PlanArtifact, desktop.SpecArtifact, desktop.PackArtifact,
		desktop.ProfileArtifact, desktop.PackageArtifact,
		desktop.AnalysisArtifact, desktop.SecretArtifact, desktop.PolicyArtifact,
		desktop.ResetArtifact, desktop.DiagnosisArtifact, desktop.DiagnosisGroupsArtifact, desktop.FindingReviewArtifact,
		desktop.CorrelationReviewArtifact, desktop.NormalizationArtifact,
		desktop.DiagnoseConfigArtifact, desktop.DecisionsArtifact,
		desktop.PacketArtifact, desktop.PortableReviewArtifact, desktop.SyntheticPacketArtifact, desktop.PreparedRerunArtifact,
		desktop.UnsupportedArtifact,
	} {
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
	source := frontend(t)
	if !strings.Contains(source, ".symbol") || !strings.Contains(source, ".label") {
		t.Error("the interface does not render the shape and the word of a status")
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
	source := frontend(t)
	for _, required := range []string{`role="separator"`, "aria-valuenow", "aria-valuemin", "aria-valuemax", "ArrowLeft", "ArrowRight", "tabIndex={0}"} {
		if !strings.Contains(source, required) {
			t.Errorf("the pane separator has no %s, so the panes resize only with a pointer", required)
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
	// Every document the shell writes outside evidence is named here. Two of
	// them hold what a person typed — a filter term and an unstored note — which
	// is the same patient data the evidence beside them holds, so leaving either
	// unnamed would be the reassurance this status exists to avoid.
	for _, document := range []string{"readmit-desktop-recent/v1", "readmit-filters/v1", desktop.SessionSchema, desktop.DraftsSchema, "readmit-desktop-commercial-selection/v1", "readmit-desktop-hub-selection/v1", "readmit-correlation-review/v1"} {
		if !strings.Contains(kept, strings.ToLower(document)) {
			t.Errorf("the privacy status does not name %s, which the shell keeps: %v", document, privacy.Kept)
		}
	}
}

// What the window claims about privacy has to hold in the sources that would
// break it. The interface reaches no network and no browser store, so no
// evidence can be sent anywhere or left behind in the webview, and it loads no
// remote resource, so everything it renders really is bundled.
func TestTheInterfaceReachesNoNetworkAndNoBrowserStorage(t *testing.T) {
	source := frontend(t)
	for _, egress := range []string{
		"fetch(", "XMLHttpRequest", "sendBeacon", "WebSocket", "EventSource",
		"localStorage", "sessionStorage", "indexedDB", "document.cookie",
		`src="http`, `href="http`, `from "http`, `import("http`, "@import url(http",
	} {
		if strings.Contains(source, egress) {
			t.Errorf("the interface uses %s, which the privacy status says the shell never does", egress)
		}
	}
}

// The window's vocabulary is declared once, in Go, and the bindings repeat it as
// closed types so the interface can be held to it. This test is what requires
// them to: a region, command, theme, status or match kind the facade declares
// and the bindings do not is caught here, not by the frontend build. The build
// adds only what the two records prove — an element for every region and an
// action for every command. A status declared without an indicator compiles
// either way, because the interface falls back to the plain status word, so it
// is TestEveryStatusIsDistinguishableWithoutColour that requires one.
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
