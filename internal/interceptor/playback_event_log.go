package interceptor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"encache/internal/logging"
)

const playbackSessionTTL = 2 * time.Minute

type PlaybackEventLog struct {
	Base
	MaxSessions int
	mu          sync.Mutex
	sessions    map[string]time.Time
}

type playbackEvent struct {
	PlaySessionID string `json:"PlaySessionId"`
}

type playbackEventContextKey struct{}

type playbackEventContext struct {
	PlaySessionID string
}

func (p *PlaybackEventLog) OnRequest(ctx *Context) (*http.Response, bool, error) {
	req := ctx.Request
	if req.Method != http.MethodPost || req.Body == nil || !isPlaybackEventPath(req.URL.Path) {
		return nil, false, nil
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, false, err
	}
	_ = req.Body.Close()
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))

	logging.Verbosef("[PlaybackEvent] %s %s body=%s\n", req.Method, req.URL.RequestURI(), string(body))

	event, err := parsePlaybackEvent(body)
	if err != nil {
		logging.Verbosef("[PlaybackEvent] parse failed %s: %v\n", req.URL.Path, err)
		return nil, false, nil
	}
	if event.PlaySessionID == "" {
		logging.Verbosef("[PlaybackEvent] decision=passthrough reason=missing-play-session-id path=%s\n", req.URL.Path)
		return nil, false, nil
	}
	ctx.Request = ctx.Request.WithContext(contextWithPlaybackEvent(req, event.PlaySessionID).Context())
	req = ctx.Request
	if p.MaxSessions < 0 {
		logging.Verbosef("[PlaybackEvent] decision=passthrough reason=limit-disabled path=%s playSessionId=%s max=%d\n", req.URL.Path, event.PlaySessionID, p.MaxSessions)
		return nil, false, nil
	}

	switch req.URL.Path {
	case "/emby/Sessions/Playing":
		if !p.acquire(event.PlaySessionID) {
			fmt.Printf("[PlaybackEvent] decision=blocked path=%s playSessionId=%s active=%d max=%d\n", req.URL.Path, event.PlaySessionID, p.active(), p.MaxSessions)
			return playbackBlockedResponse(req), true, nil
		}
		logging.Verbosef("[PlaybackEvent] decision=passthrough path=%s playSessionId=%s active=%d max=%d\n", req.URL.Path, event.PlaySessionID, p.active(), p.MaxSessions)
	case "/emby/Sessions/Playing/Progress":
		if !p.tracked(event.PlaySessionID) {
			if !p.acquire(event.PlaySessionID) {
				fmt.Printf("[PlaybackEvent] decision=blocked reason=untracked-session path=%s playSessionId=%s active=%d max=%d\n", req.URL.Path, event.PlaySessionID, p.active(), p.MaxSessions)
				return playbackBlockedResponse(req), true, nil
			}
			if err := normalizePromotedProgressBody(req, body); err != nil {
				return nil, false, err
			}
			rewritePlaybackProgressToPlaying(req)
			fmt.Printf("[PlaybackEvent] decision=passthrough action=promoted-progress originalPath=/emby/Sessions/Playing/Progress path=%s playSessionId=%s active=%d max=%d\n", req.URL.Path, event.PlaySessionID, p.active(), p.MaxSessions)
			return nil, false, nil
		}
		logging.Verbosef("[PlaybackEvent] decision=passthrough path=%s playSessionId=%s active=%d max=%d\n", req.URL.Path, event.PlaySessionID, p.active(), p.MaxSessions)
	case "/emby/Sessions/Playing/Stopped":
		if !p.tracked(event.PlaySessionID) {
			fmt.Printf("[PlaybackEvent] decision=blocked reason=untracked-session path=%s playSessionId=%s active=%d max=%d\n", req.URL.Path, event.PlaySessionID, p.active(), p.MaxSessions)
			return playbackBlockedResponse(req), true, nil
		}
		if p.release(event.PlaySessionID) {
			fmt.Printf("[PlaybackEvent] decision=passthrough action=released path=%s playSessionId=%s active=%d max=%d\n", req.URL.Path, event.PlaySessionID, p.active(), p.MaxSessions)
		}
	}
	return nil, false, nil
}

func (p *PlaybackEventLog) OnResponse(ctx *Context, response *http.Response) (*http.Response, error) {
	if ctx.Request.Method != http.MethodPost || !isPlaybackEventPath(ctx.Request.URL.Path) {
		return response, nil
	}
	event, ok := playbackEventFromContext(ctx.Request)
	if !ok {
		return response, nil
	}
	statusCode := 0
	status := ""
	body := []byte(nil)
	if response != nil {
		statusCode = response.StatusCode
		status = response.Status
		if response.Body != nil {
			var err error
			body, err = io.ReadAll(response.Body)
			if err != nil {
				return nil, err
			}
			_ = response.Body.Close()
			response.Body = io.NopCloser(bytes.NewReader(body))
			response.ContentLength = int64(len(body))
			response.Header.Set("Content-Length", fmt.Sprintf("%d", len(body)))
		}
	}
	logging.Verbosef("[PlaybackEvent] response path=%s playSessionId=%s statusCode=%d status=%q body=%s\n", ctx.Request.URL.Path, event.PlaySessionID, statusCode, status, string(body))
	return response, nil
}

func parsePlaybackEvent(body []byte) (playbackEvent, error) {
	var event playbackEvent
	err := json.Unmarshal(body, &event)
	return event, err
}

func (p *PlaybackEventLog) acquire(playSessionID string) bool {
	if p.MaxSessions < 0 {
		return true
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.expireLocked(time.Now())
	if p.sessions == nil {
		p.sessions = make(map[string]time.Time)
	}
	if _, ok := p.sessions[playSessionID]; ok {
		p.sessions[playSessionID] = time.Now()
		return true
	}
	if len(p.sessions) >= p.MaxSessions {
		return false
	}
	p.sessions[playSessionID] = time.Now()
	return true
}

func (p *PlaybackEventLog) release(playSessionID string) bool {
	if p.MaxSessions < 0 {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.expireLocked(time.Now())
	if p.sessions == nil {
		return false
	}
	if _, ok := p.sessions[playSessionID]; !ok {
		return false
	}
	delete(p.sessions, playSessionID)
	return true
}

func (p *PlaybackEventLog) tracked(playSessionID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	p.expireLocked(now)
	_, ok := p.sessions[playSessionID]
	if ok {
		p.sessions[playSessionID] = now
	}
	return ok
}

func (p *PlaybackEventLog) active() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.expireLocked(time.Now())
	return len(p.sessions)
}

func (p *PlaybackEventLog) expireLocked(now time.Time) {
	for playSessionID, lastProgress := range p.sessions {
		if now.Sub(lastProgress) > playbackSessionTTL {
			delete(p.sessions, playSessionID)
			fmt.Printf("[PlaybackEvent] expired playSessionId=%s idle=%s active=%d max=%d\n", playSessionID, now.Sub(lastProgress).Round(time.Second), len(p.sessions), p.MaxSessions)
		}
	}
}

func contextWithPlaybackEvent(req *http.Request, playSessionID string) *http.Request {
	return req.WithContext(context.WithValue(req.Context(), playbackEventContextKey{}, playbackEventContext{PlaySessionID: playSessionID}))
}

func playbackEventFromContext(req *http.Request) (playbackEventContext, bool) {
	event, ok := req.Context().Value(playbackEventContextKey{}).(playbackEventContext)
	return event, ok
}

func playbackBlockedResponse(req *http.Request) *http.Response {
	return &http.Response{
		StatusCode:    http.StatusNoContent,
		Status:        "204 No Content",
		Header:        http.Header{},
		Body:          io.NopCloser(strings.NewReader("")),
		ContentLength: 0,
		Request:       req,
	}
}

func rewritePlaybackProgressToPlaying(req *http.Request) {
	req.URL.Path = "/emby/Sessions/Playing"
	req.URL.RawPath = ""
}

func normalizePromotedProgressBody(req *http.Request, body []byte) error {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return err
	}
	payload["NowPlayingQueue"] = []any{}
	delete(payload, "EventName")
	delete(payload, "RepeatMode")
	delete(payload, "RunTimeTicks")

	normalized, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req.Body = io.NopCloser(bytes.NewReader(normalized))
	req.ContentLength = int64(len(normalized))
	req.Header.Set("Content-Length", fmt.Sprintf("%d", len(normalized)))
	return nil
}

func isPlaybackEventPath(path string) bool {
	switch path {
	case "/emby/Sessions/Playing",
		"/emby/Sessions/Playing/Progress",
		"/emby/Sessions/Playing/Stopped":
		return true
	default:
		return false
	}
}
