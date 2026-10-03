import { render,screen,within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { useRunComparison } from "./RunComparison";
import { installFacade } from "./testkit/wails";
import { expect,test } from "vitest";
import { ConnectedRunComparison } from "./ConnectedRunComparison";
import type { RunComparisonView, CatalogItem } from "./bindings";

const before={run:{kind:"run" as const,id:"newer-before"},name:"Newer run",started_at:null};
const after={run:{kind:"run" as const,id:"older-after"},name:"Older run",started_at:null};
const comparison:RunComparisonView={earlier:before,later:after,repeats:[],checks:[],configuration:[],specification:"",stability:{state:"",runs:0,passes:0,failures:0,errors:0,incomplete:0,flaky:[],reason:""},connected:{baseline:{identity:"before-retained",schema:"flow",plan:"before-plan",instance:"before-instance",engine:"retained-original-engine",state:"incomplete",verdict:"undecided"},current:{identity:"after-retained",schema:"flow",plan:"after-plan",instance:"after-instance",engine:"retained-new-engine",state:"complete",verdict:"pass"},dimensions:[{dimension:"check-definition",state:"changed",changed:["authored-count"]},{dimension:"input",state:"unchanged",changed:[]}],attribution:{outcome:"multiple-declared-changes",changed:["check-definition","engine"],reason:"Several recorded declarations changed; no cause is established."},checks:[{phase:"phase-1",check:"typed:count",baseline:"failed",current:"passed",definition:"changed",behavior:"not_compared"}],records:[{phase:"phase-1",dataset:"appointments",state:"not_compared",reason:"Insufficient coverage is not zero",keys:[]}],scope:"Compared offline from verified retained evidence."}};
const candidates:CatalogItem[]=[before,after].map(side=>({ref:side.run,name:side.name,availability:"available",created_at:null,updated_at:null,last_opened_at:null,capabilities:[],summary:{}}));

test("connected comparison preserves explicit roles, changed definitions and unavailable observations",async()=>{
 const user=userEvent.setup();const choices:string[][]=[];const opened:string[]=[];
 render(<ConnectedRunComparison comparison={comparison} candidates={candidates} onRuns={ids=>choices.push(ids)} onOpen={id=>opened.push(id)} />);
 expect(screen.getByLabelText("Before")).toHaveProperty("value","newer-before");
 const checks=within(screen.getByRole("table",{name:"Connected comparison checks"}));expect(checks.getByText("not_compared")).toBeTruthy();
 const records=within(screen.getByRole("table",{name:"Compared retained observations"}));expect(records.getAllByText("Unavailable")).toHaveLength(2);expect(records.queryByText("0")).toBeNull();
 await user.click(screen.getByRole("button",{name:"Open before evidence"}));expect(opened).toEqual(["newer-before"]);
 await user.click(screen.getByRole("button",{name:"Swap before and after"}));expect(choices).toEqual([["older-after","newer-before"]]);
 expect(screen.getByText(/Original build and target availability are not established/)).toBeTruthy();
});


test("initial connected reverse pair resolves metadata before first comparison and first Swap changes actual roles",async()=>{
 const user=userEvent.setup();
 const catalog=candidates.map(item=>({...item,summary:{run:{started_at:null,completed_at:null,uncertain:0,delivery_uncertain:false,active:false,kind:"test" as const,can_compare:true,result:"passed" as const}}}));
 const facade=installFacade({ListCatalog:query=>({state:"completed",context:query.context,page:{items:catalog,total:2,snapshot:"history",recorded:true,incomplete:[]}}),CompareRunItems:request=>({state:"completed",context:request.context,comparison:{...comparison,earlier:{...before,run:request.before!},later:{...after,run:request.after!}}})});
 function Host(){const [runs,setRuns]=useState([before.run.id,after.run.id]);const view=useRunComparison({root:"/history",runs,view:"checks",onView:()=>{},onRuns:setRuns,onOpen:()=>{}});return <main>{view.body}</main>}
 render(<Host/>);
 await screen.findByRole("region",{name:"Connected Before and After"});
 expect(facade.callsTo("CompareRunItems")[0]?.args[0]).toMatchObject({before:before.run,after:after.run});
 await user.click(screen.getByRole("button",{name:"Swap before and after"}));
 await screen.findByRole("region",{name:"Connected Before and After"});
 expect(facade.callsTo("CompareRunItems")[1]?.args[0]).toMatchObject({before:after.run,after:before.run});
 expect(facade.callsTo("ListCatalog")).toHaveLength(2);
});
