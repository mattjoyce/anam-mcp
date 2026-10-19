# anam-mcp

An MCP adapter for [Anam](https://github.com/mattjoyce/anam), a personal longitudinal record. It
lets a model read the record through a small set of tools that cite their evidence, and, when
configured to, save the person's own words as a note.

It is a client of Anam's service API and nothing more: it calls the daemon over HTTP with a token,
holds no state worth recovering, and offers only tools whose operations the daemon implements. The
API contract is `docs/API.md` in the anam repo.

Status: four reading tools and one resource, over what the daemon serves today, and two writing
tools, `save_note` and `save_fact`, offered only when `ANAM_MCP_NOTE_TOKEN_FILE` is set (anam
cards 157 and 177).

| Tool | What it does | What it publishes |
|---|---|---|
| `describe_record` | Whose record, ledger identity and watermark, time and zone, source freshness, every admitted type, every projection a read may go through, what the server can and cannot do | Nothing |
| `describe_type` | One type's Definition as published: JSON Schema with field descriptions, time semantics, identity, and the handling guidance every writer of the type agrees on | Nothing |
| `read_record` | The timeline (narrowed by `from`, `to`, `type`) or a snapshot at a fact-time; `as_of` or `watermark` pin the knowledge; `fields` (JSON pointers) answers a frame of only those columns | With a `projection`, an immutable artifact and its receipt; without one, nothing, and the answer says so |
| `verify_receipt` | Whether a receipt's artifact is intact and the ledger reproduces it | Nothing |
| `save_note` | The person's words, verbatim and at once, as a `note.annotation` under the conversation's own source, naming the facets they are about or none, marked as asserted by them; words said now take the present moment and words relayed from earlier take `at`, never in the future; the same words at the same minute are one note, and a correction (`id` and `at`) is a new version | A note in the record |
| `save_fact` | A fact the person asks to have recorded, as a facet of any type the daemon knows, under the conversation's own source and marked as asserted by them; the fields are the type's own, validated by the gate; the same fact again is a no-op and changed fields are a new version | A facet in the record |

The resource `anam://receipts/{hash}` serves a published read by its receipt hash.

- [docs/DESIGN.md](docs/DESIGN.md): decisions, tools, evidence handling, runtime, scenarios.
- [docs/API-GAPS.md](docs/API-GAPS.md): what the adapter needs from Anam and does not have yet.

## Configuration

| Variable | Meaning |
|---|---|
| `ANAM_SERVER` | The daemon's base URL. Required |
| `ANAM_TOKEN_FILE` | A file holding the token; never logged. Omit only for a daemon on loopback that asks for none |
| `ANAM_ZONE` | IANA zone for reads, such as `Australia/Sydney`. Required: a bare date is a day in it |
| `ANAM_MCP_TIMEOUT` | Deadline for each call on the daemon, sent to it so it answers in time. Default `30s`, so a stuck daemon fails the tool within half a minute rather than holding the conversation. A positive duration |
| `ANAM_MCP_NETWORK_MARGIN` | Time allowed beyond that deadline for the answer to travel, so a daemon that answers at its bound is still heard. Default `5s`, ample on a LAN; a daemon across a slow link may want more. Zero or a positive duration |
| `ANAM_MCP_MAX_RESPONSE_BYTES` | The most the adapter reads of one answer from the daemon. Default `67108864` (64 MiB), the anam CLI's own bound; the daemon's `max_read_bytes` refuses a read far smaller than this first, so this only stops a daemon that misbehaves. From 1024 to 1073741824 |
| `ANAM_MCP_NOTE_TOKEN_FILE` | A file holding the token of the importer the person's words and facts are admitted as, in the daemon's `importers_file` with source `conversation` and `any_type: true` (or types `[note.annotation]`, which offers only notes in effect). Set, the adapter offers `save_note` and `save_fact`; unset, it writes nothing. Never logged |

A value that fails its check stops the adapter at start, naming the variable. Every other value
in the code is fixed because changing it would break the MCP contract, the daemon's API, or what
a citation means; each says so where it is defined.

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

The tests run a real `anam daemon` on a scratch ledger, so they need an `anam` binary. Get one
from the public anam repo:

```sh
go install github.com/mattjoyce/anam/cmd/anam@latest
ANAM_BIN="$(go env GOPATH)/bin/anam" ./scripts/check.sh
```

`ANAM_BIN` names the binary; unset, `scripts/check.sh` builds one from a sibling checkout at
`../anam` (or `ANAM_SRC`). Without either, the tests that need a daemon are skipped and say so.
A secrets scan of the history runs as a tool, not a dependency, with the repo's own allowlist
for the README's token-file examples:

```sh
docker run --rm -v "$PWD:/repo:ro" zricethezav/gitleaks:latest git --config /repo/.gitleaks.toml /repo
```

## Licence

Apache-2.0; see `LICENSE` and `NOTICE`.
