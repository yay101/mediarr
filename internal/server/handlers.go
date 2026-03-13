package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/yay101/mediarr/internal/automation"
	"github.com/yay101/mediarr/internal/db"
	"github.com/yay101/mediarr/internal/indexer"
	"github.com/yay101/mediarr/internal/subtitles"
)

func (s *Server) handleListSubtitles(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	mTypeStr := r.PathValue("type")
	idStr := r.PathValue("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var mType db.MediaType
	switch mTypeStr {
	case "movie":
		mType = db.MediaTypeMovie
	case "tv", "episode":
		mType = db.MediaTypeTV
	default:
		http.Error(w, "invalid media type", http.StatusBadRequest)
		return
	}

	// Verify ownership/existence
	database := s.app.DB()
	if database == nil {
		http.Error(w, "database not available", http.StatusServiceUnavailable)
		return
	}

	if mType == db.MediaTypeMovie {
		table, _ := database.Movies()
		m, _ := table.Get(uint32(id))
		if m == nil || m.UserID != user.ID {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
	} else {
		table, _ := database.TVEpisodes()
		ep, _ := table.Get(uint32(id))
		if ep == nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		showTable, _ := database.TVShows()
		show, _ := showTable.Get(ep.ShowID)
		if show == nil || show.UserID != user.ID {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
	}

	subDownloader := s.app.Subtitles().(*subtitles.Downloader)
	subs, err := subDownloader.ScanForSubtitles(mType, uint32(id))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]interface{}{"subtitles": subs})
}

func (s *Server) handleDownloadSubtitles(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	mTypeStr := r.PathValue("type")
	idStr := r.PathValue("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	var req struct {
		Language string `json:"language"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var mType db.MediaType
	switch mTypeStr {
	case "movie":
		mType = db.MediaTypeMovie
	case "tv", "episode":
		mType = db.MediaTypeTV
	default:
		http.Error(w, "invalid media type", http.StatusBadRequest)
		return
	}

	subDownloader := s.app.Subtitles().(*subtitles.Downloader)
	err = subDownloader.DownloadSubtitles(mType, uint32(id), req.Language)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]string{"status": "search triggered"})
}

func (s *Server) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	user, err := s.getCurrentUser(r)
	if err != nil || user == nil {
		writeJSON(w, map[string]interface{}{
			"authenticated": false,
			"user":          nil,
		})
		return
	}

	role := "user"
	if user.Role == db.RoleAdmin {
		role = "admin"
	}

	writeJSON(w, map[string]interface{}{
		"authenticated": true,
		"user": map[string]interface{}{
			"id":       user.ID,
			"username": user.Username,
			"role":     role,
		},
	})
}

func (s *Server) handleListMedia(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	mediaType := r.URL.Query().Get("type")
	database := s.app.DB()

	switch mediaType {
	case "", "all":
		var result map[string]interface{} = make(map[string]interface{})
		if database != nil {
			if movies, err := database.Movies(); err == nil {
				var list []db.Movie
				movies.Scan(func(m db.Movie) bool {
					if m.UserID == user.ID {
						list = append(list, m)
					}
					return true
				})
				result["movies"] = list
			}
			if tvshows, err := database.TVShows(); err == nil {
				var list []db.TVShow
				tvshows.Scan(func(s db.TVShow) bool {
					if s.UserID == user.ID {
						list = append(list, s)
					}
					return true
				})
				result["tv_shows"] = list
			}
		}
		writeJSON(w, result)
	case "movie":
		if database == nil {
			writeJSON(w, []interface{}{})
			return
		}
		movies, err := database.Movies()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		var list []db.Movie
		movies.Scan(func(m db.Movie) bool {
			if m.UserID == user.ID {
				list = append(list, m)
			}
			return true
		})
		writeJSON(w, list)
	case "tv":
		if database == nil {
			writeJSON(w, []interface{}{})
			return
		}
		tvshows, err := database.TVShows()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		var list []db.TVShow
		tvshows.Scan(func(s db.TVShow) bool {
			if s.UserID == user.ID {
				list = append(list, s)
			}
			return true
		})
		writeJSON(w, list)
	}
}

func (s *Server) handleAddMedia(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Type    string `json:"type"`
		Title   string `json:"title"`
		Year    uint16 `json:"year"`
		TMDBID  uint32 `json:"tmdb_id"`
		Quality string `json:"quality"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	database := s.app.DB()
	if database == nil {
		http.Error(w, "database not available", http.StatusServiceUnavailable)
		return
	}

	var id uint32
	var err error

	switch req.Type {
	case "movie":
		table, err := database.Movies()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Check for existing movie globally (Shared Pool Path 1)
		status := db.MediaStatusQueued
		path := ""
		existing, _ := table.Filter(func(m db.Movie) bool {
			return m.TMDBID == req.TMDBID && m.Status == db.MediaStatusAvailable
		})
		if len(existing) > 0 {
			status = db.MediaStatusAvailable
			path = existing[0].Path
		}

		movie := &db.Movie{
			UserID:    user.ID,
			Title:     req.Title,
			Year:      req.Year,
			TMDBID:    req.TMDBID,
			Status:    status,
			Path:      path,
			Quality:   req.Quality,
			AddedAt:   time.Now(),
			UpdatedAt: time.Now(),
		}
		id, err = table.Insert(movie)
	case "tv":
		table, err := database.TVShows()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Check for existing show globally (Shared Pool Path 1)
		status := db.MediaStatusQueued
		existing, _ := table.Filter(func(s db.TVShow) bool {
			return s.TMDBID == req.TMDBID && s.Status == db.MediaStatusAvailable
		})
		if len(existing) > 0 {
			status = db.MediaStatusAvailable
		}

		show := &db.TVShow{
			UserID:    user.ID,
			Title:     req.Title,
			Year:      req.Year,
			TMDBID:    req.TMDBID,
			Status:    status,
			Monitored: true,
			AddedAt:   time.Now(),
			UpdatedAt: time.Now(),
		}
		id, err = table.Insert(show)
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]interface{}{"id": id, "status": "added"})
}

func (s *Server) handleGetMedia(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	mediaType := r.PathValue("type")
	idStr := r.PathValue("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	database := s.app.DB()
	if database == nil {
		http.Error(w, "database not available", http.StatusServiceUnavailable)
		return
	}

	switch mediaType {
	case "movie":
		table, err := database.Movies()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		movie, err := table.Get(uint32(id))
		if err != nil || movie.UserID != user.ID {
			http.Error(w, "movie not found", http.StatusNotFound)
			return
		}
		writeJSON(w, movie)
	case "tv":
		table, err := database.TVShows()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		show, err := table.Get(uint32(id))
		if err != nil || show.UserID != user.ID {
			http.Error(w, "show not found", http.StatusNotFound)
			return
		}
		writeJSON(w, show)
	}
}

func (s *Server) handleDeleteMedia(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	mediaType := r.PathValue("type")
	idStr := r.PathValue("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	database := s.app.DB()
	if database == nil {
		http.Error(w, "database not available", http.StatusServiceUnavailable)
		return
	}

	switch mediaType {
	case "movie", "movies":
		table, err := database.Movies()
		if err == nil {
			movie, _ := table.Get(uint32(id))
			if movie != nil && movie.UserID == user.ID {
				table.Delete(uint32(id))
			}
		}
	case "tv", "tvshow", "tvshows":
		table, err := database.TVShows()
		if err == nil {
			show, _ := table.Get(uint32(id))
			if show != nil && show.UserID == user.ID {
				table.Delete(uint32(id))
			}
		}
	}

	writeJSON(w, map[string]string{"status": "deleted"})
}

func (s *Server) handleGetCalendar(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	database := s.app.DB()
	if database == nil {
		writeJSON(w, []interface{}{})
		return
	}

	episodesTable, err := database.TVEpisodes()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	showTable, err := database.TVShows()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Find shows belonging to user
	var userShows = make(map[uint32]bool)
	showTable.Scan(func(s db.TVShow) bool {
		if s.UserID == user.ID {
			userShows[s.ID] = true
		}
		return true
	})

	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")

	var start, end time.Time
	if startStr != "" {
		start, _ = time.Parse(time.RFC3339, startStr)
	} else {
		start = time.Now().AddDate(0, 0, -7)
	}
	if endStr != "" {
		end, _ = time.Parse(time.RFC3339, endStr)
	} else {
		end = time.Now().AddDate(0, 0, 30)
	}

	var episodes []db.TVEpisode
	episodesTable.Scan(func(ep db.TVEpisode) bool {
		if userShows[ep.ShowID] && ep.AirDate.After(start) && ep.AirDate.Before(end) {
			episodes = append(episodes, ep)
		}
		return true
	})

	writeJSON(w, episodes)
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		http.Error(w, "missing query parameter 'q'", http.StatusBadRequest)
		return
	}

	automationMgr := s.app.Automation().(*automation.Manager)
	results, err := automationMgr.SearchIndexers(r.Context(), query)
	if err != nil {
		writeJSON(w, []interface{}{})
		return
	}

	if results == nil {
		results = []indexer.SearchResult{}
	}

	writeJSON(w, results)
}

func (s *Server) handleTriggerSearch(w http.ResponseWriter, r *http.Request) {
	mediaType := r.PathValue("type")
	idStr := r.PathValue("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	database := s.app.DB()
	if database == nil {
		http.Error(w, "database not available", http.StatusServiceUnavailable)
		return
	}

	var title string
	switch mediaType {
	case "movie":
		table, _ := database.Movies()
		m, _ := table.Get(uint32(id))
		if m != nil {
			title = m.Title
		}
	case "tv", "episode":
		table, _ := database.TVEpisodes()
		ep, _ := table.Get(uint32(id))
		if ep != nil {
			showTable, _ := database.TVShows()
			show, _ := showTable.Get(ep.ShowID)
			if show != nil {
				title = fmt.Sprintf("%s S%02dE%02d", show.Title, ep.Season, ep.Episode)
			}
		}
	}

	if title == "" {
		http.Error(w, "item not found", http.StatusNotFound)
		return
	}

	go func() {
		slog.Info("manual search triggered via API", "title", title)
	}()

	writeJSON(w, map[string]string{"status": "search triggered", "title": title})
}

func (s *Server) handleListRSSFeeds(w http.ResponseWriter, r *http.Request) {
	database := s.app.DB()
	if database == nil {
		writeJSON(w, []interface{}{})
		return
	}

	table, err := database.RSSFeeds()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var feeds []db.RSSFeed
	table.Scan(func(f db.RSSFeed) bool {
		feeds = append(feeds, f)
		return true
	})
	writeJSON(w, feeds)
}

func (s *Server) handleAddRSSFeed(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name      string `json:"name"`
		URL       string `json:"url"`
		Interval  uint32 `json:"interval"`
		MediaType string `json:"media_type"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	database := s.app.DB()
	if database == nil {
		http.Error(w, "database not available", http.StatusServiceUnavailable)
		return
	}

	table, err := database.RSSFeeds()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	feed := &db.RSSFeed{
		Name:      req.Name,
		URL:       req.URL,
		Interval:  req.Interval,
		Enabled:   true,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	id, err := table.Insert(feed)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]interface{}{"id": id, "status": "added"})
}

func (s *Server) handleRemoveRSSFeed(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	database := s.app.DB()
	if database == nil {
		http.Error(w, "database not available", http.StatusServiceUnavailable)
		return
	}

	table, err := database.RSSFeeds()
	if err == nil {
		table.Delete(uint32(id))
	}

	writeJSON(w, map[string]string{"status": "removed"})
}

func (s *Server) handleListWatchlist(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	database := s.app.DB()
	if database == nil {
		writeJSON(w, []interface{}{})
		return
	}

	table, err := database.Watchlist()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var items []db.WatchlistItem
	table.Scan(func(item db.WatchlistItem) bool {
		if item.UserID == user.ID {
			items = append(items, item)
		}
		return true
	})
	writeJSON(w, items)
}

func (s *Server) handleAddWatchlist(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		MediaType string `json:"media_type"`
		Title     string `json:"title"`
		Year      uint16 `json:"year"`
		Quality   string `json:"quality"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	database := s.app.DB()
	if database == nil {
		http.Error(w, "database not available", http.StatusServiceUnavailable)
		return
	}

	table, err := database.Watchlist()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	item := &db.WatchlistItem{
		UserID:    user.ID,
		Title:     req.Title,
		Year:      req.Year,
		Quality:   req.Quality,
		Status:    db.MediaStatusQueued,
		AddedAt:   time.Now(),
		UpdatedAt: time.Now(),
	}

	id, err := table.Insert(item)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]interface{}{"id": id, "status": "added"})
}

func (s *Server) handleRemoveWatchlist(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	database := s.app.DB()
	if database == nil {
		http.Error(w, "database not available", http.StatusServiceUnavailable)
		return
	}

	table, err := database.Watchlist()
	if err == nil {
		table.Delete(uint32(id))
	}

	writeJSON(w, map[string]string{"status": "removed"})
}

func (s *Server) handleListDownloads(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	database := s.app.DB()
	if database == nil {
		writeJSON(w, []interface{}{})
		return
	}

	table, err := database.Downloads()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var jobs []db.DownloadJob
	table.Scan(func(job db.DownloadJob) bool {
		if job.UserID == user.ID {
			jobs = append(jobs, job)
		}
		return true
	})
	writeJSON(w, jobs)
}

func (s *Server) handleAddDownload(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Type      string `json:"type"`
		MediaType string `json:"media_type"`
		MediaID   uint32 `json:"media_id"`
		Title     string `json:"title"`
		Data      string `json:"data"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	database := s.app.DB()
	if database == nil {
		http.Error(w, "database not available", http.StatusServiceUnavailable)
		return
	}

	table, err := database.Downloads()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var provider db.DownloadProvider
	var downloadData string

	switch req.Type {
	case "torrent", "magnet":
		provider = db.DownloadProviderTorrent
		downloadData = req.Data
	case "nzb":
		provider = db.DownloadProviderUsenet
		downloadData = req.Data
	default:
		http.Error(w, "invalid download type", http.StatusBadRequest)
		return
	}

	mType := db.MediaTypeMovie
	switch req.MediaType {
	case "movie":
		mType = db.MediaTypeMovie
	case "tv", "episode":
		mType = db.MediaTypeTV
	}

	now := time.Now()
	job := &db.DownloadJob{
		UserID:    user.ID,
		MediaType: mType,
		MediaID:   req.MediaID,
		Title:     req.Title,
		Provider:  provider,
		InfoHash:  downloadData,
		MagnetURI: downloadData,
		NZBData:   downloadData,
		Status:    db.DownloadStatusQueued,
		CreatedAt: now,
		UpdatedAt: now,
	}

	id, err := table.Insert(job)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]interface{}{
		"id":     id,
		"status": "queued",
		"type":   req.Type,
	})
}

func (s *Server) handleCancelDownload(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	// Stub
	writeJSON(w, map[string]string{"status": "cancelled", "id": strconv.FormatUint(id, 10)})
}

func (s *Server) handlePauseDownload(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"status": "paused"})
}

func (s *Server) handleResumeDownload(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"status": "resumed"})
}

func (s *Server) handleServeFile(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Access denied", http.StatusForbidden)
}

func (s *Server) handleStreamFile(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	database := s.app.DB()
	if database == nil {
		http.Error(w, "database not available", http.StatusServiceUnavailable)
		return
	}

	var filePath string
	movieTable, err := database.Movies()
	if err == nil {
		movie, _ := movieTable.Get(uint32(id))
		if movie != nil && movie.UserID == user.ID {
			filePath = movie.Path
		}
	}

	if filePath == "" {
		epTable, err := database.TVEpisodes()
		if err == nil {
			ep, _ := epTable.Get(uint32(id))
			if ep != nil {
				showTable, _ := database.TVShows()
				show, _ := showTable.Get(ep.ShowID)
				if show != nil && show.UserID == user.ID {
					filePath = ep.Path
				}
			}
		}
	}

	if filePath == "" {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	http.ServeFile(w, r, filePath)
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	cfg := s.app.Config()
	settings := map[string]interface{}{
		"server": map[string]interface{}{
			"host": cfg.Server.Host,
			"port": cfg.Server.Port,
			"url":  cfg.Server.URL,
		},
		"download": map[string]interface{}{
			"path":      cfg.Download.Path,
			"temp_path": cfg.Download.TempPath,
		},
		"auth": map[string]interface{}{
			"oidc_enabled": cfg.Auth.OIDC.Enabled,
		},
	}
	writeJSON(w, settings)
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"status": "updated"})
}

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, []interface{}{})
}

func (s *Server) handleKillTask(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"status": "killed"})
}

func (s *Server) handleReloadConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"status": "reloaded"})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	// Local login stub
	writeJSON(w, map[string]interface{}{"success": false, "error": "Use OIDC"})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie := &http.Cookie{
		Name:     "mediarr_session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	}
	http.SetCookie(w, cookie)
	writeJSON(w, map[string]string{"status": "logged out"})
}

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if s.templates == nil {
		http.Error(w, "Templates not loaded", http.StatusInternalServerError)
		return
	}

	data := map[string]interface{}{
		"Providers": []interface{}{},
	}

	if s.oidcClient != nil {
		var providerList []interface{}
		for _, p := range s.oidcClient.LibClient.Config.Providers {
			providerList = append(providerList, map[string]string{
				"ID":   p.Id,
				"Name": p.Name,
			})
		}
		data["Providers"] = providerList
	}

	err := s.templates.ExecuteTemplate(w, "login", data)
	if err != nil {
		slog.Error("failed to render login template", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
