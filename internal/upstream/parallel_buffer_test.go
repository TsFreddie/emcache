package upstream_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"encache/internal/upstream"
)

func TestParallelBufferFallback(t *testing.T) {
	var receivedBody []byte
	echoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body.Close()
		receivedBody = body
		w.Header().Set("X-Echo", "yes")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("echo OK"))
	}))
	defer echoServer.Close()

	fallbackURL, _ := url.Parse(echoServer.URL)
	primaryURL, _ := url.Parse("http://127.0.0.1:19999")
	u := upstream.New(primaryURL, fallbackURL, nil, 1*time.Minute)

	testBody := strings.Repeat("hello-world-", 5000)
	req := &upstream.Request{
		Method:        "POST",
		URL:           mustParse("/emby/Items/1/Download"),
		Body:          io.NopCloser(bytes.NewReader([]byte(testBody))),
		Header:        http.Header{},
		ContentLength: int64(len(testBody)),
	}

	resp, err := u.Do(t.Context(), req)
	if err != nil {
		t.Fatalf("Do failed: %v", err)
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(resp.Body)
	t.Logf("response: %s (status=%d)", string(respBytes), resp.StatusCode)
	t.Logf("echo server received: %d bytes", len(receivedBody))

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if len(receivedBody) != len(testBody) {
		t.Errorf("echo server received %d bytes, expected %d", len(receivedBody), len(testBody))
	}
	if string(receivedBody) != testBody {
		t.Errorf("body mismatch")
	}
}

func TestParallelBufferFallbackLargeBody(t *testing.T) {
	var receivedLen atomic.Int64
	echoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, _ := io.Copy(io.Discard, r.Body)
		r.Body.Close()
		receivedLen.Store(n)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("large OK"))
	}))
	defer echoServer.Close()

	fallbackURL, _ := url.Parse(echoServer.URL)
	primaryURL, _ := url.Parse("http://127.0.0.1:19999")
	u := upstream.New(primaryURL, fallbackURL, nil, 1*time.Minute)

	largeBody := bytes.Repeat([]byte("A"), 1024*1024) // 1 MB
	req := &upstream.Request{
		Method:        "POST",
		URL:           mustParse("/emby/Items/2/Download"),
		Body:          io.NopCloser(bytes.NewReader(largeBody)),
		Header:        http.Header{},
		ContentLength: int64(len(largeBody)),
	}

	resp, err := u.Do(t.Context(), req)
	if err != nil {
		t.Fatalf("Do failed: %v", err)
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(resp.Body)
	t.Logf("response: %s (status=%d)", string(respBytes), resp.StatusCode)
	t.Logf("echo server received: %d bytes", receivedLen.Load())

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if receivedLen.Load() != int64(len(largeBody)) {
		t.Errorf("echo server received %d, expected %d", receivedLen.Load(), len(largeBody))
	}
}

func TestParallelBufferPrimaryAvailable(t *testing.T) {
	// primary available: no fallback needed, body must reach primary correctly
	var primaryReceived []byte
	primarySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body.Close()
		primaryReceived = body
		w.Header().Set("X-Server", "primary")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("primary OK"))
	}))
	defer primarySrv.Close()

	fallbackSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("fallback should not be called when primary is available")
	}))
	defer fallbackSrv.Close()

	primaryURL, _ := url.Parse(primarySrv.URL)
	fallbackURL, _ := url.Parse(fallbackSrv.URL)
	u := upstream.New(primaryURL, fallbackURL, nil, 1*time.Minute)

	testBody := strings.Repeat("data-chunk-", 3000) // ~33KB
	req := &upstream.Request{
		Method:        "POST",
		URL:           mustParse("/emby/Items/42/Download"),
		Body:          io.NopCloser(bytes.NewReader([]byte(testBody))),
		Header:        http.Header{},
		ContentLength: int64(len(testBody)),
	}

	resp, err := u.Do(t.Context(), req)
	if err != nil {
		t.Fatalf("Do failed: %v", err)
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(resp.Body)
	t.Logf("response: %s (status=%d)", string(respBytes), resp.StatusCode)
	t.Logf("primary received: %d bytes", len(primaryReceived))

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if !strings.Contains(string(respBytes), "primary OK") {
		t.Errorf("response should come from primary, got: %s", string(respBytes))
	}
	if string(primaryReceived) != testBody {
		t.Errorf("body mismatch: primary got %d bytes, expected %d", len(primaryReceived), len(testBody))
	}
}

func TestParallelBufferPrimaryAvailableLargeBody(t *testing.T) {
	// 1 MB through primary — parallel buffering should still work transparently
	var primaryLen atomic.Int64
	primarySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, _ := io.Copy(io.Discard, r.Body)
		r.Body.Close()
		primaryLen.Store(n)
		w.Header().Set("X-Server", "primary")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("primary large OK"))
	}))
	defer primarySrv.Close()

	fallbackSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("fallback should not be called")
	}))
	defer fallbackSrv.Close()

	primaryURL, _ := url.Parse(primarySrv.URL)
	fallbackURL, _ := url.Parse(fallbackSrv.URL)
	u := upstream.New(primaryURL, fallbackURL, nil, 1*time.Minute)

	largeBody := bytes.Repeat([]byte("Z"), 2*1024*1024) // 2 MB
	req := &upstream.Request{
		Method:        "POST",
		URL:           mustParse("/emby/Items/99/Download"),
		Body:          io.NopCloser(bytes.NewReader(largeBody)),
		Header:        http.Header{},
		ContentLength: int64(len(largeBody)),
	}

	resp, err := u.Do(t.Context(), req)
	if err != nil {
		t.Fatalf("Do failed: %v", err)
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(resp.Body)
	t.Logf("response: %s (status=%d)", string(respBytes), resp.StatusCode)
	t.Logf("primary received: %d bytes", primaryLen.Load())

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if primaryLen.Load() != int64(len(largeBody)) {
		t.Errorf("primary received %d, expected %d", primaryLen.Load(), len(largeBody))
	}
}

func mustParse(raw string) *url.URL {
	u, _ := url.Parse(raw)
	return u
}