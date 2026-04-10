package torrent

import (
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/yay101/mediarr/download/torrent/bencode"
	"github.com/yay101/mediarr/download/torrent/metainfo"
	"github.com/yay101/mediarr/download/torrent/peer"
	"github.com/yay101/mediarr/download/torrent/tracker"
)

// TorrentState represents the current state of a torrent download.
type TorrentState byte

const (
	TorrentStateMetadata    TorrentState = iota // Waiting for metadata (magnet links)
	TorrentStateDownloading                     // Downloading pieces
	TorrentStateSeeding                         // Seeding completed torrent
	TorrentStatePaused                          // Download paused
	TorrentStateError                           // Error occurred
	TorrentStateComplete                        // Download complete, not yet seeding
)

// FileWrite tracks progress for a single file within a multi-file torrent.
type FileWrite struct {
	Path         string // Destination path for this file
	Length       int64  // Total file size
	BytesWritten int64  // Bytes written so far
	Priority     int    // Download priority (higher = sooner)
}

// Torrent represents a single BitTorrent download/upload.
// Handles piece management, peer connections, and tracker communication.
type Torrent struct {
	InfoHash        [20]byte
	Name            string
	State           TorrentState
	Info            *metainfo.InfoDict
	PieceManager    *PieceManager
	MetadataManager *MetadataManager

	Tracker    *tracker.Client
	TrackerURL string
	Peers      map[string]*peer.Conn
	peerID     [20]byte
	maxPeers   int

	DataDir   string
	Files     []FileWrite
	TotalSize int64
	BytesDone int64

	// Callbacks for progress, completion, and errors
	progressCB func(progress float32)
	errorCB    func(err error)
	seedingCB  func(pieceIndex int, offset int, length int) ([]byte, error)
	completeCB func()
	seeding    bool
	uploaded   int64 // Total bytes uploaded
	downloaded int64 // Total bytes downloaded

	announceInterval time.Duration // Tracker announce interval
	lastAnnounce     time.Time     // Last tracker announce time

	mu      sync.RWMutex
	closeCh chan struct{}
	wg      sync.WaitGroup
}

// TorrentConfig contains configuration for creating a new Torrent.
type TorrentConfig struct {
	InfoHash   [20]byte
	Name       string
	Info       *metainfo.InfoDict     // Torrent metadata (nil for magnet links)
	DataDir    string                 // Directory to store downloaded files
	PeerID     [20]byte               // Client peer ID
	MaxPeers   int                    // Maximum concurrent peer connections
	Seeding    bool                   // Continue seeding after download completes
	ProgressCB func(progress float32) // Progress callback
	CompleteCB func()                 // Completion callback
	ErrorCB    func(err error)        // Error callback
}

// NewTorrent creates a new Torrent instance from configuration.
func NewTorrent(cfg TorrentConfig) (*Torrent, error) {
	if cfg.MaxPeers <= 0 {
		cfg.MaxPeers = 50
	}

	t := &Torrent{
		InfoHash:   cfg.InfoHash,
		Name:       cfg.Name,
		seeding:    cfg.Seeding,
		State:      TorrentStateMetadata,
		peerID:     cfg.PeerID,
		maxPeers:   cfg.MaxPeers,
		DataDir:    cfg.DataDir,
		completeCB: cfg.CompleteCB,
		Peers:      make(map[string]*peer.Conn),
		closeCh:    make(chan struct{}),
		progressCB: cfg.ProgressCB,
		errorCB:    cfg.ErrorCB,
	}

	if cfg.Info != nil {
		t.Info = cfg.Info
		t.InfoHash = cfg.InfoHash
		t.PieceManager = NewPieceManager(cfg.Info)
		t.CalcFiles()
		t.TotalSize = cfg.Info.TotalSize
		if t.Name == "" {
			t.Name = cfg.Info.Name
		}
		t.State = TorrentStateDownloading
	}

	return t, nil
}

func (t *Torrent) CalcFiles() {
	t.Files = nil

	if t.Info == nil {
		return
	}

	if t.Info.Length > 0 {
		t.Files = append(t.Files, FileWrite{
			Path:     filepath.Join(t.DataDir, t.Info.Name),
			Length:   t.Info.Length,
			Priority: 0,
		})
		return
	}

	basePath := filepath.Join(t.DataDir, t.Info.Name)
	for i, f := range t.Info.Files {
		t.Files = append(t.Files, FileWrite{
			Path:     filepath.Join(append([]string{basePath}, f.Path...)...),
			Length:   f.Length,
			Priority: i,
		})
	}
}

func (t *Torrent) SetInfo(info *metainfo.InfoDict) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if info == nil {
		return fmt.Errorf("nil info dict")
	}

	t.Info = info
	t.PieceManager = NewPieceManager(info)
	t.CalcFiles()
	t.TotalSize = info.TotalSize
	t.State = TorrentStateDownloading

	return nil
}

func (t *Torrent) SetTracker(trackerClient *tracker.Client) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Tracker = trackerClient
}

func (t *Torrent) SetInfoDict(infoDict bencode.Dict) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if infoDict == nil {
		return fmt.Errorf("nil info dict")
	}

	info, err := t.convertInfoDict(infoDict)
	if err != nil {
		return fmt.Errorf("convert info dict: %w", err)
	}

	t.Info = info
	t.InfoHash = t.computeInfoHash(infoDict)
	t.PieceManager = NewPieceManager(info)
	t.CalcFiles()
	t.TotalSize = info.TotalSize
	t.State = TorrentStateDownloading
	t.MetadataManager = nil

	return nil
}

func (t *Torrent) convertInfoDict(d bencode.Dict) (*metainfo.InfoDict, error) {
	info := &metainfo.InfoDict{}

	if v, ok := d["piece length"]; ok {
		if i, ok := v.(bencode.Int); ok {
			info.PieceLength = int64(i)
		}
	}

	if v, ok := d["pieces"]; ok {
		if s, ok := v.(bencode.String); ok {
			info.Pieces = []byte(s)
		}
	}

	if v, ok := d["private"]; ok {
		if i, ok := v.(bencode.Int); ok {
			info.Private = int64(i)
		}
	}

	if v, ok := d["name"]; ok {
		if s, ok := v.(bencode.String); ok {
			info.Name = string(s)
		}
	}

	if v, ok := d["length"]; ok {
		if i, ok := v.(bencode.Int); ok {
			info.Length = int64(i)
		}
	}

	if v, ok := d["files"]; ok {
		if list, ok := v.(bencode.List); ok {
			for _, fileVal := range list {
				if fileDict, ok := fileVal.(bencode.Dict); ok {
					file := metainfo.FileInfo{}
					if lengthVal, ok := fileDict["length"]; ok {
						if i, ok := lengthVal.(bencode.Int); ok {
							file.Length = int64(i)
						}
					}
					if pathVal, ok := fileDict["path"]; ok {
						if pathList, ok := pathVal.(bencode.List); ok {
							for _, pathPart := range pathList {
								if s, ok := pathPart.(bencode.String); ok {
									file.Path = append(file.Path, string(s))
								}
							}
						}
					}
					info.Files = append(info.Files, file)
				}
			}
		}
	}

	info.PiecesHashes = make([][20]byte, len(info.Pieces)/20)
	for i := 0; i < len(info.Pieces)/20; i++ {
		copy(info.PiecesHashes[i][:], info.Pieces[i*20:(i+1)*20])
	}

	if info.Length > 0 {
		info.TotalSize = info.Length
	} else {
		info.TotalSize = 0
		for _, f := range info.Files {
			info.TotalSize += f.Length
		}
	}

	return info, nil
}

func (t *Torrent) computeInfoHash(infoDict bencode.Dict) [20]byte {
	infoBytes, _ := bencode.Encode(infoDict)
	hash := sha1.Sum(infoBytes)
	return hash
}

func (t *Torrent) AddPeer(conn *peer.Conn) {
	t.mu.Lock()
	defer t.mu.Unlock()

	addr := conn.RemoteAddr().String()
	if _, exists := t.Peers[addr]; exists {
		return
	}

	if len(t.Peers) >= t.maxPeers {
		conn.Close()
		return
	}

	t.Peers[addr] = conn
}

func (t *Torrent) RemovePeer(addr string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if conn, exists := t.Peers[addr]; exists {
		conn.Close()
		delete(t.Peers, addr)
		if t.PieceManager != nil {
			t.PieceManager.RemovePeer(addr)
		}
	}
}

func (t *Torrent) Close() {
	close(t.closeCh)

	t.mu.Lock()
	for _, conn := range t.Peers {
		conn.Close()
	}
	t.Peers = nil
	t.mu.Unlock()

	t.wg.Wait()
}

func (t *Torrent) GetStatus() TorrentStatus {
	t.mu.RLock()
	defer t.mu.RUnlock()

	status := TorrentStatus{
		InfoHash:       t.InfoHash,
		Name:           t.Name,
		State:          string(t.stateString()),
		BytesTotal:     t.TotalSize,
		BytesDone:      t.BytesDone,
		PeersConnected: len(t.Peers),
	}

	if t.PieceManager != nil {
		status.Progress = t.PieceManager.Progress()
		status.NumPieces = t.PieceManager.NumPieces
		status.PiecesComplete = t.PieceManager.CompletedCount()
	}

	return status
}

func (t *Torrent) stateString() string {
	switch t.State {
	case TorrentStateMetadata:
		return "metadata"
	case TorrentStateDownloading:
		return "downloading"
	case TorrentStateSeeding:
		return "seeding"
	case TorrentStatePaused:
		return "paused"
	case TorrentStateError:
		return "error"
	case TorrentStateComplete:
		return "complete"
	default:
		return "unknown"
	}
}

func (t *Torrent) WritePiece(pieceIndex int) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.PieceManager == nil {
		return fmt.Errorf("no piece manager")
	}

	data, err := t.PieceManager.GetPieceData(pieceIndex)
	if err != nil {
		return fmt.Errorf("get piece data: %w", err)
	}

	if len(t.Files) == 0 {
		return nil
	}

	pieceLength := t.Info.PieceLength
	if pieceIndex == t.PieceManager.NumPieces-1 {
		remainder := t.TotalSize % pieceLength
		if remainder > 0 {
			pieceLength = remainder
		}
	}

	offset := int64(pieceIndex) * t.Info.PieceLength

	for i, fw := range t.Files {
		if offset >= fw.Length {
			offset -= fw.Length
			continue
		}

		if err := os.MkdirAll(filepath.Dir(fw.Path), 0755); err != nil {
			return fmt.Errorf("mkdir: %w", err)
		}

		f, err := os.OpenFile(fw.Path, os.O_WRONLY|os.O_CREATE, 0644)
		if err != nil {
			return fmt.Errorf("open file: %w", err)
		}

		if offset > 0 {
			if _, err := f.Seek(offset, io.SeekStart); err != nil {
				f.Close()
				return fmt.Errorf("seek: %w", err)
			}
			offset = 0
		}

		writeLen := int64(len(data))
		if fw.Length-fw.BytesWritten < writeLen {
			writeLen = fw.Length - fw.BytesWritten
		}

		n, err := f.Write(data[:writeLen])
		f.Close()

		if err != nil {
			return fmt.Errorf("write: %w", err)
		}

		t.Files[i].BytesWritten += int64(n)
		t.BytesDone += int64(n)
		data = data[n:]

		if len(data) == 0 {
			break
		}
	}

	return nil
}

func (t *Torrent) WriteAllPieces() error {
	if t.PieceManager == nil {
		return fmt.Errorf("no piece manager")
	}

	for i := 0; i < t.PieceManager.NumPieces; i++ {
		if err := t.WritePiece(i); err != nil {
			return fmt.Errorf("write piece %d: %w", i, err)
		}
	}

	return nil
}

func (t *Torrent) StartDownloading() error {
	if t.Info == nil {
		return fmt.Errorf("metadata not available")
	}

	t.mu.Lock()
	if t.State == TorrentStateDownloading || t.State == TorrentStateSeeding {
		t.mu.Unlock()
		return nil
	}
	t.State = TorrentStateDownloading
	t.mu.Unlock()

	t.wg.Add(1)
	go t.downloadLoop()

	return nil
}

func (t *Torrent) downloadLoop() {
	defer t.wg.Done()

	peerTicker := time.NewTicker(5 * time.Second)
	announceTicker := time.NewTicker(t.announceInterval)
	defer peerTicker.Stop()
	defer announceTicker.Stop()

	if t.announceInterval == 0 {
		t.announceInterval = 30 * time.Minute
	}

	for {
		select {
		case <-t.closeCh:
			return
		case <-peerTicker.C:
			t.processPeers()
		case <-announceTicker.C:
			t.announceTracker()
		}
	}
}

func (t *Torrent) processPeers() {
	t.mu.RLock()
	peers := make([]*peer.Conn, 0, len(t.Peers))
	for _, p := range t.Peers {
		peers = append(peers, p)
	}
	t.mu.RUnlock()

	for _, p := range peers {
		t.ProcessPeer(p)
	}

	t.updateProgress()
}

func (t *Torrent) announceTracker() {
	if t.Tracker == nil || t.TrackerURL == "" {
		return
	}

	t.mu.RLock()
	left := t.TotalSize - t.BytesDone
	if left < 0 {
		left = 0
	}

	event := ""
	if t.State == TorrentStateComplete || t.PieceManager.IsComplete() {
		event = "completed"
	} else if t.State == TorrentStatePaused {
		event = "stopped"
	}
	t.mu.RUnlock()

	req := tracker.AnnounceRequest{
		InfoHash:   t.InfoHash,
		PeerID:     t.peerID,
		Port:       0,
		Uploaded:   t.uploaded,
		Downloaded: t.downloaded,
		Left:       left,
		Compact:    true,
		Event:      event,
	}

	resp, err := t.Tracker.Announce(t.TrackerURL, req)
	if err != nil {
		return
	}

	if resp.Interval > 0 {
		t.mu.Lock()
		t.announceInterval = time.Duration(resp.Interval) * time.Second
		t.lastAnnounce = time.Now()
		t.mu.Unlock()
	}

	for _, peerAddr := range resp.Peers {
		addr := fmt.Sprintf("%s:%d", peerAddr.IP, peerAddr.Port)
		t.mu.RLock()
		_, exists := t.Peers[addr]
		t.mu.RUnlock()
		if !exists {
			go t.connectToPeer(addr)
		}
	}
}

func (t *Torrent) connectToPeer(addr string) {
	p, err := peer.DialPeer(addr, t.InfoHash, t.peerID)
	if err != nil {
		return
	}

	t.AddPeer(p)

	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		t.ProcessPeer(p)
	}()
}

func (t *Torrent) ProcessPeer(p *peer.Conn) {
	for {
		msg, err := p.ReadMessage()
		if err != nil {
			t.RemovePeer(p.RemoteAddr().String())
			return
		}

		if msg == nil || msg.Type == peer.MsgChoke {
			continue
		}

		switch msg.Type {
		case peer.MsgHave:
			pieceIdx, _ := peer.ParseHave(msg)
			t.PieceManager.PeerHave(p.RemoteAddr().String(), int(pieceIdx))

		case peer.MsgBitfield:
			t.PieceManager.SetPeerBitfield(p.RemoteAddr().String(), msg.Payload)

		case peer.MsgPiece:
			index, begin, data, _ := peer.ParsePiece(msg)

			t.mu.Lock()
			t.downloaded += int64(len(data))
			t.mu.Unlock()

			if err := t.PieceManager.StoreBlock(int(index), int(begin), data); err != nil {
				continue
			}

			t.mu.RLock()
			piece := t.PieceManager.Pieces[index]
			t.mu.RUnlock()

			if piece.BlocksGot == len(piece.Blocks) {
				if ok, _ := t.PieceManager.VerifyPiece(int(index)); ok {
					if err := t.WritePiece(int(index)); err != nil {
						if t.errorCB != nil {
							t.errorCB(fmt.Errorf("write piece: %w", err))
						}
					}
				}
			}

		case peer.MsgUnchoke:
			t.RequestPiece(p)

		case peer.MsgRequest:
			if t.seeding {
				index, begin, length, _ := peer.ParseRequest(msg)
				go t.HandleRequest(p, index, begin, length)
			}
		}
	}
}

func (t *Torrent) RequestPiece(p *peer.Conn) {
	if t.Info == nil {
		return
	}

	peerID := p.RemoteAddr().String()

	pieceIdx, err := t.PieceManager.RarestPiece(peerID)
	if err != nil {
		return
	}

	if err := p.SendInterested(); err != nil {
		return
	}

	piece := t.PieceManager.Pieces[pieceIdx]
	for _, block := range piece.Blocks {
		if block.Done {
			continue
		}
		p.SendRequest(uint32(pieceIdx), uint32(block.Offset), uint32(block.Length))
	}
}

func (t *Torrent) updateProgress() {
	if t.progressCB == nil || t.PieceManager == nil {
		return
	}

	progress := t.PieceManager.Progress()
	if t.progressCB != nil {
		t.progressCB(progress)
	}

	if t.PieceManager.IsComplete() && t.State != TorrentStateComplete {
		t.mu.Lock()
		t.State = TorrentStateComplete
		if t.completeCB != nil {
			t.completeCB()
		}
		t.mu.Unlock()
	}
}

type TorrentStatus struct {
	InfoHash       [20]byte
	Name           string
	State          string
	BytesTotal     int64
	BytesDone      int64
	Progress       float32
	PeersConnected int
	NumPieces      int
	PiecesComplete int
}

func (t *Torrent) GetFiles() []FileInfo {
	t.mu.RLock()
	defer t.mu.RUnlock()

	files := make([]FileInfo, len(t.Files))
	for i, f := range t.Files {
		files[i] = FileInfo{
			Path:      f.Path,
			Length:    f.Length,
			BytesDone: f.BytesWritten,
		}
	}
	return files
}

type FileInfo struct {
	Path      string
	Length    int64
	BytesDone int64
}

func (t *Torrent) Pause() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.State = TorrentStatePaused
}

func (t *Torrent) Resume() error {
	if t.Info == nil {
		return fmt.Errorf("metadata not available")
	}
	t.mu.Lock()
	t.State = TorrentStateDownloading
	t.mu.Unlock()
	t.StartDownloading()
	return nil
}

type TorrentFile struct {
	*os.File
	Info *metainfo.InfoDict
}

func OpenTorrentFile(path string) (*TorrentFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	m, err := metainfo.ParseMetainfo(data)
	if err != nil {
		return nil, fmt.Errorf("parse metainfo: %w", err)
	}

	return &TorrentFile{Info: &m.Info}, nil
}

func (t *Torrent) SetSeeding(enabled bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.seeding = enabled
}

func (t *Torrent) IsSeeding() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.seeding
}

func (t *Torrent) GetUploaded() int64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.uploaded
}

func (t *Torrent) SetPieceReader(cb func(pieceIndex int, offset int, length int) ([]byte, error)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.seedingCB = cb
}

func (t *Torrent) ReadPieceData(pieceIndex int, offset int, length int) ([]byte, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.PieceManager != nil {
		if state := t.PieceManager.GetPieceState(pieceIndex); state == PieceVerified {
			data, err := t.PieceManager.GetPieceData(pieceIndex)
			if err == nil && len(data) >= offset+length {
				return data[offset : offset+length], nil
			}
		}
	}

	if t.seedingCB != nil {
		return t.seedingCB(pieceIndex, offset, length)
	}

	return nil, fmt.Errorf("piece not available")
}

func (t *Torrent) HandleRequest(p *peer.Conn, pieceIndex, offset, length uint32) error {
	if !t.seeding {
		return nil
	}

	data, err := t.ReadPieceData(int(pieceIndex), int(offset), int(length))
	if err != nil {
		return err
	}

	msg := &peer.Message{
		Type:    peer.MsgPiece,
		Payload: make([]byte, 8+len(data)),
	}
	binary.BigEndian.PutUint32(msg.Payload[0:4], pieceIndex)
	binary.BigEndian.PutUint32(msg.Payload[4:8], offset)
	copy(msg.Payload[8:], data)

	if err := p.WriteMessage(msg); err != nil {
		return err
	}

	t.mu.Lock()
	t.uploaded += int64(len(data))
	t.mu.Unlock()

	return nil
}
