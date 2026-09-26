// How the window names the facade's closed vocabularies. An operation state's
// word is the facade's own indicator label (internal/desktop/shell.go), so it
// is not repeated here. Each map is keyed by
// the generated type it presents, so a member Go adds without a caption here is
// a type error rather than a title-cased code on screen. Only stable concepts
// are mapped: payload text, filenames and exact protocol values are shown as
// they are, never humanized.
import type { FieldState, ResetOperator, TargetClassification, TestBoundary, TestRunnerStatus } from "./bindings";

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

export const TEST_BOUNDARIES: DisplayMap<TestBoundary> = {
  "appointment-ledger": "Appointment records",
  "ack-contract": "Acknowledgements",
};

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
