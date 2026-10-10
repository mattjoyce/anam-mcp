# AGENTS.md: anam-mcp

- The contract is `docs/API.md` in the anam repo. A tool that needs something the API does not
  offer is a change to Anam first; record it in `docs/API-GAPS.md`.
- The adapter translates requests and results. No SQL, no second fold, no domain rules, no
  imports from Anam's `internal/`.
- Go, with the official MCP SDK. Go standards follow the anam repo's `AGENTS.md`.
- Tests run a real SDK client and server against a real `anam daemon` on a scratch ledger. No
  mocks of the daemon.
- Public-repo hygiene: no tokens, hosts, paths or personal identifiers in code or examples.
