// Moving around the window the way a person does: the sidebar's destinations
// and the views inside a page. Every page stays mounted while another is
// shown, but only the shown one is in the accessibility tree, so a test goes
// to the page that holds a control before asking for it by role.
import { screen, waitFor, within } from "@testing-library/react";
import type { UserEvent } from "@testing-library/user-event";

/** The sidebar's destinations, as the window labels them. */
export type Destination =
  | "Projects"
  | "Cases"
  | "Tests"
  | "Runs"
  | "Environments"
  | "Reports"
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
  await user.click(await sidebar().findByRole("button", { name: destination }));
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
  for (let step = 0; step < 3 && !screen.queryByRole("table", { name: "Cases" }); step++) {
    const back = page().queryAllByRole("button", { name: /^Back to / })[0];
    if (!back) break;
    await user.click(back);
  }
  await user.click(await findCaseRow(name));
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
  const rows = within(screen.getByRole("table", { name: "Cases" }))
    .getAllByRole("row")
    .filter((row) => row.hasAttribute("data-row-id"));
  const row = name === undefined ? rows[0] : rows.find((candidate) => candidate.getAttribute("aria-label") === name);
  if (!row) throw new Error(`the Cases table lists no ${name ?? "case"}`);
  return row;
}

/** A row of the Cases table once the list has been read. */
export async function findCaseRow(name?: string): Promise<HTMLElement> {
  await screen.findByRole("table", { name: "Cases" });
  let row: HTMLElement | undefined;
  await waitFor(() => {
    row = caseRow(name);
  });
  return row!;
}
