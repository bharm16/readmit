// Run reviewed test: a test rebound to an approved export review's derived
// case, run against the target the original run reached to reproduce its
// failure or its pass. It is chosen here by name and reviewed in the one
// reviewed send, which binds the review's own approval and the original
// packet; nobody types an identity. It is never ordinary replay, and a
// matched phase is not proof against the original system.
import { useCallback, useEffect, useRef, useState } from "react";
import { listWholeCatalog, RequestScope, type CatalogItem } from "./bindings";
import { FormDialog } from "./layout";
import type { SendRequest } from "./RunPanel";

export function Reexecution({ workspace, onRun }: { workspace: string | null; onRun: (request: SendRequest) => void }) {
  const scope = useRef(new RequestScope());
  const context = useCallback(() => scope.current.enter(workspace ?? ""), [workspace]);
  const [open, setOpen] = useState(false);
  const [reports, setReports] = useState<CatalogItem[]>([]);
  const [tests, setTests] = useState<CatalogItem[]>([]);
  const [review, setReview] = useState("");
  const [packet, setPacket] = useState("");
  const [test, setTest] = useState("");
  const [phase, setPhase] = useState<"failure" | "pass">("failure");

  useEffect(() => {
    if (!open || !workspace) return;
    setReview("");
    setPacket("");
    setTest("");
    setPhase("failure");
    const asked = context();
    void Promise.all([listWholeCatalog({ context: asked, kind: "report", filter: {} }), listWholeCatalog({ context: asked, kind: "test", filter: {} })]).then(([reportList, testList]) => {
      setReports((reportList.page?.items ?? []).filter((item) => item.availability === "available"));
      setTests((testList.page?.items ?? []).filter((item) => item.availability === "available"));
    });
  }, [open, workspace, context]);

  const reviews = reports.filter((item) => item.summary.report?.form === "export-review");
  const packets = reports.filter((item) => item.summary.report?.form === "packet");
  const chosenTest = tests.find((item) => item.ref.id === test);
  const ready = review !== "" && packet !== "" && chosenTest !== undefined;

  return (
    <>
      <button type="button" disabled={!workspace} onClick={() => setOpen(true)}>
        Run reviewed test
      </button>
      <FormDialog
        open={open}
        title="Run reviewed test"
        submitLabel="Continue"
        submitDisabled={!ready}
        onClose={() => setOpen(false)}
        onSubmit={() => {
          if (!ready || !chosenTest) return { reason: "Choose a review, its original packet and a test." };
          setOpen(false);
          onRun({
            kind: "reviewed",
            review: { kind: "report", id: review },
            packet: { kind: "report", id: packet },
            test: { kind: "test", id: chosenTest.ref.id, ...(chosenTest.summary.test?.current_version ? { revision: chosenTest.summary.test.current_version } : {}) },
            phase,
          });
          return null;
        }}
      >
        <label htmlFor="reviewed-review">Review</label>
        <select id="reviewed-review" value={review} onChange={(event) => setReview(event.target.value)}>
          <option value="">Choose…</option>
          {reviews.map((item) => (
            <option key={item.ref.id} value={item.ref.id}>
              {item.name}
            </option>
          ))}
        </select>
        <label htmlFor="reviewed-packet">Original run</label>
        <select id="reviewed-packet" value={packet} onChange={(event) => setPacket(event.target.value)}>
          <option value="">Choose…</option>
          {packets.map((item) => (
            <option key={item.ref.id} value={item.ref.id}>
              {item.name}
            </option>
          ))}
        </select>
        <fieldset className="checks">
          <legend>Reproduce</legend>
          <label className="check">
            <input type="radio" name="reviewed-phase" checked={phase === "failure"} onChange={() => setPhase("failure")} />
            Failure
          </label>
          <label className="check">
            <input type="radio" name="reviewed-phase" checked={phase === "pass"} onChange={() => setPhase("pass")} />
            Pass
          </label>
        </fieldset>
        <label htmlFor="reviewed-test">Test</label>
        <select id="reviewed-test" value={test} onChange={(event) => setTest(event.target.value)}>
          <option value="">Choose…</option>
          {tests.map((item) => (
            <option key={item.ref.id} value={item.ref.id}>
              {item.summary.test?.current_version ? `${item.name} · v${item.summary.test.current_version}` : item.name}
            </option>
          ))}
        </select>
      </FormDialog>
    </>
  );
}
