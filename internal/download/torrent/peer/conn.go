package peer

import (
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"
)

const (
	Protocol = "BitTorrent protocol"
	ProtoLen = 19

	// Message types
	MsgChoke         = 0
	MsgUnchoke       = 1
	MsgInterested    = 2
	MsgNotInterested = 3
	MsgHave          = 4
	MsgBitfield      = 5
	MsgRequest       = 6
	MsgPiece         = 7
	MsgCancel        = 8
	MsgPort          = 9
	MsgExtended      = 20
)

const (
	ExtensionBit = 0x10
)

const (
	ExtendedHandshake = 0
	UtMetadata        = 1
	UtPex             = 2
)

type Message struct {
	Type    byte
	Payload []byte
}

type Conn struct {
	net.Conn
	InfoHash       [20]byte
	PeerID         [20]byte
	AmChoking      bool
	AmInterested   bool
	PeerChoking    bool
	PeerInterested bool
	Bitfield       []byte
}

type Handshake struct {
	Protocol [19]byte
	Reserved [8]byte
	InfoHash [20]byte
	PeerID   [20]byte
}

func (h *Handshake) Bytes() []byte {
	result := make([]byte, 68)
	copy(result[0:19], h.Protocol[:])
	copy(result[19:27], h.Reserved[:])
	copy(result[27:47], h.InfoHash[:])
	copy(result[47:67], h.PeerID[:])
	return result
}

func ParseHandshake(data []byte) (*Handshake, error) {
	if len(data) < 68 {
		return nil, fmt.Errorf("handshake too short: %d bytes", len(data))
	}

	h := &Handshake{}
	copy(h.Protocol[:], data[0:19])
	copy(h.Reserved[:], data[19:27])
	copy(h.InfoHash[:], data[27:47])
	copy(h.PeerID[:], data[47:67])

	return h, nil
}

// DialPeer connects to a peer and performs handshake
func DialPeer(addr string, infoHash, peerID [20]byte) (*Conn, error) {
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}

	c := &Conn{
		Conn:           conn,
		InfoHash:       infoHash,
		PeerID:         peerID,
		AmChoking:      true,
		AmInterested:   false,
		PeerChoking:    true,
		PeerInterested: false,
	}

	if err := c.Handshake(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("handshake: %w", err)
	}

	return c, nil
}

// Handshake performs the BitTorrent handshake
func (c *Conn) Handshake() error {
	return c.HandshakeWithExtensions(true)
}

// HandshakeWithExtensions performs the BitTorrent handshake with optional extension support
func (c *Conn) HandshakeWithExtensions(extensions bool) error {
	var protoBytes [19]byte
	copy(protoBytes[:], Protocol)

	reserved := [8]byte{0, 0, 0, 0, 0, 0, 0, 0}
	if extensions {
		reserved[5] = ExtensionBit
	}

	h := Handshake{
		Protocol: protoBytes,
		Reserved: reserved,
		InfoHash: c.InfoHash,
		PeerID:   c.PeerID,
	}

	if _, err := c.Write(h.Bytes()); err != nil {
		return err
	}

	reply := make([]byte, 68)
	if _, err := io.ReadFull(c, reply); err != nil {
		return err
	}

	remote, err := ParseHandshake(reply)
	if err != nil {
		return err
	}

	if string(remote.Protocol[:]) != Protocol {
		return fmt.Errorf("invalid protocol: %s", string(remote.Protocol[:]))
	}

	if remote.InfoHash != c.InfoHash {
		return fmt.Errorf("info hash mismatch")
	}

	return nil
}

// SupportsExtensions checks if the remote peer supports extension protocol
func (c *Conn) SupportsExtensions(remoteReserved [8]byte) bool {
	return (remoteReserved[5] & ExtensionBit) != 0
}

// SendExtendedMessage sends an extended message
func (c *Conn) SendExtendedMessage(extID byte, payload []byte) error {
	msg := &Message{
		Type:    MsgExtended,
		Payload: append([]byte{extID}, payload...),
	}
	return c.WriteMessage(msg)
}

// ReadExtendedMessage reads an extended message payload
func (c *Conn) ReadExtendedMessage() (byte, []byte, error) {
	msg, err := c.ReadMessage()
	if err != nil {
		return 0, nil, err
	}
	if msg == nil || msg.Type != MsgExtended {
		return 0, nil, fmt.Errorf("not an extended message")
	}
	if len(msg.Payload) == 0 {
		return 0, nil, fmt.Errorf("empty extended message")
	}
	return msg.Payload[0], msg.Payload[1:], nil
}

// ReadMessage reads a peer wire message
func (c *Conn) ReadMessage() (*Message, error) {
	// Read message length (4 bytes)
	var lengthBuf [4]byte
	if _, err := io.ReadFull(c, lengthBuf[:]); err != nil {
		return nil, err
	}
	length := binary.BigEndian.Uint32(lengthBuf[:])

	if length == 0 {
		// Keep-alive
		return &Message{}, nil
	}

	// Read message ID and payload
	msgLen := int(length) - 1 // subtract message ID byte
	if msgLen < 0 {
		return nil, fmt.Errorf("invalid message length: %d", length)
	}

	msg := &Message{
		Payload: make([]byte, msgLen),
	}

	if msgLen > 0 {
		if _, err := io.ReadFull(c, msg.Payload); err != nil {
			return nil, err
		}
	}

	msg.Type = msg.Payload[0]
	msg.Payload = msg.Payload[1:]

	return msg, nil
}

// WriteMessage writes a peer wire message
func (c *Conn) WriteMessage(msg *Message) error {
	payloadLen := len(msg.Payload) + 1 // +1 for message ID
	lengthBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lengthBuf, uint32(payloadLen))

	if _, err := c.Write(lengthBuf); err != nil {
		return err
	}

	if _, err := c.Write([]byte{msg.Type}); err != nil {
		return err
	}

	if len(msg.Payload) > 0 {
		if _, err := c.Write(msg.Payload); err != nil {
			return err
		}
	}

	return nil
}

// SendInterested sends an interested message
func (c *Conn) SendInterested() error {
	return c.WriteMessage(&Message{Type: MsgInterested})
}

// SendNotInterested sends a not interested message
func (c *Conn) SendNotInterested() error {
	return c.WriteMessage(&Message{Type: MsgNotInterested})
}

// SendChoke sends a choke message
func (c *Conn) SendChoke() error {
	return c.WriteMessage(&Message{Type: MsgChoke})
}

// SendUnchoke sends an unchoke message
func (c *Conn) SendUnchoke() error {
	return c.WriteMessage(&Message{Type: MsgUnchoke})
}

// SendRequest sends a piece request
func (c *Conn) SendRequest(index, begin, length uint32) error {
	payload := make([]byte, 12)
	binary.BigEndian.PutUint32(payload[0:4], index)
	binary.BigEndian.PutUint32(payload[4:8], begin)
	binary.BigEndian.PutUint32(payload[8:12], length)

	return c.WriteMessage(&Message{
		Type:    MsgRequest,
		Payload: payload,
	})
}

// SendCancel sends a cancel message
func (c *Conn) SendCancel(index, begin, length uint32) error {
	payload := make([]byte, 12)
	binary.BigEndian.PutUint32(payload[0:4], index)
	binary.BigEndian.PutUint32(payload[4:8], begin)
	binary.BigEndian.PutUint32(payload[8:12], length)

	return c.WriteMessage(&Message{
		Type:    MsgCancel,
		Payload: payload,
	})
}

// SendHave sends a have message
func (c *Conn) SendHave(pieceIndex uint32) error {
	payload := make([]byte, 4)
	binary.BigEndian.PutUint32(payload, pieceIndex)

	return c.WriteMessage(&Message{
		Type:    MsgHave,
		Payload: payload,
	})
}

// SendBitfield sends a bitfield message
func (c *Conn) SendBitfield(bitfield []byte) error {
	return c.WriteMessage(&Message{
		Type:    MsgBitfield,
		Payload: bitfield,
	})
}

// ParseHave parses a have message
func ParseHave(msg *Message) (uint32, error) {
	if len(msg.Payload) < 4 {
		return 0, fmt.Errorf("have message too short")
	}
	return binary.BigEndian.Uint32(msg.Payload), nil
}

// ParsePiece parses a piece message
func ParsePiece(msg *Message) (index, begin uint32, data []byte, err error) {
	if len(msg.Payload) < 8 {
		err = fmt.Errorf("piece message too short")
		return
	}
	index = binary.BigEndian.Uint32(msg.Payload[0:4])
	begin = binary.BigEndian.Uint32(msg.Payload[4:8])
	data = msg.Payload[8:]
	return
}

// ParseRequest parses a request message
func ParseRequest(msg *Message) (index, begin, length uint32, err error) {
	if len(msg.Payload) < 12 {
		err = fmt.Errorf("request message too short")
		return
	}
	index = binary.BigEndian.Uint32(msg.Payload[0:4])
	begin = binary.BigEndian.Uint32(msg.Payload[4:8])
	length = binary.BigEndian.Uint32(msg.Payload[8:12])
	return
}

// VerifyPiece verifies a piece against its hash
func VerifyPiece(data []byte, expectedHash [20]byte) bool {
	hash := sha1.Sum(data)
	return hash == expectedHash
}
