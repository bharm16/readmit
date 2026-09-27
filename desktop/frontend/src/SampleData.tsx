// Tools › Sample data › SIU fixture: the built-in synthetic receiver on a
// loopback address, fixed or with its defect, bounded, until Stop. What it
// captures is added to the project as a synthetic case.
import { useState } from "react";
import {
  cancel,
  checkScenarioLibrary,
  chooseInspectionPath,
  generateSynth,
  startSampleFixture,
  type ItemRef,
  type ObservationMode,
  type RequestContext,
  type SampleFixtureResult,
  type ScenarioLibraryResult,
  type SynthGenerateResult,
} from "./bindings";
import { ValueRows, folderName } from "./layout";
import { useLifecycle } from "./lifecycle";
import { useVocabulary } from "./vocabulary";

const MODES: Record<ObservationMode, string> = { fixed: "Fixed", defective: "Defective" };
const LOOPBACK = /^(127\.\d{1,3}\.\d{1,3}\.\d{1,3}|\[::1\]):\d{1,5}$/;

export function SampleFixture({ context, busy, onOpenCase }: { context: () => RequestContext; busy: boolean; onOpenCase: (ref: ItemRef, entry: string) => void }) {
  const modes = useVocabulary()?.fixture_modes ?? (["fixed", "defective"] as ObservationMode[]);
  const [mode, setMode] = useState<ObservationMode>("fixed");
  const [address, setAddress] = useState("127.0.0.1:0");
  const [limit, setLimit] = useState(100);
  const [result, setResult] = useState<SampleFixtureResult | null>(null);
  const run = useLifecycle<"fixture">({ window: true, names: { fixture: "capture" } });
  const running = run.running !== null;
  const valid = LOOPBACK.test(address) && limit >= 1;
  return (
    <section className="sample-section" aria-label="SIU fixture">
      <h2>SIU fixture</h2>
      <div className="inline-fields">
        <span>
          <label htmlFor="fixture-mode">Mode</label>
          <select id="fixture-mode" value={mode} disabled={running} onChange={(event) => setMode(event.target.value as ObservationMode)}>
            {modes.map((entry) => (
              <option key={entry} value={entry}>
                {MODES[entry]}
              </option>
            ))}
          </select>
        </span>
        <span>
          <label htmlFor="fixture-address">Address</label>
          <input id="fixture-address" type="text" value={address} disabled={running} aria-invalid={!LOOPBACK.test(address)} onChange={(event) => setAddress(event.target.value.trim())} />
        </span>
        <span>
          <label htmlFor="fixture-limit">Messages at most</label>
          <input id="fixture-limit" type="number" min={1} max={4000} value={limit} disabled={running} onChange={(event) => setLimit(Math.min(4000, Math.max(1, Math.trunc(Number(event.target.value) || 1))))} />
        </span>
      </div>
      <div className="row-actions">
        {running ? (
          <button type="button" onClick={() => cancel("capture")}>
            Stop
          </button>
        ) : (
          <button
            type="button"
            className="primary"
            disabled={busy || !valid}
            onClick={() =>
              void run.run("fixture", async (current) => {
                setResult(null);
                const answer = await startSampleFixture({ context: context(), mode, address, max_messages: limit });
                if (current()) setResult(answer);
              })
            }
          >
            Start
          </button>
        )}
      </div>
      {running ? <p role="status">Listening</p> : null}
      {result && !running ? (
        result.state === "completed" || result.state === "cancelled" ? (
          <>
            <ValueRows
              rows={[
                { label: "Received", value: result.received === 1 ? "1 message" : `${result.received} messages` },
                { label: "Origin", value: "Synthetic" },
                ...(result.bound_address ? [{ label: "Address", value: result.bound_address }] : []),
              ]}
            />
            {result.case && result.case_entry ? (
              <button type="button" onClick={() => onOpenCase(result.case!, result.case_entry!)}>
                Open case
              </button>
            ) : null}
          </>
        ) : (
          <p role="alert">{result.reason ?? "The fixture did not run."}</p>
        )
      ) : null}
    </section>
  );
}

/** A new seed and whole-second base time, allocated once. */
function freshInputs(): { seed: string; base: string } {
  const seed = String(Math.floor(Math.random() * 2 ** 32));
  return { seed, base: new Date(Math.floor(Date.now() / 1000) * 1000).toISOString().replace(".000Z", "Z") };
}

/** The reproducible synthetic SIU family, generated as `readmit synth`
 * generates it from a declared seed and base time. */
export function SyntheticFamilies({ workspace, busy }: { workspace: string; busy: boolean }) {
  const [inputs, setInputs] = useState(freshInputs);
  const [result, setResult] = useState<SynthGenerateResult | null>(null);
  const run = useLifecycle<"synth">({ window: true });
  const valid = /^(0|[1-9]\d{0,19})$/.test(inputs.seed) && /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(Z|[+-]\d{2}:\d{2})$/.test(inputs.base);
  return (
    <section className="sample-section" aria-label="Synthetic families">
      <h2>Synthetic families</h2>
      <div className="inline-fields">
        <span>
          <label htmlFor="synth-seed">Seed</label>
          <input id="synth-seed" type="text" inputMode="numeric" value={inputs.seed} aria-invalid={!valid} onChange={(event) => setInputs({ ...inputs, seed: event.target.value.trim() })} />
        </span>
        <span>
          <label htmlFor="synth-base">Base time</label>
          <input id="synth-base" type="text" value={inputs.base} aria-invalid={!valid} onChange={(event) => setInputs({ ...inputs, base: event.target.value.trim() })} />
        </span>
      </div>
      <div className="row-actions">
        <button
          type="button"
          className="primary"
          disabled={busy || !valid || run.running !== null}
          onClick={() =>
            void run.run("synth", async (current) => {
              setResult(null);
              const answer = await generateSynth({
                workspace,
                output_name: `synthetic-siu-${inputs.seed}-${Date.now()}`,
                seed: inputs.seed,
                base_time: inputs.base,
                generator_version: "readmit-synth-v1",
                profile_version: "readmit-siu-v1",
              });
              if (current()) setResult(answer);
            })
          }
        >
          Generate
        </button>
      </div>
      {result ? (
        result.state === "completed" ? (
          <ValueRows
            label="Generated family"
            rows={[
              { label: "Cases", value: String(result.cases?.length ?? 0) },
              ...(result.variants ?? []).map((variant, index) => ({ label: `Variant ${index + 1}`, value: variant.known_defect ? `Known defect · ${variant.known_defect}` : "Correct" })),
              { label: "Origin", value: "Synthetic" },
            ]}
          />
        ) : (
          <p role="alert">{result.reason ?? "The family was not generated."}</p>
        )
      ) : null}
    </section>
  );
}

/** A scenario library checked, in memory, against independently written expectations. */
export function ScenarioLibraryCheck({ workspace, busy }: { workspace: string; busy: boolean }) {
  const [library, setLibrary] = useState("");
  const [expectations, setExpectations] = useState("");
  const [result, setResult] = useState<ScenarioLibraryResult | null>(null);
  const run = useLifecycle<"check">({ window: true, names: { check: "scenario-check" } });
  const choose = async (set: (path: string) => void) => {
    const answer = await chooseInspectionPath("file");
    if (answer.state === "completed" && answer.path) {
      set(answer.path);
      setResult(null);
    }
  };
  const running = run.running !== null;
  return (
    <section className="sample-section" aria-label="Scenario library check">
      <h2>Scenario library check</h2>
      <ValueRows
        rows={[
          {
            label: "Library",
            value: (
              <span className="value-with-action">
                <span className="location-value">{library ? folderName(library) : "Not chosen"}</span>
                <button type="button" disabled={running} onClick={() => void choose(setLibrary)}>
                  Choose
                </button>
              </span>
            ),
          },
          {
            label: "Expectations",
            value: (
              <span className="value-with-action">
                <span className="location-value">{expectations ? folderName(expectations) : "Not chosen"}</span>
                <button type="button" disabled={running} onClick={() => void choose(setExpectations)}>
                  Choose
                </button>
              </span>
            ),
          },
        ]}
      />
      <div className="row-actions">
        {running ? (
          <button type="button" onClick={() => cancel("scenario-check")}>
            Stop
          </button>
        ) : (
          <button
            type="button"
            className="primary"
            disabled={busy || !library || !expectations}
            onClick={() =>
              void run.run("check", async (current) => {
                setResult(null);
                const answer = await checkScenarioLibrary({ workspace, library, expectations });
                if (current()) setResult(answer);
              })
            }
          >
            Check
          </button>
        )}
      </div>
      {result ? (
        result.state === "completed" ? (
          <ValueRows
            label="Check result"
            rows={[
              { label: "Result", value: "Matches the expectations" },
              ...(result.templates ?? []).map((template, index) => ({ label: `Template ${index + 1} · v${template.version}`, value: template.coverage.join(", ") || "—" })),
              ...(result.streams !== undefined ? [{ label: "Streams", value: String(result.streams) }] : []),
              ...(result.fields !== undefined ? [{ label: "Fields checked", value: String(result.fields) }] : []),
            ]}
          />
        ) : (
          <p role="alert">{result.reason ?? "The library does not match the expectations."}</p>
        )
      ) : null}
    </section>
  );
}
