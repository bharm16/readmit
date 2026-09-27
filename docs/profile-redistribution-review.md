# Pending redistribution review for normalized v2 metadata

Status: **pending owner review**. This packet grants no distribution authority.

The exact artifacts are produced by `readmit-profile-extractor/v2` from the adopted D1 pins. [The receipt](profile-extraction-v2-receipt.json) binds source archives, all seven output packs, and the exact license files. Derived packs and notice copies remain outside source control and release archives.

| Source | Commit | Archive SHA-256 |
| --- | --- | --- |
| hl7apy | `9550b6eca2c580e9615d756b294dbe5ea471667c` | `8b496e4e94221b8472a9df81be73d98b8726c1d6b386637a6f08264fc83ddb63` |
| nhapi | `2495edd1e23a85ab9146cb03947c17d45120cf1f` | `165a28565b88ba1a9a26f9be893639490af074a529774f8a8da77f0905330882` |

## Material copied and adaptations

- Message/group order and segment cardinalities, field positions, primitive/composite type references, repetition and available length metadata.
- Composite component positions and type references; HL7apy component usage/withdrawn positions; code-table identifiers only.
- Source comments, prose definitions and code-table values are excluded. No upstream program is executed or included in the Go runtime.
- nHapi does not supply extracted component usage: `usage_known: false` retains that limitation as an unsupported result.
- nHapi `TSComponentOne` is normalized to the evaluator’s DTM lexical rule. Source recursive component placeholders are preserved and become unsupported if a value exceeds the two wire levels.
- HL7apy supplies no extracted maximum length: zero means unavailable, not an unbounded standard constraint.

## Required owner decision

Review the exact content covered by this receipt against the upstream licenses and any applicable HL7 incorporation terms. Determine whether the selected structural/component metadata can be redistributed in Readmit, what attribution/source-availability obligations apply to the transformed JSON, and whether separate HL7 permission or another restriction applies. The existing finite nHapi field-label review does not cover these definitions. No external terminology values are proposed for distribution; adding those requires another exact-content review.

Record approved or refused status, reviewer, date, scope, obligations and exact receipt/artifact hashes in an owner-controlled decision. A general instruction to implement or close the issue is not that rights decision. Pending/withheld packs must stay unshipped and cannot be advertised as available.

## Reproduce from a source checkout without publication

```sh
python3 tools/profile_extract.py --nhapi NHAPI_ARCHIVE --hl7apy HL7APY_ARCHIVE --output NEW_REVIEW_DIRECTORY
READMIT_PROFILE_EXTRACTION=NEW_REVIEW_DIRECTORY go test -short -p 2 -tags readmit_nosync ./internal/profileeval -run TestPinnedExtractionReadback
```

The notice files are emitted as `licenses/nhapi.txt` and `licenses/hl7apy.txt`; compare their exact hashes to the receipt. Source review links: [nHapi license](https://github.com/nHapiNET/nHapi/blob/2495edd1e23a85ab9146cb03947c17d45120cf1f/LICENSE) and [HL7apy license](https://github.com/crs4/hl7apy/blob/9550b6eca2c580e9615d756b294dbe5ea471667c/LICENSE).
