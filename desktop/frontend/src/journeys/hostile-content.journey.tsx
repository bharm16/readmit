// Untrusted content renders as inert text through the real application. A
// person's evidence carries markup in a patient name, an observation and a
// note, and a file beside it is named with markup; the window lists the folder,
// verifies the case, indexes and inspects it, and searches for the hostile
// value, all against the real facade over real files. Every hostile string is
// shown as text where it is shown at all, no element is created from it and no
// planted script runs.
//
// The evidence is captured into a case bundle by the command line beside the
// application, under the person's activation, before the window ever sees it:
// the window's own import is exercised by the own-evidence journey. This runs
// in jsdom; what it shows is that the window never turns content into markup.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Journey, press, region } from "../testkit/journey";

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
  const license = journey.provisionLicense("vendor-delivered-license");
  journey.writeFile("exports/hostile.hl7", HOSTILE);
  journey.writeFile(`work/${HOSTILE_NAME}`, "{}");
  const captured = await journey.commandLine([
    "--operation-policy", `${license}/operation-policy.json`,
    "capture", "exports/hostile.hl7", "--format", "raw", "--terminator", "cr", "--output", "work/hostile-case",
  ]);
  expect(captured.code, captured.stderr).toBe(0);
  const requested: string[] = [];
  const observer = new MutationObserver(() => {
    for (const element of document.querySelectorAll("img, script, iframe, object, embed, a[href^='javascript']")) {
      requested.push(element.outerHTML);
    }
  });
  observer.observe(document.body, { childList: true, subtree: true, attributes: true });
  await journey.launch();

  // Indexing is licensed work, so the person selects their activation first.
  await press(user, screen.getByRole("button", { name: "License" }));
  const access = within(region("License"));
  await journey.chooseFolder(license, "Choose the license activation folder");
  await press(user, access.getByRole("button", { name: "Choose activation folder…" }));
  await press(user, access.getByRole("button", { name: "Refresh activation" }));
  expect(await access.findByText(/^License: active\./)).toBeTruthy();

  // The folder lists the hostile file name as the text it is.
  await journey.chooseFolder(journey.path("work"), "Open a readmit workspace folder");
  await press(user, screen.getAllByRole("button", { name: "Open workspace…" })[0] as HTMLElement);
  const navigation = within(region("Workspace"));
  expect(await navigation.findByText(HOSTILE_NAME)).toBeTruthy();

  // Verify the case and index it, as a person investigating it would.
  const listed = (await navigation.findByText("hostile-case")).closest("li")!;
  await press(user, within(listed).getByRole("button", { name: "Open case" }));
  const inspector = within(region("Inspector"));
  await press(user, await inspector.findByRole("button", { name: "Build case index" }));
  const form = within(await inspector.findByRole("form", { name: "Build index form" }));
  await user.click(form.getByRole("radio", { name: /Cryptographic digests/ }));
  await press(user, form.getByRole("button", { name: "Build index" }));
  expect(await inspector.findByText(/Showing 1 of 1 matching/)).toBeTruthy();

  // The occurrence's original bytes are shown as escaped text: every angle
  // bracket the content carried is spelled as its byte value.
  await press(user, (await inspector.findAllByRole("button", { name: /^Inspect s\d+-e\d+$/ }))[0]!);
  const occurrence = within(inspector.getByRole("region", { name: "Message inspector" }));
  const raw = (await occurrence.findByText(/^MSH\|/, { selector: "code" })).textContent ?? "";
  for (const shown of [
    '\\x3cimg src=x onerror="window.PLANTED=1"\\x3e',
    '\\x3ca href="javascript:window.PLANTED=3"\\x3eopen\\x3c/a\\x3e',
    "\\x3cscript\\x3ewindow.PLANTED=2\\x3c/script\\x3e",
  ]) {
    expect(raw).toContain(shown);
  }
  // A decoded value the person selects by its exact position is escaped text
  // too.
  await user.type(occurrence.getByLabelText("Field path"), "OBX[1]-5");
  await press(user, occurrence.getByRole("button", { name: "Inspect selector" }));
  const decoded = await occurrence.findByText("Decoded value (escaped text)");
  await waitFor(() => expect(decoded.nextElementSibling?.textContent).toContain("window.PLANTED=2"));
  expect(decoded.nextElementSibling?.textContent).not.toContain("<script>");

  // A search for the hostile value is shown as typed and answered as text.
  const commands = within(region("Commands"));
  await user.type(commands.getByLabelText("Search workspace"), SCRIPT);
  await press(user, commands.getByRole("button", { name: "Search" }));
  expect((commands.getByLabelText("Search workspace") as HTMLInputElement).value).toBe(SCRIPT);

  // Nothing the content said became an element, and nothing it planted ran.
  observer.disconnect();
  expect(requested).toEqual([]);
  expect(document.querySelectorAll("img, script, iframe, object, embed, a[href^='javascript']")).toHaveLength(0);
  expect((window as unknown as { PLANTED?: unknown }).PLANTED).toBeUndefined();
  await journey.close();
});
