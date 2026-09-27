import { useEffect, useRef, useState } from "react";
import {
  discardProtectedPackage,
  inspectProtectedPackage,
  openProtectedPackage,
  packProtectedPackage,
  readProtection,
  type Artifact,
  type ProtectionDiscardResult,
  type ProtectionPackage,
  type ProtectionPackageResult,
  type ProtectionResult,
} from "./bindings";
import { useLifecycle } from "./lifecycle";

/** Encrypted transfer packages, written, inspected, opened and discarded under
 * a project's encryption controls. The controls themselves are managed in
 * Settings → Security → Encryption. */
export function ProtectionPanel({
  workspace,
  entries,
  onRefresh,
}: {
  workspace: string | null;
  entries: Artifact[];
  onRefresh: () => void;
}) {
  const documents = entries.filter((artifact) => artifact.kind === "protection").map((artifact) => artifact.name);
  const packages = entries.filter((artifact) => artifact.kind === "transfer-package").map((artifact) => artifact.name);
  const packables = entries
    .filter((artifact) => artifact.kind !== "unsupported")
    .map((artifact) => artifact.name);

  const lifecycle = useLifecycle<"reading" | "packing" | "opening" | "discarding">({
    names: { packing: "protect", opening: "protect" },
  });
  // The pack task reads its own protection file, so
  // a control is offered only from the document the pack request names. The
  // read does not hold the panel: choosing another file meanwhile withdraws it.
  const packReads = useLifecycle<"reading">({ background: true });
  const operation = lifecycle.running;
  const busy = operation !== null;

  // Package work.
  const [packDocument, setPackDocument] = useState("");
  // What the facade last answered for the pack task's file, with the file it
  // answered for: a view of another file offers none of its controls.
  const [packView, setPackView] = useState<{ entry: string; result: ProtectionResult } | null>(null);
  const [packControl, setPackControl] = useState("");
  const [packSources, setPackSources] = useState<string[]>([]);
  const [packOutput, setPackOutput] = useState("");
  const [packed, setPacked] = useState<ProtectionPackageResult | null>(null);
  const [selectedPackage, setSelectedPackage] = useState("");
  const [packageView, setPackageView] = useState<ProtectionPackageResult | null>(null);
  const [openResult, setOpenResult] = useState<ProtectionPackageResult | null>(null);
  const [discardOverride, setDiscardOverride] = useState(false);
  // Discarding unlinks files, so it asks first, naming the package, its
  // declared retention and the retention handling chosen for it.
  const [confirmingDiscard, setConfirmingDiscard] = useState(false);
  const keepPackage = useRef<HTMLButtonElement | null>(null);
  const [discarded, setDiscarded] = useState<ProtectionDiscardResult | null>(null);

  const packShown = packView !== null && packView.entry === packDocument ? packView.result : null;
  // Only an active control of the pack task's own file writes a package, so a
  // control retired since it was chosen, or one of another file, is not the
  // one a pack would name.
  const writable = (packShown?.document?.controls ?? []).filter((control) => control.state === "active");
  const chosenControl = writable.some((control) => control.name === packControl) ? packControl : "";

  useEffect(() => {
    if (confirmingDiscard) keepPackage.current?.focus();
  }, [confirmingDiscard]);

  /** Chooses the file the pack task writes under and reads it for its own
   * controls. The control chosen for another file is withdrawn with it. */
  async function choosePackDocument(entry: string) {
    setPackDocument(entry);
    setPackControl("");
    setPackView(null);
    packReads.withdraw();
    if (!workspace || !entry) return;
    await packReads.run("reading", async (current) => {
      const result = await readProtection(workspace, entry);
      if (current()) setPackView({ entry, result });
    });
  }

  async function pack() {
    if (busy || !workspace || !packDocument || !chosenControl) return;
    await lifecycle.run("packing", async () => {
      setPacked(await packProtectedPackage({
        workspace,
        entry: packDocument,
        control: chosenControl,
        sources: packSources,
        ...(packOutput ? { output: packOutput } : {}),
      }));
      onRefresh();
    });
  }

  /** Selects a package, or clears the selection. Whatever was decided about
   * the previous one — its retention handling, a pending discard question —
   * does not carry over. */
  async function inspect(entry: string) {
    setSelectedPackage(entry);
    setPackageView(null);
    setOpenResult(null);
    setDiscarded(null);
    setDiscardOverride(false);
    setConfirmingDiscard(false);
    if (!workspace || !entry) return;
    await lifecycle.run("reading", async () => {
      setPackageView(await inspectProtectedPackage(workspace, entry));
    });
  }

  async function openPackage() {
    if (busy || !workspace || !packDocument || !selectedPackage) return;
    await lifecycle.run("opening", async () => {
      const result = await openProtectedPackage({
        workspace,
        entry: packDocument,
        package: selectedPackage,
      });
      setOpenResult(result);
      // The plaintext output is a new workspace entry: list it.
      if (result.package) onRefresh();
    });
  }

  async function discard() {
    if (busy || !workspace || !selectedPackage) return;
    setConfirmingDiscard(false);
    await lifecycle.run("discarding", async () => {
      setDiscarded(await discardProtectedPackage({
        workspace,
        package: selectedPackage,
        ...(discardOverride ? { override: true } : {}),
      }));
      onRefresh();
    });
  }

  return <section aria-labelledby="protection-title">
    <h3 id="protection-title">Encrypted packages</h3>

    <div className="actions" role="group" aria-labelledby="protection-packing-title">
      <h4 id="protection-packing-title">Encrypted transfer packages</h4>
      <label htmlFor="protection-pack-document">Protection file</label>
      <select id="protection-pack-document" value={packDocument} disabled={busy}
        onChange={(e) => void choosePackDocument(e.target.value)}>
        <option value="">Select a protection file…</option>
        {documents.map((name) => <option key={name} value={name}>{name}</option>)}
        {packDocument && !documents.includes(packDocument) ? <option value={packDocument}>{packDocument}</option> : null}
      </select>
      {packShown?.reason ? <p>{packShown.reason}</p> : null}
      <label htmlFor="protection-pack-control">Protection control</label>
      <select id="protection-pack-control" value={chosenControl} disabled={busy} aria-describedby="protection-pack-control-hint"
        onChange={(e) => setPackControl(e.target.value)}>
        <option value="">Select a control…</option>
        {writable.map((control) => (
          <option key={control.name} value={control.name}>{control.name}</option>
        ))}
      </select>
      <p id="protection-pack-control-hint" className="hint">The active controls of the protection file chosen for this package.</p>
      <label htmlFor="protection-pack-sources">Package contents</label>
      <select id="protection-pack-sources" value="" disabled={busy} aria-describedby="protection-pack-sources-hint"
        onChange={(e) => {
          if (e.target.value) setPackSources((current) => current.includes(e.target.value) ? current : [...current, e.target.value]);
        }}>
        <option value="">Add a workspace entry…</option>
        {packables.map((name) => <option key={name} value={name}>{name}</option>)}
      </select>
      <p id="protection-pack-sources-hint" className="hint">
        Each entry is copied into the package, never moved. Removing one from the package only changes this list; the workspace
        entry stays where it is.
      </p>
      {packSources.length > 0 ? <ul>{packSources.map((name) => <li key={name}>
        {name}{" "}
        <button type="button" aria-label={`Remove from package ${name}`} disabled={busy}
          onClick={() => setPackSources((current) => current.filter((source) => source !== name))}>
          Remove from package
        </button>
      </li>)}</ul> : null}
      <label htmlFor="protection-pack-output">Package folder</label>
      <input id="protection-pack-output" value={packOutput} disabled={busy} placeholder="generated at pack"
        aria-describedby="protection-pack-output-hint"
        onChange={(e) => setPackOutput(e.target.value)} />
      <p id="protection-pack-output-hint" className="hint">A new folder the encrypted package is written to.</p>
      <button disabled={busy || !packDocument || !chosenControl || packSources.length === 0} onClick={() => void pack()}>
        {operation === "packing" ? "Packing…" : "Create encrypted package"}
      </button>
      <button disabled={operation !== "packing"} onClick={lifecycle.cancel}>Cancel packing</button>
      <p className="hint">Encrypting a package is not approval to disclose it; moving it anywhere is a separate deliberate act.</p>
      {packed?.reason ? <p>{packed.reason}</p> : null}
      {packed?.package ? <PackageView view={packed.package} limitations={packed.limitations} /> : null}
    </div>

    <div className="actions">
      <h4>Packages</h4>
      <label htmlFor="protection-package">Transfer package</label>
      <select id="protection-package" value={selectedPackage} disabled={busy}
        onChange={(e) => void inspect(e.target.value)}>
        <option value="">Select a transfer package…</option>
        {packages.map((name) => <option key={name} value={name}>{name}</option>)}
      </select>
      {packageView?.reason ? <p>{packageView.reason}</p> : null}
      {packageView?.package ? <PackageView view={packageView.package} limitations={packageView.limitations} /> : null}
      {packageView?.package ? <>
        <button disabled={busy || !packDocument} onClick={() => void openPackage()}>
          {operation === "opening" ? "Opening…" : "Open package"}
        </button>
        <button disabled={operation !== "opening"} onClick={lifecycle.cancel}>Cancel opening</button>
        {openResult?.reason ? <p>{openResult.reason}</p> : null}
        {openResult?.package ? <p>Opened into <strong>{openResult.package.entry}</strong>. The output is plaintext in this workspace, in local custody. Opening ended the protection the package carried: the decrypted output is protected by this machine's own storage control and an owner-only mode, and by nothing else.</p> : null}
        <label htmlFor="protection-discard-override">Retention handling</label>
        <select id="protection-discard-override" value={discardOverride ? "override" : "declared"} disabled={busy}
          aria-describedby="protection-discard-override-hint"
          onChange={(e) => { setDiscardOverride(e.target.value === "override"); setConfirmingDiscard(false); }}>
          <option value="declared">Use declared retention</option>
          <option value="override">Override retention</option>
        </select>
        <p id="protection-discard-override-hint" className="hint">
          Override retention discards the package even before its declared retention ends. It is a deliberate choice for this
          package only, never the ordinary one.
        </p>
        {confirmingDiscard && packageView.package.entry === selectedPackage ? (
          <span
            role="group"
            aria-label={`Discard ${selectedPackage}?`}
            onKeyDown={(event) => {
              if (event.key === "Escape" && !event.nativeEvent.isComposing && !busy) {
                event.preventDefault();
                event.stopPropagation();
                setConfirmingDiscard(false);
              }
            }}
          >
            <span className="hint">
              {" "}Discard {selectedPackage}? Declared retention: {packageView.package.retention}
              {packageView.package.retain_until ? ` until ${packageView.package.retain_until}` : ""}. Retention handling:{" "}
              {discardOverride ? "Override retention" : "Use declared retention"}. Discarding unlinks the files this package
              declares; unlinking is not erasure, and a copy already moved elsewhere is untouched.
            </span>
            <button type="button" disabled={busy} onClick={() => void discard()}>Discard it</button>
            <button type="button" ref={keepPackage} disabled={busy} onClick={() => setConfirmingDiscard(false)}>Keep package</button>
          </span>
        ) : (
          <button disabled={busy || !selectedPackage} onClick={() => setConfirmingDiscard(true)}>
            {operation === "discarding" ? "Discarding…" : "Discard package"}
          </button>
        )}
        {discarded?.reason ? <p>{discarded.reason}</p> : null}
        {discarded?.removed !== undefined && discarded.removed !== null && discarded.removed > 0 ? <p>Unlinked {discarded.removed} declared files. Removal is not erasure; the result says exactly what it does not establish.</p> : null}
      </> : null}
    </div>
  </section>;
}

/** One package as its descriptor declares it, without a key. The packed names
 * and sizes are inside the encrypted index and are not facts a view can carry. */
function PackageView({ view, limitations }: { view: ProtectionPackage; limitations: string[] }) {
  return <div className="preflight">
    <p>Package <strong>{view.entry}</strong> · control {view.control} · key generation {view.generation} · {view.entries} entries{view.not_read ? ` · ${view.not_read} not read` : ""}.</p>
    <p>Written {view.created_at} · {view.cipher} with {view.derivation} · retention {view.retention}{view.retain_until ? ` until ${view.retain_until}` : ""}.</p>
    <ul>{limitations.map((limitation) => <li key={limitation}>{limitation}</li>)}</ul>
  </div>;
}
