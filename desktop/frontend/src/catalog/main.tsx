// Development-only entry. Production main.tsx never imports this module.
import { Component, type ReactNode } from "react";
import { createRoot } from "react-dom/client";
import App from "../App";
import "../styles.css";
import { installCatalogFixtures, unhandledMethods } from "./fixtures";
import inventory from "./inventory.json";

interface Recorder {
  Mode(): Promise<string>;
  Snapshot(
    name: string,
    x: number,
    y: number,
    width: number,
    height: number,
  ): Promise<void>;
  Finish(report: string): Promise<void>;
}
interface Shot {
  id: string;
  kind: string;
  scenario: string;
  component?: string;
  source?: string;
  file: string;
  rect: number[];
  partial: boolean;
}
const recorder = (window as unknown as { go: { main: { Catalog: Recorder } } })
  .go.main.Catalog;
const stub = installCatalogFixtures();
const shots: Shot[] = [];
const failures: string[] = [];

const coverage = new Map<string, number>();
const root = createRoot(document.getElementById("root")!);
let sequence = 0;
let variant = "page-ready";
class CaptureBoundary extends Component<
  { children: ReactNode },
  { error: string | null }
> {
  state: { error: string | null } = { error: null };
  static getDerivedStateFromError(error: Error) {
    return { error: error.message };
  }
  componentDidCatch(error: Error) {
    failures.push(`Render failure: ${error.message}`);
  }
  render() {
    return this.state.error ? (
      <p role="alert">Capture failed: {this.state.error}</p>
    ) : (
      this.props.children
    );
  }
}
const delay = (ms: number) =>
  new Promise<void>((resolve) => setTimeout(resolve, ms));
async function settled() {
  await delay(180);
  await document.fonts.ready;
  // WebKit may suspend animation frames while its window is occluded. The
  // native snapshot still requests an updated frame; never wait indefinitely
  // for a visibility-dependent browser callback.
  await Promise.race([
    new Promise<void>((resolve) =>
      requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
    ),
    delay(300),
  ]);
}
function visible(element: Element): element is HTMLElement {
  const r = element.getBoundingClientRect();
  return (
    r.width > 0 &&
    r.height > 0 &&
    !element.closest('[hidden], [aria-hidden="true"]') &&
    getComputedStyle(element).visibility !== "hidden"
  );
}
function buttonName(el: HTMLElement) {
  const label = el.getAttribute("aria-label");
  if (label) return label;
  const clone = el.cloneNode(true) as HTMLElement;
  clone.querySelectorAll('[aria-hidden="true"]').forEach((e) => e.remove());
  return clone.textContent?.trim() ?? "";
}
function buttons(scope: ParentNode = document) {
  return [...scope.querySelectorAll<HTMLButtonElement>("button")].filter(
    visible,
  );
}
async function clickText(text: string, scope: ParentNode = document) {
  const candidates = buttons(scope).filter((el) => buttonName(el) === text);
  if (candidates.length !== 1)
    throw new Error(`Expected one button ${text}; found ${candidates.length}`);
  candidates[0]!.click();
  await settled();
}
const slug = (value: string) =>
  value
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-|-$/g, "")
    .slice(0, 125);
function clippedRect(element: HTMLElement) {
  let r = element.getBoundingClientRect();
  let x = Math.max(0, r.left),
    y = Math.max(0, r.top),
    right = Math.min(innerWidth, r.right),
    bottom = Math.min(innerHeight, r.bottom);
  for (
    let parent = element.parentElement;
    parent;
    parent = parent.parentElement
  ) {
    const style = getComputedStyle(parent),
      p = parent.getBoundingClientRect();
    if (/auto|scroll|hidden|clip/.test(style.overflowY)) {
      y = Math.max(y, p.top);
      bottom = Math.min(bottom, p.bottom);
    }
    if (/auto|scroll|hidden|clip/.test(style.overflowX)) {
      x = Math.max(x, p.left);
      right = Math.min(right, p.right);
    }
  }
  return {
    x: Math.floor(x),
    y: Math.floor(y),
    width: Math.floor(right - x),
    height: Math.floor(bottom - y),
    partial:
      r.top < y || r.bottom > bottom + 1 || r.left < x || r.right > right + 1,
  };
}
// Only the catalog reads React's development inspection links. No production
// component is wrapped or restyled; failing to find a component is incomplete.
interface Fiber {
  type?: { name?: string; displayName?: string } | string;
  return?: Fiber;
  child?: Fiber;
  sibling?: Fiber;
  stateNode?: Node | { current?: Fiber };
}
function componentRegions() {
  const found = new Map<
    string,
    { area: number; partial: boolean; rect: ReturnType<typeof clippedRect> }
  >();
  const expected = new Set(
    inventory.map((item) => item.source + "#" + item.name),
  );
  const container = document.getElementById("root")!;
  const key = Object.keys(container).find((k) =>
    k.startsWith("__reactContainer$"),
  );
  if (!key) throw new Error("React root inspection link unavailable");
  const attached = (container as unknown as Record<string, Fiber>)[key]!;
  const current = (attached.stateNode as { current?: Fiber })?.current;
  if (!current) throw new Error("React current root unavailable");
  const openDialog = document.querySelector("dialog[open]");
  function bounds(fiber: Fiber) {
    const rects: ReturnType<typeof clippedRect>[] = [];
    function collect(node: Fiber | undefined) {
      for (let held = node; held; held = held.sibling) {
        const dom = held.stateNode;
        if (
          dom instanceof HTMLElement &&
          visible(dom) &&
          (!openDialog || openDialog.contains(dom))
        ) {
          const rect = clippedRect(dom);
          if (rect.width > 0 && rect.height > 0) rects.push(rect);
        } else if (
          dom instanceof Text &&
          dom.parentElement &&
          visible(dom.parentElement) &&
          (!openDialog || openDialog.contains(dom))
        ) {
          const range = document.createRange();
          range.selectNodeContents(dom);
          const r = range.getBoundingClientRect(),
            p = clippedRect(dom.parentElement);
          const x = Math.max(r.left, p.x),
            y = Math.max(r.top, p.y),
            right = Math.min(r.right, p.x + p.width),
            bottom = Math.min(r.bottom, p.y + p.height);
          if (right > x && bottom > y)
            rects.push({
              x: Math.floor(x),
              y: Math.floor(y),
              width: Math.floor(right - x),
              height: Math.floor(bottom - y),
              partial: r.top < y || r.bottom > bottom + 1,
            });
        }
        collect(held.child);
      }
    }
    collect(fiber.child);
    if (!rects.length) return null;
    const x = Math.min(...rects.map((r) => r.x)),
      y = Math.min(...rects.map((r) => r.y));
    return {
      x,
      y,
      width: Math.max(...rects.map((r) => r.x + r.width)) - x,
      height: Math.max(...rects.map((r) => r.y + r.height)) - y,
      partial: rects.some((r) => r.partial),
    };
  }
  function walk(node: Fiber | undefined) {
    for (let held = node; held; held = held.sibling) {
      const type = held.type,
        name =
          typeof type === "function" || typeof type === "object"
            ? (type?.displayName ?? type?.name)
            : undefined;
      if (name && expected.has(name)) {
        const rect = bounds(held);
        if (rect && rect.width > 1 && rect.height > 1) {
          const area = rect.width * rect.height,
            old = found.get(name);
          if (!old || area > old.area)
            found.set(name, { rect, area, partial: rect.partial });
        }
      }
      walk(held.child);
    }
  }
  walk(current.child);
  return found;
}
async function shot(
  kind: string,
  scenario: string,
  rect = { x: 0, y: 0, width: innerWidth, height: innerHeight, partial: false },
  component?: string,
) {
  const id = `${String(++sequence).padStart(4, "0")}-${slug(component ?? scenario)}`;
  await recorder.Snapshot(id, rect.x, rect.y, rect.width, rect.height);
  const item = inventory.find((c) => c.source + "#" + c.name === component);
  const source = item?.source;
  shots.push({
    id,
    kind,
    scenario,
    ...(component ? { component: item?.name ?? component } : {}),
    ...(source ? { source } : {}),
    file: `${id}.png`,
    rect: [rect.x, rect.y, rect.width, rect.height],
    partial: rect.partial,
  });
}
async function capture(scenario: string) {
  if (document.activeElement instanceof HTMLElement)
    document.activeElement.blur();
  await settled();
  await shot("page", scenario);
  for (const [name, region] of componentRegions()) {
    // First visible instance per scenario family; retain a second if the first
    // was clipped. Full pages and scrolled portions remain independently saved.
    const key = name + "/" + variant;
    if (coverage.get(key) === 1 || (coverage.has(key) && region.partial))
      continue;
    await shot("component", scenario, region.rect, name);
    coverage.set(key, region.partial ? 0 : 1);
  }
}
async function scrollCapture(scenario: string) {
  const rootScroller = document.scrollingElement as HTMLElement | null;
  const scrollables = [
    ...document.querySelectorAll<HTMLElement>("body *"),
  ].filter((element) => {
    if (!visible(element)) return false;
    const style = getComputedStyle(element);
    return (
      (/auto|scroll/.test(style.overflowY) &&
        element.scrollHeight > element.clientHeight + 2) ||
      (/auto|scroll/.test(style.overflowX) &&
        element.scrollWidth > element.clientWidth + 2)
    );
  });
  if (
    rootScroller &&
    (rootScroller.scrollHeight > rootScroller.clientHeight + 2 ||
      rootScroller.scrollWidth > rootScroller.clientWidth + 2)
  ) {
    scrollables.push(rootScroller);
  }
  const positions = [...new Set(scrollables)].map((element) => ({
    element,
    top: element.scrollTop,
    left: element.scrollLeft,
    behavior: element.style.scrollBehavior,
  }));
  try {
    for (const [index, saved] of positions.entries()) {
      const { element } = saved;
      element.style.scrollBehavior = "auto";
      if (element !== rootScroller)
        element.scrollIntoView({ block: "nearest", inline: "nearest" });
      const maxY = element.scrollHeight - element.clientHeight;
      const maxX = element.scrollWidth - element.clientWidth;
      const stepY = Math.max(100, element.clientHeight - 80);
      const stepX = Math.max(100, element.clientWidth - 80);
      let frames = 0;
      let prior = "";
      for (let y = 0; ; y = Math.min(y + stepY, maxY)) {
        for (let x = 0; ; x = Math.min(x + stepX, maxX)) {
          element.scrollTop = y;
          element.scrollLeft = x;
          await settled();
          const position = `${Math.round(element.scrollLeft)},${Math.round(element.scrollTop)}`;
          if (
            position !== prior &&
            (element.scrollLeft > 0 || element.scrollTop > 0)
          ) {
            if (++frames > 100)
              throw new Error(
                "Scroll capture exceeded 100 viewports for one container",
              );
            await capture(`${scenario} / scroll ${index + 1} at ${position}`);
          }
          prior = position;
          if (x >= maxX) break;
        }
        if (y >= maxY) break;
      }
    }
  } finally {
    for (const saved of positions.reverse()) {
      saved.element.scrollTop = saved.top;
      saved.element.scrollLeft = saved.left;
      saved.element.style.scrollBehavior = saved.behavior;
    }
    await settled();
  }
}
async function visit(scenario: string, action: () => Promise<void>) {
  try {
    await action();
    await capture(scenario);
    await scrollCapture(scenario);
  } catch (error) {
    failures.push(`${scenario}: ${String(error)}`);
  }
}
async function tabs(scenario: string, depth = 0) {
  if (depth > 4) throw new Error("nested tab depth exceeded");
  const page = document.querySelector(".page:not([hidden])")!;
  const lists = [
    ...page.querySelectorAll<HTMLElement>('[role="tablist"]'),
  ].filter(visible);
  const list = lists[depth];
  if (!list) return;
  for (const tab of [
    ...list.querySelectorAll<HTMLButtonElement>('[role="tab"]'),
  ]) {
    const name = tab.textContent?.trim() ?? tab.id;
    await visit(`${scenario} / ${name}`, async () => {
      tab.click();
      await settled();
    });
    await tabs(`${scenario} / ${name}`, depth + 1);
  }
}
let appGeneration = 0;
async function project() {
  root.render(<App key={++appGeneration} />);
  await settled();
  await clickText("Open");
}
async function openCase() {
  await clickText("Open case sample-case");
}
async function run() {
  const only = await recorder.Mode();
  if (only !== "private") {
    root.render(<App />);
    await settled();
    await visit("Projects / recent projects", async () => {});
    await visit("Projects / new project dialog", async () =>
      clickText("New project"),
    );
    const dialog = document.querySelector<HTMLDialogElement>("dialog[open]");
    dialog?.close();
    await settled();
    // Closing through its own button updates the parent's state too.
    const close = buttons().find((b) =>
      b.getAttribute("aria-label")?.startsWith("Close new project"),
    );
    close?.click();
    await settled();
    await visit("Projects / command palette", async () =>
      clickText("Search commands"),
    );
    await clickText("Close command palette");
    await clickText("Open");
    for (const name of [
      "Cases",
      "Tests",
      "Runs",
      "Environments",
      "Reports",
      "Tools",
      "Settings",
      "Help",
    ]) {
      await visit(name, async () =>
        clickText(name, document.querySelector(".region-navigation")!),
      );
      await tabs(name);
    }
    await visit("Cases / project overview", async () =>
      clickText("Cases", document.querySelector(".region-navigation")!),
    );
    await visit("Cases / messages", async () => {
      const b = buttons(document.querySelector(".page:not([hidden])")!).find(
        (b) =>
          (b.textContent ?? "").includes("Duplicate appointment") ||
          (b.textContent ?? "").trim() === "sample-case",
      );
      if (!b) throw new Error("case opening control missing");
      b.click();
      await settled();
    });
    await tabs("Cases / open case");
    await visit("Cases / more case actions menu", async () => {
      await project();
      await openCase();
      await clickText("More case actions");
    });
    for (const label of [
      "Edit…",
      "Edit Duplicate appointment after reschedule",
    ]) {
      await visit(`Project / ${label} dialog`, async () => {
        await project();
        await clickText(label);
      });
    }
    await visit("Project / search dialog", async () => {
      await project();
      await clickText("Search", document.querySelector(".region-commands")!);
    });
    for (const action of ["Import", "Capture", "Observations"]) {
      await visit(`Cases / ${action}`, async () => {
        await project();
        await clickText(action);
      });
      await tabs(`Cases / ${action}`);
    }
    for (const action of [
      "Create test",
      "Replay…",
      "Compare with another case",
      "Build a reproducer",
      "Reduce",
      "File details",
    ]) {
      await visit(`Cases / ${action}`, async () => {
        await project();
        await openCase();
        if (!["Create test", "Replay…"].includes(action))
          await clickText("More case actions");
        await clickText(action);
      });
    }
    for (const action of [
      "New filter…",
      "More list actions",
      "Inspect occ-000001",
    ]) {
      await visit(`Messages / ${action}`, async () => {
        await project();
        await openCase();
        await clickText(action);
      });
      if (action === "More list actions")
        await visit("Messages / Search settings dialog", async () =>
          clickText("Search settings…"),
        );
      if (action.startsWith("Inspect")) {
        for (const label of ["Fields", "Raw", "Hex"]) {
          await visit(`Message details / ${label}`, async () => {
            const t = [
              ...document.querySelectorAll<HTMLElement>('[role="tab"]'),
            ].find((t) => visible(t) && t.textContent?.trim() === label);
            if (!t) throw new Error(`Missing inspector tab ${label}`);
            t.click();
            await settled();
          });
        }
      }
    }

    await visit("Message details / more actions menu", async () => {
      await project();
      await openCase();
      await clickText("Inspect occ-000001");
      await clickText("More message actions");
    });
    await visit("Message details / Go to field dialog", async () => {
      await clickText("Go to field…");
    });
    const { examples, ExampleFrame } = await import("./examples");
    for (const example of examples) {
      variant = example.state;
      await visit(
        `Components / ${example.name} / ${example.state}`,
        async () => {
          root.render(
            <CaptureBoundary key={`${example.name}-${example.state}`}>
              <ExampleFrame example={example} />
            </CaptureBoundary>,
          );
          await settled();
        },
      );
      await tabs(`Components / ${example.name}`);
      for (const text of example.openButtons ?? []) {
        await visit(`Components / ${example.name} / ${text}`, async () =>
          clickText(text),
        );
      }
    }
  }
  const { privateExamples } = await import("./private-examples");
  const { ExampleFrame } = await import("./examples");
  for (const example of privateExamples) {
    variant = "private-ready";
    await visit(`Internal views / ${example.name}`, async () => {
      root.render(
        <CaptureBoundary key={example.name}>
          <ExampleFrame example={example} />
        </CaptureBoundary>,
      );
      await settled();
    });
  }
  for (const method of unhandledMethods)
    failures.push(`Missing fixture for ${method}`);
  const allExpected = inventory;
  const missing = allExpected.filter(
    (c) => !shots.some((s) => s.component === c.name && s.source === c.source),
  );
  await recorder.Finish(
    JSON.stringify(
      {
        schema: "readmit-screenshot-catalog/v1",
        renderer: "Wails WKWebView",
        viewport: [innerWidth, innerHeight],
        theme: matchMedia("(prefers-color-scheme: dark)").matches
          ? "dark"
          : "light",
        data: "synthetic fixtures",
        shots,
        failures,
        missing,
        inventory,
        calls: stub.calls.map((c) => c.method),
      },
      null,
      2,
    ),
  );
}
void run().catch(async (error) => {
  failures.push(String(error));
  await recorder.Finish(
    JSON.stringify({
      shots,
      failures,
      inventory,
      missing: inventory.filter((c) => c.exported),
    }),
  );
});
