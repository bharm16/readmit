import type { Artifact } from "../bindings";
import * as f from "../testkit/fixtures";
import {
  installFacade,
  installHubAdmin,
  type FacadeHandlers,
} from "../testkit/wails";

export const catalogRows = Array.from({ length: 8 }, (_, i) =>
  f.messageRow(`occ-${String(i + 1).padStart(6, "0")}`, i % 2 === 1 ? "ack" : "message", {
    observed_at: `2026-01-01T12:00:${String(i * 10).padStart(2, "0")}Z`,
    trigger_event: i % 4 === 0 ? "S12" : "S13",
    source_id: i % 2 === 1 ? "s0002" : "s0001",
    direction: i % 2 === 1 ? "inbound" : "outbound",
  }),
);
export const artifacts: Artifact[] = [
  { name: "project.json", kind: "project" },
  ...f.folderWithCase().workspace.artifacts,
  { name: "reschedule-test.json", kind: "spec" },
  { name: "baseline-run", kind: "job" },
  { name: "fixed-run", kind: "result" },
  ...f.suiteArtifacts(),
];

export const unhandledMethods = new Set<string>();

export function installCatalogFixtures() {
  const handlers: FacadeHandlers = {
    Shell: f.shellResult,
    Filters: f.filtersResult,
    EditorDrafts: () => ({ state: "empty", drafts: [] }),
    OperationStatus: () => ({
      state: "empty",
      selected: false,
      author_seats: 0,
      runner_instances: 0,
    }),
    CommercialStatus: () => ({ state: "empty" }),
    LicenseStatus: () => ({ state: "empty" }),
    HubStatus: () => f.defaultHubResult(),
    DisclosureStatus: f.disclosureStatusResult,
    RecordView: () => f.sessionStored,
    SaveEditorDraft: () => ({ state: "completed" }),
    DiscardEditorDraft: () => ({ state: "completed" }),
    Cancel: async () => {},
    Guide: () => f.guideResult("sample", 0),
    SelectWorkspace: () => f.folderChosen(f.WORKSPACE_ROOT, artifacts),
    OpenWorkspace: () => f.folderChosen(f.WORKSPACE_ROOT, artifacts),
    OpenProjectOverview: () => f.projectOverviewResult([f.registeredCase()]),
    OpenCase: () => f.caseResult(),
    ReadMessages: () => f.messagesResult(catalogRows),
    ListViews: () => ({ state: "empty", views: [] }),
    InspectOccurrence: () => f.inspectionResult(),
    ReadTarget: () => f.defaultTargetResult(),
    ReadSecrets: () => f.defaultSecretsResult(),
    ReadSendPolicy: () => f.defaultSendPolicyResult(),
    ReadResetPlan: () => f.defaultResetPlanResult(),
    OpenSequence: () =>
      f.sequenceResult([
        f.sequenceEvent(f.GRID_OCCURRENCE, 1),
        f.sequenceEvent(f.NEXT_OCCURRENCE, 2),
      ]),
    ObservationSupport: () => ({ state: "empty" }),
    OpenObservationSource: () => ({ state: "empty" }),
    OpenObservationWindow: () => ({ state: "empty" }),
    InspectProjectQuota: () => ({ state: "empty" }),
    ListProjectRecoveryCopies: () => ({ state: "empty" }),
  };
  const stub = installFacade(handlers);
  const facade = window.go!.desktop!.App!;
  window.go!.desktop!.App = new Proxy(facade, {
    get(target, key) {
      if (typeof key === "string" && !(key in handlers))
        unhandledMethods.add(key);
      return Reflect.get(target, key);
    },
  });
  installHubAdmin({});
  return stub;
}
