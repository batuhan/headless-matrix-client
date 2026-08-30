package compat

import (
	"time"

	beeperdesktopapi "github.com/beeper/desktop-api-go"
	"github.com/beeper/desktop-api-go/shared"
)

type User = shared.User

type Account struct {
	AccountID    string              `json:"accountID"`
	User         User                `json:"user"`
	NetworkID    string              `json:"networkID"`
	Network      string              `json:"network"`
	Capabilities AccountCapabilities `json:"capabilities"`
}

type AccountCapabilities struct {
	UnlimitedMessageEdits bool `json:"unlimitedMessageEdits"`
}

type SessionOutput struct {
	User     User      `json:"user"`
	Accounts []Account `json:"accounts"`
}

type Participants = beeperdesktopapi.ChatParticipants
type Attachment = shared.Attachment
type AttachmentType = shared.AttachmentType
type AttachmentSize = shared.AttachmentSize
type Reaction = shared.Reaction
type MessageType = shared.MessageType
type ChatType = beeperdesktopapi.ChatType

// Message extends the desktop API-compatible message shape with deletion
// state. Redacted Matrix events can retain their original content in the local
// database, so clients can render that content with deleted styling instead of
// replacing it with a generic tombstone.
type Message struct {
	ID              string       `json:"id"`
	AccountID       string       `json:"accountID"`
	ChatID          string       `json:"chatID"`
	SenderID        string       `json:"senderID"`
	SortKey         string       `json:"sortKey"`
	Timestamp       time.Time    `json:"timestamp"`
	Attachments     []Attachment `json:"attachments"`
	IsSender        bool         `json:"isSender"`
	IsUnread        bool         `json:"isUnread"`
	IsDeleted       bool         `json:"isDeleted,omitempty"`
	LinkedMessageID string       `json:"linkedMessageID"`
	// Mentions contains Matrix user IDs and the special @room value. A nil
	// slice is encoded as null for legacy events that predate m.mentions;
	// modern events with no mentions use a non-nil empty slice.
	Mentions   []string    `json:"mentions"`
	Reactions  []Reaction  `json:"reactions"`
	SenderName string      `json:"senderName"`
	Text       string      `json:"text"`
	Type       MessageType `json:"type"`
}

type Chat struct {
	beeperdesktopapi.Chat
	// Extension for current renderer expectations.
	Network string `json:"network,omitempty"`
	// Chat-level image (Matrix m.room.avatar). Used by clients to show a real
	// group photo instead of a participant collage. Empty when the room has
	// no explicit avatar.
	ImgURL string `json:"imgURL,omitempty"`
	// List chats includes an optional preview object.
	Preview *Message `json:"preview,omitempty"`
	// Desktop-side consumers treat marked unread separately from unreadCount.
	IsMarkedUnread bool `json:"isMarkedUnread"`
	// Low-priority is not in the public SDK schema, but Desktop consumers use it.
	IsLowPriority bool `json:"isLowPriority,omitempty"`
	// Extra metadata consumed by Desktop-side inbox/archive logic.
	Extra *ChatExtra `json:"extra,omitempty"`
	// Snooze metadata used by Desktop-side scheduling views.
	Snooze *ChatSnooze `json:"snooze,omitempty"`
}

type ChatExtra struct {
	MarkedUnreadUpdatedAt int64 `json:"markedUnreadUpdatedAt,omitempty"`
}

type ChatSnooze struct {
	SnoozeUntilMS *int64 `json:"snoozeUntilMs,omitempty"`
	UserSnoozedAt *int64 `json:"userSnoozedAt,omitempty"`
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
	// LastReadByOtherSortKey is the sortKey of the furthest message that at least
	// one other participant has read. Outgoing messages with sortKey <= this value
	// have been seen by the other side. Empty string if no read receipt exists.
	LastReadByOtherSortKey string `json:"lastReadByOtherSortKey,omitempty"`
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

type AddReactionOutput = beeperdesktopapi.ChatMessageReactionAddResponse

type RemoveReactionOutput = beeperdesktopapi.ChatMessageReactionDeleteResponse

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

type ActionSuccessOutput struct {
	Success bool `json:"success"`
}

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
	Mode           string                    `json:"mode,omitempty"`
	AccountID      string                    `json:"accountID"`
	Type           string                    `json:"type"`
	ParticipantIDs []string                  `json:"participantIDs"`
	Title          string                    `json:"title,omitempty"`
	MessageText    string                    `json:"messageText,omitempty"`
	User           *CreateChatStartUserInput `json:"user,omitempty"`
	AllowInvite    *bool                     `json:"allowInvite,omitempty"`
}

type CreateChatOutput = beeperdesktopapi.ChatNewResponse

type UnifiedSearchResults struct {
	Chats    []Chat               `json:"chats"`
	InGroups []Chat               `json:"in_groups"`
	Messages SearchMessagesOutput `json:"messages"`
}

type UnifiedSearchOutput struct {
	Results UnifiedSearchResults `json:"results"`
}
