// The interface of probes.js: what a journey reads from the machine's side of
// the window.

/** A loopback address nothing listens on yet. */
export function freeLoopbackAddress(): Promise<string>;
/** A loopback destination a journey configures: it counts every connection
 * that reaches it and answers none, the witness that nothing did. */
export interface CountingListener {
  address: string;
  accepted(): number;
  close(): Promise<void>;
}
export function countingListener(): Promise<CountingListener>;
/** Whether something accepts a connection at this loopback address now. */
export function accepts(address: string): Promise<boolean>;
/** How many entries a folder holds; zero when it does not exist yet. */
export function entries(folder: string): number;
/** Whether a path exists. */
export function exists(path: string): boolean;
/** The host's one, five and fifteen minute load averages. */
export function hostLoad(): string;
/** Whether READMIT_PERFORMANCE=1 asked for the opt-in window measurements. */
export function measuring(): boolean;
