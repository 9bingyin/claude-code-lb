package proxy

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"claude-code-lb/internal/balance"
	"claude-code-lb/internal/logger"
	"claude-code-lb/internal/stats"

	"github.com/gin-gonic/gin"
)

// formatRequestURL 格式化完整的请求URL用于日志
func formatRequestURL(method, serverURL, path, query string) string {
	fullURL := serverURL + path
	if query != "" {
		fullURL += "?" + query
	}
	// 移除双斜杠
	fullURL = strings.ReplaceAll(fullURL, "//", "/")
	// 但保留协议的 ://
	fullURL = strings.Replace(fullURL, "http:/", "http://", 1)
	fullURL = strings.Replace(fullURL, "https:/", "https://", 1)
	
	return fmt.Sprintf("%s %s", method, fullURL)
}

func Handler(balancer *balance.Balancer, statsReporter *stats.Reporter) gin.HandlerFunc {
	return func(c *gin.Context) {
		startTime := time.Now()
		statsReporter.IncrementRequestCount()

		server, err := balancer.GetNextServer()
		if err != nil {
			logger.Error("PROXY", "No available servers: %v", err)
			c.JSON(503, gin.H{"error": err.Error()})
			return
		}

		target := server.URL + c.Request.URL.Path
		if c.Request.URL.RawQuery != "" {
			target += "?" + c.Request.URL.RawQuery
		}

		// 移除双斜杠
		target = strings.ReplaceAll(target, "//", "/")
		// 但保留协议的 ://
		target = strings.Replace(target, "http:/", "http://", 1)
		target = strings.Replace(target, "https:/", "https://", 1)

		// 请求日志 - 显示完整URL
		fullRequestURL := formatRequestURL(c.Request.Method, server.URL, c.Request.URL.Path, c.Request.URL.RawQuery)
		logger.Info("PROXY", "%s", fullRequestURL)

		client := &http.Client{
			Timeout: 60 * time.Second,
		}

		req, err := http.NewRequest(c.Request.Method, target, c.Request.Body)
		if err != nil {
			logger.Error("PROXY", "Failed to create request: %v", err)
			c.JSON(500, gin.H{"error": "Failed to create request"})
			return
		}

		for key, values := range c.Request.Header {
			if strings.ToLower(key) == "authorization" {
				req.Header.Set(key, "Bearer "+server.Token)
			} else if strings.ToLower(key) != "host" {
				for _, value := range values {
					req.Header.Add(key, value)
				}
			}
		}

		resp, err := client.Do(req)
		if err != nil {
			logger.Error("PROXY", "Request failed: %s | Error: %v", fullRequestURL, err)
			balancer.MarkServerDown(server.URL)
			statsReporter.IncrementErrorCount()

			// 移除递归调用，直接返回错误
			c.JSON(502, gin.H{"error": "Upstream server failed", "details": err.Error()})
			return
		}
		defer resp.Body.Close()

		// 检查响应状态，如果是5xx错误或429速率限制，标记服务器为不可用
		if resp.StatusCode >= 500 || resp.StatusCode == 429 {
			if resp.StatusCode == 429 {
				logger.Warning("PROXY", "Rate limited: %s | Status: %d", fullRequestURL, resp.StatusCode)
			} else {
				logger.Error("PROXY", "Server error: %s | Status: %d", fullRequestURL, resp.StatusCode)
			}
			balancer.MarkServerDown(server.URL)
			statsReporter.IncrementErrorCount()

			// 不再递归调用，直接返回错误响应
			if resp.StatusCode == 429 {
				c.JSON(429, gin.H{"error": "Rate limit exceeded", "status": resp.StatusCode})
			} else {
				c.JSON(502, gin.H{"error": "Upstream server error", "status": resp.StatusCode})
			}
			return
		}

		// 记录响应时间和统计
		responseTime := time.Since(startTime)
		statsReporter.AddResponseTime(responseTime.Milliseconds())
		statsReporter.AddServerStats(server.URL, responseTime.Milliseconds())

		// 记录响应日志
		if resp.StatusCode == 200 {
			logger.Success("PROXY", "Success: %s | Status: %d (%dms)", fullRequestURL, resp.StatusCode, responseTime.Milliseconds())
		} else if resp.StatusCode < 400 {
			logger.Info("PROXY", "Response: %s | Status: %d (%dms)", fullRequestURL, resp.StatusCode, responseTime.Milliseconds())
		} else {
			logger.Warning("PROXY", "Client error: %s | Status: %d (%dms)", fullRequestURL, resp.StatusCode, responseTime.Milliseconds())
		}

		for key, values := range resp.Header {
			for _, value := range values {
				c.Header(key, value)
			}
		}

		c.Status(resp.StatusCode)

		if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
			c.Header("Content-Type", "text/event-stream")
			c.Header("Cache-Control", "no-cache")
			c.Header("Connection", "keep-alive")

			buffer := make([]byte, 1024)
			for {
				n, err := resp.Body.Read(buffer)
				if n > 0 {
					c.Writer.Write(buffer[:n])
					c.Writer.Flush()
				}
				if err != nil {
					break
				}
			}
		} else {
			c.DataFromReader(resp.StatusCode, resp.ContentLength, resp.Header.Get("Content-Type"), resp.Body, nil)
		}
	}
}