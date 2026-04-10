package tracker

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

type BasicTracker struct {
	*BaseTracker
}

func init() {
	Register(TrackerTypeBasic, func(cfg *TrackerConfig) (Tracker, error) {
		return NewBasicTracker(cfg), nil
	})
}

func NewBasicTracker(cfg *TrackerConfig) *BasicTracker {
	return &BasicTracker{
		BaseTracker: NewBaseTracker(cfg),
	}
}

func (t *BasicTracker) Authenticate(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.authed = true
	return nil
}

func (t *BasicTracker) RefreshAuth(ctx context.Context) error {
	return nil
}

func (t *BasicTracker) BuildAnnounceURL(req AnnounceParams) (string, error) {
	if t.config.PassKey != "" {
		u, err := url.Parse(t.config.URL)
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
	return ParseAnnounceURL(t.config.URL, req)
}

func (t *BasicTracker) Test(ctx context.Context) error {
	if t.config.URL == "" {
		return fmt.Errorf("%w: URL is required", ErrInvalidConfig)
	}
	return nil
}

func (t *BasicTracker) Get(ctx context.Context, path string) ([]byte, error) {
	return t.BaseTracker.Get(ctx, path)
}

func (t *BasicTracker) Post(ctx context.Context, path string, body io.Reader) ([]byte, error) {
	return t.BaseTracker.Post(ctx, path, body)
}

func (t *BasicTracker) DoRequest(req *http.Request) (*http.Response, error) {
	t.applyAuth(req)
	return t.httpClient.Do(req)
}

func (t *BasicTracker) HTTPClient() *http.Client {
	return t.httpClient
}
