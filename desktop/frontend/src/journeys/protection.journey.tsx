// Retiring a protection control decides what it writes next, never what it
// wrote. A person names a new protection document, registers a control over
// the key store their administrator staged and writes a transfer package under
// it. The window asks before retiring the control; kept active once, it is
// then retired: the window shows it retired, offers it for no new package and
// still opens the package it wrote. Over the same files the command line
// reads the same retirement, refuses a new package in the operation's own
// sentence, opens the same package to the same bytes, and retires a copy of
// the document the window read to exactly the bytes the window wrote.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { byContent, enter, Journey, press, region } from "../testkit/journey";
import { activateLicense, tabTo } from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

/** Test-only key material, generated for this journey. The administrator's
 * store prints it only for the locator it was staged under. */
const KEY_MATERIAL = "test-only-not-a-real-key-7c2e91d04b3a5f68";
const LOCATOR = "lab-evidence-key";
const CONTROL = "lab-evidence";
const RETIRED = "the protection control is retired; it opens the packages it wrote and writes no new one";

/** The protection panel of the privacy region. */
function protection() {
  return within(within(region("Privacy")).getByRole("region", { name: "Protection" }));
}

/** The row of the controls table that names the control. */
function controlRow() {
  return within(protection().getByRole("cell", { name: CONTROL }).closest("tr")!);
}

test("a control is retired only once the person confirms it, writes no new package afterwards and still opens the package it wrote, exactly as readmit protect retire decides", async () => {
  const user = userEvent.setup();
  // The key store an administrator staged: a program that prints the key its
  // locator names, and nothing for any other locator.
  const store = journey.writeFile(
    "key-store/print-key",
    `#!/bin/sh\n[ "$1" = ${LOCATOR} ] || exit 3\nprintf '%s\\n' '${KEY_MATERIAL}'\n`,
    0o700,
  );
  // The configuration this person protects: the shipped reschedule test.
  journey.makeFolder("lab");
  journey.placeFixture("test-reschedule.json", "lab/reschedule-test.json");
  const specification = journey.readFile("lab/reschedule-test.json");
  const policy = ["--operation-policy", journey.path("vendor-delivered-license", "operation-policy.json")];

  await journey.launch();
  await activateLicense(user, journey);
  await journey.chooseFolder(journey.path("lab"), "Open a readmit workspace folder");
  await press(user, screen.getAllByRole("button", { name: "Open workspace…" })[0] as HTMLElement);
  // The panel is drawn once the folder is open.
  await within(region("Privacy")).findByRole("region", { name: "Protection" });
  let panel = protection();

  // No protection document exists yet: the person names one, and registering
  // the first control writes it.
  await enter(user, panel.getByLabelText("New protection document"), "protection.json");
  await press(user, panel.getByRole("button", { name: "Use this document" }));
  expect(await panel.findByText("No control is registered in protection.json yet. Registering the first one writes it.")).toBeTruthy();
  await enter(user, panel.getByLabelText("Control name"), CONTROL);
  await enter(user, panel.getByLabelText("Absolute path of the program that prints the key"), store);
  await enter(user, panel.getByLabelText("One locator argument (never key material)"), LOCATOR);
  await press(user, panel.getByRole("button", { name: "Add argument" }));
  await press(user, panel.getByRole("button", { name: "Register control" }));
  expect(await panel.findByText(/^Registered\. Registering a reference proves nothing about the store behind it/)).toBeTruthy();
  expect(controlRow().getByRole("cell", { name: "active" })).toBeTruthy();
  expect(controlRow().getByRole("cell", { name: "******** · 1 locator arguments" })).toBeTruthy();

  // Active, the control writes a package of the specification.
  await user.selectOptions(panel.getByLabelText("Control"), CONTROL);
  await user.selectOptions(panel.getByLabelText("Entries to pack (copied, never moved)"), "reschedule-test.json");
  await enter(user, panel.getByLabelText("New package folder"), "before-retirement");
  await press(user, panel.getByRole("button", { name: "Pack protected package" }));
  expect(await panel.findByText(byContent(/^Package before-retirement · /))).toHaveProperty(
    "textContent",
    `Package before-retirement · control ${CONTROL} · key generation 1 · 1 entries.`,
  );

  // A colleague keeps a copy of the document as it stands before retirement,
  // so the command line can retire the same bytes the window reads.
  journey.writeFile("colleague/protection.json", journey.readFile("lab/protection.json"));

  // Reached from the keyboard, Retire asks first. Escape keeps the control
  // active, and focus returns to Retire; nothing was asked of the facade and
  // nothing on disk changed.
  panel = protection();
  const retire = panel.getByRole("button", { name: `Retire ${CONTROL}` });
  await user.click(panel.getByLabelText("New protection document"));
  await tabTo(user, retire);
  await user.keyboard("{Enter}");
  const question = within(panel.getByRole("group", { name: `Retire ${CONTROL}?` }));
  expect(question.getByText(/It writes no new package and still opens the packages it wrote\./)).toBeTruthy();
  expect(document.activeElement).toBe(question.getByRole("button", { name: "Keep active" }));
  await user.keyboard("{Escape}");
  expect(panel.queryByRole("group", { name: `Retire ${CONTROL}?` })).toBeNull();
  expect(document.activeElement).toBe(panel.getByRole("button", { name: `Retire ${CONTROL}` }));
  expect(journey.callsTo("RetireProtectionControl")).toHaveLength(0);
  expect(journey.readFile("lab/protection.json")).toBe(journey.readFile("colleague/protection.json"));

  // Confirmed, it is retired: shown retired, no longer offered to write.
  await user.keyboard("{Enter}");
  await press(user, panel.getByRole("button", { name: "Retire it" }));
  expect(
    await panel.findByText(
      `Retired ${CONTROL}: it writes no new package and still opens the packages it wrote. Retirement is not revocation: a recipient who already has a package keeps it.`,
    ),
  ).toBeTruthy();
  expect(controlRow().getByRole("cell", { name: "retired" })).toBeTruthy();
  expect((panel.getByRole("button", { name: `Retire ${CONTROL}` }) as HTMLButtonElement).disabled).toBe(true);
  expect(within(panel.getByLabelText("Control")).getAllByRole("option").map((option) => option.textContent)).toEqual([
    "Select a control…",
  ]);
  expect(journey.callsTo("RetireProtectionControl")).toHaveLength(1);

  // The command line reads the same retirement and writes no new package
  // under it, refusing in the operation's own sentence.
  const shown = await journey.commandLine(["protect", "show", "--protection", "lab/protection.json"]);
  expect(shown.code).toBe(0);
  expect(shown.stdout).toContain(`  ${CONTROL} storage=os-volume-encryption (declared, never verified) state=retired generation=1\n`);
  const packed = await journey.commandLine([
    "protect", "pack", "--protection", "lab/protection.json", "--name", CONTROL,
    "--output", "lab/after-retirement", "lab/reschedule-test.json",
  ]);
  expect(packed).toEqual({ code: 1, stdout: "", stderr: `readmit: ${RETIRED}\n` });
  expect(() => journey.readFile("lab/after-retirement/transfer.json")).toThrow();

  // The window still opens the package the control wrote, to the exact bytes
  // packed, and so does the command line.
  await within(panel.getByLabelText("Transfer package")).findByRole("option", { name: "before-retirement" });
  await user.selectOptions(panel.getByLabelText("Transfer package"), "before-retirement");
  // Its descriptor, read without a key, now stands below the one the pack
  // answered with.
  await waitFor(() => expect(panel.getAllByText(byContent(/^Package before-retirement · control lab-evidence · key generation 1 · /))).toHaveLength(2));
  await press(user, panel.getByRole("button", { name: "Open package" }));
  const opened = (await panel.findByText(byContent(/^Opened into \S+\. /))).textContent ?? "";
  const folder = opened.replace(/^Opened into (\S+)\. .*$/, "$1");
  expect(folder).toBe("opened-001");
  expect(journey.readFile(`lab/${folder}/reschedule-test.json`)).toBe(specification);
  const commandOpened = await journey.commandLine([
    "protect", "open", "--protection", "lab/protection.json", "--package", "lab/before-retirement", "--output", "command-opened",
  ]);
  expect(commandOpened.code).toBe(0);
  expect(journey.readFile("command-opened/reschedule-test.json")).toBe(specification);

  // `readmit protect retire` over the colleague's copy writes the bytes the
  // window wrote, and refuses a second retirement in its own sentence.
  const retired = await journey.commandLine([...policy, "protect", "retire", "--protection", "colleague/protection.json", "--name", CONTROL]);
  expect(retired.code).toBe(0);
  expect(retired.stdout).toMatch(new RegExp(`^Protection control retired: ${CONTROL}\n  ${CONTROL} storage=os-volume-encryption \\(declared, never verified\\) state=retired generation=1\n`));
  expect(journey.readFile("colleague/protection.json")).toBe(journey.readFile("lab/protection.json"));
  const again = await journey.commandLine([...policy, "protect", "retire", "--protection", "lab/protection.json", "--name", CONTROL]);
  expect(again).toEqual({ code: 1, stdout: "", stderr: "readmit: the protection control is already retired\n" });

  // The key never reached the window, the document or the command's output.
  await waitFor(() => expect(journey.callsTo("OpenProtectedPackage").at(-1)?.settled).toBe(true));
  expect(document.body.textContent).not.toContain(KEY_MATERIAL);
  for (const text of [journey.readFile("lab/protection.json"), shown.stdout, retired.stdout]) {
    expect(text).not.toContain(KEY_MATERIAL);
  }
  for (const call of journey.calls) expect(JSON.stringify(call.result ?? null)).not.toContain(KEY_MATERIAL);
  // The specification that was packed is exactly as it was.
  expect(journey.readFile("lab/reschedule-test.json")).toBe(specification);
});
