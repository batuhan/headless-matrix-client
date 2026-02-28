import type { BeeperDesktop } from "@beeper/desktop-api";

export interface CompatibleRouteTypes {
  "accounts.list": BeeperDesktop.AccountListResponse;
  "chats.list": BeeperDesktop.ChatListResponse;
  "chats.search": BeeperDesktop.ChatsCursorSearch;
  "messages.list": BeeperDesktop.CursorNoLimitResponse<BeeperDesktop.Message>;
  "messages.search": BeeperDesktop.CursorSearchResponse<BeeperDesktop.Message>;
  "messages.send": BeeperDesktop.MessageSendResponse;
  "assets.download": BeeperDesktop.AssetDownloadResponse;
  "focus": BeeperDesktop.FocusResponse;
  "search": BeeperDesktop.SearchResponse;
}

export type AccountsListResponse = CompatibleRouteTypes["accounts.list"];
export type ChatsListResponse = CompatibleRouteTypes["chats.list"];
export type ChatsSearchResponse = CompatibleRouteTypes["chats.search"];
export type MessagesListResponse = CompatibleRouteTypes["messages.list"];
export type MessagesSearchResponse = CompatibleRouteTypes["messages.search"];
export type MessageSendResponse = CompatibleRouteTypes["messages.send"];
export type AssetsDownloadResponse = CompatibleRouteTypes["assets.download"];
export type FocusResponse = CompatibleRouteTypes["focus"];
export type SearchResponse = CompatibleRouteTypes["search"];
