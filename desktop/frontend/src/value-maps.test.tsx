import { act,render,screen,waitFor,within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect,test } from "vitest";
import { ValueMaps } from "./ValueMaps";
import type { ItemRef,ValuemapDocument,ValueMapsResult,ValueMapResult } from "./bindings";
import { installFacade } from "./testkit/wails";
import { caseCatalogItem } from "./testkit/fixtures";
const context=()=>({project:"/owned-project",generation:1});const source:ItemRef={kind:"case",id:"source"};const ref:ItemRef={kind:"field-value-map",id:"map",revision:"1"};
const props={context,source,identity:"owned-identity",occurrence:"owned-occurrence",selector:"MSH[1]-3[1]",edition:"2.5.1",onBack:()=>undefined};
const map:ValuemapDocument={schema:"readmit-field-value-map/v1",project:"0123456789abcdef01234567",name:"Owned sending application map",edition:"2.5.1",source_selector:"MSH[1]-3[1]",destination_selector:"MSH[1]-3[1]",source_meaning:"Owned sender",destination_meaning:"Recorded receiver",provenance:"Owner declaration",entries:[],associations:[]};
const listing:ValueMapsResult={state:"completed",context:context(),items:[{...caseCatalogItem("map"),ref,name:map.name}]};

test("unsaved map authoring requires explicit discard before return",async()=>{const user=userEvent.setup();let backs=0;installFacade({ListValueMaps:()=>({state:"completed",context:context(),items:[]})});render(<ValueMaps {...props} onBack={()=>{backs++;}}/>);await user.click(screen.getByRole("button",{name:"Create value map"}));await user.type(screen.getByLabelText("Name"),"Owned unsaved map");await user.click(screen.getByRole("button",{name:"Return to selected message"}));expect(backs).toBe(0);expect(await screen.findByRole("dialog",{name:"Discard unsaved value map?"})).toBeTruthy();await user.click(within(screen.getByRole("dialog",{name:"Discard unsaved value map?"})).getByRole("button",{name:"Discard draft"}));expect(backs).toBe(1);});

test("source changes reset reveal and delayed contextual mappings cannot expose the prior owner",async()=>{
 const user=userEvent.setup();let finish:((result:import("./bindings").ValueMapInspectionResult)=>void)|undefined;
 const facade=installFacade({ListValueMaps:()=>listing,ReadValueMap:()=>({state:"completed",context:context(),ref,map}),InspectValueMap:request=>request.reveal?new Promise(resolve=>{finish=resolve;}):{state:"completed",context:request.context,ref:request.ref,identity:request.identity,occurrence:request.occurrence,selector:request.selector,revealed:false,mapping:"hidden",field_state:"present"}});
 const {rerender}=render(<ValueMaps {...props}/>);await user.click(await screen.findByRole("button",{name:"Show contextual mapping for this source"}));await waitFor(()=>expect(finish).toBeDefined());rerender(<ValueMaps {...props} identity="new-source"/>);await waitFor(()=>expect(facade.callsTo("InspectValueMap").at(-1)?.args[0]).toMatchObject({identity:"new-source",reveal:false}));await act(async()=>finish?.({state:"completed",context:context(),ref,identity:"owned-identity",occurrence:props.occurrence,selector:props.selector,revealed:true,mapping:"mapped",field_state:"present",entry:{source:"OWNED",destination:"STALE DESTINATION",source_meaning:"Owned",destination_meaning:"Old",provenance:"Old owner"}}));expect(screen.queryByText(/STALE DESTINATION/)).toBeNull();
});

test("late CSV drafts cannot replace another source's authoring state",async()=>{const user=userEvent.setup();let finish:((result:ValueMapResult)=>void)|undefined;installFacade({ListValueMaps:()=>({state:"completed",context:context(),items:[]}),ImportValueMapCSV:()=>new Promise(resolve=>{finish=resolve;})});const {rerender}=render(<ValueMaps {...props}/>);await user.click(screen.getByRole("button",{name:"Import CSV"}));await waitFor(()=>expect(finish).toBeDefined());rerender(<ValueMaps {...props} identity="new-source"/>);await act(async()=>finish?.({state:"completed",context:context(),map}));expect(screen.queryByText(/Owned sending application map/)).toBeNull();});

test("an unknown source edition stays unknown when the user authors a map edition",async()=>{
 const user=userEvent.setup();
 const facade=installFacade({ListValueMaps:()=>({state:"completed",context:context(),items:[]}),ReadValueMapDraft:()=>({state:"empty",context:context()}),SaveEditorDraft:draft=>({state:"completed",drafts:[{...draft,id:"owned-retained-draft"}]})});
 render(<ValueMaps {...props} edition=""/>);
 expect(screen.getByText(/source edition Not declared/)).toBeTruthy();
 await user.click(screen.getByRole("button",{name:"Create value map"}));
 await user.type(screen.getByLabelText("HL7 edition"),"2.5.1");
 await waitFor(()=>expect(facade.callsTo("SaveEditorDraft").at(-1)?.args[0]).toMatchObject({content:{edition:"",draft:{edition:"2.5.1"}}}));
 expect(screen.getByText(/source edition Not declared/)).toBeTruthy();
});

test("unretained and future map files stay visible without issuing dead revision reads",async()=>{
 const user=userEvent.setup();
 const unavailable={...caseCatalogItem("unretained"),name:"Owned unpublished map",ref:{kind:"field-value-map" as const,id:"unretained-map"},availability:"unreadable" as const,reason:"This map file has no retained catalog revision; import CSV and save explicitly.",capabilities:[]};
 const future={...unavailable,name:"Owned future declaration",ref:{kind:"field-value-map" as const,id:"future-map"},availability:"unsupported" as const,reason:"It declares a contract version this release does not read."};
 const facade=installFacade({ListValueMaps:()=>({state:"completed",context:context(),items:[unavailable,future]}),ReadValueMapDraft:()=>({state:"empty",context:context()})});
 render(<ValueMaps {...props}/>);
 const table=await screen.findByRole("table",{name:"Interface value maps"});
 expect(await within(table).findByText("Owned unpublished map")).toBeTruthy();expect(within(table).getByText("Unsupported schema")).toBeTruthy();
 await user.click(table.querySelector<HTMLElement>('[data-row-id="unretained-map"]')!);
 expect(screen.getAllByText(/no retained catalog revision/).length).toBeGreaterThan(0);
 await user.dblClick(table.querySelector<HTMLElement>('[data-row-id="future-map"]')!);
 expect(facade.callsTo("ReadValueMap")).toHaveLength(0);expect(facade.callsTo("InspectValueMap")).toHaveLength(0);
 expect(screen.queryByRole("button",{name:"Edit map"})).toBeNull();
});

test("reselecting the displayed retained map preserves its pane and current authored draft",async()=>{
 const user=userEvent.setup();installFacade({ListValueMaps:()=>listing,ReadValueMapDraft:()=>({state:"empty",context:context()}),ReadValueMap:()=>({state:"completed",context:context(),ref,map}),InspectValueMap:request=>({state:"completed",context:request.context,ref,identity:request.identity,occurrence:request.occurrence,selector:request.selector,revealed:false,mapping:"hidden",field_state:"present"})});
 render(<ValueMaps {...props}/>);await screen.findByRole("button",{name:"Edit map"});const row=screen.getByRole("table",{name:"Interface value maps"}).querySelector<HTMLElement>('[data-row-id="map"]')!;
 await user.click(row);expect(screen.getByRole("button",{name:"Edit map"})).toBeTruthy();await user.click(screen.getByRole("button",{name:"Edit map"}));await user.click(row);expect(screen.getByLabelText("Name")).toBeTruthy();expect(screen.queryByRole("dialog",{name:"Discard unsaved value map?"})).toBeNull();
});
