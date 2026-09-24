// The operation lifecycle every panel shares, driven through small panels that
// use it the way the window's panels do, against calls parked at the facade so
// each test decides when, and in which order, the answers arrive.
import { useRef, useState, type RefObject } from "react";
import { expect, test } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { explainRun, type RunExplanationRequest, type RunExplanationResult, type State } from "./bindings";
import { IndicatorsContext, Outcome, useLifecycle, useWindowBusy, type Answer, type LifecyclePolicy } from "./lifecycle";
import { installFacade } from "./testkit/wails";
import { WORKSPACE_ROOT, indicatorTable } from "./testkit/fixtures";

const request: RunExplanationRequest = { workspace: WORKSPACE_ROOT, run: "run-1", assertions: "set.json", reveal: false };
const answered = (reason: string): RunExplanationResult => ({ state: "failed", reason });

type Kind = "explaining" | "choosing";

/** A panel with two operations, the answer it last kept, and the window's slot
 * as the rest of the window sees it. */
function Panel({ policy = {}, stop }: { policy?: LifecyclePolicy<Kind>; stop?: RefObject<HTMLButtonElement | null> }) {
  const op = useLifecycle<Kind>(policy);
  const windowBusy = useWindowBusy();
  const [kept, setKept] = useState("nothing");
  const start = (which: Kind) =>
    void op
      .run(which, () => explainRun(request))
      .then((answer) => {
        if (answer) setKept(answer.reason ?? "");
      });
  return (
    <>
      <button type="button" disabled={op.running !== null} onClick={() => start("explaining")}>
        Start
      </button>
      <button type="button" onClick={() => start("explaining")}>
        Start again
      </button>
      <button type="button" onClick={() => start("choosing")}>
        Start another
      </button>
      <button type="button" onClick={() => op.withdraw()}>
        Withdraw
      </button>
      <button type="button" ref={stop} disabled={op.running === null} onClick={op.cancel}>
        Stop
      </button>
      <p>running: {op.running ?? "none"}</p>
      <p>kept: {kept}</p>
      <p>window: {windowBusy ? "unavailable" : "available"}</p>
    </>
  );
}

test("an operation holds its slot until it answers and releases it however it ends", async () => {
  const user = userEvent.setup();
  const facade = installFacade({});
  const parked = facade.park("ExplainRun");
  render(<Panel />);
  await user.click(screen.getByRole("button", { name: "Start" }));
  expect(await screen.findByText("running: explaining")).toBeTruthy();
  expect((screen.getByRole("button", { name: "Start" }) as HTMLButtonElement).disabled).toBe(true);
  parked.resolve(answered("the first answer"));
  expect(await screen.findByText("running: none")).toBeTruthy();
  expect(screen.getByText("kept: the first answer")).toBeTruthy();

  // A call the boundary rejects is still a released slot: the bindings turn it
  // into a failed answer, and the controls are available again.
  await user.click(screen.getByRole("button", { name: "Start" }));
  expect(await screen.findByText("running: explaining")).toBeTruthy();
  parked.reject();
  expect(await screen.findByText("running: none")).toBeTruthy();
  expect(screen.getByText("kept: the application did not answer")).toBeTruthy();
  expect((screen.getByRole("button", { name: "Start" }) as HTMLButtonElement).disabled).toBe(false);
});

test("work that throws still releases the slot, and the rejection reaches its caller", async () => {
  const user = userEvent.setup();
  installFacade({});
  const failures: unknown[] = [];
  function Throwing() {
    const op = useLifecycle<"reading">({ window: true });
    const windowBusy = useWindowBusy();
    return (
      <>
        <button
          type="button"
          onClick={() =>
            void op
              .run("reading", async () => {
                throw new Error("unexpected");
              })
              .catch((failure: unknown) => failures.push(failure))
          }
        >
          Read
        </button>
        <p>running: {op.running ?? "none"}</p>
        <p>window: {windowBusy ? "unavailable" : "available"}</p>
      </>
    );
  }
  render(<Throwing />);
  await user.click(screen.getByRole("button", { name: "Read" }));
  await waitFor(() => expect(failures).toHaveLength(1));
  expect(screen.getByText("running: none")).toBeTruthy();
  expect(screen.getByText("window: available")).toBeTruthy();
});

test("a window operation holds the window's one slot, and a panel operation holds only its own controls", async () => {
  const user = userEvent.setup();
  const facade = installFacade({});
  const parked = facade.park("ExplainRun");
  const { unmount } = render(
    <>
      <section aria-label="window">
        <Panel policy={{ window: true }} />
      </section>
      <section aria-label="panel">
        <Panel />
      </section>
    </>,
  );
  const windowPanel = within(screen.getByRole("region", { name: "window" }));
  const panel = within(screen.getByRole("region", { name: "panel" }));

  await user.click(panel.getByRole("button", { name: "Start" }));
  expect(await panel.findByText("running: explaining")).toBeTruthy();
  expect(windowPanel.getByText("window: available")).toBeTruthy();
  parked.resolve(answered("panel"));
  expect(await panel.findByText("running: none")).toBeTruthy();

  await user.click(windowPanel.getByRole("button", { name: "Start" }));
  expect(await panel.findByText("window: unavailable")).toBeTruthy();
  parked.resolve(answered("window"));
  expect(await panel.findByText("window: available")).toBeTruthy();

  // A window operation whose panel goes away releases the slot with it.
  await user.click(windowPanel.getByRole("button", { name: "Start" }));
  expect(await panel.findByText("window: unavailable")).toBeTruthy();
  unmount();
  render(<Panel />);
  expect(screen.getByText("window: available")).toBeTruthy();
  parked.resolve(answered("after the panel went away"));
});

test("work that starts more work holds the slot until the last of it has answered", async () => {
  const user = userEvent.setup();
  const facade = installFacade({});
  const parked = facade.park("ExplainRun");
  render(<Panel />);
  await user.click(screen.getByRole("button", { name: "Start" }));
  await user.click(screen.getByRole("button", { name: "Start another" }));
  expect(await screen.findByText("running: choosing")).toBeTruthy();
  parked.resolve(answered("first"));
  expect(await screen.findByText("kept: first")).toBeTruthy();
  expect(screen.getByText("running: choosing")).toBeTruthy();
  parked.resolve(answered("second"));
  expect(await screen.findByText("running: none")).toBeTruthy();
  expect(screen.getByText("kept: second")).toBeTruthy();
});

test("an answer that no longer describes the screen is discarded", async () => {
  const user = userEvent.setup();
  const facade = installFacade({});
  const parked = facade.park("ExplainRun");
  const { unmount } = render(<Panel />);

  // Withdrawn while it was in flight.
  await user.click(screen.getByRole("button", { name: "Start" }));
  await user.click(screen.getByRole("button", { name: "Withdraw" }));
  parked.resolve(answered("withdrawn"));
  expect(await screen.findByText("running: none")).toBeTruthy();
  expect(screen.getByText("kept: nothing")).toBeTruthy();

  // Superseded by a later run of the same operation, whichever answers first;
  // a run of another operation supersedes nothing.
  await user.click(screen.getByRole("button", { name: "Start" }));
  await user.click(screen.getByRole("button", { name: "Start again" }));
  await user.click(screen.getByRole("button", { name: "Start another" }));
  parked.resolve(answered("superseded"));
  parked.resolve(answered("latest"));
  expect(await screen.findByText("kept: latest")).toBeTruthy();
  parked.resolve(answered("another operation"));
  expect(await screen.findByText("kept: another operation")).toBeTruthy();
  expect(await screen.findByText("running: none")).toBeTruthy();

  // Arriving after its panel is gone.
  await user.click(screen.getByRole("button", { name: "Start" }));
  expect(parked.size).toBe(1);
  unmount();
  parked.resolve(answered("too late"));
  await new Promise((resolve) => setTimeout(resolve, 0));
  expect(screen.queryByText("kept: too late")).toBeNull();
});

test("a cancel names the operation the facade declares for what is running, and nothing else", async () => {
  const user = userEvent.setup();
  const cancels: string[] = [];
  const facade = installFacade({ Cancel: async (name) => void cancels.push(name) });
  const parked = facade.park("ExplainRun");
  render(<Panel policy={{ names: { explaining: "run-explanation" } }} />);
  await user.click(screen.getByRole("button", { name: "Start" }));
  await user.click(await screen.findByRole("button", { name: "Stop" }));
  expect(cancels).toEqual(["run-explanation"]);
  parked.resolve({ state: "cancelled", reason: "stopped" });
  expect(await screen.findByText("kept: stopped")).toBeTruthy();

  // An operation with no declared name is not interruptible from here.
  await user.click(screen.getByRole("button", { name: "Start another" }));
  await user.click(await screen.findByRole("button", { name: "Stop" }));
  expect(cancels).toEqual(["run-explanation"]);
  parked.resolve(answered("ran to completion"));
  expect(await screen.findByText("running: none")).toBeTruthy();
});

test("focus goes to the control a running operation offers and returns to the one that started it", async () => {
  const user = userEvent.setup();
  const facade = installFacade({});
  const parked = facade.park("ExplainRun");
  function Focused() {
    const stop = useRef<HTMLButtonElement>(null);
    return <Panel stop={stop} policy={{ stops: { explaining: stop } }} />;
  }
  render(<Focused />);
  const start = screen.getByRole("button", { name: "Start" });
  await user.click(start);
  await waitFor(() => expect(document.activeElement).toBe(screen.getByRole("button", { name: "Stop" })));
  parked.resolve(answered("answered"));
  await waitFor(() => expect(document.activeElement).toBe(start));

  // Focus a person moved elsewhere while it ran stays where they put it.
  await user.click(start);
  const elsewhere = screen.getByRole("button", { name: "Withdraw" });
  elsewhere.focus();
  parked.resolve(answered("answered again"));
  expect(await screen.findByText("running: none")).toBeTruthy();
  expect(document.activeElement).toBe(elsewhere);
});

test("when the control that started an operation has nothing left to do, focus goes where the policy sends it", async () => {
  const user = userEvent.setup();
  const facade = installFacade({});
  const parked = facade.park("ExplainRun");
  function Once() {
    const next = useRef<HTMLButtonElement>(null);
    const op = useLifecycle<"generating">({ returns: { generating: next } });
    const [used, setUsed] = useState(false);
    return (
      <>
        <button
          type="button"
          disabled={used || op.running !== null}
          onClick={() =>
            void op.run("generating", async () => {
              const answer = await explainRun(request);
              // The folder was used, whatever the answer: this control has
              // nothing left to do.
              setUsed(true);
              return answer;
            })
          }
        >
          Generate
        </button>
        <button type="button" ref={next}>
          Choose the next folder
        </button>
      </>
    );
  }
  render(<Once />);
  await user.click(screen.getByRole("button", { name: "Generate" }));
  parked.resolve(answered("generated"));
  await waitFor(() => expect(document.activeElement).toBe(screen.getByRole("button", { name: "Choose the next folder" })));
});

test("a form's operation returns focus to the form's own button, whichever field Enter was pressed in", async () => {
  const user = userEvent.setup();
  const facade = installFacade({});
  const parked = facade.park("ExplainRun");
  function Form() {
    const submit = useRef<HTMLButtonElement>(null);
    const op = useLifecycle<"opening">({ origins: { opening: submit } });
    return (
      <form
        onSubmit={(event) => {
          event.preventDefault();
          void op.run("opening", () => explainRun(request));
        }}
      >
        <label>
          Entry
          <input disabled={op.running !== null} />
        </label>
        <button type="submit" ref={submit} disabled={op.running !== null}>
          Open
        </button>
      </form>
    );
  }
  render(<Form />);
  await user.click(screen.getByLabelText("Entry"));
  await user.keyboard("{Enter}");
  expect(parked.size).toBe(1);
  parked.resolve(answered("opened"));
  await waitFor(() => expect(document.activeElement).toBe(screen.getByRole("button", { name: "Open" })));
});

test("a read the panel starts itself never moves a person's focus", async () => {
  const user = userEvent.setup();
  const facade = installFacade({});
  const parked = facade.park("ExplainRun");
  function Reading() {
    const reads = useLifecycle<"reading">({ background: true });
    return (
      <>
        <button type="button" onClick={() => void reads.run("reading", () => explainRun(request))}>
          Read
        </button>
        <button type="button">Elsewhere</button>
        <p>running: {reads.running ?? "none"}</p>
      </>
    );
  }
  render(<Reading />);
  const read = screen.getByRole("button", { name: "Read" });
  await user.click(read);
  // Focus falls away while the read runs, as it does when a disabled form
  // takes it; the read's answer puts it nowhere.
  read.blur();
  parked.resolve(answered("read"));
  expect(await screen.findByText("running: none")).toBeTruthy();
  expect(document.activeElement).toBe(document.body);
});

test("withdrawing one operation discards only its answers", async () => {
  const user = userEvent.setup();
  const facade = installFacade({});
  const parked = facade.park("ExplainRun");
  function Two() {
    const op = useLifecycle<Kind>();
    const [kept, setKept] = useState<string[]>([]);
    const start = (which: Kind) =>
      void op.run(which, () => explainRun(request)).then((answer) => {
        if (answer) setKept((held) => [...held, answer.reason ?? ""]);
      });
    return (
      <>
        <button type="button" onClick={() => start("explaining")}>
          Explain
        </button>
        <button type="button" onClick={() => start("choosing")}>
          Choose
        </button>
        <button type="button" onClick={() => op.withdraw("explaining")}>
          Withdraw the explanation
        </button>
        <p>kept: {kept.join(", ")}</p>
      </>
    );
  }
  render(<Two />);
  await user.click(screen.getByRole("button", { name: "Explain" }));
  await user.click(screen.getByRole("button", { name: "Choose" }));
  await user.click(screen.getByRole("button", { name: "Withdraw the explanation" }));
  parked.resolve(answered("the explanation"));
  parked.resolve(answered("the choice"));
  expect(await screen.findByText("kept: the choice")).toBeTruthy();
});

test("a cancel can name only an operation the facade declares", () => {
  // The names are the facade's Go constants, generated as one union, so a
  // name it does not declare is refused by the build where it is written,
  // before anything runs; `npm run build` checks this file.
  const undeclared = () =>
    // @ts-expect-error "stop-everything" is not an operation the facade declares.
    useLifecycle<"working">({ names: { working: "stop-everything" } });
  const declared = () => useLifecycle<"working">({ names: { working: "run-explanation" } });
  expect([typeof undeclared, typeof declared]).toEqual(["function", "function"]);
});

test("every one of the six states is drawn through Status, with its word, its shape and its reason", () => {
  const indicators = indicatorTable();
  const states: State[] = ["empty", "busy", "cancelled", "failed", "permission_denied", "completed"];
  for (const state of states) {
    const result: Answer = { state, reason: `the ${state} reason` };
    const { container, unmount } = render(
      <IndicatorsContext.Provider value={indicators}>
        <Outcome result={result} />
      </IndicatorsContext.Provider>,
    );
    const status = within(container).getByRole("status");
    const indicator = indicators.get(state);
    expect(indicator).toBeTruthy();
    expect(status.className).toBe(`status status-${state}`);
    expect(within(status).getByText(indicator?.label ?? "")).toBeTruthy();
    expect(within(status).getByText(indicator?.symbol ?? "").getAttribute("aria-hidden")).toBe("true");
    expect(within(status).getByText(`the ${state} reason`)).toBeTruthy();
    unmount();
  }

  // While the operation runs that is the whole story, and without the
  // facade's indicators a state reads as its plain word.
  const { container } = render(<Outcome result={{ state: "failed", reason: "an earlier refusal" }} progress="Reading the file." />);
  const status = within(container).getByRole("status");
  expect(status.className).toBe("status status-busy");
  expect(within(status).getByText("busy")).toBeTruthy();
  expect(within(status).getByText("Reading the file.")).toBeTruthy();
  expect(within(container).queryByText("an earlier refusal")).toBeNull();
});
