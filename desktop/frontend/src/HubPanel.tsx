import { useCallback, useEffect, useRef, useState } from "react";
import "./hub.css";
import {
  chooseHubTeamConfig,
  completeHubAuth,
  connectHub,
  diagnoseHub,
  discardEditorDraft,
  disconnectHub,
  hubStatus,
  readHubTeam,
  saveHubTeam,
  startHubAuth,
  type Artifact,
  type HubCheckItem,
  type HubConfigChoice,
  type HubResult,
  type HubTeamResult,
} from "./bindings";
import { TeamCollaboration, RevisionSheet } from "./TeamCollaboration";
import { TeamAdministratorSetup } from "./HubAdministration";
import { EmptyState, FormDialog, Menu, Modal, ValueRows } from "./layout";
import { useLifecycle } from "./lifecycle";
import { ViewKey, useViewState } from "./viewstate";

/** Settings › Team: the one named team this window works with. Without a
 * configuration it offers Connect team; configured, Sign in, one flow that
 * connects, checks what it needs and signs in through the person's own
 * browser; signed in, the selected project's Activity, Files and Reviews,
 * read through that session. Administrator setup holds the named tasks. */
export function HubPanel({
  workspace,
  entries = [],
  request = 0,
  onHandled,
  onConfigured,
  operatorRequest = 0,
  onOperatorHandled,
  onOperatorConfigured,
}: {
  workspace?: string | null;
  entries?: Artifact[];
  /** Each new value opens Connect team once, as Security's Add connection ›
   * Team does. */
  request?: number;
  onHandled?: () => void;
  /** Connect team ended: saved, or not. */
  onConfigured?: (chosen: boolean) => void;
  /** The same, for the operator hub in Administrator setup. */
  operatorRequest?: number;
  onOperatorHandled?: () => void;
  onOperatorConfigured?: (chosen: boolean) => void;
}) {
  const [status, setStatus] = useViewState<HubResult | null>("HubPanel.status", null);
  const [project, setProject] = useViewState<string | null>("HubPanel.project", null);
  const [page, setPage] = useViewState<"team" | "admin">("HubPanel.page", "team");
  const [sheet, setSheet] = useState<null | "connect" | "signin" | "session">(null);
  const [revision, setRevision] = useState<{ resource: string; base: string; source?: string; draft?: string } | null>(null);
  const [team, setTeam] = useState<HubTeamResult | null>(null);
  const [operatorAsk, setOperatorAsk] = useState(0);
  const reads = useLifecycle<"status" | "team">({ background: true });
  const actions = useLifecycle<"signout">();

  const authenticated = status?.authenticated ?? false;
  const configured = status?.state === "completed" && Boolean(status.config_path);
  const authorized = (status?.projects ?? []).filter((entry) => entry.authorized);
  const selected = authorized.find((entry) => entry.project === project)?.project ?? authorized[0]?.project ?? null;
  const unreadable = authorized.length === 0 ? (status?.projects ?? []).find((entry) => entry.reason)?.reason ?? null : null;
  // The project last opened, which offline revision drafts are kept for.
  const drafting = selected ?? project;
  useEffect(() => {
    if (selected && selected !== project) setProject(selected);
  }, [selected]); // eslint-disable-line react-hooks/exhaustive-deps
  const context = `${status?.session_scope ?? `${status?.hub_url ?? ""}|${status?.expires_at ?? ""}`}|${status?.config_path ?? ""}|${status?.issuer ?? ""}|${status?.subject ?? ""}|${selected ?? ""}`;
  // The context the team on screen was read for. A reply for any other one —
  // asked before the team, the person or the project changed — is dropped.
  const shownFor = useRef("");

  const refreshStatus = useCallback(async () => {
    const answer = await reads.run("status", () => hubStatus());
    if (answer) setStatus(answer);
  }, [reads, setStatus]);

  useEffect(() => {
    void refreshStatus();
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  const readTeam = useCallback(async () => {
    if (!authenticated || !selected) return;
    const asked = context;
    const answer = await reads.run("team", () => readHubTeam({ project: selected, workspace: workspace ?? "" }));
    if (!answer || shownFor.current !== asked) return;
    if (answer.state === "permission_denied") {
      // An expired session or a withdrawn grant: what the hub says now is
      // what the page shows, with Sign in where it is needed.
      await refreshStatus();
    }
    setTeam(answer);
  }, [authenticated, context, reads, refreshStatus, selected, workspace]);

  // After a deliberate sign-in the project's metadata is read through that
  // session, and read again whenever the team, the person or the project
  // changes; the previous context's data is gone first.
  useEffect(() => {
    shownFor.current = context;
    setTeam(null);
    if (authenticated && selected) void readTeam();
  }, [context]); // eslint-disable-line react-hooks/exhaustive-deps

  // A request is handled once: a remount, or StrictMode's second run, does
  // not open Connect team again.
  const handled = useRef(0);
  useEffect(() => {
    if (request === 0) handled.current = 0;
    if (request === 0 || request === handled.current) return;
    handled.current = request;
    onHandled?.();
    setPage("team");
    setSheet("connect");
  }, [request]); // eslint-disable-line react-hooks/exhaustive-deps
  const operatorHandled = useRef(0);
  useEffect(() => {
    if (operatorRequest === 0) operatorHandled.current = 0;
    if (operatorRequest === 0 || operatorRequest === operatorHandled.current) return;
    operatorHandled.current = operatorRequest;
    onOperatorHandled?.();
    setPage("admin");
    setOperatorAsk((count) => count + 1);
  }, [operatorRequest]); // eslint-disable-line react-hooks/exhaustive-deps

  const signOut = async () => {
    const answer = await actions.run("signout", () => disconnectHub());
    if (answer) setStatus(answer);
  };

  const teamName = status?.team || hostName(status?.hub_url) || "Team";
  const moreItems = [
    ...(configured ? [{ label: "Edit team", onSelect: () => setSheet("connect") }] : []),
    { label: "Administrator setup", onSelect: () => setPage("admin") },
  ];

  const connectSheet = (
    <ConnectTeamSheet
      open={sheet === "connect"}
      current={status}
      onClose={() => {
        setSheet(null);
        onConfigured?.(false);
      }}
      onSaved={(saved) => {
        setStatus(saved);
        setProject(null);
        setSheet(null);
        onConfigured?.(true);
      }}
    />
  );
  const signInSheet = (
    <SignInSheet
      open={sheet === "signin"}
      status={status}
      onEdit={() => setSheet("connect")}
      onClose={() => setSheet(null)}
      onSignedIn={(signedIn) => {
        setStatus(signedIn);
        setSheet(null);
      }}
      onStatus={setStatus}
    />
  );

  const revisionSheet = () => {
    if (!drafting) return null;
    return (
      <RevisionSheet
        open={revision !== null}
        project={drafting}
        teamName={teamName}
        workspace={workspace ?? ""}
        resource={revision?.resource ?? ""}
        base={revision?.base ?? ""}
        source={revision?.source ?? ""}
        resources={team?.state === "completed" ? team.resources : []}
        onClose={() => setRevision(null)}
        onDone={(result) => {
          // A submitted draft is done: it is no longer kept to submit again.
          if (result.outcome === "completed" && revision?.draft) void discardEditorDraft(revision.draft);
          void readTeam();
        }}
      />
    );
  };

  if (page === "admin") {
    return (
      <section className="team-view" aria-label="Administrator setup">
        <ViewKey key={context} id={context}>
        <TeamAdministratorSetup
          status={status}
          project={drafting}
          team={team}
          teamName={teamName}
          workspace={workspace ?? ""}
          operatorRequest={operatorAsk}
          onOperatorConfigured={onOperatorConfigured}
          onBack={() => setPage("team")}
          onRead={readTeam}
          onCreateRevision={(resource, base, source, draft) => setRevision({ resource, base, ...(source ? { source } : {}), ...(draft ? { draft } : {}) })}
        />
        {revisionSheet()}
        </ViewKey>
      </section>
    );
  }

  if (!status) return null;

  if (!configured) {
    return (
      <section className="team-view" aria-label="Team">
        <div className="team-header">
          <span className="team-name">Team</span>
          <Menu label="More team actions" items={moreItems} />
        </div>
        {status.state === "failed" && status.reason ? <p role="alert">{status.reason}</p> : null}
        <EmptyState
          title="No team configured"
          action={
            <button type="button" className="primary" onClick={() => setSheet("connect")}>
              Connect team
            </button>
          }
        />
        {connectSheet}
      </section>
    );
  }

  if (!authenticated) {
    return (
      <section className="team-view" aria-label="Team">
        <div className="team-header">
          <span className="team-name">{teamName}</span>
          <Menu label="More team actions" items={moreItems} />
        </div>
        <EmptyState
          title={status.reason && /expired/i.test(status.reason) ? "Session ended" : "Not connected"}
          action={
            <button type="button" className="primary" onClick={() => setSheet("signin")}>
              Sign in
            </button>
          }
        />
        {connectSheet}
        {signInSheet}
      </section>
    );
  }

  return (
    <section className="team-view" aria-label="Team">
      <div className="team-header">
        <span className="team-name">{teamName}</span>
        <span className="team-separator" aria-hidden="true">
          /
        </span>
        {authorized.length > 1 ? (
          <select aria-label="Project" value={selected ?? ""} onChange={(event) => setProject(event.target.value)}>
            {authorized.map((entry) => (
              <option key={entry.project} value={entry.project}>
                {entry.project}
              </option>
            ))}
          </select>
        ) : (
          <span className="team-project-name">{selected ?? "No project"}</span>
        )}
        <Menu
          className="team-account"
          label="Account"
          trigger={status.subject}
          items={[
            { label: "Session details", onSelect: () => setSheet("session") },
            { label: "Edit team", onSelect: () => setSheet("connect") },
            { label: "Administrator setup", onSelect: () => setPage("admin") },
            { label: "Sign out", onSelect: () => void signOut(), separated: true, disabled: actions.running !== null },
          ]}
        />
      </div>
      {selected === null ? (
        <>
          {/* A project the hub could not be asked about says why, rather
              than reading as one this person may not open. */}
          {unreadable ? <p role="alert">{unreadable}</p> : null}
          <EmptyState title="No projects you can open" />
        </>
      ) : team === null ? (
        <p aria-live="polite">Reading…</p>
      ) : team.state !== "completed" ? (
        <p role="alert">{team.reason ?? "The project could not be read."}</p>
      ) : (
        // Another team, person or project is another context: the view
        // starts over rather than keeping another one's selections.
        <ViewKey key={context} id={context}>
          <TeamCollaboration
            project={selected}
            teamName={teamName}
            team={team}
            workspace={workspace ?? ""}
            entries={entries}
            onRead={readTeam}
            onCreateRevision={(resource, base) => setRevision({ resource, base })}
          />
        </ViewKey>
      )}
      <Modal open={sheet === "session"} title="Session" size="small" onClose={() => setSheet(null)}>
        <ValueRows
          label="Session"
          rows={[
            { label: "Signed in as", value: status.subject ?? "—" },
            { label: "Identity provider", value: status.issuer ?? "—" },
            { label: "Team", value: status.hub_url ?? "—" },
            { label: "Session ends", value: status.expires_at ? new Date(status.expires_at).toLocaleString() : "—" },
          ]}
        />
      </Modal>
      {connectSheet}
      {revisionSheet()}
    </section>
  );
}

function hostName(address: string | undefined): string {
  if (!address) return "";
  try {
    return new URL(address).hostname;
  } catch {
    return "";
  }
}

/** Connect team: a name and the configuration the organization provided,
 * which carries the team's address, certificates, key locator and identity
 * provider. Save selects it; it never connects. */
function ConnectTeamSheet({
  open,
  current,
  onClose,
  onSaved,
}: {
  open: boolean;
  current: HubResult | null;
  onClose: () => void;
  onSaved: (status: HubResult) => void;
}) {
  const [name, setName] = useState("");
  const [choice, setChoice] = useState<HubConfigChoice | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  // A remembered configuration that no longer validates is not a team to
  // edit: the sheet connects one afresh, as the page offered.
  const saved = current?.state === "completed" && current.config_path ? current : null;
  useEffect(() => {
    if (!open) return;
    setName(saved?.team ?? "");
    setChoice(saved?.config_path ? { state: "completed", config: saved.config_path, ...(saved.hub_url ? { hub_url: saved.hub_url } : {}) } : null);
    setProblem(null);
  }, [open]); // eslint-disable-line react-hooks/exhaustive-deps
  const choose = async () => {
    const answer = await chooseHubTeamConfig();
    if (answer.state === "completed") {
      setChoice(answer);
      setProblem(null);
      if (!name.trim() && answer.name) setName(answer.name);
    } else if (answer.state !== "cancelled") {
      setProblem(answer.reason ?? "This configuration cannot be used.");
    }
  };
  return (
    <FormDialog
      open={open}
      title={saved ? "Edit team" : "Connect team"}
      submitLabel="Save"
      submitDisabled={!choice?.config || name.trim() === ""}
      dirty={choice?.config !== saved?.config_path || name.trim() !== (saved?.team ?? "")}
      onClose={onClose}
      status={problem ? <p role="alert">{problem}</p> : undefined}
      onSubmit={async () => {
        const saved = await saveHubTeam({ name: name.trim(), config: choice?.config ?? "" });
        if (saved.state !== "completed") return { reason: saved.reason ?? "The team was not saved.", field: "team-name" };
        onSaved(saved);
        return null;
      }}
    >
      <label htmlFor="team-name">Name</label>
      <input id="team-name" maxLength={100} value={name} onChange={(event) => setName(event.target.value)} />
      <label htmlFor="team-config">Configuration</label>
      <div className="field-with-action">
        <span id="team-config">{choice?.hub_url ? hostName(choice.hub_url) || choice.hub_url : "—"}</span>
        <button type="button" onClick={() => void choose()}>
          {choice ? "Replace…" : "Choose file…"}
        </button>
      </div>
      {choice?.projects && choice.projects.length > 0 ? <ValueRows label="Configuration" rows={[{ label: "Projects", value: choice.projects.join(", ") }]} /> : null}
    </FormDialog>
  );
}

type Step = { key: "check" | "connect" | "browser"; state: "waiting" | "running" | "done" | "failed" };

/** Sign in: one flow, started by the person, that checks the team's setup,
 * connects and signs in through the identity provider in their own browser.
 * A step that fails says why and offers Edit; nothing is retried. */
function SignInSheet({
  open,
  status,
  onEdit,
  onClose,
  onSignedIn,
  onStatus,
}: {
  open: boolean;
  status: HubResult | null;
  onEdit: () => void;
  onClose: () => void;
  onSignedIn: (status: HubResult) => void;
  onStatus: (status: HubResult) => void;
}) {
  const [steps, setSteps] = useState<Step[]>([]);
  const [checks, setChecks] = useState<HubCheckItem[]>([]);
  const [problem, setProblem] = useState<string | null>(null);
  const [page, setPage] = useState<string | null>(null);
  const flow = useLifecycle<"signing-in" | "working">({ names: { "signing-in": "hub-sign-in" } });
  const set = (key: Step["key"], state: Step["state"]) => setSteps((all) => all.map((step) => (step.key === key ? { ...step, state } : step)));
  const start = async () => {
    setProblem(null);
    setChecks([]);
    setPage(null);
    setSteps([
      { key: "check", state: "waiting" },
      { key: "connect", state: "waiting" },
      { key: "browser", state: "waiting" },
    ]);
    if (!status?.connected) {
      set("check", "running");
      const diagnosis = await flow.run("working", () => diagnoseHub());
      if (!diagnosis) return;
      if (diagnosis.state !== "completed" || !diagnosis.passed) {
        set("check", "failed");
        setChecks((diagnosis.checks ?? []).filter((check) => !check.passed));
        setProblem(diagnosis.reason ?? null);
        return;
      }
      set("check", "done");
      set("connect", "running");
      const connected = await flow.run("working", () => connectHub());
      if (!connected) return;
      if (connected.state !== "completed" || !connected.connected) {
        set("connect", "failed");
        setProblem(connected.reason ?? "The team could not be reached.");
        return;
      }
      onStatus(connected);
    }
    set("check", "done");
    set("connect", "done");
    set("browser", "running");
    await flow.run("signing-in", async () => {
      const started = await startHubAuth();
      if (started.state !== "completed" || !started.auth_url) {
        set("browser", "failed");
        setProblem(started.reason ?? "Sign-in could not start.");
        return;
      }
      if (!started.opened) setPage(started.auth_url);
      const signedIn = await completeHubAuth("", "");
      setPage(null);
      if (signedIn.state === "completed" && signedIn.authenticated) {
        set("browser", "done");
        onSignedIn(signedIn);
        return;
      }
      // A cancelled or failed browser sign-in keeps the team configured
      // and not signed in, with the reason.
      set("browser", "failed");
      setProblem(signedIn.reason ?? "Sign-in did not complete.");
    });
  };
  useEffect(() => {
    if (open) void start();
  }, [open]); // eslint-disable-line react-hooks/exhaustive-deps
  const labels: Record<Step["key"], string> = { check: "Check setup", connect: "Connect", browser: "Sign in with your browser" };
  const words: Record<Step["state"], string> = { waiting: "", running: "In progress", done: "Done", failed: "Failed" };
  const failed = steps.some((step) => step.state === "failed");
  return (
    <Modal
      open={open}
      title="Sign in"
      size="normal"
      onClose={onClose}
      footer={
        <div className="dialog-footer">
          {failed ? (
            <>
              <button type="button" onClick={onEdit}>
                Edit
              </button>
              <button type="button" className="primary" onClick={() => void start()}>
                Try again
              </button>
            </>
          ) : flow.running === "signing-in" ? (
            <button type="button" onClick={flow.cancel}>
              Stop
            </button>
          ) : null}
        </div>
      }
    >
      <ol className="sign-in-steps" aria-label="Sign-in steps">
        {steps.map((step) => (
          <li key={step.key} data-state={step.state}>
            <span>{labels[step.key]}</span>
            {words[step.state] ? <span className="step-state">{words[step.state]}</span> : null}
          </li>
        ))}
      </ol>
      {checks.length > 0 ? (
        <ul className="plain-list" aria-label="Setup problems">
          {checks.map((check) => (
            <li key={check.name}>{check.message}</li>
          ))}
        </ul>
      ) : null}
      {problem ? <p role="alert">{problem}</p> : null}
      {page ? (
        <p>
          <a href={page} target="_blank" rel="noreferrer">
            Open sign-in page
          </a>
        </p>
      ) : null}
    </Modal>
  );
}
