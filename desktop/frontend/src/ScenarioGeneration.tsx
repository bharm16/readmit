import type { CaseGenerationSettings, CasegenBindings, CasegenCode, CasegenPerson, ItemRef, CatalogItem, ScenarioScenario } from "./bindings";

/** The wire and business facts supplied to the Go encoder. Existing arrays,
 * edits and variants are retained whole; each input changes one named fact. */
export function GenerationFields({ value, scenario, onChange }: { value: CaseGenerationSettings; scenario: ScenarioScenario; onChange: (value: CaseGenerationSettings) => void }) {
  const wire = value.wire;
  const updateWire = (patch: Partial<typeof wire>) => onChange({ ...value, wire: { ...wire, ...patch } });
  const binding = value.bindings;
  const update = <K extends keyof CasegenBindings,>(key: K, next: CasegenBindings[K]) => onChange({ ...value, bindings: { ...binding, [key]: next } });
  return <>
    <fieldset><legend>Message encoding</legend>
      <Text label="Delimiters" value={wire.delimiters} onChange={(delimiters) => updateWire({ delimiters })} />
      <Pick label="Timestamp precision" value={wire.precision} choices={["second", "minute"]} onChange={(precision) => updateWire({ precision })} />
      <Text label="UTC offset" value={wire.offset} onChange={(offset) => updateWire({ offset })} />
      <Pick label="Processing mode" value={wire.processing_id} choices={["T", "D", "P"]} onChange={(processing_id) => updateWire({ processing_id })} />
      <Text label="Sending application" value={wire.sending.application} onChange={(application) => updateWire({ sending: { ...wire.sending, application } })} />
      <Text label="Sending facility" value={wire.sending.facility} onChange={(facility) => updateWire({ sending: { ...wire.sending, facility } })} />
      <Text label="Receiving application" value={wire.receiving.application} onChange={(application) => updateWire({ receiving: { ...wire.receiving, application } })} />
      <Text label="Receiving facility" value={wire.receiving.facility} onChange={(facility) => updateWire({ receiving: { ...wire.receiving, facility } })} />
      <Pick label="Resource updates" value={wire.resource_updates} choices={["action-code", "snapshot"]} onChange={(resource_updates) => updateWire({ resource_updates })} />
    </fieldset>
    {binding.patients.map((entry, at) => <fieldset key={entry.subject}><legend>Patient {entry.subject}</legend>
      <Text label="Identifier type" value={entry.identifier_type} onChange={(identifier_type) => update("patients", binding.patients.map((held, i) => i === at ? { ...held, identifier_type } : held))} />
    </fieldset>)}
    {binding.visits.map((entry, at) => {
      const change = (patch: Partial<typeof entry>) => update("visits", binding.visits.map((held, i) => i === at ? { ...held, ...patch } : held));
      return <fieldset key={entry.subject}><legend>Visit {entry.subject}</legend>
        <Text label="Patient class" value={entry.class} onChange={(v) => change({ class: v })} />
        <Text label="Identifier type" value={entry.identifier_type} onChange={(identifier_type) => change({ identifier_type })} />
        {(["point_of_care", "room", "bed"] as const).map((key) => <Text key={key} label={key.replaceAll("_", " ")} value={entry.location[key]} onChange={(v) => change({ location: { ...entry.location, [key]: v } })} />)}
      </fieldset>;
    })}
    {binding.appointments.map((entry, at) => {
      const change = (patch: Partial<typeof entry>) => update("appointments", binding.appointments.map((held, i) => i === at ? { ...held, ...patch } : held));
      return <fieldset key={entry.subject}><legend>Appointment {entry.subject}</legend>
        <Text label="Start after base time" value={entry.start_after} onChange={(start_after) => change({ start_after })} />
        <NumberField label="Duration in minutes" value={entry.duration_minutes} onChange={(duration_minutes) => change({ duration_minutes })} />
        <Text label="Placer namespace" value={entry.placer?.namespace ?? ""} onChange={(namespace) => change({ placer: { namespace, identifier: entry.placer?.identifier ?? "" } })} />
        <Text label="Placer identifier" value={entry.placer?.identifier ?? ""} onChange={(identifier) => change({ placer: { namespace: entry.placer?.namespace ?? "", identifier } })} />
        <CodeFields label="Reason" value={entry.reason} onChange={(reason) => change({ reason })} />
        <PersonFields label="Contact" value={entry.contact} onChange={(contact) => change({ contact })} />
        <PersonFields label="Entered by" value={entry.entered_by} onChange={(entered_by) => change({ entered_by })} />
        {entry.resources.map((resource, index) => <fieldset key={index}><legend>Resource {index + 1}</legend>
          <Text label="Name" value={resource.id} onChange={(id) => change({ resources: entry.resources.map((held, i) => i === index ? { ...held, id } : held) })} />
          <Pick label="Kind" value={resource.kind} choices={["service", "location", "personnel", "general"]} onChange={(kind) => change({ resources: entry.resources.map((held, i) => i === index ? { ...held, kind } : held) })} />
          <CodeFields label="Identifier" value={resource.identifier} onChange={(identifier) => change({ resources: entry.resources.map((held, i) => i === index ? { ...held, identifier } : held) })} />
          <CodeFields label="Role" value={resource.role} onChange={(role) => change({ resources: entry.resources.map((held, i) => i === index ? { ...held, role } : held) })} />
          <Text label="Identifier type" value={resource.identifier_type ?? ""} onChange={(identifier_type) => change({ resources: entry.resources.map((held, i) => i === index ? { ...held, identifier_type } : held) })} />
          <Text label="Name type" value={resource.name_type ?? ""} onChange={(name_type) => change({ resources: entry.resources.map((held, i) => i === index ? { ...held, name_type } : held) })} />
          <button type="button" onClick={() => change({ resources: entry.resources.filter((_, i) => i !== index) })}>Remove resource {index + 1}</button>
        </fieldset>)}
        <button type="button" onClick={() => change({ resources: [...entry.resources, { id: "", kind: "service", identifier: emptyCode(), role: emptyCode() }] })}>Add resource</button>
      </fieldset>;
    })}
    {binding.resources.map((entry, at) => {
      const change = (patch: Partial<typeof entry>) => update("resources", binding.resources.map((held, i) => i === at ? { ...held, ...patch } : held));
      return <fieldset key={entry.subject}><legend>Resource {entry.subject}</legend>
        <Pick label="Kind" value={entry.kind} choices={["service", "location", "personnel", "general"]} onChange={(kind) => change({ kind })} />
        <Text label="Text" value={entry.text} onChange={(text) => change({ text })} />
        <CodeFields label="Role" value={entry.role} onChange={(role) => change({ role })} />
        <Text label="Identifier type" value={entry.identifier_type ?? ""} onChange={(identifier_type) => change({ identifier_type })} />
        <Text label="Name type" value={entry.name_type ?? ""} onChange={(name_type) => change({ name_type })} />
      </fieldset>;
    })}
    {binding.orders.map((entry, at) => {
      const change = (patch: Partial<typeof entry>) => update("orders", binding.orders.map((held, i) => i === at ? { ...held, ...patch } : held));
      return <fieldset key={entry.subject}><legend>Order encoding {entry.subject}</legend>
        <CodeFields label="Service" value={entry.service} onChange={(service) => change({ service })} />
        <Text label="Observation system" value={entry.observation_system} onChange={(observation_system) => change({ observation_system })} />
      </fieldset>;
    })}
    {binding.edits.map((entry, at) => {
      const change = (patch: Partial<typeof entry>) => update("edits", binding.edits.map((held, i) => i === at ? { ...held, ...patch } : held));
      return <fieldset key={at}><legend>Business change {entry.step}</legend>
        <Pick label="Step" value={entry.step} choices={scenario.steps.map((step) => step.id)} onChange={(step) => change({ step })} />
        <Pick label="Change" value={entry.op} choices={["rename", "move", "reschedule"]} onChange={(op) => change({ op })} />
        {entry.op === "rename" ? <><Text label="Family" value={entry.family ?? ""} onChange={(family) => change({ family })} /><Text label="Given" value={entry.given ?? ""} onChange={(given) => change({ given })} /></> : null}
        {entry.op === "reschedule" ? <><Text label="Start after base time" value={entry.start_after ?? ""} onChange={(start_after) => change({ start_after })} /><NumberField label="Duration in minutes" value={entry.duration_minutes ?? 0} onChange={(duration_minutes) => change({ duration_minutes })} /></> : null}
        {entry.op === "move" ? (["point_of_care", "room", "bed"] as const).map((key) => <Text key={key} label={key.replaceAll("_", " ")} value={entry.location?.[key] ?? ""} onChange={(v) => change({ location: { point_of_care: "", room: "", bed: "", ...entry.location, [key]: v } })} />) : null}
        <button type="button" onClick={() => update("edits", binding.edits.filter((_, i) => i !== at))}>Remove business change {at + 1}</button>
      </fieldset>;
    })}
    <button type="button" onClick={() => update("edits", [...binding.edits, { step: scenario.steps[0]?.id ?? "", op: "reschedule", start_after: "", duration_minutes: 0 }])}>Add business change</button>
  </>;
}

export function initialGeneration(scenario: ScenarioScenario): CaseGenerationSettings {
  return {
    wire: { delimiters: "|^~\\&", precision: "second", offset: "+00:00", processing_id: "T", sending: { application: "", facility: "" }, receiving: { application: "", facility: "" }, resource_updates: "action-code" },
    bindings: {
      patients: scenario.subjects.filter((s) => s.kind === "patient").map((s) => ({ subject: s.id, identifier_type: "", additional_identifiers: [] })),
      visits: scenario.subjects.filter((s) => s.kind === "visit").map((s) => ({ subject: s.id, class: "", identifier_type: "", location: { point_of_care: "", room: "", bed: "" } })),
      appointments: scenario.subjects.filter((s) => s.kind === "appointment").map((s) => ({ subject: s.id, placer: null, start_after: "", duration_minutes: 0, reason: emptyCode(), contact: emptyPerson(), entered_by: emptyPerson(), resources: [] })),
      resources: scenario.subjects.filter((s) => s.kind === "resource").map((s) => ({ subject: s.id, kind: "service", text: "", role: emptyCode() })),
      orders: scenario.subjects.filter((s) => s.kind === "order").map((s) => ({ subject: s.id, service: emptyCode(), observation_system: "" })), edits: [],
    }, variants: [],
  };
}

export function ProfilePin({ value, profiles, family, onChange }: { value?: ItemRef | undefined; profiles: CatalogItem[]; family: string; onChange: (ref: ItemRef | undefined) => void }) {
  const offered = profiles.filter((item) => item.summary.profile?.form === "local-profile" && (!family || item.summary.profile.family === family));
  const current = profiles.find((item) => item.ref.id === value?.id);
  const key = value ? `${value.id}@${value.revision ?? ""}` : "";
  return <label>Local profile
    <select value={key} onChange={(event) => onChange(offered.find((item) => `${item.ref.id}@${item.ref.revision ?? ""}` === event.target.value)?.ref)}>
      <option value="">Choose a saved profile</option>
      {value && !offered.some((item) => `${item.ref.id}@${item.ref.revision ?? ""}` === key) ? <option value={key}>{current?.name ?? "Saved profile"} · v{value.revision ?? "—"}</option> : null}
      {offered.map((item) => <option key={item.ref.id} value={`${item.ref.id}@${item.ref.revision ?? ""}`}>{item.name} · v{item.ref.revision ?? "—"}</option>)}
    </select>
  </label>;
}

function emptyCode(): CasegenCode { return { code: "", text: "", system: "" }; }
function emptyPerson(): CasegenPerson { return { id: "", family: "", given: "", authority: "", id_type: "", name_type: "" }; }
function Text({ label, value, onChange }: { label: string; value: string; onChange: (value: string) => void }) { return <label>{label}<input type="text" value={value} onChange={(event) => onChange(event.target.value)} /></label>; }
function Pick({ label, value, choices, onChange }: { label: string; value: string; choices: string[]; onChange: (value: string) => void }) { return <label>{label}<select value={value} onChange={(event) => onChange(event.target.value)}>{choices.includes(value) ? null : <option value={value}>{value || "Choose"}</option>}{choices.map((choice) => <option key={choice}>{choice}</option>)}</select></label>; }
function NumberField({ label, value, onChange }: { label: string; value: number; onChange: (value: number) => void }) {
  return <label>{label}<input type="number" min={1} step={1} required value={Number.isFinite(value) ? value : ""} onChange={(event) => onChange(event.target.valueAsNumber)} /></label>;
}
function CodeFields({ label, value, onChange }: { label: string; value: CasegenCode; onChange: (value: CasegenCode) => void }) { return <fieldset><legend>{label}</legend>{(["code", "text", "system"] as const).map((key) => <Text key={key} label={key.charAt(0).toUpperCase() + key.slice(1)} value={value[key]} onChange={(text) => onChange({ ...value, [key]: text })} />)}</fieldset>; }
function PersonFields({ label, value, onChange }: { label: string; value: CasegenPerson; onChange: (value: CasegenPerson) => void }) { return <fieldset><legend>{label}</legend>{(["id", "family", "given", "authority", "id_type", "name_type"] as const).map((key) => <Text key={key} label={key.replaceAll("_", " ")} value={value[key]} onChange={(text) => onChange({ ...value, [key]: text })} />)}</fieldset>; }
