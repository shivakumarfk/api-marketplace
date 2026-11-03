// Package middleware holds Gin middleware shared by the gateway and the
// control plane.
package middleware

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const requestIDKey = "request_id"

// NewRequestID returns a random request id like req_9f2c….
func NewRequestID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return "req_" + hex.EncodeToString(b)
}

// RequestID assigns every request an id, exposes it in X-Request-Id, and
// stores it in the Gin context. Client-supplied ids are ignored so ids stay
// unique (they de-duplicate billing events downstream).
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := NewRequestID()
		c.Set(requestIDKey, id)
		c.Header("X-Request-Id", id)
		c.Next()
	}
}

// GetRequestID returns the id set by RequestID (or "" if absent).
func GetRequestID(c *gin.Context) string { return c.GetString(requestIDKey) }

// ErrorBody is the stable error shape for every API response.
type ErrorBody struct {
	Error struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"request_id,omitempty"`
	} `json:"error"`
}

// AbortWithError writes the standard error shape and stops the chain.
func AbortWithError(c *gin.Context, status int, code, msg string) {
	var b ErrorBody
	b.Error.Code, b.Error.Message, b.Error.RequestID = code, msg, GetRequestID(c)
	c.AbortWithStatusJSON(status, b)
}

// Recovery turns a panic into a 500 with the standard error shape.
func Recovery(log *slog.Logger) gin.HandlerFunc {
	return gin.CustomRecoveryWithWriter(nil, func(c *gin.Context, rec any) {
		log.Error("panic", "err", rec, "path", c.Request.URL.Path, "request_id", GetRequestID(c))
		AbortWithError(c, http.StatusInternalServerError, "INTERNAL", "internal error")
	})
}

// AccessLog logs one structured line per request.
func AccessLog(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		log.Info("request", "method", c.Request.Method, "path", c.Request.URL.Path, "status", c.Writer.Status(),
			"duration_ms", time.Since(start).Milliseconds(), "request_id", GetRequestID(c))
	}
}

// BearerToken requires "Authorization: Bearer <token>" (constant-time compare).
func BearerToken(token string) gin.HandlerFunc {
	return func(c *gin.Context) {
		got := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		if token == "" || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			AbortWithError(c, http.StatusUnauthorized, "UNAUTHORIZED", "admin token required")
			return
		}
		c.Next()
	}
}
