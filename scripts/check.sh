#!/usr/bin/env bash
# Pre-merge quality gate, the anam repo's with one addition: the tests run against a
# real anam daemon, so ANAM_BIN must name an anam binary. Unset, it is built from the
# sibling checkout at ../anam (or $ANAM_SRC) into a scratch directory.
set -uo pipefail

fail=0
run() {
  echo "→ $*"
  if ! "$@"; then
    echo "  FAILED: $*" >&2
    fail=1
  fi
}

offenders="$(gofmt -l . 2>/dev/null)"
if [ -n "$offenders" ]; then echo "gofmt would change:"; echo "$offenders"; fail=1; fi
if command -v goimports >/dev/null 2>&1; then
  offenders="$(goimports -l . 2>/dev/null)"
  if [ -n "$offenders" ]; then echo "goimports would change:"; echo "$offenders"; fail=1; fi
else
  echo "goimports not installed — skipping (install: golang.org/x/tools/cmd/goimports)"
fi

# The adapter is a client. It may import nothing of Anam's and open no store.
echo "→ client boundary (no anam internals, no database)"
imports="$(go list -f '{{.ImportPath}}{{range .Imports}} {{.}}{{end}}{{range .TestImports}} {{.}}{{end}}{{range .XTestImports}} {{.}}{{end}}' ./... 2>/dev/null)"
boundary="$(echo "$imports" | grep -E 'github.com/mattjoyce/anam/|database/sql|sqlite' || true)"
if [ -n "$boundary" ]; then
  echo "  FAILED: the adapter reaches past the API:" >&2
  echo "$boundary" | awk '{print "    " $1}' >&2
  fail=1
fi

run go vet ./...
if command -v golangci-lint >/dev/null 2>&1; then run golangci-lint run ./...; else echo "golangci-lint not installed — skipping"; fi
if command -v gosec >/dev/null 2>&1; then run gosec -severity medium -quiet ./...; else echo "gosec not installed — skipping"; fi

# A real daemon for the tests. The developer's own ANAM_ settings must not steer a
# test toward a live daemon, so every ANAM_ variable but the binary is dropped.
bin="${ANAM_BIN:-}"
if [ -z "$bin" ]; then
  src="${ANAM_SRC:-../anam}"
  if [ -d "$src/cmd/anam" ]; then
    scratch="$(mktemp -d)"
    trap 'rm -rf "$scratch"' EXIT
    echo "→ building anam from $src"
    if (cd "$src" && go build -o "$scratch/anam" ./cmd/anam); then
      bin="$scratch/anam"
    else
      echo "  FAILED: build anam from $src" >&2
      fail=1
    fi
  else
    echo "ANAM_BIN unset and no anam checkout at $src: daemon tests will be skipped" >&2
  fi
fi
for v in $(compgen -e | grep '^ANAM_' || true); do unset "$v"; done
export ANAM_BIN="$bin"
run go test -race -count=1 -shuffle=on -vet=all ./...

if [ "$fail" -ne 0 ]; then echo "CHECK FAILED"; exit 1; fi
echo "CHECK OK"
