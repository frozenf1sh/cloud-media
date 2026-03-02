package rpc

import (
	"connectrpc.com/connect"
	"context"
	"github.com/frozenf1sh/cloud-media/internal/usecase"
	"github.com/google/wire"
	pb "github.com/frozenf1sh/cloud-media/proto/gen/api/v1"
)

// ProviderSet 是 Wire 的提供者集合
var ProviderSet = wire.NewSet(NewVideoServer)

type VideoServer struct {
	usecase *usecase.VideoUseCase
}

func NewVideoServer(uc *usecase.VideoUseCase) *VideoServer {
	return &VideoServer{usecase: uc}
}

// SubmitTask 实现 Protobuf 里定义的 Connect RPC 接口
func (s *VideoServer) SubmitTask(
	ctx context.Context,
	req *connect.Request[pb.SubmitTaskRequest],
) (*connect.Response[pb.SubmitTaskResponse], error) {

	err := s.usecase.SubmitTranscodeTask(req.Msg.TaskId, req.Msg.VideoKey)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&pb.SubmitTaskResponse{
		TaskId:  req.Msg.TaskId,
		Status:  "pending",
		Message: "Task submitted to Message Queue successfully!",
	}), nil
}
