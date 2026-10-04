import {expect,test} from "vitest";
import {screen,waitFor,within} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {renderApp} from "./testkit/app";
import {folderWithCase} from "./testkit/fixtures";
import {goTo,page} from "./testkit/navigation";

test("receive configurations save real bounded source settings and return without starting capture or sending",async()=>{
 const user=userEvent.setup();
 const {facade}=await renderApp({SelectWorkspace:()=>folderWithCase(),SaveItem:request=>({state:"completed",context:request.context,outcome:"saved",saved:{kind:"source",id:"owned-source",revision:"1"},replayed:false,problems:[]})});
 await user.click(screen.getByRole("button",{name:"Open"}));await goTo(user,"Targets");
 await user.click(page().getByRole("button",{name:"Receive configurations"}));
 const configs=within(await screen.findByRole("dialog",{name:"Receive configurations"}));
 await user.click(configs.getByRole("button",{name:"New receive configuration"}));
 const editor=within(await screen.findByRole("dialog",{name:"Receive setup"}));
 await user.type(editor.getByLabelText("Name"),"Owned bounded receiver");
 await user.clear(editor.getByLabelText("Message limit"));await user.type(editor.getByLabelText("Message limit"),"100");
 await user.clear(editor.getByLabelText("Idle timeout"));await user.type(editor.getByLabelText("Idle timeout"),"5m");
 await user.selectOptions(editor.getByLabelText("ACK code"),"AA");
 await user.click(editor.getByRole("button",{name:"Save"}));
 await waitFor(()=>expect(screen.queryByRole("dialog",{name:"Receive setup"})).toBeNull());
 expect(facade.callsTo("SaveItem").at(-1)?.args[0]).toMatchObject({kind:"source",draft:{source:{type:"mllp-listener",listener:{bind_address:"127.0.0.1",message_limit:100,idle_timeout:"5m",ack_code:"AA"}}}});
 expect(facade.callsTo("StartCapture")).toHaveLength(0);expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
});
