import { useRef, useState } from "react";
import {
  chooseSyntheticPacketPath,
  generateSyntheticPacket,
  openSyntheticPacket,
  prepareSyntheticRerun,
  type PacketPathResult,
  type SyntheticPacketPathKind,
  type SyntheticPacketResult,
  type SyntheticPacketView,
  type SyntheticRerunResult,
  type SyntheticRerunView,
} from "./bindings";
import { useLifecycle } from "./lifecycle";

/** The one committed scenario `readmit report --scenario` accepts. */
const SCENARIO = "siu-reschedule-v1";
/** The loopback endpoint `readmit report prepare` proposes for manual reruns. */
const DEFAULT_ADDRESS = "127.0.0.1:2575";

type Operation = "choosing" | "generating" | "verifying" | "preparing";

/** The synthetic demonstration packets of the investigation-packet panels:
 * `readmit report`, `report verify` and `report prepare`.
 *
 * Generation runs the one committed scenario against fresh built-in defective
 * and fixed receivers on loopback and seals the packet into a new folder named
 * in the host's save dialog; verification is offline and read-only, over a
 * packet chosen in the folder dialog or the one just generated; preparation
 * writes runnable copies into a new folder outside the packet and never edits
 * it. A synthetic packet is never the person's own evidence, and every view
 * here says so and shows the packet's own limitations. Generation runs under
 * the "synthetic-packet" operation, so a cancel reaches only it; a cancelled
 * or refused generation leaves its folder explicitly incomplete. */
export function SyntheticPackets({ onRefresh }: { onRefresh: () => void }) {
  // A browser takes focus from a control it disables, so a running
  // generation hands it to its Cancel control and every action returns it
  // afterwards to the control that started it — or, when that control has
  // nothing left to do, such as Generate once its folder is used, to the
  // chooser that names the next folder.
  const cancelControl = useRef<HTMLButtonElement>(null);
  const packetChooser = useRef<HTMLButtonElement>(null);
  const rerunChooser = useRef<HTMLButtonElement>(null);
  const lifecycle = useLifecycle<Operation>({
    names: { generating: "synthetic-packet" },
    stops: { generating: cancelControl },
    returns: { generating: packetChooser, preparing: rerunChooser },
  });
  const operation = lifecycle.running;
  const busy = operation !== null;
  const [destination, setDestination] = useState("");
  const [packetPath, setPacketPath] = useState("");
  const [packet, setPacket] = useState<SyntheticPacketResult | null>(null);
  const [address, setAddress] = useState(DEFAULT_ADDRESS);
  const [rerunDestination, setRerunDestination] = useState("");
  const [rerun, setRerun] = useState<SyntheticRerunResult | null>(null);
  const [choice, setChoice] = useState<{ kind: SyntheticPacketPathKind; answer: PacketPathResult } | null>(null);
  const verified = packet?.packet ?? null;

  async function choose(kind: SyntheticPacketPathKind, apply: (path: string) => void) {
    if (busy) return;
    await lifecycle.run("choosing", async () => {
      const answer = await chooseSyntheticPacketPath(kind);
      setChoice(answer.state === "completed" ? null : { kind, answer });
      if (answer.state === "completed" && answer.path) apply(answer.path);
    });
  }

  function selectPacket(path: string) {
    setPacketPath(path);
    setPacket(null);
    setRerun(null);
    setRerunDestination("");
  }

  async function generate() {
    if (busy || !destination) return;
    await lifecycle.run("generating", async () => {
      const answer = await generateSyntheticPacket({ scenario: SCENARIO, destination });
      setPacket(answer);
      setRerun(null);
      setRerunDestination("");
      // Only a sealed packet becomes the packet the section verifies and
      // prepares from; a refused or cancelled folder is not one. A folder
      // generation reached is used either way: a retry is a new folder,
      // never an overwrite. Only a busy window reached nothing.
      if (answer.state === "completed") setPacketPath(destination);
      if (answer.state !== "busy") setDestination("");
      onRefresh();
    });
  }

  async function verify() {
    if (busy || !packetPath) return;
    await lifecycle.run("verifying", async () => {
      setPacket(await openSyntheticPacket(packetPath));
      setRerun(null);
    });
  }

  async function prepare() {
    if (busy || !packetPath || !rerunDestination || !address) return;
    await lifecycle.run("preparing", async () => {
      const answer = await prepareSyntheticRerun({ packet: packetPath, destination: rerunDestination, address });
      setRerun(answer);
      if (answer.state === "completed") setRerunDestination("");
      onRefresh();
    });
  }

  return <section aria-labelledby="synthetic-packets-title" className="run-history">
    <h4 id="synthetic-packets-title">Synthetic sample packets</h4>
    <p>Generate the committed synthetic SIU scenario ({SCENARIO}) against fresh built-in defective and fixed receivers on loopback, verify a packet offline, and prepare runnable copies outside it. A synthetic packet is never your own evidence: every message and ledger identifier in it is invented, and it establishes nothing about a production receiver, a downstream workflow or readiness for patient data.</p>

    <div className="actions">
      <button ref={packetChooser} disabled={busy} onClick={() => void choose("packet-destination", setDestination)}>Choose destination…</button>
      <p className="hint">{destination || "No new packet folder named."}</p>
      <button disabled={busy || !destination} onClick={() => void generate()}>
        {operation === "generating" ? "Generating…" : "Generate sample packet"}
      </button>
      <button ref={cancelControl} disabled={operation !== "generating"} onClick={lifecycle.cancel}>Cancel generation</button>
    </div>
    <div className="actions">
      <button disabled={busy} onClick={() => void choose("packet", selectPacket)}>Browse…</button>
      <p className="hint">{packetPath || "No synthetic packet chosen."}</p>
      <button disabled={busy || !packetPath} onClick={() => void verify()}>Verify packet</button>
    </div>
    <div role="status" aria-live="polite">
      {operation === "generating" ? <p>Generating: the synthetic messages go only to the two built-in receivers this window starts on loopback. Cancellation stops them; a partial folder remains incomplete and cannot be verified as complete.</p> : null}
      {choice && choice.kind !== "rerun-destination" && choice.answer.reason ? <p>{choice.answer.reason}</p> : null}
      {packet?.reason ? <p>{packet.reason}</p> : null}
      {verified ? <SyntheticPacketDetails view={verified} /> : null}
    </div>

    {verified ? <div className="actions">
      <h5>Runnable copies</h5>
      <p>Prepare a separate, writable rerun workspace outside the sealed packet: the same synthetic case, runnable copies of its historical specification for the baseline, post-fix and reintroduced trials, and a target on the loopback address below. Preparing is offline and never edits the packet.</p>
      <label htmlFor="synthetic-rerun-address">Loopback address for manual reruns</label>
      <input id="synthetic-rerun-address" value={address} disabled={busy}
        onChange={(e) => { setAddress(e.target.value); setRerun(null); }} />
      <button ref={rerunChooser} disabled={busy} onClick={() => void choose("rerun-destination", setRerunDestination)}>Choose destination…</button>
      <p className="hint">{rerunDestination || "No folder named for the runnable copies."}</p>
      <button disabled={busy || !rerunDestination || !address} onClick={() => void prepare()}>Prepare copies</button>
      <p className="hint">Preparing runnable copies does not run or send anything.</p>
      <div role="status" aria-live="polite">
        {choice?.kind === "rerun-destination" && choice.answer.reason ? <p>{choice.answer.reason}</p> : null}
        {rerun?.reason ? <p>{rerun.reason}</p> : null}
        {rerun?.rerun ? <SyntheticRerunDetails view={rerun.rerun} /> : null}
      </div>
    </div> : null}
  </section>;
}

function SyntheticPacketDetails({ view }: { view: SyntheticPacketView }) {
  return <div className="run-evidence">
    <p><strong>Synthetic-only demonstration packet — never your own evidence.</strong></p>
    <p>Verified: {view.folder} · identity {view.identity.slice(0, 12)}… · contract {view.schema} · state {view.state} · {view.files} indexed files.</p>
    <p>Scenario {view.scenario} · provenance {view.provenance} · boundary {view.observation_boundary} · {view.input_changed ? "input changed" : "input unchanged"}; {view.receiver_behavior_changed ? "receiver behavior changed" : "receiver behavior unchanged"}.</p>
    <ul>{view.runs.map((run) => <li key={run.path}>{run.path}: {run.status} · {run.receiver_mode} {run.receiver} · {run.ledger_count} ledger {run.ledger_count === 1 ? "record" : "records"}</li>)}</ul>
    <p>The packet's own limitations:</p>
    <ul>{view.limitations.map((limitation) => <li key={limitation}>{limitation}</li>)}</ul>
  </div>;
}

function SyntheticRerunDetails({ view }: { view: SyntheticRerunView }) {
  return <div className="run-evidence">
    <p>Runnable copies prepared in {view.folder}: packet {view.packet_identity.slice(0, 12)}… · no connection opened.</p>
    <p>Trials: baseline (defective), post-fix (fixed), reintroduced (defective) on {view.address} · bindings changed: {view.changed_bindings.join(", ")}; input identity and assertion semantics preserved.</p>
    <p>Synthetic-only: the copies rerun the committed synthetic scenario against the built-in fixture, never your own evidence. Follow the folder's RERUN.md and wait for Listening before each test.</p>
  </div>;
}
