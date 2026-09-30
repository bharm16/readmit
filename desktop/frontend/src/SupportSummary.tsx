// Support summary (#560): the share operation's own value-free summary of one
// report, previewed whole under the project's sharing policy and exported
// once under a review bound to its exact identity. For the customer hub it is
// written into the project, where the team's request and approval take it.
// The policy is its own sheet; saving it approves nothing.
import { useCallback, useEffect, useRef, useState } from "react";
import {
  chooseShareDestination,
  executeReviewedAction,
  listWholeCatalog,
  newIntentId,
  openSharedOutput,
  prepareAction,
  RequestScope,
  saveProjectSharingPolicy,
  type ActionReview,
  type CatalogItem,
  type ItemRef,
  type ReviewedActionResult,
} from "./bindings";
import { FormDialog, ValueRows } from "./layout";

const OUTCOMES: Record<string, string> = {
  pass: "Passed",
  assertion_failure: "Failed",
  execution_error: "Error",
  "reviewed-extract-only": "Reviewed extract",
};

/** The Support summary sheet of one report, or of a report chosen in it when
 * opened from Diagnostics. */
export function SupportSummarySheet({
  open,
  root,
  projectId,
  report,
  onClose,
  onRequestApproval,
}: {
  open: boolean;
  root: string;
  projectId: string;
  report: ItemRef | null;
  onClose: () => void;
  /** Continues a summary written for the customer hub in Team. */
  onRequestApproval: () => void;
}) {
  const scope = useRef(new RequestScope());
  const context = useCallback(() => scope.current.enter(root, projectId), [root, projectId]);
  const [source, setSource] = useState<string>(report?.id ?? "");
  const [hub, setHub] = useState(false);
  const [destination, setDestination] = useState<{ handle: string; name: string; location: string } | null>(null);
  const [review, setReview] = useState<ActionReview | null>(null);
  const [refusal, setRefusal] = useState<string | null>(null);
  const [policyOpen, setPolicyOpen] = useState(false);
  const [generation, setGeneration] = useState(0);
  const [result, setResult] = useState<ReviewedActionResult | null>(null);
  const intent = useRef<{ token: string; id: string } | null>(null);
  // The reports a summary can be made of, when the sheet is opened without one.
  const [reports, setReports] = useState<CatalogItem[]>([]);

  useEffect(() => {
    if (!open) return;
    setSource(report?.id ?? "");
    setHub(false);
    setDestination(null);
    setResult(null);
    if (report) return;
    let current = true;
    void listWholeCatalog({ context: context(), kind: "report", filter: {} }).then((answer) => {
      if (!current) return;
      const listed = (answer.page?.items ?? []).filter((item) => item.availability === "available" && item.summary.report?.form !== "export-review");
      setReports(listed);
      setSource((held) => held || (listed[0]?.ref.id ?? ""));
    });
    return () => {
      current = false;
    };
    // Each opening starts again.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  useEffect(() => {
    if (!open || !source) return;
    let current = true;
    setReview(null);
    setRefusal(null);
    intent.current = null;
    void prepareAction({
      context: context(),
      action: "report.support",
      items: [{ kind: "report", id: source }],
      support: { ...(hub ? { hub: true } : destination ? { destination: destination.handle } : {}) },
    }).then((answer) => {
      if (!current) return;
      if (answer.review) setReview(answer.review);
      else setRefusal(answer.reason ?? "This report has no support summary.");
    });
    return () => {
      current = false;
    };
  }, [open, source, hub, destination, generation, context]);

  const shown = review?.report_support;
  const summary = shown?.summary;
  const policy = shown?.policy;
  const done = result?.outcome === "completed";
  return (
    <>
      <FormDialog
        open={open && !policyOpen}
        title="Support summary"
        submitLabel={done ? "Done" : hub ? "Request approval" : "Export support summary"}
        submitDisabled={!done && (!review?.ready || !review.token)}
        onClose={onClose}
        status={shown && !done ? <span className="consequence">{hub ? "Writes the summary into this project for the team's approval." : "Exports the summary to a new local folder."}</span> : null}
        onSubmit={async () => {
          if (done) {
            onClose();
            return null;
          }
          if (!review?.token) return { reason: review?.refusal ?? "This summary cannot be exported." };
          if (intent.current?.token !== review.token) intent.current = { token: review.token, id: newIntentId() };
          const answer = await executeReviewedAction({ context: context(), token: review.token, intent_id: intent.current.id, decisions: {} });
          if (answer.outcome !== "completed") {
            if (answer.refreshed) setReview(answer.refreshed);
            return { reason: answer.reason ?? "The summary was not exported." };
          }
          setResult(answer);
          if (hub) onRequestApproval();
          return null;
        }}
      >
        {!report && reports.length > 0 ? (
          <>
            <label htmlFor="support-source">Report</label>
            <select id="support-source" value={source} onChange={(event) => setSource(event.target.value)}>
              {reports.map((item) => (
                <option key={item.ref.id} value={item.ref.id}>
                  {item.name}
                </option>
              ))}
            </select>
          </>
        ) : null}
        {!report && reports.length === 0 ? <p>No reports</p> : null}
        {refusal ? <p role="alert">{refusal}</p> : null}
        {review?.refusal && !done ? <p role="alert">{review.refusal}</p> : null}
        {shown ? (
          <ValueRows
            label="Summary"
            rows={[
              { label: "Source", value: shown.report },
              {
                label: "Destination",
                value: (
                  <span className="value-with-action">
                    {hub ? "Customer hub" : destination ? [destination.name, destination.location].filter(Boolean).join(" · ") : "Local file"}
                    {!hub ? (
                      <button
                        type="button"
                        className="quiet"
                        onClick={() =>
                          void chooseShareDestination({ context: context(), name: "Support summary", folder: true }).then((answer) => {
                            if (answer.state === "completed" && answer.destination) setDestination({ handle: answer.destination, name: answer.name ?? "", location: answer.location ?? "" });
                          })
                        }
                      >
                        {destination ? "Change" : "Choose"}
                      </button>
                    ) : null}
                    {shown.hub_allowed ? (
                      <button type="button" className="quiet" onClick={() => setHub(!hub)}>
                        {hub ? "Local file" : "Customer hub"}
                      </button>
                    ) : null}
                  </span>
                ),
              },
              {
                label: "Sharing policy",
                value: (
                  <span className="value-with-action">
                    {policy ? `${policy.support ? "Allowed" : "Denied"} · up to ${policy.max_bytes} bytes` : "Not set"}
                    <button type="button" className="quiet" onClick={() => setPolicyOpen(true)}>
                      Change
                    </button>
                  </span>
                ),
              },
              ...(summary
                ? [
                    { label: "Outcome", value: OUTCOMES[summary.outcome] ?? summary.outcome },
                    { label: "Size", value: `${shown.size} bytes` },
                    { label: "Source identity", value: summary.source_identity },
                    { label: "Case identity", value: summary.input_commitment },
                    { label: "Test identity", value: summary.spec_identity },
                    { label: "Policy identity", value: summary.policy_identity },
                    { label: "Scope", value: summary.scope },
                  ]
                : []),
            ]}
          />
        ) : null}
        {done && result?.support ? (
          <div className="share-result" role="status">
            <p>{result.support.hub ? "Written for the team's approval" : `Exported ${result.support.name}`}</p>
            {result.support.output && !result.support.hub ? (
              <button type="button" onClick={() => void openSharedOutput({ output: result.support!.output!, folder: true })}>
                Show in folder
              </button>
            ) : null}
          </div>
        ) : null}
      </FormDialog>
      <SharingPolicySheet
        open={open && policyOpen}
        context={context}
        policy={policy ? { support: policy.support, destinations: policy.destinations, max_bytes: policy.max_bytes } : null}
        onClose={() => setPolicyOpen(false)}
        onSaved={() => {
          setPolicyOpen(false);
          setGeneration((value) => value + 1);
        }}
      />
    </>
  );
}

/** The project's sharing policy: whether support summaries may be prepared,
 * where they may go and how large they may be. Saving it approves nothing. */
function SharingPolicySheet({
  open,
  context,
  policy,
  onClose,
  onSaved,
}: {
  open: boolean;
  context: () => { project: string; generation: number };
  policy: { support: boolean; destinations: string[]; max_bytes: number } | null;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [support, setSupport] = useState(true);
  const [local, setLocal] = useState(true);
  const [hub, setHub] = useState(false);
  const [bytes, setBytes] = useState("4096");
  useEffect(() => {
    if (!open) return;
    setSupport(policy?.support ?? true);
    setLocal(policy ? policy.destinations.includes("local-file") : true);
    setHub(policy ? policy.destinations.includes("customer-hub-download") : false);
    setBytes(String(policy?.max_bytes ?? 4096));
  }, [open, policy]);
  return (
    <FormDialog
      open={open}
      title="Sharing policy"
      size="small"
      submitLabel="Save"
      submitDisabled={!local && !hub}
      onClose={onClose}
      onSubmit={async () => {
        const destinations = [...(local ? ["local-file"] : []), ...(hub ? ["customer-hub-download"] : [])];
        const saved = await saveProjectSharingPolicy({ context: context(), support, destinations, max_bytes: Number(bytes) });
        if (saved.state !== "completed") return { reason: saved.reason ?? "The policy was not saved.", field: "sharing-max-bytes" };
        onSaved();
        return null;
      }}
    >
      <fieldset className="checks">
        <legend>Support preparation</legend>
        <label className="check">
          <input type="radio" name="sharing-support" checked={support} onChange={() => setSupport(true)} />
          Allowed
        </label>
        <label className="check">
          <input type="radio" name="sharing-support" checked={!support} onChange={() => setSupport(false)} />
          Denied
        </label>
      </fieldset>
      <fieldset className="checks">
        <legend>Destination channels</legend>
        <label className="check">
          <input type="checkbox" checked={local} onChange={(event) => setLocal(event.target.checked)} />
          Local file
        </label>
        <label className="check">
          <input type="checkbox" checked={hub} onChange={(event) => setHub(event.target.checked)} />
          Customer hub
        </label>
      </fieldset>
      <label htmlFor="sharing-max-bytes">Maximum size (bytes)</label>
      <input id="sharing-max-bytes" inputMode="numeric" value={bytes} onChange={(event) => setBytes(event.target.value)} />
    </FormDialog>
  );
}

