import { useEffect, useRef, useState } from "react";
import {
  exportTest,
  importTest,
  validateTest,
  type CanonicalTestResult,
  type EditorDraft,
} from "./bindings";
import { RetentionStatus, draftFor, useRetainer } from "./drafting";
import { useLifecycle } from "./lifecycle";

/** The full canonical document stays in memory and is retained as it is
 * edited, including clauses the guided draft cannot express and errors the
 * reader would refuse today: a draft is kept under its own identity and never
 * has to be a valid final artifact. Only the engine validates it; exporting
 * creates a new document beside the original so relative references retain
 * their base. */
export function CanonicalTestEditor({
  workspace,
  drafts,
  busy,
}: {
  workspace: string;
  drafts: EditorDraft[] | null;
  busy: boolean;
}) {
  const [entry, setEntry] = useState("");
  const [output, setOutput] = useState("");
  const [document, setDocument] = useState("");
  const { running, run } = useLifecycle<"importing" | "validating" | "exporting">();
  const pending = running !== null;
  const [result, setResult] = useState<CanonicalTestResult | null>(null);
  const retainer = useRetainer();
  const disabled = busy || pending;

  // Continue the edit this workspace had unstored when the window last
  // stopped. The load happens once per workspace, so a retention arriving
  // later never rewrites text being typed right now.
  const loaded = useRef<string | null>(null);
  useEffect(() => {
    if (loaded.current === workspace) {
      return;
    }
    loaded.current = workspace;
    const held = draftFor(drafts, "canonical-test", workspace);
    if (held && typeof held.content === "string") {
      setDocument(held.content);
      retainer.keepId(held.id);
    }
  }, [workspace, drafts, retainer]);

  function edit(next: string) {
    setDocument(next);
    setResult(null);
    if (next === "") {
      const id = retainer.currentId();
      if (id !== "") {
        retainer.drop(id);
      }
      return;
    }
    retainer.save({
      id: "",
      kind: "canonical-test",
      workspace,
      case: "",
      identity: "",
      content_schema: "readmit-test/v1",
      content: next,
    });
  }

  function perform(
    kind: "importing" | "validating" | "exporting",
    work: () => Promise<CanonicalTestResult>,
  ): Promise<CanonicalTestResult | undefined> {
    return run(kind, async () => {
      const next = await work();
      setResult(next);
      // A refusal leaves the previous edit available for correction or discard.
      if (kind === "importing" && next.state === "completed") setDocument(next.document ?? "");
      return next;
    });
  }

  // The exported spec is on disk beside the evidence, so the draft has served
  // its purpose and is dropped only after the write succeeded. The document
  // itself stays on screen, exactly as it did before the export.
  function exported() {
    const id = retainer.currentId();
    if (id !== "") {
      retainer.drop(id);
    }
    retainer.clear();
  }

  return (
    <section aria-labelledby="canonical-test-heading">
      <h3 id="canonical-test-heading">Edit test</h3>
      <p className="hint">
        Advanced canonical JSON editor. Import explicitly shows the expected
        values in the test. All supported clauses are retained, and the edit is
        kept on this machine until you export a new file or discard it; no
        messages are sent.
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
        onClick={() => void perform("importing", () => importTest(workspace, entry))}
      >
        Import and show values
      </button>
      <p className="hint">Imported values may contain patient data.</p>
      <label htmlFor="canonical-document">Complete test spec</label>
      <textarea
        id="canonical-document"
        rows={18}
        spellCheck={false}
        value={document}
        disabled={disabled}
        onChange={(event) => edit(event.target.value)}
      />
      <button
        type="button"
        disabled={disabled || !document}
        onClick={() => void perform("validating", () => validateTest(document))}
      >
        Validate
      </button>
      <button
        type="button"
        disabled={disabled || !document}
        onClick={() => {
          const id = retainer.currentId();
          if (id !== "") {
            retainer.drop(id);
          }
          retainer.clear();
          setDocument("");
          setResult(null);
          setOutput("");
        }}
      >
        Discard changes
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
          void perform("exporting", () => exportTest({ workspace, document, output })).then((next) => {
            if (next?.state === "completed") {
              exported();
            }
          })
        }
      >
        Export test
      </button>
      <p className="hint">
        Relative paths resolve from this workspace in both desktop and CLI.
        Validation checks the document, not endpoint readiness or whether its
        expectations pass. Run the exported file through the run panel or{" "}
        <code>readmit test</code> with the same explicit send approval.
      </p>
      <RetentionStatus
        retention={retainer.retention}
        onRetry={retainer.retry}
        onKeepAsNew={retainer.keepAsNew}
        onDiscard={() => {
          const id = retainer.currentId();
          if (id !== "") {
            retainer.drop(id);
          }
          retainer.clear();
          setDocument("");
          setResult(null);
        }}
      />
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
