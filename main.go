package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

type UpstreamServer struct {
	URL       string    `json:"url"`
	Weight    int       `json:"weight"`
	Token     string    `json:"token"`
	DownUntil time.Time `json:"-"` // 不可用直到这个时间
}

type LoadBalancer struct {
	Type    string           `json:"type"` // "round_robin", "weighted_round_robin", "random"
	Servers []UpstreamServer `json:"servers"`
}

type Config struct {
	Port         string       `json:"port"`
	LoadBalancer LoadBalancer `json:"load_balancer"`
	Fallback     bool         `json:"fallback"`
	Auth         struct {
		Enabled     bool     `json:"enabled"`      // 是否启用鉴权
		AllowedKeys []string `json:"allowed_keys"` // 允许的 API Key 列表
	} `json:"auth"`
	CircuitBreaker struct {
		CooldownSeconds int `json:"cooldown_seconds"` // 标记为down后的冷却时间
	} `json:"circuit_breaker"`
}

var config Config
var currentServerIndex int
var serverMutex sync.Mutex
var serverStatus map[string]bool
var statusMutex sync.RWMutex

// 日志和统计相关
var logMutex sync.Mutex
var requestCount int64
var errorCount int64
var totalResponseTime int64
var requestCountByServer map[string]int64
var responseTimeByServer map[string]int64

// 颜色常量
const (
	ColorReset  = "\033[0m"
	ColorRed    = "\033[31m"
	ColorGreen  = "\033[32m"
	ColorYellow = "\033[33m"
	ColorBlue   = "\033[34m"
	ColorPurple = "\033[35m"
	ColorCyan   = "\033[36m"
	ColorGray   = "\033[37m"
	ColorBold   = "\033[1m"
)

func loadConfig() {
	configFile := getEnv("CONFIG_FILE", "config.json")

	if _, err := os.Stat(configFile); err != nil {
		log.Fatalf("Config file %s not found. Please create it based on config.example.json", configFile)
	}

	data, err := os.ReadFile(configFile)
	if err != nil {
		log.Fatalf("Failed to read config file: %v", err)
	}

	if err := json.Unmarshal(data, &config); err != nil {
		log.Fatalf("Failed to parse config file: %v", err)
	}

	if len(config.LoadBalancer.Servers) == 0 {
		log.Fatal("At least one upstream server is required")
	}

	// 初始化服务器状态和统计
	serverStatus = make(map[string]bool)
	requestCountByServer = make(map[string]int64)
	responseTimeByServer = make(map[string]int64)
	for _, server := range config.LoadBalancer.Servers {
		serverStatus[server.URL] = true
		requestCountByServer[server.URL] = 0
		responseTimeByServer[server.URL] = 0
	}

	// 设置默认值
	if config.Port == "" {
		config.Port = "3000"
	}
	if config.LoadBalancer.Type == "" {
		config.LoadBalancer.Type = "round_robin"
	}
	if config.CircuitBreaker.CooldownSeconds == 0 {
		config.CircuitBreaker.CooldownSeconds = 60 // 默认1分钟冷却时间
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// 鉴权中间件
func authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 如果未启用鉴权，直接通过
		if !config.Auth.Enabled {
			c.Next()
			return
		}

		// 检查 Authorization header
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			logAuth(false, "Missing Authorization header from %s", c.ClientIP())
			c.JSON(401, gin.H{"error": "Missing Authorization header"})
			c.Abort()
			return
		}

		// 提取 Bearer token
		const bearerPrefix = "Bearer "
		if !strings.HasPrefix(authHeader, bearerPrefix) {
			logAuth(false, "Invalid header format from %s", c.ClientIP())
			c.JSON(401, gin.H{"error": "Invalid Authorization header format"})
			c.Abort()
			return
		}

		token := authHeader[len(bearerPrefix):]

		// 检查 token 是否在允许的列表中
		isValidToken := false
		for _, allowedKey := range config.Auth.AllowedKeys {
			if token == allowedKey {
				isValidToken = true
				break
			}
		}

		if !isValidToken {
			logAuth(false, "Invalid API key %s...%s from %s",
				token[:min(8, len(token))],
				token[max(0, len(token)-8):],
				c.ClientIP())
			c.JSON(401, gin.H{"error": "Invalid API key"})
			c.Abort()
			return
		}

		logAuth(true, "Valid API key %s...%s from %s",
			token[:min(8, len(token))],
			token[max(0, len(token)-8):],
			c.ClientIP())
		c.Next()
	}
}

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

// 日志相关函数
func formatTimestamp() string {
	return time.Now().Format("15:04:05.000")
}

func logInfo(category string, message string, args ...interface{}) {
	logMutex.Lock()
	defer logMutex.Unlock()
	timestamp := formatTimestamp()
	categoryFormatted := fmt.Sprintf("%s%-8s%s", ColorBlue, category, ColorReset)
	message = fmt.Sprintf(message, args...)
	log.Printf("%s [%s] %s", timestamp, categoryFormatted, message)
}

func logSuccess(category string, message string, args ...interface{}) {
	logMutex.Lock()
	defer logMutex.Unlock()
	timestamp := formatTimestamp()
	categoryFormatted := fmt.Sprintf("%s%-8s%s", ColorGreen, category, ColorReset)
	message = fmt.Sprintf(message, args...)
	log.Printf("%s [%s] %s", timestamp, categoryFormatted, message)
}

func logWarning(category string, message string, args ...interface{}) {
	logMutex.Lock()
	defer logMutex.Unlock()
	timestamp := formatTimestamp()
	categoryFormatted := fmt.Sprintf("%s%-8s%s", ColorYellow, category, ColorReset)
	message = fmt.Sprintf(message, args...)
	log.Printf("%s [%s] %s", timestamp, categoryFormatted, message)
}

func logError(category string, message string, args ...interface{}) {
	logMutex.Lock()
	defer logMutex.Unlock()
	timestamp := formatTimestamp()
	categoryFormatted := fmt.Sprintf("%s%-8s%s", ColorRed, category, ColorReset)
	message = fmt.Sprintf(message, args...)
	log.Printf("%s [%s] %s", timestamp, categoryFormatted, message)
}

func logAuth(success bool, message string, args ...interface{}) {
	if success {
		logSuccess("AUTH", message, args...)
	} else {
		logError("AUTH", message, args...)
	}
}

func logStats() {
	totalRequests := atomic.LoadInt64(&requestCount)
	totalErrors := atomic.LoadInt64(&errorCount)
	avgResponseTime := int64(0)
	if totalRequests > 0 {
		avgResponseTime = atomic.LoadInt64(&totalResponseTime) / totalRequests
	}
	
	logInfo("STATS", "Requests: %d | Errors: %d | Avg time: %dms", 
		totalRequests, totalErrors, avgResponseTime)
}

func getNextServer() (*UpstreamServer, error) {
	serverMutex.Lock()
	defer serverMutex.Unlock()

	availableServers := getAvailableServers()
	if len(availableServers) == 0 {
		if config.Fallback {
			logError("BALANCE", "All %d servers are down or cooling down", len(config.LoadBalancer.Servers))
			return nil, errors.New("all servers are down or cooling down")
		} else {
			return nil, errors.New("no available servers")
		}
	}

	switch config.LoadBalancer.Type {
	case "weighted_round_robin":
		return getWeightedServer(availableServers), nil
	case "random":
		return getRandomServer(availableServers), nil
	default: // round_robin
		return getRoundRobinServer(availableServers), nil
	}
}

func getAvailableServers() []UpstreamServer {
	statusMutex.RLock()
	defer statusMutex.RUnlock()

	var available []UpstreamServer
	var down, cooling int
	now := time.Now()

	for _, server := range config.LoadBalancer.Servers {
		// 检查服务器状态和冷却时间
		isUp := serverStatus[server.URL]
		notCoolingDown := now.After(server.DownUntil)

		if !isUp {
			down++
		}
		if !notCoolingDown {
			cooling++
		}

		if isUp && notCoolingDown {
			available = append(available, server)
		}
	}

	if down > 0 || cooling > 0 {
		logWarning("STATUS", "Servers: %d available, %d down, %d cooling",
			len(available), down, cooling)
	}
	return available
}

func getRoundRobinServer(servers []UpstreamServer) *UpstreamServer {
	if len(servers) == 0 {
		return nil
	}
	server := &servers[currentServerIndex%len(servers)]
	currentServerIndex++
	return server
}

func getWeightedServer(servers []UpstreamServer) *UpstreamServer {
	if len(servers) == 0 {
		return nil
	}

	totalWeight := 0
	for _, server := range servers {
		weight := server.Weight
		if weight <= 0 {
			weight = 1
		}
		totalWeight += weight
	}

	target := currentServerIndex % totalWeight
	currentServerIndex++

	currentWeight := 0
	for _, server := range servers {
		weight := server.Weight
		if weight <= 0 {
			weight = 1
		}
		currentWeight += weight
		if target < currentWeight {
			return &server
		}
	}
	return &servers[0]
}

func getRandomServer(servers []UpstreamServer) *UpstreamServer {
	if len(servers) == 0 {
		return nil
	}
	index := time.Now().UnixNano() % int64(len(servers))
	return &servers[index]
}

func markServerDown(url string) {
	statusMutex.Lock()
	defer statusMutex.Unlock()

	serverStatus[url] = false

	// 设置冷却时间
	cooldownDuration := time.Duration(config.CircuitBreaker.CooldownSeconds) * time.Second
	downUntil := time.Now().Add(cooldownDuration)

	// 更新对应服务器的冷却时间
	for i, server := range config.LoadBalancer.Servers {
		if server.URL == url {
			config.LoadBalancer.Servers[i].DownUntil = downUntil
			break
		}
	}

	atomic.AddInt64(&errorCount, 1)
	logError("CIRCUIT", "Server DOWN: %s (cooldown: %ds)", url, config.CircuitBreaker.CooldownSeconds)
}



// 格式化完整的请求URL用于日志
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

func proxyHandler(c *gin.Context) {
	startTime := time.Now()
	atomic.AddInt64(&requestCount, 1)

	server, err := getNextServer()
	if err != nil {
		logError("PROXY", "No available servers: %v", err)
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
	logInfo("PROXY", "%s", fullRequestURL)

	client := &http.Client{
		Timeout: 60 * time.Second,
	}

	req, err := http.NewRequest(c.Request.Method, target, c.Request.Body)
	if err != nil {
		logError("PROXY", "Failed to create request: %v", err)
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
		logError("PROXY", "Request failed: %s | Error: %v", fullRequestURL, err)
		markServerDown(server.URL)

		// 移除递归调用，直接返回错误
		c.JSON(502, gin.H{"error": "Upstream server failed", "details": err.Error()})
		return
	}
	defer resp.Body.Close()

	// 检查响应状态，如果是5xx错误或429速率限制，标记服务器为不可用
	if resp.StatusCode >= 500 || resp.StatusCode == 429 {
		if resp.StatusCode == 429 {
			logWarning("PROXY", "Rate limited: %s | Status: %d", fullRequestURL, resp.StatusCode)
		} else {
			logError("PROXY", "Server error: %s | Status: %d", fullRequestURL, resp.StatusCode)
		}
		markServerDown(server.URL)

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
	atomic.AddInt64(&totalResponseTime, responseTime.Milliseconds())
	
	// 更新服务器统计（需要加锁操作map）
	logMutex.Lock()
	requestCountByServer[server.URL]++
	responseTimeByServer[server.URL] += responseTime.Milliseconds()
	logMutex.Unlock()

	// 记录响应日志
	if resp.StatusCode == 200 {
		logSuccess("PROXY", "Success: %s | Status: %d (%dms)", fullRequestURL, resp.StatusCode, responseTime.Milliseconds())
	} else if resp.StatusCode < 400 {
		logInfo("PROXY", "Response: %s | Status: %d (%dms)", fullRequestURL, resp.StatusCode, responseTime.Milliseconds())
	} else {
		logWarning("PROXY", "Client error: %s | Status: %d (%dms)", fullRequestURL, resp.StatusCode, responseTime.Milliseconds())
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

func healthHandler(c *gin.Context) {
	availableServers := getAvailableServers()

	// 统计冷却中的服务器
	var coolingDownServers int
	now := time.Now()
	for _, server := range config.LoadBalancer.Servers {
		if !serverStatus[server.URL] && now.Before(server.DownUntil) {
			coolingDownServers++
		}
	}

	c.JSON(200, gin.H{
		"status":            "ok",
		"total_servers":     len(config.LoadBalancer.Servers),
		"available_servers": len(availableServers),
		"cooling_down":      coolingDownServers,
		"load_balancer":     config.LoadBalancer.Type,
		"fallback":          config.Fallback,
		"cooldown_seconds":  config.CircuitBreaker.CooldownSeconds,
		"time":              time.Now().Format(time.RFC3339),
	})
}

// 被动健康检查：定期检查冷却时间到期的服务器，将其标记为可用
func passiveHealthCheck() {
	ticker := time.NewTicker(time.Duration(config.CircuitBreaker.CooldownSeconds) * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		statusMutex.Lock()
		
		for i, server := range config.LoadBalancer.Servers {
			// 检查冷却时间是否已到期
			if !serverStatus[server.URL] && now.After(server.DownUntil) {
				// 冷却时间已到期，重新标记为可用
				serverStatus[server.URL] = true
				config.LoadBalancer.Servers[i].DownUntil = time.Time{}
				
				logSuccess("CIRCUIT", "Server recovered: %s (cooldown expired)", server.URL)
			}
		}
		
		statusMutex.Unlock()
	}
}

// 定期统计显示
func statsReporter() {
	ticker := time.NewTicker(5 * time.Minute) // 每5分钟显示一次统计
	defer ticker.Stop()

	for range ticker.C {
		logStats()
	}
}

// Gin 日志中间件
func ginLoggerMiddleware() gin.HandlerFunc {
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
			logError("GIN", "%s %s | %d | %v | %s", method, path, statusCode, latency, clientIP)
		} else if statusCode >= 400 {
			logWarning("GIN", "%s %s | %d | %v | %s", method, path, statusCode, latency, clientIP)
		} else {
			// 对于健康检查路径，使用更低级别的日志
			if path == "/health" {
				// 健康检查请求不记录日志，避免日志噪音
				return
			}
			logInfo("GIN", "%s %s | %d | %v | %s", method, path, statusCode, latency, clientIP)
		}
	}
}

func main() {
	loadConfig()

	// 设置 Gin 为发布模式，关闭调试日志
	gin.SetMode(gin.ReleaseMode)
	
	// 创建不带默认中间件的 Gin 引擎
	r := gin.New()
	
	// 添加我们自己的日志中间件
	r.Use(ginLoggerMiddleware())
	
	// 添加恢复中间件
	r.Use(gin.Recovery())

	// 设置信任的代理，如果不需要获取真实IP可以设置为空
	r.SetTrustedProxies([]string{})

	r.GET("/health", healthHandler)

	// 在需要鉴权的路由上应用鉴权中间件
	r.Any("/v1/*path", authMiddleware(), proxyHandler)

	// 启动被动健康检查（自动恢复冷却期过期的服务器）
	go passiveHealthCheck()
	
	// 启动统计报告器
	go statsReporter()

	port := config.Port
	if port == "" {
		port = "3000"
	}

	log.Printf("%s==================== Claude Code Proxy ====================%s", ColorBold, ColorReset)
	logInfo("STARTUP", "Starting server on port %s", port)
	logInfo("STARTUP", "Load balancer: %s (%d servers)", config.LoadBalancer.Type, len(config.LoadBalancer.Servers))
	logInfo("STARTUP", "Fallback: %t | Circuit breaker: %ds", config.Fallback, config.CircuitBreaker.CooldownSeconds)
	logInfo("STARTUP", "Health check: passive (auto-recovery after cooldown)")
	logInfo("STARTUP", "Authentication: %t", config.Auth.Enabled)
	if config.Auth.Enabled {
		logInfo("STARTUP", "  Allowed keys: %d", len(config.Auth.AllowedKeys))
	}
	log.Printf("%s==========================================================%s", ColorBold, ColorReset)
	log.Fatal(r.Run(":" + port))
}
