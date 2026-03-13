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
)

type Manager struct {
	db              *db.Database
	rssClient       *rss.Client
	downloadManager *download.Manager
	monitor         *monitor.Monitor
	indexerManager  *IndexerManager

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

type IndexerManager struct {
	db       *db.Database
	indexers map[uint32]indexer.Indexer
	mu       sync.RWMutex
}

func NewManager(database *db.Database, rssClient *rss.Client, dlMgr *download.Manager, mon *monitor.Monitor) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		db:              database,
		rssClient:       rssClient,
		downloadManager: dlMgr,
		monitor:         mon,
		indexerManager:  &IndexerManager{db: database, indexers: make(map[uint32]indexer.Indexer)},
		ctx:             ctx,
		cancel:          cancel,
	}
}

func (m *Manager) Start() {
	m.wg.Add(2)
	go m.rssLoop()
	go m.searchLoop()
	slog.Info("automation manager started")
}

func (m *Manager) Stop() {
	m.cancel()
	m.wg.Wait()
	slog.Info("automation manager stopped")
}

func (m *Manager) rssLoop() {
	defer m.wg.Done()
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()

	// Initial check
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
	// Convert rss.FeedItem to indexer.SearchResult for decisions
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

	// Decision logic: does this match anything in our watchlist?
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

	// 1. Check watchlist for movies/manual entries
	items, err := m.monitor.GetItemsNeedingSearch(24 * time.Hour)
	if err == nil {
		for _, item := range items {
			m.searchForItem(item)
		}
	}

	// 2. Check for missing TV episodes
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
		// Only check if show is monitored and not completed/ignored
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
	// Shared Global Pool: Check if anyone else already has this episode or is downloading it
	episodesTable, _ := m.db.TVEpisodes()
	showTable, _ := m.db.TVShows()

	// 1. Check for available episode globally
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

	// 2. Check if anyone is already downloading it
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

	query := fmt.Sprintf("%s S%02dE%02d", show.Title, ep.Season, ep.Episode)
	slog.Info("searching for episode", "query", query)

	indexers, err := m.indexerManager.GetEnabledIndexers()
	if err != nil {
		return
	}

	var allResults []indexer.SearchResult
	for _, idx := range indexers {
		results, err := idx.Search(m.ctx, query, indexer.CategoryTV, 0)
		if err != nil {
			continue
		}
		allResults = append(allResults, results...)
	}

	if len(allResults) == 0 {
		return
	}

	bestResults, err := m.monitor.Decisions(allResults)
	if err != nil || len(bestResults) == 0 {
		return
	}

	best := bestResults[0]
	m.triggerEpisodeDownload(show, ep, best)
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

	// Update episode status
	ep.Status = db.MediaStatusDownloading
	ep.UpdatedAt = time.Now()
	table, _ := m.db.TVEpisodes()
	_ = table.Update(ep.ID, &ep)
}

func (m *Manager) SearchIndexers(ctx context.Context, query string) ([]indexer.SearchResult, error) {
	indexers, err := m.indexerManager.GetEnabledIndexers()
	if err != nil {
		return nil, err
	}

	var allResults []indexer.SearchResult
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, idx := range indexers {
		wg.Add(1)
		go func(i indexer.Indexer) {
			defer wg.Done()
			results, err := i.Search(ctx, query, indexer.CategoryAll, 0)
			if err != nil {
				return
			}
			mu.Lock()
			allResults = append(allResults, results...)
			mu.Unlock()
		}(idx)
	}

	wg.Wait()
	return allResults, nil
}

func (m *Manager) SearchForItem(item monitor.WatchlistItem) {
	m.searchForItem(item)
}

func (m *Manager) searchForItem(item monitor.WatchlistItem) {
	// 1. Fetch the actual media record to get TMDBID
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
		// 2. Check if anyone has it available in global pool
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

			// 3. Check if anyone is already downloading it
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

	indexers, err := m.indexerManager.GetEnabledIndexers()
	if err != nil {
		return
	}

	var allResults []indexer.SearchResult
	for _, idx := range indexers {
		results, err := idx.Search(m.ctx, item.Title, indexer.CategoryAll, 0)
		if err != nil {
			continue
		}
		allResults = append(allResults, results...)
	}

	if len(allResults) == 0 {
		return
	}

	// Filter and pick best
	bestResults, err := m.monitor.Decisions(allResults)
	if err != nil || len(bestResults) == 0 {
		return
	}

	// For now, just pick the first one from filtered results
	best := bestResults[0]
	slog.Info("found best release via search", "title", best.Title)
	m.triggerDownload(item, best)
}

func (m *Manager) matchItem(wanted monitor.WatchlistItem, res indexer.SearchResult) bool {
	// Very simple fuzzy match
	title := strings.ToLower(res.Title)
	wantedTitle := strings.ToLower(wanted.Title)

	if !strings.Contains(title, wantedTitle) {
		return false
	}

	if wanted.Year > 0 {
		yearStr := fmt.Sprintf("%d", wanted.Year)
		if !strings.Contains(title, yearStr) {
			return false
		}
	}

	// Use monitor decisions for quality/size checks
	results, err := m.monitor.Decisions([]indexer.SearchResult{res})
	return err == nil && len(results) > 0
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

	// Update watchlist item
	m.monitor.UpdateWatchlistItem(item.ID, map[string]interface{}{"complete": true})
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

		// Create new indexer instance
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
