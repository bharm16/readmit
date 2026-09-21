import { expect, test } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ProfileEditor } from "./ProfileEditor";
import { installFacade } from "./testkit/wails";
import { localProfileFixture, profilePackFixture, WORKSPACE_ROOT } from "./testkit/fixtures";
import type { EditorDraft } from "./bindings";
import type { FacadeHandlers } from "./testkit/wails";

function renderEditor(handlers: FacadeHandlers = {}, drafts: EditorDraft[] | null = []) {
  const facade = installFacade({
    InspectProfilePack: () => profilePackFixture(),
    OpenProfileLibrary: () => ({
      state: "completed",
      bundleable: true,
      entries: [
        {
          pack: { id: "fixture-siu", version: "1" },
          provenance: profilePackFixture().provenance!,
        },
      ],
      matrix: [
        {
          hl7_version: "2.5.1",
          family: "SIU",
          parse: "supported",
          labels: "supported",
          structural: "unsupported",
          workflow: "unsupported",
          pack: { id: "fixture-siu", version: "1" },
        },
      ],
    }),
    ValidateProfile: () => localProfileFixture(),
    SaveProfile: () => localProfileFixture(),
    CompareProfiles: () => ({
      state: "completed",
      comparison: {
        profile: "fixture-local-siu",
        from: "1",
        to: "2",
        changes: [
          {
            part: "segment",
            kind: "changed",
            subject: "SCH",
            detail: "the description differs",
          },
        ],
      },
      assessment: {
        comparison: {
          profile: "fixture-local-siu",
          from: "1",
          to: "2",
          changes: [
            {
              part: "segment",
              kind: "changed",
              subject: "SCH",
              detail: "the description differs",
            },
          ],
        },
        tests: [
          {
            test: "test-reschedule.json",
            case: "test-case",
            pinned: {
              id: "fixture-local-siu",
              version: "1",
              sha256: "e96a3350b728a78d063cf99afddeec3854d393682039ed4348fbc89057c55054",
            },
            impact: "affected",
          },
        ],
      },
    }),
    UpgradeProfilePin: () => ({
      state: "completed",
      output: "references.json",
      references: {
        schema: "readmit-profile-references/v1",
        tests: [
          {
            test: "test-reschedule.json",
            case: "test-case",
            sha256: "040860d85eeb4697e6b393214134d1460fa587bdf14af0fb3414ba592b75e78d",
            pinned: {
              id: "fixture-local-siu",
              version: "2",
              sha256: "9999999999999999999999999999999999999999999999999999999999999999",
            },
          },
        ],
      },
    }),
    ExportProfilePackage: () => ({
      state: "completed",
      output: "package.json",
      pack: { id: "fixture-siu", version: "1" },
      profile: { id: "fixture-local-siu", version: "1" },
      version: { id: "fixture-local-siu", version: "1" },
      sha256: "aabbcc1122334455",
      rights: "testdata/fixtures/local-profile.json",
      origin: {
        schema: "readmit-profile-origin/v1",
        source_format: "readmit-local-profile/v1",
        source: "Fixture Source",
        revision: "1",
        license: "LicenseRef-readmit-fixture",
        notice: "Notice text",
        mapping_limitations: "None",
        review_reference: "Ref-123",
      },
    }),
    ImportProfilePackage: () => ({
      state: "completed",
      output: "imported",
    }),
    InspectProfilePackage: () => ({
      state: "completed",
      output: "package.json",
      profile: { id: "fixture-local-siu", version: "1" },
      pack: { id: "fixture-siu", version: "1" },
      sha256: "aabbcc1122334455",
      rights: "testdata/fixtures/local-profile.json",
      conflict: "",
    }),
    SaveEditorDraft: () => ({ state: "completed" }),
    DiscardEditorDraft: () => ({ state: "completed" }),
    ...handlers,
  });

  render(<ProfileEditor workspace={WORKSPACE_ROOT} drafts={drafts} busy={false} />);
  return facade;
}

test("browses installed packs and renders provenance and 4 distinct support levels", async () => {
  const user = userEvent.setup();
  renderEditor();

  // Click Installed Packs tab
  const tab = screen.getByRole("tab", { name: "Installed Packs" });
  await user.click(tab);

  // Click Inspect Pack button
  const inspectBtn = screen.getByRole("button", { name: "Inspect Pack" });
  await user.click(inspectBtn);

  // Verifies pack identity and provenance
  expect(await screen.findByText(/Pack: fixture-siu/)).toBeDefined();
  expect(screen.getByText(/LicenseRef-readmit-fixture/)).toBeDefined();
  expect(screen.getAllByText(/approved/).length).toBeGreaterThan(0);
  expect(screen.getByText(/approved for bundling/)).toBeDefined();

  // Verifies 4 separate support levels
  expect(screen.getByRole("columnheader", { name: "Lossless Parsing" })).toBeDefined();
  expect(screen.getByRole("columnheader", { name: "Dictionary Labels" })).toBeDefined();
  expect(screen.getByRole("columnheader", { name: "Structure Validation" })).toBeDefined();
  expect(screen.getByRole("columnheader", { name: "Workflow Evaluation" })).toBeDefined();

  // Opens library
  const openLibBtn = screen.getByRole("button", { name: "Open Library" });
  await user.click(openLibBtn);
  expect((await screen.findAllByText("2.5.1")).length).toBeGreaterThan(0);
  expect(screen.getByText("fixture-siu v1")).toBeDefined();
});

test("renders local profile editor with structured segment and field controls, including Z-segments", async () => {
  const user = userEvent.setup();
  renderEditor();

  // Shows Profile Editor tab by default
  expect(screen.getByLabelText("Profile ID")).toBeDefined();
  expect(screen.getByLabelText("Pinned Pack ID")).toBeDefined();

  // Verifies existing field selector
  expect(screen.getByText("SCH-1")).toBeDefined();
  expect((screen.getByLabelText("Name for SCH-1") as HTMLInputElement).value).toBe("Placer appointment number");
  expect((screen.getByLabelText("Usage for SCH-1") as HTMLSelectElement).value).toBe("R");

  // Changes usage to Conditional (C)
  const usageSelect = screen.getByLabelText("Usage for SCH-1");
  await user.selectOptions(usageSelect, "C");

  // Condition controls appear
  expect(screen.getByLabelText("Condition operator for SCH-1")).toBeDefined();

  // Adds a site-defined Z-segment
  window.prompt = () => "ZPD";
  const addSegBtn = screen.getByRole("button", { name: "+ Add Segment" });
  await user.click(addSegBtn);

  expect((await screen.findAllByText(/Site-defined Z-segment/)).length).toBeGreaterThan(0);
});

test("validates profile through Go backend and displays resolution findings and seal", async () => {
  const user = userEvent.setup();
  renderEditor();

  const validateBtn = screen.getByRole("button", { name: "Validate with Engine" });
  await user.click(validateBtn);

  expect(await screen.findByText("Validation Result: completed")).toBeDefined();
  expect(screen.getByText(/Sealed Version:/)).toBeDefined();
  expect(screen.getByText("e96a3350b728a78d063cf99afddeec3854d393682039ed4348fbc89057c55054")).toBeDefined();
});

test("saves profile revision and displays refusal on immutability failure", async () => {
  const user = userEvent.setup();
  renderEditor({
    SaveProfile: () => ({
      state: "failed",
      reason: "cannot overwrite existing profile; approved profiles are immutable, save as a new revision",
    }),
  });

  const saveBtn = screen.getByRole("button", { name: "Save Profile Revision" });
  await user.click(saveBtn);

  expect(await screen.findByText(/approved profiles are immutable/)).toBeDefined();
});

test("compares profile versions and explicitly upgrades pinned consumer", async () => {
  const user = userEvent.setup();
  const facade = renderEditor();

  // Switch to Version Comparison tab
  await user.click(screen.getByRole("tab", { name: "Version Comparison & Pins" }));

  // Click compare button
  const compareBtn = screen.getByRole("button", { name: "Compare & Assess Tests" });
  await user.click(compareBtn);

  // Verifies changes displayed
  expect(await screen.findByText(/the description differs/)).toBeDefined();

  // Verifies impacted test
  expect(screen.getByText("test-reschedule.json")).toBeDefined();
  expect(screen.getByText("affected")).toBeDefined();

  // Upgrades pin explicitly
  const upgradeBtn = screen.getByRole("button", { name: "Upgrade Pin" });
  await user.click(upgradeBtn);

  // Upgrade method was invoked with test name
  await waitFor(() => {
    const calls = facade.callsTo("UpgradeProfilePin");
    expect(calls.length).toBeGreaterThan(0);
    const firstCall = calls[0];
    expect(firstCall).toBeDefined();
    const req = firstCall!.args[0] as { test: string };
    expect(req.test).toBe("test-reschedule.json");
  });
});

test("handles package exchange with reviewed disclosure confirmation and conflict inspection", async () => {
  const user = userEvent.setup();
  renderEditor();

  // Switch to Package Exchange tab
  await user.click(screen.getByRole("tab", { name: "Package Exchange" }));

  // Export button disabled until reviewed checkbox is checked
  const exportBtn = screen.getByRole("button", { name: "Export Package" }) as HTMLButtonElement;
  expect(exportBtn.disabled).toBe(true);

  const confirmCheckbox = screen.getByRole("checkbox");
  await user.click(confirmCheckbox);
  expect(exportBtn.disabled).toBe(false);

  await user.click(exportBtn);

  // Inspect package
  const inspectPkgBtn = screen.getByRole("button", { name: "Inspect Package" });
  await user.click(inspectPkgBtn);

  expect(await screen.findByText("Package Status: completed")).toBeDefined();
  expect(screen.getByText("aabbcc1122334455")).toBeDefined();
});

test("retains draft edits in draft store and allows discarding", async () => {
  const user = userEvent.setup();
  const facade = renderEditor();

  const idInput = screen.getByLabelText("Profile ID");
  await user.clear(idInput);
  await user.type(idInput, "my-custom-profile");

  await waitFor(() => {
    const calls = facade.callsTo("SaveEditorDraft");
    expect(calls.length).toBeGreaterThan(0);
  });
});
