package tracker

import (
	"fmt"
	"sync"
	"time"
)

// StatsManager tracks upload/download statistics and seeding time
// for torrents across multiple trackers. In-memory only; persist to DB
// periodically or on shutdown for durability.
type StatsManager struct {
	cache map[string]*TorrentStats // Key: infoHash
	mu    sync.RWMutex
}

// TorrentStats holds cumulative statistics for a single torrent on a tracker.
// All values are cumulative totals unless otherwise noted.
type TorrentStats struct {
	InfoHash    string        // Torrent infohash
	TrackerID   uint32        // Which tracker this stats entry belongs to
	Uploaded    int64         // Total bytes uploaded (cumulative)
	Downloaded  int64         // Total bytes downloaded (cumulative)
	SeedTime    time.Duration // Total time spent seeding (excluding current session)
	SeedStart   time.Time     // When current seeding session started (zero if not seeding)
	TimesWarned int32         // Number of warnings received from tracker
	IsSeeding   bool          // Whether torrent is currently being seeded
	Ratio       float64       // Current ratio (Uploaded / Downloaded)
	LastUpdate  time.Time     // When stats were last modified
}

// NewStatsManager creates an empty stats cache.
func NewStatsManager() *StatsManager {
	return &StatsManager{
		cache: make(map[string]*TorrentStats),
	}
}

// GetStats retrieves stats for a torrent+tracker pair, creating an entry if needed.
// Returns a copy to prevent external mutation of cached data.
func (sm *StatsManager) GetStats(infoHash string, trackerID uint32) (*TorrentStats, error) {
	sm.mu.RLock()
	stats, ok := sm.cache[infoHash]
	sm.mu.RUnlock()

	if ok && stats.TrackerID == trackerID {
		return sm.copyStats(stats), nil
	}

	// Create new entry with defaults
	stats = &TorrentStats{
		InfoHash:    infoHash,
		TrackerID:   trackerID,
		SeedTime:    0,
		IsSeeding:   false,
		TimesWarned: 0,
		LastUpdate:  time.Now(),
	}

	sm.mu.Lock()
	sm.cache[infoHash] = stats
	sm.mu.Unlock()

	return sm.copyStats(stats), nil
}

// copyStats creates a shallow copy of stats to prevent external mutation.
func (sm *StatsManager) copyStats(s *TorrentStats) *TorrentStats {
	cp := *s
	return &cp
}

// UpdateStats increments upload/download counters and recalculates ratio.
// Use this during periodic announces to track ratio progress.
func (sm *StatsManager) UpdateStats(infoHash string, trackerID uint32, uploaded, downloaded int64) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	stats, ok := sm.cache[infoHash]
	if !ok || stats.TrackerID != trackerID {
		stats = &TorrentStats{
			InfoHash:    infoHash,
			TrackerID:   trackerID,
			SeedTime:    0,
			IsSeeding:   false,
			TimesWarned: 0,
		}
	}

	stats.Uploaded += uploaded
	stats.Downloaded += downloaded
	stats.LastUpdate = time.Now()

	if stats.Downloaded > 0 {
		stats.Ratio = float64(stats.Uploaded) / float64(stats.Downloaded)
	}

	sm.cache[infoHash] = stats

	return nil
}

// SetStats replaces the absolute values for upload/download counters.
func (sm *StatsManager) SetStats(infoHash string, trackerID uint32, uploaded, downloaded int64) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	stats := &TorrentStats{
		InfoHash:    infoHash,
		TrackerID:   trackerID,
		Uploaded:    uploaded,
		Downloaded:  downloaded,
		SeedTime:    0,
		IsSeeding:   false,
		TimesWarned: 0,
		LastUpdate:  time.Now(),
	}

	if stats.Downloaded > 0 {
		stats.Ratio = float64(stats.Uploaded) / float64(stats.Downloaded)
	}

	sm.cache[infoHash] = stats

	return nil
}

// StartSeeding marks the start of a seeding session.
// Call when torrent download completes and seeding begins.
func (sm *StatsManager) StartSeeding(infoHash string, trackerID uint32) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	stats, ok := sm.cache[infoHash]
	if !ok || stats.TrackerID != trackerID {
		stats = &TorrentStats{
			InfoHash:    infoHash,
			TrackerID:   trackerID,
			SeedTime:    0,
			TimesWarned: 0,
		}
	}

	stats.IsSeeding = true
	stats.SeedStart = time.Now()
	sm.cache[infoHash] = stats

	return nil
}

// StopSeeding ends the current seeding session and accumulates seed time.
// Call when torrent is removed or seeding is stopped.
func (sm *StatsManager) StopSeeding(infoHash string, trackerID uint32) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	stats, ok := sm.cache[infoHash]
	if !ok || stats.TrackerID != trackerID {
		return nil
	}

	// Add current session duration to total seed time
	if !stats.SeedStart.IsZero() {
		stats.SeedTime += time.Since(stats.SeedStart)
		stats.SeedStart = time.Time{}
	}
	stats.IsSeeding = false
	sm.cache[infoHash] = stats

	return nil
}

// IncrementWarning records a warning received from the tracker.
// Trackers typically warn before taking action (e.g., low ratio warning).
func (sm *StatsManager) IncrementWarning(infoHash string, trackerID uint32) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	stats, ok := sm.cache[infoHash]
	if !ok || stats.TrackerID != trackerID {
		stats = &TorrentStats{
			InfoHash:    infoHash,
			TrackerID:   trackerID,
			SeedTime:    0,
			TimesWarned: 0,
		}
	}

	stats.TimesWarned++
	sm.cache[infoHash] = stats

	return nil
}

// GetRatio returns the current ratio for a torrent.
func (sm *StatsManager) GetRatio(infoHash string, trackerID uint32) (float64, error) {
	stats, err := sm.GetStats(infoHash, trackerID)
	if err != nil {
		return 0, err
	}
	return stats.Ratio, nil
}

// GetSeedTime returns total seeding time including the current session.
func (sm *StatsManager) GetSeedTime(infoHash string, trackerID uint32) (time.Duration, error) {
	stats, err := sm.GetStats(infoHash, trackerID)
	if err != nil {
		return 0, err
	}

	seedTime := stats.SeedTime
	// Add current session if still seeding
	if stats.IsSeeding && !stats.SeedStart.IsZero() {
		seedTime += time.Since(stats.SeedStart)
	}

	return seedTime, nil
}

// RemoveStats deletes stats for a torrent.
func (sm *StatsManager) RemoveStats(infoHash string, trackerID uint32) error {
	sm.mu.Lock()
	delete(sm.cache, infoHash)
	sm.mu.Unlock()

	return nil
}

// ListStats returns all cached stats as a slice.
func (sm *StatsManager) ListStats() []*TorrentStats {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	stats := make([]*TorrentStats, 0, len(sm.cache))
	for _, s := range sm.cache {
		stats = append(stats, sm.copyStats(s))
	}

	return stats
}

// GetTotalUploaded sums uploaded bytes across all torrents.
func (sm *StatsManager) GetTotalUploaded() int64 {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var total int64
	for _, s := range sm.cache {
		total += s.Uploaded
	}
	return total
}

// GetTotalDownloaded sums downloaded bytes across all torrents.
func (sm *StatsManager) GetTotalDownloaded() int64 {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var total int64
	for _, s := range sm.cache {
		total += s.Downloaded
	}
	return total
}

// GetStatsByTracker returns all stats entries for a specific tracker.
func (sm *StatsManager) GetStatsByTracker(trackerID uint32) []*TorrentStats {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	stats := make([]*TorrentStats, 0)
	for _, s := range sm.cache {
		if s.TrackerID == trackerID {
			stats = append(stats, sm.copyStats(s))
		}
	}

	return stats
}

// Flush persists stats to storage. Currently a no-op; implement
// to save to database or file for durability across restarts.
func (sm *StatsManager) Flush() error {
	return nil
}

// ParseRatio converts a string ratio to float64.
func ParseRatio(ratioStr string) (float64, error) {
	var ratio float64
	_, err := fmt.Sscanf(ratioStr, "%f", &ratio)
	return ratio, err
}

// FormatBytes converts bytes to human-readable string (KB, MB, GB, etc.)
func FormatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// FormatRatio formats a ratio to 2 decimal places, handling near-zero values.
func FormatRatio(ratio float64) string {
	if ratio < 0.01 {
		return "0.00"
	}
	return fmt.Sprintf("%.2f", ratio)
}
