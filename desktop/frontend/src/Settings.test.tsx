// Settings → General and Security. General shows saved values and edits them
// in one sheet that previews; Security lists the configured connections the
// facade reads from saved configuration and the window's own state, and
// edits privacy values in one sheet. Fixtures carry names, states and times
// only; destinations are synthetic.
import { expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { CatalogItem, ConnectionRow, ItemDraft, ItemRequest, Preferences } from "./bindings";
import { renderApp } from "./testkit/app";
import { folderWithCase, shellResult } from "./testkit/fixtures";
import { goTo, goToView, page } from "./testkit/navigation";

type User = ReturnType<typeof userEvent.setup>;

function connection(row: Partial<ConnectionRow> & Pick<ConnectionRow, "ref" | "name" | "kind" | "state">): ConnectionRow {
  return {
    destination: "",
    checked_at: null,
    last_seen: null,
    owner: { kind: row.kind },
    disclosure: "",
    actions: [],
    detail: { signed_in: false },
    ...row,
  };
}

const QA = connection({
  ref: "environment:qa",
  name: "Scheduling QA",
  kind: "environment",
  destination: "qa.example.test:2575",
  state: "checked",
  checked_at: "2026-01-02T09:00:00Z",
  owner: { kind: "environment", object_id: "qa" },
  actions: ["edit"],
  detail: { signed_in: false, outcome: "reachable", transport: "plain" },
});
const SOURCE = connection({
  ref: "observation:appointments",
  name: "Appointments",
  kind: "source",
  destination: "records.example.test",
  state: "not-checked",
  owner: { kind: "observation", object_id: "appointments" },
  actions: ["edit"],
});
const HUB = connection({
  ref: "hub:client",
  name: "Team hub",
  kind: "team",
  destination: "team.example.test",
  state: "connected",
  last_seen: "2026-01-02T10:00:00Z",
  owner: { kind: "team" },
  actions: ["edit", "disconnect"],
  detail: { signed_in: true },
});

function rowsOf(table: HTMLElement): string[][] {
  return Array.from(table.querySelectorAll("tbody tr[data-row-id]")).map((row) => Array.from(row.querySelectorAll("td,th")).map((cell) => cell.textContent ?? ""));
}

async function openSecurity(user: User) {
  await goToView(user, "Settings", "Security");
  return page().findByRole("table", { name: "Connections" });
}

async function openProject(user: User) {
  await goTo(user, "Projects");
  await user.click(screen.getByRole("button", { name: "Open" }));
}

test("General shows saved values and Edit previews theme and text size, Cancel restores them and Save persists them", async () => {
  const user = userEvent.setup();
  const saved: Preferences = { theme: "light", text_scale: 125, reviewer: "Avery QA" };
  const { facade } = await renderApp({
    ReadPreferences: () => ({ state: "completed", preferences: saved }),
    SavePreferences: (preferences) => ({ state: "completed", preferences }),
  });
  await goToView(user, "Settings", "General");
  const general = page().getByLabelText("General", { selector: "dl" });
  await waitFor(() => expect(within(general).getByText("Light")).toBeTruthy());
  expect(within(general).getByText("125%")).toBeTruthy();
  expect(within(general).getByText("Avery QA")).toBeTruthy();
  // Values, not fields.
  expect(page().queryAllByRole("textbox")).toHaveLength(0);
  expect(document.documentElement.getAttribute("data-theme")).toBe("light");

  await user.click(page().getByRole("button", { name: "Edit" }));
  let sheet = await screen.findByRole("dialog", { name: "General" });
  await user.selectOptions(within(sheet).getByLabelText("Theme"), "dark");
  await user.selectOptions(within(sheet).getByLabelText("Text size"), "200");
  // The choice previews at once, before anything is saved.
  expect(document.documentElement.getAttribute("data-theme")).toBe("dark");
  expect(document.documentElement.style.getPropertyValue("--text-scale")).toBe("2");
  expect(facade.callsTo("SavePreferences")).toHaveLength(0);
  await user.click(within(sheet).getByRole("button", { name: "Cancel" }));
  await user.click(await screen.findByRole("button", { name: "Discard" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "General" })).toBeNull());
  expect(document.documentElement.getAttribute("data-theme")).toBe("light");
  expect(document.documentElement.style.getPropertyValue("--text-scale")).toBe("1.25");

  await user.click(page().getByRole("button", { name: "Edit" }));
  sheet = await screen.findByRole("dialog", { name: "General" });
  await user.selectOptions(within(sheet).getByLabelText("Theme"), "dark");
  await user.selectOptions(within(sheet).getByLabelText("Text size"), "175");
  await user.clear(within(sheet).getByLabelText("Reviewer"));
  await user.type(within(sheet).getByLabelText("Reviewer"), "Jordan Lab");
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "General" })).toBeNull());
  expect(facade.callsTo("SavePreferences").at(-1)?.args[0]).toEqual({ theme: "dark", text_scale: 175, reviewer: "Jordan Lab" });
  expect(within(general).getByText("Dark")).toBeTruthy();
  expect(within(general).getByText("175%")).toBeTruthy();
  expect(document.documentElement.getAttribute("data-theme")).toBe("dark");
  expect(document.documentElement.style.getPropertyValue("--text-scale")).toBe("1.75");
});

test("a failed preferences save keeps the sheet open with the chosen values", async () => {
  const user = userEvent.setup();
  await renderApp({
    SavePreferences: () => ({ state: "failed", reason: "The preferences file cannot be written.", preferences: { theme: "system", text_scale: 100 } }),
  });
  await goToView(user, "Settings", "General");
  await user.click(page().getByRole("button", { name: "Edit" }));
  const sheet = await screen.findByRole("dialog", { name: "General" });
  await user.selectOptions(within(sheet).getByLabelText("Theme"), "dark");
  await user.type(within(sheet).getByLabelText("Reviewer"), "Avery QA");
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  expect(await within(sheet).findByText("The preferences file cannot be written.")).toBeTruthy();
  expect(screen.getByRole("dialog", { name: "General" })).toBeTruthy();
  expect((within(sheet).getByLabelText("Theme") as HTMLSelectElement).value).toBe("dark");
  expect((within(sheet).getByLabelText("Reviewer") as HTMLInputElement).value).toBe("Avery QA");
});

test("About shows the application's version, build, build time and release channel", async () => {
  const user = userEvent.setup();
  const shell = shellResult();
  shell.shell!.build = { ...shell.shell!.build, modified: true };
  await renderApp({ Shell: () => shell });
  await goToView(user, "Settings", "General");
  await user.click(page().getByRole("button", { name: "More general settings" }));
  await user.click(await screen.findByRole("menuitem", { name: "About" }));
  const about = await screen.findByRole("dialog", { name: "About Readmit" });
  const value = (label: string) => Array.from(about.querySelectorAll("dt")).find((term) => term.textContent === label)?.nextElementSibling?.textContent;
  expect(value("Version")).toBe("0.0.0-test");
  expect(value("Build")).toBe("3527e801aa0b · modified");
  expect(value("Built")).toBeTruthy();
  expect(value("Release")).toBe("Development preview, unsigned");
});

test("About shows no Build or Built row for a build without a version-control stamp", async () => {
  const user = userEvent.setup();
  const shell = shellResult();
  shell.shell!.build = { version: "0.0.0-test", modified: false, channel: "Development preview, unsigned" };
  await renderApp({ Shell: () => shell });
  await goToView(user, "Settings", "General");
  await user.click(page().getByRole("button", { name: "More general settings" }));
  await user.click(await screen.findByRole("menuitem", { name: "About" }));
  const about = await screen.findByRole("dialog", { name: "About Readmit" });
  expect(Array.from(about.querySelectorAll("dt")).map((term) => term.textContent)).toEqual(["Application", "Version", "Release"]);
});

test("the security inventory lists saved environments, sources and the team hub with their actual states", async () => {
  const user = userEvent.setup();
  await renderApp({ ListConnections: (context) => ({ state: "completed", context, rows: [HUB, SOURCE, QA] }) });
  const table = await openSecurity(user);
  await waitFor(() => expect(rowsOf(table)).toHaveLength(3));
  expect(rowsOf(table)).toEqual([
    ["Team hub", "team.example.test", "Connected"],
    ["Appointments", "records.example.test", "Not checked"],
    ["Scheduling QA", "qa.example.test:2575", expect.stringMatching(/^Checked /)],
  ]);
  // Privacy values are values; nothing on the page is an input.
  expect(page().getByText("Hidden by default")).toBeTruthy();
  expect(page().getByText("This project")).toBeTruthy();
  expect(page().queryAllByRole("textbox")).toHaveLength(0);

  await user.click(within(table).getByText("Scheduling QA"));
  const detail = await screen.findByRole("dialog", { name: "Scheduling QA" });
  expect(within(detail).getByText("Environment")).toBeTruthy();
  expect(within(detail).getByText("TCP/MLLP")).toBeTruthy();
  expect(within(detail).getByRole("button", { name: "Edit" })).toBeTruthy();
  expect(within(detail).queryByRole("button", { name: "Disconnect" })).toBeNull();
});

test("with no connections Security offers Add connection", async () => {
  const user = userEvent.setup();
  await renderApp();
  await goToView(user, "Settings", "Security");
  expect(await page().findByText("No connections")).toBeTruthy();
  expect(page().getAllByRole("button", { name: "Add connection" }).length).toBeGreaterThan(0);
});

test("visiting Security calls no check, Hub status, credential or runner operation", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({ ListConnections: (context) => ({ state: "completed", context, rows: [QA, HUB] }) });
  const reaching = ["CheckEnvironment", "CheckEnvironmentDestination", "CheckCredential", "HubStatus", "ConnectHub", "ReadRunnerConfig", "DiagnoseHub", "CheckValidator", "InstallValidator", "RemoveValidator", "ImportConnectionExample"] as const;
  const before = reaching.map((method) => facade.callsTo(method).length);
  const table = await openSecurity(user);
  await waitFor(() => expect(rowsOf(table)).toHaveLength(2));
  await user.click(page().getByRole("button", { name: "Refresh status" }));
  await user.click(within(table).getByText("Team hub"));
  await screen.findByRole("dialog", { name: "Team hub" });
  expect(reaching.map((method) => facade.callsTo(method).length)).toEqual(before);
  expect(facade.callsTo("ListConnections").length).toBeGreaterThanOrEqual(2);
});

test("Security reads the saved FHIR protocol and authentication boundaries without checking the connection", async () => {
  const user = userEvent.setup();
  const fhir = connection({ ref: "environment:fhir-qa", name: "FHIR QA", kind: "environment", state: "not-checked", destination: "fhir-peer", owner: { kind: "environment", object_id: "fhir-qa" }, actions: ["edit"], detail: { signed_in: false, protocol: "fhir-r4", version: "4.0.1", authentication: "smart", transport: "https", validator: "capability-installed-worker-not-checked", data: "Reviewed FHIR resource reads", authorization: "Registered observer scope" } });
  const { facade } = await renderApp({ ListConnections: (context) => ({ state: "completed", context, rows: [fhir] }) });
  const table = await openSecurity(user);
  await user.click(await within(table).findByText("FHIR QA"));
  const detail = await screen.findByRole("dialog", { name: "FHIR QA" });
  expect(within(detail).getByText("FHIR R4 4.0.1")).toBeTruthy();
  expect(within(detail).getByText("SMART Backend Services")).toBeTruthy();
  expect(within(detail).getByText("HTTPS")).toBeTruthy();
  expect(within(detail).getByText("Local, offline · Worker not checked")).toBeTruthy();
  expect(within(detail).getByText("Reviewed FHIR resource reads")).toBeTruthy();
  expect(within(detail).getByText("Registered observer scope")).toBeTruthy();
  expect(within(detail).getByText("Not checked")).toBeTruthy();
  expect(facade.callsTo("CheckEnvironment")).toHaveLength(0);
  expect(facade.callsTo("CheckValidator")).toHaveLength(0);
  expect(facade.callsTo("PrepareAction")).toHaveLength(0);
  expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
});

test("an explicit environment check reads Checked with its time, never Connected", async () => {
  const user = userEvent.setup();
  await renderApp({ ListConnections: (context) => ({ state: "completed", context, rows: [QA] }) });
  const table = await openSecurity(user);
  await waitFor(() => expect(rowsOf(table)).toHaveLength(1));
  const status = rowsOf(table)[0]![2]!;
  expect(status).toMatch(/^Checked /);
  expect(status).not.toMatch(/Connected/);
  await user.click(within(table).getByText("Scheduling QA"));
  const detail = await screen.findByRole("dialog", { name: "Scheduling QA" });
  expect(within(detail).getByText(/^Reachable · /)).toBeTruthy();
  expect(within(detail).queryByText("Connected")).toBeNull();
});

test("an active operation stays listed after its environment is removed", async () => {
  const user = userEvent.setup();
  const active = connection({ ...QA, state: "active", checked_at: null });
  const { facade } = await renderApp({ ListConnections: (context) => ({ state: "completed", context, rows: [active, SOURCE] }) });
  const table = await openSecurity(user);
  await waitFor(() => expect(rowsOf(table)[0]).toEqual(["Scheduling QA", "qa.example.test:2575", "Active"]));
  // Another window removes the environment while the check still runs: the
  // facade keeps the row active, and the refreshed list still shows it.
  facade.reply({ ListConnections: (context) => ({ state: "completed", context, rows: [{ ...active, actions: [] }] }) });
  await user.click(page().getByRole("button", { name: "Refresh status" }));
  await waitFor(() => expect(rowsOf(table)).toEqual([["Scheduling QA", "qa.example.test:2575", "Active"]]));
});

test("Disconnect appears only for a connected Hub session", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    ListConnections: (context) => ({ state: "completed", context, rows: [HUB, QA] }),
    DisconnectHub: () => ({ state: "completed", connected: false, authenticated: false }),
  });
  const table = await openSecurity(user);
  await waitFor(() => expect(rowsOf(table)).toHaveLength(2));
  await user.click(within(table).getByText("Scheduling QA"));
  let detail = await screen.findByRole("dialog", { name: "Scheduling QA" });
  expect(within(detail).queryByRole("button", { name: "Disconnect" })).toBeNull();
  await user.keyboard("{Escape}");
  await user.click(within(table).getByText("Team hub"));
  detail = await screen.findByRole("dialog", { name: "Team hub" });
  facade.reply({
    ListConnections: (context) => ({ state: "completed", context, rows: [{ ...HUB, state: "disconnected", actions: ["edit"] }, QA] }),
  });
  await user.click(within(detail).getByRole("button", { name: "Disconnect" }));
  expect(facade.callsTo("DisconnectHub")).toHaveLength(1);
  await waitFor(() => expect(rowsOf(table)[0]).toEqual(["Team hub", "team.example.test", "Disconnected"]));
});

test("closing Add environment unsaved returns to Security with no connection selected", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    SelectWorkspace: () => folderWithCase(),
    ListConnections: (context) => ({ state: "completed", context, rows: [QA] }),
    OpenItemDraft: (request) => ({ state: "completed", context: request.context, new: true, draft: NEW_ENVIRONMENT }),
    ListCredentials: (request) => ({ state: "completed", context: request.context, credentials: [], referring: [] }),
  });
  await openProject(user);
  await openSecurity(user);
  await user.click(await page().findByRole("button", { name: "Add connection" }));
  await user.click(await screen.findByRole("menuitem", { name: "Environment" }));
  const sheet = await screen.findByRole("dialog", { name: "Add environment" });
  await user.click(within(sheet).getByRole("button", { name: "Cancel" }));
  expect(await page().findByRole("table", { name: "Connections" })).toBeTruthy();
  expect(page().getByRole("button", { name: "Security", current: "page" })).toBeTruthy();
  expect(screen.queryByRole("dialog")).toBeNull();
  expect(facade.callsTo("SaveItem")).toHaveLength(0);
});

test("Add connection's Source opens Add observation, and closing it unsaved returns to Security", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    SelectWorkspace: () => folderWithCase(),
    OpenItemDraft: (request) => ({
      state: "completed",
      context: request.context,
      new: true,
      draft: {
        observation: {
          source: {
            schema: "readmit-observation-source/v1",
            source: { kind: "file-export", identity: "scheduling-archive", scope: "appointments" },
            enabled: true,
            freshness: { max_age: "1h" },
            extraction: { envelope: "csv", encoding: "utf-8", record_key: [] },
            file: { path: "", max_bytes: 65536 },
            http: null,
            capture: null,
          },
          window: {
            schema: "readmit-observation-window/v1",
            source: { kind: "file-export", identity: "scheduling-archive", scope: "appointments" },
            watermark: { kind: "none", position: "" },
            pre_existing_state: { declaration: "declared-empty", baseline_identity: "" },
            completion: { deadline: "30s", quiet_period: "2s", stable_samples: 3, max_records: 100, max_samples: 16 },
          },
        },
      },
    }),
    ListCredentials: (request) => ({ state: "completed", context: request.context, credentials: [], referring: [] }),
    ObservationSupport: () => ({ state: "completed", support: [] }),
  });
  await openProject(user);
  await goToView(user, "Settings", "Security");
  await user.click(await page().findByRole("button", { name: "Add connection" }));
  await user.click(await screen.findByRole("menuitem", { name: "Source" }));
  const sheet = await screen.findByRole("dialog", { name: "Add observation" });
  expect(facade.callsTo("OpenItemDraft").map((call) => call.args[0])).toContainEqual(expect.objectContaining({ ref: { kind: "observation", id: "" } }));
  expect(within(sheet).getByRole("combobox", { name: "Type" })).toBeTruthy();
  await user.click(within(sheet).getByRole("button", { name: "Cancel" }));
  await waitFor(() => expect(page().getByRole("heading", { level: 1, name: "Settings" })).toBeTruthy());
  expect(facade.callsTo("SaveItem")).toHaveLength(0);
  const starts = facade.callsTo("OpenItemDraft").filter((call) => (call.args[0] as ItemRequest).ref.kind === "observation" && (call.args[0] as ItemRequest).ref.id === "").length;
  await goTo(user, "Environments");
  expect(await page().findByRole("heading", { level: 1, name: "Environments" })).toBeTruthy();
  expect(screen.queryByRole("dialog", { name: "Add observation" })).toBeNull();
  expect(facade.callsTo("OpenItemDraft").filter((call) => (call.args[0] as ItemRequest).ref.kind === "observation" && (call.args[0] as ItemRequest).ref.id === "")).toHaveLength(starts);
  expect(facade.callsTo("CheckEnvironment")).toHaveLength(0);
  expect(facade.callsTo("CollectObservation")).toHaveLength(0);
});

test("privacy Edit prefills the case's policy, offers indefinite retention and never extends an expired end", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    SelectWorkspace: () => folderWithCase(),
    ListSearchSettings: () => ({
      state: "completed",
      cases: [
        { case: "reschedule-case", identity: "id-1", settings: { fields: ["PID-3"], retention: "digests", retain_until: "2025-01-01T00:00:00Z", expired: true } },
        { case: "cancel-case", identity: "id-2", settings: { fields: ["SCH-1", "PID-3"], retention: "states", retain_until: null, expired: false } },
      ],
    }),
    SaveSearchSettings: () => ({ state: "completed" }),
  });
  await openProject(user);
  await goToView(user, "Settings", "Security");
  await user.click(await page().findByRole("button", { name: "Edit" }));
  const sheet = await screen.findByRole("dialog", { name: "Privacy" });
  expect((within(sheet).getByLabelText("Field 1") as HTMLInputElement).value).toBe("PID-3");
  expect((within(sheet).getByLabelText("Stored as") as HTMLSelectElement).value).toBe("digests");
  // The expired end is not carried forward.
  expect((within(sheet).getByLabelText("Keep until") as HTMLInputElement).value).toBe("");
  expect(within(sheet).getByRole("button", { name: "Save" })).toHaveProperty("disabled", true);

  await user.selectOptions(within(sheet).getByLabelText("Case"), "cancel-case");
  expect((within(sheet).getByLabelText("Field 2") as HTMLInputElement).value).toBe("PID-3");
  expect(within(sheet).getByRole("radio", { name: "Indefinitely" })).toHaveProperty("checked", true);
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveSearchSettings")).toHaveLength(1));
  expect(facade.callsTo("SaveSearchSettings")[0]!.args[0]).toMatchObject({ case: "cancel-case", identity: "id-2", fields: ["SCH-1", "PID-3"], retention: "states", retain_until: "indefinite" });
});

test("Clear saved searches confirms and removes only this project's views", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    SelectWorkspace: () => folderWithCase(),
    ListSearchSettings: () => ({ state: "empty", cases: [] }),
    ClearViews: () => ({ state: "empty", views: [] }),
  });
  await openProject(user);
  await goToView(user, "Settings", "Security");
  await user.click(await page().findByRole("button", { name: "Edit" }));
  const sheet = await screen.findByRole("dialog", { name: "Privacy" });
  await user.click(within(sheet).getByRole("button", { name: "Clear saved searches" }));
  const confirm = await screen.findByRole("dialog", { name: "Clear saved searches?" });
  await user.click(within(confirm).getByRole("button", { name: "Cancel" }));
  expect(facade.callsTo("ClearViews")).toHaveLength(0);
  await user.click(within(await screen.findByRole("dialog", { name: "Privacy" })).getByRole("button", { name: "Clear saved searches" }));
  await user.click(within(await screen.findByRole("dialog", { name: "Clear saved searches?" })).getByRole("button", { name: "Clear" }));
  await waitFor(() => expect(facade.callsTo("ClearViews")).toHaveLength(1));
  expect(facade.callsTo("ClearViews")[0]!.args[0]).toBe("/workspace-under-test");
  expect(await screen.findByText("Saved searches cleared.")).toBeTruthy();
});

test("an unchanged dated end is saved as the same instant", async () => {
  const user = userEvent.setup();
  const end = "2099-03-04T05:06:00.000Z";
  const { facade } = await renderApp({
    SelectWorkspace: () => folderWithCase(),
    ListSearchSettings: () => ({ state: "completed", cases: [{ case: "reschedule-case", identity: "id-1", settings: { fields: ["PID-3"], retention: "states", retain_until: end, expired: false } }] }),
    SaveSearchSettings: () => ({ state: "completed" }),
  });
  await openProject(user);
  await goToView(user, "Settings", "Security");
  await user.click(await page().findByRole("button", { name: "Edit" }));
  const sheet = await screen.findByRole("dialog", { name: "Privacy" });
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveSearchSettings")).toHaveLength(1));
  expect(new Date((facade.callsTo("SaveSearchSettings")[0]!.args[0] as { retain_until: string }).retain_until).toISOString()).toBe(end);
});

test("an unreadable project catalog is said, not shown as no connections", async () => {
  const user = userEvent.setup();
  await renderApp({ ListConnections: (context) => ({ state: "completed", context, rows: [HUB], project_reason: "The project's catalog cannot be read." }) });
  await openSecurity(user);
  expect(await page().findByText("The project's catalog cannot be read.")).toBeTruthy();
  expect(page().queryByText("No connections")).toBeNull();
});

test("Edit on an environment connection opens its connection sheet and returns to it after saving", async () => {
  const user = userEvent.setup();
  const environment: CatalogItem = {
    ref: { kind: "environment", id: "qa", revision: "1" },
    name: "Scheduling QA",
    created_at: null,
    updated_at: null,
    last_opened_at: null,
    availability: "available",
    capabilities: [],
    summary: { environment: { classification: "nonproduction", address: "qa.example.test:2575", transport: "plain", transport_approved: true, approval_required: true, last_checked_at: null, observation: null, has_policy: false, reset_actions: 0 } },
  };
  const { facade } = await renderApp({
    SelectWorkspace: () => folderWithCase(),
    ListConnections: (context) => ({ state: "completed", context, rows: [QA] }),
    ListCredentials: (request) => ({ state: "completed", context: request.context, credentials: [], referring: [] }),
    SaveItem: (request) => ({ state: "completed", context: request.context, outcome: "saved", saved: { kind: "environment", id: "qa", revision: "2" }, replayed: false, problems: [] }),
    OpenItemDraft: (request) => ({
      state: "completed",
      context: request.context,
      new: false,
      ref: environment.ref,
      draft: { name: "Scheduling QA", environment: { schema: "readmit-target/v3", name: "Scheduling-QA", test_endpoint: true, address: "qa.example.test:2575", transport: "plain", approved_transport: true, classification: "nonproduction", connect_timeout: "2s", message_timeout: "5s", max_ack_bytes: 65536 } },
    }),
  });
  facade.reply({
    ListCatalog: (query) =>
      query.kind === "environment"
        ? { state: "completed", context: query.context, page: { items: [environment], total: 1, snapshot: "s", recorded: true, incomplete: [] } }
        : { state: "completed", context: query.context, page: { items: [], total: 0, snapshot: "s", recorded: true, incomplete: [] } },
  });
  await openProject(user);
  const table = await openSecurity(user);
  await user.click(await within(table).findByText("Scheduling QA"));
  await user.click(within(await screen.findByRole("dialog", { name: "Scheduling QA" })).getByRole("button", { name: "Edit" }));
  const sheet = await screen.findByRole("dialog", { name: "Edit connection" });
  const port = within(sheet).getByRole("textbox", { name: "Port" });
  await user.clear(port);
  await user.type(port, "2580");
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(1));
  expect(facade.oneCall("SaveItem")[0]).toMatchObject({ kind: "environment", item: "qa" });
  expect(await screen.findByRole("dialog", { name: "Scheduling QA" })).toBeTruthy();
  expect(page().getByRole("button", { name: "Security", current: "page" })).toBeTruthy();
});

// ---------- Setup started from Security returns to its connection ----------

/** A new environment as the facade's validated defaults start it. */
const NEW_ENVIRONMENT: ItemDraft = {
  environment: {
    schema: "readmit-target/v3",
    name: "",
    test_endpoint: true,
    address: "",
    transport: "",
    approved_transport: false,
    classification: "unclassified",
    connect_timeout: "2s",
    message_timeout: "5s",
    max_ack_bytes: 65536,
  },
};

test("Add connection › Environment returns to its new connection after saving", async () => {
  const user = userEvent.setup();
  const NIGHTLY = connection({
    ref: "environment:env-new",
    name: "Nightly",
    kind: "environment",
    destination: "third-peer:2577",
    state: "not-checked",
    owner: { kind: "environment", object_id: "env-new" },
    actions: ["edit"],
  });
  const { facade } = await renderApp({
    SelectWorkspace: () => folderWithCase(),
    ListConnections: (context) => ({ state: "completed", context, rows: [QA] }),
    OpenItemDraft: (request) => ({ state: "completed", context: request.context, new: true, draft: NEW_ENVIRONMENT }),
    ListCredentials: (request) => ({ state: "completed", context: request.context, credentials: [], referring: [] }),
    SaveItem: (request) => ({ state: "completed", context: request.context, outcome: "saved", saved: { kind: "environment", id: "env-new", revision: "1" }, replayed: false, problems: [] }),
  });
  await openProject(user);
  await openSecurity(user);
  await user.click(await page().findByRole("button", { name: "Add connection" }));
  await user.click(await screen.findByRole("menuitem", { name: "Environment" }));
  const sheet = await screen.findByRole("dialog", { name: "Add environment" });
  await user.type(within(sheet).getByRole("textbox", { name: "Name" }), "Nightly");
  await user.type(within(sheet).getByRole("textbox", { name: "Host" }), "third-peer");
  await user.type(within(sheet).getByRole("textbox", { name: "Port" }), "2577");
  await user.click(within(sheet).getByRole("radio", { name: "TCP/MLLP" }));
  facade.reply({ ListConnections: (context) => ({ state: "completed", context, rows: [QA, NIGHTLY] }) });
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(1));
  const detail = await screen.findByRole("dialog", { name: "Nightly" });
  expect(within(detail).getByText("third-peer:2577")).toBeTruthy();
  expect(page().getByRole("heading", { level: 1, name: "Settings" })).toBeTruthy();
});

/** The saved Appointments observation, as the catalog lists it and its editor reads it. */
const APPOINTMENTS: CatalogItem = {
  ref: { kind: "observation", id: "appointments", revision: "rev-1" },
  name: "Appointments",
  created_at: null,
  updated_at: null,
  last_opened_at: null,
  availability: "available",
  capabilities: [],
  summary: { observation: { source_type: "file-export", enabled: true, latest_collection: null } },
};
const APPOINTMENTS_DRAFT: ItemDraft = {
  name: "Appointments",
  observation: {
    source: {
      schema: "readmit-observation-source/v1",
      source: { kind: "file-export", identity: "scheduling-archive", scope: "appointments" },
      enabled: true,
      freshness: { max_age: "1h" },
      extraction: { envelope: "csv", encoding: "utf-8", record_key: ["appointment"] },
      file: { path: "exports/appointments.csv", max_bytes: 65536 },
      http: null,
      capture: null,
    },
    window: {
      schema: "readmit-observation-window/v1",
      source: { kind: "file-export", identity: "scheduling-archive", scope: "appointments" },
      watermark: { kind: "none", position: "" },
      pre_existing_state: { declaration: "declared-empty", baseline_identity: "" },
      completion: { deadline: "30s", quiet_period: "2s", stable_samples: 3, max_records: 100, max_samples: 16 },
    },
  },
};

test("a source saved from Security leaves Environments on its read-only saved observation", async () => {
  const user = userEvent.setup();
  let saved = false;
  const { facade } = await renderApp({
    SelectWorkspace: () => folderWithCase(),
    ListConnections: (context) => ({ state: "completed", context, rows: saved ? [SOURCE, QA] : [QA] }),
    ListCatalog: (query) => ({ state: "completed", context: query.context, page: { items: query.kind === "observation" && saved ? [APPOINTMENTS] : [], total: query.kind === "observation" && saved ? 1 : 0, snapshot: "s", recorded: true, incomplete: [] } }),
    OpenItemDraft: (request) => ({ state: "completed", context: request.context, new: request.ref.id === "", ref: request.ref, draft: APPOINTMENTS_DRAFT }),
    ObservationHistory: (request) => ({ state: "completed", context: request.context, collections: [] }),
    ListCredentials: (request) => ({ state: "completed", context: request.context, credentials: [], referring: [] }),
    ObservationSupport: () => ({ state: "completed", support: [] }),
    ObservationFields: (request) => ({ state: "completed", context: request.context, fields: ["appointment", "status"] }),
    SaveItem: (request) => {
      saved = true;
      return { state: "completed", context: request.context, outcome: "saved", saved: APPOINTMENTS.ref, replayed: false, problems: [] };
    },
  });
  await openProject(user);
  await openSecurity(user);
  await user.click(page().getByRole("button", { name: "Add connection" }));
  await user.click(await screen.findByRole("menuitem", { name: "Source" }));
  const editor = within(await screen.findByRole("dialog", { name: "Add observation" }));
  await user.type(editor.getByLabelText("Name"), "Appointments");
  await user.click(editor.getByRole("button", { name: "Save" }));
  const selected = within(await screen.findByRole("dialog", { name: "Appointments" }));
  await user.click(selected.getByRole("button", { name: "Close appointments" }));
  const starts = facade.callsTo("OpenItemDraft").filter((call) => (call.args[0] as ItemRequest).ref.kind === "observation" && (call.args[0] as ItemRequest).ref.id === "").length;
  await goTo(user, "Environments");
  expect(await page().findByRole("heading", { level: 1, name: "Appointments" })).toBeTruthy();
  expect(screen.queryByRole("dialog", { name: "Add observation" })).toBeNull();
  expect(page().queryByRole("textbox", { name: "Name" })).toBeNull();
  expect(facade.callsTo("OpenItemDraft").filter((call) => (call.args[0] as ItemRequest).ref.kind === "observation" && (call.args[0] as ItemRequest).ref.id === "")).toHaveLength(starts);
  await user.click(page().getByRole("button", { name: "Back to environments" }));
  expect(await page().findByRole("heading", { level: 1, name: "Environments" })).toBeTruthy();
  expect(screen.queryByRole("dialog")).toBeNull();
  expect(facade.callsTo("SaveItem")).toHaveLength(1);
  expect(facade.callsTo("CheckEnvironment")).toHaveLength(0);
  expect(facade.callsTo("CollectObservation")).toHaveLength(0);
});

test("discarding a dirty source started from Security clears the remembered new-editor route", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    SelectWorkspace: () => folderWithCase(),
    ListConnections: (context) => ({ state: "completed", context, rows: [QA] }),
    OpenItemDraft: (request) => ({ state: "completed", context: request.context, new: true, draft: APPOINTMENTS_DRAFT }),
    ListCredentials: (request) => ({ state: "completed", context: request.context, credentials: [], referring: [] }),
    ObservationSupport: () => ({ state: "completed", support: [] }),
    ObservationFields: (request) => ({ state: "completed", context: request.context, fields: ["appointment", "status"] }),
  });
  await openProject(user);
  await openSecurity(user);
  await user.click(page().getByRole("button", { name: "Add connection" }));
  await user.click(await screen.findByRole("menuitem", { name: "Source" }));
  const editor = within(await screen.findByRole("dialog", { name: "Add observation" }));
  await user.type(editor.getByLabelText("Name"), "Unsaved source");
  await user.keyboard("{Escape}");
  await user.click(within(await screen.findByRole("dialog", { name: "Save changes?" })).getByRole("button", { name: "Keep editing" }));
  expect(editor.getByLabelText("Name")).toHaveProperty("value", "Unsaved source");
  await user.keyboard("{Escape}");
  await user.click(within(await screen.findByRole("dialog", { name: "Save changes?" })).getByRole("button", { name: "Discard" }));
  await page().findByRole("heading", { level: 1, name: "Settings" });
  const starts = facade.callsTo("OpenItemDraft").filter((call) => (call.args[0] as ItemRequest).ref.kind === "observation" && (call.args[0] as ItemRequest).ref.id === "").length;
  await goTo(user, "Environments");
  expect(await page().findByRole("heading", { level: 1, name: "Environments" })).toBeTruthy();
  expect(screen.queryByRole("dialog", { name: "Add observation" })).toBeNull();
  expect(facade.callsTo("OpenItemDraft").filter((call) => (call.args[0] as ItemRequest).ref.kind === "observation" && (call.args[0] as ItemRequest).ref.id === "")).toHaveLength(starts);
  expect(facade.callsTo("SaveItem")).toHaveLength(0);
  expect(facade.callsTo("CheckEnvironment")).toHaveLength(0);
  expect(facade.callsTo("CollectObservation")).toHaveLength(0);
});

test("Edit on a source connection opens its observation editor and returns to it after saving", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    SelectWorkspace: () => folderWithCase(),
    ListConnections: (context) => ({ state: "completed", context, rows: [SOURCE, QA] }),
    ListCatalog: (query) => {
      const items = query.kind === "observation" ? [APPOINTMENTS] : [];
      return { state: "completed", context: query.context, page: { items, total: items.length, snapshot: "s", recorded: true, incomplete: [] } };
    },
    OpenItemDraft: (request) => ({ state: "completed", context: request.context, new: false, ref: request.ref, draft: APPOINTMENTS_DRAFT }),
    ObservationHistory: (request) => ({ state: "completed", context: request.context, collections: [] }),
    ListCredentials: (request) => ({ state: "completed", context: request.context, credentials: [], referring: [] }),
    ObservationSupport: () => ({ state: "completed", support: [] }),
    ObservationFields: (request) => ({ state: "completed", context: request.context, fields: ["appointment", "status"] }),
    SaveItem: (request) => ({ state: "completed", context: request.context, outcome: "saved", saved: { kind: "observation", id: "appointments", revision: "rev-2" }, replayed: false, problems: [] }),
  });
  await openProject(user);
  const table = await openSecurity(user);
  await user.click(await within(table).findByText("Appointments"));
  await user.click(within(await screen.findByRole("dialog", { name: "Appointments" })).getByRole("button", { name: "Edit" }));
  const sheet = await screen.findByRole("dialog", { name: "Edit observation" });
  const size = await within(sheet).findByRole("textbox", { name: "Maximum size (bytes)" });
  await user.clear(size);
  await user.type(size, "4096");
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(1));
  expect(facade.oneCall("SaveItem")[0]).toMatchObject({ kind: "observation", item: "appointments" });
  const detail = await screen.findByRole("dialog", { name: "Appointments" });
  expect(within(detail).getByText("records.example.test")).toBeTruthy();
  expect(page().getByRole("heading", { level: 1, name: "Settings" })).toBeTruthy();
});

test("Add connection › Team chooses a hub configuration and returns to the Team hub", async () => {
  const user = userEvent.setup();
  let saved = false;
  const { facade } = await renderApp({
    ListConnections: (context) => ({ state: "completed", context, rows: saved ? [HUB, QA] : [QA] }),
    ChooseHubTeamConfig: () => ({ state: "completed", config: "/etc/readmit/hub-client.json", hub_url: "https://team.example.test", projects: ["cardio"], name: "team.example.test" }),
    SaveHubTeam: (request) => {
      saved = true;
      return { state: "completed", connected: false, authenticated: false, config_path: request.config, hub_url: "https://team.example.test", team: request.name };
    },
  });
  // Closing Connect team leaves the person on Team with nothing chosen.
  await openSecurity(user);
  await user.click(await page().findByRole("button", { name: "Add connection" }));
  await user.click(await screen.findByRole("menuitem", { name: "Team" }));
  let sheet = await screen.findByRole("dialog", { name: "Connect team" });
  await user.click(within(sheet).getByRole("button", { name: "Close connect team" }));
  expect(await page().findByRole("button", { name: "Team", current: "page" })).toBeTruthy();
  expect(page().queryByRole("table", { name: "Connections" })).toBeNull();
  expect(facade.callsTo("SaveHubTeam")).toHaveLength(0);

  // A saved team returns to Security with the Team hub open.
  await openSecurity(user);
  await user.click(await page().findByRole("button", { name: "Add connection" }));
  await user.click(await screen.findByRole("menuitem", { name: "Team" }));
  sheet = await screen.findByRole("dialog", { name: "Connect team" });
  await user.click(within(sheet).getByRole("button", { name: "Choose file…" }));
  await user.click(await within(sheet).findByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveHubTeam").length).toBe(1));
  const detail = await screen.findByRole("dialog", { name: "Team hub" });
  expect(within(detail).getByText("team.example.test")).toBeTruthy();
  expect(page().getByRole("button", { name: "Security", current: "page" })).toBeTruthy();
});

test("Add connection › Runner returns to the runner once it is added and admitted", async () => {
  const user = userEvent.setup();
  const RUNNER = connection({
    ref: "runner:config",
    name: "Runner lab",
    kind: "runner",
    destination: "hub.example.test:8443",
    state: "not-checked",
    owner: { kind: "runner" },
    disclosure: "runner",
    actions: ["edit"],
    detail: { signed_in: false, config_path: "/etc/readmit-runner/config.json" },
  });
  const saved = { kind: "runner" as const, id: "r1", revision: "1" };
  const { facade } = await renderApp({
    SelectWorkspace: () => folderWithCase(),
    OperationStatus: () => ({ state: "completed", selected: true, author_seats: 1, runner_instances: 2 }),
    ListConnections: (context) => ({ state: "completed", context, rows: facade.callsTo("EnrollRunner").length > 0 ? [RUNNER, QA] : [QA] }),
    ListRunners: (context) => ({
      state: "completed", context,
      runners: facade.callsTo("SaveItem").length > 0 ? [{ ref: saved, name: "Runner lab", config: "/p/runner.json", environment: "lab", hub_environment: "lab", hub: "hub.example.test:8443", project: "alpha", root: "/r", status: "not-checked", active_jobs: 0, local: true }] : [],
    }),
    ChooseRunnerPath: (kind) => ({ state: "completed", kind, paths: [`/etc/readmit-runner/${kind}`] }),
    PreviewRunnerConfig: () => ({ state: "completed" }),
    SaveItem: (request) => ({ state: "completed", context: request.context, outcome: "saved", saved, replayed: false, problems: [] }),
    EnrollRunner: () => ({ state: "completed", project: "alpha", environment: "lab", expires_at: "2026-09-30T08:00:10Z", max_seconds: 600, max_jobs: 10 }),
  });
  await openProject(user);
  await openSecurity(user);
  await user.click(await page().findByRole("button", { name: "Add connection" }));
  await user.click(await screen.findByRole("menuitem", { name: "Runner" }));
  const sheet = await screen.findByRole("dialog", { name: "Add runner" });
  await user.type(within(sheet).getByLabelText("Name"), "Runner lab");
  await user.type(within(sheet).getByLabelText("Customer hub"), "https://hub.example.test:8443");
  await user.type(within(sheet).getByLabelText("Hub project"), "alpha");
  for (const label of ["hub certificate authority", "runner certificate", "key reader", "token reader"]) await user.click(within(sheet).getByRole("button", { name: `Choose ${label}` }));
  await user.type(within(sheet).getByLabelText("Deployment key"), "key");
  await user.type(within(sheet).getByLabelText("Approved build"), "NEXT");
  await user.click(within(sheet).getByRole("button", { name: "Next" }));
  await user.type(within(sheet).getByLabelText("Hub environment"), "lab");
  await user.click(within(sheet).getByRole("button", { name: "Choose working folder" }));
  await user.click(within(sheet).getByRole("button", { name: "Next" }));
  await user.click(within(sheet).getByRole("button", { name: "Request admission" }));
  await waitFor(() => expect(facade.callsTo("EnrollRunner")).toHaveLength(1));
  const detail = await screen.findByRole("dialog", { name: "Runner lab" });
  expect(within(detail).getByText("hub.example.test:8443")).toBeTruthy();
  expect(page().getByRole("button", { name: "Security", current: "page" })).toBeTruthy();
});

test("Customer portal Edit opens Account portal in Administrator setup, and Save returns to the portal", async () => {
  const user = userEvent.setup();
  const PORTAL = connection({
    ref: "portal",
    name: "Customer portal",
    kind: "portal",
    destination: "Browser",
    state: "not-checked",
    owner: { kind: "license" },
    disclosure: "portal",
    actions: ["edit"],
    detail: { signed_in: false, config_path: "/etc/readmit/destinations.json" },
  });
  const { facade } = await renderApp({
    ListConnections: (context) => ({ state: "completed", context, rows: [PORTAL, QA] }),
    ReviewCommercialDestinations: () => ({ state: "completed", environment: "sandbox", portal: "https://portal.example.test", config_path: "/etc/readmit/destinations.json" }),
    SaveCommercialDestinations: (request) => ({ state: "completed", environment: "sandbox", portal: "https://portal.example.test", config_path: request.path }),
  });
  const table = await openSecurity(user);
  await user.click(await within(table).findByText("Customer portal"));
  await user.click(within(await screen.findByRole("dialog", { name: "Customer portal" })).getByRole("button", { name: "Edit" }));
  const sheet = within(await screen.findByRole("dialog", { name: "Account portal" }));
  await user.click(sheet.getByRole("button", { name: "Choose file" }));
  await user.click(sheet.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.oneCall("SaveCommercialDestinations")).toEqual([{ path: "/etc/readmit/destinations.json", portal: "https://portal.example.test" }]));
  const detail = await screen.findByRole("dialog", { name: "Customer portal" });
  expect(within(detail).getByText("/etc/readmit/destinations.json")).toBeTruthy();
  expect(page().getByRole("button", { name: "Security", current: "page" })).toBeTruthy();
});

// ---------- General at every size, and what Security holds ----------

test("choosing a text size keeps focus on Text size, and Cancel returns focus to Edit", async () => {
  const user = userEvent.setup();
  await renderApp();
  await goToView(user, "Settings", "General");
  const edit = page().getByRole("button", { name: "Edit" });
  await user.click(edit);
  const sheet = await screen.findByRole("dialog", { name: "General" });
  const size = within(sheet).getByLabelText("Text size");
  await user.selectOptions(size, "200");
  expect(document.documentElement.style.getPropertyValue("--text-scale")).toBe("2");
  expect(document.activeElement).toBe(size);
  await user.click(within(sheet).getByRole("button", { name: "Cancel" }));
  await user.click(await screen.findByRole("button", { name: "Discard" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "General" })).toBeNull());
  expect(document.documentElement.style.getPropertyValue("--text-scale")).toBe("1");
  await waitFor(() => expect(document.activeElement).toBe(page().getByRole("button", { name: "Edit" })));
});

test("with text size 200% Settings still offers every category", async () => {
  const user = userEvent.setup();
  await renderApp({ ReadPreferences: () => ({ state: "completed", preferences: { theme: "system", text_scale: 200 } }) });
  await goToView(user, "Settings", "General");
  await waitFor(() => expect(document.documentElement.style.getPropertyValue("--text-scale")).toBe("2"));
  for (const category of ["General", "License", "Team", "Runners", "Security", "Storage"]) {
    await goToView(user, "Settings", category);
    expect(await page().findByRole("button", { name: category, current: "page" })).toBeTruthy();
  }
});

test("a failed privacy save keeps the sheet and its values, and the sheet names the project", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    SelectWorkspace: () => folderWithCase(),
    ListSearchSettings: () => ({
      state: "completed",
      cases: [{ case: "cancel-case", identity: "id-2", settings: { fields: ["SCH-1"], retention: "states", retain_until: null, expired: false } }],
    }),
    SaveSearchSettings: () => ({ state: "failed", reason: "The saved search settings cannot be written." }),
  });
  await openProject(user);
  await goToView(user, "Settings", "Security");
  await user.click(await page().findByRole("button", { name: "Edit" }));
  const sheet = await screen.findByRole("dialog", { name: "Privacy" });
  const project = Array.from(sheet.querySelectorAll("dt")).find((term) => term.textContent === "Project");
  expect(project?.nextElementSibling?.textContent).toBe("workspace-under-test");
  await user.type(within(sheet).getByLabelText("Field 1"), "-2");
  await user.selectOptions(within(sheet).getByLabelText("Stored as"), "digests");
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  expect(await within(sheet).findByText("The saved search settings cannot be written.")).toBeTruthy();
  expect(facade.callsTo("SaveSearchSettings")).toHaveLength(1);
  expect(screen.getByRole("dialog", { name: "Privacy" })).toBe(sheet);
  expect((within(sheet).getByLabelText("Field 1") as HTMLInputElement).value).toBe("SCH-1-2");
  expect((within(sheet).getByLabelText("Stored as") as HTMLSelectElement).value).toBe("digests");
  expect(within(sheet).getByRole("radio", { name: "Indefinitely" })).toHaveProperty("checked", true);
});

test("an active connection is still Active after going to Environments and back", async () => {
  const user = userEvent.setup();
  const active = connection({ ...QA, state: "active", checked_at: null, detail: { signed_in: false, operation: "target-check" } });
  await renderApp({ SelectWorkspace: () => folderWithCase(), ListConnections: (context) => ({ state: "completed", context, rows: [active, SOURCE] }) });
  await openProject(user);
  let table = await openSecurity(user);
  await waitFor(() => expect(rowsOf(table)[0]).toEqual(["Scheduling QA", "qa.example.test:2575", "Active"]));
  await goTo(user, "Environments");
  expect(await page().findByRole("heading", { level: 1, name: "Environments" })).toBeTruthy();
  table = await openSecurity(user);
  await waitFor(() => expect(rowsOf(table)[0]).toEqual(["Scheduling QA", "qa.example.test:2575", "Active"]));
});

test("Security holds its connections, privacy values and Encryption, and no Team, Runners, License or Protection panel", async () => {
  const user = userEvent.setup();
  await renderApp({ SelectWorkspace: () => folderWithCase(), ListConnections: (context) => ({ state: "completed", context, rows: [QA] }) });
  await openProject(user);
  await openSecurity(user);
  for (const name of ["Team", "Runners", "Runner", "License", "Protection", "Encrypted packages"]) {
    expect(page().queryByRole("region", { name })).toBeNull();
  }
  expect(page().getByRole("list", { name: "Encryption" })).toBeTruthy();
});
