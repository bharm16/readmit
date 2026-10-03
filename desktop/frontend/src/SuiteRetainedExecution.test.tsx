import { test, expect } from "vitest";
import { render, screen, within, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { SuiteRetainedExecution } from "./SuiteRetainedExecution";
import { installFacade } from "./testkit/wails";
import type { RequestContext, RunDetail } from "./bindings";

const context:RequestContext={project:"/projects/example",generation:1};
const run:RunDetail={item:{ref:{kind:"run",id:"retained-suite"},name:"Retained suite",availability:"available",created_at:null,updated_at:null,last_opened_at:null,capabilities:[],summary:{}},name:"Retained suite",delivery_uncertain:false,checks:[],messages:[],jobs:[{id:"one",test:"First test",delivery_uncertain:false,result:"passed"},{id:"two",test:"Second test",delivery_uncertain:false,result:"failed"}],details:{journal_incomplete:false,recovered:false,started_at:null,completed_at:null,gaps:[]},revealed:false};

test("suite results read one exact retained job at a time and never credit a previous count to the newly selected job",async()=>{
 const user=userEvent.setup();let release:()=>void=()=>undefined;
 const gate=new Promise<void>(resolve=>{release=resolve;});
 const facade=installFacade({OpenRun:async request=>{
  if(request.job==="two")await gate;
  return {state:"completed",context:request.context,run:{...run,...(request.job?{job:request.job,checks:[{check:{id:"count",operator:"ledger_count",count:1},result:"passed",observed_records:request.job==="one"?1:2,hidden:true,messages:[]}]}:{})}};
 }});
 render(<SuiteRetainedExecution runs={[{run:run.item.ref,started_at:null,outcome:"passed"}]} context={()=>context}/>);
 const table=within(await screen.findByRole("table",{name:"Selected execution results"}));
 await waitFor(()=>expect(within(table.getByRole("row",{name:"First test"})).getAllByRole("cell").map(cell=>cell.textContent)).toEqual(["1","1","passed"]));
 expect(within(table.getByRole("row",{name:"Second test"})).getAllByRole("cell")[1]?.textContent).toBe("Open evidence");
 await user.click(table.getByRole("row",{name:"Second test"}));
 expect(within(table.getByRole("row",{name:"Second test"})).getAllByRole("cell")[1]?.textContent).toBe("Open evidence");
 release();await waitFor(()=>expect(within(table.getByRole("row",{name:"Second test"})).getAllByRole("cell")[1]?.textContent).toBe("2"));
 expect(facade.callsTo("OpenRun").map(call=>call.args[0])).toEqual([{context,run:run.item.ref,reveal:false},{context,run:run.item.ref,job:"one",reveal:false},{context,run:run.item.ref,job:"two",reveal:false}]);
 expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
});

test("a connected suite job with typed count results keeps an unavailable count projection explicit",async()=>{
 const retained:RunDetail={...run,connected_report:{schema:"readmit-connected-queue-report/v1",parallelism:1,executed:1,start_failed:0,refused:1,skipped:0,jobs:[{id:"one",admission:"executed",reason:"",resources:[],flow:{schema:"readmit-connected-flow-run/v4",plan:"retained-plan",instance:"retained-instance",boundary:"downstream-application",engine:"retained-engine",state:"complete",verdict:"fail",started_at:"",completed_at:"",setup:"complete",cleanup:"complete",isolation:"retained-isolation",phases:[{id:"book",state:"complete",verdict:"fail",run_identity:"retained-phase",steps:[],checks:[{id:"typed:booked-count",outcome:"failed"}]}]}},{id:"two",admission:"refused",reason:"Installed authority unavailable",resources:[]} ]}};
 const facade=installFacade({OpenRun:request=>({state:"completed",context:request.context,run:{...retained,...(request.job?{job:request.job}:{})}})});
 render(<SuiteRetainedExecution runs={[{run:run.item.ref,started_at:null,outcome:"failed"}]} context={()=>context}/>);
 const table=within(await screen.findByRole("table",{name:"Selected execution results"}));
 await waitFor(()=>expect(within(table.getByRole("row",{name:"First test"})).getAllByRole("cell").slice(0,2).map(cell=>cell.textContent)).toEqual(["Unavailable projection","Unavailable projection"]));
 expect(screen.queryByText("No count expectation")).toBeNull();
 expect(screen.queryByText("No count observation")).toBeNull();
 expect(table.getByRole("row",{name:"Second test"})).toBeTruthy();
 expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
});

test("a selected job's available typed count projection preserves actual zero and unavailable observations",async()=>{
 const user=userEvent.setup();
 const facade=installFacade({OpenRun:request=>({state:"completed",context:request.context,run:{...run,...(request.job?{job:request.job,lifecycle:{identity:"retained-job",lifecycle:{output:"retained-job",state:"complete",verdict:"fail",boundary:"application-state",setup:"complete",cleanup:"complete",phases:[],qualification:[]},steps:[],observations:[],checks:[{phase:"book",id:"typed:booked-count",kind:"application",operator:"row-count",outcome:"failed",expected_count:0,observed_count:2,hidden:true,...(request.job==="two"?{unavailable:"the observation did not complete"}:{})}]}}:{})}})});
 render(<SuiteRetainedExecution runs={[{run:run.item.ref,started_at:null,outcome:"failed"}]} context={()=>context}/>);
 const table=within(await screen.findByRole("table",{name:"Selected execution results"}));
 await waitFor(()=>expect(within(table.getByRole("row",{name:"First test"})).getAllByRole("cell").slice(0,2).map(cell=>cell.textContent)).toEqual(["0","2"]));
 await user.click(table.getByRole("row",{name:"Second test"}));
 await waitFor(()=>expect(within(table.getByRole("row",{name:"Second test"})).getAllByRole("cell").slice(0,2).map(cell=>cell.textContent)).toEqual(["0","Unavailable"]));
 expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
});
