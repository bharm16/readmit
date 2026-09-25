import { expect, test } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HubAdministration } from "./HubAdministration";
import { installFacade, installHubAdmin } from "./testkit/wails";
import type { HubAdminRequest, HubAdminResult } from "./bindings";

async function fillCommon(user: ReturnType<typeof userEvent.setup>) {
  await user.type(screen.getByLabelText("Local configuration"), "/local/config.json");
  await user.type(screen.getByLabelText("Host configuration"), "/etc/readmit-hub/config.json");
}

async function reviewCommand(operation: HubAdminRequest["operation"], label: string) {
  const user = userEvent.setup();
  installFacade();
  const localResult = operation === "schedule-pin" ? "a".repeat(64) : operation === "verify-backup" ? "The copy verified." : "";
  const result: HubAdminResult = {
    state: "completed",
    command: `readmit-hub -config '/etc/readmit-hub/config.json' ${operation}`,
    prerequisites: ["Run on the customer hub host."],
    touches: ["Host state described by this operation."],
    does_not_touch: ["The desktop never runs the command."],
    ...(localResult ? { local_result: localResult } : {}),
  };
  const admin = installHubAdmin({ Preview: async () => result });
  render(<HubAdministration />);
  await user.selectOptions(screen.getByLabelText("Operator step"), operation);
  expect(screen.getByRole("option", { name: label, selected: true })).toBeTruthy();
  await fillCommon(user);
  if (["backup", "verify-backup", "restore", "schedule-pin"].includes(operation)) {
    await user.type(screen.getByLabelText(operation === "schedule-pin" ? "Hub host test specification path" : "Hub host backup directory"), "/customer/entry");
  }
  if (operation === "verify-backup" || operation === "restore" || operation === "schedule-pin") {
    await user.type(screen.getByLabelText(operation === "schedule-pin" ? "Local test" : "Local backup"), "/local/entry");
  }
  if (operation === "schedule-init") {
    await user.type(screen.getByLabelText("Local copy of operation policy"), "/local/operation.json");
    await user.type(screen.getByLabelText("Hub host operation policy path"), "/etc/readmit-hub/operation.json");
    await user.type(screen.getByLabelText("Local copy of schedule policy"), "/local/schedules.json");
    await user.type(screen.getByLabelText("Hub host schedule policy path"), "/etc/readmit-hub/schedules.json");
  }
  const button = screen.getByRole("button", { name: "Preview command" });
  await user.click(button);
  expect(admin.callsTo("Preview")).toHaveLength(1);
  const request = admin.callsTo("Preview")[0]!.args[0] as HubAdminRequest;
  expect(request.operation).toBe(operation);
  expect(request.config_copy).toBe("/local/config.json");
  expect(request.config_path).toBe("/etc/readmit-hub/config.json");
  expect(await screen.findByText(result.command!)).toBeTruthy();
  expect(screen.getByText("Run on the customer hub host.")).toBeTruthy();
  expect(screen.getByText("Host state described by this operation.")).toBeTruthy();
  expect(screen.getByText("The desktop never runs the command.")).toBeTruthy();
  if (result.local_result) expect(screen.getByText(result.local_result)).toBeTruthy();
}

test("hub administration reviews migrate through its bound Go preview", async () => reviewCommand("migrate", "Migrate metadata"));
test("hub administration reviews check through its bound Go preview", async () => reviewCommand("check", "Check readiness"));
test("hub administration reviews backup through its bound Go preview", async () => reviewCommand("backup", "Create backup"));
test("hub administration reviews verify-backup through its bound Go preview", async () => reviewCommand("verify-backup", "Verify backup"));
test("hub administration reviews restore through its bound Go preview", async () => reviewCommand("restore", "Restore backup"));
test("hub administration reviews schedule-init through its bound Go preview", async () => reviewCommand("schedule-init", "Initialize schedules"));
test("hub administration reviews schedule-pin through its bound Go preview", async () => reviewCommand("schedule-pin", "Pin schedule inputs"));

test("hub administration refuses an invalid preview and clears stale commands when inputs change", async () => {
  const user = userEvent.setup();
  installFacade();
  let answer: HubAdminResult = { state: "completed", command: "readmit-hub -config '/etc/readmit-hub/config.json' migrate" };
  const admin = installHubAdmin({ Preview: async () => answer });
  render(<HubAdministration />);
  await fillCommon(user);
  await user.click(screen.getByRole("button", { name: "Preview command" }));
  expect(await screen.findByText(answer.command!)).toBeTruthy();
  await user.clear(screen.getByLabelText("Host configuration"));
  expect(screen.queryByText(answer.command!)).toBeNull();
  answer = { state: "failed", reason: "enter a clean absolute Linux path for the hub configuration" };
  await user.click(screen.getByRole("button", { name: "Preview command" }));
  expect((await screen.findByRole("alert")).textContent).toContain("enter a clean absolute Linux path");
  expect(screen.queryByText(/Reviewed host command:/)).toBeNull();
  expect(admin.callsTo("Preview")).toHaveLength(2);
});

test("hub administration cancels a busy review from the keyboard without publishing a stale command", async () => {
  const user = userEvent.setup();
  installFacade();
  let finish!: (result: HubAdminResult) => void;
  const pending = new Promise<HubAdminResult>((resolve) => { finish = resolve; });
  const admin = installHubAdmin({
    Preview: () => pending,
    CancelPreview: async () => ({ state: "busy", reason: "cancellation requested; waiting for the current bounded local read to finish" }),
  });
  render(<HubAdministration />);
  await fillCommon(user);
  await user.click(screen.getByRole("button", { name: "Preview command" }));
  expect(await screen.findByText("Checking local copies…")).toBeTruthy();
  expect(screen.getByRole("button", { name: "Preview command" }).hasAttribute("disabled")).toBe(true);
  const cancel = screen.getByRole("button", { name: "Cancel handoff" });
  cancel.focus();
  await user.keyboard("{Enter}");
  expect(admin.callsTo("CancelPreview")).toHaveLength(1);
  expect(await screen.findByText(/Review state: busy/)).toBeTruthy();
  expect(screen.getByText("Stopping local check…")).toBeTruthy();
  finish({ state: "completed", command: "stale command" });
  await waitFor(() => expect(screen.getByRole("button", { name: "Preview command" }).hasAttribute("disabled")).toBe(false));
  expect(screen.getByText(/Review state: cancelled/)).toBeTruthy();
  expect(screen.queryByText("stale command")).toBeNull();
});

test("hub administration cancels an idle handoff without a Go operation", async () => {
  const user = userEvent.setup();
  installFacade();
  const admin = installHubAdmin({});
  render(<HubAdministration />);
  await user.click(screen.getByRole("button", { name: "Cancel handoff" }));
  expect(await screen.findByText(/handoff review cancelled; no host action was taken/)).toBeTruthy();
  expect(admin.callsTo("Preview")).toHaveLength(0);
  expect(admin.callsTo("CancelPreview")).toHaveLength(0);
});
