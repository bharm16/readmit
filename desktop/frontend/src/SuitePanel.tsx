import "./suite.css";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  assessSuiteCoverage,
  approveSuitePromotion,
  cancel,
  expectationImpact,
  openBaseline,
  openSuite,
  prepareSuite,
  previewSuite,
  reviewSuitePromotion,
  saveSuite,
  saveSuiteCoverage,
  saveSuiteReleases,
  validateSuite,
  postHubReleaseReview,
  type BaselineResult,
  type EditorDraft,
  type HubReviewsResult,
  type SuiteDocument,
  type SuiteDocumentResult,
  type SuiteExclusionDeclaration,
  type SuiteImpactResult,
  type SuitePreparedResult,
  type SuitePreviewResult,
  type SuitePromotionResult,
  type SuiteCoverageResult,
  type SuiteReleasesResult,
} from "./bindings";
import { useRetainer, RetentionStatus } from "./drafting";

/** One entry of the workspace listing, as the panel's pickers read it. */
interface Entry {
  name: string;
  kind: string;
}

/** The suite-management panel: suites, parameters, expectation approvals,
 * coverage and environment promotion, over the one canonical contract and the
 * same engine operations the command line runs. Nothing here sends; prepared
 * execution continues in the execution center. */
export function SuitePanel({
  workspace,
  busy,
  entries,
  drafts,
  onExecute,
  onSaved,
}: {
  workspace: string;
  busy: boolean;
  entries: Entry[];
  drafts: EditorDraft[] | null;
  /** Hands one suite entry to the execution center, which owns preflight and
   * the explicit send decision; nothing is executed from this panel. */
  onExecute?: (entry: string) => void;
  /** Called once a write added an entry to the workspace, so every panel that
   * offers entries — this one's pickers and the execution center's — offers
   * it too. */
  onSaved?: () => void;
}) {
  const [tab, setTab] = useState<"suite" | "releases" | "prepare" | "coverage" | "promotion">("suite");
  const [working, setWorking] = useState(false);
  const disabled = busy || working;

  const suiteEntries = useMemo(() => entries.filter((entry) => entry.kind === "suite").map((entry) => entry.name), [entries]);
  // The separate pin set the durable-run panels and promotion reviews read.
  const releaseEntries = useMemo(
    () => entries.filter((entry) => entry.kind === "suite" || entry.kind === "suite-releases").map((entry) => entry.name),
    [entries],
  );
  const specEntries = useMemo(() => entries.filter((entry) => entry.kind === "spec").map((entry) => entry.name), [entries]);
  const caseEntries = useMemo(() => entries.filter((entry) => entry.kind === "case").map((entry) => entry.name), [entries]);
  const targetEntries = useMemo(() => entries.filter((entry) => entry.kind === "target").map((entry) => entry.name), [entries]);

  // ------------------------------------------------------------------ editor
  const [document, setDocument] = useState<SuiteDocument | null>(null);
  const [sourceEntry, setSourceEntry] = useState("");
  const [canonical, setCanonical] = useState("");
  const [opened, setOpened] = useState<SuiteDocumentResult | null>(null);
  const [imported, setImported] = useState<SuiteDocumentResult | null>(null);
  const [saved, setSaved] = useState<SuiteDocumentResult | null>(null);
  const [output, setOutput] = useState("");
  const [environment, setEnvironment] = useState("");
  const [previewReleases, setPreviewReleases] = useState("");
  const [preview, setPreview] = useState<SuitePreviewResult | null>(null);
  const [expectedText, setExpectedText] = useState<Record<string, string>>({});
  const [expectedError, setExpectedError] = useState<string | null>(null);

  const retainer = useRetainer();

  // Retain the suite being edited as unstored work, the way every editor of
  // this window does: the facade holds it in the local draft store and an
  // interruption returns it. The content is the suite editor's own contract,
  // interpreted by the suite reader at preview and save.
  const retain = useCallback(
    (doc: SuiteDocument | null, entry: string) => {
      retainer.save({
        id: retainer.currentId(),
        kind: "suite-editor",
        workspace,
        case: "",
        identity: "",
        content_schema: "readmit-suite-draft/v1",
        content: { schema: "readmit-suite-draft/v1", entry, document: doc ? JSON.stringify(doc) : "" },
      });
    },
    [retainer, workspace],
  );

  const changeDocument = useCallback(
    (next: SuiteDocument | null, entry = sourceEntry) => {
      setDocument(next);
      setOpened(null);
      setImported(null);
      setSaved(null);
      setPreview(null);
      retain(next, entry);
    },
    [retain, sourceEntry],
  );

  // A retained suite draft comes back only when it names this workspace.
  // Restoring is the work as it was, not a claim it was ever valid.
  const adopted = useRef(false);
  useEffect(() => {
    if (adopted.current || !drafts) {
      return;
    }
    adopted.current = true;
    const held = drafts.find((draft) => draft.kind === "suite-editor" && draft.workspace === workspace);
    if (!held) {
      return;
    }
    const content = held.content as { schema?: string; entry?: string; document?: string };
    if (content?.schema !== "readmit-suite-draft/v1") {
      return;
    }
    retainer.keepId(held.id);
    setSourceEntry(content.entry ?? "");
    if (content.document) {
      try {
        setDocument(JSON.parse(content.document) as SuiteDocument);
      } catch {
        setCanonical(content.document);
      }
    }
  }, [drafts, retainer, workspace]);

  const perform = useCallback(async (work: () => Promise<void>) => {
    setWorking(true);
    try {
      await work();
    } finally {
      setWorking(false);
    }
  }, []);

  const openEntry = useCallback(
    async (entry: string) => {
      await perform(async () => {
        const result = await openSuite(workspace, entry);
        setOpened(result);
        setImported(null);
        if (result.suite) {
          setDocument(result.suite);
          setSourceEntry(entry);
          setCanonical(result.document ?? "");
          setExpectedText({});
          setExpectedError(null);
          setPreview(null);
          setSaved(null);
          // The editor now holds this document, so what recovery offers back
          // is this document, not whatever unstored work it held before.
          retain(result.suite, entry);
        }
      });
    },
    [perform, retain, workspace],
  );

  // The pasted text is read by the suite reader alone; a refusal leaves the
  // editor holding what it held.
  const importCanonical = useCallback(async () => {
    await perform(async () => {
      setImported(null);
      const result = await validateSuite(canonical);
      setImported(result);
      if (result.suite) {
        setDocument(result.suite);
        setCanonical(result.document ?? "");
        setOpened(null);
        setSaved(null);
        setPreview(null);
        retain(result.suite, "");
      }
    });
  }, [canonical, perform, retain]);

  // A row's expected values are typed JSON the Go reader validates; the panel
  // only keeps the text parseable so the document it hands over is honest.
  const setRowExpected = useCallback((tableId: string, rowId: string, text: string) => {
    setExpectedText((current) => ({ ...current, [`${tableId}/${rowId}`]: text }));
    setExpectedError(null);
  }, []);

  const parsedExpected = useCallback((): SuiteDocument | null => {
    if (!document) {
      return null;
    }
    const composed: SuiteDocument = {
      ...document,
      tables: document.tables.map((table) => ({
        ...table,
        rows: table.rows.map((row) => {
          const key = `${table.id}/${row.id}`;
          const text = (expectedText[key] ?? "").trim();
          if (!text) {
            return { id: row.id, case: row.case };
          }
          try {
            const expected = JSON.parse(text) as SuiteDocument["tables"][number]["rows"][number]["expected"];
            return expected === undefined ? { id: row.id, case: row.case } : { ...row, expected };
          } catch {
            setExpectedError(`Row ${row.id} of table ${table.id} does not hold typed expected values as JSON.`);
            return row;
          }
        }),
      })),
    };
    return composed;
  }, [document, expectedText]);

  const runPreview = useCallback(async () => {
    const composed = parsedExpected();
    if (!composed || expectedError) {
      return;
    }
    await perform(async () => {
      setPreview(null);
      setPreview(
        await previewSuite({
          workspace,
          document: JSON.stringify(composed),
          entry: "",
          environment,
          releases: previewReleases,
        }),
      );
    });
  }, [environment, expectedError, parsedExpected, perform, previewReleases, workspace]);

  const saveRevision = useCallback(async () => {
    const composed = parsedExpected();
    if (!composed || expectedError) {
      return;
    }
    await perform(async () => {
      const result = await saveSuite({ workspace, document: JSON.stringify(composed), output });
      setSaved(result);
      if (result.state === "completed") {
        onSaved?.();
        setDocument(result.suite ?? composed);
        // The work is stored as a real workspace entry now, so the unstored
        // draft is dropped; a later edit retains a fresh one.
        const stored = retainer.currentId();
        if (stored) {
          retainer.drop(stored);
        }
      }
    });
  }, [expectedError, onSaved, output, parsedExpected, perform, retainer, workspace]);

  // --------------------------------------------------------------- releases
  const [sidecarRows, setSidecarRows] = useState<{ test: string; release: string; identity: string }[]>([{ test: "", release: "", identity: "" }]);
  const [sidecarOutput, setSidecarOutput] = useState("");
  const [sidecar, setSidecar] = useState<SuiteReleasesResult | null>(null);
  // What each reference's release entry holds, read by the release reader
  // `readmit expectation show` applies, so the full identity a reference pins
  // is taken from the retained release rather than typed.
  const [releaseReads, setReleaseReads] = useState<Record<number, BaselineResult | undefined>>({});
  const [impactSuite, setImpactSuite] = useState("");
  const [impactSidecar, setImpactSidecar] = useState("");
  const [impactFrom, setImpactFrom] = useState("");
  const [impactTo, setImpactTo] = useState("");
  const [impactShow, setImpactShow] = useState(false);
  const [impact, setImpact] = useState<SuiteImpactResult | null>(null);

  const writeSidecar = useCallback(async () => {
    await perform(async () => {
      const result = await saveSuiteReleases({
        workspace,
        document: JSON.stringify({
          schema: "readmit-suite-releases/v1",
          tests: sidecarRows.filter((row) => row.test && row.release && row.identity),
        }),
        output: sidecarOutput,
      });
      setSidecar(result);
      if (result.state === "completed") onSaved?.();
    });
  }, [onSaved, perform, sidecarOutput, sidecarRows, workspace]);

  const readReleaseIdentity = useCallback(
    async (index: number) => {
      const entry = sidecarRows[index]?.release ?? "";
      const earlier = releaseReads[index]?.comparison?.identity;
      await perform(async () => {
        const result = await openBaseline({
          workspace,
          release: true,
          release_id: "",
          profiles: [],
          spec: "",
          previous: entry,
          show_values: false,
          review: "",
          approver: "",
          rationale: "",
          output: "",
        });
        setReleaseReads((reads) => ({ ...reads, [index]: result }));
        // A refused read leaves no identity an earlier read of this entry
        // filled in; one the person typed stays.
        const identity = result.state === "completed" ? result.comparison?.identity : undefined;
        setSidecarRows((rows) =>
          rows.map((held, at) =>
            at !== index ? held : identity ? { ...held, identity } : earlier !== undefined && held.identity === earlier ? { ...held, identity: "" } : held,
          ),
        );
      });
    },
    [perform, releaseReads, sidecarRows, workspace],
  );

  const runImpact = useCallback(async () => {
    await perform(async () => {
      setImpact(null);
      setImpact(
        await expectationImpact({
          workspace,
          suite: impactSuite,
          releases: impactSidecar,
          from: impactFrom,
          to: impactTo,
          show_values: impactShow,
        }),
      );
    });
  }, [impactFrom, impactShow, impactSidecar, impactSuite, impactTo, perform, workspace]);

  // The team review of the successor release: the hub's review-request and
  // approval commands name the digest of the exact reviewed bytes, derived
  // from the To entry — never typed. Identity is the signed-in hub session's;
  // the panel's local approver label does not substitute for it, and the hub
  // re-verifies the release and the request chain it records.
  const [hubProject, setHubProject] = useState("");
  const [teamRecipient, setTeamRecipient] = useState("");
  const [teamRationale, setTeamRationale] = useState("");
  const [teamCommandId, setTeamCommandId] = useState("");
  const [teamReview, setTeamReview] = useState<HubReviewsResult | null>(null);

  const runReleaseReview = useCallback(
    async (kind: "review-request" | "approval") => {
      await perform(async () => {
        setTeamReview(null);
        setTeamReview(
          await postHubReleaseReview({
            project: hubProject.trim(),
            workspace,
            entry: impactTo.trim(),
            kind,
            id: teamCommandId.trim(),
            recipient: kind === "review-request" ? teamRecipient.trim() : "",
            text: teamRationale,
          }),
        );
      });
    },
    [hubProject, impactTo, perform, teamCommandId, teamRationale, teamRecipient, workspace],
  );

  // ---------------------------------------------------------------- prepare
  const [prepareEntry, setPrepareEntry] = useState("");
  const [prepareEnvironment, setPrepareEnvironment] = useState("");
  const [prepareReleases, setPrepareReleases] = useState("");
  const [prepareOutput, setPrepareOutput] = useState("");
  const [prepared, setPrepared] = useState<SuitePreparedResult | null>(null);

  const runPrepare = useCallback(async () => {
    await perform(async () => {
      setPrepared(null);
      const result = await prepareSuite({
        workspace,
        entry: prepareEntry,
        environment: prepareEnvironment,
        releases: prepareReleases,
        output: prepareOutput,
      });
      setPrepared(result);
      if (result.state === "completed") onSaved?.();
    });
  }, [onSaved, perform, prepareEntry, prepareEnvironment, prepareOutput, prepareReleases, workspace]);

  // ---------------------------------------------------------------- coverage
  const [coveragePrepared, setCoveragePrepared] = useState("");
  const [requirementRows, setRequirementRows] = useState<{ id: string; jobs: string }[]>([{ id: "", jobs: "" }]);
  const [exclusionRows, setExclusionRows] = useState<SuiteExclusionDeclaration[]>([]);
  const [coverageOutput, setCoverageOutput] = useState("");
  const [authored, setAuthored] = useState<SuiteCoverageResult | null>(null);
  const [assessPrepared, setAssessPrepared] = useState("");
  const [assessRequirements, setAssessRequirements] = useState("");
  const [assessPrevious, setAssessPrevious] = useState("");
  const [assessment, setAssessment] = useState<SuiteCoverageResult | null>(null);

  const writeCoverage = useCallback(async () => {
    await perform(async () => {
      setAuthored(null);
      const result = await saveSuiteCoverage({
        workspace,
        prepared: coveragePrepared,
        requirements: requirementRows
          .filter((row) => row.id)
          .map((row) => ({ id: row.id, jobs: row.jobs.split(/[\s,]+/).filter(Boolean) })),
        exclusions: exclusionRows.filter((row) => row.job && row.state && row.reason && row.expires),
        output: coverageOutput,
      });
      setAuthored(result);
      if (result.state === "completed") onSaved?.();
    });
  }, [coverageOutput, coveragePrepared, exclusionRows, onSaved, perform, requirementRows, workspace]);

  const runAssessment = useCallback(async () => {
    await perform(async () => {
      setAssessment(null);
      setAssessment(
        await assessSuiteCoverage({
          workspace,
          prepared: assessPrepared,
          requirements: assessRequirements,
          previous: assessPrevious.split("\n").map((name) => name.trim()).filter(Boolean),
          at: "",
        }),
      );
    });
  }, [assessPrepared, assessPrevious, assessRequirements, perform, workspace]);

  // --------------------------------------------------------------- promotion
  const [promotionEntry, setPromotionEntry] = useState("");
  const [promotionEnvironment, setPromotionEnvironment] = useState("");
  const [promotionReleases, setPromotionReleases] = useState("");
  const [revision, setRevision] = useState("");
  const [review, setReview] = useState<SuitePromotionResult | null>(null);
  const [approver, setApprover] = useState("");
  const [rationale, setRationale] = useState("");
  const [promotionOutput, setPromotionOutput] = useState("");
  const [approval, setApproval] = useState<SuitePromotionResult | null>(null);

  const runReview = useCallback(async () => {
    await perform(async () => {
      setReview(null);
      setApproval(null);
      setReview(
        await reviewSuitePromotion({
          workspace,
          entry: promotionEntry,
          environment: promotionEnvironment,
          releases: promotionReleases,
          revision,
        }),
      );
    });
  }, [perform, promotionEntry, promotionEnvironment, promotionReleases, revision, workspace]);

  const runApproval = useCallback(async () => {
    await perform(async () => {
      setApproval(null);
      const result = await approveSuitePromotion({
        workspace,
        entry: promotionEntry,
        environment: promotionEnvironment,
        releases: promotionReleases,
        revision,
        reviewed: review?.review?.identity ?? "",
        approver,
        rationale,
        output: promotionOutput,
      });
      setApproval(result);
      if (result.state === "completed") onSaved?.();
    });
  }, [approver, onSaved, perform, promotionEntry, promotionEnvironment, promotionOutput, promotionReleases, rationale, review, revision, workspace]);

  return (
    <section className="suite-panel" aria-labelledby="suite-title">
      <h2 id="suite-title">Suites and releases</h2>
      <p>
        Author suites, release expectations, declare coverage and approve environment promotion over the one canonical
        contract. Nothing here sends: preparation compiles configuration only, and execution is the execution center&rsquo;s
        explicit step.
      </p>
      <nav className="suite-tabs" aria-label="Suite views">
        {(["suite", "releases", "prepare", "coverage", "promotion"] as const).map((name) => (
          <button
            key={name}
            type="button"
            className={`suite-tab ${tab === name ? "active" : ""}`}
            onClick={() => setTab(name)}
          >
            {name === "suite" ? "Suite" : name === "releases" ? "Releases and impact" : name === "prepare" ? "Prepare" : name === "coverage" ? "Coverage" : "Promotion"}
          </button>
        ))}
      </nav>
      <RetentionStatus retention={retainer.retention} />
      {tab === "suite" ? (
        <fieldset disabled={disabled}>
          <legend>Suite editor</legend>
          <div className="suite-row">
            <label>
              Saved suite entry{" "}
              <select value={sourceEntry} onChange={(event) => setSourceEntry(event.target.value)}>
                <option value="">(none)</option>
                {suiteEntries.map((name) => (
                  <option key={name} value={name}>
                    {name}
                  </option>
                ))}
              </select>
            </label>
            <button type="button" disabled={!sourceEntry} onClick={() => void openEntry(sourceEntry)}>
              Open suite
            </button>
            <button type="button" onClick={() => changeDocument(emptySuite())}>New suite</button>
          </div>
          {document ? (
            <SuiteEditor
              document={document}
              expectedText={expectedText}
              specEntries={specEntries}
              caseEntries={caseEntries}
              targetEntries={targetEntries}
              onChange={(next) => changeDocument(next)}
              onExpected={setRowExpected}
            />
          ) : (
            <p className="hint">Open a saved suite or start a new one. A suite this window did not write opens with every clause it declares.</p>
          )}
          {opened && opened.state !== "completed" ? <p role="alert">{opened.reason}</p> : null}
          <div className="suite-row">
            <label>
              Preview environment{" "}
              <select value={environment} onChange={(event) => setEnvironment(event.target.value)}>
                <option value="">(select)</option>
                {(document?.environments ?? []).map((env) => (
                  <option key={env.id} value={env.id}>
                    {env.id}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Release references (optional){" "}
              <select value={previewReleases} onChange={(event) => setPreviewReleases(event.target.value)}>
                <option value="">(none)</option>
                {releaseEntries.map((name) => (
                  <option key={name} value={name}>
                    {name}
                  </option>
                ))}
              </select>
            </label>
            <button type="button" disabled={!document || !environment} onClick={() => void runPreview()}>
              Preview the exact expansion
            </button>
          </div>
          {expectedError ? <p role="alert">{expectedError}</p> : null}
          <ExpansionView result={preview} />
          <div className="suite-row">
            <label>
              New revision entry <input value={output} onChange={(event) => setOutput(event.target.value)} />
            </label>
            <button
              type="button"
              disabled={!document || !output.trim() || Boolean(expectedError)}
              onClick={() => void saveRevision()}
            >
              Save new version
            </button>
          </div>
          <p role="status">
            {saved?.reason ??
              (saved?.state === "completed" ? `Saved ${saved.output}. Suite identity: ${saved.sha256}` : "")}
          </p>
          {saved?.document ? (
            <details>
              <summary>Canonical JSON (expert export)</summary>
              <pre>{saved.document}</pre>
            </details>
          ) : null}
          <details>
            <summary>Import canonical JSON (expert)</summary>
            <textarea
              aria-label="Canonical suite JSON"
              value={canonical}
              onChange={(event) => {
                setCanonical(event.target.value);
                setImported(null);
              }}
            />
            <button type="button" disabled={!canonical.trim()} onClick={() => void importCanonical()}>
              Validate and load
            </button>
            {imported && imported.state !== "completed" ? <p role="alert">{imported.reason}</p> : null}
            {imported?.state === "completed" ? <p role="status">Validated and loaded into the editor; nothing was saved.</p> : null}
          </details>
        </fieldset>
      ) : null}
      {tab === "releases" ? (
        <fieldset disabled={disabled}>
          <legend>Release references and impact</legend>
          <table className="suite-table">
            <caption>readmit-suite-releases/v1: one exact released identity per suite test</caption>
            <thead>
              <tr>
                <th>Test</th>
                <th>Release entry</th>
                <th>Release identity</th>
                <th>Retained release</th>
              </tr>
            </thead>
            <tbody>
              {sidecarRows.map((row, index) => (
                <tr key={index}>
                  <td>
                    <input
                      aria-label={`Test ${index + 1}`}
                      value={row.test}
                      onChange={(event) =>
                        setSidecarRows((rows) => rows.map((held, at) => (at === index ? { ...held, test: event.target.value } : held)))
                      }
                    />
                  </td>
                  <td>
                    <input
                      aria-label={`Release entry ${index + 1}`}
                      value={row.release}
                      onChange={(event) => {
                        // An identity read from the entry this row named
                        // before no longer belongs to it.
                        const read = releaseReads[index]?.comparison?.identity;
                        setSidecarRows((rows) =>
                          rows.map((held, at) =>
                            at === index
                              ? { ...held, release: event.target.value, identity: read !== undefined && held.identity === read ? "" : held.identity }
                              : held,
                          ),
                        );
                        setReleaseReads((reads) => ({ ...reads, [index]: undefined }));
                      }}
                    />
                  </td>
                  <td>
                    <input
                      aria-label={`Release identity ${index + 1}`}
                      value={row.identity}
                      onChange={(event) =>
                        setSidecarRows((rows) => rows.map((held, at) => (at === index ? { ...held, identity: event.target.value } : held)))
                      }
                    />
                  </td>
                  <td>
                    <button
                      type="button"
                      aria-label={`Read identity of release entry ${index + 1}`}
                      disabled={!row.release.trim()}
                      onClick={() => void readReleaseIdentity(index)}
                    >
                      Read identity
                    </button>
                    <ReleaseRead result={releaseReads[index]} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          <button type="button" onClick={() => setSidecarRows((rows) => [...rows, { test: "", release: "", identity: "" }])}>
            Add test pin
          </button>
          <div className="suite-row">
            <label>
              New sidecar entry <input value={sidecarOutput} onChange={(event) => setSidecarOutput(event.target.value)} />
            </label>
            <button type="button" disabled={!sidecarOutput.trim()} onClick={() => void writeSidecar()}>
              Save release references
            </button>
          </div>
          <p role="status">{sidecar?.reason ?? (sidecar?.state === "completed" ? `Saved ${sidecar.output}.` : "")}</p>
          <h3>Expectation impact</h3>
          <p>Compare one released template with its direct successor against a saved suite. No pin is moved and no execution is assessed.</p>
          <div className="suite-row">
            <label>
              Suite{" "}
              <select value={impactSuite} onChange={(event) => setImpactSuite(event.target.value)}>
                <option value="">(select)</option>
                {suiteEntries.map((name) => (
                  <option key={name} value={name}>
                    {name}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Release references{" "}
              <select value={impactSidecar} onChange={(event) => setImpactSidecar(event.target.value)}>
                <option value="">(select)</option>
                {releaseEntries.map((name) => (
                  <option key={name} value={name}>
                    {name}
                  </option>
                ))}
              </select>
            </label>
            <label>
              From <input value={impactFrom} onChange={(event) => setImpactFrom(event.target.value)} />
            </label>
            <label>
              To <input value={impactTo} onChange={(event) => setImpactTo(event.target.value)} />
            </label>
            <label>
              <input type="checkbox" checked={impactShow} onChange={(event) => setImpactShow(event.target.checked)} /> Reveal exact values
            </label>
            <button
              type="button"
              disabled={!impactSuite || !impactSidecar || !impactFrom || !impactTo}
              onClick={() => void runImpact()}
            >
              Report impact
            </button>
          </div>
          {impact && impact.state !== "completed" ? <p role="alert">{impact.reason}</p> : null}
          {impact?.impact ? (
            <>
              <table className="suite-table">
                <caption>Every suite test beside its pin</caption>
                <thead>
                  <tr>
                    <th>Test</th>
                    <th>Rows</th>
                    <th>Pinned identity</th>
                    <th>Impact</th>
                  </tr>
                </thead>
                <tbody>
                  {impact.impact.tests.map((row) => (
                    <tr key={row.test}>
                      <th>{row.test}</th>
                      <td>{row.rows}</td>
                      <td>{row.pinned}</td>
                      <td>{row.state}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
              <table className="suite-table">
                <caption>Exact specification and profile changes</caption>
                <thead>
                  <tr>
                    <th>Part</th>
                    <th>Change</th>
                    <th>Before</th>
                    <th>After</th>
                  </tr>
                </thead>
                <tbody>
                  {impact.impact.comparison.changes.map((change, index) => (
                    <tr key={index}>
                      <th>{change.part}</th>
                      <td>{change.kind}</td>
                      <td>
                        <pre>{change.before ?? (impactShow ? "Absent" : "Hidden")}</pre>
                      </td>
                      <td>
                        <pre>{change.after ?? (impactShow ? "Absent" : "Hidden")}</pre>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </>
          ) : null}
          <h3>Team review of the successor release</h3>
          <p>
            Request review of, or approve, the exact released expectation named by the To entry. The
            commands name the digest of those reviewed bytes — never a typed value. Identity is the
            signed-in hub session&rsquo;s: the promotion tab&rsquo;s local approver label records a local
            decision and cannot approve for the team. Approval chains to the outstanding request naming
            these exact bytes, and a changed grant or stale head requires a renewed action.
          </p>
          <div className="suite-row">
            <label>
              Hub project <input value={hubProject} onChange={(event) => setHubProject(event.target.value)} />
            </label>
            <label>
              Request recipient <input value={teamRecipient} onChange={(event) => setTeamRecipient(event.target.value)} />
            </label>
            <label>
              Command id <input value={teamCommandId} onChange={(event) => setTeamCommandId(event.target.value)} />
            </label>
            <label>
              Rationale <input value={teamRationale} onChange={(event) => setTeamRationale(event.target.value)} />
            </label>
          </div>
          <div className="suite-row">
            <button
              type="button"
              disabled={!hubProject.trim() || !impactTo.trim() || !teamCommandId.trim() || !teamRationale.trim() || !teamRecipient.trim()}
              onClick={() => void runReleaseReview("review-request")}
            >
              Request team review
            </button>
            <button
              type="button"
              disabled={!hubProject.trim() || !impactTo.trim() || !teamCommandId.trim() || !teamRationale.trim()}
              onClick={() => void runReleaseReview("approval")}
            >
              Approve this release
            </button>
          </div>
          {teamReview && teamReview.state !== "completed" ? <p role="alert">{teamReview.reason}</p> : null}
          {teamReview?.events?.length ? (
            <p role="status">
              Recorded: {teamReview.events[0]?.kind} by {teamReview.events[0]?.actor}@{teamReview.events[0]?.issuer}
              {teamReview.events[0]?.release ? ` · release ${teamReview.events[0]?.release.slice(0, 12)}…` : ""}
              {teamReview.replay ? " (replayed, same command id)" : ""}
            </p>
          ) : null}
        </fieldset>
      ) : null}
      {tab === "prepare" ? (
        <fieldset disabled={disabled}>
          <legend>Prepare a suite</legend>
          <p>Compile a saved suite against one declared environment into a new private directory. Nothing is sent and existing output is never resumed.</p>
          <div className="suite-row">
            <label>
              Suite entry{" "}
              <select value={prepareEntry} onChange={(event) => setPrepareEntry(event.target.value)}>
                <option value="">(select)</option>
                {suiteEntries.map((name) => (
                  <option key={name} value={name}>
                    {name}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Environment <input value={prepareEnvironment} onChange={(event) => setPrepareEnvironment(event.target.value)} />
            </label>
            <label>
              Release references (optional){" "}
              <select value={prepareReleases} onChange={(event) => setPrepareReleases(event.target.value)}>
                <option value="">(none)</option>
                {releaseEntries.map((name) => (
                  <option key={name} value={name}>
                    {name}
                  </option>
                ))}
              </select>
            </label>
            <label>
              New directory entry <input value={prepareOutput} onChange={(event) => setPrepareOutput(event.target.value)} />
            </label>
            <button
              type="button"
              disabled={!prepareEntry || !prepareEnvironment || !prepareOutput.trim()}
              onClick={() => void runPrepare()}
            >
              Prepare configuration
            </button>
          </div>
          <p role="status">{prepared?.reason ?? (prepared?.state === "completed" ? `Prepared ${prepared.directory}; ${prepared.queue?.jobs.length ?? 0} jobs. Nothing was sent.` : "")}</p>
          {prepared?.queue ? (
            <table className="suite-table">
              <caption>The compiled queue plan</caption>
              <thead>
                <tr>
                  <th>Job</th>
                  <th>Specification</th>
                  <th>Isolation</th>
                  <th>Depends on</th>
                </tr>
              </thead>
              <tbody>
                {prepared.queue.jobs.map((job) => (
                  <tr key={job.id}>
                    <th>{job.id}</th>
                    <td>{job.spec}</td>
                    <td>{job.isolation}</td>
                    <td>{job.after?.join(", ") || "—"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          ) : null}
          {prepared?.state === "completed" ? (
            <>
              <p className="hint">
                Execution continues in the execution center, which preflights the suite against this environment and
                asks for the explicit send decision; nothing was executed here.
              </p>
              {onExecute ? (
                <button type="button" onClick={() => onExecute(prepareEntry)}>
                  Continue to the execution center
                </button>
              ) : null}
            </>
          ) : null}
        </fieldset>
      ) : null}
      {tab === "coverage" ? (
        <>
        <fieldset disabled={disabled}>
          <legend>Declared requirement coverage</legend>
          <h3>Author the coverage document</h3>
          <p>The suite digest and every specification pin are computed from the retained bytes of a prepared suite; only the requirements and exclusions are declarations. An exclusion never filters execution and expiry never enables a send.</p>
          <div className="suite-row">
            <label>
              Prepared suite directory{" "}
              <select value={coveragePrepared} onChange={(event) => setCoveragePrepared(event.target.value)}>
                <option value="">(select)</option>
                {suiteEntries.map((name) => (
                  <option key={name} value={name}>
                    {name}
                  </option>
                ))}
              </select>
            </label>
          </div>
          <table className="suite-table">
            <caption>The explicit denominator: every requirement and the jobs that establish it</caption>
            <thead>
              <tr>
                <th>Requirement</th>
                <th>Jobs</th>
              </tr>
            </thead>
            <tbody>
              {requirementRows.map((row, index) => (
                <tr key={index}>
                  <td>
                    <input
                      aria-label={`Requirement ${index + 1}`}
                      value={row.id}
                      onChange={(event) =>
                        setRequirementRows((rows) => rows.map((held, at) => (at === index ? { ...held, id: event.target.value } : held)))
                      }
                    />
                  </td>
                  <td>
                    <input
                      aria-label={`Requirement jobs ${index + 1}`}
                      value={row.jobs as unknown as string}
                      onChange={(event) =>
                        setRequirementRows((rows) => rows.map((held, at) => (at === index ? { ...held, jobs: event.target.value } : held)))
                      }
                    />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          <button
            type="button"
            onClick={() => setRequirementRows((rows) => [...rows, { id: "", jobs: "" }])}
          >
            Add requirement
          </button>
          <table className="suite-table">
            <caption>Exclusions and quarantine: state, reason and UTC expiry to the second</caption>
            <thead>
              <tr>
                <th>Job</th>
                <th>State</th>
                <th>Reason</th>
                <th>Expires</th>
              </tr>
            </thead>
            <tbody>
              {exclusionRows.map((row, index) => (
                <tr key={index}>
                  <td>
                    <input
                      aria-label={`Exclusion job ${index + 1}`}
                      value={row.job}
                      onChange={(event) =>
                        setExclusionRows((rows) => rows.map((held, at) => (at === index ? { ...held, job: event.target.value } : held)))
                      }
                    />
                  </td>
                  <td>
                    <select
                      aria-label={`Exclusion state ${index + 1}`}
                      value={row.state}
                      onChange={(event) =>
                        setExclusionRows((rows) => rows.map((held, at) => (at === index ? { ...held, state: event.target.value } : held)))
                      }
                    >
                      <option value="">(select)</option>
                      {["skipped", "unsupported", "quarantined", "disabled"].map((state) => (
                        <option key={state} value={state}>
                          {state}
                        </option>
                      ))}
                    </select>
                  </td>
                  <td>
                    <input
                      aria-label={`Exclusion reason ${index + 1}`}
                      value={row.reason}
                      onChange={(event) =>
                        setExclusionRows((rows) => rows.map((held, at) => (at === index ? { ...held, reason: event.target.value } : held)))
                      }
                    />
                  </td>
                  <td>
                    <input
                      aria-label={`Exclusion expiry ${index + 1}`}
                      value={row.expires}
                      onChange={(event) =>
                        setExclusionRows((rows) => rows.map((held, at) => (at === index ? { ...held, expires: event.target.value } : held)))
                      }
                    />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          <button
            type="button"
            onClick={() => setExclusionRows((rows) => [...rows, { job: "", state: "", reason: "", expires: "" }])}
          >
            Add exclusion
          </button>
          <div className="suite-row">
            <label>
              New coverage entry <input value={coverageOutput} onChange={(event) => setCoverageOutput(event.target.value)} />
            </label>
            <button type="button" disabled={!coveragePrepared || !coverageOutput.trim()} onClick={() => void writeCoverage()}>
              Author coverage document
            </button>
          </div>
          <p role="status">{authored?.reason ?? (authored?.state === "completed" ? `Saved ${authored.output}.` : "")}</p>
          <h3>Assess a prepared suite</h3>
          <div className="suite-row">
            <label>
              Prepared suite directory{" "}
              <select value={assessPrepared} onChange={(event) => setAssessPrepared(event.target.value)}>
                <option value="">(select)</option>
                {suiteEntries.map((name) => (
                  <option key={name} value={name}>
                    {name}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Coverage document{" "}
              <select value={assessRequirements} onChange={(event) => setAssessRequirements(event.target.value)}>
                <option value="">(select)</option>
                {suiteEntries.map((name) => (
                  <option key={name} value={name}>
                    {name}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Previous directories (one per line){" "}
              <textarea value={assessPrevious} onChange={(event) => setAssessPrevious(event.target.value)} />
            </label>
            <button type="button" disabled={!assessPrepared || !assessRequirements} onClick={() => void runAssessment()}>
              Assess coverage
            </button>
          </div>
        </fieldset>
        {working ? (
          <button
            type="button"
            onClick={() => {
              // The cancel names this panel's assessment, so it can never
              // reach an operation another panel started.
              cancel("suite-coverage-assessment");
              setAssessment({ state: "cancelled", reason: "Coverage assessment cancelled. Retained evidence is unchanged; assess again to recover." });
            }}
          >
            Cancel assessment
          </button>
        ) : null}
        <CoverageView result={assessment} />
        </>
      ) : null}
      {tab === "promotion" ? (
        <fieldset disabled={disabled}>
          <legend>Environment promotion</legend>
          <p>
            Review and approve one exact suite against one declared environment and the operator-declared target
            revision. Changed configuration invalidates the review. Promotion grants no send authority and never
            verifies the target&rsquo;s actual software. The approval recorded here is a local decision under the
            approver label typed below; the team&rsquo;s authenticated approval of the released expectations is a
            separate action in the Releases tab&rsquo;s team review.
          </p>
          <div className="suite-row">
            <label>
              Suite entry{" "}
              <select value={promotionEntry} onChange={(event) => setPromotionEntry(event.target.value)}>
                <option value="">(select)</option>
                {suiteEntries.map((name) => (
                  <option key={name} value={name}>
                    {name}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Environment <input value={promotionEnvironment} onChange={(event) => setPromotionEnvironment(event.target.value)} />
            </label>
            <label>
              Release references{" "}
              <select value={promotionReleases} onChange={(event) => setPromotionReleases(event.target.value)}>
                <option value="">(select)</option>
                {releaseEntries.map((name) => (
                  <option key={name} value={name}>
                    {name}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Target revision (operator-declared) <input value={revision} onChange={(event) => setRevision(event.target.value)} />
            </label>
            <button
              type="button"
              disabled={!promotionEntry || !promotionEnvironment || !promotionReleases || !revision.trim()}
              onClick={() => void runReview()}
            >
              Review promotion
            </button>
          </div>
          {review && review.state !== "completed" ? <p role="alert">{review.reason}</p> : null}
          {review?.review ? (
            <>
              <p>
                Review identity <code>{review.review.identity}</code>: suite <code>{review.review.suite_sha256}</code>,
                releases <code>{review.review.releases_sha256}</code>, environment <code>{review.review.environment}</code>,
                revision assumption <code>{review.review.revision_assumption}</code>.
              </p>
              <table className="suite-table">
                <caption>Every job commitment this approval would bind</caption>
                <thead>
                  <tr>
                    <th>Job</th>
                    <th>Exact input pin</th>
                  </tr>
                </thead>
                <tbody>
                  {review.review.jobs.map((job) => (
                    <tr key={job.job}>
                      <th>{job.job}</th>
                      <td>{job.sha256}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
              <div className="suite-row">
                <label>
                  Local approver <input value={approver} onChange={(event) => setApprover(event.target.value)} />
                </label>
                <label>
                  Approval rationale <input value={rationale} onChange={(event) => setRationale(event.target.value)} />
                </label>
                <label>
                  New approval entry <input value={promotionOutput} onChange={(event) => setPromotionOutput(event.target.value)} />
                </label>
                <button
                  type="button"
                  disabled={!approver.trim() || !rationale.trim() || !promotionOutput.trim()}
                  onClick={() => void runApproval()}
                >
                  Approve this exact promotion
                </button>
              </div>
            </>
          ) : null}
          <p role="status">
            {approval?.reason ?? (approval?.state === "completed" ? `Approved and saved ${approval.output}. Approval identity: ${approval.identity}` : "")}
          </p>
        </fieldset>
      ) : null}
    </section>
  );
}

/** What one release reference's entry holds, as the release reader read it:
 * the release's own test identity, revision and local approver, or the
 * reader's refusal. */
function ReleaseRead({ result }: { result: BaselineResult | undefined }) {
  if (!result) {
    return null;
  }
  if (result.state !== "completed") {
    return <p role="alert">{result.reason}</p>;
  }
  return (
    <p role="status">
      Test {result.release_id}, revision {result.comparison?.revision}, local approver {result.previous_approver}.
    </p>
  );
}

function emptySuite(): SuiteDocument {
  return {
    schema: "readmit-suite/v1",
    id: "",
    owner: "",
    tags: [],
    parallelism: 1,
    environments: [{ id: "", site: "", bindings: [{ parameter: "", target: "", observation: "" }] }],
    tables: [{ id: "", rows: [{ id: "", case: "" }] }],
    tests: [
      { id: "", spec: "", owner: "", tags: [], parameter: "", table: "", isolation: "shared", sequence: [], after: [] },
    ],
  };
}

/** The structured controls over every member of the canonical suite
 * contract: identity, environments and parameter bindings, data tables and
 * expected-value overrides, tests with templates, tables, isolation,
 * dependencies and exact send order. */
function SuiteEditor({
  document,
  expectedText,
  specEntries,
  caseEntries,
  targetEntries,
  onChange,
  onExpected,
}: {
  document: SuiteDocument;
  expectedText: Record<string, string>;
  specEntries: string[];
  caseEntries: string[];
  targetEntries: string[];
  onChange: (next: SuiteDocument) => void;
  onExpected: (tableId: string, rowId: string, text: string) => void;
}) {
  const list = (values: string[]) => values.join(", ");
  const parsed = (text: string): string[] => text.split(/[\s,]+/).filter(Boolean);
  return (
    <div className="suite-editor">
      <div className="suite-row">
        <label>
          Suite id <input value={document.id} onChange={(event) => onChange({ ...document, id: event.target.value })} />
        </label>
        <label>
          Owner <input value={document.owner} onChange={(event) => onChange({ ...document, owner: event.target.value })} />
        </label>
        <label>
          Tags <input value={list(document.tags)} onChange={(event) => onChange({ ...document, tags: parsed(event.target.value) })} />
        </label>
        <label>
          Parallelism{" "}
          <input
            type="number"
            min={1}
            max={16}
            value={document.parallelism}
            onChange={(event) => onChange({ ...document, parallelism: Number(event.target.value) })}
          />
        </label>
      </div>
      <h3>Environments</h3>
      {document.environments.map((env, envIndex) => (
        <div className="suite-block" key={envIndex}>
          <div className="suite-row">
            <label>
              Environment id{" "}
              <input
                value={env.id}
                onChange={(event) =>
                  onChange({
                    ...document,
                    environments: document.environments.map((held, at) => (at === envIndex ? { ...held, id: event.target.value } : held)),
                  })
                }
              />
            </label>
            <label>
              Site{" "}
              <input
                value={env.site}
                onChange={(event) =>
                  onChange({
                    ...document,
                    environments: document.environments.map((held, at) => (at === envIndex ? { ...held, site: event.target.value } : held)),
                  })
                }
              />
            </label>
          </div>
          {env.bindings.map((binding, bindingIndex) => (
            <div className="suite-row" key={bindingIndex}>
              <label>
                Parameter{" "}
                <input
                  value={binding.parameter}
                  onChange={(event) =>
                    onChange({
                      ...document,
                      environments: document.environments.map((held, at) =>
                        at === envIndex
                          ? { ...held, bindings: held.bindings.map((row, i) => (i === bindingIndex ? { ...row, parameter: event.target.value } : row)) }
                          : held,
                      ),
                    })
                  }
                />
              </label>
              <label>
                Target{" "}
                <select
                  value={binding.target}
                  onChange={(event) =>
                    onChange({
                      ...document,
                      environments: document.environments.map((held, at) =>
                        at === envIndex
                          ? { ...held, bindings: held.bindings.map((row, i) => (i === bindingIndex ? { ...row, target: event.target.value } : row)) }
                          : held,
                      ),
                    })
                  }
                >
                  <option value="">(select)</option>
                  {targetEntries.map((name) => (
                    <option key={name} value={name}>
                      {name}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                Observation (ledger tests){" "}
                <input
                  value={binding.observation ?? ""}
                  onChange={(event) =>
                    onChange({
                      ...document,
                      environments: document.environments.map((held, at) =>
                        at === envIndex
                          ? { ...held, bindings: held.bindings.map((row, i) => (i === bindingIndex ? { ...row, observation: event.target.value } : row)) }
                          : held,
                      ),
                    })
                  }
                />
              </label>
            </div>
          ))}
        </div>
      ))}
      <button type="button" onClick={() => onChange({ ...document, environments: [...document.environments, { id: "", site: "", bindings: [{ parameter: "", target: "" }] }] })}>
        Add environment
      </button>
      <h3>Data tables</h3>
      {document.tables.map((table, tableIndex) => (
        <div className="suite-block" key={tableIndex}>
          <div className="suite-row">
            <label>
              Table id{" "}
              <input
                value={table.id}
                onChange={(event) =>
                  onChange({ ...document, tables: document.tables.map((held, at) => (at === tableIndex ? { ...held, id: event.target.value } : held)) })
                }
              />
            </label>
          </div>
          {table.rows.map((row, rowIndex) => (
            <div className="suite-row" key={rowIndex}>
              <label>
                Row id{" "}
                <input
                  value={row.id}
                  onChange={(event) =>
                    onChange({
                      ...document,
                      tables: document.tables.map((held, at) =>
                        at === tableIndex
                          ? { ...held, rows: held.rows.map((held2, i) => (i === rowIndex ? { ...held2, id: event.target.value } : held2)) }
                          : held,
                      ),
                    })
                  }
                />
              </label>
              <label>
                Case{" "}
                <select
                  value={row.case}
                  onChange={(event) =>
                    onChange({
                      ...document,
                      tables: document.tables.map((held, at) =>
                        at === tableIndex
                          ? { ...held, rows: held.rows.map((held2, i) => (i === rowIndex ? { ...held2, case: event.target.value } : held2)) }
                          : held,
                      ),
                    })
                  }
                >
                  <option value="">(select)</option>
                  {caseEntries.map((name) => (
                    <option key={name} value={name}>
                      {name}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                Expected overrides (typed JSON){" "}
                <textarea
                  aria-label={`Expected overrides for row ${row.id} of table ${table.id}`}
                  value={expectedText[`${table.id}/${row.id}`] ?? JSON.stringify(row.expected ?? {})}
                  onChange={(event) => onExpected(table.id, row.id, event.target.value)}
                />
              </label>
            </div>
          ))}
          <button
            type="button"
            onClick={() =>
              onChange({
                ...document,
                tables: document.tables.map((held, at) => (at === tableIndex ? { ...held, rows: [...held.rows, { id: "", case: "" }] } : held)),
              })
            }
          >
            Add row
          </button>
        </div>
      ))}
      <button type="button" onClick={() => onChange({ ...document, tables: [...document.tables, { id: "", rows: [{ id: "", case: "" }] }] })}>
        Add table
      </button>
      <h3>Tests</h3>
      {document.tests.map((test, testIndex) => (
        <div className="suite-block" key={testIndex}>
          <div className="suite-row">
            <label>
              Test id{" "}
              <input
                value={test.id}
                onChange={(event) =>
                  onChange({ ...document, tests: document.tests.map((held, at) => (at === testIndex ? { ...held, id: event.target.value } : held)) })
                }
              />
            </label>
            <label>
              Template{" "}
              <select
                value={test.spec}
                onChange={(event) =>
                  onChange({ ...document, tests: document.tests.map((held, at) => (at === testIndex ? { ...held, spec: event.target.value } : held)) })
                }
              >
                <option value="">(select)</option>
                {specEntries.map((name) => (
                  <option key={name} value={name}>
                    {name}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Owner <input value={test.owner} onChange={(event) => onChange({ ...document, tests: document.tests.map((held, at) => (at === testIndex ? { ...held, owner: event.target.value } : held)) })} />
            </label>
            <label>
              Tags <input value={list(test.tags)} onChange={(event) => onChange({ ...document, tests: document.tests.map((held, at) => (at === testIndex ? { ...held, tags: parsed(event.target.value) } : held)) })} />
            </label>
          </div>
          <div className="suite-row">
            <label>
              Parameter <input value={test.parameter} onChange={(event) => onChange({ ...document, tests: document.tests.map((held, at) => (at === testIndex ? { ...held, parameter: event.target.value } : held)) })} />
            </label>
            <label>
              Table{" "}
              <select
                value={test.table}
                onChange={(event) =>
                  onChange({ ...document, tests: document.tests.map((held, at) => (at === testIndex ? { ...held, table: event.target.value } : held)) })
                }
              >
                <option value="">(select)</option>
                {document.tables.map((table) => (
                  <option key={table.id} value={table.id}>
                    {table.id}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Isolation{" "}
              <select
                value={test.isolation}
                onChange={(event) =>
                  onChange({ ...document, tests: document.tests.map((held, at) => (at === testIndex ? { ...held, isolation: event.target.value } : held)) })
                }
              >
                <option value="shared">shared (holds the environment)</option>
                <option value="isolated">isolated (may overlap)</option>
              </select>
            </label>
            <label>
              Dependencies (test ids) <input value={list(test.after ?? [])} onChange={(event) => onChange({ ...document, tests: document.tests.map((held, at) => (at === testIndex ? { ...held, after: parsed(event.target.value) } : held)) })} />
            </label>
            <label>
              Send order (exact) <input value={list(test.sequence)} onChange={(event) => onChange({ ...document, tests: document.tests.map((held, at) => (at === testIndex ? { ...held, sequence: parsed(event.target.value) } : held)) })} />
            </label>
          </div>
        </div>
      ))}
      <button
        type="button"
        onClick={() =>
          onChange({
            ...document,
            tests: [...document.tests, { id: "", spec: "", owner: "", tags: [], parameter: "", table: "", isolation: "shared", sequence: [], after: [] }],
          })
        }
      >
        Add test
      </button>
    </div>
  );
}

/** The exact expansion: every job, its effective inputs and bindings, the
 * release pins in force and the serialization the queue holds. */
function ExpansionView({ result }: { result: SuitePreviewResult | null }) {
  if (!result) {
    return null;
  }
  if (result.state !== "completed" || !result.expansion) {
    return <p role="alert">{result.reason}</p>;
  }
  const expansion = result.expansion;
  return (
    <div className="suite-expansion">
      <p role="status">
        Suite {expansion.suite.id} against environment {expansion.environment.id} ({expansion.environment.site}); engine{" "}
        {expansion.engine}; {expansion.jobs.length} expanded jobs.
      </p>
      <p>{expansion.order}</p>
      <p>{expansion.sharing}</p>
      {expansion.releases.length > 0 ? (
        <p>
          Released expectations in force: {expansion.releases.map((pin) => `${pin.test} → ${pin.identity}`).join("; ")}
        </p>
      ) : (
        <p>No release references selected: this is an ordinary suite making no approval claim.</p>
      )}
      <table className="suite-table">
        <caption>The exact jobs preparation would compile, in declared order</caption>
        <thead>
          <tr>
            <th>Job</th>
            <th>Test</th>
            <th>Row</th>
            <th>Template</th>
            <th>Effective case</th>
            <th>Effective target</th>
            <th>Boundary</th>
            <th>Observation</th>
            <th>Isolation</th>
            <th>Depends on</th>
            <th>Send order</th>
          </tr>
        </thead>
        <tbody>
          {expansion.jobs.map((job) => (
            <tr key={job.id}>
              <th>{job.id}</th>
              <td>{job.test}</td>
              <td>{job.row}</td>
              <td>{job.spec}</td>
              <td>{job.case}</td>
              <td>{job.target}</td>
              <td>{job.boundary}</td>
              <td>{job.observation || "—"}</td>
              <td>{job.isolation}</td>
              <td>{job.after.length > 0 ? job.after.join(", ") : "—"}</td>
              <td>{job.sequence.join(", ")}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

/** The coverage assessment: the explicit denominator, every requirement, and
 * every job including the ones no requirement maps. */
function CoverageView({ result }: { result: SuiteCoverageResult | null }) {
  if (!result) {
    return null;
  }
  if (result.state !== "completed" || !result.report) {
    return <p role="alert">{result.reason}</p>;
  }
  const report = result.report;
  return (
    <div className="suite-coverage">
      <p role="status">
        Declared requirement coverage: {report.passed}/{report.denominator} ({report.percent.toFixed(2)}%) at {report.at}.
      </p>
      <p>{report.scope}</p>
      <table className="suite-table">
        <caption>Every declared requirement</caption>
        <thead>
          <tr>
            <th>Requirement</th>
            <th>Jobs</th>
            <th>State</th>
          </tr>
        </thead>
        <tbody>
          {report.requirements.map((requirement) => (
            <tr key={requirement.id}>
              <th>{requirement.id}</th>
              <td>{requirement.jobs.join(", ") || "— (explicitly uncovered)"}</td>
              <td>{requirement.state}</td>
            </tr>
          ))}
        </tbody>
      </table>
      <table className="suite-table">
        <caption>Every suite job: actual execution, exclusions and retained stability</caption>
        <thead>
          <tr>
            <th>Job</th>
            <th>Execution</th>
            <th>Reason</th>
            <th>Exclusion</th>
            <th>Expiry</th>
            <th>Stability</th>
            <th>Eligible</th>
          </tr>
        </thead>
        <tbody>
          {report.jobs.map((job) => (
            <tr key={job.id}>
              <th>{job.id}</th>
              <td>{job.execution}</td>
              <td>{job.reason}</td>
              <td>
                {job.exclusion}
                {job.exclusion !== "none" ? ` (${job.exclusion_reason}; expires ${job.expires}${job.expired ? ", expired" : ""})` : ""}
              </td>
              <td>{job.expiry}</td>
              <td>
                {job.stability.state} ({job.stability.reason})
              </td>
              <td>{job.eligible ? "yes" : "no"}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
