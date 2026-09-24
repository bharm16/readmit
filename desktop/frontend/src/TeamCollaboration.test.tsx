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

  await user.click(screen.getByRole("button", { name: /Load review history/i }));
  expect(facade.callsTo("ListHubReviews").length).toBe(1);
  expect(await screen.findByText(/Assigned for review/i)).toBeTruthy();
  expect(screen.getByText(/doctor@hospital\.org/i)).toBeTruthy();

  await user.type(screen.getByLabelText(/Evidence digest/i), evidence);
  await user.type(screen.getByLabelText(/Recipient subject/i), "reviewer@hospital.org");
  await user.click(screen.getByRole("button", { name: /Submit review decision/i }));
  expect(facade.callsTo("PostHubReview").length).toBe(1);
  expect(captured?.evidence).toBe(evidence);
  expect(captured?.kind).toBe("comment");
  expect(captured?.expected).toBe(1);
  expect(await screen.findByText(/Posted team comment/i)).toBeTruthy();
  expect(screen.getByText(/Review history \(head 2\)/i)).toBeTruthy();
  // The history shown after the decision is the hub's whole history, read
  // again, not only the one event the post answered with.
  expect(facade.callsTo("ListHubReviews").length).toBe(2);
  expect(screen.getByText(/Assigned for review/i)).toBeTruthy();

  await user.click(screen.getByRole("button", { name: /Explain downloaded-copy limits/i }));
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
  await user.click(screen.getByRole("button", { name: /Load review history/i }));
  expect(await screen.findByText(/Review history \(head 0\)/i)).toBeTruthy();
  await user.type(screen.getByLabelText(/Evidence digest/i), evidence);
  await user.click(screen.getByRole("button", { name: /Submit review decision/i }));

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
      return resolved;
    },
    SaveHubOfflineDraft: async () => drafts,
  });

  render(<TeamCollaboration project="cardio-study" workspace="/workspace-under-test" />);

  await user.click(screen.getByRole("button", { name: /Load lifecycle and tips/i }));
  expect(await screen.findByText(/edit-a, edit-b/i)).toBeTruthy();
  expect(screen.getByText(/Do not overwrite silently/i)).toBeTruthy();

  const kindSelects = screen.getAllByLabelText(/^Kind$/i);
  await user.selectOptions(kindSelects[1]!, "resolve");
  const lifeIds = screen.getAllByDisplayValue("life-cmd-1");
  await user.clear(lifeIds[0]!);
  await user.type(lifeIds[0]!, "resolve-1");
  await user.type(screen.getByLabelText(/Artifact digest/i), evidence);
  await user.type(screen.getByLabelText(/Parents \(comma-separated tip ids\)/i), "edit-a, edit-b");
  await user.click(screen.getByRole("button", { name: /Submit lifecycle command/i }));
  expect(facade.callsTo("PostHubLifecycle").length).toBe(1);

  await user.type(screen.getByLabelText(/Local edited path/i), "/workspace-under-test/offline.bin");
  await user.click(screen.getByRole("button", { name: /Retain offline draft/i }));
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

  await user.selectOptions(screen.getByLabelText(/Sharing policy entry/i), "sharing-policy.json");
  await user.selectOptions(screen.getByLabelText(/Published support bundle/i), "support-bundle");
  await user.type(screen.getByLabelText(/Support command id/i), "sup-1");
  await user.type(screen.getByLabelText(/Reviewer to ask/i), "reviewer@hospital.org");

  await user.click(screen.getByRole("button", { name: /Announce sharing policy/i }));
  const announced = facade.callsTo("PostHubSupportReview")[0]?.args[0] as HubSupportReviewRequest;
  expect(announced.kind).toBe("support-policy");
  expect(announced.entry).toBe("sharing-policy.json");
  expect(announced.recipient).toBe("");
  expect(await screen.findByText(/Recorded: support-policy by author@hospital.org@https:\/\/idp\.example/)).toBeTruthy();

  await user.click(screen.getByRole("button", { name: /Request support approval/i }));
  const asked = facade.callsTo("PostHubSupportReview")[1]?.args[0] as HubSupportReviewRequest;
  expect(asked.kind).toBe("support-request");
  expect(asked.entry).toBe("support-bundle");
  expect(asked.recipient).toBe("reviewer@hospital.org");

  await user.click(screen.getByRole("button", { name: /Approve this summary/i }));
  const approved = facade.callsTo("PostHubSupportReview")[2]?.args[0] as HubSupportReviewRequest;
  expect(approved.kind).toBe("support-approval");
  expect(approved.recipient).toBe("");

  // The digest input is filled by the journey; an authorized user could also
  // name a digest a teammate's approval recorded — the hub decides either way.
  expect((screen.getByLabelText(/Approved summary digest/i) as HTMLInputElement).value).toBe("e".repeat(64));
  await user.type(screen.getByLabelText(/Download the approved summary to/i), "/downloads/support.json");
  await user.click(screen.getByRole("button", { name: /Download approved support summary/i }));
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
  const form = within(screen.getByRole("form", { name: "Search history and notifications" }));

  await user.type(form.getByLabelText("After sequence"), "-1");
  await user.click(form.getByRole("button", { name: "Search history" }));
  expect((await form.findByRole("alert")).textContent).toBe("Search after a sequence number: a whole number, 0 or more.");
  expect(facade.calls).toEqual([]);

  // Typed, then submitted with Enter from the last field.
  await user.clear(form.getByLabelText("After sequence"));
  await user.type(form.getByLabelText("After sequence"), "1");
  await user.type(form.getByLabelText("Text contains"), "reschedule");
  await user.type(form.getByLabelText("Evidence (whole SHA-256 digest)"), `${evidence}{Enter}`);
  const query = { project: "cardio-study", after: 1, text: "reschedule", evidence };
  expect(await form.findByText("History matching: 1 (head 3)")).toBeTruthy();
  expect(facade.oneCall("SearchHubReviews")).toEqual([query]);
  expect(form.queryByRole("alert")).toBeNull();
  expect(form.getAllByRole("listitem").map((item) => item.textContent)).toEqual([
    `#3 comment by reviewer@hospital.org@https://idp.example · evidence ${evidence.slice(0, 12)}… — Confirmed the reschedule`,
  ]);

  // The notifications, by the keyboard: the same query, only what is addressed to this person.
  await tabTo(user, form.getByRole("button", { name: "Search notifications" }));
  await user.keyboard("[Space]");
  expect(await form.findByText("Notifications matching: 0 (head 3)")).toBeTruthy();
  expect(form.getByText("Nothing recorded matches this search.")).toBeTruthy();
  expect(facade.oneCall("SearchHubNotifications")).toEqual([query]);

  // A query the application refuses says why, and shows no matches.
  const refused = "name the evidence by its whole SHA-256 digest: 64 lowercase hexadecimal characters";
  facade.reply({ SearchHubReviews: async () => ({ state: "failed", project: "cardio-study", reason: refused }) });
  await user.clear(form.getByLabelText("Evidence (whole SHA-256 digest)"));
  await user.type(form.getByLabelText("Evidence (whole SHA-256 digest)"), "the reschedule message");
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
  const download = screen.getByRole("button", { name: "Download approved support summary" });
  expect(download.hasAttribute("disabled")).toBe(true);

  await user.type(screen.getByLabelText("Approved summary digest"), "d".repeat(64));
  await user.type(screen.getByLabelText("Download the approved summary to"), "/workspace-under-test/support.json");
  await user.click(download);
  expect(await screen.findByText("Export permission_denied — hub access refused; insufficient permissions or role revoked")).toBeTruthy();
  expect(screen.queryByText(/Export completed/)).toBeNull();

  await user.clear(screen.getByLabelText("Approved summary digest"));
  await user.type(screen.getByLabelText("Approved summary digest"), approved);
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
