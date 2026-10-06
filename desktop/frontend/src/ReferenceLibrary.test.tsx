import { expect,test,vi } from "vitest";
import { render,screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { installFacade } from "./testkit/wails";
import { ReferenceLibrary } from "./ReferenceLibrary";
import { WORKSPACE_ROOT,CASE_IDENTITY } from "./testkit/fixtures";

test("reference browsing selects an installed edition or returns to the message edition",async()=>{
  const user=userEvent.setup();const selected=vi.fn(async()=>undefined);
  const editions=["2.9","2.8.2","2.8.1","2.8","2.7.1","2.7","2.6","2.5.1","2.5","2.4","2.3.1","2.3","2.2","2.1"].map(edition=>({edition,path:WORKSPACE_ROOT,identity:CASE_IDENTITY}));
  installFacade({ReadReferenceLibrary:()=>({state:"completed",editions})});
  render(<ReferenceLibrary open messageEdition="2.5.1" onClose={()=>undefined} onSelect={selected}/>);
  await screen.findByRole("option",{name:"HL7 2.1"});
  expect(screen.getAllByRole("option")).toHaveLength(15);
  await user.selectOptions(screen.getByLabelText("Reference version"),"2.9");
  await user.click(screen.getByRole("button",{name:"Use reference"}));
  expect(selected).toHaveBeenCalledWith(editions[0]);
  await user.selectOptions(screen.getByLabelText("Reference version"),"");
  await user.click(screen.getByRole("button",{name:"Use reference"}));
  expect(selected).toHaveBeenLastCalledWith(null);
});

test("a selected catalog is installed through its native picker before exact-edition selection",async()=>{
 const user=userEvent.setup();const selected=vi.fn(async()=>undefined);
 const editions=[{edition:"2.5.1",path:WORKSPACE_ROOT,identity:CASE_IDENTITY}];
 const facade=installFacade({ReadReferenceLibrary:()=>({state:"completed",editions:[]}),ChooseInspectionPath:kind=>({state:"completed",kind,path:WORKSPACE_ROOT}),InstallReferenceCatalog:()=>({state:"completed",editions})});
 render(<ReferenceLibrary open messageEdition="2.5.1" onClose={()=>undefined} onSelect={selected}/>);
 await screen.findByRole("button",{name:"Install catalog…"});
 await user.click(screen.getByRole("button",{name:"Install catalog…"}));
 await screen.findByRole("option",{name:"HL7 2.5.1"});
 expect(facade.callsTo("InstallReferenceCatalog")[0]?.args).toEqual([WORKSPACE_ROOT]);
 expect(selected).toHaveBeenCalledWith(null);
});
