- Added optional local FHIR R4 profile validation. The pinned official HL7
  validator 6.10.4 runs on Java 21 inside a network-less container that an
  administrator builds from digest-pinned inputs. Results distinguish
  conformance and nonconformance from unavailable terminology, profiles and
  invariants, and from worker timeouts and crashes. Reading retained evidence
  needs no worker. No command, desktop screen or runner step uses it yet, and
  non-FHIR work never needs Java.
