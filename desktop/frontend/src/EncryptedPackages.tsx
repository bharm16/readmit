// Encrypted packages (#560): the project's transfer packages, read from their
// descriptors without a key. Decrypt writes a fresh verified plaintext copy to
// a place chosen in the save dialog and opens nothing it holds; Delete package
// obeys the declared retention unless overridden, with a reason, and unlinks
// only the files the package declares.
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { chooseShareDestination, listEncryptedPackages, openSharedOutput, RequestScope, type EncryptedPackage, type ReviewedActionResult } from "./bindings";
import { DataTable } from "./DataTable";
import { PACKAGE_RETENTIONS, term } from "./display";
import { EmptyState, ValueRows } from "./layout";
import { ReviewSheet } from "./ReviewSheet";

function when(stamp?: string): string {
  return stamp ? new Date(stamp).toLocaleString() : "—";
}

/** The Encrypted packages page: its list, and the one selected. */
export function useEncryptedPackages({ root, projectId, shown }: { root: string | null; projectId: string; shown: boolean }) {
  const scope = useRef(new RequestScope());
  const context = useCallback(() => scope.current.enter(root ?? "", projectId), [root, projectId]);
  const [packages, setPackages] = useState<EncryptedPackage[] | null>(null);
  const [reason, setReason] = useState<string | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const [sheet, setSheet] = useState<null | "decrypt" | "delete">(null);
  const [destination, setDestination] = useState<{ handle: string; name: string; location: string } | null>(null);
  const [override, setOverride] = useState(false);
  const [why, setWhy] = useState("");
  const [written, setWritten] = useState<ReviewedActionResult | null>(null);

  const read = useCallback(async () => {
    if (!root) return;
    const answer = await listEncryptedPackages(context());
    if (!scope.current.current(answer)) return;
    setPackages(answer.packages);
    setReason(answer.state === "completed" || answer.state === "empty" ? null : (answer.reason ?? "The packages cannot be read."));
  }, [context, root]);
  useEffect(() => {
    if (shown) void read();
  }, [shown, read]);
  // Every new selection starts with the declared retention obeyed.
  useEffect(() => {
    setOverride(false);
    setWhy("");
    setWritten(null);
  }, [selected]);

  const chosen = packages?.find((entry) => entry.entry === selected) ?? null;
  const decryptOptions = useMemo(() => ({ package: { package: chosen?.entry ?? "", ...(destination ? { destination: destination.handle } : {}) } }), [chosen, destination]);
  const deleteOptions = useMemo(() => ({ package: { package: chosen?.entry ?? "", ...(override ? { override: true } : {}) } }), [chosen, override]);

  const actions = chosen && !chosen.problem ? (
    <>
      <button type="button" onClick={() => {
        setDestination(null);
        setSheet("decrypt");
      }}>
        Decrypt
      </button>
      <button type="button" onClick={() => setSheet("delete")}>
        Delete package
      </button>
    </>
  ) : null;

  const body = !root ? null : (
    <>
      {reason ? <p role="alert">{reason}</p> : null}
      {packages === null ? (
        <p aria-live="polite">Reading…</p>
      ) : packages.length === 0 ? (
        <EmptyState title="No encrypted packages" />
      ) : (
        <DataTable
          label="Encrypted packages"
          className="page-table"
          rows={packages}
          rowId={(row) => row.entry}
          rowLabel={(row) => row.entry}
          columns={[
            { key: "name", header: "Name", priority: 1, minWidth: 12, flex: true, render: (row) => row.entry },
            { key: "created", header: "Created", priority: 2, minWidth: 10, render: (row) => when(row.created_at) },
            { key: "retention", header: "Retention", priority: 2, minWidth: 9, render: (row) => (row.retention ? term(PACKAGE_RETENTIONS, row.retention).text : "—") },
            { key: "state", header: "State", priority: 1, minWidth: 8, render: (row) => (row.problem ? "Unreadable" : "Readable") },
          ]}
          selected={selected}
          onSelect={setSelected}
          onOpen={setSelected}
        />
      )}
      {chosen ? (
        <section aria-label={chosen.entry}>
          <div className="section-toolbar">{actions}</div>
          <ValueRows
            label="Package"
            rows={
              chosen.problem
                ? [{ label: "State", value: chosen.problem }]
                : [
                    { label: "Created", value: when(chosen.created_at) },
                    { label: "Retention", value: chosen.retention ? term(PACKAGE_RETENTIONS, chosen.retention).text : "—" },
                    ...(chosen.retain_until ? [{ label: "Retained until", value: when(chosen.retain_until) }] : []),
                    { label: "Control", value: `${chosen.control ?? ""} · Generation ${chosen.generation ?? 0}` },
                    { label: "Entries", value: String(chosen.entries ?? 0) },
                  ]
            }
          />
          {written?.package_action?.output ? (
            <div className="share-result" role="status">
              <p>Decrypted {written.package_action.name}</p>
              <button type="button" onClick={() => void openSharedOutput({ output: written.package_action!.output!, folder: true })}>
                Show in folder
              </button>
            </div>
          ) : null}
        </section>
      ) : null}
      {chosen ? (
        <ReviewSheet
          open={sheet === "decrypt"}
          title="Decrypt"
          action="package.decrypt"
          finalLabel="Decrypt"
          context={context}
          items={[]}
          options={decryptOptions}
          prepareKey={destination?.handle ?? ""}
          onClose={() => setSheet(null)}
          onDone={(answer) => {
            setWritten(answer);
            setSheet(null);
          }}
          consequence="Writes a decrypted copy on this computer."
          fields={
            <ValueRows
              rows={[
                {
                  label: "Copy",
                  value: (
                    <span className="value-with-action">
                      {destination ? [destination.name, destination.location].filter(Boolean).join(" · ") : "Not chosen"}
                      <button
                        type="button"
                        className="quiet"
                        onClick={() =>
                          void chooseShareDestination({ context: context(), name: `${chosen.entry} decrypted`, folder: true }).then((answer) => {
                            if (answer.state === "completed" && answer.destination) setDestination({ handle: answer.destination, name: answer.name ?? "", location: answer.location ?? "" });
                          })
                        }
                      >
                        {destination ? "Change" : "Choose"}
                      </button>
                    </span>
                  ),
                },
              ]}
            />
          }
          render={(review) => <ValueRows rows={[{ label: "Package", value: review.package_action?.package.entry ?? chosen.entry }]} />}
        />
      ) : null}
      {chosen ? (
        <ReviewSheet
          open={sheet === "delete"}
          title="Delete package"
          action="package.delete"
          finalLabel="Delete"
          tone="danger"
          context={context}
          items={[]}
          options={deleteOptions}
          prepareKey={String(override)}
          {...(override ? { rationale: why.trim() } : {})}
          blocked={() => override && why.trim() === ""}
          onClose={() => setSheet(null)}
          onDone={() => {
            setSheet(null);
            setSelected(null);
            void read();
          }}
          consequence="Unlinks the package's files. This is not secure erasure, and copies elsewhere are untouched."
          fields={
            <>
              <label className="check">
                <input type="checkbox" checked={override} onChange={(event) => setOverride(event.target.checked)} />
                Override retention
              </label>
              {override ? (
                <>
                  <label htmlFor="package-delete-reason">Reason</label>
                  <textarea id="package-delete-reason" rows={2} maxLength={1024} value={why} onChange={(event) => setWhy(event.target.value)} />
                </>
              ) : null}
            </>
          }
          render={(review) => (
            <ValueRows
              rows={[
                { label: "Package", value: review.package_action?.package.entry ?? chosen.entry },
                { label: "Files", value: String(review.package_action?.files ?? 0) },
                { label: "Retention", value: chosen.retention ? term(PACKAGE_RETENTIONS, chosen.retention).text : "—" },
                ...(chosen.retain_until ? [{ label: "Retained until", value: when(chosen.retain_until) }] : []),
              ]}
            />
          )}
        />
      ) : null}
    </>
  );
  return { title: "Encrypted packages", body, refresh: read };
}
