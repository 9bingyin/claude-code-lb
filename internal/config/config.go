package config

import (
	"encoding/json"
	"log"
	"os"

	"claude-code-lb/pkg/types"
)

func Load() types.Config {
	configFile := getEnv("CONFIG_FILE", "config.json")

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
	return applyDefaults(config)
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

	// 验证配置
	if len(config.Servers) == 0 {
		log.Fatal("At least one upstream server is required")
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
			log.Printf("⚠️  Server %d (%s): No token specified", i+1, server.URL)
		}
		if server.Weight <= 0 && config.Algorithm == "weighted_round_robin" {
			log.Printf("⚠️  Server %d (%s): Weight should be > 0 for weighted_round_robin", i+1, server.URL)
		}
	}

	// 验证认证配置
	if config.Auth && len(config.AuthKeys) == 0 {
		log.Fatal("Authentication enabled but no auth_keys specified")
	}

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