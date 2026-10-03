import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Journey, press, enter } from "../testkit/journey";
import { goTo, page } from "../testkit/navigation";
import {
  startFixtureProtocol,
  type FixtureProtocol,
} from "../testkit/fixture-protocol.js";
import {
  EXPORTED_BOOKING,
  EXPORTED_RESCHEDULE,
  importExport,
  licensedProject,
} from "./steps";
import { NO_QUERY } from "../Messages";
import type {
  ConnectedTestDraft,
  CatalogItem,
  ProjectOpenResult,
  ImportCSVDialect,
 ReviewedActionResult,
 RunComparisonItemsResult,
 ReportView,
 ActionReviewResult,
} from "../bindings";

let journey: Journey;
let fixture: FixtureProtocol | null = null;
beforeEach(() => {
  journey = Journey.create();
});
afterEach(async () => {
  await journey.dispose();
  await fixture?.close();
  fixture = null;
});

test("normal Run retains actual defect fix regression evidence under unchanged connected expectations", async () => {
  const user = userEvent.setup();
  const {context,downstream,author,environment,saved,draft,fixtureWitness} = await prepareConnected(user);
  const artifacts: number[] = [];
  for (const [index, mode] of [
    "duplicating",
    "fixed",
    "duplicating",
  ].entries()) {
    downstream.setMode(mode as "duplicating" | "fixed");
    await journey.settled();
    if (index === 0)
      await press(user, await page().findByRole("button", { name: "Run" }));
    else {
      await press(
        user,
        page().getByRole("button", { name: "More run actions" }),
      );
      await press(
        user,
        await screen.findByRole("menuitem", { name: "Run again" }),
      );
    }
    const review = within(
      await screen.findByRole("dialog", { name: "Run test" }),
    );
    await review.findByText(/application-state/);
    expect(fixtureWitness.created()).toBe(index);
    await press(user, review.getByRole("button", { name: /^Send/ }));
    await waitFor(() => expect(fixtureWitness.deleted()).toBe(index + 1), {
      timeout: 60000,
    });
    await waitFor(
      () => {
        const table = within(
          screen.getByRole("table", { name: "Connected checks" }),
        );
        expect(
          table.getByRole("row", { name: "typed:count" }).textContent,
        ).toContain(mode === "fixed" ? "passed" : "failed");
        expect(
          table.getByRole("row", { name: "typed:count" }).textContent,
        ).toContain(mode === "fixed" ? "1" : "2");
      },
      { timeout: 60000 },
    );
    expect(Object.keys(downstream.ledger())).toHaveLength(
      mode === "fixed" ? 1 : 2,
    );
    expect(fixtureWitness.created()).toBe(index + 1);
    expect(fixtureWitness.deleted()).toBe(index + 1);
    artifacts.push(journey.callsTo("ExecuteReviewedAction").length);
  }
  expect(artifacts).toHaveLength(3);
  const unchanged = await author.call("OpenItemDraft", {
    context,
    ref: saved.saved!,
  });
  expect(unchanged.draft?.connected_test).toEqual(draft);
  // A colleague changes the authored count after the unchanged regression
  // cycle. Compare must report a changed definition, not an engine fix.
  const originalRuns=journey.callsTo("ExecuteReviewedAction").map(call=>(call.result as ReviewedActionResult).run).filter((run):run is NonNullable<typeof run>=>!!run);
  const changed=structuredClone(draft);changed.phases[0]!.checks[0]!.check.count=2;
  const revision=await author.call("SaveItem",{context,kind:"test",item:saved.saved!.id,base_revision:saved.saved!.revision!,intent_id:"changed-history-expectation",draft:{name:"Observed rescheduling",connected_test:changed,test_links:{environment:environment.saved!.id,reset:"environment",checks:[],tags:[]}}});
  if (!revision.saved) throw new Error(`changed expectation refused: ${JSON.stringify(revision)}`);
  await goTo(user,"Tests");await goTo(user,"Tests");
  const testListing=await author.call("ListCatalog",{context,kind:"test",filter:{}});
  const actualName=testListing.page?.items.find(item=>item.ref.id===revision.saved!.id)?.name;
  if (!actualName) throw new Error("changed publication missing from catalog");
  await press(user,await page().findByRole("row",{name:new RegExp(actualName)}));await press(user,page().getByRole("button",{name:"Run"}));
  const fresh=within(await screen.findByRole("dialog",{name:"Run test"}));await fresh.findByText(/application-state/);await press(user,fresh.getByRole("button",{name:/^Send/}));
  await waitFor(()=>expect(fixtureWitness.deleted()).toBe(4),{timeout:60000});
  await waitFor(()=>expect(within(screen.getByRole("table",{name:"Connected checks"})).getByRole("row",{name:"typed:count"}).textContent).toContain("passed"),{timeout:60000});
  const current=(journey.callsTo("ExecuteReviewedAction").at(-1)?.result as ReviewedActionResult).run!;
  await goTo(user,"Runs");
  const history=within(await page().findByRole("table",{name:"Runs"}));
  const choices=(await history.findAllByRole("checkbox")).filter(box=>box.getAttribute("aria-label")?.startsWith("Select "));
  // History is ordered newest first. Select two genuine retained executions,
  // then deliberately choose their Before/After roles in the comparison.
  await user.click(choices[0]!);await user.click(choices.at(-1)!);await press(user,page().getByRole("button",{name:"Compare"}));
  await page().findByRole("region",{name:"Connected Before and After"});
  await press(user,page().getByRole("button",{name:"Swap before and after"}));
  await waitFor(()=>expect(journey.callsTo("CompareRunItems").at(-1)?.result).toMatchObject({state:"completed",comparison:{earlier:{run:{id:originalRuns[0]!.id}},later:{run:{id:current.id}}}}));
  const compared=journey.callsTo("CompareRunItems").at(-1)?.result as RunComparisonItemsResult;
  expect(compared.comparison?.connected?.checks[0]).toMatchObject({definition:"changed",behavior:"not_compared"});
  expect(compared.comparison?.connected?.dimensions.some(row=>row.dimension==="check-definition" && row.state==="changed")).toBe(true);
  expect(compared.comparison?.earlier.run.id).toBe(originalRuns[0]!.id);expect(compared.comparison?.later.run.id).toBe(current.id);
  await fixture!.close();fixture=null;
  await press(user,page().getByRole("button",{name:"Open before evidence"}));
  await page().findByRole("table",{name:"Connected checks"});
  expect(within(screen.getByRole("table",{name:"Connected checks"})).getByRole("row",{name:"typed:count"}).textContent).toContain("failed");
  await press(user,page().getByRole("button",{name:/Back to/}));
  await page().findByRole("region",{name:"Connected Before and After"});
  expect(journey.callsTo("ExecuteReviewedAction")).toHaveLength(4);

});

async function prepareConnected(user:ReturnType<typeof userEvent.setup>) {
  const project = await licensedProject(journey, user);
  const created = journey.callsTo("CreateNamedProject").at(-1)?.result as
    | ProjectOpenResult
    | undefined;
  if (!created?.context) throw new Error("project context was not published");
  const context = created.context;
  const downstream = await journey.startDownstream(
    "downstream/appointments.csv",
    "duplicating",
  );
  journey.writeFile("exports/feed.hl7", EXPORTED_BOOKING + EXPORTED_RESCHEDULE);
  await importExport(user, journey, "exports/feed.hl7", "Connected inputs");
  await journey.settled();
  const author = journey.colleague("test-author");
  const license = journey.provisionLicense("colleagues/test-author-license");
  await author.call(
    "SelectOperationPolicy",
    `${license}/operation-policy.json`,
  );
  // Another integration engineer prepared this saved test. The person under
  // test uses only their normal Run UI; no call stands in for their action.
  const cases = await author.call("ListCatalog", {
    context,
    kind: "case",
    filter: {},
  });
  const source = cases.page?.items.find(
    (item) => item.name === "Connected inputs",
  );
  if (!source?.summary.case) throw new Error("source case was not registered");
  const sourceCase = await author.call(
    "OpenCase",
    project,
    source.summary.case.entry,
  );
  if (!sourceCase.case) throw new Error("source input did not verify");
  const sourceRows = await author.call("ReadMessages", {
    workspace: project,
    case: source.summary.case.entry,
    identity: sourceCase.case.identity,
    query: NO_QUERY,
    sort: "",
    offset: 0,
    limit: 50,
  });
  const booking = sourceRows.rows.find((row) => row.trigger_event === "S12");
  const reschedule = sourceRows.rows.find((row) => row.trigger_event === "S13");
  if (!booking || !reschedule)
    throw new Error("original booking/reschedule not present");
  fixture = await startFixtureProtocol({
    folder: journey.path("operator-fixture"),
    project: context.project_id ?? "",
    reset: () => downstream.reset(),
  });
  journey.writeFile(
    "operator-fixture/provider.sh",
    "#!/bin/sh\nprintf 'journey-%s' \"$1\"\n",
    0o700,
  );
  const endpoint = new URL(fixture.url).host;
  const role = (name: string, purpose: string) => ({
    endpoint: `fixture-${name}`,
    reference: {
      endpoint,
      purpose,
      generation: "1",
      header: "Authorization",
      prefix: "Bearer ",
      locator: {
        command: journey.path("operator-fixture/provider.sh"),
        arguments: [name],
      },
    },
  });
  journey.writeFile(
    "operator-fixture/registry.json",
    JSON.stringify({
      schema: "readmit-fixture-adapter-registry/v1",
      adapters: [
        {
          id: "journey-fixture",
          revision: "1",
          project: context.project_id,
          environment: "journey",
          environment_revision: "1",
          classification: "nonproduction",
          tenant: "synthetic",
          namespace: "journey-fixtures",
          url: fixture.url,
          server_name: "localhost",
          authorities: fixture.encodedAuthorities,
          read: role("read", "observation-read"),
          setup: role("setup", "setup-action"),
          cleanup: role("cleanup", "setup-action"),
          templates: [{ id: "patient", kind: "patient", attributes: ["name"] }],
          timeout_ms: 30000,
        },
      ],
    }),
  );
  const environment = await author.call("SaveItem", {
    context,
    kind: "environment",
    intent_id: "author-environment",
    draft: {
      name: "Connected target",
      environment: {
        schema: "readmit-target/v3",
        name: "Connected target",
        classification: "nonproduction",
        test_endpoint: true,
        address: downstream.address,
        transport: "plain",
        approved_transport: true,
        connect_timeout: "5s",
        message_timeout: "10s",
        max_ack_bytes: 65536,
      },
      policy: {
        schema: "readmit-send-policy/v1",
        approved_destinations: ["127.0.0.1/32"],
      },
      isolation: {
        schema: "readmit-environment-isolation/v1",
        name: "Fresh receiver",
        registry_file: journey.path("operator-fixture/registry.json"),
        adapter: "journey-fixture",
        mode: "isolated-tenant",
        resources: [
          {
            id: "patient",
            name: "Synthetic fixture",
            kind: "patient",
            template: "patient",
            ownership: "create",
            depends_on: [],
            attributes: { name: "Synthetic" },
            identifiers: [],
          },
        ],
        manual: [],
      },
    },
  });
  if (!environment.saved)
    throw new Error(
      `environment refused: ${JSON.stringify(environment.problems)}`,
    );
  const sourceDeclaration = {
    kind: "file-export",
    identity: "independent-ledger",
    scope: "appointments",
  };
  const csv: ImportCSVDialect = {
    delimiter: ",",
    record_separator: "lf",
    header: "present",
    fields: 2,
  };
  const observed = await author.call("SaveItem", {
    context,
    kind: "observation",
    intent_id: "author-observation",
    draft: {
      name: "Appointment records",
      observation: {
        source: {
          schema: "readmit-observation-source/v2",
          source: sourceDeclaration,
          enabled: true,
          freshness: { max_age: "1h" },
          extraction: {
            envelope: "csv",
            encoding: "utf-8",
            csv,
            record_key: ["appointment"],
          },
          file: {
            path: journey.path("downstream/appointments.csv"),
            max_bytes: 65536,
          },
          http: null,
          capture: null,
        },
        window: {
          schema: "readmit-observation-window/v1",
          source: sourceDeclaration,
          watermark: { kind: "none", position: "" },
          pre_existing_state: {
            declaration: "declared-empty",
            baseline_identity: "",
          },
          completion: {
            deadline: "3s",
            quiet_period: "10ms",
            stable_samples: 2,
            max_records: 100,
            max_samples: 32,
          },
        },
        connected: {
          schema: "readmit-connected-observation-setup/v1",
          namespace: "appointments",
          phase: "both",
          business_keys: [
            { field: "appointment", variable: "appointment-key" },
          ],
          baseline: "before-run",
          completion: {
            schema: "readmit-observation-interval/v1",
            source: "",
            namespace: "appointments",
            enabled: true,
            mode: "snapshots",
            freshness: "snapshot-only",
            horizon_ms: 100,
            sample_ms: 25,
            max_gap_ms: 30000,
            max_samples: 400,
            max_records: 100,
            max_bytes: 65536,
          },
          projection: {
            schema: "readmit-dataset-projection/v1",
            id: "appointments",
            format: "csv",
            order: "source",
            envelope: { encoding: "utf-8", csv },
            columns: [
              {
                name: "appointment",
                type: "text",
                locator: ["appointment"],
                key: true,
                required: true,
                repeated: false,
              },
              {
                name: "start",
                type: "text",
                locator: ["start"],
                key: false,
                required: true,
                repeated: false,
              },
            ],
            limits: { max_rows: 100, max_bytes: 65536, timeout_ms: 30000 },
          },
        },
      },
    },
  });
  if (!observed.saved)
    throw new Error(
      `observation refused: ${JSON.stringify(observed.problems)}`,
    );
  const draft: ConnectedTestDraft = {
    schema: "readmit-connected-test-authoring/v1",
    boundary: "application-state",
    generation: { seed: 7, base_time: "2026-03-01T00:00:00Z" },
    variables: [
      { id: "appointment-key", kind: "literal", value: "PLACER-101" },
    ],
    steps: [
      {
        id: "book",
        after: [],
        source: {
          case: source.ref,
          identity: sourceCase.case.identity,
          occurrence: booking.id,
        },
        v2: {},
      },
      {
        id: "reschedule",
        after: ["book"],
        source: {
          case: source.ref,
          identity: sourceCase.case.identity,
          occurrence: reschedule.id,
        },
        v2: {},
      },
    ],
    phases: [
      {
        id: "exercise",
        name: "Exercise",
        steps: ["book", "reschedule"],
        after: [],
        observations: [
          {
            dataset: "appointments",
            observation: observed.saved,
            when: "after",
          },
        ],
        checks: [
          {
            name: "One appointment",
            check: {
              id: "count",
              operator: "row-count",
              subject: { dataset: "appointments", where: [] },
              count: 1,
            },
          },
        ],
        responses: [],
        validations: [],
        acknowledgements: [
          { id: "book-ack", name: "Booking receipt", step: "book", code: "AA" },
          {
            id: "move-ack",
            name: "Move receipt",
            step: "reschedule",
            code: "AA",
          },
        ],
      },
    ],
  };
  const saved = await author.call("SaveItem", {
    context,
    kind: "test",
    intent_id: "author-test",
    draft: {
      name: "Unchanged appointment expectation",
      connected_test: draft,
      test_links: {
        environment: environment.saved.id,
        reset: "environment",
        checks: [],
        tags: [],
      },
    },
  });
  if (!saved.saved)
    throw new Error(`test refused: ${JSON.stringify(saved.problems)}`);
  await goTo(user, "Tests");
  await user.click(
    await page().findByRole("row", {
      name: /Unchanged appointment expectation/,
    }),
  );
  if (!fixture) throw new Error("independent fixture unavailable");
 return {project,context,downstream,author,environment,saved,draft,fixtureWitness:fixture};
}

test("crashed connected execution reopens its retained scope and refuses continuation without resending",async()=>{
 const user=userEvent.setup();const {project,downstream,fixtureWitness}=await prepareConnected(user);
 downstream.holdAcknowledgements();
 await press(user,page().getByRole("button",{name:"Run"}));
 const review=within(await screen.findByRole("dialog",{name:"Run test"}));await review.findByText(/application-state/);
 await press(user,review.getByRole("button",{name:/^Send/}));
 await waitFor(()=>expect(downstream.received()).toHaveLength(1),{timeout:30000});
 expect(fixtureWitness.created()).toBe(1);
 await journey.crash();await journey.launch();
 await press(user,await page().findByRole("row",{name:"Scheduling interface"}));
 await goTo(user,"Runs");
 await page().findByRole("table",{name:"Runs"});
 await waitFor(()=>expect(page().getByRole("table",{name:"Runs"}).querySelectorAll("tr[data-row-id]"),JSON.stringify(journey.callsTo("ListCatalog").slice(-4).map(call=>({request:call.args[0],result:call.result})))).toHaveLength(1),{timeout:10000});
 await press(user,page().getByRole("table",{name:"Runs"}).querySelector<HTMLElement>("tr[data-row-id]")!);
 await waitFor(()=>expect(journey.callsTo("OpenRun").at(-1)?.result,JSON.stringify(journey.callsTo("OpenRun").at(-1)?.result)).toMatchObject({state:"completed"}),{timeout:10000});
 await press(user,await page().findByRole("tab",{name:"Details"}));
 const recovery=within(await page().findByRole("region",{name:"Recovery"}));
 await recovery.findByText(/no admitted desktop continuation checkpoint/);
 expect(recovery.queryByRole("button",{name:"Resume remaining"})).toBeNull();
 const opened=journey.callsTo("OpenRun").at(-1)?.result as {run?:{lifecycle?:{lifecycle:{state:string,cleanup:string}},item:{summary:{run?:{entry?:string,test_association?:string,can_compare?:boolean}}}}};
 expect(opened.run?.item.summary.run?.test_association).toBe("linked");
 expect(opened.run?.item.summary.run?.can_compare).toBe(false);
 expect(opened.run?.lifecycle?.lifecycle.cleanup).not.toBe("complete");
 downstream.releaseAcknowledgement();
 await press(user,page().getByRole("button",{name:/Back to/}));
 await page().findByRole("table",{name:"Runs"});
 expect(downstream.received()).toHaveLength(1);
 expect(journey.callsTo("ExecuteReviewedAction")).toHaveLength(1);
 expect(fixtureWitness.created()).toBe(1);
 expect(project).toBeTruthy();
});


test("actual connected report exports its reviewed value-free extract and reopens retained evidence offline",async()=>{
 const user=userEvent.setup();const {context,downstream,fixtureWitness,author,saved}=await prepareConnected(user);
 downstream.setMode("fixed");await press(user,page().getByRole("button",{name:"Run"}));
 const review=within(await screen.findByRole("dialog",{name:"Run test"}));await review.findByText(/application-state/);await press(user,review.getByRole("button",{name:/^Send/}));
 await waitFor(()=>expect(fixtureWitness.deleted()).toBe(1),{timeout:60000});
 await page().findByRole("table",{name:"Connected checks"});
 await press(user,page().getByRole("button",{name:"Create report"}));
 const creating=within(await screen.findByRole("dialog",{name:"New report"}));await enter(user,await creating.findByLabelText("Name"),"Actual connected evidence");await press(user,creating.getByRole("button",{name:"Create"}));
 await page().findByRole("region",{name:"Connected report evidence"});
 const opened=(journey.callsTo("OpenReport").at(-1)?.result as {report?:ReportView}).report!;
 expect(opened.connected?.runs[0]?.run.verdict).toBe("pass");expect(opened.connected?.equivalence.state).toBe("not-claimed");
 await goTo(user,"Tests");
 if(!page().queryByRole("table",{name:"Tests"}))await goTo(user,"Tests");
 const testList=await page().findByRole("table",{name:"Tests"});
 await user.dblClick(await within(testList).findByText("Unchanged appointment expectation"));
 for(const name of ["Inputs","Expectations","Runs","Before/after","Exports"])expect(page().getByRole("tab",{name})).toBeTruthy();
 await press(user,page().getByRole("tab",{name:"Runs"}));
 const ownRuns=await page().findByRole("table",{name:"Runs"});await waitFor(()=>expect(ownRuns.querySelectorAll("tr[data-row-id]")).toHaveLength(1));
 const linked=(journey.callsTo("ListCatalog").filter(call=>(call.args[0] as {kind:string}).kind==="run" && (call.result as {state?:string})?.state==="completed").at(-1)!.result as {page:{items:CatalogItem[]}}).page.items;
 expect(linked[0]?.summary.run?.test?.id).toBe(saved.saved?.id);
 await press(user,page().getByRole("tab",{name:"Exports"}));
 const ownReports=await page().findByRole("table",{name:"Test reports"});await within(ownReports).findByText("Actual connected evidence");
 await press(user,within(ownReports).getByRole("button",{name:"Open report"}));await page().findByRole("region",{name:"Connected report evidence"});
 expect(journey.callsTo("ExecuteReviewedAction")).toHaveLength(1);
 await press(user,page().getByRole("button",{name:"Share"}));
 await press(user,await page().findByRole("radio",{name:"Value-free extract"}));
 await press(user,page().getByRole("button",{name:"Redaction"}));
 await page().findByRole("table",{name:"Redaction"});
 await press(user,page().getByRole("button",{name:"Preview"}));
 await waitFor(()=>expect((journey.callsTo("PrepareAction").at(-1)?.result as ActionReviewResult)?.review?.report_share).toMatchObject({connected_mode:"value-free-extract",output:{type:"folder",name:"connected-extract"}}),{timeout:15000});
 journey.makeFolder("shared");await journey.nameNewFolder(journey.path("shared/connected-extract"),"Export package");
 await press(user,page().getByRole("button",{name:"Choose"}));
 await waitFor(()=>expect((journey.callsTo("PrepareAction").at(-1)?.result as ActionReviewResult)?.review?.ready).toBe(true),{timeout:15000});
 const preview=(journey.callsTo("PrepareAction").at(-1)?.result as ActionReviewResult).review!.report_share!;
 expect(preview.source_values).toBe(false);expect(preview.redacted).toBe(true);expect(preview.output.files[0]?.text).toContain("readmit-connected-extract/v1");
 await press(user,page().getByRole("button",{name:"Export"}));
 await waitFor(()=>expect(journey.callsTo("ExecuteReviewedAction").at(-1)?.result).toMatchObject({outcome:"completed"}));
 const extracted=journey.readFile("shared/connected-extract/extract.json");
 expect(extracted).toBe(preview.output.files[0]!.text);
 expect(JSON.parse(extracted)).toMatchObject({schema:"readmit-connected-extract/v1",evidence_class:"disclosure-reviewed-extract",equivalence:{state:"unverified"},residual:{status:"passed"}});
 expect(journey.readFile("shared/connected-extract/identity.sha256").trim()).not.toBe("");
 const original=await author.call("OpenReport",{context,ref:opened.item.ref,reveal:false});expect(original.report?.packet).toBe(opened.packet);
 expect(fixtureWitness.created()).toBe(1);expect(journey.callsTo("ExecuteReviewedAction")).toHaveLength(2);
 await press(user,page().getByRole("button",{name:"Done"}));
 await page().findByRole("region",{name:"Connected report evidence"});
 await fixtureWitness.close();fixture=null;await journey.close();await journey.launch();
 await page().findByRole("region",{name:"Connected report evidence"});
 expect((journey.callsTo("OpenReport").at(-1)?.result as {report?:ReportView}).report?.packet).toBe(opened.packet);
});
