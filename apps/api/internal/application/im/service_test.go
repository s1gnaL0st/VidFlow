package applicationim

import (
	domainim "GCFeed/internal/domain/im"
	"context"
	"errors"
	"sort"
	"testing"
)

func TestConversationMessageAndReadFlow(t *testing.T) {
	repo := newMemoryIMRepository()
	service := New(repo)
	ctx := context.Background()

	conversation, err := service.CreateDirectConversation(ctx, 1, 2)
	if err != nil {
		t.Fatalf("create direct conversation: %v", err)
	}
	if !conversation.Created || conversation.Conversation.ID <= 0 {
		t.Fatalf("unexpected conversation result: %+v", conversation)
	}

	sent, err := service.SendTextMessage(ctx, 1, conversation.Conversation.ID, "你好", "client-1")
	if err != nil {
		t.Fatalf("send message: %v", err)
	}
	if !sent.Created || sent.Message.Sequence != 1 || len(sent.RecipientIDs) != 2 {
		t.Fatalf("unexpected send result: %+v", sent)
	}

	replayed, err := service.SendTextMessage(ctx, 1, conversation.Conversation.ID, "你好", "client-1")
	if err != nil {
		t.Fatalf("replay message: %v", err)
	}
	if replayed.Created || replayed.Message.ID != sent.Message.ID {
		t.Fatalf("message replay was not idempotent: %+v", replayed)
	}

	messages, err := service.ListMessages(ctx, 2, conversation.Conversation.ID, 0, 20)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(messages) != 1 || messages[0].Content != "你好" {
		t.Fatalf("unexpected messages: %+v", messages)
	}

	updated, err := service.MarkRead(ctx, 2, conversation.Conversation.ID, sent.Message.Sequence)
	if err != nil {
		t.Fatalf("mark read: %v", err)
	}
	if updated != 1 {
		t.Fatalf("expected one member updated, got %d", updated)
	}
}

type memoryIMRepository struct {
	nextConversationID int64
	nextMessageID      int64
	conversations      map[int64]*domainim.Conversation
	members            map[int64][]*domainim.Member
	messages           map[int64][]*domainim.Message
}

func newMemoryIMRepository() *memoryIMRepository {
	return &memoryIMRepository{
		conversations: map[int64]*domainim.Conversation{},
		members:       map[int64][]*domainim.Member{},
		messages:      map[int64][]*domainim.Message{},
	}
}

func (r *memoryIMRepository) CreateConversation(ctx context.Context, conversation *domainim.Conversation, members []*domainim.Member) (*domainim.Conversation, bool, error) {
	if conversation.Type == domainim.ConversationTypeDirect {
		for _, existing := range r.conversations {
			if existing.DirectKey == conversation.DirectKey {
				return existing, false, nil
			}
		}
	}
	r.nextConversationID++
	conversation.ID = r.nextConversationID
	r.conversations[conversation.ID] = conversation
	for _, member := range members {
		copy := *member
		copy.ConversationID = conversation.ID
		r.members[conversation.ID] = append(r.members[conversation.ID], &copy)
	}
	return conversation, true, nil
}

func (r *memoryIMRepository) FindConversationForUser(ctx context.Context, conversationID int64, userID int64) (*domainim.Conversation, *domainim.Member, error) {
	conversation := r.conversations[conversationID]
	if conversation == nil {
		return nil, nil, domainim.ErrConversationNotFound
	}
	for _, member := range r.members[conversationID] {
		if member.UserID == userID {
			return conversation, member, nil
		}
	}
	return nil, nil, domainim.ErrNotConversationMember
}

func (r *memoryIMRepository) ListConversations(ctx context.Context, userID int64, limit int) ([]*domainim.ConversationSummary, error) {
	items := []*domainim.ConversationSummary{}
	for conversationID, members := range r.members {
		for _, member := range members {
			if member.UserID != userID {
				continue
			}
			conversation := r.conversations[conversationID]
			var last *domainim.Message
			if messages := r.messages[conversationID]; len(messages) > 0 {
				last = messages[len(messages)-1]
			}
			items = append(items, &domainim.ConversationSummary{
				Conversation: conversation,
				Members:      members,
				LastMessage:  last,
				UnreadCount:  member.UnreadCount,
			})
			break
		}
	}
	return items, nil
}

func (r *memoryIMRepository) ListMessages(ctx context.Context, conversationID int64, beforeSequence int64, limit int) ([]*domainim.Message, error) {
	items := []*domainim.Message{}
	for _, message := range r.messages[conversationID] {
		if beforeSequence > 0 && message.Sequence >= beforeSequence {
			continue
		}
		items = append(items, message)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Sequence > items[j].Sequence })
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (r *memoryIMRepository) SendMessage(ctx context.Context, message *domainim.Message) (*domainim.Message, []int64, bool, error) {
	if _, _, err := r.FindConversationForUser(ctx, message.ConversationID, message.SenderID); err != nil {
		return nil, nil, false, err
	}
	for _, existing := range r.messages[message.ConversationID] {
		if message.ClientMessageID != "" && existing.ClientMessageID == message.ClientMessageID {
			return existing, r.memberIDs(message.ConversationID), false, nil
		}
	}
	r.nextMessageID++
	message.ID = r.nextMessageID
	message.Sequence = int64(len(r.messages[message.ConversationID]) + 1)
	message.SenderNickname = "用户"
	r.messages[message.ConversationID] = append(r.messages[message.ConversationID], message)
	for _, member := range r.members[message.ConversationID] {
		if member.UserID != message.SenderID {
			member.UnreadCount++
		}
	}
	return message, r.memberIDs(message.ConversationID), true, nil
}

func (r *memoryIMRepository) MarkRead(ctx context.Context, conversationID int64, userID int64, sequence int64) (int, error) {
	for _, member := range r.members[conversationID] {
		if member.UserID == userID {
			member.LastReadSequence = sequence
			member.UnreadCount = 0
			return 1, nil
		}
	}
	return 0, errors.New("member not found")
}

func (r *memoryIMRepository) memberIDs(conversationID int64) []int64 {
	ids := make([]int64, 0, len(r.members[conversationID]))
	for _, member := range r.members[conversationID] {
		ids = append(ids, member.UserID)
	}
	return ids
}
