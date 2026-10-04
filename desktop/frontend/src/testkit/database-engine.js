// Independent disposable PostgreSQL and MLLP application for database journeys.
// SQL runs only through the lab owner's local socket; the observer role is
// granted SELECT on one view. It writes no Readmit outcome or dataset fixture.
import { execFileSync } from "node:child_process";
import { mkdtempSync, copyFileSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createServer } from "node:net";
export async function startDatabaseEngine({ postgresBin, certificate, key }) {
  const root = mkdtempSync(join(tmpdir(), "readmit-database-"));
  const data = join(root, "data");
  const quiet = { stdio: "pipe", timeout: 30000 };
  const run = (name, args) =>
    execFileSync(join(postgresBin, name), args, quiet).toString();
  const version = run("postgres", ["--version"]).trim();
  let started = false;
  let server = null;
  const sockets = new Set();
  let mode = "wrong";
  let received = 0;
  const frames = [];
  const errors = [];
  const reserve = createServer();
  await new Promise((resolve) => reserve.listen(0, "127.0.0.1", resolve));
  const port = reserve.address().port;
  await new Promise((resolve) => reserve.close(resolve));
  const sql = (statement) =>
    run("psql", [
      "-h",
      root,
      "-p",
      String(port),
      "-U",
      "lab_owner",
      "-d",
      "application",
      "-v",
      "ON_ERROR_STOP=1",
      "-c",
      statement,
    ]);
  const reset = () => sql("DELETE FROM private_appointments");
  try {
    run("initdb", [
      "-D",
      data,
      "--auth-local=trust",
      "--auth-host=reject",
      "-U",
      "lab_owner",
      "--no-locale",
      "--encoding=UTF8",
    ]);
    copyFileSync(certificate, join(data, "server.crt"));
    copyFileSync(key, join(data, "server.key"));
    writeFileSync(
      join(data, "pg_hba.conf"),
      "local all all trust\nhostssl application observer 127.0.0.1/32 trust\nhost all all 127.0.0.1/32 reject\n",
      { mode: 0o600 },
    );
    run("pg_ctl", [
      "-D",
      data,
      "-l",
      join(root, "server.log"),
      "-w",
      "-t",
      "20",
      "-o",
      `-h 127.0.0.1 -p ${port} -k ${root} -c ssl=on`,
      "start",
    ]);
    started = true;
    run("psql", [
      "-h",
      root,
      "-p",
      String(port),
      "-U",
      "lab_owner",
      "-d",
      "postgres",
      "-v",
      "ON_ERROR_STOP=1",
      "-c",
      "CREATE ROLE observer LOGIN",
    ]);
    run("createdb", [
      "-h",
      root,
      "-p",
      String(port),
      "-U",
      "lab_owner",
      "application",
    ]);
    sql(
      "CREATE TABLE private_appointments (appointment text,status text); CREATE VIEW observed AS SELECT appointment,status FROM private_appointments; GRANT SELECT ON observed TO observer;",
    );
    server = createServer((socket) => {
      sockets.add(socket);
      socket.on("close", () => sockets.delete(socket));
      socket.on("error", (error) => errors.push(error.message));
      let buffered = Buffer.alloc(0);
      socket.on("data", (chunk) => {
        buffered = Buffer.concat([buffered, chunk]);
        for (;;) {
          const end = buffered.indexOf(Buffer.from([0x1c, 0x0d]));
          if (end < 0) return;
          const frame = buffered.subarray(0, end + 2);
          buffered = buffered.subarray(end + 2);
          if (frame[0] !== 0x0b) {
            socket.destroy();
            return;
          }
          const input = frame.subarray(1, frame.length - 2).toString("latin1");
          frames.push(input);
          frames.push(input);
          const rows = input.split("\r");
          const header = rows[0].split("|");
          const appointment = rows
            .find((row) => row.startsWith("SCH|"))
            .split("|")[1]
            .split("^")[0];
          if (!/^[A-Za-z0-9-]+$/.test(appointment)) {
            socket.destroy();
            return;
          }
          received++;
          sql(
            `INSERT INTO private_appointments VALUES ('${appointment}','${mode === "fixed" ? "booked" : "wrong"}')`,
          );
          socket.write(
            `\x0bMSH|^~\\&|DATABASE|LAB|||20260301090000||ACK|ACK-${received}|P|2.5.1\rMSA|AA|${header[9]}\r\x1c\r`,
          );
        }
      });
    });
    await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
    return {
      version,
      address: `127.0.0.1:${server.address().port}`,
      databaseAddress: `127.0.0.1:${port}`,
      setMode(next) {
        mode = next;
      },
      reset,
      received() {
        return received;
      },
      frames() {
        return [...frames];
      },
      errors() {
        return [...errors];
      },
      async close() {
        for (const socket of sockets) socket.destroy();
        await new Promise((resolve) => server.close(resolve));
        run("pg_ctl", ["-D", data, "-m", "immediate", "-w", "stop"]);
        started = false;
        rmSync(root, { recursive: true, force: true });
      },
    };
  } catch (error) {
    if (started) run("pg_ctl", ["-D", data, "-m", "immediate", "-w", "stop"]);
    rmSync(root, { recursive: true, force: true });
    throw error;
  }
}
