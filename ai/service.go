package ai

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/yay101/mediarr/config"
)

type TaskType string

const (
	TaskSearchRefine   TaskType = "search_refine"
	TaskFileCheck      TaskType = "file_check"
	TaskMetadataEnrich TaskType = "metadata_enrich"
	TaskNaturalSearch  TaskType = "natural_search"
	TaskAlbumVerify    TaskType = "album_verify"
	TaskDidYouMean     TaskType = "did_you_mean"
)

type Service struct {
	cfg      *config.AIConfig
	provider Provider
}

func NewService(cfg *config.AIConfig) (*Service, error) {
	if !cfg.Enabled {
		return &Service{cfg: cfg, provider: nil}, nil
	}

	provider, err := Create(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create AI provider: %w", err)
	}

	return &Service{
		cfg:      cfg,
		provider: provider,
	}, nil
}

func (s *Service) IsEnabled() bool {
	return s.cfg != nil && s.cfg.Enabled && s.provider != nil
}

func (s *Service) ProviderName() string {
	if s.provider == nil {
		return ""
	}
	return s.provider.Name()
}

type SearchRefineInput struct {
	Results   []SearchResult `json:"results"`
	Query     string         `json:"query"`
	MediaType string         `json:"media_type"`
}

type SearchResult struct {
	Title   string `json:"title"`
	Size    int64  `json:"size"`
	Seeders int    `json:"seeders"`
	Quality string `json:"quality"`
	Indexer string `json:"indexer"`
}

type SearchRefineOutput struct {
	Selected   int     `json:"selected"`
	Reason     string  `json:"reason"`
	Confidence float64 `json:"confidence"`
}

func (s *Service) SearchRefine(ctx context.Context, input SearchRefineInput) (*SearchRefineOutput, error) {
	if !s.IsEnabled() {
		return nil, fmt.Errorf("AI not enabled")
	}

	resultsJSON, _ := json.MarshalIndent(input.Results, "", "  ")
	prompt := fmt.Sprintf(`You are a media release recommendation expert. Analyze these search results and select the best one.
Query: %s
Media Type: %s
Results:
%s

Respond with ONLY valid JSON:
{"selected": <index>, "reason": "<brief explanation>", "confidence": <0.0-1.0>}`, input.Query, input.MediaType, string(resultsJSON))

	resp, err := s.provider.Chat(ctx, []Message{
		{Role: "user", Content: prompt},
	})
	if err != nil {
		return nil, err
	}

	var out SearchRefineOutput
	if err := json.Unmarshal([]byte(resp.Content), &out); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &out, nil
}

type FileCheckInput struct {
	FilePath    string `json:"file_path"`
	MediaTitle  string `json:"media_title"`
	MediaType   string `json:"media_type"`
	Quality     string `json:"quality"`
	ReleaseName string `json:"release_name"`
}

type FileCheckOutput struct {
	Valid      bool    `json:"valid"`
	Issue      string  `json:"issue,omitempty"`
	Confidence float64 `json:"confidence"`
}

func (s *Service) FileCheck(ctx context.Context, input FileCheckInput) (*FileCheckOutput, error) {
	if !s.IsEnabled() {
		return nil, fmt.Errorf("AI not enabled")
	}

	prompt := fmt.Sprintf(`Verify if the downloaded file matches expected media.
File: %s
Expected: %s (%s) - %s
Release: %s

Respond with ONLY valid JSON:
{"valid": true/false, "issue": "<issue if invalid>", "confidence": <0.0-1.0>}`, input.FilePath, input.MediaTitle, input.MediaType, input.Quality, input.ReleaseName)

	resp, err := s.provider.Chat(ctx, []Message{
		{Role: "user", Content: prompt},
	})
	if err != nil {
		return nil, err
	}

	var out FileCheckOutput
	if err := json.Unmarshal([]byte(resp.Content), &out); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &out, nil
}

type MetadataEnrichInput struct {
	TMDBID    uint32 `json:"tmdb_id"`
	MediaType string `json:"media_type"`
	Title     string `json:"title"`
	Year      uint16 `json:"year"`
}

type MetadataEnrichOutput struct {
	Plot     string   `json:"plot"`
	Genres   []string `json:"genres"`
	Cast     []string `json:"cast"`
	Director string   `json:"director"`
	Rating   float64  `json:"rating"`
}

func (s *Service) MetadataEnrich(ctx context.Context, input MetadataEnrichInput) (*MetadataEnrichOutput, error) {
	if !s.IsEnabled() {
		return nil, fmt.Errorf("AI not enabled")
	}

	prompt := fmt.Sprintf(`Enrich metadata for this media item.
TMDB ID: %d
Type: %s
Title: %s (%d)

Respond with ONLY valid JSON:
{"plot": "...", "genres": [...], "cast": [...], "director": "...", "rating": <0-10>}`, input.TMDBID, input.MediaType, input.Title, input.Year)

	resp, err := s.provider.ChatJSON(ctx, []Message{
		{Role: "user", Content: prompt},
	}, MetadataEnrichOutput{})
	if err != nil {
		return nil, err
	}

	var out MetadataEnrichOutput
	if err := json.Unmarshal([]byte(resp.Content), &out); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &out, nil
}

type NaturalSearchInput struct {
	Query string `json:"query"`
}

type NaturalSearchOutput struct {
	SearchQuery string  `json:"search_query"`
	MediaType   string  `json:"media_type"`
	Quality     string  `json:"quality,omitempty"`
	Year        *uint16 `json:"year,omitempty"`
	DidYouMean  string  `json:"did_you_mean,omitempty"`
}

func (s *Service) NaturalSearch(ctx context.Context, input NaturalSearchInput) (*NaturalSearchOutput, error) {
	if !s.IsEnabled() {
		return nil, fmt.Errorf("AI not enabled")
	}

	prompt := fmt.Sprintf(`Parse this natural language search query into structured fields.
Query: %s

Respond with ONLY valid JSON:
{"search_query": "...", "media_type": "movie/tv/music/book", "quality": "720p/1080p/4k", "year": <year or null>, "did_you_mean": "<typo fix if any>"}`, input.Query)

	resp, err := s.provider.ChatJSON(ctx, []Message{
		{Role: "user", Content: prompt},
	}, NaturalSearchOutput{})
	if err != nil {
		return nil, err
	}

	var out NaturalSearchOutput
	if err := json.Unmarshal([]byte(resp.Content), &out); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &out, nil
}

type AlbumVerifyInput struct {
	Album     string   `json:"album"`
	Artist    string   `json:"artist"`
	Year      uint16   `json:"year"`
	Tracklist []string `json:"tracklist"`
}

type AlbumVerifyOutput struct {
	Valid   bool   `json:"valid"`
	Issue   string `json:"issue,omitempty"`
	Matched string `json:"matched_to,omitempty"`
}

func (s *Service) AlbumVerify(ctx context.Context, input AlbumVerifyInput) (*AlbumVerifyOutput, error) {
	if !s.IsEnabled() {
		return nil, fmt.Errorf("AI not enabled")
	}

	tracksJSON, _ := json.Marshal(input.Tracklist)
	prompt := fmt.Sprintf(`Verify if these track names match the album.
Album: %s - %s (%d)
Tracks: %s

Respond with ONLY valid JSON:
{"valid": true/false, "issue": "<issue if invalid>", "matched_to": "<actual album if different>"}`, input.Artist, input.Album, input.Year, string(tracksJSON))

	resp, err := s.provider.Chat(ctx, []Message{
		{Role: "user", Content: prompt},
	})
	if err != nil {
		return nil, err
	}

	var out AlbumVerifyOutput
	if err := json.Unmarshal([]byte(resp.Content), &out); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &out, nil
}

type DidYouMeanInput struct {
	Query string `json:"query"`
	Type  string `json:"type"` // "artist", "album", "movie", "tv"
}

type DidYouMeanOutput struct {
	Original  string `json:"original"`
	Corrected string `json:"corrected"`
}

func (s *Service) DidYouMean(ctx context.Context, input DidYouMeanInput) (*DidYouMeanOutput, error) {
	if !s.IsEnabled() {
		return nil, fmt.Errorf("AI not enabled")
	}

	prompt := fmt.Sprintf(`Did you mean? Fix any obvious typos in this %s query.
Query: %s

Respond with ONLY valid JSON:
{"original": "%s", "corrected": "<corrected or same>"}`, input.Type, input.Query, input.Query)

	resp, err := s.provider.ChatJSON(ctx, []Message{
		{Role: "user", Content: prompt},
	}, DidYouMeanOutput{})
	if err != nil {
		return nil, err
	}

	var out DidYouMeanOutput
	if err := json.Unmarshal([]byte(resp.Content), &out); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &out, nil
}
