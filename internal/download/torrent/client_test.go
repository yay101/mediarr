//go:build torrent_integration
// +build torrent_integration

package torrent

import (
	"testing"
)

func TestNewClient_DefaultValues(t *testing.T) {
	cfg := ClientConfig{
		DownloadDir: "/tmp/downloads",
	}

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}

	if client.config.MaxPeers != 50 {
		t.Errorf("expected default max peers 50, got %d", client.config.MaxPeers)
	}

	if client.config.Port != 6881 {
		t.Errorf("expected default port 6881, got %d", client.config.Port)
	}
}

func TestNewClient_PeerID(t *testing.T) {
	peerID := [20]byte{'-', 'T', 'E', 'S', 'T', '0', '0', '0', '1', '-', 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}

	cfg := ClientConfig{
		DownloadDir: "/tmp/downloads",
		PeerID:      peerID,
	}

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.peerID != peerID {
		t.Error("expected peer ID to be set")
	}
}

func TestNewClient_WithLimits(t *testing.T) {
	cfg := ClientConfig{
		DownloadDir:   "/tmp/downloads",
		DownloadLimit: 1024 * 1024,
		UploadLimit:   512 * 1024,
	}

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.limiter == nil {
		t.Error("expected limiter to be initialized")
	}
}

func TestClient_Close_WithoutStart(t *testing.T) {
	cfg := ClientConfig{
		DownloadDir: "/tmp/downloads",
	}

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	client.Close()

	if client.torrents != nil {
		t.Error("expected torrents map to be nil after close")
	}
}

func TestClient_AddTorrentFile_NotFound(t *testing.T) {
	cfg := ClientConfig{
		DownloadDir: "/tmp/downloads",
	}

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer client.Close()

	_, err = client.AddTorrentFile("/nonexistent/file.torrent")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestGeneratePeerID(t *testing.T) {
	id1 := GeneratePeerID()
	id2 := GeneratePeerID()

	if id1[0] != '-' || id1[1] != 'M' || id1[2] != 'G' {
		t.Error("expected peer ID to start with -MG")
	}

	if id2[0] != '-' || id2[1] != 'M' || id2[2] != 'G' {
		t.Error("expected peer ID to start with -MG")
	}

	if id1 == id2 {
		t.Error("expected peer IDs to be unique")
	}
}

func TestParsePeerAddress(t *testing.T) {
	tests := []struct {
		input        string
		expectedIP   string
		expectedPort int
		shouldError  bool
	}{
		{"1.2.3.4:6881", "1.2.3.4", 6881, false},
		{"1.2.3.4", "1.2.3.4", 6881, false},
		{"example.com:6881", "example.com", 6881, false},
		{"example.com", "example.com", 6881, false},
		{"[::1]:6881", "::1", 6881, false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			ip, port, err := ParsePeerAddress(tt.input)
			if tt.shouldError && err == nil {
				t.Error("expected error")
				return
			}
			if !tt.shouldError && err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if ip != tt.expectedIP {
				t.Errorf("expected IP %s, got %s", tt.expectedIP, ip)
			}
			if port != tt.expectedPort {
				t.Errorf("expected port %d, got %d", tt.expectedPort, port)
			}
		})
	}
}

func TestHashInfo(t *testing.T) {
	infoDict := []byte("d4:name10:Test Filet12:piece lengthi1048576e4:pieces20:abcdefghijklmnopqrstee")

	hash := HashInfo(infoDict)

	if hash == [20]byte{} {
		t.Error("expected non-empty hash")
	}
}

func TestClient_SetDownloadLimit(t *testing.T) {
	cfg := ClientConfig{
		DownloadDir: "/tmp/downloads",
	}

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer client.Close()

	client.SetDownloadLimit(1024 * 1024)

	if client.GetDownloadLimit() != 1024*1024 {
		t.Errorf("expected limit 1048576, got %d", client.GetDownloadLimit())
	}
}

func TestClient_SetUploadLimit(t *testing.T) {
	cfg := ClientConfig{
		DownloadDir: "/tmp/downloads",
	}

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer client.Close()

	client.SetUploadLimit(512 * 1024)

	if client.GetUploadLimit() != 512*1024 {
		t.Errorf("expected limit 524288, got %d", client.GetUploadLimit())
	}
}

func TestClient_GetDownloadStats(t *testing.T) {
	cfg := ClientConfig{
		DownloadDir: "/tmp/downloads",
	}

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer client.Close()

	rate, total := client.GetDownloadStats()
	if rate < 0 {
		t.Errorf("expected non-negative rate, got %f", rate)
	}
	if total < 0 {
		t.Errorf("expected non-negative total, got %f", total)
	}
}

func TestClient_GetUploadStats(t *testing.T) {
	cfg := ClientConfig{
		DownloadDir: "/tmp/downloads",
	}

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer client.Close()

	rate, total := client.GetUploadStats()
	if rate < 0 {
		t.Errorf("expected non-negative rate, got %f", rate)
	}
	if total < 0 {
		t.Errorf("expected non-negative total, got %f", total)
	}
}

func TestClient_GetLimiter(t *testing.T) {
	cfg := ClientConfig{
		DownloadDir:   "/tmp/downloads",
		DownloadLimit: 1024,
		UploadLimit:   512,
	}

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer client.Close()

	limiter := client.GetLimiter()
	if limiter == nil {
		t.Error("expected non-nil limiter")
	}
}

func TestClient_ListTorrents_Empty(t *testing.T) {
	cfg := ClientConfig{
		DownloadDir: "/tmp/downloads",
	}

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer client.Close()

	torrents := client.ListTorrents()
	if len(torrents) != 0 {
		t.Errorf("expected 0 torrents, got %d", len(torrents))
	}
}

func TestClient_GetTorrent_NotFound(t *testing.T) {
	cfg := ClientConfig{
		DownloadDir: "/tmp/downloads",
	}

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer client.Close()

	infoHash := [20]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13, 0x14}
	_, err = client.GetTorrent(infoHash)
	if err == nil {
		t.Error("expected error for nonexistent torrent")
	}
}

func TestClient_RemoveTorrent_NotFound(t *testing.T) {
	cfg := ClientConfig{
		DownloadDir: "/tmp/downloads",
	}

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer client.Close()

	infoHash := [20]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13, 0x14}
	err = client.RemoveTorrent(infoHash)
	if err == nil {
		t.Error("expected error for nonexistent torrent")
	}
}

func TestClient_SetFilePriority_NotFound(t *testing.T) {
	cfg := ClientConfig{
		DownloadDir: "/tmp/downloads",
	}

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer client.Close()

	infoHash := [20]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13, 0x14}
	err = client.SetFilePriority(infoHash, 0, 1)
	if err == nil {
		t.Error("expected error for nonexistent torrent")
	}
}

func TestClient_GetFiles_NotFound(t *testing.T) {
	cfg := ClientConfig{
		DownloadDir: "/tmp/downloads",
	}

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer client.Close()

	infoHash := [20]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13, 0x14}
	_, err = client.GetFiles(infoHash)
	if err == nil {
		t.Error("expected error for nonexistent torrent")
	}
}

func TestClient_SetSeeding_NotFound(t *testing.T) {
	cfg := ClientConfig{
		DownloadDir: "/tmp/downloads",
	}

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer client.Close()

	infoHash := [20]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13, 0x14}
	err = client.SetSeeding(infoHash, true)
	if err == nil {
		t.Error("expected error for nonexistent torrent")
	}
}

func TestClient_IsSeeding_NotFound(t *testing.T) {
	cfg := ClientConfig{
		DownloadDir: "/tmp/downloads",
	}

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer client.Close()

	infoHash := [20]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13, 0x14}
	if client.IsSeeding(infoHash) {
		t.Error("expected false for nonexistent torrent")
	}
}

func TestClient_GetUploadTotal_NotFound(t *testing.T) {
	cfg := ClientConfig{
		DownloadDir: "/tmp/downloads",
	}

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer client.Close()

	infoHash := [20]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13, 0x14}
	total := client.GetUploadTotal(infoHash)
	if total != 0 {
		t.Errorf("expected 0 for nonexistent torrent, got %d", total)
	}
}

func TestExtractPeers_Compact(t *testing.T) {
	peers := extractPeers(nil)
	if len(peers) != 0 {
		t.Errorf("expected 0 peers for nil dict, got %d", len(peers))
	}
}

func TestClient_StartAll_StopAll(t *testing.T) {
	cfg := ClientConfig{
		DownloadDir: "/tmp/downloads",
	}

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer client.Close()

	client.StartAll()
	client.StopAll()
}

func TestClient_AddMagnet_InvalidURI(t *testing.T) {
	cfg := ClientConfig{
		DownloadDir: "/tmp/downloads",
	}

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer client.Close()

	_, err = client.AddMagnet("invalid-magnet-uri")
	if err == nil {
		t.Error("expected error for invalid magnet URI")
	}
}

func TestClient_AddMetainfo_Duplicate(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := ClientConfig{
		DownloadDir: tmpDir,
	}

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer client.Close()
}
