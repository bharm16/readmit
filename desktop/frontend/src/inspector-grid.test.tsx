// The two evidence panes: the grid draws positions and counts and never a
// value, and the inspector reveals bytes only for the occurrence a person
// selected. Both render exactly what the facade answered, so the fixtures
// carry positions, states and counts only.
import { expect, test } from "vitest";
import { fireEvent, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Inspector } from "./Inspector";
import { MessageGrid, Palette } from "./shell";
import {
  CASE_IDENTITY,
  GRID_OCCURRENCE,
  INDEX_ENTRY,
  caseResult,
  indicatorTable,
  indexDetailsFixture,
  inspectionResult,
  filtersResult,
  gridResult,
  gridRow,
  refused,
  vocabularyFixture,
} from "./testkit/fixtures";

/** How many occurrences one window of the grid shows, as the facade publishes it. */
const GRID_WINDOW = vocabularyFixture().bounds.grid;

test("the grid states how many records the view is not showing, always", () => {
  render(
    <MessageGrid
      indicators={indicatorTable()}
      progress={null}
      result={gridResult([gridRow(GRID_OCCURRENCE)], {
        total: 10000,
        matched: 3,
        excluded: 9997,
        undecided: 2,
        undecodable: 1,
      })}
      filters={filtersResult()}
      entries={[INDEX_ENTRY]}
      busy={false}
      onOpen={() => undefined}
      onSelect={() => undefined}
      onSave={() => undefined}
      selectedOccurrence={null}
      onInspect={() => undefined}
    />,
  );
  expect(screen.getByText("Showing 1 of 3 matching")).toBeTruthy();
  expect(screen.getByText("9997 of 10000 excluded by no filter")).toBeTruthy();
  expect(screen.getByText("2 values the index could not settle")).toBeTruthy();
  expect(screen.getByText("1 the case could not decode")).toBeTruthy();
});

test("opening a window is the facade's own bound, and paging is another call", async () => {
  const user = userEvent.setup();
  const opened: Array<[string, number]> = [];
  render(
    <MessageGrid
      indicators={indicatorTable()}
      progress={null}
      result={gridResult(
        Array.from({ length: 3 }, (_, i) => gridRow(`occ-${i}`)),
        { offset: 200, limit: GRID_WINDOW, matched: 1000, total: 1000, excluded: 0 },
      )}
      filters={filtersResult()}
      entries={[INDEX_ENTRY]}
      busy={false}
      onOpen={(indexName, offset) => opened.push([indexName, offset])}
      onSelect={() => undefined}
      onSave={() => undefined}
      selectedOccurrence={null}
      onInspect={() => undefined}
    />,
  );
  const previous = screen.getByRole("button", { name: `Previous ${GRID_WINDOW} occurrences` });
  const next = screen.getByRole("button", { name: `Next ${GRID_WINDOW} occurrences` });
  expect((next as HTMLButtonElement).disabled).toBe(false);
  await user.click(previous);
  expect(opened).toEqual([[INDEX_ENTRY, 0]]);
});

test("selecting a row hands that occurrence to the inspector", async () => {
  const user = userEvent.setup();
  const inspected: string[] = [];
  render(
    <MessageGrid
      indicators={indicatorTable()}
      progress={null}
      result={gridResult([gridRow(GRID_OCCURRENCE), gridRow("occ-000002", "ack")])}
      filters={filtersResult()}
      entries={[INDEX_ENTRY]}
      busy={false}
      onOpen={() => undefined}
      onSelect={() => undefined}
      onSave={() => undefined}
      selectedOccurrence={null}
      onInspect={(occurrence) => inspected.push(occurrence)}
    />,
  );
  await user.click(screen.getByRole("button", { name: `Inspect ${GRID_OCCURRENCE}` }));
  expect(inspected).toEqual([GRID_OCCURRENCE]);
});

test("saving a filter composes the typed contract in one piece", async () => {
  const user = userEvent.setup();
  const saved: unknown[] = [];
  render(
    <MessageGrid
      indicators={indicatorTable()}
      progress={null}
      result={null}
      filters={filtersResult()}
      entries={[INDEX_ENTRY]}
      busy={false}
      onOpen={() => undefined}
      onSelect={() => undefined}
      onSave={(filter) => saved.push(filter)}
      selectedOccurrence={null}
      onInspect={() => undefined}
    />,
  );
  await user.click(screen.getByRole("button", { name: "New filter" }));
  await user.type(screen.getByLabelText("Filter name"), "rejected acks");
  await user.click(screen.getByLabelText("ACK"));
  await user.type(screen.getByLabelText("ACK codes"), "AA, AE");
  fireEvent.change(screen.getByLabelText("Field selector"), { target: { value: "MSA[1]-1[1]" } });
  await user.type(screen.getByLabelText("Match value"), "AE");
  await user.click(screen.getByRole("button", { name: "Save and apply filter" }));
  expect(saved).toEqual([
    {
      name: "rejected acks",
      kinds: ["ack"],
      sources: [],
      observed_from: null,
      observed_until: null,
      ack_codes: ["AA", "AE"],
      fields: [{ selector: "MSA[1]-1[1]", match: "contains", term: "AE", state: "" }],
    },
  ]);
});

test("a time bound is sent as the UTC instant it names, or not at all", async () => {
  const user = userEvent.setup();
  const saved: unknown[] = [];
  render(
    <MessageGrid
      indicators={indicatorTable()}
      progress={null}
      result={null}
      filters={filtersResult()}
      entries={[INDEX_ENTRY]}
      busy={false}
      onOpen={() => undefined}
      onSelect={() => undefined}
      onSave={(filter) => saved.push(filter)}
      selectedOccurrence={null}
      onInspect={() => undefined}
    />,
  );
  await user.click(screen.getByRole("button", { name: "New filter" }));
  await user.type(screen.getByLabelText("Filter name"), "that afternoon");
  fireEvent.change(screen.getByLabelText("Observed from (local time)"), { target: { value: "2026-01-01T12:00" } });
  await user.click(screen.getByRole("button", { name: "Save and apply filter" }));
  expect(saved).toEqual([
    {
      name: "that afternoon",
      kinds: [],
      sources: [],
      observed_from: new Date("2026-01-01T12:00").toISOString(),
      observed_until: null,
      ack_codes: [],
      fields: [],
    },
  ]);
});

test("the inspector reveals what the verified read returned, and no more", () => {
  render(
    <Inspector
      result={inspectionResult(GRID_OCCURRENCE, {
        children: [
          { segment: "MSH", field: 3, path: "MSH[1]-3[1]", parent: "", kind: "field", state: "present", start: 10, end: 20 },
        ],
        child_count: 1,
        raw: "",
        decoded: "",
      })}
      busy={false}
      progress={null}
      indicators={indicatorTable()}
      onInspect={() => undefined}
    />,
  );
  expect(
    screen.getByText(
      `Occurrence ${GRID_OCCURRENCE} · Source s0001 · Source offset 0 · 256 original bytes`,
    ),
  ).toBeTruthy();
  // An empty value is an absence, never a fabricated text.
  expect(screen.getByText("(no displayed bytes)")).toBeTruthy();
  expect(screen.getByText("(no decoded text)")).toBeTruthy();
});

test("a child of the tree is selected by clicking it, as a path", async () => {
  const user = userEvent.setup();
  const selections: Array<[string, number, number]> = [];
  render(
    <Inspector
      result={inspectionResult(GRID_OCCURRENCE, {
        selected: { segment: "MSH", field: 0, path: "MSH[1]-3[1]", parent: "", kind: "message", state: "present", start: 0, end: 256 },
        children: [
          { segment: "MSH", field: 3, path: "MSH[1]-3[1]", parent: "MSH[1]-3[1]", kind: "field", state: "present", start: 10, end: 20 },
        ],
        child_count: 1,
      })}
      busy={false}
      progress={null}
      indicators={indicatorTable()}
      onInspect={(path, nodeOffset, byteOffset) => selections.push([path, nodeOffset, byteOffset])}
    />,
  );
  await user.click(screen.getByRole("button", { name: /MSH\[1\]-3\[1\] · field/ }));
  await user.click(screen.getByRole("button", { name: "Message root" }));
  expect(selections).toEqual([["MSH[1]-3[1]", 0, -1], ["", 0, -1]]);
});

test("an unparsed occurrence offers no field tree and no selector entry", () => {
  render(
    <Inspector
      result={inspectionResult(GRID_OCCURRENCE, { decode_state: "unparsed", children: [], child_count: 0 })}
      busy={false}
      progress={null}
      indicators={indicatorTable()}
      onInspect={() => undefined}
    />,
  );
  expect(
    (screen.getByLabelText("Field path") as HTMLInputElement)
      .disabled,
  ).toBe(true);
  expect(
    (screen.getByRole("button", { name: "Inspect selector" }) as HTMLButtonElement).disabled,
  ).toBe(true);
});

test("a refused inspection is reported inside the inspector", () => {
  render(
    <Inspector
      result={refused("The occurrence is larger than this release displays.")}
      busy={false}
      progress={null}
      indicators={indicatorTable()}
      onInspect={() => undefined}
    />,
  );
  expect(
    screen.getByText("The occurrence is larger than this release displays."),
  ).toBeTruthy();
});

test("an unindexed case reports case metadata and offers to build an index", async () => {
  const user = userEvent.setup();
  render(
    <MessageGrid
      indicators={indicatorTable()}
      progress={null}
      result={null}
      filters={filtersResult()}
      entries={[]}
      busy={false}
      onOpen={() => undefined}
      onSelect={() => undefined}
      onSave={() => undefined}
      selectedOccurrence={null}
      onInspect={() => undefined}
      caseEvidence={caseResult().case}
      indexDetails={null}
      onBuildIndex={() => undefined}
    />
  );

  expect(screen.getByText("Case is unindexed")).toBeTruthy();
  expect(screen.getByText(/The case is not empty/)).toBeTruthy();
  expect(screen.getByRole("button", { name: "Set up index" })).toBeTruthy();

  await user.click(screen.getByRole("button", { name: "Set up index" }));
  expect(screen.getByRole("form", { name: "Build index form" })).toBeTruthy();
});

test("a stale, expired, damaged, or unsupported index shows a rebuild banner", async () => {
  const user = userEvent.setup();
  render(
    <MessageGrid
      indicators={indicatorTable()}
      progress={null}
      result={null}
      filters={filtersResult()}
      entries={[INDEX_ENTRY]}
      busy={false}
      onOpen={() => undefined}
      onSelect={() => undefined}
      onSave={() => undefined}
      selectedOccurrence={null}
      onInspect={() => undefined}
      caseEvidence={caseResult().case}
      indexDetails={indexDetailsFixture({ applicable: false, stale: true })}
      onBuildIndex={() => undefined}
    />,
  );

  expect(screen.getByRole("alert", { name: "Index rebuild notice" })).toBeTruthy();
  expect(screen.getByText("[Index rebuild required]")).toBeTruthy();
  expect(screen.getByText(/The index was built from different evidence or is stale for this case/)).toBeTruthy();

  await user.click(screen.getByRole("button", { name: "Set up rebuild" }));
  expect(screen.getByRole("form", { name: "Build index form" })).toBeTruthy();
});

test("an explicitly selected index of another case is refused without offering replacement", async () => {
  const user = userEvent.setup();
  const built: unknown[] = [];
  render(
    <MessageGrid
      indicators={indicatorTable()}
      progress={null}
      result={null}
      filters={filtersResult()}
      entries={[INDEX_ENTRY]}
      busy={false}
      onOpen={() => undefined}
      onSelect={() => undefined}
      onSave={() => undefined}
      selectedOccurrence={null}
      onInspect={() => undefined}
      caseEvidence={caseResult("followup", "followup-identity").case}
      indexDetails={indexDetailsFixture({ identity: CASE_IDENTITY, applicable: false, stale: true })}
      onBuildIndex={(request) => built.push(request)}
    />,
  );

  expect(screen.getByRole("alert", { name: "Index mismatch notice" })).toBeTruthy();
  expect(screen.queryByRole("alert", { name: "Index rebuild notice" })).toBeNull();
  expect(screen.getByText("Case is unindexed")).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Set up index" }));
  const output = screen.getByLabelText("Index file") as HTMLInputElement;
  expect(output.value).toBe("followup.index.json");
  expect(screen.queryByLabelText("Replace selected index")).toBeNull();
  fireEvent.change(output, { target: { value: INDEX_ENTRY } });
  await user.click(screen.getByRole("button", { name: "Build index" }));
  expect(built).toEqual([expect.objectContaining({ case: "followup", output: INDEX_ENTRY, replace: false })]);
});

test("an unreadable index with no known owner is not offered for replacement", async () => {
  const user = userEvent.setup();
  render(
    <MessageGrid
      indicators={indicatorTable()}
      progress={null}
      result={null}
      filters={filtersResult()}
      entries={[INDEX_ENTRY]}
      busy={false}
      onOpen={() => undefined}
      onSelect={() => undefined}
      onSave={() => undefined}
      selectedOccurrence={null}
      onInspect={() => undefined}
      caseEvidence={caseResult("followup", "followup-identity").case}
      indexDetails={indexDetailsFixture({ identity: "", applicable: false, damaged: true })}
      onBuildIndex={() => undefined}
    />,
  );

  expect(screen.getByRole("alert", { name: "Index ownership unknown notice" })).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Set up index" }));
  expect((screen.getByLabelText("Index file") as HTMLInputElement).value).toBe("followup.index.json");
  expect(screen.queryByLabelText("Replace selected index")).toBeNull();
});

test("an active index displays its retention form and permitted searches", () => {
  render(
    <MessageGrid
      indicators={indicatorTable()}
      progress={null}
      result={gridResult([gridRow(GRID_OCCURRENCE)])}
      filters={filtersResult()}
      entries={[INDEX_ENTRY]}
      busy={false}
      onOpen={() => undefined}
      onSelect={() => undefined}
      onSave={() => undefined}
      selectedOccurrence={null}
      onInspect={() => undefined}
      caseEvidence={caseResult().case}
      indexDetails={indexDetailsFixture({
        applicable: true,
        retention: "values",
        fields: ["PID-3", "PV1-19"],
      })}
    />,
  );

  expect(screen.getByText(`Index: ${INDEX_ENTRY}`)).toBeTruthy();
  expect(screen.getByText("Retention: values (active)")).toBeTruthy();
  expect(
    screen.getByText("Permitted searches: full substring search, equality, presence, and absence"),
  ).toBeTruthy();
});

test("building an index composes the typed request with retention and structured fields", async () => {
  const user = userEvent.setup();
  const built: unknown[] = [];
  render(
    <MessageGrid
      indicators={indicatorTable()}
      progress={null}
      result={null}
      filters={filtersResult()}
      entries={[]}
      busy={false}
      onOpen={() => undefined}
      onSelect={() => undefined}
      onSave={() => undefined}
      selectedOccurrence={null}
      onInspect={() => undefined}
      caseEvidence={caseResult().case}
      indexDetails={null}
      onBuildIndex={(req) => built.push(req)}
    />,
  );

  await user.click(screen.getByRole("button", { name: "Set up index" }));
  const customInput = screen.getByLabelText("Field selector");
  fireEvent.change(customInput, { target: { value: "OBX[1]-3" } });
  await user.click(screen.getByRole("button", { name: "Add field" }));

  await user.click(screen.getByLabelText(/Plaintext values/));
  await user.click(screen.getByRole("button", { name: "Build index" }));

  expect(built).toEqual([
    {
      workspace: "",
      case: "sample-case",
      identity: "case-identity-fixed-for-tests",
      output: "sample-case.index.json",
      fields: ["PID-3", "MSH-10", "OBX[1]-3"],
      retention: "values",
      retain_until: "indefinite",
      replace: false,
    },
  ]);
});

test("switching to a second unindexed case offers a separate index without replacement", async () => {
  const user = userEvent.setup();
  const built: unknown[] = [];
  const props = {
    indicators: indicatorTable(), progress: null, result: null,
    filters: filtersResult(), entries: [INDEX_ENTRY], busy: false,
    onOpen: () => undefined, onSelect: () => undefined, onSave: () => undefined,
    selectedOccurrence: null, onInspect: () => undefined,
    onBuildIndex: (request: unknown) => built.push(request),
  };
  const { rerender } = render(
    <MessageGrid {...props} caseEvidence={caseResult().case} indexDetails={indexDetailsFixture({ applicable: false, stale: true })} />,
  );

  await user.click(screen.getByRole("button", { name: "Set up rebuild" }));
  fireEvent.change(screen.getByLabelText("Index file"), { target: { value: INDEX_ENTRY } });
  expect((screen.getByLabelText("Replace selected index") as HTMLInputElement).checked).toBe(true);

  rerender(<MessageGrid {...props} caseEvidence={caseResult("followup", "followup-identity").case} indexDetails={null} />);
  expect(screen.getByText("Case is unindexed")).toBeTruthy();
  expect(screen.queryByRole("alert", { name: "Index rebuild notice" })).toBeNull();
  expect(screen.queryByRole("form", { name: "Build index form" })).toBeNull();

  await user.click(screen.getByRole("button", { name: "Set up index" }));
  const output = screen.getByLabelText("Index file") as HTMLInputElement;
  expect(output.value).toBe("followup.index.json");
  expect(screen.queryByLabelText("Replace selected index")).toBeNull();
  await user.click(screen.getByRole("button", { name: "Build index" }));
  expect(built).toEqual([expect.objectContaining({
    case: "followup", identity: "followup-identity", output: "followup.index.json", replace: false,
  })]);
});

test("an occupied conventional index filename gets a fresh suggestion", async () => {
  const user = userEvent.setup();
  const built: unknown[] = [];
  render(
    <MessageGrid
      indicators={indicatorTable()}
      progress={null}
      result={null}
      filters={filtersResult()}
      entries={["followup.index.json"]}
      busy={false}
      onOpen={() => undefined}
      onSelect={() => undefined}
      onSave={() => undefined}
      selectedOccurrence={null}
      onInspect={() => undefined}
      caseEvidence={caseResult("followup", "changed-identity").case}
      indexDetails={null}
      onBuildIndex={(request) => built.push(request)}
    />,
  );

  await user.click(screen.getByRole("button", { name: "Set up index" }));
  expect((screen.getByLabelText("Index file") as HTMLInputElement).value).toBe("followup.2.index.json");
  expect(screen.queryByLabelText("Replace selected index")).toBeNull();
  await user.click(screen.getByRole("button", { name: "Build index" }));
  expect(built).toEqual([expect.objectContaining({ case: "followup", output: "followup.2.index.json", replace: false })]);
});

/** The grid's props with nothing answered yet and every callback recorded, so a
 * test can prove which actions reached the facade and which did not. */
function explorer(overrides: Partial<Parameters<typeof MessageGrid>[0]> = {}) {
  const calls = { opened: [] as unknown[], saved: [] as unknown[], built: [] as unknown[], selected: [] as unknown[] };
  const props: Parameters<typeof MessageGrid>[0] = {
    indicators: indicatorTable(),
    progress: null,
    result: null,
    filters: filtersResult(),
    entries: [INDEX_ENTRY],
    busy: false,
    onOpen: (name, offset) => calls.opened.push([name, offset]),
    onSelect: (name) => calls.selected.push(name),
    onSave: (filter) => calls.saved.push(filter),
    selectedOccurrence: null,
    onInspect: () => undefined,
    caseEvidence: caseResult().case,
    indexDetails: null,
    onBuildIndex: (request) => calls.built.push(request),
    ...overrides,
  };
  return { props, calls };
}

test("the explorer shows messages first; setting up an index or a filter reads and writes nothing", async () => {
  const user = userEvent.setup();
  const { props, calls } = explorer({ result: gridResult([gridRow(GRID_OCCURRENCE)]), indexDetails: indexDetailsFixture() });
  render(<MessageGrid {...props} />);
  expect(screen.getByRole("region", { name: "Messages" })).toBeTruthy();
  expect(screen.getByRole("heading", { name: "Messages" })).toBeTruthy();
  // Neither configuration form competes with the evidence until asked for.
  expect(screen.queryByRole("form", { name: "Build index form" })).toBeNull();
  expect(screen.queryByRole("form", { name: "Filter editor" })).toBeNull();

  const setup = screen.getByRole("button", { name: "Set up index" });
  expect(setup.getAttribute("aria-expanded")).toBe("false");
  await user.click(setup);
  expect(screen.getByRole("form", { name: "Build index form" })).toBeTruthy();
  expect(screen.getByRole("heading", { name: "Build index" })).toBeTruthy();
  const newFilter = screen.getByRole("button", { name: "New filter" });
  await user.click(newFilter);
  expect(newFilter.getAttribute("aria-expanded")).toBe("true");
  expect(screen.getByRole("heading", { name: "Filter editor" })).toBeTruthy();
  expect(calls).toEqual({ opened: [], saved: [], built: [], selected: [] });

  // Closing setup hides it without discarding what was typed.
  const form = within(screen.getByRole("form", { name: "Build index form" }));
  fireEvent.change(form.getByLabelText("Index file"), { target: { value: "kept.index.json" } });
  await user.click(form.getByRole("button", { name: "Close setup" }));
  expect(screen.queryByRole("form", { name: "Build index form" })).toBeNull();
  await user.click(screen.getByRole("button", { name: "Set up index" }));
  expect((screen.getByLabelText("Index file") as HTMLInputElement).value).toBe("kept.index.json");
  expect(calls.built).toEqual([]);
  // The explorer context keeps the case, the open index and the filter in view.
  expect(screen.getByText("Case:", { exact: false }).textContent).toBe("Case: sample-case");
  expect(screen.getByText("Showing index:", { exact: false }).textContent).toBe(`Showing index: ${INDEX_ENTRY}`);
  expect(screen.getByText("Active filter:", { exact: false }).textContent).toBe("Active filter: No filter");
});

test("setting up a rebuild prefills the selected index's policy, and only Rebuild index writes", async () => {
  const user = userEvent.setup();
  const deadline = "2026-12-31T17:30:00Z";
  const { props, calls } = explorer({
    now: () => Date.parse("2026-06-01T00:00:00Z"),
    indexDetails: indexDetailsFixture({
      applicable: false,
      damaged: true,
      retention: "digests",
      fields: ["PID-3", "PV1-19"],
      retain_until: deadline,
    }),
  });
  render(<MessageGrid {...props} />);
  await user.click(screen.getByRole("button", { name: "Set up rebuild" }));
  expect(calls.built).toEqual([]);
  const form = within(screen.getByRole("form", { name: "Build index form" }));
  expect(form.getByRole("heading", { name: "Rebuild index" })).toBeTruthy();
  // The finite deadline stays finite, shown in local time beside the instant
  // it is stored as.
  expect((form.getByLabelText("Retain indefinitely") as HTMLInputElement).checked).toBe(false);
  const local = form.getByLabelText("Retain until (local time)") as HTMLInputElement;
  expect(new Date(local.value).toISOString()).toBe(new Date(deadline).toISOString());
  expect(form.getByText(`Stored as ${new Date(deadline).toISOString()} (UTC)`)).toBeTruthy();
  expect((form.getByLabelText(/SHA-256 digests/) as HTMLInputElement).checked).toBe(true);
  expect((form.getByLabelText("Replace selected index") as HTMLInputElement).checked).toBe(true);
  expect(form.getByText(`Replaces ${INDEX_ENTRY} of case sample-case.`)).toBeTruthy();

  await user.click(form.getByRole("button", { name: "Rebuild index" }));
  expect(calls.built).toEqual([
    {
      workspace: "",
      case: "sample-case",
      identity: CASE_IDENTITY,
      output: INDEX_ENTRY,
      fields: ["PID-3", "PV1-19"],
      retention: "digests",
      retain_until: new Date(deadline).toISOString(),
      replace: true,
    },
  ]);
});

test("replacement applies only to the selected index, never to another file name", async () => {
  const user = userEvent.setup();
  const { props, calls } = explorer({ entries: [INDEX_ENTRY, "other.index.json"], indexDetails: indexDetailsFixture({ applicable: false, damaged: true }) });
  render(<MessageGrid {...props} />);
  await user.click(screen.getByRole("button", { name: "Set up rebuild" }));
  fireEvent.change(screen.getByLabelText("Index file"), { target: { value: "other.index.json" } });
  expect(screen.getByText(`Only ${INDEX_ENTRY} of case sample-case can be replaced; another file name is written as a new index.`)).toBeTruthy();
  // Another file name is a new index, so the write says so; naming the
  // selected index again makes it the rebuild once more.
  expect(screen.queryByRole("button", { name: "Rebuild index" })).toBeNull();
  fireEvent.change(screen.getByLabelText("Index file"), { target: { value: INDEX_ENTRY } });
  expect(screen.queryByRole("button", { name: "Build index" })).toBeNull();
  fireEvent.change(screen.getByLabelText("Index file"), { target: { value: "other.index.json" } });
  await user.click(screen.getByRole("button", { name: "Build index" }));
  expect(calls.built).toEqual([expect.objectContaining({ output: "other.index.json", replace: false })]);
});

test("rebuilding an index whose retention has passed keeps its deadline finite and asks for a later one", async () => {
  const user = userEvent.setup();
  const deadline = "2026-12-31T17:30:00Z";
  const { props, calls } = explorer({
    now: () => Date.parse("2027-01-15T00:00:00Z"),
    indexDetails: indexDetailsFixture({ applicable: false, expired: true, retention_state: "ended", retain_until: deadline }),
  });
  render(<MessageGrid {...props} />);
  await user.click(screen.getByRole("button", { name: "Set up rebuild" }));
  const form = within(screen.getByRole("form", { name: "Build index form" }));
  // The ended deadline is still the one offered, never turned indefinite...
  expect((form.getByLabelText("Retain indefinitely") as HTMLInputElement).checked).toBe(false);
  const local = form.getByLabelText("Retain until (local time)") as HTMLInputElement;
  expect(new Date(local.value).toISOString()).toBe(new Date(deadline).toISOString());
  // ...but it is said to have passed, beside the field, and nothing is built.
  const passed = "This date and time has passed; enter a later one or choose Retain indefinitely. Nothing is built until you do.";
  expect(form.getByText(passed)).toBeTruthy();
  expect(local.getAttribute("aria-describedby")).toContain(form.getByText(passed).id);
  const rebuild = form.getByRole("button", { name: "Rebuild index" }) as HTMLButtonElement;
  expect(rebuild.disabled).toBe(true);
  fireEvent.submit(screen.getByRole("form", { name: "Build index form" }));
  expect(calls.built).toEqual([]);

  // Choosing indefinite retention explicitly is one way on.
  await user.click(form.getByLabelText("Retain indefinitely"));
  expect(rebuild.disabled).toBe(false);
  await user.click(form.getByLabelText("Retain indefinitely"));
  expect(rebuild.disabled).toBe(true);

  // A later deadline is the other.
  fireEvent.change(form.getByLabelText("Retain until (local time)"), { target: { value: "2027-06-30T12:00" } });
  expect(form.queryByText(passed)).toBeNull();
  await user.click(rebuild);
  expect(calls.built).toEqual([
    expect.objectContaining({ retain_until: new Date("2027-06-30T12:00").toISOString(), replace: true }),
  ]);
});

test("an incomplete local deadline blocks the build and never becomes indefinite", async () => {
  const user = userEvent.setup();
  const { props, calls } = explorer({ entries: [], now: () => Date.parse("2026-06-01T00:00:00Z") });
  render(<MessageGrid {...props} />);
  await user.click(screen.getByRole("button", { name: "Set up index" }));
  await user.click(screen.getByLabelText("Retain indefinitely"));
  const build = screen.getByRole("button", { name: "Build index" }) as HTMLButtonElement;
  expect(build.disabled).toBe(true);
  expect(screen.getByText("Enter a complete date and time; nothing is built until you do.")).toBeTruthy();
  fireEvent.submit(screen.getByRole("form", { name: "Build index form" }));
  expect(calls.built).toEqual([]);
  fireEvent.change(screen.getByLabelText("Retain until (local time)"), { target: { value: "2027-01-02T03:04" } });
  const stored = new Date("2027-01-02T03:04").toISOString();
  expect(screen.getByText(`Stored as ${stored} (UTC)`)).toBeTruthy();
  await user.click(build);
  expect(calls.built).toEqual([expect.objectContaining({ retain_until: stored })]);
});

test("removing an indexed field is a named icon that hands focus to the field left in its place", async () => {
  const user = userEvent.setup();
  const { props } = explorer({ entries: [] });
  render(<MessageGrid {...props} />);
  await user.click(screen.getByRole("button", { name: "Set up index" }));
  // The field selector is labelled visibly, with its example beside it.
  const selector = screen.getByLabelText("Field selector");
  expect(selector.getAttribute("placeholder")).toBeNull();
  expect(screen.getByText("Example: OBX[1]-3")).toBeTruthy();
  const remove = screen.getByRole("button", { name: "Remove indexed field PID-3" });
  expect(remove.querySelector("svg")?.getAttribute("aria-hidden")).toBe("true");
  expect(screen.getAllByRole("tooltip").map((tip) => tip.textContent)).toContain("Remove indexed field PID-3");
  remove.focus();
  await user.keyboard("{Enter}");
  expect(screen.queryByRole("button", { name: "Remove indexed field PID-3" })).toBeNull();
  expect(document.activeElement).toBe(screen.getByRole("button", { name: "Remove indexed field MSH-10" }));
  await user.keyboard("{Enter}");
  expect(document.activeElement).toBe(screen.getByLabelText("Field selector"));
  expect(screen.getByText("Indexed fields (0 of 16 selected)")).toBeTruthy();
});

test("the filter editor saves and applies one filter, and discarding clears only its draft", async () => {
  const user = userEvent.setup();
  const { props, calls } = explorer({ filters: filtersResult([{ name: "kept", kinds: [], sources: [], observed_from: null, observed_until: null, ack_codes: [], fields: [] }], "kept") });
  render(<MessageGrid {...props} />);
  await user.click(screen.getByRole("button", { name: "New filter" }));
  const form = within(screen.getByRole("form", { name: "Filter editor" }));
  await user.type(form.getByLabelText("Filter name"), "explicit nulls");
  await user.click(form.getByLabelText("Unparsed"));
  await user.type(form.getByLabelText("Source ID"), "s0002");
  fireEvent.change(form.getByLabelText("Field selector"), { target: { value: "PID[1]-8[1]" } });
  await user.selectOptions(form.getByLabelText("Match type"), "Field state");
  // Every field state is its own choice; none is merged into another.
  expect(within(form.getByLabelText("Field state")).getAllByRole("option").map((o) => [o.textContent, (o as HTMLOptionElement).value])).toEqual([
    ["Present", "present"],
    ["Empty", "empty"],
    ["Explicit null", "null"],
    ["Omitted", "omitted"],
  ]);
  await user.selectOptions(form.getByLabelText("Field state"), "Explicit null");
  fireEvent.change(form.getByLabelText("Observed before (local time)"), { target: { value: "2026-03-01T00:00" } });
  expect(form.getByText("The upper bound is exclusive. Both bounds are stored as UTC instants.")).toBeTruthy();
  await user.click(form.getByRole("button", { name: "Save and apply filter" }));
  expect(calls.saved).toEqual([
    {
      name: "explicit nulls",
      kinds: ["unparsed"],
      sources: ["s0002"],
      observed_from: null,
      observed_until: new Date("2026-03-01T00:00").toISOString(),
      ack_codes: [],
      fields: [{ selector: "PID[1]-8[1]", match: "state", term: "", state: "null" }],
    },
  ]);

  await user.click(form.getByRole("button", { name: "Discard filter draft" }));
  expect((form.getByLabelText("Filter name") as HTMLInputElement).value).toBe("");
  expect(document.activeElement).toBe(form.getByLabelText("Filter name"));
  // The saved filter and its selection are untouched.
  expect((screen.getByLabelText("Saved filter") as HTMLSelectElement).value).toBe("kept");
  expect(calls.selected).toEqual([]);
  expect(calls.saved).toHaveLength(1);
});

test("with no filter selected the picker says it shows every occurrence", () => {
  const { props } = explorer();
  render(<MessageGrid {...props} />);
  const picker = screen.getByLabelText("Saved filter");
  expect(within(picker).getAllByRole("option")[0]?.textContent).toBe("No filter");
  expect(document.getElementById(picker.getAttribute("aria-describedby") ?? "")?.textContent).toBe("Show every occurrence");
});

test("paging is a pair of named chevrons beside the range, disabled at each end", async () => {
  const user = userEvent.setup();
  const { props, calls } = explorer({
    result: gridResult([gridRow("occ-0")], { offset: 0, limit: GRID_WINDOW, matched: 1, total: 1, excluded: 0 }),
  });
  render(<MessageGrid {...props} />);
  const previous = screen.getByRole("button", { name: `Previous ${GRID_WINDOW} occurrences` }) as HTMLButtonElement;
  const next = screen.getByRole("button", { name: `Next ${GRID_WINDOW} occurrences` }) as HTMLButtonElement;
  expect(previous.disabled).toBe(true);
  expect(next.disabled).toBe(true);
  expect(screen.getAllByRole("tooltip").map((tip) => tip.textContent)).toEqual(
    expect.arrayContaining([`Previous ${GRID_WINDOW} occurrences`, `Next ${GRID_WINDOW} occurrences`]),
  );
  expect(screen.getByText("Occurrences 1–1")).toBeTruthy();
  expect(screen.getByText(`${GRID_WINDOW} per page`)).toBeTruthy();
  await user.click(next);
  expect(calls.opened).toEqual([]);
});

test("at a larger text scale the drawn rows and the scroll arithmetic stay on the same row", () => {
  const root = document.documentElement;
  root.style.fontSize = "32px";
  try {
    const rows = Array.from({ length: 100 }, (_, i) => gridRow(`occ-${String(i).padStart(3, "0")}`));
    const { props } = explorer({ result: gridResult(rows, { matched: 100, total: 100 }) });
    const { container } = render(<MessageGrid {...props} />);
    const viewport = container.querySelector(".grid-scroll") as HTMLDivElement;
    // Each row is 2rem: 64 pixels at a 32-pixel root.
    expect(viewport.style.maxHeight).toBe(`${64 * 16}px`);
    viewport.scrollTop = 64 * 40;
    fireEvent.scroll(viewport);
    const drawn = within(viewport).getAllByRole("row").filter((row) => row.getAttribute("aria-rowindex") !== "1");
    // Row 40 is at the top of the viewport, with the overscan drawn before it.
    expect(drawn[0]?.getAttribute("aria-rowindex")).toBe(String(40 - 8 + 2));
    const spacer = container.querySelector("tbody tr.spacer td") as HTMLTableCellElement;
    expect(spacer.style.height).toBe(`${(40 - 8) * 64}px`);
  } finally {
    root.style.fontSize = "";
  }
});

test("the command palette closes from a named icon that cancels nothing", async () => {
  const user = userEvent.setup();
  let closed = 0;
  render(<Palette open commands={[]} query="" onQuery={() => undefined} onClose={() => (closed += 1)} onRun={() => undefined} />);
  const close = screen.getByRole("button", { name: "Close command palette" });
  expect(close.querySelector("svg")?.getAttribute("aria-hidden")).toBe("true");
  expect(screen.getByRole("tooltip").textContent).toBe("Close command palette");
  close.focus();
  await user.keyboard("{Enter}");
  expect(closed).toBe(1);
});
