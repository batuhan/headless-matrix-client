import type {
  EmbeddedRealtimeCommand,
  EmbeddedRealtimeEvent,
  EmbeddedRealtimeEventType,
  EmbeddedSubscriptionsSetCommand,
} from "./bridge.js";
import { EmbeddedRuntime, type EmbeddedRealtimeConnection } from "./runtime.js";
import { normalizeRuntime, type RuntimeInput } from "./runtime-input.js";

export interface CreateEmbeddedRealtimeOptions {
  runtime?: RuntimeInput;
  autoStartRuntime?: boolean;
  connection?: EmbeddedRealtimeConnection;
}

export interface WaitForEmbeddedEventOptions<TEvent extends EmbeddedRealtimeEvent> {
  signal?: AbortSignal;
  predicate?: (event: TEvent) => boolean;
}

export interface EmbeddedRealtimeAdapter extends EventTarget {
  readonly closed: boolean;
  readonly connection: EmbeddedRealtimeConnection;
  readonly runtime?: EmbeddedRuntime;
  send(command: EmbeddedRealtimeCommand): void;
  setSubscriptions(chatIDs: string[], requestID?: string): void;
  waitFor<TType extends EmbeddedRealtimeEventType>(
    type: TType,
    options?: WaitForEmbeddedEventOptions<Extract<EmbeddedRealtimeEvent, { type: TType }>>,
  ): Promise<Extract<EmbeddedRealtimeEvent, { type: TType }>>;
  close(): Promise<void>;
}

function abortError(): Error {
  return new DOMException("The operation was aborted", "AbortError");
}

function parseRealtimeEvent(raw: string): EmbeddedRealtimeEvent {
  const parsed = JSON.parse(raw) as EmbeddedRealtimeEvent;
  if (!parsed || typeof parsed !== "object" || typeof parsed.type !== "string") {
    throw new Error("Embedded realtime payload is missing a type.");
  }
  return parsed;
}

class DefaultEmbeddedRealtimeAdapter extends EventTarget implements EmbeddedRealtimeAdapter {
  private _closed = false;

  constructor(
    readonly connection: EmbeddedRealtimeConnection,
    readonly runtime: EmbeddedRuntime | undefined,
    private readonly closeOwnedRuntime: (() => Promise<void>) | undefined,
  ) {
    super();

    connection.addEventListener("message", (event) => {
      try {
        const message = event as MessageEvent<string>;
        const parsed = parseRealtimeEvent(message.data);
        this.dispatchTypedEvent(parsed);
      } catch (error) {
        this.dispatchRuntimeError(error instanceof Error ? error : new Error(String(error)));
      }
    });

    connection.addEventListener("close", () => {
      this._closed = true;
      this.dispatchEvent(new Event("close"));
    });

    connection.addEventListener("error", (event) => {
      this.dispatchEvent(event);
    });
  }

  get closed(): boolean {
    return this._closed || this.connection.closed;
  }

  send(command: EmbeddedRealtimeCommand): void {
    if (this.closed) {
      throw new Error("Embedded realtime adapter is closed.");
    }
    this.connection.send(JSON.stringify(command));
  }

  setSubscriptions(chatIDs: string[], requestID?: string): void {
    const command: EmbeddedSubscriptionsSetCommand = {
      type: "subscriptions.set",
      chatIDs,
      requestID,
    };
    this.send(command);
  }

  setPresence(active: boolean, requestID?: string): void {
    this.send({
      type: "presence.set",
      active,
      requestID,
    });
  }

  waitFor<TType extends EmbeddedRealtimeEventType>(
    type: TType,
    options: WaitForEmbeddedEventOptions<Extract<EmbeddedRealtimeEvent, { type: TType }>> = {},
  ): Promise<Extract<EmbeddedRealtimeEvent, { type: TType }>> {
    const { signal, predicate } = options;
    if (signal?.aborted) {
      return Promise.reject(abortError());
    }

    return new Promise((resolve, reject) => {
      const onEvent = (event: Event) => {
        const detail = (event as CustomEvent<Extract<EmbeddedRealtimeEvent, { type: TType }>>).detail;
        if (predicate && !predicate(detail)) {
          return;
        }
        cleanup();
        resolve(detail);
      };
      const onAbort = () => {
        cleanup();
        reject(abortError());
      };
      const cleanup = () => {
        this.removeEventListener(type, onEvent);
        signal?.removeEventListener("abort", onAbort);
      };

      this.addEventListener(type, onEvent);
      signal?.addEventListener("abort", onAbort, { once: true });
    });
  }

  async close(): Promise<void> {
    if (this._closed) {
      return;
    }
    this._closed = true;
    this.connection.close();
    await this.closeOwnedRuntime?.();
  }

  private dispatchTypedEvent(event: EmbeddedRealtimeEvent): void {
    this.dispatchEvent(new CustomEvent("event", { detail: event }));
    this.dispatchEvent(new CustomEvent(event.type, { detail: event }));
  }

  private dispatchRuntimeError(error: Error): void {
    this.dispatchEvent(new ErrorEvent("error", { error, message: error.message }));
  }
}

export async function createEmbeddedRealtime(
  options: CreateEmbeddedRealtimeOptions = {},
): Promise<EmbeddedRealtimeAdapter> {
  const autoStartRuntime = options.autoStartRuntime ?? true;
  let runtime: EmbeddedRuntime | undefined;
  let owned = false;
  let connection = options.connection;

  if (!connection) {
    const normalized = normalizeRuntime(options.runtime);
    runtime = normalized.runtime;
    owned = normalized.owned;
    if (!runtime) {
      throw new Error("No embedded runtime is available.");
    }
    if (autoStartRuntime && !runtime.status().running) {
      await runtime.start();
    }
    connection = await runtime.connectRealtime();
  } else if (options.runtime instanceof EmbeddedRuntime) {
    runtime = options.runtime;
  }

  const closeOwnedRuntime =
    owned && runtime
      ? async () => {
          if (runtime.status().running) {
            await runtime.destroy();
          }
        }
      : undefined;

  return new DefaultEmbeddedRealtimeAdapter(connection, runtime, closeOwnedRuntime);
}
