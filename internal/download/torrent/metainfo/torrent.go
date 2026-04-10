package metainfo

import (
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"

	"github.com/yay101/mediarr/internal/download/torrent/bencode"
)

type Metainfo struct {
	Announce     string
	AnnounceList [][]string
	CreationDate int64
	Comment      string
	CreatedBy    string
	Info         InfoDict
}

type InfoDict struct {
	PieceLength  int64
	Pieces       []byte     // raw bytes, 20 bytes per piece
	PiecesHashes [][20]byte // parsed pieces
	Private      int64
	Name         string
	Length       int64      // single file mode
	Files        []FileInfo // multi file mode

	// Computed
	TotalSize int64
}

type FileInfo struct {
	Length int64
	Path   []string
}

func (m *Metainfo) InfoHash() [20]byte {
	infoBytes, err := bencode.Encode(m.Info.toDict())
	if err != nil {
		return [20]byte{}
	}
	hash := sha1.Sum(infoBytes)
	return hash
}

func (m *Metainfo) InfoHashHex() string {
	return fmt.Sprintf("%x", m.InfoHash())
}

func (m *Metainfo) CalcTotalSize() {
	if m.Info.Length > 0 {
		m.Info.TotalSize = m.Info.Length
	} else {
		m.Info.TotalSize = 0
		for _, f := range m.Info.Files {
			m.Info.TotalSize += f.Length
		}
	}
}

func (m *Metainfo) ParsePieces() {
	numPieces := len(m.Info.Pieces) / 20
	m.Info.PiecesHashes = make([][20]byte, numPieces)
	for i := 0; i < numPieces; i++ {
		copy(m.Info.PiecesHashes[i][:], m.Info.Pieces[i*20:(i+1)*20])
	}
}

func (m *Metainfo) NumPieces() int {
	return len(m.Info.Pieces) / 20
}

func ParseMetainfo(data []byte) (*Metainfo, error) {
	val, err := bencode.Decode(data)
	if err != nil {
		return nil, fmt.Errorf("bencode decode: %w", err)
	}

	dict, ok := val.(bencode.Dict)
	if !ok {
		return nil, fmt.Errorf("expected dict at root")
	}

	m := &Metainfo{}

	if v, ok := dict["announce"]; ok {
		if s, ok := v.(bencode.String); ok {
			m.Announce = string(s)
		}
	}

	if v, ok := dict["announce-list"]; ok {
		if list, ok := v.(bencode.List); ok {
			for _, tier := range list {
				if tierList, ok := tier.(bencode.List); ok {
					var tierStrs []string
					for _, t := range tierList {
						if s, ok := t.(bencode.String); ok {
							tierStrs = append(tierStrs, string(s))
						}
					}
					if len(tierStrs) > 0 {
						m.AnnounceList = append(m.AnnounceList, tierStrs)
					}
				}
			}
		}
	}

	if v, ok := dict["creation date"]; ok {
		if i, ok := v.(bencode.Int); ok {
			m.CreationDate = int64(i)
		}
	}

	if v, ok := dict["comment"]; ok {
		if s, ok := v.(bencode.String); ok {
			m.Comment = string(s)
		}
	}

	if v, ok := dict["created by"]; ok {
		if s, ok := v.(bencode.String); ok {
			m.CreatedBy = string(s)
		}
	}

	if infoVal, ok := dict["info"]; ok {
		info, err := parseInfoDict(infoVal)
		if err != nil {
			return nil, fmt.Errorf("parse info: %w", err)
		}
		m.Info = *info
	} else {
		return nil, fmt.Errorf("missing info dict")
	}

	m.ParsePieces()
	m.CalcTotalSize()

	return m, nil
}

func parseInfoDict(val bencode.Value) (*InfoDict, error) {
	dict, ok := val.(bencode.Dict)
	if !ok {
		return nil, fmt.Errorf("expected dict for info")
	}

	info := &InfoDict{}

	if v, ok := dict["piece length"]; ok {
		if i, ok := v.(bencode.Int); ok {
			info.PieceLength = int64(i)
		}
	}

	if v, ok := dict["pieces"]; ok {
		if s, ok := v.(bencode.String); ok {
			info.Pieces = []byte(s)
		}
	}

	if v, ok := dict["private"]; ok {
		if i, ok := v.(bencode.Int); ok {
			info.Private = int64(i)
		}
	}

	if v, ok := dict["name"]; ok {
		if s, ok := v.(bencode.String); ok {
			info.Name = string(s)
		}
	}

	// Single file mode
	if v, ok := dict["length"]; ok {
		if i, ok := v.(bencode.Int); ok {
			info.Length = int64(i)
		}
	}

	// Multi file mode
	if v, ok := dict["files"]; ok {
		if list, ok := v.(bencode.List); ok {
			for _, fileVal := range list {
				if fileDict, ok := fileVal.(bencode.Dict); ok {
					file := FileInfo{}
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

	return info, nil
}

func (i *InfoDict) toDict() bencode.Dict {
	d := bencode.Dict{}

	d["piece length"] = bencode.Int(i.PieceLength)
	d["pieces"] = bencode.String(i.Pieces)
	d["name"] = bencode.String(i.Name)

	if i.Private > 0 {
		d["private"] = bencode.Int(i.Private)
	}

	if i.Length > 0 {
		d["length"] = bencode.Int(i.Length)
	} else if len(i.Files) > 0 {
		var filesList bencode.List
		for _, f := range i.Files {
			fileDict := bencode.Dict{
				"length": bencode.Int(f.Length),
				"path":   pathToList(f.Path),
			}
			filesList = append(filesList, fileDict)
		}
		d["files"] = filesList
	}

	return d
}

func pathToList(path []string) bencode.List {
	var list bencode.List
	for _, p := range path {
		list = append(list, bencode.String(p))
	}
	return list
}

// Magnet represents a parsed magnet URI
type Magnet struct {
	InfoHash    [20]byte
	InfoHashHex string
	Name        string
	Trackers    []string
}

func ParseMagnet(uri string) (*Magnet, error) {
	if !strings.HasPrefix(uri, "magnet:?") {
		return nil, fmt.Errorf("not a magnet URI")
	}

	m := &Magnet{}

	u, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}

	for key, values := range u.Query() {
		if len(values) == 0 {
			continue
		}
		value := values[0]

		switch key {
		case "xt": // exact topic (urn:btih:...)
			if strings.HasPrefix(value, "urn:btih:") {
				hashStr := strings.ToLower(strings.TrimPrefix(value, "urn:btih:"))
				// Could be 40 hex chars or 32 base32 chars
				if len(hashStr) == 40 {
					// Hex format
					hash, err := hexToBytes(hashStr)
					if err != nil {
						continue
					}
					copy(m.InfoHash[:], hash)
					m.InfoHashHex = hashStr
				} else if len(hashStr) == 32 {
					// Base32 format - decode
					hash, err := base32Decode(hashStr)
					if err != nil {
						continue
					}
					copy(m.InfoHash[:], hash)
					m.InfoHashHex = fmt.Sprintf("%x", hash)
				}
			}
		case "dn": // display name
			m.Name = value
		case "tr": // tracker
			m.Trackers = append(m.Trackers, value)
		}
	}

	if m.InfoHashHex == "" {
		return nil, fmt.Errorf("no info hash found in magnet URI")
	}

	return m, nil
}

func hexToBytes(s string) ([]byte, error) {
	if len(s)%2 != 0 {
		return nil, fmt.Errorf("odd length hex string")
	}
	result := make([]byte, len(s)/2)
	for i := 0; i < len(s); i += 2 {
		c1, c2 := s[i], s[i+1]
		b1, err1 := hexChar(c1)
		b2, err2 := hexChar(c2)
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("invalid hex char")
		}
		result[i/2] = (b1 << 4) | b2
	}
	return result, nil
}

func hexChar(c byte) (byte, error) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', nil
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, nil
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, nil
	default:
		return 0, fmt.Errorf("invalid hex char: %c", c)
	}
}

func base32Decode(s string) ([]byte, error) {
	// Simplified base32 decoding for BitTorrent info hashes
	// Uses the "Extended Hex Alphabet" from RFC 4648
	const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUV"
	s = strings.ToUpper(s)

	result := make([]byte, 20)
	var buffer, bits uint32

	for i, c := range s {
		idx := strings.IndexByte(alphabet, byte(c))
		if idx < 0 {
			return nil, fmt.Errorf("invalid base32 char: %c", c)
		}

		buffer = (buffer << 5) | uint32(idx)
		bits += 5

		if bits >= 8 {
			bits -= 8
			result[i*5/8] = byte(buffer >> bits)
			if bits >= 8 {
				result[i*5/8+1] = byte(buffer >> (bits - 8))
			}
			buffer &= (1 << bits) - 1
		}
	}

	return result[:20], nil
}

func (m *Magnet) String() string {
	uri := "magnet:?xt=urn:btih:" + m.InfoHashHex
	if m.Name != "" {
		uri += "&dn=" + url.QueryEscape(m.Name)
	}
	for _, tr := range m.Trackers {
		uri += "&tr=" + url.QueryEscape(tr)
	}
	return uri
}

// PeerID generates a -XX0000- style peer ID
func PeerID(clientID string) [20]byte {
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

	// Fill remaining with random or fixed bytes
	copy(id[9:], []byte(clientID))

	return id
}

// NewDefaultPeerID creates a standard BitTorrent peer ID
func NewDefaultPeerID() [20]byte {
	return PeerID("MEDIARR001")
}

func PeerIDToString(id [20]byte) string {
	return string(id[:])
}

func PeerIDToBytes(id [20]byte) []byte {
	return id[:]
}

// PackInfoHash packs a 20-byte info hash into a string
func PackInfoHash(hash [20]byte) string {
	return string(hash[:])
}

// UnpackInfoHash unpacks a 20-byte info hash from a string
func UnpackInfoHash(s string) ([20]byte, error) {
	if len(s) != 20 {
		return [20]byte{}, fmt.Errorf("expected 20 byte info hash, got %d", len(s))
	}
	var hash [20]byte
	copy(hash[:], s)
	return hash, nil
}

func PackPort(port int) []byte {
	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, uint16(port))
	return b
}
