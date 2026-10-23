.PHONY: build fmt vet test race check linux-acceptance install uninstall

PREFIX ?= $(HOME)/.local
BINDIR ?= $(PREFIX)/bin
DESTDIR ?=
BUILD_DIR ?= .build

build:
	go build ./...

install:
	mkdir -p "$(BUILD_DIR)"
	go build -o "$(BUILD_DIR)/agenttaint" ./cmd/agenttaint
	install -d "$(DESTDIR)$(BINDIR)"
	install -m 0755 "$(BUILD_DIR)/agenttaint" "$(DESTDIR)$(BINDIR)/agenttaint"

uninstall:
	rm -f "$(DESTDIR)$(BINDIR)/agenttaint"

fmt:
	gofmt -w cmd internal

vet:
	go vet ./...

test:
	go test ./...

race:
	go test -race ./...

check:
	test -z "$$(gofmt -l cmd internal)"
	go vet ./...
	go build ./...
	go test ./...

# Run from the supported Linux guest after syncing the current source tree.
# Regenerate BPF artifacts before running the full suite, then run required
# Linux acceptance fixtures separately so helper subprocess tests remain usable.
linux-acceptance:
	@test "$$(go env GOOS)" = linux || { echo "linux-acceptance must run in a Linux guest" >&2; exit 1; }
	go generate ./internal/sensor/linux ./internal/enforce
	go vet ./...
	go build ./...
	go test -count=1 ./...
	sudo env GOMODCACHE="$$(go env GOMODCACHE)" GOCACHE=/tmp/agenttaint-linux-acceptance-root-cache AGENTTAINT_REQUIRE_ENFORCE_EXIT_DELETE=1 go test -count=1 ./...
	sudo env GOMODCACHE="$$(go env GOMODCACHE)" GOCACHE=/tmp/agenttaint-linux-acceptance-root-cache AGENTTAINT_REQUIRE_ENFORCE_EXIT_DELETE=1 go test -count=1 -run '^TestEnforceProcessExitMapDeleteFailureFailsClosed$$' ./internal/enforce
	@if [ "$$(go env GOARCH)" = arm64 ]; then \
		sudo env GOMODCACHE="$$(go env GOMODCACHE)" GOCACHE=/tmp/agenttaint-linux-acceptance-root-cache AGENTTAINT_REQUIRE_S25_BPF=1 go test -count=1 -run '^(TestRunObservedMmapPathReadFault|TestRunObservedSeparatesConcurrentRuns)$$' ./internal/runner; \
	fi
