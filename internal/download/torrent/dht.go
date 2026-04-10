package torrent

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

const (
	DHTPort = 6881

	NodeIDLength = 20
	MaxNodes     = 100
)

var (
	ErrNoPeers = errors.New("no peers found")
)

type Node struct {
	ID   [NodeIDLength]byte
	IP   net.IP
	Port int
}

func (n *Node) Addr() string {
	return fmt.Sprintf("%s:%d", n.IP, n.Port)
}

type KRPCMessage struct {
	Type string                 `bencode:"y"`
	Txn  string                 `bencode:"t"`
	Data map[string]interface{} `bencode:"a,omitempty"`
}

type QueryMessage struct {
	Type  string                 `bencode:"y"`
	Txn   string                 `bencode:"t"`
	Query map[string]interface{} `bencode:"q"`
	Args  map[string]interface{} `bencode:"a"`
}

type ResponseMessage struct {
	Type string                 `bencode:"y"`
	Txn  string                 `bencode:"t"`
	Resp map[string]interface{} `bencode:"r"`
}

type ErrorMessage struct {
	Type  string        `bencode:"y"`
	Txn   string        `bencode:"t"`
	Error []interface{} `bencode:"e"`
}

type DHTClient struct {
	nodeID  [NodeIDLength]byte
	conn    *net.UDPConn
	addr    *net.UDPAddr
	nodes   map[string]*Node
	peers   map[[20]byte][]PeerInfo
	mu      sync.RWMutex
	closeCh chan struct{}
	wg      sync.WaitGroup
}

type PeerInfo struct {
	IP   string
	Port int
}

func NewDHTClient(port int) (*DHTClient, error) {
	nodeID, err := GenerateNodeID()
	if err != nil {
		return nil, err
	}

	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, err
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, err
	}

	dht := &DHTClient{
		nodeID:  nodeID,
		conn:    conn,
		addr:    addr,
		nodes:   make(map[string]*Node),
		peers:   make(map[[20]byte][]PeerInfo),
		closeCh: make(chan struct{}),
	}

	return dht, nil
}

func GenerateNodeID() ([NodeIDLength]byte, error) {
	var id [NodeIDLength]byte
	_, err := rand.Read(id[:])
	if err != nil {
		return id, err
	}
	return id, nil
}

func (d *DHTClient) Start() {
	d.wg.Add(2)
	go d.readLoop()
	go d.refreshLoop()
}

func (d *DHTClient) Close() {
	close(d.closeCh)
	d.wg.Wait()
	d.conn.Close()
}

func (d *DHTClient) readLoop() {
	defer d.wg.Done()

	buf := make([]byte, 65536)
	for {
		select {
		case <-d.closeCh:
			return
		default:
		}

		d.conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		n, addr, err := d.conn.ReadFromUDP(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			continue
		}

		go d.handlePacket(buf[:n], addr)
	}
}

func (d *DHTClient) handlePacket(data []byte, addr *net.UDPAddr) {
	msg, err := DecodeKRPCMessage(data)
	if err != nil {
		return
	}

	switch msg.Type {
	case "q":
		d.handleQuery(msg, addr)
	case "r":
		d.handleResponse(msg, addr)
	case "e":
		d.handleError(msg, addr)
	}
}

func (d *DHTClient) handleQuery(msg *KRPCMessage, addr *net.UDPAddr) {
	query, ok := msg.Data["q"].(string)
	if !ok {
		return
	}

	switch query {
	case "ping":
		d.handlePing(msg, addr)
	case "find_node":
		d.handleFindNode(msg, addr)
	case "get_peers":
		d.handleGetPeers(msg, addr)
	case "announce_peer":
		d.handleAnnouncePeer(msg, addr)
	}
}

func (d *DHTClient) handlePing(msg *KRPCMessage, addr *net.UDPAddr) {
	resp := map[string]interface{}{
		"id": d.nodeID[:],
	}
	d.sendResponse(msg.Txn, resp, addr)
}

func (d *DHTClient) handleFindNode(msg *KRPCMessage, addr *net.UDPAddr) {
	target, ok := msg.Data["target"].([]byte)
	if !ok || len(target) != NodeIDLength {
		return
	}

	var targetHash [20]byte
	copy(targetHash[:], target)

	nodes := d.findClosestNodes(targetHash)
	nodeBytes := encodeNodes(nodes[:min(len(nodes), 8)])

	resp := map[string]interface{}{
		"id":    d.nodeID[:],
		"nodes": string(nodeBytes),
	}
	d.sendResponse(msg.Txn, resp, addr)
}

func (d *DHTClient) handleGetPeers(msg *KRPCMessage, addr *net.UDPAddr) {
	infoHash, ok := msg.Data["info_hash"].([]byte)
	if !ok || len(infoHash) != NodeIDLength {
		return
	}

	var hash [20]byte
	copy(hash[:], infoHash)

	var ih [20]byte
	copy(ih[:], infoHash)

	resp := map[string]interface{}{
		"id": d.nodeID[:],
	}

	d.mu.RLock()
	peerList, hasPeers := d.peers[ih]
	d.mu.RUnlock()

	if hasPeers && len(peerList) > 0 {
		token := fmt.Sprintf("%d", time.Now().Unix())
		resp["token"] = token
		resp["nodes"] = ""

		peerData := make([]interface{}, 0, len(peerList))
		for _, p := range peerList {
			peerData = append(peerData, map[string]interface{}{
				"ip":   p.IP,
				"port": p.Port,
			})
		}
		resp["peers"] = peerData
	} else {
		var infoHashBytes [20]byte
		copy(infoHashBytes[:], infoHash)
		nodes := d.findClosestNodes(infoHashBytes)
		nodeBytes := encodeNodes(nodes[:min(len(nodes), MaxNodes)])
		resp["nodes"] = string(nodeBytes)
	}

	d.sendResponse(msg.Txn, resp, addr)
}

func (d *DHTClient) handleAnnouncePeer(msg *KRPCMessage, addr *net.UDPAddr) {
	infoHash, ok := msg.Data["info_hash"].([]byte)
	if !ok || len(infoHash) != NodeIDLength {
		return
	}

	port, _ := msg.Data["port"].(int64)
	if port == 0 {
		port = int64(addr.Port)
	}

	var ih [20]byte
	copy(ih[:], infoHash)

	d.mu.Lock()
	if _, exists := d.peers[ih]; !exists {
		d.peers[ih] = []PeerInfo{}
	}
	d.mu.Unlock()

	fmt.Printf("DHT: peer announced for %x from %s:%d\n", infoHash, addr.IP, port)
}

func (d *DHTClient) handleResponse(msg *KRPCMessage, addr *net.UDPAddr) {
	if nodes, ok := msg.Data["nodes"].(string); ok && nodes != "" {
		d.addNodes(nodes)
	}

	if peerList, ok := msg.Data["peers"].([]interface{}); ok {
		if infoHashData, ok := msg.Data["info_hash"].([]byte); ok {
			var ih [20]byte
			copy(ih[:], infoHashData)

			d.mu.Lock()
			for _, p := range peerList {
				if peer, ok := p.(map[string]interface{}); ok {
					ip, _ := peer["ip"].(string)
					port, _ := peer["port"].(int64)
					if ip != "" && port > 0 {
						d.peers[ih] = append(d.peers[ih], PeerInfo{IP: ip, Port: int(port)})
					}
				}
			}
			d.mu.Unlock()
		}
	}
}

func (d *DHTClient) handleError(msg *KRPCMessage, addr *net.UDPAddr) {
}

func (d *DHTClient) sendResponse(txn string, data map[string]interface{}, addr *net.UDPAddr) {
	msg := map[string]interface{}{
		"y": "r",
		"t": txn,
		"r": data,
	}

	packet, _ := EncodeKRPCMessage(msg)
	d.conn.WriteToUDP(packet, addr)
}

func (d *DHTClient) sendQuery(query string, args map[string]interface{}, addr *net.UDPAddr) error {
	txn := make([]byte, 2)
	rand.Read(txn)

	msg := map[string]interface{}{
		"y": "q",
		"t": string(txn),
		"q": query,
		"a": args,
	}

	packet, err := EncodeKRPCMessage(msg)
	if err != nil {
		return err
	}

	_, err = d.conn.WriteToUDP(packet, addr)
	return err
}

func (d *DHTClient) refreshLoop() {
	defer d.wg.Done()

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	bootstrapNodes := []string{
		"router.bittorrent.com:6881",
		"dht.transmissionbt.com:6881",
		"router.utorrent.com:6881",
		"bttracker.debian.org:6881",
	}

	for _, nodeAddr := range bootstrapNodes {
		go d.bootstrapNode(nodeAddr)
	}

	for {
		select {
		case <-d.closeCh:
			return
		case <-ticker.C:
			d.refreshNodes()
		}
	}
}

func (d *DHTClient) bootstrapNode(addrStr string) {
	addr, err := net.ResolveUDPAddr("udp", addrStr)
	if err != nil {
		return
	}

	args := map[string]interface{}{
		"id":     d.nodeID[:],
		"target": d.nodeID[:],
	}

	d.sendQuery("find_node", args, addr)
}

func (d *DHTClient) refreshNodes() {
	d.mu.RLock()
	nodes := make([]*Node, 0, len(d.nodes))
	for _, n := range d.nodes {
		nodes = append(nodes, n)
	}
	d.mu.RUnlock()

	for _, node := range nodes {
		args := map[string]interface{}{
			"id":     d.nodeID[:],
			"target": d.nodeID[:],
		}
		d.sendQuery("find_node", args, &net.UDPAddr{IP: node.IP, Port: node.Port})
	}
}

func (d *DHTClient) addNodes(nodesData string) {
	if len(nodesData)%26 != 0 {
		return
	}

	for i := 0; i < len(nodesData); i += 26 {
		var node Node
		copy(node.ID[:], nodesData[i:i+20])
		node.IP = net.IP([]byte(nodesData[i+20 : i+24]))
		node.Port = int(binary.BigEndian.Uint16([]byte(nodesData[i+24 : i+26])))

		addr := node.Addr()
		d.mu.Lock()
		if _, exists := d.nodes[addr]; !exists && len(d.nodes) < MaxNodes {
			d.nodes[addr] = &node
		}
		d.mu.Unlock()
	}
}

func (d *DHTClient) findClosestNodes(target [20]byte) []*Node {
	d.mu.RLock()
	defer d.mu.RUnlock()

	type nodeDistance struct {
		node     *Node
		distance [20]byte
	}

	var distances []nodeDistance
	for _, n := range d.nodes {
		var dist [20]byte
		for i := 0; i < NodeIDLength; i++ {
			dist[i] = n.ID[i] ^ target[i]
		}
		distances = append(distances, nodeDistance{node: n, distance: dist})
	}

	for i := 0; i < len(distances); i++ {
		for j := i + 1; j < len(distances); j++ {
			if compareDistances(distances[i].distance, distances[j].distance) > 0 {
				distances[i], distances[j] = distances[j], distances[i]
			}
		}
	}

	result := make([]*Node, min(len(distances), MaxNodes))
	for i, d := range distances {
		if i >= MaxNodes {
			break
		}
		result[i] = d.node
	}

	return result
}

func compareDistances(a, b [20]byte) int {
	for i := 0; i < NodeIDLength; i++ {
		if a[i] != b[i] {
			return int(a[i]) - int(b[i])
		}
	}
	return 0
}

func encodeNodes(nodes []*Node) []byte {
	result := make([]byte, 0, len(nodes)*26)
	for _, n := range nodes {
		result = append(result, n.ID[:]...)
		result = append(result, n.IP.To4()...)
		port := make([]byte, 2)
		binary.BigEndian.PutUint16(port, uint16(n.Port))
		result = append(result, port...)
	}
	return result
}

func (d *DHTClient) GetPeers(infoHash [20]byte) []PeerInfo {
	d.mu.RLock()
	peers := d.peers[infoHash]
	d.mu.RUnlock()
	return peers
}

func (d *DHTClient) AnnouncePeer(infoHash [20]byte, port int) error {
	d.mu.RLock()
	nodes := make([]*Node, 0, len(d.nodes))
	for _, n := range d.nodes {
		nodes = append(nodes, n)
	}
	d.mu.RUnlock()

	for _, node := range nodes {
		args := map[string]interface{}{
			"id":        d.nodeID[:],
			"info_hash": infoHash[:],
			"port":      port,
			"token":     fmt.Sprintf("%d", time.Now().Unix()),
		}
		d.sendQuery("announce_peer", args, &net.UDPAddr{IP: node.IP, Port: node.Port})
	}

	return nil
}

func (d *DHTClient) SearchPeers(infoHash [20]byte, timeout time.Duration) ([]PeerInfo, error) {
	done := make(chan []PeerInfo, 1)

	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				peers := d.GetPeers(infoHash)
				if len(peers) > 0 {
					done <- peers
					return
				}

				d.mu.RLock()
				nodes := make([]*Node, 0, len(d.nodes))
				for _, n := range d.nodes {
					nodes = append(nodes, n)
				}
				d.mu.RUnlock()

				for _, node := range nodes {
					args := map[string]interface{}{
						"id":        d.nodeID[:],
						"info_hash": infoHash[:],
					}
					d.sendQuery("get_peers", args, &net.UDPAddr{IP: node.IP, Port: node.Port})
				}
			}
		}
	}()

	select {
	case peers := <-done:
		return peers, nil
	case <-time.After(timeout):
		return d.GetPeers(infoHash), ErrNoPeers
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

type KRPCBencode map[string]interface{}

func DecodeKRPCMessage(data []byte) (*KRPCMessage, error) {
	val, err := DecodeBencode(data)
	if err != nil {
		return nil, err
	}

	d, ok := val.(map[string]interface{})
	if !ok {
		return nil, errors.New("invalid KRPC message")
	}

	msg := &KRPCMessage{
		Data: d,
	}

	if y, ok := d["y"].(string); ok {
		msg.Type = y
	}
	if t, ok := d["t"].(string); ok {
		msg.Txn = t
	}

	return msg, nil
}

func EncodeKRPCMessage(msg map[string]interface{}) ([]byte, error) {
	return EncodeBencode(msg)
}

func DecodeBencode(data []byte) (interface{}, error) {
	return nil, errors.New("bencode decode not implemented in DHT - use bencode package")
}

func EncodeBencode(msg map[string]interface{}) ([]byte, error) {
	return nil, errors.New("bencode encode not implemented in DHT - use bencode package")
}
