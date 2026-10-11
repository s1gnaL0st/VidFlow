package applicationexposure

import (
	domainexposure "GCFeed/internal/domain/exposure"
	domainoutbox "GCFeed/internal/domain/outbox"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var ErrSaveExposureFailed = errors.New("failed to save exposure")
var ErrBuildViewEventOutboxFailed = errors.New("failed to build view event outbox")

const defaultViewEventOutboxExchange = "gcfeed.exposure"
const defaultViewEventOutboxRoutingKey = "exposure.view_event_recorded"

type Service struct {
	repo           domainexposure.Repository
	publisher      ViewEventPublisher
	outboxEnabled  bool
	outboxExchange string
	outboxRouting  string
}

type RecordViewEventResult struct {
	Event    *domainexposure.ViewEvent
	Exposure *domainexposure.Exposure
}

// ViewEventPublisher 投递观看行为事件，推荐画像 worker 基于该事件更新用户向量。
type ViewEventPublisher interface {
	PublishViewEventRecorded(ctx context.Context, event *ViewEventRecordedEvent) error
}

type viewEventOutboxRepository interface {
	SaveViewEventWithOutbox(
		ctx context.Context,
		event *domainexposure.ViewEvent,
		factory func(*domainexposure.ViewEvent, *domainexposure.Exposure) (*domainoutbox.Event, error),
	) (*domainexposure.ViewEvent, *domainexposure.Exposure, error)
}

type Option func(*Service)

func New(repo domainexposure.Repository, options ...Option) *Service {
	service := &Service{repo: repo}
	for _, option := range options {
		option(service)
	}
	return service
}

// WithViewEventPublisher 为曝光服务启用观看行为事件发布。
func WithViewEventPublisher(publisher ViewEventPublisher) Option {
	return func(s *Service) {
		s.publisher = publisher
	}
}

// WithViewEventOutbox 让观看事件先写入数据库 Outbox，再由 Dispatcher 投递。
func WithViewEventOutbox(exchange string, routingKey string) Option {
	return func(s *Service) {
		exchange = strings.TrimSpace(exchange)
		routingKey = strings.TrimSpace(routingKey)
		if exchange == "" {
			exchange = defaultViewEventOutboxExchange
		}
		if routingKey == "" {
			routingKey = defaultViewEventOutboxRoutingKey
		}
		s.outboxEnabled = true
		s.outboxExchange = exchange
		s.outboxRouting = routingKey
	}
}

// RecordViewEvent 写入观看行为，并在 exposed 事件时同步维护曝光聚合索引。
func (s *Service) RecordViewEvent(ctx context.Context, userID int64, videoID int64, scene string, requestID string, eventType string, watchMs int, completed bool) (*RecordViewEventResult, error) {
	event, err := domainexposure.NewViewEvent(userID, videoID, scene, requestID, eventType, watchMs, completed)
	if err != nil {
		return nil, err
	}

	var savedEvent *domainexposure.ViewEvent
	var exposure *domainexposure.Exposure
	savedWithOutbox := false
	if repo, ok := s.repo.(viewEventOutboxRepository); ok && s.outboxEnabled {
		savedEvent, exposure, err = repo.SaveViewEventWithOutbox(ctx, event, s.newViewEventOutboxEvent)
		savedWithOutbox = true
	} else {
		savedEvent, exposure, err = s.repo.SaveViewEvent(ctx, event)
	}
	if err != nil {
		if errors.Is(err, domainexposure.ErrVideoNotFound) {
			return nil, err
		}
		return nil, ErrSaveExposureFailed
	}
	if !savedWithOutbox {
		s.publishViewEventRecorded(ctx, savedEvent, exposure)
	}

	return &RecordViewEventResult{
		Event:    savedEvent,
		Exposure: exposure,
	}, nil
}

func (s *Service) newViewEventOutboxEvent(event *domainexposure.ViewEvent, exposure *domainexposure.Exposure) (*domainoutbox.Event, error) {
	message := NewViewEventRecordedEvent(event, exposure)
	if message == nil {
		return nil, ErrBuildViewEventOutboxFailed
	}
	payload, err := json.Marshal(message)
	if err != nil {
		return nil, ErrBuildViewEventOutboxFailed
	}
	outboxEvent, err := domainoutbox.NewEvent(
		message.EventID,
		s.outboxExchange,
		s.outboxRouting,
		payload,
		time.Now().UTC(),
	)
	if err != nil {
		return nil, ErrBuildViewEventOutboxFailed
	}
	return outboxEvent, nil
}

func (s *Service) publishViewEventRecorded(ctx context.Context, event *domainexposure.ViewEvent, exposure *domainexposure.Exposure) {
	if s.publisher == nil {
		return
	}
	message := NewViewEventRecordedEvent(event, exposure)
	if message == nil {
		return
	}
	_ = s.publisher.PublishViewEventRecorded(ctx, message)
}
