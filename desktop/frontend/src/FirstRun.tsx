import { useEffect, useState } from "react";
import { operationStatus, type OperationResult } from "./bindings";

/** The first-run block, shown while no workspace is open: the two real ways to
 * begin — a project of your own over your own evidence, or the free guided
 * sample — presented as equals, with the license and setup states the facade
 * actually holds. Nothing here decides anything: every choice is the typed
 * call the named screen already answers to, and the license note opens the
 * pane where activation lives rather than describing one.
 *
 * The sample's honesty is part of the choice. Its evidence is deterministic
 * and its saved tests genuinely fail and pass, so it is real practice — and it
 * is never a substitute for importing your own evidence, which is its own
 * stated choice here and the one a real investigation starts from. */
export function FirstRun({
  busy,
  onOpenWorkspace,
  onExploreSample,
  onLicense,
}: {
  busy: boolean;
  onOpenWorkspace: () => void;
  onExploreSample: () => void;
  onLicense: () => void;
}) {
  const [license, setLicense] = useState<OperationResult | null>(null);
  useEffect(() => {
    let active = true;
    void operationStatus().then((result) => {
      if (active) setLicense(result);
    });
    return () => {
      active = false;
    };
  }, []);

  return (
    <section className="first-run" aria-labelledby="first-run-title">
      <h3 id="first-run-title">Start here</h3>
      <p className="hint">
        Everything below stays on this machine: the folder you choose is an
        ordinary local folder, and nothing in it is sent anywhere by starting.
      </p>
      <div className="first-run-choices" role="group" aria-labelledby="first-run-title">
        <div className="first-run-choice">
          <h4>Your projects</h4>
          <p>
            Choose a folder this account can write to and start a real project
            in it — you will import or capture your own evidence into it next.
            A folder that already holds a workspace opens as that workspace.
          </p>
          <button type="button" disabled={busy} onClick={onOpenWorkspace}>
            Create project…
          </button>
          <button type="button" disabled={busy} onClick={onOpenWorkspace}>
            Open workspace…
          </button>
        </div>
        <div
          className="first-run-choice"
          role="group"
          aria-labelledby="guided-sample-choice-title"
        >
          <h4 id="guided-sample-choice-title" className="visually-hidden">
            Guided sample
          </h4>
          <p>
            Deterministic synthetic evidence with genuinely failing and passing
            saved tests, created in a new folder. It is practice for the
            workflow — never a substitute for your own evidence, and never a
            claim about your integration.
          </p>
          <button type="button" disabled={busy} onClick={onExploreSample}>
            Explore sample
          </button>
        </div>
      </div>
      <div className="first-run-license" role="note" aria-label="License state">
        {license === null ? null : license.clock && !license.clock.released ? (
          <p>
            License: {license.term ?? "installed"} for {license.clock.organization} — active
            until {license.expires ?? "the term the document declares"}.
            {license.clock.rollback
              ? " Clock correction must be resolved before new licensed work."
              : " Reading, verifying and exporting existing work stays free either way."}
          </p>
        ) : license.clock && license.clock.released ? (
          <p>
            The activation on this machine was released. Existing work stays
            readable, verifiable and exportable; new licensed work needs
            activation again.
          </p>
        ) : (
          <p>
            License: none activated on this machine. Reading, verifying and
            exporting existing work, and the guided sample, are free. Creating
            or running new licensed work needs the signed document your vendor
            delivered — import and activate it in License and activation.
          </p>
        )}
        <button type="button" onClick={onLicense}>
          License
        </button>
      </div>
    </section>
  );
}
