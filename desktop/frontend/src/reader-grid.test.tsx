import { useState } from "react";
import { render, screen, within, fireEvent, act } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, test, vi } from "vitest";
import { MessageReader } from "./Inspector";
import { inspectionResult } from "./testkit/fixtures";
import type { InspectionResult, InspectorNode } from "./bindings";

const nodes: InspectorNode[] = [
  {node:{path:"PV1[1]",kind:"segment",parent:"",segment:"PV1",field:0,state:"present",start:0,end:40},label:"",selector:"",segment_name:"Owned segment",value:"",truncated:false,has_children:true,expanded:true,display_path:"PV1"},
  {node:{path:"PV1[1]-1",kind:"field",parent:"PV1[1]",segment:"PV1",field:1,state:"empty",start:10,end:10},label:"Owned first",selector:"PV1[1]-1[1]",segment_name:"",value:"",truncated:false,depth:1,grid_parent:"PV1[1]",display_path:"PV1-1"},
  {node:{path:"PV1[1]-2",kind:"field",parent:"PV1[1]",segment:"PV1",field:2,state:"empty",start:11,end:11},label:"Owned second",selector:"PV1[1]-2[1]",segment_name:"",value:"",truncated:false,depth:1,grid_parent:"PV1[1]",display_path:"PV1-2"},
];

function gridResult(path="PV1[1]-1"): InspectionResult {
  const selected=nodes.find(row=>row.node.path===path)??nodes[1]!;
  return inspectionResult(undefined,{
    selected:selected.node, display_path:selected.display_path!,
    metadata:{label:selected.label,status:"available",hl7_version:"2.5.1",contract:"owned",provenance:"owned"},
    grid:{mode:"message",segment:"",offset:0,field_count:0,row_count:3,rows:nodes,expanded:["PV1[1]"],ancestors:["PV1[1]"],selected_parent:"PV1[1]"},
    reference:{status:"available",reason:"",identity:"catalog",edition:"2.5.1",coverage:{segments:1,fields:2,definitions:0,missing:[]},missing_count:0},
  });
}

test("arrow navigation selects the next original field through one accessible grid",async()=>{
  const user=userEvent.setup();
  function Reader(){
    const [result,setResult]=useState(gridResult);
    return <MessageReader result={result} loading={false} busy={false} onReveal={()=>undefined} onInspect={async(path)=>{const next=gridResult(path);setResult(next);return next;}}/>;
  }
  render(<Reader/>);
  const grid=screen.getByRole("treegrid",{name:"Message fields"});
  for(const name of ["Path","Name","Type","Opt","Value"])expect(within(grid).getByRole("columnheader",{name})).toBeTruthy();
  await user.click(within(grid).getByRole("button",{name:/PV1\[1\]-1 Owned first/}));
  await user.keyboard("{ArrowDown}");
  expect(await screen.findByRole("heading",{name:"Owned second"})).toBeTruthy();
  expect(within(grid).getByRole("button",{name:/PV1\[1\]-2 Owned second/}).getAttribute("aria-current")).toBe("location");
  expect(document.activeElement).toBe(grid);
});

test("expansion and omitted positions are requested without changing the selected source path", async()=>{
  const user=userEvent.setup();
  const requests: unknown[][]=[];
  render(<MessageReader result={gridResult()} loading={false} busy={false} onReveal={()=>undefined} onInspect={async(...args)=>{requests.push(args);return gridResult();}}/>);
  await user.click(screen.getByRole("button",{name:"Collapse PV1"}));
  expect(requests.at(-1)?.[7]).toEqual({expanded:[],show_omitted:false,offset:0,follow_selection:false});
  await user.click(screen.getByRole("checkbox",{name:"Show fields not present"}));
  expect(requests.at(-1)?.[0]).toBe("PV1[1]-1");
  expect(requests.at(-1)?.[7]).toEqual({expanded:["PV1[1]"],show_omitted:true,offset:0,follow_selection:true});
});

test("columns keep identity and value visible while layout controls work from the keyboard",async()=>{
  const user=userEvent.setup();
  render(<MessageReader result={gridResult()} loading={false} busy={false} onReveal={()=>undefined} onInspect={async()=>gridResult()}/>);
  await user.click(screen.getByRole("button",{name:"Columns"}));
  const columns=screen.getByRole("dialog",{name:"Columns"});
  for(const name of ["Path","Name","Value"])expect(within(columns).getByRole("checkbox",{name:new RegExp(name)})).toHaveProperty("disabled",true);
  await user.click(within(columns).getByRole("checkbox",{name:"Type"}));
  expect(screen.queryByRole("columnheader",{name:"Type"})).toBeNull();
  await user.click(within(columns).getByRole("checkbox",{name:"Len"}));
  expect(screen.getByRole("columnheader",{name:"Len"})).toBeTruthy();
  await user.click(within(columns).getByRole("button",{name:"Done"}));
  const nameWidth=screen.getByRole("separator",{name:"Name column width"});
  const valueWidth=screen.getByRole("separator",{name:"Value column width"});
  const before=Number(valueWidth.getAttribute("aria-valuenow"));
  nameWidth.focus(); await user.keyboard("{ArrowRight}");
  expect(Number(nameWidth.getAttribute("aria-valuenow"))).toBe(18.5);
  expect(Number(valueWidth.getAttribute("aria-valuenow"))).toBe(before);
  await user.click(screen.getByRole("button",{name:"Wrap"}));
  expect(screen.getByRole("button",{name:"Wrap"}).getAttribute("aria-pressed")).toBe("true");
});


test("returning directly to a message restores source scroll without following its selected token",()=>{
 const prior=Object.getOwnPropertyDescriptor(HTMLElement.prototype,"scrollIntoView");
 Object.defineProperty(HTMLElement.prototype,"scrollIntoView",{configurable:true,value:function(this:HTMLElement){if(this.classList.contains("raw-token-selected")){const source=this.closest<HTMLElement>(".raw-source");if(source)source.scrollTop=0;}}});
 try {
  const base=gridResult();
  const source=(occurrence:string,start:number,follow:boolean)=>inspectionResult(undefined,{...base.inspection,occurrence,revealed:true,grid:{...base.inspection!.grid!,follow_selection:follow},readable_window:{offset:0,end:100,message_start:0,message_end:100,before:"",selected:"",after:"",lines:[{number:1,tokens:[{start,end:start,path:"PV1[1]-1",text:"",empty:true,selected:true}]}]}});
  const props={loading:false,busy:false,onInspect:async()=>null,onReveal:()=>undefined};
  const {rerender}=render(<MessageReader {...props} result={source("first",1,true)}/>);
  const original=screen.getByLabelText("HL7 message text");
  original.scrollTop=100;original.scrollLeft=40;fireEvent.scroll(original);
  rerender(<MessageReader {...props} result={source("second",2,true)}/>);
  rerender(<MessageReader {...props} result={source("first",1,false)}/>);
  expect(screen.getByLabelText("HL7 message text").scrollTop).toBe(100);
  expect(screen.getByLabelText("HL7 message text").scrollLeft).toBe(40);
 } finally {if(prior)Object.defineProperty(HTMLElement.prototype,"scrollIntoView",prior);else Reflect.deleteProperty(HTMLElement.prototype,"scrollIntoView");}
});

test("the message table scrolls across host windows without paging controls or moving selection", async () => {
  const { ReaderGrid } = await import("./ReaderGrid");
  const all = Array.from({length:106}, (_, index) => ({...nodes[0]!,node:{...nodes[0]!.node,path:`SEG[${index+1}]`},display_path:`SEG[${index+1}]`,expanded:false}));
  const requests:number[]=[];
  function Table() {
    const [offset,setOffset]=useState(0);
    return <ReaderGrid grid={{mode:"message",segment:"",offset,field_count:0,row_count:all.length,rows:all.slice(offset,offset+100),follow_selection:false}} selected={all[0]!.node.path} busy={false} value={()=>""} onSelect={()=>{throw new Error("scrolling must not select a row");}} onPage={next=>{requests.push(next);setOffset(next);}}/>;
  }
  render(<Table/>);
  const grid=screen.getByRole("treegrid",{name:"Message fields"});
  Object.defineProperty(grid,"clientHeight",{configurable:true,value:460});
  expect(screen.queryByRole("button",{name:"Next rows"})).toBeNull();
  expect(grid.getAttribute("aria-rowcount")).toBe("107");
  fireEvent.scroll(grid,{target:{scrollTop:96*46}});
  expect(await within(grid).findByRole("button",{name:/^SEG\[106\] Owned segment/})).toBeTruthy();
  expect(requests.length).toBe(1);
  expect(within(grid).getAllByRole("row").length).toBeLessThanOrEqual(101);
  expect(grid.scrollTop).toBe(96*46);
  fireEvent.scroll(grid,{target:{scrollTop:0}});
  expect(await within(grid).findByRole("button",{name:/^SEG\[1\] Owned segment/})).toBeTruthy();
  expect(requests.at(-1)).toBe(0);
});

test("scrolling again during a pending window read loads the latest viewport", async () => {
  const { ReaderGrid } = await import("./ReaderGrid");
  const all=Array.from({length:1000},(_,index)=>({...nodes[0]!,node:{...nodes[0]!.node,path:`SEG[${index+1}]`},display_path:`SEG[${index+1}]`}));
  const requests:number[]=[];
  let finish:()=>void=()=>undefined;
  function Table(){
    const [offset,setOffset]=useState(0),[busy,setBusy]=useState(false);
    return <ReaderGrid grid={{mode:"message",segment:"",offset,field_count:0,row_count:all.length,rows:all.slice(offset,offset+100),follow_selection:false}} selected="" busy={busy} value={()=>""} onSelect={()=>undefined} onPage={next=>{requests.push(next);setBusy(true);finish=()=>{setOffset(next);setBusy(false);};}}/>;
  }
  render(<Table/>);
  const grid=screen.getByRole("treegrid",{name:"Message fields"});
  Object.defineProperty(grid,"clientHeight",{configurable:true,value:460});
  fireEvent.scroll(grid,{target:{scrollTop:150*46}});
  expect(requests).toEqual([140]);
  fireEvent.scroll(grid,{target:{scrollTop:990*46}});
  expect(requests).toHaveLength(1);
  await act(async()=>finish());
  expect(requests).toEqual([140,980]);
  await act(async()=>finish());
  expect(await within(grid).findByRole("button",{name:/^SEG\[1000\] Owned segment/})).toBeTruthy();
  expect(grid.scrollTop).toBe(990*46);
});

test("a tall viewport at half-size zoom settles with every visible row loaded", async () => {
 const {ReaderGrid}=await import("./ReaderGrid");
 const previous=document.documentElement.style.fontSize;
 document.documentElement.style.fontSize="8px";
 try {
  const all=Array.from({length:1000},(_,index)=>({...nodes[0]!,node:{...nodes[0]!.node,path:`SEG[${index+1}]`},display_path:`SEG[${index+1}]`}));
  const requests:{offset:number;limit:number}[]=[];
  let finish:()=>void=()=>undefined;
  function Table(){
   const [window,setWindow]=useState({offset:0,limit:100}),[busy,setBusy]=useState(false);
   return <ReaderGrid grid={{mode:"message",segment:"",offset:window.offset,field_count:0,row_count:all.length,rows:all.slice(window.offset,window.offset+window.limit),follow_selection:false}} selected="" busy={busy} value={()=>""} onSelect={()=>undefined} onPage={(offset,_edge,limit=100)=>{requests.push({offset,limit});setBusy(true);finish=()=>{setWindow({offset,limit});setBusy(false);};}}/>;
  }
  render(<Table/>);
  const grid=screen.getByRole("treegrid",{name:"Message fields"});
  Object.defineProperty(grid,"clientHeight",{configurable:true,value:3000});
  fireEvent.scroll(grid,{target:{scrollTop:2300}});
  expect(requests).toHaveLength(1);
  expect(requests[0]!.limit).toBeGreaterThan(100);
  await act(async()=>finish());
  expect(requests).toHaveLength(1);
  expect(within(grid).getByRole("button",{name:/^SEG\[230\] Owned segment/})).toBeTruthy();
  expect(grid.scrollTop).toBe(2300);
 } finally { document.documentElement.style.fontSize=previous; }
});

for(const direction of ["down","up"] as const) {
 test(`keyboard boundary ${direction} uses the adjacent absolute row in a large nonaligned window`, async()=>{
  const {ReaderGrid}=await import("./ReaderGrid");
  const rows=Array.from({length:180},(_,index)=>({...nodes[0]!,node:{...nodes[0]!.node,path:`SEG[${index+74}]`},display_path:`SEG[${index+74}]`}));
  const onPage=vi.fn();
  render(<ReaderGrid grid={{mode:"message",segment:"",offset:73,field_count:0,row_count:1000,rows}} selected={rows[direction==="down"?179:0]!.node.path} busy={false} value={()=>""} onSelect={()=>undefined} onPage={onPage}/>);
  const grid=screen.getByRole("treegrid",{name:"Message fields"});
  onPage.mockClear();
  await act(async()=>fireEvent.keyDown(grid,{key:direction==="down"?"ArrowDown":"ArrowUp"}));
  expect(onPage).toHaveBeenCalledWith(...(direction==="down"?[253,"first",180]:[0,"last",73]));
 });
}

test("wide tables share spare width between Name and Value while preserving manual resizing", async()=>{
 const {ReaderGrid}=await import("./ReaderGrid");
 const user=userEvent.setup();
 render(<ReaderGrid grid={gridResult().inspection!.grid!} selected="" busy={false} value={()=>""} onSelect={()=>undefined}/>);
 const grid=screen.getByRole("treegrid",{name:"Message fields"});
 Object.defineProperty(grid,"clientWidth",{configurable:true,value:1200});
 fireEvent.resize(window);
 const name=screen.getByRole("separator",{name:"Name column width"});
 const value=screen.getByRole("separator",{name:"Value column width"});
 expect(Number(name.getAttribute("aria-valuenow"))).toBeGreaterThan(17.5);
 expect(Number(value.getAttribute("aria-valuenow"))).toBeGreaterThan(20);
 const before=Number(name.getAttribute("aria-valuenow"));
 name.focus();await user.keyboard("{ArrowRight}");
 expect(Number(name.getAttribute("aria-valuenow"))).toBeCloseTo(before+1,1);
 const kept=Number(name.getAttribute("aria-valuenow"));
 const oldValue=Number(value.getAttribute("aria-valuenow"));
 Object.defineProperty(grid,"clientWidth",{configurable:true,value:1400});
 fireEvent.resize(window);
 expect(Number(name.getAttribute("aria-valuenow"))).toBe(kept);
 expect(Number(value.getAttribute("aria-valuenow"))).toBeGreaterThan(oldValue);
 const beforeValue=Number(value.getAttribute("aria-valuenow"));
 value.focus();await user.keyboard("{ArrowLeft}");
 expect(Number(value.getAttribute("aria-valuenow"))).toBeCloseTo(beforeValue-1,1);
 name.focus();await user.keyboard("{End}");
 const wideName=Number(name.getAttribute("aria-valuenow"));
 Object.defineProperty(grid,"clientWidth",{configurable:true,value:560});
 fireEvent.resize(window);
 await user.keyboard("{ArrowLeft}");
 expect(Number(name.getAttribute("aria-valuenow"))).toBeCloseTo(wideName-1,1);
});
