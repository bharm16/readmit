// Private result views are exposed only by vite.catalog.config.ts. Their
// production exports and component implementations remain unchanged.
import { createElement } from "react";
import components from "virtual:catalog-private";
import type { Example } from "./examples";
import * as f from "../testkit/fixtures";
import { privateData } from "./private-data";
const noop = () => {};
function p(
  file: string,
  name: string,
  props: Record<string, unknown>,
): Example {
  const id = `desktop/frontend/src/${file}.tsx#${name}`;
  return {
    name: `${file} · ${name}`,
    state: "populated",
    render: () => {
      const Component = components[id];
      if (!Component) throw new Error(`Missing private component ${id}`);
      return createElement(Component, props);
    },
  };
}
export const privateExamples: Example[] = [
  p("App", "SuiteHandoffNotice", privateData.suiteHandoff),
  p("CapturePanel", "PolicyReview", privateData.policyReview),
  p("CapturePanel", "SourceReview", privateData.sourceReview),
  p("Comparison", "NormalizationView", {
    normalization: f.normalizeResult([], []).normalization,
    busy: false,
    onNormalize: noop,
  }),
  p("CorrelationReview", "Occurrence", {
    id: f.GRID_OCCURRENCE,
    busy: false,
    onSelect: noop,
  }),
  p("EnvironmentPanel", "WrittenIdentity", {
    written: {
      kind: "Target",
      file: "synthetic-target.json",
      identity: "synthetic-digest",
    },
  }),
  p("MaintenancePanel", "InventoryList", privateData.inventoryList),
  p("MaintenancePanel", "BackupReport", privateData.backupReport),
  p("PacketPanel", "PacketInputRow", {
    label: "Source case",
    view: f.packetPreviewResult().preview?.case,
  }),
  p("PacketPanel", "PacketViewDetails", { view: f.packetResult().packet }),
  p("PacketPanel", "PacketReviewDetails", {
    view: f.packetReviewResult(false).review,
  }),
  p("PerformanceCorpus", "ProgressLine", privateData.progressLine),
  p("PerformanceCorpus", "ScanReport", privateData.scanReport),
  p("ProfileEditor", "PinStatus", {
    resolution: f.localProfileFixture().resolution,
  }),
  p("ProfileEditor", "Fact", {
    term: "Identity",
    children: "synthetic-profile-identity",
  }),
  p("ProfileEditor", "WholeNumber", { value: 3, min: 0, onCommit: noop }),
  p("ProfileEditor", "Repetitions", {
    group: "Synthetic group",
    value: { min: 1, max: 3 },
    disabled: false,
    onChange: noop,
  }),
  p("ProjectPanel", "EditableDocument", {
    result: f.revisionsResult(),
    indicators: f.indicatorTable(),
  }),
  p("ProjectPanel", "CaseDetailFields", {
    prefix: "catalog",
    details: {
      title: "Synthetic case",
      owner: "integration-team",
      status: "open",
      interface_version: "siu-2.5.1-v1",
      tags: "scheduling",
      incidents: "synthetic-incident",
    },
    versions: ["siu-2.5.1-v1"],
    defaultOwner: "integration-team",
    defaultVersion: "siu-2.5.1-v1",
    statusRequired: true,
    onChange: noop,
  }),
  p("ProtectionPanel", "ControlChange", {
    action: "retire",
    name: "Synthetic key",
    result: f.protectionResult(),
  }),
  p("ProtectionPanel", "PackageView", {
    view: f.protectionPackageResult().package,
    limitations: f.protectionPackageResult().limitations ?? [],
  }),
  p("RawInspection", "RowLine", privateData.rawRow),
  p("ReplayPanel", "Decision", { decision: privateData.decision }),
  p("ReplayPanel", "NotPreviewed", {
    result: {
      state: "failed",
      reason: "Synthetic policy refusal",
      decision: privateData.decision,
    },
  }),
  p("ReplayPanel", "Sent", {
    result: { state: "completed", run: privateData.replayRun },
  }),
  p("ReplayPanel", "RunView", { run: privateData.replayRun }),
  p("Review", "Preview", privateData.transformPreview),
  p("Review", "Inventory", {
    ...privateData.reviewInventory,
    busy: false,
    onWindow: noop,
  }),
  p("RunComparison", "Execution", {
    title: "Baseline",
    execution: f.runComparisonResult().comparison?.baseline,
  }),
  p("RunExplanation", "Refusal", {
    result: { state: "failed", reason: "Synthetic run unavailable" },
  }),
  p("RunExplanation", "ExplanationView", privateData.explanation),
  p("RunPanel", "RunEvidenceView", {
    evidence: f.runEvidenceResult().evidence,
    onOpenCase: noop,
  }),
  p("RunnerPanel", "Refusal", {
    result: { state: "failed", reason: "Synthetic runner unavailable" },
  }),
  p("RunnerPanel", "ResultLine", {
    result: { state: "completed" },
    done: "Synthetic run completed",
  }),
  p("RunnerPanel", "ScheduleRow", {
    ...privateData.scheduleRow,
    index: 0,
    onChange: noop,
    onRemove: noop,
  }),
  p("RunnerPanel", "SchedulePreviewView", privateData.schedulePreview),
  p("RunnerPanel", "GateVerification", privateData.gateVerification),
  p("RunnerPanel", "RunnerCapacity", {}),
  p("Sequence", "Relations", {
    event: {
      ...f.sequenceEvent(f.GRID_OCCURRENCE, 1),
      references: [
        { kind: "ack", related: [f.NEXT_OCCURRENCE], occurrences: 2 },
      ],
      gaps: ["unacknowledged_message"],
    },
  }),
  p("SuitePanel", "ReleaseRead", {
    result: {
      state: "completed",
      release_id: "release-1",
      comparison: { revision: "1" },
      previous_approver: "Synthetic reviewer",
    },
  }),
  p("SuitePanel", "SuiteEditor", {
    document: f.suiteDocument(),
    expectedErrors: {},
    specEntries: [f.SUITE_TEMPLATE],
    caseEntries: [f.CASE_ENTRY],
    targetEntries: [f.SUITE_TARGET],
    onChange: noop,
    onExpected: noop,
  }),
  p("SuitePanel", "ExpansionView", { result: f.suitePreviewResult() }),
  p("SuitePanel", "CoverageView", { result: f.suiteCoverageResult() }),
  p("SyntheticPackets", "SyntheticPacketDetails", privateData.syntheticPacket),
  p("SyntheticPackets", "SyntheticRerunDetails", privateData.syntheticRerun),
  p("TeamCollaboration", "LifecycleWrite", privateData.lifecycleWrite),
];
