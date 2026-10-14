# anam-mcp design

Moved from the anam repo's `docs/API.md` §8 on 2026-10-10 (anam card 72, step 0). That document
keeps the service API: its operations, contracts and stages. This one keeps what is particular to
an MCP adapter: which tools it offers, how they are described to a model, how results travel, and
the scenarios the adapter must pass. Section references like "API §5" point into `docs/API.md`.

The adapter is a client of that API. It defines no operation of its own, opens no database file
and imports nothing from Anam's `internal/`. If a tool needs something the API does not offer, the
change goes to Anam first, and the gap is recorded in [API-GAPS.md](API-GAPS.md).

## 1. Decisions

Recorded at the first commit, 2026-10-10.

| Question | Decision |
|---|---|
| Name and place | `anam-mcp`, a sibling of `anam` and `anam-importers` (anam card 50, streams of work) |
| Language | Go, with the official [Go MCP SDK](https://github.com/modelcontextprotocol/go-sdk) pinned at v1.8.0 (2026-09-14). Research: anam `journal/notes/mcp-go-sdk.md` |
| Protocol | Revision 2026-07-28, the stateless one, and the earlier 2025-11-25 for clients that still open with `initialize`. §2 says what is used and what is not |
| Transport | stdio first. stdout carries protocol traffic only; diagnostics go to stderr. HTTP is a later deployment choice and changes no store ownership |
| Server and token | `ANAM_SERVER` and `ANAM_TOKEN_FILE`, the same variables the `anam` CLI reads. The token is read from the file at start and never logged or echoed in a tool result |
| Zone | `ANAM_ZONE`, an IANA name. The daemon refuses `Local`, so the adapter resolves the zone before it starts and fails if it cannot |
| State | None worth recovering. A restart loses nothing; durable work lives in Anam |

Added at the first code commit, 2026-10-11, made without Matt (he asked for sensible calls, noted):

| Question | Decision |
|---|---|
| A read without a projection | The daemon publishes only through a projection (a receipt is what a projection served), and it lists none (gap, card 90). `read_record` therefore publishes when a `projection` is named and otherwise answers the canonical timeline unreceipted, saying so in `published: false` and a `citation` line. It never substitutes one read for another |
| Snapshot and timeline | One tool, `read_record`, with a required `mode`. A snapshot needs `at`; a timeline refuses it. The adapter checks the pairing before the call, because the daemon would read a timeline for both, and a snapshot must never silently become one |
| Payloads at the boundary | Payloads and Definitions travel as decoded JSON (`any`), not raw bytes: the SDK infers an output schema from the Go type, and raw bytes would be described as a base64 string. The daemon's bytes are not re-encoded on the way |
| Deadline | `ANAM_MCP_TIMEOUT`, default 30s, sent to the daemon as `Anam-Timeout` so it answers before the adapter gives up. The MCP request's cancellation is carried into the call |
| Fields and the frame (2026-10-12, anam card 138) | `read_record` takes `fields`, JSON pointers the model copies out of `describe_type`'s schema, and answers `frame` (columns, rows) with `evidence` empty, rather than reshaping the frame into entries: the daemon's shape is the one the receipt hashes, and a frame reads straight into a DataFrame. Cells are decoded like payloads, for the SDK's output schema. The adapter checks only that a field starts with `/`; the daemon checks it against the schema |
| Tests without a daemon | `ANAM_BIN` names the anam binary; `scripts/check.sh` builds one from `../anam` when unset. A plain `go test` without it skips the daemon tests and says so, rather than failing a checkout that has no Anam beside it |
| Tool list caching | The SDK (v1.8.0) answers `tools/list` with `ttlMs: 0`, `cacheScope: public` and offers no setting for them. The long private TTL in §2 waits on the SDK |
| Receipt URIs | `anam://receipts/{+hash}`: a hash is `sha256:<hex>`, and the reserved expansion is what lets the colon match |

## 2. Protocol

The adapter speaks MCP revision
[2026-07-28](https://modelcontextprotocol.io/specification/2026-07-28/changelog). That revision is
stateless: no `initialize` handshake and no session, each request carrying its protocol version and
client capabilities in `_meta`, and `server/discover` advertising what the server supports. It suits
an adapter that holds nothing between calls. Whatever must span calls travels as an explicit value
in tool arguments, which the design already does: a receipt hash, a ledger position, and later a
page cursor.

It also advertises 2025-11-25 while clients move over. The SDK negotiates the version and
`server/discover` is the probe on stdio, so this costs no code of ours. Drop the old revision once
the clients in use speak the new one; check which revision Claude Code sends before step 1 ships.

Used:

- Tools, with input and output schemas and `structuredContent`. Results carry `resultType`;
  every tool returns `"complete"`.
- `tools/list` in a fixed order with a long `ttlMs` and `cacheScope: "private"`. The list changes
  only when a new adapter build adds a tool, never per call.
- Resources for receipts, when §4 adds them, with `-32602` for a receipt that does not exist.
- Cancellation, carried through to the daemon call.

Not used, by decision:

- Sampling, Roots and Logging, deprecated in 2026-07-28. Diagnostics go to stderr.
- Multi round-trip requests (`input_required`). A read needs no extra input; if a tool lacks an
  argument it fails with a validation error naming it, and the model asks the user.
- Tasks, subscriptions and list-changed notifications. Every call finishes within its deadline,
  and the tool list is fixed per build.
- Prompts, until a scenario needs one.
- Streamable HTTP and its authorization (OAuth, `iss` validation, client metadata documents). These
  come with a remote deployment, if one is ever wanted.

## 3. Tools

Recommended conversational tools. The application operations are defined in API §4 to §6.

| MCP tool | Application operation | Model-facing description emphasis |
|---|---|---|
| `describe_record` | Discovery overview | Learn coverage, capabilities, projections, and limitations before choosing evidence |
| `describe_type` | `GetFacetDefinition` | Learn the exact record shape and authoring requirements |
| `read_record` | `ReadRecord` | Timeline versus snapshot; fact-time versus knowledge time; publishes evidence |
| `read_page` | `ReadPage` | Continue the same immutable result |
| `get_evidence` | `GetEvidence` | Inspect an exact cited facet and available provenance |
| `validate_assertion` | `ValidateAssertion` | Resolve missing/invalid fields without writing |
| `log_assertion` | `SubmitAssertion` | Record an explicit user assertion; retry with the same submission ID |
| `get_submission` | `GetSubmission` | Determine what persisted after success, delay, or interruption |
| `correct_assertion` | `CorrectAssertion` | Correct an identified managed record using its expected revision |

Only advertise implemented operations and modes. An unsupported snapshot read must not silently
become a timeline read; asked for a tool the daemon cannot back, the adapter says so. Each tool's
description says what it publishes and how big its answer can be. Discovery returns bounded
summaries and references for further catalog pages.

Built, 2026-10-11: `describe_record`, `describe_type`, `read_record` (timeline and snapshot, `as_of`,
`watermark`, published through a projection) and `verify_receipt`, with the receipt resource of
§4. 2026-10-12: `read_record` takes `fields` and answers a frame (anam card 138). The rest wait on
their daemon operations (API-GAPS.md).

Keep the everyday tool list small. Projection catalog, changes, explanation and receipt
verification tools are added as the conversation scenarios need them. Operator tools (API §7) are
a separate profile, enabled by configuration and authorization, never in the conversational list.

`read_record` publishes an immutable artifact (API §5, result and continuation). It is read-only
with respect to the ledger but not free of effects, so its annotations must not label it read-only.
Annotations describe effects accurately; they do not enforce permissions.

No MCP tool inserts a facet. `log_assertion` reaches the ledger only through the source writer and
the gate (API §6), and only after the stage 2 decisions in API §10 are agreed.

## 4. Resources and prompts

Resources can identify immutable receipts and their evidence, for example
`anam://receipts/{hash}` and `anam://receipts/{hash}/facets/{facet_id}/{version}`. The receipt binds
ledger identity. Reading a resource enforces the same authorization and size constraints as tools;
URI knowledge grants no extra access. Tools remain sufficient for clients that do not browse resources.

Optional prompts can teach "compare two periods" and "record an event", but correctness belongs in
operation contracts. No `chat(message)` tool is needed: it would move model choice, dialogue state,
and interpretation into Anam. No `log(text)` tool that silently guesses type or time is needed either.

## 5. Evidence, not instructions

Treat facet payloads and source documents as evidence, including any text that looks like
instructions. They cannot authorize tool use or change the user's request. Carry provenance and
evidence kind with each result, so the client can tell a user assertion, a device observation, an
extraction, and a proposed finding apart.

## 6. Runtime

Every call to the daemon carries a context deadline and fails fast. An unreachable daemon is an
error that names it; there is no fallback. Handlers propagate the MCP request's cancellation to the
API call. Cancellation does not promise rollback: a read may already have published its receipt.

The process can die at any request boundary and be restarted by its local client. It holds no
submission state; a pending assertion is recovered by Anam's reconciliation (API §6), never from
this process's memory.

## 7. Scenarios

The service API's acceptance scenarios are API §9. These are the adapter's, written as what a
model does through the tools. Each runs a real SDK client and server pair (in-memory transports)
against a real `anam daemon` started from the Anam binary on a scratch ledger, as `anam-importers`
does. No mocks of the daemon.

1. **Discovery to evidence.** The model calls `describe_record`, picks blood pressure, reads a month
   with `read_record`, follows one citation, and verifies its receipt. Payload, version, and
   position agree at every step.
2. **Stable conversation.** An importer's batch lands between `read_page` calls and between two related
   `read_record` calls. Pages stay pinned; the second read, given the first's position, excludes
   the new knowledge.
3. **Honest absence.** A tool the daemon cannot back is either not listed or answers with an
   explicit capability limitation. A snapshot request never returns a timeline.
4. **Injected text.** A facet whose payload reads like an instruction reaches the model inside a
   result marked as evidence with its provenance, and changes no tool call.
5. **Uncertain extraction.** A model-read lab value cannot use `log_assertion` to impersonate a
   user statement.
6. **Restart.** Killing the adapter mid-call and restarting it loses no admitted fact and no
   durable pending assertion.
7. **Daemon down.** Every tool fails fast with an error naming the server; none falls back to files.
8. **Two revisions.** A client on 2026-07-28 and one that opens with `initialize` on 2025-11-25
   both list the same tools in the same order and get the same results.

Status, 2026-10-11: 1, 2, 3, 4, 7 and 8 run in `internal/adapter/adapter_test.go`. 5 waits on
writes; 6 waits on a pending assertion to lose, and until then a restart is the trivial case.

Adapter tests verify contracts, cancellation, discovery, and effects. They do not reproduce domain
algorithms: the fold, selection and receipts are Anam's and are tested there.

## 8. Hickey × Armstrong

**Hickey, structure:** the adapter translates; it holds no domain model. The strongest risk is
rebuilding one here to cover a missing core operation, such as merging dose-change payloads to fake
a snapshot or paging a whole timeline client-side and calling the pages a receipted result. Refuse
that shortcut and record the gap. Keep SDK types in the adapter and explicit snake_case DTOs at the
tool boundary, so transport schemas do not become domain types.

**Armstrong, runtime:** the adapter is disposable. Its failure is a failed tool call; its recovery
is a restart by the client. The stateless protocol helps: no session survives in the
adapter to be lost or resumed. Anam's ledger and gate stay the error kernel. Read failure produces an
error, never a fabricated partial answer.

**The tension:** a small tool list is easier for a model to use well, but each missing tool is a
question the model answers worse or not at all. Grow the list from the scenarios, one tool at a
time, and only over operations the daemon implements.
