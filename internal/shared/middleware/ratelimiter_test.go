package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiter(t *testing.T) {
	const limit = 3
	rl := NewRateLimiter(limit, time.Minute)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.42:5555"

	for i := 0; i < limit; i++ {
		rec := httptest.NewRecorder()
		rl.Middleware(handler).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: got status %d, want %d", i+1, rec.Code, http.StatusOK)
		}
	}

	rec := httptest.NewRecorder()
	rl.Middleware(handler).ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("request %d: got status %d, want %d", limit+1, rec.Code, http.StatusTooManyRequests)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("limited response is missing Retry-After header")
	}
}

func TestRateLimiterPerClient(t *testing.T) {
	rl := NewRateLimiter(2, time.Minute)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	busy := httptest.NewRequest(http.MethodGet, "/", nil)
	busy.RemoteAddr = "10.0.0.1:1000"

	quiet := httptest.NewRequest(http.MethodGet, "/", nil)
	quiet.RemoteAddr = "10.0.0.2:2000"

	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		rl.Middleware(handler).ServeHTTP(rec, busy)
		if rec.Code != http.StatusOK {
			t.Fatalf("busy client request %d: got status %d", i+1, rec.Code)
		}
	}

	rec := httptest.NewRecorder()
	rl.Middleware(handler).ServeHTTP(rec, busy)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("busy client exceeded limit but got status %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	rl.Middleware(handler).ServeHTTP(rec, quiet)
	if rec.Code != http.StatusOK {
		t.Fatalf("quiet client was rate limited: got status %d", rec.Code)
	}
}

func TestRateLimiterIgnoresXForwardedFor(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.7:4000"
	req.Header.Set("X-Forwarded-For", "203.0.113.9")

	rec := httptest.NewRecorder()
	rl.Middleware(handler).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first request: got status %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	rl.Middleware(handler).ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second request should hit limit regardless of X-Forwarded-For: got status %d", rec.Code)
	}
}

func TestRateLimiterSharesBucketByIP(t *testing.T) {
	rl := NewRateLimiter(2, time.Minute)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Same IP, different source ports: all connections share one bucket.
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "10.0.0.8:12345"
		rec := httptest.NewRecorder()
		rl.Middleware(handler).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: got status %d", i+1, rec.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.8:54321"
	rec := httptest.NewRecorder()
	rl.Middleware(handler).ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("new connection from same IP: got status %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
}

func TestRateLimiterWindowExpiry(t *testing.T) {
	rl := NewRateLimiter(1, 10*time.Millisecond)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.9:3000"

	rec := httptest.NewRecorder()
	rl.Middleware(handler).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first request: got status %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	rl.Middleware(handler).ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second request within window: got status %d", rec.Code)
	}

	time.Sleep(25 * time.Millisecond)

	rec = httptest.NewRecorder()
	rl.Middleware(handler).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("request after window expiry: got status %d, want %d", rec.Code, http.StatusOK)
	}
}
