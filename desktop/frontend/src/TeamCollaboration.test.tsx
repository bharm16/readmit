import { expect, test } from "vitest";
import { render, screen } from "@testing-library/react";
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
    ListHubReviews: async () => history,
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

  await user.click(screen.getByRole("button", { name: /Explain downloaded-copy limits/i }));
  expect(facade.callsTo("ExplainHubCustody").length).toBe(1);
  expect(await screen.findByText(/already-downloaded files/i)).toBeTruthy();

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
