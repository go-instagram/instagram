package instagram

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// authCapture records the auth-bearing headers of the last request, so tests can
// assert the private endpoints send exactly what Instagram checks.
type authCapture struct {
	path      string
	appID     string
	cookie    string
	csrf      string
	userAgent string
}

func newAuthServer(t *testing.T, cap *authCapture, status int, body string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.path = r.URL.Path
		if r.URL.RawQuery != "" {
			cap.path += "?" + r.URL.RawQuery
		}
		cap.appID = r.Header.Get("x-ig-app-id")
		cap.cookie = r.Header.Get("Cookie")
		cap.csrf = r.Header.Get("X-CSRFToken")
		cap.userAgent = r.Header.Get("User-Agent")
		if status != 0 {
			w.WriteHeader(status)
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return New(WithBaseURL(srv.URL), WithSessionID("sess-1"), WithCSRFToken("csrf-1"))
}

func TestWithCSRFTokenOptionAndDefault(t *testing.T) {
	if New().CSRFToken != "" {
		t.Errorf("default CSRFToken = %q, want empty", New().CSRFToken)
	}
	if c := New(WithCSRFToken("tok")); c.CSRFToken != "tok" {
		t.Errorf("WithCSRFToken not applied: %q", c.CSRFToken)
	}
}

func TestCurrentUserIDSuccess(t *testing.T) {
	var cap authCapture
	c := newAuthServer(t, &cap, 0, `{"user":{"pk":12345,"pk_id":"12345","username":"me"},"status":"ok"}`)

	id, err := c.CurrentUserID(context.Background())
	if err != nil {
		t.Fatalf("CurrentUserID: %v", err)
	}
	if id != "12345" {
		t.Errorf("id = %q, want 12345", id)
	}
	if cap.path != "/api/v1/accounts/current_user/" {
		t.Errorf("path = %q", cap.path)
	}
	if cap.appID != DefaultAppID {
		t.Errorf("x-ig-app-id = %q", cap.appID)
	}
	if cap.userAgent != DefaultUserAgent {
		t.Errorf("User-Agent = %q", cap.userAgent)
	}
	if cap.csrf != "csrf-1" {
		t.Errorf("X-CSRFToken = %q, want csrf-1", cap.csrf)
	}
	if cap.cookie != "sessionid=sess-1; csrftoken=csrf-1" {
		t.Errorf("Cookie = %q", cap.cookie)
	}
}

func TestCurrentUserIDPKIDFallback(t *testing.T) {
	var cap authCapture
	// pk is JSON null, so the id comes from the quoted pk_id.
	c := newAuthServer(t, &cap, 0, `{"user":{"pk":null,"pk_id":"999"}}`)
	id, err := c.CurrentUserID(context.Background())
	if err != nil {
		t.Fatalf("CurrentUserID: %v", err)
	}
	if id != "999" {
		t.Errorf("id = %q, want 999 (pk_id fallback)", id)
	}
}

func TestCurrentUserIDNoID(t *testing.T) {
	var cap authCapture
	c := newAuthServer(t, &cap, 0, `{"user":{}}`)
	_, err := c.CurrentUserID(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no user id") {
		t.Fatalf("expected no-user-id error, got %v", err)
	}
}

func TestCurrentUserIDDecodeError(t *testing.T) {
	var cap authCapture
	c := newAuthServer(t, &cap, 0, `{not json`)
	_, err := c.CurrentUserID(context.Background())
	if err == nil || !strings.Contains(err.Error(), "decode current_user") {
		t.Fatalf("expected decode error, got %v", err)
	}
}

func TestCurrentUserIDStatusError(t *testing.T) {
	var cap authCapture
	c := newAuthServer(t, &cap, http.StatusForbidden, ``)
	_, err := c.CurrentUserID(context.Background())
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("expected 403 error, got %v", err)
	}
}

// TestCurrentUserIDFromSessionNoNetwork proves the id is read straight from the
// sessionid cookie: the endpoint (which now 400s for web sessions) is never hit.
func TestCurrentUserIDFromSessionNoNetwork(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request to %s: the id must come from the sessionid", r.URL.Path)
	}))
	t.Cleanup(srv.Close)
	// sessionid "<id>%3A<token>%3A<…>" (URL-encoded "<id>:<token>:…").
	c := New(WithBaseURL(srv.URL), WithSessionID("17841400000000000%3AAbCdEf%3A29"))
	id, err := c.CurrentUserID(context.Background())
	if err != nil {
		t.Fatalf("CurrentUserID: %v", err)
	}
	if id != "17841400000000000" {
		t.Errorf("id = %q, want 17841400000000000 (from the sessionid)", id)
	}
}

func TestViewerIDFromSessionID(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"url encoded", "17841400000000000%3AAbCdEf%3A29", "17841400000000000"},
		{"already decoded", "12345678:tok:29", "12345678"},
		{"all digits", "12345", "12345"},
		{"non-numeric session", "sess-1", ""},
		{"empty", "", ""},
		{"digits are a prefix, not a field", "123abc:tok", ""},
		{"invalid escape kept verbatim", "1234%", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := viewerIDFromSessionID(tc.in); got != tc.want {
				t.Errorf("viewerIDFromSessionID(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestFollowingFirstPage(t *testing.T) {
	var cap authCapture
	c := newAuthServer(t, &cap, 0, `{
		"users": [
			{"pk": 111, "pk_id": "111", "username": "alice", "full_name": "Alice"},
			{"pk": null, "pk_id": "222", "username": "bob", "full_name": ""}
		],
		"next_max_id": "CURSOR2",
		"status": "ok"
	}`)

	page, err := c.Following(context.Background(), "12345", "")
	if err != nil {
		t.Fatalf("Following: %v", err)
	}
	if cap.path != "/api/v1/friendships/12345/following/" {
		t.Errorf("path = %q (first page must carry no max_id)", cap.path)
	}
	if page.NextMaxID != "CURSOR2" {
		t.Errorf("NextMaxID = %q, want CURSOR2", page.NextMaxID)
	}
	if len(page.Users) != 2 {
		t.Fatalf("users = %d, want 2", len(page.Users))
	}
	if page.Users[0] != (FollowedUser{PK: "111", Username: "alice", FullName: "Alice"}) {
		t.Errorf("user0 = %+v", page.Users[0])
	}
	// pk null → pk_id fallback.
	if page.Users[1] != (FollowedUser{PK: "222", Username: "bob", FullName: ""}) {
		t.Errorf("user1 = %+v", page.Users[1])
	}
}

func TestFollowingWithCursor(t *testing.T) {
	var cap authCapture
	c := newAuthServer(t, &cap, 0, `{"users":[],"next_max_id":""}`)
	page, err := c.Following(context.Background(), "12345", "CURSOR2")
	if err != nil {
		t.Fatalf("Following: %v", err)
	}
	if !strings.Contains(cap.path, "max_id=CURSOR2") {
		t.Errorf("path = %q, want max_id=CURSOR2", cap.path)
	}
	if page.NextMaxID != "" || len(page.Users) != 0 {
		t.Errorf("page = %+v, want empty terminal page", page)
	}
}

func TestFollowingDecodeError(t *testing.T) {
	var cap authCapture
	c := newAuthServer(t, &cap, 0, `{not json`)
	_, err := c.Following(context.Background(), "1", "")
	if err == nil || !strings.Contains(err.Error(), "decode following list") {
		t.Fatalf("expected decode error, got %v", err)
	}
}

func TestFollowingNoCSRF(t *testing.T) {
	var cap authCapture
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.cookie = r.Header.Get("Cookie")
		cap.csrf = r.Header.Get("X-CSRFToken")
		_, _ = w.Write([]byte(`{"users":[],"next_max_id":""}`))
	}))
	defer srv.Close()

	// No CSRFToken configured: the cookie carries only sessionid and no
	// X-CSRFToken header is sent.
	c := New(WithBaseURL(srv.URL), WithSessionID("only-sess"))
	if _, err := c.Following(context.Background(), "1", ""); err != nil {
		t.Fatalf("Following: %v", err)
	}
	if cap.cookie != "sessionid=only-sess" {
		t.Errorf("Cookie = %q, want just sessionid", cap.cookie)
	}
	if cap.csrf != "" {
		t.Errorf("X-CSRFToken = %q, want empty", cap.csrf)
	}
}

func TestFollowingBuildRequestError(t *testing.T) {
	c := New(WithBaseURL("http://\x7f.example"))
	_, err := c.Following(context.Background(), "1", "")
	if err == nil || !strings.Contains(err.Error(), "build request") {
		t.Fatalf("expected build request error, got %v", err)
	}
}

func TestFollowingRequestFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // connection refused

	c := New(WithBaseURL(url))
	_, err := c.Following(context.Background(), "1", "")
	if err == nil || !strings.Contains(err.Error(), "request failed") {
		t.Fatalf("expected request failed error, got %v", err)
	}
}

func TestFollowingReadBodyError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "500") // lie: send fewer bytes then hang up
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			_, _ = w.Write([]byte("short"))
			f.Flush()
		}
		if hj, ok := w.(http.Hijacker); ok {
			conn, _, _ := hj.Hijack()
			_ = conn.Close()
		}
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	_, err := c.Following(context.Background(), "1", "")
	if err == nil || !strings.Contains(err.Error(), "read body") {
		t.Fatalf("expected read body error, got %v", err)
	}
}
