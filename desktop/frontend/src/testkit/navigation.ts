// Moving around the window the way a person does: the sidebar's destinations
// and the views inside a page. Only the shown page is mounted, so a test goes
// to the page that holds a control before asking for it.
import { screen, waitFor, within } from "@testing-library/react";
import type { UserEvent } from "@testing-library/user-event";

/** The sidebar's destinations, as the window labels them. */
export type Destination =
  | "Projects"
  | "Cases"
  | "Captures"
  | "Messages"
  | "Tests"
  | "Test cases"
  | "Runs"
  | "Environments"
  | "Targets"
  | "Reports"
  | "Library"
  | "Tools"
  | "Settings"
  | "Help";

/** The sidebar, where the destinations are. */
export function sidebar() {
  return within(screen.getByRole("region", { name: "Navigation" }));
}

/** The page shown now. */
export function page() {
  return within(screen.getByRole("region", { name: "Main content" }));
}

/** The details of the selection open beside the list, once one is open. */
export function details() {
  return within(screen.getByRole("region", { name: "Details" }));
}

/** Goes to one destination of the sidebar. The project's own destinations
 * are offered only while a project or folder is open. */
export async function goTo(
  user: UserEvent,
  destination: Destination,
): Promise<void> {
  if (destination === "Library") {
    await goTo(user,"Test cases");
    const entryPoint=await waitFor(()=>{
      const offered=page().queryByRole("button",{name:"Test page actions"}) ?? page().getByRole("button",{name:"Back to tests"});
      if(offered.matches(":disabled"))throw new Error("The test page is finishing its current work");
      return offered;
    },{timeout:10_000});
    if(entryPoint.getAttribute("aria-label")==="Back to tests")await user.click(entryPoint);
    const actions=await waitFor(()=>{
      const offered=page().getByRole("button",{name:"Test page actions"});
      if(offered.matches(":disabled"))throw new Error("Test page actions are not available while work finishes");
      return offered;
    },{timeout:10_000});
    await user.click(actions);
    const library=await waitFor(()=>{
      const offered=screen.getByRole("menuitem",{name:"Library"});
      if(offered.matches(":disabled")||offered.getAttribute("aria-disabled")==="true")throw new Error("Library is not available while work finishes");
      return offered;
    },{timeout:10_000});
    await user.click(library);
    return;
  }
  if (destination === "Tools" && !sidebar().queryByRole("button", { name: "Tools" })) {
    await goTo(user, "Settings");
    await user.click(await page().findByRole("button", { name: "Tools" }));
    await page().findByRole("heading", { level: 1, name: "Tools" });
    return;
  }
  if (destination === "Projects" && !sidebar().queryByRole("button", { name: "Projects" })) {
    const switcher = sidebar().queryByRole("button", { name: /^(Project|Source): / }) ??
      page().getByRole("button", { name: /^(Project|Source): / });
    await user.click(switcher);
    await user.click(await screen.findByRole("menuitem", { name: "Projects" }));
    return;
  }
  if ((destination === "Runs" || destination === "Reports") && !sidebar().queryByRole("button", { name: destination })) {
    await goTo(user, "Test cases");
    const action = destination === "Runs" ? "Run history" : "Exports";
    if (!page().queryByRole("button", { name: action })) await goTo(user, "Test cases");
    if (!page().queryByRole("button", { name: action }) && !page().queryByRole("button", { name: "Test page actions" })) {
      const back = page().queryByRole("button", { name: "Back to tests" });
      if (back) await user.click(back);
    }
    if (!page().queryByRole("button", { name: action })) {
      await user.click(await page().findByRole("button", { name: "Test page actions" }));
      const entry = await waitFor(() => {
        const offered = screen.getByRole("menuitem", { name: action });
        if (offered.getAttribute("aria-disabled") === "true") throw new Error(`${action} is not available while work finishes`);
        return offered;
      }, { timeout: 10_000 });
      await user.click(entry);
      return;
    }
    const entry = await waitFor(() => {
      const offered = page().getByRole("button", { name: action });
      if (offered.matches(":disabled")) throw new Error(`${action} is not available while work finishes`);
      return offered;
    }, { timeout: 10_000 });
    await user.click(entry);
    return;
  }
  // Older journey verbs retain their logical destination while using its
  // current visible entry point. Every step still presses real UI controls.
  const labels: Partial<Record<Destination, Destination>> = {
    Cases: "Captures", Tests: "Test cases", Environments: "Targets",
  };
  const label = labels[destination] ?? destination;
  const button = await waitFor(() => {
    const offered = sidebar().queryByRole("button", { name: label }) ?? sidebar().getByRole("button", { name: destination });
    if (offered.matches(":disabled")) throw new Error(`${destination} is not available while work finishes`);
    return offered;
  }, { timeout: 10_000 });
  await user.click(button);
}

/** Opens one view of the page shown now: its tab, its category, the action in
 * its header that opens it, or the item of its More menu. */
export async function openView(user: UserEvent, name: string): Promise<void> {
  const shown = page();
  if (shown.queryByRole("heading", { level: 1, name })) return;
  const tab = shown.queryByRole("tab", { name });
  if (tab) {
    await user.click(tab);
    return;
  }
  const button = shown.queryByRole("button", { name });
  if (button) {
    await user.click(button);
    return;
  }
  await user.click(shown.getByRole("button", { name: /^More .* actions$/ }));
  await user.click(await screen.findByRole("menuitem", { name }));
}

/** Goes to a destination and opens one of its views. */
export async function goToView(
  user: UserEvent,
  destination: Destination,
  view: string,
): Promise<void> {
  await goTo(user, destination);
  await openView(user, view);
}

/** Read the verified identity in File details, then return to the case. */
export async function readCaseIdentity(
  user: UserEvent,
  identity: string,
): Promise<HTMLElement> {
  await user.click(
    await screen.findByRole("button", { name: "More case actions" }),
  );
  await user.click(screen.getByRole("menuitem", { name: "File details" }));
  const dialog = await screen.findByRole("dialog", { name: "File details" });
  const shown = await within(dialog).findByText(identity);
  await user.click(
    within(dialog).getByRole("button", { name: "Close file details" }),
  );
  return shown;
}

/** Return to the case list, then select another case through its current UI. */
export async function openListedCase(
  user: UserEvent,
  name: string,
): Promise<void> {
  await goTo(user, "Cases");
  // Out of a case flow and the case, back to the list.
  for (let step = 0; step < 3 && !screen.queryByRole("table", { name: /^(Captures|Cases)$/ }); step++) {
    const back = page().queryAllByRole("button", { name: /^Back to / })[0];
    if (!back) break;
    await user.click(back);
  }
  const row = await findCaseRow(name);
  // Contextual captures select their source card on a row click. Their name
  // and Open messages action are the explicit ways into the reader.
  await user.click(within(row).queryByRole("button", { name }) ??
    within(row).queryByRole("button", { name: "Open messages" }) ?? row);
}

export async function openCaseFlow(
  user: UserEvent,
  action: string,
): Promise<void> {
  await goTo(user, "Cases");
  if (!screen.queryByRole("button", { name: "More case actions" })) {
    // A case flow is open: its way back leads to the case.
    const back = page().getAllByRole("button", { name: /^Back to / }).find((button) => button.getAttribute("aria-label") !== "Back to cases");
    await user.click(back!);
  }
  await user.click(screen.getByRole("button", { name: "More case actions" }));
  await user.click(screen.getByRole("menuitem", { name: action }));
}

/** One row of the open project's Cases table: the case named, or the first. */
export function caseRow(name?: string): HTMLElement {
  const rows = within(screen.getByRole("table", { name: /^(Captures|Cases)$/ }))
    .getAllByRole("row")
    .filter((row) => row.hasAttribute("data-row-id"));
  const row = name === undefined ? rows[0] : rows.find((candidate) => candidate.getAttribute("aria-label") === name);
  if (!row) throw new Error(`the Cases table lists no ${name ?? "case"}`);
  return row;
}

/** A row of the Cases table once the list has been read. */
export async function findCaseRow(name?: string): Promise<HTMLElement> {
  await screen.findByRole("table", { name: /^(Captures|Cases)$/ });
  let row: HTMLElement | undefined;
  await waitFor(() => {
    row = caseRow(name);
  });
  return row!;
}

/** One row of the open case's Messages table, by the occurrence it shows. */
export async function findMessageRow(occurrence: string): Promise<HTMLElement> {
  const table = await screen.findByRole("table", { name: "Messages" });
  let row: HTMLElement | null = null;
  await waitFor(() => {
    row = table.querySelector<HTMLElement>(`[data-row-id="${CSS.escape(occurrence)}"]`);
    if (!row) throw new Error(`the Messages table shows no ${occurrence}`);
  });
  return row!;
}
