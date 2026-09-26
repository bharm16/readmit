// The frame every destination of the window is drawn in: the sidebar, one page
// title with the page's own actions beside it, at most one row of local views,
// and a body that scrolls on its own. Nothing here decides anything about
// evidence; it only keeps every page laid out the same way.
import { createContext, useContext, useEffect, useId, useRef, useState, type ReactElement, type ReactNode } from "react";
import { IconButton } from "./IconButton";
import { CATEGORY_RAIL_MIN_REM } from "./geometry";
import { useMeasured } from "./measure";
import type { Destination } from "./routes";

export type { Destination } from "./routes";

export const PROJECT_DESTINATIONS: { id: Destination; label: string }[] = [
  { id: "cases", label: "Cases" },
  { id: "tests", label: "Tests" },
  { id: "runs", label: "Runs" },
  { id: "environments", label: "Environments" },
  { id: "reports", label: "Reports" },
];

export const GLOBAL_DESTINATIONS: { id: Destination; label: string }[] = [
  { id: "tools", label: "Tools" },
  { id: "settings", label: "Settings" },
  { id: "help", label: "Help" },
];

/** What every page needs from the window around it: whether the sidebar is a
 * rail, and the project switcher a rail leaves for the page header to hold. */
export const FrameContext = createContext<{ compact: boolean; switcher: ReactNode }>({ compact: false, switcher: null });

/** One page. It stays mounted while another is shown, so an unfinished edit
 * survives looking elsewhere; only the shown page is in the accessibility
 * tree. */
export function Page({
  id,
  shown,
  title,
  back,
  actions,
  children,
}: {
  id: string;
  shown: boolean;
  title: ReactNode;
  /** The way back to where this page was opened from. */
  back?: ReactNode;
  actions?: ReactNode;
  children: ReactNode;
}) {
  const { compact, switcher } = useContext(FrameContext);
  return (
    <div className="page" data-page={id} hidden={!shown}>
      <header className="page-header">
        {compact && switcher ? <div className="header-switcher">{switcher}</div> : null}
        {back}
        <h1>{title}</h1>
        {actions ? <div className="page-actions">{actions}</div> : null}
      </header>
      <div className="page-body">{children}</div>
    </div>
  );
}

/** Back to the place this one was opened from. `label` is what the button
 * shows; the accessible name says where it goes. */
export function BackLink({ label, name, onBack }: { label: string; name?: string; onBack: () => void }) {
  return (
    <button type="button" className="back-link quiet" aria-label={`Back to ${name ?? label.toLowerCase()}`} onClick={onBack}>
      <svg viewBox="0 0 16 16" width="16" height="16" aria-hidden="true" focusable="false">
        <path d="M10 3 5 8l5 5" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
      <span>{label}</span>
    </button>
  );
}

/** What a collection shows when it has nothing: its title and one action,
 * with no paragraph explaining it. */
export function EmptyState({ title, action }: { title: string; action?: ReactNode }) {
  return (
    <div className="empty-state">
      <p className="empty-title">{title}</p>
      {action ? <div className="empty-action">{action}</div> : null}
    </div>
  );
}

const navIcons: Record<Destination, ReactElement> = {
  home: <path d="M2.5 4.5h4l1.2 1.5h5.8v6.5h-11z" />,
  cases: (
    <>
      <rect x="2.5" y="3" width="11" height="10" rx="1" />
      <path d="M2.5 6.5h11" />
    </>
  ),
  tests: (
    <>
      <path d="M3 8.5l2.5 2.5L13 3.5" />
      <path d="M3 13h10" />
    </>
  ),
  runs: <path d="M5 3.5v9l7-4.5z" />,
  environments: (
    <>
      <rect x="2.5" y="3" width="11" height="4" rx="1" />
      <rect x="2.5" y="9" width="11" height="4" rx="1" />
    </>
  ),
  reports: (
    <>
      <path d="M4 2.5h5.5L12 5v8.5H4z" />
      <path d="M6 8h4M6 10.5h4" />
    </>
  ),
  tools: <path d="M10.5 2.5a3 3 0 0 0-2.8 4L3 11.2 4.8 13l4.7-4.7a3 3 0 0 0 4-2.8l-1.8 1.8-1.7-.4-.4-1.7z" />,
  settings: (
    <>
      <circle cx="8" cy="8" r="2" />
      <path d="M8 2v2M8 12v2M2 8h2M12 8h2M3.8 3.8l1.4 1.4M10.8 10.8l1.4 1.4M3.8 12.2l1.4-1.4M10.8 5.2l1.4-1.4" />
    </>
  ),
  help: (
    <>
      <circle cx="8" cy="8" r="5.5" />
      <path d="M6.4 6.4a1.7 1.7 0 1 1 2.2 1.6c-.4.2-.6.5-.6.9v.3" />
      <path d="M8 11.2v.1" />
    </>
  ),
};

/** One destination in the sidebar. The current one is marked for assistive
 * technology as the current page, and by a filled row rather than colour
 * alone. In the icon rail its name is the accessible name and a tooltip. */
export function NavItem({
  id,
  label,
  current,
  onSelect,
}: {
  id: Destination;
  label: string;
  current: boolean;
  onSelect: (id: Destination) => void;
}) {
  return (
    <li>
      <button type="button" className="nav-item" aria-label={label} aria-current={current ? "page" : undefined} onClick={() => onSelect(id)}>
        <svg className="nav-icon" viewBox="0 0 16 16" width="16" height="16" aria-hidden="true" focusable="false">
          {navIcons[id]}
        </svg>
        <span className="nav-label" aria-hidden="true">
          {label}
        </span>
        <span className="nav-tooltip" aria-hidden="true">
          {label}
        </span>
      </button>
    </li>
  );
}

/** The selected project and the ways to change it: its recent projects, Open
 * project, New project and Project settings. */
export function ProjectSwitcher({
  name,
  recent,
  onOpenRecent,
  onOpen,
  onNew,
  onSettings,
  disabled,
}: {
  name: string;
  recent: { key: string; name: string }[];
  onOpenRecent: (key: string) => void;
  onOpen: () => void;
  onNew: () => void;
  onSettings: () => void;
  disabled?: boolean;
}) {
  return (
    <Menu
      label={`Project: ${name}`}
      trigger={
        <>
          <span className="switcher-name">{name}</span>
          <svg viewBox="0 0 16 16" width="16" height="16" aria-hidden="true" focusable="false">
            <path d="M4.5 6.5 8 10l3.5-3.5" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
        </>
      }
      className="project-switcher"
      items={[
        ...recent.map((project) => ({ label: project.name, onSelect: () => onOpenRecent(project.key), disabled })),
        { label: "Open project…", onSelect: onOpen, disabled, separated: recent.length > 0 },
        { label: "New project…", onSelect: onNew, disabled },
        { label: "Project settings", onSelect: onSettings, disabled },
      ]}
    />
  );
}

/** The one operation running now, shown compactly in the sidebar so it stays
 * addressable from anywhere; Stop is offered only for work that can stop. */
export function OperationIndicator({ label, onStop }: { label: string; onStop?: (() => void) | undefined }) {
  return (
    <div className="operation" role="status">
      <span className="spinner" aria-hidden="true" />
      <span className="operation-label">{label}</span>
      {onStop ? (
        <button type="button" className="quiet" onClick={onStop}>
          Stop
        </button>
      ) : null}
    </div>
  );
}

/** Categories beside the selected one: a 10rem rail where the page is at least
 * 45rem wide, otherwise one button naming the current category that opens a
 * picker. The categories are never out of reach. */
export function Categories<K extends string>({
  label,
  categories,
  selected,
  onSelect,
  children,
}: {
  label: string;
  categories: { key: K; label: string }[];
  selected: K;
  onSelect: (key: K) => void;
  children: ReactNode;
}) {
  const [element, setElement] = useState<HTMLDivElement | null>(null);
  const { width, rem } = useMeasured(element);
  const [picking, setPicking] = useState(false);
  const narrow = width > 0 && width / rem < CATEGORY_RAIL_MIN_REM;
  const current = categories.find((category) => category.key === selected)?.label ?? "";
  return (
    <div className={narrow ? "categories narrow" : "categories"} ref={setElement}>
      {narrow ? (
        <div className="category-picker">
          <button type="button" aria-haspopup="dialog" aria-label={`${label}: ${current}`} onClick={() => setPicking(true)}>
            {current}
          </button>
          <Modal open={picking} title={label} size="small" onClose={() => setPicking(false)}>
            <ul className="picker-list">
              {categories.map((category) => (
                <li key={category.key}>
                  <button
                    type="button"
                    aria-current={category.key === selected ? "page" : undefined}
                    onClick={() => {
                      setPicking(false);
                      onSelect(category.key);
                    }}
                  >
                    {category.label}
                  </button>
                </li>
              ))}
            </ul>
          </Modal>
        </div>
      ) : (
        <nav className="category-rail" aria-label={label}>
          <ul>
            {categories.map((category) => (
              <li key={category.key}>
                <button
                  type="button"
                  className="nav-item"
                  aria-current={category.key === selected ? "page" : undefined}
                  onClick={() => onSelect(category.key)}
                >
                  {category.label}
                </button>
              </li>
            ))}
          </ul>
        </nav>
      )}
      <div className="category-body">{children}</div>
    </div>
  );
}

const FOCUSABLE =
  'a[href], button:not([disabled]), input:not([disabled]):not([type="hidden"]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';
const FIELDS = 'input:not([disabled]):not([type="hidden"]), select:not([disabled]), textarea:not([disabled])';

function focusables(root: HTMLElement): HTMLElement[] {
  return Array.from(root.querySelectorAll<HTMLElement>(FOCUSABLE)).filter((element) => !element.closest("[hidden]"));
}

/** The one modal primitive every sheet, editor and chooser is drawn with. On
 * opening, focus goes to the first relevant field (an element marked
 * data-autofocus, else the first field, else the first control); Tab and
 * Shift+Tab stay inside; Escape asks the owner to close and goes no further, so
 * it can never reach work running behind the dialog; closing hands focus back
 * to whatever opened it. Closing is always the owner's decision, which is how a
 * dirty editor gets to ask before it goes. */
export function Dialog({
  open,
  label,
  onClose,
  className,
  inert = false,
  children,
}: {
  open: boolean;
  label: string;
  onClose: () => void;
  className: string;
  /** Another dialog is open over this one. */
  inert?: boolean;
  children: ReactNode;
}) {
  const dialog = useRef<HTMLDialogElement | null>(null);
  const opener = useRef<HTMLElement | null>(null);
  const close = useRef(onClose);
  close.current = onClose;
  // What last had focus outside the dialog: a field inside it that focuses
  // itself on mounting takes focus before the dialog could ask what opened it.
  const outside = useRef<HTMLElement | null>(null);
  useEffect(() => {
    if (open) return;
    const track = (event: FocusEvent) => {
      if (event.target instanceof HTMLElement && !dialog.current?.contains(event.target)) outside.current = event.target;
    };
    document.addEventListener("focusin", track);
    return () => document.removeEventListener("focusin", track);
  }, [open]);
  useEffect(() => {
    const element = dialog.current;
    if (!element) return;
    if (open && !element.open) {
      const active = document.activeElement instanceof HTMLElement ? document.activeElement : null;
      opener.current = active && !element.contains(active) && active !== document.body ? active : outside.current;
      element.showModal();
      const first =
        element.querySelector<HTMLElement>("[data-autofocus]") ??
        element.querySelector<HTMLElement>(FIELDS) ??
        focusables(element)[0];
      first?.focus();
    } else if (!open && element.open) {
      element.close();
    }
    if (!open && opener.current) {
      const back = opener.current;
      opener.current = null;
      if (back.isConnected) back.focus();
    }
  }, [open]);
  return (
    <dialog
      className={className}
      ref={dialog}
      aria-label={label}
      aria-modal="true"
      inert={inert}
      onCancel={(event) => {
        // The platform's own Escape handling would close the dialog without
        // asking its owner; the key handler below asks instead.
        event.preventDefault();
      }}
      onKeyDown={(event) => {
        if (event.key === "Escape") {
          event.preventDefault();
          event.stopPropagation();
          close.current();
          return;
        }
        if (event.key !== "Tab" || !dialog.current) return;
        const stops = focusables(dialog.current);
        if (stops.length === 0) return;
        const first = stops[0]!;
        const last = stops[stops.length - 1]!;
        if (event.shiftKey && document.activeElement === first) {
          event.preventDefault();
          last.focus();
        } else if (!event.shiftKey && document.activeElement === last) {
          event.preventDefault();
          first.focus();
        }
      }}
    >
      {open ? children : null}
    </dialog>
  );
}

export type SheetSize = "small" | "normal" | "wide";

/** A named sheet: a fixed title row with its Close button, a body that alone
 * scrolls, and an optional fixed footer. */
export function Modal({
  open,
  title,
  onClose,
  size = "normal",
  footer,
  inert = false,
  children,
}: {
  open: boolean;
  title: string;
  onClose: () => void;
  size?: SheetSize;
  footer?: ReactNode;
  /** Another dialog is open over this one. */
  inert?: boolean;
  children: ReactNode;
}) {
  return (
    <Dialog open={open} label={title} onClose={onClose} className={`modal sheet-${size}`} inert={inert}>
      <header className="modal-header">
        <h2>{title}</h2>
        <IconButton icon="close" label={`Close ${title.toLowerCase()}`} onClick={onClose} />
      </header>
      <div className="modal-body">{children}</div>
      {footer ? <footer className="modal-footer">{footer}</footer> : null}
    </Dialog>
  );
}

/** Named values of one object, read-only: a label and its value on each row.
 * Editing them is an Edit sheet, never inputs on the page. */
export function ValueRows({ rows, label }: { rows: { label: ReactNode; value: ReactNode }[]; label?: string }) {
  return (
    <dl className="value-rows" {...(label ? { "aria-label": label } : {})}>
      {rows.map((row, index) => (
        <div key={index} className="value-row">
          <dt>{row.label}</dt>
          <dd>{row.value}</dd>
        </div>
      ))}
    </dl>
  );
}

/** The last part of a folder path, which is how a folder is named in a list;
 * the whole path stays beside it. */
export function folderName(path: string): string {
  const trimmed = path.replace(/[\\/]+$/, "");
  const parts = trimmed.split(/[\\/]/);
  return parts[parts.length - 1] || trimmed;
}

/** A code as a person reads it: words instead of separators, capitalised.
 * For display only; the code itself is what is sent anywhere. */
export function humanize(code: string): string {
  const words = code.replaceAll("_", " ").replaceAll("-", " ").trim();
  return words.charAt(0).toUpperCase() + words.slice(1);
}

/** What a failed submission answers: the facade's own reason and, when it
 * names one, the field to correct. */
export type SubmitFailure = { reason: string; field?: string };

/** A form that appears only when a person asks for it: one sheet, its fields,
 * and one footer with Cancel then the action that commits it.
 *
 * Submission is awaited. While it is pending the commit cannot be pressed
 * again; when it fails every entered value stays, the facade's reason is shown
 * and focus goes to the field it names (or the first one marked invalid).
 * Closing on success is the owner's, once the save has actually answered.
 *
 * A dirty form does not close silently: X, Cancel and Escape ask Save
 * changes?, where Keep editing is where focus starts, Discard throws the draft
 * away, and Save submits and stays open if the save fails. */
export function FormDialog({
  open,
  title,
  onClose,
  onSubmit,
  submitLabel,
  submitDisabled = false,
  busy = false,
  dirty = false,
  onDiscard,
  size = "normal",
  tone = "primary",
  status,
  children,
}: {
  open: boolean;
  title: string;
  onClose: () => void;
  onSubmit: () => void | SubmitFailure | null | Promise<void | SubmitFailure | null>;
  submitLabel: string;
  submitDisabled?: boolean;
  busy?: boolean;
  /** The form holds changes that have not been saved. */
  dirty?: boolean;
  /** Throws the unsaved changes away, before the sheet closes. */
  onDiscard?: () => void;
  size?: SheetSize;
  /** A destructive action reads as one. */
  tone?: "primary" | "danger";
  /** An answer to show beside the actions: a refusal or a validation message. */
  status?: ReactNode;
  children: ReactNode;
}) {
  const [pending, setPending] = useState(false);
  const [failure, setFailure] = useState<SubmitFailure | null>(null);
  const [confirming, setConfirming] = useState(false);
  const form = useRef<HTMLFormElement | null>(null);
  const formId = useId();
  useEffect(() => {
    if (!open) {
      setFailure(null);
      setConfirming(false);
    }
  }, [open]);

  const submit = async (): Promise<boolean> => {
    if (pending || busy || submitDisabled) return false;
    setPending(true);
    setFailure(null);
    try {
      const answer = await onSubmit();
      if (answer) {
        setFailure(answer);
        const target =
          (answer.field ? form.current?.querySelector<HTMLElement>(`#${CSS.escape(answer.field)}`) : null) ??
          form.current?.querySelector<HTMLElement>('[aria-invalid="true"]');
        target?.focus();
        return false;
      }
      return true;
    } catch (error) {
      setFailure({ reason: error instanceof Error ? error.message : String(error) });
      return false;
    } finally {
      setPending(false);
    }
  };

  const requestClose = () => {
    if (pending) return;
    if (dirty) {
      setConfirming(true);
    } else {
      onClose();
    }
  };

  return (
    <>
      {/* The editor stays open under the question, so Keep editing returns to
          it exactly as it was, focus included. */}
      <Modal
        open={open}
        inert={confirming}
        title={title}
        onClose={requestClose}
        size={size}
        footer={
          <>
            {failure ? (
              <p className="dialog-status" role="alert">
                {failure.reason}
              </p>
            ) : status ? (
              <div className="dialog-status">{status}</div>
            ) : null}
            <div className="dialog-footer">
              <button type="button" onClick={requestClose} disabled={pending}>
                Cancel
              </button>
              <button
                type="submit"
                form={formId}
                className={tone === "danger" ? "danger solid" : "primary"}
                disabled={submitDisabled || busy || pending}
              >
                {submitLabel}
              </button>
            </div>
          </>
        }
      >
        <form
          id={formId}
          ref={form}
          className="dialog-form"
          onSubmit={(event) => {
            event.preventDefault();
            void submit();
          }}
        >
          <div className="dialog-fields">{children}</div>
        </form>
      </Modal>
      <Modal open={open && confirming} title="Save changes?" size="small" onClose={() => setConfirming(false)}
        footer={
          <div className="dialog-footer">
            <button type="button" data-autofocus onClick={() => setConfirming(false)}>
              Keep editing
            </button>
            <button
              type="button"
              onClick={() => {
                setConfirming(false);
                onDiscard?.();
                onClose();
              }}
            >
              Discard
            </button>
            <button
              type="button"
              className="primary"
              disabled={submitDisabled || busy || pending}
              onClick={async () => {
                setConfirming(false);
                await submit();
              }}
            >
              Save
            </button>
          </div>
        }
      >
        <p>{title} has unsaved changes.</p>
      </Modal>
    </>
  );
}

export type MenuItem = {
  label: string;
  onSelect: () => void;
  disabled?: boolean | undefined;
  tone?: "danger";
  /** Drawn after a divider from the items before it. */
  separated?: boolean;
};

/** A menu of text items behind one button. Arrow keys, Home and End move
 * between the items; Escape closes the menu only and hands focus back to its
 * button; a chosen item closes it and runs. `trigger` is the button's visible
 * content; without one it is the More icon. */
export function Menu({
  label,
  items,
  trigger,
  className,
}: {
  label: string;
  items: MenuItem[];
  trigger?: ReactNode;
  className?: string;
}) {
  const [open, setOpen] = useState(false);
  const box = useRef<HTMLDivElement | null>(null);
  const button = () => box.current?.querySelector<HTMLButtonElement>("button") ?? null;
  const entries = () => Array.from(box.current?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]:not([disabled])') ?? []);
  useEffect(() => {
    if (!open) return;
    entries()[0]?.focus();
    const outside = (event: MouseEvent) => {
      if (box.current && !box.current.contains(event.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", outside);
    return () => document.removeEventListener("mousedown", outside);
  }, [open]);
  return (
    <div
      className={className ? `more-menu ${className}` : "more-menu"}
      ref={box}
      onKeyDown={(event) => {
        if (!open) return;
        const all = entries();
        const at = all.indexOf(document.activeElement as HTMLButtonElement);
        const move = (to: number) => {
          event.preventDefault();
          all[(to + all.length) % all.length]?.focus();
        };
        if (event.key === "Escape") {
          event.preventDefault();
          event.stopPropagation();
          setOpen(false);
          button()?.focus();
        } else if (event.key === "ArrowDown") move(at + 1);
        else if (event.key === "ArrowUp") move(at - 1);
        else if (event.key === "Home") move(0);
        else if (event.key === "End") move(all.length - 1);
      }}
    >
      {trigger ? (
        <button type="button" className="menu-trigger" aria-label={label} aria-haspopup="menu" aria-expanded={open} onClick={() => setOpen(!open)}>
          {trigger}
        </button>
      ) : (
        <IconButton icon="more" label={label} expanded={open} onClick={() => setOpen(!open)} />
      )}
      {open ? (
        <div className="menu-popover" role="menu" aria-label={label}>
          {items.map((item, index) => (
            <button
              key={`${index}:${item.label}`}
              type="button"
              role="menuitem"
              tabIndex={-1}
              disabled={item.disabled}
              className={[item.tone === "danger" ? "danger" : "", item.separated ? "separated" : ""].join(" ").trim() || undefined}
              onClick={() => {
                setOpen(false);
                button()?.focus();
                item.onSelect();
              }}
            >
              {item.label}
            </button>
          ))}
        </div>
      ) : null}
    </div>
  );
}
