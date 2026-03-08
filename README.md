# EasyMatrix

EasyMatrix is a Beeper Desktop API-compatible server built on top of `gomuks`.

It has two modes:

- A standalone Go HTTP server that exposes the Desktop API route surface.
- A Bun-only embedded runtime that loads a native shared library and lets JS code talk to the same server logic in-process.

The repo is still experimental. The public API is not treated as stable yet.

## What It Covers

- `v1` Desktop API routes for accounts, chats, messages, assets, contacts, search, focus, info, and websocket events
- OAuth discovery and token endpoints used by Desktop API clients
- A `/manage` UI for homeserver discovery, login, and recovery-key verification
- Embedded fetch/runtime/realtime helpers for Bun
- Type alignment with `@beeper/desktop-api` on the JS side and `desktop-api-go` on the Go side

## Repo Layout

- [cmd/server/main.go](/Users/batuhan/Projects/labs/easymatrix/cmd/server/main.go): standalone HTTP server
- [internal/server](/Users/batuhan/Projects/labs/easymatrix/internal/server): Desktop API route handlers and websocket implementation
- [internal/gomuksruntime](/Users/batuhan/Projects/labs/easymatrix/internal/gomuksruntime): gomuks bootstrap and JSON-command helpers
- [src/index.ts](/Users/batuhan/Projects/labs/easymatrix/src/index.ts): JS entrypoint
- [src/client.ts](/Users/batuhan/Projects/labs/easymatrix/src/client.ts): embedded SDK/fetch helpers
- [src/runtime.ts](/Users/batuhan/Projects/labs/easymatrix/src/runtime.ts): Bun native runtime bridge
- [src/realtime.ts](/Users/batuhan/Projects/labs/easymatrix/src/realtime.ts): realtime adapter for embedded mode

## Server Mode

Run the local server:

```bash
go run ./cmd/server
```

Default listen address:

```text
127.0.0.1:23373
```

Once running:

- `GET /v1/info` returns server, endpoint, and platform metadata.
- `GET /manage` opens the local login/verification UI.
- `GET /v1/spec` redirects to the public Desktop API docs.

## Environment

`.env` is loaded automatically from the current working directory if present.

- `MATRIX_API_LISTEN`: listen address for the HTTP server. Default: `127.0.0.1:23373`
- `PORT`: when `MATRIX_API_LISTEN` is unset, EasyMatrix will listen on `0.0.0.0:$PORT` for Railway-style runtimes
- `MATRIX_ACCESS_TOKEN`: static bearer token for direct API access
- `MATRIX_ALLOW_QUERY_TOKEN`: set to `true` to allow query-token auth for asset serving
- `EASYMATRIX_MANAGE_SECRET`: optional secret required to access `/manage`. Open `/manage?secret=...` once to establish the browser session.
- `MATRIX_HOMESERVER_URL`: homeserver URL used for bootstrap login. Default: `https://matrix.beeper.com`
- `MATRIX_LOGIN_TOKEN`: Matrix JWT login token
- `MATRIX_USERNAME`: username for password login
- `MATRIX_PASSWORD`: password for password login
- `MATRIX_RECOVERY_KEY`: recovery key / passphrase for verification

gomuks-compatible overrides:

- `GOMUKS_ROOT`: use a specific gomuks root with `config/`, `data/`, `cache/`, and `logs/`
- `MATRIX_STATE_DIR`: alias for `GOMUKS_ROOT`
- `GOMUKS_CONFIG_HOME`
- `GOMUKS_DATA_HOME`
- `GOMUKS_CACHE_HOME`
- `GOMUKS_LOGS_HOME`

Railway volume integration:

- `RAILWAY_VOLUME_MOUNT_PATH`: when present and no explicit gomuks root is set, EasyMatrix uses `${RAILWAY_VOLUME_MOUNT_PATH}/gomuks`

When no gomuks override is set, EasyMatrix follows gomuks' normal environment and XDG directory resolution so it can reuse existing gomuks sessions and config.

Rules:

- `MATRIX_USERNAME` and `MATRIX_PASSWORD` must be set together.
- `MATRIX_LOGIN_TOKEN` cannot be combined with `MATRIX_USERNAME` and `MATRIX_PASSWORD`.

## Login and Verification

The easiest way to create a usable Beeper session is the built-in UI:

```text
http://127.0.0.1:23373/manage
```

Supported flows:

- homeserver discovery from Matrix user ID
- password login
- custom login payloads
- Beeper email-code login helpers
- recovery-key verification

If `MATRIX_LOGIN_TOKEN` or `MATRIX_USERNAME` / `MATRIX_PASSWORD` are set, plus `MATRIX_RECOVERY_KEY`, the runtime will attempt to bootstrap the session automatically on startup.

Protected API routes require a logged-in Beeper homeserver session.

## Railway

This repo includes a root [Dockerfile](/Users/batuhan/Projects/labs/easymatrix/Dockerfile) and [railway.toml](/Users/batuhan/Projects/labs/easymatrix/railway.toml), so Railway builds a Go-only container from `./cmd/server` and healthchecks `GET /v1/info`. Bun is not used in the Railway deploy image.

Deploy button to enable after publishing the template:

```md
[![Deploy on Railway](https://railway.com/button.svg)](REPLACE_WITH_YOUR_TEMPLATE_URL)
```

For a template, configure the Railway service like this in the template composer:

- Source repo: this repository
- Public networking: enabled
- Volume: attached to the service at `/data`
- Required variables: `MATRIX_HOMESERVER_URL`, `MATRIX_USERNAME`, `MATRIX_PASSWORD`
- Required secret: `EASYMATRIX_MANAGE_SECRET`

Recommended template defaults:

- `MATRIX_ALLOW_QUERY_TOKEN=false`
- `MATRIX_ACCESS_TOKEN`: set this to a generated random secret in the template before publishing so deployers do not need to provide it manually
- Optional `MATRIX_RECOVERY_KEY` for fully automatic bootstrap, otherwise finish setup in `/manage`

With that setup, EasyMatrix will automatically persist gomuks state under `/data/gomuks`.

Suggested template variable setup:

- `MATRIX_HOMESERVER_URL`: required, default `https://matrix.beeper.com`
- `MATRIX_USERNAME`: required, no default
- `MATRIX_PASSWORD`: required, no default
- `EASYMATRIX_MANAGE_SECRET`: required, use `${{ secret(32) }}`
- `MATRIX_ACCESS_TOKEN`: optional for deployers, set template default to `${{ secret(32) }}`
- `MATRIX_ALLOW_QUERY_TOKEN`: default `false`
- `MATRIX_RECOVERY_KEY`: optional

Suggested publish flow:

1. Create a Railway project from this repo.
2. Open template creation from the project settings or workspace templates page.
3. In the template composer, attach a volume at `/data`.
4. Mark the required variables above in the Variables tab.
5. Publish the template and copy the generated template URL.
6. Replace `REPLACE_WITH_YOUR_TEMPLATE_URL` in the button snippet above.

## JS Package

Package name:

```bash
@bi/easymatrix
```

Main exports today:

- `run`
- `serveHTTP`
- `createEmbeddedFetch`
- `createRuntimeHandle`
- `createEmbeddedRealtime`
- `withEmbedded`
- `createRuntime`
- `EmbeddedRuntime`
- `BeeperDesktop`

The package also re-exports embedded bridge command/event types and native fetch helpers from [src/index.ts](/Users/batuhan/Projects/labs/easymatrix/src/index.ts).

## Embedded Runtime

The embedded runtime is Bun-only.

Build the native library and JS package first:

```bash
npm run build
```

That produces JS output in `dist/` and packages the native shared library into `dist/native`.

You can also point the runtime at a custom shared library path with:

```bash
EASYMATRIX_NATIVE_LIBRARY_PATH=/absolute/path/to/libeasymatrixffi.dylib
```

### Embedded SDK Example

```ts
import { BeeperDesktop, withEmbedded } from "@bi/easymatrix";

const embedded = await withEmbedded(BeeperDesktop, {
  runtime: {
    accessToken: "local-dev-token",
    stateDir: "/tmp/easymatrix",
    beeperHomeserverURL: "https://matrix.beeper.com",
    beeperLoginToken: process.env.MATRIX_LOGIN_TOKEN,
    beeperRecoveryKey: process.env.MATRIX_RECOVERY_KEY,
  },
});

const accounts = await embedded.sdk.accounts.list();
console.log(accounts);

await embedded.close();
```

### Embedded Runtime Handle Example

```ts
import {
  EMBEDDED_RUNTIME_INFO,
  createEmbeddedRealtime,
  createRuntimeHandle,
} from "@bi/easymatrix";

const runtime = await createRuntimeHandle({
  runtime: {
    accessToken: "local-dev-token",
  },
});

const info = await runtime.invoke({ type: EMBEDDED_RUNTIME_INFO });
console.log(info);

const realtime = await createEmbeddedRealtime({ runtime: runtime.runtime });
realtime.setSubscriptions(["*"]);

realtime.addEventListener("chat.upserted", (event) => {
  console.log((event as CustomEvent).detail);
});
```

### Embedded Fetch Example

```ts
import { BeeperDesktop, createEmbeddedFetch } from "@bi/easymatrix";

const embedded = await createEmbeddedFetch({
  runtime: {
    accessToken: "local-dev-token",
  },
});

const sdk = new BeeperDesktop({
  accessToken: "local-dev-token",
  baseURL: embedded.baseURL,
  fetch: embedded.fetch,
});

const info = await sdk.info.get();
console.log(info);

await embedded.close();
```

## Realtime

Server mode exposes websocket events at:

```text
GET /v1/ws
```

Embedded mode exposes the same domain events through `createEmbeddedRealtime`.

Typical event families:

- `ready`
- `subscriptions.updated`
- `chat.upserted`
- `chat.deleted`
- `message.upserted`
- `message.deleted`
- `error`

## CLI

The package ships a small CLI wrapper:

```bash
npx @bi/easymatrix
```

If run inside the repo it defaults to:

```bash
go run ./cmd/server
```

Outside the repo it falls back to:

```bash
go run github.com/batuhan/easymatrix/cmd/server@latest
```

There is also a helper script for driving the `/manage` login flow from the terminal:

```bash
node ./scripts/easymatrix-login.mjs \
  --base-url http://127.0.0.1:23373 \
  --homeserver-url https://matrix.beeper.com \
  --login-token "$MATRIX_LOGIN_TOKEN" \
  --recovery-key "$MATRIX_RECOVERY_KEY"
```

## Development

Useful commands:

```bash
npm run build
npm run build:native
npm run typecheck
npm run test:types
go test ./...
```

## Notes

- EasyMatrix embeds `go.mau.fi/gomuks` as a library; it does not shell out to a separate gomuks process in normal server mode.
- Account discovery for local bridges is inferred from `com.beeper.local_bridge_state`.
- The implementation is intentionally Beeper-specific and only accepts Beeper homeserver sessions.
- The JS package and route surface may still change while the project is being shaped.
