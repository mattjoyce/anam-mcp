# anam-mcp — build with version injected via ldflags, as the anam repo does.
# `-a` forces a fresh compile so ldflags values are never stale from the build cache.

BINARY     := anam-mcp
PKG        := ./cmd/anam-mcp
VERSION    := $(shell ./scripts/version.sh)
COMMIT     := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS    := -X main.version=$(VERSION) -X main.gitCommit=$(COMMIT) -X main.buildDate=$(BUILD_DATE)

.PHONY: build install test check fmt tidy clean

build:
	go build -a -ldflags "$(LDFLAGS)" -o $(BINARY) $(PKG)

install:
	go install -a -ldflags "$(LDFLAGS)" $(PKG)

# ANAM_BIN names the anam binary the tests run a daemon from; see scripts/check.sh.
test:
	go test -race -count=1 -shuffle=on -vet=all ./...

check:
	./scripts/check.sh

fmt:
	gofmt -w .
	goimports -w .

tidy:
	go mod tidy

clean:
	rm -f $(BINARY)
	go clean
