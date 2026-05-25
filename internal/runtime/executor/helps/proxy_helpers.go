package helps

import (
	"container/list"
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/proxyutil"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/http2"
)

// Default HTTP/2 PING liveness windows.
// ReadIdleTimeout fires a PING after this many seconds of read-idle;
// PingTimeout bounds how long we wait for the PING ACK before tearing the
// connection down. Combined, a dead upstream connection terminates in roughly
// ReadIdleTimeout + PingTimeout (~30s with defaults).
const (
	defaultUpstreamH2PingIntervalSeconds = 15
	defaultUpstreamH2PingTimeoutSeconds  = 15

	// defaultTransportCacheCapacity bounds the global transport cache. Each
	// entry holds a fully configured *http.Transport (TLS session pool + h2
	// connection pool). 64 is enough to cover several proxies and a few
	// distinct h2-knob configurations without unbounded memory growth even
	// under aggressive management-API config churn.
	defaultTransportCacheCapacity = 64
)

// transportKey identifies a unique transport configuration. Equal keys mean
// the cached *http.Transport is safe to reuse across requests; differing keys
// must yield distinct transports so that proxy boundaries and h2 liveness
// settings stay isolated.
type transportKey struct {
	proxyURL   string // "" when no proxy is configured
	h2Interval int    // resolved interval seconds (0 = liveness disabled for this key)
	h2Timeout  int    // resolved timeout seconds
}

// transportCache memoizes *http.Transport instances by transportKey so that
// connection pools (TLS sessions, HTTP/2 streams) are reused across requests
// with matching configuration. Without this, each call to
// NewProxyAwareHTTPClient would build a fresh transport, multiplying TLS
// handshakes and defeating h2 multiplexing under load.
//
// The cache is bounded by an LRU policy: when an entry would push the size
// past capacity, the least-recently-used entry is dropped. This keeps memory
// finite even when operators churn the h2-knob settings (e.g. via management
// API). Evicted transports are not actively closed — Go's GC reclaims them
// once their in-flight requests drain and their idle connection pools time
// out via the transport's IdleConnTimeout.
type transportCache struct {
	mu       sync.Mutex
	capacity int
	entries  map[transportKey]*list.Element
	lruList  *list.List // front = most recently used; back = eviction target
}

type transportCacheEntry struct {
	key   transportKey
	value http.RoundTripper
}

func newTransportCache(capacity int) *transportCache {
	if capacity <= 0 {
		capacity = defaultTransportCacheCapacity
	}
	return &transportCache{
		capacity: capacity,
		entries:  make(map[transportKey]*list.Element, capacity),
		lruList:  list.New(),
	}
}

func (c *transportCache) getOrBuild(key transportKey, build func() http.RoundTripper) http.RoundTripper {
	c.mu.Lock()
	if elem, ok := c.entries[key]; ok {
		c.lruList.MoveToFront(elem)
		rt := elem.Value.(*transportCacheEntry).value
		c.mu.Unlock()
		return rt
	}
	// Release the lock while we build — building can hit DNS/TLS bootstrap
	// in proxy paths and we do not want to serialize unrelated keys behind
	// each other.
	c.mu.Unlock()
	rt := build()
	if rt == nil {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	// Re-check after acquiring the lock: another goroutine may have built
	// the same key in parallel. Prefer the earlier instance to avoid leaking
	// the just-built transport.
	if elem, ok := c.entries[key]; ok {
		c.lruList.MoveToFront(elem)
		return elem.Value.(*transportCacheEntry).value
	}
	elem := c.lruList.PushFront(&transportCacheEntry{key: key, value: rt})
	c.entries[key] = elem
	for c.lruList.Len() > c.capacity {
		c.evictOldestLocked()
	}
	return rt
}

func (c *transportCache) evictOldestLocked() {
	back := c.lruList.Back()
	if back == nil {
		return
	}
	entry := back.Value.(*transportCacheEntry)
	c.lruList.Remove(back)
	delete(c.entries, entry.key)
}

func (c *transportCache) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[transportKey]*list.Element, c.capacity)
	c.lruList = list.New()
}

var globalTransportCache = newTransportCache(defaultTransportCacheCapacity)

// NewProxyAwareHTTPClient creates an HTTP client with proper proxy configuration priority:
// 1. Use auth.ProxyURL if configured (highest priority)
// 2. Use cfg.ProxyURL if auth proxy is not configured
// 3. Use RoundTripper from context if neither are configured
// 4. Otherwise, fall back to the cached default transport
//
// Transports are cached by (proxyURL, h2-interval, h2-timeout) so the
// underlying TLS session and HTTP/2 connection pool are reused across
// requests with matching configuration. HTTP/2 PING-based liveness (see
// router-for-me/CLIProxyAPI#3536) is attached to every cached transport we
// own — context-supplied RoundTrippers are left untouched because their owner
// is responsible for tuning their own liveness.
//
// Parameters:
//   - ctx: The context containing optional RoundTripper
//   - cfg: The application configuration
//   - auth: The authentication information
//   - timeout: The client timeout (0 means no timeout)
//
// Returns:
//   - *http.Client: An HTTP client with a shared, liveness-aware transport.
func NewProxyAwareHTTPClient(ctx context.Context, cfg *config.Config, auth *cliproxyauth.Auth, timeout time.Duration) *http.Client {
	httpClient := &http.Client{}
	if timeout > 0 {
		httpClient.Timeout = timeout
	}

	// Priority 1: Use auth.ProxyURL if configured
	var proxyURL string
	if auth != nil {
		proxyURL = strings.TrimSpace(auth.ProxyURL)
	}

	// Priority 2: Use cfg.ProxyURL if auth proxy is not configured
	if proxyURL == "" && cfg != nil {
		proxyURL = strings.TrimSpace(cfg.ProxyURL)
	}

	intervalSec, timeoutSec := h2LivenessSettings(cfg)

	// Priority 1+2: a proxy URL is configured
	if proxyURL != "" {
		key := transportKey{proxyURL: proxyURL, h2Interval: intervalSec, h2Timeout: timeoutSec}
		rt := globalTransportCache.getOrBuild(key, func() http.RoundTripper {
			tr := buildProxyTransport(proxyURL)
			if tr == nil {
				return nil
			}
			applyHTTP2LivenessTo(tr, intervalSec, timeoutSec)
			return tr
		})
		if rt != nil {
			httpClient.Transport = rt
			return httpClient
		}
		// If proxy setup failed, log and fall through to context RoundTripper
		log.Debugf("failed to setup proxy from URL: %s, falling back to context transport", proxyutil.Redact(proxyURL))
	}

	// Priority 3: Use RoundTripper from context (typically from RoundTripperFor)
	if rt, ok := ctx.Value("cliproxy.roundtripper").(http.RoundTripper); ok && rt != nil {
		httpClient.Transport = rt
		return httpClient
	}

	// Priority 4: No proxy, no context RoundTripper — use the cached default
	// transport so h2 liveness is attached without creating a fresh transport
	// (and a fresh TLS/h2 connection pool) on every request.
	key := transportKey{h2Interval: intervalSec, h2Timeout: timeoutSec}
	rt := globalTransportCache.getOrBuild(key, func() http.RoundTripper {
		tr := proxyutil.CloneDefaultTransport()
		if tr == nil {
			return nil
		}
		applyHTTP2LivenessTo(tr, intervalSec, timeoutSec)
		return tr
	})
	if rt != nil {
		httpClient.Transport = rt
	}

	return httpClient
}

// applyHTTP2LivenessTo configures HTTP/2 PING-based liveness directly on the
// supplied transport using already-resolved interval/timeout seconds. The
// function is a no-op when the interval is 0 (liveness disabled).
//
// HTTP/2 PING is the standard h2 mechanism for connection-level liveness: it
// is unrelated to bounded response timeouts and does not interfere with valid
// long-running streams that keep emitting frames. It only catches the failure
// mode where the upstream goes silent without closing the TCP connection.
//
// ConfigureTransports is not idempotent — call this helper exactly once per
// *http.Transport instance. The transportCache enforces this by caching the
// configured transport.
func applyHTTP2LivenessTo(transport *http.Transport, intervalSec, timeoutSec int) *http2.Transport {
	if transport == nil || intervalSec <= 0 {
		return nil
	}
	if timeoutSec <= 0 {
		timeoutSec = intervalSec
	}
	t2, err := http2.ConfigureTransports(transport)
	if err != nil {
		log.Debugf("h2 transport configure failed: %v", err)
		return nil
	}
	t2.ReadIdleTimeout = time.Duration(intervalSec) * time.Second
	t2.PingTimeout = time.Duration(timeoutSec) * time.Second
	return t2
}

// h2LivenessSettings resolves the h2 PING interval and timeout from config.
// Returns (0, 0) when liveness is explicitly disabled via
// cfg.Streaming.UpstreamH2LivenessDisabled. Otherwise returns the configured
// values, substituting package defaults for zero (unset) fields.
//
//   - cfg.Streaming.UpstreamH2LivenessDisabled = true -> (0, 0), disable
//   - field == 0 (unset)                              -> apply package default
//   - field > 0                                       -> use as seconds
//
// Negative values are clamped to the default; the YAML field is documented as
// non-negative and operators who want to disable liveness should use the
// boolean rather than a sentinel number.
func h2LivenessSettings(cfg *config.Config) (intervalSec int, timeoutSec int) {
	if cfg != nil && cfg.Streaming.UpstreamH2LivenessDisabled {
		return 0, 0
	}
	intervalSec = defaultUpstreamH2PingIntervalSeconds
	timeoutSec = defaultUpstreamH2PingTimeoutSeconds
	if cfg == nil {
		return intervalSec, timeoutSec
	}
	if v := cfg.Streaming.UpstreamH2PingIntervalSeconds; v > 0 {
		intervalSec = v
	}
	if v := cfg.Streaming.UpstreamH2PingTimeoutSeconds; v > 0 {
		timeoutSec = v
	}
	return intervalSec, timeoutSec
}

// buildProxyTransport creates an HTTP transport configured for the given proxy URL.
// It supports SOCKS5, HTTP, and HTTPS proxy protocols.
//
// Parameters:
//   - proxyURL: The proxy URL string (e.g., "socks5://user:pass@host:port", "http://host:port")
//
// Returns:
//   - *http.Transport: A configured transport, or nil if the proxy URL is invalid
func buildProxyTransport(proxyURL string) *http.Transport {
	transport, _, errBuild := proxyutil.BuildHTTPTransport(proxyURL)
	if errBuild != nil {
		log.Errorf("%v", errBuild)
		return nil
	}
	return transport
}

// resetTransportCacheForTests clears the global transport cache. Intended for
// use from tests only — production code must not call this.
func resetTransportCacheForTests() {
	globalTransportCache.reset()
}
