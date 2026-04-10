# Private Tracker Support

This package provides an abstraction layer for private BitTorrent trackers with authentication, announces, and ratio tracking.

## Overview

The tracker system is designed to be optional and extensible. Users who don't need private tracker support won't be affected - all tracker-related functionality is gated behind explicit configuration.

## Interface

All trackers implement the `Tracker` interface:

```go
type Tracker interface {
    Name() string
    Type() TrackerType
    GetConfig() *TrackerConfig
    SetConfig(*TrackerConfig)

    Authenticate(ctx context.Context) error
    IsAuthenticated() bool
    RefreshAuth(ctx context.Context) error

    BuildAnnounceURL(req AnnounceParams) (string, error)

    Get(ctx context.Context, path string) ([]byte, error)
    Post(ctx context.Context, path string, body io.Reader) ([]byte, error)

    Test(ctx context.Context) error

    Close() error
}
```

## Supported Trackers

| Tracker | Type ID | Auth Method | Notes |
|---------|---------|-------------|-------|
| Basic/Public | `basic` | None | Wrapper for public trackers |
| RED/Redacted | `redacted` | API Key | Music tracker, requires API key or passkey |
| AnonMouse | `anonamouse` | Cookie | Login form authentication |
| BTN | `btn` | Cookie/API Key | Requires invite, cookie session management |
| TorrentLeech | `torrentleech` | API Key | Cookie auth also supported |

## Adding a New Tracker

1. Create a new file `newtracker.go` in `internal/tracker/`

2. Define your tracker struct embedding `*BaseTracker`:

```go
type NewTracker struct {
    *BaseTracker
}

func init() {
    Register(TrackerTypeNewTracker, func(cfg *TrackerConfig) (Tracker, error) {
        return NewNewTracker(cfg), nil
    })
}

func NewNewTracker(cfg *TrackerConfig) *NewTracker {
    return &NewTracker{
        BaseTracker: NewBaseTracker(cfg),
    }
}
```

3. Implement the required methods:

```go
func (t *NewTracker) Authenticate(ctx context.Context) error {
    // Login logic here
    // Set t.authed = true on success
    // Store cookies/auth tokens in t.config
}

func (t *NewTracker) BuildAnnounceURL(req AnnounceParams) (string, error) {
    // Build tracker-specific announce URL
    // Include any auth tokens/passkeys
}

func (t *NewTracker) Test(ctx context.Context) error {
    // Test connectivity and authentication
}
```

## Configuration

Example YAML configuration:

```yaml
download:
  trackers:
    enabled: true
    list:
      - name: "RED"
        enabled: true
        type: "redacted"
        url: "https://redacted.ch"
        api_key: "your-api-key"
        passkey: "your-passkey"
      - name: "AnonMouse"
        enabled: true
        type: "anonamouse"
        url: "https://anonamouse.com"
        username: "user"
        password: "pass"
```

## Ratio Tracking

The `StatsManager` tracks upload/download stats per torrent per tracker:

```go
stats := manager.GetStats()
stats.UpdateStats(infoHash, trackerID, uploaded, downloaded)
stats.StartSeeding(infoHash, trackerID)
stats.StopSeeding(infoHash, trackerID)

ratio, _ := stats.GetRatio(infoHash, trackerID)
seedTime, _ := stats.GetSeedTime(infoHash, trackerID)
```

## WebUI Integration

Trackers support `GetConfig()` and `SetConfig()` for WebUI integration:

```go
// Get current config
cfg := tracker.GetConfig()

// Update config
tracker.SetConfig(newCfg)
```

This allows users to:
- Add/edit/remove trackers from the UI
- View authentication status
- Test connections
- See ratio stats
