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
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { UserEvent } from "@testing-library/user-event";
import { byContent, enter, Journey, press, region } from "../testkit/journey";
import type { Hub } from "../testkit/hub.js";
import { activateLicense, savedAckTest } from "./steps";

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
 * person's browser completes it. */
async function signIn(user: UserEvent, hub: Hub, subject: string) {
  const panel = hubPanel();
  await press(user, await panel.findByRole("button", { name: "Sign in with Customer IdP" }));
  const login = await panel.findByRole("link", { name: "Open login window" });
  expect(await hub.signIn(login.getAttribute("href") ?? "", subject)).toBe(200);
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
    const baseline = within(region("Inspector").querySelector("section.baseline-panel") as HTMLElement);
    await user.click(baseline.getByLabelText("Release a test version with profile pins"));
    await enter(user, baseline.getByLabelText("Stable test identity"), "reschedule-accepted");
    await enter(user, baseline.getByLabelText("Candidate specification in this workspace"), "reschedule-ack-test.json");
    await press(user, baseline.getByRole("button", { name: "Review test and profile changes" }));
    await enter(user, await baseline.findByLabelText("Local approver"), "analyst");
    await enter(user, baseline.getByLabelText("Approval rationale"), "Reschedule acknowledgement expectation for release.");
    await enter(user, baseline.getByLabelText("New released test filename"), "reschedule-release-1.json");
    await press(user, baseline.getByRole("button", { name: "Release this exact test version" }));
    expect(await baseline.findByText("Approved and saved reschedule-release-1.json.")).toBeTruthy();

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
