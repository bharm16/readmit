// The environment a person keeps beside a project, authored in the window
// over the real facade: credential references registered, refused, edited,
// cancelled and bound to the environment's connection, its allowed
// destinations and its reset. Each whole-environment save is recorded in the
// project's catalog under the SHA-256 of the bytes it wrote, and the journey
// states that identity from the file on disk rather than from what the window
// answered. The command line reads every document the window wrote unchanged,
// and the window opens and edits what the command line wrote.
//
// No credential value exists anywhere in this journey. A reference names the
// program that would read a credential and the locator arguments that select
// it; nothing here resolves one, and no locator argument the person typed is
// ever rendered back to them.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { enter, Journey, press } from "../testkit/journey";
import { goTo, page } from "../testkit/navigation";
import { licensedProject } from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

/** The project folder, relative to the journey's root, once it is created. */
let PROJECT = "";
const ENVIRONMENT = "Lab SIU";

async function project(user: UserEvent): Promise<void> {
  const folder = await licensedProject(journey, user);
  PROJECT = folder.slice(journey.path().length + 1);
}

/** The command line over the journey's root, admitted by the activation the
 * vendor delivered, as a person working beside the window runs it. */
function commandLine(args: string[]) {
  return journey.commandLine(["--operation-policy", journey.path("vendor-delivered-license", "operation-policy.json"), ...args]);
}

type Catalog = { items: { kind: string; name?: string; revisions: { members: { role: string; path: string; sha256: string }[] }[] }[] };

/** The environment's saved revisions, as the project's catalog records them. */
function revisions(name: string) {
  const catalog = JSON.parse(journey.readFile(`${PROJECT}/.readmit/catalog.json`)) as Catalog;
  return catalog.items.find((entry) => entry.kind === "environment" && entry.name === name)?.revisions ?? [];
}

/** One role of the environment's current revision: the file it is kept in and
 * the identity the catalog records for its bytes. */
function member(name: string, role: string): { path: string; sha256: string } {
  const found = revisions(name).at(-1)?.members.find((entry) => entry.role === role);
  if (!found) throw new Error(`${name} has no ${role}`);
  return { path: `${PROJECT}/${found.path}`, sha256: found.sha256 };
}

/** Presses a control that starts work in the facade once the window has
 * finished what it was reading, as a person acts on what has drawn: the
 * facade runs one operation at a time and refuses a click that meets a read
 * the window issued on its own. */
async function act(user: UserEvent, control: HTMLElement): Promise<void> {
  await journey.settled();
  await press(user, control);
}

/** Adds a named nonproduction environment at an address, over TLS or plain
 * TCP/MLLP, and lands on its page. */
async function addEnvironment(user: UserEvent, address: string, transport: "TLS" | "TCP/MLLP"): Promise<void> {
  const [host, port] = address.split(":") as [string, string];
  await goTo(user, "Environments");
  await press(user, (await page().findAllByRole("button", { name: "Add environment" }))[0]!);
  const sheet = within(await screen.findByRole("dialog", { name: "Add environment" }));
  await enter(user, await sheet.findByRole("textbox", { name: "Name" }), ENVIRONMENT);
  await enter(user, sheet.getByRole("textbox", { name: "Host" }), host);
  await enter(user, sheet.getByRole("textbox", { name: "Port" }), port);
  await user.selectOptions(sheet.getByRole("combobox", { name: "Classification" }), "nonproduction");
  await user.click(sheet.getByRole("radio", { name: transport }));
  await act(user, sheet.getByRole("button", { name: "Save" }));
  await page().findByRole("heading", { level: 1, name: ENVIRONMENT });
}

/** Opens one item of the environment's More menu. */
async function environmentMenu(user: UserEvent, item: string): Promise<void> {
  await press(user, page().getByRole("button", { name: "More environment actions" }));
  await press(user, await screen.findByRole("menuitem", { name: item }));
}

/** A program a person installed to read credentials from their store. */
function locator(name: string): string {
  return journey.writeFile(`tools/${name}`, "#!/bin/sh\nexit 1\n", 0o700);
}

interface Registration {
  name: string;
  purpose?: "MLLP endpoint" | "Evidence source";
  address: string;
  command: string;
  arguments?: string[];
  maxAge?: string;
}

/** Fills in New credential, choosing the locator program in the host's file
 * dialog, and presses Save. Returns the sheet. */
async function register(user: UserEvent, reference: Registration) {
  await press(user, page().getByRole("button", { name: "New credential" }));
  const sheet = within(await screen.findByRole("dialog", { name: "New credential" }));
  await enter(user, sheet.getByRole("textbox", { name: "Name" }), reference.name);
  if (reference.purpose) await user.selectOptions(sheet.getByRole("combobox", { name: "Purpose" }), reference.purpose);
  await enter(user, sheet.getByRole("textbox", { name: "Allowed address" }), reference.address);
  await journey.chooseFiles([reference.command], "Choose the locator program");
  await act(user, sheet.getByRole("button", { name: "Choose locator program" }));
  await sheet.findByText(reference.command.split("/").pop()!);
  const args = reference.arguments ?? [];
  for (const [index, argument] of args.entries()) {
    if (index > 0) await press(user, sheet.getByRole("button", { name: "Add argument" }));
    await enter(user, sheet.getByRole("textbox", { name: `Argument ${index + 1}` }), argument);
  }
  if (reference.maxAge) await enter(user, sheet.getByRole("textbox", { name: "Maximum age" }), reference.maxAge);
  await act(user, sheet.getByRole("button", { name: "Save" }));
  return sheet;
}

/** The credential's row: name, purpose, store and rotation. */
function row(name: string): string[] {
  const table = page().getByRole("table", { name: "Credentials" });
  const found = table.querySelector<HTMLElement>(`[data-row-id="${CSS.escape(name)}"]`);
  if (!found) throw new Error(`no credential ${name}`);
  return Array.from(found.querySelectorAll("td,th")).map((cell) => cell.textContent ?? "").slice(0, 4);
}

/** Opens the credential's Edit sheet from its row's menu. */
async function editCredential(user: UserEvent, name: string) {
  await press(user, page().getByRole("button", { name: `More actions for ${name}` }));
  await press(user, await screen.findByRole("menuitem", { name: "Edit…" }));
  return within(await screen.findByRole("dialog", { name: "Edit credential" }));
}

async function closed(title: string): Promise<void> {
  await waitFor(() => expect(screen.queryByRole("dialog", { name: title })).toBeNull());
}

test("credential references are registered, refused, edited, cancelled and bound in the window and read unchanged by the command line", async () => {
  const user = userEvent.setup();
  await project(user);
  await addEnvironment(user, "127.0.0.1:2576", "TLS");
  await environmentMenu(user, "Credentials");
  expect(await page().findByText("No credentials")).toBeTruthy();
  const security = locator("security");

  // A reference is registered. The locator argument with a space in it is one
  // argument, and none of them is ever shown again.
  const locatorArguments = ["find-generic-password", "-w", "-s", "readmit lab"];
  await register(user, { name: "lab-mllp", address: "127.0.0.1:2575", command: security, arguments: locatorArguments, maxAge: "720h" });
  await closed("New credential");
  await waitFor(() => expect(row("lab-mllp")).toEqual(["lab-mllp", "MLLP endpoint", "OS keychain", expect.any(String)]));
  expect(document.body.textContent).not.toContain("readmit lab");
  const registered = journey.digest(`${PROJECT}/secrets.json`);

  const shown = await journey.commandLine(["secret", "show", "--secrets", `${PROJECT}/secrets.json`]);
  expect(shown.code).toBe(0);
  expect(shown.stdout).toContain("  lab-mllp store=os-keychain purpose=mllp-endpoint address=127.0.0.1:2575 generation=1\n");
  expect(shown.stdout).toContain(" max-age: 720h\n");
  expect(shown.stdout).toContain(`    command: ${security} (4 locator arguments)\n`);

  // A second registration under the same name is refused as the command line
  // refuses it, changes no byte, and keeps what was typed to be corrected.
  const duplicate = await register(user, { name: "lab-mllp", address: "127.0.0.1:2575", command: security });
  const nameRefusal = "that name is already registered in this store";
  expect((await duplicate.findByRole("alert")).textContent).toContain(nameRefusal);
  expect((duplicate.getByRole("textbox", { name: "Name" }) as HTMLInputElement).value).toBe("lab-mllp");
  const byHand = await commandLine([
    "secret", "add", "--secrets", `${PROJECT}/secrets.json`, "--name", "lab-mllp",
    "--store", "os-keychain", "--address", "127.0.0.1:2575", "--command", security,
  ]);
  expect(byHand.code).not.toBe(0);
  expect(byHand.stderr).toContain(nameRefusal);
  expect(journey.digest(`${PROJECT}/secrets.json`)).toBe(registered);
  await user.keyboard("{Escape}");
  await press(user, within(await screen.findByRole("dialog", { name: "Save changes?" })).getByRole("button", { name: "Discard" }));
  await closed("New credential");

  // The edit, from the keyboard alone: Enter on the row opens it; Escape with
  // nothing changed closes it and writes nothing.
  const credentialRow = page().getByRole("table", { name: "Credentials" }).querySelector<HTMLElement>('[data-row-id="lab-mllp"]')!;
  credentialRow.focus();
  await user.keyboard("{Enter}");
  let form = within(await screen.findByRole("dialog", { name: "Edit credential" }));
  expect((form.getByRole("textbox", { name: "Name" }) as HTMLInputElement).disabled).toBe(true);
  expect((form.getByRole("checkbox", { name: "Replace arguments (4 stored)" }) as HTMLInputElement).checked).toBe(false);
  await user.keyboard("{Escape}");
  await closed("Edit credential");
  expect(journey.digest(`${PROJECT}/secrets.json`)).toBe(registered);

  // An address the command line refuses is refused here with its words, at
  // its field, and the edit stays open with what was typed.
  form = await editCredential(user, "lab-mllp");
  await enter(user, form.getByRole("textbox", { name: "Allowed address" }), "127.0.0.1");
  await act(user, form.getByRole("button", { name: "Save" }));
  const addressRefusal = "reference address: must be an explicit host and numeric port";
  expect((await form.findByRole("alert")).textContent).toContain(addressRefusal);
  expect(document.activeElement).toBe(form.getByRole("textbox", { name: "Allowed address" }));
  const refusedUpdate = await commandLine(["secret", "update", "--secrets", `${PROJECT}/secrets.json`, "--name", "lab-mllp", "--address", "127.0.0.1"]);
  expect(refusedUpdate.code).not.toBe(0);
  expect(refusedUpdate.stderr).toContain(addressRefusal);
  expect(journey.digest(`${PROJECT}/secrets.json`)).toBe(registered);

  // Every member an edit may change, changed, and saved.
  const vault = locator("vault");
  await enter(user, form.getByRole("textbox", { name: "Allowed address" }), "127.0.0.1:2576");
  await journey.chooseFiles([vault], "Choose the locator program");
  await act(user, form.getByRole("button", { name: "Choose locator program" }));
  await form.findByText("vault");
  await enter(user, form.getByRole("textbox", { name: "Maximum age" }), "2160h");
  await user.click(form.getByRole("checkbox", { name: "Replace arguments (4 stored)" }));
  for (const [index, argument] of ["kv", "get", "-field=password", "lab/mllp"].entries()) {
    if (index > 0) await press(user, form.getByRole("button", { name: "Add argument" }));
    await enter(user, form.getByRole("textbox", { name: `Argument ${index + 1}` }), argument);
  }
  await user.selectOptions(form.getByRole("combobox", { name: "Store" }), "customer-managed");
  await act(user, form.getByRole("button", { name: "Save" }));
  await closed("Edit credential");
  await waitFor(() => expect(row("lab-mllp")[2]).toBe("Customer-managed vault"));
  expect(journey.digest(`${PROJECT}/secrets.json`)).not.toBe(registered);
  expect(document.body.textContent).not.toContain("lab/mllp");

  // The command line reads the edit unchanged: the configuration changed and
  // the recorded rotation did not, because re-pointing is not a rotation.
  const reread = await journey.commandLine(["secret", "show", "--secrets", `${PROJECT}/secrets.json`]);
  expect(reread.stdout).toContain("  lab-mllp store=customer-managed purpose=mllp-endpoint address=127.0.0.1:2576 generation=1\n");
  expect(reread.stdout).toContain(" max-age: 2160h\n");
  expect(reread.stdout).toContain(`    command: ${vault} (4 locator arguments)\n`);
  const rotatedAt = /rotated: (\S+) /;
  expect(reread.stdout.match(rotatedAt)?.[1]).toBe(shown.stdout.match(rotatedAt)?.[1]);

  // A reference registered for evidence sources is never offered to an MLLP
  // connection, which the command line refuses as a different purpose; the
  // reference registered for that endpoint is bound instead.
  await register(user, { name: "lab-source", purpose: "Evidence source", address: "127.0.0.1:2576", command: security });
  await closed("New credential");
  await press(user, page().getAllByRole("button", { name: /^Back to / })[0]!);
  await page().findByRole("heading", { level: 1, name: ENVIRONMENT });
  await press(user, within(page().getByRole("region", { name: "Connection" })).getByRole("button", { name: "Edit" }));
  const connection = within(await screen.findByRole("dialog", { name: "Edit connection" }));
  const credential = connection.getByRole("combobox", { name: "Credential" });
  await within(credential).findByRole("option", { name: "lab-mllp" });
  expect(within(credential).getAllByRole("option").map((option) => option.textContent)).toEqual(["None", "lab-mllp"]);
  const purposeRefusal = "the credential reference declares a different purpose than this use";
  const boundByHand = await commandLine([
    "target", "set", "--target", `${PROJECT}/cli-target.json`, "--name", "lab-siu", "--classification", "nonproduction",
    "--address", "127.0.0.1:2576", "--secrets", "secrets.json", "--credential", "lab-source",
  ]);
  expect(boundByHand.code).not.toBe(0);
  expect(boundByHand.stderr).toContain(purposeRefusal);
  await user.selectOptions(credential, "lab-mllp");
  await act(user, connection.getByRole("button", { name: "Save" }));
  await closed("Edit connection");
  await waitFor(() => expect(within(page().getByRole("region", { name: "Connection" })).getByText("lab-mllp")).toBeTruthy());
  const target = member(ENVIRONMENT, "target");
  expect(target.sha256).toBe(journey.digest(target.path));
  expect(JSON.parse(journey.readFile(target.path)).credential).toEqual({ secrets_file: "secrets.json", reference: "lab-mllp" });
  const targetShown = await journey.commandLine(["target", "show", "--target", target.path]);
  expect(targetShown.code).toBe(0);
  expect(targetShown.stdout).toContain("127.0.0.1:2576");

  // A reference the command line wrote is listed in the window, edited there,
  // and read back through the command line with the window's change.
  const added = await commandLine([
    "secret", "add", "--secrets", `${PROJECT}/secrets.json`, "--name", "lab-cli", "--store", "os-keychain",
    "--address", "127.0.0.1:2577", "--command", security, "--argument", "find-generic-password", "--argument", "-w",
  ]);
  expect(added.code).toBe(0);
  await environmentMenu(user, "Credentials");
  await waitFor(() => expect(row("lab-cli")).toEqual(["lab-cli", "MLLP endpoint", "OS keychain", "Not declared"]));
  const cliForm = await editCredential(user, "lab-cli");
  await enter(user, cliForm.getByRole("textbox", { name: "Maximum age" }), "720h");
  // While the edit is open, the command line re-points the same reference.
  // The window writes only what the person changed, so that change survives.
  const meanwhile = await commandLine(["secret", "update", "--secrets", `${PROJECT}/secrets.json`, "--name", "lab-cli", "--address", "127.0.0.1:2578"]);
  expect(meanwhile.code).toBe(0);
  await act(user, cliForm.getByRole("button", { name: "Save" }));
  await closed("Edit credential");
  const cliShown = await journey.commandLine(["secret", "show", "--secrets", `${PROJECT}/secrets.json`]);
  expect(cliShown.stdout).toContain("  lab-cli store=os-keychain purpose=mllp-endpoint address=127.0.0.1:2578 generation=1\n");
  expect(cliShown.stdout).toContain(`    command: ${security} (2 locator arguments)\n`);
  expect(cliShown.stdout.slice(cliShown.stdout.indexOf("  lab-cli "))).toContain(" max-age: 720h\n");
});

test("a send policy and a reset plan are saved under the identity of their bytes, refused when invalid, and read unchanged by target check and the reset reader", async () => {
  const user = userEvent.setup();
  const downstream = await journey.startDownstream("downstream/appointments.csv", "fixed");
  await project(user);
  await addEnvironment(user, downstream.address, "TCP/MLLP");
  const saves = () => revisions(ENVIRONMENT).length;

  // A range that is not in canonical masked form is refused at its field and
  // nothing is saved; the draft stays to be corrected.
  await environmentMenu(user, "Allowed destinations");
  await press(user, within(await screen.findByRole("dialog", { name: "Allowed destinations" })).getByRole("button", { name: "Edit" }));
  const ranges = within(await screen.findByRole("dialog", { name: "Edit allowed destinations" }));
  await enter(user, ranges.getByRole("textbox", { name: "Name of range 1" }), "Loopback");
  await enter(user, ranges.getByRole("textbox", { name: "Range 1" }), "127.0.0.5/8");
  const before = saves();
  await act(user, ranges.getByRole("button", { name: "Save" }));
  expect((await ranges.findByRole("alert")).textContent).toContain("canonical masked form");
  expect(saves()).toBe(before);
  expect((ranges.getByRole("textbox", { name: "Range 1" }) as HTMLInputElement).value).toBe("127.0.0.5/8");
  await enter(user, ranges.getByRole("textbox", { name: "Range 1" }), "127.0.0.0/8");
  await act(user, ranges.getByRole("button", { name: "Save" }));
  await closed("Edit allowed destinations");
  await waitFor(() => expect(saves()).toBe(before + 1));
  const policy = member(ENVIRONMENT, "policy");
  expect(policy.sha256).toBe(journey.digest(policy.path));
  expect(JSON.parse(journey.readFile(policy.path))).toEqual({ schema: "readmit-send-policy/v1", approved_destinations: ["127.0.0.0/8"] });

  // target check reads the policy the window wrote, as the window's own
  // destination check decides it: the downstream is inside the range, an
  // address outside it is refused, and a check is not a send.
  const target = member(ENVIRONMENT, "target");
  const checked = await commandLine(["target", "check", "--target", target.path, "--policy", policy.path]);
  expect(checked.stderr).toBe("");
  expect(checked.stdout).toContain("Approved destinations: 127.0.0.0/8\n");
  expect(checked.stdout).toContain("Send policy: denied (send_not_explicit)\n");
  await environmentMenu(user, "Check destination…");
  const destination = within(await screen.findByRole("dialog", { name: "Check destination" }));
  await enter(user, destination.getByRole("textbox", { name: "Address" }), downstream.address);
  await act(user, destination.getByRole("button", { name: "Check" }));
  expect((await destination.findByRole("status")).textContent).toMatch(/^Allowed/);
  await enter(user, destination.getByRole("textbox", { name: "Address" }), "10.1.2.3:2575");
  await act(user, destination.getByRole("button", { name: "Check" }));
  expect((await destination.findByRole("status")).textContent).toBe("Refused · Outside every allowed range");
  await press(user, destination.getByRole("button", { name: "Cancel" }));
  expect(downstream.received()).toHaveLength(0);
  expect(downstream.connected()).toBe(0);

  // A reset: an action that checks an observation names one, or is refused
  // before it is added; the reset saved without it is the plan the reset
  // reader runs, by identity.
  await press(user, page().getByRole("tab", { name: "Reset" }));
  await press(user, within(page().getByRole("region", { name: "Reset" })).getByRole("button", { name: "Add reset" }));
  let sheet = within(await screen.findByRole("dialog", { name: "Edit reset" }));
  await enter(user, sheet.getByRole("textbox", { name: "Name" }), "Fresh listener");
  await press(user, sheet.getByRole("button", { name: "Add action" }));
  let action = within(await screen.findByRole("dialog", { name: "Add action" }));
  await enter(user, action.getByRole("textbox", { name: "Name" }), "Stop listener");
  await user.selectOptions(action.getByRole("combobox", { name: "Type" }), "Manual confirmation");
  await enter(user, action.getByRole("textbox", { name: "Instructions" }), "Stop the prior listen session and wait for it to exit.");
  await press(user, action.getByRole("button", { name: "Done" }));
  sheet = within(await screen.findByRole("dialog", { name: "Edit reset" }));
  await press(user, sheet.getByRole("button", { name: "Add action" }));
  action = within(await screen.findByRole("dialog", { name: "Add action" }));
  await enter(user, action.getByRole("textbox", { name: "Name" }), "Empty ledger");
  await user.selectOptions(action.getByRole("combobox", { name: "Type" }), "Check empty observation");
  await press(user, action.getByRole("button", { name: "Done" }));
  expect((await action.findByRole("alert")).textContent).toBe("Choose the observation to check.");
  await user.keyboard("{Escape}");
  await press(user, within(await screen.findByRole("dialog", { name: "Save changes?" })).getByRole("button", { name: "Discard" }));
  sheet = within(await screen.findByRole("dialog", { name: "Edit reset" }));
  const planned = Array.from(sheet.getByRole("table", { name: "Reset actions" }).querySelectorAll("tbody tr")).map((entry) => entry.querySelector("th")?.textContent);
  expect(planned).toEqual(["Stop listener"]);
  await act(user, sheet.getByRole("button", { name: "Save" }));
  await closed("Edit reset");
  const plan = member(ENVIRONMENT, "reset");
  expect(plan.sha256).toBe(journey.digest(plan.path));
  const actions = (JSON.parse(journey.readFile(plan.path)) as { actions: { id: string; operator: string }[] }).actions;
  expect(actions).toMatchObject([{ operator: "operator_confirms" }]);

  const reset = await commandLine([
    "target", "reset", "--target", member(ENVIRONMENT, "target").path, "--plan", plan.path,
    "--outcome", `${PROJECT}/reset-1.json`, "--confirm", actions[0]!.id,
  ]);
  expect(reset.stderr).toBe("");
  expect(reset.code).toBe(0);
  expect(reset.stdout).toContain(`Reset plan: ${plan.sha256} (readmit-reset-plan/v1)\n`);
  const outcome = JSON.parse(journey.readFile(`${PROJECT}/reset-1.json`)) as { plan_sha256: string; outcome: string };
  expect(outcome).toMatchObject({ plan_sha256: plan.sha256, outcome: "confirmed" });

  // The window's own reset of the same plan needs the manual step confirmed,
  // and reports it done.
  await press(user, await within(page().getByRole("region", { name: "Reset" })).findByRole("button", { name: "Reset" }));
  const review = within(await screen.findByRole("dialog", { name: "Reset" }));
  const final = (await review.findByRole("button", { name: "Reset" })) as HTMLButtonElement;
  await review.findByText("Stop the prior listen session and wait for it to exit.");
  expect(final.disabled).toBe(true);
  await user.click(review.getByRole("checkbox", { name: "Done" }));
  await act(user, final);
  expect((await screen.findByRole("list", { name: "Reset results" })).textContent).toBe("Stop listener · Done");
  expect(downstream.received()).toHaveLength(0);
});
