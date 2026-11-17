package Middlewares

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestTrustedProxies_FromEnv(t *testing.T) {
	tests := []struct {
		name     string
		env      string
		expected []string
	}{
		{"unset defaults to loopback", "", []string{"127.0.0.1", "::1"}},
		{"blank entries default to loopback", " , ", []string{"127.0.0.1", "::1"}},
		{"single CIDR", "172.16.0.0/12", []string{"172.16.0.0/12"}},
		{"list is trimmed", " 10.0.0.1 , 192.168.0.0/16 ,", []string{"10.0.0.1", "192.168.0.0/16"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TRUSTED_PROXIES", tt.env)
			if got := TrustedProxies(); !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("TrustedProxies() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// newRateLimitedRouter allows one request per IP and trusts only loopback proxies.
func newRateLimitedRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	if err := r.SetTrustedProxies([]string{"127.0.0.1", "::1"}); err != nil {
		t.Fatalf("SetTrustedProxies: %v", err)
	}
	r.Use(ErrorMiddleware())
	r.Use(NewRateLimiter(1, time.Minute).Middleware())
	r.GET("/ping", func(c *gin.Context) { c.Status(http.StatusOK) })
	return r
}

func request(r *gin.Engine, remoteAddr, forwardedFor string) int {
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.RemoteAddr = remoteAddr
	if forwardedFor != "" {
		req.Header.Set("X-Forwarded-For", forwardedFor)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code
}

func TestRateLimiter_SpoofedForwardedForFromUntrustedClient(t *testing.T) {
	r := newRateLimitedRouter(t)
	if code := request(r, "203.0.113.7:5000", "1.1.1.1"); code != http.StatusOK {
		t.Fatalf("first request = %d, want 200", code)
	}
	if code := request(r, "203.0.113.7:5000", "2.2.2.2"); code != http.StatusTooManyRequests {
		t.Errorf("second request with a different spoofed IP = %d, want 429", code)
	}
}

func TestRateLimiter_ForwardedForFromTrustedProxy(t *testing.T) {
	r := newRateLimitedRouter(t)
	if code := request(r, "127.0.0.1:5000", "198.51.100.1"); code != http.StatusOK {
		t.Fatalf("client A first request = %d, want 200", code)
	}
	if code := request(r, "127.0.0.1:5000", "198.51.100.2"); code != http.StatusOK {
		t.Errorf("client B behind the same proxy = %d, want 200 (separate bucket)", code)
	}
	if code := request(r, "127.0.0.1:5000", "198.51.100.1"); code != http.StatusTooManyRequests {
		t.Errorf("client A second request = %d, want 429", code)
	}
}
