package balance

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

type Balancer struct {
	config             types.Config
	currentServerIndex int64 // 使用 atomic 操作
	serverMutex        sync.Mutex
	serverStatus       map[string]bool
	serverWeights      map[string]int // 用于平滑加权轮询
	statusMutex        sync.RWMutex
	failureCount       map[string]int64 // 服务器失败次数
}

func New(config types.Config) *Balancer {
	b := &Balancer{
		config:        config,
		serverStatus:  make(map[string]bool),
		serverWeights: make(map[string]int),
		failureCount:  make(map[string]int64),
	}

	// 初始化服务器状态和权重
	for _, server := range config.LoadBalancer.Servers {
		b.serverStatus[server.URL] = true
		weight := server.Weight
		if weight <= 0 {
			weight = 1
		}
		b.serverWeights[server.URL] = weight
		b.failureCount[server.URL] = 0
	}

	return b
}

func (b *Balancer) GetNextServer() (*types.UpstreamServer, error) {
	return b.GetNextServerWithFallback(false)
}

// GetNextServerWithFallback 获取下一个服务器，支持 fallback 模式
func (b *Balancer) GetNextServerWithFallback(useFallback bool) (*types.UpstreamServer, error) {
	b.serverMutex.Lock()
	defer b.serverMutex.Unlock()

	availableServers := b.getAvailableServers()
	if len(availableServers) == 0 {
		if !b.config.Fallback && !useFallback {
			return nil, errors.New("no available servers")
		}
		
		// Fallback 模式：选择冷却时间最短的服务器进行紧急重试
		fallbackServer := b.getFallbackServer()
		if fallbackServer != nil {
			logger.Warning("BALANCE", "Using fallback server: %s", fallbackServer.URL)
			return fallbackServer, nil
		}
		
		logger.Error("BALANCE", "All %d servers are down, no fallback available", len(b.config.LoadBalancer.Servers))
		return nil, errors.New("all servers are down")
	}

	switch b.config.LoadBalancer.Type {
	case "weighted_round_robin":
		return b.getWeightedServer(availableServers), nil
	case "random":
		return b.getRandomServer(availableServers), nil
	default: // round_robin
		return b.getRoundRobinServer(availableServers), nil
	}
}

func (b *Balancer) getAvailableServers() []types.UpstreamServer {
	b.statusMutex.RLock()
	defer b.statusMutex.RUnlock()

	var available []types.UpstreamServer
	var down, cooling int
	now := time.Now()

	for _, server := range b.config.LoadBalancer.Servers {
		// 检查服务器状态和冷却时间
		isUp := b.serverStatus[server.URL]
		notCoolingDown := now.After(server.DownUntil)

		if !isUp {
			down++
		}
		if !notCoolingDown {
			cooling++
		}

		if isUp && notCoolingDown {
			available = append(available, server)
		}
	}

	if down > 0 || cooling > 0 {
		logger.Warning("STATUS", "Servers: %d available, %d down, %d cooling",
			len(available), down, cooling)
	}
	return available
}

func (b *Balancer) getRoundRobinServer(servers []types.UpstreamServer) *types.UpstreamServer {
	if len(servers) == 0 {
		return nil
	}
	// 使用 atomic 操作以减少锁竞争
	index := atomic.AddInt64(&b.currentServerIndex, 1) - 1
	server := &servers[index%int64(len(servers))]
	return server
}

// getWeightedServer 平滑加权轮询算法
func (b *Balancer) getWeightedServer(servers []types.UpstreamServer) *types.UpstreamServer {
	if len(servers) == 0 {
		return nil
	}
	
	if len(servers) == 1 {
		return &servers[0]
	}

	// 计算总权重
	totalWeight := 0
	for _, server := range servers {
		weight := b.serverWeights[server.URL]
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
		weight := b.serverWeights[server.URL]
		if weight <= 0 {
			weight = 1
		}
		
		// 更新当前权重（在 serverWeights 中存储当前权重）
		currentWeight := b.serverWeights[server.URL] + weight
		b.serverWeights[server.URL] = currentWeight
		
		if currentWeight > maxCurrentWeight {
			maxCurrentWeight = currentWeight
			selected = server
		}
	}
	
	if selected != nil {
		// 减去总权重
		b.serverWeights[selected.URL] -= totalWeight
	}
	
	return selected
}

func (b *Balancer) getRandomServer(servers []types.UpstreamServer) *types.UpstreamServer {
	if len(servers) == 0 {
		return nil
	}
	
	// 使用 crypto/rand 生成更好的随机数
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(servers))))
	if err != nil {
		// fallback 到时间戳
		index := time.Now().UnixNano() % int64(len(servers))
		return &servers[index]
	}
	return &servers[n.Int64()]
}

// getFallbackServer 获取 fallback 服务器（冷却时间最短的）
func (b *Balancer) getFallbackServer() *types.UpstreamServer {
	now := time.Now()
	var bestServer *types.UpstreamServer
	var shortestCooldown time.Duration = time.Hour * 24 // 初始化为很大的值
	
	for i, server := range b.config.LoadBalancer.Servers {
		if now.After(server.DownUntil) {
			// 如果已经过了冷却时间，优先选择
			return &b.config.LoadBalancer.Servers[i]
		}
		
		// 找到冷却时间最短的服务器
		cooldownRemaining := server.DownUntil.Sub(now)
		if cooldownRemaining < shortestCooldown {
			shortestCooldown = cooldownRemaining
			bestServer = &b.config.LoadBalancer.Servers[i]
		}
	}
	
	return bestServer
}

func (b *Balancer) MarkServerDown(url string) {
	b.statusMutex.Lock()
	defer b.statusMutex.Unlock()

	b.serverStatus[url] = false
	
	// 增加失败计数
	b.failureCount[url]++
	failures := b.failureCount[url]

	// 动态计算冷却时间（指数退避）
	baseCooldown := time.Duration(b.config.CircuitBreaker.CooldownSeconds) * time.Second
	dynamicCooldown := baseCooldown
	if failures > 1 {
		// 指数退避：2^(failures-1) * baseCooldown，最多 10 分钟
		multiplier := int64(1) << (failures - 1)
		if multiplier > 10 {
			multiplier = 10 // 最大 10 倍
		}
		dynamicCooldown = time.Duration(multiplier) * baseCooldown
		if dynamicCooldown > 10*time.Minute {
			dynamicCooldown = 10 * time.Minute // 最大 10 分钟
		}
	}
	
	downUntil := time.Now().Add(dynamicCooldown)

	// 更新对应服务器的冷却时间
	for i, server := range b.config.LoadBalancer.Servers {
		if server.URL == url {
			b.config.LoadBalancer.Servers[i].DownUntil = downUntil
			break
		}
	}

	logger.Error("CIRCUIT", "Server DOWN: %s (failures: %d, cooldown: %v)", url, failures, dynamicCooldown)
}

func (b *Balancer) GetAvailableServers() []types.UpstreamServer {
	return b.getAvailableServers()
}

func (b *Balancer) GetServerStatus() map[string]bool {
	b.statusMutex.RLock()
	defer b.statusMutex.RUnlock()
	
	status := make(map[string]bool)
	for k, v := range b.serverStatus {
		status[k] = v
	}
	return status
}

func (b *Balancer) RecoverServer(url string) {
	b.statusMutex.Lock()
	defer b.statusMutex.Unlock()

	b.serverStatus[url] = true
	
	// 清除冷却时间
	for i, server := range b.config.LoadBalancer.Servers {
		if server.URL == url {
			b.config.LoadBalancer.Servers[i].DownUntil = time.Time{}
			break
		}
	}
	
	logger.Success("CIRCUIT", "Server recovered: %s", url)
}

// MarkServerHealthy 标记服务器为健康（成功请求后调用）
func (b *Balancer) MarkServerHealthy(url string) {
	b.statusMutex.Lock()
	defer b.statusMutex.Unlock()
	
	// 重置失败计数
	if b.failureCount[url] > 0 {
		oldFailures := b.failureCount[url]
		b.failureCount[url] = 0
		logger.Info("CIRCUIT", "Server %s healthy, reset failure count (was %d)", url, oldFailures)
	}
	
	// 确保服务器状态为可用
	if !b.serverStatus[url] {
		b.serverStatus[url] = true
		// 清除冷却时间
		for i, server := range b.config.LoadBalancer.Servers {
			if server.URL == url {
				b.config.LoadBalancer.Servers[i].DownUntil = time.Time{}
				break
			}
		}
		logger.Success("CIRCUIT", "Server %s auto-recovered from healthy request", url)
	}
}