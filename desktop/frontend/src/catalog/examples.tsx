import { cloneElement, type ReactElement } from "react";
import * as f from "../testkit/fixtures";
import { artifacts, catalogRows } from "./fixtures";
import { VocabularyContext } from "../vocabulary";
import { IndicatorsContext, Outcome } from "../lifecycle";
import { AssertionSetAuthoring } from "../AssertionSetAuthoring";
import { Baseline } from "../Baseline";
import { CanonicalTestEditor } from "../CanonicalTestEditor";
import { CapturePanel } from "../CapturePanel";
import { Comparison } from "../Comparison";
import { ComputerLicense } from "../ComputerLicense";
import { StateHelp, HelpTopics } from "../ContextHelp";
import { ControlledDetails } from "../ControlledDetails";
import { CorrelationReview } from "../CorrelationReview";
import { Diagnosis } from "../Diagnosis";
import { EnvironmentBanner, EnvironmentPanel } from "../EnvironmentPanel";
import { GuidedSample } from "../GuidedSample";
import { NewProjectForm, DemoCallout } from "../Home";
import { HubAdministration } from "../HubAdministration";
import { HubPanel } from "../HubPanel";
import { IconButton } from "../IconButton";
import { ImportPanel } from "../ImportPanel";
import { Inspector } from "../Inspector";
import { MaintenancePanel } from "../MaintenancePanel";
import { NoteDraft } from "../NoteDraft";
import { ObservationPanel } from "../ObservationPanel";
import { OperationAccess } from "../OperationAccess";
import { OperatorHub } from "../OperatorHub";
import { PacketPanel } from "../PacketPanel";
import { PerformanceCorpus } from "../PerformanceCorpus";
import { PrivacyDisclosure, SupportGuidance } from "../PrivacyDisclosure";
import { PrivacyDocuments } from "../PrivacyDocuments";
import { PrivacyPanel } from "../PrivacyPanel";
import { ProfileEditor } from "../ProfileEditor";
import { ProjectPanel } from "../ProjectPanel";
import { ProtectionPanel } from "../ProtectionPanel";
import { RawInspection } from "../RawInspection";
import { RecentWorkspaces } from "../RecentWorkspaces";
import { Recovery, RetainedDrafts } from "../Recovery";
import { Reduction } from "../Reduction";
import { Reexecution } from "../Reexecution";
import { ReplayPanel } from "../ReplayPanel";
import { Reproducer } from "../Reproducer";
import { Review } from "../Review";
import { RevisionComparison } from "../RevisionComparison";
import {
  CorrelationRulesEditor,
  SequenceAnalysisEditor,
  NormalizationPolicyEditor,
  DiagnoseConfigEditor,
} from "../RulesEditor";
import { RunComparison } from "../RunComparison";
import { RunExplanation } from "../RunExplanation";
import { RunPanel } from "../RunPanel";
import { RunnerPanel } from "../RunnerPanel";
import { ScenarioPanel } from "../ScenarioPanel";
import { Sequence } from "../Sequence";
import { SuitePanel } from "../SuitePanel";
import { SyntheticPackets } from "../SyntheticPackets";
import { TaskTabs, TaskPanel } from "../TaskTabs";
import { TeamCollaboration, OfflineRevisionDraft } from "../TeamCollaboration";
import { TestAuthoring } from "../TestAuthoring";
import { RetentionStatus } from "../drafting";
import {
  Page,
  EmptyState,
  NavItem,
  Modal,
  FormDialog,
  MoreMenu,
} from "../layout";
import {
  Status,
  Badge,
  Report,
  Separator,
  Palette,
  IndexSetup,
  MessageGrid,
  newIndexDraft,
} from "../shell";
import {
  Lead,
  Group,
  Fields,
  Field,
  Actions,
  Stats,
  Facts,
  Result,
  Note,
  More,
} from "../ui";

const noop = () => {};
const empty = async () => ({ state: "empty" as const });
const common = {
  workspace: f.WORKSPACE_ROOT,
  project: f.WORKSPACE_ROOT,
  root: f.WORKSPACE_ROOT,
  busy: false,
  progress: null,
  indicators: f.indicatorTable(),
  drafts: [],
  entries: artifacts,
  onClose: noop,
  onRefresh: noop,
  onOpenCase: noop,
  onChanged: noop,
};
const list = ["synthetic-document.json"];
const resultProps = {
  busy: false,
  progress: null,
  indicators: f.indicatorTable(),
  result: null,
};
const content = <p>Synthetic example content for visual inspection.</p>;
const recovered = f.recoveryResult({
  schema: "readmit-desktop-session/v1",
  view: {
    workspace: f.WORKSPACE_ROOT,
    region: "evidence",
    case: f.CASE_ENTRY,
    run: "",
  },
  drafts: [f.retainedDraft()],
});
export interface Example {
  name: string;
  state: string;
  render: () => ReactElement;
  openButtons?: string[];
}
const e = (
  name: string,
  render: () => ReactElement,
  state = "ready",
  openButtons?: string[],
): Example => ({
  name,
  state,
  render,
  ...(openButtons ? { openButtons } : {}),
});
export const examples: Example[] = [
  e(
    "AssertionSetAuthoring",
    () => <AssertionSetAuthoring {...common} inspected={null} />,
    "ready",
    ["Structured", "Advanced JSON"],
  ),
  e("Baseline", () => <Baseline {...common} />),
  e("CanonicalTestEditor", () => <CanonicalTestEditor {...common} />),
  e("CapturePanel", () => <CapturePanel {...common} />),
  e("Comparison", () => (
    <Comparison
      {...resultProps}
      entries={list}
      onCompare={noop}
      result={f.compareResult([f.comparisonRow(1, "paired")])}
    />
  )),
  e("ComputerLicense", () => (
    <ComputerLicense portal={undefined} onChanged={noop} />
  )),
  e("StateHelp", () => <StateHelp state="failed" />, "error"),
  e("HelpTopics", () => <HelpTopics />),
  e("ControlledDetails", () => (
    <ControlledDetails summary="Details">{content}</ControlledDetails>
  )),
  e("CorrelationReview", () => (
    <CorrelationReview
      busy={false}
      reviews={list}
      context={{
        workspace: f.WORKSPACE_ROOT,
        case: f.CASE_ENTRY,
        identity: f.CASE_IDENTITY,
        rules: "rules.json",
        rules_sha256: "synthetic-digest",
      }}
      onReview={empty}
    />
  )),
  e("Diagnosis", () => (
    <Diagnosis
      {...common}
      caseName={f.CASE_ENTRY}
      identity={f.CASE_IDENTITY}
      configEntries={list}
      reportEntries={list}
      groupsReportEntries={list}
      caseEntries={[f.CASE_ENTRY]}
      result={f.diagnosisResult([f.diagnosisFinding("finding-1")])}
      groupsResult={null}
      reviewResult={null}
      onRun={noop}
      onOpen={noop}
      onGroup={noop}
      onOpenGroups={noop}
      onReview={noop}
      onSelect={noop}
      onPromote={noop}
    />
  )),
  e("EnvironmentBanner", () => <EnvironmentBanner />),
  e("EnvironmentPanel", () => <EnvironmentPanel {...common} />, "ready", [
    "Target",
    "Credential References",
    "Send policy",
    "Reset plan",
  ]),
  e("GuidedSample", () => (
    <GuidedSample
      {...common}
      result={f.guideResult("test", 1)}
      practice={null}
      onCreateSample={noop}
      onRun={noop}
      onCancel={noop}
    />
  )),
  e("NewProjectForm", () => (
    <NewProjectForm busy={false} onCreate={empty} onCancel={noop} />
  )),
  e("DemoCallout", () => <DemoCallout busy={false} onTryDemo={noop} />),
  e("HubAdministration", () => <HubAdministration />),
  e("HubPanel", () => <HubPanel {...common} />),
  e("IconButton", () => (
    <IconButton label="Refresh" icon="refresh" onClick={noop} />
  )),
  e("ImportPanel", () => <ImportPanel {...common} />),
  e("Inspector", () => (
    <Inspector {...common} result={f.inspectionResult()} onInspect={noop} />
  )),
  e("MaintenancePanel", () => (
    <MaintenancePanel {...common} onReopen={noop} onProjectChanged={noop} />
  )),
  e("NoteDraft", () => <NoteDraft {...common} restored={recovered} />),
  e("ObservationPanel", () => <ObservationPanel {...common} />),
  e("OperationAccess", () => <OperationAccess />),
  e("OperatorHub", () => <OperatorHub />),
  e("PacketPanel", () => <PacketPanel {...common} />),
  e("PerformanceCorpus", () => <PerformanceCorpus {...common} request={1} />),
  e("PrivacyDisclosure", () => (
    <PrivacyDisclosure
      operations={f.shellResult().shell!.privacy.operations}
      workspaceOpen={true}
      onOpenRunPanel={noop}
      onStartCapture={noop}
      onStartObservation={noop}
    />
  )),
  e("SupportGuidance", () => (
    <SupportGuidance support={f.shellResult().shell!.support} />
  )),
  e("PrivacyDocuments", () => (
    <PrivacyDocuments
      {...common}
      task="policy"
      policyName="policy.json"
      inventoryName="inventory.json"
      onSaved={noop}
    />
  )),
  e("PrivacyPanel", () => <PrivacyPanel {...common} />),
  e("ProfileEditor", () => <ProfileEditor {...common} />),
  e("ProjectPanel", () => (
    <ProjectPanel
      {...common}
      result={f.projectOverviewResult([f.registeredCase()])}
      editable={f.revisionsResult()}
      selectedCase={null}
      onUpdateSettings={async () => true}
      onRegister={noop}
      onUpdateCase={noop}
      onReadEditable={noop}
    />
  )),
  e("ProtectionPanel", () => <ProtectionPanel {...common} />),
  e("RawInspection", () => <RawInspection {...common} request={1} />),
  e("RecentWorkspaces", () => (
    <RecentWorkspaces
      {...common}
      recent={f.recentResult([f.WORKSPACE_ROOT])}
      onReopen={noop}
      onForget={async () => {}}
    />
  )),
  e("Recovery", () => (
    <Recovery restored={recovered} onChanged={noop} onReopen={noop} />
  )),
  e("RetainedDrafts", () => (
    <RetainedDrafts
      drafts={[f.editorDraft("draft-1", "test-draft", f.EMPTY_DRAFT)]}
      onDiscardDraft={noop}
    />
  )),
  e("Reduction", () => (
    <Reduction
      {...resultProps}
      ruleEntries={list}
      specEntries={list}
      targetEntries={list}
      resetEntries={list}
      policyEntries={list}
      reducing={false}
      caseOpen={true}
      onPreview={noop}
      onStart={noop}
      onCancel={noop}
    />
  )),
  e("Reexecution", () => (
    <Reexecution {...common} reviews={list} packets={list} specs={list} />
  )),
  e("ReplayPanel", () => (
    <ReplayPanel
      {...common}
      caseName={f.CASE_ENTRY}
      identity={f.CASE_IDENTITY}
      rows={catalogRows}
    />
  )),
  e("Reproducer", () => (
    <Reproducer
      {...resultProps}
      rows={catalogRows}
      inspected={null}
      onStep={noop}
      onUndo={noop}
      onBuild={noop}
    />
  )),
  e("Review", () => (
    <Review
      {...common}
      ruleEntries={list}
      planEntries={list}
      packEntries={list}
      reviewEntries={list}
      transformResult={null}
      planResult={null}
      reviewResult={null}
      caseOpen={true}
      caseIdentity={f.CASE_IDENTITY}
      transformProgress={null}
      planProgress={null}
      reviewProgress={null}
      onPreview={noop}
      onSavePlan={async () => null}
      onOpenPlan={async () => null}
      onReview={noop}
    />
  )),
  e("RevisionComparison", () => (
    <RevisionComparison {...resultProps} entries={list} onCompare={noop} />
  )),
  e("CorrelationRulesEditor", () => (
    <CorrelationRulesEditor {...common} entries={list} />
  )),
  e("SequenceAnalysisEditor", () => (
    <SequenceAnalysisEditor
      {...common}
      caseIdentity={f.CASE_IDENTITY}
      entries={list}
    />
  )),
  e("NormalizationPolicyEditor", () => (
    <NormalizationPolicyEditor {...common} entries={list} />
  )),
  e("DiagnoseConfigEditor", () => (
    <DiagnoseConfigEditor {...common} entries={list} />
  )),
  e("RunComparison", () => <RunComparison {...common} />),
  e("RunExplanation", () => <RunExplanation {...common} />),
  e("RunPanel", () => <RunPanel {...common} onWatch={async () => {}} />),
  e("RunnerPanel", () => <RunnerPanel />),
  e(
    "ScenarioPanel",
    () => <ScenarioPanel {...common} onStartTestDraft={noop} />,
    "ready",
    ["Design", "Preview", "Generate", "Library", "SIU fixtures", "Raw"],
  ),
  e("Sequence", () => (
    <Sequence
      {...common}
      onReview={empty}
      rulesEntries={list}
      analyses={list}
      reviews={list}
      result={f.sequenceResult([f.sequenceEvent(f.GRID_OCCURRENCE, 1)])}
      onOpen={noop}
      onSelect={noop}
    />
  )),
  e("SuitePanel", () => <SuitePanel {...common} />),
  e("SyntheticPackets", () => <SyntheticPackets onRefresh={noop} />),
  e("TaskTabs", () => (
    <TaskTabs
      label="Example views"
      id="catalog-tabs"
      tabs={[
        { key: "one", label: "First view" },
        { key: "two", label: "Second view" },
      ]}
      selected="one"
      onSelect={noop}
    >
      {content}
    </TaskTabs>
  )),
  e("TaskPanel", () => (
    <TaskPanel tabs="catalog-tabs" tab="one" shown>
      {content}
    </TaskPanel>
  )),
  e("TeamCollaboration", () => <TeamCollaboration {...common} />),
  e("OfflineRevisionDraft", () => (
    <OfflineRevisionDraft
      workspace={f.WORKSPACE_ROOT}
      context={{
        project: "synthetic-project",
        resource: "synthetic-resource",
        tips: ["revision-1"],
        head: 1,
      }}
    />
  )),
  e("TestAuthoring", () => (
    <TestAuthoring
      {...resultProps}
      rows={catalogRows}
      result={f.testResult(f.EMPTY_DRAFT, [])}
      inspected={null}
      onAnswer={noop}
      onSave={noop}
      onSuggest={noop}
      onApprove={noop}
    />
  )),
  e(
    "RetentionStatus",
    () => (
      <RetentionStatus
        retention={{
          state: "not-retained",
          reason: "Synthetic storage failure",
        }}
      />
    ),
    "error",
  ),
  e("Page", () => (
    <Page id="example" shown title="Page title" subtitle="Page subtitle">
      {content}
    </Page>
  )),
  e(
    "EmptyState",
    () => (
      <EmptyState title="No cases yet" action={<button>Add case</button>} />
    ),
    "empty",
  ),
  e("NavItem", () => (
    <ul>
      <NavItem id="cases" label="Cases" current onSelect={noop} />
    </ul>
  )),
  e(
    "Modal",
    () => (
      <Modal open title="Details" onClose={noop}>
        {content}
      </Modal>
    ),
    "open",
  ),
  e(
    "FormDialog",
    () => (
      <FormDialog
        open
        title="Edit case"
        onClose={noop}
        onSubmit={noop}
        submitLabel="Save"
      >
        <Field label="Title">
          <input defaultValue="Synthetic case" />
        </Field>
      </FormDialog>
    ),
    "open",
  ),
  e(
    "MoreMenu",
    () => (
      <MoreMenu
        label="More actions"
        items={[
          { label: "Edit", onSelect: noop },
          { label: "Delete", onSelect: noop, disabled: true },
        ]}
      />
    ),
    "ready",
    ["More actions"],
  ),
  e(
    "Outcome",
    () => (
      <Outcome
        result={{ state: "failed", reason: "Synthetic operation failed." }}
      />
    ),
    "error",
  ),
  e(
    "Status",
    () => (
      <Status
        indicator={f.indicatorTable().get("failed")}
        state="failed"
        reason="Synthetic operation failed."
      />
    ),
    "error",
  ),
  e("Badge", () => (
    <Badge
      indicator={f.indicatorTable().get("completed")}
      fallback="Completed"
    />
  )),
  e(
    "Report",
    () => (
      <Report
        indicators={f.indicatorTable()}
        progress="Loading synthetic evidence…"
        result={null}
      />
    ),
    "busy",
  ),
  e("Separator", () => (
    <div
      style={{
        display: "grid",
        gridTemplateColumns: "1fr 8px 1fr",
        height: 180,
      }}
    >
      <p>Messages</p>
      <Separator
        split={58}
        min={20}
        max={80}
        step={1}
        onSplit={noop}
        bounds={() => ({ left: 0, right: 1000 })}
      />
      <p>Details</p>
    </div>
  )),
  e(
    "Palette",
    () => (
      <Palette
        open
        commands={f.shellResult().shell!.commands}
        query=""
        onQuery={noop}
        onClose={noop}
        onRun={noop}
      />
    ),
    "open",
  ),
  e("IndexSetup", () => (
    <IndexSetup
      mode="build"
      draft={newIndexDraft(f.INDEX_ENTRY)}
      onDraft={noop}
      caseName={f.CASE_ENTRY}
      identity={f.CASE_IDENTITY}
      replaceTarget={null}
      busy={false}
      now={0}
      onBuild={noop}
      onClose={noop}
    />
  )),
  e("MessageGrid", () => (
    <MessageGrid
      {...common}
      result={f.gridResult(catalogRows)}
      filters={f.filtersResult()}
      entries={[f.INDEX_ENTRY]}
      onOpen={noop}
      onSelect={noop}
      onSave={noop}
      selectedOccurrence={null}
      onInspect={noop}
    />
  )),
  e("Lead", () => <Lead>{content}</Lead>),
  e("Group", () => <Group title="Group title">{content}</Group>),
  e("Fields", () => (
    <Fields>
      <Field label="Name">
        <input defaultValue="Synthetic example" />
      </Field>
    </Fields>
  )),
  e("Field", () => (
    <Field label="Name">
      <input defaultValue="Synthetic example" />
    </Field>
  )),
  e("Actions", () => (
    <Actions>
      <button className="primary">Save</button>
      <button disabled>Unavailable</button>
    </Actions>
  )),
  e("Stats", () => (
    <Stats
      items={[
        { label: "Messages", value: 8 },
        { label: "Failures", value: 1, tone: "danger" },
      ]}
    />
  )),
  e("Facts", () => (
    <Facts
      items={[
        ["Source", "Synthetic fixture"],
        ["Messages", 8],
      ]}
    />
  )),
  e("Result", () => <Result title="Result">{content}</Result>),
  e("Note", () => (
    <Note tone="warning">Review the synthetic result before continuing.</Note>
  )),
  e("More", () => <More summary="Additional details">{content}</More>),
  // Deliberate representative variants of shared state and input components.
  ...(
    [
      "empty",
      "busy",
      "cancelled",
      "failed",
      "permission_denied",
      "completed",
    ] as const
  ).map((state) =>
    e(
      "Status",
      () => <Status state={state} indicator={f.indicatorTable().get(state)} />,
      state,
    ),
  ),
  e(
    "RecentWorkspaces",
    () => (
      <RecentWorkspaces
        {...common}
        recent={f.recentResult([])}
        onReopen={noop}
        onForget={async () => {}}
      />
    ),
    "empty",
  ),
  e(
    "RecentWorkspaces",
    () => (
      <RecentWorkspaces
        {...common}
        recent={{
          state: "failed",
          roots: [],
          reason: "Synthetic folder unavailable",
        }}
        onReopen={noop}
        onForget={async () => {}}
      />
    ),
    "error",
  ),
  e(
    "NewProjectForm",
    () => <NewProjectForm busy onCreate={empty} onCancel={noop} />,
    "disabled",
  ),
  e(
    "MessageGrid",
    () => (
      <MessageGrid
        {...common}
        result={f.gridResult([])}
        filters={f.filtersResult()}
        entries={[]}
        onOpen={noop}
        onSelect={noop}
        onSave={noop}
        selectedOccurrence={null}
        onInspect={noop}
      />
    ),
    "empty",
  ),
  e(
    "MessageGrid",
    () => (
      <MessageGrid
        {...common}
        result={{ state: "failed", reason: "Synthetic index unavailable" }}
        filters={f.filtersResult()}
        entries={[]}
        onOpen={noop}
        onSelect={noop}
        onSave={noop}
        selectedOccurrence={null}
        onInspect={noop}
      />
    ),
    "error",
  ),
  e(
    "MessageGrid",
    () => (
      <MessageGrid
        {...common}
        busy
        progress="Reading messages…"
        result={null}
        filters={f.filtersResult()}
        entries={[]}
        onOpen={noop}
        onSelect={noop}
        onSave={noop}
        selectedOccurrence={null}
        onInspect={noop}
      />
    ),
    "busy",
  ),
];

// Prop-driven states are rendered with each real component's existing contract.
// No CSS is used to fake disabled controls, loading indicators or error copy.
for (const example of [...examples]) {
  if (example.state !== "ready") continue;
  const element = example.render() as ReactElement<Record<string, unknown>>;
  const variants: [string, Record<string, unknown>][] = [];
  if (Object.hasOwn(element.props, "busy"))
    variants.push(["disabled", { busy: true }]);
  if (Object.hasOwn(element.props, "result")) {
    variants.push(
      ["empty", { result: { state: "empty" } }],
      [
        "error",
        {
          result: {
            state: "failed",
            reason: "Synthetic evidence unavailable for this example.",
          },
        },
      ],
    );
  }
  for (const [state, props] of variants) {
    if (examples.some((e) => e.name === example.name && e.state === state))
      continue;
    examples.push(e(example.name, () => cloneElement(element, props), state));
  }
}

export function ExampleFrame({ example }: { example: Example }) {
  return (
    <VocabularyContext.Provider value={f.vocabularyFixture()}>
      <IndicatorsContext.Provider value={f.indicatorTable()}>
        <div className="page" data-page="catalog">
          <header className="page-header">
            <div className="page-heading">
              <h1>{example.name}</h1>
              <p className="page-subtitle">
                {example.state} · synthetic example
              </p>
            </div>
          </header>
          <div className="page-body">{example.render()}</div>
        </div>
      </IndicatorsContext.Provider>
    </VocabularyContext.Provider>
  );
}
