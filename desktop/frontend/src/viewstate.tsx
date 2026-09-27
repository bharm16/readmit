// What a page holds that is not saved anywhere: the values a person typed or
// chose in a panel, and the answers it is showing. They are kept here, outside
// the components, so a page that is not on screen is not mounted and returning
// to it shows what it had. Nothing here is written to disk; opening another
// project forgets all of it.
import { createContext, useCallback, useContext, useEffect, useRef, useSyncExternalStore, type Dispatch, type ReactNode, type SetStateAction } from "react";

const values = new Map<string, unknown>();
const listeners = new Map<string, Set<() => void>>();

function notify(key: string) {
  for (const listener of listeners.get(key) ?? []) listener();
}

/** The page a component is drawn in, which scopes its kept values. */
export const ViewScope = createContext("");

/** Forgets every kept value: another project was opened, or none. */
export function forgetViewState() {
  const keys = [...values.keys()];
  values.clear();
  for (const key of keys) notify(key);
}

/** useState whose value outlives the component: the next component of the
 * same page asking for the same key starts from it. `key` names the value
 * within its page; include the object it belongs to when the page shows one
 * of several. */
export function useViewState<T>(key: string, initial: T | (() => T)): [T, Dispatch<SetStateAction<T>>] {
  const scope = useContext(ViewScope);
  const held = `${scope}\u0000${key}`;
  if (!values.has(held)) values.set(held, typeof initial === "function" ? (initial as () => T)() : initial);
  const subscribe = useCallback(
    (listener: () => void) => {
      const set = listeners.get(held) ?? new Set();
      set.add(listener);
      listeners.set(held, set);
      return () => {
        set.delete(listener);
        if (set.size === 0) listeners.delete(held);
      };
    },
    [held],
  );
  const read = useCallback(() => values.get(held) as T, [held]);
  const value = useSyncExternalStore(subscribe, read, read);
  const set = useCallback<Dispatch<SetStateAction<T>>>(
    (next) => {
      const current = values.get(held) as T;
      const updated = typeof next === "function" ? (next as (previous: T) => T)(current) : next;
      if (Object.is(updated, current)) return;
      values.set(held, updated);
      notify(held);
    },
    [held],
  );
  return [value, set];
}

/** Calls `changed` when `value` differs from the one the page last saw under
 * `key`, and not merely because the page was mounted again: what a panel
 * keeps for one object is cleared when another object takes its place. */
export function useViewChange<T>(key: string, value: T, changed: () => void) {
  const [seen, setSeen] = useViewState<T>(key, () => value);
  const latest = useRef(changed);
  latest.current = changed;
  useEffect(() => {
    if (Object.is(seen, value)) return;
    setSeen(() => value);
    latest.current();
  }, [seen, setSeen, value]);
}

/** What `key` does for a component's own state, for its kept values: the
 * values under a new `id` start fresh, and returning to an earlier `id`
 * finds its own. */
export function ViewKey({ id, children }: { id: string; children: ReactNode }) {
  const scope = useContext(ViewScope);
  return <ViewScope.Provider value={`${scope}/${id}`}>{children}</ViewScope.Provider>;
}
