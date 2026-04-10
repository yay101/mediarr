package tracker

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// Tracker defines the interface for interacting with private BitTorrent trackers.
// Each tracker implementation handles its own authentication, API calls, and
// tracker-specific behavior (e.g., RED uses cookies, TorrentLeech uses API keys).
type Tracker interface {
	Name() string
	Type() TrackerType
	GetConfig() *TrackerConfig
	SetConfig(*TrackerConfig)

	// Authenticate establishes a session with the tracker.
	// Implementations should handle cookie/API key storage internally.
	Authenticate(ctx context.Context) error

	// IsAuthenticated returns true if the tracker has a valid session.
	// This checks both the authed flag and cookie expiry time.
	IsAuthenticated() bool

	// RefreshAuth attempts to re-authenticate if the session has expired.
	// Returns nil if refresh is not needed or was successful.
	RefreshAuth(ctx context.Context) error

	// BuildAnnounceURL generates a properly formatted tracker announce URL
	// with all required BitTorrent protocol parameters (info_hash, peer_id, etc.)
	BuildAnnounceURL(req AnnounceParams) (string, error)

	// Get performs an authenticated GET request to the tracker API.
	Get(ctx context.Context, path string) ([]byte, error)

	// Post performs an authenticated POST request to the tracker API.
	Post(ctx context.Context, path string, body io.Reader) ([]byte, error)

	// Test verifies connectivity and authentication with the tracker.
	Test(ctx context.Context) error

	// Close cleans up resources (e.g., HTTP client, auth tokens).
	Close() error
}

// AnnounceParams contains the parameters required for a BitTorrent tracker announce.
// These values are sent to the tracker to report download progress and receive peers.
type AnnounceParams struct {
	InfoHash   [20]byte // Torrent infohash (SHA1 of info dict)
	PeerID     [20]byte // Client peer ID
	Port       int      // Peer listen port
	Uploaded   int64    // Total bytes uploaded
	Downloaded int64    // Total bytes downloaded
	Left       int64    // Bytes remaining to download
	Event      string   // Event type: started, stopped, completed, empty
}

// AnnounceResponse contains the tracker's response to an announce request.
// The response includes peer list and announce interval.
type AnnounceResponse struct {
	Interval   int64  // Seconds until next announce
	Complete   int32  // Number of seeders
	Incomplete int32  // Number of leechers
	Peers      []Peer // List of available peers
	Warning    string // Tracker warning message
	Error      string // Tracker error message
}

// Peer represents a single peer from the tracker response.
type Peer struct {
	IP   string // Peer IP address
	Port int    // Peer listen port
}

// BaseTracker provides common functionality for all tracker implementations.
// It handles HTTP client setup, authentication header management, and
// implements the basic Get/Post methods that most trackers use.
// Trackers should embed BaseTracker and implement custom Authenticate methods.
type BaseTracker struct {
	config     *TrackerConfig // Tracker-specific configuration
	httpClient *http.Client   // Timeout-configured HTTP client
	mu         sync.RWMutex   // Protects config changes
	authed     bool           // Authentication state flag
	authMu     sync.Mutex     // Serializes authentication attempts
}

// NewBaseTracker creates a BaseTracker with the given configuration.
// The HTTP client is configured with a 30-second timeout to prevent hanging requests.
func NewBaseTracker(cfg *TrackerConfig) *BaseTracker {
	return &BaseTracker{
		config: cfg.Clone(),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (b *BaseTracker) Name() string      { return b.config.Name }
func (b *BaseTracker) Type() TrackerType { return b.config.Type }

// GetConfig returns a thread-safe copy of the current tracker configuration.
// Used by WebUI to display current settings without exposing internal state.
func (b *BaseTracker) GetConfig() *TrackerConfig {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.config.Clone()
}

// SetConfig updates the tracker configuration and clears the authentication flag.
// This forces re-authentication on the next operation with new credentials.
func (b *BaseTracker) SetConfig(cfg *TrackerConfig) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.config = cfg.Clone()
	b.authed = false
}

// IsAuthenticated checks if the tracker has a valid authentication session.
// Returns false if no auth was performed or if the cookie has expired.
func (b *BaseTracker) IsAuthenticated() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if !b.authed {
		return false
	}
	// Check cookie expiry if set (some trackers use session cookies)
	if !b.config.CookieExpiry.IsZero() && time.Now().After(b.config.CookieExpiry) {
		return false
	}
	return true
}

// RefreshAuth is a no-op in the base implementation.
// Trackers with token-based auth should override this to refresh access tokens.
func (b *BaseTracker) RefreshAuth(ctx context.Context) error {
	return nil
}

// Get performs an authenticated GET request to the tracker API.
// Handles common error cases: unauthorized (need to auth), forbidden (banned),
// rate limited, and general request failures.
// Trackers should use this for API calls that don't require request bodies.
func (b *BaseTracker) Get(ctx context.Context, path string) ([]byte, error) {
	u, err := url.Parse(b.config.URL)
	if err != nil {
		return nil, fmt.Errorf("parse URL: %w", err)
	}
	u.Path = path

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	b.applyAuth(req)

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Map HTTP status codes to tracker-specific errors
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrNotAuthenticated
	}
	if resp.StatusCode == http.StatusForbidden {
		return nil, ErrBanned
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, ErrRateLimited
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrRequestFailed, resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

// Post performs an authenticated POST request to the tracker API.
// Sets Content-Type to application/x-www-form-urlencoded as most tracker APIs expect.
// Use this for API calls that submit data (login forms, upload requests, etc.).
func (b *BaseTracker) Post(ctx context.Context, path string, body io.Reader) ([]byte, error) {
	u, err := url.Parse(b.config.URL)
	if err != nil {
		return nil, fmt.Errorf("parse URL: %w", err)
	}
	u.Path = path

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	b.applyAuth(req)

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrNotAuthenticated
	}
	if resp.StatusCode == http.StatusForbidden {
		return nil, ErrBanned
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, ErrRateLimited
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrRequestFailed, resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

// applyAuth adds authentication headers to the request based on tracker config.
// Checks for both cookie-based auth (Cookie header) and API key auth (Bearer token).
func (b *BaseTracker) applyAuth(req *http.Request) {
	b.mu.RLock()
	cfg := b.config
	b.mu.RUnlock()

	if cfg.Cookie != "" {
		req.Header.Set("Cookie", cfg.Cookie)
	}
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
}

// Close cleans up resources. Base implementation is a no-op since
// http.Client handles its own connection pooling.
func (b *BaseTracker) Close() error {
	return nil
}

// ParseAnnounceURL builds a BitTorrent tracker announce URL from parameters.
// Handles URL encoding of binary data (info_hash, peer_id) and adds
// a unique key to prevent IP-based connection counting issues.
func ParseAnnounceURL(announceURL string, req AnnounceParams) (string, error) {
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
	q.Set("compact", "1") // Request binary peer list for efficiency

	if req.Event != "" {
		q.Set("event", req.Event)
	}

	// Generate unique key to prevent tracker's IP-based peer filtering
	// Some trackers count connections by IP+key instead of peer_id
	var key [4]byte
	src := sha1.Sum([]byte(time.Now().String()))
	copy(key[:], src[:4])
	q.Set("key", hex.EncodeToString(key[:]))

	u.RawQuery = q.Encode()
	return u.String(), nil
}

// MakeFormBody creates an application/x-www-form-urlencoded request body
// from a map of key-value pairs. Returns nil for empty input.
func MakeFormBody(values map[string]string) io.Reader {
	if len(values) == 0 {
		return nil
	}

	var buf bytes.Buffer
	first := true
	for k, v := range values {
		if !first {
			buf.WriteByte('&')
		}
		buf.WriteString(url.QueryEscape(k))
		buf.WriteByte('=')
		buf.WriteString(url.QueryEscape(v))
		first = false
	}
	return &buf
}
