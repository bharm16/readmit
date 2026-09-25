// The environment documents a person keeps beside a project, authored in the
// window over the real facade: credential references registered, refused,
// edited, cancelled and bound to a target, an approved send policy and a
// fixture reset plan. Each save names the document it replaced by the SHA-256
// of the bytes it wrote, and the journey states that identity from the file on
// disk rather than from what the window answered. The command line reads every
// document the window wrote unchanged, and the window opens and edits what the
// command line wrote.
//
// No credential value exists anywhere in this journey. A reference names the
// program that would read a credential and the locator arguments that select
// it; nothing here resolves one, and no locator argument the person typed is
// ever rendered back to them.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { byContent, enter, Journey, press, whenEnabled } from "../testkit/journey";
import { licensedProject } from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

const PROJECT = "investigations/interface";
const LOCATOR = "/usr/bin/security";

/** The environment panel of the open project. */
function environment() {
  const heading = screen.getByRole("heading", { name: "Environments" });
  return within(heading.closest("section") as HTMLElement);
}

/** The command line over the journey's root, admitted by the activation the
 * vendor delivered, as a person working beside the window runs it. */
function commandLine(args: string[]) {
  return journey.commandLine(["--operation-policy", journey.path("vendor-delivered-license", "operation-policy.json"), ...args]);
}

/** The identity the window names the document a save just wrote by. */
async function writtenIdentity(file: string): Promise<string> {
  const escaped = file.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const line = await environment().findByText(byContent(new RegExp(`^Written to ${escaped} · identity [0-9a-f]{64}$`)));
  return (line.textContent ?? "").replace(/^.* · identity /, "");
}

/** Waits for the panel to say exactly this. */
async function says(text: string): Promise<void> {
  expect(await environment().findByText(text)).toBeTruthy();
}

/** Moves focus with Tab until the control has it, as a keyboard user does,
 * with Shift+Tab when the control comes before the focus. */
async function tabTo(user: UserEvent, control: HTMLElement): Promise<void> {
  await whenEnabled(control);
  for (let step = 0; step < 400; step++) {
    const active = document.activeElement;
    if (active === control) return;
    const after = active !== null && active !== document.body && (control.compareDocumentPosition(active) & Node.DOCUMENT_POSITION_FOLLOWING) !== 0;
    await user.tab({ shift: after });
  }
  throw new Error(`${control.getAttribute("aria-label") ?? control.textContent} is not reachable with Tab`);
}

interface Registration {
  name: string;
  purpose?: "mllp-endpoint" | "source-endpoint";
  address: string;
  command: string;
  arguments?: string[];
  maxAge?: string;
}

/** Fills in the registration form and presses Register. */
async function register(user: UserEvent, reference: Registration): Promise<void> {
  const panel = environment();
  await enter(user, panel.getByLabelText("Reference Name"), reference.name);
  await user.selectOptions(panel.getByLabelText("Purpose"), reference.purpose ?? "mllp-endpoint");
  await enter(user, panel.getByLabelText("Target Address Constraint"), reference.address);
  await enter(user, panel.getByLabelText("Locator Command (Path)"), reference.command);
  await enter(user, panel.getByLabelText("Locator Arguments (one per line)"), (reference.arguments ?? []).join("{Enter}"));
  await enter(user, panel.getByLabelText("Maximum Rotation Age"), reference.maxAge ?? "");
  await press(user, panel.getByRole("button", { name: "Add credential reference" }));
}

/** The registered reference's row in the table. */
function row(name: string) {
  const table = environment().getByRole("table", { name: "Registered credential references" });
  return within(within(table).getByText(name, { selector: "strong" }).closest("tr") as HTMLElement);
}

test("credential references are registered, refused, edited, cancelled and bound in the window and read unchanged by the command line", async () => {
  const user = userEvent.setup();
  await licensedProject(journey, user);
  const panel = environment();
  await press(user, panel.getByRole("button", { name: "Credential References" }));
  expect(await panel.findByText("No credential references registered yet.")).toBeTruthy();

  // A reference is registered. The locator argument with a space in it is one
  // argument, and none of them is ever shown again: the row counts them.
  const locator = ["find-generic-password", "-w", "-s", "readmit lab"];
  await register(user, { name: "lab-mllp", address: "127.0.0.1:2575", command: LOCATOR, arguments: locator, maxAge: "720h" });
  await says("Secret reference registered successfully.");
  const registered = await writtenIdentity("secrets.json");
  expect(registered).toBe(journey.digest(`${PROJECT}/secrets.json`));
  expect(row("lab-mllp").getByText("127.0.0.1:2575")).toBeTruthy();
  expect(row("lab-mllp").getByText(byContent(/^\/usr\/bin\/security \(4 locator arguments\)$/))).toBeTruthy();
  expect(document.body.textContent).not.toContain("readmit lab");

  const shown = await journey.commandLine(["secret", "show", "--secrets", `${PROJECT}/secrets.json`]);
  expect(shown.code).toBe(0);
  expect(shown.stdout).toContain("  lab-mllp store=os-keychain purpose=mllp-endpoint address=127.0.0.1:2575 generation=1\n");
  expect(shown.stdout).toContain(" max-age: 720h\n");
  expect(shown.stdout).toContain(`    command: ${LOCATOR} (4 locator arguments)\n`);

  // A second registration under the same name, and one naming its program
  // through PATH, are refused as the command line refuses them, and neither
  // changes a byte. What was typed stays to be corrected.
  await register(user, { name: "lab-mllp", address: "127.0.0.1:2575", command: LOCATOR });
  await says("that name is already registered in this store");
  await register(user, { name: "lab-path", address: "127.0.0.1:2575", command: "security" });
  const pathRefusal = "reference command: must be an absolute path, never a name resolved through PATH";
  await says(pathRefusal);
  expect((panel.getByLabelText("Reference Name") as HTMLInputElement).value).toBe("lab-path");
  expect(panel.queryByText(/^Written to /)).toBeNull();
  const byHand = await commandLine([
    "secret", "add", "--secrets", `${PROJECT}/secrets.json`, "--name", "lab-path",
    "--store", "os-keychain", "--address", "127.0.0.1:2575", "--command", "security",
  ]);
  expect(byHand.code).not.toBe(0);
  expect(byHand.stderr).toContain(pathRefusal);
  expect(journey.digest(`${PROJECT}/secrets.json`)).toBe(registered);

  // The edit, from the keyboard alone: Tab reaches it and Enter opens it with
  // focus inside; Escape cancels it, writes nothing and returns focus.
  const edit = row("lab-mllp").getByRole("button", { name: "Edit lab-mllp" });
  await tabTo(user, edit);
  await user.keyboard("{Enter}");
  const form = within(await panel.findByRole("form", { name: "Edit credential reference lab-mllp" }));
  expect(document.activeElement).toBe(form.getByLabelText("Store"));
  expect(form.getByText(/^Purpose: mllp-endpoint\./)).toBeTruthy();
  await user.keyboard("{Escape}");
  await says("Edit of lab-mllp cancelled; nothing was written.");
  expect(panel.queryByRole("form", { name: "Edit credential reference lab-mllp" })).toBeNull();
  await waitFor(() => expect(document.activeElement).toBe(edit));
  expect(journey.digest(`${PROJECT}/secrets.json`)).toBe(registered);

  // An address the command line refuses is refused here with its words, and
  // the edit stays open with what was typed.
  await user.keyboard("{Enter}");
  const editing = within(await panel.findByRole("form", { name: "Edit credential reference lab-mllp" }));
  await user.tab();
  expect(document.activeElement).toBe(editing.getByLabelText("Target Address Constraint"));
  await user.keyboard("{Control>}a{/Control}127.0.0.1{Enter}");
  const addressRefusal = "reference address: must be an explicit host and numeric port";
  await says(addressRefusal);
  expect((editing.getByLabelText("Target Address Constraint") as HTMLInputElement).value).toBe("127.0.0.1");
  const refusedUpdate = await commandLine(["secret", "update", "--secrets", `${PROJECT}/secrets.json`, "--name", "lab-mllp", "--address", "127.0.0.1"]);
  expect(refusedUpdate.code).not.toBe(0);
  expect(refusedUpdate.stderr).toContain(addressRefusal);
  expect(journey.digest(`${PROJECT}/secrets.json`)).toBe(registered);

  // Every member an edit may change, changed from the keyboard, and saved.
  await user.keyboard("{Control>}a{/Control}127.0.0.1:2576");
  await user.tab();
  await user.keyboard("{Control>}a{/Control}/opt/vault/bin/vault");
  await user.tab();
  await user.keyboard("{Control>}a{/Control}2160h");
  await user.tab();
  expect(document.activeElement).toBe(editing.getByLabelText("Replace the 4 registered locator arguments"));
  await user.keyboard(" ");
  await user.tab();
  await user.keyboard("kv{Enter}get{Enter}-field=password{Enter}lab/mllp");
  await user.selectOptions(editing.getByLabelText("Store"), "customer-managed");
  await tabTo(user, editing.getByRole("button", { name: "Save changes" }));
  await user.keyboard("{Enter}");
  await says("Credential reference lab-mllp updated.");
  const edited = await writtenIdentity("secrets.json");
  expect(edited).toBe(journey.digest(`${PROJECT}/secrets.json`));
  expect(edited).not.toBe(registered);
  await waitFor(() => expect(document.activeElement).toBe(row("lab-mllp").getByRole("button", { name: "Edit lab-mllp" })));
  expect(document.body.textContent).not.toContain("lab/mllp");

  // The command line reads the edit unchanged: the configuration changed and
  // the recorded rotation did not, because re-pointing is not a rotation.
  const reread = await journey.commandLine(["secret", "show", "--secrets", `${PROJECT}/secrets.json`]);
  expect(reread.stdout).toContain("  lab-mllp store=customer-managed purpose=mllp-endpoint address=127.0.0.1:2576 generation=1\n");
  expect(reread.stdout).toContain(" max-age: 2160h\n");
  expect(reread.stdout).toContain("    command: /opt/vault/bin/vault (4 locator arguments)\n");
  const rotatedAt = /rotated: (\S+) /;
  expect(reread.stdout.match(rotatedAt)?.[1]).toBe(shown.stdout.match(rotatedAt)?.[1]);

  // A reference registered for evidence sources is refused when an MLLP
  // target names it, as the command line refuses it, and the reference
  // registered for that endpoint is bound instead.
  await register(user, { name: "lab-source", purpose: "source-endpoint", address: "127.0.0.1:2576", command: LOCATOR });
  await says("Secret reference registered successfully.");
  await press(user, panel.getByRole("button", { name: "Target" }));
  await enter(user, panel.getByLabelText("Target Config File"), "lab-target.json");
  await enter(user, panel.getByLabelText("Environment Name"), "lab-siu");
  await user.selectOptions(panel.getByLabelText("Classification"), "nonproduction");
  await enter(user, panel.getByLabelText("Destination Address"), "127.0.0.1:2576");
  await user.selectOptions(panel.getByLabelText("Credential Reference"), "lab-source");
  await press(user, panel.getByRole("button", { name: "Save target" }));
  const purposeRefusal = "the credential reference declares a different purpose than this use";
  await says(purposeRefusal);
  const boundByHand = await commandLine([
    "target", "set", "--target", `${PROJECT}/cli-target.json`, "--name", "lab-siu", "--classification", "nonproduction",
    "--address", "127.0.0.1:2576", "--secrets", "secrets.json", "--credential", "lab-source",
  ]);
  expect(boundByHand.code).not.toBe(0);
  expect(boundByHand.stderr).toContain(purposeRefusal);
  await user.selectOptions(panel.getByLabelText("Credential Reference"), "lab-mllp");
  await press(user, panel.getByRole("button", { name: "Save target" }));
  await says("Target configuration saved successfully.");
  const target = await journey.commandLine(["target", "show", "--target", `${PROJECT}/lab-target.json`]);
  expect(target.code).toBe(0);
  expect(target.stdout).toContain("Environment: lab-siu\n");

  // A document the command line wrote opens in the window, is edited there,
  // and reads back through the command line with the window's change.
  const added = await commandLine([
    "secret", "add", "--secrets", `${PROJECT}/cli-secrets.json`, "--name", "lab-cli", "--store", "os-keychain",
    "--address", "127.0.0.1:2577", "--command", LOCATOR, "--argument", "find-generic-password", "--argument", "-w",
  ]);
  expect(added.code).toBe(0);
  await press(user, panel.getByRole("button", { name: "Credential References" }));
  await enter(user, panel.getByLabelText("Secrets Document File"), "cli-secrets.json");
  expect(await panel.findByText("lab-cli", { selector: "strong" })).toBeTruthy();
  expect(row("lab-cli").getByText("127.0.0.1:2577")).toBeTruthy();
  expect(row("lab-cli").getByText(byContent(/^\/usr\/bin\/security \(2 locator arguments\)$/))).toBeTruthy();
  expect(row("lab-cli").getByText("Rotation age not declared")).toBeTruthy();
  await press(user, row("lab-cli").getByRole("button", { name: "Edit lab-cli" }));
  const cliForm = within(await panel.findByRole("form", { name: "Edit credential reference lab-cli" }));
  await enter(user, cliForm.getByLabelText("Maximum Rotation Age"), "720h");
  // While the edit is open, the command line re-points the same reference.
  // The window writes only what the person changed, so that change survives.
  const meanwhile = await commandLine(["secret", "update", "--secrets", `${PROJECT}/cli-secrets.json`, "--name", "lab-cli", "--address", "127.0.0.1:2578"]);
  expect(meanwhile.code).toBe(0);
  await press(user, cliForm.getByRole("button", { name: "Save changes" }));
  await says("Credential reference lab-cli updated.");
  expect(await writtenIdentity("cli-secrets.json")).toBe(journey.digest(`${PROJECT}/cli-secrets.json`));
  expect(row("lab-cli").getByText("127.0.0.1:2578")).toBeTruthy();
  const cliShown = await journey.commandLine(["secret", "show", "--secrets", `${PROJECT}/cli-secrets.json`]);
  expect(cliShown.stdout).toContain("  lab-cli store=os-keychain purpose=mllp-endpoint address=127.0.0.1:2578 generation=1\n");
  expect(cliShown.stdout).toContain(" max-age: 720h\n");
  expect(cliShown.stdout).toContain(`    command: ${LOCATOR} (2 locator arguments)\n`);
});

test("a send policy and a reset plan are saved under the identity of their bytes, refused when invalid, and read unchanged by target check and the reset reader", async () => {
  const user = userEvent.setup();
  const downstream = await journey.startDownstream("downstream/appointments.csv", "fixed");
  await licensedProject(journey, user);
  const panel = environment();
  await enter(user, panel.getByLabelText("Target Config File"), "lab-target.json");
  await enter(user, panel.getByLabelText("Environment Name"), "lab-siu");
  await user.selectOptions(panel.getByLabelText("Classification"), "nonproduction");
  await enter(user, panel.getByLabelText("Destination Address"), downstream.address);
  await enter(user, panel.getByLabelText("Message timeout"), "500ms");
  await press(user, panel.getByRole("button", { name: "Save target" }));
  await says("Target configuration saved successfully.");

  // A prefix that is not in canonical masked form is refused on save and
  // nothing is written; the draft stays to be corrected. Enter adds a prefix.
  await press(user, panel.getByRole("button", { name: "Send policy" }));
  await press(user, await panel.findByRole("button", { name: "New send policy" }));
  await enter(user, panel.getByLabelText("Approved destination prefix"), "127.0.0.5/8{Enter}");
  expect(await panel.findByText("127.0.0.5/8", { selector: "code" })).toBeTruthy();
  await press(user, panel.getByRole("button", { name: "Save policy" }));
  await says("every approved destination is one CIDR prefix in canonical masked form, such as 127.0.0.0/8 or 10.1.0.0/16");
  expect(panel.queryByText(/^Written to /)).toBeNull();
  expect(() => journey.readFile(`${PROJECT}/send-policy.json`)).toThrow();
  await enter(user, panel.getByLabelText("Approved destination prefix"), "127.0.0.0/8{Enter}");
  const refusedPrefix = within(panel.getByText("127.0.0.5/8", { selector: "code" }).closest("li") as HTMLElement);
  await press(user, refusedPrefix.getByRole("button", { name: "Remove" }));
  await press(user, panel.getByRole("button", { name: "Save policy" }));
  await says("Approved-destination policy saved.");
  const policy = await writtenIdentity("send-policy.json");
  expect(policy).toBe(journey.digest(`${PROJECT}/send-policy.json`));

  // target check reads the policy the window wrote, and decides what the
  // window's own check decides: the destination is approved, and a check is
  // not a send.
  const checked = await commandLine(["target", "check", "--target", `${PROJECT}/lab-target.json`, "--policy", `${PROJECT}/send-policy.json`]);
  expect(checked.stderr).toBe("");
  expect(checked.stdout).toContain("Approved destinations: 127.0.0.0/8\n");
  expect(checked.stdout).toContain("Send policy: denied (send_not_explicit)\n");
  await press(user, panel.getByRole("button", { name: "Target" }));
  await press(user, panel.getByRole("button", { name: "Test connection" }));
  const decision = await panel.findByText(byContent(/^Send Policy Decision: /));
  expect(decision.textContent).toBe("Send Policy Decision: Refused (Reason: send_not_explicit)");
  expect(downstream.received()).toHaveLength(0);

  // A reset plan: an observation action without its file is refused, and the
  // plan saved without it is the plan the reset reader runs, by identity.
  await press(user, panel.getByRole("button", { name: "Reset plan" }));
  await press(user, await panel.findByRole("button", { name: "New reset plan" }));
  await enter(user, panel.getByLabelText("Environment Name Match"), "lab-siu");
  await enter(user, panel.getByLabelText("Action ID"), "stop-listener");
  await enter(user, panel.getByLabelText("Reset instructions"), "Stop the prior listen session and wait for it to exit.");
  await press(user, panel.getByRole("button", { name: "Add action" }));
  await enter(user, panel.getByLabelText("Action ID"), "empty-ledger");
  await user.selectOptions(panel.getByLabelText("Reviewed Operator"), "observation_empty");
  await enter(user, panel.getByLabelText("Reset instructions"), "The fresh listener exports an empty ledger.");
  await press(user, panel.getByRole("button", { name: "Add action" }));
  await press(user, panel.getByRole("button", { name: "Save plan" }));
  await says("an observation_empty action names one receiver observation file inside the plan's own directory");
  expect(panel.queryByText(/^Written to /)).toBeNull();
  const ledgerAction = within(panel.getByText("empty-ledger").closest(".action-item") as HTMLElement);
  await press(user, ledgerAction.getByRole("button", { name: "Remove" }));
  await press(user, panel.getByRole("button", { name: "Save plan" }));
  await says("Fixture reset plan saved.");
  const plan = await writtenIdentity("reset-plan.json");
  expect(plan).toBe(journey.digest(`${PROJECT}/reset-plan.json`));

  const reset = await commandLine([
    "target", "reset", "--target", `${PROJECT}/lab-target.json`, "--plan", `${PROJECT}/reset-plan.json`,
    "--outcome", `${PROJECT}/reset-1.json`, "--confirm", "stop-listener",
  ]);
  expect(reset.stderr).toBe("");
  expect(reset.code).toBe(0);
  expect(reset.stdout).toContain(`Reset plan: ${plan} (readmit-reset-plan/v1)\n`);
  const outcome = JSON.parse(journey.readFile(`${PROJECT}/reset-1.json`)) as { plan_sha256: string; outcome: string };
  expect(outcome).toMatchObject({ plan_sha256: plan, outcome: "confirmed" });
});

test.each([false, true])("a credential bound from the default target file resolves the displayed secrets document in target check (symlinked: %s)", async (symlinked) => {
  const user = userEvent.setup();
  const downstream = await journey.startDownstream("downstream/default-target-ledger.csv", "fixed");
  await licensedProject(journey, user);
  if (symlinked) {
    journey.makeFolder(`${PROJECT}/nested/targets`);
    journey.writeFile(`${PROJECT}/nested/secrets.json`, "{}");
    journey.makeLink(`${PROJECT}/targets`, journey.path(PROJECT, "nested/targets"));
    journey.makeLink(`${PROJECT}/alias`, journey.path(PROJECT, "nested/targets"));
  }
  const panel = environment();
  await press(user, panel.getByRole("button", { name: "Credential References" }));
  if (symlinked) {
    // The facade cleans a workspace-relative name before opening it. Keeping
    // alias/.. in an absolute binding would instead reach nested/secrets.json.
    await enter(user, panel.getByLabelText("Secrets Document File"), "alias/../secrets.json");
  }
  await register(user, { name: "default-target-key", address: downstream.address, command: LOCATOR });
  await says("Secret reference registered successfully.");

  await press(user, panel.getByRole("button", { name: "Target" }));
  expect((panel.getByLabelText("Target Config File") as HTMLInputElement).value).toBe("targets/default.json");
  await enter(user, panel.getByLabelText("Environment Name"), "default-lab");
  await user.selectOptions(panel.getByLabelText("Classification"), "nonproduction");
  await enter(user, panel.getByLabelText("Destination Address"), downstream.address);
  await user.selectOptions(panel.getByLabelText("Credential Reference"), "default-target-key");
  const secretsPath = journey.path(PROJECT, "secrets.json");
  expect(panel.getByText(secretsPath, { selector: "code" })).toBeTruthy();
  await press(user, panel.getByRole("button", { name: "Save target" }));
  await says("Target configuration saved successfully.");

  const saved = JSON.parse(journey.readFile(`${PROJECT}/targets/default.json`)) as { credential: { secrets_file: string; reference: string } };
  expect(saved.credential).toEqual({ secrets_file: secretsPath, reference: "default-target-key" });
  const checked = await commandLine(["target", "check", "--target", `${PROJECT}/targets/default.json`]);
  expect(checked.code).toBe(0);
  expect(checked.stderr).toBe("");
  expect(checked.stdout).toContain("Environment: default-lab\n");
  expect(checked.stdout).toContain("Diagnosis: reachable (phase=confirm)\n");
  expect(downstream.received()).toHaveLength(0);
});
