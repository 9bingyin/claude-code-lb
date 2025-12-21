package selector

import (
	"sync"
	"time"

	"claude-code-lb/internal/logger"
	"claude-code-lb/pkg/types"
)

// BaseSelector 包含所有选择器共享的状态和方法
type BaseSelector struct {
	config              types.Config
	serverStatus        map[string]bool
	serverDownUntil     map[string]time.Time
	failureCount        map[string]int64
	consecutiveFailures map[string]int
	serverThresholds    map[string]int
	statusMutex         sync.RWMutex
	logTag              string // 用于区分日志来源
}

// NewBaseSelector 创建基础选择器
func NewBaseSelector(config types.Config, logTag string) *BaseSelector {
	bs := &BaseSelector{
		config:              config,
		serverStatus:        make(map[string]bool),
		serverDownUntil:     make(map[string]time.Time),
		failureCount:        make(map[string]int64),
		consecutiveFailures: make(map[string]int),
		serverThresholds:    make(map[string]int),
		logTag:              logTag,
	}

	// 初始化服务器状态
	for _, server := range config.Servers {
		bs.serverStatus[server.URL] = true
		bs.serverDownUntil[server.URL] = time.Time{}
		bs.failureCount[server.URL] = 0
		bs.consecutiveFailures[server.URL] = 0
		threshold := server.FailThreshold
		if threshold <= 0 {
			threshold = 1
		}
		bs.serverThresholds[server.URL] = threshold
	}

	return bs
}

// MarkServerDown 标记服务器为不可用
func (bs *BaseSelector) MarkServerDown(url string, strict bool) {
	bs.statusMutex.Lock()
	defer bs.statusMutex.Unlock()

	// 增加连续失败计数
	bs.consecutiveFailures[url]++
	consecutiveFailures := bs.consecutiveFailures[url]
	threshold := bs.serverThresholds[url]

	// 检查是否达到失败阈值
	// 如果 strict 为 true，则忽略阈值直接标记失败
	if !strict && consecutiveFailures < threshold {
		logger.Warning(bs.logTag, "Server %s failed (%d/%d), not marking down yet", url, consecutiveFailures, threshold)
		return
	}

	bs.serverStatus[url] = false

	// 增加失败计数（用于动态冷却）
	bs.failureCount[url]++
	failures := bs.failureCount[url]

	// 动态计算冷却时间（指数退避）
	cooldownDuration := time.Duration(bs.config.Cooldown) * time.Second
	if failures > 1 {
		// 指数退避，但设置上限
		dynamicCooldown := cooldownDuration * time.Duration(failures)
		if dynamicCooldown > 10*time.Minute {
			dynamicCooldown = 10 * time.Minute // 最大 10 分钟
		}
		cooldownDuration = dynamicCooldown
	}

	downUntil := time.Now().Add(cooldownDuration)

	// 记录服务器冷却时间
	bs.serverDownUntil[url] = downUntil

	// 重置连续失败计数
	bs.consecutiveFailures[url] = 0

	logger.Warning(bs.logTag, "Server marked down: %s (threshold: %d, total failures: %d, cooldown: %v)", url, threshold, failures, cooldownDuration)
}

// MarkServerHealthy 标记服务器为健康
func (bs *BaseSelector) MarkServerHealthy(url string) {
	bs.statusMutex.Lock()
	defer bs.statusMutex.Unlock()

	// 重置连续失败计数
	if bs.consecutiveFailures[url] > 0 {
		bs.consecutiveFailures[url] = 0
	}

	// 重置失败计数
	if bs.failureCount[url] > 0 {
		oldFailures := bs.failureCount[url]
		bs.failureCount[url] = 0
		logger.Info(bs.logTag, "Server %s healthy, reset failure count (was %d)", url, oldFailures)
	}

	// 确保服务器状态为可用
	if !bs.serverStatus[url] {
		bs.serverStatus[url] = true
		// 清除冷却时间
		bs.serverDownUntil[url] = time.Time{}
		logger.Success(bs.logTag, "Server %s auto-recovered from healthy request", url)
	}
}

// IsServerAvailable 检查服务器是否可用
func (bs *BaseSelector) IsServerAvailable(url string, now time.Time) bool {
	return bs.serverStatus[url] && now.After(bs.serverDownUntil[url])
}

// GetServerStatus 获取服务器状态
func (bs *BaseSelector) GetServerStatus() map[string]bool {
	bs.statusMutex.RLock()
	defer bs.statusMutex.RUnlock()

	status := make(map[string]bool)
	for k, v := range bs.serverStatus {
		status[k] = v
	}
	return status
}

// GetServerDownUntil 获取服务器的冷却结束时间
func (bs *BaseSelector) GetServerDownUntil(url string) time.Time {
	bs.statusMutex.RLock()
	defer bs.statusMutex.RUnlock()
	return bs.serverDownUntil[url]
}

// RecoverServer 恢复服务器
func (bs *BaseSelector) RecoverServer(url string) {
	bs.statusMutex.Lock()
	defer bs.statusMutex.Unlock()

	bs.serverStatus[url] = true

	// 清除冷却时间
	bs.serverDownUntil[url] = time.Time{}

	logger.Success(bs.logTag, "Server recovered: %s", url)
}

// GetConfig 获取配置（供子类使用）
func (bs *BaseSelector) GetConfig() types.Config {
	return bs.config
}

// RLock 获取读锁（供子类使用）
func (bs *BaseSelector) RLock() {
	bs.statusMutex.RLock()
}

// RUnlock 释放读锁（供子类使用）
func (bs *BaseSelector) RUnlock() {
	bs.statusMutex.RUnlock()
}
