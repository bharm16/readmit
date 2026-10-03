import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { enter, Journey, press } from "../testkit/journey";
import type { EditorDraft, ExchangeView, ReviewedActionResult } from "../bindings";
import { goTo, openListedCase, page } from "../testkit/navigation";
import {
  BOOKING,
  configureEnvironment,
  importExport,
  licensedProject,
} from "./steps";
import { accepts, freeLoopbackAddress } from "./probes.js";

let journey: Journey;
beforeEach(() => {
  journey = Journey.create();
});
afterEach(async () => {
  await journey.dispose();
});

test("selected messages use the real owned exchange review and retained complete-zero evidence reopens without a second send", async () => {
  const user = userEvent.setup();
  const { target, review } = await prepareExchange(user);
  expect(target.received()).toHaveLength(0);
  await press(user, review.getByRole("button", { name: "Send once" }));
  await screen.findByText(
    "Complete declared horizon · 0 received",
    {},
    { timeout: 15000 },
  );
  await press(
    user,
    screen.getByRole("button", { name: "Inspect received messages" }),
  );
  await screen.findByText("No messages");
  expect(
    journey
      .callsTo("OpenExchangeCapture")
      .some(
        (call) =>
          (call.result as { state?: string } | undefined)?.state ===
          "completed",
      ),
  ).toBe(true);
  expect(target.received()).toHaveLength(1);
  expect(journey.callsTo("StartCapture")).toHaveLength(0);
  await press(user, screen.getByRole("button", { name: "Done" }));
  const executions = journey.callsTo("ExecuteReviewedAction").length;
  await journey.close();
  await journey.launch();
  await press(
    user,
    await screen.findByRole("row", { name: "Scheduling interface" }),
  );
  await journey.settled();
  if (!screen.queryByRole("button", { name: "Exchange history" }))
    await openListedCase(user, "Exchange inputs");
  await journey.settled();
  await press(user, screen.getByRole("button", { name: "Exchange history" }));
  await screen.findByRole("dialog", { name: "Retained exchanges" });
  await waitFor(
    () =>
      expect(journey.callsTo("ListExchanges").at(-1)?.result).toMatchObject({
        state: "completed",
      }),
    { timeout: 15000 },
  );
  const listed = journey.callsTo("ListExchanges").at(-1)?.result;
  expect(listed, "retained exchange history readback").toMatchObject({
    state: "completed",
  });
  await press(
    user,
    await screen.findByRole("button", { name: /Exploratory receiver →/ }),
  );
  await screen.findByText("Complete declared horizon · 0 received");
  expect(target.received()).toHaveLength(1);
  expect(journey.callsTo("ExecuteReviewedAction")).toHaveLength(executions);
});

async function prepareExchange(user: UserEvent) {
  await licensedProject(journey, user);
  const address = await freeLoopbackAddress();
  const port = address.slice(address.lastIndexOf(":") + 1);
  // Save the listener through its existing source editor, without starting it.
  await press(user, await screen.findByRole("button", { name: "Capture" }));
  let setup = within(
    await screen.findByRole("dialog", { name: "New capture" }),
  );
  await journey.settled();
  await user.selectOptions(await setup.findByLabelText("Source"), "new");
  const source = within(
    await screen.findByRole("dialog", { name: "New source" }),
  );
  await enter(user, source.getByLabelText("Name"), "Exploratory receiver");
  await enter(user, source.getByLabelText("Port"), port);
  await press(user, source.getByRole("button", { name: "Save" }));
  setup = within(await screen.findByRole("dialog", { name: "New capture" }));
  await press(user, setup.getByRole("button", { name: "Cancel" }));
  journey.writeFile("exports/booking.hl7", BOOKING);
  await goTo(user, "Cases");
  const backToCaptures = screen.queryByRole("button", {
    name: /^Back to (cases|captures)$/i,
  });
  if (backToCaptures) await press(user, backToCaptures);
  await importExport(user, journey, "exports/booking.hl7", "Exchange inputs");
  const target = await journey.startDownstream(
    "downstream/ledger.csv",
    "fixed",
  );
  await configureEnvironment(
    user,
    journey,
    target.address,
    "downstream/ledger.csv",
    "Exploratory target",
  );
  await openListedCase(user, "Exchange inputs");
  const messages = within(
    await screen.findByRole("region", { name: "Messages" }),
  );
  const choice = (
    await messages.findAllByRole("checkbox", {}, { timeout: 15000 })
  ).find((control) =>
    control.getAttribute("aria-label")?.startsWith("Select "),
  );
  if (!choice)
    throw new Error("the Messages browser has no selectable occurrence");
  await user.click(choice);
  await press(
    user,
    screen.getByRole("button", { name: "Receive and send selected" }),
  );
  const exchange = within(
    await screen.findByRole("dialog", { name: "Receive messages" }),
  );
  await exchange.findByRole("option", { name: "Exploratory receiver" });
  await user.selectOptions(
    exchange.getByLabelText("Receiver source"),
    exchange
      .getByRole("option", { name: "Exploratory receiver" })
      .getAttribute("value")!,
  );
  await user.selectOptions(
    exchange.getByLabelText("Target"),
    exchange
      .getByRole("option", { name: "Exploratory target" })
      .getAttribute("value")!,
  );
  await enter(user, exchange.getByLabelText("Post-send horizon (ms)"), "100");
  await press(user, exchange.getByRole("button", { name: "Review send" }));
  const review = within(
    await screen.findByRole("dialog", { name: "Send selected messages" }),
  );
  await review.findByText(address);
  return { target, review, address };
}

test("a crashed exploratory exchange retains uncertain delivery and reopening does not resend or rearm", async () => {
  const user = userEvent.setup();
  const { target, review, address } = await prepareExchange(user);
  target.holdAcknowledgements();
  await press(user, review.getByRole("button", { name: "Send once" }));
  await waitFor(() => expect(target.received()).toHaveLength(1), {
    timeout: 10000,
  });
  const executions = journey.callsTo("ExecuteReviewedAction").length;
  await journey.crash();
  expect(await accepts(address)).toBe(false);
  await journey.launch();
  await press(
    user,
    await screen.findByRole("row", { name: "Scheduling interface" }),
  );
  await journey.settled();
  if (!screen.queryByRole("button", { name: "Exchange history" }))
    await openListedCase(user, "Exchange inputs");
  await journey.settled();
  await press(user, screen.getByRole("button", { name: "Exchange history" }));
  await press(
    user,
    await screen.findByRole("button", {
      name: /Exploratory receiver →.*Delivery uncertain/,
    }),
  );
  await screen.findByText("Uncertain — inspect retained evidence");
  await screen.findByText("Incomplete capture · 0 retained");
  expect(target.received()).toHaveLength(1);
  expect(await accepts(address)).toBe(false);
  expect(journey.callsTo("ExecuteReviewedAction")).toHaveLength(executions);
  expect(journey.callsTo("StartCapture")).toHaveLength(0);
});


test("retained exchange becomes a named incomplete draft, resumes after restart and returns to exact evidence without another send", async () => {
 const user=userEvent.setup();const {target,review}=await prepareExchange(user);
 await press(user,review.getByRole("button",{name:"Send once"}));
 await screen.findByText("Complete declared horizon · 0 received",{},{timeout:15000});
 const actual=journey.callsTo("ExecuteReviewedAction").at(-1)?.result as ReviewedActionResult;
 const exchange=actual.exchange as ExchangeView;
 expect(exchange.inputs?.messages).toHaveLength(1);
 await press(user,await screen.findByRole("button",{name:"Create test draft from exchange"}));
 await enter(user,await page().findByRole("textbox",{name:"Name"}),"Exchange requires authored behavior");
 await page().findByText(/Observed output is evidence, never an expected result/);
 await press(user,page().getByRole("button",{name:"Save draft"}));
 await page().findByRole("table",{name:"Test drafts"});
 const retained=journey.callsTo("SaveEditorDraft").at(-1)?.args[0] as EditorDraft;
 expect(retained.content_schema).toBe("readmit-desktop-test-editor/v3");
 expect(retained.content).toMatchObject({draft:{connected_test:{variables:[],steps:exchange.inputs!.messages.map(occurrence=>({source:{occurrence,identity:exchange.input_identity},v2:{}})),phases:[{observations:[],checks:[],acknowledgements:[]}]},test_links:{source:{kind:"exchange",exchange:{origin:{id:exchange.id,identity:exchange.identity},coverage:"complete",received:0}}}}});
 const executions=journey.callsTo("ExecuteReviewedAction").length;
 await journey.close();await journey.launch();
 await page().findByRole("table",{name:"Test drafts"});
 await press(user,page().getByRole("button",{name:"Resume Exchange requires authored behavior"}));
 expect(await page().findByRole("textbox",{name:"Name"})).toHaveProperty("value","Exchange requires authored behavior");
 await page().findByRole("region",{name:"Originating exchange"});
 await press(user,page().getByRole("button",{name:"Return to retained exchange"}));
 const evidence=within(await screen.findByRole("dialog",{name:"Retained exchanges"}));
 await evidence.findByText("Complete declared horizon · 0 received");
 expect(journey.callsTo("ListExchanges").at(-1)?.result).toMatchObject({exchanges:[{id:exchange.id,identity:exchange.identity}]});
 expect(target.received()).toHaveLength(1);
 expect(journey.callsTo("ExecuteReviewedAction")).toHaveLength(executions);
 expect(journey.callsTo("StartCapture")).toHaveLength(0);
});
