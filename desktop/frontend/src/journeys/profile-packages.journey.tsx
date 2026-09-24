// Reusable interface contracts a person receives and reviews in the profile
// panel. A package somebody exported with `readmit profile export` arrives in
// a workspace beside a tampered copy and one of a version this release cannot
// read. The window refuses both in the command line's own words, imports the
// real one into a new directory, shows what it verified — the profile, the
// pinned pack, the version seal, the package's identity, the local origin and
// the pack's provenance — and says nothing was activated, which the files
// beside it confirm. The command line imports the same package into the same
// five documents byte for byte, and both refuse a directory that is already
// there. A profile the command line imported is then opened in the editor,
// resolved against the pack it pins, and shows the seal it was imported with;
// a pack it does not pin is read for nothing, a profile the reader refuses
// replaces nothing, and an edit the window retained is never replaced by an
// open. Importing and opening need no license term, as `readmit profile
// import` needs none; retaining an edit is licensed authoring.
//
// Every document here is a shipped synthetic fixture or derived from one: a
// profile of field names, codes and a namespace, never a message or a person.
import { afterEach, beforeEach, expect, test } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { enter, Journey, press } from "../testkit/journey";
import { exists } from "./probes.js";
import { activateLicense } from "./steps";

let journey: Journey;

beforeEach(() => {
  journey = Journey.create();
});

afterEach(async () => {
  await journey.dispose();
});

/** The four documents a package is exported from, as the fixtures ship them,
 * and the saved-test references index that pins the profile. */
const FIXTURES = ["local-profile.json", "profile-pack.json", "profile-version.json", "profile-origin.json", "profile-references.json"];

/** Every document an import writes. */
const IMPORTED = ["profile.json", "pack.json", "version.json", "origin.json", "package.json"];

/** Places the fixtures in the workspace and has the command line export the
 * package somebody would send, as its sender runs it. */
async function receivedPackage(): Promise<string> {
  for (const fixture of FIXTURES) {
    journey.placeFixture(fixture, `interfaces/${fixture}`);
  }
  const exported = await journey.commandLine([
    "profile", "export", "interfaces/local-profile.json", "--pack", "interfaces/profile-pack.json",
    "--version", "interfaces/profile-version.json", "--origin", "interfaces/profile-origin.json",
    "--output", "interfaces/interface-package.json", "--reviewed",
  ]);
  expect(exported.code).toBe(0);
  return journey.readFile("interfaces/interface-package.json");
}

/** Opens the workspace the packages arrived in, through the host's dialog. */
async function openInterfaces(user: UserEvent): Promise<ReturnType<typeof within>> {
  await journey.chooseFolder(journey.path("interfaces"), "Open a readmit workspace folder");
  await press(user, screen.getByRole("button", { name: "Open a workspace folder…" }));
  return within(await screen.findByRole("region", { name: "Interface Profiles & Metadata Packs" }));
}

/** What one document of the fixtures says, read from the file. */
function fixtureDocument(name: string) {
  return JSON.parse(journey.readFile(`interfaces/${name}`));
}

test("a profile package is imported into a new directory, shows what it verified and activated nothing, and the command line accepts and refuses the same packages", async () => {
  const user = userEvent.setup();
  const sent = await receivedPackage();
  journey.writeFile("interfaces/tampered-package.json", sent.replace("No external profile content is incorporated.", "External content is incorporated."));
  journey.writeFile("interfaces/next-package.json", sent.replace('"readmit-profile-package/v1"', '"readmit-profile-package/v2"'));
  const before = new Map(FIXTURES.map((name) => [name, journey.digest(`interfaces/${name}`)]));
  await journey.launch();
  const panel = await openInterfaces(user);

  await press(user, panel.getByRole("tab", { name: "Package Exchange" }));
  const form = within(panel.getByRole("form", { name: "Import profile package" }));
  const outcome = () => within(panel.getByRole("region", { name: "Package import" }));
  const importInto = async (file: string, output: string) => {
    await enter(user, form.getByLabelText("Package File:"), file);
    await enter(user, form.getByLabelText("Output Directory:"), output);
    await press(user, form.getByRole("button", { name: "Import Package" }));
  };

  // A tampered package and one of a version this release cannot read are
  // refused by the window and the command line alike, and neither writes.
  for (const [file, refusal] of [
    ["tampered-package.json", "profile package integrity check failed"],
    ["next-package.json", "unsupported profile package version"],
  ] as const) {
    await importInto(file, "imported-interface");
    expect(await panel.findByText(refusal)).toBeTruthy();
    expect(outcome().getByRole("heading", { name: "Import refused" })).toBeTruthy();
    const command = await journey.commandLine(["profile", "import", `interfaces/${file}`, "--output", "interfaces/command-refused"]);
    expect(command.code).not.toBe(0);
    expect(command.stderr).toBe(`readmit: ${refusal}\n`);
    expect(exists(journey.path("interfaces", "imported-interface"))).toBe(false);
    expect(exists(journey.path("interfaces", "command-refused"))).toBe(false);
  }

  // The package that was sent is imported into a new directory, and the
  // window shows what it verified, stated here from the documents themselves.
  await importInto("interface-package.json", "imported-interface");
  expect(await panel.findByRole("heading", { name: "Imported into imported-interface" })).toBeTruthy();
  const fact = (term: string) => outcome().getByText(term, { selector: "dt" }).nextElementSibling?.textContent;
  const profile = fixtureDocument("local-profile.json");
  const seal = fixtureDocument("profile-version.json");
  const origin = fixtureDocument("profile-origin.json");
  const pack = fixtureDocument("profile-pack.json");
  expect(fact("Profile")).toBe(`${profile.profile.id} v${profile.profile.version}`);
  expect(fact("Pinned pack")).toBe(`${profile.base.pack.id} v${profile.base.pack.version}`);
  expect(fact("Version seal")).toBe(`${seal.profile.id} v${seal.profile.version} · SHA-256 ${seal.content.sha256} · ${seal.content.bytes} bytes`);
  expect(fact("Package SHA-256")).toBe(journey.digest("interfaces/interface-package.json"));
  expect(fact("Source")).toBe(origin.source);
  expect(fact("License")).toBe(origin.license);
  expect(fact("Mapping limitations")).toBe(origin.mapping_limitations);
  expect(fact("Review reference")).toBe(origin.review_reference);
  expect(fact("Pack source")).toBe(`${pack.provenance.source.name} · ${pack.provenance.source.location} @ ${pack.provenance.source.revision}`);
  expect(fact("Rights review")).toBe(`${pack.provenance.rights_review.status} (${pack.provenance.rights_review.reference})`);
  await user.click(outcome().getByText("License notice"));
  expect(outcome().getByText(origin.notice)).toBeTruthy();
  expect(outcome().getByText(/^Nothing was activated: no project changed, no saved test was repinned, no message was evaluated/)).toBeTruthy();

  // The command line imports the same package into the same five documents,
  // byte for byte, and what the window wrote is the profile that was sent.
  const command = await journey.commandLine(["profile", "import", "interfaces/interface-package.json", "--output", "interfaces/command-imported"]);
  expect(command.code).toBe(0);
  for (const name of IMPORTED) {
    expect(journey.digest(`interfaces/imported-interface/${name}`)).toBe(journey.digest(`interfaces/command-imported/${name}`));
  }
  expect(journey.digest("interfaces/imported-interface/profile.json")).toBe(before.get("local-profile.json"));
  expect(journey.digest("interfaces/imported-interface/package.json")).toBe(journey.digest("interfaces/interface-package.json"));

  // Importing into a directory that is already there — the window's own
  // import — is refused by both, and the import is left exactly as it was.
  const occupied = "cannot create profile import directory; destination must be new and parent writable";
  const written = IMPORTED.map((name) => journey.digest(`interfaces/imported-interface/${name}`));
  await importInto("interface-package.json", "imported-interface");
  expect(await panel.findByText(occupied)).toBeTruthy();
  const again = await journey.commandLine(["profile", "import", "interfaces/interface-package.json", "--output", "interfaces/imported-interface"]);
  expect(again.stderr).toBe(`readmit: ${occupied}\n`);
  expect(IMPORTED.map((name) => journey.digest(`interfaces/imported-interface/${name}`))).toEqual(written);

  // Nothing was activated: the documents beside the package, the saved-test
  // references among them, are unchanged, and the editor opened nothing.
  for (const [name, digest] of before) {
    expect(journey.digest(`interfaces/${name}`)).toBe(digest);
  }
  expect(journey.callsTo("OpenProfile")).toHaveLength(0);
  await press(user, panel.getByRole("tab", { name: "Profile Editor" }));
  expect((panel.getByLabelText("Profile ID") as HTMLInputElement).value).not.toBe(profile.profile.id);
});

test("an existing local profile opened in the profile panel resolves against its pinned pack and shows the seal it was imported with", async () => {
  const user = userEvent.setup();
  await receivedPackage();
  const imported = await journey.commandLine(["profile", "import", "interfaces/interface-package.json", "--output", "interfaces/imported"]);
  expect(imported.code).toBe(0);
  journey.placeFixture("profile-pack-adt.json", "interfaces/adt-pack.json");
  journey.placeFixture("local-profile-refused.json", "interfaces/refused-profile.json");
  const importedDigests = IMPORTED.map((name) => journey.digest(`interfaces/imported/${name}`));
  await journey.launch();
  // Retaining an unstored edit is licensed authoring; opening and importing
  // are not, as the first journey shows.
  await activateLicense(user, journey);
  const panel = await openInterfaces(user);
  const form = within(panel.getByRole("form", { name: "Open an existing profile" }));
  const profile = JSON.parse(journey.readFile("interfaces/imported/profile.json"));
  const sealed = JSON.parse(journey.readFile("interfaces/imported/version.json"));
  const pinned = `${profile.base.pack.id} v${profile.base.pack.version}`;
  const coverage = JSON.parse(journey.readFile("interfaces/imported/pack.json")).coverage.find(
    (row: { hl7_version: string; family: string }) => row.hl7_version === profile.base.hl7_version && row.family === profile.base.family,
  );

  // From the keyboard: the profile the command line imported, and the pack
  // beside it.
  await user.type(form.getByLabelText("Profile entry"), "imported/profile.json");
  await user.tab();
  expect(document.activeElement).toBe(form.getByLabelText("Pack entry"));
  await user.keyboard("imported/pack.json{Enter}");
  expect(await panel.findByRole("heading", { name: "Open imported/profile.json: completed" })).toBeTruthy();
  expect(panel.getByText(sealed.content.sha256)).toBeTruthy();
  expect(
    panel.getByText(
      `Resolved against the pinned pack ${pinned}: parse ${coverage.parse} · labels ${coverage.labels} · structural ${coverage.structural} · workflow ${coverage.workflow}`,
    ),
  ).toBeTruthy();
  expect((panel.getByLabelText("Profile ID") as HTMLInputElement).value).toBe(profile.profile.id);
  expect((panel.getByLabelText("Pinned Pack ID") as HTMLInputElement).value).toBe(profile.base.pack.id);

  // A pack the profile does not pin is read for nothing, and the window says
  // so while the seal stays the profile's own.
  await enter(user, form.getByLabelText("Pack entry"), "adt-pack.json");
  await press(user, form.getByRole("button", { name: "Open Profile" }));
  expect(
    await panel.findByText(`Not resolved against the pinned pack ${pinned}: no pack offered satisfies the pin, so nothing was read from one.`),
  ).toBeTruthy();
  expect(panel.getByText(/^pack_not_pinned:/)).toBeTruthy();
  expect(panel.getByText(sealed.content.sha256)).toBeTruthy();

  // A profile the reader refuses replaces nothing the editor holds.
  await enter(user, form.getByLabelText("Profile entry"), "refused-profile.json");
  await enter(user, form.getByLabelText("Pack entry"), "");
  await press(user, form.getByRole("button", { name: "Open Profile" }));
  expect(await panel.findByRole("heading", { name: "Open refused-profile.json: failed" })).toBeTruthy();
  expect((panel.getByLabelText("Profile ID") as HTMLInputElement).value).toBe(profile.profile.id);

  // An edit the window retained is never replaced by an open: the window
  // says what to do, and once the edit is discarded the profile opens.
  await enter(user, panel.getByLabelText("Profile ID"), "edited-in-the-window");
  expect(await panel.findByText("Retained. It will come back if this window stops.")).toBeTruthy();
  await enter(user, form.getByLabelText("Profile entry"), "imported/profile.json");
  await press(user, form.getByRole("button", { name: "Open Profile" }));
  expect((await form.findByRole("alert")).textContent).toMatch(/^This editor holds unstored edits\./);
  expect((panel.getByLabelText("Profile ID") as HTMLInputElement).value).toBe("edited-in-the-window");
  await press(user, panel.getByRole("tab", { name: "Canonical JSON" }));
  await press(user, panel.getByRole("button", { name: "Discard Unstored Edits" }));
  await press(user, panel.getByRole("tab", { name: "Profile Editor" }));
  await press(user, within(panel.getByRole("form", { name: "Open an existing profile" })).getByRole("button", { name: "Open Profile" }));
  await waitFor(() => expect((panel.getByLabelText("Profile ID") as HTMLInputElement).value).toBe(profile.profile.id));
  expect(panel.getByRole("heading", { name: "Open imported/profile.json: completed" })).toBeTruthy();

  // Opening read the import and changed none of it.
  expect(IMPORTED.map((name) => journey.digest(`interfaces/imported/${name}`))).toEqual(importedDigests);
});
