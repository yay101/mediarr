package tracker

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type AnonMouseTracker struct {
	*BaseTracker
}

func init() {
	Register(TrackerTypeAnonMouse, func(cfg *TrackerConfig) (Tracker, error) {
		return NewAnonMouseTracker(cfg), nil
	})
}

func NewAnonMouseTracker(cfg *TrackerConfig) *AnonMouseTracker {
	return &AnonMouseTracker{
		BaseTracker: NewBaseTracker(cfg),
	}
}

func (t *AnonMouseTracker) Authenticate(ctx context.Context) error {
	if t.config.Username == "" || t.config.Password == "" {
		return fmt.Errorf("%w: username and password required", ErrAuthenticationFailed)
	}

	baseURL := t.config.URL
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}

	loginURL := baseURL + "static/login.php"

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
	var passkey string

	for _, cookie := range cookies {
		if cookie.Name == "session" || cookie.Name == "sid" {
			sessionCookie = cookie.Value
		}
		if cookie.Name == "passkey" || cookie.Name == "auth" {
			passkey = cookie.Value
		}
	}

	if sessionCookie == "" {
		return fmt.Errorf("%w: no session cookie received", ErrAuthenticationFailed)
	}

	if passkey == "" && t.config.PassKey == "" {
		passkey, err = t.fetchPasskey(ctx, sessionCookie)
		if err != nil {
			return fmt.Errorf("fetch passkey: %w", err)
		}
	} else if passkey != "" {
		t.config.PassKey = passkey
	}

	t.mu.Lock()
	t.authed = true
	t.config.Cookie = fmt.Sprintf("session=%s", sessionCookie)
	t.config.CookieExpiry = time.Now().Add(24 * time.Hour)
	t.config.LastAuth = time.Now()
	t.mu.Unlock()

	return nil
}

func (t *AnonMouseTracker) fetchPasskey(ctx context.Context, sessionCookie string) (string, error) {
	baseURL := t.config.URL
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}

	settingsURL := baseURL + "index.php?page=account"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, settingsURL, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Cookie", fmt.Sprintf("session=%s", sessionCookie))

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("settings page: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	passkeyRegex := regexp.MustCompile(`passkey[=<]\s*["']?([a-f0-9]{32})`)
	matches := passkeyRegex.FindSubmatch(body)
	if len(matches) >= 2 {
		return string(matches[1]), nil
	}

	return "", fmt.Errorf("passkey not found")
}

func (t *AnonMouseTracker) RefreshAuth(ctx context.Context) error {
	if !t.IsAuthenticated() {
		return t.Authenticate(ctx)
	}
	return nil
}

func (t *AnonMouseTracker) BuildAnnounceURL(req AnnounceParams) (string, error) {
	if t.config.PassKey == "" {
		return "", fmt.Errorf("%w: passkey not set", ErrInvalidConfig)
	}

	baseURL := t.config.URL
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}

	announceURL := baseURL + "announce.php"

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

func (t *AnonMouseTracker) Test(ctx context.Context) error {
	if t.config.Username == "" || t.config.Password == "" {
		return fmt.Errorf("%w: username and password required", ErrInvalidConfig)
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

func (t *AnonMouseTracker) Get(ctx context.Context, path string) ([]byte, error) {
	return t.BaseTracker.Get(ctx, path)
}

func (t *AnonMouseTracker) Post(ctx context.Context, path string, body io.Reader) ([]byte, error) {
	return t.BaseTracker.Post(ctx, path, body)
}
