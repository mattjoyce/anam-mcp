# API gaps

What the adapter needs from Anam's service API and does not have. Each gap is a change to Anam
first, filed as a card on Anam's board, then adopted here. The adapter does not work around a gap
by doing the core's job.

Checked against the live daemon (v0.220) on 2026-10-10. Ordered by how much each blocks the goal:
answers a person can check.

| Gap | Today | Blocks | Anam card |
|---|---|---|---|
| A receipted read over the API | `GET /v1/timeline` derives a value and its hash but publishes nothing; receipts come only from a local `anam serve` | Citing an answer and verifying it later (scenario 1) | 87 |
| A read pinned to a ledger position | Knowledge is selected by `as_of`, an anam-time instant, not by `w` | Two related reads seeing the same knowledge (scenario 2) | 87 |
| A bound on a read's size | A whole-record read is 12.8 MB, a snapshot of everything 11.5 MB, with no limit or `result_too_large` | Safe reads without the adapter refusing broad queries itself | 88 |
| A reader-only token | The operator token also starts ingests, mints and backups | Least privilege for a conversational client | 89 |
| Discovery: projections and coverage | No route lists projections (`bp`, `meds`, `sleep` are bundled); no counts or date ranges per type | `describe_record` saying what is there and how far back | 90 |
| One facet by reference | No route returns a facet version by id with its provenance | `get_evidence` | 91 |
| Paging over a published result (`ReadPage`, API §5) | Reads are whole | `read_page`, once reads are receipted | 92 |
| The assertion lifecycle (API §6, stage 2 in API §10) | No write route for a reader | `validate_assertion`, `log_assertion`, `get_submission`, `correct_assertion` | 93 |

Not gaps: snapshots (`at`, latest per series, closed series gone), bitemporal reads (`as_of`
with `at`), projections by name, `explain=true` verdicts, Definitions with field descriptions,
and per-entry provenance (`source`, `source_ref`, `content_hash`, `status`, `classification`, and
the artifact version in `versions`).
