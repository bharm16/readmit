import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, useSyncExternalStore, type RefObject } from "react";
import { cancel as cancelOperation, type InterruptibleOperation, type State } from "./bindings";
import { Report, type Indicators } from "./shell";

/** The operation lifecycle every panel of this window shares.
 *
 * The facade runs one operation at a time and answers every call with exactly
 * one state. What the window does around such a call is the same wherever it
 * is made, so it is done here once: the operation holds a slot while it runs,
 * and the slot is released however the call ends — answered, refused, or
 * rejected by the boundary — so nothing can leave a panel permanently
 * disabled. An answer that no longer describes what is on screen is
 * discarded: one withdrawn meanwhile, one a later run of the same operation
 * superseded, or one that arrives after its panel is gone. A cancel names the
 * operation the facade declares for it, never a string a panel spelled, so it
 * can stop only that operation. A browser takes focus from a control it
 * disables, so a running operation hands focus to the control it still
 * offers, and once it answers focus returns to the control that started it
 * unless something else has taken it since. And an answer is drawn one way,
 * Outcome: through Status, with its state's word and shape and its reason.
 *
 * Panels use it the way editors use the retainer: one lifecycle per group of
 * controls that are unavailable together. A lifecycle created with `window`
 * holds the window's one slot, so while it runs the rest of the window is
 * unavailable rather than answered busy; the others hold only their own
 * controls, and the facade answers another panel's call busy. */

/** Every answer the facade gives carries one state and, unless it completed,
 * the reason for it. */
export interface Answer {
  state: State;
  reason?: string | undefined;
}

export interface LifecyclePolicy<K extends string> {
  /** The name the facade declares for each of these operations that a
   * cancel control stops. An operation with no name here cannot be cancelled
   * from this lifecycle. */
  names?: Partial<Record<K, InterruptibleOperation>>;
  /** The control a running operation hands focus to: the one it still
   * offers, its cancel control. */
  stops?: Partial<Record<K, RefObject<HTMLElement | null>>>;
  /** The control that starts an operation when it need not hold focus as the
   * operation starts: a form's default button, which Enter in one of the
   * form's fields presses. Focus returns there rather than to the field. */
  origins?: Partial<Record<K, RefObject<HTMLElement | null>>>;
  /** Where focus goes once an operation answers when the control that
   * started it has nothing left to do: it is gone or disabled. */
  returns?: Partial<Record<K, RefObject<HTMLElement | null>>>;
  /** Whether these operations hold the window's one slot, making the rest of
   * the window unavailable while they run. */
  window?: boolean;
  /** Whether these operations are reads the panel starts itself rather than
   * a control a person pressed: they hand focus nowhere and return it nowhere,
   * so a read finishing never moves a person's focus. */
  background?: boolean;
}

export interface Lifecycle<K extends string> {
  /** The operation of this lifecycle that holds its slot, or null. */
  running: K | null;
  /** Runs one operation holding the slot and resolves with what its work
   * answered, or with undefined when that answer is stale. Work that commits
   * an answer itself asks `current` first. */
  run: <T>(kind: K, work: (current: () => boolean) => Promise<T>) => Promise<T | undefined>;
  /** Discards the answers in flight, of one operation or of all of them:
   * what is on screen changed, so they no longer describe it. */
  withdraw: (kind?: K) => void;
  /** Asks the facade to stop the running operation, by the name it declares
   * for it; an operation without one is not stopped. */
  cancel: () => void;
}

// The window's one slot: the operations of window lifecycles holding it now.
const holders = new Set<symbol>();
const subscribers = new Set<() => void>();

function subscribe(subscriber: () => void): () => void {
  subscribers.add(subscriber);
  return () => {
    subscribers.delete(subscriber);
  };
}

function held(): boolean {
  return holders.size > 0;
}

function release(slot: symbol): void {
  if (holders.delete(slot)) {
    for (const subscriber of subscribers) subscriber();
  }
}

/** Whether an operation holds the window's one slot, so the rest of the
 * window is unavailable while it runs. */
export function useWindowBusy(): boolean {
  return useSyncExternalStore(subscribe, held, held);
}

/** The element holding focus now, if any. */
function focused(): HTMLElement | null {
  const active = document.activeElement;
  return active instanceof HTMLElement && active !== document.body ? active : null;
}

export function useLifecycle<K extends string>(policy: LifecyclePolicy<K> = {}): Lifecycle<K> {
  const [running, setRunning] = useState<K | null>(null);
  const latest = useRef(policy);
  useEffect(() => {
    latest.current = policy;
  });
  // The operations in flight, oldest first. Work that starts more work before
  // it answers holds the slot until the last of them has answered.
  const inFlight = useRef<{ kind: K; slot: symbol }[]>([]);
  // Advanced by every withdrawal and when the panel goes away.
  const withdrawals = useRef(0);
  // How many runs of each operation have started: only the latest one's
  // answer describes what is on screen.
  const turns = useRef(new Map<K, number>());
  // The control that started the operation holding the slot, and where focus
  // was then and was handed while it ran: focus found in one of those
  // unmoved places when it answers is not somewhere a person moved it since.
  const origin = useRef<{ element: HTMLElement | null; kind: K; unmoved: Set<HTMLElement> } | null>(null);

  const run = useCallback(async <T,>(kind: K, work: (current: () => boolean) => Promise<T>): Promise<T | undefined> => {
    const slot = Symbol(kind);
    if (inFlight.current.length === 0 && !latest.current.background) {
      const active = focused();
      origin.current = {
        element: latest.current.origins?.[kind]?.current ?? active,
        kind,
        unmoved: new Set(active ? [active] : []),
      };
    }
    inFlight.current = [...inFlight.current, { kind, slot }];
    const since = withdrawals.current;
    const turn = (turns.current.get(kind) ?? 0) + 1;
    turns.current.set(kind, turn);
    if (latest.current.window) {
      holders.add(slot);
      for (const subscriber of subscribers) subscriber();
    }
    setRunning(kind);
    const current = () => withdrawals.current === since && turns.current.get(kind) === turn;
    try {
      const answer = await work(current);
      return current() ? answer : undefined;
    } finally {
      inFlight.current = inFlight.current.filter((entry) => entry.slot !== slot);
      release(slot);
      setRunning(inFlight.current[inFlight.current.length - 1]?.kind ?? null);
    }
  }, []);

  const withdraw = useCallback((kind?: K) => {
    if (kind === undefined) {
      withdrawals.current += 1;
    } else {
      turns.current.set(kind, (turns.current.get(kind) ?? 0) + 1);
    }
  }, []);

  const cancel = useCallback(() => {
    const current = inFlight.current[inFlight.current.length - 1];
    const name = current ? latest.current.names?.[current.kind] : undefined;
    if (name) {
      cancelOperation(name);
    }
  }, []);

  // A running operation hands focus to the control it still offers; once the
  // slot is released, focus returns to the control that started it, or where
  // the policy sends it when that control has nothing left to do — unless
  // something else took focus meanwhile, which then keeps it.
  useEffect(() => {
    const { stops, returns } = latest.current;
    if (running !== null) {
      const stop = stops?.[running]?.current;
      if (stop) {
        stop.focus();
        origin.current?.unmoved.add(stop);
      }
      return;
    }
    const started = origin.current;
    origin.current = null;
    const active = focused();
    if (!started || (active !== null && !active.matches(":disabled") && !started.unmoved.has(active))) {
      return;
    }
    const element = started.element;
    if (element?.isConnected && !element.matches(":disabled")) {
      element.focus();
    } else {
      returns?.[started.kind]?.current?.focus();
    }
  }, [running]);

  // A panel that is gone releases the window's slot it held, even for a call
  // that never answers, and no answer arriving after it describes anything.
  // A panel mounted again, as development mode does once, holds the slot
  // again for whatever it still has in flight.
  useEffect(() => {
    if (latest.current.window && inFlight.current.length > 0) {
      for (const entry of inFlight.current) {
        holders.add(entry.slot);
      }
      for (const subscriber of subscribers) subscriber();
    }
    return () => {
      withdrawals.current += 1;
      for (const entry of inFlight.current) {
        release(entry.slot);
      }
    };
  }, []);

  return useMemo(() => ({ running, run, withdraw, cancel }), [running, run, withdraw, cancel]);
}

/** The indicators the facade describes for every status, which the window
 * provides to every panel below it. A panel drawn without them reads each
 * state as its plain word. */
export const IndicatorsContext = createContext<Indicators>(new Map());

/** One operation's answer, drawn the way every state is drawn: through
 * Status, with its word, its shape and its reason, whichever of the six
 * states it is. While the operation runs, that is the whole story. */
export function Outcome({ result, progress = null }: { result: Answer | null; progress?: string | null }) {
  const indicators = useContext(IndicatorsContext);
  return <Report indicators={indicators} progress={progress} result={result} />;
}
