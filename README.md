# gomuks-beeper-api

HTTP API server that implements a Beeper Desktop API-compatible surface on top of `gomuks`.

## What it does

- Starts gomuks from a dedicated state directory
- Exposes Beeper-compatible routes with Bearer token auth
- Maps gomuks rooms/events into Beeper chat/message/account schemas
- Implements the current `v1` route surface used by Beeper Desktop API clients (accounts, chats, messages, assets, search, contacts)
- Exposes practical `v0` aliases used by older Beeper Connect clients
- Supports asset upload/download/serve endpoints
- Uses Matrix room/account data (`m.tag`, `com.beeper.mute`, `com.beeper.inbox.done`, `com.beeper.chats.reminder`) to enrich chat state
- Supports `POST /v1/chats` in both `mode: "create"` and `mode: "start"` formats
- Supports both contacts APIs: `GET /v1/accounts/{accountID}/contacts` and `GET /v1/accounts/{accountID}/contacts/list`
- Exposes WebSocket events at `GET /v1/ws` and `GET /ws` (`ready`, `subscriptions.*`, `chat.*`, `message.*`, `error`)
- Exposes OAuth2-compatible discovery/flow endpoints (`/.well-known/*`, `/oauth/*`) and `GET /v1/info`

## Run

```bash
go run ./cmd/server
```

## Environment

- `BEEPER_ACCESS_TOKEN` (optional): static bearer token for legacy direct Bearer auth
- `BEEPER_API_LISTEN` (optional): listen address (default `127.0.0.1:23373`)
- `BEEPER_STATE_DIR` (optional): runtime state root (default `~/.local/share/gomuks-beeper-api`)
- `BEEPER_ALLOW_QUERY_TOKEN` (optional): set `true` to allow `dangerouslyUseTokenInQuery` for `/v1/assets/serve`

## Login (Beeper Session)

`gomuks-beeper-api` now includes a built-in setup UI at:

- `http://127.0.0.1:23373/manage` (or your configured listen address)

Flow:

1. Start the server:
```bash
go run ./cmd/server
```
2. Open `/manage`.
3. Log in:
   - Recommended: use **Beeper Email Login** (request code -> submit code).
   - Alternative: use **Password Login** (homeserver URL, username, password).
4. Enter your recovery key/passphrase in **Verification**.
5. Confirm `is_logged_in=true` and `is_verified=true` in Client State.

If no valid Beeper session exists, protected API calls return `403`.

## Auth Modes

You can authenticate in two ways:

- Static token: set `BEEPER_ACCESS_TOKEN` and send it as Bearer token.
- OAuth2 flow: use `/.well-known/oauth-protected-resource` and `/oauth/*` endpoints to get access tokens.

`GET /v1/info` reports active endpoint URLs and auth discovery metadata.

## Notes

- The server imports and runs `go.mau.fi/gomuks` as a library.
- Local bridge account discovery is sourced from `com.beeper.local_bridge_state` account-data with session fallback.
- Requests are accepted only for Beeper homeserver sessions (`matrix.beeper.com`, staging, dev).
