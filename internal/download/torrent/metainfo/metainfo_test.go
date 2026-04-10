package metainfo

import (
	"testing"
)

func TestParseMetainfo(t *testing.T) {
	input := []byte("d4:infod6:lengthi12345e4:name8:test.txt6:pieces20:\x00\x01\x02\x03\x04\x05\x06\x07\x08\x09\x0a\x0b\x0c\x0d\x0e\x0f\x10\x11\x12\x13ee")

	m, err := ParseMetainfo(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if m.Info.Name != "test.txt" {
		t.Errorf("expected name=test.txt, got %s", m.Info.Name)
	}

	if m.Info.Length != 12345 {
		t.Errorf("expected length=12345, got %d", m.Info.Length)
	}

	if len(m.Info.PiecesHashes) != 1 {
		t.Errorf("expected 1 piece hash, got %d", len(m.Info.PiecesHashes))
	}
}

func TestInfoHash(t *testing.T) {
	m := &Metainfo{
		Info: InfoDict{
			Name:   "test",
			Length: 1024,
			Pieces: []byte("01234567890123456789"),
		},
	}

	hash := m.InfoHash()
	if hash == ([20]byte{}) {
		t.Error("expected non-zero info hash")
	}

	hash2 := m.InfoHash()
	if hash != hash2 {
		t.Error("info hash should be deterministic")
	}
}

func TestCalcTotalSize(t *testing.T) {
	m := &Metainfo{
		Info: InfoDict{
			Name:   "test",
			Length: 1000,
		},
	}
	m.CalcTotalSize()
	if m.Info.TotalSize != 1000 {
		t.Errorf("expected TotalSize=1000, got %d", m.Info.TotalSize)
	}

	m2 := &Metainfo{
		Info: InfoDict{
			Name: "test",
			Files: []FileInfo{
				{Length: 500},
				{Length: 300},
				{Length: 200},
			},
		},
	}
	m2.CalcTotalSize()
	if m2.Info.TotalSize != 1000 {
		t.Errorf("expected TotalSize=1000, got %d", m2.Info.TotalSize)
	}
}

func TestParseMagnet(t *testing.T) {
	tests := []struct {
		uri      string
		expected string
		hasError bool
	}{
		{
			"magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=test",
			"0123456789abcdef0123456789abcdef01234567",
			false,
		},
		{
			"not-a-magnet",
			"",
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.uri, func(t *testing.T) {
			m, err := ParseMagnet(tt.uri)
			if tt.hasError {
				if err == nil {
					t.Errorf("expected error for %s", tt.uri)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if m.InfoHashHex != tt.expected {
				t.Errorf("expected hash=%s, got %s", tt.expected, m.InfoHashHex)
			}
		})
	}
}

func TestMagnetString(t *testing.T) {
	m := &Magnet{
		InfoHashHex: "0123456789abcdef0123456789abcdef01234567",
		Name:        "test file",
		Trackers:    []string{"http://tracker.example.com/announce"},
	}

	uri := m.String()
	if uri[:8] != "magnet:?" {
		t.Errorf("expected magnet URI, got %s", uri[:8])
	}
	if m.InfoHashHex != "0123456789abcdef0123456789abcdef01234567" {
		t.Errorf("expected hash in URI")
	}
}

func TestPeerID(t *testing.T) {
	id := PeerID("TEST12345")
	if id[0] != '-' || id[8] != '-' {
		t.Error("peer ID should start with -XX0000-")
	}
	if id[1] != 'M' || id[2] != 'G' {
		t.Error("peer ID should use MG prefix")
	}
}

func TestNewDefaultPeerID(t *testing.T) {
	id := NewDefaultPeerID()
	if id[0] != '-' {
		t.Error("peer ID should start with -")
	}
}

func TestPackUnpackInfoHash(t *testing.T) {
	original := [20]byte{
		0x01, 0x02, 0x03, 0x04, 0x05,
		0x06, 0x07, 0x08, 0x09, 0x0a,
		0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
		0x10, 0x11, 0x12, 0x13, 0x14,
	}

	packed := PackInfoHash(original)
	unpacked, err := UnpackInfoHash(packed)
	if err != nil {
		t.Fatalf("unpack error: %v", err)
	}
	if unpacked != original {
		t.Error("pack/unpack mismatch")
	}
}

func TestUnpackInfoHashError(t *testing.T) {
	_, err := UnpackInfoHash("too-short")
	if err == nil {
		t.Error("expected error for short input")
	}
}

func TestNumPieces(t *testing.T) {
	m := &Metainfo{
		Info: InfoDict{
			Pieces: []byte("0123456789012345678901234567890123456789"),
		},
	}

	if m.NumPieces() != 2 {
		t.Errorf("expected 2 pieces, got %d", m.NumPieces())
	}
}

func TestParsePieces(t *testing.T) {
	m := &Metainfo{
		Info: InfoDict{
			Pieces: []byte{
				0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09,
				0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13,
				0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d,
				0x1e, 0x1f, 0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27,
			},
		},
	}

	m.ParsePieces()
	if len(m.Info.PiecesHashes) != 2 {
		t.Errorf("expected 2 pieces, got %d", len(m.Info.PiecesHashes))
	}
}
