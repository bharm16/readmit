import { expect, test } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ProfileEditor } from "./ProfileEditor";
import { installFacade } from "./testkit/wails";
import { localProfileFixture, profilePackFixture, WORKSPACE_ROOT } from "./testkit/fixtures";
import type { EditorDraft, ProfilePackageResult } from "./bindings";
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

test("validation shows the named pack reader's refusal without a seal or resolution", async () => {
  const user = userEvent.setup();
  const reason = "the profile pack must be one regular file of the open workspace";
  const facade = renderEditor();

  await user.click(screen.getByRole("button", { name: "Validate with Engine" }));
  expect(await screen.findByRole("heading", { name: "Validation Result: completed" })).toBeTruthy();
  facade.reply({ ValidateProfile: () => ({ state: "failed", reason }) });
  await user.click(screen.getByRole("button", { name: "Validate with Engine" }));
  expect(await screen.findByRole("heading", { name: "Validation Result: failed" })).toBeTruthy();
  expect(screen.getByText(reason)).toBeTruthy();
  expect(screen.queryByText(/Sealed Version:/)).toBeNull();
  expect(screen.queryByText(/Resolved against the pinned pack/)).toBeNull();
  expect(facade.callsTo("ValidateProfile").at(-1)?.args).toEqual([{
    workspace: WORKSPACE_ROOT,
    document: expect.any(String),
    pack: "profile-pack.json",
  }]);
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

test("discard waits for a delayed retention and cancels queued saves before clearing the draft", async () => {
  const user = userEvent.setup();
  let held: EditorDraft[] = [];
  const facade = renderEditor({
    DiscardEditorDraft: (id) => {
      held = held.filter((draft) => draft.id !== id);
      return { state: "completed", drafts: held };
    },
  });
  const retaining = facade.park("SaveEditorDraft");

  await user.type(screen.getByLabelText("Profile ID"), "-edited");
  expect(retaining.size).toBe(1);
  expect(facade.callsTo("SaveEditorDraft")).toHaveLength(1);
  await user.click(screen.getByRole("tab", { name: "Canonical JSON" }));
  await user.click(screen.getByRole("button", { name: "Discard Unstored Edits" }));
  expect(facade.callsTo("DiscardEditorDraft")).toHaveLength(0);

  const sent = facade.oneCall("SaveEditorDraft")[0] as EditorDraft;
  held = [{ ...sent, id: "profile-draft-1" }];
  retaining.resolve({ state: "completed", drafts: held });
  await waitFor(() => expect(facade.oneCall("DiscardEditorDraft")).toEqual(["profile-draft-1"]));
  expect(held).toEqual([]);
  expect(facade.callsTo("SaveEditorDraft")).toHaveLength(1);
  expect((screen.getByLabelText("Raw Canonical JSON") as HTMLTextAreaElement).value).toContain('"id": "local-siu-profile"');
});

test("a refused draft discard keeps the edits and their identity for a retry", async () => {
  const user = userEvent.setup();
  const reason = "the draft store could not be written";
  let refuse = true;
  let held: EditorDraft[] = [];
  const facade = renderEditor({
    SaveEditorDraft: (draft) => {
      held = [{ ...draft, id: "profile-draft-1" }];
      return { state: "completed", drafts: held };
    },
    DiscardEditorDraft: (id) => {
      if (refuse) return { state: "failed", reason, drafts: held };
      held = held.filter((draft) => draft.id !== id);
      return { state: "completed", drafts: held };
    },
  });

  await user.type(screen.getByLabelText("Profile ID"), "x");
  expect(await screen.findByText("Retained. It will come back if this window stops.")).toBeTruthy();
  await user.click(screen.getByRole("tab", { name: "Canonical JSON" }));
  await user.click(screen.getByRole("button", { name: "Discard Unstored Edits" }));
  expect(await screen.findByText(reason)).toBeTruthy();
  expect((screen.getByLabelText("Raw Canonical JSON") as HTMLTextAreaElement).value).toContain('"id": "local-siu-profilex"');
  expect(held).toHaveLength(1);

  refuse = false;
  await user.click(screen.getByRole("button", { name: "Discard Unstored Edits" }));
  await waitFor(() => expect(held).toEqual([]));
  expect(facade.callsTo("DiscardEditorDraft").map((call) => call.args[0])).toEqual([
    "profile-draft-1", "profile-draft-1",
  ]);
  expect((screen.getByLabelText("Raw Canonical JSON") as HTMLTextAreaElement).value).toContain('"id": "local-siu-profile"');
});

test("after a delayed save and refused discard, Retry retains the newest queued profile text", async () => {
  const user = userEvent.setup();
  const reason = "the draft store could not be written";
  let held: EditorDraft[] = [];
  const facade = renderEditor({
    DiscardEditorDraft: () => ({ state: "failed", reason, drafts: held }),
  });
  const retaining = facade.park("SaveEditorDraft");

  await user.type(screen.getByLabelText("Profile ID"), "xy");
  expect(retaining.size).toBe(1);
  await user.click(screen.getByRole("tab", { name: "Canonical JSON" }));
  await user.click(screen.getByRole("button", { name: "Discard Unstored Edits" }));
  const first = facade.oneCall("SaveEditorDraft")[0] as EditorDraft;
  held = [{ ...first, id: "profile-draft-1" }];
  retaining.resolve({ state: "completed", drafts: held });
  expect(await screen.findByText(reason)).toBeTruthy();
  expect(facade.callsTo("SaveEditorDraft")).toHaveLength(1);

  await user.click(screen.getByRole("button", { name: "Retain it again" }));
  await waitFor(() => expect(facade.callsTo("SaveEditorDraft")).toHaveLength(2));
  const retried = facade.callsTo("SaveEditorDraft")[1]?.args[0] as EditorDraft;
  expect(retried.id).toBe("profile-draft-1");
  expect(JSON.parse(retried.content as string).profile.id).toBe("local-siu-profilexy");
  retaining.resolve({ state: "completed", drafts: [{ ...retried, id: "profile-draft-1" }] });
  expect(await screen.findByText("Retained. It will come back if this window stops.")).toBeTruthy();
});

/** A completed import as the facade reports one: what the package carried,
 * read by the same readers the import wrote it with. */
function importedPackage(): ProfilePackageResult {
  return {
    state: "completed",
    output: "imported-interface",
    profile: { id: "fixture-local-siu", version: "1" },
    pack: { id: "fixture-siu", version: "1" },
    version: { id: "fixture-local-siu", version: "1" },
    seal: localProfileFixture().seal!,
    provenance: profilePackFixture().provenance!,
    origin: {
      schema: "readmit-profile-origin/v1",
      source_format: "readmit-local-profile/v1",
      source: "Fixture Source",
      revision: "1",
      license: "LicenseRef-readmit-fixture",
      notice: "Fixture notice text",
      mapping_limitations: "No external mapping",
      review_reference: "Ref-123",
    },
    sha256: "aabbcc1122334455",
    rights: "Ref-123",
    dependency: "fixture-siu 1",
  };
}

/** The import controls of the package exchange tab. */
async function exchange(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole("tab", { name: "Package Exchange" }));
  return within(screen.getByRole("form", { name: "Import profile package" }));
}

test("imports a package into a new directory from the keyboard and shows what it verified, its provenance and that nothing was activated", async () => {
  const user = userEvent.setup();
  const facade = renderEditor({ ImportProfilePackage: () => importedPackage() });
  const form = await exchange(user);

  await user.clear(form.getByLabelText("Package File:"));
  await user.type(form.getByLabelText("Package File:"), "interface-package.json");
  await user.clear(form.getByLabelText("Output Directory:"));
  await user.type(form.getByLabelText("Output Directory:"), "imported-interface{Enter}");

  const imported = within(await screen.findByRole("region", { name: "Package import" }));
  expect(imported.getByRole("heading", { name: "Imported into imported-interface" })).toBeTruthy();
  expect(facade.oneCall("ImportProfilePackage")).toEqual([
    { workspace: WORKSPACE_ROOT, package: "interface-package.json", output: "imported-interface" },
  ]);
  const fact = (term: string) => imported.getByText(term, { selector: "dt" }).nextElementSibling?.textContent;
  expect(fact("Profile")).toBe("fixture-local-siu v1");
  expect(fact("Pinned pack")).toBe("fixture-siu v1");
  expect(fact("Version seal")).toBe(
    "fixture-local-siu v1 · SHA-256 e96a3350b728a78d063cf99afddeec3854d393682039ed4348fbc89057c55054 · 3322 bytes",
  );
  expect(fact("Package SHA-256")).toBe("aabbcc1122334455");
  expect(fact("Source")).toBe("Fixture Source");
  expect(fact("Review reference")).toBe("Ref-123");
  expect(fact("Mapping limitations")).toBe("No external mapping");
  expect(fact("Pack source")).toBe("Fixture Author · testdata/fixtures/profile-pack.json @ 1");
  expect(fact("Rights review")).toBe("approved (testdata/README.md)");
  // The notice is disclosed on request.
  await user.click(imported.getByText("License notice"));
  expect(imported.getByText("Fixture notice text")).toBeTruthy();
  expect(imported.getByText(/^Nothing was activated: no project changed, no saved test was repinned/)).toBeTruthy();

  // The editor still holds what it held: importing opened nothing.
  await user.click(screen.getByRole("tab", { name: "Profile Editor" }));
  expect((screen.getByLabelText("Profile ID") as HTMLInputElement).value).toBe("local-siu-profile");
  expect(facade.callsTo("OpenProfile")).toHaveLength(0);
});

test("refuses tampered, unsupported and occupied imports in the engine's words and returns focus to the import", async () => {
  const user = userEvent.setup();
  const facade = renderEditor();
  const form = await exchange(user);
  for (const reason of [
    "profile package integrity check failed",
    "unsupported profile package version",
    "cannot create profile import directory; destination must be new and parent writable",
  ]) {
    facade.reply({ ImportProfilePackage: () => ({ state: "failed", reason }) });
    await user.click(form.getByRole("button", { name: "Import Package" }));
    const refused = within(await screen.findByRole("region", { name: "Package import" }));
    expect(await refused.findByText(reason)).toBeTruthy();
    expect(refused.getByRole("heading", { name: "Import refused" })).toBeTruthy();
    expect(refused.queryByText("Version seal")).toBeNull();
    expect(refused.queryByText(/Nothing was activated/)).toBeNull();
    await waitFor(() => expect(document.activeElement).toBe(form.getByRole("button", { name: "Import Package" })));
  }
  expect(facade.callsTo("ImportProfilePackage")).toHaveLength(3);
});

test("cancels an import in progress from the keyboard and says what the cancellation left", async () => {
  const user = userEvent.setup();
  const facade = renderEditor({ Cancel: () => undefined });
  const parked = facade.park("ImportProfilePackage");
  const form = await exchange(user);

  await user.click(form.getByLabelText("Output Directory:"));
  await user.keyboard("{Enter}");
  // A running import offers its cancel and moves focus to it.
  const cancelling = await form.findByRole("button", { name: "Cancel import" });
  await waitFor(() => expect(document.activeElement).toBe(cancelling));
  expect((form.getByRole("button", { name: "Import Package" }) as HTMLButtonElement).disabled).toBe(true);
  await user.keyboard("{Enter}");
  expect(facade.oneCall("Cancel")).toEqual(["profile-import"]);

  parked.resolve({ state: "cancelled", reason: "the import was cancelled before anything was written" });
  const cancelled = within(await screen.findByRole("region", { name: "Package import" }));
  expect(cancelled.getByRole("heading", { name: "Import cancelled" })).toBeTruthy();
  expect(cancelled.getByText("the import was cancelled before anything was written")).toBeTruthy();
  expect(form.queryByRole("button", { name: "Cancel import" })).toBeNull();
  await waitFor(() => expect(document.activeElement).toBe(form.getByRole("button", { name: "Import Package" })));
});

test("opens an existing local profile from the keyboard, resolves it against its pinned pack and shows its seal", async () => {
  const user = userEvent.setup();
  const opened = { ...localProfileFixture(), document: '{"schema":"readmit-local-profile/v1"}\n' };
  const facade = renderEditor({ OpenProfile: () => opened });
  const form = within(screen.getByRole("form", { name: "Open an existing profile" }));

  await user.type(form.getByLabelText("Profile entry"), "imported-interface/profile.json");
  await user.type(form.getByLabelText("Pack entry"), "imported-interface/pack.json{Enter}");

  expect(await screen.findByRole("heading", { name: "Open imported-interface/profile.json: completed" })).toBeTruthy();
  expect(facade.oneCall("OpenProfile")).toEqual([WORKSPACE_ROOT, "imported-interface/profile.json", "imported-interface/pack.json"]);
  expect(screen.getByText("e96a3350b728a78d063cf99afddeec3854d393682039ed4348fbc89057c55054")).toBeTruthy();
  expect(
    screen.getByText(
      "Resolved against the pinned pack fixture-siu v1: parse supported · labels supported · structural unsupported · workflow unsupported",
    ),
  ).toBeTruthy();
  // The opened profile is what the editor now edits, and its canonical
  // document is what the raw view holds.
  expect((screen.getByLabelText("Profile ID") as HTMLInputElement).value).toBe("fixture-local-siu");
  await waitFor(() => expect(document.activeElement).toBe(form.getByRole("button", { name: "Open Profile" })));
  await user.click(screen.getByRole("tab", { name: "Canonical JSON" }));
  expect((screen.getByLabelText("Raw Canonical JSON") as HTMLTextAreaElement).value).toBe(opened.document);
  // Opening is a read: nothing was retained or saved.
  expect(facade.callsTo("SaveEditorDraft")).toHaveLength(0);
  expect(facade.callsTo("SaveProfile")).toHaveLength(0);
});

test("an opened profile that no offered pack satisfies says nothing was read from one", async () => {
  const user = userEvent.setup();
  const unpinned = localProfileFixture();
  unpinned.resolution = {
    ...unpinned.resolution!,
    pinned: false,
    support: { parse: "unknown", labels: "unknown", structural: "unknown", workflow: "unknown" },
    findings: [{ kind: "pack_not_pinned", detail: "the pack offered is not fixture-siu 1; nothing was read from it" }],
  };
  renderEditor({ OpenProfile: () => unpinned });
  const form = within(screen.getByRole("form", { name: "Open an existing profile" }));
  await user.type(form.getByLabelText("Profile entry"), "profile.json");
  await user.type(form.getByLabelText("Pack entry"), "adt-pack.json");
  await user.click(form.getByRole("button", { name: "Open Profile" }));
  expect(
    await screen.findByText(
      "Not resolved against the pinned pack fixture-siu v1: no pack offered satisfies the pin, so nothing was read from one.",
    ),
  ).toBeTruthy();
  expect(screen.getByText(/the pack offered is not fixture-siu 1; nothing was read from it/)).toBeTruthy();
});

test("refuses to open over unstored edits and shows an open the reader refused without replacing the editor", async () => {
  const user = userEvent.setup();
  const facade = renderEditor({ OpenProfile: () => ({ state: "failed", reason: "the local profile must be one regular file of the open workspace" }) });

  // An edit is retained, so opening would replace it: the window says so and
  // opens nothing.
  await user.clear(screen.getByLabelText("Profile ID"));
  await user.type(screen.getByLabelText("Profile ID"), "edited-profile");
  await waitFor(() => expect(facade.callsTo("SaveEditorDraft").length).toBeGreaterThan(0));
  const form = within(screen.getByRole("form", { name: "Open an existing profile" }));
  await user.type(form.getByLabelText("Profile entry"), "missing.json{Enter}");
  expect((await form.findByRole("alert")).textContent).toMatch(/^This editor holds unstored edits\./);
  expect(facade.callsTo("OpenProfile")).toHaveLength(0);
  expect((screen.getByLabelText("Profile ID") as HTMLInputElement).value).toBe("edited-profile");

  // Once the edits are discarded the open reaches the facade, and its refusal
  // leaves the editor as it was.
  await user.click(screen.getByRole("tab", { name: "Canonical JSON" }));
  await user.click(screen.getByRole("button", { name: "Discard Unstored Edits" }));
  await user.click(screen.getByRole("tab", { name: "Profile Editor" }));
  const reopened = within(screen.getByRole("form", { name: "Open an existing profile" }));
  // What was typed is still there.
  expect((reopened.getByLabelText("Profile entry") as HTMLInputElement).value).toBe("missing.json");
  await user.click(reopened.getByRole("button", { name: "Open Profile" }));
  expect(await screen.findByRole("heading", { name: "Open missing.json: failed" })).toBeTruthy();
  expect(screen.getByText("the local profile must be one regular file of the open workspace")).toBeTruthy();
  expect(reopened.queryByRole("alert")).toBeNull();
  expect((screen.getByLabelText("Profile ID") as HTMLInputElement).value).toBe("local-siu-profile");
});
