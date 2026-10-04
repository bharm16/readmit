import { expect, test } from "vitest";
import { render, screen, within } from "@testing-library/react";
import { useState } from "react";
import userEvent from "@testing-library/user-event";
import { renderApp } from "./testkit/app";
import { folderWithCase } from "./testkit/fixtures";
import { page, sidebar } from "./testkit/navigation";
import { TaskTabs } from "./TaskTabs";
import { expectTabPattern } from "./testkit/tabs";

test("the workbench has four primary destinations and concrete global and retained-work owners", async () => {
  const user = userEvent.setup();
  await renderApp({ SelectWorkspace: () => folderWithCase(), WorkingSession: () => ({ state: "completed" }) });
  await user.click(screen.getByRole("button", { name: "Open" }));
  const nav = sidebar();
  for (const name of ["Messages", "Captures", "Test cases", "Targets", "Settings", "Help"]) {
    expect(await nav.findByRole("button", { name })).toBeTruthy();
  }
  expect(nav.queryByRole("button", { name: "Runs" })).toBeNull();
  expect(nav.queryByRole("button", { name: "Reports" })).toBeNull();
  expect(nav.queryByRole("button", { name: "Tools" })).toBeNull();
  await user.click(nav.getByRole("button", { name: "Test cases" }));
  await user.click(page().getByRole("button", { name: "Test page actions" }));
  for (const name of ["Run history", "Exports", "Schedules"]) expect(await screen.findByRole("menuitem", { name })).toBeTruthy();
  await user.click(screen.getByRole("menuitem", { name: "Run history" }));
  expect(await page().findByRole("heading", { level: 1, name: "Runs" })).toBeTruthy();
  await user.click(nav.getByRole("button", { name: "Messages" }));
  expect(await page().findByRole("heading", { level: 1, name: "Messages" })).toBeTruthy();
  expect(page().getByRole("button", { name: "Open file" })).toBeTruthy();
  await user.click(nav.getByRole("button", { name: "Settings" }));
  await user.click(await page().findByRole("button", { name: "Tools" }));
  const tools = page().getByRole("list", { name: "Tools" });
  expect(within(tools).getByRole("button", { name: /Inspect file/ })).toBeTruthy();
});

test("task tabs expose exactly one selected tab and tab stop through pointer and keyboard navigation", async()=>{
 function Views(){const [selected,setSelected]=useState("inputs");return <TaskTabs label="Test views" id="native-test-views" tabs={[{key:"inputs",label:"Inputs"},{key:"expectations",label:"Expectations"},{key:"setup",label:"Setup"},{key:"configuration",label:"Configuration"}]} selected={selected} onSelect={setSelected}>{selected}</TaskTabs>;}
 const user=userEvent.setup();render(<Views/>);const strip=screen.getByRole("tablist",{name:"Test views"});expectTabPattern(strip);
 expect(within(strip).getByRole("tab",{name:"Inputs",selected:true})).toBeTruthy();
 await user.click(within(strip).getByRole("tab",{name:"Expectations"}));expectTabPattern(strip);
 expect(within(strip).getByRole("tab",{name:"Inputs"}).getAttribute("aria-selected")).toBe("false");
 await user.keyboard("{ArrowRight}");expectTabPattern(strip);expect(within(strip).getByRole("tab",{name:"Setup",selected:true})).toBe(document.activeElement);
});

test("a refused navigation write explains restart limits without clearing the restoration notice", async()=>{
 const user=userEvent.setup();
 await renderApp({SelectWorkspace:()=>folderWithCase(),WorkingSession:()=>({state:"failed",reason:"The previous source is unavailable."}),RecordView:()=>({state:"failed",reason:"The checked selection is larger than the 1024-occurrence restart limit."})});
 await user.click(screen.getByRole("button",{name:"Open"}));
 expect(await screen.findByText("The checked selection is larger than the 1024-occurrence restart limit.")).toBeTruthy();
 expect(screen.getByText("The previous source is unavailable.")).toBeTruthy();
});

test("a remembered workspace that answers after deliberate navigation cannot replace the chosen destination",async()=>{
 const {waitFor,act}=await import("@testing-library/react");
 let answer:((result:import("./bindings").WorkspaceResult)=>void)|undefined;
 const {facade}=await renderApp({WorkingSession:()=>({state:"completed",session:{schema:"readmit-desktop-session/v3",view:{workspace:"/remembered",region:"evidence",case:"",run:""}}}),OpenWorkspace:()=>new Promise(resolve=>{answer=resolve;})});
 await waitFor(()=>expect(answer).toBeTruthy());const user=userEvent.setup();await user.click(sidebar().getByRole("button",{name:"Help"}));await page().findByRole("heading",{name:"Help"});
 await act(async()=>{answer!(folderWithCase());});await waitFor(()=>expect(page().getByRole("heading",{name:"Help"})).toBeTruthy());
 expect(facade.callsTo("OpenCase")).toHaveLength(0);expect(sidebar().queryByRole("button",{name:"Captures"})).toBeNull();
});
