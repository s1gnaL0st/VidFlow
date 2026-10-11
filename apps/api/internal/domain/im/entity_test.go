package domainim

import (
	"errors"
	"testing"
)

func TestDirectKeyIsStableAcrossMemberOrder(t *testing.T) {
	if DirectKey(42, 7) != DirectKey(7, 42) {
		t.Fatalf("direct key should be stable")
	}
	if DirectKey(42, 42) != "" {
		t.Fatalf("self conversation should not have a direct key")
	}
}

func TestNewGroupConversationDeduplicatesMembers(t *testing.T) {
	conversation, members, err := NewGroupConversation(7, "项目讨论", []int64{42, 7, 42, 99})
	if err != nil {
		t.Fatalf("new group: %v", err)
	}
	if conversation.Type != ConversationTypeGroup || conversation.OwnerID != 7 {
		t.Fatalf("unexpected conversation: %+v", conversation)
	}
	if len(members) != 3 {
		t.Fatalf("expected 3 unique members, got %+v", members)
	}
	if members[0].Role != MemberRoleOwner {
		t.Fatalf("owner role missing: %+v", members[0])
	}
}

func TestNewDirectConversationRejectsSelfChat(t *testing.T) {
	_, _, err := NewDirectConversation(42, 42)
	if !errors.Is(err, ErrDirectConversationWithSelf) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewVideoMessageRequiresVideoID(t *testing.T) {
	_, err := NewVideoMessage(10, 7, "", 0, "client-1")
	if !errors.Is(err, ErrInvalidVideoID) {
		t.Fatalf("unexpected error: %v", err)
	}
	message, err := NewVideoMessage(10, 7, "分享一个视频", 1001, "client-1")
	if err != nil {
		t.Fatalf("new video message: %v", err)
	}
	if message.Type != MessageTypeVideo || message.VideoID != 1001 {
		t.Fatalf("unexpected message: %+v", message)
	}
}
