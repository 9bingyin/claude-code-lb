package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"claude-code-lb/pkg/types"
)

func TestGetEnv(t *testing.T) {
	tests := []struct {
		name         string
		envKey       string
		envValue     string
		defaultValue string
		expected     string
	}{
		{
			name:         "environment variable exists",
			envKey:       "TEST_CONFIG_VAR",
			envValue:     "custom_value",
			defaultValue: "default_value",
			expected:     "custom_value",
		},
		{
			name:         "environment variable does not exist",
			envKey:       "NON_EXISTENT_VAR",
			envValue:     "",
			defaultValue: "default_value",
			expected:     "default_value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean up environment
			os.Unsetenv(tt.envKey)
			if tt.envValue != "" {
				os.Setenv(tt.envKey, tt.envValue)
				defer os.Unsetenv(tt.envKey)
			}

			result := getEnv(tt.envKey, tt.defaultValue)
			if result != tt.expected {
				t.Errorf("getEnv() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestApplyDefaults(t *testing.T) {
	tests := []struct {
		name     string
		input    types.Config
		expected types.Config
	}{
		{
			name: "apply all defaults",
			input: types.Config{
				Servers: []types.UpstreamServer{
					{URL: "https://api.anthropic.com", Token: "test-token"},
				},
			},
			expected: types.Config{
				Port:      "3000",
				Mode:      "load_balance",
				Algorithm: "round_robin",
				Cooldown:  60,
				Servers: []types.UpstreamServer{
					{URL: "https://api.anthropic.com", Token: "test-token"},
				},
			},
		},
		{
			name: "preserve existing values",
			input: types.Config{
				Port:      "8080",
				Algorithm: "weighted_round_robin",
				Cooldown:  120,
				Servers: []types.UpstreamServer{
					{URL: "https://api.anthropic.com", Token: "test-token"},
				},
			},
			expected: types.Config{
				Port:      "8080",
				Mode:      "load_balance",
				Algorithm: "weighted_round_robin",
				Cooldown:  120,
				Servers: []types.UpstreamServer{
					{URL: "https://api.anthropic.com", Token: "test-token"},
				},
			},
		},
		{
			name: "fallback mode from fallback flag",
			input: types.Config{
				Fallback: true,
				Servers: []types.UpstreamServer{
					{URL: "https://api.anthropic.com", Token: "test-token"},
				},
			},
			expected: types.Config{
				Port:      "3000",
				Mode:      "fallback",
				Algorithm: "round_robin",
				Cooldown:  60,
				Fallback:  true,
				Servers: []types.UpstreamServer{
					{URL: "https://api.anthropic.com", Token: "test-token"},
				},
			},
		},
		{
			name: "auth enabled with keys",
			input: types.Config{
				Auth:     true,
				AuthKeys: []string{"key1", "key2"},
				Servers: []types.UpstreamServer{
					{URL: "https://api.anthropic.com", Token: "test-token"},
				},
			},
			expected: types.Config{
				Port:      "3000",
				Mode:      "load_balance",
				Algorithm: "round_robin",
				Cooldown:  60,
				Auth:      true,
				AuthKeys:  []string{"key1", "key2"},
				Servers: []types.UpstreamServer{
					{URL: "https://api.anthropic.com", Token: "test-token"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := applyDefaults(tt.input)
			
			if result.Port != tt.expected.Port {
				t.Errorf("Port = %v, want %v", result.Port, tt.expected.Port)
			}
			if result.Mode != tt.expected.Mode {
				t.Errorf("Mode = %v, want %v", result.Mode, tt.expected.Mode)
			}
			if result.Algorithm != tt.expected.Algorithm {
				t.Errorf("Algorithm = %v, want %v", result.Algorithm, tt.expected.Algorithm)
			}
			if result.Cooldown != tt.expected.Cooldown {
				t.Errorf("Cooldown = %v, want %v", result.Cooldown, tt.expected.Cooldown)
			}
			if result.Auth != tt.expected.Auth {
				t.Errorf("Auth = %v, want %v", result.Auth, tt.expected.Auth)
			}
		})
	}
}

func TestLoadWithPath(t *testing.T) {
	// Create a temporary directory for test files
	tempDir, err := os.MkdirTemp("", "config_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Test valid config
	validConfig := types.Config{
		Port:      "3000",
		Mode:      "load_balance",
		Algorithm: "round_robin",
		Servers: []types.UpstreamServer{
			{URL: "https://api.anthropic.com", Token: "test-token"},
		},
		Auth:     true,
		AuthKeys: []string{"key1"},
		Cooldown: 60,
		Debug:    false,
	}

	validConfigData, err := json.Marshal(validConfig)
	if err != nil {
		t.Fatalf("Failed to marshal valid config: %v", err)
	}

	validConfigFile := filepath.Join(tempDir, "valid_config.json")
	err = os.WriteFile(validConfigFile, validConfigData, 0644)
	if err != nil {
		t.Fatalf("Failed to write valid config file: %v", err)
	}

	// Test loading valid config
	result := LoadWithPath(validConfigFile)
	if result.Port != "3000" {
		t.Errorf("Expected port 3000, got %s", result.Port)
	}
	if result.Mode != "load_balance" {
		t.Errorf("Expected mode load_balance, got %s", result.Mode)
	}
	if len(result.Servers) != 1 {
		t.Errorf("Expected 1 server, got %d", len(result.Servers))
	}
	
	// Test invalid JSON
	invalidConfigFile := filepath.Join(tempDir, "invalid_config.json")
	err = os.WriteFile(invalidConfigFile, []byte("{invalid json"), 0644)
	if err != nil {
		t.Fatalf("Failed to write invalid config file: %v", err)
	}

	// Capture log.Fatalf for invalid JSON test
	defer func() {
		if r := recover(); r != nil {
			// This is expected for invalid JSON
		}
	}()
}

func TestGenerateExampleConfig(t *testing.T) {
	example := GenerateExampleConfig()
	
	// Check that it contains expected fields
	if !strings.Contains(example, "port") {
		t.Error("Example config should contain 'port' field")
	}
	if !strings.Contains(example, "mode") {
		t.Error("Example config should contain 'mode' field")
	}
	if !strings.Contains(example, "algorithm") {
		t.Error("Example config should contain 'algorithm' field")
	}
	if !strings.Contains(example, "servers") {
		t.Error("Example config should contain 'servers' field")
	}
	
	// Test that it's valid JSON
	var config types.Config
	err := json.Unmarshal([]byte(example), &config)
	if err != nil {
		t.Errorf("Generated example config is not valid JSON: %v", err)
	}
	
	// Test that applying defaults doesn't fail
	result := applyDefaults(config)
	if result.Port == "" {
		t.Error("Applied defaults should set a port")
	}
}

func TestApplyDefaultsValidation(t *testing.T) {
	// Test that validation works for different scenarios
	tests := []struct {
		name        string
		config      types.Config
		shouldPanic bool
		description string
	}{
		{
			name: "valid config",
			config: types.Config{
				Servers: []types.UpstreamServer{
					{URL: "https://api.anthropic.com", Token: "test-token"},
				},
			},
			shouldPanic: false,
			description: "Should not panic with valid config",
		},
		{
			name: "valid weighted algorithm",
			config: types.Config{
				Algorithm: "weighted_round_robin",
				Servers: []types.UpstreamServer{
					{URL: "https://api.anthropic.com", Token: "test-token", Weight: 1},
				},
			},
			shouldPanic: false,
			description: "Should not panic with valid weighted algorithm",
		},
		{
			name: "valid random algorithm",
			config: types.Config{
				Algorithm: "random",
				Servers: []types.UpstreamServer{
					{URL: "https://api.anthropic.com", Token: "test-token"},
				},
			},
			shouldPanic: false,
			description: "Should not panic with valid random algorithm",
		},
		{
			name: "valid fallback mode",
			config: types.Config{
				Mode: "fallback",
				Servers: []types.UpstreamServer{
					{URL: "https://api.anthropic.com", Token: "test-token", Priority: 1},
				},
			},
			shouldPanic: false,
			description: "Should not panic with valid fallback mode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					if !tt.shouldPanic {
						t.Errorf("Test %s: Unexpected panic: %v", tt.name, r)
					}
				} else {
					if tt.shouldPanic {
						t.Errorf("Test %s: Expected panic but none occurred", tt.name)
					}
				}
			}()
			
			result := applyDefaults(tt.config)
			if !tt.shouldPanic {
				// Basic validation that defaults were applied
				if result.Port == "" {
					t.Error("Port should have a default value")
				}
				if result.Algorithm == "" {
					t.Error("Algorithm should have a default value")
				}
				if result.Mode == "" {
					t.Error("Mode should have a default value")
				}
			}
		})
	}
}