// The privacy journeys at the window: a derived review is prepared from the
// workspace's actual documents and reports its blockers and its identity; the
// packet is exported only under an approval naming that exact identity; and
// the value-free support summary is previewed, published under the exact
// preview identity into a folder named in the host's save dialog, and verified
// offline, again on request. The approval inputs are cleared when the
// selection changes, so no approval survives the bytes it named; a refused
// policy, a dismissed dialog and a refused bundle each say so; and nothing
// here ever shows a value.
import { expect, test } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { PrivacyPanel } from "./PrivacyPanel";
import { facadeStub, installFacade } from "./testkit/wails";
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
const REFUSED_SHARING_ENTRY = "extended-sharing.json";
const BUNDLE_ENTRY = "support-001";
const CHOSEN_FOLDER = `${WORKSPACE_ROOT}/support-for-vendor`;

const ENTRIES: Artifact[] = [
  { name: CASE_ENTRY, kind: "case", schema: "readmit-case/v3", provenance: "imported" },
  { name: SPEC_ENTRY, kind: "spec" },
  { name: POLICY_ENTRY, kind: "redact-policy", schema: "readmit-redact-policy/v1" },
  { name: INVENTORY_ENTRY, kind: "redact-inventory", schema: "readmit-redact-inventory/v1" },
  { name: "other.json", kind: "unsupported", reason: "unsupported contract" },
  { name: REVIEW_ENTRY, kind: "review" },
  { name: PRIVATE_ENTRY, kind: "unsupported", reason: "not a case bundle this release supports" },
  { name: SHARING_ENTRY, kind: "sharing-policy" },
  { name: REFUSED_SHARING_ENTRY, kind: "sharing-policy" },
  { name: BUNDLE_ENTRY, kind: "support" },
];

test("the disclosure pickers list declared policy and inventory kinds, not unrelated JSON", () => {
  renderPanel();
  const policies = within(screen.getByLabelText("Disclosure policy"));
  const inventories = within(screen.getByLabelText("Original-artifact inventory"));
  expect(policies.getByRole("option", { name: POLICY_ENTRY })).toBeTruthy();
  expect(policies.queryByRole("option", { name: INVENTORY_ENTRY })).toBeNull();
  expect(policies.queryByRole("option", { name: "other.json" })).toBeNull();
  expect(inventories.getByRole("option", { name: INVENTORY_ENTRY })).toBeTruthy();
  expect(inventories.queryByRole("option", { name: POLICY_ENTRY })).toBeNull();
  expect(inventories.queryByRole("option", { name: "other.json" })).toBeNull();
});

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
  await user.click(screen.getByRole("button", { name: "Create review" }));
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
  await user.click(screen.getByRole("button", { name: "Create review" }));
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
  const approval = screen.getByLabelText("Review ID", { selector: "#privacy-export-approval" });
  expect((approval as HTMLInputElement).disabled).toBe(true);
  await user.selectOptions(screen.getByLabelText("Review to export"), REVIEW_ENTRY);
  await user.type(screen.getByLabelText("Its private local state"), PRIVATE_ENTRY);
  await user.type(screen.getByLabelText("Review ID", { selector: "#privacy-export-approval" }), PRIVACY_REVIEW_IDENTITY);
  await user.click(screen.getByRole("button", { name: "Export packet" }));
  await waitFor(() => expect(screen.getByText(/external equivalence declined/)).toBeTruthy());
  expect(screen.getByText(/baseline assertion_failure, postfix pass/)).toBeTruthy();
  expect(exported).toEqual([PRIVACY_REVIEW_IDENTITY]);

  // Selecting a different review clears the typed approval: an approval is a
  // fresh act over the identity the bytes have now, never a retained one.
  await user.selectOptions(screen.getByLabelText("Review to export"), "");
  expect((screen.getByLabelText("Review ID", { selector: "#privacy-export-approval" }) as HTMLInputElement).value).toBe("");
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

  await user.selectOptions(screen.getByLabelText("Source"), REVIEW_ENTRY);
  await user.type(screen.getByLabelText("Private state"), PRIVATE_ENTRY);
  await user.click(screen.getByRole("button", { name: "Preview summary" }));
  await waitFor(() => expect(screen.getByText(PRIVACY_SUMMARY_IDENTITY)).toBeTruthy());
  expect(screen.getByText(/reviewed-extract-only/)).toBeTruthy();

  // Publishing is disabled until the preview exists, and the exact identity is
  // required: a stale or empty approval is a refusal, never a warning.
  expect((screen.getByRole("button", { name: "Export support bundle" }) as HTMLButtonElement).disabled).toBe(true);
  await user.type(screen.getByLabelText("Preview ID"), "stale");
  await user.type(screen.getByLabelText("New support folder"), "support-001");
  await user.click(screen.getByRole("button", { name: "Export support bundle" }));
  await waitFor(() => expect(screen.getByText(/does not name the summary/)).toBeTruthy());

  await user.clear(screen.getByLabelText("Preview ID"));
  await user.type(screen.getByLabelText("Preview ID"), PRIVACY_SUMMARY_IDENTITY);
  await user.click(screen.getByRole("button", { name: "Export support bundle" }));
  await waitFor(() => expect(screen.getByText(/support\.json, event\.json, identity\.sha256/)).toBeTruthy());
  expect(published).toEqual(["stale", PRIVACY_SUMMARY_IDENTITY]);
  expect(screen.getByText(/Exporting a file is not uploading it/)).toBeTruthy();
});

/** Moves focus with Tab from where it is until the control has it, as a
 * keyboard user does, failing when the control cannot be reached that way. */
async function tabTo(user: ReturnType<typeof userEvent.setup>, control: HTMLElement): Promise<void> {
  for (let step = 0; step < 50; step++) {
    if (document.activeElement === control) return;
    await user.tab();
  }
  throw new Error(`${control.textContent ?? ""} is not reachable with Tab`);
}

/** Selects the sharing policy and the derived review with its private state,
 * and previews the summary they produce. */
async function previewSummary(user: ReturnType<typeof userEvent.setup>) {
  await user.selectOptions(screen.getByLabelText("Sharing policy"), SHARING_ENTRY);
  await screen.findByText(/Support allowed · local-file · 4096 bytes/);
  await user.selectOptions(screen.getByLabelText("Source"), REVIEW_ENTRY);
  await user.type(screen.getByLabelText("Private state"), PRIVATE_ENTRY);
  await user.click(screen.getByRole("button", { name: "Preview summary" }));
  await screen.findByText(PRIVACY_SUMMARY_IDENTITY);
}

const supportHandlers: FacadeHandlers = {
  ReadSharingPolicy: (_workspace, entry) =>
    entry === SHARING_ENTRY
      ? supportPolicyResult()
      : { state: "failed" as const, reason: "that entry is not a sharing policy this release prepares with" },
  PreviewSupportSummary: () => supportPreviewResult(),
};

test("the publish destination is named in the host's save dialog; a dismissed or unavailable dialog says so and names nothing, and the named folder is where the bundle is published", async () => {
  const user = userEvent.setup();
  const answers = [
    { state: "cancelled" as const, reason: "no new folder was named" },
    { state: "failed" as const, reason: "the save dialog is unavailable" },
    { state: "completed" as const, path: CHOSEN_FOLDER },
  ];
  renderPanel({
    ...supportHandlers,
    PublishSupportSummary: (request) => {
      expect(request.output).toBe(CHOSEN_FOLDER);
      expect(request.approval).toBe(PRIVACY_SUMMARY_IDENTITY);
      return {
        state: "completed" as const,
        outcome: {
          bundle: "support-for-vendor",
          identity: PRIVACY_SUMMARY_IDENTITY,
          files: ["support.json", "event.json", "identity.sha256"],
          exclusions: [],
          no_upload: "Publishing this bundle wrote a local directory. Exporting a file is not uploading it.",
          limitations: [],
        },
      };
    },
  });
  await previewSummary(user);
  await user.type(screen.getByLabelText("Preview ID"), PRIVACY_SUMMARY_IDENTITY);
  const output = screen.getByLabelText("New support folder") as HTMLInputElement;
  const choose = screen.getByRole("button", { name: "Choose destination…" });

  // While the dialog is open the panel waits for it, and says nothing else is
  // running: no other control claims to be authoring, preparing or publishing.
  const dialog = facadeStub().park("ChooseSupportExportPath");
  await user.click(output);
  await tabTo(user, choose);
  await user.keyboard("{Enter}");
  await waitFor(() => expect(dialog.size).toBe(1));
  for (const name of ["Save sharing policy", "Preview summary", "Export support bundle", "Choose destination…"]) {
    expect((screen.getByRole("button", { name }) as HTMLButtonElement).disabled).toBe(true);
  }
  expect(screen.queryByText(/^(Authoring|Preparing|Publishing)…$/)).toBeNull();

  // Dismissed, the dialog names nothing and the panel says so.
  dialog.resolve(answers[0]);
  expect(await screen.findByText("no new folder was named")).toBeTruthy();
  expect(output.value).toBe("");

  // A host without a working save dialog says so, and still names nothing.
  facadeStub().reply({ ChooseSupportExportPath: () => answers[1]! });
  await tabTo(user, choose);
  await user.keyboard("{Enter}");
  expect(await screen.findByText("the save dialog is unavailable")).toBeTruthy();
  expect(screen.queryByText("no new folder was named")).toBeNull();
  expect(output.value).toBe("");

  // A named folder becomes the destination, and the bundle is published there.
  facadeStub().reply({ ChooseSupportExportPath: () => answers[2]! });
  await tabTo(user, choose);
  await user.keyboard("{Enter}");
  await waitFor(() => expect(output.value).toBe(CHOSEN_FOLDER));
  expect(screen.queryByText("the save dialog is unavailable")).toBeNull();
  await user.click(screen.getByRole("button", { name: "Export support bundle" }));
  expect(await screen.findByText(/^Bundle/)).toBeTruthy();
  expect(facadeStub().callsTo("ChooseSupportExportPath")).toHaveLength(3);
  expect(facadeStub().callsTo("PublishSupportSummary")).toHaveLength(1);
});

test("a publication into a destination that already exists is refused with the writer's reason, and a new folder is then accepted", async () => {
  const user = userEvent.setup();
  const outputs: string[] = [];
  renderPanel({
    ...supportHandlers,
    ChooseSupportExportPath: () => ({ state: "completed" as const, path: CHOSEN_FOLDER }),
    PublishSupportSummary: (request) => {
      outputs.push(request.output ?? "");
      if (request.output === CHOSEN_FOLDER) {
        return {
          state: "failed" as const,
          reason: "the publication was refused; an incomplete directory has no completion marker and recovery is a new destination with a fresh review",
        };
      }
      return {
        state: "completed" as const,
        outcome: {
          bundle: "support-002",
          identity: PRIVACY_SUMMARY_IDENTITY,
          files: ["support.json", "event.json", "identity.sha256"],
          exclusions: [],
          no_upload: "Publishing this bundle wrote a local directory.",
          limitations: [],
        },
      };
    },
  });
  await previewSummary(user);
  await user.type(screen.getByLabelText("Preview ID"), PRIVACY_SUMMARY_IDENTITY);
  await user.click(screen.getByRole("button", { name: "Choose destination…" }));
  await waitFor(() => expect((screen.getByLabelText("New support folder") as HTMLInputElement).value).toBe(CHOSEN_FOLDER));
  await user.click(screen.getByRole("button", { name: "Export support bundle" }));
  expect(await screen.findByText(/^the publication was refused; .* recovery is a new destination with a fresh review$/)).toBeTruthy();
  expect(screen.queryByText(/^Bundle/)).toBeNull();
  await user.clear(screen.getByLabelText("New support folder"));
  await user.type(screen.getByLabelText("New support folder"), "support-002");
  await user.click(screen.getByRole("button", { name: "Export support bundle" }));
  expect(await screen.findByText(/^Bundle/)).toBeTruthy();
  expect(screen.queryByText(/^the publication was refused/)).toBeNull();
  expect(outputs).toEqual([CHOSEN_FOLDER, "support-002"]);
});

test("a bundle is verified offline, a bundle changed since is refused when verified again, and the picker keeps the bundle each answer is about", async () => {
  const user = userEvent.setup();
  renderPanel({
    VerifySupportBundle: (workspace, entry) => {
      expect([workspace, entry]).toEqual([WORKSPACE_ROOT, BUNDLE_ENTRY]);
      return supportPreviewResult();
    },
  });
  const picker = screen.getByLabelText("Verify a support bundle") as HTMLSelectElement;
  const again = screen.getByRole("button", { name: "Verify again" }) as HTMLButtonElement;
  expect(again.disabled).toBe(true);
  await user.selectOptions(picker, BUNDLE_ENTRY);
  const verifiedLine = `Verified bundle identity: ${PRIVACY_SUMMARY_IDENTITY} — integrity only, not authentication or authorization.`;
  expect(await screen.findByText((_content, element) => element?.tagName === "P" && element.textContent === verifiedLine)).toBeTruthy();
  expect(picker.value).toBe(BUNDLE_ENTRY);

  // Changed on disk since, the same bundle is read again from the keyboard
  // and refused; the identity it verified to before is withdrawn.
  const refusal =
    "this directory is not a complete support bundle this release verifies; a bundle missing, holding or hiding anything beyond its three members is refused";
  const verifying = facadeStub().park("VerifySupportBundle");
  await tabTo(user, again);
  await user.keyboard("{Enter}");
  await waitFor(() => expect(verifying.size).toBe(1));
  expect(screen.queryByText(/^Verified bundle identity/)).toBeNull();
  expect(picker.disabled).toBe(true);
  expect(screen.queryByText(/^Publishing…$/)).toBeNull();
  verifying.resolve({ state: "failed", reason: refusal });
  expect(await screen.findByText(refusal)).toBeTruthy();
  expect(screen.queryByText(PRIVACY_SUMMARY_IDENTITY)).toBeNull();
  expect(picker.value).toBe(BUNDLE_ENTRY);
  expect(facadeStub().callsTo("VerifySupportBundle")).toHaveLength(2);
});

test("a sharing policy the decoder refuses is named as refused, and another policy withdraws the preview and the approval typed against it", async () => {
  const user = userEvent.setup();
  renderPanel({
    ...supportHandlers,
    SaveSharingPolicy: (request) => supportPolicyResult({ entry: request.output, max_bytes: request.max_bytes }),
  });
  await previewSummary(user);
  const approval = screen.getByLabelText("Preview ID") as HTMLInputElement;
  await user.type(approval, PRIVACY_SUMMARY_IDENTITY);

  await user.selectOptions(screen.getByLabelText("Sharing policy"), REFUSED_SHARING_ENTRY);
  expect(await screen.findByText("that entry is not a sharing policy this release prepares with")).toBeTruthy();
  expect(screen.queryByText(/Support allowed/)).toBeNull();
  expect(screen.queryByText(PRIVACY_SUMMARY_IDENTITY)).toBeNull();
  expect(approval.value).toBe("");
  expect(approval.disabled).toBe(true);

  // A policy saved here is the policy selected, and the panel shows it as it
  // was written, not the reading of the policy selected before it.
  await user.selectOptions(screen.getByLabelText("Sharing policy"), SHARING_ENTRY);
  await screen.findByText(/Support allowed · local-file · 4096 bytes/);
  await user.click(screen.getByRole("button", { name: "Preview summary" }));
  await screen.findByText(PRIVACY_SUMMARY_IDENTITY);
  await user.type(screen.getByLabelText("New policy document"), "sharing-2.json");
  await user.clear(screen.getByLabelText("Byte bound"));
  await user.type(screen.getByLabelText("Byte bound"), "2048");
  await user.click(screen.getByRole("button", { name: "Save sharing policy" }));
  expect(await screen.findByText(/^Support allowed · local-file · 2048 bytes\.$/)).toBeTruthy();
  expect(screen.queryByText(/4096 bytes\.$/)).toBeNull();
  expect(screen.queryByText(PRIVACY_SUMMARY_IDENTITY)).toBeNull();
  expect(within(screen.getByLabelText("Sharing policy")).getAllByRole("option")).toHaveLength(3);
});
