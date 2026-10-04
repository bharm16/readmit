import {useState} from "react";
import type {ConnectedSuiteRunOptions} from "./bindings";

export function blankConnectedRunnerOptions():ConnectedSuiteRunOptions {return {runner_config:"",authority:"",promotion:"",promotion_identity:"",revision:"",instance:""};}
export function completeConnectedRunnerOptions(value:ConnectedSuiteRunOptions,schedule=false):boolean {
 return value.runner_config.trim()!=="" && value.authority.trim()!=="" && value.promotion.trim()!=="" && /^[a-f0-9]{64}$/.test(value.promotion_identity) && value.revision.trim()!=="" && (schedule || value.instance.trim()!=="");
}
/** References to customer-installed authority. Typing them grants no authority;
 * the existing facade verifies every retained pin before dispatch. */
export function ConnectedRunnerOptionsFields({value,onChange,schedule=false,agent=false}:{value:ConnectedSuiteRunOptions;onChange:(next:ConnectedSuiteRunOptions)=>void;schedule?:boolean;agent?:boolean}) {
 const fields:{key:keyof ConnectedSuiteRunOptions;label:string}[]=[{key:"runner_config",label:"Runner configuration"},{key:"authority",label:"Installed authority"},{key:"promotion",label:"Approved promotion"},{key:"promotion_identity",label:"Promotion SHA-256"},{key:"revision",label:"Target revision"},...(schedule ? []:[{key:"instance" as const,label:"Dispatch identity"}])];
 return <fieldset className="checks"><legend>Connected runner</legend><p>{agent ? "Paths belong to the CI host.":"Artifact references belong to this project."} Existing installed authority and its exact promotion must verify. {schedule ? "The scheduler owns each occurrence's dispatch identity.":"Keep one dispatch identity unchanged across retries."}</p>{fields.map(field=><div key={field.key} className="form-field"><label htmlFor={`connected-${field.key}`}>{field.label}</label><input id={`connected-${field.key}`} value={value[field.key]} onChange={event=>onChange({...value,[field.key]:event.target.value})}/></div>)}</fieldset>;
}
export function ConnectedRunnerOptionsEditor({value,onApply,onDirty}:{value:ConnectedSuiteRunOptions|undefined;onApply:(options:ConnectedSuiteRunOptions)=>void;onDirty:()=>void}) {
 const [draft,setDraft]=useState(value??blankConnectedRunnerOptions());
 return <><ConnectedRunnerOptionsFields value={draft} onChange={next=>{setDraft(next);onDirty();}}/><button type="button" disabled={!completeConnectedRunnerOptions(draft)} onClick={()=>onApply({...draft})}>Review runner dispatch</button></>;
}
