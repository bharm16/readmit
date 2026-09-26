// The building blocks every panel is laid out with, so each one reads the same
// way: one sentence saying what the panel is for, then titled groups, each with
// its fields (label above, hint below), one row of actions, and what those
// actions produced set apart underneath. Secondary material goes last, folded.
// None of it decides anything; it only gives the content an order to follow.
import type { ReactNode } from "react";

/** The one sentence at the top of a panel saying what it is for. */
export function Lead({ children }: { children: ReactNode }) {
  return <p className="lead">{children}</p>;
}

/** A titled part of a panel. The title says what the group does; the
 * description, at most one sentence, says what a person needs to know first. */
export function Group({
  title,
  description,
  aside,
  className,
  children,
  label,
}: {
  title?: ReactNode;
  description?: ReactNode;
  /** A control that belongs to the whole group, drawn beside its title. */
  aside?: ReactNode;
  className?: string;
  children?: ReactNode;
  /** An accessible name for the group when it has no visible title. */
  label?: string;
}) {
  return (
    <div className={className ? `group ${className}` : "group"} {...(label ? { role: "group", "aria-label": label } : {})}>
      {title || aside ? (
        <div className="group-header">
          <div>
            {title ? <h4 className="group-title">{title}</h4> : null}
            {description ? <p className="group-description">{description}</p> : null}
          </div>
          {aside ? <div className="group-aside">{aside}</div> : null}
        </div>
      ) : description ? (
        <p className="group-description">{description}</p>
      ) : null}
      {children}
    </div>
  );
}

/** Fields side by side where there is room, one per row where there is not. */
export function Fields({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={className ? `fields ${className}` : "fields"}>{children}</div>;
}

/** One labelled control: the label above it and, when needed, one short hint
 * below it. The label is the control's accessible name through htmlFor. */
export function Field({
  label,
  htmlFor,
  hint,
  wide,
  children,
}: {
  label: ReactNode;
  htmlFor?: string;
  hint?: ReactNode;
  /** Spans the whole row, for long values and lists. */
  wide?: boolean;
  children: ReactNode;
}) {
  return (
    <div className={wide ? "field field-wide" : "field"}>
      {htmlFor ? <label htmlFor={htmlFor}>{label}</label> : <span className="field-label">{label}</span>}
      {children}
      {hint ? <p className="field-hint">{hint}</p> : null}
    </div>
  );
}

/** The actions a group ends with, in one row. The main one is marked primary
 * by the caller; a destructive one is marked danger. */
export function Actions({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={className ? `actions-bar ${className}` : "actions-bar"}>{children}</div>;
}

/** Counts that summarise a result, each a number over its word, so the
 * numbers are read first and in the same place every time. */
export function Stats({
  items,
  label,
}: {
  items: { label: ReactNode; value: ReactNode; tone?: "warning" | "danger" | "success" | "muted" }[];
  label?: string;
}) {
  return (
    <dl className="stats" {...(label ? { "aria-label": label } : {})}>
      {items.map((item, index) => (
        <div key={index} className={item.tone ? `stat stat-${item.tone}` : "stat"}>
          <dt>{item.label}</dt>
          <dd>{item.value}</dd>
        </div>
      ))}
    </dl>
  );
}

/** Named facts about one thing, as label and value pairs. */
export function Facts({ items, className }: { items: [ReactNode, ReactNode][]; className?: string }) {
  return (
    <dl className={className ? `facts ${className}` : "facts"}>
      {items.map(([name, value], index) => (
        <div key={index} className="fact">
          <dt>{name}</dt>
          <dd>{value}</dd>
        </div>
      ))}
    </dl>
  );
}

/** What an action produced, set apart from the controls that produced it. */
export function Result({ title, children, label }: { title?: ReactNode; children: ReactNode; label?: string }) {
  return (
    <div className="result" {...(label ? { role: "region", "aria-label": label } : {})}>
      {title ? <h5 className="result-title">{title}</h5> : null}
      {children}
    </div>
  );
}

/** A consequence a person needs before acting, stated once beside the action.
 * The tone adds colour to a rule beside the words, never instead of them. */
export function Note({ tone = "info", children }: { tone?: "info" | "warning" | "danger" | "success"; children: ReactNode }) {
  return <div className={`note note-${tone}`}>{children}</div>;
}

/** Material a person rarely needs — reference detail, advanced settings —
 * folded at the end of a panel rather than in the way of its work. */
export function More({ summary, children, className }: { summary: ReactNode; children: ReactNode; className?: string }) {
  return (
    <details className={className ? `more ${className}` : "more"}>
      <summary>{summary}</summary>
      <div className="more-body">{children}</div>
    </details>
  );
}
