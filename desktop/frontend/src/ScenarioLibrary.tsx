// Library › Scenarios: a synthetic scenario's ordered events, the whole
// scenario editor with one Save, a deterministic in-memory Preview read in
// the shared message reader, and Create case, which generates the saved plan
// once. Nothing here starts a receiver, sends or runs a test.
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import {
  createScenarioCase,
  inspectScenarioPreview,
  listWholeCatalog,
  newIntentId,
  openItemDraft,
  previewScenarioDraft,
  saveItem,
  type CatalogItem,
  type InspectionResult,
  type ItemDraft,
  type ItemDraftResult,
  type ItemRef,
  type RequestContext,
  type ScenarioDraft,
  type ScenarioExpectation,
  type ScenarioPlanPreviewResult,
  type ScenarioScenario,
  type ScenarioStep,
} from "./bindings";
import { DataTable } from "./DataTable";
import { IconButton } from "./IconButton";
import { saveProblem } from "./Environments";
import { MessageReader } from "./Inspector";
import { EmptyState, FormDialog, Menu, Modal, ValueRows, type SubmitFailure } from "./layout";
import { HistorySheet, SaveButtons, exportItem, type LibraryPage } from "./Library";
import { useLifecycle } from "./lifecycle";
import { typeLabel } from "./Messages";
import { useVocabulary } from "./vocabulary";

const EXPECTATIONS: Record<ScenarioExpectation, string> = { accepted: "Accepted", refused: "Refused" };

type Draft = ScenarioDraft & { profile?: ItemRef };

/** The scenario a draft generates: its template, as the editor holds it. */
function scenarioOf(draft: Draft): ScenarioScenario | null {
  return draft.template ?? ((draft.plan.template as ScenarioScenario | null) ?? null);
}

function withScenario(draft: Draft, scenario: ScenarioScenario): Draft {
  return { ...draft, template: scenario };
}

function eventName(event: string, catalog: { event: string; description: string }[]): string {
  return catalog.find((entry) => entry.event === event)?.description || event;
}

/** A scenario: its saved detail, its editor, and its preview. */
export function useScenario({
  context,
  ref,
  imported,
  shown,
  busy,
  onSaved,
  onOpenCase,
}: {
  context: () => RequestContext;
  /** The scenario, or null for a new or imported one. */
  ref: ItemRef | null;
  imported: ItemDraftResult | null;
  shown: boolean;
  busy: boolean;
  onSaved: (ref: ItemRef) => void;
  onOpenCase: (caseRef: ItemRef, entry: string) => void;
}): LibraryPage {
  const vocabulary = useVocabulary()?.scenarios;
  const [opened, setOpened] = useState<ItemDraftResult | null>(null);
  const [editing, setEditing] = useState<{ name: string; draft: Draft } | null>(null);
  const [creating, setCreating] = useState(false);
  const [sheet, setSheet] = useState<null | { step: ScenarioStep; at: number | null } | "settings" | "history" | "details">(null);
  const [preview, setPreview] = useState<ScenarioPlanPreviewResult | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [problems, setProblems] = useState<string[]>([]);
  const [profiles, setProfiles] = useState<CatalogItem[]>([]);
  const reads = useLifecycle<"reading">({ background: true });
  const work = useLifecycle<"preview" | "create">({ window: true });
  const pending = useRef(false);
  const id = ref?.id ?? "";

  const read = useCallback(async () => {
    if (!ref) return;
    await reads.run("reading", async (current) => {
      const answer = await openItemDraft({ context: context(), ref });
      if (current()) setOpened(answer);
    });
  }, [id]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    setOpened(null);
    setEditing(null);
    setPreview(null);
    setNotice(null);
    setProblems([]);
    if (!shown) return;
    void listWholeCatalog({ context: context(), kind: "profile", filter: {} }).then((answer) => setProfiles((answer.page?.items ?? []).filter((item) => item.availability === "available")));
    if (imported?.draft?.scenario) {
      setOpened(imported);
      setEditing({ name: imported.draft.name ?? "", draft: imported.draft.scenario });
    } else if (ref) void read();
    else setCreating(true);
  }, [id, shown, imported]); // eslint-disable-line react-hooks/exhaustive-deps

  const saved = opened?.draft?.scenario as Draft | undefined;
  const draft = editing?.draft ?? saved;
  const scenario = draft ? scenarioOf(draft) : null;
  const name = editing?.name ?? opened?.draft?.name ?? "";
  const family = vocabulary?.templates.find((template) => template.profile === scenario?.profile)?.family ?? "";
  const events = vocabulary?.catalog.profiles.find((entry) => entry.name === scenario?.profile)?.events.filter((event) => event.available) ?? [];
  const profileName = draft?.profile ? profiles.find((item) => item.ref.id === draft.profile!.id)?.name : undefined;

  const edit = (next: ScenarioScenario) => {
    if (!editing) return;
    setEditing({ ...editing, draft: withScenario(editing.draft, next) });
    setPreview(null);
  };

  const runPreview = async () => {
    if (!draft) return;
    await work.run("preview", async (current) => {
      const answer = await previewScenarioDraft({ context: context(), kind: "scenario", ...(ref ? { item: ref.id } : {}), draft: { name, scenario: draft } });
      if (current()) setPreview(answer);
    });
  };

  const save = async (): Promise<SubmitFailure | void> => {
    if (!editing || pending.current) return;
    pending.current = true;
    try {
      const itemDraft: ItemDraft = { name: editing.name.trim(), scenario: editing.draft };
      const answer = await saveItem({
        context: context(),
        kind: "scenario",
        ...(ref ? { item: ref.id, ...(opened?.ref?.revision ? { base_revision: opened.ref.revision } : {}) } : {}),
        draft: itemDraft,
        intent_id: newIntentId(),
      });
      if (answer.outcome !== "saved" || !answer.saved) {
        setProblems(answer.problems.map((problem) => problem.problem));
        return saveProblem(answer, { name: "scenario-name" });
      }
      setEditing(null);
      setProblems([]);
      onSaved(answer.saved);
      if (ref) await read();
    } finally {
      pending.current = false;
    }
  };

  const createCase = async () => {
    if (!ref) return;
    await work.run("create", async (current) => {
      const answer = await createScenarioCase({ context: context(), scenario: { ...ref, ...(opened?.ref?.revision ? { revision: opened.ref.revision } : {}) }, intent_id: newIntentId() });
      if (!current()) return;
      if (answer.state === "completed" && answer.case && answer.entry) onOpenCase(answer.case, answer.entry);
      else setNotice(answer.reason ?? "No case was created.");
    });
  };

  const newSheet = creating ? (
    <NewScenarioSheet
      context={context}
      profiles={profiles}
      onClose={() => {
        setCreating(false);
        if (!editing) onSaved({ kind: "scenario", id: "" });
      }}
      onCreate={(title, next) => {
        setOpened({ state: "completed", context: context(), new: true, draft: { name: title, scenario: next } });
        setEditing({ name: title, draft: next });
        setCreating(false);
      }}
    />
  ) : null;

  if (!draft || !scenario) {
    return {
      title: "Scenario",
      actions: null,
      body: (
        <>
          {opened && !creating ? (
            <EmptyState
              title={opened.reason ?? "This scenario cannot be read."}
              action={
                <button type="button" onClick={() => void read()}>
                  Retry
                </button>
              }
            />
          ) : null}
          {newSheet}
        </>
      ),
    };
  }

  const steps = scenario.steps;
  const move = (from: number, to: number) => {
    const next = [...steps];
    const [step] = next.splice(from, 1);
    next.splice(to, 0, step!);
    edit({ ...scenario, steps: next });
  };

  let body: ReactNode = (
    <>
      {notice ? (
        <div className="notice" role="status">
          {notice}
        </div>
      ) : null}
      {editing ? (
        <div className="editor-fields">
          <label htmlFor="scenario-name">Name</label>
          <input id="scenario-name" type="text" maxLength={200} value={editing.name} onChange={(event) => setEditing({ ...editing, name: event.target.value })} />
        </div>
      ) : null}
      {problems.length > 0 ? (
        <ul className="notice danger" role="alert">
          {problems.map((problem) => (
            <li key={problem}>{problem}</li>
          ))}
        </ul>
      ) : null}
      <ValueRows
        rows={[
          { label: "Profile", value: profileName ? `${profileName} · v${draft.profile?.revision ?? ""}` : family || "—" },
          { label: "Origin", value: "Synthetic" },
        ]}
      />
      <DataTable
        label="Events"
        className="page-table"
        rows={steps}
        rowId={(step) => step.id}
        rowLabel={(step) => eventName(step.event, events)}
        selected={null}
        onSelect={() => {}}
        onOpen={(stepId) => {
          const at = steps.findIndex((step) => step.id === stepId);
          if (editing && at >= 0) setSheet({ step: steps[at]!, at });
        }}
        columns={[
          { key: "event", header: "Event", priority: 1, minWidth: 12, flex: true, render: (step) => eventName(step.event, events) },
          { key: "type", header: "Message", priority: 2, minWidth: 7, render: (step) => step.event.replace("^", " ") },
          { key: "after", header: "After", priority: 1, minWidth: 6, render: (step) => step.after || "—" },
          { key: "expect", header: "Expected", priority: 3, minWidth: 7, render: (step) => EXPECTATIONS[step.expect] },
          ...(editing
            ? [
                {
                  key: "actions",
                  header: "",
                  priority: 1,
                  minWidth: 3,
                  render: (step: ScenarioStep) => {
                    const at = steps.indexOf(step);
                    return (
                      <span className="row-actions" onClick={(event) => event.stopPropagation()} onKeyDown={(event) => event.stopPropagation()}>
                        <IconButton icon="up" label={`Move ${eventName(step.event, events)} up`} disabled={at === 0} onClick={() => move(at, at - 1)} />
                        <IconButton icon="down" label={`Move ${eventName(step.event, events)} down`} disabled={at === steps.length - 1} onClick={() => move(at, at + 1)} />
                        <Menu
                          label={`More actions for ${eventName(step.event, events)}`}
                          items={[
                            { label: "Edit", onSelect: () => setSheet({ step, at }) },
                            { label: "Duplicate", onSelect: () => edit({ ...scenario, steps: [...steps.slice(0, at + 1), { ...step, id: nextStepId(steps) }, ...steps.slice(at + 1)] }) },
                            { label: "Remove", onSelect: () => edit({ ...scenario, steps: steps.filter((_, index) => index !== at) }) },
                          ]}
                        />
                      </span>
                    );
                  },
                },
              ]
            : []),
        ]}
      />
    </>
  );
  if (preview) body = <PreviewBody preview={preview} busy={busy} onClose={() => setPreview(null)} />;

  body = (
    <>
      {body}
      {newSheet}
      {editing && sheet && typeof sheet === "object" ? (
        <EventSheet
          step={sheet.step}
          isNew={sheet.at === null}
          scenario={scenario}
          events={events}
          onClose={() => setSheet(null)}
          onApply={(step) => edit({ ...scenario, steps: sheet.at === null ? [...steps, step] : steps.map((held, index) => (index === sheet.at ? step : held)) })}
        />
      ) : null}
      {editing && sheet === "settings" ? (
        <SettingsSheet
          draft={editing.draft}
          generator={vocabulary?.generator_version ?? draft.plan.generator_version}
          maxSeed={vocabulary?.max_seed ?? Number.MAX_SAFE_INTEGER}
          onClose={() => setSheet(null)}
          onApply={(next) => {
            setEditing({ ...editing, draft: next });
            setPreview(null);
          }}
        />
      ) : null}
      {sheet === "history" && ref ? <HistorySheet context={context} item={ref} onClose={() => setSheet(null)} /> : null}
      <Modal
        open={sheet === "details"}
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
        <ValueRows
          rows={[
            { label: "Scenario", value: `${scenario.scenario.id} · v${scenario.scenario.version}` },
            { label: "Profile", value: scenario.profile },
            { label: "Generator", value: draft.plan.generator_version },
            { label: "Seed", value: String(draft.plan.seed) },
            { label: "Base time", value: scenario.base_time },
          ]}
        />
      </Modal>
    </>
  );

  const running = work.running !== null;
  const title = `${name || "New scenario"} · Synthetic`;
  const actions = editing ? (
    <>
      <button type="button" disabled={busy || running} onClick={() => void runPreview()}>
        Preview
      </button>
      <Menu
        label="Add to scenario"
        items={[
          { label: "Add event", disabled: events.length === 0, onSelect: () => setSheet({ step: newStep(steps, scenario, events[0]?.event ?? ""), at: null }) },
          { label: "Generation settings", onSelect: () => setSheet("settings") },
        ]}
      />
      <SaveButtons
        dirty={JSON.stringify(editing.draft) !== JSON.stringify(saved) || editing.name !== (opened?.draft?.name ?? "")}
        disabled={busy || running || editing.name.trim() === "" || steps.length === 0}
        onSave={save}
        onCancel={() => {
          setEditing(null);
          setProblems([]);
          setPreview(null);
          if (!ref) onSaved({ kind: "scenario", id: "" });
        }}
      />
    </>
  ) : (
    <>
      <button type="button" disabled={busy || running} onClick={() => void runPreview()}>
        Preview
      </button>
      <button type="button" disabled={busy || running || !saved} onClick={() => saved && setEditing({ name, draft: saved })}>
        Edit
      </button>
      <button type="button" className="primary" disabled={busy || running || !ref} onClick={() => void createCase()}>
        Create case
      </button>
      <Menu
        label="More scenario actions"
        items={[
          { label: "History", onSelect: () => setSheet("history") },
          {
            label: "Export scenario…",
            onSelect: () => {
              if (ref) void exportItem(context(), ref).then(setNotice);
            },
          },
          { label: "Details", onSelect: () => setSheet("details") },
        ]}
      />
    </>
  );
  return { title, actions, body };
}

function nextStepId(steps: ScenarioStep[]): string {
  let n = steps.length + 1;
  const taken = new Set(steps.map((step) => step.id));
  while (taken.has(`step-${n}`)) n += 1;
  return `step-${n}`;
}

function newStep(steps: ScenarioStep[], scenario: ScenarioScenario, event: string): ScenarioStep {
  return { id: nextStepId(steps), event, subject: scenario.subjects[0]?.id ?? "", after: "10m", expect: "accepted" };
}

/** A new scenario: its name, family, template and optional local profile. */
function NewScenarioSheet({ context, profiles, onClose, onCreate }: { context: () => RequestContext; profiles: CatalogItem[]; onClose: () => void; onCreate: (name: string, draft: Draft) => void }) {
  const vocabulary = useVocabulary()?.scenarios;
  const templates = vocabulary?.templates ?? [];
  const families = [...new Set(templates.map((template) => template.family))];
  const [name, setName] = useState("");
  const [family, setFamily] = useState(families[0] ?? "");
  const [template, setTemplate] = useState(templates.find((entry) => entry.family === family)?.id ?? "");
  const [profile, setProfile] = useState("");
  // The vocabulary can arrive after the sheet opens.
  useEffect(() => {
    if (family === "" && families[0]) {
      setFamily(families[0]);
      setTemplate(templates.find((entry) => entry.family === families[0])?.id ?? "");
    }
  }, [families.join(","), family]); // eslint-disable-line react-hooks/exhaustive-deps
  const offered = profiles.filter((item) => item.summary.profile?.family === family && item.summary.profile.form !== "profile-pack");
  return (
    <FormDialog
      open
      title="New scenario"
      size="small"
      submitLabel="Create"
      submitDisabled={name.trim() === "" || template === ""}
      dirty={name.trim() !== ""}
      onClose={onClose}
      onSubmit={async () => {
        // The backend allocates the seed and base time once, for this draft.
        const answer = await openItemDraft({ context: context(), ref: { kind: "scenario", id: "" } });
        const base = answer.draft?.scenario as Draft | undefined;
        const chosen = templates.find((entry) => entry.id === template);
        if (!base || !chosen) return { reason: answer.reason ?? "The scenario cannot be started." };
        const scenario: ScenarioScenario = {
          ...(scenarioOf(base) as ScenarioScenario),
          profile: chosen.profile,
          subjects: chosen.subjects,
          steps: chosen.steps,
        };
        const picked = offered.find((item) => item.ref.id === profile);
        onCreate(name.trim(), { ...withScenario(base, scenario), ...(picked ? { profile: picked.ref } : {}) });
      }}
    >
      <label htmlFor="scenario-new-name">Name</label>
      <input id="scenario-new-name" type="text" maxLength={200} value={name} onChange={(event) => setName(event.target.value)} />
      <label htmlFor="scenario-family">Family</label>
      <select
        id="scenario-family"
        value={family}
        onChange={(event) => {
          setFamily(event.target.value);
          setTemplate(templates.find((entry) => entry.family === event.target.value)?.id ?? "");
          setProfile("");
        }}
      >
        {families.map((entry) => (
          <option key={entry}>{entry}</option>
        ))}
      </select>
      <label htmlFor="scenario-template">Template</label>
      <select id="scenario-template" value={template} onChange={(event) => setTemplate(event.target.value)}>
        {templates
          .filter((entry) => entry.family === family)
          .map((entry) => (
            <option key={entry.id} value={entry.id}>
              {entry.name}
            </option>
          ))}
      </select>
      {offered.length > 0 ? (
        <>
          <label htmlFor="scenario-profile">Local profile</label>
          <select id="scenario-profile" value={profile} onChange={(event) => setProfile(event.target.value)}>
            <option value="">None</option>
            {offered.map((item) => (
              <option key={item.ref.id} value={item.ref.id}>
                {item.name}
              </option>
            ))}
          </select>
        </>
      ) : null}
    </FormDialog>
  );
}

/** One event: its type from the family's supported events, its subject, when it follows the one before, and what it expects. */
function EventSheet({
  step,
  isNew,
  scenario,
  events,
  onClose,
  onApply,
}: {
  step: ScenarioStep;
  isNew: boolean;
  scenario: ScenarioScenario;
  events: { event: string; description: string; kind: string }[];
  onClose: () => void;
  onApply: (step: ScenarioStep) => void;
}) {
  const [value, setValue] = useState(step);
  const kind = events.find((entry) => entry.event === value.event)?.kind;
  const subjects = scenario.subjects.filter((subject) => !kind || subject.kind === kind);
  return (
    <FormDialog
      open
      title={isNew ? "Add event" : "Edit event"}
      submitLabel={isNew ? "Add" : "Done"}
      submitDisabled={value.event === "" || value.subject === "" || !/^\d+[smhd]$/.test(value.after)}
      dirty={JSON.stringify(value) !== JSON.stringify(step)}
      onClose={onClose}
      onSubmit={() => {
        onApply(value);
        onClose();
      }}
    >
      <label htmlFor="event-type">Event</label>
      <select
        id="event-type"
        value={value.event}
        onChange={(event) => {
          const next = events.find((entry) => entry.event === event.target.value);
          const subject = scenario.subjects.find((entry) => entry.kind === next?.kind)?.id ?? value.subject;
          setValue({ ...value, event: event.target.value, subject });
        }}
      >
        {events.map((entry) => (
          <option key={entry.event} value={entry.event}>
            {entry.description}
          </option>
        ))}
      </select>
      <label htmlFor="event-subject">Subject</label>
      <select id="event-subject" value={value.subject} onChange={(event) => setValue({ ...value, subject: event.target.value })}>
        {subjects.map((subject) => (
          <option key={subject.id} value={subject.id}>
            {`${subject.kind.charAt(0).toUpperCase()}${subject.kind.slice(1)} ${subject.identifier}`}
          </option>
        ))}
      </select>
      <label htmlFor="event-after">After the previous event</label>
      <input id="event-after" type="text" value={value.after} aria-invalid={!/^\d+[smhd]$/.test(value.after)} onChange={(event) => setValue({ ...value, after: event.target.value.trim() })} />
      <label htmlFor="event-expect">Expected</label>
      <select id="event-expect" value={value.expect} onChange={(event) => setValue({ ...value, expect: event.target.value as ScenarioExpectation })}>
        {(Object.keys(EXPECTATIONS) as ScenarioExpectation[]).map((expect) => (
          <option key={expect} value={expect}>
            {EXPECTATIONS[expect]}
          </option>
        ))}
      </select>
    </FormDialog>
  );
}

/** The seed, base time and pinned versions a scenario generates under. */
function SettingsSheet({ draft, generator, maxSeed, onClose, onApply }: { draft: Draft; generator: string; maxSeed: number; onClose: () => void; onApply: (draft: Draft) => void }) {
  const scenario = scenarioOf(draft)!;
  const templateName = useVocabulary()?.scenarios.templates.find((template) => template.id === scenario.scenario.id)?.name ?? "Scenario template";
  const [seed, setSeed] = useState(String(draft.plan.seed));
  const [base, setBase] = useState(scenario.base_time);
  const valid = /^(0|[1-9]\d*)$/.test(seed) && Number(seed) <= maxSeed && base.trim() !== "";
  return (
    <FormDialog
      open
      title="Generation settings"
      size="small"
      submitLabel="Done"
      submitDisabled={!valid}
      dirty={seed !== String(draft.plan.seed) || base !== scenario.base_time}
      onClose={onClose}
      onSubmit={() => {
        onApply(withScenario({ ...draft, plan: { ...draft.plan, seed: Number(seed) } }, { ...scenario, base_time: base.trim() }));
        onClose();
      }}
    >
      <label htmlFor="scenario-seed">Seed</label>
      <input id="scenario-seed" type="text" inputMode="numeric" value={seed} aria-invalid={!valid} onChange={(event) => setSeed(event.target.value.trim())} />
      <label htmlFor="scenario-base">Base time</label>
      <input id="scenario-base" type="text" value={base} onChange={(event) => setBase(event.target.value)} />
      <ValueRows
        rows={[
          { label: "Generator", value: `v${generator}` },
          { label: "Template", value: `${templateName} · v${scenario.scenario.version}` },
        ]}
      />
    </FormDialog>
  );
}

/** The generated messages, each opened in the shared reader, all synthetic. */
function PreviewBody({ preview, busy, onClose }: { preview: ScenarioPlanPreviewResult; busy: boolean; onClose: () => void }) {
  const [selected, setSelected] = useState<number | null>(null);
  const [inspection, setInspection] = useState<InspectionResult | null>(null);
  const [revealed, setRevealed] = useState(false);
  // Only the answer to the newest request is shown.
  const asked = useRef(0);
  const inspect = async (message: number, path: string, nodeOffset: number, byteOffset: number, reveal: boolean) => {
    if (!preview.preview_id) return null;
    const request = ++asked.current;
    const answer = await inspectScenarioPreview({ preview_id: preview.preview_id, message, path, node_offset: nodeOffset, byte_offset: byteOffset, reveal });
    if (request === asked.current) setInspection(answer);
    return answer;
  };
  if (preview.state !== "completed") {
    return (
      <EmptyState
        title={preview.problems.map((problem) => problem.problem).join(" ") || preview.reason || "The scenario was not generated."}
        action={
          <button type="button" onClick={onClose}>
            Back to events
          </button>
        }
      />
    );
  }
  return (
    <>
      <div className="toolbar page-toolbar">
        <span className="badge">Synthetic</span>
        <button type="button" className="quiet" onClick={onClose}>
          Back to events
        </button>
      </div>
      <DataTable
        label="Generated messages"
        className="page-table"
        rows={preview.messages}
        rowId={(message) => String(message.index)}
        rowLabel={(message) => typeLabel({ kind: "message", code: message.message_code, trigger: message.trigger_event })}
        selected={selected === null ? null : String(selected)}
        onSelect={(id) => {
          setSelected(Number(id));
          setInspection(null);
          void inspect(Number(id), "", 0, -1, revealed);
        }}
        onOpen={(id) => {
          setSelected(Number(id));
          void inspect(Number(id), "", 0, -1, revealed);
        }}
        columns={[
          { key: "time", header: "Time", priority: 1, minWidth: 7, render: (message) => message.at.replace(/^\d{8}/, "").replace(/(\d{2})(\d{2})(\d{2}).*/, "$1:$2:$3") },
          { key: "type", header: "Type", priority: 1, minWidth: 7, render: (message) => typeLabel({ kind: "message", code: message.message_code, trigger: message.trigger_event }) },
          { key: "row", header: "Patient", priority: 3, minWidth: 8, render: (message) => message.row },
          { key: "expect", header: "Expected", priority: 2, minWidth: 7, render: (message) => (message.expect ? EXPECTATIONS[message.expect as ScenarioExpectation] ?? message.expect : "—") },
        ]}
      />
      {selected !== null ? (
        <MessageReader
          result={inspection}
          loading={inspection === null}
          busy={busy}
          onInspect={(path, nodeOffset, byteOffset) => inspect(selected, path, nodeOffset, byteOffset, revealed)}
          onReveal={(reveal) => {
            setRevealed(reveal);
            void inspect(selected, inspection?.inspection?.selected.path ?? "", 0, -1, reveal);
          }}
          onClose={() => {
            setSelected(null);
            setInspection(null);
          }}
        />
      ) : null}
    </>
  );
}
