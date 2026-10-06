.DEFAULT_GOAL := check
.PHONY: check test test-focused test-boundary test-corpus test-fhir-lab test-tools verify mutate fuzz check-labels

PKGS ?=
ARGS ?=

check:
	python3 tools/toolchain.py --check
	test -z "$$(gofmt -l cmd internal tests desktop docs)"
	go vet ./...

# Race tests skip the device flush of real files (internal/artifactdir's
# readmit_nosync tag): it was about two thirds of the slowest packages' time,
# and a test cannot lose power. Writes, creates and renames are unchanged.
test-focused:
	@test -n "$(PKGS)" || { echo 'Set PKGS to the affected Go packages.' >&2; exit 2; }
	CGO_ENABLED=1 go test -race -short -tags readmit_nosync $(PKGS) $(ARGS)

# The small resource boundary and the small stream keep race coverage; their
# production-sized variants run once without instrumentation. The FHIR lab
# keeps one v2-to-FHIR defect cycle under race and runs its whole lifecycle
# set, and the retained proof read from those lifecycles, once without
# instrumentation. No behavior is omitted from the full gate,
# and each variant asserts the same contract.
# Start the long packages first; Go de-duplicates them from ./... so each
# still runs once, overlapping the short packages instead of trailing them.
# Full lifecycle runs produce gigabyte-scale file-access logs for Go's result
# cache. Re-hashing them can take minutes after the tests finish. Keep compiler
# caches and focused-test results, but always execute the full gate freshly.
# A target-specific export also reaches the three recursive boundary/lab gates
# without changing the command inventory the independent CI runner verifies.
test: export GOFLAGS := $(GOFLAGS) -count=1
test:
	CGO_ENABLED=1 go test -race -short -tags readmit_nosync ./internal/connectedrun ./tests ./internal/desktop ./...
	$(MAKE) test-boundary
	$(MAKE) test-corpus
	$(MAKE) test-fhir-lab

test-boundary:
	go test ./internal/receiver -run '^TestObservationByteLimitPreservesPriorLedgerAndFinalCase$$/production-limit$$'

test-corpus:
	go test ./internal/importer -run '^TestScanHoldsOneParsingBatchWhateverTheStreamLength$$/production-stream$$'

test-fhir-lab:
	go test -tags readmit_nosync ./internal/connectedrun ./internal/report -run '^(TestFHIRFlow|TestConnectedRetainedProof|TestConnectedExtractMaps|TestConnectedLab)'

# Pack-backed qualification of scenario case generation (docs/scenario-generation.md).
# It needs the withheld HL7 packs (READMIT_PROFILE_EXTRACTION) and a receipt path
# (RECEIPT), qualifies only a committed revision, fails when a pack is missing,
# never rewrites the retained matrix and runs uncached.
.PHONY: qualify-generation-packs
qualify-generation-packs:
	@test -n "$(READMIT_PROFILE_EXTRACTION)" || { echo "READMIT_PROFILE_EXTRACTION must name the pinned packs" >&2; exit 2; }
	@test -n "$(RECEIPT)" || { echo "RECEIPT must name the new qualification receipt" >&2; exit 2; }
	@test -z "$(READMIT_UPDATE_GENERATION_MATRIX)" || { echo "qualification never updates the retained matrix" >&2; exit 2; }
	@git diff --quiet HEAD -- || { echo "qualify a committed revision: the working tree differs from HEAD" >&2; exit 2; }
	READMIT_REQUIRE_PROFILE_EXTRACTION=1 READMIT_GENERATION_QUALIFICATION_RECEIPT="$(RECEIPT)" \
	READMIT_QUALIFIED_REVISION=$$(git rev-parse HEAD) READMIT_QUALIFIED_TREE=$$(git rev-parse 'HEAD^{tree}') \
	go test -count=1 -short -tags readmit_nosync ./internal/casegen -run '^TestPinnedGenerationMatrix$$' -v

test-tools:
	PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tools -p 'test_*.py' -v

# The #512 product-label coverage check, kept as history: #532 superseded its
# label decisions, so it is no longer part of make check (docs/labels/README.md).
check-labels:
	PYTHONDONTWRITEBYTECODE=1 python3 tools/label_coverage.py

verify:
	@directory=$$(mktemp -d); trap 'rm -rf "$$directory"' EXIT; \
	CGO_ENABLED=0 go build -trimpath -o "$$directory/readmit" ./cmd/readmit && \
	python3 tools/verify.py --binary "$$directory/readmit"

mutate:
	CGO_ENABLED=0 python3 tools/mutate.py

fuzz:
	python3 tools/fuzz.py $(ARGS)

# Opt-in local measurements: no race instrumentation for latency; interruption
# correctness runs separately with race. This is not a release-acceptance gate.
.PHONY: test-performance
test-performance:
	READMIT_PERFORMANCE=1 go test ./internal/desktop -run '^TestPerformanceEnvelope$$' -count=1 -v
	CGO_ENABLED=1 go test -race -short ./internal/suite ./internal/durablerun -run 'TestSuiteNetworkBlackholeRetainsUncertaintyAndRecovers|TestSuiteUsesActualQueueStateIsolation|TestSuiteCancellationPreservesUncertainDeliveryAndRefusesResume|TestSuiteProcessCrashRetainsUncertainJobWithoutStartingDependent|TestDiskFullDuring|TestTornTrailingRecord|TestCleanupRemovesOnlyAStaleLease' -count=1 -v
	CGO_ENABLED=1 go test -race -short ./internal/desktop ./internal/artifactdir ./tests -run 'TestAcknowledgedEditorDraftsSurviveAKill|TestReopeningAfterAKillStartsNoListener|TestAKilledImportLeavesNoRegistered|TestADiskRefusalDuringAnImport|TestCancellingAnImportWhileItWrites|TestCancellingAReplacingRebuild|TestCancellingACollectionPartWay|TestACollectorIsCancelledByTheCaptureName|TestACancellationDuringExecutionAdmission|TestControlledCrashRestoresUnstoredWork|TestWriteContextStopsBetweenFiles|TestInterruptingAnImportWhileItWrites' -count=1 -v

# Build and refresh the local Mac app; ordinary launches never run this target.
.PHONY: install-desktop
install-desktop:
	python3 tools/install_desktop.py $(ARGS)
