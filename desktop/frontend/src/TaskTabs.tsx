import type { ReactNode } from "react";
import "./task-tabs.css";

/** A set of task views: a real tablist with one tab stop, whose arrow keys,
 * Home and End move the selection, and the tabpanel each tab controls.
 *
 * By default only the selected task is rendered, in one tabpanel the
 * selected tab names. With `keepMounted` every task stays mounted, the
 * inactive ones hidden, so an unfinished edit survives looking at another
 * task; the children are then one `TaskPanel` per tab. */
export function TaskTabs<K extends string>(props: {
  label: string;
  id: string;
  tabs: { key: K; label: string }[];
  selected: K;
  onSelect: (key: K) => void;
  children: ReactNode;
  keepMounted?: boolean;
  /** The classes of the tablist and of the tabpanel, for the panel's styles. */
  tablistClass?: string;
  panelClass?: string;
}) {
  const { label, id, tabs, selected, onSelect, keepMounted = false, tablistClass = "task-tabs", panelClass = "task-panel" } = props;
  return (
    <>
      <div className={tablistClass} role="tablist" aria-label={label}>
        {tabs.map((tab, position) => (
          <button
            key={tab.key}
            type="button"
            role="tab"
            id={tabId(id, tab.key)}
            aria-selected={selected === tab.key}
            aria-controls={keepMounted ? panelId(id, tab.key) : `${id}-panel`}
            tabIndex={selected === tab.key ? 0 : -1}
            onClick={() => onSelect(tab.key)}
            onKeyDown={(event) => {
              const move = (to: number) => {
                event.preventDefault();
                const next = tabs[to]?.key ?? tab.key;
                onSelect(next);
                document.getElementById(tabId(id, next))?.focus();
              };
              if (event.key === "ArrowRight") move((position + 1) % tabs.length);
              else if (event.key === "ArrowLeft") move((position - 1 + tabs.length) % tabs.length);
              else if (event.key === "Home") move(0);
              else if (event.key === "End") move(tabs.length - 1);
            }}
          >
            {tab.label}
          </button>
        ))}
      </div>
      {keepMounted ? (
        props.children
      ) : (
        <div role="tabpanel" id={`${id}-panel`} aria-labelledby={tabId(id, selected)} className={panelClass}>
          {props.children}
        </div>
      )}
    </>
  );
}

/** One task of a `keepMounted` TaskTabs: always mounted, hidden while another
 * task is shown. `tabs` is that TaskTabs' id and `tab` this task's key. */
export function TaskPanel(props: { tabs: string; tab: string; shown: boolean; className?: string; children: ReactNode }) {
  const { tabs, tab, shown, className = "task-panel" } = props;
  return (
    <div role="tabpanel" id={panelId(tabs, tab)} aria-labelledby={tabId(tabs, tab)} hidden={!shown} className={className}>
      {props.children}
    </div>
  );
}

function tabId(id: string, key: string): string {
  return `${id}-tab-${key}`;
}

function panelId(id: string, key: string): string {
  return `${id}-panel-${key}`;
}
