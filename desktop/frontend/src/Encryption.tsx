// Settings → Security → Encryption: the project's encryption controls. A
// control is a reference to a key held in an operating system or
// customer-managed store; the key itself is read only when a control is
// checked, rotated or used, and never shown or kept. One sheet adds or edits a
// control; Check, Record rotation, Export and Retire are named actions.
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import {
  chooseEnvironmentFile,
  checkProtectionControl,
  exportProtectionControl,
  listProtectionControls,
  retireProtectionControl,
  rotateProtectionControl,
  saveProtectionControl,
  updateProtectionControl,
  type ListedProtectionControl,
  type ProtectionListResult,
  type ProtectRotationState,
  type ProtectState,
  type ProtectStorage,
} from "./bindings";
import type { DisplayMap } from "./display";
import { DataTable, type Column } from "./DataTable";
import { FilePicker, fileName } from "./Environments";
import { IconButton } from "./IconButton";
import { EmptyState, FormDialog, Menu, Modal, ValueRows, type SubmitFailure } from "./layout";

const STORAGES: DisplayMap<ProtectStorage> = {
  "os-volume-encryption": "OS volume encryption",
  "customer-key": "Customer-managed key",
  "none-declared": "None declared",
};
const STATES: DisplayMap<ProtectState> = { active: "Active", retired: "Retired" };
const ROTATIONS: DisplayMap<ProtectRotationState> = { current: "Current", overdue: "Overdue", "not-declared": "Not declared" };

/** A duration as people enter it, and the Go duration the control keeps. */
type Unit = "days" | "hours" | "minutes";
type Duration = { amount: string; unit: Unit; kept: string; touched?: boolean };

function durationOf(value: string | undefined): Duration {
  const kept = value ?? "";
  const hours = /^(\d+)h$/.exec(kept);
  if (hours) {
    const count = Number(hours[1]);
    return count % 24 === 0 ? { amount: String(count / 24), unit: "days", kept } : { amount: String(count), unit: "hours", kept };
  }
  const minutes = /^(\d+)m$/.exec(kept);
  if (minutes) return { amount: minutes[1]!, unit: "minutes", kept };
  // A saved form this sheet does not write is kept as it is until changed.
  return { amount: "", unit: "days", kept };
}

function durationText(duration: Duration): string | null {
  const amount = duration.amount.trim();
  // An emptied field clears the interval; one never touched keeps its saved form.
  if (amount === "") return duration.touched ? "" : duration.kept;
  if (!/^\d+$/.test(amount) || Number(amount) === 0) return null;
  const count = Number(amount);
  return duration.unit === "days" ? `${count * 24}h` : duration.unit === "hours" ? `${count}h` : `${count}m`;
}

function durationLabel(value: string | undefined): string {
  if (!value) return "—";
  const duration = durationOf(value);
  if (duration.amount === "") return value;
  const count = Number(duration.amount);
  const unit = duration.unit === "days" ? "day" : duration.unit === "hours" ? "hour" : "minute";
  return `${count} ${unit}${count === 1 ? "" : "s"}`;
}

function DurationField({ id, label, value, onChange }: { id: string; label: string; value: Duration; onChange: (value: Duration) => void }) {
  return (
    <>
      <label htmlFor={id}>{label}</label>
      <div className="inline-fields">
        <input id={id} type="text" inputMode="numeric" value={value.amount} onChange={(event) => onChange({ ...value, amount: event.target.value, touched: true })} />
        <select aria-label={`${label} unit`} value={value.unit} onChange={(event) => onChange({ ...value, unit: event.target.value as Unit, touched: true })}>
          <option value="days">Days</option>
          <option value="hours">Hours</option>
          <option value="minutes">Minutes</option>
        </select>
      </div>
    </>
  );
}

const keyOf = (listed: ListedProtectionControl) => `${listed.entry}\u0000${listed.control.name}`;

/** The Encryption page's actions and body. */
export function useEncryption({ root, busy, onChanged }: { root: string | null; busy: boolean; onChanged?: () => void }) {
  const [list, setList] = useState<ProtectionListResult | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const [sheet, setSheet] = useState<null | "add" | "edit" | "retire">(null);
  const [status, setStatus] = useState<{ key: string; text: ReactNode; problem?: boolean } | null>(null);
  const reads = useRef(0);

  const refresh = useCallback(async () => {
    if (!root) return;
    const read = ++reads.current;
    const answer = await listProtectionControls(root);
    if (read === reads.current) setList(answer);
  }, [root]);
  useEffect(() => {
    setList(null);
    void refresh();
  }, [refresh]);
  // A saved, rotated or retired control changes the project's files, which
  // Packages then offers.
  const changed = async () => {
    await refresh();
    onChanged?.();
  };

  const controls = list?.controls ?? [];
  const chosen = controls.find((listed) => keyOf(listed) === selected) ?? null;
  const say = (listed: ListedProtectionControl, text: ReactNode, problem = false) => setStatus({ key: keyOf(listed), text, problem });

  const check = async (listed: ListedProtectionControl) => {
    if (!root) return;
    const answer = await checkProtectionControl(root, listed.entry, listed.control.name);
    if (answer.state === "completed") say(listed, `Key resolved · generation ${answer.generation ?? listed.control.generation}`);
    else say(listed, answer.reason ?? "The key did not resolve.", true);
  };
  const rotate = async (listed: ListedProtectionControl) => {
    if (!root) return;
    const answer = await rotateProtectionControl(root, listed.entry, listed.control.name);
    if (answer.state === "completed") {
      await changed();
      const next = answer.document?.controls.find((control) => control.name === listed.control.name);
      say(listed, `Rotation recorded · generation ${next?.generation ?? listed.control.generation + 1}`);
    } else {
      say(listed, answer.reason ?? "No rotation was recorded.", true);
    }
  };
  const exportControl = async (listed: ListedProtectionControl) => {
    if (!root) return;
    const answer = await exportProtectionControl(root, listed.entry, listed.control.name);
    if (answer.state === "completed" && answer.path) say(listed, `Exported ${fileName(answer.path)}`);
    else if (answer.state !== "cancelled") say(listed, answer.reason ?? "Not exported.", true);
  };

  if (!root) return { actions: null, body: null };

  const columns: Column<ListedProtectionControl>[] = [
    { key: "name", header: "Name", priority: 1, minWidth: 12.5, render: (listed) => listed.control.name },
    { key: "storage", header: "Storage declaration", priority: 3, minWidth: 11, render: (listed) => STORAGES[listed.control.storage] },
    { key: "state", header: "State", priority: 1, minWidth: 6, render: (listed) => STATES[listed.control.state] },
    { key: "generation", header: "Generation", priority: 4, minWidth: 6, render: (listed) => String(listed.control.generation) },
    { key: "rotation", header: "Rotation", priority: 2, minWidth: 7, render: (listed) => ROTATIONS[listed.control.rotation] },
  ];

  let body: ReactNode;
  if (list && list.state === "failed") {
    body = (
      <div className="empty-state" role="alert">
        <p className="empty-title">{list.reason ?? "The encryption controls could not be read."}</p>
        <div className="empty-action">
          <button type="button" onClick={() => void refresh()}>
            Retry
          </button>
        </div>
      </div>
    );
  } else if (list && controls.length === 0) {
    body = (
      <EmptyState
        title="No encryption controls"
        action={
          <button type="button" className="primary" disabled={busy} onClick={() => setSheet("add")}>
            Add control
          </button>
        }
      />
    );
  } else {
    body = (
      <DataTable
        label="Encryption controls"
        className="page-table values-table"
        rows={controls}
        rowId={keyOf}
        rowLabel={(listed) => listed.control.name}
        columns={columns}
        selected={selected}
        onSelect={setSelected}
        onOpen={setSelected}
        loading={list === null}
      />
    );
  }

  const control = chosen?.control;
  return {
    actions:
      controls.length > 0 ? (
        <button type="button" className="primary" disabled={busy} onClick={() => setSheet("add")}>
          Add control
        </button>
      ) : null,
    body: (
      <>
        {(list?.unreadable ?? []).map((problem) => (
          <p key={problem.entry} role="alert" className="object-problem">
            {fileName(problem.entry)}: {problem.reason}
          </p>
        ))}
        {body}
        <Modal
          open={chosen !== null && sheet === null}
          title={control?.name ?? "Control"}
          onClose={() => setSelected(null)}
          footer={
            chosen && control ? (
              <>
                {status && status.key === keyOf(chosen) ? (
                  <p className="dialog-status" role={status.problem ? "alert" : "status"}>
                    {status.text}
                  </p>
                ) : null}
                <div className="dialog-footer">
                  <Menu
                    label={`More actions for ${control.name}`}
                    items={[
                      { label: "Check control", onSelect: () => void check(chosen), disabled: busy },
                      { label: "Record rotation", onSelect: () => void rotate(chosen), disabled: busy || control.state !== "active" },
                      { label: "Export control", onSelect: () => void exportControl(chosen), disabled: busy },
                      { label: "Retire control…", onSelect: () => setSheet("retire"), disabled: busy || control.state !== "active", tone: "danger", separated: true },
                    ]}
                  />
                  <button type="button" className="primary" disabled={busy || control.state !== "active"} onClick={() => setSheet("edit")}>
                    Edit
                  </button>
                </div>
              </>
            ) : null
          }
        >
          {control ? (
            <>
              <ValueRows
                rows={[
                  { label: "Storage declaration", value: STORAGES[control.storage] },
                  { label: "State", value: STATES[control.state] },
                  { label: "Generation", value: String(control.generation) },
                  { label: "Last rotation", value: control.rotated_at ? new Date(control.rotated_at).toLocaleString() : "—" },
                  { label: "Rotation interval", value: durationLabel(control.max_age) },
                  { label: "Rotation", value: ROTATIONS[control.rotation] },
                  { label: "Retention period", value: durationLabel(control.retain) },
                  { label: "Key program", value: fileName(control.command) || "—" },
                  { label: "Arguments", value: control.locator_arguments === 0 ? "None" : `${control.locator_arguments} stored` },
                ]}
              />
              {(list?.limitations ?? []).length > 0 ? (
                <ul className="plain-list limitations">
                  {list!.limitations.map((limitation) => (
                    <li key={limitation}>{limitation}</li>
                  ))}
                </ul>
              ) : null}
            </>
          ) : null}
        </Modal>
        <ControlSheet
          open={sheet === "add" || sheet === "edit"}
          mode={sheet === "edit" ? "edit" : "add"}
          listed={sheet === "edit" ? chosen : null}
          root={root}
          entry={sheet === "edit" && chosen ? chosen.entry : (list?.add_entry ?? "protection.json")}
          onClose={() => setSheet(null)}
          onSaved={async (name, rotated) => {
            setSheet(null);
            await changed();
            const entry = sheet === "edit" && chosen ? chosen.entry : (list?.add_entry ?? "protection.json");
            setSelected(`${entry}\u0000${name}`);
            if (rotated) setStatus({ key: `${entry}\u0000${name}`, text: "Saved as a rotation" });
          }}
        />
        <Modal
          open={sheet === "retire" && chosen !== null}
          title={`Retire ${control?.name ?? "control"}?`}
          size="small"
          onClose={() => setSheet(null)}
          footer={
            <div className="dialog-footer">
              <button type="button" data-autofocus onClick={() => setSheet(null)}>
                Cancel
              </button>
              <button
                type="button"
                className="danger solid"
                disabled={busy}
                onClick={async () => {
                  if (!chosen) return;
                  const answer = await retireProtectionControl(root, chosen.entry, chosen.control.name);
                  setSheet(null);
                  if (answer.state === "completed") {
                    await changed();
                    say(chosen, "Retired");
                  } else {
                    say(chosen, answer.reason ?? "Not retired.", true);
                  }
                }}
              >
                Retire
              </button>
            </div>
          }
        >
          <p>Stops new encrypted packages; existing packages remain readable.</p>
        </Modal>
      </>
    ),
  };
}

/** Add or edit one control. Stored arguments are never shown: Edit counts
 * them, and Replace arguments starts a blank list. */
function ControlSheet({
  open,
  mode,
  listed,
  root,
  entry,
  onClose,
  onSaved,
}: {
  open: boolean;
  mode: "add" | "edit";
  listed: ListedProtectionControl | null;
  root: string;
  entry: string;
  onClose: () => void;
  onSaved: (name: string, rotated: boolean) => void;
}) {
  const control = listed?.control ?? null;
  const [name, setName] = useState("");
  const [storage, setStorage] = useState<ProtectStorage>("os-volume-encryption");
  const [command, setCommand] = useState("");
  const [replacing, setReplacing] = useState(false);
  const [args, setArgs] = useState<string[]>([]);
  const [maxAge, setMaxAge] = useState<Duration>(durationOf(undefined));
  const [retain, setRetain] = useState<Duration>(durationOf(undefined));
  const [chooseFailure, setChooseFailure] = useState<string | null>(null);
  useEffect(() => {
    if (!open) return;
    setName(control?.name ?? "");
    setStorage(control?.storage ?? "os-volume-encryption");
    setCommand(control?.command ?? "");
    setReplacing(mode === "add");
    setArgs([]);
    setMaxAge(durationOf(control?.max_age));
    setRetain(durationOf(control?.retain));
    setChooseFailure(null);
  }, [open]); // eslint-disable-line react-hooks/exhaustive-deps

  const entered = args.map((argument) => argument.trim()).filter((argument) => argument !== "");
  const dirty =
    mode === "add"
      ? name !== "" || command !== "" || entered.length > 0
      : storage !== control?.storage || command !== control?.command || replacing || durationText(maxAge) !== (control?.max_age ?? "") || durationText(retain) !== (control?.retain ?? "");

  const submit = async (): Promise<SubmitFailure | null> => {
    const age = durationText(maxAge);
    if (age === null) return { reason: "Enter a whole number for the rotation interval.", field: "control-max-age" };
    const kept = durationText(retain);
    if (kept === null) return { reason: "Enter a whole number for the retention period.", field: "control-retain" };
    if (mode === "add") {
      const answer = await saveProtectionControl({
        workspace: root,
        entry,
        name: name.trim(),
        storage,
        command,
        arguments: entered,
        ...(age ? { max_age: age } : {}),
        ...(kept ? { retain: kept } : {}),
      });
      if (answer.state !== "completed") return { reason: answer.reason ?? "Not saved." };
      onSaved(name.trim(), false);
      return null;
    }
    if (!control) return { reason: "This control is not open." };
    const answer = await updateProtectionControl({
      workspace: root,
      entry,
      name: control.name,
      storage,
      command,
      ...(replacing ? { arguments: entered } : {}),
      max_age: age,
      retain: kept,
    });
    if (answer.state !== "completed") return { reason: answer.reason ?? "Not saved." };
    onSaved(control.name, answer.rotated);
    return null;
  };

  return (
    <FormDialog
      open={open}
      title={mode === "add" ? "Add control" : `Edit ${control?.name ?? "control"}`}
      submitLabel="Save"
      submitDisabled={(mode === "add" && name.trim() === "") || command === ""}
      dirty={dirty}
      onClose={onClose}
      onSubmit={submit}
    >
      {mode === "add" ? (
        <>
          <label htmlFor="control-name">Name</label>
          <input id="control-name" type="text" value={name} onChange={(event) => setName(event.target.value)} />
        </>
      ) : null}
      <label htmlFor="control-storage">Storage declaration</label>
      <select id="control-storage" value={storage} onChange={(event) => setStorage(event.target.value as ProtectStorage)}>
        {(Object.keys(STORAGES) as ProtectStorage[]).map((code) => (
          <option key={code} value={code}>
            {STORAGES[code]}
          </option>
        ))}
      </select>
      <FilePicker
        label="Key program"
        path={command}
        onChoose={() => {
          setChooseFailure(null);
          void chooseEnvironmentFile("locator-program").then((answer) => {
            if (answer.state === "completed" && answer.paths?.[0]) setCommand(answer.paths[0]);
            else if (answer.state !== "cancelled") setChooseFailure(answer.reason ?? "The program was not chosen.");
          });
        }}
      />
      {chooseFailure ? (
        <p className="field-error" role="alert">
          {chooseFailure}
        </p>
      ) : null}
      {replacing ? (
        <fieldset>
          <legend>Arguments</legend>
          {args.map((argument, index) => (
            <div key={index} className="filter-rule-row">
              <input
                type="text"
                aria-label={`Argument ${index + 1}`}
                spellCheck={false}
                autoComplete="off"
                value={argument}
                onChange={(event) => setArgs((held) => held.map((value, at) => (at === index ? event.target.value : value)))}
              />
              <IconButton icon="close" label={`Remove argument ${index + 1}`} onClick={() => setArgs((held) => held.filter((_, at) => at !== index))} />
            </div>
          ))}
          <button type="button" className="quiet" onClick={() => setArgs((held) => [...held, ""])}>
            Add argument
          </button>
        </fieldset>
      ) : (
        <div className="value-with-action">
          <span>Arguments</span>
          <span>{control && control.locator_arguments > 0 ? `${control.locator_arguments} stored` : "None"}</span>
          <button
            type="button"
            onClick={() => {
              setReplacing(true);
              setArgs([""]);
            }}
          >
            Replace arguments
          </button>
        </div>
      )}
      <DurationField id="control-max-age" label="Rotation interval" value={maxAge} onChange={setMaxAge} />
      <DurationField id="control-retain" label="Retention period" value={retain} onChange={setRetain} />
      {mode === "edit" && (command !== control?.command || replacing) ? <p className="consequence">Saving reads the key from the new program and records a rotation.</p> : null}
    </FormDialog>
  );
}
