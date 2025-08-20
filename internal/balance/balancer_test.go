package balance

import (
	"testing"

	"claude-code-lb/pkg/types"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name   string
		config types.Config
	}{
		{
			name: "load balance config",
			config: types.Config{
				Mode:      "load_balance",
				Algorithm: "round_robin",
				Servers: []types.UpstreamServer{
					{URL: "https://api1.example.com", Token: "token1"},
					{URL: "https://api2.example.com", Token: "token2"},
				},
			},
		},
		{
			name: "fallback config",
			config: types.Config{
				Mode: "fallback",
				Servers: []types.UpstreamServer{
					{URL: "https://api1.example.com", Token: "token1", Priority: 1},
					{URL: "https://api2.example.com", Token: "token2", Priority: 2},
				},
			},
		},
		{
			name: "legacy fallback config",
			config: types.Config{
				Fallback: true,
				Servers: []types.UpstreamServer{
					{URL: "https://api1.example.com", Token: "token1"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			balancer := New(tt.config)
			
			if balancer == nil {
				t.Fatal("New returned nil balancer")
			}
			
			if balancer.config.Mode != tt.config.Mode {
				t.Errorf("Expected mode %s, got %s", tt.config.Mode, balancer.config.Mode)
			}
			
			if balancer.selector == nil {
				t.Error("Selector should not be nil")
			}
		})
	}
}

func TestBalancerGetNextServer(t *testing.T) {
	config := types.Config{
		Mode:      "load_balance",
		Algorithm: "round_robin",
		Servers: []types.UpstreamServer{
			{URL: "https://api1.example.com", Token: "token1"},
			{URL: "https://api2.example.com", Token: "token2"},
		},
	}

	balancer := New(config)

	// Test that we can get servers
	server, err := balancer.GetNextServer()
	if err != nil {
		t.Fatalf("GetNextServer failed: %v", err)
	}
	if server == nil {
		t.Fatal("GetNextServer returned nil server")
	}

	// Verify the server is one of our configured servers
	validUrls := []string{"https://api1.example.com", "https://api2.example.com"}
	found := false
	for _, url := range validUrls {
		if server.URL == url {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("GetNextServer returned unexpected server: %s", server.URL)
	}
}

func TestBalancerGetNextServerWithFallback(t *testing.T) {
	config := types.Config{
		Mode: "fallback",
		Servers: []types.UpstreamServer{
			{URL: "https://api1.example.com", Token: "token1", Priority: 1},
			{URL: "https://api2.example.com", Token: "token2", Priority: 2},
		},
	}

	balancer := New(config)

	// Test with fallback enabled
	server, err := balancer.GetNextServerWithFallback(true)
	if err != nil {
		t.Fatalf("GetNextServerWithFallback failed: %v", err)
	}
	if server == nil {
		t.Fatal("GetNextServerWithFallback returned nil server")
	}

	// Should get highest priority server
	if server.URL != "https://api1.example.com" {
		t.Errorf("Expected highest priority server, got %s", server.URL)
	}

	// Test with fallback disabled (should still work the same way in new architecture)
	server2, err := balancer.GetNextServerWithFallback(false)
	if err != nil {
		t.Fatalf("GetNextServerWithFallback (disabled) failed: %v", err)
	}
	if server2 == nil {
		t.Fatal("GetNextServerWithFallback (disabled) returned nil server")
	}
}

func TestBalancerMarkServerDown(t *testing.T) {
	config := types.Config{
		Mode:      "load_balance",
		Algorithm: "round_robin",
		Servers: []types.UpstreamServer{
			{URL: "https://api1.example.com", Token: "token1"},
			{URL: "https://api2.example.com", Token: "token2"},
		},
	}

	balancer := New(config)

	// Mark a server as down
	balancer.MarkServerDown("https://api1.example.com")

	// Check server status
	status := balancer.GetServerStatus()
	if status["https://api1.example.com"] {
		t.Error("Server should be marked as down")
	}
	if !status["https://api2.example.com"] {
		t.Error("Other server should still be available")
	}
}

func TestBalancerMarkServerHealthy(t *testing.T) {
	config := types.Config{
		Mode:      "load_balance",
		Algorithm: "round_robin",
		Servers: []types.UpstreamServer{
			{URL: "https://api1.example.com", Token: "token1"},
		},
	}

	balancer := New(config)

	// Mark server as down first
	balancer.MarkServerDown("https://api1.example.com")

	// Verify it's down
	status := balancer.GetServerStatus()
	if status["https://api1.example.com"] {
		t.Error("Server should be down")
	}

	// Mark server as healthy
	balancer.MarkServerHealthy("https://api1.example.com")

	// Verify it's healthy
	status = balancer.GetServerStatus()
	if !status["https://api1.example.com"] {
		t.Error("Server should be healthy")
	}
}

func TestBalancerGetAvailableServers(t *testing.T) {
	config := types.Config{
		Mode:      "load_balance",
		Algorithm: "round_robin",
		Servers: []types.UpstreamServer{
			{URL: "https://api1.example.com", Token: "token1"},
			{URL: "https://api2.example.com", Token: "token2"},
			{URL: "https://api3.example.com", Token: "token3"},
		},
	}

	balancer := New(config)

	// Initially all servers should be available
	available := balancer.GetAvailableServers()
	if len(available) != 3 {
		t.Errorf("Expected 3 available servers, got %d", len(available))
	}

	// Mark one server as down
	balancer.MarkServerDown("https://api2.example.com")

	// Should have 2 available servers
	available = balancer.GetAvailableServers()
	if len(available) != 2 {
		t.Errorf("Expected 2 available servers after marking one down, got %d", len(available))
	}

	// Verify the down server is not in the list
	for _, server := range available {
		if server.URL == "https://api2.example.com" {
			t.Error("Down server should not be in available servers list")
		}
	}
}

func TestBalancerGetServerStatus(t *testing.T) {
	config := types.Config{
		Mode:      "load_balance",
		Algorithm: "round_robin",
		Servers: []types.UpstreamServer{
			{URL: "https://api1.example.com", Token: "token1"},
			{URL: "https://api2.example.com", Token: "token2"},
		},
	}

	balancer := New(config)

	// Get initial status
	status := balancer.GetServerStatus()
	if len(status) != 2 {
		t.Errorf("Expected status for 2 servers, got %d", len(status))
	}

	// All servers should initially be available
	for url, available := range status {
		if !available {
			t.Errorf("Server %s should initially be available", url)
		}
	}

	// Mark a server as down and check status
	balancer.MarkServerDown("https://api1.example.com")
	status = balancer.GetServerStatus()

	if status["https://api1.example.com"] {
		t.Error("Marked down server should show as unavailable")
	}
	if !status["https://api2.example.com"] {
		t.Error("Other server should still be available")
	}
}

func TestBalancerRecoverServer(t *testing.T) {
	config := types.Config{
		Mode:      "load_balance",
		Algorithm: "round_robin",
		Servers: []types.UpstreamServer{
			{URL: "https://api1.example.com", Token: "token1"},
		},
	}

	balancer := New(config)

	// Mark server as down
	balancer.MarkServerDown("https://api1.example.com")

	// Verify it's down
	status := balancer.GetServerStatus()
	if status["https://api1.example.com"] {
		t.Error("Server should be down")
	}

	// Recover the server
	balancer.RecoverServer("https://api1.example.com")

	// Verify it's recovered
	status = balancer.GetServerStatus()
	if !status["https://api1.example.com"] {
		t.Error("Server should be recovered")
	}
}

func TestBalancerIntegration(t *testing.T) {
	config := types.Config{
		Mode:      "load_balance",
		Algorithm: "round_robin",
		Servers: []types.UpstreamServer{
			{URL: "https://api1.example.com", Token: "token1"},
			{URL: "https://api2.example.com", Token: "token2"},
		},
	}

	balancer := New(config)

	// Test full workflow: get server, mark down, recover
	server, err := balancer.GetNextServer()
	if err != nil {
		t.Fatalf("GetNextServer failed: %v", err)
	}

	serverUrl := server.URL

	// Mark it down
	balancer.MarkServerDown(serverUrl)

	// Should still be able to get a server (the other one)
	server2, err := balancer.GetNextServer()
	if err != nil {
		t.Fatalf("GetNextServer failed after marking one down: %v", err)
	}

	if server2.URL == serverUrl {
		t.Error("Should get different server after marking one down")
	}

	// Recover the first server
	balancer.RecoverServer(serverUrl)

	// Should be able to get servers again
	_, err = balancer.GetNextServer()
	if err != nil {
		t.Fatalf("GetNextServer failed after recovery: %v", err)
	}
}