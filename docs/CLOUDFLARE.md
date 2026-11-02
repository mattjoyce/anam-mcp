# Serving anam-mcp to Claude on the web through Cloudflare

Claude on the web reaches a custom connector from the internet, so the adapter cannot stay on
stdio or on a private network for it. This is how to put it on a public hostname with Cloudflare
Tunnel, and behind Cloudflare Access so that only you get through. Access does the OAuth; the
adapter only checks the JWT Access signs (DESIGN.md §1, "HTTP behind Cloudflare Access").

Placeholders: `example.com` is your zone, `anam-mcp.example.com` the adapter's hostname,
`<team>` your Zero Trust team name, `<account>` your account ID, `<zone>` the zone ID, `<tunnel>`
the tunnel ID, `you@example.com` the address allowed in.

## What you need

- A zone on Cloudflare and a Zero Trust organisation (the free plan is enough).
- A Cloudflare Tunnel, managed from the dashboard (token-based), with cloudflared running on the
  host where the adapter will run.
- An Anam daemon the adapter can reach, and a reader token for it. Give the remote adapter its own
  principal, so it can be revoked alone.

## How a request travels

```
Claude ──HTTPS──► Cloudflare edge ──► Access (OAuth, policy) ──► tunnel ──► cloudflared
                                                                              │ loopback
                                       anam daemon ◄── reader token ── anam-mcp :8790/mcp
```

1. Claude calls `https://anam-mcp.example.com/mcp` with no token. Access answers 401 with
   `WWW-Authenticate: Bearer realm="OAuth" ... resource_metadata=...`.
2. Claude reads `/.well-known/oauth-authorization-server`, which Access serves, registers itself
   as a client and opens the Access login in your browser.
3. You log in. Access checks its policy and gives Claude an opaque access token.
4. Each later call carries that token. Access turns it into a signed JWT in the
   `Cf-Access-Jwt-Assertion` header and sends the request down the tunnel.
5. The adapter verifies the JWT (issuer `https://<team>.cloudflareaccess.com`, audience the app's
   AUD tag, signature from the team's published keys) and refuses the request with 401 without
   it.

## 1. Choose the hostname

Give the adapter a subdomain of its own, one level deep: `anam-mcp.example.com`.

- OAuth discovery lives at the root of the host (`/.well-known/...`). On a hostname shared with a
  website, the site's catch-all route owns those paths.
- Cloudflare's free Universal SSL certificate covers `example.com` and `*.example.com` only. A
  name two levels deep, such as `anam.mcp.example.com`, fails the TLS handshake unless you buy
  Advanced Certificate Manager.

## 2. An API token, if you configure by API

Everything below can be done in the dashboard. To do it by API, create a token with exactly these
permissions. The dashboard's names are easy to confuse, so the names here are the dashboard's:

| Scope | Permission | For |
|---|---|---|
| Zone `example.com` | Zone: Read | finding the zone ID |
| Zone `example.com` | DNS: Edit | the CNAME. Not "Zone DNS Settings", which is DNSSEC and the like |
| Account | Access: Edit ("Access applications and policies") | the Access app, its Managed OAuth, its policy |
| Account | Access: Identity Providers: Read | choosing the login method |
| Account | Access: Organizations: Read | reading the team domain |
| Account | Cloudflare One Connector: cloudflared: Edit | the tunnel route. Read is enough if you add the route in the dashboard |

Not needed: "OAuth Client" (that is Cloudflare's own API OAuth apps, not Access), "Zone
Settings", "Argo Tunnel (Legacy)". Restrict the token to your IP address and give it a short
expiry. Keep it out of variables a tool reads on its own, such as `CLOUDFLARE_API_TOKEN`.

The examples below use:

```sh
cf() { curl -sS -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" "$@"; }
API=https://api.cloudflare.com/client/v4
```

## 3. Find the team domain

Zero Trust → Settings shows the team name, or:

```sh
cf "$API/accounts/<account>/access/organizations"   # result.auth_domain: <team>.cloudflareaccess.com
```

The adapter's `ANAM_MCP_ACCESS_TEAM_DOMAIN` is `https://<team>.cloudflareaccess.com`.

## 4. Create the Access application first

Create it before the hostname resolves, so it is never reachable without Access.

In the dashboard: Zero Trust → Access controls → Applications → Add an application →
Self-hosted. Domain `anam-mcp.example.com`; login method the one you use (One-time PIN works:
Access emails you a code); a policy that allows your address only. Then, under Advanced settings,
turn on **Managed OAuth**, turn on dynamic client registration and allow Claude's callback URLs.

By API, the policy and then the app:

```sh
cf -X POST "$API/accounts/<account>/access/policies" -d '{
  "name": "anam-mcp: owner only", "decision": "allow",
  "include": [{"email": {"email": "you@example.com"}}]
}'
# result.id is the policy ID

cf "$API/accounts/<account>/access/identity_providers"   # the ID of the login method to use

cf -X POST "$API/accounts/<account>/access/apps" -d '{
  "name": "anam-mcp", "type": "self_hosted", "domain": "anam-mcp.example.com",
  "allowed_idps": ["<idp id>"], "auto_redirect_to_identity": true,
  "session_duration": "24h", "app_launcher_visible": false,
  "policies": [{"id": "<policy id>", "precedence": 1}],
  "oauth_configuration": {
    "enabled": true,
    "dynamic_client_registration": {
      "enabled": true, "allow_any_on_localhost": false, "allow_any_on_loopback": false,
      "allowed_uris": ["https://claude.ai/api/mcp/auth_callback",
                       "https://claude.com/api/mcp/auth_callback"]
    },
    "grant": {"access_token_lifetime": "15m", "session_duration": "336h"}
  }
}'
# result.aud is the AUD tag: the adapter's ANAM_MCP_ACCESS_AUD
```

- `access_token_lifetime` is how long one token lasts; Claude refreshes it, and Access checks
  the policy again at each refresh. `session_duration` under `grant` is how long refreshing works
  before you log in again (14 days here; Cloudflare suggests one to two weeks for agents).
- `auto_redirect_to_identity` skips the login-method chooser when there is only one.
- To change the app later by API, `GET` it first and `PUT` every field back: a `PUT` that leaves
  a field out clears it.
- Deleting and recreating the app gives it a new AUD tag. Update the adapter when it does.

## 5. Run the adapter

On the host that runs cloudflared, with the daemon's reader token in a file the container's user
(uid 65532) can read:

```sh
docker build -t anam-mcp .
docker run -d --name anam-mcp --restart unless-stopped --network host --user 65532:65532 \
  -e ANAM_SERVER=http://127.0.0.1:8787 \
  -e ANAM_TOKEN_FILE=/run/anam-mcp/token \
  -e ANAM_ZONE=Australia/Sydney \
  -e ANAM_MCP_HTTP_ADDR=127.0.0.1:8790 \
  -e ANAM_MCP_ACCESS_TEAM_DOMAIN=https://<team>.cloudflareaccess.com \
  -e ANAM_MCP_ACCESS_AUD=<aud> \
  -v /path/to/token:/run/anam-mcp/token:ro \
  anam-mcp
```

- Host networking keeps both ports on loopback: the daemon's and the adapter's. If cloudflared
  runs on a Docker network instead, listen on that network's address and route to it.
- Without `ANAM_MCP_NOTE_TOKEN_FILE` the adapter offers no writing tools. Add it only once you
  want Claude on the web to save notes and facts.
- The adapter will not start with `ANAM_MCP_HTTP_ADDR` set and either Access variable missing.
- On the host, `curl -X POST http://127.0.0.1:8790/mcp` answers 401: no JWT, no answer.

## 6. Point the hostname at the tunnel

In the dashboard, adding a public hostname to the tunnel (Networks → Tunnels → your tunnel →
Public hostnames) creates the route and the DNS record together: hostname `anam-mcp.example.com`,
service `http://localhost:8790`.

By API they are two calls, and the second needs care.

The DNS record:

```sh
cf -X POST "$API/zones/<zone>/dns_records" -d '{
  "type": "CNAME", "name": "anam-mcp.example.com",
  "content": "<tunnel>.cfargotunnel.com", "proxied": true
}'
```

The route. A `PUT` of the tunnel's configuration **replaces the whole ingress list**, so every
other hostname on the tunnel depends on getting it right:

1. `cf "$API/accounts/<account>/cfd_tunnel/<tunnel>/configurations" > before.json` and keep that
   file: it is the rollback.
2. Insert one rule, `{"hostname": "anam-mcp.example.com", "service": "http://localhost:8790"}`,
   anywhere before the final catch-all (`{"service": "http_status:404"}`). Rules match in order,
   and a rule for a whole hostname must come before any broader rule that would also match it.
3. Diff the new list against the old one. The only change should be the new rule.
4. Fetch the configuration again and check its `version` has not changed since step 1.
5. `cf -X PUT "$API/accounts/<account>/cfd_tunnel/<tunnel>/configurations" -d '{"config": ...}'`
   with the whole new config. The answer carries the next version.
6. cloudflared logs `Updated to new configuration` within seconds. Check the tunnel's other
   hostnames still answer as they did.

## 7. Check it from outside

```sh
curl -si -X POST https://anam-mcp.example.com/mcp | grep -iE '^(HTTP|www-authenticate)'
# HTTP/2 401, WWW-Authenticate: Bearer realm="OAuth" ... resource_metadata=...
curl -s https://anam-mcp.example.com/.well-known/oauth-authorization-server
# issuer https://<team>.cloudflareaccess.com, authorization and token endpoints
```

The 401 here comes from Access, not from the adapter: its `WWW-Authenticate` names OAuth.

## 8. Connect Claude

In Claude: Settings → Connectors → Add custom connector, URL `https://anam-mcp.example.com/mcp`.
Leave the advanced OAuth fields empty; Access registers Claude by itself. Claude opens the Access
login; log in with the allowed address. Then ask something that needs `describe_record`.

## When it does not work

| Symptom | Likely cause |
|---|---|
| TLS error on the hostname | A name two levels deep under the free certificate (step 1) |
| Login page says the redirect is not allowed | Claude's callback is not in `allowed_uris` (step 4) |
| Logged in, then 404 | No tunnel route for the hostname; the request fell through to the catch-all (step 6) |
| Logged in, then 502 | The route is there but nothing listens on the port, or the adapter listens on another address |
| Logged in, then 401 with a plain body | The adapter refused the JWT: `docker logs anam-mcp` says why. Most often the AUD changed (app recreated) or the team domain is wrong |
| 403 from the adapter | The MCP SDK's DNS-rebinding guard; the adapter turns it off for HTTP, so check the build is current |
| A tool fails with 401 or 403 from the daemon | The reader token: the file, its hash in the daemon's principals, a daemon restart after adding it |

## Revoking

Any one of these cuts Claude off: remove or disable the Access application's policy; remove the
reader principal from the daemon and restart it; stop the container. Removing the tunnel route
or the DNS record also works but needs the same care as adding it.
