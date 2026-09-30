// Share report (#560, views 27 and 46): one started flow over one report
// version, Contents → Redaction → Preview, with one shared footer. Every
// choice prepares the share again: the backend enumerates what it holds,
// derives or leaves each value it restates, and generates the exact output,
// which the preview shows. The final Export or Send acts on exactly that
// preview once; any change withdraws it and a fresh preview needs a fresh
// click. Closing keeps a draft of the choices, never the preview.
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import {
  chooseShareDestination,
  executeReviewedAction,
  newIntentId,
  openSharedOutput,
  prepareAction,
  RequestScope,
  saveShareTemplate,
  type ActionReview,
  type EditorDraft,
  type ItemRef,
  type RedactFieldRule,
  type ReportShareOptions,
  type ReportShareReview,
  type ReviewedActionResult,
  type ShareContents,
  type ShareEncryption,
  type ShareFile,
  type ShareOverrides,
  type ShareRow,
  type ShareValueTreatment,
} from "./bindings";
import { DataTable } from "./DataTable";
import { SHARE_CATEGORIES, SHARE_FORMATS, SHARE_ITEM_TYPES, SHARE_RESULTS, SHARE_TREATMENTS, term } from "./display";
import { useRetainer } from "./drafting";
import { FormDialog, Reveal, ValueRows } from "./layout";
import type { SendRequest } from "./RunPanel";
import { TaskTabs } from "./TaskTabs";
import "./share.css";

export type ShareStep = "contents" | "redaction" | "preview";
type Format = keyof typeof SHARE_FORMATS;

/** What a share draft holds: every choice, never a preview or a place. */
export type ShareDraft = {
  contents: ShareContents;
  template: string;
  overrides: ShareOverrides;
  format: Format;
  paper: "letter" | "a4";
  project: string;
  encrypt: ShareEncryption | null;
};

export const SHARE_DRAFT_KIND = "report-share";
export const SHARE_DRAFT_SCHEMA = "readmit-report-share-draft/v1";

const STEPS: { key: ShareStep; label: string }[] = [
  { key: "contents", label: "Contents" },
  { key: "redaction", label: "Redaction" },
  { key: "preview", label: "Preview" },
];

function freshDraft(): ShareDraft {
  return { contents: {}, template: "", overrides: {}, format: "pdf", paper: "letter", project: "", encrypt: null };
}

function isDraft(value: unknown): value is ShareDraft & { report: string } {
  if (value === null || typeof value !== "object") return false;
  const draft = value as Record<string, unknown>;
  return typeof draft.report === "string" && typeof draft.template === "string" && typeof draft.format === "string" && typeof draft.contents === "object";
}

/** A retained share draft of one report, when this viewer holds one. */
export function shareDraftOf(drafts: EditorDraft[] | null | undefined, workspace: string, report: string): EditorDraft | null {
  return drafts?.find((draft) => draft.kind === SHARE_DRAFT_KIND && draft.workspace === workspace && isDraft(draft.content) && draft.content.report === report) ?? null;
}

/** A place chosen for one output: its handle, what the window shows of it,
 * and the output it was chosen for. */
type Chosen = { handle: string; name: string; location: string; output: string } | null;

type ShareProps = {
  root: string | null;
  projectId: string;
  report: ItemRef | null;
  start: ShareStep;
  drafts?: EditorDraft[] | null;
  busy: boolean;
  /** Opens the reviewed run of a template's check (#555). */
  onRun: (request: SendRequest) => void;
  onManageTemplates: () => void;
  onLeave: () => void;
};

/** The Share report flow: its page title, body and footer. */
export function useShareReport({ root, projectId, report, start, drafts, busy, onRun, onManageTemplates, onLeave }: ShareProps) {
  const scope = useRef(new RequestScope());
  const context = useCallback(() => scope.current.enter(root ?? "", projectId), [root, projectId]);
  const [step, setStep] = useState<ShareStep>(start);
  const [draft, setDraft] = useState<ShareDraft>(freshDraft);
  const [chosen, setChosen] = useState<Chosen>(null);
  const [reveal, setReveal] = useState(false);
  const [review, setReview] = useState<ActionReview | null>(null);
  const [refusal, setRefusal] = useState<string | null>(null);
  const [preparing, setPreparing] = useState(false);
  const [sheet, setSheet] = useState<null | "destination" | "format" | "encryption" | "template" | "save-template" | "check" | { row: ShareRow }>(null);
  const [result, setResult] = useState<ReviewedActionResult | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [sending, setSending] = useState(false);
  // Counts previews asked for again because a final click found them stale.
  const [again, setAgain] = useState(0);
  const intent = useRef<{ token: string; id: string } | null>(null);
  const [templateName, setTemplateName] = useState("");
  const retainer = useRetainer();
  const reportId = report?.id ?? "";

  // A new report, or another project, starts a new share, from its retained
  // draft when there is one. Leaving the page and coming back keeps it.
  const started = useRef("");
  useEffect(() => {
    if (!reportId || !root) return;
    const at = JSON.stringify([root, reportId, start]);
    if (started.current === at) return;
    started.current = at;
    setStep(start);
    setChosen(null);
    setReveal(false);
    setResult(null);
    setNotice(null);
    setReview(null);
    const held = root ? shareDraftOf(drafts, root, reportId) : null;
    if (held && isDraft(held.content)) {
      const { report: _report, ...restored } = held.content;
      setDraft({ ...freshDraft(), ...restored });
      retainer.keepId(held.id);
    } else {
      setDraft(start === "preview" ? { ...freshDraft(), format: "pdf" } : freshDraft());
      retainer.clear();
    }
    // Restored once per report of a project.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [root, reportId, start]);

  // Every change of the choices is retained as the draft.
  const dirty = useRef(false);
  const change = (next: ShareDraft) => {
    dirty.current = true;
    setDraft(next);
    setResult(null);
    setNotice(null);
  };
  useEffect(() => {
    if (!dirty.current || !root || !report) return;
    retainer.save({
      id: "",
      kind: SHARE_DRAFT_KIND,
      workspace: root,
      case: "",
      identity: "",
      content_schema: SHARE_DRAFT_SCHEMA,
      content: { report: report.id, ...draft },
      ...(projectId ? { item: { project_id: projectId, ref: report } } : {}),
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [draft]);

  const send = draft.project !== "";
  const options = useMemo<ReportShareOptions>(
    () => ({
      contents: draft.contents,
      ...(draft.template ? { template: draft.template } : {}),
      overrides: draft.overrides,
      format: draft.format,
      ...(draft.format === "pdf" ? { paper: draft.paper } : {}),
      ...(send ? { project: draft.project } : chosen ? { destination: chosen.handle } : {}),
      ...(draft.encrypt && !send ? { encrypt: draft.encrypt } : {}),
      ...(reveal ? { reveal: true } : {}),
    }),
    [draft, chosen, reveal, send],
  );
  const key = JSON.stringify(options);

  // A changed choice withdraws the preview at once and prepares it again.
  useEffect(() => {
    if (!root || !report) return;
    setReview((held) => (held ? { ...held, token: "", ready: false } : held));
    intent.current = null;
    setPreparing(true);
    let current = true;
    const timer = window.setTimeout(() => {
      void prepareAction({ context: context(), action: send ? "report.send" : "report.share", items: [report], report_share: options }).then((answer) => {
        if (!current) return;
        setPreparing(false);
        if (answer.review) {
          setReview(answer.review);
          setRefusal(null);
        } else {
          setRefusal(answer.reason ?? "This report cannot be shared.");
        }
      });
    }, 200);
    return () => {
      current = false;
      window.clearTimeout(timer);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, root, reportId, again]);

  const shown = review?.report_share ?? null;
  // A place is chosen for one output: another format, name or kind of output
  // needs a place of its own.
  const outputKey = shown ? `${shown.output.type}|${shown.output.name}` : "";
  const place = chosen && (!shown || chosen.output === outputKey) ? chosen : null;
  const fileTab = useFileTab(shown?.output.files ?? []);

  const choose = async () => {
    const folder = shown ? shown.output.type !== "file" : false;
    const name = shown?.output.name ?? "Report";
    const answer = await chooseShareDestination({ context: context(), name, ...(folder ? { folder: true } : {}) });
    if (answer.state === "completed" && answer.destination) {
      setChosen({ handle: answer.destination, name: answer.name ?? name, location: answer.location ?? "", output: outputKey });
      setResult(null);
    } else if (answer.state !== "cancelled") {
      setNotice(answer.reason ?? "No place was chosen.");
    }
  };

  const finish = async () => {
    if (!review?.token || !review.ready) return;
    if (intent.current?.token !== review.token) intent.current = { token: review.token, id: newIntentId() };
    setSending(true);
    const answer = await executeReviewedAction({ context: context(), token: review.token, intent_id: intent.current.id, decisions: {} });
    setSending(false);
    setResult(answer);
    if (answer.outcome === "completed") {
      void retainer.dropCurrent();
      dirty.current = false;
    } else if (answer.refreshed) {
      intent.current = null;
      setReview(answer.refreshed);
      setNotice("What the preview showed changed; review it again.");
    } else if (answer.outcome === "stale") {
      intent.current = null;
      setNotice(answer.reason ?? "What the preview showed changed; review it again.");
      setAgain((count) => count + 1);
    }
  };

  if (!report || !root) return { title: "Share report", body: null };
  // Leaving the flow ends this share; its draft, if any, is what a later
  // Share of the report starts from.
  const leave = () => {
    started.current = "";
    onLeave();
  };

  const setContents = (contents: ShareContents) => change({ ...draft, contents });
  const removed = new Set(draft.contents.removed ?? []);
  const toggleItem = (itemKey: string) => {
    const next = new Set(removed);
    if (next.has(itemKey)) next.delete(itemKey);
    else next.add(itemKey);
    setContents({ ...draft.contents, removed: [...next] });
  };

  const outputType = shown?.output.type ?? "file";
  const destinationValue = send ? draft.project : "Local file";
  const locationValue = place ? [place.name, place.location].filter(Boolean).join(" · ") : null;

  const outputRows = (changeable: boolean) => [
    {
      label: "Destination",
      value: (
        <span className="value-with-action">
          {destinationValue}
          {changeable && (shown?.projects.length ?? 0) > 0 ? (
            <button type="button" className="quiet" onClick={() => setSheet("destination")}>
              Change
            </button>
          ) : null}
        </span>
      ),
    },
    {
      label: "Format",
      value: (
        <span className="value-with-action">
          {formatText(draft, outputType)}
          {changeable ? (
            <button type="button" className="quiet" onClick={() => setSheet("format")}>
              Change
            </button>
          ) : null}
        </span>
      ),
    },
    ...(send
      ? []
      : [
          {
            label: outputType === "file" ? "File" : "Folder",
            value: (
              <span className="value-with-action">
                {locationValue ?? "Not chosen"}
                <button type="button" className="quiet" onClick={() => void choose()}>
                  {place ? "Change" : "Choose"}
                </button>
              </span>
            ),
          },
          {
            label: "Encryption",
            value: (
              <span className="value-with-action">
                {shown?.encryption ? `${shown.encryption.control} · ${shown.encryption.reference} · Generation ${shown.encryption.generation}` : "Off"}
                {changeable ? (
                  <button type="button" className="quiet" onClick={() => setSheet("encryption")}>
                    Change
                  </button>
                ) : null}
              </span>
            ),
          },
        ]),
  ];

  const contentsBody = (
    <>
      <h2 className="section-heading">Contents</h2>
      {shown ? (
        <DataTable
          label="Contents"
          className="page-table"
          rows={shown.items}
          rowId={(item) => item.key}
          rowLabel={(item) => item.name}
          columns={[
            { key: "name", header: "Name", priority: 1, minWidth: 12, flex: true, render: (item) => item.name },
            { key: "type", header: "Type", priority: 1, minWidth: 8, render: (item) => term(SHARE_ITEM_TYPES, item.type).text },
            {
              key: "include",
              header: "",
              priority: 1,
              minWidth: 7,
              render: (item) =>
                item.removable ? (
                  <button type="button" className="quiet" aria-label={`${item.included ? "Remove" : "Add"} ${item.name}`} onClick={() => toggleItem(item.key)}>
                    {item.included ? "Remove" : "Add"}
                  </button>
                ) : null,
            },
          ]}
          selected={null}
          onSelect={() => undefined}
          onOpen={() => undefined}
        />
      ) : null}
      <fieldset className="checks">
        <legend>Include</legend>
        <label className="check">
          <input type="checkbox" checked={draft.contents.messages ?? false} onChange={(event) => setContents({ ...draft.contents, messages: event.target.checked })} />
          Selected messages
        </label>
        <label className="check">
          <input
            type="checkbox"
            checked={draft.contents.attachments ?? false}
            disabled={(shown?.attachments ?? 0) === 0 && !draft.contents.attachments}
            onChange={(event) => setContents({ ...draft.contents, attachments: event.target.checked })}
          />
          Attachments
        </label>
        <label className="check">
          <input type="checkbox" checked={draft.contents.original ?? false} onChange={(event) => setContents({ ...draft.contents, original: event.target.checked })} />
          Original evidence
        </label>
      </fieldset>
      <h2 className="section-heading">Output</h2>
      <ValueRows label="Output" rows={outputRows(true)} />
    </>
  );

  const rows = shown?.rows ?? [];
  const redactionBody = (
    <>
      <ValueRows
        label="Template"
        rows={[
          {
            label: "Template",
            value: (
              <span className="value-with-action">
                {shown?.template ?? (draft.template ? templateLabel(draft.template) : "No template")}
                <button type="button" className="quiet" onClick={() => setSheet("template")}>
                  Change
                </button>
              </span>
            ),
          },
        ]}
      />
      <div className="toolbar-group">
        <button type="button" disabled={busy} onClick={() => setSheet("save-template")}>
          Save template
        </button>
        <button type="button" onClick={onManageTemplates}>
          Manage templates
        </button>
        <Reveal revealed={reveal} onToggle={setReveal} disabled={rows.every((row) => row.result === "original")} />
      </div>
      {rows.length === 0 ? (
        <p>No values</p>
      ) : (
        <DataTable
          label="Redaction"
          className="page-table"
          rows={rows}
          rowId={(row) => row.key}
          rowLabel={(row) => row.field}
          columns={[
            { key: "field", header: "Category / Field", priority: 1, minWidth: 14, flex: true, render: (row) => `${term(SHARE_CATEGORIES, row.category).text} · ${row.field}` },
            { key: "occurrences", header: "Occurrences", priority: 2, minWidth: 6, render: (row) => String(row.occurrences) },
            { key: "treatment", header: "Treatment", priority: 1, minWidth: 8, render: (row) => term(SHARE_TREATMENTS, row.treatment).text },
            { key: "result", header: "Result", priority: 1, minWidth: 8, render: (row) => term(SHARE_RESULTS, row.result).text },
            ...(reveal
              ? [{ key: "values", header: "Values", priority: 2, minWidth: 12, render: (row: ShareRow) => (row.examples ?? []).map(exampleText).join("; ") }]
              : []),
          ]}
          selected={null}
          onSelect={() => undefined}
          onOpen={(rowKey) => {
            const row = rows.find((entry) => entry.key === rowKey);
            if (row) setSheet({ row });
          }}
        />
      )}
      {shown?.run_check ? (
        <div className="share-check">
          <ValueRows label="Check" rows={[{ label: "Check run", value: shown.run_check.satisfied ? "Matched" : "Not run" }]} />
          {!shown.run_check.satisfied ? (
            <button type="button" disabled={busy} onClick={() => setSheet("check")}>
              Run check
            </button>
          ) : null}
        </div>
      ) : null}
    </>
  );

  const previewBody = (
    <>
      <ValueRows label="Output" rows={outputRows(false)} />
      {shown && shown.issues.length > 0 ? (
        <section aria-label="Unresolved" className="share-issues">
          <h2 className="section-heading">Unresolved</h2>
          <ul>
            {shown.issues.map((issue) => (
              <li key={issue.key}>{issue.text}</li>
            ))}
          </ul>
        </section>
      ) : null}
      {shown ? <OutputViewer files={shown.output.files} tab={fileTab} /> : null}
    </>
  );

  const final = send ? "Send" : "Export";
  const done = result?.outcome === "completed";
  const outcome = result ? <ShareOutcome result={result} /> : null;
  const footer = (
    <div className="flow-footer share-footer">
      {step === "contents" ? (
        <button type="button" onClick={leave}>
          Cancel
        </button>
      ) : (
        <button type="button" disabled={sending} onClick={() => setStep(step === "preview" ? "redaction" : "contents")}>
          Back
        </button>
      )}
      <div className="share-final">
        {step === "preview" && shown?.consequence && !done ? <p className="consequence">{shown.consequence}</p> : null}
        {step === "contents" ? (
          <button type="button" className="primary" onClick={() => setStep("redaction")}>
            Redaction
          </button>
        ) : step === "redaction" ? (
          <button type="button" className="primary" onClick={() => setStep("preview")}>
            Preview
          </button>
        ) : done ? (
          <button type="button" className="primary" onClick={leave}>
            Done
          </button>
        ) : (
          <button type="button" className="primary" disabled={busy || sending || preparing || !review?.ready || !review.token} onClick={() => void finish()}>
            {final}
          </button>
        )}
      </div>
    </div>
  );

  const body = (
    <div className={step === "preview" ? "flow share-flow share-preview" : "flow share-flow"}>
      <p className="object-subtitle">{[shown?.report, shown?.version ? `Version ${shown.version}` : null].filter(Boolean).join(" · ")}</p>
      <ol className="flow-steps" aria-label="Steps">
        {STEPS.map((entry) => (
          <li key={entry.key} aria-current={entry.key === step ? "step" : undefined}>
            {entry.label}
          </li>
        ))}
      </ol>
      {refusal ? <p role="alert">{refusal}</p> : null}
      {notice ? <p role="status">{notice}</p> : null}
      {step === "preview" && review?.refusal && !done ? <p role="alert">{review.refusal}</p> : null}
      {step === "contents" ? contentsBody : step === "redaction" ? redactionBody : previewBody}
      {outcome}
      {!shown && !refusal ? <p aria-live="polite">Preparing…</p> : null}
      {footer}

      <DestinationSheet
        open={sheet === "destination"}
        projects={shown?.projects ?? []}
        project={draft.project}
        onClose={() => setSheet(null)}
        onApply={(project) => {
          change({ ...draft, project, encrypt: project ? null : draft.encrypt });
          setSheet(null);
        }}
      />
      <FormatSheet
        open={sheet === "format"}
        format={draft.format}
        paper={draft.paper}
        onClose={() => setSheet(null)}
        onApply={(format, paper) => {
          change({ ...draft, format, paper });
          setSheet(null);
        }}
      />
      <EncryptionSheet
        open={sheet === "encryption"}
        review={shown}
        encrypt={draft.encrypt}
        onClose={() => setSheet(null)}
        onApply={(encrypt) => {
          change({ ...draft, encrypt });
          setSheet(null);
        }}
      />
      <TemplateSheet
        open={sheet === "template"}
        review={shown}
        template={draft.template}
        onClose={() => setSheet(null)}
        onApply={(template) => {
          change({ ...draft, template });
          setSheet(null);
        }}
      />
      <FormDialog
        open={sheet === "save-template"}
        title="Save template"
        size="small"
        submitLabel="Save"
        submitDisabled={templateName.trim() === ""}
        onClose={() => {
          setSheet(null);
          setTemplateName("");
        }}
        onSubmit={async () => {
          const name = templateName.trim();
          const saved = await saveShareTemplate({ context: context(), name, report, ...(draft.template ? { base: draft.template } : {}), overrides: draft.overrides });
          if (saved.state !== "completed" || !saved.template) return { reason: saved.reason ?? "The template was not saved.", field: "share-template-name" };
          // The template now holds the draft's field rules and regeneration.
          const { fields: _fields, packet: _packet, ...kept } = draft.overrides;
          change({ ...draft, template: saved.template.entry, overrides: kept });
          setSheet(null);
          setTemplateName("");
          return null;
        }}
      >
        <label htmlFor="share-template-name">Name</label>
        <input id="share-template-name" value={templateName} maxLength={80} onChange={(event) => setTemplateName(event.target.value)} />
      </FormDialog>
      {typeof sheet === "object" && sheet !== null ? (
        <RowSheet
          row={sheet.row}
          review={shown}
          draft={draft}
          revealed={reveal}
          onClose={() => setSheet(null)}
          onApply={(next) => {
            change(next);
            setSheet(null);
          }}
        />
      ) : null}
      <CheckSheet
        open={sheet === "check"}
        context={context}
        report={report}
        options={options}
        onClose={() => setSheet(null)}
        onRun={(request) => {
          setSheet(null);
          onRun(request);
        }}
      />
    </div>
  );
  return { title: "Share report", body };
}

function templateLabel(entry: string): string {
  return entry.replace(/\.json$/i, "");
}

function exampleText(example: { original: string; derived: string }): string {
  return `${example.original || "Empty"} → ${example.derived || "Removed"}`;
}

function formatText(draft: ShareDraft, type: string): string {
  const format = SHARE_FORMATS[draft.format];
  const paper = draft.format === "pdf" ? ` · ${draft.paper === "a4" ? "A4" : "Letter"}` : "";
  if (type === "encrypted-package") return `Encrypted package · ${format}${paper}`;
  return format + paper;
}

/** Which of an output's files the preview shows. */
function useFileTab(files: ShareFile[]): { selected: string; select: (name: string) => void } {
  const [selected, select] = useState("");
  const names = files.map((file) => file.name);
  return { selected: names.includes(selected) ? selected : (names[0] ?? ""), select };
}

/** The exact generated output: a PDF's rendered pages, an HTML document in
 * a sandbox, any other text whole. Several files are tabs. */
function OutputViewer({ files, tab }: { files: ShareFile[]; tab: { selected: string; select: (name: string) => void } }) {
  const file = files.find((entry) => entry.name === tab.selected) ?? files[0];
  if (!file) return null;
  const view = <FileView file={file} />;
  if (files.length === 1) return <div className="share-output">{view}</div>;
  return (
    <div className="share-output">
      <TaskTabs label="Output files" id="share-output-files" tabs={files.map((entry) => ({ key: entry.name, label: entry.name }))} selected={file.name} onSelect={tab.select}>
        {view}
      </TaskTabs>
    </div>
  );
}

function FileView({ file }: { file: ShareFile }) {
  const [url, setUrl] = useState<string | null>(null);
  const pdf = file.kind === "report" && file.data !== undefined && file.data !== "";
  useEffect(() => {
    if (!pdf || !file.data) return;
    const bytes = Uint8Array.from(atob(file.data), (char) => char.charCodeAt(0));
    const made = URL.createObjectURL(new Blob([bytes], { type: "application/pdf" }));
    setUrl(made);
    return () => URL.revokeObjectURL(made);
  }, [pdf, file.data]);
  if (file.kind === "derived-test") return <p>Generated at export</p>;
  if (pdf) return url ? <iframe className="share-document" title={file.name} src={url} /> : null;
  if (file.name.endsWith(".html") && file.text !== undefined) {
    return <iframe className="share-document" title={file.name} sandbox="" srcDoc={file.text} />;
  }
  if (file.text !== undefined) {
    return (
      <pre className="share-text" aria-label={file.name}>
        {file.text}
      </pre>
    );
  }
  return <ValueRows rows={[{ label: file.name, value: `${file.size} bytes` }]} />;
}

function ShareOutcome({ result }: { result: ReviewedActionResult }) {
  const shared = result.report_share;
  if (result.outcome === "completed" && shared) {
    const where = shared.project ? `Sent to ${shared.project}` : `Exported ${shared.name}`;
    return (
      <div className="share-result" role="status">
        <p>{where}</p>
        {result.reason ? <p role="alert">{result.reason}</p> : null}
        {shared.output ? (
          <div className="toolbar-group">
            {shared.name.includes(".") ? (
              <button type="button" onClick={() => void openSharedOutput({ output: shared.output! })}>
                Open file
              </button>
            ) : null}
            <button type="button" onClick={() => void openSharedOutput({ output: shared.output!, folder: true })}>
              Show in folder
            </button>
          </div>
        ) : null}
      </div>
    );
  }
  if (result.outcome === "uncertain") {
    return <p role="alert">{result.reason ?? "The send could not be confirmed; check the team's files before sending again."}</p>;
  }
  if (result.outcome === "stale") return null;
  return (
    <div role="alert">
      <p>{result.reason ?? "Nothing was exported."}</p>
      {shared?.incomplete ? <p>Incomplete: {shared.name}</p> : null}
    </div>
  );
}

function DestinationSheet({ open, projects, project, onClose, onApply }: { open: boolean; projects: string[]; project: string; onClose: () => void; onApply: (project: string) => void }) {
  const [value, setValue] = useState(project);
  useEffect(() => {
    if (open) setValue(project);
  }, [open, project]);
  return (
    <FormDialog open={open} title="Destination" size="small" submitLabel="Apply" onClose={onClose} onSubmit={() => onApply(value)}>
      <fieldset className="checks">
        <legend>Destination</legend>
        <label className="check">
          <input type="radio" name="share-destination" checked={value === ""} onChange={() => setValue("")} />
          Local file
        </label>
        {projects.map((name) => (
          <label key={name} className="check">
            <input type="radio" name="share-destination" checked={value === name} onChange={() => setValue(name)} />
            {name}
          </label>
        ))}
      </fieldset>
    </FormDialog>
  );
}

function FormatSheet({
  open,
  format,
  paper,
  onClose,
  onApply,
}: {
  open: boolean;
  format: Format;
  paper: "letter" | "a4";
  onClose: () => void;
  onApply: (format: Format, paper: "letter" | "a4") => void;
}) {
  const [chosen, setChosen] = useState(format);
  const [sheetPaper, setPaper] = useState(paper);
  useEffect(() => {
    if (open) {
      setChosen(format);
      setPaper(paper);
    }
  }, [open, format, paper]);
  return (
    <FormDialog open={open} title="Format" size="small" submitLabel="Apply" onClose={onClose} onSubmit={() => onApply(chosen, sheetPaper)}>
      <fieldset className="checks">
        <legend>Format</legend>
        {(Object.keys(SHARE_FORMATS) as Format[]).map((key) => (
          <label key={key} className="check">
            <input type="radio" name="share-format" checked={chosen === key} onChange={() => setChosen(key)} />
            {SHARE_FORMATS[key]}
          </label>
        ))}
      </fieldset>
      {chosen === "pdf" ? (
        <fieldset className="checks">
          <legend>Paper</legend>
          <label className="check">
            <input type="radio" name="share-paper" checked={sheetPaper === "letter"} onChange={() => setPaper("letter")} />
            Letter
          </label>
          <label className="check">
            <input type="radio" name="share-paper" checked={sheetPaper === "a4"} onChange={() => setPaper("a4")} />
            A4
          </label>
        </fieldset>
      ) : null}
    </FormDialog>
  );
}

function EncryptionSheet({
  open,
  review,
  encrypt,
  onClose,
  onApply,
}: {
  open: boolean;
  review: ReportShareReview | null;
  encrypt: ShareEncryption | null;
  onClose: () => void;
  onApply: (encrypt: ShareEncryption | null) => void;
}) {
  const controls = review?.controls ?? [];
  const keyOf = (control: { entry: string; control: string }) => `${control.entry}\u0000${control.control}`;
  const [on, setOn] = useState(encrypt !== null);
  const [chosen, setChosen] = useState(encrypt ? keyOf(encrypt) : "");
  useEffect(() => {
    if (!open) return;
    setOn(encrypt !== null);
    setChosen(encrypt ? keyOf(encrypt) : (controls[0] ? keyOf(controls[0]) : ""));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);
  const picked = controls.find((control) => keyOf(control) === chosen);
  return (
    <FormDialog
      open={open}
      title="Encryption"
      size="small"
      submitLabel="Apply"
      submitDisabled={on && !picked}
      onClose={onClose}
      onSubmit={() => onApply(on && picked ? { entry: picked.entry, control: picked.control, generation: picked.generation } : null)}
    >
      <label className="check">
        <input type="checkbox" checked={on} disabled={controls.length === 0} onChange={(event) => setOn(event.target.checked)} />
        Encrypt package
      </label>
      {controls.length === 0 ? <p>No active encryption control</p> : null}
      {on ? (
        <fieldset className="checks">
          <legend>Control</legend>
          {controls.map((control) => (
            <label key={keyOf(control)} className="check">
              <input type="radio" name="share-control" checked={chosen === keyOf(control)} onChange={() => setChosen(keyOf(control))} />
              {control.control} · Generation {control.generation}
              {control.retention ? ` · Retained ${control.retention}` : ""}
            </label>
          ))}
        </fieldset>
      ) : null}
    </FormDialog>
  );
}

function TemplateSheet({
  open,
  review,
  template,
  onClose,
  onApply,
}: {
  open: boolean;
  review: ReportShareReview | null;
  template: string;
  onClose: () => void;
  onApply: (template: string) => void;
}) {
  const [chosen, setChosen] = useState(template);
  useEffect(() => {
    if (open) setChosen(template);
  }, [open, template]);
  return (
    <FormDialog open={open} title="Template" size="small" submitLabel="Apply" onClose={onClose} onSubmit={() => onApply(chosen)}>
      <fieldset className="checks">
        <legend>Template</legend>
        <label className="check">
          <input type="radio" name="share-template" checked={chosen === ""} onChange={() => setChosen("")} />
          No template
        </label>
        {(review?.templates ?? []).map((entry) => (
          <label key={entry.entry} className="check">
            <input type="radio" name="share-template" checked={chosen === entry.entry} onChange={() => setChosen(entry.entry)} />
            {entry.name}
          </label>
        ))}
      </fieldset>
    </FormDialog>
  );
}

type Treatment = "remove" | "replace" | "surrogate" | "shift" | "keep" | "regenerate" | "exclude";

const RULE_POLICIES: Record<string, Treatment> = {
  "remove-field/v1": "remove",
  "replace-field/v1": "replace",
  "scoped-surrogate/v1": "surrogate",
  "patient-date-shift/v1": "shift",
  "retain-literal/v1": "keep",
};
const POLICY_OF: Partial<Record<Treatment, string>> = {
  remove: "remove-field/v1",
  replace: "replace-field/v1",
  surrogate: "scoped-surrogate/v1",
  shift: "patient-date-shift/v1",
  keep: "retain-literal/v1",
};
const FIELD_CLASSES = Object.keys(SHARE_CATEGORIES).filter((key) => !["free-text", "metadata", "unassigned", "attachments", "original-evidence", "structural"].includes(key));

/** The treatments one row's sheet offers, by what the row is. */
function treatmentsOf(row: ShareRow): Treatment[] {
  switch (row.kind) {
    case "field":
      return row.selector ? ["remove", "replace", "surrogate", "shift", "keep"] : ["exclude"];
    case "free-text":
      return row.key === "title" ? ["replace"] : ["remove", "replace"];
    case "metadata":
      return row.key === "test-name" || row.key === "run-times" ? ["regenerate"] : [];
    case "value":
      return ["remove", "replace"];
    case "attachment":
    case "original":
      return ["exclude"];
  }
  return [];
}

const TREATMENT_LABELS: Record<Treatment, string> = {
  remove: "Remove",
  replace: "Replace",
  surrogate: "Scoped surrogate",
  shift: "Shift dates",
  keep: "Keep allowed values",
  regenerate: "Regenerate",
  exclude: "Remove from contents",
};

/** What Remove from contents takes out: a field of the messages takes out
 * the messages. */
function treatmentLabel(row: ShareRow, key: Treatment): string {
  return key === "exclude" && row.kind === "field" ? "Remove messages from contents" : TREATMENT_LABELS[key];
}

/** One row's treatment: only the treatments that apply to it, and only the
 * values each needs. Applying changes this share's draft; the template is
 * saved on its own. */
function RowSheet({
  row,
  review,
  draft,
  revealed,
  onClose,
  onApply,
}: {
  row: ShareRow;
  review: ReportShareReview | null;
  draft: ShareDraft;
  revealed: boolean;
  onClose: () => void;
  onApply: (draft: ShareDraft) => void;
}) {
  const offered = treatmentsOf(row);
  const current = row.rule ? RULE_POLICIES[row.rule.policy] : row.treatment === "regenerate" ? "regenerate" : undefined;
  const [treatment, setTreatment] = useState<Treatment | undefined>(current && offered.includes(current) ? current : offered[0]);
  const [category, setCategory] = useState(row.rule?.class && row.rule.class !== "structural" ? row.rule.class : FIELD_CLASSES.includes(row.category) ? row.category : "other-unique-identifiers");
  const [replacement, setReplacement] = useState(row.rule?.replacement ?? "");
  const [scope, setScope] = useState(row.rule?.scope ?? "");
  const [authority, setAuthority] = useState((row.rule?.authority ?? []).join(", "));
  const [allowed, setAllowed] = useState((row.rule?.allowed ?? []).join("\n"));
  const apply = (): { reason: string } | null => {
    if (!treatment) return { reason: "Nothing here can be changed" };
    const overrides: ShareOverrides = { ...draft.overrides };
    let contents = draft.contents;
    switch (row.kind) {
      case "field": {
        if (treatment === "exclude") {
          contents = { ...contents, messages: false };
          break;
        }
        const rule: RedactFieldRule = { selector: row.selector!, policy: POLICY_OF[treatment]!, class: category };
        if (treatment === "replace") rule.replacement = replacement;
        if (treatment === "surrogate") {
          rule.scope = scope.trim();
          rule.authority = authority.split(",").map((part) => part.trim()).filter(Boolean);
        }
        if (treatment === "shift") rule.class = "dates-and-ages";
        if (treatment === "keep") {
          rule.class = "structural";
          rule.allowed = allowed.split("\n").filter((line) => line !== "");
        }
        overrides.fields = [...(overrides.fields ?? []).filter((held) => held.selector !== row.selector), rule];
        break;
      }
      case "free-text":
        if (row.key === "title") overrides.title = replacement;
        else overrides.notes = treatment === "remove" ? { row: "notes", remove: true } : { row: "notes", replace: replacement };
        break;
      case "metadata":
        overrides.packet = [...new Set([...(overrides.packet ?? []), "regenerate-metadata/v1"])];
        break;
      case "value": {
        const value: ShareValueTreatment = treatment === "remove" ? { row: row.key, remove: true } : { row: row.key, replace: replacement };
        overrides.values = [...(overrides.values ?? []).filter((held) => held.row !== row.key), value];
        break;
      }
      case "attachment":
      case "original":
        contents = { ...contents, removed: [...new Set([...(contents.removed ?? []), row.key])] };
        break;
    }
    onApply({ ...draft, overrides, contents });
    return null;
  };
  const patient = review?.patient ?? null;
  const fields: ReactNode[] = [];
  if (row.kind === "field" && treatment && treatment !== "exclude" && treatment !== "shift" && treatment !== "keep") {
    fields.push(
      <label key="category-label" htmlFor="share-row-category">
        Category
      </label>,
      <select key="category" id="share-row-category" value={category} onChange={(event) => setCategory(event.target.value)}>
        {FIELD_CLASSES.map((key) => (
          <option key={key} value={key}>
            {SHARE_CATEGORIES[key]}
          </option>
        ))}
      </select>,
    );
  }
  if (treatment === "replace") {
    fields.push(
      <label key="replacement-label" htmlFor="share-row-replacement">
        Replacement
      </label>,
      <input key="replacement" id="share-row-replacement" value={replacement} maxLength={1024} onChange={(event) => setReplacement(event.target.value)} />,
    );
  }
  if (treatment === "surrogate") {
    fields.push(
      <label key="scope-label" htmlFor="share-row-scope">
        Scope
      </label>,
      <input key="scope" id="share-row-scope" value={scope} maxLength={64} onChange={(event) => setScope(event.target.value)} />,
      <label key="authority-label" htmlFor="share-row-authority">
        Authority fields
      </label>,
      <input key="authority" id="share-row-authority" value={authority} onChange={(event) => setAuthority(event.target.value)} />,
    );
  }
  if (treatment === "shift") {
    fields.push(<ValueRows key="patient" rows={[{ label: "Patient", value: patient ?? "None" }]} />);
  }
  if (treatment === "keep") {
    fields.push(
      <label key="allowed-label" htmlFor="share-row-allowed">
        Allowed values
      </label>,
      <textarea key="allowed" id="share-row-allowed" rows={4} value={allowed} onChange={(event) => setAllowed(event.target.value)} />,
    );
  }
  return (
    <FormDialog
      open
      title={row.field}
      submitLabel="Apply"
      submitDisabled={offered.length === 0 || (treatment === "shift" && !patient) || (treatment === "replace" && replacement.trim() === "")}
      onClose={onClose}
      onSubmit={apply}
    >
      <ValueRows
        rows={[
          { label: "Category", value: term(SHARE_CATEGORIES, row.category).text },
          { label: "Occurrences", value: String(row.occurrences) },
          { label: "Result", value: term(SHARE_RESULTS, row.result).text },
        ]}
      />
      {revealed && (row.examples ?? []).length > 0 ? (
        <ValueRows label="Values" rows={(row.examples ?? []).map((example, index) => ({ label: String(index + 1), value: exampleText(example) }))} />
      ) : null}
      {offered.length > 0 ? (
        <fieldset className="checks">
          <legend>Treatment</legend>
          {offered.map((key) => (
            <label key={key} className="check">
              <input type="radio" name="share-row-treatment" checked={treatment === key} onChange={() => setTreatment(key)} />
              {treatmentLabel(row, key)}
            </label>
          ))}
        </fieldset>
      ) : (
        <p>The template decides this row</p>
      )}
      {fields}
    </FormDialog>
  );
}

/** Run check: the inventory the check derives from, declared reviewed, then
 * the derived test's reviewed run (#555). Preparing it sends nothing. */
function CheckSheet({
  open,
  context,
  report,
  options,
  onClose,
  onRun,
}: {
  open: boolean;
  context: () => { project: string; generation: number };
  report: ItemRef;
  options: ReportShareOptions;
  onClose: () => void;
  onRun: (request: SendRequest) => void;
}) {
  const [review, setReview] = useState<ActionReview | null>(null);
  const [refusal, setRefusal] = useState<string | null>(null);
  const [reviewed, setReviewed] = useState(false);
  const intent = useRef<string | null>(null);
  useEffect(() => {
    if (!open) return;
    setReview(null);
    setRefusal(null);
    setReviewed(false);
    intent.current = null;
    let current = true;
    void prepareAction({ context: context(), action: "report.prepare-check", items: [report], report_share: options }).then((answer) => {
      if (!current) return;
      if (answer.review) setReview(answer.review);
      else setRefusal(answer.reason ?? "The check cannot be prepared.");
    });
    return () => {
      current = false;
    };
    // Prepared each time the sheet opens.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);
  const check = review?.share_check;
  return (
    <FormDialog
      open={open}
      title="Run check"
      submitLabel="Run check"
      submitDisabled={!review?.ready || !review.token || !reviewed}
      onClose={onClose}
      onSubmit={async () => {
        if (!review?.token || !check) return { reason: "The check cannot be prepared." };
        intent.current ??= newIntentId();
        const answer = await executeReviewedAction({ context: context(), token: review.token, intent_id: intent.current, decisions: { declared_inventory: check.inventory.digest } });
        if (answer.outcome !== "completed" || !answer.share_check) return { reason: answer.reason ?? "The check was not prepared." };
        onRun({ kind: "reviewed", review: answer.share_check.review, packet: answer.share_check.packet, phase: answer.share_check.phase === "pass" ? "pass" : "failure" });
        return null;
      }}
    >
      {refusal ? <p role="alert">{refusal}</p> : null}
      {review?.refusal ? <p role="alert">{review.refusal}</p> : null}
      {check ? (
        <>
          <ValueRows
            rows={[
              { label: "Report", value: check.report },
              { label: "Reproduces", value: check.phase === "pass" ? "The passing run" : "The failed checks" },
              { label: "Messages", value: String(check.messages) },
            ]}
          />
          <ValueRows
            label="Inventory"
            rows={check.inventory.artifacts.map((artifact) => ({ label: artifact.path === "current" ? "Current run" : "Comparison run", value: "Result" }))}
          />
          <label className="check">
            <input type="checkbox" checked={reviewed} onChange={(event) => setReviewed(event.target.checked)} />
            Contents reviewed
          </label>
          <p className="consequence">Derives the check's messages in this project; nothing is sent until the run's own Send.</p>
        </>
      ) : !refusal ? (
        <p aria-live="polite">Preparing…</p>
      ) : null}
    </FormDialog>
  );
}
