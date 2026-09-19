import { useState } from "react";
import {
  exportTest,
  importTest,
  validateTest,
  type CanonicalTestResult,
} from "./bindings";

/** The full canonical document stays in memory, including clauses the guided
 * draft cannot express. Only the engine validates it; exporting creates a new
 * document beside the original so relative references retain their base. */
export function CanonicalTestEditor({
  workspace,
  busy,
}: {
  workspace: string;
  busy: boolean;
}) {
  const [entry, setEntry] = useState("");
  const [output, setOutput] = useState("");
  const [document, setDocument] = useState("");
  const [pending, setPending] = useState(false);
  const [result, setResult] = useState<CanonicalTestResult | null>(null);
  const disabled = busy || pending;

  async function perform(
    work: () => Promise<CanonicalTestResult>,
    importing = false,
  ) {
    setPending(true);
    try {
      const next = await work();
      setResult(next);
      // A refusal leaves the previous edit available for correction or discard.
      if (importing && next.state === "completed")
        setDocument(next.document ?? "");
    } finally {
      setPending(false);
    }
  }

  return (
    <section aria-labelledby="canonical-test-heading">
      <h3 id="canonical-test-heading">Import and edit a saved test</h3>
      <p className="hint">
        Advanced canonical JSON editor. Import explicitly shows the expected
        values in the test. All supported clauses are retained, including exact
        ledger records. Edits stay in this window until you export a new file;
        no messages are sent.
      </p>
      <label htmlFor="canonical-entry">Test file in this workspace</label>
      <input
        id="canonical-entry"
        value={entry}
        onChange={(event) => setEntry(event.target.value)}
      />
      <button
        type="button"
        disabled={disabled || !entry || Boolean(document)}
        onClick={() => void perform(() => importTest(workspace, entry), true)}
      >
        Import and show values
      </button>
      <label htmlFor="canonical-document">Complete test spec</label>
      <textarea
        id="canonical-document"
        rows={18}
        spellCheck={false}
        value={document}
        disabled={disabled}
        onChange={(event) => {
          setDocument(event.target.value);
          setResult(null);
        }}
      />
      <button
        type="button"
        disabled={disabled || !document}
        onClick={() => void perform(() => validateTest(document))}
      >
        Validate with the test reader
      </button>
      <button
        type="button"
        disabled={disabled || !document}
        onClick={() => {
          setDocument("");
          setResult(null);
          setOutput("");
        }}
      >
        Discard unstored edits
      </button>
      <label htmlFor="canonical-output">New test file in this workspace</label>
      <input
        id="canonical-output"
        value={output}
        onChange={(event) => setOutput(event.target.value)}
      />
      <button
        type="button"
        disabled={disabled || !document || !output}
        onClick={() =>
          void perform(() => exportTest({ workspace, document, output }))
        }
      >
        Export new test
      </button>
      <p className="hint">
        Relative paths resolve from this workspace in both desktop and CLI.
        Validation checks the document, not endpoint readiness or whether its
        expectations pass. Run the exported file through the run panel or{" "}
        <code>readmit test</code> with the same explicit send approval.
      </p>
      <p role="status">
        {pending
          ? "Checking the test document."
          : (result?.reason ??
            (result?.output
              ? `Written to ${result.output} · spec identity ${result.identity}`
              : result?.state === "completed"
                ? "Accepted by the shared test reader."
                : ""))}
      </p>
    </section>
  );
}
