// Retiring an encryption control decides what it writes next, never what it
// wrote. A person adds a control to their project in Settings › Security ›
// Encryption over the key store their administrator staged, and checks it;
// the administrator records a rotation and writes a transfer package under it
// from the command line. Retire asks first; cancelled once, the control stays
// active, and confirmed it is retired: the window shows it retired, offers
// no further change to it, and still decrypts the package it wrote from
// Encrypted packages. Over the same files the command line reads the same
// retirement, refuses a new package in the operation's own sentence, opens
// the same package to the same bytes, and retires a copy of the document the
// window read to exactly the bytes the window wrote.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { enter, Journey, press } from "../testkit/journey";
import { goToView, page } from "../testkit/navigation";
import { licensedProject } from "./steps";

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

/** Encryption › Encrypted packages, opened from its More menu. */
async function encryptedPackages(user: ReturnType<typeof userEvent.setup>) {
  await press(user, page().getByRole("button", { name: "More encryption actions" }));
  await press(user, await screen.findByRole("menuitem", { name: "Encrypted packages" }));
  await page().findByRole("heading", { level: 1, name: "Encrypted packages" });
}

/** The detail of the control, opened from its row in the controls table. */
async function controlDetail(user: ReturnType<typeof userEvent.setup>) {
  if (!screen.queryByRole("dialog", { name: CONTROL })) {
    await press(user, await page().findByRole("row", { name: CONTROL }));
  }
  return within(await screen.findByRole("dialog", { name: CONTROL }));
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
  const policy = ["--operation-policy", journey.path("vendor-delivered-license", "operation-policy.json")];

  const folder = await licensedProject(journey, user);
  expect(folder.startsWith(`${journey.root}/`)).toBe(true);
  const lab = folder.slice(journey.root.length + 1);
  // The configuration this person protects: the shipped reschedule test,
  // placed in the project.
  journey.placeFixture("test-reschedule.json", `${lab}/reschedule-test.json`);
  const specification = journey.readFile(`${lab}/reschedule-test.json`);

  // Settings › Security › Encryption starts with no control; Add control
  // registers the reference, choosing the key program in the host's dialog.
  await goToView(user, "Settings", "Security");
  await press(user, page().getByRole("button", { name: "Encryption" }));
  expect(await page().findByText("No encryption controls")).toBeTruthy();
  await press(user, page().getByRole("button", { name: "Add control" }));
  const sheet = within(await screen.findByRole("dialog", { name: "Add control" }));
  // A person types once the page has finished drawing.
  await journey.settled();
  await enter(user, sheet.getByLabelText("Name"), CONTROL);
  await journey.chooseFiles([store], "Choose the locator program");
  await journey.settled();
  await press(user, sheet.getByRole("button", { name: "Choose key program" }));
  await press(user, sheet.getByRole("button", { name: "Add argument" }));
  await enter(user, sheet.getByLabelText("Argument 1"), LOCATOR);
  await press(user, sheet.getByRole("button", { name: "Save" }));

  await journey.settled();
  // The saved control opens on its values: active at generation 1, the
  // argument counted and never shown; checking it reads the key.
  let detail = await controlDetail(user);
  expect(detail.getByText("Active")).toBeTruthy();
  expect(detail.getByText("1 stored")).toBeTruthy();
  await press(user, detail.getByRole("button", { name: `More actions for ${CONTROL}` }));
  await press(user, await screen.findByRole("menuitem", { name: "Check control" }));
  expect(await detail.findByText("Key resolved · generation 1")).toBeTruthy();
  await user.keyboard("{Escape}");
  await waitFor(() => expect(screen.queryByRole("dialog", { name: CONTROL })).toBeNull());

  // The administrator records a rotation and writes a transfer package of
  // the specification under the control from the command line; the window
  // reads the control at its new generation.
  const rotated = await journey.commandLine([...policy, "protect", "rotate", "--protection", `${lab}/protection.json`, "--name", CONTROL]);
  expect(rotated.code, rotated.stderr).toBe(0);
  const written = await journey.commandLine([...policy, "protect", "pack", "--protection", `${lab}/protection.json`, "--name", CONTROL,
    "--output", `${lab}/before-retirement`, `${lab}/reschedule-test.json`]);
  expect(written.code, written.stderr).toBe(0);
  await press(user, page().getByRole("button", { name: "Back to settings" }));
  await press(user, await page().findByRole("button", { name: "Encryption" }));
  detail = await controlDetail(user);
  expect(await detail.findByText("2")).toBeTruthy();
  await user.keyboard("{Escape}");
  await waitFor(() => expect(screen.queryByRole("dialog", { name: CONTROL })).toBeNull());

  // A colleague keeps a copy of the document as it stands before retirement,
  // so the command line can retire the same bytes the window reads.
  journey.writeFile("colleague/protection.json", journey.readFile(`${lab}/protection.json`));

  // Retire asks first. Cancel keeps the control active; nothing was asked of
  // the facade and nothing on disk changed.
  detail = await controlDetail(user);
  await press(user, detail.getByRole("button", { name: `More actions for ${CONTROL}` }));
  await press(user, await screen.findByRole("menuitem", { name: "Retire control…" }));
  let question = within(await screen.findByRole("dialog", { name: `Retire ${CONTROL}?` }));
  expect(question.getByText("Stops new encrypted packages; existing packages remain readable.")).toBeTruthy();
  expect(document.activeElement).toBe(question.getByRole("button", { name: "Cancel" }));
  await press(user, question.getByRole("button", { name: "Cancel" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: `Retire ${CONTROL}?` })).toBeNull());
  expect(journey.callsTo("RetireProtectionControl")).toHaveLength(0);
  expect(journey.readFile(`${lab}/protection.json`)).toBe(journey.readFile("colleague/protection.json"));

  // Confirmed, it is retired: shown retired, no longer offered to write.
  detail = await controlDetail(user);
  await press(user, detail.getByRole("button", { name: `More actions for ${CONTROL}` }));
  await press(user, await screen.findByRole("menuitem", { name: "Retire control…" }));
  question = within(await screen.findByRole("dialog", { name: `Retire ${CONTROL}?` }));
  await press(user, question.getByRole("button", { name: "Retire" }));
  detail = await controlDetail(user);
  expect(await detail.findByText("Retired", { selector: ".dialog-status" })).toBeTruthy();
  expect((detail.getByRole("button", { name: "Edit" }) as HTMLButtonElement).disabled).toBe(true);
  await press(user, detail.getByRole("button", { name: `More actions for ${CONTROL}` }));
  for (const item of ["Record rotation", "Retire control…"]) {
    expect((screen.getByRole("menuitem", { name: item }) as HTMLButtonElement).disabled).toBe(true);
  }
  await user.keyboard("{Escape}");
  expect(journey.callsTo("RetireProtectionControl")).toHaveLength(1);
  await user.keyboard("{Escape}");
  await waitFor(() => expect(screen.queryByRole("dialog", { name: CONTROL })).toBeNull());

  // The command line reads the same retirement and writes no new package
  // under it, refusing in the operation's own sentence.
  const shown = await journey.commandLine(["protect", "show", "--protection", `${lab}/protection.json`]);
  expect(shown.code).toBe(0);
  expect(shown.stdout).toContain(`  ${CONTROL} storage=os-volume-encryption (declared, never verified) state=retired generation=2\n`);
  const packed = await journey.commandLine([
    "protect", "pack", "--protection", `${lab}/protection.json`, "--name", CONTROL,
    "--output", `${lab}/after-retirement`, `${lab}/reschedule-test.json`,
  ]);
  expect(packed).toEqual({ code: 1, stdout: "", stderr: `readmit: ${RETIRED}\n` });
  expect(() => journey.readFile(`${lab}/after-retirement/transfer.json`)).toThrow();

  // The window still decrypts the package the control wrote, to the exact
  // bytes packed, into a new folder named in the save dialog, and so does the
  // command line.
  await encryptedPackages(user);
  await press(user, await page().findByRole("row", { name: "before-retirement" }));
  const chosen = within(await page().findByRole("region", { name: "before-retirement" }));
  expect(within(chosen.getByLabelText("Package", { selector: "dl" })).getByText(`${CONTROL} · Generation 2`)).toBeTruthy();
  await press(user, chosen.getByRole("button", { name: "Decrypt" }));
  const decrypt = within(await screen.findByRole("dialog", { name: "Decrypt" }));
  journey.makeFolder("decrypted");
  await journey.nameNewFolder(journey.path("decrypted", "before-retirement"), "Export package");
  await press(user, decrypt.getByRole("button", { name: "Choose" }));
  expect(await decrypt.findByText(/^before-retirement · decrypted$/)).toBeTruthy();
  await press(user, decrypt.getByRole("button", { name: "Decrypt" }));
  expect(await chosen.findByText("Decrypted before-retirement")).toBeTruthy();
  expect(journey.readFile("decrypted/before-retirement/reschedule-test.json")).toBe(specification);
  const commandOpened = await journey.commandLine([
    "protect", "open", "--protection", `${lab}/protection.json`, "--package", `${lab}/before-retirement`, "--output", "command-opened",
  ]);
  expect(commandOpened.code).toBe(0);
  expect(journey.readFile("command-opened/reschedule-test.json")).toBe(specification);

  // `readmit protect retire` over the colleague's copy writes the bytes the
  // window wrote, and refuses a second retirement in its own sentence.
  const retired = await journey.commandLine([...policy, "protect", "retire", "--protection", "colleague/protection.json", "--name", CONTROL]);
  expect(retired.code).toBe(0);
  expect(retired.stdout).toMatch(new RegExp(`^Protection control retired: ${CONTROL}\n  ${CONTROL} storage=os-volume-encryption \\(declared, never verified\\) state=retired generation=2\n`));
  expect(journey.readFile("colleague/protection.json")).toBe(journey.readFile(`${lab}/protection.json`));
  const again = await journey.commandLine([...policy, "protect", "retire", "--protection", `${lab}/protection.json`, "--name", CONTROL]);
  expect(again).toEqual({ code: 1, stdout: "", stderr: "readmit: the protection control is already retired\n" });

  // The key never reached the window, the document or the command's output.
  expect(document.body.textContent).not.toContain(KEY_MATERIAL);
  for (const text of [journey.readFile(`${lab}/protection.json`), shown.stdout, retired.stdout]) {
    expect(text).not.toContain(KEY_MATERIAL);
  }
  for (const call of journey.calls) expect(JSON.stringify(call.result ?? null)).not.toContain(KEY_MATERIAL);
  // The specification that was packed is exactly as it was.
  expect(journey.readFile(`${lab}/reschedule-test.json`)).toBe(specification);
});
