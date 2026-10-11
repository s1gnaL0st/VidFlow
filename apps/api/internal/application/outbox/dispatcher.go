package applicationoutbox

import (
	domainoutbox "GCFeed/internal/domain/outbox"
	inframetrics "GCFeed/internal/infra/metrics"
	"context"
	"errors"
	"log"
	"time"
)

const defaultDispatchInterval = time.Second
const defaultDispatchBatchSize = 50
const defaultMaxRetryAttempts = 8
const defaultRetryBaseDelay = time.Second
const defaultRetryMaxDelay = 5 * time.Minute

type Publisher interface {
	PublishOutboxEvent(ctx context.Context, event *domainoutbox.Event) error
}

type Dispatcher struct {
	repo        domainoutbox.Repository
	publisher   Publisher
	interval    time.Duration
	batchSize   int
	maxAttempts int
	baseDelay   time.Duration
	maxDelay    time.Duration
	now         func() time.Time
}

type Option func(*Dispatcher)

func New(repo domainoutbox.Repository, publisher Publisher, options ...Option) *Dispatcher {
	dispatcher := &Dispatcher{
		repo:        repo,
		publisher:   publisher,
		interval:    defaultDispatchInterval,
		batchSize:   defaultDispatchBatchSize,
		maxAttempts: defaultMaxRetryAttempts,
		baseDelay:   defaultRetryBaseDelay,
		maxDelay:    defaultRetryMaxDelay,
		now:         func() time.Time { return time.Now().UTC() },
	}
	for _, option := range options {
		option(dispatcher)
	}
	return dispatcher
}

func WithInterval(interval time.Duration) Option {
	return func(d *Dispatcher) {
		if interval > 0 {
			d.interval = interval
		}
	}
}

func WithBatchSize(size int) Option {
	return func(d *Dispatcher) {
		if size > 0 {
			d.batchSize = size
		}
	}
}

func WithRetryPolicy(maxAttempts int, baseDelay time.Duration, maxDelay time.Duration) Option {
	return func(d *Dispatcher) {
		if maxAttempts > 0 {
			d.maxAttempts = maxAttempts
		}
		if baseDelay > 0 {
			d.baseDelay = baseDelay
		}
		if maxDelay > 0 {
			d.maxDelay = maxDelay
		}
	}
}

func WithNow(now func() time.Time) Option {
	return func(d *Dispatcher) {
		if now != nil {
			d.now = now
		}
	}
}

func (d *Dispatcher) Run(ctx context.Context) {
	if d == nil || d.repo == nil || d.publisher == nil {
		return
	}
	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()
	for {
		if err := d.DispatchOnce(ctx); err != nil {
			log.Printf("outbox dispatch failed: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (d *Dispatcher) DispatchOnce(ctx context.Context) error {
	if d == nil || d.repo == nil || d.publisher == nil {
		return nil
	}
	events, err := d.repo.ListPending(ctx, d.now(), d.batchSize)
	if err != nil {
		return err
	}
	var dispatchErrors []error
	for _, event := range events {
		if err := d.dispatchEvent(ctx, event); err != nil {
			dispatchErrors = append(dispatchErrors, err)
		}
	}
	return errors.Join(dispatchErrors...)
}

func (d *Dispatcher) dispatchEvent(ctx context.Context, event *domainoutbox.Event) error {
	if event == nil {
		return nil
	}
	start := time.Now()
	err := d.publisher.PublishOutboxEvent(ctx, event)
	if err == nil {
		inframetrics.ObserveWorkerJob("outbox_dispatch", time.Since(start), nil)
		return d.repo.MarkPublished(ctx, event.ID, d.now())
	}

	inframetrics.ObserveWorkerJob("outbox_dispatch", time.Since(start), err)
	nextAttempt := event.RetryCount + 1
	if nextAttempt >= d.maxAttempts {
		return d.repo.MarkDead(ctx, event.ID, err.Error())
	}
	return d.repo.MarkRetry(ctx, event.ID, d.now().Add(d.retryDelay(nextAttempt)), err.Error())
}

func (d *Dispatcher) retryDelay(attempt int) time.Duration {
	if attempt <= 1 {
		return d.baseDelay
	}
	delay := d.baseDelay
	for index := 1; index < attempt && delay < d.maxDelay; index++ {
		delay *= 2
		if delay > d.maxDelay {
			return d.maxDelay
		}
	}
	return delay
}
