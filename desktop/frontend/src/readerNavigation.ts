import { useRef } from "react";
import type { HL7ReferenceSelection, InspectionGridRequest, InspectionResult } from "./bindings";

type Windows = { nodeOffset: number; byteOffset: number; rawOffset: number };
type Position = Windows & { path: string; grid: InspectionGridRequest };
export const initialMessageGrid = (): InspectionGridRequest => ({ expanded: [], show_omitted: false, offset: 0, follow_selection: true });

/** Only user choices become overrides. A response's automatically resolved
 * catalog belongs to that message's edition, not the next message. */
export function useExplicitReaderReference() {
  const held = useRef<{source:string;catalog:string;identity:string;selection:HL7ReferenceSelection|undefined}>({source:"",catalog:"",identity:"",selection:undefined});
  return (source:string,catalog:string|undefined,selection:HL7ReferenceSelection|undefined,identity:string|undefined) => {
    if(held.current.source!==source)held.current={source,catalog:"",identity:"",selection:undefined};
    const current=held.current;
    if(catalog!==undefined){if(catalog!==current.catalog)current.identity="";current.catalog=catalog;}
    if(identity!==undefined && current.catalog)current.identity=identity;
    if(selection!==undefined)current.selection=selection;
    return {
      ...(current.catalog ? {reference_catalog:current.catalog,reference_identity:current.identity} : {}),
      ...(current.selection ? {reference_selection:current.selection} : {}),
    };
  };
}

/** Keeps only navigation, never message values or reveal permission. A changed
 * source identity discards every saved position before the next read. */
export function useReaderNavigation() {
  const held = useRef({ source: "", active: "", positions: new Map<string, Position>() });
  return {
    request(source: string, message: string | number, path: string, grid: InspectionGridRequest | undefined, windows: Windows): Position {
      if (held.current.source !== source) held.current = { source, active: "", positions: new Map() };
      const key = String(message);
      const saved = held.current.positions.get(key);
      const restoring = held.current.active !== key && path === "" && grid === undefined;
      held.current.active = key;
      return {
        ...(restoring && saved ? {nodeOffset:saved.nodeOffset,byteOffset:saved.byteOffset,rawOffset:saved.rawOffset} : windows),
        path: restoring && saved ? saved.path : path,
        grid: grid ?? { ...(saved?.grid ?? initialMessageGrid()), follow_selection: !restoring },
      };
    },
    accept(source: string, message: string | number, result: InspectionResult) {
      const inspection = result.state === "completed" ? result.inspection : null;
      if (source !== held.current.source || !inspection) return;
      const key = String(message);
      held.current.positions.delete(key);
      held.current.positions.set(key, {
        path: inspection.selected.path,
        nodeOffset: inspection.node_offset, byteOffset: inspection.byte_offset, rawOffset: inspection.readable_window?.offset ?? inspection.raw_window?.offset ?? -1,
        grid: { expanded: inspection.grid?.expanded ?? [], show_omitted: inspection.grid?.show_omitted ?? false, offset: inspection.grid?.offset ?? 0, follow_selection: false },
      });
      if (held.current.positions.size > 64) held.current.positions.delete(held.current.positions.keys().next().value!);
    },
  };
}
