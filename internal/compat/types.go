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

type SendMessageOutput struct {
	ChatID           string `json:"chatID"`
	PendingMessageID string `json:"pendingMessageID"`
}

type EditMessageOutput struct {
	ChatID    string `json:"chatID"`
	MessageID string `json:"messageID"`
	Success   bool   `json:"success"`
}

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

type DownloadAssetInput struct {
	URL string `json:"url"`
}

type DownloadAssetOutput struct {
	SrcURL string `json:"srcURL,omitempty"`
	Error  string `json:"error,omitempty"`
}

type UploadAssetInput struct {
	Content  string `json:"content"`
	FileName string `json:"fileName,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

type UploadAssetOutput struct {
	UploadID string  `json:"uploadID,omitempty"`
	SrcURL   string  `json:"srcURL,omitempty"`
	FileName string  `json:"fileName,omitempty"`
	MimeType string  `json:"mimeType,omitempty"`
	FileSize int64   `json:"fileSize,omitempty"`
	Width    int     `json:"width,omitempty"`
	Height   int     `json:"height,omitempty"`
	Duration float64 `json:"duration,omitempty"`
	Error    string  `json:"error,omitempty"`
}

type SendMessageInput struct {
	Text             string                  `json:"text,omitempty"`
	ReplyToMessageID string                  `json:"replyToMessageID,omitempty"`
	Attachment       *MessageAttachmentInput `json:"attachment,omitempty"`
}

type MessageAttachmentInput struct {
	UploadID string          `json:"uploadID"`
	MimeType string          `json:"mimeType,omitempty"`
	FileName string          `json:"fileName,omitempty"`
	Size     *AttachmentSize `json:"size,omitempty"`
	Duration float64         `json:"duration,omitempty"`
	Type     string          `json:"type,omitempty"`
}

type EditMessageInput struct {
	Text string `json:"text"`
}

type AddReactionInput struct {
	ReactionKey   string `json:"reactionKey"`
	TransactionID string `json:"transactionID,omitempty"`
}

type RemoveReactionInput struct {
	ReactionKey string `json:"reactionKey"`
}

type ArchiveChatInput struct {
	Archived bool `json:"archived"`
}

type SetChatReminderInput struct {
	Reminder map[string]any `json:"reminder"`
}

type ActionSuccessOutput struct {
	Success bool `json:"success"`
}

type SearchContactsOutput struct {
	Items []User `json:"items"`
}

type FocusAppInput struct {
	ChatID              string `json:"chatID,omitempty"`
	MessageID           string `json:"messageID,omitempty"`
	DraftText           string `json:"draftText,omitempty"`
	DraftAttachmentPath string `json:"draftAttachmentPath,omitempty"`
}

type FocusAppOutput struct {
	Success bool `json:"success"`
}

type CreateChatInput struct {
	AccountID      string   `json:"accountID"`
	Type           string   `json:"type"`
	ParticipantIDs []string `json:"participantIDs"`
	Title          string   `json:"title,omitempty"`
	MessageText    string   `json:"messageText,omitempty"`
}

type CreateChatOutput struct {
	ChatID string `json:"chatID"`
}

type UnifiedSearchResults struct {
	Chats    []Chat               `json:"chats"`
	InGroups []Chat               `json:"in_groups"`
	Messages SearchMessagesOutput `json:"messages"`
}

type UnifiedSearchOutput struct {
	Results UnifiedSearchResults `json:"results"`
}
