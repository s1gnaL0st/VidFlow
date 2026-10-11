package interfaceshttpim

import (
	domainim "GCFeed/internal/domain/im"
	"time"
)

type createConversationRequest struct {
	Type         string  `json:"type"`
	TargetUserID int64   `json:"target_user_id"`
	Title        string  `json:"title"`
	MemberIDs    []int64 `json:"member_ids"`
}

type sendMessageRequest struct {
	Type            string `json:"type"`
	Content         string `json:"content"`
	VideoID         int64  `json:"video_id"`
	ClientMessageID string `json:"client_message_id"`
}

type markReadRequest struct {
	Sequence int64 `json:"sequence"`
}

type memberResponse struct {
	UserID    int64  `json:"user_id"`
	Nickname  string `json:"nickname"`
	AvatarURL string `json:"avatar_url"`
	Role      string `json:"role"`
	Online    bool   `json:"online"`
}

type conversationResponse struct {
	ID            int64            `json:"id"`
	Type          string           `json:"type"`
	Title         string           `json:"title,omitempty"`
	OwnerID       int64            `json:"owner_id,omitempty"`
	LastMessageID int64            `json:"last_message_id,omitempty"`
	LastMessageAt *time.Time       `json:"last_message_at,omitempty"`
	UnreadCount   int              `json:"unread_count"`
	Members       []memberResponse `json:"members"`
	LastMessage   *messageResponse `json:"last_message,omitempty"`
	CreatedAt     time.Time        `json:"created_at"`
	UpdatedAt     time.Time        `json:"updated_at"`
}

type messageResponse struct {
	ID              int64     `json:"id"`
	ConversationID  int64     `json:"conversation_id"`
	SenderID        int64     `json:"sender_id"`
	SenderNickname  string    `json:"sender_nickname,omitempty"`
	SenderAvatarURL string    `json:"sender_avatar_url,omitempty"`
	Type            string    `json:"type"`
	Content         string    `json:"content,omitempty"`
	VideoID         int64     `json:"video_id,omitempty"`
	ClientMessageID string    `json:"client_message_id,omitempty"`
	Sequence        int64     `json:"sequence"`
	CreatedAt       time.Time `json:"created_at"`
}

type conversationListResponse struct {
	Items []conversationResponse `json:"items"`
}

type messageListResponse struct {
	Items []messageResponse `json:"items"`
}

type markReadResponse struct {
	UpdatedCount int `json:"updated_count"`
}

type websocketEnvelope struct {
	Type string           `json:"type"`
	Data *messageResponse `json:"data,omitempty"`
}

func responseFromConversationSummary(summary *domainim.ConversationSummary, onlineByUserID map[int64]bool) conversationResponse {
	if summary == nil || summary.Conversation == nil {
		return conversationResponse{}
	}
	conversation := summary.Conversation
	response := conversationResponse{
		ID:            conversation.ID,
		Type:          conversation.Type,
		Title:         conversation.Title,
		OwnerID:       conversation.OwnerID,
		LastMessageID: conversation.LastMessageID,
		LastMessageAt: conversation.LastMessageAt,
		UnreadCount:   summary.UnreadCount,
		Members:       make([]memberResponse, 0, len(summary.Members)),
		CreatedAt:     conversation.CreatedAt,
		UpdatedAt:     conversation.UpdatedAt,
	}
	for _, member := range summary.Members {
		if member == nil {
			continue
		}
		response.Members = append(response.Members, memberResponse{
			UserID:    member.UserID,
			Nickname:  member.Nickname,
			AvatarURL: member.AvatarURL,
			Role:      member.Role,
			Online:    onlineByUserID[member.UserID],
		})
	}
	if summary.LastMessage != nil {
		message := responseFromMessage(summary.LastMessage)
		response.LastMessage = &message
	}
	return response
}

func responseFromMessage(message *domainim.Message) messageResponse {
	if message == nil {
		return messageResponse{}
	}
	return messageResponse{
		ID:              message.ID,
		ConversationID:  message.ConversationID,
		SenderID:        message.SenderID,
		SenderNickname:  message.SenderNickname,
		SenderAvatarURL: message.SenderAvatarURL,
		Type:            message.Type,
		Content:         message.Content,
		VideoID:         message.VideoID,
		ClientMessageID: message.ClientMessageID,
		Sequence:        message.Sequence,
		CreatedAt:       message.CreatedAt,
	}
}
