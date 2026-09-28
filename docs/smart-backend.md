# SMART Backend Services

`internal/smartbackend` authorizes explicitly configured nonproduction FHIR
clients using SMART Backend Services **2.2**, client credentials and asymmetric
`private_key_jwt` authentication. It supports RS384 with 2048–4096 bit RSA keys
and ES384 with P-384 keys. This is customer-side engine behavior; it does not use
a customer Hub login, require a vendor service, or implement a frontend login.
A token or HTTP success is never an application-test verdict.

## Configuration and registration

`readmit-smart-backend/v1` names one FHIR base, exact token endpoint and matching
audience, registered client ID, role (`observer` or `setup`), explicit system
scopes, selected algorithm, key ID/generation, and an existing secret-provider
locator. Its public JWKS records the approved public keys; the selected private
key must match that public key before an assertion is sent. Duplicate key IDs,
private JWK members, unsupported algorithms and algorithm/key confusion are
refused. Client registration and customer permissions are deployment prerequisites;
readmit never enrolls or approves the client itself.

The registration can declare its exact HTTPS JWKS URL, which is placed in the
assertion's `jku` header. That URL is a registered arrangement for the authorization
server, not a URL readmit fetches automatically. Direct public JWKS registration
is also supported. No remotely supplied `jku` selects a signing key. Keys are
read only through `secret.Locator.Read`, with the existing bounded executable
provider, discarded provider diagnostics and no shell. No key store is added.

The token action carries the existing explicit plan/source, project/environment
revision, endpoint, verified TLS configuration and scoped destination policy.
Preparation reads only its input bytes and validates the local declarations;
`Preflight` reports `ready` for a supported local configuration, not verified
registration or usable credentials. Resolving the key and establishing a grant
require explicit execution authority. Environments/navigation do not call these
actions. A v5 connected lifecycle selects one client per role, bound to its
plan ([connected FHIR lifecycle tests](connected-fhir.md)). No existing no-auth laboratory or credential-reference mode changes or
silently falls back to another mode.

## Explicit discovery

`PrepareDiscovery` names exactly `FHIR_BASE/.well-known/smart-configuration` and
prepares a GET with bound `Accept: application/json` under its own scoped authority. `Execute` performs that explicit
network action and produces `readmit-smart-discovery/v1`, containing the base,
request declaration, network policy, redacted receipt and the selected public
metadata fields used by this implementation. Unknown extensions, response
headers, cookies and diagnostics are not retained as discovery metadata.
`DecodeDiscovery` reconstructs the binding offline without contacting the server.

A configuration can pin the digest of that complete reviewed discovery document.
`Prepare` verifies the digest, FHIR base, exact token endpoint, selected signing
algorithm, client-credentials grant, asymmetric authentication and permission-v2
capabilities. Replacing the metadata or changing its endpoint requires a new
reviewed configuration. A separately approved token endpoint can have a different
origin from the FHIR service; discovery itself never grants access to it.
Explicit endpoint configuration without discovery remains available.

## Scopes and actual requests

Resource names reuse the fixed FHIR R4 vocabulary. Requested permissions are
explicit `system/Resource.cruds` scopes with an ordered
nonempty subset of `cruds`; `*` is accepted only when configured explicitly.
The observer role permits only read/search (`rs`); setup uses a separately
configured identity and permissions. Granted scopes must cover exactly the
configured permission union. Split equivalent scopes are accepted; a reduced or
broader grant is refused. Legacy `.read`/`.write`, query-constrained scopes and
unrecognized scope syntax are explicitly unsupported, never widened or silently
downgraded.

The actual HTTP adapter is `Session.Execute`, using
`networkaction.PrepareRuntimeHTTP` and its `readmit-runtime-http-action/v1`
contract. The runtime action binds the SMART configuration identity. The provider
checks the same plan/project/environment revision, exact origin and FHIR base,
resource path, method and required interaction before obtaining a bearer.
Instance and history IDs use the FHIR ID grammar; dot segments, encoded paths,
`$operations`, cross-origin URLs and requests outside the base are refused.

The finite adapter currently admits type search GET, instance/version GET,
type POST create, instance PUT/PATCH update and instance DELETE. It does not
interpret REST results, complete paginated search, execute transactions, or
implement custom operations; IG11 owns those protocol semantics. A caller must
retain its own durable mutation intent before invoking a mutating action. The
runtime transport never infers a successful application outcome.

## Runtime credentials and renewal

The shared memory-only executor admits the destination before calling a provider,
then rechecks exact live authority before key resolution, dialing and writes.
The material carries a live provider guard, so disconnect or invalidation after
material resolution still prevents a later transport write. It
uses the existing single-use route and verified TLS stack. There is no ambient
proxy inheritance, redirect following or automatic transport retry. Explicit
proxies are not supported by this contract; a deployment must use direct approved
routes rather than allowing environment variables to choose one.

Assertions use `iss = sub = registered client ID`, exact token-endpoint `aud`,
`typ=JWT`, selected `alg`/`kid`, cryptographically random 256-bit `jti`, and a
two-minute lifetime. Private keys and assertions exist only while signing/sending.
Token requests have no refresh token. A bounded successful response must declare
an opaque bearer, lifetime of 1–300 seconds and granted scopes. Server clock
metadata outside a two-minute tolerance is refused.

Each session holds at most one token for its immutable configuration, client,
key generation and scopes, additionally bound to the token authority and request
actor. Concurrent calls share a single exchange. Tokens refresh before expiry with a margin of the smaller of 30 seconds or
one fifth of their lifetime; short-lived tokens remain reusable without a renewal
loop. A failed renewal never becomes an empty observation. Authority changes
refuse stale admission. Call `Invalidate` when rotating a key/changing a grant,
and `Disconnect` when leaving the authorized execution; both erase the cache,
cancel pending work and permanently close that session. A new configuration or
session requires current independent admission. Keys are not cached.

A received 401 allows at most one renewal and retry for an admitted GET. Each
retry receives fresh destination admission and live-authority checks. No POST,
PUT, PATCH, DELETE, transaction or uncertain transport failure is retried as an
authentication convenience. OAuth response diagnostics are not copied into errors.
The closed `readmit-smart-status/v1` categories include registration missing,
grant insufficient, clock outside validity, key unavailable, server unsupported,
authority changed, auth unavailable and disconnected.

## Evidence and verification

Runtime material and sessions mask every formatting verb and refuse JSON.
Responses expose private bytes only explicitly to the protocol adapter.
`readmit-runtime-http-receipt/v1` records binding, actor, state, HTTP status and
redacted destination decision; it contains no header manifest, token, assertion,
response-body digest or key. Known credential echoes in response bytes or headers
are refused. No engine log, browser storage or UI credential projection is added.
The existing artifact-producing HTTP v1 reader/executor remains unchanged.

Independent local TLS fixtures verify signatures with standard-library public-key
verification, separately parse claims and enforce client ID, audience, algorithm,
key ID, expiry, unique replay IDs and requested scopes. Tests exercise both
algorithms, actual protected requests, single-flight use, renewal, rotation,
revocation, reduced scopes, clock drift, unavailable keys, outage, explicit
metadata pinning and transport refusal/redaction. These synthetic fixtures prove
the implemented boundary; they do not qualify an EHR vendor or establish a
customer's enrollment, clinical access, data completeness or workflow success.

Sources: [SMART Backend Services 2.2](https://hl7.org/fhir/smart-app-launch/STU2.2/backend-services.html),
[asymmetric client authentication](https://hl7.org/fhir/smart-app-launch/STU2.2/client-confidential-asymmetric.html),
and [SMART scopes](https://hl7.org/fhir/smart-app-launch/STU2.2/scopes-and-launch-context.html).
