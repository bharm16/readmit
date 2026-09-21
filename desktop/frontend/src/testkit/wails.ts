// The behavior-test boundary over the typed Wails seam.
//
// Every frontend test drives the real components against a stub installed at
// window.go.desktop.App — the exact surface the production bindings read — so
// a test can mock only the calls the real facade publishes, with the real
// parameter and result types. Nothing here decides anything about evidence:
// fixtures carry positions, states and counts the engine could have reported,
// and never an HL7 value, a credential, a machine path or a network address.
// Domain meaning stays on the Go side of the seam, where the shared readers
// and evaluators are tested.
//
// This module is the reusable entry the capability ledger's frontend workflows
// go through: a new workflow gets an interaction test that installs its
// handlers here and drives real user events, and its shared-operation parity
// evidence comes from the Go facade tests, not from a fixture that repeats the
// engine's answer.
import type { Facade } from "../bindings";

/** Exactly one call the window made across the seam, in the order it happened. */
export interface StubCall {
  method: string;
  args: unknown[];
}

type Awaitable<T> = T | Promise<T>;

/** A handler must have the real method's parameter types and answer with its
 * real result type, so a facade change that a test does not follow fails the
 * type check instead of quietly mocking a call that no longer exists. */
type HandlerOf<M extends keyof Facade> = Facade[M] extends (
  ...args: infer A
) => Promise<infer R>
  ? (...args: A) => Awaitable<R>
  : never;

export type FacadeHandlers = {
  [M in keyof Facade]?: HandlerOf<M>;
};

/** The error an unanswered facade method raises. The production bindings turn
 * any rejection into a fixed failed sentence, so a test that sees that
 * sentence without having arranged it forgot a handler; a test that wants the
 * unreachable answer arranges the rejection itself. */
export class UnhandledMethod extends Error {
  constructor(method: string) {
    super(`the test stub has no answer for ${method}`);
  }
}

/** Calls parked before they are answered, for tests that need to decide the
 * order two racing operations settle in: a stale response, a cancellation
 * arriving while a read is in flight, a write that lands after a newer one. */
export class Parked {
  private readonly settle: ((value: unknown) => void)[] = [];

  /** Records an arriving call and holds it until the test resolves it. The
   * stub itself keeps every call's arguments, so only the answer waits here. */
  arrive(): Promise<unknown> {
    return new Promise((resolve) => {
      this.settle.push(resolve);
    });
  }

  /** Resolves the oldest unanswered call with the given result. */
  resolve(result: unknown): void {
    const next = this.settle.shift();
    if (!next) {
      throw new Error("no parked call to resolve");
    }
    next(result);
  }

  /** Rejects the oldest unanswered call, which the bindings report the same
   * way they report an unreachable application. */
  reject(): void {
    this.resolve(Promise.reject(new UnhandledMethod("parked call rejected by the test")));
  }

  /** How many calls arrived and are not answered yet. */
  get size(): number {
    return this.settle.length;
  }
}

/** Records every call the window makes and answers each one from the handlers
 * the test installed. A method with no handler rejects, so a test cannot pass
 * because a call it never arranged silently answered with nothing. */
export class FacadeStub {
  readonly calls: StubCall[] = [];
  private handlers: FacadeHandlers = {};

  /** Replaces or adds answers between phases of one test. */
  reply(handlers: FacadeHandlers): void {
    this.handlers = { ...this.handlers, ...handlers };
  }

  /** Parks a method so its calls are recorded but answered only when the test
   * resolves them, oldest first. */
  park<M extends keyof Facade>(method: M): Parked {
    const parked = new Parked();
    this.reply({ [method]: () => parked.arrive() } as FacadeHandlers);
    return parked;
  }

  /** Every recorded call of one method, oldest first. */
  callsTo<M extends keyof Facade>(method: M): StubCall[] {
    return this.calls.filter((call) => call.method === method);
  }

  /** The arguments of the one call of a method a test arranged, failing the
   * test when there were none or more than one. */
  oneCall<M extends keyof Facade>(method: M): Parameters<Facade[M]> {
    const matching = this.callsTo(method);
    const only = matching[0];
    if (matching.length !== 1 || !only) {
      throw new Error(`expected exactly one ${method} call, saw ${matching.length}`);
    }
    return only.args as Parameters<Facade[M]>;
  }

  /** The stub as the bindings see it: every method records, then answers. */
  asFacade(): Facade {
    const stub = this;
    return new Proxy(
      {},
      {
        get(_target, method) {
          if (typeof method !== "string") {
            return undefined;
          }
          return (...args: unknown[]) => {
            stub.calls.push({ method, args });
            const handler = stub.handlers[method as keyof Facade] as
              | ((...a: unknown[]) => unknown)
              | undefined;
            if (!handler) {
              return Promise.reject(new UnhandledMethod(method));
            }
            try {
              return Promise.resolve(handler(...args));
            } catch (failure) {
              return Promise.reject(failure);
            }
          };
        },
      },
    ) as Facade;
  }
}

let installed: FacadeStub | null = null;

/** Installs a stub at the exact namespace the production bindings read. The
 * setup file removes it after every test, so no answer from one test is
 * visible to the next. */
export function installFacade(handlers: FacadeHandlers = {}): FacadeStub {
  const stub = new FacadeStub();
  stub.reply(handlers);
  window.go = { desktop: { App: stub.asFacade() } };
  installed = stub;
  return stub;
}

/** Removes the installed stub. The setup file calls this after every test. */
export function uninstallFacade(): void {
  delete window.go;
  installed = null;
}

/** The stub installFacade installed most recently, for assertions. */
export function facadeStub(): FacadeStub {
  if (!installed) {
    throw new Error("no facade stub is installed");
  }
  return installed;
}
