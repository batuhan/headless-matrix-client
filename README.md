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

- `BEEPER_API_LISTEN`: listen address for the HTTP server. Default: `127.0.0.1:23373`
- `BEEPER_ACCESS_TOKEN`: static bearer token for direct API access
- `BEEPER_ALLOW_QUERY_TOKEN`: set to `true` to allow query-token auth for asset serving
- `BEEPER_HOMESERVER_URL`: homeserver URL used for bootstrap login. Default: `https://matrix.beeper.com`
- `BEEPER_LOGIN_TOKEN`: Beeper JWT login token
- `BEEPER_USERNAME`: username for password login
- `BEEPER_PASSWORD`: password for password login
- `BEEPER_RECOVERY_KEY`: recovery key / passphrase for verification

gomuks-compatible overrides:

- `GOMUKS_ROOT`: use a specific gomuks root with `config/`, `data/`, `cache/`, and `logs/`
- `GOMUKS_CONFIG_HOME`
- `GOMUKS_DATA_HOME`
- `GOMUKS_CACHE_HOME`
- `GOMUKS_LOGS_HOME`

When no gomuks override is set, EasyMatrix follows gomuks' normal environment and XDG directory resolution so it can reuse existing gomuks sessions and config.

Rules:

- `BEEPER_USERNAME` and `BEEPER_PASSWORD` must be set together.
- `BEEPER_LOGIN_TOKEN` cannot be combined with `BEEPER_USERNAME` and `BEEPER_PASSWORD`.

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

If `BEEPER_LOGIN_TOKEN` or `BEEPER_USERNAME` / `BEEPER_PASSWORD` are set, plus `BEEPER_RECOVERY_KEY`, the runtime will attempt to bootstrap the session automatically on startup.

Protected API routes require a logged-in Beeper homeserver session.

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
    beeperLoginToken: process.env.BEEPER_LOGIN_TOKEN,
    beeperRecoveryKey: process.env.BEEPER_RECOVERY_KEY,
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
  --login-token "$BEEPER_LOGIN_TOKEN" \
  --recovery-key "$BEEPER_RECOVERY_KEY"
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
