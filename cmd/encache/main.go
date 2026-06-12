package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"encache/internal/cache"
	"encache/internal/config"
	"encache/internal/interceptor"
	"encache/internal/proxy"
	"encache/internal/store"
	"encache/internal/upstream"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if err := os.MkdirAll(cfg.StoragePath, 0o755); err != nil {
		log.Fatalf("create storage path: %v", err)
	}

	store, err := store.Open(context.Background(), cfg.StoragePath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer store.Close()

	upstreamClient := upstream.NewClient()
	cacheManager := cache.NewManager(cfg.StoragePath, cfg.UpstreamURL, store)
	cacheManager.Client = upstreamClient
	cacheManager.StartDailyCleanup(context.Background(), cfg.CleanupDays)
	playbackEventLog := &interceptor.PlaybackEventLog{MaxSessions: cfg.MaxSessions}
	chain := []interceptor.Interceptor{}
	if cfg.EnableDownload {
		chain = append(chain, interceptor.EnableDownload{Cache: cacheManager})
	}
	chain = append(chain,
		interceptor.StreamCache{Cache: cacheManager},
		playbackEventLog,
		interceptor.ItemCapture{Store: store},
		interceptor.Logger{},
	)

	handler := proxy.NewWithClient(cfg.UpstreamURL, upstreamClient, chain)
	server := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Handler:           handler,
		ReadHeaderTimeout: 15 * time.Second,
	}

	log.Printf("Emby Proxy running on http://%s:%d", cfg.Host, cfg.Port)
	log.Printf("Upstream: %s", cfg.UpstreamURL.String())
	log.Printf("Storage: %s", cfg.StoragePath)
	if cfg.CleanupDays > 0 {
		log.Printf("Cleanup: deleting files older than %d days", cfg.CleanupDays)
	}

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
