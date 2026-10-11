package inframim

import (
	domainim "GCFeed/internal/domain/im"
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) CreateConversation(ctx context.Context, conversation *domainim.Conversation, members []*domainim.Member) (*domainim.Conversation, bool, error) {
	var saved ConversationModel
	created := true
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing ConversationModel
		if conversation.Type == domainim.ConversationTypeDirect {
			result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("direct_key = ?", conversation.DirectKey).
				Limit(1).
				Find(&existing)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected > 0 {
				saved = existing
				created = false
				return nil
			}
		}
		memberIDs := make([]int64, 0, len(members))
		for _, member := range members {
			if member != nil && member.UserID > 0 {
				memberIDs = append(memberIDs, member.UserID)
			}
		}
		if len(memberIDs) > 0 {
			var accountCount int64
			if err := tx.Table("account").Where("id IN ?", memberIDs).Count(&accountCount).Error; err != nil {
				return err
			}
			if int(accountCount) != len(memberIDs) {
				return domainim.ErrUserNotFound
			}
		}

		saved = ConversationModel{
			Type:      conversation.Type,
			Title:     conversation.Title,
			OwnerID:   conversation.OwnerID,
			DirectKey: optionalString(conversation.DirectKey),
		}
		if err := tx.Create(&saved).Error; err != nil {
			if isDuplicateKeyError(err) && conversation.Type == domainim.ConversationTypeDirect {
				if err := tx.Where("direct_key = ?", conversation.DirectKey).Take(&existing).Error; err != nil {
					return err
				}
				saved = existing
				created = false
				return nil
			}
			return err
		}
		for _, member := range members {
			if member == nil || member.UserID <= 0 {
				continue
			}
			model := MemberModel{
				ConversationID: saved.ID,
				UserID:         member.UserID,
				Role:           member.Role,
			}
			if err := tx.Create(&model).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return restoreConversation(saved), created, nil
}

func (r *Repository) FindConversationForUser(ctx context.Context, conversationID int64, userID int64) (*domainim.Conversation, *domainim.Member, error) {
	var conversation ConversationModel
	if err := r.db.WithContext(ctx).Where("id = ?", conversationID).Take(&conversation).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, domainim.ErrConversationNotFound
		}
		return nil, nil, err
	}
	var member MemberModel
	if err := r.db.WithContext(ctx).
		Where("conversation_id = ? AND user_id = ?", conversationID, userID).
		Take(&member).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, domainim.ErrNotConversationMember
		}
		return nil, nil, err
	}
	return restoreConversation(conversation), restoreMember(member), nil
}

func (r *Repository) ListConversations(ctx context.Context, userID int64, limit int) ([]*domainim.ConversationSummary, error) {
	var memberships []MemberModel
	err := r.db.WithContext(ctx).
		Table("im_conversation_member AS m").
		Select("m.*").
		Joins("JOIN im_conversation AS c ON c.id = m.conversation_id").
		Where("m.user_id = ?", userID).
		Order("COALESCE(c.last_message_at, c.created_at) DESC").
		Order("c.id DESC").
		Limit(limit).
		Scan(&memberships).
		Error
	if err != nil {
		return nil, err
	}
	if len(memberships) == 0 {
		return []*domainim.ConversationSummary{}, nil
	}

	conversationIDs := make([]int64, 0, len(memberships))
	unreadByConversation := make(map[int64]int, len(memberships))
	for _, membership := range memberships {
		conversationIDs = append(conversationIDs, membership.ConversationID)
		unreadByConversation[membership.ConversationID] = membership.UnreadCount
	}

	var conversations []ConversationModel
	if err := r.db.WithContext(ctx).Where("id IN ?", conversationIDs).Find(&conversations).Error; err != nil {
		return nil, err
	}
	conversationByID := make(map[int64]ConversationModel, len(conversations))
	lastMessageIDs := make([]int64, 0, len(conversations))
	for _, conversation := range conversations {
		conversationByID[conversation.ID] = conversation
		if conversation.LastMessageID > 0 {
			lastMessageIDs = append(lastMessageIDs, conversation.LastMessageID)
		}
	}

	membersByConversation, err := r.listMemberProfiles(ctx, conversationIDs)
	if err != nil {
		return nil, err
	}
	lastMessages, err := r.listMessageProfilesByID(ctx, lastMessageIDs)
	if err != nil {
		return nil, err
	}

	items := make([]*domainim.ConversationSummary, 0, len(conversationIDs))
	for _, conversationID := range conversationIDs {
		conversation, ok := conversationByID[conversationID]
		if !ok {
			continue
		}
		items = append(items, &domainim.ConversationSummary{
			Conversation: restoreConversation(conversation),
			Members:      membersByConversation[conversationID],
			LastMessage:  lastMessages[conversation.LastMessageID],
			UnreadCount:  unreadByConversation[conversationID],
		})
	}
	return items, nil
}

func (r *Repository) ListMessages(ctx context.Context, conversationID int64, beforeSequence int64, limit int) ([]*domainim.Message, error) {
	query := r.db.WithContext(ctx).
		Table("im_message AS m").
		Select("m.id, m.conversation_id, m.sender_id, a.nickname AS sender_nickname, a.avatar_url AS sender_avatar_url, m.type, m.content, m.video_id, m.client_message_id, m.sequence, m.created_at").
		Joins("JOIN account AS a ON a.id = m.sender_id").
		Where("m.conversation_id = ?", conversationID)
	if beforeSequence > 0 {
		query = query.Where("m.sequence < ?", beforeSequence)
	}
	var models []MessageProfileModel
	if err := query.Order("m.sequence DESC").Limit(limit).Scan(&models).Error; err != nil {
		return nil, err
	}
	items := make([]*domainim.Message, 0, len(models))
	for _, model := range models {
		items = append(items, restoreMessageProfile(model))
	}
	return items, nil
}

func (r *Repository) SendMessage(ctx context.Context, message *domainim.Message) (*domainim.Message, []int64, bool, error) {
	var saved MessageModel
	var recipientIDs []int64
	created := true
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var conversation ConversationModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", message.ConversationID).
			Take(&conversation).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domainim.ErrConversationNotFound
			}
			return err
		}

		var senderMember MemberModel
		if err := tx.Where("conversation_id = ? AND user_id = ?", message.ConversationID, message.SenderID).
			Take(&senderMember).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return domainim.ErrNotConversationMember
			}
			return err
		}

		if clientMessageID := strings.TrimSpace(message.ClientMessageID); clientMessageID != "" {
			var existing MessageModel
			result := tx.Where(
				"conversation_id = ? AND sender_id = ? AND client_message_id = ?",
				message.ConversationID,
				message.SenderID,
				clientMessageID,
			).Limit(1).Find(&existing)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected > 0 {
				saved = existing
				created = false
			}
		}
		if !created {
			var members []MemberModel
			if err := tx.Where("conversation_id = ?", message.ConversationID).Find(&members).Error; err != nil {
				return err
			}
			recipientIDs = memberIDs(members)
			return nil
		}

		nextSequence := conversation.LastSequence + 1
		now := gorm.Expr("NOW()")
		saved = MessageModel{
			ConversationID:  message.ConversationID,
			SenderID:        message.SenderID,
			Type:            message.Type,
			Content:         message.Content,
			VideoID:         message.VideoID,
			ClientMessageID: optionalString(message.ClientMessageID),
			Sequence:        nextSequence,
		}
		if err := tx.Create(&saved).Error; err != nil {
			return err
		}
		if err := tx.Model(&ConversationModel{}).
			Where("id = ?", message.ConversationID).
			Updates(map[string]any{
				"last_sequence":   nextSequence,
				"last_message_id": saved.ID,
				"last_message_at": now,
				"updated_at":      now,
			}).Error; err != nil {
			return err
		}
		if err := tx.Model(&MemberModel{}).
			Where("conversation_id = ? AND user_id <> ?", message.ConversationID, message.SenderID).
			Updates(map[string]any{
				"unread_count": gorm.Expr("unread_count + 1"),
				"updated_at":   now,
			}).Error; err != nil {
			return err
		}

		var members []MemberModel
		if err := tx.Where("conversation_id = ?", message.ConversationID).Find(&members).Error; err != nil {
			return err
		}
		recipientIDs = memberIDs(members)
		return nil
	})
	if err != nil {
		return nil, nil, false, err
	}
	profiles, err := r.listMessageProfilesByID(ctx, []int64{saved.ID})
	if err != nil {
		return nil, nil, false, err
	}
	if message := profiles[saved.ID]; message != nil {
		return message, recipientIDs, created, nil
	}
	return restoreMessage(saved), recipientIDs, created, nil
}

func (r *Repository) MarkRead(ctx context.Context, conversationID int64, userID int64, sequence int64) (int, error) {
	result := r.db.WithContext(ctx).
		Model(&MemberModel{}).
		Where("conversation_id = ? AND user_id = ?", conversationID, userID).
		Updates(map[string]any{
			"last_read_sequence": sequence,
			"unread_count":       0,
			"updated_at":         gorm.Expr("NOW()"),
		})
	if result.Error != nil {
		return 0, result.Error
	}
	return int(result.RowsAffected), nil
}

func (r *Repository) listMemberProfiles(ctx context.Context, conversationIDs []int64) (map[int64][]*domainim.Member, error) {
	var models []MemberProfileModel
	err := r.db.WithContext(ctx).
		Table("im_conversation_member AS m").
		Select("m.conversation_id, m.user_id, a.nickname, a.avatar_url, m.role, m.last_read_sequence AS last_read_seq, m.unread_count, m.joined_at, m.updated_at").
		Joins("JOIN account AS a ON a.id = m.user_id").
		Where("m.conversation_id IN ?", conversationIDs).
		Scan(&models).
		Error
	if err != nil {
		return nil, err
	}
	items := make(map[int64][]*domainim.Member, len(conversationIDs))
	for _, model := range models {
		items[model.ConversationID] = append(items[model.ConversationID], restoreMemberProfile(model))
	}
	return items, nil
}

func (r *Repository) listMessageProfilesByID(ctx context.Context, messageIDs []int64) (map[int64]*domainim.Message, error) {
	items := map[int64]*domainim.Message{}
	if len(messageIDs) == 0 {
		return items, nil
	}
	var models []MessageProfileModel
	err := r.db.WithContext(ctx).
		Table("im_message AS m").
		Select("m.id, m.conversation_id, m.sender_id, a.nickname AS sender_nickname, a.avatar_url AS sender_avatar_url, m.type, m.content, m.video_id, m.client_message_id, m.sequence, m.created_at").
		Joins("JOIN account AS a ON a.id = m.sender_id").
		Where("m.id IN ?", messageIDs).
		Scan(&models).
		Error
	if err != nil {
		return nil, err
	}
	for _, model := range models {
		items[model.ID] = restoreMessageProfile(model)
	}
	return items, nil
}

func restoreConversation(model ConversationModel) *domainim.Conversation {
	return &domainim.Conversation{
		ID:            model.ID,
		Type:          model.Type,
		Title:         strings.TrimSpace(model.Title),
		OwnerID:       model.OwnerID,
		DirectKey:     stringValue(model.DirectKey),
		LastMessageID: model.LastMessageID,
		LastMessageAt: model.LastMessageAt,
		CreatedAt:     model.CreatedAt,
		UpdatedAt:     model.UpdatedAt,
	}
}

func restoreMember(model MemberModel) *domainim.Member {
	return &domainim.Member{
		ConversationID:   model.ConversationID,
		UserID:           model.UserID,
		Role:             model.Role,
		LastReadSequence: model.LastReadSequence,
		UnreadCount:      model.UnreadCount,
		JoinedAt:         model.JoinedAt,
		UpdatedAt:        model.UpdatedAt,
	}
}

func restoreMemberProfile(model MemberProfileModel) *domainim.Member {
	return &domainim.Member{
		ConversationID:   model.ConversationID,
		UserID:           model.UserID,
		Nickname:         strings.TrimSpace(model.Nickname),
		AvatarURL:        strings.TrimSpace(model.AvatarURL),
		Role:             model.Role,
		LastReadSequence: model.LastReadSeq,
		UnreadCount:      model.UnreadCount,
		JoinedAt:         model.JoinedAt,
		UpdatedAt:        model.UpdatedAt,
	}
}

func restoreMessage(model MessageModel) *domainim.Message {
	return domainim.RestoreMessage(
		model.ID,
		model.ConversationID,
		model.SenderID,
		"",
		"",
		model.Type,
		model.Content,
		model.VideoID,
		stringValue(model.ClientMessageID),
		model.Sequence,
		model.CreatedAt,
	)
}

func restoreMessageProfile(model MessageProfileModel) *domainim.Message {
	return domainim.RestoreMessage(
		model.ID,
		model.ConversationID,
		model.SenderID,
		model.SenderNickname,
		model.SenderAvatarURL,
		model.Type,
		model.Content,
		model.VideoID,
		stringValue(model.ClientMessageID),
		model.Sequence,
		model.CreatedAt,
	)
}

func memberIDs(models []MemberModel) []int64 {
	ids := make([]int64, 0, len(models))
	for _, model := range models {
		if model.UserID > 0 {
			ids = append(ids, model.UserID)
		}
	}
	return ids
}

func optionalString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func isDuplicateKeyError(err error) bool {
	return errors.Is(err, gorm.ErrDuplicatedKey) || strings.Contains(strings.ToLower(err.Error()), "duplicate")
}
