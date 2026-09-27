import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import "./raw.css";
import {
  chooseInspectionPath,
  inspectFileMessage,
  listFileMessages,
  readFileBytes,
  saveFileCopy,
  type FileBytesResult,
  type FileMessage,
  type FileMessagesResult,
  type InspectionResult,
  type RoundTripResult,
} from "./bindings";
import { DataTable, type Column } from "./DataTable";
import { EmptyState, FormDialog, Menu, Modal, Reveal, ValueRows } from "./layout";
import { HexTable, MessageReader } from "./Inspector";
import { typeLabel } from "./Messages";
import { useLifecycle } from "./lifecycle";
import { useViewState } from "./viewstate";

type Framing = "auto" | "raw" | "mllp";
type Terminator = "auto" | "cr" | "lf" | "crlf";

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
export function useFileReader({ busy, request }: { busy: boolean; request: number }) {
  const [file, setFile] = useState("");
  const [framing, setFraming] = useState<Framing>("auto");
  const [terminator, setTerminator] = useState<Terminator>("auto");
  const [listing, setListing] = useState<FileMessagesResult | null>(null);
  const [bytes, setBytes] = useState<FileBytesResult | null>(null);
  const [bytesShown, setBytesShown] = useState(false);
  const [selected, setSelected] = useState<number | null>(null);
  const [inspection, setInspection] = useState<InspectionResult | null>(null);
  const [revealed, setRevealed] = useState(false);
  const [formatting, setFormatting] = useState(false);
  const [info, setInfo] = useState(false);
  const [copied, setCopied] = useState<RoundTripResult | null>(null);
  const [chosen, setChosen] = useState<string | null>(null);
  const { running, run } = useLifecycle<"choosing" | "reading" | "inspecting" | "copying">({ window: true });
  const disabled = busy || running !== null;
  const where = useRef({ path: "", nodeOffset: 0, byteOffset: -1 });

  const list = useCallback(
    async (path: string, format: Framing, ending: Terminator) => {
      await run("reading", async () => {
        setSelected(null);
        setInspection(null);
        setBytes(null);
        setCopied(null);
        const answer = await listFileMessages({ file: path, format, terminator: ending, offset: 0, limit: 0 });
        setListing(answer);
        setBytesShown(false);
        if (answer.state !== "completed" && answer.sha256) {
          setBytes(await readFileBytes({ file: path, expect: answer.sha256, offset: 0, reveal: false }));
        }
      });
    },
    [run],
  );

  const inspect = useCallback(
    async (message: number, path: string, nodeOffset: number, byteOffset: number, reveal: boolean, rawOffset = -1): Promise<InspectionResult | null> => {
      if (!listing?.sha256) return null;
      let answer: InspectionResult | null = null;
      await run("inspecting", async (current) => {
        const result = await inspectFileMessage({
          file,
          format: framing,
          terminator,
          expect: listing.sha256,
          message,
          path,
          node_offset: nodeOffset,
          byte_offset: byteOffset,
          raw_offset: rawOffset,
          reveal,
        });
        answer = result;
        if (!current()) return;
        // A field that is not there leaves the message as it was.
        if (result.state === "completed" || path === "") {
          setInspection(result);
          where.current = { path, nodeOffset, byteOffset };
        }
      });
      return answer;
    },
    [file, framing, listing, run, terminator],
  );

  const open = useCallback(async () => {
    await run("choosing", async () => {
      setChosen(null);
      const answer = await chooseInspectionPath("file");
      if (answer.state === "completed" && answer.path) {
        setFile(answer.path);
        setFraming("auto");
        setTerminator("auto");
        setRevealed(false);
      } else if (answer.state !== "cancelled") {
        setChosen(answer.reason ?? "The file could not be opened.");
      }
    });
  }, [run]);

  // A new file is read as soon as it is chosen.
  useEffect(() => {
    if (file) void list(file, framing, terminator);
    // Only a new file starts a read; a format change applies from its sheet.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [file]);

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
    if (single && selected === null) {
      setSelected(0);
      void inspect(0, "", 0, -1, revealed);
    }
  }, [inspect, revealed, selected, single]);

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

  const reader = (
    <MessageReader
      result={inspection}
      loading={running === "inspecting"}
      busy={disabled}
      onInspect={(path, nodeOffset, byteOffset, rawOffset) => (selected === null ? Promise.resolve(null) : inspect(selected, path, nodeOffset, byteOffset, revealed, rawOffset))}
      onReveal={reveal}
      {...(single ? {} : { onClose: () => { setSelected(null); setInspection(null); } })}
    />
  );

  const columns: Column<FileMessage>[] = [
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
  } else if (single) {
    body = reader;
  } else {
    body = (
      <DataTable
        label="Messages in this file"
        className="page-table"
        rows={listing.rows}
        rowId={(row) => String(row.index)}
        rowLabel={(row) => `Message ${row.index + 1} · ${messageType(row)}`}
        columns={columns}
        selected={selected === null ? null : String(selected)}
        onSelect={(id) => {
          const index = Number(id);
          if (index === selected) return;
          setSelected(index);
          void inspect(index, "", 0, -1, revealed);
        }}
        onOpen={() => undefined}
      />
    );
  }

  // A saved copy is silent; only a refusal is said.
  const status = chosen ?? (copied && copied.state !== "completed" ? copied.reason ?? "The copy was not saved." : null);

  return {
    title: file ? (listing?.name || fileName(file)) : "Inspect file",
    actions: (
      <>
        <button type="button" disabled={disabled} onClick={() => void open()}>
          Open file
        </button>
        {file && listing ? (
          <Menu
            label="More file actions"
            items={[
              { label: "Format…", onSelect: () => setFormatting(true), disabled },
              { label: "Save copy…", onSelect: () => void copy(), disabled: disabled || listing.state !== "completed" },
              { label: "Info", onSelect: () => setInfo(true) },
            ]}
          />
        ) : null}
      </>
    ),
    body: (
      <>
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
        <Modal open={info && listing !== null} title="Info" onClose={() => setInfo(false)}>
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
      </>
    ),
    /** The reader beside a list of several messages. */
    details: !single && selected !== null ? reader : null,
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

