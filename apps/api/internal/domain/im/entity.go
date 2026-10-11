package domainim

import (
	"strconv"
	"strings"
	"time"
)

const (
	ConversationTypeDirect = "DIRECT"
	ConversationTypeGroup  = "GROUP"

	MemberRoleOwner  = "OWNER"
	MemberRoleMember = "MEMBER"

	MessageTypeText   = "TEXT"
	MessageTypeVideo  = "VIDEO"
	MessageTypeSystem = "SYSTEM"

	MaxTitleLength           = 128
	MaxContentLength         = 4000
	MaxClientMessageIDLength = 128
	MaxLimit                 = 100
)

type Conversation struct {
	ID            int64
	Type          string
	Title         string
	OwnerID       int64
	DirectKey     string
	LastMessageID int64
	LastMessageAt *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type Member struct {
	ConversationID   int64
	UserID           int64
	Nickname         string
	AvatarURL        string
	Role             string
	LastReadSequence int64
	UnreadCount      int
	JoinedAt         time.Time
	UpdatedAt        time.Time
}

type Message struct {
	ID              int64
	ConversationID  int64
	SenderID        int64
	SenderNickname  string
	SenderAvatarURL string
	Type            string
	Content         string
	VideoID         int64
	ClientMessageID string
	Sequence        int64
	CreatedAt       time.Time
}

type ConversationSummary struct {
	Conversation *Conversation
	Members      []*Member
	LastMessage  *Message
	UnreadCount  int
}

type MessageCursor struct {
	Sequence int64
}

func NormalizeConversationType(value string) (string, error) {
	value = strings.ToUpper(strings.TrimSpace(value))
	switch value {
	case ConversationTypeDirect, ConversationTypeGroup:
		return value, nil
	default:
		return "", ErrInvalidConversationType
	}
}

func NormalizeMessageType(value string) (string, error) {
	value = strings.ToUpper(strings.TrimSpace(value))
	switch value {
	case MessageTypeText, MessageTypeVideo, MessageTypeSystem:
		return value, nil
	default:
		return "", ErrInvalidMessageType
	}
}

func DirectKey(userID int64, targetUserID int64) string {
	if userID <= 0 || targetUserID <= 0 || userID == targetUserID {
		return ""
	}
	if userID > targetUserID {
		userID, targetUserID = targetUserID, userID
	}
	return strconv.FormatInt(userID, 10) + ":" + strconv.FormatInt(targetUserID, 10)
}

func NewDirectConversation(userID int64, targetUserID int64) (*Conversation, []*Member, error) {
	directKey := DirectKey(userID, targetUserID)
	if directKey == "" {
		if userID == targetUserID {
			return nil, nil, ErrDirectConversationWithSelf
		}
		return nil, nil, ErrInvalidUserID
	}
	conversation := &Conversation{
		Type:      ConversationTypeDirect,
		DirectKey: directKey,
	}
	members := []*Member{
		{UserID: userID, Role: MemberRoleMember},
		{UserID: targetUserID, Role: MemberRoleMember},
	}
	return conversation, members, nil
}

func NewGroupConversation(ownerID int64, title string, memberIDs []int64) (*Conversation, []*Member, error) {
	if ownerID <= 0 {
		return nil, nil, ErrInvalidUserID
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, nil, ErrEmptyTitle
	}
	if len(title) > MaxTitleLength {
		return nil, nil, ErrTitleTooLong
	}
	conversation := &Conversation{
		Type:    ConversationTypeGroup,
		Title:   title,
		OwnerID: ownerID,
	}
	seen := map[int64]struct{}{ownerID: {}}
	members := []*Member{{UserID: ownerID, Role: MemberRoleOwner}}
	for _, memberID := range memberIDs {
		if memberID <= 0 {
			return nil, nil, ErrInvalidUserID
		}
		if _, exists := seen[memberID]; exists {
			continue
		}
		seen[memberID] = struct{}{}
		members = append(members, &Member{UserID: memberID, Role: MemberRoleMember})
	}
	return conversation, members, nil
}

func NewTextMessage(conversationID int64, senderID int64, content string, clientMessageID string) (*Message, error) {
	return newMessage(conversationID, senderID, MessageTypeText, content, 0, clientMessageID)
}

func NewVideoMessage(conversationID int64, senderID int64, content string, videoID int64, clientMessageID string) (*Message, error) {
	if videoID <= 0 {
		return nil, ErrInvalidVideoID
	}
	return newMessage(conversationID, senderID, MessageTypeVideo, content, videoID, clientMessageID)
}

func newMessage(conversationID int64, senderID int64, messageType string, content string, videoID int64, clientMessageID string) (*Message, error) {
	if conversationID <= 0 {
		return nil, ErrInvalidConversationID
	}
	if senderID <= 0 {
		return nil, ErrInvalidUserID
	}
	normalizedType, err := NormalizeMessageType(messageType)
	if err != nil {
		return nil, err
	}
	content = strings.TrimSpace(content)
	clientMessageID = strings.TrimSpace(clientMessageID)
	if content == "" && videoID <= 0 {
		return nil, ErrEmptyContent
	}
	if len(content) > MaxContentLength {
		return nil, ErrContentTooLong
	}
	if len(clientMessageID) > MaxClientMessageIDLength {
		return nil, ErrClientMessageIDTooLong
	}
	return &Message{
		ConversationID:  conversationID,
		SenderID:        senderID,
		Type:            normalizedType,
		Content:         content,
		VideoID:         videoID,
		ClientMessageID: clientMessageID,
	}, nil
}

func RestoreMessage(id int64, conversationID int64, senderID int64, senderNickname string, senderAvatarURL string, messageType string, content string, videoID int64, clientMessageID string, sequence int64, createdAt time.Time) *Message {
	normalizedType, _ := NormalizeMessageType(messageType)
	return &Message{
		ID:              id,
		ConversationID:  conversationID,
		SenderID:        senderID,
		SenderNickname:  strings.TrimSpace(senderNickname),
		SenderAvatarURL: strings.TrimSpace(senderAvatarURL),
		Type:            normalizedType,
		Content:         strings.TrimSpace(content),
		VideoID:         videoID,
		ClientMessageID: strings.TrimSpace(clientMessageID),
		Sequence:        sequence,
		CreatedAt:       createdAt,
	}
}
