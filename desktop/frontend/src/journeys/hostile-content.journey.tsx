// Untrusted content renders as inert text through the real application. A
// person's evidence carries markup in a patient name, an observation and a
// note, and a file beside it is named with markup; the window lists the folder,
// verifies the case, reads and inspects it, and searches for the hostile
// value, all against the real facade over real files. Every hostile string is
// shown as text where it is shown at all, no element is created from it and no
// planted script runs.
//
// The evidence is imported through the window's own Import flow, under the
// person's activation. This runs in jsdom; what it shows is that the window
// never turns content into markup.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Journey, press } from "../testkit/journey";
import { page, sidebar } from "../testkit/navigation";
import { importExport, licensedProject, selectMessage } from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
  delete (window as unknown as { PLANTED?: unknown }).PLANTED;
});

// Markup in the places a person's evidence carries free text. Every value is
// synthetic; the planted scripts would set window.PLANTED if they ever ran.
const IMAGE = '<img src=x onerror="window.PLANTED=1">';
const SCRIPT = "<script>window.PLANTED=2</script>";
const LINK = '<a href="javascript:window.PLANTED=3">open</a>';
const HOSTILE =
  "MSH|^~\\&|SCHEDULER|SYNTHETIC|RECEIVER|LAB|20260101120000+0000||SIU^S12|HOSTILE-1|P|2.5.1\r" +
  "SCH|PLACER-9^READMIT|FILLER-9^READMIT||||CHECKUP|ROUTINE|NORMAL|30|min|^^^20260102100000+0000\r" +
  `PID|1||SYNTH-9^^^READMIT||${IMAGE}^ONLY\r` +
  `NTE|1||${LINK}\r` +
  `OBX|1|TX|NOTE||${SCRIPT}\r`;
const HOSTILE_NAME = '<img src=x onerror="window.PLANTED=4">.json';

test("markup in evidence, file names and searches is shown as inert text and never becomes an element", async () => {
  const user = userEvent.setup();
  journey.writeFile("exports/hostile.hl7", HOSTILE);
  const requested: string[] = [];
  const observer = new MutationObserver(() => {
    for (const element of document.querySelectorAll("img, script, iframe, object, embed, a[href^='javascript']")) {
      requested.push(element.outerHTML);
    }
  });
  observer.observe(document.body, { childList: true, subtree: true, attributes: true });

  // The person activates, creates a project and imports their export.
  const project = await licensedProject(journey, user);
  journey.writeFile(`${project.slice(journey.path().length + 1)}/${HOSTILE_NAME}`, "{}");
  await importExport(user, journey, "exports/hostile.hl7", "hostile");

  // The occurrence's original bytes are shown as escaped text: every angle
  // bracket the content carried is spelled as its byte value.
  const occurrence = await selectMessage(user, "s0001-e000001");
  await press(user, await occurrence.findByRole("tab", { name: "Raw" }));
  await waitFor(() => expect(occurrence.getByRole("tabpanel").textContent).toContain("MSH|"));
  const raw = occurrence.getByRole("tabpanel").textContent ?? "";
  for (const shown of [
    '\\x3cimg src=x onerror="window.PLANTED=1"\\x3e',
    '\\x3ca href="javascript:window.PLANTED=3"\\x3eopen\\x3c/a\\x3e',
    "\\x3cscript\\x3ewindow.PLANTED=2\\x3c/script\\x3e",
  ]) {
    expect(raw).toContain(shown);
  }

  // A decoded value the person selects by its exact position is text too.
  await press(user, occurrence.getByRole("button", { name: "More message actions" }));
  await press(user, await screen.findByRole("menuitem", { name: "Go to field…" }));
  const go = within(await screen.findByRole("dialog", { name: "Go to field" }));
  await user.type(go.getByLabelText("Field path"), "OBX[1]-5{Enter}");
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Go to field" })).toBeNull());
  const value = await occurrence.findByText(/window\.PLANTED=2/, { selector: ".selected-field-value" });
  expect(value.textContent).toBe("\\x3cscript\\x3ewindow.PLANTED=2\\x3c/script\\x3e");

  // A search of the messages for the hostile value is kept as typed.
  await press(user, page().getByRole("button", { name: "Search messages" }));
  const search = within(await screen.findByRole("dialog", { name: "Search messages" }));
  await user.type(search.getByRole("searchbox", { name: "Search" }), SCRIPT);
  await press(user, search.getByRole("radio", { name: "Message content" }));
  expect((search.getByRole("searchbox", { name: "Search" }) as HTMLInputElement).value).toBe(SCRIPT);
  const reads = journey.callsTo("ReadMessages").length;
  await press(user, search.getByRole("button", { name: "Search" }));
  await waitFor(() => expect(journey.callsTo("ReadMessages")[reads]?.settled).toBe(true));
  expect(journey.callsTo("ReadMessages")[reads]?.args[0]).toMatchObject({ query: { search: { scope: "content", text: SCRIPT } } });
  expect(journey.callsTo("ReadMessages")[reads]?.result).toMatchObject({ state: "completed", matched: 1 });

  // The project's other files list the hostile file name as the text it is.
  await press(user, sidebar().getByRole("button", { name: /^Project: / }));
  await press(user, await screen.findByRole("menuitem", { name: "Files" }));
  expect(await within(await page().findByRole("table", { name: "Files" })).findByRole("row", { name: HOSTILE_NAME })).toBeTruthy();

  // Nothing the content said became an element, and nothing it planted ran.
  observer.disconnect();
  expect(requested).toEqual([]);
  expect(document.querySelectorAll("img, script, iframe, object, embed, a[href^='javascript']")).toHaveLength(0);
  expect((window as unknown as { PLANTED?: unknown }).PLANTED).toBeUndefined();
  await journey.close();
});
