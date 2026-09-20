#!/bin/sh
# Synthetic evaluation walkthrough: inspect, generate, capture, index, diagnose,
# diff, preview a regression test, then run and verify the sealed packet.
#
# Everything it reads is synthetic and everything it writes goes into one new
# workspace directory. It opens no connection except the loopback fixtures
# `readmit report` starts itself. See README.md beside this file.
#
#   READMIT   the readmit executable            (default: ./readmit)
#   FIXTURES  the testdata/fixtures directory   (default: ./testdata/fixtures)
#   $1        the new workspace directory       (default: readmit-walkthrough)
#
# tests/walkthrough_test.go runs this exact file against the built executable.
set -eu

READMIT=${READMIT:-./readmit}
FIXTURES=${FIXTURES:-./testdata/fixtures}
WORKSPACE=${1:-readmit-walkthrough}

case "$READMIT" in
  */*) READMIT=$(cd "$(dirname "$READMIT")" && pwd)/$(basename "$READMIT") ;;
esac
FIXTURES=$(cd "$FIXTURES" && pwd)

mkdir "$WORKSPACE"
cd "$WORKSPACE"

echo "== 1. inspect: syntax of the frozen synthetic regression pair, values hidden"
"$READMIT" inspect "$FIXTURES/synth-v1-regression.mllp" --format mllp --terminator cr

echo "== 2. synth: the reproducible seed-zero SIU family"
"$READMIT" sample synth --output family

echo "== 3. timeline: verify and reopen the generated regression case"
"$READMIT" timeline family/regression

echo "== 4. capture: import the receiver fixtures as a case with imported provenance"
"$READMIT" sample capture --fixtures "$FIXTURES" --output test-case

echo "== 5. index: build a disposable states-only index and ask it one question"
"$READMIT" sample index family/regression --output regression.index.json
"$READMIT" index search family/regression regression.index.json --field SCH-2 --state present

echo "== 6. diagnose: the regression case, then the known-invalid case"
"$READMIT" diagnose family/regression --output regression-diagnosis
"$READMIT" diagnose family/invalid --output invalid-diagnosis

echo "== 7. diff: the one field the invalid case changes"
"$READMIT" diff family/regression family/invalid --key MSH-10 \
  --format markdown --output regression-vs-invalid.md

echo "== 8. test: validate the regression spec locally; no connection, no verdict"
cp "$FIXTURES/test-reschedule.json" "$FIXTURES/test-target.json" .
"$READMIT" test test-reschedule.json

echo "== 9. report: run the spec against fresh loopback fixtures and seal the packet"
"$READMIT" report --scenario siu-reschedule-v1 --output packet

echo "== 10. report verify: read the sealed packet back"
"$READMIT" report verify packet

echo "Walkthrough complete: $WORKSPACE"
