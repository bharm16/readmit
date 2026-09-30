// What an investigation hands to a vendor: the value-free support summary of
// a report. From the report of a failed run, Support summary is refused until
// the project has a sharing policy; the policy is its own sheet; the preview
// is the actual summary the share operation prepares; and Export support
// summary writes it once into a new folder the person names; a place
// something is already at is refused in the review. The command line verifies the bundle the window
// wrote, finds no value of the evidence in it, and refuses it once changed.
//
// Every message and value here is synthetic.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { enter, Journey, press } from "../testkit/journey";
import { goTo, page } from "../testkit/navigation";
import {
  activateLicense,
  configureEnvironment,
  createProject,
  createRecordTest,
  EXPORTED_BOOKING,
  EXPORTED_RESCHEDULE,
  importExport,
  openedCase,
  pressServed,
  reviewRun,
  sendReviewed,
} from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

/** What `readmit share verify` answers for a bundle it refuses. */
const REFUSED = "readmit: sharing refused; source, policy, destination or exact approval unavailable\n";

test("a report's value-free support summary is exported under the project's sharing policy into a new folder, and the command line verifies it and refuses it once changed", async () => {
  const user = userEvent.setup();
  journey.writeFile("exports/scheduling-feed.hl7", EXPORTED_BOOKING + EXPORTED_RESCHEDULE);
  await journey.launch();
  await activateLicense(user, journey);
  const project = await createProject(user, journey, "investigations", "scheduling", "Scheduling interface");
  const ledger = `${project.slice(journey.path().length + 1)}/appointments.json`;
  const downstream = await journey.startDownstream("downstream/appointments.csv", "duplicating", ledger);
  await importExport(user, journey, "exports/scheduling-feed.hl7", "Reschedule duplicates");
  const environment = await configureEnvironment(user, journey, downstream.address, ledger);
  await goTo(user, "Cases");
  await press(user, await page().findByText("Reschedule duplicates"));
  await openedCase();
  await createRecordTest(user, journey, environment, "Reschedule keeps one appointment");
  await sendReviewed(user, journey, await reviewRun(user, journey, /^Reschedule keeps one appointment/));
  const sent = downstream.received().length;

  // The report of the failed run.
  await press(user, page().getByRole("button", { name: "Create report" }));
  const creating = within(await screen.findByRole("dialog", { name: "New report" }));
  await waitFor(() => expect((creating.getByLabelText("Name") as HTMLInputElement).value).not.toBe(""), { timeout: 10_000 });
  const report = (creating.getByLabelText("Name") as HTMLInputElement).value;
  await journey.settled();
  await pressServed(user, journey, creating.getByRole("button", { name: "Create" }), "SaveItem");
  await page().findByRole("heading", { level: 1, name: report }, { timeout: 30_000 });

  // No summary is prepared until the project has a sharing policy, which is
  // its own sheet.
  await press(user, await page().findByRole("button", { name: "More report actions" }));
  await press(user, await screen.findByRole("menuitem", { name: "Support summary" }));
  let sheet = within(await screen.findByRole("dialog", { name: "Support summary" }));
  expect(await sheet.findByText("set the sharing policy first", { exact: false }, { timeout: 30_000 })).toBeTruthy();
  expect((sheet.getByRole("button", { name: "Export support summary" }) as HTMLButtonElement).disabled).toBe(true);
  await press(user, sheet.getAllByRole("button", { name: "Change" }).at(-1)!);
  const policy = within(await screen.findByRole("dialog", { name: "Sharing policy" }));
  await press(user, policy.getByRole("radio", { name: "Allowed" }));
  await enter(user, policy.getByLabelText("Maximum size (bytes)"), "4096");
  await pressServed(user, journey, policy.getByRole("button", { name: "Save" }), "SaveProjectSharingPolicy");
  expect(journey.callsTo("ExecuteReviewedAction")).toHaveLength(1);

  // The preview is the summary itself: the run's outcome and the identities
  // it commits to, no value of the evidence.
  sheet = within(await screen.findByRole("dialog", { name: "Support summary" }));
  const summary = within(await sheet.findByLabelText("Summary", { selector: "dl" }));
  await waitFor(() => expect(summary.queryByText("Source identity")).toBeTruthy(), { timeout: 30_000 });
  expect(summary.getByText("Sharing policy").nextElementSibling?.textContent).toMatch(/^Allowed · up to 4096 bytes/);
  const identity = summary.getByText("Source identity").nextElementSibling?.textContent ?? "";
  expect(identity).not.toBe("");
  for (const value of ["PLACER-101", "SYNTH-101", "SYNTHETIC^ONLY", downstream.address]) {
    expect(within(screen.getByRole("dialog", { name: "Support summary" })).queryByText(value, { exact: false })).toBeNull();
  }

  // A place something is already at is refused in the review, before any
  // export, and nothing is written into it.
  journey.writeFile("outbox/for-vendor/kept.txt", "a file the person kept here\n");
  await journey.nameNewFolder(journey.path("outbox/for-vendor"), "Export package");
  await pressServed(user, journey, sheet.getByRole("button", { name: "Choose" }), "ChooseShareDestination");
  expect(await sheet.findByText("something is already there; choose a new name", undefined, { timeout: 30_000 })).toBeTruthy();
  expect((sheet.getByRole("button", { name: "Export support summary" }) as HTMLButtonElement).disabled).toBe(true);
  expect(journey.callsTo("ExecuteReviewedAction")).toHaveLength(1);
  expect(journey.readFile("outbox/for-vendor/kept.txt")).toBe("a file the person kept here\n");
  expect(() => journey.readFile("outbox/for-vendor/support.json")).toThrow();

  // Exported once into a new folder.
  const folder = "outbox/support-for-vendor";
  await journey.nameNewFolder(journey.path(folder), "Export package");
  // The destination's Change, the first of the summary's two.
  await pressServed(user, journey, sheet.getAllByRole("button", { name: "Change" })[0]!, "ChooseShareDestination");
  const exporting = journey.callsTo("ExecuteReviewedAction").length;
  await press(user, await waitFor(() => {
    const button = sheet.getByRole("button", { name: "Export support summary" }) as HTMLButtonElement;
    expect(button.disabled).toBe(false);
    return button;
  }, { timeout: 30_000 }));
  await waitFor(() => expect(journey.callsTo("ExecuteReviewedAction")[exporting]?.settled).toBe(true), { timeout: 30_000 });
  expect(journey.callsTo("ExecuteReviewedAction")[exporting]?.result, JSON.stringify(journey.callsTo("ExecuteReviewedAction")[exporting]?.result)).toMatchObject({ outcome: "completed" });
  expect((await sheet.findByRole("status")).textContent).toMatch(/^Exported support-for-vendor/);
  expect(journey.callsTo("ExecuteReviewedAction").slice(exporting)).toHaveLength(1);

  // The command line verifies the bundle the window wrote, and finds no
  // value of the evidence in it.
  const verified = await journey.commandLine(["share", "verify", journey.path(folder)]);
  expect([verified.code, verified.stderr]).toEqual([0, ""]);
  expect(JSON.parse(verified.stdout.split("\n")[0]!)).toMatchObject({ schema: "readmit-support-summary/v1", source_identity: identity });
  expect(verified.stdout).toMatch(/Verified support identity: [0-9a-f]{64}/);
  for (const value of ["PLACER-101", "SYNTH-101", "SYNTHETIC", downstream.address]) {
    expect(verified.stdout).not.toContain(value);
    expect(journey.readFile(`${folder}/support.json`)).not.toContain(value);
  }

  // Something rewrites the published summary: verified again, it is refused.
  const published = journey.readFile(`${folder}/support.json`);
  const altered = published.replace(/"outcome":"[a-z_]+"/, '"outcome":"pass"');
  expect(altered).not.toBe(published);
  journey.changeFile(`${folder}/support.json`, altered);
  expect(await journey.commandLine(["share", "verify", journey.path(folder)])).toEqual({ code: 1, stdout: "", stderr: REFUSED });

  // Reporting and exporting sent nothing.
  expect(downstream.received()).toHaveLength(sent);
});
