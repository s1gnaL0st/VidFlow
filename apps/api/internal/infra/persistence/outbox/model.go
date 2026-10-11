package infraoutbox

import (
	domainoutbox "GCFeed/internal/domain/outbox"
	"time"
)

// OutboxModel 映射 event_outbox 表，保存尚未可靠投递到 RabbitMQ 的事件。
type OutboxModel struct {
	ID          int64      `gorm:"column:id;primaryKey;autoIncrement"`
	EventID     string     `gorm:"column:event_id;size:64;not null;uniqueIndex:uk_event_id"`
	Exchange    string     `gorm:"column:exchange;size:128;not null"`
	RoutingKey  string     `gorm:"column:routing_key;size:128;not null"`
	Payload     string     `gorm:"column:payload;type:json;not null"`
	Status      int        `gorm:"column:status;not null;index:idx_status_retry,priority:1"`
	RetryCount  int        `gorm:"column:retry_count;not null;default:0"`
	NextRetryAt time.Time  `gorm:"column:next_retry_at;not null;index:idx_status_retry,priority:2"`
	LastError   string     `gorm:"column:last_error;size:512"`
	CreatedAt   time.Time  `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt   time.Time  `gorm:"column:updated_at;autoUpdateTime"`
	PublishedAt *time.Time `gorm:"column:published_at"`
}

func (OutboxModel) TableName() string {
	return "event_outbox"
}

func ModelFromEvent(event *domainoutbox.Event) OutboxModel {
	if event == nil {
		return OutboxModel{}
	}
	return OutboxModel{
		EventID:     event.ID,
		Exchange:    event.Exchange,
		RoutingKey:  event.RoutingKey,
		Payload:     string(event.Payload),
		Status:      event.Status,
		RetryCount:  event.RetryCount,
		NextRetryAt: event.NextRetryAt,
		LastError:   event.LastError,
		CreatedAt:   event.CreatedAt,
		UpdatedAt:   event.UpdatedAt,
		PublishedAt: event.PublishedAt,
	}
}

func restoreEvent(model OutboxModel) *domainoutbox.Event {
	return &domainoutbox.Event{
		ID:          model.EventID,
		Exchange:    model.Exchange,
		RoutingKey:  model.RoutingKey,
		Payload:     []byte(model.Payload),
		Status:      model.Status,
		RetryCount:  model.RetryCount,
		NextRetryAt: model.NextRetryAt,
		LastError:   model.LastError,
		CreatedAt:   model.CreatedAt,
		UpdatedAt:   model.UpdatedAt,
		PublishedAt: model.PublishedAt,
	}
}
