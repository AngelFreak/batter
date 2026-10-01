package middleware

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

// CORSConfig holds CORS configuration options.
type CORSConfig struct {
	AllowedOrigins []string
}

// CORS returns middleware that handles Cross-Origin Resource Sharing.
func CORS(config ...CORSConfig) gin.HandlerFunc {
	var allowedOrigins []string
	if len(config) > 0 && len(config[0].AllowedOrigins) > 0 {
		allowedOrigins = config[0].AllowedOrigins
	}

	return func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")

		allowOrigin := ""
		if origin != "" {
			if IsOriginAllowed(origin, c.Request, allowedOrigins) {
				allowOrigin = origin
			}
		}

		if allowOrigin != "" {
			c.Writer.Header().Set("Access-Control-Allow-Origin", allowOrigin)
			c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		}

		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE, PATCH")
		c.Writer.Header().Set("Access-Control-Max-Age", "86400")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

// IsOriginAllowed reports whether a browser origin may use the API and
// WebSockets. With no allowedOrigins it permits same-origin only: the origin's
// host must match the host the request was addressed to. That needs no
// configuration for the normal deployment, where the UI and API share a host.
// It is exported so the WebSocket upgrader enforces the same policy.
func IsOriginAllowed(origin string, r *http.Request, allowedOrigins []string) bool {
	if len(allowedOrigins) == 0 {
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Host == "" {
			return false
		}
		return normalizeHost(parsed.Host, parsed.Scheme) == normalizeHost(requestHost(r), parsed.Scheme)
	}

	for _, allowed := range allowedOrigins {
		if allowed == "*" {
			return true
		}
		if allowed == origin {
			return true
		}
		if strings.HasPrefix(allowed, "*.") {
			suffix := allowed[1:]
			parsed, err := url.Parse(origin)
			if err != nil {
				continue
			}
			if strings.HasSuffix(parsed.Host, suffix) || parsed.Host == allowed[2:] {
				return true
			}
		}
	}
	return false
}

// requestHost is the host the browser addressed. Behind the Next.js rewrite
// proxy the Host header is rewritten to the backend, and the original arrives
// in X-Forwarded-Host (which the proxy always overwrites). Trusting it is safe
// for an origin check: the threat is a browser on another site, and browsers
// can't set this header on WebSocket handshakes, nor on cross-origin fetches
// without a preflight that this same check then rejects.
func requestHost(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-Host"); fwd != "" {
		host, _, _ := strings.Cut(fwd, ",")
		return strings.TrimSpace(host)
	}
	return r.Host
}

// normalizeHost lowercases host and drops the scheme's default port, since
// browsers omit it from Origin but a Host header may carry it.
func normalizeHost(host, scheme string) string {
	host = strings.ToLower(host)
	switch {
	case scheme == "http" && strings.HasSuffix(host, ":80"):
		return strings.TrimSuffix(host, ":80")
	case scheme == "https" && strings.HasSuffix(host, ":443"):
		return strings.TrimSuffix(host, ":443")
	}
	return host
}
