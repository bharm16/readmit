// Tools (view 10) and Benchmarks (view 41), driven over the stubbed facade.
// What a measurement means is the Go scanner's; these prove which call a
// person's act makes and that the screen shows only what was measured.
import { expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderApp } from "./testkit/app";
import { folderWithCase, WORKSPACE_ROOT } from "./testkit/fixtures";
import { details, goTo, page, sidebar } from "./testkit/navigation";
import type { BenchmarkDefaults, BenchmarkInput, BenchmarkRecord, RequestContext } from "./bindings";

const PLAN = { schema: "readmit-import-plan/v1", framing: "mllp", terminator: "cr", encoding: "utf-8", direction: "unknown", members: [] } as const;

function defaults(): BenchmarkDefaults {
  return {
    generator_version: "readmit-corpus-v1",
    profile_version: "readmit-siu-v1",
    max_messages: 1048576,
    seed: "0",
    base_time: "2026-09-29T10:00:00Z",
    framings: ["mllp", "batch"],
    batch_boundaries: ["segment-start"],
    terminators: ["cr"],
    encodings: ["utf-8", "us-ascii"],
    directions: ["inbound", "outbound", "unknown"],
    plan: { ...PLAN, members: [] },
    batch_records: 256,
    max_batch_records: 256,
    batch_bytes: 8388608,
    max_batch_bytes: 8388608,
    window_limit: 0,
    max_window_limit: 200,
  };
}

function record(id: string, name: string, created: string, complete = true): BenchmarkRecord {
  return {
    id,
    created_at: created,
    input: { kind: "generated", id: "input-1", name },
    completion: complete ? "complete" : "incomplete",
    plan: { ...PLAN, members: [] },
    bounds: { batch_records: 256, batch_bytes: 8388608, record_bytes: 16777216, resident_bound: 42008576 },
    records: complete ? 10000 : 1234,
    bytes: complete ? 3010000 : 371000,
    batches: complete ? 40 : 5,
    ...(complete ? { elapsed_milliseconds: 1000, peak_scan_buffer_bytes: 163840, sha256: "a".repeat(64), case_bounds: "within" } : {}),
    hardware: { os: "darwin", arch: "arm64", cpus: 10, go_version: "go1.27.1" },
  } as BenchmarkRecord;
}

function input(id: string, name: string): BenchmarkInput {
  return {
    id,
    name,
    created_at: "2026-09-29T09:00:00Z",
    origin: "synthetic",
    availability: "available",
    manifest: { schema: "readmit-corpus/v1", seed: "0", base_time: "2026-09-29T10:00:00Z", generator_version: "readmit-corpus-v1", profile_version: "readmit-siu-v1", messages: 10000, plan: { ...PLAN, members: [] }, bytes: 3010000, sha256: "a".repeat(64) },
  } as BenchmarkInput;
}

async function openBenchmarks(handlers: Parameters<typeof renderApp>[0] = {}) {
  const user = userEvent.setup();
  const rendered = await renderApp({
    SelectWorkspace: () => folderWithCase(),
    BenchmarkDefaults: () => ({ state: "completed", defaults: defaults() }),
    ListBenchmarkInputs: (context: RequestContext) => ({ state: "empty", context, inputs: [] }),
    ListBenchmarks: (context: RequestContext) => ({ state: "empty", context, results: [] }),
    ...handlers,
  });
  await goTo(user, "Projects");
  await user.click(screen.getByRole("button", { name: "Open" }));
  await sidebar().findByRole("button", { name: /^Project: / });
  await goTo(user, "Tools");
  await user.click(page().getByRole("button", { name: "Benchmarks" }));
  return { user, ...rendered };
}

test("Tools shows three launcher rows and Enter opens each task", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    BenchmarkDefaults: () => ({ state: "completed", defaults: defaults() }),
    ChooseInspectionPath: () => ({ state: "cancelled" }),
  });
  await goTo(user, "Tools");
  const rows = within(page().getByRole("list", { name: "Tools" })).getAllByRole("button");
  expect(rows.map((row) => row.textContent)).toEqual(["Inspect file", "Sample data", "Benchmarks"]);
  expect(page().queryAllByRole("textbox")).toHaveLength(0);
  // Enter on a row activates its real task: Inspect file asks for the file,
  // and a cancelled chooser leaves nothing behind.
  rows[0]!.focus();
  await user.keyboard("{Enter}");
  await waitFor(() => expect(facade.callsTo("ChooseInspectionPath")).toHaveLength(1));
  // A cancelled chooser leaves the person on Tools.
  within(await page().findByRole("list", { name: "Tools" })).getByRole("button", { name: "Benchmarks" }).focus();
  await user.keyboard("{Enter}");
  expect(await screen.findByRole("heading", { level: 1, name: "Benchmarks" })).toBeTruthy();
});

test("Benchmarks lists measured results newest first or offers Start benchmark", async () => {
  const { user, facade } = await openBenchmarks();
  expect(await page().findByText("No benchmarks")).toBeTruthy();
  expect(page().getAllByRole("button", { name: "Start benchmark" })).toHaveLength(1);
  facade.reply({
    ListBenchmarks: (context: RequestContext) => ({
      state: "completed",
      context,
      results: [record("b1", "Older sample", "2026-09-27T10:00:00Z"), record("b2", "Newer sample", "2026-09-28T10:00:00Z")],
    }),
  });
  await goTo(user, "Tools");
  await user.click(page().getByRole("button", { name: "Benchmarks" }));
  const table = await page().findByRole("table", { name: "Benchmarks" });
  const rows = within(table).getAllByRole("row").slice(1);
  expect(rows.map((row) => row.getAttribute("aria-label")?.split(" · ")[0])).toEqual(["Newer sample", "Older sample"]);
  expect(within(table).getAllByRole("columnheader").map((header) => header.textContent)).toEqual(["Input", "Records", "Duration", "Peak buffer", "Date"]);
  expect(within(rows[0]!).getByText("1.00 s")).toBeTruthy();
});

test("an incomplete benchmark shows bytes and records read and no throughput", async () => {
  const stopped = record("b3", "Large sample", "2026-09-29T10:00:00Z", false);
  const { user, facade } = await openBenchmarks({
    ListBenchmarks: (context: RequestContext) => ({ state: "completed", context, results: [stopped] }),
    OpenBenchmark: (request: { context: RequestContext }) => ({ state: "completed", context: request.context, result: stopped, availability: "available" }),
    CorpusProgress: () => ({ state: "empty" }),
  });
  const table = await page().findByRole("table", { name: "Benchmarks" });
  expect(within(table).getByText("Incomplete")).toBeTruthy();
  await user.click(within(table).getAllByRole("row")[1]!);
  const shown = within(await screen.findByRole("region", { name: "Details" }));
  const value = (label: string) => shown.getByText(label).closest(".value-row")?.querySelector("dd")?.textContent;
  expect(value("Completion")).toBe("Incomplete");
  expect(value("Records")).toBe("1,234");
  expect(value("Bytes read")).toContain("371,000 bytes");
  expect(value("Duration")).toBe("—");
  expect(value("Throughput")).toBe("—");
  expect(value("Peak scan buffer")).toBe("—");
  expect(facade.oneCall("OpenBenchmark")[0].id).toBe("b3");
  expect(details()).toBeTruthy();
});

test("Start benchmark sends the documented limits unless changed", async () => {
  const measured = record("b4", "Chosen", "2026-09-29T11:00:00Z");
  const { user, facade } = await openBenchmarks({
    ListBenchmarkInputs: (context: RequestContext) => ({ state: "completed", context, inputs: [input("input-1", "Scheduling sample")] }),
    ChooseCorpusPath: (kind: string) => ({ state: "completed", kind, path: "/data/stream.hl7" }),
    ProbeImport: (request: { context?: RequestContext }) => ({
      state: "completed",
      context: request.context ?? { project: "", generation: 0 },
      inputs: [],
      formats: [{ mode: "plan", label: "HL7 v2", plan: { ...PLAN, members: [] } }],
      selected: 0,
      sample: null,
    }),
    StartBenchmark: (request: { context: RequestContext }) => ({ state: "completed", context: request.context, result: measured }),
    CorpusProgress: () => ({ state: "empty" }),
  });
  await user.click(await page().findByRole("button", { name: "Start benchmark" }));
  let sheet = within(await screen.findByRole("dialog", { name: "Start benchmark" }));
  expect((sheet.getByLabelText("Records per batch") as HTMLInputElement).value).toBe("256");
  expect((sheet.getByLabelText("Bytes per batch") as HTMLInputElement).value).toBe("8388608");
  expect((sheet.getByLabelText("Records to show") as HTMLInputElement).value).toBe("0");
  await user.click(sheet.getByRole("button", { name: "Start benchmark" }));
  await waitFor(() => expect(facade.callsTo("StartBenchmark")).toHaveLength(1));
  expect(facade.oneCall("StartBenchmark")[0]).toMatchObject({ input_id: "input-1", batch_records: 256, batch_bytes: 8388608, window_limit: 0, window_offset: 0 });

  // A chosen file's format comes from the one unambiguous detection; a
  // changed limit is the one sent.
  await user.click(await page().findByRole("button", { name: "Start benchmark" }));
  sheet = within(await screen.findByRole("dialog", { name: "Start benchmark" }));
  await user.click(sheet.getByRole("button", { name: "Choose file…" }));
  expect(await sheet.findByText(/MLLP · CR · UTF-8 · Unknown direction \(detected\)/)).toBeTruthy();
  await user.clear(sheet.getByLabelText("Records per batch"));
  await user.type(sheet.getByLabelText("Records per batch"), "16");
  await user.click(sheet.getByRole("button", { name: "Start benchmark" }));
  await waitFor(() => expect(facade.callsTo("StartBenchmark")).toHaveLength(2));
  expect(facade.callsTo("StartBenchmark")[1]!.args[0]).toMatchObject({ file: "/data/stream.hl7", plan: { framing: "mllp" }, batch_records: 16, batch_bytes: 8388608 });
  expect(facade.oneCall("ChooseCorpusPath")).toEqual(["scan-file"]);
});

test("Generate input keeps its seed and base time while editing and adds the named input", async () => {
  const generated = input("input-2", "Nightly sample");
  const { user, facade } = await openBenchmarks({
    GenerateInput: (request: { context: RequestContext }) => ({ state: "completed", context: request.context, input: generated }),
    CorpusProgress: () => ({ state: "empty" }),
  });
  await user.click(await page().findByRole("button", { name: "Generate input" }));
  const sheet = within(await screen.findByRole("dialog", { name: "Generate input" }));
  await user.type(sheet.getByLabelText("Name"), "Nightly sample");
  await user.type(sheet.getByLabelText("Message count"), "5000");
  await user.selectOptions(sheet.getByLabelText("Framing"), "batch");
  await user.selectOptions(sheet.getByLabelText("Framing"), "mllp");
  expect((sheet.getByLabelText("Seed") as HTMLInputElement).value).toBe("0");
  expect((sheet.getByLabelText("Base time") as HTMLInputElement).value).toBe("2026-09-29T10:00:00Z");
  facade.reply({ ListBenchmarkInputs: (context: RequestContext) => ({ state: "completed", context, inputs: [generated] }) });
  await user.click(sheet.getByRole("button", { name: "Generate input" }));
  await waitFor(() => expect(facade.callsTo("GenerateInput")).toHaveLength(1));
  expect(facade.oneCall("GenerateInput")[0]).toMatchObject({
    name: "Nightly sample",
    seed: "0",
    base_time: "2026-09-29T10:00:00Z",
    generator_version: "readmit-corpus-v1",
    profile_version: "readmit-siu-v1",
    messages: 5000,
    plan: { framing: "mllp", terminator: "cr", encoding: "utf-8", direction: "unknown" },
    context: { project: WORKSPACE_ROOT },
  });
  // The new input is offered, with its retained settings, in Start benchmark.
  const start = within(await screen.findByRole("dialog", { name: "Start benchmark" }));
  expect((start.getByRole("radio", { name: "Nightly sample" }) as HTMLInputElement).checked).toBe(true);
  expect(start.getByText("Synthetic")).toBeTruthy();
  expect(facade.callsTo("StartBenchmark")).toHaveLength(0);
  expect(facade.callsTo("BenchmarkDefaults").length).toBeGreaterThan(0);
});
