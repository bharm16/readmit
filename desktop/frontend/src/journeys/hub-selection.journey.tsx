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
import { Journey, press, region, whenEnabled } from "../testkit/journey";
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

/** The customer hub panel of the privacy region. */
function hubPanel() {
  return within(region("Hub"));
}

/** The privacy status row of the customer hub. */
function hubRow() {
  const table = screen.getByRole("table", { name: "Deliberately configured activities and their destinations" });
  return within(within(table).getByRole("rowheader", { name: "Customer artifact hub" }).closest("tr")!);
}

/** Reads the privacy status again and waits for the hub row to show a state
 * and the sentence beside it. */
async function expectHubState(user: UserEvent, state: string): Promise<void> {
  await press(user, screen.getByRole("button", { name: "Refresh privacy status" }));
  await waitFor(() => expect(hubRow().getAllByRole("cell")[3]?.textContent).toBe(state));
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

/** Chooses the operator's folder in the hub panel, as a person does. */
async function chooseConfiguration(user: UserEvent): Promise<void> {
  await journey.chooseFolder(journey.path("hub-operator"), "Choose customer hub configuration folder");
  await press(user, hubPanel().getByRole("button", { name: "Choose configuration…" }));
}

test("a chosen hub configuration is restored selected and offline when the window is reopened, and reopening reaches nothing", async () => {
  const user = userEvent.setup();
  const configuration = writeConfiguration(`https://${hub.address}`);
  await journey.launch();
  expect(await hubPanel().findByText("No configuration file selected. Working entirely offline.")).toBeTruthy();
  await chooseConfiguration(user);
  expect(await hubPanel().findByText(`Configuration file: ${configuration}`)).toBeTruthy();

  // Closed and reopened, the window shows the configuration it remembered,
  // offline, with connecting left as the person's own next act.
  await journey.close();
  const reopenedFrom = journey.calls.length;
  await journey.launch();
  expect(await hubPanel().findByText(`Configuration file: ${configuration}`)).toBeTruthy();
  expect(hubPanel().getByText("Offline / Local Mode")).toBeTruthy();
  await whenEnabled(hubPanel().getByRole("button", { name: "Connect to hub" }));
  await expectHubState(
    user,
    "Configured, offlineA hub configuration is selected; not connected. Connecting is a deliberate action.",
  );
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
  await chooseConfiguration(user);
  expect(await hubPanel().findByText(`Configuration file: ${configuration}`)).toBeTruthy();
  await journey.close();

  // While the window is closed the operator's file changes to name a plain
  // http endpoint, which no hub configuration may.
  writeConfiguration(`http://${hub.address}`);
  await journey.launch();
  // The panel says the remembered configuration no longer validates, that the
  // endpoint is why, and how to recover.
  expect(
    await hubPanel().findByText(
      /^the remembered hub configuration no longer validates \(.*endpoint.*https.*\); choose a hub configuration again$/,
    ),
  ).toBeTruthy();
  expect(hubPanel().getByText(`Configuration file: ${configuration}`)).toBeTruthy();
  expect(hubPanel().getByRole("button", { name: "Check connection setup" }).hasAttribute("disabled")).toBe(true);
  expect(hubPanel().getByRole("button", { name: "Connect to hub" }).hasAttribute("disabled")).toBe(true);
  await expectHubState(
    user,
    "Not configuredNo hub configuration is selected. Every hub operation is unavailable and nothing is connected.",
  );

  // Corrected and chosen again, it is selected and offline once more.
  writeConfiguration(`https://${hub.address}`);
  await chooseConfiguration(user);
  await waitFor(() => expect(hubPanel().queryByText(/no longer validates/)).toBeNull());
  expect(hubPanel().getByText(`Configuration file: ${configuration}`)).toBeTruthy();
  await whenEnabled(hubPanel().getByRole("button", { name: "Connect to hub" }));
  await journey.close();
  expect(hub.accepted()).toBe(0);
});
