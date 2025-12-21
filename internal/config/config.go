package config

import (
	"encoding/json"
	"log"
	"os"

	"claude-code-lb/pkg/types"
)

func Load() types.Config {
	return LoadWithPath("")
}

func LoadWithPath(configPath string) types.Config {
	var configFile string
	if configPath != "" {
		configFile = configPath
	} else {
		configFile = getEnv("CONFIG_FILE", "config.json")
	}

	if _, err := os.Stat(configFile); err != nil {
		log.Fatalf("Config file %s not found. Please create it based on config.example.json", configFile)
	}

	data, err := os.ReadFile(configFile)
	if err != nil {
		log.Fatalf("Failed to read config file: %v", err)
	}

	var config types.Config
	if err := json.Unmarshal(data, &config); err != nil {
		log.Fatalf("Failed to parse config file: %v", err)
	}

	log.Printf("Loading configuration format")
	config = applyDefaults(config)

	// 设置日志 debug 模式
	if config.Debug {
		log.Printf("Debug mode enabled")
	}

	return config
}

// applyHTTPClientDefaults 应用HTTP客户端默认值
func applyHTTPClientDefaults(config *types.Config) {
	hc := &config.HTTPClient
	if hc.DialTimeout == 0 {
		hc.DialTimeout = 10
	}
	if hc.KeepAlive == 0 {
		hc.KeepAlive = 30
	}
	if hc.MaxIdleConns == 0 {
		hc.MaxIdleConns = 100
	}
	if hc.MaxIdleConnsPerHost == 0 {
		hc.MaxIdleConnsPerHost = 20
	}
	if hc.IdleConnTimeout == 0 {
		hc.IdleConnTimeout = 120
	}
	if hc.ResponseHeaderTimeout == 0 {
		hc.ResponseHeaderTimeout = 30
	}
	if hc.TLSHandshakeTimeout == 0 {
		hc.TLSHandshakeTimeout = 10
	}
	if hc.WriteBufferSize == 0 {
		hc.WriteBufferSize = 4096
	}
	if hc.ReadBufferSize == 0 {
		hc.ReadBufferSize = 4096
	}
}

// applyDefaults 应用默认值并验证配置
func applyDefaults(config types.Config) types.Config {
	// 设置默认值
	if config.Port == "" {
		config.Port = "3000"
	}
	if config.Algorithm == "" {
		config.Algorithm = types.AlgorithmRoundRobin
	}
	if config.Cooldown == 0 {
		config.Cooldown = 60 // 默认1分钟冷却时间
	}
	if config.MaxRetries == 0 {
		config.MaxRetries = 2 // 默认重试2次
	}

	// 设置最大请求体大小默认值
	if config.MaxRequestBodySize == 0 {
		config.MaxRequestBodySize = types.DefaultMaxRequestBodySize
	} else if config.MaxRequestBodySize > types.MaxAllowedRequestBodySize {
		log.Printf("WARNING: max_request_body_size (%d) exceeds maximum allowed (%d), using maximum",
			config.MaxRequestBodySize, types.MaxAllowedRequestBodySize)
		config.MaxRequestBodySize = types.MaxAllowedRequestBodySize
	} else if config.MaxRequestBodySize < 0 {
		log.Printf("WARNING: max_request_body_size cannot be negative, using default")
		config.MaxRequestBodySize = types.DefaultMaxRequestBodySize
	}

	// 应用HTTP客户端默认值
	applyHTTPClientDefaults(&config)

	// 处理模式配置（向后兼容）
	if config.Mode == "" {
		if config.Fallback {
			config.Mode = types.ModeFallback
		} else {
			config.Mode = types.ModeLoadBalance
		}
	}

	// 验证配置一致性
	validateConfigConsistency(config)

	// 验证配置
	if len(config.Servers) == 0 {
		log.Fatal("At least one upstream server is required")
	}

	// 验证模式
	isValidMode := false
	for _, mode := range types.ValidModes {
		if config.Mode == mode {
			isValidMode = true
			break
		}
	}
	if !isValidMode {
		log.Fatalf("Invalid mode '%s'. Valid options: %v", config.Mode, types.ValidModes)
	}

	// 验证算法类型
	isValidAlgorithm := false
	for _, algo := range types.ValidAlgorithms {
		if config.Algorithm == algo {
			isValidAlgorithm = true
			break
		}
	}
	if !isValidAlgorithm {
		log.Fatalf("Invalid algorithm '%s'. Valid options: %v", config.Algorithm, types.ValidAlgorithms)
	}

	// 验证服务器配置
	for i, server := range config.Servers {
		if server.URL == "" {
			log.Fatalf("Server %d: URL is required", i+1)
		}
		if server.Token == "" {
			log.Printf("WARNING: Server %d (%s): No token specified", i+1, server.URL)
		}
		if server.Weight <= 0 && config.Algorithm == types.AlgorithmWeightedRoundRobin {
			log.Printf("WARNING: Server %d (%s): Weight should be > 0 for weighted_round_robin", i+1, server.URL)
		}
		// fallback模式下的优先级验证
		if config.Mode == types.ModeFallback && server.Priority == 0 {
			log.Printf("INFO: Server %d (%s): Priority not set, will use weight-based priority", i+1, server.URL)
		}
	}

	// 验证认证配置
	if config.Auth && len(config.AuthKeys) == 0 {
		log.Fatal("Authentication enabled but no auth_keys specified")
	}

	log.Printf("Configuration loaded: mode=%s, algorithm=%s, debug=%t", config.Mode, config.Algorithm, config.Debug)
	return config
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// GenerateExampleConfig 生成简化格式的示例配置
func GenerateExampleConfig() string {
	example := types.Config{
		Port:      "3000",
		Algorithm: types.AlgorithmRoundRobin,
		Servers: []types.UpstreamServer{
			{
				URL:    "https://api.anthropic.com",
				Weight: 5,
				Token:  "sk-your-primary-token-here",
			},
			{
				URL:    "https://api.packycode.com",
				Weight: 3,
				Token:  "sk-your-secondary-token-here",
			},
			{
				URL:    "https://api.example.com",
				Weight: 2,
				Token:  "sk-your-backup-token-here",
			},
		},
		Fallback: true,
		Auth:     true,
		AuthKeys: []string{
			"sk-your-client-api-key-1",
			"sk-your-client-api-key-2",
			"sk-your-client-api-key-3",
		},
		Cooldown: 60,
	}

	data, _ := json.MarshalIndent(example, "", "  ")
	return string(data)
}

// validateConfigConsistency 验证配置字段与模式的一致性
func validateConfigConsistency(config types.Config) {
	switch config.Mode {
	case types.ModeLoadBalance:
		// 负载均衡模式下，priority 字段无效
		hasPriority := false
		for _, server := range config.Servers {
			if server.Priority > 0 {
				hasPriority = true
				break
			}
		}
		if hasPriority {
			log.Printf("WARNING: 'priority' field is ignored in load_balance mode. Use 'weight' for load balancing instead.")
		}

		// 检查算法是否适用
		if config.Algorithm == "" {
			log.Printf("INFO: Using default algorithm 'round_robin' for load_balance mode.")
		}

	case types.ModeFallback:
		// 故障转移模式下，algorithm 字段无效（除非用于权重计算优先级）
		if config.Algorithm != "" && config.Algorithm != types.AlgorithmRoundRobin {
			log.Printf("WARNING: 'algorithm' field (%s) is ignored in fallback mode. Servers are selected by priority order.", config.Algorithm)
		}

		// 检查优先级设置
		hasExplicitPriority := false
		hasWeight := false
		for _, server := range config.Servers {
			if server.Priority > 0 {
				hasExplicitPriority = true
			}
			if server.Weight > 0 {
				hasWeight = true
			}
		}

		if !hasExplicitPriority && hasWeight {
			log.Printf("INFO: No explicit priorities set. Using weight-based priority calculation (higher weight = higher priority).")
		} else if !hasExplicitPriority && !hasWeight {
			log.Printf("INFO: No priorities or weights set. Servers will be prioritized by configuration order.")
		}
	}
}
