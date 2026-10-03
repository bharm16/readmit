import {act,render,screen,within,waitFor} from "@testing-library/react";
import {expect,test,vi} from "vitest";
import userEvent from "@testing-library/user-event";
import {CaptureSourceContext} from "./CaptureSourceContext";
import {installFacade} from "./testkit/wails";
import {caseCatalogItem} from "./testkit/fixtures";
import type {EditorDraft,CaptureSourceEditorDraft} from "./bindings";
const context=()=>({project:"/owned-project",project_id:"project-owner",generation:1});
const source={kind:"case" as const,id:"source-owner"};const identity="a".repeat(64);
test("partial source corrections survive closing and reopening their exact owner without publication",async()=>{
 let held:EditorDraft|undefined;const user=userEvent.setup();
 const facade=installFacade({ListCatalog:request=>({state:"completed",context:request.context,page:{items:[],total:0,snapshot:"s",recorded:true,incomplete:[]}}),ReadCaptureContext:request=>({state:"completed",context:request.context,capture:{schema:"readmit-capture-context/v1",case:source,identity,sources:[{source_id:"source-one",source:"",channel:"",basis:"unknown"}]}}),SaveEditorDraft:value=>{held={...value,id:"private-correction"};return {state:"completed",drafts:[held]};},ReadContextEditorDraft:request=>held?{state:"completed",context:request.context,retained:held,correction:held.content as CaptureSourceEditorDraft}:{state:"empty",context:request.context}});
 const props={source,context,onClose:()=>{},onChanged:()=>{}};const mounted=render(<CaptureSourceContext {...props}/>);
 await user.click(await screen.findByRole("button",{name:"Correct source context"}));const form=within(await screen.findByRole("dialog",{name:"Correct source context"}));await user.type(form.getByLabelText("Source"),"Partial authored source");await waitFor(()=>expect(held).toBeDefined());mounted.unmount();
 render(<CaptureSourceContext {...props}/>);const restored=within(await screen.findByRole("dialog",{name:"Correct source context"}));expect(await restored.findByDisplayValue("Partial authored source")).toBeTruthy();expect(restored.getByLabelText("Channel")).toHaveProperty("value","");expect(facade.callsTo("SaveCaptureContext")).toHaveLength(0);
});

import {InterfaceRequirements} from "./InterfaceRequirements";
import type {InterfaceAssociationEditorDraft} from "./bindings";
test("unfinished specification association restores authored name and documentation without copying inspected values or publishing",async()=>{
 let held:EditorDraft|undefined;const user=userEvent.setup();const facade=installFacade({ListInterfaceSpecs:request=>({state:"completed",context:request,items:[]}),ListCatalog:request=>({state:"completed",context:request.context,page:{items:[],total:0,snapshot:"s",recorded:true,incomplete:[]}}),ReadContextEditorDraft:request=>held?{state:"completed",context:request.context,retained:held,association:held.content as InterfaceAssociationEditorDraft}:{state:"empty",context:request.context},SaveEditorDraft:value=>{held={...value,id:"private-spec"};return {state:"completed",drafts:[held]};}});
 facade.reply({ChooseInterfaceSpecDocument:()=>({state:"completed",context:context(),document:{name:"authored-note.md",text:"Explicit authored documentation",sha256:"b".repeat(64)}})});
 const props={context,source,identity,occurrence:"",selector:"",onBack:()=>{}};const mounted=render(<InterfaceRequirements {...props}/>);await user.click(await screen.findByRole("button",{name:"Add specification"}));const form=within(await screen.findByRole("dialog",{name:"Interface specification"}));await user.type(form.getByLabelText("Name"),"Unfinished association");await user.click(form.getByRole("button",{name:"Add document"}));await waitFor(()=>expect(held).toBeDefined());mounted.unmount();
 render(<InterfaceRequirements {...props}/>);const restored=within(await screen.findByRole("dialog",{name:"Interface specification"}));expect(await restored.findByDisplayValue("Unfinished association")).toBeTruthy();expect(await restored.findByText(/authored-note.md ·/)).toBeTruthy();expect((restored.getByRole("button",{name:"Save specification"}) as HTMLButtonElement).disabled).toBe(true);expect(facade.callsTo("SaveInterfaceSpec")).toHaveLength(0);expect(JSON.stringify(held?.content)).not.toContain('"reveal"');
});

test("source correction publication and private discard refusal keep the exact authored fields",async()=>{
 const user=userEvent.setup();const facade=installFacade({ListCatalog:request=>({state:"completed",context:request.context,page:{items:[],total:0,snapshot:"s",recorded:true,incomplete:[]}}),ReadCaptureContext:request=>({state:"completed",context:request.context,capture:{schema:"readmit-capture-context/v1",case:source,identity,sources:[{source_id:"source-one",source:"",channel:"",basis:"unknown"}]}}),ReadContextEditorDraft:request=>({state:"empty",context:request.context}),SaveEditorDraft:value=>({state:"completed",drafts:[{...value,id:"held-correction"}]}),SaveCaptureContext:request=>({state:"failed",context:request.context,reason:"Exact base revision changed"}),DiscardEditorDraft:()=>({state:"failed",reason:"Private discard refused"})});
 render(<CaptureSourceContext source={source} context={context} onClose={()=>{}} onChanged={()=>{}}/>);await user.click(await screen.findByRole("button",{name:"Correct source context"}));const form=within(await screen.findByRole("dialog",{name:"Correct source context"}));await user.type(form.getByLabelText("Source"),"Kept authored source");await waitFor(()=>expect(facade.callsTo("SaveEditorDraft").length).toBeGreaterThan(0));await user.click(form.getByRole("button",{name:"Save"}));await form.findByText("Exact base revision changed");expect(form.getByLabelText("Source")).toHaveProperty("value","Kept authored source");await user.click(form.getByRole("button",{name:"Discard draft"}));await form.findByText("The retained corrections could not be discarded. Your work is kept.");expect(form.getByLabelText("Source")).toHaveProperty("value","Kept authored source");
});

test("a delayed initial private restore cannot overwrite corrections authored after editing begins",async()=>{
 const user=userEvent.setup();let finish:(()=>void)|undefined;
 const correction:CaptureSourceEditorDraft={schema:"readmit-capture-source-context-editor/v1",project_id:"project-owner",source,identity,sources:[{source_id:"source-one",source:"Old private snapshot",channel:"",basis:"manual"}]};
 installFacade({ListCatalog:request=>({state:"completed",context:request.context,page:{items:[],total:0,snapshot:"s",recorded:true,incomplete:[]}}),ReadCaptureContext:request=>({state:"completed",context:request.context,capture:{schema:"readmit-capture-context/v1",case:source,identity,sources:[{source_id:"source-one",source:"Original file",channel:"",basis:"unknown"}]}}),ReadContextEditorDraft:request=>new Promise(resolve=>{finish=()=>resolve({state:"completed",context:request.context,correction,retained:{id:"earlier-private-work",kind:"capture-source-context",workspace:request.context.project,case:"",identity,content_schema:correction.schema,content:correction}});}),SaveEditorDraft:value=>({state:"completed",drafts:[{...value,id:"current-authored-work"}]})});
 render(<CaptureSourceContext source={source} context={context} onClose={()=>{}} onChanged={()=>{}}/>);await user.click(await screen.findByRole("button",{name:"Correct source context"}));const form=within(await screen.findByRole("dialog",{name:"Correct source context"}));await user.clear(form.getByLabelText("Source"));await user.type(form.getByLabelText("Source"),"Latest authored correction");await waitFor(()=>expect(finish).toBeDefined());await act(async()=>finish?.());expect(form.getByLabelText("Source")).toHaveProperty("value","Latest authored correction");
});

test("a restored receive target pin does not display or adopt a newer same-ID target revision",async()=>{
 const user=userEvent.setup();const old={kind:"environment" as const,id:"target-owner",revision:"1"};const latest={...caseCatalogItem("target-owner"),ref:{...old,revision:"2"},name:"New target name"};const correction:CaptureSourceEditorDraft={schema:"readmit-capture-source-context-editor/v1",project_id:"project-owner",source,identity,sources:[{source_id:"source-one",source:"Authored source",channel:"Authored channel",basis:"manual",received_at:old}]};
 const facade=installFacade({ListCatalog:request=>({state:"completed",context:request.context,page:{items:request.kind==="environment"?[latest]:[],total:request.kind==="environment"?1:0,snapshot:"s",recorded:true,incomplete:[]}}),ReadCaptureContext:request=>({state:"completed",context:request.context,capture:{schema:"readmit-capture-context/v1",case:source,identity,sources:[{source_id:"source-one",source:"",channel:"",basis:"unknown"}]}}),ReadContextEditorDraft:request=>({state:"completed",context:request.context,correction,retained:{id:"old-target-draft",kind:"capture-source-context",workspace:request.context.project,case:"",identity,content_schema:correction.schema,content:correction}}),SaveEditorDraft:value=>({state:"completed",drafts:[{...value,id:"old-target-draft"}]})});
 render(<CaptureSourceContext source={source} context={context} onClose={()=>{}} onChanged={()=>{}}/>);const form=within(await screen.findByRole("dialog",{name:"Correct source context"}));const target=form.getByLabelText("Received at");expect(target).toHaveProperty("value","environment:target-owner@1");expect((target as HTMLSelectElement).selectedOptions[0]?.textContent).toContain("Recorded revision 1");await user.selectOptions(target,"environment:target-owner@2");await waitFor(()=>expect(facade.callsTo("SaveEditorDraft").at(-1)?.args[0]).toMatchObject({content:{sources:[{received_at:{revision:"2"}}]}}));expect(facade.callsTo("SaveCaptureContext")).toHaveLength(0);
});

test.each(["unmount", "source", "project", "context"] as const)("a successful delayed specification flush cannot navigate after %s changes its owner",async(change)=>{
 const user=userEvent.setup();const onBack=vi.fn();let release:(()=>void)|undefined;let kept:EditorDraft|undefined;
 const authored:InterfaceAssociationEditorDraft={schema:"readmit-interface-association-editor/v1",project_id:"project-owner",source,identity,occurrence:"",selector:"",name:"Held name",documents:[]};
 const facade=installFacade({
  ListInterfaceSpecs:request=>({state:"completed",context:request,items:[]}),
  ListCatalog:request=>({state:"completed",context:request.context,page:{items:[],total:0,snapshot:"s",recorded:true,incomplete:[]}}),
  ReadContextEditorDraft:request=>request.context.project===context().project&&request.source.id===source.id ? {state:"completed",context:request.context,association:authored,retained:{id:"private-spec",kind:"interface-association",workspace:request.context.project,case:"",identity,content_schema:authored.schema,content:authored}}:{state:"empty",context:request.context},
  SaveEditorDraft:value=>{if(release)return {state:"completed",drafts:[{...value,id:"new-private-spec"}]};return new Promise(resolve=>{release=()=>{kept={...value,id:"private-spec"};resolve({state:"completed",drafts:[kept]});};});},
 });
 const props={context,source,identity,occurrence:"",selector:"",onBack};const mounted=render(<InterfaceRequirements {...props}/>);
 const form=within(await screen.findByRole("dialog",{name:"Interface specification"}));await user.type(form.getByLabelText("Name"),"!");await waitFor(()=>expect(release).toBeDefined());
 await user.click(screen.getByRole("button",{name:"Return to selected message"}));expect(onBack).not.toHaveBeenCalled();
 if(change==="unmount")mounted.unmount();
 else if(change==="source")mounted.rerender(<InterfaceRequirements {...props} source={{kind:"case",id:"new-source-owner"}} identity={"c".repeat(64)}/>);
 else {const nextContext=()=>({...context(),...(change==="project" ? {project:"/new-owned-project",project_id:"new-project-owner"}:{generation:2})});mounted.rerender(<InterfaceRequirements {...props} context={nextContext}/>);}
 await act(async()=>release?.());
 expect(onBack).not.toHaveBeenCalled();expect(kept?.content).toMatchObject({name:"Held name!",source,identity});expect(facade.callsTo("SaveInterfaceSpec")).toHaveLength(0);
});

test("a refused specification draft flush keeps authored fields and does not navigate",async()=>{
 const user=userEvent.setup();const onBack=vi.fn();let release:(()=>void)|undefined;
 const authored:InterfaceAssociationEditorDraft={schema:"readmit-interface-association-editor/v1",project_id:"project-owner",source,identity,occurrence:"",selector:"",name:"Held name",documents:[]};
 installFacade({
  ListInterfaceSpecs:request=>({state:"completed",context:request,items:[]}),
  ListCatalog:request=>({state:"completed",context:request.context,page:{items:[],total:0,snapshot:"s",recorded:true,incomplete:[]}}),
  ReadContextEditorDraft:request=>({state:"completed",context:request.context,association:authored,retained:{id:"private-spec",kind:"interface-association",workspace:request.context.project,case:"",identity,content_schema:authored.schema,content:authored}}),
  SaveEditorDraft:()=>new Promise(resolve=>{release=()=>resolve({state:"failed",reason:"Private write refused"});}),
 });
 render(<InterfaceRequirements context={context} source={source} identity={identity} occurrence="" selector="" onBack={onBack}/>);
 const form=within(await screen.findByRole("dialog",{name:"Interface specification"}));await user.type(form.getByLabelText("Name"),"!");await waitFor(()=>expect(release).toBeDefined());
 await user.click(screen.getByRole("button",{name:"Return to selected message"}));await act(async()=>release?.());
 expect(onBack).not.toHaveBeenCalled();expect(form.getByLabelText("Name")).toHaveProperty("value","Held name!");expect(form.getByText("Private write refused")).toBeTruthy();expect(form.getByRole("button",{name:"Save specification"})).toBeTruthy();
});
