import { useEffect, useState } from "react";
import type {
  Diagnosis as DiagnosisView,
  DiagnosisFinding,
  DiagnosisGroupsResult,
  DiagnosisRequest,
  DiagnosisResult,
  FindingDecision,
  FindingReviewRequest,
  FindingReviewResult,
  FindingStatus,
  GroupDiagnosesRequest,
} from "./bindings";
import { DiagnoseConfigEditor } from "./RulesEditor";
import { Report, type Indicators } from "./shell";
import "./diagnosis.css";

/** How many findings of a diagnosis one window asks the facade for. It is the
 * facade's own bound. */
export const DIAGNOSIS_WINDOW = 200;

/** The three built-in configurations a diagnosis can run under, named by their
 * own profile and ruleset tokens. The vocabulary is the engine's; choosing one
 * is explicit and nothing is chosen implicitly. */
const BUILTINS: { id: string; profile: string; ruleset: string }[] = [
  { id: "siu", profile: "readmit-siu-v1", ruleset: "readmit-siu-diagnosis/v1" },
  { id: "lifecycle", profile: "readmit-lifecycle-v1", ruleset: "readmit-lifecycle-diagnosis/v1" },
  { id: "order", profile: "readmit-order-v1", ruleset: "readmit-order-diagnosis/v1" },
];

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
 * rather than weakened into an implicit expectation. */
export function Diagnosis({
  workspace,
  caseName,
  identity,
  configEntries,
  reportEntries,
  caseEntries,
  result,
  groupsResult,
  reviewResult,
  busy,
  progress,
  indicators,
  onRun,
  onOpen,
  onGroup,
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
  /** The case bundles of the open workspace, for grouping recurring findings. */
  caseEntries: string[];
  result: DiagnosisResult | null;
  groupsResult: DiagnosisGroupsResult | null;
  reviewResult: FindingReviewResult | null;
  busy: boolean;
  progress: string | null;
  indicators: Indicators;
  onRun: (request: DiagnosisRequest) => void;
  onOpen: (entry: string, offset: number) => void;
  onGroup: (request: GroupDiagnosesRequest) => void;
  onReview: (request: FindingReviewRequest, write: boolean) => void;
  onSelect: (occurrence: string) => void;
  /** Drafts a regression test from one explicitly confirmed finding's
   * promotion, carrying where it came from. */
  onPromote: (status: FindingStatus, reviewEntry: string, reportSHA256: string) => void;
  /** Focuses the interface-profile editor. Diagnosis configurations name the
   * engine's bundled profiles; local interface profiles are managed there. */
  onManageProfiles?: () => void;
  /** Called after an authored configuration landed, so the pickers offer it. */
  onSaved?: () => void;
}) {
  const [chosen, setChosen] = useState("");
  const [output, setOutput] = useState("");
  const [report, setReport] = useState("");
  const [reportEntry, setReportEntry] = useState("");
  const [grouped, setGrouped] = useState<string[]>([]);
  const [decisions, setDecisions] = useState<Record<string, DecisionDraft>>({});
  const [reviewOutput, setReviewOutput] = useState("");
  const [decisionsOutput, setDecisionsOutput] = useState("");

  const diagnosis: DiagnosisView | null = result?.diagnosis ?? null;

  // A new report is a new finding list, so decisions typed about the previous
  // one are not silently rebound to findings they were never about.
  const reportIsNew = diagnosis?.report_sha256 ?? "";
  useEffect(() => {
    setDecisions({});
  }, [reportIsNew]);

  const decide = (finding: string, change: Partial<DecisionDraft>) =>
    setDecisions((current) => ({
      ...current,
      [finding]: { ...EMPTY_DECISION, ...current[finding], ...change },
    }));

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

  const review = (write: boolean) => {
    if (!diagnosis) return;
    const request: FindingReviewRequest = {
      workspace,
      case: caseName,
      identity,
      report,
      report_sha256: diagnosis.report_sha256,
      decisions: typedDecisions(),
      offset: 0,
    };
    if (write) {
      request.output = reviewOutput;
      request.decisions_output = decisionsOutput;
    }
    onReview(request, write);
  };

  const record = reviewResult?.review?.record ?? null;

  return (
    <section className="diagnosis" aria-label="Diagnosis and finding review">
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
            {BUILTINS.map((builtin) => (
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
          Run this diagnosis
        </button>
      </form>

      {onManageProfiles ? (
        <p className="hint">
          A configuration names one of the engine's bundled fixture profiles.{" "}
          <button type="button" disabled={busy} onClick={onManageProfiles}>
            Manage interface profiles…
          </button>{" "}
          edits local interface profiles, which are a separate document.
        </p>
      ) : null}

      <details>
        <summary>Author a diagnose configuration</summary>
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
          Open this report
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
              disabled={busy || report === "" || diagnosis.offset === 0}
              onClick={() => onOpen(report, Math.max(0, diagnosis.offset - DIAGNOSIS_WINDOW))}
            >
              Previous {DIAGNOSIS_WINDOW}
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
                diagnosis.offset + diagnosis.findings.length >= diagnosis.total
              }
              onClick={() => onOpen(report, diagnosis.offset + DIAGNOSIS_WINDOW)}
            >
              Next {DIAGNOSIS_WINDOW}
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
                              disabled={busy}
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
              <h4>Evidence this diagnosis did not evaluate</h4>
              <ul className="gaps">
                {diagnosis.unsupported.map((item, index) => (
                  <li key={`${item.code}:${item.occurrence ?? ""}:${index}`}>
                    {item.occurrence ? (
                      <>
                        <button type="button" disabled={busy} onClick={() => onSelect(item.occurrence ?? "")}>
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

          <h4>Review these findings</h4>
          <p className="hint">
            A decision is one person's typed judgment: a verdict and a rationale, and for a
            suppression its scope. Previewing joins them to this exact report and writes nothing;
            recording persists the decisions document and the review directory, exactly as{" "}
            <code>readmit diagnose review</code> writes them.
          </p>
          <button type="button" disabled={busy} onClick={() => review(false)}>
            Preview the review (writes nothing)
          </button>
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
              Record these finding decisions
            </button>
          </form>
        </>
      ) : null}

      {reviewResult && reviewResult.state !== "completed" ? (
        <Report indicators={indicators} progress={null} result={reviewResult} />
      ) : null}

      {record ? (
        <section aria-label="Finding review">
          <h4>The review</h4>
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
              const draftable =
                status.verdict === "confirmed" &&
                promotion !== undefined &&
                promotion.expectations.length > 0;
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

      <h4>Recurring findings across cases</h4>
      <p className="hint">
        Re-evaluates the selected cases under the chosen configuration and groups equal finding
        signatures. Equal signatures mean the same diagnostic shape, never the same root cause, and
        no finding is hidden by its group.
      </p>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          const { builtin, config } = parseConfig(chosen);
          onGroup({
            workspace,
            cases: grouped,
            ...(config !== "" ? { config } : {}),
            ...(builtin !== "" ? { builtin } : {}),
            offset: 0,
          });
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
          Group findings across these cases
        </button>
      </form>

      {groupsResult && groupsResult.state !== "completed" ? (
        <Report indicators={indicators} progress={null} result={groupsResult} />
      ) : null}
      {groupsResult?.groups ? (
        <>
          <p className="scope">{groupsResult.groups.scope}</p>
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
                    .map((member) => `${member.finding_id} of ${member.case_identity}`)
                    .join(", ")}
                </span>
              </li>
            ))}
          </ul>
        </>
      ) : null}
    </section>
  );
}
