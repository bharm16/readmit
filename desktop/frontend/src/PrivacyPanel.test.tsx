// The privacy journeys at the window: a derived review is prepared from the
// workspace's actual documents and reports its blockers and its identity; the
// packet is exported only under an approval naming that exact identity; and
// the value-free support summary is previewed, published under the exact
// preview identity, and verified offline. The approval inputs are cleared when
// the selection changes, so no approval survives the bytes it named, and
// nothing here ever shows a value.
import { expect, test } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { PrivacyPanel } from "./PrivacyPanel";
import { installFacade } from "./testkit/wails";
import type { FacadeHandlers } from "./testkit/wails";
import type { Artifact } from "./bindings";
import {
  PRIVACY_REVIEW_IDENTITY,
  PRIVACY_SUMMARY_IDENTITY,
  WORKSPACE_ROOT,
  privacyExportResult,
  privacyReviewResult,
  supportPolicyResult,
  supportPreviewResult,
} from "./testkit/fixtures";

const CASE_ENTRY = "original.case";
const SPEC_ENTRY = "spec.json";
const POLICY_ENTRY = "policy.json";
const INVENTORY_ENTRY = "inventory.json";
const REVIEW_ENTRY = "review";
const PRIVATE_ENTRY = "review-private";
const SHARING_ENTRY = "sharing.json";

const ENTRIES: Artifact[] = [
  { name: CASE_ENTRY, kind: "case", schema: "readmit-case/v3", provenance: "imported" },
  { name: SPEC_ENTRY, kind: "spec" },
  { name: POLICY_ENTRY, kind: "unsupported", reason: "not a case bundle this release supports" },
  { name: INVENTORY_ENTRY, kind: "unsupported", reason: "not a case bundle this release supports" },
  { name: REVIEW_ENTRY, kind: "review" },
  { name: PRIVATE_ENTRY, kind: "unsupported", reason: "not a case bundle this release supports" },
  { name: SHARING_ENTRY, kind: "sharing-policy" },
];

function renderPanel(handlers: FacadeHandlers = {}, entries: Artifact[] = ENTRIES) {
  const events: string[] = [];
  installFacade({
    Cancel: async () => {
      events.push("cancel");
    },
    ...handlers,
  });
  render(
    <PrivacyPanel
      workspace={WORKSPACE_ROOT}
      entries={entries}
      onRefresh={() => events.push("refresh")}
    />,
  );
  return { events };
}

async function selectDerivationInputs(user: ReturnType<typeof userEvent.setup>) {
  await user.selectOptions(screen.getByLabelText("Case"), CASE_ENTRY);
  await user.selectOptions(screen.getByLabelText("Original specification"), SPEC_ENTRY);
  await user.selectOptions(screen.getByLabelText("Disclosure policy"), POLICY_ENTRY);
  await user.selectOptions(screen.getByLabelText("Original-artifact inventory"), INVENTORY_ENTRY);
}

test("a derived review reports its identity and statements, and pre-fills the export selections it produced", async () => {
  const user = userEvent.setup();
  renderPanel({
    DeriveExportReview: (request) => {
      expect(request.workspace).toBe(WORKSPACE_ROOT);
      expect(request.case).toBe(CASE_ENTRY);
      expect(request.spec).toBe(SPEC_ENTRY);
      expect(request.policy).toBe(POLICY_ENTRY);
      expect(request.inventory).toBe(INVENTORY_ENTRY);
      return privacyReviewResult();
    },
  });
  await selectDerivationInputs(user);
  await user.click(screen.getByRole("button", { name: "Derive review" }));
  await waitFor(() => expect(screen.getByText(/ready-for-approval/)).toBeTruthy());
  expect(screen.getByText(PRIVACY_REVIEW_IDENTITY)).toBeTruthy();
  expect(screen.getByText(/disclosure-reviewed-extract/)).toBeTruthy();
  expect(screen.getByText(new RegExp(PRIVATE_ENTRY))).toBeTruthy();
  // The export selections are the derivation's own outputs, so the journey
  // continues without retyping anything.
  expect((screen.getByLabelText("Review to export") as HTMLSelectElement).value).toBe(REVIEW_ENTRY);
  expect((screen.getByLabelText("Its private local state") as HTMLInputElement).value).toBe(PRIVATE_ENTRY);
});

test("a blocked review shows every unresolved surface as an explicit blocker and establishes nothing", async () => {
  const user = userEvent.setup();
  renderPanel({
    DeriveExportReview: () => {
      const blocked = privacyReviewResult({
        state: "blocked",
        identity: PRIVACY_REVIEW_IDENTITY,
        findings: 42,
        unresolved: 42,
      });
      if (blocked.outcome) delete blocked.outcome.establishes;
      return blocked;
    },
    OpenReview: () => ({
      state: "completed" as const,
      review: {
        name: REVIEW_ENTRY,
        report: "readmit-export-review/v1",
        state: "blocked",
        data_origin: "derived-testing-data",
        identity: PRIVACY_REVIEW_IDENTITY,
        input_commitment: "in",
        local_state_commitment: "loc",
        policies_applied: [],
        surfaces: [
          { name: "case", content: "unmapped-field", findings: 9, unresolved: 9 },
          { name: "case", content: "unknown-segment", findings: 1, unresolved: 1 },
          { name: "spec", content: "spec-literal", findings: 6, unresolved: 6 },
        ],
        coverage: [],
        uncovered_classes: ["face-images"],
        residual_scan: { status: "not-run", files_checked: 0, known_values_checked: 0, unresolved_locations: [], limitations: "Unresolved findings block generation and scanning." },
        required_failures: [1, 2],
        original_failed_assertions: [],
        decision: "incomplete-review" as const,
        decision_reason: "this review is not complete; an incomplete review cannot authorize disclosure, whatever identity approves it",
        unresolved: 42,
        offset: 0,
        limit: 200,
        total: 42,
        findings: [],
        scope: "scope",
        boundary: "boundary",
      },
    }),
  });
  await selectDerivationInputs(user);
  await user.click(screen.getByRole("button", { name: "Derive review" }));
  await waitFor(() => expect(screen.getAllByText(/blocked/)[0]).toBeTruthy());
  // Selecting the review in the export step reads it and lists the blockers.
  await user.selectOptions(screen.getByLabelText("Review to export"), REVIEW_ENTRY);
  await waitFor(() => expect(screen.getByText(/Unresolved surfaces/)).toBeTruthy());
  expect(screen.getByText(/case · unmapped-field/)).toBeTruthy();
  expect(screen.getByText(/case · unknown-segment/)).toBeTruthy();
  expect(screen.getByText(/spec · spec-literal/)).toBeTruthy();
});

test("the packet exports only under the exact approval, and changing the review clears what was typed", async () => {
  const user = userEvent.setup();
  const exported: string[] = [];
  renderPanel({
    ExportDerivedPacket: (request) => {
      expect(request.review).toBe(REVIEW_ENTRY);
      expect(request.local_state).toBe(PRIVATE_ENTRY);
      exported.push(request.approval);
      if (request.approval !== PRIVACY_REVIEW_IDENTITY) {
        return { state: "failed" as const, reason: "export requires approval of an exact fully handled and proven review" };
      }
      return privacyExportResult();
    },
  });
  const approval = screen.getByLabelText(/Approve by naming the exact review identity/);
  expect((approval as HTMLInputElement).disabled).toBe(true);
  await user.selectOptions(screen.getByLabelText("Review to export"), REVIEW_ENTRY);
  await user.type(screen.getByLabelText("Its private local state"), PRIVATE_ENTRY);
  await user.type(screen.getByLabelText(/Approve by naming the exact review identity/), PRIVACY_REVIEW_IDENTITY);
  await user.click(screen.getByRole("button", { name: "Export packet" }));
  await waitFor(() => expect(screen.getByText(/external equivalence declined/)).toBeTruthy());
  expect(screen.getByText(/baseline assertion_failure, postfix pass/)).toBeTruthy();
  expect(exported).toEqual([PRIVACY_REVIEW_IDENTITY]);

  // Selecting a different review clears the typed approval: an approval is a
  // fresh act over the identity the bytes have now, never a retained one.
  await user.selectOptions(screen.getByLabelText("Review to export"), "");
  expect((screen.getByLabelText(/Approve by naming the exact review identity/) as HTMLInputElement).value).toBe("");
});

test("the sharing policy is authored through structured controls and the summary is previewed and published under its exact identity", async () => {
  const user = userEvent.setup();
  const published: string[] = [];
  renderPanel({
    SaveSharingPolicy: (request) => {
      expect(request.output).toBe(SHARING_ENTRY);
      expect(request.support).toBe(true);
      expect(request.destinations).toEqual(["local-file"]);
      expect(request.max_bytes).toBe(4096);
      return supportPolicyResult();
    },
    ReadSharingPolicy: (workspace, entry) => {
      expect(workspace).toBe(WORKSPACE_ROOT);
      expect(entry).toBe(SHARING_ENTRY);
      return supportPolicyResult();
    },
    PreviewSupportSummary: (request) => {
      expect(request.kind).toBe("derived-review");
      expect(request.source).toBe(REVIEW_ENTRY);
      expect(request.private).toBe(PRIVATE_ENTRY);
      expect(request.policy).toBe(SHARING_ENTRY);
      return supportPreviewResult();
    },
    PublishSupportSummary: (request) => {
      published.push(request.approval);
      if (request.approval !== PRIVACY_SUMMARY_IDENTITY) {
        return {
          state: "failed" as const,
          reason: "this approval does not name the summary the current sources and policy produce; review the current preview again and approve the identity it displays",
        };
      }
      return {
        state: "completed" as const,
        outcome: {
          bundle: "support-001",
          identity: PRIVACY_SUMMARY_IDENTITY,
          files: ["support.json", "event.json", "identity.sha256"],
          exclusions: ["No evidence payload: messages, source names, notes, specification text, run values, original reports, caches and logs are excluded."],
          no_upload: "Publishing this bundle wrote a local directory. Exporting a file is not uploading it.",
          limitations: [],
        },
      };
    },
  });
  await user.type(screen.getByLabelText("New policy document"), SHARING_ENTRY);
  await user.click(screen.getByRole("button", { name: "Save sharing policy" }));
  await waitFor(() => expect(screen.getByText(/Saved sharing\.json/)).toBeTruthy());
  await user.selectOptions(screen.getByLabelText("Sharing policy"), SHARING_ENTRY);
  await waitFor(() => expect(screen.getByText(/Support allowed · local-file · 4096 bytes/)).toBeTruthy());

  await user.selectOptions(screen.getByLabelText("Source to summarize"), REVIEW_ENTRY);
  await user.type(screen.getByLabelText("Its private local state (bound, never copied)"), PRIVATE_ENTRY);
  await user.click(screen.getByRole("button", { name: "Preview summary" }));
  await waitFor(() => expect(screen.getByText(PRIVACY_SUMMARY_IDENTITY)).toBeTruthy());
  expect(screen.getByText(/reviewed-extract-only/)).toBeTruthy();

  // Publishing is disabled until the preview exists, and the exact identity is
  // required: a stale or empty approval is a refusal, never a warning.
  expect((screen.getByRole("button", { name: "Publish support bundle" }) as HTMLButtonElement).disabled).toBe(true);
  await user.type(screen.getByLabelText(/Approve by naming the exact preview identity/), "stale");
  await user.type(screen.getByLabelText("New support folder"), "support-001");
  await user.click(screen.getByRole("button", { name: "Publish support bundle" }));
  await waitFor(() => expect(screen.getByText(/does not name the summary/)).toBeTruthy());

  await user.clear(screen.getByLabelText(/Approve by naming the exact preview identity/));
  await user.type(screen.getByLabelText(/Approve by naming the exact preview identity/), PRIVACY_SUMMARY_IDENTITY);
  await user.click(screen.getByRole("button", { name: "Publish support bundle" }));
  await waitFor(() => expect(screen.getByText(/support\.json, event\.json, identity\.sha256/)).toBeTruthy());
  expect(published).toEqual(["stale", PRIVACY_SUMMARY_IDENTITY]);
  expect(screen.getByText(/Exporting a file is not uploading it/)).toBeTruthy();
});
