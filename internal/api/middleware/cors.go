package middleware

import (
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
			if IsOriginAllowed(origin, allowedOrigins) {
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

// IsOriginAllowed reports whether origin is permitted given allowedOrigins.
// When allowedOrigins is empty it falls back to localhost-only (dev default).
// It is exported so non-CORS code paths (e.g. the WebSocket upgrader) can
// enforce the same origin policy.
func IsOriginAllowed(origin string, allowedOrigins []string) bool {
	if len(allowedOrigins) == 0 {
		parsed, err := url.Parse(origin)
		if err != nil {
			return false
		}
		host := parsed.Hostname()
		return host == "localhost" || host == "127.0.0.1" || strings.HasSuffix(host, ".localhost")
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
