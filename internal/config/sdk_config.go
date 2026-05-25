// Package config provides configuration management for the CLI Proxy API server.
// It handles loading and parsing YAML configuration files, and provides structured
// access to application settings including server port, authentication directory,
// debug settings, proxy configuration, and API keys.
package config

// SDKConfig represents the application's configuration, loaded from a YAML file.
type SDKConfig struct {
	// ProxyURL is the URL of an optional proxy server to use for outbound requests.
	ProxyURL string `yaml:"proxy-url" json:"proxy-url"`

	// DisableImageGeneration controls whether the built-in image_generation tool is injected/allowed.
	//
	// Supported values:
	//   - false (default): image_generation is enabled everywhere (normal behavior).
	//   - true: image_generation is disabled everywhere. The server stops injecting it, removes it from request payloads,
	//     and returns 404 for /v1/images/generations and /v1/images/edits.
	//   - "chat": disable image_generation injection for all non-images endpoints (e.g. /v1/responses, /v1/chat/completions),
	//     while keeping /v1/images/generations and /v1/images/edits enabled and preserving image_generation there.
	DisableImageGeneration DisableImageGenerationMode `yaml:"disable-image-generation" json:"disable-image-generation"`

	// NoProxyCIDRs lists CIDRs or literal IPs whose targets should bypass the
	// configured ProxyURL. Management api-call requests to hosts falling
	// inside these ranges are sent directly instead of being routed through
	// the proxy. Applies to literal IPs and hostnames that resolve to IPs
	// inside the list.
	//
	// Defaults (used when this field is empty): 127.0.0.0/8, 10.0.0.0/8,
	// 172.16.0.0/12, 192.168.0.0/16, 169.254.0.0/16, ::1/128, fc00::/7,
	// fe80::/10. Specifying any value here REPLACES the defaults.
	NoProxyCIDRs []string `yaml:"no-proxy-cidrs,omitempty" json:"no-proxy-cidrs,omitempty"`

	// EnableGeminiCLIEndpoint controls whether Gemini CLI internal endpoints (/v1internal:*) are enabled.
	// Default is false for safety; when false, /v1internal:* requests are rejected.
	EnableGeminiCLIEndpoint bool `yaml:"enable-gemini-cli-endpoint" json:"enable-gemini-cli-endpoint"`

	// ForceModelPrefix requires explicit model prefixes (e.g., "teamA/gemini-3-pro-preview")
	// to target prefixed credentials. When false, unprefixed model requests may use prefixed
	// credentials as well.
	ForceModelPrefix bool `yaml:"force-model-prefix" json:"force-model-prefix"`

	// RequestLog enables or disables detailed request logging functionality.
	RequestLog bool `yaml:"request-log" json:"request-log"`

	// APIKeys is a list of keys for authenticating clients to this proxy server.
	APIKeys []string `yaml:"api-keys" json:"api-keys"`

	// PassthroughHeaders controls whether upstream response headers are forwarded to downstream clients.
	// Default is false (disabled).
	PassthroughHeaders bool `yaml:"passthrough-headers" json:"passthrough-headers"`

	// Streaming configures server-side streaming behavior (keep-alives and safe bootstrap retries).
	Streaming StreamingConfig `yaml:"streaming" json:"streaming"`

	// NonStreamKeepAliveInterval controls how often blank lines are emitted for non-streaming responses.
	// <= 0 disables keep-alives. Value is in seconds.
	NonStreamKeepAliveInterval int `yaml:"nonstream-keepalive-interval,omitempty" json:"nonstream-keepalive-interval,omitempty"`
}

// StreamingConfig holds server streaming behavior configuration.
type StreamingConfig struct {
	// KeepAliveSeconds controls how often the server emits SSE heartbeats (": keep-alive\n\n").
	// <= 0 disables keep-alives. Default is 0.
	KeepAliveSeconds int `yaml:"keepalive-seconds,omitempty" json:"keepalive-seconds,omitempty"`

	// BootstrapRetries controls how many times the server may retry a streaming request before any bytes are sent,
	// to allow auth rotation / transient recovery.
	// <= 0 disables bootstrap retries. Default is 0.
	BootstrapRetries int `yaml:"bootstrap-retries,omitempty" json:"bootstrap-retries,omitempty"`

	// UpstreamH2PingIntervalSeconds tunes the HTTP/2 PING-based liveness
	// detection applied to upstream connections used by executors. The h2
	// transport sends a PING frame after this many seconds of read-idle on
	// each connection; if the peer fails to ACK within
	// UpstreamH2PingTimeoutSeconds the connection is closed and the in-flight
	// request fails fast instead of hanging.
	//
	// This addresses upstream providers (notably ChatGPT/Codex OAuth on large
	// reasoning requests) that may stop emitting bytes mid-stream or before
	// the first response event without closing the TCP connection. See
	// router-for-me/CLIProxyAPI#3536.
	//
	//   - 0 (unset) -> apply package default (15 seconds)
	//   - >0        -> use as seconds
	//
	// To disable liveness entirely, set UpstreamH2LivenessDisabled = true.
	UpstreamH2PingIntervalSeconds int `yaml:"upstream-h2-ping-interval-seconds,omitempty" json:"upstream-h2-ping-interval-seconds,omitempty"`

	// UpstreamH2PingTimeoutSeconds bounds how long the h2 transport waits for
	// a PING ACK before declaring the connection dead.
	//
	//   - 0 (unset) -> apply package default (15 seconds)
	//   - >0        -> use as seconds
	UpstreamH2PingTimeoutSeconds int `yaml:"upstream-h2-ping-timeout-seconds,omitempty" json:"upstream-h2-ping-timeout-seconds,omitempty"`

	// UpstreamH2LivenessDisabled turns off HTTP/2 PING-based liveness
	// detection entirely. When true, upstream connections fall back to the
	// legacy behaviour of waiting indefinitely on a silent peer. Most
	// operators should leave this false; the escape hatch exists for
	// environments where h2 PING is undesirable (constrained proxies, custom
	// load balancers that mishandle PING frames, etc.).
	UpstreamH2LivenessDisabled bool `yaml:"upstream-h2-liveness-disabled,omitempty" json:"upstream-h2-liveness-disabled,omitempty"`
}
