package domainim

import "context"

type Repository interface {
	CreateConversation(ctx context.Context, conversation *Conversation, members []*Member) (*Conversation, bool, error)
	FindConversationForUser(ctx context.Context, conversationID int64, userID int64) (*Conversation, *Member, error)
	ListConversations(ctx context.Context, userID int64, limit int) ([]*ConversationSummary, error)
	ListMessages(ctx context.Context, conversationID int64, beforeSequence int64, limit int) ([]*Message, error)
	SendMessage(ctx context.Context, message *Message) (*Message, []int64, bool, error)
	MarkRead(ctx context.Context, conversationID int64, userID int64, sequence int64) (int, error)
}
