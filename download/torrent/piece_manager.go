package torrent

import (
	"crypto/sha1"
	"fmt"
	"sync"

	"github.com/yay101/mediarr/download/torrent/metainfo"
)

const (
	BlockSize = 16384 // 16KB blocks
)

type PieceState byte

const (
	PieceNotDownloaded PieceState = iota
	PieceDownloading
	PieceVerified
	PieceFailed
)

const (
	PrioritySkip   = -1
	PriorityLow    = 0
	PriorityNormal = 1
	PriorityHigh   = 2
)

type Block struct {
	Piece  int
	Offset int
	Length int
	Data   []byte
	Done   bool
}

type Piece struct {
	Index     int
	Hash      [20]byte
	Length    int
	State     PieceState
	Blocks    []*Block
	BlocksGot int
	Data      []byte
	mu        sync.Mutex
}

func newPiece(index int, hash [20]byte, length int) *Piece {
	p := &Piece{
		Index:  index,
		Hash:   hash,
		Length: length,
		State:  PieceNotDownloaded,
	}

	numBlocks := (length + BlockSize - 1) / BlockSize
	p.Blocks = make([]*Block, numBlocks)

	for i := 0; i < numBlocks; i++ {
		offset := i * BlockSize
		blockLen := BlockSize
		if offset+blockLen > length {
			blockLen = length - offset
		}
		p.Blocks[i] = &Block{
			Piece:  index,
			Offset: offset,
			Length: blockLen,
		}
	}

	return p
}

type PieceManager struct {
	Pieces      []*Piece
	NumPieces   int
	TotalSize   int64
	mu          sync.RWMutex
	pieceLength int64

	filePriorities  []int
	pieceToFiles    [][]int
	downloadedFiles intSet

	peerPieces   map[string][]bool
	pieceCounts  []int
	numVerifying int
}

type intSet map[int]bool

func NewPieceManager(info *metainfo.InfoDict) *PieceManager {
	numPieces := len(info.PiecesHashes)
	pm := &PieceManager{
		Pieces:          make([]*Piece, numPieces),
		NumPieces:       numPieces,
		pieceLength:     info.PieceLength,
		peerPieces:      make(map[string][]bool),
		pieceCounts:     make([]int, numPieces),
		downloadedFiles: make(intSet),
	}

	numFiles := 0
	if info.Length > 0 {
		numFiles = 1
	} else {
		numFiles = len(info.Files)
	}
	pm.filePriorities = make([]int, numFiles)
	for i := range pm.filePriorities {
		pm.filePriorities[i] = PriorityNormal
	}

	pm.pieceToFiles = pm.buildPieceToFileMap(info)

	for i := 0; i < numPieces; i++ {
		var pieceLen int
		if i == numPieces-1 {
			remainder := int(info.TotalSize) % int(info.PieceLength)
			if remainder > 0 {
				pieceLen = remainder
			} else {
				pieceLen = int(info.PieceLength)
			}
		} else {
			pieceLen = int(info.PieceLength)
		}
		pm.Pieces[i] = newPiece(i, info.PiecesHashes[i], pieceLen)
	}

	return pm
}

func (pm *PieceManager) buildPieceToFileMap(info *metainfo.InfoDict) [][]int {
	mapping := make([][]int, pm.NumPieces)

	if info.Length > 0 {
		for i := 0; i < pm.NumPieces; i++ {
			mapping[i] = []int{0}
		}
		return mapping
	}

	pieceLen := info.PieceLength
	offset := int64(0)

	for fileIdx, file := range info.Files {
		endOffset := offset + file.Length

		firstPiece := int(offset / pieceLen)
		lastPiece := int((endOffset - 1) / pieceLen)

		for p := firstPiece; p <= lastPiece && p < pm.NumPieces; p++ {
			mapping[p] = append(mapping[p], fileIdx)
		}

		offset = endOffset
	}

	return mapping
}

func (pm *PieceManager) SetFilePriority(fileIdx int, priority int) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if fileIdx < 0 || fileIdx >= len(pm.filePriorities) {
		return
	}

	pm.filePriorities[fileIdx] = priority
}

func (pm *PieceManager) GetFilePriority(fileIdx int) int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if fileIdx < 0 || fileIdx >= len(pm.filePriorities) {
		return PrioritySkip
	}

	return pm.filePriorities[fileIdx]
}

func (pm *PieceManager) IsFileWanted(fileIdx int) bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if fileIdx < 0 || fileIdx >= len(pm.filePriorities) {
		return false
	}

	return pm.filePriorities[fileIdx] != PrioritySkip
}

func (pm *PieceManager) pieceIsWanted(pieceIdx int) bool {
	if pieceIdx < 0 || pieceIdx >= len(pm.pieceToFiles) {
		return false
	}

	for _, fileIdx := range pm.pieceToFiles[pieceIdx] {
		if pm.filePriorities[fileIdx] != PrioritySkip {
			return true
		}
	}

	return false
}

func (pm *PieceManager) SetPeerBitfield(peerID string, bitfield []byte) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pieces := make([]bool, pm.NumPieces)
	for i := 0; i < pm.NumPieces && i < len(bitfield)*8; i++ {
		byteIdx := i / 8
		bitIdx := 7 - (i % 8)
		if byteIdx < len(bitfield) {
			pieces[i] = (bitfield[byteIdx] & (1 << bitIdx)) != 0
		}
	}

	oldPieces, exists := pm.peerPieces[peerID]
	if exists {
		for i := 0; i < pm.NumPieces && i < len(oldPieces); i++ {
			if oldPieces[i] && !pieces[i] {
				pm.pieceCounts[i]--
			}
		}
	}

	for i := 0; i < pm.NumPieces && i < len(pieces); i++ {
		if pieces[i] {
			if !exists || !oldPieces[i] {
				pm.pieceCounts[i]++
			}
		}
	}

	pm.peerPieces[peerID] = pieces
}

func (pm *PieceManager) PeerHave(peerID string, pieceIndex int) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pieces, exists := pm.peerPieces[peerID]
	if !exists {
		pieces = make([]bool, pm.NumPieces)
		pm.peerPieces[peerID] = pieces
	}

	if pieceIndex < pm.NumPieces && !pieces[pieceIndex] {
		pieces[pieceIndex] = true
		pm.pieceCounts[pieceIndex]++
	}
}

func (pm *PieceManager) RemovePeer(peerID string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pieces, exists := pm.peerPieces[peerID]; exists {
		for i := 0; i < pm.NumPieces && i < len(pieces); i++ {
			if pieces[i] {
				pm.pieceCounts[i]--
				if pm.pieceCounts[i] < 0 {
					pm.pieceCounts[i] = 0
				}
			}
		}
		delete(pm.peerPieces, peerID)
	}
}

func (pm *PieceManager) RarestPiece(peerID string) (int, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	pieces, hasPeer := pm.peerPieces[peerID]

	minCount := -1
	var candidates []int

	for i := 0; i < pm.NumPieces; i++ {
		pm.mu.RLock()
		state := pm.Pieces[i].State
		pm.mu.RUnlock()

		if state != PieceNotDownloaded && state != PieceFailed {
			continue
		}

		if !pm.pieceIsWanted(i) {
			continue
		}

		if hasPeer && !pieces[i] {
			continue
		}

		count := pm.pieceCounts[i]
		if minCount < 0 || count < minCount {
			minCount = count
			candidates = []int{i}
		} else if count == minCount {
			candidates = append(candidates, i)
		}
	}

	if len(candidates) == 0 {
		return -1, fmt.Errorf("no available pieces")
	}

	return candidates[0], nil
}

func (pm *PieceManager) NextRequest(pieceIndex int) *Block {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pieceIndex < 0 || pieceIndex >= pm.NumPieces {
		return nil
	}

	piece := pm.Pieces[pieceIndex]
	for _, block := range piece.Blocks {
		if !block.Done {
			return block
		}
	}
	return nil
}

func (pm *PieceManager) StoreBlock(pieceIndex int, offset int, data []byte) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pieceIndex < 0 || pieceIndex >= pm.NumPieces {
		return fmt.Errorf("invalid piece index: %d", pieceIndex)
	}

	piece := pm.Pieces[pieceIndex]
	if piece.State == PieceDownloading {
		// continue
	} else if piece.State == PieceNotDownloaded || piece.State == PieceFailed {
		piece.State = PieceDownloading
		if piece.Data == nil {
			piece.Data = make([]byte, 0, piece.Length)
		}
	}

	blockIdx := offset / BlockSize
	if blockIdx >= len(piece.Blocks) {
		return fmt.Errorf("invalid block index: %d", blockIdx)
	}

	block := piece.Blocks[blockIdx]
	if offset != block.Offset {
		return fmt.Errorf("offset mismatch: expected %d, got %d", block.Offset, offset)
	}

	if len(data) != block.Length {
		return fmt.Errorf("block length mismatch: expected %d, got %d", block.Length, len(data))
	}

	piece.Data = append(piece.Data, data...)
	block.Done = true
	piece.BlocksGot++

	return nil
}

func (pm *PieceManager) VerifyPiece(pieceIndex int) (bool, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pieceIndex < 0 || pieceIndex >= pm.NumPieces {
		return false, fmt.Errorf("invalid piece index: %d", pieceIndex)
	}

	piece := pm.Pieces[pieceIndex]

	if len(piece.Data) != piece.Length {
		return false, fmt.Errorf("piece data length mismatch: expected %d, got %d", piece.Length, len(piece.Data))
	}

	hash := sha1.Sum(piece.Data)
	if hash != piece.Hash {
		piece.State = PieceFailed
		piece.Data = nil
		piece.BlocksGot = 0
		for _, b := range piece.Blocks {
			b.Done = false
		}
		return false, nil
	}

	piece.State = PieceVerified
	return true, nil
}

func (pm *PieceManager) GetPieceState(pieceIndex int) PieceState {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if pieceIndex < 0 || pieceIndex >= pm.NumPieces {
		return PieceNotDownloaded
	}
	return pm.Pieces[pieceIndex].State
}

func (pm *PieceManager) IsComplete() bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	for _, piece := range pm.Pieces {
		if piece.State != PieceVerified {
			return false
		}
	}
	return true
}

func (pm *PieceManager) CompletedCount() int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	count := 0
	for _, piece := range pm.Pieces {
		if piece.State == PieceVerified {
			count++
		}
	}
	return count
}

func (pm *PieceManager) Progress() float32 {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if pm.NumPieces == 0 {
		return 0
	}
	return float32(pm.CompletedCount()) / float32(pm.NumPieces)
}

func (pm *PieceManager) GetPieceData(pieceIndex int) ([]byte, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if pieceIndex < 0 || pieceIndex >= pm.NumPieces {
		return nil, fmt.Errorf("invalid piece index: %d", pieceIndex)
	}

	piece := pm.Pieces[pieceIndex]
	if piece.State != PieceVerified {
		return nil, fmt.Errorf("piece not verified")
	}

	data := make([]byte, len(piece.Data))
	copy(data, piece.Data)
	return data, nil
}

func (pm *PieceManager) ResetPiece(pieceIndex int) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pieceIndex < 0 || pieceIndex >= pm.NumPieces {
		return
	}

	piece := pm.Pieces[pieceIndex]
	piece.State = PieceNotDownloaded
	piece.Data = nil
	piece.BlocksGot = 0
	for _, b := range piece.Blocks {
		b.Done = false
	}
}
