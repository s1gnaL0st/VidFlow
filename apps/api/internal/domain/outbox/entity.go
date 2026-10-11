package domainoutbox

import (
	"errors"
	"strings"
	"time"
)

const (
	StatusPending   = 1
	StatusPublished = 2
	StatusDead      = 3
)

var ErrInvalidEvent = errors.New("invalid outbox event")

// Event 保存待投递的领域事件，业务写入和事件入队可以共享同一数据库事务。
type Event struct {
	ID          string
	Exchange    string
	RoutingKey  string
	Payload     []byte
	Status      int
	RetryCount  int
	NextRetryAt time.Time
	LastError   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	PublishedAt *time.Time
}

func NewEvent(id string, exchange string, routingKey string, payload []byte, now time.Time) (*Event, error) {
	id = strings.TrimSpace(id)
	exchange = strings.TrimSpace(exchange)
	routingKey = strings.TrimSpace(routingKey)
	if id == "" || exchange == "" || routingKey == "" || len(payload) == 0 {
		return nil, ErrInvalidEvent
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return &Event{
		ID:          id,
		Exchange:    exchange,
		RoutingKey:  routingKey,
		Payload:     append([]byte(nil), payload...),
		Status:      StatusPending,
		NextRetryAt: now.UTC(),
		CreatedAt:   now.UTC(),
		UpdatedAt:   now.UTC(),
	}, nil
}

func (e *Event) MarkPublished(now time.Time) {
	if e == nil {
		return
	}
	now = now.UTC()
	e.Status = StatusPublished
	e.LastError = ""
	e.PublishedAt = &now
	e.UpdatedAt = now
}

func (e *Event) MarkRetry(nextRetryAt time.Time, lastError string) {
	if e == nil {
		return
	}
	e.Status = StatusPending
	e.RetryCount++
	e.NextRetryAt = nextRetryAt.UTC()
	e.LastError = strings.TrimSpace(lastError)
	e.UpdatedAt = time.Now().UTC()
}

func (e *Event) MarkDead(now time.Time, lastError string) {
	if e == nil {
		return
	}
	now = now.UTC()
	e.Status = StatusDead
	e.RetryCount++
	e.NextRetryAt = now
	e.LastError = strings.TrimSpace(lastError)
	e.UpdatedAt = now
}
