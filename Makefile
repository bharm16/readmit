.DEFAULT_GOAL := check
.PHONY: check test test-focused test-boundary test-corpus test-tools verify mutate fuzz

PKGS ?=
ARGS ?=

check:
	python3 tools/toolchain.py --check
	test -z "$$(gofmt -l cmd internal tests desktop)"
	go vet ./...

test-focused:
	@test -n "$(PKGS)" || { echo 'Set PKGS to the affected Go packages.' >&2; exit 2; }
	CGO_ENABLED=1 go test -race -short $(PKGS) $(ARGS)

# The small resource boundary and the small stream keep race coverage; their
# production-sized variants run once without instrumentation. No behavior is
# omitted from the full gate, and each variant asserts the same contract.
test:
	CGO_ENABLED=1 go test -race -short ./...
	$(MAKE) test-boundary
	$(MAKE) test-corpus

test-boundary:
	go test ./internal/receiver -run '^TestObservationByteLimitPreservesPriorLedgerAndFinalCase$$/production-limit$$'

test-corpus:
	go test ./internal/importer -run '^TestScanHoldsOneParsingBatchWhateverTheStreamLength$$/production-stream$$'

test-tools:
	PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tools -p 'test_*.py' -v

verify:
	@directory=$$(mktemp -d); trap 'rm -rf "$$directory"' EXIT; \
	CGO_ENABLED=0 go build -trimpath -o "$$directory/readmit" ./cmd/readmit && \
	python3 tools/verify.py --binary "$$directory/readmit"

mutate:
	CGO_ENABLED=0 python3 tools/mutate.py

fuzz:
	python3 tools/fuzz.py $(ARGS)
