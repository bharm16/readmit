import { useEffect, useId, useRef, useState } from "react";
import type { Hl7referenceRecord } from "./bindings";
import { useViewState } from "./viewstate";
import columnsAsset from "./assets/workbench/columns-2.svg";

export const referenceColumns: { name: string; attribute: keyof Pick<Hl7referenceRecord, "datatype" | "optionality" | "length" | "conformance_length" | "repetition" | "item" | "table" | "section">; width: number }[] = [
  { name: "Type", attribute: "datatype", width: 4 }, { name: "Opt", attribute: "optionality", width: 3 },
  { name: "Len", attribute: "length", width: 4 }, { name: "C-Len", attribute: "conformance_length", width: 4.5 },
  { name: "Rep", attribute: "repetition", width: 3 }, { name: "Item#", attribute: "item", width: 4.5 },
  { name: "Tbl", attribute: "table", width: 4 }, { name: "Sect", attribute: "section", width: 6 },
];
type ReaderColumnLayout = { visible: string[]; name: number; value: number; fixedColumn?: "name" | "value" };
export const useReaderColumns = () => useViewState("reader-columns", (): ReaderColumnLayout => ({ visible: ["Type", "Opt"], name: 17.5, value: 20 }));

export function ReaderColumns() {
  const [columns, setColumns] = useReaderColumns();
  const [open, setOpen] = useState(false);
  const container = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const id = useId();
  const close = () => { setOpen(false); trigger.current?.focus(); };
  useEffect(() => {
    if (!open) return;
    container.current?.querySelector<HTMLInputElement>('input:not(:disabled)')?.focus();
    const outside = (event: PointerEvent) => { if (event.target instanceof Node && !container.current?.contains(event.target)) setOpen(false); };
    document.addEventListener("pointerdown", outside);
    return () => document.removeEventListener("pointerdown", outside);
  }, [open]);
  return <div ref={container} className="reader-columns-control" onKeyDown={event => { if (event.key === "Escape" && open) { event.stopPropagation(); close(); } }}>
    <button ref={trigger} type="button" aria-label="Columns" aria-expanded={open} aria-controls={id} onClick={() => setOpen(!open)}><img src={columnsAsset} className="workbench-icon" alt="" />Columns</button>
    {open ? <div id={id} role="dialog" aria-label="Columns" className="reader-columns-popover"><h4>Columns</h4><p>Path, Name and Value stay visible.</p>
      {["Path", "Name", ...referenceColumns.map(column => column.name), "Value"].map(name => {
        const locked = name === "Path" || name === "Name" || name === "Value";
        return <label key={name}><input type="checkbox" disabled={locked} checked={locked || columns.visible.includes(name)} onChange={event => setColumns(current => ({ ...current, visible: event.target.checked ? [...current.visible, name] : current.visible.filter(column => column !== name) }))} /><span>{name}</span>{locked ? <small>Always shown</small> : null}</label>;
      })}<button type="button" onClick={close}>Done</button></div> : null}
  </div>;
}
