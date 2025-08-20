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
			// Set up environment using t.Setenv for automatic cleanup
			if tt.envValue != "" {
				t.Setenv(tt.envKey, tt.envValue)
			} else {
				// Ensure it's unset for this subtest
				t.Setenv(tt.envKey, "")
				os.Unsetenv(tt.envKey)
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
					{URL: "http://test-anthropic-api.local", Token: "test-token"},
				},
			},
			expected: types.Config{
				Port:      "3000",
				Mode:      "load_balance",
				Algorithm: "round_robin",
				Cooldown:  60,
				Servers: []types.UpstreamServer{
					{URL: "http://test-anthropic-api.local", Token: "test-token"},
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
					{URL: "http://test-anthropic-api.local", Token: "test-token"},
				},
			},
			expected: types.Config{
				Port:      "8080",
				Mode:      "load_balance",
				Algorithm: "weighted_round_robin",
				Cooldown:  120,
				Servers: []types.UpstreamServer{
					{URL: "http://test-anthropic-api.local", Token: "test-token"},
				},
			},
		},
		{
			name: "fallback mode from fallback flag",
			input: types.Config{
				Fallback: true,
				Servers: []types.UpstreamServer{
					{URL: "http://test-anthropic-api.local", Token: "test-token"},
				},
			},
			expected: types.Config{
				Port:      "3000",
				Mode:      "fallback",
				Algorithm: "round_robin",
				Cooldown:  60,
				Fallback:  true,
				Servers: []types.UpstreamServer{
					{URL: "http://test-anthropic-api.local", Token: "test-token"},
				},
			},
		},
		{
			name: "auth enabled with keys",
			input: types.Config{
				Auth:     true,
				AuthKeys: []string{"key1", "key2"},
				Servers: []types.UpstreamServer{
					{URL: "http://test-anthropic-api.local", Token: "test-token"},
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
					{URL: "http://test-anthropic-api.local", Token: "test-token"},
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
			if result.Fallback != tt.expected.Fallback {
				t.Errorf("Fallback = %v, want %v", result.Fallback, tt.expected.Fallback)
			}
			if len(result.Servers) != len(tt.expected.Servers) {
				t.Fatalf("Servers length = %d, want %d", len(result.Servers), len(tt.expected.Servers))
			}
			for i := range result.Servers {
				if result.Servers[i].URL != tt.expected.Servers[i].URL {
					t.Errorf("Servers[%d].URL = %s, want %s", i, result.Servers[i].URL, tt.expected.Servers[i].URL)
				}
				if result.Servers[i].Token != tt.expected.Servers[i].Token {
					t.Errorf("Servers[%d].Token = %s, want %s", i, result.Servers[i].Token, tt.expected.Servers[i].Token)
				}
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
			{URL: "http://test-anthropic-api.local", Token: "test-token"},
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

	// Test invalid JSON - this should cause the program to exit/panic
	// We need to test this in a subprocess to avoid killing the test runner
	// For now, just document that this path exists but can't be easily tested
	// in unit tests due to log.Fatalf behavior

	// Note: LoadWithPath calls log.Fatalf on invalid JSON, which calls os.Exit()
	// This cannot be caught with recover() and would terminate the test process
	// In a real scenario, this would exit the program with an error message
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
	// Test valid configurations (these should not cause log.Fatal)
	validTests := []struct {
		name        string
		config      types.Config
		description string
	}{
		{
			name: "valid config",
			config: types.Config{
				Servers: []types.UpstreamServer{
					{URL: "http://test-anthropic-api.local", Token: "test-token"},
				},
			},
			description: "Should not fail with valid config",
		},
		{
			name: "valid weighted algorithm",
			config: types.Config{
				Algorithm: "weighted_round_robin",
				Servers: []types.UpstreamServer{
					{URL: "http://test-anthropic-api.local", Token: "test-token", Weight: 1},
				},
			},
			description: "Should not fail with valid weighted algorithm",
		},
		{
			name: "valid random algorithm",
			config: types.Config{
				Algorithm: "random",
				Servers: []types.UpstreamServer{
					{URL: "http://test-anthropic-api.local", Token: "test-token"},
				},
			},
			description: "Should not fail with valid random algorithm",
		},
		{
			name: "valid fallback mode",
			config: types.Config{
				Mode: "fallback",
				Servers: []types.UpstreamServer{
					{URL: "http://test-anthropic-api.local", Token: "test-token", Priority: 1},
				},
			},
			description: "Should not fail with valid fallback mode",
		},
		{
			name: "auth enabled with keys",
			config: types.Config{
				Auth:     true,
				AuthKeys: []string{"key1", "key2"},
				Servers: []types.UpstreamServer{
					{URL: "http://test-anthropic-api.local", Token: "test-token"},
				},
			},
			description: "Should not fail with valid auth config",
		},
	}

	for _, tt := range validTests {
		t.Run(tt.name, func(t *testing.T) {
			result := applyDefaults(tt.config)

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

			// Verify input values are preserved
			if tt.config.Auth {
				if result.Auth != tt.config.Auth {
					t.Errorf("Auth flag not preserved: got %t, want %t", result.Auth, tt.config.Auth)
				}
				if len(result.AuthKeys) != len(tt.config.AuthKeys) {
					t.Errorf("AuthKeys not preserved: got %d keys, want %d", len(result.AuthKeys), len(tt.config.AuthKeys))
				}
			}
		})
	}

	// Note: Invalid configurations like no servers, invalid algorithms, etc.
	// cause log.Fatal which calls os.Exit(1) and cannot be tested in unit tests.
	// These would need to be tested with subprocess testing or integration tests.
	// Examples of configurations that would cause log.Fatal:
	// - config.Servers empty
	// - invalid mode (not "load_balance" or "fallback")
	// - invalid algorithm (not in validAlgorithms list)
	// - auth=true but authKeys empty
}
