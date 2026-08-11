package instagram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// FollowedUser is one account the logged-in user follows, as returned by the
// friendships following list.
type FollowedUser struct {
	PK       string // the account's numeric id (as a string)
	Username string // the @handle, without the "@"
	FullName string // the display name ("" when the account sets none)
}

// FollowingPage is one page of the logged-in user's following list. NextMaxID is
// the cursor for the following page, and is "" when there are no more pages.
type FollowingPage struct {
	Users     []FollowedUser
	NextMaxID string
}

// CurrentUserID returns the logged-in user's numeric id, read from the private
// /api/v1/accounts/current_user/ endpoint. It requires a valid sessionid (see
// [WithSessionID]); without one Instagram answers 302→login or 4xx, surfaced as
// an error. The returned id is what [Client.Following] needs as its userID.
func (c *Client) CurrentUserID(ctx context.Context) (string, error) {
	body, err := c.authGet(ctx, "/api/v1/accounts/current_user/")
	if err != nil {
		return "", err
	}
	var parsed struct {
		User struct {
			PK   flexID `json:"pk"`
			PKID flexID `json:"pk_id"`
		} `json:"user"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("instagram: decode current_user (endpoint may have "+
			"changed shape): %w", err)
	}
	id := string(parsed.User.PK)
	if id == "" {
		id = string(parsed.User.PKID)
	}
	if id == "" {
		return "", fmt.Errorf("instagram: current_user carried no user id " +
			"(sessionid may be missing or invalid)")
	}
	return id, nil
}

// Following returns one page of the accounts userID follows, starting at maxID
// ("" for the first page). It requests
// GET {BaseURL}/api/v1/friendships/<userID>/following/ with the x-ig-app-id
// header and the sessionid (and, when configured, csrftoken) cookie — the same
// authentication the private web app sends. Page through the whole list by
// passing the previous page's NextMaxID until it comes back "".
//
// This endpoint is private and Instagram gates it aggressively: without a valid
// sessionid it answers 302→login or 401/403/429, all surfaced here as errors.
func (c *Client) Following(ctx context.Context, userID, maxID string) (*FollowingPage, error) {
	path := "/api/v1/friendships/" + url.PathEscape(userID) + "/following/"
	if maxID != "" {
		path += "?max_id=" + url.QueryEscape(maxID)
	}
	body, err := c.authGet(ctx, path)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Users []struct {
			PK       flexID `json:"pk"`
			PKID     flexID `json:"pk_id"`
			Username string `json:"username"`
			FullName string `json:"full_name"`
		} `json:"users"`
		NextMaxID string `json:"next_max_id"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("instagram: decode following list (endpoint may "+
			"have changed shape): %w", err)
	}
	page := &FollowingPage{NextMaxID: parsed.NextMaxID}
	for _, u := range parsed.Users {
		id := string(u.PK)
		if id == "" {
			id = string(u.PKID)
		}
		page.Users = append(page.Users, FollowedUser{
			PK:       id,
			Username: u.Username,
			FullName: u.FullName,
		})
	}
	return page, nil
}

// authGet performs an authenticated GET against a private web endpoint, setting
// the browser-like headers Instagram checks (User-Agent, x-ig-app-id) and the
// sessionid — plus, when configured, the csrftoken cookie and X-CSRFToken header
// the friendships endpoints require. It returns the response body, or an error
// for a build/transport failure or any non-2xx status (the "Instagram is
// blocking you" 302/401/403/429 signals).
func (c *Client) authGet(ctx context.Context, path string) ([]byte, error) {
	endpoint := strings.TrimRight(c.BaseURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("instagram: build request: %w", err)
	}
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("x-ig-app-id", c.AppID)
	cookie := "sessionid=" + c.SessionID
	if c.CSRFToken != "" {
		req.Header.Set("X-CSRFToken", c.CSRFToken)
		cookie += "; csrftoken=" + c.CSRFToken
	}
	req.Header.Set("Cookie", cookie)

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
	return body, nil
}

// flexID decodes an Instagram id that the API returns as either a JSON number
// (pk) or a quoted string (pk_id), yielding it as a plain string. A JSON null
// decodes to the empty string.
type flexID string

// UnmarshalJSON accepts a number, a quoted string, or null.
func (f *flexID) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "null" {
		s = ""
	}
	*f = flexID(s)
	return nil
}
