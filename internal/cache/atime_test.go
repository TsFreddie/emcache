package cache

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestAccessTimeWorks(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("access time cleanup is only available on linux")
	}

	path := filepath.Join(t.TempDir(), "cache.mkv")
	if err := os.WriteFile(path, []byte("cache"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	oldTime := time.Now().AddDate(0, 0, -2)
	if err := os.Chtimes(path, oldTime, oldTime); err != nil {
		t.Fatalf("age file: %v", err)
	}

	if err := touchAccessTime(path); err != nil {
		t.Fatalf("touch access time: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}
	if !info.ModTime().Equal(oldTime) {
		t.Fatalf("mod time = %v, want %v", info.ModTime(), oldTime)
	}
	if !accessTime(info).After(oldTime) {
		t.Fatalf("access time = %v, want after %v", accessTime(info), oldTime)
	}
}

func TestCleanupOldFilesKeepsRecentlyAccessedOldFiles(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("access time cleanup is only available on linux")
	}

	ctx := context.Background()
	storagePath := t.TempDir()
	manager := NewManager(storagePath, mustURL(t, "http://upstream"), nil, nil, &fakeManagerStore{}, 0)

	oldTime := time.Now().AddDate(0, 0, -2)
	recentTime := time.Now()
	recentlyAccessedFile := filepath.Join(storagePath, "old-item", "recently-accessed.mkv")
	writeTestFile(t, recentlyAccessedFile, oldTime)
	if err := os.Chtimes(recentlyAccessedFile, recentTime, oldTime); err != nil {
		t.Fatalf("set recent access time: %v", err)
	}

	staleFile := filepath.Join(storagePath, "old-item", "stale.mkv")
	writeTestFile(t, staleFile, oldTime)

	manager.cleanupOldFiles(ctx, 1)

	if _, err := os.Stat(recentlyAccessedFile); err != nil {
		t.Fatalf("recently accessed file was removed: %v", err)
	}
	if _, err := os.Stat(staleFile); !os.IsNotExist(err) {
		t.Fatalf("stale file still exists or stat failed: %v", err)
	}
}
