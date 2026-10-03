import {StrictMode} from "react";
import { expect, test } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { CIResultsSheet, GateResultsSheet, SetUpCISheet } from "./CISheets";
import { installFacade } from "./testkit/wails";
import type { RequestContext, RunnerRow } from "./bindings";

const CONTEXT: RequestContext = { project: "/projects/scheduling", generation: 1 };
const RUNNER = { ref: { kind: "runner", id: "r1" }, name: "QA runner", config: "/c.json", environment: "Scheduling QA", hub_environment: "lab", hub: "hub", project: "alpha", root: "/var/lib/readmit-runner", status: "available", active_jobs: 0, local: false } as RunnerRow;

// The configuration is generated for the exact version and environment, with
// the agent paths it runs on and the reviewed gate's pinned identities taken
// from the policy itself; it is written only where the person names.
test("the CI handoff is generated in-app and results are inspected in-app", async () => {
  const user = userEvent.setup();
  let wrote = "";
  const facade = installFacade({
    ListRunners: () => ({ state: "completed", context: CONTEXT, runners: [RUNNER] }),
    SaveCIHandoff: () => ({ state: "completed", output: "/Users/qa/readmit-suite.yml", document: "#" }),
  });
  render(<SetUpCISheet suite="Scheduling smoke" version="Version 4" environments={[{ id: "qa", name: "Scheduling QA" }]} context={() => CONTEXT} onClose={() => {}} onDone={(output) => { wrote = output; }} />);
  const sheet = await screen.findByRole("dialog", { name: "Set up CI" });
  expect(within(sheet).getByText("Version 4")).toBeTruthy();
  await user.selectOptions(within(sheet).getByLabelText("Integration"), "github");
  await waitFor(() => expect(within(sheet).getAllByRole("option", { name: "QA runner" })).toHaveLength(1));
  await user.selectOptions(within(sheet).getByLabelText("Runner"), "r1");
  expect((within(sheet).getByLabelText("Run folder") as HTMLInputElement).value).toBe("/var/lib/readmit-runner/ci-runs/qa");
  await user.type(within(sheet).getByLabelText("Readmit program"), "/opt/readmit/bin/readmit");
  await user.type(within(sheet).getByLabelText("Operation policy"), "/etc/readmit/operation.json");
  await user.type(within(sheet).getByLabelText("Suite file"), "/srv/ci/suite.json");
  await user.type(within(sheet).getByLabelText("Coverage declaration"), "/srv/ci/coverage.json");
  await user.click(within(sheet).getByRole("button", { name: "Next" }));
  expect(within(sheet).getByText("Writes a configuration file; nothing is pushed, installed or enabled.")).toBeTruthy();
  await user.click(within(sheet).getByRole("button", { name: "Generate configuration" }));
  await waitFor(() => expect(wrote).toBe("/Users/qa/readmit-suite.yml"));
  expect(facade.oneCall("SaveCIHandoff")[0]).toEqual({
    integration: "github", binary: "/opt/readmit/bin/readmit", operation_policy: "/etc/readmit/operation.json", suite_file: "/srv/ci/suite.json", environment: "qa",
    run_directory: "/var/lib/readmit-runner/ci-runs/qa", coverage_file: "/srv/ci/coverage.json", output: "", suite: "Scheduling smoke Version 4",
  });
});

test("the reviewed change-gate step is added to the handoff only when asked for, with every reviewed pin, and a refusal is shown", async () => {
  const user = userEvent.setup();
  const identity = "a".repeat(64);
  const promotion = "b".repeat(64);
  const facade = installFacade({
    ListRunners: () => ({ state: "completed", context: CONTEXT, runners: [] }),
    ChooseRunnerPath: (kind: string) => ({ state: "completed", kind, paths: ["/Users/qa/gate-policy.json"] }),
    InspectGatePolicy: () => ({ state: "completed", identity, promotion_identity: promotion, environment: "qa", revision: "r-12", approver: "Dana" }),
    SaveCIHandoff: () => ({ state: "failed", reason: "the run directory, the reviewed baseline and the retained gate snapshot are three separate folders, none inside another" }),
  });
  render(<SetUpCISheet suite="Scheduling smoke" version="Version 4" environments={[{ id: "qa", name: "Scheduling QA" }]} context={() => CONTEXT} onClose={() => {}} onDone={() => {}} />);
  const sheet = await screen.findByRole("dialog", { name: "Set up CI" });
  for (const [label, value] of [["Readmit program", "/opt/readmit"], ["Operation policy", "/etc/op.json"], ["Suite file", "/srv/suite.json"], ["Run folder", "/srv/runs/1"], ["Coverage declaration", "/srv/cov.json"]] as const) {
    await user.type(within(sheet).getByLabelText(label), value);
  }
  await user.click(within(sheet).getByLabelText("Change gate"));
  await user.click(within(sheet).getByRole("button", { name: "Next" }));
  await user.click(within(sheet).getByRole("button", { name: "Choose gate policy" }));
  expect(await within(sheet).findByText("Dana")).toBeTruthy();
  for (const [label, value] of [["Release pins", "/srv/releases.json"], ["Approved promotion", "/srv/promotion.json"], ["Reviewed baseline run", "/srv/runs/1/base"], ["Gate results folder", "/srv/gates/1"], ["Target revision", "r-12"]] as const) {
    await user.type(within(sheet).getByLabelText(label), value);
  }
  await user.click(within(sheet).getByRole("button", { name: "Next" }));
  await user.click(within(sheet).getByRole("button", { name: "Generate configuration" }));
  expect(await within(sheet).findByText(/three separate folders/)).toBeTruthy();
  expect(facade.oneCall("SaveCIHandoff")[0]).toMatchObject({
    gate: { releases: "/srv/releases.json", promotion: "/srv/promotion.json", promotion_identity: promotion, revision: "r-12", baseline: "/srv/runs/1/base", policy: "/Users/qa/gate-policy.json", policy_identity: identity, snapshot_directory: "/srv/gates/1" },
  });
});

test("imported CI results show the retained suite and gate states read-only", async () => {
  const facade = installFacade({
    ChooseRunnerPath: (kind: string) => ({ state: "completed", kind, paths: ["/Users/qa/ci-run"] }),
    InspectCIResults: () => ({ state: "completed", ci: { schema: "readmit-suite-ci/v1", state: "failed", exit_code: 1 }, gate: { schema: "readmit-ci-gate/v1", state: "unknown", exit_code: 3 } }),
  });
  render(<CIResultsSheet onClose={() => {}} />);
  const sheet = await screen.findByRole("dialog", { name: "CI results" });
  expect(await within(sheet).findByText("Failed")).toBeTruthy();
  expect(within(sheet).getByText("Not verified")).toBeTruthy();
  expect(facade.oneCall("InspectCIResults")).toEqual(["/Users/qa/ci-run"]);
});

// Verify gate rechecks a retained snapshot against the policy it was pinned
// to; what it could not verify stays named, never shown as a pass.
test("a retained change gate is verified against its pinned identity, and what could not be verified is named", async () => {
  const user = userEvent.setup();
  const identity = "c".repeat(64);
  const facade = installFacade({
    ChooseRunnerPath: (kind: string) => ({ state: "completed", kind, paths: [kind === "gate-snapshot" ? "/Users/qa/gate" : "/Users/qa/policy.json"] }),
    InspectGatePolicy: () => ({ state: "completed", identity }),
    VerifyCIGate: () => ({
      state: "failed", reason: "the retained snapshot matches its manifest under this identity, but its repeated assessment is unknown; an unknown gate is never a pass",
      gate: { schema: "readmit-ci-gate/v1", state: "unknown", exit_code: 3, approval: "pass", pins: "pass", coverage: "unknown", baseline: "pass", retention: "retained", target_revision: "pass" },
      unverified: ["coverage"],
    }),
  });
  render(<GateResultsSheet onClose={() => {}} />);
  const sheet = await screen.findByRole("dialog", { name: "Gate results" });
  await user.click(within(sheet).getByRole("button", { name: "Choose gate results folder" }));
  await user.click(within(sheet).getByRole("button", { name: "Choose gate policy" }));
  await user.click(within(sheet).getByRole("button", { name: "Verify gate" }));
  expect(await within(sheet).findAllByText("Not verified")).toHaveLength(2);
  expect(within(sheet).getAllByText(/never a pass/).length).toBeGreaterThan(0);
  expect(facade.oneCall("VerifyCIGate")).toEqual(["/Users/qa/gate", identity]);
});

test("connected CI carries exact installed authority and one stable dispatch instead of legacy release flags",async()=>{
 const user=userEvent.setup();const facade=installFacade({ListRunners:()=>({state:"completed",context:CONTEXT,runners:[]}),SaveCIHandoff:()=>({state:"completed",output:"/handoff.sh"})});
 render(<SetUpCISheet suite="Connected suite" version="Version 1" connected environments={[{id:"qa",name:"QA"}]} context={()=>CONTEXT} onClose={()=>{}} onDone={()=>{}}/>);
 const sheet=within(await screen.findByRole("dialog",{name:"Set up CI"}));
 for(const [label,value] of [["Readmit program","/bin/readmit"],["Operation policy","/etc/operation.json"],["Suite file","/srv/suite.json"],["Run folder","/srv/run"],["Runner configuration","/srv/runner.json"],["Installed authority","/srv/authority.json"],["Approved promotion","/srv/promotion.json"],["Promotion SHA-256","a".repeat(64)],["Target revision","revision-1"],["Dispatch identity","dispatch-1"]])await user.type(sheet.getByLabelText(label!),value!);
 await user.click(sheet.getByRole("button",{name:"Next"}));await user.click(sheet.getByRole("button",{name:"Generate configuration"}));
 expect(facade.oneCall("SaveCIHandoff")[0]).toMatchObject({coverage_file:"",connected:{runner_config:"/srv/runner.json",authority:"/srv/authority.json",promotion:"/srv/promotion.json",promotion_identity:"a".repeat(64),revision:"revision-1",instance:"dispatch-1"}});
});


test("CI folder selection has one owner across strict effect replay and retains a refused read",async()=>{
 const close=()=>{throw new Error("A refusal closed the CI reader");};
 const facade=installFacade({ChooseRunnerPath:kind=>({state:"completed",kind,paths:["/owned/ci"]}),InspectCIResults:()=>({state:"failed",reason:"Original retained CI proof is unavailable"})});
 render(<StrictMode><CIResultsSheet onClose={close}/></StrictMode>);
 const sheet=within(await screen.findByRole("dialog",{name:"CI results"}));await sheet.findByText("Original retained CI proof is unavailable");
 expect(facade.callsTo("ChooseRunnerPath")).toHaveLength(1);expect(facade.callsTo("InspectCIResults")).toHaveLength(1);
});
