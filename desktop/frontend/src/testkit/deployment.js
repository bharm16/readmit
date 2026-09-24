// The customer's deployment authority, as the administrator who approves
// runner updates holds it: an Ed25519 key generated for one journey and never
// written anywhere, whose public half a runner configuration pins, and which
// signs readmit-runner-update/v1 manifests over a staged candidate's bytes.
// It is written from the manifest's documentation (docs/customer-runner.md),
// not from readmit's verifier: the signature covers the manifest's JSON with
// its members in order and the signature member empty.
//
// This file is JavaScript so the typed frontend needs no Node type
// declarations; deployment.d.ts states its interface.
import { generateKeyPairSync, sign } from "node:crypto";

// The platform names a Go build reports for this machine, which a manifest
// states and the verifier compares with its own.
const GOOS = { darwin: "darwin", linux: "linux", win32: "windows" };
const GOARCH = { arm64: "arm64", x64: "amd64" };

/** This machine's platform as a manifest names it. */
function platform() {
  const os = GOOS[process.platform];
  const arch = GOARCH[process.arch];
  if (!os || !arch) throw new Error(`no Go platform name for ${process.platform}/${process.arch}`);
  return { os, arch };
}

/** A new deployment authority with its own key. */
export function deploymentAuthority() {
  const { publicKey, privateKey } = generateKeyPairSync("ed25519");
  return {
    publicKey: Buffer.from(publicKey.export({ format: "jwk" }).x, "base64url").toString("base64"),
    manifest({ engine, sha256, signed = true }) {
      const claims = { schema: "readmit-runner-update/v1", engine, ...platform(), sha256, signature: "" };
      if (signed) {
        claims.signature = sign(null, Buffer.from(JSON.stringify(claims)), privateKey).toString("base64");
      }
      return JSON.stringify(claims);
    },
  };
}
