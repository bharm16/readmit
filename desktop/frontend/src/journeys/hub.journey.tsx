// Team work on a real customer hub: the checkout's readmit-hub serving over
// mutual TLS on loopback, its store in a disposable PostgreSQL cluster, and a
// customer identity provider. A person connects the team their operator
// configured in Settings › Team, signs in through the identity provider, and
// publishes evidence to the project. A colleague, in their own window on
// their own machine, reviews the same summary at the same moment; the
// person's comment, made against the history they had loaded, is refused as
// a conflict rather than silently ordered, and is recorded once they look
// again. Both windows then read one history, each entry under the identity
// the hub authenticated.
//
// Publishing is refused until a license is activated, and by the hub for a
// role that may not write; a stored copy that no longer matches its digest is
// refused, an expired session is ended by the window, a stopped hub is
// reported and not retried, and signing out ends the session. A support
// summary the team approved downloads byte for byte, and the person's
// activity addressed to them and its search read what the hub recorded. An
// operator stores and reads artifacts by digest on an operator-only hub.
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { UserEvent } from "@testing-library/user-event";
import { Journey, press, region } from "../testkit/journey";
import type { Hub } from "../testkit/hub.js";
import { goTo, openView, page } from "../testkit/navigation";
import { windowWidth } from "../testkit/window";
import { activateLicense, pressServed } from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

const PROJECT = "scheduling";

/** One category of Settings. Settings returns to the page of it last
 * shown, so a subpage is left first. */
async function settings(user: UserEvent, category: string) {
  await goTo(user, "Settings");
  const back = page().queryByRole("button", { name: "Back to settings" });
  if (back) await press(user, back);
  await openView(user, category);
}

/** Settings › Team, read afresh: the page reads the hub's status, and the
 * project through the session, each time it is shown. */
async function teamPage(user: UserEvent) {
  await settings(user, "General");
  await settings(user, "Team");
  await journey.settled();
  return within(await screen.findByRole("region", { name: "Team" }));
}

/** Connect team: the configuration the hub's operator supplied, chosen in
 * the host's file dialog, saved under the name its address offers. Saving
 * connects nothing. */
async function connectTeam(user: UserEvent, hub: Hub) {
  const panel = await teamPage(user);
  await press(user, await panel.findByRole("button", { name: "Connect team" }));
  const sheet = within(await screen.findByRole("dialog", { name: "Connect team" }));
  await journey.chooseFiles([`${hub.clientConfigFolder}/hub-client.json`], "Choose your team's configuration");
  await press(user, sheet.getByRole("button", { name: "Choose file…" }));
  expect(await sheet.findByText(PROJECT)).toBeTruthy();
  await press(user, sheet.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Connect team" })).toBeNull());
  expect(await within(region("Team")).findByText("Not connected")).toBeTruthy();
}

/** Sign in: the one flow checks the setup, connects and opens the identity
 * provider's page, which the person's browser completes as subject, for a
 * session of lifetimeSeconds when given. */
async function signIn(user: UserEvent, hub: Hub, subject: string, lifetimeSeconds?: number) {
  await press(user, await within(region("Team")).findByRole("button", { name: "Sign in" }));
  const flow = within(await screen.findByRole("dialog", { name: "Sign in" }));
  const login = await flow.findByRole("link", { name: "Open sign-in page" }, { timeout: 20_000 });
  expect(await hub.signIn(login.getAttribute("href") ?? "", subject, lifetimeSeconds)).toBe(200);
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Sign in" })).toBeNull(), { timeout: 20_000 });
  expect(await within(region("Team")).findByRole("button", { name: "Account" })).toBeTruthy();
}

/** One of the signed-in project's views: Activity, Files or Reviews. */
async function teamView(user: UserEvent, view: "Activity" | "Files" | "Reviews") {
  const project = within(await screen.findByRole("region", { name: `${PROJECT} activity` }));
  await press(user, project.getByRole("tab", { name: view }));
  return project;
}

/** One item of the signed-in person's Account menu. */
async function account(user: UserEvent, item: string) {
  await press(user, within(region("Team")).getByRole("button", { name: "Account" }));
  await press(user, await screen.findByRole("menuitem", { name: item }));
}

/** Files › Upload: the file chosen in the host's dialog, and its review. */
async function uploadReview(user: UserEvent, file: string) {
  const files = await teamView(user, "Files");
  await journey.chooseFiles([journey.path(file)], "Choose a file");
  const prepared = journey.callsTo("PrepareAction").length;
  await press(user, files.getByRole("button", { name: "Upload" }));
  const sheet = within(await screen.findByRole("dialog", { name: "Upload" }));
  await waitFor(() => expect(journey.callsTo("PrepareAction")[prepared]?.settled).toBe(true));
  return sheet;
}

/** Downloads the selected file of Files into a new file named in the save
 * dialog, and returns the facade's answer. */
async function download(user: UserEvent, name: string, saveAs: string) {
  const files = await teamView(user, "Files");
  await press(user, await files.findByRole("row", { name }));
  // A file the project's records do not name is offered as a file.
  await journey.nameNewFolder(journey.path(saveAs), "Download file");
  const asked = journey.callsTo("DownloadHubFile").length;
  await press(user, files.getByRole("button", { name: "Download" }));
  await waitFor(() => expect(journey.callsTo("DownloadHubFile")[asked]?.settled).toBe(true));
  return journey.callsTo("DownloadHubFile")[asked]?.result as { state: string; reason?: string; digest?: string };
}

/** A colleague's own window, licensed, connected to the hub and signed in
 * as subject through the same identity provider. */
async function colleagueSignedIn(hub: Hub, subject: string) {
  const colleague = journey.colleague(subject);
  await colleague.call("SelectOperationPolicy", `${journey.provisionLicense(`colleagues/${subject}-license`)}/operation-policy.json`);
  await colleague.call("SelectHubConfig", `${hub.clientConfigFolder}/hub-client.json`);
  await colleague.call("ConnectHub");
  const flow = await colleague.call("StartHubAuth");
  const signedIn = await colleague.call("CompleteHubAuth", hub.issueCode(subject), new URL(flow.auth_url ?? "").searchParams.get("state") ?? "");
  expect(signedIn).toMatchObject({ state: "completed", authenticated: true, subject });
  return colleague;
}

/** The synthetic support summary a published support bundle holds, bound to
 * the sharing policy whose digest it names. */
function supportSummary(policyDigest: string): string {
  return JSON.stringify({
    schema: "readmit-support-summary/v1",
    source_kind: "retained-packet",
    source_identity: "1".repeat(64),
    input_commitment: "2".repeat(64),
    spec_identity: "3".repeat(64),
    policy_identity: policyDigest,
    outcome: "assertion_failure",
    external_equivalence: "declined",
    scope:
      "Diagnostic metadata only; no evidence payload, disclosure certification, source authentication or regression-equivalence proof. Original evidence and private mappings remain customer-local.",
  });
}

/** The project's administrator announces its sharing policy from their own
 * machine; a teammate's value-free support summary under it is in the folder
 * the person opens; the person signs in as analyst and asks the reviewer to
 * approve the summary. Returns the summary, its digest and the request. */
async function supportRequested(user: UserEvent, hub: Hub) {
  const admin = await colleagueSignedIn(hub, "admin");
  journey.writeFile(
    "colleagues/admin/team/sharing-policy.json",
    JSON.stringify({ schema: "readmit-sharing-policy/v1", support: true, destinations: ["customer-hub-download"], max_bytes: 4096 }),
  );
  const policyDigest = journey.digest("colleagues/admin/team/sharing-policy.json");
  expect(
    await admin.call("PostHubSupportReview", {
      project: PROJECT,
      workspace: journey.path("colleagues/admin/team"),
      entry: "sharing-policy.json",
      kind: "support-policy",
      id: "sharing-policy-1",
      recipient: "",
    }),
  ).toMatchObject({ state: "completed" });
  const summary = supportSummary(policyDigest);
  journey.writeFile("support-work/published-support/support.json", summary);
  const summaryDigest = journey.digest("support-work/published-support/support.json");

  await journey.launch();
  await activateLicense(user, journey);
  await goTo(user, "Projects");
  await journey.chooseFolder(journey.path("support-work"), "Open project");
  // The activation's own reads finish before the folder is opened.
  await journey.settled();
  await pressServed(user, journey, page().getByRole("button", { name: "Open" }), "SelectWorkspace");
  expect(journey.callsTo("SelectWorkspace").at(-1)?.result).toMatchObject({ state: "completed" });
  await connectTeam(user, hub);
  await signIn(user, hub, "analyst");

  // The person asks the reviewer, from the hub's list of reviewers, to
  // approve the summary's exact bytes.
  const reviews = await teamView(user, "Reviews");
  await press(user, reviews.getByRole("button", { name: "Request review" }));
  // With a folder open, a suite version is offered first.
  await user.selectOptions(within(await screen.findByRole("dialog", { name: "Request review" })).getByLabelText("Item"), "support");
  const request = within(await screen.findByRole("dialog", { name: "Request approval" }));
  await waitFor(() => expect((request.getByLabelText("Reviewer") as HTMLSelectElement).value).toBe("reviewer"));
  expect((request.getByLabelText("Summary") as HTMLSelectElement).value).toBe("published-support");
  await press(user, request.getByRole("button", { name: "Request approval" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Request approval" })).toBeNull());
  const requested = journey.callsTo("PostHubSupportReview").at(-1)!;
  expect(requested.result).toMatchObject({ state: "completed" });
  const requestId = (requested.args[0] as { id: string }).id;
  expect(requestId).toMatch(/^request-/);
  const row = await within(await reviews.findByRole("table", { name: "Reviews" })).findByRole("row", { name: "Support summary" });
  expect(row.textContent).toContain("analyst");
  return { summary, summaryDigest, requestId };
}

/** Opens the one support summary review from Reviews. */
async function openSupportReview(user: UserEvent) {
  const reviews = await teamView(user, "Reviews");
  // Reviews keeps the review last open.
  if (reviews.queryByRole("heading", { name: "Review" })) return reviews;
  await user.dblClick(await within(await reviews.findByRole("table", { name: "Reviews" })).findByRole("row", { name: "Support summary" }));
  await screen.findByRole("heading", { name: "Review" });
  return reviews;
}

test(
  "two people review the same evidence on a real hub: the decision made against a stale history is refused as a conflict and recorded once renewed",
  async (context) => {
    // A real hub needs a PostgreSQL installation to create its cluster from.
    if (!Journey.hubAvailable) context.skip();
    const user = userEvent.setup();
    const hub = await journey.startHub(PROJECT, [
      { subject: "analyst", role: "analyst" },
      { subject: "reviewer", role: "reviewer" },
      { subject: "admin", role: "admin" },
    ]);
    const { summaryDigest, requestId } = await supportRequested(user, hub);

    // A sign-in stopped before the browser returns is cancelled, and signing
    // in again completes.
    await account(user, "Sign out");
    await press(user, await within(region("Team")).findByRole("button", { name: "Sign in" }));
    const flow = within(await screen.findByRole("dialog", { name: "Sign in" }));
    await flow.findByRole("link", { name: "Open sign-in page" }, { timeout: 20_000 });
    await press(user, flow.getByRole("button", { name: "Stop" }));
    await waitFor(() => expect(journey.callsTo("CompleteHubAuth").at(-1)?.result).toMatchObject({ state: "cancelled" }));
    await press(user, flow.getByRole("button", { name: "Close sign in" }));
    await signIn(user, hub, "analyst");

    // The person opens the review; the colleague comments on it from their
    // own window before the person does, so the person's comment names a
    // history the hub has moved past.
    const reviews = await openSupportReview(user);
    const reviewer = await colleagueSignedIn(hub, "reviewer");
    const head = (await reviewer.call("ListHubReviews", PROJECT)).head ?? 0;
    expect(
      await reviewer.call("PostHubReview", { project: PROJECT, id: "reviewer-confirms", expected: head, kind: "comment", evidence: summaryDigest, parent: requestId, recipient: "", text: "Confirmed on the lab fixture.", release: "" }),
    ).toMatchObject({ state: "completed", head: head + 1 });
    await press(user, reviews.getByRole("button", { name: "Comment" }));
    const comment = within(await screen.findByRole("dialog", { name: "Comment" }));
    await user.type(comment.getByLabelText("Comment"), "The reschedule is matched on the filler identifier.");
    await press(user, comment.getByRole("button", { name: "Comment" }));
    expect(await comment.findByText("Someone else changed this project meanwhile. Look at it again before deciding.")).toBeTruthy();
    expect(journey.callsTo("PostHubReview").at(-1)?.result).toMatchObject({ state: "failed" });

    // Read again, the comment is recorded after the colleague's as a new
    // command, and both windows read the one history the hub keeps, each
    // entry under the identity the hub authenticated.
    await press(user, comment.getByRole("button", { name: "Comment" }));
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Comment" })).toBeNull());
    const posted = journey.callsTo("PostHubReview").map((call) => call.args[0] as { id: string; expected: number });
    expect(posted.at(-1)!.expected).toBe(head + 1);
    expect(posted.at(-1)!.id).not.toBe(posted.at(-2)!.id);
    await waitFor(() =>
      expect(Array.from(reviews.getByText("Discussion").nextElementSibling!.querySelectorAll("li")).map((item) => item.textContent ?? "")).toEqual([
        expect.stringMatching(/^reviewer · Commented · .*Confirmed on the lab fixture\.$/),
        expect.stringMatching(/^analyst · Commented · .*The reschedule is matched on the filler identifier\.$/),
      ]),
    );
    const seen = await reviewer.call("ListHubReviews", PROJECT);
    expect(seen.head).toBe(head + 2);
    expect(seen.events?.slice(-2).map((event) => event.actor)).toEqual(["reviewer", "analyst"]);
  },
);

test(
  "publishing to a real hub needs an activated license and a role that may write, and a damaged stored copy, an expired session, a stopped hub and a disconnection are each reported, never retried",
  async (context) => {
    // A real hub needs a PostgreSQL installation to create its cluster from.
    if (!Journey.hubAvailable) context.skip();
    const user = userEvent.setup();
    const hub = await journey.startHub(PROJECT, [
      { subject: "analyst", role: "analyst" },
      { subject: "viewer", role: "viewer" },
    ]);
    journey.writeFile("handover/reschedule-summary.txt", "Reschedule refused by the downstream system; synthetic evidence.\n");
    const digest = journey.digest("handover/reschedule-summary.txt");
    journey.makeFolder("downloads");
    await journey.launch();

    // A dismissed dialog chooses nothing: no team is configured.
    let panel = await teamPage(user);
    await press(user, await panel.findByRole("button", { name: "Connect team" }));
    const sheet = within(await screen.findByRole("dialog", { name: "Connect team" }));
    await journey.dismissDialog("files", "Choose your team's configuration");
    await press(user, sheet.getByRole("button", { name: "Choose file…" }));
    await waitFor(() => expect(journey.callsTo("ChooseHubTeamConfig").at(-1)?.result).toMatchObject({ state: "cancelled" }));
    expect((sheet.getByRole("button", { name: "Save" }) as HTMLButtonElement).disabled).toBe(true);
    await press(user, sheet.getByRole("button", { name: "Cancel" }));
    expect(await within(region("Team")).findByText("No team configured")).toBeTruthy();

    // Signed in with no license activated on this machine, publishing is
    // refused by the window's own admission before anything is sent.
    await connectTeam(user, hub);
    await signIn(user, hub, "analyst");
    let upload = await uploadReview(user, "handover/reschedule-summary.txt");
    await press(user, upload.getByRole("button", { name: "Upload" }));
    await waitFor(() =>
      expect(within(screen.getByRole("dialog", { name: "Upload" })).getByText("operation activation is missing or invalid; select and activate an operation policy")).toBeTruthy(),
    );
    const refusedUpload = within(screen.getByRole("dialog", { name: "Upload" }));
    expect(journey.callsTo("ExecuteReviewedAction").at(-1)?.result).toMatchObject({ state: "permission_denied" });
    await press(user, refusedUpload.getByRole("button", { name: "Done" }));
    const viewer = await colleagueSignedIn(hub, "viewer");
    const received = journey.path("colleagues/viewer/received.txt");
    expect(await viewer.call("DownloadHubArtifact", { project: PROJECT, digest, destination_path: received })).toMatchObject({
      state: "failed",
      reason: "artifact not found in project",
    });

    // Activated, the same file is published, and another person's window
    // reads back exactly its bytes by its digest.
    await activateLicense(user, journey);
    await teamPage(user);
    upload = await uploadReview(user, "handover/reschedule-summary.txt");
    await press(user, upload.getByRole("button", { name: "Upload" }));
    // The review gives way to what the upload did.
    await waitFor(() => expect(within(screen.getByRole("dialog", { name: "Upload" })).getByText("Uploaded")).toBeTruthy());
    const done = within(screen.getByRole("dialog", { name: "Upload" }));
    expect(journey.callsTo("ExecuteReviewedAction").at(-1)?.result).toMatchObject({ state: "completed" });
    await press(user, done.getByRole("button", { name: "Done" }));
    expect(await viewer.call("DownloadHubArtifact", { project: PROJECT, digest, destination_path: received })).toMatchObject({ state: "completed", digest });
    expect(journey.readFile("colleagues/viewer/received.txt")).toBe(journey.readFile("handover/reschedule-summary.txt"));

    // Listed among the project's files by its digest, as nothing names it,
    // it downloads byte for byte.
    const listedAs = `Artifact · ${digest.slice(0, 7)}`;
    const files = await teamView(user, "Files");
    const listed = await files.findByRole("row", { name: listedAs });
    expect(listed.textContent).toContain("analyst");
    expect(await download(user, listedAs, "downloads/reschedule-summary.txt")).toMatchObject({ state: "completed", digest });
    expect(await files.findByText(`Saved ${journey.path("downloads/reschedule-summary.txt")}`)).toBeTruthy();
    expect(journey.readFile("downloads/reschedule-summary.txt")).toBe(journey.readFile("handover/reschedule-summary.txt"));

    // The hub's stored copy is damaged on its operator's disk, so its bytes no
    // longer match their digest: the hub refuses to serve them, the window
    // says so, and nothing is written.
    journey.changeFile(`hub-operator/artifacts/${digest}`, "Reschedule accepted by the downstream system; synthetic evidence.\n");
    expect(await download(user, listedAs, "downloads/damaged.txt")).toMatchObject({ state: "failed" });
    expect(await files.findByText("download failed with status 503")).toBeTruthy();
    expect(() => journey.readFile("downloads/damaged.txt")).toThrow();

    // Signed in as a viewer, whose role may not write, the hub refuses the
    // same publication.
    await account(user, "Sign out");
    await signIn(user, hub, "viewer");
    upload = await uploadReview(user, "handover/reschedule-summary.txt");
    await press(user, upload.getByRole("button", { name: "Upload" }));
    await waitFor(() =>
      expect(within(screen.getByRole("dialog", { name: "Upload" })).getByText("the upload was refused: hub access refused; insufficient permissions or role revoked")).toBeTruthy(),
    );
    expect(journey.callsTo("ExecuteReviewedAction").at(-1)?.result).toMatchObject({ state: "permission_denied" });
    await press(user, within(screen.getByRole("dialog", { name: "Upload" })).getByRole("button", { name: "Done" }));

    // Once the session the identity provider issued expires, the window
    // refuses to use it and sends nothing; signing in again is the person's
    // own act.
    await account(user, "Sign out");
    await signIn(user, hub, "analyst", 8);
    await account(user, "Session details");
    const session = within(await screen.findByRole("dialog", { name: "Session" }));
    const ends = Date.parse((journey.callsTo("CompleteHubAuth").at(-1)?.result as { expires_at?: string }).expires_at ?? "");
    expect(Number.isNaN(ends)).toBe(false);
    expect(session.getByText("Session ends")).toBeTruthy();
    await press(user, session.getByRole("button", { name: "Close session" }));
    await new Promise((resolve) => setTimeout(resolve, Math.max(0, ends - Date.now()) + 500));
    panel = await teamPage(user);
    expect(await panel.findByText("Session ended")).toBeTruthy();
    expect(journey.callsTo("ReadHubTeam").at(-1)?.result).toMatchObject({ state: "permission_denied" });
    expect(panel.queryByRole("region", { name: `${PROJECT} activity` })).toBeNull();
    await signIn(user, hub, "analyst");
    await teamView(user, "Activity");

    // The hub's operator takes the service down: the read the person asks
    // for is reported as failed and not retried, and asking again once the
    // service is back answers.
    await hub.operate(["migrate"]);
    const reads = journey.callsTo("ReadHubTeam").length;
    panel = await teamPage(user);
    expect(await panel.findByText("hub connection failed")).toBeTruthy();
    expect(journey.callsTo("ReadHubTeam").slice(reads).some((call) => /connection refused/.test((call.result as { reason?: string }).reason ?? ""))).toBe(true);
    // Nothing is asked of it again on its own.
    const asked = journey.calls.length;
    await new Promise((resolve) => setTimeout(resolve, 2_000));
    expect(journey.calls.slice(asked).filter((call) => call.method.includes("Hub"))).toEqual([]);
    await hub.restart([]);
    await teamPage(user);
    expect(await (await teamView(user, "Files")).findByRole("row", { name: listedAs })).toBeTruthy();

    // Signing out ends the session in this window: the project goes, and
    // signing in again runs the whole flow rather than renewing the session
    // that ended.
    await account(user, "Sign out");
    panel = within(region("Team"));
    expect(await panel.findByText("Not connected")).toBeTruthy();
    expect(journey.callsTo("DisconnectHub").at(-1)?.result).toMatchObject({
      connected: false,
      authenticated: false,
      custody_warning: "Downloaded copies remain under local custody and cannot be revoked.",
    });
    expect(panel.queryByRole("region", { name: `${PROJECT} activity` })).toBeNull();
    const connects = journey.callsTo("ConnectHub").length;
    await signIn(user, hub, "analyst");
    expect(journey.callsTo("ConnectHub").at(connects)?.result).toMatchObject({ connected: true, authenticated: false });
  },
);

test(
  "a support summary the team approved downloads from a real hub byte for byte, and the person's notifications and searches read what the hub recorded",
  async (context) => {
    // A real hub needs a PostgreSQL installation to create its cluster from.
    if (!Journey.hubAvailable) context.skip();
    const user = userEvent.setup();
    const hub = await journey.startHub(PROJECT, [
      { subject: "analyst", role: "analyst" },
      { subject: "reviewer", role: "reviewer" },
      { subject: "admin", role: "admin" },
    ]);
    journey.makeFolder("downloads");
    const { summary, summaryDigest, requestId } = await supportRequested(user, hub);

    // Until an approval names those bytes nothing offers to download them.
    await openSupportReview(user);
    expect(await screen.findByText("Check failed")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Download summary" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Approve summary" })).toBeNull();

    // The reviewer approves the same bytes from their own machine and tells
    // the person so.
    const reviewer = await colleagueSignedIn(hub, "reviewer");
    journey.writeFile("colleagues/reviewer/received/published-support/support.json", summary);
    expect(
      await reviewer.call("PostHubSupportReview", {
        project: PROJECT,
        workspace: journey.path("colleagues/reviewer/received"),
        entry: "published-support",
        kind: "support-approval",
        id: "support-approval-1",
        recipient: "",
      }),
    ).toMatchObject({ state: "completed", events: [{ parent: requestId, evidence: summaryDigest }] });
    const head = (await reviewer.call("ListHubReviews", PROJECT)).head ?? 0;
    expect(
      await reviewer.call("PostHubReview", {
        project: PROJECT,
        id: "reviewer-tells-analyst",
        expected: head,
        kind: "comment",
        evidence: summaryDigest,
        parent: "",
        recipient: "analyst",
        text: "Approved; download the summary from the hub.",
        release: "",
      }),
    ).toMatchObject({ state: "completed", head: head + 1 });

    // What is addressed to the person is what the hub recorded for them.
    await teamPage(user);
    const activity = await teamView(user, "Activity");
    await press(user, activity.getByRole("button", { name: "Filter" }));
    const filter = within(await screen.findByRole("dialog", { name: "Filter activity" }));
    await user.selectOptions(filter.getByLabelText("Show"), "me");
    await press(user, filter.getByRole("button", { name: "Apply" }));
    const rows = () => Array.from(activity.getByRole("table", { name: "Activity" }).querySelectorAll("tbody tr[data-row-id]"));
    // The reviewer's answer to the person's request and the comment
    // addressed to them; nothing the person did themselves.
    const cells = () => rows().map((row) => Array.from(row.querySelectorAll("td,th")).slice(0, 3).map((cell) => cell.textContent));
    await waitFor(() => expect(cells().map(([action]) => action).sort()).toEqual(["Approved summary", "Commented"]));
    expect(cells().every(([, , person]) => person === "reviewer")).toBe(true);

    // The history search finds the comment by its words.
    await press(user, activity.getByRole("button", { name: "Search" }));
    const search = within(await screen.findByRole("dialog", { name: "Search activity" }));
    await user.type(search.getByLabelText("Text"), "download the summary");
    await press(user, search.getByRole("button", { name: "Search" }));
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Search activity" })).toBeNull());
    expect(journey.callsTo("SearchHubReviews").at(-1)?.result).toMatchObject({ state: "completed", events: [{ actor: "reviewer", text: "Approved; download the summary from the hub." }] });
    await waitFor(() => expect(cells().map(([action]) => action)).toEqual(["Commented"]));
    await press(user, activity.getByRole("button", { name: "Clear search" }));

    // Approved, the summary downloads byte for byte into a new file.
    const reviews = await openSupportReview(user);
    await journey.nameNewFolder(journey.path("downloads/support.json"), "Download summary");
    await press(user, await reviews.findByRole("button", { name: "Download summary" }));
    expect(await reviews.findByText(`Saved ${journey.path("downloads/support.json")}`)).toBeTruthy();
    expect(journey.callsTo("DownloadHubSummary").at(-1)?.result).toMatchObject({ state: "completed", digest: summaryDigest });
    expect(journey.readFile("downloads/support.json")).toBe(summary);
  },
);

test(
  "an operator stores and reads artifacts by digest on a real operator-only hub: an unlicensed store, a damaged stored copy, a stopped hub, a hub switched to team mode and a disconnection are each reported, never retried",
  async (context) => {
    // A real hub needs a PostgreSQL installation to create its cluster from.
    if (!Journey.hubAvailable) context.skip();
    const user = userEvent.setup();
    const hub = await journey.startHub("operator-store", [], "operator");
    const probe = "Synthetic operator-only hub probe.\n";
    journey.writeFile("handover/probe.txt", probe);
    journey.writeFile("handover/second-probe.txt", "A second synthetic operator-only hub probe.\n");
    const digest = journey.digest("handover/probe.txt");
    journey.makeFolder("received");
    await journey.launch();

    // Settings › Team › Administrator setup › Operator hub.
    const team = await teamPage(user);
    await press(user, await team.findByRole("button", { name: "More team actions" }));
    await press(user, await screen.findByRole("menuitem", { name: "Administrator setup" }));
    await press(user, await within(region("Administrator setup")).findByRole("button", { name: "Operator hub" }));
    let operator = within(await screen.findByRole("region", { name: "Operator hub" }));
    const configTitle = "Choose the operator-only hub configuration";
    const storeTitle = "Choose the file to store in the operator-only hub";
    const saveTitle = "Name the file to save the artifact as";
    const outcome = () => operator.queryByText(/^(Saved |Stored · )/)?.textContent ?? operator.queryAllByRole("alert").map((alert) => alert.textContent).join(" ");
    const readAs = async (address: string, name: string) => {
      await press(user, operator.getByRole("button", { name: "Download by hash" }));
      const sheet = within(await screen.findByRole("dialog", { name: "Download by hash" }));
      await user.clear(sheet.getByLabelText("SHA-256"));
      await user.type(sheet.getByLabelText("SHA-256"), address);
      await journey.nameNewFolder(journey.path(`received/${name}`), saveTitle);
      const asked = journey.callsTo("ReadOperatorHubArtifact").length;
      await press(user, sheet.getByRole("button", { name: "Download" }));
      await waitFor(() => expect(journey.callsTo("ReadOperatorHubArtifact")[asked]?.settled).toBe(true));
      await waitFor(() => expect(screen.queryByRole("dialog", { name: "Download by hash" })).toBeNull());
      return journey.callsTo("ReadOperatorHubArtifact")[asked]?.result;
    };
    const store = async (file: string) => {
      await journey.chooseFiles([journey.path(file)], storeTitle);
      const asked = journey.callsTo("StoreOperatorHubArtifact").length;
      await press(user, operator.getByRole("button", { name: "Upload file" }));
      await waitFor(() => expect(journey.callsTo("StoreOperatorHubArtifact")[asked]?.settled).toBe(true));
      return journey.callsTo("StoreOperatorHubArtifact")[asked]?.result;
    };
    const status = () => within(operator.getByLabelText("Operator hub", { selector: "dl" })).getByText("Status", { selector: "dt" }).nextElementSibling?.textContent;

    // A dismissed dialog chooses nothing, and a team hub's configuration is
    // not an operator-only one.
    await journey.dismissDialog("files", configTitle);
    await press(user, operator.getByRole("button", { name: "Choose configuration…" }));
    await waitFor(() => expect(journey.callsTo("ChooseOperatorHubConfig").at(-1)?.result).toMatchObject({ state: "cancelled" }));
    expect((operator.getByRole("button", { name: "Connect" }) as HTMLButtonElement).disabled).toBe(true);
    await journey.chooseFiles([`${hub.clientConfigFolder}/hub-client.json`], configTitle);
    await press(user, operator.getByRole("button", { name: "Choose configuration…" }));
    expect(await operator.findByText("not an operator-only hub configuration this release reads")).toBeTruthy();

    // The operator's configuration is chosen, and connecting is its own act.
    await journey.chooseFiles([hub.operatorConfig], configTitle);
    await press(user, operator.getByRole("button", { name: "Choose configuration…" }));
    expect(await operator.findByText(`https://${hub.address}`)).toBeTruthy();
    expect(status()).toBe("Not connected");
    await press(user, operator.getByRole("button", { name: "Connect" }));
    await waitFor(() => expect(status()).toBe("Connected"));

    // With no license activated on this machine, storing is refused by the
    // window's own admission before any dialog opens.
    await press(user, operator.getByRole("button", { name: "Upload file" }));
    expect(await operator.findByText("operation activation is missing or invalid; select and activate an operation policy")).toBeTruthy();
    expect(journey.callsTo("StoreOperatorHubArtifact").at(-1)?.result).toMatchObject({ state: "permission_denied" });

    // Activated, the file is stored under its digest, and read back byte for
    // byte into a new file named in the save dialog, with the custody notice.
    await activateLicense(user, journey);
    await settings(user, "Team");
    // Team returns to the task last open in its Administrator setup.
    await waitFor(() => expect(screen.queryByRole("region", { name: "Administrator setup" })).toBeTruthy());
    const task = within(region("Administrator setup")).queryByRole("button", { name: "Operator hub" });
    if (task) await press(user, task);
    operator = within(await screen.findByRole("region", { name: "Operator hub" }));
    await waitFor(() => expect(status()).toBe("Connected"));
    expect(await store("handover/probe.txt")).toMatchObject({ state: "completed", digest, size: probe.length });
    expect(outcome()).toBe(`Stored · ${digest}`);
    expect(await readAs(digest, "probe.txt")).toMatchObject({ state: "completed", digest });
    expect(outcome()).toBe(`Saved ${journey.path("received/probe.txt")} Downloaded copies remain under local custody and cannot be revoked.`);
    expect(journey.readFile("received/probe.txt")).toBe(probe);

    // A digest typed in capitals is not the whole lowercase digest: nothing
    // is asked of the hub for it.
    await press(user, operator.getByRole("button", { name: "Download by hash" }));
    const typed = within(await screen.findByRole("dialog", { name: "Download by hash" }));
    await user.clear(typed.getByLabelText("SHA-256"));
    await user.type(typed.getByLabelText("SHA-256"), digest.toUpperCase());
    expect((typed.getByRole("button", { name: "Download" }) as HTMLButtonElement).disabled).toBe(true);
    await press(user, typed.getByRole("button", { name: "Cancel" }));

    // A digest the hub does not hold, and a stored copy damaged on the
    // hub's disk, which the hub refuses to serve: nothing is written.
    expect(await readAs("0".repeat(64), "absent.txt")).toMatchObject({ state: "failed" });
    expect(
      operator.getByText(
        "the hub holds no artifact under this digest, or does not offer the operator-only artifact store; a hub serving team mode reads evidence only through a signed-in project",
      ),
    ).toBeTruthy();
    expect(() => journey.readFile("received/absent.txt")).toThrow();
    journey.changeFile(`hub-operator/artifacts/${digest}`, "Synthetic operator-only hub probe, damaged.\n");
    expect(await readAs(digest, "damaged.txt")).toMatchObject({ state: "failed" });
    expect(
      operator.getByText("the hub could not serve the artifact: it is busy, its storage or metadata is unavailable, or its stored copy no longer matches its digest"),
    ).toBeTruthy();
    expect(() => journey.readFile("received/damaged.txt")).toThrow();

    // The operator stops the service: nothing reaches it, and once it is
    // back the next store answers.
    await hub.operate(["migrate"]);
    expect(await readAs(digest, "stopped.txt")).toMatchObject({ state: "failed", reason: "the hub could not be reached; nothing was sent" });
    expect(() => journey.readFile("received/stopped.txt")).toThrow();
    await hub.serveAs("operator");
    expect(await store("handover/second-probe.txt")).toMatchObject({ state: "completed", digest: journey.digest("handover/second-probe.txt") });

    // Served as a team hub, it offers no operator-only store; served
    // operator-only again once it has served team mode, it refuses both.
    await hub.serveAs("team");
    expect(await store("handover/probe.txt")).toMatchObject({
      state: "failed",
      reason: "the hub does not offer the operator-only artifact store; a hub serving team mode stores evidence only through a signed-in project",
    });
    expect(await readAs(digest, "team.txt")).toMatchObject({ state: "failed" });
    await hub.serveAs("operator");
    expect(await readAs(digest, "closed.txt")).toMatchObject({
      state: "permission_denied",
      reason: "the hub refused operator-only access; a hub that has served team mode reads and stores evidence only through a signed-in project",
    });
    expect(await store("handover/probe.txt")).toMatchObject({ state: "permission_denied" });
    for (const name of ["team.txt", "closed.txt"]) expect(() => journey.readFile(`received/${name}`)).toThrow();

    // Disconnecting ends the connection.
    await press(user, operator.getByRole("button", { name: "Disconnect" }));
    await waitFor(() => expect(status()).toBe("Not connected"));
    expect(operator.queryByRole("button", { name: "Upload file" })).toBeNull();
    expect(journey.callsTo("DisconnectOperatorHub").at(-1)?.result).toMatchObject({
      connected: false,
      custody_warning: "Downloaded copies remain under local custody and cannot be revoked.",
    });
  },
);

test(
  "Team views on a real hub work from the keyboard and in a compact window, ask before discarding an edited comment, keep it through a stopped hub, and record a retried decision once under its intent",
  async (context) => {
    // A real hub needs a PostgreSQL installation to create its cluster from.
    if (!Journey.hubAvailable) context.skip();
    const user = userEvent.setup();
    const hub = await journey.startHub(PROJECT, [
      { subject: "analyst", role: "analyst" },
      { subject: "reviewer", role: "reviewer" },
      { subject: "admin", role: "admin" },
    ]);
    const { summaryDigest, requestId } = await supportRequested(user, hub);
    const selectedRow = (table: HTMLElement) => table.querySelector<HTMLElement>('tr[aria-selected="true"]')?.getAttribute("aria-label");

    // Keyboard: the arrow keys move the selection along Files, Enter opens
    // the selected file, and Escape closes that sheet and nothing else.
    const files = await teamView(user, "Files");
    const fileTable = await files.findByRole("table", { name: "Files" });
    await waitFor(() => expect(fileTable.querySelectorAll("tbody tr[data-row-id]")).toHaveLength(2));
    const fileRows = Array.from(fileTable.querySelectorAll<HTMLElement>("tbody tr[data-row-id]"));
    fileRows[0]!.focus();
    await user.keyboard("{ArrowDown}");
    await waitFor(() => expect(selectedRow(fileTable)).toBe(fileRows[1]!.getAttribute("aria-label")));
    await user.keyboard("{Enter}");
    const details = await screen.findByRole("dialog", { name: fileRows[1]!.getAttribute("aria-label")! });
    expect(within(details).getByLabelText("File details", { selector: "dl" })).toBeTruthy();
    await user.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("dialog", { name: fileRows[1]!.getAttribute("aria-label")! })).toBeNull());
    expect(selectedRow(fileTable)).toBe(fileRows[1]!.getAttribute("aria-label"));
    expect(region("Team")).toBeTruthy();

    // Compact: in a narrow window the review opened from the keyboard is
    // shown alone, and Back returns to the list with it still selected.
    windowWidth(820);
    try {
      expect(document.querySelector(".app")?.classList.contains("compact")).toBe(true);
      const reviews = await teamView(user, "Reviews");
      const reviewTable = await reviews.findByRole("table", { name: "Reviews" });
      within(reviewTable).getByRole("row", { name: "Support summary" }).focus();
      await user.keyboard("{Enter}");
      expect(await reviews.findByRole("heading", { name: "Review" })).toBeTruthy();
      expect(reviews.queryByRole("table", { name: "Reviews" })).toBeNull();
      await press(user, reviews.getByRole("button", { name: "Back to reviews" }));
      const restored = await reviews.findByRole("table", { name: "Reviews" });
      expect(selectedRow(restored)).toBe("Support summary");
      within(restored).getByRole("row", { name: "Support summary" }).focus();
      await user.keyboard("{Enter}");
      await reviews.findByRole("heading", { name: "Review" });
    } finally {
      vi.restoreAllMocks();
      window.dispatchEvent(new Event("resize"));
    }
    const review = await openSupportReview(user);

    // Dirty: an edited comment is not thrown away on Escape. The question
    // is the topmost sheet, so Escape closes only it; Keep editing keeps the
    // text; nothing is sent.
    const text = "The summary matches the retained packet.";
    const posts = journey.callsTo("PostHubReview").length;
    await press(user, review.getByRole("button", { name: "Comment" }));
    const comment = within(await screen.findByRole("dialog", { name: "Comment" }));
    await user.type(comment.getByLabelText("Comment"), text);
    await user.keyboard("{Escape}");
    await screen.findByRole("dialog", { name: "Save changes?" });
    await user.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Save changes?" })).toBeNull());
    expect((comment.getByLabelText("Comment") as HTMLTextAreaElement).value).toBe(text);
    await press(user, comment.getByRole("button", { name: "Cancel" }));
    await press(user, within(await screen.findByRole("dialog", { name: "Save changes?" })).getByRole("button", { name: "Keep editing" }));
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Save changes?" })).toBeNull());
    expect((comment.getByLabelText("Comment") as HTMLTextAreaElement).value).toBe(text);
    expect(journey.callsTo("PostHubReview")).toHaveLength(posts);

    // Error: with the hub stopped, the comment is refused with the reason,
    // and the sheet keeps what was typed.
    await hub.operate(["migrate"]);
    await press(user, comment.getByRole("button", { name: "Comment" }));
    expect(await comment.findByText(/connection refused/)).toBeTruthy();
    expect((comment.getByLabelText("Comment") as HTMLTextAreaElement).value).toBe(text);
    const first = journey.callsTo("PostHubReview").at(-1)!.args[0] as { id: string; text: string };
    expect(first.text).toBe(text);

    // The decision is unsettled, so its intent is held: a changed comment is
    // refused before anything is sent.
    await user.type(comment.getByLabelText("Comment"), " Changed.");
    await press(user, comment.getByRole("button", { name: "Comment" }));
    expect(await comment.findByText("The previous request is unresolved. Retry that decision before changing it.")).toBeTruthy();
    expect(journey.callsTo("PostHubReview")).toHaveLength(posts + 1);

    // Back to the same words once the hub is back, a double click sends the
    // held command once more, under the same identity, and the hub records
    // it once.
    await user.clear(comment.getByLabelText("Comment"));
    await user.type(comment.getByLabelText("Comment"), text);
    await hub.restart([]);
    await user.dblClick(comment.getByRole("button", { name: "Comment" }));
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Comment" })).toBeNull());
    const retried = journey.callsTo("PostHubReview").slice(posts + 1);
    expect(retried.map((call) => (call.args[0] as { id: string }).id)).toEqual([first.id]);
    expect(retried[0]!.result).toMatchObject({ state: "completed" });
    const reviewer = await colleagueSignedIn(hub, "reviewer");
    let history = await reviewer.call("ListHubReviews", PROJECT);
    expect(history.events?.filter((event) => event.command_id === first.id)).toHaveLength(1);
    await waitFor(() =>
      expect(Array.from(review.getByText("Discussion").nextElementSibling!.querySelectorAll("li")).map((item) => item.textContent ?? "")).toEqual([
        expect.stringMatching(new RegExp(`^analyst · Commented · .*${text.replace(/\./g, "\\.")}$`)),
      ]),
    );

    // Through the real facade, the same command sent again is answered from
    // what the hub recorded and recorded once; the same identity with other
    // words is refused.
    const command = { project: PROJECT, id: "reviewer-once", expected: history.head ?? 0, kind: "comment", evidence: summaryDigest, parent: requestId, recipient: "", text: "Seen.", release: "" };
    expect(await reviewer.call("PostHubReview", command)).toMatchObject({ state: "completed", head: (history.head ?? 0) + 1 });
    expect(await reviewer.call("PostHubReview", command)).toMatchObject({ state: "completed", head: (history.head ?? 0) + 1 });
    expect(await reviewer.call("PostHubReview", { ...command, text: "Seen, with other words." })).toMatchObject({ state: "failed" });
    history = await reviewer.call("ListHubReviews", PROJECT);
    expect(history.events?.filter((event) => event.command_id === "reviewer-once").map((event) => event.text)).toEqual(["Seen."]);
  },
);
