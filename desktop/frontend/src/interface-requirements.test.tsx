import { act,render,screen,waitFor,within } from "@testing-library/react";
import { expect,test } from "vitest";
import userEvent from "@testing-library/user-event";
import { InterfaceRequirements } from "./InterfaceRequirements";
import type { CatalogResult,InterfaceSpecsResult,ItemRef } from "./bindings";
import { installFacade } from "./testkit/wails";
import { caseCatalogItem } from "./testkit/fixtures";

const context=()=>({project:"/owned-project",generation:1});
const source:ItemRef={kind:"case",id:"owned-source"};
const props={context,source,identity:"owned-identity",occurrence:"",selector:"",onBack:()=>undefined};

test("whole-case requirements remain explicit and a delayed old specification list cannot populate another source",async()=>{
 let finish:((result:InterfaceSpecsResult)=>void)|undefined;let calls=0;
 installFacade({ListInterfaceSpecs:()=>++calls===1?new Promise<InterfaceSpecsResult>(resolve=>{finish=resolve;}):{state:"completed",context:context(),items:[]}});
 const {rerender}=render(<InterfaceRequirements {...props}/>);
 await waitFor(()=>expect(finish).toBeDefined());
 expect(screen.getByText(/Whole retained interface case; no field occurrence/)).toBeTruthy();
 rerender(<InterfaceRequirements {...props} identity="new-source-identity"/>);
 await waitFor(()=>expect(calls).toBe(2));
 await act(async()=>finish?.({state:"completed",context:context(),items:[{...caseCatalogItem("old"),ref:{kind:"interface-spec",id:"old-spec",revision:"1"},name:"Old owner specification"}]}));
 expect(screen.queryByText("Old owner specification")).toBeNull();
});

test("a delayed profile chooser cannot reopen the old owner's authoring dialog",async()=>{
 const user=userEvent.setup();let finish:((result:CatalogResult)=>void)|undefined;
 installFacade({ListInterfaceSpecs:()=>({state:"completed",context:context(),items:[]}),ListCatalog:()=>new Promise<CatalogResult>(resolve=>{finish=resolve;})});
 const {rerender}=render(<InterfaceRequirements {...props}/>);
 await user.click(screen.getByRole("button",{name:"Add specification"}));
 await waitFor(()=>expect(finish).toBeDefined());
 rerender(<InterfaceRequirements {...props} identity="new-source-identity"/>);
 await act(async()=>finish?.({state:"completed",context:context(),page:{items:[],total:0,snapshot:"s",recorded:true,incomplete:[]}}));
 expect(screen.queryByRole("dialog",{name:"Interface specification"})).toBeNull();
});

test("unretained and future specification files cannot open revision-dependent requirements",async()=>{
 const user=userEvent.setup();
 const unpublished={...caseCatalogItem("unpublished"),name:"Owned unpublished specification",ref:{kind:"interface-spec" as const,id:"unretained-spec"},availability:"unreadable" as const,reason:"This specification file has no retained catalog revision; create a specification explicitly.",capabilities:[]};
 const future={...unpublished,name:"Owned future specification",ref:{kind:"interface-spec" as const,id:"future-spec"},availability:"unsupported" as const,reason:"It declares a contract version this release does not read."};
 const facade=installFacade({ListInterfaceSpecs:()=>({state:"completed",context:context(),items:[unpublished,future]})});
 render(<InterfaceRequirements {...props}/>);
 const table=await screen.findByRole("table",{name:"Interface specifications"});
 expect(await within(table).findByText("Owned unpublished specification")).toBeTruthy();expect(within(table).getByText("Unsupported schema")).toBeTruthy();
 await user.click(table.querySelector<HTMLElement>('[data-row-id="unretained-spec"]')!);await user.dblClick(table.querySelector<HTMLElement>('[data-row-id="future-spec"]')!);
 expect(facade.callsTo("ReadInterfaceRequirements")).toHaveLength(0);
 expect((screen.getByRole("button",{name:"Edit specification association"}) as HTMLButtonElement).disabled).toBe(true);
});
