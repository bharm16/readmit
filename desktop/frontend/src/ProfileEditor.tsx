import { useEffect, useRef, useState, type FormEvent, type ReactNode } from "react";
import {
  cancel,
  compareProfiles,
  exportProfilePackage,
  importProfilePackage,
  inspectProfilePackage,
  inspectProfilePack,
  openProfile,
  openProfileLibrary,
  saveProfile,
  upgradeProfilePin,
  validateProfile,
  type AssessedTest,
  type Condition,
  type EditorDraft,
  type Field,
  type LocalProfile,
  type LocalProfileResolution,
  type LocalProfileResult,
  type ProfileCompareResult,
  type ProfileLibraryResult,
  type ProfilePackageResult,
  type ProfilePackResult,
  type ProfileUpgradePinResult,
  type Segment,
  type State,
} from "./bindings";
import { RetentionStatus, draftFor, useRetainer } from "./drafting";
import "./profile.css";

type TabId = "packs" | "editor" | "compare" | "exchange" | "raw";

const HL7_VERSIONS = ["2.3.1", "2.4", "2.5", "2.5.1", "2.6", "2.7.1", "2.8.2"];
const FAMILIES = ["ADT", "SIU", "ORM", "ORU"];
const USAGES = ["R", "RE", "O", "C", "X"];
const CONDITION_OPERATORS = ["present", "absent", "value_in"];
const DATA_TYPES = [
  "AD", "CE", "CF", "CNE", "CP", "CQ", "CWE", "CX", "DLN", "DR", "DT", "DTM",
  "ED", "EI", "EIP", "FN", "FT", "HD", "ID", "IS", "MO", "MSG", "NM", "PL",
  "PT", "RP", "SAD", "SI", "SN", "ST", "TM", "TS", "TX", "VID", "XAD", "XCN",
  "XON", "XPN", "XTN",
];

// The name the facade gives a package import while it runs, so this panel's
// cancel stops exactly the import it started.
const PROFILE_IMPORT = "profile-import";

// What an import that did not complete is called, by the state it answered.
function importHeading(state: State): string {
  switch (state) {
    case "failed":
      return "Import refused";
    case "cancelled":
      return "Import cancelled";
    case "permission_denied":
      return "Import denied";
    case "busy":
      return "Import not started";
    default:
      return `Import ${state}`;
  }
}

/** Whether the pack a profile pins answered its resolution, and with what. */
function PinStatus({ resolution }: { resolution: LocalProfileResolution }) {
  const pinned = `${resolution.base.pack.id} v${resolution.base.pack.version}`;
  const { parse, labels, structural, workflow } = resolution.support;
  return (
    <p className="pin-status">
      {resolution.pinned
        ? `Resolved against the pinned pack ${pinned}: parse ${parse} · labels ${labels} · structural ${structural} · workflow ${workflow}`
        : `Not resolved against the pinned pack ${pinned}: no pack offered satisfies the pin, so nothing was read from one.`}
    </p>
  );
}

/** One fact of what an import verified, as a term and its description. */
function Fact({ term, children }: { term: string; children: ReactNode }) {
  return (
    <>
      <dt>{term}</dt>
      <dd>{children}</dd>
    </>
  );
}

function emptyProfile(): LocalProfile {
  return {
    schema: "readmit-local-profile/v1",
    profile: { id: "local-siu-profile", version: "1" },
    base: {
      pack: { id: "fixture-siu", version: "1" },
      hl7_version: "2.5.1",
      family: "SIU",
    },
    segments: [
      {
        id: "SCH",
        description: "Schedule activity information",
        cardinality: { min: 1, max: "1" },
        fields: [
          {
            position: 1,
            name: "Placer appointment number",
            usage: "R",
            cardinality: { min: 1, max: "1" },
            type: "EI",
          },
        ],
      },
    ],
    terminology: [],
    authorities: [],
    dates: [],
  };
}

export function ProfileEditor({
  workspace,
  drafts,
  busy,
}: {
  workspace: string;
  drafts: EditorDraft[] | null;
  busy: boolean;
}) {
  const [activeTab, setActiveTab] = useState<TabId>("editor");
  const [rawJson, setRawJson] = useState("");
  const [profile, setProfile] = useState<LocalProfile>(emptyProfile());
  const [packEntry, setPackEntry] = useState("profile-pack.json");
  const [libraryDir, setLibraryDir] = useState("");
  const [saveOutput, setSaveOutput] = useState("profile-v1.json");
  const [sealOutput, setSealOutput] = useState("profile-v1-seal.json");

  // Pack inspection and library state
  const [packResult, setPackResult] = useState<ProfilePackResult | null>(null);
  const [libraryResult, setLibraryResult] = useState<ProfileLibraryResult | null>(null);

  // Profile validation and seal state
  const [profileResult, setProfileResult] = useState<LocalProfileResult | null>(null);
  // Which action the result below answers: a validation or save, or an open.
  const [resultLabel, setResultLabel] = useState("Validation Result");

  // Opening an existing profile
  const [openEntry, setOpenEntry] = useState("");
  const [openPack, setOpenPack] = useState("");
  const [openNotice, setOpenNotice] = useState<string | null>(null);

  // Version comparison and pin upgrade state
  const [compareFrom, setCompareFrom] = useState("profile-v1.json");
  const [compareTo, setCompareTo] = useState("profile-v2.json");
  const [compareRefs, setCompareRefs] = useState("references.json");
  const [compareResult, setCompareResult] = useState<ProfileCompareResult | null>(null);
  const [upgradeResult, setUpgradeResult] = useState<ProfileUpgradePinResult | null>(null);

  // Package exchange state
  const [pkgExportProfile, setPkgExportProfile] = useState("profile.json");
  const [pkgExportPack, setPkgExportPack] = useState("pack.json");
  const [pkgExportVersion, setPkgExportVersion] = useState("version.json");
  const [pkgExportOrigin, setPkgExportOrigin] = useState("origin.json");
  const [pkgExportOutput, setPkgExportOutput] = useState("package.json");
  const [pkgExportReviewed, setPkgExportReviewed] = useState(false);
  const [pkgImportFile, setPkgImportFile] = useState("package.json");
  const [pkgImportOutput, setPkgImportOutput] = useState("imported-profile");
  const [pkgInspectFile, setPkgInspectFile] = useState("package.json");
  const [packageResult, setPackageResult] = useState<ProfilePackageResult | null>(null);
  const [importResult, setImportResult] = useState<ProfilePackageResult | null>(null);
  const [importing, setImporting] = useState(false);

  const [pending, setPending] = useState(false);
  const retainer = useRetainer();
  const disabled = busy || pending;

  // A browser takes focus from a control it disables, so once an action
  // answers focus returns to the control that started it, and a running
  // import puts focus on its cancel.
  const openButton = useRef<HTMLButtonElement>(null);
  const importButton = useRef<HTMLButtonElement>(null);
  const cancelImportButton = useRef<HTMLButtonElement>(null);
  const [returnFocus, setReturnFocus] = useState<"open" | "import" | null>(null);
  useEffect(() => {
    if (importing) {
      cancelImportButton.current?.focus();
    }
  }, [importing]);
  useEffect(() => {
    if (pending || returnFocus === null) return;
    (returnFocus === "open" ? openButton : importButton).current?.focus();
    setReturnFocus(null);
  }, [pending, returnFocus]);

  // Restore draft on mount for this workspace
  const loaded = useRef<string | null>(null);
  useEffect(() => {
    if (loaded.current === workspace) return;
    loaded.current = workspace;
    const held = draftFor(drafts, "local-profile", workspace);
    if (held && typeof held.content === "string") {
      try {
        const parsed = JSON.parse(held.content);
        if (parsed && typeof parsed === "object") {
          setProfile(parsed as LocalProfile);
          setRawJson(held.content);
          retainer.keepId(held.id);
        }
      } catch {
        setRawJson(held.content);
        retainer.keepId(held.id);
      }
    } else {
      setRawJson(JSON.stringify(profile, null, 2));
    }
  }, [workspace, drafts, retainer]);

  // Synchronize draft save on edit
  function updateProfile(next: LocalProfile) {
    setProfile(next);
    const jsonStr = JSON.stringify(next, null, 2);
    setRawJson(jsonStr);
    retainer.save({
      id: "",
      kind: "local-profile",
      workspace,
      case: "",
      identity: `${next.profile.id}:${next.profile.version}`,
      content_schema: "readmit-local-profile/v1",
      content: jsonStr,
    });
  }

  function updateRawJson(next: string) {
    setRawJson(next);
    try {
      const parsed = JSON.parse(next);
      if (parsed && typeof parsed === "object" && parsed.schema === "readmit-local-profile/v1") {
        setProfile(parsed as LocalProfile);
      }
    } catch {
      // Keep typing in raw view
    }
    retainer.save({
      id: "",
      kind: "local-profile",
      workspace,
      case: "",
      identity: `${profile.profile.id}:${profile.profile.version}`,
      content_schema: "readmit-local-profile/v1",
      content: next,
    });
  }

  async function discardDraft() {
    setPending(true);
    try {
      if (!await retainer.dropCurrent()) return;
      const clean = emptyProfile();
      setProfile(clean);
      setRawJson(JSON.stringify(clean, null, 2));
      setProfileResult(null);
    } finally {
      setPending(false);
    }
  }

  // --- Facade Calls ---

  async function handleInspectPack() {
    setPending(true);
    try {
      const res = await inspectProfilePack(workspace, packEntry);
      setPackResult(res);
    } finally {
      setPending(false);
    }
  }

  async function handleOpenLibrary() {
    setPending(true);
    try {
      const res = await openProfileLibrary(workspace, libraryDir);
      setLibraryResult(res);
    } finally {
      setPending(false);
    }
  }

  async function handleOpenProfile(event: FormEvent) {
    event.preventDefault();
    // Opening replaces what the editor shows, so it never replaces edits
    // that are retained and not stored.
    if (retainer.currentId() !== "" || retainer.retention.state !== "idle") {
      setOpenNotice(
        "This editor holds unstored edits. Save them as a profile revision, or discard them with Discard Unstored Edits under Canonical JSON, before opening another profile.",
      );
      return;
    }
    setOpenNotice(null);
    setPending(true);
    try {
      const res = await openProfile(workspace, openEntry, openPack);
      setResultLabel(`Open ${openEntry}`);
      setProfileResult(res);
      if (res.state === "completed" && res.profile) {
        setProfile(res.profile);
        setRawJson(res.document ?? JSON.stringify(res.profile, null, 2));
      }
    } finally {
      setPending(false);
      setReturnFocus("open");
    }
  }

  async function handleValidateProfile() {
    setPending(true);
    try {
      const res = await validateProfile({
        workspace,
        document: rawJson,
        pack: packEntry,
      });
      setResultLabel("Validation Result");
      setProfileResult(res);
    } finally {
      setPending(false);
    }
  }

  async function handleSaveProfile() {
    setPending(true);
    try {
      const res = await saveProfile({
        workspace,
        document: rawJson,
        output: saveOutput,
        seal_output: sealOutput,
      });
      setResultLabel("Validation Result");
      setProfileResult(res);
      if (res.state === "completed") {
        const id = retainer.currentId();
        if (id !== "") {
          retainer.drop(id);
        }
        retainer.clear();
      }
    } finally {
      setPending(false);
    }
  }

  async function handleCompareProfiles() {
    setPending(true);
    try {
      const res = await compareProfiles({
        workspace,
        from: compareFrom,
        to: compareTo,
        references: compareRefs,
      });
      setCompareResult(res);
    } finally {
      setPending(false);
    }
  }

  async function handleUpgradePin(test: AssessedTest) {
    if (!compareResult?.comparison) return;
    setPending(true);
    try {
      const res = await upgradeProfilePin({
        workspace,
        references: compareRefs,
        test: test.test,
        was_pin: test.pinned,
        now_pin: {
          id: compareResult.comparison.profile,
          version: compareResult.comparison.to,
          sha256: profileResult?.seal?.content.sha256 ?? test.pinned.sha256,
        },
        output: compareRefs,
      });
      setUpgradeResult(res);
      if (res.state === "completed") {
        // Re-run comparison to update impacted tests view
        void handleCompareProfiles();
      }
    } finally {
      setPending(false);
    }
  }

  async function handleExportPackage() {
    setPending(true);
    try {
      const res = await exportProfilePackage({
        workspace,
        profile: pkgExportProfile,
        pack: pkgExportPack,
        version: pkgExportVersion,
        origin: pkgExportOrigin,
        output: pkgExportOutput,
        reviewed: pkgExportReviewed,
      });
      setPackageResult(res);
    } finally {
      setPending(false);
    }
  }

  async function handleImportPackage(event: FormEvent) {
    event.preventDefault();
    setImportResult(null);
    setPending(true);
    setImporting(true);
    try {
      const res = await importProfilePackage({
        workspace,
        package: pkgImportFile,
        output: pkgImportOutput,
      });
      setImportResult(res);
    } finally {
      setImporting(false);
      setPending(false);
      setReturnFocus("import");
    }
  }

  async function handleInspectPackage() {
    setPending(true);
    try {
      const res = await inspectProfilePackage(workspace, pkgInspectFile);
      setPackageResult(res);
    } finally {
      setPending(false);
    }
  }

  return (
    <section aria-labelledby="profile-editor-heading" className="profile-editor-section">
      <div className="profile-editor-header">
        <h3 id="profile-editor-heading">Interface Profiles & Metadata Packs</h3>
        <p className="hint">
          Model, validate, version and exchange local interface contracts. All constraints are strictly evaluated
          against pinned metadata packs with explicit origin tracking.
        </p>
      </div>

      <RetentionStatus
        retention={retainer.retention}
        onRetry={retainer.retry}
        onKeepAsNew={retainer.keepAsNew}
        onDiscard={discardDraft}
      />

      <nav aria-label="Profile navigation" className="profile-tabs" role="tablist">
        <button
          type="button"
          role="tab"
          aria-selected={activeTab === "editor"}
          className={`tab-btn ${activeTab === "editor" ? "active" : ""}`}
          onClick={() => setActiveTab("editor")}
        >
          Profile Editor
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={activeTab === "packs"}
          className={`tab-btn ${activeTab === "packs" ? "active" : ""}`}
          onClick={() => setActiveTab("packs")}
        >
          Installed Packs
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={activeTab === "compare"}
          className={`tab-btn ${activeTab === "compare" ? "active" : ""}`}
          onClick={() => setActiveTab("compare")}
        >
          Version Comparison & Pins
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={activeTab === "exchange"}
          className={`tab-btn ${activeTab === "exchange" ? "active" : ""}`}
          onClick={() => setActiveTab("exchange")}
        >
          Package Exchange
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={activeTab === "raw"}
          className={`tab-btn ${activeTab === "raw" ? "active" : ""}`}
          onClick={() => setActiveTab("raw")}
        >
          Canonical JSON
        </button>
      </nav>

      {/* TAB 1: LOCAL PROFILE EDITOR */}
      {activeTab === "editor" && (
        <div className="tab-panel" role="tabpanel" aria-label="Profile Editor">
          <form className="open-profile" aria-label="Open an existing profile" onSubmit={(e) => void handleOpenProfile(e)}>
            <h4>Open an Existing Profile</h4>
            <p className="hint">
              Opens a local profile from the workspace or one of its folders, such as a directory a package was imported
              into, resolves it against the pack it pins and shows its seal. Opening activates nothing.
            </p>
            <div className="pack-input-row">
              <label>
                <span>Profile entry</span>
                <input
                  type="text"
                  placeholder="profile.json or folder/profile.json"
                  value={openEntry}
                  disabled={disabled}
                  onChange={(e) => setOpenEntry(e.target.value)}
                />
              </label>
              <label>
                <span>Pack entry</span>
                <input
                  type="text"
                  placeholder="Empty for the pinned pack beside the profile"
                  value={openPack}
                  disabled={disabled}
                  onChange={(e) => setOpenPack(e.target.value)}
                />
              </label>
              <button type="submit" ref={openButton} disabled={disabled || !openEntry}>
                Open Profile
              </button>
            </div>
            {openNotice && (
              <p className="warning-text" role="alert">
                {openNotice}
              </p>
            )}
          </form>

          <div className="profile-meta-grid">
            <label>
              <span>Profile ID</span>
              <input
                type="text"
                aria-label="Profile ID"
                value={profile.profile.id}
                disabled={disabled}
                onChange={(e) =>
                  updateProfile({
                    ...profile,
                    profile: { ...profile.profile, id: e.target.value },
                  })
                }
              />
            </label>
            <label>
              <span>Version</span>
              <input
                type="text"
                aria-label="Profile Version"
                value={profile.profile.version}
                disabled={disabled}
                onChange={(e) =>
                  updateProfile({
                    ...profile,
                    profile: { ...profile.profile, version: e.target.value },
                  })
                }
              />
            </label>
            <label>
              <span>Pinned Pack ID</span>
              <input
                type="text"
                aria-label="Pinned Pack ID"
                value={profile.base.pack.id}
                disabled={disabled}
                onChange={(e) =>
                  updateProfile({
                    ...profile,
                    base: {
                      ...profile.base,
                      pack: { ...profile.base.pack, id: e.target.value },
                    },
                  })
                }
              />
            </label>
            <label>
              <span>Pinned Pack Version</span>
              <input
                type="text"
                aria-label="Pinned Pack Version"
                value={profile.base.pack.version}
                disabled={disabled}
                onChange={(e) =>
                  updateProfile({
                    ...profile,
                    base: {
                      ...profile.base,
                      pack: { ...profile.base.pack, version: e.target.value },
                    },
                  })
                }
              />
            </label>
            <label>
              <span>HL7 Version</span>
              <select
                aria-label="HL7 Version"
                value={profile.base.hl7_version}
                disabled={disabled}
                onChange={(e) =>
                  updateProfile({
                    ...profile,
                    base: { ...profile.base, hl7_version: e.target.value },
                  })
                }
              >
                {HL7_VERSIONS.map((v) => (
                  <option key={v} value={v}>
                    {v}
                  </option>
                ))}
              </select>
            </label>
            <label>
              <span>Message Family</span>
              <select
                aria-label="Message Family"
                value={profile.base.family}
                disabled={disabled}
                onChange={(e) =>
                  updateProfile({
                    ...profile,
                    base: { ...profile.base, family: e.target.value },
                  })
                }
              >
                {FAMILIES.map((f) => (
                  <option key={f} value={f}>
                    {f}
                  </option>
                ))}
              </select>
            </label>
          </div>

          <div className="section-toolbar">
            <h4>Constrained Segments & Fields</h4>
            <button
              type="button"
              disabled={disabled}
              onClick={() => {
                const id = prompt("Segment ID (e.g. SCH or ZPD):")?.trim().toUpperCase();
                if (!id) return;
                const newSeg: Segment = {
                  id,
                  description: id.startsWith("Z") ? "Site-defined Z-segment" : "Constrained segment",
                  cardinality: { min: 1, max: "1" },
                  fields: [],
                };
                updateProfile({ ...profile, segments: [...profile.segments, newSeg] });
              }}
            >
              + Add Segment
            </button>
          </div>

          {profile.segments.map((seg, segIdx) => {
            const isSiteDefined = seg.id.startsWith("Z");
            return (
              <div key={seg.id} className="segment-card" data-testid={`segment-${seg.id}`}>
                <div className="segment-header">
                  <strong>
                    {seg.id} {isSiteDefined && <span className="badge badge-site">Site-defined Z-segment</span>}
                  </strong>
                  <span className="segment-desc">{seg.description}</span>
                  <span className="cardinality-badge">
                    [{seg.cardinality?.min ?? 0}..{seg.cardinality?.max ?? "1"}]
                  </span>
                  <button
                    type="button"
                    className="danger-btn"
                    disabled={disabled}
                    onClick={() => {
                      const next = profile.segments.filter((_, idx) => idx !== segIdx);
                      updateProfile({ ...profile, segments: next });
                    }}
                  >
                    Remove Segment
                  </button>
                </div>

                <div className="fields-table-wrapper">
                  <table className="fields-table">
                    <thead>
                      <tr>
                        <th>Selector</th>
                        <th>Position</th>
                        <th>Name</th>
                        <th>Origin</th>
                        <th>Usage</th>
                        <th>Condition</th>
                        <th>Cardinality</th>
                        <th>Data Type</th>
                        <th>Terminology / Auth / Date</th>
                        <th>Action</th>
                      </tr>
                    </thead>
                    <tbody>
                      {seg.fields.map((fld, fldIdx) => {
                        const selector = `${seg.id}-${fld.position}`;
                        // Look up resolution if available
                        const resolvedSeg = profileResult?.resolution?.segments.find((s) => s.id === seg.id);
                        const resolvedFld = resolvedSeg?.fields.find((f) => f.position === fld.position);
                        const origin = resolvedFld?.usage_origin ?? "local";

                        return (
                          <tr key={fld.position} data-testid={`field-row-${selector}`}>
                            <td>
                              <code className="selector-tag">{selector}</code>
                            </td>
                            <td>{fld.position}</td>
                            <td>
                              <input
                                type="text"
                                aria-label={`Name for ${selector}`}
                                value={fld.name ?? ""}
                                disabled={disabled}
                                onChange={(e) => {
                                  const updated = [...seg.fields];
                                  updated[fldIdx] = { ...fld, name: e.target.value };
                                  const nextSegs = [...profile.segments];
                                  nextSegs[segIdx] = { ...seg, fields: updated };
                                  updateProfile({ ...profile, segments: nextSegs });
                                }}
                              />
                              {resolvedFld?.pack_name && resolvedFld.pack_name !== fld.name && (
                                <div className="pack-name-note">Pack: {resolvedFld.pack_name}</div>
                              )}
                            </td>
                            <td>
                              <span className={`badge badge-origin-${origin}`}>{origin}</span>
                            </td>
                            <td>
                              <select
                                aria-label={`Usage for ${selector}`}
                                value={fld.usage}
                                disabled={disabled}
                                 onChange={(e) => {
                                  const updated = [...seg.fields];
                                  const nextFld: Field = {
                                    ...fld,
                                    usage: e.target.value,
                                  };
                                  if (e.target.value === "C") {
                                    nextFld.condition = fld.condition ?? { segment: seg.id, position: 1, operator: "present" };
                                  } else {
                                    delete nextFld.condition;
                                  }
                                  updated[fldIdx] = nextFld;
                                  const nextSegs = [...profile.segments];
                                  nextSegs[segIdx] = { ...seg, fields: updated };
                                  updateProfile({ ...profile, segments: nextSegs });
                                }}
                              >
                                {USAGES.map((u) => (
                                  <option key={u} value={u}>
                                    {u}
                                  </option>
                                ))}
                              </select>
                            </td>
                            <td>
                              {fld.usage === "C" ? (
                                <div className="condition-inputs">
                                  <select
                                    aria-label={`Condition operator for ${selector}`}
                                    value={fld.condition?.operator ?? "present"}
                                    disabled={disabled}
                                    onChange={(e) => {
                                      const updated = [...seg.fields];
                                      const cond: Condition = {
                                        segment: fld.condition?.segment ?? seg.id,
                                        position: fld.condition?.position ?? 1,
                                        operator: e.target.value,
                                      };
                                      if (e.target.value === "value_in") {
                                        cond.values = fld.condition?.values ?? ["VAL"];
                                      }
                                      updated[fldIdx] = {
                                        ...fld,
                                        condition: cond,
                                      };
                                      const nextSegs = [...profile.segments];
                                      nextSegs[segIdx] = { ...seg, fields: updated };
                                      updateProfile({ ...profile, segments: nextSegs });
                                    }}
                                  >
                                    {CONDITION_OPERATORS.map((op) => (
                                      <option key={op} value={op}>
                                        {op}
                                      </option>
                                    ))}
                                  </select>
                                  <input
                                    type="text"
                                    placeholder="Seg (e.g. SCH)"
                                    style={{ width: "50px" }}
                                    aria-label={`Condition segment for ${selector}`}
                                    value={fld.condition?.segment ?? ""}
                                    onChange={(e) => {
                                      const updated = [...seg.fields];
                                      if (fld.condition) {
                                        updated[fldIdx] = {
                                          ...fld,
                                          condition: { ...fld.condition, segment: e.target.value },
                                        };
                                        const nextSegs = [...profile.segments];
                                        nextSegs[segIdx] = { ...seg, fields: updated };
                                        updateProfile({ ...profile, segments: nextSegs });
                                      }
                                    }}
                                  />
                                  <input
                                    type="number"
                                    placeholder="Pos"
                                    style={{ width: "45px" }}
                                    aria-label={`Condition position for ${selector}`}
                                    value={fld.condition?.position ?? 1}
                                    onChange={(e) => {
                                      const updated = [...seg.fields];
                                      if (fld.condition) {
                                        updated[fldIdx] = {
                                          ...fld,
                                          condition: { ...fld.condition, position: Number(e.target.value) },
                                        };
                                        const nextSegs = [...profile.segments];
                                        nextSegs[segIdx] = { ...seg, fields: updated };
                                        updateProfile({ ...profile, segments: nextSegs });
                                      }
                                    }}
                                  />
                                </div>
                              ) : (
                                <span className="text-muted">—</span>
                              )}
                            </td>
                            <td>
                              <input
                                type="text"
                                style={{ width: "60px" }}
                                aria-label={`Cardinality max for ${selector}`}
                                value={fld.cardinality?.max ?? "1"}
                                disabled={disabled}
                                onChange={(e) => {
                                  const updated = [...seg.fields];
                                  updated[fldIdx] = {
                                    ...fld,
                                    cardinality: { min: fld.cardinality?.min ?? 0, max: e.target.value },
                                  };
                                  const nextSegs = [...profile.segments];
                                  nextSegs[segIdx] = { ...seg, fields: updated };
                                  updateProfile({ ...profile, segments: nextSegs });
                                }}
                              />
                            </td>
                            <td>
                              <select
                                aria-label={`Data type for ${selector}`}
                                value={fld.type ?? ""}
                                disabled={disabled}
                                onChange={(e) => {
                                  const updated = [...seg.fields];
                                  const nextFld: Field = { ...fld };
                                  if (e.target.value) {
                                    nextFld.type = e.target.value;
                                  } else {
                                    delete nextFld.type;
                                  }
                                  updated[fldIdx] = nextFld;
                                  const nextSegs = [...profile.segments];
                                  nextSegs[segIdx] = { ...seg, fields: updated };
                                  updateProfile({ ...profile, segments: nextSegs });
                                }}
                              >
                                <option value="">(None)</option>
                                {DATA_TYPES.map((dt) => (
                                  <option key={dt} value={dt}>
                                    {dt}
                                  </option>
                                ))}
                              </select>
                            </td>
                            <td>
                              <input
                                type="text"
                                placeholder="Terminology / Auth / Date ID"
                                aria-label={`Linked rules for ${selector}`}
                                value={fld.terminology ?? fld.authority ?? fld.date ?? ""}
                                onChange={(e) => {
                                  const val = e.target.value;
                                  const updated = [...seg.fields];
                                  const nextFld: Field = { ...fld };
                                  if (val) {
                                    nextFld.terminology = val;
                                  } else {
                                    delete nextFld.terminology;
                                  }
                                  updated[fldIdx] = nextFld;
                                  const nextSegs = [...profile.segments];
                                  nextSegs[segIdx] = { ...seg, fields: updated };
                                  updateProfile({ ...profile, segments: nextSegs });
                                }}
                              />
                            </td>
                            <td>
                              <button
                                type="button"
                                className="danger-btn"
                                disabled={disabled}
                                onClick={() => {
                                  const updated = seg.fields.filter((_, idx) => idx !== fldIdx);
                                  const nextSegs = [...profile.segments];
                                  nextSegs[segIdx] = { ...seg, fields: updated };
                                  updateProfile({ ...profile, segments: nextSegs });
                                }}
                              >
                                ×
                              </button>
                            </td>
                          </tr>
                        );
                      })}
                    </tbody>
                  </table>
                  <button
                    type="button"
                    disabled={disabled}
                    onClick={() => {
                      const nextPos = seg.fields.length > 0 ? Math.max(...seg.fields.map((f) => f.position)) + 1 : 1;
                      const newFld: Field = {
                        position: nextPos,
                        name: `Field ${nextPos}`,
                        usage: "O",
                        cardinality: { min: 0, max: "1" },
                        type: "ST",
                      };
                      const updated = [...seg.fields, newFld];
                      const nextSegs = [...profile.segments];
                      nextSegs[segIdx] = { ...seg, fields: updated };
                      updateProfile({ ...profile, segments: nextSegs });
                    }}
                  >
                    + Add Field to {seg.id}
                  </button>
                </div>
              </div>
            );
          })}

          <div className="profile-actions-bar">
            <button
              type="button"
              className="primary-btn"
              disabled={disabled}
              onClick={() => void handleValidateProfile()}
            >
              Validate with Engine
            </button>
            <button
              type="button"
              disabled={disabled}
              onClick={() => void handleSaveProfile()}
            >
              Save Profile Revision
            </button>
            <label>
              <span>Output entry:</span>
              <input
                type="text"
                value={saveOutput}
                disabled={disabled}
                onChange={(e) => setSaveOutput(e.target.value)}
              />
            </label>
            <label>
              <span>Seal output:</span>
              <input
                type="text"
                value={sealOutput}
                disabled={disabled}
                onChange={(e) => setSealOutput(e.target.value)}
              />
            </label>
          </div>

          {profileResult && (
            <div className={`result-box result-${profileResult.state}`}>
              <h4>
                {resultLabel}: {profileResult.state}
              </h4>
              {profileResult.reason && <p className="error-text">{profileResult.reason}</p>}
              {profileResult.resolution && <PinStatus resolution={profileResult.resolution} />}
              {profileResult.seal && (
                <div className="seal-details">
                  <strong>Sealed Version:</strong> {profileResult.seal.profile.id} v{profileResult.seal.profile.version}
                  <br />
                  <strong>Document SHA-256:</strong> <code>{profileResult.seal.content.sha256}</code> ({profileResult.seal.content.bytes} bytes)
                </div>
              )}
              {profileResult.resolution?.findings && profileResult.resolution.findings.length > 0 && (
                <div className="findings-list">
                  <h5>Resolution Findings ({profileResult.resolution.findings.length})</h5>
                  <ul>
                    {profileResult.resolution.findings.map((f, idx) => (
                      <li key={idx}>
                        <strong>{f.kind}:</strong> {f.subject ? `${f.subject} — ` : ""}{f.detail}
                      </li>
                    ))}
                  </ul>
                </div>
              )}
            </div>
          )}
        </div>
      )}

      {/* TAB 2: INSTALLED PACKS */}
      {activeTab === "packs" && (
        <div className="tab-panel" role="tabpanel" aria-label="Installed Packs">
          <h4>Inspect Pack File</h4>
          <div className="pack-input-row">
            <label>
              <span>Pack Entry in Workspace:</span>
              <input
                type="text"
                value={packEntry}
                disabled={disabled}
                onChange={(e) => setPackEntry(e.target.value)}
              />
            </label>
            <button type="button" disabled={disabled || !packEntry} onClick={() => void handleInspectPack()}>
              Inspect Pack
            </button>
          </div>

          {packResult?.pack && (
            <div className="pack-details-card">
              <h4>Pack: {packResult.pack.id} (v{packResult.pack.version})</h4>
              {packResult.provenance && (
                <div className="provenance-section">
                  <h5>Provenance & Rights Review</h5>
                  <p><strong>Source:</strong> {packResult.provenance.source.name} ({packResult.provenance.source.location} @ {packResult.provenance.source.revision})</p>
                  <p><strong>License:</strong> {packResult.provenance.license.spdx} (Notice: {packResult.provenance.license.notice})</p>
                  <p><strong>Rights Review:</strong> <span className={`badge badge-${packResult.provenance.rights_review.status}`}>{packResult.provenance.rights_review.status}</span> (Ref: {packResult.provenance.rights_review.reference})</p>
                  <p><strong>Bundleable:</strong> {packResult.bundleable ? "Yes — approved for bundling" : "No — review pending or unapproved"}</p>
                </div>
              )}

              <h5>Declared Support Levels by Combination</h5>
              <table className="support-levels-table">
                <thead>
                  <tr>
                    <th>HL7 Version</th>
                    <th>Message Family</th>
                    <th>Lossless Parsing</th>
                    <th>Dictionary Labels</th>
                    <th>Structure Validation</th>
                    <th>Workflow Evaluation</th>
                  </tr>
                </thead>
                <tbody>
                  {packResult.coverage?.map((cov, idx) => (
                    <tr key={idx}>
                      <td>{cov.hl7_version}</td>
                      <td>{cov.family}</td>
                      <td><span className={`badge badge-${cov.parse}`}>{cov.parse}</span></td>
                      <td><span className={`badge badge-${cov.labels}`}>{cov.labels}</span></td>
                      <td><span className={`badge badge-${cov.structural}`}>{cov.structural}</span></td>
                      <td><span className={`badge badge-${cov.workflow}`}>{cov.workflow}</span></td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}

          <hr className="divider" />

          <h4>Open Pack Library Directory</h4>
          <div className="pack-input-row">
            <label>
              <span>Library Directory Path:</span>
              <input
                type="text"
                placeholder="One folder of the workspace, or empty for the workspace itself"
                value={libraryDir}
                disabled={disabled}
                onChange={(e) => setLibraryDir(e.target.value)}
              />
            </label>
            <button type="button" disabled={disabled} onClick={() => void handleOpenLibrary()}>
              Open Library
            </button>
          </div>

          {libraryResult && (
            <div className="library-results">
              <p>
                <strong>Status:</strong> {libraryResult.state} | <strong>Bundleable:</strong> {libraryResult.bundleable ? "Yes" : "No"} | <strong>Packs:</strong> {libraryResult.entries?.length ?? 0}
              </p>
              {libraryResult.matrix && libraryResult.matrix.length > 0 && (
                <table className="matrix-table">
                  <thead>
                    <tr>
                      <th>HL7 Version</th>
                      <th>Family</th>
                      <th>Parse</th>
                      <th>Labels</th>
                      <th>Structural</th>
                      <th>Workflow</th>
                      <th>Answering Pack</th>
                    </tr>
                  </thead>
                  <tbody>
                    {libraryResult.matrix.map((row, idx) => (
                      <tr key={idx}>
                        <td>{row.hl7_version}</td>
                        <td>{row.family}</td>
                        <td><span className={`badge badge-${row.parse}`}>{row.parse}</span></td>
                        <td><span className={`badge badge-${row.labels}`}>{row.labels}</span></td>
                        <td><span className={`badge badge-${row.structural}`}>{row.structural}</span></td>
                        <td><span className={`badge badge-${row.workflow}`}>{row.workflow}</span></td>
                        <td>{row.pack?.id ? `${row.pack.id} v${row.pack.version}` : "—"}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </div>
          )}
        </div>
      )}

      {/* TAB 3: VERSION COMPARISON & IMPACT */}
      {activeTab === "compare" && (
        <div className="tab-panel" role="tabpanel" aria-label="Version Comparison & Pins">
          <h4>Compare Profile Versions & Impacted Tests</h4>
          <p className="hint">
            Compare two versions of a profile to inspect differences. Evaluates pinned consumers against the references index.
            Approved profiles are immutable; historical test pins must be upgraded one test at a time.
          </p>

          <div className="compare-inputs-grid">
            <label>
              <span>From Profile Entry:</span>
              <input
                type="text"
                value={compareFrom}
                disabled={disabled}
                onChange={(e) => setCompareFrom(e.target.value)}
              />
            </label>
            <label>
              <span>To Profile Entry:</span>
              <input
                type="text"
                value={compareTo}
                disabled={disabled}
                onChange={(e) => setCompareTo(e.target.value)}
              />
            </label>
            <label>
              <span>References Index Entry:</span>
              <input
                type="text"
                value={compareRefs}
                disabled={disabled}
                onChange={(e) => setCompareRefs(e.target.value)}
              />
            </label>
            <button
              type="button"
              className="primary-btn"
              disabled={disabled || !compareFrom || !compareTo}
              onClick={() => void handleCompareProfiles()}
            >
              Compare & Assess Tests
            </button>
          </div>

          {compareResult && (
            <div className="compare-results">
              <h4>Differences ({compareResult.comparison?.changes.length ?? 0})</h4>
              {compareResult.comparison?.changes.map((c, idx) => (
                <div key={idx} className="diff-item">
                  <span className={`badge badge-kind-${c.kind}`}>{c.kind}</span>
                  <strong>{c.part}{c.subject ? ` (${c.subject})` : ""}:</strong> {c.detail}
                </div>
              ))}

              {upgradeResult && (
                <div className={`seal-box ${upgradeResult.state === "completed" ? "valid" : "invalid"}`} style={{ marginTop: "1rem", marginBottom: "1rem" }}>
                  <strong>Upgrade Pin: {upgradeResult.state}</strong>
                  {upgradeResult.reason && <p>{upgradeResult.reason}</p>}
                </div>
              )}

              <h4>Impacted Tests from Reference Index</h4>
              {compareResult.assessment?.tests && compareResult.assessment.tests.length > 0 ? (
                <table className="impact-table">
                  <thead>
                    <tr>
                      <th>Test Spec</th>
                      <th>Case</th>
                      <th>Pinned Version</th>
                      <th>Checksum</th>
                      <th>Impact</th>
                      <th>Action</th>
                    </tr>
                  </thead>
                  <tbody>
                    {compareResult.assessment.tests.map((test, idx) => (
                      <tr key={idx} className={`impact-row-${test.impact}`}>
                        <td>{test.test}</td>
                        <td>{test.case}</td>
                        <td>v{test.pinned.version}</td>
                        <td><code>{test.pinned.sha256.slice(0, 12)}…</code></td>
                        <td>
                          <span className={`badge badge-impact-${test.impact}`}>{test.impact}</span>
                        </td>
                        <td>
                          {test.impact === "affected" ? (
                            <button
                              type="button"
                              disabled={disabled}
                              onClick={() => void handleUpgradePin(test)}
                            >
                              Upgrade Pin
                            </button>
                          ) : (
                            <span className="text-muted">No upgrade needed</span>
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              ) : (
                <p className="hint">No tests referenced or no impact assessed.</p>
              )}
            </div>
          )}
        </div>
      )}

      {/* TAB 4: PACKAGE EXCHANGE */}
      {activeTab === "exchange" && (
        <div className="tab-panel" role="tabpanel" aria-label="Package Exchange">
          <h4>Export Profile Contract Package</h4>
          <p className="hint">
            Export creates an offline package holding a verified local profile, its pinned pack, its canonical version seal,
            and its reviewed origin. No patient evidence is included.
          </p>

          <div className="exchange-grid">
            <label>
              <span>Profile Entry:</span>
              <input
                type="text"
                value={pkgExportProfile}
                disabled={disabled}
                onChange={(e) => setPkgExportProfile(e.target.value)}
              />
            </label>
            <label>
              <span>Pack Entry:</span>
              <input
                type="text"
                value={pkgExportPack}
                disabled={disabled}
                onChange={(e) => setPkgExportPack(e.target.value)}
              />
            </label>
            <label>
              <span>Version Seal Entry:</span>
              <input
                type="text"
                value={pkgExportVersion}
                disabled={disabled}
                onChange={(e) => setPkgExportVersion(e.target.value)}
              />
            </label>
            <label>
              <span>Origin Entry:</span>
              <input
                type="text"
                value={pkgExportOrigin}
                disabled={disabled}
                onChange={(e) => setPkgExportOrigin(e.target.value)}
              />
            </label>
            <label>
              <span>Output Package File:</span>
              <input
                type="text"
                value={pkgExportOutput}
                disabled={disabled}
                onChange={(e) => setPkgExportOutput(e.target.value)}
              />
            </label>
          </div>

          <div className="review-confirmation">
            <label>
              <input
                type="checkbox"
                checked={pkgExportReviewed}
                disabled={disabled}
                onChange={(e) => setPkgExportReviewed(e.target.checked)}
              />
              <span>
                I confirm that metadata, licenses, and notices were reviewed for disclosure and external exchange.
              </span>
            </label>
          </div>

          <button
            type="button"
            className="primary-btn"
            disabled={disabled || !pkgExportReviewed || !pkgExportOutput}
            onClick={() => void handleExportPackage()}
          >
            Export Package
          </button>

          <hr className="divider" />

          <form aria-label="Import profile package" onSubmit={(e) => void handleImportPackage(e)}>
            <h4>Import Profile Package</h4>
            <p className="hint">
              Import verifies a package and writes its documents into a new directory of this workspace, as
              `readmit profile import` does. It refuses a directory that already exists and activates nothing.
            </p>
            <div className="exchange-grid">
              <label>
                <span>Package File:</span>
                <input
                  type="text"
                  value={pkgImportFile}
                  disabled={disabled}
                  onChange={(e) => setPkgImportFile(e.target.value)}
                />
              </label>
              <label>
                <span>Output Directory:</span>
                <input
                  type="text"
                  value={pkgImportOutput}
                  disabled={disabled}
                  onChange={(e) => setPkgImportOutput(e.target.value)}
                />
              </label>
            </div>
            <button type="submit" ref={importButton} disabled={disabled || !pkgImportFile || !pkgImportOutput}>
              Import Package
            </button>
            {importing && (
              <button type="button" ref={cancelImportButton} onClick={() => cancel(PROFILE_IMPORT)}>
                Cancel import
              </button>
            )}
          </form>

          {importResult && (
            <section className={`result-box result-${importResult.state}`} aria-label="Package import">
              {importResult.state === "completed" ? (
                <>
                  <h4>Imported into {importResult.output}</h4>
                  <p>
                    Verified, then written as profile.json, pack.json, version.json and origin.json, with package.json
                    last as the completion record.
                  </p>
                  <dl className="import-facts">
                    {importResult.profile && (
                      <Fact term="Profile">
                        {importResult.profile.id} v{importResult.profile.version}
                      </Fact>
                    )}
                    {importResult.pack && (
                      <Fact term="Pinned pack">
                        {importResult.pack.id} v{importResult.pack.version}
                      </Fact>
                    )}
                    {importResult.seal && (
                      <Fact term="Version seal">
                        {importResult.seal.profile.id} v{importResult.seal.profile.version} · SHA-256{" "}
                        <code>{importResult.seal.content.sha256}</code> · {importResult.seal.content.bytes} bytes
                      </Fact>
                    )}
                    {importResult.sha256 && (
                      <Fact term="Package SHA-256">
                        <code>{importResult.sha256}</code>
                      </Fact>
                    )}
                  </dl>
                  {importResult.origin && (
                    <>
                      <h5>Local origin</h5>
                      <dl className="import-facts">
                        <Fact term="Source format">{importResult.origin.source_format}</Fact>
                        <Fact term="Source">{importResult.origin.source}</Fact>
                        <Fact term="Revision">{importResult.origin.revision}</Fact>
                        <Fact term="License">{importResult.origin.license}</Fact>
                        <Fact term="Mapping limitations">{importResult.origin.mapping_limitations}</Fact>
                        <Fact term="Review reference">{importResult.origin.review_reference}</Fact>
                      </dl>
                      <details>
                        <summary>License notice</summary>
                        <p className="notice-text">{importResult.origin.notice}</p>
                      </details>
                    </>
                  )}
                  {importResult.provenance && (
                    <>
                      <h5>Pack provenance</h5>
                      <dl className="import-facts">
                        <Fact term="Pack source">
                          {importResult.provenance.source.name} · {importResult.provenance.source.location} @{" "}
                          {importResult.provenance.source.revision}
                        </Fact>
                        <Fact term="Pack license">{importResult.provenance.license.spdx}</Fact>
                        <Fact term="Rights review">
                          {importResult.provenance.rights_review.status} ({importResult.provenance.rights_review.reference})
                        </Fact>
                      </dl>
                    </>
                  )}
                  <p className="activation-note">
                    Nothing was activated: no project changed, no saved test was repinned, no message was evaluated and
                    the profile in the editor is unchanged. Open {importResult.output}/profile.json in the Profile Editor
                    to review it.
                  </p>
                </>
              ) : (
                <>
                  <h4>{importHeading(importResult.state)}</h4>
                  {importResult.reason && <p className="error-text">{importResult.reason}</p>}
                </>
              )}
            </section>
          )}

          <hr className="divider" />

          <h4>Inspect Package</h4>
          <div className="pack-input-row">
            <label>
              <span>Package File to Inspect:</span>
              <input
                type="text"
                value={pkgInspectFile}
                disabled={disabled}
                onChange={(e) => setPkgInspectFile(e.target.value)}
              />
            </label>
            <button
              type="button"
              disabled={disabled || !pkgInspectFile}
              onClick={() => void handleInspectPackage()}
            >
              Inspect Package
            </button>
          </div>

          {packageResult && (
            <div className={`result-box result-${packageResult.state}`}>
              <h4>Package Status: {packageResult.state}</h4>
              {packageResult.reason && <p className="error-text">{packageResult.reason}</p>}
              {packageResult.profile && (
                <p>
                  <strong>Profile:</strong> {packageResult.profile.id} v{packageResult.profile.version}
                </p>
              )}
              {packageResult.pack && (
                <p>
                  <strong>Pack Dependency:</strong> {packageResult.pack.id} v{packageResult.pack.version}
                </p>
              )}
              {packageResult.sha256 && (
                <p>
                  <strong>Package SHA-256:</strong> <code>{packageResult.sha256}</code>
                </p>
              )}
              {packageResult.rights && (
                <p>
                  <strong>Rights Review:</strong> {packageResult.rights}
                </p>
              )}
              {packageResult.conflict && (
                <p className="warning-text">
                  <strong>Conflict warning:</strong> {packageResult.conflict}
                </p>
              )}
            </div>
          )}
        </div>
      )}

      {/* TAB 5: CANONICAL RAW JSON */}
      {activeTab === "raw" && (
        <div className="tab-panel" role="tabpanel" aria-label="Canonical JSON">
          <h4>Raw Canonical Contract Document</h4>
          <textarea
            rows={22}
            className="code-textarea"
            aria-label="Raw Canonical JSON"
            value={rawJson}
            disabled={disabled}
            onChange={(e) => updateRawJson(e.target.value)}
          />
          <div className="raw-actions">
            <button
              type="button"
              className="primary-btn"
              disabled={disabled}
              onClick={() => void handleValidateProfile()}
            >
              Validate Canonical JSON
            </button>
            <button
              type="button"
              disabled={disabled}
              onClick={() => void discardDraft()}
            >
              Discard Unstored Edits
            </button>
          </div>
        </div>
      )}
    </section>
  );
}
