package compat

type User struct {
	ID            string `json:"id"`
	Username      string `json:"username,omitempty"`
	PhoneNumber   string `json:"phoneNumber,omitempty"`
	Email         string `json:"email,omitempty"`
	FullName      string `json:"fullName,omitempty"`
	ImgURL        string `json:"imgURL,omitempty"`
	CannotMessage *bool  `json:"cannotMessage,omitempty"`
	IsSelf        *bool  `json:"isSelf,omitempty"`
}

type Account struct {
	AccountID string `json:"accountID"`
	Network   string `json:"network"`
	User      User   `json:"user"`
}

type Participants struct {
	Items   []User `json:"items"`
	HasMore bool   `json:"hasMore"`
	Total   int    `json:"total"`
}

type Attachment struct {
	ID          string          `json:"id,omitempty"`
	Type        string          `json:"type"`
	SrcURL      string          `json:"srcURL,omitempty"`
	MimeType    string          `json:"mimeType,omitempty"`
	FileName    string          `json:"fileName,omitempty"`
	FileSize    int64           `json:"fileSize,omitempty"`
	IsGif       bool            `json:"isGif,omitempty"`
	IsSticker   bool            `json:"isSticker,omitempty"`
	IsVoiceNote bool            `json:"isVoiceNote,omitempty"`
	Duration    float64         `json:"duration,omitempty"`
	PosterImg   string          `json:"posterImg,omitempty"`
	Size        *AttachmentSize `json:"size,omitempty"`
}

type AttachmentSize struct {
	Width  int `json:"width,omitempty"`
	Height int `json:"height,omitempty"`
}

type Reaction struct {
	ID            string `json:"id"`
	ReactionKey   string `json:"reactionKey"`
	ImgURL        string `json:"imgURL,omitempty"`
	ParticipantID string `json:"participantID"`
	Emoji         bool   `json:"emoji,omitempty"`
}

type Message struct {
	ID              string       `json:"id"`
	ChatID          string       `json:"chatID"`
	AccountID       string       `json:"accountID"`
	SenderID        string       `json:"senderID"`
	SenderName      string       `json:"senderName,omitempty"`
	Timestamp       string       `json:"timestamp"`
	SortKey         string       `json:"sortKey"`
	Type            string       `json:"type,omitempty"`
	Text            string       `json:"text,omitempty"`
	IsSender        bool         `json:"isSender,omitempty"`
	Attachments     []Attachment `json:"attachments,omitempty"`
	IsUnread        bool         `json:"isUnread,omitempty"`
	LinkedMessageID string       `json:"linkedMessageID,omitempty"`
	Reactions       []Reaction   `json:"reactions,omitempty"`
}

type Chat struct {
	ID                 string       `json:"id"`
	LocalChatID        *string      `json:"localChatID,omitempty"`
	AccountID          string       `json:"accountID"`
	Network            string       `json:"network"`
	Title              string       `json:"title"`
	Type               string       `json:"type"`
	Participants       Participants `json:"participants"`
	LastActivity       string       `json:"lastActivity,omitempty"`
	UnreadCount        int          `json:"unreadCount"`
	LastReadMessageKey string       `json:"lastReadMessageSortKey,omitempty"`
	IsArchived         bool         `json:"isArchived,omitempty"`
	IsMuted            bool         `json:"isMuted,omitempty"`
	IsPinned           bool         `json:"isPinned,omitempty"`
	Preview            *Message     `json:"preview,omitempty"`
}

type ListChatsOutput struct {
	Items        []Chat  `json:"items"`
	HasMore      bool    `json:"hasMore"`
	OldestCursor *string `json:"oldestCursor"`
	NewestCursor *string `json:"newestCursor"`
}

type ListMessagesOutput struct {
	Items   []Message `json:"items"`
	HasMore bool      `json:"hasMore"`
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
