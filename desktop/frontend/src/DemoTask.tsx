// The demo task: six steps over the synthetic demo project, each done in the
// ordinary screen it opens and marked done only from that screen's actual
// result. It sits in the sidebar while the demo project is open; closing it
// starts nothing and leaves the demo an ordinary project, with its origin
// still named.
import type { DemoProgress, DemoStepID } from "./bindings";
import { IconButton } from "./IconButton";
import "./help.css";

/** Whether a step is done: the project shows it, or its result was seen in
 * this window. */
export function stepDone(progress: DemoProgress, seen: ReadonlySet<DemoStepID>, id: DemoStepID): boolean {
  const step = progress.steps.find((entry) => entry.id === id);
  return !!step && (step.done || seen.has(id));
}

/** The first step not done yet, in order. */
export function nextStep(progress: DemoProgress, seen: ReadonlySet<DemoStepID>): DemoStepID | null {
  return progress.steps.find((step) => !stepDone(progress, seen, step.id))?.id ?? null;
}

export function DemoTask({
  progress,
  seen,
  shown,
  busy,
  notice,
  onStep,
  onClose,
  onShow,
}: {
  /** Why the last step did not complete. */
  notice?: string | null;
  progress: DemoProgress;
  seen: ReadonlySet<DemoStepID>;
  /** The steps are shown; otherwise only the origin is. */
  shown: boolean;
  busy: boolean;
  onStep: (id: DemoStepID) => void;
  onClose: () => void;
  onShow: () => void;
}) {
  if (!shown) {
    return (
      <button type="button" className="demo-origin quiet" onClick={onShow}>
        Demo · Synthetic
      </button>
    );
  }
  const next = nextStep(progress, seen);
  return (
    <section className="demo-task" aria-label="Demo">
      <header className="demo-task-header">
        <h2>Demo · Synthetic</h2>
        <IconButton icon="close" label="Close demo steps" onClick={onClose} />
      </header>
      <ol className="demo-steps">
        {progress.steps.map((step) => {
          const done = stepDone(progress, seen, step.id);
          return (
            <li key={step.id} className={done ? "done" : undefined} aria-label={done ? `${step.title}, done` : step.title}>
              {step.id === next ? (
                <button type="button" className="primary" disabled={busy} onClick={() => onStep(step.id)}>
                  {step.title}
                </button>
              ) : (
                <span>{step.title}</span>
              )}
            </li>
          );
        })}
      </ol>
      {notice ? (
        <p className="form-status" role="alert">
          {notice}
        </p>
      ) : null}
    </section>
  );
}
