package cache

import (
	"context"
	"testing"

	"emby-proxy-cache/internal/store"
)

type fakeManagerStore struct {
	source store.MediaSource
	gets   int
}

func (s *fakeManagerStore) GetMediaSource(context.Context, string) (store.MediaSource, bool, error) {
	s.gets++
	return s.source, true, nil
}

func (s *fakeManagerStore) GetPreferredMediaSourceByItemID(context.Context, string) (store.MediaSource, bool, error) {
	s.gets++
	return s.source, true, nil
}

func (s *fakeManagerStore) UpdateChunks(context.Context, string, []byte) error {
	return nil
}

func TestOpenPreferredByItemIDUsesMediaSourceKey(t *testing.T) {
	source := testMediaSource(ChunkSize)
	source.MediaSourceID = "media-source-id"
	store := &fakeManagerStore{source: source}
	manager := NewManager(t.TempDir(), mustURL(t, "http://upstream"), store)

	first, err := manager.OpenPreferredByItemID(context.Background(), source.ItemID)
	if err != nil {
		t.Fatalf("open preferred: %v", err)
	}
	defer first.Close()

	second, err := manager.Open(context.Background(), source.MediaSourceID)
	if err != nil {
		t.Fatalf("open media source: %v", err)
	}
	defer second.Close()

	if first.File != second.File {
		t.Fatal("preferred item open and media source open did not share cached file")
	}
	if store.gets != 1 {
		t.Fatalf("store gets = %d, want 1", store.gets)
	}
}
