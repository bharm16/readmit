import { describe, expect, it } from "vitest";

import { RequestScope, executeReviewedAction, listCatalog, listWholeCatalog, newIntentId, saveItem } from "./bindings";
import type { CatalogItem, CatalogPage, CatalogQuery, CatalogResult, SaveItemRequest } from "./bindings";
import { installFacade } from "./testkit/wails";

describe("catalog bindings", () => {
  it("drops an answer that arrives after the window moved to another project", async () => {
    const scope = new RequestScope();
    const first = scope.enter("/projects/scheduling", "aaaaaaaaaaaaaaaaaaaaaaaa");
    const stub = installFacade();
    const parked = stub.park("ListCatalog");
    const late = listCatalog({ context: first, kind: "case", filter: {} });
    const second = scope.enter("/projects/orders", "bbbbbbbbbbbbbbbbbbbbbbbb");
    const answer: CatalogResult = {
      state: "completed",
      context: first,
      page: { items: [], total: 0, snapshot: "s", recorded: true, incomplete: [] },
    };
    parked.resolve(answer);
    expect(scope.current(await late)).toBe(false);
    expect(scope.current({ context: second })).toBe(true);
    expect(scope.current({ context: scope.next() })).toBe(true);
    expect(scope.current({ context: second })).toBe(false);
  });

  it("follows next_cursor until the list is complete and drops a late page after a context switch", async () => {
    const scope = new RequestScope();
    const first = scope.enter("/projects/scheduling", "aaaaaaaaaaaaaaaaaaaaaaaa");
    const row = (id: number): CatalogItem => ({
      ref: { kind: "environment", id: String(id).padStart(24, "0") },
      name: `Environment ${id}`,
      created_at: null,
      updated_at: null,
      last_opened_at: null,
      availability: "available",
      capabilities: [],
      summary: {},
    });
    const pages: Omit<CatalogPage, "snapshot" | "recorded" | "incomplete">[] = [
      { items: [row(1), row(2)], next_cursor: "second", total: null, partial: true, reason: "more entries" },
      { items: [row(3)], next_cursor: "third", total: null, partial: true, reason: "more entries" },
      { items: [row(4)], total: 4 },
    ];
    const stub = installFacade({
      ListCatalog: (query: CatalogQuery) => {
        const at = query.cursor === "second" ? 1 : query.cursor === "third" ? 2 : 0;
        return {
          state: "completed",
          context: query.context,
          page: { ...pages[at]!, snapshot: `s${at}`, recorded: true, incomplete: [] },
        };
      },
    });
    const whole = await listWholeCatalog({ context: first, kind: "environment", filter: {}, cursor: "stale" });
    expect(whole.page?.items.map((item) => item.name)).toEqual(["Environment 1", "Environment 2", "Environment 3", "Environment 4"]);
    expect(whole.page?.total).toBe(4);
    expect(whole.page?.partial).toBeUndefined();
    expect(whole.page?.next_cursor).toBeUndefined();
    expect(stub.callsTo("ListCatalog").map((call) => (call.args[0] as CatalogQuery).cursor)).toEqual([undefined, "second", "third"]);
    expect(scope.current(whole)).toBe(true);

    // A page parked while the window moves to another project: the whole
    // list answers the context it was asked under and is dropped.
    const parked = stub.park("ListCatalog");
    const late = listWholeCatalog({ context: scope.next(), kind: "environment", filter: {} });
    scope.enter("/projects/orders", "bbbbbbbbbbbbbbbbbbbbbbbb");
    parked.resolve({
      state: "completed",
      context: first,
      page: { items: [row(9)], total: 1, snapshot: "late", recorded: true, incomplete: [] },
    });
    expect(scope.current(await late)).toBe(false);
  });

  it("asks a save that never reached the application again under the same intent", async () => {
    const intent = newIntentId();
    const stub = installFacade();
    let calls = 0;
    stub.reply({
      SaveItem: (request) => {
        calls += 1;
        if (calls === 1) {
          throw new Error("transport dropped");
        }
        return {
          state: "completed",
          context: request.context,
          outcome: "saved",
          saved: { kind: "environment", id: "cccccccccccccccccccccccc", revision: "1" },
          replayed: calls > 1,
          problems: [],
        };
      },
    });
    const request: SaveItemRequest = {
      context: { project: "/projects/scheduling", generation: 1 },
      kind: "environment",
      draft: {},
      intent_id: intent,
    };
    const saved = await saveItem(request);
    expect(saved.outcome).toBe("saved");
    const intents = stub.callsTo("SaveItem").map((call) => (call.args[0] as SaveItemRequest).intent_id);
    expect(intents).toEqual([intent, intent]);
  });

  it("never asks a final action again once the application answered it", async () => {
    const stub = installFacade({
      ExecuteReviewedAction: (request) => ({ state: "busy", context: request.context, outcome: "refused", replayed: false }),
    });
    const answered = await executeReviewedAction({
      context: { project: "/projects/scheduling", generation: 1 },
      token: "opaque",
      intent_id: newIntentId(),
      decisions: {},
    });
    expect(answered.state).toBe("busy");
    expect(stub.callsTo("ExecuteReviewedAction")).toHaveLength(1);
  });
});
