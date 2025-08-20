package balance

import (
	"context"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"claude-code-lb/internal/logger"
	"claude-code-lb/pkg/types"
)

const (
	// DefaultCommandTimeout 默认命令超时时间
	DefaultCommandTimeout = 30 * time.Second
)

// BalanceInfo 余额信息
type BalanceInfo struct {
	Balance     float64   `json:"balance"`
	LastChecked time.Time `json:"last_checked"`
	Status      string    `json:"status"` // "success", "error", "unknown"
	Error       string    `json:"error,omitempty"`
}

// BalanceChecker 余额查询器
type BalanceChecker struct {
	config          types.Config
	balances        map[string]*BalanceInfo
	mutex           sync.RWMutex
	stopChan        chan struct{}
	commandTimeout  time.Duration
	serverTimers    map[string]*time.Ticker // 每个服务器的定时器
	balancer        BalancerInterface       // 负载均衡器接口
	commandExecutor CommandExecutor         // 命令执行器接口
	stopOnce        sync.Once               // 确保Stop只执行一次
}

// BalancerInterface 负载均衡器接口（用于解耦）
type BalancerInterface interface {
	MarkServerDown(url string)
}

// CommandExecutor 命令执行器接口
type CommandExecutor interface {
	ExecuteCommand(command string) (float64, error)
}

// DefaultCommandExecutor 默认命令执行器
type DefaultCommandExecutor struct {
	Timeout time.Duration
}

// ExecuteCommand 执行系统命令
func (e *DefaultCommandExecutor) ExecuteCommand(command string) (float64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), e.Timeout)
	defer cancel()

	// 使用跨平台的 shell：Windows 使用 cmd，其他系统使用 sh
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	}

	output, err := cmd.Output()
	if err != nil {
		return 0, err
	}

	// 解析输出为数字
	balanceStr := strings.TrimSpace(string(output))
	balance, err := strconv.ParseFloat(balanceStr, 64)
	if err != nil {
		return 0, err
	}

	return balance, nil
}

// NewBalanceChecker 创建新的余额查询器
func NewBalanceChecker(config types.Config, balancer BalancerInterface) *BalanceChecker {
	return &BalanceChecker{
		config:          config,
		balances:        make(map[string]*BalanceInfo),
		stopChan:        make(chan struct{}),
		commandTimeout:  DefaultCommandTimeout,
		serverTimers:    make(map[string]*time.Ticker),
		balancer:        balancer,
		commandExecutor: &DefaultCommandExecutor{Timeout: DefaultCommandTimeout},
	}
}

// NewBalanceCheckerWithExecutor 创建带有自定义命令执行器的余额查询器
func NewBalanceCheckerWithExecutor(config types.Config, balancer BalancerInterface, executor CommandExecutor) *BalanceChecker {
	return &BalanceChecker{
		config:          config,
		balances:        make(map[string]*BalanceInfo),
		stopChan:        make(chan struct{}),
		commandTimeout:  DefaultCommandTimeout,
		serverTimers:    make(map[string]*time.Ticker),
		balancer:        balancer,
		commandExecutor: executor,
	}
}

// Start 启动余额查询
func (bc *BalanceChecker) Start() {
	// 统计有多少服务器配置了余额查询
	var enabledServers int
	for _, server := range bc.config.Servers {
		if server.BalanceCheck != "" {
			enabledServers++
		}
	}

	if enabledServers == 0 {
		logger.Info("MONEY", "Balance check disabled (no servers configured)")
		return
	}

	logger.Info("MONEY", "Balance checker started: %d servers with individual intervals", enabledServers)

	// 为每个配置了余额查询的服务器启动独立的定时器
	for _, server := range bc.config.Servers {
		if server.BalanceCheck != "" {
			bc.startServerBalanceCheck(server)
		}
	}
}

// Stop 停止余额查询
func (bc *BalanceChecker) Stop() {
	bc.stopOnce.Do(func() {
		close(bc.stopChan)

		// 停止所有服务器的定时器
		bc.mutex.Lock()
		defer bc.mutex.Unlock()

		for _, ticker := range bc.serverTimers {
			if ticker != nil {
				ticker.Stop()
			}
		}
	})
}

// startServerBalanceCheck 为单个服务器启动余额查询
func (bc *BalanceChecker) startServerBalanceCheck(server types.UpstreamServer) {
	// 如果没有设置间隔，使用默认300秒（5分钟）
	interval := server.BalanceCheckInterval
	if interval <= 0 {
		interval = 300
	}

	logger.Info("MONEY", "Starting balance check for %s: interval %d seconds", server.URL, interval)

	// 立即执行一次查询
	go bc.checkServerBalance(server)

	// 启动定时器
	go func(s types.UpstreamServer, intervalSeconds int) {
		ticker := time.NewTicker(time.Duration(intervalSeconds) * time.Second)
		defer ticker.Stop()

		// 将ticker存储到map中
		bc.mutex.Lock()
		bc.serverTimers[s.URL] = ticker
		bc.mutex.Unlock()

		for {
			select {
			case <-ticker.C:
				bc.checkServerBalance(s)
			case <-bc.stopChan:
				return
			}
		}
	}(server, interval)
}

// checkServerBalance 检查单个服务器的余额
func (bc *BalanceChecker) checkServerBalance(server types.UpstreamServer) {
	startTime := time.Now()

	balance, err := bc.commandExecutor.ExecuteCommand(server.BalanceCheck)

	bc.mutex.Lock()
	defer bc.mutex.Unlock()

	balanceInfo := &BalanceInfo{
		LastChecked: startTime,
	}

	if err != nil {
		balanceInfo.Status = "error"
		balanceInfo.Error = err.Error()
		logger.Error("MONEY", "Failed to check balance for %s: %v (server remains available)", server.URL, err)
		// 注意：余额检查失败不标记服务器为不可用，只记录错误
	} else {
		balanceInfo.Status = "success"
		balanceInfo.Balance = balance

		// 获取余额阈值，默认为0（即余额小于等于0时才标记为不可用）
		threshold := server.BalanceThreshold
		// 注意：Go中float64零值就是0，所以这里不需要额外处理

		// 检查余额是否低于或等于阈值
		if balance <= threshold {
			logger.Warning("MONEY", "Balance insufficient for %s: %.2f <= %.2f (marking as down)",
				server.URL, balance, threshold)
			// 标记服务器为不可用
			if bc.balancer != nil {
				bc.balancer.MarkServerDown(server.URL)
			}
		} else {
			logger.Success("MONEY", "Balance for %s: %.2f (checked in %dms)",
				server.URL, balance, time.Since(startTime).Milliseconds())
		}
	}

	bc.balances[server.URL] = balanceInfo
}

// GetBalance 获取服务器余额信息
func (bc *BalanceChecker) GetBalance(serverURL string) *BalanceInfo {
	bc.mutex.RLock()
	defer bc.mutex.RUnlock()

	if info, exists := bc.balances[serverURL]; exists {
		// 创建副本避免并发问题
		return &BalanceInfo{
			Balance:     info.Balance,
			LastChecked: info.LastChecked,
			Status:      info.Status,
			Error:       info.Error,
		}
	}

	return &BalanceInfo{
		Status: "unknown",
	}
}

// GetAllBalances 获取所有服务器的余额信息
func (bc *BalanceChecker) GetAllBalances() map[string]*BalanceInfo {
	bc.mutex.RLock()
	defer bc.mutex.RUnlock()

	result := make(map[string]*BalanceInfo)
	for url, info := range bc.balances {
		result[url] = &BalanceInfo{
			Balance:     info.Balance,
			LastChecked: info.LastChecked,
			Status:      info.Status,
			Error:       info.Error,
		}
	}

	return result
}
