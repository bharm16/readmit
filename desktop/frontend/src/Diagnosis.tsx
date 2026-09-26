import { useEffect, useRef, useState } from "react";
import {
  openFindingDecisions,
  saveFindingDecisions,
  type Diagnosis as DiagnosisView,
  type DiagnosisFinding,
  type DiagnosisGroupsResult,
  type DiagnosisRequest,
  type DiagnosisResult,
  type FindingDecision,
  type FindingDecisionsResult,
  type FindingReviewRequest,
  type FindingReviewResult,
  type FindingStatus,
  type GroupDiagnosesRequest,
} from "./bindings";
import { DiagnoseConfigEditor } from "./RulesEditor";
import { Report, type Indicators } from "./shell";
import "./diagnosis.css";
import { useLifecycle } from "./lifecycle";
import { useVocabulary } from "./vocabulary";

/** The verdicts a person can record about one finding, in the review engine's
 * own vocabulary. A finding nobody decided stays not reviewed, which is not a
 * decision and never becomes one implicitly. */
const VERDICTS = ["confirmed", "dismissed", "suppressed"] as const;

/** The scopes a suppression can cover, in the review engine's own vocabulary. */
const SCOPES = ["finding", "occurrence", "case"] as const;

/** How a review's verdicts and bases read. Both vocabularies are the engine's. */
const VERDICT_WORDS: Record<string, string> = {
  confirmed: "Confirmed",
  dismissed: "Dismissed",
  suppressed: "Suppressed",
  not_reviewed: "Not reviewed",
};

const BASIS_WORDS: Record<string, string> = {
  decision: "by an explicit decision",
  scope: "by the scope of another decision",
  unreviewed: "nobody has decided about it",
};

type DecisionDraft = { verdict: string; scope: string; rationale: string };

const EMPTY_DECISION: DecisionDraft = { verdict: "", scope: "finding", rationale: "" };

/** The decisions a document holds as the per-finding controls hold them. A
 * decision without a scope is not a suppression, so the scope control keeps
 * its default until one is chosen. */
function draftsOf(decisions: FindingDecision[]): Record<string, DecisionDraft> {
  return Object.fromEntries(
    decisions.map((decision) => [
      decision.finding,
      { verdict: decision.verdict, scope: decision.scope ?? "finding", rationale: decision.rationale },
    ]),
  );
}

/** The decisions document the controls compose: the report they were typed
 * against, by identity, and every decision a person made. The Go reader the
 * command line applies validates it when it is saved. */
function composeDecisions(reportSHA256: string, decisions: FindingDecision[]): string {
  return JSON.stringify(
    { schema: "readmit-finding-decisions/v1", report_sha256: reportSHA256, decisions },
    null,
    2,
  );
}

/** The configuration selection: exactly one built-in or one workspace entry. */
function parseConfig(chosen: string): { builtin: string; config: string } {
  if (chosen.startsWith("builtin:")) return { builtin: chosen.slice("builtin:".length), config: "" };
  if (chosen.startsWith("config:")) return { builtin: "", config: chosen.slice("config:".length) };
  return { builtin: "", config: "" };
}

/** The findings of one report grouped by equal signature — the rule and the
 * classification — so a recurring shape reads as one group. Grouping hides
 * nothing: every finding stays listed inside its group. */
function groupFindings(findings: DiagnosisFinding[]): { signature: string; members: DiagnosisFinding[] }[] {
  const groups = new Map<string, DiagnosisFinding[]>();
  for (const finding of findings) {
    const signature = `${finding.rule_id} · ${finding.classification}`;
    const members = groups.get(signature) ?? [];
    members.push(finding);
    groups.set(signature, members);
  }
  return [...groups.entries()].map(([signature, members]) => ({ signature, members }));
}

/** One verified case diagnosed under one explicitly chosen configuration, and
 * the explicit review of what it found.
 *
 * Nothing is decided here. The facade runs the same engine `readmit diagnose`
 * runs and writes the same report directory; the review joins the analyst's
 * typed decisions to that exact report through the same reader
 * `readmit diagnose review` uses. Every fact, violation and hypothesis links
 * to its evidence: opening an occurrence selects it, so the inspector beside
 * this panel reads the original bytes. A finding nobody explicitly confirmed
 * promotes nothing, and an unsupported promotion is shown with its reasons
 * rather than weakened into an implicit expectation. A decisions document is
 * applied only to the report it names, because finding identifiers name other
 * findings in any other report. */
export function Diagnosis({
  workspace,
  caseName,
  identity,
  configEntries,
  reportEntries,
  groupsReportEntries,
  caseEntries,
  decisionsEntries = [],
  result,
  groupsResult,
  reviewResult,
  busy: windowBusy,
  progress,
  groupsProgress = null,
  indicators,
  onRun,
  onOpen,
  onGroup,
  onOpenGroups,
  onReview,
  onSelect,
  onPromote,
  onManageProfiles,
  onSaved,
}: {
  workspace: string;
  /** The verified case this panel diagnoses, and the identity every request
   * binds to, so nothing is read out of evidence that has changed since. */
  caseName: string;
  identity: string;
  /** The entries of the open workspace declaring the diagnose-config contract. */
  configEntries: string[];
  /** The retained diagnosis report directories of the open workspace. */
  reportEntries: string[];
  /** Retained grouping report directories, opened with their own reader. */
  groupsReportEntries: string[];
  /** The case bundles of the open workspace, for grouping recurring findings. */
  caseEntries: string[];
  /** The entries of the open workspace declaring the finding-decisions contract. */
  decisionsEntries?: string[];
  result: DiagnosisResult | null;
  groupsResult: DiagnosisGroupsResult | null;
  reviewResult: FindingReviewResult | null;
  busy: boolean;
  progress: string | null;
  /** What a grouping running now is doing, shown beside the grouping. */
  groupsProgress?: string | null;
  indicators: Indicators;
  onRun: (request: DiagnosisRequest) => void;
  onOpen: (entry: string, offset: number) => void;
  onGroup: (request: GroupDiagnosesRequest) => void;
  onOpenGroups: (entry: string, offset: number) => void;
  onReview: (request: FindingReviewRequest, write: boolean) => void;
  onSelect: (occurrence: string) => void;
  /** Drafts a regression test from one explicitly confirmed finding's
   * promotion, carrying where it came from. */
  onPromote: (status: FindingStatus, reviewEntry: string, reportSHA256: string) => void;
  /** Focuses the interface-profile editor. Diagnosis configurations name the
   * engine's bundled profiles; local interface profiles are managed there. */
  onManageProfiles?: () => void;
  /** Called after an authored configuration or decisions document landed, so
   * the pickers offer it. */
  onSaved?: () => void;
}) {
  // The built-in configurations a diagnosis can run under and how many
  // findings one window asks for, as the facade publishes them. Choosing a
  // configuration is explicit and nothing is chosen implicitly.
  const vocabulary = useVocabulary();
  const builtins = vocabulary?.diagnosis_builtins ?? [];
  const page = vocabulary?.bounds.diagnosis ?? 0;
  const [chosen, setChosen] = useState("");
  const [output, setOutput] = useState("");
  const [report, setReport] = useState("");
  const [reportEntry, setReportEntry] = useState("");
  const [grouped, setGrouped] = useState<string[]>([]);
  const [groupsReportEntry, setGroupsReportEntry] = useState("");
  const [openedGroupsEntry, setOpenedGroupsEntry] = useState("");
  // The grouping the groups on screen answer, so their next window is of the
  // same cases under the same configuration whatever the form holds now.
  const [groupedRequest, setGroupedRequest] = useState<GroupDiagnosesRequest | null>(null);
  const [decisions, setDecisions] = useState<Record<string, DecisionDraft>>({});
  const [reviewOutput, setReviewOutput] = useState("");
  const [decisionsOutput, setDecisionsOutput] = useState("");
  // The decisions a review was last asked about, so a preview is shown only
  // while it is still a preview of the decisions on screen.
  const [reviewedDecisions, setReviewedDecisions] = useState<string | null>(null);
  // The standalone decisions document: the entry to open, the new entry to
  // save into, and what the last open or save answered.
  const [decisionsEntry, setDecisionsEntry] = useState("");
  const [newDecisionsEntry, setNewDecisionsEntry] = useState("");
  const [decisionsResult, setDecisionsResult] = useState<FindingDecisionsResult | null>(null);
  const [decisionsFrom, setDecisionsFrom] = useState("");
  // Whether the decisions on screen changed since they were last opened,
  // saved or recorded. Opening a document replaces them, so that asks first.
  const [unsaved, setUnsaved] = useState(false);
  const [confirming, setConfirming] = useState<string | null>(null);
  // What the panel itself is waiting on the application for: an open or a
  // save of a decisions document. Its controls wait with it.
  const lifecycle = useLifecycle<string>();
  const pending = lifecycle.running;
  const busy = windowBusy || pending !== null;
  const keep = useRef<HTMLButtonElement | null>(null);
  const openDecisionsButton = useRef<HTMLButtonElement | null>(null);

  const diagnosis: DiagnosisView | null = result?.diagnosis ?? null;

  // A new report is a new finding list, so decisions typed about the previous
  // one are not silently rebound to findings they were never about.
  const reportIsNew = diagnosis?.report_sha256 ?? "";
  useEffect(() => {
    setDecisions({});
    setUnsaved(false);
    setConfirming(null);
    setDecisionsResult(null);
  }, [reportIsNew]);

  useEffect(() => {
    if (confirming !== null) keep.current?.focus();
  }, [confirming]);

  // Once the decisions are saved or recorded there is nothing left to ask about.
  useEffect(() => {
    if (!unsaved) setConfirming(null);
  }, [unsaved]);

  // Recording persists the decisions document too, so nothing is left unsaved.
  const recorded = reviewResult?.decisions_output ?? "";
  useEffect(() => {
    if (recorded !== "") setUnsaved(false);
  }, [recorded]);

  const decide = (finding: string, change: Partial<DecisionDraft>) => {
    setDecisions((current) => ({
      ...current,
      [finding]: { ...EMPTY_DECISION, ...current[finding], ...change },
    }));
    setUnsaved(true);
  };

  const forget = (finding: string) => {
    setDecisions((current) => Object.fromEntries(Object.entries(current).filter(([id]) => id !== finding)));
    setUnsaved(true);
  };

  /** One typed decision per finding a person actually decided about. A finding
   * with no verdict is not in this list, so reviewing records nothing for it. */
  const typedDecisions = (): FindingDecision[] =>
    Object.entries(decisions).flatMap(([finding, draft]) => {
      if (draft.verdict === "") return [];
      const decision: FindingDecision = {
        finding,
        verdict: draft.verdict,
        rationale: draft.rationale,
      };
      if (draft.verdict === "suppressed") decision.scope = draft.scope;
      return [decision];
    });
  const decided = typedDecisions();
  const decidedKey = JSON.stringify(decided);

  const review = (write: boolean) => {
    if (!diagnosis) return;
    const request: FindingReviewRequest = {
      workspace,
      case: caseName,
      identity,
      report,
      report_sha256: diagnosis.report_sha256,
      decisions: decided,
      offset: 0,
    };
    if (write) {
      request.output = reviewOutput;
      request.decisions_output = decisionsOutput;
    }
    setReviewedDecisions(decidedKey);
    onReview(request, write);
  };

  // A retained document's decisions become the per-finding decisions only
  // when it names the report on screen. One recorded against another report
  // is shown for what it is and applied to nothing.
  const openDecisions = async (name: string) => {
    if (!diagnosis) return;
    const shown = diagnosis.report_sha256;
    const opened = await lifecycle.run(`Opening ${name}.`, () => openFindingDecisions(workspace, name));
    if (!opened) return;
    setDecisionsResult(opened);
    setDecisionsFrom(name);
    if (opened.state !== "completed" || !opened.decisions) return;
    if (opened.decisions.report_sha256 !== shown) return;
    setDecisions(draftsOf(opened.decisions.decisions));
    setUnsaved(false);
  };

  const saveDecisions = async () => {
    if (!diagnosis) return;
    const name = newDecisionsEntry;
    const saved = await lifecycle.run(`Saving ${name}.`, () =>
      saveFindingDecisions({
        workspace,
        document: composeDecisions(diagnosis.report_sha256, decided),
        output: name,
      }),
    );
    if (!saved) return;
    setDecisionsResult(saved);
    if (saved.state === "completed" && saved.output) {
      setNewDecisionsEntry("");
      setUnsaved(false);
      onSaved?.();
    }
  };

  const keepDecisions = () => {
    setConfirming(null);
    openDecisionsButton.current?.focus();
  };

  /** What the last open or save of a decisions document answered. */
  const decisionsStatus = (): string => {
    if (pending !== null) return pending;
    if (!decisionsResult) return "";
    if (decisionsResult.reason) return decisionsResult.reason;
    if (decisionsResult.state !== "completed") return decisionsResult.state;
    const bytes = decisionsResult.sha256 ?? "";
    if (decisionsResult.output) return `Saved to ${decisionsResult.output} · exact bytes hash to ${bytes}`;
    const named = decisionsResult.decisions?.report_sha256 ?? "";
    if (diagnosis && named !== diagnosis.report_sha256) {
      return `The decisions in ${decisionsFrom} were recorded against a different diagnosis report (${named}); finding identifiers name other findings there, so none of them is applied to this report.`;
    }
    return `Opened ${decisionsFrom} · exact bytes hash to ${bytes}`;
  };

  // A report of another case names that case's occurrences, which the open
  // case's inspector must never read as its own.
  const foreign = diagnosis !== null && diagnosis.case_identity !== identity;
  const listed = new Set(diagnosis?.findings.map((finding) => finding.id) ?? []);
  const decidedOutsideWindow = decided.filter((decision) => !listed.has(decision.finding));

  const record = reviewResult?.review?.record ?? null;
  // A recorded review is retained and stays what it is; a preview is shown
  // only while the decisions on screen are the ones it previewed.
  const reviewCurrent = Boolean(reviewResult?.output) || reviewedDecisions === decidedKey;

  return (
    <section className="diagnosis" aria-label="Diagnosis">
      <h3>Diagnosis</h3>
      <p className="hint">
        Run one supported diagnosis over this verified case under a configuration you choose, or
        reopen a retained report. Findings describe the capture window, never a complete lifecycle,
        and every finding links to the evidence it was found in. Deciding about findings is a
        separate explicit review; nothing is confirmed, dismissed or suppressed implicitly.
      </p>

      <form
        onSubmit={(event) => {
          event.preventDefault();
          const { builtin, config } = parseConfig(chosen);
          setReport(output);
          onRun({
            workspace,
            case: caseName,
            identity,
            ...(config !== "" ? { config } : {}),
            ...(builtin !== "" ? { builtin } : {}),
            output,
            offset: 0,
          });
        }}
      >
        <label htmlFor="diagnosis-config">Configuration</label>
        <select
          id="diagnosis-config"
          value={chosen}
          disabled={busy}
          onChange={(event) => setChosen(event.target.value)}
        >
          <option value="">Choose a configuration…</option>
          <optgroup label="Built-in configurations">
            {builtins.map((builtin) => (
              <option key={builtin.id} value={`builtin:${builtin.id}`}>
                {builtin.id} — {builtin.profile} · {builtin.ruleset}
              </option>
            ))}
          </optgroup>
          <optgroup label="Workspace configurations">
            {configEntries.map((entry) => (
              <option key={entry} value={`config:${entry}`}>
                {entry}
              </option>
            ))}
          </optgroup>
        </select>
        <label htmlFor="diagnosis-output">New report directory in this workspace</label>
        <input
          id="diagnosis-output"
          required
          value={output}
          disabled={busy}
          onChange={(event) => setOutput(event.target.value)}
        />
        <button type="submit" disabled={busy || chosen === "" || output === ""}>
          Diagnose
        </button>
      </form>

      {onManageProfiles ? (
        <p className="hint">
          A configuration names one of the engine's bundled fixture profiles.{" "}
          <button type="button" disabled={busy} onClick={onManageProfiles}>
            Profiles
          </button>{" "}
          edits local interface profiles, which are a separate document.
        </p>
      ) : null}

      <details>
        <summary>Diagnosis settings</summary>
        <DiagnoseConfigEditor
          workspace={workspace}
          entries={configEntries}
          busy={busy}
          {...(onSaved ? { onSaved } : {})}
        />
      </details>

      <form
        onSubmit={(event) => {
          event.preventDefault();
          setReport(reportEntry);
          onOpen(reportEntry, 0);
        }}
      >
        <label htmlFor="diagnosis-report">Retained diagnosis report</label>
        <select
          id="diagnosis-report"
          value={reportEntry}
          disabled={busy || reportEntries.length === 0}
          onChange={(event) => setReportEntry(event.target.value)}
        >
          <option value="">Choose a retained report…</option>
          {reportEntries.map((entry) => (
            <option key={entry} value={entry}>
              {entry}
            </option>
          ))}
        </select>
        <button type="submit" disabled={busy || reportEntry === ""}>
          Open report
        </button>
      </form>

      <Report indicators={indicators} progress={progress} result={result} />

      {diagnosis ? (
        <>
          <p className="counts">
            <span>
              {diagnosis.total} finding{diagnosis.total === 1 ? "" : "s"} under {diagnosis.profile}{" "}
              · {diagnosis.ruleset}
            </span>
            <span className="boundary">
              rules {diagnosis.rules.join(", ")} · report identity {diagnosis.report_sha256}
            </span>
          </p>
          {foreign ? (
            <p className="scope">
              This report was run over other evidence (case identity {diagnosis.case_identity}), not
              the case open here. Its evidence names that case&apos;s occurrences, so none of it
              opens in this case&apos;s inspector.
            </p>
          ) : null}
          <p className="scope">{diagnosis.window.description}</p>
          <p className="scope">
            {diagnosis.window.occurrences} occurrence
            {diagnosis.window.occurrences === 1 ? "" : "s"} observed
            {diagnosis.window.observed_start ? ` from ${diagnosis.window.observed_start}` : ""}
            {diagnosis.window.observed_end ? ` to ${diagnosis.window.observed_end}` : ""} ·{" "}
            {diagnosis.window.unknown_observed_times} with no recorded time
          </p>
          <p className="scope">{diagnosis.scope}</p>
          {diagnosis.no_findings ? <p className="scope">{diagnosis.no_findings}</p> : null}

          <div className="diagnosis-window">
            <button
              type="button"
              disabled={busy || report === "" || diagnosis.offset === 0 || page === 0}
              onClick={() => onOpen(report, Math.max(0, diagnosis.offset - page))}
            >
              Previous {page}
            </button>
            <span>
              Findings {diagnosis.findings.length === 0 ? diagnosis.offset : diagnosis.offset + 1}–
              {diagnosis.offset + diagnosis.findings.length} of {diagnosis.total}
            </span>
            <button
              type="button"
              disabled={
                busy ||
                report === "" ||
                page === 0 ||
                diagnosis.offset + diagnosis.findings.length >= diagnosis.total
              }
              onClick={() => onOpen(report, diagnosis.offset + page)}
            >
              Next {page}
            </button>
          </div>

          {groupFindings(diagnosis.findings).map((group) => (
            <section key={group.signature} className="finding-group" aria-label={`Findings ${group.signature}`}>
              <h4>
                {group.signature} · {group.members.length} finding
                {group.members.length === 1 ? "" : "s"}
              </h4>
              <ul className="findings">
                {group.members.map((finding) => {
                  const draft = decisions[finding.id] ?? EMPTY_DECISION;
                  return (
                    <li key={finding.id}>
                      <span className="occurrence">{finding.id}</span>
                      <span className="reason">{finding.summary}</span>
                      {finding.window ? <span className="reason">{finding.window}</span> : null}
                      <ul className="evidence">
                        {finding.evidence.map((evidence, index) => (
                          <li key={`${evidence.occurrence}:${evidence.field}:${index}`}>
                            <button
                              type="button"
                              disabled={busy || foreign}
                              onClick={() => onSelect(evidence.occurrence)}
                            >
                              {evidence.occurrence}
                            </button>
                            <span className="selector">{evidence.field}</span>
                            <span className="states">{evidence.state}</span>
                          </li>
                        ))}
                      </ul>
                      <label htmlFor={`diagnosis-verdict-${finding.id}`}>Decision</label>
                      <select
                        id={`diagnosis-verdict-${finding.id}`}
                        value={draft.verdict}
                        disabled={busy}
                        onChange={(event) => decide(finding.id, { verdict: event.target.value })}
                      >
                        <option value="">Not decided</option>
                        {VERDICTS.map((verdict) => (
                          <option key={verdict} value={verdict}>
                            {verdict}
                          </option>
                        ))}
                      </select>
                      {draft.verdict === "suppressed" ? (
                        <>
                          <label htmlFor={`diagnosis-scope-${finding.id}`}>Suppression scope</label>
                          <select
                            id={`diagnosis-scope-${finding.id}`}
                            value={draft.scope}
                            disabled={busy}
                            onChange={(event) => decide(finding.id, { scope: event.target.value })}
                          >
                            {SCOPES.map((scope) => (
                              <option key={scope} value={scope}>
                                {scope}
                              </option>
                            ))}
                          </select>
                        </>
                      ) : null}
                      {draft.verdict !== "" ? (
                        <>
                          <label htmlFor={`diagnosis-rationale-${finding.id}`}>Rationale</label>
                          <input
                            id={`diagnosis-rationale-${finding.id}`}
                            required
                            maxLength={1024}
                            value={draft.rationale}
                            disabled={busy}
                            onChange={(event) =>
                              decide(finding.id, { rationale: event.target.value })
                            }
                          />
                        </>
                      ) : null}
                    </li>
                  );
                })}
              </ul>
            </section>
          ))}

          {diagnosis.unsupported.length > 0 ? (
            <>
              <h4>Unevaluated evidence</h4>
              <ul className="gaps">
                {diagnosis.unsupported.map((item, index) => (
                  <li key={`${item.code}:${item.occurrence ?? ""}:${index}`}>
                    {item.occurrence ? (
                      <>
                        <button
                          type="button"
                          disabled={busy || foreign}
                          onClick={() => onSelect(item.occurrence ?? "")}
                        >
                          {item.occurrence}
                        </button>{" "}
                        ·{" "}
                      </>
                    ) : null}
                    {item.field ? `${item.field} · ` : ""}
                    {item.code} · {item.detail}
                  </li>
                ))}
              </ul>
            </>
          ) : null}

          <h4>Review findings</h4>
          <p className="hint">
            A decision is one person&apos;s typed judgment: a verdict and a rationale, and for a
            suppression its scope. Previewing joins them to this exact report and writes nothing;
            recording below persists the decisions document and the review directory, exactly as
            the command line&apos;s own review writes them.
          </p>
          {decidedOutsideWindow.length > 0 ? (
            <section aria-label="Unlisted decisions">
              <p className="hint">
                These decisions are about findings this window of the report does not list. They are
                part of a review and of a saved decisions document until you forget them.
              </p>
              <ul className="findings">
                {decidedOutsideWindow.map((decision) => (
                  <li key={decision.finding}>
                    <span className="occurrence">{decision.finding}</span>
                    <span className="reason">
                      {decision.verdict}
                      {decision.scope ? ` · scope ${decision.scope}` : ""} · {decision.rationale}
                    </span>
                    <button type="button" disabled={busy} onClick={() => forget(decision.finding)}>
                      Remove decision
                    </button>
                  </li>
                ))}
              </ul>
            </section>
          ) : null}
          <button type="button" disabled={busy} onClick={() => review(false)}>
            Preview
          </button>

          <section aria-label="Finding decisions document">
            <h5>Finding decisions document</h5>
            <p className="hint">
              A decisions document holds what one person decided about this exact report, named by
              its identity. Opening a retained one puts its decisions on the findings above; saving
              writes the decisions above as a new entry and records no review.
            </p>
            <form
              onSubmit={(event) => {
                event.preventDefault();
                if (unsaved && decided.length > 0) setConfirming(decisionsEntry);
                else void openDecisions(decisionsEntry);
              }}
            >
              <label htmlFor="diagnosis-decisions-entry">Retained decisions document</label>
              <select
                id="diagnosis-decisions-entry"
                value={decisionsEntry}
                disabled={busy}
                onChange={(event) => setDecisionsEntry(event.target.value)}
              >
                <option value="">Choose an entry of this workspace…</option>
                {decisionsEntries.map((entry) => (
                  <option key={entry} value={entry}>
                    {entry}
                  </option>
                ))}
              </select>
              <button type="submit" ref={openDecisionsButton} disabled={busy || decisionsEntry === ""}>
                Open decisions
              </button>
            </form>
            {confirming !== null ? (
              <div
                role="group"
                aria-label={`Open ${confirming} in place of these decisions?`}
                onKeyDown={(event) => {
                  // Escape answers this question and goes no further: the
                  // window's own Escape cancels a running operation.
                  if (event.key === "Escape" && !event.nativeEvent.isComposing && !busy) {
                    event.preventDefault();
                    event.stopPropagation();
                    keepDecisions();
                  }
                }}
              >
                <p className="hint">
                  The decisions above are not saved. Opening {confirming} replaces them.
                </p>
                <button
                  type="button"
                  disabled={busy}
                  onClick={() => {
                    const name = confirming;
                    setConfirming(null);
                    openDecisionsButton.current?.focus();
                    void openDecisions(name);
                  }}
                >
                  Replace them with {confirming}
                </button>
                <button type="button" ref={keep} disabled={busy} onClick={keepDecisions}>
                  Keep these decisions
                </button>
              </div>
            ) : null}
            <form
              onSubmit={(event) => {
                event.preventDefault();
                void saveDecisions();
              }}
            >
              <label htmlFor="diagnosis-decisions-new">New finding-decisions entry</label>
              <input
                id="diagnosis-decisions-new"
                required
                value={newDecisionsEntry}
                disabled={busy}
                onChange={(event) => setNewDecisionsEntry(event.target.value)}
              />
              <button type="submit" disabled={busy || newDecisionsEntry === ""}>
                Save as new
              </button>
            </form>
            <details>
              <summary>Exact decisions document (advanced)</summary>
              <pre>{composeDecisions(diagnosis.report_sha256, decided)}</pre>
              <p className="hint">
                This is the text a save sends. The Go reader validates it and writes its canonical
                form.
              </p>
            </details>
            <p role="status">{decisionsStatus()}</p>
          </section>

          <form
            onSubmit={(event) => {
              event.preventDefault();
              review(true);
            }}
          >
            <label htmlFor="diagnosis-review-output">New finding-review directory</label>
            <input
              id="diagnosis-review-output"
              required
              value={reviewOutput}
              disabled={busy}
              onChange={(event) => setReviewOutput(event.target.value)}
            />
            <label htmlFor="diagnosis-decisions-output">New decisions document</label>
            <input
              id="diagnosis-decisions-output"
              required
              value={decisionsOutput}
              disabled={busy}
              onChange={(event) => setDecisionsOutput(event.target.value)}
            />
            <button type="submit" disabled={busy || reviewOutput === "" || decisionsOutput === ""}>
              Save decisions
            </button>
          </form>
        </>
      ) : null}

      {reviewResult && !reviewCurrent ? (
        <p className="hint">
          The decisions above changed since this review was previewed. Preview again to see what they
          mean.
        </p>
      ) : null}

      {reviewResult && reviewCurrent && reviewResult.state !== "completed" ? (
        <Report indicators={indicators} progress={null} result={reviewResult} />
      ) : null}

      {record && reviewCurrent ? (
        <section aria-label="Finding review">
          <h4>{reviewResult?.output ? "The review" : "The review, previewed (nothing written)"}</h4>
          <p className="scope">{record.statement}</p>
          <p className="counts">
            <span className="boundary">
              diagnosis {record.diagnosis.report_sha256} · decisions {record.decisions_sha256} ·
              boundary {record.boundary}
            </span>
          </p>
          {reviewResult?.output ? (
            <p className="written">
              Recorded to {reviewResult.output}
              {reviewResult.decisions_output ? ` beside ${reviewResult.decisions_output}` : ""}.
            </p>
          ) : null}
          <ul className="findings">
            {record.findings.map((status) => {
              const promotion = status.promotion;
              const expressible =
                status.verdict === "confirmed" &&
                promotion !== undefined &&
                promotion.expectations.length > 0;
              // A draft names the review it was promoted from, so only a
              // recorded review offers one.
              const draftable = expressible && Boolean(reviewResult?.output);
              return (
                <li key={status.finding}>
                  <span className="occurrence">{status.finding}</span>
                  <span className="reason">
                    {status.rule_id} · {status.classification} ·{" "}
                    {VERDICT_WORDS[status.verdict] ?? status.verdict} ·{" "}
                    {BASIS_WORDS[status.basis] ?? status.basis}
                    {status.scope ? ` · scope ${status.scope}` : ""}
                    {status.suppressed_by ? ` · suppressed by ${status.suppressed_by}` : ""}
                  </span>
                  {status.rationale ? <span className="reason">{status.rationale}</span> : null}
                  <span className="reason">{status.next_evidence}</span>
                  {promotion ? (
                    <>
                      <span className="reason">
                        Promotes {promotion.messages.length} message
                        {promotion.messages.length === 1 ? "" : "s"} and{" "}
                        {promotion.expectations.length} expectation
                        {promotion.expectations.length === 1 ? "" : "s"}.
                      </span>
                      {promotion.unsupported.length > 0 ? (
                        <ul className="gaps">
                          {promotion.unsupported.map((item, index) => (
                            <li key={`${item.code}:${index}`}>
                              Not expressible: {item.code} · {item.detail}
                            </li>
                          ))}
                        </ul>
                      ) : null}
                      {draftable ? (
                        <button
                          type="button"
                          disabled={busy}
                          onClick={() =>
                            onPromote(
                              status,
                              reviewResult?.output ?? "",
                              record.diagnosis.report_sha256,
                            )
                          }
                        >
                          Draft a test from {status.finding}
                        </button>
                      ) : expressible ? (
                        <span className="reason">
                          Record these decisions to draft a test from it: a draft names the review
                          it was promoted from, and a preview is not retained.
                        </span>
                      ) : (
                        <span className="reason">
                          Nothing here becomes a test: promotion expresses only what an explicitly
                          confirmed finding supports.
                        </span>
                      )}
                    </>
                  ) : (
                    <span className="reason">
                      {status.verdict === "confirmed"
                        ? "Confirmed, and this release expresses no part of it as a test."
                        : "Only a confirmed finding promotes anything."}
                    </span>
                  )}
                </li>
              );
            })}
          </ul>
        </section>
      ) : null}

      <h4>Recurring findings</h4>
      <p className="hint">
        Re-evaluates the selected cases under the chosen configuration and groups equal finding
        signatures. Equal signatures mean the same diagnostic shape, never the same root cause, and
        no finding is hidden by its group.
      </p>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          const { builtin, config } = parseConfig(chosen);
          const request: GroupDiagnosesRequest = {
            workspace,
            cases: grouped,
            ...(config !== "" ? { config } : {}),
            ...(builtin !== "" ? { builtin } : {}),
            offset: 0,
          };
          setGroupedRequest(request);
          setOpenedGroupsEntry("");
          onGroup(request);
        }}
      >
        <ul className="selection">
          {caseEntries.map((entry) => (
            <li key={entry}>
              <label>
                <input
                  type="checkbox"
                  checked={grouped.includes(entry)}
                  disabled={busy}
                  onChange={(event) =>
                    setGrouped(
                      event.target.checked
                        ? [...grouped, entry]
                        : grouped.filter((other) => other !== entry),
                    )
                  }
                />
                {entry}
              </label>
            </li>
          ))}
        </ul>
        <button type="submit" disabled={busy || chosen === "" || grouped.length === 0}>
          Group findings
        </button>
      </form>

      <form
        onSubmit={(event) => {
          event.preventDefault();
          setGroupedRequest(null);
          setOpenedGroupsEntry(groupsReportEntry);
          onOpenGroups(groupsReportEntry, 0);
        }}
      >
        <label htmlFor="diagnosis-groups-report">Retained grouping report</label>
        <select
          id="diagnosis-groups-report"
          value={groupsReportEntry}
          disabled={busy || groupsReportEntries.length === 0}
          onChange={(event) => setGroupsReportEntry(event.target.value)}
        >
          <option value="">Choose a retained grouping…</option>
          {groupsReportEntries.map((entry) => (
            <option key={entry} value={entry}>
              {entry}
            </option>
          ))}
        </select>
        <button type="submit" disabled={busy || groupsReportEntry === ""}>
          Open this grouping
        </button>
      </form>

      {groupsProgress !== null || (groupsResult && groupsResult.state !== "completed") ? (
        <Report indicators={indicators} progress={groupsProgress} result={groupsResult} />
      ) : null}
      {groupsResult?.groups ? (
        <section aria-label="Recurring finding groups">
          <p className="scope">{groupsResult.groups.scope}</p>
          <div className="diagnosis-window">
            <button
              type="button"
              disabled={busy || (groupedRequest === null && openedGroupsEntry === "") || groupsResult.offset === 0 || page === 0}
              onClick={() => {
                const offset = Math.max(0, groupsResult.offset - page);
                if (groupedRequest) onGroup({ ...groupedRequest, offset });
                else if (openedGroupsEntry) onOpenGroups(openedGroupsEntry, offset);
              }}
            >
              Previous {page} groups
            </button>
            <span>
              Groups{" "}
              {groupsResult.groups.groups.length === 0 ? groupsResult.offset : groupsResult.offset + 1}–
              {groupsResult.offset + groupsResult.groups.groups.length} of {groupsResult.total} across{" "}
              {groupsResult.groups.cases.length} case{groupsResult.groups.cases.length === 1 ? "" : "s"}
            </span>
            <button
              type="button"
              disabled={
                busy ||
                (groupedRequest === null && openedGroupsEntry === "") ||
                page === 0 ||
                groupsResult.offset + groupsResult.groups.groups.length >= groupsResult.total
              }
              onClick={() => {
                const offset = groupsResult.offset + page;
                if (groupedRequest) onGroup({ ...groupedRequest, offset });
                else if (openedGroupsEntry) onOpenGroups(openedGroupsEntry, offset);
              }}
            >
              Next {page} groups
            </button>
          </div>
          <ul className="findings">
            {groupsResult.groups.groups.map((group) => (
              <li key={group.signature}>
                <span className="occurrence">{group.rule_id}</span>
                <span className="reason">
                  signature {group.signature} · {group.members.length} finding
                  {group.members.length === 1 ? "" : "s"} across cases
                </span>
                <span className="reason">
                  {group.members
                    .map((member) => {
                      const entry = groupsResult.case_entries?.[member.case_identity];
                      return `${member.finding_id} of ${entry ? `${entry} (${member.case_identity})` : member.case_identity}`;
                    })
                    .join(", ")}
                </span>
              </li>
            ))}
          </ul>
        </section>
      ) : null}
    </section>
  );
}
