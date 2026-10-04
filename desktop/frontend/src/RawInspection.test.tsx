// Tools → Inspect file: a standalone message file opened through the host's
// dialog and read through the same reader as a case's messages, with no
// project. The fixtures carry positions, types and digests, never a value.
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, test } from "vitest";
import type { FileMessage, FileMessagesRequest, FileMessagesResult } from "./bindings";
import { renderApp } from "./testkit/app";
import { WORKSPACE_ROOT, inspectionResult } from "./testkit/fixtures";
import { goTo, page } from "./testkit/navigation";
import type { FacadeHandlers } from "./testkit/wails";

const FILE = `${WORKSPACE_ROOT}/raw/appointments.hl7`;
const COPY = `${WORKSPACE_ROOT}/copies/appointments.hl7`;
const DIGEST = "0".repeat(64);

function listing(count: number, request?: FileMessagesRequest): FileMessagesResult {
  const rows: FileMessage[] = Array.from({ length: count }, (_, index) => ({ index, message_code: "SIU", trigger_event: index === 0 ? "S12" : "S13", start: index * 100, end: index * 100 + 90 }));
  return {
    state: "completed",
    name: "appointments.hl7",
    bytes: count * 100,
    sha256: DIGEST,
    format: request?.format === "auto" || !request ? "raw" : request.format,
    terminator: "cr",
    format_selection: request?.format === "auto" || !request ? "detected" : "declared",
    terminator_selection: "detected",
    total: count,
    offset: 0,
    rows,
  };
}

function handlers(extra: FacadeHandlers = {}): FacadeHandlers {
  return {
    ChooseInspectionPath: (kind) => ({ state: "completed", kind, path: kind === "file" ? FILE : COPY }),
    ListFileMessages: (request) => listing(3, request),
    InspectFileMessage: (request) => inspectionResult("", { message: request.message, occurrence: "", identity: "" }),
    ...extra,
  };
}

async function openTool(user: ReturnType<typeof userEvent.setup>) {
  await goTo(user, "Tools");
  await user.click(screen.getByRole("button", { name: "Inspect file" }));
}

test("the standalone file opens natively into the shared reader and a refused parse keeps its bytes", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(handlers());
  await openTool(user);
  // Choosing the tool goes straight to the host's Open dialog.
  await waitFor(() => expect(facade.oneCall("ChooseInspectionPath")).toEqual(["file", ""]));
  expect(await page().findByRole("heading", { name: "appointments.hl7" })).toBeTruthy();
  expect(facade.oneCall("ListFileMessages")[0]).toEqual({ file: FILE, format: "auto", terminator: "auto", offset: 0, limit: 0 });

  const table = await screen.findByRole("table", { name: "Messages in this file" });
  await user.click(table.querySelector<HTMLElement>('[data-row-id="1"]')!);
  expect(facade.oneCall("InspectFileMessage")[0]).toEqual({
    file: FILE,
    format: "auto",
    terminator: "auto",
    expect: DIGEST,
    message: 1,
    path: "",
    node_offset: 0,
    byte_offset: -1,
    raw_offset: -1,
    reveal: false,
  });
  const details = await screen.findByRole("region", { name: "Message details" });
  expect(within(details).queryByRole("tab", { name: "Fields" })).toBeNull();
  expect(within(details).getByRole("tabpanel", { name: "Fields" })).toBeTruthy();
  await user.click(within(details).getByRole("button", { name: "More message actions" }));
  await user.click(screen.getByRole("menuitem", { name: "Hex" }));
  expect(within(details).getByRole("tab", { name: "Hex", selected: true })).toBeTruthy();

  // A file the chosen format cannot parse keeps its original bytes and offers
  // Change format; nothing is repaired.
  facade.reply({
    ListFileMessages: () => ({ ...listing(0), state: "failed", reason: "no MSH segment at the start of the file", total: 0, rows: [] }),
    ReadFileBytes: (request) => ({ state: "completed", bytes: 32, offset: 0, rows: [{ offset: 0, hex: request.reveal ? "4d 53 48" : "", text: "" }] }),
  });
  await user.click(page().getByRole("button", { name: "More file actions" }));
  await user.click(screen.getByRole("menuitem", { name: "Format…" }));
  const sheet = await screen.findByRole("dialog", { name: "Format" });
  await user.selectOptions(within(sheet).getByRole("combobox", { name: "Framing" }), "MLLP");
  await user.selectOptions(within(sheet).getByRole("combobox", { name: "Segment terminator" }), "LF");
  await user.click(within(sheet).getByRole("button", { name: "Apply" }));
  expect(facade.callsTo("ListFileMessages")[1]!.args[0]).toMatchObject({ format: "mllp", terminator: "lf" });
  expect((await page().findByRole("alert")).textContent).toBe("no MSH segment at the start of the file");
  expect(page().getByRole("button", { name: "Change format" })).toBeTruthy();
  expect(facade.oneCall("ReadFileBytes")[0]).toEqual({ file: FILE, expect: DIGEST, offset: 0, reveal: false });
  // The original bytes are values too: they appear once Show values is chosen.
  expect(page().queryByText("4d 53 48")).toBeNull();
  await user.click(page().getByRole("button", { name: "Show values" }));
  expect(await page().findByText("4d 53 48")).toBeTruthy();
  expect(facade.callsTo("ReadFileBytes")[1]!.args[0]).toMatchObject({ reveal: true });
});

test("save copy writes a byte-identical new file through the native save dialog and a refused copy says why", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(
    handlers({ ListFileMessages: (request) => listing(1, request), SaveFileCopy: () => ({ state: "completed", path: COPY, bytes: 100, sha256: DIGEST }) }),
  );
  await openTool(user);
  // A file with one message opens straight into the reader.
  expect(await screen.findByRole("region", { name: "Message details" })).toBeTruthy();
  await user.click(page().getByRole("button", { name: "More file actions" }));
  await user.click(screen.getByRole("menuitem", { name: "Save copy…" }));
  await waitFor(() => expect(facade.callsTo("ChooseInspectionPath").map((call) => call.args)).toEqual([["file", ""], ["copy-destination", FILE]]));
  expect(facade.oneCall("SaveFileCopy")[0]).toEqual({ file: FILE, format: "auto", terminator: "auto", expect: DIGEST, destination: COPY });
  // A saved copy is silent.
  expect(page().queryByRole("alert")).toBeNull();

  facade.reply({ SaveFileCopy: () => ({ state: "failed", reason: "the destination already exists" }) });
  await user.click(page().getByRole("button", { name: "More file actions" }));
  await user.click(screen.getByRole("menuitem", { name: "Save copy…" }));
  expect((await page().findByRole("alert")).textContent).toBe("the destination already exists");
});

test("a single-message source keeps its browser and closing details does not immediately reopen it", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(handlers({ ListFileMessages: (request) => listing(1, request) }));
  await openTool(user);
  const reader = within(await screen.findByRole("region", { name: "Message details" }));
  expect(screen.getByRole("table", { name: "Messages in this file" }).querySelector('[data-row-id="0"]')).toBeTruthy();
  await user.click(reader.getByRole("button", { name: "Close message details" }));
  await waitFor(() => expect(screen.queryByRole("region", { name: "Message details" })).toBeNull());
  expect(facade.callsTo("InspectFileMessage")).toHaveLength(1);
  await user.click(screen.getByRole("table", { name: "Messages in this file" }).querySelector<HTMLElement>('[data-row-id="0"]')!);
  expect(await screen.findByRole("region", { name: "Message details" })).toBeTruthy();
  expect(facade.callsTo("InspectFileMessage")).toHaveLength(2);
});

test("large loose-file paging keeps checked positions and refuses a changed source before appending",async()=>{
 const user=userEvent.setup();
 const {facade}=await renderApp(handlers({ListFileMessages:request=>({...listing(401,request),offset:request.offset,rows:listing(401,request).rows.slice(request.offset,request.offset+200)})}));
 await openTool(user);
 const table=await screen.findByRole("table",{name:"Messages in this file"});
 await user.click(table.querySelector<HTMLInputElement>('[data-row-id="1"] input[type="checkbox"]')!);
 await user.click(page().getByRole("button",{name:"Load more messages"}));
 await waitFor(()=>expect(facade.callsTo("ListFileMessages")[1]?.args[0]).toMatchObject({offset:200}));
 expect(table.querySelector<HTMLInputElement>('[data-row-id="1"] input[type="checkbox"]')?.checked).toBe(true);
 facade.reply({ListFileMessages:request=>({...listing(401,request),sha256:"b".repeat(64),offset:request.offset,rows:[{index:400,message_code:"ADT",trigger_event:"A01",start:40000,end:40090}]})});
 await user.click(page().getByRole("button",{name:"Load more messages"}));
 expect(await page().findByText("The source changed while paging. The previous selection is kept; reopen the file explicitly.")).toBeTruthy();
 expect(table.querySelector<HTMLInputElement>('[data-row-id="1"] input[type="checkbox"]')?.checked).toBe(true);
});

test("browser metadata search and grouping keep the original checked occurrence",async()=>{
  const user=userEvent.setup();
  const {facade}=await renderApp(handlers({ListFileMessages:request=>{const result=listing(2,request);result.rows[1]={...result.rows[1]!,message_code:"ADT",trigger_event:"A08"};return result;}}));
  await openTool(user);
  const table=await screen.findByRole("table",{name:"Messages in this file"});
  await user.click(table.querySelector<HTMLInputElement>('[data-row-id="1"] input[type="checkbox"]')!);
  await user.click(table.querySelector<HTMLElement>('[data-row-id="0"]')!);
  await screen.findByRole("region",{name:"Message details"});
  await user.click(screen.getByRole("button",{name:"Compact view"}));
  expect(screen.getByRole("button",{name:"Wide view"})).toBeTruthy();
  expect(facade.callsTo("InspectFileMessage")).toHaveLength(1);
  await user.click(screen.getByRole("tab",{name:"By type"}));
  expect([...table.querySelectorAll<HTMLElement>('tbody tr[data-row-id]')].map(row=>row.dataset.rowId)).toEqual(["1","0"]);
  await user.type(screen.getByRole("textbox",{name:"Search messages"}),"SIU");
  expect([...table.querySelectorAll<HTMLElement>('tbody tr[data-row-id]')].map(row=>row.dataset.rowId)).toEqual(["0"]);
  await user.clear(screen.getByRole("textbox",{name:"Search messages"}));
  expect(table.querySelector<HTMLInputElement>('[data-row-id="1"] input[type="checkbox"]')?.checked).toBe(true);
});
