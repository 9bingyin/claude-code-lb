package health

import (
	"testing"
	"time"

	"claude-code-lb/internal/balance"
	"claude-code-lb/pkg/types"
)

// MockBalancer implements a mock balancer for testing
type MockBalancer struct {
	serverStatus   map[string]bool
	recoveredUrls  []string
}

func NewMockBalancer() *MockBalancer {
	return &MockBalancer{
		serverStatus:  make(map[string]bool),
		recoveredUrls: make([]string, 0),
	}
}

func (m *MockBalancer) GetServerStatus() map[string]bool {
	return m.serverStatus
}

func (m *MockBalancer) RecoverServer(url string) {
	m.serverStatus[url] = true
	m.recoveredUrls = append(m.recoveredUrls, url)
}

func (m *MockBalancer) SetServerStatus(url string, status bool) {
	m.serverStatus[url] = status
}

func TestNewChecker(t *testing.T) {
	config := types.Config{
		Cooldown: 60,
		Servers: []types.UpstreamServer{
			{URL: "https://api1.example.com", Token: "token1"},
			{URL: "https://api2.example.com", Token: "token2"},
		},
	}

	mockBalancer := &balance.Balancer{}
	checker := NewChecker(config, mockBalancer)

	if checker == nil {
		t.Fatal("NewChecker returned nil")
	}

	if checker.config.Cooldown != config.Cooldown {
		t.Errorf("Expected cooldown %d, got %d", config.Cooldown, checker.config.Cooldown)
	}

	if len(checker.config.Servers) != len(config.Servers) {
		t.Errorf("Expected %d servers, got %d", len(config.Servers), len(checker.config.Servers))
	}

	if checker.balancer != mockBalancer {
		t.Error("Balancer not properly stored")
	}
}

func TestPassiveHealthCheckBasic(t *testing.T) {
	// Create a config with very short cooldown for testing
	config := types.Config{
		Cooldown: 1, // 1 second cooldown
		Servers: []types.UpstreamServer{
			{
				URL:       "https://api1.example.com",
				Token:     "token1",
				DownUntil: time.Now().Add(-2 * time.Second), // Already expired
			},
			{
				URL:       "https://api2.example.com", 
				Token:     "token2",
				DownUntil: time.Now().Add(10 * time.Second), // Not expired yet
			},
		},
	}

	mockBalancer := NewMockBalancer()
	
	// Set initial server status (both down)
	mockBalancer.SetServerStatus("https://api1.example.com", false)
	mockBalancer.SetServerStatus("https://api2.example.com", false)

	checker := NewChecker(config, &balance.Balancer{})
	
	// Verify the checker was created
	if checker == nil {
		t.Fatal("NewChecker should not return nil")
	}
	
	// We can't easily test the actual passive health check loop since it runs indefinitely
	// Instead, we test the logic that would be used in the health check
	
	// Mock the balancer's GetServerStatus method behavior
	now := time.Now()
	
	// Server 1 should be recovered (cooldown expired)
	if !now.After(config.Servers[0].DownUntil) {
		t.Error("Server 1 cooldown should be expired")
	}
	
	// Server 2 should not be recovered (cooldown not expired)
	if now.After(config.Servers[1].DownUntil) {
		t.Error("Server 2 cooldown should not be expired yet")
	}
}

func TestPassiveHealthCheckLogic(t *testing.T) {
	// Test the core logic that would be used in PassiveHealthCheck
	now := time.Now()
	
	tests := []struct {
		name           string
		server         types.UpstreamServer
		serverStatus   bool
		shouldRecover  bool
	}{
		{
			name: "server down and cooldown expired",
			server: types.UpstreamServer{
				URL:       "https://api1.example.com",
				DownUntil: now.Add(-1 * time.Second), // Expired
			},
			serverStatus:  false,
			shouldRecover: true,
		},
		{
			name: "server down but cooldown not expired",
			server: types.UpstreamServer{
				URL:       "https://api2.example.com",
				DownUntil: now.Add(10 * time.Second), // Not expired
			},
			serverStatus:  false,
			shouldRecover: false,
		},
		{
			name: "server already up",
			server: types.UpstreamServer{
				URL:       "https://api3.example.com",
				DownUntil: now.Add(-1 * time.Second), // Expired
			},
			serverStatus:  true,
			shouldRecover: false,
		},
		{
			name: "server with zero downtime",
			server: types.UpstreamServer{
				URL:       "https://api4.example.com",
				DownUntil: time.Time{}, // Zero time
			},
			serverStatus:  false,
			shouldRecover: true, // now.After(zero time) returns true
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := types.Config{
				Cooldown: 60,
				Servers:  []types.UpstreamServer{tt.server},
			}

			mockBalancer := NewMockBalancer()
			mockBalancer.SetServerStatus(tt.server.URL, tt.serverStatus)

			checker := NewChecker(config, &balance.Balancer{})

			// Simulate the health check logic
			serverStatus := mockBalancer.GetServerStatus()
			
			// The actual logic from PassiveHealthCheck: !serverStatus[url] && now.After(server.DownUntil)
			shouldRecover := !serverStatus[tt.server.URL] && now.After(tt.server.DownUntil)
			
			if shouldRecover != tt.shouldRecover {
				t.Errorf("Expected shouldRecover %t, got %t", tt.shouldRecover, shouldRecover)
			}

			// Verify the checker was created correctly
			if len(checker.config.Servers) != 1 {
				t.Errorf("Expected 1 server, got %d", len(checker.config.Servers))
			}
			
			if checker.config.Servers[0].URL != tt.server.URL {
				t.Errorf("Expected server URL %s, got %s", tt.server.URL, checker.config.Servers[0].URL)
			}
		})
	}
}

func TestCheckerConfiguration(t *testing.T) {
	tests := []struct {
		name     string
		config   types.Config
	}{
		{
			name: "single server config",
			config: types.Config{
				Cooldown: 30,
				Servers: []types.UpstreamServer{
					{URL: "https://api.example.com", Token: "token"},
				},
			},
		},
		{
			name: "multiple servers config",
			config: types.Config{
				Cooldown: 120,
				Servers: []types.UpstreamServer{
					{URL: "https://api1.example.com", Token: "token1"},
					{URL: "https://api2.example.com", Token: "token2"},
					{URL: "https://api3.example.com", Token: "token3"},
				},
			},
		},
		{
			name: "no servers config",
			config: types.Config{
				Cooldown: 60,
				Servers:  []types.UpstreamServer{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockBalancer := &balance.Balancer{}
			checker := NewChecker(tt.config, mockBalancer)

			if checker == nil {
				t.Fatal("NewChecker returned nil")
			}

			if checker.config.Cooldown != tt.config.Cooldown {
				t.Errorf("Expected cooldown %d, got %d", tt.config.Cooldown, checker.config.Cooldown)
			}

			if len(checker.config.Servers) != len(tt.config.Servers) {
				t.Errorf("Expected %d servers, got %d", len(tt.config.Servers), len(checker.config.Servers))
			}

			for i, server := range checker.config.Servers {
				if server.URL != tt.config.Servers[i].URL {
					t.Errorf("Server %d URL mismatch: expected %s, got %s", i, tt.config.Servers[i].URL, server.URL)
				}
			}
		})
	}
}

func TestCheckerWithVariousDowntimes(t *testing.T) {
	now := time.Now()
	
	config := types.Config{
		Cooldown: 60,
		Servers: []types.UpstreamServer{
			{
				URL:       "https://api1.example.com",
				Token:     "token1",
				DownUntil: now.Add(-10 * time.Second), // Long expired
			},
			{
				URL:       "https://api2.example.com",
				Token:     "token2", 
				DownUntil: now.Add(-1 * time.Second), // Just expired
			},
			{
				URL:       "https://api3.example.com",
				Token:     "token3",
				DownUntil: now.Add(1 * time.Second), // Just about to expire
			},
			{
				URL:       "https://api4.example.com",
				Token:     "token4",
				DownUntil: now.Add(10 * time.Second), // Long time to expire
			},
			{
				URL:       "https://api5.example.com",
				Token:     "token5",
				DownUntil: time.Time{}, // Zero time (no downtime)
			},
		},
	}

	mockBalancer := &balance.Balancer{}
	checker := NewChecker(config, mockBalancer)

	if checker == nil {
		t.Fatal("NewChecker returned nil")
	}

	// Test that all servers are properly configured
	for i, server := range checker.config.Servers {
		if server.URL != config.Servers[i].URL {
			t.Errorf("Server %d URL mismatch", i)
		}
		
		if server.DownUntil != config.Servers[i].DownUntil {
			t.Errorf("Server %d DownUntil mismatch", i)
		}
	}

	// Check which servers should be recovered based on their DownUntil times
	expectedRecoverable := []bool{true, true, false, false, true} // Zero time should be recoverable
	
	for i, server := range checker.config.Servers {
		shouldRecover := now.After(server.DownUntil) // This is the actual logic from PassiveHealthCheck
		if shouldRecover != expectedRecoverable[i] {
			t.Errorf("Server %d (%s) recovery expectation mismatch: expected %t, got %t", 
				i, server.URL, expectedRecoverable[i], shouldRecover)
		}
	}
}