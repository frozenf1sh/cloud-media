package rpc

import (
	"connectrpc.com/connect"
	"context"

	"github.com/frozenf1sh/cloud-media/internal/domain"
	"github.com/frozenf1sh/cloud-media/internal/usecase"
	"github.com/frozenf1sh/cloud-media/pkg/errors"
	"github.com/frozenf1sh/cloud-media/pkg/telemetry"
	"github.com/google/wire"
	pb "github.com/frozenf1sh/cloud-media/proto/gen/api/v1"
)

// ProviderSet 是 Wire 的提供者集合
var ProviderSet = wire.NewSet(NewVideoServer)

// VideoServer 视频服务 RPC 处理器
type VideoServer struct {
	usecase *usecase.VideoUseCase
}

// NewVideoServer 创建 VideoServer 实例
func NewVideoServer(uc *usecase.VideoUseCase) *VideoServer {
	return &VideoServer{usecase: uc}
}

// GetUploadURL 获取上传预签名 URL
func (s *VideoServer) GetUploadURL(
	ctx context.Context,
	req *connect.Request[pb.GetUploadURLRequest],
) (*connect.Response[pb.GetUploadURLResponse], error) {
	ctx, span := telemetry.StartSpan(ctx, "VideoServer.GetUploadURL",
		telemetry.String("task_id", req.Msg.TaskId),
		telemetry.String("file_name", req.Msg.FileName),
	)
	defer span.End()

	taskID, uploadURL, sourceBucket, sourceKey, expiry, err := s.usecase.GetUploadURL(ctx, req.Msg.TaskId, req.Msg.FileName)
	if err != nil {
		return nil, toConnectError(ctx, err)
	}

	telemetry.SetSpanStatusOK(ctx)
	return connect.NewResponse(&pb.GetUploadURLResponse{
		TaskId:        taskID,
		UploadUrl:     uploadURL,
		SourceBucket:  sourceBucket,
		SourceKey:     sourceKey,
		ExpirySeconds: expiry,
	}), nil
}

// toConnectError 将应用错误转换为 Connect RPC 错误
func toConnectError(ctx context.Context, err error) *connect.Error {
	// 记录错误到 span
	telemetry.RecordError(ctx, err)

	if appErr, ok := errors.IsAppError(err); ok {
		var code connect.Code
		switch appErr.Code {
		case errors.CodeInvalidArgument:
			code = connect.CodeInvalidArgument
		case errors.CodeNotFound:
			code = connect.CodeNotFound
		case errors.CodeAlreadyExists:
			code = connect.CodeAlreadyExists
		case errors.CodePermissionDenied:
			code = connect.CodePermissionDenied
		case errors.CodeResourceExhausted:
			code = connect.CodeResourceExhausted
		case errors.CodeUnavailable:
			code = connect.CodeUnavailable
		default:
			code = connect.CodeInternal
		}
		connectErr := connect.NewError(code, err)
		connectErr.Meta().Set("error-code", string(appErr.Code))
		return connectErr
	}
	return connect.NewError(connect.CodeInternal, err)
}

// SubmitTask 提交视频转码任务
func (s *VideoServer) SubmitTask(
	ctx context.Context,
	req *connect.Request[pb.SubmitTaskRequest],
) (*connect.Response[pb.SubmitTaskResponse], error) {
	ctx, span := telemetry.StartSpan(ctx, "VideoServer.SubmitTask",
		telemetry.String("task_id", req.Msg.TaskId),
		telemetry.String("source_bucket", req.Msg.SourceBucket),
		telemetry.String("source_key", req.Msg.SourceKey),
	)
	defer span.End()

	task, err := s.usecase.SubmitTranscodeTask(ctx, req.Msg.TaskId, req.Msg.SourceBucket, req.Msg.SourceKey)
	if err != nil {
		return nil, toConnectError(ctx, err)
	}

	telemetry.SetSpanStatusOK(ctx)
	return connect.NewResponse(&pb.SubmitTaskResponse{
		TaskId:  task.TaskID,
		Status:  string(task.Status),
		Message: "Task submitted successfully!",
	}), nil
}

// GetTaskStatus 获取任务状态
func (s *VideoServer) GetTaskStatus(
	ctx context.Context,
	req *connect.Request[pb.GetTaskStatusRequest],
) (*connect.Response[pb.GetTaskStatusResponse], error) {
	ctx, span := telemetry.StartSpan(ctx, "VideoServer.GetTaskStatus",
		telemetry.String("task_id", req.Msg.TaskId),
	)
	defer span.End()

	task, playlistURL, thumbnailURL, err := s.usecase.GetTaskStatus(ctx, req.Msg.TaskId)
	if err != nil {
		return nil, toConnectError(ctx, err)
	}

	resp := &pb.GetTaskStatusResponse{
		TaskId:        task.TaskID,
		Status:        string(task.Status),
		Progress:      int32(task.Progress),
		SourceBucket:  task.SourceBucket,
		SourceKey:     task.SourceKey,
		ErrorMessage:  task.ErrorMessage,
		CreatedAt:     task.CreatedAt,
		PlaylistUrl:   playlistURL,
		ThumbnailUrl:  thumbnailURL,
	}

	if task.StartedAt != nil {
		resp.StartedAt = *task.StartedAt
	}
	if task.CompletedAt != nil {
		resp.CompletedAt = *task.CompletedAt
	}

	telemetry.SetSpanStatusOK(ctx)
	return connect.NewResponse(resp), nil
}

// ListTasks 列出任务
func (s *VideoServer) ListTasks(
	ctx context.Context,
	req *connect.Request[pb.ListTasksRequest],
) (*connect.Response[pb.ListTasksResponse], error) {
	ctx, span := telemetry.StartSpan(ctx, "VideoServer.ListTasks",
		telemetry.Int("page", int(req.Msg.Page)),
		telemetry.Int("page_size", int(req.Msg.PageSize)),
	)
	defer span.End()

	tasks, total, err := s.usecase.ListTasks(ctx, int(req.Msg.Page), int(req.Msg.PageSize))
	if err != nil {
		return nil, toConnectError(ctx, err)
	}

	taskInfos := make([]*pb.TaskInfo, len(tasks))
	for i, task := range tasks {
		taskInfos[i] = &pb.TaskInfo{
			TaskId:    task.TaskID,
			Status:    string(task.Status),
			Progress:  int32(task.Progress),
			SourceKey: task.SourceKey,
			CreatedAt: task.CreatedAt,
		}
	}

	telemetry.SetSpanStatusOK(ctx)
	return connect.NewResponse(&pb.ListTasksResponse{
		Tasks: taskInfos,
		Total: total,
	}), nil
}

// CancelTask 取消任务
func (s *VideoServer) CancelTask(
	ctx context.Context,
	req *connect.Request[pb.CancelTaskRequest],
) (*connect.Response[pb.CancelTaskResponse], error) {
	ctx, span := telemetry.StartSpan(ctx, "VideoServer.CancelTask",
		telemetry.String("task_id", req.Msg.TaskId),
	)
	defer span.End()

	err := s.usecase.CancelTask(ctx, req.Msg.TaskId)
	if err != nil {
		return nil, toConnectError(ctx, err)
	}

	telemetry.SetSpanStatusOK(ctx)
	return connect.NewResponse(&pb.CancelTaskResponse{
		TaskId:  req.Msg.TaskId,
		Status:  string(domain.TaskStatusCancelled),
		Message: "Task cancelled successfully",
	}), nil
}
