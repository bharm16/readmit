import { goTo } from "./testkit/navigation";
import { readCaseIdentity } from "./testkit/navigation";
// The project journey, driven as a person drives it: create a project,
// register an existing case of the workspace, choose the artifacts the
// actions apply to, and continue from what was retained — every step a real
// user event over the real components, with only the typed facade stubbed.
// What a project can hold and what a registration means is decided on the Go
// side; these tests prove the window reaches the shared operations and draws
// every outcome they can return.
import { expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {
  CASE_ENTRY,
  CASE_IDENTITY,
  OTHER_CASE_ENTRY,
  WORKSPACE_ROOT,
  caseResult,
  dialogDismissed,
  folderChosen,
  projectOverviewResult,
  refused,
  registeredCase,
} from "./testkit/fixtures";
import { renderApp } from "./testkit/app";

async function startProject(user: ReturnType<typeof userEvent.setup>) {
 await goTo(user, "Projects");
 await user.click(screen.getByRole("button", { name: "New project" }));
}
async function reopenProject(user: ReturnType<typeof userEvent.setup>) {
 await goTo(user, "Projects");
 await user.click(screen.getByRole("button", { name: "Open…" }));
}

async function openPlainWorkspace(
  user: ReturnType<typeof userEvent.setup>,
  withProject = false,
) {
  const { facade } = await renderApp({
    OpenWorkspace: () => folderChosen(WORKSPACE_ROOT, [
      { name: "project.json", kind: "project", schema: "readmit-project/v1" },
      { name: CASE_ENTRY, kind: "case", schema: "readmit-case/v3", provenance: "generated" },
    ]),
    OpenProjectOverview: () => projectOverviewResult([]),
    SelectWorkspace: () =>
      folderChosen(WORKSPACE_ROOT, [
        { name: CASE_ENTRY, kind: "case", schema: "readmit-case/v3", provenance: "generated" },
        ...(withProject ? [{ name: "project.json", kind: "project" as const, schema: "readmit-project/v1" }] : []),
      ]),
  });
  // The commands region and the first-run surface both offer the workspace
  // opening under its shared reviewed name; either opens the same flow.
  await user.click(screen.getAllByRole("button", { name: "Open…" })[0] as HTMLElement);
  await within(screen.getByRole("region", { name: "Navigation" })).findByRole("button", { name: /^Project: / });
  return { facade };
}

test("a project is created, a case is registered from the listing, and the investigation continues from it", async () => {
  const user = userEvent.setup();
  const { facade } = await openPlainWorkspace(user);

  // Start: no project is open, and the window says what to do next.
  const evidence = document.body;
  expect(screen.getByRole("button", { name: `Open case ${CASE_ENTRY}` })).toBeTruthy();

  // Create the project: the form asks for what the project document holds,
  // and the folder that holds it is chosen in the host's own dialog.
  facade.reply({
    CreateProject: (name, title, owner, versions) => {
      expect(name).toBe("scheduling-investigation");
      expect(versions).toEqual(["siu-2.5.1-v1"]);
      expect(title).toBe("Epic scheduling interface");
      expect(owner).toBe("");
      return projectOverviewResult([]);
    },
  });
  await startProject(user);
  await user.type(within(evidence).getByLabelText("Name"), "scheduling-investigation");
  await user.type(within(evidence).getByLabelText("Title", { selector: "#new-project-title" }), "Epic scheduling interface");
  await user.type(within(evidence).getByLabelText("Interface versions"), "siu-2.5.1-v1");
  await user.click(within(evidence).getByRole("button", { name: "Choose location and create…" }));
  expect(facade.oneCall("CreateProject")).toBeTruthy();
  expect(await screen.findByRole("button", { name: `Add ${CASE_ENTRY} to the project` })).toBeTruthy();

  // Register an existing case: the picker offers the case bundles the
  // workspace lists, and the registration carries what the person records.
  facade.reply({
    RegisterCase: (root, name, registration) => {
      expect(root).toBe(WORKSPACE_ROOT);
      expect(name).toBe(CASE_ENTRY);
      expect(registration.title).toBe("Duplicate appointment");
      expect(registration.tags).toEqual(["scheduling"]);
      return projectOverviewResult([registeredCase()]);
    },
  });
  await user.click(screen.getByRole("button", { name: `Add ${CASE_ENTRY} to the project` }));
  await user.clear(within(evidence).getByLabelText("Title", { selector: "#register-title" }));
  await user.type(within(evidence).getByLabelText("Title", { selector: "#register-title" }), "Duplicate appointment");
  await user.type(within(evidence).getByLabelText("Tags", { selector: "#register-tags" }), "scheduling");
  await user.click(within(evidence).getByRole("button", { name: "Add to project" }));
  expect(
    await within(evidence).findByText("Duplicate appointment after reschedule"),
  ).toBeTruthy();
  expect(screen.getByRole("button", { name: `Open case ${CASE_ENTRY}` })).toBeTruthy();

  // Continue: opening the registered case verifies it and names the next
  // action the investigation continues from.
  facade.reply({ OpenCase: () => caseResult() });
  await user.click(within(evidence).getByRole("button", { name: /^Open case(?: |$)/ }));
  expect(facade.oneCall("OpenCase")).toEqual([WORKSPACE_ROOT, CASE_ENTRY]);
  expect(await readCaseIdentity(user, CASE_IDENTITY)).toBeTruthy();
  expect(screen.getByRole("tab", { name: "Messages", selected: true })).toBeTruthy();
  // The project stays named in its switcher, and the case page has its way back.
  expect(screen.getByRole("button", { name: "Project: Scheduling investigation" })).toBeTruthy();
  expect(within(evidence).getByRole("button", { name: "Back to cases" })).toBeTruthy();
  expect(screen.getByRole("heading", { level: 1, name: /Duplicate appointment/ })).toBeTruthy();
});

/** The new project's own folder inside the open workspace, the way the
 * facade creates it, and its overview read from there. */
const CREATED = `${WORKSPACE_ROOT}/scheduling-investigation`;
function createdOverview() {
  const result = projectOverviewResult([]);
  return { ...result, overview: { ...result.overview!, root: CREATED } };
}

test("a project created in a folder inside the workspace becomes the open workspace", async () => {
  const user = userEvent.setup();
  const { facade } = await openPlainWorkspace(user);
  facade.reply({
    CreateProject: () => createdOverview(),
    OpenWorkspace: () =>
      folderChosen(CREATED, [{ name: "project.json", kind: "project", schema: "readmit-project/v1" }]),
    OpenProjectOverview: () => createdOverview(),
  });
  const evidence = screen;
  await startProject(user);
  await user.type(evidence.getByLabelText("Name"), "scheduling-investigation");
  await user.type(evidence.getByLabelText("Interface versions"), "siu-2.5.1-v1");
  await user.click(evidence.getByRole("button", { name: "Choose location and create…" }));
  // The window opens the new project's own folder and reads the project there,
  // so registering and importing act on it rather than the folder around it.
  expect(await within(screen.getByRole("region", { name: "Navigation" })).findByRole("button", { name: /^Project: / })).toBeTruthy();
  expect(facade.oneCall("OpenWorkspace")).toEqual([CREATED]);
  await waitFor(() =>
    expect(facade.callsTo("OpenProjectOverview").at(-1)).toEqual({ method: "OpenProjectOverview", args: [CREATED] }),
  );
  expect(await evidence.findByText("No cases yet")).toBeTruthy();
});

test("a new project folder that cannot be opened leaves no project open over the folder around it", async () => {
  const user = userEvent.setup();
  const { facade } = await openPlainWorkspace(user);
  facade.reply({
    CreateProject: () => createdOverview(),
    OpenWorkspace: () => refused("this account cannot read that folder"),
  });
  const evidence = screen;
  await startProject(user);
  await user.type(evidence.getByLabelText("Name"), "scheduling-investigation");
  await user.type(evidence.getByLabelText("Interface versions"), "siu-2.5.1-v1");
  await user.click(evidence.getByRole("button", { name: "Choose location and create…" }));
  expect(await screen.findAllByText("this account cannot read that folder")).toBeTruthy();
  // The workspace that was open stays open, and no project is shown over it.
  expect(within(screen.getByRole("region", { name: "Navigation" })).getByRole("button", { name: /^Project: / })).toBeTruthy();
  expect(evidence.queryByRole("button", { name: "Add to project" })).toBeNull();
  expect(facade.callsTo("OpenProjectOverview")).toHaveLength(0);
});

test("a dismissed folder dialog is a cancelled create that opened nothing", async () => {
  const user = userEvent.setup();
  const { facade } = await openPlainWorkspace(user);
  facade.reply({ CreateProject: () => dialogDismissed });
  const evidence = document.body;
  await startProject(user);
  await user.type(within(evidence).getByLabelText("Name"), "scheduling-investigation");
  await user.type(within(evidence).getByLabelText("Interface versions"), "siu-2.5.1-v1");
  await user.click(within(evidence).getByRole("button", { name: "Choose location and create…" }));
  expect(await within(evidence).findByText("The project was not created.")).toBeTruthy();
  expect(facade.callsTo("OpenCase")).toHaveLength(0);
});

test("a folder this account cannot create in is reported as denied, in words and shape", async () => {
  const user = userEvent.setup();
  const { facade } = await openPlainWorkspace(user);
  facade.reply({
    CreateProject: () => ({ state: "permission_denied" as const, reason: "this account cannot create a folder in the chosen folder" }),
  });
  const evidence = document.body;
  await startProject(user);
  await user.type(within(evidence).getByLabelText("Name"), "scheduling-investigation");
  await user.type(within(evidence).getByLabelText("Interface versions"), "siu-2.5.1-v1");
  await user.click(within(evidence).getByRole("button", { name: "Choose location and create…" }));
  expect(await within(evidence).findByText("Not created")).toBeTruthy();
  expect(
    within(evidence).getAllByText("this account cannot create a folder in the chosen folder"),
  ).toBeTruthy();
});

test("a project document this release cannot read is reported and changes nothing", async () => {
  const user = userEvent.setup();
  const { facade } = await openPlainWorkspace(user, true);
  facade.reply({
    OpenProjectOverview: () =>
      refused("the project document was written by a version this release cannot read"),
  });
  const evidence = document.body;
  await reopenProject(user);
  expect(
    await within(evidence).findByText(
      "the project document was written by a version this release cannot read",
    ),
  ).toBeTruthy();
  expect(facade.callsTo("RegisterCase")).toHaveLength(0);
  expect(facade.callsTo("UpdateProjectSettings")).toHaveLength(0);
});

test("a registration the project refuses reports the refusal and registers nothing else", async () => {
  const user = userEvent.setup();
  const { facade } = await openPlainWorkspace(user, true);
  facade.reply({
    OpenProjectOverview: () => projectOverviewResult([]),
    RegisterCase: () => refused("that name is already registered in this project"),
  });
  const evidence = document.body;
  await reopenProject(user);
  await user.click(await screen.findByRole("button", { name: `Add ${CASE_ENTRY} to the project` }));
  await user.clear(within(evidence).getByLabelText("Title", { selector: "#register-title" }));
  await user.type(within(evidence).getByLabelText("Title", { selector: "#register-title" }), "Duplicate appointment");
  await user.click(within(evidence).getByRole("button", { name: "Add to project" }));
  expect(
    await within(evidence).findByText("that name is already registered in this project"),
  ).toBeTruthy();
  expect(facade.callsTo("RegisterCase")).toHaveLength(1);
  // The refusal carries no overview; the project stays on the screen beside it.
  expect(screen.getByRole("button", { name: "Project: Scheduling investigation" })).toBeTruthy();
});

test("a refused settings change keeps the project and its controls on screen beside the refusal", async () => {
  const user = userEvent.setup();
  const { facade } = await openPlainWorkspace(user, true);
  facade.reply({
    OpenProjectOverview: () => projectOverviewResult([]),
    UpdateProjectSettings: () => ({ state: "permission_denied" as const, reason: "this device released its entitlement activation" }),
  });
  const evidence = screen;
  await reopenProject(user);
  expect(await screen.findByRole("button", { name: "Project: Scheduling investigation" })).toBeTruthy();
  await user.click(evidence.getByRole("button", { name: "Edit…" }));
  await user.clear(evidence.getByLabelText("Title", { selector: "#settings-title" }));
  await user.type(evidence.getByLabelText("Title", { selector: "#settings-title" }), "Renamed");
  await user.click(evidence.getByRole("button", { name: "Save" }));
  expect(await evidence.findAllByText("this device released its entitlement activation")).toBeTruthy();
  // The refusal carries no overview; the project the window last read stays,
  // with the controls that act on it.
  expect(screen.getByRole("button", { name: "Project: Scheduling investigation" })).toBeTruthy();
  expect(evidence.getByRole("dialog", { name: "Project settings" })).toBeTruthy();
  expect((evidence.getByLabelText("Title", { selector: "#settings-title" }) as HTMLInputElement).value).toBe("Renamed");
  expect(facade.callsTo("UpdateProjectSettings")).toHaveLength(1);
});

test("the editable document shows each note's text and each revision's lineage with the identity its parent was registered under", async () => {
  const user = userEvent.setup();
  const { facade } = await openPlainWorkspace(user, true);
  facade.reply({
    OpenProjectOverview: () => projectOverviewResult([registeredCase()]),
    OpenRevisions: () => ({
      state: "completed" as const,
      root: WORKSPACE_ROOT,
      revisions: {
        schema: "readmit-revisions/v1",
        notes: [
          { name: "handover", title: "Handover checklist", body: "Confirm the reschedule reaches the ledger once." },
          { name: "about-the-case", subject: CASE_ENTRY, title: "Why it doubled", body: "" },
        ],
        revisions: [
          {
            name: "reduced-case",
            identity: "revision-identity-fixed-for-tests",
            schema: "readmit-case/v3",
            provenance: "derived",
            operation: { name: "readmit-reproducer/v1", parent: CASE_ENTRY, parent_identity: CASE_IDENTITY },
          },
        ],
      },
    }),
  });
  const evidence = screen;
  await reopenProject(user);
  expect(await screen.findByRole("button", { name: "Project: Scheduling investigation" })).toBeTruthy();
  expect(facade.callsTo("OpenRevisions")).toHaveLength(0);

  await user.click(evidence.getByText("Project document"));
  const recorded = within(await evidence.findByRole("region", { name: "Editable project document" }));
  expect(await recorded.findByText("2 notes")).toBeTruthy();
  expect(recorded.getByText("Project draft")).toBeTruthy();
  expect(recorded.getByText(`About ${CASE_ENTRY}`)).toBeTruthy();
  expect(recorded.getByText("Confirm the reschedule reaches the ledger once.")).toBeTruthy();
  expect(recorded.getByText("1 revision")).toBeTruthy();
  expect(recorded.getByText(`Reproducer of ${CASE_ENTRY}`)).toBeTruthy();
  expect(recorded.getByText("revision-identity-fixed-for-tests")).toBeTruthy();
  expect(recorded.getByText(`parent ${CASE_IDENTITY}`)).toBeTruthy();
  expect(facade.oneCall("OpenRevisions")).toEqual([WORKSPACE_ROOT]);

  // Closing it and asking again reads it again; a refusal is shown as the
  // refusal and nothing from the earlier read stands in for it.
  await user.click(evidence.getByText("Project document"));
  facade.reply({ OpenRevisions: () => ({ state: "permission_denied" as const, reason: "this account cannot open the chosen folder" }) });
  await user.click(evidence.getByText("Project document"));
  const denied = within(await evidence.findByRole("region", { name: "Editable project document" }));
  expect(await denied.findByText("this account cannot open the chosen folder")).toBeTruthy();
  expect(denied.queryByText("Handover checklist")).toBeNull();
  expect(facade.callsTo("OpenRevisions")).toHaveLength(2);
});

test("search results open the artifact they matched, not only its name", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    SelectWorkspace: () =>
      folderChosen(WORKSPACE_ROOT, [
        { name: CASE_ENTRY, kind: "case", schema: "readmit-case/v3", provenance: "generated" },
        { name: OTHER_CASE_ENTRY, kind: "case", schema: "readmit-case/v3", provenance: "imported" },
      ]),
    Search: () => ({
      state: "completed" as const,
      matches: [
        { kind: "artifact" as const, name: OTHER_CASE_ENTRY, label: OTHER_CASE_ENTRY, field: "name", region: "navigation" as const },
      ],
    }),
    OpenCase: () => caseResult(OTHER_CASE_ENTRY),
  });
  await user.click(screen.getAllByRole("button", { name: "Open…" })[0] as HTMLElement);
  await within(screen.getByRole("region", { name: "Navigation" })).findByRole("button", { name: /^Project: / });
  await user.keyboard("{Control>}f{/Control}");
  await user.type(screen.getByRole("searchbox", { name: "Search this project" }), "other{Enter}");
  await user.click(within(await screen.findByRole("list", { name: "Search results" })).getByRole("button", { name: /other-case/ }));
  // Opening the result verified and opened the matched case, carrying its
  // counts into the inspector.
  expect(facade.oneCall("OpenCase")).toEqual([WORKSPACE_ROOT, OTHER_CASE_ENTRY]);
  expect(await readCaseIdentity(user, CASE_IDENTITY)).toBeTruthy();
});

test("a second navigation started while one runs starts nothing else, so no stale result can land under another case", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({
    SelectWorkspace: () =>
      folderChosen(WORKSPACE_ROOT, [
        { name: CASE_ENTRY, kind: "case", schema: "readmit-case/v3", provenance: "generated" },
        { name: OTHER_CASE_ENTRY, kind: "case", schema: "readmit-case/v3", provenance: "imported" },
      ]),
    Search: () => ({
      state: "completed" as const,
      matches: [
        { kind: "artifact" as const, name: CASE_ENTRY, label: CASE_ENTRY, field: "name", region: "navigation" as const },
        { kind: "artifact" as const, name: OTHER_CASE_ENTRY, label: OTHER_CASE_ENTRY, field: "name", region: "navigation" as const },
      ],
    }),
  });
  await user.click(screen.getAllByRole("button", { name: "Open…" })[0] as HTMLElement);
  await within(screen.getByRole("region", { name: "Navigation" })).findByRole("button", { name: /^Project: / });
  await user.keyboard("{Control>}f{/Control}");
  await user.type(screen.getByRole("searchbox", { name: "Search this project" }), "case{Enter}");
  const results = within(await screen.findByRole("list", { name: "Search results" })).getAllByRole("button");
  // The first open parks mid-verification; the second result is offered but
  // the window starts nothing else while an operation runs.
  const parked = facade.park("OpenCase");
  await user.click(results[0]!);
  await user.click(results[1]!);
  expect(facade.callsTo("OpenCase")).toHaveLength(1);
  parked.resolve(caseResult(CASE_ENTRY));
  expect(await readCaseIdentity(user, CASE_IDENTITY)).toBeTruthy();
  // The one case that ran is the one that renders.
  expect(facade.callsTo("OpenCase")[0]?.args[1]).toBe(CASE_ENTRY);
});

test("case details edited in the project reach the shared operation and only the changed members", async () => {
  const user = userEvent.setup();
  const { facade } = await openPlainWorkspace(user, true);
  facade.reply({
    OpenProjectOverview: () => projectOverviewResult([registeredCase()]),
    UpdateRegisteredCase: (root, name, change) => {
      expect(root).toBe(WORKSPACE_ROOT);
      expect(name).toBe(CASE_ENTRY);
      expect(change.status).toBe("investigating");
      expect(change.title).toBeUndefined();
      expect(change.tags).toBeUndefined();
      return projectOverviewResult([{ ...registeredCase(), status: "investigating" }]);
    },
  });
  const evidence = document.body;
  await reopenProject(user);
  await user.click(
    await within(evidence).findByRole("button", { name: /^Edit Duplicate appointment/ }),
  );
  await user.selectOptions(
    within(evidence).getByLabelText("Status", { selector: "#case-status" }),
    "investigating",
  );
  await user.click(within(evidence).getByRole("button", { name: "Save" }));
  // The refreshed overview is what renders, so the edit shows in the status
  // badge the project now reports.
  expect(
    await within(evidence).findAllByText("Investigating"),
  ).not.toHaveLength(0);
  expect(facade.oneCall("UpdateRegisteredCase")).toBeTruthy();
});

test("the project forms are reachable and operable with the keyboard alone", async () => {
  const user = userEvent.setup();
  const { facade } = await openPlainWorkspace(user);
  facade.reply({ CreateProject: (name) => {
    expect(name).toBe("project-from-keys");
    return projectOverviewResult([]);
  } });
  const evidence = document.body;
  await startProject(user);
  const name = within(evidence).getByLabelText("Name");
  name.focus();
  await user.keyboard("project-from-keys");
  await user.type(within(evidence).getByLabelText("Interface versions"), "siu-2.5.1-v1");
  await user.keyboard("{Enter}");
  expect(facade.oneCall("CreateProject")).toBeTruthy();
  expect(await screen.findByRole("button", { name: `Add ${CASE_ENTRY} to the project` })).toBeTruthy();
});
