package proxy

import (
	"io"
	"net/http"
	"testing"
)

func TestCountingWriterBatchesFlushes(t *testing.T) {
	writer := &flushRecorder{}
	counter := newCountingWriter(writer, "/video", true)

	for i := 0; i < int(flushBytes/copyBufferSize)-1; i++ {
		if _, err := counter.Write(make([]byte, copyBufferSize)); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	if writer.flushes != 0 {
		t.Fatalf("flushes before threshold = %d, want 0", writer.flushes)
	}

	if _, err := counter.Write(make([]byte, copyBufferSize)); err != nil {
		t.Fatalf("write threshold: %v", err)
	}
	if writer.flushes != 1 {
		t.Fatalf("flushes at threshold = %d, want 1", writer.flushes)
	}
}

func TestCountingWriterFinalFlush(t *testing.T) {
	writer := &flushRecorder{}
	counter := newCountingWriter(writer, "/video", true)

	if _, err := counter.Write([]byte("partial")); err != nil {
		t.Fatalf("write: %v", err)
	}
	counter.Flush()
	if writer.flushes != 1 {
		t.Fatalf("flushes = %d, want 1", writer.flushes)
	}
	counter.Flush()
	if writer.flushes != 1 {
		t.Fatalf("empty flush changed count to %d, want 1", writer.flushes)
	}
}

func TestIsStreamResponseIgnoresSubtitleRanges(t *testing.T) {
	response := &http.Response{
		StatusCode: http.StatusPartialContent,
		Header: http.Header{
			"Content-Type": []string{"text/x-ssa"},
		},
	}

	if isStreamResponse(response) {
		t.Fatal("subtitle range was classified as stream")
	}
}

func TestIsStreamResponseMatchesVideoWithParameters(t *testing.T) {
	response := &http.Response{
		StatusCode: http.StatusPartialContent,
		Header: http.Header{
			"Content-Type": []string{"video/x-matroska; charset=binary"},
		},
	}

	if !isStreamResponse(response) {
		t.Fatal("video response was not classified as stream")
	}
}

func TestWriteResponseFlushesMarkedHeadersBeforeReadingBody(t *testing.T) {
	body := &blockingReader{readStarted: make(chan struct{}), unblock: make(chan struct{})}
	writer := &flushRecorder{header: make(http.Header)}
	response := &http.Response{
		StatusCode: http.StatusPartialContent,
		Header: http.Header{
			"Content-Type":               []string{"application/octet-stream"},
			"X-Emby-Proxy-Flush-Headers": []string{"1"},
		},
		Body: body,
	}

	done := make(chan struct{})
	go func() {
		(&Proxy{}).writeResponse(writer, mustRequest(t), response)
		close(done)
	}()

	<-body.readStarted
	if writer.flushes != 1 {
		t.Fatalf("flushes before body unblocked = %d, want 1", writer.flushes)
	}
	if values := writer.header.Values("X-Emby-Proxy-Flush-Headers"); len(values) != 0 {
		t.Fatalf("internal flush header was sent: %v", values)
	}

	close(body.unblock)
	<-done
}

type flushRecorder struct {
	header  http.Header
	flushes int
}

func (w *flushRecorder) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *flushRecorder) Write(data []byte) (int, error) {
	return len(data), nil
}

func (w *flushRecorder) WriteHeader(int) {}

func (w *flushRecorder) Flush() {
	w.flushes++
}

type blockingReader struct {
	readStarted chan struct{}
	unblock     chan struct{}
}

func (r *blockingReader) Read([]byte) (int, error) {
	if r.readStarted != nil {
		close(r.readStarted)
		r.readStarted = nil
	}
	<-r.unblock
	return 0, io.EOF
}

func (r *blockingReader) Close() error {
	return nil
}

func mustRequest(t *testing.T) *http.Request {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, "/emby/items/1/download", nil)
	if err != nil {
		t.Fatal(err)
	}
	return request
}
