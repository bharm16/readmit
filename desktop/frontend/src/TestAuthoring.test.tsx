// The review of proposed expectations in the test authoring panel, driven by
// real user events over a panel that answers as the window does: a refused
// call leaves the draft it was given, and every other call replaces it. What
// the facade proposes and records is the journey's and the Go parity tests'
// to show; here the panel is held to what it sends, what it shows and where
// the keyboard goes.
import { expect, test } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { TestAuthoring } from "./TestAuthoring";
import type { TestResult, TestReview, TestSuggestionRequest, TestSuggestions } from "./bindings";
import {
  CASE_ENTRY,
  CASE_IDENTITY,
  GRID_OCCURRENCE,
  NEXT_OCCURRENCE,
  NO_STAGES_MISSING,
  gridRow,
  indicatorTable,
  testResult,
} from "./testkit/fixtures";

/** A test decided by the acknowledgement contract that sends two messages and
 * already expects one acknowledgement value. */
function authored(): TestResult {
  return testResult(
    {
      schema: "readmit-test-draft/v1",
      case: { entry: CASE_ENTRY, identity: CASE_IDENTITY },
      name: "Reschedule accepted",
      messages: [GRID_OCCURRENCE, NEXT_OCCURRENCE],
      target: "practice-target",
      boundary: "ack-contract",
      observation: "",
      reset: "Empty the ledger.",
      expectations: [
        { id: "reschedule-accepted", operator: "ack_field_equals", message: NEXT_OCCURRENCE, selector: "MSA-1", field: { state: "present", text: "AA" } },
      ],
    },
    NO_STAGES_MISSING,
  );
}

/** One acknowledgement proposal per message the test sends, read from the
 * named run. */
function proposals(result: string): TestSuggestions {
  const proposal = (message: string, payload: string) => ({
    id: `ack-${message}-msa-1`,
    operator: "ack_field_equals" as const,
    outcome: "supported" as const,
    message,
    selector: "MSA-1",
    field: { state: "present" as const, text: "AA" },
    evidence: { artifact: result, payload, message, selector: "MSA-1" },
  });
  return {
    origin: {
      result,
      identity: "reviewed-result-identity",
      status: "pass",
      boundary: "ack-contract",
      spec_identity: "spec-identity",
      input_identity: CASE_IDENTITY,
      run_identity: "run-identity",
      target_identity: "target-identity",
    },
    suggestions: [proposal(GRID_OCCURRENCE, "run/payloads/o000001-received.bin"), proposal(NEXT_OCCURRENCE, "run/payloads/o000002-received.bin")],
    supported: 2,
    unsupported: 0,
  };
}

const UNREVIEWED = "expectations are suggested from a run whose own expectations held; this one did not";

/** The panel as the window holds it: a refused answer keeps the test it was
 * given, and every other answer replaces it. A run named `unreviewed-run` is
 * refused as the engine refuses a failed one. */
function Harness({
  asked,
  recorded,
  start = authored,
  propose = proposals,
}: {
  asked: TestSuggestionRequest[];
  recorded: [TestSuggestionRequest, TestReview][];
  start?: () => TestResult;
  propose?: (result: string) => TestSuggestions;
}) {
  const [result, setResult] = useState<TestResult | null>(start());
  const answer = (next: TestResult) => setResult((kept) => (next.test || !kept?.test ? next : { ...next, test: kept.test }));
  return (
    <TestAuthoring
      rows={[gridRow(GRID_OCCURRENCE), gridRow(NEXT_OCCURRENCE)]}
      result={result}
      inspected={null}
      busy={false}
      progress={null}
      indicators={indicatorTable()}
      onAnswer={() => undefined}
      onSave={() => undefined}
      onSuggest={(request) => {
        asked.push(request);
        const base = start().test;
        answer(
          request.result === "unreviewed-run" || !base
            ? { state: "failed", reason: UNREVIEWED }
            : { state: "completed", test: { ...base, suggestions: propose(request.result) } },
        );
      }}
      onApprove={(request, review) => {
        recorded.push([request, review]);
      }}
    />
  );
}

/** The panel's proposal for one message, found by the position it reads. */
function proposalFor(message: string) {
  const line = screen.getByText(new RegExp(`^ack_field_equals · ${message} · MSA-1 · present · AA · `));
  const item = line.closest("li");
  if (!item) throw new Error(`no proposal for ${message}`);
  return within(item);
}

/** Moves focus with Tab, as a keyboard does, until it reaches one control. */
async function tabTo(user: ReturnType<typeof userEvent.setup>, control: HTMLElement) {
  for (let step = 0; step < 16 && document.activeElement !== control; step++) {
    await user.tab();
  }
  expect(document.activeElement).toBe(control);
}

/** Names the reviewed run and asks from the keyboard: Enter in the entry. */
async function ask(user: ReturnType<typeof userEvent.setup>, run: string) {
  const entry = screen.getByLabelText("Entry holding the reviewed run result");
  await user.clear(entry);
  await user.type(entry, `${run}{Enter}`);
}

test("a review records only what a person decided, against the run that proposed it, from the keyboard", async () => {
  const user = userEvent.setup();
  const asked: TestSuggestionRequest[] = [];
  const recorded: [TestSuggestionRequest, TestReview][] = [];
  render(<Harness asked={asked} recorded={recorded} />);

  await user.click(screen.getByLabelText("Suggest count"));
  await ask(user, "reviewed-run");
  expect(asked).toEqual([{ result: "reviewed-run", ledger: false, exact_ledger: false, positions: ["MSA-1"] }]);
  expect(screen.getByText(/^From reviewed-run · the run reports pass at ack-contract · result identity/)).toBeTruthy();
  expect(screen.getByText(/2 of 2 proposals are supported\.$/)).toBeTruthy();
  // Asking decided nothing: both proposals are unreviewed, nothing can be
  // recorded, and the test still expects only what it expected.
  expect(proposalFor(GRID_OCCURRENCE).getByText(/· Not reviewed ·/)).toBeTruthy();
  expect(proposalFor(NEXT_OCCURRENCE).getByText(/· Not reviewed ·/)).toBeTruthy();
  expect((screen.getByRole("button", { name: "Save decisions" }) as HTMLButtonElement).disabled).toBe(true);
  expect(screen.getAllByRole("button", { name: /^Remove / })).toHaveLength(1);

  // Naming another run after asking changes which run the next question is
  // about, not which run these proposals came from.
  await user.clear(screen.getByLabelText("Entry holding the reviewed run result"));
  await user.type(screen.getByLabelText("Entry holding the reviewed run result"), "another-run");

  // The booking's proposal is renamed and approved from the keyboard; the
  // reschedule's is rejected, since the test already decides it.
  const booking = proposalFor(GRID_OCCURRENCE);
  await user.click(booking.getByLabelText("Record it as"));
  await user.keyboard("booking-accepted");
  await user.tab();
  expect(document.activeElement).toBe(booking.getByLabelText("Expected value", { selector: "select" }));
  await user.tab();
  expect(document.activeElement).toBe(booking.getByLabelText("Expected value", { selector: "input" }));
  await user.tab();
  expect(document.activeElement).toBe(booking.getByRole("button", { name: `Approve ack-${GRID_OCCURRENCE}-msa-1` }));
  await user.keyboard("{Enter}");
  expect(booking.getByText(/· Approved ·/)).toBeTruthy();
  await user.click(proposalFor(NEXT_OCCURRENCE).getByRole("button", { name: `Reject ack-${NEXT_OCCURRENCE}-msa-1` }));
  expect(proposalFor(NEXT_OCCURRENCE).getByText(/· Rejected ·/)).toBeTruthy();

  await user.click(screen.getByRole("button", { name: "Save decisions" }));
  expect(recorded).toEqual([
    [
      { result: "reviewed-run", ledger: false, exact_ledger: false, positions: ["MSA-1"] },
      {
        result: "reviewed-run",
        identity: "reviewed-result-identity",
        decisions: [
          { suggestion: `ack-${GRID_OCCURRENCE}-msa-1`, approved: true, id: "booking-accepted" },
          { suggestion: `ack-${NEXT_OCCURRENCE}-msa-1`, approved: false },
        ],
      },
    ],
  ]);
});

test("cancelling a review records nothing, drops what was decided and returns to the reviewed-run entry", async () => {
  const user = userEvent.setup();
  const asked: TestSuggestionRequest[] = [];
  const recorded: [TestSuggestionRequest, TestReview][] = [];
  render(<Harness asked={asked} recorded={recorded} />);

  await user.click(screen.getByLabelText("Suggest count"));
  await ask(user, "reviewed-run");
  await user.click(proposalFor(GRID_OCCURRENCE).getByRole("button", { name: `Approve ack-${GRID_OCCURRENCE}-msa-1` }));
  expect((screen.getByRole("button", { name: "Save decisions" }) as HTMLButtonElement).disabled).toBe(false);

  await tabTo(user, screen.getByRole("button", { name: "Cancel" }));
  await user.keyboard("{Enter}");
  expect(screen.queryByText(/^From reviewed-run · /)).toBeNull();
  expect(screen.queryByRole("button", { name: "Save decisions" })).toBeNull();
  expect(document.activeElement).toBe(screen.getByLabelText("Entry holding the reviewed run result"));
  expect(recorded).toHaveLength(0);
  expect(screen.getAllByRole("button", { name: /^Remove / })).toHaveLength(1);

  // Asking again starts a new review: what was decided before is gone.
  await user.keyboard("{Enter}");
  expect(asked).toHaveLength(2);
  expect(proposalFor(GRID_OCCURRENCE).getByText(/· Not reviewed ·/)).toBeTruthy();
  expect((screen.getByRole("button", { name: "Save decisions" }) as HTMLButtonElement).disabled).toBe(true);
});

test("a refused request withdraws the proposals of the request before it", async () => {
  const user = userEvent.setup();
  const asked: TestSuggestionRequest[] = [];
  const recorded: [TestSuggestionRequest, TestReview][] = [];
  render(<Harness asked={asked} recorded={recorded} />);

  await user.click(screen.getByLabelText("Suggest count"));
  await ask(user, "reviewed-run");
  expect(screen.getByText(/^From reviewed-run · /)).toBeTruthy();

  await ask(user, "unreviewed-run");
  expect(screen.getByText(UNREVIEWED)).toBeTruthy();
  expect(screen.queryByText(/^From reviewed-run · /)).toBeNull();
  expect(screen.queryByRole("button", { name: /^Approve / })).toBeNull();
  expect(screen.getAllByRole("button", { name: /^Remove / })).toHaveLength(1);
  expect(recorded).toHaveLength(0);
});

/** A test decided by the appointment ledger that sends one message and
 * expects nothing yet. */
function ledgerDraft(): TestResult {
  return testResult(
    {
      schema: "readmit-test-draft/v1",
      case: { entry: CASE_ENTRY, identity: CASE_IDENTITY },
      name: "One appointment",
      messages: [GRID_OCCURRENCE],
      target: "practice-target",
      boundary: "appointment-ledger",
      observation: "ledger.json",
      reset: "Empty the ledger.",
      expectations: [],
    },
    NO_STAGES_MISSING,
  );
}

/** The record count and the exact ledger the named run settled on. The one
 * record carries positions only, with every identifier empty. */
function ledgerProposals(result: string): TestSuggestions {
  const empty = { value: "", namespace: "", universal_id: "", universal_id_type: "" };
  const evidence = { artifact: result, payload: "observation.json", digest: "observation-digest" };
  return {
    ...proposals(result),
    suggestions: [
      { id: "ledger-records", operator: "ledger_count", outcome: "supported", count: 1, evidence },
      {
        id: "ledger-exact",
        operator: "ledger_equals",
        outcome: "supported",
        records: [{ record_id: "r000001", patient_id: empty, placer_id: empty, filler_id: empty, appointment_start: "" }],
        evidence,
      },
    ],
  };
}

test("ledger proposals are approved with a corrected count or as an empty ledger, over the positions chosen before asking", async () => {
  const user = userEvent.setup();
  const asked: TestSuggestionRequest[] = [];
  const recorded: [TestSuggestionRequest, TestReview][] = [];
  render(<Harness asked={asked} recorded={recorded} start={ledgerDraft} propose={ledgerProposals} />);

  // The positions to propose are chosen before asking: the default one is
  // withdrawn and an ERR position added in its place.
  await user.click(screen.getByRole("button", { name: "Do not propose MSA-1" }));
  await user.clear(screen.getByLabelText("Acknowledgement position to propose a value for"));
  await user.type(screen.getByLabelText("Acknowledgement position to propose a value for"), "ERR-1");
  await user.click(screen.getByRole("button", { name: "Also propose ERR-1" }));
  expect(screen.getByRole("button", { name: "Do not propose ERR-1" })).toBeTruthy();
  await user.click(screen.getByLabelText("Suggest exact ledger"));
  await ask(user, "reviewed-run");
  expect(asked).toEqual([{ result: "reviewed-run", ledger: true, exact_ledger: true, positions: ["ERR-1"] }]);

  const proposed = (id: string) => {
    const item = screen.getByRole("button", { name: `Approve ${id}` }).closest("li");
    if (!item) throw new Error(`no proposal ${id}`);
    return within(item);
  };
  expect(proposed("ledger-records").getByText(/^ledger_count · 1 records · Not reviewed · read from reviewed-run\/observation\.json$/)).toBeTruthy();
  await user.type(proposed("ledger-records").getByLabelText("Expected records"), "2");
  await user.click(proposed("ledger-records").getByRole("button", { name: "Approve ledger-records" }));
  expect(proposed("ledger-exact").getByText(/^1 exact records proposed/)).toBeTruthy();
  await user.click(proposed("ledger-exact").getByRole("button", { name: "Approve as an empty ledger" }));
  expect(proposed("ledger-exact").getByText(/^0 exact records proposed/)).toBeTruthy();
  await user.click(proposed("ledger-exact").getByRole("button", { name: "Approve ledger-exact" }));

  await user.click(screen.getByRole("button", { name: "Save decisions" }));
  expect(recorded).toEqual([
    [
      { result: "reviewed-run", ledger: true, exact_ledger: true, positions: ["ERR-1"] },
      {
        result: "reviewed-run",
        identity: "reviewed-result-identity",
        decisions: [
          { suggestion: "ledger-records", approved: true, count: 2 },
          { suggestion: "ledger-exact", approved: true, records: [] },
        ],
      },
    ],
  ]);
});
