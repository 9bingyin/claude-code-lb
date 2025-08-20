package testutil

import "fmt"

// CommandExecutor 命令执行器接口
type CommandExecutor interface {
	ExecuteCommand(command string) (float64, error)
}

// MockCommandExecutor 模拟命令执行器
type MockCommandExecutor struct {
	// Results 预定义的命令结果
	Results map[string]float64
	// Errors 预定义的命令错误
	Errors map[string]error
	// CallCount 记录每个命令的调用次数
	CallCount map[string]int
}

// NewMockCommandExecutor 创建新的模拟命令执行器
func NewMockCommandExecutor() *MockCommandExecutor {
	return &MockCommandExecutor{
		Results:   make(map[string]float64),
		Errors:    make(map[string]error),
		CallCount: make(map[string]int),
	}
}

// ExecuteCommand 执行命令（模拟）
func (m *MockCommandExecutor) ExecuteCommand(command string) (float64, error) {
	// 记录调用次数
	m.CallCount[command]++

	// 检查是否有预定义的错误
	if err, exists := m.Errors[command]; exists {
		return 0, err
	}

	// 检查是否有预定义的结果
	if result, exists := m.Results[command]; exists {
		return result, nil
	}

	// 默认处理一些标准命令
	switch command {
	case "echo 100":
		return 100.0, nil
	case "echo 100.50":
		return 100.50, nil
	case "echo 150.75":
		return 150.75, nil
	case "echo 0":
		return 0.0, nil
	default:
		return 0, fmt.Errorf("mock: command not found: %s", command)
	}
}

// SetResult 设置命令的返回结果
func (m *MockCommandExecutor) SetResult(command string, result float64) {
	m.Results[command] = result
}

// SetError 设置命令的返回错误
func (m *MockCommandExecutor) SetError(command string, err error) {
	m.Errors[command] = err
}

// GetCallCount 获取命令的调用次数
func (m *MockCommandExecutor) GetCallCount(command string) int {
	return m.CallCount[command]
}

// Reset 重置所有状态
func (m *MockCommandExecutor) Reset() {
	m.Results = make(map[string]float64)
	m.Errors = make(map[string]error)
	m.CallCount = make(map[string]int)
}
