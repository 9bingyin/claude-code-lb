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
	config          types.Config
	serverStatus    map[string]bool
	serverDownUntil map[string]time.Time // 服务器冷却时间
	statusMutex     sync.RWMutex
	failureCount    map[string]int64       // 服务器失败次数
	orderedServers  []types.UpstreamServer // 按优先级排序的服务器列表
}

// NewFallbackSelector 创建新的fallback选择器
func NewFallbackSelector(config types.Config) *FallbackSelector {
	fs := &FallbackSelector{
		config:          config,
		serverStatus:    make(map[string]bool),
		serverDownUntil: make(map[string]time.Time),
		failureCount:    make(map[string]int64),
	}

	// 初始化服务器状态
	for _, server := range config.Servers {
		fs.serverStatus[server.URL] = true
		fs.serverDownUntil[server.URL] = time.Time{}
		fs.failureCount[server.URL] = 0
	}

	// 对服务器按优先级排序
	fs.orderedServers = make([]types.UpstreamServer, len(config.Servers))
	copy(fs.orderedServers, config.Servers)

	// 重新设计优先级分配算法，确保唯一性
	fs.assignUniquePriorities()

	// 按优先级排序（priority数字越小优先级越高）
	sort.Slice(fs.orderedServers, func(i, j int) bool {
		if fs.orderedServers[i].Priority == fs.orderedServers[j].Priority {
			// 如果优先级相同，按权重排序（权重高的优先）
			return fs.orderedServers[i].Weight > fs.orderedServers[j].Weight
		}
		return fs.orderedServers[i].Priority < fs.orderedServers[j].Priority
	})

	logger.Info("LOAD", "Fallback selector initialized with %d servers", len(fs.orderedServers))
	for i, server := range fs.orderedServers {
		logger.Info("LOAD", "Priority %d: %s (weight: %d)", i+1, server.URL, server.Weight)
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
			logger.Info("LOAD", "Selected server by priority %d: %s", i+1, server.URL)
			return &fs.orderedServers[i], nil
		}
	}

	// 如果所有服务器都不可用，尝试选择冷却时间最短的服务器进行紧急重试
	fallbackServer := fs.getEmergencyFallbackServer()
	if fallbackServer != nil {
		logger.Warning("LOAD", "Using emergency fallback server: %s", fallbackServer.URL)
		return fallbackServer, nil
	}

	logger.Error("LOAD", "No available servers in fallback mode")
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

	// 记录服务器冷却时间（统一管理，避免重复维护）
	fs.serverDownUntil[url] = downUntil

	logger.Warning("LOAD", "Server marked down: %s (priority order, failures: %d, cooldown: %v)", url, failures, cooldownDuration)
}

// GetAvailableServers 获取所有可用服务器（按优先级排序）
func (fs *FallbackSelector) GetAvailableServers() []types.UpstreamServer {
	now := time.Now()
	var available []types.UpstreamServer

	fs.statusMutex.RLock()
	defer fs.statusMutex.RUnlock()

	for _, server := range fs.orderedServers {
		if fs.isServerAvailable(server.URL, now) {
			available = append(available, server)
		}
	}

	return available
}

// isServerAvailable 统一的服务器可用性判断逻辑
func (fs *FallbackSelector) isServerAvailable(url string, now time.Time) bool {
	// 检查服务器状态和冷却时间
	return fs.serverStatus[url] && now.After(fs.serverDownUntil[url])
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
	fs.serverDownUntil[url] = time.Time{}

	logger.Success("LOAD", "Server recovered: %s", url)
}

// MarkServerHealthy 标记服务器为健康
func (fs *FallbackSelector) MarkServerHealthy(url string) {
	fs.statusMutex.Lock()
	defer fs.statusMutex.Unlock()

	// 重置失败计数
	if fs.failureCount[url] > 0 {
		oldFailures := fs.failureCount[url]
		fs.failureCount[url] = 0
		logger.Info("LOAD", "Server %s healthy, reset failure count (was %d)", url, oldFailures)
	}

	// 确保服务器状态为可用
	if !fs.serverStatus[url] {
		fs.serverStatus[url] = true
		// 清除冷却时间
		fs.serverDownUntil[url] = time.Time{}
		logger.Success("LOAD", "Server %s auto-recovered from healthy request", url)
	}
}

// assignUniquePriorities 为服务器分配唯一优先级
func (fs *FallbackSelector) assignUniquePriorities() {
	// 分离已设置优先级和未设置优先级的服务器
	var explicitPriorityServers []types.UpstreamServer
	var autoPriorityServers []types.UpstreamServer

	for i := range fs.orderedServers {
		if fs.orderedServers[i].Priority > 0 {
			explicitPriorityServers = append(explicitPriorityServers, fs.orderedServers[i])
		} else {
			autoPriorityServers = append(autoPriorityServers, fs.orderedServers[i])
		}
	}

	// 检查显式优先级是否有冲突
	fs.resolveExplicitPriorityConflicts(explicitPriorityServers)

	// 为没有设置优先级的服务器自动分配优先级
	fs.assignAutoPriorities(autoPriorityServers, explicitPriorityServers)

	// 更新 orderedServers 数组
	copy(fs.orderedServers[:len(explicitPriorityServers)], explicitPriorityServers)
	copy(fs.orderedServers[len(explicitPriorityServers):], autoPriorityServers)
}

// resolveExplicitPriorityConflicts 解决显式优先级冲突
func (fs *FallbackSelector) resolveExplicitPriorityConflicts(servers []types.UpstreamServer) {
	// 按优先级分组
	priorityGroups := make(map[int][]int)
	for i, server := range servers {
		priorityGroups[server.Priority] = append(priorityGroups[server.Priority], i)
	}

	// 解决冲突：对于同一优先级的服务器，按权重重新分配优先级
	for priority, indices := range priorityGroups {
		if len(indices) > 1 {
			// 按权重排序（权重高的优先级更高）
			sort.Slice(indices, func(i, j int) bool {
				return servers[indices[i]].Weight > servers[indices[j]].Weight
			})

			// 重新分配优先级
			for i, serverIdx := range indices {
				servers[serverIdx].Priority = priority + i
				if i > 0 {
					logger.Warning("LOAD", "Priority conflict resolved: Server %s priority adjusted from %d to %d",
						servers[serverIdx].URL, priority, priority+i)
				}
			}
		}
	}
}

// assignAutoPriorities 为未设置优先级的服务器自动分配优先级
func (fs *FallbackSelector) assignAutoPriorities(autoPriorityServers []types.UpstreamServer, explicitPriorityServers []types.UpstreamServer) {
	if len(autoPriorityServers) == 0 {
		return
	}

	// 找到已使用的最大优先级
	maxUsedPriority := 0
	for _, server := range explicitPriorityServers {
		if server.Priority > maxUsedPriority {
			maxUsedPriority = server.Priority
		}
	}

	// 按权重排序未分配优先级的服务器（权重高的获得更高优先级）
	sort.Slice(autoPriorityServers, func(i, j int) bool {
		if autoPriorityServers[i].Weight == autoPriorityServers[j].Weight {
			// 权重相同时，按配置顺序（先配置的优先级更高）
			return i < j
		}
		return autoPriorityServers[i].Weight > autoPriorityServers[j].Weight
	})

	// 分配唯一优先级
	for i := range autoPriorityServers {
		autoPriorityServers[i].Priority = maxUsedPriority + i + 1
		logger.Info("LOAD", "Auto-assigned priority %d to server %s (weight: %d)",
			autoPriorityServers[i].Priority, autoPriorityServers[i].URL, autoPriorityServers[i].Weight)
	}
}
