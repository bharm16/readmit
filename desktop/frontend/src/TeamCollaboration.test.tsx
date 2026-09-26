import { expect, test } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { TeamCollaboration } from "./TeamCollaboration";
import { installFacade, uninstallFacade } from "./testkit/wails";
import type {
  HubReviewsResult,
  HubLifecycleResult,
  HubResult,
  HubTransferResult,
  EditorDraftsResult,
  HubReviewCommandRequest,
  HubSupportReviewRequest,
  HubReviewEventView,
  HubLifecycleEventView,
} from "./bindings";

const evidence = "a".repeat(64);

test("TeamCollaboration loads history, posts a comment with service identity, and shows custody limits", async () => {
  const user = userEvent.setup();
  const history: HubReviewsResult = {
    state: "completed",
    project: "cardio-study",
    head: 1,
    warning: "Downloaded copies remain under local custody and cannot be revoked.",
    events: [
      {
        schema: "readmit-hub-review-event/v1",
        project: "cardio-study",
        sequence: 1,
        issuer: "https://idp.example",
        actor: "doctor@hospital.org",
        at: "2026-09-21T12:00:00Z",
        kind: "assignment",
        evidence,
        recipient: "reviewer@hospital.org",
        text: "Assigned for review",
        command_id: "assign-1",
      },
    ],
  };
  const posted: HubReviewsResult = {
    state: "completed",
    project: "cardio-study",
    head: 2,
    events: [
      {
        schema: "readmit-hub-review-event/v1",
        project: "cardio-study",
        sequence: 2,
        issuer: "https://idp.example",
        actor: "doctor@hospital.org",
        at: "2026-09-21T12:01:00Z",
        kind: "comment",
        evidence,
        recipient: "reviewer@hospital.org",
        text: "Posted team comment",
        command_id: "review-cmd-1",
      },
    ],
  };
  const custody: HubResult = {
    state: "completed",
    connected: false,
    authenticated: false,
    custody_warning: "Downloaded copies remain under local custody and cannot be revoked.",
    reason:
      "Deleting a server grant or removing a user refuses new authorized requests; already-downloaded files and local authorized exports remain under local custody and cannot be revoked by the hub.",
  };

  let captured: HubReviewCommandRequest | undefined;
  const facade = installFacade({
    // Once the decision is recorded, the hub's history holds both events.
    ListHubReviews: async () =>
      captured ? { ...history, head: 2, events: [...(history.events ?? []), ...(posted.events ?? [])] } : history,
    PostHubReview: async (req) => {
      captured = req;
      return posted;
    },
    ExplainHubCustody: async () => custody,
  });

  render(<TeamCollaboration project="cardio-study" />);

  await user.click(screen.getByRole("button", { name: /Review history/i }));
  expect(facade.callsTo("ListHubReviews").length).toBe(1);
  expect(await screen.findByText(/Assigned for review/i)).toBeTruthy();
  expect(screen.getByText(/doctor@hospital\.org/i)).toBeTruthy();

  const decision = within(screen.getByRole("group", { name: "Review actions" }));
  await user.type(decision.getByLabelText("Evidence SHA-256"), evidence);
  await user.type(decision.getByLabelText("Recipient subject ID"), "reviewer@hospital.org");
  await user.click(screen.getByRole("button", { name: /Post comment/i }));
  expect(facade.callsTo("PostHubReview").length).toBe(1);
  expect(captured?.evidence).toBe(evidence);
  expect(captured?.kind).toBe("comment");
  expect(captured?.expected).toBe(1);
  // A comment carries no release; its command ID is a new one in the hub's grammar.
  expect(captured?.release).toBe("");
  expect(captured?.id).toMatch(/^review-[0-9a-f]{16}$/);
  expect(await screen.findByText(/Posted team comment/i)).toBeTruthy();
  expect(screen.getByText(/Review history \(head 2\)/i)).toBeTruthy();
  // The history shown after the decision is the hub's whole history, read
  // again, not only the one event the post answered with.
  expect(facade.callsTo("ListHubReviews").length).toBe(2);
  expect(screen.getByText(/Assigned for review/i)).toBeTruthy();

  await user.click(screen.getByRole("button", { name: /Download limits/i }));
  expect(facade.callsTo("ExplainHubCustody").length).toBe(1);
  expect(await screen.findByText(/already-downloaded files/i)).toBeTruthy();

  uninstallFacade();
});

test("TeamCollaboration shows a recorded decision, never a refusal, when the history cannot be read again", async () => {
  const user = userEvent.setup();
  const event = {
    schema: "readmit-hub-review-event/v1",
    project: "cardio-study",
    issuer: "https://idp.example",
    actor: "doctor@hospital.org",
    at: "2026-09-21T12:01:00Z",
    kind: "comment",
    evidence,
  };
  let posted = false;
  installFacade({
    ListHubReviews: async () =>
      posted
        ? { state: "failed", project: "cardio-study", reason: "hub unreachable; retry the read" }
        : { state: "completed", project: "cardio-study", head: 0, events: [] },
    PostHubReview: async () => {
      posted = true;
      return { state: "completed", project: "cardio-study", head: 1, events: [{ ...event, sequence: 1, text: "Recorded once", command_id: "review-cmd-1" }] };
    },
  });

  render(<TeamCollaboration project="cardio-study" />);
  await user.click(screen.getByRole("button", { name: /Review history/i }));
  expect(await screen.findByText(/Review history \(head 0\)/i)).toBeTruthy();
  await user.type(within(screen.getByRole("group", { name: "Review actions" })).getByLabelText("Evidence SHA-256"), evidence);
  await user.click(screen.getByRole("button", { name: /Post comment/i }));

  expect(await screen.findByText(/Recorded once/i)).toBeTruthy();
  expect(screen.getByText(/Review history \(head 1\)/i)).toBeTruthy();
  expect(screen.queryByText(/hub unreachable/i)).toBeNull();

  uninstallFacade();
});

test("TeamCollaboration shows concurrent tips, resolves explicitly, and retains an offline draft", async () => {
  const user = userEvent.setup();
  const life: HubLifecycleResult = {
    state: "completed",
    project: "cardio-study",
    head: 2,
    warning: "Downloaded copies remain under local custody and cannot be revoked.",
    tips: { "case-one": ["edit-a", "edit-b"] },
  };
  const resolved: HubLifecycleResult = {
    state: "completed",
    project: "cardio-study",
    head: 3,
    warning: "Downloaded copies remain under local custody and cannot be revoked.",
    tips: { "case-one": ["resolve-1"] },
    event: {
      schema: "readmit-hub-lifecycle-event/v1",
      project: "cardio-study",
      sequence: 3,
      issuer: "https://idp.example",
      actor: "doctor@hospital.org",
      at: "2026-09-21T12:05:00Z",
      kind: "resolve",
      resource: "case-one",
      artifact: evidence,
      parents: ["edit-a", "edit-b"],
      reason: "keep both as new revision",
      command_id: "resolve-1",
    },
  };
  const drafts: EditorDraftsResult = {
    state: "completed",
    drafts: [
      {
        id: "draft-1",
        kind: "hub-revision",
        workspace: "/workspace-under-test",
        case: "case-one",
        identity: "cardio-study",
        content_schema: "readmit-hub-revision-draft/v1",
        content: "{}",
      },
    ],
  };

  const facade = installFacade({
    ListHubLifecycle: async () => life,
    PostHubLifecycle: async (req) => {
      expect(req.kind).toBe("resolve");
      expect(req.parents).toEqual(["edit-a", "edit-b"]);
      expect(req).toMatchObject({ resource: "case-one", artifact: evidence, subject: "", until: "", reason: "keep both as new revision", expected: 2 });
      return resolved;
    },
    SaveHubOfflineDraft: async () => drafts,
  });

  render(<TeamCollaboration project="cardio-study" workspace="/workspace-under-test" />);

  await user.click(screen.getByRole("tab", { name: "Revisions" }));
  await user.click(screen.getByRole("button", { name: /Version history/i }));
  expect(await screen.findByText(/edit-a, edit-b/i)).toBeTruthy();
  expect(screen.getByText(/Do not overwrite silently/i)).toBeTruthy();
  expect(screen.getByText("Event head: 2")).toBeTruthy();

  const revisions = within(screen.getByRole("group", { name: "Revisions" }));
  await user.selectOptions(revisions.getByLabelText("Action"), "resolve");
  await user.type(revisions.getByLabelText("Resource ID"), "case-one");
  await user.type(revisions.getByLabelText("Artifact SHA-256"), evidence);
  await user.type(revisions.getByLabelText("Parent revision IDs"), "edit-a, edit-b");
  await user.type(revisions.getByLabelText("Reason"), "keep both as new revision");
  await user.click(screen.getByRole("button", { name: /Resolve conflict/i }));
  expect(facade.callsTo("PostHubLifecycle").length).toBe(1);
  // The hub's recorded event is shown as it answered: kind, actor, resource and event number.
  expect(await screen.findByText("Recorded resolve by doctor@hospital.org@https://idp.example for case-one · event #3 · command resolve-1")).toBeTruthy();

  await user.type(screen.getByLabelText("Edited file"), "/workspace-under-test/offline.bin");
  await user.click(screen.getByRole("button", { name: /Save offline draft/i }));
  expect(facade.callsTo("SaveHubOfflineDraft").length).toBe(1);
  expect(await screen.findByText(/Drafts retained: 1/i)).toBeTruthy();

  uninstallFacade();
});

// The sharing journey from the hub panel above the privacy screens: the
// sharing policy is announced by its exact bytes, a published support bundle's
// summary is requested for a named reviewer, the summary is approved, and the
// approved digest downloads through the hub's gated export route. The privacy
// panel's local approval inputs are separate components and receive nothing.
test("a sharing policy is announced, a summary requested and approved, and the approved bytes downloaded", async () => {
  const user = userEvent.setup();
  const recorded = (kind: string, req: HubSupportReviewRequest): HubReviewsResult => ({
    state: "completed",
    project: req.project,
    head: 7,
    events: [
      {
        schema: "readmit-hub-review-event/v2",
        project: req.project,
        sequence: 7,
        issuer: "https://idp.example",
        actor: "author@hospital.org",
        at: "2026-09-21T12:00:00Z",
        kind,
        evidence: kind === "support-policy" ? "d".repeat(64) : "e".repeat(64),
        recipient: req.recipient,
        text: "support",
        command_id: req.id,
      },
    ],
  });
  const downloaded: HubTransferResult = {
    state: "completed",
    transfer_state: "completed",
    digest: "e".repeat(64),
    size: 512,
    path: "/downloads/support.json",
    warning: "Downloaded copies remain under local custody and cannot be revoked.",
  };
  const facade = installFacade({
    PostHubSupportReview: async (req) => recorded(req.kind, req),
    DownloadHubExport: async (req) => {
      expect(req.digest).toBe("e".repeat(64));
      return downloaded;
    },
  });

  const entries = [
    { name: "sharing-policy.json", kind: "sharing-policy" as const },
    { name: "support-bundle", kind: "support" as const },
  ];
  render(<TeamCollaboration project="cardio-study" workspace="/workspace-under-test" entries={entries} />);

  await user.click(screen.getByRole("tab", { name: "Support approvals" }));
  await user.selectOptions(screen.getByLabelText("Sharing policy file"), "sharing-policy.json");
  await user.selectOptions(screen.getByLabelText("Support bundle"), "support-bundle");
  await user.type(screen.getByLabelText("Reviewer subject ID"), "reviewer@hospital.org");

  await user.click(screen.getByRole("button", { name: /Announce policy/i }));
  const announced = facade.callsTo("PostHubSupportReview")[0]?.args[0] as HubSupportReviewRequest;
  expect(announced.kind).toBe("support-policy");
  expect(announced.entry).toBe("sharing-policy.json");
  expect(announced.recipient).toBe("");
  expect(await screen.findByText(/Recorded: support-policy by author@hospital.org@https:\/\/idp\.example/)).toBeTruthy();

  await user.click(screen.getByRole("button", { name: /Request approval/i }));
  const asked = facade.callsTo("PostHubSupportReview")[1]?.args[0] as HubSupportReviewRequest;
  expect(asked.kind).toBe("support-request");
  expect(asked.entry).toBe("support-bundle");
  expect(asked.recipient).toBe("reviewer@hospital.org");

  await user.click(screen.getByRole("button", { name: /Approve summary/i }));
  const approved = facade.callsTo("PostHubSupportReview")[2]?.args[0] as HubSupportReviewRequest;
  expect(approved.kind).toBe("support-approval");
  expect(approved.recipient).toBe("");
  // Three deliberate commands, three command IDs.
  expect(new Set([announced.id, asked.id, approved.id]).size).toBe(3);
  expect(screen.getByText("Approval status: approved in this window.")).toBeTruthy();

  // The digest input is filled by the journey; an authorized user could also
  // name a digest a teammate's approval recorded — the hub decides either way.
  expect((screen.getByLabelText("Summary SHA-256") as HTMLInputElement).value).toBe("e".repeat(64));
  await user.type(screen.getByLabelText("Summary download file"), "/downloads/support.json");
  await user.click(screen.getByRole("button", { name: /Download summary/i }));
  expect(facade.callsTo("DownloadHubExport").length).toBe(1);
  expect(await screen.findByText(/Export completed — \/downloads\/support\.json/)).toBeTruthy();

  uninstallFacade();
});

/** One recorded comment, as the hub reports it back. */
function comment(sequence: number, actor: string, recipient: string, text: string, commandId: string): HubReviewEventView {
  return {
    schema: "readmit-hub-review-event/v1",
    project: "cardio-study",
    sequence,
    issuer: "https://idp.example",
    actor,
    at: "2026-09-23T10:00:00Z",
    kind: "comment",
    evidence,
    recipient,
    text,
    command_id: commandId,
  };
}

/** Moves the keyboard focus to a control the way a person does, once the
 * panel offers it again: from the top of the window, one Tab at a time. */
async function tabTo(user: ReturnType<typeof userEvent.setup>, control: HTMLElement): Promise<void> {
  await waitFor(() => expect(control.hasAttribute("disabled")).toBe(false));
  await user.click(document.body);
  for (let presses = 0; presses < 60 && document.activeElement !== control; presses++) {
    await user.tab();
  }
  expect(document.activeElement).toBe(control);
}

// The notifications are what the hub recorded as addressed to the signed-in
// person. Loading them is the person's own act, from the pointer or the
// keyboard; an empty list says so, and a refusal says why instead of showing
// an empty list.
test("notifications addressed to the signed-in person are listed, an empty list says so, and a refusal says why", async () => {
  const user = userEvent.setup();
  const answers: HubReviewsResult[] = [
    { state: "completed", project: "cardio-study", head: 3, events: [comment(2, "reviewer@hospital.org", "doctor@hospital.org", "Confirmed on the lab fixture", "reviewer-confirms")] },
    { state: "completed", project: "cardio-study", head: 3, events: [] },
    { state: "permission_denied", project: "cardio-study", reason: "sign-in required or session expired" },
  ];
  const facade = installFacade({ ListHubNotifications: async () => answers.shift()! });
  render(<TeamCollaboration project="cardio-study" />);
  // Switching task asks nothing of the hub.
  await user.click(screen.getByRole("tab", { name: "Notifications" }));
  expect(facade.calls).toEqual([]);

  const load = screen.getByRole("button", { name: "Load notifications" });
  await user.click(load);
  expect(await screen.findByText("comment · Confirmed on the lab fixture (from reviewer@hospital.org)")).toBeTruthy();
  expect(screen.queryByText("Nothing in this project is addressed to you.")).toBeNull();

  await tabTo(user, load);
  await user.keyboard("{Enter}");
  expect(await screen.findByText("Nothing in this project is addressed to you.")).toBeTruthy();
  expect(screen.queryByText(/Confirmed on the lab fixture/)).toBeNull();

  await tabTo(user, load);
  await user.keyboard("[Space]");
  expect(await screen.findByText("sign-in required or session expired")).toBeTruthy();
  expect(screen.queryByText("Nothing in this project is addressed to you.")).toBeNull();
  expect(facade.callsTo("ListHubNotifications").map((call) => call.args)).toEqual([["cardio-study"], ["cardio-study"], ["cardio-study"]]);

  uninstallFacade();
});

// History and notifications are searched only when the person searches, with
// exactly the query they typed. A sequence that is not a whole number is
// refused before anything is asked; a refusal the application or the hub
// gives is shown with its reason; the search is reachable from the keyboard.
test("history and notifications are searched with the person's query, and a query that cannot be asked is refused with why", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    SearchHubReviews: async () => ({
      state: "completed",
      project: "cardio-study",
      head: 3,
      events: [comment(3, "reviewer@hospital.org", "doctor@hospital.org", "Confirmed the reschedule", "reviewer-confirms")],
    }),
    SearchHubNotifications: async () => ({ state: "completed", project: "cardio-study", head: 3, events: [] }),
  });
  render(<TeamCollaboration project="cardio-study" />);
  let form = within(screen.getByRole("form", { name: "Search team activity" }));

  await user.type(form.getByLabelText("After event number"), "-1");
  await user.click(form.getByRole("button", { name: "Search history" }));
  expect((await form.findByRole("alert")).textContent).toBe("Search after a sequence number: a whole number, 0 or more.");
  expect(facade.calls).toEqual([]);

  // Typed, then submitted with Enter from the last field.
  await user.clear(form.getByLabelText("After event number"));
  await user.type(form.getByLabelText("After event number"), "1");
  await user.type(form.getByLabelText("Search text"), "reschedule");
  await user.type(form.getByLabelText("Evidence SHA-256"), `${evidence}{Enter}`);
  const query = { project: "cardio-study", after: 1, text: "reschedule", evidence };
  expect(await form.findByText("History matching: 1 (head 3)")).toBeTruthy();
  expect(facade.oneCall("SearchHubReviews")).toEqual([query]);
  expect(form.queryByRole("alert")).toBeNull();
  expect(form.getAllByRole("listitem").map((item) => item.textContent)).toEqual([
    `#3 comment by reviewer@hospital.org@https://idp.example · evidence ${evidence.slice(0, 12)}… — Confirmed the reschedule`,
  ]);

  // The notifications, by the keyboard: the same query, only what is addressed to this person.
  await user.click(screen.getByRole("tab", { name: "Notifications" }));
  form = within(screen.getByRole("form", { name: "Search team activity" }));
  await tabTo(user, form.getByRole("button", { name: "Search notifications" }));
  await user.keyboard("[Space]");
  expect(await form.findByText("Notifications matching: 0 (head 3)")).toBeTruthy();
  expect(form.getByText("Nothing recorded matches this search.")).toBeTruthy();
  expect(facade.oneCall("SearchHubNotifications")).toEqual([query]);

  // A query the application refuses says why, and shows no matches.
  const refused = "name the evidence by its whole SHA-256 digest: 64 lowercase hexadecimal characters";
  facade.reply({ SearchHubReviews: async () => ({ state: "failed", project: "cardio-study", reason: refused }) });
  await user.click(screen.getByRole("tab", { name: "Reviews" }));
  form = within(screen.getByRole("form", { name: "Search team activity" }));
  await user.clear(form.getByLabelText("Evidence SHA-256"));
  await user.type(form.getByLabelText("Evidence SHA-256"), "the reschedule message");
  await user.click(form.getByRole("button", { name: "Search history" }));
  expect(await form.findByText(refused)).toBeTruthy();
  expect(form.getByText("History search did not complete")).toBeTruthy();
  expect(form.queryByText(/matching/)).toBeNull();
  expect(form.queryAllByRole("listitem")).toEqual([]);
  expect(facade.callsTo("SearchHubReviews").at(-1)?.args).toEqual([{ ...query, evidence: "the reschedule message" }]);

  uninstallFacade();
});

// The approved summary downloads only as the digest the hub's approval chain
// names. A digest it does not name is refused and shown as refused, never as
// downloaded; the corrected digest downloads, from the keyboard.
test("a support summary digest the hub refuses is reported as refused, and the approved one downloads from the keyboard", async () => {
  const user = userEvent.setup();
  const approved = "e".repeat(64);
  const facade = installFacade({
    DownloadHubExport: async (request) =>
      request.digest === approved
        ? {
            state: "completed",
            transfer_state: "completed",
            digest: approved,
            size: 512,
            path: "/workspace-under-test/support.json",
            warning: "Downloaded copies remain under local custody and cannot be revoked.",
          }
        : {
            state: "permission_denied",
            transfer_state: "permission_denied",
            reason: "hub access refused; insufficient permissions or role revoked",
          },
  });
  render(<TeamCollaboration project="cardio-study" workspace="/workspace-under-test" />);
  await user.click(screen.getByRole("tab", { name: "Support approvals" }));
  const download = screen.getByRole("button", { name: "Download summary" });
  expect(download.hasAttribute("disabled")).toBe(true);

  await user.type(screen.getByLabelText("Summary SHA-256"), "d".repeat(64));
  await user.type(screen.getByLabelText("Summary download file"), "/workspace-under-test/support.json");
  await user.click(download);
  expect(await screen.findByText("Export permission_denied — hub access refused; insufficient permissions or role revoked")).toBeTruthy();
  expect(screen.queryByText(/Export completed/)).toBeNull();

  await user.clear(screen.getByLabelText("Summary SHA-256"));
  await user.type(screen.getByLabelText("Summary SHA-256"), approved);
  await tabTo(user, download);
  await user.keyboard("{Enter}");
  expect(
    await screen.findByText(
      "Export completed — /workspace-under-test/support.json — Downloaded copies remain under local custody and cannot be revoked.",
    ),
  ).toBeTruthy();
  expect(facade.callsTo("DownloadHubExport").map((call) => call.args[0])).toEqual([
    { project: "cardio-study", digest: "d".repeat(64), destination_path: "/workspace-under-test/support.json" },
    { project: "cardio-study", digest: approved, destination_path: "/workspace-under-test/support.json" },
  ]);

  uninstallFacade();
});

// Each decision type shows and sends only the members it carries; the others
// are sent empty even when a person typed them under another type.
test("each review decision type shows only its own fields and sends only them", async () => {
  const user = userEvent.setup();
  const posted: HubReviewCommandRequest[] = [];
  installFacade({
    PostHubReview: async (request) => {
      posted.push(request);
      return { state: "failed", project: "cardio-study", reason: "refused for the test" };
    },
  });
  render(<TeamCollaboration project="cardio-study" />);
  const decision = within(screen.getByRole("group", { name: "Review actions" }));
  const fields = () => decision.getAllByRole("textbox").map((field) => field.closest("label")?.firstChild?.textContent);
  await user.type(decision.getByLabelText("Evidence SHA-256"), evidence);
  expect(fields()).toEqual(["Evidence SHA-256", "Recipient subject ID", "Parent command ID", "Comment"]);
  await user.type(decision.getByLabelText("Parent command ID"), "earlier-comment");

  await user.selectOptions(decision.getByLabelText("Decision type"), "assignment");
  expect(fields()).toEqual(["Evidence SHA-256", "Recipient subject ID", "Assignment note"]);
  await user.type(decision.getByLabelText("Recipient subject ID"), "reviewer");
  await user.type(decision.getByLabelText("Assignment note"), "Please check the reschedule");
  await user.click(decision.getByRole("button", { name: "Assign" }));
  expect(posted.at(-1)).toMatchObject({ kind: "assignment", recipient: "reviewer", parent: "", release: "", text: "Please check the reschedule" });

  await user.selectOptions(decision.getByLabelText("Decision type"), "review-request");
  expect(fields()).toEqual(["Evidence SHA-256", "Release SHA-256", "Recipient subject ID", "Rationale"]);
  await user.type(decision.getByLabelText("Release SHA-256"), "d".repeat(64));
  await user.type(decision.getByLabelText("Rationale"), "Review the release");
  await user.click(decision.getByRole("button", { name: "Request review" }));
  expect(posted.at(-1)).toMatchObject({ kind: "review-request", recipient: "reviewer", parent: "", release: "d".repeat(64) });

  await user.selectOptions(decision.getByLabelText("Decision type"), "approval");
  expect(fields()).toEqual(["Evidence SHA-256", "Release SHA-256", "Parent command ID", "Rationale"]);
  await user.type(decision.getByLabelText("Rationale"), "Approved as reviewed");
  await user.click(decision.getByRole("button", { name: "Approve review" }));
  expect(posted.at(-1)).toMatchObject({ kind: "approval", recipient: "", parent: "earlier-comment", release: "d".repeat(64), text: "Approved as reviewed" });
  uninstallFacade();
});

// A command keeps its ID while it is retried unchanged; changing what it says
// or starting a new one after it was recorded allocates a new ID, and no
// stale-head refusal writes anything by itself.
test("a retried decision keeps its command ID, and an edited or new decision gets its own", async () => {
  const user = userEvent.setup();
  const answers: HubReviewsResult[] = [
    { state: "failed", project: "cardio-study", reason: "hub unreachable; retry the request" },
    { state: "failed", project: "cardio-study", reason: "hub unreachable; retry the request" },
    { state: "failed", project: "cardio-study", reason: "the review head changed; read the history again and renew the action" },
    { state: "completed", project: "cardio-study", head: 1, events: [comment(1, "doctor@hospital.org", "", "Edited comment", "any")] },
    { state: "failed", project: "cardio-study", reason: "hub unreachable; retry the request" },
  ];
  const facade = installFacade({
    PostHubReview: async () => answers.shift()!,
    ListHubReviews: async () => ({ state: "completed", project: "cardio-study", head: 1, events: [comment(1, "doctor@hospital.org", "", "Edited comment", "any")] }),
  });
  render(<TeamCollaboration project="cardio-study" />);
  const decision = within(screen.getByRole("group", { name: "Review actions" }));
  expect(decision.getByText("A new ID is assigned when you submit.")).toBeTruthy();
  await user.type(decision.getByLabelText("Evidence SHA-256"), evidence);
  const post = decision.getByRole("button", { name: "Post comment" });
  await user.click(post);
  expect((await screen.findAllByText("hub unreachable; retry the request")).length).toBeGreaterThan(0);
  await user.click(post);
  const ids = () => facade.callsTo("PostHubReview").map((call) => (call.args[0] as HubReviewCommandRequest).id);
  await waitFor(() => expect(ids()).toHaveLength(2));
  expect(ids()[1]).toBe(ids()[0]);
  expect(decision.getByText(ids()[0]!)).toBeTruthy();

  await user.clear(decision.getByLabelText("Comment"));
  await user.type(decision.getByLabelText("Comment"), "Edited comment");
  await user.click(post);
  await waitFor(() => expect(ids()).toHaveLength(3));
  expect(ids()[2]).not.toBe(ids()[0]);
  // A stale head is reported; nothing is posted again until the person acts.
  expect((await screen.findAllByText(/the review head changed/)).length).toBeGreaterThan(0);
  expect(facade.callsTo("PostHubReview")).toHaveLength(3);

  await user.click(post);
  expect(await screen.findByText("Review history (head 1)")).toBeTruthy();
  const recorded = ids()[3]!;
  expect(recorded).toBe(ids()[2]);
  // Recorded: the next decision is a new command.
  await user.click(post);
  await waitFor(() => expect(ids()).toHaveLength(5));
  expect(ids()[4]).not.toBe(recorded);
  uninstallFacade();
});

function lifecycleEvent(sequence: number, kind: string, extra: Partial<HubLifecycleEventView> = {}): HubLifecycleEventView {
  return {
    schema: "readmit-hub-lifecycle-event/v1",
    project: "cardio-study",
    sequence,
    issuer: "https://idp.example",
    actor: "admin@hospital.org",
    at: "2026-09-23T10:00:00Z",
    kind,
    reason: "Recorded for the test",
    command_id: `${kind}-cmd`,
    ...extra,
  };
}

// Removing a user is confirmed first, naming the exact subject and what the
// hub cannot take back; the recorded event is shown even when the history
// cannot be read again afterwards.
test("removing a user is confirmed by name, and a failed history read never recasts the recorded removal", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    PostHubLifecycle: async (request) => ({
      state: "completed",
      project: "cardio-study",
      head: 4,
      event: lifecycleEvent(4, request.kind, { subject: request.subject, reason: request.reason }),
      warning: "Downloaded copies remain under local custody and cannot be revoked.",
    }),
    ListHubLifecycle: async () => ({ state: "failed", project: "cardio-study", reason: "hub unreachable; retry the read" }),
  });
  render(<TeamCollaboration project="cardio-study" capabilities={["admin"]} />);
  await user.click(screen.getByRole("tab", { name: "Administration" }));
  expect(screen.getByText(/The service grants you admin on this project/)).toBeTruthy();
  const access = within(screen.getByRole("group", { name: "Access" }));
  expect(access.getAllByRole("textbox").map((field) => field.closest("label")?.firstChild?.textContent)).toEqual(["User subject ID", "Reason"]);
  await user.type(access.getByLabelText("User subject ID"), "analyst@hospital.org");
  await user.type(access.getByLabelText("Reason"), "role ended");
  await user.click(access.getByRole("button", { name: "Remove user" }));
  expect(facade.callsTo("PostHubLifecycle")).toHaveLength(0);
  const confirm = within(access.getByRole("group", { name: "Confirm user removal" }));
  expect(confirm.getByText(/Remove analyst@hospital\.org from cardio-study\?.*cannot be revoked/)).toBeTruthy();
  await user.click(confirm.getByRole("button", { name: "Confirm removal" }));
  expect(facade.oneCall("PostHubLifecycle")[0]).toMatchObject({ kind: "remove-user", subject: "analyst@hospital.org", resource: "", artifact: "", until: "", parents: [], reason: "role ended" });
  expect(await access.findByText("Recorded remove-user by admin@hospital.org@https://idp.example for analyst@hospital.org · event #4 · command remove-user-cmd")).toBeTruthy();
  expect(screen.queryByText("hub unreachable; retry the read")).toBeNull();
  uninstallFacade();
});

// Retention names its artifact and full RFC 3339 instant; retiring asks first.
test("retention sends only its artifact, instant and reason, and retiring is confirmed", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    PostHubLifecycle: async (request) => ({ state: "completed", project: "cardio-study", event: lifecycleEvent(5, request.kind, { artifact: request.artifact }) }),
    ListHubLifecycle: async () => ({ state: "completed", project: "cardio-study", head: 5, tips: {} }),
  });
  render(<TeamCollaboration project="cardio-study" />);
  await user.click(screen.getByRole("tab", { name: "Administration" }));
  expect(screen.getByText(/has not granted you admin/)).toBeTruthy();
  const retention = within(screen.getByRole("group", { name: "Retention" }));
  await user.type(retention.getByLabelText("Artifact SHA-256"), evidence);
  await user.type(retention.getByLabelText("Retain until"), "2027-01-31T00:00:00Z");
  expect(retention.getByText(/never read as local time/)).toBeTruthy();
  await user.type(retention.getByLabelText("Reason"), "legal hold");
  await user.click(retention.getByRole("button", { name: "Set retention" }));
  expect(facade.oneCall("PostHubLifecycle")[0]).toMatchObject({ kind: "retention", artifact: evidence, until: "2027-01-31T00:00:00Z", subject: "", resource: "", reason: "legal hold" });

  await user.selectOptions(retention.getByLabelText("Action"), "retire");
  expect(retention.queryByLabelText("Retain until")).toBeNull();
  await user.click(retention.getByRole("button", { name: "Retire artifact" }));
  expect(facade.callsTo("PostHubLifecycle")).toHaveLength(1);
  await user.click(within(retention.getByRole("group", { name: "Confirm retirement" })).getByRole("button", { name: "Confirm retirement" }));
  expect(facade.callsTo("PostHubLifecycle")[1]?.args[0]).toMatchObject({ kind: "retire", artifact: evidence, until: "", subject: "" });
  uninstallFacade();
});

test("an audit export shows its review and lifecycle events and is saved only on an explicit choice", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    PostHubLifecycle: async () => ({
      state: "completed",
      project: "cardio-study",
      warning: "Downloaded copies remain under local custody and cannot be revoked.",
      audit: {
        schema: "readmit-hub-audit/v2",
        project: "cardio-study",
        review_head: 1,
        reviews: [comment(1, "reviewer@hospital.org", "", "Confirmed on the lab fixture", "reviewer-confirms")],
        lifecycle: [lifecycleEvent(2, "retention", { artifact: evidence })],
        warning: "Downloaded copies remain under local custody and cannot be revoked.",
      },
    }),
    SaveHubAudit: async () => ({
      state: "completed",
      transfer_state: "completed",
      size: 512,
      path: "/workspace-under-test/audit.json",
      warning: "Downloaded copies remain under local custody and cannot be revoked.",
    }),
  });
  render(<TeamCollaboration project="cardio-study" />);
  await user.click(screen.getByRole("tab", { name: "Administration" }));
  const audit = within(screen.getByRole("group", { name: "Audit export" }));
  await user.type(audit.getByLabelText("Reason"), "quarterly review");
  await user.click(audit.getByRole("button", { name: "Submit audit export" }));
  expect(facade.oneCall("PostHubLifecycle")[0]).toMatchObject({ kind: "audit-export", reason: "quarterly review", artifact: "", subject: "", until: "" });
  expect(await audit.findByRole("heading", { name: "Review events" })).toBeTruthy();
  expect(audit.getByText("#1 comment by reviewer@hospital.org@https://idp.example — Confirmed on the lab fixture")).toBeTruthy();
  expect(audit.getByRole("heading", { name: "Lifecycle events" })).toBeTruthy();
  expect(audit.getByText("#2 retention by admin@hospital.org@https://idp.example — Recorded for the test")).toBeTruthy();
  expect(audit.getAllByText(/cannot be revoked/).length).toBeGreaterThan(0);
  // Nothing is saved until the person chooses to.
  expect(facade.callsTo("SaveHubAudit")).toHaveLength(0);
  await user.click(audit.getByRole("button", { name: "Save audit file…" }));
  expect(facade.oneCall("SaveHubAudit")).toEqual(["cardio-study"]);
  expect(await audit.findByText(/Saved the audit export to \/workspace-under-test\/audit\.json/)).toBeTruthy();
  uninstallFacade();
});

// The event head is what the hub reported. A read that failed reports none,
// and it is shown as not reported, never as an empty history at event 0; a
// completed read of an empty history is event 0, which the facade leaves out.
test("a missing event head is shown as not reported, never as 0", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    ListHubLifecycle: async (): Promise<HubLifecycleResult> => ({ state: "failed", reason: "the hub could not be reached" }),
  });
  render(<TeamCollaboration project="cardio-study" workspace="/workspace-under-test" />);
  await user.click(screen.getByRole("tab", { name: "Revisions" }));
  await user.click(screen.getByRole("button", { name: /Version history/i }));
  expect(await screen.findByText("Event head: not reported")).toBeTruthy();
  expect(screen.queryByText("Event head: 0")).toBeNull();

  facade.reply({ ListHubLifecycle: async (): Promise<HubLifecycleResult> => ({ state: "completed", project: "cardio-study" }) });
  await user.click(screen.getByRole("button", { name: /Version history/i }));
  expect(await screen.findByText("Event head: 0")).toBeTruthy();
  uninstallFacade();
});
