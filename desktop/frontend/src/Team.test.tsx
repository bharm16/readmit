import { expect, test } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HubPanel } from "./HubPanel";
import { TeamTransferOutcome } from "./TeamCollaboration";
import { installFacade, installHubAdmin } from "./testkit/wails";
import { defaultHubResult, teamResult } from "./testkit/fixtures";
import type { ActionReview, HubResult, HubReviewCommandRequest, HubTeamResult } from "./bindings";

const signedIn = (overrides: Partial<HubResult> = {}): HubResult =>
  defaultHubResult({
    team: "Integration team",
    subject: "rui",
    projects: [{ project: "cardio-study", authorized: true }],
    ...overrides,
  });

const configured: HubResult = { state: "completed", connected: false, authenticated: false, config_path: "/etc/readmit/hub-client.json", hub_url: "https://hub.example:8443", team: "Integration team" };

function review(action: ActionReview["action"], extra: Partial<ActionReview> = {}): ActionReview {
  return { token: `${action}-token`, action, consent: "upload", items: [], destination: {}, requirements: [], ready: true, ...extra };
}

async function openTab(user: ReturnType<typeof userEvent.setup>, name: string) {
  await user.click(await screen.findByRole("tab", { name }));
}

test("an uncertain revision checks retained metadata without repeating its write", async () => {
  const user = userEvent.setup();
  let confirmed = false;
  const facade = installFacade({ ReconcileTeamTransfer: (operation) => ({ operation, state: "completed", outcome: "completed", replayed: false, context: { project: "", generation: 0 }, team: { project: "cardio-study", digest: "c".repeat(64), revision: "revision-uncertain", uploaded: true } }) });
  render(<TeamTransferOutcome result={{ operation: "uncertain-operation", state: "failed", outcome: "uncertain", replayed: false, context: { project: "", generation: 0 }, team: { project: "cardio-study", resource: "notes", digest: "c".repeat(64), command_id: "revision-uncertain", uploaded: true } }} onConfirmed={() => { confirmed = true; }} />);
  expect(screen.getByText("File uploaded")).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Check status" }));
  expect(await screen.findByText("Revision recorded")).toBeTruthy();
  expect(confirmed).toBe(true);
  expect(facade.oneCall("ReconcileTeamTransfer")[0]).toBe("uncertain-operation");
  expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
  expect(facade.callsTo("PostHubLifecycle")).toHaveLength(0);
});

test("Connect team saves a name and the organization's configuration without connecting", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    HubStatus: () => ({ state: "empty", connected: false, authenticated: false, reason: "no customer hub configured" }),
    ChooseHubTeamConfig: () => ({ state: "completed", config: "/Users/rui/hub-client.json", hub_url: "https://hub.example:8443", projects: ["cardio-study"], name: "hub.example" }),
    SaveHubTeam: (request) => ({ ...configured, team: request.name }),
  });
  render(<HubPanel />);
  await user.click(await screen.findByRole("button", { name: "Connect team" }));
  await user.click(screen.getByRole("button", { name: "Choose file…" }));
  const name = screen.getByLabelText("Name") as HTMLInputElement;
  await waitFor(() => expect(name.value).toBe("hub.example"));
  await user.clear(name);
  await user.type(name, "Integration team");
  await user.click(screen.getByRole("button", { name: "Save" }));
  expect(facade.oneCall("SaveHubTeam")[0]).toEqual({ name: "Integration team", config: "/Users/rui/hub-client.json" });
  expect(await screen.findByRole("button", { name: "Sign in" })).toBeTruthy();
  expect(screen.getByText("Integration team")).toBeTruthy();
  expect(facade.callsTo("ConnectHub")).toHaveLength(0);
  expect(facade.callsTo("StartHubAuth")).toHaveLength(0);
});

test("a remembered configuration that no longer validates is connected afresh, not edited", async () => {
  const user = userEvent.setup();
  installFacade({
    HubStatus: () => ({ state: "failed", connected: false, authenticated: false, config_path: "/etc/readmit/hub-client.json", hub_url: "http://hub.example:8443", team: "Integration team", reason: "the remembered hub configuration no longer validates (endpoint must use https); choose a hub configuration again" }),
  });
  render(<HubPanel />);
  expect(await screen.findByText(/no longer validates/)).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Connect team" }));
  const sheet = within(await screen.findByRole("dialog", { name: "Connect team" }));
  expect((sheet.getByLabelText("Name") as HTMLInputElement).value).toBe("");
  expect(sheet.getByRole("button", { name: "Choose file…" })).toBeTruthy();
  expect((sheet.getByRole("button", { name: "Save" }) as HTMLButtonElement).disabled).toBe(true);
});

test("Sign in is one flow that checks setup, connects and signs in through the browser, then reads the project", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    HubStatus: () => configured,
    DiagnoseHub: () => ({ state: "completed", passed: false, checks: [{ name: "ca", passed: false, message: "The team's certificate authority file is missing" }] }),
  });
  render(<HubPanel />);
  await user.click(await screen.findByRole("button", { name: "Sign in" }));
  // A setup problem is shown in the flow with Edit; nothing connects.
  expect(await screen.findByText("The team's certificate authority file is missing")).toBeTruthy();
  expect(screen.getByRole("button", { name: "Edit" })).toBeTruthy();
  expect(facade.callsTo("ConnectHub")).toHaveLength(0);

  facade.reply({
    DiagnoseHub: () => ({ state: "completed", passed: true, checks: [] }),
    ConnectHub: () => ({ ...configured, connected: true }),
    StartHubAuth: () => ({ state: "completed", auth_url: "https://idp.example/authorize?state=x", port: 5555, opened: false }),
    CompleteHubAuth: () => ({ state: "cancelled", connected: true, authenticated: false, reason: "sign-in was cancelled" }),
  });
  await user.click(screen.getByRole("button", { name: "Try again" }));
  // A browser the application could not open is offered as a link; a
  // cancelled sign-in keeps the team configured and says why.
  expect(await screen.findByText("sign-in was cancelled")).toBeTruthy();
  expect(facade.callsTo("ReadHubTeam")).toHaveLength(0);

  facade.reply({
    StartHubAuth: () => ({ state: "completed", auth_url: "https://idp.example/authorize?state=y", port: 5555, opened: true }),
    CompleteHubAuth: () => signedIn(),
    ReadHubTeam: () => teamResult(),
  });
  await user.click(screen.getByRole("button", { name: "Try again" }));
  // The project's metadata is read through the session it started.
  await waitFor(() => expect(facade.callsTo("ReadHubTeam")).toHaveLength(1));
  expect(facade.oneCall("ReadHubTeam")[0]).toMatchObject({ project: "cardio-study" });
  expect(await screen.findByRole("tab", { name: "Activity" })).toBeTruthy();
  expect(screen.getByText("booking-rules")).toBeTruthy();
  expect(screen.getByRole("button", { name: "Account" }).textContent).toBe("rui");

  // Search asks the hub only when the person searches.
  facade.reply({ SearchHubReviews: () => ({ state: "completed", events: [{ schema: "readmit-hub-review-event/v1", project: "cardio-study", sequence: 1, issuer: "https://idp.example", actor: "ana", at: "2026-09-18T10:00:00Z", kind: "comment", evidence: "a".repeat(64), text: "timeout", command_id: "c1" }] }) });
  await user.click(screen.getByRole("button", { name: "Search" }));
  const search = await screen.findByRole("dialog", { name: "Search activity" });
  await user.type(within(search).getByLabelText("Text"), "timeout");
  await user.click(within(search).getByRole("button", { name: "Search" }));
  expect(facade.oneCall("SearchHubReviews")[0]).toEqual({ project: "cardio-study", after: 0, text: "timeout", evidence: "" });
  expect(await screen.findByRole("button", { name: "Clear search" })).toBeTruthy();
  expect(within(screen.getByRole("table", { name: "Activity" })).getByText("Commented")).toBeTruthy();
});

test("switching project or signing out drops the previous project's data and a late reply", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    HubStatus: () => signedIn({ projects: [{ project: "cardio-study", authorized: true }, { project: "oncology", authorized: true }] }),
  });
  const first = facade.park("ReadHubTeam");
  render(<HubPanel />);
  await waitFor(() => expect(first.size).toBe(1));
  facade.reply({ ReadHubTeam: () => teamResult({ project: "oncology", activity: [{ key: "x", actor: "oli", action: "comment", at: "2026-09-21T10:00:00Z", object: "Oncology plan" }] }) });
  await user.selectOptions(screen.getByRole("combobox", { name: "Project" }), "oncology");
  expect(await screen.findByText("Oncology plan")).toBeTruthy();
  first.resolve(teamResult());
  await new Promise((resolve) => setTimeout(resolve, 20));
  expect(screen.queryByText("booking-rules")).toBeNull();

  facade.reply({ DisconnectHub: () => ({ ...configured, custody_warning: "Downloaded copies remain under local custody and cannot be revoked." }) });
  await user.click(screen.getByRole("button", { name: "Account" }));
  await user.click(screen.getByRole("menuitem", { name: "Sign out" }));
  expect(await screen.findByRole("button", { name: "Sign in" })).toBeTruthy();
  expect(screen.queryByText("Oncology plan")).toBeNull();
});

test("Files lists metadata only, Download names a new file and Upload is reviewed before it is sent", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    HubStatus: () => signedIn(),
    ReadHubTeam: () => teamResult(),
    DownloadHubFile: (request) => ({ state: "completed", transfer_state: "completed", path: `/Users/rui/${request.name}`, digest: request.digest }),
    ChooseHubLocalCopy: () => ({ state: "completed", kind: "file", paths: ["/Users/rui/results.json"] }),
    PrepareAction: (request) => ({ state: "completed", context: request.context, review: review("team.upload", { team: { team: "Integration team", project: "cardio-study", name: "results.json", size: 15 } }) }),
    ExecuteReviewedAction: (request) => ({ state: "completed", context: request.context, outcome: "completed", replayed: false, team: { project: "cardio-study", digest: "d".repeat(64) } }),
  });
  render(<HubPanel />);
  await openTab(user, "Files");
  const table = await screen.findByRole("table", { name: "Files" });
  expect(within(table).getByText("booking-rules")).toBeTruthy();
  expect(within(table).getByText(`Artifact · ${"c".repeat(7)}`)).toBeTruthy();
  await user.click(within(table).getByText("booking-rules"));
  expect(facade.callsTo("DownloadHubFile")).toHaveLength(0);
  await user.click(screen.getByRole("button", { name: "Download" }));
  expect(facade.oneCall("DownloadHubFile")[0]).toEqual({ project: "cardio-study", digest: "b".repeat(64), name: "booking-rules" });
  expect(await screen.findByText("Saved /Users/rui/booking-rules")).toBeTruthy();

  await user.click(screen.getByRole("button", { name: "Upload" }));
  const sheet = await screen.findByRole("dialog", { name: "Upload" });
  expect(await within(sheet).findByText("results.json")).toBeTruthy();
  expect(within(sheet).getByText("Uploads results.json to Integration team/cardio-study.")).toBeTruthy();
  expect(facade.callsTo("PrepareAction")[0]!.args[0]).toMatchObject({ action: "team.upload", team: { project: "cardio-study", source: "/Users/rui/results.json" } });
  expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(0);
  await user.click(within(sheet).getByRole("button", { name: "Upload" }));
  await waitFor(() => expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(1));
  expect(facade.oneCall("ExecuteReviewedAction")[0]).toMatchObject({ token: "team.upload-token" });
});

test("a review shows the version's changes and records Approve, Request changes and Comment as distinct commands once", async () => {
  const user = userEvent.setup();
  const posted: HubReviewCommandRequest[] = [];
  let failNext = true;
  let head = 2;
  const facade = installFacade({
    HubStatus: () => signedIn(),
    ReadHubTeam: () => teamResult({ review_head: head }),
    PrepareAction: (request) => ({
      state: "completed",
      context: request.context,
      review: review("suite.approve-release", {
        consent: "approve",
        suite_approval: {
          scope: "release",
          suite: "Scheduling smoke",
          version: "4",
          actor: "rui",
          tests: [],
          targets: [],
          comparison: { state: "completed", context: request.context, to: "4", first: false, tests: [], changes: [{ area: "setting", subject: "Timeout", earlier: "10 s", later: "5 s" }] },
        },
      }),
    }),
    WithdrawReview: () => ({ state: "completed", context: { project: "", generation: 0 } }),
    PostHubReview: (request) => {
      posted.push(request);
      if (failNext) {
        failNext = false;
        return { state: "failed", reason: "review refused with status 500" };
      }
      if (request.kind === "comment" && request.expected !== head) return { state: "failed", resolved: true, reason: "hub head or revision conflict; fetch current state and renew the action" };
      return { state: "completed", head: head + 1 };
    },
    ExecuteReviewedAction: (request) => ({ state: "completed", context: request.context, outcome: "completed", replayed: false }),
  });
  render(<HubPanel />);
  await openTab(user, "Reviews");
  await user.dblClick(await within(await screen.findByRole("table", { name: "Reviews" })).findByText("Scheduling smoke · Version 4"));
  expect(await screen.findByText(/Timeout/)).toBeTruthy();
  expect(screen.getByText("10 s")).toBeTruthy();
  expect(facade.callsTo("WithdrawReview")).toHaveLength(1);

  // Request changes needs a reason; a retry after a lost answer sends the
  // same command identity.
  await user.click(screen.getByRole("button", { name: "Request changes" }));
  let sheet = await screen.findByRole("dialog", { name: "Request changes" });
  expect((within(sheet).getByRole("button", { name: "Request changes" }) as HTMLButtonElement).disabled).toBe(true);
  await user.type(within(sheet).getByLabelText("Reason"), "Timeout too short");
  await user.click(within(sheet).getByRole("button", { name: "Request changes" }));
  expect(await within(sheet).findByRole("alert")).toBeTruthy();
  await user.click(within(sheet).getByRole("button", { name: "Request changes" }));
  await waitFor(() => expect(posted).toHaveLength(2));
  expect(posted[0]!.id).toBe(posted[1]!.id);
  expect(posted[1]).toMatchObject({ kind: "change-request", parent: "ask-1", recipient: "", text: "Timeout too short", release: "a".repeat(64), expected: 2 });

  // A comment made against a history that moved on is refused, read again,
  // and the next decision is a new command.
  head = 3;
  await user.click(await screen.findByRole("button", { name: "Comment" }));
  sheet = await screen.findByRole("dialog", { name: "Comment" });
  await user.type(within(sheet).getByLabelText("Comment"), "Looks close");
  await user.click(within(sheet).getByRole("button", { name: "Comment" }));
  expect(await within(sheet).findByText("Someone else changed this project meanwhile. Look at it again before deciding.")).toBeTruthy();
  await user.click(within(sheet).getByRole("button", { name: "Comment" }));
  await waitFor(() => expect(posted).toHaveLength(4));
  expect(posted[2]).toMatchObject({ kind: "comment", expected: 2, release: "" });
  expect(posted[3]).toMatchObject({ kind: "comment", expected: 3 });
  expect(posted[3]!.id).not.toBe(posted[2]!.id);

  await user.click(await screen.findByRole("button", { name: "Approve" }));
  sheet = await screen.findByRole("dialog", { name: "Approve" });
  await user.type(within(sheet).getByLabelText("Reason"), "Matches the reschedule rule");
  await user.click(within(sheet).getByRole("button", { name: "Approve" }));
  await waitFor(() => expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(1));
  expect(facade.oneCall("ExecuteReviewedAction")[0]).toMatchObject({ token: "suite.approve-release-token", decisions: { rationale: "Matches the reschedule rule" } });
});

test("a support summary review reads only the summary, approves it once and downloads the approved summary", async () => {
  const user = userEvent.setup();
  const support: HubTeamResult["reviews"][number] = {
    id: "support-ask",
    support: true,
    item: "Support summary",
    requested_by: "ana",
    recipient: "rui",
    requested: "2026-09-20T10:00:00Z",
    updated: "2026-09-20T10:00:00Z",
    status: "requested",
    evidence: "e".repeat(64),
    release: "f".repeat(64),
    to_me: true,
    discussion: [],
    requests: [{ id: "support-ask", evidence: "e".repeat(64), release: "f".repeat(64) }],
    policy_version: 2,
    policy_current: true,
  };
  const facade = installFacade({
    HubStatus: () => signedIn(),
    ReadHubTeam: () => teamResult({ reviews: [support] }),
    ReadHubSupportSummary: () => ({ state: "completed", source_kind: "retained-packet", outcome: "assertion_failure" }),
    PostHubReview: () => ({ state: "completed", head: 3 }),
  });
  facade.reply({
    ListHubReviewers: () => ({ state: "completed", members: [{ subject: "rui", role: "reviewer" }] }),
    PostHubSupportReview: () => ({ state: "completed", head: 2 }),
  });
  render(<HubPanel entries={[{ name: "support-bundle", kind: "support" } as never]} />);
  await openTab(user, "Reviews");
  // Request review asks one of the project's reviewers, from the hub's list.
  await user.click(await screen.findByRole("button", { name: "Request review" }));
  const request = await screen.findByRole("dialog", { name: "Request approval" });
  await waitFor(() => expect((within(request).getByLabelText("Reviewer") as HTMLSelectElement).value).toBe("rui"));
  await user.click(within(request).getByRole("button", { name: "Request approval" }));
  await waitFor(() => expect(facade.callsTo("PostHubSupportReview")).toHaveLength(1));
  expect(facade.oneCall("PostHubSupportReview")[0]).toMatchObject({ project: "cardio-study", entry: "support-bundle", kind: "support-request", recipient: "rui" });
  expect(facade.oneCall("ListHubReviewers")[0]).toBe("cardio-study");
  await user.dblClick(await within(await screen.findByRole("table", { name: "Reviews" })).findByText("Support summary"));
  expect(await screen.findByText("Check failed")).toBeTruthy();
  expect(facade.oneCall("ReadHubSupportSummary")[0]).toEqual({ project: "cardio-study", digest: "e".repeat(64) });
  await user.click(screen.getByRole("button", { name: "Approve summary" }));
  const sheet = await screen.findByRole("dialog", { name: "Approve summary" });
  facade.reply({
    ReadHubTeam: () => teamResult({ reviews: [{ ...support, status: "approved" }] }),
    DownloadHubSummary: () => ({ state: "completed", transfer_state: "completed", path: "/Users/rui/support-summary.json" }),
  });
  await user.click(within(sheet).getByRole("button", { name: "Approve summary" }));
  await waitFor(() => expect(facade.callsTo("PostHubReview")).toHaveLength(1));
  expect(facade.oneCall("PostHubReview")[0]).toMatchObject({ kind: "support-approval", parent: "support-ask", evidence: "e".repeat(64), release: "f".repeat(64), text: "support" });
  await user.click(await screen.findByRole("button", { name: "Download summary" }));
  expect(facade.oneCall("DownloadHubSummary")[0]).toEqual({ project: "cardio-study", digest: "e".repeat(64) });
  expect(await screen.findByText("Saved /Users/rui/support-summary.json")).toBeTruthy();
});

test("a support summary review read while the team is still being read shows the summary, not the busy answer", async () => {
  const user = userEvent.setup();
  const support: HubTeamResult["reviews"][number] = {
    id: "support-ask", support: true, item: "Support summary", requested_by: "ana", recipient: "rui", requested: "2026-09-20T10:00:00Z", updated: "2026-09-20T10:00:00Z",
    status: "requested", evidence: "e".repeat(64), release: "f".repeat(64), to_me: true, discussion: [], requests: [{ id: "support-ask", evidence: "e".repeat(64), release: "f".repeat(64) }],
  };
  let asked = 0;
  const facade = installFacade({
    HubStatus: () => signedIn(),
    ReadHubTeam: () => teamResult({ reviews: [support] }),
    ReadHubSupportSummary: () => (++asked === 1 ? { state: "busy", reason: "another operation is already running" } : { state: "completed", source_kind: "retained-packet", outcome: "assertion_failure" }),
  });
  render(<HubPanel />);
  await openTab(user, "Reviews");
  await user.dblClick(await within(await screen.findByRole("table", { name: "Reviews" })).findByText("Support summary"));
  expect(await screen.findByText("Check failed")).toBeTruthy();
  expect(screen.queryByText("another operation is already running")).toBeNull();
  expect(facade.callsTo("ReadHubSupportSummary")).toHaveLength(2);
});

test("a signed-in team whose hub cannot be reached says so instead of listing no projects silently", async () => {
  installFacade({
    HubStatus: () => signedIn({ projects: [{ project: "cardio-study", authorized: false, reason: "hub connection failed" }] }),
  });
  render(<HubPanel />);
  expect(await screen.findByText("No projects you can open")).toBeTruthy();
  expect(screen.getByRole("alert").textContent).toBe("hub connection failed");
});

async function openAdmin(user: ReturnType<typeof userEvent.setup>, task: string) {
  await user.click(await screen.findByRole("button", { name: "Account" }));
  await user.click(screen.getByRole("menuitem", { name: "Administrator setup" }));
  await user.click(await screen.findByRole("button", { name: task }));
}

test("Members add and change roles as a host setup over a local policy copy and remove through the hub", async () => {
  const user = userEvent.setup();
  let members = [
    { subject: "ana", role: "analyst" },
    { subject: "rui", role: "reviewer" },
  ];
  const facade = installFacade({
    HubStatus: () => signedIn(),
    ReadHubTeam: () => teamResult(),
    ListHubMembers: () => ({ state: "completed", members }),
    ChooseHubLocalCopy: () => ({ state: "completed", kind: "access-policy", paths: ["/Users/rui/access.json"] }),
    ExportHubSetup: () => ({ state: "completed", path: "/Users/rui/Desktop/access.json" }),
    PostHubLifecycle: () => ({ state: "completed", head: 2 }),
  });
  const admin = installHubAdmin({
    PrepareMembership: async (request) => ({
      state: "completed",
      project: request.project,
      subject: request.subject,
      after: request.role,
      command: "sudo install -o readmit-hub -g readmit-hub -m 0600 access.json '/etc/readmit-hub/.access.json.new' && sudo mv -f '/etc/readmit-hub/.access.json.new' '/etc/readmit-hub/access.json'",
      prerequisites: ["Copy the exported access.json to the hub host and run the command from the folder that holds it."],
      policy: "{}\n",
      policy_name: "access.json",
    }),
  });
  render(<HubPanel />);
  await openAdmin(user, "Members");
  expect(await within(await screen.findByRole("table", { name: "Members" })).findByText("ana")).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Add member" }));
  let sheet = await screen.findByRole("dialog", { name: "Add member" });
  await user.click(within(sheet).getByRole("button", { name: "Choose…" }));
  await user.type(within(sheet).getByLabelText("Person"), "oli");
  await user.selectOptions(within(sheet).getByLabelText("Role"), "reviewer");
  await user.click(within(sheet).getByRole("button", { name: "Prepare" }));
  expect(admin.callsTo("PrepareMembership")[0]!.args[0]).toMatchObject({ operation: "add-member", policy_copy: "/Users/rui/access.json", policy_path: "/etc/readmit-hub/access.json", project: "cardio-study", subject: "oli", role: "reviewer" });
  sheet = await screen.findByRole("dialog", { name: "Add member" });
  expect(within(sheet).getByText(/sudo mv -f/)).toBeTruthy();
  await user.click(within(sheet).getByRole("button", { name: "Export setup" }));
  expect(facade.oneCall("ExportHubSetup")[0]).toEqual({ name: "access.json", content: "{}\n" });
  expect(await within(sheet).findByText("Setup ready · installation required")).toBeTruthy();
  await user.click(within(sheet).getByRole("button", { name: "Close add member" }));
  // Exporting changed nobody's access: the member shows as setup ready
  // until the hub reads the installed policy.
  const table = screen.getByRole("table", { name: "Members" });
  expect(within(table).getByText("Setup ready · Reviewer")).toBeTruthy();
  expect(facade.callsTo("PostHubLifecycle")).toHaveLength(0);

  await user.click(within(table).getByText("ana"));
  await user.click(screen.getByRole("button", { name: "More member actions" }));
  await user.click(screen.getByRole("menuitem", { name: "Remove member" }));
  sheet = await screen.findByRole("dialog", { name: "Remove member" });
  expect(within(sheet).getByText("Removes ana from cardio-study; their downloaded copies stay with them.")).toBeTruthy();
  await user.type(within(sheet).getByLabelText("Reason"), "Left the study");
  members = [...members, { subject: "oli", role: "reviewer" }];
  await user.click(within(sheet).getByRole("button", { name: "Remove" }));
  await waitFor(() => expect(facade.callsTo("PostHubLifecycle")).toHaveLength(1));
  expect(facade.oneCall("PostHubLifecycle")[0]).toMatchObject({ kind: "remove-user", subject: "ana", reason: "Left the study", expected: 1 });
  await waitFor(() => expect(within(screen.getByRole("table", { name: "Members" })).queryByText("Setup ready · Reviewer")).toBeNull());
});

test("Retention is reviewed per current file, never shortens a date and reports files that did not change", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    HubStatus: () => signedIn(),
    ReadHubTeam: () => teamResult(),
    PreviewHubRetention: () => ({
      state: "completed",
      until: "2028-09-20T10:00:00Z",
      rows: [
        { digest: "b".repeat(64), type: "revision", current: "2027-01-01T00:00:00Z", proposed: "2028-09-20T10:00:00Z", change: "extends" },
        { digest: "c".repeat(64), type: "file", current: "2099-01-01T00:00:00Z", change: "unchanged" },
      ],
    }),
    ApplyHubRetention: () => ({
      state: "failed",
      reason: "1 of 1 files were not changed",
      until: "2028-09-20T10:00:00Z",
      rows: [{ digest: "b".repeat(64), type: "revision", proposed: "2028-09-20T10:00:00Z", change: "failed", reason: "access refused" }],
    }),
  });
  render(<HubPanel />);
  await openAdmin(user, "Retention");
  await user.click(await screen.findByRole("button", { name: "Edit retention" }));
  let sheet = await screen.findByRole("dialog", { name: "Edit retention" });
  await user.clear(within(sheet).getByLabelText("Keep for"));
  await user.type(within(sheet).getByLabelText("Keep for"), "2");
  await user.click(within(sheet).getByRole("button", { name: "Review" }));
  expect(facade.oneCall("PreviewHubRetention")[0]).toMatchObject({ project: "cardio-study", count: 2, unit: "years", scope: "all" });
  sheet = await screen.findByRole("dialog", { name: "Review retention" });
  expect(within(sheet).getByText("Unchanged")).toBeTruthy();
  expect(within(sheet).getByText("Current files only; nothing is deleted, and downloaded copies are unaffected.")).toBeTruthy();
  await user.type(within(sheet).getByLabelText("Reason"), "Study close-out");
  await user.click(within(sheet).getByRole("button", { name: "Save" }));
  expect(facade.oneCall("ApplyHubRetention")[0]).toMatchObject({ digests: ["b".repeat(64)], reason: "Study close-out", until: "2028-09-20T10:00:00Z" });
  sheet = await screen.findByRole("dialog", { name: "Retention" });
  expect(within(sheet).getByRole("alert").textContent).toBe("1 of 1 files were not changed");
  expect(within(sheet).getByText("Not changed")).toBeTruthy();
});

test("the audit log filters the project's history and exports it to a new file", async () => {
  const user = userEvent.setup();
  let recorded = false;
  let refresh: () => void = () => undefined;
  const refreshed = new Promise<void>((resolve) => { refresh = resolve; });
  let saves = 0;
  const facade = installFacade({
    HubStatus: () => signedIn(),
    ReadHubTeam: async () => { if (recorded) await refreshed; return teamResult(); },
    PostHubLifecycle: () => { recorded = true; return { state: "completed", audit: { schema: "readmit-hub-audit/v1", project: "cardio-study", lifecycle: [], review_head: 2, reviews: [], warning: "" } }; },
    SaveHubAudit: () => ++saves === 1 ? { state: "cancelled" } : { state: "completed", path: "/Users/rui/audit.json" },
  });
  render(<HubPanel />);
  await openAdmin(user, "Audit log");
  const table = await screen.findByRole("table", { name: "Audit log" });
  expect(within(table).getByText("Requested review")).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Filter" }));
  const filter = await screen.findByRole("dialog", { name: "Filter audit log" });
  await user.selectOptions(within(filter).getByLabelText("Action"), "revision");
  await user.click(within(filter).getByRole("button", { name: "Apply" }));
  expect(within(screen.getByRole("table", { name: "Audit log" })).queryByText("Requested review")).toBeNull();
  await user.click(screen.getByRole("button", { name: "Export audit" }));
  const sheet = await screen.findByRole("dialog", { name: "Export audit" });
  await user.type(within(sheet).getByLabelText("Reason"), "Quarterly review");
  await user.click(within(sheet).getByRole("button", { name: "Export audit" }));
  await waitFor(() => expect(facade.callsTo("ReadHubTeam")).toHaveLength(2));
  expect(facade.callsTo("SaveHubAudit")).toHaveLength(0);
  refresh();
  await within(sheet).findByText("No file was named. Choose a destination to save the recorded export.");
  await user.click(within(sheet).getByRole("button", { name: "Export audit" }));
  expect(await within(sheet).findByText("Saved /Users/rui/audit.json")).toBeTruthy();
  expect(facade.oneCall("PostHubLifecycle")[0]).toMatchObject({ kind: "audit-export", reason: "Quarterly review" });
  expect(facade.callsTo("SaveHubAudit")).toHaveLength(2);
  expect(facade.callsTo("SaveHubAudit")[1]!.args[0]).toBe("cardio-study");
});

test("revisions are submitted against their base and a conflict is resolved part by part as one new revision", async () => {
  const user = userEvent.setup();
  const conflicted = teamResult({
    resources: [
      {
        resource: "booking-rules",
        revisions: [
          { id: "rev-1", artifact: "b".repeat(64), actor: "ana", at: "2026-09-19T10:00:00Z", reason: "first", parents: [] },
          { id: "rev-2", artifact: "c".repeat(64), actor: "rui", at: "2026-09-20T10:00:00Z", reason: "mine", parents: ["rev-1"] },
          { id: "rev-3", artifact: "d".repeat(64), actor: "ana", at: "2026-09-20T11:00:00Z", reason: "theirs", parents: ["rev-1"] },
        ],
        tips: ["rev-2", "rev-3"],
      },
    ],
  });
  const facade = installFacade({
    HubStatus: () => signedIn(),
    ReadHubTeam: () => conflicted,
    EditorDrafts: () => ({ state: "completed", drafts: [] }),
    OpenHubConflict: () => ({
      state: "completed",
      scope: "conflict-1",
      tips: conflicted.resources[0]!.revisions.slice(1),
      base: conflicted.resources[0]!.revisions[0]!,
      yours: "rev-2",
      current: "rev-3",
      text: true,
      hunks: [{ lines: ["a\n"] }, { conflict: true, base: ["b\n"], yours: ["B1\n"], current: ["B2\n"] }],
    }),
    PrepareAction: (request) => ({ state: "completed", context: request.context, review: review(request.action, { requirements: ["rationale"], team: { project: "cardio-study", resource: "booking-rules", conflict_scope: "conflict-1", size: 6, tips: conflicted.resources[0]!.revisions.slice(1) } }) }),
    ExecuteReviewedAction: (request) => ({ state: "completed", context: request.context, outcome: "completed", replayed: false, team: { project: "cardio-study", digest: "e".repeat(64), revision: "resolve-1" } }),
  });
  facade.reply({
    ChooseHubLocalCopy: () => ({ state: "completed", kind: "file", paths: ["/Users/rui/rules.txt"] }),
    SaveHubOfflineDraft: () => ({ state: "completed", drafts: [] }),
  });
  render(<HubPanel workspace="/Users/rui/project" />);
  await openAdmin(user, "Revisions");
  await user.click(await within(await screen.findByRole("table", { name: "Resources" })).findByText("booking-rules"));
  expect(screen.getByText("Conflict")).toBeTruthy();
  // Create revision continues the base it names; Save draft keeps it on this
  // computer only.
  await user.click(screen.getByRole("button", { name: "Create revision" }));
  let revise = await screen.findByRole("dialog", { name: "Create revision" });
  await user.click(within(revise).getByRole("button", { name: "Choose…" }));
  await user.type(within(revise).getByLabelText("Change"), "Later start");
  await user.click(within(revise).getByRole("button", { name: "Save draft" }));
  expect(await within(revise).findByText("Draft saved on this computer")).toBeTruthy();
  expect(facade.oneCall("SaveHubOfflineDraft")[0]).toMatchObject({ project: "cardio-study", resource: "booking-rules", parent_tips: ["rev-2"], local_path: "/Users/rui/rules.txt" });
  await waitFor(() => expect(facade.callsTo("PrepareAction").length).toBeGreaterThan(0));
  expect(facade.callsTo("PrepareAction").at(-1)!.args[0]).toMatchObject({ action: "team.revision", team: { resource: "booking-rules", base: "rev-2", source: "/Users/rui/rules.txt" } });
  await user.click(await within(revise).findByRole("button", { name: "Submit revision" }));
  await waitFor(() => expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(1));
  revise = await screen.findByRole("dialog", { name: "Create revision" });
  await user.click(within(revise).getByRole("button", { name: "Done" }));
  const prepared = facade.callsTo("PrepareAction").length;
  await user.click(screen.getByRole("button", { name: "Resolve" }));
  const sheet = await screen.findByRole("dialog", { name: "Resolve" });
  expect(await within(sheet).findByText("B1")).toBeTruthy();
  expect(facade.callsTo("PrepareAction")).toHaveLength(prepared);
  await user.click(within(within(sheet).getByRole("group", { name: "Conflict 1" })).getByRole("radio", { name: /Current/ }));
  await user.type(within(sheet).getByLabelText("Change"), "Kept the longer slot");
  await waitFor(() => expect(facade.callsTo("PrepareAction").length).toBeGreaterThan(prepared));
  expect(facade.callsTo("PrepareAction").at(-1)!.args[0]).toMatchObject({ action: "team.resolve", team: { project: "cardio-study", resource: "booking-rules", resolution: { choices: ["current"] } } });
  await user.click(await within(sheet).findByRole("button", { name: "Save resolution" }));
  await waitFor(() => expect(facade.callsTo("ExecuteReviewedAction")).toHaveLength(2));
  expect(facade.callsTo("ExecuteReviewedAction")[1]!.args[0]).toMatchObject({ decisions: { rationale: "Kept the longer slot" } });
});

test("host tasks preview a command from local copies and export the setup; the operator hub connects on its own", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    HubStatus: () => ({ state: "empty", connected: false, authenticated: false }),
    ChooseHubLocalCopy: () => ({ state: "completed", kind: "config", paths: ["/Users/rui/copies/config.json"] }),
    ExportHubSetup: () => ({ state: "completed", path: "/Users/rui/migrate.sh" }),
    ChooseOperatorHubConfig: () => ({ state: "completed", connected: false, authenticated: false, config_path: "/Users/rui/operator.json", hub_url: "https://store.example:8443" }),
    ConnectOperatorHub: () => ({ state: "completed", connected: true, authenticated: false, config_path: "/Users/rui/operator.json", hub_url: "https://store.example:8443" }),
    ReadOperatorHubArtifact: () => ({ state: "completed", path: "/Users/rui/object.bin", digest: "a".repeat(64) }),
  });
  const admin = installHubAdmin({
    Preview: async (request) => ({ state: "completed", command: `readmit-hub -config '${request.config_path}' ${request.operation}`, prerequisites: ["Stop the hub service first."], touches: ["Applies metadata migrations."] }),
  });
  render(<HubPanel />);
  await user.click(await screen.findByRole("button", { name: "More team actions" }));
  await user.click(screen.getByRole("menuitem", { name: "Administrator setup" }));
  expect(screen.queryByRole("button", { name: "Members" })).toBeNull();
  await user.click(screen.getByRole("button", { name: "Host tasks" }));
  await user.click(screen.getByRole("button", { name: "Migrate metadata" }));
  let sheet = await screen.findByRole("dialog", { name: "Migrate metadata" });
  await user.click(within(sheet).getByRole("button", { name: "Choose…" }));
  await user.click(within(sheet).getByRole("button", { name: "Preview command" }));
  expect(admin.callsTo("Preview")[0]!.args[0]).toMatchObject({ operation: "migrate", config_copy: "/Users/rui/copies/config.json", config_path: "/etc/readmit-hub/config.json" });
  sheet = await screen.findByRole("dialog", { name: "Migrate metadata" });
  expect(within(sheet).getByText("readmit-hub -config '/etc/readmit-hub/config.json' migrate")).toBeTruthy();
  await user.click(within(sheet).getByRole("button", { name: "Export setup" }));
  expect(facade.oneCall("ExportHubSetup")[0]!.content).toContain("readmit-hub -config '/etc/readmit-hub/config.json' migrate");
  await user.click(within(sheet).getByRole("button", { name: "Close migrate metadata" }));

  await user.click(screen.getByRole("button", { name: "Back to administrator setup" }));
  await user.click(screen.getByRole("button", { name: "Operator hub" }));
  await user.click(screen.getByRole("button", { name: "Choose configuration…" }));
  await user.click(await screen.findByRole("button", { name: "Connect" }));
  expect(await screen.findByText("Every holder of this hub's client certificates can read and store every file here.")).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Download by hash" }));
  const read = await screen.findByRole("dialog", { name: "Download by hash" });
  await user.type(within(read).getByLabelText("SHA-256"), "a".repeat(64));
  await user.click(within(read).getByRole("button", { name: "Download" }));
  expect(facade.oneCall("ReadOperatorHubArtifact")[0]).toBe("a".repeat(64));
  expect(await screen.findByText(/Saved \/Users\/rui\/object.bin/)).toBeTruthy();
  expect(facade.callsTo("ConnectHub")).toHaveLength(0);
  facade.reply({ DisconnectOperatorHub: () => ({ state: "completed", connected: false, authenticated: false, config_path: "/Users/rui/operator.json" }) });
  await user.click(screen.getByRole("button", { name: "Disconnect" }));
  expect(await screen.findByRole("button", { name: "Connect" })).toBeTruthy();
});

const HOST_OPERATIONS = ["migrate", "check", "backup", "verify-backup", "restore", "schedule-init", "schedule-pin"] as const;
const HOST_LABELS: Record<(typeof HOST_OPERATIONS)[number], string> = {
  migrate: "Migrate metadata",
  check: "Check readiness",
  backup: "Create backup",
  "verify-backup": "Verify backup",
  restore: "Restore backup",
  "schedule-init": "Initialize schedules",
  "schedule-pin": "Pin inputs",
};

async function openHostTask(user: ReturnType<typeof userEvent.setup>, label: string, handlers: Parameters<typeof installHubAdmin>[0]) {
  installFacade({
    HubStatus: () => ({ state: "empty", connected: false, authenticated: false }),
    ChooseHubLocalCopy: (kind) => ({ state: "completed", kind, paths: [`/Users/rui/copies/${kind}`] }),
  });
  const admin = installHubAdmin(handlers);
  render(<HubPanel />);
  await user.click(await screen.findByRole("button", { name: "More team actions" }));
  await user.click(screen.getByRole("menuitem", { name: "Administrator setup" }));
  await user.click(screen.getByRole("button", { name: "Host tasks" }));
  await user.click(screen.getByRole("button", { name: label }));
  return { admin, sheet: await screen.findByRole("dialog", { name: label }) };
}

async function previewHostTask(operation: (typeof HOST_OPERATIONS)[number]) {
  const user = userEvent.setup();
  const { admin, sheet } = await openHostTask(user, HOST_LABELS[operation], { Preview: async (request) => ({ state: "completed", command: `readmit-hub ${request.operation}`, prerequisites: [], touches: [] }) });
  for (const choose of within(sheet).getAllByRole("button", { name: "Choose…" })) await user.click(choose);
  const directory = sheet.querySelector<HTMLInputElement>("#host-directory");
  if (directory) await user.type(directory, "/srv/readmit-hub/backups/new");
  await user.click(within(sheet).getByRole("button", { name: "Preview command" }));
  await waitFor(() => expect(admin.callsTo("Preview")).toHaveLength(1));
  expect(admin.callsTo("Preview")[0]!.args[0]).toMatchObject({ operation, config_copy: "/Users/rui/copies/config" });
  expect(await screen.findByText(`readmit-hub ${operation}`)).toBeTruthy();
}

test("host task migrate previews its command through the bound Go preview", () => previewHostTask("migrate"));
test("host task check previews its command through the bound Go preview", () => previewHostTask("check"));
test("host task backup previews its command through the bound Go preview", () => previewHostTask("backup"));
test("host task verify-backup previews its command through the bound Go preview", () => previewHostTask("verify-backup"));
test("host task restore previews its command through the bound Go preview", () => previewHostTask("restore"));
test("host task schedule-init previews its command through the bound Go preview", () => previewHostTask("schedule-init"));
test("host task schedule-pin previews its command through the bound Go preview", () => previewHostTask("schedule-pin"));

test("a host task preview is stopped from its sheet and shows no command", async () => {
  const user = userEvent.setup();
  let finish: (result: { state: "cancelled"; reason: string }) => void = () => {};
  const { admin, sheet } = await openHostTask(user, "Migrate metadata", {
    Preview: () => new Promise((resolve) => (finish = resolve)),
    CancelPreview: async () => {
      finish({ state: "cancelled", reason: "administration preview cancelled" });
      return { state: "busy", reason: "cancellation requested" };
    },
  });
  await user.click(within(sheet).getByRole("button", { name: "Choose…" }));
  await user.click(within(sheet).getByRole("button", { name: "Preview command" }));
  await user.click(await within(sheet).findByRole("button", { name: "Stop" }));
  expect(admin.callsTo("CancelPreview")).toHaveLength(1);
  expect(await within(sheet).findByText("administration preview cancelled")).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Copy command" })).toBeNull();
});

test("a support summary review read while the team is still being read shows the summary, not the busy answer", async () => {
  const user = userEvent.setup();
  const support: HubTeamResult["reviews"][number] = {
    id: "support-ask", support: true, item: "Support summary", requested_by: "ana", recipient: "rui", requested: "2026-09-20T10:00:00Z", updated: "2026-09-20T10:00:00Z",
    status: "requested", evidence: "e".repeat(64), release: "f".repeat(64), to_me: true, discussion: [], requests: [{ id: "support-ask", evidence: "e".repeat(64), release: "f".repeat(64) }],
  };
  let asked = 0;
  const facade = installFacade({
    HubStatus: () => signedIn(),
    ReadHubTeam: () => teamResult({ reviews: [support] }),
    ReadHubSupportSummary: () => (++asked === 1 ? { state: "busy", reason: "another operation is already running" } : { state: "completed", source_kind: "retained-packet", outcome: "assertion_failure" }),
  });
  render(<HubPanel />);
  await openTab(user, "Reviews");
  await user.dblClick(await within(await screen.findByRole("table", { name: "Reviews" })).findByText("Support summary"));
  expect(await screen.findByText("Check failed")).toBeTruthy();
  expect(screen.queryByText("another operation is already running")).toBeNull();
  expect(facade.callsTo("ReadHubSupportSummary")).toHaveLength(2);
});
