// The synthetic demonstration, reached from the packet panels. A person with
// no activation at all — the synthetic walkthrough is ungated, as it is on the
// command line — names a new folder in the save dialog and generates the
// committed scenario into it against the built-in defective and fixed
// receivers the window starts on loopback. The packet is labelled synthetic in
// the panel and in the workspace listing, never as the person's own evidence,
// and `readmit report verify` verifies it under the identity the window
// showed. Runnable copies are prepared into a new folder outside it, byte for
// byte what `readmit report prepare` writes from the same packet, after the
// window refuses a folder inside the packet and an address that is not
// numeric loopback in the command line's words. Once a file of the packet is
// changed, both refuse it, and a folder that is not a packet is refused too.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, within } from "@testing-library/react";
import { byContent, enter, Journey, press, region } from "../testkit/journey";
import userEvent from "@testing-library/user-event";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

const PACKET_DIALOG = "Choose a new folder for the synthetic demonstration packet";
const VERIFY_DIALOG = "Choose a synthetic demonstration packet to verify";
const RERUN_DIALOG = "Choose a new folder for the runnable copies";

/** The synthetic demonstration section of the packet panels. */
function synthetic() {
  const packets = within(screen.getByRole("region", { name: "Investigation packets" }));
  return within(packets.getByRole("region", { name: "Synthetic demonstration packets" }));
}

/** The text of the one line a pattern matches, once the window shows it. */
async function line(scope: ReturnType<typeof within>, pattern: RegExp): Promise<string> {
  return (await scope.findByText(byContent(pattern))).textContent ?? "";
}

test("a synthetic demonstration packet is generated into a new folder, verified as readmit report verify verifies it, prepared into runnable copies outside it and refused once changed, labelled synthetic throughout", async () => {
  const user = userEvent.setup();
  journey.makeFolder("demo");
  await journey.launch();
  await journey.chooseFolder(journey.path("demo"), "Open a readmit workspace folder");
  await press(user, screen.getByRole("button", { name: "Open a workspace folder…" }));
  const panel = synthetic();

  // Dismissing the save dialog names nothing, and nothing can be generated.
  await journey.dismissDialog("save", PACKET_DIALOG);
  await press(user, panel.getByRole("button", { name: "Choose new packet folder…" }));
  expect(await panel.findByText("no new folder was named")).toBeTruthy();
  expect((panel.getByRole("button", { name: "Generate synthetic packet" }) as HTMLButtonElement).disabled).toBe(true);

  // Generated into the folder named in the save dialog, and read back as the
  // scenario states it: a defective baseline that fails with two appointment
  // records and a fixed post-fix that passes with one, over one unchanged,
  // synthetic input.
  await journey.nameNewFolder(journey.path("demo", "synthetic-demo"), PACKET_DIALOG);
  await press(user, panel.getByRole("button", { name: "Choose new packet folder…" }));
  expect(await panel.findByText(journey.path("demo", "synthetic-demo"))).toBeTruthy();
  await press(user, panel.getByRole("button", { name: "Generate synthetic packet" }));
  const verified = await line(panel, /^Verified: synthetic-demo · identity [0-9a-f]{12}… · contract readmit-report\/v1 · state complete · \d+ indexed files\.$/);
  const identity = verified.replace(/^.*identity ([0-9a-f]{12})….*$/, "$1");
  // Every file the packet's own manifest indexes is counted.
  const indexed = (JSON.parse(journey.readFile("demo/synthetic-demo/manifest.json")) as { files: unknown[] }).files.length;
  expect(verified).toContain(` · ${indexed} indexed files.`);
  expect(panel.getByText("Synthetic-only demonstration packet — never your own evidence.")).toBeTruthy();
  expect(await line(panel, /^Scenario /)).toBe(
    "Scenario siu-reschedule-v1 · provenance synthetic-only · boundary appointment-ledger · input unchanged; receiver behavior changed.",
  );
  expect(await line(panel, /^baseline: /)).toBe("baseline: assertion_failure · defective readmit built-in SIU fixture · 2 ledger records");
  expect(await line(panel, /^post-fix: /)).toBe("post-fix: pass · fixed readmit built-in SIU fixture · 1 ledger record");
  expect(await line(panel, /^Synthetic-only evidence; /)).toMatch(/no patient or customer-derived input/);

  // The listing names it a synthetic demonstration packet, and the panels for
  // the person's own retained packets do not offer it.
  const navigation = within(region("Project navigation"));
  const listed = (await navigation.findByText("synthetic-demo")).closest("li")!;
  expect(within(listed).getByText("Synthetic demonstration packet")).toBeTruthy();
  const retained = within(screen.getByRole("region", { name: "Investigation packets" })).getByLabelText("Packets of this workspace");
  expect(within(retained).queryByRole("option", { name: "synthetic-demo" })).toBeNull();

  // The command line verifies the folder the window wrote, under the same
  // identity, and reads the same synthetic label from its manifest.
  const checked = await journey.commandLine(["report", "verify", "demo/synthetic-demo"]);
  expect(checked.code, checked.stderr).toBe(0);
  expect(checked.stdout).toMatch(new RegExp(`^Packet verified: ${identity}[0-9a-f]{52}\nSynthetic-only; input unchanged; appointment-ledger boundary\n$`));
  const manifest = JSON.parse(journey.readFile("demo/synthetic-demo/manifest.json")) as { provenance: string; scenario: string };
  expect(manifest).toMatchObject({ provenance: "synthetic-only", scenario: "siu-reschedule-v1" });

  // Runnable copies are never prepared inside the sealed packet, nor for an
  // address that is not numeric loopback; the window refuses both in the
  // command line's words and writes nothing.
  const address = panel.getByLabelText("Loopback address for manual reruns") as HTMLInputElement;
  expect(address.value).toBe("127.0.0.1:2575");
  await journey.nameNewFolder(journey.path("demo", "synthetic-demo", "rerun"), RERUN_DIALOG);
  await press(user, panel.getByRole("button", { name: "Choose new folder for runnable copies…" }));
  await panel.findByText(journey.path("demo", "synthetic-demo", "rerun"));
  await press(user, panel.getByRole("button", { name: "Prepare runnable copies" }));
  const inside = "output must be outside the immutable input case or enclosing evidence";
  expect(await panel.findByText(inside)).toBeTruthy();
  const commandInside = await journey.commandLine(["report", "prepare", "demo/synthetic-demo", "--output", "demo/synthetic-demo/rerun", "--address", "127.0.0.1:2575"]);
  expect([commandInside.code, commandInside.stderr]).toEqual([1, `readmit: ${inside}\n`]);
  await enter(user, address, "192.0.2.10:2575");
  await press(user, panel.getByRole("button", { name: "Prepare runnable copies" }));
  const wide = "report preparation requires a numeric loopback address and port";
  expect(await panel.findByText(wide)).toBeTruthy();
  const commandWide = await journey.commandLine(["report", "prepare", "demo/synthetic-demo", "--output", "demo/wide-rerun", "--address", "192.0.2.10:2575"]);
  expect([commandWide.code, commandWide.stderr]).toEqual([1, `readmit: ${wide}\n`]);
  expect(panel.queryByText(/^Runnable copies prepared in/)).toBeNull();

  // Prepared into a new folder beside the packet: the command line prepares
  // the same workspace, byte for byte, from the same packet and address.
  await enter(user, address, "127.0.0.1:2575");
  await journey.nameNewFolder(journey.path("demo", "rerun"), RERUN_DIALOG);
  await press(user, panel.getByRole("button", { name: "Choose new folder for runnable copies…" }));
  await panel.findByText(journey.path("demo", "rerun"));
  await press(user, panel.getByRole("button", { name: "Prepare runnable copies" }));
  expect(await line(panel, /^Runnable copies prepared in /)).toBe(`Runnable copies prepared in rerun: packet ${identity}… · no connection opened.`);
  expect(await line(panel, /^Trials: /)).toBe(
    "Trials: baseline (defective), post-fix (fixed), reintroduced (defective) on 127.0.0.1:2575 · bindings changed: input.case, target, observation.path; input identity and assertion semantics preserved.",
  );
  expect(await line(panel, /^Synthetic-only: /)).toMatch(/never your own evidence/);
  const prepared = await journey.commandLine(["report", "prepare", "demo/synthetic-demo", "--output", "demo/command-rerun", "--address", "127.0.0.1:2575"]);
  expect(prepared.code, prepared.stderr).toBe(0);
  for (const file of ["preparation.json", "preparation.sha256", "target.json", "RERUN.md", "baseline/spec.json", "post-fix/spec.json", "reintroduced/spec.json", "reproducer/manifest.json"]) {
    expect(journey.readFile(`demo/rerun/${file}`), file).toBe(journey.readFile(`demo/command-rerun/${file}`));
  }
  // The copies keep the synthetic case, generated rather than recorded.
  const copied = JSON.parse(journey.readFile("demo/rerun/reproducer/manifest.json")) as { provenance: { mode: string } };
  expect(copied.provenance.mode).toBe("generated");

  // A folder that is not a packet is refused by both.
  await journey.chooseFolder(journey.path("demo", "rerun"), VERIFY_DIALOG);
  await press(user, panel.getByRole("button", { name: "Choose a synthetic packet…" }));
  await panel.findByText(journey.path("demo", "rerun"));
  await press(user, panel.getByRole("button", { name: "Verify synthetic packet" }));
  const invalid = "invalid, incomplete, changed, or unsupported report packet";
  expect(await panel.findByText(invalid)).toBeTruthy();
  expect(panel.queryByText(byContent(/^Verified: /))).toBeNull();
  const notAPacket = await journey.commandLine(["report", "verify", "demo/rerun"]);
  expect([notAPacket.code, notAPacket.stderr]).toEqual([1, `readmit: ${invalid}\n`]);

  // Once a file of the packet is changed, the window refuses it and offers
  // nothing to prepare from it, as the command line refuses it.
  await journey.chooseFolder(journey.path("demo", "synthetic-demo"), VERIFY_DIALOG);
  await press(user, panel.getByRole("button", { name: "Choose a synthetic packet…" }));
  await press(user, panel.getByRole("button", { name: "Verify synthetic packet" }));
  expect(await line(panel, /^Verified: synthetic-demo · /)).toBe(verified);
  journey.changeFile("demo/synthetic-demo/SUMMARY.md", "# Customer evidence\n");
  await press(user, panel.getByRole("button", { name: "Verify synthetic packet" }));
  expect(await panel.findByText(invalid)).toBeTruthy();
  expect(panel.queryByText(byContent(/^Verified: /))).toBeNull();
  expect(panel.queryByRole("heading", { name: "Runnable copies" })).toBeNull();
  const changed = await journey.commandLine(["report", "verify", "demo/synthetic-demo"]);
  expect([changed.code, changed.stderr]).toEqual([1, `readmit: ${invalid}\n`]);
});
