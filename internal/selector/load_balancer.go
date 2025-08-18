package selector

import (
	"crypto/rand"
	"errors"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"claude-code-lb/internal/logger"
	"claude-code-lb/pkg/types"
)

// LoadBalancer 负载均衡选择器
type LoadBalancer struct {
	config             types.Config
	currentServerIndex int64 // 使用 atomic 操作
	serverMutex        sync.Mutex
	serverStatus       map[string]bool
	serverWeights      map[string]int // 用于平滑加权轮询
	statusMutex        sync.RWMutex
	failureCount       map[string]int64 // 服务器失败次数
}

// NewLoadBalancer 创建新的负载均衡选择器
func NewLoadBalancer(config types.Config) *LoadBalancer {
	lb := &LoadBalancer{
		config:        config,
		serverStatus:  make(map[string]bool),
		serverWeights: make(map[string]int),
		failureCount:  make(map[string]int64),
	}

	// 初始化服务器状态和权重
	for _, server := range config.Servers {
		lb.serverStatus[server.URL] = true
		weight := server.Weight
		if weight <= 0 {
			weight = 1
		}
		lb.serverWeights[server.URL] = weight
		lb.failureCount[server.URL] = 0
	}

	logger.Info("SELECTOR", "Load balancer initialized with algorithm: %s", config.Algorithm)
	return lb
}

// SelectServer 选择一个可用的服务器
func (lb *LoadBalancer) SelectServer() (*types.UpstreamServer, error) {
	availableServers := lb.GetAvailableServers()
	if len(availableServers) == 0 {
		logger.Error("SELECTOR", "No available servers for load balancing")
		return nil, errors.New("no available servers")
	}

	var selectedServer *types.UpstreamServer

	switch lb.config.Algorithm {
	case "weighted_round_robin":
		selectedServer = lb.getWeightedServer(availableServers)
	case "random":
		selectedServer = lb.getRandomServer(availableServers)
	default: // round_robin
		selectedServer = lb.getRoundRobinServer(availableServers)
	}

	if selectedServer == nil {
		return nil, errors.New("failed to select server")
	}

	logger.Info("SELECTOR", "Selected server: %s (algorithm: %s)", selectedServer.URL, lb.config.Algorithm)
	return selectedServer, nil
}

// getRoundRobinServer 轮询算法选择服务器
func (lb *LoadBalancer) getRoundRobinServer(servers []types.UpstreamServer) *types.UpstreamServer {
	if len(servers) == 0 {
		return nil
	}

	lb.serverMutex.Lock()
	defer lb.serverMutex.Unlock()

	index := atomic.AddInt64(&lb.currentServerIndex, 1) % int64(len(servers))
	return &servers[index]
}

// getWeightedServer 加权轮询算法选择服务器
func (lb *LoadBalancer) getWeightedServer(servers []types.UpstreamServer) *types.UpstreamServer {
	if len(servers) == 0 {
		return nil
	}

	if len(servers) == 1 {
		return &servers[0]
	}

	lb.serverMutex.Lock()
	defer lb.serverMutex.Unlock()

	// 计算总权重
	totalWeight := 0
	for _, server := range servers {
		weight := lb.serverWeights[server.URL]
		if weight <= 0 {
			weight = 1
		}
		totalWeight += weight
	}

	// 找到当前权重最大的服务器
	var selected *types.UpstreamServer
	maxCurrentWeight := -1

	for i := range servers {
		server := &servers[i]
		weight := lb.serverWeights[server.URL]
		if weight <= 0 {
			weight = 1
		}

		// 更新当前权重
		currentWeight := lb.serverWeights[server.URL] + weight
		lb.serverWeights[server.URL] = currentWeight

		if currentWeight > maxCurrentWeight {
			maxCurrentWeight = currentWeight
			selected = server
		}
	}

	if selected != nil {
		// 减去总权重
		lb.serverWeights[selected.URL] -= totalWeight
	}

	return selected
}

// getRandomServer 随机算法选择服务器
func (lb *LoadBalancer) getRandomServer(servers []types.UpstreamServer) *types.UpstreamServer {
	if len(servers) == 0 {
		return nil
	}

	// 使用 crypto/rand 生成更好的随机数
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(servers))))
	if err != nil {
		// 如果随机数生成失败，回退到轮询
		return lb.getRoundRobinServer(servers)
	}

	return &servers[n.Int64()]
}

// MarkServerDown 标记服务器为不可用
func (lb *LoadBalancer) MarkServerDown(url string) {
	lb.statusMutex.Lock()
	defer lb.statusMutex.Unlock()

	lb.serverStatus[url] = false

	// 增加失败计数
	lb.failureCount[url]++
	failures := lb.failureCount[url]

	// 动态计算冷却时间
	cooldownDuration := time.Duration(lb.config.Cooldown) * time.Second
	if failures > 1 {
		// 指数退避，但设置上限
		dynamicCooldown := cooldownDuration * time.Duration(failures)
		if dynamicCooldown > 10*time.Minute {
			dynamicCooldown = 10 * time.Minute // 最大 10 分钟
		}
	}

	downUntil := time.Now().Add(cooldownDuration)

	// 更新对应服务器的冷却时间
	for i, server := range lb.config.Servers {
		if server.URL == url {
			lb.config.Servers[i].DownUntil = downUntil
			break
		}
	}

	logger.Warning("SELECTOR", "Server marked down: %s (failures: %d, cooldown: %v)", url, failures, cooldownDuration)
}

// GetAvailableServers 获取所有可用服务器
func (lb *LoadBalancer) GetAvailableServers() []types.UpstreamServer {
	now := time.Now()
	var available []types.UpstreamServer

	lb.statusMutex.RLock()
	defer lb.statusMutex.RUnlock()

	for _, server := range lb.config.Servers {
		if lb.serverStatus[server.URL] && now.After(server.DownUntil) {
			available = append(available, server)
		}
	}

	return available
}

// GetServerStatus 获取服务器状态
func (lb *LoadBalancer) GetServerStatus() map[string]bool {
	lb.statusMutex.RLock()
	defer lb.statusMutex.RUnlock()

	status := make(map[string]bool)
	for k, v := range lb.serverStatus {
		status[k] = v
	}
	return status
}

// RecoverServer 恢复服务器
func (lb *LoadBalancer) RecoverServer(url string) {
	lb.statusMutex.Lock()
	defer lb.statusMutex.Unlock()

	lb.serverStatus[url] = true

	// 清除冷却时间
	for i, server := range lb.config.Servers {
		if server.URL == url {
			lb.config.Servers[i].DownUntil = time.Time{}
			break
		}
	}

	logger.Success("SELECTOR", "Server recovered: %s", url)
}

// MarkServerHealthy 标记服务器为健康
func (lb *LoadBalancer) MarkServerHealthy(url string) {
	lb.statusMutex.Lock()
	defer lb.statusMutex.Unlock()

	// 重置失败计数
	if lb.failureCount[url] > 0 {
		oldFailures := lb.failureCount[url]
		lb.failureCount[url] = 0
		logger.Info("SELECTOR", "Server %s healthy, reset failure count (was %d)", url, oldFailures)
	}

	// 确保服务器状态为可用
	if !lb.serverStatus[url] {
		lb.serverStatus[url] = true
		// 清除冷却时间
		for i, server := range lb.config.Servers {
			if server.URL == url {
				lb.config.Servers[i].DownUntil = time.Time{}
				break
			}
		}
		logger.Success("SELECTOR", "Server %s auto-recovered from healthy request", url)
	}
}
