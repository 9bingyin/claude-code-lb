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

// ProxyHandler 代理处理器，封装所有依赖
type ProxyHandler struct {
	balancer            *balance.Balancer
	statsReporter       *stats.Reporter
	debugMode           bool
	maxRetries          int
	maxRequestBodySize  int64
	httpClient          *http.Client // 默认客户端（HTTP/2）
	http11Client        *http.Client // HTTP/1.1 客户端
	preserveHTTPVersion bool         // 是否保持客户端HTTP协议版本
	streamBufPool       *sync.Pool
}

// NewProxyHandler 创建代理处理器
func NewProxyHandler(balancer *balance.Balancer, statsReporter *stats.Reporter, config types.Config) *ProxyHandler {
	maxBodySize := config.MaxRequestBodySize
	if maxBodySize == 0 {
		maxBodySize = types.DefaultMaxRequestBodySize
	}

	ph := &ProxyHandler{
		balancer:            balancer,
		statsReporter:       statsReporter,
		debugMode:           config.Debug,
		maxRetries:          config.MaxRetries,
		maxRequestBodySize:  maxBodySize,
		httpClient:          createHTTPClient(config.HTTPClient, true),  // HTTP/2 客户端
		preserveHTTPVersion: config.HTTPClient.PreserveHTTPVersion,
		streamBufPool: &sync.Pool{
			New: func() any {
				buf := make([]byte, 4*1024) // 4KB 缓冲区
				return &buf
			},
		},
	}

	// 如果启用了保持HTTP版本功能，创建HTTP/1.1专用客户端
	if config.HTTPClient.PreserveHTTPVersion {
		ph.http11Client = createHTTPClient(config.HTTPClient, false) // HTTP/1.1 客户端
	}

	return ph
}

// createHTTPClient 根据配置创建HTTP客户端
// useHTTP2 参数控制是否使用 HTTP/2 协议
func createHTTPClient(config types.HTTPClientConfig, useHTTP2 bool) *http.Client {
	return &http.Client{
		// 不设置整体超时，让流式响应可以持续任意时长
		// 超时由 Transport 层的各项配置分别控制
		Timeout: 0,
		Transport: &http.Transport{
			// TCP 连接优化
			DialContext: (&net.Dialer{
				Timeout:   time.Duration(config.DialTimeout) * time.Second,
				KeepAlive: time.Duration(config.KeepAlive) * time.Second,
			}).DialContext,
			// 连接池配置
			MaxIdleConns:        config.MaxIdleConns,
			MaxIdleConnsPerHost: config.MaxIdleConnsPerHost,
			MaxConnsPerHost:     0, // 不限制每个 host 的最大连接数
			IdleConnTimeout:     time.Duration(config.IdleConnTimeout) * time.Second,
			// 流式响应优化
			DisableCompression:    true,     // 禁用压缩，避免缓冲延迟
			ForceAttemptHTTP2:     useHTTP2, // 根据参数决定是否使用 HTTP/2
			WriteBufferSize:       config.WriteBufferSize,
			ReadBufferSize:        config.ReadBufferSize,
			ResponseHeaderTimeout: time.Duration(config.ResponseHeaderTimeout) * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			// TLS 优化
			TLSHandshakeTimeout: time.Duration(config.TLSHandshakeTimeout) * time.Second,
		},
	}
}

// Handler 返回 gin.HandlerFunc
func (ph *ProxyHandler) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		startTime := time.Now()
		ph.statsReporter.IncrementRequestCount()

		// 预先读取请求体（支持重试），使用 MaxBytesReader 限制大小
		var requestBody []byte
		var err error
		if c.Request.Body != nil {
			// 使用 MaxBytesReader 限制请求体大小，防止 OOM 攻击
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, ph.maxRequestBodySize)

			requestBody, err = io.ReadAll(c.Request.Body)
			if err != nil {
				// 检查是否是请求体过大错误
				if err.Error() == "http: request body too large" {
					logger.Warning("PROXY", "Request body too large (limit: %d bytes)", ph.maxRequestBodySize)
					c.JSON(http.StatusRequestEntityTooLarge, gin.H{
						"error": gin.H{
							"type":    "request_too_large",
							"message": fmt.Sprintf("Request body exceeds maximum allowed size of %d bytes", ph.maxRequestBodySize),
						},
					})
					return
				}
				logger.Error("PROXY", "Failed to read request body: %v", err)
				c.JSON(502, gin.H{"error": "Failed to read request body"})
				return
			}
			c.Request.Body.Close()
		}

		// 记录已尝试的服务器，避免重复
		triedServers := make(map[string]bool)

		for attempt := 0; attempt <= ph.maxRetries; attempt++ {
			// 获取可用服务器
			server, err := ph.balancer.GetNextServer()
			if err != nil {
				logger.Error("PROXY", "No available servers: %v", err)
				break
			}

			// 跳过已尝试的服务器
			if triedServers[server.URL] {
				continue
			}
			triedServers[server.URL] = true

			// 转发请求到选定的服务器
			success := ph.forwardRequestWithBody(c, server, requestBody, startTime)
			if success {
				return
			}

			// 请求失败，如果还有重试机会，记录日志
			if attempt < ph.maxRetries {
				logger.Warning("PROXY", "Attempt %d failed for %s, retrying with another server...", attempt+1, server.URL)
			}
		}

		ph.statsReporter.IncrementErrorCount()
		c.JSON(502, gin.H{"error": "All servers failed"})
	}
}

// forwardRequestWithBody 转发请求到指定服务器（使用预读取的请求体）
func (ph *ProxyHandler) forwardRequestWithBody(c *gin.Context, server *types.UpstreamServer, requestBody []byte, startTime time.Time) bool {
	target := server.URL + c.Request.URL.Path
	if c.Request.URL.RawQuery != "" {
		target += "?" + c.Request.URL.RawQuery
	}

	// 移除双斜杠
	target = strings.ReplaceAll(target, "//", "/")
	// 但保留协议的 ://
	target = strings.Replace(target, "http:/", "http://", 1)
	target = strings.Replace(target, "https:/", "https://", 1)

	// 请求日志 - 显示完整URL和协议版本
	fullRequestURL := formatRequestURL(c.Request.Method, server.URL, c.Request.URL.Path, c.Request.URL.RawQuery)
	logger.Info("PROXY", "%s [%s]", fullRequestURL, c.Request.Proto)

	// 使用 NewRequestWithContext 传递上下文（修复上下文传递问题）
	req, err := http.NewRequestWithContext(c.Request.Context(), c.Request.Method, target, bytes.NewReader(requestBody))
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
	req.Host = "" // 让 Go 自动从 URL 设置 Host

	// Debug 模式下记录请求详细信息
	if ph.debugMode {
		// 请求概览信息
		requestOverview := fmt.Sprintf("Method: %s\nURL: %s\nProtocol: %s\nContent-Type: %s\nContent-Length: %d bytes\nClient IP: %s",
			c.Request.Method,
			fullRequestURL,
			c.Request.Proto,
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

	// 根据配置和客户端HTTP版本选择对应的HTTP客户端
	client := ph.httpClient
	if ph.preserveHTTPVersion && c.Request.ProtoMajor == 1 && ph.http11Client != nil {
		client = ph.http11Client
		if ph.debugMode {
			logger.Debug("PROXY", "Using HTTP/1.1 client (client protocol: %s)", c.Request.Proto)
		}
	}

	// 发送请求
	resp, err := client.Do(req)
	if err != nil {
		logger.Error("PROXY", "Request failed: %s | Error: %v", fullRequestURL, err)
		ph.balancer.MarkServerDown(server.URL, false)
		return false
	}
	defer resp.Body.Close()

	// Debug 模式下记录响应头
	if ph.debugMode {
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
		if ph.debugMode {
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
	if ph.debugMode && !isStreaming {
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
		ph.balancer.MarkServerDown(server.URL, isStrict)
		return false
	}

	// 记录响应时间和统计
	responseTime := time.Since(startTime)
	ph.statsReporter.AddResponseTime(responseTime.Milliseconds())
	ph.statsReporter.AddServerStats(server.URL, responseTime.Milliseconds())

	// 标记服务器为健康（重置失败计数）
	ph.balancer.MarkServerHealthy(server.URL)

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
		if ph.debugMode {
			writer = &debugChunkWriter{
				Writer: c.Writer,
				logger: logger.DebugMultiline,
			}
		}

		// 从 sync.Pool 获取缓冲区，减少 GC 压力
		bufPtr := ph.streamBufPool.Get().(*[]byte)
		buf := *bufPtr
		defer ph.streamBufPool.Put(bufPtr)

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
			if ph.debugMode {
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

// Handler 向后兼容的 Handler 函数
// Deprecated: 请使用 NewProxyHandler 创建 ProxyHandler 实例
func Handler(balancer *balance.Balancer, statsReporter *stats.Reporter, debugMode bool) gin.HandlerFunc {
	// 创建默认配置
	config := types.Config{
		Debug:      debugMode,
		MaxRetries: 2,
		HTTPClient: types.HTTPClientConfig{
			DialTimeout:           10,
			KeepAlive:             30,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   20,
			IdleConnTimeout:       120,
			ResponseHeaderTimeout: 30,
			TLSHandshakeTimeout:   10,
			WriteBufferSize:       4096,
			ReadBufferSize:        4096,
		},
	}

	ph := NewProxyHandler(balancer, statsReporter, config)
	return ph.Handler()
}
