// Help (views 12 and 44): the five tasks, a search over the articles bundled
// with this build, and one article at a time. Nothing here reaches a network
// or reads a project: articles are part of the build.
import { useEffect, useRef, useState, type ReactNode } from "react";
import {
  diagnostics,
  helpArticle,
  helpTopics,
  searchHelp,
  type Diagnostics,
  type HelpActionID,
  type HelpArticleResult,
  type HelpSummary,
  type Privacy,
} from "./bindings";
import { EmptyState, Modal, ValueRows } from "./layout";
import { platformName } from "./Storage";
import "./help.css";

function Chevron() {
  return (
    <svg viewBox="0 0 16 16" width="16" height="16" aria-hidden="true" focusable="false">
      <path d="M6 3l5 5-5 5" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

/** Rows that each open one article, as the Tools launcher's rows do. */
function ArticleRows({ label, articles, onOpen }: { label: string; articles: HelpSummary[]; onOpen: (id: string) => void }) {
  return (
    <ul className="launcher" aria-label={label}>
      {articles.map((article) => (
        <li key={article.id}>
          <button type="button" className="launcher-row" onClick={() => onOpen(article.id)}>
            <span>{article.title}</span>
            <Chevron />
          </button>
        </li>
      ))}
    </ul>
  );
}

/** Help's landing: the five tasks, in order. */
export function HelpTopics({ onOpen, onDemo, busy=false }: { onOpen: (id: string) => void; onDemo?: () => void; busy?: boolean }) {
  const [topics, setTopics] = useState<HelpSummary[] | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  useEffect(() => {
    void helpTopics().then((answer) => {
      if (answer.state === "completed") setTopics(answer.topics);
      else setFailure(answer.reason ?? "Help could not be read.");
    });
  }, []);
  if (failure) {
    return (
      <p className="form-status" role="alert">
        {failure}
      </p>
    );
  }
  return topics ? <div className="help-workspace"><aside className="help-topic-rail"><h2>Help topics</h2><ArticleRows label="Help topics" articles={topics} onOpen={onOpen} /></aside><section className="help-start" aria-label="Get started"><h2>Try the scheduling demo</h2><p>Follow synthetic messages through the existing capture, test, and report workflow.</p>{onDemo ? <button type="button" className="primary" disabled={busy} onClick={onDemo}>Open demo</button>:null}<h3>Common tasks</h3><ul className="help-common-tasks">{topics.map(topic=><li key={topic.id}><button type="button" className="link" onClick={()=>onOpen(topic.id)}>{({"import-messages":"Import captures","investigate-a-case":"Read selected messages","create-a-regression-test":"Create a test case","connect-a-test-system":"Set up targets","share-a-report":"Export retained evidence"} as Record<string,string>)[topic.id] || `Read ${topic.title}`}</button></li>)}</ul></section></div> : null;
}

/** Search help: a query over the bundled articles' titles and content. A
 * result's title opens it. */
export function SearchHelpSheet({ open, onClose, onOpen }: { open: boolean; onClose: () => void; onOpen: (id: string) => void }) {
  const [query, setQuery] = useState("");
  const [matches, setMatches] = useState<HelpSummary[] | null>(null);
  const asked = useRef(0);
  useEffect(() => {
    if (!open) {
      setQuery("");
      setMatches(null);
    }
  }, [open]);
  useEffect(() => {
    const text = query.trim();
    const turn = ++asked.current;
    if (!text) {
      setMatches(null);
      return;
    }
    void searchHelp(text).then((answer) => {
      if (turn === asked.current) setMatches(answer.state === "completed" || answer.state === "empty" ? answer.matches : []);
    });
  }, [query]);
  return (
    <Modal open={open} title="Search help" size="small" onClose={onClose}>
      <label htmlFor="help-query" className="visually-hidden">
        Search help
      </label>
      <input id="help-query" type="search" data-autofocus maxLength={200} value={query} onChange={(event) => setQuery(event.target.value)} />
      {matches && matches.length > 0 ? (
        <ArticleRows
          label="Matching topics"
          articles={matches}
          onOpen={(id) => {
            onClose();
            onOpen(id);
          }}
        />
      ) : null}
      {matches && matches.length === 0 ? (
        <EmptyState
          title="No matching topics"
          action={
            <button type="button" onClick={() => setQuery("")}>
              Clear search
            </button>
          }
        />
      ) : null}
    </Modal>
  );
}

/** One article: its task's steps or its explanation, and the articles it
 * links. A task article's action is the page's; a missing article says so and
 * is never replaced by another. */
export function useHelpArticle(id: string | undefined, onAction: (action: HelpActionID) => void) {
  const [answer, setAnswer] = useState<HelpArticleResult | null>(null);
  useEffect(() => {
    setAnswer(null);
    if (!id) return;
    let current = true;
    void helpArticle(id).then((read) => current && setAnswer(read));
    return () => {
      current = false;
    };
  }, [id]);
  const article = answer?.state === "completed" ? answer.article : undefined;
  return {
    title: article?.title ?? (answer ? "Help" : ""),
    actions: article?.action ? (
      <button type="button" className="primary" onClick={() => onAction(article.action!.id)}>
        {article.action.label}
      </button>
    ) : null,
    body: (onOpen: (id: string) => void): ReactNode =>
      !answer ? null : !article ? (
        <p className="form-status" role="alert">
          {answer.reason ? `${answer.reason[0]!.toUpperCase()}${answer.reason.slice(1)}.` : "This article cannot be read."}
        </p>
      ) : (
        <article className="help-article">
          {article.steps.length > 0 ? (
            <ol className="help-steps">
              {article.steps.map((step) => (
                <li key={step}>{step}</li>
              ))}
            </ol>
          ) : null}
          {article.body.map((paragraph) => (
            <p key={paragraph}>{paragraph}</p>
          ))}
          {article.related.length > 0 ? (
            <section aria-labelledby="help-related">
              <h2 id="help-related">Related</h2>
              <ArticleRows label="Related topics" articles={article.related.map((link) => ({ ...link, kind: "troubleshooting" }))} onOpen={onOpen} />
            </section>
          ) : null}
        </article>
      ),
  };
}

/** An operation as a short phrase: "StartBenchmark" reads "Start benchmark". */
function operationName(method: string): string {
  const words = method.replace(/([a-z0-9])([A-Z])/g, "$1 $2").split(" ");
  return words.map((word, index) => (index === 0 || /^[A-Z0-9]{2,}$/.test(word) ? word : word.toLowerCase())).join(" ");
}

/** Diagnostics, on demand: this build, what it supports and the errors the
 * window holds now. Nothing is sent anywhere. */
export function DiagnosticsSheet({
  open,
  errors,
  privacy,
  onClose,
  onExport,
}: {
  open: boolean;
  errors: string[];
  /** What this build states about privacy, as the facade describes it. */
  privacy?: Privacy | undefined;
  onClose: () => void;
  /** Opens the support summary's review; absent without a project. */
  onExport?: (() => void) | undefined;
}) {
  const [read, setRead] = useState<Diagnostics | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  useEffect(() => {
    if (!open) return;
    void diagnostics().then((answer) => {
      if (answer.state === "completed" && answer.diagnostics) setRead(answer.diagnostics);
      else setFailure(answer.reason ?? "Diagnostics could not be read.");
    });
  }, [open]);
  return (
    <Modal
      open={open}
      title="Diagnostics"
      onClose={onClose}
      footer={
        onExport ? (
          <button type="button" onClick={onExport}>
            Export support summary
          </button>
        ) : null
      }
    >
      {failure ? (
        <p className="form-status" role="alert">
          {failure}
        </p>
      ) : null}
      {read ? (
        <>
          <ValueRows
            rows={[
              { label: "Version", value: read.version },
              { label: "Platform", value: platformName(read.os, read.arch) },
              { label: "Go", value: read.go_version },
              ...(errors.length > 0 ? [{ label: "Current errors", value: errors.join("\n") }] : []),
            ]}
          />
          <h3>Operations</h3>
          <table>
            <thead>
              <tr>
                <th scope="col">Operation</th>
                <th scope="col">Stoppable</th>
                <th scope="col">Requires</th>
              </tr>
            </thead>
            <tbody>
              {read.operations.map((operation) => (
                <tr key={operation.method}>
                  <td>{operationName(operation.method)}</td>
                  <td>{operation.interruptible ? "Yes" : "No"}</td>
                  <td>{operation.prerequisites.length > 0 ? operation.prerequisites.map((need) => (need === "author" ? "License" : need === "execute" ? "Execution license" : need)).join(", ") : "—"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </>
      ) : null}
      {privacy ? (
        <section aria-labelledby="diagnostics-privacy">
          <h3 id="diagnostics-privacy">Privacy</h3>
          <p>{privacy.statement}</p>
          <ul className="plain-list">
            {privacy.absent.map((claim) => (
              <li key={claim}>{claim}</li>
            ))}
          </ul>
          <h4>Kept on this machine</h4>
          <ul className="plain-list">
            {privacy.kept.map((item) => (
              <li key={item}>{item}</li>
            ))}
          </ul>
        </section>
      ) : null}
    </Modal>
  );
}
