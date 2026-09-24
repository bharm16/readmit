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
// Unavailable carries the action of every open cli/desktop row verbatim, so
// the check beside this declaration fails the moment the ledger and the
// window disagree in either direction.
type Support struct {
	Notes       []string `json:"notes"`
	Unavailable []string `json:"unavailable"`
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
	Support    Support     `json:"support"`
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
	{Status: string(SuiteArtifact), Symbol: "◫", Label: "Suite management"},
	{Status: string(PacketArtifact), Symbol: "◍", Label: "Investigation packet"},
	{Status: string(PortableReviewArtifact), Symbol: "◕", Label: "Portable review"},
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
	{ID: "manage-assertions", Title: "Author assertion sets…", Keys: "Ctrl+Shift+A", Region: InspectorRegion},
	{ID: "inspect-raw-file", Title: "Inspect a raw HL7 file…", Region: InspectorRegion},
	{ID: "performance-corpus", Title: "Generate or scan a performance corpus…", Region: InspectorRegion},
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
		"the editor work you had not stored yet — notes you were writing, test drafts, assertion-set drafts, canonical edits, suite drafts and reproducer plans — in readmit-desktop-drafts/v1",
		"the commercial destinations file you selected, as a path only, in readmit-desktop-commercial-selection/v1",
		"the customer hub configuration file you selected, as a path only, in readmit-desktop-hub-selection/v1",
	},
	Operations: []OperationDisclosure{
		{
			ID:            "run",
			Activity:      "Durable test execution",
			Destination:   "the test target the executed test or suite names — a nonproduction endpoint a send policy you approved explicitly allows",
			Data:          "the messages the executed test declares and the acknowledgements they draw; your imported evidence is never a send list",
			Authorization: "an activated license for new work, an admitted send policy naming the destination, and your explicit Execute action",
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
			Destination:   "the address your recorded named environment names: a connectivity check opens one connection there, with the TLS handshake the target declares, even where a send policy would refuse a send; a fixture reset, including one a controlled reduction performs, opens the one connection its plan needs; evaluating a send policy resolves the host names it approves",
			Data:          "no HL7 payload for a connectivity check; the typed reset operations your reviewed reset plan declares; the names a send-policy evaluation looks up",
			Authorization: "a recorded target and your explicit Check, Evaluate or Reset action; a reset also needs an activated license and an admitted send decision for its one connection",
		},
		{
			ID:            "hub",
			Activity:      "Customer artifact hub",
			Destination:   "the customer-controlled hub your configuration names over mutual TLS; sign-in goes to your own identity provider in your browser",
			Data:          "the artifacts you explicitly upload or download and the project, role and membership facts the hub returns; nothing is uploaded or fetched by itself",
			Authorization: "an operator-selected hub configuration, the mutual-TLS material it names, and your per-session identity-provider sign-in",
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
// unavailable list is the action of every cli/desktop row the checked
// capability ledger (docs/capability-ledger.json) still has open, so a
// capability without its completed screen and parity evidence is named as
// open here rather than promised; the check beside the shell declaration
// keeps this list and the ledger in agreement in both directions. The notes
// state the refusals the verified qualification state actually holds: the
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
	Unavailable: []string{
		"admit and renew runner instances from the runner host itself",
		"align two verified collections and report one window of rows",
		"check a staged upgrade and take its recovery archive",
		"check independent fixture expectations against a pinned library template",
		"choose the private folder a new local activation is created in",
		"compare two built revisions and what their runs decided",
		"compare two collections under a declared policy and list every suppressed, retained, undecided and unaddressed difference",
		"compare two pinned library revisions without mutating either",
		"download an authorized support export with custody notice",
		"execute, queue, resume, recover and inspect durable runs",
		"explain what a run's evidence decided assertion by assertion",
		"export a scenario library to a new destination",
		"export exact reviewed assertion-set bytes to a new workspace entry",
		"export the installed entitlement byte for byte",
		"generate reproducible SIU synthetic case bundles from declared inputs",
		"generate, verify and prepare runnable copies of synthetic demonstration packets",
		"group findings from selected cases by signature without hiding any finding",
		"import a scenario library into a new workspace entry",
		"import the frozen receiver fixtures as an imported sample case without activation",
		"install a later issue of the same entitlement into an entitlement store",
		"join a diagnosis with analyst decisions and report every verdict, basis, scope and promotion without writing",
		"link case occurrences under declared source, session and authority rules",
		"list previously opened folders most recent first",
		"list saved filters and the selected one",
		"open a reusable scenario library document",
		"open a saved scenario or order-scenario document",
		"open an existing complete assertion set into the structured draft",
		"open an existing local profile, resolve against its pack and compute its seal",
		"open an explicitly selected review history and record accept, reject or added pairs with analyst and reason",
		"open one retained diagnosis report directory and report the identity a review must name",
		"preview a relationship-preserving transform plan against a verified case, writing nothing into evidence",
		"preview or explicitly send selected case messages",
		"preview partition, pinned groups and the bounded-minimality boundary without sending",
		"propose expectations a reviewed run supports, recording none",
		"read team collaboration notifications",
		"read the editable project document beside the evidence",
		"record decisions about proposals into the draft",
		"record which saved filter the grid applies",
		"recover a selected project document recovery copy",
		"reexecute reviewed transformed evidence against an explicitly selected authorized target",
		"register a built revision's lineage in the project",
		"release an entitlement store's device activation so the seat can be reissued",
		"remove the last step and resolve what remains",
		"reopen a declared responder policy",
		"reopen a declared source registration",
		"reopen a saved diagnosis configuration",
		"reopen a saved finding-decisions document",
		"reopen a saved normalization policy",
		"reopen a saved sequence-analysis declaration",
		"reopen a saved transform plan",
		"report an entitlement store's installed entitlement, term state and scope",
		"resolve an expired clock and restate the high-water",
		"retain and verify a reviewed CI change gate snapshot",
		"run a bounded reduction under existing execution and reset authorisation",
		"run a controlled SIU fixture and export its ledger",
		"save a finding-decisions document as a new workspace entry",
		"save a relationship-preserving transform plan with its rules and profile pins",
		"save a validated scenario document without overwriting another entry",
		"save or version a library template without overwriting another revision",
		"search team collaboration notifications",
		"search team collaboration review history",
		"select a saved test or suite through the host's native file dialog",
		"select an absolute customer hub configuration path explicitly",
		"select an absolute policy path explicitly",
		"select lifecycle profile and generator version from a saved local interface profile",
		"select the operation policy through the native chooser",
		"stop a control writing new packages while it still opens what it wrote",
		"store one named filter and select it",
		"validate an observation source document locally without collecting",
		"validate an observation window document locally without collecting",
		"validate the structured runner configuration and show its canonical document without writing",
		"verify a received entitlement and install it into an entitlement store for one device",
		"verify a staged runner update against the pinned customer deployment key without running it",
		"verify and unpack an interface package into a new directory",
		"write an entitlement store's installed entitlement back out, byte for byte",
		"write the reproducer as a new derived case folder and offer register/compare/test handoffs",
	},
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
	}
	return ShellResult{State: Completed, Shell: &described}
}
