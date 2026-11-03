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
	"encoding/json"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"strconv"
	"time"

	"apimarket/internal/config"
)

func main() {
	log := config.Logger("mockprovider")
	want := config.String("MOCK_PROVIDER_KEY", "")
	header := config.String("MOCK_PROVIDER_HEADER", "X-Provider-Key")

	auth := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if want != "" && r.Header.Get(header) != want {
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{"error": "missing or wrong provider credential"})
				return
			}
			next(w, r)
		}
	}
	writeJSON := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(v)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("GET /today", auth(func(w http.ResponseWriter, r *http.Request) {
		city := r.URL.Query().Get("city")
		if city == "" {
			city = "Bengaluru"
		}
		writeJSON(w, map[string]any{"city": city, "temp_c": 20 + rand.IntN(12), "condition": "partly cloudy"})
	}))
	mux.HandleFunc("/echo", auth(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		writeJSON(w, map[string]any{"method": r.Method, "path": r.URL.Path, "query": r.URL.RawQuery, "body": string(b)})
	}))
	mux.HandleFunc("GET /slow", auth(func(w http.ResponseWriter, r *http.Request) {
		ms, _ := strconv.Atoi(r.URL.Query().Get("ms"))
		time.Sleep(time.Duration(ms) * time.Millisecond)
		writeJSON(w, map[string]any{"slept_ms": ms})
	}))
	mux.HandleFunc("GET /status/{code}", auth(func(w http.ResponseWriter, r *http.Request) {
		code, err := strconv.Atoi(r.PathValue("code"))
		if err != nil || code < 100 || code > 599 {
			code = 400
		}
		w.WriteHeader(code)
	}))
	mux.HandleFunc("GET /flaky", auth(func(w http.ResponseWriter, r *http.Request) {
		pct, _ := strconv.Atoi(r.URL.Query().Get("fail_pct"))
		if rand.IntN(100) < pct {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	}))

	addr := config.String("ADDR", ":9000")
	log.Info("mock provider listening", "addr", addr)
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	if err := srv.ListenAndServe(); err != nil {
		log.Error("server", "err", err)
		os.Exit(1)
	}
}
