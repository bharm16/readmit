// Moving around the window the way a person does: the sidebar's destinations
// and the views inside a page. Every page stays mounted while another is
// shown, but only the shown one is in the accessibility tree, so a test goes
// to the page that holds a control before asking for it by role.
import { screen, within } from "@testing-library/react";
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

/** The details of the message open beside the case, once one is open. */
export function details() {
  return within(screen.getByRole("region", { name: "Message details" }));
}

/** Goes to one destination of the sidebar. The project's own destinations
 * are offered only while a project or folder is open. */
export async function goTo(
  user: UserEvent,
  destination: Destination,
): Promise<void> {
  await user.click(await sidebar().findByRole("button", { name: destination }));
}

/** Opens one view of the page shown now, by its tab's name. */
export async function openView(user: UserEvent, name: string): Promise<void> {
  await user.click(await screen.findByRole("tab", { name }));
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
  const trail = screen.queryByRole("navigation", { name: "Where you are" });
  if (trail) await user.click(within(trail).getAllByRole("button")[0]!);
  await user.click(
    await screen.findByRole("button", { name: `Open case ${name}` }),
  );
}

export async function openCaseFlow(
  user: UserEvent,
  action: string,
): Promise<void> {
  await goTo(user, "Cases");
  if (!screen.queryByRole("button", { name: "More case actions" })) {
    const trail = screen.getByRole("navigation", { name: "Where you are" });
    await user.click(within(trail).getAllByRole("button").at(-1)!);
  }
  await user.click(screen.getByRole("button", { name: "More case actions" }));
  await user.click(screen.getByRole("menuitem", { name: action }));
}
