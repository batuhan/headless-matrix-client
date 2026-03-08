#!/usr/bin/env node

import { randomBytes, randomUUID } from "node:crypto";
import { spawn } from "node:child_process";
import { access, mkdtemp, rm } from "node:fs/promises";
import { constants as fsConstants } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import process from "node:process";
import { setTimeout as delay } from "node:timers/promises";
import { fileURLToPath } from "node:url";

import BeeperDesktop from "@beeper/desktop-api";

const REPO_ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const DEFAULT_TIMEOUT_MS = 120_000;
const DEFAULT_REQUEST_TIMEOUT_MS = 20_000;
const DEFAULT_BOT_PORT = 23373;
const DEFAULT_SENDER_PORT = 23374;

function parseArgs(argv) {
  const args = {};
  for (let index = 0; index < argv.length; index += 1) {
    const token = argv[index];
    if (!token.startsWith("--")) {
      throw new Error(`Unexpected argument: ${token}`);
    }
    const [key, inlineValue] = token.slice(2).split("=", 2);
    if (inlineValue !== undefined) {
      args[key] = inlineValue;
      continue;
    }
    const next = argv[index + 1];
    if (!next || next.startsWith("--")) {
      args[key] = true;
      continue;
    }
    args[key] = next;
    index += 1;
  }
  return {
    help: args.help === true,
    creatorScript: readOptionalString(args["creator-script"]) ?? process.env.BEEPER_ACCOUNT_CREATOR,
    botPort: parsePositiveInteger(args["bot-port"], DEFAULT_BOT_PORT, "bot-port"),
    senderPort: parsePositiveInteger(args["sender-port"], DEFAULT_SENDER_PORT, "sender-port"),
    timeoutMs: parsePositiveInteger(args["timeout-ms"], DEFAULT_TIMEOUT_MS, "timeout-ms"),
  };
}

function readOptionalString(value) {
  return typeof value === "string" && value.trim() ? value.trim() : undefined;
}

function parsePositiveInteger(rawValue, fallback, label) {
  if (rawValue === undefined || rawValue === true) {
    return fallback;
  }
  const parsed = Number(rawValue);
  if (!Number.isInteger(parsed) || parsed <= 0) {
    throw new Error(`--${label} must be a positive integer`);
  }
  return parsed;
}

function printHelp() {
  process.stdout.write("Run a staging end-to-end suite against two local EasyMatrix instances.\n\n");
  process.stdout.write("Usage:\n");
  process.stdout.write("  node scripts/e2e-staging.mjs\n");
  process.stdout.write("  node scripts/e2e-staging.mjs --creator-script /abs/path/to/bootstrap.js\n\n");
  process.stdout.write("Environment fallback:\n");
  process.stdout.write("  E2E_BASE_URL, E2E_BOT_LOGIN_TOKEN, E2E_BOT_RECOVERY_KEY, E2E_BOT_ACCESS_TOKEN, E2E_BOT_USER_ID,\n");
  process.stdout.write("  E2E_SENDER_LOGIN_TOKEN, E2E_SENDER_RECOVERY_KEY, E2E_SENDER_ACCESS_TOKEN, E2E_SENDER_USER_ID\n");
}

async function ensurePathExists(filePath) {
  try {
    await access(filePath, fsConstants.R_OK);
    return true;
  } catch {
    return false;
  }
}

async function resolveCreatorScript(explicitPath) {
  const candidates = [explicitPath].filter(Boolean);

  for (const candidate of candidates) {
    if (await ensurePathExists(candidate)) {
      return candidate;
    }
  }
  return null;
}

async function runCommand(command, args, options = {}) {
  return await new Promise((resolve, reject) => {
    const child = spawn(command, args, {
      cwd: options.cwd ?? REPO_ROOT,
      env: { ...process.env, ...(options.env ?? {}) },
      stdio: ["ignore", "pipe", "pipe"],
    });

    const stdout = [];
    const stderr = [];
    child.stdout.on("data", (chunk) => stdout.push(Buffer.from(chunk)));
    child.stderr.on("data", (chunk) => stderr.push(Buffer.from(chunk)));
    child.on("error", reject);
    child.on("exit", (code, signal) => {
      const stdoutText = Buffer.concat(stdout).toString("utf8");
      const stderrText = Buffer.concat(stderr).toString("utf8");
      if (code === 0) {
        resolve({ stdout: stdoutText, stderr: stderrText });
        return;
      }
      reject(
        new Error(
          `${command} ${args.join(" ")} failed with ${signal ? `signal ${signal}` : `exit code ${code}`}\n${stderrText || stdoutText}`,
        ),
      );
    });
  });
}

async function createAccount(creatorScript) {
  const { stdout } = await runCommand("node", [creatorScript, "--format", "json"]);
  return JSON.parse(stdout);
}

function accountFromEnv(prefix) {
  const baseUrl = readOptionalString(process.env.E2E_BASE_URL);
  const loginToken = readOptionalString(process.env[`${prefix}_LOGIN_TOKEN`]);
  const recoveryKey = readOptionalString(process.env[`${prefix}_RECOVERY_KEY`]);
  const accessToken = readOptionalString(process.env[`${prefix}_ACCESS_TOKEN`]);
  const userId = readOptionalString(process.env[`${prefix}_USER_ID`]);
  if (!baseUrl || !loginToken || !recoveryKey || !accessToken || !userId) {
    return null;
  }
  return { baseUrl, loginToken, recoveryKey, accessToken, userId };
}

async function loadAccounts(options) {
  const envBot = accountFromEnv("E2E_BOT");
  const envSender = accountFromEnv("E2E_SENDER");
  if (envBot && envSender) {
    return { bot: envBot, sender: envSender };
  }

  const creatorScript = await resolveCreatorScript(options.creatorScript);
  if (!creatorScript) {
    throw new Error("No bootstrap script configured. Set BEEPER_ACCOUNT_CREATOR or pass --creator-script.");
  }

  process.stdout.write("Using configured account bootstrap script\n");
  const [bot, sender] = await Promise.all([createAccount(creatorScript), createAccount(creatorScript)]);
  return { bot, sender };
}

function prefixedPipe(stream, prefix) {
  stream.on("data", (chunk) => {
    const text = chunk.toString("utf8");
    for (const line of text.split(/\r?\n/)) {
      if (line) {
        process.stderr.write(`[${prefix}] ${line}\n`);
      }
    }
  });
}

async function fetchJSON(url, init = {}) {
  const response = await fetch(url, {
    ...init,
    signal: init.signal ?? AbortSignal.timeout(DEFAULT_REQUEST_TIMEOUT_MS),
  });
  const text = await response.text();
  let payload = null;
  try {
    payload = text ? JSON.parse(text) : null;
  } catch {
    payload = text ? { raw: text } : null;
  }
  if (!response.ok) {
    throw new Error(payload?.message ?? payload?.error ?? payload?.raw ?? `HTTP ${response.status}`);
  }
  return payload;
}

async function waitFor(check, timeoutMs, label) {
  const startedAt = Date.now();
  let lastError = null;
  while (Date.now() - startedAt < timeoutMs) {
    try {
      return await check();
    } catch (error) {
      lastError = error;
      await delay(500);
    }
  }
  throw new Error(`Timed out waiting for ${label}: ${lastError instanceof Error ? lastError.message : String(lastError)}`);
}

async function startServer(name, port, account, rootDir) {
  const stateDir = path.join(rootDir, name);
  const accessToken = `easymatrix-${name}-${randomBytes(12).toString("hex")}`;
  const child = spawn("go", ["run", "./cmd/server"], {
    cwd: REPO_ROOT,
    env: {
      ...process.env,
      BEEPER_ACCESS_TOKEN: accessToken,
      BEEPER_ALLOW_QUERY_TOKEN: "true",
      BEEPER_API_LISTEN: `127.0.0.1:${port}`,
      GOMUKS_ROOT: stateDir,
      BEEPER_HOMESERVER_URL: account.baseUrl,
      BEEPER_LOGIN_TOKEN: account.loginToken,
      BEEPER_RECOVERY_KEY: account.recoveryKey,
    },
    stdio: ["ignore", "pipe", "pipe"],
  });
  prefixedPipe(child.stdout, name);
  prefixedPipe(child.stderr, name);

  const baseURL = `http://127.0.0.1:${port}`;
  const client = new BeeperDesktop({
    baseURL,
    accessToken,
    maxRetries: 0,
    timeout: DEFAULT_REQUEST_TIMEOUT_MS,
  });

  child.on("exit", (code, signal) => {
    if (code !== 0) {
      process.stderr.write(`[${name}] exited with ${signal ? `signal ${signal}` : `code ${code}`}\n`);
    }
  });

  await waitFor(async () => {
    const state = await fetchJSON(`${baseURL}/manage/state`);
    const clientState = state?.client_state ?? {};
    if (!clientState.is_logged_in || !clientState.is_verified || !state?.is_beeper_homeserver) {
      throw new Error("session not ready yet");
    }
    return state;
  }, DEFAULT_TIMEOUT_MS, `${name} server readiness`);

  return { child, client, baseURL, accessToken };
}

async function stopServer(server) {
  if (!server?.child || server.child.exitCode !== null) {
    return;
  }
  server.child.kill("SIGTERM");
  await Promise.race([
    new Promise((resolve) => server.child.once("exit", resolve)),
    delay(10_000).then(() => {
      server.child.kill("SIGKILL");
    }),
  ]);
}

async function matrixRequest(account, method, requestPath, body) {
  return await fetchJSON(`${account.baseUrl}/_matrix/client/v3${requestPath}`, {
    method,
    headers: {
      authorization: `Bearer ${account.accessToken}`,
      "content-type": "application/json",
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
}

async function createJoinedRoom(botAccount, senderAccount, roomName) {
  const created = await matrixRequest(botAccount, "POST", "/createRoom", {
    preset: "private_chat",
    name: roomName,
    invite: [senderAccount.userId],
  });
  await matrixRequest(senderAccount, "POST", `/rooms/${encodeURIComponent(created.room_id)}/join`, {});
  return created.room_id;
}

function pageItems(page) {
  return Array.isArray(page?.items) ? page.items : [];
}

async function waitForChat(client, chatID, timeoutMs) {
  return await waitFor(async () => {
    const chat = await client.chats.retrieve(chatID);
    if (!chat?.id) {
      throw new Error(`chat ${chatID} not visible yet`);
    }
    return chat;
  }, timeoutMs, `chat ${chatID}`);
}

async function waitForMessage(client, chatID, predicate, timeoutMs, label) {
  return await waitFor(async () => {
    const page = await client.messages.list(chatID, { limit: 50 });
    const match = pageItems(page).find(predicate);
    if (!match) {
      throw new Error(`${label} not visible yet`);
    }
    return match;
  }, timeoutMs, label);
}

async function waitForCondition(check, timeoutMs, label) {
  return await waitFor(async () => {
    const value = await check();
    if (!value) {
      throw new Error(`${label} not satisfied yet`);
    }
    return value;
  }, timeoutMs, label);
}

class WSRecorder {
  constructor(baseURL, accessToken) {
    this.url = `${baseURL.replace(/^http/, "ws")}/v1/ws?dangerouslyUseTokenInQuery=${encodeURIComponent(accessToken)}`;
    this.messages = [];
    this.waiters = [];
    this.ws = null;
  }

  async connect(timeoutMs) {
    this.ws = new WebSocket(this.url);
    this.ws.addEventListener("message", async (event) => {
      const raw =
        typeof event.data === "string"
          ? event.data
          : typeof event.data?.text === "function"
            ? await event.data.text()
            : Buffer.from(event.data).toString("utf8");
      const payload = JSON.parse(raw);
      this.messages.push(payload);
      this.resolveWaiters();
    });
    await this.waitFor((message) => message.type === "ready", timeoutMs, "websocket ready");
  }

  subscribeAll() {
    this.ws.send(JSON.stringify({ type: "subscriptions.set", requestID: "sub-all", chatIDs: ["*"] }));
    return this.waitFor(
      (message) => message.type === "subscriptions.updated" && message.requestID === "sub-all",
      DEFAULT_REQUEST_TIMEOUT_MS,
      "websocket subscriptions update",
    );
  }

  async waitFor(predicate, timeoutMs, label) {
    const existing = this.messages.find(predicate);
    if (existing) {
      return existing;
    }
    return await new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.waiters = this.waiters.filter((entry) => entry.resolve !== resolve);
        reject(new Error(`Timed out waiting for ${label}`));
      }, timeoutMs);
      this.waiters.push({ predicate, resolve, reject, timer });
    });
  }

  resolveWaiters() {
    this.waiters = this.waiters.filter((entry) => {
      const match = this.messages.find(entry.predicate);
      if (!match) {
        return true;
      }
      clearTimeout(entry.timer);
      entry.resolve(match);
      return false;
    });
  }

  close() {
    if (this.ws) {
      this.ws.close();
    }
  }
}

function assert(condition, message) {
  if (!condition) {
    throw new Error(message);
  }
}

async function runSuite(accounts, servers, timeoutMs) {
  const ws = new WSRecorder(servers.sender.baseURL, servers.sender.accessToken);
  await ws.connect(timeoutMs);
  await ws.subscribeAll();

  const botAccounts = await servers.bot.client.accounts.list();
  const senderAccounts = await servers.sender.client.accounts.list();
  const botAccountID = botAccounts?.[0]?.accountID;
  const senderAccountID = senderAccounts?.[0]?.accountID;
  assert(botAccountID, "bot account list returned no account");
  assert(senderAccountID, "sender account list returned no account");

  const mainRoomName = `EasyMatrix E2E ${randomUUID().slice(0, 8)}`;
  const mainChatID = await createJoinedRoom(accounts.bot, accounts.sender, mainRoomName);
  await waitForChat(servers.bot.client, mainChatID, timeoutMs);
  await waitForChat(servers.sender.client, mainChatID, timeoutMs);

  const contacts = await servers.bot.client.accounts.contacts.search(botAccountID, {
    query: accounts.sender.userId.split(":")[0].slice(1),
  });
  assert(Array.isArray(contacts?.items) && contacts.items.some((item) => item.id === accounts.sender.userId), "contacts search did not find sender");

  const createdChat = await servers.bot.client.chats.create({
    accountID: botAccountID,
    participantIDs: [accounts.sender.userId],
    type: "group",
    title: `EasyMatrix Created ${randomUUID().slice(0, 6)}`,
  });
  assert(createdChat?.chatID, "chat creation did not return chatID");
  await matrixRequest(accounts.sender, "POST", `/rooms/${encodeURIComponent(createdChat.chatID)}/join`, {});
  await waitForChat(servers.bot.client, createdChat.chatID, timeoutMs);
  await waitForChat(servers.sender.client, createdChat.chatID, timeoutMs);

  await servers.bot.client.chats.archive(createdChat.chatID, { archived: true });
  await waitForCondition(async () => {
    const chat = await servers.bot.client.chats.retrieve(createdChat.chatID);
    return chat?.isArchived === true;
  }, timeoutMs, "archived chat state");
  await servers.bot.client.chats.archive(createdChat.chatID, { archived: false });

  const remindAtMs = Date.now() + 30 * 60 * 1000;
  await servers.bot.client.chats.reminders.create(createdChat.chatID, {
    reminder: { remindAtMs, dismissOnIncomingMessage: true },
  });
  const reminderData = await matrixRequest(
    accounts.bot,
    "GET",
    `/user/${encodeURIComponent(accounts.bot.userId)}/rooms/${encodeURIComponent(createdChat.chatID)}/account_data/com.beeper.chats.reminder`,
  );
  assert(reminderData?.remind_at_ms === remindAtMs, "room reminder account data was not written");
  await servers.bot.client.chats.reminders.delete(createdChat.chatID);

  const chatSearch = await servers.bot.client.chats.search({ query: mainRoomName, limit: 10 });
  assert(pageItems(chatSearch).some((chat) => chat.id === mainChatID), "chat search did not return main room");

  const messageText = `hello from easymatrix ${randomUUID().slice(0, 8)}`;
  await servers.bot.client.messages.send(mainChatID, { text: messageText });
  const senderWSMessage = await ws.waitFor(
    (message) =>
      message.type === "message.upserted" &&
      message.chatID === mainChatID &&
      Array.isArray(message.entries) &&
      message.entries.some((entry) => entry.text === messageText),
    timeoutMs,
    "sender websocket message upsert",
  );
  assert(senderWSMessage.entries.length > 0, "websocket message event did not include entries");

  const botMessage = await waitForMessage(
    servers.bot.client,
    mainChatID,
    (message) => message.text === messageText,
    timeoutMs,
    "bot message visibility",
  );

  const editedText = `${messageText} edited`;
  await servers.bot.client.put(`/v1/chats/${encodeURIComponent(mainChatID)}/messages/${encodeURIComponent(botMessage.id)}`, {
    body: { text: editedText },
  });
  await waitForMessage(
    servers.sender.client,
    mainChatID,
    (message) => message.id === botMessage.id && message.text === editedText,
    timeoutMs,
    "edited message visibility",
  );

  await servers.sender.client.post(`/v1/chats/${encodeURIComponent(mainChatID)}/messages/${encodeURIComponent(botMessage.id)}/reactions`, {
    body: { reactionKey: "🔥" },
  });
  await waitForMessage(
    servers.bot.client,
    mainChatID,
    (message) => message.id === botMessage.id && Array.isArray(message.reactions) && message.reactions.some((reaction) => reaction.reactionKey === "🔥"),
    timeoutMs,
    "reaction visibility",
  );
  await servers.sender.client.delete(`/v1/chats/${encodeURIComponent(mainChatID)}/messages/${encodeURIComponent(botMessage.id)}/reactions`, {
    query: { reactionKey: "🔥" },
  });

  const uploadContent = Buffer.from(`easymatrix-asset-${randomUUID()}`, "utf8").toString("base64");
  const upload = await servers.bot.client.post("/v1/assets/upload/base64", {
    body: {
      content: uploadContent,
      fileName: "e2e.txt",
      mimeType: "text/plain",
    },
  });
  assert(upload?.uploadID, "asset upload did not return uploadID");
  await servers.bot.client.post(`/v1/chats/${encodeURIComponent(mainChatID)}/messages`, {
    body: {
      text: "attachment message",
      attachment: { uploadID: upload.uploadID },
    },
  });
  const attachmentMessage = await waitForMessage(
    servers.sender.client,
    mainChatID,
    (message) => message.text === "attachment message" && Array.isArray(message.attachments) && message.attachments.length > 0,
    timeoutMs,
    "attachment message visibility",
  );
  const attachmentURL = attachmentMessage.attachments?.[0]?.srcURL;
  assert(attachmentURL, "attachment message did not expose attachment srcURL");
  const downloaded = await servers.sender.client.assets.download({ url: attachmentURL });
  assert(downloaded?.srcURL?.startsWith("file://"), "asset download did not return a local file URL");

  const servedResponse = await fetch(
    `${servers.sender.baseURL}/v1/assets/serve?url=${encodeURIComponent(attachmentURL)}&dangerouslyUseTokenInQuery=${encodeURIComponent(servers.sender.accessToken)}`,
    { signal: AbortSignal.timeout(DEFAULT_REQUEST_TIMEOUT_MS) },
  );
  const servedText = await servedResponse.text();
  assert(servedText.startsWith("easymatrix-asset-"), "served asset content mismatch");

  await waitForCondition(async () => {
    const messageSearch = await servers.sender.client.messages.search({ query: "edited", chatIDs: [mainChatID], limit: 10 });
    return pageItems(messageSearch).some((message) => message.id === botMessage.id);
  }, timeoutMs, "message search indexing");

  await waitForCondition(async () => {
    const unifiedSearch = await servers.sender.client.search({ query: "edited" });
    return unifiedSearch?.results?.messages?.items?.some((message) => message.id === botMessage.id);
  }, timeoutMs, "top-level search indexing");

  const focus = await servers.bot.client.focus({ chatID: mainChatID, draftText: "draft-check" });
  assert(focus?.success === true, "focus endpoint did not succeed");

  ws.close();

  return {
    botAccountID,
    senderAccountID,
    mainChatID,
    createdChatID: createdChat.chatID,
    editedMessageID: botMessage.id,
    attachmentURL,
  };
}

async function main() {
  const options = parseArgs(process.argv.slice(2));
  if (options.help) {
    printHelp();
    return;
  }

  const accounts = await loadAccounts(options);
  const tempRoot = await mkdtemp(path.join(tmpdir(), "easymatrix-e2e-"));
  const servers = {};

  try {
    servers.bot = await startServer("bot", options.botPort, accounts.bot, tempRoot);
    servers.sender = await startServer("sender", options.senderPort, accounts.sender, tempRoot);
    const result = await runSuite(accounts, servers, options.timeoutMs);
    process.stdout.write(`${JSON.stringify({ ok: true, result }, null, 2)}\n`);
  } finally {
    await Promise.allSettled([stopServer(servers.bot), stopServer(servers.sender)]);
    await rm(tempRoot, { recursive: true, force: true });
  }
}

main().catch((error) => {
  const message = error instanceof Error ? error.stack ?? error.message : String(error);
  process.stderr.write(`${message}\n`);
  process.exit(1);
});
