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
  const lifecycle = useLifecycle<"reading" | "registering" | "rotating" | "retiring" | "packing" | "opening" | "discarding">({
    names: { packing: "protect", opening: "protect" },
  });
  // The pack task reads its own protection file, apart from the one above, so
  // a control is offered only from the document the pack request names. The
  // read does not hold the panel: choosing another file meanwhile withdraws it.
  const packReads = useLifecycle<"reading">({ background: true });
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

  const controls: ProtectionControl[] = documentView?.document?.controls ?? [];
  const packShown = packView !== null && packView.entry === packDocument ? packView.result : null;
  // Only an active control of the pack task's own file writes a package, so a
  // control retired since it was chosen, or one of another file, is not the
  // one a pack would name.
  const writable = (packShown?.document?.controls ?? []).filter((control) => control.state === "active");
  const chosenControl = writable.some((control) => control.name === packControl) ? packControl : "";

  // A question about a control the document no longer holds active has
  // nothing left to ask.
  const confirmable = confirming !== null && controls.some((control) => control.name === confirming && control.state === "active");
  useEffect(() => {
    if (confirming !== null && !confirmable) setConfirming(null);
  }, [confirming, confirmable]);

  useEffect(() => {
    if (confirming !== null) keep.current?.focus();
  }, [confirming]);

  useEffect(() => {
    if (confirmingDiscard) keepPackage.current?.focus();
  }, [confirmingDiscard]);

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
    if (!result.document) return;
    setDocumentView(result);
    // The pack task's view of the same file is what was just written there.
    if (documentEntry !== "" && documentEntry === packDocument) {
      packReads.withdraw();
      setPackView({ entry: packDocument, result });
    }
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
    if (busy || !workspace || !documentEntry || !selectedPackage) return;
    await lifecycle.run("opening", async () => {
      const result = await openProtectedPackage({
        workspace,
        entry: documentEntry,
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
    <h3 id="protection-title" ref={heading} tabIndex={-1}>Protection</h3>
    <p>
      Encrypt evidence into transfer packages under a control whose key stays in an operating system or
      customer-managed store. readmit holds no key material: a control registers a reference, and the key
      exists only inside the one operation that reads it. A package's recipient and authority facts are the
      control and generation it names and the retention it declares — and none of it is source
      authentication, revocation or erasure.
    </p>

    <div className="actions" role="group" aria-labelledby="protection-document-title">
      <h4 id="protection-document-title">Protection document</h4>
      <label htmlFor="protection-document">Protection file</label>
      <select id="protection-document" value={documentEntry} disabled={busy}
        onChange={(e) => void show(e.target.value)}>
        <option value="">Select a protection document…</option>
        {documents.map((name) => <option key={name} value={name}>{name}</option>)}
        {documentEntry && !documents.includes(documentEntry) ? <option value={documentEntry}>{documentEntry}</option> : null}
      </select>
      <label htmlFor="protection-new-document">New protection file</label>
      <input id="protection-new-document" value={newDocument} disabled={busy} placeholder="protection.json"
        aria-describedby="protection-new-document-hint"
        onChange={(e) => setNewDocument(e.target.value)} />
      <p id="protection-new-document-hint" className="hint">
        Selecting a file that does not exist yet writes nothing and registers no control: registering its first control creates it.
      </p>
      <button type="button" disabled={busy || newDocument === ""} onClick={() => { void show(newDocument); setNewDocument(""); }}>
        Select file
      </button>
      {documentView?.reason ? <p>{documentView.reason}</p> : null}
      {documentView?.document && controls.length === 0 ? <p>No control is registered in {documentView.document.entry} yet. Registering the first one writes it.</p> : null}
      {controls.length > 0 ? <div className="review-scroll">
        <table>
          <caption>Every registered control. The key is masked because it was never read here, and the
            locator arguments are counted rather than echoed.</caption>
          <thead>
            <tr>
              <th scope="col">Control name</th>
              <th scope="col">Declared storage <span className="hint">(never verified)</span></th>
              <th scope="col">State</th>
              <th scope="col">Key generation</th>
              <th scope="col">Rotation status</th>
              <th scope="col">Retention period</th>
              <th scope="col">Key reference</th>
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
                    <button type="button" ref={keep} disabled={busy} onClick={() => keepActive(control.name)}>Keep active</button>
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

      <h4>Add control</h4>
      <label htmlFor="protection-name">Control name</label>
      <input id="protection-name" value={newName} disabled={busy} onChange={(e) => setNewName(e.target.value)} />
      <label htmlFor="protection-storage">Declared storage protection</label>
      <select id="protection-storage" value={newStorage} disabled={busy} aria-describedby="protection-storage-hint"
        onChange={(e) => setNewStorage(e.target.value)}>
        <option value="os-volume-encryption">OS volume encryption</option>
        <option value="customer-key">Customer-managed key</option>
        <option value="none-declared">None declared</option>
      </select>
      <p id="protection-storage-hint" className="hint">A declaration about where the key is stored at rest; readmit never verifies it.</p>
      <label htmlFor="protection-command">Key lookup program</label>
      <input id="protection-command" value={newCommand} disabled={busy} aria-describedby="protection-command-hint"
        onChange={(e) => setNewCommand(e.target.value)} />
      <p id="protection-command-hint" className="hint">
        The absolute path of a program that prints the key when it runs with the lookup arguments. readmit records this
        reference only; never enter key material here.
      </p>
      <label htmlFor="protection-argument">Lookup argument</label>
      <input id="protection-argument" value={newArgument} disabled={busy} aria-describedby="protection-argument-hint"
        onChange={(e) => setNewArgument(e.target.value)} />
      <p id="protection-argument-hint" className="hint">One argument that selects the key in its store, never key material itself.</p>
      <button type="button" disabled={busy || newArgument === ""} onClick={() => { setNewArguments((current) => [...current, newArgument]); setNewArgument(""); }}>
        Add argument
      </button>
      {newArguments.length > 0 ? <ol>{newArguments.map((argument, at) => <li key={String(at)}>
        {argument}{" "}
        <button type="button" aria-label={`Remove argument ${at + 1}`} disabled={busy}
          onClick={() => setNewArguments((current) => current.filter((_, index) => index !== at))}>
          Remove argument
        </button>
      </li>)}</ol> : null}
      <label htmlFor="protection-max-age">Rotation interval (optional)</label>
      <input id="protection-max-age" value={newMaxAge} disabled={busy} placeholder="720h" aria-describedby="protection-max-age-hint"
        onChange={(e) => setNewMaxAge(e.target.value)} />
      <p id="protection-max-age-hint" className="hint">
        A Go duration, such as 720h, for which a recorded rotation stays current. readmit never rotates the external key itself.
      </p>
      <label htmlFor="protection-retain">Package retention (optional)</label>
      <input id="protection-retain" value={newRetain} disabled={busy} placeholder="2160h" aria-describedby="protection-retain-hint"
        onChange={(e) => setNewRetain(e.target.value)} />
      <p id="protection-retain-hint" className="hint">
        A Go duration, such as 2160h, that every package written under this control declares. It is a declaration: nothing is
        deleted or revoked automatically when it ends.
      </p>
      <button disabled={busy || !documentEntry || !newName || !newCommand} onClick={() => void register()}>
        {operation === "registering" ? "Registering…" : "Register control"}
      </button>
      {lastChange?.action === "register" && lastChange.result?.reason ? <p>{lastChange.result.reason}</p> : null}
      {lastChange?.action === "register" && lastChange.result?.document ? <p>Registered. Registering a reference proves nothing about the store behind it: a rotation the store answers for is what records a generation.</p> : null}
    </div>

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
        <button disabled={busy || !documentEntry} onClick={() => void openPackage()}>
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
