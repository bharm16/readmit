import { useEffect, useRef, useState } from "react";
import { chooseInterfaceSpecDocument, listInterfaceSpecs, readInterfaceRequirements, saveInterfaceSpec, listWholeCatalog, newIntentId,readContextEditorDraft } from "./bindings";
import type { RequestContext, ItemRef, InterfaceRequirementsResult, CatalogItem, InterfacespecDocumentation,InterfaceAssociationEditorDraft } from "./bindings";
import { useProfile } from "./ProfileLibrary";
import { DataTable, type Column } from "./DataTable";
import {useRetainer,RetentionStatus} from "./drafting";
import { FormDialog } from "./layout";
import "./interface-requirements.css";
import "./system-workbench.css";
/** Project-scoped descriptive Spec + supported whole-object local-profile
 * editor. No document is interpreted as a clause or receiver commitment. */
export function InterfaceRequirements({ context, source, identity, occurrence, selector, busy = false, onBack,projectID: suppliedProjectID="" }: {
    context: () => RequestContext;
    projectID?:string;
    source: ItemRef;
    identity: string;
    occurrence: string;
    selector: string;
    busy?: boolean;
    onBack: () => void;
}) {
    const [specs, setSpecs] = useState<CatalogItem[]>([]);
    const [selected, setSelected] = useState<ItemRef | null>(null);
    const [answer, setAnswer] = useState<InterfaceRequirementsResult | null>(null);
    const [problem, setProblem] = useState("");
    const [profiles, setProfiles] = useState<CatalogItem[]>([]);
    const [editing, setEditing] = useState(false);
    const [name, setName] = useState("");
    const [profile, setProfile] = useState("");
    const [documents, setDocuments] = useState<InterfacespecDocumentation[]>([]);
    const [editedProfile, setEditedProfile] = useState<ItemRef | null>(null);
    const owner = JSON.stringify([context().project,source, identity, occurrence, selector]);
    const retainer=useRetainer(owner);
    const [projectID,setProjectID]=useState(suppliedProjectID||context().project_id||"");
    const [formBase,setFormBase]=useState<ItemRef|undefined>(undefined);
    const [working,setWorking]=useState(false);
    const retained=useRef("");
    const [cleanupPending,setCleanupPending]=useState(false);
    const savedIntent=useRef<string|null>(null);
    const profilePin=()=>{const split=profile.lastIndexOf("@");return split<0 ? undefined:{kind:"profile" as const,id:profile.slice(0,split),revision:profile.slice(split+1)};};
    const discardDraft=async()=>{if(!await retainer.dropCurrent()){setProblem("The retained specification draft could not be discarded. Your work is kept.");return;}retained.current="";setWorking(false);setEditing(false);setCleanupPending(false);setProblem("");return true;};
    const current = useRef(owner);
    current.current = owner;
    const serial = useRef(0);
    const currentContext = useRef(context);
    currentContext.current = context;
    const formSerial = useRef(0);
    const live = (held: string, heldContext: () => RequestContext) => current.current === held && currentContext.current === heldContext;
    const mounted = useRef(false);
    const leaveSerial = useRef(0);
    useEffect(() => {
        mounted.current = true;
        return () => { mounted.current = false; ++leaveSerial.current; };
    }, []);
    const keepAndLeave = async (action: () => void) => {
        const held = owner;
        const heldContext = context;
        const mine = ++leaveSerial.current;
        const outcome = working ? await retainer.flush() : null;
        if (!mounted.current || !live(held, heldContext) || leaveSerial.current !== mine) return;
        if (outcome && outcome.state !== "saved") {
            setProblem("The latest specification draft could not be kept. Retry its private save before leaving.");
            return;
        }
        action();
    };
    const refresh = async () => { const held = owner; const result = await listInterfaceSpecs(context()); if (!live(held, context))
        return; if (result.state === "completed") {
        setSpecs(result.items);
        setSelected(old => old || result.items.find(item=>item.availability==="available"&&item.ref.revision)?.ref || null);
    }
    else
        setProblem(result.reason || "Interface specifications could not be read."); };
    useEffect(() => { ++serial.current; setAnswer(null); setSelected(null); setEditedProfile(null); setEditing(false); setDocuments([]); setProblem(""); ++formSerial.current; void refresh(); const held=owner;const mine=formSerial.current;void Promise.all([readContextEditorDraft({context:context(),schema:"readmit-interface-association-editor/v1",source,identity,occurrence,selector}),listWholeCatalog({context:context(),kind:"profile",filter:{}})]).then(([result,choices])=>{if(!live(held,context))return;setProjectID(held=>result.context.project_id||suppliedProjectID||held);if(formSerial.current!==mine)return;setProfiles(choices.page?.items??[]);if(result.state==="completed"&&result.association&&result.retained){const content=result.association;retainer.keepId(result.retained.id);retained.current=JSON.stringify(content);setFormBase(content.base);setSelected(content.base??null);setName(content.name);setProfile(content.profile ? `${content.profile.id}@${content.profile.revision}`:"");setDocuments(content.documents);setWorking(true);setEditing(true);}else if(result.state!=="empty")setProblem(result.reason??"Private specification work could not be read.");}); return () => { ++serial.current; ++formSerial.current; }; }, [owner, context]);
    useEffect(() => { if (!selected)
        return; const held = owner; const mine = ++serial.current; setAnswer(null); setEditedProfile(null); void readInterfaceRequirements({ context: context(), source, identity, occurrence, selector, spec: selected }).then(result => { if (live(held, context) && serial.current === mine) {
        setAnswer(result);
        if (result.state !== "completed")
            setProblem(result.reason || "The selected requirements could not be read.");
    } }); }, [owner, context, selected?.id, selected?.revision]);
    useEffect(()=>{if(!working || !projectID)return;const pin=profilePin();const content:InterfaceAssociationEditorDraft={schema:"readmit-interface-association-editor/v1",project_id:projectID,source,identity,occurrence,selector,...(formBase ? {base:formBase}:{}),name,...(pin ? {profile:pin}:{}),documents};const text=JSON.stringify(content);if(text===retained.current)return;retained.current=text;savedIntent.current=null;retainer.save({id:retainer.currentId(),kind:"interface-association",workspace:context().project,case:"",identity,content_schema:content.schema,content});},[working,projectID,formBase,name,profile,documents]);
    const editor = useProfile({ context, ref: editedProfile || answer?.profile || null, imported: null, shown: Boolean(answer?.profile), busy, onSaved: ref => setEditedProfile(ref), onImport: () => setProblem("Import local profiles through the existing Profiles library, then select their exact revision here."), onImported: () => undefined, ...(answer?.segment && answer.position > 0 ? { initialField: { segment: answer.segment, position: answer.position } } : {}), evaluationCase: source });
    const openEdit = async (create = false, preferred?: ItemRef) => { if(working){setEditing(true);return;}setFormBase(create ? undefined:selected??undefined);const held = owner; const mine = ++formSerial.current; setProblem(""); const result = await listWholeCatalog({ context: context(), kind: "profile", filter: {} }); if (!live(held, context) || formSerial.current !== mine)
        return; if (result.state !== "completed" && result.state !== "empty") {
        setProblem(result.reason || "Local profile revisions could not be read.");
        return;
    } setProjectID(held=>result.context.project_id||suppliedProjectID||held);setProfiles(result.page?.items || []); setName(create ? "" : answer?.spec?.name || ""); setProfile(create ? "" : preferred ? preferred.id + "@" + preferred.revision : answer?.spec ? answer.spec.profile.item + "@" + answer.spec.profile.revision : ""); setDocuments(create ? [] : answer?.spec?.documents || []);setWorking(true); setEditing(true); };
    const publish = async (pin: ItemRef) => {if(cleanupPending)return !!(await discardDraft());if((await retainer.flush()).state!=="saved"){setProblem("The latest specification is not retained. Retry its private save.");return false;}const held = owner; const mine = formSerial.current; const result = await saveInterfaceSpec({ context: context(), ...(formBase ? { base: formBase } : {}), intent_id: savedIntent.current??(savedIntent.current=newIntentId()), name, profile: pin, documents }); if (!live(held, context) || formSerial.current !== mine)
        return false; if (result.state !== "completed" || !result.ref) {
        setProblem(result.reason || "The specification was not saved.");
        return false;
    } setSelected(result.ref);if(!await retainer.dropCurrent()){setCleanupPending(true);setProblem("Specification saved. Its private draft could not be discarded; retry cleanup.");return false;}retained.current="";setWorking(false);setEditing(false);await refresh();return true; };
    const chooseSpec=(id:string)=>{
        const item=specs.find(candidate=>candidate.ref.id===id);
        if(item?.availability==="available"&&item.ref.id===selected?.id&&item.ref.revision===selected?.revision)return;
        ++serial.current;++formSerial.current;setAnswer(null);setEditedProfile(null);
        if(!item||item.availability!=="available"||!item.ref.revision){setSelected(null);setProblem(item?.reason||"This file has no retained catalog revision. Create a specification explicitly.");return;}
        setProblem("");setSelected(item.ref);
    };
    const columns:Column<CatalogItem>[]=[{key:"name",header:"Spec",priority:1,minWidth:11,flex:true,render:item=><div className="requirements-rail-item"><strong>{item.name}</strong><span>{item.ref.revision ? `Revision ${item.ref.revision}`:"Unretained"}</span><span>{item.availability==="available" ? "Retained":item.availability==="unsupported" ? "Unsupported schema":"Unavailable"}</span>{item.reason ? <small>{item.reason}</small>:null}</div>}];
    const fieldRows = answer?.resolution?.segments.flatMap(segment => segment.fields.map(field => ({ ...field, segment: segment.id }))).filter(field => !answer.segment || field.segment === answer.segment && (answer.position === 0 || field.position === answer.position)) || [];
    return <section className="interface-requirements" aria-label="Interface requirements"><header className="requirements-header"><h2>Specification</h2><button type="button" onClick={()=>void keepAndLeave(onBack)}>Return to selected message</button></header>
 <p>{occurrence ? <>Source occurrence {occurrence} · {selector || "Message"}.</> : <>Whole retained interface case; no field occurrence is selected.</>} Documents are descriptive provenance; selected local profiles remain separate executable declarations.</p>
 {problem ? <p role="alert">{problem}</p> : null}
 <div className="requirements-layout"><aside className="requirements-rail"><DataTable label="Interface specifications" rows={specs} rowId={item => item.ref.id} rowLabel={item => item.name} columns={columns} selected={selected?.id || null} onSelect={chooseSpec} onOpen={chooseSpec}/></aside><div className="requirements-main"> <div className="requirements-actions"><button type="button" disabled={busy} onClick={() => { setSelected(null); setAnswer(null); void openEdit(true); }}>Add specification</button><button type="button" disabled={busy || !selected} onClick={() => void openEdit()}>Edit specification association</button></div>

 {answer?.spec ? <><h3>{answer.spec.name} · revision {answer.spec_ref?.revision}</h3><div className="requirements-actions"><button type="button" disabled={busy} onClick={()=>void openEdit()}>Add document</button></div><p>Profile: {answer.spec.profile.id} · {answer.spec.profile.version} · catalog revision {answer.spec.profile.revision} · {answer.spec.profile.sha256}</p><p>{answer.applicability} · {answer.reason}</p><table className="data-table" aria-label="Requirement provenance"><thead><tr><th>Field</th><th>Name</th><th>Requirement</th><th>Source</th></tr></thead><tbody>{fieldRows.map(field => <tr key={field.segment + "/" + field.position}><td>{field.segment}-{field.position}</td><td>{field.name || field.pack_name || "Not declared"}</td><td>{field.usage || "Not declared"}</td><td>{field.usage_origin} · base and local states remain separate</td></tr>)}</tbody></table>
 <h3>Supplied documents</h3>{answer.spec.documents.map(doc => <details key={doc.sha256}><summary>{doc.name}</summary><p>{doc.sha256}</p><pre className="value">{doc.text}</pre></details>)}<p>Attachments and partner narratives do not prove receiver requirements or external deployment.</p>
 {answer.profile ? <section aria-label="Supported profile editor"><header className="requirements-header"><h3>{editor.title}</h3>{editor.actions}</header>{editor.body}</section> : null}
 {editedProfile && editedProfile.revision !== answer.spec.profile.revision ? <><p>The profile edit created revision {editedProfile.revision}; this specification still pins {answer.spec.profile.revision}.</p><button type="button" disabled={busy} onClick={() => { void openEdit(false, editedProfile); }}>Associate an edited profile revision</button></> : null}
 </> : null}
</div></div>
 <FormDialog className="context-authoring-sheet" open={editing} title="Interface specification" submitLabel="Save specification" submitDisabled={!name.trim() || !profile || busy} onClose={() => void keepAndLeave(()=>{++formSerial.current;setEditing(false);})} secondary={<button type="button" onClick={()=>void discardDraft()}>Discard draft</button>} onSubmit={async () => { const pin=profilePin(); if (!pin)
        return { reason: "Choose an exact saved local profile revision." }; return await publish(pin) ? null : { reason: "The specification was not saved." }; }}>
 <RetentionStatus retention={retainer.retention} onRetry={retainer.retry}/>{problem ? <p role="alert">{problem}</p>:null}
 <label htmlFor="spec-name">Name</label><input id="spec-name" value={name} onChange={event => setName(event.target.value)}/><label htmlFor="spec-profile">Local profile revision</label><select id="spec-profile" value={profile} onChange={event => setProfile(event.target.value)}><option value="">Choose local profile</option>{profile && !profiles.some(item=>`${item.ref.id}@${item.ref.revision}`===profile) ? <option value={profile}>Recorded exact profile revision {profile}</option>:null}{profiles.map(item => <option key={item.ref.id} value={item.ref.id + "@" + item.ref.revision}>{item.name} · {item.ref.revision}</option>)}</select>
 <button type="button" disabled={busy} onClick={async () => { const held = owner; const mine = formSerial.current; const chosen = await chooseInterfaceSpecDocument(context()); if (!live(held, context) || formSerial.current !== mine)
        return; if (chosen.state === "completed" && chosen.document)
        setDocuments(old => [...old, chosen.document!]);
    else if (chosen.state !== "cancelled")
        setProblem(chosen.reason || "The document could not be read."); }}>Add document</button>
 {documents.map((doc, index) => <div key={doc.sha256}><span>{doc.name} · {doc.sha256}</span><button type="button" onClick={() => setDocuments(old => old.filter((_, at) => at !== index))}>Remove {doc.name}</button></div>)}<p>Only explicit profile clauses execute. These UTF-8 documents are copied with their byte identities.</p>
 </FormDialog></section>;
}
