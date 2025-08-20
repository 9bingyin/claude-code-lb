package proxy

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"claude-code-lb/internal/balance"
	"claude-code-lb/internal/stats"
	"claude-code-lb/pkg/types"

	"github.com/gin-gonic/gin"
)



func TestGetHopByHopHeaders(t *testing.T) {
	tests := []struct {
		name             string
		connectionHeader string
		expectedHeaders  []string
	}{
		{
			name:             "empty connection header",
			connectionHeader: "",
			expectedHeaders:  []string{"connection", "keep-alive", "proxy-authenticate", "proxy-authorization", "te", "trailers", "transfer-encoding", "upgrade"},
		},
		{
			name:             "connection header with custom headers",
			connectionHeader: "close, x-custom-header",
			expectedHeaders:  []string{"connection", "keep-alive", "proxy-authenticate", "proxy-authorization", "te", "trailers", "transfer-encoding", "upgrade", "x-custom-header"},
		},
		{
			name:             "connection header with keep-alive",
			connectionHeader: "keep-alive",
			expectedHeaders:  []string{"connection", "keep-alive", "proxy-authenticate", "proxy-authorization", "te", "trailers", "transfer-encoding", "upgrade"},
		},
		{
			name:             "connection header with multiple custom headers",
			connectionHeader: "x-header1, x-header2, close",
			expectedHeaders:  []string{"connection", "keep-alive", "proxy-authenticate", "proxy-authorization", "te", "trailers", "transfer-encoding", "upgrade", "x-header1", "x-header2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headers := getHopByHopHeaders(tt.connectionHeader)
			
			// Check that all expected headers are present
			for _, expectedHeader := range tt.expectedHeaders {
				if !headers[expectedHeader] {
					t.Errorf("Expected header %s to be in hop-by-hop headers", expectedHeader)
				}
			}
		})
	}
}

func TestFormatRequestURL(t *testing.T) {
	tests := []struct {
		name      string
		method    string
		serverURL string
		path      string
		query     string
		expected  string
	}{
		{
			name:      "simple GET request",
			method:    "GET",
			serverURL: "https://api.example.com",
			path:      "/v1/messages",
			query:     "",
			expected:  "GET https://api.example.com/v1/messages",
		},
		{
			name:      "POST request with query",
			method:    "POST",
			serverURL: "https://api.example.com",
			path:      "/v1/chat",
			query:     "model=claude-3",
			expected:  "POST https://api.example.com/v1/chat?model=claude-3",
		},
		{
			name:      "URL with double slashes",
			method:    "GET",
			serverURL: "https://api.example.com/",
			path:      "/v1/messages",
			query:     "",
			expected:  "GET https://api.example.com/v1/messages",
		},
		{
			name:      "HTTP URL",
			method:    "PUT",
			serverURL: "http://localhost:8080",
			path:      "/api/test",
			query:     "debug=true",
			expected:  "PUT http://localhost:8080/api/test?debug=true",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatRequestURL(tt.method, tt.serverURL, tt.path, tt.query)
			if result != tt.expected {
				t.Errorf("formatRequestURL() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestParseUsageInfo(t *testing.T) {
	tests := []struct {
		name         string
		responseBody []byte
		contentType  string
		expectedModel string
		expectedUsage types.ClaudeUsage
		expectSuccess bool
	}{
		{
			name: "valid JSON response",
			responseBody: []byte(`{
				"model": "claude-3-sonnet-20240229",
				"usage": {
					"input_tokens": 100,
					"output_tokens": 50,
					"cache_creation_input_tokens": 10,
					"cache_read_input_tokens": 5
				}
			}`),
			contentType: "application/json",
			expectedModel: "claude-3-sonnet-20240229",
			expectedUsage: types.ClaudeUsage{
				InputTokens:              100,
				OutputTokens:             50,
				CacheCreationInputTokens: 10,
				CacheReadInputTokens:     5,
			},
			expectSuccess: true,
		},
		{
			name: "SSE response with message_start",
			responseBody: []byte(`event: message_start
data: {"type": "message_start", "message": {"model": "claude-3-haiku", "usage": {"input_tokens": 75, "output_tokens": 25}}}

event: ping
data: {"type": "ping"}

event: message_delta
data: {"type": "message_delta", "usage": {"output_tokens": 30}}

`),
			contentType: "text/event-stream",
			expectedModel: "claude-3-haiku",
			expectedUsage: types.ClaudeUsage{
				InputTokens:  0,  // message_delta overwrites this with 0 (not present in delta)
				OutputTokens: 30, // Should be overwritten by message_delta
			},
			expectSuccess: true,
		},
		{
			name:         "invalid JSON",
			responseBody: []byte(`{invalid json`),
			contentType:  "application/json",
			expectSuccess: false,
		},
		{
			name:         "unsupported content type",
			responseBody: []byte(`some text`),
			contentType:  "text/plain",
			expectSuccess: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model, usage, success := parseUsageInfo(tt.responseBody, tt.contentType)
			
			if success != tt.expectSuccess {
				t.Errorf("parseUsageInfo() success = %v, want %v", success, tt.expectSuccess)
			}
			
			if tt.expectSuccess {
				if model != tt.expectedModel {
					t.Errorf("parseUsageInfo() model = %v, want %v", model, tt.expectedModel)
				}
				
				if usage.InputTokens != tt.expectedUsage.InputTokens {
					t.Errorf("parseUsageInfo() usage.InputTokens = %v, want %v", usage.InputTokens, tt.expectedUsage.InputTokens)
				}
				
				if usage.OutputTokens != tt.expectedUsage.OutputTokens {
					t.Errorf("parseUsageInfo() usage.OutputTokens = %v, want %v", usage.OutputTokens, tt.expectedUsage.OutputTokens)
				}
				
				if usage.CacheCreationInputTokens != tt.expectedUsage.CacheCreationInputTokens {
					t.Errorf("parseUsageInfo() usage.CacheCreationInputTokens = %v, want %v", usage.CacheCreationInputTokens, tt.expectedUsage.CacheCreationInputTokens)
				}
				
				if usage.CacheReadInputTokens != tt.expectedUsage.CacheReadInputTokens {
					t.Errorf("parseUsageInfo() usage.CacheReadInputTokens = %v, want %v", usage.CacheReadInputTokens, tt.expectedUsage.CacheReadInputTokens)
				}
			}
		})
	}
}

func TestHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Create a test upstream server
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := types.ClaudeResponse{
			Model: "claude-3-sonnet",
			Usage: types.ClaudeUsage{
				InputTokens:  100,
				OutputTokens: 50,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(response)
	}))
	defer upstream.Close()

	// Create config with the test server
	config := types.Config{
		Mode:      "load_balance",
		Algorithm: "round_robin",
		Servers: []types.UpstreamServer{
			{URL: upstream.URL, Token: "test-token"},
		},
	}
	
	// Create real balancer
	balancer := balance.New(config)
	statsReporter := stats.New()

	// Create handler
	handler := Handler(balancer, statsReporter, false)

	// Create Gin router
	router := gin.New()
	router.Any("/*path", handler)

	tests := []struct {
		name           string
		method         string
		path           string
		expectedStatus int
	}{
		{
			name:           "successful proxy request",
			method:         "POST",
			path:           "/v1/messages",
			expectedStatus: 200,
		},
		{
			name:           "GET request",
			method:         "GET",
			path:           "/v1/models",
			expectedStatus: 200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest(tt.method, tt.path, bytes.NewBufferString(`{"test": "data"}`))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}
		})
	}
}

func TestHandlerNoAvailableServers(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Create config with no servers
	config := types.Config{
		Mode:      "load_balance",
		Algorithm: "round_robin",
		Servers:   []types.UpstreamServer{},
	}
	
	// Create real balancer with no servers
	balancer := balance.New(config)
	statsReporter := stats.New()

	// Create handler
	handler := Handler(balancer, statsReporter, false)

	// Create Gin router
	router := gin.New()
	router.Any("/*path", handler)

	req, _ := http.NewRequest("POST", "/v1/messages", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Should return 502 Bad Gateway
	if w.Code != 502 {
		t.Errorf("Expected status 502, got %d", w.Code)
	}

	// Check response body
	var response map[string]string
	json.Unmarshal(w.Body.Bytes(), &response)
	if response["error"] != "No available servers" {
		t.Errorf("Expected error message 'No available servers', got %s", response["error"])
	}
}

func TestHandlerUpstreamError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Create a test upstream server that returns 500
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("Internal Server Error"))
	}))
	defer upstream.Close()

	// Create config with the test server
	config := types.Config{
		Mode:      "load_balance",
		Algorithm: "round_robin",
		Servers: []types.UpstreamServer{
			{URL: upstream.URL, Token: "test-token"},
		},
	}
	
	balancer := balance.New(config)
	statsReporter := stats.New()

	// Create handler
	handler := Handler(balancer, statsReporter, false)

	// Create Gin router
	router := gin.New()
	router.Any("/*path", handler)

	req, _ := http.NewRequest("POST", "/v1/messages", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Should return 502 (request failed due to upstream error)
	if w.Code != 502 {
		t.Errorf("Expected status 502, got %d", w.Code)
	}

	// Verify that the server was marked as down in the balancer
	status := balancer.GetServerStatus()
	if status[upstream.URL] {
		t.Error("Expected server to be marked as down")
	}
}

func TestHandlerRateLimited(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Create a test upstream server that returns 429
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		w.Write([]byte("Rate limited"))
	}))
	defer upstream.Close()

	// Create config with the test server
	config := types.Config{
		Mode:      "load_balance",
		Algorithm: "round_robin",
		Servers: []types.UpstreamServer{
			{URL: upstream.URL, Token: "test-token"},
		},
	}
	
	balancer := balance.New(config)
	statsReporter := stats.New()

	// Create handler
	handler := Handler(balancer, statsReporter, false)

	// Create Gin router
	router := gin.New()
	router.Any("/*path", handler)

	req, _ := http.NewRequest("POST", "/v1/messages", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Should return 502 (request failed due to rate limiting)
	if w.Code != 502 {
		t.Errorf("Expected status 502, got %d", w.Code)
	}

	// Verify that the server was marked as down due to rate limiting
	status := balancer.GetServerStatus()
	if status[upstream.URL] {
		t.Error("Expected server to be marked as down due to rate limiting")
	}
}

func TestHandlerDebugMode(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Create a test upstream server
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Test-Header", "test-value")
		w.WriteHeader(200)
		w.Write([]byte("test response"))
	}))
	defer upstream.Close()

	// Create config with the test server
	config := types.Config{
		Mode:      "load_balance",
		Algorithm: "round_robin",
		Servers: []types.UpstreamServer{
			{URL: upstream.URL, Token: "test-token"},
		},
	}
	
	balancer := balance.New(config)
	statsReporter := stats.New()

	// Create handler with debug mode enabled
	handler := Handler(balancer, statsReporter, true)

	// Create Gin router
	router := gin.New()
	router.Any("/*path", handler)

	req, _ := http.NewRequest("POST", "/v1/messages", bytes.NewBufferString(`{"test": "data"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer original-token")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// In debug mode, additional logging should occur (we can't easily test this)
	// but we can verify the request still works correctly
	if !strings.Contains(w.Body.String(), "test response") {
		t.Error("Expected response body to contain 'test response'")
	}
}

func TestHandlerSSEResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Create a test upstream server that returns SSE
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(200)
		
		// Write SSE data
		w.Write([]byte("event: message_start\n"))
		w.Write([]byte(`data: {"type": "message_start", "message": {"model": "claude-3", "usage": {"input_tokens": 10, "output_tokens": 5}}}`))
		w.Write([]byte("\n\n"))
		
		w.Write([]byte("event: message_delta\n"))
		w.Write([]byte(`data: {"type": "message_delta", "usage": {"output_tokens": 15}}`))
		w.Write([]byte("\n\n"))
	}))
	defer upstream.Close()

	// Create config with the test server
	config := types.Config{
		Mode:      "load_balance",
		Algorithm: "round_robin",
		Servers: []types.UpstreamServer{
			{URL: upstream.URL, Token: "test-token"},
		},
	}
	
	balancer := balance.New(config)
	statsReporter := stats.New()

	// Create handler
	handler := Handler(balancer, statsReporter, false)

	// Create Gin router
	router := gin.New()
	router.Any("/*path", handler)

	req, _ := http.NewRequest("POST", "/v1/messages", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Check that SSE headers are preserved
	if w.Header().Get("Content-Type") != "text/event-stream" {
		t.Error("Expected Content-Type to be text/event-stream")
	}

	// Check that SSE data is in response
	body := w.Body.String()
	if !strings.Contains(body, "message_start") {
		t.Error("Expected response to contain message_start event")
	}
	
	if !strings.Contains(body, "message_delta") {
		t.Error("Expected response to contain message_delta event")
	}
}