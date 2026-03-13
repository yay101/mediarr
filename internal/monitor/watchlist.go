package monitor

import (
	"sort"
	"strings"
	"time"

	"github.com/yay101/mediarr/internal/db"
	"github.com/yay101/mediarr/internal/indexer"
)

type WatchlistItem struct {
	ID          uint32
	UserID      uint32
	MediaType   db.MediaType
	MediaID     uint32
	Title       string
	Year        uint16
	Quality     string
	Keywords    []string
	Complete    bool
	LastSearch  time.Time
	SearchCount uint32
	AddedAt     time.Time
	UpdatedAt   time.Time
}

type QualityProfile struct {
	ID            uint32
	Name          string
	PreferredType string
	MinScore      uint32
	AllowedRes    []string
	PreferredRes  string
	AllowedCodecs []string
	AllowedGroups []string
	MinSize       int64
	MaxSize       int64
}

type Monitor struct {
	db      *db.Database
	profile *QualityProfile
}

func NewMonitor(database *db.Database, profile *QualityProfile) *Monitor {
	return &Monitor{
		db:      database,
		profile: profile,
	}
}

func (m *Monitor) AddToWatchlist(userID uint32, mediaType db.MediaType, title string, year uint16, quality string) (uint32, error) {
	table, err := m.db.Watchlist()
	if err != nil {
		return 0, err
	}

	now := time.Now()
	item := &db.WatchlistItem{
		UserID:      userID,
		MediaType:   mediaType,
		Title:       title,
		Year:        year,
		Quality:     quality,
		Status:      db.MediaStatusQueued,
		Complete:    false,
		LastSearch:  now,
		SearchCount: 0,
		AddedAt:     now,
		UpdatedAt:   now,
	}

	return table.Insert(item)
}

func (m *Monitor) RemoveFromWatchlist(id uint32) error {
	table, err := m.db.Watchlist()
	if err != nil {
		return err
	}

	return table.Delete(id)
}

func (m *Monitor) GetWatchlist() ([]WatchlistItem, error) {
	table, err := m.db.Watchlist()
	if err != nil {
		return nil, err
	}

	var items []db.WatchlistItem
	table.Scan(func(item db.WatchlistItem) bool {
		items = append(items, item)
		return true
	})

	result := make([]WatchlistItem, len(items))
	for i, item := range items {
		result[i] = WatchlistItem{
			ID:          item.ID,
			UserID:      item.UserID,
			MediaType:   item.MediaType,
			MediaID:     item.MediaID,
			Title:       item.Title,
			Year:        item.Year,
			Quality:     item.Quality,
			Keywords:    splitString(item.Keywords),
			Complete:    item.Complete,
			LastSearch:  item.LastSearch,
			SearchCount: item.SearchCount,
			AddedAt:     item.AddedAt,
			UpdatedAt:   item.UpdatedAt,
		}
	}

	return result, nil
}

func (m *Monitor) GetWatchlistItem(id uint32) (*WatchlistItem, error) {
	table, err := m.db.Watchlist()
	if err != nil {
		return nil, err
	}

	item, err := table.Get(id)
	if err != nil {
		return nil, err
	}

	return &WatchlistItem{
		ID:          item.ID,
		UserID:      item.UserID,
		MediaType:   item.MediaType,
		MediaID:     item.MediaID,
		Title:       item.Title,
		Year:        item.Year,
		Quality:     item.Quality,
		Keywords:    splitString(item.Keywords),
		Complete:    item.Complete,
		LastSearch:  item.LastSearch,
		SearchCount: item.SearchCount,
		AddedAt:     item.AddedAt,
		UpdatedAt:   item.UpdatedAt,
	}, nil
}

func (m *Monitor) UpdateWatchlistItem(id uint32, updates map[string]interface{}) error {
	table, err := m.db.Watchlist()
	if err != nil {
		return err
	}

	item, err := table.Get(id)
	if err != nil {
		return err
	}

	if quality, ok := updates["quality"].(string); ok {
		item.Quality = quality
	}
	if complete, ok := updates["complete"].(bool); ok {
		item.Complete = complete
		if complete {
			item.Status = db.MediaStatusAvailable
		}
	}
	if keywords, ok := updates["keywords"].([]string); ok {
		item.Keywords = strings.Join(keywords, ",")
	}

	item.UpdatedAt = time.Now()
	return table.Update(id, item)
}

func (m *Monitor) MarkAsSearching(id uint32) error {
	table, err := m.db.Watchlist()
	if err != nil {
		return err
	}

	item, err := table.Get(id)
	if err != nil {
		return err
	}

	item.Status = db.MediaStatusSearching
	item.LastSearch = time.Now()
	item.SearchCount++
	item.UpdatedAt = time.Now()

	return table.Update(id, item)
}

func (m *Monitor) GetItemsNeedingSearch(interval time.Duration) ([]WatchlistItem, error) {
	table, err := m.db.Watchlist()
	if err != nil {
		return nil, err
	}

	threshold := time.Now().Add(-interval)
	var items []db.WatchlistItem
	table.Scan(func(item db.WatchlistItem) bool {
		if !item.Complete && (item.LastSearch.Before(threshold) || item.SearchCount == 0) {
			items = append(items, item)
		}
		return true
	})

	result := make([]WatchlistItem, len(items))
	for i, item := range items {
		result[i] = WatchlistItem{
			ID:          item.ID,
			UserID:      item.UserID,
			MediaType:   item.MediaType,
			MediaID:     item.MediaID,
			Title:       item.Title,
			Year:        item.Year,
			Quality:     item.Quality,
			Keywords:    splitString(item.Keywords),
			Complete:    item.Complete,
			LastSearch:  item.LastSearch,
			SearchCount: item.SearchCount,
			AddedAt:     item.AddedAt,
			UpdatedAt:   item.UpdatedAt,
		}
	}

	return result, nil
}

func (m *Monitor) ImportFromMedia() error {
	movies, err := m.db.Movies()
	if err != nil {
		return err
	}

	now := time.Now()
	watchlistTable, err := m.db.Watchlist()
	if err != nil {
		return err
	}

	movies.Scan(func(movie db.Movie) bool {
		if movie.Status == db.MediaStatusQueued {
			item := &db.WatchlistItem{
				UserID:      movie.UserID,
				MediaType:   db.MediaTypeMovie,
				MediaID:     movie.ID,
				Title:       movie.Title,
				Year:        movie.Year,
				Quality:     movie.Quality,
				Status:      db.MediaStatusQueued,
				Complete:    false,
				LastSearch:  now,
				SearchCount: 0,
				AddedAt:     now,
				UpdatedAt:   now,
			}
			watchlistTable.Insert(item)
		}
		return true
	})

	tvshows, err := m.db.TVShows()
	if err != nil {
		return err
	}

	tvshows.Scan(func(show db.TVShow) bool {
		if show.Status == db.MediaStatusQueued {
			item := &db.WatchlistItem{
				UserID:      show.UserID,
				MediaType:   db.MediaTypeTV,
				MediaID:     show.ID,
				Title:       show.Title,
				Year:        show.Year,
				Status:      db.MediaStatusQueued,
				Complete:    false,
				LastSearch:  now,
				SearchCount: 0,
				AddedAt:     now,
				UpdatedAt:   now,
			}
			watchlistTable.Insert(item)
		}
		return true
	})

	return nil
}

func (m *Monitor) SetQualityProfile(profile *QualityProfile) {
	m.profile = profile
}

func (m *Monitor) GetQualityProfile() *QualityProfile {
	return m.profile
}

func (m *Monitor) GetQualityProfiles() ([]QualityProfile, error) {
	table, err := m.db.QualityProfiles()
	if err != nil {
		return nil, err
	}

	var profiles []db.QualityProfile
	table.Scan(func(p db.QualityProfile) bool {
		profiles = append(profiles, p)
		return true
	})

	result := make([]QualityProfile, len(profiles))
	for i, p := range profiles {
		result[i] = QualityProfile{
			ID:            p.ID,
			Name:          p.Name,
			PreferredType: p.PreferredType,
			MinScore:      p.MinScore,
			AllowedRes:    splitString(p.AllowedRes),
			PreferredRes:  p.PreferredRes,
			AllowedCodecs: splitString(p.AllowedCodecs),
			AllowedGroups: splitString(p.AllowedGroups),
			MinSize:       p.MinSize,
			MaxSize:       p.MaxSize,
		}
	}

	return result, nil
}

func splitString(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

func (m *Monitor) CreateQualityProfile(profile *QualityProfile) (uint32, error) {
	table, err := m.db.QualityProfiles()
	if err != nil {
		return 0, err
	}

	now := time.Now()
	p := &db.QualityProfile{
		Name:          profile.Name,
		PreferredType: profile.PreferredType,
		MinScore:      profile.MinScore,
		AllowedRes:    joinStrings(profile.AllowedRes),
		PreferredRes:  profile.PreferredRes,
		AllowedCodecs: joinStrings(profile.AllowedCodecs),
		AllowedGroups: joinStrings(profile.AllowedGroups),
		MinSize:       profile.MinSize,
		MaxSize:       profile.MaxSize,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	return table.Insert(p)
}

func joinStrings(s []string) string {
	return strings.Join(s, ",")
}

func (m *Monitor) Decisions(results []indexer.SearchResult) ([]indexer.SearchResult, error) {
	if len(results) == 0 {
		return nil, nil
	}

	type ScoredResult struct {
		res   indexer.SearchResult
		score int
	}

	var scored []ScoredResult
	for _, res := range results {
		if !m.isAllowed(res) {
			continue
		}
		scored = append(scored, ScoredResult{
			res:   res,
			score: m.calculateScore(res),
		})
	}

	// Sort by score descending
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	var filtered []indexer.SearchResult
	for _, s := range scored {
		filtered = append(filtered, s.res)
	}
	return filtered, nil
}

func (m *Monitor) calculateScore(res indexer.SearchResult) int {
	score := 0

	if m.profile == nil {
		return score
	}

	// Resolution score
	title := strings.ToLower(res.Title)
	if m.profile.PreferredRes != "" {
		if strings.Contains(title, strings.ToLower(m.profile.PreferredRes)) {
			score += 100
		}
	}

	// Codec preference (stub)
	if strings.Contains(title, "x265") || strings.Contains(title, "hevc") {
		score += 50
	}

	// Seeders score
	score += res.Seeders / 10

	return score
}

func (m *Monitor) isAllowed(res indexer.SearchResult) bool {
	if m.profile == nil {
		return true
	}

	if m.profile.MinSize > 0 && res.Size < m.profile.MinSize {
		return false
	}
	if m.profile.MaxSize > 0 && res.Size > m.profile.MaxSize {
		return false
	}

	if len(m.profile.AllowedRes) > 0 {
		allowed := false
		for _, r := range m.profile.AllowedRes {
			if strings.Contains(strings.ToLower(res.Title), strings.ToLower(r)) {
				allowed = true
				break
			}
		}
		if !allowed {
			return false
		}
	}

	return true
}
