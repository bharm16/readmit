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
  BuildIndexResult,
  IndexDetails,
  IndexResult,
  TargetResult,
  TargetCheckResult,
  SecretsResult,
  SecretTestResult,
  SecretScanResult,
  SendPolicyResult,
  SendPolicyEvalResult,
  ResetPlanResult,
  TargetResetResult,
  Diagnosis,
  DiagnosisEvidence,
  DiagnosisFinding,
  DiagnosisFindingGroup,
  DiagnosisGroupsResult,
  DiagnosisResult,
  FindingPromotion,
  FindingReviewRecord,
  FindingReviewResult,
  FindingStatus,
  Normalization,
  NormalizationDifference,
  NormalizationRuleReport,
  NormalizeResult,
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
  built?: { output: string; identity: string },
): ReproducerResult {
  return {
    state: "completed",
    reproducer: built
      ? { plan, resolution, output: built.output, identity: built.identity }
      : { plan, resolution },
  };
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

/** Synthetic index details describing one index of a case. */
export function indexDetailsFixture(overrides: Partial<IndexDetails> = {}): IndexDetails {
  return {
    index_name: INDEX_ENTRY,
    case_name: CASE_ENTRY,
    identity: CASE_IDENTITY,
    retention: "states",
    retention_state: "active",
    fields: ["PID-3", "MSH-10"],
    records: 100,
    decoded: 98,
    undecodable: 2,
    built_at: "2026-09-21T00:00:00Z",
    applicable: true,
    ...overrides,
  };
}

/** Synthetic IndexResult reporting the index status of a case. */
export function indexResultFixture(
  details?: IndexDetails | null,
  overrides: Partial<IndexResult> = {},
): IndexResult {
  if (details === null) {
    return {
      state: "empty",
      ...overrides,
    };
  }
  return {
    state: "completed",
    index: details ?? indexDetailsFixture(),
    ...overrides,
  };
}

/** Synthetic BuildIndexResult reporting index build completion. */
export function buildIndexResultFixture(
  details?: IndexDetails,
  overrides: Partial<BuildIndexResult> = {},
): BuildIndexResult {
  return {
    state: "completed",
    index: details ?? indexDetailsFixture(),
    ...overrides,
  };
}

export function profilePackFixture(): import("../bindings").ProfilePackResult {
  return {
    state: "completed",
    pack: { id: "fixture-siu", version: "1" },
    provenance: {
      source: { name: "Fixture Author", location: "testdata/fixtures/profile-pack.json", revision: "1" },
      extraction: { method: "Manual authoring", content_digest: "sha256:0000" },
      license: { spdx: "LicenseRef-readmit-fixture", notice: "testdata/README.md" },
      rights_review: { status: "approved", reference: "testdata/README.md" },
    },
    coverage: [
      {
        hl7_version: "2.5.1",
        family: "SIU",
        parse: "supported",
        labels: "supported",
        structural: "unsupported",
        workflow: "unsupported",
      },
    ],
    bundleable: true,
  };
}

export function localProfileFixture(): import("../bindings").LocalProfileResult {
  return {
    state: "completed",
    profile: {
      schema: "readmit-local-profile/v1",
      profile: { id: "fixture-local-siu", version: "1" },
      base: { pack: { id: "fixture-siu", version: "1" }, hl7_version: "2.5.1", family: "SIU" },
      segments: [
        {
          id: "SCH",
          description: "Scheduling segment",
          cardinality: { min: 1, max: "1" },
          fields: [
            {
              position: 1,
              name: "Placer appointment number",
              usage: "R",
              cardinality: { min: 1, max: "1" },
              type: "EI",
            },
          ],
        },
      ],
    },
    resolution: {
      profile: { id: "fixture-local-siu", version: "1" },
      base: { pack: { id: "fixture-siu", version: "1" }, hl7_version: "2.5.1", family: "SIU" },
      pinned: true,
      support: { parse: "supported", labels: "supported", structural: "unsupported", workflow: "unsupported" },
      segments: [
        {
          id: "SCH",
          description: "Scheduling segment",
          site_defined: false,
          cardinality_origin: "local",
          fields: [
            {
              position: 1,
              name: "Placer appointment number",
              pack_name: "Placer Appointment Number",
              name_origin: "profile",
              usage: "R",
              usage_origin: "local",
              condition_origin: "undeclared",
              cardinality_origin: "local",
              type_origin: "local",
              terminology_origin: "undeclared",
              authority_origin: "undeclared",
              date_origin: "undeclared",
            },
          ],
        },
      ],
      findings: [],
    },
    seal: {
      schema: "readmit-profile-version/v1",
      profile: { id: "fixture-local-siu", version: "1" },
      content: { bytes: 3322, sha256: "e96a3350b728a78d063cf99afddeec3854d393682039ed4348fbc89057c55054" },
    },
  };
}

export function defaultTargetResult(overrides: Partial<TargetResult> = {}): TargetResult {
  return {
    state: "completed",
    target_file: "targets/default.json",
    target: {
      schema: "readmit-target/v3",
      name: "staging-mllp",
      classification: "nonproduction",
      address: "peer-under-test",
      transport: "plain",
      approved_transport: true,
      test_endpoint: true,
      connect_timeout: "5s",
      message_timeout: "10s",
      max_ack_bytes: 1024,
      credential: {
        secrets_file: "secrets.json",
        reference: "mllp-basic-auth",
      },
    },
    ...overrides,
  };
}

export function defaultTargetCheckResult(overrides: Partial<TargetCheckResult> = {}): TargetCheckResult {
  return {
    state: "completed",
    report: {
      name: "staging-mllp",
      classification: "isolated_testing",
      peer: "peer-under-test",
      outcome: "connected",
      phase: "established",
      unsolicited: 0,
    },
    decision: {
      schema: "readmit-send-decision/v1",
      allowed: true,
      reason: "address matches approved destination list",
      address: "peer-under-test",
      classification: "isolated_testing",
      explicit_send: true,
      policy_selected: true,
      approved_destinations: ["approved-peer"],
      resolved_addresses: ["peer-under-test"],
      decided_at: "2026-09-21T12:00:00Z",
    },
    ...overrides,
  };
}

export function defaultSecretsResult(overrides: Partial<SecretsResult> = {}): SecretsResult {
  return {
    state: "completed",
    secrets_file: "secrets.json",
    document: {
      schema: "readmit-secrets/v1",
      references: [
        {
          name: "mllp-basic-auth",
          store: "os-keychain",
          purpose: "mllp-endpoint",
          address: "peer-under-test",
          command: "locator",
          arguments: ["named-reference"],
          generation: 1,
          rotated_at: "2026-09-21T12:00:00Z",
          max_age: "720h",
        },
      ],
    },
    ...overrides,
  };
}

export function defaultSecretTestResult(overrides: Partial<SecretTestResult> = {}): SecretTestResult {
  return {
    state: "completed",
    name: "mllp-basic-auth",
    success: true,
    ...overrides,
  };
}

export function defaultSecretScanResult(overrides: Partial<SecretScanResult> = {}): SecretScanResult {
  return {
    state: "completed",
    skipped: 0,
    scan: {
      status: "clean",
      files_checked: 5,
      known_values_checked: 1,
      unresolved_locations: [],
      limitations: "Checked known secret hashes across workspace",
    },
    ...overrides,
  };
}

export function defaultSendPolicyResult(overrides: Partial<SendPolicyResult> = {}): SendPolicyResult {
  return {
    state: "completed",
    policy_file: "send-policy.json",
    policy: {
      schema: "readmit-send-policy/v1",
      approved_destinations: ["approved-peer", "second-peer"],
    },
    ...overrides,
  };
}

export function defaultSendPolicyEvalResult(overrides: Partial<SendPolicyEvalResult> = {}): SendPolicyEvalResult {
  return {
    state: "completed",
    decision: {
      schema: "readmit-send-decision/v1",
      allowed: true,
      reason: "address matches approved destination list",
      address: "peer-under-test",
      classification: "isolated_testing",
      explicit_send: true,
      policy_selected: true,
      approved_destinations: ["approved-peer"],
      resolved_addresses: ["peer-under-test"],
      decided_at: "2026-09-21T12:00:00Z",
    },
    ...overrides,
  };
}

export function defaultResetPlanResult(overrides: Partial<ResetPlanResult> = {}): ResetPlanResult {
  return {
    state: "completed",
    plan_file: "reset-plan.json",
    plan: {
      schema: "readmit-reset-plan/v1",
      environment: "staging-mllp",
      actions: [
        {
          id: "step-1",
          operator: "operator_confirms",
          authority: "none",
          instructions: "Confirm patient database is wiped.",
        },
        {
          id: "step-2",
          operator: "observation_empty",
          authority: "read_declared_file",
          instructions: "Check observation file is empty.",
          observation: "inbox.json",
        },
      ],
    },
    ...overrides,
  };
}

export function defaultTargetResetResult(overrides: Partial<TargetResetResult> = {}): TargetResetResult {
  return {
    state: "completed",
    result: {
      schema: "readmit-reset-outcome/v1",
      state: "completed",
      outcome: "succeeded",
      reason: "all reset actions executed successfully",
      environment: "staging-mllp",
      classification: "isolated_testing",
      plan_sha256: "abc123def456",
      actions: [
        {
          id: "step-1",
          operator: "operator_confirms",
          authority: "none",
          outcome: "confirmed",
          reason: "operator confirmed",
        },
        {
          id: "step-2",
          operator: "observation_empty",
          authority: "read_declared_file",
          outcome: "observed",
          reason: "file empty",
        },
      ],
      attempted_at: "2026-09-21T12:00:00Z",
    },
    ...overrides,
  };
}

export function scenarioCatalogFixture() {
  return {
    state: "completed" as const,
    catalog: {
      generator_version: "readmit-scenario-generator-v1",
      profiles: [
        {
          name: "readmit-siu-lifecycle-v1",
          schema: "readmit-scenario/v1",
          order: false,
          kinds: [
            { kind: "patient", states: ["active"] },
            { kind: "appointment", states: ["none", "booked", "cancelled", "noshow"] },
          ],
          events: [
            { event: "S12", description: "new appointment booking", kind: "appointment", profile: "readmit-siu-lifecycle-v1", available: true },
          ],
        },
      ],
      all_events: [
        {
          event: "A01",
          description: "admit or visit notification",
          kind: "visit",
          profile: "readmit-siu-lifecycle-v1",
          available: false,
          reason: "profile readmit-siu-lifecycle-v1 declares no event A01; it belongs to readmit-adt-lifecycle-v1",
        },
      ],
    },
  };
}

export function scenarioPreviewFixture(options: { reveal?: boolean } = {}) {
  return {
    state: "completed" as const,
    scenario: "siu-draft",
    version: "1",
    profile: "readmit-siu-lifecycle-v1",
    base_time: "2026-01-01T12:00:00Z",
    accepted: 1,
    refused: 0,
    subjects: options.reveal
      ? [
          {
            id: "patient-a",
            kind: "patient",
            initial_state: "active",
            masked: false,
            namespace: "READMIT",
            identifier: "SYNTH-PATIENT-A",
          },
        ]
      : [
          {
            id: "patient-a",
            kind: "patient",
            initial_state: "active",
            masked: true,
          },
        ],
    steps: [
      {
        ordinal: 1,
        id: "book",
        at: "2026-01-01T12:00:00Z",
        event: "S12",
        description: "new appointment booking",
        subject: "appointment-a",
        expect: "accepted",
        from: "none",
        to: "booked",
      },
    ],
  };
}

/** The synthetic diagnosis report entry the diagnosis journeys write, and the
 * fixed identity token a finding review names. */
export const REPORT_ENTRY = "diagnosis-report";
export const REPORT_SHA256 = "report-sha256-fixed-for-tests";

/** One evidence reference of one finding: a position and a state, no value. */
export function diagnosisEvidence(
  occurrence: string = GRID_OCCURRENCE,
  field = "MSH-10",
): DiagnosisEvidence {
  return { occurrence, field, state: "present", offset: null, length: null };
}

/** One finding of a diagnosis: rule, classification and evidence positions. */
export function diagnosisFinding(
  id: string,
  ruleId = "ack.msa-outcome",
  overrides: Partial<DiagnosisFinding> = {},
): DiagnosisFinding {
  return {
    id,
    rule_id: ruleId,
    classification: "protocol",
    profile: "readmit-siu-v1",
    ruleset: "readmit-siu-diagnosis/v1",
    summary: "The acknowledgement outcome is not what the ruleset expects.",
    evidence: [diagnosisEvidence()],
    ...overrides,
  };
}

/** One diagnosis report windowed for the panes: counts, rules and evidence
 * positions, never a message byte or a field value. */
export function diagnosisResult(
  findings: DiagnosisFinding[],
  overrides: Partial<Diagnosis> = {},
): DiagnosisResult {
  return {
    state: "completed",
    output: REPORT_ENTRY,
    diagnosis: {
      case: CASE_ENTRY,
      report_sha256: REPORT_SHA256,
      schema: "readmit-diagnosis/v1",
      profile: "readmit-siu-v1",
      ruleset: "readmit-siu-diagnosis/v1",
      rules: ["message.duplicate-control-id", "ack.msa-outcome"],
      window: {
        description: "The observed window is what the capture recorded, never a complete lifecycle.",
        occurrences: 2,
        observed_start: "2026-01-01T12:00:00Z",
        observed_end: "2026-01-01T12:01:00Z",
        unknown_observed_times: 0,
        sources: [
          { source_id: "s0001", first_occurrence: GRID_OCCURRENCE, last_occurrence: NEXT_OCCURRENCE },
        ],
      },
      scope: "A diagnosis describes the capture window, never a complete lifecycle.",
      offset: 0,
      total: findings.length,
      findings,
      unsupported: [],
      ...overrides,
    },
  };
}

/** Findings of several cases grouped by equal signature. */
export function diagnosisGroupsResult(groups: DiagnosisFindingGroup[]): DiagnosisGroupsResult {
  return {
    state: "completed",
    offset: 0,
    total: groups.length,
    groups: {
      schema: "readmit-diagnosis-groups/v1",
      scope: "Equal signatures mean the same diagnostic shape, never the same root cause.",
      cases: [],
      groups,
    },
  };
}

/** What one confirmed finding promotes to. The expected text is a synthetic
 * literal, never a value read out of evidence. */
export function findingPromotion(messages: string[] = [GRID_OCCURRENCE]): FindingPromotion {
  return {
    messages,
    expectations: [
      {
        id: "promoted-expectation",
        operator: "ack_field_equals",
        message: messages[0] ?? "",
        selector: "MSA-1",
        field: { state: "present", text: "expected-token" },
      },
    ],
    unsupported: [],
  };
}

/** One finding as a review leaves it: verdict, basis and what it promotes to. */
export function findingStatus(
  finding: string,
  verdict: string,
  overrides: Partial<FindingStatus> = {},
): FindingStatus {
  return {
    finding,
    rule_id: "ack.msa-outcome",
    classification: "protocol",
    verdict,
    basis: verdict === "not_reviewed" ? "unreviewed" : "decision",
    next_evidence: "Capture the acknowledgement the case does not hold.",
    ...overrides,
  };
}

/** One finding review joined to its diagnosis, bound by fixed identity tokens. */
export function findingReviewResult(
  statuses: FindingStatus[],
  overrides: Partial<FindingReviewRecord> = {},
  result: Partial<FindingReviewResult> = {},
): FindingReviewResult {
  return {
    state: "completed",
    review: {
      record: {
        schema: "readmit-finding-review/v1",
        engine: "readmit",
        diagnosis: {
          schema: "readmit-diagnosis/v1",
          report_sha256: REPORT_SHA256,
          case_identity: CASE_IDENTITY,
          config_sha256: "config-sha256-fixed-for-tests",
          profile: "readmit-siu-v1",
          ruleset: "readmit-siu-diagnosis/v1",
        },
        decisions_sha256: "decisions-sha256-fixed-for-tests",
        boundary: "ack-contract",
        findings: statuses,
        statement:
          "The machine's findings and one person's judgment of them, joined but distinguishable.",
        ...overrides,
      },
      offset: 0,
      total: statuses.length,
    },
    ...result,
  };
}

/** One authored normalization rule as it was applied: counts only. */
export function normalizationRuleReport(
  id: string,
  selector: string,
  overrides: Partial<NormalizationRuleReport> = {},
): NormalizationRuleReport {
  return {
    id,
    selector,
    operator: "ignore",
    compared: 1,
    suppressed: 1,
    retained: 0,
    undecided: 0,
    ...overrides,
  };
}

/** One field the comparison reported and what the policy did about it. */
export function normalizationDifference(
  selector: string,
  outcome: string,
  overrides: Partial<NormalizationDifference> = {},
): NormalizationDifference {
  return {
    left_occurrence: GRID_OCCURRENCE,
    right_occurrence: "occ-000001",
    selector,
    status: "changed",
    left_state: "present",
    right_state: "present",
    outcome,
    ...overrides,
  };
}

/** One policy-scoped reading of one comparison, windowed: outcomes and counts,
 * never a value. The raw comparison stays its own unchanged report. */
export function normalizeResult(
  differences: NormalizationDifference[],
  rules: NormalizationRuleReport[] = [],
  overrides: Partial<Normalization> = {},
): NormalizeResult {
  const outcomes = (wanted: string) =>
    differences.filter((entry) => entry.outcome === wanted).length;
  return {
    state: "completed",
    normalization: {
      left: CASE_ENTRY,
      right: OTHER_CASE_ENTRY,
      policy: "normalization-policy.json",
      policy_sha256: "policy-sha256-fixed-for-tests",
      report: "readmit-normalization/v1",
      policy_schema: "readmit-normalization-policy/v1",
      scope: "A policy-scoped reading of one comparison; the raw comparison is unchanged.",
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
      keys: [],
      fields: [],
      rules,
      summary: {
        paired: differences.length,
        differences: differences.length,
        uncompared: 0,
        suppressed: outcomes("suppressed"),
        retained: outcomes("retained"),
        undecided: outcomes("undecided"),
        unaddressed: outcomes("unaddressed"),
        inserted: 0,
        missing: 0,
        ambiguous: 0,
        unaligned: 0,
      },
      offset: 0,
      limit: 200,
      total: differences.length,
      differences,
      unsupported: [],
      ...overrides,
    },
  };
}
