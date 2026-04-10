package tracker

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

type BTNTracker struct {
	*BaseTracker
}

func init() {
	Register(TrackerTypeBTN, func(cfg *TrackerConfig) (Tracker, error) {
		return NewBTNTracker(cfg), nil
	})
}

func NewBTNTracker(cfg *TrackerConfig) *BTNTracker {
	return &BTNTracker{
		BaseTracker: NewBaseTracker(cfg),
	}
}

func (t *BTNTracker) Authenticate(ctx context.Context) error {
	if t.config.Username == "" || t.config.Password == "" {
		if t.config.APIKey == "" {
			return fmt.Errorf("%w: username/password or API key required", ErrAuthenticationFailed)
		}
		t.mu.Lock()
		t.authed = true
		t.config.LastAuth = time.Now()
		t.mu.Unlock()
		return nil
	}

	baseURL := t.config.URL
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}

	loginURL := baseURL + "login.php"

	data := url.Values{}
	data.Set("username", t.config.Username)
	data.Set("password", t.config.Password)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, loginURL, strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusFound {
		return fmt.Errorf("%w: status %d", ErrAuthenticationFailed, resp.StatusCode)
	}

	cookies := resp.Cookies()
	var sessionCookie string

	for _, cookie := range cookies {
		if cookie.Name == "session" || cookie.Name == "keeploggedin" {
			sessionCookie = cookie.Value
		}
	}

	if sessionCookie == "" {
		return fmt.Errorf("%w: no session cookie received", ErrAuthenticationFailed)
	}

	t.mu.Lock()
	t.authed = true
	t.config.Cookie = fmt.Sprintf("session=%s", sessionCookie)
	t.config.CookieExpiry = time.Now().Add(7 * 24 * time.Hour)
	t.config.LastAuth = time.Now()
	t.mu.Unlock()

	return nil
}

func (t *BTNTracker) RefreshAuth(ctx context.Context) error {
	if !t.IsAuthenticated() {
		return t.Authenticate(ctx)
	}

	t.mu.RLock()
	expiry := t.config.CookieExpiry
	t.mu.RUnlock()

	if time.Until(expiry) < 24*time.Hour {
		return t.Authenticate(ctx)
	}

	return nil
}

func (t *BTNTracker) BuildAnnounceURL(req AnnounceParams) (string, error) {
	baseURL := t.config.URL
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}

	announceURL := baseURL + "announce"

	u, err := url.Parse(announceURL)
	if err != nil {
		return "", fmt.Errorf("parse announce URL: %w", err)
	}

	q := u.Query()
	q.Set("info_hash", string(req.InfoHash[:]))
	q.Set("peer_id", string(req.PeerID[:]))
	q.Set("port", fmt.Sprintf("%d", req.Port))
	q.Set("uploaded", fmt.Sprintf("%d", req.Uploaded))
	q.Set("downloaded", fmt.Sprintf("%d", req.Downloaded))
	q.Set("left", fmt.Sprintf("%d", req.Left))
	q.Set("compact", "1")

	if req.Event != "" {
		q.Set("event", req.Event)
	}

	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (t *BTNTracker) Test(ctx context.Context) error {
	if t.config.APIKey == "" && (t.config.Username == "" || t.config.Password == "") {
		return fmt.Errorf("%w: API key or username/password required", ErrInvalidConfig)
	}

	if t.config.APIKey != "" {
		t.mu.Lock()
		t.authed = true
		t.mu.Unlock()
		return nil
	}

	if err := t.Authenticate(ctx); err != nil {
		return err
	}

	baseURL := t.config.URL
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}

	testURL := baseURL + "index.php"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, testURL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	t.mu.RLock()
	cookie := t.config.Cookie
	t.mu.RUnlock()

	req.Header.Set("Cookie", cookie)

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("%w: session expired", ErrAuthenticationFailed)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: status %d", ErrTestFailed, resp.StatusCode)
	}

	return nil
}

func (t *BTNTracker) Get(ctx context.Context, path string) ([]byte, error) {
	return t.BaseTracker.Get(ctx, path)
}

func (t *BTNTracker) Post(ctx context.Context, path string, body io.Reader) ([]byte, error) {
	return t.BaseTracker.Post(ctx, path, body)
}

func (t *BTNTracker) GetTorrents(ctx context.Context, search string) ([]BTNTorrent, error) {
	baseURL := t.config.URL
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}

	apiURL := baseURL + "api/torrents.php"

	u, err := url.Parse(apiURL)
	if err != nil {
		return nil, fmt.Errorf("parse URL: %w", err)
	}

	q := u.Query()
	if t.config.APIKey != "" {
		q.Set("api_key", t.config.APIKey)
	}
	if search != "" {
		q.Set("search", search)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	t.mu.RLock()
	cookie := t.config.Cookie
	t.mu.RUnlock()
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrNotAuthenticated
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrRequestFailed, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var result []BTNTorrent
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	return result, nil
}

func (t *BTNTracker) GetUserStats(ctx context.Context) (*BTNUserStats, error) {
	baseURL := t.config.URL
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}

	apiURL := baseURL + "api/user.php"

	u, err := url.Parse(apiURL)
	if err != nil {
		return nil, fmt.Errorf("parse URL: %w", err)
	}

	q := u.Query()
	if t.config.APIKey != "" {
		q.Set("api_key", t.config.APIKey)
	}
	q.Set("action", "userstats")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	t.mu.RLock()
	cookie := t.config.Cookie
	t.mu.RUnlock()
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrNotAuthenticated
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrRequestFailed, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var stats BTNUserStats
	if err := json.Unmarshal(body, &stats); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	return &stats, nil
}

type BTNTorrent struct {
	GroupID        int    `json:"groupId"`
	TorrentID      int    `json:"torrentId"`
	GroupName      string `json:"groupName"`
	Artist         string `json:"artist"`
	Codec          string `json:"codec"`
	Resolution     string `json:"resolution"`
	Region         string `json:"region"`
	Container      string `json:"container"`
	Format         string `json:"format"`
	Encoding       string `json:"encoding"`
	Size           int64  `json:"size"`
	Seeders        int    `json:"seeders"`
	Leechers       int    `json:"leechers"`
	Snatched       int    `json:"snatched"`
	FreeTorrent    bool   `json:"freeTorrent"`
	NeutralTorrent bool   `json:"neutralTorrent"`
	HL             bool   `json:"hl"`
	UploadedTime   string `json:"uploadedTime"`
}

type BTNUserStats struct {
	ID            int     `json:"id"`
	Username      string  `json:"username"`
	Uploaded      int64   `json:"uploaded"`
	Downloaded    int64   `json:"downloaded"`
	Ratio         float64 `json:"ratio"`
	RequiredRatio float64 `json:"requiredRatio"`
	Class         string  `json:"class"`
}
