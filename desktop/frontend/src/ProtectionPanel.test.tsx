// Encrypted packages: packages pack from workspace entries under an active
// control, and opening, retention and discard carry the operation's own
// refusals and limits. Controls themselves are managed in Encryption.test.
import { expect, test } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ProtectionPanel } from "./ProtectionPanel";
import { facadeStub, installFacade } from "./testkit/wails";
import type { FacadeHandlers } from "./testkit/wails";
import type { Artifact } from "./bindings";
import {
  WORKSPACE_ROOT,
  protectionDiscardResult,
  protectionPackageResult,
  protectionResult,
} from "./testkit/fixtures";

const DOCUMENT_ENTRY = "protection.json";
const CONTROL_NAME = "lab-evidence";
const SOURCE_ENTRY = "export-001";
const PACKAGE_ENTRY = "protected-001";

const ENTRIES: Artifact[] = [
  { name: DOCUMENT_ENTRY, kind: "protection" },
  { name: SOURCE_ENTRY, kind: "derived-export" },
  { name: PACKAGE_ENTRY, kind: "transfer-package" },
];

/** The packing task, with its own protection file and control. */
function packTask() {
  return within(screen.getByRole("group", { name: "Encrypted transfer packages" }));
}

function renderPanel(handlers: FacadeHandlers = {}, entries: Artifact[] = ENTRIES) {
  const events: string[] = [];
  installFacade({
    Cancel: async () => {
      events.push("cancel");
    },
    ...handlers,
  });
  render(
    <ProtectionPanel
      workspace={WORKSPACE_ROOT}
      entries={entries}
      onRefresh={() => events.push("refresh")}
    />,
  );
  return { events };
}

test("a package packs under an active control, reads its descriptor without a key, and carries the retention and limitation statements", async () => {
  const user = userEvent.setup();
  renderPanel({
    ReadProtection: () => protectionResult(),
    PackProtectedPackage: (request) => {
      expect(request.control).toBe(CONTROL_NAME);
      expect(request.sources).toEqual([SOURCE_ENTRY]);
      expect(request.entry).toBe(DOCUMENT_ENTRY);
      return protectionPackageResult();
    },
    InspectProtectedPackage: (workspace, entry) => {
      expect(workspace).toBe(WORKSPACE_ROOT);
      expect(entry).toBe(PACKAGE_ENTRY);
      return protectionPackageResult({ entry: PACKAGE_ENTRY });
    },
  });
  await user.selectOptions(packTask().getByLabelText("Protection file"), DOCUMENT_ENTRY);
  await waitFor(() => expect(screen.getAllByText(CONTROL_NAME).length).toBeGreaterThan(0));
  await user.selectOptions(packTask().getByLabelText("Protection file"), DOCUMENT_ENTRY);
  await packTask().findByRole("option", { name: CONTROL_NAME });
  await user.selectOptions(packTask().getByLabelText("Protection control"), CONTROL_NAME);
  await user.selectOptions(screen.getByLabelText("Package contents"), SOURCE_ENTRY);
  await user.click(screen.getByRole("button", { name: "Create encrypted package" }));
  await waitFor(() => expect(screen.getAllByText(/within-retention/).length).toBeGreaterThan(0));
  expect(screen.getAllByText(/aes-256-gcm with hkdf-sha256/).length).toBeGreaterThan(0);
  expect(screen.getAllByText(/Retirement is not revocation and deletion is not erasure/).length).toBeGreaterThan(0);

  await user.selectOptions(screen.getByLabelText("Transfer package"), PACKAGE_ENTRY);
  await waitFor(() => expect(screen.getAllByText(/control lab-evidence · key generation 1/).length).toBeGreaterThan(0));
});

test("opening and discarding carry the operation's own refusals, and the retention override is an explicit choice", async () => {
  const user = userEvent.setup();
  renderPanel({
    ReadProtection: () => protectionResult(),
    InspectProtectedPackage: () => protectionPackageResult({ entry: PACKAGE_ENTRY }),
    OpenProtectedPackage: (request) => {
      expect(request.package).toBe(PACKAGE_ENTRY);
      expect(request.entry).toBe(DOCUMENT_ENTRY);
      return {
        state: "failed" as const,
        reason: "the package records an earlier key generation than this control now reads, and that key does not open it",
        limitations: [],
      };
    },
    DiscardProtectedPackage: (request) => {
      if (!request.override) {
        return {
          state: "failed" as const,
          reason: "the package is declared retained until 2026-12-17T12:00:00Z; nothing was removed",
        };
      }
      return protectionDiscardResult({ overridden: true });
    },
  });
  await user.selectOptions(packTask().getByLabelText("Protection file"), DOCUMENT_ENTRY);
  await user.selectOptions(screen.getByLabelText("Transfer package"), PACKAGE_ENTRY);
  await waitFor(() => expect((screen.getByRole("button", { name: "Open package" }) as HTMLButtonElement).disabled).toBe(false));
  await user.click(screen.getByRole("button", { name: "Open package" }));
  await waitFor(() => expect(screen.getByText(/earlier key generation/)).toBeTruthy());

  await user.click(screen.getByRole("button", { name: "Discard package" }));
  await user.click(screen.getByRole("button", { name: "Discard it" }));
  await waitFor(() => expect(screen.getByText(/declared retained until 2026-12-17T12:00:00Z; nothing was removed/)).toBeTruthy());

  await user.selectOptions(screen.getByLabelText("Retention handling"), "override");
  await user.click(screen.getByRole("button", { name: "Discard package" }));
  await user.click(screen.getByRole("button", { name: "Discard it" }));
  await waitFor(() => expect(screen.getByText(/Unlinked 3 declared files/)).toBeTruthy());
});

const OTHER_DOCUMENT = "controls.json";
const OTHER_CONTROL = "archive-evidence";
const TWO_DOCUMENTS: Artifact[] = [...ENTRIES, { name: OTHER_DOCUMENT, kind: "protection" }];

/** The fixture document under another name, holding one other control. */
function otherDocument() {
  const shown = protectionResult();
  const control = shown.document!.controls[0]!;
  return { ...protectionResult({ entry: OTHER_DOCUMENT, controls: [{ ...control, name: OTHER_CONTROL }] }), entry: OTHER_DOCUMENT };
}

/** The option texts of the packing task's control selector. */
function packControls() {
  return within(packTask().getByLabelText("Protection control")).getAllByRole("option").map((option) => option.textContent);
}

test("a late read of the previous packing file cannot populate the controls of the file chosen since", async () => {
  const user = userEvent.setup();
  renderPanel({}, TWO_DOCUMENTS);
  const parked = facadeStub().park("ReadProtection");
  await user.selectOptions(packTask().getByLabelText("Protection file"), DOCUMENT_ENTRY);
  await user.selectOptions(packTask().getByLabelText("Protection file"), OTHER_DOCUMENT);
  expect(parked.size).toBe(2);
  // The read of the first file answers last-but-one, then the second's.
  parked.resolve(protectionResult());
  await waitFor(() => expect(parked.size).toBe(1));
  expect(packControls()).toEqual(["Select a control…"]);
  parked.resolve(otherDocument());
  await packTask().findByRole("option", { name: OTHER_CONTROL });
  expect(packControls()).toEqual(["Select a control…", OTHER_CONTROL]);
});

test("opening says Cancel opening and packing says Cancel packing, each stopping only its own operation, and an opened package is listed as plaintext in local custody", async () => {
  const user = userEvent.setup();
  const { events } = renderPanel({
    ReadProtection: () => protectionResult(),
    InspectProtectedPackage: () => protectionPackageResult({ entry: PACKAGE_ENTRY }),
  });
  const opening = facadeStub().park("OpenProtectedPackage");
  const packing = facadeStub().park("PackProtectedPackage");
  await user.selectOptions(packTask().getByLabelText("Protection file"), DOCUMENT_ENTRY);
  await user.selectOptions(screen.getByLabelText("Transfer package"), PACKAGE_ENTRY);
  const open = await screen.findByRole("button", { name: "Open package" });
  await waitFor(() => expect((open as HTMLButtonElement).disabled).toBe(false));
  await user.click(open);
  const cancelOpening = await screen.findByRole("button", { name: "Cancel opening" });
  await waitFor(() => expect((cancelOpening as HTMLButtonElement).disabled).toBe(false));
  expect((screen.getByRole("button", { name: "Cancel packing" }) as HTMLButtonElement).disabled).toBe(true);
  await user.click(cancelOpening);
  expect(facadeStub().oneCall("Cancel")).toEqual(["protect"]);
  opening.resolve(protectionPackageResult({ entry: "opened-001" }));
  expect(await screen.findByText(/^Opened into/)).toHaveProperty(
    "textContent",
    "Opened into opened-001. The output is plaintext in this workspace, in local custody. Opening ended the protection the package carried: the decrypted output is protected by this machine's own storage control and an owner-only mode, and by nothing else.",
  );
  // The plaintext output is listed with the workspace.
  expect(events).toContain("refresh");

  await user.selectOptions(packTask().getByLabelText("Protection file"), DOCUMENT_ENTRY);
  await packTask().findByRole("option", { name: CONTROL_NAME });
  await user.selectOptions(packTask().getByLabelText("Protection control"), CONTROL_NAME);
  await user.selectOptions(screen.getByLabelText("Package contents"), SOURCE_ENTRY);
  await user.click(screen.getByRole("button", { name: "Create encrypted package" }));
  await waitFor(() => expect((screen.getByRole("button", { name: "Cancel packing" }) as HTMLButtonElement).disabled).toBe(false));
  expect((screen.getByRole("button", { name: "Cancel opening" }) as HTMLButtonElement).disabled).toBe(true);
  packing.resolve(protectionPackageResult());
  await waitFor(() => expect((screen.getByRole("button", { name: "Cancel packing" }) as HTMLButtonElement).disabled).toBe(true));
});

test("an open the package refuses lists nothing new", async () => {
  const user = userEvent.setup();
  const { events } = renderPanel({
    ReadProtection: () => protectionResult(),
    InspectProtectedPackage: () => protectionPackageResult({ entry: PACKAGE_ENTRY }),
    OpenProtectedPackage: () => ({ state: "failed" as const, reason: "the key did not resolve from its declared store", limitations: [] }),
  });
  await user.selectOptions(packTask().getByLabelText("Protection file"), DOCUMENT_ENTRY);
  await user.selectOptions(screen.getByLabelText("Transfer package"), PACKAGE_ENTRY);
  const open = await screen.findByRole("button", { name: "Open package" });
  await waitFor(() => expect((open as HTMLButtonElement).disabled).toBe(false));
  await user.click(open);
  expect(await screen.findByText("the key did not resolve from its declared store")).toBeTruthy();
  expect(events).not.toContain("refresh");
  expect(screen.queryByText(/plaintext/)).toBeNull();
});

test("discarding asks first, naming the package, its declared retention and the retention handling, and Keep package discards nothing", async () => {
  const user = userEvent.setup();
  renderPanel({
    InspectProtectedPackage: () => protectionPackageResult({ entry: PACKAGE_ENTRY }),
    DiscardProtectedPackage: (request) => {
      expect(request).toEqual({ workspace: WORKSPACE_ROOT, package: PACKAGE_ENTRY, override: true });
      return protectionDiscardResult({ overridden: true });
    },
  });
  await user.selectOptions(screen.getByLabelText("Transfer package"), PACKAGE_ENTRY);
  await screen.findByRole("button", { name: "Discard package" });
  await user.selectOptions(screen.getByLabelText("Retention handling"), "override");
  await user.click(screen.getByRole("button", { name: "Discard package" }));
  const question = within(screen.getByRole("group", { name: `Discard ${PACKAGE_ENTRY}?` }));
  expect(question.getByText(/^Discard protected-001\?/).textContent?.replace(/\s+/g, " ").trim()).toBe(
    "Discard protected-001? Declared retention: within-retention until 2026-12-17T12:00:00Z. Retention handling: Override retention. Discarding unlinks the files this package declares; unlinking is not erasure, and a copy already moved elsewhere is untouched.",
  );
  expect(document.activeElement).toBe(question.getByRole("button", { name: "Keep package" }));
  await user.keyboard("{Enter}");
  expect(screen.queryByRole("group", { name: `Discard ${PACKAGE_ENTRY}?` })).toBeNull();
  await user.click(screen.getByRole("button", { name: "Discard package" }));
  await user.keyboard("{Escape}");
  expect(screen.queryByRole("group", { name: `Discard ${PACKAGE_ENTRY}?` })).toBeNull();
  expect(facadeStub().callsTo("DiscardProtectedPackage")).toHaveLength(0);

  await user.click(screen.getByRole("button", { name: "Discard package" }));
  await user.click(screen.getByRole("button", { name: "Discard it" }));
  expect(await screen.findByText(/Unlinked 3 declared files/)).toBeTruthy();
  expect(facadeStub().callsTo("DiscardProtectedPackage")).toHaveLength(1);
});

test("selecting another package or clearing the selection resets the retention handling and withdraws a pending discard question", async () => {
  const user = userEvent.setup();
  const SECOND = "protected-002";
  renderPanel(
    { InspectProtectedPackage: (_workspace, entry) => protectionPackageResult({ entry }) },
    [...ENTRIES, { name: SECOND, kind: "transfer-package" }],
  );
  const handling = () => screen.getByLabelText("Retention handling") as HTMLSelectElement;
  await user.selectOptions(screen.getByLabelText("Transfer package"), PACKAGE_ENTRY);
  await screen.findByRole("button", { name: "Discard package" });
  await user.selectOptions(handling(), "override");
  await user.click(screen.getByRole("button", { name: "Discard package" }));
  expect(screen.getByRole("group", { name: `Discard ${PACKAGE_ENTRY}?` })).toBeTruthy();

  await user.selectOptions(screen.getByLabelText("Transfer package"), SECOND);
  await screen.findByText(byText(`Package ${SECOND} · `));
  expect(screen.queryByRole("group", { name: /^Discard / })).toBeNull();
  expect(handling().value).toBe("declared");

  await user.selectOptions(handling(), "override");
  await user.click(screen.getByRole("button", { name: "Discard package" }));
  await user.selectOptions(screen.getByLabelText("Transfer package"), "");
  expect(screen.queryByRole("group", { name: /^Discard / })).toBeNull();
  expect(screen.queryByLabelText("Retention handling")).toBeNull();
  await user.selectOptions(screen.getByLabelText("Transfer package"), PACKAGE_ENTRY);
  await screen.findByRole("button", { name: "Discard package" });
  expect(handling().value).toBe("declared");
  expect(facadeStub().callsTo("DiscardProtectedPackage")).toHaveLength(0);
});

/** Matches the paragraph whose whole text starts with the given words. */
function byText(start: string) {
  return (_content: string, element: Element | null) =>
    element?.tagName === "P" && (element.textContent ?? "").startsWith(start);
}

/** The protection file's lab-evidence control at the given key generation. */
function atGeneration(generation: number) {
  const result = protectionResult();
  result.document!.controls[0]!.generation = generation;
  return result;
}

test("a package is packed under the key generation its control was chosen at", async () => {
  const user = userEvent.setup();
  renderPanel({ ReadProtection: () => atGeneration(3), PackProtectedPackage: () => protectionPackageResult() });
  await user.selectOptions(packTask().getByLabelText("Protection file"), DOCUMENT_ENTRY);
  await packTask().findByRole("option", { name: CONTROL_NAME });
  await user.selectOptions(packTask().getByLabelText("Protection control"), CONTROL_NAME);
  await user.selectOptions(screen.getByLabelText("Package contents"), SOURCE_ENTRY);
  await user.click(screen.getByRole("button", { name: "Create encrypted package" }));
  await waitFor(() => expect(facadeStub().callsTo("PackProtectedPackage")).toHaveLength(1));
  expect(facadeStub().oneCall("PackProtectedPackage")[0]).toMatchObject({ entry: DOCUMENT_ENTRY, control: CONTROL_NAME, generation: 3, sources: [SOURCE_ENTRY] });
});

test("a control rotated since it was chosen is withdrawn from the pack task when Packages opens again", async () => {
  const user = userEvent.setup();
  installFacade({ ReadProtection: () => atGeneration(1) });
  const first = render(<ProtectionPanel workspace={WORKSPACE_ROOT} entries={ENTRIES} onRefresh={() => {}} />);
  await user.selectOptions(packTask().getByLabelText("Protection file"), DOCUMENT_ENTRY);
  await packTask().findByRole("option", { name: CONTROL_NAME });
  await user.selectOptions(packTask().getByLabelText("Protection control"), CONTROL_NAME);
  expect((packTask().getByLabelText("Protection control") as HTMLSelectElement).value).toBe(CONTROL_NAME);
  first.unmount();

  // The control is rotated in Encryption while Packages is closed.
  facadeStub().reply({ ReadProtection: () => atGeneration(2) });
  render(<ProtectionPanel workspace={WORKSPACE_ROOT} entries={ENTRIES} onRefresh={() => {}} />);
  expect(await screen.findByText("Generation changed; choose the control again.")).toBeTruthy();
  expect((packTask().getByLabelText("Protection control") as HTMLSelectElement).value).toBe("");
  expect((screen.getByRole("button", { name: "Create encrypted package" }) as HTMLButtonElement).disabled).toBe(true);
  expect(facadeStub().callsTo("PackProtectedPackage")).toHaveLength(0);
});
