// Correlating a case under declared rules, reviewing the links, and reopening
// a sequence analysis, over the real facade.
//
// A scheduling system books appointments into a clinic's receiver through an
// interface engine. The person captured, with the command line, what the
// scheduler sent — four bookings and the two acknowledgements it got back —
// and what the receiver took in, into one case of two sources. The receiver
// took the second booking twice and never saw the fourth. A colleague wrote
// the rules the team correlates by: an acknowledgement within its own source,
// one booking across both captures by its control ID, and one patient across
// both under the READMIT medical-record authority.
//
// The window lays the case out under those rules exactly as `readmit
// correlate` links it, and refuses a rules document the command line refuses
// in the same sentence. The person reviews the links: accepts one, rejects
// one, adds the pair the rules could not decide, each with their name and a
// reason, into new directories beside the machine finding the command line
// reports, and reopens a history by name; a decision written over an earlier
// one is refused, and a review under rules changed on disk is refused as
// stale. They reopen a colleague's sequence-analysis declaration, extend it
// and save it as a new entry the sequence applies; an open that would replace
// their unsaved work is asked first and cancelled.
//
// Every message and value here is synthetic.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { enter, Journey, press, region, whenEnabled } from "../testkit/journey";
import { activateLicense, framed, tabTo } from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

/** One booking the scheduler sent, MLLP-framed. Every value is synthetic. */
function booking(event: string, control: string, record: string): string {
  return framed(
    `MSH|^~\\&|SCHEDULER|SYNTHETIC|CLINIC|RECEIVER|20260105090000+0000||SIU^${event}|${control}|P|2.5.1\r` +
      `SCH|PLACER-${control}^READMIT\r` +
      `PID|1||${record}^^^READMIT||SYNTHETIC^ONLY\r`,
  );
}

/** The receiver's acknowledgement of one booking. */
function acknowledgement(control: string, acknowledged: string): string {
  return framed(
    `MSH|^~\\&|CLINIC|RECEIVER|SCHEDULER|SYNTHETIC|20260105090001+0000||ACK^S12|${control}|P|2.5.1\r` +
      `MSA|AA|${acknowledged}\r`,
  );
}

/** What the scheduler's capture holds, in order, and what the receiver's
 * does: the second booking twice, and never the fourth. */
const SENT = [
  booking("S12", "BOOK-1", "MR-1"),
  acknowledgement("ACK-1", "BOOK-1"),
  booking("S12", "BOOK-2", "MR-2"),
  acknowledgement("ACK-2", "BOOK-2"),
  booking("S14", "BOOK-3", "MR-1"),
  booking("S12", "BOOK-4", "MR-3"),
];
const RECEIVED = [booking("S12", "BOOK-1", "MR-1"), booking("S12", "BOOK-2", "MR-2"), booking("S12", "BOOK-2", "MR-2"), booking("S14", "BOOK-3", "MR-1")];

/** The occurrence the nth message of a source is. */
const sent = (n: number) => `s0001-e${String(n).padStart(6, "0")}`;
const received = (n: number) => `s0002-e${String(n).padStart(6, "0")}`;
const OCCURRENCES = SENT.length + RECEIVED.length;
const ACKNOWLEDGEMENTS = 2;
const MESSAGES = OCCURRENCES - ACKNOWLEDGEMENTS;

/** The colleague's rules. */
const RULES = `${JSON.stringify(
  {
    schema: "readmit-correlation-rules/v1",
    authorities: [{ key: "READMIT-MR", namespace: "READMIT", universal_id: "", universal_id_type: "" }],
    rules: [
      { id: "acknowledgements", operator: "acknowledges", scope: "source" },
      { id: "same-booking", operator: "control-id", scope: "declared", sources: ["s0001", "s0002"] },
      {
        id: "patient",
        operator: "identifier",
        scope: "declared",
        sources: ["s0001", "s0002"],
        value: "PID-3.1",
        authority: ["PID-3.4.1", "PID-3.4.2", "PID-3.4.3"],
      },
    ],
  },
  null,
  2,
)}\n`;

/** A document naming only two of an identifier's three authority parts. */
const TWO_PARTS = `{"schema":"readmit-correlation-rules/v1","rules":[{"id":"patient","operator":"identifier","scope":"source","value":"PID-3.1","authority":["PID-3.4.1","PID-3.4.2"]}]}\n`;
const TWO_PARTS_REFUSED = "an identifier rule declares exactly three authority selectors: namespace, universal ID and universal ID type";

/** What the rules link, stated from the scenario. Each acknowledgement names
 * the booking before it in the scheduler's capture; bookings 1 and 3 are one
 * control ID in both captures, and booking 2 is one control ID three times,
 * twice in the receiver's, which is a collision rather than a link; MR-1 is
 * bookings 1 and 3 in both captures, and MR-2 booking 2 in all three places.
 * MR-3 is only the fourth booking, which nothing else holds. */
const LINKS = [
  { id: "l000001", rule: "acknowledgements", linkage: "observed", occurrences: [sent(2), sent(1)] },
  { id: "l000002", rule: "acknowledgements", linkage: "observed", occurrences: [sent(4), sent(3)] },
  { id: "l000003", rule: "same-booking", linkage: "inferred", occurrences: [sent(1), received(1)] },
  { id: "l000004", rule: "same-booking", linkage: "inferred", occurrences: [sent(5), received(4)] },
  { id: "l000005", rule: "patient", linkage: "inferred", occurrences: [sent(1), sent(5), received(1), received(4)] },
  { id: "l000006", rule: "patient", linkage: "inferred", occurrences: [sent(3), received(2), received(3)] },
];
const COLLISION = { rule: "same-booking", reason: "duplicate_control_id", occurrences: [sent(3), received(2), received(3)] };

/** Each rule as the sequence lists it: what it considered, linked and did
 * not. Every occurrence carries a control ID and every booking a patient. */
const RULES_APPLIED = [
  `acknowledgements acknowledges source applied ${OCCURRENCES} considered · 4 linked · ${OCCURRENCES - 4} unlinked`,
  `same-booking control-id declared (s0001, s0002) applied ${OCCURRENCES} considered · 4 linked · ${OCCURRENCES - 4} unlinked`,
  `patient identifier declared (s0001, s0002) applied ${MESSAGES} considered · 7 linked · 1 unlinked`,
];

interface CorrelationReport {
  case_identity: string;
  rules_sha256: string;
  summary: Record<string, number>;
  rules: { id: string; considered: number; linked: number; unlinked: number }[];
  links: { id: string; rule: string; linkage: string; occurrences: { occurrence: string }[] }[];
  collisions: { rule: string; reason: string; occurrences: { occurrence: string }[] }[];
}

/** The license's operation policy: capturing is licensed work. */
function operationPolicy(): string {
  return journey.path("vendor-delivered-license", "operation-policy.json");
}

/** Captures both feeds into one case with the command line, as the person
 * did before opening the window, places the colleague's rules beside it, and
 * returns the identity the capture reported. */
async function captureIncident(): Promise<string> {
  journey.writeFile("exports/scheduler-sent.mllp", SENT.join(""));
  journey.writeFile("exports/receiver-took.mllp", RECEIVED.join(""));
  journey.makeFolder("scheduling");
  const captured = await journey.commandLine([
    "--operation-policy",
    operationPolicy(),
    "capture",
    "exports/scheduler-sent.mllp",
    "exports/receiver-took.mllp",
    "--output",
    "scheduling/incident",
  ]);
  expect(captured.code, captured.stderr).toBe(0);
  journey.writeFile("scheduling/scheduling.rules.json", RULES);
  const identity = /^Bundle: ([0-9a-f]{64})$/m.exec(captured.stdout)?.[1];
  expect(identity).toBeTruthy();
  return identity!;
}

/** `readmit correlate --format json` over the incident under one rules entry. */
async function correlated(rules: string): Promise<{ report: CorrelationReport; printed: string }> {
  const run = await journey.commandLine(["correlate", "scheduling/incident", "--rules", `scheduling/${rules}`, "--format", "json"]);
  expect(run.code, run.stderr).toBe(0);
  return { report: JSON.parse(run.stdout) as CorrelationReport, printed: run.stdout };
}

/** Opens the scheduling folder and verifies the incident, and returns the
 * sequence panel. */
async function openIncident(user: UserEvent) {
  await journey.chooseFolder(journey.path("scheduling"), "Open workspace");
  await press(user, screen.getAllByRole("button", { name: "Open workspace…" })[0] as HTMLElement);
  const navigation = within(region("Workspace"));
  const listed = (await navigation.findByText("incident", { selector: ".name" })).closest("li") as HTMLElement;
  await press(user, within(listed).getByRole("button", { name: "Open case" }));
  return within(await screen.findByRole("region", { name: "Sequence" }));
}

/** Chooses a rules document and an analysis, lays the case out from the
 * keyboard and waits for the answer. */
async function layOut(user: UserEvent, panel: ReturnType<typeof within>, rules: string, analysis = ""): Promise<void> {
  const rulesPicker = panel.getByLabelText("Correlation rules");
  if (rules !== "") await within(rulesPicker).findByRole("option", { name: rules });
  await user.selectOptions(await whenEnabled(rulesPicker), rules);
  const analysisPicker = panel.getByLabelText("Observations");
  if (analysis !== "") await within(analysisPicker).findByRole("option", { name: analysis });
  await user.selectOptions(analysisPicker, analysis);
  const asked = journey.callsTo("OpenSequence").length;
  await tabTo(user, panel.getByRole("button", { name: "View sequence" }));
  await user.keyboard("{Enter}");
  await waitFor(() => expect(journey.callsTo("OpenSequence")[asked]?.settled).toBe(true));
}

/** Each rule the sequence lists as applied, read as a person reads the row. */
function rulesApplied(panel: ReturnType<typeof within>): string[] {
  const list = panel.getByRole("heading", { name: "Rules applied" }).nextElementSibling as HTMLElement;
  return within(list)
    .getAllByRole("listitem")
    .map((item: HTMLElement) => Array.from(item.children).map((part) => part.textContent ?? "").join(" "));
}

/** What the sequence lists beside one opened event: the kind of each
 * reference, the rule that made it, and what else it names. */
function referencesDrawn(section: HTMLElement): string[] {
  return Array.from(section.querySelectorAll("ul.references > li")).map((item) =>
    [".reference-kind", ".rule", ".related"].map((part) => item.querySelector(part)?.textContent ?? "").filter(Boolean).join(" · "),
  );
}

test("a case is laid out under a chosen correlation-rules document exactly as readmit correlate links it, and a rules document the command line refuses is refused in its sentence, in the sequence and in the editor", async () => {
  const user = userEvent.setup();
  journey.provisionLicense("vendor-delivered-license");
  const identity = await captureIncident();
  journey.writeFile("scheduling/two-parts.rules.json", TWO_PARTS);
  await journey.launch();
  const panel = await openIncident(user);

  // The rules picker offers the entries declaring the rules contract, beside
  // no rules at all.
  const picker = panel.getByLabelText("Correlation rules") as HTMLSelectElement;
  expect(Array.from(picker.options).map((option) => option.value)).toEqual(["", "scheduling.rules.json", "two-parts.rules.json"]);

  // A document naming two of three authority parts is refused in the
  // command line's sentence, and nothing is laid out beside the refusal.
  await layOut(user, panel, "two-parts.rules.json");
  expect(await panel.findByText(TWO_PARTS_REFUSED)).toBeTruthy();
  expect(panel.queryByRole("table")).toBeNull();
  const refusedRun = await journey.commandLine(["correlate", "scheduling/incident", "--rules", "scheduling/two-parts.rules.json"]);
  expect(refusedRun.code).toBe(1);
  expect(refusedRun.stdout).toBe("");
  expect(refusedRun.stderr).toBe(`readmit: ${TWO_PARTS_REFUSED}\n`);

  // Under the colleague's rules, the case is linked as the scenario says,
  // and the command line links it the same way.
  await layOut(user, panel, "scheduling.rules.json");
  expect(await panel.findByText("6 links · 1 collision · 0 not evaluated · readmit-correlation/v1")).toBeTruthy();
  const events = panel.getByRole("table", { name: `incident · ${OCCURRENCES} occurrences over 2 sources` });
  expect(rulesApplied(panel)).toEqual(RULES_APPLIED);
  const { report } = await correlated("scheduling.rules.json");
  expect(report.case_identity).toBe(identity);
  expect(report.summary).toMatchObject({ occurrences: OCCURRENCES, links: 6, observed: 2, inferred: 4, collisions: 1, unsupported: 0 });
  expect(report.links.map((link) => ({ id: link.id, rule: link.rule, linkage: link.linkage, occurrences: link.occurrences.map((named) => named.occurrence) }))).toEqual(LINKS);
  expect(report.collisions.map((collision) => ({ rule: collision.rule, reason: collision.reason, occurrences: collision.occurrences.map((named) => named.occurrence) }))).toEqual([COLLISION]);
  expect(report.rules.map((rule) => [rule.id, rule.considered, rule.linked, rule.unlinked])).toEqual([
    ["acknowledgements", OCCURRENCES, 4, OCCURRENCES - 4],
    ["same-booking", OCCURRENCES, 4, OCCURRENCES - 4],
    ["patient", MESSAGES, 7, 1],
  ]);

  // The digest the sequence names is the canonical one the command line
  // reports, which is not the digest of the file's bytes.
  expect(panel.getByText(`Read under the rules in scheduling.rules.json, whose canonical rules SHA-256 is ${report.rules_sha256}: the digest readmit correlate reports for them and a sequence analysis names.`)).toBeTruthy();
  expect(report.rules_sha256).not.toBe(journey.digest("scheduling/scheduling.rules.json"));

  // The scheduler's second booking is position 3: acknowledged in its own
  // capture, the same patient as both of the receiver's copies, and one of
  // three occurrences of one control ID, which were never merged.
  await press(user, within(events).getByRole("button", { name: "3" }));
  const opened = await panel.findByRole("region", { name: "What is recorded about the selected event" });
  expect(within(opened).getByRole("heading", { name: `${sent(3)} · 4 references` })).toBeTruthy();
  expect(referencesDrawn(opened)).toEqual([
    `Acknowledgement recorded by the evidence · with ${sent(4)}`,
    `Linked by a declared rule · rule acknowledgements · with ${sent(4)}`,
    `Linked by a declared rule · rule patient · with ${received(2)}, ${received(3)}`,
    `Equal keys that were never merged · rule same-booking · with ${received(2)}, ${received(3)}`,
  ]);

  // In the editor, the colleague's rules open as their three rules and their
  // authority, named by the digest of the file's bytes.
  await user.click(panel.getByText("Rules and analysis"));
  const editor = within(panel.getByRole("region", { name: "Correlation rules editor" }));
  const rules = () => editor.queryAllByRole("button", { name: /^Remove rule / }).map((button) => button.textContent);
  await user.selectOptions(await whenEnabled(editor.getByLabelText("Retained rules document")), "scheduling.rules.json");
  await press(user, editor.getByRole("button", { name: "Open" }));
  await waitFor(() => expect(rules()).toEqual(["Remove rule acknowledgements", "Remove rule same-booking", "Remove rule patient"]));
  expect(editor.getByRole("button", { name: "Remove authority READMIT-MR" })).toBeTruthy();
  expect(editor.getByText(`Opened scheduling.rules.json · exact bytes hash to ${journey.digest("scheduling/scheduling.rules.json")}`)).toBeTruthy();

  // The person adds a rule of their own. Opening the refused document now
  // would replace it: the window asks, and Escape keeps the rules and reads
  // nothing.
  await enter(user, editor.getByLabelText("Rule ID"), "placer-booking");
  await user.selectOptions(editor.getByLabelText("Scope"), "session");
  await user.click(editor.getByRole("button", { name: "Add rule" }));
  expect(rules()).toHaveLength(4);
  const opens = journey.callsTo("OpenCorrelationRules").length;
  await user.selectOptions(editor.getByLabelText("Retained rules document"), "two-parts.rules.json");
  await press(user, editor.getByRole("button", { name: "Open" }));
  const question = within(editor.getByRole("group", { name: "Open two-parts.rules.json in place of these rules?" }));
  expect(document.activeElement).toBe(question.getByRole("button", { name: "Keep rules" }));
  await user.keyboard("{Escape}");
  expect(editor.queryByRole("group", { name: /^Open / })).toBeNull();
  expect(document.activeElement).toBe(editor.getByRole("button", { name: "Open" }));
  expect(journey.callsTo("OpenCorrelationRules")).toHaveLength(opens);

  // Answered the other way, the document is refused in the command line's
  // sentence and the four rules stay.
  await user.keyboard("{Enter}");
  await press(user, editor.getByRole("button", { name: "Replace rules" }));
  expect(await editor.findByText(TWO_PARTS_REFUSED)).toBeTruthy();
  expect(rules()).toHaveLength(4);
});

/** The review panel below a laid-out sequence. */
function reviewPanel(panel: ReturnType<typeof within>) {
  return within(panel.getByRole("region", { name: "Correlation review" }));
}

/** The original collisions the review lists, which no decision overwrites. */
function collisionsListed(review: ReturnType<typeof within>): string {
  const heading = review.getByRole("heading", { name: "Original ambiguities (never overwritten by decisions)" });
  return (heading.nextElementSibling as HTMLElement).textContent ?? "";
}

/** How the review lists the one collision the rules found. */
const COLLISION_LISTED = `${COLLISION.rule} · ${COLLISION.reason} · declaring noneCandidates: ${COLLISION.occurrences.join(", ")} (3 total)`;

/** The reviewed links as the table draws them: link, origin, occurrences and
 * status. */
function reviewedLinks(review: ReturnType<typeof within>): string[] {
  return review.getAllByRole("row").slice(1).map((row: HTMLElement) =>
    Array.from(row.children)
      .slice(0, 4)
      .map((cell) => cell.textContent ?? "")
      .join(" | "),
  );
}

function linkRow(link: (typeof LINKS)[number], status: string): string {
  return `${link.id} | ${link.linkage} · ${link.rule} | ${link.occurrences.join(", ")} (${link.occurrences.length} total) | ${status}`;
}

/** Saves the decision the review form holds into a new directory. */
async function saveDecision(user: UserEvent, review: ReturnType<typeof within>, output: string): Promise<void> {
  await enter(user, review.getByLabelText("New review directory"), output);
  const asked = journey.callsTo("DecideCorrelation").length;
  await press(user, review.getByRole("button", { name: "Save decision" }));
  await waitFor(() => expect(journey.callsTo("DecideCorrelation")[asked]?.settled).toBe(true));
}

/** Records one decision from the review form, in the analyst's name and
 * with their reason, into a new directory. */
async function decide(user: UserEvent, review: ReturnType<typeof within>, analyst: string, reason: string, output: string): Promise<void> {
  await enter(user, review.getByLabelText("Analyst (local declaration, not authenticated)"), analyst);
  await enter(user, review.getByLabelText("Reason"), reason);
  await saveDecision(user, review, output);
}

/** One recorded history, as the retained decisions document holds it. */
interface ReviewDecisions {
  schema: string;
  decisions: { action: string; link: string; from: string; to: string; actor: string; reason: string }[];
}

const ANALYST = "Synthetic Analyst";
const ACCEPTED = "the acknowledgement names this booking";
const REJECTED = "MR-2 is reused by the receiver's test patient";
const PAIRED = "the receiver's first copy is the delivery; the second is the engine's retry";
const WRITE_REFUSED = "correlation review destination must be new and its parent readable and writable";
const STALE_RULES = "the displayed rules changed; reopen the sequence before reviewing";
const STALE_HISTORY = "correlation review is incomplete, changed or incompatible; reopen the original evidence and rules";

test("a review history is opened from the machine findings and reopened by name, accept, reject and an added pair are recorded with analyst and reason into new directories beside the finding readmit correlate reports, and a refused write and a stale review change nothing", async () => {
  const user = userEvent.setup();
  await journey.launch();
  await activateLicense(user, journey);
  await captureIncident();
  const panel = await openIncident(user);
  await layOut(user, panel, "scheduling.rules.json");
  const { printed } = await correlated("scheduling.rules.json");

  // Opened with no retained history, the review starts from the machine
  // finding: every link unreviewed, and the collision listed as it is.
  let review = reviewPanel(panel);
  await press(user, review.getByRole("button", { name: "Open mapping" }));
  expect(await review.findByText("Mapping verified locally.")).toBeTruthy();
  expect(review.getByText(`${LINKS.length} links · 1 original collisions · 0 decisions`)).toBeTruthy();
  expect(reviewedLinks(review)).toEqual(LINKS.map((link) => linkRow(link, "unreviewed")));
  expect(collisionsListed(review)).toBe(COLLISION_LISTED);

  // The person accepts the first acknowledgement link, reaching Accept from
  // the keyboard, in their own name and with their reason.
  const firstRow = review.getAllByRole("row")[1]!;
  await tabTo(user, within(firstRow).getByRole("button", { name: "Accept" }));
  await user.keyboard("{Enter}");
  expect(review.getByRole("heading", { name: "accept l000001" })).toBeTruthy();
  await decide(user, review, ANALYST, ACCEPTED, "review-accepted");
  expect(await review.findByText("Saved to review-accepted · mapping verified locally.")).toBeTruthy();
  expect(reviewedLinks(review)[0]).toBe(linkRow(LINKS[0]!, "accepted"));
  expect(review.getByText("accept l000001 · analyst and reason hidden")).toBeTruthy();
  // The machine finding kept beside the decision is exactly what the command
  // line printed, and the decision is kept as the person made it.
  expect(journey.readFile("scheduling/review-accepted/machine.json")).toBe(printed.replace(/\n$/, ""));
  const accepted = JSON.parse(journey.readFile("scheduling/review-accepted/decisions.json")) as ReviewDecisions;
  expect(accepted.schema).toBe("readmit-correlation-review/v1");
  expect(accepted.decisions).toEqual([{ action: "accept", link: "l000001", from: "", to: "", actor: ANALYST, reason: ACCEPTED }]);
  const acceptedDigest = journey.digest("scheduling/review-accepted/decisions.json");

  // The rejection of the MR-2 patient link, written over that directory, is
  // refused: no view stands beside the refusal and the directory is as it
  // was. Reopened, the decision is still in the form.
  await press(user, within(review.getAllByRole("row")[6]!).getByRole("button", { name: "Reject" }));
  await decide(user, review, ANALYST, REJECTED, "review-accepted");
  expect(await review.findByText(WRITE_REFUSED)).toBeTruthy();
  expect(review.queryByRole("table")).toBeNull();
  expect(journey.digest("scheduling/review-accepted/decisions.json")).toBe(acceptedDigest);
  await press(user, review.getByRole("button", { name: "Open mapping" }));
  expect(await review.findByRole("heading", { name: "reject l000006" })).toBeTruthy();
  await saveDecision(user, review, "review-rejected");
  expect(await review.findByText("Saved to review-rejected · mapping verified locally.")).toBeTruthy();
  expect(reviewedLinks(review)[5]).toBe(linkRow(LINKS[5]!, "rejected"));

  // The pair the rules could not decide: the scheduler's second booking and
  // the receiver's first copy of it. The collision stays listed as it was.
  await press(user, review.getByRole("button", { name: "Select pair" }));
  await enter(user, review.getByLabelText("First occurrence ID"), sent(3));
  await enter(user, review.getByLabelText("Second occurrence ID"), received(2));
  await decide(user, review, ANALYST, PAIRED, "review-paired");
  expect(await review.findByText("Saved to review-paired · mapping verified locally.")).toBeTruthy();
  expect(reviewedLinks(review)).toEqual([
    linkRow(LINKS[0]!, "accepted"),
    ...LINKS.slice(1, 5).map((link) => linkRow(link, "unreviewed")),
    linkRow(LINKS[5]!, "rejected"),
    `manual-000003 | manual | ${sent(3)}, ${received(2)} (2 total) | accepted`,
  ]);
  expect(collisionsListed(review)).toBe(COLLISION_LISTED);
  const paired = JSON.parse(journey.readFile("scheduling/review-paired/decisions.json")) as ReviewDecisions;
  expect(paired.decisions.map((decision) => [decision.action, decision.link, decision.from, decision.to])).toEqual([
    ["accept", "l000001", "", ""],
    ["reject", "l000006", "", ""],
    ["add", "", sent(3), received(2)],
  ]);
  // The earlier revision is unchanged, and the command line's report, which
  // no human review changes, is too.
  expect(journey.digest("scheduling/review-accepted/decisions.json")).toBe(acceptedDigest);
  expect((await correlated("scheduling.rules.json")).printed).toBe(printed);

  // The first history, reopened by name, holds its one decision; its analyst
  // and reason are shown only when asked for.
  await enter(user, review.getByLabelText("Previous review"), "review-accepted");
  await press(user, review.getByRole("button", { name: "Open mapping" }));
  expect(await review.findByText(`${LINKS.length} links · 1 original collisions · 1 decisions`)).toBeTruthy();
  expect(reviewedLinks(review)).toEqual([linkRow(LINKS[0]!, "accepted"), ...LINKS.slice(1).map((link) => linkRow(link, "unreviewed"))]);
  await press(user, review.getByLabelText("Reveal retained analyst and reason text"));
  expect(await review.findByText(`accept l000001 · ${ANALYST}: ${ACCEPTED}`)).toBeTruthy();

  // The colleague drops the patient rule from the rules on disk. The review
  // is refused as stale: the rules on screen are not the rules any more.
  const narrowed = RULES.replace(/,\n {4}\{\n {6}"id": "patient"[\s\S]*?\n {4}\}/, "");
  expect(narrowed).not.toBe(RULES);
  journey.changeFile("scheduling/scheduling.rules.json", narrowed);
  await press(user, review.getByRole("button", { name: "Open mapping" }));
  expect(await review.findByText(STALE_RULES)).toBeTruthy();
  expect(review.queryByRole("table")).toBeNull();
  const { report: changed } = await correlated("scheduling.rules.json");
  expect(changed.links).toHaveLength(4);

  // Laid out again under the rules as they are now, the history recorded
  // under the old ones is refused rather than applied to another finding.
  await layOut(user, panel, "scheduling.rules.json");
  expect(await panel.findByText("4 links · 1 collision · 0 not evaluated · readmit-correlation/v1")).toBeTruthy();
  review = reviewPanel(panel);
  await enter(user, review.getByLabelText("Previous review"), "review-paired");
  await press(user, review.getByRole("button", { name: "Open mapping" }));
  expect(await review.findByText(STALE_HISTORY)).toBeTruthy();
  expect(review.queryByRole("table")).toBeNull();
});

/** A colleague's sequence-analysis declaration over the incident: both
 * captures' windows, and one downstream expectation, of the fourth booking. */
function declaration(identity: string, rulesSHA256: string): string {
  return `${JSON.stringify(
    {
      schema: "readmit-sequence-analysis/v1",
      case_identity: identity,
      rules_sha256: rulesSHA256,
      clock_tolerance_seconds: 5,
      windows: [
        { source: "s0001", start: "2026-01-05T09:00:00Z", end: "2026-01-05T10:00:00Z", coverage: "partial" },
        { source: "s0002", start: "2026-01-05T09:00:00Z", end: "2026-01-05T10:00:00Z", coverage: "partial" },
      ],
      retries: [],
      downstream: [{ occurrence: sent(6), source: "s0002", rule: "same-booking" }],
    },
    null,
    2,
  )}\n`;
}

const UNKNOWN_OFFSET_REFUSED = "invalid sequence analysis document";

/** The downstream findings the sequence lists, one line each. */
function downstreamFindings(panel: ReturnType<typeof within>): string[] {
  const section = within(panel.getByRole("region", { name: "Sequence explanations" }));
  return section
    .getAllByRole("listitem")
    .map((item: HTMLElement) => item.textContent ?? "")
    .filter((line) => / downstream /.test(line))
    .map((line) => line.split(":")[0]!);
}

test("a retained sequence-analysis declaration is reopened into the editor with the identity of its bytes, extended and saved as a new entry the sequence applies, an open over unsaved work is cancelled with Escape, and a declaration the reader refuses is refused wherever it is read", async () => {
  const user = userEvent.setup();
  await journey.launch();
  await activateLicense(user, journey);
  const identity = await captureIncident();
  const { report } = await correlated("scheduling.rules.json");
  journey.writeFile("scheduling/colleague.analysis.json", declaration(identity, report.rules_sha256));
  journey.writeFile("scheduling/unknown-offset.analysis.json", declaration(identity, report.rules_sha256).replace('"2026-01-05T10:00:00Z"', '"2026-01-05T10:00:00-00:00"'));
  const colleagueDigest = journey.digest("scheduling/colleague.analysis.json");
  const panel = await openIncident(user);

  // Under the colleague's declaration the fourth booking, which the receiver
  // never took, has no linked output downstream.
  await layOut(user, panel, "scheduling.rules.json", "colleague.analysis.json");
  expect(await panel.findByText("Applied declaration: colleague.analysis.json; clock comparison tolerance 5 seconds.")).toBeTruthy();
  expect(downstreamFindings(panel)).toEqual([`${sent(6)} unobserved downstream output; source s0002`]);

  // A declaration with an unknown offset is refused, and nothing is laid out.
  await layOut(user, panel, "scheduling.rules.json", "unknown-offset.analysis.json");
  expect(await panel.findByText(UNKNOWN_OFFSET_REFUSED)).toBeTruthy();
  expect(panel.queryByRole("region", { name: "Sequence explanations" })).toBeNull();

  // The colleague's declaration opens into the editor: its windows and its
  // expectation, the canonical rules digest it pins, and the digest of the
  // file's own bytes.
  await user.click(panel.getByText("Rules and analysis"));
  const editor = within(panel.getByRole("region", { name: "Sequence analysis editor" }));
  const expectations = () => editor.queryAllByRole("button", { name: /^Remove downstream / }).map((button) => button.textContent);
  await user.selectOptions(await whenEnabled(editor.getByLabelText("Retained analysis document")), "colleague.analysis.json");
  await press(user, editor.getByRole("button", { name: "Open" }));
  expect(await editor.findByText(`Opened colleague.analysis.json · exact bytes hash to ${colleagueDigest}`)).toBeTruthy();
  expect(editor.queryAllByRole("button", { name: /^Remove window / }).map((button) => button.textContent)).toEqual(["Remove window s0001", "Remove window s0002"]);
  expect(expectations()).toEqual([`Remove downstream ${sent(6)}`]);
  expect((editor.getByLabelText("Correlation rules hash") as HTMLInputElement).value).toBe(report.rules_sha256);
  expect(editor.getByText(new RegExp(`binds to the verified case identity ${identity}\\.`))).toBeTruthy();

  // The person expects the first booking downstream as well, from the
  // keyboard.
  await enter(user, editor.getByLabelText("Upstream occurrence"), sent(1));
  await enter(user, editor.getByLabelText("Expected downstream source"), "s0002");
  await enter(user, editor.getByLabelText("Under rule"), "same-booking");
  await user.keyboard("{Enter}");
  expect(expectations()).toEqual([`Remove downstream ${sent(6)}`, `Remove downstream ${sent(1)}`]);

  // Opening the refused declaration now would replace that: asked, and
  // cancelled with Escape, nothing is read.
  const opens = journey.callsTo("OpenSequenceAnalysis").length;
  await user.selectOptions(editor.getByLabelText("Retained analysis document"), "unknown-offset.analysis.json");
  await press(user, editor.getByRole("button", { name: "Open" }));
  const question = within(editor.getByRole("group", { name: "Open unknown-offset.analysis.json in place of this declaration?" }));
  expect(document.activeElement).toBe(question.getByRole("button", { name: "Keep declaration" }));
  await user.keyboard("{Escape}");
  expect(editor.queryByRole("group", { name: /^Open / })).toBeNull();
  expect(document.activeElement).toBe(editor.getByRole("button", { name: "Open" }));
  expect(journey.callsTo("OpenSequenceAnalysis")).toHaveLength(opens);

  // Answered the other way, it is refused in the reader's sentence, and the
  // declaration on screen stays.
  await user.keyboard("{Enter}");
  await press(user, editor.getByRole("button", { name: "Replace declaration" }));
  expect(await editor.findByText(UNKNOWN_OFFSET_REFUSED)).toBeTruthy();
  expect(expectations()).toHaveLength(2);

  // Saved as a new entry beside the colleague's, which is unchanged.
  await enter(user, editor.getByLabelText("New sequence-analysis entry"), "extended.analysis.json");
  await press(user, editor.getByRole("button", { name: "Save as new" }));
  const saved = await editor.findByText(/^Saved to extended\.analysis\.json · exact bytes hash to /);
  expect(saved.textContent).toBe(`Saved to extended.analysis.json · exact bytes hash to ${journey.digest("scheduling/extended.analysis.json")}`);
  expect(journey.digest("scheduling/colleague.analysis.json")).toBe(colleagueDigest);

  // Laid out under it, the first booking has a linked copy downstream whose
  // capture recorded no time, so it is unresolved rather than observed; the
  // fourth is still unobserved.
  await layOut(user, panel, "scheduling.rules.json", "extended.analysis.json");
  expect(await panel.findByText("Applied declaration: extended.analysis.json; clock comparison tolerance 5 seconds.")).toBeTruthy();
  expect(downstreamFindings(panel)).toEqual([
    `${sent(6)} unobserved downstream output; source s0002`,
    `${sent(1)} downstream unresolved; source s0002`,
  ]);
});
