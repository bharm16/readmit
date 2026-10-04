import { expect, test } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ExchangePanel } from "./ExchangePanel";
import { installFacade } from "./testkit/wails";
import type { CatalogItem, ExchangeView } from "./bindings";
const context = () => ({
  project: "/workspace-under-test",
  project_id: "project-under-test",
  generation: 1,
});
const source: CatalogItem = {
  ref: { kind: "source", id: "source-under-test", revision: "1" },
  name: "Receiver under test",
  created_at: null,
  updated_at: null,
  last_opened_at: null,
  availability: "available",
  capabilities: [],
  summary: {},
};
const target: CatalogItem = {
  ...source,
  ref: { kind: "environment", id: "target-under-test", revision: "1" },
  name: "Target under test",
};
const exchange: ExchangeView = {
  schema: "readmit-exploratory-exchange/v1",
  id: "exchange-under-test",
  review: {
    max_frame_bytes: 1048576,
    max_connections: 1,
    max_sessions: 128,
    configuration: { type: "mllp-listener" },
    source_identity: "source-revision-under-test",
    source: source.ref,
    name: source.name,
    address: "listener-under-test",
    ack_code: "AA",
    options: {
      source: source.ref,
      matching: {
        schema: "readmit-exchange-matching/v1",
        mode: "unscoped",
        run_selector: "MSH-3",
        run_id: "runtime-under-test",
        input_key_selector: "MSH-10",
        output_key_selector: "MSH-10",
      },
      horizon_ms: 5000,
      max_messages: 100,
      max_bytes: 8388608,
    },
  },
  input_identity: "input-under-test",
  send_identity: "send-under-test",
  target: {
    name: target.name,
    classification: "nonproduction",
    address: "target-under-test",
    transport: "plain",
    test_endpoint: true,
    approved_transport: false,
    connect_timeout: "1s",
    message_timeout: "1s",
    max_ack_bytes: 65536,
    credential: false,
  },
  output: "run-under-test",
  armed_at: "",
  stimulus_at: "",
  window_ended_at: "",
  completed_at: "",
  bound_address: "listener-under-test",
  coverage: "incomplete",
  delivery_uncertain: true,
  capture_identity: "capture-under-test",
  capture_entry: "capture-entry-under-test",
  received: 0,
  matches: [],
  excluded: [],
};

test("Receive and send selected reviews one owned action and reopening history never rearms or resends", async () => {
  const stub = installFacade({
    ListCatalog: (request) => ({
      state: "completed",
      context: request.context,
      page: {
        items: request.kind === "source" ? [source] : [target],
        total: 1,
        snapshot: "catalog-under-test",
        recorded: true,
        incomplete: [],
      },
    }),
    PrepareAction: (request) => ({
      state: "completed",
      context: request.context,
      review: {
        token: "exchange-review-under-test",
        action: "replay.send",
        consent: "send",
        items: [source, target],
        destination: {
          name: target.name,
          classification: "nonproduction",
          address: "target-under-test",
        },
        ready: true,
        requirements: [],
        exchange: exchange.review,
      },
    }),
    ExecuteReviewedAction: (request) => ({
      state: "completed",
      context: request.context,
      outcome: "uncertain",
      replayed: false,
      exchange,
    }),
    WithdrawReview: () => ({ state: "completed", context: context() }),
    OpenExchangeCapture: (request) => ({
      state: "failed",
      context: request.context,
      session: request.exchange,
      reason: "Retained capture under test is unavailable",
    }),
    ListExchanges: (request) => ({
      state: "completed",
      context: request,
      exchanges: [exchange],
    }),
  });
  const user = userEvent.setup();
  render(
    <ExchangePanel
      context={context}
      caseRef={{ kind: "case", id: "case-under-test" }}
      selected={["occurrence-under-test"]}
      busy={false}
      onCreateVariant={() => {}}
    />,
  );
  await user.click(
    screen.getByRole("button", { name: "Receive and send selected" }),
  );
  await screen.findByRole("option", { name: source.name });
  await user.selectOptions(
    screen.getByLabelText("Receiver source"),
    source.ref.id,
  );
  await user.selectOptions(screen.getByLabelText("Target"), target.ref.id);

  await user.click(screen.getByRole("button", { name: "Review send" }));
  await waitFor(() =>
    expect(
      screen
        .getByRole("button", { name: "Send once" })
        .hasAttribute("disabled"),
    ).toBe(false),
  );
  expect(stub.callsTo("ExecuteReviewedAction")).toHaveLength(0);
  expect(stub.callsTo("PrepareAction")[0]?.args[0]).toMatchObject({
    action: "replay.send",
    replay: {
      messages: ["occurrence-under-test"],
      exchange: {
        source: source.ref,
        matching: { schema: "readmit-exchange-matching/v1", mode: "unscoped" },
      },
    },
  });
  await user.dblClick(screen.getByRole("button", { name: "Send once" }));
  await screen.findByText("Uncertain — inspect retained evidence");
  await user.click(
    screen.getByRole("button", { name: "Inspect received messages" }),
  );
  await screen.findByText("Retained capture under test is unavailable");
  expect(stub.callsTo("OpenExchangeCapture")[0]?.args[0]).toEqual({
    context: context(),
    exchange: exchange.id,
    identity: exchange.capture_identity,
  });
  expect(stub.callsTo("ExecuteReviewedAction")).toHaveLength(1);
  await user.click(screen.getByRole("button", { name: "Done" }));
  await user.click(screen.getByRole("button", { name: "Exchange history" }));
  await user.click(
    await screen.findByRole("button", {
      name: /Receiver under test → Target under test/,
    }),
  );
  expect(screen.getByText("Incomplete capture · 0 retained")).toBeTruthy();
  expect(stub.callsTo("ExecuteReviewedAction")).toHaveLength(1);
  expect(stub.callsTo("StartCapture")).toHaveLength(0);
  expect(stub.callsTo("SendReplay")).toHaveLength(0);
});

test("runtime marker issuance opens the existing variant owner explicitly and never edits or sends originals", async () => {
  const stub = installFacade({
    ListCatalog: (request) => ({
      state: "completed",
      context: request.context,
      page: {
        items: request.kind === "source" ? [source] : [target],
        total: 1,
        snapshot: "catalog",
        recorded: true,
        incomplete: [],
      },
    }),
    IssueExchangeRuntimeMarker: (request) => ({
      state: "completed",
      context: request,
      marker: "opaque-marker-under-test",
    }),
  });
  const user = userEvent.setup();
  let variants = 0;
  render(
    <ExchangePanel
      context={context}
      caseRef={{ kind: "case", id: "original-under-test" }}
      selected={["selected-under-test"]}
      busy={false}
      onCreateVariant={() => {
        variants++;
      }}
    />,
  );
  await user.click(
    screen.getByRole("button", { name: "Receive and send selected" }),
  );
  await user.click(
    screen.getByRole("checkbox", {
      name: "Associate output using a locally reserved runtime marker",
    }),
  );
  await user.click(
    screen.getByRole("button", { name: "Create runtime marker" }),
  );
  await waitFor(() =>
    expect(
      (screen.getByLabelText("Issued runtime marker") as HTMLInputElement)
        .value,
    ).toBe("opaque-marker-under-test"),
  );
  await user.click(
    screen.getByRole("button", { name: "Create variant for runtime marker" }),
  );
  expect(variants).toBe(1);
  expect(stub.callsTo("IssueExchangeRuntimeMarker")[0]?.args).toEqual([
    context(),
  ]);
  expect(stub.callsTo("SaveItem")).toHaveLength(0);
  expect(stub.callsTo("PrepareAction")).toHaveLength(0);
  expect(stub.callsTo("ExecuteReviewedAction")).toHaveLength(0);
});

test("exact retained partial exchange offers a draft while keeping observed output outside expectations", async () => {
  const user = userEvent.setup();
  const pinned: ExchangeView = { ...exchange, schema: "readmit-exploratory-exchange/v2", identity: "exact-retained-identity", inputs: { case: { kind: "case", id: "original-case" }, identity: exchange.input_identity, messages: ["second-input", "first-input"] }, target_ref: target.ref, received: 0 };
  const stub = installFacade({ ListExchanges: request => ({ state: "completed", context: request, exchanges: [pinned] }) });
  const promoted: ExchangeView[] = [];
  render(<ExchangePanel context={context} caseRef={null} selected={[]} busy={false} onCreateVariant={()=>{}} requestedExchange={{ id: pinned.id, identity: pinned.identity! }} onCreateTest={value=>promoted.push(value)} />);
  expect(await screen.findByText(/Received records are observed evidence/)).toBeTruthy();
  await user.click(await screen.findByRole("button", { name: "Create test draft from exchange" }));
  expect(promoted).toEqual([pinned]);
  expect(stub.callsTo("PrepareAction")).toHaveLength(0);
  expect(stub.callsTo("ExecuteReviewedAction")).toHaveLength(0);
});
