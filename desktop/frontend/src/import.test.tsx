// Import, driven as a person drives it: one started flow — Source, Format,
// Preview — over the real components, with only the typed facade stubbed.
// What a probe proposes, what a preview reads and what an import writes are
// decided on the Go side (internal/desktop's import flow tests); these tests
// prove the window sends exactly the selection, format and mapping a person
// chose, keeps them when something is refused, and never guesses.
import { StrictMode } from "react";
import { expect, test, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ImportFlow, importDrop } from "./Import";
import { VocabularyContext } from "./vocabulary";
import { installFacade, type FacadeHandlers } from "./testkit/wails";
import { WORKSPACE_ROOT, inspectionResult, caseCatalogItem, caseResult, folderChosen, messagesResult, projectOverviewResult, vocabularyFixture } from "./testkit/fixtures";
import { renderApp } from "./testkit/app";
import { page, sidebar } from "./testkit/navigation";
import type {
  CatalogItem,
  CatalogQuery,
  CatalogResult,
  EditorDraft,
  ImportPreviewResult,
  ImportProbeFormat,
  ImportProbeRequest,
  ImportProbeResult,
  ImportRequest,
  ItemRef,
  RequestContext,
} from "./bindings";

type User = ReturnType<typeof userEvent.setup>;

const CONTEXT: RequestContext = { project: WORKSPACE_ROOT, project_id: "p1", generation: 1 };
const HL7_PLAN = { schema: "readmit-import-plan/v1", framing: "raw", terminator: "cr", encoding: "utf-8", direction: "unknown", members: [] } as const;
const HL7: ImportProbeFormat = { mode: "plan", label: "HL7 v2", plan: { ...HL7_PLAN, members: [] } };
const CSV: ImportProbeFormat = { mode: "recipe", label: "CSV", envelope: "csv" };
const TEXT: ImportProbeFormat = { mode: "recipe", label: "Text", envelope: "text" };
const CASE_REF: ItemRef = { kind: "case", id: "case-case-001", revision: "1" };
const CSV_SAMPLE = { envelope: "csv" as const, columns: ["time", "payload", "flow"], paths: [] };

/** The open project's request context, one callback for the flow's life as
 * the window holds it. */
const projectContext = () => CONTEXT;

function baseName(path: string): string {
  return path.split("/").pop() ?? path;
}

/** A probe that names each chosen input and proposes the formats given; one
 * format is selected only when it is the only one. */
function probing(formats: ImportProbeFormat[], sample: ImportProbeResult["sample"] = null) {
  return (request: ImportProbeRequest): ImportProbeResult => {
    const inputs = [
      ...(request.files ?? []).map((path, index) => ({ name: baseName(path), kind: "file" as const, source: "file" as const, index })),
      ...(request.staged ?? []).map((_, index) => ({ name: "Pasted messages", kind: "file" as const, source: "staged" as const, index })),
      ...(request.folders ?? []).map((path, index) => ({ name: baseName(path), kind: "folder" as const, source: "folder" as const, index })),
      ...(request.archives ?? []).map((path, index) => ({ name: baseName(path), kind: "archive" as const, source: "archive" as const, index })),
    ].map((input, container) => ({ ...input, container, size: 120, accepted: true }));
    return { state: "completed", context: request.context ?? CONTEXT, inputs, formats, selected: formats.length === 1 ? 0 : null, sample };
  };
}

/** A preview of two messages under a fresh token for each request it reads. */
function previewing() {
  let read = 0;
  return (): ImportPreviewResult => {
    read += 1;
    return {
      state: "completed",
      mode: "plan",
      preview_token: `token-${read}`,
      rows: [
        { index: 0, time: null, type: "SIU^S12", source: "feed.hl7", direction: "unknown", kind: "message", member: "feed.hl7" },
        { index: 1, time: null, type: "SIU^S14", source: "feed.hl7", direction: "unknown", kind: "message", member: "feed.hl7" },
      ],
      row_total: 2,
    };
  };
}

function flowHandlers(handlers: FacadeHandlers = {}): FacadeHandlers {
  return {
    SaveEditorDraft: () => ({ state: "completed" }),
    DiscardEditorDraft: () => ({ state: "completed" }),
    ProbeImport: probing([HL7]),
    PreviewImport: previewing(),
    ...handlers,
  };
}

/** The flow as the Cases page opens it, over the open project. */
function renderFlow(handlers: FacadeHandlers = {}, drafts: EditorDraft[] | null = []) {
  const facade = installFacade(flowHandlers(handlers));
  const onImported = vi.fn();
  const onClose = vi.fn();
  render(
    <StrictMode>
      <VocabularyContext.Provider value={vocabularyFixture()}>
        <ImportFlow open root={WORKSPACE_ROOT} context={projectContext} drafts={drafts} busy={false} onClose={onClose} onImported={onImported} />
      </VocabularyContext.Provider>
    </StrictMode>,
  );
  return { facade, onImported, onClose };
}

function flow() {
  return within(screen.getByRole("dialog", { name: "Import" }));
}

function chosen(paths: string[], kind = "files") {
  return () => ({ state: "completed" as const, kind, paths });
}

async function chooseFiles(user: User, facade: ReturnType<typeof installFacade>, paths: string[]) {
  facade.reply({ ChooseImportSources: chosen(paths) });
  await user.click(flow().getByRole("button", { name: "Choose files" }));
  for (const path of paths) await flow().findByRole("rowheader", { name: baseName(path) });
}

async function chooseMore(user: User, facade: ReturnType<typeof installFacade>, item: "Choose folder" | "Choose ZIP", paths: string[], kind: string) {
  facade.reply({ ChooseImportSources: chosen(paths, kind) });
  await user.click(flow().getByRole("button", { name: "More ways to choose" }));
  await user.click(await screen.findByRole("menuitem", { name: item }));
}

function names(): string[] {
  const table = flow().queryByRole("table", { name: "Selected inputs" });
  return table ? within(table).getAllByRole("rowheader").map((cell) => cell.firstChild?.textContent ?? "") : [];
}

/** Chooses CSV and maps the message and the direction with two direction
 * rows. */
async function mapCsv(user: User) {
  await user.selectOptions(await flow().findByLabelText("Choose format"), "CSV");
  const sheet = within(await screen.findByRole("dialog", { name: "Mapping" }));
  await user.selectOptions(sheet.getByLabelText("Message"), "payload");
  await user.selectOptions(sheet.getByLabelText("Direction from"), "field");
  await user.selectOptions(sheet.getAllByRole("combobox").find((select) => select.id === "mapping-direction-field")!, "flow");
  await user.click(sheet.getByRole("button", { name: "Add mapping" }));
  await user.type(sheet.getByLabelText("Source value 1"), "IN");
  await user.click(sheet.getByRole("button", { name: "Add mapping" }));
  await user.type(sheet.getByLabelText("Source value 2"), "OUT");
  await user.selectOptions(sheet.getByLabelText("Direction 2"), "outbound");
  await user.click(sheet.getByRole("button", { name: "Done" }));
}

test("Choose files, folder and ZIP name each input, and a cancelled picker keeps the selection", async () => {
  const user = userEvent.setup();
  const { facade } = renderFlow();
  await chooseFiles(user, facade, ["/exports/feed.hl7"]);
  await chooseMore(user, facade, "Choose folder", ["/exports/nightly"], "folder");
  await flow().findByRole("rowheader", { name: "nightly" });
  await chooseMore(user, facade, "Choose ZIP", ["/exports/week.zip"], "archive");
  await flow().findByRole("rowheader", { name: "week.zip" });
  expect(names()).toEqual(["feed.hl7", "nightly", "week.zip"]);
  const table = flow().getByRole("table", { name: "Selected inputs" });
  expect(within(table).getAllByRole("row").slice(1).map((row) => within(row).getAllByRole("cell")[0]?.textContent)).toEqual(["File", "Folder", "ZIP"]);
  expect(facade.callsTo("ChooseImportSources").map((call) => call.args[0])).toEqual(["files", "folder", "archive"]);

  // A cancelled picker leaves every input where it was.
  facade.reply({ ChooseImportSources: () => ({ state: "cancelled" }) });
  await user.click(flow().getByRole("button", { name: "Add files" }));
  await waitFor(() => expect(facade.callsTo("ChooseImportSources")).toHaveLength(4));
  expect(names()).toEqual(["feed.hl7", "nightly", "week.zip"]);

  // Removing one takes its reference out of the import, not its bytes.
  await user.click(flow().getByRole("button", { name: "Remove nightly from import" }));
  await waitFor(() => expect(names()).toEqual(["feed.hl7", "week.zip"]));
  const last = facade.callsTo("ProbeImport").at(-1)!.args[0] as ImportProbeRequest;
  expect(last).toMatchObject({ files: ["/exports/feed.hl7"], folders: [], archives: ["/exports/week.zip"] });
});

test("FHIR import declares R4 source semantics before preview and passes the same declaration to Import", async () => {
  const user = userEvent.setup();
  const { facade } = renderFlow({ ProbeImport: probing([CSV, TEXT]), PreviewImport: () => ({ state: "completed", mode: "fhir-r4", preview_token: "r4-preview", row_total: 2, rows: [{ index: 0, time: null, type: "Bundle", source: "synthetic-r4", direction: "unknown", kind: "resource", member: "synthetic-r4" }, { index: 1, time: null, type: "Patient", source: "synthetic-r4", direction: "unknown", kind: "resource", member: "synthetic-r4" }] }),
    InspectImportPreview:()=>({state:"failed",reason:"Owned preview refusal"}),
    ImportCase: (request) => ({ state: "completed", context: request.context, case: CASE_REF, entry: "managed-r4", replayed: false }),
  });
  await chooseFiles(user, facade, ["/exports/synthetic-r4.json"]);
  await user.click(flow().getByRole("button", { name: "Next" }));
  await user.selectOptions(await flow().findByLabelText("Choose format"), "FHIR R4 JSON");
  const format = within(await screen.findByRole("dialog", { name: "Format" }));
  expect((format.getByLabelText("FHIR version") as HTMLSelectElement).value).toBe("4.0.1");
  await user.selectOptions(format.getByLabelText("Source type"), "bundle");
  await user.click(format.getByRole("button", { name: "Done" }));
  await user.click(flow().getByRole("button", { name: "Next" }));
  await flow().findByRole("table", { name: "Preview" });
  expect(facade.oneCall("PreviewImport")[0]).toMatchObject({ mode: "fhir-r4", fhir: { source_kind: "bundle", context: { version: "4.0.1", media_type: "application/fhir+json", base: "" } } });
  await user.click(flow().getByRole("table",{name:"Preview"}).querySelector<HTMLElement>('[data-row-id="0"]')!);
  await waitFor(()=>expect(facade.callsTo("InspectImportPreview")[0]?.args[0]).toMatchObject({reveal:false}));
  await user.click(flow().getByRole("button", { name: "Import" }));
  await waitFor(() => expect(facade.callsTo("ImportCase")).toHaveLength(1));
  const previewed = facade.oneCall("PreviewImport")[0];
  expect(facade.oneCall("ImportCase")[0].source.fhir).toEqual(previewed.fhir);
  expect(facade.callsTo("CheckTarget")).toHaveLength(0);
});

test("pasted messages become their own source named Pasted messages", async () => {
  const user = userEvent.setup();
  const { facade } = renderFlow({
    StagePastedContent: (request) => ({ state: "completed", staged_id: "0123456789abcdef01234567", name: request.name ?? "Pasted messages", size: request.content.length, encoding: "utf-8" }),
  });
  await user.click(flow().getByRole("button", { name: "Paste" }));
  const sheet = within(await screen.findByRole("dialog", { name: "Paste" }));
  expect((sheet.getByLabelText("Name") as HTMLInputElement).value).toBe("Pasted messages");
  expect((sheet.getByRole("button", { name: "Add" }) as HTMLButtonElement).disabled).toBe(true);
  await user.type(sheet.getByLabelText("Messages"), "MSH|^~\\&|SEND");
  await user.click(sheet.getByRole("button", { name: "Add" }));
  await flow().findByRole("rowheader", { name: "Pasted messages" });
  const [staged] = facade.oneCall("StagePastedContent");
  expect(staged).toMatchObject({ context: CONTEXT, name: "Pasted messages", content: "MSH|^~\\&|SEND" });
  // It is named by its identity when the inputs are read, never by a path.
  const probed = facade.callsTo("ProbeImport").at(-1)!.args[0] as ImportProbeRequest;
  expect(probed).toMatchObject({ files: [], staged: ["0123456789abcdef01234567"] });
});

test("the case name defaults to the source's name, or Imported messages for several", async () => {
  const user = userEvent.setup();
  const { facade } = renderFlow();
  const caseName = () => (flow().getByLabelText("Case") as HTMLInputElement).value;
  await chooseFiles(user, facade, ["/exports/appointments.mllp"]);
  expect(caseName()).toBe("appointments");
  await chooseFiles(user, facade, ["/exports/second.hl7"]);
  expect(caseName()).toBe("Imported messages");
  // A name the person typed is kept whatever is chosen next.
  await user.clear(flow().getByLabelText("Case"));
  await user.type(flow().getByLabelText("Case"), "Reschedule feed");
  await user.click(flow().getByRole("button", { name: "Remove second.hl7 from import" }));
  await waitFor(() => expect(names()).toEqual(["appointments.mllp"]));
  expect(caseName()).toBe("Reschedule feed");
});

test("an unambiguous HL7 file goes straight to preview and an ambiguous one asks Choose format", async () => {
  const user = userEvent.setup();
  const { facade } = renderFlow();
  await chooseFiles(user, facade, ["/exports/feed.hl7"]);
  await user.click(flow().getByRole("button", { name: "Next" }));
  expect(await flow().findByRole("table", { name: "Preview" })).toBeTruthy();
  expect(flow().queryByLabelText("Choose format")).toBeNull();
  const [previewed] = facade.oneCall("PreviewImport");
  expect(previewed).toMatchObject({ mode: "plan", plan: HL7_PLAN, files: ["/exports/feed.hl7"] });

  // Comma-separated text reads as CSV or as text: the person chooses.
  await user.click(flow().getByRole("button", { name: "Back" }));
  await user.click(flow().getByRole("button", { name: "Back" }));
  facade.reply({ ProbeImport: probing([CSV, TEXT]) });
  await user.click(flow().getByRole("button", { name: "Remove feed.hl7 from import" }));
  await chooseFiles(user, facade, ["/exports/feed.csv"]);
  await user.click(flow().getByRole("button", { name: "Next" }));
  const choice = await flow().findByLabelText("Choose format");
  expect(within(choice).getAllByRole("option").map((option) => option.textContent)).toEqual([
    "Choose a format",
    "CSV",
    "Text",
    "FHIR R4 JSON",
    "Mirth Connect 4.5.2",
    "Open Integration Engine 4.6.0",
  ]);
  expect((flow().getByRole("button", { name: "Next" }) as HTMLButtonElement).disabled).toBe(true);
  expect(facade.callsTo("PreviewImport")).toHaveLength(1);
});

test("an unsupported engine export version shows its refusal", async () => {
  const user = userEvent.setup();
  const refusal = "unsupported engine export declaration or XML variant";
  const { facade } = renderFlow({ ProbeImport: probing([]), PreviewImport: () => ({ state: "failed", reason: refusal }) });
  await chooseFiles(user, facade, ["/exports/channel-export.xml"]);
  await user.click(flow().getByRole("button", { name: "Next" }));
  await user.selectOptions(await flow().findByLabelText("Choose format"), "Mirth Connect 4.5.2");
  // The version is the supported one, read-only.
  await user.click(flow().getByRole("button", { name: "Edit" }));
  const sheet = within(await screen.findByRole("dialog", { name: "Format" }));
  expect(sheet.getByText("4.5.2")).toBeTruthy();
  expect(sheet.queryByRole("textbox", { name: "Version" })).toBeNull();
  await user.click(sheet.getByRole("button", { name: "Done" }));
  await user.click(flow().getByRole("button", { name: "Next" }));
  expect(await flow().findByText(refusal)).toBeTruthy();
  const [previewed] = facade.oneCall("PreviewImport");
  expect(previewed).toMatchObject({ mode: "engine", engine_plan: { engine: "mirth", version: "4.5.2", format: "raw", terminator: "cr" } });
  expect((flow().getByRole("button", { name: "Import" }) as HTMLButtonElement).disabled).toBe(true);
});

test("mapping fields and direction rows survive Back and reopening the draft", async () => {
  const user = userEvent.setup();
  const { facade } = renderFlow({ ProbeImport: probing([CSV, TEXT], CSV_SAMPLE) });
  await chooseFiles(user, facade, ["/exports/feed.csv"]);
  await user.click(flow().getByRole("button", { name: "Next" }));
  await mapCsv(user);
  expect(flow().getByText("payload")).toBeTruthy();
  await user.click(flow().getByRole("button", { name: "Back" }));
  await user.click(flow().getByRole("button", { name: "Next" }));
  await user.click(flow().getAllByRole("button", { name: "Edit" })[1]!);
  let sheet = within(await screen.findByRole("dialog", { name: "Mapping" }));
  expect((sheet.getByLabelText("Source value 1") as HTMLInputElement).value).toBe("IN");
  expect((sheet.getByLabelText("Source value 2") as HTMLInputElement).value).toBe("OUT");
  expect((sheet.getByLabelText("Direction 2") as HTMLSelectElement).value).toBe("outbound");
  await user.click(sheet.getByRole("button", { name: "Cancel" }));

  // The draft the flow kept reopens with the same mapping and rows.
  await waitFor(() => expect(facade.callsTo("SaveEditorDraft").length).toBeGreaterThan(0));
  const kept = facade.callsTo("SaveEditorDraft").at(-1)!.args[0] as EditorDraft;
  expect(kept).toMatchObject({ kind: "import", workspace: WORKSPACE_ROOT, content_schema: "readmit-import-draft/v2" });
  cleanup();
  renderFlow({ ProbeImport: probing([CSV, TEXT], CSV_SAMPLE) }, [kept]);
  await flow().findByRole("heading", { name: "Mapping" });
  await user.click(flow().getAllByRole("button", { name: "Edit" })[1]!);
  sheet = within(await screen.findByRole("dialog", { name: "Mapping" }));
  expect((sheet.getByLabelText("Message") as HTMLSelectElement).value).toBe("payload");
  expect((sheet.getByLabelText("Source value 1") as HTMLInputElement).value).toBe("IN");
  expect((sheet.getByLabelText("Source value 2") as HTMLInputElement).value).toBe("OUT");
  expect((sheet.getByLabelText("Direction 2") as HTMLSelectElement).value).toBe("outbound");
});

test("a changed source or mapping discards the preview", async () => {
  const user = userEvent.setup();
  const { facade } = renderFlow({
    ProbeImport: probing([HL7]),
    InspectImportPreview: (request) => ({ state: "failed", reason: `read ${request.preview_token}` }),
  });
  await chooseFiles(user, facade, ["/exports/feed.hl7"]);
  await user.click(flow().getByRole("button", { name: "Next" }));
  await user.click(await within(await flow().findByRole("table", { name: "Preview" })).findByRole("row", { name: "SIU^S12 1" }));
  await waitFor(() => expect(facade.callsTo("InspectImportPreview")[0]?.args[0]).toMatchObject({ preview_token: "token-1", row: 0 }));

  // Back to the source and a changed selection: the preview is withdrawn
  // and read again under the new inputs, never reused.
  await user.click(flow().getByRole("button", { name: "Back" }));
  await user.click(flow().getByRole("button", { name: "Back" }));
  await chooseFiles(user, facade, ["/exports/more.hl7"]);
  await user.click(flow().getByRole("button", { name: "Next" }));
  await flow().findByRole("table", { name: "Preview" });
  const previews = facade.callsTo("PreviewImport").map((call) => call.args[0] as ImportRequest);
  expect(previews).toHaveLength(2);
  expect(previews[1]!.files).toEqual(["/exports/feed.hl7", "/exports/more.hl7"]);
  await user.click(await flow().findByRole("row", { name: "SIU^S14 2" }));
  await waitFor(() => expect(facade.callsTo("InspectImportPreview").at(-1)!.args[0]).toMatchObject({ preview_token: "token-2", row: 1 }));

  // A changed mapping withdraws the preview the old mapping read.
  cleanup();
  const again = renderFlow({ ProbeImport: probing([CSV, TEXT], CSV_SAMPLE) }).facade;
  await chooseFiles(user, again, ["/exports/feed.csv"]);
  await user.click(flow().getByRole("button", { name: "Next" }));
  await mapCsv(user);
  await user.click(flow().getByRole("button", { name: "Next" }));
  await flow().findByRole("table", { name: "Preview" });
  await user.click(flow().getByRole("button", { name: "Back" }));
  await user.click(flow().getAllByRole("button", { name: "Edit" })[1]!);
  const sheet = within(await screen.findByRole("dialog", { name: "Mapping" }));
  await user.selectOptions(sheet.getByLabelText("Direction 1"), "outbound");
  await user.click(sheet.getByRole("button", { name: "Done" }));
  await user.click(flow().getByRole("button", { name: "Next" }));
  await flow().findByRole("table", { name: "Preview" });
  const directions = again.callsTo("PreviewImport").map((call) => (call.args[0] as ImportRequest).recipe!.direction);
  expect(directions).toHaveLength(2);
  expect(directions[1]).toMatchObject({ operator: "field", locator: ["flow"], values: [{ envelope: "IN", mapped: "outbound" }, { envelope: "OUT", mapped: "outbound" }] });
});

test("a failed import keeps the selection, mapping and reason", async () => {
  const user = userEvent.setup();
  const reason = "the project's catalog cannot be read; it is left exactly as written";
  const { facade, onImported } = renderFlow({
    ProbeImport: probing([CSV, TEXT], CSV_SAMPLE),
    ImportCase: (request) => ({ state: "failed", reason, context: request.context, replayed: false }),
  });
  await chooseFiles(user, facade, ["/exports/feed.csv"]);
  await user.click(flow().getByRole("button", { name: "Next" }));
  await mapCsv(user);
  await user.click(flow().getByRole("button", { name: "Next" }));
  await flow().findByRole("table", { name: "Preview" });
  await user.click(flow().getByRole("button", { name: "Import" }));
  expect(await flow().findByText(reason)).toBeTruthy();
  expect(onImported).not.toHaveBeenCalled();
  expect(facade.callsTo("DiscardEditorDraft")).toHaveLength(0);
  await user.click(flow().getByRole("button", { name: "Back" }));
  await user.click(flow().getAllByRole("button", { name: "Edit" })[1]!);
  const sheet = within(await screen.findByRole("dialog", { name: "Mapping" }));
  expect((sheet.getByLabelText("Message") as HTMLSelectElement).value).toBe("payload");
  expect((sheet.getByLabelText("Source value 2") as HTMLInputElement).value).toBe("OUT");
  await user.click(sheet.getByRole("button", { name: "Cancel" }));
  await user.click(flow().getByRole("button", { name: "Back" }));
  expect(names()).toEqual(["feed.csv"]);
});

test("Stop cancels an import while it writes its case, and the flow keeps its selection and says why", async () => {
  const user = userEvent.setup();
  const { facade, onImported } = renderFlow({ ProbeImport: probing([CSV, TEXT], CSV_SAMPLE) });
  const writing = facade.park("ImportCase");
  await chooseFiles(user, facade, ["/exports/feed.csv"]);
  await user.click(flow().getByRole("button", { name: "Next" }));
  await mapCsv(user);
  await user.click(flow().getByRole("button", { name: "Next" }));
  await flow().findByRole("table", { name: "Preview" });
  expect(flow().queryByRole("button", { name: "Stop" })).toBeNull();
  await user.click(flow().getByRole("button", { name: "Import" }));
  await user.click(await flow().findByRole("button", { name: "Stop" }));
  expect(facade.callsTo("Cancel").at(-1)?.args).toEqual(["import"]);
  writing.resolve({ state: "cancelled", reason: "the operation was cancelled", context: CONTEXT, replayed: false });
  expect(await flow().findByText("the operation was cancelled")).toBeTruthy();
  expect(flow().queryByRole("button", { name: "Stop" })).toBeNull();
  expect(onImported).not.toHaveBeenCalled();
});

test("Save mapping asks a name and publishes a preset", async () => {
  const user = userEvent.setup();
  const { facade } = renderFlow({
    ProbeImport: probing([CSV, TEXT], CSV_SAMPLE),
    SaveItem: (request) => ({ state: "completed", context: request.context, outcome: "saved", replayed: false, problems: [], saved: { kind: "mapping", id: "m1", revision: "1" } }),
  });
  await chooseFiles(user, facade, ["/exports/feed.csv"]);
  await user.click(flow().getByRole("button", { name: "Next" }));
  await mapCsv(user);
  // Mapping is enough to import; nothing was published yet.
  expect(facade.callsTo("SaveItem")).toHaveLength(0);
  await user.click(flow().getByRole("button", { name: "More mapping actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "Save mapping…" }));
  const sheet = within(await screen.findByRole("dialog", { name: "Save mapping" }));
  expect((sheet.getByRole("button", { name: "Save" }) as HTMLButtonElement).disabled).toBe(true);
  await user.type(sheet.getByLabelText("Name"), "Scheduling CSV");
  await user.click(sheet.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(facade.callsTo("SaveItem")).toHaveLength(1));
  const [saved] = facade.oneCall("SaveItem");
  expect(saved).toMatchObject({ kind: "mapping", draft: { name: "Scheduling CSV", mapping: { envelope: "csv", payload: { locator: ["payload"] } } } });
  expect(saved.intent_id).not.toBe("");
});

test("dropped files and folders join the selection by name and a link is refused", async () => {
  const user = userEvent.setup();
  const { facade } = renderFlow({
    ClassifyDroppedSources: (paths) => ({
      state: "completed",
      files: paths.filter((path) => path.endsWith(".hl7") && !path.endsWith("link.hl7")),
      folders: paths.filter((path) => path.endsWith("nightly")),
      archives: [],
      refused: [{ name: "link.hl7", reason: "it is a link; drop what it points to" }],
    }),
  });
  await chooseFiles(user, facade, ["/exports/feed.hl7"]);
  importDrop.deliver?.(["/drops/more.hl7", "/drops/nightly", "/drops/link.hl7"]);
  await flow().findByRole("rowheader", { name: "nightly" });
  expect(names()).toEqual(["feed.hl7", "more.hl7", "nightly"]);
  expect(facade.oneCall("ClassifyDroppedSources")[0]).toEqual(["/drops/more.hl7", "/drops/nightly", "/drops/link.hl7"]);
  expect(await flow().findByText("link.hl7: it is a link; drop what it points to")).toBeTruthy();
});

// ---------- Through the window ----------

const PROJECT: CatalogItem = {
  ref: { kind: "project", id: "p1", revision: "rev-project-1" },
  name: "Scheduling investigation",
  created_at: null,
  updated_at: null,
  last_opened_at: "2026-09-26T10:00:00Z",
  availability: "available",
  capabilities: [],
  summary: { project: { folder: WORKSPACE_ROOT, schema: "readmit-project/v2", cases: 0, interface_versions: ["v1"], owner: "Integration team", tags: [], revisions: [{ id: "v1", name: "v1", default: true }] } },
};

test("Import sends one intent, opens Messages, and a second click creates nothing", async () => {
  const user = userEvent.setup();
  const cases: CatalogItem[] = [];
  const imported = { ...caseCatalogItem("case-001", "imported", "open"), ref: CASE_REF, name: "feed" };
  const importCase = vi.fn();
  const { facade } = await renderApp({
    SelectWorkspace: () => folderChosen(WORKSPACE_ROOT, [{ name: "project.json", kind: "project", schema: "readmit-project/v2" }]),
    OpenProjectOverview: () => projectOverviewResult([]),
    OpenNamedProject: () => ({ state: "completed", context: { project: "", generation: 0 }, recorded: true }),
    ListCatalog: (query: CatalogQuery): CatalogResult => {
      const items = query.kind === "project" ? [PROJECT] : query.kind === "case" ? cases : [];
      return { state: "completed", context: query.context, page: { items, total: items.length, snapshot: "s", recorded: true, incomplete: [] } };
    },
    ListCaptureSessions: (context) => ({ state: "empty", context, sessions: [] }),
    CaptureProgress: () => ({ state: "empty" }),
    ...flowHandlers(),
    ImportCase: (request) => {
      importCase(request);
      if (!cases.includes(imported)) cases.push(imported);
      return { state: "completed", context: request.context, case: CASE_REF, replayed: importCase.mock.calls.length > 1 };
    },
    OpenCase: (_workspace, name) => caseResult(name),
    ReadMessages: () => messagesResult([]),
  });
  await user.click(screen.getAllByRole("button", { name: "Open" })[0] as HTMLElement);
  await sidebar().findByRole("button", { name: "Project: Scheduling investigation" });
  // The page header's Import (an empty project also offers one in its body).
  await user.click(page().getAllByRole("button", { name: "Import" })[0]!);
  await chooseFiles(user, facade, ["/exports/feed.hl7"]);
  await user.click(flow().getByRole("button", { name: "Next" }));
  await flow().findByRole("table", { name: "Preview" });
  // A second click while the first is answered sends nothing more.
  await user.dblClick(flow().getByRole("button", { name: "Import" }));
  // The case opens on its Messages.
  expect(await screen.findByRole("region", { name: "Messages" }, { timeout: 5000 })).toBeTruthy();
  expect(facade.callsTo("OpenCase").at(-1)!.args).toEqual([WORKSPACE_ROOT, "case-001"]);
  const [request] = importCase.mock.calls[0]!;
  expect(request).toMatchObject({ name: "feed", preview_token: "token-1", source: { mode: "plan", files: ["/exports/feed.hl7"] } });
  expect(request.intent_id).not.toBe("");
  expect(importCase).toHaveBeenCalledTimes(1);

  // A later import, from a new opening of the flow, is a new click.
  await user.click(page().getAllByRole("button", { name: /^Back to / })[0] ?? page().getByRole("button", { name: "Cases" }));
  await user.click((await page().findAllByRole("button", { name: "Import" }))[0]!);
  await chooseFiles(user, facade, ["/exports/feed.hl7"]);
  await user.click(flow().getByRole("button", { name: "Next" }));
  await flow().findByRole("table", { name: "Preview" });
  await user.click(flow().getByRole("button", { name: "Import" }));
  await waitFor(() => expect(importCase).toHaveBeenCalledTimes(2));
  expect(importCase.mock.calls[1]![0].intent_id).not.toBe(request.intent_id);
});

test("Stop cancels an import while it writes its case, and the flow keeps its selection and says why", async () => {
  const user = userEvent.setup();
  const { facade, onImported } = renderFlow({ ProbeImport: probing([CSV, TEXT], CSV_SAMPLE) });
  const writing = facade.park("ImportCase");
  await chooseFiles(user, facade, ["/exports/feed.csv"]);
  await user.click(flow().getByRole("button", { name: "Next" }));
  await mapCsv(user);
  await user.click(flow().getByRole("button", { name: "Next" }));
  await flow().findByRole("table", { name: "Preview" });
  expect(flow().queryByRole("button", { name: "Stop" })).toBeNull();
  await user.click(flow().getByRole("button", { name: "Import" }));
  await user.click(await flow().findByRole("button", { name: "Stop" }));
  expect(facade.callsTo("Cancel").at(-1)?.args).toEqual(["import"]);
  writing.resolve({ state: "cancelled", reason: "the operation was cancelled", context: CONTEXT, replayed: false });
  expect(await flow().findByText("the operation was cancelled")).toBeTruthy();
  expect(flow().queryByRole("button", { name: "Stop" })).toBeNull();
  expect(onImported).not.toHaveBeenCalled();
});


test("preview messages keep explicit reference choices without pinning an automatic edition",async()=>{
 const user=userEvent.setup();
 const catalog=WORKSPACE_ROOT+"/owned-catalog.json",profile=WORKSPACE_ROOT+"/owned-profile.json";
 const selection={profile,profile_identity:"owned-profile-identity"};
 const ref={status:"available",reason:"",identity:"manual-catalog",edition:"2.5.1",coverage:{segments:0,fields:0,definitions:0,missing:[]},missing_count:0};
 const {facade}=renderFlow({
  InspectImportPreview:request=>inspectionResult("",{identity:request.preview_token,occurrence:"",message:request.row,revealed:true,phi_masked:request.mask_phi??false,reference_catalog:request.reference_catalog || WORKSPACE_ROOT+"/automatic-"+request.row,reference:{...ref,identity:request.reference_identity || "automatic-"+request.row,edition:request.row===0?"2.5.1":"2.7.1"}}),
  ChooseInspectionPath:kind=>({state:"completed",kind,path:kind==="reference-catalog"?catalog:profile}),
  ReadReferenceCatalog:()=>({state:"completed",reference:ref}),
  ReadHL7ReferenceSelection:()=>({state:"completed",overlay:{status:"profile_selected",reason:"",selection}}),
 });
 await chooseFiles(user,facade,[WORKSPACE_ROOT+"/feed.hl7"]);
 await user.click(flow().getByRole("button",{name:"Next"}));
 await user.click(await flow().findByRole("row",{name:"SIU^S12 1"}));
 await user.click(await flow().findByRole("button",{name:"More message actions"}));
 await user.click(screen.getByRole("menuitem",{name:"Choose local profile…"}));
 await waitFor(()=>expect(facade.callsTo("InspectImportPreview").at(-1)?.args[0]).toMatchObject({reference_selection:selection}));
 await user.click(flow().getByRole("row",{name:"SIU^S14 2"}));
 await waitFor(()=>expect(facade.callsTo("InspectImportPreview").at(-1)?.args[0]).toMatchObject({row:1,reference_selection:selection}));
 expect(facade.callsTo("InspectImportPreview").at(-1)?.args[0]).not.toHaveProperty("reference_catalog");
 await user.click(flow().getByRole("button",{name:"Enable PHI masking"}));
 await waitFor(()=>expect(facade.callsTo("InspectImportPreview").at(-1)?.args[0]).toMatchObject({mask_phi:true,reference_selection:selection}));
 await user.click(flow().getByRole("button",{name:"HL7 version"}));
 await user.click(screen.getByRole("menuitem",{name:"Use custom definitions…"}));
 await waitFor(()=>expect(facade.callsTo("InspectImportPreview").at(-1)?.args[0]).toMatchObject({reference_catalog:catalog,reference_identity:"manual-catalog"}));
 await user.click(flow().getByRole("row",{name:"SIU^S12 1"}));
 await waitFor(()=>expect(facade.callsTo("InspectImportPreview").at(-1)?.args[0]).toMatchObject({row:0,reference_catalog:catalog,reference_identity:"manual-catalog",reference_selection:selection}));
});
