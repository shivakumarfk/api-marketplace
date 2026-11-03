// Command mockprovider is a fake third-party API for local development and
// tests. It checks that the gateway injected the upstream credential and can
// be told to be slow or to fail.
//
//	GET  /today?city=X        weather-style JSON
//	ANY  /echo                echoes method, path, query and body
//	GET  /slow?ms=1500        responds after a delay
//	GET  /status/503          responds with that status
//	GET  /flaky?fail_pct=30   fails that share of requests with 503
package main

import (
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"apimarket/internal/platform/config"
	"apimarket/internal/platform/run"
)

func main() {
	cfg := config.LoadMockProvider()
	log := config.Logger("mockprovider", "info")
	gin.SetMode(gin.ReleaseMode)
	ctx, stop := run.Context()
	defer stop()

	r := gin.New()
	r.Use(gin.Recovery())
	r.GET("/healthz", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	api := r.Group("/", func(c *gin.Context) {
		if cfg.Key != "" && c.GetHeader(cfg.Header) != cfg.Key {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing or wrong provider credential"})
			return
		}
		c.Next()
	})
	api.GET("/today", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"city": c.DefaultQuery("city", "Bengaluru"), "temp_c": 20 + rand.IntN(12), "condition": "partly cloudy"})
	})
	api.Any("/echo", func(c *gin.Context) {
		b, _ := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
		c.JSON(http.StatusOK, gin.H{"method": c.Request.Method, "path": c.Request.URL.Path, "query": c.Request.URL.RawQuery, "body": string(b)})
	})
	api.GET("/slow", func(c *gin.Context) {
		ms, _ := strconv.Atoi(c.Query("ms"))
		time.Sleep(time.Duration(ms) * time.Millisecond)
		c.JSON(http.StatusOK, gin.H{"slept_ms": ms})
	})
	api.GET("/status/:code", func(c *gin.Context) {
		code, err := strconv.Atoi(c.Param("code"))
		if err != nil || code < 100 || code > 599 {
			code = http.StatusBadRequest
		}
		c.Status(code)
	})
	api.GET("/flaky", func(c *gin.Context) {
		pct, _ := strconv.Atoi(c.Query("fail_pct"))
		if rand.IntN(100) < pct {
			c.Status(http.StatusServiceUnavailable)
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	if err := run.Serve(ctx, log, cfg.Addr, r); err != nil {
		run.Fatal(log, "server", err)
	}
}
