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

type RedactedTracker struct {
	*BaseTracker
}

func init() {
	Register(TrackerTypeRedacted, func(cfg *TrackerConfig) (Tracker, error) {
		return NewRedactedTracker(cfg), nil
	})
}

func NewRedactedTracker(cfg *TrackerConfig) *RedactedTracker {
	return &RedactedTracker{
		BaseTracker: NewBaseTracker(cfg),
	}
}

func (t *RedactedTracker) Authenticate(ctx context.Context) error {
	if t.config.APIKey == "" {
		return fmt.Errorf("%w: API key required for RED", ErrAuthenticationFailed)
	}

	t.mu.Lock()
	t.authed = true
	t.config.LastAuth = time.Now()
	t.mu.Unlock()

	return nil
}

func (t *RedactedTracker) BuildAnnounceURL(req AnnounceParams) (string, error) {
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
	q.Set("passkey", t.config.PassKey)
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

func (t *RedactedTracker) Test(ctx context.Context) error {
	if t.config.APIKey == "" && t.config.PassKey == "" {
		return fmt.Errorf("%w: API key or passkey required", ErrInvalidConfig)
	}

	if t.config.APIKey != "" {
		u, err := url.Parse(t.config.URL)
		if err != nil {
			return fmt.Errorf("parse URL: %w", err)
		}
		u.Path = "/ajax.php"
		q := u.Query()
		q.Set("action", "index")
		q.Set("auth", t.config.APIKey)
		u.RawQuery = q.Encode()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}

		resp, err := t.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("request failed: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("%w: invalid API key", ErrAuthenticationFailed)
		}

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("%w: status %d", ErrTestFailed, resp.StatusCode)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("read response: %w", err)
		}

		var result struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return fmt.Errorf("parse response: %w", err)
		}

		if result.Status != "success" {
			return fmt.Errorf("%w: unexpected status", ErrTestFailed)
		}
	}

	return nil
}

func (t *RedactedTracker) Get(ctx context.Context, path string) ([]byte, error) {
	return t.BaseTracker.Get(ctx, path)
}

func (t *RedactedTracker) Post(ctx context.Context, path string, body io.Reader) ([]byte, error) {
	return t.BaseTracker.Post(ctx, path, body)
}

func (t *RedactedTracker) GetTorrents(ctx context.Context, search string) ([]RedactedTorrent, error) {
	if t.config.APIKey == "" {
		return nil, fmt.Errorf("%w: API key required", ErrNotAuthenticated)
	}

	u, err := url.Parse(t.config.URL)
	if err != nil {
		return nil, fmt.Errorf("parse URL: %w", err)
	}
	u.Path = "/ajax.php"
	q := u.Query()
	q.Set("action", "browse")
	q.Set("auth", t.config.APIKey)
	if search != "" {
		q.Set("search", search)
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
		Results []RedactedTorrent `json:"results"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	return result.Results, nil
}

func (t *RedactedTracker) GetUserStats(ctx context.Context) (*RedactedUserStats, error) {
	if t.config.APIKey == "" {
		return nil, fmt.Errorf("%w: API key required", ErrNotAuthenticated)
	}

	u, err := url.Parse(t.config.URL)
	if err != nil {
		return nil, fmt.Errorf("parse URL: %w", err)
	}
	u.Path = "/ajax.php"
	q := u.Query()
	q.Set("action", "userstats")
	q.Set("auth", t.config.APIKey)
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

	var stats RedactedUserStats
	if err := json.Unmarshal(body, &stats); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	return &stats, nil
}

type RedactedTorrent struct {
	GroupName      string `json:"groupName"`
	GroupID        int    `json:"groupId"`
	TorrentID      int    `json:"torrentId"`
	MediaInfo      string `json:"mediaInfo"`
	Format         string `json:"format"`
	Encoding       string `json:"encoding"`
	HasLog         bool   `json:"hasLog"`
	LogScore       int    `json:"logScore"`
	HasCue         bool   `json:"hasCue"`
	Scene          bool   `json:"scene"`
	VanityHouse    bool   `json:"vanityHouse"`
	FileCount      int    `json:"fileCount"`
	Size           int64  `json:"size"`
	Seeders        int    `json:"seeders"`
	Leechers       int    `json:"leechers"`
	Snatched       int    `json:"snatched"`
	FreeTorrent    bool   `json:"freeTorrent"`
	NeutralTorrent bool   `json:"neutralTorrent"`
	HL             bool   `json:"hl"`
	UpMultiplier   int    `json:"upMultiplier"`
	DownMultiplier int    `json:"downMultiplier"`
	Time           string `json:"time"`
	ReleaseName    string `json:"releaseName"`
}

type RedactedUserStats struct {
	ID            int     `json:"id"`
	Username      string  `json:"username"`
	AuthKey       string  `json:"authKey"`
	PassKey       string  `json:"passKey"`
	Uploaded      int64   `json:"uploaded"`
	Downloaded    int64   `json:"downloaded"`
	Ratio         float64 `json:"ratio"`
	RequiredRatio float64 `json:"requiredRatio"`
	Class         string  `json:"class"`
	Priority      int     `json:"priority"`
	DownloadedAV  int64   `json:"downloadedAb"`
	UploadedAV    int64   `json:"uploadedAb"`
	Buffer        int64   `json:"buffer"`
	NumTorrents   []int   `json:"torrents"`
}
