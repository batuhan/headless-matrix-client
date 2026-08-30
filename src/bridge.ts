import type { NativeRequest, NativeResponse } from "./native-fetch.js";

export const EMBEDDED_HTTP_REQUEST = "http.request";
export const EMBEDDED_HTTP_RESPONSE = "http.response";
export const EMBEDDED_RUNTIME_INFO = "runtime.info";

export interface EmbeddedRuntimeInfo {
  started: boolean;
  listenAddr?: string;
  stateDir?: string;
}

export interface EmbeddedHTTPRequestCommand {
  type: typeof EMBEDDED_HTTP_REQUEST;
  request: NativeRequest;
}

export interface EmbeddedRuntimeInfoCommand {
  type: typeof EMBEDDED_RUNTIME_INFO;
}

export type EmbeddedCommand = EmbeddedHTTPRequestCommand | EmbeddedRuntimeInfoCommand;

export interface EmbeddedHTTPResponseResult {
  type: typeof EMBEDDED_HTTP_RESPONSE;
  response: NativeResponse;
}

export interface EmbeddedRuntimeInfoResult {
  type: typeof EMBEDDED_RUNTIME_INFO;
  runtimeInfo: EmbeddedRuntimeInfo;
}

export type EmbeddedCommandResult = EmbeddedHTTPResponseResult | EmbeddedRuntimeInfoResult;

export type EmbeddedCommandInvoker = (
  command: EmbeddedCommand,
) => EmbeddedCommandResult | Promise<EmbeddedCommandResult>;

export interface EmbeddedSubscriptionsSetCommand {
  type: "subscriptions.set";
  requestID?: string;
  chatIDs: string[];
}

export interface EmbeddedPresenceSetCommand {
  type: "presence.set";
  requestID?: string;
  active: boolean;
}

export type EmbeddedRealtimeCommand =
  | EmbeddedSubscriptionsSetCommand
  | EmbeddedPresenceSetCommand
  | ({ type: string } & Record<string, unknown>);

export interface EmbeddedReadyEvent {
  type: "ready";
  version: number;
  chatIDs: string[];
}

export interface EmbeddedSubscriptionsUpdatedEvent {
  type: "subscriptions.updated";
  requestID?: string;
  chatIDs: string[];
}

export interface EmbeddedPresenceUpdatedEvent {
  type: "presence.updated";
  requestID?: string;
  active: boolean;
}

export interface EmbeddedRealtimeErrorEvent {
  type: "error";
  requestID?: string;
  code: string;
  message: string;
}

export interface EmbeddedDomainEvent {
  type: "chat.upserted" | "chat.deleted" | "message.upserted" | "message.deleted";
  seq: number;
  ts: number;
  chatID: string;
  ids: string[];
  entries?: Record<string, unknown>[];
}

export type EmbeddedRealtimeEvent =
  | EmbeddedReadyEvent
  | EmbeddedSubscriptionsUpdatedEvent
  | EmbeddedPresenceUpdatedEvent
  | EmbeddedRealtimeErrorEvent
  | EmbeddedDomainEvent
  | ({ type: string } & Record<string, unknown>);

export type EmbeddedRealtimeEventType = EmbeddedRealtimeEvent["type"];

export function isEmbeddedHTTPResponseResult(
  result: EmbeddedCommandResult,
): result is EmbeddedHTTPResponseResult {
  return result.type === EMBEDDED_HTTP_RESPONSE;
}

export function isEmbeddedRuntimeInfoResult(
  result: EmbeddedCommandResult,
): result is EmbeddedRuntimeInfoResult {
  return result.type === EMBEDDED_RUNTIME_INFO;
}
