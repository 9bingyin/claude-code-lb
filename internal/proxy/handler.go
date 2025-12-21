package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"claude-code-lb/internal/balance"
	"claude-code-lb/internal/logger"
	"claude-code-lb/internal/stats"
	"claude-code-lb/pkg/types"

	"github.com/gin-gonic/gin"
)

// Hop-by-hop headers. 这些头部只对单次传输有效，不应该被代理转发
// 参考 RFC 7230, section 6.1
var hopHeaders = []string{
	"Connection",
	"Proxy-Connection", // 非标准但常见
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te",
	"Trailer",
	"Transfer-Encoding",
	"Upgrade",
}

// debugChunkWriter 在 debug 模式下记录每个数据块
type debugChunkWriter struct {
	io.Writer
	logger func(tag, title, content string)
}

func (w *debugChunkWriter) Write(p []byte) (n int, err error) {
	// 记录数据块内容
	chunkData := strings.TrimSpace(string(p))
	if chunkData != "" {
		w.logger("PROXY", fmt.Sprintf("Stream Chunk (%d bytes)", len(p)), chunkData)
	}
	return w.Writer.Write(p)
}

// streamBufPool 用于复用流式传输的缓冲区，减少 GC 压力
var streamBufPool = sync.Pool{
	New: func() any {
		buf := make([]byte, 4*1024) // 4KB 缓冲区
		return &buf
	},
}

// sseUsageCollector 轻量级 SSE 收集器，只捕获统计信息相关的事件
// 避免缓存整个响应体，大幅减少内存使用
type sseUsageCollector struct {
	buffer      bytes.Buffer // 当前行缓冲
	model       string
	usage       types.ClaudeUsage
	hasUsage    bool
	debugBuffer *bytes.Buffer // 仅 debug 模式使用
}

func newSSEUsageCollector() *sseUsageCollector {
	return &sseUsageCollector{}
}

func (c *sseUsageCollector) EnableDebug() {
	c.debugBuffer = &bytes.Buffer{}
}

func (c *sseUsageCollector) Write(p []byte) (n int, err error) {
	// 如果启用了 debug 模式，保存完整数据
	if c.debugBuffer != nil {
		c.debugBuffer.Write(p)
	}

	// 将数据添加到行缓冲
	c.buffer.Write(p)

	// 按行处理数据
	for {
		line, err := c.buffer.ReadString('\n')
		if err != nil {
			// 没有完整的行，将未处理的数据放回缓冲
			c.buffer.WriteString(line)
			break
		}

		// 处理 SSE 事件行
		c.processLine(line)
	}

	return len(p), nil
}

func (c *sseUsageCollector) processLine(line string) {
	// 只处理 data: 开头的行
	dataJSON, ok := strings.CutPrefix(line, "data: ")
	if !ok {
		return
	}

	dataJSON = strings.TrimSpace(dataJSON)
	if dataJSON == "" {
		return
	}

	// 快速检查是否包含我们关心的事件类型
	if !strings.Contains(dataJSON, "message_start") && !strings.Contains(dataJSON, "message_delta") {
		return
	}

	var eventData map[string]any
	if err := json.Unmarshal([]byte(dataJSON), &eventData); err != nil {
		return
	}

	eventType, ok := eventData["type"].(string)
	if !ok {
		return
	}

	switch eventType {
	case "message_start":
		if message, ok := eventData["message"].(map[string]any); ok {
			if modelValue, ok := message["model"].(string); ok {
				c.model = modelValue
			}
			if usageData, ok := message["usage"].(map[string]any); ok {
				c.usage = parseUsageFromMap(usageData)
				c.hasUsage = true
			}
		}
	case "message_delta":
		if usageData, ok := eventData["usage"].(map[string]any); ok {
			c.usage = parseUsageFromMap(usageData)
			c.hasUsage = true
		}
	}
}

func (c *sseUsageCollector) GetUsageInfo() (model string, usage types.ClaudeUsage, success bool) {
	return c.model, c.usage, c.hasUsage
}

func (c *sseUsageCollector) GetDebugContent() string {
	if c.debugBuffer != nil {
		return c.debugBuffer.String()
	}
	return ""
}

// httpClient 是一个优化过的 HTTP 客户端，用于反向代理
// 针对流式响应优化，确保最低延迟
var httpClient = &http.Client{
	// 不设置整体超时，让流式响应可以持续任意时长
	// 超时由 Transport 层的各项配置分别控制
	Timeout: 0,
	Transport: &http.Transport{
		// TCP 连接优化
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second, // 连接超时
			KeepAlive: 30 * time.Second, // TCP keep-alive 间隔
		}).DialContext,
		// 连接池配置
		MaxIdleConns:          100,               // 最大空闲连接数
		MaxIdleConnsPerHost:   20,                // 每个 host 的最大空闲连接数
		MaxConnsPerHost:       0,                 // 不限制每个 host 的最大连接数
		IdleConnTimeout:       120 * time.Second, // 空闲连接超时时间
		// 流式响应优化
		DisableCompression:    true,              // 禁用压缩，避免缓冲延迟
		ForceAttemptHTTP2:     true,              // 使用 HTTP/2 多路复用
		WriteBufferSize:       4 * 1024,          // 4KB 写缓冲
		ReadBufferSize:        4 * 1024,          // 4KB 读缓冲
		ResponseHeaderTimeout: 30 * time.Second,  // 等待响应头的超时时间
		ExpectContinueTimeout: 1 * time.Second,   // Expect: 100-continue 超时
		// TLS 优化
		TLSHandshakeTimeout: 10 * time.Second,
	},
}

// removeHopByHopHeaders 移除不应该被代理转发的 hop-by-hop 头部
func removeHopByHopHeaders(h http.Header) {
	// 先移除 Connection 头中指定的所有头部
	for _, f := range h["Connection"] {
		for _, sf := range strings.Split(f, ",") {
			if sf = strings.TrimSpace(sf); sf != "" {
				h.Del(sf)
			}
		}
	}

	// 移除标准的 hop-by-hop 头部
	for _, hh := range hopHeaders {
		h.Del(hh)
	}
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

	// 读取请求体内容用于调试和转发
	var requestBody []byte
	var err error
	if c.Request.Body != nil {
		requestBody, err = io.ReadAll(c.Request.Body)
		if err != nil {
			logger.Error("PROXY", "Failed to read request body: %v", err)
			return false
		}
		c.Request.Body.Close()
	}

	req, err := http.NewRequest(c.Request.Method, target, bytes.NewReader(requestBody))
	if err != nil {
		logger.Error("PROXY", "Failed to create request: %v", err)
		return false
	}

	// 复制请求头（使用标准库的方式）
	req.Header = c.Request.Header.Clone()

	// 移除不应该被代理转发的 hop-by-hop 头部
	removeHopByHopHeaders(req.Header)

	// 设置特定的头部
	req.Header.Set("Authorization", "Bearer "+server.Token)
	req.Host = ""  // 让 Go 自动从 URL 设置 Host

	// Debug 模式下记录请求详细信息
	if debugMode {
		// 请求概览信息
		requestOverview := fmt.Sprintf("Method: %s\nURL: %s\nContent-Type: %s\nContent-Length: %d bytes\nClient IP: %s",
			c.Request.Method,
			fullRequestURL,
			c.Request.Header.Get("Content-Type"),
			len(requestBody),
			c.ClientIP(),
		)
		logger.DebugMultiline("PROXY", "Request Overview", requestOverview)

		// 记录请求头
		var reqHeaders strings.Builder
		for key, values := range req.Header {
			for _, value := range values {
				reqHeaders.WriteString(fmt.Sprintf("%s: %s\n", key, value))
			}
		}
		logger.DebugMultiline("PROXY", "Request Headers", strings.TrimSpace(reqHeaders.String()))

		// 记录请求体
		if len(requestBody) > 0 {
			contentType := c.Request.Header.Get("Content-Type")
			if strings.Contains(contentType, "application/json") {
				// JSON格式化显示
				logger.DebugJSON("PROXY", "Request Body", requestBody)
			} else {
				// 普通文本显示
				logger.DebugMultiline("PROXY", fmt.Sprintf("Request Body (%d bytes)", len(requestBody)), string(requestBody))
			}
		}
	}

	// 发送请求（使用优化过的 HTTP 客户端）
	resp, err := httpClient.Do(req)
	if err != nil {
		logger.Error("PROXY", "Request failed: %s | Error: %v", fullRequestURL, err)
		balancer.MarkServerDown(server.URL, false)
		return false
	}
	defer resp.Body.Close()

	// Debug 模式下记录响应头
	if debugMode {
		// 记录响应头
		var respHeaders strings.Builder
		for key, values := range resp.Header {
			for _, value := range values {
				respHeaders.WriteString(fmt.Sprintf("%s: %s\n", key, value))
			}
		}
		logger.DebugMultiline("PROXY", "Response Headers", strings.TrimSpace(respHeaders.String()))
	}

	// 检查是否为流式响应
	isStreaming := strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream")

	// 流式响应使用专门的 SSE 收集器，非流式响应使用完整 buffer
	var responseBody bytes.Buffer
	var sseCollector *sseUsageCollector
	var responseReader io.Reader

	if isStreaming {
		// 流式响应：使用轻量级 SSE 收集器只捕获统计信息相关的事件
		sseCollector = newSSEUsageCollector()
		if debugMode {
			sseCollector.EnableDebug() // debug 模式下保存完整响应用于日志
		}
		responseReader = io.TeeReader(resp.Body, sseCollector)
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

	// 检查响应状态，如果是5xx错误、429速率限制、400错误或403错误，标记服务器为不可用
	if resp.StatusCode >= 500 || resp.StatusCode == 429 || resp.StatusCode == 400 || resp.StatusCode == 403 {
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
		
		// 400, 403, 429 错误触发严格模式（立即标记失败），500 错误允许宽容（遵循阈值）
		isStrict := resp.StatusCode == 429 || resp.StatusCode == 400 || resp.StatusCode == 403
		balancer.MarkServerDown(server.URL, isStrict)
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

	// 复制响应头（使用标准库辅助函数）
	dst := c.Writer.Header()
	for k, vv := range resp.Header {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}

	// 移除不应该被代理转发的 hop-by-hop 头部
	removeHopByHopHeaders(dst)

	c.Status(resp.StatusCode)

	// 处理响应转发
	if isStreaming {
		// 流式响应处理 - 优化延迟
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
		c.Header("Connection", "keep-alive")
		c.Header("X-Accel-Buffering", "no")       // 禁用 Nginx 缓冲
		c.Header("X-Content-Type-Options", "nosniff")

		// 获取 Flusher 接口用于立即刷新数据
		flusher, ok := c.Writer.(http.Flusher)
		if !ok {
			logger.Error("PROXY", "Response writer does not support flushing")
			return false
		}

		// 创建一个包装的 writer，用于 debug 模式下记录数据块
		var writer io.Writer = c.Writer
		if debugMode {
			writer = &debugChunkWriter{
				Writer: c.Writer,
				logger: logger.DebugMultiline,
			}
		}

		// 从 sync.Pool 获取缓冲区，减少 GC 压力
		bufPtr := streamBufPool.Get().(*[]byte)
		buf := *bufPtr
		defer streamBufPool.Put(bufPtr)

		// 使用 bufio.Reader 包装以优化小块读取
		bufferedReader := bufio.NewReaderSize(responseReader, 4*1024)

		// 流式转发数据
		for {
			n, err := bufferedReader.Read(buf)
			if n > 0 {
				if _, writeErr := writer.Write(buf[:n]); writeErr != nil {
					logger.Error("PROXY", "Failed to write response: %v", writeErr)
					break
				}
				flusher.Flush() // 立即刷新，确保数据及时到达客户端
			}
			if err != nil {
				if err != io.EOF {
					logger.Error("PROXY", "Stream read error: %v", err)
				}
				break
			}
		}

		// 流式响应完成后解析统计信息（使用轻量级收集器）
		if sseCollector != nil {
			// DEBUG 模式下输出完整流式响应体
			if debugMode {
				responseContent := strings.TrimSpace(sseCollector.GetDebugContent())
				if responseContent != "" {
					logger.DebugMultiline("PROXY", fmt.Sprintf("Complete streaming response body (%d bytes)", len(responseContent)), responseContent)
				}
			}

			model, usage, parseSuccess := sseCollector.GetUsageInfo()
			if parseSuccess && model != "" {
				logger.Success("PROXY", "Streaming Success: %s | Status: %d (%dms) | Model: %s | Input: %d | Output: %d | Cache Create: %d | Cache Read: %d",
					fullRequestURL, resp.StatusCode, responseTime.Milliseconds(),
					model, usage.InputTokens, usage.OutputTokens, usage.CacheCreationInputTokens, usage.CacheReadInputTokens)
			}
		}
	} else {
		// 非流式响应：直接写入响应体
		if _, err := c.Writer.Write(responseBody.Bytes()); err != nil {
			logger.Error("PROXY", "Failed to write response: %v", err)
			return false
		}
	}

	return true // 请求成功
}
