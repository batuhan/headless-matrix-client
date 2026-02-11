# gomuks-beeper-api

HTTP API server that implements a Beeper Desktop API-compatible surface on top of `gomuks`.

## What it does

- Starts gomuks from a dedicated state directory
- Exposes Beeper-compatible routes with Bearer token auth
- Maps gomuks rooms/events into Beeper chat/message/account schemas
- Supports asset upload/download/serve endpoints
- Exposes unsupported routes as explicit `501 NOT_IMPLEMENTED`

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
- `v0` aliases are exposed for compatible endpoints where defined in Beeper Desktop API spec.
