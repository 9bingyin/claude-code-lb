package selector

import (
	"fmt"
	"testing"

	"claude-code-lb/internal/testutil"
	"claude-code-lb/pkg/types"
)

func TestCreateSelector(t *testing.T) {
	tests := []struct {
		name         string
		config       types.Config
		expectedType string
		shouldError  bool
	}{
		{
			name: "load_balance mode",
			config: types.Config{
				Mode:      "load_balance",
				Algorithm: "round_robin",
				Servers: []types.UpstreamServer{
					{URL: "http://test-api.local", Token: testutil.TestToken1},
				},
			},
			expectedType: "*selector.LoadBalancer",
			shouldError:  false,
		},
		{
			name: "fallback mode",
			config: types.Config{
				Mode: "fallback",
				Servers: []types.UpstreamServer{
					{URL: "http://test-api.local", Token: testutil.TestToken1, Priority: 1},
					{URL: "http://test-api.local", Token: "token2", Priority: 2},
				},
			},
			expectedType: "*selector.FallbackSelector",
			shouldError:  false,
		},
		{
			name: "legacy fallback mode",
			config: types.Config{
				Fallback: true,
				Servers: []types.UpstreamServer{
					{URL: "http://test-api.local", Token: testutil.TestToken1},
				},
			},
			expectedType: "*selector.FallbackSelector",
			shouldError:  false,
		},
		{
			name: "default mode (load_balance)",
			config: types.Config{
				Algorithm: "weighted_round_robin",
				Servers: []types.UpstreamServer{
					{URL: "http://test-api.local", Token: testutil.TestToken1, Weight: 1},
				},
			},
			expectedType: "*selector.LoadBalancer",
			shouldError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			selector, err := CreateSelector(tt.config)

			if tt.shouldError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if selector == nil {
				t.Error("Expected selector but got nil")
				return
			}

			// Check that the selector implements the interface and has correct type
			actualType := fmt.Sprintf("%T", selector)
			if actualType != tt.expectedType {
				t.Errorf("Expected type %s, got %s", tt.expectedType, actualType)
			}

			// Verify it implements ServerSelector interface
			var _ ServerSelector = selector
		})
	}
}

func TestGetSelectorType(t *testing.T) {
	tests := []struct {
		name     string
		config   types.Config
		expected string
	}{
		{
			name: "load_balance with round_robin",
			config: types.Config{
				Mode:      "load_balance",
				Algorithm: "round_robin",
			},
			expected: "Load Balancer (round_robin)",
		},
		{
			name: "load_balance with weighted_round_robin",
			config: types.Config{
				Mode:      "load_balance",
				Algorithm: "weighted_round_robin",
			},
			expected: "Load Balancer (weighted_round_robin)",
		},
		{
			name: "load_balance with random",
			config: types.Config{
				Mode:      "load_balance",
				Algorithm: "random",
			},
			expected: "Load Balancer (random)",
		},
		{
			name: "fallback mode",
			config: types.Config{
				Mode: "fallback",
			},
			expected: "Fallback Selector",
		},
		{
			name: "legacy fallback mode",
			config: types.Config{
				Fallback: true,
			},
			expected: "Fallback Selector (Legacy)",
		},
		{
			name: "legacy load balance mode",
			config: types.Config{
				Algorithm: "round_robin",
			},
			expected: "Load Balancer (round_robin, Legacy)",
		},
		{
			name: "unknown mode defaults to load balance",
			config: types.Config{
				Mode:      "unknown",
				Algorithm: "round_robin",
			},
			expected: "Load Balancer (round_robin, Legacy)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetSelectorType(tt.config)
			if result != tt.expected {
				t.Errorf("GetSelectorType() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestLoadBalancerImplementsInterface(t *testing.T) {
	config := types.Config{
		Mode:      "load_balance",
		Algorithm: "round_robin",
		Servers: []types.UpstreamServer{
			{URL: "http://test-api.local", Token: "token"},
		},
	}

	lb := NewLoadBalancer(config)

	// Test that it implements all interface methods
	var _ ServerSelector = lb

	// Test individual method calls don't panic
	_, err := lb.SelectServer()
	if err != nil {
		t.Logf("SelectServer returned error: %v (expected for minimal config)", err)
	}

	// Test other methods
	lb.MarkServerDown("http://test-api.local")
	lb.MarkServerHealthy("http://test-api.local")

	servers := lb.GetAvailableServers()
	if servers == nil {
		t.Error("GetAvailableServers should return a slice")
	}

	status := lb.GetServerStatus()
	if status == nil {
		t.Error("GetServerStatus should return a map")
	}

	lb.RecoverServer("http://test-api.local")
}

func TestFallbackSelectorImplementsInterface(t *testing.T) {
	config := types.Config{
		Mode: "fallback",
		Servers: []types.UpstreamServer{
			{URL: "http://test-api.local", Token: testutil.TestToken1, Priority: 1},
			{URL: "http://test-api.local", Token: "token2", Priority: 2},
		},
	}

	fs := NewFallbackSelector(config)

	// Test that it implements all interface methods
	var _ ServerSelector = fs

	// Test individual method calls don't panic
	_, err := fs.SelectServer()
	if err != nil {
		t.Logf("SelectServer returned error: %v (expected for minimal config)", err)
	}

	// Test other methods
	fs.MarkServerDown("http://test-api.local")
	fs.MarkServerHealthy("http://test-api.local")

	servers := fs.GetAvailableServers()
	if servers == nil {
		t.Error("GetAvailableServers should return a slice")
	}

	status := fs.GetServerStatus()
	if status == nil {
		t.Error("GetServerStatus should return a map")
	}

	fs.RecoverServer("http://test-api.local")
}
