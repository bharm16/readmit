// Typed fixtures for the facade's operation states, in the vocabulary of
// docs/desktop.md: empty, busy, cancelled, failed, permission_denied,
// completed. Every value here is synthetic — entry names, fixed identity
// tokens, counts and states the engine could have reported. No HL7 field
// value, message byte, real folder, credential or developer-machine path is
// in any of them, because what a component does with domain meaning is not
// these tests' subject: the Go readers decide that, and the Go tests prove it.
import type {
  SuiteRunJob,
  Artifact,
  CaseResult,
  EditorDraft,
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
  RunComparisonResult,
  Sequence,
  SequenceResult,
  SessionResult,
  Shell,
  ShellResult,
  Vocabulary,
  DisclosureStatusResult,
  StatusValue,
  RunState,
  TestRunnerStatus,
  TestDraftDocument,
  TestResult,
  TestResolution,
  HubResult,
  HubTeamResult,
  HubDiagnosisResult,
  HubArtifactsResult,
  BuildIndexResult,
  MessageRow,
  MessagesResult,
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
  FindingRow,
  DiagnosisFindingGroup,
  DiagnosisGroupsResult,
  DiagnosisResult,
  FindingPromotion,
  FindingStatus,
  RunPreflightResult,
  RunProgressResult,
  RunEvidenceResult,
  SuiteRunResult,
  RunSpecChoiceResult,
  ConnectedTestContext,
  SuiteTestVersion,
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
    "prepared-rerun",
    "unsupported",
    "open",
    "investigating",
    "resolved",
    "closed",
  ];
  return statuses.map((status, position) => ({
    status,
    symbol: String.fromCharCode(0x25a0 + position),
    label: status === "prepared-rerun" ? "Prepared rerun workspace" : status,
  }));
}

/** The window's description of itself, as the facade answers `Shell`. */
export function shellResult(): ShellResult {
  const shell: Shell = {
    regions: [
      { id: "navigation", label: "Navigation" },
      { id: "evidence", label: "Main content" },
      { id: "inspector", label: "Details" },
    ],
    indicators: indicatorFixtures(),
    commands: [
      { id: "command-palette", title: "Search commands", keys: "Ctrl+K" },
      { id: "search-workspace", title: "Search this project", keys: "Ctrl+F", region: "evidence" },
      { id: "new-project", title: "New project", region: "evidence" },
      { id: "open-workspace", title: "Open project", keys: "Ctrl+O", region: "evidence" },
      { id: "create-test", title: "Create test", region: "evidence" },
      { id: "create-report", title: "Create report", region: "evidence" },
      { id: "maintain-workspace", title: "Storage", region: "evidence" },
      { id: "check-staged-upgrade", title: "Updates", region: "evidence" },
      { id: "inspect-raw-file", title: "Inspect file", region: "evidence" },
      { id: "performance-corpus", title: "Benchmarks", region: "evidence" },
      { id: "cancel-operation", title: "Cancel operation" },
    ],
    themes: ["system", "light", "dark"],
    text_scales: [50, 75, 90, 100, 125, 150, 175, 200],
    version: "0.0.0-test",
    build: {
      version: "0.0.0-test",
      revision: "3527e801aa0b9d6f2c5e1c9f0f4f8e7d6c5b4a39",
      built_at: "2026-09-27T12:00:00Z",
      modified: false,
      channel: "Development preview, unsigned",
    },
    privacy: {
      statement: "Nothing leaves this machine.",
      absent: ["No telemetry, crash reporting or update check."],
      kept: [
        "Recent folders, saved filters and the working session, in this user's own configuration.",
      ],
      operations: [
        {
          id: "run",
          activity: "Durable test execution",
          destination: "The fixture's test target, under an approved send policy.",
          data: "The messages the executed test declares.",
          authorization: "An activated license, an approved policy and an explicit execute.",
        },
        {
          id: "runner",
          activity: "Hub-enrolled runner execution",
          destination: "The hub the runner enrollment names.",
          data: "The run records and artifacts the enrolled schedule covers.",
          authorization: "A runner enrollment and the schedule you enabled.",
        },
        {
          id: "capture",
          activity: "Capture and source collection",
          destination: "The saved capture source, or the address its listener serves.",
          data: "The bytes the source delivers, into a new local case bundle.",
          authorization: "A saved capture source and an explicit Start capture.",
        },
        {
          id: "observe",
          activity: "Observation windows",
          destination: "The validated export file or approved HTTPS API.",
          data: "The records the window reads within its declared scope.",
          authorization: "A validated source and window pair, and an explicit start.",
        },
        {
          id: "hub",
          activity: "Customer artifact hub",
          destination: "The hub the selected configuration names.",
          data: "The artifacts a person uploads or downloads deliberately.",
          authorization: "A selected configuration and a per-session sign-in.",
        },
        {
          id: "declared-program",
          activity: "Operator-declared programs",
          destination: "Whatever the program is configured to reach; Readmit cannot see or vouch for it.",
          data: "Only the arguments the operator declared.",
          authorization: "A program declared by absolute path, and the action that needs it.",
        },
        {
          id: "portal",
          activity: "Commercial portal",
          destination: "The portal address the destinations file names, in your browser.",
          data: "Nothing from this window.",
          authorization: "Your deliberate choice to open the link.",
        },
      ],
    },
    support: {
      notes: [
        "Connector support is declared, not qualified.",
        "Database observation is selected and unqualified.",
      ],
      unavailable: ["generate reproducible SIU synthetic case bundles from declared inputs"],
    },
    vocabulary: vocabularyFixture(),
  };
  return { state: "completed", shell };
}

/** What the window offers and pages by, as the facade publishes it in the
 * window's description. A test that is about one of these passes its own. */
export function vocabularyFixture(bounds: Partial<Vocabulary["bounds"]> = {}): Vocabulary {
  return {
    connected_tests: {
      boundaries: ["engine-output", "application-state"],
      operators: [
        { operator: "value-equals", types: [] },
        { operator: "decimal-equals", types: ["decimal"] },
        { operator: "instant-equals", types: ["date", "datetime"] },
        { operator: "value-related", types: [] },
        { operator: "value-changed", types: [] },
        { operator: "row-count", types: [] },
        { operator: "unique-keys", types: [] },
        { operator: "each-equals", types: [] },
        { operator: "sequence-equals", types: [] },
      ],
      methods: ["POST", "PUT", "DELETE", "GET"],
      preferences: ["return=minimal", "return=representation", "return=OperationOutcome"],
      response_outcomes: ["succeeded", "not-modified", "conflict", "not-found", "pending", "rejected", "rejected-transaction", "partial-failure", "unauthorized", "forbidden", "throttled", "unavailable"],
      ack_codes: ["AA", "AE", "AR", "CA", "CE", "CR"],
      variable_kinds: ["literal", "synthetic-id", "timestamp", "response"],
      binding_froms: ["logical-id", "version-id"],
      binding_scopes: ["phase", "lifecycle"],
      phase_requires: ["pass", "complete"],
      condition_results: ["passed", "failed", "skipped"],
      quantifiers: ["every", "any", "none"],
    },
    connected: {
      connection: { schema: "readmit-fhir-connection/v1", base: "", version: "4.0.1", classification: "unclassified", authentication: "", server_name: "" },
      observation: {
        schema: "readmit-connected-observation-setup/v1", namespace: "observed", phase: "both", business_keys: [], baseline: "before-run",
        completion: { schema: "readmit-observation-interval/v1", source: "", namespace: "", enabled: true, mode: "snapshots", freshness: "snapshot-only", horizon_ms: 30000, sample_ms: 1000, max_gap_ms: 3000, max_samples: 64, max_records: 1000, max_bytes: 16777216 },
      },
      search: { resource: "Appointment", boundary: "", criteria: [], fields: [], budget: { pages: 16, rows: 1000, bytes: 16777216, timeout_ms: 30000 } },
      resources: [{ resource: "Appointment", fields: [
        { id: "status", type: "code", code_system: "http://hl7.org/fhir/appointmentstatus", selector: { steps: [{ field: "status", each: false }] }, repeated: false },
        { id: "start", type: "datetime", selector: { steps: [{ field: "start", each: false }] }, repeated: false },
      ] }],
      value_types: ["text", "decimal", "boolean", "date", "datetime", "code"], phases: ["before", "after", "both"],
      boundaries: ["authoritative-application-api", "delayed-replica", "reference-fhir-store"], baseline_modes: ["before-run"],
    },
    diagnosis_builtins: [
      { id: "siu", name: "SIU", profile: "readmit-siu-v1", ruleset: "readmit-siu-diagnosis/v1" },
      { id: "lifecycle", name: "Lifecycle", profile: "readmit-lifecycle-v1", ruleset: "readmit-lifecycle-diagnosis/v1" },
      { id: "order", name: "Orders", profile: "readmit-order-v1", ruleset: "readmit-order-diagnosis/v1" },
    ],
    import_plan: {
      framings: ["raw", "mllp", "batch"],
      payload_framings: ["raw", "mllp"],
      boundaries: ["segment-start", "hl7-batch"],
      terminators: ["cr", "lf", "crlf"],
      encodings: ["utf-8", "us-ascii", "iso-8859-1", "unknown"],
      directions: ["inbound", "outbound", "unknown"],
    },
    reset_operators: [
      { operator: "operator_confirms", authority: "none" },
      { operator: "observation_empty", authority: "read_declared_file" },
      { operator: "collection_empty", authority: "read_declared_file" },
      { operator: "endpoint_quiet", authority: "connect_approved_target" },
    ],
    receiver_faults: {
      actions: [
        { action: "delay", waits: true },
        { action: "reject", waits: false },
        { action: "malformed-ack", waits: false },
        { action: "missing-response", waits: true },
        { action: "disconnect", waits: false },
      ],
      default_delay_ms: 50,
    },
    bounds: { grid: 200, comparison: 200, review: 200, sequence: 200, diagnosis: 200, ...bounds },
    ack_positions: [
      "MSA-1",
      "MSA-2",
      "MSA-3",
      "MSA-4",
      "MSA-5",
      "MSA-6",
      "ERR-1",
      "ERR-2",
      "ERR-3",
      "ERR-4",
      "ERR-5",
      "ERR-6",
      "ERR-7",
      "ERR-8",
      "ERR-9",
      "ERR-10",
      "ERR-11",
      "ERR-12",
    ],
    checks: {operators: ["field_equals", "field_not_equals", "field_state", "text_matches", "numeric_range", "numeric_tolerance", "date_window", "values_equal", "record_count", "records_unique", "records_contain", "records_ordered", "record_multiplicity", "records_absent", "record_key_matches", "records_changed"], message_scopes: ["input", "observed"], record_scopes: ["before", "after"], quantifiers: ["every", "any", "none"], field_states: ["present", "empty", "null", "omitted"]},
    profiles: {hl7_versions: ["2.3.1", "2.4", "2.5", "2.5.1", "2.6", "2.7.1", "2.8.2"], families: ["ADT", "SIU", "ORM", "ORU"], usages: ["R", "RE", "C", "O", "X"], data_types: ["AD", "CE", "CF", "CNE", "CP", "CQ", "CWE", "CX", "DLN", "DR", "DT", "DTM", "ED", "EI", "EIP", "FN", "FT", "HD", "ID", "IS", "MO", "MSG", "NM", "PL", "PT", "RP", "SAD", "SI", "SN", "ST", "TM", "TS", "TX", "VID", "XAD", "XCN", "XON", "XPN", "XTN"], condition_operators: ["present", "absent", "value_in"], bindings: ["required", "suggested"], universal_id_types: ["DNS", "GUID", "HCD", "HL7", "ISO", "L", "M", "N", "Random", "URI", "UUID", "x400", "x500"], precisions: ["year", "month", "day", "hour", "minute", "second", "fraction"], timezone_rules: ["required", "optional", "forbidden"], unbounded: "*", origins: ["profile", "overridden", "local", "undeclared"], support_outcomes: ["supported", "untested", "unsupported", "unknown"]},
    scenarios: {
      catalog: {profiles: [{name: "readmit-siu-lifecycle-v1", kinds: [{kind: "appointment", states: ["booked", "cancelled", "none", "noshow"]}, {kind: "patient", states: ["active"]}], events: [{event: "S12", description: "new appointment booking", kind: "appointment", profile: "readmit-siu-lifecycle-v1", available: true}, {event: "S13", description: "appointment rescheduling", kind: "appointment", profile: "readmit-siu-lifecycle-v1", available: true}, {event: "S14", description: "appointment modification", kind: "appointment", profile: "readmit-siu-lifecycle-v1", available: true}, {event: "S15", description: "appointment cancellation", kind: "appointment", profile: "readmit-siu-lifecycle-v1", available: true}, {event: "S26", description: "patient did not show up for appointment", kind: "appointment", profile: "readmit-siu-lifecycle-v1", available: true}], schema: "readmit-scenario/v1", order: false}], generator_version: "readmit-scenario-generator-v1", all_events: [{event: "A01", description: "admit or visit notification", kind: "visit", profile: "readmit-siu-lifecycle-v1", available: false, reason: "profile readmit-siu-lifecycle-v1 declares no event A01; it belongs to readmit-adt-lifecycle-v1"}, {event: "A02", description: "transfer a patient", kind: "visit", profile: "readmit-siu-lifecycle-v1", available: false, reason: "profile readmit-siu-lifecycle-v1 declares no event A02; it belongs to readmit-adt-lifecycle-v1"}, {event: "A03", description: "discharge or end visit", kind: "visit", profile: "readmit-siu-lifecycle-v1", available: false, reason: "profile readmit-siu-lifecycle-v1 declares no event A03; it belongs to readmit-adt-lifecycle-v1"}, {event: "A04", description: "register a patient", kind: "visit", profile: "readmit-siu-lifecycle-v1", available: false, reason: "profile readmit-siu-lifecycle-v1 declares no event A04; it belongs to readmit-adt-lifecycle-v1"}, {event: "A08", description: "update patient information", kind: "visit", profile: "readmit-siu-lifecycle-v1", available: false, reason: "profile readmit-siu-lifecycle-v1 declares no event A08; it belongs to readmit-adt-lifecycle-v1"}, {event: "A11", description: "cancel admit or visit notification", kind: "visit", profile: "readmit-siu-lifecycle-v1", available: false, reason: "profile readmit-siu-lifecycle-v1 declares no event A11; it belongs to readmit-adt-lifecycle-v1"}, {event: "A13", description: "cancel discharge or end visit", kind: "visit", profile: "readmit-siu-lifecycle-v1", available: false, reason: "profile readmit-siu-lifecycle-v1 declares no event A13; it belongs to readmit-adt-lifecycle-v1"}, {event: "A40", description: "merge patient identifier list", kind: "patient", profile: "readmit-siu-lifecycle-v1", available: false, reason: "profile readmit-siu-lifecycle-v1 declares no event A40; it belongs to readmit-adt-lifecycle-v1"}, {event: "ORM-CA", description: "cancel order request", kind: "order", profile: "readmit-siu-lifecycle-v1", available: false, reason: "profile readmit-siu-lifecycle-v1 declares no event ORM-CA; it belongs to readmit-orm-lifecycle-v1"}, {event: "ORM-NW", description: "new order request", kind: "order", profile: "readmit-siu-lifecycle-v1", available: false, reason: "profile readmit-siu-lifecycle-v1 declares no event ORM-NW; it belongs to readmit-orm-lifecycle-v1"}, {event: "ORM-XO", description: "change order request", kind: "order", profile: "readmit-siu-lifecycle-v1", available: false, reason: "profile readmit-siu-lifecycle-v1 declares no event ORM-XO; it belongs to readmit-orm-lifecycle-v1"}, {event: "ORU-C", description: "corrected result report", kind: "order", profile: "readmit-siu-lifecycle-v1", available: false, reason: "profile readmit-siu-lifecycle-v1 declares no event ORU-C; it belongs to readmit-oru-lifecycle-v1"}, {event: "ORU-F", description: "final result report", kind: "order", profile: "readmit-siu-lifecycle-v1", available: false, reason: "profile readmit-siu-lifecycle-v1 declares no event ORU-F; it belongs to readmit-oru-lifecycle-v1"}, {event: "ORU-P", description: "preliminary result report", kind: "order", profile: "readmit-siu-lifecycle-v1", available: false, reason: "profile readmit-siu-lifecycle-v1 declares no event ORU-P; it belongs to readmit-oru-lifecycle-v1"}]},
      templates: [{id: "siu-basic", name: "Booking and reschedule", profile: "readmit-siu-lifecycle-v1", family: "SIU", subjects: [{id: "patient-a", kind: "patient", namespace: "READMIT", identifier: "SYNTH-PATIENT-A", initial_state: "active"}, {id: "appointment-a", kind: "appointment", namespace: "READMIT", identifier: "SYNTH-APPOINTMENT-A", patient: "patient-a", initial_state: "none"}], steps: [{id: "booking", event: "S12", subject: "appointment-a", after: "0s", expect: "accepted"}, {id: "reschedule", event: "S13", subject: "appointment-a", after: "10m", expect: "accepted"}]}],
      expectations: ["accepted", "refused"],
      encodings: ["utf-8", "iso-8859-1"],
      generator_version: "readmit-scenario-generator-v1",
      max_seed: 9007199254740991,
    },
    fixture_modes: ["fixed", "defective"],
    affected_test_impacts: ["affected", "unaffected", "current", "unrelated", "unknown"],
    observation_starts: [{"schema": "readmit-observation-source/v1", "source": {"kind": "file-export", "identity": "scheduling-archive", "scope": "appointments"}, "enabled": true, "freshness": {"max_age": "1h"}, "extraction": {"envelope": "csv", "encoding": "utf-8", "csv": {"delimiter": ",", "record_separator": "lf", "header": "present", "fields": 2}, "record_key": []}, "file": {"path": "", "max_bytes": 65536}, "http": null, "capture": null}, {"schema": "readmit-observation-source/v1", "source": {"kind": "http-api", "identity": "scheduling-archive", "scope": "appointments"}, "enabled": true, "freshness": {"max_age": "1h"}, "extraction": {"envelope": "json", "encoding": "utf-8", "json": {"record_path": []}, "record_key": []}, "file": null, "http": {"url": "", "classification": "unclassified", "ca_file": "", "server_name": "", "timeout": "10s", "max_bytes": 1048576, "retry": {"attempts": 1, "delay": "1s"}, "credential": null}, "capture": null}, {"schema": "readmit-observation-source/v2", "source": {"kind": "downstream-capture", "identity": "scheduling-archive", "scope": "appointments"}, "enabled": true, "freshness": {"max_age": "1h"}, "extraction": null, "file": null, "http": null, "capture": {"path": "", "kinds": ["message"], "record_key": "", "max_occurrences": 1000}}, {"schema": "readmit-observation-source/v3", "source": {"kind": "database-query", "identity": "scheduling-archive", "scope": "appointments"}, "enabled": true, "freshness": {"max_age": "1h"}, "extraction": null, "file": null, "http": null, "capture": null, "database": {"driver": "postgresql", "address": "", "classification": "unclassified", "name": "", "username": "", "ca_file": "", "server_name": "", "credential": {"store": "os-keychain", "address": "", "purpose": "database-observation", "command": "", "arguments": []}, "view": [], "record_key": "", "key_type": "text", "filters": [], "limits": null}}],
    coverage: { declared_coverages: ["partial", "complete"], retry_bases: ["operator_reported_retry"], max_clock_tolerance_seconds: 86400 },
    import_engines: [
      { engine: "mirth", name: "Mirth Connect", version: "4.5.2", formats: ["raw", "message-xml"], terminators: ["cr", "lf", "crlf"] },
      { engine: "oie", name: "Open Integration Engine", version: "4.6.0", formats: ["raw", "message-xml"], terminators: ["cr", "lf", "crlf"] },
    ],
    capture_source_starts: [{"type": "local-folder", "evidence": {"schema": "readmit-source/v1", "name": "", "kind": "directory", "scope": "", "quota": {"max_entries": 64, "max_entry_bytes": 4194304, "max_total_bytes": 33554432}, "retry": {"attempts": 1, "backoff": "250ms"}}, "plan": {"schema": "readmit-import-plan/v1", "framing": "raw", "terminator": "cr", "encoding": "utf-8", "direction": "inbound", "members": [".hl7", ".mllp"]}}, {"type": "transfer", "evidence": {"schema": "readmit-source/v1", "name": "", "kind": "transfer", "scope": "", "quota": {"max_entries": 64, "max_entry_bytes": 4194304, "max_total_bytes": 33554432}, "retry": {"attempts": 1, "backoff": "250ms"}, "classification": "unclassified"}, "plan": {"schema": "readmit-import-plan/v1", "framing": "raw", "terminator": "cr", "encoding": "utf-8", "direction": "inbound", "members": [".hl7", ".mllp"]}}, {"type": "mllp-listener", "listener": {"schema": "readmit-capture-listener/v1", "bind_address": "127.0.0.1", "port": 0, "transport": "plain", "message_limit": 0, "connection_limit": 1, "idle_timeout": "30s", "ack_code": "AA", "allow_remote": false}, "responder_choices": {"name": "listener", "source_label": "listener", "accepted_message_types": {"operator": "any-message-type", "values": []}, "enhanced": false, "fault": "none", "fault_delay_ms": 50}}],
    capture_source_types: [
      { type: "local-folder", available: true },
      { type: "transfer", available: true },
      { type: "mllp-listener", available: true },
      { type: "api", available: false },
    ],
    listener_transports: ["plain", "tls", "mutual-tls"],
    ack_codes: ["AA", "AE", "AR"],
    import_time_operators: ["unknown", "rfc3339", "unix-seconds", "unix-milliseconds", "hl7-dtm"],
  };
}

/** The live half of the privacy disclosure: how each disclosed activity
 * stands right now, as the facade answers it without contacting anything. */
export function disclosureStatusResult(
  states: DisclosureStatusResult["states"] = [
    { id: "run", state: "idle", detail: "No run is in progress." },
    { id: "runner", state: "idle", detail: "No recurring execution is in progress." },
    { id: "capture", state: "idle", detail: "No capture or collection is in progress." },
    { id: "observe", state: "idle", detail: "No observation window is open." },
    { id: "hub", state: "not-configured", detail: "No hub configuration is selected." },
    { id: "declared-program", state: "idle", detail: "No operator-declared program is running." },
    { id: "portal", state: "not-configured", detail: "No destinations file is selected." },
  ],
): DisclosureStatusResult {
  return { state: "completed", states };
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

/** One window of a case's messages under a transient query: positions,
 * types and counts, never a value. */
export function messagesResult(rows: MessageRow[], overrides: Partial<MessagesResult> = {}): MessagesResult {
  return {
    state: rows.length === 0 ? "empty" : "completed",
    rows,
    total: rows.length,
    matched: rows.length,
    undecided: 0,
    undecodable: 0,
    complete: true,
    scanned: rows.length,
    facets: {
      types: [
        { kind: "message", code: "SIU", trigger: "S12" },
        { kind: "ack", code: "", trigger: "" },
      ],
      sources: [
        { id: "s0001", name: "" },
        { id: "s0002", name: "" },
      ],
      ack_codes: ["AA", "AE"],
    },
    search_index: "",
    ...overrides,
  };
}

/** A message row: where the occurrence is, what it is and when it was seen. */
export function messageRow(
  id: string,
  kind: "message" | "ack" | "unparsed" = "message",
  overrides: Partial<MessageRow> = {},
): MessageRow {
  return {
    ...gridRow(id, kind),
    source_name: "",
    sequence: 0,
    message_code: kind === "message" ? "SIU" : "",
    trigger_event: kind === "message" ? "S12" : "",
    ...overrides,
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
      message: 0,
      source_id: "s0001",
      source_name: "",
      source_offset: 0,
      size: 256,
      message_code: "SIU",
      trigger_event: "S12",
      observed_at: null,
      selected,
      selector: "",
      segment_name: "",
      children: [],
      node_offset: 0,
      child_count: 0,
      bytes: [],
      byte_offset: 0,
      revealed: false,
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
    { id: "sample", title: "Create sample workspace", detail: "Writes synthetic evidence.", done: false },
    { id: "test", title: "Create sample test", detail: "Answers the authoring stages.", done: false },
    { id: "baseline", title: "Run failing example", detail: "The defect makes it fail.", done: false },
    { id: "post-fix", title: "Run fixed example", detail: "The same test passes.", done: false },
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
export function practiceResult(trial: "baseline" | "post-fix", status: TestRunnerStatus): PracticeResult {
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
    context: { project: WORKSPACE_ROOT, generation: 0 },
    sequence: {
      context: { project: WORKSPACE_ROOT, generation: 0 },
      basis: "observed",
      clocks: [{ id: "session", kind: "session", sources: ["s0001"] }],
      untimed: 0,
      relations: [],
      problems: { unresolved_links: 0, gaps: 0 },
      fields: [],
      case: CASE_ENTRY,
      identity: CASE_IDENTITY,
      rules: "",
      session_declared: false,
      declared: [],
      unsupported: [],
      lanes: [
        {
          source_id: "s0001",
          source_name: "",
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
    source_sequence: position,
    at: new Date(Date.UTC(2026, 0, 1, 12, 0, position * 10)).toISOString(),
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

/** One preflight answer: a plan the panel can show for a saved test. */
export function runPreflightResult(overrides: Partial<NonNullable<RunPreflightResult["preflight"]>> = {}): RunPreflightResult {
  return {
    state: "completed",
    preflight: {
      kind: "test",
      spec: "reschedule-test.json",
      name: "Rescheduling updates the original appointment",
      schema: "readmit-test/v1",
      identity: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
      selected: [{ source: "s0001-e000001", outbound: "o000001" }],
      target: {
        classification: "unclassified",
        address: "127.0.0.1:25000",
        transport: "plain",
        test_endpoint: true,
        approved_transport: false,
        connect_timeout: "2s",
        message_timeout: "5s",
        max_ack_bytes: 4096,
        credential: false,
      },
      boundary: "ack-contract",
      initial_state: "operator-declared",
      reset: "reset the fixture deliberately",
      engine: { engine: "dev", spec: "readmit-test/v1", profile: "readmit-siu-v1" },
      deadline: "30m0s",
      destination: { name: "job-001", generated: true, fresh: true },
      admission: { admitted: true },
      ...overrides,
    },
  };
}

/** One read of a run folder's recovery counts. */
export function runProgressResult(overrides: Partial<NonNullable<RunProgressResult["progress"]>> = {}): RunProgressResult {
  return {
    state: "completed",
    progress: {
      executing: false,
      phase: "passed",
      acknowledged: 1,
      uncertain: 0,
      not_attempted: 0,
      lease: "released",
      ...overrides,
    },
  };
}

/** One retained execution reopened read-only, values hidden by default. */
export function runEvidenceResult(overrides: Partial<NonNullable<RunEvidenceResult["evidence"]>> = {}): RunEvidenceResult {
  return {
    state: "completed",
    evidence: {
      entry: "job-001",
      durable: true,
      run_state: "passed",
      stop_reason: "passed",
      delivery_uncertain: false,
      journal_incomplete: false,
      recovered: false,
      terminal: true,
      lease: "released",
      acknowledged: 1,
      uncertain: 0,
      not_attempted: 0,
      status: "passed",
      identity: "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210",
      spec_identity: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
      spec_name: "Rescheduling updates the original appointment",
      source_case: "regression",
      source_identity: "7d266d0a09e92d3322d6346cf16c9dd37c768c02a11f8ea6c41870adc44915df",
      boundary: "ack-contract",
      started_at: "2026-01-01T12:00:00Z",
      completed_at: "2026-01-01T12:00:01Z",
      elapsed: "1s",
      pin: { engine: "dev", spec: "readmit-test/v1", profile: "readmit-siu-v1" },
      pin_recorded: true,
      planned: 1,
      readable: 1,
      unreadable: 0,
      messages: [{ source: "s0001-e000001", outbound: "o000001", response: "run/o000001-received.bin", readable: true }],
      assertions: [
        {
          id: "ack",
          operator: "ack_field_equals",
          message: "s0001-e000001",
          selector: "MSA-1",
          status: "passed",
          evidence: "run/o000001-received.bin",
        },
      ],
      gaps: [],
      revealed: false,
      ...overrides,
    },
  };
}

/** One suite queue report. */
export function suiteRunResult(): SuiteRunResult {
  return {
    state: "completed",
    output: "suite-run",
    report: {
      schema: "readmit-run-queue-report/v1",
      parallelism: 1,
      executed: 1,
      start_failed: 0,
      refused: 0,
      skipped: 0,
      jobs: (() => {
        const summary = durableRunResult("passed").run;
        const job: SuiteRunJob = { id: "booking-one", admission: "executed", isolation: "shared" };
        if (summary) job.run = summary;
        return [job];
      })(),
    },
  };
}

/** The native file dialog's selection of one workspace entry. */
export function runSpecChoice(entry: string): RunSpecChoiceResult {
  return { state: "completed", entry };
}

/** The editable project document a stored note lands in. */
export function revisionsResult() {
  return {
    state: "completed" as const,
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
export function indicatorTable(): Map<string, Shell["indicators"][number]> {
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
    schema: "readmit-index/v1",
    provenance: "captured",
    retention: "states",
    retention_state: "active",
    fields: ["PID-3", "MSH-10"],
    sources: 1,
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
        secrets_file: `${WORKSPACE_ROOT}/secrets.json`,
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
      classification: "nonproduction",
      peer: "peer-under-test",
      outcome: "connected",
      phase: "established",
      unsolicited: 0,
    },
    decision: {
      schema: "readmit-send-decision/v1",
      allowed: true,
      reason: "approved",
      address: "peer-under-test",
      classification: "nonproduction",
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
    // What the facade decides about the document: each reference's rotation
    // state and the secrets document a target's credential records for it.
    rotations: [{ name: "mllp-basic-auth", rotation: "current" }],
    credential_file: `${WORKSPACE_ROOT}/secrets.json`,
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
      reason: "approved",
      address: "peer-under-test",
      classification: "nonproduction",
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
      state: "passed",
      outcome: "confirmed",
      reason: "every_action_confirmed",
      environment: "staging-mllp",
      classification: "nonproduction",
      plan_sha256: "abc123def456",
      actions: [
        {
          id: "step-1",
          operator: "operator_confirms",
          authority: "none",
          outcome: "confirmed",
          reason: "operator_confirmed",
        },
        {
          id: "step-2",
          operator: "observation_empty",
          authority: "read_declared_file",
          outcome: "confirmed",
          reason: "ledger_empty",
        },
      ],
      attempted_at: "2026-09-21T12:00:00Z",
    },
    ...overrides,
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
    classification: "observed_fact",
    profile: "readmit-siu-v1",
    ruleset: "readmit-siu-diagnosis/v1",
    summary: "The acknowledgement outcome is not what the ruleset expects.",
    evidence: [diagnosisEvidence()],
    ...overrides,
  };
}

/** One finding as the Findings view lists it: the finding, the severity its
 * rule declares and its evidence field's label, never a field value. */
export function findingRow(
  id: string,
  ruleId = "ack.msa-outcome",
  overrides: Partial<FindingRow> = {},
): FindingRow {
  return {
    ...diagnosisFinding(id, ruleId),
    severity: ruleId === "message.duplicate-control-id" ? "warning" : "error",
    labels: { "MSH-10": "Message Control ID" },
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
      case_identity: CASE_IDENTITY,
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
  verdict: import("../bindings").FindingVerdict,
  overrides: Partial<FindingStatus> = {},
): FindingStatus {
  return {
    finding,
    rule_id: "ack.msa-outcome",
    classification: "observed_fact",
    verdict,
    basis: verdict === "not_reviewed" ? "unreviewed" : "decision",
    next_evidence: "Capture the acknowledgement the case does not hold.",
    ...overrides,
  };
}

export const PACKET_IDENTITY = "1122334455667788112233445566778811223344556677881122334455667788";

/** The privacy screens' fixtures. Every value here is synthetic — entry names,
 * fixed identity tokens, counts and states the engine could have reported.
 * The planted sensitive values the journey refuses to carry stay on the Go
 * side of the seam, where `internal/desktop` proves none of them ever reaches
 * a result; these fixtures carry none by construction. */
export const PRIVACY_REVIEW_IDENTITY = "aabb0011aabb0011aabb0011aabb0011aabb0011aabb0011aabb0011aabb0011";
export const PRIVACY_SUMMARY_IDENTITY = "ccdd2244ccdd2244ccdd2244ccdd2244ccdd2244ccdd2244ccdd2244ccdd2244";

/** The catalog page the facade answers for the folder the window last opened:
 * each case entry of that listing as an unregistered case, named by its entry.
 * Projects and every other kind list nothing unless a test arranges them. */
export function catalogOfListing(
  query: { context: { project: string; generation: number }; kind: string },
  stub: { answered: Map<string, unknown> },
): import("../bindings").CatalogResult {
  const opened = ["OpenWorkspace", "SelectWorkspace", "CreateSampleWorkspace"]
    .map((method) => stub.answered.get(method) as { workspace?: { root: string; artifacts: Artifact[] } } | undefined)
    .filter((answer) => answer?.workspace && answer.workspace.root === query.context.project)
    .map((answer) => answer!.workspace!)[0];
  const items =
    query.kind === "case" && opened
      ? opened.artifacts
          .filter((artifact) => artifact.kind === "case")
          .map((artifact) => caseCatalogItem(artifact.name, artifact.provenance ?? ""))
      : [];
  return { state: "completed", context: query.context, page: { items, total: items.length, snapshot: "s", recorded: true, incomplete: [] } };
}

/** One case as the catalog lists it. */
export function caseCatalogItem(entry: string, provenance = "", status?: import("../bindings").CaseStatus): import("../bindings").CatalogItem {
  return {
    ref: { kind: "case", id: `case-${entry}` },
    name: entry,
    created_at: null,
    updated_at: null,
    last_opened_at: null,
    availability: "available",
    capabilities: [],
    summary: { case: { registered: status !== undefined, entry, tags: [], incidents: [], ...(status ? { status } : {}), evidence: "verified", ...(provenance ? { provenance } : {}) } },
  };
}

/** What a backup of one project would hold, as Create backup shows it. */
export function storageScopeFixture(
  overrides: Partial<import("../bindings").StorageScopeResult> = {},
): import("../bindings").StorageScopeResult {
  return { state: "completed", project: "Scheduling investigation", project_id: "p1", files: 12, bytes: 44_040_192, evidence: 2, indexes: 1, ...overrides };
}

/** One readable recovery copy of the project document, opened read-only. */
export function recoveryCopyFixture(
  overrides: Partial<import("../bindings").RecoveryCopyResult> = {},
): import("../bindings").RecoveryCopyResult {
  return {
    state: "completed",
    copy: { document: "project.json", digest: "a".repeat(64), size: 553, state: "readable", kept_at: "2026-01-02T09:00:00Z", reason: "saved" },
    contents: { schema: "readmit-project/v2", title: "Scheduling investigation", cases: 2, interface_versions: ["v1"] },
    ...overrides,
  };
}

/** A signed-in team project as the Team page reads it: synthetic people,
 * files named from the project's own records, and reviews in every status. */
export function teamResult(overrides: Partial<HubTeamResult> = {}): HubTeamResult {
  const at = "2026-09-20T10:00:00Z";
  return {
    state: "completed",
    project: "cardio-study",
    me: "rui",
    capabilities: ["evidence.read", "evidence.write", "approval", "export", "admin"],
    review_head: 2,
    lifecycle_head: 1,
    activity: [
      { key: "review-2", actor: "ana", action: "review-request", object: "Scheduling smoke · Version 4", at, text: "Reschedule keeps one appointment", to_me: true },
      { key: "history-1", actor: "ana", action: "revision", object: "booking-rules", at: "2026-09-19T10:00:00Z", text: "first" },
    ],
    reviews: [
      {
        id: "ask-1",
        item: "Scheduling smoke",
        version: "4",
        suite: { kind: "suite", id: "suite-1", revision: "4" },
        requested_by: "ana",
        recipient: "rui",
        requested: at,
        updated: at,
        status: "requested",
        reason: "Reschedule keeps one appointment",
        evidence: "a".repeat(64),
        release: "a".repeat(64),
        to_me: true,
        discussion: [],
        requests: [{ id: "ask-1", evidence: "a".repeat(64), release: "a".repeat(64) }],
      },
    ],
    files: [
      { digest: "b".repeat(64), size: 2048, name: "booking-rules", type: "revision", added_by: "ana", added_at: "2026-09-19T10:00:00Z", resource: "booking-rules" },
      { digest: "c".repeat(64), size: 12, type: "file" },
    ],
    resources: [{ resource: "booking-rules", revisions: [{ id: "rev-1", artifact: "b".repeat(64), actor: "ana", at: "2026-09-19T10:00:00Z", reason: "first", parents: [] }], tips: ["rev-1"] }],
    ...overrides,
  };
}

/** What a connected test's editor reads beside its draft: a FHIR observation
 * of appointments by business key, a v2 engine and a FHIR application, each
 * with its typed isolation. Every name and identifier is synthetic. */
export function connectedTestContext(): ConnectedTestContext {
  const fhir = { kind: "environment" as const, id: "env-fhir", revision: "3" };
  return {
    observations: [
      {
        ref: { kind: "observation", id: "obs-appointments", revision: "4" },
        name: "Appointments",
        protocol: "fhir",
        phases: ["both"],
        environment: fhir.id,
        projection: "p1",
        horizon_ms: 30000,
        readable: true,
        columns: [
          { name: "key", type: "text", key: true, required: true, repeated: false, states: ["present", "absent"] },
          { name: "identity", type: "text", key: false, required: true, repeated: false, states: ["present", "absent"] },
          { name: "start", type: "datetime", key: false, required: false, repeated: false, states: ["present", "absent"] },
          { name: "status", type: "code", code_system: "http://hl7.org/fhir/appointmentstatus", key: false, required: false, repeated: false, states: ["present", "absent"] },
        ],
      },
    ],
    pinned: [],
    environments: [
      { ref: { kind: "environment", id: "env-engine", revision: "2" }, name: "Scheduling engine", protocol: "v2", isolation: "Lab tenant", effects: ["Creates Patient"], capabilities: false },
      { ref: fhir, name: "Scheduling FHIR", protocol: "fhir", isolation: "Lab tenant", effects: ["Creates Patient"], capabilities: true, validator: "not-configured", authentication: "none" },
    ],
  };
}

/** A connected test version as a suite uses it: the environments it names,
 * its phase and check, and the one expected value a dataset row can override. */
export function connectedSuiteVersion(ref: { id: string; revision?: string }, name: string): SuiteTestVersion {
  return {
    ref: { kind: "test", id: ref.id, revision: ref.revision ?? "3" },
    name,
    version: ref.revision ?? "3",
    checks: [],
    messages: [],
    sequence: [],
    ledger: false,
    connected: {
      environment: "env-qa",
      server: "env-fhir",
      phases: ["Reschedule"],
      checks: ["Reschedule · Moved start"],
      expected: [
        {
          key: "reschedule/moved-start",
          name: "Reschedule · Moved start",
          field: { name: "start", type: "datetime", key: false, required: false, repeated: false, states: ["present", "absent"] },
          value: { state: "present", type: "datetime", text: "2026-03-02T09:30:00Z", precision: "second", timezone: "+00:00" },
        },
      ],
    },
  };
}
