// A real customer hub for journeys: the checkout's readmit-hub binary serving
// over mutual TLS on loopback, its store in a disposable PostgreSQL cluster
// created inside the journey's root and removed with it, and a customer
// identity provider that answers an authorization code with the signed access
// token of the person the journey names. Nothing here is a stub of the hub:
// the window reaches the service its operator runs, through the same client
// configuration and sign-in a person uses. Every key, certificate and token is
// newly generated and synthetic, and every listener binds 127.0.0.1.
//
// This file is JavaScript for the same reason as bridge-process.js; hub.d.ts
// states its interface.
import { execFileSync, spawn } from "node:child_process";
import { createHash, generateKeyPairSync, randomBytes, sign, X509Certificate } from "node:crypto";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { createServer, request as httpRequest } from "node:http";
import { request as httpsRequest } from "node:https";
import { createServer as createNetServer } from "node:net";
import { join } from "node:path";
import { isolated, pathInRoot } from "./bridge-process.js";

const ISSUER = "https://idp.journey.test";
const AUDIENCE = "https://hub.journey.test";
const CLIENT = "readmit-journey-client";
const SCOPES = ["evidence.read", "evidence.write", "execution", "approval", "export", "enrollment", "admin", "ownership"];
const DATABASE = "readmit_hub";
const ROLE = "hub";

/** Whether a child process has ended, by exit or by signal. */
function ended(child) {
  return child.exitCode !== null || child.signalCode !== null;
}

/** A free loopback port, as the operating system assigns one. */
function freePort() {
  return new Promise((resolve, reject) => {
    const server = createNetServer();
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => {
      const { port } = server.address();
      server.close(() => resolve(port));
    });
  });
}

function writeJSON(path, value) {
  writeFileSync(path, JSON.stringify(value), { mode: 0o600 });
  return path;
}

function base64url(value) {
  return Buffer.from(value).toString("base64url");
}

/** The customer identity provider: an access token endpoint on loopback that
 * answers each authorization code the journey issued, once, with a token valid
 * for the lifetime it was issued with. */
function startIdentityProvider() {
  const { privateKey, publicKey } = generateKeyPairSync("rsa", { modulusLength: 2048 });
  const jwk = publicKey.export({ format: "jwk" });
  const codes = new Map();
  const token = (subject, lifetimeSeconds = 300) => {
    const now = Math.floor(Date.now() / 1000);
    const header = base64url(JSON.stringify({ alg: "RS256", typ: "at+jwt", kid: "journey" }));
    const claims = base64url(
      JSON.stringify({
        iss: ISSUER,
        aud: AUDIENCE,
        sub: subject,
        client_id: CLIENT,
        jti: randomBytes(12).toString("hex"),
        iat: now - 1,
        exp: now + lifetimeSeconds,
        scope: SCOPES.join(" "),
      }),
    );
    const input = `${header}.${claims}`;
    return `${input}.${sign("sha256", Buffer.from(input), privateKey).toString("base64url")}`;
  };
  const server = createServer((request, response) => {
    let body = "";
    request.setEncoding("utf8");
    request.on("data", (chunk) => {
      body += chunk;
    });
    request.on("end", () => {
      const code = new URLSearchParams(body).get("code") ?? "";
      const issued = codes.get(code);
      codes.delete(code);
      if (request.method !== "POST" || request.url !== "/token" || !issued) {
        response.writeHead(400, { "Content-Type": "application/json" });
        response.end(JSON.stringify({ error: "invalid_grant" }));
        return;
      }
      response.writeHead(200, { "Content-Type": "application/json" });
      response.end(JSON.stringify({ access_token: issued.token, token_type: "Bearer", expires_in: issued.lifetimeSeconds }));
    });
  });
  const listening = new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => resolve(server.address().port));
  });
  return listening.then((port) => ({
    port,
    key: { kid: "journey", n: jwk.n, e: jwk.e },
    issue(code, subject, lifetimeSeconds = 300) {
      codes.set(code, { token: token(subject, lifetimeSeconds), lifetimeSeconds });
    },
    close: () => new Promise((resolve) => server.close(() => resolve())),
  }));
}

/** GETs a loopback URL and resolves with its status once the answer ends:
 * what the person's browser does when the identity provider redirects it
 * back to the window. */
function visit(url) {
  return new Promise((resolve, reject) => {
    const request = httpRequest(url, (response) => {
      response.resume();
      response.on("end", () => resolve(response.statusCode));
    });
    request.on("error", reject);
    request.end();
  });
}

/** Asks the hub whether it is ready, presenting the client identity. */
function probe(address, identity) {
  return new Promise((resolve) => {
    const request = httpsRequest(
      { host: "127.0.0.1", port: Number(address.split(":")[1]), path: "/health/ready", method: "GET", ...identity, timeout: 2000 },
      (response) => {
        response.resume();
        response.on("end", () => resolve(response.statusCode));
      },
    );
    request.on("error", () => resolve(0));
    request.on("timeout", () => {
      request.destroy();
      resolve(0);
    });
    request.end();
  });
}

/** Starts a real hub inside root: see this file's opening comment. In
 * "operator" mode the hub is served operator-only, without its access policy:
 * an opaque artifact store any client of its authority reaches with the client
 * certificate alone, whose operation policy binds that certificate to the
 * licensed author, as its operator does. */
export async function startHub({ hubBinary, bridgeBinary, postgresBin, root, folder, project, grants, licensePolicy, mode = "team" }) {
  const dir = pathInRoot(root, folder);
  mkdirSync(dir, { recursive: true, mode: 0o700 });
  // Every process here sees the journey's isolated environment: an empty
  // PATH, so each tool is named by its absolute path, and a home inside root.
  const environment = isolated(root);
  const quiet = { env: environment, stdio: ["ignore", "pipe", "pipe"] };

  // The operator's certificates: a synthetic authority, the hub's server
  // identity and the one client identity the people and the runner present.
  execFileSync(bridgeBinary, ["--root", root, "--hub-certificates", dir], quiet);
  const clientCertificate = new X509Certificate(readFileSync(join(dir, "client.pem")));
  const certificateDigest = createHash("sha256").update(clientCertificate.raw).digest("hex");
  const identity = {
    ca: readFileSync(join(dir, "ca.pem")),
    cert: readFileSync(join(dir, "client.pem")),
    key: readFileSync(join(dir, "client-key.pem")),
  };

  // The hub's store: a disposable cluster whose only door is a socket inside
  // root, removed with root once it is stopped.
  const data = join(dir, "postgres");
  const socket = pathInRoot(root, "pg");
  mkdirSync(socket, { recursive: true, mode: 0o700 });
  const postgresPort = await freePort();
  let databaseStarted = false;
  const stopDatabase = () => {
    if (!databaseStarted) return;
    databaseStarted = false;
    // pg_ctl refuses to stop a server that is not running; either way no
    // server outlives the journey.
    try {
      execFileSync(join(postgresBin, "pg_ctl"), ["-D", data, "-m", "fast", "-w", "stop"], quiet);
    } catch {
      // Not running.
    }
  };
  let hubProcess = null;
  let provider = null;
  try {
    execFileSync(join(postgresBin, "initdb"), ["-D", data, "-A", "trust", "-U", ROLE, "-E", "UTF8", "--no-locale"], quiet);
    databaseStarted = true;
    execFileSync(
      join(postgresBin, "pg_ctl"),
      ["-D", data, "-l", join(dir, "postgres.log"), "-o", `-k ${socket} -p ${postgresPort} -h ''`, "-w", "start"],
      quiet,
    );
    execFileSync(join(postgresBin, "createdb"), ["-h", socket, "-p", String(postgresPort), "-U", ROLE, DATABASE], quiet);
    provider = await startIdentityProvider();
    const hubPort = await freePort();
    const address = `127.0.0.1:${hubPort}`;
    mkdirSync(join(dir, "artifacts"), { recursive: true, mode: 0o700 });
    const config = writeJSON(join(dir, "hub.json"), {
      schema: "readmit-hub-config/v1",
      listen: address,
      artifact_root: join(dir, "artifacts"),
      postgres_socket: socket,
      postgres_port: postgresPort,
      postgres_database: DATABASE,
      postgres_user: ROLE,
      tls_certificate: join(dir, "server.pem"),
      tls_key: join(dir, "server-key.pem"),
      client_ca: join(dir, "ca.pem"),
      max_storage_bytes: 256 * 1024 * 1024,
    });
    const access = { schema: "readmit-hub-access/v1", issuer: ISSUER, audience: AUDIENCE, clients: [CLIENT], keys: [provider.key], grants: grants.map((grant) => ({ project, subject: grant.subject, role: grant.role })), tokens: [] };
    const accessPath = writeJSON(join(dir, "access.json"), access);
    // Operator-only, the certificate itself is the author's identity.
    const bindings =
      mode === "operator"
        ? [{ issuer: "mutual-tls", subject: certificateDigest, certificate_sha256: certificateDigest, author: "test-author", device: "test-device" }]
        : grants.map((grant) => ({ issuer: ISSUER, subject: grant.subject, certificate_sha256: certificateDigest, author: "test-author", device: "test-device" }));
    const operation = writeJSON(join(dir, "hub-operation.json"), {
      schema: "readmit-hub-operation-policy/v1",
      operation_policy: licensePolicy,
      bindings,
    });
    const clientFolder = join(dir, "client");
    mkdirSync(clientFolder, { recursive: true });
    writeJSON(join(clientFolder, "hub-client.json"), {
      schema: "readmit-hub-client/v1",
      hub: `https://${address}`,
      ca: join(dir, "ca.pem"),
      certificate: join(dir, "client.pem"),
      key: { command: "/bin/cat", arguments: [join(dir, "client-key.pem")] },
      idp: {
        issuer: ISSUER,
        client_id: CLIENT,
        audience: AUDIENCE,
        authorize_endpoint: `${ISSUER}/authorize`,
        token_endpoint: `http://127.0.0.1:${provider.port}/token`,
        scopes: SCOPES,
      },
      projects: [project],
    });
    // The operator-only configuration names the same client identity and no
    // identity provider or project, which an operator-only hub has none of.
    const operatorConfig = writeJSON(join(clientFolder, "hub-operator.json"), {
      schema: "readmit-hub-operator-client/v1",
      hub: `https://${address}`,
      ca: join(dir, "ca.pem"),
      certificate: join(dir, "client.pem"),
      key: { command: "/bin/cat", arguments: [join(dir, "client-key.pem")] },
    });
    execFileSync(hubBinary, ["-config", config, "migrate"], quiet);

    let log = "";
    let team = mode !== "operator";
    const serve = async (extra) => {
      const access = team ? ["-access-policy", accessPath] : [];
      const child = spawn(hubBinary, ["-operation-policy", operation, "-config", config, ...access, ...extra, "serve"], {
        env: environment,
        stdio: ["ignore", "pipe", "pipe"],
      });
      child.stdout.setEncoding("utf8");
      child.stderr.setEncoding("utf8");
      child.stdout.on("data", (chunk) => {
        log += chunk;
      });
      child.stderr.on("data", (chunk) => {
        log += chunk;
      });
      const deadline = Date.now() + 30_000;
      for (;;) {
        if (ended(child)) throw new Error(`the hub exited while starting: ${log}`);
        if ((await probe(address, identity)) === 204) break;
        if (Date.now() > deadline) {
          // A hub that never became ready is ended here; nothing it started
          // outlives the journey.
          child.kill("SIGKILL");
          throw new Error(`the hub did not become ready: ${log}`);
        }
        await new Promise((resolve) => setTimeout(resolve, 50));
      }
      return child;
    };
    const stopHub = () =>
      new Promise((resolve) => {
        const child = hubProcess;
        hubProcess = null;
        if (!child || ended(child)) {
          resolve();
          return;
        }
        child.once("exit", () => resolve());
        child.kill("SIGTERM");
      });
    hubProcess = await serve([]);

    let issuedTokens = 0;
    return {
      address,
      clientConfigFolder: clientFolder,
      operatorConfig,
      certificateAuthority: join(dir, "ca.pem"),
      clientCertificate: join(dir, "client.pem"),
      clientKey: join(dir, "client-key.pem"),
      /** Grants subject the runner role and a scoped runner token for
       * enrollment and execution in the project, bound to the client
       * certificate, as the hub's operator does in the access policy the hub
       * reads on every request. Returns the file the token is kept in, which
       * the runner's token reader reads. */
      runnerToken(subject) {
        issuedTokens += 1;
        const token = `rh_${randomBytes(32).toString("base64url")}`;
        // A runner token belongs to a subject the policy grants the runner
        // role in the project.
        if (!access.grants.some((grant) => grant.subject === subject)) {
          access.grants.push({ project, subject, role: "runner" });
        }
        access.tokens.push({
          sha256: createHash("sha256").update(token).digest("hex"),
          subject,
          project,
          actions: ["enrollment", "execution"],
          expires_at: new Date(Date.now() + 60 * 60 * 1000).toISOString().replace(/\.\d{3}Z$/, "Z"),
          certificate_sha256: certificateDigest,
          kind: "runner",
        });
        writeJSON(accessPath, access);
        const path = join(dir, `runner-token-${issuedTokens}`);
        writeFileSync(path, token, { mode: 0o600 });
        return path;
      },
      /** Runs one of the hub binary's operator operations, such as
       * schedule-init or schedule-pin, with the hub's own configuration and
       * the service stopped, as its operator does: the store admits one hub
       * process at a time. The service stays stopped until restart. */
      async operate(extra) {
        await stopHub();
        return execFileSync(hubBinary, ["-operation-policy", operation, "-config", config, ...extra], { env: environment, encoding: "utf8" });
      },
      /** Completes a sign-in the window started, as the person's browser and
       * the identity provider do: the provider authenticates subject and
       * redirects to the window's loopback callback with a fresh code, for a
       * session of lifetimeSeconds (five minutes unless given). */
      async signIn(authorizationURL, subject, lifetimeSeconds) {
        const authorization = new URL(authorizationURL);
        const redirect = new URL(authorization.searchParams.get("redirect_uri") ?? "");
        if (redirect.hostname !== "127.0.0.1") throw new Error("the sign-in redirects somewhere other than loopback");
        const code = randomBytes(16).toString("hex");
        provider.issue(code, subject, lifetimeSeconds);
        redirect.searchParams.set("code", code);
        redirect.searchParams.set("state", authorization.searchParams.get("state") ?? "");
        return visit(redirect.toString());
      },
      /** Issues a code for subject without a browser, for a colleague's
       * window that exchanges it itself. */
      issueCode(subject) {
        const code = randomBytes(16).toString("hex");
        provider.issue(code, subject);
        return code;
      },
      /** Restarts the hub as its operator would to install policies. */
      async restart(extra) {
        await stopHub();
        hubProcess = await serve(extra);
      },
      /** Restarts the hub in the mode its operator chooses: "team" serves it
       * with its access policy, which marks its store team-enabled for good,
       * and "operator" serves it without. */
      async serveAs(next) {
        await stopHub();
        team = next === "team";
        hubProcess = await serve([]);
      },
      async stop() {
        await stopHub();
        await provider.close();
        stopDatabase();
      },
    };
  } catch (error) {
    if (hubProcess) hubProcess.kill("SIGKILL");
    if (provider) await provider.close();
    stopDatabase();
    throw error;
  }
}
