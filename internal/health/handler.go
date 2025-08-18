package health

import (
	"time"

	"claude-code-lb/internal/balance"
	"claude-code-lb/pkg/types"

	"github.com/gin-gonic/gin"
)

func Handler(config types.Config, balancer *balance.Balancer) gin.HandlerFunc {
	return func(c *gin.Context) {
		availableServers := balancer.GetAvailableServers()
		serverStatus := balancer.GetServerStatus()

		// 统计冷却中的服务器
		var coolingDownServers int
		now := time.Now()
		for _, server := range config.Servers {
			if !serverStatus[server.URL] && now.Before(server.DownUntil) {
				coolingDownServers++
			}
		}

		c.JSON(200, gin.H{
			"status":            "ok",
			"total_servers":     len(config.Servers),
			"available_servers": len(availableServers),
			"cooling_down":      coolingDownServers,
			"load_balancer":     config.Algorithm,
			"fallback":          config.Fallback,
			"cooldown_seconds":  config.Cooldown,
			"time":              time.Now().Format(time.RFC3339),
		})
	}
}
