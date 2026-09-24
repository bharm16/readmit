# Synthetic integration-engine export lab

These `.xml` files are **unaltered message exports produced by the engines** in
isolated, disposable labs on 2026-09-24. Every message fed to the engines came
from the synthetic seed-35 generator in `lab/`; no patient data or customer
connection was used. `SHA256SUMS` identifies the exact committed bytes. The
channel XML and scripts under `lab/` are setup material, not message evidence.
The fixture set is a technical compatibility candidate; redistribution and
rights review assigned to the owner remains open. An adapter declaration does
not authenticate a customer's file as having come from either engine.

| Engine | Exact lab artifact | Runtime | Message export source |
| --- | --- | --- | --- |
| Mirth Connect 4.5.2 | `nextgenhealthcare/connect@sha256:4afa295cfe7c5ffd596efee69594157fea87202e33d66bb4a98a52db4598f836` (`linux/arm64`, image ID `sha256:66669e2ac26f74e1e8dd2d1701c1f035bb19a4f56f2567340ebbf2187d8fbfe9`) | OpenJDK 17.0.13, embedded Derby | REST `POST /api/channels/{id}/messages/_export` on the running 4.5.2 server |
| Open Integration Engine 4.6.0 | Official `oie_unix_4_6_0.tar.gz`, SHA-256 `7c82e79027e671277e1d78d0f7bbb1c53ddf1be476f1e80c2ddbfcf7855900ea`, release source `cd1110e304aa2fbd0bc3de966af8a920d9fc6150` | `eclipse-temurin@sha256:e85989f3e4d136b3d7dde921e157fddb9c7016805a225c1ec483326b825b3ca5` (`linux/arm64`, image ID `sha256:01642244739f542c8e61da3bd8247029f77446b41a36738d1c76d05181b795ce`), OpenJDK 17.0.20, embedded Derby | Same server-side REST export on running OIE 4.6.0 |

The OIE tarball hash matched its release `sha256sums` before the container was
started. Its published 4.6.0 release requires Java 17 or newer; the pinned JRE
satisfies that requirement. Both servers ran with a host binding only on
`127.0.0.1` (Mirth `18443`, OIE `28443`), on the temporary `readmit35-lab`
bridge. A generated administrator password replaced the fresh-install value
and was verified before any synthetic message was sent. No credential value was
retained. Containers, network and temporary secret files were removed after
the exports were copied and hashed.

## Fixture selection and provenance

`lab/issue-35-generate.py` (seed 35; SHA-256
`7fe94e418e7eb4db1a2b6c34347942f6b14199424b87275e58f3f943246c937c`)
wrote the input files in `lab/issue-35-inputs/`. The two ADT inputs are exact
duplicates. The SIU input carries an explicit `-0600` time, the ORU input has
UTF-8 non-ASCII text, and the last two inputs are deliberately partial and
malformed. Input hashes are in `SHA256SUMS`. The tests compare source RAW
payload digests with those independently generated inputs, not with a value
reported by the engine or the adapter.

`lab/issue-35-channels.py` created source-only, two-destination VM routing, and
attachment-enabled source channels from the structure of two public OIE
examples at `OpenIntegrationEngine/oie-examples` commit
`977974da67590bf0cefe54a12b976a81a52e5dd1`. The pinned example files are
`Channels/Get Num Pages From Embedded PDF/Example - Get Num Pages From Embedded PDF.xml`
(SHA-256 `54c2f1858b37570436de2862b39c0911c205d98ec4189e662afda02c0c0074eb`)
and `Channels/Validate XSD Source Filter/Example - Validate XSD.xml`
(SHA-256 `90e7145e438103e683fedd1978e110b9db42b06dc7060b6a14478e7a197c5712`).
The lab generator removes their scripts, inbound/outbound payload templates and
example-specific metadata;
the resulting channel configurations and hashes are committed under
`lab/issue-35-channels/`. These configurations are setup, not a message export,
and the upstream example's redistribution terms remain part of owner review.
The servers' `GET /api/channels/{id}` responses were separately hashed in the
lab record; they were not used as message evidence.

Each `source/1.xml` through `source/4.xml` is one whole-message export from a
source-only channel: ADT, duplicate ADT, SIU, and non-ASCII ORU. `negative/7.xml`
and `negative/8.xml` are whole-message exports whose source RAW bytes are the
partial and malformed inputs. Their XML container is valid; the HL7 payload
remains quarantined by Readmit. `multi/` contains Mirth message IDs 3–4 and OIE
IDs 1–2 from a channel with source connector 0 and destination connectors 1–2.
`encrypted/1.xml` has `encrypted=true` content. `attachment/1.xml` has a
nonempty synthetic attachment produced through `messagesWithObj`; the synthetic
request bytes are in `lab/issue-35-attachment-request.xml`. `raw/1.xml` is the
engine's RAW-only export, including exporter-added separators; its stage is
unknown under raw fallback. Route-target exports were observed separately but
not chosen as source fixtures: their raw payload is a VM routing representation,
not the original input.

Whole-message exports were requested with `contentType` absent,
`destinationContent=false`, `encrypt=false`, `includeAttachments=false`,
`pageSize=100` and `filePattern=${message.messageId}.xml`. Mirth's selected
two-destination messages used `minMessageId=3`; OIE's used `minMessageId=1`.
The negative encrypted export set `encrypt=true`; the attachment export set
`includeAttachments=true`. A request for absent source `TRANSFORMED` content
produced no file in either engine. Before correcting the filename template, a
request with `filePattern=${messageId}.xml` produced one **literal filename**
per export directory containing several appended messages. No message was
overwritten; the corrected template created one file per engine message ID.
Mirth's selected destination IDs 3–4 reflect two earlier source-only probe
sends on that channel; a fresh run with only the qualified sends uses IDs 1–2
and `minMessageId=1`. Both filename options and the observed counts are
retained in the local lab record.

## Qualification boundary

| Engine/version | Export option and exact fixture cell | Readmit result |
| --- | --- | --- |
| Mirth 4.5.2 | Source-only whole-message XML, `source/1.xml`–`4.xml` | Source connector 0 RAW bytes imported; duplicate, two time forms and non-ASCII retained |
| Mirth 4.5.2 | Source-only XML with partial/malformed RAW, `negative/7.xml`–`8.xml` | Container imported; source bytes quarantined as unparsed |
| Mirth 4.5.2 | RAW-only export, `raw/1.xml` | Entire file including exporter separators retained; stage unknown |
| Mirth 4.5.2 | Two destinations, `multi/3.xml`–`4.xml` | Structured import refused; IDs 1–2 and sent/response never promoted |
| Mirth 4.5.2 | Encrypted `encrypted/1.xml`; nonempty `attachment/1.xml` | Both refused with no partial case |
| OIE 4.6.0 | Source-only whole-message XML, `source/1.xml`–`4.xml` | Same finite source RAW selection and byte identity |
| OIE 4.6.0 | Partial/malformed `negative/7.xml`–`8.xml`; RAW-only `raw/1.xml` | Quarantine and unknown-stage fallback as above |
| OIE 4.6.0 | Two destinations `multi/1.xml`–`2.xml`; `encrypted/1.xml`; `attachment/1.xml` | Each structured variant refused without a partial case |
| Both named releases | Source-only `contentType=TRANSFORMED` | No file was emitted because that stage was absent; no content or stage inferred |
| Both named releases | GET channel configuration, without message export | Refused as message XML; configuration digests retained separately from message evidence |
| Both named releases | CDATA in exported source RAW | Not observed from the engines' default message exporter; parser-level CDATA tests remain separate |

The `message-xml` adapter selects only the explicit, unencrypted, `HL7V2` RAW
stage of source connector key and metadata ID `0`. It tolerates the tested
exporters' separate, unselected `processedRaw` and `encoded` records and map
class labels without interpreting their content. A destination entry, changed
source stage, encryption, nonempty attachment, unknown class/reference, or
other untested XML form refuses the entire structured file. Direction,
observed time, engine correlation and delivery remain unknown. `raw` fallback
retains the entire exporter file and makes no content-stage claim. The original
XML container is retained inside the v5 case, so every extracted source byte
and offset remains checkable against it.

This matrix establishes only the named versions and options. It is not a
certification of either engine, proof of a file's origin, or qualification of
destination, encrypted or attachment stages. The public CLI tests use these
exact exports; the existing independent verification corpus remains a recorded
gap until the owner approves the fixture rights and scope for promotion.

## Reproduction

1. Obtain the exact official OIE tarball and verify its published SHA-256;
   obtain the two pinned OIE example channel XMLs at the commit and hashes
   above, placing them beside `lab/issue-35-channels.py` under the names
   `issue-35-oie-example-pdf.xml` and `issue-35-oie-example.xml`.
2. Run `python3 lab/issue-35-generate.py` and
   `python3 lab/issue-35-channels.py`. Their outputs are compared against
   `SHA256SUMS`; no real message or credential is an input.
3. Run Mirth by the image digest above, and OIE's verified tarball under the
   Java 17 image digest above (the disposable launch script is
   `lab/issue-35-oie-entrypoint.sh`). Publish only the container's HTTPS port
   to `127.0.0.1` on the host. Keep their appdata and export folders disposable.
   Supply the fresh-install password to `READMIT35_INITIAL_ADMIN_PASSWORD`
   without logging it. Generate a new administrator credential into a private
   temporary file with `lab/issue-35-api.py`'s `rotate` phase immediately;
   do not record either value in the fixture tree.
4. Import/deploy the synthetic channels, send only the generated input files,
   then invoke each engine's own server-side message export with the options
   above. For destination evidence, pass `destinationMetaDataId=1` and `2`
   explicitly when processing the two multi-destination messages. Use the
   attachment-enabled channel and the saved synthetic XML request for the
   attachment variant. Keep the resulting export bytes and SHA-256; inspect
   the source RAW payload against the input hashes without treating other
   stages as accepted evidence.
5. Stop and remove every lab container and the lab network; delete temporary
   credential files. The retained exports are inputs to `readmit import
   engine --plan ... --file ... --preview` and then the matching import with
   `--output NEW_CASE` under an activated test policy. `readmit timeline` and
   the v5 case reader independently reopen the case and check the original
   container bytes.

The original lab ran on Readmit base revision
`aae0c3354354cd465628643f2c352da5202aa078`. Runtime-generated server IDs,
receive timestamps and XML serialization metadata mean a new lab does not
produce the same export SHA-256; matching source payloads, structure, status,
counts and refusal cells is the reproducible test. The exact bytes of this run
remain in the fixture tree and `SHA256SUMS`.
