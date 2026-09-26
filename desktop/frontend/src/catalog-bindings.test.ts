import { describe, expect, it } from "vitest";

import { RequestScope, executeReviewedAction, listCatalog, newIntentId, saveItem } from "./bindings";
import type { CatalogResult, SaveItemRequest } from "./bindings";
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
