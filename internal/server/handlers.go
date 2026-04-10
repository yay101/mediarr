package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/yay101/mediarr/internal/automation"
	"github.com/yay101/mediarr/internal/db"
	"github.com/yay101/mediarr/internal/indexer"
	"github.com/yay101/mediarr/internal/monitor"
	"github.com/yay101/mediarr/internal/search"
	"github.com/yay101/mediarr/internal/storage"
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
			"api_key":  user.APIKey,
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
			if albums, err := database.MusicAlbums(); err == nil {
				var list []db.MusicAlbum
				albums.Scan(func(a db.MusicAlbum) bool {
					list = append(list, a)
					return true
				})
				result["music"] = list
			}
			if books, err := database.Books(); err == nil {
				var list []db.Book
				books.Scan(func(b db.Book) bool {
					list = append(list, b)
					return true
				})
				result["books"] = list
			}
			if manga, err := database.Manga(); err == nil {
				var list []db.Manga
				manga.Scan(func(m db.Manga) bool {
					list = append(list, m)
					return true
				})
				result["manga"] = list
			}
		}
		writeJSON(w, result)
	case "movie":
		if database == nil {
			writeJSON(w, map[string]interface{}{"movies": []db.Movie{}})
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
		writeJSON(w, map[string]interface{}{"movies": list})
	case "tv":
		if database == nil {
			writeJSON(w, map[string]interface{}{"tv_shows": []db.TVShow{}})
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
		writeJSON(w, map[string]interface{}{"tv_shows": list})
	case "music":
		if database == nil {
			writeJSON(w, map[string]interface{}{"music": []db.MusicAlbum{}})
			return
		}
		albums, err := database.MusicAlbums()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		var list []db.MusicAlbum
		albums.Scan(func(a db.MusicAlbum) bool {
			list = append(list, a)
			return true
		})
		writeJSON(w, map[string]interface{}{"music": list})
	case "book":
		if database == nil {
			writeJSON(w, map[string]interface{}{"books": []db.Book{}})
			return
		}
		books, err := database.Books()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		var list []db.Book
		books.Scan(func(b db.Book) bool {
			list = append(list, b)
			return true
		})
		writeJSON(w, map[string]interface{}{"books": list})
	case "audiobook":
		if database == nil {
			writeJSON(w, map[string]interface{}{"audiobooks": []db.Audiobook{}})
			return
		}
		table, err := database.Audiobooks()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		var list []db.Audiobook
		table.Scan(func(a db.Audiobook) bool {
			if a.UserID == user.ID {
				list = append(list, a)
			}
			return true
		})
		writeJSON(w, map[string]interface{}{"audiobooks": list})
	case "manga":
		if database == nil {
			writeJSON(w, map[string]interface{}{"manga": []db.Manga{}})
			return
		}
		manga, err := database.Manga()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		var list []db.Manga
		manga.Scan(func(m db.Manga) bool {
			list = append(list, m)
			return true
		})
		writeJSON(w, map[string]interface{}{"manga": list})
	}
}

func (s *Server) handleAddMedia(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Type        string `json:"type"`
		Title       string `json:"title"`
		Year        uint16 `json:"year"`
		TMDBID      uint32 `json:"tmdb_id"`
		ExternalID  string `json:"external_id"`
		ExternalSrc string `json:"external_src"`
		Quality     string `json:"quality"`
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
			return (m.TMDBID == req.TMDBID || (req.ExternalSrc == "tmdb" && m.TMDBID != 0 && fmt.Sprintf("%d", m.TMDBID) == req.ExternalID)) && m.Status == db.MediaStatusAvailable
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
		if movie.TMDBID == 0 && req.ExternalSrc == "tmdb" {
			fmt.Sscanf(req.ExternalID, "%d", &movie.TMDBID)
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
			return (s.TMDBID == req.TMDBID || (req.ExternalSrc == "tmdb" && s.TMDBID != 0 && fmt.Sprintf("%d", s.TMDBID) == req.ExternalID)) && s.Status == db.MediaStatusAvailable
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
		if show.TMDBID == 0 && req.ExternalSrc == "tmdb" {
			fmt.Sscanf(req.ExternalID, "%d", &show.TMDBID)
		}
		id, err = table.Insert(show)
	case "music":
		table, err := database.MusicAlbums()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		album := &db.MusicAlbum{
			Title:         req.Title,
			Year:          req.Year,
			MusicBrainzID: req.ExternalID,
			Status:        db.MediaStatusQueued,
			AddedAt:       time.Now(),
			UpdatedAt:     time.Now(),
		}
		id, err = table.Insert(album)
	case "book":
		table, err := database.Books()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		book := &db.Book{
			Title:         req.Title,
			Year:          req.Year,
			OpenLibraryID: req.ExternalID,
			Status:        db.MediaStatusQueued,
			AddedAt:       time.Now(),
			UpdatedAt:     time.Now(),
		}
		id, err = table.Insert(book)
	case "audiobook":
		table, err := database.Audiobooks()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		audiobook := &db.Audiobook{
			UserID:    user.ID,
			Title:     req.Title,
			Author:    req.ExternalID,
			Year:      req.Year,
			ASIN:      req.ExternalID,
			Status:    db.MediaStatusQueued,
			AddedAt:   time.Now(),
			UpdatedAt: time.Now(),
		}
		id, err = table.Insert(audiobook)
	case "manga":
		table, err := database.Manga()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		manga := &db.Manga{
			Title:      req.Title,
			Year:       req.Year,
			MangaDexID: req.ExternalID,
			Status:     db.MediaStatusQueued,
			AddedAt:    time.Now(),
			UpdatedAt:  time.Now(),
		}
		id, err = table.Insert(manga)
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	userID := user.ID
	mediaType := req.Type
	title := req.Title
	year := req.Year
	quality := req.Quality
	go s.triggerAutoSearch(userID, mediaType, id, title, year, quality)

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

		// Load episodes
		episodesTable, err := database.TVEpisodes()
		var episodes []db.TVEpisode
		if err == nil {
			episodes, _ = episodesTable.Query("ShowID", show.ID)
		}

		writeJSON(w, map[string]interface{}{
			"show":     show,
			"episodes": episodes,
		})
	case "music":
		table, err := database.MusicAlbums()
		if err == nil {
			album, _ := table.Get(uint32(id))
			if album != nil {
				writeJSON(w, album)
				return
			}
		}
		http.Error(w, "music not found", http.StatusNotFound)
	case "book":
		table, err := database.Books()
		if err == nil {
			book, _ := table.Get(uint32(id))
			if book != nil {
				writeJSON(w, book)
				return
			}
		}
		http.Error(w, "book not found", http.StatusNotFound)
	case "audiobook":
		table, err := database.Audiobooks()
		if err == nil {
			ab, _ := table.Get(uint32(id))
			if ab != nil && ab.UserID == user.ID {
				writeJSON(w, ab)
				return
			}
		}
		http.Error(w, "audiobook not found", http.StatusNotFound)
	case "manga":
		table, err := database.Manga()
		if err == nil {
			manga, _ := table.Get(uint32(id))
			if manga != nil {
				writeJSON(w, manga)
				return
			}
		}
		http.Error(w, "manga not found", http.StatusNotFound)
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
	case "music", "musicalbum", "musicalbums":
		table, err := database.MusicAlbums()
		if err == nil {
			table.Delete(uint32(id))
		}
	case "book", "books":
		table, err := database.Books()
		if err == nil {
			table.Delete(uint32(id))
		}
	case "audiobook", "audiobooks":
		table, err := database.Audiobooks()
		if err == nil {
			ab, _ := table.Get(uint32(id))
			if ab != nil && ab.UserID == user.ID {
				table.Delete(uint32(id))
			}
		}
	case "manga":
		table, err := database.Manga()
		if err == nil {
			table.Delete(uint32(id))
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

	searchType := r.URL.Query().Get("type")
	if searchType == "" {
		searchType = "all"
	}

	cfg := s.app.Config()
	// 1. Try Metadata Search via mediarr-server
	if cfg.MetadataAPI.URL != "" && !strings.Contains(cfg.MetadataAPI.URL, "themoviedb.org") {
		results, err := s.searchMetadataServer(query, searchType)
		if err == nil && len(results) > 0 {
			writeJSON(w, map[string]interface{}{"type": "metadata", "results": results})
			return
		}
	}

	// 2. Fallback to direct TMDB if configured (backward compatibility)
	if cfg.MetadataAPI.APIKey != "" && cfg.MetadataAPI.APIKey != "YOUR_TMDB_API_KEY" && strings.Contains(cfg.MetadataAPI.URL, "themoviedb.org") {
		results, err := s.searchMetadata(query)
		if err == nil && len(results) > 0 {
			writeJSON(w, map[string]interface{}{"type": "metadata", "results": results})
			return
		}
	}

	// 3. Fallback to Indexer Search (Releases)
	automationMgr := s.app.Automation().(*automation.Manager)
	results, err := automationMgr.SearchIndexers(r.Context(), query)
	if err != nil {
		writeJSON(w, map[string]interface{}{"type": "releases", "results": []interface{}{}})
		return
	}

	if results == nil {
		results = []indexer.SearchResult{}
	}

	writeJSON(w, map[string]interface{}{"type": "releases", "results": results})
}

func (s *Server) searchMetadataServer(query string, searchType string) ([]interface{}, error) {
	cfg := s.app.Config()
	apiURL := cfg.MetadataAPI.URL

	var allResults []interface{}

	searchURL := fmt.Sprintf("%s/search?query=%s&type=%s", apiURL, url.QueryEscape(query), searchType)
	resp, err := http.Get(searchURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var apiResp struct {
		Success bool `json:"success"`
		Data    struct {
			Items []struct {
				Title       string `json:"title"`
				Year        int    `json:"year"`
				TMDBID      string `json:"tmdb_id"`
				ExternalID  string `json:"external_id"`
				ExternalSrc string `json:"external_src"`
				Type        string `json:"type"`
				Description string `json:"description"`
				Poster      *struct {
					URL string `json:"url"`
				} `json:"poster"`
				Rating *struct {
					Value float64 `json:"value"`
				} `json:"rating"`
			} `json:"items"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err == nil && apiResp.Success {
		for _, item := range apiResp.Data.Items {
			tmdbID := uint32(0)
			if item.TMDBID != "" {
				fmt.Sscanf(item.TMDBID, "%d", &tmdbID)
			}

			posterURL := ""
			if item.Poster != nil {
				posterURL = item.Poster.URL
			}

			rating := 0.0
			if item.Rating != nil {
				rating = item.Rating.Value
			}

			allResults = append(allResults, map[string]interface{}{
				"Type":        item.Type,
				"TMDBID":      tmdbID,
				"ExternalID":  item.ExternalID,
				"ExternalSrc": item.ExternalSrc,
				"Title":       item.Title,
				"Year":        uint16(item.Year),
				"Overview":    item.Description,
				"PosterURL":   posterURL,
				"VoteAverage": rating,
			})
		}
	}

	return allResults, nil
}

func (s *Server) searchMetadata(query string) ([]interface{}, error) {
	cfg := s.app.Config()
	apiKey := cfg.MetadataAPI.APIKey

	// TMDB Search Movie
	movieURL := fmt.Sprintf("https://api.themoviedb.org/3/search/movie?api_key=%s&query=%s", apiKey, url.QueryEscape(query))
	resp, err := http.Get(movieURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var movieData struct {
		Results []struct {
			ID          uint32  `json:"id"`
			Title       string  `json:"title"`
			ReleaseDate string  `json:"release_date"`
			Overview    string  `json:"overview"`
			PosterPath  string  `json:"poster_path"`
			VoteAverage float64 `json:"vote_average"`
		} `json:"results"`
	}
	json.NewDecoder(resp.Body).Decode(&movieData)

	var results []interface{}
	for _, m := range movieData.Results {
		year := uint16(0)
		if len(m.ReleaseDate) >= 4 {
			fmt.Sscanf(m.ReleaseDate[:4], "%d", &year)
		}
		results = append(results, map[string]interface{}{
			"Type":        "movie",
			"TMDBID":      m.ID,
			"Title":       m.Title,
			"Year":        year,
			"Overview":    m.Overview,
			"PosterURL":   "https://image.tmdb.org/t/p/w500" + m.PosterPath,
			"VoteAverage": m.VoteAverage,
		})
	}

	// TMDB Search TV
	tvURL := fmt.Sprintf("https://api.themoviedb.org/3/search/tv?api_key=%s&query=%s", apiKey, url.QueryEscape(query))
	respTV, err := http.Get(tvURL)
	if err == nil {
		defer respTV.Body.Close()
		var tvData struct {
			Results []struct {
				ID           uint32  `json:"id"`
				Name         string  `json:"name"`
				FirstAirDate string  `json:"first_air_date"`
				Overview     string  `json:"overview"`
				PosterPath   string  `json:"poster_path"`
				VoteAverage  float64 `json:"vote_average"`
			} `json:"results"`
		}
		json.NewDecoder(respTV.Body).Decode(&tvData)
		for _, t := range tvData.Results {
			year := uint16(0)
			if len(t.FirstAirDate) >= 4 {
				fmt.Sscanf(t.FirstAirDate[:4], "%d", &year)
			}
			results = append(results, map[string]interface{}{
				"Type":        "tv",
				"TMDBID":      t.ID,
				"Title":       t.Name,
				"Year":        year,
				"Overview":    t.Overview,
				"PosterURL":   "https://image.tmdb.org/t/p/w500" + t.PosterPath,
				"VoteAverage": t.VoteAverage,
			})
		}
	}

	return results, nil
}

func (s *Server) handleTriggerSearch(w http.ResponseWriter, r *http.Request) {
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

	var item monitor.WatchlistItem
	item.UserID = user.ID
	item.MediaID = uint32(id)

	switch mediaType {
	case "movie":
		item.MediaType = db.MediaTypeMovie
		table, _ := database.Movies()
		m, _ := table.Get(uint32(id))
		if m != nil {
			item.Title = m.Title
			item.Year = m.Year
			item.Quality = m.Quality
		}
	case "tv":
		item.MediaType = db.MediaTypeTV
		table, _ := database.TVShows()
		show, _ := table.Get(uint32(id))
		if show != nil {
			item.Title = show.Title
			item.Year = show.Year
		}
	case "episode":
		item.MediaType = db.MediaTypeTV
		epTable, _ := database.TVEpisodes()
		ep, _ := epTable.Get(uint32(id))
		if ep != nil {
			showTable, _ := database.TVShows()
			show, _ := showTable.Get(ep.ShowID)
			if show != nil {
				item.Title = fmt.Sprintf("%s S%02dE%02d", show.Title, ep.Season, ep.Episode)
				item.Year = show.Year
				item.MediaID = show.ID
			}
		}
	case "music":
		item.MediaType = db.MediaTypeMusic
		table, _ := database.MusicAlbums()
		album, _ := table.Get(uint32(id))
		if album != nil {
			item.Title = fmt.Sprintf("%s %s", album.Artist, album.Title)
			item.Year = album.Year
		}
	case "book":
		item.MediaType = db.MediaTypeBook
		table, _ := database.Books()
		book, _ := table.Get(uint32(id))
		if book != nil {
			item.Title = fmt.Sprintf("%s %s", book.Author, book.Title)
			item.Year = book.Year
		}
	case "audiobook":
		item.MediaType = db.MediaTypeAudiobook
		table, _ := database.Audiobooks()
		ab, _ := table.Get(uint32(id))
		if ab != nil {
			item.Title = fmt.Sprintf("%s %s", ab.Author, ab.Title)
			item.Year = ab.Year
		}
	case "manga":
		item.MediaType = db.MediaTypeManga
		table, _ := database.Manga()
		manga, _ := table.Get(uint32(id))
		if manga != nil {
			item.Title = manga.Title
			item.Year = manga.Year
		}
	}

	if item.Title == "" {
		http.Error(w, "item not found", http.StatusNotFound)
		return
	}

	go func() {
		automationMgr := s.app.Automation().(*automation.Manager)
		slog.Info("manual search triggered via API", "title", item.Title)
		automationMgr.SearchForItem(item)
	}()

	writeJSON(w, map[string]string{"status": "search triggered", "title": item.Title})
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
	var storageLocationID uint32

	movieTable, err := database.Movies()
	if err == nil {
		movie, _ := movieTable.Get(uint32(id))
		if movie != nil && movie.UserID == user.ID {
			filePath = movie.Path
			storageLocationID = movie.StorageLocationID
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
					storageLocationID = ep.StorageLocationID
				}
			}
		}
	}

	if filePath == "" {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	if storageMgr := s.app.Storage(); storageMgr != nil {
		storageManager, ok := storageMgr.(*storage.Manager)
		if ok && storageLocationID > 0 {
			loc, found := storageManager.GetLocation(storageLocationID)
			if found && loc.Type == "s3" {
				locID, key, err := storageManager.ParseVirtualPath(filePath)
				if err == nil && locID > 0 {
					backend, err := storageManager.GetBackendForLocation(loc)
					if err == nil {
						reader, err := backend.Read(r.Context(), key)
						if err != nil {
							http.Error(w, "failed to read from storage", http.StatusInternalServerError)
							return
						}
						defer reader.Close()

						if size, err := backend.GetSize(r.Context(), key); err == nil && size > 0 {
							w.Header().Set("Content-Type", "application/octet-stream")
							w.Header().Set("Content-Disposition", "attachment; filename="+filepath.Base(filePath))
							w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
							io.Copy(w, reader)
							return
						}
					}
				}
			}
		}
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

func (s *Server) handleVerifyMedia(w http.ResponseWriter, r *http.Request) {
	storageMgr := s.app.Storage()
	if storageMgr == nil {
		http.Error(w, "Storage not configured", http.StatusServiceUnavailable)
		return
	}

	storageManager, ok := storageMgr.(*storage.Manager)
	if !ok {
		http.Error(w, "Invalid storage manager", http.StatusInternalServerError)
		return
	}

	verifier := storage.NewVerifier(s.app.DB(), storageManager)
	results := verifier.VerifyAllMedia(r.Context())

	var successCount, failCount int
	for _, r := range results {
		if r.Success {
			successCount++
		} else {
			failCount++
		}
	}

	writeJSON(w, map[string]interface{}{
		"total":   len(results),
		"success": successCount,
		"failed":  failCount,
		"results": results,
	})
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

func (s *Server) triggerAutoSearch(userID uint32, mediaType string, mediaID uint32, title string, year uint16, quality string) {
	automationMgr := s.app.Automation().(*automation.Manager)

	item := monitor.WatchlistItem{
		UserID:  userID,
		MediaID: mediaID,
		Title:   title,
		Year:    year,
		Quality: quality,
	}

	switch mediaType {
	case "movie":
		item.MediaType = db.MediaTypeMovie
	case "tv":
		item.MediaType = db.MediaTypeTV
	case "music":
		item.MediaType = db.MediaTypeMusic
	case "book":
		item.MediaType = db.MediaTypeBook
	case "audiobook":
		item.MediaType = db.MediaTypeAudiobook
	case "manga":
		item.MediaType = db.MediaTypeManga
	default:
		slog.Warn("unknown media type for auto-search", "type", mediaType)
		return
	}

	slog.Info("auto-search triggered for new media", "type", mediaType, "title", title, "id", mediaID)
	automationMgr.SearchForItem(item)
}

func (s *Server) handleManualSearch(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Query   string `json:"query"`
		Type    string `json:"type"`
		MediaID uint32 `json:"media_id"`
		Quality string `json:"quality"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.Query == "" {
		http.Error(w, "query is required", http.StatusBadRequest)
		return
	}

	var mediaType db.MediaType
	switch req.Type {
	case "movie":
		mediaType = db.MediaTypeMovie
	case "tv":
		mediaType = db.MediaTypeTV
	case "music":
		mediaType = db.MediaTypeMusic
	case "book":
		mediaType = db.MediaTypeBook
	default:
		mediaType = db.MediaTypeMovie
	}

	searchMgr := s.app.Search()
	session, err := searchMgr.SearchAll(nil, req.Query, mediaType, req.MediaID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]interface{}{
		"session_id": session.ID,
		"query":      session.Query,
		"status":     "searching",
	})
}

func (s *Server) handleGetSearchResults(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	sessionID := r.PathValue("session_id")
	if sessionID == "" {
		http.Error(w, "session_id is required", http.StatusBadRequest)
		return
	}

	searchMgr := s.app.Search()
	session, found := searchMgr.GetSession(sessionID)
	if !found {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	response := map[string]interface{}{
		"session_id": session.ID,
		"query":      session.Query,
		"results":    session.Results,
		"created_at": session.CreatedAt,
	}

	writeJSON(w, response)
}

func (s *Server) handleDownloadSearchResult(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		SessionID string `json:"session_id"`
		ResultIdx int    `json:"result_index"`
		MediaID   uint32 `json:"media_id"`
		MediaType string `json:"media_type"`
		Title     string `json:"title"`
		Quality   string `json:"quality"`
		Force     bool   `json:"force"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	searchMgr := s.app.Search()
	session, found := searchMgr.GetSession(req.SessionID)
	if !found {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	if req.ResultIdx < 0 || req.ResultIdx >= len(session.Results) {
		http.Error(w, "invalid result index", http.StatusBadRequest)
		return
	}

	result := session.Results[req.ResultIdx]

	var mediaType db.MediaType
	switch req.MediaType {
	case "movie":
		mediaType = db.MediaTypeMovie
	case "tv":
		mediaType = db.MediaTypeTV
	case "music":
		mediaType = db.MediaTypeMusic
	case "book":
		mediaType = db.MediaTypeBook
	default:
		mediaType = db.MediaTypeMovie
	}

	downloadReq := &search.DownloadRequest{
		Result:    &result,
		MediaID:   req.MediaID,
		MediaType: mediaType,
		Title:     req.Title,
		Quality:   req.Quality,
		Force:     req.Force,
		UserID:    user.ID,
	}

	job, err := searchMgr.CreateDownloadJob(downloadReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	downloadMgr := s.app.Download()
	if err := downloadMgr.AddDownload(job); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	searchMgr.ClearSession(req.SessionID)

	writeJSON(w, map[string]interface{}{
		"job_id": job.ID,
		"status": "queued",
		"title":  result.Title,
		"force":  req.Force,
	})
}

func (s *Server) handleClearSearchSession(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	sessionID := r.PathValue("session_id")
	if sessionID == "" {
		http.Error(w, "session_id is required", http.StatusBadRequest)
		return
	}

	searchMgr := s.app.Search()
	searchMgr.ClearSession(sessionID)

	writeJSON(w, map[string]string{"status": "cleared"})
}

func (s *Server) handleSearchWebSocket(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	query := r.URL.Query().Get("q")
	if query == "" {
		http.Error(w, "query parameter 'q' is required", http.StatusBadRequest)
		return
	}

	mediaTypeStr := r.URL.Query().Get("type")
	mediaIDStr := r.URL.Query().Get("media_id")

	var mediaType db.MediaType
	switch mediaTypeStr {
	case "movie":
		mediaType = db.MediaTypeMovie
	case "tv":
		mediaType = db.MediaTypeTV
	case "music":
		mediaType = db.MediaTypeMusic
	case "book":
		mediaType = db.MediaTypeBook
	default:
		mediaType = db.MediaTypeMovie
	}

	var mediaID uint32
	if mediaIDStr != "" {
		if id, err := strconv.ParseUint(mediaIDStr, 10, 32); err == nil {
			mediaID = uint32(id)
		}
	}

	hub := s.app.SearchHub()
	if hub == nil {
		http.Error(w, "search hub not available", http.StatusServiceUnavailable)
		return
	}

	conn, err := websocket.Upgrade(w, r, nil, 0, 0)
	if err != nil {
		slog.Error("websocket upgrade failed", "error", err)
		return
	}

	go hub.HandleSearch(conn, query, mediaType, mediaID)
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
