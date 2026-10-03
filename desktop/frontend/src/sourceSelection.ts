import { useEffect, useRef, useState } from "react";
import { readMessages, type MessageRow } from "./bindings";
import { NO_QUERY } from "./Messages";

type Source = { root:string; entry:string; identity:string };
type Selection = { owner:string; key:string; rows:MessageRow[]; loading:boolean; reason:string|null };

/** Checked identities and row metadata belong to one verified source. Filters
 * and pages change its presentation; they cannot narrow the authored scope. */
export function useSourceSelection(source:Source|null, checked:ReadonlySet<string>,visible:MessageRow[]) {
 const owner=source ? JSON.stringify([source.root,source.entry,source.identity]):"";
 const key=JSON.stringify([...checked]);
 const cache=useRef<{owner:string;rows:Map<string,MessageRow>}>({owner:"",rows:new Map()});
 const [selection,setSelection]=useState<Selection|null>(null);
 const turns=useRef(0);
 useEffect(()=>{
  const turn=++turns.current;
  if(!source) {cache.current={owner:"",rows:new Map()};setSelection(null);return;}
  if(cache.current.owner!==owner)cache.current={owner,rows:new Map()};
  for(const row of visible)cache.current.rows.set(row.id,row);
  const ids=[...checked];
  const complete=()=>setSelection({owner,key,rows:ids.flatMap(id=>{const row=cache.current.rows.get(id);return row ? [row]:[];}),loading:false,reason:null});
  if(ids.every(id=>cache.current.rows.has(id))) {complete();return;}
  setSelection({owner,key,rows:[],loading:true,reason:null});
  let live=true;
  void(async()=>{
   let offset=0;
   while(offset<ids.length) {
    const answer=await readMessages({workspace:source.root,case:source.entry,identity:source.identity,query:NO_QUERY,sort:"",offset,limit:0,occurrences:ids});
    if(!live || turn!==turns.current || cache.current.owner!==owner)return;
    if(answer.state!=="completed" && answer.state!=="empty") {setSelection({owner,key,rows:[],loading:false,reason:answer.reason??"The selected source occurrences could not be verified."});return;}
    for(const row of answer.rows)cache.current.rows.set(row.id,row);
    offset+=answer.rows.length;
    if(answer.rows.length===0 || offset>=answer.matched)break;
   }
   if(!live || turn!==turns.current)return;
   if(ids.some(id=>!cache.current.rows.has(id))) {setSelection({owner,key,rows:[],loading:false,reason:"Some selected occurrences are unavailable. The checked scope is kept; inspect its source before proceeding."});return;}
   complete();
  })();
  return()=>{live=false;};
 },[owner,key,visible]); // exact source and checked order fence every asynchronous read
 return selection?.owner===owner && selection.key===key ? selection : {owner,key,rows:[],loading:checked.size>0,reason:null};
}
