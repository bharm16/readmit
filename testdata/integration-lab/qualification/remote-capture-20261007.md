# Saved remote capture: actual OIE, native Desktop and production runner

On October 7, 2026, the native Readmit Desktop and the production
`customerrunner.RunConnectedSuite` path executed saved received-HL7 tests against
the same independently versioned OIE channel in each mode. The actual target was
Open Integration Engine 4.6.0 on Java 17.0.17, inside the pinned Linux/aarch64
OIE/HAPI/PostgreSQL lab. The host was macOS 27.0.1 arm64.

## Executed topology and results

Readmit sent the reviewed SIU S13 stimulus to the lab's explicitly approved
plain loopback input. OIE mapped MSH-3 to `OIE-LAB` and returned the framed output
through its recorded `fixture:7001` → Docker-exec byte relay →
`192.168.68.75:49355` path. That saved, approved non-loopback listener used
**mutual TLS**, with a purpose-scoped server-key reference and the declared
client CA. TLS remained end to end between OIE and Readmit; the relay did not
terminate TLS, rewrite bytes, or create a public listener. Actual acquisitions
negotiated TLS 1.3 and retained both peer certificate identities.

| Revision | Native | Runner | Intended mapped-start check | Post-stimulus coverage, native / runner |
| --- | --- | --- | --- | --- |
| positive, `lab-fab6d58509d6bc17ff39c0ae` | complete / pass | complete / pass | `20300102100000+0000` | 5133 / 5119 ms |
| defective, `lab-eeb46a6f76a54bc253c7569a` | complete / fail | complete / fail | actual wrong field `20300102094500+0000` | 5125 / 5084 ms |
| corrected, `lab-5c6ca3d703937375cb0de8c4` | complete / pass | complete / pass | `20300102100000+0000` | 5160 / 5125 ms |

Both defective runs failed only the intended mapped-start assertion. The
configured full horizon was 5000 ms, with a finite 400-sample allowance covering
the send budget plus that horizon. Each native/runner pair used the same
selected channel revision. Every captured inbound frame and outbound AA ACK
matched the actual external OIE sender's independently acquired full bytes.
All six ACKs were complete and correlated to the original MSH-10 identity.

The saved authored-check SHA-256 was unchanged across modes:
`d34c8bbc0b8a6cd4fdec023b9add32cb81994baad2c6f3198c230b207e05a673`.
The passive verifier separately compared the actual retained typed and wire
assertion bytes, yielding
`4fd52ff3412488e64d0d940ca974ffa2941df00023c3109a8f9672bc5e794e07`.
The lab's `ZLG` marker and the product's fresh, one-use MSH-4 runtime marker
remain separate. MSH-10 supplies the explicit phase identity.

## Producers, native review and offline readers

The actual native application was `readmit-desktop version
0.0.0+issue699.7800e9e08dfb`, source commit
`7800e9e08dfb400747aa73deb4e4609e4fa0fe99`, executable SHA-256
`9ce31668daa911148ff2f498515434ae823523d19debf36405f23854e1e53bfc`.
Its build receipt and Go build information are retained. The exact application
was preserved outside Git, with a retained qualification Git reference. The
approved [dedicated #699 Figma page](https://www.figma.com/design/xI2G6uB2BQil3v7UWOhTz1/Readmit?node-id=2484-109)
was checked against the native review and all three result states. Screenshots
and accessibility observations remain inline in the implementation conversation;
no screenshot files are claimed as members of this archive. The observed native
result durations were 14.0 seconds, 13.0 seconds and 13.0 seconds.

Runner qualification used the actual production customer-runner API, mTLS hub,
durable dispatch and disposable PostgreSQL, invoked from a Go race-test binary.
It is not an independently packaged runner-binary qualification. Retained runner
flows accurately report engine `dev`. The defective/corrected producer's SHA-256
was `6031ce1e536585a74bcb9012c66380df834ec41d824e2cdb25a44662f2df4184`;
the positive temporary test binary's digest was **not retained**. That limitation
is explicit in `qualification/runner-producer-remaining.json` and is not filled
with an invented build identity.

The original native/runner execution code remained unchanged through the cycle.
Offline reanalysis then exposed a real reader-family dispatch bug for the new
retained lifecycle. The corrected passive verifier ran after both the capture
lab/relay and PostgreSQL were down, without resending or contacting sources.
Its canonical receipt is
`session/qualification/verified-all-1270783903/receipt.json`; exact assertion
bytes are adjacent. `offline-reader-source.json` and `offline-reader-fix.patch`
retain the later reader changes separately from the native producer:

- `internal/connectedrun/flow_analysis.go` SHA-256:
  `73b6615c6933fd50442710ec027de96ee0037188a6dd203270d222e8d0243f45`.
- `internal/connectedrun/runtime_flow_read.go` SHA-256:
  `4da69570380712d1d70b86b7b007263502f4fcb113065499a9471a61ea64976a`.

All six retained flows passed the fresh offline checks for captures, packet/report
readback, reanalysis, linked members, original-byte agreement, complete interval
coverage and unchanged actual assertions.

## Failures preserved separately

The first actual native attempt, live4, remains an incomplete/uncertain run.
Its fixture had a 100-sample ceiling; the capture reached that safety limit and
cancelled the connection at `07:33:55.013Z`. OIE retained a peer-EOF TLS handshake
failure at `07:33:55.038Z`, before any HL7 output write. There was no acquired
ACK and no basis to claim absence. The original product artifact, OIE receipt,
configuration and diagnostic log remain under `qualification/failures/live4`.
Only the fixture's declared sample budget was corrected to 400 before the fresh
cycle. TLS verification and the production transport were not weakened.

The positive cycle's original `qualification.json` still says
`test_failed: true`: its wrapper addressed a retained runner job with the wrong
ID after the real native and runner executions had passed. A subsequent passive
run then exposed the real reanalysis reader bug described above. Both original
failures and the later successful checks are retained separately; neither failed
receipt was rewritten as a passing one.

## Reproduce and reopen

The [credential-free acquisition archive](remote-capture-20261007.tar.gz) is
**2,254,324 bytes**, SHA-256
`699889fda21a51fd2939fe47b42adbfb44e4c286db959351ead13bbfb71c2b72`.
The [archive verification receipt](remote-capture-20261007-verification.json)
and [extracted-artifact report log](remote-capture-20261007-readback.log) record
safe extraction, no symlinks, only archive-relative hardlinks to prior regular
members, and successful authoritative six-flow report/capture/reanalysis checks
on the extracted hardlinked files. Original member hashes remained unchanged
after that check. Bundled public certificates make this check independent of
the original private lab directory; no private key is included.

The live qualification and passive report verification are distinct tests:

```sh
# Live: provision the owned lab, PostgreSQL and native harness as documented by
# hub/runner_connected_capture_test.go; provide READMIT_CAPTURE_LAB_SESSION,
# READMIT_CAPTURE_EVIDENCE_DIR, READMIT_HUB_TEST_SOCKET and its native handoff.
cd hub
go test -race -tags readmit_nosync -run '^TestSavedConnectedCaptureActualOIERunnerAndNativeDefectCycle$' -count=1 -v .

# Passive: after preparing the archive-only verifier state below:
READMIT_CAPTURE_EVIDENCE_DIR="$LAB_EVIDENCE_ROOT/session/qualification" \
READMIT_CAPTURE_LAB_SESSION="$LAB_VERIFY_ROOT" \
go test -race -tags readmit_nosync -run '^TestSavedConnectedCaptureRetainedReports$' -count=1 -v .
```

For an archive-only readback, extract the package into a new directory named by
`LAB_EVIDENCE_ROOT`, and choose a different new directory for `LAB_VERIFY_ROOT`.
The following tested harness setup copies only independent receipts and names
the bundled public certificates; it does not create an execution configuration,
resolve credentials, bind a listener or contact a server:

```sh
python3 - "$LAB_EVIDENCE_ROOT" "$LAB_VERIFY_ROOT" <<'PY'
from pathlib import Path
import json, shutil, sys
root, state = (Path(value).resolve() for value in sys.argv[1:])
state.mkdir(mode=0o700)
(state / "session-evidence").mkdir()
for mode in ("positive", "defective", "corrected"):
    source = root / "session/qualification" / mode
    generation = json.loads((source / "qualification.json").read_text())["generation"]
    target = state / "session-evidence" / generation
    target.mkdir()
    shutil.copytree(source / "return-receipts", target / "return-receipts")
certificates = root / "session/qualification/public-certificates"
(state / "connection.json").write_text(json.dumps({"return_listener": {
    "certificate": str(certificates / "return-server.pem"),
    "client_certificate": str(certificates / "return-client.pem")}}))
PY
```

The archive contains the complete credential-filtered `session/` export and a
fresh `reference/` export. Identical bytes use ordinary internal tar hardlinks;
every original logical path remains covered by its byte manifest. The session
retains exact channel/runtime/configuration identities, original stimulus
provenance, independent transmitted/ACK/TLS receipts, six native/runner flows,
reports, failures, producer limitations, scoped reset and verified teardown.
The fresh original reference qualifier passed all 24 revisions at all three
boundaries, including its security and delayed-defect checks, then verified
removal of only its own containers, network and volumes. The existing #698
archive and independent acquisition-integrity tests also remain passing.

Both exports were scanned for generated credentials, private keys and tokens.
This is a finite local engine-output qualification using wholly synthetic
fixtures. It does not close the broader #593 matrix, customer-system acceptance,
other operating systems, or independent packaged runner qualification.
