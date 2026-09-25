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
  await user.selectOptions(screen.getByLabelText("Document entry"), DOCUMENT_ENTRY);
  await waitFor(() => expect(screen.getAllByText(CONTROL_NAME).length).toBeGreaterThan(0));
  // The key is the mask, and the locator arguments are a count.
  expect(screen.getByText(/· 4 locator arguments/)).toBeTruthy();
  expect(screen.queryByText("readmit-lab-key")).toBeNull();

  await user.type(screen.getByLabelText("Control name"), CONTROL_NAME);
  await user.type(screen.getByLabelText("Absolute path of the program that prints the key"), "/absolute/key-store-program");
  await user.type(screen.getByLabelText("One locator argument (never key material)"), "find-generic-password");
  await user.click(screen.getByRole("button", { name: "Add argument" }));
  await user.type(screen.getByLabelText("Packages declare retention of (Go duration, optional)"), "2160h");
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
  await user.selectOptions(screen.getByLabelText("Document entry"), DOCUMENT_ENTRY);
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
  await user.selectOptions(screen.getByLabelText("Document entry"), DOCUMENT_ENTRY);
  await waitFor(() => expect(screen.getAllByText(CONTROL_NAME).length).toBeGreaterThan(0));
  await user.selectOptions(screen.getByLabelText("Control"), CONTROL_NAME);
  await user.selectOptions(screen.getByLabelText("Entries to pack (copied, never moved)"), SOURCE_ENTRY);
  await user.click(screen.getByRole("button", { name: "Pack protected package" }));
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
  await user.selectOptions(screen.getByLabelText("Document entry"), DOCUMENT_ENTRY);
  await user.selectOptions(screen.getByLabelText("Transfer package"), PACKAGE_ENTRY);
  await waitFor(() => expect((screen.getByRole("button", { name: "Open package" }) as HTMLButtonElement).disabled).toBe(false));
  await user.click(screen.getByRole("button", { name: "Open package" }));
  await waitFor(() => expect(screen.getByText(/earlier key generation/)).toBeTruthy());

  await user.click(screen.getByRole("button", { name: "Discard package" }));
  await waitFor(() => expect(screen.getByText(/declared retained until 2026-12-17T12:00:00Z; nothing was removed/)).toBeTruthy());

  await user.selectOptions(screen.getByLabelText("Declared retention override"), "override");
  await user.click(screen.getByRole("button", { name: "Discard package" }));
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
    await user.selectOptions(screen.getByLabelText("Document entry"), DOCUMENT_ENTRY);
    const retire = await screen.findByRole("button", { name: `Retire ${CONTROL_NAME}` });
    await waitFor(() => expect((retire as HTMLButtonElement).disabled).toBe(false));
    expect(within(screen.getByLabelText("Control")).getAllByRole("option").map((option) => option.textContent)).toEqual(["Select a control…", CONTROL_NAME]);

    // Reached from the keyboard, Retire asks; Escape answers the question and
    // goes no further than it, and focus returns to Retire.
    await user.click(screen.getByLabelText("New protection document"));
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
    expect(within(screen.getByLabelText("Control")).getAllByRole("option").map((option) => option.textContent)).toEqual(["Select a control…"]);
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
  await user.selectOptions(screen.getByLabelText("Document entry"), DOCUMENT_ENTRY);
  await waitFor(() => expect(screen.getAllByText(CONTROL_NAME).length).toBeGreaterThan(0));
  await user.selectOptions(screen.getByLabelText("Control"), CONTROL_NAME);
  await user.selectOptions(screen.getByLabelText("Entries to pack (copied, never moved)"), SOURCE_ENTRY);
  expect((screen.getByRole("button", { name: "Pack protected package" }) as HTMLButtonElement).disabled).toBe(false);
  await user.click(screen.getByRole("button", { name: `Retire ${CONTROL_NAME}` }));
  await user.click(screen.getByRole("button", { name: "Retire it" }));
  await screen.findByText(/^Retired lab-evidence: /);
  expect((screen.getByLabelText("Control") as HTMLSelectElement).value).toBe("");
  expect((screen.getByRole("button", { name: "Pack protected package" }) as HTMLButtonElement).disabled).toBe(true);
  expect(facadeStub().callsTo("PackProtectedPackage")).toHaveLength(0);
});

test("a refused retirement keeps the control active and says why, and a recorded rotation shows the generation it recorded", async () => {
  const user = userEvent.setup();
  renderPanel({
    ReadProtection: () => protectionResult(),
    RetireProtectionControl: () => ({ state: "failed" as const, reason: "the protection control is already retired" }),
    RotateProtectionControl: () => withControl({ generation: 2 }),
  });
  await user.selectOptions(screen.getByLabelText("Document entry"), DOCUMENT_ENTRY);
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
  await user.selectOptions(screen.getByLabelText("Document entry"), DOCUMENT_ENTRY);
  await user.click(await screen.findByRole("button", { name: `Retire ${CONTROL_NAME}` }));
  await user.click(screen.getByRole("button", { name: "Retire it" }));
  expect(await screen.findByText(`Retiring ${CONTROL_NAME}.`)).toBeTruthy();
  for (const name of [`Retire ${CONTROL_NAME}`, "Record rotation", "Use this document"]) {
    expect((screen.getByRole("button", { name }) as HTMLButtonElement).disabled).toBe(true);
  }
  expect((screen.getByLabelText("Document entry") as HTMLSelectElement).disabled).toBe(true);
  parked.resolve(withControl({ state: "retired" }));
  expect(await screen.findByText(/^Retired lab-evidence: /)).toBeTruthy();
  expect(screen.queryByText(`Retiring ${CONTROL_NAME}.`)).toBeNull();
  expect((screen.getByLabelText("Document entry") as HTMLSelectElement).disabled).toBe(false);
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
  expect(within(screen.getByLabelText("Document entry")).getAllByRole("option")).toHaveLength(1);
  await user.type(screen.getByLabelText("New protection document"), NEW_DOCUMENT);
  await user.click(screen.getByRole("button", { name: "Use this document" }));
  expect(await screen.findByText(`No control is registered in ${NEW_DOCUMENT} yet. Registering the first one writes it.`)).toBeTruthy();
  expect((screen.getByLabelText("Document entry") as HTMLSelectElement).value).toBe(NEW_DOCUMENT);
  expect((screen.getByLabelText("New protection document") as HTMLInputElement).value).toBe("");

  await user.type(screen.getByLabelText("Control name"), CONTROL_NAME);
  await user.type(screen.getByLabelText("Absolute path of the program that prints the key"), "/absolute/key-store-program");
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
  const field = screen.getByLabelText("New protection document");
  const use = screen.getByRole("button", { name: "Use this document" }) as HTMLButtonElement;
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
