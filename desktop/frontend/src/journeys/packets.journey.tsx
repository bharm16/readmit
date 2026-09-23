// What an investigation hands on. The runs a person made — the failing one on
// the downstream system's defect and the passing one once it was fixed — are
// assembled into a sealed packet only after a preview names every input and
// states its limitations; the packet is verified read-only, exported with its
// offline renderings as a portable review that reopens read-only with its
// report text revealed only on purpose, and summarized into a value-free
// support bundle published only under the exact identity its preview showed.
// A planted example goes through privacy review: a policy that leaves
// findings unresolved blocks the review, and the handled policy's review is
// exported only under its exact identity. The command line verifies every
// artifact the window wrote.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { UserEvent } from "@testing-library/user-event";
import { byContent, enter, Journey, press, region } from "../testkit/journey";
import { activateLicense, createProject, runOnce, savedAckTest } from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

const PROJECT = "investigations/scheduling-investigation";

/** The packets panel of the open workspace. */
function packets() {
  return within(screen.getByRole("region", { name: "Investigation packets" }));
}

/** The privacy review, export and support panel of the open workspace. */
function privacy() {
  return within(within(region("Privacy status")).getByRole("region", { name: "Privacy review and protected export" }));
}

/** The text of the one line a pattern matches, once the window shows it. */
async function line(scope: ReturnType<typeof within>, pattern: RegExp): Promise<string> {
  return (await scope.findByText(byContent(pattern))).textContent ?? "";
}

/** Two retained runs of the saved test: one on the downstream's defect, one
 * once it was fixed. */
async function failedThenFixed(user: UserEvent) {
  const { downstream } = await savedAckTest(user, journey, "defective");
  expect(await runOnce(user, downstream, "run-defective")).toBe("assertion_failed");
  downstream.setMode("fixed");
  expect(await runOnce(user, downstream, "run-fixed")).toBe("passed");
  return downstream;
}

test("the failing and fixed runs become a sealed packet, a portable review that reopens read-only, and a reviewed support summary", async () => {
  const user = userEvent.setup();
  const downstream = await failedThenFixed(user);
  const sent = downstream.received().length;

  // The preview names every input before anything is written: the case, the
  // exact specification the runs kept, the fixed run as current and the
  // failing one as the observed baseline, and a fresh destination.
  let panel = packets();
  // The folder is read again once a run lands; its runs are offered then.
  await panel.findAllByRole("option", { name: "run-fixed" });
  await user.selectOptions(panel.getByLabelText("Case"), "reschedule-feed");
  await user.selectOptions(panel.getByLabelText("Historical specification"), "reschedule-ack-test.json");
  await user.selectOptions(panel.getByLabelText("Current result"), "run-fixed");
  await user.selectOptions(panel.getByLabelText("Baseline (optional)"), "run-defective");
  await press(user, panel.getByRole("button", { name: "Preview assembly" }));
  expect(await line(panel, /^Destination: packet-001 \(generated\) · fresh$/)).toBeTruthy();
  expect(await line(panel, /^Case: reschedule-feed — /)).toBe("Case: reschedule-feed — found · provenance imported · matches the retained case");
  expect(await line(panel, /^Historical specification: /)).toBe(
    "Historical specification: reschedule-ack-test.json — found · matches the retained specification",
  );
  expect(await line(panel, /^Current result: /)).toBe("Current result: run-fixed — found · pass · passed · boundary ack-contract");
  expect(await line(panel, /^Baseline: run-defective — /)).toBe(
    "Baseline: run-defective — found · assertion_failure · assertion_failed · boundary ack-contract",
  );
  expect(await line(panel, /^Sensitivity: /)).toBe("Sensitivity: contains original source values · export policy customer-local-only");
  expect(
    panel.getByText(
      "Hashes establish integrity, not source authenticity, disclosure approval or a regression-equivalence claim.",
    ),
  ).toBeTruthy();
  await press(user, panel.getByRole("button", { name: "Assemble packet" }));
  const sealed = await line(panel, /^Packet packet-001 sealed: identity [0-9a-f]{12}… · registered in the workspace navigation\.$/);
  expect(sealed).toBeTruthy();

  // Verified read-only, as the command line verifies it.
  panel = packets();
  await panel.findByRole("option", { name: "packet-001" });
  await user.selectOptions(panel.getByLabelText("Packets of this workspace"), "packet-001");
  await press(user, panel.getByRole("button", { name: "Verify read-only" }));
  const packetIdentity = sealed.replace(/^.*identity ([0-9a-f]{12})….*$/, "$1");
  expect(await line(panel, /^Verified: identity /)).toMatch(
    new RegExp(`^Verified: identity ${packetIdentity}… · contract readmit-retained-packet/v1 · state complete · \\d+ indexed files\\.$`),
  );
  expect(await line(panel, /^Current: /)).toBe("Current: pass · boundary ack-contract · provenance imported · run passed");
  expect(await line(panel, /^Baseline: assertion_failure/)).toBe("Baseline: assertion_failure · boundary ack-contract · run assertion_failed");
  const verified = await journey.commandLine(["report", "verify-retained", `${PROJECT}/packet-001`]);
  expect(verified.code).toBe(0);
  expect(verified.stdout).toMatch(new RegExp(`^Retained packet verified: ${packetIdentity}[0-9a-f]{52}\n`));
  const fullPacketIdentity = verified.stdout.replace(/^Retained packet verified: ([0-9a-f]{64})\n[\s\S]*$/, "$1");

  // The portable review: exported into a new folder chosen natively, with its
  // five offline renderings, and reopened read-only with its report text
  // hidden until it is revealed on purpose.
  await journey.chooseFolder(journey.path(PROJECT, "reschedule-review"), "Choose a new folder for the portable review");
  await press(user, panel.getByRole("button", { name: "Choose destination…" }));
  expect(await panel.findByText(journey.path(PROJECT, "reschedule-review"))).toBeTruthy();
  await press(user, panel.getByRole("button", { name: "Export portable review" }));
  const exported = await line(panel, /^Review reschedule-review sealed: /);
  expect(exported).toMatch(new RegExp(`^Review reschedule-review sealed: identity [0-9a-f]{12}… · packet ${packetIdentity}…$`));
  expect(await line(panel, /^Renderings: /)).toBe(
    "Renderings: offline HTML, PDF, Markdown, strict JSON, JUnit · sensitivity retained: contains original source values · policy customer-local-only",
  );
  await panel.findByRole("option", { name: "reschedule-review" });
  await user.selectOptions(panel.getByLabelText("Reviews of this workspace"), "reschedule-review");
  await press(user, panel.getByRole("button", { name: "Open read-only" }));
  expect(await line(panel, /^Runs: current /)).toBe(
    "Runs: current pass · baseline assertion_failure. Statuses are the retained evidence's own labels, never a passing run or an approved disclosure.",
  );
  expect(await line(panel, /^Renderings present: /)).toBe("Renderings present: junit.xml, report.html, report.json, report.md, report.pdf.");
  expect(panel.getByText(/^Report text hidden\./)).toBeTruthy();
  await press(user, panel.getByRole("button", { name: "Reveal report text" }));
  const revealed = (await panel.findByText(/./, { selector: "pre.report-lines" })).textContent ?? "";
  expect(revealed).toContain(`Packet identity: ${fullPacketIdentity}`);
  // The command line reads the same review offline, to the same report.
  const reviewed = await journey.commandLine(["report", "review", `${PROJECT}/reschedule-review`, "--format", "json"]);
  expect(reviewed.code).toBe(0);
  const report = JSON.parse(reviewed.stdout) as { schema: string; packet_identity: string; lines: string[] };
  expect(report.schema).toBe("readmit-portable-report/v1");
  expect(report.packet_identity).toBe(fullPacketIdentity);
  expect(report.lines.join("\n")).toBe(revealed);

  // A value-free support summary of the portable review, published only under
  // the exact identity its preview showed: a sharing policy authored through
  // the panel's own controls, a preview, a refused approval that names some
  // other identity, and the exact one.
  const support = privacy();
  await enter(user, support.getByLabelText("New policy document"), "sharing.json");
  await press(user, support.getByRole("button", { name: "Save sharing policy" }));
  expect(await line(support, /^Saved sharing\.json: /)).toBe("Saved sharing.json: support allowed, local-file, 4096 bytes.");
  await support.findByRole("option", { name: "sharing.json" });
  await user.selectOptions(support.getByLabelText("Sharing policy"), "sharing.json");
  expect(await line(support, /^Support allowed · /)).toBe("Support allowed · local-file · 4096 bytes.");
  await user.selectOptions(support.getByLabelText("Source to summarize"), "reschedule-review");
  await press(user, support.getByRole("button", { name: "Preview summary" }));
  const identity = (await line(support, /^Preview identity: [0-9a-f]{64}$/)).replace("Preview identity: ", "");
  const other = identity.slice(0, -1) + (identity.endsWith("0") ? "1" : "0");
  await enter(user, support.getByLabelText("Approve by naming the exact preview identity"), other);
  await press(user, support.getByRole("button", { name: "Publish support bundle" }));
  expect(
    await support.findByText(
      "this approval does not name the summary the current sources and policy produce; review the current preview again and approve the identity it displays",
    ),
  ).toBeTruthy();
  expect(journey.callsTo("PublishSupportSummary").at(-1)?.result).toMatchObject({ state: "failed" });
  await enter(user, support.getByLabelText("Approve by naming the exact preview identity"), identity);
  await journey.chooseFolder(journey.path(PROJECT, "support-for-vendor"), "Choose a new folder for the reviewed support bundle");
  await press(user, support.getByRole("button", { name: "Choose destination…" }));
  await waitFor(() =>
    expect((support.getByLabelText("New support folder") as HTMLInputElement).value).toBe(journey.path(PROJECT, "support-for-vendor")),
  );
  await press(user, support.getByRole("button", { name: "Publish support bundle" }));
  expect(await line(support, /^Bundle /)).toBe("Bundle support-for-vendor: support.json, event.json, identity.sha256.");
  await support.findByRole("option", { name: "support-for-vendor" });
  await user.selectOptions(support.getByLabelText("Verify a support bundle"), "support-for-vendor");
  const bundle = (await line(support, /^Verified bundle identity: /)).replace(/^Verified bundle identity: ([0-9a-f]{64}) — .*$/, "$1");
  const shared = await journey.commandLine(["share", "verify", `${PROJECT}/support-for-vendor`]);
  expect(shared.code).toBe(0);
  const summary = JSON.parse(shared.stdout.split("\n")[0]!) as Record<string, string>;
  expect(summary).toMatchObject({
    schema: "readmit-support-summary/v1",
    source_kind: "portable-review",
    outcome: "pass",
    external_equivalence: "declined",
  });
  expect(shared.stdout).toContain(`Verified support identity: ${bundle}`);
  // The summary carries no value from the evidence.
  for (const value of ["PLACER-101", "SYNTH-101", "SYNTHETIC", downstream.address]) {
    expect(shared.stdout).not.toContain(value);
    expect(journey.readFile(`${PROJECT}/support-for-vendor/support.json`)).not.toContain(value);
  }

  // Assembling, exporting, reviewing and sharing sent nothing.
  expect(downstream.received()).toHaveLength(sent);
});

/** The shipped planted example, byte for byte: every value in it is invented.
 * The two captures are the person's own evidence; the specification, the two
 * disclosure policies and the inventory are the documents the privacy panel
 * selects but does not author. */
function placePlantedExample(project: string) {
  journey.placeFixture("redact-booking.mllp", "captures/redact-booking.mllp");
  journey.placeFixture("redact-reschedule.mllp", "captures/redact-reschedule.mllp");
  journey.placeFixture("redact-spec.json", `${project}/spec.json`);
  journey.placeFixture("redact-policy.json", `${project}/policy.json`);
  journey.placeFixture("redact-policy-blocked.json", `${project}/blocked-policy.json`);
  journey.placeFixture("redact-inventory.json", `${project}/inventory.json`);
}

/** Imports the two MLLP captures into the open project as one registered
 * case, with the framing the capture used. */
async function importCaptures(user: UserEvent, caseName: string) {
  const evidence = within(region("Evidence"));
  await press(user, evidence.getByRole("button", { name: "Import evidence into this project…" }));
  await journey.chooseFiles([journey.path("captures/redact-booking.mllp"), journey.path("captures/redact-reschedule.mllp")], "Choose evidence files to import");
  await press(user, await screen.findByRole("button", { name: "Select Files…" }));
  await within(screen.getByRole("region", { name: "Declared sources" })).findByText(/redact-reschedule\.mllp/);
  await user.selectOptions(screen.getByLabelText("Framing"), "mllp");
  await press(user, screen.getByRole("button", { name: "Preview extraction" }));
  const commit = within(screen.getByRole("region", { name: "Commit import" }));
  await enter(user, commit.getByLabelText("Case bundle folder name"), caseName);
  await enter(user, commit.getByLabelText("Receipt file name"), `${caseName}-receipt.json`);
  await enter(user, commit.getByLabelText("Case title"), "Planted example");
  await press(user, commit.getByRole("button", { name: "Commit import" }));
  expect(await commit.findByText("Import Completed Successfully")).toBeTruthy();
  await press(user, commit.getByRole("button", { name: "Open this case in inspector" }));
  expect(await within(region("Inspector")).findByText(caseName, { selector: "dd" })).toBeTruthy();
}

/** Derives a disclosure review of the case under one policy and returns what
 * the panel says about it. */
async function derive(user: UserEvent, policy: string) {
  const panel = privacy();
  await user.selectOptions(panel.getByLabelText("Case"), "original.case");
  await user.selectOptions(panel.getByLabelText("Original specification"), "spec.json");
  await user.selectOptions(panel.getByLabelText("Disclosure policy"), policy);
  await user.selectOptions(panel.getByLabelText("Original-artifact inventory"), "inventory.json");
  await press(user, panel.getByRole("button", { name: "Derive review" }));
  const review = await line(panel, /^Review review-\d+ · /);
  const identity = (await line(panel, /^Identity an approval must name: /)).replace("Identity an approval must name: ", "");
  return { review, identity };
}

test("a planted example's privacy review is blocked while its policy leaves findings, and exported only under the exact identity once handled", async () => {
  const user = userEvent.setup();
  await journey.launch();
  await activateLicense(user, journey);
  await createProject(user, journey, "reviews", "planted-review", "Planted disclosure review");
  const project = "reviews/planted-review";
  placePlantedExample(project);
  await importCaptures(user, "original.case");

  const blocked = await derive(user, "blocked-policy.json");
  expect(blocked.review).toMatch(/^Review review-001 · blocked · (\d+) findings \(\1 unresolved\)\.$/);
  const handled = await derive(user, "policy.json");
  expect(handled.review).toMatch(/^Review review-002 · ready-for-approval · \d+ findings \(0 unresolved\)\.$/);
  expect(await line(privacy(), /^This review establishes: /)).toBe("This review establishes: disclosure-reviewed-extract.");

  // The command line derives the same reviews from the same documents: the
  // same states and the same located findings, each under its own identity,
  // since every derivation draws its own surrogates.
  const cli = async (policy: string, output: string) => {
    const derived = await journey.commandLine([
      "--operation-policy",
      journey.path("vendor-delivered-license", "operation-policy.json"),
      "redact",
      `${project}/original.case`,
      "--spec",
      `${project}/spec.json`,
      "--policy",
      `${project}/${policy}`,
      "--inventory",
      `${project}/inventory.json`,
      "--local-state",
      `${project}/${output}-private`,
      "--output",
      `${project}/${output}`,
    ]);
    const review = JSON.parse(journey.readFile(`${project}/${output}/review.json`)) as Record<string, unknown>;
    return { code: derived.code, review };
  };
  const located = (review: Record<string, unknown>) => ({ state: review.state, findings: review.findings });
  const derivedInWindow = (entry: string) => JSON.parse(journey.readFile(`${project}/${entry}/review.json`)) as Record<string, unknown>;
  const cliBlocked = await cli("blocked-policy.json", "cli-blocked-review");
  expect(cliBlocked.code).toBe(2);
  expect(located(cliBlocked.review)).toEqual(located(derivedInWindow("review-001")));
  const cliHandled = await cli("policy.json", "cli-review");
  expect(cliHandled.code).toBe(0);
  expect(located(cliHandled.review)).toEqual(located(derivedInWindow("review-002")));

  // A blocked review is never exported, even under its exact identity.
  const exporting = privacy();
  await user.selectOptions(exporting.getByLabelText("Review to export"), "review-001");
  expect(await line(exporting, /^review-001: blocked · /)).toMatch(/^review-001: blocked · decision \S+ · \d+ findings \(\d+ unresolved\)\.$/);
  expect(exporting.getByText("Unresolved surfaces — each is an explicit blocker:")).toBeTruthy();
  await enter(user, exporting.getByLabelText("Its private local state"), "review-private-001");
  await enter(user, exporting.getByLabelText("Approve by naming the exact review identity shown in the inventory"), blocked.identity);
  await press(user, exporting.getByRole("button", { name: "Export packet" }));
  const refusal = "export requires approval of an exact fully handled and proven review";
  expect(await exporting.findByText(refusal)).toBeTruthy();

  // The ready review is exported only under its exact identity.
  await user.selectOptions(exporting.getByLabelText("Review to export"), "review-002");
  await enter(user, exporting.getByLabelText("Its private local state"), "review-private-002");
  await enter(user, exporting.getByLabelText("Approve by naming the exact review identity shown in the inventory"), blocked.identity);
  const asked = journey.callsTo("ExportDerivedPacket").length;
  await press(user, exporting.getByRole("button", { name: "Export packet" }));
  await waitFor(() => expect(journey.callsTo("ExportDerivedPacket")[asked]?.result).toEqual({ state: "failed", reason: refusal }));
  await enter(user, exporting.getByLabelText("Approve by naming the exact review identity shown in the inventory"), handled.identity);
  await press(user, exporting.getByRole("button", { name: "Export packet" }));
  expect(await line(exporting, /^Packet export-001 generated: /)).toMatch(/^Packet export-001 generated: \d+ files, identity [0-9a-f]{12}…$/);
  // Exporting reran the derived test against fresh built-in fixtures: it
  // fails on the defective one and passes on the fixed one, as the original
  // did, and claims nothing about any other system.
  expect(await line(exporting, /^Proof: /)).toBe(
    "Proof: baseline assertion_failure, postfix pass · establishes disclosure-reviewed-extract · external equivalence declined.",
  );
  expect(journey.callsTo("ExportDerivedPacket").filter((call) => (call.result as { state: string }).state === "completed")).toHaveLength(1);

  // What leaves in the packet carries none of the planted values.
  const planted = (JSON.parse(journey.readFile(`${project}/inventory.json`)) as { residual_values: string[] }).residual_values;
  for (const file of ["export-review.json", "spec.json"]) {
    const text = journey.readFile(`${project}/export-001/${file}`);
    for (const value of planted) expect(text).not.toContain(value);
  }
});
