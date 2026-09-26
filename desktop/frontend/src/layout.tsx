// The frame every destination of the window is drawn in: one page title, the
// page's own actions beside it, an optional row of views, and the body that
// scrolls on its own. Nothing here decides anything about evidence; it only
// keeps every page laid out the same way.
import { useEffect, useRef, useState, type ReactElement, type ReactNode } from "react";
import { IconButton } from "./IconButton";

/** The window's destinations. Home is the project list; the next five exist
 * while a project or folder is open; the last three are always available. */
export type Destination =
  | "home"
  | "cases"
  | "tests"
  | "runs"
  | "environments"
  | "reports"
  | "tools"
  | "settings"
  | "help";

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

/** One page. It stays mounted while another is shown, so an unfinished edit
 * survives looking elsewhere; only the shown page is in the accessibility
 * tree. */
export function Page({
  id,
  shown,
  title,
  eyebrow,
  subtitle,
  actions,
  children,
}: {
  id: string;
  shown: boolean;
  title: ReactNode;
  /** A small line above the title: where this page sits, or a way back. */
  eyebrow?: ReactNode;
  /** One quiet line under the title: what the page holds, in numbers. */
  subtitle?: ReactNode;
  actions?: ReactNode;
  children: ReactNode;
}) {
  return (
    <div className="page" data-page={id} hidden={!shown}>
      <header className="page-header">
        <div className="page-heading">
          {eyebrow ? <div className="page-eyebrow">{eyebrow}</div> : null}
          <h1>{title}</h1>
          {subtitle ? <p className="page-subtitle">{subtitle}</p> : null}
        </div>
        {actions ? <div className="page-actions">{actions}</div> : null}
      </header>
      <div className="page-body">{children}</div>
    </div>
  );
}

/** What a page shows when there is nothing to work on yet: one sentence and
 * the one action that changes that, instead of a form that cannot be used. */
export function EmptyState({ title, children, action }: { title: string; children?: ReactNode; action?: ReactNode }) {
  return (
    <div className="empty-state">
      <p className="empty-title">{title}</p>
      {children ? <p className="empty-body">{children}</p> : null}
      {action ? <div className="empty-action">{action}</div> : null}
    </div>
  );
}

const navIcons: Record<Destination, ReactElement> = {
  home: <path d="M3 7.5 8 3l5 4.5V13H3z" />,
  cases: (
    <>
      <path d="M2.5 4.5h4l1.2 1.5h5.8v6.5h-11z" />
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
 * alone. */
export function NavItem({
  id,
  label,
  current,
  onSelect,
  badge,
}: {
  id: Destination;
  label: string;
  current: boolean;
  onSelect: (id: Destination) => void;
  badge?: ReactNode;
}) {
  return (
    <li>
      <button
        type="button"
        className="nav-item"
        title={label}
        aria-current={current ? "page" : undefined}
        onClick={() => onSelect(id)}
      >
        <svg
          className="nav-icon"
          viewBox="0 0 16 16"
          width="16"
          height="16"
          aria-hidden="true"
          focusable="false"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.4"
          strokeLinecap="round"
          strokeLinejoin="round"
        >
          {navIcons[id]}
        </svg>
        <span className="nav-label">{label}</span>
        {badge}
      </button>
    </li>
  );
}

/** A modal sheet: focus moves into it when it opens and back to where it was
 * when it closes, Escape closes it, and the X names what it closes. */
export function Modal({
  open,
  title,
  onClose,
  children,
}: {
  open: boolean;
  title: string;
  onClose: () => void;
  children: ReactNode;
}) {
  const dialog = useRef<HTMLDialogElement | null>(null);
  useEffect(() => {
    const element = dialog.current;
    if (!element) return;
    if (open && !element.open) {
      element.showModal();
    } else if (!open && element.open) {
      element.close();
    }
  }, [open]);
  return (
    <dialog className="modal" ref={dialog} aria-label={title} onClose={onClose}>
      {open ? (
        <>
          <header className="modal-header">
            <h2>{title}</h2>
            <IconButton icon="close" label={`Close ${title.toLowerCase()}`} onClick={onClose} />
          </header>
          <div className="modal-body">{children}</div>
        </>
      ) : null}
    </dialog>
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

/** A form that appears only when a person asks for it: a modal sheet with its
 * fields and, at the bottom, Cancel and the one action that submits it. The
 * page behind it never carries empty fields waiting to be filled. */
export function FormDialog({
  open,
  title,
  onClose,
  onSubmit,
  submitLabel,
  submitDisabled = false,
  busy = false,
  tone = "primary",
  status,
  children,
}: {
  open: boolean;
  title: string;
  onClose: () => void;
  onSubmit: () => void;
  submitLabel: string;
  submitDisabled?: boolean;
  busy?: boolean;
  /** A destructive action reads as one. */
  tone?: "primary" | "danger";
  /** An answer to show beside the actions: a refusal or a validation message. */
  status?: ReactNode;
  children: ReactNode;
}) {
  return (
    <Modal open={open} title={title} onClose={onClose}>
      <form
        className="dialog-form"
        onSubmit={(event) => {
          event.preventDefault();
          if (!submitDisabled && !busy) onSubmit();
        }}
      >
        <div className="dialog-fields">{children}</div>
        {status ? <div className="dialog-status">{status}</div> : null}
        <div className="dialog-footer">
          <button type="button" onClick={onClose}>
            Cancel
          </button>
          <button type="submit" className={tone === "danger" ? "danger solid" : "primary"} disabled={submitDisabled || busy}>
            {submitLabel}
          </button>
        </div>
      </form>
    </Modal>
  );
}

/** A "more actions" menu: the actions a screen offers but rarely needs, kept
 * out of the way until asked for. Escape or a click elsewhere closes it; a
 * chosen item closes it and runs. */
export function MoreMenu({
  label,
  items,
}: {
  label: string;
  items: { label: string; onSelect: () => void; disabled?: boolean; tone?: "danger" }[];
}) {
  const [open, setOpen] = useState(false);
  const box = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    if (!open) return;
    const outside = (event: MouseEvent) => {
      if (box.current && !box.current.contains(event.target as Node)) setOpen(false);
    };
    // Escape answers the menu and goes no further: the window's own Escape
    // cancels a running operation.
    const escape = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.stopPropagation();
        setOpen(false);
      }
    };
    document.addEventListener("mousedown", outside);
    document.addEventListener("keydown", escape, true);
    return () => {
      document.removeEventListener("mousedown", outside);
      document.removeEventListener("keydown", escape, true);
    };
  }, [open]);
  return (
    <div className="more-menu" ref={box}>
      <IconButton icon="more" label={label} expanded={open} onClick={() => setOpen(!open)} />
      {open ? (
        <div className="menu-popover" role="menu" aria-label={label}>
          {items.map((item) => (
            <button
              key={item.label}
              type="button"
              role="menuitem"
              disabled={item.disabled}
              className={item.tone === "danger" ? "danger" : undefined}
              onClick={() => {
                setOpen(false);
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
