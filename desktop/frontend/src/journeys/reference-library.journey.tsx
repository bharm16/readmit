import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Journey, press } from "../testkit/journey";
import { sidebar } from "../testkit/navigation";

let journey:Journey;
beforeEach(()=>{journey=Journey.create();});
afterEach(async()=>{await journey.dispose();});

test("installing every reference edition makes exact message selection automatic and persists across restart",async()=>{
  const user=userEvent.setup();
  const editions=["2.1","2.2","2.3","2.3.1","2.4","2.5","2.5.1","2.6","2.7","2.7.1","2.8","2.8.1","2.8.2","2.9"];
  const absent={state:"not_specified",value:""};
  const files=editions.map(edition=>{
    const name="hl7-"+edition+".json";
    journey.writeFile("references/"+name,JSON.stringify({schema:"readmit-hl7-reference/v1",edition,sources:[{role:"standard",file:"owned-reference.txt",sha256:"a".repeat(64),publisher:"Owned reference fixture"}],coverage:{segments:0,fields:1,definitions:1,missing:[]},records:[{key:"field/ZAA/1",kind:"field",segment:"ZAA",field:1,name:"Owned field",datatype:{state:"specified",value:"ST"},optionality:absent,length:absent,conformance_length:absent,repetition:absent,item:absent,table:absent,section:absent,definition:"Owned edition "+edition+" definition.",source:"owned-reference.txt"}]}));
    return name;
  });
  journey.writeFile("references/library.json",JSON.stringify({schema:"readmit-hl7-reference-library/v1",catalogs:files}));
  const source=journey.writeFile("source.mllp","\x0bMSH|^~\\&|OWNED|TEST|RECV|TEST|20260101||ADT^A01|owned-1|P|2.1\rZAA|OWNED\r\x1c\r\x0bMSH|^~\\&|OWNED|TEST|RECV|TEST|20260101||ADT^A01|owned-2|P|2.9\rZAA|OWNED\r\x1c\r");
  const identity=journey.digest("source.mllp");
  await journey.launch();await press(user,sidebar().getByRole("button",{name:"Messages"}));
  await journey.chooseFiles([source],"Open HL7 file");await press(user,screen.getByRole("button",{name:"Open file"}));
  let list=await screen.findByRole("table",{name:"Messages in this file"});
  await press(user,list.querySelector<HTMLElement>('[data-row-id="0"]')!);
  await press(user,screen.getByRole("button",{name:"HL7 reference version"}));await press(user,screen.getByRole("menuitem",{name:"Manage reference library…"}));
  const library=within(await screen.findByRole("dialog",{name:"HL7 reference library"}));
  await library.findByRole("option",{name:"HL7 2.9 · Not installed"});
  await journey.chooseFolder(journey.path("references"),"Install HL7 reference library");await press(user,library.getByRole("button",{name:"Install library…"}));
  await library.findByRole("option",{name:"HL7 2.9"});
  await press(user,library.getByRole("button",{name:"Use reference"}));
  const select=async()=>{
    const details=within(screen.getByRole("region",{name:"Message details"}));
    await press(user,details.getByRole("button",{name:"More message actions"}));await press(user,screen.getByRole("menuitem",{name:"Go to field…"}));
    const go=within(await screen.findByRole("dialog",{name:"Go to field"}));await user.type(go.getByLabelText("Field path"),"ZAA-1");await press(user,go.getByRole("button",{name:"Go"}));
  };
  await select();await screen.findByText("Owned edition 2.1 definition.");
  expect(screen.getByRole("button",{name:"Enable PHI masking"})).toBeTruthy();
  await press(user,list.querySelector<HTMLElement>('[data-row-id="1"]')!);await select();await screen.findByText("Owned edition 2.9 definition.");
  await journey.settled();
  expect(journey.callsTo("InspectFileMessage").at(-1)?.args[0]).not.toHaveProperty("reference_catalog");
  await journey.close();await journey.launch();
  await screen.findByText("Owned edition 2.9 definition.");
  expect(screen.getByRole("button",{name:"Enable PHI masking"})).toBeTruthy();
  expect(journey.digest("source.mllp")).toBe(identity);
  await waitFor(()=>expect(journey.callsTo("InstallReferenceLibrary")).toHaveLength(1));
});
