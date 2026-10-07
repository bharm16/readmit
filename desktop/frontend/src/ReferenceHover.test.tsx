import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import { ReaderGrid } from "./ReaderGrid";
import { ReaderColumns } from "./ReaderColumns";
import { MessageReader } from "./Inspector";
import type { HL7ReferenceRequest, HL7ReferenceResult, Hl7referenceAttribute, Hl7referenceRecord, InspectorNode } from "./bindings";
import type { ReferenceHoverScope } from "./ReferenceHover";
import { installFacade } from "./testkit/wails";
import { WORKSPACE_ROOT, inspectionResult } from "./testkit/fixtures";

const specified = (value: string): Hl7referenceAttribute => ({ state: "specified", value });
const absent: Hl7referenceAttribute = { state: "not_specified", value: "" };
const field: Hl7referenceRecord = { key: "field/ZAA/1", kind: "field", segment: "ZAA", field: 1, name: "Owned field", datatype: specified("ZC"), optionality: specified("R"), length: specified("7"), conformance_length: absent, repetition: absent, item: specified("90001"), table: specified("0099"), section: specified("3.9.1"), definition: "Owned field definition.", source: "owned-source" };
const rows: InspectorNode[] = [1, 2].map(number => ({ node: { path: `ZAA[1]-${number}`, parent: "ZAA[1]", kind: "field", segment: "ZAA", field: number, state: "empty", start: number, end: number }, display_path: `ZAA-${number}`, reference: { ...field, key: `field/ZAA/${number}`, field: number }, label: "Owned field", selector: "", segment_name: "", value: "", truncated: false }));
function reply(request: HL7ReferenceRequest, name = "Owned datatype"): HL7ReferenceResult {
  const record: Hl7referenceRecord = request.attribute === "table" ? { ...field, key: "table/0099", kind: "table", name: "Owned table", table_id: "0099", table_kind: "hl7", content_state: "available", table_metadata: { description: "Owned terminology context.", binding: "3", table_oid: "2.16.840.1.9999", code_system_oid: "2.16.840.1.9998", value_set_oid: "2.16.840.1.9997", origin: { kind: "normative", source: "terminology", locator: "owned-table" } } } : { ...field, key: "datatype/ZC", kind: "datatype", container: "ZC", name };
  const codes: Hl7referenceRecord[] = [
    { ...field, key: "code/0099/QzAx", kind: "code", name: "C01", code: "C01", definition: "Owned alpha meaning." },
    { ...field, key: "code/0099/QzAy", kind: "code", name: "C02", code: "C02", definition: "Owned beta meaning." },
  ];
  const children = request.attribute === "table" ? codes.filter(code => !request.query || `${code.code} ${code.definition}`.toLowerCase().includes(request.query.toLowerCase())) : [{ ...field, key: "component/ZC/1", kind: "component", position: 1, name: "Owned component", datatype: specified("ST") }];
  return { state: "completed", reference: { identity: request.identity, edition: request.edition, status: "available", reason: "", coverage: { segments: 1, fields: 2, definitions: 2, missing: [] }, missing_count: 0, record }, preview: { kind: request.attribute === "table" ? "table" : "datatype", code: request.attribute === "table" ? "0099" : "ZC", title: record.name, definition: "Owned independent definition.", target_key: record.key, context: field }, children, offset: request.offset, child_count: children.length, total_count: request.attribute === "table" ? 2 : 1 };
}
function scope(owner = "source/message/path/reveal/pin", onOpen = vi.fn()): ReferenceHoverScope {
  return { owner, catalog: WORKSPACE_ROOT, identity: "owned-catalog", edition: "2.5.1", messageEdition: "2.5.1", onOpen, onChoose: vi.fn() };
}
function Grid({ reference = scope(), onSelect = vi.fn() }: { reference?: ReferenceHoverScope; onSelect?: (path: string) => void }) {
  return <><ReaderColumns /><ReaderGrid reference={reference} grid={{ mode: "message", segment: "", offset: 0, field_count: 0, row_count: 2, rows }} selected="ZAA[1]-1" busy={false} value={() => "Empty"} onSelect={onSelect} /></>;
}
afterEach(() => vi.useRealTimers());

test("hover waits for intent, reads once and never moves the selected field", async () => {
  vi.useFakeTimers();
  const facade = installFacade({ LookupHL7Reference: request => reply(request) });
  const select = vi.fn();
  render(<Grid onSelect={select} />);
  const target = screen.getByRole("button", { name: "Type reference for ZAA-2: ZC" });
  fireEvent.pointerEnter(target);
  await act(async () => { vi.advanceTimersByTime(349); });
  expect(facade.callsTo("LookupHL7Reference")).toHaveLength(0);
  await act(async () => { vi.advanceTimersByTime(1); });
  expect(screen.getByRole("dialog", { name: "Data type reference for ZAA-2" })).toBeTruthy();
  expect(screen.getByText("Owned independent definition.")).toBeTruthy();
  expect(facade.callsTo("LookupHL7Reference")[0]?.args[0]).toMatchObject({ identity: "owned-catalog", key: "field/ZAA/2", attribute: "datatype", limit: 5 });
  expect(select).not.toHaveBeenCalled();
  fireEvent.pointerLeave(target);
  fireEvent.pointerEnter(screen.getByRole("dialog"));
  await act(async () => { vi.advanceTimersByTime(200); });
  expect(screen.getByRole("dialog")).toBeTruthy();
  fireEvent.pointerLeave(screen.getByRole("dialog"));
  await act(async () => { vi.advanceTimersByTime(181); });
  expect(screen.queryByRole("dialog")).toBeNull();
});

test("keyboard preview opens the reference without selecting another row and Escape restores focus", async () => {
  const user = userEvent.setup();
  installFacade({ LookupHL7Reference: request => reply(request) });
  const onOpen = vi.fn(), onSelect = vi.fn();
  render(<Grid reference={scope(undefined, onOpen)} onSelect={onSelect} />);
  const grid = screen.getByRole("treegrid");
  grid.focus();
  await user.keyboard("{Enter}");
  const target = screen.getByRole("button", { name: "Type reference for ZAA-1: ZC" });
  expect(document.activeElement).toBe(target);
  await screen.findByText("Owned independent definition.");
  await user.tab();
  expect(document.activeElement).toBe(screen.getByRole("button", { name: "Open reference" }));
  await user.keyboard("{Escape}");
  expect(screen.queryByRole("dialog")).toBeNull();
  expect(document.activeElement).toBe(grid);
  await user.click(target);
  await user.click(await screen.findByRole("button", { name: "Open reference" }));
  expect(onOpen).toHaveBeenCalledWith("datatype/ZC", "");
  expect(onSelect).not.toHaveBeenCalled();
});

test("table hover searches code meanings, retains counts and copies identifiers and descriptions separately", async () => {
  const user = userEvent.setup();
  const clipboard = vi.spyOn(navigator.clipboard, "writeText").mockResolvedValue();
  const facade = installFacade({ LookupHL7Reference: request => reply(request) });
  render(<Grid />);
  await user.click(screen.getByRole("button", { name: "Columns" }));
  await user.click(screen.getByRole("checkbox", { name: "Tbl" }));
  await user.click(screen.getByRole("button", { name: "Done" }));
  fireEvent.focus(screen.getByRole("button", { name: "Tbl reference for ZAA-1: 0099" }));
  const card = await screen.findByRole("dialog", { name: "Table reference for ZAA-1" });
  await within(card).findByText("Owned table");
  await user.click(within(card).getByRole("button", { name: "Copy table OID" }));
  expect(clipboard).toHaveBeenLastCalledWith("2.16.840.1.9999");
  await user.type(within(card).getByRole("textbox", { name: "Search table values" }), "beta");
  await waitFor(() => expect(facade.callsTo("LookupHL7Reference").at(-1)?.args[0]).toMatchObject({ query: "beta", offset: 0, limit: 100 }));
  await within(card).findByText("1 of 2 values");
  expect(within(card).queryByRole("button", { name: "Copy code C01" })).toBeNull();
  await user.click(within(card).getByRole("button", { name: "Copy code C02" }));
  expect(clipboard).toHaveBeenLastCalledWith("C02");
  await user.click(within(card).getByRole("button", { name: "Copy description for C02" }));
  expect(clipboard).toHaveBeenLastCalledWith("Owned beta meaning.");
  expect(within(card).getByText("Copied description.")).toBeTruthy();
  const input = within(card).getByRole("textbox", { name: "Search table values" });
  await user.clear(input); await user.type(input, "unmatched");
  await within(card).findByText("0 of 2 values");
  expect(within(card).getByText("No codes or descriptions match “unmatched”.")).toBeTruthy();
});

test("pointer actions retain focus when a native webview does not focus buttons by default", async () => {
  installFacade({ LookupHL7Reference: request => reply(request) });
  const onOpen = vi.fn(), onSelect = vi.fn();
  render(<Grid reference={scope(undefined, onOpen)} onSelect={onSelect} />);
  const target = screen.getByRole("button", { name: "Type reference for ZAA-1: ZC" });
  act(() => target.focus());
  const action = await screen.findByRole("button", { name: "Open reference" });
  expect(fireEvent.mouseDown(action)).toBe(false);
  expect(document.activeElement).toBe(action);
  expect(screen.getByRole("dialog")).toBeTruthy();
  fireEvent.click(action);
  expect(onOpen).toHaveBeenCalledWith("datatype/ZC", "");
  expect(onSelect).not.toHaveBeenCalled();
});

test("a source or reveal-context change fences late hover replies and clears the old card", async () => {
  let finish: ((value: HL7ReferenceResult) => void) | undefined;
  let pending: HL7ReferenceRequest | undefined;
  installFacade({ LookupHL7Reference: request => { pending = request; return new Promise<HL7ReferenceResult>(resolve => { finish = resolve; }); } });
  const { rerender } = render(<Grid reference={scope("old-owner")} />);
  fireEvent.focus(screen.getByRole("button", { name: "Type reference for ZAA-1: ZC" }));
  await screen.findByText("Loading reference…");
  rerender(<Grid reference={scope("new-owner")} />);
  await act(async () => { finish?.(reply(pending!, "Stale datatype")); });
  expect(screen.queryByRole("dialog")).toBeNull();
  expect(screen.queryByText("Stale datatype")).toBeNull();
});

test("scroll inside a card keeps it open while scrolling the source grid dismisses it", async () => {
  installFacade({ LookupHL7Reference: request => reply(request) });
  render(<Grid />);
  fireEvent.focus(screen.getByRole("button", { name: "Type reference for ZAA-1: ZC" }));
  const card = await screen.findByRole("dialog");
  await within(card).findByText("Owned datatype");
  fireEvent.scroll(card.querySelector('.reference-hover-body')!);
  expect(screen.getByRole("dialog")).toBeTruthy();
  fireEvent.scroll(screen.getByRole("treegrid"));
  expect(screen.queryByRole("dialog")).toBeNull();
});

test("catalog refusal clears old information and offers a deliberate reference choice", async () => {
  installFacade({ LookupHL7Reference: () => ({ state: "failed", reason: "The selected reference catalog changed; select it again.", children: [], offset: 0, child_count: 0, total_count: 0 }) });
  const reference = scope();
  render(<Grid reference={reference} />);
  fireEvent.focus(screen.getByRole("button", { name: "Type reference for ZAA-1: ZC" }));
  const card = await screen.findByRole("dialog");
  await within(card).findByText("Reference unavailable");
  expect(within(card).queryByText("Owned independent definition.")).toBeNull();
  fireEvent.click(within(card).getByRole("button", { name: "Choose HL7 version" }));
  expect(reference.onChoose).toHaveBeenCalledOnce();
});

test("source scrolling cancels a pending hover before its intent delay expires", async () => {
  vi.useFakeTimers();
  const facade=installFacade({LookupHL7Reference:request=>reply(request)});
  render(<Grid/>);
  fireEvent.pointerEnter(screen.getByRole("button",{name:"Type reference for ZAA-1: ZC"}));
  await act(async()=>{vi.advanceTimersByTime(100);});
  fireEvent.scroll(screen.getByRole("treegrid"));
  await act(async()=>{vi.advanceTimersByTime(400);});
  expect(screen.queryByRole("dialog")).toBeNull();
  expect(facade.callsTo("LookupHL7Reference")).toHaveLength(0);
});

test("composite hover keeps a sourced datatype maximum separate from its field length", async()=>{
  installFacade({LookupHL7Reference:request=>{const result=reply(request);result.preview!.maximum_length="20";return result;}});
  render(<Grid/>);
  fireEvent.focus(screen.getByRole("button",{name:"Type reference for ZAA-1: ZC"}));
  expect(await screen.findByText("Maximum Length: 20")).toBeTruthy();
  expect(screen.getByText("At ZAA-1 · field length 7")).toBeTruthy();
  expect(screen.getByText("Components · 1")).toBeTruthy();
  expect(screen.queryByText("No internal components")).toBeNull();
});

function readerResult(){
  return inspectionResult(undefined,{selected:rows[0]!.node,display_path:"ZAA-1",reference_catalog:WORKSPACE_ROOT,metadata:{label:"Owned field",status:"available",hl7_version:"2.5.1",contract:"owned",provenance:"owned"},grid:{mode:"message",segment:"",offset:0,field_count:0,row_count:2,rows},reference:{status:"available",reason:"",identity:"owned-catalog",edition:"2.5.1",coverage:{segments:1,fields:2,definitions:2,missing:[]},missing_count:0,record:field,datatype_key:"datatype/ZC"}});
}

test("hover reference can temporarily replace owned exchange context and return without rereading evidence",async()=>{
  const user=userEvent.setup();
  installFacade({LookupHL7Reference:request=>reply(request)});
  const inspect=vi.fn(async()=>null);
  render(<MessageReader result={readerResult()} referenceCatalog={WORKSPACE_ROOT} loading={false} busy={false} onInspect={inspect} onReveal={()=>undefined} contextDetails={<p>Owned exchange context</p>} contextDetailsTitle="Recorded exchange"/>);
  expect(screen.getByRole("region",{name:"Context details"})).toBeTruthy();
  fireEvent.focus(screen.getByRole("button",{name:"Type reference for ZAA-1: ZC"}));
  await user.click(await screen.findByRole("button",{name:"Open reference"}));
  const details=await screen.findByRole("region",{name:"Reference details"});
  await within(details).findByRole("heading",{name:"Owned datatype"});
  expect(screen.queryByText("Owned exchange context")).toBeNull();
  await user.click(screen.getByRole("button",{name:"Back to Recorded exchange"}));
  expect(screen.getByText("Owned exchange context")).toBeTruthy();
  expect(inspect).not.toHaveBeenCalled();
  expect(screen.getByRole("treegrid").querySelector('[aria-selected="true"]')?.textContent).toContain("ZAA-1");
});

test("a delayed full-reference handoff never reclaims focus after the person moves it",async()=>{
  const user=userEvent.setup();
  let finish:((result:HL7ReferenceResult)=>void)|undefined;
  let pending:HL7ReferenceRequest|undefined;
  installFacade({LookupHL7Reference:request=>request.attribute?reply(request):new Promise<HL7ReferenceResult>(resolve=>{pending=request;finish=resolve;})});
  render(<><button type="button">Other control</button><MessageReader result={readerResult()} referenceCatalog={WORKSPACE_ROOT} loading={false} busy={false} onInspect={async()=>null} onReveal={()=>undefined}/></>);
  fireEvent.focus(screen.getByRole("button",{name:"Type reference for ZAA-1: ZC"}));
  await user.click(await screen.findByRole("button",{name:"Open reference"}));
  await screen.findByText("Reading reference…");
  await user.click(screen.getByRole("button",{name:"Other control"}));
  await act(async()=>{finish?.(reply(pending!));});
  expect(document.activeElement).toBe(screen.getByRole("button",{name:"Other control"}));
});
