package selector

import (
	"claude-code-lb/pkg/types"
)

// ServerSelector 服务器选择器接口
type ServerSelector interface {
	// SelectServer 选择一个可用的服务器
	SelectServer() (*types.UpstreamServer, error)

	// MarkServerDown 标记服务器为不可用
	// strict: 如果为true，忽略阈值直接标记为失败；如果为false，遵循FailThreshold逻辑
	MarkServerDown(url string, strict bool)

	// MarkServerHealthy 标记服务器为健康
	MarkServerHealthy(url string)

	// GetAvailableServers 获取所有可用服务器
	GetAvailableServers() []types.UpstreamServer

	// GetServerStatus 获取服务器状态
	GetServerStatus() map[string]bool

	// RecoverServer 恢复服务器
	RecoverServer(url string)
}
