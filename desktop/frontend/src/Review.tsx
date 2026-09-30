import type { Review as ReviewDocument, ReviewDecision, ReviewResult } from "./bindings";
import { Report, type Indicators } from "./shell";
import "./review.css";
import { useViewState } from "./viewstate";

/** How each decision reads. The facade names them and decides all of them; this
 * maps each to a sentence and none of them to a colour alone. */
const DECISIONS: Record<ReviewDecision, string> = {
  "not-decided": "Not decided",
  "incomplete-review": "Incomplete review",
  "stale-approval": "Stale approval",
  approved: "Approved",
};

/** Read one export review: what the declared policy did to every surface that
 * can enter an export, read back through the same verified reader the export
 * gate uses. Transformations of a case are its variants' (#558).
 *
 * No value crosses this boundary. A finding is a location, a class and the
 * named policy that handled it. */
export function Review({
  reviewEntries,
  reviewResult,
  busy,
  reviewProgress,
  indicators,
  onReview,
}: {
  /** The export review folders of the open workspace. */
  reviewEntries: string[];
  reviewResult: ReviewResult | null;
  busy: boolean;
  reviewProgress: string | null;
  indicators: Indicators;
  onReview: (review: string, approve: string, offset: number) => void;
}) {
  const [entry, setEntry] = useViewState("Review.entry", "");
  const [approval, setApproval] = useViewState("Review.approval", "");
  const review: ReviewDocument | null = reviewResult?.review ?? null;

  return (
    <section className="review" aria-label="Export review">
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

      <Report outcome indicators={indicators} progress={reviewProgress} result={reviewResult} />

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
