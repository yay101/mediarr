package tracker

import (
	"testing"
)

func TestParsePeersList(t *testing.T) {
	input := []byte("d5:peersld2:ip3:foo4:porti1234eeee")
	val, err := parseAnnounceResponse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(val.Peers) != 1 {
		t.Fatalf("expected 1 peer, got %d", len(val.Peers))
	}
	if val.Peers[0].IP != "foo" {
		t.Errorf("expected IP=foo, got %s", val.Peers[0].IP)
	}
	if val.Peers[0].Port != 1234 {
		t.Errorf("expected Port=1234, got %d", val.Peers[0].Port)
	}
}

func TestParsePeersCompact(t *testing.T) {
	input := []byte("d5:peers6:\x01\x02\x03\x04\x01\x2ee")

	val, err := parseAnnounceResponse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(val.Peers) != 1 {
		t.Fatalf("expected 1 peer, got %d", len(val.Peers))
	}
	if val.Peers[0].IP != "1.2.3.4" {
		t.Errorf("expected IP=1.2.3.4, got %s", val.Peers[0].IP)
	}
	if val.Peers[0].Port != 302 {
		t.Errorf("expected Port=302, got %d", val.Peers[0].Port)
	}
}

func TestParseAnnounceResponse(t *testing.T) {
	input := []byte("d8:intervali1800e5:peers6:\x01\x02\x03\x04\x01\x2eee")

	val, err := parseAnnounceResponse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if val.Interval != 1800 {
		t.Errorf("expected interval=1800, got %d", val.Interval)
	}
}

func TestBuildScrapeRequest(t *testing.T) {
	url := BuildScrapeRequest("http://tracker.example.com/announce?foo=bar")
	if url != "http://tracker.example.com/scrape?foo=bar" {
		t.Errorf("unexpected scrape URL: %s", url)
	}
}

func TestBuildTrackerURL(t *testing.T) {
	req := AnnounceRequest{
		InfoHash:   [20]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13, 0x14},
		PeerID:     [20]byte{0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28, 0x29, 0x2a, 0x2b, 0x2c, 0x2d, 0x2e, 0x2f, 0x30, 0x31, 0x32, 0x33},
		Port:       6881,
		Uploaded:   0,
		Downloaded: 0,
		Left:       1234567,
		Compact:    true,
		Event:      "started",
	}

	url, err := BuildTrackerURL("http://tracker.example.com/announce", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if url == "" {
		t.Error("expected non-empty URL")
	}
}

func TestNewClient(t *testing.T) {
	peerID := [20]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13, 0x14}
	c := NewClient(peerID, 6881)

	if c == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestAnnounceRequestFields(t *testing.T) {
	req := AnnounceRequest{
		InfoHash:   [20]byte{0x01},
		PeerID:     [20]byte{0x02},
		Port:       6881,
		Uploaded:   1024,
		Downloaded: 512,
		Left:       2048,
		Compact:    true,
		Event:      "started",
	}

	if req.Port != 6881 {
		t.Errorf("expected port=6881, got %d", req.Port)
	}
	if req.Event != "started" {
		t.Errorf("expected event=started, got %s", req.Event)
	}
}
