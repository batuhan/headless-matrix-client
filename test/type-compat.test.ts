import type { BeeperDesktop } from "@beeper/desktop-api";

import type { CompatibleRouteTypes } from "../src/type-contract.js";

type Assert<T extends true> = T;
type IsAssignable<From, To> = [From] extends [To] ? true : false;

// Compile-time contracts between our package route aliases and upstream SDK types.
type _AccountsList = Assert<
  IsAssignable<CompatibleRouteTypes["accounts.list"], BeeperDesktop.AccountListResponse>
>;
type _ChatsList = Assert<IsAssignable<CompatibleRouteTypes["chats.list"], BeeperDesktop.ChatListResponse>>;
type _ChatsSearch = Assert<
  IsAssignable<CompatibleRouteTypes["chats.search"], BeeperDesktop.ChatsCursorSearch>
>;
type _MessagesList = Assert<
  IsAssignable<CompatibleRouteTypes["messages.list"], BeeperDesktop.CursorNoLimitResponse<BeeperDesktop.Message>>
>;
type _MessagesSearch = Assert<
  IsAssignable<CompatibleRouteTypes["messages.search"], BeeperDesktop.CursorSearchResponse<BeeperDesktop.Message>>
>;
type _SendMessage = Assert<
  IsAssignable<CompatibleRouteTypes["messages.send"], BeeperDesktop.MessageSendResponse>
>;
type _AssetsDownload = Assert<
  IsAssignable<CompatibleRouteTypes["assets.download"], BeeperDesktop.AssetDownloadResponse>
>;
type _Focus = Assert<IsAssignable<CompatibleRouteTypes["focus"], BeeperDesktop.FocusResponse>>;
type _Search = Assert<IsAssignable<CompatibleRouteTypes["search"], BeeperDesktop.SearchResponse>>;

void (0 as unknown as _AccountsList);
void (0 as unknown as _ChatsList);
void (0 as unknown as _ChatsSearch);
void (0 as unknown as _MessagesList);
void (0 as unknown as _MessagesSearch);
void (0 as unknown as _SendMessage);
void (0 as unknown as _AssetsDownload);
void (0 as unknown as _Focus);
void (0 as unknown as _Search);
