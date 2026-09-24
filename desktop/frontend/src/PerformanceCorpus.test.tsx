import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { UserEvent } from "@testing-library/user-event";
import { expect, test } from "vitest";
import type { CorpusScanView, CorpusGenerateRequest, ImportPlan } from "./bindings";
import { PerformanceCorpus } from "./PerformanceCorpus";
import { renderApp } from "./testkit/app";
import { WORKSPACE_ROOT } from "./testkit/fixtures";
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
  const toggle = screen.getByRole("button", { name: "Generate or scan a performance corpus" });
  expect(toggle.getAttribute("aria-expanded")).toBe("false");
  await user.click(toggle);
  expect(toggle.getAttribute("aria-expanded")).toBe("true");
}

const generation = () => within(screen.getByRole("region", { name: "Generate a corpus" }));
const scanning = () => within(screen.getByRole("region", { name: "Scan a stream" }));

async function declareGeneration(user: UserEvent, seed = "18446744073709551615") {
  const part = generation();
  await user.type(part.getByLabelText("Seed"), seed);
  await user.type(part.getByLabelText(/Base time/), "2026-01-02T03:04:05Z");
  await user.selectOptions(part.getByLabelText("Generator version"), "readmit-corpus-v1");
  await user.selectOptions(part.getByLabelText("Profile version"), "readmit-siu-v1");
  await user.type(part.getByLabelText("Messages"), "5000");
  await user.selectOptions(part.getByLabelText("Framing"), "batch");
  await user.selectOptions(part.getByLabelText("Batch boundary"), "segment-start");
  await user.selectOptions(part.getByLabelText("Segment terminator"), "cr");
  await user.selectOptions(part.getByLabelText("Encoding"), "utf-8");
  await user.selectOptions(part.getByLabelText("Direction"), "inbound");
  await user.click(part.getByRole("button", { name: "Choose a folder for the corpus…" }));
  expect(await part.findByText(FOLDER)).toBeTruthy();
}

async function declareScan(user: UserEvent) {
  const part = scanning();
  await user.click(part.getByRole("button", { name: "Choose a stream to scan…" }));
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
  await user.clear(generation().getByLabelText("Corpus file name"));
  expect((generate as HTMLButtonElement).disabled).toBe(true);
  await user.type(generation().getByLabelText("Corpus file name"), "siu.mllp");
  await user.clear(generation().getByLabelText("Manifest file name"));
  await user.type(generation().getByLabelText("Manifest file name"), "siu.json");
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
  expect((scanning().getByRole("button", { name: "Scan stream" }) as HTMLButtonElement).disabled).toBe(true);
  await user.click(generation().getByRole("button", { name: "Cancel generation" }));
  expect(facade.oneCall("Cancel")).toEqual(["corpus"]);
  parked.resolve({
    state: "cancelled",
    reason: "the generation was cancelled; the partial corpus was removed and no manifest was written",
  });
  expect(
    await screen.findByText("the generation was cancelled; the partial corpus was removed and no manifest was written"),
  ).toBeTruthy();
  expect(screen.getByText("cancelled")).toBeTruthy();
  expect(screen.queryByLabelText("Written corpus")).toBeNull();
  expect(screen.queryByText(/Generated so far/)).toBeNull();
  expect(generation().queryByRole("button", { name: "Cancel generation" })).toBeNull();
  expect((generation().getByRole("button", { name: "Generate corpus" }) as HTMLButtonElement).disabled).toBe(false);
});

test("scanning a stream reports the command's counts, case bounds, window and benchmark", async () => {
  const facade = installFacade(handlers());
  const user = userEvent.setup();
  render(<PerformanceCorpus busy={false} indicators={new Map()} request={0} />);
  await openScreen(user);
  const scan = scanning().getByRole("button", { name: "Scan stream" });
  expect((scan as HTMLButtonElement).disabled).toBe(true);
  await declareScan(user);
  const part = scanning();
  await user.type(part.getByLabelText(/Records per parsing batch/), "7");
  await user.type(part.getByLabelText(/Bytes per parsing batch/), "4194304");
  await user.clear(part.getByLabelText("Window offset"));
  await user.type(part.getByLabelText("Window offset"), "150");
  await user.clear(part.getByLabelText(/Window records/));
  await user.type(part.getByLabelText(/Window records/), "4");
  await user.click(part.getByLabelText(/Write a readmit-benchmark\/v1 document/));
  // A benchmark needs its own new destination before a scan may start.
  expect((scan as HTMLButtonElement).disabled).toBe(true);
  await user.click(part.getByRole("button", { name: "Choose a folder for the benchmark…" }));
  expect(await part.findByText(FOLDER)).toBeTruthy();
  await user.clear(part.getByLabelText("Benchmark file name"));
  expect((scan as HTMLButtonElement).disabled).toBe(true);
  await user.type(part.getByLabelText("Benchmark file name"), "benchmark.json");
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
  await user.clear(part.getByLabelText(/Window records/));
  await user.type(part.getByLabelText(/Window records/), "5");
  expect(screen.queryByLabelText("Scan report")).toBeNull();
  expect(screen.queryByText(/Benchmark written to/)).toBeNull();
  await user.click(scan);
  expect(await screen.findByLabelText("Scan report")).toBeTruthy();
  await user.click(part.getByRole("button", { name: "Choose a stream to scan…" }));
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
  await user.click(scanning().getByLabelText(/Write a readmit-benchmark\/v1 document/));
  await user.click(scanning().getByRole("button", { name: "Choose a folder for the benchmark…" }));
  await scanning().findByText(FOLDER);
  await user.click(scanning().getByRole("button", { name: "Scan stream" }));
  expect(
    await screen.findByText("Scanned so far: 155648 bytes, 512 records, 512 occurrences, 2 parsing batches"),
  ).toBeTruthy();
  expect(facade.callsTo("CorpusProgress").length).toBeGreaterThan(0);
  await user.click(scanning().getByRole("button", { name: "Cancel scan" }));
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
  expect(scanning().queryByRole("button", { name: "Cancel scan" })).toBeNull();
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
  await user.click(generation().getByRole("button", { name: "Choose a folder for the corpus…" }));
  expect(await screen.findByText("no folder was chosen")).toBeTruthy();
  expect(generation().getByText("No folder chosen.")).toBeTruthy();

  facade.reply(handlers({ GenerateCorpus: () => ({ state: "failed", reason: "a corpus holds between 1 and 1048576 messages" }) }));
  await declareGeneration(user, "7");
  // A count that is not a plain whole number cannot be sent at all: not a
  // unit, not a leading zero the command would read as octal, and not a
  // number a JavaScript number cannot hold exactly.
  for (const typed of ["5k", "010", "9007199254740993"]) {
    await user.clear(generation().getByLabelText("Messages"));
    await user.type(generation().getByLabelText("Messages"), typed);
    expect((generation().getByRole("button", { name: "Generate corpus" }) as HTMLButtonElement).disabled).toBe(true);
  }
  await user.clear(generation().getByLabelText("Messages"));
  await user.type(generation().getByLabelText("Messages"), "0");
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

test("the palette opens the performance corpus screen and Escape cancels a running scan from the keyboard", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(handlers());
  const parked = facade.park("ScanCorpus");
  await user.keyboard("{Control>}k{/Control}");
  await user.type(screen.getByLabelText("Type a command"), "performance corpus{Enter}");
  const toggle = screen.getByRole("button", { name: "Generate or scan a performance corpus" });
  expect(toggle.getAttribute("aria-expanded")).toBe("true");
  expect(document.activeElement).toBe(toggle);

  const choose = scanning().getByRole("button", { name: "Choose a stream to scan…" });
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
  expect((screen.getByRole("button", { name: "Open a workspace folder…" }) as HTMLButtonElement).disabled).toBe(true);
  await user.keyboard("{Escape}");
  expect(facade.callsTo("Cancel").map((call) => call.args[0])).toEqual([""]);
  parked.resolve({
    state: "cancelled",
    reason: "the scan was cancelled; these are the counts it reached, the case bounds were not evaluated and no benchmark was written",
    scan: scanView({ case_bounds: "not-evaluated", exceeded: [], window_limit: 0, rows: [] }),
  });
  expect(await screen.findByText("not evaluated; the scan was cancelled")).toBeTruthy();
  await waitFor(() =>
    expect((screen.getByRole("button", { name: "Open a workspace folder…" }) as HTMLButtonElement).disabled).toBe(false),
  );
});
