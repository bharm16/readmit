// The protection screen at the window: controls register as structured
// references with the key masked and the locator arguments counted, a rotation
// records nothing unless the declared store answers, packages pack from
// workspace entries under an active control, a control is retired only once
// the person confirms it and is then shown retired and offered for no new
// package, and opening, retention and discard carry the operation's own
// refusals and limits.
import { expect, test } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ProtectionPanel } from "./ProtectionPanel";
import { facadeStub, installFacade } from "./testkit/wails";
import type { FacadeHandlers } from "./testkit/wails";
import type { Artifact, ProtectionControl } from "./bindings";
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

/** The protection-file task: the document read above, its controls and
 * their registration. */
function documentTask() {
  return within(screen.getByRole("group", { name: "Protection document" }));
}

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

test("a control registers as a structured reference, and the document shows the key masked with the locator counted", async () => {
  const user = userEvent.setup();
  renderPanel({
    ReadProtection: (workspace, entry) => {
      expect(workspace).toBe(WORKSPACE_ROOT);
      expect(entry).toBe(DOCUMENT_ENTRY);
      return protectionResult();
    },
    SaveProtectionControl: (request) => {
      expect(request.entry).toBe(DOCUMENT_ENTRY);
      expect(request.name).toBe(CONTROL_NAME);
      expect(request.storage).toBe("os-volume-encryption");
      expect(request.command).toBe("/absolute/key-store-program");
      expect(request.arguments).toEqual(["find-generic-password"]);
      expect(request.retain).toBe("2160h");
      return protectionResult();
    },
  });
  await user.selectOptions(documentTask().getByLabelText("Protection file"), DOCUMENT_ENTRY);
  await waitFor(() => expect(screen.getAllByText(CONTROL_NAME).length).toBeGreaterThan(0));
  // The key is the mask, and the locator arguments are a count.
  expect(screen.getByText(/· 4 locator arguments/)).toBeTruthy();
  expect(screen.queryByText("readmit-lab-key")).toBeNull();

  await user.type(screen.getByLabelText("Control name"), CONTROL_NAME);
  await user.type(screen.getByLabelText("Key lookup program"), "/absolute/key-store-program");
  await user.type(screen.getByLabelText("Lookup argument"), "find-generic-password");
  await user.click(screen.getByRole("button", { name: "Add argument" }));
  await user.type(screen.getByLabelText("Package retention (optional)"), "2160h");
  await user.click(screen.getByRole("button", { name: "Register control" }));
  await waitFor(() =>
    expect(
      screen.getByText(/Registering a reference proves nothing about the store behind it/),
    ).toBeTruthy(),
  );
});

test("a rotation the store does not answer for records nothing, and the sentence says so", async () => {
  const user = userEvent.setup();
  renderPanel({
    ReadProtection: () => protectionResult(),
    RotateProtectionControl: (workspace, entry, name) => {
      expect(workspace).toBe(WORKSPACE_ROOT);
      expect(entry).toBe(DOCUMENT_ENTRY);
      expect(name).toBe(CONTROL_NAME);
      return {
        state: "failed" as const,
        reason: "the key did not resolve from its declared store; the recorded rotation is unchanged",
      };
    },
  });
  await user.selectOptions(documentTask().getByLabelText("Protection file"), DOCUMENT_ENTRY);
  await waitFor(() => expect((screen.getByRole("button", { name: "Record rotation" }) as HTMLButtonElement).disabled).toBe(false));
  await user.click(screen.getByRole("button", { name: "Record rotation" }));
  await waitFor(() => expect(screen.getByText(/the key did not resolve from its declared store/)).toBeTruthy());
});

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
  await user.selectOptions(documentTask().getByLabelText("Protection file"), DOCUMENT_ENTRY);
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
  await user.selectOptions(documentTask().getByLabelText("Protection file"), DOCUMENT_ENTRY);
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

/** Moves focus with Tab from where it is until the control has it, as a
 * keyboard user does, failing when the control cannot be reached that way. */
async function tabTo(user: ReturnType<typeof userEvent.setup>, control: HTMLElement): Promise<void> {
  for (let step = 0; step < 50; step++) {
    if (document.activeElement === control) return;
    await user.tab();
  }
  throw new Error(`${control.textContent ?? ""} is not reachable with Tab`);
}

/** The one registered control of the fixture document, in another state. */
function withControl(changes: Partial<ProtectionControl>) {
  const shown = protectionResult();
  const control = shown.document!.controls[0]!;
  return protectionResult({ controls: [{ ...control, ...changes }] });
}

/** The row of the controls table that names one control. */
function row(name: string) {
  return within(screen.getByRole("cell", { name }).closest("tr")!);
}

test("retiring asks first: Escape and Keep it active retire nothing, and a confirmed retirement shows the control retired and offers it for no new package", async () => {
  const user = userEvent.setup();
  const reached: string[] = [];
  const listen = (event: KeyboardEvent) => reached.push(event.key);
  window.addEventListener("keydown", listen);
  const { events } = renderPanel({
    ReadProtection: () => protectionResult(),
    RetireProtectionControl: (workspace, entry, name) => {
      expect([workspace, entry, name]).toEqual([WORKSPACE_ROOT, DOCUMENT_ENTRY, CONTROL_NAME]);
      return withControl({ state: "retired" });
    },
  });
  try {
    await user.selectOptions(documentTask().getByLabelText("Protection file"), DOCUMENT_ENTRY);
    const retire = await screen.findByRole("button", { name: `Retire ${CONTROL_NAME}` });
    await waitFor(() => expect((retire as HTMLButtonElement).disabled).toBe(false));
    await user.selectOptions(packTask().getByLabelText("Protection file"), DOCUMENT_ENTRY);
    await packTask().findByRole("option", { name: CONTROL_NAME });
    expect(within(packTask().getByLabelText("Protection control")).getAllByRole("option").map((option) => option.textContent)).toEqual(["Select a control…", CONTROL_NAME]);

    // Reached from the keyboard, Retire asks; Escape answers the question and
    // goes no further than it, and focus returns to Retire.
    await user.click(screen.getByLabelText("New protection file"));
    await tabTo(user, retire);
    await user.keyboard("{Enter}");
    const question = within(screen.getByRole("group", { name: `Retire ${CONTROL_NAME}?` }));
    expect(question.getByText(/It writes no new package and still opens the packages it wrote\. No command makes a retired control active again\./)).toBeTruthy();
    expect(document.activeElement).toBe(question.getByRole("button", { name: "Keep active" }));
    await user.keyboard("{Escape}");
    expect(screen.queryByRole("group", { name: `Retire ${CONTROL_NAME}?` })).toBeNull();
    expect(document.activeElement).toBe(screen.getByRole("button", { name: `Retire ${CONTROL_NAME}` }));
    expect(reached).not.toContain("Escape");

    // Keep it active answers it the same way.
    await user.keyboard("{Enter}");
    await user.keyboard("{Enter}");
    expect(screen.queryByRole("group", { name: `Retire ${CONTROL_NAME}?` })).toBeNull();
    expect(document.activeElement).toBe(screen.getByRole("button", { name: `Retire ${CONTROL_NAME}` }));
    expect(facadeStub().callsTo("RetireProtectionControl")).toHaveLength(0);
    expect(row(CONTROL_NAME).getByRole("cell", { name: "active" })).toBeTruthy();

    // Confirmed, the control is retired: the table says so, Retire is no
    // longer offered for it, it is no longer offered to write a package, and
    // focus rests on the panel rather than on a control that went away.
    await user.keyboard("{Enter}");
    await user.keyboard("{Shift>}{Tab}{/Shift}");
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "Retire it" }));
    await user.keyboard("{Enter}");
    expect(
      await screen.findByText(
        `Retired ${CONTROL_NAME}: it writes no new package and still opens the packages it wrote. Retirement is not revocation: a recipient who already has a package keeps it.`,
      ),
    ).toBeTruthy();
    expect(row(CONTROL_NAME).getByRole("cell", { name: "retired" })).toBeTruthy();
    expect((screen.getByRole("button", { name: `Retire ${CONTROL_NAME}` }) as HTMLButtonElement).disabled).toBe(true);
    expect(within(packTask().getByLabelText("Protection control")).getAllByRole("option").map((option) => option.textContent)).toEqual(["Select a control…"]);
    await waitFor(() => expect(document.activeElement).toBe(screen.getByRole("heading", { name: "Protection" })));
    expect(facadeStub().callsTo("RetireProtectionControl")).toHaveLength(1);
    expect(events).not.toContain("cancel");
  } finally {
    window.removeEventListener("keydown", listen);
  }
});

test("a control chosen for packing and then retired is no longer the control a pack names", async () => {
  const user = userEvent.setup();
  renderPanel({
    ReadProtection: () => protectionResult(),
    RetireProtectionControl: () => withControl({ state: "retired" }),
  });
  await user.selectOptions(documentTask().getByLabelText("Protection file"), DOCUMENT_ENTRY);
  await waitFor(() => expect(screen.getAllByText(CONTROL_NAME).length).toBeGreaterThan(0));
  await user.selectOptions(packTask().getByLabelText("Protection file"), DOCUMENT_ENTRY);
  await packTask().findByRole("option", { name: CONTROL_NAME });
  await user.selectOptions(packTask().getByLabelText("Protection control"), CONTROL_NAME);
  await user.selectOptions(screen.getByLabelText("Package contents"), SOURCE_ENTRY);
  expect((screen.getByRole("button", { name: "Create encrypted package" }) as HTMLButtonElement).disabled).toBe(false);
  await user.click(screen.getByRole("button", { name: `Retire ${CONTROL_NAME}` }));
  await user.click(screen.getByRole("button", { name: "Retire it" }));
  await screen.findByText(/^Retired lab-evidence: /);
  expect((packTask().getByLabelText("Protection control") as HTMLSelectElement).value).toBe("");
  expect((screen.getByRole("button", { name: "Create encrypted package" }) as HTMLButtonElement).disabled).toBe(true);
  expect(facadeStub().callsTo("PackProtectedPackage")).toHaveLength(0);
});

test("a refused retirement keeps the control active and says why, and a recorded rotation shows the generation it recorded", async () => {
  const user = userEvent.setup();
  renderPanel({
    ReadProtection: () => protectionResult(),
    RetireProtectionControl: () => ({ state: "failed" as const, reason: "the protection control is already retired" }),
    RotateProtectionControl: () => withControl({ generation: 2 }),
  });
  await user.selectOptions(documentTask().getByLabelText("Protection file"), DOCUMENT_ENTRY);
  await user.click(await screen.findByRole("button", { name: `Retire ${CONTROL_NAME}` }));
  await user.click(screen.getByRole("button", { name: "Retire it" }));
  expect(await screen.findByText("the protection control is already retired")).toBeTruthy();
  expect(row(CONTROL_NAME).getByRole("cell", { name: "active" })).toBeTruthy();
  expect(screen.queryByText(/^Retired /)).toBeNull();
  // The Retire control is still offered, so focus returns to it.
  await waitFor(() => expect(document.activeElement).toBe(screen.getByRole("button", { name: `Retire ${CONTROL_NAME}` })));

  await user.click(screen.getByRole("button", { name: "Record rotation" }));
  expect(await screen.findByText(`Recorded a rotation of ${CONTROL_NAME}. readmit never read the previous key, so a recorded rotation is an assertion, not a verification.`)).toBeTruthy();
  expect(row(CONTROL_NAME).getByRole("cell", { name: "2" })).toBeTruthy();
  expect(screen.queryByText(/^Registered\./)).toBeNull();
});

test("a retirement in progress says so and holds the panel until the facade answers", async () => {
  const user = userEvent.setup();
  renderPanel({ ReadProtection: () => protectionResult() });
  const parked = facadeStub().park("RetireProtectionControl");
  await user.selectOptions(documentTask().getByLabelText("Protection file"), DOCUMENT_ENTRY);
  await user.click(await screen.findByRole("button", { name: `Retire ${CONTROL_NAME}` }));
  await user.click(screen.getByRole("button", { name: "Retire it" }));
  expect(await screen.findByText(`Retiring ${CONTROL_NAME}.`)).toBeTruthy();
  for (const name of [`Retire ${CONTROL_NAME}`, "Record rotation", "Select file"]) {
    expect((screen.getByRole("button", { name }) as HTMLButtonElement).disabled).toBe(true);
  }
  expect((documentTask().getByLabelText("Protection file") as HTMLSelectElement).disabled).toBe(true);
  parked.resolve(withControl({ state: "retired" }));
  expect(await screen.findByText(/^Retired lab-evidence: /)).toBeTruthy();
  expect(screen.queryByText(`Retiring ${CONTROL_NAME}.`)).toBeNull();
  expect((documentTask().getByLabelText("Protection file") as HTMLSelectElement).disabled).toBe(false);
});

test("a new protection document is named, shown empty, and written by its first registration", async () => {
  const user = userEvent.setup();
  const NEW_DOCUMENT = "controls.json";
  const empty = protectionResult({ entry: NEW_DOCUMENT, controls: [] });
  const { events } = renderPanel(
    {
      ReadProtection: (workspace, entry) => {
        expect([workspace, entry]).toEqual([WORKSPACE_ROOT, NEW_DOCUMENT]);
        return { ...empty, entry: NEW_DOCUMENT };
      },
      SaveProtectionControl: (request) => {
        expect(request.entry).toBe(NEW_DOCUMENT);
        expect(request.name).toBe(CONTROL_NAME);
        return { ...protectionResult({ entry: NEW_DOCUMENT }), entry: NEW_DOCUMENT };
      },
    },
    ENTRIES.filter((entry) => entry.kind !== "protection"),
  );
  // No protection document exists yet, so none is offered to select.
  expect(within(documentTask().getByLabelText("Protection file")).getAllByRole("option")).toHaveLength(1);
  await user.type(screen.getByLabelText("New protection file"), NEW_DOCUMENT);
  await user.click(screen.getByRole("button", { name: "Select file" }));
  expect(await screen.findByText(`No control is registered in ${NEW_DOCUMENT} yet. Registering the first one writes it.`)).toBeTruthy();
  expect((documentTask().getByLabelText("Protection file") as HTMLSelectElement).value).toBe(NEW_DOCUMENT);
  expect((screen.getByLabelText("New protection file") as HTMLInputElement).value).toBe("");

  await user.type(screen.getByLabelText("Control name"), CONTROL_NAME);
  await user.type(screen.getByLabelText("Key lookup program"), "/absolute/key-store-program");
  await user.click(screen.getByRole("button", { name: "Register control" }));
  expect(await screen.findByText(/^Registered\. Registering a reference proves nothing/)).toBeTruthy();
  expect(row(CONTROL_NAME).getByRole("cell", { name: "active" })).toBeTruthy();
  expect(screen.queryByText(/No control is registered in/)).toBeNull();
  expect(events).toContain("refresh");
});

test("a document is named from the keyboard, and an entry that is not a protection document is refused by its reader", async () => {
  const user = userEvent.setup();
  const OTHER = "notes.json";
  renderPanel({
    ReadProtection: (workspace, entry) => {
      expect([workspace, entry]).toEqual([WORKSPACE_ROOT, OTHER]);
      return { state: "failed" as const, reason: "unsupported protection document version" };
    },
  });
  const field = screen.getByLabelText("New protection file");
  const use = screen.getByRole("button", { name: "Select file" }) as HTMLButtonElement;
  expect(use.disabled).toBe(true);
  await user.click(field);
  await user.keyboard(OTHER);
  await tabTo(user, use);
  await user.keyboard("{Enter}");
  expect(await screen.findByText("unsupported protection document version")).toBeTruthy();
  expect(screen.queryByText(/No control is registered in/)).toBeNull();
  expect(screen.queryByRole("table")).toBeNull();
  expect(facadeStub().callsTo("ReadProtection")).toHaveLength(1);
  expect((field as HTMLInputElement).value).toBe("");
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

test("the packing control comes only from the packing task's own protection file, and changing that file withdraws the chosen control", async () => {
  const user = userEvent.setup();
  renderPanel(
    {
      ReadProtection: (_workspace, entry) => (entry === OTHER_DOCUMENT ? otherDocument() : protectionResult()),
      PackProtectedPackage: (request) => {
        expect([request.entry, request.control]).toEqual([OTHER_DOCUMENT, OTHER_CONTROL]);
        return protectionPackageResult({ control: OTHER_CONTROL });
      },
    },
    TWO_DOCUMENTS,
  );
  // The document above holds lab-evidence; nothing of it is offered for packing.
  await user.selectOptions(documentTask().getByLabelText("Protection file"), DOCUMENT_ENTRY);
  await waitFor(() => expect(row(CONTROL_NAME).getByRole("cell", { name: "active" })).toBeTruthy());
  expect(packControls()).toEqual(["Select a control…"]);

  await user.selectOptions(packTask().getByLabelText("Protection file"), DOCUMENT_ENTRY);
  await packTask().findByRole("option", { name: CONTROL_NAME });
  await user.selectOptions(packTask().getByLabelText("Protection control"), CONTROL_NAME);
  expect((packTask().getByLabelText("Protection control") as HTMLSelectElement).value).toBe(CONTROL_NAME);

  // Another file: the control of the first is withdrawn and never offered.
  await user.selectOptions(packTask().getByLabelText("Protection file"), OTHER_DOCUMENT);
  expect((packTask().getByLabelText("Protection control") as HTMLSelectElement).value).toBe("");
  await packTask().findByRole("option", { name: OTHER_CONTROL });
  expect(packControls()).toEqual(["Select a control…", OTHER_CONTROL]);
  // The document above is unchanged by the packing task's choice.
  expect(row(CONTROL_NAME).getByRole("cell", { name: "active" })).toBeTruthy();

  await user.selectOptions(packTask().getByLabelText("Protection control"), OTHER_CONTROL);
  await user.selectOptions(screen.getByLabelText("Package contents"), SOURCE_ENTRY);
  await user.click(screen.getByRole("button", { name: "Create encrypted package" }));
  await waitFor(() => expect(facadeStub().callsTo("PackProtectedPackage")).toHaveLength(1));

  // Clearing the file offers no control and packs nothing.
  await user.selectOptions(packTask().getByLabelText("Protection file"), "");
  expect(packControls()).toEqual(["Select a control…"]);
  expect((screen.getByRole("button", { name: "Create encrypted package" }) as HTMLButtonElement).disabled).toBe(true);
});

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

test("draft lookup arguments and package contents are removed from the draft before submission, and nothing is deleted", async () => {
  const user = userEvent.setup();
  const EXTRA = "export-002";
  renderPanel(
    {
      ReadProtection: () => protectionResult(),
      SaveProtectionControl: (request) => {
        expect(request.arguments).toEqual(["second"]);
        return protectionResult();
      },
      PackProtectedPackage: (request) => {
        expect(request.sources).toEqual([EXTRA]);
        return protectionPackageResult();
      },
    },
    [...ENTRIES, { name: EXTRA, kind: "derived-export" }],
  );
  await user.selectOptions(documentTask().getByLabelText("Protection file"), DOCUMENT_ENTRY);
  await waitFor(() => expect(row(CONTROL_NAME).getByRole("cell", { name: "active" })).toBeTruthy());
  await user.type(screen.getByLabelText("Control name"), "second-control");
  await user.type(screen.getByLabelText("Key lookup program"), "/absolute/key-store-program");
  for (const argument of ["first", "second"]) {
    await user.type(screen.getByLabelText("Lookup argument"), argument);
    await user.click(screen.getByRole("button", { name: "Add argument" }));
  }
  const removeFirst = screen.getByRole("button", { name: "Remove argument 1" });
  expect(removeFirst.textContent).toBe("Remove argument");
  await user.click(removeFirst);
  expect(screen.queryByRole("button", { name: "Remove argument 2" })).toBeNull();
  await user.click(screen.getByRole("button", { name: "Register control" }));
  await waitFor(() => expect(facadeStub().callsTo("SaveProtectionControl")).toHaveLength(1));

  await user.selectOptions(packTask().getByLabelText("Protection file"), DOCUMENT_ENTRY);
  await packTask().findByRole("option", { name: CONTROL_NAME });
  await user.selectOptions(packTask().getByLabelText("Protection control"), CONTROL_NAME);
  await user.selectOptions(screen.getByLabelText("Package contents"), SOURCE_ENTRY);
  await user.selectOptions(screen.getByLabelText("Package contents"), EXTRA);
  const remove = screen.getByRole("button", { name: `Remove from package ${SOURCE_ENTRY}` });
  expect(remove.textContent).toBe("Remove from package");
  await user.click(remove);
  expect(screen.queryByRole("button", { name: `Remove from package ${SOURCE_ENTRY}` })).toBeNull();
  // The workspace entry is still offered: removal only edited the draft.
  expect(within(screen.getByLabelText("Package contents")).getByRole("option", { name: SOURCE_ENTRY })).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Create encrypted package" }));
  await waitFor(() => expect(facadeStub().callsTo("PackProtectedPackage")).toHaveLength(1));
  expect(facadeStub().callsTo("DiscardProtectedPackage")).toHaveLength(0);
});

test("opening says Cancel opening and packing says Cancel packing, each stopping only its own operation, and an opened package is listed as plaintext in local custody", async () => {
  const user = userEvent.setup();
  const { events } = renderPanel({
    ReadProtection: () => protectionResult(),
    InspectProtectedPackage: () => protectionPackageResult({ entry: PACKAGE_ENTRY }),
  });
  const opening = facadeStub().park("OpenProtectedPackage");
  const packing = facadeStub().park("PackProtectedPackage");
  await user.selectOptions(documentTask().getByLabelText("Protection file"), DOCUMENT_ENTRY);
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
  await user.selectOptions(documentTask().getByLabelText("Protection file"), DOCUMENT_ENTRY);
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
