package infraoutbox

import (
	domainoutbox "GCFeed/internal/domain/outbox"
	"context"
	"time"

	"gorm.io/gorm"
)

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) ListPending(ctx context.Context, now time.Time, limit int) ([]*domainoutbox.Event, error) {
	if limit <= 0 {
		return []*domainoutbox.Event{}, nil
	}
	var models []OutboxModel
	err := r.db.WithContext(ctx).
		Where("status = ? AND next_retry_at <= ?", domainoutbox.StatusPending, now.UTC()).
		Order("next_retry_at ASC").
		Order("id ASC").
		Limit(limit).
		Find(&models).
		Error
	if err != nil {
		return nil, err
	}
	events := make([]*domainoutbox.Event, 0, len(models))
	for _, model := range models {
		events = append(events, restoreEvent(model))
	}
	return events, nil
}

func (r *Repository) MarkPublished(ctx context.Context, eventID string, publishedAt time.Time) error {
	return r.db.WithContext(ctx).
		Model(&OutboxModel{}).
		Where("event_id = ?", eventID).
		Updates(map[string]any{
			"status":       domainoutbox.StatusPublished,
			"last_error":   "",
			"published_at": publishedAt.UTC(),
			"updated_at":   publishedAt.UTC(),
		}).
		Error
}

func (r *Repository) MarkRetry(ctx context.Context, eventID string, nextRetryAt time.Time, lastError string) error {
	return r.db.WithContext(ctx).
		Model(&OutboxModel{}).
		Where("event_id = ?", eventID).
		Updates(map[string]any{
			"status":        domainoutbox.StatusPending,
			"retry_count":   gorm.Expr("retry_count + 1"),
			"next_retry_at": nextRetryAt.UTC(),
			"last_error":    lastError,
			"updated_at":    time.Now().UTC(),
		}).
		Error
}

func (r *Repository) MarkDead(ctx context.Context, eventID string, lastError string) error {
	now := time.Now().UTC()
	return r.db.WithContext(ctx).
		Model(&OutboxModel{}).
		Where("event_id = ?", eventID).
		Updates(map[string]any{
			"status":        domainoutbox.StatusDead,
			"retry_count":   gorm.Expr("retry_count + 1"),
			"next_retry_at": now,
			"last_error":    lastError,
			"updated_at":    now,
		}).
		Error
}
