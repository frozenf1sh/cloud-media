// Package persistence 实现领域仓储接口，使用 GORM 访问 PostgreSQL。
package persistence

import "github.com/google/wire"

// RepositoryProviderSet 是 Repository 的 Wire 提供者集合
var RepositoryProviderSet = wire.NewSet(
	NewVideoTaskRepository,
	NewOutboxRepository,
	NewProcessedMessageRepository,
)
