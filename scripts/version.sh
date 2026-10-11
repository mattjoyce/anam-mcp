#!/usr/bin/env bash
# Emit a version string like v0.<commit-count>-<short-hash>, or "dev" outside a git repo.
# Kept deliberately simple; the Makefile injects the result via ldflags.
set -euo pipefail

if ! git rev-parse --git-dir >/dev/null 2>&1; then
  echo "dev"
  exit 0
fi

count="$(git rev-list --count HEAD 2>/dev/null || echo 0)"
hash="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
echo "v0.${count}-${hash}"
