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

// applyDefaults 应用默认值并验证配置
func applyDefaults(config types.Config) types.Config {
	// 设置默认值
	if config.Port == "" {
		config.Port = "3000"
	}
	if config.Algorithm == "" {
		config.Algorithm = "round_robin"
	}
	if config.Cooldown == 0 {
		config.Cooldown = 60 // 默认1分钟冷却时间
	}

	// 处理模式配置（向后兼容）
	if config.Mode == "" {
		if config.Fallback {
			config.Mode = "fallback"
		} else {
			config.Mode = "load_balance"
		}
	}

	// 验证配置一致性
	validateConfigConsistency(config)

	// 验证配置
	if len(config.Servers) == 0 {
		log.Fatal("At least one upstream server is required")
	}

	// 验证模式
	validModes := []string{"load_balance", "fallback"}
	isValidMode := false
	for _, mode := range validModes {
		if config.Mode == mode {
			isValidMode = true
			break
		}
	}
	if !isValidMode {
		log.Fatalf("Invalid mode '%s'. Valid options: %v", config.Mode, validModes)
	}

	// 验证算法类型
	validAlgorithms := []string{"round_robin", "weighted_round_robin", "random"}
	isValidAlgorithm := false
	for _, algo := range validAlgorithms {
		if config.Algorithm == algo {
			isValidAlgorithm = true
			break
		}
	}
	if !isValidAlgorithm {
		log.Fatalf("Invalid algorithm '%s'. Valid options: %v", config.Algorithm, validAlgorithms)
	}

	// 验证服务器配置
	for i, server := range config.Servers {
		if server.URL == "" {
			log.Fatalf("Server %d: URL is required", i+1)
		}
		if server.Token == "" {
			log.Printf("WARNING: Server %d (%s): No token specified", i+1, server.URL)
		}
		if server.Weight <= 0 && config.Algorithm == "weighted_round_robin" {
			log.Printf("WARNING: Server %d (%s): Weight should be > 0 for weighted_round_robin", i+1, server.URL)
		}
		// fallback模式下的优先级验证
		if config.Mode == "fallback" && server.Priority == 0 {
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
		Algorithm: "round_robin",
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
	case "load_balance":
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

	case "fallback":
		// 故障转移模式下，algorithm 字段无效（除非用于权重计算优先级）
		if config.Algorithm != "" && config.Algorithm != "round_robin" {
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
