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
  startDatabaseEngine,
  type DatabaseEngine,
} from "../testkit/database-engine.js";
import { NO_QUERY } from "../Messages";
import type { ConnectedTestDraft, ProjectOpenResult } from "../bindings";

let database: DatabaseEngine | null = null;
let journey: Journey;
let fixture: FixtureProtocol | null = null;
beforeEach(() => {
  journey = Journey.create();
});
afterEach(async () => {
  await journey.dispose();
  await fixture?.close();
  await database?.close();
  database = null;
  fixture = null;
});

test.skipIf(!process.env.READMIT_POSTGRES_BIN)(
  "normal connected Run observes actual PostgreSQL fields through its registered reference with unchanged expectations",
  async () => {
    const user = userEvent.setup();
    const project = await licensedProject(journey, user);
    const created = journey.callsTo("CreateNamedProject").at(-1)?.result as
      ProjectOpenResult | undefined;
    if (!created?.context) throw new Error("project context was not published");
    const context = created.context;
    journey.writeFile(
      "exports/feed.hl7",
      EXPORTED_BOOKING + EXPORTED_RESCHEDULE,
    );
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
    if (!source?.summary.case)
      throw new Error("source case was not registered");
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
    const reschedule = sourceRows.rows.find(
      (row) => row.trigger_event === "S13",
    );
    if (!booking || !reschedule)
      throw new Error("original booking/reschedule not present");
    fixture = await startFixtureProtocol({
      folder: journey.path("operator-fixture"),
      project: context.project_id ?? "",
      reset: () => database?.reset(),
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
            templates: [
              { id: "patient", kind: "patient", attributes: ["name"] },
            ],
            timeout_ms: 30000,
          },
        ],
      }),
    );
    database = await startDatabaseEngine({
      postgresBin: process.env.READMIT_POSTGRES_BIN!,
      certificate: journey.path("operator-fixture/certificate.pem"),
      key: journey.path("operator-fixture/key.pem"),
    });
    const downstream = database;
    const databaseWitness = database;
    expect(database.version).toMatch(/PostgreSQL/);
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

    journey.writeFile(
      "operator-fixture/database-reference.sh",
      "#!/bin/sh\nprintf 'synthetic-database-reference-secret'\n",
      0o700,
    );
    const registered = await author.call("SaveCredential", {
      context,
      name: "database-observer",
      purpose: "source-endpoint",
      store: "customer-managed",
      address: database.databaseAddress,
      command: journey.path("operator-fixture/database-reference.sh"),
      arguments: [],
      update: false,
      replace_arguments: false,
    });
    if (registered.state !== "completed")
      throw new Error(
        `reference refused: ${JSON.stringify(registered.problems)}`,
      );
    const defaults = await author.call("OpenItemDraft", {
      context,
      ref: { kind: "observation", id: "" },
    });
    if (!defaults.draft?.observation)
      throw new Error("observation defaults unavailable");
    const sourceDeclaration = {
      kind: "database-query",
      identity: "independent-postgres",
      scope: "appointments",
    };
    const observed = await author.call("SaveItem", {
      context,
      kind: "observation",
      intent_id: "database-observation",
      draft: {
        name: "Actual PostgreSQL appointment",
        observation: {
          ...defaults.draft.observation,
          credential: "database-observer",
          source: {
            schema: "readmit-observation-source/v3",
            source: sourceDeclaration,
            enabled: true,
            freshness: { max_age: "30s" },
            extraction: null,
            file: null,
            http: null,
            capture: null,
            database: {
              driver: "postgresql",
              address: database.databaseAddress,
              classification: "nonproduction",
              name: "application",
              username: "observer",
              ca_file: journey.path("operator-fixture/certificate.pem"),
              server_name: "localhost",
              credential: {
                store: "customer-managed",
                address: database.databaseAddress,
                purpose: "database-observation",
                command: journey.path("operator-fixture/database-reference.sh"),
                arguments: [],
              },
              view: ["public", "observed"],
              record_key: "appointment",
              key_type: "text",
              filters: [],
              limits: { timeout: "5s", max_rows: 100, max_bytes: 65536 },
            },
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
              max_samples: 20,
            },
          },
          connected: {
            schema: "readmit-connected-observation-setup/v1",
            environment: environment.saved.id,
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
              sample_ms: 100,
              max_gap_ms: 1000,
              max_samples: 400,
              max_records: 100,
              max_bytes: 65536,
            },
            projection: {
              schema: "readmit-dataset-projection/v1",
              id: "database-appointments",
              format: "database",
              order: "unordered",
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
                  name: "status",
                  type: "text",
                  locator: ["status"],
                  key: false,
                  required: true,
                  repeated: false,
                },
              ],
              limits: { max_rows: 100, max_bytes: 65536, timeout_ms: 5000 },
            },
          },
        },
      },
    });
    if (!observed.saved)
      throw new Error(
        `database observation refused: ${JSON.stringify(observed.problems)}`,
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
      ],
      phases: [
        {
          id: "exercise",
          name: "Exercise",
          steps: ["book"],
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
            {
              name: "Authored status",
              check: {
                id: "status",
                operator: "value-equals",
                subject: { dataset: "appointments", where: [] },
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
      intent_id: "database-test",
      draft: {
        name: "Database field expectation",
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
      await page().findByRole("row", { name: /Database field expectation/ }),
    );
    const fixtureWitness = fixture;

    for (const [index, mode] of ["wrong", "fixed"].entries()) {
      database.setMode(mode as "wrong" | "fixed");
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
      await review.findByText(/reference database-observer v1/);
      expect(database.received()).toBe(index);
      expect(fixtureWitness.created()).toBe(index);
      await press(user, review.getByRole("button", { name: /^Send/ }));
      await waitFor(
        () =>
          expect(
            fixtureWitness.deleted(),
            JSON.stringify(
              journey.callsTo("ExecuteReviewedAction").at(-1)?.result,
            ),
          ).toBe(index + 1),
        { timeout: 60000 },
      );
      await waitFor(
        () =>
          expect(
            within(
              screen.getByRole("table", { name: "Connected checks" }),
            ).getByRole("row", { name: "typed:status" }).textContent,
            JSON.stringify({
              result: journey.callsTo("ExecuteReviewedAction").at(-1)?.result,
              detail: journey.callsTo("OpenRun").at(-1)?.result,
              received: databaseWitness.received(),
              frames: databaseWitness.frames(),
            }),
          ).toContain(mode === "fixed" ? "passed" : "failed"),
        { timeout: 60000 },
      );
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
      expect(database.received()).toBe(index + 1);
      expect(
        JSON.stringify({
          result: journey.callsTo("ExecuteReviewedAction").at(-1)?.result,
          detail: journey.callsTo("OpenRun").at(-1)?.result,
          received: databaseWitness.received(),
          frames: databaseWitness.frames(),
        }),
      ).not.toContain("synthetic-database-reference-secret");
    }
    const unchanged = await author.call("OpenItemDraft", {
      context,
      ref: saved.saved,
    });
    expect(unchanged.draft?.connected_test).toEqual(draft);
  },
);
