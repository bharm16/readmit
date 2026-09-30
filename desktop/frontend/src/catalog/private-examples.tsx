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
  p("SyntheticPackets", "SyntheticPacketDetails", privateData.syntheticPacket),
  p("SyntheticPackets", "SyntheticRerunDetails", privateData.syntheticRerun),
];
