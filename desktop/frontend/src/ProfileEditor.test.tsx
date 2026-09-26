import { expect, test } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ProfileEditor } from "./ProfileEditor";
import { installFacade } from "./testkit/wails";
import { localProfileFixture, profilePackFixture, WORKSPACE_ROOT } from "./testkit/fixtures";
import type { EditorDraft, LocalProfile, LocalProfileResult, ProfileCompareResult, ProfilePackageResult } from "./bindings";
import type { FacadeHandlers } from "./testkit/wails";

/** The digest of the later compared profile's seal, as the comparison decides it. */
const LATER_DIGEST = "b".repeat(64);

/** A completed comparison of versions 1 and 2 that finds one test affected
 * and decides the pin it may move to. */
function compareFixture(): ProfileCompareResult {
  return {
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
    later_pin: { pin: { id: "fixture-local-siu", version: "2", sha256: LATER_DIGEST } },
  };
}

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
    CompareProfiles: () => compareFixture(),
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
  expect(screen.getByRole("columnheader", { name: "Lossless parsing" })).toBeDefined();
  expect(screen.getByRole("columnheader", { name: "Field labels" })).toBeDefined();
  expect(screen.getByRole("columnheader", { name: "Structure validation" })).toBeDefined();
  expect(screen.getByRole("columnheader", { name: "Workflow evaluation" })).toBeDefined();

  // Opens library
  const openLibBtn = screen.getByRole("button", { name: "Open Library" });
  await user.click(openLibBtn);
  expect((await screen.findAllByText("2.5.1")).length).toBeGreaterThan(0);
  expect(screen.getByText("fixture-siu v1")).toBeDefined();
});

test("renders the segment list with a field-detail editor, captions each usage by its meaning and adds a Z-segment from an inline row", async () => {
  const user = userEvent.setup();
  const facade = renderEditor();

  // Shows Profile Editor tab by default, with the exact pin grouped once.
  expect(screen.getByLabelText("Profile ID")).toBeDefined();
  const pinned = within(screen.getByRole("group", { name: "Pinned metadata pack" }));
  expect((pinned.getByLabelText("Pack ID") as HTMLInputElement).value).toBe("fixture-siu");
  expect((pinned.getByLabelText("Pack version") as HTMLInputElement).value).toBe("1");
  expect((screen.getByLabelText("HL7 version") as HTMLSelectElement).value).toBe("2.5.1");
  expect((screen.getByLabelText("Message family") as HTMLSelectElement).value).toBe("SIU");

  // The overview keeps the selector, position, field name, usage and rule
  // origin; nothing has been resolved yet, so no origin is claimed.
  const row = within(screen.getByTestId("field-row-SCH-1"));
  expect(row.getByText("SCH-1")).toBeDefined();
  expect(row.getByText("Placer appointment number")).toBeDefined();
  expect(row.getByText("R — Required")).toBeDefined();
  expect(row.getByText("Not resolved")).toBeDefined();
  expect(screen.getByRole("columnheader", { name: "Rule origin" })).toBeDefined();

  // The field's clauses are edited in its detail panel, reached from the keyboard.
  row.getByRole("button", { name: "Edit field SCH-1" }).focus();
  await user.keyboard("{Enter}");
  const detail = within(screen.getByRole("region", { name: "Field SCH-1" }));
  await waitFor(() => expect(document.activeElement).toBe(detail.getByRole("heading", { name: "Field SCH-1" })));
  expect((detail.getByLabelText("Field name") as HTMLInputElement).value).toBe("Placer appointment number");
  const usage = detail.getByLabelText("Usage") as HTMLSelectElement;
  expect(usage.value).toBe("R");
  expect([...usage.options].map((option) => option.textContent)).toEqual([
    "R — Required",
    "RE — Required or empty",
    "O — Optional",
    "C — Conditional",
    "X — Not supported",
  ]);
  expect(within(detail.getByLabelText("Data type")).getByRole("option", { name: "Not specified" })).toBeDefined();

  // Conditional usage offers its condition, labelled; other usages none.
  expect(detail.queryByRole("group", { name: "Condition" })).toBeNull();
  await user.selectOptions(usage, "C");
  const condition = within(detail.getByRole("group", { name: "Condition" }));
  expect([...(condition.getByLabelText("Operator") as HTMLSelectElement).options].map((option) => option.textContent)).toEqual([
    "Choose an operator",
    "Present",
    "Absent",
    "One of these values",
  ]);
  expect(condition.getByText(/Absent: the named position is omitted, empty or explicitly null\./)).toBeDefined();

  // A site-defined Z-segment is added from its own labelled row, not a prompt.
  const opener = screen.getByRole("button", { name: "Add segment" });
  await user.click(opener);
  const row2 = within(screen.getByRole("group", { name: "New segment" }));
  await waitFor(() => expect(document.activeElement).toBe(row2.getByLabelText("Segment ID")));
  expect(row2.getByText(/an ID beginning with Z is a site-defined Z-segment/)).toBeDefined();
  const commit = row2.getByRole("button", { name: "Add segment" }) as HTMLButtonElement;
  expect(commit.disabled).toBe(true);
  await user.type(row2.getByLabelText("Segment ID"), "zpd");
  await user.click(commit);
  const added = within(screen.getByRole("region", { name: "Segment ZPD" }));
  expect(added.getByText("Site-defined Z-segment")).toBeDefined();
  await waitFor(() => expect(document.activeElement).toBe(screen.getByRole("button", { name: "Add segment" })));

  // A field added inside the named segment opens in the detail panel with only
  // its position and usage: no name or type is invented for it.
  await user.click(added.getByRole("button", { name: "Add field to ZPD" }));
  expect(screen.getByRole("region", { name: "Field ZPD-1" })).toBeDefined();
  await waitFor(() => expect(facade.callsTo("SaveEditorDraft").length).toBeGreaterThan(0));
  const retained = JSON.parse((facade.callsTo("SaveEditorDraft").at(-1)!.args[0] as EditorDraft).content as string) as LocalProfile;
  expect(retained.segments[1]).toEqual({ id: "ZPD", cardinality: { min: 1, max: "1" }, fields: [{ position: 1, usage: "O" }] });
  expect(retained.segments[0]!.fields[0]).not.toHaveProperty("condition");
});

// #518 PR15: a condition is never fabricated. Choosing conditional usage
// leaves it for the person to author; the shared reader says what is missing
// at validation and save, and only what was typed is ever sent.
test("choosing conditional usage leaves the condition for the person to author and never validates or saves an invented one", async () => {
  const user = userEvent.setup();
  const missing = "SCH-1 is conditional, so it declares the condition its presence depends on";
  const facade = renderEditor({
    ValidateProfile: () => ({ state: "failed", reason: missing }),
    SaveProfile: () => ({ state: "failed", reason: missing }),
  });
  await user.click(within(screen.getByTestId("field-row-SCH-1")).getByRole("button", { name: "Edit field SCH-1" }));
  const detail = within(screen.getByRole("region", { name: "Field SCH-1" }));
  await user.selectOptions(detail.getByLabelText("Usage"), "C");

  // Every clause of the condition is empty until the person authors it.
  const condition = within(detail.getByRole("group", { name: "Condition" }));
  expect((condition.getByLabelText("Operator") as HTMLSelectElement).value).toBe("");
  expect((condition.getByLabelText("Condition segment") as HTMLInputElement).value).toBe("");
  expect((condition.getByLabelText("Condition field position") as HTMLInputElement).value).toBe("");

  // Validation and save send the field with no condition, and say what is missing.
  await user.click(screen.getByRole("button", { name: "Validate" }));
  expect(await screen.findByRole("heading", { name: "Validation: failed" })).toBeTruthy();
  expect(screen.getByText(missing)).toBeTruthy();
  expect(lastDocument(facade, "ValidateProfile").segments[0]!.fields[0]).toEqual(
    expect.objectContaining({ position: 1, usage: "C" }),
  );
  expect(lastDocument(facade, "ValidateProfile").segments[0]!.fields[0]).not.toHaveProperty("condition");
  await user.click(screen.getByRole("button", { name: "Save revision" }));
  expect(await screen.findByRole("heading", { name: "Save result: failed" })).toBeTruthy();
  expect(screen.getByText(missing)).toBeTruthy();
  expect(lastDocument(facade, "SaveProfile").segments[0]!.fields[0]).not.toHaveProperty("condition");

  // A partly authored condition carries what was typed and leaves the rest
  // blank, for the shared reader to refuse.
  await user.type(condition.getByLabelText("Condition segment"), "SCH");
  const partial = { segment: "SCH", position: 0, operator: "" };
  expect(lastDocument(facade, "SaveEditorDraft").segments[0]!.fields[0]!.condition).toEqual(partial);
  expect((condition.getByLabelText("Condition field position") as HTMLInputElement).value).toBe("");
  expect((condition.getByLabelText("Operator") as HTMLSelectElement).value).toBe("");
  await user.click(screen.getByRole("button", { name: "Save revision" }));
  await waitFor(() => expect(facade.callsTo("SaveProfile")).toHaveLength(2));
  expect(lastDocument(facade, "SaveProfile").segments[0]!.fields[0]!.condition).toEqual(partial);

  // Once complete, the authored condition is what is saved.
  await user.type(condition.getByLabelText("Condition field position"), "7");
  await user.selectOptions(condition.getByLabelText("Operator"), "absent");
  facade.reply({ SaveProfile: () => localProfileFixture() });
  await user.click(screen.getByRole("button", { name: "Save revision" }));
  expect(await screen.findByRole("heading", { name: "Saved revision" })).toBeTruthy();
  expect(lastDocument(facade, "SaveProfile").segments[0]!.fields[0]!.condition).toEqual({
    segment: "SCH",
    position: 7,
    operator: "absent",
  });
});

/** A profile with a conditional value list, optional and unbounded
 * repetitions, each kind of rule and a site-defined Z-segment: the shipped
 * local-profile fixture's shape. */
function richProfile(): LocalProfile {
  return {
    schema: "readmit-local-profile/v1",
    profile: { id: "fixture-local-siu", version: "1" },
    base: { pack: { id: "fixture-siu", version: "1" }, hl7_version: "2.5.1", family: "SIU" },
    terminology: [
      {
        id: "local-visit-reason",
        description: "The reason codes this site sends.",
        binding: "required",
        codes: [
          { code: "ROUTINE", display: "Routine appointment" },
          { code: "URGENT" },
        ],
      },
      { id: "local-urgency", binding: "suggested", codes: [{ code: "HIGH" }] },
    ],
    authorities: [
      { id: "local-mrn-authority", namespace: "FIXTURECARE", universal_id: "2.16.840.1.113883.3.72", universal_id_type: "ISO" },
      { id: "local-visit-authority", namespace: "FIXTUREVISIT" },
    ],
    dates: [
      { id: "appointment-instant", description: "To the minute, with an offset.", precision: "minute", timezone: "required" },
      { id: "birth-date", precision: "day", timezone: "forbidden" },
    ],
    segments: [
      {
        id: "SCH",
        description: "The scheduling segment.",
        cardinality: { min: 1, max: "1" },
        fields: [
          { position: 1, name: "Placer appointment number", usage: "R", cardinality: { min: 1, max: "1" }, type: "EI" },
          { position: 2, usage: "RE", type: "EI" },
          {
            position: 11,
            usage: "C",
            condition: { segment: "ZPD", position: 2, operator: "present" },
            type: "DTM",
            date: "appointment-instant",
          },
        ],
      },
      {
        id: "ZPD",
        description: "The site-defined patient extension.",
        cardinality: { min: 0, max: "*" },
        fields: [
          { position: 2, name: "Local medical record number", usage: "RE", type: "CX", authority: "local-mrn-authority" },
          {
            position: 3,
            name: "Local visit reason",
            usage: "C",
            condition: { segment: "SCH", position: 25, operator: "value_in", values: ["BOOKED", "RESCHEDULED"] },
            type: "IS",
            terminology: "local-visit-reason",
          },
          { position: 4, name: "Local coverage note", usage: "O", cardinality: { min: 0, max: "3" }, type: "ST" },
        ],
      },
    ],
  };
}

/** The editor over the rich profile, opened from the workspace. */
async function openRich(user: ReturnType<typeof userEvent.setup>, handlers: FacadeHandlers = {}) {
  const opened: LocalProfileResult = { ...localProfileFixture(), profile: richProfile(), document: JSON.stringify(richProfile(), null, 2) };
  delete opened.resolution;
  const facade = renderEditor({ OpenProfile: () => opened, ...handlers });
  const form = within(screen.getByRole("form", { name: "Open profile" }));
  await user.type(form.getByLabelText("Profile file"), "profile.json{Enter}");
  expect(await screen.findByRole("heading", { name: "Open profile.json: completed" })).toBeTruthy();
  return facade;
}

/** The profile the last retention or save carried. */
function lastDocument(facade: ReturnType<typeof renderEditor>, method: "SaveEditorDraft" | "SaveProfile" | "ValidateProfile"): LocalProfile {
  const call = facade.callsTo(method).at(-1)!;
  const argument = call.args[0] as { content?: unknown; document?: string };
  return JSON.parse((method === "SaveEditorDraft" ? argument.content : argument.document) as string) as LocalProfile;
}

test("an opened profile with conditional values, optional repetitions, every rule type and a Z-segment is edited through separate bindings and saved without losing an untouched clause", async () => {
  const user = userEvent.setup();
  const facade = await openRich(user);

  // Each rule collection has its own editor, with its declared IDs.
  const terminology = within(screen.getByRole("region", { name: "Terminology" }));
  const reason = within(terminology.getByRole("group", { name: "Terminology set local-visit-reason" }));
  expect((reason.getByLabelText("Binding") as HTMLSelectElement).value).toBe("required");
  expect([...(reason.getByLabelText("Binding") as HTMLSelectElement).options].map((option) => option.textContent)).toEqual(["Required", "Suggested"]);
  expect(reason.getAllByLabelText("Code").map((input) => (input as HTMLInputElement).value)).toEqual(["ROUTINE", "URGENT"]);
  expect(reason.getAllByLabelText("Display text (optional)").map((input) => (input as HTMLInputElement).value)).toEqual(["Routine appointment", ""]);
  const authorities = within(screen.getByRole("region", { name: "Authorities" }));
  const mrn = within(authorities.getByRole("group", { name: "Assigning authority local-mrn-authority" }));
  expect((mrn.getByLabelText("Universal ID type") as HTMLSelectElement).value).toBe("ISO");
  expect(authorities.getByText(/a universal ID is declared together with its type/)).toBeDefined();
  const dates = within(screen.getByRole("region", { name: "Date rules" }));
  const instant = within(dates.getByRole("group", { name: "Date rule appointment-instant" }));
  expect([...(instant.getByLabelText("Precision") as HTMLSelectElement).options].map((option) => option.textContent)).toEqual([
    "Year", "Month", "Day", "Hour", "Minute", "Second", "Fraction",
  ]);
  expect([...(instant.getByLabelText("Time zone") as HTMLSelectElement).options].map((option) => option.textContent)).toEqual([
    "Required", "Optional", "Forbidden",
  ]);

  // The segment's unbounded repetitions and the value list are shown as they are.
  const zpd = within(screen.getByRole("region", { name: "Segment ZPD" }));
  const segmentRepetitions = within(zpd.getByRole("group", { name: "Repetitions" }));
  expect((segmentRepetitions.getByLabelText("Unbounded") as HTMLInputElement).checked).toBe(true);
  expect((segmentRepetitions.getByLabelText("Maximum") as HTMLInputElement).disabled).toBe(true);
  await user.click(zpd.getByRole("button", { name: "Edit field ZPD-3" }));
  let detail = within(screen.getByRole("region", { name: "Field ZPD-3" }));
  const values = within(detail.getByRole("group", { name: "Allowed values" }));
  expect(values.getAllByRole("textbox").map((input) => (input as HTMLInputElement).value)).toEqual(["BOOKED", "RESCHEDULED"]);

  // Each binding writes only its own member.
  expect((detail.getByLabelText("Terminology set") as HTMLSelectElement).value).toBe("local-visit-reason");
  await user.selectOptions(detail.getByLabelText("Terminology set"), "local-urgency");
  await user.selectOptions(detail.getByLabelText("Assigning authority"), "local-visit-authority");
  await user.selectOptions(detail.getByLabelText("Date rule"), "birth-date");
  let edited = lastDocument(facade, "SaveEditorDraft").segments[1]!.fields[1]!;
  expect(edited.terminology).toBe("local-urgency");
  expect(edited.authority).toBe("local-visit-authority");
  expect(edited.date).toBe("birth-date");
  await user.selectOptions(detail.getByLabelText("Assigning authority"), "None");
  await user.selectOptions(detail.getByLabelText("Date rule"), "None");
  edited = lastDocument(facade, "SaveEditorDraft").segments[1]!.fields[1]!;
  expect(edited).not.toHaveProperty("authority");
  expect(edited).not.toHaveProperty("date");
  expect(edited.terminology).toBe("local-urgency");

  // The field of another segment keeps its own authority binding.
  await user.click(zpd.getByRole("button", { name: "Edit field ZPD-2" }));
  detail = within(screen.getByRole("region", { name: "Field ZPD-2" }));
  expect((detail.getByLabelText("Assigning authority") as HTMLSelectElement).value).toBe("local-mrn-authority");
  expect((detail.getByLabelText("Terminology set") as HTMLSelectElement).value).toBe("");

  // Saving writes the document with only those three members changed.
  await user.click(screen.getByRole("button", { name: "Save revision" }));
  expect(await screen.findByRole("heading", { name: "Saved revision" })).toBeTruthy();
  const expected = richProfile();
  expected.segments[1]!.fields[1] = { ...expected.segments[1]!.fields[1]!, terminology: "local-urgency" };
  delete expected.segments[1]!.fields[1]!.authority;
  expect(lastDocument(facade, "SaveProfile")).toEqual(expected);
  const save = facade.callsTo("SaveProfile").at(-1)!.args[0] as { output: string; seal_output: string };
  const saving = within(screen.getByRole("group", { name: "Save revision" }));
  expect(save.output).toBe((saving.getByLabelText("Profile file") as HTMLInputElement).value);
  expect(save.seal_output).toBe((saving.getByLabelText("Version seal file") as HTMLInputElement).value);
  expect(saving.getByText(/it is not an approval or a certificate/)).toBeDefined();
});

test("a rule in use cannot be removed or renamed silently: the fields that reference it are named and keep their binding", async () => {
  const user = userEvent.setup();
  const facade = await openRich(user);
  const terminology = within(screen.getByRole("region", { name: "Terminology" }));
  const reason = within(terminology.getByRole("group", { name: "Terminology set local-visit-reason" }));
  expect(reason.getByText(/^Used by ZPD-3\./)).toBeDefined();
  expect((reason.getByLabelText("Set ID") as HTMLInputElement).readOnly).toBe(true);

  await user.click(reason.getByRole("button", { name: "Remove set local-visit-reason" }));
  expect((await terminology.findByRole("alert")).textContent).toBe(
    "Terminology set local-visit-reason is used by ZPD-3. Choose another terminology set, or none, for those fields before removing it.",
  );
  expect(facade.callsTo("SaveEditorDraft")).toHaveLength(0);
  const dates = within(screen.getByRole("region", { name: "Date rules" }));
  await user.click(dates.getByRole("button", { name: "Remove date rule appointment-instant" }));
  expect((await dates.findByRole("alert")).textContent).toMatch(/^Date rule appointment-instant is used by SCH-11\./);

  // An unused rule is removed; a rule the person adds is written only with
  // what they chose, and codes are authored one at a time.
  await user.click(terminology.getByRole("button", { name: "Remove set local-urgency" }));
  expect(lastDocument(facade, "SaveEditorDraft").terminology!.map((set) => set.id)).toEqual(["local-visit-reason"]);
  await user.click(terminology.getByRole("button", { name: "Add set" }));
  const added = within(terminology.getByRole("group", { name: "Terminology set 2" }));
  await user.type(added.getByLabelText("Set ID"), "local-slot");
  const named = within(terminology.getByRole("group", { name: "Terminology set local-slot" }));
  await user.selectOptions(named.getByLabelText("Binding"), "suggested");
  await user.click(named.getByRole("button", { name: "Add code to local-slot" }));
  await user.type(named.getByLabelText("Code"), "AM");
  expect(lastDocument(facade, "SaveEditorDraft").terminology![1]).toEqual({ id: "local-slot", binding: "suggested", codes: [{ code: "AM" }] });
  await user.click(named.getByRole("button", { name: "Remove code AM from local-slot" }));
  expect(lastDocument(facade, "SaveEditorDraft").terminology![1]!.codes).toEqual([]);

  const authorities = within(screen.getByRole("region", { name: "Authorities" }));
  await user.click(authorities.getByRole("button", { name: "Add authority" }));
  const authority = within(authorities.getByRole("group", { name: "Assigning authority 3" }));
  await user.type(authority.getByLabelText("Authority ID"), "local-slot-authority");
  await user.type(authority.getByLabelText("Namespace (optional)"), "SLOTS");
  expect(lastDocument(facade, "SaveEditorDraft").authorities![2]).toEqual({ id: "local-slot-authority", namespace: "SLOTS" });
  await user.click(dates.getByRole("button", { name: "Add date rule" }));
  const rule = within(dates.getByRole("group", { name: "Date rule 3" }));
  await user.type(rule.getByLabelText("Rule ID"), "slot-day");
  const slot = within(dates.getByRole("group", { name: "Date rule slot-day" }));
  await user.selectOptions(slot.getByLabelText("Precision"), "Day");
  await user.selectOptions(slot.getByLabelText("Time zone"), "Optional");
  expect(lastDocument(facade, "SaveEditorDraft").dates![2]).toEqual({ id: "slot-day", precision: "day", timezone: "optional" });
});

test("allowed values, repetitions and positions are authored exactly, a duplicate position is left to the shared reader, and removing a segment with rules asks first", async () => {
  const user = userEvent.setup();
  const duplicate = "segment ZPD declares position 2 more than once";
  const facade = await openRich(user, { ValidateProfile: () => ({ state: "failed", reason: duplicate }) });

  // One of these values never inserts a placeholder value.
  const sch = within(screen.getByRole("region", { name: "Segment SCH" }));
  await user.click(sch.getByRole("button", { name: "Edit field SCH-11" }));
  let detail = within(screen.getByRole("region", { name: "Field SCH-11" }));
  await user.selectOptions(detail.getByLabelText("Operator"), "One of these values");
  expect(lastDocument(facade, "SaveEditorDraft").segments[0]!.fields[2]!.condition).toEqual({
    segment: "ZPD", position: 2, operator: "value_in", values: [],
  });
  const values = within(detail.getByRole("group", { name: "Allowed values" }));
  await user.click(values.getByRole("button", { name: "Add value" }));
  await user.type(values.getByLabelText("Value 1"), "EARLY");
  await user.click(values.getByRole("button", { name: "Add value" }));
  await user.type(values.getByLabelText("Value 2"), "LATE");
  await user.click(values.getByRole("button", { name: "Remove value 1" }));
  expect(lastDocument(facade, "SaveEditorDraft").segments[0]!.fields[2]!.condition!.values).toEqual(["LATE"]);

  // Repetitions: not specified leaves the member out; unbounded is "*"; neither is zero.
  const repetitions = within(detail.getByRole("group", { name: "Repetitions" }));
  expect((repetitions.getByLabelText("Not specified") as HTMLInputElement).checked).toBe(true);
  await user.click(repetitions.getByLabelText("Specified"));
  await user.click(repetitions.getByLabelText("Unbounded"));
  expect(lastDocument(facade, "SaveEditorDraft").segments[0]!.fields[2]!.cardinality).toEqual({ min: 0, max: "*" });
  await user.clear(repetitions.getByLabelText("Minimum"));
  await user.type(repetitions.getByLabelText("Minimum"), "2");
  expect(lastDocument(facade, "SaveEditorDraft").segments[0]!.fields[2]!.cardinality).toEqual({ min: 2, max: "*" });
  await user.click(repetitions.getByLabelText("Not specified"));
  expect(lastDocument(facade, "SaveEditorDraft").segments[0]!.fields[2]).not.toHaveProperty("cardinality");

  // Any position can be named without listing those before it; a repeated
  // one is sent as typed and refused by the shared reader.
  const zpd = within(screen.getByRole("region", { name: "Segment ZPD" }));
  await user.click(zpd.getByRole("button", { name: "Edit field ZPD-4" }));
  detail = within(screen.getByRole("region", { name: "Field ZPD-4" }));
  await user.clear(detail.getByLabelText("Position"));
  await user.type(detail.getByLabelText("Position"), "2");
  expect(screen.getByRole("region", { name: "Field ZPD-2" })).toBeDefined();
  expect(zpd.getAllByText("ZPD-2")).toHaveLength(2);
  await user.click(screen.getByRole("button", { name: "Validate" }));
  expect(await screen.findByRole("heading", { name: "Validation: failed" })).toBeTruthy();
  expect(screen.getByText(duplicate)).toBeTruthy();
  expect(lastDocument(facade, "ValidateProfile").segments[1]!.fields.map((field) => field.position)).toEqual([2, 3, 2]);

  // Removing a field names it, and focus lands on the segment's Add field.
  await user.click(zpd.getByRole("button", { name: "Remove field ZPD-3" }));
  await waitFor(() => expect(document.activeElement).toBe(zpd.getByRole("button", { name: "Add field to ZPD" })));
  expect(lastDocument(facade, "SaveEditorDraft").segments[1]!.fields.map((field) => field.position)).toEqual([2, 2]);

  // Removing a segment that holds rules asks first, and keeping it keeps it.
  await user.click(zpd.getByRole("button", { name: "Remove segment ZPD" }));
  const confirm = within(zpd.getByRole("alert"));
  expect(confirm.getByText(/^Remove segment ZPD and its 2 field rules from this draft\?/)).toBeDefined();
  expect(confirm.getByText(/original evidence is never changed/)).toBeDefined();
  await user.click(confirm.getByRole("button", { name: "Keep segment" }));
  expect(screen.getByRole("region", { name: "Segment ZPD" })).toBeDefined();
  await user.click(zpd.getByRole("button", { name: "Remove segment ZPD" }));
  await user.click(within(zpd.getByRole("alert")).getByRole("button", { name: "Confirm removal" }));
  expect(screen.queryByRole("region", { name: "Segment ZPD" })).toBeNull();
  expect(lastDocument(facade, "SaveEditorDraft").segments.map((segment) => segment.id)).toEqual(["SCH"]);
});

test("canonical JSON the structured editor cannot show keeps its text editable and withholds the controls instead of failing or overwriting it", async () => {
  const user = userEvent.setup();
  const facade = renderEditor();
  await user.click(screen.getByRole("tab", { name: "Canonical JSON" }));
  const raw = screen.getByLabelText("Raw Canonical JSON") as HTMLTextAreaElement;
  // Tagged with the schema, but without the members the controls read.
  const tagged = '{"schema": "readmit-local-profile/v1", "profile": {"id": "tagged"}}';
  await user.clear(raw);
  await user.click(raw);
  await user.paste(tagged);
  await user.click(screen.getByRole("tab", { name: "Profile Editor" }));
  expect(screen.getByRole("alert").textContent).toMatch(/^The canonical JSON is not a profile document these controls can show/);
  expect(screen.queryByLabelText("Profile ID")).toBeNull();
  expect(screen.queryByRole("region", { name: "Terminology" })).toBeNull();

  // Validating sends the text exactly as it stands.
  await user.click(screen.getByRole("button", { name: "Validate" }));
  await waitFor(() => expect(facade.callsTo("ValidateProfile")).toHaveLength(1));
  expect((facade.callsTo("ValidateProfile")[0]!.args[0] as { document: string }).document).toBe(tagged);
  await user.click(screen.getByRole("tab", { name: "Canonical JSON" }));
  expect((screen.getByLabelText("Raw Canonical JSON") as HTMLTextAreaElement).value).toBe(tagged);

  // Once it is a document the controls can show, they return with it.
  await user.clear(screen.getByLabelText("Raw Canonical JSON"));
  await user.click(screen.getByLabelText("Raw Canonical JSON"));
  await user.paste(JSON.stringify(richProfile()));
  await user.click(screen.getByRole("tab", { name: "Profile Editor" }));
  expect((screen.getByLabelText("Profile ID") as HTMLInputElement).value).toBe("fixture-local-siu");
});

test("validation, a saved revision and a refused save are each headed by what happened", async () => {
  const user = userEvent.setup();
  const reason = "cannot overwrite existing profile; approved profiles are immutable, save as a new revision";
  const facade = renderEditor();

  await user.click(screen.getByRole("button", { name: "Validate" }));
  expect(await screen.findByRole("heading", { name: "Validation: completed" })).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Save revision" }));
  expect(await screen.findByRole("heading", { name: "Saved revision" })).toBeTruthy();
  facade.reply({ SaveProfile: () => ({ state: "failed", reason }) });
  await user.click(screen.getByRole("button", { name: "Save revision" }));
  expect(await screen.findByRole("heading", { name: "Save result: failed" })).toBeTruthy();
  expect(screen.getByText(reason)).toBeTruthy();
  expect(screen.queryByRole("heading", { name: "Saved revision" })).toBeNull();

  // An edit withdraws the result: it described other content.
  await user.type(screen.getByLabelText("Profile ID"), "-next");
  expect(screen.queryByRole("heading", { name: "Save result: failed" })).toBeNull();
});

test("validates profile through Go backend and displays resolution findings and seal", async () => {
  const user = userEvent.setup();
  renderEditor();

  const validateBtn = screen.getByRole("button", { name: "Validate" });
  await user.click(validateBtn);

  expect(await screen.findByText("Validation: completed")).toBeDefined();
  expect(screen.getByText(/Sealed Version:/)).toBeDefined();
  expect(screen.getByText("e96a3350b728a78d063cf99afddeec3854d393682039ed4348fbc89057c55054")).toBeDefined();
});

test("validation shows the named pack reader's refusal without a seal or resolution", async () => {
  const user = userEvent.setup();
  const reason = "the profile pack must be one regular file of the open workspace";
  const facade = renderEditor();

  await user.click(screen.getByRole("button", { name: "Validate" }));
  expect(await screen.findByRole("heading", { name: "Validation: completed" })).toBeTruthy();
  facade.reply({ ValidateProfile: () => ({ state: "failed", reason }) });
  await user.click(screen.getByRole("button", { name: "Validate" }));
  expect(await screen.findByRole("heading", { name: "Validation: failed" })).toBeTruthy();
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

  const saveBtn = screen.getByRole("button", { name: "Save revision" });
  await user.click(saveBtn);

  expect(await screen.findByText(/approved profiles are immutable/)).toBeDefined();
});

const OTHER_DIGEST = "c".repeat(64);

// Updating a test pin uses the pin the comparison decided for the later
// compared profile, even after another profile was validated, and shows both
// exact identities before the action.
test("compares profile versions and explicitly upgrades pinned consumer", async () => {
  const user = userEvent.setup();
  const facade = renderEditor({
    // A different profile, most recently validated in the editor.
    ValidateProfile: () => ({ ...localProfileFixture(), seal: { schema: "readmit-profile-version/v1", profile: { id: "fixture-local-siu", version: "9" }, content: { bytes: 1, sha256: OTHER_DIGEST } } }),
  });
  await user.click(screen.getByRole("button", { name: "Validate" }));
  expect(await screen.findByRole("heading", { name: "Validation: completed" })).toBeTruthy();

  await user.click(screen.getByRole("tab", { name: "Versions and pins" }));
  await user.click(screen.getByRole("button", { name: "Compare" }));
  expect(await screen.findByText(/the description differs/)).toBeDefined();
  expect(
    screen.getByText("Earlier profile profile-v1.json (fixture-local-siu version 1) · Later profile profile-v2.json (fixture-local-siu version 2) · Test references file references.json"),
  ).toBeDefined();
  // The pin comes from the comparison itself; the window reads no profile to make one.
  expect(facade.callsTo("OpenProfile")).toHaveLength(0);
  for (const header of ["Test", "Case", "Pinned version", "SHA-256", "Impact"]) {
    expect(screen.getByRole("columnheader", { name: header })).toBeDefined();
  }
  const pinned = "e96a3350b728a78d063cf99afddeec3854d393682039ed4348fbc89057c55054";
  expect(screen.getAllByText(pinned, { exact: false }).length).toBeGreaterThan(0);

  const update = screen.getByRole("button", { name: "Update test pin" });
  expect(update.getAttribute("aria-describedby")).toBeTruthy();
  expect(document.getElementById(update.getAttribute("aria-describedby")!)!.textContent).toBe(
    `From fixture-local-siu version 1 · SHA-256 ${pinned} to fixture-local-siu version 2 · SHA-256 ${LATER_DIGEST}`,
  );
  await user.click(update);
  await waitFor(() => expect(facade.callsTo("UpgradeProfilePin")).toHaveLength(1));
  expect(facade.oneCall("UpgradeProfilePin")).toEqual([{
    workspace: WORKSPACE_ROOT,
    references: "references.json",
    test: "test-reschedule.json",
    was_pin: { id: "fixture-local-siu", version: "1", sha256: pinned },
    now_pin: { id: "fixture-local-siu", version: "2", sha256: LATER_DIGEST },
    output: "references.json",
  }]);
  const updated = within(await screen.findByRole("region", { name: "Test pin update" }));
  expect(updated.getByText("Update test pin: completed")).toBeDefined();
  expect(updated.getByText(/compare again to assess it\.$/)).toBeDefined();
  // The references changed, so the assessment is withdrawn and nothing re-runs.
  expect(screen.queryByRole("button", { name: "Update test pin" })).toBeNull();
  expect(facade.callsTo("CompareProfiles")).toHaveLength(1);
});

test("a comparison that refuses the later profile's pin blocks the pin update, and changing an input withdraws the comparison", async () => {
  const user = userEvent.setup();
  const refused: ProfileCompareResult = {
    ...compareFixture(),
    later_pin: { refusal: "the later profile is not a file of the open workspace, so no saved test can be pinned to it" },
  };
  let answer = refused;
  const facade = renderEditor({ CompareProfiles: () => answer });
  await user.click(screen.getByRole("tab", { name: "Versions and pins" }));

  await user.click(screen.getByRole("button", { name: "Compare" }));
  expect(
    await screen.findByText("the later profile is not a file of the open workspace, so no saved test can be pinned to it"),
  ).toBeDefined();
  expect(screen.queryByRole("button", { name: "Update test pin" })).toBeNull();

  answer = compareFixture();
  await user.click(screen.getByRole("button", { name: "Compare" }));
  expect(await screen.findByRole("button", { name: "Update test pin" })).toBeDefined();
  // A changed references file or compared profile withdraws result and action.
  await user.type(screen.getByLabelText("Test references file"), "x");
  expect(screen.queryByRole("button", { name: "Update test pin" })).toBeNull();
  expect(screen.queryByText(/the description differs/)).toBeNull();
  await user.click(screen.getByRole("button", { name: "Compare" }));
  expect(await screen.findByRole("button", { name: "Update test pin" })).toBeDefined();
  await user.type(screen.getByLabelText("Earlier profile"), "x");
  expect(screen.queryByRole("button", { name: "Update test pin" })).toBeNull();
  await user.click(screen.getByRole("button", { name: "Compare" }));
  expect(await screen.findByRole("button", { name: "Update test pin" })).toBeDefined();
  await user.type(screen.getByLabelText("Later profile"), "x");
  expect(screen.queryByRole("button", { name: "Update test pin" })).toBeNull();
  expect(facade.callsTo("UpgradeProfilePin")).toHaveLength(0);
});

test("handles package exchange with reviewed disclosure confirmation and conflict inspection", async () => {
  const user = userEvent.setup();
  renderEditor();

  // Switch to Package Exchange tab
  await user.click(screen.getByRole("tab", { name: "Package Exchange" }));

  // Export button disabled until reviewed checkbox is checked
  const exportBtn = screen.getByRole("button", { name: "Export Package" }) as HTMLButtonElement;
  expect(exportBtn.disabled).toBe(true);

  const confirmCheckbox = screen.getByRole("checkbox") as HTMLInputElement;
  await user.click(confirmCheckbox);
  expect(exportBtn.disabled).toBe(false);

  // The confirmation holds only for the files it was given for: changing any
  // export input withdraws it.
  const exporting = within(screen.getByRole("region", { name: "Export package" }));
  for (const label of ["Profile file", "Pack file", "Version seal file", "Origin file", "Package file"]) {
    await user.type(exporting.getByLabelText(label), "x");
    expect(confirmCheckbox.checked).toBe(false);
    expect(exportBtn.disabled).toBe(true);
    await user.click(confirmCheckbox);
  }
  expect(exportBtn.disabled).toBe(false);
  expect(screen.getByText("I confirm that metadata, licenses, and notices were reviewed for disclosure and external exchange.")).toBeDefined();

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
  await user.click(screen.getByRole("button", { name: "Discard changes" }));
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
  await user.click(screen.getByRole("button", { name: "Discard changes" }));
  expect(await screen.findByText(reason)).toBeTruthy();
  expect((screen.getByLabelText("Raw Canonical JSON") as HTMLTextAreaElement).value).toContain('"id": "local-siu-profilex"');
  expect(held).toHaveLength(1);

  refuse = false;
  await user.click(screen.getByRole("button", { name: "Discard changes" }));
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
  await user.click(screen.getByRole("button", { name: "Discard changes" }));
  const first = facade.oneCall("SaveEditorDraft")[0] as EditorDraft;
  held = [{ ...first, id: "profile-draft-1" }];
  retaining.resolve({ state: "completed", drafts: held });
  expect(await screen.findByText(reason)).toBeTruthy();
  expect(facade.callsTo("SaveEditorDraft")).toHaveLength(1);

  await user.click(screen.getByRole("button", { name: "Retry draft save" }));
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
  return within(screen.getByRole("form", { name: "Import package" }));
}

test("imports a package into a new directory from the keyboard and shows what it verified, its provenance and that nothing was activated", async () => {
  const user = userEvent.setup();
  const facade = renderEditor({ ImportProfilePackage: () => importedPackage() });
  const form = await exchange(user);

  await user.clear(form.getByLabelText("Package file"));
  await user.type(form.getByLabelText("Package file"), "interface-package.json");
  await user.clear(form.getByLabelText("Import folder"));
  await user.type(form.getByLabelText("Import folder"), "imported-interface{Enter}");

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

  await user.click(form.getByLabelText("Import folder"));
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
  const form = within(screen.getByRole("form", { name: "Open profile" }));

  await user.type(form.getByLabelText("Profile file"), "imported-interface/profile.json");
  await user.type(form.getByLabelText("Pack file (optional)"), "imported-interface/pack.json{Enter}");

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
  const form = within(screen.getByRole("form", { name: "Open profile" }));
  await user.type(form.getByLabelText("Profile file"), "profile.json");
  await user.type(form.getByLabelText("Pack file (optional)"), "adt-pack.json");
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
  const form = within(screen.getByRole("form", { name: "Open profile" }));
  await user.type(form.getByLabelText("Profile file"), "missing.json{Enter}");
  expect((await form.findByRole("alert")).textContent).toMatch(/^This editor holds unstored edits\./);
  expect(facade.callsTo("OpenProfile")).toHaveLength(0);
  expect((screen.getByLabelText("Profile ID") as HTMLInputElement).value).toBe("edited-profile");

  // Once the edits are discarded the open reaches the facade, and its refusal
  // leaves the editor as it was.
  await user.click(screen.getByRole("tab", { name: "Canonical JSON" }));
  await user.click(screen.getByRole("button", { name: "Discard changes" }));
  await user.click(screen.getByRole("tab", { name: "Profile Editor" }));
  const reopened = within(screen.getByRole("form", { name: "Open profile" }));
  // What was typed is still there.
  expect((reopened.getByLabelText("Profile file") as HTMLInputElement).value).toBe("missing.json");
  await user.click(reopened.getByRole("button", { name: "Open Profile" }));
  expect(await screen.findByRole("heading", { name: "Open missing.json: failed" })).toBeTruthy();
  expect(screen.getByText("the local profile must be one regular file of the open workspace")).toBeTruthy();
  expect(reopened.queryByRole("alert")).toBeNull();
  expect((screen.getByLabelText("Profile ID") as HTMLInputElement).value).toBe("local-siu-profile");
});
