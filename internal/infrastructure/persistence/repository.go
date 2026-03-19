package persistence

import "github.com/google/wire"

// RepositoryProviderSet 是 Repository 的 Wire 提供者集合
var RepositoryProviderSet = wire.NewSet(
	NewVideoTaskRepository,
	NewOutboxRepository,
	NewProcessedMessageRepository,
)
