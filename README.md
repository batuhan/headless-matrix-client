# gomuks-beeper-api

HTTP API server that implements a Beeper Desktop API-compatible surface on top of `gomuks`.

## What it does

- Starts gomuks from a dedicated state directory
- Exposes Beeper-compatible routes with Bearer token auth
- Maps gomuks rooms/events into Beeper chat/message/account schemas
- Implements the current `v1` route surface used by Beeper Desktop API clients (accounts, chats, messages, assets, search, contacts)
- Supports asset upload/download/serve endpoints
- Uses Matrix room/account data (`m.tag`, `com.beeper.mute`, `com.beeper.inbox.done`, `com.beeper.chats.reminder`) to enrich chat state
- Supports `POST /v1/chats` in both `mode: "create"` and `mode: "start"` formats
- Supports both contacts APIs: `GET /v1/accounts/{accountID}/contacts` and `GET /v1/accounts/{accountID}/contacts/list`

## Run

```bash
BEEPER_ACCESS_TOKEN=your_token go run ./cmd/server
```

## Environment

- `BEEPER_ACCESS_TOKEN` (required): static bearer token
- `BEEPER_API_LISTEN` (optional): listen address (default `127.0.0.1:23373`)
- `BEEPER_STATE_DIR` (optional): runtime state root (default `~/.local/share/gomuks-beeper-api`)
- `BEEPER_ALLOW_QUERY_TOKEN` (optional): set `true` to allow `dangerouslyUseTokenInQuery` for `/v1/assets/serve`

## Notes

- The server imports and runs `go.mau.fi/gomuks` as a library.
- Local bridge account discovery is sourced from `com.beeper.local_bridge_state` account-data with session fallback.
- `v0` aliases are intentionally not exposed.
- Requests are accepted only for Beeper homeserver sessions (`matrix.beeper.com`, staging, dev).
