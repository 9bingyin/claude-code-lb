package balance

import (
	"claude-code-lb/internal/logger"
	"claude-code-lb/internal/selector"
	"claude-code-lb/pkg/types"
)

// Balancer 负载均衡器（现在是选择器的包装器）
type Balancer struct {
	config   types.Config
	selector selector.ServerSelector
}

// New 创建新的负载均衡器
func New(config types.Config) *Balancer {
	// 使用工厂函数创建适当的选择器
	sel, err := selector.CreateSelector(config)
	if err != nil {
		logger.Error("LOAD", "Failed to create selector: %v", err)
		// 降级到负载均衡模式
		sel = selector.NewLoadBalancer(config)
	}

	selectorType := selector.GetSelectorType(config)
	logger.Info("LOAD", "Balancer initialized with: %s", selectorType)

	return &Balancer{
		config:   config,
		selector: sel,
	}
}

// GetNextServer 获取下一个服务器
func (b *Balancer) GetNextServer() (*types.UpstreamServer, error) {
	return b.selector.SelectServer()
}

// GetNextServerWithFallback 获取下一个服务器（向后兼容方法）
func (b *Balancer) GetNextServerWithFallback(useFallback bool) (*types.UpstreamServer, error) {
	// 在新的架构中，fallback逻辑由选择器内部处理
	// 这个参数现在主要用于向后兼容
	return b.selector.SelectServer()
}

// MarkServerDown 标记服务器为不可用
func (b *Balancer) MarkServerDown(url string) {
	b.selector.MarkServerDown(url)
}

// GetAvailableServers 获取所有可用服务器
func (b *Balancer) GetAvailableServers() []types.UpstreamServer {
	return b.selector.GetAvailableServers()
}

// GetServerStatus 获取服务器状态
func (b *Balancer) GetServerStatus() map[string]bool {
	return b.selector.GetServerStatus()
}

// RecoverServer 恢复服务器
func (b *Balancer) RecoverServer(url string) {
	b.selector.RecoverServer(url)
}

// MarkServerHealthy 标记服务器为健康
func (b *Balancer) MarkServerHealthy(url string) {
	b.selector.MarkServerHealthy(url)
}
