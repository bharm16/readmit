// The interface of deployment.js, the customer's deployment authority that
// approves staged runner updates.

export interface DeploymentAuthority {
  /** The Ed25519 public key a runner configuration pins, in standard
   * base64. */
  publicKey: string;
  /** A readmit-runner-update/v1 manifest approving the candidate whose bytes
   * have this SHA-256 as engine on this machine's platform, signed by this
   * authority unless signed is false. */
  manifest(claims: { engine: string; sha256: string; signed?: boolean }): string;
}

export function deploymentAuthority(): DeploymentAuthority;
