package domain

type VideoTask struct {
	TaskID   string
	VideoKey string
	Status   string // "pending", "processing", "success"
}

type MQBroker interface {
	PublishVideoTask(task *VideoTask) error
}
