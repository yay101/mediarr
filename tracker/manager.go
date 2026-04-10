package tracker

import (
	"context"
	"fmt"
	"log"
	"sync"
)

// Manager coordinates multiple tracker instances and their statistics.
// It provides thread-safe access to trackers, configuration management,
// and seeding statistics tracking across all active torrents.
type Manager struct {
	trackers map[uint32]Tracker        // Active tracker instances by ID
	configs  map[uint32]*TrackerConfig // Cached configurations
	stats    *StatsManager             // Seeding/ratio statistics
	mu       sync.RWMutex              // Protects tracker and config maps
	closeCh  chan struct{}             // Close signal for background goroutines
}

// ManagerConfig contains the initial tracker configurations.
// Trackers with Enabled=false are skipped during initialization.
type ManagerConfig struct {
	Trackers []TrackerConfig
}

// NewManager creates a manager and initializes enabled trackers from config.
// Failed tracker creation (e.g., invalid config) is logged but doesn't
// prevent other trackers from being created.
func NewManager(cfg ManagerConfig) *Manager {
	m := &Manager{
		trackers: make(map[uint32]Tracker),
		configs:  make(map[uint32]*TrackerConfig),
		stats:    NewStatsManager(),
		closeCh:  make(chan struct{}),
	}

	for _, tc := range cfg.Trackers {
		if !tc.Enabled {
			continue
		}

		t, err := Create(&tc)
		if err != nil {
			log.Printf("Failed to create tracker %s: %v", tc.Name, err)
			continue
		}

		m.trackers[tc.ID] = t
		m.configs[tc.ID] = tc.Clone()
	}

	return m
}

// GetTracker retrieves a tracker by ID. Returns error if not found.
func (m *Manager) GetTracker(id uint32) (Tracker, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	t, ok := m.trackers[id]
	if !ok {
		return nil, fmt.Errorf("tracker %d not found", id)
	}
	return t, nil
}

// GetTrackerByName finds a tracker by its display name.
func (m *Manager) GetTrackerByName(name string) (Tracker, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, t := range m.trackers {
		if t.Name() == name {
			return t, nil
		}
	}
	return nil, fmt.Errorf("tracker %s not found", name)
}

// GetTrackerByType finds the first tracker of a given type.
// Useful when there's only one tracker of each type.
func (m *Manager) GetTrackerByType(tp TrackerType) (Tracker, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, t := range m.trackers {
		if t.Type() == tp {
			return t, nil
		}
	}
	return nil, fmt.Errorf("tracker of type %s not found", tp)
}

// ListTrackers returns all registered trackers regardless of enabled state.
func (m *Manager) ListTrackers() []Tracker {
	m.mu.RLock()
	defer m.mu.RUnlock()

	trackers := make([]Tracker, 0, len(m.trackers))
	for _, t := range m.trackers {
		trackers = append(trackers, t)
	}
	return trackers
}

// ListEnabledTrackers returns only trackers that are currently enabled.
// Uses cached config to avoid accessing tracker internals.
func (m *Manager) ListEnabledTrackers() []Tracker {
	m.mu.RLock()
	defer m.mu.RUnlock()

	trackers := make([]Tracker, 0)
	for _, t := range m.trackers {
		cfg := m.configs[t.GetConfig().ID]
		if cfg != nil && cfg.Enabled {
			trackers = append(trackers, t)
		}
	}
	return trackers
}

// AddTracker registers a new tracker with the manager.
// Fails if a tracker with the same ID already exists.
func (m *Manager) AddTracker(cfg *TrackerConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.trackers[cfg.ID]; exists {
		return fmt.Errorf("tracker with ID %d already exists", cfg.ID)
	}

	t, err := Create(cfg)
	if err != nil {
		return fmt.Errorf("create tracker: %w", err)
	}

	m.trackers[cfg.ID] = t
	m.configs[cfg.ID] = cfg.Clone()

	return nil
}

// RemoveTracker unregisters a tracker and cleans up its resources.
func (m *Manager) RemoveTracker(id uint32) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	t, ok := m.trackers[id]
	if !ok {
		return fmt.Errorf("tracker %d not found", id)
	}

	t.Close()

	delete(m.trackers, id)
	delete(m.configs, id)

	return nil
}

// UpdateTracker replaces an existing tracker with a new instance using updated config.
// Closes the old tracker before creating the new one.
func (m *Manager) UpdateTracker(id uint32, cfg *TrackerConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	t, ok := m.trackers[id]
	if !ok {
		return fmt.Errorf("tracker %d not found", id)
	}

	t.Close()

	newT, err := Create(cfg)
	if err != nil {
		return fmt.Errorf("create new tracker: %w", err)
	}

	m.trackers[id] = newT
	m.configs[id] = cfg.Clone()

	return nil
}

// AuthenticateAll attempts to authenticate with all enabled trackers.
// Returns the last error encountered, or nil if all succeeded.
// Logs individual failures but continues attempting remaining trackers.
func (m *Manager) AuthenticateAll(ctx context.Context) error {
	m.mu.RLock()
	trackers := make([]Tracker, 0, len(m.trackers))
	for _, t := range m.trackers {
		cfg := m.configs[t.GetConfig().ID]
		if cfg != nil && cfg.Enabled {
			trackers = append(trackers, t)
		}
	}
	m.mu.RUnlock()

	var lastErr error
	for _, t := range trackers {
		if err := t.Authenticate(ctx); err != nil {
			log.Printf("Failed to authenticate tracker %s: %v", t.Name(), err)
			lastErr = err
		}
	}

	return lastErr
}

// Authenticate triggers authentication for a specific tracker.
func (m *Manager) Authenticate(id uint32, ctx context.Context) error {
	m.mu.RLock()
	t, ok := m.trackers[id]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("tracker %d not found", id)
	}

	return t.Authenticate(ctx)
}

// TestTracker verifies connectivity and authentication for a single tracker.
func (m *Manager) TestTracker(id uint32, ctx context.Context) error {
	m.mu.RLock()
	t, ok := m.trackers[id]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("tracker %d not found", id)
	}

	return t.Test(ctx)
}

// TestAll checks connectivity for all enabled trackers.
// Returns a map of tracker ID to error (nil means success).
func (m *Manager) TestAll(ctx context.Context) map[uint32]error {
	results := make(map[uint32]error)

	m.mu.RLock()
	for id, t := range m.trackers {
		cfg := m.configs[t.GetConfig().ID]
		if cfg != nil && cfg.Enabled {
			results[id] = t.Test(ctx)
		}
	}
	m.mu.RUnlock()

	return results
}

// GetStats returns the stats manager for tracking seeding statistics.
func (m *Manager) GetStats() *StatsManager {
	return m.stats
}

// Stats retrieves seeding statistics for a specific torrent+tracker combination.
func (m *Manager) Stats(infoHash string, trackerID uint32) (*TorrentStats, error) {
	return m.stats.GetStats(infoHash, trackerID)
}

// UpdateStats updates upload/download counters for a torrent.
// Used during announce callbacks to track ratio progress.
func (m *Manager) UpdateStats(infoHash string, trackerID uint32, uploaded, downloaded int64) error {
	return m.stats.UpdateStats(infoHash, trackerID, uploaded, downloaded)
}

// StartSeeding marks the beginning of a seeding session for ratio tracking.
func (m *Manager) StartSeeding(infoHash string, trackerID uint32) error {
	return m.stats.StartSeeding(infoHash, trackerID)
}

// StopSeeding ends a seeding session and records total seed time.
func (m *Manager) StopSeeding(infoHash string, trackerID uint32) error {
	return m.stats.StopSeeding(infoHash, trackerID)
}

// Close shuts down the manager, closing all trackers and flushing stats.
func (m *Manager) Close() error {
	close(m.closeCh)

	m.mu.Lock()
	defer m.mu.Unlock()

	var lastErr error
	for _, t := range m.trackers {
		if err := t.Close(); err != nil {
			lastErr = err
		}
	}

	if err := m.stats.Flush(); err != nil {
		lastErr = err
	}

	return lastErr
}

// GetConfig returns a copy of a tracker's configuration.
func (m *Manager) GetConfig(id uint32) (*TrackerConfig, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cfg, ok := m.configs[id]
	if !ok {
		return nil, fmt.Errorf("tracker %d not found", id)
	}
	return cfg.Clone(), nil
}

// SetConfig updates a tracker's configuration and applies it immediately.
func (m *Manager) SetConfig(id uint32, cfg *TrackerConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.configs[id]; !exists {
		return fmt.Errorf("tracker %d not found", id)
	}

	m.configs[id] = cfg.Clone()
	m.trackers[id].SetConfig(cfg)

	return nil
}

// ListConfigs returns copies of all tracked configurations.
func (m *Manager) ListConfigs() []*TrackerConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()

	configs := make([]*TrackerConfig, 0, len(m.configs))
	for _, cfg := range m.configs {
		configs = append(configs, cfg.Clone())
	}
	return configs
}
