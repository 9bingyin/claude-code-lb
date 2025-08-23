package logger

import (
	"bytes"
	"log"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSetDebugMode(t *testing.T) {
	// Test enabling debug mode
	SetDebugMode(true)
	if !debugEnabled {
		t.Error("Expected debug mode to be enabled")
	}

	// Test disabling debug mode
	SetDebugMode(false)
	if debugEnabled {
		t.Error("Expected debug mode to be disabled")
	}
}

func TestFormatTimestamp(t *testing.T) {
	timestamp := formatTimestamp()

	// Check total length with color codes
	if len(timestamp) != 21 {
		t.Errorf("Expected timestamp length 21 (with colors), got %d", len(timestamp))
	}

	// Check color codes
	if !strings.HasPrefix(timestamp, ColorGray) || !strings.HasSuffix(timestamp, ColorReset) {
		t.Errorf("Timestamp should have gray color codes: %s", timestamp)
	}

	// Extract plain timestamp (remove color codes)
	plainTimestamp := strings.TrimPrefix(timestamp, ColorGray)
	plainTimestamp = strings.TrimSuffix(plainTimestamp, ColorReset)

	// Check plain timestamp format - should be HH:MM:SS.mmm
	if len(plainTimestamp) != 12 {
		t.Errorf("Expected plain timestamp length 12, got %d", len(plainTimestamp))
	}

	// Check format structure
	if plainTimestamp[2] != ':' || plainTimestamp[5] != ':' || plainTimestamp[8] != '.' {
		t.Errorf("Timestamp format incorrect: %s", plainTimestamp)
	}

	// Parse to verify it's a valid time
	_, err := time.Parse("15:04:05.000", plainTimestamp)
	if err != nil {
		t.Errorf("Invalid timestamp format: %s, error: %v", plainTimestamp, err)
	}
}

func captureLogOutput(f func()) string {
	var buf bytes.Buffer
	originalOutput := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(originalOutput) // Restore original output
	f()
	return buf.String()
}

func TestInfo(t *testing.T) {
	output := captureLogOutput(func() {
		Info("TEST", "Test message")
	})

	if !strings.Contains(output, " TEST") {
		t.Errorf("Expected output to contain ' TEST', got: %s", output)
	}
	if !strings.Contains(output, "Test message") {
		t.Error("Expected output to contain 'Test message'")
	}
	// Note: Color codes are present but may not be visible in captured output
}

func TestInfoWithArgs(t *testing.T) {
	output := captureLogOutput(func() {
		Info("TEST", "Message %s %d", "hello", 42)
	})

	if !strings.Contains(output, "Message hello 42") {
		t.Error("Expected formatted message not found in output")
	}
}

func TestSuccess(t *testing.T) {
	output := captureLogOutput(func() {
		Success("TEST", "Success message")
	})

	if !strings.Contains(output, " TEST") {
		t.Errorf("Expected output to contain ' TEST', got: %s", output)
	}
	if !strings.Contains(output, "Success message") {
		t.Error("Expected output to contain 'Success message'")
	}
}

func TestWarning(t *testing.T) {
	output := captureLogOutput(func() {
		Warning("TEST", "Warning message")
	})

	if !strings.Contains(output, " TEST") {
		t.Errorf("Expected output to contain ' TEST', got: %s", output)
	}
	if !strings.Contains(output, "Warning message") {
		t.Error("Expected output to contain 'Warning message'")
	}
}

func TestError(t *testing.T) {
	output := captureLogOutput(func() {
		Error("TEST", "Error message")
	})

	if !strings.Contains(output, " TEST") {
		t.Errorf("Expected output to contain ' TEST', got: %s", output)
	}
	if !strings.Contains(output, "Error message") {
		t.Error("Expected output to contain 'Error message'")
	}
}

func TestAuth(t *testing.T) {
	tests := []struct {
		name    string
		success bool
		color   string
	}{
		{
			name:    "successful auth",
			success: true,
			color:   ColorGreen,
		},
		{
			name:    "failed auth",
			success: false,
			color:   ColorRed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := captureLogOutput(func() {
				Auth(tt.success, "Auth message")
			})

			if !strings.Contains(output, " AUTH") {
				t.Errorf("Expected output to contain ' AUTH', got: %s", output)
			}
			if !strings.Contains(output, "Auth message") {
				t.Error("Expected output to contain 'Auth message'")
			}
		})
	}
}

func TestDebug(t *testing.T) {
	tests := []struct {
		name          string
		debugEnabled  bool
		shouldContain bool
	}{
		{
			name:          "debug enabled",
			debugEnabled:  true,
			shouldContain: true,
		},
		{
			name:          "debug disabled",
			debugEnabled:  false,
			shouldContain: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			SetDebugMode(tt.debugEnabled)

			output := captureLogOutput(func() {
				Debug("TEST", "Debug message")
			})

			if tt.shouldContain {
				if !strings.Contains(output, " TEST") {
					t.Errorf("Expected output to contain ' TEST', got: %s", output)
				}
				if !strings.Contains(output, "Debug message") {
					t.Error("Expected output to contain 'Debug message'")
				}
			} else {
				if strings.Contains(output, "Debug message") {
					t.Error("Expected no debug output when debug is disabled")
				}
			}
		})
	}
}

func TestColorConstants(t *testing.T) {
	expectedColors := map[string]string{
		"ColorReset":  "\033[0m",
		"ColorRed":    "\033[31m",
		"ColorGreen":  "\033[32m",
		"ColorYellow": "\033[33m",
		"ColorBlue":   "\033[34m",
		"ColorPurple": "\033[35m",
		"ColorCyan":   "\033[36m",
		"ColorGray":   "\033[37m",
		"ColorBold":   "\033[1m",
	}

	tests := []struct {
		name     string
		actual   string
		expected string
	}{
		{"ColorReset", ColorReset, expectedColors["ColorReset"]},
		{"ColorRed", ColorRed, expectedColors["ColorRed"]},
		{"ColorGreen", ColorGreen, expectedColors["ColorGreen"]},
		{"ColorYellow", ColorYellow, expectedColors["ColorYellow"]},
		{"ColorBlue", ColorBlue, expectedColors["ColorBlue"]},
		{"ColorPurple", ColorPurple, expectedColors["ColorPurple"]},
		{"ColorCyan", ColorCyan, expectedColors["ColorCyan"]},
		{"ColorGray", ColorGray, expectedColors["ColorGray"]},
		{"ColorBold", ColorBold, expectedColors["ColorBold"]},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.actual != tt.expected {
				t.Errorf("Color constant %s: got %q, want %q", tt.name, tt.actual, tt.expected)
			}
		})
	}
}

func TestConcurrentLogging(t *testing.T) {
	// Test that concurrent logging doesn't cause race conditions
	// Capture output to avoid spamming test logs
	originalOutput := log.Writer()
	defer log.SetOutput(originalOutput)
	log.SetOutput(os.Stderr) // Use stderr to avoid buffer issues

	done := make(chan bool, 4)

	for i := 0; i < 4; i++ {
		go func(id int) {
			for j := 0; j < 10; j++ {
				Info("CONC", "Message from goroutine %d iteration %d", id, j)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 4; i++ {
		<-done
	}

	// If we reach here without hanging or panicking, the test passes
}
