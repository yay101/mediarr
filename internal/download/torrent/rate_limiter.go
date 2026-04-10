package torrent

import (
	"sync"
	"time"
)

type RateLimiter struct {
	downloadLimit int
	uploadLimit   int

	mu           sync.Mutex
	downloadRate float64
	uploadRate   float64

	bytesDownloaded int64
	bytesUploaded   int64

	lastCheck time.Time
	window    []byteWindow
}

type byteWindow struct {
	timestamp time.Time
	bytes     int64
}

func NewRateLimiter() *RateLimiter {
	rl := &RateLimiter{
		downloadLimit: 0,
		uploadLimit:   0,
		window:        make([]byteWindow, 0, 60),
		lastCheck:     time.Now(),
	}
	return rl
}

func (rl *RateLimiter) SetDownloadLimit(bytesPerSecond int) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.downloadLimit = bytesPerSecond
}

func (rl *RateLimiter) SetUploadLimit(bytesPerSecond int) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.uploadLimit = bytesPerSecond
}

func (rl *RateLimiter) GetDownloadLimit() int {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	return rl.downloadLimit
}

func (rl *RateLimiter) GetUploadLimit() int {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	return rl.uploadLimit
}

func (rl *RateLimiter) RecordDownload(bytes int64) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.bytesDownloaded += bytes
	rl.addToWindow(&rl.window, bytes)
}

func (rl *RateLimiter) RecordUpload(bytes int64) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.bytesUploaded += bytes
	rl.addToWindow(&rl.window, bytes)
}

func (rl *RateLimiter) addToWindow(window *[]byteWindow, bytes int64) {
	now := time.Now()
	*window = append(*window, byteWindow{timestamp: now, bytes: bytes})
	rl.cleanWindow(window, now)
}

func (rl *RateLimiter) cleanWindow(window *[]byteWindow, now time.Time) {
	cutoff := now.Add(-time.Second)
	filtered := make([]byteWindow, 0, len(*window))
	for _, w := range *window {
		if w.timestamp.After(cutoff) {
			filtered = append(filtered, w)
		}
	}
	*window = filtered
}

func (rl *RateLimiter) GetCurrentDownloadRate() float64 {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	rl.cleanWindow(&rl.window, now)

	var total int64
	for _, w := range rl.window {
		total += w.bytes
	}

	return float64(total)
}

func (rl *RateLimiter) GetCurrentUploadRate() float64 {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	return float64(rl.bytesUploaded)
}

func (rl *RateLimiter) GetTotalDownloaded() int64 {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	return rl.bytesDownloaded
}

func (rl *RateLimiter) GetTotalUploaded() int64 {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	return rl.bytesUploaded
}

func (rl *RateLimiter) CanDownload(bytes int) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if rl.downloadLimit <= 0 {
		return true
	}

	now := time.Now()
	rl.cleanWindow(&rl.window, now)

	var windowBytes int64
	for _, w := range rl.window {
		windowBytes += w.bytes
	}

	return windowBytes+int64(bytes) <= int64(rl.downloadLimit)
}

func (rl *RateLimiter) CanUpload(bytes int) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if rl.uploadLimit <= 0 {
		return true
	}

	return true
}

func (rl *RateLimiter) WaitForDownload(bytes int) {
	rl.mu.Lock()

	if rl.downloadLimit <= 0 {
		rl.mu.Unlock()
		return
	}

	for !rl.CanDownload(bytes) {
		rl.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
		rl.mu.Lock()
	}

	rl.mu.Unlock()
}

func (rl *RateLimiter) WaitForUpload(bytes int) {
	rl.mu.Lock()

	if rl.uploadLimit <= 0 {
		rl.mu.Unlock()
		return
	}

	rl.mu.Unlock()
}

type MultiLimiter struct {
	mu          sync.RWMutex
	connections map[string]*RateLimiter
	globalDL    int
	globalUL    int
}

func NewMultiLimiter() *MultiLimiter {
	return &MultiLimiter{
		connections: make(map[string]*RateLimiter),
	}
}

func (ml *MultiLimiter) SetGlobalLimits(download, upload int) {
	ml.mu.Lock()
	defer ml.mu.Unlock()
	ml.globalDL = download
	ml.globalUL = upload
}

func (ml *MultiLimiter) GetConnection(id string) *RateLimiter {
	ml.mu.RLock()
	rl, exists := ml.connections[id]
	ml.mu.RUnlock()

	if exists {
		return rl
	}

	ml.mu.Lock()
	defer ml.mu.Unlock()

	if rl, exists = ml.connections[id]; exists {
		return rl
	}

	rl = &RateLimiter{
		downloadLimit: ml.globalDL,
		uploadLimit:   ml.globalUL,
		window:        make([]byteWindow, 0, 60),
		lastCheck:     time.Now(),
	}
	ml.connections[id] = rl
	return rl
}

func (ml *MultiLimiter) RemoveConnection(id string) {
	ml.mu.Lock()
	defer ml.mu.Unlock()
	delete(ml.connections, id)
}

func (ml *MultiLimiter) GetGlobalDownloadLimit() int {
	ml.mu.RLock()
	defer ml.mu.RUnlock()
	return ml.globalDL
}

func (ml *MultiLimiter) GetGlobalUploadLimit() int {
	ml.mu.RLock()
	defer ml.mu.RUnlock()
	return ml.globalUL
}

func (ml *MultiLimiter) GetTotalDownloaded() int64 {
	ml.mu.RLock()
	defer ml.mu.RUnlock()

	var total int64
	for _, rl := range ml.connections {
		total += rl.GetTotalDownloaded()
	}
	return total
}

func (ml *MultiLimiter) GetTotalUploaded() int64 {
	ml.mu.RLock()
	defer ml.mu.RUnlock()

	var total int64
	for _, rl := range ml.connections {
		total += rl.GetTotalUploaded()
	}
	return total
}
