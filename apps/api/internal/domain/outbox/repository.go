package domainoutbox

import (
	"context"
	"time"
)

// Repository 定义 Outbox 事件的扫描和状态更新能力。
type Repository interface {
	ListPending(ctx context.Context, now time.Time, limit int) ([]*Event, error)
	MarkPublished(ctx context.Context, eventID string, publishedAt time.Time) error
	MarkRetry(ctx context.Context, eventID string, nextRetryAt time.Time, lastError string) error
	MarkDead(ctx context.Context, eventID string, lastError string) error
}
