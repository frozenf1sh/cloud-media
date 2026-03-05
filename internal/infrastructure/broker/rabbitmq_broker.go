package broker

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/frozenf1sh/cloud-media/internal/domain"
	"github.com/frozenf1sh/cloud-media/pkg/logger"
	"github.com/frozenf1sh/cloud-media/pkg/telemetry"
	"github.com/google/wire"
	"go.opentelemetry.io/otel/trace"
	amqp "github.com/rabbitmq/amqp091-go"
)

// ProviderSet 是 Wire 的提供者集合
var ProviderSet = wire.NewSet(
	NewRabbitMQBroker,
	// 实现 domain 内接口
	wire.Bind(new(domain.MQBroker), new(*RabbitMQBroker)),
)

// TaskHandler 任务处理函数类型
type TaskHandler func(ctx context.Context, task *domain.VideoTask) error

type RabbitMQBroker struct {
	conn    *amqp.Connection
	channel *amqp.Channel
	queue   amqp.Queue
	url     string
}

func NewRabbitMQBroker(url string) (*RabbitMQBroker, error) {
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to open channel: %w", err)
	}

	// 设置 QoS - 公平分发
	if err := ch.Qos(
		1,     // prefetch count
		0,     // prefetch size
		false, // global
	); err != nil {
		ch.Close()
		conn.Close()
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
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("failed to declare queue: %w", err)
	}

	return &RabbitMQBroker{
		conn:    conn,
		channel: ch,
		queue:   q,
		url:     url,
	}, nil
}

func (r *RabbitMQBroker) PublishVideoTask(ctx context.Context, task *domain.VideoTask) error {
	body, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("failed to marshal task: %w", err)
	}

	// 注入 trace 信息到消息头
	headers := make(amqp.Table)
	carrier := make(map[string]string)
	telemetry.InjectToCarrier(ctx, carrier)

	// 调试：记录注入的 carrier
	logger.DebugContext(ctx, "Injecting trace context to AMQP headers",
		logger.Any("carrier", carrier))

	for k, v := range carrier {
		headers[k] = v
	}

	err = r.channel.Publish(
		"",
		r.queue.Name,
		false,
		false,
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			Body:         body,
			Headers:      headers,
		},
	)
	if err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}

	logger.InfoContext(ctx, "Published task",
		logger.String("task_id", task.TaskID),
		logger.String("trace_id", task.TraceID),
		logger.String("source_key", task.SourceKey),
	)
	return nil
}

// ConsumeTasks 开始消费任务
func (r *RabbitMQBroker) ConsumeTasks(ctx context.Context, handler TaskHandler) error {
	logger.InfoContext(ctx, "Started consuming tasks", logger.String("queue", r.queue.Name))

	msgs, err := r.channel.Consume(
		r.queue.Name,
		"",    // consumer tag
		false, // auto-ack
		false, // exclusive
		false, // no-local
		false, // no-wait
		nil,   // args
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

	// 调试：记录提取的 carrier
	logger.DebugContext(ctx, "Extracted trace context from AMQP headers",
		logger.Any("carrier", carrier),
		logger.String("task_id", task.TaskID))

	ctx = telemetry.ExtractFromCarrier(ctx, carrier)

	// 调试：记录提取后的 span context
	sc := trace.SpanFromContext(ctx).SpanContext()
	logger.DebugContext(ctx, "Span context after extract",
		logger.String("trace_id", sc.TraceID().String()),
		logger.String("span_id", sc.SpanID().String()),
		logger.Bool("is_valid", sc.IsValid()),
		logger.String("task_id", task.TaskID))

	// 只有在没有有效的 trace context 时，才使用 task.TraceID 或 task.TaskID 作为 fallback
	if !sc.IsValid() {
		logger.DebugContext(ctx, "No valid span context, using fallback",
			logger.String("task_trace_id", task.TraceID),
			logger.String("task_id", task.TaskID))

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

	// 调试：确认 span 创建成功
	sc = trace.SpanFromContext(ctx).SpanContext()
	logger.DebugContext(ctx, "Created span for handling message",
		logger.String("trace_id", sc.TraceID().String()),
		logger.String("span_id", sc.SpanID().String()),
		logger.String("task_id", task.TaskID))

	logger.InfoContext(ctx, "Received task",
		logger.String("task_id", task.TaskID))

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
		logger.String("task_id", task.TaskID))
	return nil
}

// Reconnect 重新连接
func (r *RabbitMQBroker) Reconnect() error {
	if r.conn != nil {
		_ = r.conn.Close()
	}

	conn, err := amqp.Dial(r.url)
	if err != nil {
		return fmt.Errorf("failed to reconnect: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to open channel on reconnect: %w", err)
	}

	// 重新设置 QoS
	if err := ch.Qos(1, 0, false); err != nil {
		ch.Close()
		conn.Close()
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
		ch.Close()
		conn.Close()
		return fmt.Errorf("failed to declare queue on reconnect: %w", err)
	}

	r.conn = conn
	r.channel = ch
	r.queue = q

	return nil
}

func (r *RabbitMQBroker) Close() {
	if r.channel != nil {
		_ = r.channel.Close()
	}
	if r.conn != nil {
		_ = r.conn.Close()
	}
}
