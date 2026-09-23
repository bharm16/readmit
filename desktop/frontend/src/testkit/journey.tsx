// The journey harness: the production App tree, mounted the way main.tsx
// mounts it, driven by real user events against the real internal/desktop
// facade over real files in a temporary root.
//
// Nothing here answers a facade call. Each call the window makes crosses to a
// journeybridge process (desktop/journeybridge) the way Wails carries it: the
// arguments are serialized with JSON.stringify, decoded by encoding/json into
// the Go method's own parameter types, and the result comes back as the JSON
// Wails would marshal. Where Wails would leave a call unanswered — a name
// nothing is bound under, a method that panics — the bridge rejects it. What
// the harness does supply is what the native shell supplies and a person does
// outside the window: answers to the host's folder and file dialogs, their own
// files already on disk, and an activation folder their vendor delivered.
// Closing the window ends the process; reopening starts a new one over the same
// root, so whatever survives a reopen survived on disk.
//
// A journey fails at close for anything that would otherwise pass silently: a
// call Wails would have rejected (the facade returns typed results and never an
// error, so every rejection is a broken binding), a dialog nobody scripted or
// answered differently than scripted, and a scripted answer nothing consumed.
import { StrictMode } from "react";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import type { UserEvent } from "@testing-library/user-event";
import { inject } from "vitest";
import App from "../App";
import type { Facade } from "../bindings";
import { startDownstream } from "./downstream.js";
import type { Downstream, DownstreamMode } from "./downstream.js";
import {
  backdateInRoot,
  createRoot,
  makeFolderInRoot,
  pathInRoot,
  provisionInRoot,
  removeRoot,
  runCommandLine,
  startBridge,
  writeInRoot,
} from "./bridge-process.js";
import type { BridgeExit, BridgeProcess, CommandLineRun } from "./bridge-process.js";

declare module "vitest" {
  export interface ProvidedContext {
    /** The journeybridge executable the global setup built for this run. */
    journeyBridge: string;
    /** The readmit command line the global setup built for this run. */
    journeyCommandLine: string;
  }
}

/** One call the window made across the seam, and how Wails would have
 * settled it. */
export interface JourneyCall {
  method: string;
  args: unknown[];
  /** Whether the call had settled when the application last ended. */
  settled: boolean;
  /** Set when the application ended while the call was still running. */
  abandoned?: boolean;
  /** What the facade answered, once it answered. */
  result?: unknown;
  rejected?: string;
}

/** The two host dialogs the facade opens. */
type DialogKind = "folder" | "files";

interface DialogReport {
  shown: { kind: DialogKind; title: string; problem?: string }[];
  unanswered: number;
}

/** One named region of the window: the facade declares them, and every
 * journey step happens inside one. */
export function region(name: string): HTMLElement {
  return screen.getByRole("region", { name });
}

/** Waits until a control the window disabled while it worked is enabled
 * again, and returns it. A person cannot press a disabled button, and neither
 * can a journey: user events on one do nothing, so a step that follows real
 * work waits for the window to offer it. */
export async function whenEnabled<E extends HTMLElement>(element: E): Promise<E> {
  await waitFor(() => {
    if ((element as unknown as { disabled?: boolean }).disabled) {
      throw new Error("the control is still disabled");
    }
  });
  return element;
}

/** Presses a button the way a person does: once the window offers it. */
export async function press(user: UserEvent, element: HTMLElement): Promise<void> {
  await user.click(await whenEnabled(element));
}

/** A text matcher for a line the window composes from several elements —
 * "Run: " and its status, a label and its value: the innermost element whose
 * whole text matches. */
export function byContent(pattern: RegExp): (content: string, element: Element | null) => boolean {
  return (_content, element) =>
    element !== null &&
    pattern.test(element.textContent ?? "") &&
    !Array.from(element.children).some((child) => pattern.test(child.textContent ?? ""));
}

/** Replaces what a field holds with text, typed the way a person types it,
 * once the window offers the field: a disabled field takes no keystrokes. */
export async function enter(user: UserEvent, field: HTMLElement, text: string): Promise<void> {
  await user.clear(await whenEnabled(field));
  if (text !== "") await user.type(field, text);
}

/** How long a closing window waits for its calls to settle, and how long it
 * must then stay quiet: a person closes the window after it has answered. */
const SETTLE_MS = 30_000;
const QUIET_MS = 200;

export class Journey {
  /** The journey's temporary root: this person's machine, for the journey. */
  readonly root: string;
  /** Every call the window made, oldest first, across every launch. */
  readonly calls: JourneyCall[] = [];
  private bridge: BridgeProcess | null = null;
  private readonly downstreams: Downstream[] = [];
  private readonly problems: string[] = [];
  private readonly binary: string;
  /** How many calls an earlier close has already checked. */
  private checked = 0;

  private constructor(binary: string, root: string) {
    this.binary = binary;
    this.root = root;
  }

  /** A new journey over an empty temporary root. Nothing is running yet. */
  static create(): Journey {
    return new Journey(inject("journeyBridge"), createRoot());
  }

  /** An absolute path inside the journey's root. */
  path(...parts: string[]): string {
    return [this.root, ...parts].join("/");
  }

  /** Starts the application and mounts its window, then waits for the window
   * to draw its regions from the facade's own shell description. */
  async launch(): Promise<void> {
    if (this.bridge) {
      throw new Error("the application is already running");
    }
    this.bridge = startBridge(this.binary, this.root);
    window.go = { desktop: { App: this.facade(this.bridge) } };
    render(
      <StrictMode>
        <App />
      </StrictMode>,
    );
    await screen.findByRole("region", { name: "Evidence" });
  }

  /** Closes the window and ends the application the way a person closing it
   * does: once the window has finished what it was doing. Every call has
   * settled and none has followed for a moment before the window goes; a call
   * still running after the bound fails the journey, which ends abruptly only
   * through crash(). Throws for anything the journey let pass that should have
   * failed it. */
  async close(): Promise<BridgeExit> {
    await this.settled();
    const exit = await this.end((bridge) => bridge.close());
    this.raise();
    return exit;
  }

  /** Ends the application at once, as a crash or a forced quit does. Calls
   * still in flight are abandoned with it. */
  crash(): Promise<BridgeExit> {
    return this.end((bridge) => bridge.kill());
  }

  /** Ends the journey: closes a running application and removes the root.
   * The root is removed even when closing reports a problem. */
  async dispose(): Promise<void> {
    try {
      if (this.bridge) {
        await this.close();
      } else {
        this.raise();
      }
    } finally {
      // A close that failed leaves the process running; nothing outlives the
      // journey that started it.
      if (this.bridge) {
        await this.end((bridge) => bridge.kill());
      }
      for (const downstream of this.downstreams.splice(0)) {
        await downstream.close();
      }
      removeRoot(this.root);
    }
  }

  /** The person picks this folder in the next folder dialog the application
   * opens. A title, when given, is the dialog that must be the one asking. */
  chooseFolder(folder: string, title?: string): Promise<void> {
    return this.script("folder", [folder], title);
  }

  /** The person picks these files in the next file dialog. */
  chooseFiles(files: string[], title?: string): Promise<void> {
    return this.script("files", files, title);
  }

  /** The person dismisses the next dialog of this kind without choosing. */
  dismissDialog(kind: DialogKind, title?: string): Promise<void> {
    return this.script(kind, [], title);
  }

  /** Places a file on this person's machine before the application reads it:
   * their own evidence, exported from somewhere the application never saw. */
  writeFile(relative: string, content: string | Uint8Array): string {
    return writeInRoot(this.root, relative, content);
  }

  /** Creates an empty folder on this person's machine, such as the one they
   * will choose to keep a new project in. */
  makeFolder(relative: string): string {
    return makeFolderInRoot(this.root, relative);
  }

  /** Provisions the activation folder a vendor delivers — a newly signed test
   * entitlement, its trust store and an activated operation policy — at a
   * folder inside the root, and returns that folder. Choosing it in the
   * window is still the person's step. */
  provisionLicense(relative: string): string {
    return provisionInRoot(this.binary, this.root, relative);
  }

  /** Starts the independent downstream scheduling system a person's
   * interface feeds (testkit/downstream.js), on a loopback port, writing its
   * ledger export to a file inside the root. It stops when the journey ends. */
  async startDownstream(exportFile: string, mode?: DownstreamMode): Promise<Downstream> {
    const downstream = await startDownstream({ exportPath: pathInRoot(this.root, exportFile), ...(mode ? { mode } : {}) });
    this.downstreams.push(downstream);
    return downstream;
  }

  /** Makes a file on this person's machine look as though nothing rewrote it
   * for this long. */
  backdate(relative: string, milliseconds: number): string {
    return backdateInRoot(this.root, relative, milliseconds);
  }

  /** Runs the readmit command line over this journey's root, as a person
   * or an automation job would run it beside the application: the same
   * files, read by the command line's own entry point. */
  commandLine(args: string[]): Promise<CommandLineRun> {
    return runCommandLine(inject("journeyCommandLine"), this.root, args);
  }

  /** The calls of one facade method, oldest first. */
  callsTo(method: keyof Facade): JourneyCall[] {
    return this.calls.filter((call) => call.method === method);
  }

  /** Waits until every call the window made has settled and none has
   * followed for QUIET_MS, or fails naming the calls still running. */
  private async settled(): Promise<void> {
    const deadline = Date.now() + SETTLE_MS;
    let quietSince = Date.now();
    for (;;) {
      const running = this.calls.filter((call) => !call.settled);
      if (running.length > 0) {
        quietSince = Date.now();
        if (Date.now() > deadline) {
          throw new Error(
            `the window was closed while ${running.map((call) => call.method).join(", ")} still ran; a journey that means to interrupt it uses crash()`,
          );
        }
      } else if (Date.now() - quietSince >= QUIET_MS) {
        return;
      }
      await new Promise((resolve) => setTimeout(resolve, 20));
    }
  }

  private async script(dialog: DialogKind, paths: string[], title?: string): Promise<void> {
    await this.control({ op: "dialog", dialog, paths, ...(title ? { title } : {}) });
  }

  /** Takes the window down and ends the process one way or the other, then
   * records what the journey let pass. The window goes the way a closing
   * webview goes: the seam is removed before React unmounts, so the page
   * cannot call the application again, and whatever was still running is
   * abandoned. */
  private async end(how: (bridge: BridgeProcess) => Promise<BridgeExit>): Promise<BridgeExit> {
    const bridge = this.running();
    const report = (await this.control({ op: "dialogs" })) as DialogReport;
    delete window.go;
    cleanup();
    for (const call of this.calls) {
      if (!call.settled) call.abandoned = true;
    }
    this.bridge = null;
    const exit = await how(bridge);
    this.collect(bridge, report);
    return exit;
  }

  private running(): BridgeProcess {
    if (!this.bridge) {
      throw new Error("the application is not running");
    }
    return this.bridge;
  }

  private async control(message: Record<string, unknown>): Promise<unknown> {
    const reply = await this.running().send(message);
    if (reply.error !== null && reply.error !== undefined) {
      throw new Error(String(reply.error));
    }
    return reply.result;
  }

  /** The facade as Wails publishes it: every method name resolves to a call
   * that serializes its arguments, crosses to the bridge and settles with the
   * Go method's own result, or rejects with Wails' error. */
  private facade(bridge: BridgeProcess): Facade {
    const journey = this;
    return new Proxy(
      {},
      {
        get(_target, method) {
          if (typeof method !== "string" || method === "then") {
            return undefined;
          }
          return async (...args: unknown[]) => {
            const serialized = JSON.parse(JSON.stringify(args)) as unknown[];
            const call: JourneyCall = { method, args: serialized, settled: false };
            journey.calls.push(call);
            const reply = await bridge.send({
              op: "call",
              name: ["desktop", "App", method].join("."),
              args: serialized,
            });
            call.settled = true;
            // Wails rejects a call whose callback carries a truthy error.
            if (reply.error) {
              call.rejected = String(reply.error);
              throw new Error(call.rejected);
            }
            call.result = reply.result;
            return reply.result;
          };
        },
      },
    ) as Facade;
  }

  private collect(bridge: BridgeProcess, report: DialogReport): void {
    // Calls still in flight when the process ends are abandoned with it and
    // are not a broken binding; every call that settled is checked once.
    for (const call of this.calls.slice(this.checked)) {
      if (call.rejected && !call.abandoned) {
        this.problems.push(`${call.method} was rejected: ${call.rejected}`);
      }
    }
    this.checked = this.calls.length;
    for (const dialog of report.shown) {
      if (dialog.problem) {
        this.problems.push(`${dialog.kind} dialog "${dialog.title}": ${dialog.problem}`);
      }
    }
    if (report.unanswered > 0) {
      this.problems.push(`${report.unanswered} scripted dialog answer(s) were never used`);
    }
    // What the process said is the context a failure needs, a panic above all.
    const said = bridge.stderr().trim();
    if (this.problems.length > 0 && said) {
      this.problems.push(`the application wrote: ${said}`);
    }
  }

  private raise(): void {
    if (this.problems.length > 0) {
      const problems = this.problems.splice(0);
      throw new Error(`the journey let something pass that should have failed it:\n- ${problems.join("\n- ")}`);
    }
  }
}
