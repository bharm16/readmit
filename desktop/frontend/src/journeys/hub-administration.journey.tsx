import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { enter, Journey, press, region } from "../testkit/journey";

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
  const panel = within(within(region("Privacy")).getByRole("region", { name: "Hub" }));
  await press(user, panel.getByRole("button", { name: "Host administration" }));
  const admin = within(panel.getByRole("region", { name: "Hub host administration" }));
  await enter(user, admin.getByLabelText("Local configuration"), config);
  await enter(user, admin.getByLabelText("Host configuration"), "/etc/readmit-hub/config.json");
  await press(user, admin.getByRole("button", { name: "Preview command" }));
  expect(await admin.findByText("readmit-hub -config '/etc/readmit-hub/config.json' migrate")).toBeTruthy();
  expect(journey.calls.filter((call) => call.method === "Preview").at(-1)?.result).toMatchObject({ state: "completed" });
  await enter(user, admin.getByLabelText("Host configuration"), "relative.json");
  expect(admin.queryByText("readmit-hub -config '/etc/readmit-hub/config.json' migrate")).toBeNull();
  await press(user, admin.getByRole("button", { name: "Preview command" }));
  expect((await admin.findByRole("alert")).textContent).toContain("clean absolute Linux path");
  await press(user, admin.getByRole("button", { name: "Cancel handoff" }));
  expect(await admin.findByText(/handoff review cancelled; no host action was taken/)).toBeTruthy();
  expect(screen.queryByText("readmit-hub -config 'relative.json' migrate")).toBeNull();
});
