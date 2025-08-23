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
	contentTypeLower := strings.ToLower(contentType)

	// 处理 JSON 格式响应
	if strings.Contains(contentTypeLower, "application/json") {
		var response types.ClaudeResponse
		if err := json.Unmarshal(responseBody, &response); err != nil {
			return "", types.ClaudeUsage{}, false
		}
		return response.Model, response.Usage, true
	}

	// 处理 Server-Sent Events (SSE) 格式响应
	if strings.Contains(contentTypeLower, "text/event-stream") {
		return parseSSEUsageInfo(responseBody)
	}

	return "", types.ClaudeUsage{}, false
}

// parseSSEUsageInfo 解析 SSE 格式响应中的 usage 信息
func parseSSEUsageInfo(responseBody []byte) (model string, usage types.ClaudeUsage, success bool) {
	lines := strings.Split(string(responseBody), "\n")

	for _, line := range lines {
		// 查找 message_start 或 message_delta 事件
		if dataJSON, ok := strings.CutPrefix(line, "data: "); ok {
			dataJSON = strings.TrimSpace(dataJSON)

			// 跳过空行和 ping 消息
			if dataJSON == "" || strings.Contains(dataJSON, `"type": "ping"`) {
				continue
			}

			var eventData map[string]any
			if err := json.Unmarshal([]byte(dataJSON), &eventData); err != nil {
				continue
			}

			// 检查是否是 message_start 或 message_delta 事件
			eventType, ok := eventData["type"].(string)
			if !ok {
				continue
			}

			switch eventType {
			case "message_start":
				if message, ok := eventData["message"].(map[string]any); ok {
					if modelValue, ok := message["model"].(string); ok {
						model = modelValue
					}
					if usageData, ok := message["usage"].(map[string]any); ok {
						usage = parseUsageFromMap(usageData)
						success = true
					}
				}
			case "message_delta":
				if usageData, ok := eventData["usage"].(map[string]any); ok {
					// message_delta 中的 usage 是最终数据，覆盖之前的值
					usage = parseUsageFromMap(usageData)
					success = true
				}
			}
		}
	}

	return model, usage, success
}

// parseUsageFromMap 从 map 中解析 usage 信息
func parseUsageFromMap(usageData map[string]any) types.ClaudeUsage {
	var usage types.ClaudeUsage

	if inputTokens, ok := usageData["input_tokens"].(float64); ok {
		usage.InputTokens = int(inputTokens)
	}
	if outputTokens, ok := usageData["output_tokens"].(float64); ok {
		usage.OutputTokens = int(outputTokens)
	}
	if cacheCreateTokens, ok := usageData["cache_creation_input_tokens"].(float64); ok {
		usage.CacheCreationInputTokens = int(cacheCreateTokens)
	}
	if cacheReadTokens, ok := usageData["cache_read_input_tokens"].(float64); ok {
		usage.CacheReadInputTokens = int(cacheReadTokens)
	}

	return usage
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
		logger.DebugMultiline("PROXY", "Request Headers", strings.TrimSpace(reqHeaders.String()))

		// 记录响应头
		var respHeaders strings.Builder
		for key, values := range resp.Header {
			for _, value := range values {
				respHeaders.WriteString(fmt.Sprintf("%s: %s\n", key, value))
			}
		}
		logger.DebugMultiline("PROXY", "Response Headers", strings.TrimSpace(respHeaders.String()))
	}

	// 使用 TeeReader 同时进行统计和流式传输
	var responseBody bytes.Buffer
	var responseReader io.Reader

	// 检查是否为流式响应
	isStreaming := strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream")

	if isStreaming {
		// 流式响应：使用 TeeReader 同时收集数据和传输
		responseReader = io.TeeReader(resp.Body, &responseBody)
	} else {
		// 非流式响应：先读取完整响应体
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			logger.Error("PROXY", "Failed to read response body: %v", err)
			return false
		}
		responseBody.Write(bodyBytes)
		responseReader = bytes.NewReader(bodyBytes)
	}

	// Debug 模式下记录完整原始响应（仅限非流式响应）
	if debugMode && !isStreaming {
		responseContent := responseBody.String()
		if responseContent != "" {
			// 检查是否为JSON格式并尝试格式化
			if strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
				if jsonBytes := responseBody.Bytes(); len(jsonBytes) > 0 {
					logger.DebugJSON("PROXY", "Response Body", jsonBytes)
				}
			} else {
				logger.DebugMultiline("PROXY", fmt.Sprintf("Response Body (%d bytes)", responseBody.Len()), responseContent)
			}
		}
	}

	// 检查响应状态，如果是5xx错误或429速率限制，标记服务器为不可用
	if resp.StatusCode >= 500 || resp.StatusCode == 429 {
		// 对于非流式响应，使用已读取的响应体
		var errorDetail string
		if !isStreaming {
			errorDetail = strings.TrimSpace(responseBody.String())
		} else {
			errorDetail = "streaming response error"
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
		// 对于非流式响应，直接解析统计信息
		var model string
		var usage types.ClaudeUsage
		var parseSuccess bool

		if !isStreaming {
			model, usage, parseSuccess = parseUsageInfo(responseBody.Bytes(), resp.Header.Get("Content-Type"))
		} else {
			// 流式响应的统计会在后续处理
			parseSuccess = true
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

	// 处理响应转发
	if isStreaming {
		// 流式响应处理
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")

		// 流式转发数据，同时收集统计信息
		buffer := make([]byte, 1024)
		for {
			n, err := responseReader.Read(buffer)
			if n > 0 {
				// DEBUG 模式下记录每个数据块
				if debugMode {
					chunkData := strings.TrimSpace(string(buffer[:n]))
					if chunkData != "" {
						logger.DebugMultiline("PROXY", fmt.Sprintf("Stream Chunk (%d bytes)", n), chunkData)
					}
				}
				c.Writer.Write(buffer[:n])
				c.Writer.Flush()
			}
			if err != nil {
				break
			}
		}

		// 流式响应完成后解析统计信息
		if responseBody.Len() > 0 {
			// DEBUG 模式下输出完整流式响应体
			if debugMode {
				responseContent := strings.TrimSpace(responseBody.String())
				if responseContent != "" {
					logger.DebugMultiline("PROXY", fmt.Sprintf("Complete streaming response body (%d bytes)", responseBody.Len()), responseContent)
				}
			}

			model, usage, parseSuccess := parseUsageInfo(responseBody.Bytes(), resp.Header.Get("Content-Type"))
			if parseSuccess && model != "" {
				logger.Success("PROXY", "Streaming Success: %s | Status: %d (%dms) | Model: %s | Input: %d | Output: %d | Cache Create: %d | Cache Read: %d",
					fullRequestURL, resp.StatusCode, responseTime.Milliseconds(),
					model, usage.InputTokens, usage.OutputTokens, usage.CacheCreationInputTokens, usage.CacheReadInputTokens)
			}
		}
	} else {
		// 对于非流式响应，使用已读取的响应体
		c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), responseBody.Bytes())
	}

	return true // 请求成功
}
