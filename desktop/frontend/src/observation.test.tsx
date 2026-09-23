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
  ObservationSourceRequest,
  ObservationSourceResult,
  ObservationSupportResult,
  ObservationValidateResult,
  ObservationWindowResult,
} from "./bindings";

async function openProject(user: ReturnType<typeof userEvent.setup>) {
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
        },
        identity: "source-identity",
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
        identity: "window-identity",
      }),
  });
  await user.click(screen.getByRole("button", { name: "Open a workspace folder…" }));
  await screen.findByText(WORKSPACE_ROOT);
  const readBtn = await screen.findByRole("button", { name: "Read the project" });
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
  await user.click(within(evidence).getByRole("button", { name: "Set up observation…" }));
  expect(await screen.findByRole("heading", { name: "Observation sources and windows" })).toBeTruthy();
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
  await user.click(within(evidence).getByRole("button", { name: "Set up observation…" }));
  await screen.findByRole("heading", { name: "Observation sources and windows" });

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
  await user.click(within(evidence).getByRole("button", { name: "Set up observation…" }));
  await screen.findByRole("heading", { name: "Observation sources and windows" });
  await user.click(screen.getByRole("button", { name: "Explain completion" }));
  expect(await screen.findByText(/Status:/)).toBeTruthy();
  expect(screen.getByText("stale")).toBeTruthy();
  expect(screen.getByText(/not supported/)).toBeTruthy();
  expect(screen.getByText(/never evidence of no output/)).toBeTruthy();
});

test("a v1 source the facade answered with every transport member is saved again as v1 declares it, and a refused save says so", async () => {
  const user = userEvent.setup();
  const { facade } = await openProject(user);
  const saved: ObservationSourceRequest[] = [];
  facade.reply({
    SaveObservationSource: (request): Promise<ObservationSourceResult> => {
      saved.push(request);
      // The facade answers with the source struct, whose capture transport
      // is an explicit null even for a v1 document.
      return Promise.resolve({ state: "completed", source: { ...request.source!, capture: null }, identity: "source-identity" });
    },
    SaveObservationWindow: (request): Promise<ObservationWindowResult> =>
      Promise.resolve({ state: "completed", window: request.window!, identity: "window-identity" }),
  });
  const evidence = screen.getByRole("region", { name: "Evidence" });
  await user.click(within(evidence).getByRole("button", { name: "Set up observation…" }));
  await screen.findByRole("heading", { name: "Observation sources and windows" });
  await user.click(screen.getByRole("button", { name: "Save source and window" }));
  expect(await screen.findByText(/^Saved through shared Go writers/)).toBeTruthy();
  await user.clear(screen.getByLabelText("Export path"));
  await user.type(screen.getByLabelText("Export path"), "never-exported.csv");
  await user.click(screen.getByRole("button", { name: "Save source and window" }));
  await waitFor(() => expect(saved).toHaveLength(2));
  // Sent back with capture, a v1 source is a document v1 never allowed.
  expect(saved[1]?.source?.file?.path).toBe("never-exported.csv");
  expect(saved[1]?.source && "capture" in saved[1].source).toBe(false);

  facade.reply({
    SaveObservationSource: (): Promise<ObservationSourceResult> =>
      Promise.resolve({ state: "failed", reason: "the source document could not be written" }),
  });
  await user.click(screen.getByRole("button", { name: "Save source and window" }));
  expect(
    await screen.findByText(/^Not saved: the source document could not be written\. A collection reads the documents saved before\./),
  ).toBeTruthy();
  expect(screen.queryByText(/^Saved through shared Go writers/)).toBeNull();
});
