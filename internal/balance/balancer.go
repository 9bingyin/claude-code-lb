package balance

import (
	"errors"
	"sync"
	"time"

	"claude-code-lb/internal/logger"
	"claude-code-lb/pkg/types"
)

type Balancer struct {
	config             types.Config
	currentServerIndex int
	serverMutex        sync.Mutex
	serverStatus       map[string]bool
	statusMutex        sync.RWMutex
}

func New(config types.Config) *Balancer {
	b := &Balancer{
		config:       config,
		serverStatus: make(map[string]bool),
	}

	// 初始化服务器状态
	for _, server := range config.LoadBalancer.Servers {
		b.serverStatus[server.URL] = true
	}

	return b
}

func (b *Balancer) GetNextServer() (*types.UpstreamServer, error) {
	b.serverMutex.Lock()
	defer b.serverMutex.Unlock()

	availableServers := b.getAvailableServers()
	if len(availableServers) == 0 {
		if b.config.Fallback {
			logger.Error("BALANCE", "All %d servers are down or cooling down", len(b.config.LoadBalancer.Servers))
			return nil, errors.New("all servers are down or cooling down")
		} else {
			return nil, errors.New("no available servers")
		}
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
	server := &servers[b.currentServerIndex%len(servers)]
	b.currentServerIndex++
	return server
}

func (b *Balancer) getWeightedServer(servers []types.UpstreamServer) *types.UpstreamServer {
	if len(servers) == 0 {
		return nil
	}

	totalWeight := 0
	for _, server := range servers {
		weight := server.Weight
		if weight <= 0 {
			weight = 1
		}
		totalWeight += weight
	}

	target := b.currentServerIndex % totalWeight
	b.currentServerIndex++

	currentWeight := 0
	for _, server := range servers {
		weight := server.Weight
		if weight <= 0 {
			weight = 1
		}
		currentWeight += weight
		if target < currentWeight {
			return &server
		}
	}
	return &servers[0]
}

func (b *Balancer) getRandomServer(servers []types.UpstreamServer) *types.UpstreamServer {
	if len(servers) == 0 {
		return nil
	}
	index := time.Now().UnixNano() % int64(len(servers))
	return &servers[index]
}

func (b *Balancer) MarkServerDown(url string) {
	b.statusMutex.Lock()
	defer b.statusMutex.Unlock()

	b.serverStatus[url] = false

	// 设置冷却时间
	cooldownDuration := time.Duration(b.config.CircuitBreaker.CooldownSeconds) * time.Second
	downUntil := time.Now().Add(cooldownDuration)

	// 更新对应服务器的冷却时间
	for i, server := range b.config.LoadBalancer.Servers {
		if server.URL == url {
			b.config.LoadBalancer.Servers[i].DownUntil = downUntil
			break
		}
	}

	logger.Error("CIRCUIT", "Server DOWN: %s (cooldown: %ds)", url, b.config.CircuitBreaker.CooldownSeconds)
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