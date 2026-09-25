// The replay screen: `readmit replay` beside the verified case. The panel
// previews what the facade says one send would do and sends only once the
// person approved that exact preview, carrying the identity the preview fixed.
// Whether a preview may be sent is the facade's answer, never the panel's; a
// changed input withdraws the preview and its approval; a send, whatever it
// established, spends the approval and nothing is sent again; values appear
// only on purpose; and each cancel names its own operation. Every fixture here
// carries positions, states, counts and placeholder names, never an HL7 value,
// a machine path or a network address.
import { expect, test } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ReplayPanel } from "./ReplayPanel";
import { installFacade } from "./testkit/wails";
import type { FacadeHandlers } from "./testkit/wails";
import type { Artifact, ReplayPreview, ReplayRequest, ReplayResult, ReplayRun, ReplaySendRequest, SendPolicyDecision } from "./bindings";
import { gridRow, WORKSPACE_ROOT } from "./testkit/fixtures";

const IDENTITY = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef";
const PREVIEWED = "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210";
const CASE_IDENTITY = "aaaabbbbccccddddeeeeffff0000111122223333444455556666777788889999";

const ENTRIES: Artifact[] = [
  { name: "case", kind: "case", schema: "readmit-case/v3", provenance: "imported" },
  { name: "downstream-target.json", kind: "target" },
  { name: "production-target.json", kind: "target" },
  { name: "send-policy.json", kind: "policy" },
  { name: "reschedule-test.json", kind: "spec" },
];

const ROWS = [gridRow("s0001-e000001"), gridRow("s0001-e000002", "ack"), gridRow("s0002-e000001")];

function decision(overrides: Partial<SendPolicyDecision> = {}): SendPolicyDecision {
  return {
    schema: "readmit-send-decision/v1",
    allowed: false,
    reason: "send_not_explicit",
    address: "the-test-endpoint",
    classification: "nonproduction",
    explicit_send: false,
    policy_selected: true,
    approved_destinations: ["approved-prefix"],
    resolved_addresses: ["resolved-endpoint"],
    decided_at: "2026-01-01T12:00:00Z",
    ...overrides,
  };
}

/** One completed preview of both messages, rebased and shifted, as the
 * facade answers it. */
function previewed(request: ReplayRequest, overrides: Partial<ReplayPreview> = {}): ReplayResult {
  const revealed = request.reveal;
  return {
    state: "completed",
    decision: decision(),
    preview: {
      case: "case",
      identity: PREVIEWED,
      source_identity: CASE_IDENTITY,
      target: {
        name: "scheduling-downstream",
        classification: "nonproduction",
        address: "the-test-endpoint",
        transport: "plain",
        test_endpoint: true,
        approved_transport: true,
        connect_timeout: "2s",
        message_timeout: "5s",
        max_ack_bytes: 65536,
        credential: false,
      },
      messages: [
        { source: "s0001-e000001", outbound: "o000001", wire_bytes: 312 },
        { source: "s0002-e000001", outbound: "o000002", wire_bytes: 318 },
      ],
      transformations: request.transformations,
      changes: [
        {
          transformation: "rebase-control-ids",
          source: "s0001-e000001",
          outbound: "o000001",
          selector: "MSH[1]-10[1]",
          old_state: "present",
          new_state: "present",
          ...(revealed ? { old: "revealed-before", new: "revealed-after" } : {}),
        },
      ],
      destination: request.output ? { name: request.output, generated: false, fresh: true } : { name: "replay-001", generated: true, fresh: true },
      decision_file: `${request.output || "replay-001"}.decision.json`,
      admission: { admitted: true },
      sendable: true,
      revealed,
      ...overrides,
    },
  };
}

/** The run a cancelled send retained: the message in flight uncertain, the
 * next never attempted. */
function cancelledRun(): ReplayRun {
  return {
    output: "replay-001",
    decision_file: "replay-001.decision.json",
    identity: IDENTITY,
    schema: "readmit-run/v1",
    state: "complete",
    contains_source_values: true,
    export_policy: "customer-local-only",
    successful: false,
    uncertain: 1,
    messages: [
      {
        source: "s0001-e000001",
        outbound: "o000001",
        outcome: "cancelled",
        delivery: "uncertain",
        sent_bytes: 312,
        received_bytes: 0,
        ack: "",
        correlation: "none",
        elapsed: "1.2s",
        error_class: "cancelled",
        error_phase: "read",
      },
      {
        source: "s0002-e000001",
        outbound: "o000002",
        outcome: "not_attempted",
        delivery: "not_sent",
        sent_bytes: 0,
        received_bytes: 0,
        ack: "",
        correlation: "none",
        elapsed: "0s",
      },
    ],
  };
}

function renderPanel(handlers: FacadeHandlers = {}, rows = ROWS) {
  const facade = installFacade({ Cancel: async () => {}, ...handlers });
  let refreshed = 0;
  render(
    <ReplayPanel
      workspace={WORKSPACE_ROOT}
      caseName="case"
      identity={CASE_IDENTITY}
      rows={rows}
      entries={ENTRIES}
      busy={false}
      onSent={() => {
        refreshed++;
      }}
    />,
  );
  return { facade, refreshed: () => refreshed };
}

/** Moves focus with Tab until the control has it, as a keyboard user does,
 * and fails when the control is not reachable that way at all. */
async function tabTo(user: ReturnType<typeof userEvent.setup>, control: HTMLElement): Promise<void> {
  for (let step = 0; step < 80; step++) {
    if (document.activeElement === control) return;
    await user.tab();
  }
  throw new Error(`${control.textContent ?? control.id} is not reachable with Tab`);
}

/** Chooses both messages, the downstream target and its policy, and both
 * transformations with a day's shift: what a person does before a preview. */
async function describeReplay(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole("button", { name: "Replay s0001-e000001" }));
  await user.click(screen.getByRole("button", { name: "Replay s0002-e000001" }));
  await name(user, "Target configuration", "downstream-target.json");
  await name(user, "Send policy", "send-policy.json");
  await user.click(screen.getByLabelText(/Rebase control IDs/));
  await user.click(screen.getByLabelText(/Shift timestamps/));
  await user.type(screen.getByLabelText("Shift by"), "24h");
}

/** Replaces what an entry field holds, typed the way a person types it. */
async function name(user: ReturnType<typeof userEvent.setup>, label: string, entry: string): Promise<void> {
  await user.clear(screen.getByLabelText(label));
  await user.type(screen.getByLabelText(label), entry);
}

function preview() {
  return within(screen.getByRole("region", { name: "Replay preview" }));
}

test("a preview names every message, change and refusal, and a changed input withdraws it and its approval", async () => {
  const user = userEvent.setup();
  const { facade } = renderPanel({ PreviewReplay: (request) => previewed(request) });
  // Only messages can be chosen; an acknowledgement is never a send.
  expect(screen.queryByRole("button", { name: "Replay s0001-e000002" })).toBeNull();
  expect(screen.getByText("No message is chosen, so all 2 message(s) of the case are replayed, in source order.")).toBeTruthy();
  expect((screen.getByRole("button", { name: "Preview replay" }) as HTMLButtonElement).disabled).toBe(true);
  // The listing's target configurations and send policies are offered.
  const offered = (list: string) => Array.from(document.querySelectorAll(`#${list} option`)).map((option) => option.getAttribute("value"));
  expect(offered("replay-target-options")).toEqual(["downstream-target.json", "production-target.json"]);
  expect(offered("replay-policy-options")).toEqual(["send-policy.json"]);
  // A choice can be taken back, all at once.
  await user.click(screen.getByRole("button", { name: "Replay s0002-e000001" }));
  expect(screen.getByText("Chosen: s0002-e000001. They are replayed in source order, whatever order they were chosen in.")).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Select all messages" }));
  expect(screen.getByText("No message is chosen, so all 2 message(s) of the case are replayed, in source order.")).toBeTruthy();

  await describeReplay(user);
  expect(screen.getByRole("button", { name: "Do not replay s0001-e000001" }).getAttribute("aria-pressed")).toBe("true");
  await user.click(screen.getByRole("button", { name: "Preview replay" }));
  expect(await screen.findByText("Dry run: no connection opened.")).toBeTruthy();
  expect(facade.oneCall("PreviewReplay")).toEqual([
    {
      workspace: WORKSPACE_ROOT,
      case: "case",
      identity: CASE_IDENTITY,
      target: "downstream-target.json",
      policy: "send-policy.json",
      messages: ["s0001-e000001", "s0002-e000001"],
      transformations: [{ name: "rebase-control-ids" }, { name: "shift-timestamps", shift: "24h" }],
      reveal: false,
    },
  ]);
  const shown = preview();
  expect(shown.getByText("Send policy: denied (send_not_explicit)")).toBeTruthy();
  expect(shown.getByText("Destination: the-test-endpoint resolved to resolved-endpoint")).toBeTruthy();
  expect(shown.getByText("Approved destinations: approved-prefix")).toBeTruthy();
  expect(shown.getByText("Messages: 2")).toBeTruthy();
  expect(shown.getByText("Transformation: shift-timestamps shift=24h")).toBeTruthy();
  const wire = shown.getByRole("table", { name: "Messages this send would put on the wire, in order" });
  expect(within(wire).getByRole("row", { name: /o000002/ }).textContent).toBe("o000002s0002-e000001318");
  const changes = shown.getByRole("table", { name: /values hidden until revealed/ });
  expect(within(changes).getByRole("row", { name: /MSH\[1\]-10\[1\]/ }).textContent).toBe("s0001-e000001rebase-control-idsMSH[1]-10[1]presentpresent");
  expect(shown.getByText("Run folder: replay-001 (generated) · fresh · decision retained in replay-001.decision.json")).toBeTruthy();
  expect(shown.getByText("Admission: admitted")).toBeTruthy();
  // The proposed run folder is now what a send would write.
  expect((screen.getByLabelText("Run folder") as HTMLInputElement).value).toBe("replay-001");

  // Values appear only on purpose, and hide again.
  await user.click(shown.getByRole("button", { name: "Show values" }));
  const revealed = within(await screen.findByRole("table", { name: /values revealed/ }));
  expect(revealed.getByRole("row", { name: /MSH\[1\]-10\[1\]/ }).textContent).toContain("revealed-before (present)revealed-after (present)");
  expect(facade.callsTo("PreviewReplay").map((call) => (call.args[0] as ReplayRequest).reveal)).toEqual([false, true]);

  // An approval given to this preview is withdrawn with it by any change.
  await user.click(preview().getByLabelText(/I approve sending these 2 message\(s\) once to the-test-endpoint/));
  expect((preview().getByRole("button", { name: "Send once" }) as HTMLButtonElement).disabled).toBe(false);
  await user.type(screen.getByLabelText("Shift by"), "0");
  expect(screen.queryByRole("region", { name: "Replay preview" })).toBeNull();
  expect(screen.queryByRole("button", { name: "Send once" })).toBeNull();
  await user.click(screen.getByRole("button", { name: "Preview replay" }));
  expect(await screen.findByText("Dry run: no connection opened.")).toBeTruthy();
  expect((preview().getByLabelText(/I approve sending/) as HTMLInputElement).checked).toBe(false);
  expect((preview().getByRole("button", { name: "Send once" }) as HTMLButtonElement).disabled).toBe(true);
  expect(facade.callsTo("SendReplay")).toHaveLength(0);

  // A refusal is the command's sentence beside the decision it reached, and
  // shows no plan and no approval.
  facade.reply({
    PreviewReplay: () => ({
      state: "failed",
      reason: "this configuration records the production classification; readmit does not replay to a production-classified environment",
      decision: decision({ reason: "production_classification", classification: "production" }),
    }),
  });
  await name(user, "Target configuration", "production-target.json");
  await user.click(screen.getByRole("button", { name: "Preview replay" }));
  expect(
    await screen.findByText(
      "Not previewed: this configuration records the production classification; readmit does not replay to a production-classified environment",
    ),
  ).toBeTruthy();
  expect(screen.getByText("Send policy: denied (production_classification)")).toBeTruthy();
  expect(screen.queryByRole("region", { name: "Replay preview" })).toBeNull();

  // A destination the policy does not approve is previewed and never offered.
  facade.reply({
    PreviewReplay: (request) =>
      previewed(request, {
        sendable: false,
        refusal: "the send policy refuses this destination (unapproved_destination); a send is refused before anything is sent",
      }),
  });
  await name(user, "Target configuration", "downstream-target.json");
  await user.click(screen.getByRole("button", { name: "Preview replay" }));
  expect(
    await screen.findByText(
      "This preview cannot be sent: the send policy refuses this destination (unapproved_destination); a send is refused before anything is sent",
    ),
  ).toBeTruthy();
  expect(screen.queryByLabelText(/I approve sending/)).toBeNull();

  // Another operation holding the slot is answered busy, with no plan.
  facade.reply({ PreviewReplay: () => ({ state: "busy", reason: "another operation is already running" }) });
  await user.click(screen.getByRole("button", { name: "Preview replay" }));
  expect(await screen.findByText("Not previewed: another operation is already running")).toBeTruthy();
  expect(facade.callsTo("SendReplay")).toHaveLength(0);
});

test("a send happens only after explicit approval, is cancelled from the keyboard and is never offered again after an uncertain delivery", async () => {
  const user = userEvent.setup();
  const cancels: string[] = [];
  const { facade, refreshed } = renderPanel({
    PreviewReplay: (request) => previewed(request),
    Cancel: async (operation) => void cancels.push(operation),
  });
  const sending = facade.park("SendReplay");
  await describeReplay(user);

  // Everything from here is the keyboard.
  await tabTo(user, screen.getByRole("button", { name: "Preview replay" }));
  await user.keyboard("{Enter}");
  expect(await screen.findByText("Dry run: no connection opened.")).toBeTruthy();
  const send = preview().getByRole("button", { name: "Send once" }) as HTMLButtonElement;
  expect(send.disabled).toBe(true);
  await tabTo(user, preview().getByLabelText(/I approve sending/));
  await user.keyboard(" ");
  await tabTo(user, send);
  await user.keyboard("{Enter}");
  expect(await screen.findByText(/^Sending\. Cancel stops at the message in flight/)).toBeTruthy();
  expect(sending.size).toBe(1);
  expect(facade.oneCall("SendReplay")).toEqual([
    {
      replay: {
        workspace: WORKSPACE_ROOT,
        case: "case",
        identity: CASE_IDENTITY,
        target: "downstream-target.json",
        policy: "send-policy.json",
        messages: ["s0001-e000001", "s0002-e000001"],
        transformations: [{ name: "rebase-control-ids" }, { name: "shift-timestamps", shift: "24h" }],
        output: "replay-001",
        reveal: false,
      },
      expected_identity: PREVIEWED,
      approved: true,
    } satisfies ReplaySendRequest,
  ]);

  // While it sends, the keyboard lands on the cancel, which names the send's
  // own operation; the send's answer is what it retained, and it is shown.
  await waitFor(() => expect(document.activeElement).toBe(screen.getByRole("button", { name: "Cancel send" })));
  await user.keyboard("{Enter}");
  expect(cancels).toEqual(["replay"]);
  sending.resolve({
    state: "cancelled",
    reason: "the replay was cancelled; messages after the one in flight were not attempted, and nothing is sent again",
    decision: decision({ allowed: true, reason: "approved", explicit_send: true }),
    run: cancelledRun(),
  } satisfies ReplayResult);
  expect(await screen.findByText("the replay was cancelled; messages after the one in flight were not attempted, and nothing is sent again")).toBeTruthy();
  const established = within(screen.getByRole("table", { name: "What each message's send established" }));
  expect(established.getByRole("row", { name: /o000001/ }).textContent).toBe("o000001s0001-e000001cancelleduncertain3120none none1.2scancelled (read)");
  expect(established.getByRole("row", { name: /o000002/ }).textContent).toBe("o000002s0002-e000001not_attemptednot_sent00none none0s—");
  expect(screen.getByText(/^Delivery uncertain for 1 message\(s\): inspect the receiver before any new send\./)).toBeTruthy();
  expect(screen.getByText("Not every message was accepted. This is not a passing replay.")).toBeTruthy();
  expect(screen.getByText(/^Retained in replay-001, with its send decision in replay-001\.decision\.json\./)).toBeTruthy();

  // The approval is spent: no preview, no approval and no send is offered,
  // the proposed folder is gone and the listing is read again.
  expect(screen.queryByRole("region", { name: "Replay preview" })).toBeNull();
  expect(screen.queryByRole("button", { name: "Send once" })).toBeNull();
  expect((screen.getByLabelText("Run folder") as HTMLInputElement).value).toBe("");
  expect(refreshed()).toBe(1);
  expect(facade.callsTo("SendReplay")).toHaveLength(1);

  // A new send is a new preview and a new approval.
  await tabTo(user, screen.getByRole("button", { name: "Preview replay" }));
  await user.keyboard("{Enter}");
  expect(await screen.findByText("Dry run: no connection opened.")).toBeTruthy();
  expect((preview().getByLabelText(/I approve sending/) as HTMLInputElement).checked).toBe(false);
  expect(facade.callsTo("PreviewReplay")).toHaveLength(2);
  expect(facade.callsTo("SendReplay")).toHaveLength(1);
});

test("a refused or denied send shows its refusal and the decision it retained, and a cancelled preview drops its late answer", async () => {
  const user = userEvent.setup();
  const cancels: string[] = [];
  const { facade } = renderPanel({
    PreviewReplay: (request) => previewed(request),
    SendReplay: () => ({
      state: "failed",
      reason: "the send was refused by policy before anything was sent: unapproved_destination",
      decision: decision({ reason: "unapproved_destination", explicit_send: true }),
    }),
    Cancel: async (operation) => void cancels.push(operation),
  });
  await describeReplay(user);
  await user.click(screen.getByRole("button", { name: "Preview replay" }));
  await user.click(await preview().findByLabelText(/I approve sending/));
  await user.click(preview().getByRole("button", { name: "Send once" }));
  expect(await screen.findByText("Not sent: the send was refused by policy before anything was sent: unapproved_destination")).toBeTruthy();
  expect(screen.getByText("Send policy: denied (unapproved_destination)")).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Send once" })).toBeNull();

  // An unactivated window is refused a send in the admission's own words.
  facade.reply({ SendReplay: () => ({ state: "permission_denied", reason: "operation admission unavailable" }) });
  await user.click(screen.getByRole("button", { name: "Preview replay" }));
  await user.click(await preview().findByLabelText(/I approve sending/));
  await user.click(preview().getByRole("button", { name: "Send once" }));
  expect(await screen.findByText("Not sent: operation admission unavailable")).toBeTruthy();

  // A preview cancelled from the keyboard names its own operation, and the
  // answer that arrives afterwards is not shown.
  const parked = facade.park("PreviewReplay");
  await tabTo(user, screen.getByRole("button", { name: "Preview replay" }));
  await user.keyboard("{Enter}");
  expect(await screen.findByText("Preparing the replay locally. Nothing is sent.")).toBeTruthy();
  await waitFor(() => expect(document.activeElement).toBe(screen.getByRole("button", { name: "Cancel preview" })));
  await user.keyboard("{Enter}");
  expect(cancels).toEqual(["replay-preview"]);
  parked.resolve(previewed({ ...(facade.callsTo("PreviewReplay").at(-1)?.args[0] as ReplayRequest) }, { case: "A LATE ANSWER" }));
  expect(await screen.findByText("Not previewed: The preview was cancelled. Nothing was sent or written; preview again to see what would be sent.")).toBeTruthy();
  await waitFor(() => expect((screen.getByRole("button", { name: "Cancel preview" }) as HTMLButtonElement).disabled).toBe(true));
  expect(screen.queryByRole("region", { name: "Replay preview" })).toBeNull();
  expect(facade.callsTo("SendReplay")).toHaveLength(2);
});

test("without a case index every message of the case is replayed, into the run folder the person names", async () => {
  const user = userEvent.setup();
  const { facade } = renderPanel({ PreviewReplay: (request) => previewed(request) }, []);
  expect(screen.getByText("Build the case index to choose messages one by one.")).toBeTruthy();
  await name(user, "Target configuration", "downstream-target.json");
  await user.type(screen.getByLabelText("Run folder"), "reschedule-replay");
  await user.keyboard("{Enter}");
  expect(await screen.findByText("Dry run: no connection opened.")).toBeTruthy();
  expect(facade.oneCall("PreviewReplay")[0]).toEqual({
    workspace: WORKSPACE_ROOT,
    case: "case",
    identity: CASE_IDENTITY,
    target: "downstream-target.json",
    messages: [],
    transformations: [],
    output: "reschedule-replay",
    reveal: false,
  });
  expect(preview().getByText("Run folder: reschedule-replay · fresh · decision retained in reschedule-replay.decision.json")).toBeTruthy();

  // A run folder already taken is shown as the reason no send is offered.
  const taken = "that run folder or the decision file beside it already exists; a send writes both as new entries";
  facade.reply({
    PreviewReplay: (request) =>
      previewed(request, {
        destination: { name: "reschedule-replay", generated: false, fresh: false, reason: taken },
        sendable: false,
        refusal: taken,
      }),
  });
  await user.clear(screen.getByLabelText("Run folder"));
  await user.type(screen.getByLabelText("Run folder"), "reschedule-replay{Enter}");
  expect(await screen.findByText(`This preview cannot be sent: ${taken}`)).toBeTruthy();
  expect(preview().getByText(`Run folder: reschedule-replay · ${taken} · decision retained in reschedule-replay.decision.json`)).toBeTruthy();
  expect(screen.queryByLabelText(/I approve sending/)).toBeNull();
  expect(facade.callsTo("SendReplay")).toHaveLength(0);
});
