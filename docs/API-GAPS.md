# API gaps

What the adapter needs from Anam's service API and does not have. Each gap is a change to Anam
first, filed as a card on Anam's board, then adopted here. The adapter does not work around a gap
by doing the core's job.

| Gap | Blocks | Anam card |
|---|---|---|
| Paging over an immutable published result (`ReadPage`, API §5) | `read_page` | to file when step 1 hits it |
| Evidence by facet within a receipt (`GetEvidence`, API §5) | `get_evidence` | to file when step 1 hits it |
| Snapshot reads (API §5, stage 3 in API §10) | `read_record` in snapshot mode | to file when step 1 hits it |
| A reader-only token: today's operator token can also start ingests, mints and backups | Least privilege for a conversational client | to file when step 1 hits it |
| The assertion lifecycle (API §6, stage 2 in API §10) | `validate_assertion`, `log_assertion`, `get_submission`, `correct_assertion` | waits on API §10's source-writer decisions |
