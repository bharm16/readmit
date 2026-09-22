// The protection screen at the window: controls register as structured
// references with the key masked and the locator arguments counted, a rotation
// records nothing unless the declared store answers, packages pack from
// workspace entries under an active control, and opening, retention and
// discard carry the operation's own refusals and limits.
import { expect, test } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ProtectionPanel } from "./ProtectionPanel";
import { installFacade } from "./testkit/wails";
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
  await user.click(screen.getByRole("button", { name: "Add this locator argument" }));
  await user.type(screen.getByLabelText("Packages declare retention of (Go duration, optional)"), "2160h");
  await user.click(screen.getByRole("button", { name: "Register this control" }));
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
  await waitFor(() => expect((screen.getByRole("button", { name: "Open with the control above" }) as HTMLButtonElement).disabled).toBe(false));
  await user.click(screen.getByRole("button", { name: "Open with the control above" }));
  await waitFor(() => expect(screen.getByText(/earlier key generation/)).toBeTruthy());

  await user.click(screen.getByRole("button", { name: "Discard this package" }));
  await waitFor(() => expect(screen.getByText(/declared retained until 2026-12-17T12:00:00Z; nothing was removed/)).toBeTruthy());

  await user.selectOptions(screen.getByLabelText("Declared retention override"), "override");
  await user.click(screen.getByRole("button", { name: "Discard this package" }));
  await waitFor(() => expect(screen.getByText(/Unlinked 3 declared files/)).toBeTruthy());
});
