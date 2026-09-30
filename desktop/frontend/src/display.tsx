// How the window names the facade's closed vocabularies. An operation state's
// word is the facade's own indicator label (internal/desktop/shell.go), so it
// is not repeated here. Each map is keyed by
// the generated type it presents, so a member Go adds without a caption here is
// a type error rather than a title-cased code on screen. Only stable concepts
// are mapped: payload text, filenames and exact protocol values are shown as
// they are, never humanized.
import type { CheckChange, RunCheckResult, RunDelivery, RunQueueIsolation, RunResult, BundleMode, CorrelationDecisionAction, CorrelationReviewStatus, SequenceAnalysisDeclaredCoverage, SequenceAnalysisRetryBasis, SearchField, Theme, DiagnosisClassification, DiagnosisSeverity, FindingScope, FindingVerdict, SimilarMemberState, FieldState, ResetOperator, ResetReason, SendPolicyReason, TargetClassification, TestBoundary, TestChange, TestExpectationOperator, TestRunnerStatus, SuiteApprovalScope, SuiteResult, HubActivityKind, HubFileType, HubReviewStatus, RetentionChange } from "./bindings";

/** A caption for every member of one closed vocabulary. */
export type DisplayMap<K extends string> = Record<K, string>;

export const TEST_RESULTS: DisplayMap<TestRunnerStatus> = {
  pass: "Passed",
  assertion_failure: "Failed",
  execution_error: "Error",
};

export const TARGET_CLASSIFICATIONS: DisplayMap<TargetClassification> = {
  nonproduction: "Nonproduction",
  production: "Production",
  unclassified: "Not classified",
};

export const RESET_OPERATORS: DisplayMap<ResetOperator> = {
  operator_confirms: "Manual confirmation",
  observation_empty: "Receiver snapshot empty",
  collection_empty: "Observation empty",
  endpoint_quiet: "Endpoint quiet",
};

/** Why a proposed send was allowed or refused. */
export const SEND_POLICY_REASONS: DisplayMap<SendPolicyReason> = {
  approved: "Inside an allowed range",
  loopback_destination: "This computer",
  invalid_policy: "The allowed ranges cannot be read",
  production_classification: "Production environments never receive sends",
  unrecorded_classification: "The environment is not classified",
  policy_required: "No allowed ranges are saved",
  unresolvable_destination: "The address does not resolve",
  ambiguous_destination: "The address resolves to more than one destination",
  unapproved_destination: "Outside every allowed range",
  send_not_explicit: "Not a send",
};

/** Why one reset action ended as it did. */
export const RESET_REASONS: DisplayMap<ResetReason> = {
  operator_confirmed: "Confirmed",
  ledger_empty: "Empty",
  collection_empty: "Empty",
  endpoint_reachable: "Endpoint reachable",
  every_action_confirmed: "Every action confirmed",
  awaiting_operator_confirmation: "Not confirmed",
  observation_unreadable: "The observation cannot be read",
  ledger_not_empty: "Not empty",
  no_completed_collection: "No completed collection",
  collection_older_than_freshness: "The latest collection is too old",
  collection_not_empty: "Not empty",
  endpoint_refused_connection: "The endpoint refused the connection",
  endpoint_not_quiet: "The endpoint is not quiet",
  endpoint_not_confirmed: "The endpoint was not confirmed",
  endpoint_configuration_unusable: "The environment's connection cannot be used",
  plan_refused: "The reset cannot be read",
  plan_environment_mismatch: "The reset names another environment",
  production_environment: "Production environments are never reset",
  environment_not_recorded_nonproduction: "The environment is not recorded as nonproduction",
  destination_refused: "The destination was refused",
  operator_approval_names_a_machine_action: "A manual confirmation names an automatic action",
  earlier_action_stopped_the_reset: "An earlier action stopped the reset",
  interrupted: "Interrupted",
};

export const TEST_BOUNDARIES: DisplayMap<TestBoundary> = {
  "appointment-ledger": "Appointment records",
  "ack-contract": "Acknowledgements",
};

/** The checks a test holds, by the operator each is written as. */
export const TEST_CHECKS: DisplayMap<TestExpectationOperator> = {
  ack_field_equals: "ACK field",
  ledger_count: "Record count",
  ledger_equals: "Exact records",
};

/** What changed between two versions of a test. */
export const TEST_CHANGES: DisplayMap<TestChange> = {
  name: "Name",
  case: "Case",
  messages: "Messages",
  environment: "Environment",
  boundary: "Outcome",
  observation: "Observation",
  reset: "Reset",
  checks: "Checks",
  tags: "Tags",
};

/** What kind of claim an analysis finding makes. */
export const CLASSIFICATIONS: DisplayMap<DiagnosisClassification> = { observed_fact: "Fact", profile_violation: "Violation", hypothesis: "Hypothesis" };

/** How much a finding matters, as its rule declares. */
export const SEVERITIES: DisplayMap<DiagnosisSeverity> = { error: "Error", warning: "Warning", info: "Info" };

/** Where a finding's review stands. */
export const FINDING_VERDICTS: DisplayMap<FindingVerdict> = { not_reviewed: "New", confirmed: "Confirmed", dismissed: "Dismissed", suppressed: "Suppressed" };

/** What a suppression covers. */
export const FINDING_SCOPES: DisplayMap<FindingScope> = { finding: "This finding", occurrence: "This occurrence", case: "This case" };

/** Whether a chosen case was compared. */
export const SIMILAR_MEMBER_STATES: DisplayMap<SimilarMemberState> = { analyzed: "Analyzed", unavailable: "Unavailable", unsupported: "Unsupported", over_limit: "Over the limit" };

/** A field's state. "" is how Go writes a state it did not record, which is
 * not a state a person can be told anything about. */
export const FIELD_STATES: DisplayMap<Exclude<FieldState, "">> = {
  present: "Present",
  empty: "Empty",
  null: "Null",
  omitted: "Not present",
};

/** Where a case's evidence came from. Generated evidence is synthetic. */
export const PROVENANCES: DisplayMap<BundleMode> = {
  imported: "Imported",
  generated: "Synthetic",
  recorded: "Captured",
  derived: "Variant",
  collected: "Collected",
};

/** Where a reviewed link stands. */
export const REVIEW_STATUSES: DisplayMap<CorrelationReviewStatus> = { unreviewed: "Not reviewed", accepted: "Accepted", rejected: "Rejected", withdrawn: "Withdrawn" };

/** What one relationship decision did, as History lists it. */
export const DECISION_ACTIONS: DisplayMap<CorrelationDecisionAction> = { accept: "Accepted", reject: "Rejected", add: "Added", withdraw: "Undone" };

/** What a source window's capture is declared to hold. */
export const DECLARED_COVERAGES: DisplayMap<SequenceAnalysisDeclaredCoverage> = { partial: "Partial", complete: "Complete" };

/** What a retry declaration rests on. */
export const RETRY_BASES: DisplayMap<SequenceAnalysisRetryBasis> = { operator_reported_retry: "Reported by an operator" };

export const THEMES: DisplayMap<Theme> = { system: "System", light: "Light", dark: "Dark" };

/** The declared detail a project search matched. A message-content match is
 * named by its exact field path instead. */
export const SEARCH_FIELDS: DisplayMap<Exclude<SearchField, "content">> = {
  name: "Name",
  title: "Title",
  owner: "Owner",
  tag: "Tag",
  incident: "Incident",
  status: "Status",
  "interface version": "Interface revision",
};

/** A value that exists but has not been deliberately revealed. */
export const HIDDEN_VALUE = "Hidden";

/** What a code reads as: its caption, or Unsupported when the window has none.
 * An unsupported member is never guessed at; the code stays available as its
 * diagnostic, and an action that depends on it must refuse. */
export type Term = { text: string; supported: true } | { text: "Unsupported"; supported: false; code: string };

export function term<K extends string>(map: DisplayMap<K>, code: string): Term {
  if (Object.prototype.hasOwnProperty.call(map, code)) {
    return { text: map[code as K], supported: true };
  }
  return { text: "Unsupported", supported: false, code };
}

/** A code on screen. An unsupported one reads Unsupported and keeps the exact
 * code behind a disclosure, for a support request. */
export function DisplayTerm<K extends string>({ map, code }: { map: DisplayMap<K>; code: string }) {
  const shown = term(map, code);
  if (shown.supported) {
    return <>{shown.text}</>;
  }
  return (
    <details className="unsupported-term">
      <summary>Unsupported</summary>
      <code>{shown.code}</code>
    </details>
  );
}

/** A suite test's outcome in one run, the worst of its rows. */
export const SUITE_RESULTS: DisplayMap<SuiteResult> = {
  passed: "Passed",
  failed: "Failed",
  error: "Error",
  skipped: "Skipped",
  uncertain: "Uncertain",
  unknown: "Unknown",
};

/** What one approval of a suite version is. */
export const SUITE_APPROVALS: DisplayMap<SuiteApprovalScope> = {
  baseline: "Baseline",
  "review-request": "Review requested",
  release: "Released",
  environment: "Approved for environment",
};

/** How a suite run ended, as the catalog words its outcome. */
export const SUITE_RUN_OUTCOMES: DisplayMap<"executed" | "stopped" | "incomplete"> = {
  executed: "Completed",
  stopped: "Stopped",
  incomplete: "Interrupted",
};

/** A declared exclusion's state. Each keeps its own assessment effect. */
export const EXCLUSION_STATES: DisplayMap<"skipped" | "unsupported" | "quarantined" | "disabled"> = {
  skipped: "Skipped",
  unsupported: "Unsupported",
  quarantined: "Quarantined",
  disabled: "Disabled",
};

/** A declared requirement's assessment. */
export const REQUIREMENT_STATES: DisplayMap<"passed" | "uncovered" | "not_passed"> = {
  passed: "Passed",
  uncovered: "Uncovered",
  not_passed: "Not passed",
};

/** Where a change between two suite versions is. */
export const SUITE_CHANGE_AREAS: DisplayMap<"test" | "dataset" | "row" | "environment" | "binding" | "requirement" | "exclusion" | "setting"> = {
  test: "Test",
  dataset: "Dataset",
  row: "Row",
  environment: "Environment",
  binding: "Binding",
  requirement: "Requirement",
  exclusion: "Exclusion",
  setting: "Setting",
};

/** The one result a run is shown with, as run history ranks them. */
export const RUN_RESULTS: DisplayMap<RunResult> = {
  running: "Running",
  interrupted: "Interrupted",
  incomplete: "Incomplete",
  blocked: "Blocked",
  error: "Error",
  failed: "Failed",
  passed: "Passed",
  accepted: "Accepted",
  not_accepted: "Not accepted",
};

/** What is known about one message a run was to send. */
export const RUN_DELIVERIES: DisplayMap<RunDelivery> = {
  acknowledged: "Acknowledged",
  uncertain: "Uncertain",
  not_attempted: "Not attempted",
};

/** What a run decided about one check. */
export const CHECK_RESULTS: DisplayMap<RunCheckResult> = {
  passed: "Passed",
  failed: "Failed",
  not_evaluated: "Not evaluated",
};

/** How one check changed from the earlier run to the later. */
export const CHECK_CHANGES: DisplayMap<CheckChange> = {
  improved: "Improved",
  regressed: "Regressed",
  unchanged: "Unchanged",
  observed_changed: "Different value",
  changed_check: "Changed check",
  added: "Added check",
  removed: "Removed check",
  not_compared: "Not compared",
};

/** Whether a suite test shares state with the others. */
export const STATE_SHARING: DisplayMap<RunQueueIsolation> = { shared: "Shared", isolated: "Isolated" };

/** The parts of a run's configuration a comparison compares on their own. */
export const CONFIGURATION_PARTS: DisplayMap<"input" | "target" | "environment" | "rule"> = {
  input: "Messages",
  target: "Target",
  environment: "Engine",
  rule: "Profile",
};

/** What comparing one part of the configuration established. */
export const CONFIGURATION_OUTCOMES: DisplayMap<"unchanged" | "changed" | "undecided" | "undeclared"> = {
  unchanged: "Unchanged",
  changed: "Changed",
  undecided: "Unknown",
  undeclared: "Not recorded",
};

/** What the results of several runs of one test show. */
export const STABILITY_STATES: DisplayMap<"insufficient_history" | "unresolved" | "no_observed_flakiness" | "possible_flakiness"> = {
  insufficient_history: "Not enough runs",
  unresolved: "Unresolved",
  no_observed_flakiness: "No check changed result",
  possible_flakiness: "Some checks changed result",
};

/** What one recorded team event did, as Activity lists it. */
export const TEAM_ACTIVITY: DisplayMap<HubActivityKind> = {
  comment: "Commented",
  assignment: "Assigned",
  "review-request": "Requested review",
  approval: "Approved",
  "change-request": "Requested changes",
  "support-policy": "Published sharing policy",
  "support-request": "Requested summary approval",
  "support-approval": "Approved summary",
  revision: "Added revision",
  resolve: "Resolved revisions",
  "remove-user": "Removed member",
  retention: "Set retention",
  retire: "Retired file",
  "audit-export": "Exported audit",
};

/** What a team project's records say a file is. */
export const TEAM_FILE_TYPES: DisplayMap<HubFileType> = {
  revision: "Revision",
  "test-release": "Test release",
  evidence: "Evidence",
  "sharing-policy": "Sharing policy",
  "support-summary": "Support summary",
  file: "File",
};

/** Where one team review stands. */
export const TEAM_REVIEW_STATUSES: DisplayMap<HubReviewStatus> = {
  requested: "Requested",
  approved: "Approved",
  "changes-requested": "Changes requested",
  stale: "Stale",
};

/** A role a team's access policy grants. */
export const TEAM_ROLES: DisplayMap<"owner" | "admin" | "analyst" | "reviewer" | "runner" | "viewer"> = {
  owner: "Owner",
  admin: "Administrator",
  analyst: "Analyst",
  reviewer: "Reviewer",
  runner: "Runner",
  viewer: "Viewer",
};

/** What a retention change does, or did, to one file. */
export const RETENTION_CHANGES: DisplayMap<RetentionChange> = {
  extends: "Extended",
  unchanged: "Unchanged",
  retired: "Retired",
  applied: "Applied",
  failed: "Not changed",
};
