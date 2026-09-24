// The reexecution step of the privacy panel, the one step there that sends.
// A preview names the target the actual original execution recorded and the
// messages of the approved derived case a send would deliver, and sends
// nothing. The send happens only under an explicit authorization given after
// that preview, pinned to it; it spends both, so no convenience renews either.
// Refusals, an admission the license refuses, a cancellation while the target
// holds its acknowledgement and an uncertain delivery each say so, and nothing
// is ever offered for resending. Every control is reached from the keyboard.
import { expect, test } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { PrivacyPanel } from "./PrivacyPanel";
import { facadeStub, installFacade } from "./testkit/wails";
import type { FacadeHandlers } from "./testkit/wails";
import type { Artifact, ReexecutionPreviewResult, ReexecutionRequest, ReexecutionResult, ReexecutionSendRequest } from "./bindings";
import { PRIVACY_REVIEW_IDENTITY, WORKSPACE_ROOT } from "./testkit/fixtures";

const PREVIEW_IDENTITY = "eeff5566eeff5566eeff5566eeff5566eeff5566eeff5566eeff5566eeff5566";
const RESULT_IDENTITY = "1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd";
const UNAPPROVED = "reexecution requires the exact complete disclosure review; changed artifacts require review again";
const LICENSE_REFUSED = "operation activation is missing or invalid; select and activate an operation policy";

const ENTRIES: Artifact[] = [
  { name: "review", kind: "review" },
  { name: "review-private", kind: "unsupported", reason: "not a case bundle this release supports" },
  { name: "original-packet", kind: "packet" },
  { name: "rebound.json", kind: "spec" },
];

type User = ReturnType<typeof userEvent.setup>;

function assessment(criteria: string, reason: string, result = "") {
  return {
    schema: "readmit-reexecution-assessment/v1",
    review_identity: PRIVACY_REVIEW_IDENTITY,
    original_packet_identity: "packet-identity-fixed-for-tests",
    original_result_identity: "original-result-identity-fixed-for-tests",
    derived_case_identity: "derived-case-identity-fixed-for-tests",
    execution_spec_identity: "execution-spec-identity-fixed-for-tests",
    phase: "failure",
    result_identity: result,
    criteria,
    external_equivalence: "declined",
    disclosure: "customer-local-only-new-review-required",
    reason,
  };
}

function previewResult(admitted = true): ReexecutionPreviewResult {
  return {
    state: "completed",
    preview: {
      identity: PREVIEW_IDENTITY,
      assessment: assessment("not-executed", "No reexecution yet. Review approval does not authorize disclosure of new execution evidence."),
      target: {
        name: "scheduling-lab",
        classification: "nonproduction",
        address: "peer-under-test",
        transport: "plain",
        test_endpoint: true,
        approved_transport: false,
        connect_timeout: "3s",
        message_timeout: "3s",
        max_ack_bytes: 65536,
        credential: false,
      },
      selected: [
        { source: "s0001-e000001", outbound: "o000001" },
        { source: "s0002-e000001", outbound: "o000002" },
      ],
      initial_state: "empty-ledger",
      reset: "Reset the fixture to an empty ledger.",
      destination: { name: "reexecution-001", generated: true, fresh: true },
      admission: admitted ? { admitted: true } : { admitted: false, reason: LICENSE_REFUSED },
      limitations: ["The preview sends nothing."],
    },
  };
}

function matched(): ReexecutionResult {
  return {
    state: "completed",
    outcome: {
      job: "reexecution-001",
      assessment: assessment("matched", "Selected criteria matched one actual retained reexecution.", RESULT_IDENTITY),
      retained: {
        executing: false,
        phase: "assertion_failed",
        run: { schema: "readmit-job/v1", state: "assertion_failed", stop_reason: "assertion_failed", delivery_uncertain: false, planned: 2, recorded: 2, result_identity: RESULT_IDENTITY, recovered: false, journal_incomplete: false },
        acknowledged: 2,
        uncertain: 0,
        not_attempted: 0,
      },
      limitations: [],
    },
  };
}

function renderPanel(handlers: FacadeHandlers = {}, entries: Artifact[] = ENTRIES) {
  const events: string[] = [];
  installFacade({
    Cancel: async (operation) => {
      events.push(`cancel ${operation}`);
    },
    OpenReview: () => ({ state: "empty" }),
    ...handlers,
  });
  render(<PrivacyPanel workspace={WORKSPACE_ROOT} entries={entries} onRefresh={() => events.push("refresh")} />);
  return { events };
}

/** Selects everything a reexecution names and types the exact review identity. */
async function select(user: User, phase = "failure") {
  await user.selectOptions(screen.getByLabelText("Approved review"), "review");
  await user.type(screen.getByLabelText("Private local state its derivation wrote"), "review-private");
  await user.selectOptions(screen.getByLabelText(/^Original packet/), "original-packet");
  await user.selectOptions(screen.getByLabelText("Rebound execution specification"), "rebound.json");
  await user.selectOptions(screen.getByLabelText("Phase"), phase);
  await user.type(screen.getByLabelText("Exact review identity approving this reexecution"), PRIVACY_REVIEW_IDENTITY);
}

async function tabTo(user: User, control: HTMLElement): Promise<void> {
  for (let step = 0; step < 80; step++) {
    if (document.activeElement === control) return;
    await user.tab();
  }
  throw new Error(`${control.textContent ?? control.id} is not reachable with Tab`);
}

/** Matches the one paragraph whose whole text, across its elements, is the
 * given line. */
const line = (text: string) => (_: string, element: Element | null) => element?.tagName === "P" && element.textContent === text;

const authorization = () => screen.getByLabelText(/^I authorize this single nonproduction send/) as HTMLInputElement;
const sendButton = () => screen.getByRole("button", { name: "Send once" }) as HTMLButtonElement;

test("a preview names the target and the messages a send would deliver, and sends nothing", async () => {
  const user = userEvent.setup();
  renderPanel({
    PreviewReexecution: (request: ReexecutionRequest) => {
      expect(request).toEqual({
        workspace: WORKSPACE_ROOT,
        review: "review",
        local_state: "review-private",
        original_packet: "original-packet",
        spec: "rebound.json",
        phase: "failure",
        approval: PRIVACY_REVIEW_IDENTITY,
      });
      return previewResult();
    },
  });
  const preview = screen.getByRole("button", { name: "Preview reexecution" }) as HTMLButtonElement;
  expect(preview.disabled).toBe(true);
  expect(authorization().disabled).toBe(true);
  expect(sendButton().disabled).toBe(true);
  await select(user);
  await tabTo(user, preview);
  await user.keyboard("{Enter}");

  expect(await screen.findByText(line(`Preview identity: ${PREVIEW_IDENTITY}`))).toBeTruthy();
  expect(screen.getByText("Target: scheduling-lab · nonproduction · plain · peer-under-test")).toBeTruthy();
  expect(screen.getByText("Sends 2 messages of the approved derived case:")).toBeTruthy();
  expect(screen.getByText("s0001-e000001 → o000001")).toBeTruthy();
  expect(screen.getByText("s0002-e000001 → o000002")).toBeTruthy();
  expect(screen.getByText("Initial state: empty-ledger · reset you perform first: Reset the fixture to an empty ledger.")).toBeTruthy();
  expect(screen.getByText("Criteria: not-executed · external equivalence declined · disclosure customer-local-only-new-review-required")).toBeTruthy();
  expect(screen.getByText("Destination: reexecution-001 (generated) · fresh")).toBeTruthy();
  expect(screen.getByText("Admission: admitted")).toBeTruthy();
  expect(screen.getByText("This is local validation. No message was sent, nothing was reset, and no result exists yet.")).toBeTruthy();
  // Previewing is not authorizing: the send stays unavailable until the
  // person decides, and nothing was sent.
  expect(authorization().disabled).toBe(false);
  expect(authorization().checked).toBe(false);
  expect(sendButton().disabled).toBe(true);
  expect(facadeStub().callsTo("ReexecuteReviewedEvidence")).toHaveLength(0);
});

test("the send happens only under the explicit authorization, pinned to the reviewed preview, and spends both", async () => {
  const user = userEvent.setup();
  const { events } = renderPanel({
    PreviewReexecution: () => previewResult(),
    ReexecuteReviewedEvidence: (request: ReexecutionSendRequest) => {
      expect(request).toEqual({
        workspace: WORKSPACE_ROOT,
        review: "review",
        local_state: "review-private",
        original_packet: "original-packet",
        spec: "rebound.json",
        phase: "failure",
        approval: PRIVACY_REVIEW_IDENTITY,
        output: "reexecution-001",
        expected_identity: PREVIEW_IDENTITY,
        authorize: true,
      });
      return matched();
    },
  });
  await select(user);
  await user.click(screen.getByRole("button", { name: "Preview reexecution" }));
  await screen.findByText(line(`Preview identity: ${PREVIEW_IDENTITY}`));

  // The keyboard gives the authorization and sends.
  await tabTo(user, authorization());
  await user.keyboard(" ");
  expect(authorization().checked).toBe(true);
  await tabTo(user, sendButton());
  await user.keyboard("{Enter}");

  expect(await screen.findByText(line("Criteria: matched · external equivalence declined"))).toBeTruthy();
  expect(screen.getByText("Selected criteria matched one actual retained reexecution.")).toBeTruthy();
  expect(screen.getByText(line("Job reexecution-001 · run assertion_failed · stop reason assertion_failed"))).toBeTruthy();
  expect(screen.getByText("Deliveries: acknowledged 2 · uncertain 0 · not attempted 0")).toBeTruthy();
  expect(screen.getByText(`Result identity: ${RESULT_IDENTITY}`)).toBeTruthy();
  expect(screen.queryByText(/^Delivery is uncertain/)).toBeNull();
  expect(facadeStub().callsTo("ReexecuteReviewedEvidence")).toHaveLength(1);
  expect(events).toContain("refresh");

  // The attempt spent the preview and the authorization: sending again needs
  // a new preview and a new decision.
  expect(screen.queryByText(line(`Preview identity: ${PREVIEW_IDENTITY}`))).toBeNull();
  expect(authorization().checked).toBe(false);
  expect(authorization().disabled).toBe(true);
  expect(sendButton().disabled).toBe(true);
});

test("a refused preview says the operation's own sentence and offers no send", async () => {
  const user = userEvent.setup();
  renderPanel({ PreviewReexecution: () => ({ state: "failed", reason: UNAPPROVED }) });
  await select(user);
  await tabTo(user, screen.getByRole("button", { name: "Preview reexecution" }));
  await user.keyboard("{Enter}");
  expect(await screen.findByText(UNAPPROVED)).toBeTruthy();
  expect(screen.queryByText(/^Preview identity:/)).toBeNull();
  expect(authorization().disabled).toBe(true);
  expect(sendButton().disabled).toBe(true);
  expect(facadeStub().callsTo("ReexecuteReviewedEvidence")).toHaveLength(0);
});

test("an admission the license refuses is stated in the preview, and the send stays unavailable", async () => {
  const user = userEvent.setup();
  renderPanel({ PreviewReexecution: () => previewResult(false) });
  await select(user);
  await user.click(screen.getByRole("button", { name: "Preview reexecution" }));
  expect(await screen.findByText(`Admission: refused — ${LICENSE_REFUSED}`)).toBeTruthy();
  expect(authorization().disabled).toBe(true);
  expect(sendButton().disabled).toBe(true);
});

test("a send in flight is cancelled from the keyboard, and the cancelled send is reported uncertain and never offered again", async () => {
  const user = userEvent.setup();
  const { events } = renderPanel({ PreviewReexecution: () => previewResult() });
  const sending = facadeStub().park("ReexecuteReviewedEvidence");
  await select(user);
  await user.click(screen.getByRole("button", { name: "Preview reexecution" }));
  await screen.findByText(line(`Preview identity: ${PREVIEW_IDENTITY}`));
  await user.click(authorization());
  await user.click(sendButton());
  await waitFor(() => expect(sending.size).toBe(1));

  // While it sends, nothing else can be started or changed.
  expect(screen.getByRole("button", { name: "Sending…" })).toBeTruthy();
  for (const name of ["Preview reexecution", "Sending…"]) {
    expect((screen.getByRole("button", { name }) as HTMLButtonElement).disabled).toBe(true);
  }
  for (const label of ["Approved review", "Phase", "Exact review identity approving this reexecution", "New job folder"]) {
    expect((screen.getByLabelText(label) as HTMLInputElement).disabled).toBe(true);
  }
  const stop = screen.getByRole("button", { name: "Cancel reexecution" }) as HTMLButtonElement;
  expect(stop.disabled).toBe(false);
  await tabTo(user, stop);
  await user.keyboard("{Enter}");
  expect(events).toContain("cancel reexecution");

  const cancelledReason =
    "the reexecution was cancelled; the job retains what happened, and whatever may already have been delivered is never resent — reconcile it at the target before a separately authorized new attempt";
  sending.resolve({
    state: "cancelled",
    reason: cancelledReason,
    outcome: {
      job: "reexecution-001",
      assessment: assessment("unavailable-or-unstable", "Incomplete, cancelled or uncertain execution cannot establish equivalence; reconcile delivery and reset before a separately authorized new attempt."),
      retained: {
        executing: false,
        phase: "delivery_uncertain",
        run: { schema: "readmit-job/v1", state: "delivery_uncertain", stop_reason: "cancelled", delivery_uncertain: true, planned: 2, recorded: 0, recovered: false, journal_incomplete: false },
        acknowledged: 0,
        uncertain: 1,
        not_attempted: 1,
      },
      limitations: [],
    },
  } satisfies ReexecutionResult);
  expect(await screen.findByText(cancelledReason)).toBeTruthy();
  expect(screen.getByText(line("Criteria: unavailable-or-unstable · external equivalence declined"))).toBeTruthy();
  expect(screen.getByText(line("Job reexecution-001 · run delivery_uncertain · stop reason cancelled"))).toBeTruthy();
  expect(screen.getByText("Deliveries: acknowledged 0 · uncertain 1 · not attempted 1")).toBeTruthy();
  expect(screen.getByText(/^Delivery is uncertain\. Reconcile it at the target before a separately previewed and authorized new attempt/)).toBeTruthy();
  // No resend is offered: the send needs a new preview and authorization.
  expect(sendButton().disabled).toBe(true);
  expect(authorization().disabled).toBe(true);
  expect(screen.queryByRole("button", { name: /resend|retry|resume/i })).toBeNull();
  expect(facadeStub().callsTo("ReexecuteReviewedEvidence")).toHaveLength(1);
});

test("changing any selection withdraws the preview and the authorization, and another review clears the typed identity", async () => {
  const user = userEvent.setup();
  renderPanel({ PreviewReexecution: () => previewResult() });
  await select(user);
  const preview = screen.getByRole("button", { name: "Preview reexecution" });
  for (const change of [
    () => user.selectOptions(screen.getByLabelText("Phase"), "pass"),
    () => user.type(screen.getByLabelText("New job folder"), "x"),
    () => user.selectOptions(screen.getByLabelText("Rebound execution specification"), ""),
  ]) {
    if ((preview as HTMLButtonElement).disabled) await user.selectOptions(screen.getByLabelText("Rebound execution specification"), "rebound.json");
    await user.click(preview);
    await screen.findByText(line(`Preview identity: ${PREVIEW_IDENTITY}`));
    await user.click(authorization());
    expect(sendButton().disabled).toBe(false);
    await change();
    expect(screen.queryByText(line(`Preview identity: ${PREVIEW_IDENTITY}`))).toBeNull();
    expect(authorization().checked).toBe(false);
    expect(sendButton().disabled).toBe(true);
  }
  const approval = screen.getByLabelText("Exact review identity approving this reexecution") as HTMLInputElement;
  expect(approval.value).toBe(PRIVACY_REVIEW_IDENTITY);
  await user.selectOptions(screen.getByLabelText("Approved review"), "");
  expect(approval.value).toBe("");
  expect(approval.disabled).toBe(true);
});

test("a send the backend refuses shows its refusal, retains nothing, and needs a new preview", async () => {
  const user = userEvent.setup();
  const stale = "the reexecution inputs changed after the preview; preview again and authorize the send it shows";
  renderPanel({
    PreviewReexecution: () => previewResult(),
    ReexecuteReviewedEvidence: () => ({ state: "failed", reason: stale }),
  });
  await select(user);
  await user.click(screen.getByRole("button", { name: "Preview reexecution" }));
  await screen.findByText(line(`Preview identity: ${PREVIEW_IDENTITY}`));
  await user.click(authorization());
  await user.click(sendButton());
  expect(await screen.findByText(stale)).toBeTruthy();
  expect(screen.queryByText(/^Job /)).toBeNull();
  expect(screen.queryByText(/^Preview identity:/)).toBeNull();
  expect(sendButton().disabled).toBe(true);
});

/** Previews, authorizes and sends once, and waits for the send to answer. */
async function previewAndSend(user: User) {
  await user.click(screen.getByRole("button", { name: "Preview reexecution" }));
  await screen.findByText(line(`Preview identity: ${PREVIEW_IDENTITY}`));
  await user.click(authorization());
  await user.click(sendButton());
  await waitFor(() => expect(screen.queryByRole("button", { name: "Sending…" })).toBeNull());
}

test("a changed phase and a timed-out send are refused in the assessment's own words, and each keeps its job", async () => {
  const user = userEvent.setup();
  const changedReason = "Reexecution changed the selected failure set or full pass criteria; external equivalence is declined.";
  const unstableReason =
    "Incomplete, cancelled or uncertain execution cannot establish equivalence; reconcile delivery and reset before a separately authorized new attempt.";
  renderPanel({ PreviewReexecution: () => previewResult() });
  facadeStub().reply({
    ReexecuteReviewedEvidence: () => ({
      state: "failed",
      reason: changedReason,
      outcome: {
        job: "reexecution-001",
        assessment: assessment("changed", changedReason, RESULT_IDENTITY),
        retained: {
          executing: false,
          phase: "passed",
          run: { schema: "readmit-job/v1", state: "passed", stop_reason: "passed", delivery_uncertain: false, planned: 2, recorded: 2, result_identity: RESULT_IDENTITY, recovered: false, journal_incomplete: false },
          acknowledged: 2,
          uncertain: 0,
          not_attempted: 0,
        },
        limitations: [],
      },
    }),
  });
  await select(user);
  await previewAndSend(user);
  expect(screen.getAllByText(changedReason)).toHaveLength(1);
  expect(screen.getByText(line("Criteria: changed · external equivalence declined"))).toBeTruthy();
  expect(screen.getByText(line("Job reexecution-001 · run passed · stop reason passed"))).toBeTruthy();
  expect(screen.queryByText(/^Delivery is uncertain/)).toBeNull();

  // A send the deadline stopped with a message unacknowledged is no verdict:
  // its job says so, and the uncertain delivery is never resent.
  facadeStub().reply({
    ReexecuteReviewedEvidence: () => ({
      state: "failed",
      reason: unstableReason,
      outcome: {
        job: "reexecution-002",
        assessment: assessment("unavailable-or-unstable", unstableReason),
        retained: {
          executing: false,
          phase: "delivery_uncertain",
          run: { schema: "readmit-job/v1", state: "delivery_uncertain", stop_reason: "timed_out", delivery_uncertain: true, planned: 2, recorded: 1, recovered: false, journal_incomplete: false },
          acknowledged: 1,
          uncertain: 1,
          not_attempted: 0,
        },
        limitations: [],
      },
    }),
  });
  await previewAndSend(user);
  expect(screen.queryByText(changedReason)).toBeNull();
  expect(screen.getAllByText(unstableReason)).toHaveLength(1);
  expect(screen.getByText(line("Criteria: unavailable-or-unstable · external equivalence declined"))).toBeTruthy();
  expect(screen.getByText(line("Job reexecution-002 · run delivery_uncertain · stop reason timed_out"))).toBeTruthy();
  expect(screen.getByText(/^Delivery is uncertain\. Reconcile it at the target/)).toBeTruthy();
  expect(screen.queryByText(/^Result identity:/)).toBeNull();
  expect(sendButton().disabled).toBe(true);
  expect(facadeStub().callsTo("ReexecuteReviewedEvidence")).toHaveLength(2);
});

test("a preview in flight holds every control, and a busy slot or a refused admission is said as such", async () => {
  const user = userEvent.setup();
  renderPanel();
  const previewing = facadeStub().park("PreviewReexecution");
  await select(user);
  await user.click(screen.getByRole("button", { name: "Preview reexecution" }));
  await waitFor(() => expect(previewing.size).toBe(1));
  expect((screen.getByRole("button", { name: "Previewing…" }) as HTMLButtonElement).disabled).toBe(true);
  for (const label of ["Approved review", "Phase", "Rebound execution specification", "New job folder"]) {
    expect((screen.getByLabelText(label) as HTMLInputElement).disabled).toBe(true);
  }
  expect(authorization().disabled).toBe(true);
  expect((screen.getByRole("button", { name: "Cancel reexecution" }) as HTMLButtonElement).disabled).toBe(true);
  previewing.resolve({ state: "busy", reason: "another operation is already running" } satisfies ReexecutionPreviewResult);
  expect(await screen.findByText("another operation is already running")).toBeTruthy();
  expect(sendButton().disabled).toBe(true);

  // The admission the preview asked can still be refused at the send, which
  // takes it itself: the refusal is shown and nothing is retained.
  facadeStub().reply({
    PreviewReexecution: () => previewResult(),
    ReexecuteReviewedEvidence: () => ({ state: "permission_denied", reason: LICENSE_REFUSED }),
  });
  await previewAndSend(user);
  expect(await screen.findByText(LICENSE_REFUSED)).toBeTruthy();
  expect(screen.queryByText(/^Job /)).toBeNull();
  expect(sendButton().disabled).toBe(true);
});

test("a workspace without a ready review or a retained packet says where each comes from", async () => {
  renderPanel({}, [{ name: "rebound.json", kind: "spec" }]);
  expect(screen.getByText(/^A reexecution starts from a ready review and a retained packet of the actual original run/)).toBeTruthy();
  expect((screen.getByRole("button", { name: "Preview reexecution" }) as HTMLButtonElement).disabled).toBe(true);
});
