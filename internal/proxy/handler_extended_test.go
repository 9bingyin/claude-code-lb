package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"claude-code-lb/internal/balance"
	"claude-code-lb/internal/stats"
	"claude-code-lb/pkg/types"

	"github.com/gin-gonic/gin"
)

func TestHandlerClientErrorAsFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		statusCode int
	}{
		{
			name:       "400 Bad Request",
			statusCode: 400,
		},
		{
			name:       "403 Forbidden",
			statusCode: 403,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test upstream server that returns the specific status code
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte("Error"))
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

			// Should return 502 (request failed due to upstream error being treated as failure)
			if w.Code != 502 {
				t.Errorf("Expected status 502, got %d", w.Code)
			}

			// Verify that the server was marked as down
			// We need to check if the balancer has marked the server as unhealthy
			// Since balancer.GetServerStatus returns a map of server URL to boolean (true = healthy, false = unhealthy? Or true = down?)
			// Let's check internal/balance/balancer.go or previous test usage.
			// In TestHandlerUpstreamError:
			// status := balancer.GetServerStatus()
			// if status[upstream.URL] {
			//     t.Error("Expected server to be marked as down")
			// }
			// So true means healthy/available, false means down/unavailable? 
			// Wait, let's verify GetServerStatus return meaning.
			
			// Checking internal/balance/balancer.go via previous knowledge or read_file if unsure.
			// Assuming based on TestHandlerUpstreamError logic:
			// if status[upstream.URL] { t.Error(...) } -> implies status[url] being true is BAD when server is down.
			// So status[url] probably represents "Is Available/Healthy".
			
			status := balancer.GetServerStatus()
			if status[upstream.URL] {
				t.Errorf("Expected server to be marked as down (unhealthy) for status code %d", tt.statusCode)
			}
		})
	}
}
