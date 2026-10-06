import { expect, test, vi } from "vitest";
import { render, screen, within, waitFor, act } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MessageReader } from "./Inspector";
import type { ReferenceCatalogResult, HL7ReferenceResult, HL7Node } from "./bindings";
import { installFacade } from "./testkit/wails";
import { CASE_IDENTITY, WORKSPACE_ROOT, inspectionResult } from "./testkit/fixtures";

// Positions, counts and source metadata only; source/value acceptance is at
// the real facade journey, never fabricated clinical evidence in this test.
test("the reader keeps eleven columns reachable and distinguishes absent reference attributes", () => {
  const result = inspectionResult(undefined, {
    selected: { path:"MSH[1]", kind:"segment", parent:"", segment:"MSH", field:0, state:"present", start:0, end:128 },
    reference: { status:"available", reason:"", identity:"catalog-identity", edition:"2.5.1", coverage:{ segments:1, fields:1, definitions:0, missing:[] }, missing_count:0 },
    children:[{ node:{ path:"MSH[1]-9", kind:"field", parent:"MSH[1]", segment:"MSH", field:9, state:"empty", start:10, end:10 }, label:"Message Type", selector:"MSH[1]-9[1]", segment_name:"", value:"", truncated:false }],
  });
  render(<MessageReader result={result} loading={false} busy={false} onInspect={async()=>result} onReveal={()=>undefined} />);
  const grid = screen.getByRole("region", { name:"Segment grid" });
  for (const column of ["Path","Name","Type","Opt","Len","C-Len","Rep","Item#","Tbl","Sect","Value"]) expect(within(grid).getByText(column, {exact:true})).toBeTruthy();
  expect(within(grid).getByText("Empty")).toBeTruthy();
  expect(grid.classList.contains("reader-grid-scroll")).toBe(true);
  expect(screen.getByRole("region",{name:"Reference details"})).toBeTruthy();
  expect(screen.getByRole("button",{name:"HL7 reference version"})).toBeTruthy();
});

test("the reference details distinguish catalog failure without removing raw or hex views", async () => {
  const user=userEvent.setup();
  const result=inspectionResult(undefined,{reference:{status:"not_available",reason:"Catalog is unavailable.",identity:"",edition:"",coverage:{segments:0,fields:0,definitions:0,missing:[]},missing_count:0}});
  render(<MessageReader result={result} loading={false} busy={false} onInspect={async()=>result} onReveal={()=>undefined} />);
  expect(screen.getByText("Catalog is unavailable.")).toBeTruthy();
  await user.click(screen.getByRole("button",{name:"Reference information"}));
  const information=screen.getByRole("dialog",{name:"Reference information"});
  expect(within(information).getByText("Reference edition: Not available")).toBeTruthy();
  await user.click(within(information).getByRole("button",{name:"Close reference information"}));
  await user.click(screen.getByRole("button",{name:"More message actions"}));
  await user.click(screen.getByRole("menuitem",{name:"Hex"}));
  expect(screen.getByRole("tabpanel",{name:"Hex"})).toBeTruthy();
});

test("a delayed catalog summary cannot repopulate a changed source or reveal context", async () => {
  const user=userEvent.setup();
  let resolveSummary: ((value:ReferenceCatalogResult)=>void)|undefined;
  const facade=installFacade({
    ChooseInspectionPath:()=>({state:"completed",kind:"reference-catalog",path:WORKSPACE_ROOT}),
    ReadReferenceCatalog:()=>new Promise<ReferenceCatalogResult>((resolve)=>{resolveSummary=resolve;}),
  });
  const inspect=vi.fn(async()=>null);
  const first=inspectionResult(undefined,{reference:{status:"not_selected",reason:"Select an offline reference catalog.",identity:"",edition:"",coverage:{segments:0,fields:0,definitions:0,missing:[]},missing_count:0}});
  const {rerender}=render(<MessageReader result={first} loading={false} busy={false} onInspect={inspect} onReveal={()=>undefined} />);
  await user.click(screen.getByRole("button",{name:"HL7 reference version"}));
  await user.click(screen.getByRole("menuitem",{name:"Use local catalog…"}));
  await waitFor(()=>expect(facade.callsTo("ReadReferenceCatalog")).toHaveLength(1));
  const second=inspectionResult(undefined,{...first.inspection,identity:CASE_IDENTITY+"-new",revealed:true});
  rerender(<MessageReader result={second} loading={false} busy={false} onInspect={inspect} onReveal={()=>undefined} />);
  resolveSummary?.({state:"completed",reference:{status:"available",reason:"",identity:"old-catalog",edition:"2.5.1",coverage:{segments:1,fields:1,definitions:1,missing:[]},missing_count:0}});
  await waitFor(()=>expect((screen.getByRole("button",{name:"HL7 reference version"}) as HTMLButtonElement).disabled).toBe(false));
  expect(inspect).not.toHaveBeenCalled();
  expect(screen.queryByText("old-catalog")).toBeNull();
});

function ownedFieldReference() {
  const missing={state:"not_specified",value:""};
  return {key:"field/MSH/9",kind:"field",segment:"MSH",field:9,name:"Message Type",datatype:{state:"specified",value:"MSG"},optionality:missing,length:missing,conformance_length:missing,repetition:missing,item:missing,table:missing,section:missing,definition:"",source:"owned"};
}

test("the selected row owns navigation and the Details panel reopens without rereading evidence",async()=>{
 const user=userEvent.setup();
 const selected:HL7Node={path:"MSH[1]-9[1].1",kind:"component",parent:"MSH[1]-9[1]",segment:"MSH",field:9,state:"empty",start:10,end:10};
 const sibling={...selected,path:"MSH[1]-9[1].2"};
 const result=inspectionResult(undefined,{selected,reference:{status:"available",reason:"",identity:"catalog",edition:"2.5.1",coverage:{segments:0,fields:1,definitions:0,missing:[]},missing_count:0,record:ownedFieldReference()},children:[{node:selected,label:"First component",selector:"",segment_name:"",value:"",truncated:false},{node:sibling,label:"Second component",selector:"",segment_name:"",value:"",truncated:false}]});
 const inspect=vi.fn(async()=>null);
 render(<MessageReader result={result} loading={false} busy={false} onInspect={inspect} onReveal={()=>undefined}/>);
 expect(screen.getAllByRole("group",{name:"Selected position navigation"})).toHaveLength(1);
 expect(screen.getAllByRole("button",{name:"Next position"})).toHaveLength(1);
 expect((screen.getByRole("button",{name:"Previous position"}) as HTMLButtonElement).disabled).toBe(true);
 await user.click(screen.getByRole("button",{name:"Next position"}));
 expect(inspect).toHaveBeenCalledWith(sibling.path,0,-1,undefined,undefined,undefined,"catalog");
 await user.click(screen.getByRole("button",{name:"Hide Details panel"}));
 expect(screen.queryByRole("region",{name:"Reference details"})).toBeNull();
 await user.click(screen.getByRole("button",{name:"More message actions"}));
 await user.click(screen.getByRole("menuitem",{name:"Show Details panel"}));
 expect(screen.getByRole("region",{name:"Reference details"})).toBeTruthy();
 await user.click(screen.getByRole("button",{name:"Hide Details panel"}));
 await user.click(screen.getByRole("button",{name:"Column guide"}));
 expect(screen.getByRole("region",{name:"Reference attributes"})).toBeTruthy();
 expect(inspect).toHaveBeenCalledTimes(1);
});

test("datatype drilldown and Back retain the selected evidence and reject delayed replies after Back",async()=>{
  const user=userEvent.setup();
  let reply:((value:HL7ReferenceResult)=>void)|undefined;
  const facade=installFacade({LookupHL7Reference:()=>new Promise<HL7ReferenceResult>((resolve)=>{reply=resolve;})});
  const inspect=vi.fn(async()=>null);
  const result=inspectionResult(undefined,{selected:{path:"MSH[1]-9",kind:"field",parent:"MSH[1]",segment:"MSH",field:9,state:"empty",start:10,end:10},reference:{status:"available",reason:"",identity:"catalog-identity",edition:"2.5.1",coverage:{segments:0,fields:1,definitions:0,missing:[]},missing_count:0,record:ownedFieldReference(),datatype_key:"datatype/MSG"}});
  render(<MessageReader result={result} referenceCatalog={WORKSPACE_ROOT} loading={false} busy={false} onInspect={inspect} onReveal={()=>undefined} />);
  await user.click(screen.getByRole("button",{name:"Datatype · MSG"}));
  expect(screen.getByText("Selected field: MSH[1]-9")).toBeTruthy();
  expect(facade.callsTo("LookupHL7Reference")[0]?.args[0]).toMatchObject({catalog:WORKSPACE_ROOT,identity:"catalog-identity",edition:"2.5.1",key:"datatype/MSG",offset:0,limit:100});
  await user.click(screen.getByRole("button",{name:"Back to selected field"}));
  reply?.({state:"completed",children:[],offset:0,child_count:0,total_count:0,reference:{status:"available",reason:"",identity:"catalog-identity",edition:"2.5.1",coverage:{segments:0,fields:1,definitions:0,missing:[]},missing_count:0,record:{...ownedFieldReference(),kind:"datatype",segment:"",field:0,container:"MSG",position:0,name:"Old datatype pane"}}});
  await waitFor(()=>expect(screen.getByRole("button",{name:"Datatype · MSG"})).toBeTruthy());
  expect(screen.queryByText(/Old datatype pane/)).toBeNull();
  expect(inspect).not.toHaveBeenCalled();
});

test("a delayed profile selection cannot repopulate a changed profile owner",async()=>{
 const user=userEvent.setup();
 let reply:((value:import("./bindings").HL7ReferenceSelectionResult)=>void)|undefined;
 const facade=installFacade({ChooseInspectionPath:()=>({state:"completed",kind:"reference-profile",path:WORKSPACE_ROOT}),ReadHL7ReferenceSelection:()=>new Promise((resolve)=>{reply=resolve;})});
 const inspect=vi.fn(async()=>null);
 const first=inspectionResult(undefined,{selected:{path:"MSH[1]-9",kind:"field",parent:"MSH[1]",segment:"MSH",field:9,state:"empty",start:10,end:10},reference:{status:"not_selected",reason:"",identity:"",edition:"",coverage:{segments:0,fields:0,definitions:0,missing:[]},missing_count:0}});
 const {rerender}=render(<MessageReader result={first} loading={false} busy={false} onInspect={inspect} onReveal={()=>undefined}/>);
 await user.click(screen.getByRole("button",{name:"More message actions"}));
 await user.click(screen.getByRole("menuitem",{name:"Choose local profile…"}));
 await waitFor(()=>expect(facade.callsTo("ReadHL7ReferenceSelection")).toHaveLength(1));
 const changed=inspectionResult(undefined,{...first.inspection,reference_overlay:{status:"profile_selected",reason:"",selection:{profile:WORKSPACE_ROOT,profile_identity:"sha256:new"}}});
 rerender(<MessageReader result={changed} loading={false} busy={false} onInspect={inspect} onReveal={()=>undefined}/>);
 reply?.({state:"completed",overlay:{status:"profile_selected",reason:"",selection:{profile:WORKSPACE_ROOT,profile_identity:"sha256:old"}}});
 await waitFor(()=>expect(screen.getByRole("region",{name:"Selected profile context"})).toBeTruthy());
 expect(inspect).not.toHaveBeenCalled();
 expect(screen.queryByText("sha256:old")).toBeNull();
});

test("a new field intent cancels a pending catalog selection before the host reply",async()=>{
 const user=userEvent.setup();let reply:((value:ReferenceCatalogResult)=>void)|undefined;
 const facade=installFacade({ChooseInspectionPath:()=>({state:"completed",kind:"reference-catalog",path:WORKSPACE_ROOT}),ReadReferenceCatalog:()=>new Promise<ReferenceCatalogResult>((resolve)=>{reply=resolve;})});
 const inspect=vi.fn(async()=>null);
 const result=inspectionResult(undefined,{children:[{node:{path:"MSH[1]",kind:"segment",parent:"",segment:"MSH",field:0,state:"present",start:0,end:100},label:"",selector:"",segment_name:"",value:"",truncated:false}],reference:{status:"not_selected",reason:"",identity:"",edition:"",coverage:{segments:0,fields:0,definitions:0,missing:[]},missing_count:0}});
 render(<MessageReader result={result} loading={false} busy={false} onInspect={inspect} onReveal={()=>undefined}/>);
 await user.click(screen.getByRole("button",{name:"HL7 reference version"}));
  await user.click(screen.getByRole("menuitem",{name:"Use local catalog…"}));await waitFor(()=>expect(facade.callsTo("ReadReferenceCatalog")).toHaveLength(1));
 await user.click(screen.getByRole("button",{name:/^MSH\[1\]/}));
 expect(inspect).toHaveBeenCalledTimes(1);
 await act(async()=>{reply?.({state:"completed",reference:{status:"available",reason:"",identity:"old",edition:"2.4",coverage:{segments:1,fields:1,definitions:1,missing:[]},missing_count:0}});});
 await waitFor(()=>expect((screen.getByRole("button",{name:"HL7 reference version"}) as HTMLButtonElement).disabled).toBe(false));
 expect(inspect).toHaveBeenCalledTimes(1);
});

test("return to the declared edition requires an explicitly chosen matching local catalog",async()=>{
 const user=userEvent.setup();let edition="2.7.1";
 installFacade({ChooseInspectionPath:()=>({state:"completed",kind:"reference-catalog",path:WORKSPACE_ROOT}),ReadReferenceCatalog:()=>({state:"completed",reference:{status:"available",reason:"",identity:"catalog",edition,coverage:{segments:1,fields:1,definitions:1,missing:[]},missing_count:0}})});
 const inspect=vi.fn(async()=>null);
 const result=inspectionResult(undefined,{reference:{status:"available",reason:"",identity:"old",edition:"2.7.1",coverage:{segments:1,fields:1,definitions:1,missing:[]},missing_count:0}});
 render(<MessageReader result={result} loading={false} busy={false} onInspect={inspect} onReveal={()=>undefined}/>);
 await user.click(screen.getByRole("button",{name:"Return to message edition"}));
 expect((await screen.findByRole("alert")).textContent).toContain("Choose a catalog for the declared edition 2.5.1.");expect(inspect).not.toHaveBeenCalled();
 edition="2.5.1";await user.click(screen.getByRole("button",{name:"Return to message edition"}));await waitFor(()=>expect(inspect).toHaveBeenCalledTimes(1));
 expect(inspect).toHaveBeenCalledWith("",0,-1,undefined,WORKSPACE_ROOT,undefined,"catalog");
});

test("catalog refusal with the held hash clears an earlier entity pane",async()=>{
 const user=userEvent.setup();
 installFacade({LookupHL7Reference:()=>({state:"completed",children:[],offset:0,child_count:0,total_count:0,reference:{status:"available",reason:"",identity:"catalog-identity",edition:"2.5.1",coverage:{segments:0,fields:1,definitions:0,missing:[]},missing_count:0,record:{...ownedFieldReference(),kind:"datatype",segment:"",field:0,container:"MSG",position:0,name:"Earlier entity pane"}}})});
 const first=inspectionResult(undefined,{selected:{path:"MSH[1]-9",kind:"field",parent:"MSH[1]",segment:"MSH",field:9,state:"empty",start:10,end:10},reference:{status:"available",reason:"",identity:"catalog-identity",edition:"2.5.1",coverage:{segments:0,fields:1,definitions:0,missing:[]},missing_count:0,record:ownedFieldReference(),datatype_key:"datatype/MSG"}});
 const {rerender}=render(<MessageReader result={first} referenceCatalog={WORKSPACE_ROOT} referenceIdentity="catalog-identity" loading={false} busy={false} onInspect={async()=>null} onReveal={()=>undefined}/>);
 await user.click(screen.getByRole("button",{name:"Datatype · MSG"}));expect(await screen.findByRole("heading",{name:"Earlier entity pane"})).toBeTruthy();
 const refused=inspectionResult(undefined,{...first.inspection,reference:{status:"not_available",reason:"The catalog changed.",identity:"catalog-identity",edition:"",coverage:{segments:0,fields:0,definitions:0,missing:[]},missing_count:0}});
 rerender(<MessageReader result={refused} referenceCatalog={WORKSPACE_ROOT} referenceIdentity="catalog-identity" loading={false} busy={false} onInspect={async()=>null} onReveal={()=>undefined}/>);
 expect(await screen.findByText("The catalog changed.")).toBeTruthy();expect(screen.queryByRole("heading",{name:"Earlier entity pane"})).toBeNull();
});

test("validation navigation keeps inspection evidence and opens the exact field specification without a verdict",async()=>{
  const user=userEvent.setup();
  const requirements=vi.fn();const inspect=vi.fn(async()=>null);
  const result=inspectionResult(undefined,{selected:{path:"MSH[1]-9",kind:"field",parent:"MSH[1]",segment:"MSH",field:9,state:"empty",start:10,end:10},selector:"MSH[1]-9[1]",reference:{status:"available",reason:"",identity:"catalog-identity",edition:"2.5.1",coverage:{segments:0,fields:1,definitions:0,missing:[]},missing_count:0,record:ownedFieldReference()}});
  render(<MessageReader result={result} loading={false} busy={false} onInspect={inspect} onReveal={()=>undefined} onRequirements={requirements}/>);
  expect(screen.queryByRole("tab",{name:"Validation"})).toBeNull();
  await user.click(screen.getByRole("button",{name:"More message actions"}));
  await user.click(screen.getByRole("menuitem",{name:"Validation"}));
  expect(screen.getByText("No validation result available in this inspection")).toBeTruthy();
  await user.click(screen.getByRole("button",{name:"Select specification"}));
  expect(requirements).toHaveBeenCalledWith("MSH[1]-9[1]");
  expect(inspect).not.toHaveBeenCalled();
  await user.click(screen.getByRole("tab",{name:"Details"}));
  expect(screen.getByRole("button",{name:"Open specification"})).toBeTruthy();
});

test("the grid separates unavailable metadata from sourced unspecified notation",()=>{
  const record={...ownedFieldReference(),length:{state:"not_available",value:""},conformance_length:{state:"not_specified",value:""}};
  const result=inspectionResult(undefined,{selected:{path:"MSH[1]",kind:"segment",parent:"",segment:"MSH",field:0,state:"present",start:0,end:100},reference:{status:"available",reason:"",identity:"catalog-identity",edition:"2.5.1",coverage:{segments:0,fields:1,definitions:0,missing:[]},missing_count:0},children:[{node:{path:"MSH[1]-9",kind:"field",parent:"MSH[1]",segment:"MSH",field:9,state:"empty",start:10,end:10},label:"Message Type",selector:"MSH[1]-9[1]",segment_name:"",value:"",truncated:false,reference:record}]});
  render(<MessageReader result={result} loading={false} busy={false} onInspect={async()=>null} onReveal={()=>undefined}/>);
  const row=screen.getByRole("button",{name:/^MSH\[1\]-9 Message Type Empty$/});
  expect(row.querySelector('[title="Not available"]')?.textContent).toBe("?");
  expect(row.querySelector('[title="Not specified"]')?.textContent).toBe("—");
});

test("compact overview shows the sourced definition while Expand retains its component preface",async()=>{
  const user=userEvent.setup();
  const full="Components: owned component metadata. Definition: Owned explanatory text.";
  const record={...ownedFieldReference(),definition:full};
  const result=inspectionResult(undefined,{selected:{path:"MSH[1]-9",kind:"field",parent:"MSH[1]",segment:"MSH",field:9,state:"empty",start:10,end:10},reference:{status:"available",reason:"",identity:"catalog-identity",edition:"2.5.1",coverage:{segments:0,fields:1,definitions:1,missing:[]},missing_count:0,record}});
  render(<MessageReader result={result} loading={false} busy={false} onInspect={async()=>null} onReveal={()=>undefined}/>);
  expect(screen.getByText("Owned explanatory text.")).toBeTruthy();
  expect(screen.queryByText(full)).toBeNull();
  await user.click(screen.getByRole("button",{name:"Expand definition"}));
  expect(screen.getByText(full)).toBeTruthy();
});

test("explicit owned context replaces the right pane without rereading evidence or losing grid columns",()=>{
  const inspect=vi.fn(async()=>null);
  const result=inspectionResult(undefined,{reference:{status:"available",reason:"",identity:"catalog-identity",edition:"2.5.1",coverage:{segments:1,fields:0,definitions:0,missing:[]},missing_count:0},children:[{node:{path:"MSH[1]",kind:"segment",parent:"",segment:"MSH",field:0,state:"present",start:0,end:100},label:"",selector:"",segment_name:"",value:"",truncated:false}]});
  const {rerender}=render(<MessageReader result={result} loading={false} busy={false} onInspect={inspect} onReveal={()=>undefined} contextDetails={<p>Owned workflow context</p>} contextDetailsTitle="Recorded exchange"/>);
  expect(screen.getByRole("region",{name:"Context details"})).toBeTruthy();
  expect(screen.getByRole("heading",{name:"Recorded exchange"})).toBeTruthy();
  expect(screen.queryByRole("region",{name:"Reference details"})).toBeNull();
  const grid=within(screen.getByRole("region",{name:"Segment grid"}));
  for(const column of ["Path","Name","Type","Opt","Len","C-Len","Rep","Item#","Tbl","Sect","Value"])expect(grid.getByText(column,{exact:true})).toBeTruthy();
  rerender(<MessageReader result={result} loading={false} busy={false} onInspect={inspect} onReveal={()=>undefined}/>);
  expect(screen.queryByText("Owned workflow context")).toBeNull();
  expect(screen.getByRole("region",{name:"Reference details"})).toBeTruthy();
  expect(inspect).not.toHaveBeenCalled();
});

test("a primitive field retains Overview without an empty Components tab",()=>{
 const result=inspectionResult(undefined,{selected:{path:"MSH[1]-8",kind:"field",parent:"MSH[1]",segment:"MSH",field:8,state:"empty",start:10,end:10},reference:{status:"available",reason:"",identity:"catalog",edition:"2.5.1",coverage:{segments:0,fields:1,definitions:0,missing:[]},missing_count:0,record:{...ownedFieldReference(),name:"Security",datatype:{state:"specified",value:"ST"}},datatype_key:"datatype/ST"}});
 render(<MessageReader result={result} loading={false} busy={false} onInspect={async()=>result} onReveal={()=>undefined}/>);
 expect(screen.getByRole("tab",{name:"Overview"})).toBeTruthy();
 expect(screen.queryByRole("tab",{name:"Components"})).toBeNull();
 expect(screen.getByRole("button",{name:"Datatype · ST"})).toBeTruthy();
});


test("the selected segment disclosure collapses and expands its field rows", async () => {
  const user = userEvent.setup();
  const segment = { path:"PV1[1]", kind:"segment", parent:"", segment:"PV1", field:0, state:"present" as const, start:0, end:30 };
  const result = inspectionResult(undefined, { selected:segment, grid:{ segment:segment.path, offset:0, field_count:1, rows:[
    { node:segment, label:"", selector:"", segment_name:"Patient Visit", value:"", truncated:false },
    { node:{...segment,path:"PV1[1]-2",kind:"field",field:2,parent:segment.path}, label:"Patient Class", selector:"PV1-2", segment_name:"", value:"", truncated:false, depth:1 },
  ]} });
  render(<MessageReader result={result} loading={false} busy={false} onInspect={async()=>result} onReveal={()=>undefined}/>);
  const grid = within(screen.getByRole("region", {name:"Segment grid"}));
  const disclosure = grid.getByRole("button", {name:/PV1\[1\] Patient Visit/});
  expect(disclosure.getAttribute("aria-expanded")).toBe("true");
  await user.click(disclosure);
  expect(disclosure.getAttribute("aria-expanded")).toBe("false");
  expect(grid.queryByText("Patient Class")).toBeNull();
  await user.click(disclosure);
  expect(disclosure.getAttribute("aria-expanded")).toBe("true");
  expect(grid.getByText("Patient Class")).toBeTruthy();
});


test("a nested composite offers its direct Subcomponents view", async () => {
  const user = userEvent.setup();
  const selected:HL7Node = {path:"PV1[1]-3[1].4",kind:"component",parent:"PV1[1]-3[1]",segment:"PV1",field:3,state:"empty",start:10,end:10};
  const reference = {status:"available",reason:"",identity:"catalog",edition:"2.5.1",coverage:{segments:0,fields:1,definitions:0,missing:[]},missing_count:0,record:{...ownedFieldReference(),name:"Facility",datatype:{state:"specified",value:"HD"}},datatype_key:"datatype/HD"};
  const result = inspectionResult(undefined,{selected,reference,reference_values:[1,2,3].map(position=>({key:`component/HD/${position}`,node:{...selected,kind:"subcomponent",parent:selected.path,path:`${selected.path}.${position}`,state:"omitted" as const,start:0,end:0},encoded:"",decoded:"",decode_state:"omitted",truncated:false}))});
  const facade=installFacade({LookupHL7Reference:()=>({state:"completed",children:[],offset:0,child_count:0,total_count:0,reference:{...reference,record:{...reference.record,kind:"datatype",name:"HD"}}})});
  render(<MessageReader result={result} loading={false} busy={false} onInspect={async()=>result} onReveal={()=>undefined}/>);
  await user.click(screen.getByRole("tab",{name:"Subcomponents"}));
  expect(screen.getByRole("tab",{name:"Subcomponents",selected:true})).toBeTruthy();
  expect(facade.callsTo("LookupHL7Reference")[0]?.args[0]).toMatchObject({key:"datatype/HD",identity:"catalog"});
});

test("component Details keeps its single-repetition parent field context", () => {
  const selected:HL7Node={path:"PV1[1]-3[1].4",kind:"component",parent:"PV1[1]-3[1]",segment:"PV1",field:3,state:"empty",start:10,end:10};
  const field={...selected,path:"PV1[1]-3",kind:"field",parent:"PV1[1]"};
  const result=inspectionResult(undefined,{selected,reference:{status:"available",reason:"",identity:"catalog",edition:"2.5.1",coverage:{segments:0,fields:1,definitions:0,missing:[]},missing_count:0,record:{...ownedFieldReference(),name:"Facility"}},grid:{segment:"PV1[1]",offset:0,field_count:1,rows:[{node:field,label:"Assigned Patient Location",selector:"PV1-3",segment_name:"",value:"",truncated:false},{node:selected,label:"Facility",selector:selected.path,segment_name:"",value:"",truncated:false}]}});
  render(<MessageReader result={result} loading={false} busy={false} onInspect={async()=>result} onReveal={()=>undefined}/>);
  expect(screen.getByRole("region",{name:"Reference details"}).querySelector(".reader-parent-context")?.textContent).toContain("Assigned Patient Location");
});
