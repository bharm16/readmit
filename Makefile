.DEFAULT_GOAL := check
.PHONY: check test test-focused test-boundary test-tools verify mutate fuzz

PKGS ?=
ARGS ?=

check:
	python3 tools/toolchain.py --check
	test -z "$$(gofmt -l cmd internal tests desktop)"
	go vet ./...

test-focused:
	@test -n "$(PKGS)" || { echo 'Set PKGS to the affected Go packages.' >&2; exit 2; }
	CGO_ENABLED=1 go test -race -short $(PKGS) $(ARGS)

# The small resource boundary keeps race coverage; its production-sized variant
# runs once without instrumentation. No behavior is omitted from the full gate.
test:
	CGO_ENABLED=1 go test -race -short ./...
	$(MAKE) test-boundary

test-boundary:
	go test ./internal/receiver -run '^TestObservationByteLimitPreservesPriorLedgerAndFinalCase$$/production-limit$$'

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
