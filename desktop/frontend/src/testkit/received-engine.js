// Independent synthetic integration engine. It reads standard-delimiter input
// and forwards actual custom-delimiter HL7 over a separate MLLP connection.
// No Readmit parser, evaluator or result document is used by this witness.
import { createConnection, createServer } from "node:net";
export async function startReceivedEngine(captureAddress) {
  let mode = "defective";
  const received = [];
  const outputs = [];
  const connections = new Set();
  const server = createServer((socket) => {
    connections.add(socket);
    socket.on("close", () => connections.delete(socket));
    socket.on("error", () => {});
    let buffer = Buffer.alloc(0);
    let processing = Promise.resolve();
    socket.on("data", (chunk) => {
      buffer = Buffer.concat([buffer, chunk]);
      for (;;) {
        const end = buffer.indexOf(Buffer.from([0x1c, 0x0d]));
        if (end < 0) return;
        const frame = buffer.subarray(0, end + 2);
        buffer = buffer.subarray(end + 2);
        processing = processing
          .then(async () => {
            if (frame[0] !== 0x0b) throw new Error("invalid stimulus frame");
            const original = frame
              .subarray(1, frame.length - 2)
              .toString("latin1");
            received.push(original);
            const header = original.split("\r")[0].split("|");
            const appointment = original
              .split("\r")
              .find((row) => row.startsWith("SCH|"))
              .split("|")[1];
            const marker = header[2],
              control = header[9];
            const body = `MSH*@$%!*${marker}*ENGINE***20260301090000**SIU@S12*${control}*P*2.5.1\rSCH*${appointment}*${mode === "fixed" ? "booked" : "wrong"}\r`;
            const [host, port] = captureAddress.split(":");
            await new Promise((resolve, reject) => {
              const output = createConnection({ host, port: Number(port) });
              connections.add(output);
              output.on("close", () => connections.delete(output));
              output.on("error", reject);
              output.setTimeout(5000, () =>
                output.destroy(
                  new Error("capture did not acknowledge actual output"),
                ),
              );
              let reply = Buffer.alloc(0);
              output.on("connect", () => {
                outputs.push(body);
                output.write(
                  Buffer.concat([
                    Buffer.from([0x0b]),
                    Buffer.from(body, "latin1"),
                    Buffer.from([0x1c, 0x0d]),
                  ]),
                );
              });
              output.on("data", (chunk) => {
                reply = Buffer.concat([reply, chunk]);
                if (reply.indexOf(Buffer.from([0x1c, 0x0d])) >= 0) {
                  output.end();
                  resolve();
                }
              });
            });
            socket.write(
              `\x0bMSH|^~\\&|ENGINE|LAB|||20260301090000||ACK|ACK-${received.length}|P|2.5.1\rMSA|AA|${control}\r\x1c\r`,
            );
          })
          .catch(() => socket.destroy());
      }
    });
  });
  await new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });
  return {
    address: `127.0.0.1:${server.address().port}`,
    setMode(next) {
      mode = next;
    },
    received() {
      return [...received];
    },
    outputs() {
      return [...outputs];
    },
    reset() {},
    async close() {
      for (const connection of connections) connection.destroy();
      await new Promise((resolve) => server.close(resolve));
    },
  };
}
