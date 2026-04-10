package torrent

import (
	"crypto/sha1"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/yay101/mediarr/internal/download/torrent/bencode"
	"github.com/yay101/mediarr/internal/download/torrent/metainfo"
	"github.com/yay101/mediarr/internal/download/torrent/peer"
	"github.com/yay101/mediarr/internal/download/torrent/tracker"
)

type ClientConfig struct {
	DownloadDir   string
	PeerID        [20]byte
	Port          int
	MaxPeers      int
	DownloadLimit int
	UploadLimit   int
	ProgressCB    func(infoHash [20]byte, progress float32)
	ErrorCB       func(infoHash [20]byte, err error)
}

type Client struct {
	config     ClientConfig
	torrents   map[[20]byte]*Torrent
	peerID     [20]byte
	port       int
	httpClient *http.Client
	mu         sync.RWMutex
	closeCh    chan struct{}
	wg         sync.WaitGroup
	listener   net.Listener
	limiter    *MultiLimiter
	dht        *DHTClient
}

func NewClient(cfg ClientConfig) (*Client, error) {
	if cfg.PeerID == ([20]byte{}) {
		cfg.PeerID = metainfo.NewDefaultPeerID()
	}
	if cfg.MaxPeers <= 0 {
		cfg.MaxPeers = 50
	}
	if cfg.Port <= 0 {
		cfg.Port = 6881
	}

	c := &Client{
		config:     cfg,
		torrents:   make(map[[20]byte]*Torrent),
		peerID:     cfg.PeerID,
		port:       cfg.Port,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		closeCh:    make(chan struct{}),
		limiter:    NewMultiLimiter(),
	}

	if cfg.DownloadLimit > 0 || cfg.UploadLimit > 0 {
		c.limiter.SetGlobalLimits(cfg.DownloadLimit, cfg.UploadLimit)
	}

	dht, err := NewDHTClient(cfg.Port)
	if err == nil {
		c.dht = dht
	}

	return c, nil
}

func (c *Client) Start() error {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", c.port))
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	c.listener = ln

	c.wg.Add(1)
	go c.acceptLoop()

	if c.dht != nil {
		c.dht.Start()
	}

	return nil
}

func (c *Client) acceptLoop() {
	defer c.wg.Done()

	for {
		conn, err := c.listener.Accept()
		if err != nil {
			select {
			case <-c.closeCh:
				return
			default:
				continue
			}
		}

		c.wg.Add(1)
		go c.handleInbound(conn)
	}
}

func (c *Client) handleInbound(conn net.Conn) {
	defer c.wg.Done()
	defer conn.Close()

	handshakeBuf := make([]byte, 68)
	if _, err := conn.Read(handshakeBuf); err != nil {
		return
	}

	h, err := peer.ParseHandshake(handshakeBuf)
	if err != nil {
		return
	}

	c.mu.RLock()
	t, exists := c.torrents[h.InfoHash]
	c.mu.RUnlock()

	if !exists {
		return
	}

	p := &peer.Conn{
		Conn:         conn,
		InfoHash:     h.InfoHash,
		PeerID:       h.PeerID,
		AmChoking:    true,
		AmInterested: false,
	}

	if err := p.WriteMessage(&peer.Message{Type: peer.MsgUnchoke}); err != nil {
		return
	}

	t.AddPeer(p)

	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		t.ProcessPeer(p)
	}()
}

func (c *Client) Close() {
	close(c.closeCh)

	c.mu.Lock()
	for _, t := range c.torrents {
		t.Close()
	}
	c.torrents = nil
	c.mu.Unlock()

	if c.listener != nil {
		c.listener.Close()
	}

	if c.dht != nil {
		c.dht.Close()
	}

	c.wg.Wait()
}

func (c *Client) AddTorrentFile(filePath string) (*Torrent, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	m, err := metainfo.ParseMetainfo(data)
	if err != nil {
		return nil, fmt.Errorf("parse metainfo: %w", err)
	}

	return c.AddMetainfo(m)
}

func (c *Client) AddMetainfo(m *metainfo.Metainfo) (*Torrent, error) {
	infoHash := m.InfoHash()

	c.mu.Lock()
	defer c.mu.Unlock()

	if t, exists := c.torrents[infoHash]; exists {
		return t, nil
	}

	peerID := c.peerID
	if peerID == ([20]byte{}) {
		peerID = metainfo.NewDefaultPeerID()
	}

	t, err := NewTorrent(TorrentConfig{
		InfoHash: infoHash,
		Name:     m.Info.Name,
		Info:     &m.Info,
		DataDir:  c.config.DownloadDir,
		PeerID:   peerID,
		MaxPeers: c.config.MaxPeers,
		Seeding:  true,
		ProgressCB: func(progress float32) {
			if c.config.ProgressCB != nil {
				c.config.ProgressCB(infoHash, progress)
			}
		},
		ErrorCB: func(err error) {
			if c.config.ErrorCB != nil {
				c.config.ErrorCB(infoHash, err)
			}
		},
	})
	if err != nil {
		return nil, fmt.Errorf("new torrent: %w", err)
	}

	c.torrents[infoHash] = t

	return t, nil
}

func (c *Client) AddMagnet(uri string) (*Torrent, error) {
	m, err := metainfo.ParseMagnet(uri)
	if err != nil {
		return nil, fmt.Errorf("parse magnet: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if t, exists := c.torrents[m.InfoHash]; exists {
		return t, nil
	}

	peerID := c.peerID
	if peerID == ([20]byte{}) {
		peerID = metainfo.NewDefaultPeerID()
	}

	t, err := NewTorrent(TorrentConfig{
		InfoHash: m.InfoHash,
		Name:     m.Name,
		DataDir:  c.config.DownloadDir,
		PeerID:   peerID,
		MaxPeers: c.config.MaxPeers,
		ProgressCB: func(progress float32) {
			if c.config.ProgressCB != nil {
				c.config.ProgressCB(m.InfoHash, progress)
			}
		},
		ErrorCB: func(err error) {
			if c.config.ErrorCB != nil {
				c.config.ErrorCB(m.InfoHash, err)
			}
		},
	})
	if err != nil {
		return nil, fmt.Errorf("new torrent: %w", err)
	}

	c.torrents[m.InfoHash] = t

	if len(m.Trackers) > 0 {
		tr := tracker.NewClient(peerID, c.port)
		t.SetTracker(tr)
	}

	go c.downloadMetadata(t, m.Trackers)

	if c.dht != nil {
		go c.searchDHTPeers(t)
	}

	return t, nil
}

func (c *Client) downloadMetadata(t *Torrent, trackers []string) {
	c.mu.RLock()
	peerID := c.peerID
	c.mu.RUnlock()

	allPeers := make([]string, 0)

	for attempt := 0; attempt < 3; attempt++ {
		for _, trackerURL := range trackers {
			req := tracker.AnnounceRequest{
				InfoHash:   t.InfoHash,
				PeerID:     peerID,
				Port:       c.port,
				Uploaded:   0,
				Downloaded: 0,
				Left:       0,
				Compact:    true,
				Event:      "started",
			}

			tc := tracker.NewClient(peerID, c.port)
			resp, err := tc.Announce(trackerURL, req)
			if err != nil {
				continue
			}

			for _, p := range resp.Peers {
				addr := fmt.Sprintf("%s:%d", p.IP, p.Port)
				allPeers = append(allPeers, addr)
			}

			if len(allPeers) > 0 {
				break
			}
		}

		if len(allPeers) > 0 {
			break
		}

		time.Sleep(2 * time.Second)
	}

	if len(allPeers) == 0 {
		fmt.Printf("No peers found from trackers, trying DHT...\n")
		return
	}

	fmt.Printf("Found %d peers from trackers\n", len(allPeers))

	for _, addr := range allPeers {
		c.connectToPeerWithMetadata(t, addr)
		time.Sleep(100 * time.Millisecond)
	}

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if t.Info != nil {
				return
			}

			for _, addr := range allPeers {
				c.connectToPeerWithMetadata(t, addr)
			}
		}
	}
}

func (c *Client) searchDHTPeers(t *Torrent) {
	if c.dht == nil {
		return
	}

	timeout := time.After(60 * time.Second)
	ticker := time.NewTicker(10 * time.Second)

	for {
		select {
		case <-timeout:
			return
		case <-ticker.C:
			if t.Info != nil {
				return
			}

			peers := c.dht.GetPeers(t.InfoHash)
			for _, p := range peers {
				addr := fmt.Sprintf("%s:%d", p.IP, p.Port)
				c.connectToPeerWithMetadata(t, addr)
			}
		}
	}
}

func (c *Client) handleTrackerResponse(t *Torrent, data []byte) error {
	val, err := bencode.Decode(data)
	if err != nil {
		return err
	}

	dict, ok := val.(bencode.Dict)
	if !ok {
		return fmt.Errorf("expected dict response")
	}

	peers := extractPeers(dict)
	for _, peerAddr := range peers {
		c.connectToPeer(t, peerAddr)
	}

	return nil
}

func extractPeers(dict bencode.Dict) []string {
	var peers []string

	if peerList, ok := dict["peers"]; ok {
		if list, ok := peerList.(bencode.List); ok {
			for _, p := range list {
				if peerDict, ok := p.(bencode.Dict); ok {
					if ip, ok := peerDict["ip"].(bencode.String); ok {
						if portVal, ok := peerDict["port"].(bencode.Int); ok {
							peers = append(peers, fmt.Sprintf("%s:%d", string(ip), int(portVal)))
						}
					}
				}
			}
		}
	}

	return peers
}

func (c *Client) RemoveTorrent(infoHash [20]byte) error {
	c.mu.Lock()
	t, exists := c.torrents[infoHash]
	c.mu.Unlock()

	if !exists {
		return fmt.Errorf("torrent not found")
	}

	if t.Tracker != nil && t.TrackerURL != "" {
		req := tracker.AnnounceRequest{
			InfoHash:   infoHash,
			PeerID:     c.peerID,
			Port:       c.port,
			Uploaded:   0,
			Downloaded: 0,
			Left:       0,
			Event:      "stopped",
		}
		t.Tracker.Announce(t.TrackerURL, req)
	}

	c.mu.Lock()
	t.Close()
	delete(c.torrents, infoHash)
	c.mu.Unlock()

	return nil
}

func (c *Client) GetTorrent(infoHash [20]byte) (*Torrent, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	t, exists := c.torrents[infoHash]
	if !exists {
		return nil, fmt.Errorf("torrent not found")
	}
	return t, nil
}

func (c *Client) ListTorrents() []TorrentStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()

	statuses := make([]TorrentStatus, 0, len(c.torrents))
	for _, t := range c.torrents {
		statuses = append(statuses, t.GetStatus())
	}
	return statuses
}

func (c *Client) connectToPeer(t *Torrent, addr string) {
	c.mu.RLock()
	peerID := c.peerID
	infoHash := t.InfoHash
	c.mu.RUnlock()

	p, err := peer.DialPeer(addr, infoHash, peerID)
	if err != nil {
		return
	}

	t.AddPeer(p)

	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		t.RequestPiece(p)
	}()
}

func (c *Client) connectToPeerWithMetadata(t *Torrent, addr string) {
	c.mu.RLock()
	peerID := c.peerID
	infoHash := t.InfoHash
	c.mu.RUnlock()

	p, err := peer.DialPeer(addr, infoHash, peerID)
	if err != nil {
		return
	}

	t.AddPeer(p)

	t.wg.Add(1)
	go func() {
		defer t.wg.Done()

		if c.requestMetadataFromPeer(t, p) {
			return
		}

		t.RequestPiece(p)
	}()
}

func (c *Client) requestMetadataFromPeer(t *Torrent, p *peer.Conn) bool {
	extPayload := map[string]interface{}{
		"m": map[string]int{
			"ut_metadata": 1,
		},
	}

	data, err := bencode.Encode(extPayload)
	if err != nil {
		return false
	}

	if err := p.SendExtendedMessage(peer.ExtendedHandshake, data); err != nil {
		return false
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	timeout := time.After(30 * time.Second)

	for {
		select {
		case <-timeout:
			return false
		case <-ticker.C:
			msg, err := p.ReadMessage()
			if err != nil {
				return false
			}

			if msg == nil || msg.Type != peer.MsgExtended {
				continue
			}

			if len(msg.Payload) == 0 {
				continue
			}

			extID := msg.Payload[0]
			extData := msg.Payload[1:]

			switch extID {
			case peer.ExtendedHandshake:
				if t.Info != nil {
					return false
				}

				hs, err := DecodeMetadataHandshake(extData)
				if err != nil {
					continue
				}

				if hs.MetadataSize == 0 {
					continue
				}

				t.MetadataManager = NewMetadataManager(t.InfoHash)
				t.MetadataManager.SetSize(hs.MetadataSize)

				for _, piece := range t.MetadataManager.MissingPieces() {
					reqPayload, _ := BuildMetadataRequest(piece)
					p.SendExtendedMessage(peer.UtMetadata, reqPayload)
				}

			case peer.UtMetadata:
				msgType, piece, payload, _ := ParseMetadataMessage(extData)
				if msgType == 1 && t.MetadataManager != nil {
					if err := t.MetadataManager.StorePiece(piece, payload); err == nil {
						if t.MetadataManager.IsComplete() {
							infoDict, err := t.MetadataManager.GetInfoDict()
							if err == nil {
								c.mu.Lock()
								t.SetInfoDict(infoDict)
								c.mu.Unlock()
								return true
							}
						}
					}
				}
			}
		}
	}
}

func (c *Client) StartAll() {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, t := range c.torrents {
		if t.Info != nil && t.State == TorrentStateDownloading {
			t.StartDownloading()
		}
	}
}

func (c *Client) StopAll() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, t := range c.torrents {
		t.Pause()
	}
}

func (c *Client) UpdatePeers(t *Torrent) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if t.Tracker == nil {
		return
	}

	req := tracker.AnnounceRequest{
		InfoHash: t.InfoHash,
		PeerID:   c.peerID,
		Port:     c.port,
		Compact:  true,
		Event:    "started",
	}

	resp, err := t.Tracker.Announce(t.TrackerURL, req)
	if err != nil {
		log.Printf("tracker announce error: %v", err)
		return
	}

	for _, peerAddr := range resp.Peers {
		go c.connectToPeer(t, fmt.Sprintf("%s:%d", peerAddr.IP, peerAddr.Port))
	}
}

func GeneratePeerID() [20]byte {
	var id [20]byte
	id[0] = '-'
	id[1] = 'M'
	id[2] = 'G'
	id[3] = '1'
	id[4] = '6'
	id[5] = '0'
	id[6] = '0'
	id[7] = '0'
	id[8] = '-'

	randBytes := make([]byte, 12)
	rand.Read(randBytes)
	copy(id[9:], randBytes)

	return id
}

func HashInfo(infoDict []byte) [20]byte {
	return sha1.Sum(infoDict)
}

func ParsePeerAddress(addr string) (string, int, error) {
	u, err := url.Parse("//" + addr)
	if err != nil {
		return "", 0, err
	}

	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = "6881"
	}

	var portInt int
	fmt.Sscanf(port, "%d", &portInt)

	return host, portInt, nil
}

func (c *Client) SetFilePriority(infoHash [20]byte, fileIndex int, priority int) error {
	c.mu.RLock()
	t, exists := c.torrents[infoHash]
	c.mu.RUnlock()

	if !exists {
		return fmt.Errorf("torrent not found")
	}

	if t.PieceManager == nil {
		return fmt.Errorf("piece manager not initialized")
	}

	t.PieceManager.SetFilePriority(fileIndex, priority)
	return nil
}

func (c *Client) GetFiles(infoHash [20]byte) ([]FileInfo, error) {
	c.mu.RLock()
	t, exists := c.torrents[infoHash]
	c.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("torrent not found")
	}

	return t.GetFiles(), nil
}

func (c *Client) SetDownloadLimit(bytesPerSecond int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.DownloadLimit = bytesPerSecond
	c.limiter.SetGlobalLimits(bytesPerSecond, c.config.UploadLimit)
}

func (c *Client) SetUploadLimit(bytesPerSecond int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config.UploadLimit = bytesPerSecond
	c.limiter.SetGlobalLimits(c.config.DownloadLimit, bytesPerSecond)
}

func (c *Client) GetDownloadLimit() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.config.DownloadLimit
}

func (c *Client) GetUploadLimit() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.config.UploadLimit
}

func (c *Client) GetDownloadStats() (currentRate, totalBytes float64) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return float64(c.limiter.GetTotalDownloaded()), 0
}

func (c *Client) GetUploadStats() (currentRate, totalBytes float64) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return float64(c.limiter.GetTotalUploaded()), 0
}

func (c *Client) GetLimiter() *MultiLimiter {
	return c.limiter
}

func (c *Client) SetSeeding(infoHash [20]byte, enabled bool) error {
	c.mu.RLock()
	t, exists := c.torrents[infoHash]
	c.mu.RUnlock()

	if !exists {
		return fmt.Errorf("torrent not found")
	}

	t.SetSeeding(enabled)
	return nil
}

func (c *Client) IsSeeding(infoHash [20]byte) bool {
	c.mu.RLock()
	t, exists := c.torrents[infoHash]
	c.mu.RUnlock()

	if !exists {
		return false
	}

	return t.IsSeeding()
}

func (c *Client) GetUploadTotal(infoHash [20]byte) int64 {
	c.mu.RLock()
	t, exists := c.torrents[infoHash]
	c.mu.RUnlock()

	if !exists {
		return 0
	}

	return t.GetUploaded()
}
