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
  await user.click(screen.getByRole("button", { name: "Name this assertion set" }));
  expect(facade.callsTo("AuthorAssertionSet").length).toBeGreaterThan(0);

  await user.type(screen.getByLabelText("Assertion id"), "ack-accepted");
  await user.selectOptions(screen.getByLabelText("Operator"), "field_equals");
  await user.click(screen.getByRole("button", { name: /Use the inspected position MSA-1/i }));
  await user.type(screen.getByLabelText("Expected text"), "AA");
  await user.click(screen.getByRole("button", { name: "Add this assertion" }));

  const withAssertions = facade
    .callsTo("AuthorAssertionSet")
    .map((call) => call.args[0] as { assertions?: { id: string; operator: string }[] })
    .filter((request) => request.assertions && request.assertions.length > 0);
  expect(withAssertions.length).toBeGreaterThan(0);
  expect(withAssertions.at(-1)?.assertions?.[0]?.id).toBe("ack-accepted");
  expect(withAssertions.at(-1)?.assertions?.[0]?.operator).toBe("field_equals");

  await user.type(screen.getByLabelText("New assertion set entry in this workspace"), "expectations.json");
  await user.click(screen.getByRole("button", { name: "Write the assertion set" }));
  await waitFor(() => expect(facade.callsTo("SaveAssertionSet").length).toBe(1));
  expect(await screen.findByText(/saved-assertion-identity/)).toBeTruthy();

  await user.click(screen.getByRole("button", { name: "Advanced JSON" }));
  fireEvent.change(screen.getByLabelText("Complete assertion set"), {
    target: { value: '{"schema":"readmit-assertion-set/v1"}' },
  });
  await user.click(screen.getByRole("button", { name: "Validate with the assertion reader" }));
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

  await user.click(screen.getByRole("button", { name: /Use the inspected position PID-5.1/i }));
  expect(facade.callsTo("AuthorAssertionSet").length).toBe(0);
  expect((screen.getByLabelText("Selector") as HTMLInputElement).value).toBe("PID-5.1");

  uninstallFacade();
});
