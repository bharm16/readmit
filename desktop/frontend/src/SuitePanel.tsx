import { HIDDEN_VALUE } from "./display";
import "./suite.css";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  assessSuiteCoverage,
  approveSuitePromotion,
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
  type RunQueueIsolation,
  type SuiteDocument,
  type SuiteDocumentResult,
  type SuiteExclusionDeclaration,
  type SuiteImpactResult,
  type SuitePreparedResult,
  type SuitePreviewResult,
  type SuitePromotionResult,
  type SuiteCoverageResult,
  type SuiteReleasesResult,
  type SuiteRole,
} from "./bindings";
import { useRetainer, RetentionStatus } from "./drafting";
import { useLifecycle } from "./lifecycle";

/** One entry of the workspace listing, as the panel's pickers read it. The
 * role is the listing's claim, from the declared contract or the directory's
 * marker, of which suite artifact the entry is; the reader that opens it still
 * decides. */
interface Entry {
  name: string;
  kind: string;
  role?: SuiteRole | undefined;
}

/** What Go to runs hands the execution center: the suite and environment the
 * preparation the person saw was made from, the folder that preparation wrote
 * and the release pins it was prepared with ("" for none). It is navigation
 * only; the run view preflights the suite entry and environment again and asks
 * its own explicit send decision. Its preflight takes no release pins and does
 * not read the prepared folder, so those two only name what the person saw. */
export interface SuiteRunHandoff {
  entry: string;
  environment: string;
  prepared: string;
  releases: string;
}

type SuiteRow = SuiteDocument["tables"][number]["rows"][number];

/** A row as the editor holds it: the suite row, and the expected-override
 * text the person edited, exactly as typed. The edit lives on the row itself,
 * so it follows the row through a rename of the row or its table and goes
 * when the row is removed; it is never looked up by IDs another row may take. */
type DraftRow = SuiteRow & { expectedEdit?: string };

function expectedEdit(row: SuiteRow): string | undefined {
  return (row as DraftRow).expectedEdit;
}

/** The position key the composed errors are reported under. */
function rowPosition(tableIndex: number, rowIndex: number): string {
  return `${tableIndex}/${rowIndex}`;
}

/** The suite document the editor holds with every edited expected override
 * applied and the editor's edit text removed, and what could not be applied,
 * by row position. An override nobody edited keeps the value it was opened
 * with; an edit emptied on purpose removes it; an edit that is not typed JSON
 * is reported and never read as "no overrides". */
function composeExpected(document: SuiteDocument): { document: SuiteDocument; errors: Record<string, string> } {
  const errors: Record<string, string> = {};
  const composed: SuiteDocument = {
    ...document,
    tables: document.tables.map((table, tableIndex) => ({
      ...table,
      rows: table.rows.map((held, rowIndex) => {
        const { expectedEdit: edit, ...row } = held as DraftRow;
        if (edit === undefined) {
          return row;
        }
        const text = edit.trim();
        const { expected: _unused, ...rest } = row;
        if (!text) {
          return rest;
        }
        try {
          const expected = JSON.parse(text) as SuiteRow["expected"];
          return expected === undefined ? rest : { ...rest, expected };
        } catch {
          errors[rowPosition(tableIndex, rowIndex)] = `Row ${row.id} of table ${table.id} does not hold typed expected values as JSON.`;
          return row;
        }
      }),
    })),
  };
  return { document: composed, errors };
}

/** The document without the editor's edit text, and the edits by the
 * "table/row" IDs the rows hold now: the suite draft contract's form. */
function splitEdits(document: SuiteDocument): { document: SuiteDocument; edits: Record<string, string> } {
  const edits: Record<string, string> = {};
  const plain: SuiteDocument = {
    ...document,
    tables: document.tables.map((table) => ({
      ...table,
      rows: table.rows.map((held) => {
        const { expectedEdit: edit, ...row } = held as DraftRow;
        if (edit !== undefined) {
          edits[`${table.id}/${row.id}`] = edit;
        }
        return row;
      }),
    })),
  };
  return { document: plain, edits };
}

/** A retained document with its retained edits put back on the rows whose
 * "table/row" IDs they were retained under. */
function joinEdits(document: SuiteDocument, edits: Record<string, string>): SuiteDocument {
  return {
    ...document,
    tables: document.tables.map((table) => ({
      ...table,
      rows: table.rows.map((row) => {
        const edit = edits[`${table.id}/${row.id}`];
        return edit === undefined ? row : ({ ...row, expectedEdit: edit } as DraftRow);
      }),
    })),
  };
}

/** A draft row is blank when nothing was typed into it, and partial when some
 * but not all of its required fields were: a blank row is left out of what is
 * saved, a partial one stops the save and says which fields it lacks. */
function missingFields<T extends Record<string, string>>(row: T, required: (keyof T)[], names: Record<keyof T, string>): string[] | null {
  const filled = required.filter((field) => (row[field] ?? "").trim() !== "");
  if (filled.length === 0 || filled.length === required.length) {
    return null;
  }
  return required.filter((field) => (row[field] ?? "").trim() === "").map((field) => names[field]);
}

function isBlank(row: Record<string, string>): boolean {
  return Object.values(row).every((value) => value.trim() === "");
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
  onExecute?: (handoff: SuiteRunHandoff) => void;
  /** Called once a write added an entry to the workspace, so every panel that
   * offers entries — this one's pickers and the execution center's — offers
   * it too. */
  onSaved?: () => void;
}) {
  const [tab, setTab] = useState<"suite" | "releases" | "prepare" | "coverage" | "promotion">("suite");
  // A coverage assessment runs under the facade's own operation name, so its
  // cancel stops that assessment and can never reach another panel's work.
  const { running, run, cancel } = useLifecycle<"working" | "assessing">({ names: { assessing: "suite-coverage-assessment" } });
  const disabled = busy || running !== null;

  // Each picker offers only the suite artifact its reader takes, by the role
  // the listing names from the entry's declared contract or directory marker:
  // a prepared suite is never offered as a suite definition or a coverage
  // file. The readers still verify what they are handed.
  const ofRole = useCallback(
    (role: SuiteRole) => entries.filter((entry) => entry.role === role).map((entry) => entry.name),
    [entries],
  );
  const suiteEntries = useMemo(() => ofRole("suite-definition"), [ofRole]);
  const preparedEntries = useMemo(() => ofRole("prepared-suite"), [ofRole]);
  const coverageEntries = useMemo(() => ofRole("suite-coverage"), [ofRole]);
  // The separate pin set the durable-run panels and promotion reviews read.
  const releaseEntries = useMemo(() => ofRole("suite-releases"), [ofRole]);
  // A retained test release: what one pin names and what impact compares.
  const testReleaseEntries = useMemo(() => ofRole("test-release"), [ofRole]);
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

  const retainer = useRetainer();

  // Retain the suite being edited as unstored work, the way every editor of
  // this window does: the facade holds it in the local draft store and an
  // interruption returns it. The content is the suite editor's own contract,
  // interpreted by the suite reader at preview and save.
  const retain = useCallback(
    (held: SuiteDocument | null, entry: string) => {
      const { document: doc, edits: expected } = held ? splitEdits(held) : { document: null, edits: {} };
      retainer.save({
        id: retainer.currentId(),
        kind: "suite-editor",
        workspace,
        case: "",
        identity: "",
        content_schema: "readmit-suite-draft/v1",
        content: {
          schema: "readmit-suite-draft/v1",
          entry,
          document: doc ? JSON.stringify(doc) : "",
          ...(Object.keys(expected).length > 0 ? { expected } : {}),
        },
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
    const content = held.content as { schema?: string; entry?: string; document?: string; expected?: Record<string, string> };
    if (content?.schema !== "readmit-suite-draft/v1") {
      return;
    }
    retainer.keepId(held.id);
    setSourceEntry(content.entry ?? "");
    if (content.document) {
      try {
        setDocument(joinEdits(JSON.parse(content.document) as SuiteDocument, content.expected ?? {}));
      } catch {
        setCanonical(content.document);
      }
    }
  }, [drafts, retainer, workspace]);

  const openEntry = useCallback(
    async (entry: string) => {
      await run("working", async () => {
        const result = await openSuite(workspace, entry);
        setOpened(result);
        setImported(null);
        if (result.suite) {
          setDocument(result.suite);
          setSourceEntry(entry);
          setCanonical(result.document ?? "");
          setPreview(null);
          setSaved(null);
          // The editor now holds this document, so what recovery offers back
          // is this document, not whatever unstored work it held before.
          retain(result.suite, entry);
        }
      });
    },
    [retain, run, workspace],
  );

  // The pasted text is read by the suite reader alone; a refusal leaves the
  // editor holding what it held.
  const importCanonical = useCallback(async () => {
    await run("working", async () => {
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
  }, [canonical, retain, run]);

  // A row's expected values are typed JSON the Go reader validates; the panel
  // only keeps the text parseable so the document it hands over is honest. The
  // edit is kept on the row it was made in, retained as typed, invalid text
  // included, and withdraws the preview it no longer matches.
  const setRowExpected = useCallback(
    (tableIndex: number, rowIndex: number, text: string) => {
      if (!document) {
        return;
      }
      const next: SuiteDocument = {
        ...document,
        tables: document.tables.map((table, at) =>
          at !== tableIndex
            ? table
            : { ...table, rows: table.rows.map((row, i) => (i === rowIndex ? ({ ...row, expectedEdit: text } as DraftRow) : row)) },
        ),
      };
      setDocument(next);
      setPreview(null);
      setSaved(null);
      retain(next, sourceEntry);
    },
    [document, retain, sourceEntry],
  );

  // Composed on every render, so what preview and save hand over and the
  // errors shown beside each override are decided together.
  const composition = useMemo(() => (document ? composeExpected(document) : null), [document]);
  const expectedErrors = composition?.errors ?? {};
  const expectedInvalid = Object.keys(expectedErrors).length > 0;

  const runPreview = useCallback(async () => {
    if (!composition || expectedInvalid) {
      return;
    }
    const composed = composition.document;
    await run("working", async () => {
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
  }, [composition, environment, expectedInvalid, previewReleases, run, workspace]);

  const saveRevision = useCallback(async () => {
    if (!composition || expectedInvalid) {
      return;
    }
    const composed = composition.document;
    await run("working", async () => {
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
  }, [composition, expectedInvalid, onSaved, output, retainer, run, workspace]);

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

  // A pin row left partly filled is never dropped quietly: saving stops and
  // the row says what it lacks. Only rows nothing was typed into are left out.
  const [pinsChecked, setPinsChecked] = useState(false);
  const pinProblems = sidecarRows.map((row) =>
    missingFields(row, ["test", "release", "identity"], { test: "Test", release: "Release file", identity: "Release ID" }),
  );

  const writeSidecar = useCallback(async () => {
    if (pinProblems.some((problem) => problem !== null)) {
      setPinsChecked(true);
      setSidecar(null);
      return;
    }
    await run("working", async () => {
      const result = await saveSuiteReleases({
        workspace,
        document: JSON.stringify({
          schema: "readmit-suite-releases/v1",
          tests: sidecarRows.filter((row) => !isBlank(row)),
        }),
        output: sidecarOutput,
      });
      setSidecar(result);
      if (result.state === "completed") onSaved?.();
    });
  }, [onSaved, pinProblems, run, sidecarOutput, sidecarRows, workspace]);

  const readReleaseIdentity = useCallback(
    async (index: number) => {
      const entry = sidecarRows[index]?.release ?? "";
      const earlier = releaseReads[index]?.comparison?.identity;
      await run("working", async () => {
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
    [releaseReads, run, sidecarRows, workspace],
  );

  const runImpact = useCallback(async () => {
    await run("working", async () => {
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
  }, [impactFrom, impactShow, impactSidecar, impactSuite, impactTo, run, workspace]);

  // The team review of the successor release: the hub's review-request and
  // approval commands name the digest of the exact reviewed bytes, derived
  // from the Later release entry — never typed. Identity is the signed-in hub session's;
  // the panel's local approver label does not substitute for it, and the hub
  // re-verifies the release and the request chain it records.
  const [hubProject, setHubProject] = useState("");
  const [teamRecipient, setTeamRecipient] = useState("");
  const [teamRationale, setTeamRationale] = useState("");
  const [teamCommandId, setTeamCommandId] = useState("");
  const [teamReview, setTeamReview] = useState<HubReviewsResult | null>(null);

  const runReleaseReview = useCallback(
    async (kind: "review-request" | "approval") => {
      await run("working", async () => {
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
    [hubProject, impactTo, run, teamCommandId, teamRationale, teamRecipient, workspace],
  );

  // ---------------------------------------------------------------- prepare
  const [prepareEntry, setPrepareEntry] = useState("");
  const [prepareEnvironment, setPrepareEnvironment] = useState("");
  const [prepareReleases, setPrepareReleases] = useState("");
  const [prepareOutput, setPrepareOutput] = useState("");
  // The preparation shown, with the exact selection it was made from: Go to
  // runs hands over that selection, and changing any of it withdraws the
  // result rather than leaving an apparently valid handoff beside new inputs.
  const [prepared, setPrepared] = useState<{ result: SuitePreparedResult; entry: string; environment: string; releases: string } | null>(null);
  const changePrepare = (apply: () => void) => {
    apply();
    setPrepared(null);
  };

  const runPrepare = useCallback(async () => {
    await run("working", async () => {
      setPrepared(null);
      const selection = { entry: prepareEntry, environment: prepareEnvironment, releases: prepareReleases };
      const result = await prepareSuite({ workspace, ...selection, output: prepareOutput });
      setPrepared({ result, ...selection });
      if (result.state === "completed") onSaved?.();
    });
  }, [onSaved, prepareEntry, prepareEnvironment, prepareOutput, prepareReleases, run, workspace]);

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

  // A requirement with jobs but no ID, or an exclusion missing any of its
  // four members, stops the save and says so; a requirement with an ID and no
  // jobs is declared explicitly uncovered, never dropped.
  const [coverageChecked, setCoverageChecked] = useState(false);
  const requirementProblems = requirementRows.map((row) =>
    row.id.trim() === "" && row.jobs.trim() !== "" ? ["Requirement ID"] : null,
  );
  const exclusionProblems = exclusionRows.map((row) =>
    missingFields(row as unknown as Record<string, string>, ["job", "state", "reason", "expires"], {
      job: "Job ID",
      state: "Exclusion state",
      reason: "Reason",
      expires: "Expires (UTC)",
    }),
  );

  const writeCoverage = useCallback(async () => {
    if ([...requirementProblems, ...exclusionProblems].some((problem) => problem !== null)) {
      setCoverageChecked(true);
      setAuthored(null);
      return;
    }
    await run("working", async () => {
      setAuthored(null);
      const result = await saveSuiteCoverage({
        workspace,
        prepared: coveragePrepared,
        requirements: requirementRows
          .filter((row) => !isBlank(row))
          .map((row) => ({ id: row.id, jobs: row.jobs.split(/[\s,]+/).filter(Boolean) })),
        exclusions: exclusionRows.filter((row) => !isBlank(row as unknown as Record<string, string>)),
        output: coverageOutput,
      });
      setAuthored(result);
      if (result.state === "completed") onSaved?.();
    });
  }, [coverageOutput, coveragePrepared, exclusionProblems, exclusionRows, onSaved, requirementProblems, requirementRows, run, workspace]);

  const runAssessment = useCallback(async () => {
    await run("assessing", async () => {
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
  }, [assessPrepared, assessPrevious, assessRequirements, run, workspace]);

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
  // The review describes the suite, environment, release pins and revision it
  // was asked for. Changing any of them withdraws the review and its approval
  // affordances; a typed name or rationale cannot bring a stale review back.
  const changePromotion = (apply: () => void) => {
    apply();
    setReview(null);
    setApproval(null);
  };

  const runReview = useCallback(async () => {
    await run("working", async () => {
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
  }, [promotionEntry, promotionEnvironment, promotionReleases, revision, run, workspace]);

  const runApproval = useCallback(async () => {
    await run("working", async () => {
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
  }, [approver, onSaved, promotionEntry, promotionEnvironment, promotionOutput, promotionReleases, rationale, review, revision, run, workspace]);

  return (
    <section className="suite-panel" aria-labelledby="suite-title">
      <h2 id="suite-title">Suites</h2>
      <p>
        Author suites, release expectations, declare coverage and approve environment promotion over the one canonical
        contract. Nothing here sends: preparation compiles configuration only, and execution is the execution center&rsquo;s
        explicit step.
      </p>
      {/* Real tabs, not navigation buttons: one tablist, one selected tab per
       * tabpanel, automatic activation on arrow keys, and Home/End to the
       * ends — the W3C tabs pattern, so what assistive technology announces
       * and what a keyboard does are the same relationship the eye sees. */}
      <div className="suite-tabs" role="tablist" aria-label="Suite views">
        {(["suite", "releases", "prepare", "coverage", "promotion"] as const).map((name, position, tabs) => (
          <button
            key={name}
            type="button"
            role="tab"
            id={`suite-tab-${name}`}
            aria-selected={tab === name}
            aria-controls="suite-tabpanel"
            tabIndex={tab === name ? 0 : -1}
            className={`suite-tab ${tab === name ? "active" : ""}`}
            onKeyDown={(event) => {
              const move = (to: number) => {
                event.preventDefault();
                const next = tabs[to] ?? name;
                setTab(next);
                // The panel's own document state shadows the DOM's document.
                globalThis.document.getElementById(`suite-tab-${next}`)?.focus();
              };
              if (event.key === "ArrowRight") move((position + 1) % tabs.length);
              else if (event.key === "ArrowLeft") move((position - 1 + tabs.length) % tabs.length);
              else if (event.key === "Home") move(0);
              else if (event.key === "End") move(tabs.length - 1);
            }}
            onClick={() => setTab(name)}
          >
            {name === "suite" ? "Configuration" : name === "releases" ? "Releases" : name === "prepare" ? "Prepare" : name === "coverage" ? "Coverage" : "Promotion"}
          </button>
        ))}
      </div>
      <RetentionStatus retention={retainer.retention} />
      {/* One tabpanel, named by whichever tab is selected; each tab controls
       * it, and the selected tab's own id labels it. */}
      <div role="tabpanel" id="suite-tabpanel" aria-labelledby={`suite-tab-${tab}`} tabIndex={0} className="suite-tabpanel">
      {tab === "suite" ? (
        <fieldset disabled={disabled}>
          <legend>Suite configuration</legend>
          <div className="suite-row">
            <label>
              Suite{" "}
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
              expectedErrors={expectedErrors}
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
              <select
                value={environment}
                onChange={(event) => {
                  setEnvironment(event.target.value);
                  setPreview(null);
                }}
              >
                <option value="">(select)</option>
                {(document?.environments ?? []).map((env) => (
                  <option key={env.id} value={env.id}>
                    {env.id}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Release pins (optional){" "}
              <select
                value={previewReleases}
                onChange={(event) => {
                  setPreviewReleases(event.target.value);
                  setPreview(null);
                }}
              >
                <option value="">(none)</option>
                {releaseEntries.map((name) => (
                  <option key={name} value={name}>
                    {name}
                  </option>
                ))}
              </select>
            </label>
            <button type="button" disabled={!document || !environment || expectedInvalid} onClick={() => void runPreview()}>
              Preview
            </button>
          </div>
          {expectedInvalid ? (
            <p role="alert">
              An expected override is not typed JSON. Correct or clear it before previewing or saving; the edit is kept as
              typed.
            </p>
          ) : null}
          <ExpansionView result={preview} />
          <div className="suite-row">
            <label>
              Version file <input value={output} onChange={(event) => setOutput(event.target.value)} />
            </label>
            <button
              type="button"
              disabled={!document || !output.trim() || expectedInvalid}
              onClick={() => void saveRevision()}
            >
              Save version
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
              Import JSON
            </button>
            <p className="hint">Validates the pasted JSON and loads it into the editor; nothing is saved.</p>
            {imported && imported.state !== "completed" ? <p role="alert">{imported.reason}</p> : null}
            {imported?.state === "completed" ? <p role="status">Validated and loaded into the editor; nothing was saved.</p> : null}
          </details>
        </fieldset>
      ) : null}
      {tab === "releases" ? (
        <fieldset disabled={disabled}>
          <legend>Version impact</legend>
          <table className="suite-table">
            <caption>readmit-suite-releases/v1: one exact released identity per suite test</caption>
            <thead>
              <tr>
                <th>Test</th>
                <th>Release file</th>
                <th>Release ID</th>
                <th>Release details</th>
                <th>
                  <span className="visually-hidden">Remove</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {sidecarRows.map((row, index) => (
                <tr key={index}>
                  <td>
                    <input
                      aria-label={`Test ${index + 1}`}
                      aria-invalid={pinsChecked && pinProblems[index] !== null && !row.test.trim() ? true : undefined}
                      value={row.test}
                      onChange={(event) =>
                        setSidecarRows((rows) => rows.map((held, at) => (at === index ? { ...held, test: event.target.value } : held)))
                      }
                    />
                  </td>
                  <td>
                    <select
                      aria-label={`Release file ${index + 1}`}
                      aria-invalid={pinsChecked && pinProblems[index] !== null && !row.release.trim() ? true : undefined}
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
                    >
                      <option value="">(select)</option>
                      {testReleaseEntries.map((name) => (
                        <option key={name} value={name}>
                          {name}
                        </option>
                      ))}
                    </select>
                  </td>
                  <td>
                    <input
                      aria-label={`Release ID ${index + 1}`}
                      aria-invalid={pinsChecked && pinProblems[index] !== null && !row.identity.trim() ? true : undefined}
                      value={row.identity}
                      onChange={(event) =>
                        setSidecarRows((rows) => rows.map((held, at) => (at === index ? { ...held, identity: event.target.value } : held)))
                      }
                    />
                  </td>
                  <td>
                    <button
                      type="button"
                      aria-label={`Read release ID of release file ${index + 1}`}
                      disabled={!row.release.trim()}
                      onClick={() => void readReleaseIdentity(index)}
                    >
                      Read release ID
                    </button>
                    <ReleaseRead result={releaseReads[index]} />
                    {pinsChecked && pinProblems[index] ? (
                      <p role="alert">
                        Pin {index + 1} is missing {pinProblems[index]?.join(", ")}. Complete it or remove the pin.
                      </p>
                    ) : null}
                  </td>
                  <td>
                    <button
                      type="button"
                      aria-label={`Remove pin ${index + 1}`}
                      onClick={() => {
                        setSidecarRows((rows) => rows.filter((_, at) => at !== index));
                        // Reads follow their rows' positions.
                        setReleaseReads((reads) => {
                          const next: Record<number, BaselineResult | undefined> = {};
                          for (const [at, read] of Object.entries(reads)) {
                            const position = Number(at);
                            if (position < index) next[position] = read;
                            else if (position > index) next[position - 1] = read;
                          }
                          return next;
                        });
                        setSidecar(null);
                      }}
                    >
                      Remove pin
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          <button type="button" onClick={() => setSidecarRows((rows) => [...rows, { test: "", release: "", identity: "" }])}>
            Pin test version
          </button>
          <div className="suite-row">
            <label>
              Release pins file <input value={sidecarOutput} onChange={(event) => setSidecarOutput(event.target.value)} />
            </label>
            <button type="button" disabled={!sidecarOutput.trim()} onClick={() => void writeSidecar()}>
              Save release pins
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
              Earlier release{" "}
              <select value={impactFrom} aria-describedby="suite-impact-successor" onChange={(event) => setImpactFrom(event.target.value)}>
                <option value="">(select)</option>
                {testReleaseEntries.map((name) => (
                  <option key={name} value={name}>
                    {name}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Later release{" "}
              <select value={impactTo} aria-describedby="suite-impact-successor" onChange={(event) => setImpactTo(event.target.value)}>
                <option value="">(select)</option>
                {testReleaseEntries.map((name) => (
                  <option key={name} value={name}>
                    {name}
                  </option>
                ))}
              </select>
            </label>
            <span className="hint" id="suite-impact-successor">
              The later release must be the earlier release&rsquo;s direct successor.
            </span>
            <label>
              <input type="checkbox" checked={impactShow} onChange={(event) => setImpactShow(event.target.checked)} /> Reveal exact values
            </label>
            <button
              type="button"
              disabled={!impactSuite || !impactSidecar || !impactFrom || !impactTo}
              onClick={() => void runImpact()}
            >
              Compare versions
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
                  {[...impact.impact.comparison.baseline.changes, ...impact.impact.comparison.profiles].map((change, index) => (
                    <tr key={index}>
                      <th>{change.part}</th>
                      <td>{change.kind}</td>
                      <td>
                        <pre>{change.before ?? (impactShow ? "Absent" : HIDDEN_VALUE)}</pre>
                      </td>
                      <td>
                        <pre>{change.after ?? (impactShow ? "Absent" : HIDDEN_VALUE)}</pre>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </>
          ) : null}
          <h3>Release review</h3>
          <p>
            Request review of, or approve, the exact released expectation named by the Later release entry. The
            commands name the digest of those reviewed bytes — never a typed value. Identity is the
            signed-in hub session&rsquo;s: the promotion tab&rsquo;s local approver label records a local
            decision and cannot approve for the team. Approval chains to the outstanding request naming
            these exact bytes, and a changed grant or stale head requires a renewed action.
          </p>
          <div className="suite-row">
            <label>
              Project <input value={hubProject} onChange={(event) => setHubProject(event.target.value)} />
            </label>
            <label>
              Reviewer <input value={teamRecipient} onChange={(event) => setTeamRecipient(event.target.value)} />
            </label>
            <label>
              Command ID <input value={teamCommandId} onChange={(event) => setTeamCommandId(event.target.value)} />
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
              Request review
            </button>
            <button
              type="button"
              disabled={!hubProject.trim() || !impactTo.trim() || !teamCommandId.trim() || !teamRationale.trim()}
              onClick={() => void runReleaseReview("approval")}
            >
              Approve release
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
        <fieldset disabled={disabled} aria-label="Prepare a suite">
          <p>Compile a saved suite against one declared environment into a new private directory. Nothing is sent and existing output is never resumed.</p>
          <div className="suite-row">
            <label>
              Suite entry{" "}
              <select value={prepareEntry} onChange={(event) => changePrepare(() => setPrepareEntry(event.target.value))}>
                <option value="">(select)</option>
                {suiteEntries.map((name) => (
                  <option key={name} value={name}>
                    {name}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Environment <input value={prepareEnvironment} onChange={(event) => changePrepare(() => setPrepareEnvironment(event.target.value))} />
            </label>
            <label>
              Release pins (optional){" "}
              <select value={prepareReleases} onChange={(event) => changePrepare(() => setPrepareReleases(event.target.value))}>
                <option value="">(none)</option>
                {releaseEntries.map((name) => (
                  <option key={name} value={name}>
                    {name}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Output folder <input value={prepareOutput} onChange={(event) => changePrepare(() => setPrepareOutput(event.target.value))} />
            </label>
            <button
              type="button"
              disabled={!prepareEntry || !prepareEnvironment || !prepareOutput.trim()}
              onClick={() => void runPrepare()}
            >
              Prepare suite
            </button>
          </div>
          <p role="status">
            {prepared?.result.reason ??
              (prepared?.result.state === "completed"
                ? `Prepared ${prepared.result.directory} from ${prepared.entry} against environment ${prepared.environment}${prepared.releases ? ` with release pins ${prepared.releases}` : ""}; ${prepared.result.queue?.jobs.length ?? 0} jobs. Nothing was sent.`
                : "")}
          </p>
          {prepared?.result.queue ? (
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
                {prepared.result.queue.jobs.map((job) => (
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
          {prepared?.result.state === "completed" ? (
            <>
              <p className="hint">
                Execution continues in the execution center with {prepared.entry} and environment {prepared.environment}{" "}
                selected, handed over from prepared folder {prepared.result.directory}
                {prepared.releases ? ` with release pins ${prepared.releases}` : " with no release pins"}. It preflights the
                suite again and asks for the explicit send decision; nothing was executed here.
                {prepared.releases
                  ? " The run view does not apply these release pins or read the prepared folder: its own preflight decides what runs."
                  : " The run view does not read the prepared folder: its own preflight decides what runs."}
              </p>
              {onExecute ? (
                <button
                  type="button"
                  onClick={() =>
                    onExecute({
                      entry: prepared.entry,
                      environment: prepared.environment,
                      prepared: prepared.result.directory ?? "",
                      releases: prepared.releases,
                    })
                  }
                >
                  Go to runs
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
          <h3 className="visually-hidden">Coverage declaration</h3>
          <p>The suite digest and every specification pin are computed from the retained bytes of a prepared suite; only the requirements and exclusions are declarations. An exclusion never filters execution and expiry never enables a send.</p>
          <div className="suite-row">
            <label>
              Prepared suite{" "}
              <select value={coveragePrepared} onChange={(event) => setCoveragePrepared(event.target.value)}>
                <option value="">(select)</option>
                {preparedEntries.map((name) => (
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
                <th>Requirement ID</th>
                <th>Job IDs</th>
                <th>
                  <span className="visually-hidden">Remove</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {requirementRows.map((row, index) => (
                <tr key={index}>
                  <td>
                    <input
                      aria-label={`Requirement ID ${index + 1}`}
                      aria-invalid={coverageChecked && requirementProblems[index] !== null ? true : undefined}
                      value={row.id}
                      onChange={(event) =>
                        setRequirementRows((rows) => rows.map((held, at) => (at === index ? { ...held, id: event.target.value } : held)))
                      }
                    />
                  </td>
                  <td>
                    <input
                      aria-label={`Job IDs ${index + 1}`}
                      value={row.jobs as unknown as string}
                      onChange={(event) =>
                        setRequirementRows((rows) => rows.map((held, at) => (at === index ? { ...held, jobs: event.target.value } : held)))
                      }
                    />
                    {coverageChecked && requirementProblems[index] ? (
                      <p role="alert">Requirement {index + 1} names jobs but no Requirement ID. Complete it or remove the requirement.</p>
                    ) : null}
                  </td>
                  <td>
                    <button
                      type="button"
                      aria-label={`Remove requirement ${index + 1}`}
                      onClick={() => {
                        setRequirementRows((rows) => rows.filter((_, at) => at !== index));
                        setAuthored(null);
                      }}
                    >
                      Remove requirement
                    </button>
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
          <p className="hint">Job IDs are separated by commas or spaces. A requirement with no job IDs stays in the denominator as uncovered.</p>
          <table className="suite-table">
            <caption>Exclusions and quarantine: state, reason and UTC expiry to the second</caption>
            <thead>
              <tr>
                <th>Job ID</th>
                <th>Exclusion state</th>
                <th>Reason</th>
                <th>Expires (UTC)</th>
                <th>
                  <span className="visually-hidden">Remove</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {exclusionRows.map((row, index) => (
                <tr key={index}>
                  <td>
                    <input
                      aria-label={`Exclusion job ID ${index + 1}`}
                      aria-invalid={coverageChecked && exclusionProblems[index] !== null && !row.job.trim() ? true : undefined}
                      value={row.job}
                      onChange={(event) =>
                        setExclusionRows((rows) => rows.map((held, at) => (at === index ? { ...held, job: event.target.value } : held)))
                      }
                    />
                  </td>
                  <td>
                    <select
                      aria-label={`Exclusion state ${index + 1}`}
                      aria-invalid={coverageChecked && exclusionProblems[index] !== null && !row.state ? true : undefined}
                      value={row.state}
                      onChange={(event) =>
                        setExclusionRows((rows) => rows.map((held, at) => (at === index ? { ...held, state: event.target.value } : held)))
                      }
                    >
                      <option value="">(select)</option>
                      {/* Display case only: each value keeps its own assessment effect. */}
                      <option value="skipped">Skipped</option>
                      <option value="unsupported">Unsupported</option>
                      <option value="quarantined">Quarantined</option>
                      <option value="disabled">Disabled</option>
                    </select>
                  </td>
                  <td>
                    <input
                      aria-label={`Exclusion reason ${index + 1}`}
                      aria-invalid={coverageChecked && exclusionProblems[index] !== null && !row.reason.trim() ? true : undefined}
                      value={row.reason}
                      onChange={(event) =>
                        setExclusionRows((rows) => rows.map((held, at) => (at === index ? { ...held, reason: event.target.value } : held)))
                      }
                    />
                  </td>
                  <td>
                    <input
                      aria-label={`Exclusion expiry ${index + 1}`}
                      aria-invalid={coverageChecked && exclusionProblems[index] !== null && !row.expires.trim() ? true : undefined}
                      placeholder="2026-10-01T00:00:00Z"
                      value={row.expires}
                      onChange={(event) =>
                        setExclusionRows((rows) => rows.map((held, at) => (at === index ? { ...held, expires: event.target.value } : held)))
                      }
                    />
                    {coverageChecked && exclusionProblems[index] ? (
                      <p role="alert">
                        Exclusion {index + 1} is missing {exclusionProblems[index]?.join(", ")}. Complete it or remove the exclusion.
                      </p>
                    ) : null}
                  </td>
                  <td>
                    <button
                      type="button"
                      aria-label={`Remove exclusion ${index + 1}`}
                      onClick={() => {
                        setExclusionRows((rows) => rows.filter((_, at) => at !== index));
                        setAuthored(null);
                      }}
                    >
                      Remove exclusion
                    </button>
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
              Coverage file <input value={coverageOutput} onChange={(event) => setCoverageOutput(event.target.value)} />
            </label>
            <button type="button" disabled={!coveragePrepared || !coverageOutput.trim()} onClick={() => void writeCoverage()}>
              Save coverage
            </button>
          </div>
          <p role="status">{authored?.reason ?? (authored?.state === "completed" ? `Saved ${authored.output}.` : "")}</p>
          <div role="group" aria-labelledby="suite-assess-title">
          <h3 id="suite-assess-title">Assess coverage</h3>
          <div className="suite-row">
            <label>
              Prepared suite{" "}
              <select value={assessPrepared} onChange={(event) => setAssessPrepared(event.target.value)}>
                <option value="">(select)</option>
                {preparedEntries.map((name) => (
                  <option key={name} value={name}>
                    {name}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Coverage file{" "}
              <select value={assessRequirements} onChange={(event) => setAssessRequirements(event.target.value)}>
                <option value="">(select)</option>
                {coverageEntries.map((name) => (
                  <option key={name} value={name}>
                    {name}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Previous run folders{" "}
              <textarea value={assessPrevious} aria-describedby="suite-assess-previous" onChange={(event) => setAssessPrevious(event.target.value)} />
            </label>
            <span className="hint" id="suite-assess-previous">
              One per line: retained earlier runs of the same suite and environment, read for stability history.
            </span>
            <button type="button" disabled={!assessPrepared || !assessRequirements} onClick={() => void runAssessment()}>
              Assess coverage
            </button>
          </div>
          </div>
        </fieldset>
        {running === "assessing" ? (
          <button
            type="button"
            onClick={() => {
              // The cancel names this panel's assessment, so it can never
              // reach an operation another panel started.
              cancel();
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
        <fieldset disabled={disabled} aria-label="Environment promotion">
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
              <select value={promotionEntry} onChange={(event) => changePromotion(() => setPromotionEntry(event.target.value))}>
                <option value="">(select)</option>
                {suiteEntries.map((name) => (
                  <option key={name} value={name}>
                    {name}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Environment <input value={promotionEnvironment} onChange={(event) => changePromotion(() => setPromotionEnvironment(event.target.value))} />
            </label>
            <label>
              Release references{" "}
              <select value={promotionReleases} onChange={(event) => changePromotion(() => setPromotionReleases(event.target.value))}>
                <option value="">(select)</option>
                {releaseEntries.map((name) => (
                  <option key={name} value={name}>
                    {name}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Target revision <input value={revision} onChange={(event) => changePromotion(() => setRevision(event.target.value))} />
            </label>
            <span className="hint">Operator-declared</span>
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
                  Rationale <input value={rationale} onChange={(event) => setRationale(event.target.value)} />
                </label>
                <label>
                  Approval file <input value={promotionOutput} onChange={(event) => setPromotionOutput(event.target.value)} />
                </label>
                <button
                  type="button"
                  disabled={!approver.trim() || !rationale.trim() || !promotionOutput.trim()}
                  onClick={() => void runApproval()}
                >
                  Approve promotion
                </button>
              </div>
            </>
          ) : null}
          <p role="status">
            {approval?.reason ?? (approval?.state === "completed" ? `Approved and saved ${approval.output}. Approval identity: ${approval.identity}` : "")}
          </p>
        </fieldset>
      ) : null}
      </div>
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
  expectedErrors,
  specEntries,
  caseEntries,
  targetEntries,
  onChange,
  onExpected,
}: {
  document: SuiteDocument;
  /** Why an edited override cannot be applied, by row position. */
  expectedErrors: Record<string, string>;
  specEntries: string[];
  caseEntries: string[];
  targetEntries: string[];
  onChange: (next: SuiteDocument) => void;
  onExpected: (tableIndex: number, rowIndex: number, text: string) => void;
}) {
  const list = (values: string[]) => values.join(", ");
  const parsed = (text: string): string[] => text.split(/[\s,]+/).filter(Boolean);
  // An item other parts of the suite name is removed only after the person
  // has seen what names it; one nothing names is removed at once.
  const [pending, setPending] = useState<{ key: string; references: string[]; remove: () => void } | null>(null);
  // A confirmation describes the suite as it was when asked; any edit since
  // withdraws it rather than removing from a stale copy.
  useEffect(() => setPending(null), [document]);
  const removeChecked = (key: string, references: string[], remove: () => void) => {
    if (references.length === 0) {
      setPending(null);
      remove();
    } else {
      setPending({ key, references, remove });
    }
  };
  const confirmation = (key: string) =>
    pending?.key === key ? (
      <div role="group" aria-label="Confirm removal" className="suite-row">
        <p role="alert">
          Still referenced by {pending.references.join("; ")}. Removing it leaves those references unresolved until you change them.
        </p>
        <button
          type="button"
          onClick={() => {
            pending.remove();
            setPending(null);
          }}
        >
          Confirm removal
        </button>
        <button type="button" onClick={() => setPending(null)}>
          Cancel
        </button>
      </div>
    ) : null;
  const setEnvironments = (environments: SuiteDocument["environments"]) => onChange({ ...document, environments });
  const setTables = (tables: SuiteDocument["tables"]) => onChange({ ...document, tables });
  const setTests = (tests: SuiteDocument["tests"]) => onChange({ ...document, tests });
  return (
    <div className="suite-editor">
      <div className="suite-row">
        <label>
          Suite ID <input value={document.id} onChange={(event) => onChange({ ...document, id: event.target.value })} />
        </label>
        <label>
          Owner <input value={document.owner} onChange={(event) => onChange({ ...document, owner: event.target.value })} />
        </label>
        <label>
          Tags <input value={list(document.tags)} onChange={(event) => onChange({ ...document, tags: parsed(event.target.value) })} />
        </label>
        <label>
          Maximum parallel jobs{" "}
          <input
            type="number"
            min={1}
            max={16}
            aria-describedby="suite-parallelism-help"
            value={document.parallelism}
            onChange={(event) => onChange({ ...document, parallelism: Number(event.target.value) })}
          />
        </label>
        <span className="hint" id="suite-parallelism-help">
          1 to 16. An upper bound, not a promise: shared-state tests still hold the environment one at a time.
        </span>
      </div>
      <h3>Environments</h3>
      {document.environments.map((env, envIndex) => (
        <div className="suite-block" key={envIndex}>
          <div className="suite-row">
            <label>
              Environment ID{" "}
              <input
                value={env.id}
                onChange={(event) =>
                  setEnvironments(document.environments.map((held, at) => (at === envIndex ? { ...held, id: event.target.value } : held)))
                }
              />
            </label>
            <label>
              Site{" "}
              <input
                value={env.site}
                onChange={(event) =>
                  setEnvironments(document.environments.map((held, at) => (at === envIndex ? { ...held, site: event.target.value } : held)))
                }
              />
            </label>
            <button
              type="button"
              aria-label={`Remove environment ${env.id || envIndex + 1}`}
              onClick={() => setEnvironments(document.environments.filter((_, at) => at !== envIndex))}
            >
              Remove environment
            </button>
          </div>
          {env.bindings.map((binding, bindingIndex) => {
            const setBinding = (change: Partial<typeof binding>) =>
              setEnvironments(
                document.environments.map((held, at) =>
                  at === envIndex ? { ...held, bindings: held.bindings.map((row, i) => (i === bindingIndex ? { ...row, ...change } : row)) } : held,
                ),
              );
            const key = `binding/${envIndex}/${bindingIndex}`;
            const users = binding.parameter
              ? document.tests.filter((test) => test.parameter === binding.parameter).map((test) => `test ${test.id || "(no ID)"}`)
              : [];
            return (
              <div key={bindingIndex}>
                <div className="suite-row">
                  <label>
                    Parameter ID <input value={binding.parameter} onChange={(event) => setBinding({ parameter: event.target.value })} />
                  </label>
                  <label>
                    Target{" "}
                    <select value={binding.target} onChange={(event) => setBinding({ target: event.target.value })}>
                      <option value="">(select)</option>
                      {targetEntries.map((name) => (
                        <option key={name} value={name}>
                          {name}
                        </option>
                      ))}
                    </select>
                  </label>
                  <label>
                    Observation file{" "}
                    <input
                      value={binding.observation ?? ""}
                      aria-describedby={`suite-observation-help-${envIndex}-${bindingIndex}`}
                      onChange={(event) => setBinding({ observation: event.target.value })}
                    />
                  </label>
                  <span className="hint" id={`suite-observation-help-${envIndex}-${bindingIndex}`}>
                    Ledger tests only: the saved observation reference this binding supplies.
                  </span>
                  <button
                    type="button"
                    aria-label={`Remove binding ${binding.parameter || bindingIndex + 1} of environment ${env.id || envIndex + 1}`}
                    onClick={() =>
                      removeChecked(key, users, () =>
                        setEnvironments(
                          document.environments.map((held, at) =>
                            at === envIndex ? { ...held, bindings: held.bindings.filter((_, i) => i !== bindingIndex) } : held,
                          ),
                        ),
                      )
                    }
                  >
                    Remove binding
                  </button>
                </div>
                {confirmation(key)}
              </div>
            );
          })}
          <button
            type="button"
            onClick={() =>
              setEnvironments(
                document.environments.map((held, at) =>
                  at === envIndex ? { ...held, bindings: [...held.bindings, { parameter: "", target: "", observation: "" }] } : held,
                ),
              )
            }
          >
            Add binding
          </button>
        </div>
      ))}
      <button type="button" onClick={() => onChange({ ...document, environments: [...document.environments, { id: "", site: "", bindings: [{ parameter: "", target: "" }] }] })}>
        Add environment
      </button>
      <h3>Data tables</h3>
      {document.tables.map((table, tableIndex) => {
        const tableKey = `table/${tableIndex}`;
        const tableUsers = table.id ? document.tests.filter((test) => test.table === table.id).map((test) => `test ${test.id || "(no ID)"}`) : [];
        return (
          <div className="suite-block" key={tableIndex}>
            <div className="suite-row">
              <label>
                Table ID{" "}
                <input
                  value={table.id}
                  onChange={(event) => setTables(document.tables.map((held, at) => (at === tableIndex ? { ...held, id: event.target.value } : held)))}
                />
              </label>
              <button
                type="button"
                aria-label={`Remove table ${table.id || tableIndex + 1}`}
                onClick={() => removeChecked(tableKey, tableUsers, () => setTables(document.tables.filter((_, at) => at !== tableIndex)))}
              >
                Remove table
              </button>
            </div>
            {confirmation(tableKey)}
            {table.rows.map((row, rowIndex) => {
              const setRow = (change: Partial<typeof row>) =>
                setTables(
                  document.tables.map((held, at) =>
                    at === tableIndex ? { ...held, rows: held.rows.map((held2, i) => (i === rowIndex ? { ...held2, ...change } : held2)) } : held,
                  ),
                );
              const error = expectedErrors[rowPosition(tableIndex, rowIndex)];
              const help = `suite-expected-help-${tableIndex}-${rowIndex}`;
              return (
                <div className="suite-row" key={rowIndex}>
                  <label>
                    Row ID <input value={row.id} onChange={(event) => setRow({ id: event.target.value })} />
                  </label>
                  <label>
                    Case{" "}
                    <select value={row.case} onChange={(event) => setRow({ case: event.target.value })}>
                      <option value="">(select)</option>
                      {caseEntries.map((name) => (
                        <option key={name} value={name}>
                          {name}
                        </option>
                      ))}
                    </select>
                  </label>
                  <label>
                    Expected overrides{" "}
                    <textarea
                      aria-label={`Expected overrides for row ${row.id} of table ${table.id}`}
                      aria-describedby={help}
                      aria-invalid={error ? true : undefined}
                      value={expectedEdit(row) ?? JSON.stringify(row.expected ?? {})}
                      onChange={(event) => onExpected(tableIndex, rowIndex, event.target.value)}
                    />
                  </label>
                  <span className="hint" id={help}>
                    {error ?? "Typed JSON: an object of expected values for this row. Clear it to remove the overrides."}
                  </span>
                  <button
                    type="button"
                    aria-label={`Remove row ${row.id || rowIndex + 1} of table ${table.id || tableIndex + 1}`}
                    onClick={() =>
                      setTables(document.tables.map((held, at) => (at === tableIndex ? { ...held, rows: held.rows.filter((_, i) => i !== rowIndex) } : held)))
                    }
                  >
                    Remove row
                  </button>
                </div>
              );
            })}
            <button
              type="button"
              onClick={() => setTables(document.tables.map((held, at) => (at === tableIndex ? { ...held, rows: [...held.rows, { id: "", case: "" }] } : held)))}
            >
              Add row
            </button>
          </div>
        );
      })}
      <button type="button" onClick={() => onChange({ ...document, tables: [...document.tables, { id: "", rows: [{ id: "", case: "" }] }] })}>
        Add table
      </button>
      <h3>Tests</h3>
      {document.tests.map((test, testIndex) => {
        const setTest = (change: Partial<typeof test>) =>
          setTests(document.tests.map((held, at) => (at === testIndex ? { ...held, ...change } : held)));
        const testKey = `test/${testIndex}`;
        const testUsers = test.id
          ? document.tests.filter((other, at) => at !== testIndex && (other.after ?? []).includes(test.id)).map((other) => `test ${other.id || "(no ID)"}`)
          : [];
        const isolationHelp = `suite-isolation-help-${testIndex}`;
        const afterHelp = `suite-after-help-${testIndex}`;
        const sequenceHelp = `suite-sequence-help-${testIndex}`;
        return (
          <div className="suite-block" key={testIndex}>
            <div className="suite-row">
              <label>
                Test ID <input value={test.id} onChange={(event) => setTest({ id: event.target.value })} />
              </label>
              <label>
                Test template{" "}
                <select value={test.spec} onChange={(event) => setTest({ spec: event.target.value })}>
                  <option value="">(select)</option>
                  {specEntries.map((name) => (
                    <option key={name} value={name}>
                      {name}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                Owner <input value={test.owner} onChange={(event) => setTest({ owner: event.target.value })} />
              </label>
              <label>
                Tags <input value={list(test.tags)} onChange={(event) => setTest({ tags: parsed(event.target.value) })} />
              </label>
              <button
                type="button"
                aria-label={`Remove test ${test.id || testIndex + 1}`}
                onClick={() => removeChecked(testKey, testUsers, () => setTests(document.tests.filter((_, at) => at !== testIndex)))}
              >
                Remove test
              </button>
            </div>
            {confirmation(testKey)}
            <div className="suite-row">
              <label>
                Parameter ID <input value={test.parameter} onChange={(event) => setTest({ parameter: event.target.value })} />
              </label>
              <label>
                Data table{" "}
                <select value={test.table} onChange={(event) => setTest({ table: event.target.value })}>
                  <option value="">(select)</option>
                  {document.tables.map((table) => (
                    <option key={table.id} value={table.id}>
                      {table.id}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                State isolation{" "}
                <select
                  value={test.isolation}
                  aria-describedby={isolationHelp}
                  onChange={(event) => setTest({ isolation: event.target.value as RunQueueIsolation })}
                >
                  <option value="shared">Shared state</option>
                  <option value="isolated">Isolated state</option>
                </select>
              </label>
              <span className="hint" id={isolationHelp}>
                {test.isolation === "isolated" ? "Isolated state: may overlap with other jobs." : "Shared state: holds the environment while it runs."}
              </span>
              <label>
                Prerequisite tests{" "}
                <input value={list(test.after ?? [])} aria-describedby={afterHelp} onChange={(event) => setTest({ after: parsed(event.target.value) })} />
              </label>
              <span className="hint" id={afterHelp}>
                Test IDs, separated by commas or spaces. This test runs only after each of them.
              </span>
              <label>
                Message send order{" "}
                <input value={list(test.sequence)} aria-describedby={sequenceHelp} onChange={(event) => setTest({ sequence: parsed(event.target.value) })} />
              </label>
              <span className="hint" id={sequenceHelp}>
                The exact sequence the messages are sent in, separated by commas or spaces; it is never re-sorted.
              </span>
            </div>
          </div>
        );
      })}
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
