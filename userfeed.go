package instagram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// userFeedCount is how many recent posts UserPosts requests. Instagram's default
// page is around a dozen; this matches what the (now-empty) web timeline returned.
const userFeedCount = 12

// UserPosts returns an account's recent posts from the private feed endpoint
// /api/v1/feed/user/<id>/, which still serves the media to a logged-in session
// where the public web_profile_info endpoint now returns an empty list. userID is
// the account's numeric id (see [Client.UserProfile], which reads it from the
// profile and calls this automatically when the web timeline comes back empty);
// profileUser is used as the post owner when an item omits it.
//
// It requires a sessionid (see [WithSessionID]); Instagram answers an
// unauthenticated or blocked read with 401/403/429, surfaced as an error.
func (c *Client) UserPosts(ctx context.Context, userID, profileUser string) ([]Post, error) {
	endpoint := fmt.Sprintf("%s/api/v1/feed/user/%s/?count=%d",
		strings.TrimRight(c.BaseURL, "/"), userID, userFeedCount)

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
		return nil, fmt.Errorf("instagram: unexpected status %d (%s); Instagram "+
			"may be blocking the request or require a valid sessionid cookie",
			resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("instagram: read body: %w", err)
	}

	var parsed userFeedResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("instagram: decode feed response (endpoint may have "+
			"changed shape): %w", err)
	}
	posts := make([]Post, 0, len(parsed.Items))
	for _, it := range parsed.Items {
		posts = append(posts, mapFeedItem(it, profileUser))
	}
	return posts, nil
}

// mapFeedItem converts one private-feed item into a Post, taking the display image
// and any playable video from the item — or, for a carousel (media_type 8), from
// its first entry — to match the single-media-per-post shape the web timeline
// produced.
func mapFeedItem(it feedItem, profileUser string) Post {
	owner := it.User.Username
	if owner == "" {
		owner = profileUser
	}
	p := Post{
		ID:        it.ID,
		Shortcode: it.Code,
		Caption:   it.Caption.Text,
		Owner:     owner,
		Permalink: DefaultBaseURL + "/p/" + it.Code + "/",
		Likes:     it.LikeCount,
		Comments:  it.CommentCount,
		Timestamp: time.Unix(it.TakenAt, 0).UTC(),
	}
	node := it.mediaNode()
	p.DisplayURL = node.ImageVersions2.best()
	if v := node.bestVideo(); v != "" {
		p.IsVideo = true
		p.VideoURL = v
	}
	return p
}

// userFeedResponse mirrors the subset of the /feed/user/ response consumed.
type userFeedResponse struct {
	Items  []feedItem `json:"items"`
	Status string     `json:"status"`
}

// feedItem is one post in the private feed. A carousel post (MediaType 8) carries
// its photos/videos in CarouselMedia rather than at the top level.
type feedItem struct {
	ID           string `json:"id"`
	Code         string `json:"code"`
	TakenAt      int64  `json:"taken_at"`
	MediaType    int    `json:"media_type"`
	LikeCount    int    `json:"like_count"`
	CommentCount int    `json:"comment_count"`
	Caption      struct {
		Text string `json:"text"`
	} `json:"caption"`
	User struct {
		Username string `json:"username"`
	} `json:"user"`
	feedMedia
	CarouselMedia []feedMedia `json:"carousel_media"`
}

// mediaNode returns the media carrying this item's display image/video: the item
// itself, or the first entry of a carousel.
func (it feedItem) mediaNode() feedMedia {
	if len(it.CarouselMedia) > 0 {
		return it.CarouselMedia[0]
	}
	return it.feedMedia
}

// feedMedia is the image/video block shared by a top-level item and each carousel
// entry.
type feedMedia struct {
	ImageVersions2 imageVersions  `json:"image_versions2"`
	VideoVersions  []videoVersion `json:"video_versions"`
}

// bestVideo returns the highest-resolution progressive video URL, or "" when the
// media is a photo.
func (m feedMedia) bestVideo() string {
	best, bestW := "", -1
	for _, v := range m.VideoVersions {
		if v.URL != "" && v.Width > bestW {
			best, bestW = v.URL, v.Width
		}
	}
	return best
}

type imageVersions struct {
	Candidates []imageCandidate `json:"candidates"`
}

// best returns the largest candidate's URL (Instagram lists candidates
// largest-first, but pick by width so a reordering cannot downgrade the image).
func (iv imageVersions) best() string {
	best, bestW := "", -1
	for _, c := range iv.Candidates {
		if c.URL != "" && c.Width > bestW {
			best, bestW = c.URL, c.Width
		}
	}
	return best
}

type imageCandidate struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type videoVersion struct {
	URL   string `json:"url"`
	Width int    `json:"width"`
}
