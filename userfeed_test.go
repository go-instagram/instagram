package instagram

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// emptyEdgesProfile is a web_profile_info body with the user + id but an empty
// media list — what Instagram now returns, triggering the feed fallback.
const emptyEdgesProfile = `{"data":{"user":{"id":"999","username":"acme","full_name":"ACME",` +
	`"biography":"bio","edge_followed_by":{"count":10},` +
	`"edge_owner_to_timeline_media":{"count":298,"edges":[]}}},"status":"ok"}`

// feedBody carries a photo post, a video post (empty owner → username fallback,
// highest-bitrate variant chosen) and a carousel (display taken from its first
// entry).
const feedBody = `{"status":"ok","items":[
  {"id":"1_9","code":"AAA","taken_at":1700000000,"media_type":1,"like_count":10,"comment_count":2,
   "caption":{"text":"photo post"},"user":{"username":"acme"},
   "image_versions2":{"candidates":[{"url":"https://cdn/big.jpg","width":1080,"height":1080},{"url":"https://cdn/small.jpg","width":320,"height":320}]}},
  {"id":"2_9","code":"BBB","taken_at":1700000100,"media_type":2,"like_count":5,"comment_count":1,
   "caption":{"text":"video post"},"user":{"username":""},
   "image_versions2":{"candidates":[{"url":"https://cdn/vthumb.jpg","width":640,"height":640}]},
   "video_versions":[{"url":"https://cdn/hi.mp4","width":720},{"url":"https://cdn/lo.mp4","width":480}]},
  {"id":"3_9","code":"CCC","taken_at":1700000200,"media_type":8,"like_count":0,"comment_count":0,
   "caption":{"text":"carousel"},"user":{"username":"acme"},
   "carousel_media":[{"image_versions2":{"candidates":[{"url":"https://cdn/c1.jpg","width":1080,"height":1080}]}},
                     {"video_versions":[{"url":"https://cdn/c2.mp4","width":720}]}]}
]}`

// routedServer answers web_profile_info with profileBody and the private feed with
// feedBody/feedStatus, so a UserProfile call exercises the empty-edges fallback.
func routedServer(t *testing.T, profileBody, feedBody string, feedStatus int) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "web_profile_info"):
			_, _ = w.Write([]byte(profileBody))
		case strings.Contains(r.URL.Path, "/feed/user/"):
			if feedStatus != 0 {
				w.WriteHeader(feedStatus)
			}
			_, _ = w.Write([]byte(feedBody))
		default:
			http.Error(w, "unexpected "+r.URL.Path, http.StatusInternalServerError)
		}
	}))
	t.Cleanup(srv.Close)
	return New(WithBaseURL(srv.URL), WithSessionID("secret"))
}

func TestUserProfileFallsBackToFeed(t *testing.T) {
	c := routedServer(t, emptyEdgesProfile, feedBody, 0)
	prof, err := c.UserProfile(context.Background(), "acme")
	if err != nil {
		t.Fatalf("UserProfile: %v", err)
	}
	if prof.Username != "acme" || prof.Followers != 10 {
		t.Errorf("profile metadata lost: %+v", prof)
	}
	if len(prof.Posts) != 3 {
		t.Fatalf("posts = %d, want 3", len(prof.Posts))
	}
	p0 := prof.Posts[0]
	if p0.ID != "1_9" || p0.Shortcode != "AAA" || p0.Caption != "photo post" ||
		p0.Owner != "acme" || p0.DisplayURL != "https://cdn/big.jpg" || p0.IsVideo ||
		p0.Likes != 10 || p0.Comments != 2 {
		t.Errorf("photo post wrong: %+v", p0)
	}
	if p0.Permalink != DefaultBaseURL+"/p/AAA/" || !p0.Timestamp.Equal(time.Unix(1700000000, 0).UTC()) {
		t.Errorf("photo post permalink/time wrong: %+v", p0)
	}
	p1 := prof.Posts[1]
	if p1.Owner != "acme" { // empty user → profile username
		t.Errorf("video owner = %q, want acme fallback", p1.Owner)
	}
	if !p1.IsVideo || p1.VideoURL != "https://cdn/hi.mp4" || p1.DisplayURL != "https://cdn/vthumb.jpg" {
		t.Errorf("video post wrong: %+v", p1)
	}
	p2 := prof.Posts[2]
	if p2.DisplayURL != "https://cdn/c1.jpg" || p2.IsVideo {
		t.Errorf("carousel post should show its first entry's image: %+v", p2)
	}
}

func TestUserProfileFeedError(t *testing.T) {
	c := routedServer(t, emptyEdgesProfile, ``, http.StatusTooManyRequests)
	_, err := c.UserProfile(context.Background(), "acme")
	if err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("want 429 from the feed fallback, got %v", err)
	}
}

func TestUserProfileEmptyEdgesNoSessionNoFallback(t *testing.T) {
	// No session: the empty-edges profile is returned as-is (no feed call).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/feed/user/") {
			t.Error("feed endpoint must not be called without a session")
		}
		_, _ = w.Write([]byte(emptyEdgesProfile))
	}))
	defer srv.Close()
	c := New(WithBaseURL(srv.URL)) // no session
	prof, err := c.UserProfile(context.Background(), "acme")
	if err != nil {
		t.Fatalf("UserProfile: %v", err)
	}
	if len(prof.Posts) != 0 {
		t.Errorf("posts = %d, want 0 (no fallback without a session)", len(prof.Posts))
	}
}

func TestUserProfileEmptyEdgesNoIDNoFallback(t *testing.T) {
	// Session present but the profile carries no id: nothing to query the feed by.
	noID := strings.Replace(emptyEdgesProfile, `"id":"999",`, ``, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/feed/user/") {
			t.Error("feed endpoint must not be called without a user id")
		}
		_, _ = w.Write([]byte(noID))
	}))
	defer srv.Close()
	c := New(WithBaseURL(srv.URL), WithSessionID("secret"))
	prof, err := c.UserProfile(context.Background(), "acme")
	if err != nil {
		t.Fatalf("UserProfile: %v", err)
	}
	if len(prof.Posts) != 0 {
		t.Errorf("posts = %d, want 0", len(prof.Posts))
	}
}

func TestUserPostsSuccessDirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if want := "/api/v1/feed/user/999/"; r.URL.Path != want {
			t.Errorf("path = %q, want %q", r.URL.Path, want)
		}
		if r.Header.Get("Cookie") != "sessionid=secret" {
			t.Errorf("cookie = %q", r.Header.Get("Cookie"))
		}
		_, _ = w.Write([]byte(feedBody))
	}))
	defer srv.Close()
	c := New(WithBaseURL(srv.URL), WithSessionID("secret"))
	posts, err := c.UserPosts(context.Background(), "999", "fallbackuser")
	if err != nil {
		t.Fatalf("UserPosts: %v", err)
	}
	if len(posts) != 3 {
		t.Fatalf("posts = %d, want 3", len(posts))
	}
}

func TestUserPostsStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	c := New(WithBaseURL(srv.URL), WithSessionID("s"))
	if _, err := c.UserPosts(context.Background(), "1", "u"); err == nil ||
		!strings.Contains(err.Error(), "403") {
		t.Fatalf("want 403, got %v", err)
	}
}

func TestUserPostsDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{not json`))
	}))
	defer srv.Close()
	c := New(WithBaseURL(srv.URL), WithSessionID("s"))
	if _, err := c.UserPosts(context.Background(), "1", "u"); err == nil ||
		!strings.Contains(err.Error(), "decode feed") {
		t.Fatalf("want decode error, got %v", err)
	}
}

func TestUserPostsBuildRequestError(t *testing.T) {
	c := New(WithBaseURL("http://\x7f.example"), WithSessionID("s"))
	if _, err := c.UserPosts(context.Background(), "1", "u"); err == nil ||
		!strings.Contains(err.Error(), "build request") {
		t.Fatalf("want build request error, got %v", err)
	}
}

func TestUserPostsRequestFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	c := New(WithBaseURL(url), WithSessionID("s"))
	if _, err := c.UserPosts(context.Background(), "1", "u"); err == nil {
		t.Fatal("want transport error, got nil")
	}
}

func TestUserPostsReadBodyError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "500")
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
	c := New(WithBaseURL(srv.URL), WithSessionID("s"))
	if _, err := c.UserPosts(context.Background(), "1", "u"); err == nil {
		t.Fatal("want read body error, got nil")
	}
}

func TestFeedMediaEmpty(t *testing.T) {
	// A media block with no candidates / no videos yields empty URLs.
	var m feedMedia
	if m.ImageVersions2.best() != "" || m.bestVideo() != "" {
		t.Error("empty media should map to empty URLs")
	}
	// A candidate with an empty URL is skipped.
	m.ImageVersions2.Candidates = []imageCandidate{{URL: "", Width: 999}}
	m.VideoVersions = []videoVersion{{URL: "", Width: 999}}
	if m.ImageVersions2.best() != "" || m.bestVideo() != "" {
		t.Error("empty-URL candidates should be skipped")
	}
}
