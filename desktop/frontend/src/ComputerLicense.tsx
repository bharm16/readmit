// This computer's license, handled the way any software purchase is: one
// license per computer, activated from the file received at purchase or its
// pasted contents and checked here with no network, renewed by activating the
// renewed file, saved as a copy, and deactivated to move the seat to another
// computer. It is the license the command line uses too. Everything the
// person reads says license, activate, renew and deactivate; the facade
// decides every fact and every refusal, and this pane only shows them.
import { useEffect, useRef, useState } from "react";
import {
  activateLicense, deactivateLicense, exportInstalledLicense, licenseStatus, reviewLicense,
  type InstalledLicenseResult, type InstalledLicenseView, type LicenseDocumentView,
  type LicenseExportResult, type LicenseReviewResult,
} from "./bindings";

/** The calendar date an instant falls on, in UTC as the license states it. */
function day(instant: string | undefined): string {
  return (instant ?? "").slice(0, 10);
}

function count(n: number | undefined, noun: string): string {
  const value = n ?? 0;
  return `${value} ${noun}${value === 1 ? "" : "s"}`;
}

/** What a received license includes and when it runs, in plain words. */
function describeReceived(document: LicenseDocumentView): string {
  const grace = document.grace_days ? `, with grace until ${day(document.grace_ends)}` : "";
  return `License for ${document.organization} on the ${document.plan} plan: ${count(document.seats, "author seat")} and ` +
    `${count(document.runner_instances, "runner slot")}, valid from ${day(document.not_before)} until ${day(document.expires)}${grace}. ` +
    "It was checked on this computer with your vendor's verification keys.";
}

/** Where this computer's license stands, in plain words. */
function describeTerm(license: InstalledLicenseView): string {
  if (license.deactivated) {
    const when = license.deactivated_at ? ` on ${day(license.deactivated_at)}` : "";
    return `This computer was deactivated${when}. Your vendor can reissue its seat for another computer from your account. ` +
      "Existing work stays readable, verifiable and exportable.";
  }
  switch (license.term) {
    case "not-yet-valid":
      return `This license starts on ${day(license.starts)}.`;
    case "grace":
      return `This license expired on ${day(license.expires)}. New work is still allowed during its grace period, until ` +
        `${day(license.grace_ends)}. Activate the renewed license to keep working after that.`;
    case "expired":
      return `This license expired on ${day(license.expires)}. Existing work stays readable, verifiable and exportable; ` +
        "creating or running new work needs the renewed license.";
    default:
      return license.renew_soon
        ? `This license expires on ${day(license.expires)}, in ${count(license.days_left, "day")}. Renew it to keep creating ` +
          "and running new work: get the renewed license from your account and activate it here, where it replaces this one."
        : `Valid until ${day(license.expires)}.`;
  }
}

/** The pane's own section of the license and activation region. portal is
 * the account address an operator configured, opened only by a click;
 * onChanged lets the region re-read what the activation changed. */
export function ComputerLicense({ portal, onChanged }: { portal: string | undefined; onChanged: () => void }) {
  const [status, setStatus] = useState<InstalledLicenseResult | null>(null);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);
  const [pasting, setPasting] = useState(false);
  const [pasted, setPasted] = useState("");
  const [review, setReview] = useState<LicenseReviewResult | null>(null);
  // The pasted text the review checked, which activation sends again; empty
  // when the license came from a file.
  const [reviewed, setReviewed] = useState("");
  const [author, setAuthor] = useState("");
  const [device, setDevice] = useState("");
  const [pool, setPool] = useState("");
  const [confirming, setConfirming] = useState(false);
  const [exported, setExported] = useState<LicenseExportResult | null>(null);
  const keep = useRef<HTMLButtonElement | null>(null);
  const deactivateControl = useRef<HTMLButtonElement | null>(null);

  useEffect(() => {
    let active = true;
    void licenseStatus().then((value) => { if (active) setStatus(value); });
    return () => { active = false; };
  }, []);

  useEffect(() => {
    if (confirming) keep.current?.focus();
  }, [confirming]);

  async function perform<T>(action: () => Promise<T>, apply: (value: T) => void) {
    if (busy) return;
    setBusy(true);
    setNotice(null);
    try { apply(await action()); } finally { setBusy(false); }
  }

  const license = status?.state === "completed" ? status.license : undefined;
  const active = license !== undefined && !license.deactivated;
  const document = review?.state === "completed" ? review.document : undefined;
  const assignments = document?.assignments ?? [];
  const devices = document?.operation_capable
    ? assignments.find((entry) => entry.author === author)?.devices ?? []
    : document?.devices ?? [];
  const pools = document?.authorities ?? [];
  const ready = review?.renewal || (document?.operation_capable ? author !== "" && device !== "" : device !== "");
  const renewalDue = license !== undefined && !license.deactivated &&
    (license.renew_soon || license.term === "grace" || license.term === "expired");

  function discardReview() {
    setReview(null);
    setReviewed("");
  }

  function startReview(contents: string, chooseKeys: boolean) {
    void perform(() => reviewLicense({ contents, choose_keys: chooseKeys }), (value) => {
      setExported(null);
      if (value.state !== "completed" || !value.document) {
        setReview(value.state === "cancelled" ? null : value);
        setReviewed(contents);
        if (value.state === "cancelled") setNotice(value.reason ?? null);
        return;
      }
      setReview(value);
      setReviewed(contents);
      // A single choice the license offers is shown chosen; the person can
      // still change it before activating.
      const only = value.document.assignments?.length === 1 ? value.document.assignments[0] : undefined;
      setAuthor(only?.author ?? "");
      const offered = value.document.operation_capable ? only?.devices ?? [] : value.document.devices ?? [];
      setDevice(offered.length === 1 ? offered[0] ?? "" : "");
      const offeredPools = value.document.authorities ?? [];
      setPool(offeredPools.length === 1 ? offeredPools[0]?.id ?? "" : "");
    });
  }

  function activate() {
    if (!review) return;
    const renewal = review.renewal;
    // An empty member is an absent one: a renewal keeps the person, computer
    // and runner pool this computer's license was activated for.
    void perform(() => activateLicense({
      entitlement: review.entitlement ?? "",
      contents: reviewed,
      trust: review.trust ?? "",
      author: renewal ? "" : author,
      device: renewal ? "" : device,
      authority: renewal ? "" : pool,
    }), (value) => {
      if (value.state !== "completed") {
        setNotice(value.reason ?? null);
        return;
      }
      setStatus(value);
      discardReview();
      setPasting(false);
      setPasted("");
      setNotice(value.outcome === "renewed" ? "The renewed license replaced the previous one." : "This computer's license is activated.");
      onChanged();
    });
  }

  function deactivate() {
    setConfirming(false);
    void perform(deactivateLicense, (value) => {
      if (value.state === "completed") {
        setStatus(value);
        setNotice("This computer is deactivated.");
        onChanged();
      } else {
        setNotice(value.reason ?? null);
      }
      deactivateControl.current?.focus();
    });
  }

  function keepLicense() {
    setConfirming(false);
    // The control is drawn again once the question is gone.
    setTimeout(() => deactivateControl.current?.focus(), 0);
  }

  return <section aria-labelledby="computer-license-title" className="computer-license">
    <h4 id="computer-license-title">This computer's license</h4>
    <div role="status" aria-live="polite">
      {status === null ? <p>Reading this computer's license…</p> : null}
      {status && status.state !== "completed" ? <p>{status.state === "empty"
        ? "No license is activated on this computer. Opening, verifying and exporting existing work, and the guided sample, never need one."
        : status.reason}</p> : null}
      {license ? <>
        <p>Licensed to {license.organization} on the {license.plan} plan: {count(license.author_seats, "author seat")} and {count(license.runner_slots, "runner slot")}.</p>
        {license.current_format
          ? <p>Activated on {day(license.activated)} for {license.author} on the computer {license.device}{license.runner_pool ? `; tests run from here count against the ${license.runner_pool} runner pool` : ""}.</p>
          : <p>Activated on {day(license.activated)} for the computer {license.device}. This license is in an earlier format that lists licensed computers; it does not let this computer create or run new work. Ask your vendor for a current license file.</p>}
        <p>{describeTerm(license)}</p>
        {license.current_format && !license.deactivated && !license.new_work && (license.term === "active" || license.term === "grace")
          ? <p>New work is not admitted through this license yet. If its activation was interrupted, activating the same license file again finishes it; otherwise deactivate this computer and activate its license again.</p>
          : null}
      </> : null}
      {notice ? <p>{notice}</p> : null}
    </div>

    <div className="operation-actions">
      {active ? <>
        <button type="button" disabled={busy} onClick={() => startReview("", false)}>Renew with a license file…</button>
        <button type="button" disabled={busy} onClick={() => setPasting(true)}>Paste a renewed license…</button>
      </> : <>
        <button type="button" disabled={busy} onClick={() => startReview("", false)}>Activate a license file…</button>
        <button type="button" disabled={busy} onClick={() => setPasting(true)}>Paste a license…</button>
      </>}
      {license ? <button type="button" disabled={busy} onClick={() => void perform(exportInstalledLicense, setExported)}>Save a copy of this license…</button> : null}
      {active ? (confirming ? (
        <span
          role="group"
          aria-label="Deactivate this computer?"
          onKeyDown={(event) => {
            // Escape answers this question and goes no further: the window's
            // own Escape cancels a running operation.
            if (event.key === "Escape" && !event.nativeEvent.isComposing) {
              event.preventDefault();
              event.stopPropagation();
              keepLicense();
            }
          }}
        >
          <span className="hint"> Deactivate this computer? New work stops here and on the command line; existing work stays readable, verifiable and exportable, and your vendor can reissue the seat for another computer.</span>
          <button type="button" disabled={busy} onClick={deactivate}>Deactivate</button>
          <button type="button" ref={keep} disabled={busy} onClick={keepLicense}>Keep this license</button>
        </span>
      ) : (
        <button type="button" ref={deactivateControl} disabled={busy} onClick={() => setConfirming(true)}>Deactivate this computer…</button>
      )) : null}
      <button type="button" disabled={busy} onClick={() => void perform(licenseStatus, setStatus)}>Refresh this computer's license</button>
    </div>

    {renewalDue || license?.deactivated ? (
      portal
        ? <p><a href={portal} target="_blank" rel="noreferrer">{license?.deactivated ? "Open your account" : "Get renewed license"}</a></p>
        : <p role="note">{license?.deactivated
          ? "Your account's address is not configured here: ask your vendor to reissue this seat."
          : "Your account's address is not configured here: ask your vendor for the renewed license file."}</p>
    ) : null}

    {pasting ? (
      <div className="license-paste">
        <label htmlFor="license-contents">License file contents</label>
        <textarea id="license-contents" rows={6} spellCheck={false} value={pasted} disabled={busy} onChange={(event) => setPasted(event.target.value)} />
        <button type="button" disabled={busy || pasted.trim() === ""} onClick={() => startReview(pasted, false)}>Check the pasted license</button>
        <button type="button" disabled={busy} onClick={() => { setPasting(false); setPasted(""); }}>Cancel pasting</button>
      </div>
    ) : null}

    {review ? (
      <div
        className="license-review"
        role="group"
        aria-labelledby="license-review-title"
        onKeyDown={(event) => {
          if (event.key === "Escape" && !event.nativeEvent.isComposing && !busy) {
            event.preventDefault();
            event.stopPropagation();
            discardReview();
          }
        }}
      >
        <h5 id="license-review-title">{review.renewal ? "Renewed license" : "License to activate"}</h5>
        {document ? <p>{describeReceived(document)}</p> : <>
          <p role="note">{review.reason}</p>
          <button type="button" disabled={busy} onClick={() => startReview(reviewed, true)}>Check it with an updated keys file…</button>
        </>}
        {document && review.renewal ? <p>Activating it replaces this computer's license in place, for the same person and computer.</p> : null}
        {document && !review.renewal && document.operation_capable ? <>
          <label htmlFor="license-person">Who uses this computer</label>
          <select id="license-person" value={author} disabled={busy} onChange={(event) => { setAuthor(event.target.value); setDevice(""); }}>
            <option value="">Choose a person</option>
            {assignments.map((entry) => <option key={entry.author} value={entry.author}>{entry.author}</option>)}
          </select>
          <label htmlFor="license-computer">This computer</label>
          <select id="license-computer" value={device} disabled={busy || author === ""} onChange={(event) => setDevice(event.target.value)}>
            <option value="">Choose a computer</option>
            {devices.map((name) => <option key={name} value={name}>{name}</option>)}
          </select>
          <label htmlFor="license-pool">Tests run from this computer count against</label>
          <select id="license-pool" value={pool} disabled={busy} onChange={(event) => setPool(event.target.value)}>
            <option value="">No runner pool: this computer runs no tests</option>
            {pools.map((entry) => <option key={entry.id} value={entry.id}>{entry.id} ({count(entry.instances, "slot")})</option>)}
          </select>
        </> : null}
        {document && !review.renewal && !document.operation_capable ? <>
          <label htmlFor="license-computer">This computer</label>
          <select id="license-computer" value={device} disabled={busy} onChange={(event) => setDevice(event.target.value)}>
            <option value="">Choose a computer</option>
            {devices.map((name) => <option key={name} value={name}>{name}</option>)}
          </select>
          <p role="note">This license is in an earlier format that lists licensed computers; activating it does not let this computer create or run new work.</p>
        </> : null}
        {document ? <button type="button" disabled={busy || !ready} onClick={activate}>{review.renewal ? "Install the renewed license" : "Activate on this computer"}</button> : null}
        <button type="button" disabled={busy} onClick={discardReview}>Cancel</button>
      </div>
    ) : null}

    {exported ? (
      exported.state === "completed"
        ? <p>A copy of this license was saved to {exported.path}, exactly as it was received.</p>
        : <p role="note">{exported.reason}</p>
    ) : null}
  </section>;
}
