# AGENTS.md: anam-mcp

- The contract is `docs/API.md` in the anam repo. A tool that needs something the API does not
  offer is a change to Anam first; record it in `docs/API-GAPS.md`.
- The adapter translates requests and results. No SQL, no second fold, no domain rules, no
  imports from Anam's `internal/`.
- Go, with the official MCP SDK. Go standards follow the anam repo's `AGENTS.md`.
- Tests run a real SDK client and server against a real `anam daemon` on a scratch ledger. No
  mocks of the daemon.
- Public-repo hygiene: no tokens, hosts, paths or personal identifiers in code or examples.
- A config key overrides every opinionated default (anam card 151). A value someone could
  reasonably choose differently is a default: an `ANAM_MCP_*` variable read in `internal/app`,
  a `Default…` constant holding today's value, range-checked at start, and passed down as a
  parameter. A value whose change breaks the MCP contract, the daemon's API or what a citation
  means is an invariant: a constant with a comment saying why it is not configurable. No test
  hooks: a value a test changes is a default, and the test sets it as a caller would. Document
  each key in the README's configuration table.
