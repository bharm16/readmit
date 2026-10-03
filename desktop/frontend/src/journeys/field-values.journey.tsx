import { afterEach,beforeEach,expect,test } from "vitest";
import { screen,within,waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { enter,Journey,press } from "../testkit/journey";
import { details,page,sidebar } from "../testkit/navigation";
import { licensedProject } from "./steps";
let journey:Journey;
beforeEach(()=>{journey=Journey.create();});afterEach(async()=>{await journey.dispose();});

test("complete large-capture field counts open exact occurrences and return without losing multi-selection",async()=>{
 const user=userEvent.setup();const messages:string[]=[];
 for(let i=0;i<600;i++) {
  let tail="ZPD|OWNED_A\r";
  if(i>=580&&i<590)tail="ZPD|\r";else if(i>=590&&i<595)tail='ZPD|""\r';else if(i>=595&&i<598)tail="";else if(i>=598)tail="ZPD|\\Q\\\r";else if(i>=300)tail="ZPD|OWNED_B\r";
  messages.push("\x0bMSH|^~\\&|READMIT|TEST|RECV|LAB|20260101||ADT^A08|owned-"+i+"|P|2.5.1\r"+tail+"\x1c\r");
 }
 const file=journey.writeFile("exports/counts.mllp",messages.join(""));const before=journey.digest("exports/counts.mllp");
 await licensedProject(journey,user);await press(user,sidebar().getByRole("button",{name:"Messages"}));
 await journey.chooseFiles([file],"Open HL7 file");await press(user,page().getByRole("button",{name:"Open file"}));
 const loose=await screen.findByRole("table",{name:"Messages in this file"});expect(loose.querySelector('[data-row-id="0"]')).toBeTruthy();await press(user,loose.querySelector<HTMLElement>('[data-row-id="0"]')!);
 await press(user,page().getByRole("button",{name:"Retain capture"}));
 const flow=within(await screen.findByRole("dialog",{name:"Import"}));await enter(user,flow.getByLabelText("Case"),"Large count capture");await journey.settled();await press(user,flow.getByRole("button",{name:"Next"}));await flow.findByRole("table",{name:"Preview"});await press(user,flow.getByRole("button",{name:/^Import$/}));await waitFor(()=>expect(screen.queryByRole("dialog",{name:"Import"})).toBeNull());
 await page().findByRole("heading",{name:"Large count capture"});
 const table=await screen.findByRole("table",{name:"Messages"});await waitFor(()=>expect(table.querySelector('[data-row-id="s0001-e000001"]')).toBeTruthy());await press(user,table.querySelector<HTMLElement>('[data-row-id="s0001-e000001"]')!);
 for(const id of ["s0001-e000001","s0001-e000002"])await user.click(table.querySelector<HTMLInputElement>('[data-row-id="'+id+'"] input[type="checkbox"]')!);
 await press(user,details().getByRole("button",{name:"More message actions"}));await press(user,screen.getByRole("menuitem",{name:"Go to field…"}));
 const go=within(await screen.findByRole("dialog",{name:"Go to field"}));await enter(user,go.getByLabelText("Field path"),"ZPD-1");await press(user,go.getByRole("button",{name:"Go"}));await journey.settled();
 await press(user,details().getByRole("button",{name:"More message actions"}));await press(user,screen.getByRole("menuitem",{name:"Field values"}));
 const aggregate=within(await screen.findByRole("region",{name:"Field values"}));
 expect(await aggregate.findByText("600 matched of 600 capture occurrences · 600 examined · Complete scope scan")).toBeTruthy();
 expect(aggregate.queryByText("OWNED_A",{exact:true})).toBeNull();expect(aggregate.queryByText("OWNED_B",{exact:true})).toBeNull();
 const hidden=aggregate.getByRole("table",{name:"Field value counts"});const hiddenRow=within(hidden).getByText("Hidden present values").closest("tr")!;expect(within(hiddenRow).getByText("580")).toBeTruthy();
 const countResult=journey.callsTo("ReadFieldValues").at(-1)?.result;expect(countResult).toMatchObject({scanned:600,scan_complete:true,complete:false,counts:{present:580,empty:10,null:5,omitted:3,undecodable:2,undecided:0}});
 await press(user,aggregate.getByRole("button",{name:"Show values for this scope"}));await journey.settled();
 const revealed=aggregate.getByRole("table",{name:"Field value counts"});const ownedRow=within(revealed).getByText("OWNED_B",{exact:true}).closest("tr")!;expect(within(ownedRow).getByText("280")).toBeTruthy();await press(user,within(ownedRow).getByRole("button",{name:"Show messages"}));await journey.settled();
 const counted=aggregate.getByRole("table",{name:"Counted messages"});expect(aggregate.getByText("280 exact counted occurrences")).toBeTruthy();expect(counted.querySelector('[data-row-id="s0001-e000301"]')).toBeTruthy();expect(counted.querySelector('[data-row-id="s0001-e000001"]')).toBeNull();
 expect(counted.querySelector('[data-row-id="s0001-e000301"]')).toBeTruthy();await press(user,counted.querySelector<HTMLElement>('[data-row-id="s0001-e000301"]')!);await journey.settled();expect(journey.callsTo("InspectOccurrence").at(-1)?.args[0]).toMatchObject({occurrence:"s0001-e000301",path:"ZPD[1]-1[1]",reveal:false});
 await press(user,details().getByRole("button",{name:"Close message details"}));
 expect(aggregate.getByRole("table",{name:"Counted messages"})).toBeTruthy();await press(user,aggregate.getByRole("button",{name:"Back to aggregate"}));await press(user,aggregate.getByRole("button",{name:"Back to reader"}));await journey.settled();
 expect(journey.callsTo("InspectOccurrence").at(-1)?.args[0]).toMatchObject({occurrence:"s0001-e000001",path:"ZPD[1]-1",reveal:false});
 const restored=screen.getByRole("table",{name:"Messages"});for(const id of ["s0001-e000001","s0001-e000002"])expect(restored.querySelector<HTMLInputElement>('[data-row-id="'+id+'"] input[type="checkbox"]')?.checked).toBe(true);
 expect(journey.digest("exports/counts.mllp")).toBe(before);
});
