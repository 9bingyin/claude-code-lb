package balance

import (
	"testing"
	"time"

	"claude-code-lb/pkg/types"
)

// MockBalancer implements BalancerInterface for testing
type MockBalancer struct {
	downServers map[string]bool
}

func NewMockBalancer() *MockBalancer {
	return &MockBalancer{
		downServers: make(map[string]bool),
	}
}

func (m *MockBalancer) MarkServerDown(url string) {
	m.downServers[url] = true
}

func TestNewBalanceChecker(t *testing.T) {
	config := types.Config{
		Servers: []types.UpstreamServer{
			{
				URL:                  "https://api1.example.com",
				Token:                "token1",
				BalanceCheck:         "echo 100.50",
				BalanceCheckInterval: 300,
				BalanceThreshold:     10.0,
			},
		},
	}

	mockBalancer := NewMockBalancer()
	checker := NewBalanceChecker(config, mockBalancer)

	if checker == nil {
		t.Fatal("NewBalanceChecker returned nil")
	}

	if checker.config.Servers[0].URL != config.Servers[0].URL {
		t.Error("Config not properly stored")
	}

	if checker.balancer != mockBalancer {
		t.Error("Balancer not properly stored")
	}

	if checker.commandTimeout != 30*time.Second {
		t.Errorf("Expected command timeout 30s, got %v", checker.commandTimeout)
	}

	if checker.balances == nil {
		t.Error("Balances map should be initialized")
	}

	if checker.stopChan == nil {
		t.Error("Stop channel should be initialized")
	}

	if checker.serverTimers == nil {
		t.Error("Server timers map should be initialized")
	}
}

func TestBalanceCheckerGetBalance(t *testing.T) {
	config := types.Config{
		Servers: []types.UpstreamServer{
			{URL: "https://api1.example.com", Token: "token1"},
		},
	}

	mockBalancer := NewMockBalancer()
	checker := NewBalanceChecker(config, mockBalancer)

	// Initially should return unknown status (no balance info)
	balance := checker.GetBalance("https://api1.example.com")
	if balance == nil {
		t.Fatal("Expected balance info, got nil")
	}
	if balance.Status != "unknown" {
		t.Errorf("Expected status unknown for server with no balance info, got %s", balance.Status)
	}

	// Add some balance info manually
	checker.balances["https://api1.example.com"] = &BalanceInfo{
		Balance:     150.75,
		LastChecked: time.Now(),
		Status:      "success",
	}

	// Should now return the balance info
	balance = checker.GetBalance("https://api1.example.com")
	if balance == nil {
		t.Fatal("Expected balance info, got nil")
	}

	if balance.Balance != 150.75 {
		t.Errorf("Expected balance 150.75, got %f", balance.Balance)
	}

	if balance.Status != "success" {
		t.Errorf("Expected status success, got %s", balance.Status)
	}

	// Test non-existent server
	balance = checker.GetBalance("https://nonexistent.example.com")
	if balance == nil {
		t.Fatal("Expected balance info, got nil")
	}
	if balance.Status != "unknown" {
		t.Errorf("Expected status unknown for non-existent server, got %s", balance.Status)
	}
}

func TestBalanceCheckerGetAllBalances(t *testing.T) {
	config := types.Config{
		Servers: []types.UpstreamServer{
			{URL: "https://api1.example.com", Token: "token1"},
			{URL: "https://api2.example.com", Token: "token2"},
		},
	}

	mockBalancer := NewMockBalancer()
	checker := NewBalanceChecker(config, mockBalancer)

	// Initially should return empty map
	allBalances := checker.GetAllBalances()
	if len(allBalances) != 0 {
		t.Errorf("Expected empty balances map, got %d entries", len(allBalances))
	}

	// Add balance info for servers
	checker.balances["https://api1.example.com"] = &BalanceInfo{
		Balance:     100.0,
		LastChecked: time.Now(),
		Status:      "success",
	}

	checker.balances["https://api2.example.com"] = &BalanceInfo{
		Balance:     50.0,
		LastChecked: time.Now(),
		Status:      "success",
	}

	// Should return all balance info
	allBalances = checker.GetAllBalances()
	if len(allBalances) != 2 {
		t.Errorf("Expected 2 balance entries, got %d", len(allBalances))
	}

	if allBalances["https://api1.example.com"].Balance != 100.0 {
		t.Errorf("Expected balance 100.0 for api1, got %f", allBalances["https://api1.example.com"].Balance)
	}

	if allBalances["https://api2.example.com"].Balance != 50.0 {
		t.Errorf("Expected balance 50.0 for api2, got %f", allBalances["https://api2.example.com"].Balance)
	}
}

func TestBalanceCheckerStart(t *testing.T) {
	tests := []struct {
		name                string
		servers             []types.UpstreamServer
		expectedLogMessage  string
	}{
		{
			name:                "no servers with balance check",
			servers:             []types.UpstreamServer{
				{URL: "https://api1.example.com", Token: "token1"},
			},
			expectedLogMessage:  "Balance check disabled",
		},
		{
			name:                "servers with balance check",
			servers:             []types.UpstreamServer{
				{
					URL:          "https://api1.example.com",
					Token:        "token1",
					BalanceCheck: "echo 100",
				},
			},
			expectedLogMessage:  "Balance checker started",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := types.Config{
				Servers: tt.servers,
			}

			mockBalancer := NewMockBalancer()
			checker := NewBalanceChecker(config, mockBalancer)

			// Start should not panic or hang
			checker.Start()

			// For servers with balance check, timers should be created
			if len(tt.servers) > 0 && tt.servers[0].BalanceCheck != "" {
				// Give a small delay for timer creation
				time.Sleep(10 * time.Millisecond)
				
				// Check that timer was created (indirectly by checking serverTimers map is not empty)
				if len(checker.serverTimers) == 0 {
					t.Error("Expected server timers to be created")
				}
			}
		})
	}
}

func TestBalanceCheckerStop(t *testing.T) {
	config := types.Config{
		Servers: []types.UpstreamServer{
			{
				URL:          "https://api1.example.com",
				Token:        "token1",
				BalanceCheck: "echo 100",
			},
		},
	}

	mockBalancer := NewMockBalancer()
	checker := NewBalanceChecker(config, mockBalancer)

	// Start the checker
	checker.Start()

	// Stop should not panic
	checker.Stop()

	// After stop, stopChan should be closed
	select {
	case <-checker.stopChan:
		// Expected - channel is closed
	default:
		t.Error("Stop channel should be closed after Stop()")
	}
}

func TestExecuteBalanceCommand(t *testing.T) {
	config := types.Config{}
	mockBalancer := NewMockBalancer()
	checker := NewBalanceChecker(config, mockBalancer)

	tests := []struct {
		name        string
		command     string
		expectError bool
		expectedVal float64
	}{
		{
			name:        "valid command with integer",
			command:     "echo 100",
			expectError: false,
			expectedVal: 100.0,
		},
		{
			name:        "valid command with float",
			command:     "echo 150.75",
			expectError: false,
			expectedVal: 150.75,
		},
		{
			name:        "invalid command",
			command:     "nonexistent_command_12345",
			expectError: true,
		},
		{
			name:        "command with non-numeric output",
			command:     "echo hello",
			expectError: true,
		},
		{
			name:        "command with empty output",
			command:     "echo ''",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := checker.executeBalanceCommand(tt.command)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if result != tt.expectedVal {
					t.Errorf("Expected result %f, got %f", tt.expectedVal, result)
				}
			}
		})
	}
}

func TestBalanceInfo(t *testing.T) {
	now := time.Now()
	
	balanceInfo := &BalanceInfo{
		Balance:     123.45,
		LastChecked: now,
		Status:      "success",
		Error:       "",
	}

	if balanceInfo.Balance != 123.45 {
		t.Errorf("Expected balance 123.45, got %f", balanceInfo.Balance)
	}

	if balanceInfo.LastChecked != now {
		t.Error("LastChecked time mismatch")
	}

	if balanceInfo.Status != "success" {
		t.Errorf("Expected status success, got %s", balanceInfo.Status)
	}

	if balanceInfo.Error != "" {
		t.Errorf("Expected empty error, got %s", balanceInfo.Error)
	}
}

func TestBalanceInfoWithError(t *testing.T) {
	balanceInfo := &BalanceInfo{
		Balance:     0,
		LastChecked: time.Now(),
		Status:      "error",
		Error:       "Command failed",
	}

	if balanceInfo.Status != "error" {
		t.Errorf("Expected status error, got %s", balanceInfo.Status)
	}

	if balanceInfo.Error != "Command failed" {
		t.Errorf("Expected error 'Command failed', got %s", balanceInfo.Error)
	}

	if balanceInfo.Balance != 0 {
		t.Errorf("Expected balance 0 for error case, got %f", balanceInfo.Balance)
	}
}

func TestBalanceCheckerConcurrency(t *testing.T) {
	config := types.Config{
		Servers: []types.UpstreamServer{
			{URL: "https://api1.example.com", Token: "token1"},
			{URL: "https://api2.example.com", Token: "token2"},
		},
	}

	mockBalancer := NewMockBalancer()
	checker := NewBalanceChecker(config, mockBalancer)

	// Test concurrent access to GetBalance and GetAllBalances
	done := make(chan bool, 4)

	// Goroutine 1: GetBalance
	go func() {
		defer func() { done <- true }()
		for i := 0; i < 10; i++ {
			checker.GetBalance("https://api1.example.com")
		}
	}()

	// Goroutine 2: GetAllBalances
	go func() {
		defer func() { done <- true }()
		for i := 0; i < 10; i++ {
			checker.GetAllBalances()
		}
	}()

	// Goroutine 3: Simulate balance updates
	go func() {
		defer func() { done <- true }()
		for i := 0; i < 10; i++ {
			checker.balances["https://api1.example.com"] = &BalanceInfo{
				Balance:     float64(i),
				LastChecked: time.Now(),
				Status:      "success",
			}
		}
	}()

	// Goroutine 4: More GetBalance calls
	go func() {
		defer func() { done <- true }()
		for i := 0; i < 10; i++ {
			checker.GetBalance("https://api2.example.com")
		}
	}()

	// Wait for all goroutines to complete
	for i := 0; i < 4; i++ {
		<-done
	}

	// If we get here without hanging or panicking, the test passes
}