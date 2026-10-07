import { initialMessageGrid, useReaderNavigation } from "./readerNavigation";
import type { InspectionGridRequest } from "./bindings";
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import searchAsset from "./assets/workbench/search.svg";
import moreAsset from "./assets/workbench/more.svg";
import "./raw.css";
import {
  chooseInspectionPath,
  inspectFileMessage,
  listFileMessages,
  readFileBytes,
  saveFileCopy,
  readReferenceCatalog,
  readHL7ReferenceSelection,
  type FileBytesResult,
  type FileMessage,
  type FileMessagesResult,
  type InspectionResult,
  type HL7ReferenceSelection,
  type RoundTripResult,
} from "./bindings";
import { DataTable, type Column } from "./DataTable";
import { EmptyState, FormDialog, Menu, Modal, Reveal, ValueRows } from "./layout";
import { IconButton } from "./IconButton";
import { HexTable, MessageReader } from "./Inspector";
import { typeLabel } from "./Messages";
import { useLifecycle } from "./lifecycle";
import { useViewState } from "./viewstate";

type Framing = "auto" | "raw" | "mllp";
type Terminator = "auto" | "cr" | "lf" | "crlf";
type FileNavigation = Pick<import("./bindings.gen").ViewNavigation, "phi_masked" | "file" | "file_identity" | "file_message" | "file_messages" | "file_format" | "file_terminator" | "field_path" | "node_offset" | "reference_path" | "reference_identity" | "reference_edition" | "reference_selection">;

const FRAMINGS: { value: Framing; label: string }[] = [
  { value: "auto", label: "Auto" },
  { value: "raw", label: "Raw HL7" },
  { value: "mllp", label: "MLLP" },
];

const TERMINATORS: { value: Terminator; label: string }[] = [
  { value: "auto", label: "Auto" },
  { value: "cr", label: "CR" },
  { value: "lf", label: "LF" },
  { value: "crlf", label: "CRLF" },
];

function fileName(path: string): string {
  const parts = path.split(/[\\/]/);
  return parts[parts.length - 1] || path;
}

/** A framing or terminator as the Format sheet names it, and whether it was detected. */
function formatLabel(choices: { value: string; label: string }[], value: string, selection: string): string {
  if (!value) return "—";
  const label = choices.find((choice) => choice.value === value)?.label ?? value.toUpperCase();
  return selection === "detected" ? `${label} (detected)` : label;
}

const messageType = (row: FileMessage) => typeLabel({ kind: "message", code: row.message_code, trigger: row.trigger_event });

/** Tools → Inspect file: a standalone message file read through the same
 * reader as a case's messages, with no project. The file is read, never
 * imported or changed. The page's header, body and details pane are owned by
 * the window; this hook supplies each. */
export function useFileReader({ busy, request, onCancelled, onOpened, onNavigation }: { busy: boolean; request: number; onCancelled?: () => void; onOpened?: () => void; onNavigation?: (navigation: FileNavigation | null) => void }) {
  // A chooser cancelled before any file was open leaves the person where they
  // chose Inspect file.
  const cancelled = useRef(onCancelled);
  cancelled.current = onCancelled;
  const opened = useRef(onOpened);
  opened.current = onOpened;
  const [file, setFile] = useState("");
  const [openRevision, setOpenRevision] = useState(0);
  const [framing, setFraming] = useState<Framing>("auto");
  const [terminator, setTerminator] = useState<Terminator>("auto");
  const [listing, setListing] = useState<FileMessagesResult | null>(null);
  const [bytes, setBytes] = useState<FileBytesResult | null>(null);
  const [bytesShown, setBytesShown] = useState(false);
  const [messageSearch,setMessageSearch]=useState("");
  const [messageGrouping,setMessageGrouping]=useState<"all"|"type">("all");
  const [referenceRequest,setReferenceRequest]=useState(0);
  const [referenceLibraryRequest,setReferenceLibraryRequest]=useState(0);
  const [referenceResetRequest,setReferenceResetRequest]=useState(0);
  const [selected, setSelected] = useState<number | null>(null);
 const [checked,setChecked]=useState<Set<string>>(new Set());
 const checkedOwner=useRef("");
  const [inspection, setInspection] = useState<InspectionResult | null>(null);
  const [revealed, setRevealed] = useState(true);
  const [formatting, setFormatting] = useState(false);
  const [info, setInfo] = useState(false);
  const [copied, setCopied] = useState<RoundTripResult | null>(null);
  const [chosen, setChosen] = useState<string | null>(null);
  const { running, run } = useLifecycle<"choosing" | "reading" | "inspecting" | "copying">({ window: true });
  const disabled = busy || running !== null;
  const readerNavigation = useReaderNavigation();
  const where = useRef({ path: "", nodeOffset: 0, byteOffset: -1 });
  const referenceCatalog = useRef("");
  const referenceIdentity = useRef("");
  const referenceSelection = useRef<HL7ReferenceSelection | undefined>(undefined);
  const restoredFile = useRef("");
  const automaticSingleSelection = useRef("");

  const navigation: FileNavigation | null = file && listing?.sha256 ? {
    ...(!revealed ? {phi_masked:true} : {}),
    file, file_identity: listing.sha256, file_message: selected ?? 0, file_messages: [...checked].map(Number), file_format: framing, file_terminator: terminator,
    ...(inspection?.inspection ? { field_path: where.current.path, node_offset: where.current.nodeOffset } : {}),
    ...(referenceSelection.current ? { reference_selection: referenceSelection.current } : {}),
    ...(referenceCatalog.current && referenceIdentity.current ? { reference_path: referenceCatalog.current, reference_identity: referenceIdentity.current, ...(inspection?.inspection?.reference?.edition ? { reference_edition: inspection.inspection.reference.edition } : {}) } : {}),
  } : null;
  const navigationText = JSON.stringify(navigation);
  useEffect(() => { onNavigation?.(navigation); }, [navigationText, onNavigation]); // identities and a protective mask preference; no values or reveal authorization

  const restore = useCallback(async (saved: import("./bindings.gen").ViewNavigation): Promise<boolean> => {
    if (!saved.file || !saved.file_identity) return false;
    const format = FRAMINGS.find((choice) => choice.value === saved.file_format)?.value ?? "auto";
    const ending = TERMINATORS.find((choice) => choice.value === saved.file_terminator)?.value ?? "auto";
    let accepted = false;
    await run("reading", async (current) => {
      const answer = await listFileMessages({ file: saved.file!, format, terminator: ending, offset: 0, limit: 0 });
      if (!current()) return;
      if (answer.state !== "completed" || answer.sha256 !== saved.file_identity) {
        setChosen("The previous message file changed or is unavailable. Open the source again to inspect it.");
        return;
      }
      const catalog = saved.reference_path ?? "";
      if (saved.reference_path) {
        const reference = await readReferenceCatalog(saved.reference_path);
        if (!current()) return;
        if (reference.reference?.identity !== saved.reference_identity) setChosen("The previous reference catalog changed or is unavailable. Select it again explicitly.");
      }
      const selection = saved.reference_selection;
      if (selection) {
        const checked = await readHL7ReferenceSelection(selection);
        if (!current()) return;
        if (checked.state !== "completed" || checked.overlay?.status === "not_available") setChosen("A previous profile or documentation file changed or is unavailable. Select it again explicitly; the source stays inspectable.");
      }
      const result = await inspectFileMessage({ grid: initialMessageGrid(), file: saved.file!, format, terminator: ending, expect: answer.sha256, message: saved.file_message ?? 0, path: saved.field_path ?? "", node_offset: saved.node_offset ?? 0, byte_offset: -1, raw_offset: -1, reveal: true, ...(saved.phi_masked ? {mask_phi:true} : {}), ...(catalog ? { reference_catalog: catalog, reference_identity: saved.reference_identity ?? "" } : {}), ...(selection ? { reference_selection: selection } : {}) });
      if (!current()) return;
      restoredFile.current = saved.file!;
      referenceCatalog.current = catalog;
      referenceIdentity.current = catalog ? saved.reference_identity ?? "" : "";
      referenceSelection.current = selection;
      where.current = { path: saved.field_path ?? "", nodeOffset: saved.node_offset ?? 0, byteOffset: -1 };
      setFile(saved.file!); setFraming(format); setTerminator(ending); setListing(answer);
      setChecked(new Set((saved.file_messages??[]).map(String)));checkedOwner.current=answer.sha256;
      setSelected(saved.file_message ?? 0); setInspection(result); setRevealed(!saved.phi_masked); setBytesShown(false);
      accepted = true;
    });
    return accepted;
  }, [run]);

  const list = useCallback(
    async (path: string, format: Framing, ending: Terminator) => {
      await run("reading", async (current) => {
        setSelected(null);
        setListing(null);
        setInspection(null);
        setBytes(null);
        setCopied(null);
        const answer = await listFileMessages({ file: path, format, terminator: ending, offset: 0, limit: 0 });
        if(!current())return;
        setListing(answer);
 if(answer.state==="completed" && checkedOwner.current!==answer.sha256) {checkedOwner.current=answer.sha256;setChecked(new Set());}
        setBytesShown(false);
        if (answer.state !== "completed" && answer.sha256) {
          const bytes=await readFileBytes({ file: path, expect: answer.sha256, offset: 0, reveal: false });
          if(current())setBytes(bytes);
        }
      });
    },
    [run],
  );

  const inspect = useCallback(
    async (message: number, path: string, nodeOffset: number, byteOffset: number, reveal: boolean, rawOffset = -1, catalogPath?: string, selection?: HL7ReferenceSelection, catalogIdentity?: string, grid?: InspectionGridRequest): Promise<InspectionResult | null> => {
      if (!listing?.sha256) return null;
      if (catalogPath !== undefined) {
        if (catalogPath !== referenceCatalog.current) referenceIdentity.current = catalogIdentity ?? "";
        referenceCatalog.current = catalogPath;
      }
      if (catalogIdentity !== undefined) referenceIdentity.current = catalogIdentity;
      if (selection !== undefined) referenceSelection.current = selection;
      const sourceKey = JSON.stringify([file, framing, terminator, listing.sha256]);
      const position = readerNavigation.request(sourceKey, message, path, grid,{nodeOffset,byteOffset,rawOffset});
      let answer: InspectionResult | null = null;
      await run("inspecting", async (current) => {
        const result = await inspectFileMessage({
          file,
          format: framing,
          terminator,
          expect: listing.sha256,
          message,
          path: position.path,
          grid: position.grid,
          node_offset: position.nodeOffset,
          byte_offset: position.byteOffset,
          raw_offset: position.rawOffset,
          reveal: true,
          ...(!reveal ? {mask_phi:true} : {}),
          ...(referenceCatalog.current ? { reference_catalog: referenceCatalog.current } : {}),
          ...(referenceIdentity.current ? { reference_identity: referenceIdentity.current } : {}),
          ...(referenceSelection.current ? { reference_selection: referenceSelection.current } : {}),
        });
        answer = result;
        if (!current()) return;
        // A field that is not there leaves the message as it was.
        if (result.state === "completed" || path === "") {
          if (!referenceIdentity.current && result.inspection?.reference?.identity) referenceIdentity.current = result.inspection.reference.identity;
          readerNavigation.accept(sourceKey, message, result);
          setInspection(result);
          where.current = { path: result.inspection?.selected.path ?? path, nodeOffset, byteOffset };
        }
      });
      return answer;
    },
    [file, framing, listing, run, terminator],
  );

  const open = useCallback(async () => {
    await run("choosing", async (current) => {
      setChosen(null);
      const answer = await chooseInspectionPath("file");
      if (!current()) return;
      if (answer.state === "completed" && answer.path) {
        referenceCatalog.current = "";
        referenceIdentity.current = "";
        referenceSelection.current = undefined;
        automaticSingleSelection.current = "";
        setFile(answer.path);
        setOpenRevision(revision => revision + 1);
        setFraming("auto");
        setTerminator("auto");
        setRevealed(true);
        opened.current?.();
      } else if (answer.state !== "cancelled") {
        setChosen(answer.reason ?? "The file could not be opened.");
      } else if (!openFile.current) {
        cancelled.current?.();
      }
    });
  }, [run]);

  const openFile = useRef(file);
  openFile.current = file;

  // A file named elsewhere, such as a project's own file, is read without
  // asking the host for one.
  const openPath = useCallback(
    (path: string) => {
      setChosen(null);
      setFraming("auto");
      setTerminator("auto");
      setRevealed(true);
      if (path === file) void list(path, "auto", "auto");
      else {
        referenceCatalog.current = "";
        referenceIdentity.current = "";
        referenceSelection.current = undefined;
        setFile(path);
      }
    },
    [file, list],
  );

  // A new file is read as soon as it is chosen.
  useEffect(() => {
    if (restoredFile.current === file) { restoredFile.current = ""; return; }
    if (file) void list(file, framing, terminator);
    // Only a new file starts a read; a format change applies from its sheet.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [file, openRevision]);

  // Tools → Inspect file goes straight to the host's Open dialog.
  const asked = useRef(0);
  useEffect(() => {
    if (request > asked.current) {
      asked.current = request;
      void open();
    }
  }, [open, request]);

  // A single message opens straight into the reader.
  const single = listing?.state === "completed" && listing.total === 1;
  useEffect(() => {
    if (!single) return;
    const owner = `${file}|${listing?.sha256 ?? ""}|${framing}|${terminator}`;
    if (selected !== null) automaticSingleSelection.current = owner;
    else if (automaticSingleSelection.current !== owner) {
      automaticSingleSelection.current = owner;
      setSelected(0);
      void inspect(0, "", 0, -1, revealed);
    }
  }, [file, framing, inspect, listing?.sha256, revealed, selected, single, terminator]);

  const copy = async () => {
    if (!listing?.sha256) return;
    await run("copying", async () => {
      setCopied(null);
      const destination = await chooseInspectionPath("copy-destination", file);
      if (destination.state !== "completed" || !destination.path) {
        if (destination.state !== "cancelled") setCopied({ state: destination.state, reason: destination.reason ?? "No destination was chosen." });
        return;
      }
      setCopied(await saveFileCopy({ file, format: framing, terminator, expect: listing.sha256, destination: destination.path }));
    });
  };

  const reveal = (next: boolean) => {
    setRevealed(next);
    if (selected !== null) void inspect(selected, where.current.path, where.current.nodeOffset, where.current.byteOffset, next);
  };

  const loadMore=async()=>{
 if(!listing || listing.state!=="completed" || listing.rows.length>=listing.total)return;
 const held=listing;
 await run("reading",async(current)=>{
 const answer=await listFileMessages({file,format:framing,terminator,offset:held.rows.length,limit:0});
 if(!current())return;
 if(answer.sha256!==held.sha256) {setChosen("The source changed while paging. The previous selection is kept; reopen the file explicitly.");return;}
 if(answer.state!=="completed") {setChosen(answer.reason??"Later messages could not be read. The previous selection is kept.");return;}
 setListing({...held,rows:[...held.rows,...answer.rows]});
 });
 };

  const reader = (
    <MessageReader
      referenceCatalog={referenceCatalog.current}
      referenceIdentity={referenceIdentity.current}
      {...(referenceSelection.current ? { referenceSelection: referenceSelection.current } : {})}
      toolbarReference
      referenceRequest={referenceRequest}
      referenceLibraryRequest={referenceLibraryRequest}
      referenceResetRequest={referenceResetRequest}
      result={inspection}
      valuesHidden={!revealed && !inspection?.inspection?.phi_masked}
      loading={running === "inspecting"}
      busy={disabled}
      onInspect={(path, nodeOffset, byteOffset, rawOffset, catalogPath?: string, selection?: HL7ReferenceSelection, catalogIdentity?: string, grid?: InspectionGridRequest) => (selected === null ? Promise.resolve(null) : inspect(selected, path, nodeOffset, byteOffset, revealed, rawOffset, catalogPath, selection, catalogIdentity, grid))}
      onReveal={reveal}
      onClose={() => { setSelected(null); setInspection(null); }}
    />
  );

  const columns: Column<FileMessage>[] = selected !== null ? [
    { key: "message", header: "Messages", priority: 1, minWidth: 6, flex: true, render: (row) => <span className="message-browser-entry"><strong>{messageType(row)}</strong><span>Message {row.index + 1} · {row.end - row.start} bytes</span></span> },
  ] : [
    { key: "message", header: "Message", priority: 1, minWidth: 6, render: (row) => String(row.index + 1) },
    { key: "type", header: "Type", priority: 1, minWidth: 8, render: messageType },
    { key: "bytes", header: "Bytes", priority: 2, minWidth: 6, render: (row) => String(row.end - row.start) },
  ];

  let body: ReactNode;
  if (!file) {
    body = (
      <EmptyState
        title="No file open"
        action={
          <button type="button" className="primary" disabled={disabled} onClick={() => void open()}>
            Open file
          </button>
        }
      />
    );
  } else if (!listing) {
    body = <p aria-live="polite">Reading…</p>;
  } else if (listing.state !== "completed") {
    body = (
      <div className="file-refused">
        <p role="alert">{listing.reason ?? "This file could not be read."}</p>
        <div>
          <button type="button" disabled={disabled} onClick={() => setFormatting(true)}>
            Change format
          </button>
        </div>
        {bytes && bytes.state === "completed" && bytesShown ? (
          <HexTable
            rows={bytes.rows}
            selection={null}
            total={bytes.bytes}
            offset={bytes.offset}
            busy={disabled}
            onPage={(offset) => void readFileBytes({ file, expect: listing.sha256, offset, reveal: true }).then(setBytes)}
          />
        ) : null}
        {bytes && bytes.state === "completed" ? (
          <Reveal
            revealed={bytesShown}
            disabled={disabled}
            onToggle={(next) => {
              setBytesShown(next);
              void readFileBytes({ file, expect: listing.sha256, offset: bytes.offset, reveal: next }).then(setBytes);
            }}
          />
        ) : null}
      </div>
    );
  } else {
    body = (
      <div className={selected !== null ? "file-reader-browser" : "file-reader-list"}>
      {selected !== null ? <><label className="file-browser-search"><img className="workbench-icon" src={searchAsset} alt="" /><input aria-label="Search messages" placeholder="Search…" value={messageSearch} onChange={event=>setMessageSearch(event.target.value)} /></label><div className="file-browser-tabs" role="tablist" aria-label="Message grouping"><button type="button" role="tab" aria-selected={messageGrouping==="all"} onClick={()=>setMessageGrouping("all")}>All</button><button type="button" role="tab" aria-selected={messageGrouping==="type"} onClick={()=>setMessageGrouping("type")}>By type</button></div></> : null}
      <div className="file-browser-scroll"><DataTable
        label="Messages in this file"
        className={selected !== null ? `page-table messages-table${checked.size ? " has-checked" : ""}` : "page-table"}
        {...(selected !== null ? {rowHeightRem:4,hideHeader:true} : {})}
        rows={[...listing.rows].filter(row=>!messageSearch || `${messageType(row)} ${row.index+1}`.toLowerCase().includes(messageSearch.toLowerCase())).sort((a,b)=>messageGrouping==="type" ? messageType(a).localeCompare(messageType(b)) || a.index-b.index : a.index-b.index)}
        rowId={(row) => String(row.index)}
        rowLabel={(row) => `Message ${row.index + 1} · ${messageType(row)}`}
        columns={columns}
        checked={checked}
        onCheck={setChecked}
        selected={selected === null ? null : String(selected)}
        onSelect={(id) => {
          const index = Number(id);
          if (index === selected) return;
          setSelected(index);
          setInspection(null);
          void inspect(index, "", 0, -1, revealed);
        }}
        onOpen={() => undefined}
      />
      </div>
      {listing.rows.length<listing.total ? <button type="button" disabled={disabled} onClick={()=>void loadMore()}>Load more messages</button> : null}
      </div>
    );
  }

  // A saved copy is silent; only a refusal is said.
  const status = chosen ?? (copied && copied.state !== "completed" ? copied.reason ?? "The copy was not saved." : null);

  const fileActions = file && listing ? [
    {label:"Format…",onSelect:()=>setFormatting(true),disabled},
    {label:"Save copy…",onSelect:()=>void copy(),disabled:disabled || listing.state!=="completed"},
    {label:"File details",onSelect:()=>setInfo(true)},
  ] : [];
  return {
    disabled,
    fileActions,
    openFile:()=>void open(),
    useMessageReference:()=>setReferenceResetRequest(count=>count+1),
    navigation,
    retention: listing?.state==="completed" && listing.sha256 ? {file, identity:listing.sha256, format:framing, terminator, messages:[...checked].map(Number), ...(selected!==null ? {selected} : {}),path:where.current.path,node_offset:where.current.nodeOffset} satisfies import("./bindings").ImportInvestigation : null,
    restore,
    openPath,
    edition: inspection?.inspection?.metadata.hl7_version || "",
    referenceEdition:inspection?.inspection?.reference?.edition || "",
    chooseReference:()=>setReferenceRequest(count=>count+1),
    chooseReferenceVersion:()=>setReferenceLibraryRequest(count=>count+1),
    title: file ? (listing?.name || fileName(file)) : "Inspect file",
    actions: (
      <>
        <IconButton icon="open" label="Open file" className="reader-action" disabled={disabled} onClick={()=>void open()} />
        {file && listing ? (
          <Menu
            className="reader-menu" trigger={<img className="workbench-icon" src={moreAsset} alt="" />}
            label="More file actions"
            items={fileActions}
          />
        ) : null}
      </>
    ),
    body: (
      <div className={selected !== null ? "file-reader-body" : ""}>
        {status ? (
          <p className="file-status" role="alert">
            {status}
          </p>
        ) : null}
        {body}
        <FormatSheet
          open={formatting}
          framing={framing}
          terminator={terminator}
          onApply={(nextFraming, nextTerminator) => {
            setFraming(nextFraming);
            setTerminator(nextTerminator);
            setFormatting(false);
            void list(file, nextFraming, nextTerminator);
          }}
          onClose={() => setFormatting(false)}
        />
        <Modal open={info && listing !== null} title="File details" onClose={() => setInfo(false)}>
          {listing ? (
            <ValueRows
              rows={[
                { label: "File", value: listing.name || fileName(file) },
                { label: "Bytes", value: String(listing.bytes) },
                { label: "Messages", value: listing.state === "completed" ? String(listing.total) : "—" },
                { label: "Framing", value: formatLabel(FRAMINGS, listing.format, listing.format_selection) },
                { label: "Segment terminator", value: formatLabel(TERMINATORS, listing.terminator, listing.terminator_selection) },
                { label: "SHA-256", value: <code>{listing.sha256}</code> },
              ]}
            />
          ) : null}
        </Modal>
      </div>
    ),
    /** The same browser and reader layout applies to one or many messages. */
    details: selected !== null ? reader : null,
    closeDetails: () => {
      setSelected(null);
      setInspection(null);
    },
  };
}

function FormatSheet({
  open,
  framing,
  terminator,
  onApply,
  onClose,
}: {
  open: boolean;
  framing: Framing;
  terminator: Terminator;
  onApply: (framing: Framing, terminator: Terminator) => void;
  onClose: () => void;
}) {
  const [nextFraming, setNextFraming] = useViewState("FormatSheet.nextFraming", framing);
  const [nextTerminator, setNextTerminator] = useViewState("FormatSheet.nextTerminator", terminator);
  useEffect(() => {
    if (open) {
      setNextFraming(framing);
      setNextTerminator(terminator);
    }
  }, [open, framing, terminator]);
  return (
    <FormDialog open={open} title="Format" size="small" submitLabel="Apply" onClose={onClose} onSubmit={() => onApply(nextFraming, nextTerminator)}>
      <label htmlFor="file-framing">Framing</label>
      <select id="file-framing" value={nextFraming} onChange={(event) => setNextFraming(event.target.value as Framing)}>
        {FRAMINGS.map((choice) => (
          <option key={choice.value} value={choice.value}>
            {choice.label}
          </option>
        ))}
      </select>
      <label htmlFor="file-terminator">Segment terminator</label>
      <select id="file-terminator" value={nextTerminator} onChange={(event) => setNextTerminator(event.target.value as Terminator)}>
        {TERMINATORS.map((choice) => (
          <option key={choice.value} value={choice.value}>
            {choice.label}
          </option>
        ))}
      </select>
    </FormDialog>
  );
}
