import { expect, test } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useSchedules } from "./Schedules";
import { installFacade } from "./testkit/wails";
import type { CatalogItem, RequestContext, RunnerRow, ScheduleCommandRequest, ScheduleReview, ScheduleRow } from "./bindings";

const ROOT = "/projects/scheduling";

function Page(props: { suite?: string; runner?: string }) {
  const page = useSchedules({ root: ROOT, shown: true, ...props });
  return (
    <>
      <div>{page.actions}</div>
      {page.body}
    </>
  );
}

const SUITE: CatalogItem = {
  ref: { kind: "suite", id: "s1" }, name: "Scheduling smoke", created_at: null, updated_at: null, last_opened_at: null, availability: "available",
  capabilities: [], summary: {} as CatalogItem["summary"],
};
const RUNNER = { ref: { kind: "runner", id: "r1" }, name: "QA runner", config: "/c.json", environment: "Scheduling QA", hub_environment: "lab", hub: "hub", project: "alpha", root: "/r", status: "available", active_jobs: 0, local: false } as RunnerRow;

function schedule(over: Partial<ScheduleRow> = {}): ScheduleRow {
  return {
    id: "s-1", revision: 1, name: "Scheduling smoke", suite: "Scheduling smoke", version: "Version 4", environment: "Scheduling QA", runner: "QA runner",
    repeat: "daily", days: [], at: "02:30", zone: "UTC", window_minutes: 30, route: "", state: "enabled", next: "2026-10-01T02:30:00Z", recent: [],
    draft: { name: "Scheduling smoke", suite: { kind: "suite", id: "s1", revision: "4" }, environment: "qa", runner: "r1", repeat: "daily", days: [], at: "02:30", zone: "UTC", window_minutes: 30, route: "" },
    ...over,
  };
}

const REVIEW: ScheduleReview = {
  name: "Scheduling smoke", suite: "Scheduling smoke", version: "Version 4", environment: "Scheduling QA", runner: "QA runner",
  tests: [{ name: "Booking receives ACK", version: "Version 2" }], targets: [{ name: "Scheduling QA", version: "Version 3" }], resets: [],
  repeat: "weekdays", days: [], at: "02:30", zone: "America/New_York", next: ["2026-10-01T06:30:00Z", "2026-10-02T06:30:00Z", "2026-10-05T06:30:00Z"],
  window_minutes: 30, route: "", consequence: "Runs Scheduling smoke on Scheduling QA at the displayed times until paused.", token: "t".repeat(64),
};

function base(rows: () => ScheduleRow[], extra: object = {}) {
  return {
    ListSchedules: (request: { context: RequestContext }) => ({ state: "completed" as const, context: request.context, schedules: rows() }),
    ListCatalog: (query: { context: RequestContext }) => ({ state: "completed" as const, context: query.context, page: { items: [SUITE], total: 1, snapshot: "", recorded: true, incomplete: [] } }),
    ListRunners: (context: RequestContext) => ({ state: "completed" as const, context, runners: [RUNNER] }),
    OpenItemDraft: (request: { context: RequestContext }) => ({
      state: "completed" as const, context: request.context, new: false, ref: { kind: "suite" as const, id: "s1", revision: "4" },
      draft: { name: "Scheduling smoke", suite: { tags: [], concurrency: 1, tests: [], datasets: [], environments: [{ id: "qa", name: "Scheduling QA", site: "", bindings: [] }], requirements: [], exclusions: [] } },
    }),
    ...extra,
  };
}

// The recurrence is chosen from fixed choices and the review shows exact
// pins, targets and the next three occurrences in the schedule's own zone;
// the scheduler's acknowledgement is what enables it.
test("a new schedule is reviewed with exact pins and next occurrences, and enabled by the scheduler's acknowledgement", async () => {
  const user = userEvent.setup();
  let rows: ScheduleRow[] = [];
  const facade = installFacade(base(() => rows, {
    PrepareSchedule: (request: { context: RequestContext }) => ({ state: "completed", context: request.context, review: REVIEW, problems: [] }),
    CommandSchedule: (request: ScheduleCommandRequest) => {
      rows = [schedule({ zone: "America/New_York", repeat: "weekdays" })];
      return { state: "completed", context: request.context, schedule: "s-1", revision: 1, status: "enabled", next: REVIEW.next[0], pending: false, replayed: false };
    },
  }));
  render(<Page suite="s1" />);
  expect(await screen.findByText("No schedules")).toBeTruthy();
  expect(facade.oneCall("ListSchedules")[0]).toMatchObject({ suite: "s1" });
  await user.click(screen.getAllByRole("button", { name: "New schedule" })[0]!);
  const sheet = await screen.findByRole("dialog", { name: "New schedule" });
  await waitFor(() => expect((within(sheet).getByLabelText("Name") as HTMLInputElement).value).toBe("Scheduling smoke"));
  expect(within(sheet).getByText("Version 4")).toBeTruthy();
  expect(within(sheet).queryByLabelText(/cron|anchor|policy|path/i)).toBeNull();
  await user.selectOptions(within(sheet).getByLabelText("Runner"), "r1");
  await user.selectOptions(within(sheet).getByLabelText("Repeat"), "days");
  expect(within(sheet).getByRole("button", { name: "Review" }).hasAttribute("disabled")).toBe(true);
  await user.click(within(sheet).getByLabelText("Monday"));
  await user.selectOptions(within(sheet).getByLabelText("Repeat"), "weekdays");
  await user.selectOptions(within(sheet).getByLabelText("Time zone"), "America/New_York");
  await user.click(within(sheet).getByRole("button", { name: "Review" }));
  expect(await within(sheet).findByText("Booking receives ACK · Version 2")).toBeTruthy();
  expect(within(sheet).getByText(REVIEW.consequence)).toBeTruthy();
  expect(within(sheet).getByText(/Thu, Oct 1.*02:30/)).toBeTruthy();
  expect(facade.oneCall("PrepareSchedule")[0]).toMatchObject({ draft: { suite: { id: "s1", revision: "4" }, environment: "qa", runner: "r1", repeat: "weekdays", days: [], at: "02:30", zone: "America/New_York" } });
  expect(within(sheet).getByRole("button", { name: "Save paused" })).toBeTruthy();
  await user.click(within(sheet).getByRole("button", { name: "Enable schedule" }));
  await waitFor(() => expect(facade.callsTo("CommandSchedule")).toHaveLength(1));
  expect(facade.oneCall("CommandSchedule")[0]).toMatchObject({ kind: "create", enable: true, token: REVIEW.token, expected_revision: 0 });
  const table = await screen.findByRole("grid", { name: "Schedules" }).catch(() => screen.findByRole("table", { name: "Schedules" }));
  expect(within(table).getByText("Enabled")).toBeTruthy();
});

test("Save paused creates a real paused schedule without future authority", async () => {
  const user = userEvent.setup();
  const facade = installFacade(base(() => [], {
    PrepareSchedule: (request: { context: RequestContext }) => ({ state: "completed", context: request.context, review: REVIEW, problems: [] }),
    CommandSchedule: (request: ScheduleCommandRequest) => ({ state: "completed", context: request.context, schedule: "s-1", revision: 1, status: "paused", pending: false, replayed: false }),
  }));
  render(<Page />);
  await user.click((await screen.findAllByRole("button", { name: "New schedule" }))[0]!);
  const sheet = await screen.findByRole("dialog", { name: "New schedule" });
  await user.selectOptions(within(sheet).getByLabelText("Suite"), "s1");
  await user.selectOptions(within(sheet).getByLabelText("Runner"), "r1");
  await user.selectOptions(within(sheet).getByLabelText("Time zone"), "UTC");
  await waitFor(() => expect(within(sheet).getByRole("button", { name: "Review" }).hasAttribute("disabled")).toBe(false));
  await user.click(within(sheet).getByRole("button", { name: "Review" }));
  await user.click(await within(sheet).findByRole("button", { name: "Save paused" }));
  await waitFor(() => expect(facade.callsTo("CommandSchedule")).toHaveLength(1));
  expect(facade.oneCall("CommandSchedule")[0]).toMatchObject({ kind: "create", enable: false });
});

// A change the scheduler has not acknowledged reads Pending, never Paused or
// Enabled, and Send again sends that same change.
test("an unacknowledged change stays Pending and is sent again, never shown as done", async () => {
  const user = userEvent.setup();
  let rows = [schedule({ pending: "pause", pending_reason: "The hub did not acknowledge this change" })];
  const facade = installFacade(base(() => rows, {
    CommandSchedule: (request: ScheduleCommandRequest) => {
      rows = [schedule({ state: "paused", next: "" })];
      return { state: "completed", context: request.context, schedule: "s-1", revision: 2, status: "paused", pending: false, replayed: true };
    },
  }));
  render(<Page />);
  const table = await screen.findByRole("grid", { name: "Schedules" }).catch(() => screen.findByRole("table", { name: "Schedules" }));
  expect(within(table).getByText("Pending")).toBeTruthy();
  expect(within(table).queryByText("Paused")).toBeNull();
  await user.dblClick(within(table).getByText("Scheduling smoke"));
  const detail = await screen.findByRole("dialog", { name: "Scheduling smoke" });
  expect(within(detail).getByText("The hub did not acknowledge this change")).toBeTruthy();
  expect(within(detail).getByRole("button", { name: "Pause" }).hasAttribute("disabled")).toBe(true);
  await user.click(within(detail).getByRole("button", { name: "Send again" }));
  await waitFor(() => expect(facade.callsTo("CommandSchedule")).toHaveLength(1));
  expect(facade.oneCall("CommandSchedule")[0]).toMatchObject({ kind: "resend", schedule: "s-1" });
  expect(await within(table).findByText("Paused")).toBeTruthy();
});

test("Pause and Delete name the schedule and state their consequence; a refusal keeps the state", async () => {
  const user = userEvent.setup();
  const facade = installFacade(base(() => [schedule()], {
    CommandSchedule: (request: ScheduleCommandRequest) => ({ state: "failed", reason: "The hub's schedule service is not running. Nothing was changed.", context: request.context, revision: 0, pending: false, replayed: false }),
  }));
  render(<Page />);
  const table = await screen.findByRole("grid", { name: "Schedules" }).catch(() => screen.findByRole("table", { name: "Schedules" }));
  await user.dblClick(within(table).getByText("Scheduling smoke"));
  await user.click(within(await screen.findByRole("dialog", { name: "Scheduling smoke" })).getByRole("button", { name: "Pause" }));
  const pause = await screen.findByRole("dialog", { name: "Pause schedule" });
  expect(within(pause).getByText("Stops future scheduled runs; active runs continue.")).toBeTruthy();
  await user.click(within(pause).getByRole("button", { name: "Pause Scheduling smoke" }));
  expect(await within(pause).findByText("The hub's schedule service is not running. Nothing was changed.")).toBeTruthy();
  expect(facade.oneCall("CommandSchedule")[0]).toMatchObject({ kind: "pause", schedule: "s-1", expected_revision: 1 });
  expect(within(table).getByText("Enabled")).toBeTruthy();
  await user.click(within(pause).getByRole("button", { name: "Cancel" }));
  await user.click(within(screen.getByRole("dialog", { name: "Scheduling smoke" })).getByRole("button", { name: "Delete" }));
  const remove = await screen.findByRole("dialog", { name: "Delete schedule" });
  expect(within(remove).getByText("Removes this schedule; run history stays.")).toBeTruthy();
  expect(within(remove).getByRole("button", { name: "Delete Scheduling smoke" })).toBeTruthy();
});

test("without the hub's scheduler the page says why and schedules nothing locally", async () => {
  installFacade({
    ...base(() => []),
    ListSchedules: (request: { context: RequestContext }) => ({ state: "failed", reason: "Schedules run on the team hub. Connect to it and sign in.", context: request.context, schedules: [] }),
  });
  render(<Page />);
  expect(await screen.findByText("Schedules run on the team hub. Connect to it and sign in.")).toBeTruthy();
  expect(screen.queryByRole("button", { name: "New schedule" })).toBeNull();
});

// Export policy is the expert action for a hub run from an installed policy
// file: pins are computed from each test, never typed.
test("schedule revisions show effective timing, missed and overlap semantics", async () => {
  const user = userEvent.setup();
  const entry = { id: "nightly", zone: "UTC", at: "02:30", window_seconds: 1800, runner_config: "/c.json", spec: "/srv/tests/booking.json", input_sha256: "", route: "", approved: false };
  const preview = { state: "completed" as const, identity: "i".repeat(64), concurrency: "serial-skip-missed",
    entries: [{ entry: { ...entry, input_sha256: "p".repeat(64) }, identity: "p".repeat(64), pin_state: "computed", occurrences: [{ day: "2026-10-01", utc: "2026-10-01T02:30:00Z", state: "scheduled" }, { day: "2026-10-02", state: "dst-gap" }] }] };
  const facade = installFacade(base(() => [schedule()], {
    ChooseRunnerPath: (kind: string) => ({ state: "completed", kind, paths: [kind === "spec" ? "/srv/tests/booking.json" : "/etc/readmit-hub/schedules.json"] }),
    OpenSchedulePolicy: () => ({ state: "completed", entries: [] }),
    PreviewSchedulePolicy: () => preview,
    SaveSchedulePolicy: () => ({ ...preview }),
  }));
  render(<Page />);
  await user.click(await screen.findByRole("button", { name: "More schedule actions" }));
  await user.click(await screen.findByRole("menuitem", { name: "Export policy" }));
  const sheet = await screen.findByRole("dialog", { name: "Export policy" });
  await user.click(within(sheet).getByRole("button", { name: "Open policy…" }));
  await waitFor(() => expect(facade.callsTo("OpenSchedulePolicy")).toHaveLength(1));
  await user.type(within(sheet).getByLabelText("Entry name"), "nightly");
  await user.selectOptions(within(sheet).getByLabelText("Time zone"), "UTC");
  await user.selectOptions(within(sheet).getByLabelText("Runner"), "r1");
  await user.click(within(sheet).getByRole("button", { name: "Choose test" }));
  await user.click(within(sheet).getByRole("button", { name: "Add entry" }));
  await user.click(within(sheet).getByRole("button", { name: "Next" }));
  expect(await within(sheet).findByText("Matches the test")).toBeTruthy();
  expect(within(sheet).getByText(/2026-10-02 skipped/)).toBeTruthy();
  expect(facade.oneCall("PreviewSchedulePolicy")[0]).toMatchObject({ entries: [{ id: "nightly", input_sha256: "", runner_config: "/c.json", window_seconds: 1800 }] });
  await user.click(within(sheet).getByRole("button", { name: "Export policy" }));
  await waitFor(() => expect(facade.callsTo("SaveSchedulePolicy")).toHaveLength(1));
  expect(facade.oneCall("SaveSchedulePolicy")[0]).toMatchObject({ output: "" });
});
