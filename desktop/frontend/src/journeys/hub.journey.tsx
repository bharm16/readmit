// Team work on a real customer hub: the checkout's readmit-hub serving over
// mutual TLS on loopback, its store in a disposable PostgreSQL cluster, and a
// customer identity provider. A person selects the hub configuration their
// operator supplied, connects, signs in through the identity provider, and
// publishes evidence to the project. A colleague, in their own window on
// their own machine, reviews the same evidence at the same moment; the
// person's decision, made against the history they had loaded, is refused as
// a conflict rather than silently ordered, and is recorded once they load the
// current history and renew it. Both windows then read one history, each entry
// under the identity the hub authenticated. A released test version is put to
// the team the same way: the request names the released bytes, the hub
// refuses the requester's own approval and that of a reviewer the request did
// not name, and chains the requested reviewer's approval to it.
//
// Publishing is refused before anything is sent until a license is activated,
// and by the hub for a role that may not write; a stored copy that no longer
// matches its digest is refused, an expired session is refused by the window,
// a stopped hub is reported and not retried, and disconnecting ends the
// session. A support summary the team approved
// downloads byte for byte only under the digest the approval names, and the
// person's notifications and the history and notification searches read what
// the hub recorded.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { UserEvent } from "@testing-library/user-event";
import { byContent, enter, Journey, press, region } from "../testkit/journey";
import type { Hub } from "../testkit/hub.js";
import { activateLicense, releaseSavedTest, savedAckTest } from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

const PROJECT = "scheduling";

/** The customer hub panel of the privacy region. */
function hubPanel() {
  return within(within(region("Privacy status")).getByRole("region", { name: "Customer Artifact Hub" }));
}

/** Selects the configuration the hub's operator supplied and connects. */
async function openHub(user: UserEvent, hub: Hub) {
  const panel = hubPanel();
  await journey.chooseFolder(hub.clientConfigFolder, "Choose customer hub configuration folder");
  await press(user, panel.getByRole("button", { name: "Choose hub configuration…" }));
  await press(user, await panel.findByRole("button", { name: "Connect to hub" }));
  expect(await panel.findByText(`Connected (https://${hub.address})`)).toBeTruthy();
}

/** Signs in as subject through the customer identity provider, as the
 * person's browser completes it, for a session of lifetimeSeconds when given. */
async function signIn(user: UserEvent, hub: Hub, subject: string, lifetimeSeconds?: number) {
  const panel = hubPanel();
  await press(user, await panel.findByRole("button", { name: "Sign in with Customer IdP" }));
  const login = await panel.findByRole("link", { name: "Open login window" });
  expect(await hub.signIn(login.getAttribute("href") ?? "", subject, lifetimeSeconds)).toBe(200);
  expect(await panel.findByRole("heading", { name: "Authenticated User" })).toBeTruthy();
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

test(
  "two people review the same evidence on a real hub: the decision made against a stale history is refused as a conflict and recorded once renewed",
  async (context) => {
    // A real hub needs a PostgreSQL installation to create its cluster from.
    if (!Journey.hubAvailable) context.skip();
    const user = userEvent.setup();
    const hub = await journey.startHub(PROJECT, [
      { subject: "analyst", role: "analyst" },
      { subject: "reviewer", role: "reviewer" },
    ]);
    journey.writeFile("handover/reschedule-summary.txt", "Reschedule refused by the downstream system; synthetic evidence.\n");
    await journey.launch();
    await activateLicense(user, journey);

    // A sign-in abandoned before the browser returns is cancelled, and the
    // window offers it again at once; the next one completes.
    await openHub(user, hub);
    let panel = hubPanel();
    await press(user, await panel.findByRole("button", { name: "Sign in with Customer IdP" }));
    await panel.findByRole("link", { name: "Open login window" });
    await press(user, panel.getByRole("button", { name: "Cancel sign-in" }));
    await waitFor(() => expect(journey.callsTo("CompleteHubAuth").at(-1)?.result).toMatchObject({ state: "cancelled" }));
    await signIn(user, hub, "analyst");
    panel = hubPanel();
    expect(panel.getByText("Subject:").parentElement?.textContent).toBe("Subject: analyst");
    const project = within(panel.getByText(PROJECT, { selector: "strong" }).closest("li")!);
    expect(project.getByText("Authorized")).toBeTruthy();

    // Evidence published to the project, under the digest the hub stores it by.
    await press(user, project.getByRole("button", { name: "View Project Artifacts" }));
    await enter(user, await panel.findByLabelText("Upload source path:"), journey.path("handover/reschedule-summary.txt"));
    await press(user, panel.getByRole("button", { name: "Publish Artifact" }));
    expect(await panel.findByText(byContent(/^Transfer state: completed \(65 bytes\)$/))).toBeTruthy();
    const digest = journey.digest("handover/reschedule-summary.txt");
    expect(await panel.findByText(byContent(new RegExp(`^Artifact digest: ${digest}$`)))).toBeTruthy();

    // The colleague signs in from their own window on their own machine.
    const reviewer = await colleagueSignedIn(hub, "reviewer");

    // The person loads the history; the colleague posts before the person
    // does, so the person's decision names a head the hub has moved past.
    const team = within(screen.getByRole("region", { name: "Team collaboration" }));
    await press(user, team.getByRole("button", { name: "Load review history" }));
    expect(await team.findByRole("heading", { name: "Review history (head 0)" })).toBeTruthy();
    expect(
      await reviewer.call("PostHubReview", { project: PROJECT, id: "reviewer-confirms", expected: 0, kind: "comment", evidence: digest, parent: "", recipient: "", text: "Confirmed on the lab fixture.", release: "" }),
    ).toMatchObject({ state: "completed", head: 1 });
    const decision = within(team.getByRole("heading", { name: "Post collaboration decision" }).parentElement!);
    await user.selectOptions(decision.getByLabelText("Kind"), "comment");
    await enter(user, decision.getByLabelText("Command id"), "analyst-finding");
    await enter(user, decision.getByLabelText("Evidence digest"), digest);
    await enter(user, decision.getByLabelText("Text"), "The reschedule is matched on the filler identifier.");
    await press(user, decision.getByRole("button", { name: "Submit review decision" }));
    expect((await team.findAllByText("hub head or revision conflict; fetch current state and renew the action")).length).toBeGreaterThan(0);
    expect(journey.callsTo("PostHubReview").at(-1)?.result).toMatchObject({ state: "failed" });

    // Loaded again and renewed, the decision is recorded after the
    // colleague's, and both windows read the one history the hub keeps,
    // each entry under the identity the hub authenticated.
    await press(user, team.getByRole("button", { name: "Load review history" }));
    expect(await team.findByRole("heading", { name: "Review history (head 1)" })).toBeTruthy();
    await press(user, decision.getByRole("button", { name: "Submit review decision" }));
    expect(await team.findByRole("heading", { name: "Review history (head 2)" })).toBeTruthy();
    const entries = team.getAllByRole("listitem").map((item) => item.textContent ?? "");
    expect(entries).toEqual([
      `comment by reviewer@https://idp.journey.test · evidence ${digest.slice(0, 12)}… — Confirmed on the lab fixture.`,
      `comment by analyst@https://idp.journey.test · evidence ${digest.slice(0, 12)}… — The reschedule is matched on the filler identifier.`,
    ]);
    const seen = await reviewer.call("ListHubReviews", PROJECT);
    expect(seen.head).toBe(2);
    expect(seen.events?.map((event) => event.actor)).toEqual(["reviewer", "analyst"]);
  },
);

/** The suites panel of the open workspace. */
function suites() {
  return within(region("Suites and releases"));
}

test(
  "a released test version is put to the team on the real hub: the request names its exact bytes, only the requested reviewer approves it",
  async (context) => {
    // A real hub needs a PostgreSQL installation to create its cluster from.
    if (!Journey.hubAvailable) context.skip();
    const user = userEvent.setup();
    const hub = await journey.startHub(PROJECT, [
      { subject: "analyst", role: "analyst" },
      { subject: "reviewer", role: "reviewer" },
      { subject: "auditor", role: "reviewer" },
    ]);
    await savedAckTest(user, journey, "fixed");
    await openHub(user, hub);
    await signIn(user, hub, "analyst");

    // The saved test is released as an immutable version, reviewed locally.
    await releaseSavedTest(user, {
      id: "reschedule-accepted",
      approver: "analyst",
      rationale: "Reschedule acknowledgement expectation for release.",
      output: "reschedule-release-1.json",
    });

    // The team review is requested through the hub session, naming the exact
    // released bytes rather than a typed value.
    await press(user, within(suites().getByRole("navigation", { name: "Suite views" })).getByRole("button", { name: "Releases and impact" }));
    const releases = suites();
    await enter(user, releases.getByLabelText("To"), "reschedule-release-1.json");
    await enter(user, releases.getByLabelText("Hub project"), PROJECT);
    await enter(user, releases.getByLabelText("Request recipient"), "reviewer");
    await enter(user, releases.getAllByLabelText("Command id").at(-1)!, "release-review-1");
    await enter(user, releases.getByLabelText("Rationale"), "Please review the released expectation.");
    await press(user, releases.getByRole("button", { name: "Request team review" }));
    const releasedFile = "investigations/scheduling-investigation/reschedule-release-1.json";
    const release = journey.digest(releasedFile);
    expect((await releases.findByText(byContent(/^Recorded: review-request by analyst@/))).textContent).toBe(
      `Recorded: review-request by analyst@https://idp.journey.test · release ${release.slice(0, 12)}…`,
    );

    // The analyst's own approval is refused by the hub: the role grants no
    // approval, whatever the window offers.
    await enter(user, releases.getAllByLabelText("Command id").at(-1)!, "release-self-approval");
    await press(user, releases.getByRole("button", { name: "Approve this release" }));
    expect(await releases.findByText("hub access refused; insufficient permissions or role revoked")).toBeTruthy();

    // A reviewer the request did not name is refused, though their role
    // grants approval; the requested reviewer approves the same released
    // bytes, handed over to their own machine, from their own window.
    const approve = async (subject: string, id: string) => {
      const colleague = await colleagueSignedIn(hub, subject);
      journey.writeFile(`colleagues/${subject}/received/reschedule-release-1.json`, journey.readFile(releasedFile));
      return colleague.call("PostHubReleaseReview", {
        project: PROJECT,
        workspace: journey.path(`colleagues/${subject}/received`),
        entry: "reschedule-release-1.json",
        kind: "approval",
        id,
        recipient: "",
        text: "Approved for the next suite.",
      });
    };
    expect(await approve("auditor", "release-approval-unrequested")).toMatchObject({
      state: "permission_denied",
      reason: "hub access refused; insufficient permissions or role revoked",
    });
    expect(await approve("reviewer", "release-approval-1")).toMatchObject({
      state: "completed",
      events: [{ kind: "approval", actor: "reviewer", parent: "release-review-1", release }],
    });

    // The person's window reads the request and the one approval the hub
    // chained to it, each under the identity the hub authenticated.
    await press(user, hubPanel().getByRole("button", { name: "View Project Artifacts" }));
    const team = within(await screen.findByRole("region", { name: "Team collaboration" }));
    await press(user, team.getByRole("button", { name: "Load review history" }));
    expect(await team.findByRole("heading", { name: "Review history (head 2)" })).toBeTruthy();
    expect(team.getAllByRole("listitem").map((item) => item.textContent)).toEqual([
      `review-request by analyst@https://idp.journey.test · evidence ${release.slice(0, 12)}… · release ${release.slice(0, 12)}… — Please review the released expectation.`,
      `approval by reviewer@https://idp.journey.test · evidence ${release.slice(0, 12)}… · release ${release.slice(0, 12)}… — Approved for the next suite.`,
    ]);
  },
);

/** The team collaboration panel below the hub panel. */
function teamPanel() {
  return within(screen.getByRole("region", { name: "Team collaboration" }));
}

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
    const source = journey.path("handover/reschedule-summary.txt");
    const digest = journey.digest("handover/reschedule-summary.txt");
    await journey.launch();

    // A dismissed dialog chooses nothing: the panel stays offline, with no
    // configuration selected.
    await journey.dismissDialog("folder", "Choose customer hub configuration folder");
    await press(user, hubPanel().getByRole("button", { name: "Choose hub configuration…" }));
    await waitFor(() => expect(journey.callsTo("ChooseHubConfig").at(-1)?.result).toMatchObject({ state: "cancelled" }));
    expect(hubPanel().getByText("No configuration file selected. Working entirely offline.")).toBeTruthy();

    // Signed in with no license activated on this machine, publishing is
    // refused by the window's own admission before anything is sent.
    await openHub(user, hub);
    await signIn(user, hub, "analyst");
    let panel = hubPanel();
    await press(user, panel.getByRole("button", { name: "View Project Artifacts" }));
    await enter(user, await panel.findByLabelText("Upload source path:"), source);
    await press(user, panel.getByRole("button", { name: "Publish Artifact" }));
    expect(await panel.findByText("operation activation is missing or invalid; select and activate an operation policy")).toBeTruthy();
    expect(panel.getByText(/^Transfer state:/).textContent).toBe("Transfer state: permission_denied");
    const viewer = await colleagueSignedIn(hub, "viewer");
    const received = journey.path("colleagues/viewer/received.txt");
    expect(await viewer.call("DownloadHubArtifact", { project: PROJECT, digest, destination_path: received })).toMatchObject({
      state: "failed",
      reason: "artifact not found in project",
    });

    // Activated, the same file is published, and another person's window
    // reads back exactly its bytes by its digest.
    await activateLicense(user, journey);
    panel = hubPanel();
    await press(user, panel.getByRole("button", { name: "Publish Artifact" }));
    expect(await panel.findByText(byContent(/^Transfer state: completed \(65 bytes\)$/))).toBeTruthy();
    expect(panel.getByText(byContent(new RegExp(`^Artifact digest: ${digest}$`)))).toBeTruthy();
    expect(await viewer.call("DownloadHubArtifact", { project: PROJECT, digest, destination_path: received })).toMatchObject({
      state: "completed",
      digest,
    });
    expect(journey.readFile("colleagues/viewer/received.txt")).toBe(journey.readFile("handover/reschedule-summary.txt"));

    // Recorded as a revision, the evidence is listed among the project's
    // artifacts and downloads byte for byte.
    const lifecycle = within(teamPanel().getByRole("heading", { name: "Lifecycle / conflict / admin" }).parentElement!);
    await enter(user, lifecycle.getByLabelText("Command id"), "reschedule-summary-1");
    await enter(user, lifecycle.getByLabelText("Resource"), "reschedule-summary");
    await enter(user, lifecycle.getByLabelText("Artifact digest"), digest);
    await press(user, lifecycle.getByRole("button", { name: "Submit lifecycle command" }));
    expect(await teamPanel().findByRole("heading", { name: "Lifecycle (head 1)" })).toBeTruthy();
    await press(user, panel.getByRole("button", { name: "View Project Artifacts" }));
    const listed = within((await panel.findByTitle(digest)).closest("tr")!);
    journey.makeFolder("downloads");
    await enter(user, panel.getByLabelText("Download destination path:"), journey.path("downloads/reschedule-summary.txt"));
    await press(user, listed.getByRole("button", { name: "Download" }));
    await waitFor(() => expect(journey.callsTo("DownloadHubArtifact").at(-1)?.result).toMatchObject({ state: "completed", digest }));
    expect(journey.readFile("downloads/reschedule-summary.txt")).toBe(journey.readFile("handover/reschedule-summary.txt"));

    // The hub's stored copy is damaged on its operator's disk, so its bytes no
    // longer match their digest: the hub refuses to serve them, the window
    // says so, and nothing is written.
    journey.changeFile(`hub-operator/artifacts/${digest}`, "Reschedule accepted by the downstream system; synthetic evidence.\n");
    await enter(user, panel.getByLabelText("Download destination path:"), journey.path("downloads/damaged.txt"));
    await press(user, listed.getByRole("button", { name: "Download" }));
    expect(await panel.findByText("download failed with status 503")).toBeTruthy();
    expect(journey.callsTo("DownloadHubArtifact").at(-1)?.result).toMatchObject({ state: "failed" });
    expect(() => journey.readFile("downloads/damaged.txt")).toThrow();

    // Signed in as a viewer, whose role may not write, the hub refuses the
    // same publication.
    await press(user, panel.getByRole("button", { name: "Log out" }));
    await press(user, await panel.findByRole("button", { name: "Connect to hub" }));
    await signIn(user, hub, "viewer");
    panel = hubPanel();
    await press(user, panel.getByRole("button", { name: "View Project Artifacts" }));
    await press(user, await panel.findByRole("button", { name: "Publish Artifact" }));
    expect(await panel.findByText("hub access refused; insufficient permissions or role revoked")).toBeTruthy();
    expect(journey.callsTo("UploadHubArtifact").at(-1)?.result).toMatchObject({ state: "permission_denied" });

    // Once the session the identity provider issued expires, the window
    // refuses to use it and sends nothing; signing in again is the person's
    // own act.
    await press(user, panel.getByRole("button", { name: "Log out" }));
    await press(user, await panel.findByRole("button", { name: "Connect to hub" }));
    await signIn(user, hub, "analyst", 8);
    panel = hubPanel();
    const expires = Date.parse(panel.getByText("Session expires:").parentElement?.textContent?.replace("Session expires: ", "") ?? "");
    expect(Number.isNaN(expires)).toBe(false);
    await press(user, panel.getByRole("button", { name: "View Project Artifacts" }));
    await new Promise((resolve) => setTimeout(resolve, Math.max(0, expires - Date.now()) + 500));
    await press(user, teamPanel().getByRole("button", { name: "Load notifications" }));
    expect(await teamPanel().findByText("sign-in required or session expired")).toBeTruthy();
    await press(user, panel.getByRole("button", { name: "Publish Artifact" }));
    await waitFor(() =>
      expect(journey.callsTo("UploadHubArtifact").at(-1)?.result).toMatchObject({
        state: "permission_denied",
        reason: "sign-in required or session expired",
      }),
    );
    await press(user, panel.getByRole("button", { name: "Refresh status" }));
    expect(await panel.findByText("hub session expired; sign in again")).toBeTruthy();
    expect(screen.queryByRole("region", { name: "Team collaboration" })).toBeNull();
    await signIn(user, hub, "analyst");

    // The hub's operator takes the service down: what the person asks for is
    // reported as failed and not retried, and asking again once the service
    // is back answers.
    await hub.operate(["migrate"]);
    await press(user, teamPanel().getByRole("button", { name: "Load notifications" }));
    expect(await teamPanel().findByText(/connection refused/)).toBeTruthy();
    expect(journey.callsTo("ListHubNotifications").at(-1)?.result).toMatchObject({ state: "failed" });
    await hub.restart([]);
    await press(user, teamPanel().getByRole("button", { name: "Load notifications" }));
    expect(await teamPanel().findByText("Nothing in this project is addressed to you.")).toBeTruthy();

    // Disconnecting ends the session in this window: the team panel and the
    // transfers go, and connecting again needs a new sign-in rather than
    // renewing the one that ended.
    await press(user, panel.getByRole("button", { name: "Disconnect" }));
    expect(await panel.findByText("Offline / Local Mode")).toBeTruthy();
    expect(journey.callsTo("DisconnectHub").at(-1)?.result).toMatchObject({
      connected: false,
      authenticated: false,
      custody_warning: "Downloaded copies remain under local custody and cannot be revoked.",
    });
    expect(screen.queryByRole("region", { name: "Team collaboration" })).toBeNull();
    expect(panel.queryByRole("button", { name: "Publish Artifact" })).toBeNull();
    await press(user, panel.getByRole("button", { name: "Connect to hub" }));
    expect(await panel.findByRole("button", { name: "Sign in with Customer IdP" })).toBeTruthy();
    expect(journey.callsTo("ConnectHub").at(-1)?.result).toMatchObject({ connected: true, authenticated: false });
  },
);

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

    // The project's administrator announces its sharing policy from their
    // own machine.
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

    // A teammate published a value-free support summary under that policy and
    // handed the bundle over; the person opens the folder they keep it in.
    const summary = supportSummary(policyDigest);
    journey.writeFile("support-work/published-support/support.json", summary);
    const summaryDigest = journey.digest("support-work/published-support/support.json");
    journey.makeFolder("downloads");
    await journey.launch();
    await activateLicense(user, journey);
    await journey.chooseFolder(journey.path("support-work"), "Open a readmit workspace folder");
    await press(user, screen.getByRole("button", { name: "Open a workspace folder…" }));
    expect(await within(region("Project navigation")).findByText("published-support")).toBeTruthy();
    await openHub(user, hub);
    await signIn(user, hub, "analyst");
    await press(user, hubPanel().getByRole("button", { name: "View Project Artifacts" }));

    // The person asks the reviewer to approve the summary's exact bytes.
    const team = teamPanel();
    await team.findByRole("option", { name: "published-support" });
    await user.selectOptions(team.getByLabelText("Published support bundle"), "published-support");
    await enter(user, team.getByLabelText("Support command id"), "support-request-1");
    await enter(user, team.getByLabelText("Reviewer to ask (request)"), "reviewer");
    await press(user, team.getByRole("button", { name: "Request support approval" }));
    expect((await team.findByText(byContent(/^Recorded: support-request by /))).textContent).toBe(
      "Recorded: support-request by analyst@https://idp.journey.test",
    );
    const digestField = team.getByLabelText("Approved summary digest") as HTMLInputElement;
    expect(digestField.value).toBe(summaryDigest);

    // Until an approval names those bytes the hub serves nothing under them.
    const download = team.getByRole("button", { name: "Download approved support summary" });
    await enter(user, team.getByLabelText("Download the approved summary to"), journey.path("downloads/support.json"));
    await press(user, download);
    expect(
      await team.findByText("Export permission_denied — hub access refused; insufficient permissions or role revoked"),
    ).toBeTruthy();

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
    ).toMatchObject({ state: "completed", events: [{ parent: "support-request-1", evidence: summaryDigest }] });
    expect(
      await reviewer.call("PostHubReview", {
        project: PROJECT,
        id: "reviewer-tells-analyst",
        expected: 3,
        kind: "comment",
        evidence: summaryDigest,
        parent: "",
        recipient: "analyst",
        text: "Approved; download the summary from the hub.",
        release: "",
      }),
    ).toMatchObject({ state: "completed", head: 4 });

    // The person's notifications hold what was addressed to them.
    await press(user, team.getByRole("button", { name: "Load notifications" }));
    expect(await team.findByText("comment · Approved; download the summary from the hub. (from reviewer)")).toBeTruthy();

    // The history search finds the decisions about this summary after the
    // policy was announced; a query naming the evidence by anything but its
    // digest is refused before the hub is asked; the notification search
    // finds what was addressed to the person.
    const search = within(team.getByRole("form", { name: "Search history and notifications" }));
    await enter(user, search.getByLabelText("Text contains"), "support");
    await enter(user, search.getByLabelText("Evidence (whole SHA-256 digest)"), summaryDigest);
    await enter(user, search.getByLabelText("After sequence"), "1");
    await press(user, search.getByRole("button", { name: "Search history" }));
    expect(await search.findByText("History matching: 2 (head 4)")).toBeTruthy();
    expect(search.getAllByRole("listitem").map((item) => item.textContent)).toEqual([
      `#2 support-request by analyst@https://idp.journey.test · evidence ${summaryDigest.slice(0, 12)}… — support`,
      `#3 support-approval by reviewer@https://idp.journey.test · evidence ${summaryDigest.slice(0, 12)}… — support`,
    ]);
    await enter(user, search.getByLabelText("Evidence (whole SHA-256 digest)"), "published-support");
    await press(user, search.getByRole("button", { name: "Search history" }));
    expect(
      await search.findByText("name the evidence by its whole SHA-256 digest: 64 lowercase hexadecimal characters"),
    ).toBeTruthy();
    await enter(user, search.getByLabelText("Text contains"), "download");
    await enter(user, search.getByLabelText("Evidence (whole SHA-256 digest)"), "");
    await enter(user, search.getByLabelText("After sequence"), "");
    await press(user, search.getByRole("button", { name: "Search notifications" }));
    expect(await search.findByText("Notifications matching: 1 (head 4)")).toBeTruthy();
    expect(search.getAllByRole("listitem").map((item) => item.textContent)).toEqual([
      `#4 comment by reviewer@https://idp.journey.test · evidence ${summaryDigest.slice(0, 12)}… — Approved; download the summary from the hub.`,
    ]);

    // A digest no approval names is refused, though the hub holds its bytes:
    // the policy's own digest.
    await enter(user, digestField, policyDigest);
    await press(user, download);
    await waitFor(() => expect(journey.callsTo("DownloadHubExport")).toHaveLength(2));
    await waitFor(() =>
      expect(journey.callsTo("DownloadHubExport").at(-1)?.result).toMatchObject({ state: "permission_denied" }),
    );
    expect(() => journey.readFile("downloads/support.json")).toThrow();

    // The approved digest downloads the summary, byte for byte.
    await enter(user, digestField, summaryDigest);
    await press(user, download);
    expect(await team.findByText(byContent(/^Export completed — /))).toBeTruthy();
    expect(journey.callsTo("DownloadHubExport").at(-1)?.result).toMatchObject({ state: "completed", digest: summaryDigest });
    expect(journey.readFile("downloads/support.json")).toBe(summary);
  },
);

/** The hub panel's operator-only mode, opened from its disclosure. */
async function operatorMode(user: UserEvent) {
  const section = hubPanel().getByRole("region", { name: "Operator-only hub" });
  const disclosure = within(section).getByRole("button", { name: "Operator-only hub" });
  if (disclosure.getAttribute("aria-expanded") !== "true") await press(user, disclosure);
  return within(section);
}

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
    const operator = await operatorMode(user);
    const choose = operator.getByRole("button", { name: "Choose operator-only hub configuration…" });
    const configTitle = "Choose the operator-only hub configuration";
    const storeTitle = "Choose the file to store in the operator-only hub";
    const saveTitle = "Name the file to save the artifact as";
    const outcome = () => operator.getByText(/^(Store|Read):/).textContent;
    const readAs = async (address: string, name: string) => {
      await enter(user, operator.getByLabelText("Artifact digest (SHA-256)"), address);
      await journey.nameNewFolder(journey.path(`received/${name}`), saveTitle);
      const asked = journey.callsTo("ReadOperatorHubArtifact").length;
      await press(user, operator.getByRole("button", { name: "Read and save…" }));
      await waitFor(() => expect(journey.callsTo("ReadOperatorHubArtifact")[asked]?.settled).toBe(true));
      return journey.callsTo("ReadOperatorHubArtifact")[asked]?.result;
    };
    const store = async (file: string) => {
      await journey.chooseFiles([journey.path(file)], storeTitle);
      const asked = journey.callsTo("StoreOperatorHubArtifact").length;
      await press(user, operator.getByRole("button", { name: "Store a file…" }));
      await waitFor(() => expect(journey.callsTo("StoreOperatorHubArtifact")[asked]?.settled).toBe(true));
      return journey.callsTo("StoreOperatorHubArtifact")[asked]?.result;
    };

    // A dismissed dialog chooses nothing, and a team hub's configuration is
    // not an operator-only one.
    await journey.dismissDialog("files", configTitle);
    await press(user, choose);
    await waitFor(() => expect(journey.callsTo("ChooseOperatorHubConfig").at(-1)?.result).toMatchObject({ state: "cancelled" }));
    expect(operator.getByText("No operator-only hub configuration chosen.")).toBeTruthy();
    await journey.chooseFiles([`${hub.clientConfigFolder}/hub-client.json`], configTitle);
    await press(user, choose);
    expect(await operator.findByText("not an operator-only hub configuration this release reads")).toBeTruthy();

    // The operator's configuration is chosen, and connecting is its own act.
    await journey.chooseFiles([hub.operatorConfig], configTitle);
    await press(user, choose);
    expect(await operator.findByText(`Operator-only configuration: ${hub.operatorConfig}`)).toBeTruthy();
    await press(user, operator.getByRole("button", { name: "Connect to operator-only hub" }));
    expect(await operator.findByText(`Connected to operator-only hub (https://${hub.address})`)).toBeTruthy();

    // With no license activated on this machine, storing is refused by the
    // window's own admission before any dialog opens.
    await press(user, operator.getByRole("button", { name: "Store a file…" }));
    expect(await operator.findByText("operation activation is missing or invalid; select and activate an operation policy")).toBeTruthy();
    expect(outcome()).toBe("Store: permission_denied");

    // Activated, the file is stored under its digest, and read back byte for
    // byte into a new file named in the save dialog, with the custody notice.
    await activateLicense(user, journey);
    expect(await store("handover/probe.txt")).toMatchObject({ state: "completed", digest, size: probe.length });
    expect(outcome()).toBe(`Store: completed (${probe.length} bytes)`);
    expect(operator.getByText(digest)).toBeTruthy();
    expect(await readAs(digest, "probe.txt")).toMatchObject({ state: "completed", digest });
    expect(operator.getByText(byContent(/^Saved as: .*received\/probe\.txt$/))).toBeTruthy();
    expect(operator.getAllByText("Downloaded copies remain under local custody and cannot be revoked.").length).toBeGreaterThan(0);
    expect(journey.readFile("received/probe.txt")).toBe(probe);

    // A digest typed in capitals is not the whole lowercase digest: it is
    // refused before any dialog opens, and stays in the field to correct.
    await enter(user, operator.getByLabelText("Artifact digest (SHA-256)"), digest.toUpperCase());
    await press(user, operator.getByRole("button", { name: "Read and save…" }));
    expect(await operator.findByText("an artifact is named by its whole SHA-256 digest: 64 lowercase hexadecimal characters")).toBeTruthy();
    expect(outcome()).toBe("Read: failed");

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
    expect(outcome()).toBe("Read: permission_denied");
    expect(await store("handover/probe.txt")).toMatchObject({ state: "permission_denied" });
    for (const name of ["team.txt", "closed.txt"]) expect(() => journey.readFile(`received/${name}`)).toThrow();

    // Disconnecting ends the connection and keeps the custody notice.
    await press(user, operator.getByRole("button", { name: "Disconnect from operator-only hub" }));
    expect(await operator.findByText("Not connected")).toBeTruthy();
    expect(operator.queryByRole("button", { name: "Store a file…" })).toBeNull();
    expect(journey.callsTo("DisconnectOperatorHub").at(-1)?.result).toMatchObject({
      connected: false,
      custody_warning: "Downloaded copies remain under local custody and cannot be revoked.",
    });
    expect(operator.getByRole("note").textContent).toBe("Custody Notice: Downloaded copies remain under local custody and cannot be revoked.");
  },
);
