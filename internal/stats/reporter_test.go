package stats

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestNew(t *testing.T) {
	reporter := New()

	if reporter == nil {
		t.Fatal("New() returned nil")
	}

	if reporter.requestCount != 0 {
		t.Errorf("Expected initial request count 0, got %d", reporter.requestCount)
	}

	if reporter.errorCount != 0 {
		t.Errorf("Expected initial error count 0, got %d", reporter.errorCount)
	}

	if reporter.totalResponseTime != 0 {
		t.Errorf("Expected initial response time 0, got %d", reporter.totalResponseTime)
	}

	if reporter.requestCountByServer == nil {
		t.Error("requestCountByServer should be initialized")
	}

	if reporter.responseTimeByServer == nil {
		t.Error("responseTimeByServer should be initialized")
	}
}

func TestIncrementRequestCount(t *testing.T) {
	reporter := New()

	// Initial count should be 0
	if reporter.requestCount != 0 {
		t.Errorf("Expected initial request count 0, got %d", reporter.requestCount)
	}

	// Increment once
	reporter.IncrementRequestCount()
	if reporter.requestCount != 1 {
		t.Errorf("Expected request count 1, got %d", reporter.requestCount)
	}

	// Increment multiple times
	for range 10 {
		reporter.IncrementRequestCount()
	}

	if reporter.requestCount != 11 {
		t.Errorf("Expected request count 11, got %d", reporter.requestCount)
	}
}

func TestIncrementErrorCount(t *testing.T) {
	reporter := New()

	// Initial count should be 0
	if reporter.errorCount != 0 {
		t.Errorf("Expected initial error count 0, got %d", reporter.errorCount)
	}

	// Increment once
	reporter.IncrementErrorCount()
	if reporter.errorCount != 1 {
		t.Errorf("Expected error count 1, got %d", reporter.errorCount)
	}

	// Increment multiple times
	for range 5 {
		reporter.IncrementErrorCount()
	}

	if reporter.errorCount != 6 {
		t.Errorf("Expected error count 6, got %d", reporter.errorCount)
	}
}

func TestAddResponseTime(t *testing.T) {
	reporter := New()

	// Initial response time should be 0
	if reporter.totalResponseTime != 0 {
		t.Errorf("Expected initial response time 0, got %d", reporter.totalResponseTime)
	}

	// Add response time
	reporter.AddResponseTime(100)
	if reporter.totalResponseTime != 100 {
		t.Errorf("Expected total response time 100, got %d", reporter.totalResponseTime)
	}

	// Add more response times
	reporter.AddResponseTime(50)
	reporter.AddResponseTime(150)

	expected := int64(100 + 50 + 150)
	if reporter.totalResponseTime != expected {
		t.Errorf("Expected total response time %d, got %d", expected, reporter.totalResponseTime)
	}
}

func TestAddServerStats(t *testing.T) {
	reporter := New()

	serverURL := "http://test-api.local"
	responseTime := int64(250)

	// Add server stats
	reporter.AddServerStats(serverURL, responseTime)

	// Check request count for server
	if reporter.requestCountByServer[serverURL] != 1 {
		t.Errorf("Expected request count 1 for server, got %d", reporter.requestCountByServer[serverURL])
	}

	// Check response time for server
	if reporter.responseTimeByServer[serverURL] != responseTime {
		t.Errorf("Expected response time %d for server, got %d", responseTime, reporter.responseTimeByServer[serverURL])
	}

	// Add more stats for same server
	reporter.AddServerStats(serverURL, 150)

	if reporter.requestCountByServer[serverURL] != 2 {
		t.Errorf("Expected request count 2 for server, got %d", reporter.requestCountByServer[serverURL])
	}

	expectedTotalTime := responseTime + 150
	if reporter.responseTimeByServer[serverURL] != expectedTotalTime {
		t.Errorf("Expected total response time %d for server, got %d", expectedTotalTime, reporter.responseTimeByServer[serverURL])
	}

	// Add stats for different server
	server2URL := "http://test-api2.local"
	reporter.AddServerStats(server2URL, 300)

	if reporter.requestCountByServer[server2URL] != 1 {
		t.Errorf("Expected request count 1 for server2, got %d", reporter.requestCountByServer[server2URL])
	}

	if reporter.responseTimeByServer[server2URL] != 300 {
		t.Errorf("Expected response time 300 for server2, got %d", reporter.responseTimeByServer[server2URL])
	}
}

func TestLogStats(t *testing.T) {
	reporter := New()

	// Add some test data
	reporter.IncrementRequestCount()
	reporter.IncrementRequestCount()
	reporter.IncrementErrorCount()
	reporter.AddResponseTime(100)
	reporter.AddResponseTime(200)
	reporter.AddServerStats("http://test-api.local", 150)
	reporter.AddServerStats("http://test-api.local", 250)

	// LogStats should not panic
	reporter.LogStats()

	// We can't easily test the log output, but we can ensure it doesn't crash
}

func TestStartReporter(t *testing.T) {
	reporter := New()

	// StartReporter should not panic when called
	// Note: This starts a goroutine that runs indefinitely, so we can't easily test its behavior
	// In a real test environment, you might want to add a way to stop the reporter
	go reporter.StartReporter()

	// Give it a moment to start
	time.Sleep(10 * time.Millisecond)

	// If we reach here without panic, the test passes
}

func TestGinLoggerMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	reporter := New()

	tests := []struct {
		name           string
		method         string
		path           string
		query          string
		statusCode     int
		expectedStatus int
	}{
		{
			name:           "successful request",
			method:         "GET",
			path:           "/api/test",
			query:          "",
			statusCode:     200,
			expectedStatus: 200,
		},
		{
			name:           "request with query params",
			method:         "POST",
			path:           "/api/data",
			query:          "param1=value1&param2=value2",
			statusCode:     201,
			expectedStatus: 201,
		},
		{
			name:           "client error",
			method:         "GET",
			path:           "/api/notfound",
			query:          "",
			statusCode:     404,
			expectedStatus: 404,
		},
		{
			name:           "server error",
			method:         "POST",
			path:           "/api/error",
			query:          "",
			statusCode:     500,
			expectedStatus: 500,
		},
		{
			name:           "health check",
			method:         "GET",
			path:           "/health",
			query:          "",
			statusCode:     200,
			expectedStatus: 200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create Gin router
			router := gin.New()

			// Add the logger middleware
			router.Use(reporter.GinLoggerMiddleware())

			// Add test handler that returns the specified status code
			router.Any("/*path", func(c *gin.Context) {
				c.Status(tt.statusCode)
			})

			// Create request
			var requestURL string
			if tt.query != "" {
				requestURL = tt.path + "?" + tt.query
			} else {
				requestURL = tt.path
			}

			req, _ := http.NewRequest(tt.method, requestURL, nil)
			w := httptest.NewRecorder()

			// Perform request
			router.ServeHTTP(w, req)

			// Check status code
			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}
		})
	}
}

func TestGinLoggerMiddlewareHealthCheck(t *testing.T) {
	gin.SetMode(gin.TestMode)

	reporter := New()

	// Create router with middleware
	router := gin.New()
	router.Use(reporter.GinLoggerMiddleware())

	// Add health endpoint
	router.GET("/health", func(c *gin.Context) {
		c.Status(200)
	})

	// Make health check request
	req, _ := http.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Should return 200
	if w.Code != 200 {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Health check requests should not be logged (no log output expected)
	// This is hard to test directly, but the middleware should handle it without error
}

func TestReporterConcurrency(t *testing.T) {
	reporter := New()

	// Test concurrent access to reporter methods
	done := make(chan bool, 6)

	// Goroutine 1: Increment request count
	go func() {
		defer func() { done <- true }()
		for range 100 {
			reporter.IncrementRequestCount()
		}
	}()

	// Goroutine 2: Increment error count
	go func() {
		defer func() { done <- true }()
		for range 50 {
			reporter.IncrementErrorCount()
		}
	}()

	// Goroutine 3: Add response times
	go func() {
		defer func() { done <- true }()
		for i := range 100 {
			reporter.AddResponseTime(int64(i))
		}
	}()

	// Goroutine 4: Add server stats for server 1
	go func() {
		defer func() { done <- true }()
		for i := range 50 {
			reporter.AddServerStats("http://test-api1.local", int64(i*10))
		}
	}()

	// Goroutine 5: Add server stats for server 2
	go func() {
		defer func() { done <- true }()
		for i := range 50 {
			reporter.AddServerStats("http://test-api2.local", int64(i*20))
		}
	}()

	// Goroutine 6: Log stats
	go func() {
		defer func() { done <- true }()
		for range 10 {
			reporter.LogStats()
			time.Sleep(1 * time.Millisecond)
		}
	}()

	// Wait for all goroutines to complete
	for i := 0; i < 6; i++ {
		<-done
	}

	// Verify final counts
	if reporter.requestCount != 100 {
		t.Errorf("Expected request count 100, got %d", reporter.requestCount)
	}

	if reporter.errorCount != 50 {
		t.Errorf("Expected error count 50, got %d", reporter.errorCount)
	}

	if reporter.requestCountByServer["http://test-api1.local"] != 50 {
		t.Errorf("Expected server1 request count 50, got %d", reporter.requestCountByServer["http://test-api1.local"])
	}

	if reporter.requestCountByServer["http://test-api2.local"] != 50 {
		t.Errorf("Expected server2 request count 50, got %d", reporter.requestCountByServer["http://test-api2.local"])
	}
}
