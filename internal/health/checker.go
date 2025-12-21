package health

import (
	"sync"
	"time"

	"claude-code-lb/internal/balance"
	"claude-code-lb/internal/logger"
	"claude-code-lb/pkg/types"
)

type Checker struct {
	config   types.Config
	balancer *balance.Balancer
	stopChan chan struct{}
	stopOnce sync.Once
}

func NewChecker(config types.Config, balancer *balance.Balancer) *Checker {
	return &Checker{
		config:   config,
		balancer: balancer,
		stopChan: make(chan struct{}),
	}
}

// Stop 停止健康检查器
func (h *Checker) Stop() {
	h.stopOnce.Do(func() {
		close(h.stopChan)
	})
}

// PassiveHealthCheck 被动健康检查：定期检查冷却时间到期的服务器，将其标记为可用
func (h *Checker) PassiveHealthCheck() {
	ticker := time.NewTicker(time.Duration(h.config.Cooldown) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			now := time.Now()
			serverStatus := h.balancer.GetServerStatus()

			for _, server := range h.config.Servers {
				// 检查冷却时间是否已到期
				// 注意：使用 balancer.GetServerDownUntil 获取实际的冷却时间
				// 而不是 server.DownUntil（那是配置中的零值）
				downUntil := h.balancer.GetServerDownUntil(server.URL)
				if !serverStatus[server.URL] && now.After(downUntil) {
					// 冷却时间已到期，重新标记为可用
					h.balancer.RecoverServer(server.URL)
					logger.Success("HEAL", "Server recovered: %s (cooldown expired)", server.URL)
				}
			}
		case <-h.stopChan:
			logger.Info("HEAL", "Health checker stopped")
			return
		}
	}
}
