package automation

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/yay101/mediarr/internal/db"
	"github.com/yay101/mediarr/internal/download"
	"github.com/yay101/mediarr/internal/indexer"
	"github.com/yay101/mediarr/internal/monitor"
	"github.com/yay101/mediarr/internal/rss"
	"github.com/yay101/mediarr/internal/storage"
)

// Manager orchestrates automated content discovery and download.
// It monitors RSS feeds, searches indexers for wanted content,
// and queues downloads for processing by the download worker.
type Manager struct {
	db              *db.Database
	rssClient       *rss.Client
	downloadManager *download.Manager
	monitor         *monitor.Monitor
	indexerManager  *IndexerManager
	searchCache     *SearchDeduplicator
	storageManager  *storage.Manager

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// IndexerManager manages registered indexer instances for searching.
type IndexerManager struct {
	db       *db.Database
	indexers map[uint32]indexer.Indexer
	mu       sync.RWMutex
}

// SearchDeduplicator prevents duplicate searches for the same content.
// Tracks recent searches and skips duplicates within a configurable window.
// This prevents hammering indexers with identical queries.
type SearchDeduplicator struct {
	mu       sync.RWMutex
	searches map[string]time.Time // Key: search query hash, Value: last search time
	maxAge   time.Duration        // How long to remember searches
}

// NewSearchDeduplicator creates a deduplicator with the specified memory window.
func NewSearchDeduplicator(maxAge time.Duration) *SearchDeduplicator {
	return &SearchDeduplicator{
		searches: make(map[string]time.Time),
		maxAge:   maxAge,
	}
}

// ShouldSearch checks if a search should proceed or be skipped as duplicate.
// Returns true if the search should proceed, false if it was recently searched.
func (sd *SearchDeduplicator) ShouldSearch(key string) bool {
	sd.mu.Lock()
	defer sd.mu.Unlock()

	if lastSearch, exists := sd.searches[key]; exists {
		if time.Since(lastSearch) < sd.maxAge {
			return false
		}
	}
	sd.searches[key] = time.Now()
	return true
}

// Clear removes all tracked searches.
func (sd *SearchDeduplicator) Clear() {
	sd.mu.Lock()
	defer sd.mu.Unlock()
	sd.searches = make(map[string]time.Time)
}

// CleanOld removes searches older than maxAge to prevent memory growth.
func (sd *SearchDeduplicator) CleanOld() {
	sd.mu.Lock()
	defer sd.mu.Unlock()

	cutoff := time.Now().Add(-sd.maxAge)
	for key, lastSearch := range sd.searches {
		if lastSearch.Before(cutoff) {
			delete(sd.searches, key)
		}
	}
}

// NewManager creates an automation manager with the required dependencies.
func NewManager(database *db.Database, rssClient *rss.Client, dlMgr *download.Manager, mon *monitor.Monitor, storageMgr *storage.Manager) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		db:              database,
		rssClient:       rssClient,
		downloadManager: dlMgr,
		monitor:         mon,
		indexerManager:  &IndexerManager{db: database, indexers: make(map[uint32]indexer.Indexer)},
		searchCache:     NewSearchDeduplicator(6 * time.Hour),
		storageManager:  storageMgr,
		ctx:             ctx,
		cancel:          cancel,
	}
}

func (m *Manager) Start() {
	m.wg.Add(3)
	go m.rssLoop()
	go m.searchLoop()
	go m.verifyLoop()
	slog.Info("automation manager started")
}

func (m *Manager) Stop() {
	m.cancel()
	m.wg.Wait()
	slog.Info("automation manager stopped")
}

func (m *Manager) verifyLoop() {
	defer m.wg.Done()
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()

	m.verifyMedia()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.verifyMedia()
		}
	}
}

func (m *Manager) verifyMedia() {
	if m.storageManager == nil {
		return
	}

	slog.Info("verifying media storage integrity")
	verifier := storage.NewVerifier(m.db, m.storageManager)
	results := verifier.VerifyAllMedia(m.ctx)

	var failed int
	for _, r := range results {
		if !r.Success {
			failed++
			slog.Warn("media verification failed", "type", r.MediaType, "id", r.ID, "error", r.Error)
		}
	}

	slog.Info("media verification complete", "total", len(results), "failed", failed)
}

func (m *Manager) rssLoop() {
	defer m.wg.Done()
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()

	m.checkRSS()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.checkRSS()
		}
	}
}

func (m *Manager) checkRSS() {
	slog.Info("checking RSS feeds")
	results, err := m.rssClient.CheckAllFeeds()
	if err != nil {
		slog.Error("RSS check failed", "error", err)
		return
	}

	for url, items := range results {
		slog.Info("processing RSS items", "url", url, "count", len(items))
		for _, item := range items {
			m.processRSSItem(item)
		}
	}
}

func (m *Manager) processRSSItem(item rss.FeedItem) {
	res := indexer.SearchResult{
		Title:      item.Title,
		Size:       item.Size,
		InfoHash:   item.InfoHash,
		MagnetURI:  item.MagnetURI,
		TorrentURL: item.TorrentURL,
		NZBLink:    item.NZBLink,
		Quality:    item.Quality,
		Resolution: item.Resolution,
		Codec:      item.Codec,
	}

	watchlist, err := m.monitor.GetWatchlist()
	if err != nil {
		return
	}

	for _, wanted := range watchlist {
		if wanted.Complete {
			continue
		}

		if m.matchItem(wanted, res) {
			slog.Info("found match in RSS", "title", res.Title, "for", wanted.Title)
			m.triggerDownload(wanted, res)
		}
	}
}

func (m *Manager) searchLoop() {
	defer m.wg.Done()
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.performAutoSearch()
		}
	}
}

func (m *Manager) performAutoSearch() {
	slog.Info("performing automatic search for watchlist items")

	m.searchCache.CleanOld()

	items, err := m.monitor.GetItemsNeedingSearch(24 * time.Hour)
	if err == nil {
		for _, item := range items {
			cacheKey := fmt.Sprintf("search:%d:%d", item.MediaType, item.MediaID)
			if m.searchCache.ShouldSearch(cacheKey) {
				m.searchForItem(item)
			}
		}
	}

	m.checkMissingEpisodes()
}

func (m *Manager) checkMissingEpisodes() {
	slog.Info("checking for missing TV episodes")
	tvshows, err := m.db.TVShows()
	if err != nil {
		return
	}

	episodesTable, err := m.db.TVEpisodes()
	if err != nil {
		return
	}

	tvshows.Scan(func(show db.TVShow) bool {
		if show.Monitored && (show.Status == db.MediaStatusAvailable || show.Status == db.MediaStatusQueued) {
			eps, _ := episodesTable.Query("ShowID", show.ID)
			for _, ep := range eps {
				if ep.Monitored && ep.Status == db.MediaStatusMissing && ep.AirDate.Before(time.Now()) {
					slog.Info("found missing episode", "show", show.Title, "S", ep.Season, "E", ep.Episode)
					m.searchForEpisode(show, ep)
				}
			}
		}
		return true
	})
}

func (m *Manager) searchForEpisode(show db.TVShow, ep db.TVEpisode) {
	episodesTable, _ := m.db.TVEpisodes()
	showTable, _ := m.db.TVShows()

	available, _ := episodesTable.Filter(func(e db.TVEpisode) bool {
		if e.Season != ep.Season || e.Episode != ep.Episode || e.Status != db.MediaStatusAvailable {
			return false
		}
		s, _ := showTable.Get(e.ShowID)
		return s != nil && s.TMDBID == show.TMDBID
	})

	if len(available) > 0 {
		slog.Info("episode already available in global pool, linking", "show", show.Title, "S", ep.Season, "E", ep.Episode)
		ep.Status = db.MediaStatusAvailable
		ep.Path = available[0].Path
		ep.UpdatedAt = time.Now()
		_ = episodesTable.Update(ep.ID, &ep)
		return
	}

	downloading, _ := episodesTable.Filter(func(e db.TVEpisode) bool {
		if e.Season != ep.Season || e.Episode != ep.Episode || (e.Status != db.MediaStatusDownloading && e.Status != db.MediaStatusQueued) {
			return false
		}
		if e.ID == ep.ID {
			return false
		}
		s, _ := showTable.Get(e.ShowID)
		return s != nil && s.TMDBID == show.TMDBID
	})

	if len(downloading) > 0 {
		slog.Info("episode already being downloaded by another user, skipping search", "show", show.Title, "S", ep.Season, "E", ep.Episode)
		return
	}

	cacheKey := fmt.Sprintf("episode:%d:%d:%d", show.TMDBID, ep.Season, ep.Episode)
	if !m.searchCache.ShouldSearch(cacheKey) {
		slog.Info("recently searched for episode, skipping", "show", show.Title, "S", ep.Season, "E", ep.Episode)
		return
	}

	query := fmt.Sprintf("%s S%02dE%02d", show.Title, ep.Season, ep.Episode)
	slog.Info("searching for episode", "query", query)

	results := m.searchAllIndexers(query, indexer.CategoryTV)
	if len(results) == 0 {
		return
	}

	bestResults, err := m.monitor.Decisions(results)
	if err != nil || len(bestResults) == 0 {
		return
	}

	best := bestResults[0]
	m.triggerEpisodeDownload(show, ep, best)
}

func (m *Manager) searchAllIndexers(query string, category indexer.Category) []indexer.SearchResult {
	indexers, err := m.indexerManager.GetEnabledIndexers()
	if err != nil {
		return nil
	}

	var allResults []indexer.SearchResult
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, idx := range indexers {
		cfg := idx.GetConfig()
		limiter := indexer.GlobalSearchLimiter.GetLimiter(cfg.ID)

		if limiter.IsCooldown(cfg.ID) {
			slog.Debug("indexer in cooldown", "indexer", cfg.Name)
			continue
		}

		limiter.Wait(cfg.ID)

		wg.Add(1)
		go func(i indexer.Indexer) {
			defer wg.Done()

			cfg := i.GetConfig()
			results, err := i.Search(m.ctx, query, category, 0)
			if err != nil {
				slog.Warn("search failed", "indexer", cfg.Name, "error", err)
				indexer.GlobalSearchLimiter.GetLimiter(cfg.ID).RecordFailure(cfg.ID)
				return
			}

			indexer.GlobalSearchLimiter.GetLimiter(cfg.ID).RecordSuccess(cfg.ID)

			mu.Lock()
			for j := range results {
				results[j].Indexer = cfg.Name
			}
			allResults = append(allResults, results...)
			mu.Unlock()
		}(idx)
	}

	wg.Wait()
	return allResults
}

func (m *Manager) SearchIndexers(ctx context.Context, query string) ([]indexer.SearchResult, error) {
	return m.searchAllIndexers(query, indexer.CategoryAll), nil
}

func (m *Manager) SearchForItem(item monitor.WatchlistItem) {
	m.searchForItem(item)
}

func (m *Manager) searchForItem(item monitor.WatchlistItem) {
	var tmdbID uint32
	if item.MediaType == db.MediaTypeMovie {
		table, _ := m.db.Movies()
		mov, _ := table.Get(item.MediaID)
		if mov != nil {
			tmdbID = mov.TMDBID
		}
	} else if item.MediaType == db.MediaTypeTV {
		table, _ := m.db.TVShows()
		show, _ := table.Get(item.MediaID)
		if show != nil {
			tmdbID = show.TMDBID
		}
	}

	if tmdbID > 0 {
		if item.MediaType == db.MediaTypeMovie {
			table, _ := m.db.Movies()
			available, _ := table.Filter(func(mov db.Movie) bool {
				return mov.TMDBID == tmdbID && mov.Status == db.MediaStatusAvailable
			})
			if len(available) > 0 {
				slog.Info("movie already available in global pool, linking", "title", item.Title)
				m.monitor.UpdateWatchlistItem(item.ID, map[string]interface{}{"complete": true})
				mov, _ := table.Get(item.MediaID)
				if mov != nil {
					mov.Status = db.MediaStatusAvailable
					mov.Path = available[0].Path
					_ = table.Update(mov.ID, mov)
				}
				return
			}

			downloading, _ := table.Filter(func(mov db.Movie) bool {
				return mov.TMDBID == tmdbID && (mov.Status == db.MediaStatusDownloading || mov.Status == db.MediaStatusQueued) && mov.ID != item.MediaID
			})
			if len(downloading) > 0 {
				slog.Info("movie already being downloaded by another user, skipping search", "title", item.Title)
				return
			}
		}
	}

	slog.Info("searching for item", "title", item.Title)

	results := m.searchAllIndexers(item.Title, indexer.CategoryAll)
	if len(results) == 0 {
		return
	}

	bestResults, err := m.monitor.Decisions(results)
	if err != nil || len(bestResults) == 0 {
		return
	}

	best := bestResults[0]
	slog.Info("found best release via search", "title", best.Title)
	m.triggerDownload(item, best)
}

func (m *Manager) matchItem(wanted monitor.WatchlistItem, res indexer.SearchResult) bool {
	title := normalizeTitle(res.Title)
	wantedTitle := normalizeTitle(wanted.Title)

	if !strings.Contains(title, wantedTitle) {
		if wanted.Year > 0 {
			yearStr := fmt.Sprintf("%d", wanted.Year)
			if !strings.Contains(title, wantedTitle) || !strings.Contains(title, yearStr) {
				return false
			}
		} else {
			return false
		}
	}

	if wanted.Year > 0 {
		yearStr := fmt.Sprintf("%d", wanted.Year)
		if !strings.Contains(title, yearStr) {
			return false
		}
	}

	results, err := m.monitor.Decisions([]indexer.SearchResult{res})
	return err == nil && len(results) > 0
}

func normalizeTitle(title string) string {
	title = strings.ToLower(title)
	title = strings.ReplaceAll(title, "_", " ")
	title = strings.ReplaceAll(title, ".", " ")
	title = strings.TrimSpace(title)
	return title
}

func (m *Manager) triggerDownload(item monitor.WatchlistItem, res indexer.SearchResult) {
	job := &db.DownloadJob{
		UserID:    item.UserID,
		MediaType: item.MediaType,
		MediaID:   item.MediaID,
		Title:     res.Title,
		Status:    db.DownloadStatusQueued,
	}

	if res.NZBLink != "" {
		job.Provider = db.DownloadProviderUsenet
		job.NZBData = res.NZBLink
	} else {
		job.Provider = db.DownloadProviderTorrent
		job.MagnetURI = res.MagnetURI
		job.InfoHash = res.InfoHash
		if job.MagnetURI == "" && res.TorrentURL != "" {
			job.MagnetURI = res.TorrentURL
		}
	}

	if err := m.downloadManager.AddDownload(job); err != nil {
		slog.Error("failed to trigger download", "error", err)
		return
	}

	m.monitor.UpdateWatchlistItem(item.ID, map[string]interface{}{"complete": true})
}

func (m *Manager) triggerEpisodeDownload(show db.TVShow, ep db.TVEpisode, res indexer.SearchResult) {
	job := &db.DownloadJob{
		MediaType: db.MediaTypeTV,
		MediaID:   ep.ID,
		Title:     res.Title,
		Status:    db.DownloadStatusQueued,
	}

	if res.NZBLink != "" {
		job.Provider = db.DownloadProviderUsenet
		job.NZBData = res.NZBLink
	} else {
		job.Provider = db.DownloadProviderTorrent
		job.MagnetURI = res.MagnetURI
		job.InfoHash = res.InfoHash
	}

	if err := m.downloadManager.AddDownload(job); err != nil {
		return
	}

	ep.Status = db.MediaStatusDownloading
	ep.UpdatedAt = time.Now()
	table, _ := m.db.TVEpisodes()
	_ = table.Update(ep.ID, &ep)
}

func (im *IndexerManager) GetEnabledIndexers() ([]indexer.Indexer, error) {
	table, err := im.db.IndexerConfigs()
	if err != nil {
		return nil, err
	}

	configs, err := table.Filter(func(c db.IndexerConfig) bool {
		return c.Enabled
	})
	if err != nil {
		return nil, err
	}

	im.mu.Lock()
	defer im.mu.Unlock()

	var result []indexer.Indexer
	for _, cfg := range configs {
		if idx, ok := im.indexers[cfg.ID]; ok {
			result = append(result, idx)
			continue
		}

		catList := strings.Split(cfg.Categories, ",")
		var categories []indexer.Category
		for _, catStr := range catList {
			categories = append(categories, indexer.ParseCategory(catStr))
		}

		idxCfg := &indexer.IndexerConfig{
			ID:         cfg.ID,
			Name:       cfg.Name,
			Type:       indexer.IndexerType(cfg.Type),
			URL:        cfg.URL,
			APIKey:     cfg.APIKey,
			Username:   cfg.Username,
			Password:   cfg.Password,
			Categories: categories,
			Enabled:    cfg.Enabled,
		}

		idx, err := indexer.CreateIndexer(idxCfg)
		if err != nil {
			continue
		}
		im.indexers[cfg.ID] = idx
		result = append(result, idx)
	}

	return result, nil
}

func (im *IndexerManager) ResetIndexer(id uint32) {
	im.mu.Lock()
	defer im.mu.Unlock()
	delete(im.indexers, id)
}
