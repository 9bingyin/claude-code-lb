package proxy

import (
	"bytes"
	"encoding/json"
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

// parseUsageInfo 解析响应体中的 usage 信息
func parseUsageInfo(responseBody []byte, contentType string) (model string, usage types.ClaudeUsage, success bool) {
	// 只处理 JSON 响应
	if !strings.Contains(strings.ToLower(contentType), "application/json") {
		return "", types.ClaudeUsage{}, false
	}

	var response types.ClaudeResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return "", types.ClaudeUsage{}, false
	}

	return response.Model, response.Usage, true
}

func Handler(balancer *balance.Balancer, statsReporter *stats.Reporter, debugMode bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		startTime := time.Now()
		statsReporter.IncrementRequestCount()

		// 获取可用服务器
		server, err := balancer.GetNextServer()
		if err != nil {
			logger.Error("PROXY", "No available servers: %v", err)
			c.JSON(502, gin.H{"error": "No available servers"})
			return
		}

		// 转发请求到选定的服务器
		success := forwardRequest(c, server, balancer, statsReporter, startTime, debugMode)
		if !success {
			statsReporter.IncrementErrorCount()
			c.JSON(502, gin.H{"error": "Request failed"})
		}
	}
}

// forwardRequest 转发请求到指定服务器
func forwardRequest(c *gin.Context, server *types.UpstreamServer, balancer *balance.Balancer, statsReporter *stats.Reporter, startTime time.Time, debugMode bool) bool {
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

	// Debug 模式下记录请求和响应头
	if debugMode {
		// 记录请求头
		var reqHeaders strings.Builder
		for key, values := range req.Header {
			for _, value := range values {
				reqHeaders.WriteString(fmt.Sprintf("%s: %s\n", key, value))
			}
		}
		logger.Debug("PROXY", "Request headers:\n%s", reqHeaders.String())

		// 记录响应头
		var respHeaders strings.Builder
		for key, values := range resp.Header {
			for _, value := range values {
				respHeaders.WriteString(fmt.Sprintf("%s: %s\n", key, value))
			}
		}
		logger.Debug("PROXY", "Response headers:\n%s", respHeaders.String())
	}

	// 在 debug 模式下读取响应体用于日志记录
	var responseBody []byte
	var responseReader io.Reader = resp.Body

	if debugMode {
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			logger.Error("PROXY", "Failed to read response body for debug: %v", err)
			return false
		}
		responseBody = bodyBytes
		responseReader = bytes.NewReader(bodyBytes)
		
		// 记录完整原始响应
		logger.Debug("PROXY", "Raw response body:\n%s", string(responseBody))
	}

	// 检查响应状态，如果是5xx错误或429速率限制，标记服务器为不可用
	if resp.StatusCode >= 500 || resp.StatusCode == 429 {
		var errorDetail string
		
		if debugMode && responseBody != nil {
			// debug 模式下已经读取了响应体
			errorDetail = strings.TrimSpace(string(responseBody))
		} else {
			// 非 debug 模式下才读取响应体
			bodyBytes, bodyErr := io.ReadAll(responseReader)
			if bodyErr != nil {
				errorDetail = fmt.Sprintf("(failed to read response body: %v)", bodyErr)
			} else {
				errorDetail = strings.TrimSpace(string(bodyBytes))
			}
		}
		
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
		// 尝试解析 usage 信息以增强日志（所有模式下都启用）
		var model string
		var usage types.ClaudeUsage
		var parseSuccess bool
		
		if debugMode && responseBody != nil {
			// debug 模式下直接使用已读取的响应体
			model, usage, parseSuccess = parseUsageInfo(responseBody, resp.Header.Get("Content-Type"))
		} else {
			// 非 debug 模式下需要重新读取响应体来解析
			if contentType := resp.Header.Get("Content-Type"); strings.Contains(strings.ToLower(contentType), "application/json") {
				if bodyBytes, err := io.ReadAll(responseReader); err == nil {
					model, usage, parseSuccess = parseUsageInfo(bodyBytes, contentType)
					// 重新创建 reader 供后续使用
					responseReader = bytes.NewReader(bodyBytes)
				}
			}
		}
		
		if parseSuccess && model != "" {
			logger.Success("PROXY", "Success: %s | Status: %d (%dms) | Model: %s | Input: %d | Output: %d | Cache Create: %d | Cache Read: %d", 
				fullRequestURL, resp.StatusCode, responseTime.Milliseconds(), 
				model, usage.InputTokens, usage.OutputTokens, usage.CacheCreationInputTokens, usage.CacheReadInputTokens)
		} else {
			logger.Success("PROXY", "Success: %s | Status: %d (%dms)", fullRequestURL, resp.StatusCode, responseTime.Milliseconds())
		}
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
			n, err := responseReader.Read(buffer)
			if n > 0 {
				c.Writer.Write(buffer[:n])
				c.Writer.Flush()
			}
			if err != nil {
				break
			}
		}
	} else {
		// 对于非流式响应，检查是否已经读取了响应体
		if (debugMode && responseBody != nil) || (!debugMode && strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "application/json")) {
			// 如果已经读取了响应体（debug模式或非debug模式下的JSON响应），使用 responseReader
			if bodyBytes, err := io.ReadAll(responseReader); err == nil {
				c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), bodyBytes)
			} else {
				// 读取失败，返回错误
				c.JSON(500, gin.H{"error": "Failed to read response body"})
			}
		} else {
			// 使用原始的响应体
			c.DataFromReader(resp.StatusCode, resp.ContentLength, resp.Header.Get("Content-Type"), responseReader, nil)
		}
	}

	return true // 请求成功
}
