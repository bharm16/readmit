// Findings: the saved analysis of the open case version as one list, the
// selected finding beside it, and Analyze, History, Analysis settings and
// Similar findings as named tasks. Opening the page reads what is saved; it
// never analyzes. A review decision is a local analyst decision saved as a new
// revision of the analysis's review; history is never rewritten.
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import {
  analyzeCase,
  exportAnalysisSettings,
  findingReviewHistory,
  importAnalysisSettings,
  findSimilarFindings,
  listAnalysisProfiles,
  listWholeCatalog,
  newIntentId,
  openCaseFindings,
  openItemDraft,
  openSimilarFindings,
  previewFindingReview,
  readMessages,
  readPreferences,
  RequestScope,
  savePreferences,
  saveItem,
  type AnalysisProfile,
  type AnalysisProfileRef,
  type CatalogItem,
  type DiagnoseConfig,
  type DiagnoseConfigNamespace,
  type DiagnosisSeverity,
  type FindingDecision,
  type FindingReviewHistoryResult,
  type FindingRow,
  type FindingScope,
  type FindingStatus,
  type FindingVerdict,
  type FindingsResult,
  type ItemRef,
  type MessageRow,
  type SimilarResult,
} from "./bindings";
import { DataTable, firstShownRow, ReturnAnchor, type Column } from "./DataTable";
import { IconButton } from "./IconButton";
import { EmptyState, folderName, FormDialog, Menu, Modal, ValueRows, type SubmitFailure } from "./layout";
import { useLifecycle } from "./lifecycle";
import { listDate } from "./Projects";
import { saveProblem } from "./Environments";
import { useVocabulary } from "./vocabulary";
import { CLASSIFICATIONS, FIELD_STATES, FINDING_SCOPES, FINDING_VERDICTS as VERDICTS, SEVERITIES, SIMILAR_MEMBER_STATES as MEMBER_STATES } from "./display";
import { NO_QUERY, rowType, sourceLabel, timeOfDay } from "./Messages";
import "./findings.css";

const DECISIONS: { value: Exclude<FindingVerdict, "not_reviewed">; label: string }[] = [
  { value: "confirmed", label: "Confirm" },
  { value: "dismissed", label: "Dismiss" },
  { value: "suppressed", label: "Suppress" },
];

type Filter = { severities: DiagnosisSeverity[]; verdicts: string[]; rules: string[] };
const NO_FILTER: Filter = { severities: [], verdicts: [], rules: [] };

/** One piece of evidence: the message it is in and the field in it. */
export type EvidenceRef = { occurrence: string; field: string };

function profileRef(profile: AnalysisProfile): AnalysisProfileRef {
  return profile.settings ? { settings: profile.settings } : { builtin: profile.builtin ?? "" };
}

export type FindingsProps = {
  root: string | null;
  caseRef: ItemRef | null;
  /** The open case's name and its entry in the project, which its messages
   * are read by. */
  caseName: string;
  caseEntry: string;
  identity: string;
  shown: boolean;
  busy: boolean;
  /** Shows exactly these messages of the case with the first one's field
   * selected, and the way back here. */
  onViewMessages: (evidence: EvidenceRef[]) => void;
  onSimilar: () => void;
  /** Opens a saved comparison of similar findings. */
  onOpenComparison: (ref: ItemRef) => void;
  /** Opens the test editor from a confirmed finding: its messages, its
   * provenance, and its expectations as undecided proposals. */
  onCreateTest?: (status: FindingStatus, review: string, reportSHA256: string, title: string) => void;
};

/** The Findings view's toolbar, body and selected-finding details. */
export function useFindings({ root, caseRef, caseName, caseEntry, identity, shown, busy, onViewMessages, onSimilar, onOpenComparison, onCreateTest }: FindingsProps) {
  const scope = useRef(new RequestScope());
  const context = useCallback(() => scope.current.enter(root ?? ""), [root]);
  const [result, setResult] = useState<FindingsResult | null>(null);
  const [findings, setFindings] = useState<FindingRow[]>([]);
  const [review, setReview] = useState<FindingReviewHistoryResult | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const [filter, setFilter] = useState<Filter>(NO_FILTER);
  const [sheet, setSheet] = useState<null | "filter" | "analyze" | "history" | "settings" | "review" | "details">(null);
  const [profiles, setProfiles] = useState<AnalysisProfile[] | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [unevaluated, setUnevaluated] = useState(false);
  // Analyze found no profile this case can be analyzed under.
  const [noProfile, setNoProfile] = useState(false);
  // The messages the selected finding's evidence is in, read in the background.
  const [evidenceRows, setEvidenceRows] = useState<{ finding: string; rows: MessageRow[]; reason?: string } | null>(null);
  const evidenceReads = useLifecycle<"messages">({ background: true });
  // Where the list was when View messages left it: the panel is mounted
  // again on the way back, and scrolls to the same first row.
  const listArea = useRef<HTMLDivElement | null>(null);
  const [returnTo, setReturnTo] = useState<{ anchor?: string | undefined; at?: object }>({});
  const viewMessages = (evidence: EvidenceRef[]) => {
    const list = listArea.current?.querySelector<HTMLElement>(".table-view");
    setReturnTo({ anchor: list ? firstShownRow(list) : undefined, at: {} });
    onViewMessages(evidence);
  };
  const analyzing = useLifecycle<"analyzing">({ names: { analyzing: "analysis" } });
  const reads = useLifecycle<"reading">({ background: true });
  // The analysis last asked for: the current one, or one opened from History.
  const requested = useRef<ItemRef | undefined>(undefined);

  const readReview = useCallback(
    async (answer: FindingsResult) => {
      if (!answer.analysis) {
        setReview(null);
        return;
      }
      setReview(await findingReviewHistory({ context: context(), ref: answer.analysis.ref }));
    },
    [context],
  );

  const read = useCallback(
    async (analysis?: ItemRef, severities: DiagnosisSeverity[] = []) => {
      if (!caseRef || !identity) return;
      requested.current = analysis;
      await reads.run("reading", async (current) => {
        const answer = await openCaseFindings({
          context: context(),
          case: caseRef,
          identity,
          ...(analysis ? { analysis } : {}),
          offset: 0,
          ...(severities.length > 0 ? { severities } : {}),
        });
        if (!current()) return;
        setResult(answer);
        setFindings(answer.analysis?.findings ?? []);
        setSelected(null);
        await readReview(answer);
      });
    },
    [caseRef?.id, identity, context, readReview], // eslint-disable-line react-hooks/exhaustive-deps
  );

  useEffect(() => {
    // Answers in flight belong to the case being left.
    analyzing.withdraw();
    reads.withdraw();
    setResult(null);
    setFindings([]);
    setReview(null);
    setFilter(NO_FILTER);
    setNotice(null);
    setUnevaluated(false);
    setNoProfile(false);
    setEvidenceRows(null);
  }, [caseRef?.id, identity]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    if (shown && result === null) void read();
  }, [shown, result, read]);

  const loadingMore = useRef(false);
  const loadMore = async () => {
    const analysis = result?.analysis;
    if (!analysis || !caseRef || findings.length >= analysis.matching || loadingMore.current) return;
    loadingMore.current = true;
    try {
      const answer = await openCaseFindings({
        context: context(),
        case: caseRef,
        identity,
        analysis: analysis.ref,
        offset: findings.length,
        ...(filter.severities.length > 0 ? { severities: filter.severities } : {}),
      });
      if (scope.current.current(answer) && answer.analysis?.ref.id === analysis.ref.id) setFindings((held) => [...held, ...(answer.analysis?.findings ?? [])]);
    } finally {
      loadingMore.current = false;
    }
  };

  // Review and Rule filter the rows read so far, so while either is applied
  // the rest of the analysis is read too: a match beyond the first window is
  // never reported as no match.
  const clientFiltered = filter.rules.length > 0 || filter.verdicts.length > 0;
  useEffect(() => {
    if (clientFiltered && result?.analysis && findings.length < result.analysis.matching) void loadMore();
  }, [clientFiltered, findings.length, result]); // eslint-disable-line react-hooks/exhaustive-deps

  const runAnalysis = async (profile: AnalysisProfile) => {
    if (!caseRef) return;
    setSheet(null);
    setNotice(null);
    await analyzing.run("analyzing", async (current) => {
      const answer = await analyzeCase({ context: context(), case: caseRef, identity, profile: profileRef(profile), intent_id: newIntentId() });
      if (!current()) return;
      if ((answer.state === "completed" || answer.state === "empty") && filter.severities.length > 0) {
        // The new analysis is shown under the severities chosen.
        await read(undefined, filter.severities);
      } else if (answer.state === "completed" || answer.state === "empty") {
        setResult(answer);
        setFindings(answer.analysis?.findings ?? []);
        setSelected(null);
        await readReview(answer);
      } else if (answer.state === "cancelled") {
        setNotice("Analysis stopped. Nothing was saved.");
      } else {
        setNotice(answer.reason ?? "The case was not analyzed.");
      }
    });
  };

  const analyze = async () => {
    if (!caseRef) return;
    const answer = await listAnalysisProfiles({ context: context(), ref: caseRef });
    if (answer.state !== "completed" && answer.state !== "empty") {
      setNotice(answer.reason ?? "The analysis profiles could not be read.");
      return;
    }
    const offered = answer.profiles;
    setProfiles(offered);
    const compatible = offered.filter((profile) => profile.compatible);
    setNoProfile(compatible.length === 0);
    if (compatible.length === 1) await runAnalysis(compatible[0]!);
    else if (compatible.length > 1 || result?.analysis) setSheet("analyze");
  };

  // The selected finding's messages, whether or not the grid has read them.
  const selectedFinding = findings.find((entry) => entry.id === selected) ?? null;
  useEffect(() => {
    evidenceReads.withdraw();
    if (!selectedFinding || !root || !caseEntry) {
      setEvidenceRows(null);
      return;
    }
    const id = selectedFinding.id;
    const occurrences = [...new Set(selectedFinding.evidence.map((evidence) => evidence.occurrence).filter((occurrence) => occurrence !== ""))];
    if (occurrences.length === 0) {
      setEvidenceRows({ finding: id, rows: [] });
      return;
    }
    void evidenceReads.run("messages", async (current) => {
      const answer = await readMessages({ workspace: root, case: caseEntry, identity, query: NO_QUERY, sort: "", offset: 0, limit: 0, occurrences });
      if (current()) setEvidenceRows({ finding: id, rows: answer.state === "completed" ? answer.rows : [], ...(answer.state === "completed" ? {} : { reason: answer.reason ?? "The messages could not be read." }) });
    });
  }, [selectedFinding?.id, root, caseEntry, identity]); // eslint-disable-line react-hooks/exhaustive-deps

  const statusOf = (id: string): FindingStatus | undefined => review?.statuses.find((status) => status.finding === id);
  const verdictOf = (id: string) => statusOf(id)?.verdict ?? "not_reviewed";
  const ruleName = (id: string) => result?.rules.find((rule) => rule.id === id)?.name ?? id;
  const decisions: FindingDecision[] = review?.revisions[0]?.decisions ?? [];

  /** Saves the review with this finding's decision replaced, or removed. */
  const saveDecision = async (finding: string, decision: FindingDecision | null): Promise<SubmitFailure | null> => {
    const analysis = result?.analysis;
    if (!analysis) return { reason: "No analysis is open." };
    const next = decisions.filter((entry) => entry.finding !== finding);
    if (decision) next.push(decision);
    const answer = await saveItem({
      context: context(),
      kind: "finding-review",
      ...(review?.review ? { item: review.review.id } : {}),
      ...(review?.review?.revision ? { base_revision: review.review.revision } : {}),
      draft: { finding_review: { analysis: analysis.ref, report_sha256: analysis.report_sha256, decisions: next } },
      intent_id: newIntentId(),
    });
    if (answer.outcome !== "saved") return saveProblem(answer, {});
    await readReview({ ...result });
    return null;
  };

  /** Applies a filter; a changed severity reads the analysis again, since the
   * facade sorts and filters by severity across every finding. */
  const applyFilter = (next: Filter) => {
    const changed = JSON.stringify(next.severities) !== JSON.stringify(filter.severities);
    setFilter(next);
    if (changed) void read(result?.analysis && !result.analysis.current ? result.analysis.ref : undefined, next.severities);
  };

  if (!root || !caseRef) return { toolbar: null, body: null, details: null, selected: null, close: () => undefined };

  const analysis = result?.analysis;
  const running = analyzing.running !== null;
  const shownRows = findings.filter(
    (finding) => (filter.verdicts.length === 0 || filter.verdicts.includes(verdictOf(finding.id))) && (filter.rules.length === 0 || filter.rules.includes(finding.rule_id)),
  );
  const columns: Column<FindingRow>[] = [
    { key: "finding", header: "Finding", priority: 1, minWidth: 15, render: (finding) => ruleName(finding.rule_id) },
    { key: "severity", header: "Severity", priority: 2, minWidth: 7, render: (finding) => (finding.severity ? SEVERITIES[finding.severity] : "—") },
    { key: "review", header: "Review", priority: 1, minWidth: 8, render: (finding) => VERDICTS[verdictOf(finding.id)] ?? "—" },
  ];

  let body: ReactNode;
  if (result && result.state === "failed") {
    body = (
      <div className="empty-state" role="alert">
        <p className="empty-title">{result.reason ?? "The findings could not be read."}</p>
        <div className="empty-action">
          <button type="button" onClick={() => void read(requested.current, filter.severities)}>
            Retry
          </button>
        </div>
      </div>
    );
  } else if (result && !analysis && noProfile) {
    body = (
      <EmptyState
        title="No supported profile"
        action={
          <button type="button" onClick={() => setSheet("analyze")}>
            Choose profile
          </button>
        }
      />
    );
  } else if (result && !analysis) {
    body = (
      <EmptyState
        title="Not analyzed"
        action={
          <button type="button" className="primary" disabled={busy || running} onClick={() => void analyze()}>
            Analyze
          </button>
        }
      />
    );
  } else if (analysis && (unevaluated || (analysis.diagnosis.total === 0 && analysis.diagnosis.unsupported.length > 0))) {
    body = (
      <DataTable
        label="Unevaluated evidence"
        className="page-table"
        rows={analysis.diagnosis.unsupported.map((item, index) => ({ ...item, key: String(index) }))}
        rowId={(item) => item.key}
        rowLabel={(item) => item.detail}
        selected={null}
        onSelect={() => {}}
        onOpen={() => {}}
        columns={[
          { key: "detail", header: "Not evaluated", priority: 1, minWidth: 15, render: (item) => item.detail },
          { key: "field", header: "Field", priority: 2, minWidth: 8, render: (item) => item.field || "—" },
        ]}
      />
    );
  } else if (analysis && analysis.diagnosis.total === 0) {
    body = <EmptyState title="No findings" />;
  } else if (analysis && shownRows.length === 0) {
    body = (
      <EmptyState
        title="No matching findings"
        action={
          <button type="button" onClick={() => applyFilter(NO_FILTER)}>
            Clear filters
          </button>
        }
      />
    );
  } else {
    body = (
      <DataTable
        label="Findings"
        className="page-table"
        rows={shownRows}
        rowId={(finding) => finding.id}
        rowLabel={(finding) => ruleName(finding.rule_id)}
        columns={columns}
        selected={selected}
        onSelect={setSelected}
        onOpen={setSelected}
        loading={result === null}
        onNearEnd={() => void loadMore()}
      />
    );
  }

  const finding = findings.find((entry) => entry.id === selected) ?? null;
  const status = finding ? statusOf(finding.id) : undefined;
  // Each revision that changed this finding's decision: a decision made, or
  // one withdrawn (Undo), newest first.
  const history: { revision: NonNullable<typeof review>["revisions"][number]; text: string; reason: string }[] = [];
  if (finding) {
    const revisions = review?.revisions ?? [];
    revisions.forEach((revision, index) => {
      const mine = revision.decisions.find((decision) => decision.finding === finding.id);
      const before = revisions[index + 1]?.decisions.find((decision) => decision.finding === finding.id);
      if (mine && JSON.stringify(mine) !== JSON.stringify(before)) history.push({ revision, text: VERDICTS[mine.verdict], reason: mine.rationale });
      else if (!mine && before) history.push({ revision, text: "Undone", reason: "" });
    });
  }
  const current = finding ? decisions.find((decision) => decision.finding === finding.id) : undefined;

  const details = finding ? (
    <section className="finding-details" aria-label="Finding">
      <header className="section-header">
        <h2>{ruleName(finding.rule_id)}</h2>
        <IconButton icon="close" label="Close finding" onClick={() => setSelected(null)} />
      </header>
      <ValueRows
        rows={[
          ...(finding.severity ? [{ label: "Severity", value: SEVERITIES[finding.severity] }] : []),
          ...(CLASSIFICATIONS[finding.classification] ? [{ label: "Classification", value: CLASSIFICATIONS[finding.classification] }] : []),
          { label: "Review", value: VERDICTS[verdictOf(finding.id)] },
          ...finding.evidence.map((evidence, index) => ({
            label: `Evidence ${index + 1}`,
            value: `${fieldName(evidence.field, finding.labels)}${evidence.state ? ` · ${FIELD_STATES[evidence.state]}` : ""}`,
          })),
        ]}
      />
      {finding.summary ? <p>{finding.summary}</p> : null}
      {evidenceRows?.finding === finding.id && evidenceRows.reason ? <p className="row-reason">{evidenceRows.reason}</p> : null}
      {evidenceRows?.finding === finding.id && evidenceRows.rows.length > 0 ? (
        <>
        <h3>Messages</h3>
        <ul className="evidence-messages" aria-label="Messages">
          {evidenceRows.rows.map((row) => (
            <li key={row.id}>
              <button
                type="button"
                className="link"
                onClick={() => viewMessages(finding.evidence.filter((evidence) => evidence.occurrence === row.id).map(({ occurrence, field }) => ({ occurrence, field })))}
              >
                {[row.observed_at ? timeOfDay(row.observed_at) : "", rowType(row), sourceLabel(row)].filter((part) => part !== "").join(" · ")}
              </button>
            </li>
          ))}
        </ul>
        </>
      ) : null}
      <div className="row-actions">
        <button
          type="button"
          disabled={finding.evidence.length === 0}
          onClick={() => viewMessages(finding.evidence.map(({ occurrence, field }) => ({ occurrence, field })))}
        >
          View messages
        </button>
        <button type="button" className="primary" disabled={busy || running || !analysis?.current} onClick={() => setSheet("review")}>
          Review
        </button>
        {status?.verdict === "confirmed" && onCreateTest && analysis ? (
          <button
            type="button"
            disabled={busy || running || !status.promotion}
            onClick={() => onCreateTest(status, review?.review?.id ?? "", analysis.report_sha256, ruleName(finding.rule_id))}
          >
            Create test
          </button>
        ) : null}
        <Menu label="More finding actions" items={[{ label: "Details", onSelect: () => setSheet("details") }]} />
      </div>
      {history.length > 0 ? (
        <>
          <h3>History</h3>
          <ul className="plain-list decision-history">
            {history.map(({ revision, text, reason }) => (
              <li key={revision.revision}>
                {text} · {revision.author || "—"} · {listDate(revision.published_at)}
                {reason ? <span className="row-reason">{reason}</span> : null}
              </li>
            ))}
          </ul>
          {current && analysis?.current ? (
            <button
              type="button"
              className="quiet"
              disabled={busy || running}
              onClick={() =>
                void saveDecision(finding.id, null).then((failure) => {
                  if (failure) setNotice(failure.reason);
                })
              }
            >
              Undo decision
            </button>
          ) : null}
        </>
      ) : null}
      {status && status.verdict === "confirmed" && !status.promotion && status.next_evidence ? <p className="row-reason">{status.next_evidence}</p> : null}
    </section>
  ) : null;

  const toolbar = (
    <div className="toolbar list-toolbar">
      <div className="toolbar-group">
        <IconButton icon="filter" label="Filter findings" onClick={() => setSheet("filter")} />
        {analysis && analysis.diagnosis.unsupported.length > 0 ? (
          <button type="button" className="chip" aria-pressed={unevaluated} onClick={() => setUnevaluated(!unevaluated)}>
            Unevaluated {analysis.diagnosis.unsupported.length}
          </button>
        ) : null}
        {analysis && !analysis.current ? (
          <span className="chip">
            {listDate(analysis.created_at)} · {analysis.profile_name}
            <button type="button" className="quiet" onClick={() => void read(undefined, filter.severities)}>
              Show current
            </button>
          </span>
        ) : null}
      </div>
      <div className="toolbar-group">
        {running ? (
          <>
            <span role="status">Analyzing…</span>
            <button type="button" onClick={analyzing.cancel}>
              Stop
            </button>
          </>
        ) : analysis || result === null ? (
          <button type="button" className="primary" disabled={busy} onClick={() => void analyze()}>
            Analyze
          </button>
        ) : null}
        <Menu
          label="More findings actions"
          items={[
            { label: "History", onSelect: () => setSheet("history") },
            { label: "Analysis settings", onSelect: () => setSheet("settings") },
            { label: "Similar findings", onSelect: onSimilar },
          ]}
        />
      </div>
    </div>
  );

  return {
    toolbar,
    selected,
    details,
    close: () => setSelected(null),
    body: (
      <>
        {notice ? <p role="alert">{notice}</p> : null}
        <div ref={listArea} aria-busy={running || undefined} className={running ? "historical" : undefined}>
          <ReturnAnchor.Provider value={returnTo}>{body}</ReturnAnchor.Provider>
        </div>
        <FilterSheet open={sheet === "filter"} filter={filter} onApply={applyFilter} rules={[...new Map((result?.rules ?? []).filter((rule) => rule.ruleset === result?.analysis?.diagnosis.ruleset).map((rule) => [rule.id, rule.name])).entries()]} onClose={() => setSheet(null)} />
        <AnalyzeSheet
          open={sheet === "analyze"}
          profiles={profiles ?? []}
          current={analysis?.config_sha256 ?? ""}
          version={`${caseName}${caseRef.revision ? ` · v${caseRef.revision}` : ""}`} onClose={() => setSheet(null)} onAnalyze={(profile) => void runAnalysis(profile)} />
        {sheet === "history" ? (
          <HistorySheet
            context={context}
            caseRef={caseRef}
            onClose={() => setSheet(null)}
            onOpen={(ref) => {
              setSheet(null);
              void read(ref, filter.severities);
            }}
            onOpenComparison={(ref) => {
              setSheet(null);
              onOpenComparison(ref);
            }}
          />
        ) : null}
        {sheet === "settings" ? <SettingsSheet context={context} rules={result?.rules ?? []} onClose={() => setSheet(null)} /> : null}
        {finding && analysis && sheet === "review" ? (
          <ReviewSheet
            finding={finding}
            title={ruleName(finding.rule_id)}
            current={current}
            preview={async (decision) => {
              const next = [...decisions.filter((entry) => entry.finding !== finding.id), decision];
              const answer = await previewFindingReview({
                context: context(),
                kind: "finding-review",
                ...(review?.review ? { item: review.review.id } : {}),
                draft: { finding_review: { analysis: analysis.ref, report_sha256: analysis.report_sha256, decisions: next } },
              });
              return answer.effects.find((effect) => effect.finding === finding.id)?.findings.length ?? null;
            }}
            onClose={() => setSheet(null)}
            onSave={async (decision) => {
              const failure = await saveDecision(finding.id, decision);
              if (!failure) setSheet(null);
              return failure;
            }}
          />
        ) : null}
        <Modal
          open={sheet === "details" && finding !== null}
          title="Details"
          onClose={() => setSheet(null)}
          footer={
            <div className="dialog-footer">
              <button type="button" onClick={() => setSheet(null)}>
                Close
              </button>
            </div>
          }
        >
          {finding ? (
            <ValueRows
              rows={[
                { label: "Finding", value: finding.id },
                { label: "Rule", value: finding.rule_id },
                { label: "Ruleset", value: finding.ruleset },
                { label: "Profile", value: finding.profile },
                ...finding.evidence.map((evidence, index) => ({
                  label: `Evidence ${index + 1}`,
                  value: `${evidence.occurrence} · ${evidence.field || "—"}${evidence.state ? ` · ${FIELD_STATES[evidence.state]}` : ""}`,
                })),
              ]}
            />
          ) : null}
        </Modal>
      </>
    ),
  };
}

function FilterSheet({ open, filter, rules, onClose, onApply }: { open: boolean; filter: Filter; rules: [string, string][]; onClose: () => void; onApply: (filter: Filter) => void }) {
  const [draft, setDraft] = useState(filter);
  useEffect(() => {
    if (open) setDraft(filter);
  }, [open, filter]);
  const toggle = (list: string[], value: string) => (list.includes(value) ? list.filter((v) => v !== value) : [...list, value]);
  return (
    <FormDialog
      open={open}
      title="Filter findings"
      size="small"
      submitLabel="Apply"
      onClose={onClose}
      onSubmit={() => {
        onApply(draft);
        onClose();
      }}
    >
      <fieldset className="checks">
        <legend>Severity</legend>
        {(Object.keys(SEVERITIES) as DiagnosisSeverity[]).map((value) => (
          <label key={value} className="check">
            <input
              type="checkbox"
              checked={draft.severities.includes(value)}
              onChange={() => setDraft({ ...draft, severities: draft.severities.includes(value) ? draft.severities.filter((v) => v !== value) : [...draft.severities, value] })}
            />
            {SEVERITIES[value]}
          </label>
        ))}
      </fieldset>
      <fieldset className="checks">
        <legend>Review</legend>
        {Object.entries(VERDICTS).map(([value, label]) => (
          <label key={value} className="check">
            <input type="checkbox" checked={draft.verdicts.includes(value)} onChange={() => setDraft({ ...draft, verdicts: toggle(draft.verdicts, value) })} />
            {label}
          </label>
        ))}
      </fieldset>
      {rules.length > 0 ? (
        <fieldset className="checks">
          <legend>Rule</legend>
          {rules.map(([id, name]) => (
            <label key={id} className="check">
              <input type="checkbox" checked={draft.rules.includes(id)} onChange={() => setDraft({ ...draft, rules: toggle(draft.rules, id) })} />
              {name}
            </label>
          ))}
        </fieldset>
      ) : null}
    </FormDialog>
  );
}

function AnalyzeSheet({
  open,
  profiles,
  current,
  version,
  onClose,
  onAnalyze,
}: {
  open: boolean;
  profiles: AnalysisProfile[];
  current: string;
  /** The case version analyzed, as a person reads it. */
  version: string;
  onClose: () => void;
  onAnalyze: (profile: AnalysisProfile) => void;
}) {
  const compatible = profiles.filter((profile) => profile.compatible);
  const [chosen, setChosen] = useState("");
  const keyOf = (profile: AnalysisProfile) => profile.settings?.id ?? `builtin:${profile.builtin ?? ""}`;
  useEffect(() => {
    if (open) setChosen(keyOf(compatible.find((profile) => profile.config_sha256 === current) ?? compatible[0] ?? ({} as AnalysisProfile)));
  }, [open]); // eslint-disable-line react-hooks/exhaustive-deps
  const profile = compatible.find((entry) => keyOf(entry) === chosen);
  return (
    <FormDialog
      open={open}
      title="Analyze"
      size="small"
      submitLabel="Analyze"
      submitDisabled={!profile}
      onClose={onClose}
      onSubmit={() => {
        if (profile) onAnalyze(profile);
      }}
    >
      <ValueRows rows={[{ label: "Case", value: version }]} />
      {compatible.length === 0 ? (
        <>
          <p className="empty-title">No supported profile</p>
          <ul className="plain-list">
            {profiles.map((entry) => (
              <li key={keyOf(entry)}>
                {entry.name}
                <span className="row-reason">{entry.refusals.map((refusal) => refusal.detail).join(" ")}</span>
              </li>
            ))}
          </ul>
        </>
      ) : (
        <>
          <label htmlFor="analyze-profile">Profile</label>
          <select id="analyze-profile" value={chosen} onChange={(event) => setChosen(event.target.value)}>
            {compatible.map((entry) => (
              <option key={keyOf(entry)} value={keyOf(entry)}>
                {entry.name}
              </option>
            ))}
          </select>
        </>
      )}
    </FormDialog>
  );
}

function HistorySheet({
  context,
  caseRef,
  onClose,
  onOpen,
  onOpenComparison,
}: {
  context: () => import("./bindings").RequestContext;
  caseRef: ItemRef;
  onClose: () => void;
  onOpen: (ref: ItemRef) => void;
  onOpenComparison: (ref: ItemRef) => void;
}) {
  const [items, setItems] = useState<CatalogItem[] | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const reads = useLifecycle<"reading">({ background: true });
  useEffect(() => {
    void reads.run("reading", async (current) => {
      const answer = await listWholeCatalog({ context: context(), kind: "analysis", filter: { related_case: caseRef }, sort: "created" });
      if (!current()) return;
      if (answer.state !== "completed" && answer.state !== "empty") {
        setFailure(answer.reason ?? "The history could not be read.");
        setItems([]);
        return;
      }
      const listed = answer.page?.items ?? [];
      // Newest first, undated last.
      setItems([...listed].sort((a, b) => (b.created_at ?? "").localeCompare(a.created_at ?? "")));
    });
  }, []); // eslint-disable-line react-hooks/exhaustive-deps
  return (
    <Modal
      open
      title="History"
      size="normal"
      onClose={onClose}
      footer={
        <div className="dialog-footer">
          <button type="button" onClick={onClose}>
            Close
          </button>
        </div>
      }
    >
      {failure ? (
        <p role="alert">{failure}</p>
      ) : items && items.length === 0 ? (
        <p>No analyses</p>
      ) : (
        <DataTable
          label="Analyses"
          className="values-table"
          rows={items ?? []}
          rowId={(item) => item.ref.id}
          rowLabel={(item) => item.name}
          selected={null}
          onSelect={() => {}}
          onOpen={(id) => {
            const item = items?.find((entry) => entry.ref.id === id);
            if (!item) return;
            if (item.summary.analysis?.form === "grouping") onOpenComparison(item.ref);
            else onOpen(item.ref);
          }}
          loading={items === null}
          columns={[
            { key: "date", header: "Date", priority: 1, minWidth: 8, render: (item) => listDate(item.created_at) },
            {
              key: "profile",
              header: "Profile",
              priority: 1,
              minWidth: 8,
              render: (item) => (item.summary.analysis?.form === "grouping" ? `Similar findings · ${item.name}` : item.summary.analysis?.profile_name || "—"),
            },
            { key: "findings", header: "Findings", priority: 2, minWidth: 6, render: (item) => String(item.summary.analysis?.findings ?? 0) },
          ]}
        />
      )}
    </Modal>
  );
}

function ReviewSheet({
  finding,
  title,
  current,
  preview,
  onClose,
  onSave,
}: {
  finding: FindingRow;
  title: string;
  current: FindingDecision | undefined;
  preview: (decision: FindingDecision) => Promise<number | null>;
  onClose: () => void;
  onSave: (decision: FindingDecision) => Promise<SubmitFailure | null>;
}) {
  const [verdict, setVerdict] = useState<Exclude<FindingVerdict, "not_reviewed">>(current && current.verdict !== "not_reviewed" ? current.verdict : "confirmed");
  const [scope, setScope] = useState<FindingScope>(current?.scope ?? "finding");
  const [reason, setReason] = useState(current?.rationale ?? "");
  const [covers, setCovers] = useState<number | null>(null);
  // The local name decisions are recorded under; asked for only when none is set.
  const [reviewer, setReviewer] = useState<string | null>(null);
  const [newReviewer, setNewReviewer] = useState("");
  useEffect(() => {
    void readPreferences().then((answer) => setReviewer(answer.preferences.reviewer ?? ""));
  }, []);
  const decision = (): FindingDecision => ({ finding: finding.id, verdict, ...(verdict === "suppressed" ? { scope } : {}), rationale: reason.trim() });
  useEffect(() => {
    setCovers(null);
    if (verdict !== "suppressed") return;
    let live = true;
    void preview({ finding: finding.id, verdict, scope, rationale: reason.trim() || "-" }).then((count) => {
      if (live) setCovers(count);
    });
    return () => {
      live = false;
    };
  }, [verdict, scope]); // eslint-disable-line react-hooks/exhaustive-deps
  return (
    <FormDialog
      open
      title={`Review ${title}`}
      submitLabel="Save"
      submitDisabled={reason.trim() === ""}
      dirty={verdict !== (current?.verdict ?? "confirmed") || scope !== (current?.scope ?? "finding") || reason !== (current?.rationale ?? "")}
      onClose={onClose}
      onSubmit={async () => {
        if (reviewer === "" && newReviewer.trim() !== "") {
          const saved = await readPreferences();
          const answer = await savePreferences({ ...saved.preferences, reviewer: newReviewer.trim() });
          if (answer.state !== "completed") return { reason: answer.reason ?? "The reviewer name was not saved.", field: "finding-reviewer" };
        }
        return onSave(decision());
      }}
    >
      {reviewer ? (
        <ValueRows rows={[{ label: "Reviewer", value: `${reviewer} · This computer` }]} />
      ) : reviewer === "" ? (
        <>
          <label htmlFor="finding-reviewer">Reviewer on this computer</label>
          <input id="finding-reviewer" type="text" maxLength={200} value={newReviewer} onChange={(event) => setNewReviewer(event.target.value)} />
        </>
      ) : null}
      <fieldset className="checks">
        <legend>Decision</legend>
        {DECISIONS.map((choice) => (
          <label key={choice.value} className="check">
            <input type="radio" name="finding-decision" checked={verdict === choice.value} onChange={() => setVerdict(choice.value)} />
            {choice.label}
          </label>
        ))}
      </fieldset>
      {verdict === "suppressed" ? (
        <>
          <label htmlFor="finding-scope">Scope</label>
          <select id="finding-scope" value={scope} onChange={(event) => setScope(event.target.value as FindingScope)}>
            {(Object.keys(FINDING_SCOPES) as FindingScope[]).map((choice) => (
              <option key={choice} value={choice}>
                {FINDING_SCOPES[choice]}
              </option>
            ))}
          </select>
          {covers !== null ? <p className="consequence">Covers {covers === 1 ? "1 finding" : `${covers} findings`}.</p> : null}
        </>
      ) : null}
      <label htmlFor="finding-reason">Reason</label>
      <textarea id="finding-reason" rows={3} value={reason} onChange={(event) => setReason(event.target.value)} />
    </FormDialog>
  );
}

/** Named analysis settings: profile, rules and namespaces, saved as one version. */
function SettingsSheet({ context, rules, onClose }: { context: () => import("./bindings").RequestContext; rules: { id: string; ruleset: string; name: string }[]; onClose: () => void }) {
  const builtins = useVocabulary()?.diagnosis_builtins ?? [];
  const [items, setItems] = useState<CatalogItem[] | null>(null);
  const [chosen, setChosen] = useState<string>("");
  const [ref, setRef] = useState<ItemRef | null>(null);
  const [name, setName] = useState("");
  const [config, setConfig] = useState<DiagnoseConfig | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  const [exported, setExported] = useState<string | null>(null);
  const reads = useLifecycle<"reading">({ background: true });
  // The saved settings are read once, when the sheet opens.
  useEffect(() => {
    void reads.run("reading", async (current) => {
      const answer = await listWholeCatalog({ context: context(), kind: "analysis-settings", filter: {} });
      if (!current()) return;
      if (answer.state !== "completed" && answer.state !== "empty") setProblem(answer.reason ?? "The analysis settings could not be read.");
      setItems(answer.page?.items ?? []);
    });
  }, []); // eslint-disable-line react-hooks/exhaustive-deps
  const open = async (id: string) => {
    setChosen(id);
    setProblem(null);
    const item = items?.find((entry) => entry.ref.id === id);
    const answer = await openItemDraft({ context: context(), ref: item ? item.ref : { kind: "analysis-settings", id: "" } });
    if (answer.state !== "completed" || !answer.draft?.analysis_settings) {
      setProblem(answer.reason ?? "These settings cannot be read.");
      setConfig(null);
      return;
    }
    setRef(answer.ref && answer.ref.id ? answer.ref : null);
    setName(item?.name ?? "");
    setConfig(answer.draft.analysis_settings);
  };
  // Import reads a settings file into this sheet as new, unsaved settings,
  // exactly as written, supported or not.
  const importSettings = async () => {
    setProblem(null);
    setExported(null);
    const answer = await importAnalysisSettings(context());
    if (answer.state === "cancelled") return;
    if (answer.state !== "completed" || !answer.draft?.analysis_settings) {
      setProblem(answer.reason ?? "The settings file cannot be read.");
      return;
    }
    setChosen("new");
    setRef(null);
    setName(answer.draft.name ?? "");
    setConfig(answer.draft.analysis_settings);
  };
  const exportSettings = async () => {
    if (!ref) return;
    setProblem(null);
    setExported(null);
    const answer = await exportAnalysisSettings({ context: context(), ref });
    if (answer.state === "cancelled") return;
    if (answer.state !== "completed") setProblem(answer.reason ?? "The settings were not exported.");
    else if (answer.path) setExported(`Exported ${folderName(answer.path)}`);
  };
  const builtin = config ? builtins.find((entry) => entry.profile === config.profile && entry.ruleset === config.ruleset) : undefined;
  const supported = config === null || builtin !== undefined;
  const rulesHere = rules.filter((rule) => rule.ruleset === config?.ruleset);
  const setNamespace = (index: number, patch: Partial<DiagnoseConfigNamespace>) =>
    config && setConfig({ ...config, namespaces: config.namespaces.map((entry, at) => (at === index ? { ...entry, ...patch } : entry)) });
  return (
    <FormDialog
      open
      title="Analysis settings"
      submitLabel="Save"
      submitDisabled={!config || name.trim() === ""}
      onClose={onClose}
      status={problem ? <p role="alert">{problem}</p> : exported ? <p role="status">{exported}</p> : null}
      onSubmit={async () => {
        if (!config) return { reason: "Choose settings." };
        const answer = await saveItem({
          context: context(),
          kind: "analysis-settings",
          ...(ref ? { item: ref.id, ...(ref.revision ? { base_revision: ref.revision } : {}) } : {}),
          draft: { name: name.trim(), analysis_settings: config },
          intent_id: newIntentId(),
        });
        if (answer.outcome !== "saved") return saveProblem(answer, { name: "analysis-settings-name" });
        onClose();
        return null;
      }}
    >
      <label htmlFor="analysis-settings-choice">Settings</label>
      <div className="value-with-action">
      <select id="analysis-settings-choice" value={chosen} onChange={(event) => void open(event.target.value)}>
        <option value="" disabled>
          Choose settings
        </option>
        {(items ?? []).map((item) => (
          <option key={item.ref.id} value={item.ref.id}>
            {item.name}
          </option>
        ))}
        <option value="new">New settings</option>
      </select>
      <Menu
        label="More settings actions"
        items={[
          { label: "Import settings…", onSelect: () => void importSettings() },
          { label: "Export settings…", onSelect: () => void exportSettings(), disabled: ref === null },
        ]}
      />
      </div>
      {config ? (
        <>
          <label htmlFor="analysis-settings-name">Name</label>
          <input id="analysis-settings-name" type="text" maxLength={200} value={name} onChange={(event) => setName(event.target.value)} />
          {!supported ? (
            <ValueRows
              rows={[
                { label: "Profile", value: `${config.profile} (unsupported)` },
                { label: "Ruleset", value: config.ruleset },
                { label: "Rules", value: config.rules.length === 0 ? "All" : config.rules.join(", ") },
                ...config.namespaces.map((entry) => ({ label: entry.key || "Namespace", value: [entry.namespace, entry.universal_id, entry.universal_id_type].filter((part) => part).join(" · ") || "—" })),
              ]}
            />
          ) : (
            <>
              <label htmlFor="analysis-settings-profile">Profile</label>
              <select
                id="analysis-settings-profile"
                value={builtin?.id ?? ""}
                onChange={(event) => {
                  const next = builtins.find((entry) => entry.id === event.target.value);
                  if (next) setConfig({ ...config, profile: next.profile, ruleset: next.ruleset, rules: [] });
                }}
              >
                {builtins.map((entry) => (
                  <option key={entry.id} value={entry.id}>
                    {entry.name}
                  </option>
                ))}
              </select>
              {rulesHere.length > 0 ? (
                <fieldset className="checks">
                  <legend>Rules</legend>
                  {rulesHere.map((rule) => (
                    <label key={rule.id} className="check">
                      <input
                        type="checkbox"
                        checked={config.rules.length === 0 || config.rules.includes(rule.id)}
                        onChange={() => {
                          const all = config.rules.length === 0 ? rulesHere.map((entry) => entry.id) : config.rules;
                          const next = all.includes(rule.id) ? all.filter((id) => id !== rule.id) : [...all, rule.id];
                          setConfig({ ...config, rules: next.length === rulesHere.length ? [] : next });
                        }}
                      />
                      {rule.name}
                    </label>
                  ))}
                </fieldset>
              ) : null}
              <fieldset>
                <legend>Namespaces</legend>
                {config.namespaces.map((entry, index) => (
                  <div key={index} className="namespace-row">
                    <input aria-label={`Name ${index + 1}`} type="text" value={entry.key} onChange={(event) => setNamespace(index, { key: event.target.value })} />
                    <input aria-label={`Namespace ${index + 1}`} type="text" value={entry.namespace} onChange={(event) => setNamespace(index, { namespace: event.target.value })} />
                    <input aria-label={`Universal ID ${index + 1}`} type="text" value={entry.universal_id} onChange={(event) => setNamespace(index, { universal_id: event.target.value })} />
                    <input aria-label={`ID type ${index + 1}`} type="text" value={entry.universal_id_type} onChange={(event) => setNamespace(index, { universal_id_type: event.target.value })} />
                    <IconButton icon="close" label={`Remove namespace ${index + 1}`} onClick={() => setConfig({ ...config, namespaces: config.namespaces.filter((_, at) => at !== index) })} />
                  </div>
                ))}
                <button
                  type="button"
                  className="quiet"
                  onClick={() => setConfig({ ...config, namespaces: [...config.namespaces, { key: "", namespace: "", universal_id: "", universal_id_type: "" }] })}
                >
                  Add namespace
                </button>
              </fieldset>
            </>
          )}
        </>
      ) : null}
    </FormDialog>
  );
}

// ---------- Similar findings ----------

/** Similar findings: a project child page grouping the findings of chosen cases. */
export function useSimilarFindings({
  root,
  caseRef,
  saved: savedId,
  busy,
  onViewEvidence,
}: {
  root: string | null;
  caseRef: ItemRef | null;
  /** A saved comparison to show, by its identity. */
  saved: string | undefined;
  busy: boolean;
  /** Opens a member case on exactly these messages. */
  onViewEvidence: (caseRef: ItemRef, occurrences: string[]) => void;
}) {
  const scope = useRef(new RequestScope());
  const context = useCallback(() => scope.current.enter(root ?? ""), [root]);
  const [cases, setCases] = useState<CatalogItem[]>([]);
  const [profiles, setProfiles] = useState<AnalysisProfile[]>([]);
  const [choosing, setChoosing] = useState(false);
  const [result, setResult] = useState<SimilarResult | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const comparing = useLifecycle<"comparing">({ names: { comparing: "analysis" } });
  const [lastRequest, setLastRequest] = useState<{ cases: ItemRef[]; profile: AnalysisProfile } | null>(null);
  const [naming, setNaming] = useState(false);

  const reads = useLifecycle<"cases" | "profiles" | "saved">({ background: true });
  const [failure, setFailure] = useState<string | null>(null);

  useEffect(() => {
    reads.withdraw();
    setFailure(null);
    if (!root) return;
    void reads.run("cases", async (current) => {
      const answer = await listWholeCatalog({ context: context(), kind: "case", filter: {} });
      if (!current()) return;
      if (answer.state !== "completed" && answer.state !== "empty") setFailure(answer.reason ?? "The project's cases could not be read.");
      setCases(answer.page?.items ?? []);
    });
    if (caseRef)
      void reads.run("profiles", async (current) => {
        const answer = await listAnalysisProfiles({ context: context(), ref: caseRef });
        if (!current()) return;
        if (answer.state !== "completed" && answer.state !== "empty") setFailure(answer.reason ?? "The analysis profiles could not be read.");
        setProfiles(answer.profiles);
      });
  }, [root, caseRef?.id]); // eslint-disable-line react-hooks/exhaustive-deps

  // A saved comparison opens as it was saved; nothing is compared again.
  useEffect(() => {
    if (!root || !savedId) return;
    setSelected(null);
    setLastRequest(null);
    void reads.run("saved", async (current) => {
      const answer = await openSimilarFindings({ context: context(), ref: { kind: "analysis", id: savedId } });
      if (current()) setResult(answer);
    });
  }, [root, savedId]); // eslint-disable-line react-hooks/exhaustive-deps

  const nameOf = (ref: ItemRef) => cases.find((item) => item.ref.id === ref.id)?.name ?? ref.id;
  const group = result?.groups.find((entry) => entry.signature === selected) ?? null;
  const members = result?.members ?? [];
  const unanalyzed = members.map((member, at) => ({ ...member, at })).filter((member) => member.state !== "analyzed");

  const body = (
    <>
      {failure ? <p role="alert">{failure}</p> : null}
      {comparing.running ? (
        <p role="status">
          Comparing…{" "}
          <button type="button" onClick={comparing.cancel}>
            Stop
          </button>
        </p>
      ) : null}
      {result === null ? (
        <EmptyState
          title="No comparison"
          action={
            <button type="button" className="primary" disabled={busy} onClick={() => setChoosing(true)}>
              Choose cases
            </button>
          }
        />
      ) : result.state === "failed" ? (
        <p role="alert">{result.reason ?? "The cases were not compared."}</p>
      ) : (
        <>
          {result.groups.length === 0 ? (
            <p className="empty-title">No similar findings</p>
          ) : (
            <DataTable
              label="Similar findings"
              className="page-table"
              rows={result.groups}
              rowId={(entry) => entry.signature}
              rowLabel={(entry) => entry.rule_name || entry.rule_id}
              selected={selected}
              onSelect={setSelected}
              onOpen={setSelected}
              columns={[
                { key: "finding", header: "Finding", priority: 1, minWidth: 15, render: (entry) => entry.rule_name || "—" },
                { key: "cases", header: "Cases", priority: 1, minWidth: 6, render: (entry) => String(entry.cases.length) },
                { key: "classification", header: "Classification", priority: 2, minWidth: 8, render: (entry) => CLASSIFICATIONS[entry.classification] },
              ]}
            />
          )}
          {unanalyzed.length > 0 ? (
            <DataTable
              label="Cases not compared"
              className="values-table"
              rows={unanalyzed}
              rowId={(member) => String(member.at)}
              rowLabel={(member) => member.name}
              selected={null}
              onSelect={() => {}}
              onOpen={() => {}}
              columns={[
                { key: "case", header: "Case", priority: 1, minWidth: 12, render: (member) => member.name },
                { key: "state", header: "State", priority: 1, minWidth: 8, render: (member) => MEMBER_STATES[member.state] },
                { key: "reason", header: "Reason", priority: 2, minWidth: 12, render: (member) => member.reason ?? "" },
              ]}
            />
          ) : null}
        </>
      )}
      <ChooseCasesSheet
        open={choosing}
        cases={cases}
        current={caseRef}
        profiles={profiles.filter((profile) => profile.compatible)}
        onClose={() => setChoosing(false)}
        onCompare={async (chosen, profile) => {
          setChoosing(false);
          setSelected(null);
          setLastRequest({ cases: chosen, profile });
          await comparing.run("comparing", async (current) => {
            const answer = await findSimilarFindings({ context: context(), cases: chosen, profile: profileRef(profile), save: false });
            if (current()) setResult(answer);
          });
        }}
      />
      {lastRequest ? (
        <SaveComparisonSheet
          open={naming}
          uncompared={unanalyzed.length}
          onClose={() => setNaming(false)}
          onSave={async (name) => {
            let failure: SubmitFailure | null = null;
            await comparing.run("comparing", async (current) => {
              const answer = await findSimilarFindings({ context: context(), cases: lastRequest.cases, profile: profileRef(lastRequest.profile), save: true, name });
              if (!current()) return;
              if (!answer.saved) {
                failure = { reason: answer.reason ?? "Not saved.", field: "comparison-name" };
                return;
              }
              setResult(answer);
              setNaming(false);
            });
            return failure;
          }}
        />
      ) : null}
      <Modal
        open={group !== null}
        title={group?.rule_name || "Group"}
        onClose={() => setSelected(null)}
        footer={
          <div className="dialog-footer">
            <button type="button" onClick={() => setSelected(null)}>
              Close
            </button>
          </div>
        }
      >
        {group
          ? [...new Set(group.members.map((member) => member.member))].map((position) => {
              const compared = members[position];
              const found = group.members.filter((member) => member.member === position);
              const occurrences = [...new Set(found.flatMap((member) => member.occurrences))];
              const name = compared?.name || (compared ? nameOf(compared.case) : "");
              return (
                <section key={position} className="similar-member" aria-label={name}>
                  <header className="section-header">
                    <h3>{name}</h3>
                    {compared?.state === "analyzed" ? (
                      <button type="button" disabled={busy || occurrences.length === 0} onClick={() => onViewEvidence(compared.case, occurrences)}>
                        View messages
                      </button>
                    ) : null}
                  </header>
                  <p className="row-reason">{compared?.state === "analyzed" ? (found.length === 1 ? "1 finding" : `${found.length} findings`) : (compared?.reason ?? "")}</p>
                </section>
              );
            })
          : null}
      </Modal>
    </>
  );
  return {
    title: result?.name ? `Similar findings · ${result.name}` : "Similar findings",
    actions: (
      <>
        {result && result.state === "completed" && lastRequest && !result.saved ? (
          <button type="button" disabled={busy || comparing.running !== null} onClick={() => setNaming(true)}>
            Save comparison
          </button>
        ) : null}
        <button type="button" disabled={busy || comparing.running !== null} onClick={() => setChoosing(true)}>
          Choose cases
        </button>
      </>
    ),
    body,
  };
}

function ChooseCasesSheet({
  open,
  cases,
  current,
  profiles,
  onClose,
  onCompare,
}: {
  open: boolean;
  cases: CatalogItem[];
  current: ItemRef | null;
  profiles: AnalysisProfile[];
  onClose: () => void;
  onCompare: (cases: ItemRef[], profile: AnalysisProfile) => void;
}) {
  const [chosen, setChosen] = useState<Set<string>>(new Set());
  const [profile, setProfile] = useState(0);
  useEffect(() => {
    if (open) {
      setChosen(new Set(current ? [current.id] : []));
      setProfile(0);
    }
  }, [open]); // eslint-disable-line react-hooks/exhaustive-deps
  return (
    <FormDialog
      open={open}
      title="Choose cases"
      size="wide"
      submitLabel="Compare"
      submitDisabled={chosen.size === 0 || profiles.length === 0}
      onClose={onClose}
      onSubmit={() => {
        const picked = profiles[profile];
        if (picked) onCompare(cases.filter((item) => chosen.has(item.ref.id)).map((item) => item.ref), picked);
      }}
    >
      <fieldset className="checks">
        <legend>Cases</legend>
        {cases.map((item) => (
          <label key={item.ref.id} className="check">
            <input
              type="checkbox"
              checked={chosen.has(item.ref.id)}
              onChange={() =>
                setChosen((held) => {
                  const next = new Set(held);
                  if (next.has(item.ref.id)) next.delete(item.ref.id);
                  else next.add(item.ref.id);
                  return next;
                })
              }
            />
            {item.name}
          </label>
        ))}
      </fieldset>
      <label htmlFor="similar-profile">Profile</label>
      <select id="similar-profile" value={profile} onChange={(event) => setProfile(Number(event.target.value))}>
        {profiles.map((entry, index) => (
          <option key={index} value={index}>
            {entry.name}
          </option>
        ))}
      </select>
    </FormDialog>
  );
}

/** A field as the analysis names it: its label and path, or the path alone
 * when no label applies, or Message for evidence about a whole message. */
function fieldName(field: string, labels: Record<string, string>): string {
  if (!field) return "Message";
  const label = labels[field];
  return label ? `${label} (${field})` : field;
}

/** Names a comparison before it is saved to each compared case's history. */
function SaveComparisonSheet({
  open,
  uncompared,
  onClose,
  onSave,
}: {
  open: boolean;
  /** Cases chosen but not compared, which the saved comparison does not keep. */
  uncompared: number;
  onClose: () => void;
  onSave: (name: string) => Promise<SubmitFailure | null>;
}) {
  const [name, setName] = useState("");
  useEffect(() => {
    if (open) setName("");
  }, [open]);
  return (
    <FormDialog
      open={open}
      title="Save comparison"
      size="small"
      submitLabel="Save"
      submitDisabled={name.trim() === ""}
      dirty={name.trim() !== ""}
      onClose={onClose}
      onSubmit={() => onSave(name.trim())}
    >
      <label htmlFor="comparison-name">Name</label>
      <input id="comparison-name" type="text" maxLength={200} value={name} onChange={(event) => setName(event.target.value)} />
      {uncompared > 0 ? <p className="consequence">{uncompared === 1 ? "The case not compared is not saved with it." : `The ${uncompared} cases not compared are not saved with it.`}</p> : null}
    </FormDialog>
  );
}
