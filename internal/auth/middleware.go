package auth

import (
	"slices"
	"strings"

	"claude-code-lb/internal/logger"
	"claude-code-lb/pkg/types"

	"github.com/gin-gonic/gin"
)

// 辅助函数
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Middleware 鉴权中间件
func Middleware(config types.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 如果未启用鉴权，直接通过
		if !config.Auth {
			c.Next()
			return
		}

		// 检查 Authorization header
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			logger.Auth(false, "Missing Authorization header from %s", c.ClientIP())
			c.JSON(401, gin.H{"error": "Missing Authorization header"})
			c.Abort()
			return
		}

		// 提取 Bearer token
		const bearerPrefix = "Bearer "
		if !strings.HasPrefix(authHeader, bearerPrefix) {
			logger.Auth(false, "Invalid header format from %s", c.ClientIP())
			c.JSON(401, gin.H{"error": "Invalid Authorization header format"})
			c.Abort()
			return
		}

		token := authHeader[len(bearerPrefix):]

		// 检查 token 是否在允许的列表中
		if !slices.Contains(config.AuthKeys, token) {
			logger.Auth(false, "Invalid API key %s...%s from %s",
				token[:min(8, len(token))],
				token[max(0, len(token)-8):],
				c.ClientIP())
			c.JSON(401, gin.H{"error": "Invalid API key"})
			c.Abort()
			return
		}

		logger.Auth(true, "Valid API key %s...%s from %s",
			token[:min(8, len(token))],
			token[max(0, len(token)-8):],
			c.ClientIP())
		c.Next()
	}
}
