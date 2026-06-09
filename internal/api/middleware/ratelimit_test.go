package middleware

import (
	"testing"
	"time"
)

func TestRateLimiterAllowsUpToLimit(t *testing.T) {
	rl := newRateLimiter(3, time.Minute)
	now := time.Unix(1_700_000_000, 0)

	for i := 0; i < 3; i++ {
		ok, _ := rl.allow("1.2.3.4", now)
		if !ok {
			t.Fatalf("request %d within limit was denied", i+1)
		}
	}

	ok, retryAfter := rl.allow("1.2.3.4", now)
	if ok {
		t.Fatal("request over the limit was allowed")
	}
	if retryAfter <= 0 || retryAfter > time.Minute {
		t.Fatalf("retryAfter = %v, want (0, 1m]", retryAfter)
	}
}

func TestRateLimiterResetsAfterWindow(t *testing.T) {
	rl := newRateLimiter(1, time.Minute)
	now := time.Unix(1_700_000_000, 0)

	if ok, _ := rl.allow("ip", now); !ok {
		t.Fatal("first request denied")
	}
	if ok, _ := rl.allow("ip", now); ok {
		t.Fatal("second request in window allowed")
	}

	// After the window elapses, the counter resets.
	later := now.Add(time.Minute + time.Second)
	if ok, _ := rl.allow("ip", later); !ok {
		t.Fatal("request after window reset was denied")
	}
}

func TestRateLimiterIsPerKey(t *testing.T) {
	rl := newRateLimiter(1, time.Minute)
	now := time.Unix(1_700_000_000, 0)

	if ok, _ := rl.allow("a", now); !ok {
		t.Fatal("first key denied")
	}
	// A different key has its own budget.
	if ok, _ := rl.allow("b", now); !ok {
		t.Fatal("second key should be independent")
	}
}

func TestItoa(t *testing.T) {
	cases := map[int]string{0: "1", -5: "1", 1: "1", 9: "9", 10: "10", 61: "61"}
	for in, want := range cases {
		if got := itoa(in); got != want {
			t.Errorf("itoa(%d) = %q, want %q", in, got, want)
		}
	}
}
