package upstream

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"encache/internal/logging"
)

type Upstream struct {
	Primary  *url.URL
	Fallback *url.URL
	Client   *http.Client
}

type Request struct {
	Method        string
	URL           *url.URL
	Body          io.ReadCloser
	GetBody       func() (io.ReadCloser, error)
	Header        http.Header
	ContentLength int64
	NoFallback    bool
}

func New(primary, fallback *url.URL, client *http.Client) *Upstream {
	if client == nil {
		client = NewClient()
	}
	return &Upstream{
		Primary:  primary,
		Fallback: fallback,
		Client:   client,
	}
}

func (u *Upstream) Do(ctx context.Context, req *Request) (*http.Response, error) {
	return u.doWithBase(ctx, req, false)
}

func (u *Upstream) DoFallback(ctx context.Context, req *Request) (*http.Response, error) {
	return u.doWithBase(ctx, req, true)
}

func IsNetworkError(err error) bool {
	return isNetworkError(err)
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

	var body io.ReadCloser
	if isFallback {
		if req.GetBody != nil {
			body, _ = req.GetBody()
		}
	} else {
		body = req.Body
		if req.GetBody != nil {
			if freshBody, bodyErr := req.GetBody(); bodyErr == nil {
				body = freshBody
			}
		}
	}
	request, err := http.NewRequestWithContext(ctx, req.Method, requestURL.String(), body)
	if err != nil {
		return nil, err
	}
	copyHeaders(request.Header, req.Header)
	request.Host = requestURL.Host
	if req.ContentLength > 0 {
		request.ContentLength = req.ContentLength
	}
	response, err := u.Client.Do(request)
	if err != nil {
		if u.Fallback != nil && !isFallback && !req.NoFallback && isNetworkError(err) {
			logging.Verbosef("[Upstream] failed %s: %v — retrying via fallback %s\n", request.URL.String(), err, u.Fallback.String())
			resp, fbErr := u.doWithBase(ctx, req, true)
			if fbErr != nil {
				logging.Verbosef("[Upstream] fallback also failed %s: %v\n", request.URL.String(), fbErr)
				return nil, fbErr
			}
			logging.Verbosef("[Upstream] fallback succeeded %s\n", request.URL.String())
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
		if isHopHeader(key) {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func isHopHeader(key string) bool {
	switch key {
	case "Connection", "Transfer-Encoding", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "TE", "Trailer", "Upgrade":
		return true
	}
	return false
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
