import { expect, test } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { AssertionSetAuthoring } from "./AssertionSetAuthoring";
import { installFacade, uninstallFacade } from "./testkit/wails";
import { WORKSPACE_ROOT } from "./testkit/fixtures";
import type {
  AssertionSetDraftDocument,
  AssertionSetResult,
  CanonicalAssertionResult,
} from "./bindings";

const emptyDraft = (): AssertionSetDraftDocument => ({
  schema: "readmit-assertion-set-draft/v1",
  name: "",
  assertions: [],
});

const stateClause = (id: string): AssertionSetDraftDocument["assertions"][number] => ({
  id,
  operator: "field_state",
  subject: { field: { scope: "observed", message: "s0001-e000001", selector: "MSA-1" } },
  when: null,
  expected: { state: "present" },
});

function setResult(
  draft: AssertionSetDraftDocument,
  extra: Partial<NonNullable<AssertionSetResult["set"]>> = {},
): AssertionSetResult {
  return {
    state: draft.name || draft.assertions.length ? "completed" : "empty",
    set: { draft, ...extra },
  };
}

test("AssertionSetAuthoring authors, saves, and validates through the facade", async () => {
  const user = userEvent.setup();
  let draft = emptyDraft();
  const facade = installFacade({
    AuthorAssertionSet: async (request) => {
      if (request.name) {
        draft = { ...draft, ...request.draft, name: request.name };
      }
      if (request.assertions) {
        draft = { ...draft, ...request.draft, assertions: request.assertions };
      }
      if (!request.name && !request.assertions) {
        draft = request.draft.schema ? request.draft : emptyDraft();
      }
      return setResult(draft);
    },
    SaveAssertionSet: async (request) =>
      setResult(request.draft, {
        output: request.output ?? "expectations.json",
        identity: "saved-assertion-identity",
      }),
    ImportAssertionSet: async () =>
      setResult({
        schema: "readmit-assertion-set-draft/v1",
        name: "Imported set",
        assertions: [
          {
            id: "ack-accepted",
            operator: "field_equals",
            subject: {
              field: { scope: "observed", message: "s0001-e000001", selector: "MSA-1" },
            },
            when: null,
            expected: { field: { state: "present", text: "AA" } },
          },
        ],
      }),
    ValidateAssertionSet: async (document): Promise<CanonicalAssertionResult> => ({
      state: "completed",
      document,
    }),
    ExportAssertionSet: async (request): Promise<CanonicalAssertionResult> => ({
      state: "completed",
      document: request.document,
      output: request.output,
      identity: "exported-assertion-identity",
    }),
    SaveEditorDraft: async () => ({ state: "completed", drafts: [] }),
    DiscardEditorDraft: async () => ({ state: "completed", drafts: [] }),
  });

  render(
    <AssertionSetAuthoring
      workspace={WORKSPACE_ROOT}
      drafts={[]}
      busy={false}
      inspected={{ occurrence: "s0001-e000001", path: "MSA-1" }}
    />,
  );

  expect(await screen.findByRole("heading", { name: "Assertion set" })).toBeTruthy();

  await user.type(screen.getByLabelText("Assertion set name"), "Synthetic expectations");
  await user.click(screen.getByRole("button", { name: "Save name" }));
  expect(facade.callsTo("AuthorAssertionSet").length).toBeGreaterThan(0);

  await user.type(screen.getByLabelText("Assertion id"), "ack-accepted");
  await user.selectOptions(screen.getByLabelText("Operator"), "field_equals");
  await user.click(screen.getByRole("button", { name: /Use selected field MSA-1/i }));
  await user.type(screen.getByLabelText("Expected text"), "AA");
  await user.click(screen.getByRole("button", { name: "Add assertion" }));

  const withAssertions = facade
    .callsTo("AuthorAssertionSet")
    .map((call) => call.args[0] as { assertions?: { id: string; operator: string }[] })
    .filter((request) => request.assertions && request.assertions.length > 0);
  expect(withAssertions.length).toBeGreaterThan(0);
  expect(withAssertions.at(-1)?.assertions?.[0]?.id).toBe("ack-accepted");
  expect(withAssertions.at(-1)?.assertions?.[0]?.operator).toBe("field_equals");

  await user.type(screen.getByLabelText("New assertion set entry in this workspace"), "expectations.json");
  await user.click(screen.getByRole("button", { name: "Save assertion set" }));
  await waitFor(() => expect(facade.callsTo("SaveAssertionSet").length).toBe(1));
  expect(await screen.findByText(/saved-assertion-identity/)).toBeTruthy();

  await user.click(screen.getByRole("button", { name: "Advanced JSON" }));
  fireEvent.change(screen.getByLabelText("Complete assertion set"), {
    target: { value: '{"schema":"readmit-assertion-set/v1"}' },
  });
  await user.click(screen.getByRole("button", { name: "Validate" }));
  expect(await screen.findByText(/Accepted by the shared assertion reader/)).toBeTruthy();

  uninstallFacade();
});

test("selecting an inspected field does not auto-add an assertion", async () => {
  const user = userEvent.setup();
  const facade = installFacade({
    AuthorAssertionSet: async (request) => setResult(request.draft.schema ? request.draft : emptyDraft()),
    SaveEditorDraft: async () => ({ state: "completed", drafts: [] }),
    DiscardEditorDraft: async () => ({ state: "completed", drafts: [] }),
  });

  render(
    <AssertionSetAuthoring
      workspace={WORKSPACE_ROOT}
      drafts={[]}
      busy={false}
      inspected={{ occurrence: "s0001-e000002", path: "PID-5.1" }}
    />,
  );

  await user.click(screen.getByRole("button", { name: /Use selected field PID-5.1/i }));
  expect(facade.callsTo("AuthorAssertionSet").length).toBe(0);
  expect((screen.getByLabelText("Selector") as HTMLInputElement).value).toBe("PID-5.1");

  uninstallFacade();
});

test("import asks before replacing unsaved clauses, Escape and refusal keep them, and a saved draft imports directly", async () => {
  const user = userEvent.setup();
  const imported: AssertionSetDraftDocument = {
    schema: "readmit-assertion-set-draft/v1",
    name: "Received expectations",
    assertions: [stateClause("first"), stateClause("second")],
  };
  const refused = "the operator record_count reads a collection subject";
  const facade = installFacade({
    AuthorAssertionSet: async (request) =>
      setResult({ ...request.draft, assertions: request.assertions ?? request.draft.assertions }),
    ImportAssertionSet: async (_workspace, entry) =>
      entry === "refused.json" ? { state: "failed", reason: refused } : setResult(imported),
    SaveAssertionSet: async (request) => setResult(request.draft, { output: request.output ?? "saved.json", identity: "saved" }),
    SaveEditorDraft: async () => ({ state: "completed", drafts: [] }),
    DiscardEditorDraft: async () => ({ state: "completed", drafts: [] }),
  });
  render(<AssertionSetAuthoring workspace={WORKSPACE_ROOT} drafts={[]} busy={false} inspected={null} />);
  const entry = screen.getByLabelText("Assertion set entry");
  const clauses = () => screen.queryAllByRole("button", { name: /^Remove / }).map((button) => button.textContent);

  await user.type(entry, "received.json{Enter}");
  await waitFor(() => expect(clauses()).toEqual(["Remove first", "Remove second"]));
  expect(facade.callsTo("ImportAssertionSet")).toHaveLength(1);

  await user.click(screen.getByRole("button", { name: "Remove first" }));
  await waitFor(() => expect(clauses()).toEqual(["Remove second"]));
  await user.clear(entry);
  await user.type(entry, "another.json{Enter}");
  expect(screen.getByRole("group", { name: "Import another.json in place of these assertions?" })).toBeTruthy();
  expect(facade.callsTo("ImportAssertionSet")).toHaveLength(1);
  await user.keyboard("{Escape}");
  expect(clauses()).toEqual(["Remove second"]);
  expect(facade.callsTo("ImportAssertionSet")).toHaveLength(1);

  await user.click(screen.getByRole("button", { name: "Import" }));
  await user.click(screen.getByRole("button", { name: "Keep assertions" }));
  expect(clauses()).toEqual(["Remove second"]);
  expect(facade.callsTo("ImportAssertionSet")).toHaveLength(1);

  await user.clear(entry);
  await user.type(entry, "refused.json{Enter}");
  await user.click(screen.getByRole("button", { name: "Replace assertions" }));
  expect(await screen.findByText(refused)).toBeTruthy();
  expect(clauses()).toEqual(["Remove second"]);

  await user.type(screen.getByLabelText("New assertion set entry in this workspace"), "saved.json");
  await user.click(screen.getByRole("button", { name: "Save assertion set" }));
  expect(await screen.findByText(/Written to saved.json/)).toBeTruthy();
  await user.clear(entry);
  await user.type(entry, "another.json{Enter}");
  await waitFor(() => expect(clauses()).toEqual(["Remove first", "Remove second"]));
  expect(screen.queryByRole("group", { name: /in place of these assertions/ })).toBeNull();
  expect(facade.callsTo("ImportAssertionSet").map((call) => call.args[1])).toEqual([
    "received.json", "refused.json", "another.json",
  ]);

  await user.click(screen.getByRole("button", { name: "Remove first" }));
  await user.click(screen.getByRole("button", { name: "Remove second" }));
  await waitFor(() => expect(clauses()).toEqual([]));
  await user.click(screen.getByRole("button", { name: "Import" }));
  await waitFor(() => expect(clauses()).toEqual(["Remove first", "Remove second"]));
  expect(screen.queryByRole("group", { name: /in place of these assertions/ })).toBeNull();
  expect(facade.callsTo("ImportAssertionSet")).toHaveLength(4);
  uninstallFacade();
});

test("a retained clause is protected from import after reopening its draft", async () => {
  const user = userEvent.setup();
  const held: AssertionSetDraftDocument = {
    schema: "readmit-assertion-set-draft/v1",
    name: "Still editing",
    assertions: [stateClause("keep-this")],
  };
  const facade = installFacade({
    ImportAssertionSet: async () => setResult(emptyDraft()),
    SaveEditorDraft: async () => ({ state: "completed", drafts: [] }),
    DiscardEditorDraft: async () => ({ state: "completed", drafts: [] }),
  });
  render(<AssertionSetAuthoring workspace={WORKSPACE_ROOT} drafts={[{
    id: "draft-1",
    kind: "assertion-set-draft",
    workspace: WORKSPACE_ROOT,
    case: "",
    identity: "",
    content_schema: "readmit-assertion-set-draft/v1",
    content: held,
  }]} busy={false} inspected={null} />);

  expect(await screen.findByRole("button", { name: "Remove keep-this" })).toBeTruthy();
  await user.type(screen.getByLabelText("Assertion set entry"), "another.json{Enter}");
  expect(screen.getByRole("group", { name: "Import another.json in place of these assertions?" })).toBeTruthy();
  expect(facade.callsTo("ImportAssertionSet")).toHaveLength(0);
  await user.click(screen.getByRole("button", { name: "Keep assertions" }));
  expect(screen.getByRole("button", { name: "Remove keep-this" })).toBeTruthy();
  uninstallFacade();
});

// An import the reader refuses changes nothing the structured draft holds and
// says why; an export over an entry that already exists is refused, claims no
// identity, and leaves the reviewed text to be exported to a new name. Both
// forms answer Enter.
test("a refused import leaves the draft and an occupied export destination is refused, from the keyboard", async () => {
  const user = userEvent.setup();
  const refusedByReader = "the operator record_count reads a collection subject";
  const occupied = "that name is already an entry of this workspace; an assertion set is written to a new entry";
  const imported: AssertionSetDraftDocument = {
    schema: "readmit-assertion-set-draft/v1",
    name: "Received expectations",
    assertions: [
      {
        id: "booking-accepted",
        operator: "field_equals",
        subject: { field: { scope: "observed", message: "s0001-e000001", selector: "MSA-1" } },
        when: null,
        expected: { field: { state: "present", text: "AA" } },
      },
    ],
  };
  const facade = installFacade({
    ImportAssertionSet: async (_workspace, entry) =>
      entry === "refused-assertions.json" ? { state: "failed", reason: refusedByReader } : setResult(imported),
    ExportAssertionSet: async (request): Promise<CanonicalAssertionResult> =>
      request.output === "received-assertions.json"
        ? { state: "failed", reason: occupied }
        : { state: "completed", document: request.document, output: request.output, identity: "exported-assertion-identity" },
    SaveEditorDraft: async () => ({ state: "completed", drafts: [] }),
    DiscardEditorDraft: async () => ({ state: "completed", drafts: [] }),
  });
  render(<AssertionSetAuthoring workspace={WORKSPACE_ROOT} drafts={[]} busy={false} inspected={null} />);

  await user.type(screen.getByLabelText("Assertion set entry"), "refused-assertions.json{Enter}");
  expect(await screen.findByText(refusedByReader)).toBeTruthy();
  expect((screen.getByLabelText("Assertion set name") as HTMLInputElement).value).toBe("");
  expect(screen.queryByRole("button", { name: /^Remove / })).toBeNull();

  await user.clear(screen.getByLabelText("Assertion set entry"));
  await user.type(screen.getByLabelText("Assertion set entry"), "received-assertions.json{Enter}");
  expect(await screen.findByRole("button", { name: "Remove booking-accepted" })).toBeTruthy();
  expect((screen.getByLabelText("Assertion set name") as HTMLInputElement).value).toBe("Received expectations");
  expect(screen.queryByText(refusedByReader)).toBeNull();
  expect(facade.callsTo("ImportAssertionSet").map((call) => call.args)).toEqual([
    [WORKSPACE_ROOT, "refused-assertions.json"],
    [WORKSPACE_ROOT, "received-assertions.json"],
  ]);

  await user.click(screen.getByRole("button", { name: "Advanced JSON" }));
  await user.click(screen.getByLabelText("Complete assertion set"));
  await user.paste('{"schema":"readmit-assertion-set/v1"}');
  await user.type(screen.getByLabelText("New assertion set entry"), "received-assertions.json");
  await user.click(screen.getByRole("button", { name: "Export assertion set" }));
  expect(await screen.findByText(occupied)).toBeTruthy();
  expect(screen.queryByText(/^Written to /)).toBeNull();
  expect((screen.getByLabelText("Complete assertion set") as HTMLTextAreaElement).value).toBe('{"schema":"readmit-assertion-set/v1"}');

  await user.clear(screen.getByLabelText("New assertion set entry"));
  await user.type(screen.getByLabelText("New assertion set entry"), "reviewed-assertions.json");
  await user.tab();
  expect(document.activeElement).toBe(screen.getByRole("button", { name: "Export assertion set" }));
  await user.keyboard("{Enter}");
  expect(await screen.findByText(/^Written to reviewed-assertions\.json · identity/)).toBeTruthy();
  expect(screen.getByText("exported-assertion-identity")).toBeTruthy();
  expect(screen.queryByText(occupied)).toBeNull();

  uninstallFacade();
});
