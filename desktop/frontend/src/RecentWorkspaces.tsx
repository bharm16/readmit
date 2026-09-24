// The recent list of the navigation region: folders this viewer opened, most
// recent first, read from the facade's own local list. Reopening one opens it
// as a workspace again; forgetting one removes only its entry from the list,
// after the person confirms it, and leaves the folder and everything in it
// exactly where it is. The list holds folder paths and nothing read out of
// them.
import { useEffect, useRef, useState } from "react";
import type { RecentResult } from "./bindings";
import type { Indicators } from "./shell";
import { Status } from "./shell";

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
      <h3 ref={heading} tabIndex={-1}>
        Recent workspaces
      </h3>
      {recent && recent.state !== "completed" ? (
        <Status indicator={indicators.get(recent.state)} state={recent.state} reason={recent.reason} />
      ) : null}
      <ul className="recent" aria-label="Recent workspaces">
        {roots.map((folder) => (
          <li key={folder}>
            <button type="button" disabled={busy} onClick={() => onReopen(folder)}>
              {folder}
            </button>
            {confirming === folder ? (
              <span
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
                <span className="hint"> Forget this folder? It stays where it is, with everything in it.</span>
                <button
                  type="button"
                  disabled={busy}
                  onClick={() => {
                    setConfirming(null);
                    setReturning({ to: "list", folder });
                    void onForget(folder);
                  }}
                >
                  Forget it
                </button>
                <button type="button" ref={keep} disabled={busy} onClick={() => cancelForget(folder)}>
                  Keep it
                </button>
              </span>
            ) : (
              <button
                type="button"
                aria-label={`Forget ${folder}`}
                disabled={busy}
                ref={(control) => {
                  if (control) forgetControls.current.set(folder, control);
                  else forgetControls.current.delete(folder);
                }}
                onClick={() => setConfirming(folder)}
              >
                Forget
              </button>
            )}
          </li>
        ))}
      </ul>
    </>
  );
}
