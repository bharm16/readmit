// Grouping findings across cases, reopening retained reports, previewing a
// review, keeping decisions in their own document and reopening a diagnose
// configuration, over the real facade.
//
// A clinic's scheduling interface misbehaved for a week. With the command line
// the person captured what the scheduler sent: on Monday and on Tuesday it
// rescheduled an appointment whose booking the capture never held, and on
// Wednesday it booked one and the receiver rejected the booking with an
// application error naming a missing required field. They diagnosed Monday and
// Wednesday and grouped the three days, and a colleague left them a diagnose
// configuration and the decisions they took over a narrower diagnosis of
// Wednesday.
//
// The window groups the three days exactly as `readmit diagnose groups` does:
// the two reschedules are one recurring shape of two findings, Wednesday's
// rejection two shapes of one. It reopens Wednesday's report under the identity
// of its bytes, refuses the grouping's report the listing names beside it in
// the words `readmit diagnose review` refuses it in, and shows Monday's report
// as another case's without opening its evidence in Wednesday's inspector. A
// review is previewed and writes nothing; the person's decisions are saved as
// their own document, which the command line reviews; the colleague's
// decisions, recorded against another report, are applied to nothing, and so
// are the person's own once the report they were about has changed on disk.
// The colleague's configuration opens into the editor with the identity of its
// bytes, is extended and saved as a new entry, and a diagnosis under it lists
// the rule this release does not define as not evaluated, byte for byte as the
// command line does; a configuration of a later contract version is refused in
// the command line's sentence, and opens that would replace unsaved work are
// asked first and can be cancelled.
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

/** A reschedule of an appointment whose booking the capture never held. */
function reschedule(appointment: number, sent: string, moved: string): string {
  return (
    `MSH|^~\\&|SCHEDULE|SYNTHETIC|RECEIVER|CLINIC|${sent}+0000||SIU^S13|MOVE-${appointment}|P|2.5.1\r` +
    `SCH|PLACER-${appointment}^READMIT|FILLER-${appointment}^READMIT||||CHECKUP|ROUTINE|NORMAL|30|min|^^^${moved}+0000\r` +
    `PID|1||SYNTH-${appointment}^^^READMIT||SYNTHETIC^ONLY\r`
  );
}

/** Wednesday's booking and the receiver's rejection of it, MLLP-framed. */
const WEDNESDAY =
  framed(
    "MSH|^~\\&|SCHEDULE|SYNTHETIC|RECEIVER|CLINIC|20260107090000+0000||SIU^S12|BOOK-303|P|2.5.1\r" +
      "SCH|PLACER-303^READMIT|FILLER-303^READMIT||||CHECKUP|ROUTINE|NORMAL|30|min|^^^20260108100000+0000\r" +
      "PID|1||SYNTH-303^^^READMIT||SYNTHETIC^ONLY\r",
  ) +
  framed(
    "MSH|^~\\&|RECEIVER|CLINIC|SCHEDULE|SYNTHETIC|20260107090100+0000||ACK|BOOK-303-ACK|P|2.5.1\r" +
      "MSA|AE|BOOK-303\r" +
      "ERR|||101^REQUIRED FIELD MISSING^HL70357|E\r",
  );

/** What each day's diagnosis under the bundled SIU configuration finds, rule
 * by rule: a reschedule with no captured booking is one hypothesis, and a
 * rejected booking is its acknowledgement's outcome and its error. */
const BOOKING_NOT_OBSERVED = "siu.booking-not-observed";
const ACK_OUTCOME = "ack.msa-outcome";
const ACK_ERROR = "ack.err-outcome";
const FINDINGS: Record<string, string[]> = {
  monday: [BOOKING_NOT_OBSERVED],
  tuesday: [BOOKING_NOT_OBSERVED],
  wednesday: [ACK_OUTCOME, ACK_ERROR],
};

const NOT_A_REPORT = "diagnosis report declares a contract version this release does not read";
const OTHER_EVIDENCE = "this diagnosis was run over different evidence; open the case the report names";
const OTHER_REPORT = "these decisions were recorded against a different diagnosis report; finding identifiers name other findings there";
const CHANGED = "the displayed diagnosis changed; reopen the report before reviewing";
const LATER_CONFIG = "unsupported diagnosis configuration schema";
const MISSPELLED_CONFIG = "invalid diagnosis configuration";

/** The colleague's configuration: the bundled SIU profile, the outcome rule,
 * and a rule a later release of theirs defines and this one does not. */
const COLLEAGUE_CONFIG = `${JSON.stringify(
  {
    schema: "readmit-diagnose-config/v1",
    profile: "readmit-siu-v1",
    ruleset: "readmit-siu-diagnosis/v1",
    rules: [ACK_OUTCOME, "siu.retired-rule"],
    namespaces: [{ key: "READMIT", namespace: "READMIT", universal_id: "", universal_id_type: "" }],
  },
  null,
  2,
)}\n`;

interface GroupsReport {
  cases: { case_identity: string }[];
  groups: { signature: string; rule_id: string; members: { case_identity: string; finding_id: string }[] }[];
}

interface ReviewRecord {
  decisions_sha256: string;
  findings: { finding: string; verdict: string; basis: string }[];
}

/** The license's operation policy: capturing is licensed work. */
function operationPolicy(): string {
  return journey.path("vendor-delivered-license", "operation-policy.json");
}

/** Runs the command line and expects it to succeed. */
async function commandLine(args: string[]): Promise<string> {
  const ran = await journey.commandLine(args);
  expect(ran.code, ran.stderr).toBe(0);
  return ran.stdout;
}

/** Runs the command line and expects the one refusal the window gave. */
async function refusedByCommandLine(args: string[], sentence: string): Promise<void> {
  const ran = await journey.commandLine(args);
  expect(ran.code).toBe(1);
  expect(ran.stdout).toBe("");
  expect(ran.stderr).toBe(`readmit: ${sentence}\n`);
}

/** Captures the three days into case bundles with the command line, as the
 * person did before opening the window. */
async function captureWeek(): Promise<void> {
  journey.writeFile("exports/monday.hl7", reschedule(301, "20260105090000", "20260107100000"));
  journey.writeFile("exports/tuesday.hl7", reschedule(302, "20260106090000", "20260108110000"));
  journey.writeFile("exports/wednesday.mllp", WEDNESDAY);
  journey.makeFolder("clinic");
  for (const [day, file] of [
    ["monday", "exports/monday.hl7"],
    ["tuesday", "exports/tuesday.hl7"],
    ["wednesday", "exports/wednesday.mllp"],
  ] as const) {
    await commandLine(["--operation-policy", operationPolicy(), "capture", file, "--output", `clinic/${day}`]);
  }
}

/** The identity a case bundle records for itself. */
function caseIdentity(day: string): string {
  return journey.readFile(`clinic/${day}/identity.sha256`).trim();
}

/** Opens the clinic folder and verifies Wednesday's case. */
async function openWednesday(user: UserEvent) {
  await journey.chooseFolder(journey.path("clinic"), "Open a readmit workspace folder");
  await press(user, screen.getByRole("button", { name: "Open a workspace folder…" }));
  const navigation = within(region("Project navigation"));
  const listed = (await navigation.findByText("wednesday", { selector: ".name" })).closest("li") as HTMLElement;
  await press(user, within(listed).getByRole("button", { name: "Verify and open" }));
  return within(await screen.findByRole("region", { name: "Diagnosis and finding review" }));
}

type Panel = Awaited<ReturnType<typeof openWednesday>>;

/** Opens one retained report from the panel's own picker and waits for the
 * facade's answer. */
async function openReport(user: UserEvent, panel: Panel, entry: string): Promise<void> {
  const picker = panel.getByLabelText("Retained diagnosis report");
  await within(picker).findByRole("option", { name: entry });
  await user.selectOptions(await whenEnabled(picker), entry);
  const asked = journey.callsTo("OpenDiagnosisReport").length;
  await press(user, panel.getByRole("button", { name: "Open this report" }));
  await waitFor(() => expect(journey.callsTo("OpenDiagnosisReport")[asked]?.settled).toBe(true));
}

/** Previews the review of the decisions on screen from the keyboard and waits
 * for the facade's answer. */
async function preview(user: UserEvent, panel: Panel): Promise<void> {
  const asked = journey.callsTo("ReviewFindings").length;
  panel.getAllByLabelText("Decision").at(-1)!.focus();
  await tabTo(user, await whenEnabled(panel.getByRole("button", { name: "Preview the review (writes nothing)" })));
  await user.keyboard("{Enter}");
  await waitFor(() => expect(journey.callsTo("ReviewFindings")[asked]?.settled).toBe(true));
}

/** Each finding the panel lists, as its identifier and rule. */
function listedFindings(panel: Panel): string[] {
  return panel
    .getAllByRole("region", { name: /^Findings / })
    .flatMap((group: HTMLElement) => {
      const rule = (group.getAttribute("aria-label") ?? "").replace(/^Findings /, "").split(" · ")[0];
      return Array.from(group.querySelectorAll(".findings > li > .occurrence")).map((id) => `${id.textContent} ${rule}`);
    });
}

test("findings recurring across captured cases are grouped exactly as readmit diagnose groups groups them, a retained report is reopened under the identity of its bytes, and a grouping's report or another case's report is never read as this case's diagnosis", async () => {
  const user = userEvent.setup();
  journey.provisionLicense("vendor-delivered-license");
  await captureWeek();
  await commandLine(["diagnose", "clinic/wednesday", "--output", "clinic/wednesday-diagnosis"]);
  await commandLine(["diagnose", "clinic/monday", "--output", "clinic/monday-diagnosis"]);
  await commandLine(["diagnose", "groups", "clinic/monday", "clinic/tuesday", "clinic/wednesday", "--output", "clinic/weekly-groups"]);
  const identities = { monday: caseIdentity("monday"), tuesday: caseIdentity("tuesday"), wednesday: caseIdentity("wednesday") };
  await journey.launch();
  const panel = await openWednesday(user);

  // Grouped under the bundled SIU configuration, the two reschedules are one
  // shape of two findings and Wednesday's rejection two shapes of one.
  await user.selectOptions(panel.getByLabelText("Configuration"), "builtin:siu");
  for (const day of ["monday", "tuesday", "wednesday"]) {
    await user.click(panel.getByRole("checkbox", { name: day }));
  }
  await press(user, panel.getByRole("button", { name: "Group findings across these cases" }));
  expect(await panel.findByText("Groups 1–3 of 3 across 3 cases")).toBeTruthy();
  // A group lists its members case by case in the order of the cases'
  // identities, as the grouping orders the cases themselves.
  const reschedules = [identities.monday, identities.tuesday].sort();
  const expected = [
    `${BOOKING_NOT_OBSERVED} 2: f000001 of ${reschedules[0]}, f000001 of ${reschedules[1]}`,
    `${ACK_OUTCOME} 1: f000001 of ${identities.wednesday}`,
    `${ACK_ERROR} 1: f000002 of ${identities.wednesday}`,
  ];
  const groups = within(panel.getByRole("region", { name: "Recurring finding groups" }));
  const drawn = groups.getAllByRole("listitem").map((item: HTMLElement) => {
    const [rule, signature, members] = Array.from(item.children).map((child) => child.textContent ?? "");
    return { rule, signature: signature!.replace(/^signature (\S+) · .*/, "$1"), line: `${rule} ${signature!.replace(/.* · (\d+) findings?.*/, "$1")}: ${members}` };
  });
  // The command line's grouping of the same days is the same, group by
  // group and member by member, in the order it lists them.
  const cli = JSON.parse(journey.readFile("clinic/weekly-groups/report.json")) as GroupsReport;
  const reported = cli.groups.map((group) => ({
    rule: group.rule_id,
    signature: group.signature,
    line: `${group.rule_id} ${group.members.length}: ${group.members.map((member) => `${member.finding_id} of ${member.case_identity}`).join(", ")}`,
  }));
  expect(drawn).toEqual(reported);
  expect([...drawn.map((group) => group.line)].sort()).toEqual([...expected].sort());
  expect(cli.cases.map((reportedCase) => reportedCase.case_identity).sort()).toEqual(Object.values(identities).sort());

  // Wednesday's retained report reopens under the identity of its own bytes,
  // with the two findings the rejection holds.
  await openReport(user, panel, "wednesday-diagnosis");
  const reportIdentity = journey.digest("clinic/wednesday-diagnosis/report.json");
  expect(await panel.findByText(new RegExp(`report identity ${reportIdentity}$`))).toBeTruthy();
  expect(panel.getByText("2 findings under readmit-siu-v1 · readmit-siu-diagnosis/v1")).toBeTruthy();
  expect(listedFindings(panel)).toEqual(FINDINGS.wednesday!.map((rule, index) => `f00000${index + 1} ${rule}`));
  expect(panel.queryByText(/was run over other evidence/)).toBeNull();
  // Its evidence opens in Wednesday's inspector: the rejection is the second
  // message captured.
  const inspected = journey.callsTo("InspectOccurrence").length;
  await press(user, panel.getAllByRole("button", { name: "s0001-e000002" })[0]!);
  await waitFor(() => expect(journey.callsTo("InspectOccurrence")).toHaveLength(inspected + 1));

  // The grouping's report is listed as a diagnosis beside the others, and it
  // is refused as one in the words `readmit diagnose review` refuses it in.
  journey.writeFile(
    "clinic/wednesday-decisions.json",
    JSON.stringify({
      schema: "readmit-finding-decisions/v1",
      report_sha256: reportIdentity,
      decisions: [{ finding: "f000001", verdict: "confirmed", rationale: "the receiver must keep rejecting this booking" }],
    }),
  );
  await openReport(user, panel, "weekly-groups");
  expect(await panel.findByText(NOT_A_REPORT)).toBeTruthy();
  expect(panel.queryByText(/report identity /)).toBeNull();
  await refusedByCommandLine(
    ["--operation-policy", operationPolicy(), "diagnose", "review", "clinic/weekly-groups", "--case", "clinic/wednesday", "--decisions", "clinic/wednesday-decisions.json", "--output", "weekly-review"],
    NOT_A_REPORT,
  );

  // Monday's report opens as what it is: run over Monday's evidence. None of
  // its evidence opens in Wednesday's inspector, and reviewing it over
  // Wednesday's case is refused by the window and the command line alike.
  await openReport(user, panel, "monday-diagnosis");
  expect(
    await panel.findByText(
      `This report was run over other evidence (case identity ${identities.monday}), not the case open here. Its evidence names that case's occurrences, so none of it opens in this case's inspector.`,
    ),
  ).toBeTruthy();
  expect(listedFindings(panel)).toEqual([`f000001 ${BOOKING_NOT_OBSERVED}`]);
  const evidence = panel.getAllByRole("button", { name: "s0001-e000001" });
  expect(evidence.every((button: HTMLElement) => (button as HTMLButtonElement).disabled)).toBe(true);
  await preview(user, panel);
  expect(await panel.findByText(OTHER_EVIDENCE)).toBeTruthy();
  journey.writeFile(
    "clinic/monday-decisions.json",
    JSON.stringify({
      schema: "readmit-finding-decisions/v1",
      report_sha256: journey.digest("clinic/monday-diagnosis/report.json"),
      decisions: [{ finding: "f000001", verdict: "dismissed", rationale: "the booking was made before the capture began" }],
    }),
  );
  await refusedByCommandLine(
    ["--operation-policy", operationPolicy(), "diagnose", "review", "clinic/monday-diagnosis", "--case", "clinic/wednesday", "--decisions", "clinic/monday-decisions.json", "--output", "monday-review"],
    OTHER_EVIDENCE,
  );
  expect(journey.callsTo("InspectOccurrence")).toHaveLength(inspected + 1);
});

test("a review is previewed without writing anything, decisions are saved as their own document the command line reviews, and decisions about another report or about a report changed on disk are applied to nothing", async () => {
  const user = userEvent.setup();
  await journey.launch();
  await activateLicense(user, journey);
  await captureWeek();
  // The colleague diagnosed Wednesday under the outcome rule alone and
  // dismissed what that narrower report found.
  journey.writeFile(
    "clinic/outcome-only.json",
    JSON.stringify({ schema: "readmit-diagnose-config/v1", profile: "readmit-siu-v1", ruleset: "readmit-siu-diagnosis/v1", rules: [ACK_OUTCOME], namespaces: [] }),
  );
  await commandLine(["diagnose", "clinic/wednesday", "--config", "clinic/outcome-only.json", "--output", "clinic/colleague-diagnosis"]);
  const colleagueReport = journey.digest("clinic/colleague-diagnosis/report.json");
  journey.writeFile(
    "clinic/colleague-decisions.json",
    JSON.stringify({
      schema: "readmit-finding-decisions/v1",
      report_sha256: colleagueReport,
      decisions: [{ finding: "f000001", verdict: "dismissed", rationale: "this test receiver rejects every booking" }],
    }),
  );
  const panel = await openWednesday(user);
  const verdict = (finding: string) => (panel.getByLabelText("Decision", { selector: `#diagnosis-verdict-${finding}` }) as HTMLSelectElement).value;

  // The person diagnoses Wednesday in the window and confirms the rejection.
  await user.selectOptions(panel.getByLabelText("Configuration"), "builtin:siu");
  await enter(user, panel.getByLabelText("New report directory in this workspace"), "wednesday-diagnosis");
  await press(user, panel.getByRole("button", { name: "Run this diagnosis" }));
  const reportIdentity = await waitFor(() => {
    const shown = panel.getByText(/report identity [0-9a-f]{64}$/).textContent ?? "";
    return shown.replace(/.*report identity /, "");
  });
  expect(reportIdentity).toBe(journey.digest("clinic/wednesday-diagnosis/report.json"));
  expect(listedFindings(panel)).toEqual(FINDINGS.wednesday!.map((rule, index) => `f00000${index + 1} ${rule}`));
  await user.selectOptions(panel.getByLabelText("Decision", { selector: "#diagnosis-verdict-f000001" }), "confirmed");
  await enter(user, panel.getByLabelText("Rationale"), "the receiver must keep rejecting this booking");

  // Previewed from the keyboard, the review joins the decision to this report
  // and writes nothing: no draft is offered from it.
  await preview(user, panel);
  const review = within(await panel.findByRole("region", { name: "Finding review" }));
  expect(review.getByText("The review, previewed (nothing written)")).toBeTruthy();
  expect(review.getByText(new RegExp(`^${ACK_OUTCOME} · observed_fact · Confirmed · by an explicit decision$`))).toBeTruthy();
  expect(review.getByText(new RegExp(`^${ACK_ERROR} · observed_fact · Not reviewed · nobody has decided about it$`))).toBeTruthy();
  expect(review.queryByRole("button", { name: /^Draft a test from / })).toBeNull();
  expect(journey.callsTo("DecideFindings")).toHaveLength(0);

  // Saved as their own document, the decisions are named by the digest of
  // the bytes written, and the command line reviews them over the report.
  const section = within(panel.getByRole("region", { name: "Finding decisions document" }));
  await enter(user, section.getByLabelText("New finding-decisions entry"), "my-decisions.json");
  await press(user, section.getByRole("button", { name: "Save these decisions as a new entry" }));
  const saved = await section.findByText(/^Saved to my-decisions\.json · exact bytes hash to /);
  expect(saved.textContent).toBe(`Saved to my-decisions.json · exact bytes hash to ${journey.digest("clinic/my-decisions.json")}`);
  await commandLine(["--operation-policy", operationPolicy(), "diagnose", "review", "clinic/wednesday-diagnosis", "--case", "clinic/wednesday", "--decisions", "clinic/my-decisions.json", "--output", "clinic/cli-review"]);
  const record = JSON.parse(journey.readFile("clinic/cli-review/review.json")) as ReviewRecord;
  expect(record.decisions_sha256).toBe(journey.digest("clinic/my-decisions.json"));
  expect(record.findings.map((finding) => `${finding.finding} ${finding.verdict} ${finding.basis}`)).toEqual([
    "f000001 confirmed decision",
    "f000002 not_reviewed unreviewed",
  ]);

  // A further decision is not saved: opening the colleague's document asks
  // first, and Escape keeps the decisions and reads nothing.
  await user.selectOptions(panel.getByLabelText("Decision", { selector: "#diagnosis-verdict-f000002" }), "suppressed");
  await user.selectOptions(panel.getByLabelText("Suppression scope"), "case");
  await enter(user, panel.getAllByLabelText("Rationale")[1]!, "the error text is the receiver's own wording");
  const picker = section.getByLabelText("Retained decisions document");
  await within(picker).findByRole("option", { name: "my-decisions.json" });
  await user.selectOptions(picker, "colleague-decisions.json");
  const opens = journey.callsTo("OpenFindingDecisions").length;
  await press(user, section.getByRole("button", { name: "Open these decisions" }));
  const question = within(section.getByRole("group", { name: "Open colleague-decisions.json in place of these decisions?" }));
  expect(document.activeElement).toBe(question.getByRole("button", { name: "Keep these decisions" }));
  await user.keyboard("{Escape}");
  expect(section.queryByRole("group", { name: /^Open / })).toBeNull();
  expect(document.activeElement).toBe(section.getByRole("button", { name: "Open these decisions" }));
  expect(journey.callsTo("OpenFindingDecisions")).toHaveLength(opens);
  expect([verdict("f000001"), verdict("f000002")]).toEqual(["confirmed", "suppressed"]);

  // Asked again and answered, the colleague's decisions are read and, being
  // about their narrower report, applied to nothing — as the command line
  // refuses them over this report.
  await user.keyboard("{Enter}");
  await press(user, section.getByRole("button", { name: "Replace them with colleague-decisions.json" }));
  expect(
    await section.findByText(
      `The decisions in colleague-decisions.json were recorded against a different diagnosis report (${colleagueReport}); finding identifiers name other findings there, so none of them is applied to this report.`,
    ),
  ).toBeTruthy();
  expect([verdict("f000001"), verdict("f000002")]).toEqual(["confirmed", "suppressed"]);
  await refusedByCommandLine(
    ["--operation-policy", operationPolicy(), "diagnose", "review", "clinic/wednesday-diagnosis", "--case", "clinic/wednesday", "--decisions", "clinic/colleague-decisions.json", "--output", "colleague-review"],
    OTHER_REPORT,
  );

  // The person's own document puts back what they saved.
  await user.selectOptions(picker, "my-decisions.json");
  await press(user, section.getByRole("button", { name: "Open these decisions" }));
  await press(user, section.getByRole("button", { name: "Replace them with my-decisions.json" }));
  expect(await section.findByText(`Opened my-decisions.json · exact bytes hash to ${journey.digest("clinic/my-decisions.json")}`)).toBeTruthy();
  expect([verdict("f000001"), verdict("f000002")]).toEqual(["confirmed", ""]);

  // The report is rewritten on disk after the window showed it: the same
  // findings in other bytes. A preview of decisions typed against what was
  // shown is refused, and the command line refuses the saved decisions over
  // it, since they name the report as it was.
  const reformatted = `${JSON.stringify(JSON.parse(journey.readFile("clinic/wednesday-diagnosis/report.json")), null, 1)}\n`;
  journey.changeFile("clinic/wednesday-diagnosis/report.json", reformatted);
  await preview(user, panel);
  expect(await panel.findByText(CHANGED)).toBeTruthy();
  expect(panel.queryByRole("region", { name: "Finding review" })).toBeNull();
  await refusedByCommandLine(
    ["--operation-policy", operationPolicy(), "diagnose", "review", "clinic/wednesday-diagnosis", "--case", "clinic/wednesday", "--decisions", "clinic/my-decisions.json", "--output", "stale-review"],
    OTHER_REPORT,
  );

  // Reopened, the report is named by its new bytes, the decisions typed about
  // the old ones are gone, and the saved document is applied to nothing.
  await openReport(user, panel, "wednesday-diagnosis");
  const rewritten = journey.digest("clinic/wednesday-diagnosis/report.json");
  expect(rewritten).not.toBe(reportIdentity);
  expect(await panel.findByText(new RegExp(`report identity ${rewritten}$`))).toBeTruthy();
  expect([verdict("f000001"), verdict("f000002")]).toEqual(["", ""]);
  const reopened = within(panel.getByRole("region", { name: "Finding decisions document" }));
  await user.selectOptions(reopened.getByLabelText("Retained decisions document"), "my-decisions.json");
  await press(user, reopened.getByRole("button", { name: "Open these decisions" }));
  expect(
    await reopened.findByText(
      `The decisions in my-decisions.json were recorded against a different diagnosis report (${reportIdentity}); finding identifiers name other findings there, so none of them is applied to this report.`,
    ),
  ).toBeTruthy();
  expect([verdict("f000001"), verdict("f000002")]).toEqual(["", ""]);
});

test("a retained diagnose configuration opens into the editor under the identity of its bytes, is extended and saved as a new entry, a diagnosis under it reports the rule this release lacks exactly as readmit diagnose does, and configurations the reader cannot read are refused as the command line refuses them", async () => {
  const user = userEvent.setup();
  await journey.launch();
  await activateLicense(user, journey);
  await captureWeek();
  journey.writeFile("clinic/colleague-config.json", COLLEAGUE_CONFIG);
  journey.writeFile(
    "clinic/later-config.json",
    JSON.stringify({ schema: "readmit-diagnose-config/v2", profile: "readmit-siu-v1", ruleset: "readmit-siu-diagnosis/v1", rules: [ACK_OUTCOME], namespaces: [] }),
  );
  // A hand-edited copy names its rules under a misspelled member.
  journey.writeFile(
    "clinic/misspelled-config.json",
    JSON.stringify({ schema: "readmit-diagnose-config/v1", profile: "readmit-siu-v1", ruleset: "readmit-siu-diagnosis/v1", rule: [ACK_OUTCOME], namespaces: [] }),
  );
  const colleagueDigest = journey.digest("clinic/colleague-config.json");
  const panel = await openWednesday(user);
  await user.click(panel.getByText("Author a diagnose configuration"));
  const editor = within(panel.getByRole("region", { name: "Diagnose configuration editor" }));
  const namespaces = () => editor.queryAllByRole("button", { name: /^Remove namespace / }).map((button) => button.textContent);

  // A configuration of a later contract version is listed in the folder and
  // offered nowhere a configuration is read, and the command line refuses it.
  expect(within(region("Project navigation")).getByText("later-config.json", { selector: ".name" })).toBeTruthy();
  const offered = (picker: HTMLElement) => Array.from((picker as HTMLSelectElement).options).map((option) => option.textContent);
  expect(offered(editor.getByLabelText("Retained configuration document"))).toEqual([
    "Choose an entry of this workspace…",
    "colleague-config.json",
    "misspelled-config.json",
  ]);
  expect(offered(panel.getByLabelText("Configuration"))).not.toContain("later-config.json");
  await refusedByCommandLine(["diagnose", "clinic/wednesday", "--config", "clinic/later-config.json", "--output", "later-diagnosis"], LATER_CONFIG);

  // The colleague's configuration opens into the controls, named by its file
  // and the digest of its bytes.
  await user.selectOptions(editor.getByLabelText("Retained configuration document"), "colleague-config.json");
  await press(user, editor.getByRole("button", { name: "Open this document" }));
  expect(await editor.findByText(`Opened colleague-config.json · exact bytes hash to ${colleagueDigest}`)).toBeTruthy();
  expect((editor.getByLabelText("Bundled profile and ruleset") as HTMLSelectElement).value).toBe("readmit-siu-v1");
  expect((editor.getByLabelText("Rule identifiers, separated by spaces") as HTMLInputElement).value).toBe(`${ACK_OUTCOME} siu.retired-rule`);
  expect(namespaces()).toEqual(["Remove namespace READMIT"]);

  // The person adds the clinic's own authority, from the keyboard.
  await enter(user, editor.getByLabelText("Namespace key"), "CLINIC");
  await enter(user, editor.getByLabelText("Namespace"), "CLINIC");
  await user.keyboard("{Enter}");
  expect(namespaces()).toEqual(["Remove namespace READMIT", "Remove namespace CLINIC"]);

  // Opening the misspelled copy now would replace that unsaved change: the
  // editor asks, and Escape keeps it and reads nothing.
  const opens = journey.callsTo("OpenDiagnoseConfig").length;
  await user.selectOptions(editor.getByLabelText("Retained configuration document"), "misspelled-config.json");
  await press(user, editor.getByRole("button", { name: "Open this document" }));
  const question = within(editor.getByRole("group", { name: "Open misspelled-config.json in place of this configuration?" }));
  expect(document.activeElement).toBe(question.getByRole("button", { name: "Keep this configuration" }));
  await user.keyboard("{Escape}");
  expect(editor.queryByRole("group", { name: /^Open / })).toBeNull();
  expect(document.activeElement).toBe(editor.getByRole("button", { name: "Open this document" }));
  expect(journey.callsTo("OpenDiagnoseConfig")).toHaveLength(opens);
  expect(namespaces()).toHaveLength(2);

  // Asked again and answered, the misspelled copy is refused in the sentence
  // the command line refuses it in, and the controls stay.
  await user.keyboard("{Enter}");
  await press(user, editor.getByRole("button", { name: "Replace it with misspelled-config.json" }));
  expect(await editor.findByText(MISSPELLED_CONFIG)).toBeTruthy();
  expect(namespaces()).toHaveLength(2);
  await refusedByCommandLine(["diagnose", "clinic/wednesday", "--config", "clinic/misspelled-config.json", "--output", "misspelled-diagnosis"], MISSPELLED_CONFIG);

  // Saved beside the colleague's, which is unchanged, the extended
  // configuration is named by the digest of what was written.
  await enter(user, editor.getByLabelText("New diagnose-config entry"), "extended-config.json");
  await press(user, editor.getByRole("button", { name: "Save as a new entry" }));
  const saved = await editor.findByText(/^Saved to extended-config\.json · exact bytes hash to /);
  expect(saved.textContent).toBe(`Saved to extended-config.json · exact bytes hash to ${journey.digest("clinic/extended-config.json")}`);
  expect(journey.digest("clinic/colleague-config.json")).toBe(colleagueDigest);
  const extended = JSON.parse(journey.readFile("clinic/extended-config.json")) as { rules: string[]; namespaces: { key: string }[] };
  expect(extended.rules).toEqual([ACK_OUTCOME, "siu.retired-rule"]);
  expect(extended.namespaces.map((declared) => declared.key)).toEqual(["READMIT", "CLINIC"]);

  // A diagnosis under the colleague's configuration evaluates the outcome
  // rule and lists the rule this release lacks as not evaluated, byte for
  // byte as the command line writes it; so does one under the extension.
  for (const [config, output] of [
    ["colleague-config.json", "colleague-run"],
    ["extended-config.json", "extended-run"],
  ] as const) {
    const choice = panel.getByLabelText("Configuration");
    await within(choice).findByRole("option", { name: config });
    await user.selectOptions(await whenEnabled(choice), `config:${config}`);
    await enter(user, panel.getByLabelText("New report directory in this workspace"), output);
    const runs = journey.callsTo("RunDiagnosis").length;
    await press(user, panel.getByRole("button", { name: "Run this diagnosis" }));
    await waitFor(() => expect(journey.callsTo("RunDiagnosis")[runs]?.settled).toBe(true));
    expect(await panel.findByText(new RegExp(`report identity ${journey.digest(`clinic/${output}/report.json`)}$`))).toBeTruthy();
    expect(panel.getByText("1 finding under readmit-siu-v1 · readmit-siu-diagnosis/v1")).toBeTruthy();
    expect(listedFindings(panel)).toEqual([`f000001 ${ACK_OUTCOME}`]);
    expect(panel.getByText("unsupported_rule · A configured rule is unsupported: siu.retired-rule")).toBeTruthy();
    await commandLine(["diagnose", "clinic/wednesday", "--config", `clinic/${config}`, "--output", `cli-${output}`]);
    expect(journey.readFile(`clinic/${output}/report.json`)).toBe(journey.readFile(`cli-${output}/report.json`));
  }
});
