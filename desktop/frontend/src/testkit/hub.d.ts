// The interface of hub.js, the real customer hub journeys sign in to.

/** One person the hub's access policy grants a role in the project. */
export interface HubGrant {
  subject: string;
  role: "owner" | "admin" | "analyst" | "reviewer" | "viewer" | "runner";
}

export interface Hub {
  /** The loopback address the hub serves on, host and port. */
  address: string;
  /** The folder holding the hub-client.json a person's window selects. */
  clientConfigFolder: string;
  /** The operator's certificate authority, and the client identity's
   * certificate and key, as files a runner configuration names. */
  certificateAuthority: string;
  clientCertificate: string;
  clientKey: string;
  /** Grants subject a scoped runner token in the project and returns the
   * file that holds it. */
  runnerToken(subject: string): string;
  /** Stops the service, runs one of the hub binary's operator operations and
   * returns its output; the service stays stopped until restart. */
  operate(extra: string[]): Promise<string>;
  /** Completes a sign-in the window started, as the browser and the identity
   * provider do, authenticating subject for a session of lifetimeSeconds (five
   * minutes unless given). Resolves with the callback's status. */
  signIn(authorizationURL: string, subject: string, lifetimeSeconds?: number): Promise<number>;
  /** Issues an authorization code for subject, for a colleague's window. */
  issueCode(subject: string): string;
  /** Restarts the hub with further serve flags, as its operator would. */
  restart(extra: string[]): Promise<void>;
  stop(): Promise<void>;
}

export function startHub(options: {
  hubBinary: string;
  bridgeBinary: string;
  postgresBin: string;
  root: string;
  folder: string;
  project: string;
  grants: HubGrant[];
  licensePolicy: string;
}): Promise<Hub>;
