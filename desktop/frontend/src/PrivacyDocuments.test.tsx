import { expect, test } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { PrivacyDocuments } from "./PrivacyDocuments";
import { facadeStub, installFacade } from "./testkit/wails";
import type { EditorDraft, RedactInventory, RedactPolicy } from "./bindings";

const workspace = "/workspace";
const existingPolicy: RedactPolicy = {
  schema: "readmit-redact-policy/v1",
  patient: { selector: "PID-3.1", authority: [] },
  fields: [{ selector: "PID-5", policy: "remove-field/v1", class: "names" }],
  remove_segments: ["NTE"], packet_policies: ["regenerate-filenames/v1"],
  spec_bindings: [], required_failures: [1],
};
const existingInventory: RedactInventory = {
  schema: "readmit-redact-inventory/v1", complete: true,
  artifacts: [{ kind: "run", path: "original-run" }], residual_values: [],
};

test("disclosure documents reopen into structured controls and save new validated entries", async () => {
  const user = userEvent.setup();
  const saved: string[] = [];
  installFacade({
    ReadRedactPolicy: () => ({ state: "completed", entry: "policy.json", policy: existingPolicy }),
    ReadRedactInventory: () => ({ state: "completed", entry: "inventory.json", inventory: existingInventory }),
    SaveRedactPolicy: ({ policy, output }) => {
      expect(policy.patient.selector).toBe("PID-3.1");
      expect(policy.fields).toEqual([{ selector: "PID-5", policy: "remove-field/v1", class: "names" }]);
      expect(policy.required_failures).toEqual([1]);
      expect(output).toBe("edited-policy.json");
      return { state: "completed", entry: output, policy };
    },
    SaveRedactInventory: ({ inventory, output }) => {
      expect(inventory.complete).toBe(true);
      expect(inventory.artifacts).toEqual([{ kind: "run", path: "original-run" }]);
      expect(output).toBe("edited-inventory.json");
      return { state: "completed", entry: output, inventory };
    },
  });
  const onSaved = (kind: string, entry: string) => { saved.push(`${kind}:${entry}`); };
  const view = render(<PrivacyDocuments task="policy" workspace={workspace} policyName="policy.json" inventoryName="inventory.json"
    onSaved={onSaved} />);
  expect((screen.getByLabelText("Patient ID selector") as HTMLInputElement).value).toBe("");
  expect(screen.queryByLabelText("Known value 1")).toBeNull();
  await user.click(screen.getByRole("button", { name: "Edit policy" }));
  expect((screen.getByLabelText("Patient ID selector") as HTMLInputElement).value).toBe("PID-3.1");
  await user.type(screen.getByLabelText("Policy file"), "edited-policy.json");
  await user.click(screen.getByRole("button", { name: "Save policy" }));
  expect(await screen.findByText("Saved disclosure policy edited-policy.json.")).toBeTruthy();

  // The inventory is its own task: the policy's controls leave the screen.
  view.rerender(<PrivacyDocuments task="inventory" workspace={workspace} policyName="policy.json" inventoryName="inventory.json"
    onSaved={onSaved} />);
  expect(screen.queryByRole("button", { name: "Save policy" })).toBeNull();
  await user.click(screen.getByRole("button", { name: "Edit inventory" }));
  expect((screen.getByLabelText("Artifact path 1") as HTMLInputElement).value).toBe("original-run");
  await user.type(screen.getByLabelText("New original-artifact inventory document"), "edited-inventory.json");
  await user.click(screen.getByRole("button", { name: "Save inventory" }));
  expect(await screen.findByText("Saved original-artifact inventory edited-inventory.json.")).toBeTruthy();
  expect(saved).toEqual(["policy:edited-policy.json", "inventory:edited-inventory.json"]);
  expect(facadeStub().callsTo("SaveRedactPolicy")).toHaveLength(1);
  expect(facadeStub().callsTo("SaveRedactInventory")).toHaveLength(1);
});

test("new disclosure documents start without values and show reader refusals", async () => {
  const user = userEvent.setup();
  installFacade({
    SaveRedactPolicy: () => ({ state: "failed", reason: "invalid redaction policy: required_failures declares no assertion position" }),
    SaveRedactInventory: () => ({ state: "failed", reason: "inventory requires explicit complete scope and supported bounded entries" }),
  });
  const refuse = () => { throw new Error("invalid document was saved"); };
  const view = render(<PrivacyDocuments task="policy" workspace={workspace} policyName="" inventoryName="" onSaved={refuse} />);
  expect((screen.getByLabelText("Patient ID selector") as HTMLInputElement).value).toBe("");
  expect(screen.queryByLabelText("Field selector 1")).toBeNull();
  expect(screen.queryByLabelText("Known value 1")).toBeNull();
  await user.type(screen.getByLabelText("Policy file"), "policy.json");
  await user.click(screen.getByRole("button", { name: "Save policy" }));
  expect(await screen.findByText("invalid redaction policy: required_failures declares no assertion position")).toBeTruthy();
  view.rerender(<PrivacyDocuments task="inventory" workspace={workspace} policyName="" inventoryName="" onSaved={refuse} />);
  await user.type(screen.getByLabelText("New original-artifact inventory document"), "inventory.json");
  await user.click(screen.getByRole("button", { name: "Save inventory" }));
  expect(await screen.findByText("inventory requires explicit complete scope and supported bounded entries")).toBeTruthy();
});

test("an unfinished disclosure edit restores from the customer-local draft boundary", async () => {
  const draft: EditorDraft = {
    id: "draft-1", kind: "redact-policy", workspace, case: "", identity: "",
    content_schema: "readmit-redact-policy-draft/v1",
    content: { schema: "readmit-redact-policy-draft/v1", output: "pending-policy.json", policy: existingPolicy },
  };
  installFacade({
    SaveEditorDraft: (candidate) => ({ state: "completed", drafts: [{ ...candidate, id: "draft-1" }] }),
  });
  render(<PrivacyDocuments task="policy" workspace={workspace} policyName="" inventoryName="" drafts={[draft]} onSaved={() => {}} />);
  await waitFor(() => expect((screen.getByLabelText("Patient ID selector") as HTMLInputElement).value).toBe("PID-3.1"));
  expect((screen.getByLabelText("Policy file") as HTMLInputElement).value).toBe("pending-policy.json");
  expect((screen.getByRole("button", { name: "Edit policy" }) as HTMLButtonElement).disabled).toBe(true);
  expect(screen.getByText("Restored the retained disclosure policy draft.")).toBeTruthy();
  expect(facadeStub().callsTo("SaveEditorDraft")).toHaveLength(0);
});

test("an unsupported retained disclosure draft is reported without replacing the empty editor", async () => {
  const draft: EditorDraft = {
    id: "draft-unknown", kind: "redact-policy", workspace, case: "", identity: "",
    content_schema: "readmit-redact-policy-draft/v1",
    content: { schema: "readmit-redact-policy-draft/v1", policy: { schema: "readmit-redact-policy/v1" } },
  };
  render(<PrivacyDocuments task="policy" workspace={workspace} policyName="" inventoryName="" drafts={[draft]} onSaved={() => {}} />);
  expect(await screen.findByText(/retained disclosure policy draft is unsupported/)).toBeTruthy();
  expect((screen.getByLabelText("Patient ID selector") as HTMLInputElement).value).toBe("");
});

test("a refused open keeps the valid disclosure document already on screen", async () => {
  const user = userEvent.setup();
  installFacade({
    ReadRedactPolicy: () => ({ state: "completed", policy: existingPolicy }),
    ReadRedactInventory: () => ({ state: "completed", inventory: existingInventory }),
  });
  const documents = (task: "policy" | "inventory") =>
    <PrivacyDocuments task={task} workspace={workspace} policyName="policy.json" inventoryName="inventory.json" onSaved={() => {}} />;
  const view = render(documents("policy"));
  await user.click(screen.getByRole("button", { name: "Edit policy" }));
  view.rerender(documents("inventory"));
  await user.click(screen.getByRole("button", { name: "Edit inventory" }));
  expect((screen.getByLabelText("Patient ID selector") as HTMLInputElement).value).toBe("PID-3.1");
  expect((screen.getByLabelText("Artifact path 1") as HTMLInputElement).value).toBe("original-run");
  facadeStub().reply({
    ReadRedactPolicy: () => ({ state: "failed", reason: "unsupported disclosure policy contract" }),
    ReadRedactInventory: () => ({ state: "failed", reason: "inventory requires explicit complete scope" }),
  });
  view.rerender(documents("policy"));
  await user.click(screen.getByRole("button", { name: "Edit policy" }));
  expect(await screen.findByText(/The selected policy was not opened: unsupported disclosure policy contract/)).toBeTruthy();
  view.rerender(documents("inventory"));
  await user.click(screen.getByRole("button", { name: "Edit inventory" }));
  expect(await screen.findByText(/The selected policy was not opened: unsupported disclosure policy contract/)).toBeTruthy();
  expect(await screen.findByText(/The selected inventory was not opened: inventory requires explicit complete scope/)).toBeTruthy();
  expect((screen.getByLabelText("Patient ID selector") as HTMLInputElement).value).toBe("PID-3.1");
  expect((screen.getByLabelText("Artifact path 1") as HTMLInputElement).value).toBe("original-run");
});

const editors = [
  { kind: "policy", task: "policy", output: "Policy file", discard: "Discard draft", refusal: "The disclosure policy draft could not be discarded", name: "pending-policy.json" },
  { kind: "inventory", task: "inventory", output: "New original-artifact inventory document", discard: "Discard draft", refusal: "The original-artifact inventory draft could not be discarded", name: "pending-inventory.json" },
] as const;

test.each(editors)("a $kind edit queues retention before immediate navigation", async ({ task, output, name }) => {
  const facade = installFacade({});
  const retaining = facade.park("SaveEditorDraft");
  const mounted = render(<PrivacyDocuments task={task} workspace={workspace} policyName="" inventoryName="" onSaved={() => {}} />);
  fireEvent.change(screen.getByLabelText(output), { target: { value: name } });
  await waitFor(() => expect(retaining.size).toBe(1));
  const queued = facade.oneCall("SaveEditorDraft")[0] as EditorDraft;
  expect((queued.content as { output: string }).output).toBe(name);
  expect(screen.queryByText("Retained. It will come back if this window stops.")).toBeNull();
  mounted.unmount();
  expect(facade.callsTo("SaveEditorDraft")).toHaveLength(1);
  retaining.resolve({ state: "completed", drafts: [{ ...queued, id: "retained-1" }] });
});

test.each(editors)("a refused $kind draft discard preserves text and retries the same identity", async ({ task, output, discard, refusal, name }) => {
  const user = userEvent.setup();
  const discarded: string[] = [];
  const facade = installFacade({
    DiscardEditorDraft: (id) => {
      discarded.push(id);
      return discarded.length === 1
        ? { state: "failed", reason: "the draft store could not discard it" }
        : { state: "completed", drafts: [] };
    },
  });
  const retaining = facade.park("SaveEditorDraft");
  render(<PrivacyDocuments task={task} workspace={workspace} policyName="" inventoryName="" onSaved={() => {}} />);
  fireEvent.change(screen.getByLabelText(output), { target: { value: name } });
  await waitFor(() => expect(retaining.size).toBe(1));
  await user.click(screen.getByRole("button", { name: discard }));
  expect((screen.getByLabelText(output) as HTMLInputElement).value).toBe(name);
  const first = facade.oneCall("SaveEditorDraft")[0] as EditorDraft;
  retaining.resolve({ state: "completed", drafts: [{ ...first, id: "retained-1" }] });
  expect(await screen.findByText(new RegExp(refusal))).toBeTruthy();
  expect((screen.getByLabelText(output) as HTMLInputElement).value).toBe(name);
  expect(discarded).toEqual(["retained-1"]);
  await new Promise((resolve) => setTimeout(resolve, 300));
  expect(facade.callsTo("SaveEditorDraft")).toHaveLength(1);
  await user.click(screen.getByRole("button", { name: discard }));
  await waitFor(() => expect((screen.getByLabelText(output) as HTMLInputElement).value).toBe(""));
  expect(discarded).toEqual(["retained-1", "retained-1"]);
});

test.each(editors)("a saved $kind document reports refused draft cleanup", async ({ kind, task, output, discard, name }) => {
  const user = userEvent.setup();
  const saved: string[] = [];
  installFacade({
    SaveEditorDraft: (draft) => ({ state: "completed", drafts: [{ ...draft, id: "retained-1" }] }),
    DiscardEditorDraft: () => ({ state: "failed", reason: "the draft store could not discard it" }),
    SaveRedactPolicy: ({ output: entry, policy }) => ({ state: "completed", entry, policy }),
    SaveRedactInventory: ({ output: entry, inventory }) => ({ state: "completed", entry, inventory }),
  });
  render(<PrivacyDocuments task={task} workspace={workspace} policyName="" inventoryName="" onSaved={(savedKind, entry) => saved.push(`${savedKind}:${entry}`)} />);
  fireEvent.change(screen.getByLabelText(output), { target: { value: name } });
  expect(await screen.findByText("Retained. It will come back if this window stops.")).toBeTruthy();
  await user.click(screen.getByRole("button", { name: kind === "policy" ? "Save policy" : "Save inventory" }));
  expect(await screen.findByText(new RegExp(`Saved .*${name.replace(".", "\\.")}, but its working draft could not be discarded`))).toBeTruthy();
  expect((screen.getByLabelText(output) as HTMLInputElement).value).toBe(name);
  expect(screen.getByRole("button", { name: discard })).toBeTruthy();
  expect(saved).toEqual([`${kind}:${name}`]);
});

test("category, treatment and artifact captions are shown while the saved documents hold the exact versioned and hyphenated tokens", async () => {
  const user = userEvent.setup();
  installFacade({
    SaveEditorDraft: (draft) => ({ state: "completed", drafts: [{ ...draft, id: "retained-1" }] }),
    DiscardEditorDraft: () => ({ state: "completed", drafts: [] }),
    SaveRedactPolicy: ({ output, policy }) => ({ state: "completed", entry: output, policy }),
    SaveRedactInventory: ({ output, inventory }) => ({ state: "completed", entry: output, inventory }),
  });
  const documents = (task: "policy" | "inventory") =>
    <PrivacyDocuments task={task} workspace={workspace} policyName="" inventoryName="" onSaved={() => {}} />;
  const view = render(documents("policy"));
  await user.click(screen.getByRole("button", { name: "Add field rule" }));

  // A row keeps its number in every accessible name while its visible label is short.
  const category = screen.getByLabelText("Data category 1") as HTMLSelectElement;
  expect(screen.getByText("Data category", { selector: "label" }).getAttribute("for")).toBe(category.id);
  expect(Array.from(category.options).map((option) => [option.value, option.textContent])).toContainEqual(["social-security-numbers", "Social Security numbers"]);
  expect(Array.from(category.options).map((option) => option.textContent)).toEqual([
    "Structural", "Names", "Geography", "Dates and ages", "Telephone numbers", "Fax numbers", "Email addresses",
    "Social Security numbers", "Medical record numbers", "Health plan numbers", "Account numbers",
    "Certificate and license numbers", "Vehicle identifiers", "Device identifiers", "URLs", "IP addresses",
    "Biometric identifiers", "Face images", "Other unique identifiers",
  ]);
  const treatment = screen.getByLabelText("Field treatment 1") as HTMLSelectElement;
  expect(Array.from(treatment.options).map((option) => option.textContent)).toEqual([
    "Scoped surrogate", "Patient date shift", "Remove field", "Replace field", "Retain allowed literals",
  ]);

  await user.type(screen.getByLabelText("Field selector 1"), "PID-5");
  await user.selectOptions(category, "Social Security numbers");
  await user.selectOptions(treatment, "Scoped surrogate");
  await user.type(screen.getByLabelText("Surrogate scope 1"), "patient");
  await user.click(screen.getByRole("checkbox", { name: "Rewrite test literals" }));
  expect(screen.getByText("Field rule 1: PID-5 · Scoped surrogate", { selector: "summary" })).toBeTruthy();
  expect(screen.getByText("scoped-surrogate/v1", { selector: "code" })).toBeTruthy();
  expect(screen.getByText("social-security-numbers", { selector: "code" })).toBeTruthy();
  expect(screen.getByText("rewrite-spec-literals/v1", { selector: "code" })).toBeTruthy();
  await user.type(screen.getByLabelText("Policy file"), "captioned-policy.json");
  await user.click(screen.getByRole("button", { name: "Save policy" }));
  await screen.findByText("Saved disclosure policy captioned-policy.json.");
  const [policyRequest] = facadeStub().oneCall("SaveRedactPolicy");
  expect(policyRequest.policy.fields).toEqual([{ selector: "PID-5", policy: "scoped-surrogate/v1", class: "social-security-numbers", scope: "patient" }]);
  expect(policyRequest.policy.packet_policies).toEqual(["rewrite-spec-literals/v1"]);

  view.rerender(documents("inventory"));
  await user.click(screen.getByRole("button", { name: "Add original artifact" }));
  const kind = screen.getByLabelText("Artifact type 1") as HTMLSelectElement;
  expect(Array.from(kind.options).map((option) => option.textContent)).toEqual(["Run", "Result", "Diagnosis JSON", "Diagnosis Markdown"]);
  await user.selectOptions(kind, "Diagnosis JSON");
  await user.type(screen.getByLabelText("Artifact path 1"), "diagnosis.json");
  await user.type(screen.getByLabelText("New original-artifact inventory document"), "captioned-inventory.json");
  await user.click(screen.getByRole("button", { name: "Save inventory" }));
  await screen.findByText("Saved original-artifact inventory captioned-inventory.json.");
  expect(facadeStub().oneCall("SaveRedactInventory")[0].inventory.artifacts).toEqual([{ kind: "diagnosis-json", path: "diagnosis.json" }]);
});

test("field rules read from a document are collapsed rows named by selector and treatment, and each row's controls keep their row in their names", async () => {
  const user = userEvent.setup();
  installFacade({
    ReadRedactPolicy: () => ({
      state: "completed",
      policy: {
        ...existingPolicy,
        patient: { selector: "PID-3.1", authority: ["PID-3.4"] },
        fields: [
          { selector: "PID-5", policy: "remove-field/v1", class: "names" },
          { selector: "PID-7", policy: "patient-date-shift/v1", class: "dates-and-ages" },
        ],
      },
    }),
  });
  render(<PrivacyDocuments task="policy" workspace={workspace} policyName="policy.json" inventoryName="" onSaved={() => {}} />);
  await user.click(screen.getByRole("button", { name: "Edit policy" }));
  const first = screen.getByText("Field rule 1: PID-5 · Remove field", { selector: "summary" });
  const second = screen.getByText("Field rule 2: PID-7 · Patient date shift", { selector: "summary" });
  expect((first.parentElement as HTMLDetailsElement).open).toBe(false);
  expect((second.parentElement as HTMLDetailsElement).open).toBe(false);
  await user.click(second);
  expect((second.parentElement as HTMLDetailsElement).open).toBe(true);
  expect((first.parentElement as HTMLDetailsElement).open).toBe(false);
  expect((screen.getByRole("combobox", { name: "Data category 2" }) as HTMLSelectElement).value).toBe("dates-and-ages");
  expect(screen.getByRole("button", { name: "Remove field rule 2" }).textContent).toBe("Remove field rule");
  expect(screen.getByRole("textbox", { name: "Authority selector 1" })).toBeTruthy();
  expect(screen.getByRole("button", { name: "Remove authority 1" }).textContent).toBe("Remove authority");
  expect(screen.getByRole("button", { name: "Remove segment rule 1" }).textContent).toBe("Remove segment rule");
  expect(screen.getByRole("spinbutton", { name: "Assertion position 1" })).toBeTruthy();
  expect(screen.getByRole("button", { name: "Remove required failure 1" }).textContent).toBe("Remove required failure");

  // A rule added here opens for editing; the collapsed ones stay as they were.
  await user.click(screen.getByRole("button", { name: "Add field rule" }));
  const added = screen.getByText("Field rule 3: no selector · Remove field", { selector: "summary" });
  expect((added.parentElement as HTMLDetailsElement).open).toBe(true);
  expect((first.parentElement as HTMLDetailsElement).open).toBe(false);
});
