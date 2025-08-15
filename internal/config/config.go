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

	if len(config.LoadBalancer.Servers) == 0 {
		log.Fatal("At least one upstream server is required")
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

	return config
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}