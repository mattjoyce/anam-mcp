# API gaps

What the adapter needs from Anam's service API and does not have. Each gap is a change to Anam
first, filed as a card on Anam's board, then adopted here. The adapter does not work around a gap
by doing the core's job.

Checked against the live daemon (v0.220) on 2026-10-10, and against master a452f18 on 2026-10-11
when the first tools were built. Ordered by how much each blocks the goal: answers a person can
check.

| Gap | Today | Blocks | Anam card |
|---|---|---|---|
| ~~A receipted read over the API~~ | Done (card 87): `POST /v1/receipts` publishes the read a projection serves. Adopted by `read_record` | | 87 |
| ~~A read pinned to a ledger position~~ | Done (card 87): `w` reads at a receipt's watermark. Adopted as `read_record`'s `watermark` | | 87 |
| ~~A bound on a read's size~~ | Done (card 88): 413 `result_too_large` with what to narrow by. Passed through as the tool's error | | 88 |
| A receipt without a projection | A read of the canonical, or of one type without a projection, cannot be published: the daemon publishes only what a projection served, and names none the model could pick (card 90) | Citing a read that names no projection; today `read_record` answers it unreceipted and says so | 90 |
| Evidence kind per facet | An entry carries `source`, `source_ref`, `status` and `classification`, but nothing says whether it is a device observation, a person's own assertion or an extraction | Telling a user assertion from an extraction in a result (design §5); the adapter must not infer it from the source name | 93 |
| A reader-only token | The operator token also starts ingests, mints and backups | Least privilege for a conversational client | 89 |
| Discovery: projections and coverage | No route lists projections (`bp`, `meds`, `sleep` are bundled); no counts or date ranges per type | `describe_record` saying what is there and how far back | 90 |
| One facet by reference | No route returns a facet version by id with its provenance | `get_evidence` | 91 |
| Paging over a published result (`ReadPage`, API §5) | Reads are whole | `read_page`, once reads are receipted | 92 |
| The assertion lifecycle (API §6, stage 2 in API §10) | No write route for a reader | `validate_assertion`, `log_assertion`, `get_submission`, `correct_assertion` | 93 |

Not gaps: snapshots (`at`, latest per series, closed series gone), bitemporal reads (`as_of`
with `at`), projections by name, `explain=true` verdicts, Definitions with field descriptions,
and per-entry provenance (`source`, `source_ref`, `content_hash`, `status`, `classification`, and
the artifact version in `versions`).
