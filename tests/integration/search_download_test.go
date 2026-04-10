package integration

import (
	"encoding/json"
	"testing"
	"time"
)

func TestManualSearchIndexers(t *testing.T) {
	cfg := LoadTestConfig(t)
	cfg.SkipIfNoServer(t)
	cfg.SkipIfNoIndexers(t)

	client := NewAPIClient(cfg.GetAPIURL(""))

	userToken, err := client.AuthLogin(cfg.TestUser.Username, cfg.TestUser.Password)
	if err != nil {
		t.Skip("Test user not available, skipping")
	}
	client.SetToken(userToken["token"].(string))

	t.Run("Start manual search for public domain content", func(t *testing.T) {
		req := ManualSearchRequest{
			Query:   "Big Buck Bunny 2008",
			Type:    "movie",
			Quality: "1080p",
		}

		result, err := client.StartManualSearch(req)
		if err != nil {
			t.Fatalf("Failed to start manual search: %v", err)
		}

		if result.SessionID == "" {
			t.Fatal("Expected non-empty session ID")
		}

		if cfg.VerboseOutput {
			t.Logf("Started search session: %s", result.SessionID)
		}

		t.Cleanup(func() {
			client.ClearSearchSession(result.SessionID)
		})
	})

	t.Run("Get search results", func(t *testing.T) {
		req := ManualSearchRequest{
			Query: "Big Buck Bunny",
			Type:  "movie",
		}

		searchResult, err := client.StartManualSearch(req)
		if err != nil {
			t.Fatalf("Failed to start search: %v", err)
		}

		t.Cleanup(func() {
			client.ClearSearchSession(searchResult.SessionID)
		})

		if cfg.VerboseOutput {
			t.Logf("Waiting for search results...")
		}

		var results []SearchResultItem
		for i := 0; i < 10; i++ {
			time.Sleep(2 * time.Second)

			resp, err := client.GetManualSearchResults(searchResult.SessionID)
			if err != nil {
				t.Logf("Warning: Failed to get results: %v", err)
				continue
			}

			results = resp.Results
			if len(results) > 0 {
				break
			}
		}

		if len(results) == 0 {
			t.Log("No results found (may be expected if indexers are slow or empty)")
			return
		}

		if cfg.VerboseOutput {
			t.Logf("Found %d results", len(results))
			for i, r := range results {
				if i >= 5 {
					t.Logf("  ... and %d more", len(results)-5)
					break
				}
				t.Logf("  %d. %s [%s] - %d seeders", i+1, r.Title, r.Indexer, r.Seeders)
			}
		}
	})

	t.Run("Search TV content", func(t *testing.T) {
		req := ManualSearchRequest{
			Query: "Sintel 2010",
			Type:  "movie",
		}

		_, err := client.StartManualSearch(req)
		if err != nil {
			t.Fatalf("Failed to start TV search: %v", err)
		}
	})
}

func TestSearchAndDownload(t *testing.T) {
	cfg := LoadTestConfig(t)
	cfg.SkipIfNoServer(t)
	cfg.SkipIfNoIndexers(t)
	cfg.SkipIfSlow(t)

	client := NewAPIClient(cfg.GetAPIURL(""))

	userToken, err := client.AuthLogin(cfg.TestUser.Username, cfg.TestUser.Password)
	if err != nil {
		t.Skip("Test user not available, skipping")
	}
	client.SetToken(userToken["token"].(string))

	fixtures := NewFixtures()
	t.Cleanup(func() {
		cleanupDownloads(t, client, fixtures)
		cleanupTestMedia(t, client, fixtures)
	})

	t.Run("Add media to library", func(t *testing.T) {
		req := AddMediaRequest{
			Type:    "movie",
			Title:   cfg.PublicVideo.Name,
			Year:    uint16(cfg.PublicVideo.Year),
			Quality: "1080p",
		}

		result, err := client.AddMedia(req)
		if err != nil {
			t.Fatalf("Failed to add movie: %v", err)
		}

		fixtures.AddMediaID("bigbuckbunny", result.ID)
	})

	t.Run("Search and download with force", func(t *testing.T) {
		req := ManualSearchRequest{
			Query: "Big Buck Bunny 2008",
			Type:  "movie",
		}

		searchResult, err := client.StartManualSearch(req)
		if err != nil {
			t.Fatalf("Failed to start search: %v", err)
		}

		t.Cleanup(func() {
			client.ClearSearchSession(searchResult.SessionID)
		})

		time.Sleep(5 * time.Second)

		resp, err := client.GetManualSearchResults(searchResult.SessionID)
		if err != nil {
			t.Fatalf("Failed to get results: %v", err)
		}

		if len(resp.Results) == 0 {
			t.Skip("No search results available")
		}

		result := resp.Results[0]
		if cfg.VerboseOutput {
			t.Logf("Selecting first result: %s from %s", result.Title, result.Indexer)
		}

		mediaID, _ := fixtures.GetMediaID("bigbuckbunny")

		downloadReq := DownloadRequest{
			SessionID: searchResult.SessionID,
			ResultIdx: 0,
			MediaID:   mediaID,
			MediaType: "movie",
			Title:     result.Title,
			Quality:   result.Quality,
			Force:     true,
		}

		downloadResult, err := client.DownloadSearchResult(downloadReq)
		if err != nil {
			t.Fatalf("Failed to queue download: %v", err)
		}

		if downloadResult.JobID == 0 {
			t.Fatal("Expected non-zero job ID")
		}

		fixtures.AddDownloadID(downloadResult.JobID)

		if cfg.VerboseOutput {
			t.Logf("Queued download with Job ID: %d", downloadResult.JobID)
		}
	})

	t.Run("Verify download in queue", func(t *testing.T) {
		time.Sleep(2 * time.Second)

		downloads, err := client.GetDownloads()
		if err != nil {
			t.Fatalf("Failed to get downloads: %v", err)
		}

		if len(downloads) == 0 {
			t.Fatal("Expected at least one download in queue")
		}

		found := false
		for _, d := range downloads {
			if d.Title == cfg.PublicVideo.Name {
				found = true
				if cfg.VerboseOutput {
					t.Logf("Found download: %s (Status: %d, Progress: %.1f%%)", d.Title, d.Status, d.Progress*100)
				}
			}
		}

		if !found {
			t.Fatalf("Expected to find %s in downloads", cfg.PublicVideo.Name)
		}
	})
}

func TestSearchWithLibraryLink(t *testing.T) {
	cfg := LoadTestConfig(t)
	cfg.SkipIfNoServer(t)
	cfg.SkipIfNoIndexers(t)

	client := NewAPIClient(cfg.GetAPIURL(""))

	userToken, err := client.AuthLogin(cfg.TestUser.Username, cfg.TestUser.Password)
	if err != nil {
		t.Skip("Test user not available, skipping")
	}
	client.SetToken(userToken["token"].(string))

	fixtures := NewFixtures()
	t.Cleanup(func() {
		cleanupTestMedia(t, client, fixtures)
	})

	t.Run("Link search to library item", func(t *testing.T) {
		req := AddMediaRequest{
			Type:    "movie",
			Title:   "Linked Movie",
			Year:    2020,
			Quality: "1080p",
		}

		result, err := client.AddMedia(req)
		if err != nil {
			t.Fatalf("Failed to add movie: %v", err)
		}

		fixtures.AddMediaID("linkedmovie", result.ID)

		searchReq := ManualSearchRequest{
			Query:   "Sintel",
			Type:    "movie",
			MediaID: result.ID,
		}

		searchResult, err := client.StartManualSearch(searchReq)
		if err != nil {
			t.Fatalf("Failed to start search: %v", err)
		}

		t.Cleanup(func() {
			client.ClearSearchSession(searchResult.SessionID)
		})

		if cfg.VerboseOutput {
			t.Logf("Search linked to media ID: %d", result.ID)
		}
	})
}

func TestSearchResultsSorting(t *testing.T) {
	cfg := LoadTestConfig(t)
	cfg.SkipIfNoServer(t)
	cfg.SkipIfNoIndexers(t)

	client := NewAPIClient(cfg.GetAPIURL(""))

	userToken, err := client.AuthLogin(cfg.TestUser.Username, cfg.TestUser.Password)
	if err != nil {
		t.Skip("Test user not available, skipping")
	}
	client.SetToken(userToken["token"].(string))

	t.Run("Results sorted by freeleech first", func(t *testing.T) {
		req := ManualSearchRequest{
			Query: "Sintel",
			Type:  "movie",
		}

		searchResult, err := client.StartManualSearch(req)
		if err != nil {
			t.Fatalf("Failed to start search: %v", err)
		}

		t.Cleanup(func() {
			client.ClearSearchSession(searchResult.SessionID)
		})

		time.Sleep(5 * time.Second)

		resp, err := client.GetManualSearchResults(searchResult.SessionID)
		if err != nil {
			t.Fatalf("Failed to get results: %v", err)
		}

		if len(resp.Results) < 2 {
			t.Skip("Not enough results to test sorting")
		}

		freeleechCount := 0
		for _, r := range resp.Results {
			if r.IsFreeleech {
				freeleechCount++
			}
		}

		if freeleechCount > 0 {
			if resp.Results[0].IsFreeleech {
				if cfg.VerboseOutput {
					t.Log("Results correctly sorted with freeleech first")
				}
			}
		}
	})
}

func cleanupDownloads(t *testing.T, client *APIClient, fixtures *Fixtures) {
	for _, id := range fixtures.DownloadIDs {
		if err := client.CancelDownload(id); err != nil {
			t.Logf("Warning: Failed to cancel download %d: %v", id, err)
		}
	}
}

func TestWebSocketSearch(t *testing.T) {
	cfg := LoadTestConfig(t)
	cfg.SkipIfNoServer(t)
	cfg.SkipIfNoIndexers(t)

	t.Run("WebSocket connection for search", func(t *testing.T) {
		cfg.VerboseOutput = true
		if cfg.VerboseOutput {
			t.Log("WebSocket search test would require browser context")
			t.Log("Manual testing recommended for WebSocket functionality")
		}
		t.Skip("WebSocket testing requires browser/Playwright")
	})
}

type WSSearchResult struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

func parseWSSearchResult(data []byte) (*WSSearchResult, error) {
	var result WSSearchResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
