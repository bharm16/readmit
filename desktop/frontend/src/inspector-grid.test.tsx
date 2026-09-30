// The Messages view and the shared reader. The list draws what an occurrence
// is and where it came from, never a value; filters and searches are
// transient queries the facade answers without writing anything, and a view is
// stored only by Save view. The reader keeps values hidden until Show values.
// Fixtures carry positions, types, states and counts only.
import { expect, test } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import type { GridQuery, InspectionResult, MessageRow, MessagesResult } from "./bindings";
import { MessageReader } from "./Inspector";
import { MessageList, NO_QUERY, typeLabel, type FilterSeed } from "./Messages";
import {
  CASE_ENTRY,
  CASE_IDENTITY,
  GRID_OCCURRENCE,
  NEXT_OCCURRENCE,
  WORKSPACE_ROOT,
  caseResult,
  folderWithCase,
  inspectionResult,
  messageRow,
  messagesResult,
} from "./testkit/fixtures";
import { renderApp } from "./testkit/app";
import { Parked } from "./testkit/wails";
import { findCaseRow, findMessageRow, goTo, page } from "./testkit/navigation";

type User = ReturnType<typeof userEvent.setup>;

/** A MessageList holding its own query and selection, as the window does. */
const FIELDS = [
  { segment: "MSH", segment_name: "Message header", field: 10, label: "Message Control ID", selector: "MSH[1]-10[1]" },
  { segment: "SCH", segment_name: "Scheduling activity information", field: 11, label: "Appointment timing quantity", selector: "SCH[1]-11[1]" },
  { segment: "PID", segment_name: "Patient identification", field: 8, label: "Administrative Sex", selector: "PID[1]-8[1]" },
];

function List({
  result,
  onQuery = () => undefined,
  seed = null,
  onAction = () => undefined,
}: {
  result: MessagesResult | null;
  onQuery?: (query: GridQuery) => void;
  seed?: FilterSeed | null;
  onAction?: (action: string) => void;
}) {
  const [query, setQuery] = useState<GridQuery>(NO_QUERY);
  const [checked, setChecked] = useState<Set<string>>(new Set());
  const [selected, setSelected] = useState<string | null>(null);
  const [heldSeed, setSeed] = useState(seed);
  return (
    <MessageList
      result={result}
      rows={result?.rows ?? []}
      loading={false}
      query={query}
      onQuery={(next) => {
        setQuery(next);
        onQuery(next);
      }}
      sort={null}
      onSort={() => undefined}
      views={[]}
      view=""
      onView={() => undefined}
      onSaveView={async () => null}
      onRenameView={async () => null}
      onRemoveView={async () => null}
      selected={selected}
      onInspect={setSelected}
      checked={checked}
      onCheck={setChecked}
      onCreateTest={() => onAction("test")}
      onSendSelected={() => onAction("send")}
      onCreateVariant={() => onAction("variant")}
      onLoadMore={() => undefined}
      onRetry={() => onAction("retry")}
      onImport={() => onAction("import")}
      onSearchSettings={() => undefined}
      onFields={async () => ({ state: "completed", fields: FIELDS, complete: true })}
      seed={heldSeed}
      onSeedUsed={() => setSeed(null)}
      busy={false}
    />
  );
}

async function openCase(facade: Awaited<ReturnType<typeof renderApp>>["facade"], user: User, rows: MessageRow[]) {
  facade.reply({ SelectWorkspace: () => folderWithCase(), OpenCase: () => caseResult(), ReadMessages: () => messagesResult(rows) });
  await goTo(user, "Projects");
  await user.click(screen.getByRole("button", { name: "Open" }));
  await user.click(await findCaseRow());
  await findMessageRow(rows[0]!.id);
}

test("the messages list reads a case without an index and applies a transient filter without saving it", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp();
  await openCase(facade, user, [messageRow(GRID_OCCURRENCE), messageRow(NEXT_OCCURRENCE, "ack")]);
  // Opening the case read its messages: no index was described, chosen or built.
  const [first] = facade.oneCall("ReadMessages");
  expect(first).toEqual({ workspace: WORKSPACE_ROOT, case: CASE_ENTRY, identity: CASE_IDENTITY, query: NO_QUERY, sort: "", offset: 0, limit: 0 });
  expect(within(screen.getByRole("region", { name: "Messages" })).queryByText(/index/i)).toBeNull();

  await user.click(screen.getByRole("button", { name: "Filter messages" }));
  const sheet = await screen.findByRole("dialog", { name: "Filter" });
  await user.selectOptions(within(sheet).getByRole("combobox", { name: "Field of rule 1" }), "Type");
  await user.click(within(sheet).getByRole("checkbox", { name: "SIU · S12" }));
  await user.click(within(sheet).getByRole("button", { name: "Apply" }));

  await waitFor(() => expect(facade.callsTo("ReadMessages")).toHaveLength(2));
  expect(facade.callsTo("ReadMessages")[1]!.args[0]).toMatchObject({
    query: { ...NO_QUERY, types: [{ kind: "message", code: "SIU", trigger: "S12" }] },
    offset: 0,
  });
  expect(screen.getByText("Type is SIU · S12")).toBeTruthy();
  expect(screen.getByRole("button", { name: "Save view" })).toBeTruthy();
  // Applying wrote nothing: no view, no saved filter, no selection.
  for (const write of ["SaveView", "SaveFilter", "SelectFilter"] as const) expect(facade.callsTo(write)).toHaveLength(0);

  // Removing the chip is the unfiltered list again.
  await user.click(screen.getByRole("button", { name: "Remove Type is SIU · S12" }));
  await waitFor(() => expect(facade.callsTo("ReadMessages")).toHaveLength(3));
  expect(facade.callsTo("ReadMessages")[2]!.args[0]).toMatchObject({ query: NO_QUERY });
});

test("a view is saved only from the applied toolbar, listed for its project, renamed and removed", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp();
  await openCase(facade, user, [messageRow(GRID_OCCURRENCE)]);
  expect(facade.oneCall("ListViews")).toEqual([WORKSPACE_ROOT]);
  // With nothing applied there is nothing to save.
  expect(screen.queryByRole("button", { name: "Save view" })).toBeNull();

  await user.click(screen.getByRole("button", { name: "Filter messages" }));
  const sheet = await screen.findByRole("dialog", { name: "Filter" });
  await user.selectOptions(within(sheet).getByRole("combobox", { name: "Field of rule 1" }), "Direction");
  await user.click(within(sheet).getByRole("checkbox", { name: "Outbound" }));
  await user.click(within(sheet).getByRole("button", { name: "Apply" }));

  const query: GridQuery = { ...NO_QUERY, directions: ["outbound"] };
  facade.reply({ SaveView: (_root, name, saved) => ({ state: "completed", views: [{ name, query: saved }] }) });
  await user.click(await screen.findByRole("button", { name: "Save view" }));
  const naming = await screen.findByRole("dialog", { name: "Save view" });
  await user.type(within(naming).getByRole("textbox", { name: "Name" }), "Outbound only");
  await user.click(within(naming).getByRole("button", { name: "Save" }));
  expect(facade.oneCall("SaveView")).toEqual([WORKSPACE_ROOT, "Outbound only", query]);
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Save view" })).toBeNull());
  expect(screen.getByRole("button", { name: "Messages view" }).textContent).toBe("Outbound only");
  expect(screen.queryByRole("button", { name: "Save view" })).toBeNull();

  facade.reply({ RenameView: (_root, _from, to) => ({ state: "completed", views: [{ name: to, query }] }) });
  await user.click(screen.getByRole("button", { name: "Messages view" }));
  await user.click(screen.getByRole("menuitem", { name: "Rename view…" }));
  const renaming = await screen.findByRole("dialog", { name: "Rename view" });
  const name = within(renaming).getByRole("textbox", { name: "Name" });
  await user.clear(name);
  await user.type(name, "Sent");
  await user.click(within(renaming).getByRole("button", { name: "Rename" }));
  expect(facade.oneCall("RenameView")).toEqual([WORKSPACE_ROOT, "Outbound only", "Sent"]);

  facade.reply({ RemoveView: () => ({ state: "empty", views: [] }) });
  await waitFor(() => expect(screen.getByRole("button", { name: "Messages view" }).textContent).toBe("Sent"));
  await user.click(screen.getByRole("button", { name: "Messages view" }));
  await user.click(screen.getByRole("menuitem", { name: "Remove view…" }));
  const removing = await screen.findByRole("dialog", { name: "Remove view" });
  await user.click(within(removing).getByRole("button", { name: "Remove" }));
  expect(facade.oneCall("RemoveView")).toEqual([WORKSPACE_ROOT, "Sent"]);
  await waitFor(() => expect(screen.getByRole("button", { name: "Messages view" }).textContent).toBe("All messages"));
});

test("search settings prefill from the case index and save without naming a file", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    DescribeSearchSettings: () => ({
      state: "completed",
      settings: { fields: ["PID[1]-3[1]", "MSH[1]-10[1]"], retention: "digests", retain_until: "2099-01-01T00:00:00Z", expired: false },
    }),
    SaveSearchSettings: () => ({ state: "completed" }),
  });
  facade.reply({ SelectWorkspace: () => folderWithCase(), OpenCase: () => caseResult(), ReadMessages: () => ({ ...messagesResult([messageRow(GRID_OCCURRENCE)]), search_index: "expired" }) });
  await goTo(user, "Projects");
  await user.click(screen.getByRole("button", { name: "Open" }));
  await user.click(await findCaseRow());
  await findMessageRow(GRID_OCCURRENCE);
  await user.click(screen.getByRole("button", { name: "More message list actions" }));
  await user.click(screen.getByRole("menuitem", { name: "Search settings…" }));
  const sheet = await screen.findByRole("dialog", { name: "Search settings" });
  expect(facade.oneCall("DescribeSearchSettings")).toEqual([WORKSPACE_ROOT, CASE_ENTRY, CASE_IDENTITY]);
  expect((within(sheet).getByRole("textbox", { name: "Field 1" }) as HTMLInputElement).value).toBe("PID[1]-3[1]");
  expect((within(sheet).getByRole("textbox", { name: "Field 2" }) as HTMLInputElement).value).toBe("MSH[1]-10[1]");
  expect((within(sheet).getByRole("combobox", { name: "Search mode" }) as HTMLSelectElement).value).toBe("digests");
  // No file name, no replacement choice.
  expect(within(sheet).queryByText(/index|replace|\.json/i)).toBeNull();
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  const [request] = facade.oneCall("SaveSearchSettings");
  expect(request).toMatchObject({ workspace: WORKSPACE_ROOT, case: CASE_ENTRY, identity: CASE_IDENTITY, fields: ["PID[1]-3[1]", "MSH[1]-10[1]"], retention: "digests" });
  expect(Object.keys(request as object).sort()).toEqual(["case", "fields", "identity", "retain_until", "retention", "workspace"]);
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Search settings" })).toBeNull());
});

test("types read as the parsed code and trigger, ACK, Unparsed or Message, and an unknown time as a dash", () => {
  expect(typeLabel({ kind: "message", code: "SIU", trigger: "S13" })).toBe("SIU · S13");
  expect(typeLabel({ kind: "message", code: "", trigger: "" })).toBe("Message");
  expect(typeLabel({ kind: "ack", code: "ACK", trigger: "S12" })).toBe("ACK");
  expect(typeLabel({ kind: "unparsed", code: "", trigger: "" })).toBe("Unparsed");
  render(
    <List
      result={messagesResult([
        messageRow("a", "message", { message_code: "SIU", trigger_event: "S13", observed_at: "2026-01-01T12:00:10Z" }),
        messageRow("b", "message", { message_code: "", trigger_event: "", observed_at: null }),
        messageRow("c", "unparsed", { direction: "unknown" }),
      ])}
    />,
  );
  const table = screen.getByRole("table", { name: "Messages" });
  expect(within(table).getAllByRole("columnheader").map((cell) => cell.textContent)).toEqual(["Selected", "Time", "Type", "Source", "Direction"]);
  const cells = (id: string) => Array.from(table.querySelector(`[data-row-id="${id}"]`)!.querySelectorAll("td,th")).map((cell) => cell.textContent);
  expect(cells("a").slice(1)).toEqual(["12:00:10", "SIU · S13", "s0001", "Outbound"]);
  expect(cells("b").slice(1)).toEqual(["—", "Message", "s0001", "Outbound"]);
  expect(cells("c").slice(1)).toEqual(["12:00:00", "Unparsed", "s0001", "—"]);
});

test("an empty case, a filtered-empty list and a failed read each say what they are with one action", async () => {
  const user = userEvent.setup();
  const actions: string[] = [];
  const { rerender } = render(<List result={messagesResult([])} onAction={(action) => actions.push(action)} />);
  expect(screen.getByText("No messages")).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Import" }));

  rerender(<List result={messagesResult([], { state: "completed", total: 5, matched: 0 })} onAction={(action) => actions.push(action)} />);
  expect(screen.getByText("No matching messages")).toBeTruthy();
  expect(screen.getByRole("button", { name: "Clear filters" })).toBeTruthy();

  rerender(<List result={{ ...messagesResult([]), state: "failed", reason: "the case changed on disk" }} onAction={(action) => actions.push(action)} />);
  expect(screen.getByRole("alert").textContent).toContain("the case changed on disk");
  await user.click(screen.getByRole("button", { name: "Retry" }));
  expect(actions).toEqual(["import", "retry"]);
});

test("actions for selected messages appear only with a selection, and an ACK alone cannot be sent", async () => {
  const user = userEvent.setup();
  const actions: string[] = [];
  render(<List result={messagesResult([messageRow("m1"), messageRow("a1", "ack")])} onAction={(action) => actions.push(action)} />);
  expect(screen.queryByRole("button", { name: "Create test" })).toBeNull();
  await user.click(screen.getByRole("checkbox", { name: /SIU · S12/ }));
  const group = screen.getByRole("group", { name: "Selected messages" });
  await user.click(within(group).getByRole("button", { name: "Create test" }));
  expect(actions).toEqual(["test"]);
  await user.click(screen.getByRole("checkbox", { name: /SIU · S12/ }));
  await user.click(screen.getByRole("checkbox", { name: /ACK/ }));
  expect((within(screen.getByRole("group", { name: "Selected messages" })).getByRole("button", { name: "Send selected" }) as HTMLButtonElement).disabled).toBe(true);
});

test("a partly filled rule blocks Apply at its field, an end time is exclusive in UTC, and field states stay distinct", async () => {
  const user = userEvent.setup();
  const applied: GridQuery[] = [];
  render(<List result={messagesResult([messageRow("m1")])} onQuery={(query) => applied.push(query)} />);
  await user.click(screen.getByRole("button", { name: "Filter messages" }));
  const sheet = await screen.findByRole("dialog", { name: "Filter" });
  await user.selectOptions(within(sheet).getByRole("combobox", { name: "Field of rule 1" }), "Observed time");
  await user.selectOptions(within(sheet).getByRole("combobox", { name: "Operator of rule 1" }), "before");
  await user.click(within(sheet).getByRole("button", { name: "Apply" }));
  expect((await within(sheet).findByRole("alert")).textContent).toBe("Enter a complete date and time.");
  expect(applied).toEqual([]);

  const when = within(sheet).getByLabelText("Time of rule 1");
  await user.type(when, "2026-01-01T12:00:00");
  await user.click(within(sheet).getByRole("button", { name: "Add rule" }));
  await user.selectOptions(within(sheet).getByRole("combobox", { name: "Field of rule 2" }), "Message field");
  await user.selectOptions(within(sheet).getByRole("combobox", { name: "Operator of rule 2" }), "has state");
  await user.selectOptions(within(sheet).getByRole("combobox", { name: "Message field of rule 2" }), "PID-8 · Administrative Sex");
  await user.selectOptions(within(sheet).getByRole("combobox", { name: "State of rule 2" }), "Null");
  // A blank third rule is simply left out.
  await user.click(within(sheet).getByRole("button", { name: "Add rule" }));
  await user.click(within(sheet).getByRole("button", { name: "Apply" }));
  expect(applied).toEqual([
    { ...NO_QUERY, observed_until: "2026-01-01T12:00:00Z", fields: [{ selector: "PID[1]-8[1]", match: "state", term: "", state: "null" }] },
  ]);
  expect(screen.getByText("Before 2026-01-01 12:00:00 UTC")).toBeTruthy();
  expect(screen.getByText("PID[1]-8[1] is null")).toBeTruthy();
});

test("searching message content is literal, applies on Search and names its consequence", async () => {
  const user = userEvent.setup();
  const applied: GridQuery[] = [];
  render(<List result={messagesResult([messageRow("m1")])} onQuery={(query) => applied.push(query)} />);
  await user.click(screen.getByRole("button", { name: "Search messages" }));
  const sheet = await screen.findByRole("dialog", { name: "Search messages" });
  await user.type(within(sheet).getByRole("searchbox", { name: "Search" }), "S12|S13");
  expect(applied).toEqual([]);
  expect(within(sheet).queryByText("May contain patient data.")).toBeNull();
  await user.click(within(sheet).getByRole("radio", { name: "Message content" }));
  expect(within(sheet).getByText("May contain patient data.")).toBeTruthy();
  await user.click(within(sheet).getByRole("button", { name: "Search" }));
  expect(applied).toEqual([{ ...NO_QUERY, search: { scope: "content", text: "S12|S13" } }]);
});

test("Filter by this field opens Filter with the selected field's rule, and applies nothing by itself", async () => {
  const applied: GridQuery[] = [];
  render(<List result={messagesResult([messageRow("m1")])} seed={{ selector: "SCH[1]-11[1]", value: null, state: "present" }} onQuery={(query) => applied.push(query)} />);
  const sheet = await screen.findByRole("dialog", { name: "Filter" });
  await waitFor(() => expect((within(sheet).getByRole("combobox", { name: "Message field of rule 1" }) as HTMLSelectElement).value).toBe("SCH[1]-11[1]"));
  expect((within(sheet).getByRole("combobox", { name: "State of rule 1" }) as HTMLSelectElement).value).toBe("present");
  expect(applied).toEqual([]);
});

/** A reader over one inspection, answering Show values with the revealed read. */
function Reader({ hidden, shown, onFilter }: { hidden: InspectionResult; shown: InspectionResult; onFilter?: (seed: FilterSeed) => void }) {
  const [result, setResult] = useState(hidden);
  return (
    <MessageReader
      result={result}
      loading={false}
      busy={false}
      onInspect={async () => result}
      onReveal={(next) => setResult(next ? shown : hidden)}
      onFilterByField={(selector, value, state) => onFilter?.({ selector, value, state })}
    />
  );
}

const fieldNode = (path: string, state: "present" | "empty" | "null" | "omitted") => ({
  node: { path, kind: "field", parent: "SCH[1]", segment: "SCH", field: 11, state, start: 10, end: 20 },
  label: "Appointment timing",
  selector: `${path}[1]`,
  segment_name: "",
  value: "",
  truncated: false,
});

test("values stay hidden until Show values, and Empty, Null and Not present stay distinct", async () => {
  const user = userEvent.setup();
  const selected = { path: "SCH[1]", kind: "segment", parent: "", segment: "SCH", field: 0, state: "present" as const, start: 0, end: 60 };
  const children = [fieldNode("SCH[1]-11", "present"), fieldNode("SCH[1]-12", "empty"), fieldNode("SCH[1]-13", "null"), fieldNode("SCH[1]-14", "omitted")];
  const hidden = inspectionResult(GRID_OCCURRENCE, { selected, children, child_count: 4 });
  const shown = inspectionResult(GRID_OCCURRENCE, {
    selected,
    revealed: true,
    children: children.map((child, index) => (index === 0 ? { ...child, value: "20260101120000" } : child)),
    child_count: 4,
  });
  render(<Reader hidden={hidden} shown={shown} />);
  expect(screen.getByRole("heading", { name: "SIU · S12" })).toBeTruthy();
  const outline = () => within(screen.getByRole("list", { name: "Parts of SCH[1]" })).getAllByRole("button").map((row) => row.querySelector(".outline-value")?.textContent);
  expect(outline()).toEqual(["Hidden", "Empty", "Null", "Not present"]);
  expect(screen.getByText("May contain patient data.")).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Show values" }));
  expect(outline()).toEqual(["20260101120000", "Empty", "Null", "Not present"]);
  expect(screen.getByRole("button", { name: "Hide values" })).toBeTruthy();
});

test("Copy value exists only for a revealed selected field, and Filter by this field sends its selector", async () => {
  const user = userEvent.setup();
  const selected = { path: "SCH[1]-11", kind: "field", parent: "SCH[1]", segment: "SCH", field: 11, state: "present" as const, start: 10, end: 24 };
  const base = { selected, selector: "SCH[1]-11[1]", metadata: { label: "Appointment timing", status: "", hl7_version: "2.5.1", contract: "", provenance: "" } };
  const hidden = inspectionResult(GRID_OCCURRENCE, base);
  const shown = inspectionResult(GRID_OCCURRENCE, { ...base, revealed: true, decoded: "20260101120000", raw: "20260101120000" });
  const seeds: FilterSeed[] = [];
  render(<Reader hidden={hidden} shown={shown} onFilter={(seed) => seeds.push(seed)} />);
  expect(screen.queryByRole("button", { name: "Copy value" })).toBeNull();
  await user.click(screen.getByRole("button", { name: "Filter by this field" }));
  await user.click(screen.getByRole("button", { name: "Show values" }));
  expect(screen.getByRole("button", { name: "Copy value" })).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Filter by this field" }));
  expect(seeds).toEqual([
    { selector: "SCH[1]-11[1]", value: null, state: "present" },
    { selector: "SCH[1]-11[1]", value: "20260101120000", state: "present" },
  ]);
});

test("selecting a message reads it through the shared reader bound to the displayed case", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({ InspectOccurrence: () => inspectionResult() });
  await openCase(facade, user, [messageRow(GRID_OCCURRENCE), messageRow(NEXT_OCCURRENCE, "ack")]);
  await user.click(await findMessageRow(GRID_OCCURRENCE));
  expect(facade.oneCall("InspectOccurrence")[0]).toEqual({
    workspace: WORKSPACE_ROOT,
    case: CASE_ENTRY,
    identity: CASE_IDENTITY,
    occurrence: GRID_OCCURRENCE,
    path: "",
    node_offset: 0,
    byte_offset: -1,
    raw_offset: -1,
    reveal: false,
  });
  const details = await screen.findByRole("region", { name: "Message details" });
  expect(within(details).getByRole("heading", { name: "SIU · S12" })).toBeTruthy();
  // Closing the details keeps the list where it was.
  await user.click(within(details).getByRole("button", { name: "Close message details" }));
  expect(page().getByRole("table", { name: "Messages" })).toBeTruthy();
});

test("reading a case's messages never holds the window, so what a person types elsewhere is kept", async () => {
  const user = userEvent.setup();
  const reading = new Parked();
  const { facade } = await renderApp();
  facade.reply({ SelectWorkspace: () => folderWithCase(), OpenCase: () => caseResult(), ReadMessages: () => reading.arrive() as Promise<MessagesResult> });
  await goTo(user, "Projects");
  await user.click(screen.getByRole("button", { name: "Open" }));
  await user.click(await findCaseRow());
  await waitFor(() => expect(reading.size).toBe(1));
  // The read is still out, and the window is not busy: no operation holds it.
  expect(document.querySelector(".sidebar .operation")).toBeNull();
  reading.resolve(messagesResult([messageRow(GRID_OCCURRENCE)]));
  await findMessageRow(GRID_OCCURRENCE);
});

test("a busy answer while the list is still reading is asked again", async () => {
  const user = userEvent.setup();
  let asked = 0;
  const { facade } = await renderApp({
    InspectOccurrence: () => (++asked === 1 ? { state: "busy", reason: "another operation is running" } : inspectionResult()),
  });
  await openCase(facade, user, [messageRow(GRID_OCCURRENCE)]);
  await user.click(await findMessageRow(GRID_OCCURRENCE));
  const details = await screen.findByRole("region", { name: "Message details" });
  expect(await within(details).findByRole("heading", { name: "SIU · S12" })).toBeTruthy();
  expect(facade.callsTo("InspectOccurrence")).toHaveLength(2);
  expect(within(details).queryByText("another operation is running")).toBeNull();
});

test("Search settings is offered only when this case's own index has expired or cannot answer", async () => {
  const user = userEvent.setup();
  const { rerender } = render(<List result={messagesResult([messageRow("m1")])} />);
  await user.click(screen.getByRole("button", { name: "More message list actions" }));
  expect(screen.queryByRole("menuitem", { name: "Search settings…" })).toBeNull();
  await user.keyboard("{Escape}");
  rerender(<List result={{ ...messagesResult([messageRow("m1")]), search_index: "insufficient" }} />);
  await user.click(screen.getByRole("button", { name: "More message list actions" }));
  expect(screen.getByRole("menuitem", { name: "Search settings…" })).toBeTruthy();
});

test("a source reads by its declared name and filters by its exact ID", async () => {
  const user = userEvent.setup();
  const applied: GridQuery[] = [];
  const named = { ...messageRow("m1"), source_id: "s0002", source_name: "Scheduler" };
  const result = { ...messagesResult([named, messageRow("m2")]), facets: { types: [], ack_codes: [], sources: [{ id: "s0001", name: "" }, { id: "s0002", name: "Scheduler" }] } };
  render(<List result={result} onQuery={(query) => applied.push(query)} />);
  const table = screen.getByRole("table", { name: "Messages" });
  expect(within(table).getByText("Scheduler")).toBeTruthy();
  expect(within(table).getByText("s0001")).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Filter messages" }));
  const sheet = await screen.findByRole("dialog", { name: "Filter" });
  await user.selectOptions(within(sheet).getByRole("combobox", { name: "Field of rule 1" }), "Source");
  await user.click(within(sheet).getByRole("checkbox", { name: "Scheduler" }));
  await user.click(within(sheet).getByRole("button", { name: "Apply" }));
  expect(applied.at(-1)).toEqual({ ...NO_QUERY, sources: ["s0002"] });
  expect(screen.getByText("Source is Scheduler")).toBeTruthy();
});

test("the Message field rule picks a field by name and applies its exact path", async () => {
  const user = userEvent.setup();
  const applied: GridQuery[] = [];
  render(<List result={messagesResult([messageRow("m1")])} onQuery={(query) => applied.push(query)} />);
  await user.click(screen.getByRole("button", { name: "Filter messages" }));
  const sheet = await screen.findByRole("dialog", { name: "Filter" });
  await user.selectOptions(within(sheet).getByRole("combobox", { name: "Field of rule 1" }), "Message field");
  const picker = within(sheet).getByRole("combobox", { name: "Message field of rule 1" });
  await waitFor(() => expect((picker as HTMLSelectElement).disabled).toBe(false));
  expect(within(picker).getAllByRole("option").map((option) => option.textContent)).toEqual([
    "Choose a field",
    "MSH-10 · Message Control ID",
    "SCH-11 · Appointment timing quantity",
    "PID-8 · Administrative Sex",
    "Other field…",
  ]);
  // A listed field is picked; no path box shows until another is asked for.
  expect(within(sheet).queryByRole("textbox", { name: /Field path/ })).toBeNull();
  await user.selectOptions(picker, "MSH-10 · Message Control ID");
  await user.type(within(sheet).getByRole("textbox", { name: "Value of rule 1" }), "CTRL");
  // A component the list does not hold is typed as its exact path.
  await user.click(within(sheet).getByRole("button", { name: "Add rule" }));
  await user.selectOptions(within(sheet).getByRole("combobox", { name: "Field of rule 2" }), "Message field");
  await user.selectOptions(within(sheet).getByRole("combobox", { name: "Operator of rule 2" }), "has state");
  await user.selectOptions(within(sheet).getByRole("combobox", { name: "Message field of rule 2" }), "Other field…");
  await user.click(within(sheet).getByRole("textbox", { name: "Field path of rule 2" }));
  await user.paste("PID[1]-3[1].1");
  await user.click(within(sheet).getByRole("button", { name: "Apply" }));
  expect(applied.at(-1)).toEqual({
    ...NO_QUERY,
    fields: [
      { selector: "MSH[1]-10[1]", match: "equals", term: "CTRL", state: "" },
      { selector: "PID[1]-3[1].1", match: "state", term: "", state: "present" },
    ],
  });
});

test("Type is and Type is not can both be set, but not two rules of either", async () => {
  const user = userEvent.setup();
  const applied: GridQuery[] = [];
  const result = { ...messagesResult([messageRow("m1")]), facets: { sources: [], ack_codes: [], types: [{ kind: "message" as const, code: "SIU", trigger: "S12" }, { kind: "message" as const, code: "SIU", trigger: "S13" }] } };
  render(<List result={result} onQuery={(query) => applied.push(query)} />);
  await user.click(screen.getByRole("button", { name: "Filter messages" }));
  const sheet = await screen.findByRole("dialog", { name: "Filter" });
  await user.selectOptions(within(sheet).getByRole("combobox", { name: "Field of rule 1" }), "Type");
  await user.click(within(within(sheet).getByRole("group", { name: "Values of rule 1" })).getByRole("checkbox", { name: "SIU · S12" }));
  await user.click(within(sheet).getByRole("button", { name: "Add rule" }));
  await user.selectOptions(within(sheet).getByRole("combobox", { name: "Field of rule 2" }), "Type");
  // The second Type rule starts on the operator still free, and the used one is not offered.
  expect((within(sheet).getByRole("combobox", { name: "Operator of rule 2" }) as HTMLSelectElement).value).toBe("is-not");
  expect((within(within(sheet).getByRole("combobox", { name: "Operator of rule 2" })).getByRole("option", { name: "is" }) as HTMLOptionElement).disabled).toBe(true);
  await user.click(within(within(sheet).getByRole("group", { name: "Values of rule 2" })).getByRole("checkbox", { name: "SIU · S13" }));
  await user.click(within(sheet).getByRole("button", { name: "Add rule" }));
  const used = within(within(sheet).getByRole("combobox", { name: "Field of rule 3" })).getByRole("option", { name: "Type · already in a rule" }) as HTMLOptionElement;
  expect(used.disabled).toBe(true);
  await user.click(within(sheet).getByRole("button", { name: "Apply" }));
  expect(applied.at(-1)).toMatchObject({ types: [{ kind: "message", code: "SIU", trigger: "S12" }], not_types: [{ kind: "message", code: "SIU", trigger: "S13" }] });
});

test("the could-not-be-matched and not-decoded counts open exactly those rows as a removable filter", async () => {
  const user = userEvent.setup();
  const applied: GridQuery[] = [];
  const result = { ...messagesResult([messageRow("m1")]), undecided: 2, undecodable: 1 };
  render(<List result={result} onQuery={(query) => applied.push(query)} />);
  await user.click(screen.getByRole("button", { name: "2 could not be matched" }));
  expect(applied.at(-1)).toEqual({ ...NO_QUERY, scope: "undecided" });
  await user.click(screen.getByRole("button", { name: "Remove Could not be matched" }));
  expect(applied.at(-1)).toEqual({ ...NO_QUERY, scope: "" });
  await user.click(screen.getByRole("button", { name: "1 not decoded" }));
  expect(applied.at(-1)).toEqual({ ...NO_QUERY, scope: "undecodable" });
});

test("Columns shows Kind and hides Direction from the list's own menu", async () => {
  const user = userEvent.setup();
  render(<List result={messagesResult([messageRow("m1"), messageRow("m2", "ack")])} />);
  const headers = () => within(screen.getByRole("table", { name: "Messages" })).getAllByRole("columnheader").map((cell) => cell.textContent);
  expect(headers()).not.toContain("Kind");
  await user.click(screen.getByRole("button", { name: "More message list actions" }));
  await user.click(screen.getByRole("menuitem", { name: "Columns…" }));
  const sheet = within(await screen.findByRole("dialog", { name: "Columns" }));
  await user.click(sheet.getByRole("checkbox", { name: "Kind" }));
  await user.click(sheet.getByRole("checkbox", { name: "Direction" }));
  await user.click(sheet.getByRole("button", { name: "Apply" }));
  expect(headers()).toContain("Kind");
  expect(headers()).not.toContain("Direction");
  const table = screen.getByRole("table", { name: "Messages" });
  expect(within(table).getByText("Message")).toBeTruthy();
  expect(within(table).getAllByText("ACK").length).toBeGreaterThan(0);
});

test("Raw shows the whole message with the selected field marked, a window at a time, and the reader names direction", async () => {
  const user = userEvent.setup();
  const selected = { path: "SCH[1]-11", kind: "field", parent: "SCH[1]", segment: "SCH", field: 11, state: "present" as const, start: 10, end: 24 };
  const window = (offset: number) => ({ offset, end: Math.min(offset + 4096, 9000), message_start: 0, message_end: 9000, before: `text ${offset} `, selected: offset === 0 ? "FIELD" : "", after: " more" });
  const hidden = inspectionResult(GRID_OCCURRENCE, { selected, direction: "outbound" });
  const pages: number[] = [];
  function Paged() {
    const [result, setResult] = useState(hidden);
    return (
      <MessageReader
        result={result}
        loading={false}
        busy={false}
        onInspect={async (_path, _node, _byte, raw = -1) => {
          pages.push(raw);
          const next = inspectionResult(GRID_OCCURRENCE, { selected, direction: "outbound", revealed: true, raw_window: window(raw < 0 ? 0 : raw) });
          setResult(next);
          return next;
        }}
        onReveal={(next) => setResult(next ? inspectionResult(GRID_OCCURRENCE, { selected, direction: "outbound", revealed: true, raw_window: window(0) }) : hidden)}
      />
    );
  }
  render(<Paged />);
  expect(screen.getByText(/Outbound/)).toBeTruthy();
  await user.click(screen.getByRole("tab", { name: "Raw" }));
  expect(screen.getByText("Hidden")).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Show values" }));
  expect(screen.getByText("FIELD").tagName).toBe("MARK");
  expect(screen.getByText("1–4096 of 9000 bytes")).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Next raw text" }));
  expect(pages).toEqual([4096]);
  expect(await screen.findByText("4097–8192 of 9000 bytes")).toBeTruthy();
});

test("a slow read of one message answered after the next was chosen never shows its fields under the next", async () => {
  const user = userEvent.setup();
  const first = new Parked();
  const { facade } = await renderApp({
    InspectOccurrence: (request) =>
      request.occurrence === GRID_OCCURRENCE ? (first.arrive() as Promise<InspectionResult>) : inspectionResult(NEXT_OCCURRENCE, { message_code: "ACK", trigger_event: "", source_id: "s0002" }),
  });
  await openCase(facade, user, [messageRow(GRID_OCCURRENCE), messageRow(NEXT_OCCURRENCE)]);
  await user.click(await findMessageRow(GRID_OCCURRENCE));
  await waitFor(() => expect(first.size).toBe(1));
  await user.click(await findMessageRow(NEXT_OCCURRENCE));
  const details = await screen.findByRole("region", { name: "Message details" });
  expect(await within(details).findByRole("heading", { name: "ACK" })).toBeTruthy();
  // The first read answers late: the reader stays on the message now chosen.
  first.resolve(inspectionResult(GRID_OCCURRENCE));
  await new Promise((settle) => setTimeout(settle, 50));
  expect(within(details).queryByRole("heading", { name: "SIU · S12" })).toBeNull();
  expect(within(details).getByRole("heading", { name: "ACK" })).toBeTruthy();
});

test("every part of a message is reached from the keyboard, and Raw and Hex stay on the occurrence chosen", async () => {
  const user = userEvent.setup();
  const segments = inspectionResult(GRID_OCCURRENCE, {
    children: [
      { node: { path: "MSH[1]", kind: "segment", parent: "", segment: "MSH", field: 0, state: "present", start: 0, end: 40 }, label: "", selector: "", segment_name: "Message header", value: "", truncated: false },
      { node: { path: "SCH[1]", kind: "segment", parent: "", segment: "SCH", field: 0, state: "present", start: 41, end: 90 }, label: "", selector: "", segment_name: "Scheduling activity information", value: "", truncated: false },
    ],
    child_count: 2,
  });
  const { facade } = await renderApp({ InspectOccurrence: (request) => (request.path === "" ? segments : inspectionResult(GRID_OCCURRENCE, { selected: { path: request.path, kind: "segment", parent: "", segment: "SCH", field: 0, state: "present", start: 41, end: 90 } })) });
  await openCase(facade, user, [messageRow(GRID_OCCURRENCE)]);
  const row = await findMessageRow(GRID_OCCURRENCE);
  row.focus();
  await user.keyboard("{Enter}");
  const details = await screen.findByRole("region", { name: "Message details" });
  const sch = await within(details).findByRole("button", { name: /SCH/ });
  // Tab reaches each part in order; Enter opens it.
  while (document.activeElement !== sch) await user.tab();
  await user.keyboard("{Enter}");
  await waitFor(() => expect(facade.callsTo("InspectOccurrence").at(-1)?.args[0]).toMatchObject({ occurrence: GRID_OCCURRENCE, path: "SCH[1]" }));
  // The views are keyboard tabs over the same occurrence.
  await user.click(within(details).getByRole("tab", { name: "Fields" }));
  await user.keyboard("{ArrowRight}");
  expect(within(details).getByRole("tab", { name: "Raw", selected: true })).toBeTruthy();
  await user.keyboard("{ArrowRight}");
  expect(within(details).getByRole("tab", { name: "Hex", selected: true })).toBeTruthy();
  for (const call of facade.callsTo("InspectOccurrence")) expect(call.args[0]).toMatchObject({ occurrence: GRID_OCCURRENCE });
});

test("Create variant from checked messages opens the variant editor with exactly them included", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    MessageFields: () => ({ state: "completed", fields: [], complete: true }),
    ResolveVariant: (request) => ({ state: "completed", context: request.context, problems: [], variant: { messages: [], sequence: [], changes: [], relations: [], profile: [], notes: [], blocking: [], revealed: false } }),
  });
  await openCase(facade, user, [messageRow(GRID_OCCURRENCE), messageRow(NEXT_OCCURRENCE)]);
  for (const id of [GRID_OCCURRENCE, NEXT_OCCURRENCE]) {
    await user.click(within(await findMessageRow(id)).getByRole("checkbox"));
  }
  await user.click(screen.getByRole("button", { name: "Create variant" }));
  expect(await screen.findByRole("heading", { name: "Case variant" })).toBeTruthy();
  await waitFor(() => expect(facade.callsTo("ResolveVariant").length).toBeGreaterThan(0));
  const resolved = facade.callsTo("ResolveVariant")[0]!.args[0] as { draft: { plan: { steps: unknown[] } } };
  // A new plan holding exactly the chosen messages, and nothing kept as a
  // draft until the person edits it.
  expect(resolved.draft.plan.steps).toEqual([
    { operator: "select-occurrence/v1", occurrence: GRID_OCCURRENCE },
    { operator: "select-occurrence/v1", occurrence: NEXT_OCCURRENCE },
  ]);
  expect(facade.callsTo("SaveEditorDraft")).toHaveLength(0);
  expect(facade.callsTo("EditReproducer")).toHaveLength(0);
});

test("Go to field validates the path against the selected message and keeps it when it is not there", async () => {
  const user = userEvent.setup();
  const asked: string[] = [];
  render(
    <MessageReader
      result={inspectionResult(GRID_OCCURRENCE)}
      loading={false}
      busy={false}
      onInspect={async (path) => {
        asked.push(path);
        return path === "PID-3" ? inspectionResult(GRID_OCCURRENCE) : { state: "failed", reason: "PID-99 is not in this message" };
      }}
      onReveal={() => undefined}
    />,
  );
  await user.click(screen.getByRole("button", { name: "More message actions" }));
  await user.click(screen.getByRole("menuitem", { name: "Go to field…" }));
  const sheet = within(await screen.findByRole("dialog", { name: "Go to field" }));
  await user.type(sheet.getByLabelText("Field path"), "PID-99{Enter}");
  expect(await sheet.findByText("PID-99 is not in this message")).toBeTruthy();
  expect((sheet.getByLabelText("Field path") as HTMLInputElement).value).toBe("PID-99");
  await user.clear(sheet.getByLabelText("Field path"));
  await user.type(sheet.getByLabelText("Field path"), "PID-3{Enter}");
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Go to field" })).toBeNull());
  expect(asked).toEqual(["PID-99", "PID-3"]);
});

test("a later page answered busy is asked again and never replaces the rows already read", async () => {
  const user = userEvent.setup();
  const first = Array.from({ length: 10 }, (_, at) => messageRow(`m${at}`));
  const next = Array.from({ length: 10 }, (_, at) => messageRow(`n${at}`));
  let later = 0;
  const { facade } = await renderApp();
  facade.reply({
    SelectWorkspace: () => folderWithCase(),
    OpenCase: () => caseResult(),
    ReadMessages: (request) =>
      request.offset === 0
        ? { ...messagesResult(first), total: 20, matched: 20 }
        : ++later <= 2
          ? ({ state: "busy", reason: "another operation is running", rows: null } as unknown as MessagesResult)
          : { ...messagesResult(next), total: 20, matched: 20 },
  });
  await goTo(user, "Projects");
  await user.click(screen.getByRole("button", { name: "Open" }));
  await user.click(await findCaseRow());
  await findMessageRow("m0");
  // The list asked for its next window as its last rows were drawn.
  await waitFor(() => expect(document.querySelector('[data-row-id="n0"]')).toBeTruthy(), { timeout: 10_000 });
  expect(await findMessageRow("m0")).toBeTruthy();
  expect(later).toBeGreaterThanOrEqual(3);
}, 15_000);

test("a later page that is refused keeps the rows read, says why and asks again from Retry", async () => {
  const user = userEvent.setup();
  const first = Array.from({ length: 10 }, (_, at) => messageRow(`m${at}`));
  const next = Array.from({ length: 10 }, (_, at) => messageRow(`n${at}`));
  let later = 0;
  const { facade } = await renderApp();
  facade.reply({
    SelectWorkspace: () => folderWithCase(),
    OpenCase: () => caseResult(),
    ReadMessages: (request) =>
      request.offset === 0
        ? { ...messagesResult(first), total: 20, matched: 20 }
        : ++later === 1
          ? { ...messagesResult([]), state: "failed", reason: "the case changed on disk since it was opened" }
          : { ...messagesResult(next), total: 20, matched: 20 },
  });
  await goTo(user, "Projects");
  await user.click(screen.getByRole("button", { name: "Open" }));
  await user.click(await findCaseRow());
  await findMessageRow("m0");
  expect(await screen.findByText("the case changed on disk since it was opened")).toBeTruthy();
  expect(document.querySelector('[data-row-id="m9"]')).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Retry" }));
  await waitFor(() => expect(document.querySelector('[data-row-id="n0"]')).toBeTruthy());
  expect(screen.queryByText("the case changed on disk since it was opened")).toBeNull();
});

test("Send selected hands over exactly the checked message rows, and a row's checkbox is reached from the keyboard", async () => {
  const user = userEvent.setup();
  const actions: string[] = [];
  render(<List result={messagesResult([messageRow("m1"), messageRow("m2"), messageRow("a1", "ack")])} onAction={(action) => actions.push(action)} />);
  const table = screen.getByRole("table", { name: "Messages" });
  const row = within(table).getAllByRole("row")[1]!;
  row.focus();
  await user.keyboard(" ");
  expect((within(row).getByRole("checkbox") as HTMLInputElement).checked).toBe(true);
  await user.click(screen.getByRole("button", { name: "Send selected" }));
  expect(actions).toEqual(["send"]);
});
