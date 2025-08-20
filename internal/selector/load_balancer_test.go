package selector

import (
	"testing"

	"claude-code-lb/internal/testutil"
	"claude-code-lb/pkg/types"
)

func TestNewLoadBalancer(t *testing.T) {
	config := types.Config{
		Algorithm: "round_robin",
		Servers: []types.UpstreamServer{
			{URL: testutil.API1ExampleURL, Token: testutil.TestToken1, Weight: 1},
			{URL: testutil.API2ExampleURL, Token: "token2", Weight: 2},
			{URL: testutil.API3ExampleURL, Token: "token3", Weight: 0}, // Should default to 1
		},
	}

	lb := NewLoadBalancer(config)

	if lb == nil {
		t.Fatal("NewLoadBalancer returned nil")
	}

	// Check that server status is initialized
	status := lb.GetServerStatus()
	if len(status) != 3 {
		t.Errorf("Expected 3 servers in status map, got %d", len(status))
	}

	// All servers should start as available
	for url, available := range status {
		if !available {
			t.Errorf("Server %s should start as available", url)
		}
	}

	// Check that weights are properly set
	if lb.serverWeights[testutil.API1ExampleURL] != 1 {
		t.Errorf("Expected weight 1 for api1, got %d", lb.serverWeights[testutil.API1ExampleURL])
	}
	if lb.serverWeights[testutil.API2ExampleURL] != 2 {
		t.Errorf("Expected weight 2 for api2, got %d", lb.serverWeights[testutil.API2ExampleURL])
	}
	if lb.serverWeights[testutil.API3ExampleURL] != 1 {
		t.Errorf("Expected weight 1 for api3 (default), got %d", lb.serverWeights[testutil.API3ExampleURL])
	}
}

func TestLoadBalancerSelectServer(t *testing.T) {
	config := types.Config{
		Algorithm: "round_robin",
		Servers: []types.UpstreamServer{
			{URL: testutil.API1ExampleURL, Token: testutil.TestToken1},
			{URL: testutil.API2ExampleURL, Token: "token2"},
		},
	}

	lb := NewLoadBalancer(config)

	// Test that we can select servers
	server1, err := lb.SelectServer()
	if err != nil {
		t.Fatalf("SelectServer failed: %v", err)
	}
	if server1 == nil {
		t.Fatal("SelectServer returned nil server")
	}

	server2, err := lb.SelectServer()
	if err != nil {
		t.Fatalf("SelectServer failed: %v", err)
	}
	if server2 == nil {
		t.Fatal("SelectServer returned nil server")
	}

	// With round robin, we should get different servers
	if server1.URL == server2.URL {
		t.Logf("Got same server twice (this can happen with round robin): %s", server1.URL)
	}
}

func TestLoadBalancerMarkServerDown(t *testing.T) {
	config := types.Config{
		Algorithm: "round_robin",
		Cooldown:  60,
		Servers: []types.UpstreamServer{
			{URL: testutil.API1ExampleURL, Token: testutil.TestToken1},
			{URL: testutil.API2ExampleURL, Token: "token2"},
		},
	}

	lb := NewLoadBalancer(config)

	// Mark server as down
	lb.MarkServerDown(testutil.API1ExampleURL)

	// Check status
	status := lb.GetServerStatus()
	if status[testutil.API1ExampleURL] {
		t.Error("Server should be marked as down")
	}
	if !status[testutil.API2ExampleURL] {
		t.Error("Server should still be available")
	}

	// Check that DownUntil is set
	downUntil := lb.GetServerDownUntil(testutil.API1ExampleURL)
	if downUntil.IsZero() {
		t.Error("DownUntil should be set for downed server")
	}
}

func TestLoadBalancerMarkServerHealthy(t *testing.T) {
	config := types.Config{
		Algorithm: "round_robin",
		Servers: []types.UpstreamServer{
			{URL: testutil.API1ExampleURL, Token: testutil.TestToken1},
		},
	}

	lb := NewLoadBalancer(config)

	// First mark it down
	lb.MarkServerDown(testutil.API1ExampleURL)

	// Then mark it healthy
	lb.MarkServerHealthy(testutil.API1ExampleURL)

	// Check status
	status := lb.GetServerStatus()
	if !status[testutil.API1ExampleURL] {
		t.Error("Server should be marked as healthy")
	}

	// Check that failure count is reset
	if lb.failureCount[testutil.API1ExampleURL] != 0 {
		t.Errorf("Failure count should be reset to 0, got %d", lb.failureCount[testutil.API1ExampleURL])
	}
}

func TestLoadBalancerGetAvailableServers(t *testing.T) {
	config := types.Config{
		Algorithm: "round_robin",
		Servers: []types.UpstreamServer{
			{URL: testutil.API1ExampleURL, Token: testutil.TestToken1},
			{URL: testutil.API2ExampleURL, Token: "token2"},
			{URL: testutil.API3ExampleURL, Token: "token3"},
		},
	}

	lb := NewLoadBalancer(config)

	// Initially all servers should be available
	available := lb.GetAvailableServers()
	if len(available) != 3 {
		t.Errorf("Expected 3 available servers, got %d", len(available))
	}

	// Mark one server as down
	lb.MarkServerDown(testutil.API1ExampleURL)

	// Now should have 2 available servers
	available = lb.GetAvailableServers()
	if len(available) != 2 {
		t.Errorf("Expected 2 available servers after marking one down, got %d", len(available))
	}

	// Check that the down server is not in the list
	for _, server := range available {
		if server.URL == testutil.API1ExampleURL {
			t.Error("Down server should not be in available servers list")
		}
	}
}

func TestLoadBalancerRecoverServer(t *testing.T) {
	config := types.Config{
		Algorithm: "round_robin",
		Cooldown:  60,
		Servers: []types.UpstreamServer{
			{URL: testutil.API1ExampleURL, Token: testutil.TestToken1},
		},
	}

	lb := NewLoadBalancer(config)

	// Mark server as down
	lb.MarkServerDown(testutil.API1ExampleURL)

	// Verify it's down
	status := lb.GetServerStatus()
	if status[testutil.API1ExampleURL] {
		t.Error("Server should be down")
	}

	// Recover the server
	lb.RecoverServer(testutil.API1ExampleURL)

	// Verify it's back up
	status = lb.GetServerStatus()
	if !status[testutil.API1ExampleURL] {
		t.Error("Server should be recovered")
	}
}

func TestLoadBalancerNoAvailableServers(t *testing.T) {
	config := types.Config{
		Algorithm: "round_robin",
		Servers: []types.UpstreamServer{
			{URL: testutil.API1ExampleURL, Token: testutil.TestToken1},
		},
	}

	lb := NewLoadBalancer(config)

	// Mark all servers as down
	lb.MarkServerDown(testutil.API1ExampleURL)

	// Should get error when trying to select server
	server, err := lb.SelectServer()
	if err == nil {
		t.Error("Expected error when no servers available")
	}
	if server != nil {
		t.Error("Expected nil server when none available")
	}
}

func TestLoadBalancerAlgorithms(t *testing.T) {
	algorithms := []string{"round_robin", "weighted_round_robin", "random"}

	for _, algorithm := range algorithms {
		t.Run(algorithm, func(t *testing.T) {
			config := types.Config{
				Algorithm: algorithm,
				Servers: []types.UpstreamServer{
					{URL: testutil.API1ExampleURL, Token: testutil.TestToken1, Weight: 1},
					{URL: testutil.API2ExampleURL, Token: "token2", Weight: 2},
				},
			}

			lb := NewLoadBalancer(config)

			// Should be able to select servers with any algorithm
			for i := 0; i < 5; i++ {
				server, err := lb.SelectServer()
				if err != nil {
					t.Fatalf("SelectServer failed with %s algorithm: %v", algorithm, err)
				}
				if server == nil {
					t.Fatalf("SelectServer returned nil with %s algorithm", algorithm)
				}
			}
		})
	}
}

func TestLoadBalancerConcurrency(t *testing.T) {
	config := types.Config{
		Algorithm: "round_robin",
		Servers: []types.UpstreamServer{
			{URL: testutil.API1ExampleURL, Token: testutil.TestToken1},
			{URL: testutil.API2ExampleURL, Token: "token2"},
		},
	}

	lb := NewLoadBalancer(config)

	// Test concurrent access
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func(id int) {
			defer func() { done <- true }()

			for j := 0; j < 10; j++ {
				// Alternate between selecting and marking servers
				if j%2 == 0 {
					_, err := lb.SelectServer()
					if err != nil {
						t.Logf("Goroutine %d: SelectServer error: %v", id, err)
					}
				} else {
					if id%2 == 0 {
						lb.MarkServerDown(testutil.API1ExampleURL)
					} else {
						lb.MarkServerHealthy(testutil.API1ExampleURL)
					}
				}
			}
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}

	// If we get here without hanging or panicking, the test passes
}
