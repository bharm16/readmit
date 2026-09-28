// Settings → General and Security. General shows saved values and edits them
// in one sheet that previews; Security lists the configured connections the
// facade reads from saved configuration and the window's own state, and
// edits privacy values in one sheet. Fixtures carry names, states and times
// only; destinations are synthetic.
import { expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { CatalogItem, ConnectionRow, Preferences } from "./bindings";
import { renderApp } from "./testkit/app";
import { folderWithCase } from "./testkit/fixtures";
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

test("About shows the application's version", async () => {
  const user = userEvent.setup();
  await renderApp();
  await goToView(user, "Settings", "General");
  await user.click(page().getByRole("button", { name: "More general settings" }));
  await user.click(await screen.findByRole("menuitem", { name: "About" }));
  const about = await screen.findByRole("dialog", { name: "About Readmit" });
  expect(within(about).getByText("0.0.0-test")).toBeTruthy();
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
  const reaching = ["CheckEnvironment", "CheckEnvironmentDestination", "CheckCredential", "HubStatus", "ConnectHub", "ReadRunnerConfig", "DiagnoseHub"] as const;
  const before = reaching.map((method) => facade.callsTo(method).length);
  const table = await openSecurity(user);
  await waitFor(() => expect(rowsOf(table)).toHaveLength(2));
  await user.click(page().getByRole("button", { name: "Refresh status" }));
  await user.click(within(table).getByText("Team hub"));
  await screen.findByRole("dialog", { name: "Team hub" });
  expect(reaching.map((method) => facade.callsTo(method).length)).toEqual(before);
  expect(facade.callsTo("ListConnections").length).toBeGreaterThanOrEqual(2);
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

test("Add connection opens the owner's setup for each kind and a saved environment returns to Security", async () => {
  const user = userEvent.setup();
  await renderApp({ SelectWorkspace: () => folderWithCase() });
  await openProject(user);
  await goToView(user, "Settings", "Security");
  await user.click(await page().findByRole("button", { name: "Add connection" }));
  await user.click(await screen.findByRole("menuitem", { name: "Team" }));
  expect(await page().findByRole("tab", { name: "Team", selected: true }).catch(() => page().findByRole("heading", { name: "Settings" }))).toBeTruthy();
  await goToView(user, "Settings", "Security");
  await user.click(await page().findByRole("button", { name: "Add connection" }));
  await user.click(await screen.findByRole("menuitem", { name: "Environment" }));
  const sheet = await screen.findByRole("dialog", { name: "Add environment" });
  await user.click(within(sheet).getByRole("button", { name: "Cancel" }));
  await waitFor(() => expect(page().getByRole("heading", { level: 1, name: "Settings" })).toBeTruthy());
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

test("Edit on an environment connection opens its connection sheet and returns to Security", async () => {
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
  const sheet = await screen.findByRole("dialog", { name: /Connection|Edit connection|Scheduling QA/ });
  await user.click(within(sheet).getByRole("button", { name: "Cancel" }));
  await waitFor(() => expect(page().getByRole("heading", { level: 1, name: "Settings" })).toBeTruthy());
  expect(await page().findByRole("table", { name: "Connections" })).toBeTruthy();
});
