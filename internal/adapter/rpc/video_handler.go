package rpc

import (
	"connectrpc.com/connect"
	"context"

	"github.com/frozenf1sh/cloud-media/internal/domain"
	"github.com/frozenf1sh/cloud-media/internal/usecase"
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

// SubmitTask 提交视频转码任务
func (s *VideoServer) SubmitTask(
	ctx context.Context,
	req *connect.Request[pb.SubmitTaskRequest],
) (*connect.Response[pb.SubmitTaskResponse], error) {

	task, err := s.usecase.SubmitTranscodeTask(ctx, req.Msg.TaskId, req.Msg.SourceBucket, req.Msg.SourceKey)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&pb.SubmitTaskResponse{
		TaskId:  req.Msg.TaskId,
		Status:  string(task.Status),
		Message: "Task submitted successfully!",
	}), nil
}

// GetTaskStatus 获取任务状态
func (s *VideoServer) GetTaskStatus(
	ctx context.Context,
	req *connect.Request[pb.GetTaskStatusRequest],
) (*connect.Response[pb.GetTaskStatusResponse], error) {

	task, err := s.usecase.GetTaskStatus(ctx, req.Msg.TaskId)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}

	resp := &pb.GetTaskStatusResponse{
		TaskId:       task.TaskID,
		Status:        string(task.Status),
		Progress:      int32(task.Progress),
		SourceBucket:  task.SourceBucket,
		SourceKey:     task.SourceKey,
		ErrorMessage:  task.ErrorMessage,
		CreatedAt:    task.CreatedAt,
	}

	if task.StartedAt != nil {
		resp.StartedAt = *task.StartedAt
	}
	if task.CompletedAt != nil {
		resp.CompletedAt = *task.CompletedAt
	}

	return connect.NewResponse(resp), nil
}

// ListTasks 列出任务
func (s *VideoServer) ListTasks(
	ctx context.Context,
	req *connect.Request[pb.ListTasksRequest],
) (*connect.Response[pb.ListTasksResponse], error) {

	tasks, total, err := s.usecase.ListTasks(ctx, int(req.Msg.Page), int(req.Msg.PageSize))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
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

	err := s.usecase.CancelTask(ctx, req.Msg.TaskId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&pb.CancelTaskResponse{
		TaskId:  req.Msg.TaskId,
		Status:  string(domain.TaskStatusCancelled),
		Message: "Task cancelled successfully",
	}), nil
}
