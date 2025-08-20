package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"claude-code-lb/pkg/types"

	"github.com/gin-gonic/gin"
)

func TestMinMax(t *testing.T) {
	tests := []struct {
		name   string
		a, b   int
		minExp int
		maxExp int
	}{
		{"a < b", 3, 5, 3, 5},
		{"a > b", 7, 2, 2, 7},
		{"a == b", 4, 4, 4, 4},
		{"negative numbers", -3, -1, -3, -1},
		{"zero and positive", 0, 5, 0, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			minResult := min(tt.a, tt.b)
			maxResult := max(tt.a, tt.b)

			if minResult != tt.minExp {
				t.Errorf("min(%d, %d) = %d, want %d", tt.a, tt.b, minResult, tt.minExp)
			}
			if maxResult != tt.maxExp {
				t.Errorf("max(%d, %d) = %d, want %d", tt.a, tt.b, maxResult, tt.maxExp)
			}
		})
	}
}

func TestMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		config         types.Config
		authHeader     string
		expectedStatus int
		expectedBody   string
	}{
		{
			name: "auth disabled - should pass through",
			config: types.Config{
				Auth: false,
			},
			authHeader:     "",
			expectedStatus: http.StatusOK,
			expectedBody:   "success",
		},
		{
			name: "auth enabled - missing header",
			config: types.Config{
				Auth:     true,
				AuthKeys: []string{"valid-key"},
			},
			authHeader:     "",
			expectedStatus: http.StatusUnauthorized,
			expectedBody:   `{"error":"Missing Authorization header"}`,
		},
		{
			name: "auth enabled - invalid header format",
			config: types.Config{
				Auth:     true,
				AuthKeys: []string{"valid-key"},
			},
			authHeader:     "Invalid header",
			expectedStatus: http.StatusUnauthorized,
			expectedBody:   `{"error":"Invalid Authorization header format"}`,
		},
		{
			name: "auth enabled - invalid key",
			config: types.Config{
				Auth:     true,
				AuthKeys: []string{"valid-key"},
			},
			authHeader:     "Bearer invalid-key",
			expectedStatus: http.StatusUnauthorized,
			expectedBody:   `{"error":"Invalid API key"}`,
		},
		{
			name: "auth enabled - valid key",
			config: types.Config{
				Auth:     true,
				AuthKeys: []string{"valid-key", "another-key"},
			},
			authHeader:     "Bearer valid-key",
			expectedStatus: http.StatusOK,
			expectedBody:   "success",
		},
		{
			name: "auth enabled - valid key from multiple keys",
			config: types.Config{
				Auth:     true,
				AuthKeys: []string{"key1", "key2", "key3"},
			},
			authHeader:     "Bearer key2",
			expectedStatus: http.StatusOK,
			expectedBody:   "success",
		},
		{
			name: "auth enabled - short key truncation",
			config: types.Config{
				Auth:     true,
				AuthKeys: []string{"short"},
			},
			authHeader:     "Bearer invalid",
			expectedStatus: http.StatusUnauthorized,
			expectedBody:   `{"error":"Invalid API key"}`,
		},
		{
			name: "auth enabled - long key truncation",
			config: types.Config{
				Auth:     true,
				AuthKeys: []string{"very-long-api-key-that-should-be-truncated-in-logs"},
			},
			authHeader:     "Bearer invalid-long-key-that-will-be-truncated",
			expectedStatus: http.StatusUnauthorized,
			expectedBody:   `{"error":"Invalid API key"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create Gin router
			router := gin.New()

			// Add auth middleware
			router.Use(Middleware(tt.config))

			// Add test endpoint
			router.GET("/test", func(c *gin.Context) {
				c.String(http.StatusOK, "success")
			})

			// Create request
			req, _ := http.NewRequest("GET", "/test", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			// Create response recorder
			w := httptest.NewRecorder()

			// Perform request
			router.ServeHTTP(w, req)

			// Check status code
			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			// Check response body
			if w.Body.String() != tt.expectedBody {
				t.Errorf("Expected body %q, got %q", tt.expectedBody, w.Body.String())
			}
		})
	}
}

func TestMiddlewareIntegration(t *testing.T) {
	gin.SetMode(gin.TestMode)

	config := types.Config{
		Auth:     true,
		AuthKeys: []string{"test-key-123", "another-test-key"},
	}

	router := gin.New()
	router.Use(Middleware(config))
	router.GET("/protected", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "protected resource"})
	})

	// Test multiple requests
	tests := []struct {
		name   string
		header string
		status int
	}{
		{"valid key 1", "Bearer test-key-123", http.StatusOK},
		{"valid key 2", "Bearer another-test-key", http.StatusOK},
		{"invalid key", "Bearer wrong-key", http.StatusUnauthorized},
		{"no header", "", http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", "/protected", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != tt.status {
				t.Errorf("Expected status %d, got %d", tt.status, w.Code)
			}
		})
	}
}

func TestAuthMiddlewareWithDifferentMethods(t *testing.T) {
	gin.SetMode(gin.TestMode)

	config := types.Config{
		Auth:     true,
		AuthKeys: []string{"api-key"},
	}

	router := gin.New()
	router.Use(Middleware(config))

	// Add endpoints for different HTTP methods
	router.GET("/test", func(c *gin.Context) { c.String(http.StatusOK, "GET success") })
	router.POST("/test", func(c *gin.Context) { c.String(http.StatusOK, "POST success") })
	router.PUT("/test", func(c *gin.Context) { c.String(http.StatusOK, "PUT success") })
	router.DELETE("/test", func(c *gin.Context) { c.String(http.StatusOK, "DELETE success") })

	methods := []string{"GET", "POST", "PUT", "DELETE"}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			req, _ := http.NewRequest(method, "/test", nil)
			req.Header.Set("Authorization", "Bearer api-key")

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("Method %s: Expected status 200, got %d", method, w.Code)
			}
		})
	}
}

func TestEmptyAuthKeys(t *testing.T) {
	gin.SetMode(gin.TestMode)

	config := types.Config{
		Auth:     true,
		AuthKeys: []string{}, // Empty auth keys
	}

	router := gin.New()
	router.Use(Middleware(config))
	router.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "success")
	})

	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer any-key")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Should reject since no valid keys are configured
	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", w.Code)
	}
}
