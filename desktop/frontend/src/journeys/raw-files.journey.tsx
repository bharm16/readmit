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
import { enter, Journey, press } from "../testkit/journey";
import { activateLicense } from "./steps";

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

  // Values appear only once a person asks for them.
  await press(user, table.querySelector<HTMLElement>('[data-row-id="0"]')!);
  const details = within(await screen.findByRole("region", { name: "Message details" }));
  expect(details.queryByText(/EXAMPLE\^CHARLIE/)).toBeNull();
  await press(user, details.getByRole("button", { name: /^PID/ }));
  await press(user, details.getByRole("button", { name: "Show values" }));
  expect((await details.findAllByText(/EXAMPLE\^CHARLIE/)).length).toBeGreaterThan(0);

  // A byte-identical copy goes to a new file the person named.
  await journey.nameNewFolder(journey.path("copies", "window.mllp"), "Save copy");
  await press(user, screen.getByRole("button", { name: "More file actions" }));
  await press(user, screen.getByRole("menuitem", { name: "Save copy…" }));
  expect(await screen.findByText("Saved window.mllp")).toBeTruthy();
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
