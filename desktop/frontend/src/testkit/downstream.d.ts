// The interface of downstream.js, the independent downstream scheduling
// system journeys send to and observe.

/** How the system matches a reschedule: on the filler ID, which is its
 * defect, or on the placer ID, which is its fix. */
export type DownstreamMode = "defective" | "fixed";

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

export function startDownstream(options: { exportPath: string; mode?: DownstreamMode }): Promise<Downstream>;
