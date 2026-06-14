package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
)

type Config struct {
	UpstreamURL         *url.URL
	FallbackUpstreamURL *url.URL
	Host                string
	Port                int
	StoragePath         string
	MaxSessions         int
	EnableDownload      bool
	CleanupDays         int
}

func Load() (Config, error) {
	upstream := getenv("UPSTREAM_URL", "http://localhost:8096")
	upstreamURL, err := url.Parse(upstream)
	if err != nil {
		return Config{}, err
	}

	fallbackRaw := getenv("FALLBACK_UPSTREAM_URL", "")
	var fallbackUpstreamURL *url.URL
	if fallbackRaw != "" {
		fallbackUpstreamURL, err = url.Parse(fallbackRaw)
		if err != nil {
			return Config{}, fmt.Errorf("invalid FALLBACK_UPSTREAM_URL: %w", err)
		}
	}

	port, err := strconv.Atoi(getenv("PORT", "3000"))
	if err != nil {
		return Config{}, err
	}
	maxSessions, err := strconv.Atoi(getenv("MAX_SESSIONS", "1"))
	if err != nil {
		return Config{}, err
	}
	cleanupDays, err := strconv.Atoi(getenv("CLEANUP_DAYS", "0"))
	if err != nil {
		return Config{}, err
	}

	return Config{
		UpstreamURL:         upstreamURL,
		FallbackUpstreamURL: fallbackUpstreamURL,
		Host:                getenv("HOST", "0.0.0.0"),
		Port:                port,
		StoragePath:         getenv("STORAGE_PATH", "./storage"),
		MaxSessions:         maxSessions,
		EnableDownload:      os.Getenv("ENABLE_DOWNLOAD") == "1",
		CleanupDays:         cleanupDays,
	}, nil
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
