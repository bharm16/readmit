import { test, expect } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { TestInputEvidence } from "./TestInputEvidence";
import { installFacade } from "./testkit/wails";
import { CASE_ENTRY, CASE_IDENTITY, GRID_OCCURRENCE, NEXT_OCCURRENCE, WORKSPACE_ROOT, inspectionResult } from "./testkit/fixtures";

test("test input preview reads exact original identity only after explicit inspection and reveal",async()=>{
 const user=userEvent.setup();
 const facade=installFacade({InspectOccurrence:request=>inspectionResult(request.occurrence,{identity:request.identity,revealed:request.reveal})});
 render(<TestInputEvidence workspace={WORKSPACE_ROOT} entry={CASE_ENTRY} identity={CASE_IDENTITY} occurrence={GRID_OCCURRENCE}/>);
 expect(facade.callsTo("InspectOccurrence")).toHaveLength(0);
 await user.click(screen.getByRole("button",{name:"Open original message"}));
 await waitFor(()=>expect(facade.callsTo("InspectOccurrence")).toHaveLength(1));
 expect(facade.callsTo("InspectOccurrence")[0]?.args[0]).toMatchObject({workspace:WORKSPACE_ROOT,case:CASE_ENTRY,identity:CASE_IDENTITY,occurrence:GRID_OCCURRENCE,reveal:false});
 await user.click(screen.getByRole("button",{name:"Show values"}));
 await waitFor(()=>expect(facade.callsTo("InspectOccurrence")).toHaveLength(2));
 expect(facade.callsTo("InspectOccurrence")[1]?.args[0]).toMatchObject({identity:CASE_IDENTITY,occurrence:GRID_OCCURRENCE,reveal:true});
 expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
});

test("changing selected input fences a delayed original read and rejects a foreign identity",async()=>{
 const user=userEvent.setup();let release:()=>void=()=>undefined;
 const facade=installFacade({InspectOccurrence:async request=>{
  if(request.occurrence===GRID_OCCURRENCE)await new Promise<void>(resolve=>{release=resolve;});
  return inspectionResult(request.occurrence,{identity:request.occurrence===NEXT_OCCURRENCE?"foreign-identity":request.identity,revealed:request.reveal});
 }});
 const rendered=render(<TestInputEvidence workspace={WORKSPACE_ROOT} entry={CASE_ENTRY} identity={CASE_IDENTITY} occurrence={GRID_OCCURRENCE}/>);
 await user.click(screen.getByRole("button",{name:"Open original message"}));
 rendered.rerender(<TestInputEvidence workspace={WORKSPACE_ROOT} entry={CASE_ENTRY} identity={CASE_IDENTITY} occurrence={NEXT_OCCURRENCE}/>);
 await user.click(screen.getByRole("button",{name:"Open original message"}));
 expect((await screen.findByRole("alert")).textContent).toContain("The original input identity changed");
 release();await waitFor(()=>expect(facade.callsTo("InspectOccurrence")).toHaveLength(2));
 expect(screen.getByRole("alert").textContent).toContain("The original input identity changed");
});


test("original input uses the Go readable window with original segment lines instead of escaped raw bytes",async()=>{
 const user=userEvent.setup();
 const facade=installFacade({InspectOccurrence:request=>{const answer=inspectionResult(request.occurrence,{identity:request.identity,revealed:request.reveal});if(!answer.inspection)throw new Error("Expected inspection fixture");return {...answer,inspection:{...answer.inspection,readable_window:{before:"MSH|sender\nSCH|appointment\n",selected:"PID|patient",after:"\n",offset:0,end:40,message_start:0,message_end:40},raw_window:{before:"MSH|sender\\x0dSCH|appointment\\x0d",selected:"PID|patient",after:"\\x0d",offset:0,end:40,message_start:0,message_end:40}}};}});
 render(<TestInputEvidence workspace={WORKSPACE_ROOT} entry={CASE_ENTRY} identity={CASE_IDENTITY} occurrence={GRID_OCCURRENCE} source="Recorded source"/>);
 await user.click(screen.getByRole("button",{name:"Show values"}));
 const preview=await screen.findByText("PID|patient",{selector:"mark"});
 expect(preview.closest("pre")?.textContent).toBe("MSH|sender\nSCH|appointment\nPID|patient\n");
 expect(facade.callsTo("InspectOccurrence")[0]?.args[0]).toMatchObject({identity:CASE_IDENTITY,occurrence:GRID_OCCURRENCE,reveal:true});
 expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
});
