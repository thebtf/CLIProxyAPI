package helps

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/proxyutil"
)

// http2.ConfigureTransports is not idempotent — calling it twice on the same
// transport returns "protocol https already registered". The transportCache
// makes that fact safe at the call-site by handing out the same configured
// transport across requests with matching keys. Tests exercise the lower-level
// helper applyHTTP2LivenessTo against a fresh cloned transport so they can
// inspect the resulting *http2.Transport directly.

func TestApplyHTTP2LivenessTo_DefaultsUsePackageValues(t *testing.T) {
	tr := proxyutil.CloneDefaultTransport()
	t2 := applyHTTP2LivenessTo(tr, defaultUpstreamH2PingIntervalSeconds, defaultUpstreamH2PingTimeoutSeconds)
	if t2 == nil {
		t.Fatalf("expected non-nil *http2.Transport when liveness is enabled")
	}
	wantRead := time.Duration(defaultUpstreamH2PingIntervalSeconds) * time.Second
	if t2.ReadIdleTimeout != wantRead {
		t.Errorf("ReadIdleTimeout = %v, want %v", t2.ReadIdleTimeout, wantRead)
	}
	wantPing := time.Duration(defaultUpstreamH2PingTimeoutSeconds) * time.Second
	if t2.PingTimeout != wantPing {
		t.Errorf("PingTimeout = %v, want %v", t2.PingTimeout, wantPing)
	}
}

func TestApplyHTTP2LivenessTo_CustomIntervalAndTimeout(t *testing.T) {
	tr := proxyutil.CloneDefaultTransport()
	t2 := applyHTTP2LivenessTo(tr, 5, 7)
	if t2 == nil {
		t.Fatalf("expected non-nil *http2.Transport")
	}
	if t2.ReadIdleTimeout != 5*time.Second {
		t.Errorf("ReadIdleTimeout = %v, want 5s", t2.ReadIdleTimeout)
	}
	if t2.PingTimeout != 7*time.Second {
		t.Errorf("PingTimeout = %v, want 7s", t2.PingTimeout)
	}
}

func TestApplyHTTP2LivenessTo_DisabledOnZeroInterval(t *testing.T) {
	tr := proxyutil.CloneDefaultTransport()
	t2 := applyHTTP2LivenessTo(tr, 0, 15)
	if t2 != nil {
		t.Errorf("expected nil *http2.Transport when interval is 0, got %+v", t2)
	}
}

func TestApplyHTTP2LivenessTo_ZeroTimeoutFallsBackToInterval(t *testing.T) {
	tr := proxyutil.CloneDefaultTransport()
	t2 := applyHTTP2LivenessTo(tr, 12, 0)
	if t2 == nil {
		t.Fatalf("expected non-nil *http2.Transport when interval > 0")
	}
	if t2.PingTimeout != 12*time.Second {
		t.Errorf("PingTimeout = %v, want 12s (fallback to interval)", t2.PingTimeout)
	}
}

func TestH2LivenessSettings_ZeroFieldsUseDefaults(t *testing.T) {
	intervalSec, timeoutSec := h2LivenessSettings(&config.Config{})
	if intervalSec != defaultUpstreamH2PingIntervalSeconds {
		t.Errorf("intervalSec = %d, want %d", intervalSec, defaultUpstreamH2PingIntervalSeconds)
	}
	if timeoutSec != defaultUpstreamH2PingTimeoutSeconds {
		t.Errorf("timeoutSec = %d, want %d", timeoutSec, defaultUpstreamH2PingTimeoutSeconds)
	}
}

func TestH2LivenessSettings_NilConfigUsesDefaults(t *testing.T) {
	intervalSec, timeoutSec := h2LivenessSettings(nil)
	if intervalSec != defaultUpstreamH2PingIntervalSeconds {
		t.Errorf("intervalSec = %d, want %d", intervalSec, defaultUpstreamH2PingIntervalSeconds)
	}
	if timeoutSec != defaultUpstreamH2PingTimeoutSeconds {
		t.Errorf("timeoutSec = %d, want %d", timeoutSec, defaultUpstreamH2PingTimeoutSeconds)
	}
}

func TestH2LivenessSettings_CustomValuesPropagate(t *testing.T) {
	cfg := &config.Config{}
	cfg.SDKConfig.Streaming.UpstreamH2PingIntervalSeconds = 30
	cfg.SDKConfig.Streaming.UpstreamH2PingTimeoutSeconds = 45
	intervalSec, timeoutSec := h2LivenessSettings(cfg)
	if intervalSec != 30 {
		t.Errorf("intervalSec = %d, want 30", intervalSec)
	}
	if timeoutSec != 45 {
		t.Errorf("timeoutSec = %d, want 45", timeoutSec)
	}
}

func TestH2LivenessSettings_DisabledFlagReturnsZeros(t *testing.T) {
	cfg := &config.Config{}
	cfg.SDKConfig.Streaming.UpstreamH2LivenessDisabled = true
	// Even with non-zero interval set, disabled flag wins.
	cfg.SDKConfig.Streaming.UpstreamH2PingIntervalSeconds = 30
	intervalSec, timeoutSec := h2LivenessSettings(cfg)
	if intervalSec != 0 || timeoutSec != 0 {
		t.Errorf("expected (0, 0) when disabled=true, got (%d, %d)", intervalSec, timeoutSec)
	}
}

func TestH2LivenessSettings_NegativeValuesClampToDefault(t *testing.T) {
	cfg := &config.Config{}
	cfg.SDKConfig.Streaming.UpstreamH2PingIntervalSeconds = -5
	cfg.SDKConfig.Streaming.UpstreamH2PingTimeoutSeconds = -1
	intervalSec, timeoutSec := h2LivenessSettings(cfg)
	if intervalSec != defaultUpstreamH2PingIntervalSeconds {
		t.Errorf("intervalSec = %d, want %d (negative clamped)", intervalSec, defaultUpstreamH2PingIntervalSeconds)
	}
	if timeoutSec != defaultUpstreamH2PingTimeoutSeconds {
		t.Errorf("timeoutSec = %d, want %d (negative clamped)", timeoutSec, defaultUpstreamH2PingTimeoutSeconds)
	}
}

func TestNewProxyAwareHTTPClient_ReusesTransportAcrossCalls(t *testing.T) {
	resetTransportCacheForTests()
	cfg := &config.Config{}

	c1 := NewProxyAwareHTTPClient(context.Background(), cfg, nil, 0)
	c2 := NewProxyAwareHTTPClient(context.Background(), cfg, nil, 0)
	if c1.Transport == nil || c2.Transport == nil {
		t.Fatalf("expected both clients to share a non-nil transport")
	}
	if c1.Transport != c2.Transport {
		t.Errorf("expected identical *http.Transport instances across calls; got %p vs %p", c1.Transport, c2.Transport)
	}
}

func TestNewProxyAwareHTTPClient_DifferentH2KnobsYieldDifferentTransports(t *testing.T) {
	resetTransportCacheForTests()

	cfgA := &config.Config{}
	cfgA.SDKConfig.Streaming.UpstreamH2PingIntervalSeconds = 5

	cfgB := &config.Config{}
	cfgB.SDKConfig.Streaming.UpstreamH2PingIntervalSeconds = 30

	a := NewProxyAwareHTTPClient(context.Background(), cfgA, nil, 0)
	b := NewProxyAwareHTTPClient(context.Background(), cfgB, nil, 0)
	if a.Transport == nil || b.Transport == nil {
		t.Fatalf("expected non-nil transports")
	}
	if a.Transport == b.Transport {
		t.Errorf("expected distinct transports for different h2 configs")
	}
}

func TestNewProxyAwareHTTPClient_InstallsCachedTransport(t *testing.T) {
	resetTransportCacheForTests()
	c := NewProxyAwareHTTPClient(context.Background(), &config.Config{}, nil, 0)
	if c.Transport == nil {
		t.Fatalf("expected Transport to be set so h2 liveness applies")
	}
	if _, ok := c.Transport.(*http.Transport); !ok {
		t.Errorf("transport type = %T, want *http.Transport", c.Transport)
	}
}

func TestNewProxyAwareHTTPClient_ContextRoundTripperBypassesCache(t *testing.T) {
	resetTransportCacheForTests()
	custom := &countingRoundTripper{}
	ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", http.RoundTripper(custom))
	c := NewProxyAwareHTTPClient(ctx, &config.Config{}, nil, 0)
	if c.Transport != custom {
		t.Errorf("expected context RoundTripper to win priority over the cache; got %T", c.Transport)
	}
}

func TestTransportCache_LRUEvictsLeastRecentlyUsed(t *testing.T) {
	c := newTransportCache(2)

	build := func(label string) func() http.RoundTripper {
		return func() http.RoundTripper {
			return &countingRoundTripper{label: label}
		}
	}

	keyA := transportKey{proxyURL: "a"}
	keyB := transportKey{proxyURL: "b"}
	keyC := transportKey{proxyURL: "c"}

	a := c.getOrBuild(keyA, build("a"))
	b := c.getOrBuild(keyB, build("b"))
	// Touch A so it becomes more recently used than B.
	_ = c.getOrBuild(keyA, build("a-rebuild-should-not-fire"))
	// Insert C — capacity is 2, so the LRU entry (B) must be evicted.
	c.getOrBuild(keyC, build("c"))

	// A should still be cached; building "a-rebuild" would have replaced it.
	got := c.getOrBuild(keyA, func() http.RoundTripper {
		t.Fatalf("expected cached A, but build was invoked again")
		return nil
	})
	if got != a {
		t.Errorf("expected same A instance; got %T", got)
	}

	// B should have been evicted, so a new build must run.
	rebuiltB := false
	_ = c.getOrBuild(keyB, func() http.RoundTripper {
		rebuiltB = true
		return &countingRoundTripper{label: "b-after-evict"}
	})
	if !rebuiltB {
		t.Errorf("expected B to be rebuilt after eviction")
	}

	// b instance check: ensure original B is no longer the cached one
	_ = b
}

func TestTransportCache_BoundedUnderHighChurn(t *testing.T) {
	const cap = 8
	c := newTransportCache(cap)
	for i := 0; i < 100; i++ {
		key := transportKey{proxyURL: fmt.Sprintf("p%d", i)}
		c.getOrBuild(key, func() http.RoundTripper {
			return &countingRoundTripper{}
		})
	}
	c.mu.Lock()
	entries := len(c.entries)
	listLen := c.lruList.Len()
	c.mu.Unlock()
	if entries != cap {
		t.Errorf("entries = %d, want %d", entries, cap)
	}
	if listLen != cap {
		t.Errorf("listLen = %d, want %d", listLen, cap)
	}
}

type countingRoundTripper struct {
	label string
	calls int
}

func (r *countingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	r.calls++
	return nil, nil
}
