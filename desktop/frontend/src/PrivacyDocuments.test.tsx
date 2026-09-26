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
  render(<PrivacyDocuments workspace={workspace} policyName="policy.json" inventoryName="inventory.json"
    onSaved={(kind, entry) => saved.push(`${kind}:${entry}`)} />);
  expect((screen.getByLabelText("Patient identifier selector") as HTMLInputElement).value).toBe("");
  expect(screen.queryByLabelText("Known value 1")).toBeNull();
  await user.click(screen.getByRole("button", { name: "Edit policy" }));
  expect((screen.getByLabelText("Patient identifier selector") as HTMLInputElement).value).toBe("PID-3.1");
  await user.type(screen.getByLabelText("Policy file"), "edited-policy.json");
  await user.click(screen.getByRole("button", { name: "Save policy" }));
  expect(await screen.findByText("Saved disclosure policy edited-policy.json.")).toBeTruthy();

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
  render(<PrivacyDocuments workspace={workspace} policyName="" inventoryName="" onSaved={() => { throw new Error("invalid document was saved"); }} />);
  expect((screen.getByLabelText("Patient identifier selector") as HTMLInputElement).value).toBe("");
  expect(screen.queryByLabelText("Field selector 1")).toBeNull();
  expect(screen.queryByLabelText("Known value 1")).toBeNull();
  await user.type(screen.getByLabelText("Policy file"), "policy.json");
  await user.click(screen.getByRole("button", { name: "Save policy" }));
  expect(await screen.findByText("invalid redaction policy: required_failures declares no assertion position")).toBeTruthy();
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
  render(<PrivacyDocuments workspace={workspace} policyName="" inventoryName="" drafts={[draft]} onSaved={() => {}} />);
  await waitFor(() => expect((screen.getByLabelText("Patient identifier selector") as HTMLInputElement).value).toBe("PID-3.1"));
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
  render(<PrivacyDocuments workspace={workspace} policyName="" inventoryName="" drafts={[draft]} onSaved={() => {}} />);
  expect(await screen.findByText(/retained disclosure policy draft is unsupported/)).toBeTruthy();
  expect((screen.getByLabelText("Patient identifier selector") as HTMLInputElement).value).toBe("");
});

test("a refused open keeps the valid disclosure document already on screen", async () => {
  const user = userEvent.setup();
  installFacade({
    ReadRedactPolicy: () => ({ state: "completed", policy: existingPolicy }),
    ReadRedactInventory: () => ({ state: "completed", inventory: existingInventory }),
  });
  render(<PrivacyDocuments workspace={workspace} policyName="policy.json" inventoryName="inventory.json" onSaved={() => {}} />);
  await user.click(screen.getByRole("button", { name: "Edit policy" }));
  await user.click(screen.getByRole("button", { name: "Edit inventory" }));
  expect((screen.getByLabelText("Patient identifier selector") as HTMLInputElement).value).toBe("PID-3.1");
  expect((screen.getByLabelText("Artifact path 1") as HTMLInputElement).value).toBe("original-run");
  facadeStub().reply({
    ReadRedactPolicy: () => ({ state: "failed", reason: "unsupported disclosure policy contract" }),
    ReadRedactInventory: () => ({ state: "failed", reason: "inventory requires explicit complete scope" }),
  });
  await user.click(screen.getByRole("button", { name: "Edit policy" }));
  await user.click(screen.getByRole("button", { name: "Edit inventory" }));
  expect(await screen.findByText(/The selected policy was not opened: unsupported disclosure policy contract/)).toBeTruthy();
  expect(await screen.findByText(/The selected inventory was not opened: inventory requires explicit complete scope/)).toBeTruthy();
  expect((screen.getByLabelText("Patient identifier selector") as HTMLInputElement).value).toBe("PID-3.1");
  expect((screen.getByLabelText("Artifact path 1") as HTMLInputElement).value).toBe("original-run");
});

const editors = [
  { kind: "policy", output: "Policy file", discard: "Discard draft", refusal: "The disclosure policy draft could not be discarded", name: "pending-policy.json" },
  { kind: "inventory", output: "New original-artifact inventory document", discard: "Discard draft", refusal: "The original-artifact inventory draft could not be discarded", name: "pending-inventory.json" },
] as const;

test.each(editors)("a $kind edit queues retention before immediate navigation", async ({ output, name }) => {
  const facade = installFacade({});
  const retaining = facade.park("SaveEditorDraft");
  const mounted = render(<PrivacyDocuments workspace={workspace} policyName="" inventoryName="" onSaved={() => {}} />);
  fireEvent.change(screen.getByLabelText(output), { target: { value: name } });
  await waitFor(() => expect(retaining.size).toBe(1));
  const queued = facade.oneCall("SaveEditorDraft")[0] as EditorDraft;
  expect((queued.content as { output: string }).output).toBe(name);
  expect(screen.queryByText("Retained. It will come back if this window stops.")).toBeNull();
  mounted.unmount();
  expect(facade.callsTo("SaveEditorDraft")).toHaveLength(1);
  retaining.resolve({ state: "completed", drafts: [{ ...queued, id: "retained-1" }] });
});

test.each(editors)("a refused $kind draft discard preserves text and retries the same identity", async ({ output, discard, refusal, name }) => {
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
  render(<PrivacyDocuments workspace={workspace} policyName="" inventoryName="" onSaved={() => {}} />);
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

test.each(editors)("a saved $kind document reports refused draft cleanup", async ({ kind, output, discard, name }) => {
  const user = userEvent.setup();
  const saved: string[] = [];
  installFacade({
    SaveEditorDraft: (draft) => ({ state: "completed", drafts: [{ ...draft, id: "retained-1" }] }),
    DiscardEditorDraft: () => ({ state: "failed", reason: "the draft store could not discard it" }),
    SaveRedactPolicy: ({ output: entry, policy }) => ({ state: "completed", entry, policy }),
    SaveRedactInventory: ({ output: entry, inventory }) => ({ state: "completed", entry, inventory }),
  });
  render(<PrivacyDocuments workspace={workspace} policyName="" inventoryName="" onSaved={(savedKind, entry) => saved.push(`${savedKind}:${entry}`)} />);
  fireEvent.change(screen.getByLabelText(output), { target: { value: name } });
  expect(await screen.findByText("Retained. It will come back if this window stops.")).toBeTruthy();
  await user.click(screen.getByRole("button", { name: kind === "policy" ? "Save policy" : "Save inventory" }));
  expect(await screen.findByText(new RegExp(`Saved .*${name.replace(".", "\\.")}, but its working draft could not be discarded`))).toBeTruthy();
  expect((screen.getByLabelText(output) as HTMLInputElement).value).toBe(name);
  expect(screen.getByRole("button", { name: discard })).toBeTruthy();
  expect(saved).toEqual([`${kind}:${name}`]);
});
