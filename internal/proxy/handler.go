package proxy

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"claude-code-lb/internal/balance"
	"claude-code-lb/internal/logger"
	"claude-code-lb/internal/stats"
	"claude-code-lb/pkg/types"

	"github.com/gin-gonic/gin"
)

// getHopByHopHeaders 返回hop-by-hop头集合，包括Connection头中指定的自定义头
func getHopByHopHeaders(connectionHeader string) map[string]bool {
	hopByHopHeaders := map[string]bool{
		"connection":          true,
		"keep-alive":          true,
		"proxy-authenticate":  true,
		"proxy-authorization": true,
		"te":                  true,
		"trailers":            true,
		"transfer-encoding":   true,
		"upgrade":             true,
	}

	// 解析Connection头中指定的额外hop-by-hop头
	if connectionHeader != "" {
		connectionTokens := strings.Split(connectionHeader, ",")
		for _, token := range connectionTokens {
			token = strings.ToLower(strings.TrimSpace(token))
			if token != "" && token != "close" && token != "keep-alive" {
				hopByHopHeaders[token] = true
			}
		}
	}

	return hopByHopHeaders
}

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

		// 简化重试逻辑：最多尝试3次
		maxRetries := 3
		var lastErr error

		for attempt := 0; attempt < maxRetries; attempt++ {
			// 获取服务器（选择器内部处理负载均衡或fallback逻辑）
			server, err := balancer.GetNextServer()
			if err != nil {
				lastErr = err
				logger.Warning("PROXY", "Attempt %d failed to get server: %v", attempt+1, err)
				continue
			}

			// 尝试请求当前服务器
			if success := tryRequest(c, server, balancer, statsReporter, startTime, attempt); success {
				return // 请求成功，结束
			}

			// 请求失败，记录错误并继续重试
			statsReporter.IncrementErrorCount()
		}

		// 所有重试都失败
		logger.Error("PROXY", "All retries exhausted, last error: %v", lastErr)
		c.JSON(502, gin.H{"error": "All servers failed after retries"})
	}
}

// tryRequest 尝试对指定服务器发起请求
func tryRequest(c *gin.Context, server *types.UpstreamServer, balancer *balance.Balancer, statsReporter *stats.Reporter, startTime time.Time, attempt int) bool {
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
	if attempt > 0 {
		logger.Info("PROXY", "Retry %d: %s", attempt, fullRequestURL)
	} else {
		logger.Info("PROXY", "%s", fullRequestURL)
	}

	client := &http.Client{
		Timeout: 60 * time.Second,
	}

	req, err := http.NewRequest(c.Request.Method, target, c.Request.Body)
	if err != nil {
		logger.Error("PROXY", "Failed to create request: %v", err)
		return false
	}

	// 获取需要过滤的hop-by-hop头 (RFC 2616)
	hopByHopHeaders := getHopByHopHeaders(c.Request.Header.Get("Connection"))
	hopByHopHeaders["host"] = true // 额外添加host头

	for key, values := range c.Request.Header {
		lowerKey := strings.ToLower(key)
		if lowerKey == "authorization" {
			req.Header.Set(key, "Bearer "+server.Token)
		} else if !hopByHopHeaders[lowerKey] {
			for _, value := range values {
				req.Header.Add(key, value)
			}
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		logger.Error("PROXY", "Request failed: %s | Error: %v", fullRequestURL, err)
		balancer.MarkServerDown(server.URL)
		return false
	}
	defer resp.Body.Close()

	// 检查响应状态，如果是5xx错误或429速率限制，标记服务器为不可用
	if resp.StatusCode >= 500 || resp.StatusCode == 429 {
		// 对于服务器错误，读取响应体以获取错误详情（因为不会转发给客户端）
		bodyBytes, bodyErr := io.ReadAll(resp.Body)
		var errorDetail string
		if bodyErr != nil {
			errorDetail = fmt.Sprintf("(failed to read response body: %v)", bodyErr)
		} else {
			errorDetail = strings.TrimSpace(string(bodyBytes))
			// 清理换行符，使日志更紧凑
			errorDetail = strings.ReplaceAll(errorDetail, "\n", " ")
			errorDetail = strings.ReplaceAll(errorDetail, "\r", "")
			// 限制错误详情长度以避免日志过长
			if len(errorDetail) > 500 {
				errorDetail = errorDetail[:500] + "..."
			}
			if errorDetail == "" {
				errorDetail = "(empty response body)"
			}
		}

		if resp.StatusCode == 429 {
			logger.Warning("PROXY", "Rate limited: %s | Status: %d | Response: %s", fullRequestURL, resp.StatusCode, errorDetail)
		} else {
			logger.Error("PROXY", "Server error: %s | Status: %d | Response: %s", fullRequestURL, resp.StatusCode, errorDetail)
		}
		balancer.MarkServerDown(server.URL)
		return false
	}

	// 记录响应时间和统计
	responseTime := time.Since(startTime)
	statsReporter.AddResponseTime(responseTime.Milliseconds())
	statsReporter.AddServerStats(server.URL, responseTime.Milliseconds())

	// 标记服务器为健康（重置失败计数）
	balancer.MarkServerHealthy(server.URL)

	// 记录响应日志
	if resp.StatusCode == 200 {
		logger.Success("PROXY", "Success: %s | Status: %d (%dms)", fullRequestURL, resp.StatusCode, responseTime.Milliseconds())
	} else if resp.StatusCode < 400 {
		logger.Info("PROXY", "Response: %s | Status: %d (%dms)", fullRequestURL, resp.StatusCode, responseTime.Milliseconds())
	} else {
		// 对于客户端错误，只记录状态码（因为响应体会被转发给客户端）
		logger.Warning("PROXY", "Client error: %s | Status: %d (%dms)", fullRequestURL, resp.StatusCode, responseTime.Milliseconds())
	}

	// 复制响应头，但要过滤hop-by-hop头
	responseHopByHopHeaders := getHopByHopHeaders(resp.Header.Get("Connection"))

	for key, values := range resp.Header {
		lowerKey := strings.ToLower(key)
		if !responseHopByHopHeaders[lowerKey] {
			for _, value := range values {
				c.Header(key, value)
			}
		}
	}

	c.Status(resp.StatusCode)

	// 处理流式响应
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

	return true // 请求成功
}
