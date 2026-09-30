// The customer hub configuration a person chooses is remembered across
// sessions (#335). Reopening the window reads it back — the remembered
// selection and the configuration it names, both local files — and shows it
// selected and offline: nothing reaches the hub, no sign-in starts and no
// session is renewed. The configured hub is a loopback listener that counts
// every connection made to it, the independent witness that none was. The
// certificate material the configuration names is never read by choosing or
// restoring it, so none is placed. A remembered configuration that has stopped
// validating is shown after a reopen with which one it was and why, and
// choosing a configuration again recovers.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { UserEvent } from "@testing-library/user-event";
import { Journey, press, region } from "../testkit/journey";
import { goToView, page } from "../testkit/navigation";
import { countingListener } from "./probes.js";
import type { CountingListener } from "./probes.js";

let journey: Journey;
let hub: CountingListener;

beforeEach(async () => {
  journey = Journey.create();
  hub = await countingListener();
});

afterEach(async () => {
  await journey.dispose();
  await hub.close();
});

/** Settings › Team. */
async function team(user: UserEvent) {
  await goToView(user, "Settings", "Team");
  return within(await screen.findByRole("region", { name: "Team" }));
}

/** The team hub's row in Settings › Security, as its cells read. */
async function securityRow(user: UserEvent): Promise<string[]> {
  await goToView(user, "Settings", "Security");
  const row = await page().findByRole("row", { name: "Team hub" });
  return Array.from(row.querySelectorAll("td,th")).map((cell) => cell.textContent ?? "");
}

/** The operator's hub client configuration, naming hub as its endpoint. */
function writeConfiguration(hubEndpoint: string): string {
  const operator = journey.path("hub-operator");
  return journey.writeFile(
    "hub-operator/hub-client.json",
    JSON.stringify({
      schema: "readmit-hub-client/v1",
      hub: hubEndpoint,
      ca: `${operator}/customer-ca.pem`,
      certificate: `${operator}/desktop-client.pem`,
      key: { command: "/bin/cat", arguments: [`${operator}/desktop-client-key.pem`] },
      idp: {
        issuer: "https://idp.customer.example",
        client_id: "readmit-desktop",
        audience: "readmit-hub",
        authorize_endpoint: "https://idp.customer.example/authorize",
        token_endpoint: "https://idp.customer.example/token",
        scopes: ["evidence.read"],
      },
      projects: ["cardio-icu"],
    }) + "\n",
  );
}

/** Connect team: the operator's file chosen in the host's dialog and saved
 * under the name the file's address offers. */
async function connectTeam(user: UserEvent, configuration: string): Promise<void> {
  const panel = await team(user);
  await press(user, await panel.findByRole("button", { name: "Connect team" }));
  const sheet = within(await screen.findByRole("dialog", { name: "Connect team" }));
  await journey.chooseFiles([configuration], "Choose your team's configuration");
  await press(user, sheet.getByRole("button", { name: "Choose file…" }));
  expect(await sheet.findByText("cardio-icu")).toBeTruthy();
  expect((sheet.getByLabelText("Name") as HTMLInputElement).value).toBe("127.0.0.1");
  await press(user, sheet.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Connect team" })).toBeNull());
}

test("a chosen hub configuration is restored selected and offline when the window is reopened, and reopening reaches nothing", async () => {
  const user = userEvent.setup();
  const configuration = writeConfiguration(`https://${hub.address}`);
  await journey.launch();
  expect(await (await team(user)).findByText("No team configured")).toBeTruthy();
  await connectTeam(user, configuration);
  expect(await within(region("Team")).findByText("Not connected")).toBeTruthy();

  // Closed and reopened, the window shows the team it remembered, offline,
  // with signing in left as the person's own next act.
  await journey.close();
  const reopenedFrom = journey.calls.length;
  await journey.launch();
  const reopened = await team(user);
  expect(await reopened.findByText("Not connected")).toBeTruthy();
  expect(reopened.getByText("127.0.0.1")).toBeTruthy();
  expect(reopened.getByRole("button", { name: "Sign in" })).toBeTruthy();
  expect(await securityRow(user)).toContain("Not checked");
  await journey.close();

  // The reopened window asked the hub for nothing: its only hub call was the
  // status read, which answered offline and signed out, and the hub counted
  // no connection.
  const hubCalls = journey.calls.slice(reopenedFrom).filter((call) => call.method.includes("Hub"));
  expect(new Set(hubCalls.map((call) => call.method))).toEqual(new Set(["HubStatus"]));
  for (const call of hubCalls) {
    expect(call.result).toMatchObject({ connected: false, authenticated: false });
  }
  expect(hubCalls.some((call) => (call.result as { config_path?: string }).config_path === configuration)).toBe(true);
  expect(hub.accepted()).toBe(0);
});

test("a remembered hub configuration that stopped validating is shown with why after a reopen, and choosing one again recovers", async () => {
  const user = userEvent.setup();
  const configuration = writeConfiguration(`https://${hub.address}`);
  await journey.launch();
  await connectTeam(user, configuration);
  expect(await within(region("Team")).findByText("Not connected")).toBeTruthy();
  await journey.close();

  // While the window is closed the operator's file changes to name a plain
  // http endpoint, which no hub configuration may.
  writeConfiguration(`http://${hub.address}`);
  await journey.launch();
  // Team says the remembered configuration no longer validates, that the
  // endpoint is why, and offers to connect again; nothing can sign in.
  const panel = await team(user);
  expect(
    await panel.findByText(/^the remembered hub configuration no longer validates \(.*endpoint.*https.*\); choose a hub configuration again$/),
  ).toBeTruthy();
  expect(panel.getByText("No team configured")).toBeTruthy();
  expect(panel.queryByRole("button", { name: "Sign in" })).toBeNull();
  expect(await securityRow(user)).toContain("Unavailable");

  // Corrected and chosen again, it is selected and offline once more.
  writeConfiguration(`https://${hub.address}`);
  await connectTeam(user, configuration);
  const recovered = within(region("Team"));
  expect(await recovered.findByText("Not connected")).toBeTruthy();
  expect(recovered.queryByText(/no longer validates/)).toBeNull();
  expect(recovered.getByRole("button", { name: "Sign in" })).toBeTruthy();
  await journey.close();
  expect(hub.accepted()).toBe(0);
});
