# anam-mcp

An MCP adapter for [Anam](https://github.com/mattjoyce/anam), a personal longitudinal record. It
lets a model read the record through a small set of tools that cite their evidence, without
touching the ledger.

It is a client of Anam's service API and nothing more: it calls the daemon over HTTP with a token,
holds no state worth recovering, and offers only tools whose operations the daemon implements. The
API contract is `docs/API.md` in the anam repo.

Status: design only. No code yet.

- [docs/DESIGN.md](docs/DESIGN.md): decisions, tools, evidence handling, runtime, scenarios.
- [docs/API-GAPS.md](docs/API-GAPS.md): what the adapter needs from Anam and does not have yet.

## Configuration

| Variable | Meaning |
|---|---|
| `ANAM_SERVER` | The daemon's base URL |
| `ANAM_TOKEN_FILE` | A file holding the token; never logged |
| `ANAM_ZONE` | IANA zone for reads, such as `Australia/Sydney` |
