// The recent list's answers a real folder cannot easily be made to give: a
// list this account cannot replace and a forget that meets a running
// operation. Both keep the list on screen, say why in words, and forget
// nothing; the journeys drive the rest against the real facade.
import { expect, test } from "vitest";
import { within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { WORKSPACE_ROOT, recentResult } from "./testkit/fixtures";
import { renderApp } from "./testkit/app";
import { page } from "./testkit/navigation";

const OTHER_ROOT = "/another-workspace-under-test";

test("a forget the facade refuses keeps the list and says why, and asks again only when the person does", async () => {
  const user = userEvent.setup();
  const { facade } = await renderApp({ RecentWorkspaces: () => recentResult([WORKSPACE_ROOT, OTHER_ROOT]) });
  const home = page();
  const list = within(await home.findByRole("list", { name: "Recent projects" }));
  expect(await list.findByRole("button", { name: `Remove ${OTHER_ROOT} from recent projects` })).toBeTruthy();

  for (const [state, reason] of [
    ["permission_denied", "this account cannot write the recent workspace list"],
    ["busy", "another operation is already running"],
  ] as const) {
    facade.reply({ ForgetWorkspace: () => ({ state, reason, roots: [WORKSPACE_ROOT, OTHER_ROOT] }) });
    await user.click(list.getByRole("button", { name: `Remove ${OTHER_ROOT} from recent projects` }));
    await user.click(within(list.getByRole("group", { name: `Forget ${OTHER_ROOT}?` })).getByRole("button", { name: "Remove" }));
    expect(await home.findByText(reason)).toBeTruthy();
    expect(list.getByRole("button", { name: `Open ${OTHER_ROOT}` })).toBeTruthy();
    expect(list.getByRole("button", { name: `Open ${WORKSPACE_ROOT}` })).toBeTruthy();
  }
  expect(facade.callsTo("ForgetWorkspace").map((call) => call.args)).toEqual([[OTHER_ROOT], [OTHER_ROOT]]);
});
