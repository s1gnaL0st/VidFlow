package applicationoutbox

import (
	domainoutbox "GCFeed/internal/domain/outbox"
	"context"
	"errors"
	"testing"
	"time"
)

type fakeOutboxRepository struct {
	events         []*domainoutbox.Event
	publishedIDs   []string
	retried        map[string]time.Time
	deadIDs        []string
	lastRetryError string
}

func (r *fakeOutboxRepository) ListPending(ctx context.Context, now time.Time, limit int) ([]*domainoutbox.Event, error) {
	return r.events, nil
}

func (r *fakeOutboxRepository) MarkPublished(ctx context.Context, eventID string, publishedAt time.Time) error {
	r.publishedIDs = append(r.publishedIDs, eventID)
	return nil
}

func (r *fakeOutboxRepository) MarkRetry(ctx context.Context, eventID string, nextRetryAt time.Time, lastError string) error {
	if r.retried == nil {
		r.retried = map[string]time.Time{}
	}
	r.retried[eventID] = nextRetryAt
	r.lastRetryError = lastError
	return nil
}

func (r *fakeOutboxRepository) MarkDead(ctx context.Context, eventID string, lastError string) error {
	r.deadIDs = append(r.deadIDs, eventID)
	r.lastRetryError = lastError
	return nil
}

type fakeOutboxPublisher struct {
	err error
}

func (p fakeOutboxPublisher) PublishOutboxEvent(ctx context.Context, event *domainoutbox.Event) error {
	return p.err
}

func TestDispatcherMarksPublished(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	repo := &fakeOutboxRepository{
		events: []*domainoutbox.Event{{ID: "event-1"}},
	}
	dispatcher := New(repo, fakeOutboxPublisher{}, WithNow(func() time.Time { return now }))

	if err := dispatcher.DispatchOnce(context.Background()); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(repo.publishedIDs) != 1 || repo.publishedIDs[0] != "event-1" {
		t.Fatalf("unexpected published ids: %+v", repo.publishedIDs)
	}
}

func TestDispatcherSchedulesRetry(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	repo := &fakeOutboxRepository{
		events: []*domainoutbox.Event{{ID: "event-1", RetryCount: 0}},
	}
	dispatcher := New(
		repo,
		fakeOutboxPublisher{err: errors.New("publish failed")},
		WithNow(func() time.Time { return now }),
		WithRetryPolicy(3, 2*time.Second, time.Minute),
	)

	if err := dispatcher.DispatchOnce(context.Background()); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if got := repo.retried["event-1"]; !got.Equal(now.Add(2 * time.Second)) {
		t.Fatalf("unexpected retry time: %s", got)
	}
	if repo.lastRetryError != "publish failed" {
		t.Fatalf("unexpected retry error: %s", repo.lastRetryError)
	}
}

func TestDispatcherMarksDeadAfterMaxRetries(t *testing.T) {
	repo := &fakeOutboxRepository{
		events: []*domainoutbox.Event{{ID: "event-1", RetryCount: 2}},
	}
	dispatcher := New(
		repo,
		fakeOutboxPublisher{err: errors.New("publish failed")},
		WithRetryPolicy(3, time.Second, time.Minute),
	)

	if err := dispatcher.DispatchOnce(context.Background()); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(repo.deadIDs) != 1 || repo.deadIDs[0] != "event-1" {
		t.Fatalf("unexpected dead ids: %+v", repo.deadIDs)
	}
}
