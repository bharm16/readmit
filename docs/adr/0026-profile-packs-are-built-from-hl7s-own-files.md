---
status: accepted
date: 2026-09-28
---

# Profile packs are built from HL7's own published files

Base v2 profile packs are built from two files HL7 International publishes to
its registered users for each edition: the "HL7 Version 2.x Messaging Schemas"
and the edition's normative chapters. nHapi, HL7apy, NIST's export of the HL7
database and the hl7.eu rendering are no longer sources. This replaces the
source choice in ADR-0009 and D1, and the NIST source in ADR-0013; the pack
contract, evaluator and support levels are unchanged.

A development-time extractor reads each edition's two files once into a JSON
dataset. Builds read only datasets. The chapter's tables decide usage,
repetition and table, and fill what the schema lacks (omitted elements,
2.7.1's conformance lengths). The schema supplies message structures with
their groups and choices, element lists, data types and lengths. The receipt
lists every usage, repetition, table and data type difference between the
two. The
reviewed conditions keep their typed encodings; their basis spans cite HL7's
chapter text.

## Trade-off

HL7's v2 database is the one source that holds everything, but it is a
separate purchase and is not offered to the owner's account. HL7's schemas are
free to registered users and come from that database, but they carry no usage
code beyond required-or-not, and HL7 states they are not normative. Derived
open-source models carry defects of their own: NIST's export nests ten
ADT/SIU/ORM/ORU structures wrongly (in 2.3.1, 2.4, 2.5 and 2.8.2) and omits
most 2.3.1 ADT structures; nHapi flattens choices, and nHapi and HL7apy differ
from HL7's schemas in six 2.6–2.8.2 ADT structures; HL7apy makes single 2.8.2
fields repeating. They also add MPL-2.0 and MIT obligations. Reading the schemas and the chapters, cross-checked against each
other, gives HL7's own structures and its normative usage. The cost is a PDF
text reader whose output depends on the pdftotext version the dataset records,
and a short list of reviewed repairs and one adaptation, each recorded in the
receipt.

## Consequences

The rights question narrows to HL7's own terms; it still blocks distribution
(#627), and datasets and packs stay outside source control. Packs are new
identities (`hl7-v2-<version>` version 1) under `readmit-profile-pack/v5`;
earlier pins, operators and stored results are unchanged. Updating to a newer
HL7 file is a new manifest entry, a new dataset and a new pack version.
