// Help (views 12 and 44) and the demo, driven over the stubbed facade. The
// articles are the build's own; the demo's steps are done only from the
// actual answers of the ordinary screens they open.
import { expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderApp } from "./testkit/app";
import { CASE_ENTRY, CASE_IDENTITY, caseResult, folderWithCase, WORKSPACE_ROOT } from "./testkit/fixtures";
import { goTo, page, sidebar } from "./testkit/navigation";
import type { DemoProgress, HelpArticle, RequestContext } from "./bindings";

const TOPICS = [
  { id: "import-messages", title: "Import messages", kind: "task" as const },
  { id: "investigate-a-case", title: "Investigate a case", kind: "task" as const },
  { id: "create-a-regression-test", title: "Create a regression test", kind: "task" as const },
  { id: "connect-a-test-system", title: "Connect a test system", kind: "task" as const },
  { id: "share-a-report", title: "Share a report", kind: "task" as const },
];

const INVESTIGATE: HelpArticle = {
  id: "investigate-a-case",
  title: "Investigate a case",
  kind: "task",
  steps: [
    "Open a case to read its messages.",
    "Use Search or Filter to narrow the list.",
    "Select a message to open Fields, Raw or Hex.",
    "Use Timeline for event relationships and Findings for analysis results.",
  ],
  body: [],
  action: { id: "open-cases", label: "Open cases" },
  related: [{ id: "message-timestamps", title: "Message timestamps" }],
};

const helpHandlers = {
  HelpTopics: () => ({ state: "completed" as const, topics: TOPICS }),
  HelpArticle: (id: string) =>
    id === INVESTIGATE.id ? { state: "completed" as const, article: INVESTIGATE } : { state: "failed" as const, reason: "this help article is not in this version" },
};

async function openProject(user: ReturnType<typeof userEvent.setup>) {
  await goTo(user, "Projects");
  await user.click(screen.getByRole("button", { name: "Open" }));
  await sidebar().findByRole("button", { name: /^Project: / });
}

test("Help lists the five tasks and an article launches its task in the current project", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({ ...helpHandlers, SelectWorkspace: () => folderWithCase() });
  await openProject(user);
  await goTo(user, "Help");
  const topics = within(await page().findByRole("list", { name: "Help topics" }));
  expect(topics.getAllByRole("button").map((row) => row.textContent)).toEqual(TOPICS.map((topic) => topic.title));
  await user.click(topics.getByRole("button", { name: "Investigate a case" }));
  expect(await screen.findByRole("heading", { level: 1, name: "Investigate a case" })).toBeTruthy();
  expect([...page().getByRole("article").querySelectorAll("ol > li")].map((step) => step.textContent)).toEqual(INVESTIGATE.steps);
  expect(page().getByRole("button", { name: "Message timestamps" })).toBeTruthy();
  await user.click(page().getByRole("button", { name: "Open cases" }));
  expect(sidebar().getByRole("button", { name: "Cases" }).getAttribute("aria-current")).toBe("page");
  expect(facade.oneCall("HelpArticle")).toEqual(["investigate-a-case"]);
});

test("a missing article says so and offers Back", async () => {
  const user = userEvent.setup();
  await renderApp(helpHandlers);
  await goTo(user, "Help");
  await user.click(await page().findByRole("button", { name: "Investigate a case" }));
  await screen.findByRole("heading", { level: 1, name: "Investigate a case" });
  await user.click(page().getByRole("button", { name: "Message timestamps" }));
  expect(await page().findByRole("alert")).toHaveProperty("textContent", "This help article is not in this version.");
  expect(page().queryByText("Open cases")).toBeNull();
  await user.click(page().getByRole("button", { name: "Back to help" }));
  expect(await screen.findByRole("heading", { level: 1, name: "Investigate a case" })).toBeTruthy();
});

test("Search help finds bundled articles or offers Clear search", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    ...helpHandlers,
    SearchHelp: (query: string) =>
      query.includes("time")
        ? { state: "completed" as const, matches: [{ id: "message-timestamps", title: "Message timestamps", kind: "troubleshooting" as const }] }
        : { state: "empty" as const, reason: "no bundled topic matches", matches: [] },
  });
  await goTo(user, "Help");
  await user.click(page().getByRole("button", { name: "Search help" }));
  const sheet = within(await screen.findByRole("dialog", { name: "Search help" }));
  await user.type(sheet.getByRole("searchbox"), "zzz");
  expect(await sheet.findByText("No matching topics")).toBeTruthy();
  await user.click(sheet.getByRole("button", { name: "Clear search" }));
  expect((sheet.getByRole("searchbox") as HTMLInputElement).value).toBe("");
  await user.type(sheet.getByRole("searchbox"), "time");
  await user.click(await sheet.findByRole("button", { name: "Message timestamps" }));
  await waitFor(() => expect(facade.callsTo("HelpArticle").at(-1)?.args).toEqual(["message-timestamps"]));
});

test("Diagnostics shows the version and supported operations on demand", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    Diagnostics: () => ({
      state: "completed" as const,
      diagnostics: { version: "1.4.0", go_version: "go1.27.1", os: "darwin", arch: "arm64", operations: [{ method: "StartBenchmark", name: "corpus", interruptible: true, prerequisites: [] }] },
    }),
  });
  await goTo(user, "Help");
  expect(facade.callsTo("Diagnostics")).toHaveLength(0);
  await user.click(page().getByRole("button", { name: "More help actions" }));
  await user.click(screen.getByRole("menuitem", { name: "Diagnostics" }));
  const sheet = within(await screen.findByRole("dialog", { name: "Diagnostics" }));
  expect(await sheet.findByText("1.4.0")).toBeTruthy();
  expect(sheet.getByText("macOS, Apple silicon")).toBeTruthy();
  expect(sheet.getByText("Start benchmark")).toBeTruthy();
  expect(facade.callsTo("Diagnostics")).toHaveLength(1);
});

function demo(steps: Partial<Record<string, { done?: boolean; ref?: string; output?: string }>>, spec?: string): DemoProgress {
  const titles: [DemoProgress["steps"][number]["id"], string, "project" | "session"][] = [
    ["open-messages", "Open sample messages", "session"],
    ["create-test", "Create supplied test", "project"],
    ["run-defective", "Run defective receiver", "project"],
    ["view-failed-check", "View failed check", "session"],
    ["run-fixed", "Run fixed receiver", "project"],
    ["compare", "Compare results", "session"],
  ];
  return {
    case: CASE_ENTRY,
    identity: CASE_IDENTITY,
    case_ref: { kind: "case", id: "case-1" },
    target: "practice-target.json",
    ...(spec ? { spec } : {}),
    test: { schema: "readmit-test-draft/v1", case: { entry: CASE_ENTRY, identity: CASE_IDENTITY }, name: "Rescheduling updates the original appointment", messages: [], target: "practice-target.json", boundary: "appointment-ledger", observation: "practice-observation.json", reset: "", expectations: [] },
    steps: titles.map(([id, title, evidence]) => ({
      id,
      title,
      evidence,
      done: steps[id]?.done ?? false,
      ...(steps[id]?.ref ? { ref: { kind: "run" as const, id: steps[id]!.ref! } } : {}),
      ...(steps[id]?.output ? { output: steps[id]!.output! } : {}),
    })),
  };
}

async function openDemo(handlers: Parameters<typeof renderApp>[0] = {}) {
  const user = userEvent.setup();
  const rendered = await renderApp({
    OpenDemoProject: () => ({ state: "completed", context: { project: WORKSPACE_ROOT, project_id: "demo-1", generation: 0 }, recorded: true }),
    OpenWorkspace: () => folderWithCase(),
    OpenNamedProject: () => ({ state: "completed", context: { project: WORKSPACE_ROOT, generation: 0 }, recorded: true }),
    DemoProgress: (context: RequestContext) => ({ state: "completed", demo: demo({}), context }),
    ...handlers,
  });
  await goTo(user, "Help");
  await user.click(page().getByRole("button", { name: "Try demo" }));
  const task = within(await sidebar().findByRole("region", { name: "Demo" }));
  return { user, task, ...rendered };
}

test("Try demo opens the managed demo project and marks a step done only after its result", async () => {
  const { user, task, facade } = await openDemo();
  expect(facade.oneCall("OpenDemoProject")).toEqual([]);
  expect(task.getByText("Demo · Synthetic")).toBeTruthy();
  // Opening the sample messages is done only once the case verified.
  const parked = facade.park("OpenCase");
  await user.click(task.getByRole("button", { name: "Open sample messages" }));
  expect(task.getByRole("listitem", { name: "Open sample messages" })).toBeTruthy();
  parked.resolve(caseResult());
  expect(await task.findByRole("listitem", { name: "Open sample messages, done" })).toBeTruthy();

  // The project holds the created test now; the defective run executes it
  // against the practice receiver, into the entry the demo names.
  facade.reply({
    DemoProgress: () => ({ state: "completed", demo: demo({ "create-test": { done: true }, "run-defective": { output: "defective-run" } }, "test-1-test.json") }),
  });
  await goTo(user, "Cases");
  const practice = facade.park("RunPractice");
  await user.click(await task.findByRole("button", { name: "Run defective receiver" }));
  expect(facade.oneCall("RunPractice")[0]).toEqual({ workspace: WORKSPACE_ROOT, spec: "test-1-test.json", trial: "baseline", output: "defective-run" });
  expect(task.queryByRole("listitem", { name: "Run defective receiver, done" })).toBeNull();
  facade.reply({
    DemoProgress: () => ({ state: "completed", demo: demo({ "create-test": { done: true }, "run-defective": { done: true, ref: "run-1" } }, "test-1-test.json") }),
    OpenRun: (request: { context: RequestContext }) => ({ state: "failed", context: request.context, reason: "not arranged" }),
  });
  practice.resolve({ state: "completed", practice: { output: "defective-run", trial: "baseline", status: "assertion_failure", identity: "x", spec_identity: "y", assertions: [], changed_bindings: [] } });
  expect(await task.findByRole("listitem", { name: "Run defective receiver, done" })).toBeTruthy();
  // It opens on its ordinary run page.
  await waitFor(() => expect(facade.callsTo("OpenRun").at(-1)?.args[0]).toMatchObject({ run: { kind: "run", id: "run-1" } }));
});

test("closing demo guidance leaves Cases usable and starts nothing", async () => {
  const { user, task, facade } = await openDemo();
  await user.click(task.getByRole("button", { name: "Close demo steps" }));
  expect(sidebar().queryByRole("region", { name: "Demo" })).toBeNull();
  expect(sidebar().getByRole("button", { name: "Demo · Synthetic" })).toBeTruthy();
  await goTo(user, "Cases");
  expect(await page().findByRole("button", { name: "Import" })).toBeTruthy();
  expect(facade.callsTo("RunPractice")).toHaveLength(0);
  expect(facade.callsTo("OpenCase")).toHaveLength(0);
});
