// Host tasks hand the hub host's operator a reviewed command: the window reads
// a real local copy of the hub's configuration, previews the command through
// the bound Go preview, refuses a host path that is not a clean absolute one,
// and never runs anything or contacts a host.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { enter, Journey, press } from "../testkit/journey";
import { goToView } from "../testkit/navigation";

let journey: Journey;
beforeEach(() => { journey = Journey.create(); });
afterEach(async () => { await journey.dispose(); });

test("the hub administration handoff reads a real configuration and never runs its host step", async () => {
  const user = userEvent.setup();
  const config = journey.writeFile("operator-copy/config.json", JSON.stringify({
    schema: "readmit-hub-config/v1",
    listen: "127.0.0.1:8443",
    artifact_root: "/var/lib/readmit-hub/artifacts",
    postgres_socket: "/var/run/postgresql",
    postgres_port: 5432,
    postgres_database: "readmit_hub",
    postgres_user: "readmit-hub",
    tls_certificate: "/etc/readmit-hub/server.pem",
    tls_key: "/etc/readmit-hub/server-key.pem",
    client_ca: "/etc/readmit-hub/client-ca.pem",
    max_storage_bytes: 1073741824,
  }));
  await journey.launch();
  await goToView(user, "Settings", "Team");
  const team = within(await screen.findByRole("region", { name: "Team" }));
  await press(user, await team.findByRole("button", { name: "More team actions" }));
  await press(user, await screen.findByRole("menuitem", { name: "Administrator setup" }));
  const setup = within(await screen.findByRole("region", { name: "Administrator setup" }));
  await press(user, setup.getByRole("button", { name: "Host tasks" }));
  await press(user, await setup.findByRole("button", { name: "Migrate metadata" }));

  // The configuration copy is chosen in the host's dialog and the command
  // previewed against the path it has on the hub host.
  let sheet = within(await screen.findByRole("dialog", { name: "Migrate metadata" }));
  await journey.chooseFiles([config], "Choose a copy of the hub configuration");
  await press(user, sheet.getByRole("button", { name: "Choose…" }));
  await sheet.findByText("config.json");
  expect((sheet.getByLabelText("Configuration on host") as HTMLInputElement).value).toBe("/etc/readmit-hub/config.json");
  await press(user, sheet.getByRole("button", { name: "Preview command" }));
  sheet = within(await screen.findByRole("dialog", { name: "Migrate metadata" }));
  expect(await sheet.findByText("readmit-hub -config '/etc/readmit-hub/config.json' migrate")).toBeTruthy();
  expect(journey.calls.filter((call) => call.method === "Preview").at(-1)?.result).toMatchObject({ state: "completed" });
  await press(user, sheet.getByRole("button", { name: "Close migrate metadata" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Migrate metadata" })).toBeNull());

  // A host path that is not a clean absolute one is refused and previews no
  // command.
  await press(user, setup.getByRole("button", { name: "Migrate metadata" }));
  sheet = within(await screen.findByRole("dialog", { name: "Migrate metadata" }));
  await enter(user, sheet.getByLabelText("Configuration on host"), "relative.json");
  await press(user, sheet.getByRole("button", { name: "Preview command" }));
  expect((await sheet.findAllByRole("alert")).map((alert) => alert.textContent).join(" ")).toContain("clean absolute Linux path");
  expect(screen.queryByText("readmit-hub -config 'relative.json' migrate")).toBeNull();
  await press(user, sheet.getByRole("button", { name: "Cancel" }));

  // Nothing ran a host step, exported a setup or reached a hub.
  expect(journey.calls.filter((call) => /Hub|Export/.test(call.method)).map((call) => call.method).filter((method) => !["HubStatus", "ChooseHubLocalCopy"].includes(method))).toEqual([]);
});
