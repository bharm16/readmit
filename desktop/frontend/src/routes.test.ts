// The route model: Back returns to the place left with what it had, a view
// change is not a place to go back to, the sidebar returns each destination to
// where it was left, and switching project starts over.
import { expect, test } from "vitest";
import { initialRoutes, routeReducer, sidebarOf, viewOf, type RouteAction, type RouteState } from "./routes";

const start = initialRoutes({ destination: "cases", projectId: "p1" });
const apply = (state: RouteState, ...actions: RouteAction[]) => actions.reduce(routeReducer, state);

test("Back returns to the list with its selection, query, sort and scroll position", () => {
  const leaving = { selection: "case-7", scrollTop: 440 };
  const opened = apply(start, { type: "go", to: { destination: "cases", objectId: "case-7", view: "messages" }, leaving });
  expect(opened.current).toEqual({ destination: "cases", projectId: "p1", objectId: "case-7", view: "messages" });
  const back = apply(opened, { type: "view", view: "timeline" }, { type: "back" });
  expect(back.current).toEqual({ destination: "cases", projectId: "p1", returnContext: leaving });
  // Changing view after returning does not restore the list again.
  expect(routeReducer(back, { type: "view", view: "all" }).current.returnContext).toBeUndefined();
  // There is nowhere further back.
  expect(routeReducer(back, { type: "back" })).toBe(back);
});

test("the sidebar returns a destination to where it was left, and Back never leaves the destination", () => {
  const state = apply(
    start,
    { type: "go", to: { destination: "cases", objectId: "case-7", view: "findings" }, leaving: { selection: "case-7" } },
    { type: "destination", destination: "tests" },
    { type: "go", to: { destination: "library", view: "profiles" } },
    { type: "destination", destination: "cases" },
  );
  expect(state.current).toEqual({ destination: "cases", projectId: "p1", objectId: "case-7", view: "findings" });
  const listed = routeReducer(state, { type: "back" });
  expect(listed.current).toEqual({ destination: "cases", projectId: "p1", returnContext: { selection: "case-7" } });
  const library = routeReducer(listed, { type: "destination", destination: "tests" });
  expect(library.current).toEqual({ destination: "library", projectId: "p1", view: "profiles" });
  // Library was entered from Tests, so Back goes to Tests.
  expect(routeReducer(library, { type: "back" }).current).toEqual({ destination: "tests", projectId: "p1" });
});

test("an object opened in another's stead goes back where the first would have", () => {
  const caseA = apply(start, { type: "go", to: { destination: "cases", objectId: "a" }, leaving: { selection: "a" } });
  const caseB = routeReducer(caseA, { type: "replace", to: { destination: "cases", objectId: "b", view: "messages" } });
  expect(caseB.current).toEqual({ destination: "cases", projectId: "p1", objectId: "b", view: "messages" });
  expect(routeReducer(caseB, { type: "back" }).current).toEqual({ destination: "cases", projectId: "p1", returnContext: { selection: "a" } });
});

test("the sidebar's own destination, asked for from inside it, is its landing", () => {
  const library = apply(start, { type: "destination", destination: "tests" }, { type: "go", to: { destination: "library", view: "profiles" } });
  expect(routeReducer(library, { type: "destination", destination: "tests" }).current).toEqual({ destination: "tests", projectId: "p1" });
});

test("a view is the place's own or its first", () => {
  const views = [{ key: "messages" }, { key: "timeline" }] as const;
  expect(viewOf({ destination: "cases", view: "timeline" }, "cases", views)).toBe("timeline");
  expect(viewOf({ destination: "cases", view: "bogus" }, "cases", views)).toBe("messages");
  expect(viewOf({ destination: "tests", view: "timeline" }, "cases", views)).toBe("messages");
});

test("a child reached from another destination goes back to its own landing", () => {
  const state = apply(start, { type: "go", to: { destination: "run-test", objectId: "suite-a" } });
  expect(routeReducer(state, { type: "back" }).current).toEqual({ destination: "runs", projectId: "p1" });
});

test("switching project keeps nothing of the last one", () => {
  const deep = apply(
    start,
    { type: "go", to: { destination: "cases", objectId: "case-7" }, leaving: { selection: "case-7" } },
    { type: "destination", destination: "tests" },
  );
  const switched = routeReducer(deep, { type: "project", projectId: "p2", to: { destination: "cases" } });
  expect(switched).toEqual({ current: { destination: "cases", projectId: "p2" }, history: {}, left: {} });
  const closed = routeReducer(deep, { type: "project", projectId: undefined, to: { destination: "home" } });
  expect(closed.current).toEqual({ destination: "home" });
});

test("a child destination sits under its sidebar destination", () => {
  expect(sidebarOf("library")).toBe("tests");
  expect(sidebarOf("run-test")).toBe("runs");
  expect(sidebarOf("benchmarks")).toBe("tools");
  expect(sidebarOf("environments")).toBe("environments");
});
