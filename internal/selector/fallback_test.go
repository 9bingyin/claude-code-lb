package selector

import (
	"testing"

	"claude-code-lb/pkg/types"
)

func TestNewFallbackSelector(t *testing.T) {
	config := types.Config{
		Mode: "fallback",
		Servers: []types.UpstreamServer{
			{URL: "https://api1.example.com", Token: "token1", Priority: 2, Weight: 1},
			{URL: "https://api2.example.com", Token: "token2", Priority: 1, Weight: 2},
			{URL: "https://api3.example.com", Token: "token3", Priority: 3, Weight: 3},
		},
	}

	fs := NewFallbackSelector(config)

	if fs == nil {
		t.Fatal("NewFallbackSelector returned nil")
	}

	// Check that servers are ordered by priority
	if len(fs.orderedServers) != 3 {
		t.Errorf("Expected 3 ordered servers, got %d", len(fs.orderedServers))
	}

	// Should be ordered by priority (1, 2, 3)
	if fs.orderedServers[0].Priority != 1 {
		t.Errorf("First server should have priority 1, got %d", fs.orderedServers[0].Priority)
	}
	if fs.orderedServers[1].Priority != 2 {
		t.Errorf("Second server should have priority 2, got %d", fs.orderedServers[1].Priority)
	}
	if fs.orderedServers[2].Priority != 3 {
		t.Errorf("Third server should have priority 3, got %d", fs.orderedServers[2].Priority)
	}

	// Check URLs are in correct order
	expectedUrls := []string{
		"https://api2.example.com", // priority 1
		"https://api1.example.com", // priority 2
		"https://api3.example.com", // priority 3
	}

	for i, expectedUrl := range expectedUrls {
		if fs.orderedServers[i].URL != expectedUrl {
			t.Errorf("Server %d should be %s, got %s", i, expectedUrl, fs.orderedServers[i].URL)
		}
	}
}

func TestNewFallbackSelectorWeightBasedPriority(t *testing.T) {
	config := types.Config{
		Mode: "fallback",
		Servers: []types.UpstreamServer{
			{URL: "https://api1.example.com", Token: "token1", Weight: 1}, // priority 0, should become 3
			{URL: "https://api2.example.com", Token: "token2", Weight: 3}, // priority 0, should become 1
			{URL: "https://api3.example.com", Token: "token3", Weight: 2}, // priority 0, should become 2
		},
	}

	fs := NewFallbackSelector(config)

	// Servers should be ordered by weight (higher weight = higher priority = lower priority number)
	// Weight 3 -> Priority 1, Weight 2 -> Priority 2, Weight 1 -> Priority 3
	expectedUrls := []string{
		"https://api2.example.com", // weight 3 -> priority 1
		"https://api3.example.com", // weight 2 -> priority 2
		"https://api1.example.com", // weight 1 -> priority 3
	}

	for i, expectedUrl := range expectedUrls {
		if fs.orderedServers[i].URL != expectedUrl {
			t.Errorf("Server %d should be %s, got %s", i, expectedUrl, fs.orderedServers[i].URL)
		}
	}
}

func TestFallbackSelectorSelectServer(t *testing.T) {
	config := types.Config{
		Mode: "fallback",
		Servers: []types.UpstreamServer{
			{URL: "https://api1.example.com", Token: "token1", Priority: 2},
			{URL: "https://api2.example.com", Token: "token2", Priority: 1},
		},
	}

	fs := NewFallbackSelector(config)

	// Should always select the highest priority (lowest number) available server
	server, err := fs.SelectServer()
	if err != nil {
		t.Fatalf("SelectServer failed: %v", err)
	}
	if server == nil {
		t.Fatal("SelectServer returned nil")
	}
	if server.URL != "https://api2.example.com" {
		t.Errorf("Should select highest priority server, got %s", server.URL)
	}

	// Select again, should get the same server
	server2, err := fs.SelectServer()
	if err != nil {
		t.Fatalf("SelectServer failed: %v", err)
	}
	if server2.URL != server.URL {
		t.Error("Fallback selector should consistently return the same highest priority server")
	}
}

func TestFallbackSelectorWithFailover(t *testing.T) {
	config := types.Config{
		Mode: "fallback",
		Servers: []types.UpstreamServer{
			{URL: "https://api1.example.com", Token: "token1", Priority: 1},
			{URL: "https://api2.example.com", Token: "token2", Priority: 2},
			{URL: "https://api3.example.com", Token: "token3", Priority: 3},
		},
	}

	fs := NewFallbackSelector(config)

	// Initially should select priority 1 server
	server, err := fs.SelectServer()
	if err != nil {
		t.Fatalf("SelectServer failed: %v", err)
	}
	if server.URL != "https://api1.example.com" {
		t.Errorf("Should select priority 1 server, got %s", server.URL)
	}

	// Mark priority 1 server as down
	fs.MarkServerDown("https://api1.example.com")

	// Should now select priority 2 server
	server, err = fs.SelectServer()
	if err != nil {
		t.Fatalf("SelectServer failed: %v", err)
	}
	if server.URL != "https://api2.example.com" {
		t.Errorf("Should select priority 2 server after priority 1 is down, got %s", server.URL)
	}

	// Mark priority 2 server as down
	fs.MarkServerDown("https://api2.example.com")

	// Should now select priority 3 server
	server, err = fs.SelectServer()
	if err != nil {
		t.Fatalf("SelectServer failed: %v", err)
	}
	if server.URL != "https://api3.example.com" {
		t.Errorf("Should select priority 3 server after priorities 1 and 2 are down, got %s", server.URL)
	}
}

func TestFallbackSelectorMarkServerDown(t *testing.T) {
	config := types.Config{
		Mode:     "fallback",
		Cooldown: 60,
		Servers: []types.UpstreamServer{
			{URL: "https://api1.example.com", Token: "token1", Priority: 1},
		},
	}

	fs := NewFallbackSelector(config)

	// Mark server as down
	fs.MarkServerDown("https://api1.example.com")

	// Check status
	status := fs.GetServerStatus()
	if status["https://api1.example.com"] {
		t.Error("Server should be marked as down")
	}
}

func TestFallbackSelectorMarkServerHealthy(t *testing.T) {
	config := types.Config{
		Mode: "fallback",
		Servers: []types.UpstreamServer{
			{URL: "https://api1.example.com", Token: "token1", Priority: 1},
		},
	}

	fs := NewFallbackSelector(config)

	// Mark it down first
	fs.MarkServerDown("https://api1.example.com")

	// Then mark it healthy
	fs.MarkServerHealthy("https://api1.example.com")

	// Check status
	status := fs.GetServerStatus()
	if !status["https://api1.example.com"] {
		t.Error("Server should be marked as healthy")
	}
}

func TestFallbackSelectorGetAvailableServers(t *testing.T) {
	config := types.Config{
		Mode: "fallback",
		Servers: []types.UpstreamServer{
			{URL: "https://api1.example.com", Token: "token1", Priority: 1},
			{URL: "https://api2.example.com", Token: "token2", Priority: 2},
		},
	}

	fs := NewFallbackSelector(config)

	// Initially all servers should be available
	available := fs.GetAvailableServers()
	if len(available) != 2 {
		t.Errorf("Expected 2 available servers, got %d", len(available))
	}

	// Mark one server as down
	fs.MarkServerDown("https://api1.example.com")

	// Should have 1 available server
	available = fs.GetAvailableServers()
	if len(available) != 1 {
		t.Errorf("Expected 1 available server after marking one down, got %d", len(available))
	}

	if available[0].URL != "https://api2.example.com" {
		t.Errorf("Available server should be api2, got %s", available[0].URL)
	}
}

func TestFallbackSelectorRecoverServer(t *testing.T) {
	config := types.Config{
		Mode: "fallback",
		Servers: []types.UpstreamServer{
			{URL: "https://api1.example.com", Token: "token1", Priority: 1},
		},
	}

	fs := NewFallbackSelector(config)

	// Mark server as down
	fs.MarkServerDown("https://api1.example.com")

	// Verify it's down
	status := fs.GetServerStatus()
	if status["https://api1.example.com"] {
		t.Error("Server should be down")
	}

	// Recover the server
	fs.RecoverServer("https://api1.example.com")

	// Verify it's back up
	status = fs.GetServerStatus()
	if !status["https://api1.example.com"] {
		t.Error("Server should be recovered")
	}
}

func TestFallbackSelectorNoAvailableServers(t *testing.T) {
	config := types.Config{
		Mode: "fallback",
		Servers: []types.UpstreamServer{
			{URL: "https://api1.example.com", Token: "token1", Priority: 1},
		},
	}

	fs := NewFallbackSelector(config)

	// Mark all servers as down
	fs.MarkServerDown("https://api1.example.com")

	// Should get an emergency fallback or error
	server, err := fs.SelectServer()
	if err == nil && server != nil {
		// If we get a server, it should be the emergency fallback
		if server.URL != "https://api1.example.com" {
			t.Errorf("Emergency fallback should return the configured server, got %s", server.URL)
		}
	} else if err != nil {
		// Error is also acceptable when no servers are available
		t.Logf("No servers available, got error: %v", err)
	}
}

func TestFallbackSelectorEmergencyFallback(t *testing.T) {
	config := types.Config{
		Mode:     "fallback",
		Cooldown: 60,
		Servers: []types.UpstreamServer{
			{URL: "https://api1.example.com", Token: "token1", Priority: 1},
			{URL: "https://api2.example.com", Token: "token2", Priority: 2},
		},
	}

	fs := NewFallbackSelector(config)

	// Mark all servers as down
	fs.MarkServerDown("https://api1.example.com")
	fs.MarkServerDown("https://api2.example.com")

	// Should get emergency fallback (server with shortest remaining cooldown)
	server, err := fs.SelectServer()
	if err != nil && server == nil {
		t.Logf("No emergency fallback available, got error: %v", err)
	} else if server != nil {
		// Should get one of the configured servers
		validUrls := []string{"https://api1.example.com", "https://api2.example.com"}
		found := false
		for _, url := range validUrls {
			if server.URL == url {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Emergency fallback returned unexpected server: %s", server.URL)
		}
	}
}

func TestFallbackSelectorPriorityConsistency(t *testing.T) {
	config := types.Config{
		Mode: "fallback",
		Servers: []types.UpstreamServer{
			{URL: "https://api1.example.com", Token: "token1", Priority: 3},
			{URL: "https://api2.example.com", Token: "token2", Priority: 1},
			{URL: "https://api3.example.com", Token: "token3", Priority: 2},
		},
	}

	fs := NewFallbackSelector(config)

	// Multiple selections should always return the same highest priority server
	var selectedUrl string
	for i := 0; i < 5; i++ {
		server, err := fs.SelectServer()
		if err != nil {
			t.Fatalf("SelectServer failed on iteration %d: %v", i, err)
		}
		
		if i == 0 {
			selectedUrl = server.URL
			if selectedUrl != "https://api2.example.com" {
				t.Errorf("First selection should be highest priority server (api2), got %s", selectedUrl)
			}
		} else {
			if server.URL != selectedUrl {
				t.Errorf("Selection %d returned different server: expected %s, got %s", i, selectedUrl, server.URL)
			}
		}
	}
}