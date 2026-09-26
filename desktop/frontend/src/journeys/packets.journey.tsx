// What an investigation hands on. The runs a person made — the failing one on
// the downstream system's defect and the passing one once it was fixed — are
// assembled into a sealed packet only after a preview names every input and
// states its limitations; the packet is verified read-only, exported with its
// offline renderings as a portable review that reopens read-only with its
// report text revealed only on purpose, and summarized into a value-free
// support bundle published only under the exact identity its preview showed,
// into a new folder named natively: a policy its decoder refuses, a dismissed
// dialog and a folder that already exists publish nothing, and a bundle changed
// after publication is refused when verified again, exactly as `readmit share`
// and `readmit share verify` refuse them.
// A planted example goes through privacy review: a policy that leaves
// findings unresolved blocks the review, and the handled policy's review is
// exported only under its exact identity. The command line verifies every
// artifact the window wrote. An approved review is then reexecuted against
// the target its actual original failing run recorded — a ledger receiver the
// journey starts on loopback — sending only after a preview and an explicit
// authorization, refused under a wrong identity exactly as `readmit redact
// reexecute` refuses it, and retaining a job the command line recovers.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { UserEvent } from "@testing-library/user-event";
import { byContent, enter, Journey, press, region } from "../testkit/journey";
import { freeLoopbackAddress } from "./probes.js";
import { activateLicense, createProject, runOnce, runs, savedAckTest, tabTo } from "./steps";
import type { RedactInventory, RedactPolicy } from "../bindings";

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
  return within(within(region("Privacy")).getByRole("region", { name: "Privacy review" }));
}

/** The portable-review export block of a verified packet, distinct from the
 * synthetic sample packets' own destination choices below it. */
function portableReview() {
  return within(packets().getByRole("heading", { name: "Portable review" }).parentElement as HTMLElement);
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
  await press(user, panel.getByRole("button", { name: "Preview" }));
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
  await press(user, panel.getByRole("button", { name: "Create packet" }));
  const sealed = await line(panel, /^Packet packet-001 sealed: identity [0-9a-f]{12}… · registered in the workspace navigation\.$/);
  expect(sealed).toBeTruthy();

  // Verified read-only, as the command line verifies it.
  panel = packets();
  await panel.findByRole("option", { name: "packet-001" });
  await user.selectOptions(panel.getByLabelText("Packets"), "packet-001");
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

  // The portable review: exported into a new folder named in the host's save
  // dialog, with its five offline renderings, and reopened read-only with its
  // report text hidden until it is revealed on purpose.
  // Dismissing the save dialog names nothing, and nothing can be exported.
  await journey.dismissDialog("save", "Choose a new folder for the portable review");
  await press(user, portableReview().getByRole("button", { name: "Choose destination…" }));
  await waitFor(() => expect(journey.callsTo("ChoosePacketExportPath").at(-1)?.result).toMatchObject({ state: "cancelled" }));
  expect(portableReview().getByText("No destination chosen.")).toBeTruthy();
  expect((portableReview().getByRole("button", { name: "Export review" }) as HTMLButtonElement).disabled).toBe(true);
  await journey.nameNewFolder(journey.path(PROJECT, "reschedule-review"), "Choose a new folder for the portable review");
  await press(user, portableReview().getByRole("button", { name: "Choose destination…" }));
  expect(await panel.findByText(journey.path(PROJECT, "reschedule-review"))).toBeTruthy();
  await press(user, panel.getByRole("button", { name: "Export review" }));
  const exported = await line(panel, /^Review reschedule-review sealed: /);
  expect(exported).toMatch(new RegExp(`^Review reschedule-review sealed: identity [0-9a-f]{12}… · packet ${packetIdentity}…$`));
  expect(await line(panel, /^Renderings: /)).toBe(
    "Renderings: offline HTML, PDF, Markdown, strict JSON, JUnit · sensitivity retained: contains original source values · policy customer-local-only",
  );
  await panel.findByRole("option", { name: "reschedule-review" });
  await user.selectOptions(panel.getByLabelText("Reviews"), "reschedule-review");
  await press(user, panel.getByRole("button", { name: "Open read-only" }));
  expect(await line(panel, /^Runs: current /)).toBe(
    "Runs: current pass · baseline assertion_failure. Statuses are the retained evidence's own labels, never a passing run or an approved disclosure.",
  );
  expect(await line(panel, /^Renderings present: /)).toBe("Renderings present: junit.xml, report.html, report.json, report.md, report.pdf.");
  expect(panel.getByText(/^Report text hidden\./)).toBeTruthy();
  await press(user, panel.getByRole("button", { name: "Show report" }));
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
  // A colleague extended a policy with a member the sharing contract does not
  // have. It declares the contract, so the folder lists it once read again.
  journey.writeFile(
    `${PROJECT}/extended-sharing.json`,
    '{"schema":"readmit-sharing-policy/v1","support":true,"destinations":["local-file"],"max_bytes":4096,"recipient":"vendor"}\n',
  );
  await enter(user, support.getByLabelText("New policy document"), "sharing.json");
  await press(user, support.getByRole("button", { name: "Save sharing policy" }));
  expect(await line(support, /^Saved sharing\.json: /)).toBe("Saved sharing.json: support allowed, local-file, 4096 bytes.");
  await support.findByRole("option", { name: "sharing.json" });
  // The extended policy is refused by the contract's own decoder: the window
  // names the refusal and shows no reading, and `readmit share` prepares
  // nothing under it either.
  const refused = "readmit: sharing refused; source, policy, destination or exact approval unavailable\n";
  await support.findByRole("option", { name: "extended-sharing.json" });
  await user.selectOptions(support.getByLabelText("Sharing policy"), "extended-sharing.json");
  expect(await support.findByText("that entry is not a sharing policy this release prepares with")).toBeTruthy();
  expect(support.queryByText(byContent(/^Support allowed · /))).toBeNull();
  const extended = await journey.commandLine([
    "share", `${PROJECT}/reschedule-review`, "--kind", "portable-review", "--policy", `${PROJECT}/extended-sharing.json`,
  ]);
  expect(extended.code).toBe(1);
  expect(extended.stdout).toBe("");
  expect(extended.stderr.endsWith(refused)).toBe(true);
  await user.selectOptions(support.getByLabelText("Sharing policy"), "sharing.json");
  expect(await line(support, /^Support allowed · /)).toBe("Support allowed · local-file · 4096 bytes.");
  await user.selectOptions(support.getByLabelText("Source"), "reschedule-review");
  await press(user, support.getByRole("button", { name: "Preview summary" }));
  const identity = (await line(support, /^Preview identity: [0-9a-f]{64}$/)).replace("Preview identity: ", "");
  const other = identity.slice(0, -1) + (identity.endsWith("0") ? "1" : "0");
  await enter(user, support.getByLabelText("Preview ID"), other);
  await press(user, support.getByRole("button", { name: "Export support bundle" }));
  expect(
    await support.findByText(
      "this approval does not name the summary the current sources and policy produce; review the current preview again and approve the identity it displays",
    ),
  ).toBeTruthy();
  expect(journey.callsTo("PublishSupportSummary").at(-1)?.result).toMatchObject({ state: "failed" });
  await enter(user, support.getByLabelText("Preview ID"), identity);
  // Dismissing the save dialog names nothing, and the panel says so.
  const destination = "Choose a new folder for the reviewed support bundle";
  await journey.dismissDialog("save", destination);
  await press(user, support.getByRole("button", { name: "Choose destination…" }));
  expect(await support.findByText("no new folder was named")).toBeTruthy();
  expect((support.getByLabelText("New support folder") as HTMLInputElement).value).toBe("");
  // A folder that already exists, which the save dialog returns once the
  // person confirms replacing it, is refused by the writer and nothing is
  // written into it; `readmit share` refuses it the same way.
  journey.writeFile("outbox/for-vendor/kept.txt", "a file the person kept here\n");
  await journey.nameNewFolder(journey.path("outbox/for-vendor"), destination);
  await press(user, support.getByRole("button", { name: "Choose destination…" }));
  await waitFor(() => expect((support.getByLabelText("New support folder") as HTMLInputElement).value).toBe(journey.path("outbox/for-vendor")));
  expect(support.queryByText("no new folder was named")).toBeNull();
  await press(user, support.getByRole("button", { name: "Export support bundle" }));
  expect(
    await support.findByText(
      "the publication was refused; an incomplete directory has no completion marker and recovery is a new destination with a fresh review",
    ),
  ).toBeTruthy();
  const occupied = await journey.commandLine([
    "share", `${PROJECT}/reschedule-review`, "--kind", "portable-review", "--policy", `${PROJECT}/sharing.json`,
    "--approve", identity, "--output", "outbox/for-vendor",
  ]);
  expect(occupied.code).toBe(1);
  expect(occupied.stdout).toBe("");
  expect(occupied.stderr.endsWith(refused)).toBe(true);
  expect(journey.readFile("outbox/for-vendor/kept.txt")).toBe("a file the person kept here\n");
  expect(() => journey.readFile("outbox/for-vendor/support.json")).toThrow();
  await journey.nameNewFolder(journey.path(PROJECT, "support-for-vendor"), destination);
  await press(user, support.getByRole("button", { name: "Choose destination…" }));
  await waitFor(() =>
    expect((support.getByLabelText("New support folder") as HTMLInputElement).value).toBe(journey.path(PROJECT, "support-for-vendor")),
  );
  await press(user, support.getByRole("button", { name: "Export support bundle" }));
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

  // Something rewrites the published summary after it was verified. Verified
  // again, the window refuses the bundle and shows no identity for it, and
  // `readmit share verify` refuses it the same way.
  const summaryFile = `${PROJECT}/support-for-vendor/support.json`;
  const publishedSummary = journey.readFile(summaryFile);
  const alteredSummary = publishedSummary.replace('"outcome":"pass"', '"outcome":"fail"');
  expect(alteredSummary).not.toBe(publishedSummary);
  journey.changeFile(summaryFile, alteredSummary);
  await press(user, support.getByRole("button", { name: "Verify again" }));
  expect(
    await support.findByText(
      "this directory is not a complete support bundle this release verifies; a bundle missing, holding or hiding anything beyond its three members is refused",
    ),
  ).toBeTruthy();
  expect(support.queryByText(byContent(/^Verified bundle identity: /))).toBeNull();
  expect((support.getByLabelText("Verify a support bundle") as HTMLSelectElement).value).toBe("support-for-vendor");
  expect(await journey.commandLine(["share", "verify", `${PROJECT}/support-for-vendor`])).toEqual({ code: 1, stdout: "", stderr: refused });

  // Assembling, exporting, reviewing and sharing sent nothing.
  expect(downstream.received()).toHaveLength(sent);
});

/** The shipped planted example, byte for byte: every value in it is invented.
 * This older journey also proves existing hand-written documents still derive. */
function placePlantedExample(project: string) {
  journey.placeFixture("redact-booking.mllp", "captures/redact-booking.mllp");
  journey.placeFixture("redact-reschedule.mllp", "captures/redact-reschedule.mllp");
  journey.placeFixture("redact-spec.json", `${project}/spec.json`);
  journey.placeFixture("redact-policy.json", `${project}/policy.json`);
  journey.placeFixture("redact-policy-blocked.json", `${project}/blocked-policy.json`);
  journey.placeFixture("redact-inventory.json", `${project}/inventory.json`);
}

test("a policy and inventory authored in the window derive a ready review and the command line accepts their documents", async () => {
  const user = userEvent.setup();
  await journey.launch();
  await activateLicense(user, journey);
  await createProject(user, journey, "reviews", "authored-review", "Authored disclosure review");
  const project = "reviews/authored-review";
  journey.placeFixture("redact-booking.mllp", "captures/redact-booking.mllp");
  journey.placeFixture("redact-reschedule.mllp", "captures/redact-reschedule.mllp");
  journey.placeFixture("redact-spec.json", `${project}/spec.json`);
  // These shipped synthetic examples are input to the test operator. They are
  // outside the project: only actions in the window write its two documents.
  journey.placeFixture("redact-policy.json", "templates/policy.json");
  journey.placeFixture("redact-inventory.json", "templates/inventory.json");
  const policy = JSON.parse(journey.readFile("templates/policy.json")) as RedactPolicy;
  const inventory = JSON.parse(journey.readFile("templates/inventory.json")) as RedactInventory;
  await importCaptures(user, "original.case");
  const panel = privacy();

  await enter(user, panel.getByLabelText("Policy file"), "authored-policy.json");
  await press(user, panel.getByRole("button", { name: "Save policy" }));
  expect(await panel.findByText("invalid redaction policy: required_failures declares no assertion position")).toBeTruthy();
  await enter(user, panel.getByLabelText("Patient identifier selector"), policy.patient.selector);
  for (const [index, selector] of policy.patient.authority.entries()) {
    await press(user, panel.getByRole("button", { name: "Add patient authority" }));
    await enter(user, panel.getByLabelText(`Authority selector ${index + 1}`), selector);
  }
  for (const [index, rule] of policy.fields.entries()) {
    await press(user, panel.getByRole("button", { name: "Add field rule" }));
    await enter(user, panel.getByLabelText(`Field selector ${index + 1}`), rule.selector);
    await user.selectOptions(panel.getByLabelText(`Field class ${index + 1}`), rule.class);
    await user.selectOptions(panel.getByLabelText(`Field policy ${index + 1}`), rule.policy);
    if (rule.policy === "scoped-surrogate/v1") {
      await enter(user, panel.getByLabelText(`Surrogate scope ${index + 1}`), rule.scope ?? "");
      for (const [at, selector] of (rule.authority ?? []).entries()) {
        await press(user, panel.getByRole("button", { name: `Add rule authority ${index + 1}` }));
        await enter(user, panel.getByLabelText(`Rule authority selector ${index + 1}.${at + 1}`), selector);
      }
    }
    if (rule.policy === "replace-field/v1") {
      await enter(user, panel.getByLabelText(`Replacement ${index + 1}`), rule.replacement ?? "");
    }
    if (rule.policy === "retain-literal/v1") {
      for (const [at, value] of (rule.allowed ?? []).entries()) {
        await press(user, panel.getByRole("button", { name: `Add literal ${index + 1}` }));
        await enter(user, panel.getByLabelText(`Allowed literal ${index + 1}.${at + 1}`), value);
      }
    }
  }
  for (const [index, segment] of policy.remove_segments.entries()) {
    await press(user, panel.getByRole("button", { name: "Add segment" }));
    await enter(user, panel.getByLabelText(`Segment ${index + 1}`), segment);
  }
  for (const name of policy.packet_policies) {
    await user.click(panel.getByRole("checkbox", { name }));
  }
  for (const [index, item] of policy.spec_bindings.entries()) {
    await press(user, panel.getByRole("button", { name: "Add literal binding" }));
    await enter(user, panel.getByLabelText(`Binding location ${index + 1}`), item.location);
    if (item.constant !== undefined) {
      await user.selectOptions(panel.getByLabelText(`Binding source ${index + 1}`), "constant");
      await user.selectOptions(panel.getByLabelText(`Protocol code ${index + 1}`), item.constant);
    } else {
      await enter(user, panel.getByLabelText(`Source occurrence ${index + 1}`), item.occurrence ?? "");
      await enter(user, panel.getByLabelText(`Source selector ${index + 1}`), item.selector ?? "");
    }
  }
  for (const [index, position] of policy.required_failures.entries()) {
    await press(user, panel.getByRole("button", { name: "Add failure" }));
    await enter(user, panel.getByLabelText(`Assertion position ${index + 1}`), String(position));
  }
  await press(user, panel.getByRole("button", { name: "Save policy" }));
  expect(await panel.findByText("Saved disclosure policy authored-policy.json.")).toBeTruthy();
  await panel.findByRole("option", { name: "authored-policy.json" });

  await user.click(panel.getByRole("checkbox", { name: "Confirm inventory" }));
  for (const [index, artifact] of inventory.artifacts.entries()) {
    await press(user, panel.getByRole("button", { name: "Add original artifact" }));
    await user.selectOptions(panel.getByLabelText(`Artifact kind ${index + 1}`), artifact.kind);
    await enter(user, panel.getByLabelText(`Artifact path ${index + 1}`), artifact.path);
  }
  for (const [index, value] of inventory.residual_values.entries()) {
    await press(user, panel.getByRole("button", { name: "Add known value" }));
    await enter(user, panel.getByLabelText(`Known value ${index + 1}`), value);
  }
  await enter(user, panel.getByLabelText("New original-artifact inventory document"), "authored-inventory.json");
  await press(user, panel.getByRole("button", { name: "Save inventory" }));
  expect(await panel.findByText("Saved original-artifact inventory authored-inventory.json.")).toBeTruthy();
  await panel.findByRole("option", { name: "authored-inventory.json" });

  await user.selectOptions(panel.getByLabelText("Case"), "original.case");
  await user.selectOptions(panel.getByLabelText("Original specification"), "spec.json");
  await user.selectOptions(panel.getByLabelText("Disclosure policy"), "authored-policy.json");
  await user.selectOptions(panel.getByLabelText("Original-artifact inventory"), "authored-inventory.json");
  await press(user, panel.getByRole("button", { name: "Create review" }));
  expect(await line(panel, /^Review review-\d+ · ready-for-approval/)).toBeTruthy();
  const windowIdentity = (await line(panel, /^Identity an approval must name: /)).replace("Identity an approval must name: ", "");
  const cli = await journey.commandLine([
    "--operation-policy", journey.path("vendor-delivered-license", "operation-policy.json"),
    "redact", `${project}/original.case`, "--spec", `${project}/spec.json`,
    "--policy", `${project}/authored-policy.json`, "--inventory", `${project}/authored-inventory.json`,
    "--local-state", `${project}/cli-private`, "--output", `${project}/cli-review`,
  ]);
  expect(cli.code).toBe(0);
  expect(JSON.parse(journey.readFile(`${project}/cli-review/review.json`)).state).toBe("ready-for-approval");
  const exported = await journey.commandLine([
    "redact", "export", `${project}/review-001`, "--local-state", `${project}/review-private-001`,
    "--approve", windowIdentity, "--output", `${project}/cli-export-of-window-review`,
  ]);
  expect(exported.code).toBe(0);
  expect(JSON.parse(journey.readFile(`${project}/cli-export-of-window-review/export-review.json`)).approved_review_identity).toBe(windowIdentity);
  expect(journey.callsTo("SaveRedactPolicy")).toHaveLength(2);
  expect(journey.callsTo("SaveRedactInventory")).toHaveLength(1);
});

/** Imports the two MLLP captures into the open project as one registered
 * case, with the framing the capture used. */
async function importCaptures(user: UserEvent, caseName: string) {
  const evidence = within(region("Evidence"));
  await press(user, evidence.getByRole("button", { name: "Import" }));
  await journey.chooseFiles([journey.path("captures/redact-booking.mllp"), journey.path("captures/redact-reschedule.mllp")], "Choose evidence files to import");
  await press(user, await screen.findByRole("button", { name: "Select Files…" }));
  await within(screen.getByRole("region", { name: "Declared sources" })).findByText(/redact-reschedule\.mllp/);
  await user.selectOptions(screen.getByLabelText("Framing"), "mllp");
  await press(user, within(screen.getByRole("region", { name: "Extraction preview" })).getByRole("button", { name: "Preview" }));
  const commit = within(screen.getByRole("region", { name: "Commit import" }));
  await enter(user, commit.getByLabelText("Case bundle folder name"), caseName);
  await enter(user, commit.getByLabelText("Receipt file name"), `${caseName}-receipt.json`);
  await enter(user, commit.getByLabelText("Case title"), "Planted example");
  await press(user, commit.getByRole("button", { name: "Import" }));
  expect(await commit.findByText("Import Completed Successfully")).toBeTruthy();
  await press(user, commit.getByRole("button", { name: "Open case" }));
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
  await press(user, panel.getByRole("button", { name: "Create review" }));
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
  await enter(user, exporting.getByLabelText("Review ID", { selector: "#privacy-export-approval" }), blocked.identity);
  await press(user, exporting.getByRole("button", { name: "Export packet" }));
  const refusal = "export requires approval of an exact fully handled and proven review";
  expect(await exporting.findByText(refusal)).toBeTruthy();

  // The ready review is exported only under its exact identity.
  await user.selectOptions(exporting.getByLabelText("Review to export"), "review-002");
  await enter(user, exporting.getByLabelText("Its private local state"), "review-private-002");
  await enter(user, exporting.getByLabelText("Review ID", { selector: "#privacy-export-approval" }), blocked.identity);
  const asked = journey.callsTo("ExportDerivedPacket").length;
  await press(user, exporting.getByRole("button", { name: "Export packet" }));
  await waitFor(() => expect(journey.callsTo("ExportDerivedPacket")[asked]?.result).toEqual({ state: "failed", reason: refusal }));
  await enter(user, exporting.getByLabelText("Review ID", { selector: "#privacy-export-approval" }), handled.identity);
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

/** The command line under the vendor-delivered activation the window uses. */
function licensed(args: string[]) {
  return journey.commandLine(["--operation-policy", journey.path("vendor-delivered-license", "operation-policy.json"), ...args]);
}

/** Starts the built-in appointment-ledger receiver on the recorded target's
 * address for the two messages a phase sends, writing the observation the
 * specification names, once that observation exists. `finished` settles when
 * the receiver has taken both messages and exited. */
async function ledgerReceiver(address: string, observation: string, output: string) {
  const finished = licensed(["listen", "--address", address, "--mode", "defective", "--output", output, "--observation", observation, "--max-messages", "2"]);
  await waitFor(() => expect(() => journey.readFile(observation)).not.toThrow(), { timeout: 30_000 });
  return { finished };
}

test("an approved review is reexecuted against the target its original failing run recorded, only after a preview and an explicit authorization", async () => {
  const user = userEvent.setup();
  await journey.launch();
  await activateLicense(user, journey);
  await createProject(user, journey, "reviews", "reexecution", "Reviewed reexecution");
  const project = "reviews/reexecution";
  placePlantedExample(project);
  // The target the original run was sent to, recorded by the person before
  // its receiver started: the loopback address its ledger receiver takes.
  const address = await freeLoopbackAddress();
  journey.writeFile(
    `${project}/original-target.json`,
    JSON.stringify({ schema: "readmit-target/v1", test_endpoint: true, address, transport: "plain", approved_transport: false, connect_timeout: "10s", message_timeout: "30s", max_ack_bytes: 65536 }),
  );
  journey.makeFolder("receiver");
  await importCaptures(user, "original.case");

  // The actual original phase: the planted test run once through the window
  // against the defective ledger, failing as reviewed.
  const original = (await ledgerReceiver(address, `${project}/test-observation.json`, "receiver/original")).finished;
  const running = runs();
  await running.findByRole("option", { name: "spec.json (test)" });
  await user.selectOptions(running.getByLabelText("Saved test or suite"), "spec.json");
  await enter(user, running.getByLabelText("Run folder"), "run-original");
  await press(user, running.getByRole("button", { name: "Preview run" }));
  expect(await running.findByText(byContent(/^Admission: admitted$/))).toBeTruthy();
  await press(user, running.getByRole("button", { name: "Send test" }));
  expect((await running.findByText(byContent(/^Run: \w+ · Stop reason: \w+$/))).textContent).toBe("Run: assertion_failed · Stop reason: assertion_failed");
  expect((await original).code).toBe(0);

  // The review, then the specification the person rebinds by hand to the
  // approved derived case, the same target and a new observation, exactly as
  // the reexecution guide describes: nothing else in it changes.
  const handled = await derive(user, "policy.json");
  expect(handled.review).toMatch(/^Review review-001 · ready-for-approval · /);
  const rebound = JSON.parse(journey.readFile(`${project}/review-001/spec.json`)) as {
    input: { case: string };
    target: string;
    observation: { path: string };
  };
  rebound.input.case = "review-001/case";
  rebound.target = "original-target.json";
  rebound.observation.path = "reexecution-observation.json";
  journey.writeFile(`${project}/rebound.json`, JSON.stringify(rebound));

  // The original run becomes the current run of a retained packet.
  const packet = packets();
  await packet.findAllByRole("option", { name: "run-original" });
  await user.selectOptions(packet.getByLabelText("Case"), "original.case");
  await user.selectOptions(packet.getByLabelText("Historical specification"), "spec.json");
  await user.selectOptions(packet.getByLabelText("Current result"), "run-original");
  await press(user, packet.getByRole("button", { name: "Preview" }));
  expect(await line(packet, /^Current result: /)).toBe("Current result: run-original — found · assertion_failure · assertion_failed · boundary appointment-ledger");
  await press(user, await packet.findByRole("button", { name: "Create packet" }));
  expect(await line(packet, /^Packet packet-001 sealed: /)).toBeTruthy();

  const step = within(privacy().getByRole("group", { name: "Rerun" }));
  await step.findByRole("option", { name: "rebound.json" });
  await user.selectOptions(step.getByLabelText("Approved review"), "review-001");
  await enter(user, step.getByLabelText("Private local state its derivation wrote"), "review-private-001");
  await user.selectOptions(step.getByLabelText(/^Original packet/), "packet-001");
  await user.selectOptions(step.getByLabelText("Rebound execution specification"), "rebound.json");
  await user.selectOptions(step.getByLabelText("Phase"), "failure");
  const approving = step.getByLabelText("Review ID");
  const reexecute = (approval: string, ...send: string[]) =>
    licensed(["redact", "reexecute", `${project}/review-001`, "--local-state", `${project}/review-private-001`, "--approve", approval,
      "--original-packet", `${project}/packet-001`, "--spec", `${project}/rebound.json`, "--phase", "failure", ...send]);

  // An identity that is not the review's is refused by the window and the
  // command line in the same sentence, and nothing is offered to send.
  const unapproved = "reexecution requires the exact complete disclosure review; changed artifacts require review again";
  await enter(user, approving, "0".repeat(64));
  await press(user, step.getByRole("button", { name: "Preview" }));
  expect(await step.findByText(unapproved)).toBeTruthy();
  expect((step.getByRole("button", { name: "Send once" }) as HTMLButtonElement).disabled).toBe(true);
  const refused = await reexecute("0".repeat(64));
  expect(refused.code).not.toBe(0);
  expect(refused.stderr).toBe(`readmit: ${unapproved}\n`);

  // The review's own identity previews what a send would do, and the command
  // line previews the same; the preview sends nothing: the receiver waits for
  // both messages until the person authorizes the one send.
  await enter(user, approving, handled.identity);
  const receiving = (await ledgerReceiver(address, `${project}/reexecution-observation.json`, "receiver/reexecuted")).finished;
  let received = false;
  void receiving.then(() => (received = true));
  await press(user, step.getByRole("button", { name: "Preview" }));
  expect(await line(step, /^Target: /)).toBe(`Target: (unnamed) · unclassified · plain · ${address}`);
  expect(await line(step, /^Sends /)).toBe("Sends 2 messages of the approved derived case:");
  expect(step.getByText("s0001-e000001 → o000001")).toBeTruthy();
  expect(step.getByText("s0002-e000001 → o000002")).toBeTruthy();
  expect(await line(step, /^Destination: /)).toBe("Destination: reexecution-001 (generated) · fresh");
  expect(await line(step, /^Admission: /)).toBe("Admission: admitted");
  expect(await line(step, /^Criteria: /)).toBe("Criteria: not-executed · external equivalence declined · disclosure customer-local-only-new-review-required");
  const specIdentity = journey.digest(`${project}/rebound.json`);
  expect(step.getByText(handled.identity, { selector: "dd" })).toBeTruthy();
  expect(step.getByText(specIdentity, { selector: "dd" })).toBeTruthy();
  const previewed = await reexecute(handled.identity);
  expect(previewed.code).toBe(0);
  const cliPreview = JSON.parse(previewed.stdout) as Record<string, string>;
  expect(cliPreview).toMatchObject({
    review_identity: handled.identity,
    execution_spec_identity: specIdentity,
    criteria: "not-executed",
    external_equivalence: "declined",
  });
  const packetIdentity = journey.digest(`${project}/packet-001/manifest.json`);
  expect(cliPreview.original_packet_identity).toBe(packetIdentity);
  expect(step.getByText(packetIdentity, { selector: "dd" })).toBeTruthy();
  expect(received).toBe(false);

  // Authorized from the keyboard, it sends once and is assessed as the
  // command assesses it: the reviewed failure set matched, and external
  // equivalence declined.
  const authorize = step.getByLabelText(/^I authorize this single nonproduction send/) as HTMLInputElement;
  await tabTo(user, authorize);
  await user.keyboard(" ");
  await tabTo(user, step.getByRole("button", { name: "Send once" }));
  await user.keyboard("{Enter}");
  expect(await line(step, /^Criteria: matched/)).toBe("Criteria: matched · external equivalence declined");
  expect(await line(step, /^Job /)).toBe("Job reexecution-001 · run assertion_failed · stop reason assertion_failed");
  expect(await line(step, /^Deliveries: /)).toBe("Deliveries: acknowledged 2 · uncertain 0 · not attempted 0");
  expect((await receiving).code).toBe(0);
  expect(journey.callsTo("ReexecuteReviewedEvidence")).toHaveLength(1);
  // The attempt spent the preview and the authorization: nothing is offered
  // to send again.
  expect(authorize.checked).toBe(false);
  expect((step.getByRole("button", { name: "Send once" }) as HTMLButtonElement).disabled).toBe(true);

  // The command line recovers the job the window retained, to the result the
  // window assessed.
  const resultLine = await line(step, /^Result identity: /);
  const recovered = await journey.commandLine(["run", "status", `${project}/reexecution-001`, "--recovery", "--json"]);
  expect(recovered.code).toBe(1);
  const recovery = JSON.parse(recovered.stdout) as { terminal: boolean; uncertain: number; run: { result_identity: string } };
  expect(recovery.terminal).toBe(true);
  expect(recovery.uncertain).toBe(0);
  expect(`Result identity: ${recovery.run.result_identity}`).toBe(resultLine);
});
