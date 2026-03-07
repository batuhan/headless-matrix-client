# EasyMatrix

EasyMatrix is a Beeper Desktop API-compatible headless Matrix client built on `gomuks`. It can be used with Desktop API JS SDK and even comes with an adapter that embeds the entire client (via N-API).

## JS Adapter

Adapter package: `@bi/easymatrix`

```bash
npx @bi/easymatrix
```

```ts
import { desktopAPIFetch, run } from "@bi/easymatrix";
```

## What it does

- Starts gomuks from a dedicated state directory
- Exposes Beeper-compatible routes with Bearer token auth
- Maps gomuks rooms/events into Beeper chat/message/account schemas
- Implements the current `v1` route surface used by Beeper Desktop API clients (accounts, chats, messages, assets, search, contacts)
- Supports asset upload/download/serve endpoints
- Uses Matrix room/account data (`m.tag`, `com.beeper.mute`, `com.beeper.inbox.done`, `com.beeper.chats.reminder`) to enrich chat state
- Supports `POST /v1/chats` in both `mode: "create"` and `mode: "start"` formats
- Supports both contacts APIs: `GET /v1/accounts/{accountID}/contacts` and `GET /v1/accounts/{accountID}/contacts/list`
- Exposes WebSocket events at `GET /v1/ws` (`ready`, `subscriptions.*`, `chat.*`, `message.*`, `error`)
- Exposes OAuth2-compatible discovery/flow endpoints (`/.well-known/*`, `/oauth/*`) and `GET /v1/info`

## Run

```bash
go run ./cmd/server
```

## Environment

- `.env` in the current working directory is loaded automatically (if present)
- `BEEPER_ACCESS_TOKEN` (optional): static bearer token for legacy direct Bearer auth
- `BEEPER_API_LISTEN` (optional): listen address (default `127.0.0.1:23373`)
- `BEEPER_STATE_DIR` (optional): runtime state root (default `~/.local/share/easymatrix`)
- `BEEPER_ALLOW_QUERY_TOKEN` (optional): set `true` to allow `dangerouslyUseTokenInQuery` for `/v1/assets/serve`
- `BEEPER_HOMESERVER_URL` (optional): homeserver for password login bootstrap (default `https://matrix.beeper.com`)
- `BEEPER_LOGIN_TOKEN` (optional): run JWT login automatically on startup
- `BEEPER_USERNAME` + `BEEPER_PASSWORD` (optional, must be set together): run password login automatically on startup
- `BEEPER_RECOVERY_KEY` (optional): run verification automatically on startup

## Login (Beeper Session)

`EasyMatrix` now includes a built-in setup UI at:

- `http://127.0.0.1:23373/manage` (or your configured listen address)

Flow:

1. Start the server:
```bash
go run ./cmd/server
```
2. Open `/manage`.
3. Log in:
   - Recommended: use **Beeper Email Login** (request code -> submit code).
   - Staging-friendly: use **JWT / Login Token** with a token from your account bootstrap script.
   - Alternative: use **Password Login** (homeserver URL, username, password).
4. Enter your recovery key/passphrase in **Verification**.
5. Confirm `is_logged_in=true` and `is_verified=true` in Client State.

If no valid Beeper session exists, protected API calls return `403`.

If you set `BEEPER_LOGIN_TOKEN` or `BEEPER_USERNAME`/`BEEPER_PASSWORD`, plus `BEEPER_RECOVERY_KEY`, startup will automatically login and verify without opening `/manage`.

`/manage` only supports recovery-key/passphrase verification today. Emoji / SAS confirmation is not exposed by gomuks' JSON command API yet.

## Embedded Bun Runtime

The embedded runtime is Bun-only and follows the same long-lived native runtime pattern gomuks uses for its JS-facing environments: start one native client, then talk to it through a narrow request/event bridge.

Current embedded options support the same startup auth inputs as server mode:

- `beeperHomeserverURL`
- `beeperLoginToken`
- `beeperUsername` / `beeperPassword`
- `beeperRecoveryKey`

Example:

```ts
import { BeeperDesktop, withEmbedded } from "@bi/easymatrix";

const embedded = await withEmbedded(BeeperDesktop, {
  runtime: {
    accessToken: "local-dev-token",
    stateDir: "/tmp/easymatrix-bun",
    beeperHomeserverURL: "https://matrix.beeper-staging.com",
    beeperLoginToken: process.env.BEEPER_LOGIN_TOKEN,
    beeperRecoveryKey: process.env.BEEPER_RECOVERY_KEY,
  },
});

const accounts = await embedded.sdk.accounts.list();
```

The native library must still be built first:

```bash
npm run build
```

## CLI Login

You can drive the same `/manage` flows from the terminal:

```bash
node ./scripts/easymatrix-login.mjs \
  --base-url http://127.0.0.1:23373 \
  --homeserver-url https://matrix.beeper-staging.com \
  --login-token "$BEEPER_LOGIN_TOKEN" \
  --recovery-key "$BEEPER_RECOVERY_KEY"
```

For email-code login:

```bash
node ./scripts/easymatrix-login.mjs \
  --base-url http://127.0.0.1:23373 \
  --domain beeper-staging.com \
  --email qatest+123456@beeper.com \
  --code 959729 \
  --recovery-key "$BEEPER_RECOVERY_KEY"
```

## Staging E2E

`EasyMatrix` now includes a staging harness that:

- creates or reuses two Beeper staging accounts
- starts two easymatrix instances on ports `23373` and `23374`
- verifies both accounts with recovery keys
- exercises most of the Desktop API-compatible surface (accounts, chats, messages, search, reminders, archive, assets, websocket events)

Run it with:

```bash
npm run e2e:staging
```

If the account bootstrap script is not in the default workspace location, set `BEEPER_ACCOUNT_CREATOR=/abs/path/to/create-beeper-account.js`.

## Auth Modes

You can authenticate in two ways:

- Static token: set `BEEPER_ACCESS_TOKEN` and send it as Bearer token.
- OAuth2 flow: use `/.well-known/oauth-protected-resource` and `/oauth/*` endpoints to get access tokens.

`GET /v1/info` reports active endpoint URLs and auth discovery metadata.

## Notes

- The server imports and runs `go.mau.fi/gomuks` as a library.
- Local bridge account discovery is sourced from `com.beeper.local_bridge_state` account-data with session fallback.
- Requests are accepted only for Beeper homeserver sessions (`matrix.beeper.com`, staging, dev).
