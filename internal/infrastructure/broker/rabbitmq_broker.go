
package broker

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/frozenf1sh/cloud-media/internal/domain"
	"github.com/frozenf1sh/cloud-media/pkg/logger"
	"github.com/frozenf1sh/cloud-media/pkg/telemetry"
	"github.com/google/uuid"
	"github.com/google/wire"
	"go.opentelemetry.io/otel/trace"
	amqp "github.com/rabbitmq/amqp091-go"
)

// ProviderSet 是 Wire 的提供者集合
var ProviderSet = wire.NewSet(
	NewRabbitMQBroker,
	// 实现 domain 内接口
	wire.Bind(new(domain.MQBroker), new(*RabbitMQBroker)),
	wire.Bind(new(domain.ReliableMQBroker), new(*RabbitMQBroker)),
)

// TaskHandler 任务处理函数类型
type TaskHandler func(ctx context.Context, task *domain.VideoTask) error

const (
	// 默认消费者 ID
	defaultConsumerID = "cloud-media-worker"
	// 发布确认超时时间
	publishConfirmTimeout = 30 * time.Second
	// 最大重试次数
	defaultMaxRetries = 10
)

// RabbitMQBroker 支持发布确认的 RabbitMQ broker
type RabbitMQBroker struct {
	conn         *amqp.Connection
	channel      *amqp.Channel
	queue        amqp.Queue
	url          string
	consumerID   string

	// 发布确认相关
	confirms     chan amqp.Confirmation
	returns      chan amqp.Return
	publishMutex sync.Mutex

	// 生命周期
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	running      bool
}

// NewRabbitMQBroker 创建 RabbitMQBroker 实例
func NewRabbitMQBroker(url string) (*RabbitMQBroker, error) {
	return NewRabbitMQBrokerWithConsumerID(url, defaultConsumerID)
}

// NewRabbitMQBrokerWithConsumerID 创建带消费者 ID 的 RabbitMQBroker 实例
func NewRabbitMQBrokerWithConsumerID(url, consumerID string) (*RabbitMQBroker, error) {
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("failed to open channel: %w", err)
	}

	// 启用发布确认
	if err := ch.Confirm(false); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, fmt.Errorf("failed to enable publisher confirms: %w", err)
	}

	// 设置 QoS - 公平分发
	if err := ch.Qos(
		1,     // prefetch count
		0,     // prefetch size
		false, // global
	); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, fmt.Errorf("failed to set QoS: %w", err)
	}

	q, err := ch.QueueDeclare(
		"video_transcode_tasks",
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, fmt.Errorf("failed to declare queue: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &RabbitMQBroker{
		conn:       conn,
		channel:    ch,
		queue:      q,
		url:        url,
		consumerID: consumerID,
		confirms:   make(chan amqp.Confirmation, 1),
		returns:    make(chan amqp.Return, 1),
		ctx:        ctx,
		cancel:     cancel,
	}, nil
}

// Start 启动 broker（监听确认和返回）
func (r *RabbitMQBroker) Start(ctx context.Context) error {
	r.publishMutex.Lock()
	if r.running {
		r.publishMutex.Unlock()
		return nil
	}
	r.running = true
	r.publishMutex.Unlock()

	// 注册确认和返回通道
	r.confirms = r.channel.NotifyPublish(make(chan amqp.Confirmation, 100))
	r.returns = r.channel.NotifyReturn(make(chan amqp.Return, 10))

	r.wg.Add(1)
	go r.handleConfirmations()

	logger.Info("RabbitMQ broker started",
		logger.String("consumer_id", r.consumerID),
		logger.String("queue", r.queue.Name))

	return nil
}

// handleConfirmations 处理发布确认和返回
func (r *RabbitMQBroker) handleConfirmations() {
	defer r.wg.Done()

	for {
		select {
		case <-r.ctx.Done():
			logger.Info("Stopping confirmation handler")
			return

		case confirm, ok := <-r.confirms:
			if !ok {
				logger.Warn("Confirmation channel closed")
				return
			}
			if !confirm.Ack {
				logger.Warn("Message not acknowledged by broker",
					logger.Uint64("delivery_tag", confirm.DeliveryTag))
			}

		case ret, ok := <-r.returns:
			if !ok {
				logger.Warn("Return channel closed")
				return
			}
			logger.Error("Message returned by broker",
				logger.Int("reply_code", int(ret.ReplyCode)),
				logger.String("reply_text", ret.ReplyText),
				logger.String("routing_key", ret.RoutingKey))
		}
	}
}

// Stop 停止 broker
func (r *RabbitMQBroker) Stop() error {
	r.publishMutex.Lock()
	if !r.running {
		r.publishMutex.Unlock()
		return nil
	}
	r.running = false
	r.publishMutex.Unlock()

	r.cancel()
	r.wg.Wait()

	if r.channel != nil {
		_ = r.channel.Close()
	}
	if r.conn != nil {
		_ = r.conn.Close()
	}

	logger.Info("RabbitMQ broker stopped")
	return nil
}

// PublishVideoTask 发布视频任务（兼容旧接口，不使用 Outbox）
func (r *RabbitMQBroker) PublishVideoTask(ctx context.Context, task *domain.VideoTask) error {
	body, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("failed to marshal task: %w", err)
	}

	// 注入 trace 信息到消息头
	headers := make(amqp.Table)
	carrier := make(map[string]string)
	telemetry.InjectToCarrier(ctx, carrier)

	for k, v := range carrier {
		headers[k] = v
	}

	// 生成消息 ID（用于幂等）
	messageID := uuid.New().String()
	headers["message_id"] = messageID

	r.publishMutex.Lock()
	defer r.publishMutex.Unlock()

	// 获取下一个 delivery tag
	deliveryTag := r.channel.GetNextPublishSeqNo()

	err = r.channel.Publish(
		"",
		r.queue.Name,
		true,  // mandatory：如果无法路由，返回消息
		false,
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			MessageId:    messageID,
			Body:         body,
			Headers:      headers,
		},
	)
	if err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}

	// 等待确认
	confirmCtx, cancel := context.WithTimeout(ctx, publishConfirmTimeout)
	defer cancel()

	select {
	case confirm := <-r.confirms:
		if confirm.DeliveryTag != deliveryTag {
			logger.Warn("Unexpected delivery tag in confirmation",
				logger.Uint64("expected", deliveryTag),
				logger.Uint64("got", confirm.DeliveryTag))
		}
		if !confirm.Ack {
			return fmt.Errorf("message nacked by broker")
		}
	case <-confirmCtx.Done():
		return fmt.Errorf("publish confirm timeout: %w", confirmCtx.Err())
	}

	logger.InfoContext(ctx, "Published task (with confirm)",
		logger.String("task_id", task.TaskID),
		logger.String("trace_id", task.TraceID),
		logger.String("source_key", task.SourceKey),
		logger.String("message_id", messageID),
		logger.Uint64("delivery_tag", deliveryTag))

	return nil
}

// PublishWithConfirm 带确认的发布（使用 Outbox 事件）
func (r *RabbitMQBroker) PublishWithConfirm(ctx context.Context, event *domain.OutboxEvent) error {
	r.publishMutex.Lock()
	defer r.publishMutex.Unlock()

	// 注入 trace 信息到消息头
	headers := make(amqp.Table)
	carrier := make(map[string]string)
	telemetry.InjectToCarrier(ctx, carrier)

	for k, v := range carrier {
		headers[k] = v
	}

	// 使用 event ID 作为消息 ID
	headers["event_id"] = event.EventID
	headers["aggregate_id"] = event.AggregateID
	headers["aggregate_type"] = event.AggregateType

	// 获取下一个 delivery tag
	deliveryTag := r.channel.GetNextPublishSeqNo()

	err := r.channel.Publish(
		"",
		r.queue.Name,
		true,  // mandatory：如果无法路由，返回消息
		false,
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			MessageId:    event.EventID,
			Body:         event.Payload,
			Headers:      headers,
		},
	)
	if err != nil {
		return fmt.Errorf("failed to publish event: %w", err)
	}

	// 等待确认
	confirmCtx, cancel := context.WithTimeout(ctx, publishConfirmTimeout)
	defer cancel()

	select {
	case confirm := <-r.confirms:
		if confirm.DeliveryTag != deliveryTag {
			logger.Warn("Unexpected delivery tag in confirmation",
				logger.Uint64("expected", deliveryTag),
				logger.Uint64("got", confirm.DeliveryTag))
		}
		if !confirm.Ack {
			return fmt.Errorf("event nacked by broker")
		}
	case <-confirmCtx.Done():
		return fmt.Errorf("publish confirm timeout: %w", confirmCtx.Err())
	}

	logger.InfoContext(ctx, "Published event (with confirm)",
		logger.String("event_id", event.EventID),
		logger.String("event_type", event.EventType),
		logger.String("aggregate_id", event.AggregateID),
		logger.Uint64("delivery_tag", deliveryTag))

	return nil
}

// ConsumeTasks 开始消费任务
func (r *RabbitMQBroker) ConsumeTasks(ctx context.Context, handler TaskHandler) error {
	logger.InfoContext(ctx, "Started consuming tasks",
		logger.String("queue", r.queue.Name),
		logger.String("consumer_id", r.consumerID))

	msgs, err := r.channel.Consume(
		r.queue.Name,
		r.consumerID, // consumer tag
		false,        // auto-ack (手动确认)
		false,        // exclusive
		false,        // no-local
		false,        // no-wait
		nil,          // args
	)
	if err != nil {
		return fmt.Errorf("failed to register consumer: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			logger.InfoContext(ctx, "Stopping consumer...")
			return ctx.Err()
		case msg, ok := <-msgs:
			if !ok {
				logger.ErrorContext(ctx, "Message channel closed")
				return fmt.Errorf("message channel closed")
			}

			// 处理消息
			if err := r.handleMessage(ctx, msg, handler); err != nil {
				logger.ErrorContext(ctx, "Failed to handle message", logger.Err(err))
			}
		}
	}
}

// handleMessage 处理单条消息
func (r *RabbitMQBroker) handleMessage(ctx context.Context, msg amqp.Delivery, handler TaskHandler) error {
	var task domain.VideoTask
	if err := json.Unmarshal(msg.Body, &task); err != nil {
		logger.ErrorContext(ctx, "Failed to unmarshal task", logger.Err(err))
		// 拒绝消息，不重新入队（无效消息）
		_ = msg.Nack(false, false)
		return fmt.Errorf("failed to unmarshal task: %w", err)
	}

	// 从消息头提取 trace 信息
	carrier := make(map[string]string)
	for k, v := range msg.Headers {
		if s, ok := v.(string); ok {
			carrier[k] = s
		}
	}

	ctx = telemetry.ExtractFromCarrier(ctx, carrier)

	// 只有在没有有效的 trace context 时，才使用 task.TraceID 或 task.TaskID 作为 fallback
	sc := trace.SpanFromContext(ctx).SpanContext()
	if !sc.IsValid() {
		traceID := task.TraceID
		if traceID == "" {
			traceID = task.TaskID
		}
		if traceID != "" {
			ctx = telemetry.WithTraceID(ctx, traceID)
		}
	}

	// 开始处理 span
	ctx, span := telemetry.StartSpan(ctx, "RabbitMQBroker.handleMessage",
		telemetry.String("task_id", task.TaskID),
	)
	defer span.End()

	// 获取消息 ID（用于幂等）
	messageID := msg.MessageId
	if messageID == "" {
		// 向后兼容：从 header 获取或生成
		if mid, ok := msg.Headers["message_id"].(string); ok && mid != "" {
			messageID = mid
		} else if eid, ok := msg.Headers["event_id"].(string); ok && eid != "" {
			messageID = eid
		} else {
			messageID = task.TaskID
		}
	}

	logger.InfoContext(ctx, "Received task",
		logger.String("task_id", task.TaskID),
		logger.String("message_id", messageID))

	// 调用 handler 处理任务
	if err := handler(ctx, &task); err != nil {
		telemetry.RecordError(ctx, err)
		logger.ErrorContext(ctx, "Task handling failed",
			logger.Err(err),
			logger.String("task_id", task.TaskID))
		// 处理失败，不重新入队（已更新数据库状态）
		_ = msg.Nack(false, false)
		return err
	}

	// 确认消息
	if err := msg.Ack(false); err != nil {
		logger.WarnContext(ctx, "Failed to ack message",
			logger.Err(err),
			logger.String("task_id", task.TaskID))
	}

	logger.InfoContext(ctx, "Task completed and acknowledged",
		logger.String("task_id", task.TaskID),
		logger.String("message_id", messageID))
	return nil
}

// Reconnect 重新连接
func (r *RabbitMQBroker) Reconnect() error {
	r.publishMutex.Lock()
	defer r.publishMutex.Unlock()

	// 先停止
	if r.conn != nil {
		_ = r.conn.Close()
	}

	conn, err := amqp.Dial(r.url)
	if err != nil {
		return fmt.Errorf("failed to reconnect: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("failed to open channel on reconnect: %w", err)
	}

	// 启用发布确认
	if err := ch.Confirm(false); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return fmt.Errorf("failed to enable publisher confirms on reconnect: %w", err)
	}

	// 重新设置 QoS
	if err := ch.Qos(1, 0, false); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return fmt.Errorf("failed to set QoS on reconnect: %w", err)
	}

	q, err := ch.QueueDeclare(
		"video_transcode_tasks",
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return fmt.Errorf("failed to declare queue on reconnect: %w", err)
	}

	r.conn = conn
	r.channel = ch
	r.queue = q

	// 重新注册确认通道
	if r.running {
		r.confirms = r.channel.NotifyPublish(make(chan amqp.Confirmation, 100))
		r.returns = r.channel.NotifyReturn(make(chan amqp.Return, 10))
	}

	logger.Info("RabbitMQ reconnected successfully")
	return nil
}

// Close 关闭连接（保持向后兼容）
func (r *RabbitMQBroker) Close() {
	_ = r.Stop()
}
