// Steps the interruption and measurement journeys share: the start of every
// licensed investigation, and the way a timing is written down. A step is
// what a person does in the window, through the harness's own verbs; nothing
// here answers a facade call.
import { expect } from "vitest";
import { screen, within } from "@testing-library/react";
import type { UserEvent } from "@testing-library/user-event";
import { press, region, whenEnabled } from "../testkit/journey";
import type { Journey } from "../testkit/journey";
import { hostLoad } from "./probes.js";

/** Synthetic MLLP-framed booking; every value is synthetic. */
export const BOOKING =
  "MSH|^~\\&|SCHEDULER|SYNTHETIC|RECEIVER|LAB|20260101120000||SIU^S12|CTL-1|P|2.5.1\rPID|1||SYNTH-1^^^READMIT||SYNTHETIC^ONLY\r";
export const framed = (message: string) => `\x0b${message}\x1c\r`;

/** Starts the application, selects the activation folder the vendor
 * delivered and creates a project, as the own-evidence journey does, and
 * returns the project folder once its overview has drawn. */
export async function licensedProject(journey: Journey, user: UserEvent): Promise<string> {
  const license = journey.provisionLicense("vendor-delivered-license");
  journey.makeFolder("investigations");
  await journey.launch();
  await press(user, screen.getByRole("button", { name: "License and activation…" }));
  const access = within(region("License and trial activation"));
  await journey.chooseFolder(license, "Choose the license activation folder");
  await press(user, access.getByRole("button", { name: "Select a supplied activation folder…" }));
  await whenEnabled(access.getByRole("button", { name: "Activate license" }));
  await journey.chooseFolder(journey.path("investigations"), "Open a readmit workspace folder");
  await press(user, screen.getByRole("button", { name: "Choose a folder for a new project…" }));
  const evidence = within(region("Evidence"));
  await press(user, await evidence.findByRole("button", { name: "Create a project…" }));
  await user.type(evidence.getByLabelText("Folder name for the new project"), "interface");
  await user.type(evidence.getByLabelText("Title", { selector: "#project-title" }), "Scheduling interface");
  await user.type(evidence.getByLabelText("Interface versions, comma-separated"), "siu-2.5.1-v1");
  await journey.chooseFolder(journey.path("investigations"), "Choose a folder for the new project");
  await press(user, evidence.getByRole("button", { name: "Create the project…" }));
  const project = journey.path("investigations", "interface");
  expect(await within(region("Project navigation")).findByText(project, { selector: ".root" })).toBeTruthy();
  // The project overview has drawn, so its controls are the ones a person sees.
  expect(await evidence.findByText(/Nothing is registered yet/)).toBeTruthy();
  return project;
}

/** Logs how long the window took to show what a person waited for, beside
 * the host's load, so a number lifted out of the log still says what else the
 * machine was doing. The page is jsdom driven by the real window code, so no
 * number here is a native painted frame. */
export function logTiming(label: string, samples: number[]): void {
  const ordered = [...samples].sort((a, b) => a - b);
  const p95 = ordered[Math.ceil(ordered.length * 0.95) - 1] ?? Number.NaN;
  console.log(
    `[window timing] ${label}: samples_ms=[${samples.map((value) => value.toFixed(1)).join(" ")}] ` +
      `nearest_rank_p95_ms=${p95.toFixed(1)} (jsdom, not a painted frame; load ${hostLoad()})`,
  );
}
