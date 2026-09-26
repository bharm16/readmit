import { useEffect, useRef, useState, type FormEvent, type ReactNode } from "react";
import {
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
  type LocalProfileAuthority,
  type LocalProfileBinding,
  type LocalProfileCardinality,
  type LocalProfileConditionOperator,
  type LocalProfileDateHandling,
  type LocalProfilePrecision,
  type LocalProfileResolution,
  type LocalProfileResult,
  type LocalProfileTerminologySet,
  type LocalProfileTimeZoneRule,
  type LocalProfileUsage,
  type ProfileCompareResult,
  type ProfileLibraryResult,
  type ProfilePackageResult,
  type ProfilePackResult,
  type ProfileUpgradePinResult,
  type ProfileVersionPin,
  type Segment,
  type State,
} from "./bindings";
import { RetentionStatus, draftFor, useRetainer } from "./drafting";
import "./profile.css";
import { useLifecycle } from "./lifecycle";

type TabId = "packs" | "editor" | "compare" | "exchange" | "raw";

const HL7_VERSIONS = ["2.3.1", "2.4", "2.5", "2.5.1", "2.6", "2.7.1", "2.8.2"];
const FAMILIES = ["ADT", "SIU", "ORM", "ORU"];
const DATA_TYPES = [
  "AD", "CE", "CF", "CNE", "CP", "CQ", "CWE", "CX", "DLN", "DR", "DT", "DTM",
  "ED", "EI", "EIP", "FN", "FT", "HD", "ID", "IS", "MO", "MSG", "NM", "PL",
  "PT", "RP", "SAD", "SI", "SN", "ST", "TM", "TS", "TX", "VID", "XAD", "XCN",
  "XON", "XPN", "XTN",
];
const UNIVERSAL_ID_TYPES = [
  "DNS", "GUID", "HCD", "HL7", "ISO", "L", "M", "N", "Random", "URI", "UUID", "x400", "x500",
];

// Each caption names the profile contract's own meaning of a code; the code
// itself is what the document keeps.
const USAGE_CAPTIONS: Record<LocalProfileUsage, string> = {
  R: "R — Required",
  RE: "RE — Required or empty",
  O: "O — Optional",
  C: "C — Conditional",
  X: "X — Not supported",
};
const OPERATOR_CAPTIONS: Record<LocalProfileConditionOperator, string> = {
  present: "Present",
  absent: "Absent",
  value_in: "One of these values",
};
const BINDING_CAPTIONS: Record<LocalProfileBinding, string> = {
  required: "Required",
  suggested: "Suggested",
};
const PRECISION_CAPTIONS: Record<LocalProfilePrecision, string> = {
  year: "Year",
  month: "Month",
  day: "Day",
  hour: "Hour",
  minute: "Minute",
  second: "Second",
  fraction: "Fraction",
};
const TIME_ZONE_CAPTIONS: Record<LocalProfileTimeZoneRule, string> = {
  required: "Required",
  optional: "Optional",
  forbidden: "Forbidden",
};

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

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

/** Whether a parsed document has every member the structured editor reads,
 * with the kind of value it reads there. This is no validation — the shared Go
 * reader decides what a profile may say — only a guard that a document merely
 * tagged with the schema is never handed to controls that would fail on a
 * missing member. */
function editableProfile(value: unknown): value is LocalProfile {
  if (!isRecord(value) || !isRecord(value.profile) || !isRecord(value.base) || !isRecord(value.base.pack)) {
    return false;
  }
  const texts = (record: Record<string, unknown>, ...keys: string[]) => keys.every((key) => typeof record[key] === "string");
  if (!texts(value.profile, "id", "version") || !texts(value.base, "hl7_version", "family") || !texts(value.base.pack, "id", "version")) {
    return false;
  }
  const cardinality = (item: unknown) => item === undefined || (isRecord(item) && typeof item.min === "number" && typeof item.max === "string");
  const list = (item: unknown, each: (entry: Record<string, unknown>) => boolean) =>
    item === undefined || (Array.isArray(item) && item.every((entry) => isRecord(entry) && each(entry)));
  const optionalText = (record: Record<string, unknown>, ...keys: string[]) =>
    keys.every((key) => record[key] === undefined || typeof record[key] === "string");
  if (!Array.isArray(value.segments)) {
    return false;
  }
  return (
    list(value.segments, (segment) =>
      typeof segment.id === "string" &&
      optionalText(segment, "description") &&
      cardinality(segment.cardinality) &&
      Array.isArray(segment.fields) &&
      list(segment.fields, (field) =>
        typeof field.position === "number" &&
        typeof field.usage === "string" &&
        optionalText(field, "name", "type", "terminology", "authority", "date") &&
        cardinality(field.cardinality) &&
        (field.condition === undefined ||
          (isRecord(field.condition) &&
            typeof field.condition.segment === "string" &&
            typeof field.condition.position === "number" &&
            typeof field.condition.operator === "string" &&
            (field.condition.values === undefined ||
              (Array.isArray(field.condition.values) && field.condition.values.every((entry) => typeof entry === "string"))))),
      ),
    ) &&
    list(value.terminology, (set) =>
      typeof set.id === "string" &&
      typeof set.binding === "string" &&
      optionalText(set, "description") &&
      list(set.codes, (code) => typeof code.code === "string" && optionalText(code, "display")) &&
      Array.isArray(set.codes),
    ) &&
    list(value.authorities, (authority) =>
      typeof authority.id === "string" && optionalText(authority, "description", "namespace", "universal_id", "universal_id_type"),
    ) &&
    list(value.dates, (date) =>
      typeof date.id === "string" && typeof date.precision === "string" && typeof date.timezone === "string" && optionalText(date, "description"),
    )
  );
}

/** A copy of a record with one optional text member set, or left out when it
 * is empty: an optional member the person cleared is not written as "". */
function withText<T extends object>(record: T, key: keyof T & string, value: string): T {
  const next = { ...record } as Record<string, unknown>;
  if (value === "") {
    delete next[key];
  } else {
    next[key] = value;
  }
  return next as T;
}

/** The selector of one field: its segment and position. */
function selectorOf(segment: Segment, field: Field): string {
  return `${segment.id}-${field.position}`;
}

/** The exact identity of a pin, as a person compares two of them. */
function pinText(pin: ProfileVersionPin): string {
  return `${pin.id} version ${pin.version} · SHA-256 ${pin.sha256}`;
}

/** A whole-number input that keeps what is typed while it is not yet a
 * number, such as while it is cleared to type another, and commits only a
 * whole number. */
function WholeNumber({
  value,
  min,
  disabled,
  describedBy,
  onCommit,
}: {
  /** Undefined until the person enters a number: none is assumed. */
  value: number | undefined;
  min: number;
  disabled?: boolean;
  describedBy?: string;
  onCommit: (value: number) => void;
}) {
  const [text, setText] = useState(value === undefined ? "" : String(value));
  useEffect(() => {
    setText((typed) => (value === undefined ? typed : typed !== "" && Number(typed) === value ? typed : String(value)));
  }, [value]);
  return (
    <input
      type="number"
      min={min}
      value={text}
      disabled={disabled}
      aria-describedby={describedBy}
      onChange={(e) => {
        setText(e.target.value);
        if (/^[0-9]+$/.test(e.target.value)) {
          onCommit(Number(e.target.value));
        }
      }}
    />
  );
}

/** How many times a segment or field may repeat: not specified at all, or a
 * minimum with a maximum that is a count or unbounded ("*"). Neither "not
 * specified" nor "unbounded" is written as zero. */
function Repetitions({
  group,
  value,
  disabled,
  onChange,
}: {
  group: string;
  value: LocalProfileCardinality | undefined;
  disabled: boolean;
  onChange: (next: LocalProfileCardinality | undefined) => void;
}) {
  const unbounded = value?.max === "*";
  return (
    <fieldset className="repetitions" disabled={disabled}>
      <legend>Repetitions</legend>
      <label className="inline-choice">
        <input type="radio" name={group} checked={value === undefined} onChange={() => onChange(undefined)} />
        Not specified
      </label>
      <label className="inline-choice">
        <input
          type="radio"
          name={group}
          checked={value !== undefined}
          onChange={() => onChange(value ?? { min: 0, max: "1" })}
        />
        Specified
      </label>
      {value !== undefined && (
        <div className="repetition-bounds">
          <label>
            <span>Minimum</span>
            <WholeNumber value={value.min} min={0} onCommit={(min) => onChange({ ...value, min })} />
          </label>
          <label>
            <span>Maximum</span>
            <input
              type="number"
              min={0}
              value={unbounded ? "" : value.max}
              disabled={disabled || unbounded}
              onChange={(e) => onChange({ ...value, max: e.target.value })}
            />
          </label>
          <label className="inline-choice">
            <input
              type="checkbox"
              checked={unbounded}
              onChange={(e) => onChange({ ...value, max: e.target.checked ? "*" : "" })}
            />
            Unbounded
          </label>
        </div>
      )}
    </fieldset>
  );
}

/** Which result the result box answers: a validation, a save, or an open. */
type ResultKind = { kind: "validation" } | { kind: "save" } | { kind: "open"; entry: string };

function resultHeading(kind: ResultKind, state: State): string {
  switch (kind.kind) {
    case "validation":
      return `Validation: ${state}`;
    case "save":
      return state === "completed" ? "Saved revision" : `Save result: ${state}`;
    case "open":
      return `Open ${kind.entry}: ${state}`;
  }
}

/** One answered comparison and the exact inputs it answered for. An update of
 * a test pin is bound to exactly this: changing any input withdraws it. */
interface Compared {
  from: string;
  to: string;
  references: string;
  result: ProfileCompareResult;
}

/** An answered pin update, with the exact identities it moved between. */
interface PinUpdate {
  test: string;
  was: ProfileVersionPin;
  now: ProfileVersionPin;
  result: ProfileUpgradePinResult;
}

/** A condition as the person authors it: a member not yet entered is blank,
 * never a guessed value. */
type AuthoredCondition = Omit<Condition, "operator"> & { operator: LocalProfileConditionOperator | "" };

/** The kinds of rule a field references by ID, with the names they are shown
 * under. */
type RuleKind = "terminology" | "authority" | "date";
const RULE_NAMES: Record<RuleKind, { one: string; field: string }> = {
  terminology: { one: "Terminology set", field: "terminology set" },
  authority: { one: "Assigning authority", field: "assigning authority" },
  date: { one: "Date rule", field: "date rule" },
};

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
  // Whether the canonical JSON is a document the structured editor can show.
  // While it is not, the structured controls are withheld, so they can never
  // write the last editable profile over text a person is still fixing.
  const [structured, setStructured] = useState(true);
  const [packEntry, setPackEntry] = useState("profile-pack.json");
  const [libraryDir, setLibraryDir] = useState("");
  const [saveOutput, setSaveOutput] = useState("profile-v1.json");
  const [sealOutput, setSealOutput] = useState("profile-v1-seal.json");

  // The field whose clauses the detail panel edits, by segment and field index.
  const [selected, setSelected] = useState<{ segment: number; field: number } | null>(null);
  // The segment being added, while its row is open.
  const [newSegment, setNewSegment] = useState<{ id: string; description: string } | null>(null);
  // The segment whose removal is awaiting confirmation.
  const [confirmingRemoval, setConfirmingRemoval] = useState<number | null>(null);
  // Why a rule could not be removed: the fields that still reference it.
  const [ruleNotice, setRuleNotice] = useState<{ kind: RuleKind; text: string } | null>(null);
  // The control focus moves to once the edit that removed the focused one lands.
  const [focusAfter, setFocusAfter] = useState<string | null>(null);

  // Pack inspection and library state
  const [packResult, setPackResult] = useState<ProfilePackResult | null>(null);
  const [libraryResult, setLibraryResult] = useState<ProfileLibraryResult | null>(null);

  // Profile validation and seal state
  const [profileResult, setProfileResult] = useState<LocalProfileResult | null>(null);
  // Which action the result below answers: a validation, a save, or an open.
  const [resultKind, setResultKind] = useState<ResultKind>({ kind: "validation" });

  // Opening an existing profile
  const [openEntry, setOpenEntry] = useState("");
  const [openPack, setOpenPack] = useState("");
  const [openNotice, setOpenNotice] = useState<string | null>(null);

  // Version comparison and pin update state
  const [compareFrom, setCompareFrom] = useState("profile-v1.json");
  const [compareTo, setCompareTo] = useState("profile-v2.json");
  const [compareRefs, setCompareRefs] = useState("references.json");
  const [compared, setCompared] = useState<Compared | null>(null);
  const [pinUpdate, setPinUpdate] = useState<PinUpdate | null>(null);

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

  // A browser takes focus from a control it disables, so once an action
  // answers focus returns to the control that started it — for opening and
  // importing, the form's own button, whichever field Enter was pressed in —
  // and a running import puts focus on its cancel. The import runs under the
  // facade's profile-import operation, which that cancel stops.
  const openButton = useRef<HTMLButtonElement>(null);
  const importButton = useRef<HTMLButtonElement>(null);
  const cancelImportButton = useRef<HTMLButtonElement>(null);
  const lifecycle = useLifecycle<"working" | "opening" | "importing">({
    names: { importing: "profile-import" },
    stops: { importing: cancelImportButton },
    origins: { opening: openButton, importing: importButton },
  });
  const importing = lifecycle.running === "importing";
  const pending = lifecycle.running !== null;
  const retainer = useRetainer();
  const disabled = busy || pending;

  useEffect(() => {
    if (focusAfter === null) return;
    document.getElementById(focusAfter)?.focus();
    setFocusAfter(null);
  }, [focusAfter]);

  // Restore draft on mount for this workspace
  const loaded = useRef<string | null>(null);
  useEffect(() => {
    if (loaded.current === workspace) return;
    loaded.current = workspace;
    const held = draftFor(drafts, "local-profile", workspace);
    if (held && typeof held.content === "string") {
      adoptRaw(held.content);
      retainer.keepId(held.id);
    } else {
      setRawJson(JSON.stringify(profile, null, 2));
    }
  }, [workspace, drafts, retainer]);

  /** Shows raw text, and the profile it holds when that is one the structured
   * editor can show. */
  function adoptRaw(text: string) {
    setRawJson(text);
    try {
      const parsed: unknown = JSON.parse(text);
      if (editableProfile(parsed)) {
        setProfile(parsed);
        setStructured(true);
        return;
      }
    } catch {
      // Keep typing in raw view
    }
    setStructured(false);
    setSelected(null);
  }

  // Synchronize draft save on edit. An edit withdraws the result of an
  // earlier validation, save or open: it described other content.
  function updateProfile(next: LocalProfile) {
    setProfile(next);
    setStructured(true);
    setProfileResult(null);
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
    adoptRaw(next);
    setProfileResult(null);
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

  function updateSegment(segIdx: number, next: Segment) {
    const segments = [...profile.segments];
    segments[segIdx] = next;
    updateProfile({ ...profile, segments });
  }

  function updateField(segIdx: number, fldIdx: number, next: Field) {
    const segment = profile.segments[segIdx]!;
    const fields = [...segment.fields];
    fields[fldIdx] = next;
    updateSegment(segIdx, { ...segment, fields });
  }

  function removeSegment(segIdx: number) {
    setConfirmingRemoval(null);
    setSelected(null);
    updateProfile({ ...profile, segments: profile.segments.filter((_, idx) => idx !== segIdx) });
    setFocusAfter("profile-add-segment");
  }

  function addSegment() {
    if (!newSegment) return;
    const id = newSegment.id.trim().toUpperCase();
    if (!id) return;
    const segment: Segment = { id, cardinality: { min: 1, max: "1" }, fields: [] };
    const description = newSegment.description.trim();
    updateProfile({
      ...profile,
      segments: [...profile.segments, description ? { ...segment, description } : segment],
    });
    setNewSegment(null);
    setFocusAfter("profile-add-segment");
  }

  function addField(segIdx: number) {
    const segment = profile.segments[segIdx]!;
    const position = segment.fields.length > 0 ? Math.max(...segment.fields.map((f) => f.position)) + 1 : 1;
    updateSegment(segIdx, { ...segment, fields: [...segment.fields, { position, usage: "O" }] });
    setSelected({ segment: segIdx, field: segment.fields.length });
    setFocusAfter("profile-field-detail-heading");
  }

  function removeField(segIdx: number, fldIdx: number) {
    const segment = profile.segments[segIdx]!;
    updateSegment(segIdx, { ...segment, fields: segment.fields.filter((_, idx) => idx !== fldIdx) });
    setSelected(null);
    setFocusAfter(`profile-add-field-${segIdx}`);
  }

  /** The selectors of every field that references one rule by its ID. */
  function usersOf(kind: RuleKind, id: string): string[] {
    if (id === "") return [];
    return profile.segments.flatMap((segment) =>
      segment.fields.filter((field) => field[kind] === id).map((field) => selectorOf(segment, field)),
    );
  }

  /** Removes one rule unless a field still references it; then the fields are
   * named and nothing is cleared or retargeted. */
  function removeRule(kind: RuleKind, id: string, remove: () => void) {
    const users = usersOf(kind, id);
    if (users.length > 0) {
      setRuleNotice({
        kind,
        text: `${RULE_NAMES[kind].one} ${id} is used by ${users.join(", ")}. Choose another ${RULE_NAMES[kind].field}, or none, for those fields before removing it.`,
      });
      return;
    }
    setRuleNotice(null);
    remove();
  }

  function updateTerminology(idx: number, next: LocalProfileTerminologySet) {
    const terminology = [...(profile.terminology ?? [])];
    terminology[idx] = next;
    updateProfile({ ...profile, terminology });
  }

  function updateAuthority(idx: number, next: LocalProfileAuthority) {
    const authorities = [...(profile.authorities ?? [])];
    authorities[idx] = next;
    updateProfile({ ...profile, authorities });
  }

  function updateDate(idx: number, next: LocalProfileDateHandling) {
    const dates = [...(profile.dates ?? [])];
    dates[idx] = next;
    updateProfile({ ...profile, dates });
  }

  async function discardDraft() {
    await lifecycle.run("working", async () => {
      if (!await retainer.dropCurrent()) return;
      const clean = emptyProfile();
      setProfile(clean);
      setStructured(true);
      setSelected(null);
      setRawJson(JSON.stringify(clean, null, 2));
      setProfileResult(null);
    });
  }

  // --- Facade Calls ---

  async function handleInspectPack() {
    await lifecycle.run("working", async () => {
      const res = await inspectProfilePack(workspace, packEntry);
      setPackResult(res);
    });
  }

  async function handleOpenLibrary() {
    await lifecycle.run("working", async () => {
      const res = await openProfileLibrary(workspace, libraryDir);
      setLibraryResult(res);
    });
  }

  async function handleOpenProfile(event: FormEvent) {
    event.preventDefault();
    // Opening replaces what the editor shows, so it never replaces edits
    // that are retained and not stored.
    if (retainer.currentId() !== "" || retainer.retention.state !== "idle") {
      setOpenNotice(
        "This editor holds unstored edits. Save them as a profile revision, or discard them with Discard changes under Canonical JSON, before opening another profile.",
      );
      return;
    }
    setOpenNotice(null);
    await lifecycle.run("opening", async () => {
      const res = await openProfile(workspace, openEntry, openPack);
      setResultKind({ kind: "open", entry: openEntry });
      setProfileResult(res);
      if (res.state === "completed" && res.profile) {
        setProfile(res.profile);
        setStructured(true);
        setSelected(null);
        setRawJson(res.document ?? JSON.stringify(res.profile, null, 2));
      }
    });
  }

  async function handleValidateProfile() {
    await lifecycle.run("working", async () => {
      const res = await validateProfile({
        workspace,
        document: rawJson,
        pack: packEntry,
      });
      setResultKind({ kind: "validation" });
      setProfileResult(res);
    });
  }

  async function handleSaveProfile() {
    await lifecycle.run("working", async () => {
      const res = await saveProfile({
        workspace,
        document: rawJson,
        output: saveOutput,
        seal_output: sealOutput,
      });
      setResultKind({ kind: "save" });
      setProfileResult(res);
      if (res.state === "completed") {
        const id = retainer.currentId();
        if (id !== "") {
          retainer.drop(id);
        }
        retainer.clear();
      }
    });
  }

  async function handleCompareProfiles() {
    const inputs = { from: compareFrom, to: compareTo, references: compareRefs };
    await lifecycle.run("working", async () => {
      setPinUpdate(null);
      const result = await compareProfiles({ workspace, ...inputs });
      setCompared({ ...inputs, result });
    });
  }

  /** Changing a compared profile or the references file withdraws the
   * comparison and every action bound to it. */
  function withdrawComparison() {
    setCompared(null);
    setPinUpdate(null);
  }

  async function handleUpdatePin(test: AssessedTest) {
    const bound = compared;
    // The comparison decided this pin in Go; it is only passed back.
    const now = bound?.result.later_pin?.pin;
    if (!bound || !now) return;
    await lifecycle.run("working", async () => {
      const result = await upgradeProfilePin({
        workspace,
        references: bound.references,
        test: test.test,
        was_pin: test.pinned,
        now_pin: now,
        output: bound.references,
      });
      setPinUpdate({ test: test.test, was: test.pinned, now, result });
      if (result.state === "completed") {
        // The references file changed, so the assessment no longer describes it.
        setCompared(null);
      }
    });
  }

  /** An export input changed, so the disclosure confirmation given for the
   * previous inputs no longer holds. */
  function exportInput(set: (value: string) => void) {
    return (value: string) => {
      set(value);
      setPkgExportReviewed(false);
    };
  }

  async function handleExportPackage() {
    await lifecycle.run("working", async () => {
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
    });
  }

  async function handleImportPackage(event: FormEvent) {
    event.preventDefault();
    setImportResult(null);
    await lifecycle.run("importing", async () => {
      const res = await importProfilePackage({
        workspace,
        package: pkgImportFile,
        output: pkgImportOutput,
      });
      setImportResult(res);
    });
  }

  async function handleInspectPackage() {
    await lifecycle.run("working", async () => {
      const res = await inspectProfilePackage(workspace, pkgInspectFile);
      setPackageResult(res);
    });
  }

  /** A field's reference to one rule collection, by the rule's real ID. The
   * select writes only its own member; a reference to an ID the profile does
   * not declare stays selected, named as undeclared, rather than cleared. */
  function RuleReference({ kind, segIdx, fldIdx }: { kind: RuleKind; segIdx: number; fldIdx: number }) {
    const field = profile.segments[segIdx]!.fields[fldIdx]!;
    const current = field[kind] ?? "";
    const declared = (kind === "terminology" ? profile.terminology : kind === "authority" ? profile.authorities : profile.dates) ?? [];
    const ids = declared.map((rule) => rule.id).filter((id) => id !== "");
    return (
      <label>
        <span>{RULE_NAMES[kind].one}</span>
        <select
          value={current}
          disabled={disabled}
          onChange={(e) => updateField(segIdx, fldIdx, withText(field, kind, e.target.value))}
        >
          <option value="">None</option>
          {ids.map((id) => (
            <option key={id} value={id}>
              {id}
            </option>
          ))}
          {current !== "" && !ids.includes(current) && <option value={current}>{`${current} (not declared)`}</option>}
        </select>
      </label>
    );
  }

  /** Every clause of the selected field: where it is, what it is called and
   * what the profile requires of it. */
  function renderFieldDetail() {
    if (!selected) return null;
    const segment = profile.segments[selected.segment];
    const field = segment?.fields[selected.field];
    if (!segment || !field) return null;
    const { segment: segIdx, field: fldIdx } = selected;
    const selector = selectorOf(segment, field);
    const resolved = profileResult?.resolution?.segments
      .find((s) => s.id === segment.id)
      ?.fields.find((f) => f.position === field.position);
    const set = (next: Field) => updateField(segIdx, fldIdx, next);
    // A conditional field's condition is the person's to author: until they
    // enter some of it there is none, and what they have not entered stays
    // blank. The shared reader refuses either at validation and save, saying
    // what is missing.
    const condition: AuthoredCondition = field.condition ?? { segment: "", position: 0, operator: "" };
    const setCondition = (next: AuthoredCondition) => set({ ...field, condition: next as Condition });
    return (
      <section key={`${segIdx}-${fldIdx}`} className="field-detail" aria-labelledby="profile-field-detail-heading">
        <h5 id="profile-field-detail-heading" tabIndex={-1}>
          Field {selector}
        </h5>
        <div className="field-detail-grid">
          <label>
            <span>Position</span>
            <WholeNumber
              value={field.position}
              min={1}
              disabled={disabled}
              describedBy="profile-position-help"
              onCommit={(position) => set({ ...field, position })}
            />
          </label>
          <p id="profile-position-help" className="hint">
            Any position of the segment; the preceding positions need not be listed. Validation refuses a position
            that is repeated or out of range.
          </p>
          <label>
            <span>Field name</span>
            <input
              type="text"
              value={field.name ?? ""}
              disabled={disabled}
              onChange={(e) => set(withText(field, "name", e.target.value))}
            />
          </label>
          {resolved?.pack_name && resolved.pack_name !== field.name && (
            <p className="pack-name-note">Pinned pack label: {resolved.pack_name}</p>
          )}
          <label>
            <span>Usage</span>
            <select
              value={field.usage}
              disabled={disabled}
              onChange={(e) => {
                const usage = e.target.value as LocalProfileUsage;
                const next: Field = { ...field, usage };
                if (usage !== "C") {
                  delete next.condition;
                }
                set(next);
              }}
            >
              {(Object.keys(USAGE_CAPTIONS) as LocalProfileUsage[]).map((usage) => (
                <option key={usage} value={usage}>
                  {USAGE_CAPTIONS[usage]}
                </option>
              ))}
            </select>
          </label>
        </div>

        {field.usage === "C" && (
          <fieldset className="condition-inputs" disabled={disabled}>
            <legend>Condition</legend>
            <label>
              <span>Operator</span>
              <select
                value={condition.operator}
                onChange={(e) => {
                  const operator = e.target.value as LocalProfileConditionOperator;
                  const next: AuthoredCondition = { segment: condition.segment, position: condition.position, operator };
                  if (operator === "value_in") {
                    next.values = condition.values ?? [];
                  }
                  setCondition(next);
                }}
              >
                {condition.operator === "" && (
                  <option value="" disabled>
                    Choose an operator
                  </option>
                )}
                {(Object.keys(OPERATOR_CAPTIONS) as LocalProfileConditionOperator[]).map((operator) => (
                  <option key={operator} value={operator}>
                    {OPERATOR_CAPTIONS[operator]}
                  </option>
                ))}
              </select>
            </label>
            <p className="hint">
              Present: the named position carries a value. Absent: the named position is omitted, empty or explicitly
              null. One of these values: the named position carries one of the allowed values.
            </p>
            <label>
              <span>Condition segment</span>
              <input
                type="text"
                value={condition.segment}
                aria-describedby="profile-condition-segment-help"
                onChange={(e) => setCondition({ ...condition, segment: e.target.value })}
              />
            </label>
            <p id="profile-condition-segment-help" className="hint">
              For example SCH.
            </p>
            <label>
              <span>Condition field position</span>
              <WholeNumber
                value={condition.position === 0 ? undefined : condition.position}
                min={1}
                onCommit={(position) => setCondition({ ...condition, position })}
              />
            </label>
            {condition.operator === "value_in" && (
              <fieldset className="allowed-values">
                <legend>Allowed values</legend>
                {(condition.values ?? []).map((value, idx) => (
                  <div key={idx} className="allowed-value-row">
                    <label>
                      <span>Value {idx + 1}</span>
                      <input
                        type="text"
                        value={value}
                        onChange={(e) => {
                          const values = [...(condition.values ?? [])];
                          values[idx] = e.target.value;
                          setCondition({ ...condition, values });
                        }}
                      />
                    </label>
                    <button
                      type="button"
                      onClick={() =>
                        setCondition({ ...condition, values: (condition.values ?? []).filter((_, at) => at !== idx) })
                      }
                    >
                      Remove value {idx + 1}
                    </button>
                  </div>
                ))}
                <button
                  type="button"
                  onClick={() => setCondition({ ...condition, values: [...(condition.values ?? []), ""] })}
                >
                  Add value
                </button>
              </fieldset>
            )}
          </fieldset>
        )}

        <Repetitions
          group="profile-field-repetitions"
          value={field.cardinality}
          disabled={disabled}
          onChange={(cardinality) => {
            const next: Field = { ...field };
            if (cardinality) {
              next.cardinality = cardinality;
            } else {
              delete next.cardinality;
            }
            set(next);
          }}
        />

        <div className="field-detail-grid">
          <label>
            <span>Data type</span>
            <select
              value={field.type ?? ""}
              disabled={disabled}
              onChange={(e) => set(withText(field, "type", e.target.value))}
            >
              <option value="">Not specified</option>
              {DATA_TYPES.map((dt) => (
                <option key={dt} value={dt}>
                  {dt}
                </option>
              ))}
              {field.type && !DATA_TYPES.includes(field.type) && <option value={field.type}>{field.type}</option>}
            </select>
          </label>
          {RuleReference({ kind: "terminology", segIdx, fldIdx })}
          {RuleReference({ kind: "authority", segIdx, fldIdx })}
          {RuleReference({ kind: "date", segIdx, fldIdx })}
        </div>
      </section>
    );
  }

  /** The three rule collections a field references by ID. */
  function renderRules() {
    const terminology = profile.terminology ?? [];
    const authorities = profile.authorities ?? [];
    const dates = profile.dates ?? [];
    const notice = (kind: RuleKind) =>
      ruleNotice?.kind === kind && (
        <p className="warning-text" role="alert">
          {ruleNotice.text}
        </p>
      );
    const usedBy = (kind: RuleKind, id: string, helpId: string) => {
      const users = usersOf(kind, id);
      return users.length > 0 ? (
        <p id={helpId} className="hint">
          Used by {users.join(", ")}. Choose another {RULE_NAMES[kind].field}, or none, for those fields before
          changing this ID or removing it.
        </p>
      ) : null;
    };
    return (
      <>
        <section className="rule-collection" aria-labelledby="profile-terminology-heading">
          <h4 id="profile-terminology-heading">Terminology</h4>
          {terminology.map((set, idx) => {
            const helpId = `profile-terminology-${idx}-users`;
            const inUse = usersOf("terminology", set.id).length > 0;
            const name = set.id || `${idx + 1}`;
            return (
              <fieldset key={idx} className="rule-card" disabled={disabled}>
                <legend>{`Terminology set ${name}`}</legend>
                <label>
                  <span>Set ID</span>
                  <input
                    type="text"
                    value={set.id}
                    readOnly={inUse}
                    aria-describedby={inUse ? helpId : undefined}
                    onChange={(e) => updateTerminology(idx, { ...set, id: e.target.value })}
                  />
                </label>
                {usedBy("terminology", set.id, helpId)}
                <label>
                  <span>Description (optional)</span>
                  <input
                    type="text"
                    value={set.description ?? ""}
                    onChange={(e) => updateTerminology(idx, withText(set, "description", e.target.value))}
                  />
                </label>
                <label>
                  <span>Binding</span>
                  <select
                    value={set.binding}
                    onChange={(e) => updateTerminology(idx, { ...set, binding: e.target.value as LocalProfileBinding })}
                  >
                    {!(set.binding in BINDING_CAPTIONS) && <option value={set.binding}>Choose a binding</option>}
                    {(Object.keys(BINDING_CAPTIONS) as LocalProfileBinding[]).map((binding) => (
                      <option key={binding} value={binding}>
                        {BINDING_CAPTIONS[binding]}
                      </option>
                    ))}
                  </select>
                </label>
                {set.codes.map((code, at) => (
                  <div key={at} className="rule-code-row">
                    <label>
                      <span>Code</span>
                      <input
                        type="text"
                        value={code.code}
                        onChange={(e) => {
                          const codes = [...set.codes];
                          codes[at] = { ...code, code: e.target.value };
                          updateTerminology(idx, { ...set, codes });
                        }}
                      />
                    </label>
                    <label>
                      <span>Display text (optional)</span>
                      <input
                        type="text"
                        value={code.display ?? ""}
                        onChange={(e) => {
                          const codes = [...set.codes];
                          codes[at] = withText(code, "display", e.target.value);
                          updateTerminology(idx, { ...set, codes });
                        }}
                      />
                    </label>
                    <button
                      type="button"
                      aria-label={`Remove code ${code.code || at + 1} from ${name}`}
                      onClick={() => updateTerminology(idx, { ...set, codes: set.codes.filter((_, other) => other !== at) })}
                    >
                      Remove code
                    </button>
                  </div>
                ))}
                <div className="rule-actions">
                  <button
                    type="button"
                    aria-label={`Add code to ${name}`}
                    onClick={() => updateTerminology(idx, { ...set, codes: [...set.codes, { code: "" }] })}
                  >
                    Add code
                  </button>
                  <button
                    type="button"
                    className="danger-btn"
                    aria-label={`Remove set ${name}`}
                    onClick={() =>
                      removeRule("terminology", set.id, () =>
                        updateProfile({ ...profile, terminology: terminology.filter((_, other) => other !== idx) }),
                      )
                    }
                  >
                    Remove set
                  </button>
                </div>
              </fieldset>
            );
          })}
          {notice("terminology")}
          <button
            type="button"
            disabled={disabled}
            onClick={() =>
              updateProfile({ ...profile, terminology: [...terminology, { id: "", binding: "" as LocalProfileBinding, codes: [] }] })
            }
          >
            Add set
          </button>
        </section>

        <section className="rule-collection" aria-labelledby="profile-authorities-heading">
          <h4 id="profile-authorities-heading">Authorities</h4>
          <p className="hint">
            Each authority declares a namespace, a universal ID, or both, and a universal ID is declared together with
            its type.
          </p>
          {authorities.map((authority, idx) => {
            const helpId = `profile-authority-${idx}-users`;
            const inUse = usersOf("authority", authority.id).length > 0;
            const name = authority.id || `${idx + 1}`;
            return (
              <fieldset key={idx} className="rule-card" disabled={disabled}>
                <legend>{`Assigning authority ${name}`}</legend>
                <label>
                  <span>Authority ID</span>
                  <input
                    type="text"
                    value={authority.id}
                    readOnly={inUse}
                    aria-describedby={inUse ? helpId : undefined}
                    onChange={(e) => updateAuthority(idx, { ...authority, id: e.target.value })}
                  />
                </label>
                {usedBy("authority", authority.id, helpId)}
                <label>
                  <span>Description (optional)</span>
                  <input
                    type="text"
                    value={authority.description ?? ""}
                    onChange={(e) => updateAuthority(idx, withText(authority, "description", e.target.value))}
                  />
                </label>
                <label>
                  <span>Namespace (optional)</span>
                  <input
                    type="text"
                    value={authority.namespace ?? ""}
                    onChange={(e) => updateAuthority(idx, withText(authority, "namespace", e.target.value))}
                  />
                </label>
                <label>
                  <span>Universal ID (optional)</span>
                  <input
                    type="text"
                    value={authority.universal_id ?? ""}
                    onChange={(e) => updateAuthority(idx, withText(authority, "universal_id", e.target.value))}
                  />
                </label>
                <label>
                  <span>Universal ID type</span>
                  <select
                    value={authority.universal_id_type ?? ""}
                    onChange={(e) => updateAuthority(idx, withText(authority, "universal_id_type", e.target.value))}
                  >
                    <option value="">Not specified</option>
                    {UNIVERSAL_ID_TYPES.map((type) => (
                      <option key={type} value={type}>
                        {type}
                      </option>
                    ))}
                    {authority.universal_id_type && !UNIVERSAL_ID_TYPES.includes(authority.universal_id_type) && (
                      <option value={authority.universal_id_type}>{authority.universal_id_type}</option>
                    )}
                  </select>
                </label>
                <div className="rule-actions">
                  <button
                    type="button"
                    className="danger-btn"
                    aria-label={`Remove authority ${name}`}
                    onClick={() =>
                      removeRule("authority", authority.id, () =>
                        updateProfile({ ...profile, authorities: authorities.filter((_, other) => other !== idx) }),
                      )
                    }
                  >
                    Remove authority
                  </button>
                </div>
              </fieldset>
            );
          })}
          {notice("authority")}
          <button
            type="button"
            disabled={disabled}
            onClick={() => updateProfile({ ...profile, authorities: [...authorities, { id: "" }] })}
          >
            Add authority
          </button>
        </section>

        <section className="rule-collection" aria-labelledby="profile-dates-heading">
          <h4 id="profile-dates-heading">Date rules</h4>
          {dates.map((date, idx) => {
            const helpId = `profile-date-${idx}-users`;
            const inUse = usersOf("date", date.id).length > 0;
            const name = date.id || `${idx + 1}`;
            return (
              <fieldset key={idx} className="rule-card" disabled={disabled}>
                <legend>{`Date rule ${name}`}</legend>
                <label>
                  <span>Rule ID</span>
                  <input
                    type="text"
                    value={date.id}
                    readOnly={inUse}
                    aria-describedby={inUse ? helpId : undefined}
                    onChange={(e) => updateDate(idx, { ...date, id: e.target.value })}
                  />
                </label>
                {usedBy("date", date.id, helpId)}
                <label>
                  <span>Description (optional)</span>
                  <input
                    type="text"
                    value={date.description ?? ""}
                    onChange={(e) => updateDate(idx, withText(date, "description", e.target.value))}
                  />
                </label>
                <label>
                  <span>Precision</span>
                  <select
                    value={date.precision}
                    onChange={(e) => updateDate(idx, { ...date, precision: e.target.value as LocalProfilePrecision })}
                  >
                    {!(date.precision in PRECISION_CAPTIONS) && <option value={date.precision}>Choose a precision</option>}
                    {(Object.keys(PRECISION_CAPTIONS) as LocalProfilePrecision[]).map((precision) => (
                      <option key={precision} value={precision}>
                        {PRECISION_CAPTIONS[precision]}
                      </option>
                    ))}
                  </select>
                </label>
                <label>
                  <span>Time zone</span>
                  <select
                    value={date.timezone}
                    onChange={(e) => updateDate(idx, { ...date, timezone: e.target.value as LocalProfileTimeZoneRule })}
                  >
                    {!(date.timezone in TIME_ZONE_CAPTIONS) && <option value={date.timezone}>Choose an offset rule</option>}
                    {(Object.keys(TIME_ZONE_CAPTIONS) as LocalProfileTimeZoneRule[]).map((rule) => (
                      <option key={rule} value={rule}>
                        {TIME_ZONE_CAPTIONS[rule]}
                      </option>
                    ))}
                  </select>
                </label>
                <div className="rule-actions">
                  <button
                    type="button"
                    className="danger-btn"
                    aria-label={`Remove date rule ${name}`}
                    onClick={() =>
                      removeRule("date", date.id, () => updateProfile({ ...profile, dates: dates.filter((_, other) => other !== idx) }))
                    }
                  >
                    Remove date rule
                  </button>
                </div>
              </fieldset>
            );
          })}
          {notice("date")}
          <button
            type="button"
            disabled={disabled}
            onClick={() =>
              updateProfile({
                ...profile,
                dates: [...dates, { id: "", precision: "" as LocalProfilePrecision, timezone: "" as LocalProfileTimeZoneRule }],
              })
            }
          >
            Add date rule
          </button>
        </section>
      </>
    );
  }

  return (
    <section aria-labelledby="profile-editor-heading" className="profile-editor-section">
      <div className="profile-editor-header">
        <h3 id="profile-editor-heading">Profiles</h3>
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
          Versions and pins
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
          <form className="open-profile" aria-label="Open profile" onSubmit={(e) => void handleOpenProfile(e)}>
            <h4>Open profile</h4>
            <p className="hint">
              Opens a local profile from the workspace or one of its folders, such as a directory a package was imported
              into, resolves it against the pack it pins and shows its seal. Opening activates nothing.
            </p>
            <div className="pack-input-row">
              <label>
                <span>Profile file</span>
                <input
                  type="text"
                  placeholder="profile.json or folder/profile.json"
                  value={openEntry}
                  disabled={disabled}
                  onChange={(e) => setOpenEntry(e.target.value)}
                />
              </label>
              <label>
                <span>Pack file (optional)</span>
                <input
                  type="text"
                  aria-describedby="profile-open-pack-help"
                  value={openPack}
                  disabled={disabled}
                  onChange={(e) => setOpenPack(e.target.value)}
                />
              </label>
              <button type="submit" ref={openButton} disabled={disabled || !openEntry}>
                Open Profile
              </button>
            </div>
            <p id="profile-open-pack-help" className="hint">
              Empty for the pinned pack in the workspace.
            </p>
            {openNotice && (
              <p className="warning-text" role="alert">
                {openNotice}
              </p>
            )}
          </form>

          {!structured ? (
            <p className="warning-text" role="alert">
              The canonical JSON is not a profile document these controls can show, so they are withheld and nothing
              here can replace it. Correct it under Canonical JSON; validating reports what the profile reader refuses.
            </p>
          ) : (
            <>
              <div className="profile-meta-grid">
                <label>
                  <span>Profile ID</span>
                  <input
                    type="text"
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
                <fieldset className="pinned-pack" disabled={disabled}>
                  <legend>Pinned metadata pack</legend>
                  <label>
                    <span>Pack ID</span>
                    <input
                      type="text"
                      value={profile.base.pack.id}
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
                    <span>Pack version</span>
                    <input
                      type="text"
                      value={profile.base.pack.version}
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
                </fieldset>
                <label>
                  <span>HL7 version</span>
                  <select
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
                    {!HL7_VERSIONS.includes(profile.base.hl7_version) && (
                      <option value={profile.base.hl7_version}>{`${profile.base.hl7_version} (not supported)`}</option>
                    )}
                  </select>
                </label>
                <label>
                  <span>Message family</span>
                  <select
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
                    {!FAMILIES.includes(profile.base.family) && (
                      <option value={profile.base.family}>{`${profile.base.family} (not supported)`}</option>
                    )}
                  </select>
                </label>
              </div>

              <div className="section-toolbar">
                <h4>Segments and fields</h4>
                {newSegment === null && (
                  <button
                    type="button"
                    id="profile-add-segment"
                    disabled={disabled}
                    onClick={() => {
                      setNewSegment({ id: "", description: "" });
                      setFocusAfter("profile-new-segment-id");
                    }}
                  >
                    Add segment
                  </button>
                )}
              </div>

              {newSegment !== null && (
                <div className="add-segment-row" role="group" aria-label="New segment">
                  <label>
                    <span>Segment ID</span>
                    <input
                      id="profile-new-segment-id"
                      type="text"
                      value={newSegment.id}
                      disabled={disabled}
                      aria-describedby="profile-new-segment-help"
                      onChange={(e) => setNewSegment({ ...newSegment, id: e.target.value })}
                    />
                  </label>
                  <label>
                    <span>Description (optional)</span>
                    <input
                      type="text"
                      value={newSegment.description}
                      disabled={disabled}
                      onChange={(e) => setNewSegment({ ...newSegment, description: e.target.value })}
                    />
                  </label>
                  <button type="button" disabled={disabled || newSegment.id.trim() === ""} onClick={addSegment}>
                    Add segment
                  </button>
                  <button
                    type="button"
                    onClick={() => {
                      setNewSegment(null);
                      setFocusAfter("profile-add-segment");
                    }}
                  >
                    Cancel
                  </button>
                  <p id="profile-new-segment-help" className="hint">
                    For example SCH, or ZPD: an ID beginning with Z is a site-defined Z-segment.
                  </p>
                </div>
              )}

              <div className="profile-structure">
                <div className="segment-list">
                  {profile.segments.map((seg, segIdx) => {
                    const isSiteDefined = seg.id.startsWith("Z");
                    return (
                      <section
                        key={segIdx}
                        className="segment-card"
                        aria-label={`Segment ${seg.id}`}
                        data-testid={`segment-${seg.id}`}
                      >
                        <div className="segment-header">
                          <strong className="segment-id">
                            {seg.id} {isSiteDefined && <span className="badge badge-site">Site-defined Z-segment</span>}
                          </strong>
                          {seg.description && <span className="segment-desc">{seg.description}</span>}
                          <button
                            type="button"
                            className="danger-btn"
                            aria-label={`Remove segment ${seg.id}`}
                            disabled={disabled}
                            onClick={() => (seg.fields.length > 0 ? setConfirmingRemoval(segIdx) : removeSegment(segIdx))}
                          >
                            Remove segment
                          </button>
                        </div>
                        {confirmingRemoval === segIdx && (
                          <div className="confirm-row" role="alert">
                            <p>
                              Remove segment {seg.id} and its {seg.fields.length}{" "}
                              {seg.fields.length === 1 ? "field rule" : "field rules"} from this draft? Only the draft
                              changes; original evidence is never changed.
                            </p>
                            <button type="button" className="danger-btn" onClick={() => removeSegment(segIdx)}>
                              Confirm removal
                            </button>
                            <button type="button" onClick={() => setConfirmingRemoval(null)}>
                              Keep segment
                            </button>
                          </div>
                        )}
                        <Repetitions
                          group={`profile-segment-${segIdx}-repetitions`}
                          value={seg.cardinality}
                          disabled={disabled}
                          onChange={(cardinality) => {
                            const next: Segment = { ...seg };
                            if (cardinality) {
                              next.cardinality = cardinality;
                            } else {
                              delete next.cardinality;
                            }
                            updateSegment(segIdx, next);
                          }}
                        />

                        <div className="fields-table-wrapper">
                          <table className="fields-table">
                            <thead>
                              <tr>
                                <th>Selector</th>
                                <th>Position</th>
                                <th>Field name</th>
                                <th>Usage</th>
                                <th>Rule origin</th>
                                <th>Action</th>
                              </tr>
                            </thead>
                            <tbody>
                              {seg.fields.map((fld, fldIdx) => {
                                const selector = selectorOf(seg, fld);
                                const resolvedFld = profileResult?.resolution?.segments
                                  .find((s) => s.id === seg.id)
                                  ?.fields.find((f) => f.position === fld.position);
                                const origin = resolvedFld?.usage_origin;
                                const isSelected = selected?.segment === segIdx && selected.field === fldIdx;
                                return (
                                  <tr
                                    key={fldIdx}
                                    data-testid={`field-row-${selector}`}
                                    className={isSelected ? "field-row-selected" : undefined}
                                  >
                                    <td>
                                      <code className="selector-tag">{selector}</code>
                                    </td>
                                    <td>{fld.position}</td>
                                    <td>
                                      {fld.name ?? <span className="text-muted">Not specified</span>}
                                      {resolvedFld?.pack_name && resolvedFld.pack_name !== fld.name && (
                                        <div className="pack-name-note">Pinned pack label: {resolvedFld.pack_name}</div>
                                      )}
                                    </td>
                                    <td>{USAGE_CAPTIONS[fld.usage] ?? fld.usage}</td>
                                    <td>
                                      {origin ? (
                                        <span className={`badge badge-origin-${origin}`}>{origin}</span>
                                      ) : (
                                        <span className="text-muted">Not resolved</span>
                                      )}
                                    </td>
                                    <td className="field-actions">
                                      <button
                                        type="button"
                                        aria-label={`Edit field ${selector}`}
                                        aria-pressed={isSelected}
                                        disabled={disabled}
                                        onClick={() => {
                                          setSelected({ segment: segIdx, field: fldIdx });
                                          setFocusAfter("profile-field-detail-heading");
                                        }}
                                      >
                                        Edit
                                      </button>
                                      <button
                                        type="button"
                                        className="danger-btn"
                                        aria-label={`Remove field ${selector}`}
                                        disabled={disabled}
                                        onClick={() => removeField(segIdx, fldIdx)}
                                      >
                                        Remove field
                                      </button>
                                    </td>
                                  </tr>
                                );
                              })}
                            </tbody>
                          </table>
                          <button
                            type="button"
                            id={`profile-add-field-${segIdx}`}
                            aria-label={`Add field to ${seg.id}`}
                            disabled={disabled}
                            onClick={() => addField(segIdx)}
                          >
                            Add field
                          </button>
                        </div>
                      </section>
                    );
                  })}
                </div>
                {renderFieldDetail()}
              </div>

              {renderRules()}
            </>
          )}

          <div className="profile-actions-bar" role="group" aria-label="Save revision">
            <button
              type="button"
              className="primary-btn"
              disabled={disabled}
              onClick={() => void handleValidateProfile()}
            >
              Validate
            </button>
            <button
              type="button"
              disabled={disabled}
              onClick={() => void handleSaveProfile()}
            >
              Save revision
            </button>
            <label>
              <span>Profile file</span>
              <input
                type="text"
                value={saveOutput}
                disabled={disabled}
                onChange={(e) => setSaveOutput(e.target.value)}
              />
            </label>
            <label>
              <span>Version seal file</span>
              <input
                type="text"
                value={sealOutput}
                disabled={disabled}
                onChange={(e) => setSealOutput(e.target.value)}
              />
            </label>
            <p className="hint">
              Saving writes a new revision; an existing file is never replaced. The version seal records the revision's
              exact content digest; it is not an approval or a certificate.
            </p>
          </div>

          {profileResult && (
            <div className={`result-box result-${profileResult.state}`}>
              <h4>{resultHeading(resultKind, profileResult.state)}</h4>
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
          <h4>Open pack</h4>
          <div className="pack-input-row">
            <label>
              <span>Pack file</span>
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

              <h5>Declared support</h5>
              <table className="support-levels-table">
                <thead>
                  <tr>
                    <th>HL7 version</th>
                    <th>Message family</th>
                    <th>Lossless parsing</th>
                    <th>Field labels</th>
                    <th>Structure validation</th>
                    <th>Workflow evaluation</th>
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

          <h4>Open pack library</h4>
          <p className="hint">Opens a local directory of packs on this computer, not an online marketplace.</p>
          <div className="pack-input-row">
            <label>
              <span>Pack library folder (optional)</span>
              <input
                type="text"
                aria-describedby="profile-library-help"
                value={libraryDir}
                disabled={disabled}
                onChange={(e) => setLibraryDir(e.target.value)}
              />
            </label>
            <button type="button" disabled={disabled} onClick={() => void handleOpenLibrary()}>
              Open Library
            </button>
          </div>
          <p id="profile-library-help" className="hint">
            One folder of the workspace, or empty for the workspace itself.
          </p>

          {libraryResult && (
            <div className="library-results">
              <p>
                <strong>Status:</strong> {libraryResult.state} | <strong>Bundleable:</strong> {libraryResult.bundleable ? "Yes" : "No"} | <strong>Packs:</strong> {libraryResult.entries?.length ?? 0}
              </p>
              {libraryResult.matrix && libraryResult.matrix.length > 0 && (
                <table className="matrix-table">
                  <thead>
                    <tr>
                      <th>HL7 version</th>
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
        <div className="tab-panel" role="tabpanel" aria-label="Versions and pins">
          <h4>Version impact</h4>
          <p className="hint">
            Compare two versions of a profile to inspect differences. Evaluates pinned consumers against the references index.
            Approved profiles are immutable; historical test pins must be upgraded one test at a time.
          </p>

          <div className="compare-inputs-grid">
            <label>
              <span>Earlier profile</span>
              <input
                type="text"
                value={compareFrom}
                disabled={disabled}
                onChange={(e) => {
                  setCompareFrom(e.target.value);
                  withdrawComparison();
                }}
              />
            </label>
            <label>
              <span>Later profile</span>
              <input
                type="text"
                value={compareTo}
                disabled={disabled}
                onChange={(e) => {
                  setCompareTo(e.target.value);
                  withdrawComparison();
                }}
              />
            </label>
            <label>
              <span>Test references file</span>
              <input
                type="text"
                value={compareRefs}
                disabled={disabled}
                onChange={(e) => {
                  setCompareRefs(e.target.value);
                  withdrawComparison();
                }}
              />
            </label>
            <button
              type="button"
              className="primary-btn"
              disabled={disabled || !compareFrom || !compareTo}
              onClick={() => void handleCompareProfiles()}
            >
              Compare
            </button>
          </div>

          {pinUpdate && (
            <section
              className={`seal-box ${pinUpdate.result.state === "completed" ? "valid" : "invalid"}`}
              aria-label="Test pin update"
            >
              <strong>Update test pin: {pinUpdate.result.state}</strong>
              {pinUpdate.result.state === "completed" ? (
                <p>
                  Updated the pin of {pinUpdate.test} in {pinUpdate.result.output ?? ""} from {pinText(pinUpdate.was)} to{" "}
                  {pinText(pinUpdate.now)}. The references file changed, so compare again to assess it.
                </p>
              ) : (
                pinUpdate.result.reason && <p>{pinUpdate.result.reason}</p>
              )}
            </section>
          )}

          {compared && (
            <div className="compare-results">
              {compared.result.state !== "completed" ? (
                <>
                  <h4>Comparison: {compared.result.state}</h4>
                  {compared.result.reason && <p className="error-text">{compared.result.reason}</p>}
                </>
              ) : (
                <>
                  {compared.result.comparison && (
                    <p className="compared-inputs">
                      Earlier profile {compared.from} ({compared.result.comparison.profile} version{" "}
                      {compared.result.comparison.from}) · Later profile {compared.to} (
                      {compared.result.comparison.profile} version {compared.result.comparison.to})
                      {compared.references ? ` · Test references file ${compared.references}` : ""}
                    </p>
                  )}
                  <h4>Differences ({compared.result.comparison?.changes.length ?? 0})</h4>
                  {compared.result.comparison?.changes.map((c, idx) => (
                    <div key={idx} className="diff-item">
                      <span className={`badge badge-kind-${c.kind}`}>{c.kind}</span>
                      <strong>{c.part}{c.subject ? ` (${c.subject})` : ""}:</strong> {c.detail}
                    </div>
                  ))}

                  <h4>Impacted Tests from Reference Index</h4>
                  {compared.result.assessment?.tests && compared.result.assessment.tests.length > 0 ? (
                    <table className="impact-table">
                      <thead>
                        <tr>
                          <th>Test</th>
                          <th>Case</th>
                          <th>Pinned version</th>
                          <th>SHA-256</th>
                          <th>Impact</th>
                          <th>Action</th>
                        </tr>
                      </thead>
                      <tbody>
                        {compared.result.assessment.tests.map((test, idx) => {
                          const later = compared.result.later_pin;
                          return (
                            <tr key={idx} className={`impact-row-${test.impact}`}>
                              <td>{test.test}</td>
                              <td>{test.case}</td>
                              <td>v{test.pinned.version}</td>
                              <td>
                                <code className="digest">{test.pinned.sha256}</code>
                              </td>
                              <td>
                                <span className={`badge badge-impact-${test.impact}`}>{test.impact}</span>
                              </td>
                              <td>
                                {test.impact !== "affected" ? (
                                  <span className="text-muted">No pin update needed</span>
                                ) : later?.pin ? (
                                  <>
                                    <p id={`profile-pin-change-${idx}`} className="pin-change">
                                      From {pinText(test.pinned)} to {pinText(later.pin)}
                                    </p>
                                    <button
                                      type="button"
                                      disabled={disabled}
                                      aria-describedby={`profile-pin-change-${idx}`}
                                      onClick={() => void handleUpdatePin(test)}
                                    >
                                      Update test pin
                                    </button>
                                  </>
                                ) : (
                                  <p className="warning-text">{later?.refusal ?? ""}</p>
                                )}
                              </td>
                            </tr>
                          );
                        })}
                      </tbody>
                    </table>
                  ) : (
                    <p className="hint">No tests referenced or no impact assessed.</p>
                  )}
                </>
              )}
            </div>
          )}
        </div>
      )}
      {/* TAB 4: PACKAGE EXCHANGE */}
      {activeTab === "exchange" && (
        <div className="tab-panel" role="tabpanel" aria-label="Package Exchange">
          <section aria-labelledby="profile-export-heading">
          <h4 id="profile-export-heading">Export package</h4>
          <p className="hint">
            Export creates an offline package holding a verified local profile, its pinned pack, its canonical version seal,
            and its reviewed origin. No patient evidence is included.
          </p>

          <div className="exchange-grid">
            <label>
              <span>Profile file</span>
              <input
                type="text"
                value={pkgExportProfile}
                disabled={disabled}
                onChange={(e) => exportInput(setPkgExportProfile)(e.target.value)}
              />
            </label>
            <label>
              <span>Pack file</span>
              <input
                type="text"
                value={pkgExportPack}
                disabled={disabled}
                onChange={(e) => exportInput(setPkgExportPack)(e.target.value)}
              />
            </label>
            <label>
              <span>Version seal file</span>
              <input
                type="text"
                value={pkgExportVersion}
                disabled={disabled}
                onChange={(e) => exportInput(setPkgExportVersion)(e.target.value)}
              />
            </label>
            <label>
              <span>Origin file</span>
              <input
                type="text"
                value={pkgExportOrigin}
                disabled={disabled}
                onChange={(e) => exportInput(setPkgExportOrigin)(e.target.value)}
              />
            </label>
            <label>
              <span>Package file</span>
              <input
                type="text"
                value={pkgExportOutput}
                disabled={disabled}
                aria-describedby="profile-export-output-help"
                onChange={(e) => exportInput(setPkgExportOutput)(e.target.value)}
              />
            </label>
          </div>
          <p id="profile-export-output-help" className="hint">
            A new file of the workspace; an existing file is never replaced. Changing any of these files clears the
            confirmation below.
          </p>

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
          </section>

          <hr className="divider" />

          <form aria-label="Import package" onSubmit={(e) => void handleImportPackage(e)}>
            <h4>Import package</h4>
            <p className="hint">
              Import verifies a package and writes its documents into a new directory of this workspace, as
              `readmit profile import` does. It refuses a directory that already exists and activates nothing.
            </p>
            <div className="exchange-grid">
              <label>
                <span>Package file</span>
                <input
                  type="text"
                  value={pkgImportFile}
                  disabled={disabled}
                  onChange={(e) => setPkgImportFile(e.target.value)}
                />
              </label>
              <label>
                <span>Import folder</span>
                <input
                  type="text"
                  aria-describedby="profile-import-folder-help"
                  value={pkgImportOutput}
                  disabled={disabled}
                  onChange={(e) => setPkgImportOutput(e.target.value)}
                />
              </label>
            </div>
            <p id="profile-import-folder-help" className="hint">
              A new folder of the workspace; an existing folder is refused.
            </p>
            <button type="submit" ref={importButton} disabled={disabled || !pkgImportFile || !pkgImportOutput}>
              Import Package
            </button>
            {importing && (
              <button type="button" ref={cancelImportButton} onClick={lifecycle.cancel}>
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

          <section aria-labelledby="profile-inspect-heading">
          <h4 id="profile-inspect-heading">Inspect Package</h4>
          <div className="pack-input-row">
            <label>
              <span>Package file</span>
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
          </section>

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
          <h4>JSON</h4>
          <p className="hint">
            The raw editor holds the canonical contract document itself: JSON under schema{" "}
            <code>readmit-local-profile/v1</code>. Edits here round-trip into the editor's fields once they parse
            with that schema; validate against the pinned pack before saving a revision.
          </p>
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
              Discard changes
            </button>
          </div>
        </div>
      )}
    </section>
  );
}
