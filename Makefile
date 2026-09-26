.DEFAULT_GOAL := check
.PHONY: check test test-focused test-boundary test-corpus test-tools verify mutate fuzz check-labels

PKGS ?=
ARGS ?=

check:
	python3 tools/toolchain.py --check
	test -z "$$(gofmt -l cmd internal tests desktop docs)"
	go vet ./...
	$(MAKE) check-labels

# Race tests skip the device flush of real files (internal/artifactdir's
# readmit_nosync tag): it was about two thirds of the slowest packages' time,
# and a test cannot lose power. Writes, creates and renames are unchanged.
test-focused:
	@test -n "$(PKGS)" || { echo 'Set PKGS to the affected Go packages.' >&2; exit 2; }
	CGO_ENABLED=1 go test -race -short -tags readmit_nosync $(PKGS) $(ARGS)

# The small resource boundary and the small stream keep race coverage; their
# production-sized variants run once without instrumentation. No behavior is
# omitted from the full gate, and each variant asserts the same contract.
test:
	CGO_ENABLED=1 go test -race -short -tags readmit_nosync ./...
	$(MAKE) test-boundary
	$(MAKE) test-corpus

test-boundary:
	go test ./internal/receiver -run '^TestObservationByteLimitPreservesPriorLedgerAndFinalCase$$/production-limit$$'

test-corpus:
	go test ./internal/importer -run '^TestScanHoldsOneParsingBatchWhateverTheStreamLength$$/production-stream$$'

test-tools:
	PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tools -p 'test_*.py' -v

# The product-label coverage gate: implemented label decisions must still hold
# in the presentation sources, and --strict is the issue's closure gate while
# any file is still pending per-label review (docs/labels/inventory.json).
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
