// What a journey observes outside the window, from the machine's side:
// whether anything accepts a connection on a loopback address, how many
// connections reached a destination it configured, how many entries a folder
// the application writes holds, and the host's load. The journeys run in Node
// beneath jsdom, so these are ordinary Node calls; they read, and never write,
// listen or connect anywhere but loopback.
import { existsSync, readdirSync } from "node:fs";
import { connect, createServer } from "node:net";
import { loadavg } from "node:os";

export async function freeLoopbackAddress() {
  const server = createServer();
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  const { port } = server.address();
  await new Promise((resolve) => server.close(() => resolve()));
  return `127.0.0.1:${port}`;
}

export async function countingListener() {
  let accepted = 0;
  const server = createServer((socket) => {
    accepted += 1;
    socket.destroy();
  });
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  const { port } = server.address();
  return {
    address: `127.0.0.1:${port}`,
    accepted: () => accepted,
    close: () => new Promise((resolve) => server.close(() => resolve())),
  };
}

export function accepts(address) {
  const [host, port] = address.split(":");
  return new Promise((resolve) => {
    const socket = connect({ host, port: Number(port) });
    socket.once("connect", () => {
      socket.destroy();
      resolve(true);
    });
    socket.once("error", () => resolve(false));
  });
}

export function entries(folder) {
  return existsSync(folder) ? readdirSync(folder).length : 0;
}

export function exists(path) {
  return existsSync(path);
}

export function hostLoad() {
  return loadavg().map((value) => value.toFixed(2)).join(" ");
}

/** Whether the opt-in window measurements were asked for. */
export function measuring() {
  return process.env.READMIT_PERFORMANCE === "1";
}
