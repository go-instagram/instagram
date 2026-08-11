// Package instagram is a pure-Go, dependency-free, best-effort read client for
// public Instagram content served through Instagram's web JSON endpoints.
//
// This client is fragile BY NATURE. Instagram does not offer these endpoints as
// a stable public API: it changes, rate-limits, and locks them frequently, and
// it may reject unauthenticated requests outright. Reads therefore often require
// a valid logged-in "sessionid" cookie (see WithSessionID), and any request may
// break at any time when Instagram changes its endpoints, headers, response
// shape, or blocking policy. Treat non-2xx responses (particularly 401, 403 and
// 429) as signals that Instagram is blocking the request rather than as a bug in
// this library.
//
// The package deliberately hides this fragility behind a small, stable Go API so
// that callers can depend on the types even as the underlying transport shifts.
//
// It uses only the Go standard library and builds with CGO disabled.
package instagram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultBaseURL is the default Instagram web origin.
const DefaultBaseURL = "https://www.instagram.com"

// DefaultAppID is the public web app id Instagram's own site sends as the
// x-ig-app-id header. It is required by the web_profile_info endpoint.
const DefaultAppID = "936619743392459"

// DefaultUserAgent is a browser-like User-Agent used when none is configured.
const DefaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) " +
	"AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

// Post is a single piece of timeline media on a profile.
type Post struct {
	ID         string
	Shortcode  string
	Caption    string
	Owner      string // username
	Permalink  string // https://www.instagram.com/p/<shortcode>/
	DisplayURL string // main image
	IsVideo    bool
	VideoURL   string
	Likes      int
	Comments   int
	Timestamp  time.Time
}

// Profile is a public profile and its recent posts.
type Profile struct {
	Username  string
	FullName  string
	Biography string
	Followers int
	Posts     []Post
}

// Client is a best-effort Instagram web read client. Construct it with New.
type Client struct {
	// BaseURL is the origin requests are sent to. Defaults to DefaultBaseURL.
	BaseURL string
	// HTTPClient performs requests. Defaults to http.DefaultClient.
	HTTPClient *http.Client
	// UserAgent is sent as the User-Agent header.
	UserAgent string
	// SessionID, when set, is sent as the "sessionid" cookie to authenticate
	// reads. Instagram frequently requires this for public data.
	SessionID string
	// CSRFToken, when set, is sent as the "csrftoken" cookie and the X-CSRFToken
	// header. The private friendships endpoints (see [Client.Following]) require
	// it in addition to the sessionid.
	CSRFToken string
	// AppID is sent as the x-ig-app-id header. Defaults to DefaultAppID.
	AppID string
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient sets the http.Client used for requests.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.HTTPClient = h }
}

// WithBaseURL overrides the request origin (useful for testing).
func WithBaseURL(u string) Option {
	return func(c *Client) { c.BaseURL = u }
}

// WithUserAgent sets the User-Agent header.
func WithUserAgent(ua string) Option {
	return func(c *Client) { c.UserAgent = ua }
}

// WithSessionID sets the "sessionid" cookie sent with requests.
func WithSessionID(sessionID string) Option {
	return func(c *Client) { c.SessionID = sessionID }
}

// WithCSRFToken sets the "csrftoken" cookie / X-CSRFToken header sent with
// requests to the private friendships endpoints (see [Client.Following]).
func WithCSRFToken(token string) Option {
	return func(c *Client) { c.CSRFToken = token }
}

// New builds a Client with sane defaults, then applies the given options.
func New(opts ...Option) *Client {
	c := &Client{
		BaseURL:    DefaultBaseURL,
		HTTPClient: http.DefaultClient,
		UserAgent:  DefaultUserAgent,
		AppID:      DefaultAppID,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// webProfileInfo mirrors the subset of the web_profile_info response we consume.
type webProfileInfo struct {
	Data struct {
		User *struct {
			Username     string `json:"username"`
			FullName     string `json:"full_name"`
			Biography    string `json:"biography"`
			EdgeFollowed struct {
				Count int `json:"count"`
			} `json:"edge_followed_by"`
			Timeline struct {
				Edges []struct {
					Node mediaNode `json:"node"`
				} `json:"edges"`
			} `json:"edge_owner_to_timeline_media"`
		} `json:"user"`
	} `json:"data"`
}

type mediaNode struct {
	ID        string `json:"id"`
	Shortcode string `json:"shortcode"`
	Caption   struct {
		Edges []struct {
			Node struct {
				Text string `json:"text"`
			} `json:"node"`
		} `json:"edges"`
	} `json:"edge_media_to_caption"`
	Owner struct {
		Username string `json:"username"`
	} `json:"owner"`
	DisplayURL string `json:"display_url"`
	IsVideo    bool   `json:"is_video"`
	VideoURL   string `json:"video_url"`
	LikedBy    struct {
		Count int `json:"count"`
	} `json:"edge_liked_by"`
	PreviewLike struct {
		Count int `json:"count"`
	} `json:"edge_media_preview_like"`
	Comments struct {
		Count int `json:"count"`
	} `json:"edge_media_to_comment"`
	TakenAt int64 `json:"taken_at_timestamp"`
}

// UserProfile fetches a public profile and its recent posts.
//
// It requests GET {BaseURL}/api/v1/users/web_profile_info/?username=<u> with the
// x-ig-app-id header (and the sessionid cookie when configured). This endpoint is
// undocumented and may require authentication; see the package documentation for
// the fragility caveats.
func (c *Client) UserProfile(ctx context.Context, username string) (*Profile, error) {
	endpoint := strings.TrimRight(c.BaseURL, "/") +
		"/api/v1/users/web_profile_info/?username=" + url.QueryEscape(username)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("instagram: build request: %w", err)
	}
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("x-ig-app-id", c.AppID)
	if c.SessionID != "" {
		req.Header.Set("Cookie", "sessionid="+c.SessionID)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("instagram: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// 401/403/429 are the usual "Instagram is blocking you" signals.
		return nil, fmt.Errorf("instagram: unexpected status %d (%s); Instagram "+
			"may be blocking the request or require a valid sessionid cookie",
			resp.StatusCode, http.StatusText(resp.StatusCode))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("instagram: read body: %w", err)
	}

	var parsed webProfileInfo
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("instagram: decode response (endpoint may have "+
			"changed shape): %w", err)
	}
	if parsed.Data.User == nil {
		return nil, fmt.Errorf("instagram: no user in response for %q "+
			"(profile may be private, missing, or the endpoint changed)", username)
	}

	u := parsed.Data.User
	profile := &Profile{
		Username:  u.Username,
		FullName:  u.FullName,
		Biography: u.Biography,
		Followers: u.EdgeFollowed.Count,
	}
	profile.Posts = make([]Post, 0, len(u.Timeline.Edges))
	for _, edge := range u.Timeline.Edges {
		profile.Posts = append(profile.Posts, c.toPost(edge.Node, u.Username))
	}
	return profile, nil
}

// toPost converts a raw media node into a Post, falling back to the profile
// username as owner when the node omits it.
func (c *Client) toPost(n mediaNode, profileUser string) Post {
	caption := ""
	if len(n.Caption.Edges) > 0 {
		caption = n.Caption.Edges[0].Node.Text
	}

	owner := n.Owner.Username
	if owner == "" {
		owner = profileUser
	}

	likes := n.LikedBy.Count
	if likes == 0 {
		likes = n.PreviewLike.Count
	}

	return Post{
		ID:         n.ID,
		Shortcode:  n.Shortcode,
		Caption:    caption,
		Owner:      owner,
		Permalink:  DefaultBaseURL + "/p/" + n.Shortcode + "/",
		DisplayURL: n.DisplayURL,
		IsVideo:    n.IsVideo,
		VideoURL:   n.VideoURL,
		Likes:      likes,
		Comments:   n.Comments.Count,
		Timestamp:  time.Unix(n.TakenAt, 0).UTC(),
	}
}
