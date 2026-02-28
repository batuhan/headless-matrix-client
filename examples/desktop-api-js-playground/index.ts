import BeeperDesktop from "@beeper/desktop-api";

type RunRequest = {
  baseURL?: string;
  accessToken?: string;
  operation:
    | "info"
    | "accounts"
    | "chats_list"
    | "chats_search"
    | "messages_search"
    | "messages_list"
    | "contacts_search"
    | "focus"
    | "send_message"
    | "raw_get";
  params?: Record<string, unknown>;
};

const host = process.env.HOST ?? "127.0.0.1";
const port = Number(process.env.PORT ?? 4791);

function json(data: unknown, status = 200) {
  return new Response(JSON.stringify(data, null, 2), {
    status,
    headers: { "content-type": "application/json; charset=utf-8" },
  });
}

function clientFrom(req: RunRequest) {
  return new BeeperDesktop({
    baseURL: req.baseURL?.trim() || "http://127.0.0.1:23373",
    accessToken: req.accessToken?.trim() || undefined,
    maxRetries: 0,
    timeout: 20_000,
  });
}

function pageToJSON(page: any) {
  return {
    items: page?.items ?? [],
    hasMore: page?.hasMore ?? false,
    oldestCursor: page?.oldestCursor ?? null,
    newestCursor: page?.newestCursor ?? null,
  };
}

async function runOperation(input: RunRequest) {
  const client = clientFrom(input);
  const params = input.params ?? {};

  switch (input.operation) {
    case "info":
      return await client.get("/v1/info");
    case "accounts":
      return await client.accounts.list();
    case "chats_list":
      return pageToJSON(await client.chats.list(params as any));
    case "chats_search":
      return pageToJSON(await client.chats.search(params as any));
    case "messages_search":
      return pageToJSON(await client.messages.search(params as any));
    case "messages_list": {
      const chatID = String(params.chatID ?? "").trim();
      if (!chatID) {
        throw new Error("messages_list requires params.chatID");
      }
      const query = { ...params };
      delete (query as any).chatID;
      return pageToJSON(await client.messages.list(chatID, query as any));
    }
    case "contacts_search": {
      const accountID = String(params.accountID ?? "").trim();
      const query = String(params.query ?? "").trim();
      if (!accountID || !query) {
        throw new Error("contacts_search requires params.accountID and params.query");
      }
      return await client.accounts.contacts.search(accountID, { query });
    }
    case "focus":
      return await client.focus(params as any);
    case "send_message": {
      const chatID = String(params.chatID ?? "").trim();
      if (!chatID) {
        throw new Error("send_message requires params.chatID");
      }
      const body = { ...params };
      delete (body as any).chatID;
      return await client.messages.send(chatID, body as any);
    }
    case "raw_get": {
      const path = String(params.path ?? "").trim();
      if (!path.startsWith("/")) {
        throw new Error("raw_get requires params.path starting with /");
      }
      return await client.get(path);
    }
    default:
      throw new Error(`Unsupported operation: ${(input as any).operation}`);
  }
}

Bun.serve({
  hostname: host,
  port,
  async fetch(req) {
    const url = new URL(req.url);

    if (url.pathname === "/") {
      return new Response(Bun.file("public/index.html"));
    }

    if (url.pathname === "/api/run" && req.method === "POST") {
      try {
        const body = (await req.json()) as RunRequest;
        if (!body?.operation) {
          return json({ error: "operation is required" }, 400);
        }
        const result = await runOperation(body);
        return json({ ok: true, result });
      } catch (err: any) {
        return json(
          {
            ok: false,
            error: err?.message ?? String(err),
            status: err?.status ?? null,
            code: err?.code ?? null,
            details: err?.error ?? null,
          },
          500,
        );
      }
    }

    return new Response("Not Found", { status: 404 });
  },
});

console.log(`desktop-api-js playground listening on http://${host}:${port}`);
