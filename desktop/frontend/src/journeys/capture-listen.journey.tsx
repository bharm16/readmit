// The capture screen's listeners and declared documents, against the real
// facade over real files. The built-in SIU fixture listens on the port it
// chose, which the window shows so an upstream system can be pointed at it;
// it books and reschedules, is cancelled part way from the keyboard, and
// refuses a wider address or one another program holds, each as `readmit
// listen` does. A collector the application died under reopens its journal
// read-only and never listens again. A source registration and a responder
// policy written for the command line reopen for review and further editing,
// and the command line reads what the window then saved. The upstream system
// (upstream.js) shares no readmit code.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { enter, Journey, press, region, whenEnabled } from "../testkit/journey";
import { accepts, countingListener, exists } from "./probes.js";
import { BOOKING, EXPORTED_BOOKING, licensedProject, tabTo } from "./steps";
import { sendMllp } from "./upstream.js";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

const PROJECT = "investigations/interface";

/** The command line under the vendor-delivered activation the window uses. */
function licensed(args: string[]) {
  return journey.commandLine(["--operation-policy", journey.path("vendor-delivered-license", "operation-policy.json"), ...args]);
}

/** The sentence the command line refused with, before window-specific guidance. */
function refusal(stderr: string): string {
  return stderr.replace(/^readmit: /, "").trim();
}

async function openCapture(user: ReturnType<typeof userEvent.setup>) {
  await press(user, await within(region("Evidence")).findByRole("button", { name: "Capture" }));
  return within(await screen.findByRole("region", { name: "Capture" }));
}

/** The address the running capture reports it bound, read off the screen the
 * way a person reads it to configure the system that sends to it. */
async function listeningAddress(capture: ReturnType<typeof within>): Promise<string> {
  const line = await capture.findByText(/· Listening on 127\.0\.0\.1:\d+$/);
  const address = /Listening on (127\.0\.0\.1:\d+)$/.exec(line.textContent ?? "")?.[1];
  if (!address) throw new Error(`no address in ${line.textContent}`);
  return address;
}

const PLAN = ["--framing", "raw", "--terminator", "cr", "--encoding", "utf-8", "--direction", "inbound", "--member", ".hl7"];

test("the SIU fixture listens on the port it chose, completes, is cancelled part way and refuses a wider or taken address, as readmit listen does", async () => {
  const user = userEvent.setup();
  await licensedProject(journey, user);
  journey.placeFixture("listen-s12.hl7", "upstream/listen-s12.hl7");
  journey.placeFixture("listen-s13.hl7", "upstream/listen-s13.hl7");
  journey.placeFixture("listen-defective.json", "expected/listen-defective.json");
  const booking = journey.readFile("upstream/listen-s12.hl7");
  const reschedule = journey.readFile("upstream/listen-s13.hl7");

  const capture = await openCapture(user);
  await press(user, capture.getByRole("tab", { name: "SIU fixture" }));
  const address = capture.getByLabelText("Listen address");
  const preview = capture.getByRole("button", { name: "Preview fixture" });
  const start = capture.getByRole("button", { name: "Start listener" });

  // Every interface without the explicit approval is refused before anything
  // binds; the fixture window directs the person to a loopback address while
  // the command line retains its flag wording.
  await enter(user, address, "0.0.0.0:0");
  await press(user, preview);
  const wider = await licensed(["listen", "--address", "0.0.0.0:0", "--mode", "fixed",
    "--output", `${PROJECT}/never.case`, "--observation", `${PROJECT}/never.json`]);
  expect(wider.code).not.toBe(0);
  expect(await capture.findByText("Choose a loopback listen address for the SIU fixture.")).toBeTruthy();
  expect(refusal(wider.stderr)).toBe("accepting connections from beyond this machine is opt-in: pass --approved-bind to bind a nonloopback address");
  expect((start as HTMLButtonElement).disabled).toBe(true);

  // An address another program already holds is refused at the bind, as the
  // command line refuses it; nothing reaches that program and nothing is
  // written.
  const held = await countingListener();
  await enter(user, address, held.address);
  await press(user, preview);
  await press(user, start);
  const taken = await licensed(["listen", "--address", held.address, "--mode", "fixed",
    "--output", `${PROJECT}/never.case`, "--observation", `${PROJECT}/never.json`]);
  expect(await capture.findByText(refusal(taken.stderr))).toBeTruthy();
  expect(held.accepted()).toBe(0);
  await held.close();
  for (const entry of ["capture.case", "observation.json", "never.case", "never.json"]) {
    expect(exists(journey.path(PROJECT, entry))).toBe(false);
  }

  // At the port it chooses, the fixture tells the window where it listens,
  // and the upstream system books and reschedules there.
  await enter(user, address, "127.0.0.1:0");
  await user.selectOptions(capture.getByLabelText("Mode"), "defective");
  await enter(user, capture.getByLabelText("Max messages (0 = until cancel)"), "2");
  await press(user, preview);
  await press(user, start);
  const bound = await listeningAddress(capture);
  const acknowledgements = await sendMllp(bound, [booking, reschedule]);
  expect(acknowledgements[0]).toMatch(/\rMSA\|AA\|LISTEN-BOOK\r/);
  expect(acknowledgements[1]).toMatch(/\rMSA\|AA\|LISTEN-MOVE\r/);
  expect(await capture.findByText("Case sealed: capture.case · 2 messages · 1 sources")).toBeTruthy();
  expect(
    capture.getByText(
      "Appointment ledger observation.json: readmit-observation/v1 · Receiver mode: defective · Processed occurrences: 2 · Ledger records: 2 · Consistent: true",
    ),
  ).toBeTruthy();
  expect(capture.queryByText(/Listening on/)).toBeNull();
  expect(await accepts(bound)).toBe(false);

  // The exported ledger is the hand-authored expectation for the defective
  // mode: a second record with the same filler identifier. The command line
  // reads the sealed case as the window reported it.
  const exported = JSON.parse(journey.readFile(`${PROJECT}/observation.json`)) as { records: unknown[] };
  expect(exported.records).toEqual(JSON.parse(journey.readFile("expected/listen-defective.json")));
  const timeline = await journey.commandLine(["timeline", `${PROJECT}/capture.case`]);
  expect(timeline.code).toBe(0);
  expect(timeline.stdout).toContain("Schema: readmit-case/v2\nProvenance: recorded\nSources: 1\nOccurrences: 4\nMessages: 2\nACKs: 2\n");
  expect(timeline.stdout).toContain(
    "Observation: readmit-observation/v1\nObservation profile: readmit-siu-v1\nReceiver mode: defective\nProcessed occurrences: 2\nLedger records: 2\nConsistent: true\n",
  );

  // Started again until cancelled, it takes the booking, and Cancel — which
  // holds the focus while the fixture listens — stops it from the keyboard.
  // The case seals what arrived before the cancellation and says it was
  // cancelled, not completed.
  await enter(user, capture.getByLabelText("Case output name"), "cancelled.case");
  await enter(user, capture.getByLabelText("Observation file"), "cancelled.json");
  await enter(user, capture.getByLabelText("Max messages (0 = until cancel)"), "0");
  await press(user, preview);
  await press(user, start);
  const again = await listeningAddress(capture);
  expect((await sendMllp(again, [booking]))[0]).toMatch(/\rMSA\|AA\|LISTEN-BOOK\r/);
  const stop = capture.getByRole("button", { name: "Cancel" });
  await waitFor(() => expect(document.activeElement).toBe(stop));
  await user.keyboard("{Enter}");
  expect(await capture.findByText("the operation was cancelled")).toBeTruthy();
  expect(journey.callsTo("Cancel").at(-1)?.args).toEqual(["capture"]);
  expect(capture.getByText("Case sealed: cancelled.case · 1 messages · 1 sources")).toBeTruthy();
  expect(
    capture.getByText(
      "Appointment ledger cancelled.json: readmit-observation/v1 · Receiver mode: defective · Processed occurrences: 1 · Ledger records: 1 · Consistent: true",
    ),
  ).toBeTruthy();
  expect(await accepts(again)).toBe(false);
  await waitFor(() => expect(document.activeElement).toBe(start));
  const cancelled = await journey.commandLine(["timeline", `${PROJECT}/cancelled.case`]);
  expect(cancelled.stdout).toContain("Messages: 1\nACKs: 1\n");
  expect(cancelled.stdout).toContain("Processed occurrences: 1\nLedger records: 1\nConsistent: true\n");
});

test("a controlled collector rejection authored in the window passes the real policy reader before preview", async () => {
  const user = userEvent.setup();
  await licensedProject(journey, user);
  const capture = await openCapture(user);
  await press(user, capture.getByRole("tab", { name: "MLLP collect" }));

  await user.selectOptions(capture.getByLabelText("Controlled fault (v3 synthetic)"), "reject");
  expect((capture.getByLabelText("Listen address") as HTMLInputElement).value).toBe("127.0.0.1:2575");
  await press(user, capture.getByRole("button", { name: "Preview collector" }));
  await whenEnabled(capture.getByRole("button", { name: "Start collecting" }));

  const saved = JSON.parse(journey.readFile(`${PROJECT}/receiver-policy.json`));
  expect(saved).toMatchObject({
    schema: "readmit-receiver-policy/v3",
    faults: {
      environment_class: "nonproduction",
      approved_test_endpoints: ["127.0.0.1:2575"],
      steps: [{ message: 1, stage: "application", action: "reject", delay_ms: 0 }],
    },
  });
  expect(journey.callsTo("SaveReceiverPolicy")).toHaveLength(1);
  expect(journey.callsTo("PreviewCapture").at(-1)?.result).toMatchObject({ state: "completed" });

  // If the person changes that explicit endpoint back to port zero, the
  // shared reader refuses the attempted replacement without changing the
  // accepted document or running a collector preview.
  const policyBytes = journey.digest(`${PROJECT}/receiver-policy.json`);
  await enter(user, capture.getByLabelText("Listen address"), "127.0.0.1:0");
  await press(user, capture.getByRole("button", { name: "Preview collector" }));
  expect(await capture.findByText("fault endpoints must be literal unicast IP addresses and nonzero ports")).toBeTruthy();
  expect(journey.digest(`${PROJECT}/receiver-policy.json`)).toBe(policyBytes);
  expect(journey.callsTo("PreviewCapture")).toHaveLength(1);
});

test("a declared source registration and responder policy reopen for review and further editing, read as the command line reads them", async () => {
  const user = userEvent.setup();
  await licensedProject(journey, user);
  const exports = journey.makeFolder(`${PROJECT}/exports`);
  journey.writeFile(`${PROJECT}/exports/booking.hl7`, EXPORTED_BOOKING);
  const registration = journey.writeFile(
    `${PROJECT}/sources/exports.json`,
    JSON.stringify(
      {
        schema: "readmit-source/v1",
        name: "scheduling-exports",
        kind: "directory",
        scope: "appointments",
        root: exports,
        quota: { max_entries: 16, max_entry_bytes: 65536, max_total_bytes: 1048576 },
        retry: { attempts: 2, backoff: "250ms" },
      },
      null,
      2,
    ),
  );
  const brokenRegistration = journey.writeFile(
    `${PROJECT}/sources/broken.json`,
    journey.readFile(`${PROJECT}/sources/exports.json`).replace('"name"', '"extra": true, "name"'),
  );
  const declaredPolicy = {
    schema: "readmit-receiver-policy/v3",
    name: "faulting-sink",
    source_label: "downstream-test-endpoint",
    acknowledgement: { operator: "original-mode-fixed-code", code: "AE" },
    accepted_message_types: { operator: "message-type-in", values: ["SIU^S12", "SIU^S13"] },
    enhanced_acknowledgement: {
      operator: "unsupported",
      accept_code: "",
      application_code: "",
      application_delivery: "",
      application_endpoint: "",
      approved_transport: false,
    },
    faults: {
      environment_class: "nonproduction",
      approved_test_endpoints: ["127.0.0.1:2575"],
      steps: [
        { message: 1, stage: "application", action: "delay", delay_ms: 25 },
        { message: 3, stage: "application", action: "reject", delay_ms: 0 },
      ],
    },
  };
  const policy = journey.writeFile(`${PROJECT}/policies/faulting.json`, JSON.stringify(declaredPolicy, null, 2));
  const brokenPolicy = journey.writeFile(
    `${PROJECT}/policies/broken.json`,
    JSON.stringify({ ...declaredPolicy, schema: "readmit-receiver-policy/v1" }),
  );

  // The command line diagnoses the registration as it was written for it.
  const authored = await licensed(["source", "diagnose", `${PROJECT}/sources/exports.json`, ...PLAN]);
  expect(authored.stderr).toBe("");
  expect(authored.stdout).toContain("Source: scheduling-exports\nKind: directory\nScope: appointments\n");

  const capture = await openCapture(user);
  const openRegistration = capture.getByRole("button", { name: "Open registration…" });

  // A dismissed dialog opens nothing.
  await journey.dismissDialog("files", "Choose a source registration");
  await press(user, openRegistration);
  await waitFor(() => expect(journey.callsTo("ChooseCapturePath")[0]?.result).toMatchObject({ state: "cancelled" }));
  expect(journey.callsTo("ReadSourceRegistration")).toHaveLength(0);
  expect(capture.queryByLabelText("Opened source registration")).toBeNull();

  // A registration its reader refuses is refused in the command line's words
  // and opens nothing.
  await journey.chooseFiles([brokenRegistration], "Choose a source registration");
  await press(user, openRegistration);
  const broken = await licensed(["source", "diagnose", `${PROJECT}/sources/broken.json`, ...PLAN]);
  expect(broken.code).not.toBe(0);
  expect(await capture.findByText(refusal(broken.stderr))).toBeTruthy();
  expect(capture.queryByLabelText("Opened source registration")).toBeNull();

  // From the keyboard, the authored registration opens for review and fills
  // the form, focus returning to the control that opened it.
  await journey.chooseFiles([registration], "Choose a source registration");
  await tabTo(user, openRegistration);
  await user.keyboard("{Enter}");
  const source = within(await capture.findByLabelText("Opened source registration"));
  expect(source.getByText("scheduling-exports")).toBeTruthy();
  expect(source.getByText("16 entries, 65536 bytes per entry, 1048576 bytes in total")).toBeTruthy();
  expect(source.getByText("2 attempts, backoff 250ms")).toBeTruthy();
  expect(source.getByText(exports)).toBeTruthy();
  expect((capture.getByLabelText("Scope") as HTMLInputElement).value).toBe("appointments");
  expect((capture.getByLabelText("Registration file") as HTMLInputElement).value).toBe(registration);
  await waitFor(() => expect(document.activeElement).toBe(openRegistration));

  // Edited and saved under a new name, the review shows what is on disk, the
  // authored document is untouched, and the command line diagnoses the
  // window's copy exactly as the original but for the edited scope.
  const registrationBytes = journey.digest(`${PROJECT}/sources/exports.json`);
  await enter(user, capture.getByLabelText("Scope"), "referrals");
  await enter(user, capture.getByLabelText("Registration file"), "sources/referrals.json");
  await press(user, capture.getByRole("button", { name: "Save registration" }));
  expect(await source.findByText("referrals")).toBeTruthy();
  expect(source.getByText("sources/referrals.json")).toBeTruthy();
  expect(journey.digest(`${PROJECT}/sources/exports.json`)).toBe(registrationBytes);
  const copied = await licensed(["source", "diagnose", `${PROJECT}/sources/referrals.json`, ...PLAN]);
  expect(copied.stderr).toBe("");
  expect(copied.stdout).toBe(authored.stdout.replace("Scope: appointments\n", "Scope: referrals\n"));

  // The responder policy: one the reader refuses first.
  await press(user, capture.getByRole("tab", { name: "MLLP collect" }));
  const openPolicy = capture.getByRole("button", { name: "Open policy…" });
  await journey.chooseFiles([brokenPolicy], "Choose a receiver policy");
  await press(user, openPolicy);
  const refused = await licensed(["collect", "--address", "127.0.0.1:0", "--policy", `${PROJECT}/policies/broken.json`, "--output", `${PROJECT}/never.case`]);
  expect(refused.code).not.toBe(0);
  expect(await capture.findByText(refusal(refused.stderr))).toBeTruthy();
  expect(capture.queryByLabelText("Opened responder policy")).toBeNull();

  // Opened from the keyboard, every declaration is shown, including the
  // second fault step and the approved endpoint the form has no control for.
  await journey.chooseFiles([policy], "Choose a receiver policy");
  await tabTo(user, openPolicy);
  await user.keyboard("{Enter}");
  const opened = within(await capture.findByLabelText("Opened responder policy"));
  expect(opened.getByText("readmit-receiver-policy/v3")).toBeTruthy();
  expect(opened.getByText("original-mode-fixed-code AE")).toBeTruthy();
  expect(opened.getByText("message-type-in: SIU^S12, SIU^S13")).toBeTruthy();
  expect(
    opened.getByText("nonproduction · approved 127.0.0.1:2575 · message 1 application delay 25 ms; message 3 application reject"),
  ).toBeTruthy();
  expect((capture.getByLabelText("Controlled fault (v3 synthetic)") as HTMLSelectElement).value).toBe("delay");
  expect((capture.getByLabelText("Policy name") as HTMLInputElement).value).toBe("faulting-sink");
  expect((capture.getByLabelText("Listen address") as HTMLInputElement).value).toBe("127.0.0.1:2575");

  // Its approved address previews immediately without rewriting the saved
  // document. An explicitly chosen port zero or another unapproved address
  // is still refused before anything binds, in the command line's words.
  const policyBytes = journey.digest(`${PROJECT}/policies/faulting.json`);
  await press(user, capture.getByRole("button", { name: "Preview collector" }));
  await whenEnabled(capture.getByRole("button", { name: "Start collecting" }));
  expect(journey.callsTo("SaveReceiverPolicy")).toHaveLength(0);
  expect(journey.digest(`${PROJECT}/policies/faulting.json`)).toBe(policyBytes);
  for (const unapproved of ["127.0.0.1:0", "127.0.0.1:2576"]) {
    await enter(user, capture.getByLabelText("Listen address"), unapproved);
    await press(user, capture.getByRole("button", { name: "Preview collector" }));
    const command = await licensed(["collect", "--address", unapproved, "--policy", `${PROJECT}/policies/faulting.json`, "--output", `${PROJECT}/never.case`]);
    expect(refusal(command.stderr)).toMatch(/^fault endpoint/);
    expect(await capture.findByText(refusal(command.stderr))).toBeTruthy();
  }
  expect(journey.callsTo("SaveReceiverPolicy")).toHaveLength(0);
  expect(journey.digest(`${PROJECT}/policies/faulting.json`)).toBe(policyBytes);

  // Edited, saved under a new name and previewed at the endpoint it approves:
  // the copy keeps both fault steps and the approved endpoint, the review
  // shows it, and the command line reads it as far as the same endpoint rule.
  await enter(user, capture.getByLabelText("Source label"), "scheduling-archive");
  await enter(user, capture.getByLabelText("Policy file"), "policies/archive.json");
  await enter(user, capture.getByLabelText("Listen address"), "127.0.0.1:2575");
  await press(user, capture.getByRole("button", { name: "Preview collector" }));
  // The preview completed: starting is offered, and nothing was started.
  await whenEnabled(capture.getByRole("button", { name: "Start collecting" }));
  expect(await opened.findByText("scheduling-archive")).toBeTruthy();
  expect(opened.getByText("policies/archive.json")).toBeTruthy();
  expect(JSON.parse(journey.readFile(`${PROJECT}/policies/archive.json`))).toEqual({
    ...declaredPolicy,
    source_label: "scheduling-archive",
  });
  expect(journey.digest(`${PROJECT}/policies/faulting.json`)).toBe(policyBytes);
  const windowCopy = await licensed(["collect", "--address", "127.0.0.1:2576", "--policy", `${PROJECT}/policies/archive.json`, "--output", `${PROJECT}/never.case`]);
  expect(refusal(windowCopy.stderr)).toBe("fault endpoint is not an explicitly approved test endpoint");
  expect(exists(journey.path(PROJECT, "never.case"))).toBe(false);
});

test("a collector the application died under reopens its journal read-only after a restart and never listens again, as readmit collect status reads it", async () => {
  const user = userEvent.setup();
  await licensedProject(journey, user);
  const capture = await openCapture(user);
  await press(user, capture.getByRole("tab", { name: "MLLP collect" }));
  await press(user, capture.getByRole("button", { name: "Preview collector" }));
  await press(user, capture.getByRole("button", { name: "Start collecting" }));
  const bound = await listeningAddress(capture);
  expect((await sendMllp(bound, [BOOKING]))[0]).toMatch(/\rMSA\|AA\|CTL-1\r/);
  // The journal has retained the frame and the acknowledgement the upstream
  // system received when the application dies; the command line reads it
  // while the collector still runs, and changes nothing.
  const journalOf = async () => JSON.parse((await journey.commandLine(["collect", "status", `${PROJECT}/capture.journal`, "--json"])).stdout) as unknown;
  await waitFor(async () => expect(await journalOf()).toMatchObject({ received: 1, acknowledged: 1 }));
  await journey.crash();
  expect(await accepts(bound)).toBe(false);

  // Reopened, the window goes back to the project and starts nothing; the
  // journal is read, never resumed.
  const before = journey.calls.length;
  await journey.launch();
  await press(user, await screen.findByRole("button", { name: "Reopen session" }));
  expect(await within(region("Workspace")).findByText(journey.path(PROJECT), { selector: ".root" })).toBeTruthy();
  const reopened = await openCapture(user);
  await press(user, reopened.getByRole("tab", { name: "MLLP collect" }));
  await tabTo(user, reopened.getByRole("button", { name: "Reopen journal" }));
  await user.keyboard("{Enter}");
  const journal = await reopened.findByText(/^Journal \w+: received/);
  expect(journal.textContent).toBe("Journal interrupted: received 1, acknowledged 1 · recovery never resumes or resends");
  expect(journey.calls.slice(before).map((call) => call.method)).not.toContain("StartCapture");
  expect(await accepts(bound)).toBe(false);

  // The command line recovers the same journal the same way.
  const status = await journey.commandLine(["collect", "status", `${PROJECT}/capture.journal`, "--json"]);
  expect(status.code).toBe(2);
  const recovered = journey.callsTo("OpenCaptureJournal").at(-1)?.result as { journal?: unknown } | undefined;
  expect(JSON.parse(status.stdout)).toMatchObject({ state: "interrupted", received: 1, acknowledged: 1, recovered: true });
  expect(JSON.parse(status.stdout)).toEqual(recovered?.journal);
});
