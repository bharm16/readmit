import { expect,test,vi } from "vitest";
import { render,screen,waitFor,within,act } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { FieldValues } from "./FieldValues";
import type { FieldValuesResult,FieldValueScope } from "./bindings";
import { installFacade } from "./testkit/wails";
import { CASE_IDENTITY,WORKSPACE_ROOT,messageRow } from "./testkit/fixtures";
import { NO_QUERY } from "./Messages";

const scope:FieldValueScope={workspace:WORKSPACE_ROOT,case:"incident",identity:CASE_IDENTITY,query:NO_QUERY};
function counts(identity=CASE_IDENTITY,matched=600):FieldValuesResult {return {state:"completed",identity,scope_identity:"opaque-scope",snapshot:"opaque-snapshot",selector:"MSH[1]-10[1]",unit:"message occurrences; one exact selector",total:600,matched,scope_undecided:0,scope_undecodable:0,scanned:matched,scan_complete:true,complete:true,revealed:false,counts:{present:matched,empty:0,null:0,omitted:0,undecodable:0,undecided:0},rows:[{id:"opaque-present",state:"present",hidden:true,messages:matched}],group_count:1,offset:0,limit:100};}

test("field counts declare full scope and drill down to opaque occurrence-backed membership",async()=>{
 const user=userEvent.setup();const open=vi.fn();const back=vi.fn();
 const facade=installFacade({ReadFieldValues:()=>counts(),ReadFieldValueOccurrences:()=>({state:"completed",identity:CASE_IDENTITY,scope_identity:"opaque-scope",snapshot:"opaque-snapshot",bucket:"opaque-present",rows:[messageRow("first-occurrence")],total:600,offset:0,limit:200})});
 render(<FieldValues scope={scope} selector="MSH[1]-10[1]" onBack={back} onInspectOccurrence={open}/>);
 expect(await screen.findByText("600 matched of 600 capture occurrences · 600 examined · Complete scope scan")).toBeTruthy();
 expect(screen.getByText("Hidden present values")).toBeTruthy();
 expect(facade.callsTo("ReadFieldValues")[0]?.args[0]).toMatchObject({scope,selector:"MSH[1]-10[1]",reveal:false,offset:0,limit:100});
 await user.click(screen.getByRole("button",{name:"Show messages"}));
 const table=await screen.findByRole("table",{name:"Counted messages"});
 expect(facade.callsTo("ReadFieldValueOccurrences")[0]?.args[0]).toMatchObject({snapshot:"opaque-snapshot",bucket:"opaque-present",reveal:false,scope});
 const row=table.querySelector<HTMLElement>('[data-row-id="first-occurrence"]')!;await user.dblClick(row);
 expect(open).toHaveBeenCalledWith("first-occurrence","MSH[1]-10[1]");
 await user.click(screen.getByRole("button",{name:"Back to aggregate"}));expect(screen.queryByRole("table",{name:"Counted messages"})).toBeNull();expect(screen.getByRole("table",{name:"Field value counts"})).toBeTruthy();
 await user.click(screen.getByRole("button",{name:"Back to reader"}));expect(back).toHaveBeenCalledOnce();
});

test("changed scope fences delayed counts before they can replace the new owner",async()=>{
 let reply:((value:FieldValuesResult)=>void)|undefined;
 installFacade({ReadFieldValues:request=>request.scope.identity===CASE_IDENTITY?new Promise<FieldValuesResult>(resolve=>{reply=resolve;}):counts(CASE_IDENTITY+"-new",8)});
 const {rerender}=render(<FieldValues scope={scope} selector="MSH-10" onBack={()=>undefined} onInspectOccurrence={()=>undefined}/>);
 await waitFor(()=>expect(reply).toBeDefined());
 rerender(<FieldValues scope={{...scope,identity:CASE_IDENTITY+"-new"}} selector="MSH-10" onBack={()=>undefined} onInspectOccurrence={()=>undefined}/>);
 expect(await screen.findByText(/8 matched of600|8 matched of 600/)).toBeTruthy();
 await act(async()=>{reply?.(counts());});
 expect(screen.queryByText(/600 matched of 600/)).toBeNull();
});

test("an undecided query count reports its uncertainty instead of complete coverage",async()=>{
 installFacade({ReadFieldValues:()=>({...counts(),complete:false,scope_undecided:3,scope_undecodable:2,counts:{present:1,empty:2,null:3,omitted:4,undecodable:5,undecided:6}})});
 render(<FieldValues scope={scope} selector="MSH-10" onBack={()=>undefined} onInspectOccurrence={()=>undefined}/>);
 expect(await screen.findByText("Some field values or query membership remain unavailable or undecided.")).toBeTruthy();
 expect(screen.getByText("Query uncertainty: 3 undecided; 2 eligible occurrences could not be parsed.")).toBeTruthy();
 const values=within(screen.getByRole("region",{name:"Field values"}));for(const state of ["Readable present","Empty","Null","Omitted","Undecodable","Undecided"])expect(values.getByText(state,{exact:true})).toBeTruthy();
});

test("reveal consent does not carry to a changed query scope",async()=>{
 const user=userEvent.setup();
 const facade=installFacade({ReadFieldValues:request=>({...counts(request.scope.identity),revealed:request.reveal,rows:[{id:"opaque-present",state:"present",hidden:!request.reveal,messages:600}]})});
 const {rerender}=render(<FieldValues scope={scope} selector="MSH-10" onBack={()=>undefined} onInspectOccurrence={()=>undefined}/>);
 await screen.findByText(/600 matched of 600/);await user.click(screen.getByRole("button",{name:"Show values for this scope"}));
 await waitFor(()=>expect(facade.callsTo("ReadFieldValues").at(-1)?.args[0]).toMatchObject({reveal:true}));
 const changed={...scope,query:{...NO_QUERY,sources:["s0001"]}};
 rerender(<FieldValues scope={changed} selector="MSH-10" onBack={()=>undefined} onInspectOccurrence={()=>undefined}/>);
 await waitFor(()=>expect(facade.callsTo("ReadFieldValues").at(-1)?.args[0]).toMatchObject({scope:changed,reveal:false}));
 expect(screen.getByRole("button",{name:"Show values for this scope"})).toBeTruthy();
});

test("Stop counting cancels the local count owner and returns no partial distribution",async()=>{
 const user=userEvent.setup();let reply:((value:FieldValuesResult)=>void)|undefined;
 const facade=installFacade({ReadFieldValues:()=>new Promise<FieldValuesResult>(resolve=>{reply=resolve;}),Cancel:()=>{reply?.({...counts(),state:"cancelled",reason:"Counting was stopped.",rows:[],snapshot:"",complete:false,scan_complete:false});}});
 render(<FieldValues scope={scope} selector="MSH-10" onBack={()=>undefined} onInspectOccurrence={()=>undefined}/>);
 await user.click(await screen.findByRole("button",{name:"Stop counting"}));
 expect(facade.callsTo("Cancel")[0]?.args).toEqual(["field-values-read"]);
 expect(await screen.findByRole("alert")).toBeTruthy();expect(screen.queryByRole("table",{name:"Field value counts"})).toBeNull();
});
