// Real source/catalog files and the production Go facade, with independently
// authored reference prose. Official-source Go checks cover controlled extracts.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Journey, press } from "../testkit/journey";
import type { Hl7referenceRecord } from "../bindings";

let journey:Journey;
beforeEach(()=>{journey=Journey.create();});
afterEach(async()=>{await journey.dispose();});

function ownedCatalog():string {
  const specified=(value:string)=>({state:"specified",value});
  const absent={state:"not_specified",value:""};
  const na={state:"not_applicable",value:""};
  const records:Hl7referenceRecord[]=[
    {key:"segment/MSH",kind:"segment",segment:"MSH",field:0,name:"Owned message header",datatype:na,optionality:na,length:na,conformance_length:na,repetition:na,item:na,table:na,section:specified("2.15.9"),definition:"Owned reference overview for the message header.",source:"owned-chapter.pdf"},
    {key:"field/MSH/9",kind:"field",segment:"MSH",field:9,name:"Message Type",datatype:specified("MSG"),optionality:specified("R"),length:specified("15"),conformance_length:absent,repetition:absent,item:specified("00009"),table:absent,section:specified("2.15.9.9"),definition:"Owned reference definition for the message code and trigger.",source:"owned-chapter.pdf"},
  ];
  return JSON.stringify({schema:"readmit-hl7-reference/v1",edition:"2.5.1",sources:[{role:"standard",file:"owned-chapter.pdf",sha256:"a".repeat(64),publisher:"Readmit owned fixture"}],coverage:{segments:1,fields:1,definitions:2,missing:[]},records});
}

test("real reader selection keeps original spans, all metadata columns and explicit offline reference coverage",async()=>{
  const user=userEvent.setup();
  const file=journey.writeFile("messages.mllp","\x0bMSH|^~\\&|READMIT|TEST|RECV|LAB|20260101120000||SIU^S12|synthetic-1|P|2.5.1\rPID|1||OWNED^^^READMIT^MR||EXAMPLE^ALICE\r\x1c\r\x0bMSH|^~\\&|READMIT|TEST|RECV|LAB|20260101120100||ADT^A01|synthetic-2|P|2.5.1\rPID|1||OWNED^^^READMIT^MR||EXAMPLE^BOB\r\x1c\r");
  const catalog=journey.writeFile("owned-reference.json",ownedCatalog());
  const before=journey.digest("messages.mllp");
  await journey.launch();
  await journey.chooseFiles([file],"Open HL7 file");
  await user.keyboard("{Control>}k{/Control}");
  await user.type(screen.getByLabelText("Search commands"),"Inspect file{Enter}");
  const list=await screen.findByRole("table",{name:"Messages in this file"});
  await press(user,list.querySelector<HTMLElement>('[data-row-id="0"]')!);
  const details=within(await screen.findByRole("region",{name:"Message details"}));
  expect(await details.findByText(/Select an offline reference catalog/)).toBeTruthy();
  await journey.chooseFiles([catalog],"Open offline HL7 reference catalog");
  await press(user,screen.getByRole("button",{name:"Reference"}));
  await journey.settled();
  await waitFor(()=>expect(journey.callsTo("ReadReferenceCatalog")[0]?.result).toMatchObject({state:"completed",reference:{coverage:{segments:1,fields:1}}}));
  await press(user,await details.findByRole("button",{name:/^MSH\[1\].*Owned message header/}));
  await press(user,await details.findByRole("button",{name:/^MSH\[1\]-9\s/}));
  await press(user,details.getByRole("button",{name:"Show values"}));
  expect(await details.findByRole("heading",{name:"SIU^S12 · synthetic-1"})).toBeTruthy();
  const original=within(details.getByRole("region",{name:"Original message"}));
  await waitFor(()=>expect(original.getByText("SIU^S12",{selector:"mark"})).toBeTruthy());
  const reference=within(details.getByRole("region",{name:"Reference details"}));
  expect(reference.getByText("00009")).toBeTruthy();
  expect(reference.getByText("MSG")).toBeTruthy();
  expect(details.getByRole("button",{name:/^MSH\[1\]-3\s/})).toBeTruthy();
  expect(details.getByRole("button",{name:/^MSH\[1\]-12\s/})).toBeTruthy();
  expect(reference.getByText("Owned reference definition for the message code and trigger.")).toBeTruthy();
  const grid=within(details.getByRole("region",{name:"Segment grid"}));
  for(const column of ["Path","Name","Type","Opt","Len","C-Len","Rep","Item#","Tbl","Sect","Value"]) expect(grid.getByText(column,{exact:true})).toBeTruthy();
  expect(grid.getByRole("button",{current:"location"}).getAttribute("aria-label")).toContain("MSH[1]-9");
  expect(within(grid.getByRole("button",{current:"location"})).getByTitle("MSH[1]-9").textContent).toBe("MSH-9");
  await selectReaderPath(user,"MSH[1]-9[1]");
  await press(user,await details.findByRole("button",{name:/^MSH\[1\]-9\[1\]\.1\s/}));
  expect(await reference.findByText(/component definitions are not available/)).toBeTruthy();
  expect(reference.queryByText("Owned reference definition for the message code and trigger.")).toBeNull();
  await press(user,list.querySelector<HTMLElement>('[data-row-id="1"]')!);
  expect(await details.findByRole("heading",{name:"ADT · A01"})).toBeTruthy();
  expect(reference.queryByText("Owned reference definition for the message code and trigger.")).toBeNull();
  expect(journey.digest("messages.mllp")).toBe(before);
  await journey.close();
  await journey.launch();
  expect(journey.digest("messages.mllp")).toBe(before);
});

function ownedCompositionCatalog():string {
  const base=JSON.parse(ownedCatalog());
  const specified=(value:string)=>({state:"specified",value});
  const absent={state:"not_specified",value:""};
  const na={state:"not_applicable",value:""};
  const row=(key:string,kind:string,container:string,position:number,name:string,datatype:string,section:string):Hl7referenceRecord=>({key,kind,container,position,segment:"",field:0,name,datatype:datatype ? specified(datatype) : na,optionality:absent,length:absent,conformance_length:absent,repetition:na,item:na,table:absent,section:specified(section),definition:`Owned datatype reference for ${name}.`,source:"owned-chapter.pdf"});
  const attending=row("field/PV1/7","field","",0,"Attending Doctor","XCN","3.4.3.7");
  attending.segment="PV1";attending.field=7;attending.item=specified("00137");attending.repetition=specified("Y");
  base.records.push(attending,
    row("datatype/MSG","datatype","MSG",0,"Message Type","","2.A.44"),
    row("datatype/XCN","datatype","XCN",0,"Extended Composite ID and Name","","2.A.86"),
    row("datatype/FN","datatype","FN",0,"Family Name","","2.A.30"),
    row("component/MSG/1","component","MSG",1,"Message Code","ID","2.A.44.1"),
    row("component/MSG/2","component","MSG",2,"Trigger Event","ID","2.A.44.2"),
    row("component/MSG/3","component","MSG",3,"Message Structure","ID","2.A.44.3"),
    row("component/XCN/1","component","XCN",1,"ID Number","ST","2.A.86.1"),
    row("component/XCN/2","component","XCN",2,"Family Name","FN","2.A.86.2"),
    row("component/FN/1","component","FN",1,"Surname","ST","2.A.30.1"),
    row("component/FN/2","component","FN",2,"Own Surname Prefix","ST","2.A.30.2"),
  );
  base.schema="readmit-hl7-reference/v2";
  base.coverage={segments:1,fields:2,datatypes:3,components:7,definitions:13,missing:[]};
  return JSON.stringify(base);
}

async function selectReaderPath(user:ReturnType<typeof userEvent.setup>,path:string) {
  const details=within(screen.getByRole("region",{name:"Message details"}));
  await press(user,details.getByRole("button",{name:"More message actions"}));
  await press(user,screen.getByRole("menuitem",{name:"Go to field…"}));
  const dialog=within(await screen.findByRole("dialog",{name:"Go to field"}));
  const input=dialog.getByLabelText("Field path");
  await user.click(input);
  await user.paste(path);
  expect((input as HTMLInputElement).value).toBe(path);
  await press(user,dialog.getByRole("button",{name:/^Go$/}));
  await waitFor(()=>expect(screen.queryByRole("dialog",{name:"Go to field"})).toBeNull());
  await journey.settled();
  await waitFor(()=>expect(journey.callsTo("InspectFileMessage").at(-1)?.result).toMatchObject({state:"completed",inspection:{selected:{path}}}));
}

test("real datatype drilldown and component inspection keep original repeated custom-delimiter evidence",async()=>{
  const user=userEvent.setup();
  const file=journey.writeFile("composition.mllp","\x0bMSH|^~\\&|READMIT|TEST|RECV|LAB|20260101120000||SIU^S12|synthetic-1|P|2.5.1\rPID|1||OWNED^^^READMIT^MR||EXAMPLE^ALICE\r\x1c\r\x0bMSH*%~\\&*READMIT*TEST*RECV*LAB*20260101120100**ADT%A01*synthetic-2*P*2.5.1\rPV1*******123%FIRST&FAMILY\rPV1*******456%SECOND&FAMILY~789%BETA&OWN\r\x1c\r");
  const catalog=journey.writeFile("owned-composition.json",ownedCompositionCatalog());
  const before=journey.digest("composition.mllp");
  await journey.launch();
  await journey.chooseFiles([file],"Open HL7 file");
  await user.keyboard("{Control>}k{/Control}");
  await user.type(screen.getByLabelText("Search commands"),"Inspect file{Enter}");
  const list=await screen.findByRole("table",{name:"Messages in this file"});
  await press(user,list.querySelector<HTMLElement>('[data-row-id="0"]')!);
  const details=within(await screen.findByRole("region",{name:"Message details"}));
  await journey.chooseFiles([catalog],"Open offline HL7 reference catalog");
  await press(user,screen.getByRole("button",{name:"Reference"}));
  await journey.settled();
  await waitFor(()=>expect(journey.callsTo("ReadReferenceCatalog")[0]?.result).toMatchObject({state:"completed"}));
  await selectReaderPath(user,"MSH[1]-9");
  await press(user,details.getByRole("button",{name:"Show values"}));
  const original=within(details.getByRole("region",{name:"Original message"}));
  const reference=within(details.getByRole("region",{name:"Reference details"}));
  await press(user,reference.getByRole("button",{name:"Datatype · MSG"}));
  expect(await reference.findByText("Selected field: MSH[1]-9")).toBeTruthy();
  expect(await reference.findByText("Original value: S12")).toBeTruthy();
  expect(reference.getByText("Original value: Omitted")).toBeTruthy();
  expect(original.getByText("SIU^S12",{selector:"mark"})).toBeTruthy();
  await press(user,await reference.findByRole("button",{name:"2 · Trigger Event"}));
  expect(await reference.findByText("Owned datatype reference for Trigger Event.")).toBeTruthy();
  expect(original.getByText("SIU^S12",{selector:"mark"})).toBeTruthy();
  await press(user,reference.getByRole("button",{name:"Back to reference"}));
  await reference.findByRole("button",{name:"2 · Trigger Event"});
  await press(user,reference.getByRole("button",{name:"Back to selected field"}));
  expect(reference.getByRole("button",{name:"Datatype · MSG"})).toBeTruthy();
  await selectReaderPath(user,"MSH[1]-9[1].2");
  expect(await reference.findByRole("heading",{name:"Trigger Event"})).toBeTruthy();
  expect(reference.getByText("2.A.44.2",{selector:"dd"})).toBeTruthy();
  expect(reference.getAllByText("Not applicable",{selector:"dd"}).length).toBeGreaterThanOrEqual(2);
  expect(original.getByText("S12",{selector:"mark"})).toBeTruthy();
  await press(user,list.querySelector<HTMLElement>('[data-row-id="1"]')!);
  await details.findByRole("heading",{name:"ADT · A01"});
  await selectReaderPath(user,"PV1[2]-7[2].2.1");
  expect(await reference.findByRole("heading",{name:"Surname"})).toBeTruthy();
  expect(original.getByText("BETA",{selector:"mark"})).toBeTruthy();
  expect(reference.getByText("2.A.30.1",{selector:"dd"})).toBeTruthy();
  expect(reference.getByText(/Parent Item#: 00137/)).toBeTruthy();
  await press(user,reference.getByRole("button",{name:"Parent datatype · FN"}));
  expect(await reference.findByRole("button",{name:"1 · Surname"})).toBeTruthy();
  expect(reference.getByText("Original value: BETA")).toBeTruthy();
  expect(original.getByText("BETA",{selector:"mark"})).toBeTruthy();
  await press(user,reference.getByRole("button",{name:"Back to selected field"}));
  expect(reference.getByRole("heading",{name:"Surname"})).toBeTruthy();
  expect(journey.digest("composition.mllp")).toBe(before);
  await journey.close();
  await journey.launch();
  expect(journey.digest("composition.mllp")).toBe(before);
});

function ownedTablesCatalog():string {
  const base=JSON.parse(ownedCompositionCatalog());
  const a=(value:string)=>({state:"specified",value});
  const na={state:"not_applicable",value:""};
  base.records.find((r:Hl7referenceRecord)=>r.key==="component/MSG/2").table=a("0003");
  base.records.find((r:Hl7referenceRecord)=>r.key==="component/MSG/3").table=a("0354");
  const entity=(key:string,kind:string,name:string,definition:string):Hl7referenceRecord=>({key,kind,segment:"",field:0,name,datatype:na,optionality:na,length:na,conformance_length:na,repetition:na,item:na,table:na,section:a("2.17.2"),definition,source:"owned-chapter.pdf",content_state:"available"});
  const table=entity("table/0003","table","Event type","Owned event table.");table.table_id="0003";table.table_kind="hl7";
  const element=entity("element/00009","element","Message Type","Owned full data element definition. <img src=x onerror=window.__readerInjected=true> This text remains literal. "+"Owned narrative paragraph. ".repeat(64)+"END_SOURCE_DEFINITION");element.item_id="00009";element.datatype=a("MSG");element.section=a("2.15.9.9");element.uses=["field/MSH/9"];
  const code=(value:string,meaning:string)=>{const record=entity(`code/0003/${btoa(value).replaceAll("=","")}`,"code",value,meaning);record.code=value;record.table_id="0003";return record;};
  base.records.push(table,element,code("S12","Owned new appointment notice."),code("S13","Owned appointment rescheduling notice."));
  for(let n=0;n<199;n++){const value=`X${String(n).padStart(3,"0")}`;base.records.push(code(value,`Owned lexical example ${value}.`));}
  base.schema="readmit-hl7-reference/v3";
  base.coverage={segments:1,fields:2,datatypes:3,components:7,tables:1,elements:1,codes:201,definitions:216,missing:[]};
  return JSON.stringify(base);
}

test("real offline table search, element and section drilldowns preserve context and render narrative as text",async()=>{
  const user=userEvent.setup();
  const file=journey.writeFile("references.mllp","\x0bMSH|^~\\&|READMIT|TEST|RECV|LAB|20260101120000||SIU^S13|synthetic-1|P|2.5.1\rPID|1||OWNED^^^READMIT^MR||EXAMPLE^ALICE\r\x1c\r\x0bMSH|^~\\&|READMIT|TEST|RECV|LAB|20260101120100||ADT^A01|synthetic-2|P|2.5.1\rPID|1||OWNED^^^READMIT^MR||EXAMPLE^BOB\r\x1c\r");
  const catalog=journey.writeFile("owned-tables.json",ownedTablesCatalog());
  const before=journey.digest("references.mllp");
  await journey.launch();
  await journey.chooseFiles([file],"Open HL7 file");
  await user.keyboard("{Control>}k{/Control}");
  await user.type(screen.getByLabelText("Search commands"),"Inspect file{Enter}");
  const list=await screen.findByRole("table",{name:"Messages in this file"});
  await press(user,list.querySelector<HTMLElement>('[data-row-id="0"]')!);
  const details=within(await screen.findByRole("region",{name:"Message details"}));
  await journey.chooseFiles([catalog],"Open offline HL7 reference catalog");
  await press(user,screen.getByRole("button",{name:"Reference"}));
  await journey.settled();
  await waitFor(()=>expect(journey.callsTo("ReadReferenceCatalog")[0]?.result).toMatchObject({state:"completed"}));
  await selectReaderPath(user,"MSH[1]-9[1].2");
  await press(user,details.getByRole("button",{name:"Show values"}));
  const original=within(details.getByRole("region",{name:"Original message"}));
  const reference=within(details.getByRole("region",{name:"Reference details"}));
  await press(user,reference.getByRole("button",{name:"Table · 0003"}));
  let codes=within(await reference.findByRole("table",{name:"Reference codes"}));
  expect(codes.getAllByRole("row")).toHaveLength(101);
  expect(original.getByText("S13",{selector:"mark"})).toBeTruthy();
  expect(details.getByRole("region",{name:"Original message"}).querySelector("pre")?.textContent).toContain("\nPID|");
  expect(details.getByRole("region",{name:"Original message"}).querySelector("pre")?.textContent).toContain("^~\\&");
  await press(user,reference.getByRole("button",{name:"Next codes"}));
  codes=within(await reference.findByRole("table",{name:"Reference codes"}));
  await codes.findByRole("button",{name:"X098"});
  await user.type(reference.getByLabelText("Search codes"),"S13");
  await press(user,reference.getByRole("button",{name:/^Search$/}));
  codes=within(await reference.findByRole("table",{name:"Reference codes"}));
  await waitFor(()=>expect(codes.getAllByRole("row")).toHaveLength(2));
  expect(codes.getByText("Owned appointment rescheduling notice.")).toBeTruthy();
  const lookup=journey.callsTo("LookupHL7Reference").at(-1);
  expect(lookup?.args[0]).toMatchObject({key:"table/0003",offset:0,limit:100,query:"S13"});
  await press(user,reference.getByRole("button",{name:"Back to selected field"}));
  await selectReaderPath(user,"MSH[1]-9");
  await press(user,reference.getByRole("button",{name:"Item# · 00009"}));
  expect(await reference.findByText("Item number: 00009")).toBeTruthy();
  expect(reference.getByText(/<img src=x onerror=window.__readerInjected=true>/)).toBeTruthy();
  const definition=reference.getByText(/END_SOURCE_DEFINITION/);
  expect(definition.textContent!.length).toBeGreaterThan(512);
  expect(definition.textContent!.endsWith("END_SOURCE_DEFINITION")).toBe(true);
  expect(document.querySelector('.reader-definition img')).toBeNull();
  expect((window as unknown as {__readerInjected?:boolean}).__readerInjected).toBeUndefined();
  expect(original.getByText("SIU^S13",{selector:"mark"})).toBeTruthy();
  await press(user,reference.getByRole("button",{name:"Back to selected field"}));
  await press(user,reference.getByRole("button",{name:"Sect · §2.15.9.9"}));
  expect(await reference.findByText("Owned reference definition for the message code and trigger.")).toBeTruthy();
  expect(reference.getByText("Selected field: MSH[1]-9")).toBeTruthy();
  await press(user,reference.getByRole("button",{name:"Back to selected field"}));
  await press(user,details.getByRole("button",{name:"Column guide"}));
  expect(reference.getByRole("region",{name:"Reference attributes"})).toBeTruthy();
  expect(reference.getByText(/Not specified or unavailable is not zero/)).toBeTruthy();
  await press(user,reference.getByRole("button",{name:"Back to reader"}));
  await selectReaderPath(user,"MSH[1]-9[1].3");
  await press(user,reference.getByRole("button",{name:"Table · 0354"}));
  expect(await reference.findByText(/selected reference entity is not available/)).toBeTruthy();
  expect(reference.queryByText("Owned appointment rescheduling notice.")).toBeNull();
  expect(journey.digest("references.mllp")).toBe(before);
});

function ownedContextCatalog():string {
  const base=JSON.parse(ownedTablesCatalog());
  const na={state:"not_applicable",value:""};
  const a=(value:string)=>({state:"specified",value});
  const record=(key:string,kind:string,name:string,section:string)=>({key,kind,name,segment:"",field:0,datatype:na,optionality:na,length:na,conformance_length:na,repetition:na,item:na,table:na,section:a(section),definition:`Owned overview for ${name}.`,source:"owned-messages.pdf",content_state:"available"});
  base.records.push(
    {...record("message/SIU/S13","message","Appointment rescheduling","10.4.2"),message_code:"SIU",event:"S13",structures:["SIU_S12"]},
    {...record("message/ADT/A08","message","Update patient information","3.3.8"),message_code:"ADT",event:"A08",structures:["ADT_A01"]},
    {...record("structure/SIU_S12","structure","SIU_S12","10.4.1"),structure_id:"SIU_S12",sequence:[{name:"header",segment:"MSH",min:1,max:"1"},{name:"appointment",segment:"SCH",min:1,max:"1"}]},
    {...record("structure/ADT_A01","structure","ADT_A01","3.3.1"),structure_id:"ADT_A01",sequence:[{name:"header",segment:"MSH",min:1,max:"1"},{name:"PATIENT",min:1,max:"*",children:[{name:"patient",segment:"PID",min:1,max:"1"},{name:"visit",segment:"PV1",min:0,max:"1"}]}]},
  );
  base.schema="readmit-hl7-reference/v4";
  base.coverage={...base.coverage,messages:2,structures:2,definitions:base.coverage.definitions+4};
  return JSON.stringify(base);
}

test("real message context keeps source structure omitted and selected profile documentation pinned across reopen",async()=>{
  const user=userEvent.setup();
  const file=journey.writeFile("contexts.mllp","\x0bMSH|^~\\&|READMIT|TEST|RECV|LAB|20260101120000||SIU^S13|synthetic-1|P|2.5.1\rSCH|OWNED\r\x1c\r\x0bMSH|^~\\&|READMIT|TEST|RECV|LAB|20260101120100||ADT^A08|synthetic-2|P|2.5.1\rPID|1||OWNED^^^READMIT^MR||EXAMPLE^ALICE\rPV1|||||||123^FIRST\rPID|2||OWNED-2^^^READMIT^MR||EXAMPLE^BOB\rPV1|||||||456^SECOND\r\x1c\r");
  const catalog=journey.writeFile("owned-context.json",ownedContextCatalog());
  const profile=journey.writeFile("owned-profile.json",JSON.stringify({schema:"readmit-local-profile/v1",profile:{id:"owned-adt",version:"1"},base:{pack:{id:"owned-pack",version:"1"},hl7_version:"2.5.1",family:"ADT"},segments:[{id:"PV1",description:"Owned local requirement",fields:[{position:7,usage:"C",condition:{segment:"PV1",position:2,operator:"present"},type:"XCN",cardinality:{min:0,max:"2"}}]}]}));
  const doc=journey.writeFile("local-reference.md","Owned partner note. <script>window.__profileInjected=true</script>");
  const before=journey.digest("contexts.mllp");
  await journey.launch();
  await journey.chooseFiles([file],"Open HL7 file");
  await user.keyboard("{Control>}k{/Control}");
  await user.type(screen.getByLabelText("Search commands"),"Inspect file{Enter}");
  const list=await screen.findByRole("table",{name:"Messages in this file"});
  await press(user,list.querySelector<HTMLElement>('[data-row-id="0"]')!);
  let details=within(await screen.findByRole("region",{name:"Message details"}));
  await journey.chooseFiles([catalog],"Open offline HL7 reference catalog");
  await press(user,screen.getByRole("button",{name:"Reference"}));
  await journey.settled();
  await waitFor(()=>expect(journey.callsTo("ReadReferenceCatalog")[0]?.result).toMatchObject({state:"completed"}));
  let context=within(details.getByRole("region",{name:"Message context"}));
  expect(context.getByText("Structure: SIU_S12")).toBeTruthy();
  expect(context.getByText("Resolved from message code + trigger event")).toBeTruthy();
  const provenance=()=>within(details.getByText("Message structure provenance",{selector:"summary"}).closest("details")!);
  await press(user,details.getByText("Message structure provenance",{selector:"summary"}));
  expect(provenance().getByText("Transmitted MSH-9.3: Omitted")).toBeTruthy();
  expect(provenance().getByText("Reference structure: SIU_S12 · inferred")).toBeTruthy();
  await press(user,provenance().getByRole("button",{name:"Message overview"}));
  expect(await details.findByText("Owned overview for Appointment rescheduling.")).toBeTruthy();
  await press(user,details.getByRole("button",{name:"Back to selected field"}));
  await press(user,list.querySelector<HTMLElement>('[data-row-id="1"]')!);
  await details.findByRole("heading",{name:"ADT · A08"});
  context=within(details.getByRole("region",{name:"Message context"}));
  expect(context.getByText("Structure: ADT_A01")).toBeTruthy();
  expect(provenance().getByText("Transmitted MSH-9.3: Omitted")).toBeTruthy();
  expect(provenance().getByText("Reference structure: ADT_A01 · inferred")).toBeTruthy();
  await selectReaderPath(user,"PV1[2]-7");
  await press(user,details.getByText("Selected occurrence placement",{selector:"summary"}));
  expect(details.getByText(/PATIENT\[2\].*visit\[1\]/)).toBeTruthy();
  await journey.chooseFiles([profile],"Open local HL7 profile");
  await press(user,details.getByRole("button",{name:"More message actions"}));
  await press(user,screen.getByRole("menuitem",{name:"Choose local profile…"}));
  const overlay=within(await details.findByRole("region",{name:"Selected profile context"}));
  expect(await overlay.findByText(/Profile: owned-adt/)).toBeTruthy();
  expect(overlay.getByText(/Usage C · Datatype XCN/)).toBeTruthy();
  expect(overlay.getByText(/Conditional predicate: PV1-2 · present/)).toBeTruthy();
  expect(overlay.getByText(/Local field cardinality: 0..2/)).toBeTruthy();
  await journey.chooseFiles([doc],"Open local reference documentation");
  await press(user,details.getByRole("button",{name:"More message actions"}));
  await press(user,screen.getByRole("menuitem",{name:"Choose local documentation…"}));
  await press(user,await overlay.findByText("local-reference.md",{selector:"summary"}));
  expect(await overlay.findByText(/Owned partner note/)).toBeTruthy();
  expect((window as unknown as {__profileInjected?:boolean}).__profileInjected).toBeUndefined();
  await journey.settled();
  await journey.close();
  await journey.launch();
  details=within(await screen.findByRole("region",{name:"Message details"}));
  expect(await details.findByText(/Profile: owned-adt/)).toBeTruthy();
  expect(details.queryByText("456^SECOND")).toBeNull();
  await journey.close();
  journey.writeFile("owned-profile.json",journey.readFile("owned-profile.json")+"\n");
  await journey.launch();
  details=within(await screen.findByRole("region",{name:"Message details"}));
  expect(await details.findByText(/selected reference changed; select its new identity explicitly/)).toBeTruthy();
  expect(details.queryByText(/Usage C · Datatype XCN/)).toBeNull();
  expect(journey.digest("contexts.mllp")).toBe(before);
});

test("real reader selects each earlier local edition while retaining the declared source and exact paths",async()=>{
 const user=userEvent.setup();
 const file=journey.writeFile("edition-source.mllp","\x0bMSH|^~\\&|READMIT|TEST|RECV|LAB|20260101120000||SIU^S13|synthetic-1|P|2.5.1\rSCH|OWNED\r\x1c\r");
 const before=journey.digest("edition-source.mllp");
 const paths=["2.3.1","2.4","2.5"].map((edition)=>{
  const d=JSON.parse(ownedContextCatalog());d.edition=edition;
  const field=d.records.find((r:Hl7referenceRecord)=>r.key==="field/MSH/9");field.name="Owned edition "+edition+" message type";field.length={state:"specified",value:edition==="2.3.1"?"7":edition==="2.4"?"13":"15"};
  return journey.writeFile("edition-"+edition+".json",JSON.stringify(d));
 });
 await journey.launch();await journey.chooseFiles([file],"Open HL7 file");
 await user.keyboard("{Control>}k{/Control}");await user.type(screen.getByLabelText("Search commands"),"Inspect file{Enter}");
 const list=await screen.findByRole("table",{name:"Messages in this file"});await press(user,list.querySelector<HTMLElement>('[data-row-id="0"]')!);
 const details=within(await screen.findByRole("region",{name:"Message details"}));
 for(let i=0;i<paths.length;i++) {
  await journey.chooseFiles([paths[i]!],"Open offline HL7 reference catalog");await press(user,screen.getByRole("button",{name:"Reference"}));
  await journey.settled();
  await waitFor(()=>expect(journey.callsTo("ReadReferenceCatalog")).toHaveLength(i+1));
  await selectReaderPath(user,"MSH[1]-9");
  const reference=within(details.getByRole("region",{name:"Reference details"}));
  expect(await reference.findByRole("heading",{name:"Owned edition "+["2.3.1","2.4","2.5"][i]+" message type"})).toBeTruthy();
  expect(reference.getByText("Message edition: 2.5.1")).toBeTruthy();
  expect(reference.getByText("Reference edition: "+["2.3.1","2.4","2.5"][i])).toBeTruthy();
  expect(reference.getByText(/Selected reference edition differs/)).toBeTruthy();
  expect(reference.getByText(i===0?"7":i===1?"13":"15",{exact:true})).toBeTruthy();
  await press(user,reference.getByRole("button",{name:"Datatype · MSG"}));
  expect(await details.findByRole("heading",{name:"Message Type"})).toBeTruthy();
  await press(user,details.getByRole("button",{name:"Back to selected field"}));
  expect(details.getByRole("button",{current:"location"}).getAttribute("aria-label")).toContain("MSH[1]-9");
  expect(within(details.getByRole("button",{current:"location"})).getByTitle("MSH[1]-9").textContent).toBe("MSH-9");
 }
 expect(journey.digest("edition-source.mllp")).toBe(before);
});

test.skipIf(!process.env.READMIT_HL7_REFERENCE_DIRECTORY)("supplied official earlier edition catalogs browse through the real reader without changing its declaration",async()=>{
 const dir=process.env.READMIT_HL7_REFERENCE_DIRECTORY;
 if(!dir)return;
 const user=userEvent.setup();
 const file=journey.writeFile("official-edition-source.mllp","\x0bMSH|^~\\&|READMIT|TEST|RECV|LAB|20260101120000||ADT^A08|synthetic-1|P|2.5.1\rPV1|||||||123^OWNED\r\x1c\r");
 const before=journey.digest("official-edition-source.mllp");
 await journey.launch();await journey.chooseFiles([file],"Open HL7 file");await user.keyboard("{Control>}k{/Control}");await user.type(screen.getByLabelText("Search commands"),"Inspect file{Enter}");
 const list=await screen.findByRole("table",{name:"Messages in this file"});await press(user,list.querySelector<HTMLElement>('[data-row-id="0"]')!);
 const details=within(await screen.findByRole("region",{name:"Message details"}));
 for(const [edition,slug,length] of [["2.3.1","v231","7"],["2.4","v24","13"],["2.5","v25","15"]]) {
  await journey.chooseFiles([dir+"/hl7-"+slug+"-qualification.json"],"Open offline HL7 reference catalog");await press(user,screen.getByRole("button",{name:"Reference"}));
  await journey.settled();
  await selectReaderPath(user,"MSH[1]-9");
  const reference=within(details.getByRole("region",{name:"Reference details"}));
  await waitFor(()=>expect(reference.getByText("Reference edition: "+edition)).toBeTruthy());
  expect(reference.getByText("Message edition: 2.5.1")).toBeTruthy();expect(reference.getByText(length!,{exact:true})).toBeTruthy();expect(reference.getByText("00009",{exact:true})).toBeTruthy();
  await press(user,reference.getByRole("button",{name:"Item# · 00009"}));
  expect(await reference.findByRole("heading",{name:/^00009 · /})).toBeTruthy();
  await press(user,reference.getByRole("button",{name:"Back to selected field"}));
  const section=reference.getByRole("button",{name:/^Sect · /});await press(user,section);
  expect(await reference.findByRole("heading",{name:"Source definition"})).toBeTruthy();
  await press(user,reference.getByRole("button",{name:"Back to selected field"}));
  const table=reference.queryByRole("button",{name:"Table · 0003"});
  if(table){await press(user,table);expect(await reference.findByRole("heading",{name:"Table reference"})).toBeTruthy();await press(user,reference.getByRole("button",{name:"Back to selected field"}));}
  await press(user,reference.getByRole("button",{name:"Message overview"}));
  expect(await reference.findByRole("heading",{name:"Source definition"})).toBeTruthy();
  await press(user,reference.getByRole("button",{name:"Back to selected field"}));
  await selectReaderPath(user,"PV1[1]-7[1].2");
  expect(reference.getByText("Reference edition: "+edition)).toBeTruthy();
  await press(user,reference.getByRole("button",{name:"Parent datatype · XCN"}));
  expect(await reference.findByRole("heading",{name:/^XCN · /})).toBeTruthy();
  await press(user,reference.getByRole("button",{name:"Back to selected field"}));
  expect(details.getByRole("button",{current:"location"}).textContent).toContain("PV1[1]-7[1].2");
 }
 expect(journey.digest("official-edition-source.mllp")).toBe(before);
});

test.skipIf(!process.env.READMIT_HL7_LATER_REFERENCE_DIRECTORY)("supplied later reference editions preserve exact Len and C-Len through the real reader and return",async()=>{
 const dir=process.env.READMIT_HL7_LATER_REFERENCE_DIRECTORY!;
 const user=userEvent.setup();
 const file=journey.writeFile("later-edition-source.mllp","\x0bMSH|^~\\&|READMIT|TEST|RECV|LAB|20260101120000||SIU^S13|synthetic-1|P|2.5.1\rSCH|OWNED\r\x1c\r");
 const baseline=journey.digest("later-edition-source.mllp");
 const returned=journey.writeFile("return-reference.json",ownedCatalog());
 await journey.launch();await journey.chooseFiles([file],"Open HL7 file");await user.keyboard("{Control>}k{/Control}");await user.type(screen.getByLabelText("Search commands"),"Inspect file{Enter}");
 const list=await screen.findByRole("table",{name:"Messages in this file"});await press(user,list.querySelector<HTMLElement>('[data-row-id="0"]')!);
 const details=within(await screen.findByRole("region",{name:"Message details"}));
 for(const [edition,slug] of [["2.6","v26"],["2.7.1","v271"],["2.8.2","v282"]]) {
  await journey.chooseFiles([dir+"/hl7-"+slug+"-qualification.json"],"Open offline HL7 reference catalog");await press(user,screen.getByRole("button",{name:"Reference"}));await journey.settled();
  for(const [field,expected] of [[2,edition==="2.6"?"4":"4..5"],[7,"DTM"],[8,edition==="2.6"?"40":"40="],[10,edition==="2.6"?"199":"1..199"]] as const) {
   await selectReaderPath(user,"MSH[1]-"+field);
   const reference=within(details.getByRole("region",{name:"Reference details"}));
   expect(reference.getByText("Message edition: 2.5.1")).toBeTruthy();expect(reference.getByText("Reference edition: "+edition)).toBeTruthy();expect(reference.getByText(expected,{exact:true})).toBeTruthy();
   if(field===10&&edition!=="2.6") {expect(reference.getByText("=",{exact:true})).toBeTruthy();expect(reference.getByText(/receiving application’s storage capacity/)).toBeTruthy();}
  }
  await journey.chooseFiles([returned],"Open offline HL7 reference catalog");await press(user,details.getByRole("button",{name:"Return to message edition"}));await journey.settled();
  expect(details.getByText("Reference edition: 2.5.1")).toBeTruthy();expect(details.queryByRole("button",{name:"Return to message edition"})).toBeNull();
 }
 expect(journey.digest("later-edition-source.mllp")).toBe(baseline);
});

test("real catalog identity is held across changed-file reads and close/reopen without repinning",async()=>{
 const user=userEvent.setup();
 const file=journey.writeFile("catalog-pin-source.mllp","\x0bMSH|^~\\&|READMIT|TEST|RECV|LAB|20260101120000||SIU^S13|synthetic-1|P|2.5.1\rSCH|OWNED\r\x1c\r");
 const catalog=journey.writeFile("catalog-pin.json",ownedCatalog());const original=journey.digest("catalog-pin-source.mllp");
 await journey.launch();await journey.chooseFiles([file],"Open HL7 file");await user.keyboard("{Control>}k{/Control}");await user.type(screen.getByLabelText("Search commands"),"Inspect file{Enter}");
 const list=await screen.findByRole("table",{name:"Messages in this file"});await press(user,list.querySelector<HTMLElement>('[data-row-id="0"]')!);
 let details=within(await screen.findByRole("region",{name:"Message details"}));
 await journey.chooseFiles([catalog],"Open offline HL7 reference catalog");await press(user,screen.getByRole("button",{name:"Reference"}));await journey.settled();await selectReaderPath(user,"MSH[1]-9");
 expect(details.getByText("Owned reference definition for the message code and trigger.")).toBeTruthy();
 const chosenIdentity="sha256:"+journey.digest("catalog-pin.json");
 expect(journey.callsTo("InspectFileMessage").at(-1)?.args[0]).toMatchObject({reference_identity:chosenIdentity});
 journey.changeFile("catalog-pin.json",ownedCatalog()+"\n");await selectReaderPath(user,"MSH[1]-9");
 expect(details.getByText(/selected reference catalog changed/)).toBeTruthy();expect(details.queryByText("Owned reference definition for the message code and trigger.")).toBeNull();
 await journey.settled();await journey.close();await journey.launch();details=within(await screen.findByRole("region",{name:"Message details"}));
 expect(await details.findByText(/selected reference catalog changed/)).toBeTruthy();
 expect(journey.callsTo("InspectFileMessage").at(-1)?.args[0]).toMatchObject({reference_identity:chosenIdentity});expect(journey.digest("catalog-pin-source.mllp")).toBe(original);
});

test("real reader Next parts pages later repetitions components and subcomponents with exact source spans",async()=>{
 const user=userEvent.setup();const parts=Array.from({length:101},(_,index)=>"OWNED_"+String(index+1).padStart(3,"0"));const records=[{delimiter:"~",parent:"ZAA[1]-1",later:"ZAA[1]-1[101]"},{delimiter:"^",parent:"ZAA[1]-1[1]",later:"ZAA[1]-1[1].101"},{delimiter:"&",parent:"ZAA[1]-1[1].1",later:"ZAA[1]-1[1].1.101"}];
 const file=journey.writeFile("paged-reader.mllp",records.map(record=>"\x0bMSH|^~\\&|A|B|C|D|20260101||SIU^S12|owned-page|P|2.5.1\rZAA|"+parts.join(record.delimiter)+"|SIBLING\r\x1c\r").join(""));const before=journey.digest("paged-reader.mllp");
 await journey.launch();await journey.chooseFiles([file],"Open HL7 file");await user.keyboard("{Control>}k{/Control}");await user.type(screen.getByLabelText("Search commands"),"Inspect file{Enter}");const list=await screen.findByRole("table",{name:"Messages in this file"});
 for(let index=0;index<records.length;index++){
  const record=records[index]!;await press(user,list.querySelector<HTMLElement>('[data-row-id="'+index+'"]')!);await selectReaderPath(user,record.parent);const detail=within(screen.getByRole("region",{name:"Message details"}));await press(user,detail.getByRole("button",{name:"Next parts"}));await journey.settled();expect(journey.callsTo("InspectFileMessage").at(-1)?.args[0]).toMatchObject({path:record.parent,node_offset:100});
  const grid=within(detail.getByRole("region",{name:"Segment grid"}));const escaped=record.later.replace(/[.*+?^${}()|[\]\\]/g,"\\$&");await press(user,await grid.findByRole("button",{name:new RegExp("^"+escaped+"\\s")}));await journey.settled();if(detail.queryByRole("button",{name:"Show values"}))await press(user,detail.getByRole("button",{name:"Show values"}));await journey.settled();expect(journey.callsTo("InspectFileMessage").at(-1)?.result).toMatchObject({state:"completed",inspection:{selected:{path:record.later},raw_window:{selected:"OWNED_101"}}});expect(grid.getByRole("button",{current:"location"}).textContent).toContain(record.later);expect(grid.getByRole("button",{name:/^ZAA\[1\]-2\s/})).toBeTruthy();
 }
 expect(journey.digest("paged-reader.mllp")).toBe(before);
});

test("real reader exposes independent v5 attribute origins and unknown-edition profile applicability",async()=>{
 const user=userEvent.setup();const original="\x0bMSH|^~\\&|A|B|C|D|20260101||SIU^S13|owned-provenance|P|\rSCH|OWNED\r\x1c\r";const file=journey.writeFile("origin-reader.mllp",original);const before=journey.digest("origin-reader.mllp");
 const data=JSON.parse(ownedCatalog());data.schema="readmit-hl7-reference/v5";data.sources.push({role:"schemas",file:"owned-fields.xsd",sha256:"b".repeat(64),publisher:"Owned schema fixture"});
 for(const record of data.records){record.name_origin={kind:"normative",source:"standard",locator:"owned-chapter.pdf"};record.definition_origin=record.definition?{kind:"normative",source:"standard",locator:"owned-chapter.pdf"}:{kind:"not_available"};for(const key of ["datatype","optionality","length","conformance_length","repetition","item","table","section"]){const a=record[key];a.origin=["not_applicable","not_available"].includes(a.state)?{kind:a.state}:{kind:"normative",source:"standard",locator:"owned-chapter.pdf#"+key};}}
 const field=data.records.find((record:Hl7referenceRecord)=>record.key==="field/MSH/9");field.datatype.origin={kind:"schema",source:"schemas",locator:"fields.xsd#MSH.9.Type"};field.item.origin={kind:"schema",source:"schemas",locator:"fields.xsd#MSH.9.Item"};field.name_origin={kind:"schema",source:"schemas",locator:"fields.xsd#MSH.9.LongName"};field.definition="";field.definition_origin={kind:"not_available"};data.coverage.definitions--;
 const catalog=journey.writeFile("owned-origin-reference.json",JSON.stringify(data));const profile=(family:string)=>journey.writeFile("owned-profile-"+family+".json",JSON.stringify({schema:"readmit-local-profile/v1",profile:{id:"owned-"+family.toLowerCase(),version:"1"},base:{pack:{id:"owned-pack",version:"1"},hl7_version:"2.5.1",family},segments:[{id:"MSH",fields:[{position:9,usage:"R"}]}]}));const adt=profile("ADT"),siu=profile("SIU");
 await journey.launch();await journey.chooseFiles([file],"Open HL7 file");await user.keyboard("{Control>}k{/Control}");await user.type(screen.getByLabelText("Search commands"),"Inspect file{Enter}");await press(user,(await screen.findByRole("table",{name:"Messages in this file"})).querySelector<HTMLElement>('[data-row-id="0"]')!);const details=within(await screen.findByRole("region",{name:"Message details"}));await journey.chooseFiles([catalog],"Open offline HL7 reference catalog");await press(user,screen.getByRole("button",{name:"Reference"}));await journey.settled();await selectReaderPath(user,"MSH[1]-9");const reference=within(details.getByRole("region",{name:"Reference details"}));await press(user,reference.getByText("Attribute origins",{selector:"summary"}));expect(reference.getByText("Messaging schema · owned-fields.xsd · fields.xsd#MSH.9.Type")).toBeTruthy();expect(reference.getByText("Messaging schema · owned-fields.xsd · fields.xsd#MSH.9.Item")).toBeTruthy();expect(reference.getByText(/Normative chapter.*owned-chapter.pdf#length/)).toBeTruthy();expect(reference.getByText("Definition not available.")).toBeTruthy();
 for(const [path,status,applicability]of [[adt,"not_available","incompatible"],[siu,"profile_selected","unknown_edition"]]){await journey.chooseFiles([path!],"Open local HL7 profile");await press(user,details.getByRole("button",{name:"More message actions"}));await press(user,screen.getByRole("menuitem",{name:"Choose local profile…"}));await journey.settled();expect(journey.callsTo("ReadHL7ReferenceSelection").at(-1)?.result).toMatchObject({state:"completed"});expect(journey.callsTo("InspectFileMessage").at(-1)?.result).toMatchObject({state:"completed",inspection:{metadata:{hl7_version:""},reference_overlay:{status,applicability}}});expect(details.queryByText(/Usage R · Datatype/)).toBeNull();}
 expect(journey.digest("origin-reader.mllp")).toBe(before);
});
