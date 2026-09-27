// How the window names the facade's closed vocabularies. An operation state's
// word is the facade's own indicator label (internal/desktop/shell.go), so it
// is not repeated here. Each map is keyed by
// the generated type it presents, so a member Go adds without a caption here is
// a type error rather than a title-cased code on screen. Only stable concepts
// are mapped: payload text, filenames and exact protocol values are shown as
// they are, never humanized.
import type { DiagnosisClassification, FindingScope, FindingVerdict, SimilarMemberState, FieldState, ResetOperator, ResetReason, SendPolicyReason, TargetClassification, TestBoundary, TestChange, TestExpectationOperator, TestRunnerStatus } from "./bindings";

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
  observation_empty: "Observation empty",
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
  endpoint_reachable: "Endpoint reachable",
  every_action_confirmed: "Every action confirmed",
  awaiting_operator_confirmation: "Not confirmed",
  observation_unreadable: "The observation cannot be read",
  ledger_not_empty: "Not empty",
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
