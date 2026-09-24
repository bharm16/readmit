// The packet journeys: a packet is assembled from actual retained evidence
// after a preview shows the inputs, their boundaries and every mismatch;
// assembly reads the identity back and registers the packet; export seals all
// five offline renderings into a natively chosen destination; and packets and
// portable reviews reopen read-only with the report text revealed only on
// purpose. A missing baseline is stated as absent, a mismatched historical
// specification is never substituted, a cancelled or failed write leaves its
// destination explicitly incomplete, and opening anything here acquires no
// authority at all.
import { expect, test } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { PacketPanel } from "./PacketPanel";
import { installFacade } from "./testkit/wails";
import type { FacadeHandlers } from "./testkit/wails";
import type { Artifact } from "./bindings";
import {
  PACKET_IDENTITY,
  packetExportResult,
  packetPreviewResult,
  packetResult,
  packetReviewResult,
  WORKSPACE_ROOT,
} from "./testkit/fixtures";

const CASE_ENTRY = "regression";
const SPEC_ENTRY = "reschedule-test.json";
const CURRENT_ENTRY = "job-001";
const BASELINE_ENTRY = "job-000";
const PACKET_ENTRY = "packet-001";
const REVIEW_ENTRY = "review";

const ENTRIES: Artifact[] = [
  { name: CASE_ENTRY, kind: "case", schema: "readmit-case/v3", provenance: "imported" },
  { name: SPEC_ENTRY, kind: "spec" },
  { name: CURRENT_ENTRY, kind: "job" },
  { name: BASELINE_ENTRY, kind: "job" },
];

function renderPanel(handlers: FacadeHandlers = {}, entries: Artifact[] = ENTRIES, workspace: string | null = WORKSPACE_ROOT) {
  const events: string[] = [];
  const facade = installFacade({
    Cancel: async () => {
      events.push("cancel");
    },
    ...handlers,
  });
  render(
    <PacketPanel
      workspace={workspace}
      entries={entries}
      onRefresh={() => events.push("refresh")}
    />,
  );
  return { facade, events };
}

async function selectInputs(user: ReturnType<typeof userEvent.setup>, baseline: boolean) {
  await user.selectOptions(screen.getByLabelText("Case"), CASE_ENTRY);
  await user.selectOptions(screen.getByLabelText("Historical specification"), SPEC_ENTRY);
  await user.selectOptions(screen.getByLabelText("Current result"), CURRENT_ENTRY);
  if (baseline) {
    await user.selectOptions(screen.getByLabelText("Baseline (optional)"), BASELINE_ENTRY);
  }
}

test("actual retained baseline and current are previewed, assembled, and the identity is read back and registered", async () => {
  const user = userEvent.setup();
  const { facade, events } = renderPanel({
    PreviewPacket: (request) => {
      expect(request.workspace).toBe(WORKSPACE_ROOT);
      expect(request.case).toBe(CASE_ENTRY);
      expect(request.spec).toBe(SPEC_ENTRY);
      expect(request.current).toBe(CURRENT_ENTRY);
      expect(request.baseline).toBe(BASELINE_ENTRY);
      expect(request.baseline_case).toBe(CASE_ENTRY);
      return packetPreviewResult({
        baseline_supplied: true,
        baseline: { entry: BASELINE_ENTRY, found: true, status: "assertion_failure", boundary: "ack-contract", problems: [] },
        limitations: [
          "Baseline and current share one input and one target configuration identity; their outcomes differ only as the retained evidence records.",
        ],
      });
    },
    AssemblePacket: (request) => {
      expect(request.output).toBe(PACKET_ENTRY);
      return packetResult();
    },
  });
  await selectInputs(user, true);
  await user.click(screen.getByRole("button", { name: "Preview assembly" }));
  expect(await screen.findByText("Assembly preview")).toBeTruthy();
  expect(screen.getAllByText(/boundary ack-contract/).length).toBeGreaterThan(0);
  expect(screen.getByText(/Baseline and current share one input/)).toBeTruthy();
  expect(screen.getByText(/Packet inventory:/)).toBeTruthy();
  expect(screen.getByText(/current\/ — the retained execution, byte for byte/)).toBeTruthy();
  expect(facade.callsTo("AssemblePacket")).toHaveLength(0);
  await user.click(screen.getByRole("button", { name: "Assemble packet" }));
  await screen.findByText(/sealed: identity 112233445566/);
  // The packet registered, and the panel says where it goes next without
  // doing it: privacy review is a separate, deliberate step.
  expect(events).toContain("refresh");
  expect(screen.getByText(/registered in the workspace navigation/)).toBeTruthy();
  expect(screen.getByText(/Ready for privacy review/)).toBeTruthy();
  expect(screen.getByText(/nothing was uploaded or shared/i)).toBeTruthy();
});

test("a missing baseline stays absent: the single-run limitation is shown and assembly still works", async () => {
  const user = userEvent.setup();
  const { facade } = renderPanel({
    PreviewPacket: () => packetPreviewResult(),
    AssemblePacket: () => packetResult(),
  });
  await selectInputs(user, false);
  await user.click(screen.getByRole("button", { name: "Preview assembly" }));
  await screen.findByText(/No observed baseline/);
  expect(screen.getByText(/none selected/)).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Assemble packet" }));
  await screen.findByText(/sealed: identity/);
  expect(facade.callsTo("AssemblePacket")).toHaveLength(1);
});

test("a mismatched historical specification is named before assembly and offers nothing to run", async () => {
  const user = userEvent.setup();
  const { facade } = renderPanel({
    PreviewPacket: () =>
      packetPreviewResult({
        spec: {
          entry: SPEC_ENTRY,
          found: true,
          spec_match: false,
          problems: [
            "the specification is not the exact one the current result retained; assembly never substitutes the current editable test for a historical one",
          ],
        },
        problems: ["the specification is not the exact one the current result retained; assembly never substitutes the current editable test for a historical one"],
      }),
  });
  await selectInputs(user, false);
  await user.click(screen.getByRole("button", { name: "Preview assembly" }));
  await screen.findAllByText(/never substitutes the current editable test for a historical one/);
  expect(screen.queryByRole("button", { name: "Assemble packet" })).toBeNull();
  expect(facade.callsTo("AssemblePacket")).toHaveLength(0);
});

test("an output collision is named before assembly and offers nothing to run", async () => {
  const user = userEvent.setup();
  const { facade } = renderPanel({
    PreviewPacket: () =>
      packetPreviewResult({
        destination: { name: PACKET_ENTRY, generated: false, fresh: false, reason: "the packet folder already exists; assembly requires a fresh destination" },
        problems: ["the packet folder already exists; assembly requires a fresh destination"],
      }),
  });
  await selectInputs(user, false);
  await user.click(screen.getByRole("button", { name: "Preview assembly" }));
  await screen.findAllByText(/already exists/);
  expect(screen.queryByRole("button", { name: "Assemble packet" })).toBeNull();
  expect(facade.callsTo("AssemblePacket")).toHaveLength(0);
});

test("a permission denial reports its fixed sentence and never a completed packet", async () => {
  const user = userEvent.setup();
  renderPanel({
    PreviewPacket: () => packetPreviewResult(),
    AssemblePacket: () => ({ state: "permission_denied", reason: "this account cannot write the chosen folder" }),
  });
  await selectInputs(user, false);
  await user.click(screen.getByRole("button", { name: "Preview assembly" }));
  await screen.findByText("Assembly preview");
  await user.click(screen.getByRole("button", { name: "Assemble packet" }));
  expect(await screen.findByText(/cannot write the chosen folder/)).toBeTruthy();
  expect(screen.queryByText(/sealed: identity/)).toBeNull();
});

test("a failed write names the incomplete output it left and never reads as complete", async () => {
  const user = userEvent.setup();
  renderPanel({
    PreviewPacket: () => packetPreviewResult(),
    AssemblePacket: () => ({
      state: "failed",
      reason: "cannot write report evidence; incomplete output retained",
    }),
  });
  await selectInputs(user, false);
  await user.click(screen.getByRole("button", { name: "Preview assembly" }));
  await screen.findByText("Assembly preview");
  await user.click(screen.getByRole("button", { name: "Assemble packet" }));
  expect(await screen.findByText(/incomplete output retained/)).toBeTruthy();
  expect(screen.queryByText(/sealed: identity/)).toBeNull();
});

test("cancelling names the packet operation and says a partial destination stays incomplete", async () => {
  const user = userEvent.setup();
  const { facade } = renderPanel({
    PreviewPacket: () => packetPreviewResult(),
    AssemblePacket: () => ({
      state: "cancelled",
      reason: "the packet operation was cancelled; any partial destination remains incomplete and cannot be verified as complete",
    }),
  });
  await selectInputs(user, false);
  await user.click(screen.getByRole("button", { name: "Preview assembly" }));
  await screen.findByText("Assembly preview");
  const parked = facade.park("AssemblePacket");
  await user.click(screen.getByRole("button", { name: "Assemble packet" }));
  await screen.findByText(/Cancellation stops the copy/);
  await user.click(screen.getByRole("button", { name: "Cancel packet work" }));
  expect(facade.oneCall("Cancel")).toEqual(["packet"]);
  parked.resolve({
    state: "cancelled",
    reason: "the packet operation was cancelled; any partial destination remains incomplete and cannot be verified as complete",
  });
  await screen.findByText(/remains incomplete and cannot be verified as complete/);
});

test("a corrupted packet is refused by name and nothing is rendered as verified", async () => {
  const user = userEvent.setup();
  const packets: Artifact[] = [...ENTRIES, { name: PACKET_ENTRY, kind: "packet", schema: "readmit-retained-packet/v1" }];
  const { facade } = renderPanel(
    {
      OpenPacket: (_workspace, entry) => {
        expect(entry).toBe(PACKET_ENTRY);
        return { state: "failed", reason: "invalid, incomplete, changed or unsupported retained packet" };
      },
    },
    packets,
  );
  await user.selectOptions(screen.getByLabelText("Packets of this workspace"), PACKET_ENTRY);
  await user.click(screen.getByRole("button", { name: "Verify read-only" }));
  await screen.findByText(/invalid, incomplete, changed or unsupported retained packet/);
  expect(screen.queryByText(/Verified: identity/)).toBeNull();
  expect(facade.callsTo("ExportPacketReview")).toHaveLength(0);
});

test("a packet from an unsupported version is refused and nothing is rendered as verified", async () => {
  const user = userEvent.setup();
  const packets: Artifact[] = [...ENTRIES, { name: PACKET_ENTRY, kind: "packet", schema: "readmit-retained-packet/v1" }];
  const { facade } = renderPanel(
    {
      OpenPacket: () => ({
        state: "failed",
        reason: "the evidence was evaluated by a version this release cannot read; it has not been changed",
      }),
    },
    packets,
  );
  await user.selectOptions(screen.getByLabelText("Packets of this workspace"), PACKET_ENTRY);
  await user.click(screen.getByRole("button", { name: "Verify read-only" }));
  expect(await screen.findByText(/a version this release cannot read/)).toBeTruthy();
  expect(facade.callsTo("ExportPacketReview")).toHaveLength(0);
});

test("export seals all five renderings into a natively chosen destination and refuses without one", async () => {
  const user = userEvent.setup();
  const packets: Artifact[] = [...ENTRIES, { name: PACKET_ENTRY, kind: "packet", schema: "readmit-retained-packet/v1" }];
  const { facade } = renderPanel(
    {
      OpenPacket: () => packetResult(),
      ChoosePacketExportPath: () => Promise.resolve({ state: "completed", path: "/chosen/review" }),
      ExportPacketReview: (request) => {
        expect(request.packet).toBe(PACKET_ENTRY);
        expect(request.destination).toBe("/chosen/review");
        return packetExportResult();
      },
    },
    packets,
  );
  await user.selectOptions(screen.getByLabelText("Packets of this workspace"), PACKET_ENTRY);
  await user.click(screen.getByRole("button", { name: "Verify read-only" }));
  await screen.findByText(/Verified: identity/);
  expect((screen.getByRole("button", { name: "Export portable review" }) as HTMLButtonElement).disabled).toBe(true);
  await user.click(screen.getByRole("button", { name: "Choose destination…" }));
  await waitFor(() => expect(screen.getByText("/chosen/review")).toBeTruthy());
  await user.click(screen.getByRole("button", { name: "Export portable review" }));
  await screen.findByText(/sealed: identity eeddffee/);
  expect(screen.getByText(/offline HTML, PDF, Markdown, strict JSON, JUnit/)).toBeTruthy();
  expect(facade.oneCall("ExportPacketReview")[0]).toMatchObject({ packet: PACKET_ENTRY, destination: "/chosen/review" });
  expect(facade.callsTo("OpenPacketReview")).toHaveLength(0);

});

test("packet export chooser shows dismissal and unavailability reasons", async () => {
  const user = userEvent.setup();
  const packets: Artifact[] = [...ENTRIES, { name: PACKET_ENTRY, kind: "packet", schema: "readmit-retained-packet/v1" }];
  const { facade } = renderPanel(
    {
      OpenPacket: () => packetResult(),
      ChoosePacketExportPath: () => ({ state: "cancelled", reason: "no new folder was named" }),
    },
    packets,
  );
  await user.selectOptions(screen.getByLabelText("Packets of this workspace"), PACKET_ENTRY);
  await user.click(screen.getByRole("button", { name: "Verify read-only" }));
  await screen.findByText(/Verified: identity/);
  const choose = screen.getByRole("button", { name: "Choose destination…" });
  const exportButton = screen.getByRole("button", { name: "Export portable review" }) as HTMLButtonElement;

  await user.click(choose);
  expect(await screen.findByText("no new folder was named")).toBeTruthy();
  expect(screen.getByText("No destination chosen.")).toBeTruthy();
  expect(exportButton.disabled).toBe(true);

  facade.reply({ ChoosePacketExportPath: () => ({ state: "failed", reason: "the save dialog is unavailable" }) });
  await user.click(choose);
  expect(await screen.findByText("the save dialog is unavailable")).toBeTruthy();
  expect(screen.queryByText("no new folder was named")).toBeNull();
  expect(screen.getByText("No destination chosen.")).toBeTruthy();
  expect(exportButton.disabled).toBe(true);
  expect(facade.callsTo("ExportPacketReview")).toHaveLength(0);

  facade.reply({ ChoosePacketExportPath: () => ({ state: "completed", path: "/chosen/review" }) });
  await user.click(choose);
  expect(await screen.findByText("/chosen/review")).toBeTruthy();
  expect(screen.queryByText("the save dialog is unavailable")).toBeNull();
  expect(facade.callsTo("ChoosePacketExportPath")).toHaveLength(3);
});

test("an export refusal is shown as itself and never as a sealed review", async () => {
  const user = userEvent.setup();
  const packets: Artifact[] = [...ENTRIES, { name: PACKET_ENTRY, kind: "packet", schema: "readmit-retained-packet/v1" }];
  const { facade } = renderPanel(
    {
      OpenPacket: () => packetResult(),
      ChoosePacketExportPath: () => Promise.resolve({ state: "completed", path: "/chosen/review" }),
      ExportPacketReview: () => ({
        state: "failed",
        reason: "artifact destination must name a new file or directory",
      }),
    },
    packets,
  );
  await user.selectOptions(screen.getByLabelText("Packets of this workspace"), PACKET_ENTRY);
  await user.click(screen.getByRole("button", { name: "Verify read-only" }));
  await screen.findByText(/Verified: identity/);
  await user.click(screen.getByRole("button", { name: "Choose destination…" }));
  await waitFor(() => expect(screen.getByText("/chosen/review")).toBeTruthy());
  await user.click(screen.getByRole("button", { name: "Export portable review" }));
  expect(await screen.findByText(/must name a new file or directory/)).toBeTruthy();
  expect(screen.queryByText(/sealed: identity/)).toBeNull();
  expect(facade.callsTo("ExportPacketReview")).toHaveLength(1);
});

test("a portable review opens read-only and reveals the report text only on purpose", async () => {
  const user = userEvent.setup();
  const reviews: Artifact[] = [...ENTRIES, { name: REVIEW_ENTRY, kind: "portable-review", schema: "readmit-portable-review/v1" }];
  const { facade } = renderPanel(
    {
      OpenPacketReview: (request) => {
        expect(request.entry).toBe(REVIEW_ENTRY);
        return packetReviewResult(request.reveal);
      },
    },
    reviews,
  );
  await user.selectOptions(screen.getByLabelText("Reviews of this workspace"), REVIEW_ENTRY);
  await user.click(screen.getByRole("button", { name: "Open read-only" }));
  await screen.findByText(/Verified read-only: identity eeddffee/);
  expect(screen.getByText(/report.html/)).toBeTruthy();
  expect(screen.getByText(/Runs: current passed · no baseline/)).toBeTruthy();
  expect(screen.getByText(/never a passing run or an approved disclosure/)).toBeTruthy();
  expect(screen.getByText(/Report text hidden/)).toBeTruthy();
  expect(facade.callsTo("StartDurableRun")).toHaveLength(0);
  expect(facade.callsTo("ExportPacketReview")).toHaveLength(0);
  await user.click(screen.getByRole("button", { name: "Reveal report text" }));
  const calls = facade.callsTo("OpenPacketReview");
  expect(calls).toHaveLength(2);
  expect(calls[1]?.args[0]).toMatchObject({ reveal: true });
  expect(await screen.findByText(/REVEALED-REPORT-LINE/)).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Hide report text" }));
  await screen.findByText(/Report text hidden/);
});

test("an empty workspace offers nothing to select and the panels render their honest states", () => {
  renderPanel({}, []);
  expect((screen.getByLabelText("Case") as HTMLSelectElement).value).toBe("");
  expect((screen.getByLabelText("Packets of this workspace") as HTMLSelectElement).value).toBe("");
  expect((screen.getByLabelText("Reviews of this workspace") as HTMLSelectElement).value).toBe("");
});

test("PACKET_IDENTITY fixture stays well-formed for the identity displays", () => {
  expect(PACKET_IDENTITY).toMatch(/^[0-9a-f]{64}$/);
});
