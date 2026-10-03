import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { enter, Journey, press } from "../testkit/journey";
import { page } from "../testkit/navigation";
import { EXPORTED_BOOKING, EXPORTED_RESCHEDULE, importExport, licensedProject } from "./steps";
import { filesUnder } from "./probes.js";

let journey:Journey;
beforeEach(()=>{journey=Journey.create();});
afterEach(async()=>{await journey.dispose();});


async function captureAction(user: ReturnType<typeof userEvent.setup>,name:string,activate=true){
 const direct=page().queryByRole("button",{name});if(direct){if(activate)await press(user,direct);else await waitFor(()=>expect(direct.hasAttribute("disabled")).toBe(false),{timeout:10_000});return;}
 await press(user,page().getByRole("button",{name:"More case actions"}));const entry=await waitFor(()=>{const offered=screen.getByRole("menuitem",{name});expect(offered.hasAttribute("disabled")).toBe(false);return offered;},{timeout:10_000});if(activate)await press(user,entry);else await user.keyboard("{Escape}");
}
test("capture-only selected export previews and writes exact checked order, reopens its actual artifact and retains source history across restart",async()=>{
 const user=userEvent.setup();
 journey.writeFile("exports/feed.hl7",EXPORTED_BOOKING+EXPORTED_RESCHEDULE);
 const project=await licensedProject(journey,user);
 const entry=await importExport(user,journey,"exports/feed.hl7","Original capture");
 const sourceRoot=`${project}/${entry}`;
 const originalHashes=Object.fromEntries(filesUnder(sourceRoot).map((name:string)=>[name,journey.digest(`${sourceRoot.slice(journey.path().length+1)}/${name}`)]));
 const table=await screen.findByRole("table",{name:"Messages"});
 await waitFor(()=>expect(table.querySelectorAll("tbody tr[data-row-id]")).toHaveLength(2));
 const rows=[...table.querySelectorAll<HTMLElement>("tbody tr[data-row-id]")];
 const order=[rows[1]!.dataset.rowId!,rows[0]!.dataset.rowId!];
 await press(user,within(rows[1]!).getByRole("checkbox"));
 await press(user,within(rows[0]!).getByRole("checkbox"));
 await journey.settled();
 await captureAction(user,"Export selected",false);
 // A filter may hide selected occurrences; the export retains their checked scope.
 await press(user,screen.getByRole("button",{name:"Filter messages"}));
 const filter=within(await screen.findByRole("dialog",{name:"Filter"}));
 await user.selectOptions(filter.getByLabelText("Field of rule 1"),"field");
 await waitFor(()=>expect(within(filter.getByLabelText("Message field of rule 1")).getByRole("option",{name:"MSH-9 · Message Type"})).toBeTruthy());
 await user.selectOptions(filter.getByLabelText("Message field of rule 1"),"MSH-9 · Message Type");
 await user.selectOptions(filter.getByLabelText("Operator of rule 1"),"contains");
 await enter(user,filter.getByLabelText("Value of rule 1"),"S13");
 await press(user,filter.getByRole("button",{name:"Apply"}));
 await waitFor(()=>expect(table.querySelectorAll("tbody tr[data-row-id]")).toHaveLength(1));
 await journey.settled();
 await captureAction(user,"Export selected");
 let exporter=within(await screen.findByRole("dialog",{name:"Export capture"}));
 const contents=await exporter.findByRole("table",{name:"Contents"});
 expect([...contents.querySelectorAll("tbody tr[data-row-id]")].map(row=>row.getAttribute("data-row-id"))).toEqual(order);
 await press(user,exporter.getByRole("tab",{name:"Redaction"}));
 expect(exporter.getByText("Not redacted")).toBeTruthy();
 await press(user,exporter.getByRole("tab",{name:"Preview"}));
 await press(user,exporter.getByRole("button",{name:"Show values"}));
 const preview=await exporter.findByLabelText("Selected messages.hl7",{selector:"pre"});
 expect(preview.textContent).toBe(EXPORTED_RESCHEDULE+EXPORTED_BOOKING);
 journey.makeFolder("exported");
 await journey.nameNewFolder(journey.path("exported/selection.hl7"),"Export report");
 await press(user,exporter.getByRole("button",{name:"Choose location"}));
 await waitFor(()=>expect(exporter.getByRole("button",{name:"Save local file"})).toHaveProperty("disabled",false));
 await user.dblClick(exporter.getByRole("button",{name:"Save local file"}));
 await exporter.findByText("Exported selection.hl7");
 expect(journey.readFile("exported/selection.hl7")).toBe(EXPORTED_RESCHEDULE+EXPORTED_BOOKING);
 expect(journey.callsTo("ExecuteReviewedAction").filter(call=>(call.result as {selected_export?:unknown}|undefined)?.selected_export)).toHaveLength(1);
 expect(filesUnder(journey.path("exported"))).toEqual(["selection.hl7"]);
 for(const [name,hash]of Object.entries(originalHashes))expect(journey.digest(`${sourceRoot.slice(journey.path().length+1)}/${name}`)).toBe(hash);
 await press(user,exporter.getByRole("button",{name:"Cancel"}));
 await journey.settled();
 await journey.close();
 await journey.launch();
 await page().findByRole("table",{name:"Messages"});
 await journey.settled();
 await captureAction(user,"Capture exports");
 const history=within(await screen.findByRole("dialog",{name:"Capture exports"}));
 const exports=await history.findByRole("table",{name:"Exports"});
 expect(within(exports).getByText("selection.hl7")).toBeTruthy();
 expect(within(exports).getByText("2 selected occurrences")).toBeTruthy();
 await journey.chooseFiles([journey.path("exported/selection.hl7")],"Open exported selected messages");
 await press(user,history.getByRole("button",{name:"Open preview"}));
 expect((await history.findByLabelText("selection.hl7",{selector:"pre"})).textContent).toBe(EXPORTED_RESCHEDULE+EXPORTED_BOOKING);
 expect(journey.callsTo("StartDurableRun")).toHaveLength(0);
 expect(journey.callsTo("StartCapture")).toHaveLength(0);
 expect(journey.callsTo("SaveItem").filter(call=>(call.args[0] as {kind?:string})?.kind==="test")).toHaveLength(0);
});
