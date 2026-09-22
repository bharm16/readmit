import type { RegionId, State } from "./bindings";

// Fixed, bundled copy. Neither evidence nor diagnostic strings are used as
// help lookup keys, URLs, or persisted context.
const screens: Record<RegionId, { title: string; steps: string; guide: string }> = {
  commands: { title: "Start or resume", steps: "Open your workspace folder, or create the synthetic sample in a new folder. The sample needs no activation. Use Commands to move between regions. If setup fails, check the status help below before trying again.", guide: "guided-sample.md" },
  navigation: { title: "Choose evidence", steps: "A folder listing is not verification. Open a case to verify its identity. Unsupported entries are not empty cases. If a saved workspace moved, choose its new folder; never edit a sealed bundle to make it open.", guide: "project.md" },
  evidence: { title: "Read and compare", steps: "Inspect original evidence before making a reproducer or test. Filters can hide occurrences; check excluded and undecided counts. Correlation links and missing-ACK windows describe retained coverage, not proof of loss. Compare like observation boundaries.", guide: "correlate.md" },
  inspector: { title: "Investigate and author", steps: "Values are hidden until explicitly revealed. Choose occurrences, declare initial state and observation boundary, then review assertions before saving a test. A prepared test has sent nothing. A failed assertion differs from an execution error; an incomplete observation cannot prove absence.", guide: "test-authoring.md" },
  privacy: { title: "Control access and disclosure", steps: "The per-operation table below names every destination this build can reach, what it carries, what it takes, and whether it is connected right now — nothing connects on its own. Evidence read, verify and export remain available after expiry. New paid work needs explicit local activation and a valid clock state. Keep credentials in their approved store. Derive a disclosure review from what the workspace really holds, approve its exact identity, and export or protect the result deliberately — a blocked review names every unresolved surface. Support summaries are value-free; review the preview before approving it, and verify any bundle you receive. Hidden values do not make screenshots or notes safe to share.", guide: "redact.md" },
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

export function ContextHelp({ region }: { region: RegionId }) {
  const help = screens[region];
  return <details className="context-help"><summary>Help for this screen: {help.title}</summary><p>{help.steps}</p><details><summary>Message-family recipes and verdicts</summary>
    <p>ADT: retain registration/admission/transfer/discharge/merge evidence, select the lifecycle diagnosis profile, and check assigning authorities before interpreting missing visits.</p>
    <p>SIU: use the synthetic sample to author one-appointment expectations, run the defective baseline, then the fixed receiver. AA alone does not prove the appointment was updated.</p>
    <p>ORM: retain orders and their ACKs, select the order diagnosis profile, and compare placer/filler identifiers and declared acknowledgement stages.</p>
    <p>ORU: retain the originating order and result groups, select the order diagnosis profile, and inspect result status progression and duplicate outputs. An unobserved order is not proof it never existed.</p>
    <p>These are finite fixture profiles, not general HL7 conformance. Unknown versions, unsupported triggers, missing evidence and incomplete observations remain unsupported or undecided.</p>
  </details><p>Offline references: docs/{help.guide} and docs/workflow-help.md in the matching CLI archive or source checkout. Help never opens a network connection.</p></details>;
}
