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
import type {
  ObservationCollectFacadeRequest,
  ObservationCompletionResult,
  ObservationSource,
  ObservationSourceRequest,
  ObservationSourceResult,
  ObservationSupportResult,
  ObservationValidateResult,
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
  await user.click(screen.getAllByRole("button", { name: "Open workspace…" })[0]!);
  await screen.findByText(WORKSPACE_ROOT);
  const readBtn = await screen.findByRole("button", { name: "Open project" });
  await waitFor(() => expect((readBtn as HTMLButtonElement).disabled).toBe(false));
  await user.click(readBtn);
  await screen.findByRole("heading", { name: "Scheduling investigation" });
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

  const evidence = screen.getByRole("region", { name: "Evidence" });
  await user.click(within(evidence).getByRole("button", { name: "Observations" }));
  expect(await screen.findByRole("heading", { name: "Observations" })).toBeTruthy();
  expect(screen.getByText(/Editor opened locally/)).toBeTruthy();
  expect(screen.getByText(/postgresql/)).toBeTruthy();
  expect(screen.getByText(/not a production claim/)).toBeTruthy();
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

  const evidence = screen.getByRole("region", { name: "Evidence" });
  await user.click(within(evidence).getByRole("button", { name: "Observations" }));
  await screen.findByRole("heading", { name: "Observations" });

  await user.click(screen.getByRole("button", { name: "Validate locally" }));
  expect(await screen.findByText(/Local configuration validation passed/)).toBeTruthy();

  await user.click(screen.getByRole("button", { name: "Collect once" }));
  // Collect stays disabled until authorize is checked; force a click path via authorize.
  expect(calls).toHaveLength(0);
  await user.click(screen.getByLabelText(/I authorize a read-only collection/));
  await user.click(screen.getByRole("button", { name: "Collect once" }));
  await waitFor(() => expect(calls).toHaveLength(1));
  expect(calls[0]?.authorize).toBe(true);
  expect(await screen.findByText(/Absence claim:/)).toBeTruthy();
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
  const evidence = screen.getByRole("region", { name: "Evidence" });
  await user.click(within(evidence).getByRole("button", { name: "Observations" }));
  await screen.findByRole("heading", { name: "Observations" });
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
  const evidence = screen.getByRole("region", { name: "Evidence" });
  await user.click(within(evidence).getByRole("button", { name: "Observations" }));
  await screen.findByRole("heading", { name: "Observations" });
  await user.click(screen.getByRole("button", { name: "Save observation" }));
  expect(await screen.findByText(/^Saved through shared Go writers/)).toBeTruthy();
  await user.clear(screen.getByLabelText("Export path"));
  await user.type(screen.getByLabelText("Export path"), "never-exported.csv");
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
  await user.click(panel.getByRole("button", { name: "http-api" }));
  await user.click(panel.getByRole("button", { name: "file-export" }));
  await user.click(panel.getByRole("button", { name: "Save observation" }));
  await waitFor(() => expect(saved).toHaveLength(1));
  expect(saved[0]?.choices?.source.kind).toBe("file-export");
  expect(saved[0]?.choices?.file).not.toBeNull();
  expect(saved[0]?.choices?.http).toBeNull();
  expect(saved[0]?.choices?.capture).toBeNull();
  expect(saved[0]?.choices && "database" in saved[0].choices).toBe(false);
  expect(saved[0]?.choices && "schema" in saved[0].choices).toBe(false);
  expect(await panel.findByText("Source document observation-source.json: saved readmit-observation-source/v1, identity source-identity.")).toBeTruthy();

  await user.click(panel.getByRole("button", { name: "downstream-capture" }));
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
  const evidence = screen.getByRole("region", { name: "Evidence" });
  await user.click(within(evidence).getByRole("button", { name: "Observations" }));
  return within(await screen.findByRole("region", { name: "Observation setup" }));
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
  await waitFor(() => expect((panel.getByRole("button", { name: "Save observation" }) as HTMLButtonElement).disabled).toBe(false));
  expect(panel.getByText(byContent(/^Pinned identities/)).textContent).toBe(
    "Pinned identities — source: not saved; window: not saved",
  );
  await user.click(panel.getByRole("button", { name: "Save observation" }));
  await panel.findByText(/^Saved through shared Go writers/);
  expect(panel.getByText(byContent(/^Pinned identities/)).textContent).toBe(
    "Pinned identities — source: saved-source-identity; window: saved-window-identity",
  );
});

test("changing a source name asks before replacing unsaved edits, and Escape keeps them", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user);
  const panel = await openObservationSetup(user);
  await user.clear(panel.getByLabelText("Export path"));
  await user.type(panel.getByLabelText("Export path"), "edited.csv");
  const reads = facade.callsTo("OpenObservationSource").length;
  await user.clear(panel.getByLabelText("Source document"));
  await user.type(panel.getByLabelText("Source document"), "other-source.json");
  expect(panel.getByRole("group", { name: /Replace source document/ })).toBeTruthy();
  expect(facade.callsTo("OpenObservationSource")).toHaveLength(reads);
  expect((panel.getByLabelText("Export path") as HTMLInputElement).value).toBe("edited.csv");
  await user.keyboard("{Escape}");
  expect(panel.queryByRole("group", { name: /Replace source document/ })).toBeNull();
  expect((panel.getByLabelText("Source document") as HTMLInputElement).value).toBe("observation-source.json");
  expect((panel.getByLabelText("Export path") as HTMLInputElement).value).toBe("edited.csv");
  expect(facade.callsTo("OpenObservationSource")).toHaveLength(reads);

  await user.clear(panel.getByLabelText("Source document"));
  await user.type(panel.getByLabelText("Source document"), "other-source.json");
  await user.click(panel.getByRole("button", { name: "Replace source document" }));
  await waitFor(() => expect(facade.callsTo("OpenObservationSource").length).toBeGreaterThan(reads));
  await waitFor(() => expect((panel.getByLabelText("Export path") as HTMLInputElement).value).toBe("export.csv"));
  expect((panel.getByLabelText("Source document") as HTMLInputElement).value).toBe("other-source.json");

  await user.clear(panel.getByLabelText("Export path"));
  await user.type(panel.getByLabelText("Export path"), "still-edited.csv");
  facade.reply({
    OpenObservationSource: (): Promise<ObservationSourceResult> =>
      Promise.resolve({ state: "failed", reason: "later source version" }),
  });
  await user.clear(panel.getByLabelText("Source document"));
  await user.type(panel.getByLabelText("Source document"), "later-source.json");
  await user.click(panel.getByRole("button", { name: "Replace source document" }));
  await panel.findByText("Source document later-source.json not opened: later source version");
  expect((panel.getByLabelText("Export path") as HTMLInputElement).value).toBe("still-edited.csv");
  await user.clear(panel.getByLabelText("Source document"));
  await user.type(panel.getByLabelText("Source document"), "third-source.json");
  expect(panel.getByRole("group", { name: "Replace source document?" })).toBeTruthy();
  expect((panel.getByLabelText("Export path") as HTMLInputElement).value).toBe("still-edited.csv");
});

test("changing a window name asks before replacing unsaved edits, and Keep retains them", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user);
  const panel = await openObservationSetup(user);
  await user.clear(panel.getByLabelText("Deadline"));
  await user.type(panel.getByLabelText("Deadline"), "45s");
  const reads = facade.callsTo("OpenObservationWindow").length;
  await user.clear(panel.getByLabelText("Window document"));
  await user.type(panel.getByLabelText("Window document"), "other-window.json");
  expect(panel.getByRole("group", { name: /Replace window document/ })).toBeTruthy();
  expect(facade.callsTo("OpenObservationWindow")).toHaveLength(reads);
  await user.click(panel.getByRole("button", { name: "Keep window edits" }));
  expect((panel.getByLabelText("Window document") as HTMLInputElement).value).toBe("observation-window.json");
  expect((panel.getByLabelText("Deadline") as HTMLInputElement).value).toBe("45s");
  expect(facade.callsTo("OpenObservationWindow")).toHaveLength(reads);

  facade.reply({
    OpenObservationWindow: (): Promise<ObservationWindowResult> =>
      Promise.resolve({ state: "failed", reason: "later window version" }),
  });
  await user.clear(panel.getByLabelText("Window document"));
  await user.type(panel.getByLabelText("Window document"), "later-window.json");
  await user.click(panel.getByRole("button", { name: "Replace window document" }));
  await panel.findByText("Window document later-window.json not opened: later window version");
  expect((panel.getByLabelText("Deadline") as HTMLInputElement).value).toBe("45s");
  await user.clear(panel.getByLabelText("Window document"));
  await user.type(panel.getByLabelText("Window document"), "third-window.json");
  expect(panel.getByRole("group", { name: "Replace window document?" })).toBeTruthy();
});

test("a newly named document does not show the previous file's identity while its read is pending", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user);
  const panel = await openObservationSetup(user);
  await waitFor(() => expect(panel.getByText(byContent(/^Pinned identities/)).textContent).toBe(
    "Pinned identities — source: source-identity; window: window-identity",
  ));
  const reading = facade.park("OpenObservationSource");
  await user.clear(panel.getByLabelText("Source document"));
  await user.type(panel.getByLabelText("Source document"), "new-source.json");
  await waitFor(() => expect(reading.size).toBeGreaterThan(0));
  expect(panel.getByText(byContent(/^Pinned identities/)).textContent).toBe(
    "Pinned identities — source: not saved; window: window-identity",
  );
  await waitFor(() => {
    while (reading.size > 0) reading.resolve({ state: "failed", reason: "not found" });
    expect(panel.getByText("Source document new-source.json not opened: not found")).toBeTruthy();
  });
  const windowReading = facade.park("OpenObservationWindow");
  await user.clear(panel.getByLabelText("Window document"));
  await user.type(panel.getByLabelText("Window document"), "new-window.json");
  await waitFor(() => expect(windowReading.size).toBeGreaterThan(0));
  expect(panel.getByText(byContent(/^Pinned identities/)).textContent).toBe(
    "Pinned identities — source: not saved; window: not saved",
  );
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

  await tabTo(user, panel.getByRole("button", { name: "Validate source" }));
  await user.keyboard("{Enter}");
  expect(
    await panel.findByText(
      "Source document observation-source.json: valid readmit-observation-source/v1, identity saved-source-identity. Nothing was collected.",
    ),
  ).toBeTruthy();
  await tabTo(user, panel.getByRole("button", { name: "Validate window" }));
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
  await user.click(panel.getByRole("button", { name: "Save observation" }));
  expect(
    await panel.findByText("Not saved: cannot write an observation source here. A collection reads the documents saved before."),
  ).toBeTruthy();
  expect(panel.getByText("Source document observation-source.json not saved: cannot write an observation source here")).toBeTruthy();
  expect(facade.callsTo("SaveObservationWindow")).toHaveLength(0);
  expect(panel.getByText(byContent(/^Pinned identities/)).textContent).toBe(
    "Pinned identities — source: source-identity; window: window-identity",
  );

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
  expect(panel.getByText(byContent(/^Pinned identities/)).textContent).toBe(
    "Pinned identities — source: source-identity-2; window: window-identity",
  );
});

test("a document the reader refuses is said to be refused on opening, and naming one document never reads the other again", async () => {
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
  await user.clear(panel.getByLabelText("Export path"));
  await user.type(panel.getByLabelText("Export path"), "exports/appointments.csv");
  await user.clear(panel.getByLabelText("Window document"));
  await user.type(panel.getByLabelText("Window document"), "later.json");
  expect(
    await panel.findByText("Window document later.json not opened: an observation window must declare readmit-observation-window/v1"),
  ).toBeTruthy();
  expect(panel.getByText(byContent(/^Pinned identities/)).textContent).toBe("Pinned identities — source: source-identity; window: not saved");
  // The source was not read again, so what was typed into it stands.
  expect(facade.callsTo("OpenObservationSource")).toHaveLength(sourceReads);
  expect((panel.getByLabelText("Export path") as HTMLInputElement).value).toBe("exports/appointments.csv");
  // The refused window is never replaced by what the editor holds.
  expect((panel.getByRole("button", { name: "Save observation" }) as HTMLButtonElement).disabled).toBe(true);
  expect(panel.getByText(/^Saving is closed while a named document is refused/)).toBeTruthy();
  await user.clear(panel.getByLabelText("Window document"));
  await user.type(panel.getByLabelText("Window document"), "observation-window.json");
  await waitFor(() => expect((panel.getByRole("button", { name: "Save observation" }) as HTMLButtonElement).disabled).toBe(false));
  expect(panel.queryByText(/^Saving is closed/)).toBeNull();
  expect(facade.callsTo("SaveObservationSource")).toHaveLength(0);
  expect(facade.callsTo("SaveObservationWindow")).toHaveLength(0);
});

test("an edit abandoned by closing the panel from the keyboard writes nothing, and the panel opened again reads the saved document", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user);
  let panel = await openObservationSetup(user);
  await waitFor(() => expect((panel.getByLabelText("Export path") as HTMLInputElement).disabled).toBe(false));
  await user.clear(panel.getByLabelText("Export path"));
  await user.type(panel.getByLabelText("Export path"), "exports/elsewhere.csv");
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
  await waitFor(() => expect((panel.getByLabelText("Export path") as HTMLInputElement).value).toBe("export.csv"));
  expect(panel.getByText(byContent(/^Pinned identities/)).textContent).toBe("Pinned identities — source: source-identity; window: window-identity");
});

test("the editor stays closed while a document is read, so a late read never replaces what was typed", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user);
  const reading = facade.park("OpenObservationSource");
  const panel = await openObservationSetup(user);
  await waitFor(() => expect(reading.size).toBeGreaterThan(0));
  const exportPath = panel.getByLabelText("Export path") as HTMLInputElement;
  // Nothing can be typed, saved or validated while the document it would
  // replace is being read; the document's name can still be typed.
  expect(exportPath.disabled).toBe(true);
  expect((panel.getByRole("button", { name: "Save observation" }) as HTMLButtonElement).disabled).toBe(true);
  expect((panel.getByRole("button", { name: "Validate source" }) as HTMLButtonElement).disabled).toBe(true);
  expect((panel.getByLabelText("Source document") as HTMLInputElement).disabled).toBe(false);
  const answer: ObservationSourceResult = {
    state: "completed",
    source: {
      schema: "readmit-observation-source/v1",
      source: { kind: "file-export", identity: "scheduling-archive", scope: "appointments" },
      enabled: true,
      freshness: { max_age: "1h" },
      extraction: { envelope: "csv", encoding: "utf-8", record_key: ["appointment"] },
      file: { path: "exports/appointments.csv", max_bytes: 65536 },
      http: null,
      capture: null,
    },
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
