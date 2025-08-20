package testutil

import "sync"

// MockBalancer Mock负载均衡器，实现balance.BalancerInterface
type MockBalancer struct {
	downServers map[string]bool
	mutex       sync.RWMutex
	// 记录调用历史
	MarkDownCalls    []string
	MarkHealthyCalls []string
	RecoverCalls     []string
}

// NewMockBalancer 创建新的Mock负载均衡器
func NewMockBalancer() *MockBalancer {
	return &MockBalancer{
		downServers:      make(map[string]bool),
		MarkDownCalls:    make([]string, 0),
		MarkHealthyCalls: make([]string, 0),
		RecoverCalls:     make([]string, 0),
	}
}

// MarkServerDown 标记服务器为不可用
func (m *MockBalancer) MarkServerDown(url string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.downServers[url] = true
	m.MarkDownCalls = append(m.MarkDownCalls, url)
}

// MarkServerHealthy 标记服务器为健康
func (m *MockBalancer) MarkServerHealthy(url string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.downServers[url] = false
	m.MarkHealthyCalls = append(m.MarkHealthyCalls, url)
}

// RecoverServer 恢复服务器
func (m *MockBalancer) RecoverServer(url string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.downServers[url] = false
	m.RecoverCalls = append(m.RecoverCalls, url)
}

// IsServerDown 检查服务器是否已宕机
func (m *MockBalancer) IsServerDown(url string) bool {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.downServers[url]
}

// GetServerStatus 获取服务器状态（up=true, down=false）
func (m *MockBalancer) GetServerStatus() map[string]bool {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	result := make(map[string]bool)
	for url, isDown := range m.downServers {
		result[url] = !isDown // 返回up状态
	}
	return result
}

// SetServerStatus 设置服务器状态（用于测试）
func (m *MockBalancer) SetServerStatus(url string, isUp bool) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.downServers[url] = !isUp
}

// Reset 重置所有状态
func (m *MockBalancer) Reset() {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.downServers = make(map[string]bool)
	m.MarkDownCalls = make([]string, 0)
	m.MarkHealthyCalls = make([]string, 0)
	m.RecoverCalls = make([]string, 0)
}

// GetMarkDownCallCount 获取MarkServerDown的调用次数
func (m *MockBalancer) GetMarkDownCallCount(url string) int {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	count := 0
	for _, call := range m.MarkDownCalls {
		if call == url {
			count++
		}
	}
	return count
}
