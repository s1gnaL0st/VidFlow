package inframim

import "time"

type ConversationModel struct {
	ID            int64      `gorm:"column:id;primaryKey;autoIncrement"`
	Type          string     `gorm:"column:type;size:16;not null"`
	Title         string     `gorm:"column:title;size:128"`
	OwnerID       int64      `gorm:"column:owner_id;not null;default:0"`
	DirectKey     *string    `gorm:"column:direct_key;size:64;uniqueIndex"`
	LastSequence  int64      `gorm:"column:last_sequence;not null;default:0"`
	LastMessageID int64      `gorm:"column:last_message_id;not null;default:0"`
	LastMessageAt *time.Time `gorm:"column:last_message_at"`
	CreatedAt     time.Time  `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt     time.Time  `gorm:"column:updated_at;autoUpdateTime;index:idx_updated"`
}

func (ConversationModel) TableName() string {
	return "im_conversation"
}

type MemberModel struct {
	ID               int64     `gorm:"column:id;primaryKey;autoIncrement"`
	ConversationID   int64     `gorm:"column:conversation_id;not null;uniqueIndex:uk_conversation_user,priority:1;index:idx_user_updated,priority:1"`
	UserID           int64     `gorm:"column:user_id;not null;uniqueIndex:uk_conversation_user,priority:2;index:idx_user_updated,priority:2"`
	Role             string    `gorm:"column:role;size:16;not null"`
	LastReadSequence int64     `gorm:"column:last_read_sequence;not null;default:0"`
	UnreadCount      int       `gorm:"column:unread_count;not null;default:0"`
	JoinedAt         time.Time `gorm:"column:joined_at;autoCreateTime"`
	UpdatedAt        time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (MemberModel) TableName() string {
	return "im_conversation_member"
}

type MessageModel struct {
	ID              int64     `gorm:"column:id;primaryKey;autoIncrement"`
	ConversationID  int64     `gorm:"column:conversation_id;not null;uniqueIndex:uk_conversation_sequence,priority:1;uniqueIndex:uk_conversation_client,priority:1;index:idx_conversation_sequence,priority:1"`
	SenderID        int64     `gorm:"column:sender_id;not null;uniqueIndex:uk_conversation_client,priority:2"`
	Type            string    `gorm:"column:type;size:16;not null"`
	Content         string    `gorm:"column:content;size:4000"`
	VideoID         int64     `gorm:"column:video_id;not null;default:0"`
	ClientMessageID *string   `gorm:"column:client_message_id;size:128;uniqueIndex:uk_conversation_client,priority:3"`
	Sequence        int64     `gorm:"column:sequence;not null;uniqueIndex:uk_conversation_sequence,priority:2;index:idx_conversation_sequence,priority:2"`
	CreatedAt       time.Time `gorm:"column:created_at;autoCreateTime;index:idx_conversation_sequence,priority:3"`
}

func (MessageModel) TableName() string {
	return "im_message"
}

type MemberProfileModel struct {
	ConversationID int64
	UserID         int64
	Nickname       string
	AvatarURL      string
	Role           string
	LastReadSeq    int64
	UnreadCount    int
	JoinedAt       time.Time
	UpdatedAt      time.Time
}

type MessageProfileModel struct {
	ID              int64
	ConversationID  int64
	SenderID        int64
	SenderNickname  string
	SenderAvatarURL string
	Type            string
	Content         string
	VideoID         int64
	ClientMessageID *string
	Sequence        int64
	CreatedAt       time.Time
}
