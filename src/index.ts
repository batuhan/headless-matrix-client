export { BeeperDesktop } from "@beeper/desktop-api";
export type { ClientOptions } from "@beeper/desktop-api";

export {
  createFetchFromNativeRequest,
  createFetchFromNapiModule,
  type FetchLike,
  type NapiNativeHTTPModule,
  type NativeRequest,
  type NativeRequestBody,
  type NativeRequestFn,
  type NativeResponse,
  type NativeResponseBody,
} from "./native-fetch.js";

export {
  EmbeddedRuntime,
  type EmbeddedRuntimeOptions,
  type EmbeddedRuntimeStatus,
} from "./runtime.js";

export {
  createEmbeddedFetch,
  createEmbeddedFetch as desktopAPIFetch,
  withEmbedded,
  type RuntimeInput,
  type CreateEmbeddedFetchOptions,
  type EmbeddedFetchHandle,
  type WithEmbeddedOptions,
  type EmbeddedSDKHandle,
} from "./client.js";

export { run, type RunOptions } from "./run.js";

export type {
  CompatibleRouteTypes,
  AccountsListResponse,
  ChatsListResponse,
  ChatsSearchResponse,
  MessagesListResponse,
  MessagesSearchResponse,
  MessageSendResponse,
  AssetsDownloadResponse,
  FocusResponse,
  SearchResponse,
} from "./type-contract.js";
