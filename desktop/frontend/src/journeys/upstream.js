// The upstream system a capture journey points at the window's listener: it
// sends HL7 messages over MLLP, one after another on one connection, and
// returns each acknowledgement the listener answered with. It is written from
// the protocol description and shares no readmit code — a frame is 0x0B, the
// message, 0x1C 0x0D — and it connects to loopback only.
import { connect } from "node:net";

const START = "\x0b";
const END = "\x1c\r";

export function sendMllp(address, messages) {
  const split = address.lastIndexOf(":");
  const host = address.slice(0, split);
  const port = Number(address.slice(split + 1));
  if (host !== "127.0.0.1") {
    return Promise.reject(new Error(`the upstream system sends to loopback only, not ${address}`));
  }
  return new Promise((resolve, reject) => {
    const acknowledgements = [];
    let buffered = "";
    let sent = 0;
    const socket = connect({ host, port });
    socket.setEncoding("latin1");
    socket.setTimeout(15_000, () => socket.destroy(new Error("no acknowledgement arrived in time")));
    const next = () => {
      if (sent < messages.length) {
        socket.write(START + messages[sent++] + END, "latin1");
      } else {
        socket.end();
        resolve(acknowledgements);
      }
    };
    socket.once("connect", next);
    socket.on("data", (chunk) => {
      buffered += chunk;
      for (let end = buffered.indexOf(END); end >= 0; end = buffered.indexOf(END)) {
        acknowledgements.push(buffered.slice(buffered.indexOf(START) + 1, end));
        buffered = buffered.slice(end + END.length);
        next();
      }
    });
    socket.once("error", reject);
    socket.once("close", () => {
      if (acknowledgements.length < messages.length) {
        reject(new Error(`the listener closed after ${acknowledgements.length} of ${messages.length} acknowledgements`));
      }
    });
  });
}
