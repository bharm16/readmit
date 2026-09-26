// The first thing the window shows: the projects this viewer works in and the
// three ways to start — a new project, an existing one, or the demo. Nothing
// about licensing, connections or what this build writes is on it; those are
// Settings and Help.
import { useState } from "react";
import type { State } from "./bindings";

/** The new-project form. Creating asks the host for the folder that will hold
 * the project in its own dialog, so the form holds only what the project
 * document records. A refusal stays in the form beside what was typed. */
export function NewProjectForm({
  busy,
  onCreate,
  onCancel,
}: {
  busy: boolean;
  onCreate: (name: string, title: string, owner: string, versions: string[]) => Promise<{ state: State; reason?: string | undefined }>;
  onCancel: () => void;
}) {
  const [name, setName] = useState("");
  const [title, setTitle] = useState("");
  const [owner, setOwner] = useState("");
  const [versions, setVersions] = useState("");
  const [refusal, setRefusal] = useState<string | null>(null);
  const declared = versions
    .split(",")
    .map((version) => version.trim())
    .filter((version) => version !== "");
  return (
    <form
      className="form-stack"
      aria-label="New project"
      onSubmit={(event) => {
        event.preventDefault();
        setRefusal(null);
        void onCreate(name.trim(), title.trim() || name.trim(), owner.trim(), declared).then((answer) => {
          if (answer.state !== "completed") {
            setRefusal(answer.reason ?? "The project was not created.");
          }
        });
      }}
    >
      <div className="field">
        <label htmlFor="new-project-name">Name</label>
        <input
          id="new-project-name"
          type="text"
          required
          autoFocus
          value={name}
          onChange={(event) => setName(event.target.value)}
        />
        <p className="hint">Also the name of the project&apos;s folder.</p>
      </div>
      <div className="field">
        <label htmlFor="new-project-title">Title</label>
        <input
          id="new-project-title"
          type="text"
          placeholder={name.trim() || "Same as the name"}
          value={title}
          onChange={(event) => setTitle(event.target.value)}
        />
      </div>
      <div className="field">
        <label htmlFor="new-project-owner">Owner</label>
        <input id="new-project-owner" type="text" value={owner} onChange={(event) => setOwner(event.target.value)} />
      </div>
      <div className="field">
        <label htmlFor="new-project-versions">Interface versions</label>
        <input
          id="new-project-versions"
          type="text"
          placeholder="2.5.1, 2.3"
          value={versions}
          onChange={(event) => setVersions(event.target.value)}
        />
        <p className="hint">The HL7 versions this interface exchanges. The first is the default.</p>
      </div>
      {refusal ? (
        <p className="status status-failed" role="status">
          <span className="state">Not created</span>
          <span className="reason">{refusal}</span>
        </p>
      ) : null}
      <div className="form-buttons">
        <button type="button" onClick={onCancel}>
          Cancel
        </button>
        <button type="submit" disabled={busy || name.trim() === "" || declared.length === 0}>
          Choose location and create…
        </button>
      </div>
    </form>
  );
}

/** The demo, offered quietly below the projects. */
export function DemoCallout({ busy, onTryDemo }: { busy: boolean; onTryDemo: () => void }) {
  return (
    <div className="callout">
      <p>
        <strong>New to Readmit?</strong> Open a demo project with synthetic messages and a test that fails
        before the fix and passes after it.
      </p>
      <button type="button" disabled={busy} onClick={onTryDemo}>
        Try demo
      </button>
    </div>
  );
}
