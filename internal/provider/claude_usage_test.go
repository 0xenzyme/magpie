package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestClaudeWindowsRateLimit(t *testing.T) {
	var hits atomic.Int32
	limited := atomic.Bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if limited.Load() {
			w.Header().Set("Retry-After", "600")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Write([]byte(`{"five_hour":{"utilization":40,"resets_at":"` + time.Now().Add(time.Hour).UTC().Format(time.RFC3339) +
			`"},"seven_day":{"utilization":10,"resets_at":"` + time.Now().Add(48*time.Hour).UTC().Format(time.RFC3339) + `"}}`))
	}))
	defer srv.Close()
	old := claudeBase
	claudeBase = srv.URL
	defer func() { claudeBase = old }()
	claudeUsage.m = nil
	ctx := context.Background()

	ws, err := claudeWindows(ctx, "A@x", "t")
	if err != nil || len(ws) != 2 || ws[0].Used != 40 {
		t.Fatalf("first: %v %+v", err, ws)
	}
	// the Usage page and the switch list ask again: one request between them
	if ws, err = claudeWindows(ctx, "a@x", "t2"); err != nil || len(ws) != 2 || hits.Load() != 1 {
		t.Fatalf("cached: %v %d", err, hits.Load())
	}

	// past the cache and turned away: what was known stays, a window that
	// has reset since starting from nothing, and nothing is asked until
	// the wait Anthropic named is over
	limited.Store(true)
	e := claudeUsage.m["a@x"]
	e.at = e.at.Add(-2 * time.Hour)
	past := time.Now().Add(-time.Minute)
	e.ws[0].ResetsAt = &past
	claudeUsage.m["a@x"] = e
	if ws, err = claudeWindows(ctx, "a@x", "t"); err != nil || len(ws) != 2 || ws[0].Used != 0 || ws[1].Used != 10 || hits.Load() != 2 {
		t.Fatalf("limited: %v %+v %d", err, ws, hits.Load())
	}
	if _, err = claudeWindows(ctx, "a@x", "t"); err != nil || hits.Load() != 2 {
		t.Fatalf("waiting: %v %d", err, hits.Load())
	}

	// an account never read, turned away: said so, and not asked again
	// until the wait is over
	if _, err = claudeWindows(ctx, "b@x", "t"); err == nil || !strings.Contains(err.Error(), "10m") || hits.Load() != 3 {
		t.Fatalf("new limited: %v %d", err, hits.Load())
	}
	if _, err = claudeWindows(ctx, "b@x", "t"); err == nil || hits.Load() != 3 {
		t.Fatalf("new waiting: %v %d", err, hits.Load())
	}
}
