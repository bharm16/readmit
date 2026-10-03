import { expect, test } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { SelectedMessagesExportPanel } from "./SelectedMessagesExport";
import { installFacade } from "./testkit/wails";
import type { ActionReview } from "./bindings";

const source={kind:"case" as const,id:"source-under-test",revision:"1"};
const context={project:"project-under-test",generation:1};
const reviewed=(reveal:boolean):ActionReview=>({action:"capture.export-selected",consent:"export",items:[],ready:true,requirements:[],token:"review-under-test",destination:{},selected_export:{source,source_name:"Selected source",source_identity:"identity-under-test",occurrences:[{occurrence:"second",offset:0,bytes:4,sha256:"b".repeat(64),kind:"message"},{occurrence:"first",offset:4,bytes:3,sha256:"a".repeat(64),kind:"ack"}],source_values:true,redacted:false,class:"selected-original-message-bytes",output:{type:"file",format:"original-message-bytes",name:"Selected messages.hl7",size:7,files:[{name:"Selected messages.hl7",kind:"message",size:7,...(reveal?{text:"preview"}:{})}]},destination:{kind:"local",name:"selected.hl7"},consequence:"Contains original values"}});

test("capture export preserves the complete checked order and requires exact preview before its one final action",async()=>{
 const user=userEvent.setup();
 let finals=0;
 const facade=installFacade({PrepareAction:request=>({state:"completed",context:request.context,review:reviewed(request.selected_export?.reveal??false)}),WithdrawReview:()=>({state:"completed",context}),ExecuteReviewedAction:request=>{finals++;return {state:"completed",context:request.context,outcome:"completed",replayed:false,selected_export:{name:"selected.hl7",sha256:"c".repeat(64),source,source_identity:"identity-under-test"}};}});
 render(<SelectedMessagesExportPanel open context={()=>context} source={source} identity="identity-under-test" messages={["second","first"]} sourceName="Selected source" onClose={()=>undefined}/>);
 const panel=within(await screen.findByRole("dialog",{name:"Export capture"}));
 await panel.findByRole("table",{name:"Contents"});
 expect(facade.callsTo("PrepareAction").at(-1)?.args[0]).toMatchObject({selected_export:{messages:["second","first"],transformation:"original"}});
 await user.click(panel.getByRole("tab",{name:"Redaction"}));
 expect(panel.getByText("Not redacted")).toBeTruthy();
 await user.click(panel.getByRole("tab",{name:"Preview"}));
 expect(panel.getByRole("button",{name:"Save local file"})).toHaveProperty("disabled",true);
 await user.click(panel.getByRole("button",{name:"Show values"}));
 await panel.findByText("preview");
 const final=panel.getByRole("button",{name:"Save local file"});
 await user.dblClick(final);
 await waitFor(()=>expect(finals).toBe(1));
});
