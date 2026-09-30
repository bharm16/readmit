// The interface of downstream.js, the independent downstream scheduling
// system journeys send to and observe.

/** How the system matches a reschedule: on the filler ID, which is its
 * defect (refused AE, or accepted AA as a second appointment when
 * duplicating), or on the placer ID, which is its fix. */
export type DownstreamMode = "defective" | "duplicating" | "fixed";

export interface Downstream {
  /** The loopback address it listens on, host and port. */
  address: string;
  setMode(mode: DownstreamMode): void;
  holdAcknowledgements(): void;
  releaseAcknowledgement(): void;
  reset(): void;
  received(): string[];
  ledger(): Record<string, string>;
  connected(): number;
  close(): Promise<void>;
}

/** exportPath is the ledger's CSV export; observationPath, when given, is
 * where it keeps the readmit-observation/v1 handoff and adds a receipt to
 * every acknowledgement. */
export function startDownstream(options: { exportPath: string; observationPath?: string; mode?: DownstreamMode }): Promise<Downstream>;
