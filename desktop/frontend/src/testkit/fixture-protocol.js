// Independent operator fixture service. It shares no Readmit code or verdicts.
import { createServer } from "node:https";
import { execFileSync } from "node:child_process";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { isDeepStrictEqual } from "node:util";
export async function startFixtureProtocol({ folder, project, reset }) {
  mkdirSync(folder, { recursive: true });
  const config = join(folder, "certificate.cnf"),
    key = join(folder, "key.pem"),
    certificate = join(folder, "certificate.pem");
  writeFileSync(
    config,
    "[req]\ndistinguished_name=dn\nx509_extensions=ext\nprompt=no\n[dn]\nCN=localhost\n[ext]\nsubjectAltName=DNS:localhost,IP:127.0.0.1\n",
    { mode: 0o600 },
  );
  execFileSync(
    "openssl",
    [
      "req",
      "-x509",
      "-newkey",
      "rsa:2048",
      "-nodes",
      "-days",
      "2",
      "-config",
      config,
      "-keyout",
      key,
      "-out",
      certificate,
    ],
    { stdio: "ignore" },
  );
  const authorities = readFileSync(certificate, "utf8");
  let lease = {},
    resource = null,
    version = 0,
    created = 0,
    deleted = 0;
  const server = createServer(
    { key: readFileSync(key), cert: authorities },
    async (request, response) => {
      const url = new URL(request.url, "https://localhost"),
        action = url.pathname.replace("/fixture/v1/", "");
      const role =
        request.method === "GET"
          ? "read"
          : ["delete", "lease-release"].includes(action)
            ? "cleanup"
            : "setup";
      const reject = (text) => {
        response.writeHead(409);
        response.end(text);
      };
      if (request.headers.authorization !== `Bearer journey-${role}`) {
        response.writeHead(403);
        response.end();
        return;
      }
      const chunks = [];
      for await (const chunk of request) chunks.push(chunk);
      let body, scope;
      try {
        body = chunks.length
          ? JSON.parse(Buffer.concat(chunks).toString())
          : null;
        scope =
          request.method === "GET"
            ? JSON.parse(url.searchParams.get("scope"))
            : body.scope;
      } catch {
        reject("invalid scope");
        return;
      }
      if (
        scope.project !== project ||
        scope.environment !== "journey" ||
        scope.revision !== "1" ||
        scope.adapter_revision !== "1" ||
        scope.tenant !== "synthetic" ||
        scope.namespace !== "journey-fixtures" ||
        !scope.owner ||
        !scope.lease_key
      ) {
        reject("wrong deployment");
        return;
      }
      const send = (value) => {
        response.writeHead(200, { "Content-Type": "application/json" });
        response.end(JSON.stringify(value));
      };
      const schema = "readmit-fixture-adapter/v1";
      if (action === "capabilities") {
        send({
          schema,
          scope,
          protocol: "typed-fixture-v1",
          lease_mode: "exclusive-no-expiry",
          version_guards: true,
          templates: [{ id: "patient", kind: "patient", attributes: ["name"] }],
        });
        return;
      }
      if (action === "state") {
        send({ schema, scope, lease, resources: resource ? [resource] : [] });
        return;
      }
      if (!body || body.schema !== schema || body.action !== action) {
        reject("invalid request");
        return;
      }
      if (action === "lease-acquire") {
        if (lease.owner) {
          reject("already leased");
          return;
        }
        lease = {
          key: scope.lease_key,
          owner: scope.owner,
          version: String(++version),
        };
      } else {
        if (
          !isDeepStrictEqual(body.lease, lease) ||
          lease.owner !== scope.owner
        ) {
          reject("wrong fence");
          return;
        }
        if (action === "create") {
          if (
            resource ||
            body.resource?.kind !== "patient" ||
            body.resource?.template !== "patient"
          ) {
            reject("wrong resource");
            return;
          }
          resource = {
            ...body.resource,
            owner: scope.owner,
            version: String(++version),
          };
          created++;
          reset();
        } else if (action === "delete") {
          if (!resource || !isDeepStrictEqual(body.resource, resource)) {
            reject("changed owned resource");
            return;
          }
          resource = null;
          deleted++;
        } else if (action === "lease-release") {
          if (resource) {
            reject("resource remains");
            return;
          }
          lease = {};
        } else {
          reject("unsupported action");
          return;
        }
      }
      send({
        schema,
        scope,
        lease,
        ...(resource ? { resource } : {}),
        outcome: "applied",
      });
    },
  );
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  return {
    url: `https://127.0.0.1:${server.address().port}`,
    authorities,
    encodedAuthorities: Buffer.from(authorities).toString("base64"),
    created: () => created,
    deleted: () => deleted,
    close: () => new Promise((resolve) => server.close(resolve)),
  };
}
