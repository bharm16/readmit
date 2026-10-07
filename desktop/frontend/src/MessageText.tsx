import { useEffect, useRef, useState, type KeyboardEvent } from "react";
import type { RawLine, RawToken } from "./bindings";

const tokenKey = (token: RawToken) => `${token.start}:${token.end}:${token.path || ""}`;

/** Only renders Go's source projection. Paths never come from displayed text. */
export function MessageText({ lines, busy, onSelect, followSelection = true }: { lines: RawLine[]; busy: boolean; followSelection?: boolean; onSelect?: ((path: string) => void) | undefined }) {
  const container = useRef<HTMLDivElement>(null);
  const [focused, setFocused] = useState("");
  const tokens = lines.flatMap(line => line.tokens).filter(token => token.path);
  const active = tokens.find(token => tokenKey(token) === focused) ?? tokens.find(token => token.selected) ?? tokens[0];
  const selection = tokens.filter(token => token.selected).map(tokenKey).join("/");
  useEffect(() => {
    if (!followSelection) return;
    container.current?.querySelector<HTMLElement>(".raw-token-selected")?.scrollIntoView?.({ block: "nearest", inline: "nearest" });
  }, [selection,followSelection]);
  const keyDown = (event: KeyboardEvent<HTMLSpanElement>, token: RawToken) => {
    if (busy || !onSelect || !token.path) return;
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      onSelect(token.path);
      return;
    }
    const controls = Array.from(container.current?.querySelectorAll<HTMLElement>("[data-source-path]") ?? []);
    const index = controls.indexOf(event.currentTarget);
    const next = event.key === "ArrowRight" ? index + 1 : event.key === "ArrowLeft" ? index - 1 : event.key === "Home" ? 0 : event.key === "End" ? controls.length - 1 : -1;
    if (next >= 0 && next < controls.length) {
      event.preventDefault();
      controls[next]?.focus();
    }
  };
  return <div ref={container} className="raw-source" aria-label="HL7 message text">
    <pre className="value raw raw-numbered">{lines.map(line => <span key={line.number} className={`raw-line${line.tokens.some(token => token.selected) ? " raw-line-selected" : ""}`}>
      <span className="raw-line-number" aria-hidden="true">{line.number}</span>
      <span className="raw-line-code">{line.tokens.map(token => {
        const interactive = Boolean(token.path && onSelect);
        return <span key={tokenKey(token)} className={`raw-token${token.role ? ` syntax-${token.role}` : ""}${token.selected ? " raw-token-selected" : ""}${token.empty ? " raw-token-empty" : ""}`}
          {...(interactive ? { role: "button", "aria-label": `Inspect ${token.path}${token.empty ? " (empty)" : ""}`, "aria-disabled": busy, "aria-pressed": Boolean(token.selected), "data-source-path": token.path, tabIndex: active === token ? 0 : -1 } : {})}
          onFocus={() => setFocused(tokenKey(token))}
          onKeyDown={event => keyDown(event, token)}
          onClick={() => { if (!busy && token.path && !window.getSelection()?.toString()) onSelect?.(token.path); }}
        >{token.text}</span>;
      })}</span>
    </span>)}</pre>
  </div>;
}
