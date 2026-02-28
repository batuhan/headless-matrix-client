# desktop-api-js-playground

Tiny Bun + browser UI playground for testing your local Beeper Desktop API-compatible server with the official JS SDK (`@beeper/desktop-api`, from the `desktop-api-js` repo).

## Install

```bash
bun install
```

## Run

```bash
bun run dev
```

Then open:

- `http://127.0.0.1:4791`

## What it tests

- `GET /v1/info`
- `accounts.list`
- `chats.list`
- `chats.search`
- `messages.search`
- custom SDK calls via `POST /api/run`

You can set base URL and access token in the page.
