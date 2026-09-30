// An independent downstream scheduling system for journeys: the system a
// person's interface feeds, which readmit sends to and observes but did not
// write. It is written here from the MLLP and HL7 v2 encoding descriptions
// and shares nothing with readmit's Go framing, parsing or acknowledgement
// code, so agreement between the two is evidence rather than one assumption
// stated twice. It binds loopback only and holds synthetic evidence only.
//
// It keeps an appointment ledger keyed by the placer appointment ID (SCH-1):
// an S12 books one appointment, an S13 moves the appointment it names. Its
// defect is the kind an interface investigation finds: in the defective mode
// a reschedule is matched on the filler ID (SCH-2) instead, finds nothing,
// and is answered AE with the ledger unchanged; the fixed mode matches on the
// placer ID and answers AA. The duplicating mode is the same defect as a
// busy receiver commits it: the reschedule it cannot match on the filler ID is
// booked as a new appointment under that ID and answered AA, so the ledger
// holds two appointments where one was moved. After every message it rewrites its own export —
// a CSV of the ledger, written whole and renamed into place — which is the
// external state a journey observes.
//
// Given an observation path it also keeps the observation handoff docs/listen.md
// documents for a fixture receiver: a readmit-observation/v1 snapshot of its
// appointment records and the occurrences it processed, written inconsistent,
// updated, written consistent and only then acknowledged, and a ZRT receipt
// segment naming its session and its own received occurrence on every ACK. The
// snapshot is written from that description alone, so a test run that reads it
// is reading a receiver it did not write.
//
// This file is JavaScript for the same reason as bridge-process.js;
// downstream.d.ts states its interface.
import { createServer } from "node:net";
import { randomBytes } from "node:crypto";
import { mkdirSync, renameSync, writeFileSync } from "node:fs";
import { dirname } from "node:path";

const START = 0x0b;
const END = [0x1c, 0x0d];

/** Starts the downstream system on an ephemeral loopback port. */
export function startDownstream({ exportPath, observationPath, mode = "defective" }) {
  const state = {
    mode,
    observationPath,
    session: randomBytes(16).toString("hex"),
    processed: [],
    records: [],
    received: [],
    ledger: new Map(),
    held: null,
    holding: false,
    connections: new Set(),
  };
  mkdirSync(dirname(exportPath), { recursive: true });
  const writeExport = () => {
    const rows = [...state.ledger.entries()].map(([appointment, start]) => `${appointment},${start}\n`);
    writeFileSync(`${exportPath}.partial`, "appointment,start\n" + rows.join(""), { mode: 0o600 });
    renameSync(`${exportPath}.partial`, exportPath);
  };
  writeExport();
  if (observationPath) writeObservation(state, true);

  const server = createServer((socket) => {
    state.connections.add(socket);
    socket.on("close", () => state.connections.delete(socket));
    socket.on("error", () => {});
    let buffered = Buffer.alloc(0);
    socket.on("data", (chunk) => {
      buffered = Buffer.concat([buffered, chunk]);
      for (;;) {
        const end = frameEnd(buffered);
        if (end < 0) return;
        const wire = buffered.subarray(0, end);
        buffered = buffered.subarray(end);
        if (wire[0] !== START) {
          socket.destroy();
          return;
        }
        const payload = wire.subarray(1, wire.length - 2).toString("latin1");
        state.received.push(payload);
        if (state.observationPath) writeObservation(state, false);
        const reply = frame(answer(state, payload));
        if (state.observationPath) writeObservation(state, true);
        writeExport();
        if (state.holding) {
          // The message was received and applied; its acknowledgement waits,
          // as a slow or stalled system's would.
          state.held = () => socket.write(reply);
        } else {
          socket.write(reply);
        }
      }
    });
  });
  const listening = new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => resolve());
  });

  return listening.then(() => {
    const { port } = server.address();
    return {
      address: `127.0.0.1:${port}`,
      /** Switches how a reschedule is matched: the defect, or its fix. */
      setMode(next) {
        state.mode = next;
      },
      /** From now on, holds the acknowledgement of each message received,
       * as a stalled system would. A sender waits for each acknowledgement,
       * so one is outstanding at a time. */
      holdAcknowledgements() {
        state.holding = true;
      },
      /** Stops holding, and sends the acknowledgement being held, if any. */
      releaseAcknowledgement() {
        state.holding = false;
        const held = state.held;
        state.held = null;
        if (held) held();
      },
      /** Empties the ledger and rewrites the export: the reset an operator
       * performs between runs. */
      reset() {
        state.ledger.clear();
        writeExport();
        if (state.observationPath) {
          state.processed = [];
          state.records = [];
          writeObservation(state, true);
        }
      },
      /** Every message payload received, in order, exactly as it arrived. */
      received() {
        return [...state.received];
      },
      /** The ledger as the system holds it. */
      ledger() {
        return Object.fromEntries(state.ledger);
      },
      /** How many connections are open to it now: a sender or a
       * connectivity check holding one is reaching it at this moment. */
      connected() {
        return state.connections.size;
      },
      close() {
        for (const socket of state.connections) socket.destroy();
        return new Promise((resolve) => server.close(() => resolve()));
      },
    };
  });
}

/** The end of the first complete MLLP block, or -1 when none is complete. */
function frameEnd(buffered) {
  for (let index = 1; index < buffered.length; index++) {
    if (buffered[index - 1] === END[0] && buffered[index] === END[1]) return index + 1;
  }
  return -1;
}

function frame(text) {
  return Buffer.concat([Buffer.from([START]), Buffer.from(text, "latin1"), Buffer.from(END)]);
}

/** One HL7 v2 message split the way the encoding rules describe: segments on
 * CR, fields on the separator MSH declares, components on its first encoding
 * character. MSH-1 is the separator itself, so MSH fields are counted from it. */
function parse(payload) {
  const separator = payload[3];
  const component = payload[4];
  const segments = payload.split("\r").filter((segment) => segment !== "");
  const field = (id, position) => {
    const segment = segments.find((candidate) => candidate.startsWith(id + separator));
    if (!segment) return "";
    const fields = segment.split(separator);
    const index = id === "MSH" ? position - 1 : position;
    return fields[index] ?? "";
  };
  const first = (value) => value.split(component)[0] ?? "";
  return { separator, component, field, first };
}

/** Applies one message to the ledger and builds its acknowledgement. */
function answer(state, payload) {
  const message = parse(payload);
  const control = message.field("MSH", 10);
  const trigger = message.field("MSH", 9).split(message.component)[1] ?? "";
  const placer = message.first(message.field("SCH", 1));
  const filler = message.first(message.field("SCH", 2));
  const start = message.field("SCH", 11).split(message.component)[3] ?? "";
  let code = "AA";
  let error = "";
  if (trigger === "S12") {
    state.ledger.set(placer, start);
  } else if (trigger === "S13") {
    const key = state.mode === "fixed" ? placer : filler;
    if (state.ledger.has(key) || state.mode === "duplicating") {
      state.ledger.set(key, start);
    } else {
      code = "AE";
      error = "appointment not found";
    }
  } else {
    code = "AR";
    error = "unsupported trigger";
  }
  if (state.observationPath) applyToRecords(state, message, trigger, control, code === "AA" || state.mode === "duplicating");
  const s = message.separator;
  const segments = [
    ["MSH", "^~\\&", "DOWNSTREAM", "SYNTHETIC", message.field("MSH", 3), message.field("MSH", 4), "20260101120000+0000", "", `ACK^${trigger}^ACK`, `ACK-${control}`, "P", "2.5.1"].join(s),
    ["MSA", code, control].join(s),
  ];
  if (error) segments.push(["ERR", "", "", "", "E", "", "", "", error].join(s));
  if (state.observationPath) segments.push(["ZRT", "readmit-receipt/v1", state.session, state.processed.at(-1).occurrence_id].join(s));
  return segments.join("\r") + "\r";
}

/** An EI or CX identifier with its assigning authority, as the observation
 * handoff records it: value, namespace, universal ID and its type. */
function identifier(message, value, authority) {
  const sub = "&";
  const parts = (authority ?? "").split(sub);
  return { value, namespace: parts[0] ?? "", universal_id: parts[1] ?? "", universal_id_type: parts[2] ?? "" };
}

/** Records the occurrence this receiver processed and, for an accepted
 * booking or reschedule, the appointment record it keeps: a reschedule
 * updates the record it names, except in the duplicating mode, which books it
 * again as a new record. */
function applyToRecords(state, message, trigger, control, accepted) {
  const occurrence = `s0001-e${String(state.processed.length + 1).padStart(6, "0")}`;
  state.processed.push({ occurrence_id: occurrence, control_id: control });
  if (!accepted) return;
  const component = (value, index) => value.split(message.component)[index] ?? "";
  const pid = message.field("PID", 3).split("~")[0] ?? "";
  const placer = message.field("SCH", 1);
  const filler = message.field("SCH", 2);
  const record = {
    patient_id: identifier(message, component(pid, 0), component(pid, 3)),
    placer_id: { value: component(placer, 0), namespace: component(placer, 1), universal_id: component(placer, 2), universal_id_type: component(placer, 3) },
    filler_id: { value: component(filler, 0), namespace: component(filler, 1), universal_id: component(filler, 2), universal_id_type: component(filler, 3) },
    appointment_start: component(message.field("SCH", 11), 3),
  };
  const same = state.records.findIndex((held) => held.filler_id.value === record.filler_id.value && held.placer_id.value === record.placer_id.value);
  if (trigger === "S13" && state.mode === "fixed" && same >= 0) {
    state.records[same] = { ...state.records[same], appointment_start: record.appointment_start };
    return;
  }
  state.records.push({ record_id: `r${String(state.records.length + 1).padStart(6, "0")}`, ...record });
}

/** Installs the observation snapshot whole, by a rename in its folder. */
function writeObservation(state, consistent) {
  mkdirSync(dirname(state.observationPath), { recursive: true });
  const snapshot = {
    schema: "readmit-observation/v1",
    profile: "readmit-siu-v1",
    session_id: state.session,
    mode: state.mode === "fixed" ? "fixed" : "defective",
    processed: state.processed,
    consistent,
    records: state.records,
  };
  writeFileSync(`${state.observationPath}.partial`, JSON.stringify(snapshot) + "\n", { mode: 0o600 });
  renameSync(`${state.observationPath}.partial`, state.observationPath);
}
