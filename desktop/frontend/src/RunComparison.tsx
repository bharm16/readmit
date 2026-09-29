// Compare runs (view 24): two finished runs of a test, earlier and later by
// when each started, each check aligned by its identity and definition, and
// what they were run with compared part by part. Added runs are counted for
// their results; nothing here infers a cause or a probability.
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import {
  compareRunItems,
  listWholeCatalog,
  RequestScope,
  type CatalogItem,
  type RunComparisonItemsResult,
  type RunComparisonSide,
} from "./bindings";
import { DataTable } from "./DataTable";
import { CHECK_CHANGES, CHECK_RESULTS, CONFIGURATION_OUTCOMES, CONFIGURATION_PARTS, RUN_RESULTS, STABILITY_STATES, term } from "./display";
import { EmptyState, FormDialog, ValueRows } from "./layout";
import { comparable, startedText } from "./Runs";
import { TaskTabs } from "./TaskTabs";
import { checkTitle } from "./TestEditor";
import "./runs.css";

/** How many further runs a comparison counts. */
export const MAX_REPEATS = 14;

type Tab = "checks" | "configuration" | "stability";

function sideText(side: RunComparisonSide): string {
  return [side.version ? `${side.name} · v${side.version}` : side.name, startedText(side.started_at), side.environment_name].filter(Boolean).join(" · ");
}

function outcomeText(status: string, observed: number | undefined): string {
  const word = status in CHECK_RESULTS ? term(CHECK_RESULTS, status).text : status === "excluded" ? "Not in this run" : status === "unknown" ? "Unknown" : status;
  return observed === undefined ? word : `${observed} / ${word}`;
}

type CompareProps = {
  root: string | null;
  runs: string[];
  view: string;
  onView: (view: string) => void;
  onRuns: (runs: string[]) => void;
  onOpen: (id: string) => void;
};

/** Compare runs: its title, actions and body. */
export function useRunComparison({ root, runs, view, onView, onRuns, onOpen }: CompareProps) {
  const scope = useRef(new RequestScope());
  const context = useCallback(() => scope.current.enter(root ?? ""), [root]);
  const [result, setResult] = useState<RunComparisonItemsResult | null>(null);
  const [candidates, setCandidates] = useState<CatalogItem[]>([]);
  const [sheet, setSheet] = useState<null | "add" | "change">(null);
  const key = runs.join("..");

  useEffect(() => {
    if (!root || runs.length < 2) return;
    setResult(null);
    const asked = context();
    void compareRunItems({ context: asked, runs: runs.map((id) => ({ kind: "run", id })) }).then((answer) => {
      if (scope.current.current(answer)) setResult(answer);
    });
    void listWholeCatalog({ context: asked, kind: "run", filter: {} }).then((answer) => {
      if (scope.current.current(answer)) setCandidates((answer.page?.items ?? []).filter(comparable));
    });
    // Compared again whenever the chosen runs change.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, root]);

  const comparison = result?.comparison;
  const tabs: { key: Tab; label: string }[] = [
    { key: "checks", label: "Checks" },
    { key: "configuration", label: "Configuration" },
    ...(comparison && comparison.repeats.length > 0 ? [{ key: "stability" as Tab, label: "Stability" }] : []),
  ];
  const tab = (tabs.find((entry) => entry.key === view)?.key ?? "checks") as Tab;
  const test = candidates.find((item) => item.ref.id === runs[0])?.summary.run?.test?.id;
  const sameTest = candidates.filter((item) => !runs.includes(item.ref.id) && (!test || item.summary.run?.test?.id === test));

  let body: ReactNode;
  if (!result) {
    body = <p aria-live="polite">Comparing…</p>;
  } else if (result.state !== "completed" || !comparison) {
    body = (
      <div className="empty-state" role="alert">
        <p className="empty-title">{result.reason ?? "These runs cannot be compared."}</p>
        <ul>
          {runs.map((id) => {
            const selected = candidates.find((item) => item.ref.id === id);
            return <li key={id}><button type="button" className="link" onClick={() => onOpen(id)}>{selected?.name || "Run"}</button></li>;
          })}
        </ul>
        <div className="empty-action">
          <button type="button" onClick={() => setSheet("change")}>
            Change selection
          </button>
        </div>
      </div>
    );
  } else {
    const checks =
      comparison.checks.length === 0 ? (
        <EmptyState title="No checks" />
      ) : (
        <DataTable
          label="Checks"
          className="page-table"
          rows={comparison.checks}
          rowId={(row) => row.check.id}
          rowLabel={(row) => checkTitle(row.check, [])}
          columns={[
            { key: "check", header: "Check", priority: 1, minWidth: 12, flex: true, render: (row) => checkTitle(row.check, []) },
            { key: "earlier", header: "Earlier", priority: 2, minWidth: 8, render: (row) => outcomeText(row.earlier, row.earlier_observed) },
            { key: "later", header: "Later", priority: 2, minWidth: 8, render: (row) => outcomeText(row.later, row.later_observed) },
            { key: "change", header: "Change", priority: 1, minWidth: 8, render: (row) => term(CHECK_CHANGES, row.change).text },
          ]}
          selected={null}
          onSelect={() => undefined}
          onOpen={() => undefined}
        />
      );
    const configuration = (
      <DataTable
        label="Configuration"
        className="page-table"
        rows={comparison.configuration}
        rowId={(row) => row.part}
        rowLabel={(row) => row.part}
        columns={[
          { key: "part", header: "Part", priority: 1, minWidth: 8, render: (row) => term(CONFIGURATION_PARTS, row.part).text },
          { key: "change", header: "Change", priority: 1, minWidth: 8, render: (row) => term(CONFIGURATION_OUTCOMES, row.outcome).text },
          { key: "parts", header: "What differs", priority: 2, minWidth: 12, flex: true, render: (row) => row.parts.join(", ") || row.reason || "—" },
        ]}
        selected={null}
        onSelect={() => undefined}
        onOpen={() => undefined}
      />
    );
    const stability = comparison.stability;
    const every = [comparison.earlier, comparison.later, ...comparison.repeats];
    const stabilityBody = (
      <>
        <ValueRows
          label="Stability"
          rows={[
            { label: "Runs", value: String(stability.runs) },
            { label: "Passed", value: String(stability.passes) },
            { label: "Failed", value: String(stability.failures) },
            { label: "Errors", value: String(stability.errors) },
            { label: "Incomplete", value: String(stability.incomplete) },
            { label: "Checks", value: term(STABILITY_STATES, stability.state).text },
          ]}
        />
        <DataTable
          label="Compared runs"
          className="page-table"
          rows={every}
          rowId={(side) => side.run.id}
          rowLabel={(side) => sideText(side)}
          columns={[
            { key: "run", header: "Run", priority: 1, minWidth: 14, flex: true, render: (side) => sideText(side) },
            { key: "result", header: "Result", priority: 1, minWidth: 8, render: (side) => (side.result ? term(RUN_RESULTS, side.result).text : "—") },
          ]}
          selected={null}
          onSelect={() => undefined}
          onOpen={onOpen}
        />
      </>
    );
    body = (
      <div className="object-page">
        <ValueRows
          label="Compared runs"
          rows={[
            {
              label: "Earlier",
              value: (
                <button type="button" className="link" onClick={() => onOpen(comparison.earlier.run.id)}>
                  {sideText(comparison.earlier)}
                </button>
              ),
            },
            {
              label: "Later",
              value: (
                <button type="button" className="link" onClick={() => onOpen(comparison.later.run.id)}>
                  {sideText(comparison.later)}
                </button>
              ),
            },
            ...(comparison.repeats.length > 0 ? [{ label: "Added", value: comparison.repeats.length === 1 ? "1 run" : `${comparison.repeats.length} runs` }] : []),
          ]}
        />
        <TaskTabs label="Comparison views" id="compare-views" tabs={tabs} selected={tab} onSelect={(next) => onView(next)}>
          {tab === "checks" ? checks : tab === "configuration" ? configuration : stabilityBody}
        </TaskTabs>
      </div>
    );
  }

  return {
    title: "Compare runs",
    actions: (
      <>
        <button type="button" onClick={() => setSheet("change")}>
          Change selection
        </button>
        <button type="button" disabled={!comparison || runs.length - 2 >= MAX_REPEATS || sameTest.length === 0} onClick={() => setSheet("add")}>
          Add runs
        </button>
      </>
    ),
    body: (
      <>
        {body}
        <RunChoice
          open={sheet === "change"}
          title="Change selection"
          candidates={candidates}
          chosen={runs.slice(0, 2)}
          exactly={2}
          onClose={() => setSheet(null)}
          onChoose={(ids) => {
            setSheet(null);
            onRuns(ids);
          }}
        />
        <RunChoice
          open={sheet === "add"}
          title="Add runs"
          candidates={sameTest}
          chosen={runs.slice(2)}
          most={MAX_REPEATS}
          onClose={() => setSheet(null)}
          onChoose={(ids) => {
            setSheet(null);
            onRuns([...runs.slice(0, 2), ...ids]);
          }}
        />
      </>
    ),
  };
}

/** A named multi-select of finished runs of a test. */
function RunChoice({
  open,
  title,
  candidates,
  chosen,
  exactly,
  most,
  onChoose,
  onClose,
}: {
  open: boolean;
  title: string;
  candidates: CatalogItem[];
  chosen: string[];
  exactly?: number;
  most?: number;
  onChoose: (ids: string[]) => void;
  onClose: () => void;
}) {
  const [draft, setDraft] = useState<string[]>(chosen);
  useEffect(() => {
    if (open) setDraft(chosen);
    // Starts from what is chosen each time it opens.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);
  const valid = exactly !== undefined ? draft.length === exactly : draft.length <= (most ?? Infinity);
  return (
    <FormDialog open={open} title={title} submitLabel="Compare" submitDisabled={!valid} onClose={onClose} onSubmit={() => (onChoose(draft), null)}>
      {candidates.length === 0 ? <p>No other finished runs of this test.</p> : null}
      <fieldset className="checks picker">
        <legend>Runs</legend>
        {candidates.map((item) => {
          const summary = item.summary.run;
          const label = [summary?.version ? `${item.name} · v${summary.version}` : item.name, startedText(summary?.started_at), summary?.environment_name].filter(Boolean).join(" · ");
          return (
            <label key={item.ref.id} className="check">
              <input
                type="checkbox"
                checked={draft.includes(item.ref.id)}
                disabled={!draft.includes(item.ref.id) && (exactly !== undefined ? draft.length >= exactly : draft.length >= (most ?? Infinity))}
                onChange={(event) => setDraft(event.target.checked ? [...draft, item.ref.id] : draft.filter((id) => id !== item.ref.id))}
              />
              {label}
            </label>
          );
        })}
      </fieldset>
    </FormDialog>
  );
}
