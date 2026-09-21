// The two evidence panes: the grid draws positions and counts and never a
// value, and the inspector reveals bytes only for the occurrence a person
// selected. Both render exactly what the facade answered, so the fixtures
// carry positions, states and counts only.
import { expect, test } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Inspector } from "./Inspector";
import { GRID_WINDOW, MessageGrid } from "./shell";
import {
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
} from "./testkit/fixtures";

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
  const previous = screen.getByRole("button", { name: `Previous ${GRID_WINDOW}` });
  const next = screen.getByRole("button", { name: `Next ${GRID_WINDOW}` });
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
  await user.type(screen.getByLabelText("Name"), "rejected acks");
  await user.click(screen.getByLabelText("ack"));
  await user.type(screen.getByLabelText("ACK outcomes"), "AA, AE");
  fireEvent.change(screen.getByLabelText("Field"), { target: { value: "MSA[1]-1[1]" } });
  await user.type(screen.getByLabelText("Value"), "AE");
  await user.click(screen.getByRole("button", { name: "Save and select" }));
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
  await user.type(screen.getByLabelText("Name"), "that afternoon");
  fireEvent.change(screen.getByLabelText("Observed from"), { target: { value: "2026-01-01T12:00" } });
  await user.click(screen.getByRole("button", { name: "Save and select" }));
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
    (screen.getByLabelText("Exact field, repetition, component or subcomponent") as HTMLInputElement)
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
  expect(screen.getByRole("button", { name: "Build case index" })).toBeTruthy();

  await user.click(screen.getByRole("button", { name: "Build case index" }));
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

  await user.click(screen.getByRole("button", { name: "Rebuild index" }));
  expect(screen.getByRole("form", { name: "Build index form" })).toBeTruthy();
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

  await user.click(screen.getByRole("button", { name: "Build case index" }));
  const customInput = screen.getByLabelText("Custom field selector");
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

