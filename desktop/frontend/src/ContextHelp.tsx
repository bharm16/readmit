import type { RegionId, State } from "./bindings";

// Fixed, bundled copy. Neither evidence nor diagnostic strings are used as
// help lookup keys, URLs, or persisted context.
type Topic = "search" | RegionId | "privacy";
const screens: Record<Topic, { title: string; steps: string; guide: string }> = {
  search: { title: "Search and commands", steps: "Search finds the cases, registered case details and indexed message content of the open project. Ctrl+K (⌘K on a Mac) lists every command the window offers, with its shortcut.", guide: "guided-sample.md" },
  navigation: { title: "Moving around", steps: "Projects lists the projects you opened recently. With a project open, Cases, Tests, Runs, Environments and Reports hold its work; Tools, Settings and Help are always there. F6 moves between the sidebar, the page and the details of a selection. If a saved project moved, open its new folder; never edit a sealed bundle to make it open.", guide: "project.md" },
  evidence: { title: "Cases and messages", steps: "Opening a case verifies it; unsupported entries are not empty cases. Inspect original evidence before making a reproducer or test. Filters can hide messages, so check the excluded and undecided counts. Correlation links and missing-ACK windows describe retained coverage, not proof of loss. Compare like observation boundaries.", guide: "correlate.md" },
  inspector: { title: "Message details and tests", steps: "Values are hidden until you choose to show them. Choose messages, declare the initial state and observation boundary, then review assertions before saving a test. A prepared test has sent nothing. A failed assertion differs from an execution error; an incomplete observation cannot prove absence.", guide: "test-authoring.md" },
  privacy: { title: "Privacy, connections and license", steps: "An operation shows its progress where it runs. Settings › Security lists every destination this build can reach, what it carries and whether it is connected right now; nothing connects on its own. Reading, verifying and exporting stay available after a license expires; new work needs an activated license and a valid clock. Keep credentials in their approved store. Derive a disclosure review from what the project really holds and approve its exact identity before exporting. Hidden values do not make screenshots or notes safe to share.", guide: "redact.md" },
};

const states: Record<State, { code: string; action: string }> = {
  empty: { code: "RM-EMPTY", action: "Choose an input or workspace first. An empty list or filter result is not proof that the source contains no records." },
  busy: { code: "RM-BUSY", action: "Wait, or use this operation's Cancel control when available. Cancellation cannot undo bytes already sent. Do not start a duplicate send." },
  cancelled: { code: "RM-CANCELLED", action: "No completed result is implied. Keep any partial output. Inspect durable-run recovery before retrying network work; an uncertain send must not be repeated automatically. For a read-only operation, reopen the input and retry." },
  failed: { code: "RM-FAILED", action: "Read the local reason. Check the selected artifact type/version, completeness and identity, explicit configuration and destination. For writes choose a new writable output outside sealed evidence. Preserve partial output; never add a completion marker or disable verification. Retry read-only work after correcting its cause." },
  permission_denied: { code: "RM-PERMISSION", action: "Ask the workspace owner for the minimum required access: read/traverse input folders and write access to a new output parent. On Windows check inherited ACLs; on macOS check folder access. Check configured credential references and application roles separately. Do not run as administrator or broaden access to everyone as a shortcut." },
  completed: { code: "RM-COMPLETED", action: "This operation completed; it does not establish clinical correctness. Preparation is not execution. Read the test verdict separately: failed, undecided, skipped and execution error are never a pass. Only a completed observation window can prove an empty state within its declared scope." },
};

export function StateHelp({ state }: { state: State }) {
  const help = states[state];
  return <details className="context-help"><summary>Help: {help.code}</summary><p>{help.action}</p><p>Offline reference: docs/workflow-help.md → {help.code}. Share the code and operation name, not the private diagnostic text.</p></details>;
}

/** Every help topic, as the Help page shows them: one per area of the
 * window, the HL7 guidance and where the offline references live. */
export function HelpTopics() {
  return (
    <>
      <div className="help-topics">
        {(Object.keys(screens) as Topic[]).map((region) => (
          <section key={region} className="help-topic" aria-labelledby={`help-${region}`}>
            <h3 id={`help-${region}`}>{screens[region].title}</h3>
            <p>{screens[region].steps}</p>
            <p className="hint">Reference: docs/{screens[region].guide}</p>
          </section>
        ))}
      </div>
      <section className="help-topic" aria-labelledby="help-hl7" style={{ marginTop: "1rem" }}>
        <h3 id="help-hl7">HL7 guidance</h3>
        <p>ADT: retain registration/admission/transfer/discharge/merge evidence, select the lifecycle diagnosis profile, and check assigning authorities before interpreting missing visits.</p>
        <p>SIU: use the demo to author one-appointment expectations, run the defective baseline, then the fixed receiver. AA alone does not prove the appointment was updated.</p>
        <p>ORM: retain orders and their ACKs, select the order diagnosis profile, and compare placer/filler identifiers and declared acknowledgement stages.</p>
        <p>ORU: retain the originating order and result groups, select the order diagnosis profile, and inspect result status progression and duplicate outputs. An unobserved order is not proof it never existed.</p>
        <p>These are finite fixture profiles, not general HL7 conformance. Unknown versions, unsupported triggers, missing evidence and incomplete observations remain unsupported or undecided.</p>
        <p className="hint">Offline references: docs/workflow-help.md in the matching CLI archive or source checkout. Help never opens a network connection.</p>
      </section>
    </>
  );
}
