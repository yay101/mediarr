package storage

import (
	"context"
	"log/slog"
	"sync"

	"github.com/yay101/mediarr/db"
)

type VerifyOptions struct {
	Parallelism int
}

type VerifyResult struct {
	MediaType  string
	ID         uint32
	Path       string
	Success    bool
	Error      string
	ActualSize int64
}

type Verifier struct {
	db   *db.Database
	mgr  *Manager
	opts VerifyOptions
	mu   sync.Mutex
	wg   sync.WaitGroup
}

func NewVerifier(db *db.Database, mgr *Manager) *Verifier {
	return &Verifier{
		db:  db,
		mgr: mgr,
		opts: VerifyOptions{
			Parallelism: 5,
		},
	}
}

func (v *Verifier) VerifyAllMedia(ctx context.Context) []VerifyResult {
	var results []VerifyResult

	movieResults := v.verifyMovies(ctx)
	results = append(results, movieResults...)

	episodeResults := v.verifyTVEpisodes(ctx)
	results = append(results, episodeResults...)

	return results
}

func (v *Verifier) verifyMovies(ctx context.Context) []VerifyResult {
	var results []VerifyResult

	table, err := v.db.Movies()
	if err != nil {
		slog.Error("failed to get movies table", "error", err)
		return results
	}

	movies, err := table.Filter(func(m db.Movie) bool {
		return m.Status == db.MediaStatusAvailable && m.Path != ""
	})
	if err != nil {
		slog.Error("failed to filter movies", "error", err)
		return results
	}

	for i := range movies {
		movie := &movies[i]
		result := v.verifyMovie(ctx, movie)
		results = append(results, result)
	}

	return results
}

func (v *Verifier) verifyTVEpisodes(ctx context.Context) []VerifyResult {
	var results []VerifyResult

	table, err := v.db.TVEpisodes()
	if err != nil {
		slog.Error("failed to get tv episodes table", "error", err)
		return results
	}

	episodes, err := table.Filter(func(e db.TVEpisode) bool {
		return e.Status == db.MediaStatusAvailable && e.Path != ""
	})
	if err != nil {
		slog.Error("failed to filter episodes", "error", err)
		return results
	}

	for i := range episodes {
		episode := &episodes[i]
		result := v.verifyTVEpisode(ctx, episode)
		results = append(results, result)
	}

	return results
}

func (v *Verifier) verifyMovie(ctx context.Context, movie *db.Movie) VerifyResult {
	if movie.StorageLocationID == 0 {
		return VerifyResult{
			MediaType: "movie",
			ID:        movie.ID,
			Path:      movie.Path,
			Success:   true,
			Error:     "local storage - skipped",
		}
	}

	loc, found := v.mgr.GetLocation(movie.StorageLocationID)
	if !found {
		return VerifyResult{
			MediaType: "movie",
			ID:        movie.ID,
			Path:      movie.Path,
			Success:   false,
			Error:     "storage location not found",
		}
	}

	backend, err := v.mgr.GetBackendForLocation(loc)
	if err != nil {
		return VerifyResult{
			MediaType: "movie",
			ID:        movie.ID,
			Path:      movie.Path,
			Success:   false,
			Error:     err.Error(),
		}
	}

	_, key, err := v.mgr.ParseVirtualPath(movie.Path)
	if err != nil {
		return VerifyResult{
			MediaType: "movie",
			ID:        movie.ID,
			Path:      movie.Path,
			Success:   false,
			Error:     "failed to parse path",
		}
	}

	exists, err := backend.Exists(ctx, key)
	if err != nil || !exists {
		return VerifyResult{
			MediaType: "movie",
			ID:        movie.ID,
			Path:      movie.Path,
			Success:   false,
			Error:     "file not found in storage",
		}
	}

	size, err := backend.GetSize(ctx, key)
	if err != nil {
		return VerifyResult{
			MediaType: "movie",
			ID:        movie.ID,
			Path:      movie.Path,
			Success:   false,
			Error:     "failed to get size: " + err.Error(),
		}
	}

	if movie.Size != uint64(size) {
		slog.Info("size mismatch, updating", "movie", movie.Title, "old", movie.Size, "new", size)
		movie.Size = uint64(size)
		table, _ := v.db.Movies()
		if table != nil {
			table.Update(movie.ID, movie)
		}
	}

	return VerifyResult{
		MediaType:  "movie",
		ID:         movie.ID,
		Path:       movie.Path,
		Success:    true,
		ActualSize: size,
	}
}

func (v *Verifier) verifyTVEpisode(ctx context.Context, episode *db.TVEpisode) VerifyResult {
	if episode.StorageLocationID == 0 {
		return VerifyResult{
			MediaType: "tv_episode",
			ID:        episode.ID,
			Path:      episode.Path,
			Success:   true,
			Error:     "local storage - skipped",
		}
	}

	loc, found := v.mgr.GetLocation(episode.StorageLocationID)
	if !found {
		return VerifyResult{
			MediaType: "tv_episode",
			ID:        episode.ID,
			Path:      episode.Path,
			Success:   false,
			Error:     "storage location not found",
		}
	}

	backend, err := v.mgr.GetBackendForLocation(loc)
	if err != nil {
		return VerifyResult{
			MediaType: "tv_episode",
			ID:        episode.ID,
			Path:      episode.Path,
			Success:   false,
			Error:     err.Error(),
		}
	}

	_, key, err := v.mgr.ParseVirtualPath(episode.Path)
	if err != nil {
		return VerifyResult{
			MediaType: "tv_episode",
			ID:        episode.ID,
			Path:      episode.Path,
			Success:   false,
			Error:     "failed to parse path",
		}
	}

	exists, err := backend.Exists(ctx, key)
	if err != nil || !exists {
		return VerifyResult{
			MediaType: "tv_episode",
			ID:        episode.ID,
			Path:      episode.Path,
			Success:   false,
			Error:     "file not found in storage",
		}
	}

	size, err := backend.GetSize(ctx, key)
	if err != nil {
		return VerifyResult{
			MediaType: "tv_episode",
			ID:        episode.ID,
			Path:      episode.Path,
			Success:   false,
			Error:     "failed to get size: " + err.Error(),
		}
	}

	if episode.Size != uint64(size) {
		slog.Info("size mismatch, updating", "episode", episode.ID, "old", episode.Size, "new", size)
		episode.Size = uint64(size)
		table, _ := v.db.TVEpisodes()
		if table != nil {
			table.Update(episode.ID, episode)
		}
	}

	return VerifyResult{
		MediaType:  "tv_episode",
		ID:         episode.ID,
		Path:       episode.Path,
		Success:    true,
		ActualSize: size,
	}
}
