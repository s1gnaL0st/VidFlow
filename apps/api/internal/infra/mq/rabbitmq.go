package inframq

import (
	applicationexposure "GCFeed/internal/application/exposure"
	applicationinteraction "GCFeed/internal/application/interaction"
	applicationvideo "GCFeed/internal/application/video"
	domainoutbox "GCFeed/internal/domain/outbox"
	infraconfig "GCFeed/internal/infra/config"
	inframetrics "GCFeed/internal/infra/metrics"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const defaultInteractionExchange = "gcfeed.interaction"
const defaultActionChangedQueue = "gcfeed.interaction.action_changed"
const defaultActionChangedRouting = "interaction.action_changed"
const defaultVideoExchange = "gcfeed.video"
const defaultVideoPublishedQueue = "gcfeed.video.published"
const defaultVideoEmbeddingQueue = "gcfeed.video.embedding"
const defaultVideoPublishedRouting = "video.published"
const defaultExposureExchange = "gcfeed.exposure"
const defaultViewEventRecordedQueue = "gcfeed.exposure.view_event_recorded"
const defaultViewEventRecordedRouting = "exposure.view_event_recorded"
const defaultMaxDeliveryRetries = 5
const defaultRetryQueueTTL = 5 * time.Second

var ErrEmptyRabbitMQURL = errors.New("rabbitmq url is empty")

type RabbitMQ struct {
	conn            *amqp.Connection
	publishChannel  *amqp.Channel
	consumerChannel *amqp.Channel
	config          infraconfig.RabbitMQConfig
	publishMu       sync.Mutex
	confirmations   <-chan amqp.Confirmation
}

func NewRabbitMQ(cfg infraconfig.RabbitMQConfig) (*RabbitMQ, error) {
	cfg = normalizeRabbitMQConfig(cfg)
	if strings.TrimSpace(cfg.URL) == "" {
		return nil, ErrEmptyRabbitMQURL
	}

	conn, err := amqp.Dial(cfg.URL)
	if err != nil {
		return nil, err
	}
	channel, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	consumerChannel, err := conn.Channel()
	if err != nil {
		_ = channel.Close()
		_ = conn.Close()
		return nil, err
	}
	if err := channel.Confirm(false); err != nil {
		_ = consumerChannel.Close()
		_ = channel.Close()
		_ = conn.Close()
		return nil, err
	}

	client := &RabbitMQ{
		conn:            conn,
		publishChannel:  channel,
		consumerChannel: consumerChannel,
		config:          cfg,
		confirmations:   channel.NotifyPublish(make(chan amqp.Confirmation, 1)),
	}
	if err := client.ensureTopology(); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

func (r *RabbitMQ) Close() error {
	if r == nil {
		return nil
	}
	if r.publishChannel != nil {
		_ = r.publishChannel.Close()
	}
	if r.consumerChannel != nil {
		_ = r.consumerChannel.Close()
	}
	if r.conn != nil {
		return r.conn.Close()
	}
	return nil
}

func (r *RabbitMQ) PublishActionChanged(ctx context.Context, event *applicationinteraction.ActionChangedEvent) error {
	if event == nil {
		return nil
	}
	content, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return r.publishConfirmed(
		ctx,
		r.config.InteractionExchange,
		r.config.ActionChangedRouting,
		event.EventID,
		content,
	)
}

func (r *RabbitMQ) PublishVideoPublished(ctx context.Context, event *applicationvideo.PublishedEvent) error {
	if event == nil {
		return nil
	}
	content, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return r.publishConfirmed(
		ctx,
		r.config.VideoExchange,
		r.config.VideoPublishedRouting,
		event.EventID,
		content,
	)
}

func (r *RabbitMQ) PublishViewEventRecorded(ctx context.Context, event *applicationexposure.ViewEventRecordedEvent) error {
	if event == nil {
		return nil
	}
	content, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return r.publishConfirmed(
		ctx,
		r.config.ExposureExchange,
		r.config.ViewEventRecordedRouting,
		event.EventID,
		content,
	)
}

// PublishOutboxEvent 投递 Outbox Dispatcher 扫描到的持久化事件。
func (r *RabbitMQ) PublishOutboxEvent(ctx context.Context, event *domainoutbox.Event) error {
	if event == nil {
		return nil
	}
	return r.publishConfirmed(ctx, event.Exchange, event.RoutingKey, event.ID, event.Payload)
}

func (r *RabbitMQ) publishConfirmed(ctx context.Context, exchange string, routingKey string, messageID string, body []byte) error {
	return r.publishConfirmedWithHeaders(ctx, exchange, routingKey, messageID, body, nil)
}

func (r *RabbitMQ) publishConfirmedWithHeaders(
	ctx context.Context,
	exchange string,
	routingKey string,
	messageID string,
	body []byte,
	headers amqp.Table,
) error {
	r.publishMu.Lock()
	defer r.publishMu.Unlock()

	if err := r.publishChannel.PublishWithContext(
		ctx,
		exchange,
		routingKey,
		false,
		false,
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			MessageId:    messageID,
			Timestamp:    time.Now(),
			Headers:      headers,
			Body:         body,
		},
	); err != nil {
		return err
	}

	select {
	case confirmation, ok := <-r.confirmations:
		if !ok {
			return errors.New("rabbitmq publisher confirmations closed")
		}
		if !confirmation.Ack {
			return fmt.Errorf("rabbitmq rejected message %s", messageID)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *RabbitMQ) retryOrDeadLetter(ctx context.Context, delivery amqp.Delivery, queue string, cause error) {
	retryCount := deliveryRetryCount(delivery.Headers)
	targetQueue := retryQueueName(queue)
	headers := cloneDeliveryHeaders(delivery.Headers)
	if retryCount >= defaultMaxDeliveryRetries {
		targetQueue = deadQueueName(queue)
		headers["x-dead-reason"] = cause.Error()
	} else {
		headers["x-retry-count"] = retryCount + 1
	}
	if err := r.publishConfirmedWithHeaders(ctx, "", targetQueue, delivery.MessageId, delivery.Body, headers); err != nil {
		_ = delivery.Nack(false, true)
		return
	}
	_ = delivery.Ack(false)
}

func (r *RabbitMQ) deadLetter(ctx context.Context, delivery amqp.Delivery, queue string, cause error) {
	headers := cloneDeliveryHeaders(delivery.Headers)
	headers["x-dead-reason"] = cause.Error()
	if err := r.publishConfirmedWithHeaders(ctx, "", deadQueueName(queue), delivery.MessageId, delivery.Body, headers); err != nil {
		_ = delivery.Nack(false, true)
		return
	}
	_ = delivery.Ack(false)
}

func retryQueueName(queue string) string {
	return queue + ".retry"
}

func deadQueueName(queue string) string {
	return queue + ".dead"
}

func deliveryRetryCount(headers amqp.Table) int {
	value, ok := headers["x-retry-count"]
	if !ok {
		return 0
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func cloneDeliveryHeaders(headers amqp.Table) amqp.Table {
	cloned := amqp.Table{}
	for key, value := range headers {
		cloned[key] = value
	}
	return cloned
}

func (r *RabbitMQ) ConsumeActionChanged(ctx context.Context, handler func(context.Context, *applicationinteraction.ActionChangedEvent) error) error {
	deliveries, err := r.consumerChannel.ConsumeWithContext(
		ctx,
		r.config.ActionChangedQueue,
		"",
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return err
	}

	go func() {
		for delivery := range deliveries {
			start := time.Now()
			var event applicationinteraction.ActionChangedEvent
			if err := json.Unmarshal(delivery.Body, &event); err != nil {
				inframetrics.ObserveWorkerJob("mq_action_changed_decode", time.Since(start), err)
				r.deadLetter(ctx, delivery, r.config.ActionChangedQueue, err)
				continue
			}
			if err := handler(ctx, &event); err != nil {
				inframetrics.ObserveWorkerJob("mq_action_changed_consume", time.Since(start), err)
				r.retryOrDeadLetter(ctx, delivery, r.config.ActionChangedQueue, err)
				continue
			}
			inframetrics.ObserveWorkerJob("mq_action_changed_consume", time.Since(start), nil)
			_ = delivery.Ack(false)
		}
	}()
	return nil
}

func (r *RabbitMQ) ConsumeVideoPublished(ctx context.Context, handler func(context.Context, *applicationvideo.PublishedEvent) error) error {
	return r.consumeVideoPublishedQueue(ctx, r.config.VideoPublishedQueue, handler)
}

func (r *RabbitMQ) ConsumeVideoPublishedForEmbedding(ctx context.Context, handler func(context.Context, *applicationvideo.PublishedEvent) error) error {
	return r.consumeVideoPublishedQueue(ctx, r.config.VideoEmbeddingQueue, handler)
}

func (r *RabbitMQ) consumeVideoPublishedQueue(ctx context.Context, queue string, handler func(context.Context, *applicationvideo.PublishedEvent) error) error {
	deliveries, err := r.consumerChannel.ConsumeWithContext(
		ctx,
		queue,
		"",
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return err
	}

	go func() {
		for delivery := range deliveries {
			start := time.Now()
			var event applicationvideo.PublishedEvent
			if err := json.Unmarshal(delivery.Body, &event); err != nil {
				inframetrics.ObserveWorkerJob("mq_video_published_decode", time.Since(start), err)
				r.deadLetter(ctx, delivery, queue, err)
				continue
			}
			if err := handler(ctx, &event); err != nil {
				inframetrics.ObserveWorkerJob("mq_video_published_consume", time.Since(start), err)
				r.retryOrDeadLetter(ctx, delivery, queue, err)
				continue
			}
			inframetrics.ObserveWorkerJob("mq_video_published_consume", time.Since(start), nil)
			_ = delivery.Ack(false)
		}
	}()
	return nil
}

func (r *RabbitMQ) ConsumeViewEventRecorded(ctx context.Context, handler func(context.Context, *applicationexposure.ViewEventRecordedEvent) error) error {
	deliveries, err := r.consumerChannel.ConsumeWithContext(
		ctx,
		r.config.ViewEventRecordedQueue,
		"",
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return err
	}

	go func() {
		for delivery := range deliveries {
			start := time.Now()
			var event applicationexposure.ViewEventRecordedEvent
			if err := json.Unmarshal(delivery.Body, &event); err != nil {
				inframetrics.ObserveWorkerJob("mq_view_event_decode", time.Since(start), err)
				r.deadLetter(ctx, delivery, r.config.ViewEventRecordedQueue, err)
				continue
			}
			if err := handler(ctx, &event); err != nil {
				inframetrics.ObserveWorkerJob("mq_view_event_consume", time.Since(start), err)
				r.retryOrDeadLetter(ctx, delivery, r.config.ViewEventRecordedQueue, err)
				continue
			}
			inframetrics.ObserveWorkerJob("mq_view_event_consume", time.Since(start), nil)
			_ = delivery.Ack(false)
		}
	}()
	return nil
}

func (r *RabbitMQ) ensureTopology() error {
	if err := r.publishChannel.ExchangeDeclare(
		r.config.InteractionExchange,
		"topic",
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		return err
	}
	if err := r.declareReliableQueue(
		r.config.ActionChangedQueue,
		r.config.InteractionExchange,
		r.config.ActionChangedRouting,
	); err != nil {
		return err
	}
	if err := r.publishChannel.ExchangeDeclare(
		r.config.VideoExchange,
		"topic",
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		return err
	}
	if err := r.declareReliableQueue(
		r.config.VideoPublishedQueue,
		r.config.VideoExchange,
		r.config.VideoPublishedRouting,
	); err != nil {
		return err
	}
	if err := r.declareReliableQueue(
		r.config.VideoEmbeddingQueue,
		r.config.VideoExchange,
		r.config.VideoPublishedRouting,
	); err != nil {
		return err
	}
	if err := r.publishChannel.ExchangeDeclare(
		r.config.ExposureExchange,
		"topic",
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		return err
	}
	return r.declareReliableQueue(
		r.config.ViewEventRecordedQueue,
		r.config.ExposureExchange,
		r.config.ViewEventRecordedRouting,
	)
}

func (r *RabbitMQ) declareReliableQueue(queue string, exchange string, routingKey string) error {
	if _, err := r.publishChannel.QueueDeclare(
		retryQueueName(queue),
		true,
		false,
		false,
		false,
		amqp.Table{
			"x-message-ttl":             int32(defaultRetryQueueTTL / time.Millisecond),
			"x-dead-letter-exchange":    exchange,
			"x-dead-letter-routing-key": routingKey,
		},
	); err != nil {
		return err
	}
	if _, err := r.publishChannel.QueueDeclare(
		deadQueueName(queue),
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		return err
	}
	if _, err := r.publishChannel.QueueDeclare(
		queue,
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		return err
	}
	return r.publishChannel.QueueBind(queue, routingKey, exchange, false, nil)
}

func normalizeRabbitMQConfig(cfg infraconfig.RabbitMQConfig) infraconfig.RabbitMQConfig {
	cfg.URL = strings.TrimSpace(cfg.URL)
	cfg.InteractionExchange = strings.TrimSpace(cfg.InteractionExchange)
	cfg.ActionChangedQueue = strings.TrimSpace(cfg.ActionChangedQueue)
	cfg.ActionChangedRouting = strings.TrimSpace(cfg.ActionChangedRouting)
	if cfg.InteractionExchange == "" {
		cfg.InteractionExchange = defaultInteractionExchange
	}
	if cfg.ActionChangedQueue == "" {
		cfg.ActionChangedQueue = defaultActionChangedQueue
	}
	if cfg.ActionChangedRouting == "" {
		cfg.ActionChangedRouting = defaultActionChangedRouting
	}
	if cfg.VideoExchange == "" {
		cfg.VideoExchange = defaultVideoExchange
	}
	if cfg.VideoPublishedQueue == "" {
		cfg.VideoPublishedQueue = defaultVideoPublishedQueue
	}
	cfg.VideoEmbeddingQueue = strings.TrimSpace(cfg.VideoEmbeddingQueue)
	if cfg.VideoEmbeddingQueue == "" {
		cfg.VideoEmbeddingQueue = defaultVideoEmbeddingQueue
	}
	if cfg.VideoPublishedRouting == "" {
		cfg.VideoPublishedRouting = defaultVideoPublishedRouting
	}
	cfg.ExposureExchange = strings.TrimSpace(cfg.ExposureExchange)
	cfg.ViewEventRecordedQueue = strings.TrimSpace(cfg.ViewEventRecordedQueue)
	cfg.ViewEventRecordedRouting = strings.TrimSpace(cfg.ViewEventRecordedRouting)
	if cfg.ExposureExchange == "" {
		cfg.ExposureExchange = defaultExposureExchange
	}
	if cfg.ViewEventRecordedQueue == "" {
		cfg.ViewEventRecordedQueue = defaultViewEventRecordedQueue
	}
	if cfg.ViewEventRecordedRouting == "" {
		cfg.ViewEventRecordedRouting = defaultViewEventRecordedRouting
	}
	return cfg
}
