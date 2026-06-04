package cache

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"testing"
)

func TestValidateContentRange(t *testing.T) {
	if err := validateContentRange("bytes 0-99/1000", 0, 99, 1000); err != nil {
		t.Fatalf("validate content range: %v", err)
	}
}

func TestValidateContentRangeRejectsMismatch(t *testing.T) {
	if err := validateContentRange("bytes 0-98/1000", 0, 99, 1000); err == nil {
		t.Fatal("expected content range mismatch")
	}
}

func TestFetchSegmentClearsFirstPendingOnEarlyFailure(t *testing.T) {
	ctx := context.Background()
	file := newTestCachedFile(t, ChunkSize)
	pending, claimed := file.AwaitOrClaim(0)
	if !claimed {
		t.Fatal("claim chunk 0 failed")
	}

	_, err := fetchSegment(ctx, file, 0, pending, FetchOptions{
		Request:     &http.Request{Method: http.MethodGet, Header: make(http.Header)},
		UpstreamURL: mustURL(t, "http://upstream/video.mkv"),
		Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("upstream failed")
		})},
	})
	if err == nil {
		t.Fatal("expected fetch error")
	}
	if file.PendingCount() != 0 {
		t.Fatalf("pending count = %d, want 0", file.PendingCount())
	}
	if file.ChunkComplete(0) {
		t.Fatal("failed chunk was marked complete")
	}
}

func TestFetchSequentialWrapsToEarlierMissingChunkAndFinalizes(t *testing.T) {
	ctx := context.Background()
	file := newTestCachedFile(t, ChunkSize*3)
	if err := file.WriteChunk(ctx, 1, chunkBytes(1, ChunkSize)); err != nil {
		t.Fatalf("write chunk 1: %v", err)
	}
	pending, claimed := file.AwaitOrClaim(2)
	if !claimed {
		t.Fatal("claim chunk 2 failed")
	}

	err := fetchSequential(ctx, file, 2, pending, FetchOptions{
		Request:     &http.Request{Method: http.MethodGet, Header: make(http.Header)},
		UpstreamURL: mustURL(t, "http://upstream/video.mkv"),
		Client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.Header.Get("Range") {
			case fmt.Sprintf("bytes=%d-", ChunkSize*2):
				return partialResponse(req, ChunkSize*2, ChunkSize*3-1, ChunkSize*3, chunkBytes(2, ChunkSize)), nil
			case "bytes=0-":
				return partialResponse(req, 0, ChunkSize*3-1, ChunkSize*3, chunkBytes(0, ChunkSize)), nil
			default:
				return nil, fmt.Errorf("unexpected range %s", req.Header.Get("Range"))
			}
		})},
	})
	if err != nil {
		t.Fatalf("fetch sequential: %v", err)
	}
	if !file.Finalized() {
		t.Fatal("file was not finalized after wrap fill")
	}
	for i := 0; i < 3; i++ {
		if !file.ChunkComplete(i) {
			t.Fatalf("chunk %d incomplete", i)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newTestCachedFile(t *testing.T, size int64) *CachedFile {
	t.Helper()
	file, err := OpenCachedFile(context.Background(), t.TempDir(), testMediaSource(size), &fakeChunkStore{})
	if err != nil {
		t.Fatalf("open cached file: %v", err)
	}
	t.Cleanup(func() { _ = file.Close(context.Background()) })
	return file
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	return u
}

func partialResponse(req *http.Request, start, end, total int64, body []byte) *http.Response {
	return &http.Response{
		StatusCode: http.StatusPartialContent,
		Header: http.Header{
			"Content-Range":  []string{fmt.Sprintf("bytes %d-%d/%d", start, end, total)},
			"Content-Length": []string{fmt.Sprintf("%d", len(body))},
		},
		Body:    io.NopCloser(bytes.NewReader(body)),
		Request: req,
	}
}

func chunkBytes(chunk int, size int64) []byte {
	data := bytes.Repeat([]byte{byte(chunk + 1)}, int(size))
	return data
}
