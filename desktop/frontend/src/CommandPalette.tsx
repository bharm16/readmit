// The command palette: one focused search input over the actions the
// current object offers and the window's destinations. It searches names only,
// never patient values. A chosen entry runs the same thing its button does, so
// a consequential one opens its normal review rather than acting from Enter.
import { useEffect, useId, useRef, useState } from "react";
import { Dialog } from "./layout";
import { IconButton } from "./IconButton";

export type PaletteEntry = {
  /** Stable, unique among the entries. */
  id: string;
  label: string;
  /** The platform's shortcut, as a person presses it. */
  keys?: string;
  run: () => void;
};

/** The entries a query matches, best first: a label that starts with the
 * query, then one with a later word that does, then one that contains it, each
 * tier in label order. An empty query keeps the given order, which is the
 * current object's actions and then the destinations. */
export function rankEntries(entries: PaletteEntry[], query: string): PaletteEntry[] {
  const wanted = query.trim().toLowerCase();
  if (wanted === "") return entries;
  const tier = (label: string): number => {
    const lower = label.toLowerCase();
    if (lower.startsWith(wanted)) return 0;
    if (lower.split(/\s+/).some((word) => word.startsWith(wanted))) return 1;
    if (lower.includes(wanted)) return 2;
    return 3;
  };
  return entries
    .map((entry) => ({ entry, tier: tier(entry.label) }))
    .filter((ranked) => ranked.tier < 3)
    .sort((a, b) => a.tier - b.tier || a.entry.label.localeCompare(b.entry.label))
    .map((ranked) => ranked.entry);
}

export function CommandPalette({
  open,
  entries,
  onClose,
}: {
  open: boolean;
  entries: PaletteEntry[];
  onClose: () => void;
}) {
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  const list = useId();
  const input = useRef<HTMLInputElement | null>(null);
  const results = rankEntries(entries, query);
  const selected = Math.min(active, Math.max(0, results.length - 1));

  useEffect(() => {
    if (open) {
      setQuery("");
      setActive(0);
    }
  }, [open]);
  useEffect(() => {
    document.getElementById(`${list}-${selected}`)?.scrollIntoView?.({ block: "nearest" });
  }, [list, selected]);

  const choose = (entry: PaletteEntry | undefined) => {
    if (!entry) return;
    onClose();
    entry.run();
  };

  return (
    <Dialog open={open} label="Commands" onClose={onClose} className="palette">
      <div className="palette-search">
        <input
          ref={input}
          type="text"
          role="combobox"
          aria-label="Search commands"
          aria-expanded={results.length > 0}
          aria-controls={list}
          aria-activedescendant={results.length > 0 ? `${list}-${selected}` : undefined}
          autoComplete="off"
          spellCheck={false}
          value={query}
          onChange={(event) => {
            setQuery(event.target.value);
            setActive(0);
          }}
          onKeyDown={(event) => {
            const last = results.length - 1;
            const to = (index: number) => {
              event.preventDefault();
              setActive(Math.min(Math.max(0, index), Math.max(0, last)));
            };
            if (event.key === "ArrowDown") to(selected + 1);
            else if (event.key === "ArrowUp") to(selected - 1);
            else if (event.key === "Home") to(0);
            else if (event.key === "End") to(last);
            else if (event.key === "Enter") {
              event.preventDefault();
              choose(results[selected]);
            }
          }}
        />
        <IconButton icon="close" label="Close commands" onClick={onClose} />
      </div>
      {results.length > 0 ? (
        <ul className="palette-results" id={list} role="listbox" aria-label="Commands">
          {results.map((entry, index) => (
            <li
              key={entry.id}
              id={`${list}-${index}`}
              role="option"
              aria-selected={index === selected}
              onMouseMove={() => setActive(index)}
              onClick={() => choose(entry)}
            >
              <span className="name">{entry.label}</span>
              {entry.keys ? <kbd>{entry.keys}</kbd> : null}
            </li>
          ))}
        </ul>
      ) : (
        <div className="palette-empty">
          <p>No commands found</p>
          <button
            type="button"
            onClick={() => {
              setQuery("");
              input.current?.focus();
            }}
          >
            Clear search
          </button>
        </div>
      )}
    </Dialog>
  );
}

/** The platform's name for a shortcut: ⌘ on macOS, Ctrl elsewhere. */
export function shortcut(keys: string, mac: boolean = isMac()): string {
  return mac ? keys.replace(/Ctrl\+/g, "⌘").replace(/Shift\+/g, "⇧") : keys;
}

export function isMac(): boolean {
  return typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent);
}
