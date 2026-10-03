// Share report (#560): Contents → Redaction → Preview as one started flow over
// one report version; the preview is the exact generated output, a changed
// choice withdraws it, and Export or Send acts on it once. Support summaries,
// templates and encrypted packages go through the same review lifecycle.
// Fixtures carry names, states and synthetic tokens only.
import { expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ActionReview, CatalogItem, CatalogQuery, PrepareActionRequest, ReportShareReview, ReportView } from "./bindings";
import { renderApp } from "./testkit/app";
import { CASE_ENTRY, caseCatalogItem, catalogOfListing, folderChosen, WORKSPACE_ROOT } from "./testkit/fixtures";
import { goTo, page, sidebar } from "./testkit/navigation";
import type { FacadeHandlers, FacadeStub } from "./testkit/wails";

type User = ReturnType<typeof userEvent.setup>;

const CASE = caseCatalogItem(CASE_ENTRY);
const REPORT: CatalogItem = {
  ref: { kind: "report", id: "rep-regression" },
  name: "Reschedule regression",
  created_at: null,
  updated_at: "2026-01-03T10:00:00Z",
  last_opened_at: null,
  availability: "available",
  capabilities: [],
  summary: { report: { form: "report", related_case: CASE.ref, status: "draft" } },
};

function view(): ReportView {
  const result = { outcome: "failed" as const, status: "assertion_failure", error_class: "", run_state: "", journal_incomplete: false, delivery_uncertain: false };
  return {
    item: REPORT,
    form: "report",
    revision: "2",
    current: true,
    title: "Reschedule regression",
    result,
    runs: [],
    checks: [],
    messages: [],
    limitations: [],
    evidence: [],
    packet: "packet-token",
    versions: [],
    review: "draft",
    revealed: false,
    shares: [],
  };
}

const MARKDOWN = "# Shared regression\n\nFailed\n";

function share(overrides: Partial<ReportShareReview> = {}): ReportShareReview {
  return {
    report: "Reschedule regression",
    version: "2",
    items: [
      { key: "report", name: "Reschedule regression", type: "report", included: true, removable: false },
      { key: "notes", name: "Notes", type: "notes", included: true, removable: true },
    ],
    rows: [
      { key: "notes", kind: "free-text", category: "free-text", field: "Notes", occurrences: 1, treatment: "none", result: "unresolved" },
      { key: "field:PID[1]-5[1]", kind: "field", category: "names", field: "PID-5", selector: "PID[1]-5[1]", occurrences: 2, treatment: "remove", result: "removed", rule: { selector: "PID[1]-5[1]", policy: "remove-field/v1", class: "names" } },
    ],
    issues: [{ key: "notes", text: "Notes is unresolved" }],
    output: { type: "file", format: "markdown", name: "Shared regression.md", size: MARKDOWN.length, files: [{ name: "Shared regression.md", kind: "report", size: MARKDOWN.length, text: MARKDOWN }] },
    destination: { kind: "local" },
    source_values: true,
    redacted: false,
    revealed: false,
    consequence: "Exports a file containing patient data.",
    encrypted: false,
    templates: [{ entry: "Clinic A.json", name: "Clinic A" }],
    controls: [],
    projects: [],
    attachments: 0,
    ...overrides,
  };
}

function reviewed(request: PrepareActionRequest, display: ReportShareReview, ready: boolean): ActionReview {
  return {
    ...(ready ? { token: `token-${JSON.stringify(request.report_share ?? {}).length}` } : {}),
    action: request.action,
    consent: request.action === "report.send" ? "upload" : "export",
    items: [REPORT],
    destination: {},
    requirements: [],
    ready,
    ...(ready ? {} : { refusal: "choose where the output is written" }),
    report_share: display,
  };
}

function listing(facade: FacadeStub) {
  facade.reply({
    ListCatalog: (query: CatalogQuery) =>
      query.kind === "report"
        ? { state: "completed", context: query.context, page: { items: [REPORT], total: 1, snapshot: "s", recorded: true, incomplete: [] } }
        : catalogOfListing(query, facade),
  });
}

async function openReport(user: User, handlers: FacadeHandlers) {
  const rendered = await renderApp({
    SelectWorkspace: () => folderChosen(WORKSPACE_ROOT, [{ name: CASE_ENTRY, kind: "case", schema: "readmit-case/v3", provenance: "generated" }]),
    OpenReport: (request) => ({ state: "completed", context: request.context, report: view() }),
    ...handlers,
  });
  listing(rendered.facade);
  await goTo(user, "Projects");
  await user.click(screen.getByRole("button", { name: "Open" }));
  await sidebar().findByRole("button", { name: /^Project: / });
  await goTo(user, "Reports");
  await user.click(await page().findByRole("row", { name: /Reschedule regression/ }));
  await user.keyboard("{Enter}");
  await page().findByRole("heading", { level: 1, name: "Reschedule regression" });
  return rendered;
}

function prepared(facade: FacadeStub, action = "report.share"): PrepareActionRequest[] {
  return facade.callsTo("PrepareAction").map((call) => call.args[0] as PrepareActionRequest).filter((request) => request.action === action);
}

test("Share report prepares the actual share at each step and exports exactly the previewed output once", async () => {
  const user = userEvent.setup();
  const { facade } = await openReport(user, {
    PrepareAction: (request) => ({ state: "completed", context: request.context, review: reviewed(request, share(), request.report_share?.destination !== undefined) }),
    ChooseShareDestination: (request) => ({ state: "completed", context: request.context, destination: "place-1", name: "Shared regression.md", location: "Exports" }),
    ExecuteReviewedAction: (request) => ({
      state: "completed",
      context: request.context,
      outcome: "completed",
      replayed: false,
      report_share: { name: "Shared regression.md", output: "output-1" },
    }),
    OpenSharedOutput: () => ({ state: "completed", context: { project: "", generation: 0 } }),
  });
  await user.click(page().getByRole("button", { name: "Share" }));
  await page().findByRole("heading", { level: 1, name: "Share report" });
  const steps = page().getByRole("list", { name: "Steps" });
  expect(within(steps).getByText("Contents").getAttribute("aria-current")).toBe("step");
  const contents = await page().findByRole("table", { name: "Contents" });
  expect(within(contents).getAllByText("Notes")).toHaveLength(2);
  expect(page().queryByRole("textbox")).toBeNull();
  await user.click(page().getByRole("checkbox", { name: "Selected messages" }));
  await waitFor(() => expect(prepared(facade).at(-1)!.report_share).toMatchObject({ contents: { messages: true }, format: "pdf", paper: "letter" }));
  await user.click(within(contents).getByRole("button", { name: "Remove Notes" }));
  await waitFor(() => expect(prepared(facade).at(-1)!.report_share!.contents.removed).toEqual(["notes"]));
  expect(facade.callsTo("SaveEditorDraft").length).toBeGreaterThan(0);
  expect(JSON.stringify(facade.callsTo("SaveEditorDraft").at(-1)!.args[0])).not.toContain("token");

  // Format is the first thing on this output that changes here.
  await user.click(page().getAllByRole("button", { name: "Change" })[0]!);
  const format = await screen.findByRole("dialog", { name: "Format" });
  await user.click(within(format).getByRole("radio", { name: "Markdown" }));
  await user.click(within(format).getByRole("button", { name: "Apply" }));
  await waitFor(() => expect(prepared(facade).at(-1)!.report_share!.format).toBe("markdown"));
  await user.click(page().getByRole("button", { name: "Redaction" }));
  const rows = await page().findByRole("table", { name: "Redaction" });
  expect(within(rows).getByText("Free text · Notes")).toBeTruthy();
  expect(within(rows).getByText("Unresolved")).toBeTruthy();
  await user.click(page().getByRole("button", { name: "Preview" }));
  expect(await page().findByText("Notes is unresolved")).toBeTruthy();
  expect(page().getByLabelText("Shared regression.md").textContent).toBe(MARKDOWN);
  expect(page().getByRole("button", { name: "Export" }).hasAttribute("disabled")).toBe(true);
  await user.click(page().getByRole("button", { name: "Choose" }));
  await waitFor(() => expect(prepared(facade).at(-1)!.report_share!.destination).toBe("place-1"));
  expect(await page().findByText("Shared regression.md · Exports")).toBeTruthy();
  expect(page().getByText("Exports a file containing patient data.")).toBeTruthy();
  await waitFor(() => expect(page().getByRole("button", { name: "Export" }).hasAttribute("disabled")).toBe(false));
  expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
  await user.click(page().getByRole("button", { name: "Export" }));
  await waitFor(() => expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(1));
  expect(await page().findByText("Exported Shared regression.md")).toBeTruthy();
  await user.click(page().getByRole("button", { name: "Show in folder" }));
  expect(facade.oneCall("OpenSharedOutput")[0]).toEqual({ output: "output-1", folder: true });
  expect(facade.callsTo("DiscardEditorDraft").length + facade.callsTo("SaveEditorDraft").length).toBeGreaterThan(0);
});

test("Export opens the preview of the report alone, and a changed choice withdraws the preview before its Export can be pressed", async () => {
  const user = userEvent.setup();
  let release: (() => void) | null = null;
  const { facade } = await openReport(user, {
    PrepareAction: async (request) => {
      if (request.report_share?.format === "html") await new Promise<void>((resolve) => (release = resolve));
      return { state: "completed", context: request.context, review: reviewed(request, share(), true) };
    },
    ExecuteReviewedAction: (request) => ({ state: "failed", context: request.context, outcome: "stale", replayed: false, reason: "the report changed; review it again" }),
  });
  await user.click(page().getByRole("button", { name: "Export" }));
  const steps = await page().findByLabelText("Steps",{selector:"ol"});
  expect(within(steps).getByText("Preview").getAttribute("aria-current")).toBe("step");
  await waitFor(() => expect(prepared(facade).at(-1)!.report_share).toMatchObject({ contents: {}, format: "pdf" }));
  expect(prepared(facade).at(-1)!.report_share!.template).toBeUndefined();
  await waitFor(() => expect(page().getByRole("button", { name: "Export" }).hasAttribute("disabled")).toBe(false));
  await user.click(page().getByRole("button", { name: "Back" }));
  await user.click(page().getByRole("button", { name: "Back" }));
  await user.click(page().getAllByRole("button", { name: "Change" })[0]!);
  const format = await screen.findByRole("dialog", { name: "Format" });
  await user.click(within(format).getByRole("radio", { name: "HTML" }));
  await user.click(within(format).getByRole("button", { name: "Apply" }));
  await user.click(page().getByRole("button", { name: "Redaction" }));
  await user.click(page().getByRole("button", { name: "Preview" }));
  // The earlier preview is withdrawn while the changed one is prepared.
  expect(page().getByRole("button", { name: "Export" }).hasAttribute("disabled")).toBe(true);
  await waitFor(() => expect(release).not.toBeNull());
  release!();
  await waitFor(() => expect(page().getByRole("button", { name: "Export" }).hasAttribute("disabled")).toBe(false));
  await user.click(page().getByRole("button", { name: "Export" }));
  expect(await page().findByText("the report changed; review it again")).toBeTruthy();
});

test("a redaction row's treatment and a saved template change only this share's draft, and Show values shows examples on request", async () => {
  const user = userEvent.setup();
  const { facade } = await openReport(user, {
    PrepareAction: (request) => {
      const shown = share(request.report_share?.reveal ? { revealed: true, rows: share().rows.map((row) => ({ ...row, examples: [{ original: "DOE^JANE", derived: "" }] })) } : {});
      return { state: "completed", context: request.context, review: reviewed(request, shown, false) };
    },
    SaveShareTemplate: (request) => ({ state: "completed", context: request.context, template: { entry: "Clinic B.json", name: "Clinic B" } }),
  });
  await user.click(page().getByRole("button", { name: "Share" }));
  await user.click(await page().findByRole("button", { name: "Redaction" }));
  const rows = await page().findByRole("table", { name: "Redaction" });
  await user.click(within(rows).getByRole("row", { name: /Notes/ }));
  await user.keyboard("{Enter}");
  const sheet = await screen.findByRole("dialog", { name: "Notes" });
  await user.click(within(sheet).getByRole("radio", { name: "Replace" }));
  await user.type(within(sheet).getByLabelText("Replacement"), "Seen in QA");
  await user.click(within(sheet).getByRole("button", { name: "Apply" }));
  await waitFor(() => expect(prepared(facade).at(-1)!.report_share!.overrides).toEqual({ notes: { row: "notes", replace: "Seen in QA" } }));

  await user.click(within(rows).getByRole("row", { name: /PID-5/ }));
  await user.keyboard("{Enter}");
  const field = await screen.findByRole("dialog", { name: "PID-5" });
  await user.click(within(field).getByRole("radio", { name: "Scoped surrogate" }));
  await user.type(within(field).getByLabelText("Scope"), "patient");
  await user.click(within(field).getByRole("button", { name: "Apply" }));
  await waitFor(() =>
    expect(prepared(facade).at(-1)!.report_share!.overrides.fields).toEqual([{ selector: "PID[1]-5[1]", policy: "scoped-surrogate/v1", class: "names", scope: "patient", authority: [] }]),
  );

  await user.click(page().getByRole("button", { name: "Show values" }));
  await waitFor(() => expect(prepared(facade).at(-1)!.report_share!.reveal).toBe(true));
  expect(await within(await page().findByRole("table", { name: "Redaction" })).findAllByText("DOE^JANE → Removed")).not.toHaveLength(0);

  await user.click(page().getByRole("button", { name: "Save template" }));
  const save = await screen.findByRole("dialog", { name: "Save template" });
  await user.type(within(save).getByLabelText("Name"), "Clinic B");
  await user.click(within(save).getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveShareTemplate")).toHaveLength(1));
  expect(facade.oneCall("SaveShareTemplate")[0]).toMatchObject({ name: "Clinic B", report: REPORT.ref, overrides: { notes: { row: "notes", replace: "Seen in QA" } } });
  await waitFor(() => expect(prepared(facade).at(-1)!.report_share).toMatchObject({ template: "Clinic B.json", overrides: { notes: { row: "notes", replace: "Seen in QA" } } }));
  expect(prepared(facade).at(-1)!.report_share!.overrides.fields).toBeUndefined();
  expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
});

test("a template that requires a check keeps the share blocked, and Run check hands the prepared check to its reviewed run", async () => {
  const user = userEvent.setup();
  const { facade } = await openReport(user, {
    PrepareAction: (request) => {
      if (request.action === "report.prepare-check") {
        return {
          state: "completed",
          context: request.context,
          review: {
            token: "check-token",
            action: "report.prepare-check",
            consent: "derive",
            items: [REPORT],
            destination: {},
            requirements: ["inventory-declaration"],
            ready: true,
            share_check: { report: "Reschedule regression", phase: "failure", messages: 2, inventory: { entry: "Reschedule regression", artifacts: [{ kind: "result", path: "current" }], residual_values: 0, digest: "inventory-token" } },
          },
        };
      }
      if (request.action === "run.reviewed-test") {
        return { state: "completed", context: request.context, review: { action: "run.reviewed-test", consent: "send", items: [], destination: {}, requirements: [], ready: false, refusal: "choose an environment" } };
      }
      return { state: "completed", context: request.context, review: reviewed(request, share({ run_check: { satisfied: false, reason: "run the check the template requires" } }), false) };
    },
    ExecuteReviewedAction: (request) => ({
      state: "completed",
      context: request.context,
      outcome: "completed",
      replayed: false,
      share_check: { review: { kind: "report", id: "rep-review" }, packet: REPORT.ref, phase: "failure" },
    }),
  });
  await user.click(page().getByRole("button", { name: "Share" }));
  await user.click(await page().findByRole("button", { name: "Redaction" }));
  expect(await page().findByText("Not run")).toBeTruthy();
  await user.click(page().getByRole("button", { name: "Run check" }));
  const sheet = await screen.findByRole("dialog", { name: "Run check" });
  await within(sheet).findByText("The failed checks");
  const run = within(sheet).getByRole("button", { name: "Run check" });
  expect(run.hasAttribute("disabled")).toBe(true);
  await user.click(within(sheet).getByRole("checkbox", { name: "Contents reviewed" }));
  await user.click(run);
  await waitFor(() => expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(1));
  expect(facade.oneCall("ExecuteReviewedAction")[0]).toMatchObject({ token: "check-token", decisions: { declared_inventory: "inventory-token" } });
  await waitFor(() => expect(prepared(facade, "run.reviewed-test")).toHaveLength(1));
  expect(prepared(facade, "run.reviewed-test")[0]).toMatchObject({ items: [{ kind: "report", id: "rep-review" }, REPORT.ref], run: { phase: "failure" } });
  expect(await screen.findByRole("dialog", { name: "Run check" })).toBeTruthy();
});

test("a Send names the team project it reaches, and the destination sheet offers only signed-in team projects", async () => {
  const user = userEvent.setup();
  const { facade } = await openReport(user, {
    PrepareAction: (request) => {
      const send = request.action === "report.send";
      const shown = share({ projects: ["clinic"], ...(send ? { destination: { kind: "hub", team: "Integration team", project: "clinic" }, consequence: "Sends the reviewed report to Integration team.", redacted: true, source_values: false, issues: [] } : {}) });
      return { state: "completed", context: request.context, review: reviewed(request, shown, send) };
    },
  });
  await user.click(page().getByRole("button", { name: "Share" }));
  await page().findByRole("table", { name: "Contents" });
  await user.click(page().getAllByRole("button", { name: "Change" })[0]!);
  const destination = await screen.findByRole("dialog", { name: "Destination" });
  await user.click(within(destination).getByRole("radio", { name: "clinic" }));
  await user.click(within(destination).getByRole("button", { name: "Apply" }));
  await waitFor(() => expect(prepared(facade, "report.send").at(-1)!.report_share).toMatchObject({ project: "clinic" }));
  expect(prepared(facade, "report.send").at(-1)!.report_share!.destination).toBeUndefined();
  await user.click(page().getByRole("button", { name: "Redaction" }));
  await user.click(page().getByRole("button", { name: "Preview" }));
  expect(await page().findByText("Sends the reviewed report to Integration team.")).toBeTruthy();
  await waitFor(() => expect(page().getByRole("button", { name: "Send" }).hasAttribute("disabled")).toBe(false));
});

test("a support summary is previewed under the project's policy, the policy is its own sheet, and Export support summary writes it once", async () => {
  const user = userEvent.setup();
  let allowed = false;
  const summary = {
    source_kind: "retained-packet",
    source_identity: "source-token",
    input_commitment: "case-token",
    spec_identity: "test-token",
    policy_identity: "policy-token",
    outcome: "assertion_failure",
    external_equivalence: "declined",
    scope: "Diagnostic metadata only.",
    identity: "summary-token",
    max_bytes: 4096,
    within_policy: true,
  };
  const { facade } = await openReport(user, {
    PrepareAction: (request) => ({
      state: "completed",
      context: request.context,
      review: {
        ...(allowed && request.support?.destination ? { token: "support-token" } : {}),
        action: "report.support",
        consent: "export",
        items: [REPORT],
        destination: {},
        requirements: [],
        ready: allowed && request.support?.destination !== undefined,
        ...(allowed ? {} : { refusal: "set the sharing policy first" }),
        report_support: {
          report: "Reschedule regression",
          ...(allowed ? { policy: { entry: "sharing-policy.json", schema: "readmit-sharing-policy/v1", support: true, destinations: ["local-file"], max_bytes: 4096 }, summary, size: 512 } : { size: 0 }),
          destination: request.support?.destination ? { kind: "local", name: "Support summary", location: "Exports" } : { kind: "local" },
          hub_allowed: false,
        },
      },
    }),
    SaveProjectSharingPolicy: () => {
      allowed = true;
      return { state: "completed", policy: { entry: "sharing-policy.json", schema: "readmit-sharing-policy/v1", support: true, destinations: ["local-file"], max_bytes: 4096 } };
    },
    ChooseShareDestination: (request) => ({ state: "completed", context: request.context, destination: "place-2", name: "Support summary", location: "Exports" }),
    ExecuteReviewedAction: (request) => ({ state: "completed", context: request.context, outcome: "completed", replayed: false, support: { name: "Support summary", output: "output-2" } }),
  });
  await user.click(page().getByRole("button", { name: "More report actions" }));
  await user.click(screen.getByRole("menuitem", { name: "Support summary" }));
  const sheet = await screen.findByRole("dialog", { name: "Support summary" });
  expect(await within(sheet).findByText("set the sharing policy first")).toBeTruthy();
  await user.click(within(sheet).getAllByRole("button", { name: "Change" }).at(-1)!);
  const policy = await screen.findByRole("dialog", { name: "Sharing policy" });
  expect(within(policy).getByRole("radio", { name: "Allowed" })).toBeTruthy();
  expect((within(policy).getByLabelText("Maximum size (bytes)") as HTMLInputElement).value).toBe("4096");
  await user.click(within(policy).getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveProjectSharingPolicy")).toHaveLength(1));
  expect(facade.oneCall("SaveProjectSharingPolicy")[0]).toMatchObject({ support: true, destinations: ["local-file"], max_bytes: 4096 });
  expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
  const again = await screen.findByRole("dialog", { name: "Support summary" });
  expect(await within(again).findByText("source-token")).toBeTruthy();
  await user.click(within(again).getByRole("button", { name: "Choose" }));
  await waitFor(() => expect(within(again).getByRole("button", { name: "Export support summary" }).hasAttribute("disabled")).toBe(false));
  await user.click(within(again).getByRole("button", { name: "Export support summary" }));
  await waitFor(() => expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(1));
  expect(facade.oneCall("ExecuteReviewedAction")[0]).toMatchObject({ token: "support-token" });
  expect(await within(again).findByText("Exported Support summary")).toBeTruthy();
});

test("encrypted packages are listed, decrypted to a chosen place, and deleted only with an override and a reason", async () => {
  const user = userEvent.setup();
  const PACKAGE = { entry: "protected-001", created_at: "2026-01-04T10:00:00Z", retention: "within-retention", retain_until: "2026-02-04T10:00:00Z", control: "lab-evidence", generation: 2, entries: 1 };
  const { facade } = await renderApp({
    SelectWorkspace: () => folderChosen(WORKSPACE_ROOT, [{ name: CASE_ENTRY, kind: "case", schema: "readmit-case/v3", provenance: "generated" }]),
    ListEncryptedPackages: (context) => ({ state: "completed", context, packages: [PACKAGE] }),
    ChooseShareDestination: (request) => ({ state: "completed", context: request.context, destination: "place-3", name: "protected-001 decrypted", location: "Exports" }),
    PrepareAction: (request) => {
      const decrypt = request.action === "package.decrypt";
      const ready = decrypt ? request.package?.destination !== undefined : request.package?.override === true;
      return {
        state: "completed",
        context: request.context,
        review: {
          ...(ready ? { token: `${request.action}-token` } : {}),
          action: request.action,
          consent: decrypt ? "export" : "delete",
          items: [],
          destination: {},
          requirements: !decrypt && request.package?.override ? ["rationale"] : [],
          ready,
          ...(ready ? {} : { refusal: decrypt ? "choose where the decrypted copy is written" : "the package is retained; override its retention to delete it" }),
          package_action: { package: PACKAGE, destination: request.package?.destination ? { kind: "local", name: "protected-001 decrypted", location: "Exports" } : { kind: "local" }, files: 3, override: request.package?.override ?? false, limitations: [] },
        },
      };
    },
    ExecuteReviewedAction: (request) => ({
      state: "completed",
      context: request.context,
      outcome: "completed",
      replayed: false,
      package_action: request.token === "package.decrypt-token" ? { name: "protected-001 decrypted", output: "output-3" } : { name: "protected-001", removed: 3 },
    }),
  });
  listing(facade);
  await goTo(user, "Projects");
  await user.click(screen.getByRole("button", { name: "Open" }));
  await sidebar().findByRole("button", { name: /^Project: / });
  await goTo(user, "Reports");
  await user.click(await page().findByRole("button", { name: "More report actions" }));
  await user.click(screen.getByRole("menuitem", { name: "Encrypted packages" }));
  const table = await page().findByRole("table", { name: "Encrypted packages" });
  await user.click(within(table).getByRole("row", { name: /protected-001/ }));
  expect(await page().findByRole("region", { name: "protected-001" })).toBeTruthy();
  expect(page().getAllByText("Retained")).toHaveLength(2);
  await user.click(page().getByRole("button", { name: "Decrypt" }));
  const decrypt = await screen.findByRole("dialog", { name: "Decrypt" });
  expect(within(decrypt).getByText("Writes a decrypted copy on this computer.")).toBeTruthy();
  expect(within(decrypt).getByRole("button", { name: "Decrypt" }).hasAttribute("disabled")).toBe(true);
  await user.click(within(decrypt).getByRole("button", { name: "Choose" }));
  await waitFor(() => expect(within(decrypt).getByRole("button", { name: "Decrypt" }).hasAttribute("disabled")).toBe(false));
  await user.click(within(decrypt).getByRole("button", { name: "Decrypt" }));
  await waitFor(() => expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(1));
  expect(await page().findByText("Decrypted protected-001 decrypted")).toBeTruthy();

  await user.click(page().getByRole("button", { name: "Delete package" }));
  const remove = await screen.findByRole("dialog", { name: "Delete package" });
  expect(within(remove).getByRole("checkbox", { name: "Override retention" })).toHaveProperty("checked", false);
  expect(await within(remove).findByText("the package is retained; override its retention to delete it")).toBeTruthy();
  await user.click(within(remove).getByRole("checkbox", { name: "Override retention" }));
  await waitFor(() => expect(within(remove).getByLabelText("Reason")).toBeTruthy());
  const final = within(remove).getByRole("button", { name: "Delete" });
  await waitFor(() => expect(facade.callsTo("PrepareAction").at(-1)!.args[0]).toMatchObject({ action: "package.delete", package: { package: "protected-001", override: true } }));
  expect(final.hasAttribute("disabled")).toBe(true);
  await user.type(within(remove).getByLabelText("Reason"), "Sent by other means");
  await waitFor(() => expect(within(remove).getByRole("button", { name: "Delete" }).hasAttribute("disabled")).toBe(false));
  await user.click(within(remove).getByRole("button", { name: "Delete" }));
  await waitFor(() => expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(2));
  expect(facade.callsTo("ExecuteReviewedAction")[1]!.args[0]).toMatchObject({ token: "package.delete-token", decisions: { rationale: "Sent by other means" } });
});

test("templates are listed by name, and a template is edited part by part and saved over the version that was opened", async () => {
  const user = userEvent.setup();
  const POLICY = { schema: "readmit-redact-policy/v1", patient: { selector: "PID-3.1", authority: [] }, fields: [{ selector: "PID-5", policy: "remove-field/v1", class: "names" }], remove_segments: [], packet_policies: ["rerun-derived-tests/v1"], spec_bindings: [], required_failures: [1] };
  const { facade } = await renderApp({
    SelectWorkspace: () => folderChosen(WORKSPACE_ROOT, [{ name: CASE_ENTRY, kind: "case", schema: "readmit-case/v3", provenance: "generated" }]),
    ListShareTemplates: (context) => ({ state: "completed", context, templates: [{ entry: "Clinic A.json", name: "Clinic A" }] }),
    ReadShareTemplate: (request) => ({ state: "completed", context: request.context, template: { entry: "Clinic A.json", name: "Clinic A" }, policy: POLICY, digest: "version-1" }),
    SaveShareTemplate: (request) => ({ state: "completed", context: request.context, template: { entry: "Clinic A.json", name: "Clinic A" }, policy: request.policy!, digest: "version-2" }),
  });
  listing(facade);
  await goTo(user, "Projects");
  await user.click(screen.getByRole("button", { name: "Open" }));
  await sidebar().findByRole("button", { name: /^Project: / });
  await goTo(user, "Reports");
  await user.click(await page().findByRole("button", { name: "More report actions" }));
  await user.click(screen.getByRole("menuitem", { name: "Templates" }));
  const table = await page().findByRole("table", { name: "Templates" });
  await user.click(within(table).getByRole("row", { name: /Clinic A/ }));
  await user.keyboard("{Enter}");
  expect(await page().findByText("Checked by a run")).toBeTruthy();
  expect(page().getByRole("button", { name: "Save" }).hasAttribute("disabled")).toBe(true);
  await user.click(page().getAllByRole("button", { name: "Edit" })[1]!);
  const segments = await screen.findByRole("dialog", { name: "Segments removed" });
  await user.type(within(segments).getByLabelText("Segments"), "nte, obx");
  await user.click(within(segments).getByRole("button", { name: "Apply" }));
  await user.click(page().getByRole("button", { name: "Add field" }));
  const rule = await screen.findByRole("dialog", { name: "Add field" });
  await user.type(within(rule).getByLabelText("Field"), "PID-7");
  await user.click(within(rule).getByRole("radio", { name: "Shift dates" }));
  await user.click(within(rule).getByRole("button", { name: "Apply" }));
  await user.click(page().getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveShareTemplate")).toHaveLength(1));
  expect(facade.oneCall("SaveShareTemplate")[0]).toMatchObject({
    entry: "Clinic A.json",
    digest: "version-1",
    policy: { remove_segments: ["NTE", "OBX"], fields: [POLICY.fields[0], { selector: "PID-7", policy: "patient-date-shift/v1", class: "dates-and-ages" }] },
  });
});

test("Encrypt package writes the share as an encrypted package under an active control chosen at its key generation", async () => {
  const user = userEvent.setup();
  const { facade } = await openReport(user, {
    PrepareAction: (request) => {
      const encrypt = request.report_share?.encrypt;
      const shown = share({
        controls: [{ entry: "protection.json", control: "lab-evidence", generation: 3, retention: "720h" }],
        ...(encrypt ? { encrypted: true, encryption: { control: "lab-evidence", reference: "security", generation: 3, retention: "720h" }, output: { ...share().output, type: "encrypted-package" } } : {}),
      });
      return { state: "completed", context: request.context, review: reviewed(request, shown, false) };
    },
  });
  await user.click(page().getByRole("button", { name: "Share" }));
  await page().findByRole("table", { name: "Contents" });
  expect(page().getByText("Off")).toBeTruthy();
  await user.click(page().getAllByRole("button", { name: "Change" })[1]!);
  const sheet = await screen.findByRole("dialog", { name: "Encryption" });
  const toggle = within(sheet).getByRole("checkbox", { name: "Encrypt package" });
  expect(toggle).toHaveProperty("checked", false);
  await user.click(toggle);
  await user.click(within(sheet).getByRole("radio", { name: /lab-evidence · Generation 3/ }));
  await user.click(within(sheet).getByRole("button", { name: "Apply" }));
  await waitFor(() => expect(prepared(facade).at(-1)!.report_share!.encrypt).toEqual({ entry: "protection.json", control: "lab-evidence", generation: 3 }));
  expect(await page().findByText("lab-evidence · security · Generation 3")).toBeTruthy();
  expect(page().getByText(/^Encrypted package · PDF/)).toBeTruthy();
});


test("connected output offers supported formats and refuses unsupported paper choices before review", async () => {
  const user = userEvent.setup();
  const { facade } = await openReport(user, {
    PrepareAction: request => ({ state: "completed", context: request.context, review: reviewed(request, share({connected_mode:request.report_share?.connected_mode ?? "original-report"}), false) }),
  });
  await user.click(page().getByRole("button", {name:"Share"}));
  await page().findByRole("radio", {name:"Original report with retained values"});
  await user.click(page().getAllByRole("button", {name:"Change"})[0]!);
  const format = await screen.findByRole("dialog", {name:"Format"});
  expect(within(format).getByRole("radio", {name:"A4"}).hasAttribute("disabled")).toBe(true);
  await user.click(within(format).getByRole("button", {name:"Apply"}));
  await user.click(page().getByRole("radio", {name:"Value-free extract"}));
  await waitFor(()=>expect(prepared(facade).at(-1)?.report_share?.connected_mode).toBe("value-free-extract"));
  await user.click(page().getAllByRole("button", {name:"Change"})[0]!);
  const extractFormat = await screen.findByRole("dialog", {name:"Format"});
  expect(within(extractFormat).getByRole("radio", {name:"PDF"}).hasAttribute("disabled")).toBe(true);
  expect(within(extractFormat).getByRole("radio", {name:"JSON"}).hasAttribute("disabled")).toBe(false);
});
