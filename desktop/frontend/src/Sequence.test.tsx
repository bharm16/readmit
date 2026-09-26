import { readCaseIdentity } from "./testkit/navigation";
// The sequence panel driven through the window, as a person drives it: the
// real App over the stubbed facade, so choosing a rules document reaches
// OpenSequence, the review panel reaches OpenCorrelationReview and
// DecideCorrelation, and the two editors reach OpenCorrelationRules,
// OpenSequenceAnalysis and their saves the way the production bindings call
// them. The answers carry positions, states and counts; what they mean for
// the evidence is the Go readers' subject, held to `readmit correlate` by the
// parity tests.
import { expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type {
  CorrelationOccurrence,
  CorrelationReviewRequest,
  CorrelationReviewView,
  CorrelationRulesDocument,
  SequenceAnalysisDeclaration,
} from "./bindings";
import { renderApp } from "./testkit/app";
import {
  CASE_ENTRY,
  CASE_IDENTITY,
  GRID_OCCURRENCE,
  NEXT_OCCURRENCE,
  WORKSPACE_ROOT,
  caseResult,
  folderChosen,
  refused,
  sequenceEvent,
  sequenceResult,
} from "./testkit/fixtures";

const RULES = "scheduling.rules.json";
const BROKEN_RULES = "broken.rules.json";
const ANALYSIS = "analysis.json";
const OTHER_ANALYSIS = "other-analysis.json";
const REVIEW = "review-1";
/** The canonical digest of the rules a sequence was laid out under, as the
 * engine reports it; the digest of an entry's bytes is another value. */
const RULES_SHA256 = "canonical-rules-sha256-fixed-for-tests";
const ENTRY_SHA256 = "entry-bytes-sha256-fixed-for-tests";
const MAPPING = "mapping-fixed-for-tests";
const DECIDED = "decided-mapping-fixed-for-tests";
const WRITE_REFUSED = "correlation review destination must be new and its parent readable and writable";
const STALE = "the displayed rules changed; reopen the sequence before reviewing";

type Stub = Awaited<ReturnType<typeof renderApp>>["facade"];
type User = ReturnType<typeof userEvent.setup>;

/** A workspace holding the open case, two rules documents, two sequence
 * analyses and one retained correlation review. */
function workspace() {
  return folderChosen(WORKSPACE_ROOT, [
    { name: CASE_ENTRY, kind: "case", schema: "readmit-case/v3", provenance: "imported" },
    { name: RULES, kind: "rules" },
    { name: BROKEN_RULES, kind: "rules" },
    { name: ANALYSIS, kind: "analysis" },
    { name: OTHER_ANALYSIS, kind: "analysis" },
    { name: REVIEW, kind: "correlation-review" },
  ]);
}

/** Opens the workspace and verifies its case, as a person does before the
 * sequence panel is offered at all. */
async function openCase(facade: Stub, user: User) {
  facade.reply({ SelectWorkspace: () => workspace(), OpenWorkspace: () => workspace(), OpenCase: () => caseResult() });
  await user.click(screen.getAllByRole("button", { name: "Open…" })[0] as HTMLElement);
  await within(screen.getByRole("region", { name: "Navigation" })).findByText(WORKSPACE_ROOT);
  await user.click(screen.getByRole("button", { name: `Open case ${CASE_ENTRY}` }));
  await readCaseIdentity(user, CASE_IDENTITY);
  await user.click(screen.getByRole("tab", { name: "Timeline" }));
  return within(await screen.findByRole("region", { name: "Sequence" }));
}

/** The case laid out under RULES: one link and one collision. */
function laidOut() {
  const result = sequenceResult([sequenceEvent(GRID_OCCURRENCE, 1), sequenceEvent(NEXT_OCCURRENCE, 2)], {
    rules: RULES,
    rules_sha256: RULES_SHA256,
    report: "readmit-correlation/v1",
    declared: [{ id: "same-booking", operator: "control-id", scope: "source", applied: true, considered: 2, linked: 2, unlinked: 0 }],
  });
  result.sequence!.summary.links = 1;
  result.sequence!.summary.collisions = 1;
  return result;
}

/** Lays the case out under RULES and returns the review panel beneath it. */
async function layOut(facade: Stub, user: User, panel: ReturnType<typeof within>) {
  facade.reply({ OpenSequence: () => laidOut() });
  await user.selectOptions(panel.getByLabelText("Correlation rules"), RULES);
  return within(await panel.findByRole("region", { name: "Correlation review" }));
}

const occurrence = (id: string): CorrelationOccurrence => ({ occurrence: id, source_id: "s0001", kind: "message" });

/** One reviewed mapping: two machine links and one original collision. */
function reviewed(overrides: Partial<CorrelationReviewView> = {}): CorrelationReviewView {
  return {
    mapping: MAPPING,
    machine: "machine-finding-fixed-for-tests",
    values_shown: false,
    boundary: "Human decisions are local analyst assertions, not authenticated identity or observed causality.",
    offset: 0,
    total_links: 2,
    total_collisions: 1,
    total_decisions: 0,
    links: [
      { id: "l000001", linkage: "observed", rule: "acknowledgements", status: "unreviewed", occurrences: [occurrence(GRID_OCCURRENCE), occurrence(NEXT_OCCURRENCE)], total_occurrences: 2 },
      { id: "l000002", linkage: "inferred", rule: "same-booking", status: "unreviewed", occurrences: [occurrence(GRID_OCCURRENCE), occurrence("occ-000003")], total_occurrences: 2 },
    ],
    collisions: [
      { finding: { rule: "same-booking", operator: "identifier", reason: "duplicate_control_id", occurrences: [occurrence(NEXT_OCCURRENCE), occurrence("occ-000003")] }, total_occurrences: 2 },
    ],
    history: [],
    ...overrides,
  };
}

/** Moves focus with Tab until the control has it, as a keyboard user does,
 * failing when the control cannot be reached that way. */
async function tabTo(user: User, control: HTMLElement): Promise<void> {
  for (let step = 0; step < 200; step++) {
    if (document.activeElement === control) return;
    await user.tab();
  }
  throw new Error(`${control.textContent ?? ""} is not reachable with Tab`);
}

test("a case is laid out under the rules document chosen in the sequence panel, a refused document leaves no sequence beside its refusal, and Escape while it is laid out reaches Cancel", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp();
  const panel = await openCase(facade, user);

  // Only the entries declaring the rules contract are offered, beside no
  // rules at all; there is no default rule set.
  const picker = panel.getByLabelText("Correlation rules") as HTMLSelectElement;
  expect(Array.from(picker.options).map((option) => option.value)).toEqual(["", RULES, BROKEN_RULES]);

  // A document the reader refuses is refused in its sentence, chosen and laid
  // out from the keyboard, and no sequence and no review stand beside it.
  facade.reply({ OpenSequence: () => refused("invalid correlation rules JSON") });
  await user.selectOptions(picker, BROKEN_RULES);

  expect(await panel.findByText("invalid correlation rules JSON")).toBeTruthy();
  expect(facade.callsTo("OpenSequence").at(-1)?.args[0]).toEqual({
    workspace: WORKSPACE_ROOT,
    case: CASE_ENTRY,
    identity: CASE_IDENTITY,
    rules: BROKEN_RULES,
    analysis: "",
    offset: 0,
    limit: 200,
  });
  expect(panel.queryByRole("table")).toBeNull();
  expect(panel.queryByRole("region", { name: "Correlation review" })).toBeNull();

  // The chosen document: while the case is laid out the panel says so and
  // holds its controls, Escape asks for a cancellation, and what the facade
  // completes is drawn as completed.
  const parked = facade.park("OpenSequence");
  await user.selectOptions(picker, RULES);
  expect(await panel.findByText("Loading the timeline.")).toBeTruthy();
  expect(picker.disabled).toBe(true);
  const cancels = facade.callsTo("Cancel").length;
  await user.keyboard("{Escape}");
  expect(facade.callsTo("Cancel").slice(cancels).map((call) => call.args)).toEqual([[""]]);
  parked.resolve(laidOut());
  expect(await panel.findByRole("button", { name: GRID_OCCURRENCE })).toBeTruthy();
  const correlationCounts = within(panel.getByLabelText("Correlations", { selector: "dl" }));
  expect(correlationCounts.getByText("Links").nextElementSibling?.textContent).toBe("1");
  expect(correlationCounts.getByText("Collisions").nextElementSibling?.textContent).toBe("1");
  expect(facade.callsTo("OpenSequence").at(-1)?.args[0]).toMatchObject({ rules: RULES, analysis: "", offset: 0 });
  expect(panel.queryByText("cancelled")).toBeNull();
  // The digest a sequence names is the canonical one the command line
  // reports, which is what a sequence analysis pins.
  await user.click(panel.getByText("Rules document"));
  expect(panel.getByText(RULES_SHA256)).toBeTruthy();
  expect(panel.getByRole("region", { name: "Correlation review" })).toBeTruthy();
});

/** The analyst and the reasons the review tests record, short so a keyboard
 * test stays quick on a loaded machine. */
const ANALYST = "analyst one";
const ACCEPTED = "acked booking";
const REJECTED = "separate copy";
const PAIRED = "same delivery";

test("a retained review history is opened by name, accept is recorded from the keyboard with analyst and reason into a new directory the listing then offers, and the retained text is revealed deliberately", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp();
  const panel = await openCase(facade, user);
  const review = await layOut(facade, user, panel);

  // The retained history is chosen by name and opened; nothing is implied.
  facade.reply({ OpenCorrelationReview: () => ({ state: "completed", view: reviewed() }) });
  await user.type(review.getByLabelText("Previous review"), `${REVIEW}{Enter}`);
  expect(await review.findByText("Mapping verified locally.")).toBeTruthy();
  expect(facade.oneCall("OpenCorrelationReview")[0]).toEqual({
    workspace: WORKSPACE_ROOT,
    case: CASE_ENTRY,
    identity: CASE_IDENTITY,
    rules: RULES,
    rules_sha256: RULES_SHA256,
    previous: REVIEW,
    mapping: "",
    decision: { action: "add", link: "", from: "", to: "", actor: "", reason: "" },
    output: "",
    show_values: false,
    offset: 0,
  });
  expect(review.getByText("2 links · 1 original collisions · 0 decisions")).toBeTruthy();

  // Accept, from the keyboard: the decision names the link, the analyst and
  // the reason, binds to the mapping on screen and writes a new directory.
  const listings = facade.callsTo("OpenWorkspace").length;
  facade.reply({
    DecideCorrelation: (request) => ({
      state: "completed",
      output: request.output,
      view: reviewed({ mapping: DECIDED, total_decisions: 1, history: [{ ...request.decision, actor: "", reason: "" }] }),
    }),
  });
  await tabTo(user, review.getAllByRole("button", { name: "Accept" })[0]!);
  await user.keyboard("{Enter}");
  expect(review.getByRole("heading", { name: "accept l000001" })).toBeTruthy();
  await user.type(review.getByLabelText("Analyst (local declaration, not authenticated)"), ANALYST);
  await user.type(review.getByLabelText("Reason"), ACCEPTED);
  await user.type(review.getByLabelText("New review directory"), "review-2{Enter}");
  expect(await review.findByText("Saved to review-2 · mapping verified locally.")).toBeTruthy();
  expect(facade.oneCall("DecideCorrelation")[0]).toMatchObject({
    previous: REVIEW,
    mapping: MAPPING,
    output: "review-2",
    decision: { action: "accept", link: "l000001", from: "", to: "", actor: ANALYST, reason: ACCEPTED },
  });
  // The new revision is where the next decision continues from, and the
  // folder is read again so the retained reviews offer it.
  expect((review.getByLabelText("Previous review") as HTMLInputElement).value).toBe("review-2");
  await waitFor(() => expect(facade.callsTo("OpenWorkspace").length).toBe(listings + 1));
  expect(review.getByText("accept l000001 · analyst and reason hidden")).toBeTruthy();

  // Revealing the retained analyst and reason text is a deliberate read of
  // the same mapping, from the keyboard.
  facade.reply({
    OpenCorrelationReview: (request) => ({
      state: "completed",
      view: reviewed({
        mapping: DECIDED,
        values_shown: request.show_values,
        total_decisions: 1,
        history: [{ action: "accept", link: "l000001", from: "", to: "", actor: ANALYST, reason: ACCEPTED }],
      }),
    }),
  });
  review.getByLabelText("New review directory").focus();
  await tabTo(user, review.getByLabelText("Reveal retained analyst and reason text"));
  await user.keyboard(" ");
  expect(await review.findByText(`accept l000001 · ${ANALYST}: ${ACCEPTED}`)).toBeTruthy();
  expect(facade.callsTo("OpenCorrelationReview").at(-1)?.args[0]).toMatchObject({ previous: "review-2", mapping: DECIDED, show_values: true });
});

test("a decision written over an existing directory is refused leaving no view but keeping the decision, reject and an added pair are then recorded, and a stale review leaves no mapping", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp();
  const panel = await openCase(facade, user);
  const review = await layOut(facade, user, panel);
  facade.reply({ OpenCorrelationReview: () => ({ state: "completed", view: reviewed() }) });
  await user.click(review.getByRole("button", { name: "Open mapping" }));
  await review.findByText("Mapping verified locally.");

  // Reject, written over an existing directory: refused, no view is left
  // standing, and the decision stays for correction.
  facade.reply({ DecideCorrelation: () => refused(WRITE_REFUSED) });
  await user.click(review.getAllByRole("button", { name: "Reject" })[1]!);
  await user.type(review.getByLabelText("Analyst (local declaration, not authenticated)"), ANALYST);
  await user.type(review.getByLabelText("Reason"), REJECTED);
  await user.type(review.getByLabelText("New review directory"), `${REVIEW}{Enter}`);
  expect(await review.findByText(WRITE_REFUSED)).toBeTruthy();
  expect(review.queryByRole("table")).toBeNull();
  expect(facade.oneCall("DecideCorrelation")[0]).toMatchObject({
    previous: "",
    mapping: MAPPING,
    output: REVIEW,
    decision: { action: "reject", link: "l000002", actor: ANALYST, reason: REJECTED },
  });
  await user.click(review.getByRole("button", { name: "Open mapping" }));
  expect(await review.findByRole("heading", { name: "reject l000002" })).toBeTruthy();
  expect((review.getByLabelText("Reason") as HTMLInputElement).value).toBe(REJECTED);
  expect((review.getByLabelText("New review directory") as HTMLInputElement).value).toBe(REVIEW);

  // Saved to a new directory, the rejection lands.
  facade.reply({
    DecideCorrelation: (request) => ({ state: "completed", output: request.output, view: reviewed({ mapping: DECIDED, total_decisions: 1 }) }),
  });
  await user.clear(review.getByLabelText("New review directory"));
  await user.type(review.getByLabelText("New review directory"), "review-2{Enter}");
  expect(await review.findByText("Saved to review-2 · mapping verified locally.")).toBeTruthy();

  // An added pair names two exact occurrences, with its own analyst and
  // reason: a recorded decision leaves nothing of itself in the form.
  await user.click(review.getByRole("button", { name: "Select pair" }));
  expect(review.getByRole("heading", { name: "Add an analyst link" })).toBeTruthy();
  expect((review.getByLabelText("Reason") as HTMLInputElement).value).toBe("");
  await user.type(review.getByLabelText("First occurrence ID"), NEXT_OCCURRENCE);
  await user.type(review.getByLabelText("Second occurrence ID"), "occ-000003");
  await user.type(review.getByLabelText("Analyst (local declaration, not authenticated)"), ANALYST);
  await user.type(review.getByLabelText("Reason"), PAIRED);
  await user.type(review.getByLabelText("New review directory"), "review-3{Enter}");
  expect(await review.findByText("Saved to review-3 · mapping verified locally.")).toBeTruthy();
  expect(facade.callsTo("DecideCorrelation")[2]?.args[0]).toMatchObject({
    previous: "review-2",
    mapping: DECIDED,
    output: "review-3",
    decision: { action: "add", link: "", from: NEXT_OCCURRENCE, to: "occ-000003", actor: ANALYST, reason: PAIRED },
  });

  // The rules changed on disk since the sequence was laid out: the review is
  // refused and no mapping is left on screen.
  facade.reply({ OpenCorrelationReview: () => refused(STALE) });
  await user.click(review.getByRole("button", { name: "Open mapping" }));
  expect(await review.findByText(STALE)).toBeTruthy();
  expect(review.queryByRole("table")).toBeNull();
});

test("while a review opens or a decision is saved the panel says so and holds its controls, Escape reaches Cancel, and what the facade completed is shown as completed", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp();
  const panel = await openCase(facade, user);
  const review = await layOut(facade, user, panel);

  const opening = facade.park("OpenCorrelationReview");
  await user.click(review.getByRole("button", { name: "Open mapping" }));
  expect(await review.findByText("Reading the selected mapping.")).toBeTruthy();
  // The sequence above does not claim to be laid out again.
  expect(panel.queryByText("Loading the timeline.")).toBeNull();
  expect((review.getByRole("button", { name: "Open mapping" }) as HTMLButtonElement).disabled).toBe(true);
  expect((panel.getByLabelText("Correlation rules") as HTMLButtonElement).disabled).toBe(true);
  const cancels = facade.callsTo("Cancel").length;
  await user.keyboard("{Escape}");
  expect(facade.callsTo("Cancel").slice(cancels).map((call) => call.args)).toEqual([[""]]);
  opening.resolve({ state: "completed", view: reviewed() });
  expect(await review.findByText("Mapping verified locally.")).toBeTruthy();

  const saving = facade.park("DecideCorrelation");
  facade.reply({ OpenWorkspace: () => workspace() });
  await user.click(review.getAllByRole("button", { name: "Reject" })[0]!);
  await user.type(review.getByLabelText("Analyst (local declaration, not authenticated)"), "analyst one");
  await user.type(review.getByLabelText("Reason"), "a separate observation");
  await user.type(review.getByLabelText("New review directory"), "review-2{Enter}");
  expect(await review.findByText("Saving this decision to review-2.")).toBeTruthy();
  expect((review.getByRole("button", { name: "Open mapping" }) as HTMLButtonElement).disabled).toBe(true);
  await user.keyboard("{Escape}");
  expect(facade.callsTo("Cancel").slice(cancels).map((call) => call.args)).toEqual([[""], [""]]);
  const request = facade.oneCall("DecideCorrelation")[0] as CorrelationReviewRequest;
  saving.resolve({ state: "completed", output: request.output, view: reviewed({ mapping: DECIDED, total_decisions: 1 }) });
  expect(await review.findByText("Saved to review-2 · mapping verified locally.")).toBeTruthy();
  expect(review.queryByText("cancelled")).toBeNull();
});

test("a review of more links than one page is paged from the keyboard under the mapping on screen", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp();
  const panel = await openCase(facade, user);
  const review = await layOut(facade, user, panel);
  facade.reply({
    OpenCorrelationReview: (request) => ({ state: "completed", view: reviewed({ offset: request.offset, total_links: 250 }) }),
  });
  await user.click(review.getByRole("button", { name: "Open mapping" }));
  expect(await review.findByText("Each list shows up to 200 items starting at 1; membership shows up to 32 occurrences.")).toBeTruthy();
  expect((review.getByRole("button", { name: "Previous review page" }) as HTMLButtonElement).disabled).toBe(true);

  review.getByRole("button", { name: "Open mapping" }).focus();
  await tabTo(user, review.getByRole("button", { name: "Next review page" }));
  await user.keyboard("{Enter}");
  expect(await review.findByText("Each list shows up to 200 items starting at 201; membership shows up to 32 occurrences.")).toBeTruthy();
  expect(facade.callsTo("OpenCorrelationReview")[1]?.args[0]).toMatchObject({ previous: "", mapping: MAPPING, offset: 200, show_values: false });
  expect((review.getByRole("button", { name: "Next review page" }) as HTMLButtonElement).disabled).toBe(true);

  // The page on screen is withdrawn while the next one is read, so the
  // keyboard starts again from the review's own controls.
  review.getByRole("button", { name: "Open mapping" }).focus();
  await tabTo(user, review.getByRole("button", { name: "Previous review page" }));
  await user.keyboard("{Enter}");
  expect(await review.findByText("Each list shows up to 200 items starting at 1; membership shows up to 32 occurrences.")).toBeTruthy();
  expect(facade.callsTo("OpenCorrelationReview")[2]?.args[0]).toMatchObject({ mapping: MAPPING, offset: 0 });
});

/** The rules RULES declares, as the Go reader decoded them. */
const RETAINED_RULES: CorrelationRulesDocument = {
  schema: "readmit-correlation-rules/v1",
  authorities: [{ key: "READMIT-MR", namespace: "READMIT", universal_id: "", universal_id_type: "" }],
  rules: [
    { id: "acknowledgements", operator: "acknowledges", scope: "source" },
    { id: "same-booking", operator: "control-id", scope: "declared", sources: ["s0001", "s0002"] },
  ],
};

async function rulesEditor(user: User, panel: ReturnType<typeof within>) {
  await user.click(panel.getByText("Rules and coverage documents"));
  return within(panel.getByRole("region", { name: "Correlation rules editor" }));
}

test("a retained rules document opens into the structured rules with its identity, an identifier rule is added with its value and authority selectors, an open over unsaved rules asks first and Escape keeps them, and a refused document leaves them", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp();
  const panel = await openCase(facade, user);
  const editor = await rulesEditor(user, panel);
  const rules = () => editor.queryAllByRole("button", { name: /^Remove rule / }).map((button) => button.textContent);

  facade.reply({
    OpenCorrelationRules: (_workspace, entry) =>
      entry === RULES
        ? { state: "completed", document: JSON.stringify(RETAINED_RULES, null, 2), sha256: ENTRY_SHA256, rules: RETAINED_RULES }
        : refused("invalid correlation rules JSON"),
  });
  await user.selectOptions(editor.getByLabelText("Retained rules document"), RULES);
  await user.click(editor.getByRole("button", { name: "Open" }));
  await waitFor(() => expect(rules()).toEqual(["Remove rule acknowledgements", "Remove rule same-booking"]));
  expect(editor.getByRole("button", { name: "Remove authority READMIT-MR" })).toBeTruthy();
  expect(editor.getByText(`Opened ${RULES} · exact bytes hash to ${ENTRY_SHA256}`)).toBeTruthy();

  // An identifier rule names its value and its three authority selectors,
  // and is added to the opened rules rather than in place of them.
  await user.type(editor.getByLabelText("Rule ID"), "patient");
  await user.selectOptions(editor.getByLabelText("Operator"), "identifier");
  await user.selectOptions(editor.getByLabelText("Scope"), "declared");
  await user.type(editor.getByLabelText("Sources"), "s0001 s0002");
  await user.type(editor.getByLabelText("Identifier value selector"), "PID-3.1");
  await user.type(
    editor.getByLabelText("Assigning authorities"),
    "PID-3.4.1 PID-3.4.2 PID-3.4.3{Enter}",
  );
  expect(rules()).toEqual(["Remove rule acknowledgements", "Remove rule same-booking", "Remove rule patient"]);
  const composed = JSON.parse((editor.getByLabelText("Document JSON") as HTMLTextAreaElement).value) as CorrelationRulesDocument;
  expect(composed).toEqual({
    ...RETAINED_RULES,
    rules: [
      ...RETAINED_RULES.rules,
      { id: "patient", operator: "identifier", scope: "declared", sources: ["s0001", "s0002"], value: "PID-3.1", authority: ["PID-3.4.1", "PID-3.4.2", "PID-3.4.3"] },
    ],
  });

  // Opening another document now would replace unsaved rules: it asks, and
  // Escape keeps them without reading anything or cancelling anything else.
  await user.selectOptions(editor.getByLabelText("Retained rules document"), BROKEN_RULES);
  await user.click(editor.getByRole("button", { name: "Open" }));
  const question = within(editor.getByRole("group", { name: `Open ${BROKEN_RULES} in place of these rules?` }));
  expect(document.activeElement).toBe(question.getByRole("button", { name: "Keep rules" }));
  const cancels = facade.callsTo("Cancel").length;
  await user.keyboard("{Escape}");
  expect(editor.queryByRole("group", { name: /^Open / })).toBeNull();
  expect(document.activeElement).toBe(editor.getByRole("button", { name: "Open" }));
  expect(facade.callsTo("OpenCorrelationRules")).toHaveLength(1);
  expect(facade.callsTo("Cancel")).toHaveLength(cancels);

  // Keep these rules, pressed, answers the same way; Replace reads the
  // document, and the reader's refusal leaves the rules.
  await user.keyboard("{Enter}");
  await user.click(editor.getByRole("button", { name: "Keep rules" }));
  expect(facade.callsTo("OpenCorrelationRules")).toHaveLength(1);
  await user.keyboard("{Enter}");
  await user.click(editor.getByRole("button", { name: "Replace rules" }));
  expect(await editor.findByText("invalid correlation rules JSON")).toBeTruthy();
  expect(facade.callsTo("OpenCorrelationRules")[1]?.args).toEqual([WORKSPACE_ROOT, BROKEN_RULES]);
  expect(rules()).toHaveLength(3);

  // Saved while the question is open, the rules are no longer unsaved: the
  // question is withdrawn, and the new entry is offered once the folder is
  // read again.
  await user.click(editor.getByRole("button", { name: "Open" }));
  expect(editor.getByRole("group", { name: `Open ${BROKEN_RULES} in place of these rules?` })).toBeTruthy();
  const saving = facade.park("SaveCorrelationRules");
  await user.type(editor.getByLabelText("New correlation-rules entry"), "extended.rules.json{Enter}");
  expect(await editor.findByText("Saving extended.rules.json.")).toBeTruthy();
  expect((editor.getByRole("button", { name: "Add rule" }) as HTMLButtonElement).disabled).toBe(true);
  saving.resolve({ state: "completed", output: "extended.rules.json", sha256: "saved-sha256-fixed-for-tests", document: "{}" });
  expect(await editor.findByText("Saved to extended.rules.json · exact bytes hash to saved-sha256-fixed-for-tests")).toBeTruthy();
  expect(editor.queryByRole("group", { name: /^Open / })).toBeNull();
  expect(JSON.parse(facade.oneCall("SaveCorrelationRules")[0].document)).toEqual(composed);
});

/** The declaration ANALYSIS holds, as the Go reader decoded it. */
const RETAINED_ANALYSIS: SequenceAnalysisDeclaration = {
  schema: "readmit-sequence-analysis/v1",
  case_identity: CASE_IDENTITY,
  rules_sha256: RULES_SHA256,
  clock_tolerance_seconds: 5,
  windows: [{ source: "s0001", start: "2026-01-05T09:00:00Z", end: "2026-01-05T10:00:00Z", coverage: "partial" }],
  retries: [],
  downstream: [{ occurrence: GRID_OCCURRENCE, source: "s0002", rule: "same-booking" }],
};

test("a retained sequence-analysis declaration opens into the structured controls with its identity, is extended and saved, an open over unsaved work asks first and Escape keeps it, and a declaration of another case keeps the identity it names", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp();
  const panel = await openCase(facade, user);
  await user.click(panel.getByText("Rules and coverage documents"));
  const editor = within(panel.getByRole("region", { name: "Sequence analysis editor" }));
  const windows = () => editor.queryAllByRole("button", { name: /^Remove window / }).map((button) => button.textContent);

  // While the declaration is read the editor says so and holds its controls;
  // Escape reaches the window's Cancel, and the completed open is shown.
  const opening = facade.park("OpenSequenceAnalysis");
  await user.selectOptions(editor.getByLabelText("Retained analysis document"), ANALYSIS);
  await user.click(editor.getByRole("button", { name: "Open" }));
  expect(await editor.findByText(`Opening ${ANALYSIS}.`)).toBeTruthy();
  expect((editor.getByRole("button", { name: "Add window" }) as HTMLButtonElement).disabled).toBe(true);
  const cancels = facade.callsTo("Cancel").length;
  await user.keyboard("{Escape}");
  expect(facade.callsTo("Cancel").slice(cancels)).toHaveLength(1);
  opening.resolve({ state: "completed", document: JSON.stringify(RETAINED_ANALYSIS, null, 2), sha256: ENTRY_SHA256, declaration: RETAINED_ANALYSIS });
  expect(await editor.findByText(`Opened ${ANALYSIS} · exact bytes hash to ${ENTRY_SHA256}`)).toBeTruthy();
  expect(windows()).toEqual(["Remove window s0001"]);
  expect(editor.getByRole("button", { name: `Remove downstream ${GRID_OCCURRENCE}` })).toBeTruthy();
  expect((editor.getByLabelText("Correlation rules hash") as HTMLInputElement).value).toBe(RULES_SHA256);
  expect((editor.getByLabelText("Clock comparison tolerance, seconds") as HTMLInputElement).value).toBe("5");

  // A second window extends the opened declaration.
  await user.type(editor.getByLabelText("Window source"), "s0002");
  await user.type(editor.getByLabelText("Start (UTC offset required)"), "2026-01-05T09:00:00Z");
  await user.type(editor.getByLabelText("End (UTC offset required)"), "2026-01-05T10:00:00Z{Enter}");
  expect(windows()).toEqual(["Remove window s0001", "Remove window s0002"]);
  const composed = JSON.parse((editor.getByLabelText("Document JSON") as HTMLTextAreaElement).value) as SequenceAnalysisDeclaration;
  expect(composed).toEqual({
    ...RETAINED_ANALYSIS,
    windows: [...RETAINED_ANALYSIS.windows, { source: "s0002", start: "2026-01-05T09:00:00Z", end: "2026-01-05T10:00:00Z", coverage: "partial" }],
  });

  // Another declaration, asked about first: Escape keeps this one.
  const other: SequenceAnalysisDeclaration = { ...RETAINED_ANALYSIS, case_identity: "another-case-identity", downstream: [] };
  facade.reply({
    OpenSequenceAnalysis: (_workspace, entry) =>
      entry === OTHER_ANALYSIS
        ? { state: "completed", document: JSON.stringify(other, null, 2), sha256: "other-sha256-fixed-for-tests", declaration: other }
        : refused("sequence analysis requires every declared member"),
  });
  await user.selectOptions(editor.getByLabelText("Retained analysis document"), OTHER_ANALYSIS);
  await user.click(editor.getByRole("button", { name: "Open" }));
  const question = within(editor.getByRole("group", { name: `Open ${OTHER_ANALYSIS} in place of this declaration?` }));
  expect(document.activeElement).toBe(question.getByRole("button", { name: "Keep declaration" }));
  await user.keyboard("{Escape}");
  expect(editor.queryByRole("group", { name: /^Open / })).toBeNull();
  expect(document.activeElement).toBe(editor.getByRole("button", { name: "Open" }));
  expect(facade.callsTo("OpenSequenceAnalysis")).toHaveLength(1);
  expect(windows()).toHaveLength(2);

  // Saved, the extended declaration is a new entry.
  facade.reply({
    SaveSequenceAnalysis: (request) => ({ state: "completed", output: request.output, sha256: "saved-sha256-fixed-for-tests", document: request.document }),
  });
  await user.type(editor.getByLabelText("New sequence-analysis entry"), "extended-analysis.json{Enter}");
  expect(await editor.findByText("Saved to extended-analysis.json · exact bytes hash to saved-sha256-fixed-for-tests")).toBeTruthy();
  expect(JSON.parse(facade.oneCall("SaveSequenceAnalysis")[0].document)).toEqual(composed);

  // With nothing unsaved, the other case's declaration opens at once and
  // keeps the identity it binds to; the window says the open case's differs.
  await user.click(editor.getByRole("button", { name: "Open" }));
  await waitFor(() => expect(windows()).toEqual(["Remove window s0001"]));
  expect(editor.getByText(/binds to the verified case identity another-case-identity\./)).toBeTruthy();
  expect(editor.getByText(new RegExp(`The open case's identity is ${CASE_IDENTITY}\\. Laying the open case out under this declaration is refused`))).toBeTruthy();
  await user.type(editor.getByLabelText("Window source"), "s0002");
  await user.type(editor.getByLabelText("Start (UTC offset required)"), "2026-01-05T09:00:00Z");
  await user.type(editor.getByLabelText("End (UTC offset required)"), "2026-01-05T10:00:00Z{Enter}");
  expect((JSON.parse((editor.getByLabelText("Document JSON") as HTMLTextAreaElement).value) as SequenceAnalysisDeclaration).case_identity).toBe("another-case-identity");

  // A declaration the reader refuses leaves the controls as they were.
  await user.selectOptions(editor.getByLabelText("Retained analysis document"), ANALYSIS);
  await user.click(editor.getByRole("button", { name: "Open" }));
  await user.click(editor.getByRole("button", { name: "Replace declaration" }));
  expect(await editor.findByText("sequence analysis requires every declared member")).toBeTruthy();
  expect(windows()).toEqual(["Remove window s0001", "Remove window s0002"]);
});
