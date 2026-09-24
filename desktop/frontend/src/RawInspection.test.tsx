import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, test } from "vitest";
import type { InspectionRow, RawInspectionRequest, RawInspectionResult } from "./bindings";
import { RawInspection } from "./RawInspection";
import { renderApp } from "./testkit/app";
import { WORKSPACE_ROOT } from "./testkit/fixtures";
import { installFacade, type FacadeHandlers } from "./testkit/wails";

const FILE = `${WORKSPACE_ROOT}/raw/message.hl7`;
const FOLDER = `${WORKSPACE_ROOT}/copies`;
const DIGEST = "0".repeat(64);

/** Rows the way the facade answers them: positions, states and labels. The
 * value, when asked for, is an opaque escaped token, never an HL7 value. */
function rows(total: number, showValues: boolean): InspectionRow[] {
  const all: InspectionRow[] = [
    { kind: "message", message: 1, terminator: "cr", profile: "HL7 v2.5.1 field labels v1 (syntax only)", start: 0, end: 4096 },
    { kind: "segment", message: 1, segment: "PID", start: 60, end: 120 },
    {
      kind: "field",
      message: 1,
      segment: "PID",
      field: 3,
      label: "Patient Identifier List",
      state: "present",
      start: 70,
      end: 82,
      ...(showValues ? { value: '"escaped-token"' } : {}),
    },
    { kind: "repetition", message: 1, segment: "PID", field: 3, repetition: 2, state: "present", start: 76, end: 82 },
    { kind: "field", message: 1, segment: "PID", field: 8, label: "Administrative Sex", state: "omitted", start: 0, end: 0 },
    {
      kind: "field",
      message: 1,
      segment: "OBX",
      field: 5,
      label: "Observation Value",
      state: "present",
      start: 1000,
      end: 11000,
      // A 2-byte character starts at byte 4096, so the facade returns 4095 bytes.
      ...(showValues ? { value: '"escaped-leading-bytes"', value_truncated: true, value_shown_bytes: 4095 } : {}),
    },
  ];
  while (all.length < total) {
    const field = all.length;
    all.push({ kind: "field", message: 1, segment: "ZPD", field, state: "empty", start: 200 + field, end: 200 + field });
  }
  return all.slice(0, total);
}

function inspectionFor(total: number) {
  return (request: RawInspectionRequest): RawInspectionResult => {
    const all = rows(total, request.show_values);
    // Zero asks for the facade's own bound, which the answer names.
    const limit = request.limit || 200;
    return {
      state: "completed",
      inspection: {
        format: request.format === "auto" ? "raw" : request.format,
        format_selection: request.format === "auto" ? "detected" : "declared",
        terminator_selection: request.terminator === "auto" ? "detected" : "declared",
        messages: 1,
        bytes: 4096,
        sha256: DIGEST,
        show_values: request.show_values,
        offset: request.offset,
        limit,
        value_bytes: 4096,
        total,
        rows: all.slice(request.offset, request.offset + limit),
      },
    };
  };
}

function handlers(extra: FacadeHandlers = {}): FacadeHandlers {
  return {
    ChooseInspectionPath: (kind) => ({ state: "completed", kind, path: kind === "file" ? FILE : FOLDER }),
    InspectRawFile: inspectionFor(250),
    ...extra,
  };
}

async function openScreen() {
  const user = userEvent.setup();
  const toggle = screen.getByRole("button", { name: "Inspect a raw HL7 file" });
  expect(toggle.getAttribute("aria-expanded")).toBe("false");
  await user.click(toggle);
  expect(toggle.getAttribute("aria-expanded")).toBe("true");
  return user;
}

function rowsList() {
  return within(screen.getByRole("list", { name: "Inspection rows" }));
}

test("raw inspection chooses a file natively, pages every row the command reports and shows values only on request", async () => {
  const facade = installFacade(handlers());
  render(<RawInspection busy={false} indicators={new Map()} request={0} />);
  const user = await openScreen();
  const inspect = screen.getByRole("button", { name: "Inspect" });
  expect((inspect as HTMLButtonElement).disabled).toBe(true);
  expect(screen.getByText("Nothing has been read yet.")).toBeTruthy();

  await user.click(screen.getByRole("button", { name: "Choose a file to inspect…" }));
  expect(await screen.findByText(FILE)).toBeTruthy();
  expect(facade.oneCall("ChooseInspectionPath")).toEqual(["file"]);
  await user.selectOptions(screen.getByLabelText("Framing"), "mllp");
  await user.selectOptions(screen.getByLabelText("Segment terminator"), "cr");
  await user.click(inspect);

  expect(await screen.findByText("Rows 1–200 of 250")).toBeTruthy();
  expect(facade.oneCall("InspectRawFile")).toEqual([
    { file: FILE, format: "mllp", terminator: "cr", show_values: false, offset: 0, limit: 0 },
  ]);
  expect(screen.getByText(/Format: mllp \(declared\) · Messages: 1 · 4096 bytes/)).toBeTruthy();
  expect(screen.getByText(/Values are hidden/)).toBeTruthy();
  const list = rowsList();
  expect(list.getByText(/Message 1 · terminator cr \(declared\) · bytes \[0,4096\)/)).toBeTruthy();
  expect(list.getByText("PID-3 Patient Identifier List · present · 12 bytes")).toBeTruthy();
  expect(list.getByText("repetition 2 · present · 6 bytes")).toBeTruthy();
  // An omitted field has no bytes to count.
  expect(list.getByText("PID-8 Administrative Sex · omitted")).toBeTruthy();
  expect(list.getAllByRole("listitem")).toHaveLength(200);
  expect(screen.queryByText(/escaped-token/)).toBeNull();

  await user.click(screen.getByRole("button", { name: "Next rows" }));
  expect(await screen.findByText("Rows 201–250 of 250")).toBeTruthy();
  // A later page names the digest the shown rows were read from.
  expect(facade.callsTo("InspectRawFile")[1]?.args[0]).toMatchObject({ offset: 200, limit: 0, expect: DIGEST });
  expect(rowsList().getAllByRole("listitem")).toHaveLength(50);
  expect((screen.getByRole("button", { name: "Next rows" }) as HTMLButtonElement).disabled).toBe(true);
  await user.click(screen.getByRole("button", { name: "Previous rows" }));
  expect(await screen.findByText("Rows 1–200 of 250")).toBeTruthy();

  // Asking for values is a new declaration: what was shown without them goes.
  await user.click(screen.getByLabelText(/Show field values as escaped byte strings/));
  expect(screen.queryByRole("list", { name: "Inspection rows" })).toBeNull();
  await user.click(screen.getByRole("button", { name: "Inspect" }));
  expect(await screen.findByText('"escaped-token"')).toBeTruthy();
  // A field longer than the window shows says it is shown in part.
  const long = rowsList()
    .getAllByRole("listitem")
    .find((item) => item.textContent?.startsWith("OBX-5 Observation Value"));
  expect(long?.textContent).toBe(
    'OBX-5 Observation Value · present · 10000 bytes · "escaped-leading-bytes" · shown in part: the first 4095 of 10000 bytes',
  );
  expect(facade.callsTo("InspectRawFile").at(-1)?.args[0]).not.toHaveProperty("expect");
  expect(facade.callsTo("InspectRawFile").at(-1)?.args[0]).toMatchObject({ show_values: true, offset: 0 });
  expect(screen.queryByText(/Values are hidden/)).toBeNull();
  // Inspection writes nothing: no copy was asked for, so none was written.
  expect(facade.callsTo("WriteRoundTrip")).toHaveLength(0);
});

test("raw inspection reports refusals, a denied file and a dismissed dialog, and a new declaration clears what was shown", async () => {
  const facade = installFacade(
    handlers({ ChooseInspectionPath: () => ({ state: "cancelled", reason: "no file was chosen" }) }),
  );
  render(<RawInspection busy={false} indicators={new Map()} request={0} />);
  const user = await openScreen();
  await user.click(screen.getByRole("button", { name: "Choose a file to inspect…" }));
  expect(await screen.findByText("no file was chosen")).toBeTruthy();
  expect(screen.getByText("No file chosen.")).toBeTruthy();
  expect((screen.getByRole("button", { name: "Inspect" }) as HTMLButtonElement).disabled).toBe(true);

  facade.reply({
    ChooseInspectionPath: (kind) => ({ state: "completed", kind, path: FILE }),
    InspectRawFile: () => ({ state: "failed", reason: "truncated MLLP frame: missing end block" }),
  });
  await user.click(screen.getByRole("button", { name: "Choose a file to inspect…" }));
  expect(await screen.findByText(FILE)).toBeTruthy();
  expect(screen.queryByText("no file was chosen")).toBeNull();
  await user.click(screen.getByRole("button", { name: "Inspect" }));
  expect(await screen.findByText("truncated MLLP frame: missing end block")).toBeTruthy();
  expect(screen.getByText("failed")).toBeTruthy();
  expect(screen.queryByRole("list", { name: "Inspection rows" })).toBeNull();

  facade.reply({ InspectRawFile: () => ({ state: "permission_denied", reason: "cannot open input file" }) });
  await user.click(screen.getByRole("button", { name: "Inspect" }));
  expect(await screen.findByText("cannot open input file")).toBeTruthy();
  expect(screen.getByText("permission_denied")).toBeTruthy();

  facade.reply({ InspectRawFile: inspectionFor(250) });
  await user.click(screen.getByRole("button", { name: "Inspect" }));
  expect(await screen.findByText("Rows 1–200 of 250")).toBeTruthy();
  expect(screen.queryByText("cannot open input file")).toBeNull();
  // The file changed between two pages: the next page is refused, and the
  // rows of the earlier reading are not left beside the refusal.
  facade.reply({
    InspectRawFile: () => ({ state: "failed", reason: "the file changed since its earlier rows were read; inspect it again" }),
  });
  await user.click(screen.getByRole("button", { name: "Next rows" }));
  expect(await screen.findByText("the file changed since its earlier rows were read; inspect it again")).toBeTruthy();
  expect(screen.queryByRole("list", { name: "Inspection rows" })).toBeNull();

  facade.reply({ InspectRawFile: inspectionFor(5) });
  await user.click(screen.getByRole("button", { name: "Inspect" }));
  expect(await screen.findByText("Rows 1–5 of 5")).toBeTruthy();
  // A different declaration means a different reading of the same bytes.
  await user.selectOptions(screen.getByLabelText("Segment terminator"), "lf");
  expect(screen.queryByRole("list", { name: "Inspection rows" })).toBeNull();
  expect(screen.getByText("Nothing has been read yet.")).toBeTruthy();
});

test("a byte-identical copy is written to a new file of a natively chosen folder and a refused copy says why", async () => {
  const facade = installFacade(
    handlers({
      WriteRoundTrip: (request) => ({ state: "completed", path: `${request.folder}/${request.name}`, bytes: 4096, sha256: DIGEST }),
    }),
  );
  render(<RawInspection busy={false} indicators={new Map()} request={0} />);
  const user = await openScreen();
  const write = screen.getByRole("button", { name: "Write the copy" });
  expect((write as HTMLButtonElement).disabled).toBe(true);
  await user.click(screen.getByRole("button", { name: "Choose a file to inspect…" }));
  await screen.findByText(FILE);
  await user.click(screen.getByRole("button", { name: "Inspect" }));
  await screen.findByText("Rows 1–200 of 250");
  // A dismissed folder dialog is answered beside the copy, not the inspection.
  facade.reply({ ChooseInspectionPath: () => ({ state: "cancelled", reason: "no folder was chosen" }) });
  await user.click(screen.getByRole("button", { name: "Choose a folder for the copy…" }));
  const dismissed = await screen.findByText("no folder was chosen");
  expect(write.compareDocumentPosition(dismissed) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  expect(screen.getByText("Rows 1–200 of 250")).toBeTruthy();
  facade.reply({ ChooseInspectionPath: (kind) => ({ state: "completed", kind, path: kind === "file" ? FILE : FOLDER }) });
  await user.click(screen.getByRole("button", { name: "Choose a folder for the copy…" }));
  expect(await screen.findByText(FOLDER)).toBeTruthy();
  expect(screen.queryByText("no folder was chosen")).toBeNull();
  expect(facade.callsTo("ChooseInspectionPath").map((call) => call.args[0])).toEqual(["file", "round-trip-folder", "round-trip-folder"]);
  expect((write as HTMLButtonElement).disabled).toBe(true);
  await user.type(screen.getByLabelText("New file name"), "copy.hl7");
  await user.click(write);
  expect(await screen.findByText(/Wrote 4096 bytes to .*copy\.hl7 · SHA-256 0{64} · the same bytes the inspection read/)).toBeTruthy();
  expect(facade.oneCall("WriteRoundTrip")).toEqual([
    { file: FILE, format: "auto", terminator: "auto", folder: FOLDER, name: "copy.hl7" },
  ]);

  facade.reply({
    WriteRoundTrip: () => ({ state: "failed", reason: "cannot create round-trip file; destination must be new and writable" }),
  });
  await user.click(write);
  expect(await screen.findByText("cannot create round-trip file; destination must be new and writable")).toBeTruthy();
  expect(screen.queryByText(/Wrote 4096 bytes/)).toBeNull();
});

test("the palette opens raw inspection and a keyboard alone chooses, declares and inspects a file", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp(handlers());
  await user.keyboard("{Control>}k{/Control}");
  await user.type(screen.getByLabelText("Type a command"), "raw HL7{Enter}");
  const toggle = screen.getByRole("button", { name: "Inspect a raw HL7 file" });
  expect(toggle.getAttribute("aria-expanded")).toBe("true");
  expect(document.activeElement).toBe(toggle);

  await user.tab();
  expect(document.activeElement).toBe(screen.getByRole("button", { name: "Choose a file to inspect…" }));
  await user.keyboard("{Enter}");
  expect(await screen.findByText(FILE)).toBeTruthy();
  await user.tab();
  expect(document.activeElement).toBe(screen.getByLabelText("Framing"));
  await user.tab();
  expect(document.activeElement).toBe(screen.getByLabelText("Segment terminator"));
  await user.tab();
  await user.keyboard(" ");
  expect((screen.getByLabelText(/Show field values/) as HTMLInputElement).checked).toBe(true);
  await user.tab();
  expect(document.activeElement).toBe(screen.getByRole("button", { name: "Inspect" }));
  await user.keyboard("{Enter}");
  expect(await screen.findByText("Rows 1–200 of 250")).toBeTruthy();
  expect(facade.oneCall("InspectRawFile")[0]).toMatchObject({ file: FILE, format: "auto", terminator: "auto", show_values: true });
});
