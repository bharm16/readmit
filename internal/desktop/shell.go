package desktop

import "github.com/bharm16/readmit/internal/project"

// Region is one focusable area of the window. The order of the declared
// regions is the order focus moves through them, which is the order the
// investigation itself runs: choose a workspace, move through what it holds,
// read the evidence, inspect one case, and check what stays on this machine.
// The window renders this order rather than one of its own, so what a person
// tabs through and what an assistive technology announces cannot diverge.
type Region struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// Indicator is how one status is told apart without colour. Status is an
// operation state, an artifact kind, or a registered case status; Label is the
// word for it and Symbol a shape beside that word. Colour may be added on top,
// but it is never the only difference between two statuses.
type Indicator struct {
	Status string `json:"status"`
	Symbol string `json:"symbol"`
	Label  string `json:"label"`
}

// Command is one entry of the command palette. Keys is the shortcut shown for
// it, empty when the command is reached through the palette alone. Region, when
// present, is the region the command moves focus to, so every region has a way
// in that does not depend on a pointer.
type Command struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Keys   string `json:"keys,omitzero"`
	Region string `json:"region,omitzero"`
}

// Privacy is what the window tells a person about their data, stated rather
// than implied. Absent names the things this product does not do at all; Kept
// names everything the shell writes outside evidence. Both are facts about this
// build, not reassurance.
type Privacy struct {
	Statement string   `json:"statement"`
	Absent    []string `json:"absent"`
	Kept      []string `json:"kept"`
}

// Shell is the window's description of itself: the regions focus moves through,
// how every status reads without colour, the commands the palette lists, the
// appearance choices offered, and the privacy status. It is fixed data, so the
// interface renders it instead of keeping a second copy that can drift.
type Shell struct {
	Regions    []Region    `json:"regions"`
	Indicators []Indicator `json:"indicators"`
	Commands   []Command   `json:"commands"`
	Themes     []string    `json:"themes"`
	TextScales []int       `json:"text_scales"`
	Privacy    Privacy     `json:"privacy"`
}

// ShellResult carries one state, like every other result the interface reads.
// The facade itself always completes: it reads nothing, writes nothing, and
// cannot be cancelled. The state exists because the binding can be unavailable
// while the window is still starting, which the interface reports as Failed
// rather than drawing a window with no commands and no privacy status.
type ShellResult struct {
	State  State  `json:"state"`
	Reason string `json:"reason,omitzero"`
	Shell  *Shell `json:"shell,omitzero"`
}

// Region identifiers. A command names one of these to move focus to it, and a
// search match names the one that reveals what was found.
const (
	CommandsRegion   = "commands"
	NavigationRegion = "navigation"
	EvidenceRegion   = "evidence"
	InspectorRegion  = "inspector"
	PrivacyRegion    = "privacy"
)

// regions are declared in focus order. Adding one adds a step to the journey.
var regions = []Region{
	{ID: CommandsRegion, Label: "Commands and search"},
	{ID: NavigationRegion, Label: "Project navigation"},
	{ID: EvidenceRegion, Label: "Evidence"},
	{ID: InspectorRegion, Label: "Inspector"},
	{ID: PrivacyRegion, Label: "Privacy status"},
}

// indicators give every status a word and a shape. The word carries the meaning
// on its own, because a shape depends on the platform font having the glyph;
// the shape is a second channel for a person reading at a glance. No status is
// distinguished by colour alone, and no two share a word or a shape.
var indicators = []Indicator{
	{Status: string(Empty), Symbol: "○", Label: "Nothing here yet"},
	{Status: string(Busy), Symbol: "⟳", Label: "Working"},
	{Status: string(Cancelled), Symbol: "↩", Label: "Cancelled"},
	{Status: string(Failed), Symbol: "✕", Label: "Failed"},
	{Status: string(PermissionDenied), Symbol: "⊘", Label: "Permission denied"},
	{Status: string(Completed), Symbol: "✓", Label: "Completed"},

	{Status: string(CaseArtifact), Symbol: "▣", Label: "Case evidence"},
	{Status: string(ProjectArtifact), Symbol: "☰", Label: "Project document"},
	{Status: string(RevisionsArtifact), Symbol: "▚", Label: "Editable project document"},
	{Status: string(ResultArtifact), Symbol: "▤", Label: "Retained test result"},
	{Status: string(JobArtifact), Symbol: "▥", Label: "Durable run"},
	{Status: string(ReviewArtifact), Symbol: "▦", Label: "Export review"},
	{Status: string(IndexArtifact), Symbol: "▧", Label: "Case index"},
	{Status: string(TargetArtifact), Symbol: "▨", Label: "Test environment"},
	{Status: string(RulesArtifact), Symbol: "▩", Label: "Correlation rules"},
	{Status: string(PlanArtifact), Symbol: "◈", Label: "Transform plan"},
	{Status: string(SpecArtifact), Symbol: "◉", Label: "Test specification"},
	{Status: string(PackArtifact), Symbol: "◎", Label: "Profile pack"},
	{Status: string(ProfileArtifact), Symbol: "◓", Label: "Local profile"},
	{Status: string(PackageArtifact), Symbol: "◪", Label: "Profile package"},
	{Status: string(AnalysisArtifact), Symbol: "◒", Label: "Sequence analysis"},
	{Status: string(SecretArtifact), Symbol: "⚿", Label: "Credential references"},
	{Status: string(PolicyArtifact), Symbol: "◖", Label: "Send policy"},
	{Status: string(ResetArtifact), Symbol: "↺", Label: "Fixture reset"},
	{Status: string(DiagnosisArtifact), Symbol: "⊞", Label: "Diagnosis report"},
	{Status: string(FindingReviewArtifact), Symbol: "⊟", Label: "Finding review"},
	{Status: string(CorrelationReviewArtifact), Symbol: "⊠", Label: "Correlation review"},
	{Status: string(NormalizationArtifact), Symbol: "⊡", Label: "Normalization policy"},
	{Status: string(DiagnoseConfigArtifact), Symbol: "⌬", Label: "Diagnose configuration"},
	{Status: string(DecisionsArtifact), Symbol: "⊦", Label: "Finding decisions"},
	{Status: string(UnsupportedArtifact), Symbol: "?", Label: "Unsupported here"},

	{Status: string(project.StatusOpen), Symbol: "◇", Label: "Open"},
	{Status: string(project.StatusInvestigating), Symbol: "◑", Label: "Investigating"},
	{Status: string(project.StatusResolved), Symbol: "★", Label: "Resolved"},
	{Status: string(project.StatusClosed), Symbol: "▮", Label: "Closed"},
}

// commands are everything the window can be asked to do, in the order the
// palette lists them. Ctrl is written for the shortcut key; the platform
// command key is accepted wherever it is shown.
var commands = []Command{
	{ID: "command-palette", Title: "Command palette", Keys: "Ctrl+K", Region: CommandsRegion},
	{ID: "search-workspace", Title: "Search this workspace", Keys: "Ctrl+F", Region: CommandsRegion},
	{ID: "open-workspace", Title: "Open a workspace folder…", Keys: "Ctrl+O", Region: NavigationRegion},
	{ID: "create-sample-workspace", Title: "Create the sample workspace…", Region: NavigationRegion},
	{ID: "open-project", Title: "Open the project of this workspace", Region: EvidenceRegion},
	{ID: "manage-profiles", Title: "Manage interface profiles…", Region: InspectorRegion},
	{ID: "manage-scenarios", Title: "Design synthetic scenarios…", Keys: "Ctrl+Shift+S", Region: InspectorRegion},
	{ID: "maintain-workspace", Title: "Maintain this workspace…", Region: EvidenceRegion},
	{ID: "check-staged-upgrade", Title: "Check a staged upgrade…", Region: EvidenceRegion},
	{ID: "cancel-operation", Title: "Cancel the running operation", Keys: "Escape"},
	{ID: "next-region", Title: "Go to the next region", Keys: "F6"},
	{ID: "previous-region", Title: "Go to the previous region", Keys: "Shift+F6"},
	{ID: "go-to-commands", Title: "Go to commands and search", Region: CommandsRegion},
	{ID: "go-to-navigation", Title: "Go to project navigation", Region: NavigationRegion},
	{ID: "go-to-evidence", Title: "Go to evidence", Region: EvidenceRegion},
	{ID: "go-to-inspector", Title: "Go to the inspector", Region: InspectorRegion},
	{ID: "go-to-privacy", Title: "Go to the privacy status", Region: PrivacyRegion},
	{ID: "larger-text", Title: "Larger text", Keys: "Ctrl+="},
	{ID: "smaller-text", Title: "Smaller text", Keys: "Ctrl+-"},
	{ID: "switch-theme", Title: "Switch between system, light and dark"},
}

// themes and textScales are the appearance choices offered. Text scales to
// twice its size, and neither choice is written anywhere: both follow the
// system until they are changed, and start from the system again next launch.
var (
	themes     = []string{"system", "light", "dark"}
	textScales = []int{100, 125, 150, 200}
)

// privacyStatus is the truth about this build, stated in the window. There is
// no telemetry, crash reporting, update check or analytics anywhere in the
// product, the interface fetches nothing at run time, and the only things the
// shell writes outside evidence are the recent folder list, the filters a
// person saved and the working session they have not stored. A saved filter
// holds what they typed to filter by, and a retained draft holds a note they
// were writing; both are the same patient data the evidence beside them holds,
// so the window names them here rather than leaving them to be discovered.
var privacyStatus = Privacy{
	Statement: "Everything here is read on this machine. No evidence, folder name or result is sent anywhere.",
	Absent: []string{
		"no telemetry or usage analytics",
		"no crash reporting",
		"no update checks",
		"no network request of any kind",
		"nothing kept in browser storage",
	},
	Kept: []string{
		"explicit correlation decisions, including analyst and reason text you enter, in separate owner-readable readmit-correlation-review/v1 directories",
		"the explicitly selected operation policy path, signed entitlement, local UTC high-water and runner admission records; no evidence is stored in them",
		"the folders you have opened, as paths only, in readmit-desktop-recent/v1",
		"the filters you have saved, including any value you typed to filter by, in readmit-filters/v1",
		"the notes you had not stored yet and where you were, in readmit-desktop-session/v1",
		"the editor work you had not stored yet — notes you were writing, test drafts, canonical edits and reproducer plans — in readmit-desktop-drafts/v1",
	},
}

// Shell describes the window: the regions focus moves through, how each status
// reads without colour, the commands the palette lists, the appearance choices,
// and the privacy status. It reads nothing and writes nothing, so it does not
// claim the operation slot and stays available while an operation runs: the
// palette and the privacy status work whenever the window is open.
func (a *App) Shell() ShellResult {
	described := Shell{
		Regions:    regions,
		Indicators: indicators,
		Commands:   commands,
		Themes:     themes,
		TextScales: textScales,
		Privacy:    privacyStatus,
	}
	return ShellResult{State: Completed, Shell: &described}
}
