import {afterEach,beforeEach,expect,test} from "vitest";
import {screen,within} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {Journey,press} from "../testkit/journey";
import {goTo,page} from "../testkit/navigation";
import {licensedProject} from "./steps";
let journey:Journey;beforeEach(()=>{journey=Journey.create();});afterEach(async()=>{await journey.dispose();});
test("Test cases and Suites expose runner, team, schedule and CI routes and Back restores their initiating view without execution",async()=>{
 const user=userEvent.setup();await licensedProject(journey,user);await goTo(user,"Tests");await page().findByRole("heading",{name:"Test cases"});
 await press(user,page().getByRole("tab",{name:"Suites"}));await page().findByRole("table",{name:"Suites"});
 await press(user,page().getByRole("button",{name:"Reusable work"}));await press(user,screen.getByRole("menuitem",{name:"Runners"}));await page().findByRole("region",{name:"Runners"});
 await press(user,page().getByRole("button",{name:"Back"}));await page().findByRole("table",{name:"Suites"});expect(page().getByRole("tab",{name:"Suites"}).getAttribute("aria-selected")).toBe("true");
 await press(user,page().getByRole("button",{name:"Schedules"}));await page().findByRole("heading",{name:"Schedules"});await press(user,page().getByRole("button",{name:"Back"}));await page().findByRole("table",{name:"Suites"});
 await press(user,page().getByRole("button",{name:"Reusable work"}));await press(user,screen.getByRole("menuitem",{name:"Team"}));await page().findByRole("heading",{name:"Settings"});await press(user,page().getByRole("button",{name:"Back"}));await page().findByRole("table",{name:"Suites"});
 journey.makePrivateFolder("ci-empty");await journey.chooseFolder(journey.path("ci-empty"),"Import CI results");await press(user,page().getByRole("button",{name:"Reusable work"}));await press(user,screen.getByRole("menuitem",{name:"Import CI results"}));
 const ci=within(await screen.findByRole("dialog",{name:"CI results"}));await ci.findByRole("alert");await press(user,ci.getByRole("button",{name:"Close"}));await page().findByRole("table",{name:"Suites"});
 expect(journey.callsTo("ExecuteReviewedAction")).toHaveLength(0);expect(journey.callsTo("StartDurableRun")).toHaveLength(0);expect(journey.callsTo("CommandSchedule")).toHaveLength(0);expect(journey.callsTo("SaveSchedulePolicy")).toHaveLength(0);
});
