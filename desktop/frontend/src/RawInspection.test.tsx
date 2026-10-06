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

test("reader navigation expands and collapses without rereading or duplicating the message heading", async () => {
  const user=userEvent.setup();
  const {facade}=await renderApp(handlers({ListFileMessages:request=>listing(1,request)}));
  await openTool(user);
  await screen.findByRole("region",{name:"Message details"});
  const count=facade.callsTo("InspectFileMessage").length;
  const expand=screen.getByRole("button",{name:"Expand navigation"});
  expect(expand.getAttribute("aria-expanded")).toBe("false");
  await user.click(expand);
  expect(screen.getByRole("button",{name:"Collapse navigation"}).getAttribute("aria-expanded")).toBe("true");
  expect(screen.getAllByRole("heading",{name:"Messages"})).toHaveLength(1);
  expect(screen.getByRole("textbox",{name:"Search messages"})).toBeTruthy();
  await user.click(screen.getByRole("button",{name:"Collapse navigation"}));
  expect(screen.getByRole("button",{name:"Expand navigation"})).toBeTruthy();
  expect(facade.callsTo("InspectFileMessage")).toHaveLength(count);
});

async function openTool(user: ReturnType<typeof userEvent.setup>) {
  await goTo(user, "Tools");
  await user.click(screen.getByRole("button", { name: "Inspect file" }));
}

test("raw token and table navigation share canonical paths, preserve the raw page, and retain PHI masking", async () => {
  const user=userEvent.setup();
  const first={path:"MSH[1]-9",kind:"field",parent:"MSH[1]",segment:"MSH",field:9,state:"empty" as const,start:4106,end:4106};
  const left={path:"PID[1]-5[1].1",kind:"component",parent:"PID[1]-5[1]",segment:"PID",field:5,state:"empty" as const,start:4120,end:4120};
  const right={...left,path:"PID[1]-5[1].2",start:4130,end:4130};
  const nodes=[first,left,right];
  const {facade}=await renderApp(handlers({
    ListFileMessages:request=>listing(1,request),
    InspectFileMessage:request=>{
      const selected=nodes.find(node=>node.path===request.path) ?? first;
      return inspectionResult("",{identity:DIGEST,message:0,occurrence:"",selected,revealed:true,phi_masked:request.mask_phi ?? false,
        readable_window:{offset:4096,end:4196,message_start:0,message_end:5000,before:"",selected:"",after:"",lines:nodes.map((node,index)=>({number:20+index,tokens:[{text:"",path:node.path,start:node.start,end:node.end,empty:true,selected:node.path===selected.path}]}))},
        grid:{segment:"PID[1]",offset:0,field_count:3,rows:nodes.map((node,index)=>({node,label:`Position ${index+1}`,selector:node.path,segment_name:"",value:"",truncated:false,depth:2}))},
      });
    },
  }));
  await openTool(user);
  const source=await screen.findByRole("region",{name:"Original message"});
  expect(screen.queryByRole("heading",{name:"Original message"})).toBeNull();
  expect(screen.queryByRole("button",{name:/^Source:/})).toBeNull();
  expect(screen.getAllByRole("heading",{name:"Messages"})).toHaveLength(1);
  await user.click(within(source).getByRole("button",{name:`Inspect ${right.path} (empty)`}));
  await waitFor(()=>expect(facade.callsTo("InspectFileMessage").at(-1)?.args[0]).toMatchObject({path:right.path,raw_offset:4096,node_offset:0}));
  const grid=screen.getByRole("region",{name:"Segment grid"});
  await waitFor(()=>expect(within(grid).getByRole("button",{name:/PID\[1\]-5\[1\]\.2 Position 3/}).getAttribute("aria-current")).toBe("location"));
  await user.click(within(grid).getByRole("button",{name:/PID\[1\]-5\[1\]\.1 Position 2/}));
  await waitFor(()=>expect(within(source).getByRole("button",{name:`Inspect ${left.path} (empty)`}).getAttribute("aria-pressed")).toBe("true"));
  within(source).getByRole("button",{name:`Inspect ${left.path} (empty)`}).focus();
  await user.keyboard("{ArrowRight}{Enter}");
  await waitFor(()=>expect(facade.callsTo("InspectFileMessage").at(-1)?.args[0]).toMatchObject({path:right.path,raw_offset:4096}));
  await user.click(screen.getByRole("button",{name:"Enable PHI masking"}));
  await waitFor(()=>expect(facade.callsTo("InspectFileMessage").at(-1)?.args[0]).toMatchObject({mask_phi:true}));
  expect(screen.getByRole("button",{name:"Disable PHI masking"}).getAttribute("aria-pressed")).toBe("true");
});

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
    reveal: true,
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
  await user.click(screen.getByRole("button",{name:"More file actions"}));
 await user.click(screen.getByRole("menuitem",{name:"Compact view"}));
  await user.click(screen.getByRole("button",{name:"More file actions"}));
  expect(screen.getByRole("menuitem",{name:"Wide view"})).toBeTruthy();
  await user.keyboard("{Escape}");
  expect(facade.callsTo("InspectFileMessage")).toHaveLength(1);
  await user.click(screen.getByRole("tab",{name:"By type"}));
  expect([...table.querySelectorAll<HTMLElement>('tbody tr[data-row-id]')].map(row=>row.dataset.rowId)).toEqual(["1","0"]);
  await user.type(screen.getByRole("textbox",{name:"Search messages"}),"SIU");
  expect([...table.querySelectorAll<HTMLElement>('tbody tr[data-row-id]')].map(row=>row.dataset.rowId)).toEqual(["0"]);
  await user.clear(screen.getByRole("textbox",{name:"Search messages"}));
  expect(table.querySelector<HTMLInputElement>('[data-row-id="1"] input[type="checkbox"]')?.checked).toBe(true);
});


test("explicitly reopening the same file replaces its stale listing and identity", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(handlers({ ListFileMessages: request => listing(1, request) }));
  await openTool(user);
  await screen.findByRole("region", { name: "Message details" });
  facade.reply({ ListFileMessages: request => ({ ...listing(2, request), sha256: "b".repeat(64), bytes: 713 }) });
  await user.click(page().getByRole("button", { name: "Open file" }));
  await waitFor(() => expect(facade.callsTo("ListFileMessages")).toHaveLength(2));
  const table = await screen.findByRole("table", { name: "Messages in this file" });
  expect(table.querySelectorAll("tbody tr[data-row-id]")).toHaveLength(2);
  expect(screen.queryByRole("region", { name: "Message details" })).toBeNull();
  await user.click(table.querySelector<HTMLElement>('[data-row-id="1"]')!);
  await waitFor(() => expect(facade.callsTo("InspectFileMessage").at(-1)?.args[0]).toMatchObject({ expect: "b".repeat(64), message: 1 }));
});


test("Settings Back returns to the same loose-file selection without rereading it", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(handlers({ ListFileMessages: request => listing(1, request) }));
  await openTool(user);
  await screen.findByRole("region", { name: "Message details" });
  const reads = facade.callsTo("InspectFileMessage").length;
  await goTo(user, "Settings");
  await user.click(page().getByRole("button", { name: "Back" }));
  expect(await screen.findByRole("region", { name: "Message details" })).toBeTruthy();
  expect(facade.callsTo("InspectFileMessage")).toHaveLength(reads);
});


test("PHI masking keeps the active datatype reference and Components tab", async () => {
  const user=userEvent.setup();
  const missing={state:"not_specified",value:""};
  const record={key:"field/MSH/9",kind:"field",segment:"MSH",field:9,name:"Message Type",datatype:{state:"specified",value:"MSG"},optionality:missing,length:missing,conformance_length:missing,repetition:missing,item:missing,table:missing,section:missing,definition:"",source:"owned"};
  const reference={status:"available",reason:"",identity:"catalog",edition:"2.5.1",coverage:{segments:0,fields:1,definitions:0,missing:[]},missing_count:0,record,datatype_key:"datatype/MSG"};
  await renderApp(handlers({
    ListFileMessages:request=>listing(1,request),
    InspectFileMessage:request=>inspectionResult("",{identity:DIGEST,message:0,occurrence:"",selected:{path:"MSH[1]-9",kind:"field",parent:"MSH[1]",segment:"MSH",field:9,state:"empty",start:10,end:10},revealed:true,phi_masked:request.mask_phi??false,reference}),
    LookupHL7Reference:()=>({state:"completed",children:[],offset:0,child_count:0,total_count:0,reference:{...reference,record:{...record,kind:"datatype",name:"Message datatype"}}}),
  }));
  await openTool(user);
  await screen.findByRole("region",{name:"Message details"});
  await user.click(screen.getByRole("button",{name:"Datatype · MSG"}));
  await screen.findByRole("heading",{name:"Message datatype"});
  await user.click(screen.getByRole("tab",{name:"Components"}));
  await user.click(screen.getByRole("button",{name:"Enable PHI masking"}));
  await screen.findByRole("button",{name:"Disable PHI masking"});
  expect(screen.getByRole("tab",{name:"Components",selected:true})).toBeTruthy();
});


test("restored Settings hides Back when no return location was retained", async () => {
  await renderApp(handlers({
    WorkingSession:()=>({state:"completed",session:{schema:"readmit-desktop-session/v3",view:{workspace:"",case:"",region:"evidence",run:"",navigation:{destination:"settings",source_kind:"file",file:FILE,file_identity:DIGEST}}}}),
    ListFileMessages:request=>listing(1,request),
  }));
  await page().findByRole("heading",{level:1,name:"Settings"});
  expect(page().queryByRole("button",{name:"Back"})).toBeNull();
});
