# Mediarr Implementation Plan

## Overview
Replace all *arr apps (Sonarr, Radarr, Lidarr, Readarr, Bazarr) plus external torrent/usenet clients with a single unified Go application using embeddb.

---

## Phase 1: Download Engine (Core)

### 1.1 Torrent Client Implementation
- Create `internal/download/torrent/` package
- Integrate `github.com/anacrolix/torrent` library
- Implement:
  - `Client` struct with config (port, limits, peer settings)
  - `AddTorrent(magnetURI string, torrentData []byte) (infoHash, error)`
  - `RemoveTorrent(infoHash string) error`
  - `GetStatus(infoHash string) (TorrentStatus, error)`
  - `SetUploadLimit(bytes/s)`, `SetDownloadLimit(bytes/s)`
  - `GetFiles(infoHash string) ([]FileInfo, error)`
  - `SetFilePriority(infoHash, fileIndex, priority)`

- TorrentStatus struct:
  - InfoHash, Name, State (downloading/seeding/complete/error)
  - BytesDone, BytesTotal
  - DownloadRate, UploadRate
  - PeersConnected, PeersTotal
  - Progress (0.0-1.0)

### 1.2 Usenet Client Implementation
- Create `internal/download/usenet/` package
- Integrate `github.com/ze0nni/go-nntp` or custom implementation
- Implement:
  - `Client` struct with server configs
  - `Connect(server ServerConfig) error`
  - `Download(articleID string) (data []byte, error)`
  - `GetArticle(group, articleID string) (header+body, error)`
  - `Authenticate(user, pass) error`
  - PAR2 repair integration (external or pure Go)

### 1.3 Download Worker
- Create `internal/download/worker.go`
- Implement queue processor:
  - Poll DownloadJob table for queued items
  - Start download based on provider (torrent/usenet)
  - Update progress to database periodically
  - Handle pause/resume/cancel signals
  - On complete: trigger file organization
  - On failure: update status, retry logic

### 1.4 Download Manager
- Create `internal/download/manager.go`
- Coordinate torrent + usenet clients
- Unified interface:
  - `StartDownload(job *DownloadJob) error`
  - `PauseDownload(id uint32) error`
  - `ResumeDownload(id uint32) error`
  - `CancelDownload(id uint32) error`
  - `GetProgress(id uint32) (DownloadProgress, error)`

---

## Phase 2: Indexer Integration

### 2.1 Indexer Framework
- Create `internal/indexer/` package
- Indexer interface:
  - `Search(query string, category Category, limit int) ([]SearchResult, error)`
  - `GetCapabilities() (Capabilities, error)`
  - `Test() error`

### 2.2 Torznab Client
- Implement `torznab.go`
- Parse Torznab XML responses
- Map to SearchResult struct

### 2.3 Newznab Client  
- Implement `newznab.go`
- Similar interface to Torznab

### 2.4 Indexer Management
- `internal/indexer/manager.go`
- Store indexer configs in embeddb
- Health checks
- API key management

---

## Phase 3: RSS & Automation

### 3.1 RSS Parser
- Create `internal/rss/` package
- Standard RSS/Atom parsing
- New item detection (store last seen)

### 3.2 Monitor System
- Create `internal/monitor/` package
- Watchlist management (wanted items)
- Quality profiles
- Auto-search triggers
- Decision engine (best match vs preferences)

### 3.3 Tv Show Tracking
- Episode calendar
- Season/episode history
- Continue downloading flag

---

## Phase 4: Media Organization

### 4.1 File Organizer
- Create `internal/organize/` package
- Template-based renaming
- Folder structure creation
- Hardlink/copy/move options
- Permissions handling

### 4.2 Media Identifier
- Detect media type from file
- Extract info from filename (name, year, season, episode)
- Match to library items

---

## Phase 5: Subtitle Management

### 5.1 Subtitle Downloader
- Create `internal/subtitles/` package
- OpenSubtitles.com API
- Subscene scraper
- Language matching

---

## Phase 6: UI

### 6.1 API Extensions
- Add endpoints for download control
- Add indexer management
- Add watchlist management

### 6.2 Frontend Updates
- Progress display
- Calendar view
- Statistics

---

## File Structure

```
mediarr/
├── cmd/mediarr/main.go
├── internal/
│   ├── config/config.go
│   ├── db/
│   │   ├── db.go
│   │   └── models.go (updated with metadata)
│   ├── server/
│   │   ├── handlers.go
│   │   └── server.go
│   ├── download/
│   │   ├── torrent/
│   │   │   └── client.go
│   │   ├── usenet/
│   │   │   └── client.go
│   │   ├── worker.go
│   │   └── manager.go
│   ├── indexer/
│   │   ├── client.go (interface)
│   │   ├── torznab.go
│   │   ├── newznab.go
│   │   └── manager.go
│   ├── rss/
│   │   ├── parser.go
│   │   └── client.go
│   ├── monitor/
│   │   ├── watchlist.go
│   │   └── decisions.go
│   ├── organize/
│   │   └── file_organizer.go
│   ├── subtitles/
│   │   └── downloader.go
│   └── tasks/manager.go
├── go.mod
└── config.yaml
```

---

## Key Decisions to Make During Implementation

1. **Torrent library**: Use `github.com/anacrolix/torrent` - well maintained, full-featured
2. **Usenet**: Start with basic implementation, can add PAR2 later
3. **Single worker**: Start with single goroutine processing queue, add parallel later
4. **Config storage**: Use embeddb for indexer configs, credentials

---

## Testing Strategy

1. Mock metadata server for download tests
2. Use public torrents for torrent client tests
3. Skip usenet tests without server
4. Integration tests with real downloads (mark as slow)
