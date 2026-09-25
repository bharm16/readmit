// The synthetic demonstration packets of the investigation-packet panels: a
// packet is generated into a new folder named in the save dialog, read back
// as verified and labelled synthetic in every view, a chosen packet is
// verified read-only, and runnable copies are prepared into a new folder
// outside it. A dismissed dialog names nothing, a cancelled generation names
// only its own operation and leaves an incomplete folder, and a changed,
// unsupported or refused packet is shown as itself and never as one that
// verified. The Go facade tests hold these answers to the command line's.
import { expect, test } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { PacketPanel } from "./PacketPanel";
import { installFacade } from "./testkit/wails";
import type { FacadeHandlers } from "./testkit/wails";
import type { SyntheticPacketResult, SyntheticRerunResult } from "./bindings";
import { WORKSPACE_ROOT } from "./testkit/fixtures";

const PACKET_FOLDER = "named-new-packet-folder";
const CHOSEN_PACKET = "chosen-packet-folder";
const RERUN_FOLDER = "named-new-rerun-folder";
const IDENTITY = "5566778899aabbccddeeff00112233445566778899aabbccddeeff0011223344";
const SYNTHETIC_LIMITATION =
  "Synthetic-only evidence; no patient or customer-derived input. This does not establish readiness to accept customer PHI.";

function syntheticPacket(folder = "demo-packet"): SyntheticPacketResult {
  return {
    state: "completed",
    packet: {
      folder,
      identity: IDENTITY,
      schema: "readmit-report/v1",
      state: "complete",
      scenario: "siu-reschedule-v1",
      provenance: "synthetic-only",
      input_identity: "7d266d0a09e9",
      spec_identity: "spec-identity",
      observation_boundary: "appointment-ledger",
      input_changed: false,
      receiver_behavior_changed: true,
      runs: [
        { path: "baseline", receiver_mode: "defective", receiver: "readmit built-in SIU fixture", status: "assertion_failure", ledger_count: 2, result_identity: "baseline-identity" },
        { path: "post-fix", receiver_mode: "fixed", receiver: "readmit built-in SIU fixture", status: "pass", ledger_count: 1, result_identity: "post-fix-identity" },
      ],
      files: 42,
      limitations: [SYNTHETIC_LIMITATION],
    },
  };
}

function prepared(address: string): SyntheticRerunResult {
  return {
    state: "completed",
    rerun: {
      folder: "rerun",
      address,
      packet_identity: IDENTITY,
      historical_spec_identity: "spec-identity",
      input_identity: "7d266d0a09e9",
      target_sha256: "target-identity",
      changed_bindings: ["input.case", "target", "observation.path"],
      specs: [
        { path: "baseline/spec.json", sha256: "a" },
        { path: "post-fix/spec.json", sha256: "b" },
        { path: "reintroduced/spec.json", sha256: "c" },
      ],
    },
  };
}

function renderSection(handlers: FacadeHandlers = {}) {
  const events: string[] = [];
  const facade = installFacade({
    Cancel: async () => {
      events.push("cancel");
    },
    ...handlers,
  });
  render(<PacketPanel workspace={WORKSPACE_ROOT} entries={[]} onRefresh={() => events.push("refresh")} />);
  const section = within(screen.getByRole("region", { name: "Synthetic sample packets" }));
  return { facade, events, section };
}

/** The runnable-copies block a verified packet shows; its destination chooser
 * carries the same label as the packet folder's own. */
function rerunCopies(section: ReturnType<typeof renderSection>["section"]) {
  return within(section.getByRole("heading", { name: "Runnable copies" }).closest("div")!);
}

function named(path: string) {
  return () => Promise.resolve({ state: "completed" as const, path });
}

async function tabTo(user: ReturnType<typeof userEvent.setup>, target: HTMLElement) {
  for (let step = 0; step < 60 && document.activeElement !== target; step++) await user.tab();
  expect(document.activeElement).toBe(target);
}

test("a synthetic packet is generated into a newly named folder, read back as verified and labelled synthetic, never as your own evidence", async () => {
  const user = userEvent.setup();
  const { facade, events, section } = renderSection({
    ChooseSyntheticPacketPath: named(PACKET_FOLDER),
    GenerateSyntheticPacket: () => syntheticPacket(),
  });
  const generate = section.getByRole("button", { name: "Generate sample packet" }) as HTMLButtonElement;
  expect(generate.disabled).toBe(true);
  expect(section.getByText("No new packet folder named.")).toBeTruthy();
  await user.click(section.getByRole("button", { name: "Choose destination…" }));
  expect(await section.findByText(PACKET_FOLDER)).toBeTruthy();
  expect(facade.oneCall("ChooseSyntheticPacketPath")).toEqual(["packet-destination"]);
  await user.click(generate);
  expect(facade.oneCall("GenerateSyntheticPacket")).toEqual([{ scenario: "siu-reschedule-v1", destination: PACKET_FOLDER }]);
  expect(await section.findByText("Synthetic-only demonstration packet — never your own evidence.")).toBeTruthy();
  expect(section.getByText(/^Verified: demo-packet · identity 5566778899aa… · contract readmit-report\/v1 · state complete · 42 indexed files\.$/)).toBeTruthy();
  expect(section.getByText("Scenario siu-reschedule-v1 · provenance synthetic-only · boundary appointment-ledger · input unchanged; receiver behavior changed.")).toBeTruthy();
  expect(section.getByText("baseline: assertion_failure · defective readmit built-in SIU fixture · 2 ledger records")).toBeTruthy();
  expect(section.getByText("post-fix: pass · fixed readmit built-in SIU fixture · 1 ledger record")).toBeTruthy();
  expect(section.getByText(SYNTHETIC_LIMITATION)).toBeTruthy();
  // The generated packet is now the one the section verifies and prepares
  // from, and the folder it named is used up.
  expect(section.getByText(PACKET_FOLDER)).toBeTruthy();
  expect(section.getByText("No new packet folder named.")).toBeTruthy();
  expect(section.getByRole("heading", { name: "Runnable copies" })).toBeTruthy();
  expect(events).toContain("refresh");
  // Nothing about a synthetic packet reaches the person's own evidence.
  expect(facade.callsTo("AssemblePacket")).toHaveLength(0);
  expect(facade.callsTo("ExportPacketReview")).toHaveLength(0);
});

test("a dismissed save dialog names nothing and generation stays unavailable", async () => {
  const user = userEvent.setup();
  const { facade, section } = renderSection({
    ChooseSyntheticPacketPath: () => ({ state: "cancelled", reason: "no new folder was named" }),
  });
  await user.click(section.getByRole("button", { name: "Choose destination…" }));
  expect(await section.findByText("no new folder was named")).toBeTruthy();
  expect(section.getByText("No new packet folder named.")).toBeTruthy();
  expect((section.getByRole("button", { name: "Generate sample packet" }) as HTMLButtonElement).disabled).toBe(true);
  expect(facade.callsTo("GenerateSyntheticPacket")).toHaveLength(0);
});

test("generation is cancelled from the keyboard: Cancel holds the focus, names only the synthetic operation and states the folder incomplete", async () => {
  const user = userEvent.setup();
  const { facade, events, section } = renderSection({ ChooseSyntheticPacketPath: named(PACKET_FOLDER) });
  await user.click(section.getByRole("button", { name: "Choose destination…" }));
  await section.findByText(PACKET_FOLDER);
  const parked = facade.park("GenerateSyntheticPacket");
  const generate = section.getByRole("button", { name: "Generate sample packet" });
  await tabTo(user, generate);
  await user.keyboard("{Enter}");
  expect(await section.findByText(/the synthetic messages go only to the two built-in receivers/)).toBeTruthy();
  const stop = section.getByRole("button", { name: "Cancel generation" });
  await waitFor(() => expect(document.activeElement).toBe(stop));
  await user.keyboard("{Enter}");
  expect(facade.oneCall("Cancel")).toEqual(["synthetic-packet"]);
  expect(events).toContain("cancel");
  parked.resolve({
    state: "cancelled",
    reason: "the synthetic packet generation was cancelled; the partial folder remains incomplete and cannot be verified as complete",
  });
  expect(await section.findByText(/the partial folder remains incomplete and cannot be verified as complete/)).toBeTruthy();
  expect(section.queryByText(/^Verified:/)).toBeNull();
  expect(section.queryByRole("heading", { name: "Runnable copies" })).toBeNull();
  expect(section.getByText("No synthetic packet chosen.")).toBeTruthy();
  // The folder the cancelled generation reached is used: recovery is a new
  // folder, so focus goes to the chooser that names one, and Generate waits.
  expect(section.getByText("No new packet folder named.")).toBeTruthy();
  expect((section.getByRole("button", { name: "Generate sample packet" }) as HTMLButtonElement).disabled).toBe(true);
  await waitFor(() => expect(document.activeElement).toBe(section.getByRole("button", { name: "Choose destination…" })));
  expect((stop as HTMLButtonElement).disabled).toBe(true);
  // A new folder named from the keyboard is generated into.
  facade.reply({ GenerateSyntheticPacket: () => syntheticPacket() });
  await user.keyboard("{Enter}");
  await section.findByText(PACKET_FOLDER);
  await tabTo(user, section.getByRole("button", { name: "Generate sample packet" }));
  await user.keyboard("{Enter}");
  expect(await section.findByText(/^Verified: demo-packet · /)).toBeTruthy();
  expect(facade.callsTo("GenerateSyntheticPacket")).toHaveLength(2);
});

test("a refused or busy generation is shown as itself and never as a packet", async () => {
  const user = userEvent.setup();
  const { facade, section } = renderSection({
    ChooseSyntheticPacketPath: named(PACKET_FOLDER),
    GenerateSyntheticPacket: () => ({
      state: "failed",
      reason: "cannot create report output; destination must be new and parent readable and writable",
    }),
  });
  await user.click(section.getByRole("button", { name: "Choose destination…" }));
  await section.findByText(PACKET_FOLDER);
  await user.click(section.getByRole("button", { name: "Generate sample packet" }));
  expect(await section.findByText(/destination must be new and parent readable and writable/)).toBeTruthy();
  expect(section.queryByText(/^Verified:/)).toBeNull();
  expect(section.queryByRole("heading", { name: "Runnable copies" })).toBeNull();
  expect(section.getByText("No new packet folder named.")).toBeTruthy();
  // A busy window reached nothing, so the folder stays named for a retry.
  facade.reply({ GenerateSyntheticPacket: () => ({ state: "busy", reason: "another operation is already running" }) });
  await user.click(section.getByRole("button", { name: "Choose destination…" }));
  await section.findByText(PACKET_FOLDER);
  await user.click(section.getByRole("button", { name: "Generate sample packet" }));
  expect(await section.findByText("another operation is already running")).toBeTruthy();
  expect(section.queryByText(/^Verified:/)).toBeNull();
  expect(section.getByText(PACKET_FOLDER)).toBeTruthy();
  expect((section.getByRole("button", { name: "Generate sample packet" }) as HTMLButtonElement).disabled).toBe(false);
  expect(facade.callsTo("GenerateSyntheticPacket")).toHaveLength(2);
});

test("a chosen packet is verified read-only, and a changed or unsupported one is refused by name with nothing to prepare", async () => {
  const user = userEvent.setup();
  const { facade, section } = renderSection({
    ChooseSyntheticPacketPath: named(CHOSEN_PACKET),
    OpenSyntheticPacket: () => ({ state: "failed", reason: "invalid, incomplete, changed, or unsupported report packet" }),
  });
  const verify = section.getByRole("button", { name: "Verify packet" }) as HTMLButtonElement;
  expect(verify.disabled).toBe(true);
  expect(section.getByText("No synthetic packet chosen.")).toBeTruthy();
  await user.click(section.getByRole("button", { name: "Browse…" }));
  expect(await section.findByText(CHOSEN_PACKET)).toBeTruthy();
  expect(facade.oneCall("ChooseSyntheticPacketPath")).toEqual(["packet"]);
  await user.click(verify);
  expect(await section.findByText("invalid, incomplete, changed, or unsupported report packet")).toBeTruthy();
  expect(section.queryByText(/^Verified:/)).toBeNull();
  expect(section.queryByRole("heading", { name: "Runnable copies" })).toBeNull();
  // The same packet verifies once it is intact again, from the keyboard.
  facade.reply({ OpenSyntheticPacket: () => syntheticPacket(CHOSEN_PACKET) });
  await tabTo(user, verify);
  await user.keyboard("{Enter}");
  expect(await section.findByText(/^Verified: chosen-packet-folder · identity 5566778899aa…/)).toBeTruthy();
  expect(section.getByText("Synthetic-only demonstration packet — never your own evidence.")).toBeTruthy();
  const opened = facade.callsTo("OpenSyntheticPacket");
  expect(opened.map((call) => call.args)).toEqual([[CHOSEN_PACKET], [CHOSEN_PACKET]]);
  await waitFor(() => expect(document.activeElement).toBe(verify));
});

test("runnable copies are prepared into a newly named folder on the chosen loopback address, and a refusal is shown as itself", async () => {
  const user = userEvent.setup();
  const { facade, events, section } = renderSection({
    ChooseSyntheticPacketPath: (kind) => ({ state: "completed", path: kind === "rerun-destination" ? RERUN_FOLDER : PACKET_FOLDER }),
    GenerateSyntheticPacket: () => syntheticPacket(),
    PrepareSyntheticRerun: (request) => prepared(request.address),
  });
  await user.click(section.getByRole("button", { name: "Choose destination…" }));
  await section.findByText(PACKET_FOLDER);
  await user.click(section.getByRole("button", { name: "Generate sample packet" }));
  await section.findByRole("heading", { name: "Runnable copies" });

  const address = section.getByLabelText("Loopback address for manual reruns") as HTMLInputElement;
  expect(address.value).toBe("127.0.0.1:2575");
  const prepare = section.getByRole("button", { name: "Prepare copies" }) as HTMLButtonElement;
  expect(prepare.disabled).toBe(true);
  // A dismissed save dialog names nothing here either.
  facade.reply({ ChooseSyntheticPacketPath: () => ({ state: "cancelled", reason: "no new folder was named" }) });
  await user.click(rerunCopies(section).getByRole("button", { name: "Choose destination…" }));
  expect(await section.findByText("no new folder was named")).toBeTruthy();
  expect(prepare.disabled).toBe(true);
  facade.reply({ ChooseSyntheticPacketPath: named(RERUN_FOLDER) });
  await user.click(rerunCopies(section).getByRole("button", { name: "Choose destination…" }));
  expect(await section.findByText(RERUN_FOLDER)).toBeTruthy();

  // The address is typed; preparation is started from the keyboard.
  await user.clear(address);
  await user.type(address, "chosen-loopback-address");
  await tabTo(user, prepare);
  await user.keyboard("{Enter}");
  expect(await section.findByText(/^Runnable copies prepared in rerun: packet 5566778899aa… · no connection opened\.$/)).toBeTruthy();
  expect(facade.oneCall("PrepareSyntheticRerun")).toEqual([{ packet: PACKET_FOLDER, destination: RERUN_FOLDER, address: "chosen-loopback-address" }]);
  expect(
    section.getByText(
      "Trials: baseline (defective), post-fix (fixed), reintroduced (defective) on chosen-loopback-address · bindings changed: input.case, target, observation.path; input identity and assertion semantics preserved.",
    ),
  ).toBeTruthy();
  expect(section.getByText(/^Synthetic-only: the copies rerun the committed synthetic scenario/)).toBeTruthy();
  expect(section.getByText("No folder named for the runnable copies.")).toBeTruthy();
  await waitFor(() => expect(document.activeElement).toBe(rerunCopies(section).getByRole("button", { name: "Choose destination…" })));
  expect(events.filter((event) => event === "refresh")).toHaveLength(2);

  // A refusal is the operation's own sentence and never reads as prepared.
  facade.reply({
    ChooseSyntheticPacketPath: named(RERUN_FOLDER),
    PrepareSyntheticRerun: () => ({ state: "failed", reason: "report preparation requires a numeric loopback address and port" }),
  });
  await user.click(rerunCopies(section).getByRole("button", { name: "Choose destination…" }));
  await section.findByText(RERUN_FOLDER);
  await user.clear(address);
  await user.type(address, "wide-address");
  expect(section.queryByText(/^Runnable copies prepared in/)).toBeNull();
  await user.click(prepare);
  expect(await section.findByText("report preparation requires a numeric loopback address and port")).toBeTruthy();
  expect(section.queryByText(/^Runnable copies prepared in/)).toBeNull();
  expect(facade.callsTo("PrepareSyntheticRerun")).toHaveLength(2);
});
