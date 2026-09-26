import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { UserEvent } from "@testing-library/user-event";
import { expect, test } from "vitest";
import type { CorpusScanView, CorpusGenerateRequest, ImportPlan } from "./bindings";
import { PerformanceCorpus } from "./PerformanceCorpus";
import { renderApp } from "./testkit/app";
import { WORKSPACE_ROOT } from "./testkit/fixtures";
import { expectTabPattern } from "./testkit/tabs";
import { installFacade, type FacadeHandlers } from "./testkit/wails";

const FOLDER = `${WORKSPACE_ROOT}/corpora`;
const STREAM = `${WORKSPACE_ROOT}/corpora/corpus.mllp`;
const DIGEST = "a".repeat(64);
const TARGETS = "engineering targets, not measurements or customer requirements";

const plan: ImportPlan = {
  schema: "readmit-import-plan/v1",
  framing: "mllp",
  terminator: "cr",
  encoding: "us-ascii",
  direction: "inbound",
  members: [],
};

/** A scan the way the facade reports one: counts, bounds and positions. */
function scanView(extra: Partial<CorpusScanView> = {}): CorpusScanView {
  return {
    plan,
    bytes: 91200,
    sha256: DIGEST,
    records: 300,
    occurrences: 300,
    decoded: 299,
    undecodable: 1,
    batches: 43,
    peak_resident_bytes: 65536,
    bounds: { batch_records: 7, batch_bytes: 8388608, record_bytes: 16777216, resident_bound: 42008576 },
    case_bounds: "exceeded",
    exceeded: ["sources (128)"],
    window_offset: 150,
    window_limit: 4,
    rows: [151, 152, 153, 154].map((ordinal) => ({
      ordinal,
      offset: ordinal * 304,
      size: 304,
      occurrences: 1,
      decoded: ordinal === 153 ? 0 : 1,
      undecodable: ordinal === 153 ? 1 : 0,
    })),
    elapsed_milliseconds: 12,
    targets: TARGETS,
    ...extra,
  };
}

function handlers(extra: FacadeHandlers = {}): FacadeHandlers {
  return {
    ChooseCorpusPath: (kind) => ({ state: "completed", kind, path: kind === "scan-file" ? STREAM : FOLDER }),
    CorpusProgress: () => ({ state: "empty" }),
    GenerateCorpus: (request) => ({
      state: "completed",
      corpus: `${request.folder}/${request.corpus_name}`,
      manifest_path: `${request.folder}/${request.manifest_name}`,
      manifest: {
        schema: "readmit-corpus/v1",
        seed: request.seed,
        base_time: "2026-01-02T03:04:05Z",
        generator_version: request.generator_version,
        profile_version: request.profile_version,
        messages: request.messages,
        plan: request.plan,
        bytes: 1520000,
        sha256: DIGEST,
      },
    }),
    ScanCorpus: (request) => ({
      state: "completed",
      scan: scanView(),
      ...(request.report_folder ? { benchmark: `${request.report_folder}/${request.report_name}` } : {}),
    }),
    ...extra,
  };
}

async function openScreen(user: UserEvent) {
  const toggle = screen.getByRole("button", { name: "Performance corpus" });
  expect(toggle.getAttribute("aria-expanded")).toBe("false");
  await user.click(toggle);
  expect(toggle.getAttribute("aria-expanded")).toBe("true");
}

const generation = () => within(screen.getByRole("tabpanel", { name: "Generate" }));
const scanning = () => within(screen.getByRole("tabpanel", { name: "Scan" }));

/** Shows one corpus task; the other keeps what was typed into it. */
async function show(user: UserEvent, task: "Generate" | "Scan") {
  await user.click(screen.getByRole("tab", { name: task }));
  expect(screen.getByRole("tab", { name: task }).getAttribute("aria-selected")).toBe("true");
}

/** Reveals the batch-bound overrides of a scan. */
async function advanced(user: UserEvent) {
  const summary = scanning().getByText("Advanced scan limits");
  if (!summary.closest("details")?.open) {
    await user.click(summary);
  }
}

async function declareGeneration(user: UserEvent, seed = "18446744073709551615") {
  await show(user, "Generate");
  const part = generation();
  await user.type(part.getByLabelText("Seed"), seed);
  await user.type(part.getByLabelText(/Base time/), "2026-01-02T03:04:05Z");
  await user.selectOptions(part.getByLabelText("Generator version"), "readmit-corpus-v1");
  await user.selectOptions(part.getByLabelText("Profile version"), "readmit-siu-v1");
  await user.type(part.getByLabelText("Message count"), "5000");
  await user.selectOptions(part.getByLabelText("Framing"), "batch");
  await user.selectOptions(part.getByLabelText("Batch boundary"), "segment-start");
  await user.selectOptions(part.getByLabelText("Segment terminator"), "cr");
  await user.selectOptions(part.getByLabelText("Encoding"), "utf-8");
  await user.selectOptions(part.getByLabelText("Direction"), "inbound");
  await user.click(part.getByRole("button", { name: "Choose destination…" }));
  expect(await part.findByText(FOLDER)).toBeTruthy();
}

async function declareScan(user: UserEvent) {
  await show(user, "Scan");
  const part = scanning();
  await user.click(part.getByRole("button", { name: "Browse…" }));
  expect(await part.findByText(STREAM)).toBeTruthy();
  await user.selectOptions(part.getByLabelText("Framing"), "mllp");
  await user.selectOptions(part.getByLabelText("Segment terminator"), "cr");
  await user.selectOptions(part.getByLabelText("Encoding"), "us-ascii");
  await user.selectOptions(part.getByLabelText("Direction"), "inbound");
}

test("generating a corpus takes every declaration from structured controls and reports the manifest it wrote", async () => {
  const facade = installFacade(handlers());
  const user = userEvent.setup();
  render(<PerformanceCorpus busy={false} indicators={new Map()} request={0} />);
  await openScreen(user);
  const generate = generation().getByRole("button", { name: "Generate corpus" });
  expect((generate as HTMLButtonElement).disabled).toBe(true);
  // A batch boundary is declared only for batch framing.
  expect(generation().queryByLabelText("Batch boundary")).toBeNull();
  await declareGeneration(user);
  expect(facade.oneCall("ChooseCorpusPath")).toEqual(["corpus-folder"]);
  expect((generate as HTMLButtonElement).disabled).toBe(false);
  // The two new files are named in the chosen folder; neither may be empty.
  await user.clear(generation().getByLabelText("Corpus filename"));
  expect((generate as HTMLButtonElement).disabled).toBe(true);
  await user.type(generation().getByLabelText("Corpus filename"), "siu.mllp");
  await user.clear(generation().getByLabelText("Manifest filename"));
  await user.type(generation().getByLabelText("Manifest filename"), "siu.json");
  await user.click(generate);

  const facts = within(await screen.findByLabelText("Written corpus"));
  expect(facts.getByText(`${FOLDER}/siu.mllp`)).toBeTruthy();
  expect(facts.getByText(`${FOLDER}/siu.json (readmit-corpus/v1)`)).toBeTruthy();
  // The seed survives whole, past what a JavaScript number can hold.
  expect(facts.getByText("18446744073709551615")).toBeTruthy();
  expect(facts.getByText(DIGEST)).toBeTruthy();
  expect(facts.getByText("5000")).toBeTruthy();
  const [request] = facade.oneCall("GenerateCorpus") as [CorpusGenerateRequest];
  expect(request).toEqual({
    seed: "18446744073709551615",
    base_time: "2026-01-02T03:04:05Z",
    generator_version: "readmit-corpus-v1",
    profile_version: "readmit-siu-v1",
    messages: 5000,
    plan: {
      schema: "readmit-import-plan/v1",
      framing: "batch",
      batch_boundary: "segment-start",
      terminator: "cr",
      encoding: "utf-8",
      direction: "inbound",
      members: [],
    },
    folder: FOLDER,
    corpus_name: "siu.mllp",
    manifest_name: "siu.json",
  });
  expect(facade.callsTo("ScanCorpus")).toHaveLength(0);
});

test("a running generation shows progress and a cancelled one says no corpus or manifest remains", async () => {
  const facade = installFacade(
    handlers({
      CorpusProgress: () => ({
        state: "completed",
        progress: { operation: "generate", messages: 4096, bytes: 1245184, records: 0, occurrences: 0, batches: 0 },
      }),
    }),
  );
  const parked = facade.park("GenerateCorpus");
  const user = userEvent.setup();
  render(<PerformanceCorpus busy={false} indicators={new Map()} request={0} />);
  await openScreen(user);
  await declareGeneration(user, "7");
  await user.click(generation().getByRole("button", { name: "Generate corpus" }));
  expect(await screen.findByText("Generated so far: 4096 messages, 1245184 bytes")).toBeTruthy();
  expect(screen.getByText("Generating the corpus.")).toBeTruthy();
  expect((generation().getByRole("button", { name: "Generate corpus" }) as HTMLButtonElement).disabled).toBe(true);
  // Changing to the other task neither cancels the generation nor hides it:
  // its named status and cancellation stay in view, and the scan cannot start.
  await show(user, "Scan");
  expect((scanning().getByRole("button", { name: "Scan stream" }) as HTMLButtonElement).disabled).toBe(true);
  const status = within(screen.getByRole("group", { name: "Generation running" }));
  expect(status.getByText("Generating the corpus.")).toBeTruthy();
  expect(status.getByText("Generated so far: 4096 messages, 1245184 bytes")).toBeTruthy();
  expect(facade.callsTo("Cancel")).toHaveLength(0);
  await user.click(status.getByRole("button", { name: "Cancel generation" }));
  expect(facade.oneCall("Cancel")).toEqual(["corpus"]);
  parked.resolve({
    state: "cancelled",
    reason: "the generation was cancelled; the partial corpus was removed and no manifest was written",
  });
  await waitFor(() => expect(screen.queryByRole("group", { name: "Generation running" })).toBeNull());
  // The outcome stays with its task.
  await show(user, "Generate");
  expect(
    await generation().findByText("the generation was cancelled; the partial corpus was removed and no manifest was written"),
  ).toBeTruthy();
  expect(screen.getByText("cancelled")).toBeTruthy();
  expect(screen.queryByLabelText("Written corpus")).toBeNull();
  expect(screen.queryByText(/Generated so far/)).toBeNull();
  expect(screen.queryByRole("button", { name: "Cancel generation" })).toBeNull();
  expect((generation().getByRole("button", { name: "Generate corpus" }) as HTMLButtonElement).disabled).toBe(false);
});

test("scanning a stream reports the command's counts, case bounds, window and benchmark", async () => {
  const facade = installFacade(handlers());
  const user = userEvent.setup();
  render(<PerformanceCorpus busy={false} indicators={new Map()} request={0} />);
  await openScreen(user);
  await show(user, "Scan");
  const scan = scanning().getByRole("button", { name: "Scan stream" });
  expect((scan as HTMLButtonElement).disabled).toBe(true);
  await declareScan(user);
  const part = scanning();
  await advanced(user);
  await user.type(part.getByLabelText(/Records per batch/), "7");
  await user.type(part.getByLabelText(/Bytes per batch/), "4194304");
  await user.clear(part.getByLabelText("Preview record offset"));
  await user.type(part.getByLabelText("Preview record offset"), "150");
  await user.clear(part.getByLabelText("Preview records"));
  await user.type(part.getByLabelText("Preview records"), "4");
  await user.click(part.getByLabelText(/Save benchmark/));
  // A benchmark needs its own new destination before a scan may start.
  expect((scan as HTMLButtonElement).disabled).toBe(true);
  await user.click(part.getByRole("button", { name: "Choose a folder for the benchmark…" }));
  expect(await part.findByText(FOLDER)).toBeTruthy();
  await user.clear(part.getByLabelText("Benchmark filename"));
  expect((scan as HTMLButtonElement).disabled).toBe(true);
  await user.type(part.getByLabelText("Benchmark filename"), "benchmark.json");
  await user.click(scan);

  const report = within(await screen.findByLabelText("Scan report"));
  expect(facade.oneCall("ScanCorpus")).toEqual([
    {
      file: STREAM,
      plan,
      batch_records: 7,
      batch_bytes: 4194304,
      window_offset: 150,
      window_limit: 4,
      report_folder: FOLDER,
      report_name: "benchmark.json",
    },
  ]);
  expect(report.getByText("completed")).toBeTruthy();
  expect(report.getByText("exceeded sources (128)")).toBeTruthy();
  expect(report.getByText("7 records, 8388608 bytes")).toBeTruthy();
  expect(report.getByText("65536 of a resident bound of 42008576")).toBeTruthy();
  expect(report.getByText(TARGETS)).toBeTruthy();
  const window = within(report.getByRole("table", { name: "4 records from offset 150 of 300" }));
  expect(window.getAllByRole("row")).toHaveLength(5);
  expect(window.getByRole("rowheader", { name: "153" })).toBeTruthy();
  expect(screen.getByText(`Benchmark written to ${FOLDER}/benchmark.json`)).toBeTruthy();

  // The report belongs to the stream and declarations it was scanned under:
  // changing one clears it rather than leaving it beside choices it does not
  // describe.
  await user.clear(part.getByLabelText("Preview records"));
  await user.type(part.getByLabelText("Preview records"), "5");
  expect(screen.queryByLabelText("Scan report")).toBeNull();
  expect(screen.queryByText(/Benchmark written to/)).toBeNull();
  await user.click(scan);
  expect(await screen.findByLabelText("Scan report")).toBeTruthy();
  await user.click(part.getByRole("button", { name: "Browse…" }));
  await waitFor(() => expect(screen.queryByLabelText("Scan report")).toBeNull());
});

test("a running scan shows progress, cancels on request and reports the counts it reached without a benchmark", async () => {
  const facade = installFacade(
    handlers({
      CorpusProgress: () => ({
        state: "completed",
        progress: { operation: "scan", messages: 0, bytes: 155648, records: 512, occurrences: 512, batches: 2 },
      }),
    }),
  );
  const parked = facade.park("ScanCorpus");
  const user = userEvent.setup();
  render(<PerformanceCorpus busy={false} indicators={new Map()} request={0} />);
  await openScreen(user);
  await declareScan(user);
  await user.click(scanning().getByLabelText(/Save benchmark/));
  await user.click(scanning().getByRole("button", { name: "Choose a folder for the benchmark…" }));
  await scanning().findByText(FOLDER);
  await user.click(scanning().getByRole("button", { name: "Scan stream" }));
  expect(
    await screen.findByText("Scanned so far: 155648 bytes, 512 records, 512 occurrences, 2 parsing batches"),
  ).toBeTruthy();
  expect(facade.callsTo("CorpusProgress").length).toBeGreaterThan(0);
  await user.click(screen.getByRole("button", { name: "Cancel scan" }));
  expect(facade.oneCall("Cancel")).toEqual(["corpus"]);
  parked.resolve({
    state: "cancelled",
    reason: "the scan was cancelled; these are the counts it reached, the case bounds were not evaluated and no benchmark was written",
    scan: scanView({ records: 512, occurrences: 512, decoded: 512, undecodable: 0, batches: 2, case_bounds: "not-evaluated", exceeded: [], window_limit: 0, rows: [] }),
  });
  const report = within(await screen.findByLabelText("Scan report"));
  expect(report.getByText("cancelled")).toBeTruthy();
  expect(report.getByText("not evaluated; the scan was cancelled")).toBeTruthy();
  expect(report.getAllByText("512").length).toBeGreaterThan(0);
  expect(screen.getByText(/the case bounds were not evaluated and no benchmark was written/)).toBeTruthy();
  expect(screen.queryByText(/Benchmark written to/)).toBeNull();
  expect(screen.queryByText(/Scanned so far/)).toBeNull();
  expect(screen.queryByRole("button", { name: "Cancel scan" })).toBeNull();
  // Polling stops with the scan.
  const polls = facade.callsTo("CorpusProgress").length;
  await new Promise((resolve) => setTimeout(resolve, 400));
  expect(facade.callsTo("CorpusProgress").length).toBe(polls);
});

test("corpus refusals, a denied stream and a dismissed dialog leave the screen usable", async () => {
  const facade = installFacade(
    handlers({
      ChooseCorpusPath: () => ({ state: "cancelled", reason: "no folder was chosen" }),
      GenerateCorpus: () => ({ state: "failed", reason: "a corpus holds between 1 and 1048576 messages" }),
    }),
  );
  const user = userEvent.setup();
  render(<PerformanceCorpus busy={false} indicators={new Map()} request={0} />);
  await openScreen(user);
  await user.click(generation().getByRole("button", { name: "Choose destination…" }));
  expect(await screen.findByText("no folder was chosen")).toBeTruthy();
  expect(generation().getByText("No folder chosen.")).toBeTruthy();

  facade.reply(handlers({ GenerateCorpus: () => ({ state: "failed", reason: "a corpus holds between 1 and 1048576 messages" }) }));
  await declareGeneration(user, "7");
  // A count that is not a plain whole number cannot be sent at all: not a
  // unit, not a leading zero the command would read as octal, and not a
  // number a JavaScript number cannot hold exactly.
  for (const typed of ["5k", "010", "9007199254740993"]) {
    await user.clear(generation().getByLabelText("Message count"));
    await user.type(generation().getByLabelText("Message count"), typed);
    expect((generation().getByRole("button", { name: "Generate corpus" }) as HTMLButtonElement).disabled).toBe(true);
  }
  await user.clear(generation().getByLabelText("Message count"));
  await user.type(generation().getByLabelText("Message count"), "0");
  await user.click(generation().getByRole("button", { name: "Generate corpus" }));
  expect(await screen.findByText("a corpus holds between 1 and 1048576 messages")).toBeTruthy();
  expect(screen.queryByLabelText("Written corpus")).toBeNull();

  facade.reply({ ScanCorpus: () => ({ state: "permission_denied", reason: "cannot open the declared stream" }) });
  await declareScan(user);
  await user.click(scanning().getByRole("button", { name: "Scan stream" }));
  expect(await screen.findByText("cannot open the declared stream")).toBeTruthy();
  expect(screen.getByText("permission_denied")).toBeTruthy();
  expect(screen.queryByLabelText("Scan report")).toBeNull();

  // A benchmark that could not be written still reports the scan it measured.
  facade.reply({
    ScanCorpus: () => ({ state: "failed", reason: "cannot create the benchmark; destination must be new and writable", scan: scanView() }),
  });
  await user.click(scanning().getByRole("button", { name: "Scan stream" }));
  expect(await screen.findByText("cannot create the benchmark; destination must be new and writable")).toBeTruthy();
  expect(within(screen.getByLabelText("Scan report")).getByText("exceeded sources (128)")).toBeTruthy();
  expect(screen.queryByText(/Benchmark written to/)).toBeNull();
});

test("the palette opens the benchmarks screen and a running scan is cancelled from its own control, never by Escape", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(handlers());
  const parked = facade.park("ScanCorpus");
  await user.keyboard("{Control>}k{/Control}");
  await user.type(screen.getByRole("combobox", { name: "Search commands" }), "Benchmarks{Enter}");
  const toggle = screen.getByRole("button", { name: "Performance corpus" });
  expect(toggle.getAttribute("aria-expanded")).toBe("true");
  expect(document.activeElement).toBe(toggle);

  // The corpus tasks are one tab stop: the selected task's tab, whose arrow
  // keys move to the other.
  const tab = screen.getByRole("tab", { name: "Generate" });
  for (let step = 0; step < 40 && document.activeElement !== tab; step++) {
    await user.tab();
  }
  expect(document.activeElement).toBe(tab);
  await user.keyboard("{ArrowRight}");
  expect(document.activeElement).toBe(screen.getByRole("tab", { name: "Scan", selected: true }));
  const choose = scanning().getByRole("button", { name: "Browse…" });
  for (let step = 0; step < 40 && document.activeElement !== choose; step++) {
    await user.tab();
  }
  expect(document.activeElement).toBe(choose);
  await user.keyboard("{Enter}");
  expect(await scanning().findByText(STREAM)).toBeTruthy();
  const part = scanning();
  await user.selectOptions(part.getByLabelText("Framing"), "mllp");
  await user.selectOptions(part.getByLabelText("Segment terminator"), "cr");
  await user.selectOptions(part.getByLabelText("Encoding"), "us-ascii");
  await user.selectOptions(part.getByLabelText("Direction"), "inbound");
  const scan = part.getByRole("button", { name: "Scan stream" });
  for (let step = 0; step < 40 && document.activeElement !== scan; step++) {
    await user.tab();
  }
  expect(document.activeElement).toBe(scan);
  await user.keyboard("{Enter}");
  expect(await screen.findByText("Scanning the stream.")).toBeTruthy();
  // While the scan holds the facade, the rest of the window is unavailable
  // rather than answered busy.
  expect((screen.getByRole("button", { name: "Back to tools" }) as HTMLButtonElement).disabled).toBe(false);
  await user.click(screen.getByRole("button", { name: "Tools" }));
  expect((screen.getByRole("button", { name: "Inspect file" }) as HTMLButtonElement).disabled).toBe(false);
  await user.click(screen.getByRole("button", { name: "Benchmarks" }));
  await user.keyboard("{Escape}");
  expect(facade.callsTo("Cancel")).toEqual([]);
  await user.click(screen.getByRole("button", { name: "Cancel scan" }));
  expect(facade.callsTo("Cancel").map((call) => call.args[0])).toEqual(["corpus"]);
  parked.resolve({
    state: "cancelled",
    reason: "the scan was cancelled; these are the counts it reached, the case bounds were not evaluated and no benchmark was written",
    scan: scanView({ case_bounds: "not-evaluated", exceeded: [], window_limit: 0, rows: [] }),
  });
  expect(await screen.findByText("not evaluated; the scan was cancelled")).toBeTruthy();
  await waitFor(() => expect(screen.queryByRole("button", { name: "Cancel scan" })).toBeNull());
});

const offered = (select: HTMLElement) =>
  [...(select as HTMLSelectElement).options].map((option) => [option.value, option.text]);

test("the corpus tasks follow the tabs pattern: one tab stop, arrow keys, Home and End, and each tab controls its rendered panel", async () => {
  installFacade(handlers());
  const user = userEvent.setup();
  render(<PerformanceCorpus busy={false} indicators={new Map()} request={0} />);
  await openScreen(user);
  const list = screen.getByRole("tablist", { name: "Corpus tasks" });
  expectTabPattern(list);
  screen.getByRole("tab", { name: "Generate" }).focus();
  await user.keyboard("{ArrowRight}");
  expect(document.activeElement).toBe(screen.getByRole("tab", { name: "Scan", selected: true }));
  expect(scanning().getByRole("heading", { name: "Scan stream" })).toBeTruthy();
  expectTabPattern(list);
  await user.keyboard("{ArrowRight}");
  expect(document.activeElement).toBe(screen.getByRole("tab", { name: "Generate", selected: true }));
  await user.keyboard("{End}");
  expect(document.activeElement).toBe(screen.getByRole("tab", { name: "Scan", selected: true }));
  await user.keyboard("{Home}");
  expect(document.activeElement).toBe(screen.getByRole("tab", { name: "Generate", selected: true }));
  await user.keyboard("{ArrowLeft}");
  expect(document.activeElement).toBe(screen.getByRole("tab", { name: "Scan", selected: true }));
  expectTabPattern(list);
});

test("generate and scan are separate tasks shown one at a time, each keeping its unsaved inputs", async () => {
  const facade = installFacade(handlers());
  const user = userEvent.setup();
  render(<PerformanceCorpus busy={false} indicators={new Map()} request={0} />);
  await openScreen(user);
  expect(screen.getByRole("tab", { name: "Generate" }).getAttribute("aria-selected")).toBe("true");
  expect(screen.getByRole("heading", { name: "Generate corpus" })).toBeTruthy();
  expect(screen.queryByRole("tabpanel", { name: "Scan" })).toBeNull();
  await user.type(generation().getByLabelText("Seed"), "42");
  await user.type(generation().getByLabelText("Message count"), "12");

  await show(user, "Scan");
  expect(screen.queryByRole("tabpanel", { name: "Generate" })).toBeNull();
  expect(screen.getByRole("heading", { name: "Scan stream" })).toBeTruthy();
  await user.clear(scanning().getByLabelText("Preview records"));
  await user.type(scanning().getByLabelText("Preview records"), "0");

  await show(user, "Generate");
  expect((generation().getByLabelText("Seed") as HTMLInputElement).value).toBe("42");
  expect((generation().getByLabelText("Message count") as HTMLInputElement).value).toBe("12");
  await show(user, "Scan");
  expect((scanning().getByLabelText("Preview records") as HTMLInputElement).value).toBe("0");
  // Moving between tasks starts nothing.
  expect(facade.callsTo("GenerateCorpus")).toHaveLength(0);
  expect(facade.callsTo("ScanCorpus")).toHaveLength(0);
  expect(facade.callsTo("ChooseCorpusPath")).toHaveLength(0);
});

test("each task offers only its own format vocabulary, captioned in words over the unchanged plan values", async () => {
  const facade = installFacade(handlers());
  const user = userEvent.setup();
  render(<PerformanceCorpus busy={false} indicators={new Map()} request={0} />);
  await openScreen(user);
  const output = within(generation().getByRole("group", { name: "Output format" }));
  // Generation never offers raw framing, a second terminator or an unknown
  // encoding: it writes only what a corpus can be written with.
  expect(offered(output.getByLabelText("Framing"))).toEqual([["", "Choose…"], ["mllp", "MLLP frames"], ["batch", "Batch"]]);
  expect(offered(output.getByLabelText("Segment terminator"))).toEqual([["", "Choose…"], ["cr", "CR"]]);
  expect(offered(output.getByLabelText("Encoding"))).toEqual([["", "Choose…"], ["us-ascii", "US-ASCII"], ["utf-8", "UTF-8"]]);
  expect(offered(output.getByLabelText("Direction"))).toEqual([
    ["", "Choose…"], ["inbound", "Inbound"], ["outbound", "Outbound"], ["unknown", "Unknown"],
  ]);
  await user.selectOptions(output.getByLabelText("Framing"), "Batch");
  expect(offered(output.getByLabelText("Batch boundary"))).toEqual([["", "Choose…"], ["segment-start", "Segment start"]]);
  // The reproducibility pins are chosen, never preselected.
  expect((generation().getByLabelText("Generator version") as HTMLSelectElement).value).toBe("");
  expect((generation().getByLabelText("Profile version") as HTMLSelectElement).value).toBe("");
  // The base time's format requirements stay beside it, with no default.
  const base = generation().getByLabelText("Base time") as HTMLInputElement;
  expect(base.value).toBe("");
  expect(document.getElementById(base.getAttribute("aria-describedby") ?? "")?.textContent).toMatch(
    /Whole-second RFC 3339 with a time zone/,
  );

  await show(user, "Scan");
  const input = within(scanning().getByRole("group", { name: "Input format" }));
  expect(offered(input.getByLabelText("Framing"))).toEqual([
    ["", "Choose…"], ["raw", "Raw HL7"], ["mllp", "MLLP frames"], ["batch", "Batch"],
  ]);
  expect(offered(input.getByLabelText("Segment terminator"))).toEqual([["", "Choose…"], ["cr", "CR"], ["lf", "LF"], ["crlf", "CRLF"]]);
  expect(offered(input.getByLabelText("Encoding"))).toEqual([
    ["", "Choose…"], ["utf-8", "UTF-8"], ["us-ascii", "US-ASCII"], ["iso-8859-1", "ISO-8859-1"], ["unknown", "Unknown"],
  ]);
  await user.selectOptions(input.getByLabelText("Framing"), "Batch");
  expect(offered(input.getByLabelText("Batch boundary"))).toEqual([
    ["", "Choose…"], ["segment-start", "Segment start"], ["hl7-batch", "HL7 batch"],
  ]);
  await user.click(scanning().getByRole("button", { name: "Browse…" }));
  await scanning().findByText(STREAM);
  await user.selectOptions(input.getByLabelText("Batch boundary"), "HL7 batch");
  await user.selectOptions(input.getByLabelText("Segment terminator"), "CRLF");
  await user.selectOptions(input.getByLabelText("Encoding"), "ISO-8859-1");
  await user.selectOptions(input.getByLabelText("Direction"), "Unknown");
  await user.click(scanning().getByRole("button", { name: "Scan stream" }));
  await screen.findByLabelText("Scan report");
  expect(facade.oneCall("ScanCorpus")[0]).toMatchObject({
    plan: {
      schema: "readmit-import-plan/v1",
      framing: "batch",
      batch_boundary: "hl7-batch",
      terminator: "crlf",
      encoding: "iso-8859-1",
      direction: "unknown",
      members: [],
    },
  });
});

test("a scan keeps its preview in the normal flow, batch bounds under advanced limits and benchmark fields until asked for", async () => {
  const facade = installFacade(handlers());
  const user = userEvent.setup();
  render(<PerformanceCorpus busy={false} indicators={new Map()} request={0} />);
  await openScreen(user);
  await declareScan(user);
  const limits = within(scanning().getByRole("group", { name: "Scan limits and preview" }));
  const preview = limits.getByLabelText("Preview records") as HTMLInputElement;
  expect(preview.value).toBe("20");
  expect(document.getElementById(preview.getAttribute("aria-describedby") ?? "")?.textContent).toBe(
    "0 shows counts only; maximum 200.",
  );
  expect((limits.getByLabelText("Preview record offset") as HTMLInputElement).value).toBe("0");
  // The batch bounds are behind their own disclosure, and their defaults are
  // stated where the fields are.
  const details = limits.getByText("Advanced scan limits").closest("details")!;
  expect(details.open).toBe(false);
  await advanced(user);
  expect(details.open).toBe(true);
  for (const [label, hint] of [
    ["Records per batch", "Empty uses 256 records."],
    ["Bytes per batch", "Empty uses 8,388,608 bytes."],
  ] as const) {
    const field = limits.getByLabelText(label) as HTMLInputElement;
    expect(field.value).toBe("");
    expect(document.getElementById(field.getAttribute("aria-describedby") ?? "")?.textContent).toBe(hint);
  }
  // A malformed bound is refused, not replaced with a guess.
  await user.type(limits.getByLabelText("Bytes per batch"), "8MB");
  expect((scanning().getByRole("button", { name: "Scan stream" }) as HTMLButtonElement).disabled).toBe(true);
  await user.clear(limits.getByLabelText("Bytes per batch"));

  const benchmark = within(scanning().getByRole("group", { name: "Benchmark output" }));
  expect(benchmark.queryByLabelText("Benchmark filename")).toBeNull();
  expect(benchmark.queryByRole("button", { name: "Choose a folder for the benchmark…" })).toBeNull();
  await user.click(benchmark.getByLabelText("Save benchmark"));
  expect(benchmark.getByLabelText("Benchmark filename")).toBeTruthy();
  await user.click(benchmark.getByLabelText("Save benchmark"));
  expect(benchmark.queryByLabelText("Benchmark filename")).toBeNull();

  // Zero preview records is counts only; empty bounds are sent as the
  // backend's own defaults.
  await user.clear(preview);
  await user.type(preview, "0");
  await user.click(scanning().getByRole("button", { name: "Scan stream" }));
  await screen.findByLabelText("Scan report");
  expect(facade.oneCall("ScanCorpus")[0]).toMatchObject({ batch_records: 0, batch_bytes: 0, window_offset: 0, window_limit: 0 });
  expect(facade.oneCall("ScanCorpus")[0]).not.toHaveProperty("report_folder");
});

test("closing the screen while a scan runs keeps its named status and cancellation in view", async () => {
  const facade = installFacade(
    handlers({
      CorpusProgress: () => ({
        state: "completed",
        progress: { operation: "scan", messages: 0, bytes: 1024, records: 3, occurrences: 3, batches: 1 },
      }),
    }),
  );
  const parked = facade.park("ScanCorpus");
  const user = userEvent.setup();
  render(<PerformanceCorpus busy={false} indicators={new Map()} request={0} />);
  await openScreen(user);
  await declareScan(user);
  await user.click(scanning().getByRole("button", { name: "Scan stream" }));
  await screen.findByText("Scanned so far: 1024 bytes, 3 records, 3 occurrences, 1 parsing batches");
  await user.click(screen.getByRole("button", { name: "Performance corpus" }));
  expect(screen.queryByRole("tabpanel")).toBeNull();
  const status = within(screen.getByRole("group", { name: "Scan running" }));
  expect(status.getByText("Scanning the stream.")).toBeTruthy();
  expect(facade.callsTo("Cancel")).toHaveLength(0);
  await user.click(status.getByRole("button", { name: "Cancel scan" }));
  expect(facade.oneCall("Cancel")).toEqual(["corpus"]);
  parked.resolve({
    state: "cancelled",
    reason: "the scan was cancelled; these are the counts it reached, the case bounds were not evaluated and no benchmark was written",
    scan: scanView({ case_bounds: "not-evaluated", exceeded: [], window_limit: 0, rows: [] }),
  });
  await waitFor(() => expect(screen.queryByRole("group", { name: "Scan running" })).toBeNull());
  // Reopening shows the cancelled scan as cancelled, never as a completed benchmark.
  await user.click(screen.getByRole("button", { name: "Performance corpus" }));
  const report = within(await scanning().findByLabelText("Scan report"));
  expect(report.getByText("cancelled")).toBeTruthy();
  expect(screen.queryByText(/Benchmark written to/)).toBeNull();
});
