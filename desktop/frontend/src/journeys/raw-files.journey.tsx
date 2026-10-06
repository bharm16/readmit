// Raw files the window never imports. A person inspects an exported MLLP file
// the way `readmit inspect` does — every message, segment and field, paged,
// with values only on request — writes a byte-identical copy of it into a
// folder they chose, and is refused a second copy over the first. They then
// generate a declared synthetic input into their project and benchmark it,
// scanning it in bounded batches with a window of its records. Every
// answer comes from the real facade over real files, the source is unchanged
// throughout, and the command line, run over the same files with the same
// declarations, prints the same rows and writes the same corpus, manifest and
// benchmark.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { enter, Journey, press } from "../testkit/journey";
import { goTo, page } from "../testkit/navigation";
import { licensedProject } from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

test("a raw file is inspected and copied in the window exactly as the command line inspects and copies it", async () => {
  const user = userEvent.setup();
  const exported = journey.placeFixture("two-messages.mllp", "exports/two-messages.mllp");
  const before = journey.digest("exports/two-messages.mllp");
  journey.makeFolder("copies");
  await journey.launch();

  // The palette opens the tool, which goes straight to the host's Open dialog.
  await journey.chooseFiles([exported], "Open HL7 file");
  await user.keyboard("{Control>}k{/Control}");
  await user.type(screen.getByLabelText("Search commands"), "Inspect file{Enter}");
  expect(await screen.findByRole("heading", { name: "two-messages.mllp" })).toBeTruthy();

  // Auto detection reads the MLLP frames; the command line lists the same messages.
  const table = await screen.findByRole("table", { name: "Messages in this file" });
  const command = await journey.commandLine(["inspect", "exports/two-messages.mllp", "--format", "mllp"]);
  expect(command.code).toBe(0);
  const messages = command.stdout.split("\n").filter((line) => /^Message \d+:/.test(line));
  await waitFor(() => expect(within(table).getAllByRole("row").filter((row) => row.hasAttribute("data-row-id"))).toHaveLength(messages.length));

  // Parser values are visible by default.
  await press(user, table.querySelector<HTMLElement>('[data-row-id="0"]')!);
  const details = within(await screen.findByRole("region", { name: "Message details" }));
  await waitFor(() => expect(details.getByRole("region", {name:"Original message"}).textContent).toContain("EXAMPLE^CHARLIE"));
  await press(user, details.getByRole("button", { name: /^PID/ }));
  await waitFor(() => expect(details.getByRole("region", {name:"Original message"}).textContent).toContain("EXAMPLE^CHARLIE"));

  // A byte-identical copy goes to a new file the person named.
  await journey.nameNewFolder(journey.path("copies", "window.mllp"), "Save copy");
  await press(user, screen.getByRole("button", { name: "More file actions" }));
  await press(user, screen.getByRole("menuitem", { name: "Save copy…" }));
  // A saved copy is silent; the facade answered it written.
  await waitFor(() => expect(journey.callsTo("SaveFileCopy")[0]?.settled).toBe(true));
  expect(journey.callsTo("SaveFileCopy")[0]?.result).toMatchObject({ state: "completed" });
  expect(screen.queryByRole("alert")).toBeNull();
  const copied = await journey.commandLine(["inspect", "exports/two-messages.mllp", "--format", "mllp", "--roundtrip", "copies/command.mllp"]);
  expect(copied.code).toBe(0);
  expect(journey.digest("copies/window.mllp")).toBe(before);
  expect(journey.digest("copies/command.mllp")).toBe(before);

  // A second copy over the first is refused, and neither file changes.
  await journey.nameNewFolder(journey.path("copies", "window.mllp"), "Save copy");
  await press(user, screen.getByRole("button", { name: "More file actions" }));
  await press(user, screen.getByRole("menuitem", { name: "Save copy…" }));
  expect(await screen.findByRole("alert")).toBeTruthy();
  expect(journey.digest("copies/window.mllp")).toBe(before);
  expect(journey.digest("exports/two-messages.mllp")).toBe(before);
  expect(journey.callsTo("SaveFileCopy").every((call) => call.settled)).toBe(true);
});

test("a corpus generated and scanned in the window is the command line's corpus, manifest and benchmark", async () => {
  const user = userEvent.setup();
  journey.makeFolder("command");
  const project = await licensedProject(journey, user);
  const inProject = (path: string) => `${project.slice(journey.path().length + 1)}/.readmit/benchmarks/${path}`;

  // Tools › Benchmarks › Generate input, with every declaration the command
  // line takes.
  await goTo(user, "Tools");
  await press(user, page().getByRole("button", { name: "Benchmarks" }));
  await press(user, (await page().findAllByRole("button", { name: "Generate input" }))[0]!);
  const generation = within(await screen.findByRole("dialog", { name: "Generate input" }));
  await enter(user, generation.getByLabelText("Name"), "Nightly sample");
  await enter(user, generation.getByLabelText("Message count"), "3000");
  await user.selectOptions(generation.getByLabelText("Framing"), "mllp");
  await enter(user, generation.getByLabelText("Seed"), "7");
  await enter(user, generation.getByLabelText("Base time"), "2026-01-02T03:04:05Z");
  await user.selectOptions(generation.getByLabelText("Terminator"), "cr");
  await user.selectOptions(generation.getByLabelText("Encoding"), "us-ascii");
  await user.selectOptions(generation.getByLabelText("Direction"), "inbound");
  await press(user, generation.getByRole("button", { name: "Generate input" }));
  await waitFor(() => expect(journey.callsTo("GenerateInput")[0]?.settled).toBe(true), { timeout: 60_000 });
  const input = (journey.callsTo("GenerateInput")[0]?.result as { state: string; input?: { id: string } }).input;
  expect(input?.id).toBeTruthy();

  const generated = await journey.commandLine([
    "--operation-policy", journey.path("vendor-delivered-license", "operation-policy.json"),
    "corpus", "generate", "--output", "command/corpus", "--manifest", "command/manifest.json",
    "--seed", "7", "--base-time", "2026-01-02T03:04:05Z",
    "--generator-version", "readmit-corpus-v1", "--profile-version", "readmit-siu-v1", "--messages", "3000",
    "--framing", "mllp", "--terminator", "cr", "--encoding", "us-ascii", "--direction", "inbound",
  ]);
  expect(generated.code, generated.stderr).toBe(0);
  const corpus = inProject(`inputs/${input!.id}/corpus`);
  expect(journey.digest(corpus)).toBe(journey.digest("command/corpus"));
  expect(journey.readFile(inProject(`inputs/${input!.id}/manifest.json`))).toBe(journey.readFile("command/manifest.json"));

  // The new input is offered in Start benchmark, scanned in bounded batches
  // with a window of its records.
  const start = within(await screen.findByRole("dialog", { name: "Start benchmark" }));
  expect((start.getByRole("radio", { name: "Nightly sample" }) as HTMLInputElement).checked).toBe(true);
  await enter(user, start.getByLabelText("Records per batch"), "64");
  await enter(user, start.getByLabelText("Records to show"), "3");
  await enter(user, start.getByLabelText("Offset"), "1500");
  await press(user, start.getByRole("button", { name: "Start benchmark" }));
  await waitFor(() => expect(journey.callsTo("StartBenchmark")[0]?.settled).toBe(true), { timeout: 60_000 });
  const measured = (journey.callsTo("StartBenchmark")[0]?.result as { state: string; result?: { id: string } }).result;
  expect(measured?.id).toBeTruthy();

  const scanned = await journey.commandLine([
    "corpus", "scan", "command/corpus", "--framing", "mllp", "--terminator", "cr", "--encoding", "us-ascii",
    "--direction", "inbound", "--batch-records", "64", "--window-offset", "1500", "--window-limit", "3",
    "--report", "command/benchmark.json",
  ]);
  expect(scanned.code, scanned.stderr).toBe(0);
  const printed = (name: string) => new RegExp(`^${name}: (.*)$`, "m").exec(scanned.stdout)?.[1];

  // The benchmark's page states the counts the command prints.
  const table = await page().findByRole("table", { name: "Benchmarks" });
  await press(user, await within(table).findByRole("row", { name: /^Nightly sample/ }));
  const details = within(await screen.findByRole("region", { name: "Nightly sample" }));
  const stated = (label: string) => details.getByText(label, { selector: "dt" }).nextElementSibling?.textContent;
  expect(stated("Completion")).toBe("Complete");
  expect(stated("Records")?.replace(/,/g, "")).toBe(printed("Records"));
  expect(stated("Batches")?.replace(/,/g, "")).toBe(printed("Parsing batches"));

  // The benchmark document the window kept is the command's, apart from the
  // time each scan took.
  const benchmark = (path: string) => {
    const document = JSON.parse(journey.readFile(path));
    delete document.measured.elapsed_milliseconds;
    return document;
  };
  expect(benchmark(inProject(`results/${measured!.id}/benchmark.json`))).toEqual(benchmark("command/benchmark.json"));
  expect(journey.digest(corpus)).toBe(journey.digest("command/corpus"));
});
