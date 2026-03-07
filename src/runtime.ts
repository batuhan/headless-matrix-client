import { existsSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import type { NativeRequest, NativeResponse } from "./native-fetch.js";

export interface EmbeddedRuntimeOptions {
  nativeLibraryPath?: string;
  accessToken?: string;
  stateDir?: string;
  allowQueryTokenAuth?: boolean;
  beeperHomeserverURL?: string;
  beeperLoginToken?: string;
  beeperUsername?: string;
  beeperPassword?: string;
  beeperRecoveryKey?: string;
}

export interface EmbeddedRuntimeStatus {
  running: boolean;
  stateDir?: string;
  nativeLibraryPath: string;
}

export interface EmbeddedRealtimeConnection extends EventTarget {
  readonly closed: boolean;
  send(payload: string | Record<string, unknown>): void;
  close(): void;
  onmessage: ((event: MessageEvent<string>) => void) | null;
  onclose: ((event: Event) => void) | null;
  onerror: ((event: ErrorEvent) => void) | null;
}

interface RuntimeConfigPayload {
  accessToken?: string;
  stateDir?: string;
  allowQueryTokenAuth?: boolean;
  beeperHomeserverUrl?: string;
  beeperLoginToken?: string;
  beeperUsername?: string;
  beeperPassword?: string;
  beeperRecoveryKey?: string;
}

interface FFIResponsePayload {
  error?: string;
  status?: number;
  statusText?: string;
  headers?: Record<string, string[]>;
  body_base64?: string;
}

type BunFFIModule = typeof import("bun:ffi");

const RUNTIME_FILE_DIR = dirname(fileURLToPath(import.meta.url));
const DEFAULT_REPO_ROOT = resolve(RUNTIME_FILE_DIR, "..");

function assertBunRuntime(): void {
  if (typeof Bun === "undefined") {
    throw new Error("Embedded runtime is Bun-only. Use the HTTP server mode outside Bun.");
  }
}

function defaultNativeLibraryPath(suffix: string): string {
  return resolve(DEFAULT_REPO_ROOT, "bin", `libeasymatrixffi.${suffix}`);
}

function decodeBody(bodyBase64?: string): Uint8Array | undefined {
  if (!bodyBase64) {
    return undefined;
  }
  return new Uint8Array(Buffer.from(bodyBase64, "base64"));
}

function encodeBody(body?: Uint8Array | ArrayBuffer | string | null): string | undefined {
  if (body == null) {
    return undefined;
  }
  if (typeof body === "string") {
    return Buffer.from(body).toString("base64");
  }
  const bytes = body instanceof Uint8Array ? body : new Uint8Array(body);
  return Buffer.from(bytes).toString("base64");
}

function normalizeHeaders(headers?: Record<string, string | readonly string[]>): Record<string, string[]> | undefined {
  if (!headers) {
    return undefined;
  }
  const out: Record<string, string[]> = {};
  for (const [key, value] of Object.entries(headers)) {
    out[key] = typeof value === "string" ? [value] : [...value];
  }
  return out;
}

class RealtimeConnection extends EventTarget implements EmbeddedRealtimeConnection {
  private _onmessage: ((event: MessageEvent<string>) => void) | null = null;
  onclose: ((event: Event) => void) | null = null;
  onerror: ((event: ErrorEvent) => void) | null = null;
  closed = false;
  private readonly backlog: string[] = [];

  constructor(
    private readonly runtime: EmbeddedRuntime,
    private readonly id: bigint,
  ) {
    super();
  }

  get onmessage(): ((event: MessageEvent<string>) => void) | null {
    return this._onmessage;
  }

  set onmessage(value: ((event: MessageEvent<string>) => void) | null) {
    this._onmessage = value;
    if (!value || this.backlog.length === 0) {
      return;
    }
    const pending = this.backlog.splice(0);
    for (const raw of pending) {
      const evt = new MessageEvent("message", { data: raw });
      this.dispatchEvent(evt);
      value(evt);
    }
  }

  send(payload: string | Record<string, unknown>): void {
    if (this.closed) {
      throw new Error("Realtime connection is closed.");
    }
    const raw = typeof payload === "string" ? payload : JSON.stringify(payload);
    this.runtime.sendRealtime(this.id, raw);
  }

  close(): void {
    if (this.closed) {
      return;
    }
    this.closed = true;
    this.runtime.closeRealtime(this.id);
    const evt = new Event("close");
    this.dispatchEvent(evt);
    this.onclose?.(evt);
  }

  dispatchMessage(raw: string): void {
    if (this.closed) {
      return;
    }
    if (!this._onmessage) {
      this.backlog.push(raw);
      return;
    }
    const evt = new MessageEvent("message", { data: raw });
    this.dispatchEvent(evt);
    this._onmessage?.(evt);
  }

  dispatchError(error: Error): void {
    if (this.closed) {
      return;
    }
    const evt = new ErrorEvent("error", { error, message: error.message });
    this.dispatchEvent(evt);
    this.onerror?.(evt);
  }
}

export class EmbeddedRuntime {
  readonly options: Readonly<EmbeddedRuntimeOptions>;

  private handle?: bigint;
  private ffi?: BunFFIModule;
  private lib?: any;
  private realtimeCallback?: any;
  private readonly realtimeConnections = new Map<bigint, RealtimeConnection>();
  private resolvedNativeLibraryPath?: string;

  constructor(options: EmbeddedRuntimeOptions = {}) {
    this.options = options;
  }

  status(): EmbeddedRuntimeStatus {
    return {
      running: this.handle != null,
      stateDir: this.options.stateDir,
      nativeLibraryPath: this.resolvedNativeLibraryPath ?? this.options.nativeLibraryPath ?? "",
    };
  }

  async start(): Promise<void> {
    if (this.handle != null) {
      return;
    }
    assertBunRuntime();
    const ffi = await import("bun:ffi");
    const nativeLibraryPath = this.options.nativeLibraryPath ?? defaultNativeLibraryPath(ffi.suffix);
    if (!existsSync(nativeLibraryPath)) {
      throw new Error(`Native library not found at ${nativeLibraryPath}. Build bin/libeasymatrixffi.${ffi.suffix} first.`);
    }

    const lib: any = ffi.dlopen(nativeLibraryPath, {
      EasyMatrixCreate: {
        args: ["cstring"],
        returns: "u64",
      },
      EasyMatrixStart: {
        args: ["u64"],
        returns: "i32",
      },
      EasyMatrixStop: {
        args: ["u64"],
        returns: "void",
      },
      EasyMatrixDestroy: {
        args: ["u64"],
        returns: "void",
      },
      EasyMatrixHandleRequest: {
        args: ["u64", "cstring"],
        returns: "ptr",
      },
      EasyMatrixRealtimeConnect: {
        args: ["u64", "function"],
        returns: "u64",
      },
      EasyMatrixRealtimeSend: {
        args: ["u64", "u64", "cstring"],
        returns: "ptr",
      },
      EasyMatrixRealtimeClose: {
        args: ["u64", "u64"],
        returns: "void",
      },
      EasyMatrixFreeCString: {
        args: ["ptr"],
        returns: "void",
      },
    });

    const realtimeCallback = new ffi.JSCallback(
      (realtimeID: bigint, payloadPtr: any) => {
        if (!payloadPtr) {
          return;
        }
        try {
          const payload = String(new ffi.CString(payloadPtr));
          const connection = this.realtimeConnections.get(realtimeID);
          connection?.dispatchMessage(payload);
        } finally {
          lib.symbols.EasyMatrixFreeCString(payloadPtr);
        }
      },
      {
        args: ["u64", "ptr"],
        returns: "void",
      },
    );

    const cfgPayload: RuntimeConfigPayload = {
      accessToken: this.options.accessToken,
      stateDir: this.options.stateDir,
      allowQueryTokenAuth: this.options.allowQueryTokenAuth,
      beeperHomeserverUrl: this.options.beeperHomeserverURL,
      beeperLoginToken: this.options.beeperLoginToken,
      beeperUsername: this.options.beeperUsername,
      beeperPassword: this.options.beeperPassword,
      beeperRecoveryKey: this.options.beeperRecoveryKey,
    };
    const rawHandle = lib.symbols.EasyMatrixCreate(JSON.stringify(cfgPayload));
    const handle = rawHandle ? BigInt(rawHandle) : 0n;
    if (!handle) {
      realtimeCallback.close();
      lib.close();
      throw new Error("Failed to create embedded runtime.");
    }
    const startResult = lib.symbols.EasyMatrixStart(handle);
    if (startResult !== 0) {
      lib.symbols.EasyMatrixDestroy(handle);
      realtimeCallback.close();
      lib.close();
      throw new Error(`Embedded runtime failed to start (code ${startResult}).`);
    }

    this.ffi = ffi;
    this.lib = lib;
    this.handle = handle;
    this.realtimeCallback = realtimeCallback;
    this.resolvedNativeLibraryPath = nativeLibraryPath;
  }

  async stop(): Promise<void> {
    if (!this.handle || !this.lib) {
      return;
    }
    for (const connection of this.realtimeConnections.values()) {
      connection.close();
    }
    this.realtimeConnections.clear();
    this.lib.symbols.EasyMatrixStop(this.handle);
    this.lib.symbols.EasyMatrixDestroy(this.handle);
    this.realtimeCallback?.close();
    this.realtimeCallback = undefined;
    this.lib.close();
    this.lib = undefined;
    this.ffi = undefined;
    this.handle = undefined;
  }

  async destroy(): Promise<void> {
    await this.stop();
  }

  async request(request: NativeRequest): Promise<NativeResponse> {
    await this.start();
    const response = this.parseJSONPointer(
      this.lib!.symbols.EasyMatrixHandleRequest(
        this.handle!,
        JSON.stringify({
          method: request.method,
          url: request.url,
          headers: normalizeHeaders(request.headers),
          body_base64: encodeBody(request.body ?? null),
        }),
      ),
    ) as FFIResponsePayload;
    if (response.error) {
      throw new Error(response.error);
    }
    return {
      status: response.status ?? 500,
      statusText: response.statusText,
      headers: response.headers,
      body: decodeBody(response.body_base64),
    };
  }

  async connectRealtime(): Promise<EmbeddedRealtimeConnection> {
    await this.start();
    const rawConnectionID = this.lib!.symbols.EasyMatrixRealtimeConnect(this.handle!, this.realtimeCallback!);
    const connectionID = rawConnectionID ? BigInt(rawConnectionID) : 0n;
    if (!connectionID) {
      throw new Error("Failed to open realtime connection.");
    }
    const connection = new RealtimeConnection(this, connectionID);
    this.realtimeConnections.set(connectionID, connection);
    return connection;
  }

  private parseJSONPointer(pointer: any): unknown {
    if (!pointer) {
      return {};
    }
    try {
      const raw = String(new this.ffi!.CString(pointer));
      return raw ? JSON.parse(raw) : {};
    } finally {
      this.lib!.symbols.EasyMatrixFreeCString(pointer);
    }
  }

  sendRealtime(connectionID: bigint, payload: string): void {
    const responsePtr = this.lib!.symbols.EasyMatrixRealtimeSend(this.handle!, connectionID, payload);
    if (!responsePtr) {
      return;
    }
    const response = this.parseJSONPointer(responsePtr) as { error?: string };
    if (response.error) {
      throw new Error(response.error);
    }
  }

  closeRealtime(connectionID: bigint): void {
    if (!this.handle || !this.lib) {
      return;
    }
    this.realtimeConnections.delete(connectionID);
    this.lib.symbols.EasyMatrixRealtimeClose(this.handle, connectionID);
  }
}

export function createRuntime(options: EmbeddedRuntimeOptions = {}): EmbeddedRuntime {
  return new EmbeddedRuntime(options);
}
