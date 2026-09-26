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
import { render, screen, waitFor, within } from "@testing-library/react";
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
  await user.selectOptions(screen.getByLabelText("Historical test file"), SPEC_ENTRY);
  await user.selectOptions(screen.getByLabelText("Current run or result"), CURRENT_ENTRY);
  if (baseline) {
    await user.selectOptions(screen.getByLabelText("Baseline run or result (optional)"), BASELINE_ENTRY);
  }
}

/** The sealed-packets area, where the export destination is chosen; the
 * synthetic section names its own destination chooser the same. */
function sealedPackets() {
  return within(screen.getByRole("heading", { name: "Sealed packets" }).closest("div")!);
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
  await user.click(screen.getByRole("button", { name: "Preview" }));
  expect(await screen.findByText("Assembly preview")).toBeTruthy();
  expect(screen.getAllByText(/boundary ack-contract/).length).toBeGreaterThan(0);
  expect(screen.getByText(/Baseline and current share one input/)).toBeTruthy();
  expect(screen.getByText(/Packet inventory:/)).toBeTruthy();
  expect(screen.getByText(/current\/ — the retained execution, byte for byte/)).toBeTruthy();
  expect(facade.callsTo("AssemblePacket")).toHaveLength(0);
  await user.click(screen.getByRole("button", { name: "Create packet" }));
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
  await user.click(screen.getByRole("button", { name: "Preview" }));
  await screen.findByText(/No observed baseline/);
  expect(screen.getByText(/none selected/)).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Create packet" }));
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
  await user.click(screen.getByRole("button", { name: "Preview" }));
  await screen.findAllByText(/never substitutes the current editable test for a historical one/);
  expect(screen.queryByRole("button", { name: "Create packet" })).toBeNull();
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
  await user.click(screen.getByRole("button", { name: "Preview" }));
  await screen.findAllByText(/already exists/);
  expect(screen.queryByRole("button", { name: "Create packet" })).toBeNull();
  expect(facade.callsTo("AssemblePacket")).toHaveLength(0);
});

test("a permission denial reports its fixed sentence and never a completed packet", async () => {
  const user = userEvent.setup();
  renderPanel({
    PreviewPacket: () => packetPreviewResult(),
    AssemblePacket: () => ({ state: "permission_denied", reason: "this account cannot write the chosen folder" }),
  });
  await selectInputs(user, false);
  await user.click(screen.getByRole("button", { name: "Preview" }));
  await screen.findByText("Assembly preview");
  await user.click(screen.getByRole("button", { name: "Create packet" }));
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
  await user.click(screen.getByRole("button", { name: "Preview" }));
  await screen.findByText("Assembly preview");
  await user.click(screen.getByRole("button", { name: "Create packet" }));
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
  await user.click(screen.getByRole("button", { name: "Preview" }));
  await screen.findByText("Assembly preview");
  const parked = facade.park("AssemblePacket");
  await user.click(screen.getByRole("button", { name: "Create packet" }));
  await screen.findByText(/Cancellation stops the copy/);
  await user.click(screen.getByRole("button", { name: "Cancel" }));
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
  await user.selectOptions(screen.getByLabelText("Packets"), PACKET_ENTRY);
  await user.click(sealedPackets().getByRole("button", { name: "Verify packet" }));
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
  await user.selectOptions(screen.getByLabelText("Packets"), PACKET_ENTRY);
  await user.click(sealedPackets().getByRole("button", { name: "Verify packet" }));
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
  await user.selectOptions(screen.getByLabelText("Packets"), PACKET_ENTRY);
  await user.click(sealedPackets().getByRole("button", { name: "Verify packet" }));
  await screen.findByText(/Verified: identity/);
  expect((screen.getByRole("button", { name: "Export review" }) as HTMLButtonElement).disabled).toBe(true);
  await user.click(sealedPackets().getByRole("button", { name: "Choose destination…" }));
  await waitFor(() => expect(screen.getByText("/chosen/review")).toBeTruthy());
  await user.click(screen.getByRole("button", { name: "Export review" }));
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
  await user.selectOptions(screen.getByLabelText("Packets"), PACKET_ENTRY);
  await user.click(sealedPackets().getByRole("button", { name: "Verify packet" }));
  await screen.findByText(/Verified: identity/);
  const choose = sealedPackets().getByRole("button", { name: "Choose destination…" });
  const exportButton = screen.getByRole("button", { name: "Export review" }) as HTMLButtonElement;

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
  await user.selectOptions(screen.getByLabelText("Packets"), PACKET_ENTRY);
  await user.click(sealedPackets().getByRole("button", { name: "Verify packet" }));
  await screen.findByText(/Verified: identity/);
  await user.click(sealedPackets().getByRole("button", { name: "Choose destination…" }));
  await waitFor(() => expect(screen.getByText("/chosen/review")).toBeTruthy());
  await user.click(screen.getByRole("button", { name: "Export review" }));
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
  await user.selectOptions(screen.getByLabelText("Reviews"), REVIEW_ENTRY);
  await user.click(screen.getByRole("button", { name: "Open review" }));
  await screen.findByText(/Verified read-only: identity eeddffee/);
  expect(screen.getByText(/report.html/)).toBeTruthy();
  expect(screen.getByText(/Runs: current passed · no baseline/)).toBeTruthy();
  expect(screen.getByText(/never a passing run or an approved disclosure/)).toBeTruthy();
  expect(screen.getByText(/Report text hidden/)).toBeTruthy();
  expect(facade.callsTo("StartDurableRun")).toHaveLength(0);
  expect(facade.callsTo("ExportPacketReview")).toHaveLength(0);
  await user.click(screen.getByRole("button", { name: "Show report" }));
  const calls = facade.callsTo("OpenPacketReview");
  expect(calls).toHaveLength(2);
  expect(calls[1]?.args[0]).toMatchObject({ reveal: true });
  expect(await screen.findByText(/REVEALED-REPORT-LINE/)).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Hide report" }));
  await screen.findByText(/Report text hidden/);
});

test("verification and portable export are separate tasks over the one selected packet", async () => {
  const user = userEvent.setup();
  const OTHER_PACKET = "packet-002";
  const packets: Artifact[] = [
    ...ENTRIES,
    { name: PACKET_ENTRY, kind: "packet", schema: "readmit-retained-packet/v1" },
    { name: OTHER_PACKET, kind: "packet", schema: "readmit-retained-packet/v1" },
  ];
  const { facade } = renderPanel(
    {
      OpenPacket: (_workspace, entry) => packetResult({ entry }),
      ChoosePacketExportPath: () => Promise.resolve({ state: "completed", path: "/chosen/review" }),
      ExportPacketReview: () => packetExportResult(),
    },
    packets,
  );
  const verification = within(sealedPackets().getByRole("heading", { name: "Verification" }).parentElement!);
  const exporting = within(sealedPackets().getByRole("heading", { name: "Portable review" }).parentElement!);
  const verify = verification.getByRole("button", { name: "Verify packet" }) as HTMLButtonElement;
  const choose = exporting.getByRole("button", { name: "Choose destination…" }) as HTMLButtonElement;
  const exportButton = exporting.getByRole("button", { name: "Export review" }) as HTMLButtonElement;
  // The read-only explanation stays beside Verify packet.
  expect(verify.getAttribute("aria-describedby")).toBeTruthy();
  expect(document.getElementById(verify.getAttribute("aria-describedby")!)?.textContent).toMatch(/read-only: nothing is executed, sent or changed/);
  // Export is its own task, offered only for a packet that verified.
  expect(verify.disabled).toBe(true);
  expect(choose.disabled).toBe(true);
  expect(exportButton.disabled).toBe(true);
  expect(exporting.getByText("Verify the selected packet before exporting it.")).toBeTruthy();

  await user.selectOptions(screen.getByLabelText("Packets"), PACKET_ENTRY);
  expect(choose.disabled).toBe(true);
  await user.click(verify);
  await verification.findByText(/Verified: identity/);
  expect(exporting.getByText(PACKET_ENTRY)).toBeTruthy();
  await user.click(choose);
  await exporting.findByText("/chosen/review");
  await user.click(exportButton);
  await exporting.findByText(/sealed: identity eeddffee/);
  expect(facade.oneCall("OpenPacket")).toEqual([WORKSPACE_ROOT, PACKET_ENTRY]);
  expect(facade.oneCall("ExportPacketReview")[0]).toMatchObject({ packet: PACKET_ENTRY, destination: "/chosen/review" });

  // Selecting another packet withdraws the verification, the destination and
  // the export: nothing verified for one packet is exported as another.
  await user.selectOptions(screen.getByLabelText("Packets"), OTHER_PACKET);
  expect(verification.queryByText(/Verified: identity/)).toBeNull();
  expect(exporting.queryByText(/sealed: identity/)).toBeNull();
  expect(exporting.getByText("No destination chosen.")).toBeTruthy();
  expect(choose.disabled).toBe(true);
  expect(exportButton.disabled).toBe(true);
  await user.click(verify);
  await verification.findByText(/Verified: identity/);
  expect(exporting.getByText(OTHER_PACKET)).toBeTruthy();
  await user.click(choose);
  await exporting.findByText("/chosen/review");
  await user.click(exportButton);
  await waitFor(() => expect(facade.callsTo("ExportPacketReview")).toHaveLength(2));
  expect(facade.callsTo("OpenPacket").map((call) => call.args[1])).toEqual([PACKET_ENTRY, OTHER_PACKET]);
  expect(facade.callsTo("ExportPacketReview")[1]?.args[0]).toMatchObject({ packet: OTHER_PACKET });
});

test("original retained packets, portable reviews, derived exports and synthetic samples are never one unlabelled list", () => {
  renderPanel({}, [
    ...ENTRIES,
    { name: PACKET_ENTRY, kind: "packet", schema: "readmit-retained-packet/v1" },
    { name: REVIEW_ENTRY, kind: "portable-review", schema: "readmit-portable-review/v1" },
    { name: "derived-export", kind: "derived-export" },
    { name: "synthetic-demo", kind: "synthetic-packet", schema: "readmit-report/v1" },
  ]);
  const options = (label: string) =>
    within(screen.getByLabelText(label)).getAllByRole("option").map((option) => (option as HTMLOptionElement).value).filter(Boolean);
  expect(options("Packets")).toEqual([PACKET_ENTRY]);
  expect(options("Reviews")).toEqual([REVIEW_ENTRY]);
  expect(screen.getByLabelText("Packets").getAttribute("aria-describedby")).toBeTruthy();
  expect(screen.queryByRole("option", { name: "derived-export" })).toBeNull();
  expect(screen.queryByRole("option", { name: "synthetic-demo" })).toBeNull();
  // The samples are their own labelled section.
  expect(screen.getByRole("region", { name: "Samples" })).toBeTruthy();
});

test("changing any input after a preview withdraws it, and a selected baseline names its case explicitly", async () => {
  const user = userEvent.setup();
  const { facade } = renderPanel({
    PreviewPacket: () => packetPreviewResult(),
    AssemblePacket: () => packetResult(),
  });
  await selectInputs(user, false);
  // The historical test file must be the exact one the run retained.
  expect(document.getElementById(screen.getByLabelText("Historical test file").getAttribute("aria-describedby")!)?.textContent).toMatch(
    /exact test file the current run retained; today's saved test is never substituted/,
  );
  expect(within(screen.getByLabelText("Baseline run or result (optional)")).getByRole("option", { name: "No baseline (single-run report)" })).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Preview" }));
  await screen.findByText(/none selected — the packet states that no observed baseline exists/);
  expect(screen.getByText(/^Historical test file: /)).toBeTruthy();
  expect(screen.getByText(/^Current run or result: /)).toBeTruthy();
  expect(screen.getByRole("button", { name: "Create packet" })).toBeTruthy();

  await user.selectOptions(screen.getByLabelText("Baseline run or result (optional)"), BASELINE_ENTRY);
  expect(screen.queryByText("Assembly preview")).toBeNull();
  expect(screen.queryByRole("button", { name: "Create packet" })).toBeNull();
  expect((screen.getByLabelText("Baseline case") as HTMLSelectElement).value).toBe("");
  expect(within(screen.getByLabelText("Baseline case")).getByRole("option", { name: "Same as the current case" })).toBeTruthy();

  await user.click(screen.getByRole("button", { name: "Preview" }));
  await screen.findByText("Assembly preview");
  await user.type(screen.getByLabelText("Packet folder"), "-edited");
  expect(screen.queryByRole("button", { name: "Create packet" })).toBeNull();
  expect(facade.callsTo("AssemblePacket")).toHaveLength(0);
  const previews = facade.callsTo("PreviewPacket").map((call) => call.args[0]);
  expect(previews[0]).not.toHaveProperty("baseline");
  expect(previews[1]).toMatchObject({ baseline: BASELINE_ENTRY, baseline_case: CASE_ENTRY });
});

test("an empty workspace offers nothing to select and the panels render their honest states", () => {
  renderPanel({}, []);
  expect((screen.getByLabelText("Case") as HTMLSelectElement).value).toBe("");
  expect((screen.getByLabelText("Packets") as HTMLSelectElement).value).toBe("");
  expect((screen.getByLabelText("Reviews") as HTMLSelectElement).value).toBe("");
});

test("PACKET_IDENTITY fixture stays well-formed for the identity displays", () => {
  expect(PACKET_IDENTITY).toMatch(/^[0-9a-f]{64}$/);
});
