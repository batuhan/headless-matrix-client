package compat

import (
	beeperdesktopapi "github.com/beeper/desktop-api-go"
	"github.com/beeper/desktop-api-go/shared"
)

type User = shared.User
type Account = beeperdesktopapi.Account
type Participants = beeperdesktopapi.ChatParticipants
type Attachment = shared.Attachment
type AttachmentType = shared.AttachmentType
type AttachmentSize = shared.AttachmentSize
type Reaction = shared.Reaction
type Message = shared.Message
type MessageType = shared.MessageType
type ChatType = beeperdesktopapi.ChatType

type Chat struct {
	beeperdesktopapi.Chat
	// Extension for current renderer expectations.
	Network string `json:"network,omitempty"`
	// List chats includes an optional preview object.
	Preview *Message `json:"preview,omitempty"`
}

type ListChatsOutput struct {
	Items        []Chat  `json:"items"`
	HasMore      bool    `json:"hasMore"`
	OldestCursor *string `json:"oldestCursor"`
	NewestCursor *string `json:"newestCursor"`
}

type SearchChatsOutput = ListChatsOutput

type ListMessagesOutput struct {
	Items   []Message `json:"items"`
	HasMore bool      `json:"hasMore"`
}

type SearchMessagesOutput struct {
	Items        []Message       `json:"items"`
	Chats        map[string]Chat `json:"chats"`
	HasMore      bool            `json:"hasMore"`
	OldestCursor *string         `json:"oldestCursor"`
	NewestCursor *string         `json:"newestCursor"`
}

type SendMessageOutput = beeperdesktopapi.MessageSendResponse
type EditMessageOutput = beeperdesktopapi.MessageUpdateResponse

type AddReactionOutput struct {
	Success       bool   `json:"success"`
	ChatID        string `json:"chatID"`
	MessageID     string `json:"messageID"`
	ReactionKey   string `json:"reactionKey"`
	TransactionID string `json:"transactionID"`
}

type RemoveReactionOutput struct {
	Success     bool   `json:"success"`
	ChatID      string `json:"chatID"`
	MessageID   string `json:"messageID"`
	ReactionKey string `json:"reactionKey"`
}

type DownloadAssetInput = beeperdesktopapi.AssetDownloadParams
type DownloadAssetOutput = beeperdesktopapi.AssetDownloadResponse

type UploadAssetInput = beeperdesktopapi.AssetUploadBase64Params
type UploadAssetOutput = beeperdesktopapi.AssetUploadBase64Response

type SendMessageInput = beeperdesktopapi.MessageSendParams
type MessageAttachmentInput = beeperdesktopapi.MessageSendParamsAttachment
type EditMessageInput = beeperdesktopapi.MessageUpdateParams

type AddReactionInput struct {
	ReactionKey   string `json:"reactionKey"`
	TransactionID string `json:"transactionID,omitempty"`
}

type RemoveReactionInput struct {
	ReactionKey string `json:"reactionKey"`
}

type ArchiveChatInput = beeperdesktopapi.ChatArchiveParams
type SetChatReminderInput = beeperdesktopapi.ChatReminderNewParams

type ActionSuccessOutput = beeperdesktopapi.ChatArchiveResponse
type SearchContactsOutput = beeperdesktopapi.AccountContactSearchResponse

type ListContactsOutput struct {
	Items        []User  `json:"items"`
	HasMore      bool    `json:"hasMore"`
	OldestCursor *string `json:"oldestCursor"`
	NewestCursor *string `json:"newestCursor"`
}

type FocusAppInput = beeperdesktopapi.FocusParams
type FocusAppOutput = beeperdesktopapi.FocusResponse

type CreateChatStartUserInput = shared.User

type CreateChatInput struct {
	beeperdesktopapi.ChatNewParams
	Mode        string                    `json:"mode,omitempty"`
	User        *CreateChatStartUserInput `json:"user,omitempty"`
	AllowInvite *bool                     `json:"allowInvite,omitempty"`
}

type CreateChatOutput struct {
	beeperdesktopapi.ChatNewResponse
	Status string `json:"status,omitempty"`
}

type UnifiedSearchResults struct {
	Chats    []Chat               `json:"chats"`
	InGroups []Chat               `json:"in_groups"`
	Messages SearchMessagesOutput `json:"messages"`
}

type UnifiedSearchOutput struct {
	Results UnifiedSearchResults `json:"results"`
}
