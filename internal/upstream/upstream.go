package upstream

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"emcache/internal/logging"
)

type Upstream struct {
	Primary          *url.URL
	Fallback         *url.URL
	Client           *http.Client
	stickyUntil      time.Time
	stickyMu         sync.Mutex
	fallbackDuration time.Duration
}

type Request struct {
	Method        string
	URL           *url.URL
	Body          io.ReadCloser
	GetBody       func() (io.ReadCloser, error)
	Header        http.Header
	ContentLength int64
	NoFallback    bool
	cachedBody    []byte // buffered body for retry replay
}

func New(primary, fallback *url.URL, client *http.Client, fallbackDuration time.Duration) *Upstream {
	if client == nil {
		client = NewClient()
	}
	return &Upstream{
		Primary:          primary,
		Fallback:         fallback,
		Client:           client,
		fallbackDuration: fallbackDuration,
	}
}

func (u *Upstream) Do(ctx context.Context, req *Request) (*http.Response, error) {
	// Check sticky fallback before trying primary
	if u.isFallbackSticky() {
		logging.Verbosef("[Upstream] sticky fallback active, skipping primary\n")
		return u.doWithBase(ctx, req, true)
	}
	return u.doWithBase(ctx, req, false)
}

func (u *Upstream) DoFallback(ctx context.Context, req *Request) (*http.Response, error) {
	return u.doWithBase(ctx, req, true)
}

func IsNetworkError(err error) bool {
	return isNetworkError(err)
}

// MarkFallback records that fallback succeeded; primary won't be used for
// fallbackDuration from now.
func (u *Upstream) MarkFallback() {
	u.stickyMu.Lock()
	defer u.stickyMu.Unlock()
	u.stickyUntil = time.Now().Add(u.fallbackDuration)
	logging.Verbosef("[Upstream] marked sticky fallback until %s (duration=%s)\n",
		u.stickyUntil.Format(time.RFC3339Nano), u.fallbackDuration)
}

// isFallbackSticky returns true if primary should be skipped in favor of fallback.
func (u *Upstream) isFallbackSticky() bool {
	if u.Fallback == nil || u.fallbackDuration <= 0 {
		return false
	}
	u.stickyMu.Lock()
	defer u.stickyMu.Unlock()
	return time.Now().Before(u.stickyUntil)
}

func (u *Upstream) doWithBase(ctx context.Context, req *Request, isFallback bool) (*http.Response, error) {
	base := u.Primary
	if isFallback {
		base = u.Fallback
	}

	var requestURL *url.URL
	if isFallback {
		requestURL = u.buildURL(base, req.URL)
	} else if req.URL.IsAbs() {
		requestURL = req.URL
	} else {
		requestURL = u.buildURL(base, req.URL)
	}

	var body io.Reader
	var bufferDone <-chan error     // set when parallel buffering is active
	var closePipeReader func()      // set when parallel buffering is active

	if isFallback {
		if req.GetBody != nil {
			rc, _ := req.GetBody()
			body = rc
		} else if req.cachedBody != nil {
			body = bytes.NewReader(req.cachedBody)
		} else {
			body = req.Body
		}
	} else {
		// Parallel buffer: drain body into cachedBody while simultaneously
		// feeding upstream via io.Pipe. If upstream fails, cachedBody is
		// already ready for fallback retry.
		if req.Body != nil && u.Fallback != nil && !req.NoFallback && req.cachedBody == nil {
			pipeBody, done, closeReader := startParallelBuffer(req)
			body = pipeBody
			bufferDone = done
			closePipeReader = closeReader
		} else {
			body = req.Body
			if req.GetBody != nil {
				if freshBody, bodyErr := req.GetBody(); bodyErr == nil {
					body = freshBody
				}
			}
		}
	}
	request, err := http.NewRequestWithContext(ctx, req.Method, requestURL.String(), body)
	if err != nil {
		if closePipeReader != nil {
			closePipeReader()
		}
		return nil, err
	}
	copyHeaders(request.Header, req.Header)
	request.Host = requestURL.Host
	if req.ContentLength > 0 {
		request.ContentLength = req.ContentLength
	}
	response, err := u.Client.Do(request)
	if err != nil {
		// Abort the pipe so the goroutine can finish buffering without
		// blocking on pipeWriter.Write.
		if closePipeReader != nil {
			closePipeReader()
		}
		// Wait for buffering to complete before fallback retry.
		if bufferDone != nil {
			if bufferErr := <-bufferDone; bufferErr != nil {
				return nil, bufferErr
			}
		}
		if u.Fallback != nil && !isFallback && !req.NoFallback && isNetworkError(err) {
			logging.Verbosef("[Upstream] failed %s: %v — retrying via fallback %s\n", request.URL.String(), err, u.Fallback.String())
			resp, fbErr := u.doWithBase(ctx, req, true)
			if fbErr != nil {
				logging.Verbosef("[Upstream] fallback also failed %s: %v\n", request.URL.String(), fbErr)
				return nil, fbErr
			}
			logging.Verbosef("[Upstream] fallback succeeded %s\n", request.URL.String())
			u.MarkFallback()
			return resp, nil
		}
		return nil, err
	}
	return response, nil
}

func (u *Upstream) BuildURL(requestURL *url.URL, isFallback bool) *url.URL {
	base := u.Primary
	if isFallback {
		base = u.Fallback
	}
	return u.buildURL(base, requestURL)
}

func (u *Upstream) buildURL(base *url.URL, requestURL *url.URL) *url.URL {
	upstreamURL := *base
	upstreamURL.Path = joinPath(base.Path, requestURL.Path)
	upstreamURL.RawQuery = requestURL.RawQuery
	upstreamURL.Fragment = ""
	return &upstreamURL
}

func copyHeaders(dst, src http.Header) {
	for key, values := range src {
		switch key {
		case "Connection", "Transfer-Encoding", "Keep-Alive",
			"Proxy-Authenticate", "Proxy-Authorization", "TE", "Trailer", "Upgrade":
			continue
		}
		dst[key] = append(dst[key], values...)
	}
}

func isNetworkError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return true
	}
	return false
}

func joinPath(base, path string) string {
	if base == "" || base == "/" {
		return path
	}
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")
}

// startParallelBuffer drains req.Body into req.cachedBody while simultaneously
// feeding a copy to the upstream via an io.Pipe. Returns the pipe-wrapped reader
// (to be used as the upstream request body), a channel that resolves when
// buffering is complete, and a function to close the pipe reader (unblocks the
// goroutine if the upstream never reads from the pipe).
func startParallelBuffer(req *Request) (io.Reader, <-chan error, func()) {
	pipeReader, pipeWriter := io.Pipe()
	var buf bytes.Buffer
	done := make(chan error, 1)

	go func() {
		chunk := make([]byte, 32*1024)
		pipeActive := true

		for {
			n, readErr := req.Body.Read(chunk)
			if n > 0 {
				// Buffer always succeeds.
				buf.Write(chunk[:n])

				// Pipe may fail if upstream closed the reader.
				if pipeActive {
					if _, writeErr := pipeWriter.Write(chunk[:n]); writeErr != nil {
						pipeWriter.CloseWithError(writeErr)
						pipeActive = false
					}
				}
			}
			if readErr != nil {
				if readErr != io.EOF {
					if pipeActive {
						pipeWriter.CloseWithError(readErr)
					}
					if closer, ok := req.Body.(io.Closer); ok {
						closer.Close()
					}
					done <- readErr
					return
				}
				break
			}
		}

		pipeWriter.Close()
		if closer, ok := req.Body.(io.Closer); ok {
			closer.Close()
		}
		req.cachedBody = buf.Bytes()
		done <- nil
	}()

	return pipeReader, done, func() { pipeReader.Close() }
}
