package torrent

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
)

type TorrentStatus struct {
	InfoHash       string
	Name           string
	State          string
	BytesDone      int64
	BytesTotal     int64
	DownloadRate   int64
	UploadRate     int64
	PeersConnected int
	PeersTotal     int
	Progress       float32
	Eta            time.Duration
	Error          string
	StoragePath    string
}

type FileInfo struct {
	Path     string
	Size     int64
	Progress float32
}

type Client struct {
	client      *torrent.Client
	config      Config
	downloads   map[string]*torrent.Torrent
	mu          sync.RWMutex
	rateLimiter *RateLimiter
}

type Config struct {
	ListenPort         int
	DownloadDir        string
	MaxConnections     int
	MaxPeersPerTorrent int
	UploadRateLimit    int
	DownloadRateLimit  int
	DataDir            string
}

type RateLimiter struct {
	downloadLimit int
	uploadLimit   int
}

func NewClient(cfg Config) (*Client, error) {
	if cfg.ListenPort == 0 {
		cfg.ListenPort = 6881
	}

	torrentCfg := torrent.NewDefaultClientConfig()
	torrentCfg.ListenPort = cfg.ListenPort
	torrentCfg.DataDir = cfg.DataDir

	client, err := torrent.NewClient(torrentCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create torrent client: %w", err)
	}

	c := &Client{
		client:    client,
		config:    cfg,
		downloads: make(map[string]*torrent.Torrent),
		rateLimiter: &RateLimiter{
			downloadLimit: cfg.DownloadRateLimit,
			uploadLimit:   cfg.UploadRateLimit,
		},
	}

	return c, nil
}

func (c *Client) AddMagnet(magnetURI string) (string, error) {
	t, err := c.client.AddMagnet(magnetURI)
	if err != nil {
		return "", fmt.Errorf("failed to add magnet: %w", err)
	}

	infoHash := t.InfoHash().HexString()

	c.mu.Lock()
	c.downloads[infoHash] = t
	c.mu.Unlock()

	go c.monitorTorrent(t)

	return infoHash, nil
}

func (c *Client) AddTorrent(torrentData []byte) (string, error) {
	mi, err := metainfo.Load(bytes.NewReader(torrentData))
	if err != nil {
		return "", fmt.Errorf("failed to load metainfo: %w", err)
	}
	t, err := c.client.AddTorrent(mi)
	if err != nil {
		return "", fmt.Errorf("failed to add torrent: %w", err)
	}

	infoHash := t.InfoHash().HexString()

	c.mu.Lock()
	c.downloads[infoHash] = t
	c.mu.Unlock()

	go c.monitorTorrent(t)

	return infoHash, nil
}

func (c *Client) monitorTorrent(t *torrent.Torrent) {
	<-t.GotInfo()
	t.DownloadAll()
}

func (c *Client) RemoveTorrent(infoHash string) error {
	c.mu.Lock()
	t, ok := c.downloads[strings.ToLower(infoHash)]
	if !ok {
		c.mu.Unlock()
		return nil
	}
	delete(c.downloads, strings.ToLower(infoHash))
	c.mu.Unlock()

	t.Drop()
	return nil
}

func (c *Client) GetStatus(infoHash string) (TorrentStatus, error) {
	c.mu.RLock()
	t, ok := c.downloads[strings.ToLower(infoHash)]
	c.mu.RUnlock()

	if !ok {
		return TorrentStatus{}, fmt.Errorf("torrent not found: %s", infoHash)
	}

	return c.statusFromTorrent(t), nil
}

func (c *Client) statusFromTorrent(t *torrent.Torrent) TorrentStatus {
	status := TorrentStatus{
		InfoHash: t.InfoHash().HexString(),
		State:    "checking",
	}

	if t.Info() != nil {
		status.Name = t.Name()
		status.BytesTotal = t.Length()
		status.StoragePath = filepath.Join(c.config.DataDir, t.Name())
	}

	status.BytesDone = t.BytesCompleted()
	status.PeersConnected = len(t.PeerConns())

	if status.BytesTotal > 0 {
		status.Progress = float32(status.BytesDone) / float32(status.BytesTotal)
	}

	if t.Seeding() {
		status.State = "seeding"
	} else if status.BytesDone < status.BytesTotal {
		status.State = "downloading"
	} else {
		status.State = "complete"
	}

	return status
}

func (c *Client) GetFiles(infoHash string) ([]FileInfo, error) {
	c.mu.RLock()
	t, ok := c.downloads[strings.ToLower(infoHash)]
	c.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("torrent not found: %s", infoHash)
	}

	<-t.GotInfo()

	tFiles := t.Files()
	files := make([]FileInfo, len(tFiles))
	for i, f := range tFiles {
		files[i] = FileInfo{
			Path: f.Path(),
			Size: f.Length(),
		}
	}

	return files, nil
}

func (c *Client) SetFilePriority(infoHash string, filePath string, priority int) error {
	c.mu.RLock()
	t, ok := c.downloads[strings.ToLower(infoHash)]
	c.mu.RUnlock()

	if !ok {
		return fmt.Errorf("torrent not found: %s", infoHash)
	}

	<-t.GotInfo()

	for _, f := range t.Files() {
		if f.Path() == filePath {
			if priority == 0 {
				f.SetPriority(torrent.PiecePriorityNone)
			} else if priority >= 2 {
				f.SetPriority(torrent.PiecePriorityHigh)
			} else {
				f.SetPriority(torrent.PiecePriorityNormal)
			}
			return nil
		}
	}

	return fmt.Errorf("file not found: %s", filePath)
}

func (c *Client) SetUploadLimit(bytesPerSecond int) {
	// anacrolix/torrent rate limiting is complex, leaving stub
}

func (c *Client) SetDownloadLimit(bytesPerSecond int) {
	// anacrolix/torrent rate limiting is complex, leaving stub
}

func (c *Client) Close() error {
	c.mu.Lock()
	for _, t := range c.downloads {
		t.Drop()
	}
	c.downloads = make(map[string]*torrent.Torrent)
	c.mu.Unlock()

	c.client.Close()
	return nil
}
