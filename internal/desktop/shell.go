package desktop

import (
	"slices"

	"github.com/bharm16/readmit/docs"
	"github.com/bharm16/readmit/internal/capability"
	"github.com/bharm16/readmit/internal/project"
)

// Region is one focusable area of the window. The order of the declared
// regions is the order focus moves through them, which is the order they sit
// in the window: search and navigation down the sidebar, then the page, the
// details of the message open beside it, and the status line along the
// bottom. The window draws them in this order, so what a person tabs through
// and what an assistive technology announces cannot diverge.
type Region struct {
	ID    RegionID `json:"id"`
	Label string   `json:"label"`
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
	ID     CommandID `json:"id"`
	Title  string    `json:"title"`
	Keys   string    `json:"keys,omitzero"`
	Region RegionID  `json:"region,omitzero"`
}

// OperationDisclosure is one deliberately configurable activity of this build
// that reaches a destination outside the window itself, stated before anybody
// relies on it: where it reaches, what data it carries, and what a person must
// configure and approve before it can happen. Every other operation of this
// build is local to the machine, and the statement says so rather than leaving
// the boundary to be inferred from the absence of a row.
type OperationDisclosure struct {
	ID            string `json:"id"`
	Activity      string `json:"activity"`
	Destination   string `json:"destination"`
	Data          string `json:"data"`
	Authorization string `json:"authorization"`
}

// Privacy is what the window tells a person about their data, stated rather
// than implied. Absent names the things this product does not do at all; Kept
// names everything the shell writes outside evidence; Operations names every
// deliberately configurable activity that can reach a destination and what it
// takes to reach it. All three are facts about this build, not reassurance.
type Privacy struct {
	Statement  string                `json:"statement"`
	Absent     []string              `json:"absent"`
	Kept       []string              `json:"kept"`
	Operations []OperationDisclosure `json:"operations"`
}

// Support is the guidance the window gives about what this build supports.
// It is derived from the checked capability ledger and the verified
// qualification state, never from what a screen can draw: a new panel
// certifies no connector, no de-identification and no external workflow, and
// a ledger row still open is named here rather than silently promised.
// Unavailable carries the action of every open cli/desktop row verbatim, read
// from the ledger this build embeds, so no delivery edits it by hand.
type Support struct {
	Notes       []string `json:"notes"`
	Unavailable []string `json:"unavailable"`
}

// Shell is the window's description of itself: the regions focus moves through,
// how every status reads without colour, the commands the palette lists, the
// appearance choices offered, the privacy status, and the vocabulary its
// panels offer and page by. It is fixed data, so the interface renders it
// instead of keeping a second copy that can drift.
type Shell struct {
	Regions    []Region    `json:"regions"`
	Indicators []Indicator `json:"indicators"`
	Commands   []Command   `json:"commands"`
	Themes     []Theme     `json:"themes"`
	TextScales []int       `json:"text_scales"`
	Privacy    Privacy     `json:"privacy"`
	Support    Support     `json:"support"`
	Vocabulary Vocabulary  `json:"vocabulary"`
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

// RegionID identifies a region. A command names one to move focus to it, and
// a search match names the one that reveals what was found.
type RegionID string

const (
	CommandsRegion   RegionID = "commands"
	NavigationRegion RegionID = "navigation"
	EvidenceRegion   RegionID = "evidence"
	InspectorRegion  RegionID = "inspector"
	PrivacyRegion    RegionID = "privacy"
)

// regions are declared in focus order. Adding one adds a step to the journey.
var regions = []Region{
	{ID: CommandsRegion, Label: "Search"},
	{ID: NavigationRegion, Label: "Navigation"},
	{ID: EvidenceRegion, Label: "Main content"},
	{ID: InspectorRegion, Label: "Message details"},
	{ID: PrivacyRegion, Label: "Status"},
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
	{Status: string(DiagnosisGroupsArtifact), Symbol: "⊕", Label: "Diagnosis grouping report"},
	{Status: string(FindingReviewArtifact), Symbol: "⊟", Label: "Finding review"},
	{Status: string(CorrelationReviewArtifact), Symbol: "⊠", Label: "Correlation review"},
	{Status: string(NormalizationArtifact), Symbol: "⊡", Label: "Normalization policy"},
	{Status: string(DiagnoseConfigArtifact), Symbol: "⌬", Label: "Diagnose configuration"},
	{Status: string(DecisionsArtifact), Symbol: "⊦", Label: "Finding decisions"},
	{Status: string(SuiteArtifact), Symbol: "◫", Label: "Suite management"},
	{Status: string(PacketArtifact), Symbol: "◍", Label: "Investigation packet"},
	{Status: string(PortableReviewArtifact), Symbol: "◕", Label: "Portable review"},
	{Status: string(SyntheticPacketArtifact), Symbol: "◌", Label: "Synthetic demonstration packet"},
	{Status: string(PreparedRerunArtifact), Symbol: "◔", Label: "Prepared rerun workspace"},
	{Status: string(UnsupportedArtifact), Symbol: "?", Label: "Unsupported here"},

	{Status: string(project.StatusOpen), Symbol: "◇", Label: "Open"},
	{Status: string(project.StatusInvestigating), Symbol: "◑", Label: "Investigating"},
	{Status: string(project.StatusResolved), Symbol: "★", Label: "Resolved"},
	{Status: string(project.StatusClosed), Symbol: "▮", Label: "Closed"},
}

// CommandID identifies one command of the palette.
type CommandID string

const (
	CommandPaletteCommand        CommandID = "command-palette"
	SearchWorkspaceCommand       CommandID = "search-workspace"
	OpenWorkspaceCommand         CommandID = "open-workspace"
	CreateSampleWorkspaceCommand CommandID = "create-sample-workspace"
	OpenProjectCommand           CommandID = "open-project"
	ManageProfilesCommand        CommandID = "manage-profiles"
	ManageScenariosCommand       CommandID = "manage-scenarios"
	MaintainWorkspaceCommand     CommandID = "maintain-workspace"
	CheckStagedUpgradeCommand    CommandID = "check-staged-upgrade"
	ManageAssertionsCommand      CommandID = "manage-assertions"
	InspectRawFileCommand        CommandID = "inspect-raw-file"
	PerformanceCorpusCommand     CommandID = "performance-corpus"
	CancelOperationCommand       CommandID = "cancel-operation"
	NextRegionCommand            CommandID = "next-region"
	PreviousRegionCommand        CommandID = "previous-region"
	GoToCommandsCommand          CommandID = "go-to-commands"
	GoToNavigationCommand        CommandID = "go-to-navigation"
	GoToEvidenceCommand          CommandID = "go-to-evidence"
	GoToInspectorCommand         CommandID = "go-to-inspector"
	GoToPrivacyCommand           CommandID = "go-to-privacy"
	LargerTextCommand            CommandID = "larger-text"
	SmallerTextCommand           CommandID = "smaller-text"
	SwitchThemeCommand           CommandID = "switch-theme"
)

// commands are everything the window can be asked to do, in the order the
// palette lists them. Ctrl is written for the shortcut key; the platform
// command key is accepted wherever it is shown.
var commands = []Command{
	{ID: CommandPaletteCommand, Title: "Command palette", Keys: "Ctrl+K", Region: CommandsRegion},
	{ID: SearchWorkspaceCommand, Title: "Search this project", Keys: "Ctrl+F", Region: CommandsRegion},
	{ID: OpenWorkspaceCommand, Title: "Open…", Keys: "Ctrl+O", Region: EvidenceRegion},
	{ID: CreateSampleWorkspaceCommand, Title: "Try demo", Region: EvidenceRegion},
	{ID: OpenProjectCommand, Title: "Project overview", Region: EvidenceRegion},
	{ID: ManageProfilesCommand, Title: "Profiles", Region: EvidenceRegion},
	{ID: ManageScenariosCommand, Title: "Scenarios", Keys: "Ctrl+Shift+S", Region: EvidenceRegion},
	{ID: MaintainWorkspaceCommand, Title: "Storage and backups", Region: EvidenceRegion},
	{ID: CheckStagedUpgradeCommand, Title: "Updates", Region: EvidenceRegion},
	{ID: ManageAssertionsCommand, Title: "Assertion sets", Keys: "Ctrl+Shift+A", Region: EvidenceRegion},
	{ID: InspectRawFileCommand, Title: "Inspect file", Region: EvidenceRegion},
	{ID: PerformanceCorpusCommand, Title: "Benchmarks", Region: EvidenceRegion},
	{ID: CancelOperationCommand, Title: "Cancel operation", Keys: "Escape"},
	{ID: NextRegionCommand, Title: "Next section", Keys: "F6"},
	{ID: PreviousRegionCommand, Title: "Previous section", Keys: "Shift+F6"},
	{ID: GoToCommandsCommand, Title: "Focus search", Region: CommandsRegion},
	{ID: GoToNavigationCommand, Title: "Focus navigation", Region: NavigationRegion},
	{ID: GoToEvidenceCommand, Title: "Focus content", Region: EvidenceRegion},
	{ID: GoToInspectorCommand, Title: "Focus message details", Region: InspectorRegion},
	{ID: GoToPrivacyCommand, Title: "Focus status", Region: PrivacyRegion},
	{ID: LargerTextCommand, Title: "Increase text size", Keys: "Ctrl+="},
	{ID: SmallerTextCommand, Title: "Decrease text size", Keys: "Ctrl+-"},
	{ID: SwitchThemeCommand, Title: "Change theme"},
}

// Theme is one appearance the window offers.
type Theme string

const (
	SystemTheme Theme = "system"
	LightTheme  Theme = "light"
	DarkTheme   Theme = "dark"
)

// themes and textScales are the appearance choices offered. Text scales to
// twice its size, and neither choice is written anywhere: both follow the
// system until they are changed, and start from the system again next launch.
var (
	themes     = []Theme{SystemTheme, LightTheme, DarkTheme}
	textScales = []int{100, 125, 150, 200}
)

// privacyStatus is the truth about this build, stated in the window. There is
// no telemetry, crash reporting, update check or analytics anywhere in the
// product, the interface fetches nothing at run time, and nothing connects on
// its own: the only destinations this build ever reaches are the deliberately
// configurable operations disclosed below, each reached only by an action a
// person configured and started. The operations are disclosed per activity —
// destination, data, authorization — because a blanket claim would hide the
// durable execution this build genuinely performs. The things the shell writes
// outside evidence are the recent folder list, the filters a person saved and
// the working session they have not stored. A saved filter holds what they
// typed to filter by, and a retained draft holds a note they were writing;
// both are the same patient data the evidence beside them holds, so the
// window names them here rather than leaving them to be discovered.
var privacyStatus = Privacy{
	Statement: "Evidence is read, written and kept on this machine. The only destinations this build ever reaches are the deliberately configured activities listed below, each reached only while you are running it; startup contacts nothing.",
	Absent: []string{
		"no telemetry or usage analytics",
		"no crash reporting",
		"no update checks",
		"no automatic connection of any kind: every disclosed activity happens only when you configure and start it, and nothing reconnects, retries or synchronizes on its own",
		"nothing kept in browser storage",
	},
	Kept: []string{
		"explicit correlation decisions, including analyst and reason text you enter, in separate owner-readable readmit-correlation-review/v1 directories",
		"the explicitly selected operation policy path, signed entitlement, local UTC high-water and runner admission records; no evidence is stored in them",
		"the folders you have opened, as paths only, in readmit-desktop-recent/v1",
		"the filters you have saved, including any value you typed to filter by, in readmit-filters/v1",
		"the notes you had not stored yet and where you were, in readmit-desktop-session/v1",
		"the editor work you had not stored yet — notes you were writing, test drafts, assertion-set drafts, canonical edits, suite drafts and reproducer plans — in readmit-desktop-drafts/v1, or readmit-desktop-drafts/v2 once a draft names the object it edits",
		"the folder new projects are created in and each project you have opened, as its identity, folder and name, in readmit-desktop-projects/v1",
		"inside each project, beside its evidence, the names, identities and dates of its objects and the files the application saved for them, in its own readmit-catalog/v1 catalog",
		"the commercial destinations file you selected, as a path only, in readmit-desktop-commercial-selection/v1",
		"the customer hub configuration file you selected, as a path only, in readmit-desktop-hub-selection/v1",
	},
	Operations: []OperationDisclosure{
		{
			ID:            "run",
			Activity:      "Durable test execution",
			Destination:   "the test target the executed test or suite names, or the target configuration a replay you previewed names — a nonproduction endpoint a send policy you approved explicitly allows",
			Data:          "the messages the executed test declares, or the case messages you selected for a replay exactly as its preview showed them, and the acknowledgements they draw; nothing else of your imported evidence is sent",
			Authorization: "an activated license for new work, an admitted send policy naming the destination, and your explicit Execute action, or your explicit approval of the replay's preview",
		},
		{
			ID:            "runner",
			Activity:      "Hub-enrolled runner execution",
			Destination:   "the customer hub your runner enrollment names, to fetch scheduled work and report run records",
			Data:          "the run records and artifacts the enrolled schedule covers — never evidence outside the enrolled project",
			Authorization: "a runner enrollment an administrator created, the execution lease it holds, and the recurring schedule you enabled",
		},
		{
			ID:            "capture",
			Activity:      "Capture and source collection",
			Destination:   "the sources your registration declares — a local directory, your own transfer program's output, or a declared nonproduction endpoint under the receiver policy; a capture listener accepts connections on the local port you chose while it runs",
			Data:          "the HL7 bytes those sources deliver, written into a new case bundle on this machine",
			Authorization: "a source registration and receiver policy you saved and your explicit start; an api source is declarable and refused",
		},
		{
			ID:            "observe",
			Activity:      "Observation windows",
			Destination:   "the observation source you validated — a local file export, or an approved HTTPS API whose non-loopback address the source names explicitly",
			Data:          "the records the window reads within its declared scope, watermarks and byte bounds",
			Authorization: "a validated observation source and window pair, and your explicit start",
		},
		{
			ID:            "environment",
			Activity:      "Environment checks and fixture resets",
			Destination:   "the address your recorded named environment names: a connectivity check opens one connection there, with the TLS handshake the target declares, even where a send policy would refuse a send; a fixture reset, including one a controlled reduction performs, opens the one connection its plan needs; evaluating a send policy, or previewing a replay under one, resolves the host names it approves",
			Data:          "no HL7 payload for a connectivity check; the typed reset operations your reviewed reset plan declares; the names a send-policy evaluation or a replay preview looks up",
			Authorization: "a recorded target and your explicit Check, Evaluate, Preview replay or Reset action; a reset also needs an activated license and an admitted send decision for its one connection",
		},
		{
			ID:            "hub",
			Activity:      "Customer artifact hub",
			Destination:   "the customer-controlled hub your configuration names over mutual TLS; sign-in goes to your own identity provider in your browser",
			Data:          "the artifacts you explicitly upload or download, or store and read by digest in an operator-only hub, and the project, role and membership facts the hub returns; nothing is uploaded or fetched by itself",
			Authorization: "an operator-selected hub configuration, the mutual-TLS material it names, and your per-session identity-provider sign-in; an operator-only hub admits the client certificate alone",
		},
		{
			ID:            "declared-program",
			Activity:      "Operator-declared programs",
			Destination:   "whatever the program is configured to reach, such as a secret store or vault, a key service, or the source a transfer program reads; Readmit cannot see or vouch for that program's destinations, and running one adds no network access of Readmit's own",
			Data:          "only the arguments the operator declared, and for a transfer program the credential its reference names, on standard input; Readmit reads back one bounded credential or key, or the listing and entries a transfer program prints, and discards the program's diagnostics",
			Authorization: "a program declared by absolute path in a credential reference, a hub or runner configuration, a protection control, a source registration or an observation source, and your explicit action that needs it: testing, rotating or scanning credential references, a hub, runner or protection action, or a check, reset, reduction, collection, capture or observation whose configuration declares one",
		},
		{
			ID:            "portal",
			Activity:      "Commercial portal",
			Destination:   "the vendor portal address your operator's destinations file names, opened in your own browser",
			Data:          "nothing from this window: the application makes no request to the portal, and the browser session is outside it",
			Authorization: "your deliberate choice to open the portal link",
		},
	},
}

// supportStatus is the window's feature and support guidance. The
// The notes state the refusals the verified qualification state actually holds: the
// connector qualification #35 owns and the database matrix #75 owns are not
// delivered, no de-identification determination exists, external regression
// equivalence is declined by every prepared disclosure, and nothing in this
// development preview is signed for distribution or accepted by release.
var supportStatus = Support{
	Notes: []string{
		"Connector support is declared, not qualified: the engine-export adapters parse a deliberately finite subset, the qualification evidence #35 owns is not delivered, and no screen in this window establishes connector certification.",
		"Database observation is selected and unqualified: adapters stay unqualified until #75's compatibility and authorization matrix passes on the declared engines, and this window never certifies a driver.",
		"De-identification is not certified: a disclosure review is a fail-closed review of one prepared extract, and no Safe Harbor or de-identification determination exists anywhere in this build.",
		"External workflow correctness is declined: fixture proof never substitutes for evidence from the actual target, and every prepared disclosure records external_equivalence as declined.",
		"This is an unsigned development preview: the only published candidate is an early prerelease, nothing is signed for distribution or notarized, and packaged acceptance (#109), release acceptance (#153) and accessibility/privacy acceptance (#111) are open. A new screen certifies none of them.",
	},
	Unavailable: openCapabilities(docs.CapabilityLedger),
}

// openCapabilities is the action of every cli/desktop row the checked
// capability ledger (docs/capability-ledger.json) still has open, so a
// capability without its completed screen and parity evidence is named as
// open rather than promised. The ledger is embedded and checked by
// internal/capability's tests, so one that does not decode is a broken build.
func openCapabilities(data []byte) []string {
	ledger, err := capability.Decode(data)
	if err != nil {
		panic("the embedded capability ledger does not decode: " + err.Error())
	}
	open := []string{}
	for _, row := range ledger.Rows {
		if row.Disposed() || row.Implemented {
			continue
		}
		if row.Kind != capability.KindCLI && row.Kind != capability.KindDesktop {
			continue
		}
		if !slices.Contains(open, row.Action) {
			open = append(open, row.Action)
		}
	}
	return open
}

// Shell describes the window: the regions focus moves through, how each status
// reads without colour, the commands the palette lists, the appearance choices,
// the support guidance and the privacy status. It reads nothing and writes
// nothing, so it does not claim the operation slot and stays available while an
// operation runs: the palette and the privacy status work whenever the window
// is open. The per-operation connected/offline states live in DisclosureStatus,
// which answers through the slot like every other read.
func (a *App) Shell() ShellResult {
	described := Shell{
		Regions:    regions,
		Indicators: indicators,
		Commands:   commands,
		Themes:     themes,
		TextScales: textScales,
		Privacy:    privacyStatus,
		Support:    supportStatus,
		Vocabulary: vocabulary(),
	}
	return ShellResult{State: Completed, Shell: &described}
}
