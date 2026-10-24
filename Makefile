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
# Keep test directories trusted and execute the root runner suite from a
# traversable test binary path so target-identity helpers can re-exec it.
linux-acceptance:
	@umask 022; test "$$(go env GOOS)" = linux || { echo "linux-acceptance must run in a Linux guest" >&2; exit 1; }
	go generate ./internal/sensor/linux ./internal/enforce
	go vet ./...
	go build ./...
	umask 022; go test -count=1 ./...
	umask 022; mkdir -p "$(BUILD_DIR)"; go test -c -o "$(BUILD_DIR)/linux-acceptance-runner.test" ./internal/runner
	sudo env GOMODCACHE="$$(go env GOMODCACHE)" GOCACHE=/tmp/agenttaint-linux-acceptance-root-cache AGENTTAINT_REQUIRE_ENFORCE_EXIT_DELETE=1 sh -lc 'umask 022; exec "./$(BUILD_DIR)/linux-acceptance-runner.test" -test.count=1'
	sudo env GOMODCACHE="$$(go env GOMODCACHE)" GOCACHE=/tmp/agenttaint-linux-acceptance-root-cache AGENTTAINT_REQUIRE_ENFORCE_EXIT_DELETE=1 sh -lc 'packages=$$(go list ./... | sed "/^github.com\/bocopile\/AgentTaint\/internal\/runner$$/d"); exec go test -count=1 $$packages'
	sudo env GOMODCACHE="$$(go env GOMODCACHE)" GOCACHE=/tmp/agenttaint-linux-acceptance-root-cache AGENTTAINT_REQUIRE_ENFORCE_EXIT_DELETE=1 go test -count=1 -run '^TestEnforceProcessExitMapDeleteFailureFailsClosed$$' ./internal/enforce
	@if [ "$$(go env GOARCH)" = arm64 ]; then \
		sudo env GOMODCACHE="$$(go env GOMODCACHE)" GOCACHE=/tmp/agenttaint-linux-acceptance-root-cache AGENTTAINT_REQUIRE_S25_BPF=1 sh -lc 'umask 022; exec "./$(BUILD_DIR)/linux-acceptance-runner.test" -test.count=1 -test.run="^(TestRunObservedMmapPathReadFault|TestRunObservedSeparatesConcurrentRuns)$$"'; \
	fi
