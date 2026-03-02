package usecase

import (
	"github.com/frozenf1sh/cloud-media/internal/domain"
	"github.com/google/wire"
)

// ProviderSet 是 Wire 的提供者集合
var ProviderSet = wire.NewSet(NewVideoUseCase)

type VideoUseCase struct {
	mq domain.MQBroker
}

func NewVideoUseCase(mq domain.MQBroker) *VideoUseCase {
	return &VideoUseCase{mq: mq}
}

// 核心业务逻辑
func (uc *VideoUseCase) SubmitTranscodeTask(taskID, videoKey string) error {
	task := &domain.VideoTask{
		TaskID:   taskID,
		VideoKey: videoKey,
		Status:   "pending",
	}
	// 把任务丢给 MQ
	return uc.mq.PublishVideoTask(task)
}
