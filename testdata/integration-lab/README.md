# Owned independent lab fixtures

Every identifier, name, appointment, result and location here is fictional.
No customer export, real patient record, clinical notification or vendor service
is involved. `LAB`, `PLACER` and `FILLER` are fixture identifier authorities;
`urn:readmit:lab:*` are their explicitly selected FHIR identifier systems.
`ZLG` binds each input to its isolated lab generation and is not clinical data.

The interface contract is deliberately finite:

- ADT A01 creates the fictional Lark patient and an ambulatory encounter.
- SIU S12 books at 09:30 UTC on 2030-01-02; S13 changes the same appointment
  business identifier to 10:00 UTC. SCH-11's fourth component carries the start.
  AIP-3 and AIL-3 name the separately provisioned practitioner and location.
- ORM O01 creates the explicitly named service request, preserving placer and
  filler authorities. ORU R01 emits two textual observations and a diagnostic
  report linked to that request and patient. Numeric clinical interpretation
  is deliberately outside this fixture contract.
- The FHIR-native Appointment uses a separate baseline patient so that its
  qualification does not depend on the ADT route producing a patient.

`baseline.json` contains only prerequisites and named negative-control targets.
The wrong patient/order exist so that a linkage defect can receive positive
transport responses. The qualification helper never constructs or inserts the
expected OIE-transformed Patient, Encounter, Appointment, Observation or
DiagnosticReport. OIE's actual channel owns those transformations.

`expected.json` is the independently authored oracle. Neither the OIE channel
nor the native API target imports it. The receiver retains every accepted frame
until a generation reset; it never stops at an expected message count.

The source protocol facts, target-specific mapping and expected behavior apply
only to this fictional interface. They are not universal HL7/FHIR mappings,
clinical rules, EHR compatibility claims or Epic/Oracle Health certification.
