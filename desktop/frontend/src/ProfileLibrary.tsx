// Library › Profiles: a local interface profile's saved detail — its
// segments and fields as an outline, the selected field beside it — and the
// whole-profile editor one Save publishes as a new version. Editing one field
// keeps every other clause, override, pin and attribution as it was.
import { useCallback, useEffect, useRef, useState } from "react";
import {
  applyLibraryDocument,
  chooseLibraryFile,
  importLibraryItem,
  evaluateProfile,
  listWholeCatalog,
  compareProfileVersions,
  libraryDocument,
  metadataPacks,
  newIntentId,
  openItemDraft,
  profileAffectedTests,
  resolveProfileDraft,
  saveItem,
  upgradeProfilePins,
  type AffectedTest,
  type CatalogItem,
  type ProfileEvaluationResult,
  type Field,
  type ItemDraft,
  type ItemDraftResult,
  type ItemRef,
  type LocalProfile,
  type LocalProfileAuthority,
  type LocalProfileCode,
  type LocalProfileConditionOperator,
  type LocalProfileOrigin,
  type LocalProfilePrecision,
  type LocalProfileResolvedField,
  type LocalProfileTimeZoneRule,
  type LocalProfileUsage,
  type MetadataPack,
  type ProfileDraft,
  type ProfilePackOutcome,
  type ProfileResolutionResult,
  type ProfileVersionComparison,
  type RequestContext,
  type Segment,
} from "./bindings";
import { DataTable } from "./DataTable";
import { saveProblem } from "./Environments";
import { EmptyState, FormDialog, Menu, Modal, ValueRows, type SubmitFailure } from "./layout";
import { HistorySheet, SaveButtons, exportItem, type LibraryPage } from "./Library";
import { messageName } from "./Checks";
import { useLifecycle } from "./lifecycle";
import { useVocabulary } from "./vocabulary";
import { FHIRProfileFields, FHIRProfileOrigin } from "./FHIRProfileFields";

const PRESENCE: Record<LocalProfileUsage, string> = { R: "Required", RE: "Required when known", O: "Optional", C: "Conditional", X: "Not used" };
const ORIGINS: Record<LocalProfileOrigin, string> = { profile: "Base", overridden: "Override", local: "Local", undeclared: "Not declared" };
const OUTCOMES: Record<ProfilePackOutcome, string> = { supported: "Supported", untested: "Untested", unsupported: "Unsupported", unknown: "Unknown" };
const CONDITIONS: Record<LocalProfileConditionOperator, string> = { present: "is present", absent: "is absent", value_in: "is one of" };
const PRECISIONS: Record<LocalProfilePrecision, string> = { year: "Year", month: "Month", day: "Day", hour: "Hour", minute: "Minute", second: "Second", fraction: "Fraction of a second" };
const ZONE_RULES: Record<LocalProfileTimeZoneRule, string> = { required: "Zone required", optional: "Zone optional", forbidden: "No zone" };

const pathOf = (segment: string, position: number) => `${segment}-${position}`;

function repetitions(field: { cardinality?: { min: number; max: string } }, unbounded: string): string {
  if (!field.cardinality) return "—";
  const max = field.cardinality.max === unbounded ? "many" : field.cardinality.max;
  return `${field.cardinality.min}..${max}`;
}

/** The next version a published profile is saved as. */
function nextVersion(version: string): string {
  return /^\d+$/.test(version) ? String(Number(version) + 1) : version;
}

/** One field's recorded values, with where each came from. */
function FieldDetail({ profile, segment, field, resolved, unbounded }: { profile: LocalProfile; segment: Segment; field: Field; resolved: LocalProfileResolvedField | undefined; unbounded: string }) {
  const codes = profile.terminology?.find((set) => set.id === field.terminology);
  const authority = profile.authorities?.find((entry) => entry.id === field.authority);
  const date = profile.dates?.find((entry) => entry.id === field.date);
  const condition = field.condition;
  return (
    <ValueRows
      rows={[
        { label: "Label", value: field.name || resolved?.pack_name || "—" },
        { label: "Path", value: pathOf(segment.id, field.position) },
        {
          label: "Presence",
          value: `${PRESENCE[field.usage]}${condition ? ` when ${pathOf(condition.segment, condition.position)} ${CONDITIONS[condition.operator]}${condition.values?.length ? ` ${condition.values.join(", ")}` : ""}` : ""}`,
        },
        { label: "Type", value: field.type || resolved?.type || "—" },
        { label: "Repetitions", value: repetitions(field, unbounded) },
        ...(codes ? [{ label: "Codes", value: `${codes.codes.map((code) => code.code).join(", ")}${codes.binding === "suggested" ? " (suggested)" : ""}` }] : []),
        ...(authority ? [{ label: "Authority", value: [authority.namespace, authority.universal_id, authority.universal_id_type].filter(Boolean).join(" · ") || authority.id }] : []),
        ...(date ? [{ label: "Date handling", value: `${PRECISIONS[date.precision]} · ${ZONE_RULES[date.timezone]}` }] : []),
        ...(resolved ? [{ label: "Origin", value: ORIGINS[resolved.usage_origin] }] : []),
      ]}
    />
  );
}

type Selected = { segment: string; position: number } | null;

/** A profile: its saved detail, or the editor it was opened into. */
export function useProfile({
  context,
  ref,
  imported,
  shown,
  busy,
  onSaved,
  onImport,
  onImported,
  initialField,
  evaluationCase,
}: {
  /** Imports another profile, as the list's Import does. */
  onImport: () => void;
  onImported: (draft: ItemDraftResult) => void;
  context: () => RequestContext;
  /** The profile, or null for one being imported. */
  ref: ItemRef | null;
  /** An imported, unsaved draft to edit before its first Save. */
  imported: ItemDraftResult | null;
  shown: boolean;
  busy: boolean;
  onSaved: (ref: ItemRef) => void;
  initialField?: {segment:string;position:number};
  evaluationCase?: ItemRef;
}): LibraryPage {
  const vocabulary = useVocabulary()?.profiles;
  const unbounded = vocabulary?.unbounded ?? "*";
  const [opened, setOpened] = useState<ItemDraftResult | null>(null);
  const [editing, setEditing] = useState<{ name: string; draft: ProfileDraft } | null>(null);
  const [selected, setSelected] = useState<Selected>(null);
  const [resolution, setResolution] = useState<ProfileResolutionResult | null>(null);
  const [sheet, setSheet] = useState<null | "setup" | "field" | "segment" | "history" | "affected" | "packs" | "json" | "details" | "evaluate">(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [problems, setProblems] = useState<string[]>([]);
  const reads = useLifecycle<"reading">({ background: true });
  const pending = useRef(false);
  const packIntent = useRef<{ content: string; intent: string } | null>(null);
  const id = ref?.id ?? "";
  // Metadata packs by identity and version, for naming a profile's base.
  const [packNames, setPackNames] = useState<Record<string, string>>({});
  useEffect(() => {
    if (!shown) return;
    void metadataPacks(context()).then((answer) =>
      setPackNames(Object.fromEntries(answer.packs.filter((pack) => pack.pack).map((pack) => [`${pack.pack!.id}@${pack.pack!.version}`, pack.item.name]))),
    );
  }, [shown]); // eslint-disable-line react-hooks/exhaustive-deps
  // Only the newest resolution is shown, whatever order answers arrive in.
  const resolving = useRef(0);

  const read = useCallback(async () => {
    if (!ref) return;
    await reads.run("reading", async (current) => {
      const answer = await openItemDraft({ context: context(), ref });
      if (current()) setOpened(answer);
    });
  }, [id, ref?.revision, context]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    setOpened(null);
    setEditing(null);
    setSelected(null);
    setNotice(null);
    setProblems([]);
    if (!shown) return;
    if (imported?.draft?.profile) {
      setOpened(imported);
      setEditing({ name: imported.draft.name ?? "", draft: imported.draft.profile });
    } else void read();
  }, [id, ref?.revision, shown, imported]); // eslint-disable-line react-hooks/exhaustive-deps

  const saved = opened?.draft?.profile;
  const draft = editing?.draft ?? saved;
  const profile = draft?.profile;
  const name = editing?.name ?? opened?.draft?.name ?? "";
  useEffect(()=>{if(initialField&&profile)setSelected(initialField);},[id,opened?.ref?.revision,initialField?.segment,initialField?.position,Boolean(profile)]);

  // What the base pack declares for this draft: origins and support.
  const content = JSON.stringify(draft ?? null);
  useEffect(() => {
    const asked = ++resolving.current;
    if (!draft || draft.metadata_pack) return;
    setResolution(null);
    const timer = window.setTimeout(() => {
      void resolveProfileDraft({ context: context(), kind: "profile", ...(ref ? { item: ref.id } : {}), draft: { name, profile: draft } }).then((answer) => {
        if (asked === resolving.current) setResolution(answer);
      });
    }, 250);
    return () => window.clearTimeout(timer);
  }, [content]); // eslint-disable-line react-hooks/exhaustive-deps

  if (draft?.metadata_pack && opened) {
    const held = draft.metadata_pack.metadata;
    const publish = async (): Promise<SubmitFailure | void> => {
      if (!editing || pending.current) return;
      pending.current = true;
      try {
        const itemDraft: ItemDraft = { name: editing.name.trim(), profile: editing.draft };
        const content = JSON.stringify(itemDraft);
        if (packIntent.current?.content !== content) packIntent.current = { content, intent: newIntentId() };
        const answer = await saveItem({ context: context(), kind: "profile", ...(ref ? { item: ref.id, ...(opened.ref?.revision ? { base_revision: opened.ref.revision } : {}) } : {}), draft: itemDraft, intent_id: packIntent.current.intent });
        if (answer.outcome !== "saved" || !answer.saved) {
          setProblems(answer.problems.map((problem) => problem.problem));
          return saveProblem(answer, { name: "profile-name" });
        }
        setEditing(null); setProblems([]); onSaved(answer.saved);
        if (ref) await read();
      } finally { pending.current = false; }
    };
    return { title: `${name || "Metadata pack"} · v${held.pack.version}`, actions: editing ? <SaveButtons dirty={editing.name !== (opened.draft?.name ?? "")} disabled={busy || editing.name.trim() === ""} onSave={publish} onCancel={() => { setEditing(null); setProblems([]); if (!ref) onSaved({ kind: "profile", id: "" }); }} /> : <Menu label="More profile actions" items={[{ label: "Metadata packs", onSelect: () => setSheet("packs") }]} />, body: <>
      <h2>Metadata pack</h2>
      {editing ? <div className="editor-fields"><label htmlFor="profile-name">Name</label><input id="profile-name" type="text" maxLength={200} value={editing.name} onChange={(event) => setEditing({ ...editing, name: event.target.value })} /></div> : null}
      <ValueRows rows={[{ label: "Pack", value: held.pack.id }, { label: "Version", value: held.pack.version }, { label: "Source", value: held.provenance.source.name }, { label: "Source revision", value: held.provenance.source.revision }, { label: "License", value: held.provenance.license.spdx }, { label: "Rights review", value: held.provenance.rights_review.status }, { label: "Review reference", value: held.provenance.rights_review.reference }]} />
      <DataTable label="Declared support" className="values-table" rows={held.coverage} rowId={(row) => `${row.hl7_version}-${row.family}`} rowLabel={(row) => `${row.family} ${row.hl7_version}`} selected={null} onSelect={() => {}} onOpen={() => {}} columns={[
        { key: "family", header: "Family", priority: 1, minWidth: 8, render: (row) => `${row.family} ${row.hl7_version}` }, { key: "parse", header: "Parse", priority: 1, minWidth: 6, render: (row) => OUTCOMES[row.parse] }, { key: "labels", header: "Labels", priority: 2, minWidth: 6, render: (row) => OUTCOMES[row.labels] }, { key: "structure", header: "Structure", priority: 2, minWidth: 6, render: (row) => OUTCOMES[row.structural] }, { key: "workflow", header: "Workflow", priority: 3, minWidth: 6, render: (row) => OUTCOMES[row.workflow] },
      ]} />
      <p className="row-reason">Metadata is read-only. Import and Save retain the complete source document; its support levels stay scoped to the declared families and editions.</p>
      {problems.length > 0 ? <ul role="alert">{problems.map((problem, at) => <li key={at}>{problem}</li>)}</ul> : null}
      {sheet === "packs" ? <PacksSheet context={context} onClose={() => setSheet(null)} onImported={onImported} /> : null}
    </> };
  }

  if (draft?.fhir && opened) {
    const definition = draft.fhir;
    const saveFHIR = async (): Promise<SubmitFailure | void> => {
      if (!editing || pending.current) return;
      pending.current = true;
      try {
        const answer = await saveItem({ context: context(), kind: "profile", ...(ref ? { item: ref.id, ...(opened.ref?.revision ? { base_revision: opened.ref.revision } : {}) } : {}), draft: { name: editing.name.trim(), profile: editing.draft }, intent_id: newIntentId() });
        if (answer.outcome !== "saved" || !answer.saved) {
          setProblems(answer.problems.map((problem) => problem.problem));
          return saveProblem(answer, { name: "profile-name" });
        }
        setEditing(null); setProblems([]); onSaved(answer.saved);
        if (ref) await read();
      } finally { pending.current = false; }
    };
    return { title: `${name || "New profile"} · v${definition.identity.version}`, actions: editing ? <SaveButtons dirty={JSON.stringify(editing.draft) !== JSON.stringify(saved) || editing.name !== (opened.draft?.name ?? "")} disabled={busy || editing.name.trim() === ""} onSave={saveFHIR} onCancel={() => { setEditing(null); setProblems([]); if (!ref) onSaved({ kind: "profile", id: "" }); }} /> : <>
      <button type="button" className="primary" disabled={busy || !saved} onClick={() => saved && setEditing({ name, draft: { ...saved, fhir: { ...definition, identity: { ...definition.identity, version: nextVersion(definition.identity.version) } } } })}>Edit</button>
      <Menu label="More profile actions" items={[{ label: "History", onSelect: () => setSheet("history") }, { label: "Export profile…", onSelect: () => { if (ref) void exportItem(context(), ref).then(setNotice); } }, { label: "Import profile…", onSelect: onImport }]} />
    </>, body: <>
      {notice ? <p role="status">{notice}</p> : null}
      {problems.length > 0 ? <ul role="alert">{problems.map((problem, at) => <li key={at}>{problem}</li>)}</ul> : null}
      <ValueRows rows={[
        { label: "Protocol", value: "FHIR R4 · 4.0.1" }, { label: "Resource type", value: definition.resource_type },
        { label: "Validator", value: definition.validator ? `Saved connection · v${definition.validator.revision ?? "—"}` : "Unavailable" },
        { label: "Capability pin", value: definition.capability || "Unavailable" },
        { label: "Availability", value: resolution?.fhir?.state ?? "Unavailable" },
        { label: "Scope", value: "Saved local metadata. Validation and workflow correctness are separate." },
        ...(draft.origin ? [{ label: "Source", value: `${draft.origin.source} · ${draft.origin.license}` }] : []),
      ]} />
      {resolution?.fhir?.reason ? <p role="status">{resolution.fhir.reason}</p> : null}
      {resolution?.fhir?.packages.length ? <ValueRows label="Installed package metadata" rows={resolution.fhir.packages.map((p, at) => ({ label: `Package ${at + 1}`, value: `${p.id} · ${p.version} · ${p.license}` }))} /> : null}
      {editing ? <div className="editor-fields"><label htmlFor="profile-name">Name</label><input id="profile-name" type="text" maxLength={200} value={editing.name} onChange={(event) => setEditing({ ...editing, name: event.target.value })} /><FHIRProfileFields value={definition} context={context} availability={resolution?.fhir} onChange={(fhir) => setEditing({ ...editing, draft: { ...editing.draft, fhir } })} /><FHIRProfileOrigin value={editing.draft.origin} onChange={(origin) => setEditing({ ...editing, draft: { ...editing.draft, origin } })} /></div> : <>
        <ValueRows label="Requirements" rows={[{ label: "Terminology", value: definition.requirements.terminology }, { label: "Invariants", value: definition.requirements.invariants }, { label: "Failure severities", value: definition.requirements.fail_severities.join(", ") }]} />
        <ValueRows label="Package pins" rows={definition.packages.map((p, at) => ({ label: `Package ${at + 1}`, value: `${p.id} · ${p.version}` }))} />
        <ValueRows label="Canonical pins" rows={definition.profiles.map((p, at) => ({ label: `Canonical ${at + 1}`, value: `${p.url} · ${p.version} · ${p.sha256}` }))} />
      </>}
      {sheet === "history" && ref ? <HistorySheet context={context} item={ref} onClose={() => setSheet(null)} /> : null}
    </> };
  }

  if (!opened) return { title: "Profile", actions: null, body: <p className="row-reason">Loading</p> };
  if (!profile) {
    return {
      title: "Profile",
      actions: null,
      body: (
        <EmptyState
          title={opened.reason ?? "This profile cannot be read."}
          action={
            <button type="button" onClick={() => void read()}>
              Retry
            </button>
          }
        />
      ),
    };
  }

  const edit = (next: LocalProfile) => editing && setEditing({ ...editing, draft: { ...editing.draft, profile: next } });
  const segment = profile.segments.find((entry) => entry.id === selected?.segment);
  const field = segment?.fields.find((entry) => entry.position === selected?.position);
  const resolvedField = resolution?.resolution?.segments.find((entry) => entry.id === segment?.id)?.fields.find((entry) => entry.position === field?.position);

  const save = async (): Promise<SubmitFailure | void> => {
    if (!editing || pending.current) return;
    pending.current = true;
    try {
      const itemDraft: ItemDraft = { name: editing.name.trim(), profile: editing.draft };
      const answer = await saveItem({
        context: context(),
        kind: "profile",
        ...(ref ? { item: ref.id, ...(opened.ref?.revision ? { base_revision: opened.ref.revision } : {}) } : {}),
        draft: itemDraft,
        intent_id: newIntentId(),
      });
      if (answer.outcome !== "saved" || !answer.saved) {
        setProblems(answer.problems.map((problem) => problem.problem));
        return saveProblem(answer, { name: "profile-name" });
      }
      setEditing(null);
      setProblems([]);
      onSaved(answer.saved);
      if (ref) await read();
    } finally {
      pending.current = false;
    }
  };

  const outline = (
    <details className="profile-outline" open>
      <summary>Segments and fields</summary>
      <ul aria-label="Segments and fields">
        {profile.segments.map((entry) => (
          <li key={entry.id}>
            <span className="outline-segment">{entry.id}</span>
            <ul>
              {entry.fields.map((item) => {
                const current = selected?.segment === entry.id && selected.position === item.position;
                return (
                  <li key={item.position}>
                    <button type="button" className="outline-field" aria-current={current ? "true" : undefined} onClick={() => setSelected({ segment: entry.id, position: item.position })}>
                      {pathOf(entry.id, item.position)}
                      {item.name ? <span className="row-reason"> {item.name}</span> : null}
                    </button>
                  </li>
                );
              })}
            </ul>
          </li>
        ))}
      </ul>
    </details>
  );

  const detail =
    segment && field ? (
      <section className="profile-field" aria-label={pathOf(segment.id, field.position)}>
        <header className="section-header">
          <h2>{field.name || pathOf(segment.id, field.position)}</h2>
          {editing ? (
            <span className="row-actions">
              <button type="button" onClick={() => setSheet("field")}>
                Edit field
              </button>
              <button
                type="button"
                className="quiet"
                onClick={() => {
                  edit({ ...profile, segments: profile.segments.map((entry) => (entry.id === segment.id ? { ...entry, fields: entry.fields.filter((item) => item.position !== field.position) } : entry)) });
                  setSelected(null);
                }}
              >
                Remove field
              </button>
            </span>
          ) : null}
        </header>
        <FieldDetail profile={profile} segment={segment} field={field} resolved={resolvedField} unbounded={unbounded} />
        {resolution?.support && resolution.support.structural !== "supported" ? (
          <ValueRows rows={[{ label: "Support", value: `${OUTCOMES[resolution.support.structural]} by the base` }]} />
        ) : null}
        {(resolution?.resolution?.findings ?? [])
          .filter((finding) => finding.subject === pathOf(segment.id, field.position))
          .map((finding) => (
            <p key={finding.kind + finding.detail} className="row-reason" role="note">
              {finding.detail}
            </p>
          ))}
      </section>
    ) : segment ? null : (
      <ValueRows
        rows={[
          { label: "Profile", value: `${name} · v${profile.profile.version}` },
          { label: "HL7 version", value: profile.base.hl7_version },
          { label: "Family", value: profile.base.family },
          { label: "Base", value: `${packNames[`${profile.base.pack.id}@${profile.base.pack.version}`] ?? "Metadata pack"} · v${profile.base.pack.version}` },
          ...(resolution?.support
            ? [{ label: "Support", value: `Parse ${OUTCOMES[resolution.support.parse]} · Labels ${OUTCOMES[resolution.support.labels]} · Structure ${OUTCOMES[resolution.support.structural]} · Workflow ${OUTCOMES[resolution.support.workflow]}` }]
            : []),
          ...(draft?.origin ? [{ label: "Source", value: `${draft.origin.source} · ${draft.origin.license}` }] : []),
        ]}
      />
    );

  const body = (
    <>
      {notice ? (
        <div className="notice" role="status">
          {notice}
        </div>
      ) : null}
      {problems.length > 0 ? (
        <ul className="notice danger" role="alert">
          {problems.map((problem) => (
            <li key={problem}>{problem}</li>
          ))}
        </ul>
      ) : null}
      {resolution?.problems && resolution.problems.length > 0 && editing ? (
        <ul className="notice danger" role="alert">
          {resolution.problems.map((problem) => (
            <li key={`${problem.field}-${problem.problem}`}>{problem.problem}</li>
          ))}
        </ul>
      ) : null}
      <div className="profile-layout">
        {outline}
        <div className="profile-detail">{detail}</div>
      </div>
      {editing && sheet === "setup" ? (
        <SetupSheet
          name={editing.name}
          draft={editing.draft}
          context={context}
          onClose={() => setSheet(null)}
          onApply={(nextName, next) => setEditing({ name: nextName, draft: next })}
        />
      ) : null}
      {editing && sheet === "segment" ? (
        <SegmentSheet
          taken={profile.segments.map((entry) => entry.id)}
          onClose={() => setSheet(null)}
          onAdd={(id) => {
            edit({ ...profile, segments: [...profile.segments, { id, fields: [] }] });
            setSelected({ segment: id, position: 0 });
          }}
        />
      ) : null}
      {editing && sheet === "field" && segment ? (
        <FieldSheet
          profile={profile}
          segment={segment}
          field={field ?? null}
          unbounded={unbounded}
          onClose={() => setSheet(null)}
          onApply={(next, position) => {
            edit(next);
            setSelected({ segment: segment.id, position });
          }}
        />
      ) : null}
      {sheet === "history" && ref ? (
        <HistorySheet context={context} item={ref} onClose={() => setSheet(null)} extra={(revision) => (revision.current || !revision.version ? null : <CompareButton context={context} profile={ref} from={revision.version} />)} />
      ) : null}
      {sheet === "affected" && ref && opened.ref ? <AffectedTestsSheet context={context} profile={{ ...ref, ...(opened.ref.revision ? { revision: opened.ref.revision } : {}) }} onClose={() => setSheet(null)} /> : null}
      {sheet === "packs" ? <PacksSheet context={context} onClose={() => setSheet(null)} onImported={onImported} /> : null}
      {sheet === "evaluate" && ref && opened.ref ? (
        <EvaluateSheet context={context} initialCase={evaluationCase} profile={{ ...ref, ...(opened.ref.revision ? { revision: opened.ref.revision } : {}) }} onClose={() => setSheet(null)} />
      ) : null}
      {sheet === "json" && draft ? (
        <JsonSheet
          context={context}
          draft={{ name, profile: draft }}
          onClose={() => setSheet(null)}
          onApply={(next) => {
            if (next.profile) setEditing({ name: next.name ?? name, draft: next.profile });
          }}
        />
      ) : null}
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
            { label: "Identity", value: profile.profile.id },
            { label: "Version", value: profile.profile.version },
            { label: "Catalog", value: ref?.id ?? "—" },
            ...(resolution?.seal ? [{ label: "SHA-256", value: <span className="identity">{resolution.seal.content.sha256}</span> }] : []),
          ]}
        />
      </Modal>
    </>
  );

  const title = `${name || "New profile"} · v${profile.profile.version}`;
  const actions = editing ? (
    <>
      <Menu
        label="Add to profile"
        items={[
          { label: "Setup", onSelect: () => setSheet("setup") },
          { label: "Add segment", onSelect: () => setSheet("segment") },
          {
            label: "Add field",
            disabled: !segment,
            onSelect: () => {
              setSelected(segment ? { segment: segment.id, position: 0 } : null);
              setSheet("field");
            },
          },
          { label: "Edit JSON", onSelect: () => setSheet("json") },
        ]}
      />
      <SaveButtons
        dirty={JSON.stringify(editing.draft) !== JSON.stringify(saved) || editing.name !== (opened.draft?.name ?? "")}
        disabled={busy || editing.name.trim() === ""}
        onSave={save}
        onCancel={() => {
          setEditing(null);
          setProblems([]);
          if (!ref) onSaved({ kind: "profile", id: "" });
        }}
      />
    </>
  ) : (
    <>
      <button
        type="button"
        className="primary"
        disabled={busy || !saved}
        onClick={() => saved && setEditing({ name, draft: { ...saved, profile: { ...saved.profile, profile: { ...saved.profile.profile, version: nextVersion(saved.profile.profile.version) } } } })}
      >
        Edit
      </button>
      <Menu
        label="More profile actions"
        items={[
          { label: "History", onSelect: () => setSheet("history") },
          { label: "Affected tests", onSelect: () => setSheet("affected") },
          { label: "Evaluate a case…", onSelect: () => setSheet("evaluate") },
          {
            label: "Export profile…",
            onSelect: () => {
              if (ref) void exportItem(context(), ref).then(setNotice);
            },
          },
          { label: "Metadata packs", onSelect: () => setSheet("packs") },
          { label: "Import profile…", onSelect: onImport },
          {
            label: "Edit JSON",
            onSelect: () => {
              if (!saved) return;
              setEditing({ name, draft: { ...saved, profile: { ...saved.profile, profile: { ...saved.profile.profile, version: nextVersion(saved.profile.profile.version) } } } });
              setSheet("json");
            },
          },
          { label: "Details", onSelect: () => setSheet("details") },
        ]}
      />
    </>
  );
  return { title, actions, body };
}

/** Name, HL7 version, family and the base pack by its exact version. */
function SetupSheet({ name, draft, context, onClose, onApply }: { name: string; draft: ProfileDraft; context: () => RequestContext; onClose: () => void; onApply: (name: string, draft: ProfileDraft) => void }) {
  const vocabulary = useVocabulary()?.profiles;
  const [label, setLabel] = useState(name);
  const [base, setBase] = useState(draft.profile.base);
  const [packs, setPacks] = useState<MetadataPack[]>([]);
  useEffect(() => {
    void metadataPacks(context()).then((answer) => setPacks(answer.packs.filter((pack) => pack.pack)));
  }, []); // eslint-disable-line react-hooks/exhaustive-deps
  const packKey = (id: string, version: string) => `${id}@${version}`;
  return (
    <FormDialog
      open
      title="Setup"
      submitLabel="Done"
      submitDisabled={label.trim() === ""}
      dirty={label !== name || JSON.stringify(base) !== JSON.stringify(draft.profile.base)}
      onClose={onClose}
      onSubmit={() => {
        const pack = packs.find((entry) => entry.pack && packKey(entry.pack.id, entry.pack.version) === packKey(base.pack.id, base.pack.version));
        onApply(label, { ...draft, profile: { ...draft.profile, base }, ...(pack ? { pack: pack.item.ref } : {}) });
        onClose();
      }}
    >
      <label htmlFor="profile-name">Name</label>
      <input id="profile-name" type="text" maxLength={200} value={label} onChange={(event) => setLabel(event.target.value)} />
      <label htmlFor="profile-hl7">HL7 version</label>
      <select id="profile-hl7" value={base.hl7_version} onChange={(event) => setBase({ ...base, hl7_version: event.target.value })}>
        {(vocabulary?.hl7_versions ?? [base.hl7_version]).map((version) => (
          <option key={version}>{version}</option>
        ))}
      </select>
      <label htmlFor="profile-family">Family</label>
      <select id="profile-family" value={base.family} onChange={(event) => setBase({ ...base, family: event.target.value })}>
        {(vocabulary?.families ?? [base.family]).map((family) => (
          <option key={family}>{family}</option>
        ))}
      </select>
      <label htmlFor="profile-base">Base</label>
      <select
        id="profile-base"
        value={packKey(base.pack.id, base.pack.version)}
        onChange={(event) => {
          const pack = packs.find((entry) => entry.pack && packKey(entry.pack.id, entry.pack.version) === event.target.value)?.pack;
          if (pack) setBase({ ...base, pack });
        }}
      >
        {packs.some((entry) => entry.pack && packKey(entry.pack.id, entry.pack.version) === packKey(base.pack.id, base.pack.version)) ? null : (
          <option value={packKey(base.pack.id, base.pack.version)}>
            {base.pack.id} · v{base.pack.version}
          </option>
        )}
        {packs.map((entry) =>
          entry.pack ? (
            <option key={packKey(entry.pack.id, entry.pack.version)} value={packKey(entry.pack.id, entry.pack.version)}>
              {entry.item.name} · v{entry.pack.version}
            </option>
          ) : null,
        )}
      </select>
    </FormDialog>
  );
}

function SegmentSheet({ taken, onClose, onAdd }: { taken: string[]; onClose: () => void; onAdd: (id: string) => void }) {
  const [id, setId] = useState("");
  const valid = /^[A-Z][A-Z0-9]{2}$/.test(id) && !taken.includes(id);
  return (
    <FormDialog open title="Add segment" size="small" submitLabel="Add" submitDisabled={!valid} dirty={id !== ""} onClose={onClose} onSubmit={() => (onAdd(id), onClose())}>
      <label htmlFor="segment-id">Segment</label>
      <input id="segment-id" type="text" maxLength={3} value={id} aria-invalid={id !== "" && !valid} onChange={(event) => setId(event.target.value.toUpperCase())} />
    </FormDialog>
  );
}

/** Every value one field declares, and the shared entries it names. */
function FieldSheet({
  profile,
  segment,
  field,
  unbounded,
  onClose,
  onApply,
}: {
  profile: LocalProfile;
  segment: Segment;
  /** The field edited, or null for a new one. */
  field: Field | null;
  unbounded: string;
  onClose: () => void;
  onApply: (profile: LocalProfile, position: number) => void;
}) {
  const vocabulary = useVocabulary()?.profiles;
  const codesOf = profile.terminology?.find((set) => set.id === field?.terminology);
  const authorityOf = profile.authorities?.find((entry) => entry.id === field?.authority);
  const dateOf = profile.dates?.find((entry) => entry.id === field?.date);
  const [value, setValue] = useState<Field>(field ?? { position: Math.max(0, ...segment.fields.map((entry) => entry.position)) + 1, usage: "O" });
  const [min, setMin] = useState(field?.cardinality?.min ?? 0);
  const [max, setMax] = useState(field?.cardinality ? (field.cardinality.max === unbounded ? "" : field.cardinality.max) : "1");
  const [many, setMany] = useState(field?.cardinality?.max === unbounded);
  const [repeatTouched, setRepeatTouched] = useState(false);
  const [codes, setCodes] = useState<LocalProfileCode[]>(codesOf?.codes ?? []);
  const [binding, setBinding] = useState(codesOf?.binding ?? "required");
  const [authority, setAuthority] = useState<LocalProfileAuthority>(authorityOf ?? { id: "" });
  const [precision, setPrecision] = useState<LocalProfilePrecision | "">(dateOf?.precision ?? "");
  const [zone, setZone] = useState<LocalProfileTimeZoneRule>(dateOf?.timezone ?? "optional");
  const taken = field === null && segment.fields.some((entry) => entry.position === value.position);
  const path = pathOf(segment.id, value.position);
  // How many fields of the profile name an entry: one shared with another
  // field is never changed in place.
  const users = (key: "terminology" | "authority" | "date", id: string | undefined) =>
    id ? profile.segments.reduce((count, entry) => count + entry.fields.filter((item) => item[key] === id).length, 0) : 0;
  const apply = () => {
    const next: Field = { ...value };
    // Repetitions are written only once they were set here or already were.
    if (field?.cardinality || repeatTouched) next.cardinality = { min, max: many ? unbounded : String(max || min) };
    let terminology = profile.terminology ?? [];
    let authorities = profile.authorities ?? [];
    let dates = profile.dates ?? [];
    const own = (key: "terminology" | "authority" | "date", id: string | undefined, suffix: string) =>
      id && users(key, id) <= 1 ? id : `${path}-${suffix}`;
    if (codes.length > 0) {
      const changed = !codesOf || JSON.stringify(codesOf.codes) !== JSON.stringify(codes) || codesOf.binding !== binding;
      if (changed) {
        const id = own("terminology", field?.terminology, "codes");
        terminology = [...terminology.filter((set) => set.id !== id), { id, binding, codes }];
        next.terminology = id;
      }
    } else delete next.terminology;
    if (authority.namespace || authority.universal_id || authority.universal_id_type) {
      const changed = !authorityOf || JSON.stringify({ ...authorityOf, id: "" }) !== JSON.stringify({ ...authority, id: "" });
      if (changed) {
        const id = own("authority", field?.authority, "authority");
        authorities = [...authorities.filter((entry) => entry.id !== id), { ...authority, id }];
        next.authority = id;
      }
    } else delete next.authority;
    if (precision) {
      const changed = !dateOf || dateOf.precision !== precision || dateOf.timezone !== zone;
      if (changed) {
        const id = own("date", field?.date, "date");
        dates = [...dates.filter((entry) => entry.id !== id), { id, precision, timezone: zone }];
        next.date = id;
      }
    } else delete next.date;
    if (next.usage !== "C") delete next.condition;
    else if (next.condition?.operator === "value_in") next.condition = { ...next.condition, values: valuesText.split(",").map((part) => part.trim()).filter(Boolean) };
    else if (next.condition) {
      const { values: _unused, ...rest } = next.condition;
      next.condition = rest;
    }
    const fields = field === null ? [...segment.fields, next].sort((a, b) => a.position - b.position) : segment.fields.map((entry) => (entry.position === field.position ? next : entry));
    onApply({ ...profile, terminology, authorities, dates, segments: profile.segments.map((entry) => (entry.id === segment.id ? { ...entry, fields } : entry)) }, next.position);
    onClose();
  };
  const condition = value.condition ?? { segment: segment.id, position: 1, operator: "present" as LocalProfileConditionOperator };
  const [valuesText, setValuesText] = useState((field?.condition?.values ?? []).join(", "));
  return (
    <FormDialog open title={field ? `Edit ${path}` : "Add field"} submitLabel="Done" submitDisabled={value.position < 1 || taken || (!many && Number(max) < min)} dirty onClose={onClose} onSubmit={apply}>
      {field === null ? (
        <>
          <label htmlFor="field-position">Position</label>
          <input id="field-position" type="number" min={1} value={value.position} aria-invalid={taken} onChange={(event) => setValue({ ...value, position: Math.max(1, Math.trunc(Number(event.target.value) || 1)) })} />
        </>
      ) : null}
      <label htmlFor="field-label">Label</label>
      <input id="field-label" type="text" value={value.name ?? ""} onChange={(event) => setValue({ ...value, name: event.target.value })} />
      <label htmlFor="field-presence">Presence</label>
      <select id="field-presence" value={value.usage} onChange={(event) => {
          const usage = event.target.value as LocalProfileUsage;
          // Conditional always carries its predicate, starting from the default one.
          setValue({ ...value, usage, ...(usage === "C" ? { condition } : {}) });
        }}>
        {(vocabulary?.usages ?? (Object.keys(PRESENCE) as LocalProfileUsage[])).map((usage) => (
          <option key={usage} value={usage}>
            {PRESENCE[usage]}
          </option>
        ))}
      </select>
      {value.usage === "C" ? (
        <fieldset>
          <legend>Condition</legend>
          <div className="inline-fields">
            <span>
              <label htmlFor="condition-segment">Segment</label>
              <input id="condition-segment" type="text" maxLength={3} value={condition.segment} onChange={(event) => setValue({ ...value, condition: { ...condition, segment: event.target.value.toUpperCase() } })} />
            </span>
            <span>
              <label htmlFor="condition-position">Field</label>
              <input id="condition-position" type="number" min={1} value={condition.position} onChange={(event) => setValue({ ...value, condition: { ...condition, position: Math.max(1, Math.trunc(Number(event.target.value) || 1)) } })} />
            </span>
          </div>
          <label htmlFor="condition-operator">Rule</label>
          <select id="condition-operator" value={condition.operator} onChange={(event) => setValue({ ...value, condition: { ...condition, operator: event.target.value as LocalProfileConditionOperator } })}>
            {(Object.keys(CONDITIONS) as LocalProfileConditionOperator[]).map((operator) => (
              <option key={operator} value={operator}>
                {CONDITIONS[operator]}
              </option>
            ))}
          </select>
          {condition.operator === "value_in" ? (
            <>
              <label htmlFor="condition-values">Values</label>
              <input
                id="condition-values"
                type="text"
                value={valuesText}
                onChange={(event) => setValuesText(event.target.value)}
              />
            </>
          ) : null}
        </fieldset>
      ) : null}
      <label htmlFor="field-type">Type</label>
      <select id="field-type" value={value.type ?? ""} onChange={(event) => setValue({ ...value, ...(event.target.value ? { type: event.target.value } : { type: undefined }) } as Field)}>
        <option value="">From the base</option>
        {(vocabulary?.data_types ?? []).map((type) => (
          <option key={type}>{type}</option>
        ))}
      </select>
      <fieldset>
        <legend>Repetitions</legend>
        <div className="inline-fields">
          <span>
            <label htmlFor="field-min">Minimum</label>
            <input id="field-min" type="number" min={0} value={min} onChange={(event) => {
                setRepeatTouched(true);
                setMin(Math.max(0, Math.trunc(Number(event.target.value) || 0)));
              }} />
          </span>
          <span>
            <label htmlFor="field-max">Maximum</label>
            <input id="field-max" type="number" min={1} disabled={many} value={many ? "" : max} onChange={(event) => {
                setRepeatTouched(true);
                setMax(event.target.value);
              }} />
          </span>
        </div>
        <label className="check">
          <input
            type="checkbox"
            checked={many}
            onChange={(event) => {
              setRepeatTouched(true);
              setMany(event.target.checked);
            }}
          />
          Unbounded
        </label>
      </fieldset>
      <fieldset>
        <legend>Codes</legend>
        {codes.map((code, index) => (
          <div key={index} className="inline-fields">
            <span>
              <label htmlFor={`code-${index}`}>Code</label>
              <input id={`code-${index}`} type="text" value={code.code} onChange={(event) => setCodes(codes.map((entry, at) => (at === index ? { ...entry, code: event.target.value } : entry)))} />
            </span>
            <span>
              <label htmlFor={`code-display-${index}`}>Display</label>
              <input id={`code-display-${index}`} type="text" value={code.display ?? ""} onChange={(event) => setCodes(codes.map((entry, at) => (at === index ? { ...entry, display: event.target.value } : entry)))} />
            </span>
            <button type="button" className="quiet" aria-label={`Remove code ${code.code || index + 1}`} onClick={() => setCodes(codes.filter((_, at) => at !== index))}>
              Remove
            </button>
          </div>
        ))}
        <button type="button" className="quiet" onClick={() => setCodes([...codes, { code: "" }])}>
          Add code
        </button>
        {codes.length > 0 ? (
          <label className="check">
            <input type="checkbox" checked={binding === "suggested"} onChange={(event) => setBinding(event.target.checked ? "suggested" : "required")} />
            Other codes allowed
          </label>
        ) : null}
      </fieldset>
      <fieldset>
        <legend>Authority</legend>
        <label htmlFor="authority-namespace">Namespace</label>
        <input id="authority-namespace" type="text" value={authority.namespace ?? ""} onChange={(event) => setAuthority({ ...authority, namespace: event.target.value })} />
        <label htmlFor="authority-uid">Universal ID</label>
        <input id="authority-uid" type="text" value={authority.universal_id ?? ""} onChange={(event) => setAuthority({ ...authority, universal_id: event.target.value })} />
        <label htmlFor="authority-type">ID type</label>
        <select id="authority-type" value={authority.universal_id_type ?? ""} onChange={(event) => setAuthority({ ...authority, universal_id_type: event.target.value })}>
          <option value="">None</option>
          {(vocabulary?.universal_id_types ?? []).map((type) => (
            <option key={type}>{type}</option>
          ))}
        </select>
      </fieldset>
      <fieldset>
        <legend>Date handling</legend>
        <label htmlFor="date-precision">Precision</label>
        <select id="date-precision" value={precision} onChange={(event) => setPrecision(event.target.value as LocalProfilePrecision | "")}>
          <option value="">Not a date</option>
          {(Object.keys(PRECISIONS) as LocalProfilePrecision[]).map((entry) => (
            <option key={entry} value={entry}>
              {PRECISIONS[entry]}
            </option>
          ))}
        </select>
        {precision ? (
          <>
            <label htmlFor="date-zone">Time zone</label>
            <select id="date-zone" value={zone} onChange={(event) => setZone(event.target.value as LocalProfileTimeZoneRule)}>
              {(Object.keys(ZONE_RULES) as LocalProfileTimeZoneRule[]).map((entry) => (
                <option key={entry} value={entry}>
                  {ZONE_RULES[entry]}
                </option>
              ))}
            </select>
          </>
        ) : null}
      </fieldset>
    </FormDialog>
  );
}

/** What changed since an earlier version, for History. */
function CompareButton({ context, profile, from }: { context: () => RequestContext; profile: ItemRef; from: string }) {
  const [comparison, setComparison] = useState<ProfileVersionComparison | string | null>(null);
  return (
    <>
      <button
        type="button"
        className="quiet"
        onClick={() =>
          void compareProfileVersions({ context: context(), ref: profile, from }).then((answer) =>
            setComparison(answer.comparison ?? answer.reason ?? "The versions cannot be compared."),
          )
        }
      >
        Compare
      </button>
      <Modal
        open={comparison !== null}
        title={`Changes since v${from}`}
        onClose={() => setComparison(null)}
        footer={
          <div className="dialog-footer">
            <button type="button" onClick={() => setComparison(null)}>
              Close
            </button>
          </div>
        }
      >
        {typeof comparison === "string" ? <p role="alert">{comparison}</p> : <ChangeList comparison={comparison} />}
      </Modal>
    </>
  );
}

function ChangeList({ comparison }: { comparison: ProfileVersionComparison | null }) {
  if (!comparison) return null;
  if (comparison.changes.length === 0) return <p>No changes</p>;
  return <ValueRows rows={comparison.changes.map((change) => ({ label: change.subject || change.part, value: change.detail }))} />;
}

/** The tests that pin this profile, and a reviewed move of chosen pins. */
function AffectedTestsSheet({ context, profile, onClose }: { context: () => RequestContext; profile: ItemRef; onClose: () => void }) {
  const [tests, setTests] = useState<AffectedTest[] | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const [chosen, setChosen] = useState<Set<string>>(new Set());
  const [review, setReview] = useState<{ tests: AffectedTest[]; changes: Map<string, ProfileVersionComparison | string> } | null>(null);
  const [outcome, setOutcome] = useState<string | null>(null);
  const load = () =>
    void profileAffectedTests({ context: context(), ref: profile }).then((answer) => {
      setFailure(answer.state === "completed" || answer.state === "empty" ? null : (answer.reason ?? "The tests cannot be read."));
      setTests(answer.tests);
    });
  useEffect(load, []); // eslint-disable-line react-hooks/exhaustive-deps
  const IMPACTS: Record<string, string> = { affected: "Affected", unaffected: "Not affected", current: "Current", unrelated: "Not related", unknown: "Unknown" };
  const upgradeable = (test: AffectedTest) => test.impact === "affected" || test.impact === "unaffected";
  return (
    <>
      <Modal
        open={review === null}
        title="Affected tests"
        onClose={onClose}
        footer={
          <div className="dialog-footer">
            <button type="button" onClick={onClose}>
              Close
            </button>
            <button
              type="button"
              className="primary"
              disabled={chosen.size === 0}
              onClick={async () => {
                const picked = (tests ?? []).filter((test) => chosen.has(test.ref.id));
                const changes = new Map<string, ProfileVersionComparison | string>();
                for (const version of new Set(picked.map((test) => test.pinned.version))) {
                  const answer = await compareProfileVersions({ context: context(), ref: profile, from: version });
                  changes.set(version, answer.comparison ?? answer.reason ?? "The versions cannot be compared.");
                }
                setReview({ tests: picked, changes });
              }}
            >
              Upgrade selected
            </button>
          </div>
        }
      >
        {failure ? <p role="alert">{failure}</p> : null}
        {outcome ? <p role="status">{outcome}</p> : null}
        {tests && tests.length === 0 ? (
          <p>No tests use this profile</p>
        ) : (
          <table className="drafts-table">
            <thead>
              <tr>
                <th scope="col">
                  <span className="visually-hidden">Upgrade</span>
                </th>
                <th scope="col">Test</th>
                <th scope="col">Pinned</th>
                <th scope="col">Impact</th>
              </tr>
            </thead>
            <tbody>
              {(tests ?? []).map((test) => (
                <tr key={test.ref.id}>
                  <td>
                    <input
                      type="checkbox"
                      aria-label={`Upgrade ${test.name}`}
                      disabled={!upgradeable(test)}
                      checked={chosen.has(test.ref.id)}
                      onChange={() =>
                        setChosen((held) => {
                          const next = new Set(held);
                          if (next.has(test.ref.id)) next.delete(test.ref.id);
                          else next.add(test.ref.id);
                          return next;
                        })
                      }
                    />
                  </td>
                  <th scope="row">{test.name}</th>
                  <td>v{test.pinned.version}</td>
                  <td>{IMPACTS[test.impact] ?? test.impact}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Modal>
      {review ? (
        <FormDialog
          open
          title="Upgrade pins"
          submitLabel="Upgrade"
          onClose={() => setReview(null)}
          onSubmit={async () => {
            const answer = await upgradeProfilePins({ context: context(), profile, tests: review.tests.map((test) => test.ref), intent_id: newIntentId() });
            if (answer.state !== "completed") return { reason: answer.reason ?? "No pin was moved." };
            setOutcome(
              [answer.upgraded.length > 0 ? `${answer.upgraded.length === 1 ? "1 test" : `${answer.upgraded.length} tests`} upgraded` : "", ...answer.refused.map((entry) => entry.reason)].filter(Boolean).join(". "),
            );
            setReview(null);
            setChosen(new Set());
            load();
          }}
        >
          <p className="consequence">Each chosen test is saved as a new version pinned to this profile version; other tests keep their pins.</p>
          {[...review.changes.entries()].map(([version, change]) => (
            <section key={version} aria-label={`Changes since v${version}`}>
              <h3>
                {review.tests.filter((test) => test.pinned.version === version).map((test) => test.name).join(", ")} · from v{version}
              </h3>
              {typeof change === "string" ? <p role="alert">{change}</p> : <ChangeList comparison={change} />}
            </section>
          ))}
        </FormDialog>
      ) : null}
    </>
  );
}

/** The metadata packs the project holds, each with its support by family. */
export function PacksSheet({ context, onClose, onImported }: { context: () => RequestContext; onClose: () => void; onImported: (draft: ItemDraftResult) => void }) {
  const [packs, setPacks] = useState<MetadataPack[] | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const closeControl = useRef<HTMLButtonElement | null>(null);
  const reads = useLifecycle<"reading">({ background: true });
  const operation = useLifecycle<"importing">({ stops: { importing: closeControl } });
  const { run, withdraw } = operation;
  const { run: read, withdraw: withdrawRead } = reads;
  const latestContext = useRef(context);
  latestContext.current = context;
  useEffect(() => {
    withdraw();
    withdrawRead();
    setPacks(null);
    setFailure(null);
    const requested = context();
    void read("reading", async (current) => {
      const answer = await metadataPacks(requested);
      if (!current() || latestContext.current !== context) return;
      setPacks(answer.packs);
      if (answer.state !== "completed" && answer.state !== "empty") setFailure(answer.reason ?? "The metadata packs could not be read.");
    });
    return () => { withdraw(); withdrawRead(); };
  }, [context, read, withdraw, withdrawRead]);
  const close = () => { withdraw(); withdrawRead(); onClose(); };
  const shown = packs?.find((pack) => pack.item.ref.id === selected) ?? null;
  return (
    <Modal
      open
      title="Metadata packs"
      size="wide"
      onClose={close}
      footer={
        <div className="dialog-footer">
          <button type="button" disabled={operation.running !== null || packs === null} onClick={() => {
            setFailure(null);
            const requested = context();
            const owner = context;
            void run("importing", async (current) => {
              const wanted = () => current() && latestContext.current === owner;
              const chosen = await chooseLibraryFile("metadata-pack");
              if (!wanted() || chosen.state === "cancelled") return;
              const path = chosen.paths?.[0];
              if (chosen.state !== "completed" || !path) { setFailure(chosen.reason ?? "No metadata pack was chosen."); return; }
              const answer = await importLibraryItem({ context: requested, kind: "profile", path });
              if (!wanted()) return;
              if (answer.state !== "completed" || !answer.draft?.profile?.metadata_pack) { setFailure(answer.reason ?? "This is not a supported metadata pack."); return; }
              onClose(); onImported(answer);
            });
          }}>Import metadata pack</button>
          <button type="button" ref={closeControl} onClick={close}>
            Close
          </button>
        </div>
      }
    >
      {failure ? <p role="alert">{failure}</p> : null}
      <DataTable
        label="Metadata packs"
        className="values-table"
        rows={packs ?? []}
        rowId={(pack) => pack.item.ref.id}
        rowLabel={(pack) => pack.item.name}
        selected={selected}
        onSelect={setSelected}
        onOpen={setSelected}
        loading={packs === null}
        columns={[
          { key: "name", header: "Pack", priority: 1, minWidth: 12, flex: true, render: (pack) => pack.item.name },
          { key: "version", header: "Version", priority: 2, minWidth: 6, render: (pack) => (pack.pack ? `v${pack.pack.version}` : "—") },
        ]}
      />
      {shown ? (
        <DataTable
          label={`Support of ${shown.item.name}`}
          className="values-table"
          rows={shown.matrix}
          rowId={(row) => `${row.hl7_version}-${row.family}`}
          rowLabel={(row) => `${row.family} ${row.hl7_version}`}
          selected={null}
          onSelect={() => {}}
          onOpen={() => {}}
          columns={[
            { key: "family", header: "Family", priority: 1, minWidth: 6, render: (row) => `${row.family} ${row.hl7_version}` },
            { key: "parse", header: "Parse", priority: 1, minWidth: 6, render: (row) => OUTCOMES[row.parse] },
            { key: "labels", header: "Labels", priority: 2, minWidth: 6, render: (row) => OUTCOMES[row.labels] },
            { key: "structural", header: "Structure", priority: 2, minWidth: 6, render: (row) => OUTCOMES[row.structural] },
            { key: "workflow", header: "Workflow", priority: 3, minWidth: 6, render: (row) => OUTCOMES[row.workflow] },
          ]}
        />
      ) : null}
    </Modal>
  );
}

/** The saved document of a draft, edited as text and read back strictly. */
export function JsonSheet({ context, draft, kind = "profile", onClose, onApply }: { context: () => RequestContext; draft: ItemDraft; kind?: "profile" | "check-group" | "scenario"; onClose: () => void; onApply: (draft: ItemDraft) => void }) {
  const [text, setText] = useState<string | null>(null);
  const [original, setOriginal] = useState("");
  const [failure, setFailure] = useState<string | null>(null);
  useEffect(() => {
    void libraryDocument({ context: context(), kind, draft }).then((answer) => {
      if (answer.document === undefined) setFailure(answer.reason ?? "The document cannot be shown.");
      setText(answer.document ?? "");
      setOriginal(answer.document ?? "");
    });
  }, []); // eslint-disable-line react-hooks/exhaustive-deps
  return (
    <FormDialog
      open
      title="Edit JSON"
      size="wide"
      submitLabel="Apply"
      submitDisabled={text === null || failure !== null && text === original}
      dirty={text !== original}
      onClose={onClose}
      onSubmit={async () => {
        const answer = await applyLibraryDocument({ context: context(), kind, draft, document: text ?? "" });
        if (answer.state !== "completed" || !answer.draft) return { reason: answer.problems?.map((problem) => problem.problem).join(" ") || answer.reason || "The document does not read.", field: "library-json" };
        onApply(answer.draft);
        onClose();
      }}
    >
      {failure ? <p role="alert">{failure}</p> : null}
      <label htmlFor="library-json" className="visually-hidden">
        Document
      </label>
      <textarea id="library-json" className="json-editor" rows={24} spellCheck={false} value={text ?? ""} onChange={(event) => setText(event.target.value)} />
    </FormDialog>
  );
}


const VERDICTS: Record<string, string> = { pass: "Conforms", fail: "Does not conform", undecided: "Undecided", unsupported: "Unsupported" };
const SUPPORT: Record<string, string> = { supported: "Supported", unsupported: "Unsupported", undeclared: "Not declared", untested: "Untested", unknown: "Unknown" };

/** A case evaluated against this profile version, as `readmit profile evaluate` does. */
function EvaluateSheet({ context, profile, initialCase, onClose }: { context: () => RequestContext; profile: ItemRef; initialCase?: ItemRef | undefined; onClose: () => void }) {
  const [cases, setCases] = useState<CatalogItem[]>([]);
  const [chosen, setChosen] = useState(initialCase?.id ?? "");
  const [complete, setComplete] = useState(false);
  const [report, setReport] = useState<ProfileEvaluationResult | null>(null);
  useEffect(() => {
    void listWholeCatalog({ context: context(), kind: "case", filter: {} }).then((answer) => setCases((answer.page?.items ?? []).filter((item) => item.availability === "available")));
  }, []); // eslint-disable-line react-hooks/exhaustive-deps
  const evaluated = report?.report;
  return (
    <FormDialog
      open
      title="Evaluate a case"
      size="wide"
      submitLabel="Evaluate"
      submitDisabled={chosen === ""}
      onClose={onClose}
      onSubmit={async () => {
        const item = cases.find((entry) => entry.ref.id === chosen);
        if (!item) return { reason: "Choose a case." };
        const answer = await evaluateProfile({ context: context(), profile, case: item.ref, complete_capture: complete });
        setReport(answer);
        // The report shows here; the sheet stays open to read it.
        return answer.state === "completed" ? undefined : { reason: answer.reason ?? "The case was not evaluated." };
      }}
    >
      <label htmlFor="evaluate-case">Case</label>
      <select id="evaluate-case" value={chosen} onChange={(event) => (setChosen(event.target.value), setReport(null))}>
        <option value="" disabled>
          Choose a case
        </option>
        {cases.map((item) => (
          <option key={item.ref.id} value={item.ref.id}>
            {item.name}
          </option>
        ))}
      </select>
      <label className="check">
        <input type="checkbox" checked={complete} onChange={(event) => (setComplete(event.target.checked), setReport(null))} />
        The case holds the whole capture
      </label>
      {evaluated ? (
        <>
          <ValueRows
            rows={[
              { label: "Result", value: VERDICTS[evaluated.verdict] ?? evaluated.verdict },
              { label: "Local rules", value: VERDICTS[evaluated.local_verdict] ?? evaluated.local_verdict },
              { label: "Base support", value: SUPPORT[evaluated.base_support] ?? evaluated.base_support },
              { label: "Workflow support", value: SUPPORT[evaluated.workflow_support] ?? evaluated.workflow_support },
            ]}
          />
          {evaluated.findings.length > 0 ? (
            <DataTable
              label="Findings"
              className="values-table"
              rows={evaluated.findings.map((finding, index) => ({ ...finding, key: String(index) }))}
              rowId={(finding) => finding.key}
              rowLabel={(finding) => `${messageName(finding.occurrence)} ${finding.selector ?? ""}`}
              selected={null}
              onSelect={() => {}}
              onOpen={() => {}}
              columns={[
                { key: "message", header: "Message", priority: 1, minWidth: 7, render: (finding) => messageName(finding.occurrence) },
                { key: "field", header: "Field", priority: 1, minWidth: 6, render: (finding) => finding.selector || "—" },
                { key: "outcome", header: "Outcome", priority: 1, minWidth: 7, render: (finding) => VERDICTS[finding.outcome] ?? finding.outcome },
                { key: "origin", header: "Rule from", priority: 3, minWidth: 6, render: (finding) => ORIGINS[finding.origin as LocalProfileOrigin] ?? finding.origin },
              ]}
            />
          ) : null}
        </>
      ) : null}
    </FormDialog>
  );
}
