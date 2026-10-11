package applicationim

import (
	domainim "GCFeed/internal/domain/im"
	"context"
	"errors"
)

const defaultConversationLimit = 50
const defaultMessageLimit = 30

var ErrLoadConversationFailed = errors.New("failed to load conversation")
var ErrSaveConversationFailed = errors.New("failed to save conversation")
var ErrSaveMessageFailed = errors.New("failed to save message")
var ErrLoadMessageFailed = errors.New("failed to load conversation messages")
var ErrMarkReadFailed = errors.New("failed to mark conversation read")

type Service struct {
	repo domainim.Repository
}

type CreateConversationResult struct {
	Conversation *domainim.Conversation
	Created      bool
}

type MessageResult struct {
	Message      *domainim.Message
	RecipientIDs []int64
	Created      bool
}

func New(repo domainim.Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) CreateDirectConversation(ctx context.Context, userID int64, targetUserID int64) (*CreateConversationResult, error) {
	conversation, members, err := domainim.NewDirectConversation(userID, targetUserID)
	if err != nil {
		return nil, err
	}
	saved, created, err := s.repo.CreateConversation(ctx, conversation, members)
	if err != nil {
		return nil, mapCreateConversationError(err)
	}
	return &CreateConversationResult{Conversation: saved, Created: created}, nil
}

func (s *Service) CreateGroupConversation(ctx context.Context, ownerID int64, title string, memberIDs []int64) (*CreateConversationResult, error) {
	conversation, members, err := domainim.NewGroupConversation(ownerID, title, memberIDs)
	if err != nil {
		return nil, err
	}
	saved, created, err := s.repo.CreateConversation(ctx, conversation, members)
	if err != nil {
		return nil, mapCreateConversationError(err)
	}
	return &CreateConversationResult{Conversation: saved, Created: created}, nil
}

func (s *Service) ListConversations(ctx context.Context, userID int64, limit int) ([]*domainim.ConversationSummary, error) {
	if userID <= 0 {
		return nil, domainim.ErrInvalidUserID
	}
	items, err := s.repo.ListConversations(ctx, userID, normalizeConversationLimit(limit))
	if err != nil {
		return nil, ErrLoadConversationFailed
	}
	return items, nil
}

func (s *Service) ListMessages(ctx context.Context, userID int64, conversationID int64, beforeSequence int64, limit int) ([]*domainim.Message, error) {
	if userID <= 0 {
		return nil, domainim.ErrInvalidUserID
	}
	if conversationID <= 0 {
		return nil, domainim.ErrInvalidConversationID
	}
	if beforeSequence < 0 {
		return nil, domainim.ErrInvalidSequence
	}
	if _, _, err := s.repo.FindConversationForUser(ctx, conversationID, userID); err != nil {
		return nil, mapConversationAccessError(err)
	}
	items, err := s.repo.ListMessages(ctx, conversationID, beforeSequence, normalizeMessageLimit(limit))
	if err != nil {
		return nil, ErrLoadMessageFailed
	}
	return items, nil
}

func (s *Service) SendTextMessage(ctx context.Context, userID int64, conversationID int64, content string, clientMessageID string) (*MessageResult, error) {
	message, err := domainim.NewTextMessage(conversationID, userID, content, clientMessageID)
	if err != nil {
		return nil, err
	}
	return s.sendMessage(ctx, message)
}

func (s *Service) SendVideoMessage(ctx context.Context, userID int64, conversationID int64, content string, videoID int64, clientMessageID string) (*MessageResult, error) {
	message, err := domainim.NewVideoMessage(conversationID, userID, content, videoID, clientMessageID)
	if err != nil {
		return nil, err
	}
	return s.sendMessage(ctx, message)
}

func (s *Service) sendMessage(ctx context.Context, message *domainim.Message) (*MessageResult, error) {
	saved, recipients, created, err := s.repo.SendMessage(ctx, message)
	if err != nil {
		return nil, mapConversationAccessError(err)
	}
	return &MessageResult{
		Message:      saved,
		RecipientIDs: recipients,
		Created:      created,
	}, nil
}

func (s *Service) MarkRead(ctx context.Context, userID int64, conversationID int64, sequence int64) (int, error) {
	if userID <= 0 {
		return 0, domainim.ErrInvalidUserID
	}
	if conversationID <= 0 {
		return 0, domainim.ErrInvalidConversationID
	}
	if sequence < 0 {
		return 0, domainim.ErrInvalidSequence
	}
	if _, _, err := s.repo.FindConversationForUser(ctx, conversationID, userID); err != nil {
		return 0, mapConversationAccessError(err)
	}
	count, err := s.repo.MarkRead(ctx, conversationID, userID, sequence)
	if err != nil {
		return 0, ErrMarkReadFailed
	}
	return count, nil
}

func normalizeConversationLimit(limit int) int {
	if limit <= 0 {
		return defaultConversationLimit
	}
	if limit > domainim.MaxLimit {
		return domainim.MaxLimit
	}
	return limit
}

func normalizeMessageLimit(limit int) int {
	if limit <= 0 {
		return defaultMessageLimit
	}
	if limit > domainim.MaxLimit {
		return domainim.MaxLimit
	}
	return limit
}

func mapConversationAccessError(err error) error {
	switch {
	case errors.Is(err, domainim.ErrConversationNotFound):
		return domainim.ErrConversationNotFound
	case errors.Is(err, domainim.ErrNotConversationMember):
		return domainim.ErrNotConversationMember
	default:
		return ErrLoadConversationFailed
	}
}

func mapCreateConversationError(err error) error {
	if errors.Is(err, domainim.ErrUserNotFound) {
		return err
	}
	return ErrSaveConversationFailed
}
