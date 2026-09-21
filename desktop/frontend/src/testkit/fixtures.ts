// Typed fixtures for the facade's operation states, in the vocabulary of
// docs/desktop.md: empty, busy, cancelled, failed, permission_denied,
// completed. Every value here is synthetic — entry names, fixed identity
// tokens, counts and states the engine could have reported. No HL7 field
// value, message byte, real folder, credential or developer-machine path is
// in any of them, because what a component does with domain meaning is not
// these tests' subject: the Go readers decide that, and the Go tests prove it.
import type {
  Artifact,
  CaseResult,
  EditorDraft,
  Comparison,
  CompareResult,
  ComparisonRow,
  CanonicalTestResult,
  DurableRunResult,
  FiltersResult,
  Grid,
  GridResult,
  GridRow,
  GuideResult,
  GuideStepId,
  Inspection,
  InspectionResult,
  PracticeResult,
  ProjectOverview,
  ProjectOverviewResult,
  RecentResult,
  RecoveryResult,
  RevisionsResult,
  RunComparisonResult,
  Sequence,
  SequenceResult,
  SessionResult,
  Shell,
  ShellResult,
  StatusValue,
  ReproducerPlan,
  ReproducerResolution,
  ReproducerResult,
  RunState,
  TestDraftDocument,
  TestResult,
  TestResolution,
  HubResult,
  HubDiagnosisResult,
  HubArtifactsResult,
} from "../bindings";

/** The synthetic workspace root the fixtures name. It is not a path on any
 * machine that runs these tests; in production the host's own folder dialog
 * supplies it, and here the test is the host. */
export const WORKSPACE_ROOT = "/workspace-under-test";

/** The synthetic case entry the journeys open, its fixed identity token, and
 * the index entry the workspace lists beside it. */
export const CASE_ENTRY = "sample-case";
export const CASE_IDENTITY = "case-identity-fixed-for-tests";
export const INDEX_ENTRY = "sample-case.index.json";
export const GRID_OCCURRENCE = "occ-000001";
export const NEXT_OCCURRENCE = "occ-000002";

/** Every status the window can draw, each with its own word and its own
 * shape, the way the facade declares them. */
function indicatorFixtures(): Shell["indicators"] {
  const statuses: StatusValue[] = [
    "empty",
    "busy",
    "cancelled",
    "failed",
    "permission_denied",
    "completed",
    "case",
    "project",
    "revisions",
    "result",
    "job",
    "review",
    "index",
    "target",
    "rules",
    "plan",
    "spec",
    "pack",
    "analysis",
    "unsupported",
    "open",
    "investigating",
    "resolved",
    "closed",
  ];
  return statuses.map((status, position) => ({
    status,
    symbol: String.fromCharCode(0x25a0 + position),
    label: status,
  }));
}

/** The window's description of itself, as the facade answers `Shell`. */
export function shellResult(): ShellResult {
  const shell: Shell = {
    regions: [
      { id: "commands", label: "Commands" },
      { id: "navigation", label: "Workspace" },
      { id: "evidence", label: "Evidence" },
      { id: "inspector", label: "Inspector" },
      { id: "privacy", label: "Privacy" },
    ],
    indicators: indicatorFixtures(),
    commands: [
      { id: "command-palette", title: "Command palette", keys: "Ctrl+K" },
      { id: "search-workspace", title: "Search this workspace", keys: "Ctrl+F" },
      { id: "open-workspace", title: "Open a workspace folder", keys: "Ctrl+O" },
      { id: "cancel-operation", title: "Cancel the running operation", keys: "Escape" },
    ],
    themes: ["system", "light", "dark"],
    text_scales: [100, 125, 150],
    privacy: {
      statement: "Nothing leaves this machine.",
      absent: ["No telemetry, crash reporting or update check."],
      kept: [
        "Recent folders, saved filters and the working session, in this user's own configuration.",
      ],
    },
  };
  return { state: "completed", shell };
}

export function recentResult(roots: string[]): RecentResult {
  return { state: roots.length === 0 ? "empty" : "completed", roots };
}

export function filtersResult(
  filters: FiltersResult["filters"] = [],
  selected = "",
): FiltersResult {
  return { state: "completed", filters, selected };
}

/** A dialog outcome: the folder the host chose, with the entries it listed. */
export function folderChosen(
  root: string = WORKSPACE_ROOT,
  artifacts: Artifact[] = [],
): { state: "completed"; workspace: { root: string; artifacts: Artifact[] } } {
  return { state: "completed", workspace: { root, artifacts } };
}

/** A workspace holding one case entry and the index file listed beside it.
 * The index is a file, listed as what it declares rather than as evidence. */
export function folderWithCase(name: string = CASE_ENTRY): ReturnType<typeof folderChosen> {
  return folderChosen(WORKSPACE_ROOT, [
    { name, kind: "case", schema: "readmit-case/v3", provenance: "generated" },
    { name: INDEX_ENTRY, kind: "index" },
  ]);
}

/** A second case entry, for journeys that register or compare. */
export const OTHER_CASE_ENTRY = "other-case";

/** The project overview the facade re-reads from disk after every read or
 * write. Synthetic titles, statuses and states only. */
export function projectOverviewResult(
  cases: ProjectOverview["cases"] = [],
  state: ProjectOverviewResult["state"] = cases.length > 0 ? "completed" : "empty",
): ProjectOverviewResult {
  return {
    state,
    overview: {
      root: WORKSPACE_ROOT,
      title: "Scheduling investigation",
      default_owner: "integration-team",
      default_version: "siu-2.5.1-v1",
      interface_versions: ["siu-2.5.1-v1"],
      cases,
      revisions: [],
      notes: [],
    },
  };
}

/** One registered case of the overview, verified beside its recorded facts. */
export function registeredCase(name: string = CASE_ENTRY): ProjectOverview["cases"][number] {
  return {
    name,
    identity: CASE_IDENTITY,
    schema: "readmit-case/v3",
    provenance: "generated",
    interface_version: "siu-2.5.1-v1",
    title: "Duplicate appointment after reschedule",
    status: "open",
    owner: "integration-team",
    tags: ["scheduling"],
    incidents: [],
    evidence: "verified",
  };
}

/** The dialog was dismissed without choosing. */
export const dialogDismissed = { state: "cancelled" } as const;

/** The account cannot read or write the chosen folder. */
export const folderDenied = { state: "permission_denied" } as const;

/** The engine refused the operation, in its own fixed sentence. */
export function refused(reason: string): { state: "failed"; reason: string } {
  return { state: "failed", reason };
}

/** Verified evidence: counts and an identity, and no content. */
export function caseResult(
  name: string = CASE_ENTRY,
  identity: string = CASE_IDENTITY,
): CaseResult {
  return {
    state: "completed",
    case: {
      name,
      identity,
      schema: "readmit-case/v3",
      provenance: "generated",
      sources: 2,
      occurrences: 2,
      messages: 1,
      acknowledgements: 1,
      unparsed: 0,
    },
  };
}

/** One window of the grid: positions, kinds and counts, never a value. */
export function gridResult(rows: GridRow[], overrides: Partial<Grid> = {}): GridResult {
  return {
    state: "completed",
    grid: {
      case: CASE_ENTRY,
      index: INDEX_ENTRY,
      identity: CASE_IDENTITY,
      filter: "",
      offset: 0,
      limit: 200,
      total: rows.length,
      matched: rows.length,
      excluded: 0,
      undecided: 0,
      undecodable: 0,
      rows,
      ...overrides,
    },
  };
}

/** A grid row: where the occurrence is and what it is. */
export function gridRow(
  id: string,
  kind: "message" | "ack" | "unparsed" = "message",
): GridRow {
  return {
    id,
    source_id: "s0001",
    offset: 0,
    size: 256,
    kind,
    direction: "outbound",
    observed_at: "2026-01-01T12:00:00Z",
    decoded: kind !== "unparsed",
  };
}

/** One revealed occurrence: positions, states and byte windows with no
 * displayed value — the raw and decoded texts are empty, which the window
 * draws as the absence of a value rather than inventing one. */
export function inspectionResult(
  occurrence: string = GRID_OCCURRENCE,
  overrides: Partial<Inspection> = {},
): InspectionResult {
  const selected = {
    segment: "MSH",
    field: 0,
    path: "",
    parent: "",
    kind: "message",
    state: "present" as const,
    start: 0,
    end: 256,
  };
  return {
    state: "completed",
    inspection: {
      metadata: {
        label: "",
        status: "",
        hl7_version: "2.5.1",
        contract: "readmit-field-labels/v1",
        provenance: "nHapi (MPL-2.0)",
      },
      identity: CASE_IDENTITY,
      occurrence,
      source_id: "s0001",
      source_offset: 0,
      size: 256,
      selected,
      children: [],
      node_offset: 0,
      child_count: 0,
      bytes: [],
      byte_offset: 0,
      raw: "",
      decoded: "",
      encoding: "ASCII",
      decode_state: "parsed",
      notice: "",
      ...overrides,
    },
  };
}

/** The guided sample read out of the open folder, at a chosen step. `next`
 * absent means every step is done. */
export function guideResult(next: GuideStepId | undefined, done: number): GuideResult {
  const steps: NonNullable<GuideResult["guide"]>["steps"] = [
    { id: "sample", title: "Create the sample workspace", detail: "Writes synthetic evidence.", done: false },
    { id: "test", title: "Author a regression test", detail: "Answers the authoring stages.", done: false },
    { id: "baseline", title: "Run against the fixture as it misbehaves", detail: "The defect makes it fail.", done: false },
    { id: "post-fix", title: "Run against the corrected fixture", detail: "The same test passes.", done: false },
  ];
  return {
    state: "completed",
    guide: {
      case: CASE_ENTRY,
      identity: CASE_IDENTITY,
      spec: "reschedule-test.json",
      steps: steps.map((step, position) => ({ ...step, done: position < done })),
      ...(next ? { next } : {}),
    },
  };
}

/** What one practice run produced: statuses and a verdict, no values. */
export function practiceResult(trial: "baseline" | "post-fix", status: string): PracticeResult {
  return {
    state: "completed",
    practice: {
      output: trial === "baseline" ? "baseline-run" : "post-fix-run",
      trial,
      status,
      identity: "run-identity-fixed-for-tests",
      spec_identity: "spec-identity-fixed-for-tests",
      assertions: [{ id: "ledger-has-booking", operator: "ledger_count", status }],
      changed_bindings: ["case", "observation", "reset"],
    },
  };
}

/** The retained working session the window restores, with unstored drafts. */
export function recoveryResult(
  session?: RecoveryResult["session"],
  run?: RecoveryResult["run"],
  runReason?: string,
): RecoveryResult {
  return {
    state: session || run ? "completed" : "empty",
    ...(session ? { session } : {}),
    ...(run ? { run } : {}),
    ...(runReason ? { run_reason: runReason } : {}),
  };
}

export function retainedDraft(
  project = WORKSPACE_ROOT,
  name = "triage",
): NonNullable<RecoveryResult["session"]>["drafts"][number] {
  return {
    project,
    note: { name, title: "First pass", body: "still writing this" },
  };
}

/** The session a retention or discard changed. */
export const sessionStored: SessionResult = { state: "completed" };

/** One retained editor draft: the store's envelope around an editor's own
 * unstored work, carried under an internal identity. Positions and names only,
 * never a value read out of evidence. */
export function editorDraft(
  id: string,
  kind: string,
  content: unknown,
  overrides: Partial<EditorDraft> = {},
): EditorDraft {
  return {
    id,
    kind,
    workspace: WORKSPACE_ROOT,
    case: CASE_ENTRY,
    identity: CASE_IDENTITY,
    content_schema: "readmit-test-draft/v1",
    content,
    ...overrides,
  };
}

/** One synchronized layout of the case: lanes, counts and event positions
 * over sources, with nothing about what any message says. */
export function sequenceResult(events: Sequence["events"], overrides: Partial<Sequence> = {}): SequenceResult {
  return {
    state: "completed",
    sequence: {
      analysis_entry: "",
      case: CASE_ENTRY,
      identity: CASE_IDENTITY,
      rules: "",
      session_declared: false,
      declared: [],
      unsupported: [],
      lanes: [
        {
          source_id: "s0001",
          occurrences: events.length,
          messages: events.length,
          acknowledgements: 0,
          unparsed: 0,
          ordered: events.length,
          unordered: 0,
          earliest: "2026-01-01T12:00:00Z",
          latest: "2026-01-01T12:01:00Z",
        },
      ],
      gaps: [
        { gap: "unacknowledged_message", count: 0 },
        { gap: "unknown_observed_time", count: 0 },
      ],
      summary: {
        occurrences: events.length,
        ordered: events.length,
        unordered: 0,
        messages: events.length,
        acknowledgements: 0,
        unparsed: 0,
        sent: events.length,
        received: 0,
        unknown_direction: 0,
        links: 0,
        collisions: 0,
        unsupported: 0,
      },
      offset: 0,
      limit: 200,
      total: events.length,
      events,
      clock: "A recorded time is one capture's own clock.",
      scope: "Order is not causality.",
      ...overrides,
    },
  };
}

/** One event of a sequence: a position in a lane, never a value. */
export function sequenceEvent(
  occurrence: string,
  position: number,
): Sequence["events"][number] {
  return {
    position,
    occurrence,
    source_id: "s0001",
    kind: "message",
    direction: "outbound",
    offset: 0,
    size: 256,
    ordering: "observed",
    observed_at: "2026-01-01T12:00:00Z",
    declared_state: "present",
    decoded: true,
    gaps: [],
    references: [],
    referenced: 0,
  };
}

export const EMPTY_DRAFT: TestDraftDocument = {
  schema: "",
  case: { entry: "", identity: "" },
  name: "",
  messages: [],
  target: "",
  boundary: "",
  observation: "",
  reset: "",
  expectations: [],
};

/** A draft and what it resolves to, as the engine reports one. The resolution
 * here is the reporting shape only: which stages are still missing, the send
 * order and the workspace's targets. */
export function testResult(
  draft: TestDraftDocument,
  missing: TestResolution["missing"],
): TestResult {
  return {
    state: "completed",
    test: {
      draft,
      resolution: {
        stage: missing[0] ?? "",
        missing,
        messages: draft.messages,
        targets: [
          { name: "practice-target", schema: "readmit-target/v3", classification: "nonproduction" },
        ],
        coverage: { ledger: { applies: draft.boundary === "appointment-ledger", covered: false }, messages: [], uncovered: [] },
      },
    },
  };
}

export const NO_STAGES_MISSING: TestResolution["missing"] = [];

/** A reproducer resolution: what a plan retains and edits, as positions. */
export function reproducerResult(
  plan: ReproducerPlan,
  resolution: ReproducerResolution,
): ReproducerResult {
  return { state: "completed", reproducer: { plan, resolution } };
}

/** One row both panes of a comparison draw. */
export function comparisonRow(
  position: number,
  kind: ComparisonRow["kind"],
  overrides: Partial<ComparisonRow> = {},
): ComparisonRow {
  return { position, kind, ...overrides };
}

/** One comparison of two collections: rows, counts and scope, positions only. */
export function compareResult(rows: ComparisonRow[], overrides: Partial<Comparison> = {}): CompareResult {
  return {
    state: "completed",
    comparison: {
      left: CASE_ENTRY,
      right: "other-case",
      report: "readmit-diff/v1",
      boundary: "messages",
      left_summary: {
        kind: "case",
        identity: CASE_IDENTITY,
        payloads: "2",
        occurrences: 2,
        excluded: 0,
      },
      right_summary: {
        kind: "case",
        identity: "other-case-identity-fixed-for-tests",
        payloads: "2",
        occurrences: 2,
        excluded: 0,
      },
      alignment: "by declared key",
      scope: "A comparison of stored messages is not a comparison of everything.",
      keys: [],
      fields: [],
      summary: {
        paired: rows.length,
        changed: 0,
        unchanged: 0,
        uncompared: 0,
        field_changes: 0,
        inserted: 0,
        missing: 0,
        ambiguous: 0,
        unaligned: 0,
      },
      offset: 0,
      limit: 200,
      total: rows.length,
      rows,
      unsupported: [],
      ...overrides,
    },
  };
}

/** What comparing two retained executions reports, values hidden. */
export function runComparisonResult(overrides: Partial<NonNullable<RunComparisonResult["comparison"]>> = {}): RunComparisonResult {
  return {
    state: "completed",
    comparison: {
      baseline: executionView("result-a"),
      current: executionView("result-b"),
      repeats: [],
      drift: {
        schema: "readmit-drift/v1",
        scope: "configuration and environment",
        left: driftSide(),
        right: driftSide(),
        drift: [],
        attribution: { outcome: "no recorded change", changed: [], unresolved: [] },
      },
      assertions: [],
      specification: "unchanged",
      approval: "none",
      approval_revision: 0,
      stability: {
        state: "no_observed_flakiness",
        runs: 2,
        failures: 0,
        passes: 2,
        errors: 0,
        incomplete: 0,
        flaky_assertions: [],
        reason: "Two distinct retained results.",
      },
      scope: "A behavior change does not prove a cause.",
      ...overrides,
    },
  };
}

function executionView(identity: string): NonNullable<RunComparisonResult["comparison"]>["baseline"] {
  return {
    identity,
    status: "passed",
    run_state: "passed",
    error_class: "",
    boundary: "messages",
    planned: 1,
    observed: 1,
    unobserved: 0,
    unevaluated: 0,
    excluded: "0",
    gaps: [],
    assertions: [],
  };
}

function driftSide(): NonNullable<RunComparisonResult["comparison"]>["drift"]["left"] {
  return {
    kind: "result",
    identity: "identity-fixed-for-tests",
    input: { state: "completed", transformations: [], recorded_changes: 0 },
    target: { state: "completed", revision: "revision-token" },
    environment: { state: "completed" },
    rule: { state: "undeclared" },
  };
}

/** A durable run summary, in durablerun's own state vocabulary. */
export function durableRunResult(state: RunState, overrides: Partial<NonNullable<DurableRunResult["run"]>> = {}): DurableRunResult {
  return {
    state: "completed",
    run: {
      schema: "readmit-run/v1",
      state,
      stop_reason: state,
      delivery_uncertain: false,
      planned: 1,
      recorded: 1,
      recovered: false,
      journal_incomplete: false,
      ...overrides,
    },
  };
}

/** The editable project document a stored note lands in. */
export function revisionsResult(): RevisionsResult {
  return {
    state: "completed",
    revisions: { schema: "readmit-revisions/v1", notes: [], revisions: [] },
  };
}

/** A canonical test read, validated or exported by the shared reader. */
export function canonicalResult(
  overrides: Partial<Omit<CanonicalTestResult, "state">> = {},
): CanonicalTestResult {
  return { state: "completed", ...overrides };
}

/** The indicator table panels are given, the way App builds it from Shell. */
export function indicatorTable(): Map<StatusValue, Shell["indicators"][number]> {
  const shell = shellResult().shell;
  return new Map(shell ? shell.indicators.map((indicator) => [indicator.status, indicator]) : []);
}

export function defaultHubResult(overrides: Partial<HubResult> = {}): HubResult {
  return {
    state: "completed",
    connected: true,
    authenticated: true,
    subject: "analyst@customer.example",
    issuer: "https://idp.customer.example",
    audience: "readmit-hub",
    expires_at: "2026-09-21T18:00:00Z",
    config_path: "/etc/readmit/hub-client.json",
    hub_url: "https://hub.customer.example:8443",
    custody_warning: "Downloaded copies remain under local custody and cannot be revoked.",
    projects: [
      {
        project: "cardio-icu",
        authorized: true,
        capabilities: ["evidence.read", "evidence.write", "approval"],
        head: 12,
        warning: "Custody notice applied",
      },
      {
        project: "restricted-study",
        authorized: false,
        reason: "access refused; role or grant denied",
      },
    ],
    ...overrides,
  };
}

export function defaultDiagnosisResult(overrides: Partial<HubDiagnosisResult> = {}): HubDiagnosisResult {
  return {
    state: "completed",
    passed: true,
    checks: [
      { name: "ca_certificate", passed: true, message: "CA certificate is valid" },
      { name: "client_certificate", passed: true, message: "client certificate file is readable" },
      { name: "client_key_reference", passed: true, message: "private key reference resolved successfully" },
      { name: "key_pair_match", passed: true, message: "certificate and private key match" },
      { name: "hub_endpoint", passed: true, message: "hub endpoint URL is well-formed", detail: "hub.customer.example:8443" },
      { name: "hub_tls_handshake", passed: true, message: "mutual TLS connection established" },
      { name: "hub_liveness", passed: true, message: "hub is live" },
      { name: "hub_readiness", passed: true, message: "hub is ready" },
      { name: "idp_configuration", passed: true, message: "IdP configuration is valid", detail: "https://idp.customer.example" },
    ],
    ...overrides,
  };
}

export function defaultArtifactsResult(overrides: Partial<HubArtifactsResult> = {}): HubArtifactsResult {
  return {
    state: "completed",
    project: "cardio-icu",
    head: 12,
    warning: "Custody notice applied",
    artifacts: [
      {
        digest: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
        resource: "evidence",
        kind: "record",
        actor: "lead@hospital.org",
        at: "2026-09-21T10:00:00Z",
        reason: "baseline study data",
      },
    ],
    ...overrides,
  };
}

