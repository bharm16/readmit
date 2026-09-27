// Findings: the saved analysis of the open case version as one list, the
// selected finding beside it, and Analyze, History, Analysis settings and
// Similar findings as named tasks. Opening the page reads what is saved; it
// never analyzes. A review decision is a local analyst decision saved as a new
// revision of the analysis's review; history is never rewritten.
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import {
  analyzeCase,
  findingReviewHistory,
  findSimilarFindings,
  listAnalysisProfiles,
  listCatalog,
  newIntentId,
  openCaseFindings,
  openItemDraft,
  previewFindingReview,
  readPreferences,
  RequestScope,
  savePreferences,
  saveItem,
  type AnalysisProfile,
  type AnalysisProfileRef,
  type CatalogItem,
  type DiagnoseConfig,
  type DiagnoseConfigNamespace,
  type DiagnosisFinding,
  type FindingDecision,
  type FindingReviewHistoryResult,
  type FindingScope,
  type FindingStatus,
  type FindingVerdict,
  type FindingsResult,
  type ItemRef,
  type SimilarResult,
} from "./bindings";
import { DataTable, type Column } from "./DataTable";
import { IconButton } from "./IconButton";
import { EmptyState, FormDialog, Menu, Modal, ValueRows, type SubmitFailure } from "./layout";
import { useLifecycle } from "./lifecycle";
import { listDate } from "./Projects";
import { saveProblem } from "./Environments";
import { useVocabulary } from "./vocabulary";
import { CLASSIFICATIONS, FIELD_STATES, FINDING_SCOPES, FINDING_VERDICTS as VERDICTS, SIMILAR_MEMBER_STATES as MEMBER_STATES } from "./display";
import "./findings.css";

const DECISIONS: { value: Exclude<FindingVerdict, "not_reviewed">; label: string }[] = [
  { value: "confirmed", label: "Confirm" },
  { value: "dismissed", label: "Dismiss" },
  { value: "suppressed", label: "Suppress" },
];

type Filter = { verdicts: string[]; rules: string[] };
const NO_FILTER: Filter = { verdicts: [], rules: [] };

function profileRef(profile: AnalysisProfile): AnalysisProfileRef {
  return profile.settings ? { settings: profile.settings } : { builtin: profile.builtin ?? "" };
}

export type FindingsProps = {
  root: string | null;
  caseRef: ItemRef | null;
  identity: string;
  shown: boolean;
  busy: boolean;
  /** Shows exactly these messages of the case, with the way back here. */
  onViewMessages: (occurrences: string[]) => void;
  onSimilar: () => void;
  /** Opens the test editor from a confirmed finding: its messages, its
   * provenance, and its expectations as undecided proposals. */
  onCreateTest?: (status: FindingStatus, review: string, reportSHA256: string, title: string) => void;
};

/** The Findings view's toolbar, body and selected-finding details. */
export function useFindings({ root, caseRef, identity, shown, busy, onViewMessages, onSimilar, onCreateTest }: FindingsProps) {
  const scope = useRef(new RequestScope());
  const context = useCallback(() => scope.current.enter(root ?? ""), [root]);
  const [result, setResult] = useState<FindingsResult | null>(null);
  const [findings, setFindings] = useState<DiagnosisFinding[]>([]);
  const [review, setReview] = useState<FindingReviewHistoryResult | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const [filter, setFilter] = useState<Filter>(NO_FILTER);
  const [sheet, setSheet] = useState<null | "filter" | "analyze" | "history" | "settings" | "review" | "details">(null);
  const [profiles, setProfiles] = useState<AnalysisProfile[] | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [unevaluated, setUnevaluated] = useState(false);
  const analyzing = useLifecycle<"analyzing">({ names: { analyzing: "analysis" } });
  const reads = useLifecycle<"reading">({ background: true });

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
    async (analysis?: ItemRef) => {
      if (!caseRef || !identity) return;
      await reads.run("reading", async (current) => {
        const answer = await openCaseFindings({ context: context(), case: caseRef, identity, ...(analysis ? { analysis } : {}), offset: 0 });
        if (!current()) return;
        setResult(answer);
        setFindings(answer.analysis?.diagnosis.findings ?? []);
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
  }, [caseRef?.id, identity]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    if (shown && result === null) void read();
  }, [shown, result, read]);

  const loadMore = async () => {
    const analysis = result?.analysis;
    if (!analysis || !caseRef || findings.length >= analysis.diagnosis.total) return;
    const answer = await openCaseFindings({ context: context(), case: caseRef, identity, analysis: analysis.ref, offset: findings.length });
    if (scope.current.current(answer) && answer.analysis?.ref.id === analysis.ref.id) setFindings((held) => [...held, ...(answer.analysis?.diagnosis.findings ?? [])]);
  };

  const runAnalysis = async (profile: AnalysisProfile) => {
    if (!caseRef) return;
    setSheet(null);
    setNotice(null);
    await analyzing.run("analyzing", async (current) => {
      const answer = await analyzeCase({ context: context(), case: caseRef, identity, profile: profileRef(profile), intent_id: newIntentId() });
      if (!current()) return;
      if (answer.state === "completed" || answer.state === "empty") {
        setResult(answer);
        setFindings(answer.analysis?.diagnosis.findings ?? []);
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
    if (compatible.length === 1) await runAnalysis(compatible[0]!);
    else setSheet("analyze");
  };

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

  if (!root || !caseRef) return { toolbar: null, body: null, details: null, selected: null };

  const analysis = result?.analysis;
  const running = analyzing.running !== null;
  const shownRows = findings.filter(
    (finding) => (filter.verdicts.length === 0 || filter.verdicts.includes(verdictOf(finding.id))) && (filter.rules.length === 0 || filter.rules.includes(finding.rule_id)),
  );
  const columns: Column<DiagnosisFinding>[] = [
    { key: "finding", header: "Finding", priority: 1, minWidth: 15, render: (finding) => ruleName(finding.rule_id) },
    { key: "classification", header: "Classification", priority: 3, minWidth: 7, render: (finding) => CLASSIFICATIONS[finding.classification] ?? "—" },
    { key: "review", header: "Review", priority: 1, minWidth: 8, render: (finding) => VERDICTS[verdictOf(finding.id)] ?? "—" },
  ];

  let body: ReactNode;
  if (result && result.state === "failed") {
    body = (
      <div className="empty-state" role="alert">
        <p className="empty-title">{result.reason ?? "The findings could not be read."}</p>
        <div className="empty-action">
          <button type="button" onClick={() => void read()}>
            Retry
          </button>
        </div>
      </div>
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
  } else if (analysis && (unevaluated || (findings.length === 0 && analysis.diagnosis.unsupported.length > 0))) {
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
  } else if (analysis && findings.length === 0) {
    body = <EmptyState title="No findings" />;
  } else if (analysis && shownRows.length === 0) {
    body = (
      <EmptyState
        title="No matching findings"
        action={
          <button type="button" onClick={() => setFilter(NO_FILTER)}>
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
          ...(CLASSIFICATIONS[finding.classification] ? [{ label: "Classification", value: CLASSIFICATIONS[finding.classification] }] : []),
          { label: "Review", value: VERDICTS[verdictOf(finding.id)] },
          ...finding.evidence.map((evidence, index) => ({
            label: `Evidence ${index + 1}`,
            value: `${evidence.field || "Message"}${evidence.state ? ` · ${FIELD_STATES[evidence.state]}` : ""}`,
          })),
        ]}
      />
      {finding.summary ? <p>{finding.summary}</p> : null}
      <div className="row-actions">
        <button type="button" disabled={finding.evidence.length === 0} onClick={() => onViewMessages([...new Set(finding.evidence.map((evidence) => evidence.occurrence))])}>
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
            <button type="button" className="quiet" onClick={() => void read()}>
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
    body: (
      <>
        {notice ? <p role="alert">{notice}</p> : null}
        <div aria-busy={running || undefined} className={running ? "historical" : undefined}>
          {body}
        </div>
        <FilterSheet open={sheet === "filter"} filter={filter} rules={[...new Map(findings.map((f) => [f.rule_id, ruleName(f.rule_id)])).entries()]} onClose={() => setSheet(null)} onApply={setFilter} />
        <AnalyzeSheet open={sheet === "analyze"} profiles={profiles ?? []} current={analysis?.config_sha256 ?? ""} onClose={() => setSheet(null)} onAnalyze={(profile) => void runAnalysis(profile)} />
        {sheet === "history" ? (
          <HistorySheet
            context={context}
            caseRef={caseRef}
            onClose={() => setSheet(null)}
            onOpen={(ref) => {
              setSheet(null);
              void read(ref);
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
                ...finding.evidence.map((evidence, index) => ({ label: `Evidence ${index + 1}`, value: `${evidence.occurrence} · ${evidence.field || "—"} · ${evidence.state}` })),
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

function AnalyzeSheet({ open, profiles, current, onClose, onAnalyze }: { open: boolean; profiles: AnalysisProfile[]; current: string; onClose: () => void; onAnalyze: (profile: AnalysisProfile) => void }) {
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

function HistorySheet({ context, caseRef, onClose, onOpen }: { context: () => import("./bindings").RequestContext; caseRef: ItemRef; onClose: () => void; onOpen: (ref: ItemRef) => void }) {
  const [items, setItems] = useState<CatalogItem[] | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  useEffect(() => {
    let live = true;
    void listCatalog({ context: context(), kind: "analysis", filter: { related_case: caseRef }, sort: "created" }).then((answer) => {
      if (!live) return;
      if (answer.state !== "completed" && answer.state !== "empty") {
        setFailure(answer.reason ?? "The history could not be read.");
        setItems([]);
        return;
      }
      const listed = answer.page?.items ?? [];
      // Newest first, undated last.
      setItems([...listed].sort((a, b) => (b.created_at ?? "").localeCompare(a.created_at ?? "")));
    });
    return () => {
      live = false;
    };
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
            if (item && item.summary.analysis?.form !== "grouping") onOpen(item.ref);
          }}
          loading={items === null}
          columns={[
            { key: "date", header: "Date", priority: 1, minWidth: 8, render: (item) => listDate(item.created_at) },
            {
              key: "profile",
              header: "Profile",
              priority: 1,
              minWidth: 8,
              render: (item) => (item.summary.analysis?.form === "grouping" ? "Similar findings" : item.summary.analysis?.profile_name || "—"),
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
  finding: DiagnosisFinding;
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
        <ValueRows rows={[{ label: "Reviewer", value: reviewer }]} />
      ) : reviewer === "" ? (
        <>
          <label htmlFor="finding-reviewer">Reviewer</label>
          <input id="finding-reviewer" type="text" maxLength={200} value={newReviewer} onChange={(event) => setNewReviewer(event.target.value)} />
        </>
      ) : null}
      <fieldset>
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
  const load = useCallback(async () => {
    const answer = await listCatalog({ context: context(), kind: "analysis-settings", filter: {} });
    setItems(answer.page?.items ?? []);
  }, [context]);
  useEffect(() => {
    void load();
  }, [load]);
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
      submitDisabled={!config || !supported || name.trim() === ""}
      onClose={onClose}
      status={problem ? <p role="alert">{problem}</p> : null}
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
export function useSimilarFindings({ root, caseRef, busy }: { root: string | null; caseRef: ItemRef | null; busy: boolean }) {
  const scope = useRef(new RequestScope());
  const context = useCallback(() => scope.current.enter(root ?? ""), [root]);
  const [cases, setCases] = useState<CatalogItem[]>([]);
  const [profiles, setProfiles] = useState<AnalysisProfile[]>([]);
  const [choosing, setChoosing] = useState(false);
  const [result, setResult] = useState<SimilarResult | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const comparing = useLifecycle<"comparing">({ names: { comparing: "analysis" } });

  useEffect(() => {
    if (!root) return;
    let live = true;
    void listCatalog({ context: context(), kind: "case", filter: {} }).then((answer) => {
      if (live) setCases(answer.page?.items ?? []);
    });
    if (caseRef)
      void listAnalysisProfiles({ context: context(), ref: caseRef }).then((answer) => {
        if (live) setProfiles(answer.profiles);
      });
    return () => {
      live = false;
    };
  }, [root, caseRef?.id]); // eslint-disable-line react-hooks/exhaustive-deps

  const nameOf = (ref: ItemRef) => cases.find((item) => item.ref.id === ref.id)?.name ?? ref.id;
  const group = result?.groups.find((entry) => entry.signature === selected) ?? null;
  const unanalyzed = (result?.members ?? []).filter((member) => member.state !== "analyzed");

  const [lastRequest, setLastRequest] = useState<{ cases: ItemRef[]; profile: AnalysisProfile } | null>(null);
  const [saved, setSaved] = useState<string | null>(null);
  const body = (
    <>
      {saved ? <p role="status">{saved}</p> : null}
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
              rowId={(member) => member.case.id}
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
          setSaved(null);
          await comparing.run("comparing", async (current) => {
            const answer = await findSimilarFindings({ context: context(), cases: chosen, profile: profileRef(profile), save: false });
            if (current()) setResult(answer);
          });
        }}
      />
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
        {group ? <ValueRows rows={group.cases.map((ref) => ({ label: nameOf(ref), value: String(group.members.filter((member) => member.case.id === ref.id).length) }))} /> : null}
      </Modal>
    </>
  );
  return {
    title: "Similar findings",
    actions: (
      <>
        {result && result.state === "completed" && lastRequest ? (
          <button
            type="button"
            disabled={busy || comparing.running !== null}
            onClick={() =>
              void comparing.run("comparing", async (current) => {
                const answer = await findSimilarFindings({ context: context(), cases: lastRequest.cases, profile: profileRef(lastRequest.profile), save: true });
                if (!current()) return;
                setResult(answer);
                setSaved(answer.saved ? "Saved to each case's history." : (answer.reason ?? "Not saved."));
              })
            }
          >
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
