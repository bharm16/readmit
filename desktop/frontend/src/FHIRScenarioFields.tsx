import { useState } from "react";
import type { FHIRScenarioStep } from "./bindings";
import { FormDialog } from "./layout";

/** Exact resource JSON and declared request facts are authored together.
 * Go validates and retains the whole step; the editor never parses JSON. */
export function FHIRStepSheet({ step, isNew, onClose, onSave }: { step: FHIRScenarioStep; isNew: boolean; onClose: () => void; onSave: (step: FHIRScenarioStep) => void }) {
  const [draft, setDraft] = useState(step);
  const patch = (next: Partial<FHIRScenarioStep>) => setDraft({ ...draft, ...next });
  const complete = draft.id.trim() !== "" && draft.after.trim() !== "" && (draft.source_kind === "request" ? Boolean(draft.request?.base && draft.request.url) : draft.document.trim() !== "");
  return <FormDialog open title={isNew ? "Add event" : "Edit event"} submitLabel={isNew ? "Add" : "Done"} submitDisabled={!complete} dirty={JSON.stringify(draft) !== JSON.stringify(step)} onClose={onClose} onSubmit={() => { onSave(draft); onClose(); }}>
    <label>Step name<input type="text" value={draft.id} onChange={(event) => patch({ id: event.target.value })} /></label>
    <label>After the previous event<input type="text" value={draft.after} onChange={(event) => patch({ after: event.target.value })} /></label>
    <label>Source type<select value={draft.source_kind} onChange={(event) => {
      const source_kind = event.target.value;
      const { request: _request, ...held } = draft;
      setDraft({ ...held, source_kind, ...(source_kind === "request" ? { request: draft.request ?? { base: "", method: "GET", url: "", headers: {} } } : {}) });
    }}><option value="resource">Resource</option><option value="bundle">Bundle</option><option value="request">Request evidence</option></select></label>
    <label>R4 JSON<textarea rows={12} spellCheck={false} value={draft.document} onChange={(event) => patch({ document: event.target.value })} /></label>
    {draft.request ? <>
      <label>Reference base<input type="text" value={draft.request.base} onChange={(event) => patch({ request: { ...draft.request!, base: event.target.value } })} /></label>
      <label>Request method<select value={draft.request.method} onChange={(event) => patch({ request: { ...draft.request!, method: event.target.value } })}>{["GET", "HEAD", "POST", "PUT", "PATCH", "DELETE"].map((method) => <option key={method}>{method}</option>)}</select></label>
      <label>Request URL<input type="text" value={draft.request.url} onChange={(event) => patch({ request: { ...draft.request!, url: event.target.value } })} /></label>
      {(["if_match", "if_none_match", "if_modified_since", "if_none_exist", "prefer"] as const).map((key) => <label key={key}>{key.replaceAll("_", "-")}<input type="text" value={draft.request?.headers[key] ?? ""} onChange={(event) => patch({ request: { ...draft.request!, headers: { ...draft.request!.headers, [key]: event.target.value } } })} /></label>)}
    </> : null}
    <p className="row-reason">FHIR R4 · 4.0.1 · application/fhir+json. Saving and generating retain evidence locally. Execution uses a separately reviewed target.</p>
  </FormDialog>;
}
