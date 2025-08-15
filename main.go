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
	Type    string           `json:"type"`     // "round_robin", "weighted_round_robin", "random"
	Servers []UpstreamServer `json:"servers"`
}

type Config struct {
	Port         string        `json:"port"`
	LoadBalancer LoadBalancer `json:"load_balancer"`
	Fallback     bool          `json:"fallback"`
	CircuitBreaker struct {
		CooldownSeconds int `json:"cooldown_seconds"` // 标记为down后的冷却时间
	} `json:"circuit_breaker"`
	HealthCheck  struct {
		Enabled  bool `json:"enabled"`
		Interval int  `json:"interval"` // seconds
		Timeout  int  `json:"timeout"`  // seconds
	} `json:"health_check"`
}

var config Config
var currentServerIndex int
var serverMutex sync.Mutex
var serverStatus map[string]bool
var statusMutex sync.RWMutex

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
	
	// 初始化服务器状态
	serverStatus = make(map[string]bool)
	for _, server := range config.LoadBalancer.Servers {
		serverStatus[server.URL] = true
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
	if config.HealthCheck.Interval == 0 {
		config.HealthCheck.Interval = 30
	}
	if config.HealthCheck.Timeout == 0 {
		config.HealthCheck.Timeout = 5
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getNextServer() (*UpstreamServer, error) {
	serverMutex.Lock()
	defer serverMutex.Unlock()

	availableServers := getAvailableServers()
	if len(availableServers) == 0 {
		if config.Fallback {
			// 如果启用fallback，尝试所有服务器
			availableServers = config.LoadBalancer.Servers
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
	now := time.Now()
	
	for _, server := range config.LoadBalancer.Servers {
		// 检查服务器状态和冷却时间
		if serverStatus[server.URL] && now.After(server.DownUntil) {
			available = append(available, server)
		}
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
	
	log.Printf("Marked server as down: %s (cooldown until: %s)", url, downUntil.Format(time.RFC3339))
}

func markServerUp(url string) {
	statusMutex.Lock()
	defer statusMutex.Unlock()
	
	serverStatus[url] = true
	
	// 清除冷却时间
	for i, server := range config.LoadBalancer.Servers {
		if server.URL == url {
			config.LoadBalancer.Servers[i].DownUntil = time.Time{}
			break
		}
	}
	
	log.Printf("Marked server as up: %s", url)
}

func proxyHandler(c *gin.Context) {
	server, err := getNextServer()
	if err != nil {
		c.JSON(503, gin.H{"error": "No available servers"})
		return
	}

	target := server.URL + c.Request.URL.Path
	if c.Request.URL.RawQuery != "" {
		target += "?" + c.Request.URL.RawQuery
	}

	client := &http.Client{
		Timeout: 60 * time.Second,
	}

	req, err := http.NewRequest(c.Request.Method, target, c.Request.Body)
	if err != nil {
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
		log.Printf("Request to %s failed: %v", server.URL, err)
		markServerDown(server.URL)
		
		if config.Fallback {
			// 尝试下一个服务器
			proxyHandler(c)
			return
		}
		
		c.JSON(500, gin.H{"error": "Failed to forward request"})
		return
	}
	defer resp.Body.Close()

	// 检查响应状态，如果是5xx错误，标记服务器为不可用
	if resp.StatusCode >= 500 {
		log.Printf("Server %s returned %d, marking as down", server.URL, resp.StatusCode)
		markServerDown(server.URL)
		
		if config.Fallback {
			// 尝试下一个服务器
			proxyHandler(c)
			return
		}
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
		"status":             "ok",
		"total_servers":      len(config.LoadBalancer.Servers),
		"available_servers":  len(availableServers),
		"cooling_down":       coolingDownServers,
		"load_balancer":      config.LoadBalancer.Type,
		"fallback":           config.Fallback,
		"cooldown_seconds":   config.CircuitBreaker.CooldownSeconds,
		"time":               time.Now().Format(time.RFC3339),
	})
}

func healthCheck() {
	if !config.HealthCheck.Enabled {
		return
	}

	ticker := time.NewTicker(time.Duration(config.HealthCheck.Interval) * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		for _, server := range config.LoadBalancer.Servers {
			go func(serverURL string) {
				client := &http.Client{
					Timeout: time.Duration(config.HealthCheck.Timeout) * time.Second,
				}

				resp, err := client.Get(serverURL + "/health")
				if err != nil || resp.StatusCode != 200 {
					markServerDown(serverURL)
				} else {
					markServerUp(serverURL)
				}
				if resp != nil {
					resp.Body.Close()
				}
			}(server.URL)
		}
	}
}

func main() {
	loadConfig()

	r := gin.Default()
	
	// 设置信任的代理，如果不需要获取真实IP可以设置为空
	r.SetTrustedProxies([]string{})

	r.GET("/health", healthHandler)
	r.Any("/v1/*path", proxyHandler)

	// 启动健康检查
	if config.HealthCheck.Enabled {
		go healthCheck()
	}

	port := config.Port
	if port == "" {
		port = "3000"
	}

	fmt.Printf("Server starting on port %s\n", port)
	fmt.Printf("Load balancer type: %s\n", config.LoadBalancer.Type)
	fmt.Printf("Total servers: %d\n", len(config.LoadBalancer.Servers))
	fmt.Printf("Fallback enabled: %t\n", config.Fallback)
	fmt.Printf("Health check enabled: %t\n", config.HealthCheck.Enabled)
	fmt.Printf("Circuit breaker cooldown: %d seconds\n", config.CircuitBreaker.CooldownSeconds)
	log.Fatal(r.Run(":" + port))
}
