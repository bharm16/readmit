import { useCallback, useEffect, useState } from "react";
import {
  disclosureStatus,
  type DisclosureState,
  type OperationDisclosure,
  type Support,
} from "./bindings";
import { IconButton } from "./IconButton";
import { useLifecycle } from "./lifecycle";

/** How each closed state word reads. The word carries the meaning; the
 * sentence beside it says what it means here, so neither colour nor icon is
 * ever the difference between two of them. */
const STATE_WORDS: Record<DisclosureState["state"], string> = {
  idle: "Idle — nothing is connected",
  active: "Active now",
  "not-configured": "Not configured",
  offline: "Configured, offline",
  connected: "Connected",
  configured: "Configured (browser only)",
};

/** The live half of the privacy status. The shell declares what each
 * deliberately configurable activity is and what it takes; this table joins
 * the facade's answer about where each one stands right now, read without
 * contacting anything. Each activity's next action opens the real screen
 * where that activity is configured or run — a disclosure that could only
 * describe its screen would not be recovery. */
export function PrivacyDisclosure({
  operations,
  workspaceOpen,
  onOpenRunPanel,
  onStartCapture,
  onStartObservation,
}: {
  operations: OperationDisclosure[];
  workspaceOpen: boolean;
  onOpenRunPanel: () => void;
  onStartCapture: () => void;
  onStartObservation: () => void;
}) {
  const [states, setStates] = useState<DisclosureState[] | null>(null);
  const [reason, setReason] = useState<string | null>(null);
  // Reading the states claims no operation slot, so only this section's own
  // controls wait for it.
  const { running, run } = useLifecycle<"reading">();
  const busy = running !== null;

  const refresh = useCallback(async () => {
    await run("reading", async () => {
      setReason(null);
      const result = await disclosureStatus();
      if (result.states) {
        setStates(result.states);
      } else {
        setReason(result.reason ?? "The connection states could not be read just now.");
      }
    });
  }, [run]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const stateOf = (id: string): DisclosureState | null =>
    states?.find((state) => state.id === id) ?? null;

  return (
    <section aria-labelledby="privacy-disclosure-title">
      <h3 id="privacy-disclosure-title">Network access</h3>
      <p className="hint">
        These are the only activities that can reach a destination outside this
        window. Each happens only when you configure and start it; startup and
        every local operation contact nothing.
      </p>
      <table aria-label="Deliberately configured activities and their destinations">
        <caption className="visually-hidden">
          Destination, data, authorization and current state of every activity
          that can reach outside this window
        </caption>
        <thead>
          <tr>
            <th scope="col">Activity</th>
            <th scope="col">Destination</th>
            <th scope="col">What it carries</th>
            <th scope="col">Before it happens</th>
            <th scope="col">State now</th>
            <th scope="col">Next action</th>
          </tr>
        </thead>
        <tbody>
          {operations.map((operation) => {
            const live = stateOf(operation.id);
            return (
              <tr key={operation.id}>
                <th scope="row">{operation.activity}</th>
                <td>{operation.destination}</td>
                <td>{operation.data}</td>
                <td>{operation.authorization}</td>
                <td>
                  {live ? (
                    <>
                      <span className={`disclosure-state disclosure-${live.state}`}>
                        {STATE_WORDS[live.state]}
                      </span>
                      <span className="reason">{live.detail}</span>
                    </>
                  ) : reason ? (
                    <span className="unsupported">{reason}</span>
                  ) : (
                    <span className="unsupported">States could not be read just now.</span>
                  )}
                </td>
                <td>
                  {operation.id === "run" ? (
                    <button type="button" disabled={busy} onClick={onOpenRunPanel}>
                      Runs
                    </button>
                  ) : null}
                  {operation.id === "runner" ? (
                    <span className="reason">The runner screens sit in this region.</span>
                  ) : null}
                  {operation.id === "capture" ? (
                    <>
                      <button
                        type="button"
                        disabled={busy || !workspaceOpen}
                        onClick={onStartCapture}
                      >
                        Capture
                      </button>
                      {!workspaceOpen ? (
                        <span className="reason">Open a workspace folder first.</span>
                      ) : null}
                    </>
                  ) : null}
                  {operation.id === "observe" ? (
                    <>
                      <button
                        type="button"
                        disabled={busy || !workspaceOpen}
                        onClick={onStartObservation}
                      >
                        Observations
                      </button>
                      {!workspaceOpen ? (
                        <span className="reason">Open a workspace folder first.</span>
                      ) : null}
                    </>
                  ) : null}
                  {operation.id === "hub" ? (
                    <span className="reason">Connect or disconnect below.</span>
                  ) : null}
                  {operation.id === "portal" ? (
                    <span className="reason">
                      The portal link sits in License and activation above.
                    </span>
                  ) : null}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
      {/* A scoped refresh of the disclosed states only: no new network check
       * or collection hides behind this utility. */}
      <IconButton label="Refresh privacy status" icon="refresh" disabled={busy} onClick={() => void refresh()} />
      {busy ? <p className="hint">Reading the connection states.</p> : null}
    </section>
  );
}

/** The support guidance, carried verbatim from the facade: the qualification
 * and certification refusals the verified state actually holds, and the
 * ledger rows still open. It is rendered here, beside the privacy status,
 * because both are statements about what this build may truthfully claim. */
export function SupportGuidance({ support }: { support: Support }) {
  if (support.notes.length === 0 && support.unavailable.length === 0) {
    return null;
  }
  return (
    <section aria-labelledby="support-guidance-title">
      <h3 id="support-guidance-title">Capabilities</h3>
      <ul className="support-notes">
        {support.notes.map((note) => (
          <li key={note}>{note}</li>
        ))}
      </ul>
      {support.unavailable.length > 0 ? (
        <>
          <h4>Still without a checked application screen</h4>
          <p className="hint">
            The coverage ledger keeps each row below open: the capability is
            tracked without its completed screen and parity evidence, and it is
            named here rather than promised.
          </p>
          <ul className="support-unavailable">
            {support.unavailable.map((entry) => (
              <li key={entry}>{entry}</li>
            ))}
          </ul>
        </>
      ) : null}
    </section>
  );
}
