// Where the window is, as one explicit value, and the way back. A route names
// a destination, the project it belongs to, the object open in it and its local
// view. Going forward keeps the place being left, with the selection and scroll
// position it had, so Back returns to exactly that. A list that gains a query,
// filter or sort adds it to ReturnContext when it does. Switching
// project starts a new history: nothing selected, typed or revealed in one
// project is carried into another.
import { useCallback, useMemo, useState } from "react";

/** The sidebar's destinations. */
export type Destination =
  | "home"
  | "cases"
  | "tests"
  | "runs"
  | "environments"
  | "reports"
  | "tools"
  | "settings"
  | "help";

/** Destinations reached from inside another, each with its way back. */
export type ChildDestination =
  | "library"
  | "baselines"
  | "run-test"
  | "compare-runs"
  | "share-report"
  | "export-report"
  | "notes"
  | "case-notes"
  | "case-attachments"
  | "project-files"
  | "inspect-file"
  | "sample-data"
  | "benchmarks";

export const CHILD_OF: Record<ChildDestination, Destination> = {
  library: "tests",
  baselines: "tests",
  "run-test": "runs",
  "compare-runs": "runs",
  "share-report": "reports",
  "export-report": "reports",
  notes: "cases",
  "case-notes": "cases",
  "case-attachments": "cases",
  "project-files": "cases",
  "inspect-file": "tools",
  "sample-data": "tools",
  benchmarks: "tools",
};

export type Place = Destination | ChildDestination;

/** The sidebar destination a place sits under. */
export function sidebarOf(place: Place): Destination {
  return place in CHILD_OF ? CHILD_OF[place as ChildDestination] : (place as Destination);
}

/** What a list had when it was left: restored by Back. */
export type ReturnContext = {
  /** The object selected in the list left. */
  selection?: string;
  /** How far the page had been scrolled, in CSS pixels. */
  scrollTop?: number;
};

export type Route = {
  destination: Place;
  projectId?: string;
  objectId?: string;
  view?: string;
  returnContext?: ReturnContext;
};

/** The current route, and per sidebar destination the places to go back to
 * and where it was last left. */
export type RouteState = {
  current: Route;
  history: Partial<Record<Destination, Route[]>>;
  left: Partial<Record<Destination, Route>>;
};

export type RouteAction =
  /** Goes forward. Within one sidebar destination the place left (with what it
   * had) is kept to come back to; into another, it starts there. */
  | { type: "go"; to: Route; leaving?: ReturnContext }
  /** Moves to a sidebar destination, back where it was last left; asked for
   * from inside that destination, it goes to its landing. */
  | { type: "destination"; destination: Destination; leaving?: ReturnContext }
  /** Changes the local view of the current place; nothing to go back to. */
  | { type: "view"; view: string }
  /** Puts another object in the current place's stead; Back still leads
   * where it did. */
  | { type: "replace"; to: Route }
  /** Returns to the place last left in this destination, or its landing. */
  | { type: "back" }
  /** Opens a project, or none: a new history. */
  | { type: "project"; projectId: string | undefined; to: Route };

export function initialRoutes(current: Route): RouteState {
  return { current, history: {}, left: {} };
}

export function routeReducer(state: RouteState, action: RouteAction): RouteState {
  const here = sidebarOf(state.current.destination);
  const leftWith = (context: ReturnContext | undefined): Route =>
    context ? { ...state.current, returnContext: context } : state.current;
  const inProject = (to: Route): Route =>
    to.projectId === undefined && state.current.projectId !== undefined ? { ...to, projectId: state.current.projectId } : to;
  switch (action.type) {
    case "go": {
      const there = sidebarOf(action.to.destination);
      if (there === here) {
        const stack = [...(state.history[here] ?? []), leftWith(action.leaving)];
        return { ...state, current: inProject(action.to), history: { ...state.history, [here]: stack } };
      }
      return {
        current: inProject(action.to),
        history: { ...state.history, [there]: [] },
        left: { ...state.left, [here]: leftWith(action.leaving) },
      };
    }
    case "destination": {
      if (action.destination === here) {
        if (state.current.destination === here && state.current.objectId === undefined) return state;
        return { ...state, current: inProject({ destination: here }), history: { ...state.history, [here]: [] } };
      }
      const to = state.left[action.destination] ?? inProject({ destination: action.destination });
      return { ...state, current: to, left: { ...state.left, [here]: leftWith(action.leaving) } };
    }
    case "view": {
      // A view change is not a return: what Back restored is not restored again.
      const { returnContext: _returned, ...here } = state.current;
      return { ...state, current: { ...here, view: action.view } };
    }
    case "replace":
      return { ...state, current: inProject(action.to) };
    case "back": {
      const stack = state.history[here] ?? [];
      const previous = stack[stack.length - 1];
      if (previous) {
        return { ...state, current: previous, history: { ...state.history, [here]: stack.slice(0, -1) } };
      }
      if (state.current.destination === here && state.current.objectId === undefined) return state;
      return { ...state, current: inProject({ destination: here }) };
    }
    case "project":
      return initialRoutes(action.projectId === undefined ? action.to : { ...action.to, projectId: action.projectId });
  }
}

/** The route's local view when it is one of the place's own views, else the
 * place's first. */
export function viewOf<K extends string>(route: Route, place: Place, views: readonly { key: K }[]): K {
  const first = views[0]!.key;
  if (route.destination !== place) return first;
  return views.find((view) => view.key === route.view)?.key ?? first;
}

/** The window's route and the ways to change it. */
export function useRoutes(initial: Route) {
  const [state, setState] = useState<RouteState>(() => initialRoutes(initial));
  const dispatch = useCallback((action: RouteAction) => setState((held) => routeReducer(held, action)), []);
  return useMemo(() => ({ route: state.current, dispatch }), [state, dispatch]);
}
