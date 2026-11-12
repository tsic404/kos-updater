# Local entry points for the kos-updater extension package. CI runs `lint`,
# `test` and `acceptance`, one step per target; nothing here pushes or publishes.
SHELL := /bin/sh
GO ?= go

.PHONY: all lint fmt-check vet test acceptance pack clean

all: lint test

# fmt-check fails on any file gofmt would rewrite, so a stale format cannot hide
# behind a passing test run.
fmt-check:
	@unformatted=$$(gofmt -l $$(git ls-files '*.go')); \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt required for:"; echo "$$unformatted"; exit 1; \
	fi

vet:
	$(GO) vet ./...

lint: fmt-check vet

test:
	$(GO) test ./...

acceptance:
	$(GO) test ./tests/... -count=1 -v

pack:
	$(GO) run ./cmd/kos-updater-pack -out dist/kos-updater.tar

clean:
	$(GO) clean -testcache
	rm -rf dist
