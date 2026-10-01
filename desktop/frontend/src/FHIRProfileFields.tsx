import { useEffect, useState } from "react";
import { listWholeCatalog, type CatalogItem, type FHIRProfileAvailability, type FHIRProfileDefinition, type ProfilePackageOrigin, type RequestContext } from "./bindings";

/** Saved FHIR requirements inside the existing profile editor. Listing
 * installed metadata and saved connections grants no validation authority. */
export function FHIRProfileFields({ value, context, availability, onChange }: { value: FHIRProfileDefinition; context: () => RequestContext; availability?: FHIRProfileAvailability | undefined; onChange: (value: FHIRProfileDefinition) => void }) {
  const [environments, setEnvironments] = useState<CatalogItem[]>([]);
  useEffect(() => {
    let current = true;
    void listWholeCatalog({ context: context(), kind: "environment", filter: {} }).then((answer) => {
      if (current) setEnvironments((answer.page?.items ?? []).filter((item) => item.availability === "available" && item.summary.environment?.protocol === "fhir-r4"));
    });
    return () => { current = false; };
  }, [context]);
  const patch = (change: Partial<FHIRProfileDefinition>) => onChange({ ...value, ...change });
  const validator = value.validator ? `${value.validator.id}@${value.validator.revision ?? ""}` : "";
  const packages = availability?.packages ?? [];
  const canonicals = availability?.profiles ?? [];
  const packageKey = (entry: { id: string; version: string }) => `${entry.id}@${entry.version}`;
  const canonicalKey = (entry: FHIRProfileDefinition["profiles"][number]) => `${entry.url}|${entry.version}|${entry.sha256}`;
  return <>
    <label>Resource type<input type="text" required value={value.resource_type} onChange={(event) => patch({ resource_type: event.target.value })} /></label>
    <label>Validator connection<select value={validator} onChange={(event) => {
      const selected = environments.find((item) => `${item.ref.id}@${item.ref.revision ?? ""}` === event.target.value);
      const { validator: _validator, capability: _capability, ...held } = value;
      // A changed validator must be pinned again by Go on Save. It cannot
      // inherit a capability identity from another connection revision.
      onChange({ ...held, ...(selected ? { validator: selected.ref } : {}) });
    }}>
      <option value="">Unavailable</option>
      {validator && !environments.some((item) => `${item.ref.id}@${item.ref.revision ?? ""}` === validator) ? <option value={validator}>Saved connection · v{value.validator?.revision}</option> : null}
      {environments.map((item) => <option key={item.ref.id} value={`${item.ref.id}@${item.ref.revision ?? ""}`}>{item.name} · v{item.ref.revision ?? "—"}</option>)}
    </select></label>
    {(["terminology", "invariants"] as const).map((key) => <label key={key}>{key === "terminology" ? "Terminology" : "Invariants"}<select value={value.requirements[key]} onChange={(event) => patch({ requirements: { ...value.requirements, [key]: event.target.value } })}>
      <option value="not-requested">Not requested</option><option value="required">Required</option>
    </select></label>)}
    <fieldset><legend>Failure severities</legend>{["fatal", "error", "warning", "information"].map((severity) => <label key={severity} className="check"><input type="checkbox" checked={value.requirements.fail_severities.includes(severity)} disabled={severity === "fatal" || severity === "error"} onChange={(event) => patch({ requirements: { ...value.requirements, fail_severities: event.target.checked ? [...value.requirements.fail_severities, severity] : value.requirements.fail_severities.filter((held) => held !== severity) } })} />{severity}</label>)}</fieldset>
    <fieldset><legend>Package requirements</legend>
      {value.packages.map((entry, at) => <fieldset key={at}><legend>Package {at + 1}</legend>
        {packages.some((held) => packageKey(held) === packageKey(entry)) ? <label>Package<select value={packageKey(entry)} onChange={(event) => {
          const selected = packages.find((held) => packageKey(held) === event.target.value);
          if (selected) patch({ packages: value.packages.map((held, i) => i === at ? { id: selected.id, version: selected.version } : held) });
        }}>{packages.map((held) => <option key={packageKey(held)} value={packageKey(held)}>{held.id} · {held.version}</option>)}</select></label> : <p>{entry.id} · {entry.version} · Retained pin, unavailable in the selected metadata</p>}
        <button type="button" onClick={() => patch({ packages: value.packages.filter((_, i) => i !== at) })}>Remove package {at + 1}</button>
      </fieldset>)}
      <button type="button" disabled={packages.length === 0} onClick={() => { const first = packages[0]; if (first) patch({ packages: [...value.packages, { id: first.id, version: first.version }] }); }}>Add package requirement</button>
    </fieldset>
    <fieldset><legend>Canonical profiles</legend>
      {value.profiles.map((entry, at) => <fieldset key={at}><legend>Canonical {at + 1}</legend>
        {canonicals.some((held) => canonicalKey(held) === canonicalKey(entry)) ? <label>Canonical profile<select value={canonicalKey(entry)} onChange={(event) => {
          const selected = canonicals.find((held) => canonicalKey(held) === event.target.value);
          if (selected) patch({ profiles: value.profiles.map((held, i) => i === at ? selected : held) });
        }}>{canonicals.map((held) => <option key={canonicalKey(held)} value={canonicalKey(held)}>{held.url} · {held.version} · {held.package.id}</option>)}</select></label> : <p>{entry.url} · {entry.version} · Retained pin, unavailable in the selected metadata</p>}
        <p>Package {entry.package.id} · {entry.package.version}</p>
        <button type="button" onClick={() => patch({ profiles: value.profiles.filter((_, i) => i !== at) })}>Remove canonical {at + 1}</button>
      </fieldset>)}
      <button type="button" disabled={canonicals.length === 0} onClick={() => { const first = canonicals[0]; if (first) patch({ profiles: [...value.profiles, first] }); }}>Add canonical profile</button>
    </fieldset>
    {!availability || packages.length === 0 && canonicals.length === 0 ? <p className="row-reason">Select a saved validator connection with installed metadata to add pins. Retained pins stay intact.</p> : null}
  </>;
}

export function FHIRProfileOrigin({ value, onChange }: { value?: ProfilePackageOrigin | undefined; onChange: (value: ProfilePackageOrigin) => void }) {
  if (!value) return <button type="button" onClick={() => onChange({ schema: "readmit-profile-origin/v1", source_format: "readmit-fhir-profile/v1", source: "", revision: "", license: "", notice: "", mapping_limitations: "", review_reference: "" })}>Add source attribution</button>;
  const labels = { source: "Source", revision: "Source revision", license: "License", notice: "Attribution notice", mapping_limitations: "Mapping limitations", review_reference: "Review reference" };
  const format = value.source_format === "readmit-fhir-profile/v1" ? "FHIR profile" : value.source_format === "manual-external-mapping" ? "Manual external mapping" : "Retained source format";
  return <fieldset><legend>Source attribution</legend><p>{format}</p>{(Object.keys(labels) as (keyof typeof labels)[]).map((key) => <label key={key}>{labels[key]}<input type="text" value={value[key]} onChange={(event) => onChange({ ...value, [key]: event.target.value })} /></label>)}</fieldset>;
}
