import { useState } from "react";
import { render, screen, within, fireEvent } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, test } from "vitest";
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
