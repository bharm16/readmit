import { expect, test } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { RunnerPanel } from "./RunnerPanel";
import { installFacade } from "./testkit/wails";
import type { CatalogItem, RequestContext, RunnerEnrollmentResult, RunnerListResult, RunnerRow, SaveItemRequest, RunnerStatusResult } from "./bindings";

const ROOT = "/projects/scheduling";
const CONFIG = "/projects/scheduling/.readmit/objects/runner.json";

function row(over: Partial<RunnerRow> = {}): RunnerRow {
  return {
    ref: { kind: "runner", id: "r1", revision: "1" }, name: "QA runner", config: CONFIG, environment: "Scheduling QA", hub_environment: "lab",
    hub: "hub.example.test:8443", project: "alpha", root: "/var/lib/readmit-runner", status: "not-checked", active_jobs: 0, local: false, ...over,
  };
}

function listing(context: RequestContext, rows: RunnerRow[]): RunnerListResult {
  return { state: "completed", context, runners: rows };
}

const ENV: CatalogItem = {
  ref: { kind: "environment", id: "e1" }, name: "Scheduling QA", created_at: null, updated_at: null, last_opened_at: null, availability: "available",
  capabilities: [], summary: {} as CatalogItem["summary"],
};


function base(rows: () => RunnerRow[]) {
  return {
    ListRunners: (context: RequestContext) => listing(context, rows()),
    OperationStatus: () => ({ state: "completed" as const, selected: true, author_seats: 1, runner_instances: 2 }),
    ListCatalog: (query: { context: RequestContext }) => ({ state: "completed" as const, context: query.context, page: { items: [ENV], total: 1, snapshot: "", recorded: true, incomplete: [] } }),
  };
}

// A project with no runner offers exactly Add runner; one nothing has
// checked reads Not checked, and a recorded status is dated.
test("the runner list is named actual state, with Not checked and Offline apart and no forms", async () => {
  let rows: RunnerRow[] = [];
  installFacade(base(() => rows));
  const { unmount } = render(<RunnerPanel root={ROOT} />);
  expect(await screen.findByText("No runners")).toBeTruthy();
  expect(screen.queryByRole("textbox")).toBeNull();
  unmount();
  rows = [row({ name: "Registration runner", status: "offline", last_seen: "2026-09-29T08:00:00Z", reason: "The hub could not be reached" }), row()];
  render(<RunnerPanel root={ROOT} />);
  const table = await screen.findByRole("grid", { name: "Runners" }).catch(() => screen.findByRole("table", { name: "Runners" }));
  expect(within(table).getByText("Offline")).toBeTruthy();
  expect(within(table).getByText("Not checked")).toBeTruthy();
  expect(within(table).queryByText("Available")).toBeNull();
  expect(screen.queryByRole("textbox")).toBeNull();
});

// Add runner ends in a real admission for a runner on this Mac: the
// configuration's own reader decides before the review, and Request
// admission saves the runner and asks the hub.
test("Add runner saves the named runner and requests a real admission", async () => {
  const user = userEvent.setup();
  let rows: RunnerRow[] = [];
  const facade = installFacade({
    ...base(() => rows),
    ChooseRunnerPath: (kind: string) => ({ state: "completed", kind, paths: [kind === "working-folder" ? "/Users/qa/runner" : `/etc/${kind}.pem`] }),
    PreviewRunnerConfig: () => ({ state: "completed", document: "{}" }),
    SaveItem: (request: SaveItemRequest) => {
      rows = [row({ ref: { kind: "runner", id: "r2", revision: "1" }, name: request.draft.name ?? "" })];
      return { state: "completed", context: request.context, outcome: "saved", saved: { kind: "runner", id: "r2", revision: "1" }, replayed: false, problems: [] };
    },
    EnrollRunner: () => {
      rows = [row({ ref: { kind: "runner", id: "r2", revision: "1" }, status: "available", last_seen: "2026-09-30T08:00:00Z" })];
      return { state: "completed", project: "alpha", environment: "lab", expires_at: "2026-09-30T08:00:10Z", max_seconds: 600, max_jobs: 10 };
    },
  });
  render(<RunnerPanel root={ROOT} />);
  await user.click(await screen.findByRole("button", { name: "Add runner" }));
  const sheet = await screen.findByRole("dialog", { name: "Add runner" });
  await user.type(within(sheet).getByLabelText("Name"), "QA runner");
  await user.type(within(sheet).getByLabelText("Customer hub"), "https://hub.example.test:8443");
  await user.type(within(sheet).getByLabelText("Hub project"), "alpha");
  for (const label of ["hub certificate authority", "runner certificate", "key reader", "token reader"]) await user.click(within(sheet).getByRole("button", { name: `Choose ${label}` }));
  await user.type(within(sheet).getByLabelText("Deployment key"), "A".repeat(43) + "=");
  await user.type(within(sheet).getByLabelText("Approved build"), "NEXT");
  await user.click(within(sheet).getByRole("button", { name: "Next" }));
  await user.selectOptions(within(sheet).getByLabelText("Environment"), "e1");
  await user.type(within(sheet).getByLabelText("Hub environment"), "lab");
  await user.click(within(sheet).getByRole("button", { name: "Choose working folder" }));
  await user.click(within(sheet).getByRole("button", { name: "Next" }));
  expect(facade.callsTo("PreviewRunnerConfig")).toHaveLength(1);
  expect(within(sheet).getByText("Asks https://hub.example.test:8443 to admit this runner.")).toBeTruthy();
  await user.click(within(sheet).getByRole("button", { name: "Request admission" }));
  await waitFor(() => expect(facade.callsTo("EnrollRunner")).toHaveLength(1));
  const saved = facade.oneCall("SaveItem")[0] as SaveItemRequest;
  expect(saved.kind).toBe("runner");
  expect(saved.draft.runner).toMatchObject({ project: "alpha", environment: "lab", root: "/Users/qa/runner", assigned: "e1" });
  expect(facade.oneCall("EnrollRunner")[0]).toBe(CONFIG);
  expect(await screen.findByRole("heading", { name: "QA runner" })).toBeTruthy();
  expect(await screen.findByText("Available")).toBeTruthy();
});

// Setup handed to another host's administrator is exported and the runner
// stays Setup required: nothing claims it was installed or admitted.
test("Add runner for another host exports its setup and never claims installation", async () => {
  const user = userEvent.setup();
  let rows: RunnerRow[] = [];
  const facade = installFacade({
    ...base(() => rows),
    ChooseRunnerPath: (kind: string) => ({ state: "completed", kind, paths: [`/etc/${kind}.pem`] }),
    PreviewRunnerConfig: () => ({ state: "completed" }),
    SaveItem: (request: SaveItemRequest) => {
      rows = [row({ ref: { kind: "runner", id: "r3", revision: "1" } })];
      return { state: "completed", context: request.context, outcome: "saved", saved: { kind: "runner", id: "r3", revision: "1" }, replayed: false, problems: [] };
    },
    SaveRunnerConfig: () => {
      rows = [row({ ref: { kind: "runner", id: "r3", revision: "1" }, status: "setup-required", last_seen: "2026-09-30T08:00:00Z" })];
      return { state: "completed", output: "/Users/qa/readmit-runner.json" };
    },
  });
  render(<RunnerPanel root={ROOT} />);
  await user.click(await screen.findByRole("button", { name: "Add runner" }));
  const sheet = await screen.findByRole("dialog", { name: "Add runner" });
  await user.type(within(sheet).getByLabelText("Name"), "Remote runner");
  await user.type(within(sheet).getByLabelText("Customer hub"), "https://hub.example.test:8443");
  await user.type(within(sheet).getByLabelText("Hub project"), "alpha");
  for (const label of ["hub certificate authority", "runner certificate", "key reader", "token reader"]) await user.click(within(sheet).getByRole("button", { name: `Choose ${label}` }));
  await user.type(within(sheet).getByLabelText("Deployment key"), "key");
  await user.type(within(sheet).getByLabelText("Approved build"), "NEXT");
  await user.click(within(sheet).getByRole("button", { name: "Next" }));
  await user.type(within(sheet).getByLabelText("Hub environment"), "lab");
  await user.click(within(sheet).getByLabelText("Another host"));
  await user.type(within(sheet).getByLabelText("Working folder"), "/var/lib/readmit-runner");
  await user.click(within(sheet).getByRole("button", { name: "Next" }));
  await user.click(within(sheet).getByRole("button", { name: "Export setup" }));
  await waitFor(() => expect(facade.callsTo("SaveRunnerConfig")).toHaveLength(1));
  expect(facade.oneCall("SaveRunnerConfig")[0]).toMatchObject({ source: CONFIG, output: "", root: "/var/lib/readmit-runner" });
  expect(facade.callsTo("EnrollRunner")).toHaveLength(0);
  expect(await screen.findByText("Setup required")).toBeTruthy();
  expect(screen.queryByText(/installed|enrolled/i)).toBeNull();
});

// Access, Capacity, Recovery and Update are named tasks of one runner, each
// scoped to it; capacity is settled only by the action naming the instance.
test("runner capacity is shown and settled explicitly, never silently", async () => {
  const user = userEvent.setup();
  const held = (): RunnerStatusResult => ({
    state: "completed", organization: "example-hospital", authority: "local-runner", instances: 2, active: 1, stale: 1, free: 0,
    admissions: [
      { instance: "build-4821", admitted: "2026-09-20T09:00:00Z", lease_until: "2026-09-20T10:00:00Z", state: "active" },
      { instance: "build-4822", admitted: "2026-09-20T09:30:00Z", lease_until: "2026-09-20T09:40:00Z", state: "stale" },
    ],
  });
  const facade = installFacade({
    ...base(() => [row()]),
    ShowRunnerAdmissions: () => held(),
    SettleRunnerAdmission: () => held(),
  });
  render(<RunnerPanel root={ROOT} />);
  await user.dblClick(await screen.findByText("QA runner"));
  await user.click(await screen.findByRole("button", { name: "More runner actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "Capacity" }));
  const sheet = await screen.findByRole("dialog", { name: "Capacity" });
  expect(await within(sheet).findByText("Licensed")).toBeTruthy();
  expect(facade.callsTo("SettleRunnerAdmission")).toHaveLength(0);
  await user.click(within(sheet).getByRole("button", { name: "Actions for build-4821" }));
  expect((await screen.findByRole("menuitem", { name: "Release" })).hasAttribute("disabled")).toBe(true);
  await user.keyboard("{Escape}");
  await user.click(within(sheet).getByRole("button", { name: "Actions for build-4822" }));
  await user.click(await screen.findByRole("menuitem", { name: "Reconcile" }));
  const confirm = await screen.findByRole("dialog", { name: "Reconcile" });
  expect(within(confirm).getByText("Frees capacity held by this stale instance.")).toBeTruthy();
  await user.click(within(confirm).getByRole("button", { name: "Reconcile build-4822" }));
  await waitFor(() => expect(facade.callsTo("SettleRunnerAdmission")).toHaveLength(1));
  expect(facade.oneCall("SettleRunnerAdmission")[0]).toEqual({ instance: "build-4822", reconcile: true });
});

test("Access shows the actual grants and saves one grant after its exact scope", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    ...base(() => [row()]),
    ChooseRunnerPath: (kind: string) => ({ state: "completed", kind, paths: ["/etc/readmit-hub/runners.json"] }),
    ReadRunnerGrants: () => ({ state: "completed", grants: [{ project: "alpha", subject: "runner-qa", environment: "lab", engine: "E1", spec: "readmit-test/v1", profile: "readmit-siu-v1", max_seconds: 900, max_jobs: 5 }] }),
    SaveRunnerGrant: () => ({ state: "completed", output: "/Users/qa/readmit-runner-policy.json" }),
  });
  render(<RunnerPanel root={ROOT} />);
  await user.dblClick(await screen.findByText("QA runner"));
  await user.click(await screen.findByRole("button", { name: "More runner actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "Access" }));
  const sheet = await screen.findByRole("dialog", { name: "Access" });
  await user.click(within(sheet).getByRole("button", { name: "Choose current runner policy" }));
  const grants = await within(sheet).findByRole("table", { name: "Grants" });
  expect(within(grants).getByText("runner-qa")).toBeTruthy();
  expect((within(sheet).getByLabelText("Runner subject") as HTMLInputElement).value).toBe("runner-qa");
  await user.click(within(sheet).getByRole("button", { name: "Next" }));
  expect(within(sheet).getByText("900 s")).toBeTruthy();
  await user.click(within(sheet).getByRole("button", { name: "Save grant" }));
  await waitFor(() => expect(facade.callsTo("SaveRunnerGrant")).toHaveLength(1));
  expect(facade.oneCall("SaveRunnerGrant")[0]).toMatchObject({ policy: "/etc/readmit-hub/runners.json", project: "alpha", subject: "runner-qa", environment: "lab", max_seconds: 900, max_jobs: 5, output: "" });
});

test("Recovery reads retained jobs and Update verifies a staged candidate without running it", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    ...base(() => [row({ local: true })]),
    ReadRunnerConfig: () => ({ state: "completed", jobs: [{ id: "nightly-001", state: "cancelled", delivery_uncertain: true, journal_incomplete: false }] }),
    ReadRunnerRecovery: () => ({ state: "completed", job_id: "nightly-001", acknowledged: 1, uncertain: 1, not_attempted: 2 }),
    ChooseRunnerPath: (kind: string) => ({ state: "completed", kind, paths: [`/staged/${kind}`] }),
    VerifyRunnerUpdate: () => ({ state: "completed", engine: "NEXT" }),
  });
  render(<RunnerPanel root={ROOT} />);
  await user.dblClick(await screen.findByText("QA runner"));
  await user.click(await screen.findByRole("button", { name: "More runner actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "Recovery" }));
  const recovery = await screen.findByRole("dialog", { name: "Recovery" });
  await user.click(within(recovery).getByRole("button", { name: "nightly-001" }));
  expect(await within(recovery).findByText("Not attempted")).toBeTruthy();
  expect(within(recovery).queryByRole("button", { name: /Retry|Resend|Send/ })).toBeNull();
  await user.click(within(recovery).getByRole("button", { name: "Close" }));
  await user.click(screen.getByRole("button", { name: "More runner actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "Update" }));
  const update = await screen.findByRole("dialog", { name: "Update" });
  await user.click(within(update).getByRole("button", { name: "Choose update manifest" }));
  await user.click(within(update).getByRole("button", { name: "Choose staged program" }));
  await user.click(within(update).getByRole("button", { name: "Verify update" }));
  expect(await within(update).findByText(/Verified for build NEXT/)).toBeTruthy();
  expect(facade.oneCall("VerifyRunnerUpdate")).toEqual([CONFIG, "/staged/update-manifest", "/staged/update-program"]);
});

test("a retained job id is never replayed and no resend is offered", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    ...base(() => [row()]),
    ChooseRunnerPath: (kind: string) => ({ state: "completed", kind, paths: ["/srv/tests/booking.json"] }),
    SaveRunnerJob: () => ({ state: "completed", output: "/srv/jobs/nightly-002.json" }),
    InspectRunnerJob: () => ({ state: "completed", job_id: "nightly-002", spec: "/srv/tests/booking.json", input_identity: "f".repeat(64), environment: "lab" }),
    ExecuteRunnerJob: () => ({ state: "failed", reason: "the runner already holds job nightly-002; a retained job id is never run again" }),
  });
  render(<RunnerPanel root={ROOT} />);
  await user.dblClick(await screen.findByText("QA runner"));
  await user.click(await screen.findByRole("button", { name: "More runner actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "Run job" }));
  const sheet = await screen.findByRole("dialog", { name: "Run job" });
  await user.click(within(sheet).getByLabelText("New job"));
  await user.type(within(sheet).getByLabelText("Job name"), "nightly-002");
  await user.click(within(sheet).getByRole("button", { name: "Choose test" }));
  await user.click(within(sheet).getByRole("button", { name: "Next" }));
  expect(await within(sheet).findByText("Sends this job's messages to lab once.")).toBeTruthy();
  await user.click(within(sheet).getByRole("button", { name: "Run job" }));
  expect(await within(sheet).findByText(/never run again/)).toBeTruthy();
  expect(facade.oneCall("ExecuteRunnerJob")[0]).toEqual({ config_path: CONFIG, job_path: "/srv/jobs/nightly-002.json", expected_identity: "f".repeat(64) });
  expect(within(sheet).queryByRole("button", { name: /Retry|Resend/ })).toBeNull();
});

// Request admission reports the lease the hub granted; a refusal is the
// hub's own reason.
test("enrollment reports the lease the hub granted", async () => {
  const user = userEvent.setup();
  let answer: RunnerEnrollmentResult = { state: "permission_denied", reason: "hub refused admission (Forbidden): version or environment refused" };
  installFacade({ ...base(() => [row()]), EnrollRunner: () => answer });
  render(<RunnerPanel root={ROOT} />);
  await user.dblClick(await screen.findByText("QA runner"));
  await user.click(await screen.findByRole("button", { name: "Request admission" }));
  expect(await screen.findByRole("alert")).toHaveProperty("textContent", "hub refused admission (Forbidden): version or environment refused");
  answer = { state: "completed", project: "alpha", environment: "lab", expires_at: "2026-09-30T08:00:10Z", max_seconds: 600, max_jobs: 10 };
  await user.click(screen.getByRole("button", { name: "Request admission" }));
  expect(await screen.findByText("600 s")).toBeTruthy();
  expect(screen.getByText("Max jobs")).toBeTruthy();
});
