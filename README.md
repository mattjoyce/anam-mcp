# anam-mcp

An MCP adapter for [Anam](https://github.com/mattjoyce/anam), a personal longitudinal record. It
lets a model read the record through a small set of tools that cite their evidence, without
touching the ledger.

It is a client of Anam's service API and nothing more: it calls the daemon over HTTP with a token,
holds no state worth recovering, and offers only tools whose operations the daemon implements. The
API contract is `docs/API.md` in the anam repo.

Status: read-only first cut. Four tools and one resource, over what the daemon serves today.

| Tool | What it does | What it publishes |
|---|---|---|
| `describe_record` | Whose record, ledger identity and watermark, time and zone, source freshness, every admitted type, every projection a read may go through, what the server can and cannot do | Nothing |
| `describe_type` | One type's Definition as published: JSON Schema with field descriptions, time semantics, identity | Nothing |
| `read_record` | The timeline (narrowed by `from`, `to`, `type`) or a snapshot at a fact-time; `as_of` or `watermark` pin the knowledge; `fields` (JSON pointers) answers a frame of only those columns | With a `projection`, an immutable artifact and its receipt; without one, nothing, and the answer says so |
| `verify_receipt` | Whether a receipt's artifact is intact and the ledger reproduces it | Nothing |

The resource `anam://receipts/{hash}` serves a published read by its receipt hash.

- [docs/DESIGN.md](docs/DESIGN.md): decisions, tools, evidence handling, runtime, scenarios.
- [docs/API-GAPS.md](docs/API-GAPS.md): what the adapter needs from Anam and does not have yet.

## Configuration

| Variable | Meaning |
|---|---|
| `ANAM_SERVER` | The daemon's base URL. Required |
| `ANAM_TOKEN_FILE` | A file holding the token; never logged. Omit only for a daemon on loopback that asks for none |
| `ANAM_ZONE` | IANA zone for reads, such as `Australia/Sydney`. Required: a bare date is a day in it |
| `ANAM_MCP_TIMEOUT` | Deadline for each call on the daemon. Default `30s` |

The first two are the variables the `anam` CLI reads, so one shell serves both.

## Running

```sh
make build
ANAM_SERVER=http://127.0.0.1:8787 ANAM_TOKEN_FILE=~/.config/anam/token ANAM_ZONE=Australia/Sydney ./anam-mcp
```

The server speaks MCP over stdio; stdout carries protocol traffic only and diagnostics go to
stderr. For Claude Code:

```sh
claude mcp add anam -e ANAM_SERVER=http://127.0.0.1:8787 -e ANAM_TOKEN_FILE=$HOME/.config/anam/token -e ANAM_ZONE=Australia/Sydney -- /path/to/anam-mcp
```

## Developing

```sh
./scripts/check.sh
```

The tests run a real `anam daemon` on a scratch ledger. `ANAM_BIN` names the binary; unset,
`scripts/check.sh` builds one from the sibling checkout at `../anam` (or `ANAM_SRC`). Without
either, the tests that need a daemon are skipped and say so.
