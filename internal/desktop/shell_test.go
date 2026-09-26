package desktop_test

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/bharm16/readmit/internal/collection"
	"github.com/bharm16/readmit/internal/desktop"
	"github.com/bharm16/readmit/internal/fixturereset"
	"github.com/bharm16/readmit/internal/grid"
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
	// generatedBindings is what desktop/bindgen writes from the Go types.
	generatedBindings = frontendDirectory + "/bindings.gen.ts"
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

// Focus moves through the regions in the order they sit in the window: search
// and navigation down the sidebar, the page, the details of an open message,
// and the status line. That is the order the window renders.
func TestFocusOrderFollowsTheInvestigationJourney(t *testing.T) {
	journey := []desktop.RegionID{"commands", "navigation", "evidence", "inspector", "privacy"}
	regions := shell(t).Regions
	ordered := make([]desktop.RegionID, 0, len(regions))
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
	// The window places every declared region, in the declared order, and
	// every declared region has an element, because the record holding them
	// is keyed by the declared identifiers and ReactElement does not admit
	// nothing.
	placed := -1
	for _, region := range regions {
		at := strings.Index(source, `region("`+string(region.ID)+`"`)
		if at < 0 {
			t.Errorf("the interface does not place region %q", region.ID)
			continue
		}
		if at < placed {
			t.Errorf("the interface places region %q out of the declared order", region.ID)
		}
		placed = at
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
	targeted := make(map[desktop.RegionID]bool)
	identifiers := make(map[desktop.CommandID]bool)
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
	for _, token := range []string{"--text-scale", `[data-theme="light"]`, `[data-theme="dark"]`, "color-scheme: light dark"} {
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

// declaredUnion is the values of one union the generated bindings declare.
func declaredUnion(t *testing.T, bindings, name string) []string {
	t.Helper()
	declaration := regexp.MustCompile(`(?m)^export type ` + name + ` =([^;]*);`).FindStringSubmatch(bindings)
	if declaration == nil {
		t.Fatalf("%s declares no union %s", generatedBindings, name)
	}
	var values []string
	for _, quoted := range regexp.MustCompile(`"[^"]*"`).FindAllString(declaration[1], -1) {
		value, err := strconv.Unquote(quoted)
		if err != nil {
			t.Fatal(err)
		}
		values = append(values, value)
	}
	return values
}

// A panel's cancel names what it stops by one of the names the generated
// bindings declare for it, InterruptibleOperation, generated from the Go
// constants those operations run under. Every such name is the name of an
// interruptible operation the facade declares a profile for, so a cancel
// naming it reaches that operation, never one that cannot be interrupted or
// that runs under another name.
func TestTheWindowCancelsOnlyOperationsTheFacadeInterrupts(t *testing.T) {
	interruptible := map[string]bool{}
	for _, profile := range desktop.DeclaredProfilesForTest() {
		if profile.Interruptible {
			interruptible[profile.Name] = true
		}
	}
	for _, name := range declaredUnion(t, read(t, generatedBindings), "InterruptibleOperation") {
		if !interruptible[name] {
			t.Errorf("a cancel can name %q, which no interruptible operation of the facade runs under", name)
		}
	}
}

// The window's vocabulary is declared once, in Go, as the constants of its
// named types, and the generated bindings declare each of those types as the
// union of its constants, so the interface is held to that vocabulary and no
// other. This test holds the description the window renders to the same
// constants, in both directions: a region, command or theme described with a
// value that is not a declared constant would be missing from its union, and
// the records keyed by the union would not require an element or an action
// for it. A status is an operation state, an artifact kind or a registered case
// status, each its own union. The build adds only what the two records prove —
// an element for every region and an action for every command. A status
// declared without an indicator compiles either way, because the interface
// falls back to the plain status word, so it is
// TestEveryStatusIsDistinguishableWithoutColour that requires one.
func TestFrontendBindingsDeclareTheWindowsVocabulary(t *testing.T) {
	described := shell(t)
	bindings := read(t, generatedBindings)
	union := func(name string) []string {
		t.Helper()
		return declaredUnion(t, bindings, name)
	}
	exactly := func(kind, name string, described []string) {
		t.Helper()
		declared := union(name)
		for _, value := range described {
			if !slices.Contains(declared, value) {
				t.Errorf("%s %q is described but %s does not declare it", kind, value, name)
			}
		}
		for _, value := range declared {
			if !slices.Contains(described, value) {
				t.Errorf("%s declares %s %q, which the window does not describe", name, kind, value)
			}
		}
	}
	var regions, commands, themes []string
	for _, region := range described.Regions {
		regions = append(regions, string(region.ID))
	}
	for _, command := range described.Commands {
		commands = append(commands, string(command.ID))
	}
	for _, theme := range described.Themes {
		themes = append(themes, string(theme))
	}
	exactly("region", "RegionId", regions)
	exactly("command", "CommandId", commands)
	exactly("theme", "Theme", themes)
	statuses := slices.Concat(union("State"), union("Kind"), union("CaseStatus"))
	for _, indicator := range described.Indicators {
		if !slices.Contains(statuses, indicator.Status) {
			t.Errorf("status %q is neither an operation state, an artifact kind nor a case status the bindings declare", indicator.Status)
		}
	}
	for _, kind := range []desktop.MatchKind{desktop.ArtifactMatch, desktop.RegisteredMatch} {
		if !slices.Contains(union("MatchKind"), string(kind)) {
			t.Errorf("match kind %q has no declaration in %s", kind, generatedBindings)
		}
	}
}

// interfaceSources is every hand-written interface source: the components and
// their helpers, without the generated bindings and without the tests, whose
// fixtures state what the facade answers.
func interfaceSources(t *testing.T) string {
	t.Helper()
	entries, err := os.ReadDir(frontendDirectory)
	if err != nil {
		t.Fatal(err)
	}
	var sources strings.Builder
	for _, entry := range entries {
		name := entry.Name()
		if entry.Type().IsRegular() && filepath.Join(frontendDirectory, name) != generatedBindings && !strings.Contains(name, ".test.") {
			sources.WriteString(read(t, filepath.Join(frontendDirectory, name)))
		}
	}
	return sources.String()
}

// What a person chooses among and the bounds the window pages by are published
// by the facade in the window's description, and they are exactly what the Go
// side accepts: every import-plan value is a constant the generated bindings
// declare for its union, every reset operator is published with the authority
// the reviewed table requires, every fault action is one a step declares and
// waits exactly when a step must declare a delay for it, every built-in
// diagnosis resolves to the configuration it names, and every window bound is
// the facade's own. The
// interface keeps no copy of any of them.
func TestTheFacadePublishesTheWindowsChoicesAndBounds(t *testing.T) {
	vocabulary := shell(t).Vocabulary
	bindings := read(t, generatedBindings)
	exactly := func(name string, published []string) {
		t.Helper()
		declared := declaredUnion(t, bindings, name)
		slices.Sort(declared)
		sorted := slices.Sorted(slices.Values(published))
		if !slices.Equal(declared, sorted) {
			t.Errorf("the facade publishes %v for %s, which declares %v", published, name, declared)
		}
	}
	plan := vocabulary.ImportPlan
	strs := func(values ...string) []string { return values }
	var framings, payload, boundaries, terminators, encodings, directions []string
	for _, v := range plan.Framings {
		framings = append(framings, string(v))
	}
	for _, v := range plan.PayloadFramings {
		payload = append(payload, string(v))
	}
	for _, v := range plan.Boundaries {
		boundaries = append(boundaries, string(v))
	}
	for _, v := range plan.Terminators {
		terminators = append(terminators, string(v))
	}
	for _, v := range plan.Encodings {
		encodings = append(encodings, string(v))
	}
	for _, v := range plan.Directions {
		directions = append(directions, string(v))
	}
	exactly("ImportFraming", framings)
	exactly("ImportBoundary", boundaries)
	exactly("HL7Terminator", terminators)
	exactly("ImportEncoding", encodings)
	exactly("BundleDirection", directions)
	if !slices.Equal(payload, strs("raw", "mllp")) {
		t.Errorf("the framings that need no batch boundary are published as %v", payload)
	}

	var operators []string
	for _, reviewed := range vocabulary.ResetOperators {
		operators = append(operators, string(reviewed.Operator))
		if required, ok := fixturereset.RequiredAuthority(reviewed.Operator); !ok || required != reviewed.Authority {
			t.Errorf("reset operator %s is published with authority %s, the review requires %s", reviewed.Operator, reviewed.Authority, required)
		}
	}
	exactly("ResetOperator", operators)

	faults := vocabulary.ReceiverFaults
	if !slices.Equal(faults.Actions, collection.FaultActions()) {
		t.Errorf("the facade publishes fault actions %v, a step declares %v", faults.Actions, collection.FaultActions())
	}
	if faults.DefaultDelayMS < 1 || faults.DefaultDelayMS > collection.MaxFaultDelayMS {
		t.Errorf("a waiting fault starts with a delay of %d ms, which no step may declare", faults.DefaultDelayMS)
	}

	if len(vocabulary.DiagnosisBuiltins) != 3 {
		t.Errorf("the facade publishes %d built-in diagnoses", len(vocabulary.DiagnosisBuiltins))
	}
	for _, builtin := range vocabulary.DiagnosisBuiltins {
		config, ok := desktop.DiagnosisBuiltinForTest(builtin.ID)
		if !ok || config.Profile != builtin.Profile || config.Ruleset != builtin.Ruleset {
			t.Errorf("built-in diagnosis %q is published as %s · %s but runs %+v", builtin.ID, builtin.Profile, builtin.Ruleset, config)
		}
	}

	bounds := vocabulary.Bounds
	for name, pair := range map[string][2]int{
		"grid":       {bounds.Grid, grid.MaxRows},
		"comparison": {bounds.Comparison, desktop.MaxComparisonRows},
		"review":     {bounds.Review, desktop.MaxReviewFindings},
		"sequence":   {bounds.Sequence, desktop.MaxSequenceEvents},
		"diagnosis":  {bounds.Diagnosis, desktop.MaxDiagnosisFindings},
	} {
		if pair[0] != pair[1] || pair[0] < 1 {
			t.Errorf("the %s window is published as %d, the facade bounds it at %d", name, pair[0], pair[1])
		}
	}

	// The interface reads these rather than a copy: no built-in ruleset, no
	// reset authority and no contract version of the documents the window
	// composes is written into it.
	sources := interfaceSources(t)
	retired := []string{"readmit-receiver-policy/v", "readmit-observation-source/v"}
	for _, builtin := range vocabulary.DiagnosisBuiltins {
		retired = append(retired, `"`+builtin.Ruleset+`"`)
	}
	for _, reviewed := range vocabulary.ResetOperators {
		// "none" is also an ordinary choice of many controls.
		if reviewed.Authority != fixturereset.NoAuthority {
			retired = append(retired, `"`+string(reviewed.Authority)+`"`)
		}
	}
	for _, copy := range retired {
		if strings.Contains(sources, copy) {
			t.Errorf("the interface keeps its own copy of %s", copy)
		}
	}
}
