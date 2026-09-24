import { useEffect, useRef, useState } from "react";
import {
  discardProtectedPackage,
  inspectProtectedPackage,
  openProtectedPackage,
  packProtectedPackage,
  readProtection,
  retireProtectionControl,
  rotateProtectionControl,
  saveProtectionControl,
  type Artifact,
  type ProtectionControl,
  type ProtectionControlRequest,
  type ProtectionDiscardResult,
  type ProtectionPackage,
  type ProtectionPackageResult,
  type ProtectionResult,
} from "./bindings";
import { useLifecycle } from "./lifecycle";

/** The protection screen: controls that reference keys readmit never holds,
 * and the encrypted transfer packages written, inspected, opened and discarded
 * under them.
 *
 * Nothing here is this window's own answer. Registering a control is `protect
 * register` writing a reference — the absolute path of the program that prints
 * the key and the locator arguments that select it — and every package is
 * `protect pack`, `inspect`, `open` and `discard` over that control. Key
 * material never crosses into the window: the views show the one mask, the
 * locator arguments are counted rather than echoed, and a rotation is recorded
 * only after the declared store answers. Recipient and authority facts are the
 * package's own — the control, the generation, the declared retention — shown
 * beside what encryption does not establish: not source authentication, not
 * revocation, and deletion is not erasure. Packing a package writes it here;
 * moving it anywhere is somebody's separate deliberate act. */
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

  // Control lifecycle. The document on screen is what the facade last answered
  // for it: the read, or the document a registration, rotation or retirement
  // then wrote. A refusal leaves it as it was.
  const [documentEntry, setDocumentEntry] = useState("");
  const [documentView, setDocumentView] = useState<ProtectionResult | null>(null);
  const [newDocument, setNewDocument] = useState("");
  const lifecycle = useLifecycle<"reading" | "registering" | "rotating" | "retiring" | "packing" | "discarding">({
    names: { packing: "protect" },
  });
  const operation = lifecycle.running;
  const busy = operation !== null;

  const [newName, setNewName] = useState("");
  const [newStorage, setNewStorage] = useState("os-volume-encryption");
  const [newCommand, setNewCommand] = useState("");
  const [newArgument, setNewArgument] = useState("");
  const [newArguments, setNewArguments] = useState<string[]>([]);
  const [newMaxAge, setNewMaxAge] = useState("");
  const [newRetain, setNewRetain] = useState("");
  // The last change to one control and, once the facade answered, its answer.
  const [lastChange, setLastChange] = useState<{ action: ControlAction; name: string; result?: ProtectionResult } | null>(null);
  // Retiring cannot be undone, so it asks first. The one control whose
  // retirement is waiting for the person's answer, and where focus goes once
  // the window has answered: back to the Retire control a kept control still
  // offers, or to the panel's heading once there is none.
  const [confirming, setConfirming] = useState<string | null>(null);
  const [returning, setReturning] = useState<string | null>(null);
  const heading = useRef<HTMLHeadingElement | null>(null);
  const keep = useRef<HTMLButtonElement | null>(null);
  const retireControls = useRef(new Map<string, HTMLButtonElement>());

  // Package work.
  const [packDocument, setPackDocument] = useState("");
  const [packControl, setPackControl] = useState("");
  const [packSources, setPackSources] = useState<string[]>([]);
  const [packOutput, setPackOutput] = useState("");
  const [packed, setPacked] = useState<ProtectionPackageResult | null>(null);
  const [selectedPackage, setSelectedPackage] = useState("");
  const [packageView, setPackageView] = useState<ProtectionPackageResult | null>(null);
  const [openResult, setOpenResult] = useState<ProtectionPackageResult | null>(null);
  const [discardOverride, setDiscardOverride] = useState(false);
  const [discarded, setDiscarded] = useState<ProtectionDiscardResult | null>(null);

  const controls: ProtectionControl[] = documentView?.document?.controls ?? [];
  // Only an active control writes a package, so a control retired since it was
  // chosen is no longer the one a pack would name.
  const writable = controls.filter((control) => control.state === "active");
  const chosenControl = writable.some((control) => control.name === packControl) ? packControl : "";

  // A question about a control the document no longer holds active has
  // nothing left to ask.
  const confirmable = confirming !== null && writable.some((control) => control.name === confirming);
  useEffect(() => {
    if (confirming !== null && !confirmable) setConfirming(null);
  }, [confirming, confirmable]);

  useEffect(() => {
    if (confirming !== null) keep.current?.focus();
  }, [confirming]);

  useEffect(() => {
    if (returning === null || busy) return;
    const control = retireControls.current.get(returning);
    (control && !control.disabled ? control : heading.current)?.focus();
    setReturning(null);
  }, [busy, returning, documentView]);

  /** Records what the facade answered for one control, and shows the document
   * it wrote in place of the one it replaced. */
  function showChange(action: ControlAction, name: string, result: ProtectionResult) {
    setLastChange({ action, name, result });
    if (result.document) setDocumentView(result);
  }

  async function show(entry: string) {
    setDocumentEntry(entry);
    setDocumentView(null);
    setLastChange(null);
    setConfirming(null);
    if (!workspace || !entry) return;
    await lifecycle.run("reading", async () => {
      setDocumentView(await readProtection(workspace, entry));
    });
  }

  async function register() {
    if (busy || !workspace || !documentEntry) return;
    const request: ProtectionControlRequest = {
      workspace,
      entry: documentEntry,
      name: newName,
      storage: newStorage,
      command: newCommand,
      arguments: newArguments,
      ...(newMaxAge ? { max_age: newMaxAge } : {}),
      ...(newRetain ? { retain: newRetain } : {}),
    };
    await lifecycle.run("registering", async () => {
      showChange("register", newName, await saveProtectionControl(request));
      onRefresh();
    });
  }

  async function rotate(name: string) {
    if (busy || !workspace || !documentEntry) return;
    await lifecycle.run("rotating", async () => {
      showChange("rotate", name, await rotateProtectionControl(workspace, documentEntry, name));
    });
  }

  async function retire(name: string) {
    if (busy || !workspace || !documentEntry) return;
    setConfirming(null);
    setLastChange({ action: "retire", name });
    await lifecycle.run("retiring", async () => {
      try {
        showChange("retire", name, await retireProtectionControl(workspace, documentEntry, name));
      } finally {
        setReturning(name);
      }
    });
  }

  function keepActive(name: string) {
    setConfirming(null);
    setReturning(name);
  }

  async function pack() {
    if (busy || !workspace) return;
    await lifecycle.run("packing", async () => {
      setPacked(await packProtectedPackage({
        workspace,
        entry: packDocument || documentEntry,
        control: chosenControl,
        sources: packSources,
        ...(packOutput ? { output: packOutput } : {}),
      }));
      onRefresh();
    });
  }

  async function inspect(entry: string) {
    if (!workspace || !entry) return;
    await lifecycle.run("reading", async () => {
      setSelectedPackage(entry);
      setOpenResult(null);
      setDiscarded(null);
      setPackageView(await inspectProtectedPackage(workspace, entry));
    });
  }

  async function openPackage() {
    if (busy || !workspace || !documentEntry || !selectedPackage) return;
    await lifecycle.run("packing", async () => {
      setOpenResult(await openProtectedPackage({
        workspace,
        entry: documentEntry,
        package: selectedPackage,
      }));
    });
  }

  async function discard() {
    if (busy || !workspace || !selectedPackage) return;
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
    <h3 id="protection-title" ref={heading} tabIndex={-1}>Protection</h3>
    <p>
      Encrypt evidence into transfer packages under a control whose key stays in an operating system or
      customer-managed store. readmit holds no key material: a control registers a reference, and the key
      exists only inside the one operation that reads it. A package's recipient and authority facts are the
      control and generation it names and the retention it declares — and none of it is source
      authentication, revocation or erasure.
    </p>

    <div className="actions">
      <h4>Protection document</h4>
      <label htmlFor="protection-document">Document entry</label>
      <select id="protection-document" value={documentEntry} disabled={busy}
        onChange={(e) => void show(e.target.value)}>
        <option value="">Select a protection document…</option>
        {documents.map((name) => <option key={name} value={name}>{name}</option>)}
        {documentEntry && !documents.includes(documentEntry) ? <option value={documentEntry}>{documentEntry}</option> : null}
      </select>
      <label htmlFor="protection-new-document">New protection document</label>
      <input id="protection-new-document" value={newDocument} disabled={busy} placeholder="protection.json"
        onChange={(e) => setNewDocument(e.target.value)} />
      <button type="button" disabled={busy || newDocument === ""} onClick={() => { void show(newDocument); setNewDocument(""); }}>
        Use this document
      </button>
      {documentView?.reason ? <p>{documentView.reason}</p> : null}
      {documentView?.document && controls.length === 0 ? <p>No control is registered in {documentView.document.entry} yet. Registering the first one writes it.</p> : null}
      {controls.length > 0 ? <div className="review-scroll">
        <table>
          <caption>Every registered control. The key is masked because it was never read here, and the
            locator arguments are counted rather than echoed.</caption>
          <thead>
            <tr>
              <th scope="col">Name</th>
              <th scope="col">Storage (declared, never verified)</th>
              <th scope="col">State</th>
              <th scope="col">Generation</th>
              <th scope="col">Rotation</th>
              <th scope="col">Retain</th>
              <th scope="col">Key</th>
              <th scope="col">Actions</th>
            </tr>
          </thead>
          <tbody>
            {controls.map((control) => <tr key={control.name}>
              <td>{control.name}</td>
              <td>{control.storage}</td>
              <td>{control.state}</td>
              <td>{control.generation}</td>
              <td>{control.rotation}</td>
              <td>{control.retain || "not declared"}</td>
              <td>{control.key} · {control.locator_arguments} locator arguments</td>
              <td>
                {confirming === control.name ? (
                  <span
                    role="group"
                    aria-label={`Retire ${control.name}?`}
                    onKeyDown={(event) => {
                      // Escape answers this question and goes no further: the
                      // window's own Escape cancels a running operation.
                      if (event.key === "Escape" && !event.nativeEvent.isComposing && !busy) {
                        event.preventDefault();
                        event.stopPropagation();
                        keepActive(control.name);
                      }
                    }}
                  >
                    <span className="hint"> Retire {control.name}? It writes no new package and still opens the packages it wrote. No command makes a retired control active again.</span>
                    <button type="button" disabled={busy} onClick={() => void retire(control.name)}>Retire it</button>
                    <button type="button" ref={keep} disabled={busy} onClick={() => keepActive(control.name)}>Keep it active</button>
                  </span>
                ) : (
                  <button
                    type="button"
                    aria-label={`Retire ${control.name}`}
                    disabled={busy || control.state !== "active"}
                    ref={(button) => {
                      if (button) retireControls.current.set(control.name, button);
                      else retireControls.current.delete(control.name);
                    }}
                    onClick={() => setConfirming(control.name)}
                  >
                    Retire
                  </button>
                )}
                <button disabled={busy} onClick={() => void rotate(control.name)}>
                  {operation === "rotating" ? "Rotating…" : "Record rotation"}
                </button>
              </td>
            </tr>)}
          </tbody>
        </table>
      </div> : null}
      <div role="status" aria-live="polite">
        {lastChange && lastChange.action !== "register" ? <ControlChange action={lastChange.action} name={lastChange.name} result={lastChange.result} /> : null}
      </div>
      {documentView?.document ? <ul>{documentView.document.limitations.map((limitation) => <li key={limitation}>{limitation}</li>)}</ul> : null}

      <h4>Register a control</h4>
      <label htmlFor="protection-name">Control name</label>
      <input id="protection-name" value={newName} disabled={busy} onChange={(e) => setNewName(e.target.value)} />
      <label htmlFor="protection-storage">Declared at-rest storage</label>
      <select id="protection-storage" value={newStorage} disabled={busy}
        onChange={(e) => setNewStorage(e.target.value)}>
        <option value="os-volume-encryption">os-volume-encryption</option>
        <option value="customer-key">customer-key</option>
        <option value="none-declared">none-declared</option>
      </select>
      <label htmlFor="protection-command">Absolute path of the program that prints the key</label>
      <input id="protection-command" value={newCommand} disabled={busy} onChange={(e) => setNewCommand(e.target.value)} />
      <label htmlFor="protection-argument">One locator argument (never key material)</label>
      <input id="protection-argument" value={newArgument} disabled={busy}
        onChange={(e) => setNewArgument(e.target.value)} />
      <button type="button" disabled={busy || newArgument === ""} onClick={() => { setNewArguments((current) => [...current, newArgument]); setNewArgument(""); }}>
        Add this locator argument
      </button>
      {newArguments.length > 0 ? <ol>{newArguments.map((argument, at) => <li key={String(at)}>{argument}</li>)}</ol> : null}
      <label htmlFor="protection-max-age">Rotation stays current for (Go duration, optional)</label>
      <input id="protection-max-age" value={newMaxAge} disabled={busy} placeholder="720h" onChange={(e) => setNewMaxAge(e.target.value)} />
      <label htmlFor="protection-retain">Packages declare retention of (Go duration, optional)</label>
      <input id="protection-retain" value={newRetain} disabled={busy} placeholder="2160h" onChange={(e) => setNewRetain(e.target.value)} />
      <button disabled={busy || !documentEntry || !newName || !newCommand} onClick={() => void register()}>
        {operation === "registering" ? "Registering…" : "Register this control"}
      </button>
      {lastChange?.action === "register" && lastChange.result?.reason ? <p>{lastChange.result.reason}</p> : null}
      {lastChange?.action === "register" && lastChange.result?.document ? <p>Registered. Registering a reference proves nothing about the store behind it: a rotation the store answers for is what records a generation.</p> : null}
    </div>

    <div className="actions">
      <h4>Encrypted transfer packages</h4>
      <label htmlFor="protection-pack-document">Control document to write under</label>
      <select id="protection-pack-document" value={packDocument || documentEntry} disabled={busy}
        onChange={(e) => setPackDocument(e.target.value)}>
        <option value="">Same as the document above</option>
        {documents.map((name) => <option key={name} value={name}>{name}</option>)}
      </select>
      <label htmlFor="protection-pack-control">Control</label>
      <select id="protection-pack-control" value={chosenControl} disabled={busy}
        onChange={(e) => setPackControl(e.target.value)}>
        <option value="">Select a control…</option>
        {writable.map((control) => (
          <option key={control.name} value={control.name}>{control.name}</option>
        ))}
      </select>
      <label htmlFor="protection-pack-sources">Entries to pack (copied, never moved)</label>
      <select id="protection-pack-sources" value="" disabled={busy}
        onChange={(e) => {
          if (e.target.value) setPackSources((current) => current.includes(e.target.value) ? current : [...current, e.target.value]);
        }}>
        <option value="">Add a workspace entry…</option>
        {packables.map((name) => <option key={name} value={name}>{name}</option>)}
      </select>
      {packSources.length > 0 ? <ul>{packSources.map((name) => <li key={name}>{name}</li>)}</ul> : null}
      <label htmlFor="protection-pack-output">New package folder</label>
      <input id="protection-pack-output" value={packOutput} disabled={busy} placeholder="generated at pack"
        onChange={(e) => setPackOutput(e.target.value)} />
      <button disabled={busy || (!packDocument && !documentEntry) || !chosenControl || packSources.length === 0} onClick={() => void pack()}>
        {operation === "packing" ? "Packing…" : "Pack protected package"}
      </button>
      <button disabled={operation !== "packing"} onClick={lifecycle.cancel}>Cancel packing</button>
      {packed?.reason ? <p>{packed.reason}</p> : null}
      {packed?.package ? <PackageView view={packed.package} limitations={packed.limitations} /> : null}
    </div>

    <div className="actions">
      <h4>Packages of this workspace</h4>
      <label htmlFor="protection-package">Transfer package</label>
      <select id="protection-package" value={selectedPackage} disabled={busy}
        onChange={(e) => void inspect(e.target.value)}>
        <option value="">Select a transfer package…</option>
        {packages.map((name) => <option key={name} value={name}>{name}</option>)}
      </select>
      {packageView?.reason ? <p>{packageView.reason}</p> : null}
      {packageView?.package ? <PackageView view={packageView.package} limitations={packageView.limitations} /> : null}
      {packageView?.package ? <>
        <button disabled={busy || !documentEntry} onClick={() => void openPackage()}>
          {operation === "packing" ? "Opening…" : "Open with the control above"}
        </button>
        {openResult?.reason ? <p>{openResult.reason}</p> : null}
        {openResult?.package ? <p>Opened into <strong>{openResult.package.entry}</strong>. Opening ended the protection the package carried: the decrypted output is protected by this machine's own storage control and an owner-only mode, and by nothing else.</p> : null}
        <label htmlFor="protection-discard-override">Declared retention override</label>
        <select id="protection-discard-override" value={discardOverride ? "override" : "declared"} disabled={busy}
          onChange={(e) => setDiscardOverride(e.target.value === "override")}>
          <option value="declared">Respect the declared retention</option>
          <option value="override">Override the declared retention</option>
        </select>
        <button disabled={busy || !selectedPackage} onClick={() => void discard()}>
          {operation === "discarding" ? "Discarding…" : "Discard this package"}
        </button>
        {discarded?.reason ? <p>{discarded.reason}</p> : null}
        {discarded?.removed !== undefined && discarded.removed !== null && discarded.removed > 0 ? <p>Unlinked {discarded.removed} declared files. Removal is not erasure; the result says exactly what it does not establish.</p> : null}
      </> : null}
    </div>
  </section>;
}

/** What the panel changes about one registered control. */
type ControlAction = "register" | "rotate" | "retire";

/** What a rotation or a retirement did to one control, or its refusal. A
 * retirement is not revocation, and the sentence says so. */
function ControlChange({ action, name, result }: { action: Exclude<ControlAction, "register">; name: string; result: ProtectionResult | undefined }) {
  if (!result) return action === "retire" ? <p>Retiring {name}.</p> : null;
  if (result.reason) return <p>{result.reason}</p>;
  if (!result.document) return null;
  return action === "retire"
    ? <p>Retired {name}: it writes no new package and still opens the packages it wrote. Retirement is not revocation: a recipient who already has a package keeps it.</p>
    : <p>Recorded a rotation of {name}. readmit never read the previous key, so a recorded rotation is an assertion, not a verification.</p>;
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
