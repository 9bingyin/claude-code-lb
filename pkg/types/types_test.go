package types

import (
	"encoding/json"
	"testing"
	"time"
)

func TestUpstreamServer(t *testing.T) {
	tests := []struct {
		name   string
		server UpstreamServer
	}{
		{
			name: "basic server configuration",
			server: UpstreamServer{
				URL:      "http://test-anthropic-api.local",
				Weight:   1,
				Priority: 1,
				Token:    "test-token",
			},
		},
		{
			name: "server with balance check",
			server: UpstreamServer{
				URL:                  "http://test-anthropic-api.local",
				Weight:               2,
				Priority:             1,
				Token:                "test-token",
				BalanceCheck:         "curl -s http://test-anthropic-api.local/v1/balance",
				BalanceCheckInterval: 300,
				BalanceThreshold:     10.0,
			},
		},
		{
			name: "server with downtime",
			server: UpstreamServer{
				URL:       "http://test-anthropic-api.local",
				Weight:    1,
				Priority:  2,
				Token:     "test-token",
				DownUntil: time.Now().Add(time.Hour),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test JSON marshaling
			data, err := json.Marshal(tt.server)
			if err != nil {
				t.Fatalf("Failed to marshal server: %v", err)
			}

			// Test JSON unmarshaling
			var unmarshaled UpstreamServer
			err = json.Unmarshal(data, &unmarshaled)
			if err != nil {
				t.Fatalf("Failed to unmarshal server: %v", err)
			}

			// Verify basic fields (excluding DownUntil as it's not marshaled)
			if unmarshaled.URL != tt.server.URL {
				t.Errorf("URL mismatch: got %s, want %s", unmarshaled.URL, tt.server.URL)
			}
			if unmarshaled.Weight != tt.server.Weight {
				t.Errorf("Weight mismatch: got %d, want %d", unmarshaled.Weight, tt.server.Weight)
			}
			if unmarshaled.Priority != tt.server.Priority {
				t.Errorf("Priority mismatch: got %d, want %d", unmarshaled.Priority, tt.server.Priority)
			}
			if unmarshaled.Token != tt.server.Token {
				t.Errorf("Token mismatch: got %s, want %s", unmarshaled.Token, tt.server.Token)
			}
		})
	}
}

func TestConfig(t *testing.T) {
	tests := []struct {
		name   string
		config Config
	}{
		{
			name: "basic load balance config",
			config: Config{
				Port:      "3000",
				Mode:      "load_balance",
				Algorithm: "round_robin",
				Servers: []UpstreamServer{
					{
						URL:      "https://api1.anthropic.com",
						Weight:   1,
						Priority: 1,
						Token:    "token1",
					},
					{
						URL:      "https://api2.anthropic.com",
						Weight:   2,
						Priority: 2,
						Token:    "token2",
					},
				},
				Auth:     true,
				AuthKeys: []string{"key1", "key2"},
				Cooldown: 60,
				Debug:    true,
			},
		},
		{
			name: "fallback config",
			config: Config{
				Port:      "8080",
				Mode:      "fallback",
				Algorithm: "weighted_round_robin",
				Servers: []UpstreamServer{
					{
						URL:      "https://primary.anthropic.com",
						Weight:   3,
						Priority: 1,
						Token:    "primary-token",
					},
				},
				Auth:     false,
				Cooldown: 120,
				Debug:    false,
			},
		},
		{
			name: "backward compatibility with fallback flag",
			config: Config{
				Port:      "3000",
				Algorithm: "random",
				Servers: []UpstreamServer{
					{URL: "http://test-anthropic-api.local", Token: "token"},
				},
				Fallback: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test JSON marshaling
			data, err := json.Marshal(tt.config)
			if err != nil {
				t.Fatalf("Failed to marshal config: %v", err)
			}

			// Test JSON unmarshaling
			var unmarshaled Config
			err = json.Unmarshal(data, &unmarshaled)
			if err != nil {
				t.Fatalf("Failed to unmarshal config: %v", err)
			}

			// Verify fields
			if unmarshaled.Port != tt.config.Port {
				t.Errorf("Port mismatch: got %s, want %s", unmarshaled.Port, tt.config.Port)
			}
			if unmarshaled.Mode != tt.config.Mode {
				t.Errorf("Mode mismatch: got %s, want %s", unmarshaled.Mode, tt.config.Mode)
			}
			if unmarshaled.Algorithm != tt.config.Algorithm {
				t.Errorf("Algorithm mismatch: got %s, want %s", unmarshaled.Algorithm, tt.config.Algorithm)
			}
			if len(unmarshaled.Servers) != len(tt.config.Servers) {
				t.Errorf("Servers length mismatch: got %d, want %d", len(unmarshaled.Servers), len(tt.config.Servers))
			}
			if unmarshaled.Auth != tt.config.Auth {
				t.Errorf("Auth mismatch: got %t, want %t", unmarshaled.Auth, tt.config.Auth)
			}
			if unmarshaled.Cooldown != tt.config.Cooldown {
				t.Errorf("Cooldown mismatch: got %d, want %d", unmarshaled.Cooldown, tt.config.Cooldown)
			}
		})
	}
}

func TestClaudeUsage(t *testing.T) {
	usage := ClaudeUsage{
		InputTokens:              100,
		OutputTokens:             50,
		CacheCreationInputTokens: 10,
		CacheReadInputTokens:     5,
	}

	// Test JSON marshaling
	data, err := json.Marshal(usage)
	if err != nil {
		t.Fatalf("Failed to marshal usage: %v", err)
	}

	// Test JSON unmarshaling
	var unmarshaled ClaudeUsage
	err = json.Unmarshal(data, &unmarshaled)
	if err != nil {
		t.Fatalf("Failed to unmarshal usage: %v", err)
	}

	// Verify all fields
	if unmarshaled.InputTokens != usage.InputTokens {
		t.Errorf("InputTokens mismatch: got %d, want %d", unmarshaled.InputTokens, usage.InputTokens)
	}
	if unmarshaled.OutputTokens != usage.OutputTokens {
		t.Errorf("OutputTokens mismatch: got %d, want %d", unmarshaled.OutputTokens, usage.OutputTokens)
	}
	if unmarshaled.CacheCreationInputTokens != usage.CacheCreationInputTokens {
		t.Errorf("CacheCreationInputTokens mismatch: got %d, want %d", unmarshaled.CacheCreationInputTokens, usage.CacheCreationInputTokens)
	}
	if unmarshaled.CacheReadInputTokens != usage.CacheReadInputTokens {
		t.Errorf("CacheReadInputTokens mismatch: got %d, want %d", unmarshaled.CacheReadInputTokens, usage.CacheReadInputTokens)
	}
}

func TestClaudeResponse(t *testing.T) {
	response := ClaudeResponse{
		Model: "claude-3-sonnet-20240229",
		Usage: ClaudeUsage{
			InputTokens:              100,
			OutputTokens:             50,
			CacheCreationInputTokens: 10,
			CacheReadInputTokens:     5,
		},
	}

	// Test JSON marshaling
	data, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("Failed to marshal response: %v", err)
	}

	// Test JSON unmarshaling
	var unmarshaled ClaudeResponse
	err = json.Unmarshal(data, &unmarshaled)
	if err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	// Verify fields
	if unmarshaled.Model != response.Model {
		t.Errorf("Model mismatch: got %s, want %s", unmarshaled.Model, response.Model)
	}
	if unmarshaled.Usage.InputTokens != response.Usage.InputTokens {
		t.Errorf("Usage.InputTokens mismatch: got %d, want %d", unmarshaled.Usage.InputTokens, response.Usage.InputTokens)
	}
	if unmarshaled.Usage.OutputTokens != response.Usage.OutputTokens {
		t.Errorf("Usage.OutputTokens mismatch: got %d, want %d", unmarshaled.Usage.OutputTokens, response.Usage.OutputTokens)
	}
}
