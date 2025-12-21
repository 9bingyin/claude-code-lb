package selector

import (
	"crypto/rand"
	"errors"
	"math/big"
	"sync"
	"time"

	"claude-code-lb/internal/logger"
	"claude-code-lb/pkg/types"
)

// LoadBalancer 负载均衡选择器
type LoadBalancer struct {
	*BaseSelector                  // 嵌入基础选择器
	currentServerIndex int64       // 轮询索引
	serverMutex        sync.Mutex
	serverWeights      map[string]int // 用于平滑加权轮询
}

// NewLoadBalancer 创建新的负载均衡选择器
func NewLoadBalancer(config types.Config) *LoadBalancer {
	lb := &LoadBalancer{
		BaseSelector:  NewBaseSelector(config, "LOAD"),
		serverWeights: make(map[string]int),
	}

	// 初始化权重
	for _, server := range config.Servers {
		weight := server.Weight
		if weight <= 0 {
			weight = 1
		}
		lb.serverWeights[server.URL] = weight
	}

	logger.Info("LOAD", "Load balancer initialized with algorithm: %s", config.Algorithm)
	return lb
}

// SelectServer 选择一个可用的服务器
func (lb *LoadBalancer) SelectServer() (*types.UpstreamServer, error) {
	availableServers := lb.GetAvailableServers()
	if len(availableServers) == 0 {
		logger.Error("LOAD", "No available servers for load balancing")
		return nil, errors.New("no available servers")
	}

	var selectedServer *types.UpstreamServer

	switch lb.config.Algorithm {
	case types.AlgorithmWeightedRoundRobin:
		selectedServer = lb.getWeightedServer(availableServers)
	case types.AlgorithmRandom:
		selectedServer = lb.getRandomServer(availableServers)
	default: // round_robin
		selectedServer = lb.getRoundRobinServer(availableServers)
	}

	if selectedServer == nil {
		return nil, errors.New("failed to select server")
	}

	logger.Info("LOAD", "Selected server: %s (algorithm: %s)", selectedServer.URL, lb.config.Algorithm)
	return selectedServer, nil
}

// getRoundRobinServer 轮询算法选择服务器
func (lb *LoadBalancer) getRoundRobinServer(servers []types.UpstreamServer) *types.UpstreamServer {
	if len(servers) == 0 {
		return nil
	}

	lb.serverMutex.Lock()
	defer lb.serverMutex.Unlock()

	lb.currentServerIndex++
	index := lb.currentServerIndex % int64(len(servers))
	return &servers[index]
}

// getWeightedServer 平滑加权轮询算法选择服务器
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
		weight := server.Weight
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
		originalWeight := server.Weight
		if originalWeight <= 0 {
			originalWeight = 1
		}

		// 增加原始权重到当前权重
		lb.serverWeights[server.URL] += originalWeight
		currentWeight := lb.serverWeights[server.URL]

		if currentWeight > maxCurrentWeight {
			maxCurrentWeight = currentWeight
			selected = server
		}
	}

	if selected != nil {
		// 选中的服务器减去总权重
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

// GetAvailableServers 获取所有可用服务器
func (lb *LoadBalancer) GetAvailableServers() []types.UpstreamServer {
	now := time.Now()
	var available []types.UpstreamServer

	lb.RLock()
	defer lb.RUnlock()

	for _, server := range lb.config.Servers {
		if lb.IsServerAvailable(server.URL, now) {
			available = append(available, server)
		}
	}

	return available
}
