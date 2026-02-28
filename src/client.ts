import { BeeperDesktop, type ClientOptions } from "@beeper/desktop-api";

import { type FetchLike } from "./native-fetch.js";
import { EmbeddedRuntime, type EmbeddedRuntimeOptions } from "./runtime.js";

export type RuntimeInput = EmbeddedRuntime | EmbeddedRuntimeOptions | false;

export interface CreateEmbeddedFetchOptions {
  runtime?: RuntimeInput;
  autoStartRuntime?: boolean;
  fetch?: FetchLike;
}

export interface EmbeddedFetchHandle {
  fetch: FetchLike;
  runtime?: EmbeddedRuntime;
  baseURL?: string;
  close(): Promise<void>;
}

export interface WithEmbeddedOptions {
  runtime?: RuntimeInput;
  autoStartRuntime?: boolean;
  sdkOptions?: ClientOptions;
}

export interface EmbeddedSDKHandle<TSDK> extends EmbeddedFetchHandle {
  sdk: TSDK;
}

function resolveFetch(fetchOverride?: FetchLike): FetchLike {
  if (fetchOverride) {
    return fetchOverride;
  }
  if (typeof globalThis.fetch !== "function") {
    throw new Error("No fetch implementation available. Provide options.fetch.");
  }
  return globalThis.fetch.bind(globalThis);
}

function normalizeRuntime(runtime: RuntimeInput | undefined): { runtime?: EmbeddedRuntime; owned: boolean } {
  if (runtime === false) {
    return { runtime: undefined, owned: false };
  }
  if (runtime instanceof EmbeddedRuntime) {
    return { runtime, owned: false };
  }
  return {
    runtime: new EmbeddedRuntime(runtime ?? {}),
    owned: true,
  };
}

function normalizeWithEmbeddedOptions(
  options: WithEmbeddedOptions | EmbeddedRuntime | undefined,
): WithEmbeddedOptions {
  if (options instanceof EmbeddedRuntime) {
    return { runtime: options };
  }
  return options ?? {};
}

function isSDKConstructor(sdk: typeof BeeperDesktop | BeeperDesktop): sdk is typeof BeeperDesktop {
  return typeof sdk === "function";
}

export async function createEmbeddedFetch(
  options: CreateEmbeddedFetchOptions = {},
): Promise<EmbeddedFetchHandle> {
  const autoStartRuntime = options.autoStartRuntime ?? true;
  const runtimeResolved = normalizeRuntime(options.runtime);
  const runtime = runtimeResolved.runtime;

  if (runtime && autoStartRuntime && !runtime.status().running) {
    await runtime.start();
  }

  return {
    fetch: resolveFetch(options.fetch),
    runtime,
    baseURL: runtime?.baseURL,
    async close() {
      if (runtime && runtimeResolved.owned && runtime.status().running) {
        await runtime.stop();
      }
    },
  };
}

export async function withEmbedded(
  sdk: typeof BeeperDesktop,
  options?: WithEmbeddedOptions | EmbeddedRuntime,
): Promise<EmbeddedSDKHandle<BeeperDesktop>>;
export async function withEmbedded(
  sdk: BeeperDesktop,
  options?: WithEmbeddedOptions | EmbeddedRuntime,
): Promise<EmbeddedSDKHandle<BeeperDesktop>>;
export async function withEmbedded(
  sdk: typeof BeeperDesktop | BeeperDesktop,
  options?: WithEmbeddedOptions | EmbeddedRuntime,
): Promise<EmbeddedSDKHandle<BeeperDesktop>> {
  const normalized = normalizeWithEmbeddedOptions(options);

  const embedded = await createEmbeddedFetch({
    runtime: normalized.runtime,
    autoStartRuntime: normalized.autoStartRuntime,
    fetch: normalized.sdkOptions?.fetch as FetchLike | undefined,
  });

  const sdkOptions: ClientOptions = {
    ...(normalized.sdkOptions ?? {}),
    fetch: (normalized.sdkOptions?.fetch as FetchLike | undefined) ?? embedded.fetch,
    baseURL: normalized.sdkOptions?.baseURL ?? embedded.baseURL,
  };

  const nextSDK = isSDKConstructor(sdk) ? new sdk(sdkOptions) : sdk.withOptions(sdkOptions);

  return {
    sdk: nextSDK,
    fetch: embedded.fetch,
    runtime: embedded.runtime,
    baseURL: embedded.baseURL,
    close: embedded.close,
  };
}
