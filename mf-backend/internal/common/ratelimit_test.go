package common

import (
	"context"
	"net/http/httptest"
	"strconv"
	"testing"
)

func BenchmarkClientIP(b *testing.B) {
	r := httptest.NewRequest("GET", "/llm/runs", nil)
	r.RemoteAddr = "203.0.113.7:44321"
	b.ReportAllocs()
	for b.Loop() {
		_ = ClientIP(r)
	}
}

// BenchmarkRateLimiterParallel measures lock contention across cores. A single
// global mutex serialises every caller; the sharded map exists to keep this
// from becoming a bottleneck if the limiter is ever applied API-wide.
func BenchmarkRateLimiterParallel(b *testing.B) {
	rl := NewRateLimiter(context.Background(), 1e9, 1e9)
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			rl.allow("10.0.0." + strconv.Itoa(i%256))
			i++
		}
	})
}

// The header is client-controlled. Honouring it would let a caller mint a new
// bucket per request and bypass the limiter entirely.
func TestClientIPIgnoresForwardedForHeader(t *testing.T) {
	r := httptest.NewRequest("GET", "/auth/login", nil)
	r.RemoteAddr = "198.51.100.9:5555"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")

	if got := ClientIP(r); got != "198.51.100.9" {
		t.Errorf("ClientIP = %q, want the RemoteAddr host 198.51.100.9", got)
	}
}

func TestRateLimiterBlocksAfterBurst(t *testing.T) {
	// rate 0 so the bucket never refills during the test.
	rl := NewRateLimiter(context.Background(), 0, 3)

	for i := range 3 {
		if !rl.allow("10.0.0.1") {
			t.Fatalf("request %d denied, want allowed within burst of 3", i+1)
		}
	}
	if rl.allow("10.0.0.1") {
		t.Error("4th request allowed, want denied once the burst is spent")
	}
	// A different key has its own bucket.
	if !rl.allow("10.0.0.2") {
		t.Error("a distinct key was denied, want its own independent bucket")
	}
}

// The visitor map is keyed by caller-influenced input, so it must not grow
// without bound even when every key is unique.
func TestRateLimiterCapsMemory(t *testing.T) {
	rl := NewRateLimiter(context.Background(), 1e9, 1e9)

	for i := range 500_000 {
		rl.allow("key-" + strconv.Itoa(i))
	}

	total := 0
	for i := range rl.shards {
		rl.shards[i].mu.Lock()
		total += len(rl.shards[i].visitors)
		rl.shards[i].mu.Unlock()
	}
	if max := shardCount * maxVisitorsPerShard; total > max {
		t.Errorf("tracked %d visitors, want at most %d", total, max)
	}
}

// The eviction goroutine must stop with its context, or every limiter ever
// constructed leaks one for the life of the process.
func TestRateLimiterStopsWithContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	rl := NewRateLimiter(ctx, 1, 1)
	cancel()

	// Nothing to assert directly without goleak; this at least documents the
	// contract and fails to compile if the signature regresses.
	if !rl.allow("10.0.0.1") {
		t.Error("limiter should still serve requests after its cleanup loop exits")
	}
}
