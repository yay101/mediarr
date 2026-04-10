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

type TorrentLeechTracker struct {
	*BaseTracker
}

func init() {
	Register(TrackerTypeTorrentLeech, func(cfg *TrackerConfig) (Tracker, error) {
		return NewTorrentLeechTracker(cfg), nil
	})
}

func NewTorrentLeechTracker(cfg *TrackerConfig) *TorrentLeechTracker {
	return &TorrentLeechTracker{
		BaseTracker: NewBaseTracker(cfg),
	}
}

func (t *TorrentLeechTracker) Authenticate(ctx context.Context) error {
	if t.config.APIKey == "" {
		if t.config.Username == "" || t.config.Password == "" {
			return fmt.Errorf("%w: API key or username/password required", ErrAuthenticationFailed)
		}

		baseURL := t.config.URL
		if !strings.HasSuffix(baseURL, "/") {
			baseURL += "/"
		}

		loginURL := baseURL + "user/account-login"

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

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("%w: status %d", ErrAuthenticationFailed, resp.StatusCode)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("read response: %w", err)
		}

		var loginResp struct {
			APIKey   string `json:"apikey"`
			APITTL   int    `json:"apiTtl"`
			Username string `json:"username"`
		}

		if err := json.Unmarshal(body, &loginResp); err != nil {
			return fmt.Errorf("parse login response: %w", err)
		}

		if loginResp.APIKey == "" {
			return fmt.Errorf("%w: no API key received", ErrAuthenticationFailed)
		}

		t.mu.Lock()
		t.authed = true
		t.config.APIKey = loginResp.APIKey
		t.config.CookieExpiry = time.Now().Add(time.Duration(loginResp.APITTL) * time.Second)
		t.config.LastAuth = time.Now()
		t.mu.Unlock()

		return nil
	}

	t.mu.Lock()
	t.authed = true
	t.config.LastAuth = time.Now()
	t.mu.Unlock()

	return nil
}

func (t *TorrentLeechTracker) RefreshAuth(ctx context.Context) error {
	if !t.IsAuthenticated() {
		return t.Authenticate(ctx)
	}

	t.mu.RLock()
	expiry := t.config.CookieExpiry
	t.mu.RUnlock()

	if !expiry.IsZero() && time.Until(expiry) < 1*time.Hour {
		return t.Authenticate(ctx)
	}

	return nil
}

func (t *TorrentLeechTracker) BuildAnnounceURL(req AnnounceParams) (string, error) {
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

func (t *TorrentLeechTracker) Test(ctx context.Context) error {
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

	return nil
}

func (t *TorrentLeechTracker) Get(ctx context.Context, path string) ([]byte, error) {
	return t.BaseTracker.Get(ctx, path)
}

func (t *TorrentLeechTracker) Post(ctx context.Context, path string, body io.Reader) ([]byte, error) {
	return t.BaseTracker.Post(ctx, path, body)
}

func (t *TorrentLeechTracker) Search(ctx context.Context, query string, limit int) ([]TorrentLeechResult, error) {
	t.mu.RLock()
	apiKey := t.config.APIKey
	t.mu.RUnlock()

	if apiKey == "" {
		return nil, fmt.Errorf("%w: API key required", ErrNotAuthenticated)
	}

	baseURL := t.config.URL
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}

	searchURL := baseURL + "api/torrents"

	u, err := url.Parse(searchURL)
	if err != nil {
		return nil, fmt.Errorf("parse URL: %w", err)
	}

	q := u.Query()
	q.Set("apikey", apiKey)
	if query != "" {
		q.Set("search", query)
	}
	if limit > 0 {
		q.Set("limit", fmt.Sprintf("%d", limit))
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
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

	var result struct {
		Torrents []TorrentLeechResult `json:"torrents"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	return result.Torrents, nil
}

func (t *TorrentLeechTracker) GetUserStats(ctx context.Context) (*TorrentLeechUserStats, error) {
	t.mu.RLock()
	apiKey := t.config.APIKey
	t.mu.RUnlock()

	if apiKey == "" {
		return nil, fmt.Errorf("%w: API key required", ErrNotAuthenticated)
	}

	baseURL := t.config.URL
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}

	statsURL := baseURL + "api/user"

	u, err := url.Parse(statsURL)
	if err != nil {
		return nil, fmt.Errorf("parse URL: %w", err)
	}

	q := u.Query()
	q.Set("apikey", apiKey)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
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

	var stats TorrentLeechUserStats
	if err := json.Unmarshal(body, &stats); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	return &stats, nil
}

type TorrentLeechResult struct {
	ID           int       `json:"id"`
	Name         string    `json:"name"`
	Hash         string    `json:"hash"`
	Size         int64     `json:"size"`
	Seeders      int       `json:"seeders"`
	Leechers     int       `json:"leechers"`
	Snatches     int       `json:"snatches"`
	CategoryID   int       `json:"categoryId"`
	CategoryName string    `json:"categoryName"`
	DownloadURL  string    `json:"downloadUrl"`
	Freeleech    bool      `json:"freeleech"`
	FreeleechExp time.Time `json:"freeleechExp"`
	UploadedAt   time.Time `json:"uploadedAt"`
}

type TorrentLeechUserStats struct {
	Uploaded    int64   `json:"uploaded"`
	Downloaded  int64   `json:"downloaded"`
	Ratio       float64 `json:"ratio"`
	BonusPoints int64   `json:"bonusPoints"`
	InviteCount int     `json:"inviteCount"`
}
