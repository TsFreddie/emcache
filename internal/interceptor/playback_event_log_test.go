package interceptor

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPlaybackEventLogLimitsNewSessions(t *testing.T) {
	log := &PlaybackEventLog{MaxSessions: 1}

	response, handled, err := log.OnRequest(playbackContext("/emby/Sessions/Playing", `{"PlaySessionId":"one"}`))
	if err != nil || handled || response != nil {
		t.Fatalf("first session response=%v handled=%v err=%v", response, handled, err)
	}
	if active := log.active(); active != 1 {
		t.Fatalf("active sessions = %d, want 1", active)
	}

	response, handled, err = log.OnRequest(playbackContext("/emby/Sessions/Playing", `{"PlaySessionId":"two"}`))
	if err != nil {
		t.Fatalf("second session: %v", err)
	}
	if !handled || response == nil || response.StatusCode != http.StatusNoContent {
		t.Fatalf("second session response=%v handled=%v, want 204 handled", response, handled)
	}
	if active := log.active(); active != 1 {
		t.Fatalf("active sessions = %d, want 1", active)
	}
}

func TestPlaybackEventLogZeroBlocksEveryNewSession(t *testing.T) {
	log := &PlaybackEventLog{MaxSessions: 0}

	response, handled, err := log.OnRequest(playbackContext("/emby/Sessions/Playing", `{"PlaySessionId":"one"}`))
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	if !handled || response == nil || response.StatusCode != http.StatusNoContent {
		t.Fatalf("session response=%v handled=%v, want 204 handled", response, handled)
	}
	if active := log.active(); active != 0 {
		t.Fatalf("active sessions = %d, want 0", active)
	}

	response, handled, err = log.OnRequest(playbackContext("/emby/Sessions/Playing/Progress", `{"PlaySessionId":"one"}`))
	if err != nil {
		t.Fatalf("progress: %v", err)
	}
	if !handled || response == nil || response.StatusCode != http.StatusNoContent {
		t.Fatalf("progress response=%v handled=%v, want 204 handled", response, handled)
	}

	response, handled, err = log.OnRequest(playbackContext("/emby/Sessions/Playing/Stopped", `{"PlaySessionId":"one"}`))
	if err != nil {
		t.Fatalf("stopped: %v", err)
	}
	if !handled || response == nil || response.StatusCode != http.StatusNoContent {
		t.Fatalf("stopped response=%v handled=%v, want 204 handled", response, handled)
	}
}

func TestPlaybackEventLogNegativeDisablesLimit(t *testing.T) {
	log := &PlaybackEventLog{MaxSessions: -1}

	for _, playSessionID := range []string{"one", "two"} {
		response, handled, err := log.OnRequest(playbackContext("/emby/Sessions/Playing", `{"PlaySessionId":"`+playSessionID+`"}`))
		if err != nil || handled || response != nil {
			t.Fatalf("session %s response=%v handled=%v err=%v", playSessionID, response, handled, err)
		}
	}
	if active := log.active(); active != 0 {
		t.Fatalf("active sessions = %d, want 0", active)
	}
}

func TestPlaybackEventLogAllowsExistingSession(t *testing.T) {
	log := &PlaybackEventLog{MaxSessions: 1}

	_, _, err := log.OnRequest(playbackContext("/emby/Sessions/Playing", `{"PlaySessionId":"one"}`))
	if err != nil {
		t.Fatalf("first session: %v", err)
	}
	response, handled, err := log.OnRequest(playbackContext("/emby/Sessions/Playing/Progress", `{"PlaySessionId":"one"}`))
	if err != nil || handled || response != nil {
		t.Fatalf("progress response=%v handled=%v err=%v", response, handled, err)
	}
	if active := log.active(); active != 1 {
		t.Fatalf("active sessions = %d, want 1", active)
	}
}

func TestPlaybackEventLogExpiresIdleSession(t *testing.T) {
	log := &PlaybackEventLog{
		MaxSessions: 1,
		sessions: map[string]time.Time{
			"one": time.Now().Add(-playbackSessionTTL - time.Second),
		},
	}

	response, handled, err := log.OnRequest(playbackContext("/emby/Sessions/Playing", `{"PlaySessionId":"two"}`))
	if err != nil || handled || response != nil {
		t.Fatalf("new session response=%v handled=%v err=%v", response, handled, err)
	}
	if active := log.active(); active != 1 {
		t.Fatalf("active sessions = %d, want 1", active)
	}
	if !log.tracked("two") {
		t.Fatal("new session was not tracked")
	}
	if log.tracked("one") {
		t.Fatal("expired session is still tracked")
	}
}

func TestPlaybackEventLogPromotesUntrackedProgressWhenSlotAvailable(t *testing.T) {
	log := &PlaybackEventLog{MaxSessions: 1}
	ctx := playbackContext("/emby/Sessions/Playing/Progress", `{"PlaySessionId":"unknown","EventName":"TimeUpdate","RepeatMode":"RepeatNone","RunTimeTicks":123}`)

	response, handled, err := log.OnRequest(ctx)
	if err != nil || handled || response != nil {
		t.Fatalf("progress response=%v handled=%v err=%v", response, handled, err)
	}
	if ctx.Request.URL.Path != "/emby/Sessions/Playing" {
		t.Fatalf("path = %q, want /emby/Sessions/Playing", ctx.Request.URL.Path)
	}
	if active := log.active(); active != 1 {
		t.Fatalf("active sessions = %d, want 1", active)
	}
	body, err := io.ReadAll(ctx.Request.Body)
	if err != nil {
		t.Fatalf("read promoted body: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("parse promoted body: %v", err)
	}
	if _, ok := payload["NowPlayingQueue"]; !ok {
		t.Fatal("promoted body missing NowPlayingQueue")
	}
	for _, key := range []string{"EventName", "RepeatMode", "RunTimeTicks"} {
		if _, ok := payload[key]; ok {
			t.Fatalf("promoted body still has %s", key)
		}
	}
}

func TestPlaybackEventLogBlocksUntrackedProgressWhenFull(t *testing.T) {
	log := &PlaybackEventLog{MaxSessions: 1}
	_, _, err := log.OnRequest(playbackContext("/emby/Sessions/Playing", `{"PlaySessionId":"one"}`))
	if err != nil {
		t.Fatalf("first session: %v", err)
	}

	response, handled, err := log.OnRequest(playbackContext("/emby/Sessions/Playing/Progress", `{"PlaySessionId":"unknown"}`))
	if err != nil {
		t.Fatalf("progress: %v", err)
	}
	if !handled || response == nil || response.StatusCode != http.StatusNoContent {
		t.Fatalf("progress response=%v handled=%v, want 204 handled", response, handled)
	}
}

func TestPlaybackEventLogBlocksUntrackedStopped(t *testing.T) {
	log := &PlaybackEventLog{MaxSessions: 1}

	response, handled, err := log.OnRequest(playbackContext("/emby/Sessions/Playing/Stopped", `{"PlaySessionId":"unknown"}`))
	if err != nil {
		t.Fatalf("stopped: %v", err)
	}
	if !handled || response == nil || response.StatusCode != http.StatusNoContent {
		t.Fatalf("stopped response=%v handled=%v, want 204 handled", response, handled)
	}
}

func TestPlaybackEventLogReleasesStoppedSession(t *testing.T) {
	log := &PlaybackEventLog{MaxSessions: 1}

	_, _, err := log.OnRequest(playbackContext("/emby/Sessions/Playing", `{"PlaySessionId":"one"}`))
	if err != nil {
		t.Fatalf("first session: %v", err)
	}
	_, _, err = log.OnRequest(playbackContext("/emby/Sessions/Playing/Stopped", `{"PlaySessionId":"one"}`))
	if err != nil {
		t.Fatalf("stopped session: %v", err)
	}
	if active := log.active(); active != 0 {
		t.Fatalf("active sessions = %d, want 0", active)
	}

	response, handled, err := log.OnRequest(playbackContext("/emby/Sessions/Playing", `{"PlaySessionId":"two"}`))
	if err != nil || handled || response != nil {
		t.Fatalf("new session response=%v handled=%v err=%v", response, handled, err)
	}
}

func TestPlaybackEventLogRestoresRequestBody(t *testing.T) {
	log := &PlaybackEventLog{MaxSessions: 1}
	ctx := playbackContext("/emby/Sessions/Playing", `{"PlaySessionId":"one"}`)

	_, _, err := log.OnRequest(ctx)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	body, err := io.ReadAll(ctx.Request.Body)
	if err != nil {
		t.Fatalf("read restored body: %v", err)
	}
	if string(body) != `{"PlaySessionId":"one"}` {
		t.Fatalf("body = %q", string(body))
	}
}

func TestPlaybackEventLogRestoresResponseBody(t *testing.T) {
	log := &PlaybackEventLog{MaxSessions: 1}
	ctx := playbackContext("/emby/Sessions/Playing", `{"PlaySessionId":"one"}`)

	_, _, err := log.OnRequest(ctx)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	response := &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
	}
	response, err = log.OnResponse(ctx, response)
	if err != nil {
		t.Fatalf("response: %v", err)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read restored body: %v", err)
	}
	if string(body) != `{"ok":true}` {
		t.Fatalf("body = %q", string(body))
	}
	if response.ContentLength != int64(len(`{"ok":true}`)) {
		t.Fatalf("content length = %d", response.ContentLength)
	}
}

func playbackContext(path string, body string) *Context {
	req := httptest.NewRequest(http.MethodPost, path+"?reqformat=json", strings.NewReader(body))
	return &Context{Request: req}
}
