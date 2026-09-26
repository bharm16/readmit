// The guided sample's import of the frozen receiver fixtures, against the
// real facade over real files and without any activation. A person copies
// the two synthetic receiver fixtures readmit ships into a folder, creates
// the sample workspace, and imports them as one imported case from the
// keyboard: the case the window writes is the case `readmit sample capture`
// writes from the same folder, byte for byte apart from the instant each
// records as its import time. A dismissed dialog, a folder whose bytes are
// not the frozen fixtures and a second import over the first write nothing,
// the fixture refusal in the command's own words.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { enter, Journey, press, region } from "../testkit/journey";
import { tabTo } from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

/** The instant a case bundle records as its import time. */
function importedAt(bundle: string): string {
  const manifest = JSON.parse(journey.readFile(`${bundle}/manifest.json`)) as { provenance: { imported_at: string } };
  return manifest.provenance.imported_at;
}

/** Every file of a case bundle of two single-message sources, with the
 * instant it recorded as its import time written as `IMPORTED_AT`. */
function withoutInstant(bundle: string): Record<string, string> {
  const instant = importedAt(bundle);
  const files: Record<string, string> = {};
  for (const name of ["manifest.json", "events.jsonl", "correlations.jsonl", "payloads/s0001-e000001.bin", "payloads/s0002-e000001.bin"]) {
    files[name] = journey.readFile(`${bundle}/${name}`).split(instant).join("IMPORTED_AT");
  }
  return files;
}

test("the frozen receiver fixtures are imported by the guided sample without activation as the command line's case", async () => {
  const user = userEvent.setup();
  const fixtures = journey.makeFolder("fixtures");
  journey.placeFixture("listen-s12.hl7", "fixtures/listen-s12.hl7");
  journey.placeFixture("listen-s13.hl7", "fixtures/listen-s13.hl7");
  const altered = journey.makeFolder("altered");
  journey.writeFile("altered/listen-s12.hl7", "MSH|^~\\&|SCHEDULER|SYNTHETIC|RECEIVER|LAB|20260101120000||SIU^S12|ALTERED|P|2.5.1\r");
  journey.placeFixture("listen-s13.hl7", "altered/listen-s13.hl7");
  journey.makeFolder("work");
  await journey.launch();

  await journey.chooseFolder(journey.path("work"), "Choose sample location");
  await press(user, screen.getByRole("button", { name: "Explore sample" }));
  const guided = within(region("Guided sample"));
  const importing = within(await guided.findByRole("form", { name: "Import fixtures" }));
  const submit = importing.getByRole("button", { name: "Import fixtures…" });
  expect((importing.getByLabelText("New case folder in this workspace") as HTMLInputElement).value).toBe("receiver-sample");
  const written = (entry: string) => {
    try {
      journey.readFile(`work/readmit-sample/${entry}/manifest.json`);
      return true;
    } catch {
      return false;
    }
  };

  // The new entry is one name in this folder, never a path, and is refused
  // before any dialog opens; an empty name offers nothing to press.
  const name = importing.getByLabelText("New case folder in this workspace");
  await enter(user, name, "nested/receiver-sample");
  await press(user, submit);
  expect(await guided.findByText("the sample case needs one new folder name in the open workspace, never a path")).toBeTruthy();
  await enter(user, name, "");
  expect((submit as HTMLButtonElement).disabled).toBe(true);
  await enter(user, name, "receiver-sample");

  // A dismissed dialog imports nothing.
  await journey.dismissDialog("folder", "Choose fixture folder");
  await press(user, submit);
  expect(await guided.findByText("no folder was chosen")).toBeTruthy();
  expect(written("receiver-sample")).toBe(false);

  // Bytes that are not the frozen fixtures are refused before a case exists,
  // in the words the command line refuses them with.
  await journey.chooseFolder(altered, "Choose fixture folder");
  await press(user, submit);
  expect(await guided.findByText("sample requires the unchanged frozen synthetic fixtures")).toBeTruthy();
  const refused = await journey.commandLine(["sample", "capture", "--fixtures", "altered", "--output", "work/readmit-sample/command-altered"]);
  expect(refused.code).not.toBe(0);
  expect(refused.stderr).toContain("sample requires the unchanged frozen synthetic fixtures");
  expect(written("receiver-sample")).toBe(false);
  expect(written("command-altered")).toBe(false);

  // From the keyboard: Tab to the import and Enter, then the folder holding
  // the fixtures in the host's own dialog.
  await journey.chooseFolder(fixtures, "Choose fixture folder");
  await tabTo(user, submit);
  await user.keyboard("{Enter}");
  const facts = await guided.findByText(/^receiver-sample: /);
  expect(facts.textContent).toBe("receiver-sample: imported · readmit-case/v1 · 2 sources · 2 occurrences · 2 messages");
  const identity = guided.getByText(/^Verified identity [0-9a-f]{64}$/).textContent?.replace("Verified identity ", "");
  expect(journey.readFile("work/readmit-sample/receiver-sample/identity.sha256").trim()).toBe(identity);
  expect(await within(region("Workspace")).findByText("receiver-sample", { selector: ".artifacts .name" })).toBeTruthy();
  // Nothing was ever activated for any of it.
  expect(journey.callsTo("SelectOperationPolicy")).toHaveLength(0);

  // The command line writes the same case from the same folder: every file
  // is the same bytes once the instant each recorded as its import time is
  // set aside, and it reports the same counts.
  const command = await journey.commandLine(["sample", "capture", "--fixtures", "fixtures", "--output", "work/readmit-sample/command-sample"]);
  expect(command.stderr).toBe("");
  expect(command.code).toBe(0);
  expect(command.stdout).toContain("Schema: readmit-case/v1\nProvenance: imported\nSources: 2\nOccurrences: 2\nMessages: 2\nACKs: 0\nUnparsed: 0\n");
  expect(withoutInstant("work/readmit-sample/receiver-sample")).toEqual(withoutInstant("work/readmit-sample/command-sample"));
  expect(journey.digest("work/readmit-sample/receiver-sample/payloads/s0002-e000001.bin")).toBe(
    journey.digest("work/readmit-sample/command-sample/payloads/s0002-e000001.bin"),
  );

  // The case it wrote opens through the reader every case is opened with. The
  // guided panel offers two buttons of this name: the sample's own authoring
  // step first, the imported case's beside the import it reports.
  await press(user, guided.getAllByRole("button", { name: "Open case" })[1]!);
  const inspector = within(region("Inspector"));
  expect(await inspector.findByText("receiver-sample", { selector: "dd" })).toBeTruthy();
  expect(inspector.getByText(identity ?? "", { selector: "dd" })).toBeTruthy();

  // A second import over the first is refused and leaves the case as it was.
  const manifest = journey.digest("work/readmit-sample/receiver-sample/manifest.json");
  await journey.chooseFolder(fixtures, "Choose fixture folder");
  await press(user, submit);
  expect(await guided.findByText("cannot create bundle; destination must be new and parent readable and writable")).toBeTruthy();
  expect(journey.digest("work/readmit-sample/receiver-sample/manifest.json")).toBe(manifest);
});
