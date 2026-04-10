package tracker

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/yay101/mediarr/download/torrent/bencode"
)

type Client struct {
	peerID [20]byte
	port   int
	client *http.Client
}

type AnnounceRequest struct {
	InfoHash   [20]byte
	PeerID     [20]byte
	Port       int
	Uploaded   int64
	Downloaded int64
	Left       int64
	Compact    bool
	Event      string
}

type AnnounceResponse struct {
	Interval   int64
	Complete   int32
	Incomplete int32
	Peers      []Peer
}

type Peer struct {
	IP   string
	Port int
}

func NewClient(peerID [20]byte, port int) *Client {
	return &Client{
		peerID: peerID,
		port:   port,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (c *Client) Announce(announceURL string, req AnnounceRequest) (*AnnounceResponse, error) {
	u, err := url.Parse(announceURL)
	if err != nil {
		return nil, fmt.Errorf("parse announce URL: %w", err)
	}

	switch u.Scheme {
	case "http", "https":
		return c.httpAnnounce(announceURL, req)
	case "udp":
		return c.udpAnnounce(u.Host, req)
	default:
		return nil, fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}
}

func (c *Client) httpAnnounce(announceURL string, req AnnounceRequest) (*AnnounceResponse, error) {
	u, err := url.Parse(announceURL)
	if err != nil {
		return nil, fmt.Errorf("parse announce URL: %w", err)
	}

	q := u.Query()
	q.Set("info_hash", string(req.InfoHash[:]))
	q.Set("peer_id", string(req.PeerID[:]))
	q.Set("port", fmt.Sprintf("%d", req.Port))
	q.Set("uploaded", fmt.Sprintf("%d", req.Uploaded))
	q.Set("downloaded", fmt.Sprintf("%d", req.Downloaded))
	q.Set("left", fmt.Sprintf("%d", req.Left))
	q.Set("compact", "1")

	if req.Event != "" {
		q.Set("event", req.Event)
	}

	var tid [4]byte
	rand.Read(tid[:])
	q.Set("key", fmt.Sprintf("%x", tid))

	u.RawQuery = q.Encode()

	resp, err := c.client.Get(u.String())
	if err != nil {
		return nil, fmt.Errorf("HTTP request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tracker returned status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	return parseAnnounceResponse(data)
}

func (c *Client) udpAnnounce(host string, req AnnounceRequest) (*AnnounceResponse, error) {
	addr, err := net.ResolveUDPAddr("udp", host)
	if err != nil {
		return nil, fmt.Errorf("resolve UDP address: %w", err)
	}

	conn, err := net.ListenUDP("udp", nil)
	if err != nil {
		return nil, fmt.Errorf("listen UDP: %w", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(30 * time.Second))

	connectionID, err := c.udpConnect(conn, addr)
	if err != nil {
		return nil, fmt.Errorf("UDP connect: %w", err)
	}

	resp, err := c.udpAnnounceReq(conn, addr, connectionID, req)
	if err != nil {
		return nil, fmt.Errorf("UDP announce: %w", err)
	}

	return resp, nil
}

func (c *Client) udpConnect(conn *net.UDPConn, addr *net.UDPAddr) (uint64, error) {
	transID := generateTransID()
	connect := udpConnectPacket{
		ProtocolID:    0x41727101980,
		Action:        0,
		TransactionID: transID,
	}

	buf := make([]byte, 16)
	binary.LittleEndian.PutUint64(buf[0:8], connect.ProtocolID)
	binary.BigEndian.PutUint32(buf[8:12], connect.Action)
	binary.BigEndian.PutUint32(buf[12:16], connect.TransactionID)

	if _, err := conn.WriteToUDP(buf, addr); err != nil {
		return 0, err
	}

	conn.SetReadDeadline(time.Now().Add(15 * time.Second))

	respBuf := make([]byte, 16)
	n, _, err := conn.ReadFromUDP(respBuf)
	if err != nil {
		return 0, err
	}

	if n < 16 {
		return 0, fmt.Errorf("short response: %d bytes", n)
	}

	action := binary.BigEndian.Uint32(respBuf[0:4])
	respTransID := binary.BigEndian.Uint32(respBuf[4:8])
	connectionID := binary.LittleEndian.Uint64(respBuf[8:16])

	if action != 0 {
		return 0, fmt.Errorf("connect action mismatch: %d", action)
	}
	if respTransID != transID {
		return 0, fmt.Errorf("transaction ID mismatch")
	}

	return connectionID, nil
}

func (c *Client) udpAnnounceReq(conn *net.UDPConn, addr *net.UDPAddr, connectionID uint64, req AnnounceRequest) (*AnnounceResponse, error) {
	transID := generateTransID()

	buf := make([]byte, 98)
	binary.LittleEndian.PutUint64(buf[0:8], connectionID)
	binary.BigEndian.PutUint32(buf[8:12], 1)
	binary.BigEndian.PutUint32(buf[12:16], transID)
	copy(buf[16:36], req.InfoHash[:])
	copy(buf[36:56], req.PeerID[:])
	binary.BigEndian.PutUint64(buf[56:64], uint64(req.Downloaded))
	binary.BigEndian.PutUint64(buf[64:72], uint64(req.Left))
	binary.BigEndian.PutUint64(buf[72:80], uint64(req.Uploaded))
	binary.BigEndian.PutUint32(buf[80:84], udpEventFromString(req.Event))
	binary.BigEndian.PutUint32(buf[84:88], 0)
	binary.BigEndian.PutUint32(buf[88:92], 0)
	binary.BigEndian.PutUint32(buf[92:96], 0xffffffff)
	binary.BigEndian.PutUint16(buf[96:98], uint16(req.Port))

	if _, err := conn.WriteToUDP(buf, addr); err != nil {
		return nil, err
	}

	conn.SetReadDeadline(time.Now().Add(15 * time.Second))

	respBuf := make([]byte, 1024)
	n, _, err := conn.ReadFromUDP(respBuf)
	if err != nil {
		return nil, err
	}

	if n < 20 {
		return nil, fmt.Errorf("short response: %d bytes", n)
	}

	action := binary.BigEndian.Uint32(respBuf[0:4])

	if action != 1 {
		return nil, fmt.Errorf("announce action mismatch: %d", action)
	}

	interval := binary.BigEndian.Uint32(respBuf[8:12])
	complete := binary.BigEndian.Uint32(respBuf[12:16])
	incomplete := binary.BigEndian.Uint32(respBuf[16:20])

	result := &AnnounceResponse{
		Interval:   int64(interval),
		Complete:   int32(complete),
		Incomplete: int32(incomplete),
		Peers:      make([]Peer, 0),
	}

	for i := 20; i+6 <= n; i += 6 {
		peer := Peer{
			IP:   fmt.Sprintf("%d.%d.%d.%d", respBuf[i], respBuf[i+1], respBuf[i+2], respBuf[i+3]),
			Port: int(binary.BigEndian.Uint16(respBuf[i+4 : i+6])),
		}
		result.Peers = append(result.Peers, peer)
	}

	return result, nil
}

func generateTransID() uint32 {
	var id uint32
	binary.Read(rand.Reader, binary.BigEndian, &id)
	return id
}

func generateTransIDBytes() []byte {
	b := make([]byte, 4)
	rand.Read(b)
	return b
}

func udpEventFromString(event string) uint32 {
	switch event {
	case "started":
		return 2
	case "stopped":
		return 3
	case "completed":
		return 1
	default:
		return 0
	}
}

type udpConnectPacket struct {
	ProtocolID    uint64
	Action        uint32
	TransactionID uint32
}

type udpConnectResp struct {
	Action        uint32
	TransactionID uint32
	ConnectionID  uint64
}

type udpAnnouncePacket struct {
	ConnectionID  uint64
	Action        uint32
	TransactionID uint32
	InfoHash      [20]byte
	PeerID        [20]byte
	Downloaded    uint64
	Left          uint64
	Uploaded      uint64
	Event         uint32
	Key           uint32
	IP            uint32
	NumWant       int32
	Port          uint16
}

type udpAnnounceResp struct {
	Action        uint32
	TransactionID uint32
	Interval      uint32
	Complete      uint32
	Incomplete    uint32
	NumPeers      uint32
	Peers         []byte
}

func parseAnnounceResponse(data []byte) (*AnnounceResponse, error) {
	val, err := bencode.Decode(data)
	if err != nil {
		return nil, fmt.Errorf("bencode decode: %w", err)
	}

	dict, ok := val.(bencode.Dict)
	if !ok {
		return nil, fmt.Errorf("expected dict response")
	}

	resp := &AnnounceResponse{}

	if v, ok := dict["interval"]; ok {
		if i, ok := v.(bencode.Int); ok {
			resp.Interval = int64(i)
		}
	}

	if v, ok := dict["complete"]; ok {
		if i, ok := v.(bencode.Int); ok {
			resp.Complete = int32(i)
		}
	}

	if v, ok := dict["incomplete"]; ok {
		if i, ok := v.(bencode.Int); ok {
			resp.Incomplete = int32(i)
		}
	}

	if v, ok := dict["peers"]; ok {
		resp.Peers = parsePeers(v)
	}

	return resp, nil
}

func parsePeers(val bencode.Value) []Peer {
	switch peers := val.(type) {
	case bencode.List:
		var result []Peer
		for _, p := range peers {
			if pdict, ok := p.(bencode.Dict); ok {
				peer := Peer{}
				if ip, ok := pdict["ip"].(bencode.String); ok {
					peer.IP = string(ip)
				}
				if port, ok := pdict["port"].(bencode.Int); ok {
					peer.Port = int(port)
				}
				result = append(result, peer)
			}
		}
		return result

	case bencode.String:
		data := []byte(peers)
		var result []Peer
		for i := 0; i+6 <= len(data); i += 6 {
			peer := Peer{
				IP:   fmt.Sprintf("%d.%d.%d.%d", data[i], data[i+1], data[i+2], data[i+3]),
				Port: int(binary.BigEndian.Uint16(data[i+4 : i+6])),
			}
			result = append(result, peer)
		}
		return result

	default:
		return nil
	}
}

func BuildScrapeRequest(announceURL string) string {
	u, err := url.Parse(announceURL)
	if err != nil {
		return ""
	}

	u.Path = "/scrape"
	return u.String()
}

func BuildTrackerURL(announceURL string, req AnnounceRequest) (string, error) {
	u, err := url.Parse(announceURL)
	if err != nil {
		return "", fmt.Errorf("parse announce URL: %w", err)
	}

	q := u.Query()
	q.Set("info_hash", string(req.InfoHash[:]))
	q.Set("peer_id", string(req.PeerID[:]))
	q.Set("port", fmt.Sprintf("%d", req.Port))
	q.Set("uploaded", fmt.Sprintf("%d", req.Uploaded))
	q.Set("downloaded", fmt.Sprintf("%d", req.Downloaded))
	q.Set("left", fmt.Sprintf("%d", req.Left))
	q.Set("compact", "1")

	if req.Event != "" {
		q.Set("event", req.Event)
	}

	var tid [4]byte
	rand.Read(tid[:])
	q.Set("key", fmt.Sprintf("%x", tid))

	u.RawQuery = q.Encode()
	return u.String(), nil
}
