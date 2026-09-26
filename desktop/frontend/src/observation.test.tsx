import { goTo } from "./testkit/navigation";
import { expect, test, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {
  CASE_ENTRY,
  WORKSPACE_ROOT,
  folderChosen,
  projectOverviewResult,
} from "./testkit/fixtures";
import { renderApp } from "./testkit/app";
import { expectTabPattern } from "./testkit/tabs";
import type {
  ObservationCollectFacadeRequest,
  ObservationCompletionResult,
  ObservationSource,
  ObservationSourceRequest,
  ObservationSourceResult,
  ObservationSupportResult,
  ObservationValidateResult,
  ObservationWindowRequest,
  ObservationWindowResult,
} from "./bindings";

/** The source the facade answers for a save of these choices: the document
 * it wrote, under the contract version it picked for the chosen kind, which
 * the test states as the facade's reader declares it. */
function savedSource(request: ObservationSourceRequest): ObservationSource {
  const choices = request.choices!;
  const versions: Record<string, string> = {
    "file-export": "readmit-observation-source/v1",
    "http-api": "readmit-observation-source/v1",
    "downstream-capture": "readmit-observation-source/v2",
    "database-query": "readmit-observation-source/v3",
  };
  return { schema: versions[choices.source.kind] ?? "", ...choices };
}

async function openProject(user: ReturnType<typeof userEvent.setup>, defaults = false) {
  const { facade } = await renderApp({
    SelectWorkspace: () =>
      folderChosen(WORKSPACE_ROOT, [
        { name: "project.json", kind: "project", schema: "readmit-project/v1" },
        { name: CASE_ENTRY, kind: "case", schema: "readmit-case/v3", provenance: "generated" },
      ]),
    OpenProjectOverview: () =>
      projectOverviewResult([
        {
          name: CASE_ENTRY,
          identity: "sha256:1111",
          schema: "readmit-case/v3",
          provenance: "generated",
          interface_version: "siu-2.5.1-v1",
          title: "Initial Case",
          status: "open",
          owner: "test-user",
          tags: ["test"],
          incidents: [],
          evidence: "verified",
        },
      ]),
    ObservationSupport: (): Promise<ObservationSupportResult> =>
      Promise.resolve({
        state: "completed",
        support: [
          {
            kind: "file-export",
            schema: "readmit-observation-source/v1",
            adapter: "file-export",
            version: "v1",
            qualification: "supported",
            production_claim: true,
          },
          {
            kind: "database-query",
            schema: "readmit-observation-source/v3",
            adapter: "postgresql",
            version: "lab-harness",
            qualification: "unqualified",
            production_claim: false,
            notes: "Live qualification evidence is owned by bharm16/readmit#75",
          },
        ],
      }),
    OpenObservationSource: (): Promise<ObservationSourceResult> =>
      Promise.resolve({
        state: "completed",
        source: {
          schema: "readmit-observation-source/v1",
          source: { kind: "file-export", identity: "scheduling-archive", scope: "appointments" },
          enabled: true,
          freshness: { max_age: "1h" },
          extraction: {
            envelope: "csv",
            encoding: "utf-8",
            csv: { delimiter: ",", record_separator: "lf", header: "present", fields: 2 },
            record_key: ["appointment"],
          },
          file: { path: "export.csv", max_bytes: 65536 },
          http: null,
          capture: null,
        },
        ...(defaults ? {} : { identity: "source-identity" }),
      }),
    OpenObservationWindow: (): Promise<ObservationWindowResult> =>
      Promise.resolve({
        state: "completed",
        window: {
          schema: "readmit-observation-window/v1",
          source: { kind: "file-export", identity: "scheduling-archive", scope: "appointments" },
          watermark: { kind: "none", position: "" },
          pre_existing_state: { declaration: "declared-empty", baseline_identity: "" },
          completion: {
            deadline: "30s",
            quiet_period: "2s",
            stable_samples: 3,
            max_records: 100,
            max_samples: 16,
          },
        },
        ...(defaults ? {} : { identity: "window-identity" }),
      }),
  });
  // The first-run card and the command region's action bar both offer the same
  // open-workspace action, so either button starts the same chooser.
  await user.click(screen.getAllByRole("button", { name: "Open" })[0]!);
  await within(screen.getByRole("region", { name: "Navigation" })).findByRole("button", { name: /^Project: / });
  await screen.findByRole("button", { name: "Project: Scheduling investigation" });
  return { facade };
}

test("opening observation editor never queries and shows qualification state", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user);
  const collect = vi.fn();
  facade.reply({
    CollectObservation: (req: ObservationCollectFacadeRequest): Promise<ObservationCompletionResult> => {
      collect(req);
      return Promise.resolve({ state: "failed", reason: "should not collect on open" });
    },
  });

  const evidence = screen.getByRole("region", { name: "Main content" });
  await goTo(user, "Environments");
  await user.click(within(evidence).getByRole("button", { name: "Observations" }));
  expect(await screen.findByRole("heading", { name: "Observations", level: 3 })).toBeTruthy();
  expect(screen.getByText(/Editor opened locally/)).toBeTruthy();
  expect(screen.getByText(/postgresql/)).toBeTruthy();
  expect(screen.getAllByText(/not a production claim/).length).toBeGreaterThan(0);
  // The selected type's qualification sits beside its choice, and the
  // support matrix is a named disclosure.
  expect(screen.getByRole("button", { name: "Database view" }).getAttribute("aria-describedby")).toBe("observation-kind-database-query");
  expect(document.getElementById("observation-kind-database-query")?.textContent).toBe("unqualified; not a production claim");
  expect(screen.getByText("Adapter support").tagName).toBe("SUMMARY");
  expect(collect).not.toHaveBeenCalled();
});

test("local validation and unauthorized collect stay separate", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user);
  const calls: ObservationCollectFacadeRequest[] = [];
  facade.reply({
    ValidateObservationPair: (): Promise<ObservationValidateResult> =>
      Promise.resolve({ state: "completed" }),
    CollectObservation: (req): Promise<ObservationCompletionResult> => {
      calls.push(req);
      if (!req.authorize) {
        return Promise.resolve({
          state: "failed",
          reason: "collection requires explicit authorization; opening an editor never queries a source",
        });
      }
      return Promise.resolve({
        state: "completed",
        summary: {
          supported: true,
          status: "complete",
          records_observed: 1,
          mapped_correlations: 1,
          unmapped_correlations: 0,
          stale: false,
          partial: false,
          trustworthy: true,
        },
      });
    },
  });

  const evidence = screen.getByRole("region", { name: "Main content" });
  await goTo(user, "Environments");
  await user.click(within(evidence).getByRole("button", { name: "Observations" }));
  await screen.findByRole("heading", { name: "Observations", level: 3 });

  await waitFor(() => expect(screen.getByText(byContent(/^Saved window ID/)).textContent).toBe("Saved window ID: window-identity (observation-window.json)"));
  await user.click(screen.getByRole("button", { name: "Validate saved configuration" }));
  expect(
    await screen.findByText(
      "Saved configuration valid: observation-source.json and observation-window.json as saved. No endpoint was queried.",
    ),
  ).toBeTruthy();
  expect(screen.getByText(/No endpoint is queried\./)).toBeTruthy();

  await user.click(screen.getByRole("tab", { name: "Collect and results" }));
  await user.click(screen.getByRole("button", { name: "Collect once" }));
  // Collect stays disabled until authorize is checked; force a click path via authorize.
  expect(calls).toHaveLength(0);
  await user.click(screen.getByLabelText(/I authorize a read-only collection/));
  await user.click(screen.getByRole("button", { name: "Collect once" }));
  await waitFor(() => expect(calls).toHaveLength(1));
  expect(calls[0]?.authorize).toBe(true);
  // The collection names the reviewed saved pair by the identities it was
  // authorized with, so a document changed on disk since is refused.
  expect(calls[0]?.expected_source_identity).toBe("source-identity");
  expect(calls[0]?.expected_window_identity).toBe("window-identity");
  expect(await screen.findByText(/Absence claim:/)).toBeTruthy();
  // One authorization is one collection.
  expect((screen.getByLabelText(/I authorize a read-only collection/) as HTMLInputElement).checked).toBe(false);
});

test("denied and stale completion summaries stay distinct from absence", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user);
  facade.reply({
    ExplainObservation: (): Promise<ObservationCompletionResult> =>
      Promise.resolve({
        state: "completed",
        summary: {
          supported: false,
          reason: "the source returned state predating the window's watermark",
          status: "stale",
          records_observed: 0,
          mapped_correlations: 0,
          unmapped_correlations: 0,
          stale: true,
          partial: false,
          trustworthy: false,
        },
      }),
  });
  const evidence = screen.getByRole("region", { name: "Main content" });
  await goTo(user, "Environments");
  await user.click(within(evidence).getByRole("button", { name: "Observations" }));
  await screen.findByRole("heading", { name: "Observations", level: 3 });
  await user.click(screen.getByRole("tab", { name: "Collect and results" }));
  await user.click(screen.getByRole("button", { name: "Explain completion" }));
  expect(await screen.findByText(/Status:/)).toBeTruthy();
  expect(screen.getByText("stale")).toBeTruthy();
  expect(screen.getByText(/not supported/)).toBeTruthy();
  expect(screen.getByText(/never evidence of no output/)).toBeTruthy();
});

test("a source the facade answered is saved again as the person's choices, never under a version the window picked, and a refused save says so", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user);
  const saved: ObservationSourceRequest[] = [];
  facade.reply({
    SaveObservationSource: (request): Promise<ObservationSourceResult> => {
      saved.push(request);
      // The facade answers with the source struct, whose capture transport
      // is an explicit null even for a v1 document.
      return Promise.resolve({ state: "completed", source: { ...savedSource(request), capture: null }, identity: "source-identity" });
    },
    SaveObservationWindow: (request): Promise<ObservationWindowResult> =>
      Promise.resolve({ state: "completed", window: request.window!, identity: "window-identity" }),
  });
  const evidence = screen.getByRole("region", { name: "Main content" });
  await goTo(user, "Environments");
  await user.click(within(evidence).getByRole("button", { name: "Observations" }));
  await screen.findByRole("heading", { name: "Observations", level: 3 });
  await user.click(screen.getByRole("button", { name: "Save observation" }));
  expect(await screen.findByText(/^Saved through shared Go writers/)).toBeTruthy();
  await user.clear(screen.getByLabelText("Export file"));
  await user.type(screen.getByLabelText("Export file"), "never-exported.csv");
  await user.click(screen.getByRole("button", { name: "Save observation" }));
  await waitFor(() => expect(saved).toHaveLength(2));
  // What the person declared goes back, and the contract version is the
  // facade's to pick: no document and no version is sent.
  expect(saved[1]?.choices?.file?.path).toBe("never-exported.csv");
  expect(saved[1]?.source).toBeUndefined();
  expect(saved[1]?.choices && "schema" in saved[1].choices).toBe(false);
  expect(JSON.stringify(saved[1])).not.toContain("readmit-observation-source/");

  facade.reply({
    SaveObservationSource: (): Promise<ObservationSourceResult> =>
      Promise.resolve({ state: "failed", reason: "the source document could not be written" }),
  });
  await user.click(screen.getByRole("button", { name: "Save observation" }));
  expect(
    await screen.findByText(/^Not saved: the source document could not be written\. A collection reads the documents saved before\./),
  ).toBeTruthy();
  expect(screen.queryByText(/^Saved through shared Go writers/)).toBeNull();
});

// A source switched to another kind and back is sent with only the transport
// its kind declares, and never with a contract version: the facade picks the
// version the chosen kind needs.
test("a source switched between kinds is saved as the choices of its own kind, and the facade picks the version", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user);
  const saved: ObservationSourceRequest[] = [];
  facade.reply({
    SaveObservationSource: (request): Promise<ObservationSourceResult> => {
      saved.push(request);
      return Promise.resolve({ state: "completed", source: savedSource(request), identity: "source-identity" });
    },
    SaveObservationWindow: (request): Promise<ObservationWindowResult> =>
      Promise.resolve({ state: "completed", window: request.window!, identity: "window-identity" }),
  });
  const panel = await openObservationSetup(user);
  await user.click(panel.getByRole("button", { name: "HTTPS API" }));
  await user.click(panel.getByRole("button", { name: "File export" }));
  await user.click(panel.getByRole("button", { name: "Save observation" }));
  await waitFor(() => expect(saved).toHaveLength(1));
  expect(saved[0]?.choices?.source.kind).toBe("file-export");
  expect(saved[0]?.choices?.file).not.toBeNull();
  expect(saved[0]?.choices?.http).toBeNull();
  expect(saved[0]?.choices?.capture).toBeNull();
  expect(saved[0]?.choices && "database" in saved[0].choices).toBe(false);
  expect(saved[0]?.choices && "schema" in saved[0].choices).toBe(false);
  expect(await panel.findByText("Source document observation-source.json: saved readmit-observation-source/v1, identity source-identity.")).toBeTruthy();

  await user.click(panel.getByRole("button", { name: "Downstream capture" }));
  await user.click(panel.getByRole("button", { name: "Save observation" }));
  await waitFor(() => expect(saved).toHaveLength(2));
  expect(saved[1]?.choices?.source.kind).toBe("downstream-capture");
  expect(saved[1]?.choices?.capture).not.toBeNull();
  expect(saved[1]?.choices && "database" in saved[1].choices).toBe(false);
  expect(saved[1]?.choices && "schema" in saved[1].choices).toBe(false);
  expect(await panel.findByText("Source document observation-source.json: saved readmit-observation-source/v2, identity source-identity.")).toBeTruthy();
});

/** The innermost element whose whole text matches: a line the panel composes
 * from several elements. */
function byContent(pattern: RegExp): (content: string, element: Element | null) => boolean {
  return (_content, element) =>
    element !== null &&
    pattern.test(element.textContent ?? "") &&
    !Array.from(element.children).some((child) => pattern.test(child.textContent ?? ""));
}

async function tabTo(user: ReturnType<typeof userEvent.setup>, target: HTMLElement) {
  for (let step = 0; step < 200 && document.activeElement !== target; step++) await user.tab();
  expect(document.activeElement).toBe(target);
}

async function openObservationSetup(user: ReturnType<typeof userEvent.setup>) {
  const evidence = screen.getByRole("region", { name: "Main content" });
  await goTo(user, "Environments");
  await user.click(within(evidence).getByRole("button", { name: "Observations" }));
  return within(await screen.findByRole("region", { name: "Observation setup" }));
}

type Panel = Awaited<ReturnType<typeof openObservationSetup>>;

/** Names a document in its file field; nothing is read until Open. */
async function nameFile(user: ReturnType<typeof userEvent.setup>, panel: Panel, kind: "Source" | "Window", name: string) {
  await user.clear(panel.getByLabelText(`${kind} file`));
  await user.type(panel.getByLabelText(`${kind} file`), name);
}

/** Names a document and presses its Open action. */
async function openFile(user: ReturnType<typeof userEvent.setup>, panel: Panel, kind: "Source" | "Window", name: string) {
  await nameFile(user, panel, kind, name);
  await user.click(panel.getByRole("button", { name: `Open ${kind.toLowerCase()}` }));
}

/** The saved-configuration summary's two lines. */
function savedIDs(panel: Panel): [string, string] {
  return [
    panel.getByText(byContent(/^Saved source ID/)).textContent ?? "",
    panel.getByText(byContent(/^Saved window ID/)).textContent ?? "",
  ];
}

test("new observation defaults have no pinned identity until each document is saved", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user, true);
  facade.reply({
    SaveObservationSource: (request): Promise<ObservationSourceResult> =>
      Promise.resolve({ state: "completed", source: savedSource(request), identity: "saved-source-identity" }),
    SaveObservationWindow: (request): Promise<ObservationWindowResult> =>
      Promise.resolve({ state: "completed", window: request.window!, identity: "saved-window-identity" }),
  });
  const panel = await openObservationSetup(user);
  await waitFor(() => expect(savedIDs(panel)).toEqual(["Saved source ID: not saved", "Saved window ID: not saved"]));
  await user.click(panel.getByRole("tab", { name: "Completion rules" }));
  await waitFor(() => expect((panel.getByLabelText("Pre-existing state") as HTMLSelectElement).disabled).toBe(false));
  await user.selectOptions(panel.getByLabelText("Pre-existing state"), "Unknown");
  await waitFor(() => expect((panel.getByRole("button", { name: "Save observation" }) as HTMLButtonElement).disabled).toBe(false));
  await user.click(panel.getByRole("button", { name: "Save observation" }));
  await panel.findByText(/^Saved through shared Go writers/);
  expect(savedIDs(panel)).toEqual([
    "Saved source ID: saved-source-identity (observation-source.json)",
    "Saved window ID: saved-window-identity (observation-window.json)",
  ]);
});

test("opening another source is explicit: a typed name reads nothing, Open asks before replacing unsaved edits, and Escape keeps them", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user);
  const panel = await openObservationSetup(user);
  await waitFor(() => expect((panel.getByLabelText("Export file") as HTMLInputElement).disabled).toBe(false));
  await user.clear(panel.getByLabelText("Export file"));
  await user.type(panel.getByLabelText("Export file"), "edited.csv");
  const reads = facade.callsTo("OpenObservationSource").length;
  // Typing a name only stages it.
  await nameFile(user, panel, "Source", "other-source.json");
  expect(facade.callsTo("OpenObservationSource")).toHaveLength(reads);
  expect(panel.queryByRole("group", { name: /Replace unsaved source edits/ })).toBeNull();
  await user.click(panel.getByRole("button", { name: "Open source" }));
  expect(panel.getByRole("group", { name: "Replace unsaved source edits?" })).toBeTruthy();
  expect(panel.getByText(/Opening other-source\.json will replace those unsaved edits\./)).toBeTruthy();
  expect(facade.callsTo("OpenObservationSource")).toHaveLength(reads);
  expect((panel.getByLabelText("Export file") as HTMLInputElement).value).toBe("edited.csv");
  await user.keyboard("{Escape}");
  expect(panel.queryByRole("group", { name: /Replace unsaved source edits/ })).toBeNull();
  expect((panel.getByLabelText("Source file") as HTMLInputElement).value).toBe("observation-source.json");
  expect((panel.getByLabelText("Export file") as HTMLInputElement).value).toBe("edited.csv");
  expect(facade.callsTo("OpenObservationSource")).toHaveLength(reads);

  await openFile(user, panel, "Source", "other-source.json");
  await user.click(panel.getByRole("button", { name: "Open selected source" }));
  await waitFor(() => expect(facade.callsTo("OpenObservationSource").length).toBeGreaterThan(reads));
  expect(facade.callsTo("OpenObservationSource").at(-1)?.args).toEqual([WORKSPACE_ROOT, "other-source.json"]);
  await waitFor(() => expect((panel.getByLabelText("Export file") as HTMLInputElement).value).toBe("export.csv"));
  expect((panel.getByLabelText("Source file") as HTMLInputElement).value).toBe("other-source.json");

  await user.clear(panel.getByLabelText("Export file"));
  await user.type(panel.getByLabelText("Export file"), "still-edited.csv");
  facade.reply({
    OpenObservationSource: (): Promise<ObservationSourceResult> =>
      Promise.resolve({ state: "failed", reason: "later source version" }),
  });
  await openFile(user, panel, "Source", "later-source.json");
  await user.click(panel.getByRole("button", { name: "Open selected source" }));
  await panel.findByText("Source document later-source.json not opened: later source version");
  expect((panel.getByLabelText("Export file") as HTMLInputElement).value).toBe("still-edited.csv");
  await openFile(user, panel, "Source", "third-source.json");
  expect(panel.getByRole("group", { name: "Replace unsaved source edits?" })).toBeTruthy();
  expect((panel.getByLabelText("Export file") as HTMLInputElement).value).toBe("still-edited.csv");
});

test("opening another window asks before replacing unsaved edits, and Keep retains them", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user);
  const panel = await openObservationSetup(user);
  await user.click(panel.getByRole("tab", { name: "Completion rules" }));
  await waitFor(() => expect((panel.getByLabelText("Collection deadline") as HTMLInputElement).disabled).toBe(false));
  await user.clear(panel.getByLabelText("Collection deadline"));
  await user.type(panel.getByLabelText("Collection deadline"), "45s");
  const reads = facade.callsTo("OpenObservationWindow").length;
  await openFile(user, panel, "Window", "other-window.json");
  expect(panel.getByRole("group", { name: "Replace unsaved window edits?" })).toBeTruthy();
  expect(facade.callsTo("OpenObservationWindow")).toHaveLength(reads);
  await user.click(panel.getByRole("button", { name: "Keep window edits" }));
  expect((panel.getByLabelText("Window file") as HTMLInputElement).value).toBe("observation-window.json");
  expect((panel.getByLabelText("Collection deadline") as HTMLInputElement).value).toBe("45s");
  expect(facade.callsTo("OpenObservationWindow")).toHaveLength(reads);

  facade.reply({
    OpenObservationWindow: (): Promise<ObservationWindowResult> =>
      Promise.resolve({ state: "failed", reason: "later window version" }),
  });
  await openFile(user, panel, "Window", "later-window.json");
  await user.click(panel.getByRole("button", { name: "Open selected window" }));
  await panel.findByText("Window document later-window.json not opened: later window version");
  expect((panel.getByLabelText("Collection deadline") as HTMLInputElement).value).toBe("45s");
  await openFile(user, panel, "Window", "third-window.json");
  expect(panel.getByRole("group", { name: "Replace unsaved window edits?" })).toBeTruthy();
});

test("a newly opened document does not show the previous file's identity while its read is pending", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user);
  const panel = await openObservationSetup(user);
  await waitFor(() =>
    expect(savedIDs(panel)).toEqual([
      "Saved source ID: source-identity (observation-source.json)",
      "Saved window ID: window-identity (observation-window.json)",
    ]),
  );
  const reading = facade.park("OpenObservationSource");
  await openFile(user, panel, "Source", "new-source.json");
  await waitFor(() => expect(reading.size).toBeGreaterThan(0));
  expect(savedIDs(panel)).toEqual(["Saved source ID: not saved", "Saved window ID: window-identity (observation-window.json)"]);
  await waitFor(() => {
    while (reading.size > 0) reading.resolve({ state: "failed", reason: "not found" });
    expect(panel.getByText("Source document new-source.json not opened: not found")).toBeTruthy();
  });
  const windowReading = facade.park("OpenObservationWindow");
  await openFile(user, panel, "Window", "new-window.json");
  await waitFor(() => expect(windowReading.size).toBeGreaterThan(0));
  expect(savedIDs(panel)).toEqual(["Saved source ID: not saved", "Saved window ID: not saved"]);
  await waitFor(() => {
    while (windowReading.size > 0) windowReading.resolve({ state: "failed", reason: "not found" });
    expect(panel.getByText("Window document new-window.json not opened: not found")).toBeTruthy();
  });
});

test("each saved document is validated on its own from the keyboard, with its identity or the reader's refusal", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user);
  facade.reply({
    ValidateObservationSource: (): Promise<ObservationSourceResult> =>
      Promise.resolve({
        state: "completed",
        source: {
          schema: "readmit-observation-source/v1",
          source: { kind: "file-export", identity: "scheduling-archive", scope: "appointments" },
          enabled: true,
          freshness: { max_age: "1h" },
          extraction: { envelope: "csv", encoding: "utf-8", record_key: ["appointment"] },
          file: { path: "export.csv", max_bytes: 65536 },
          http: null,
          capture: null,
        },
        source_file: "observation-source.json",
        identity: "saved-source-identity",
      }),
    ValidateObservationWindow: (): Promise<ObservationWindowResult> =>
      Promise.resolve({ state: "failed", reason: "a window whose quiet period outlasts its deadline can never complete" }),
  });
  const panel = await openObservationSetup(user);

  await tabTo(user, panel.getByRole("button", { name: "Validate saved source" }));
  await user.keyboard("{Enter}");
  expect(
    await panel.findByText(
      "Source document observation-source.json: valid readmit-observation-source/v1, identity saved-source-identity. Nothing was collected.",
    ),
  ).toBeTruthy();
  await tabTo(user, panel.getByRole("button", { name: "Validate saved window" }));
  await user.keyboard(" ");
  expect(
    await panel.findByText(
      "Window document observation-window.json refused: a window whose quiet period outlasts its deadline can never complete",
    ),
  ).toBeTruthy();
  // Each action read the document its own field names, and nothing else.
  expect(facade.callsTo("ValidateObservationSource").map((call) => call.args)).toEqual([[WORKSPACE_ROOT, "observation-source.json"]]);
  expect(facade.callsTo("ValidateObservationWindow").map((call) => call.args)).toEqual([[WORKSPACE_ROOT, "observation-window.json"]]);
  expect(facade.callsTo("ValidateObservationPair")).toHaveLength(0);
  expect(facade.callsTo("CollectObservation")).toHaveLength(0);
});

test("a refused source save leaves the window as it was saved, and a refused window save says the source was saved", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user);
  facade.reply({
    SaveObservationSource: (): Promise<ObservationSourceResult> =>
      Promise.resolve({ state: "failed", reason: "cannot write an observation source here" }),
    SaveObservationWindow: (request): Promise<ObservationWindowResult> =>
      Promise.resolve({ state: "completed", window: request.window!, identity: "window-identity-2" }),
  });
  const panel = await openObservationSetup(user);
  await waitFor(() => expect((panel.getByRole("button", { name: "Save observation" }) as HTMLButtonElement).disabled).toBe(false));
  await user.click(panel.getByRole("button", { name: "Save observation" }));
  expect(
    await panel.findByText("Not saved: cannot write an observation source here. A collection reads the documents saved before."),
  ).toBeTruthy();
  expect(panel.getByText("Source document observation-source.json not saved: cannot write an observation source here")).toBeTruthy();
  expect(facade.callsTo("SaveObservationWindow")).toHaveLength(0);
  expect(savedIDs(panel)).toEqual([
    "Saved source ID: source-identity (observation-source.json)",
    "Saved window ID: window-identity (observation-window.json)",
  ]);

  facade.reply({
    SaveObservationSource: (request): Promise<ObservationSourceResult> =>
      Promise.resolve({ state: "completed", source: savedSource(request), identity: "source-identity-2" }),
    SaveObservationWindow: (): Promise<ObservationWindowResult> =>
      Promise.resolve({ state: "permission_denied", reason: "authoring requires an active license" }),
  });
  await tabTo(user, panel.getByRole("button", { name: "Save observation" }));
  await user.keyboard("{Enter}");
  expect(
    await panel.findByText(
      "Window not saved: authoring requires an active license. The source was saved; a collection reads the window saved before.",
    ),
  ).toBeTruthy();
  expect(panel.getByText("Source document observation-source.json: saved readmit-observation-source/v1, identity source-identity-2.")).toBeTruthy();
  expect(panel.getByText("Window document observation-window.json not saved: authoring requires an active license")).toBeTruthy();
  expect(savedIDs(panel)).toEqual([
    "Saved source ID: source-identity-2 (observation-source.json)",
    "Saved window ID: window-identity (observation-window.json)",
  ]);
  // Neither editor has unsaved edits now, yet the pair is a new source with
  // the old window: Collect stays closed until both are saved.
  await user.click(panel.getByRole("tab", { name: "Collect and results" }));
  expect((panel.getByLabelText(/I authorize a read-only collection/) as HTMLInputElement).disabled).toBe(true);
  expect((panel.getByRole("button", { name: "Collect once" }) as HTMLButtonElement).disabled).toBe(true);
  expect(panel.getByText("The last save did not write both documents. Save observation again before collecting.")).toBeTruthy();
  facade.reply({
    SaveObservationWindow: (request): Promise<ObservationWindowResult> =>
      Promise.resolve({ state: "completed", window: request.window!, identity: "window-identity-2" }),
  });
  await user.click(panel.getByRole("button", { name: "Save observation" }));
  await panel.findByText("Saved through shared Go writers. Identities pinned for test binding.");
  expect((panel.getByLabelText(/I authorize a read-only collection/) as HTMLInputElement).disabled).toBe(false);
});

test("a document the reader refuses is said to be refused on opening, and opening one document never reads the other again", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user);
  const panel = await openObservationSetup(user);
  await waitFor(() => expect(facade.callsTo("OpenObservationWindow").length).toBeGreaterThan(0));
  facade.reply({
    OpenObservationWindow: (_workspace, windowFile): Promise<ObservationWindowResult> =>
      Promise.resolve(
        windowFile === "later.json"
          ? { state: "failed", reason: "an observation window must declare readmit-observation-window/v1" }
          : {
              state: "completed",
              window: {
                schema: "readmit-observation-window/v1",
                source: { kind: "file-export", identity: "scheduling-archive", scope: "appointments" },
                watermark: { kind: "none", position: "" },
                pre_existing_state: { declaration: "declared-empty", baseline_identity: "" },
                completion: { deadline: "30s", quiet_period: "2s", stable_samples: 3, max_records: 100, max_samples: 16 },
              },
              identity: "window-identity",
            },
      ),
  });
  const sourceReads = facade.callsTo("OpenObservationSource").length;
  await waitFor(() => expect((panel.getByLabelText("Export file") as HTMLInputElement).disabled).toBe(false));
  await user.clear(panel.getByLabelText("Export file"));
  await user.type(panel.getByLabelText("Export file"), "exports/appointments.csv");
  await openFile(user, panel, "Window", "later.json");
  expect(
    await panel.findByText("Window document later.json not opened: an observation window must declare readmit-observation-window/v1"),
  ).toBeTruthy();
  expect(savedIDs(panel)).toEqual(["Saved source ID: source-identity (observation-source.json)", "Saved window ID: not saved"]);
  // The source was not read again, so what was typed into it stands.
  expect(facade.callsTo("OpenObservationSource")).toHaveLength(sourceReads);
  expect((panel.getByLabelText("Export file") as HTMLInputElement).value).toBe("exports/appointments.csv");
  // The refused window is never replaced by what the editor holds.
  expect((panel.getByRole("button", { name: "Save observation" }) as HTMLButtonElement).disabled).toBe(true);
  expect(panel.getByText(/^Saving is closed while a named document is refused/)).toBeTruthy();
  await openFile(user, panel, "Window", "observation-window.json");
  await waitFor(() => expect((panel.getByRole("button", { name: "Save observation" }) as HTMLButtonElement).disabled).toBe(false));
  expect(panel.queryByText(/^Saving is closed/)).toBeNull();
  expect(facade.callsTo("SaveObservationSource")).toHaveLength(0);
  expect(facade.callsTo("SaveObservationWindow")).toHaveLength(0);
});

test("an edit abandoned by closing the panel from the keyboard writes nothing, and the panel opened again reads the saved document", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user);
  let panel = await openObservationSetup(user);
  await waitFor(() => expect((panel.getByLabelText("Export file") as HTMLInputElement).disabled).toBe(false));
  await user.clear(panel.getByLabelText("Export file"));
  await user.type(panel.getByLabelText("Export file"), "exports/elsewhere.csv");
  const reads = facade.callsTo("OpenObservationSource").length;
  // Close is above the editor: Shift+Tab reaches it from the field.
  const close = panel.getByRole("button", { name: "Close" });
  for (let step = 0; step < 80 && document.activeElement !== close; step++) await user.tab({ shift: true });
  expect(document.activeElement).toBe(close);
  await user.keyboard("{Enter}");
  await waitFor(() => expect(screen.queryByRole("region", { name: "Observation setup" })).toBeNull());
  expect(facade.callsTo("SaveObservationSource")).toHaveLength(0);
  expect(facade.callsTo("SaveObservationWindow")).toHaveLength(0);
  panel = await openObservationSetup(user);
  await waitFor(() => expect(facade.callsTo("OpenObservationSource").length).toBeGreaterThan(reads));
  await waitFor(() => expect((panel.getByLabelText("Export file") as HTMLInputElement).value).toBe("export.csv"));
  expect(savedIDs(panel)).toEqual([
    "Saved source ID: source-identity (observation-source.json)",
    "Saved window ID: window-identity (observation-window.json)",
  ]);
});

test("the editor stays closed while a document is read, so a late read never replaces what was typed", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user);
  const reading = facade.park("OpenObservationSource");
  const panel = await openObservationSetup(user);
  await waitFor(() => expect(reading.size).toBeGreaterThan(0));
  const exportPath = panel.getByLabelText("Export file") as HTMLInputElement;
  // Nothing can be typed, saved or validated while the document it would
  // replace is being read; the document's name can still be typed.
  expect(exportPath.disabled).toBe(true);
  expect((panel.getByRole("button", { name: "Save observation" }) as HTMLButtonElement).disabled).toBe(true);
  expect((panel.getByRole("button", { name: "Validate saved source" }) as HTMLButtonElement).disabled).toBe(true);
  expect((panel.getByLabelText("Source file") as HTMLInputElement).disabled).toBe(false);
  const answer: ObservationSourceResult = {
    state: "completed",
    source: fileSource("exports/appointments.csv"),
    identity: "source-identity",
  };
  // Every read the panel started answers, and the last one fills the editor.
  await waitFor(() => {
    while (reading.size > 0) reading.resolve(answer);
    expect(exportPath.value).toBe("exports/appointments.csv");
  });
  await waitFor(() => expect(exportPath.disabled).toBe(false));
  expect(exportPath.value).toBe("exports/appointments.csv");
});

/** A file-export source reading the named export. */
function fileSource(path: string): ObservationSource {
  return {
    schema: "readmit-observation-source/v1",
    source: { kind: "file-export", identity: "scheduling-archive", scope: "appointments" },
    enabled: true,
    freshness: { max_age: "1h" },
    extraction: { envelope: "csv", encoding: "utf-8", record_key: ["appointment"] },
    file: { path, max_bytes: 65536 },
    http: null,
    capture: null,
  };
}

test("a slow open of another source, answered after a later one, never commits into the editor", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user);
  const panel = await openObservationSetup(user);
  await waitFor(() => expect((panel.getByLabelText("Export file") as HTMLInputElement).disabled).toBe(false));
  const reading = facade.park("OpenObservationSource");
  await openFile(user, panel, "Source", "slow.json");
  await waitFor(() => expect(reading.size).toBe(1));
  await openFile(user, panel, "Source", "fast.json");
  await waitFor(() => expect(reading.size).toBe(2));
  // The read of slow.json answers after fast.json was asked for: it is not
  // the current read and commits nothing.
  reading.resolve({ state: "completed", source: fileSource("slow.csv"), identity: "slow-identity" });
  reading.resolve({ state: "completed", source: fileSource("fast.csv"), identity: "fast-identity" });
  await waitFor(() => expect((panel.getByLabelText("Export file") as HTMLInputElement).value).toBe("fast.csv"));
  expect(savedIDs(panel)[0]).toBe("Saved source ID: fast-identity (fast.json)");
  expect(panel.queryByText(/slow/)).toBeNull();
});

test("collection is closed while the editor differs from the reviewed saved pair, and authorization is withdrawn by any change", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user);
  const collected: ObservationCollectFacadeRequest[] = [];
  facade.reply({
    SaveObservationSource: (request): Promise<ObservationSourceResult> =>
      Promise.resolve({ state: "completed", source: savedSource(request), identity: "source-identity-2" }),
    SaveObservationWindow: (request): Promise<ObservationWindowResult> =>
      Promise.resolve({ state: "completed", window: request.window!, identity: "window-identity-2" }),
    CollectObservation: (request): Promise<ObservationCompletionResult> => {
      collected.push(request);
      return Promise.resolve({
        state: "failed",
        reason: "the saved observation source changed since it was reviewed; open it again before collecting",
      });
    },
  });
  const panel = await openObservationSetup(user);
  await user.click(panel.getByRole("tab", { name: "Collect and results" }));
  const authorize = () => panel.getByLabelText(/I authorize a read-only collection/) as HTMLInputElement;
  const collect = () => panel.getByRole("button", { name: "Collect once" }) as HTMLButtonElement;
  await waitFor(() => expect(authorize().disabled).toBe(false));
  await user.click(authorize());
  expect(collect().disabled).toBe(false);
  expect(panel.getByText(/This authorizes one collection of the saved source observation-source\.json/)).toBeTruthy();

  // An edit withdraws the authorization and closes collection with a reason
  // that routes to Save; nothing is saved on its own.
  await user.click(panel.getByRole("tab", { name: "Source" }));
  await user.clear(panel.getByLabelText("Export file"));
  await user.type(panel.getByLabelText("Export file"), "exports/new.csv");
  expect(panel.getByText("The editor has unsaved edits; these saved IDs do not represent them until the observation is saved.")).toBeTruthy();
  await user.click(panel.getByRole("tab", { name: "Collect and results" }));
  expect(authorize().checked).toBe(false);
  expect(authorize().disabled).toBe(true);
  expect(collect().disabled).toBe(true);
  expect(
    panel.getByText("The editor has unsaved edits. Save observation first: Collect reads only the saved documents, never the editor."),
  ).toBeTruthy();
  expect(collect().getAttribute("aria-describedby")).toBe("observation-collect-blocked");
  expect(facade.callsTo("SaveObservationSource")).toHaveLength(0);

  // A staged file name that is not open closes it as well.
  await user.click(panel.getByRole("button", { name: "Save observation" }));
  await panel.findByText("Saved through shared Go writers. Identities pinned for test binding.");
  expect(authorize().checked).toBe(false);
  await nameFile(user, panel, "Window", "elsewhere.json");
  expect(authorize().disabled).toBe(true);
  expect(
    panel.getByText("The file fields name documents that are not open. Open them, or restore the open names, before collecting."),
  ).toBeTruthy();
  await nameFile(user, panel, "Window", "observation-window.json");

  // Collect sends the saved pair it was authorized for, and the facade's
  // refusal of a document changed on disk since is shown, not a result.
  await user.click(authorize());
  await user.click(collect());
  await waitFor(() => expect(collected).toHaveLength(1));
  expect(collected[0]).toMatchObject({
    source_file: "observation-source.json",
    window_file: "observation-window.json",
    authorize: true,
    expected_source_identity: "source-identity-2",
    expected_window_identity: "window-identity-2",
  });
  expect(
    await panel.findByText("the saved observation source changed since it was reviewed; open it again before collecting", { selector: "p" }),
  ).toBeTruthy();
  expect(panel.queryByText(/^Status:/)).toBeNull();
});

test("saving twice at once writes the pair once", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user);
  const panel = await openObservationSetup(user);
  await waitFor(() => expect((panel.getByRole("button", { name: "Save observation" }) as HTMLButtonElement).disabled).toBe(false));
  const sourceSaves = facade.park("SaveObservationSource");
  const save = panel.getByRole("button", { name: "Save observation" });
  await user.dblClick(save);
  await user.click(save);
  await waitFor(() => expect(sourceSaves.size).toBe(1));
  expect(facade.callsTo("SaveObservationSource")).toHaveLength(1);
  // While the pair is being written, collection stays closed.
  await user.click(panel.getByRole("tab", { name: "Collect and results" }));
  expect((panel.getByLabelText(/I authorize a read-only collection/) as HTMLInputElement).disabled).toBe(true);
  expect(panel.getByText("The observation is being saved.")).toBeTruthy();
  facade.reply({
    SaveObservationWindow: (request): Promise<ObservationWindowResult> =>
      Promise.resolve({ state: "completed", window: request.window!, identity: "window-identity" }),
  });
  sourceSaves.resolve({ state: "completed", source: fileSource("export.csv"), identity: "source-identity" });
  await panel.findByText("Saved through shared Go writers. Identities pinned for test binding.");
  expect(facade.callsTo("SaveObservationSource")).toHaveLength(1);
  expect(facade.callsTo("SaveObservationWindow")).toHaveLength(1);
});

test("validating the saved configuration names the saved files and says unsaved edits were not validated", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user);
  facade.reply({
    ValidateObservationPair: (): Promise<ObservationValidateResult> => Promise.resolve({ state: "completed" }),
  });
  const panel = await openObservationSetup(user);
  await waitFor(() => expect((panel.getByLabelText("Export file") as HTMLInputElement).disabled).toBe(false));
  await user.clear(panel.getByLabelText("Export file"));
  await user.type(panel.getByLabelText("Export file"), "draft-only.csv");
  await user.click(panel.getByRole("button", { name: "Validate saved configuration" }));
  expect(
    await panel.findByText(
      "Saved configuration valid: observation-source.json and observation-window.json as saved. No endpoint was queried. Unsaved edits in the editor were not validated.",
    ),
  ).toBeTruthy();
  expect(facade.callsTo("ValidateObservationPair").map((call) => call.args)).toEqual([
    [{ workspace: WORKSPACE_ROOT, source_file: "observation-source.json", window_file: "observation-window.json" }],
  ]);
  expect(facade.callsTo("SaveObservationSource")).toHaveLength(0);
  expect(facade.callsTo("CollectObservation")).toHaveLength(0);
});

test("database filters are controlled rows that survive edit, save and reopening", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user);
  const saved: ObservationSourceRequest[] = [];
  facade.reply({
    SaveObservationSource: (request): Promise<ObservationSourceResult> => {
      saved.push(request);
      return Promise.resolve({ state: "completed", source: savedSource(request), identity: "db-identity" });
    },
    SaveObservationWindow: (request): Promise<ObservationWindowResult> =>
      Promise.resolve({ state: "completed", window: request.window!, identity: "window-identity" }),
  });
  const panel = await openObservationSetup(user);
  await waitFor(() => expect((panel.getByRole("button", { name: "Database view" }) as HTMLButtonElement).disabled).toBe(false));
  await user.click(panel.getByRole("button", { name: "Database view" }));
  const filters = within(panel.getByRole("group", { name: "Filters" }));
  expect(filters.getByText("No filters: every row of the view is in scope.")).toBeTruthy();
  expect((filters.getByRole("button", { name: "Add filter" }) as HTMLButtonElement).disabled).toBe(true);
  // The value typed after its column is captured with it; nothing commits
  // when a field loses focus.
  await user.type(filters.getByLabelText("Filter column"), "status");
  await user.tab();
  await user.type(filters.getByLabelText("Filter value"), "ready");
  await user.click(filters.getByRole("button", { name: "Add filter" }));
  await user.type(filters.getByLabelText("Filter column"), "clinic");
  await user.type(filters.getByLabelText("Filter value"), "north");
  await user.click(filters.getByRole("button", { name: "Add filter" }));
  expect(filters.getAllByRole("button", { name: /^Edit filter / }).map((button) => button.textContent)).toEqual(["Edit filter", "Edit filter"]);
  expect(filters.getByRole("button", { name: "Edit filter status" })).toBeTruthy();
  expect(filters.getByRole("button", { name: "Remove filter status" })).toBeTruthy();
  // Editing a row changes that row only.
  await user.click(filters.getByRole("button", { name: "Edit filter clinic" }));
  expect((filters.getByLabelText("Filter column") as HTMLInputElement).value).toBe("clinic");
  await user.clear(filters.getByLabelText("Filter value"));
  await user.type(filters.getByLabelText("Filter value"), "south");
  await user.click(filters.getByRole("button", { name: "Update filter" }));
  await user.click(panel.getByRole("button", { name: "Save observation" }));
  await waitFor(() => expect(saved).toHaveLength(1));
  expect(saved[0]?.choices?.database?.filters).toEqual([
    { column: "status", value: "ready" },
    { column: "clinic", value: "south" },
  ]);
  expect(saved[0]?.choices?.database?.driver).toBe("postgresql");

  // Reopened, every stored filter is shown, and one can be removed on purpose.
  const stored = savedSource(saved[0]!);
  facade.reply({
    OpenObservationSource: (): Promise<ObservationSourceResult> =>
      Promise.resolve({ state: "completed", source: stored, identity: "db-identity" }),
  });
  await openFile(user, panel, "Source", "observation-source.json");
  await waitFor(() => expect(within(panel.getByRole("group", { name: "Filters" })).getAllByRole("button", { name: /^Remove filter / })).toHaveLength(2));
  const reopened = within(panel.getByRole("group", { name: "Filters" }));
  expect(reopened.getByText("ready")).toBeTruthy();
  expect(reopened.getByText("south")).toBeTruthy();
  await user.click(reopened.getByRole("button", { name: "Remove filter status" }));
  expect(reopened.queryByText("ready")).toBeNull();
  expect(reopened.getByText("south")).toBeTruthy();
});

test("a recorded baseline and both collection limits are editable and saved without dropping the other rules", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user);
  const windows: ObservationWindowRequest[] = [];
  facade.reply({
    SaveObservationSource: (request): Promise<ObservationSourceResult> =>
      Promise.resolve({ state: "completed", source: savedSource(request), identity: "source-identity" }),
    SaveObservationWindow: (request): Promise<ObservationWindowResult> => {
      windows.push(request);
      return Promise.resolve({ state: "completed", window: request.window!, identity: "window-identity-2" });
    },
  });
  const panel = await openObservationSetup(user);
  await user.click(panel.getByRole("tab", { name: "Completion rules" }));
  await waitFor(() => expect((panel.getByLabelText("Pre-existing state") as HTMLSelectElement).disabled).toBe(false));
  // The existing declaration stands until the person chooses another; the
  // baseline field exists only for a recorded baseline.
  expect((panel.getByLabelText("Pre-existing state") as HTMLSelectElement).value).toBe("declared-empty");
  expect(panel.queryByLabelText("Baseline ID")).toBeNull();
  expect(panel.getByRole("option", { name: "Declared empty" })).toBeTruthy();
  expect(panel.getByRole("option", { name: "Unknown" })).toBeTruthy();
  await user.selectOptions(panel.getByLabelText("Pre-existing state"), "Recorded baseline");
  const baseline = "a".repeat(64);
  await user.type(panel.getByLabelText("Baseline ID"), baseline);
  const limits = within(panel.getByRole("group", { name: "Collection limits" }));
  await user.clear(limits.getByLabelText("Maximum records"));
  await user.type(limits.getByLabelText("Maximum records"), "250");
  await user.clear(limits.getByLabelText("Maximum samples"));
  await user.type(limits.getByLabelText("Maximum samples"), "8");
  await user.selectOptions(panel.getByLabelText("Watermark"), "Collection start");
  await user.click(panel.getByRole("button", { name: "Save observation" }));
  await waitFor(() => expect(windows).toHaveLength(1));
  expect(windows[0]?.window).toEqual({
    schema: "readmit-observation-window/v1",
    source: { kind: "file-export", identity: "scheduling-archive", scope: "appointments" },
    watermark: { kind: "collection-start", position: "" },
    pre_existing_state: { declaration: "recorded-baseline", baseline_identity: baseline },
    completion: { deadline: "30s", quiet_period: "2s", stable_samples: 3, max_records: 250, max_samples: 8 },
  });
});

test("the saved window binds into a test only while the editor shows it unchanged", async () => {
  const user = userEvent.setup();
  await openProject(user);
  const panel = await openObservationSetup(user);
  await user.click(panel.getByRole("tab", { name: "Collect and results" }));
  const bind = () => panel.getByRole("button", { name: "Use saved window in test" }) as HTMLButtonElement;
  await waitFor(() => expect(bind().disabled).toBe(false));
  expect(panel.getByText("Binds the saved window observation-window.json into the test draft. No test is run.")).toBeTruthy();
  await user.click(panel.getByRole("tab", { name: "Completion rules" }));
  await user.clear(panel.getByLabelText("Quiet period"));
  await user.type(panel.getByLabelText("Quiet period"), "5s");
  await user.click(panel.getByRole("tab", { name: "Collect and results" }));
  expect(bind().disabled).toBe(true);
  expect(panel.getByText(/^Save the window first/)).toBeTruthy();
});

test("a result stays tied to the saved pair and output that produced it", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user);
  facade.reply({
    CollectObservation: (): Promise<ObservationCompletionResult> =>
      Promise.resolve({
        state: "completed",
        summary: {
          supported: false,
          reason: "the window reached its record cap",
          status: "capped",
          records_observed: 100,
          mapped_correlations: 0,
          unmapped_correlations: 100,
          stale: false,
          partial: true,
          trustworthy: false,
        },
      }),
  });
  const panel = await openObservationSetup(user);
  await user.click(panel.getByRole("tab", { name: "Collect and results" }));
  const authorize = panel.getByLabelText(/I authorize a read-only collection/) as HTMLInputElement;
  await waitFor(() => expect(authorize.disabled).toBe(false));
  await user.click(authorize);
  await user.click(panel.getByRole("button", { name: "Collect once" }));
  expect(
    await panel.findByText(
      "Collection of saved source observation-source.json and saved window observation-window.json, into observation-completion.json.",
    ),
  ).toBeTruthy();
  expect(panel.getByText(byContent(/^Status: /)).textContent).toBe("Status: capped (not trustworthy) · partial/incomplete");
  expect(panel.getByText(byContent(/^Absence claim: /)).textContent).toBe("Absence claim: not supported — the window reached its record cap");
  // Another output is not what produced this result.
  await user.clear(panel.getByLabelText("Completion file"));
  await user.type(panel.getByLabelText("Completion file"), "another.json");
  expect(panel.getByText(/^Historical result, not evidence for the configuration shown now\./)).toBeTruthy();
});

test("the three views are keyboard tabs, and every source type keeps a named group and its qualification beside it", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user);
  const panel = await openObservationSetup(user);
  const list = panel.getByRole("tablist", { name: "Observation views" });
  const tabs = within(list).getAllByRole("tab");
  expect(tabs.map((tab) => tab.textContent)).toEqual(["Source", "Completion rules", "Collect and results"]);
  tabs[0]?.focus();
  await user.keyboard("{ArrowRight}");
  expect(within(list).getByRole("tab", { selected: true }).textContent).toBe("Completion rules");
  expect(document.activeElement).toBe(tabs[1]);
  await user.keyboard("{End}");
  expect(within(list).getByRole("tab", { selected: true }).textContent).toBe("Collect and results");
  await user.keyboard("{Home}");
  expect(within(list).getByRole("tab", { selected: true }).textContent).toBe("Source");
  expect(panel.getByRole("tabpanel").getAttribute("aria-labelledby")).toBe(tabs[0]?.id);
  expectTabPattern(list);
  // The files and their saved state stay in view on every tab.
  await user.keyboard("{ArrowLeft}");
  expect(panel.getByLabelText("Source file")).toBeTruthy();
  expect(panel.getByRole("group", { name: "Saved configuration" })).toBeTruthy();
  await user.keyboard("{Home}");
  // Choosing a tab reads, saves, validates and collects nothing.
  expect(facade.callsTo("SaveObservationSource")).toHaveLength(0);
  expect(facade.callsTo("ValidateObservationPair")).toHaveLength(0);
  expect(facade.callsTo("CollectObservation")).toHaveLength(0);

  await waitFor(() => expect((panel.getByRole("button", { name: "HTTPS API" }) as HTMLButtonElement).disabled).toBe(false));
  const groups: Record<string, string> = {
    "File export": "Export file",
    "HTTPS API": "HTTPS URL",
    "Downstream capture": "Captured case",
    "Database view": "Database address",
  };
  for (const [kind, field] of Object.entries(groups)) {
    const choice = panel.getByRole("button", { name: kind });
    await user.click(choice);
    expect(choice.getAttribute("aria-pressed")).toBe("true");
    const group = within(panel.getByRole("group", { name: kind }));
    expect(group.getByLabelText(field)).toBeTruthy();
    // The qualification is stated beside the choice, including one the
    // facade reports no support for.
    const described = document.getElementById(choice.getAttribute("aria-describedby") ?? "");
    expect(described?.textContent).toBe(
      kind === "File export" ? "supported" : kind === "Database view" ? "unqualified; not a production claim" : "support not reported",
    );
  }
  // Help stays attached to its field for assistive technology.
  const address = panel.getByLabelText("Database address");
  expect(document.getElementById(address.getAttribute("aria-describedby") ?? "")?.textContent).toMatch(/not an HTTPS URL/);
  expect(panel.getByRole("option", { name: "PostgreSQL" })).toBeTruthy();
  expect(panel.getByRole("option", { name: "SQL Server" })).toBeTruthy();
  expect(panel.getByRole("option", { name: "Oracle" })).toBeTruthy();
  await user.click(panel.getByRole("tab", { name: "Collect and results" }));
  expect(panel.getByText(/^Selected adapter:/)).toBeTruthy();
});

test("a new window's pre-existing state is the person's explicit choice: nothing is chosen for them and Save says why it waits", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user, true);
  const windows: ObservationWindowRequest[] = [];
  facade.reply({
    SaveObservationSource: (request): Promise<ObservationSourceResult> =>
      Promise.resolve({ state: "completed", source: savedSource(request), identity: "saved-source-identity" }),
    SaveObservationWindow: (request): Promise<ObservationWindowResult> => {
      windows.push(request);
      return Promise.resolve({ state: "completed", window: request.window!, identity: "saved-window-identity" });
    },
  });
  const panel = await openObservationSetup(user);
  await user.click(panel.getByRole("tab", { name: "Completion rules" }));
  await waitFor(() => expect((panel.getByLabelText("Pre-existing state") as HTMLSelectElement).disabled).toBe(false));
  // The facade's new window carries a declaration, but the panel offers it
  // as no choice at all until the person makes one.
  const state = panel.getByLabelText("Pre-existing state") as HTMLSelectElement;
  expect(state.value).toBe("");
  expect(state.selectedOptions[0]?.textContent).toBe("Not chosen");
  expect((panel.getByRole("button", { name: "Save observation" }) as HTMLButtonElement).disabled).toBe(true);
  expect(panel.getByText("Choose the pre-existing state under Completion rules before saving: readmit does not choose it for you.")).toBeTruthy();
  await user.click(panel.getByRole("button", { name: "Save observation" }));
  expect(facade.callsTo("SaveObservationWindow")).toHaveLength(0);

  await user.selectOptions(state, "Declared empty");
  expect(panel.queryByText(/readmit does not choose it for you/)).toBeNull();
  await user.click(panel.getByRole("button", { name: "Save observation" }));
  await waitFor(() => expect(windows).toHaveLength(1));
  expect(windows[0]?.window?.pre_existing_state).toEqual({ declaration: "declared-empty", baseline_identity: "" });
});

test("an existing window opens with its saved pre-existing state", async () => {
  const user = userEvent.setup();
  await openProject(user);
  const panel = await openObservationSetup(user);
  await user.click(panel.getByRole("tab", { name: "Completion rules" }));
  await waitFor(() => expect((panel.getByLabelText("Pre-existing state") as HTMLSelectElement).disabled).toBe(false));
  expect((panel.getByLabelText("Pre-existing state") as HTMLSelectElement).value).toBe("declared-empty");
  expect((panel.getByRole("button", { name: "Save observation" }) as HTMLButtonElement).disabled).toBe(false);
});

test("a validation answered after another file was opened commits nothing into the form", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user);
  const panel = await openObservationSetup(user);
  await waitFor(() => expect(savedIDs(panel)[0]).toBe("Saved source ID: source-identity (observation-source.json)"));
  const sources = facade.park("ValidateObservationSource");
  const windows = facade.park("ValidateObservationWindow");
  const pairs = facade.park("ValidateObservationPair");
  await user.click(panel.getByRole("button", { name: "Validate saved source" }));
  await user.click(panel.getByRole("button", { name: "Validate saved window" }));
  await user.click(panel.getByRole("button", { name: "Validate saved configuration" }));
  await waitFor(() => expect(sources.size + windows.size + pairs.size).toBe(3));

  // Both files change before the validations answer.
  await openFile(user, panel, "Source", "other-source.json");
  await waitFor(() => expect(savedIDs(panel)[0]).toBe("Saved source ID: source-identity (other-source.json)"));
  await openFile(user, panel, "Window", "other-window.json");
  await waitFor(() => expect(savedIDs(panel)[1]).toBe("Saved window ID: window-identity (other-window.json)"));
  sources.resolve({ state: "failed", reason: "late source answer" });
  windows.resolve({ state: "failed", reason: "late window answer" });
  pairs.resolve({ state: "failed", reason: "late pair answer" });
  // A later validation of the current files is shown, so the late answers
  // had their chance to land before it.
  facade.reply({
    ValidateObservationSource: (): Promise<ObservationSourceResult> => Promise.resolve({ state: "failed", reason: "current source answer" }),
  });
  await user.click(panel.getByRole("button", { name: "Validate saved source" }));
  expect(await panel.findByText("Source document other-source.json refused: current source answer")).toBeTruthy();
  expect(panel.queryByText(/late (source|window|pair) answer/)).toBeNull();
  expect(screen.queryByText(/late (source|window|pair) answer/)).toBeNull();
});
