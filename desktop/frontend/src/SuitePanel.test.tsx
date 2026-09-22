import { expect, test } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { SuitePanel } from "./SuitePanel";
import { installFacade, uninstallFacade, type FacadeStub } from "./testkit/wails";
import {
  CASE_ENTRY,
  SUITE_APPROVAL_IDENTITY,
  SUITE_ENTRY,
  SUITE_IDENTITY,
  SUITE_OCCURRENCE,
  SUITE_PREPARED,
  SUITE_REVIEW_IDENTITY,
  SUITE_TEMPLATE,
  WORKSPACE_ROOT,
  suiteArtifacts,
  suiteCoverageResult,
  suiteDocument,
  suiteDocumentResult,
  suiteImpactResult,
  suitePreparedResult,
  suitePreviewResult,
  suitePromotionApproval,
  suitePromotionReview,
  suiteReleasesResult,
} from "./testkit/fixtures";
import type { HubReleaseReviewRequest } from "./bindings";

const entries = suiteArtifacts();

function suiteFacade(answers: Record<string, unknown> = {}): FacadeStub {
  return installFacade({
    SaveEditorDraft: () => ({ state: "completed" }),
    DiscardEditorDraft: () => ({ state: "completed" }),
    // The panel's cancel control reaches the shared facade; the stub answers
    // it so the one call the journey makes never rejects unhandled.
    Cancel: () => {},
    ...answers,
  } as never);
}

// The whole journey the issue names: create a suite through structured
// controls, parameterize it against an environment, preview the exact
// expansion, save reviewed versions, prepare configuration, and read coverage
// and promotion over the same contracts. Nothing here sends.
test("a suite is created, parameterized, previewed exactly and versioned through structured controls", async () => {
  const user = userEvent.setup();
  const facade = suiteFacade({
    PreviewSuite: () => suitePreviewResult(),
    SaveSuite: () => suiteDocumentResult(),
  });
  render(<SuitePanel workspace={WORKSPACE_ROOT} busy={false} entries={entries} drafts={[]} />);

  await user.click(screen.getByRole("button", { name: "New suite" }));

  // Suite identity and organization metadata.
  await user.type(screen.getByLabelText("Suite id"), "nightly");
  const owners = screen.getAllByLabelText("Owner");
  const suiteOwner = owners[0];
  if (!suiteOwner) {
    throw new Error("the suite has no owner field");
  }
  await user.type(suiteOwner, "interop");
  const tagsFields = screen.getAllByLabelText("Tags");
  const suiteTags = tagsFields[0];
  if (!suiteTags) {
    throw new Error("the suite has no tags field");
  }
  await user.type(suiteTags, "release");
  await user.clear(screen.getByLabelText("Parallelism"));
  await user.type(screen.getByLabelText("Parallelism"), "2");

  // Parameterization: one environment binding one parameter to a target.
  await user.type(screen.getByLabelText("Environment id"), "east");
  await user.type(screen.getByLabelText("Site"), "hospital-a");
  const parameters = screen.getAllByLabelText("Parameter");
  const environmentParameter = parameters[0];
  if (!environmentParameter) {
    throw new Error("the environment binding has no parameter field");
  }
  await user.type(environmentParameter, "interface");
  await user.selectOptions(screen.getByLabelText("Target"), "east-target.json");

  // Data table: one row selecting a verified case.
  await user.type(screen.getByLabelText("Table id"), "patients");
  await user.type(screen.getByLabelText("Row id"), "one");
  await user.selectOptions(screen.getByLabelText("Case"), CASE_ENTRY);

  // Test: template, parameter, table, isolation, dependencies, exact order.
  await user.type(screen.getByLabelText("Test id"), "booking");
  await user.selectOptions(screen.getByLabelText("Template"), SUITE_TEMPLATE);
  const ownerFields = screen.getAllByLabelText("Owner");
  const testOwner = ownerFields.at(-1);
  if (!testOwner) {
    throw new Error("the test has no owner field");
  }
  await user.clear(testOwner);
  await user.type(testOwner, "scheduling");
  const testParameter = parameters.at(-1);
  if (!testParameter) {
    throw new Error("the test has no parameter field");
  }
  await user.type(testParameter, "interface");
  await user.selectOptions(screen.getByLabelText("Table"), "patients");
  await user.selectOptions(screen.getByLabelText("Isolation"), "shared");
  await user.type(screen.getByLabelText("Send order (exact)"), SUITE_OCCURRENCE);

  // Preview the exact expansion against the declared environment.
  await user.selectOptions(screen.getByLabelText("Preview environment"), "east");
  await user.click(screen.getByRole("button", { name: "Preview the exact expansion" }));
  const preview = facade.oneCall("PreviewSuite")[0] as { document: string; environment: string };
  const composed = JSON.parse(preview.document) as ReturnType<typeof suiteDocument>;
  expect(composed.id).toBe("nightly");
  expect(composed.environments[0]?.bindings[0]?.target).toBe("east-target.json");
  expect(composed.tables[0]?.rows[0]?.case).toBe(CASE_ENTRY);
  expect(composed.tests[0]?.sequence).toEqual([SUITE_OCCURRENCE]);
  expect(preview.environment).toBe("east");

  // The expansion is exact: both statements and every effective input are shown.
  expect(
    await screen.findByText(/Jobs appear in the suite's declared test and row order/i),
  ).toBeTruthy();
  expect(screen.getByText(/hold the selected environment and the endpoint its target records/i)).toBeTruthy();
  expect((await screen.findAllByText("setup-one")).length).toBeGreaterThan(0);
  expect(screen.getAllByText("booking-one").length).toBeGreaterThan(0);
  expect(screen.getAllByText(`${WORKSPACE_ROOT}/${CASE_ENTRY}`).length).toBeGreaterThan(0);

  // Save a new version: a new entry with the exact canonical identity.
  await user.type(screen.getByLabelText("New revision entry"), "nightly-v2.json");
  await user.click(screen.getByRole("button", { name: "Save new version" }));
  const saved = facade.oneCall("SaveSuite")[0] as { output: string };
  expect(saved.output).toBe("nightly-v2.json");
  expect(await screen.findByText(new RegExp(SUITE_IDENTITY))).toBeTruthy();
  uninstallFacade();
});

// A suite the command line wrote opens with no clause dropped; a version this
// release cannot read is refused, never migrated.
test("a CLI-authored suite opens whole and an unsupported version is refused", async () => {
  const user = userEvent.setup();
  const facade = suiteFacade({
    OpenSuite: () => suiteDocumentResult(),
  });
  render(<SuitePanel workspace={WORKSPACE_ROOT} busy={false} entries={entries} drafts={[]} />);
  await user.selectOptions(screen.getByLabelText("Saved suite entry"), SUITE_ENTRY);
  await user.click(screen.getByRole("button", { name: "Open suite" }));
  expect(facade.callsTo("OpenSuite")[0]?.args[1]).toBe(SUITE_ENTRY);
  // Every clause the document declares is in the editor.
  expect(await screen.findByDisplayValue("nightly")).toBeTruthy();
  expect(screen.getByDisplayValue("hospital-a")).toBeTruthy();
  expect((await screen.findAllByDisplayValue(SUITE_TEMPLATE)).length).toBeGreaterThan(0);

  facade.reply({
    OpenSuite: () => ({ state: "failed", reason: "a suite document written by a version this release cannot read" }),
  });
  await user.click(screen.getByRole("button", { name: "Open suite" }));
  expect(await screen.findByText(/a version this release cannot read/i)).toBeTruthy();
  uninstallFacade();
});

// Dependencies, shared resources and exact send order are part of the
// expansion; the preview is refused when the engine refuses the expansion.
test("preview shows dependencies, shared serialization and refuses what preparation refuses", async () => {
  const user = userEvent.setup();
  const facade = suiteFacade({
    OpenSuite: () => suiteDocumentResult(),
    PreviewSuite: () => suitePreviewResult(),
  });
  render(<SuitePanel workspace={WORKSPACE_ROOT} busy={false} entries={entries} drafts={[]} />);
  await user.selectOptions(screen.getByLabelText("Saved suite entry"), SUITE_ENTRY);
  await user.click(screen.getByRole("button", { name: "Open suite" }));
  await user.selectOptions(screen.getByLabelText("Preview environment"), "east");
  await user.click(screen.getByRole("button", { name: "Preview the exact expansion" }));
  // The dependent job names the setup job it waits for.
  expect((await screen.findAllByText("setup-one")).length).toBeGreaterThan(0);
  expect(screen.getAllByText(/shared/i).length).toBeGreaterThan(0);

  facade.reply({ PreviewSuite: () => ({ state: "failed", reason: "suite sequence differs from its template" }) });
  await user.click(screen.getByRole("button", { name: "Preview the exact expansion" }));
  expect(await screen.findByText(/suite sequence differs from its template/i)).toBeTruthy();
  uninstallFacade();
});

// Preparation compiles configuration only. Nothing is sent; the handoff seeds
// the execution center's own selection with the suite entry, so preflight and
// the explicit send decision happen there without a path copied by hand.
test("preparation compiles the queue and hands the suite to the execution center", async () => {
  const user = userEvent.setup();
  const facade = suiteFacade({ PrepareSuite: () => suitePreparedResult() });
  let handedTo = "";
  render(
    <SuitePanel
      workspace={WORKSPACE_ROOT}
      busy={false}
      entries={entries}
      drafts={[]}
      onExecute={(entry) => {
        handedTo = entry;
      }}
    />,
  );
  await user.click(screen.getByRole("button", { name: "Prepare" }));
  await user.selectOptions(screen.getByLabelText("Suite entry"), SUITE_ENTRY);
  await user.type(screen.getByLabelText("Environment"), "east");
  await user.type(screen.getByLabelText("New directory entry"), "east-run");
  await user.click(screen.getByRole("button", { name: "Prepare configuration" }));
  const request = facade.oneCall("PrepareSuite")[0] as { entry: string; environment: string; output: string };
  expect(request).toEqual({ workspace: WORKSPACE_ROOT, entry: SUITE_ENTRY, environment: "east", releases: "", output: "east-run" });
  expect(await screen.findByText(/Nothing was sent/i)).toBeTruthy();
  expect(screen.getAllByText("setup-one").length).toBeGreaterThan(0);
  // The handoff names the suite entry itself: the execution center preflights
  // and executes it there, and this panel duplicates none of that surface.
  await user.click(screen.getByRole("button", { name: "Continue to the execution center" }));
  expect(handedTo).toBe(SUITE_ENTRY);
  expect(screen.getAllByText(/execution center/i).length).toBeGreaterThan(0);
  uninstallFacade();
});

// Coverage: the explicit denominator, missing requirements, quarantine with
// reason and expiry, incomplete observations and the reduced-set disclaimer.
test("coverage shows the declared denominator, quarantine expiry and unknown executions", async () => {
  const user = userEvent.setup();
  const facade = suiteFacade({
    SaveSuiteCoverage: () => ({ state: "completed", document: "{}", output: "coverage.json" }),
    AssessSuiteCoverage: () => suiteCoverageResult(),
  });
  render(<SuitePanel workspace={WORKSPACE_ROOT} busy={false} entries={entries} drafts={[]} />);
  await user.click(screen.getByRole("button", { name: "Coverage" }));

  // Authoring: requirements and an exclusion with reason and expiry.
  const preparedPicks = screen.getAllByLabelText("Prepared suite directory");
  const authoringPick = preparedPicks[0];
  const assessmentPick = preparedPicks.at(-1);
  if (!authoringPick || !assessmentPick) {
    throw new Error("the coverage tab has no prepared-suite pickers");
  }
  await user.selectOptions(authoringPick, SUITE_PREPARED);
  await user.type(screen.getByLabelText("Requirement 1"), "accept-booking");
  await user.type(screen.getByLabelText("Requirement jobs 1"), "booking-one");
  await user.click(screen.getByRole("button", { name: "Add exclusion" }));
  await user.type(screen.getByLabelText("Exclusion job 1"), "booking-one");
  await user.selectOptions(screen.getByLabelText("Exclusion state 1"), "quarantined");
  await user.type(screen.getByLabelText("Exclusion reason 1"), "Fixture intermittently refuses bookings");
  await user.type(screen.getByLabelText("Exclusion expiry 1"), "2026-10-01T00:00:00Z");
  await user.type(screen.getByLabelText("New coverage entry"), "coverage.json");
  await user.click(screen.getByRole("button", { name: "Author coverage document" }));
  const authored = facade.oneCall("SaveSuiteCoverage")[0] as {
    prepared: string;
    requirements: { id: string; jobs: string[] }[];
    exclusions: { job: string; state: string; reason: string; expires: string }[];
  };
  expect(authored.prepared).toBe(SUITE_PREPARED);
  expect(authored.requirements).toEqual([{ id: "accept-booking", jobs: ["booking-one"] }]);
  expect(authored.exclusions).toEqual([
    { job: "booking-one", state: "quarantined", reason: "Fixture intermittently refuses bookings", expires: "2026-10-01T00:00:00Z" },
  ]);

  // Assessment: explicit denominator, expired quarantine, unknown execution.
  await user.selectOptions(assessmentPick, SUITE_PREPARED);
  await user.selectOptions(screen.getByLabelText("Coverage document"), SUITE_ENTRY);
  await user.click(screen.getByRole("button", { name: "Assess coverage" }));
  expect(await screen.findByText(/0\/2 \(0\.00%\)/)).toBeTruthy();
  expect(screen.getByText("downstream-persistence")).toBeTruthy();
  expect(screen.getByText(/explicitly uncovered/i)).toBeTruthy();
  expect(screen.getByText(/expired/i)).toBeTruthy();
  expect(screen.getByText(/No durable execution exists; completion is unknown/i)).toBeTruthy();
  expect(screen.getByText(/not universal HL7 assurance/i)).toBeTruthy();
  uninstallFacade();
});

// A coverage assessment can be cancelled; retained evidence stays unchanged
// and the message says so.
test("a coverage assessment is cancelled without touching retained evidence", async () => {
  const user = userEvent.setup();
  const facade = suiteFacade({});
  const parked = facade.park("AssessSuiteCoverage");
  render(<SuitePanel workspace={WORKSPACE_ROOT} busy={false} entries={entries} drafts={[]} />);
  await user.click(screen.getByRole("button", { name: "Coverage" }));
  const picks = screen.getAllByLabelText("Prepared suite directory");
  const assessPick = picks.at(-1);
  if (!assessPick) {
    throw new Error("the coverage tab has no assessment picker");
  }
  await user.selectOptions(assessPick, SUITE_PREPARED);
  await user.selectOptions(screen.getByLabelText("Coverage document"), SUITE_ENTRY);
  await user.click(screen.getByRole("button", { name: "Assess coverage" }));
  await user.click(await screen.findByRole("button", { name: "Cancel assessment" }));
  expect(await screen.findByText(/Coverage assessment cancelled/i)).toBeTruthy();
  expect(screen.getByText(/Retained evidence is unchanged/i)).toBeTruthy();
  const cancelled = facade.callsTo("Cancel")[0]?.args[0];
  expect(cancelled).toBe("suite-coverage-assessment");
  parked.reject();
  uninstallFacade();
});

// Promotion: review shows the exact pins, approval is an explicit local
// decision, and a changed configuration refuses the stale commitment.
test("promotion review and approval bind exact pins and refuse stale reviews", async () => {
  const user = userEvent.setup();
  const facade = suiteFacade({
    ReviewSuitePromotion: () => suitePromotionReview(),
    ApproveSuitePromotion: () => suitePromotionApproval(),
  });
  render(<SuitePanel workspace={WORKSPACE_ROOT} busy={false} entries={entries} drafts={[]} />);
  await user.click(screen.getByRole("button", { name: "Promotion" }));
  await user.selectOptions(screen.getByLabelText("Suite entry"), SUITE_ENTRY);
  await user.type(screen.getByLabelText("Environment"), "east");
  await user.selectOptions(screen.getByLabelText("Release references"), SUITE_ENTRY);
  await user.type(screen.getByLabelText("Target revision (operator-declared)"), "fixture-build-7");
  await user.click(screen.getByRole("button", { name: "Review promotion" }));
  const request = facade.oneCall("ReviewSuitePromotion")[0] as {
    entry: string;
    environment: string;
    releases: string;
    revision: string;
  };
  expect(request).toEqual({ workspace: WORKSPACE_ROOT, entry: SUITE_ENTRY, environment: "east", releases: SUITE_ENTRY, revision: "fixture-build-7" });
  expect(await screen.findByText(new RegExp(SUITE_REVIEW_IDENTITY))).toBeTruthy();
  expect(screen.getByText(/grants no send authority/i)).toBeTruthy();

  await user.type(screen.getByLabelText("Local approver"), "Local reviewer");
  await user.type(screen.getByLabelText("Approval rationale"), "Reviewed dev mapping and isolation");
  await user.type(screen.getByLabelText("New approval entry"), "dev-promotion.json");
  await user.click(screen.getByRole("button", { name: "Approve this exact promotion" }));
  const approval = facade.oneCall("ApproveSuitePromotion")[0] as { reviewed: string; approver: string; output: string };
  expect(approval.reviewed).toBe(SUITE_REVIEW_IDENTITY);
  expect(approval.approver).toBe("Local reviewer");
  expect(approval.output).toBe("dev-promotion.json");
  expect(await screen.findByText(new RegExp(SUITE_APPROVAL_IDENTITY))).toBeTruthy();

  // A stale review is refused: the engine re-reads every input.
  facade.reply({
    ReviewSuitePromotion: () => suitePromotionReview(),
    ApproveSuitePromotion: () => ({ state: "failed", reason: "suite inputs changed since promotion approval" }),
  });
  await user.click(screen.getByRole("button", { name: "Review promotion" }));
  await user.click(screen.getByRole("button", { name: "Approve this exact promotion" }));
  expect(await screen.findByText(/suite inputs changed since promotion approval/i)).toBeTruthy();
  uninstallFacade();
});

// The release sidecar is authored with exact identities, and impact reports
// what a successor release changes for the suite without moving any pin.
test("release references are authored and impact reports affected tests", async () => {
  const user = userEvent.setup();
  const facade = suiteFacade({
    SaveSuiteReleases: () => suiteReleasesResult(),
    ExpectationImpact: () => suiteImpactResult(),
  });
  render(<SuitePanel workspace={WORKSPACE_ROOT} busy={false} entries={entries} drafts={[]} />);
  await user.click(screen.getByRole("button", { name: "Releases and impact" }));
  await user.type(screen.getByLabelText("Test 1"), "booking");
  await user.type(screen.getByLabelText("Release entry 1"), "booking-1.json");
  await user.type(screen.getByLabelText("Release identity 1"), "release-identity-fixed-for-tests");
  await user.type(screen.getByLabelText("New sidecar entry"), "releases.json");
  await user.click(screen.getByRole("button", { name: "Save release references" }));
  const sidecar = JSON.parse(facade.oneCall("SaveSuiteReleases")[0].document) as {
    schema: string;
    tests: { test: string; release: string; identity: string }[];
  };
  expect(sidecar.schema).toBe("readmit-suite-releases/v1");
  expect(sidecar.tests).toEqual([{ test: "booking", release: "booking-1.json", identity: "release-identity-fixed-for-tests" }]);

  await user.selectOptions(screen.getByLabelText("Suite"), SUITE_ENTRY);
  await user.selectOptions(screen.getByLabelText("Release references"), SUITE_ENTRY);
  await user.type(screen.getByLabelText("From"), "booking-1.json");
  await user.type(screen.getByLabelText("To"), "booking-2.json");
  await user.click(screen.getByRole("button", { name: "Report impact" }));
  expect(await screen.findByText("affected")).toBeTruthy();
  expect(screen.getByText("booking")).toBeTruthy();
  uninstallFacade();
});

// The team review journey from the release surface: the successor release the
// To entry names is requested and approved on the hub against the digest of
// those exact bytes, under the signed-in identity the hub records — with the
// refusal surfaced while no request names the release yet.
test("the successor release is requested and approved for team review by content", async () => {
  const user = userEvent.setup();
  let requested = false;
  const releaseEvent = (kind: string, recipient: string) => ({
    schema: "readmit-hub-review-event/v1",
    project: "cardio-study",
    sequence: 7,
    issuer: "https://idp.example",
    actor: "author@hospital.org",
    at: "2026-09-21T12:00:00Z",
    kind,
    evidence: "e".repeat(64),
    parent: kind === "approval" ? "rel-request-1" : "",
    recipient,
    text: "Review the exact released expectations",
    release: "d".repeat(64),
    command_id: "rel-request-1",
  });
  const facade = suiteFacade({
    PostHubReleaseReview: (request: HubReleaseReviewRequest) => {
      if (request.kind === "approval" && !requested) {
        return {
          state: "failed",
          project: request.project,
          reason: "no review request names this exact release content; ask the author to request review, then approve the new request",
        };
      }
      requested = true;
      return {
        state: "completed",
        project: request.project,
        head: 7,
        events: [releaseEvent(request.kind, request.recipient)],
      };
    },
  });
  render(<SuitePanel workspace={WORKSPACE_ROOT} busy={false} entries={entries} drafts={[]} />);
  await user.click(screen.getByRole("button", { name: "Releases and impact" }));
  await user.type(screen.getByLabelText("To"), "booking-2.json");
  await user.type(screen.getByLabelText("Hub project"), "cardio-study");
  await user.type(screen.getByLabelText("Request recipient"), "reviewer@hospital.org");
  await user.type(screen.getByLabelText("Command id"), "rel-request-1");
  await user.type(screen.getByLabelText("Rationale"), "Review the exact released expectations");

  // Approving while no request names the release is refused and surfaced.
  await user.click(screen.getByRole("button", { name: "Approve this release" }));
  expect(await screen.findByText(/no review request names this exact release content/i)).toBeTruthy();
  const refused = facade.callsTo("PostHubReleaseReview")[0]?.args[0] as { kind: string; recipient: string };
  expect(refused.kind).toBe("approval");
  expect(refused.recipient).toBe("");

  // The request carries the entry the panel named and the subject it asks.
  await user.click(screen.getByRole("button", { name: "Request team review" }));
  expect(await screen.findByText(/Recorded: review-request by author@hospital.org@https:\/\/idp\.example/)).toBeTruthy();
  const asked = facade.callsTo("PostHubReleaseReview")[1]?.args[0] as {
    kind: string;
    recipient: string;
    entry: string;
    workspace: string;
    project: string;
  };
  expect(asked.kind).toBe("review-request");
  expect(asked.recipient).toBe("reviewer@hospital.org");
  expect(asked.entry).toBe("booking-2.json");
  expect(asked.workspace).toBe(WORKSPACE_ROOT);
  expect(asked.project).toBe("cardio-study");

  await user.click(screen.getByRole("button", { name: "Approve this release" }));
  expect(await screen.findByText(/Recorded: approval by author@hospital.org/)).toBeTruthy();
  const approved = facade.callsTo("PostHubReleaseReview")[2]?.args[0] as { kind: string; recipient: string };
  expect(approved.kind).toBe("approval");
  expect(approved.recipient).toBe("");
  uninstallFacade();
});

// Unsaved suite work is retained as an editor draft and offered back.
test("unsaved suite work is retained and restored", async () => {
  const user = userEvent.setup();
  const facade = suiteFacade({
    SaveSuite: () => ({ ...suiteDocumentResult(), suite: { ...suiteDocument(), owner: "release-team" } }),
  });
  render(
    <SuitePanel
      workspace={WORKSPACE_ROOT}
      busy={false}
      entries={entries}
      drafts={[
        {
          id: "held-1",
          kind: "suite-editor",
          workspace: WORKSPACE_ROOT,
          case: "",
          identity: "",
          content_schema: "readmit-suite-draft/v1",
          content: { schema: "readmit-suite-draft/v1", entry: SUITE_ENTRY, document: JSON.stringify(suiteDocument()) },
        },
      ]}
    />,
  );
  // The retained draft is the editor's starting point.
  expect(await screen.findByDisplayValue("nightly")).toBeTruthy();
  // An edit retains the work under the same internal identity.
  const ownerFields = screen.getAllByLabelText("Owner");
  const suiteOwner = ownerFields[0];
  if (!suiteOwner) {
    throw new Error("the suite has no owner field");
  }
  await user.clear(suiteOwner);
  await user.type(suiteOwner, "release-team");
  await waitFor(() => {
    const retained = facade.callsTo("SaveEditorDraft").at(-1)?.args[0] as { kind: string; id: string };
    expect(retained.kind).toBe("suite-editor");
    expect(retained.id).toBe("held-1");
  });
  uninstallFacade();
});
