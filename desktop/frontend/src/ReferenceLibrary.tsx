import { useEffect, useRef, useState } from "react";
import type { ReferenceEdition, ReferenceLibraryResult } from "./bindings";
import { chooseInspectionPath, installReferenceCatalog, installReferenceLibrary, readReferenceLibrary } from "./bindings";
import { Modal } from "./layout";

export function ReferenceLibrary({ open, messageEdition, onClose, onSelect }: {
  open:boolean;
  messageEdition:string;
  onClose:()=>void;
  onSelect:(edition:ReferenceEdition|null)=>Promise<void>;
}) {
  const [library,setLibrary]=useState<ReferenceLibraryResult|null>(null);
  const [edition,setEdition]=useState("");
  const [loading,setLoading]=useState(false);
  const [problem,setProblem]=useState("");
  const generation=useRef(0);
  useEffect(()=>{
    const owner=++generation.current;
    if(!open)return;
    setEdition("");setProblem("");setLoading(true);
    void readReferenceLibrary().then(answer=>{
      if(generation.current!==owner)return;
      setLibrary(answer);setProblem(answer.state==="completed"?"":answer.reason||"The reference library cannot be read.");setLoading(false);
    });
    return ()=>{++generation.current;};
  },[open]);

  const install=async(folder:boolean)=>{
    const owner=generation.current;setLoading(true);setProblem("");
    const chosen=await chooseInspectionPath(folder?"reference-library":"reference-catalog");
    if(generation.current!==owner)return;
    if(chosen.state!=="completed"||!chosen.path){setLoading(false);if(chosen.state!=="cancelled")setProblem(chosen.reason||"No reference source was selected.");return;}
    const answer=folder?await installReferenceLibrary(chosen.path):await installReferenceCatalog(chosen.path);
    if(generation.current!==owner)return;
    setLibrary(answer);setLoading(false);
    if(answer.state!=="completed"){setProblem(answer.reason||"The reference source could not be installed.");return;}
    await onSelect(null);
  };

  const useEdition=async()=>{
    const selected=edition ? library?.editions.find(item=>item.edition===edition):null;
    if(edition&&!selected?.path)return;
    setLoading(true);
    await onSelect(selected??null);
    setLoading(false);onClose();
  };

  return <Modal open={open} title="HL7 reference library" onClose={onClose}>
    {loading?<p role="status">Reading reference library…</p>:null}
    {problem?<p role="alert">{problem}</p>:null}
    <label htmlFor="reference-edition">Reference version</label>
    <select id="reference-edition" value={edition} disabled={loading} onChange={event=>setEdition(event.target.value)}>
      <option value="">Use message version{messageEdition?` (${messageEdition})`:""}</option>
      {library?.editions.map(item=><option key={item.edition} value={item.edition} disabled={!item.path}>HL7 {item.edition}{item.path?"":" · Not installed"}</option>)}
    </select>
    <p>The message version stays as received. A different reference version is shown explicitly.</p>
    <div className="toolbar">
      <button type="button" disabled={loading} onClick={()=>void install(true)}>Install library…</button>
      <button type="button" disabled={loading} onClick={()=>void install(false)}>Install catalog…</button>
    </div>
    <button type="button" className="primary" disabled={loading||library?.state!=="completed"} onClick={()=>void useEdition()}>Use reference</button>
  </Modal>;
}
