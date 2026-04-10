package search

import (
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/yay101/mediarr/internal/db"
	"github.com/yay101/mediarr/internal/indexer"
	"github.com/yay101/mediarr/internal/tracker"
)

type Manager struct {
	DB             *db.Database
	TrackerManager *tracker.Manager
	IndexerManager *IndexerManager
	downloadQueue  chan *DownloadRequest
	results        map[string]*SearchSession
	mu             sync.RWMutex
}

type IndexerManager struct {
	DB       *db.Database
	Indexers map[uint32]indexer.Indexer
	mu       sync.RWMutex
}

func NewIndexerManager(db *db.Database) *IndexerManager {
	return &IndexerManager{
		DB:       db,
		Indexers: make(map[uint32]indexer.Indexer),
	}
}

type SearchSession struct {
	ID        string
	Query     string
	MediaType db.MediaType
	MediaID   uint32
	Results   []SearchResult
	CreatedAt time.Time
	ExpiresAt time.Time
	mu        sync.Mutex
}

type SearchResult struct {
	Guid        string
	Title       string
	Link        string
	Size        int64
	Seeders     int
	Leechers    int
	InfoHash    string
	MagnetURI   string
	TorrentURL  string
	Quality     string
	Codec       string
	Resolution  string
	Indexer     string
	IndexerID   uint32
	Tracker     string
	TrackerID   uint32
	IsFreeleech bool
	IsRepack    bool
	PublishDate time.Time
	Score       int
}

type DownloadRequest struct {
	Result    *SearchResult
	MediaID   uint32
	MediaType db.MediaType
	Title     string
	Quality   string
	Force     bool
	UserID    uint32
}

type ManagerConfig struct {
	DB             *db.Database
	TrackerManager *tracker.Manager
	IndexerManager *IndexerManager
	DownloadQueue  chan *DownloadRequest
}

func NewManager(cfg ManagerConfig) *Manager {
	if cfg.DownloadQueue == nil {
		cfg.DownloadQueue = make(chan *DownloadRequest, 100)
	}

	return &Manager{
		DB:             cfg.DB,
		TrackerManager: cfg.TrackerManager,
		IndexerManager: cfg.IndexerManager,
		downloadQueue:  cfg.DownloadQueue,
		results:        make(map[string]*SearchSession),
	}
}

func (m *Manager) RegisterIndexer(idx indexer.Indexer) {
	m.IndexerManager.mu.Lock()
	defer m.IndexerManager.mu.Unlock()
	m.IndexerManager.Indexers[idx.GetConfig().ID] = idx
}

func (m *Manager) UnregisterIndexer(id uint32) {
	m.IndexerManager.mu.Lock()
	defer m.IndexerManager.mu.Unlock()
	delete(m.IndexerManager.Indexers, id)
}

func (m *Manager) SearchAll(ctx interface{}, query string, mediaType db.MediaType, mediaID uint32) (*SearchSession, error) {
	session := &SearchSession{
		ID:        generateSessionID(),
		Query:     query,
		MediaType: mediaType,
		MediaID:   mediaID,
		Results:   make([]SearchResult, 0),
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(30 * time.Minute),
	}

	m.mu.Lock()
	m.results[session.ID] = session
	m.mu.Unlock()

	go m.performSearch(session)

	return session, nil
}

func (m *Manager) performSearch(session *SearchSession) {
	var wg sync.WaitGroup

	m.IndexerManager.mu.RLock()
	indexers := make([]indexer.Indexer, 0, len(m.IndexerManager.Indexers))
	for _, idx := range m.IndexerManager.Indexers {
		if idx.GetConfig().Enabled {
			indexers = append(indexers, idx)
		}
	}
	m.IndexerManager.mu.RUnlock()

	category := m.mediaTypeToCategory(session.MediaType)

	for _, idx := range indexers {
		wg.Add(1)
		go func(idx indexer.Indexer) {
			defer wg.Done()

			indexerID := idx.GetConfig().ID
			if !indexer.GlobalSearchLimiter.Allow(indexerID) {
				indexer.GlobalSearchLimiter.Wait(indexerID)
			}

			results, err := idx.Search(nil, session.Query, category, 50)
			if err != nil {
				slog.Warn("indexer search failed", "indexer", idx.GetConfig().Name, "error", err)
				indexer.GlobalSearchLimiter.RecordFailure(indexerID)
				return
			}

			indexer.GlobalSearchLimiter.RecordSuccess(indexerID)

			for _, r := range results {
				result := SearchResult{
					Guid:        r.Guid,
					Title:       r.Title,
					Link:        r.Link,
					Size:        r.Size,
					Seeders:     r.Seeders,
					Leechers:    r.Leechers,
					InfoHash:    r.InfoHash,
					MagnetURI:   r.MagnetURI,
					TorrentURL:  r.TorrentURL,
					Quality:     r.Quality,
					Codec:       r.Codec,
					Resolution:  r.Resolution,
					Indexer:     idx.GetConfig().Name,
					IndexerID:   indexerID,
					IsFreeleech: r.IsFreeleech,
					IsRepack:    r.IsRepack,
					PublishDate: r.PublishDate,
					Score:       r.Score,
				}
				session.AddResult(result)
			}
		}(idx)
	}

	if m.TrackerManager != nil {
		for _, t := range m.TrackerManager.ListEnabledTrackers() {
			if t.Type() == "basic" {
				wg.Add(1)
				go func(trk tracker.Tracker) {
					defer wg.Done()
				}(t)
			}
		}
	}

	wg.Wait()
	session.SortResults()
}

func (s *SearchSession) AddResult(result SearchResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Results = append(s.Results, result)
}

func (s *SearchSession) SortResults() {
	s.mu.Lock()
	defer s.mu.Unlock()

	sort.Slice(s.Results, func(i, j int) bool {
		a, b := s.Results[i], s.Results[j]

		if a.IsFreeleech && !b.IsFreeleech {
			return true
		}
		if !a.IsFreeleech && b.IsFreeleech {
			return false
		}

		if a.Score != b.Score {
			return a.Score > b.Score
		}

		if a.Seeders != b.Seeders {
			return a.Seeders > b.Seeders
		}

		if a.Size != b.Size {
			return a.Size > b.Size
		}

		return a.PublishDate.After(b.PublishDate)
	})
}

func (m *Manager) GetSession(id string) (*SearchSession, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.results[id], true
}

func (m *Manager) ClearSession(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.results, id)
}

func (m *Manager) QueueDownload(req *DownloadRequest) error {
	select {
	case m.downloadQueue <- req:
		return nil
	default:
		return nil
	}
}

func (m *Manager) CreateDownloadJob(req *DownloadRequest) (*db.DownloadJob, error) {
	job := &db.DownloadJob{
		UserID:    req.UserID,
		MediaType: req.MediaType,
		MediaID:   req.MediaID,
		Title:     req.Title,
		Status:    db.DownloadStatusQueued,
	}

	if req.Result.MagnetURI != "" {
		job.Provider = db.DownloadProviderTorrent
		job.MagnetURI = req.Result.MagnetURI
		job.InfoHash = req.Result.InfoHash
	} else if req.Result.InfoHash != "" {
		job.Provider = db.DownloadProviderTorrent
		job.InfoHash = req.Result.InfoHash
	}

	return job, nil
}

func (m *Manager) mediaTypeToCategory(mediaType db.MediaType) indexer.Category {
	switch mediaType {
	case db.MediaTypeMovie:
		return indexer.CategoryMovie
	case db.MediaTypeTV:
		return indexer.CategoryTV
	case db.MediaTypeMusic:
		return indexer.CategoryAudio
	case db.MediaTypeBook:
		return indexer.CategoryBook
	default:
		return indexer.CategoryAll
	}
}

func generateSessionID() string {
	return fmt.Sprintf("search_%d_%d", time.Now().UnixNano(), time.Now().UnixMicro())
}
