package instagram

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const sampleResponse = `{
  "data": {
    "user": {
      "username": "acme",
      "full_name": "ACME Corp",
      "biography": "We make everything.",
      "edge_followed_by": {"count": 4321},
      "edge_owner_to_timeline_media": {
        "edges": [
          {
            "node": {
              "id": "111",
              "shortcode": "ABC123",
              "edge_media_to_caption": {"edges": [{"node": {"text": "hello world"}}]},
              "owner": {"username": "acme"},
              "display_url": "https://cdn.example/img1.jpg",
              "is_video": false,
              "video_url": "",
              "edge_liked_by": {"count": 100},
              "edge_media_preview_like": {"count": 5},
              "edge_media_to_comment": {"count": 7},
              "taken_at_timestamp": 1700000000
            }
          },
          {
            "node": {
              "id": "222",
              "shortcode": "VID999",
              "edge_media_to_caption": {"edges": []},
              "owner": {"username": ""},
              "display_url": "https://cdn.example/img2.jpg",
              "is_video": true,
              "video_url": "https://cdn.example/vid2.mp4",
              "edge_liked_by": {"count": 0},
              "edge_media_preview_like": {"count": 42},
              "edge_media_to_comment": {"count": 3},
              "taken_at_timestamp": 1700000100
            }
          }
        ]
      }
    }
  }
}`

func TestNewDefaults(t *testing.T) {
	c := New()
	if c.BaseURL != DefaultBaseURL {
		t.Errorf("BaseURL = %q, want %q", c.BaseURL, DefaultBaseURL)
	}
	if c.HTTPClient != http.DefaultClient {
		t.Error("HTTPClient not defaulted")
	}
	if c.UserAgent != DefaultUserAgent {
		t.Errorf("UserAgent = %q", c.UserAgent)
	}
	if c.AppID != DefaultAppID {
		t.Errorf("AppID = %q", c.AppID)
	}
	if c.SessionID != "" {
		t.Errorf("SessionID = %q, want empty", c.SessionID)
	}
}

func TestOptions(t *testing.T) {
	hc := &http.Client{Timeout: time.Second}
	c := New(
		WithHTTPClient(hc),
		WithBaseURL("https://example.test/"),
		WithUserAgent("ua/1"),
		WithSessionID("sess-abc"),
	)
	if c.HTTPClient != hc {
		t.Error("WithHTTPClient not applied")
	}
	if c.BaseURL != "https://example.test/" {
		t.Errorf("WithBaseURL not applied: %q", c.BaseURL)
	}
	if c.UserAgent != "ua/1" {
		t.Errorf("WithUserAgent not applied: %q", c.UserAgent)
	}
	if c.SessionID != "sess-abc" {
		t.Errorf("WithSessionID not applied: %q", c.SessionID)
	}
}

func TestUserProfileSuccessWithSession(t *testing.T) {
	var gotCookie, gotAppID, gotUA, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCookie = r.Header.Get("Cookie")
		gotAppID = r.Header.Get("x-ig-app-id")
		gotUA = r.Header.Get("User-Agent")
		gotPath = r.URL.Path + "?" + r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sampleResponse))
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL), WithSessionID("secret"))
	prof, err := c.UserProfile(context.Background(), "acme")
	if err != nil {
		t.Fatalf("UserProfile: %v", err)
	}

	if gotCookie != "sessionid=secret" {
		t.Errorf("Cookie = %q", gotCookie)
	}
	if gotAppID != DefaultAppID {
		t.Errorf("x-ig-app-id = %q", gotAppID)
	}
	if gotUA != DefaultUserAgent {
		t.Errorf("User-Agent = %q", gotUA)
	}
	if want := "/api/v1/users/web_profile_info/?username=acme"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}

	if prof.Username != "acme" || prof.FullName != "ACME Corp" ||
		prof.Biography != "We make everything." || prof.Followers != 4321 {
		t.Errorf("profile fields wrong: %+v", prof)
	}
	if len(prof.Posts) != 2 {
		t.Fatalf("posts = %d, want 2", len(prof.Posts))
	}

	p0 := prof.Posts[0]
	if p0.ID != "111" || p0.Shortcode != "ABC123" || p0.Caption != "hello world" ||
		p0.Owner != "acme" || p0.DisplayURL != "https://cdn.example/img1.jpg" ||
		p0.IsVideo || p0.VideoURL != "" || p0.Likes != 100 || p0.Comments != 7 {
		t.Errorf("post0 wrong: %+v", p0)
	}
	if p0.Permalink != DefaultBaseURL+"/p/ABC123/" {
		t.Errorf("post0 permalink = %q", p0.Permalink)
	}
	if !p0.Timestamp.Equal(time.Unix(1700000000, 0).UTC()) {
		t.Errorf("post0 timestamp = %v", p0.Timestamp)
	}

	// Second post: empty caption edges, empty owner (falls back to username),
	// zero edge_liked_by (falls back to preview_like), video.
	p1 := prof.Posts[1]
	if p1.Caption != "" {
		t.Errorf("post1 caption = %q, want empty", p1.Caption)
	}
	if p1.Owner != "acme" {
		t.Errorf("post1 owner = %q, want fallback acme", p1.Owner)
	}
	if p1.Likes != 42 {
		t.Errorf("post1 likes = %d, want 42 (preview fallback)", p1.Likes)
	}
	if !p1.IsVideo || p1.VideoURL != "https://cdn.example/vid2.mp4" {
		t.Errorf("post1 video wrong: %+v", p1)
	}
}

func TestUserProfileNoSessionNoCookie(t *testing.T) {
	var hadCookie bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hadCookie = r.Header["Cookie"]
		_, _ = w.Write([]byte(`{"data":{"user":{"username":"x","edge_owner_to_timeline_media":{"edges":[]}}}}`))
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL)) // no session
	prof, err := c.UserProfile(context.Background(), "x")
	if err != nil {
		t.Fatalf("UserProfile: %v", err)
	}
	if hadCookie {
		t.Error("Cookie header set despite no SessionID")
	}
	if len(prof.Posts) != 0 {
		t.Errorf("posts = %d, want 0", len(prof.Posts))
	}
}

func TestUserProfileNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	_, err := c.UserProfile(context.Background(), "acme")
	if err == nil {
		t.Fatal("expected error for 403")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error missing status: %v", err)
	}
}

func TestUserProfileDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{not json`))
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	_, err := c.UserProfile(context.Background(), "acme")
	if err == nil || !strings.Contains(err.Error(), "decode response") {
		t.Fatalf("expected decode error, got %v", err)
	}
}

func TestUserProfileNilUser(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"user":null}}`))
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	_, err := c.UserProfile(context.Background(), "ghost")
	if err == nil || !strings.Contains(err.Error(), "no user") {
		t.Fatalf("expected no-user error, got %v", err)
	}
}

func TestUserProfileBadRequest(t *testing.T) {
	// Invalid control character in URL makes http.NewRequestWithContext fail.
	c := New(WithBaseURL("http://\x7f.example"))
	_, err := c.UserProfile(context.Background(), "acme")
	if err == nil || !strings.Contains(err.Error(), "build request") {
		t.Fatalf("expected build request error, got %v", err)
	}
}

func TestUserProfileRequestFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // connection will be refused

	c := New(WithBaseURL(url))
	_, err := c.UserProfile(context.Background(), "acme")
	if err == nil || !strings.Contains(err.Error(), "request failed") {
		t.Fatalf("expected request failed error, got %v", err)
	}
}

func TestUserProfileReadBodyError(t *testing.T) {
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
	_, err := c.UserProfile(context.Background(), "acme")
	if err == nil || !strings.Contains(err.Error(), "read body") {
		t.Fatalf("expected read body error, got %v", err)
	}
}
