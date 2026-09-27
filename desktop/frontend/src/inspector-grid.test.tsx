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
  await openCase(facade, user, [messageRow(GRID_OCCURRENCE)]);
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
  await user.click(within(sheet).getByRole("textbox", { name: "Field path of rule 2" }));
  await user.paste("PID[1]-8[1]");
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
  expect((within(sheet).getByRole("textbox", { name: "Field path of rule 1" }) as HTMLInputElement).value).toBe("SCH[1]-11[1]");
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
