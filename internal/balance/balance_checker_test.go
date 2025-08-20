package balance

import (
	"errors"
	"sync"
	"testing"
	"time"

	"claude-code-lb/internal/testutil"
	"claude-code-lb/pkg/types"
)

func TestNewBalanceChecker(t *testing.T) {
	config := types.Config{
		Servers: []types.UpstreamServer{
			{
				URL:                  testutil.API1ExampleURL,
				Token:                testutil.TestToken1,
				BalanceCheck:         "echo 100.50",
				BalanceCheckInterval: 300,
				BalanceThreshold:     10.0,
			},
		},
	}

	mockBalancer := testutil.NewMockBalancer()
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

	if checker.commandExecutor == nil {
		t.Error("Command executor should be initialized")
	}
}

func TestBalanceCheckerGetBalance(t *testing.T) {
	config := types.Config{
		Servers: []types.UpstreamServer{
			{URL: testutil.API1ExampleURL, Token: testutil.TestToken1},
		},
	}

	mockBalancer := testutil.NewMockBalancer()
	checker := NewBalanceChecker(config, mockBalancer)

	// Initially should return unknown status (no balance info)
	balance := checker.GetBalance(testutil.API1ExampleURL)
	if balance == nil {
		t.Fatal("Expected balance info, got nil")
	}
	if balance.Status != "unknown" {
		t.Errorf("Expected status unknown for server with no balance info, got %s", balance.Status)
	}

	// Add some balance info manually (thread-safe)
	checker.mutex.Lock()
	checker.balances[testutil.API1ExampleURL] = &BalanceInfo{
		Balance:     150.75,
		LastChecked: time.Now(),
		Status:      "success",
	}
	checker.mutex.Unlock()

	// Should now return the balance info
	balance = checker.GetBalance(testutil.API1ExampleURL)
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
	balance = checker.GetBalance("http://nonexistent.local")
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
			{URL: testutil.API1ExampleURL, Token: testutil.TestToken1},
			{URL: testutil.API2ExampleURL, Token: testutil.TestToken2},
		},
	}

	mockBalancer := testutil.NewMockBalancer()
	checker := NewBalanceChecker(config, mockBalancer)

	// Initially should return empty map
	allBalances := checker.GetAllBalances()
	if len(allBalances) != 0 {
		t.Errorf("Expected empty balances map, got %d entries", len(allBalances))
	}

	// Add balance info for servers (thread-safe)
	checker.mutex.Lock()
	checker.balances[testutil.API1ExampleURL] = &BalanceInfo{
		Balance:     100.0,
		LastChecked: time.Now(),
		Status:      "success",
	}

	checker.balances[testutil.API2ExampleURL] = &BalanceInfo{
		Balance:     50.0,
		LastChecked: time.Now(),
		Status:      "success",
	}
	checker.mutex.Unlock()

	// Should return all balance info
	allBalances = checker.GetAllBalances()
	if len(allBalances) != 2 {
		t.Errorf("Expected 2 balance entries, got %d", len(allBalances))
	}

	if allBalances[testutil.API1ExampleURL].Balance != 100.0 {
		t.Errorf("Expected balance 100.0 for api1, got %f", allBalances[testutil.API1ExampleURL].Balance)
	}

	if allBalances[testutil.API2ExampleURL].Balance != 50.0 {
		t.Errorf("Expected balance 50.0 for api2, got %f", allBalances[testutil.API2ExampleURL].Balance)
	}
}

func TestBalanceCheckerStart(t *testing.T) {
	tests := []struct {
		name               string
		servers            []types.UpstreamServer
		expectedLogMessage string
	}{
		{
			name: "no servers with balance check",
			servers: []types.UpstreamServer{
				{URL: testutil.API1ExampleURL, Token: testutil.TestToken1},
			},
			expectedLogMessage: "Balance check disabled",
		},
		{
			name: "servers with balance check",
			servers: []types.UpstreamServer{
				{
					URL:          testutil.API1ExampleURL,
					Token:        testutil.TestToken1,
					BalanceCheck: "echo 100",
				},
			},
			expectedLogMessage: "Balance checker started",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := types.Config{
				Servers: tt.servers,
			}

			mockBalancer := testutil.NewMockBalancer()
			mockExecutor := testutil.NewMockCommandExecutor()
			checker := NewBalanceCheckerWithExecutor(config, mockBalancer, mockExecutor)

			// Start should not panic or hang
			checker.Start()

			// For servers with balance check, give time for setup
			if len(tt.servers) > 0 && tt.servers[0].BalanceCheck != "" {
				// Give time for the goroutines to start
				time.Sleep(50 * time.Millisecond)
			}

			// Stop to clean up
			checker.Stop()
		})
	}
}

func TestBalanceCheckerStop(t *testing.T) {
	config := types.Config{
		Servers: []types.UpstreamServer{
			{
				URL:          testutil.API1ExampleURL,
				Token:        testutil.TestToken1,
				BalanceCheck: "echo 100",
			},
		},
	}

	mockBalancer := testutil.NewMockBalancer()
	mockExecutor := testutil.NewMockCommandExecutor()
	checker := NewBalanceCheckerWithExecutor(config, mockBalancer, mockExecutor)

	// Start the checker
	checker.Start()

	// Give some time for startup
	time.Sleep(10 * time.Millisecond)

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
	mockBalancer := testutil.NewMockBalancer()
	mockExecutor := testutil.NewMockCommandExecutor()
	checker := NewBalanceCheckerWithExecutor(config, mockBalancer, mockExecutor)

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
			// Set up mock executor expectations
			if tt.expectError {
				mockExecutor.SetError(tt.command, errors.New("mock error"))
			} else {
				mockExecutor.SetResult(tt.command, tt.expectedVal)
			}

			result, err := checker.commandExecutor.ExecuteCommand(tt.command)

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
		Balance: 0,
		Status:  "error",
		Error:   "Command failed",
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
			{URL: testutil.API1ExampleURL, Token: testutil.TestToken1},
			{URL: testutil.API2ExampleURL, Token: testutil.TestToken2},
		},
	}

	mockBalancer := testutil.NewMockBalancer()
	checker := NewBalanceChecker(config, mockBalancer)

	// Use wait group to ensure all goroutines complete
	var wg sync.WaitGroup
	const numGoroutines = 4
	wg.Add(numGoroutines)

	// Goroutine 1: GetBalance
	go func() {
		defer wg.Done()
		for range 10 {
			checker.GetBalance(testutil.API1ExampleURL)
			time.Sleep(1 * time.Millisecond) // Small delay to avoid tight loops
		}
	}()

	// Goroutine 2: GetAllBalances
	go func() {
		defer wg.Done()
		for range 10 {
			checker.GetAllBalances()
			time.Sleep(1 * time.Millisecond)
		}
	}()

	// Goroutine 3: Simulate balance updates (thread-safe)
	go func() {
		defer wg.Done()
		for i := range 10 {
			checker.mutex.Lock()
			checker.balances[testutil.API1ExampleURL] = &BalanceInfo{
				Balance:     float64(i),
				LastChecked: time.Now(),
				Status:      "success",
			}
			checker.mutex.Unlock()
			time.Sleep(1 * time.Millisecond)
		}
	}()

	// Goroutine 4: More GetBalance calls
	go func() {
		defer wg.Done()
		for range 10 {
			checker.GetBalance(testutil.API2ExampleURL)
			time.Sleep(1 * time.Millisecond)
		}
	}()

	// Wait for all goroutines to complete with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All goroutines completed successfully
	case <-time.After(5 * time.Second):
		t.Fatal("Test timed out - possible deadlock")
	}
}

func TestNewBalanceCheckerWithExecutor(t *testing.T) {
	config := types.Config{
		Servers: []types.UpstreamServer{
			{URL: testutil.API1ExampleURL, Token: testutil.TestToken1},
		},
	}

	mockBalancer := testutil.NewMockBalancer()
	mockExecutor := testutil.NewMockCommandExecutor()
	checker := NewBalanceCheckerWithExecutor(config, mockBalancer, mockExecutor)

	if checker == nil {
		t.Fatal("NewBalanceCheckerWithExecutor returned nil")
	}

	if checker.commandExecutor != mockExecutor {
		t.Error("Custom command executor not properly set")
	}
}

// ==============================================
// 向后兼容性和集成测试（从原独立测试文件合并）
// ==============================================

// TestBackwardCompatibilitySupport 验证向后兼容性
func TestBackwardCompatibilitySupport(t *testing.T) {
	config := types.Config{
		Servers: []types.UpstreamServer{
			{
				URL:                  testutil.API1ExampleURL,
				Token:                testutil.TestToken1,
				BalanceCheck:         "echo 100.50",
				BalanceCheckInterval: 300,
				BalanceThreshold:     10.0,
			},
		},
	}

	mockBalancer := testutil.NewMockBalancer()

	// 测试原有构造函数
	checker := NewBalanceChecker(config, mockBalancer)

	// 验证所有字段都正确初始化
	if checker == nil {
		t.Fatal("NewBalanceChecker returned nil")
	}

	if checker.commandExecutor == nil {
		t.Fatal("CommandExecutor should be automatically initialized")
	}

	// 验证默认执行器类型
	if _, ok := checker.commandExecutor.(*DefaultCommandExecutor); !ok {
		t.Error("Default executor should be DefaultCommandExecutor")
	}

	// 验证其他字段与之前一致
	if checker.commandTimeout != 30*time.Second {
		t.Errorf("Expected command timeout 30s, got %v", checker.commandTimeout)
	}

	if checker.balances == nil {
		t.Error("Balances map should be initialized")
	}

	if checker.serverTimers == nil {
		t.Error("Server timers map should be initialized")
	}

	if checker.stopChan == nil {
		t.Error("Stop channel should be initialized")
	}
}

// TestDefaultExecutorFunctionality 验证默认执行器行为与原来一致
func TestDefaultExecutorFunctionality(t *testing.T) {
	executor := &DefaultCommandExecutor{Timeout: 30 * time.Second}

	// 测试简单命令执行
	result, err := executor.ExecuteCommand("echo 42")
	if err != nil {
		t.Fatalf("Default executor failed: %v", err)
	}

	if result != 42.0 {
		t.Errorf("Expected result 42.0, got %f", result)
	}

	// 测试浮点数命令
	result, err = executor.ExecuteCommand("echo 123.45")
	if err != nil {
		t.Fatalf("Default executor failed: %v", err)
	}

	if result != 123.45 {
		t.Errorf("Expected result 123.45, got %f", result)
	}

	// 测试错误命令
	_, err = executor.ExecuteCommand("nonexistent_command_xyz")
	if err == nil {
		t.Error("Expected error for invalid command")
	}
}

// TestEndToEndFunctionality 测试真实功能集成
func TestEndToEndFunctionality(t *testing.T) {
	config := types.Config{
		Servers: []types.UpstreamServer{
			{
				URL:                  testutil.API1ExampleURL,
				Token:                testutil.TestToken1,
				BalanceCheck:         "echo 88.88", // 使用真实的shell命令
				BalanceCheckInterval: 1,            // 1秒间隔，便于测试
				BalanceThreshold:     10.0,
			},
		},
	}

	mockBalancer := testutil.NewMockBalancer()

	// 使用原有构造函数（这会使用DefaultCommandExecutor）
	checker := NewBalanceChecker(config, mockBalancer)

	// 启动检查器
	checker.Start()

	// 等待一次检查完成
	time.Sleep(200 * time.Millisecond)

	// 验证余额信息被正确获取
	balance := checker.GetBalance(testutil.API1ExampleURL)
	if balance == nil {
		t.Fatal("Expected balance info, got nil")
	}

	if balance.Status != "success" {
		t.Errorf("Expected status success, got %s", balance.Status)
	}

	if balance.Balance != 88.88 {
		t.Errorf("Expected balance 88.88, got %f", balance.Balance)
	}

	// 停止检查器
	checker.Stop()

	// 验证停止后不会继续更新
	oldTime := balance.LastChecked
	time.Sleep(100 * time.Millisecond)

	newBalance := checker.GetBalance(testutil.API1ExampleURL)
	if !newBalance.LastChecked.Equal(oldTime) {
		t.Error("Balance should not be updated after Stop()")
	}
}

// TestCompleteAPIFunctionality 测试所有原有API都正常工作
func TestCompleteAPIFunctionality(t *testing.T) {
	config := types.Config{
		Servers: []types.UpstreamServer{
			{URL: testutil.API1ExampleURL, Token: testutil.TestToken1},
			{URL: testutil.API2ExampleURL, Token: testutil.TestToken2},
		},
	}

	mockBalancer := testutil.NewMockBalancer()
	checker := NewBalanceChecker(config, mockBalancer)

	// 测试所有公共方法都存在且工作

	// 1. GetBalance
	balance := checker.GetBalance(testutil.API1ExampleURL)
	if balance == nil {
		t.Error("GetBalance should return non-nil result")
	}

	// 2. GetAllBalances
	allBalances := checker.GetAllBalances()
	if allBalances == nil {
		t.Error("GetAllBalances should return non-nil result")
	}

	// 3. Start (should not panic)
	checker.Start()

	// 4. Stop (should not panic)
	checker.Stop()
}
