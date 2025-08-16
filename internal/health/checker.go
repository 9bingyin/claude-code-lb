package health

import (
	"time"

	"claude-code-lb/internal/balance"
	"claude-code-lb/internal/logger"
	"claude-code-lb/pkg/types"
)

type Checker struct {
	config   types.Config
	balancer *balance.Balancer
}

func NewChecker(config types.Config, balancer *balance.Balancer) *Checker {
	return &Checker{
		config:   config,
		balancer: balancer,
	}
}

// PassiveHealthCheck 被动健康检查：定期检查冷却时间到期的服务器，将其标记为可用
func (h *Checker) PassiveHealthCheck() {
	ticker := time.NewTicker(time.Duration(h.config.Cooldown) * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		serverStatus := h.balancer.GetServerStatus()
		
		for _, server := range h.config.Servers {
			// 检查冷却时间是否已到期
			if !serverStatus[server.URL] && now.After(server.DownUntil) {
				// 冷却时间已到期，重新标记为可用
				h.balancer.RecoverServer(server.URL)
				logger.Success("CIRCUIT", "Server recovered: %s (cooldown expired)", server.URL)
			}
		}
	}
}