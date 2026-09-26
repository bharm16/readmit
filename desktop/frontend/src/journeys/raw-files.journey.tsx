// Raw files the window never imports. A person inspects an exported MLLP file
// the way `readmit inspect` does — every message, segment and field, paged,
// with values only on request — writes a byte-identical copy of it into a
// folder they chose, and is refused a second copy over the first. They then
// generate a declared performance corpus into another chosen folder and scan
// it in bounded batches, with a window of its records and a benchmark. Every
// answer comes from the real facade over real files, the source is unchanged
// throughout, and the command line, run over the same files with the same
// declarations, prints the same rows and writes the same corpus, manifest and
// benchmark.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { UserEvent } from "@testing-library/user-event";
import { enter, Journey, press, region } from "../testkit/journey";
import { activateLicense } from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

/** One line of `readmit inspect` as the window writes the same row. */
function asWindowRow(line: string): string {
  const message = /^Message (\d+): terminator=(\S+) \((\w+)\), bytes \[(\d+),(\d+)\), (.*)$/.exec(line);
  if (message) return `Message ${message[1]} · terminator ${message[2]} (${message[3]}) · bytes [${message[4]},${message[5]}) · ${message[6]}`;
  const segment = /^ {2}(\S+) bytes \[(\d+),(\d+)\)$/.exec(line);
  if (segment) return `${segment[1]} · bytes [${segment[2]},${segment[3]})`;
  const repetition = /^ {6}repetition (\d+): (\w+) \((\d+) bytes\)$/.exec(line);
  if (repetition) return `repetition ${repetition[1]} · ${repetition[2]} · ${repetition[3]} bytes`;
  const field = /^ {4}(.+): (\w+)(?: \((\d+) bytes\))?$/.exec(line);
  if (field) return `${field[1]} · ${field[2]}${field[3] !== undefined ? ` · ${field[3]} bytes` : ""}`;
  throw new Error(`not an inspect row: ${line}`);
}

/** Every row the window shows, page by page, as a person reads them. */
async function everyRow(user: UserEvent): Promise<string[]> {
  const inspection = within(region("Inspect HL7 file"));
  const shown: string[] = [];
  for (;;) {
    const list = await inspection.findByRole("list", { name: "Inspection rows" });
    shown.push(...within(list).getAllByRole("listitem").map((item) => item.textContent ?? ""));
    const next = inspection.getByRole("button", { name: "Next page" }) as HTMLButtonElement;
    if (next.disabled) return shown;
    const page = inspection.getByText(/^Rows \d+–\d+ of \d+$/).textContent;
    await press(user, next);
    await waitFor(() => expect(inspection.getByText(/^Rows \d+–\d+ of \d+$/).textContent).not.toBe(page));
  }
}

test("a raw file is inspected and copied in the window exactly as the command line inspects and copies it", async () => {
  const user = userEvent.setup();
  const exported = journey.placeFixture("two-messages.mllp", "exports/two-messages.mllp");
  const before = journey.digest("exports/two-messages.mllp");
  journey.makeFolder("copies");
  await journey.launch();

  // The palette opens the screen, from the keyboard.
  await user.keyboard("{Control>}k{/Control}");
  await user.type(screen.getByLabelText("Search commands"), "inspect HL7{Enter}");
  const inspection = within(region("Inspect HL7 file"));
  await journey.chooseFiles([exported], "Open HL7 file");
  await press(user, inspection.getByRole("button", { name: "Browse…" }));
  expect(await inspection.findByText(exported)).toBeTruthy();
  await user.selectOptions(inspection.getByLabelText("Framing"), "mllp");
  await press(user, inspection.getByRole("button", { name: "Inspect" }));
  expect(await inspection.findByText(/^Format: mllp \(declared\) · Messages: 2 · /)).toBeTruthy();
  const shown = await everyRow(user);

  const command = await journey.commandLine(["inspect", "exports/two-messages.mllp", "--format", "mllp"]);
  expect(command.code).toBe(0);
  const lines = command.stdout.trimEnd().split("\n");
  expect(lines.slice(0, 2)).toEqual(["Format: mllp (declared)", "Messages: 2"]);
  expect(shown).toEqual(lines.slice(2).map(asWindowRow));
  expect(inspection.queryByText(/EXAMPLE\^CHARLIE/)).toBeNull();

  // Values appear only once a person asks for them, escaped as the command
  // escapes them.
  await user.click(inspection.getByLabelText("Show values"));
  await press(user, inspection.getByRole("button", { name: "Inspect" }));
  expect(await inspection.findByText(/"EXAMPLE\^CHARLIE"/)).toBeTruthy();

  // A byte-identical copy goes to a new file of a folder the person chose.
  await journey.chooseFolder(journey.path("copies"), "Choose copy destination");
  await press(user, inspection.getByRole("button", { name: "Choose destination…" }));
  expect(await inspection.findByText(journey.path("copies"))).toBeTruthy();
  await enter(user, inspection.getByLabelText("Copy filename"), "window.mllp");
  await press(user, inspection.getByRole("button", { name: "Save copy" }));
  expect(await inspection.findByText(/· the same bytes the inspection read$/)).toBeTruthy();
  const copied = await journey.commandLine(["inspect", "exports/two-messages.mllp", "--format", "mllp", "--roundtrip", "copies/command.mllp"]);
  expect(copied.code).toBe(0);
  expect(journey.digest("copies/window.mllp")).toBe(before);
  expect(journey.digest("copies/command.mllp")).toBe(before);

  // A second copy over the first is refused in the command's own words, and
  // neither the copy nor the source changes.
  await press(user, inspection.getByRole("button", { name: "Save copy" }));
  expect(await inspection.findByText("cannot create round-trip file; destination must be new and writable")).toBeTruthy();
  expect(journey.digest("copies/window.mllp")).toBe(before);
  expect(journey.digest("exports/two-messages.mllp")).toBe(before);
  expect(journey.callsTo("InspectRawFile").every((call) => call.settled)).toBe(true);
});

test("a corpus generated and scanned in the window is the command line's corpus, manifest and benchmark", async () => {
  const user = userEvent.setup();
  journey.makeFolder("corpora");
  journey.makeFolder("command");
  journey.makeFolder("benchmarks");
  await journey.launch();
  await activateLicense(user, journey);

  await press(user, screen.getByRole("button", { name: "Performance corpus" }));
  const generation = within(screen.getByRole("tabpanel", { name: "Generate" }));
  await enter(user, generation.getByLabelText("Seed"), "7");
  await enter(user, generation.getByLabelText(/Base time/), "2026-01-02T03:04:05Z");
  await user.selectOptions(generation.getByLabelText("Generator version"), "readmit-corpus-v1");
  await user.selectOptions(generation.getByLabelText("Profile version"), "readmit-siu-v1");
  await enter(user, generation.getByLabelText("Message count"), "3000");
  await user.selectOptions(generation.getByLabelText("Framing"), "mllp");
  await user.selectOptions(generation.getByLabelText("Segment terminator"), "cr");
  await user.selectOptions(generation.getByLabelText("Encoding"), "us-ascii");
  await user.selectOptions(generation.getByLabelText("Direction"), "inbound");
  await journey.chooseFolder(journey.path("corpora"), "Choose the folder for the new corpus and its manifest");
  await press(user, generation.getByRole("button", { name: "Choose destination…" }));
  expect(await generation.findByText(journey.path("corpora"))).toBeTruthy();
  await press(user, generation.getByRole("button", { name: "Generate corpus" }));
  const written = within(await generation.findByLabelText("Written corpus"));
  expect(written.getByText("3000")).toBeTruthy();

  const generated = await journey.commandLine([
    "--operation-policy", journey.path("vendor-delivered-license", "operation-policy.json"),
    "corpus", "generate", "--output", "command/corpus.mllp", "--manifest", "command/corpus.json",
    "--seed", "7", "--base-time", "2026-01-02T03:04:05Z",
    "--generator-version", "readmit-corpus-v1", "--profile-version", "readmit-siu-v1", "--messages", "3000",
    "--framing", "mllp", "--terminator", "cr", "--encoding", "us-ascii", "--direction", "inbound",
  ]);
  expect(generated.code).toBe(0);
  expect(journey.digest("corpora/corpus.mllp")).toBe(journey.digest("command/corpus.mllp"));
  expect(journey.readFile("corpora/corpus.json")).toBe(journey.readFile("command/corpus.json"));
  expect(written.getByText(journey.digest("corpora/corpus.mllp"))).toBeTruthy();

  // Scanning the corpus the window wrote, with a window and a benchmark.
  await press(user, screen.getByRole("tab", { name: "Scan" }));
  const scanning = within(screen.getByRole("tabpanel", { name: "Scan" }));
  await journey.chooseFiles([journey.path("corpora", "corpus.mllp")], "Choose the stream to scan");
  await press(user, scanning.getByRole("button", { name: "Browse…" }));
  // The declarations are offered again once the dialog has answered.
  expect(await scanning.findByText(journey.path("corpora", "corpus.mllp"))).toBeTruthy();
  await user.selectOptions(scanning.getByLabelText("Framing"), "mllp");
  await user.selectOptions(scanning.getByLabelText("Segment terminator"), "cr");
  await user.selectOptions(scanning.getByLabelText("Encoding"), "us-ascii");
  await user.selectOptions(scanning.getByLabelText("Direction"), "inbound");
  await user.click(scanning.getByText("Advanced scan limits"));
  await enter(user, scanning.getByLabelText(/Records per batch/), "64");
  await enter(user, scanning.getByLabelText("Preview record offset"), "1500");
  await enter(user, scanning.getByLabelText("Preview records"), "3");
  await user.click(scanning.getByLabelText("Save benchmark"));
  await journey.chooseFolder(journey.path("benchmarks"), "Choose the folder for the new benchmark");
  await press(user, scanning.getByRole("button", { name: "Choose a folder for the benchmark…" }));
  expect(await scanning.findByText(journey.path("benchmarks"))).toBeTruthy();
  await enter(user, scanning.getByLabelText("Benchmark filename"), "window.json");
  await press(user, scanning.getByRole("button", { name: "Scan stream" }));
  const report = within(await scanning.findByLabelText("Scan report"));
  expect(await scanning.findByText(`Benchmark written to ${journey.path("benchmarks", "window.json")}`)).toBeTruthy();

  const scanned = await journey.commandLine([
    "corpus", "scan", "corpora/corpus.mllp", "--framing", "mllp", "--terminator", "cr", "--encoding", "us-ascii",
    "--direction", "inbound", "--batch-records", "64", "--window-offset", "1500", "--window-limit", "3",
    "--report", "benchmarks/command.json",
  ]);
  expect(scanned.code).toBe(0);
  // Each count the command prints is the one the window states beside it.
  const printed = (name: string) => new RegExp(`^${name}: (.*)$`, "m").exec(scanned.stdout)?.[1];
  const stated = (name: string) => report.getByText(name, { selector: "dt" }).nextElementSibling?.textContent;
  for (const [line, fact] of [
    ["Bytes", "Bytes read"], ["Digest", "Digest"], ["Records", "Records"], ["Occurrences", "Occurrences"],
    ["Decoded", "Decoded"], ["Undecodable", "Undecodable"], ["Parsing batches", "Parsing batches"],
    ["Batch bounds", "Batch bounds"], ["Case bounds", "Case bounds"], ["Targets", "Targets"],
  ] as const) {
    expect(stated(fact)).toBe(printed(line));
  }
  expect(stated("Peak resident bytes")).toBe(`${printed("Peak resident bytes")} of a resident bound of ${printed("Resident bound")}`);
  const window = within(report.getByRole("table", { name: "3 records from offset 1500 of 3000" }));
  for (const line of scanned.stdout.split("\n").filter((row) => row.startsWith("  record "))) {
    const ordinal = /^ {2}record (\d+) /.exec(line)?.[1] ?? "";
    expect(window.getByRole("rowheader", { name: ordinal })).toBeTruthy();
  }
  const benchmark = (name: string) => {
    const document = JSON.parse(journey.readFile(`benchmarks/${name}`));
    delete document.measured.elapsed_milliseconds;
    return document;
  };
  expect(benchmark("window.json")).toEqual(benchmark("command.json"));
  expect(journey.digest("corpora/corpus.mllp")).toBe(journey.digest("command/corpus.mllp"));
});
