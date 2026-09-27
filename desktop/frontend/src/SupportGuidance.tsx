import type { Support } from "./bindings";

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
