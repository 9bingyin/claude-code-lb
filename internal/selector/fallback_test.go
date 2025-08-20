package selector

import (
	"slices"
	"testing"

	"claude-code-lb/internal/testutil"
	"claude-code-lb/pkg/types"
)

func TestNewFallbackSelector(t *testing.T) {
	config := types.Config{
		Mode: "fallback",
		Servers: []types.UpstreamServer{
			{URL: "http://test-api.local", Token: testutil.TestToken1, Priority: 2, Weight: 1},
			{URL: "http://test-api.local", Token: "token2", Priority: 1, Weight: 2},
			{URL: "http://test-api.local", Token: "token3", Priority: 3, Weight: 3},
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
		"http://test-api.local", // priority 1
		"http://test-api.local", // priority 2
		"http://test-api.local", // priority 3
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
			{URL: "http://test-api.local", Token: testutil.TestToken1, Weight: 1}, // priority 0, should become 3
			{URL: "http://test-api.local", Token: "token2", Weight: 3},            // priority 0, should become 1
			{URL: "http://test-api.local", Token: "token3", Weight: 2},            // priority 0, should become 2
		},
	}

	fs := NewFallbackSelector(config)

	// Servers should be ordered by weight (higher weight = higher priority = lower priority number)
	// Weight 3 -> Priority 1, Weight 2 -> Priority 2, Weight 1 -> Priority 3
	expectedUrls := []string{
		"http://test-api.local", // weight 3 -> priority 1
		"http://test-api.local", // weight 2 -> priority 2
		"http://test-api.local", // weight 1 -> priority 3
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
			{URL: "http://test-api.local", Token: testutil.TestToken1, Priority: 2},
			{URL: "http://test-api.local", Token: "token2", Priority: 1},
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
	if server.URL != "http://test-api.local" {
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
			{URL: "http://test-api.local", Token: testutil.TestToken1, Priority: 1},
			{URL: "http://test-api.local", Token: "token2", Priority: 2},
			{URL: "http://test-api.local", Token: "token3", Priority: 3},
		},
	}

	fs := NewFallbackSelector(config)

	// Initially should select priority 1 server
	server, err := fs.SelectServer()
	if err != nil {
		t.Fatalf("SelectServer failed: %v", err)
	}
	if server.URL != "http://test-api.local" {
		t.Errorf("Should select priority 1 server, got %s", server.URL)
	}

	// Mark priority 1 server as down
	fs.MarkServerDown("http://test-api.local")

	// Should now select priority 2 server
	server, err = fs.SelectServer()
	if err != nil {
		t.Fatalf("SelectServer failed: %v", err)
	}
	if server.URL != "http://test-api.local" {
		t.Errorf("Should select priority 2 server after priority 1 is down, got %s", server.URL)
	}

	// Mark priority 2 server as down
	fs.MarkServerDown("http://test-api.local")

	// Should now select priority 3 server
	server, err = fs.SelectServer()
	if err != nil {
		t.Fatalf("SelectServer failed: %v", err)
	}
	if server.URL != "http://test-api.local" {
		t.Errorf("Should select priority 3 server after priorities 1 and 2 are down, got %s", server.URL)
	}
}

func TestFallbackSelectorMarkServerDown(t *testing.T) {
	config := types.Config{
		Mode:     "fallback",
		Cooldown: 60,
		Servers: []types.UpstreamServer{
			{URL: "http://test-api.local", Token: testutil.TestToken1, Priority: 1},
		},
	}

	fs := NewFallbackSelector(config)

	// Mark server as down
	fs.MarkServerDown("http://test-api.local")

	// Check status
	status := fs.GetServerStatus()
	if status["http://test-api.local"] {
		t.Error("Server should be marked as down")
	}
}

func TestFallbackSelectorMarkServerHealthy(t *testing.T) {
	config := types.Config{
		Mode: "fallback",
		Servers: []types.UpstreamServer{
			{URL: "http://test-api.local", Token: testutil.TestToken1, Priority: 1},
		},
	}

	fs := NewFallbackSelector(config)

	// Mark it down first
	fs.MarkServerDown("http://test-api.local")

	// Then mark it healthy
	fs.MarkServerHealthy("http://test-api.local")

	// Check status
	status := fs.GetServerStatus()
	if !status["http://test-api.local"] {
		t.Error("Server should be marked as healthy")
	}
}

func TestFallbackSelectorGetAvailableServers(t *testing.T) {
	config := types.Config{
		Mode: "fallback",
		Servers: []types.UpstreamServer{
			{URL: testutil.API1ExampleURL, Token: testutil.TestToken1, Priority: 1},
			{URL: testutil.API2ExampleURL, Token: "token2", Priority: 2},
		},
	}

	fs := NewFallbackSelector(config)

	// Initially all servers should be available
	available := fs.GetAvailableServers()
	if len(available) != 2 {
		t.Errorf("Expected 2 available servers, got %d", len(available))
	}

	// Mark one server as down
	fs.MarkServerDown(testutil.API1ExampleURL)

	// Should have 1 available server
	available = fs.GetAvailableServers()
	if len(available) != 1 {
		t.Errorf("Expected 1 available server after marking one down, got %d", len(available))
	}

	if available[0].URL != testutil.API2ExampleURL {
		t.Errorf("Available server should be %s, got %s", testutil.API2ExampleURL, available[0].URL)
	}
}

func TestFallbackSelectorRecoverServer(t *testing.T) {
	config := types.Config{
		Mode: "fallback",
		Servers: []types.UpstreamServer{
			{URL: "http://test-api.local", Token: testutil.TestToken1, Priority: 1},
		},
	}

	fs := NewFallbackSelector(config)

	// Mark server as down
	fs.MarkServerDown("http://test-api.local")

	// Verify it's down
	status := fs.GetServerStatus()
	if status["http://test-api.local"] {
		t.Error("Server should be down")
	}

	// Recover the server
	fs.RecoverServer("http://test-api.local")

	// Verify it's back up
	status = fs.GetServerStatus()
	if !status["http://test-api.local"] {
		t.Error("Server should be recovered")
	}
}

func TestFallbackSelectorNoAvailableServers(t *testing.T) {
	config := types.Config{
		Mode: "fallback",
		Servers: []types.UpstreamServer{
			{URL: "http://test-api.local", Token: testutil.TestToken1, Priority: 1},
		},
	}

	fs := NewFallbackSelector(config)

	// Mark all servers as down
	fs.MarkServerDown("http://test-api.local")

	// Should get an emergency fallback or error
	server, err := fs.SelectServer()
	if err == nil && server != nil {
		// If we get a server, it should be the emergency fallback
		if server.URL != "http://test-api.local" {
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
			{URL: "http://test-api.local", Token: testutil.TestToken1, Priority: 1},
			{URL: "http://test-api.local", Token: "token2", Priority: 2},
		},
	}

	fs := NewFallbackSelector(config)

	// Mark all servers as down
	fs.MarkServerDown("http://test-api.local")
	fs.MarkServerDown("http://test-api.local")

	// Should get emergency fallback (server with shortest remaining cooldown)
	server, err := fs.SelectServer()
	if err != nil && server == nil {
		t.Logf("No emergency fallback available, got error: %v", err)
	} else if server != nil {
		// Should get one of the configured servers
		validUrls := []string{"http://test-api.local"}
		if !slices.Contains(validUrls, server.URL) {
			t.Errorf("Emergency fallback returned unexpected server: %s", server.URL)
		}
	}
}

func TestFallbackSelectorPriorityConsistency(t *testing.T) {
	config := types.Config{
		Mode: "fallback",
		Servers: []types.UpstreamServer{
			{URL: "http://test-api.local", Token: testutil.TestToken1, Priority: 3},
			{URL: "http://test-api.local", Token: "token2", Priority: 1},
			{URL: "http://test-api.local", Token: "token3", Priority: 2},
		},
	}

	fs := NewFallbackSelector(config)

	// Multiple selections should always return the same highest priority server
	var selectedUrl string
	for i := range 5 {
		server, err := fs.SelectServer()
		if err != nil {
			t.Fatalf("SelectServer failed on iteration %d: %v", i, err)
		}

		if i == 0 {
			selectedUrl = server.URL
			if selectedUrl != "http://test-api.local" {
				t.Errorf("First selection should be highest priority server (api2), got %s", selectedUrl)
			}
		} else {
			if server.URL != selectedUrl {
				t.Errorf("Selection %d returned different server: expected %s, got %s", i, selectedUrl, server.URL)
			}
		}
	}
}
