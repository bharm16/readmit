import { useEffect, useRef, useState } from "react";
import type {
  AuthoredTransformPlan,
  Review as ReviewDocument,
  ReviewDecision,
  ReviewResult,
  Transformation,
  TransformOperator,
  TransformPlanResult,
  TransformResult,
  TransformStep,
} from "./bindings";
import { Report, type Indicators } from "./shell";
import "./review.css";

/** How many findings of a review one window asks the facade for. It is the
 * facade's own bound: a large inventory is rendered one window at a time and
 * the next one is another call. */
export const REVIEW_WINDOW = 200;

/** How each decision reads. The facade names them and decides all of them; this
 * maps each to a sentence and none of them to a colour alone. */
const DECISIONS: Record<ReviewDecision, string> = {
  "not-decided": "Not decided",
  "incomplete-review": "Incomplete review",
  "stale-approval": "Stale approval",
  approved: "Approved",
};

/** Review and transform the whole case.
 *
 * Two answers, neither of them this view's own. A transformation preview is
 * what `readmit transform` would do to the sequence a replay sends, and it
 * writes nothing at all. An export review is what the declared policy did to
 * every surface that can enter an export, read back through the same verified
 * reader the export gate uses.
 *
 * No value crosses this boundary, transformed or original. A change is a
 * position and a relation number; a finding is a location, a class and the
 * named policy that handled it. Reading a transformed value is the inspector
 * over the derived case the review names: open the review folder as a workspace
 * and open the `case` entry in it, exactly as any other evidence is read. */
export function Review({
  ruleEntries,
  planEntries,
  packEntries,
  reviewEntries,
  transformResult,
  planResult,
  reviewResult,
  caseOpen,
  caseIdentity,
  busy,
  transformProgress,
  planProgress,
  reviewProgress,
  indicators,
  onPreview,
  onSavePlan,
  onOpenPlan,
  onReview,
}: {
  /** The entries of the open workspace that declare each contract this panel
   * names. The listing classifies every entry by what it declares, so each
   * picker offers the applicable documents instead of every entry. */
  ruleEntries: string[];
  planEntries: string[];
  packEntries: string[];
  reviewEntries: string[];
  transformResult: TransformResult | null;
  planResult: TransformPlanResult | null;
  reviewResult: ReviewResult | null;
  /** Whether a case is open. A preview is over one verified case; a review is
   * over a folder, and a workspace holding only a review is read without one. */
  caseOpen: boolean;
  /** The identity of the case the window verified, or empty. */
  caseIdentity: string;
  busy: boolean;
  transformProgress: string | null;
  planProgress: string | null;
  reviewProgress: string | null;
  indicators: Indicators;
  onPreview: (rules: string, plan: string, profile: string) => void;
  /** Saves the authored steps as a new plan entry and resolves with the
   * facade's answer once the listing offers it, or null when nothing was
   * asked. */
  onSavePlan: (
    rules: string,
    profile: string,
    steps: TransformStep[],
    output: string,
  ) => Promise<TransformPlanResult | null>;
  /** Reads one plan entry back through the shared decoder and resolves with
   * the facade's answer, or null when nothing was asked. */
  onOpenPlan: (entry: string) => Promise<TransformPlanResult | null>;
  onReview: (review: string, approve: string, offset: number) => void;
}) {
  const [rules, setRules] = useState("");
  const [plan, setPlan] = useState("");
  const [profile, setProfile] = useState("");
  const [entry, setEntry] = useState("");
  const [approval, setApproval] = useState("");
  const [planOutput, setPlanOutput] = useState("");
  const [operator, setOperator] = useState<TransformOperator>("rebase-identifiers/v1");
  const [stepRule, setStepRule] = useState("patient");
  const [stepShift, setStepShift] = useState("24h");
  const [stepEntry, setStepEntry] = useState("t000001");
  const [stepPosition, setStepPosition] = useState("1");
  const [authoredSteps, setAuthoredSteps] = useState<TransformStep[]>([]);
  // The steps as the plan the panel last saved or opened held them, so a
  // change nobody saved is known before an open would replace it.
  const [storedSteps, setStoredSteps] = useState<TransformStep[]>([]);
  // What the plan line below reports: the plan saved or opened last, by the
  // action that produced it. A refusal is the status line's alone.
  const [storedPlan, setStoredPlan] = useState<{ action: "saved" | "opened"; plan: AuthoredTransformPlan } | null>(null);
  const [reopenEntry, setReopenEntry] = useState("");
  const [confirming, setConfirming] = useState<string | null>(null);
  const openButton = useRef<HTMLButtonElement>(null);
  const keep = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (confirming !== null) keep.current?.focus();
  }, [confirming]);

  // Steps are authored over the case on screen and a save binds them to it,
  // so another case starts the plan again rather than carrying steps written
  // against other evidence.
  useEffect(() => {
    setAuthoredSteps([]);
    setStoredSteps([]);
    setStoredPlan(null);
    setConfirming(null);
  }, [caseIdentity]);

  // The window withdraws a plan answer when the case it was about is closed,
  // and the line naming that plan goes with it.
  useEffect(() => {
    if (planResult === null) setStoredPlan(null);
  }, [planResult]);

  const transformation: Transformation | null = transformResult?.transformation ?? null;
  const review: ReviewDocument | null = reviewResult?.review ?? null;
  const unsaved = JSON.stringify(authoredSteps) !== JSON.stringify(storedSteps);
  const authoring = busy || !caseOpen;

  const open = async (name: string) => {
    setStoredPlan(null);
    const answered = await onOpenPlan(name);
    const opened = answered?.plan;
    if (!opened) return;
    setAuthoredSteps(opened.plan.steps);
    setStoredSteps(opened.plan.steps);
    setStoredPlan({ action: "opened", plan: opened });
    setPlan(name);
  };

  const keepSteps = () => {
    setConfirming(null);
    openButton.current?.focus();
  };

  return (
    <section className="review" aria-label="Review and transform">
      <h3>Review and transform</h3>
      <p className="hint">
        Preview what a transformation plan would do to the sequence a replay sends, and read an
        export review of everything that would leave this machine. Neither shows a value: a change
        is a position and a finding is a location, and reading a transformed value is the inspector
        over the derived case a review names.
      </p>

      <h4>Transform plan</h4>
      <p className="hint">
        Compose the operators the engine already supports. Correlation rules are
        selected documents authored in the Sequence panel — this panel does not
        invent a second rules editor. Preview effects before anything is applied;
        originals are never normalised or redacted in place.
      </p>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          if (unsaved && authoredSteps.length > 0) {
            setConfirming(reopenEntry);
          } else {
            void open(reopenEntry);
          }
        }}
      >
        <label htmlFor="transform-reopen">Saved plan to reopen</label>
        <select
          id="transform-reopen"
          value={reopenEntry}
          disabled={authoring || planEntries.length === 0}
          onChange={(event) => setReopenEntry(event.target.value)}
        >
          <option value="">Choose a plan document of this workspace…</option>
          {planEntries.map((name) => (
            <option key={name} value={name}>
              {name}
            </option>
          ))}
        </select>
        <button type="submit" ref={openButton} disabled={authoring || reopenEntry === ""}>
          Open plan
        </button>
      </form>
      {confirming !== null ? (
        <div
          role="group"
          aria-label={`Open ${confirming} in place of these steps?`}
          onKeyDown={(event) => {
            // Escape answers this question and goes no further: the window's
            // own Escape cancels a running operation.
            if (event.key === "Escape" && !event.nativeEvent.isComposing && !busy) {
              event.preventDefault();
              event.stopPropagation();
              keepSteps();
            }
          }}
        >
          <p className="hint">
            The steps below are not saved. Opening {confirming} replaces them.
          </p>
          <button
            type="button"
            disabled={busy}
            onClick={() => {
              const name = confirming;
              setConfirming(null);
              openButton.current?.focus();
              void open(name);
            }}
          >
            Replace them with {confirming}
          </button>
          <button type="button" ref={keep} disabled={busy} onClick={keepSteps}>
            Keep these steps
          </button>
        </div>
      ) : null}
      <form
        onSubmit={(event) => {
          event.preventDefault();
          const step: TransformStep = { operator };
          if (operator === "rebase-identifiers/v1") step.rule = stepRule;
          if (operator === "shift-dates/v1") step.shift = stepShift;
          if (operator === "reorder-occurrence/v1") {
            step.entry = stepEntry;
            step.position = Number(stepPosition);
          }
          if (operator === "duplicate-occurrence/v1" || operator === "drop-occurrence/v1") {
            step.entry = stepEntry;
          }
          setAuthoredSteps((current) => [...current, step]);
        }}
      >
        <label htmlFor="transform-step-operator">Operator</label>
        <select
          id="transform-step-operator"
          value={operator}
          disabled={authoring}
          onChange={(event) => setOperator(event.target.value as TransformOperator)}
        >
          <option value="rebase-identifiers/v1">Rebase identifiers of one correlation rule</option>
          <option value="shift-dates/v1">Shift dates by a duration</option>
          <option value="reorder-occurrence/v1">Reorder one sequence entry</option>
          <option value="duplicate-occurrence/v1">Duplicate one sequence entry</option>
          <option value="drop-occurrence/v1">Drop one sequence entry</option>
        </select>
        {operator === "rebase-identifiers/v1" ? (
          <>
            <label htmlFor="transform-step-rule">Correlation rule id</label>
            <input id="transform-step-rule" value={stepRule} disabled={authoring} onChange={(e) => setStepRule(e.target.value)} />
          </>
        ) : null}
        {operator === "shift-dates/v1" ? (
          <>
            <label htmlFor="transform-step-shift">Shift duration</label>
            <input id="transform-step-shift" value={stepShift} disabled={authoring} onChange={(e) => setStepShift(e.target.value)} />
          </>
        ) : null}
        {operator === "reorder-occurrence/v1" || operator === "duplicate-occurrence/v1" || operator === "drop-occurrence/v1" ? (
          <>
            <label htmlFor="transform-step-entry">Sequence entry</label>
            <input id="transform-step-entry" value={stepEntry} disabled={authoring} onChange={(e) => setStepEntry(e.target.value)} />
          </>
        ) : null}
        {operator === "reorder-occurrence/v1" ? (
          <>
            <label htmlFor="transform-step-position">One-based position</label>
            <input
              id="transform-step-position"
              value={stepPosition}
              disabled={authoring}
              onChange={(e) => setStepPosition(e.target.value)}
            />
          </>
        ) : null}
        <button type="submit" disabled={authoring}>
          Add step
        </button>
      </form>
      {authoredSteps.length ? (
        <ol className="steps" aria-label="Authored transformation steps">
          {authoredSteps.map((step, index) => (
            <li key={`${index}:${step.operator}`}>
              {step.operator}
              {step.rule ? ` · ${step.rule}` : ""}
              {step.shift ? ` · ${step.shift}` : ""}
              {step.entry ? ` · ${step.entry}` : ""}
              {step.position ? ` · position ${step.position}` : ""}{" "}
              <button
                type="button"
                disabled={authoring}
                aria-label={`Remove step ${index + 1}`}
                onClick={() => setAuthoredSteps((current) => current.filter((_, other) => other !== index))}
              >
                Remove
              </button>
            </li>
          ))}
        </ol>
      ) : null}
      <form
        onSubmit={(event) => {
          event.preventDefault();
          const steps = authoredSteps;
          const output = planOutput;
          setStoredPlan(null);
          void onSavePlan(rules, profile, steps, output).then((answered) => {
            const saved = answered?.plan;
            if (!saved) return;
            setStoredSteps(steps);
            setStoredPlan({ action: "saved", plan: saved });
            setPlanOutput("");
            setPlan(output);
          });
        }}
      >
        <label htmlFor="transform-plan-output">New plan document in this workspace</label>
        <input
          id="transform-plan-output"
          value={planOutput}
          disabled={authoring}
          onChange={(event) => setPlanOutput(event.target.value)}
        />
        <button type="submit" disabled={authoring || rules === "" || planOutput === ""}>
          Save plan
        </button>
        {rules === "" ? (
          <p className="hint">Choose the correlation rules below; a plan pins the rules it preserves.</p>
        ) : null}
      </form>
      <Report indicators={indicators} progress={planProgress} result={planResult} />
      {storedPlan ? (
        <p className="hint">
          {storedPlan.action === "saved" ? "Saved" : "Opened"} {storedPlan.plan.output} · {storedPlan.plan.plan.steps.length}{" "}
          {storedPlan.plan.plan.steps.length === 1 ? "step" : "steps"} · rules digest {storedPlan.plan.digest}
          {storedPlan.plan.plan.profile
            ? ` · pins profile pack ${storedPlan.plan.plan.profile.id} ${storedPlan.plan.plan.profile.version}`
            : ""}
          . It is selected below to preview.
        </p>
      ) : null}

      <form
        onSubmit={(event) => {
          event.preventDefault();
          onPreview(rules, plan, profile);
        }}
      >
        <label htmlFor="transform-rules">Correlation rules</label>
        <select
          id="transform-rules"
          value={rules}
          disabled={busy || !caseOpen || ruleEntries.length === 0}
          onChange={(event) => setRules(event.target.value)}
        >
          <option value="">Choose a rules document of this workspace…</option>
          {ruleEntries.map((name) => (
            <option key={name} value={name}>
              {name}
            </option>
          ))}
        </select>
        <p className="hint">The transform preserves the named rules' relations; the choice is not arbitrary.</p>

        <label htmlFor="transform-plan">Transform plan</label>
        <select
          id="transform-plan"
          value={plan}
          disabled={busy || !caseOpen || planEntries.length === 0}
          onChange={(event) => setPlan(event.target.value)}
        >
          <option value="">Choose a plan document of this workspace…</option>
          {planEntries.map((name) => (
            <option key={name} value={name}>
              {name}
            </option>
          ))}
        </select>

        <label htmlFor="transform-profile">Pinned profile pack</label>
        <select
          id="transform-profile"
          value={profile}
          disabled={busy || !caseOpen || packEntries.length === 0}
          onChange={(event) => setProfile(event.target.value)}
        >
          <option value="">No pinned pack</option>
          {packEntries.map((name) => (
            <option key={name} value={name}>
              {name}
            </option>
          ))}
        </select>
        <p className="hint">Only a pack the plan pinned, if it pinned one; no pack is inferred or chosen by default.</p>

        <button type="submit" disabled={busy || !caseOpen || rules === "" || plan === ""}>
          Preview
        </button>
        {caseOpen ? null : (
          <p className="hint">Open a case to preview a transformation over it.</p>
        )}
      </form>

      <Report indicators={indicators} progress={transformProgress} result={transformResult} />

      {transformation ? (
        <p className="hint">
          Preview of {transformation.plan} under {transformation.rules}
          {transformation.profile ? ` with ${transformation.profile}` : ""}.
        </p>
      ) : null}
      {transformation ? <Preview transformation={transformation} /> : null}

      <form
        onSubmit={(event) => {
          event.preventDefault();
          onReview(entry, approval, 0);
        }}
      >
        <label htmlFor="review-entry">Export review to read</label>
        <select
          id="review-entry"
          value={entry}
          disabled={busy || reviewEntries.length === 0}
          onChange={(event) => setEntry(event.target.value)}
        >
          <option value="">Choose a review folder of this workspace…</option>
          {reviewEntries.map((name) => (
            <option key={name} value={name}>
              {name}
            </option>
          ))}
        </select>

        <label htmlFor="review-approval">Review ID</label>
        <input
          id="review-approval"
          value={approval}
          disabled={busy}
          onChange={(event) => setApproval(event.target.value)}
        />
        <p className="hint">
          Approval of this review is recorded only when its complete exact identity is named; a blank
          records no approval, and nothing is prefilled.
        </p>

        <button type="submit" disabled={busy || entry === ""}>
          Open review
        </button>
      </form>

      <Report indicators={indicators} progress={reviewProgress} result={reviewResult} />

      {review ? (
        <Inventory
          review={review}
          busy={busy}
          onWindow={(offset) => onReview(review.name, approval, offset)}
        />
      ) : null}
    </section>
  );
}

/** What one plan would do, as positions. Every count is the engine's own. */
function Preview({ transformation }: { transformation: Transformation }) {
  const preview = transformation.preview;
  return (
    <>
      <p className="counts">
        <span>
          {preview.summary.entries} entries · {preview.summary.copies} copies ·{" "}
          {preview.summary.changes} positions rewritten
        </span>
        <span>
          {preview.summary.preserved} of {preview.summary.relations} relations preserved ·{" "}
          {preview.summary.unsupported} left alone
        </span>
        <span>{preview.schema}</span>
      </p>

      <div className="review-scroll">
        <table>
          <caption>
            Every position this plan would rewrite. Two changes carrying one relation number receive
            one value, which is what keeps a relation true; no value is shown.
          </caption>
          <thead>
            <tr>
              <th scope="col">Entry</th>
              <th scope="col">Occurrence</th>
              <th scope="col">Operator</th>
              <th scope="col">Position</th>
              <th scope="col">Was</th>
              <th scope="col">Relation</th>
            </tr>
          </thead>
          <tbody>
            {preview.changes.map((change, at) => (
              <tr key={`${change.entry}-${change.selector}-${at}`}>
                <td>{change.entry}</td>
                <td>{change.parent}</td>
                <td>{change.operator}</td>
                <td>{change.selector}</td>
                <td>{change.state}</td>
                <td>{change.group ?? "—"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {preview.relations.length > 0 ? (
        <div className="review-scroll">
          <table>
            <caption>What the edited sequence did to every declared relation.</caption>
            <thead>
              <tr>
                <th scope="col">Rule</th>
                <th scope="col">Linkage</th>
                <th scope="col">Occurrences</th>
                <th scope="col">Preserved</th>
              </tr>
            </thead>
            <tbody>
              {preview.relations.map((relation, at) => (
                <tr key={`${relation.rule}-${at}`}>
                  <td>{relation.rule}</td>
                  <td>{relation.linkage || relation.operator}</td>
                  <td>{relation.occurrences.length}</td>
                  <td>{relation.preserved ? "yes" : (relation.reason ?? "no")}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}

      {preview.profile.length > 0 ? (
        <div className="review-scroll">
          <table>
            <caption>
              What the transformed sequence declares about itself, and what the pinned pack says
              about it. Only a supported outcome passes.
            </caption>
            <thead>
              <tr>
                <th scope="col">Version</th>
                <th scope="col">Family</th>
                <th scope="col">Entries</th>
                <th scope="col">Parse</th>
                <th scope="col">Labels</th>
                <th scope="col">Structural</th>
                <th scope="col">Workflow</th>
              </tr>
            </thead>
            <tbody>
              {preview.profile.map((combination, at) => (
                <tr key={`${combination.version}-${combination.family}-${at}`}>
                  <td>{combination.version || "—"}</td>
                  <td>{combination.family || "—"}</td>
                  <td>{combination.entries}</td>
                  <td>{combination.parse}</td>
                  <td>{combination.labels}</td>
                  <td>{combination.structural}</td>
                  <td>{combination.workflow}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}

      {preview.unsupported.length > 0 ? (
        <div className="review-scroll">
          <table>
            <caption>
              Everything the transformation left exactly as the evidence has it. None of these
              passes.
            </caption>
            <thead>
              <tr>
                <th scope="col">Reported</th>
                <th scope="col">Where</th>
                <th scope="col">What it means</th>
              </tr>
            </thead>
            <tbody>
              {preview.unsupported.map((item, at) => (
                <tr key={`${item.code}-${at}`}>
                  <td>{item.code}</td>
                  <td>{[item.entry, item.rule, item.selector].filter(Boolean).join(" ") || "—"}</td>
                  <td>{item.detail}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}

      <p className="scope">{preview.scope}</p>
      <p className="boundary">{transformation.boundary}</p>
    </>
  );
}

/** One export review: what it binds, what it found on every surface, and the
 * reviewer's decision. */
function Inventory({
  review,
  busy,
  onWindow,
}: {
  review: ReviewDocument;
  busy: boolean;
  onWindow: (offset: number) => void;
}) {
  return (
    <>
      <div className="decision">
        <span className="named">{DECISIONS[review.decision]}</span>
        <span className="reason">{review.decision_reason}</span>
        {review.establishes ? (
          <span className="reason">This review establishes: {review.establishes}.</span>
        ) : null}
      </div>

      <dl className="identities">
        <dt>Review identity an approval names</dt>
        <dd>{review.identity}</dd>
        <dt>Inputs it is bound to</dt>
        <dd>{review.input_commitment}</dd>
        <dt>Private state it is bound to</dt>
        <dd>{review.local_state_commitment}</dd>
        <dt>Derived case</dt>
        <dd>{review.derived_case_identity ?? "none: this review derived nothing"}</dd>
        <dt>Derived specification</dt>
        <dd>{review.derived_spec_sha256 ?? "none: this review derived nothing"}</dd>
      </dl>

      <p className="counts">
        <span>
          {review.total} located findings · <span className="unresolved">{review.unresolved}</span>{" "}
          unresolved
        </span>
        <span>
          residual scan {review.residual_scan.status} over {review.residual_scan.files_checked}{" "}
          files and {review.residual_scan.known_values_checked} known values
        </span>
        <span>
          {review.uncovered_classes.length} of {review.coverage.length} checklist categories
          unresolved
        </span>
        <span>
          {review.state} · {review.report} · {review.data_origin}
        </span>
      </p>

      <div className="review-scroll">
        <table>
          <caption>
            Every export surface this review inventoried. Each finding belongs to exactly one, so
            the counts are the whole of what was found.
          </caption>
          <thead>
            <tr>
              <th scope="col">Where</th>
              <th scope="col">What was found there</th>
              <th scope="col">Findings</th>
              <th scope="col">Unresolved</th>
            </tr>
          </thead>
          <tbody>
            {review.surfaces.map((surface) => (
              <tr key={`${surface.name}-${surface.content}`}>
                <td>{surface.name}</td>
                <td>{surface.content}</td>
                <td>{surface.findings}</td>
                <td className={surface.unresolved > 0 ? "unresolved" : undefined}>
                  {surface.unresolved}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <div className="review-window">
        <button
          type="button"
          disabled={busy || review.offset === 0}
          onClick={() => onWindow(Math.max(0, review.offset - review.limit))}
        >
          Previous {review.limit}
        </button>
        <span>
          Findings {review.findings.length === 0 ? review.offset : review.offset + 1}–
          {review.offset + review.findings.length} of {review.total}
        </span>
        <button
          type="button"
          disabled={busy || review.offset + review.findings.length >= review.total}
          onClick={() => onWindow(review.offset + review.limit)}
        >
          Next {review.limit}
        </button>
      </div>

      <div className="review-scroll">
        <table>
          <caption>
            One window of the inventory. A location is where an item is; no value is here, and no
            private source mapping was read to produce it.
          </caption>
          <thead>
            <tr>
              <th scope="col">Surface</th>
              <th scope="col">Location</th>
              <th scope="col">Category</th>
              <th scope="col">What it is</th>
              <th scope="col">Handled by</th>
            </tr>
          </thead>
          <tbody>
            {review.findings.map((finding, at) => (
              <tr key={`${finding.location}-${at}`}>
                <td>{finding.surface}</td>
                <td>{finding.location}</td>
                <td>{finding.class}</td>
                <td>{finding.reason}</td>
                <td className={finding.resolved ? undefined : "unresolved"}>
                  {finding.resolved ? finding.policy : "nothing: unresolved"}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <div className="review-scroll">
        <table>
          <caption>
            The eighteen checklist categories. A configured rule establishes limited coverage at its
            listed locations and never coverage of the category.
          </caption>
          <thead>
            <tr>
              <th scope="col">Category</th>
              <th scope="col">Status</th>
              <th scope="col">Handled</th>
              <th scope="col">Unresolved</th>
            </tr>
          </thead>
          <tbody>
            {review.coverage.map((category) => (
              <tr key={category.class}>
                <td>{category.class}</td>
                <td>{category.status}</td>
                <td>{category.handled_locations}</td>
                <td className={category.unresolved_locations > 0 ? "unresolved" : undefined}>
                  {category.unresolved_locations}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <p className="scope">{review.residual_scan.limitations}</p>
      <p className="scope">{review.scope}</p>
      <p className="boundary">{review.boundary}</p>
    </>
  );
}
