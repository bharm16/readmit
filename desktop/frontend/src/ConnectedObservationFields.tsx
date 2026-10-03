// Typed fields in the existing Observation Source/Completion sheet. Go owns
// every selector, supported search parameter and projection template.
import { useEffect, useState } from "react";
import {
  chooseEnvironmentFile, observationFields,
  type CatalogItem, type ConnectedObservation, type ConnectedVocabulary, type DatasetColumn,
  type ObservationFieldsResult, type ObservationSource, type RequestContext,
} from "./bindings";
import { IconButton } from "./IconButton";
import { ValueRows } from "./layout";
import { FilePicker, wholeNumber } from "./Environments";

export type ConnectedNumberProblem = { field: string; label: string; step: "source" | "completion" };
const TYPES: Record<string, string> = { text: "Text", integer: "Whole number", decimal: "Decimal", boolean: "True or false", date: "Date", datetime: "Date and time", code: "Code" };
const PHASES: Record<string, string> = { before: "Before run", after: "After run", both: "Before and after" };
const BOUNDARIES: Record<string, string> = { "authoritative-application-api": "Application API", "delayed-replica": "Delayed replica", "reference-fhir-store": "Reference FHIR store" };

export function ConnectedObservationFields({
  step, setup, source, vocabulary, context, environments, observations, onChange, onTouched, onNumberProblem, onOptions,
}: {
  step: "source" | "completion";
  setup: ConnectedObservation;
  source: ObservationSource;
  vocabulary: ConnectedVocabulary;
  context: () => RequestContext;
  environments: CatalogItem[];
  observations: CatalogItem[];
  onChange: (change: (value: ConnectedObservation) => void) => void;
  onTouched: () => void;
  onNumberProblem: (problem: ConnectedNumberProblem | null) => void;
  onOptions: (options: ObservationFieldsResult | null) => void;
}) {
  const [options, setOptions] = useState<ObservationFieldsResult | null>(null);
  const [sample, setSample] = useState("");
  const [sampleProblem, setSampleProblem] = useState<string | null>(null);
  const [numbers, setNumbers] = useState<Record<string, string>>({});
  const fhir = setup.fhir;
  const resource = fhir?.resource;
  const environment = setup.environment;
  const sourceSelection = JSON.stringify(source);
  // This reads saved metadata or a selected local sample only. It never
  // introspects a remote source or requests current capabilities.
  useEffect(() => {
    let live = true;
    setOptions(null);
    onOptions(null);
    void observationFields({ context: context(), typed: true, ...(resource ? { resource, ...(environment ? { environment } : {}) } : { source }), ...(sample ? { schema_sample: sample } : {}) }).then((answer) => {
      if (live) { setOptions(answer); onOptions(answer); }
    });
    return () => { live = false; };
  }, [context, resource, environment, sourceSelection, sample, onOptions]);

  const number = (id: string, label: string, value: number, _where: "source" | "completion", apply: (draft: ConnectedObservation, number: number) => void) => <>
    <label htmlFor={id}>{label}</label>
    <input id={id} type="text" inputMode="numeric" value={numbers[id] ?? String(value)} onChange={(event) => {
      const typed = event.target.value;
      const next = { ...numbers, [id]: typed };
      setNumbers(next);
      const parsed = wholeNumber(typed);
      if (parsed !== null) onChange((draft) => apply(draft, parsed));
      else onTouched();
      const invalid = Object.keys(next).find((field) => wholeNumber(next[field]!) === null);
      onNumberProblem(invalid ? { field: invalid, label: invalid === id ? label : "this limit", step: invalid.startsWith("observation-paging-") || invalid.startsWith("observation-projection-") ? "source" : "completion" } : null);
    }} />
  </>;

  const fhirFields = vocabulary.resources.find((choice) => choice.resource === resource)?.fields ?? [];
  const projectedKeys = (fhir?.fields ?? setup.projection?.columns ?? []).filter((field) => field.key);
  const chooseSample = async () => {
    setSampleProblem(null);
    const answer = await chooseEnvironmentFile("observation-input");
    if (answer.state === "completed" && answer.paths?.[0]) setSample(answer.paths[0]);
    else if (answer.state !== "cancelled") setSampleProblem(answer.reason ?? "The sample was not chosen.");
  };
  const fieldChoice = (column: DatasetColumn) => options?.choices?.find((choice) => column.selector ? choice.selector === column.selector : column.locator && JSON.stringify(choice.locator) === JSON.stringify(column.locator));
  const continuation = setup.projection?.continuation ?? [];
  const continuationChoices = (options?.continuations ?? []).filter((choice) => choice.locator?.length);
  const continuationChoice = continuationChoices.find((choice) => JSON.stringify(choice.locator) === JSON.stringify(continuation));
  const continuationValue = continuation.length ? continuationChoice?.id ?? "retained-unsupported-continuation" : "";
  const template = options?.projection;
  const differentFormat = !fhir && setup.projection && template && (setup.projection.format !== template.format || setup.projection.order !== template.order || JSON.stringify(setup.projection.envelope) !== JSON.stringify(template.envelope));

  return <>
    {step === "source" ? <>
      {fhir ? <>
        <label htmlFor="observation-connected-environment">Environment</label>
        <select id="observation-connected-environment" value={setup.environment ?? ""} onChange={(event) => onChange((value) => { value.environment = event.target.value; })}>
          <option value="">Choose a FHIR environment</option>
          {environments.filter((item) => item.summary.environment?.protocol === "fhir-r4").map((item) => <option key={item.ref.id} value={item.ref.id}>{item.name}{item.availability !== "available" ? " · Unavailable" : ""}</option>)}
          {setup.environment && !environments.some((item) => item.ref.id === setup.environment) ? <option value={setup.environment}>Environment no longer available</option> : null}
        </select>
        <label htmlFor="observation-connected-resource">Resource type</label>
        <select id="observation-connected-resource" value={fhir.resource} onChange={(event) => onChange((value) => { value.fhir!.resource = event.target.value; })}>
          {vocabulary.resources.map((choice) => <option key={choice.resource} value={choice.resource}>{choice.resource}</option>)}
          {!vocabulary.resources.some((choice) => choice.resource === fhir.resource) ? <option value={fhir.resource}>Unsupported resource type</option> : null}
        </select>
        <label htmlFor="observation-connected-boundary">Source boundary</label>
        <select id="observation-connected-boundary" value={fhir.boundary} onChange={(event) => onChange((value) => { value.fhir!.boundary = event.target.value; })}>
          <option value="">Choose a boundary</option>
          {vocabulary.boundaries.map((boundary) => <option key={boundary} value={boundary}>{BOUNDARIES[boundary] ?? "Unsupported boundary"}</option>)}
          {fhir.boundary && !vocabulary.boundaries.includes(fhir.boundary) ? <option value={fhir.boundary}>Unsupported saved boundary</option> : null}
        </select>
        <fieldset id="observation-connected-criteria" tabIndex={-1}>
          <legend>Search criteria</legend>
          {fhir.criteria.map((criterion, index) => <div key={index} className="filter-rule">
            <label htmlFor={`observation-criterion-${index}`}>Criterion {index + 1}</label>
            <select id={`observation-criterion-${index}`} value={criterion.parameter} onChange={(event) => {
              const selected = options?.search?.find((choice) => choice.name === event.target.value);
              if (selected) onChange((value) => { value.fhir!.criteria[index]!.parameter = selected.name; value.fhir!.criteria[index]!.type = selected.type; });
            }}>
              <option value="">Choose a criterion</option>
              {(options?.search ?? []).map((choice) => <option key={choice.name} value={choice.name}>{choice.name === "_id" ? "Logical ID" : choice.name === "identifier" ? "Identifier" : choice.name}</option>)}
              {criterion.parameter && !options?.search?.some((choice) => choice.name === criterion.parameter && choice.type === criterion.type) ? <option value={criterion.parameter}>Unsupported saved criterion</option> : null}
            </select>
            {criterion.parameter === "identifier" ? <>
              <label htmlFor={`observation-criterion-system-${index}`}>Identifier system {index + 1}</label>
              <input id={`observation-criterion-system-${index}`} type="text" spellCheck={false} value={criterion.system ?? ""} onChange={(event) => onChange((value) => { value.fhir!.criteria[index]!.system = event.target.value; })} />
            </> : null}
            <label htmlFor={`observation-criterion-value-${index}`}>Criterion value {index + 1}</label>
            <input id={`observation-criterion-value-${index}`} type="text" value={criterion.value} onChange={(event) => onChange((value) => { value.fhir!.criteria[index]!.value = event.target.value; })} />
            {options && criterion.parameter && !options.search?.some((choice) => choice.name === criterion.parameter && choice.type === criterion.type) ? <p role="alert">This criterion is unsupported for the selected source. Choose a supported criterion.</p> : null}
            {criterion.parameter !== "identifier" && criterion.system ? <p role="alert">This criterion still has an identifier system. Clear it before using another criterion.<button type="button" className="quiet" onClick={() => onChange((value) => { delete value.fhir!.criteria[index]!.system; })}>Clear identifier system</button></p> : null}
            <IconButton icon="close" label={`Remove criterion ${index + 1}`} onClick={() => onChange((value) => { value.fhir!.criteria.splice(index, 1); })} />
          </div>)}
          <button type="button" disabled={fhir.criteria.length >= 16 || !options?.search?.length} onClick={() => onChange((value) => { value.fhir!.criteria.push({ parameter: "", type: "", value: "" }); })}>Add criterion</button>
        </fieldset>
      </> : <>
        {source.source.kind === "http-api" || source.source.kind === "database-query" ? <FilePicker label="Schema sample" path={sample} onChoose={() => void chooseSample()} /> : null}
        {sampleProblem ? <p role="alert">{sampleProblem}</p> : null}
      </>}
      {options?.reason ? <p role={options.state === "completed" ? "status" : "alert"}>{options.reason}</p> : null}
      {differentFormat && template ? <>
        <p role="alert">The saved projection uses an earlier source format. Its fields are still held.</p>
        <button type="button" onClick={() => {
          onChange((value) => {
            if (!value.projection) return;
            value.projection.format = template.format;
            value.projection.order = template.order;
            value.projection.limits = structuredClone(template.limits);
            if (template.envelope) value.projection.envelope = structuredClone(template.envelope);
            else delete value.projection.envelope;
          });
          const nextNumbers: Record<string, string> = { ...numbers, "observation-projection-rows": String(template.limits.max_rows), "observation-projection-bytes": String(template.limits.max_bytes), "observation-projection-timeout": String(template.limits.timeout_ms) };
          setNumbers(nextNumbers);
          const invalid = Object.keys(nextNumbers).find((field) => wholeNumber(nextNumbers[field]!) === null);
          onNumberProblem(invalid ? { field: invalid, label: "this limit", step: invalid.startsWith("observation-paging-") || invalid.startsWith("observation-projection-") ? "source" : "completion" } : null);
        }}>Use current source format</button>
      </> : null}
      {!fhir && source.source.kind === "http-api" && source.extraction?.envelope === "json" ? <>
        <label htmlFor="observation-projection-continuation">Page continuation field</label>
        <select id="observation-projection-continuation" disabled={!continuationChoices.length} value={continuationValue} onChange={(event) => {
          const choice = continuationChoices.find((entry) => entry.id === event.target.value);
          const locator = choice?.locator;
          if (locator) onChange((value) => {
            if (!value.projection && options?.projection) value.projection = structuredClone(options.projection);
            if (value.projection) value.projection.continuation = [...locator];
          });
        }}>
          <option value="">Choose a continuation field</option>
          {continuationChoices.map((choice) => <option key={choice.id} value={choice.id}>{choice.id}</option>)}
          {continuation.length && !continuationChoice ? <option value="retained-unsupported-continuation">Unsupported saved continuation</option> : null}
        </select>
        {continuation.length && options?.state === "completed" && !continuationChoice ? <p role="alert">The saved continuation is unsupported by this sample. Its projection is still held; choose a supported field.</p> : null}
      </> : null}
      {!fhir && continuation.length > 0 ? <button type="button" className="quiet" onClick={() => onChange((value) => { if (value.projection) delete value.projection.continuation; })}>Remove saved continuation</button> : null}
      <fieldset id="observation-connected-fields" tabIndex={-1}>
        <legend>Typed fields</legend>
        {(fhir?.fields ?? setup.projection?.columns ?? []).map((field, index) => {
          const fhirField = fhir?.fields[index];
          const column = setup.projection?.columns[index];
          const selected = fhirField?.field ?? (column?.locator || column?.selector ? fieldChoice(column)?.id ?? "saved-unsupported" : "");
          const supported = fhirField ? fhirField.field === "" || fhirField.field === "resource-identity" || fhirFields.some((choice) => choice.id === fhirField.field) : !options || selected !== "saved-unsupported";
          return <div key={index} className="filter-rule">
            <label htmlFor={`observation-field-${index}`}>Source field {index + 1}</label>
            <select id={`observation-field-${index}`} value={selected} onChange={(event) => {
              const chosen = event.target.value;
              if (fhirField) onChange((value) => { value.fhir!.fields[index]!.field = chosen; });
              else {
                const choice = options?.choices?.find((entry) => entry.id === chosen);
                if (choice) onChange((value) => { const current = value.projection!.columns[index]!; delete current.locator; delete current.selector; if (choice.locator) current.locator = [...choice.locator]; if (choice.selector) current.selector = choice.selector; });
              }
            }}>
              <option value="">Choose a field</option>
              {fhir ? <option value="resource-identity">Resource identity</option> : null}
              {(fhir ? fhirFields : options?.choices ?? []).map((choice) => <option key={choice.id} value={choice.id}>{choice.id}{"repeated" in choice && choice.repeated ? " · All values" : choice.id.includes("[0]") ? " · First value" : ""}</option>)}
              {!supported || selected === "saved-unsupported" ? <option value={selected}>Unsupported saved field</option> : null}
            </select>
            <label htmlFor={`observation-field-name-${index}`}>Field name {index + 1}</label>
            <input id={`observation-field-name-${index}`} type="text" value={field.name} onChange={(event) => onChange((value) => { (value.fhir?.fields ?? value.projection!.columns)[index]!.name = event.target.value; })} />
            {column ? <>
              <label htmlFor={`observation-field-type-${index}`}>Value type {index + 1}</label>
              <select id={`observation-field-type-${index}`} value={column.type} onChange={(event) => onChange((value) => { value.projection!.columns[index]!.type = event.target.value; })}>
                {vocabulary.value_types.map((type) => <option key={type} value={type}>{TYPES[type] ?? "Unsupported type"}</option>)}
                {!vocabulary.value_types.includes(column.type) ? <option value={column.type}>Unsupported saved type</option> : null}
              </select>
              {column.type === "code" ? <>
                <label htmlFor={`observation-field-code-system-${index}`}>Code system {index + 1}</label>
                <input id={`observation-field-code-system-${index}`} type="text" value={column.code_system ?? ""} onChange={(event) => onChange((value) => { value.projection!.columns[index]!.code_system = event.target.value; })} />
              </> : null}
            </> : fhirField ? <ValueRows rows={[{ label: "Value type", value: fhirField.field === "resource-identity" ? "Text" : TYPES[fhirFields.find((choice) => choice.id === fhirField.field)?.type ?? ""] ?? "Unsupported" }]} /> : null}
            <label className="check"><input type="checkbox" aria-label={`Business key ${index + 1}`} checked={field.key} onChange={(event) => onChange((value) => { (value.fhir?.fields ?? value.projection!.columns)[index]!.key = event.target.checked; })} />Business key</label>
            <label className="check"><input type="checkbox" aria-label={`Required field ${index + 1}`} checked={field.required} onChange={(event) => onChange((value) => { (value.fhir?.fields ?? value.projection!.columns)[index]!.required = event.target.checked; })} />Required</label>
            {column ? <label className="check"><input type="checkbox" aria-label={`Repeated field ${index + 1}`} checked={column.repeated} onChange={(event) => onChange((value) => { value.projection!.columns[index]!.repeated = event.target.checked; })} />Multiple values</label> : fhirFields.find((choice) => choice.id === fhirField?.field)?.repeated ? <p>Multiple values</p> : null}
            {!supported ? <p role="alert">This mapping is unsupported for the selected source. Choose a supported field.</p> : null}
            <IconButton icon="close" label={`Remove field ${index + 1}`} onClick={() => onChange((value) => { (value.fhir?.fields ?? value.projection!.columns).splice(index, 1); })} />
          </div>;
        })}
        <button type="button" disabled={!fhir && !options?.projection} onClick={() => onChange((value) => {
          if (value.fhir) value.fhir.fields.push({ name: "", field: "", key: false, required: false });
          else if (options?.projection) { value.projection ??= structuredClone(options.projection); value.projection.columns.push({ name: "", type: vocabulary.value_types[0] ?? "", key: false, required: false, repeated: false }); }
        })}>Add field</button>
      </fieldset>
      {fhir ? <fieldset>
        <legend>Paging limits</legend>
        {number("observation-paging-pages", "Maximum pages", fhir.budget.pages, "source", (value, n) => { value.fhir!.budget.pages = n; })}
        {number("observation-paging-rows", "Maximum resources", fhir.budget.rows, "source", (value, n) => { value.fhir!.budget.rows = n; })}
        {number("observation-paging-bytes", "Maximum response bytes", fhir.budget.bytes, "source", (value, n) => { value.fhir!.budget.bytes = n; })}
        {number("observation-paging-timeout", "Search deadline (ms)", fhir.budget.timeout_ms, "source", (value, n) => { value.fhir!.budget.timeout_ms = n; })}
      </fieldset> : setup.projection ? <fieldset>
        <legend>Projection limits</legend>
        {number("observation-projection-rows", "Maximum projected records", setup.projection.limits.max_rows, "source", (value, n) => { value.projection!.limits.max_rows = n; })}
        {number("observation-projection-bytes", "Maximum projected bytes", setup.projection.limits.max_bytes, "source", (value, n) => { value.projection!.limits.max_bytes = n; })}
        {number("observation-projection-timeout", "Projection deadline (ms)", setup.projection.limits.timeout_ms, "source", (value, n) => { value.projection!.limits.timeout_ms = n; })}
      </fieldset> : null}
      <label htmlFor="observation-connected-namespace">Result group</label>
      <input id="observation-connected-namespace" type="text" value={setup.namespace} onChange={(event) => onChange((value) => { value.namespace = event.target.value; })} />
      <label htmlFor="observation-connected-phase">Observe</label>
      <select id="observation-connected-phase" value={setup.phase} onChange={(event) => onChange((value) => { value.phase = event.target.value; })}>
        {(setup.capture?["after"]:vocabulary.phases).map((phase) => <option key={phase} value={phase}>{PHASES[phase] ?? "Unsupported phase"}</option>)}
        {!vocabulary.phases.includes(setup.phase) ? <option value={setup.phase}>Unsupported saved phase</option> : null}
      </select>
      <fieldset id="observation-connected-business-keys" tabIndex={-1}>
        <legend>Run business keys</legend>
        {setup.business_keys.map((key, index) => <div key={index} className="filter-rule">
          <label htmlFor={`observation-business-key-${index}`}>Business key field {index + 1}</label>
          <select id={`observation-business-key-${index}`} value={key.field} onChange={(event) => onChange((value) => { value.business_keys[index]!.field = event.target.value; })}>
            <option value="">Choose a projected key</option>
            {projectedKeys.map((field) => <option key={field.name} value={field.name}>{field.name}</option>)}
            {key.field && !projectedKeys.some((field) => field.name === key.field) ? <option value={key.field}>{key.field} · No longer a projected key</option> : null}
          </select>
          <label htmlFor={`observation-run-variable-${index}`}>Run variable {index + 1}</label>
          <input id={`observation-run-variable-${index}`} type="text" value={key.variable} onChange={(event) => onChange((value) => { value.business_keys[index]!.variable = event.target.value; })} />
          {key.field && !projectedKeys.some((field) => field.name === key.field) ? <p role="alert">This business key no longer has a projected key field. Keep it or choose its replacement before saving.</p> : null}
          <IconButton icon="close" label={`Remove business key ${index + 1}`} onClick={() => onChange((value) => { value.business_keys.splice(index, 1); })} />
        </div>)}
        <button type="button" disabled={setup.business_keys.length >= 16} onClick={() => onChange((value) => { value.business_keys.push({ field: "", variable: "" }); })}>Add business key</button>
      </fieldset>
    </> : <>
      <ValueRows rows={[{ label: "Baseline", value: setup.baseline === "before-run" ? "Recorded before this run" : "Unsupported saved baseline" }]} />
      <label htmlFor="observation-connected-barrier">Processing barrier</label>
      <select disabled={!!setup.capture} id="observation-connected-barrier" value={setup.barrier_observation ?? ""} onChange={(event) => onChange((value) => {
        if (event.target.value) value.barrier_observation = event.target.value;
        else { delete value.barrier_observation; delete value.barrier_destination; delete value.barrier_work; delete value.completion.barrier; }
      })}>
        <option value="">Full observation horizon</option>
        {observations.map((item) => <option key={item.ref.id} value={item.ref.id}>{item.name}</option>)}
        {setup.barrier_observation && !observations.some((item) => item.ref.id === setup.barrier_observation) ? <option value={setup.barrier_observation}>Barrier no longer available</option> : null}
      </select>
      {setup.barrier_observation ? <>
        <label htmlFor="observation-barrier-destination">Barrier destination</label>
        <select id="observation-barrier-destination" value={setup.barrier_destination ?? ""} onChange={(event) => onChange((value) => { value.barrier_destination = event.target.value; })}>
          <option value="">Choose an environment</option>
          {environments.map((item) => <option key={item.ref.id} value={item.ref.id}>{item.name}{item.availability !== "available" ? " · Unavailable" : ""}</option>)}
          {setup.barrier_destination && !environments.some((item) => item.ref.id === setup.barrier_destination) ? <option value={setup.barrier_destination}>Destination no longer available</option> : null}
        </select>
        <label htmlFor="observation-barrier-work">Work step</label>
        <input id="observation-barrier-work" type="text" value={setup.barrier_work ?? ""} onChange={(event) => onChange((value) => { value.barrier_work = event.target.value; })} />
      </> : setup.completion.barrier ? <p role="alert">The saved processing scope is still held. Choose a named barrier, destination and work step before saving, or explicitly choose the full observation horizon.</p> : null}
      {number("observation-connected-horizon", "Observation horizon (ms)", setup.completion.horizon_ms, "completion", (value, n) => { value.completion.horizon_ms = n; })}
      {number("observation-connected-sample", "Sample interval (ms)", setup.completion.sample_ms, "completion", (value, n) => { value.completion.sample_ms = n; })}
      {number("observation-connected-gap", "Maximum sample gap (ms)", setup.completion.max_gap_ms, "completion", (value, n) => { value.completion.max_gap_ms = n; })}
      {number("observation-connected-samples", "Maximum samples", setup.completion.max_samples, "completion", (value, n) => { value.completion.max_samples = n; })}
      {number("observation-connected-records", "Maximum interval records", setup.completion.max_records, "completion", (value, n) => { value.completion.max_records = n; })}
      {number("observation-connected-bytes", "Maximum interval bytes", setup.completion.max_bytes, "completion", (value, n) => { value.completion.max_bytes = n; })}
      <p className="consequence">A run records the baseline before input and observes the full horizon or its declared processing barrier. A standalone collection records one snapshot. Live capture runs only inside its owned connected lifecycle.</p>
    </>}
  </>;
}
