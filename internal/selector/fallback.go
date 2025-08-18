package selector

import (
	"errors"
	"sort"
	"sync"
	"time"

	"claude-code-lb/internal/logger"
	"claude-code-lb/pkg/types"
)

// FallbackSelector fallback选择器
type FallbackSelector struct {
	config         types.Config
	serverStatus   map[string]bool
	statusMutex    sync.RWMutex
	failureCount   map[string]int64       // 服务器失败次数
	orderedServers []types.UpstreamServer // 按优先级排序的服务器列表
}

// NewFallbackSelector 创建新的fallback选择器
func NewFallbackSelector(config types.Config) *FallbackSelector {
	fs := &FallbackSelector{
		config:       config,
		serverStatus: make(map[string]bool),
		failureCount: make(map[string]int64),
	}

	// 初始化服务器状态
	for _, server := range config.Servers {
		fs.serverStatus[server.URL] = true
		fs.failureCount[server.URL] = 0
	}

	// 对服务器按优先级排序
	fs.orderedServers = make([]types.UpstreamServer, len(config.Servers))
	copy(fs.orderedServers, config.Servers)

	// 如果没有设置priority，则使用权重作为优先级（权重越高优先级越高）
	for i := range fs.orderedServers {
		if fs.orderedServers[i].Priority == 0 {
			// 权重越高，优先级数字越小（优先级越高）
			maxWeight := 0
			for _, s := range config.Servers {
				if s.Weight > maxWeight {
					maxWeight = s.Weight
				}
			}
			fs.orderedServers[i].Priority = maxWeight - fs.orderedServers[i].Weight + 1
		}
	}

	// 按优先级排序（priority数字越小优先级越高）
	sort.Slice(fs.orderedServers, func(i, j int) bool {
		return fs.orderedServers[i].Priority < fs.orderedServers[j].Priority
	})

	logger.Info("SELECTOR", "Fallback selector initialized with %d servers", len(fs.orderedServers))
	for i, server := range fs.orderedServers {
		logger.Info("SELECTOR", "Priority %d: %s (weight: %d)", i+1, server.URL, server.Weight)
	}

	return fs
}

// SelectServer 按优先级选择一个可用的服务器
func (fs *FallbackSelector) SelectServer() (*types.UpstreamServer, error) {
	now := time.Now()

	fs.statusMutex.RLock()
	defer fs.statusMutex.RUnlock()

	// 按优先级顺序查找可用服务器
	for i, server := range fs.orderedServers {
		// 检查服务器是否可用且未在冷却期
		if fs.serverStatus[server.URL] && now.After(server.DownUntil) {
			logger.Info("SELECTOR", "Selected server by priority %d: %s", i+1, server.URL)
			return &fs.orderedServers[i], nil
		}
	}

	// 如果所有服务器都不可用，尝试选择冷却时间最短的服务器进行紧急重试
	fallbackServer := fs.getEmergencyFallbackServer()
	if fallbackServer != nil {
		logger.Warning("SELECTOR", "Using emergency fallback server: %s", fallbackServer.URL)
		return fallbackServer, nil
	}

	logger.Error("SELECTOR", "No available servers in fallback mode")
	return nil, errors.New("no available servers")
}

// getEmergencyFallbackServer 获取紧急fallback服务器（冷却时间最短的）
func (fs *FallbackSelector) getEmergencyFallbackServer() *types.UpstreamServer {
	now := time.Now()
	var bestServer *types.UpstreamServer
	var shortestCooldown time.Duration = time.Hour * 24 // 初始化为很大的值

	// 优先考虑按优先级排序的服务器
	for i, server := range fs.orderedServers {
		if now.After(server.DownUntil) {
			// 如果已经过了冷却时间，直接选择
			return &fs.orderedServers[i]
		}

		// 找到冷却时间最短的服务器
		cooldownRemaining := server.DownUntil.Sub(now)
		if cooldownRemaining < shortestCooldown {
			shortestCooldown = cooldownRemaining
			bestServer = &fs.orderedServers[i]
		}
	}

	return bestServer
}

// MarkServerDown 标记服务器为不可用
func (fs *FallbackSelector) MarkServerDown(url string) {
	fs.statusMutex.Lock()
	defer fs.statusMutex.Unlock()

	fs.serverStatus[url] = false

	// 增加失败计数
	fs.failureCount[url]++
	failures := fs.failureCount[url]

	// 动态计算冷却时间
	cooldownDuration := time.Duration(fs.config.Cooldown) * time.Second
	if failures > 1 {
		// 指数退避，但设置上限
		dynamicCooldown := cooldownDuration * time.Duration(failures)
		if dynamicCooldown > 10*time.Minute {
			dynamicCooldown = 10 * time.Minute // 最大 10 分钟
		}
		cooldownDuration = dynamicCooldown
	}

	downUntil := time.Now().Add(cooldownDuration)

	// 更新对应服务器的冷却时间
	for i, server := range fs.orderedServers {
		if server.URL == url {
			fs.orderedServers[i].DownUntil = downUntil
			break
		}
	}

	// 同时更新config中的服务器状态
	for i, server := range fs.config.Servers {
		if server.URL == url {
			fs.config.Servers[i].DownUntil = downUntil
			break
		}
	}

	logger.Warning("SELECTOR", "Server marked down: %s (priority order, failures: %d, cooldown: %v)", url, failures, cooldownDuration)
}

// GetAvailableServers 获取所有可用服务器（按优先级排序）
func (fs *FallbackSelector) GetAvailableServers() []types.UpstreamServer {
	now := time.Now()
	var available []types.UpstreamServer

	fs.statusMutex.RLock()
	defer fs.statusMutex.RUnlock()

	for _, server := range fs.orderedServers {
		if fs.serverStatus[server.URL] && now.After(server.DownUntil) {
			available = append(available, server)
		}
	}

	return available
}

// GetServerStatus 获取服务器状态
func (fs *FallbackSelector) GetServerStatus() map[string]bool {
	fs.statusMutex.RLock()
	defer fs.statusMutex.RUnlock()

	status := make(map[string]bool)
	for k, v := range fs.serverStatus {
		status[k] = v
	}
	return status
}

// RecoverServer 恢复服务器
func (fs *FallbackSelector) RecoverServer(url string) {
	fs.statusMutex.Lock()
	defer fs.statusMutex.Unlock()

	fs.serverStatus[url] = true

	// 清除冷却时间
	for i, server := range fs.orderedServers {
		if server.URL == url {
			fs.orderedServers[i].DownUntil = time.Time{}
			break
		}
	}

	// 同时更新config中的服务器状态
	for i, server := range fs.config.Servers {
		if server.URL == url {
			fs.config.Servers[i].DownUntil = time.Time{}
			break
		}
	}

	logger.Success("SELECTOR", "Server recovered: %s", url)
}

// MarkServerHealthy 标记服务器为健康
func (fs *FallbackSelector) MarkServerHealthy(url string) {
	fs.statusMutex.Lock()
	defer fs.statusMutex.Unlock()

	// 重置失败计数
	if fs.failureCount[url] > 0 {
		oldFailures := fs.failureCount[url]
		fs.failureCount[url] = 0
		logger.Info("SELECTOR", "Server %s healthy, reset failure count (was %d)", url, oldFailures)
	}

	// 确保服务器状态为可用
	if !fs.serverStatus[url] {
		fs.serverStatus[url] = true
		// 清除冷却时间
		for i, server := range fs.orderedServers {
			if server.URL == url {
				fs.orderedServers[i].DownUntil = time.Time{}
				break
			}
		}
		// 同时更新config中的服务器状态
		for i, server := range fs.config.Servers {
			if server.URL == url {
				fs.config.Servers[i].DownUntil = time.Time{}
				break
			}
		}
		logger.Success("SELECTOR", "Server %s auto-recovered from healthy request", url)
	}
}
