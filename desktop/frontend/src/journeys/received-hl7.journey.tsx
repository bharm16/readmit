import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Journey, press } from "../testkit/journey";
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
import {
  startReceivedEngine,
  type ReceivedEngine,
} from "../testkit/received-engine.js";
import { freeLoopbackAddress } from "./probes.js";
import { NO_QUERY } from "../Messages";
import type { ConnectedTestDraft, ProjectOpenResult } from "../bindings";

let engine: ReceivedEngine | null = null;
let journey: Journey;
let fixture: FixtureProtocol | null = null;
beforeEach(() => {
  journey = Journey.create();
});
afterEach(async () => {
  await journey.dispose();
  await fixture?.close();
  await engine?.close();
  engine = null;
  fixture = null;
});

test("received HL7 normal Run proves an actual authored field failure and corrected pass with original evidence retained", async () => {
  const user = userEvent.setup();
  const project = await licensedProject(journey, user);
  const created = journey.callsTo("CreateNamedProject").at(-1)?.result as
    | ProjectOpenResult
    | undefined;
  if (!created?.context) throw new Error("project context was not published");
  const context = created.context;
  const captureAddress = await freeLoopbackAddress();
  engine = await startReceivedEngine(captureAddress);
  const downstream = engine;
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
  const listener = await author.call("SaveItem", {
    context,
    kind: "source",
    intent_id: "captured-source",
    draft: {
      name: "Received listener",
      source: {
        type: "mllp-listener",
        listener: {
          schema: "readmit-listener/v1",
          bind_address: "127.0.0.1",
          port: Number(captureAddress.split(":")[1]),
          transport: "plain",
          message_limit: 0,
          connection_limit: 2,
          idle_timeout: "5s",
          ack_code: "AA",
          allow_remote: false,
        },
      },
    },
  });
  if (!listener.saved)
    throw new Error(`listener refused: ${JSON.stringify(listener.problems)}`);
  const defaults = await author.call("OpenItemDraft", {
    context,
    ref: { kind: "observation", id: "" },
  });
  if (!defaults.draft?.observation)
    throw new Error("observation defaults unavailable");
  const observed = await author.call("SaveItem", {
    context,
    kind: "observation",
    intent_id: "received-observation",
    draft: {
      name: "Received appointment",
      observation: {
        ...defaults.draft.observation,
        connected: {
          schema: "readmit-connected-observation-setup/v1",
          namespace: "received",
          phase: "after",
          business_keys: [
            { field: "appointment", variable: "appointment-key" },
          ],
          baseline: "before-run",
          capture: {
            source: listener.saved,
            run_selector: "MSH-3",
            output_key_selector: "MSH-10",input_key_selector:"MSH-10",
            include: [],
          },
          completion: {
            schema: "readmit-observation-interval/v1",
            source: "",
            namespace: "received",
            enabled: true,
            mode: "stream",
            freshness: "ingress",
            horizon_ms: 100,
            sample_ms: 100,
            max_gap_ms: 30000,
            max_samples: 400,
            max_records: 100,
            max_bytes: 1 << 20,
          },
          projection: {
            schema: "readmit-dataset-projection/v1",
            id: "received-appointment",
            format: "hl7",
            order: "source",
            columns: [
              {
                name: "appointment",
                type: "text",
                selector: "SCH-1",
                key: true,
                required: true,
                repeated: false,
              },
              {
                name: "status",
                type: "text",
                selector: "SCH-2",
                key: false,
                required: true,
                repeated: false,
              },
            ],
            limits: { max_rows: 100, max_bytes: 1 << 20, timeout_ms: 30000 },
          },
        },
      },
    },
  });
  if (!observed.saved)
    throw new Error(
      `capture observation refused: ${JSON.stringify(observed.problems)}`,
    );
  const draft: ConnectedTestDraft = {
    schema: "readmit-connected-test-authoring/v1",
    boundary: "engine-output",
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
        v2: { runtime_marker_selector: "MSH-3" },
      },
    ],
    phases: [
      {
        id: "exercise",
        name: "Exercise",
        steps: ["book"],
        after: [],
        observations: [
          { dataset: "received", observation: observed.saved, when: "after" },
        ],
        checks: [
          {
            name: "Authored status",
            check: {
              id: "status",
              operator: "value-equals",
              subject: { dataset: "received", where: [] },
              column: "status",
              expected: { state: "present", type: "text", text: "booked" },
            },
          },
        ],
        responses: [],
        validations: [],
        acknowledgements: [],
      },
    ],
  };
  const saved = await author.call("SaveItem", {
    context,
    kind: "test",
    intent_id: "received-test",
    draft: {
      name: "Received field expectation",
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
    await page().findByRole("row", { name: /Received field expectation/ }),
  );
  const fixtureWitness = fixture;
  for (const [index, mode] of ["defective", "fixed"].entries()) {
    downstream.setMode(mode as "defective" | "fixed");
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
    await review.findByText(/engine-output/);
    expect(journey.callsTo("IssueExchangeRuntimeMarker")).toHaveLength(
      index + 1,
    );
    expect(downstream.received()).toHaveLength(index);
    expect(fixtureWitness.created()).toBe(index);
    await press(user, review.getByRole("button", { name: /^Send/ }));
    await waitFor(
      () => {
        const result = journey.callsTo("ExecuteReviewedAction").at(-1)?.result;
        expect(fixtureWitness.deleted(), JSON.stringify(result)).toBe(
          index + 1,
        );
      },
      {
        timeout: 60000,
      },
    );
    await waitFor(
      () =>
        expect(
          within(
            screen.getByRole("table", { name: "Connected checks" }),
          ).getByRole("row", { name: "typed:status" }).textContent,
        ).toContain(mode === "fixed" ? "passed" : "failed"),
      { timeout: 60000 },
    );
    expect(downstream.received()).toHaveLength(index + 1);
    expect(downstream.outputs()[index]).toContain(
      mode === "fixed" ? "booked" : "wrong",
    );
    expect(
      downstream.received()[index]!.split("\r")[0]!.split("|")[2]!,
    ).toMatch(/^run-[0-9a-f]{32}$/);
    await press(
      user,
      screen.getByRole("button", { name: "View retained observation" }),
    );
    const retained = within(
      await screen.findByRole("dialog", { name: "Retained observation" }),
    );
    await retained.findByText(/1 retained records/);
    await press(
      user,
      retained.getByRole("button", { name: "Close retained observation" }),
    );
    await press(
      user,
      screen.getByRole("button", { name: "Inspect supporting HL7" }),
    );
    const raw = within(
      await screen.findByRole("dialog", { name: "Supporting received HL7" }),
    );
    await raw.findByRole("table", { name: "Kept messages" });
    expect(
      journey.callsTo("OpenConnectedCapture").at(-1)?.result,
    ).toMatchObject({ state: "completed" });
    await press(user, raw.getByRole("button", { name: "Close" }));
    expect(downstream.received()).toHaveLength(index + 1);
  }
  const original = await author.call("ReadMessages", {
    workspace: project,
    case: source.summary.case.entry,
    identity: sourceCase.case.identity,
    query: NO_QUERY,
    sort: "",
    offset: 0,
    limit: 50,
  });
  expect(original.rows.find((row) => row.id === booking.id)).toBeDefined();
  const originalCase = await author.call(
    "OpenCase",
    project,
    source.summary.case.entry,
  );
  expect(originalCase.case?.identity).toBe(sourceCase.case.identity);
  const unchanged = await author.call("OpenItemDraft", {
    context,
    ref: saved.saved,
  });
  expect(unchanged.draft?.connected_test).toEqual(draft);
});
