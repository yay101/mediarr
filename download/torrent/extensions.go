package torrent

import (
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/yay101/mediarr/download/torrent/bencode"
)

const (
	ExtensionBit      = 0x10  // Reserved byte index for extension protocol
	MetadataPieceSize = 16384 // 16KB chunks
)

const (
	ExtendedMessageHandshake  = 0
	ExtendedMessageUtMetadata = 1
	ExtendedMessageUtPex      = 2
)

const (
	UtMetadata HandshakeMessageType = iota
	UtPex
)

type HandshakeMessageType int

type ExtensionMessage struct {
	Type    HandshakeMessageType
	Payload []byte
}

type MetadataRequest struct {
	MsgType int
	Piece   int
}

type MetadataData struct {
	MsgType   int
	Piece     int
	TotalSize int
	Data      []byte
}

type MetadataHandshake struct {
	M            string    `bencode:"m"`
	UTMetadata   int       `bencode:"ut_metadata"`
	MetadataSize int       `bencode:"metadata_size,omitempty"`
	Port         int       `bencode:"p,omitempty"`
	Yourip       string    `bencode:"yourip,omitempty"`
	Reqq         int       `bencode:"reqq,omitempty"`
	Encryption   *struct{} `bencode:"e,omitempty"`
}

func NewMetadataHandshake(utMetadataID int) *MetadataHandshake {
	return &MetadataHandshake{
		M:          "ut_metadata",
		UTMetadata: utMetadataID,
		Reqq:       250,
	}
}

func (h *MetadataHandshake) Encode() ([]byte, error) {
	d := bencode.Dict{}

	mDict := bencode.Dict{
		"ut_metadata": bencode.Int(h.UTMetadata),
	}
	d["m"] = mDict

	if h.MetadataSize > 0 {
		d["metadata_size"] = bencode.Int(h.MetadataSize)
	}
	if h.Port > 0 {
		d["p"] = bencode.Int(h.Port)
	}
	if h.Yourip != "" {
		d["yourip"] = bencode.String(h.Yourip)
	}
	if h.Reqq > 0 {
		d["reqq"] = bencode.Int(h.Reqq)
	}

	return bencode.Encode(d)
}

func DecodeMetadataHandshake(data []byte) (*MetadataHandshake, error) {
	val, err := bencode.Decode(data)
	if err != nil {
		return nil, err
	}

	dict, ok := val.(bencode.Dict)
	if !ok {
		return nil, fmt.Errorf("expected dict for handshake")
	}

	h := &MetadataHandshake{}

	if mVal, ok := dict["m"]; ok {
		if mDict, ok := mVal.(bencode.Dict); ok {
			if ut, ok := mDict["ut_metadata"]; ok {
				if i, ok := ut.(bencode.Int); ok {
					h.UTMetadata = int(i)
				}
			}
		}
	}

	if v, ok := dict["metadata_size"]; ok {
		if i, ok := v.(bencode.Int); ok {
			h.MetadataSize = int(i)
		}
	}

	if v, ok := dict["p"]; ok {
		if i, ok := v.(bencode.Int); ok {
			h.Port = int(i)
		}
	}

	if v, ok := dict["yourip"]; ok {
		if s, ok := v.(bencode.String); ok {
			h.Yourip = string(s)
		}
	}

	if v, ok := dict["reqq"]; ok {
		if i, ok := v.(bencode.Int); ok {
			h.Reqq = int(i)
		}
	}

	return h, nil
}

type MetadataManager struct {
	infoDict       bencode.Dict
	infoBytes      []byte
	infoHash       [20]byte
	pieceSize      int
	totalPieces    int
	receivedPieces []bool
	completed      bool
	mu             sync.Mutex
}

func NewMetadataManager(infoHash [20]byte) *MetadataManager {
	mm := &MetadataManager{
		infoHash: infoHash,
	}
	return mm
}

func (mm *MetadataManager) SetSize(size int) {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	mm.pieceSize = MetadataPieceSize
	mm.totalPieces = (size + MetadataPieceSize - 1) / MetadataPieceSize
	mm.receivedPieces = make([]bool, mm.totalPieces)
	mm.infoBytes = make([]byte, 0, size)
}

func (mm *MetadataManager) StorePiece(piece int, data []byte) error {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	if piece < 0 || piece >= mm.totalPieces {
		return fmt.Errorf("invalid piece index: %d", piece)
	}

	if mm.receivedPieces[piece] {
		return nil
	}

	mm.receivedPieces[piece] = true
	mm.infoBytes = append(mm.infoBytes, data...)

	for _, received := range mm.receivedPieces {
		if !received {
			return nil
		}
	}

	mm.completed = true
	return nil
}

func (mm *MetadataManager) IsComplete() bool {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	return mm.completed
}

func (mm *MetadataManager) GetInfoDict() (bencode.Dict, error) {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	if !mm.completed {
		return nil, fmt.Errorf("metadata not complete")
	}

	hash := sha1.Sum(mm.infoBytes)
	if hash != mm.infoHash {
		return nil, fmt.Errorf("metadata hash mismatch")
	}

	val, err := bencode.Decode(mm.infoBytes)
	if err != nil {
		return nil, fmt.Errorf("decode metadata: %w", err)
	}

	dict, ok := val.(bencode.Dict)
	if !ok {
		return nil, fmt.Errorf("expected dict for info")
	}

	mm.infoDict = dict
	return dict, nil
}

func (mm *MetadataManager) MissingPieces() []int {
	var missing []int
	for i, received := range mm.receivedPieces {
		if !received {
			missing = append(missing, i)
		}
	}
	return missing
}

func BuildMetadataRequest(piece int) ([]byte, error) {
	payload := make([]byte, 8)
	binary.BigEndian.PutUint32(payload[0:4], 0) // msg_type = request
	binary.BigEndian.PutUint32(payload[4:8], uint32(piece))
	return payload, nil
}

func ParseMetadataMessage(data []byte) (int, int, []byte, error) {
	if len(data) < 8 {
		return 0, 0, nil, fmt.Errorf("metadata message too short")
	}

	msgType := int(binary.BigEndian.Uint32(data[0:4]))
	piece := int(binary.BigEndian.Uint32(data[4:8]))
	payload := data[8:]

	return msgType, piece, payload, nil
}

func BuildMetadataData(piece int, totalSize int, data []byte) ([]byte, error) {
	payload := make([]byte, 8+len(data))
	binary.BigEndian.PutUint32(payload[0:4], 1) // msg_type = data
	binary.BigEndian.PutUint32(payload[4:8], uint32(piece))
	copy(payload[8:], data)

	// Add metadata_size for first piece
	if piece == 0 {
		sizeBytes := make([]byte, 4)
		binary.BigEndian.PutUint32(sizeBytes, uint32(totalSize))
		payload = append(sizeBytes, payload...)
	}

	return payload, nil
}

type ExtensionHandshake struct {
	Client  *Client
	peerExt map[string]*PeerExtension
}

type PeerExtension struct {
	PeerID       [20]byte
	UTMetadata   int
	MetadataSize int
	HasMetadata  bool
}

func NewExtensionHandshake(c *Client) *ExtensionHandshake {
	return &ExtensionHandshake{
		Client:  c,
		peerExt: make(map[string]*PeerExtension),
	}
}

func (e *ExtensionHandshake) HandleHandshake(peerID string, extID int, data []byte) error {
	hs, err := DecodeMetadataHandshake(data)
	if err != nil {
		return err
	}

	ext := &PeerExtension{
		PeerID:       [20]byte{},
		UTMetadata:   hs.UTMetadata,
		MetadataSize: hs.MetadataSize,
		HasMetadata:  hs.MetadataSize > 0,
	}

	e.peerExt[peerID] = ext
	return nil
}

func (e *ExtensionHandshake) BuildHandshakeMessage() []byte {
	hs := NewMetadataHandshake(1) // Our ut_metadata ID is 1
	hs.MetadataSize = 0           // We don't have metadata to share (seeding only)
	data, _ := hs.Encode()
	return data
}

func (e *ExtensionHandshake) GetPeerExt(peerID string) *PeerExtension {
	return e.peerExt[peerID]
}
