# anam-mcp

An MCP adapter for [Anam](https://github.com/mattjoyce/anam), a personal longitudinal record. It
lets a model read the record through a small set of tools that cite their evidence, and, when
configured to, save the person's own words as a note.

It is a client of Anam's service API and nothing more: it calls the daemon over HTTP with a token,
holds no state worth recovering, and offers only tools whose operations the daemon implements. The
API contract is `docs/API.md` in the anam repo.

Status: six reading tools and one resource, over what the daemon serves today; two selection tools,
`save_selection` and `open_selection`; and two writing tools, `save_note` and
`save_fact`, offered only when `ANAM_MCP_NOTE_TOKEN_FILE` is set.

| Tool | What it does | What it publishes |
|---|---|---|
| `describe_record` | Whose record, ledger identity and watermark, time and zone, source freshness, every admitted type, each type's count, fact-time range and sources at the watermark, every projection a read may go through, the operator's default projection to read first when one is set, what the server can and cannot do | Nothing |
| `describe_type` | One type's Definition as published: JSON Schema with field descriptions, time semantics, identity, and the handling guidance every writer of the type agrees on | Nothing |
| `read_record` | The timeline (narrowed by `from`, `to`, `type`) or a snapshot at a fact-time; `as_of` or `watermark` pin the knowledge; `fields` (JSON pointers) answers a frame of only those columns; a published read too large for one answer comes as its first page, with `page` and a `page_notice` that says it is partial | With a `projection`, an immutable artifact and its receipt; without one, nothing, and the answer says so |
| `describe_query` | What a query program is written against: the syntax (aggregates included), the base relations (`month_of/2` and `day_of/2` among them) and the `instant/1` parameter, the exact fact-time lexeme, each type's payload pointers at each Definition version, and the bundled rules (`current/2`, `displaced/2`) with their hash and text | Nothing |
| `query_record` | A Datalog program run through a projection (a listed one by `projection`, or one written in the call with `projection_inline` when none covers the question; exactly one of the two), built on the bundled rules, answered as one relation: joins across types and along time, selection by payload field, what was current at each instant the program names, and counts, sums, minimums and maximums grouped by type, value or UTC month and day (`month_of/2`, `day_of/2`; no average, so divide the sum by the count); a program that cannot run is refused with what to change | An immutable artifact (the answer) and its receipt, naming the projection (`inline`, version 1, for a `projection_inline` query, marked `template_inline`), the rule set's hash and the program; `dry_run` files nothing |
| `read_receipt` | A published read again by its receipt hash, a page at a time: without `cursor` the receipt and its first page, with a page's `next_cursor` the page after it, cut from the filed artifact and never a new read; another receipt's cursor is refused with what to pass instead | Nothing |
| `verify_receipt` | Whether a receipt's artifact is intact and the ledger reproduces it, for a read's receipt or a query's | Nothing |
| `save_selection` | A selection program (one defining `pick/1`) filed under its short code, `sel:` and eight hex digits, the program's own hash: the same program always gets the same code. Answers the code and the addresses of the person's timeline, highlighted timeline and select page at it | The program, as an artifact; never a fact |
| `open_selection` | A code back to its program and the facets it picks now, through a named projection, each with its id, type and fact time; an unknown code is an error, never an empty selection | Nothing: the run is a dry run, and `query_record` with relation `pick` gives the receipted answer |
| `save_note` | The person's words, verbatim and at once, as a `record.edge` under the conversation's own source, naming the facets they are about or none, marked as asserted by them; words said now take the present moment and words relayed from earlier take `at`, never in the future; the same words at the same minute are one note, and a correction (`id` and `at`) is a new version | A note in the record |
| `save_fact` | A fact the person asks to have recorded, as a facet of any type the daemon knows, under the conversation's own source and marked as asserted by them; the fields are the type's own, validated by the gate; the same fact again is a no-op and changed fields are a new version | A facet in the record |

The resource `anam://receipts/{hash}` serves a published read by its receipt hash: the receipt and
its first page.

- [docs/DESIGN.md](docs/DESIGN.md): decisions, tools, evidence handling, runtime, scenarios.
- [docs/API-GAPS.md](docs/API-GAPS.md): what the adapter needs from Anam and does not have yet.
- [docs/CLOUDFLARE.md](docs/CLOUDFLARE.md): serving it to Claude on the web through Cloudflare
  Tunnel and Access, step by step.

## Configuration

| Variable | Meaning |
|---|---|
| `ANAM_SERVER` | The daemon's base URL. Required |
| `ANAM_TOKEN_FILE` | A file holding the token; never logged. Omit only for a daemon on loopback that asks for none |
| `ANAM_ZONE` | IANA zone for reads, such as `Australia/Sydney`. Required: a bare date is a day in it |
| `ANAM_MCP_TIMEOUT` | Deadline for each call on the daemon, sent to it so it answers in time. Default `30s`, so a stuck daemon fails the tool within half a minute rather than holding the conversation. A positive duration |
| `ANAM_MCP_NETWORK_MARGIN` | Time allowed beyond that deadline for the answer to travel, so a daemon that answers at its bound is still heard. Default `5s`, ample on a LAN; a daemon across a slow link may want more. Zero or a positive duration |
| `ANAM_MCP_MAX_RESPONSE_BYTES` | The most the adapter reads of one answer from the daemon. Default `67108864` (64 MiB), the anam CLI's own bound; the daemon's `max_read_bytes` refuses a read far smaller than this first, so this only stops a daemon that misbehaves. From 1024 to 1073741824 |
| `ANAM_MCP_HTTP_ADDR` | Serve MCP over HTTP at `/mcp` on this address, such as `127.0.0.1:8790`, instead of stdio. Every request must carry a JWT from Cloudflare Access (`Cf-Access-Jwt-Assertion`), so the two Access variables are required with it. A `host:port`. Unset, the adapter speaks stdio |
| `ANAM_MCP_ACCESS_TEAM_DOMAIN` | The Access team domain that signs the JWT, such as `https://example.cloudflareaccess.com`. Required with `ANAM_MCP_HTTP_ADDR` |
| `ANAM_MCP_ACCESS_AUD` | The AUD tag of the Access application in front of the adapter. Required with `ANAM_MCP_HTTP_ADDR` |
| `ANAM_MCP_NOTE_TOKEN_FILE` | A file holding the token of the importer the person's words and facts are admitted as, in the daemon's `importers_file` with source `conversation` and `any_type: true` (or types `[record.edge]`, which offers only notes in effect). Set, the adapter offers `save_note` and `save_fact`; unset, it writes nothing. Never logged |

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

### Over HTTP, behind Cloudflare Access

For a client that cannot run a local process, such as Claude on the web, the adapter serves
stateless Streamable HTTP at `/mcp` and leaves OAuth to Cloudflare Access:

1. Run the adapter with `ANAM_MCP_HTTP_ADDR` on a loopback address beside cloudflared.
2. In the tunnel, route a hostname of its own (a subdomain, so OAuth discovery at the host's root
   is Access's) to that address.
3. Add a self-hosted Access application for the hostname with a policy that admits only you,
   and turn on Managed OAuth. Access then answers discovery, registers the client, logs you in
   and hands the adapter a signed JWT on each request.
4. Set `ANAM_MCP_ACCESS_TEAM_DOMAIN` and `ANAM_MCP_ACCESS_AUD` from that application.
5. Add `https://<hostname>/mcp` as a custom connector in the client.

[docs/CLOUDFLARE.md](docs/CLOUDFLARE.md) has each step in the dashboard and by API, the token
permissions, the checks and what to look at when it fails.

The `Dockerfile` builds a static image on a distroless base, running as a non-root user. It sets
no configuration; pass the variables above and mount the token files where that user can read
them. With host networking beside cloudflared, both the daemon and the adapter stay on loopback.

The adapter refuses any request without a valid JWT for that application, so a route that
bypasses Access reaches nothing. Leave `ANAM_MCP_NOTE_TOKEN_FILE` unset to serve reads only.

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
