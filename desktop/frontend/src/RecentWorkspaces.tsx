// The recent projects on the home page: folders this viewer opened, most
// recent first, read from the facade's own local list. Opening one opens it
// again; removing one takes only its entry off the list, after the person
// confirms it, and leaves the folder and everything in it exactly where it
// is. The list holds folder paths and nothing read out of them.
import { useEffect, useRef, useState } from "react";
import type { RecentResult } from "./bindings";
import type { Indicators } from "./shell";
import { Status } from "./shell";
import { EmptyState, folderName } from "./layout";

export function RecentWorkspaces({
  recent,
  busy,
  indicators,
  onReopen,
  onForget,
}: {
  recent: RecentResult | null;
  busy: boolean;
  indicators: Indicators;
  onReopen: (folder: string) => void;
  onForget: (folder: string) => Promise<void>;
}) {
  // The one folder whose forgetting is waiting for the person's answer.
  const [confirming, setConfirming] = useState<string | null>(null);
  // Where focus goes once the window has answered: back to the Forget control
  // a cancelled confirmation came from, or into the list a forget changed.
  const [returning, setReturning] = useState<{ to: "forget" | "list"; folder: string } | null>(null);
  const heading = useRef<HTMLHeadingElement | null>(null);
  const keep = useRef<HTMLButtonElement | null>(null);
  const forgetControls = useRef(new Map<string, HTMLButtonElement>());
  const roots = recent?.roots ?? [];

  // A confirmation for a folder the list no longer holds has nothing to ask.
  useEffect(() => {
    if (confirming !== null && !(recent?.roots ?? []).includes(confirming)) setConfirming(null);
  }, [confirming, recent]);

  useEffect(() => {
    if (confirming !== null) keep.current?.focus();
  }, [confirming]);

  useEffect(() => {
    if (returning === null || busy) return;
    const control = returning.to === "forget" ? forgetControls.current.get(returning.folder) : undefined;
    (control ?? heading.current)?.focus();
    setReturning(null);
  }, [busy, returning, recent]);

  const cancelForget = (folder: string) => {
    setConfirming(null);
    setReturning({ to: "forget", folder });
  };

  return (
    <>
      <div className="section-title">
        <h2 ref={heading} tabIndex={-1}>
          Recent projects
        </h2>
        {roots.length > 0 ? <span className="count">{roots.length}</span> : null}
      </div>
      {recent && recent.state !== "completed" ? (
        <Status indicator={indicators.get(recent.state)} state={recent.state} reason={recent.reason} />
      ) : null}
      {roots.length === 0 && recent?.state === "completed" ? (
        <EmptyState title="No projects yet">
          Create a project, open one you already have, or try the demo.
        </EmptyState>
      ) : null}
      <ul className={roots.length > 0 ? "item-list recent" : "recent"} aria-label="Recent projects">
        {roots.map((folder) => (
          <li key={folder}>
            <span className="item-icon" aria-hidden="true">
              <svg viewBox="0 0 16 16" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinejoin="round">
                <path d="M2.5 4.5h4l1.2 1.5h5.8v6.5h-11z" />
              </svg>
            </span>
            <span className="item-main">
              <span className="item-title">{folderName(folder)}</span>
              <span className="item-sub" title={folder}>
                {folder}
              </span>
            </span>
            {confirming === folder ? (
              <span
                className="confirm"
                role="group"
                aria-label={`Forget ${folder}?`}
                onKeyDown={(event) => {
                  // Escape answers this question and goes no further: the
                  // window's own Escape cancels a running operation.
                  if (event.key === "Escape" && !event.nativeEvent.isComposing && !busy) {
                    event.preventDefault();
                    event.stopPropagation();
                    cancelForget(folder);
                  }
                }}
              >
                <span className="hint">Remove from this list? The folder is not changed.</span>
                <button
                  type="button"
                  disabled={busy}
                  onClick={() => {
                    setConfirming(null);
                    setReturning({ to: "list", folder });
                    void onForget(folder);
                  }}
                >
                  Remove
                </button>
                <button type="button" ref={keep} disabled={busy} onClick={() => cancelForget(folder)}>
                  Keep
                </button>
              </span>
            ) : (
              <span className="item-actions">
                <button type="button" aria-label={`Remove ${folder} from recent projects`} className="quiet" disabled={busy}
                  ref={(control) => {
                    if (control) forgetControls.current.set(folder, control);
                    else forgetControls.current.delete(folder);
                  }}
                  onClick={() => setConfirming(folder)}
                >
                  Remove
                </button>
                <button type="button" aria-label={`Open ${folder}`} disabled={busy} onClick={() => onReopen(folder)}>
                  Open
                </button>
              </span>
            )}
          </li>
        ))}
      </ul>
    </>
  );
}
