package stats

import (
	"sync"
	"sync/atomic"
	"time"

	"claude-code-lb/internal/logger"

	"github.com/gin-gonic/gin"
)

type Reporter struct {
	requestCount         int64
	errorCount           int64
	totalResponseTime    int64
	requestCountByServer map[string]int64
	responseTimeByServer map[string]int64
	mutex                sync.Mutex
}

func New() *Reporter {
	return &Reporter{
		requestCountByServer: make(map[string]int64),
		responseTimeByServer: make(map[string]int64),
	}
}

func (r *Reporter) IncrementRequestCount() {
	atomic.AddInt64(&r.requestCount, 1)
}

func (r *Reporter) IncrementErrorCount() {
	atomic.AddInt64(&r.errorCount, 1)
}

func (r *Reporter) AddResponseTime(responseTime int64) {
	atomic.AddInt64(&r.totalResponseTime, responseTime)
}

func (r *Reporter) AddServerStats(serverURL string, responseTime int64) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.requestCountByServer[serverURL]++
	r.responseTimeByServer[serverURL] += responseTime
}

func (r *Reporter) LogStats() {
	totalRequests := atomic.LoadInt64(&r.requestCount)
	totalErrors := atomic.LoadInt64(&r.errorCount)
	avgResponseTime := int64(0)
	if totalRequests > 0 {
		avgResponseTime = atomic.LoadInt64(&r.totalResponseTime) / totalRequests
	}

	logger.Info("STATS", "Requests: %d | Errors: %d | Avg time: %dms",
		totalRequests, totalErrors, avgResponseTime)
}

// StartReporter 定期统计显示
func (r *Reporter) StartReporter() {
	ticker := time.NewTicker(5 * time.Minute) // 每5分钟显示一次统计
	defer ticker.Stop()

	for range ticker.C {
		r.LogStats()
	}
}

// GinLoggerMiddleware Gin 日志中间件
func (r *Reporter) GinLoggerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		raw := c.Request.URL.RawQuery

		// 处理请求
		c.Next()

		// 计算延迟
		latency := time.Since(start)

		// 获取状态码
		statusCode := c.Writer.Status()

		// 获取客户端IP
		clientIP := c.ClientIP()

		// 获取请求方法
		method := c.Request.Method

		// 构建完整路径
		if raw != "" {
			path = path + "?" + raw
		}

		// 根据状态码选择日志级别
		if statusCode >= 500 {
			logger.Error("HTTP", "%s %s | %d | %v | %s", method, path, statusCode, latency, clientIP)
		} else if statusCode >= 400 {
			logger.Warning("HTTP", "%s %s | %d | %v | %s", method, path, statusCode, latency, clientIP)
		} else {
			// 对于健康检查路径，使用更低级别的日志
			if path == "/health" {
				// 健康检查请求不记录日志，避免日志噪音
				return
			}
			logger.Info("HTTP", "%s %s | %d | %v | %s", method, path, statusCode, latency, clientIP)
		}
	}
}
