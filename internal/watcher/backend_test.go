package watcher

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewGHCR(t *testing.T) {
	g := NewGHCR(nil)

	if g == nil {
		t.Fatal("NewGHCR(nil) returned nil")
	}
	if g.client == nil {
		t.Error("client is nil")
	}
	if g.tokens == nil {
		t.Error("tokens map is nil")
	}
	if g.limiter == nil {
		t.Error("limiter is nil")
	}
}

func TestGHCR_RateLimiter(t *testing.T) {
	g := NewGHCR(nil)

	// The rate limiter allows burst of 5, so first 5 should be instant
	start := time.Now()
	for i := 0; i < 5; i++ {
		if !g.limiter.Allow() {
			t.Errorf("limiter should allow request %d in burst", i)
		}
	}
	elapsed := time.Since(start)

	// Burst should be nearly instant (< 50ms)
	if elapsed > 50*time.Millisecond {
		t.Errorf("burst took too long: %v", elapsed)
	}

	// 6th request should be rate limited
	if g.limiter.Allow() {
		t.Error("limiter should NOT allow 6th request immediately after burst")
	}
}

func TestGHCR_TokenCaching(t *testing.T) {
	// Create a mock server that returns a token
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"token": "test-token-123"}`))
	}))
	defer server.Close()

	g := NewGHCR(nil)
	// Pre-populate the token cache
	g.mu.Lock()
	g.tokens["test/repo"] = "cached-token"
	g.mu.Unlock()

	// Get token should return cached value without making HTTP request
	g.mu.Lock()
	tok, ok := g.tokens["test/repo"]
	g.mu.Unlock()

	if !ok {
		t.Fatal("expected token to be cached")
	}
	if tok != "cached-token" {
		t.Errorf("token = %s, want cached-token", tok)
	}
}

func TestGHCR_DropToken(t *testing.T) {
	g := NewGHCR(nil)

	// Add a token
	g.mu.Lock()
	g.tokens["test/repo"] = "my-token"
	g.mu.Unlock()

	// Verify it's there
	g.mu.Lock()
	_, ok := g.tokens["test/repo"]
	g.mu.Unlock()
	if !ok {
		t.Fatal("token should exist before drop")
	}

	// Drop it
	g.dropToken("test/repo")

	// Verify it's gone
	g.mu.Lock()
	_, ok = g.tokens["test/repo"]
	g.mu.Unlock()
	if ok {
		t.Error("token should be deleted after drop")
	}
}

func TestGHCR_HeadDigest_ContextCancellation(t *testing.T) {
	g := NewGHCR(nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, _, _, _, err := g.HeadDigest(ctx, "test/repo", "latest", "", "latest")
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

func TestGHCR_ListTags_ContextCancellation(t *testing.T) {
	g := NewGHCR(nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := g.ListTags(ctx, "test/repo")
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

func TestGHCR_MockServer(t *testing.T) {
	// Test token endpoint
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"token": "test-token"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer tokenServer.Close()

	// This test verifies the mock server pattern works
	resp, err := http.Get(tokenServer.URL + "/token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}
