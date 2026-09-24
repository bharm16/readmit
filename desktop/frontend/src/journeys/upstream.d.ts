// The interface of upstream.js: the upstream system a capture journey sends
// from.

/** Sends each message in its MLLP frame, one after another on one loopback
 * connection, and resolves with each acknowledgement's text once every
 * message was answered. */
export function sendMllp(address: string, messages: string[]): Promise<string[]>;
