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
// placer ID and answers AA. After every message it rewrites its own export —
// a CSV of the ledger, written whole and renamed into place — which is the
// external state a journey observes.
//
// This file is JavaScript for the same reason as bridge-process.js;
// downstream.d.ts states its interface.
import { createServer } from "node:net";
import { mkdirSync, renameSync, writeFileSync } from "node:fs";
import { dirname } from "node:path";

const START = 0x0b;
const END = [0x1c, 0x0d];

/** Starts the downstream system on an ephemeral loopback port. */
export function startDownstream({ exportPath, mode = "defective" }) {
  const state = {
    mode,
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
        const reply = frame(answer(state, payload));
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
      },
      /** Every message payload received, in order, exactly as it arrived. */
      received() {
        return [...state.received];
      },
      /** The ledger as the system holds it. */
      ledger() {
        return Object.fromEntries(state.ledger);
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
    const key = state.mode === "defective" ? filler : placer;
    if (state.ledger.has(key)) {
      state.ledger.set(key, start);
    } else {
      code = "AE";
      error = "appointment not found";
    }
  } else {
    code = "AR";
    error = "unsupported trigger";
  }
  const s = message.separator;
  const segments = [
    ["MSH", "^~\\&", "DOWNSTREAM", "SYNTHETIC", message.field("MSH", 3), message.field("MSH", 4), "20260101120000+0000", "", `ACK^${trigger}^ACK`, `ACK-${control}`, "P", "2.5.1"].join(s),
    ["MSA", code, control].join(s),
  ];
  if (error) segments.push(["ERR", "", "", "", "E", "", "", "", error].join(s));
  return segments.join("\r") + "\r";
}
