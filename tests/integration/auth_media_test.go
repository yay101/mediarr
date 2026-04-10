package integration

import (
	"net/http"
	"strings"
	"testing"
)

func TestAuth(t *testing.T) {
	cfg := LoadTestConfig(t)
	cfg.SkipIfNoServer(t)
	client := NewAPIClient(cfg.GetAPIURL(""))

	t.Run("Login as admin", func(t *testing.T) {
		result, err := client.AuthLogin(cfg.AdminUser, cfg.AdminPass)
		if err != nil {
			t.Fatalf("Admin login failed: %v", err)
		}

		if result["token"] == nil {
			t.Fatal("Expected token in response")
		}

		token := result["token"].(string)
		if token == "" {
			t.Fatal("Token is empty")
		}

		if cfg.VerboseOutput {
			t.Logf("Admin login successful, token: %s...", token[:20])
		}
	})

	t.Run("Get current user", func(t *testing.T) {
		resp, err := client.Get("/api/v1/auth/me")
		if err != nil {
			t.Fatalf("Get current user failed: %v", err)
		}
		defer resp.Body.Close()

		AssertStatusCode(t, resp, http.StatusOK)

		var result map[string]interface{}
		if err := client.ParseResponse(resp, &result); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if result["authenticated"] != true {
			t.Fatal("Expected authenticated=true")
		}
	})

	t.Run("Login with invalid credentials", func(t *testing.T) {
		_, err := client.AuthLogin("invalid", "wrongpassword")
		if err == nil {
			t.Fatal("Expected error for invalid credentials")
		}
	})
}

func TestUserCreation(t *testing.T) {
	cfg := LoadTestConfig(t)
	cfg.SkipIfNoServer(t)

	adminClient := NewAPIClient(cfg.GetAPIURL(""))

	_, err := adminClient.AuthLogin(cfg.AdminUser, cfg.AdminPass)
	if err != nil {
		t.Fatalf("Admin login failed: %v", err)
	}

	t.Run("Create new user", func(t *testing.T) {
		userData := map[string]interface{}{
			"username": cfg.TestUser.Username,
			"password": cfg.TestUser.Password,
			"email":    cfg.TestUser.Email,
		}

		resp, err := adminClient.Post("/api/v1/auth/register", userData)
		if err != nil {
			t.Fatalf("User creation failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusConflict {
			t.Log("User already exists, skipping creation")
			return
		}

		AssertStatusCode(t, resp, http.StatusCreated)
	})

	t.Run("Login as new user", func(t *testing.T) {
		result, err := adminClient.AuthLogin(cfg.TestUser.Username, cfg.TestUser.Password)
		if err != nil {
			t.Fatalf("User login failed: %v", err)
		}

		if result["token"] == nil {
			t.Fatal("Expected token in response")
		}
	})
}

func TestUserSearchForPublicVideo(t *testing.T) {
	cfg := LoadTestConfig(t)
	cfg.SkipIfNoServer(t)
	client := NewAPIClient(cfg.GetAPIURL(""))

	userToken, err := client.AuthLogin(cfg.TestUser.Username, cfg.TestUser.Password)
	if err != nil {
		t.Skip("Test user not available, skipping")
	}
	client.SetToken(userToken["token"].(string))

	t.Run("Search for Big Buck Bunny", func(t *testing.T) {
		query := cfg.PublicVideo.Name
		resp, err := client.Get("/api/v1/search?q=" + query)
		if err != nil {
			t.Fatalf("Metadata search failed: %v", err)
		}
		defer resp.Body.Close()

		AssertStatusCode(t, resp, http.StatusOK)

		var result map[string]interface{}
		if err := client.ParseResponse(resp, &result); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if result["type"] != "metadata" {
			t.Fatalf("Expected metadata results, got: %v", result["type"])
		}

		results := result["results"].([]interface{})
		if len(results) == 0 {
			t.Fatal("Expected at least one result for Big Buck Bunny")
		}

		if cfg.VerboseOutput {
			t.Logf("Found %d results for '%s'", len(results), query)
		}
	})
}

func TestAddToLibrary(t *testing.T) {
	cfg := LoadTestConfig(t)
	cfg.SkipIfNoServer(t)
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

	t.Run("Add movie to library", func(t *testing.T) {
		req := AddMediaRequest{
			Type:        "movie",
			Title:       cfg.PublicVideo.Name,
			Year:        uint16(cfg.PublicVideo.Year),
			TMDBID:      uint32(cfg.PublicVideo.TMDBID),
			ExternalSrc: "tmdb",
			Quality:     "1080p",
		}

		result, err := client.AddMedia(req)
		if err != nil {
			t.Fatalf("Failed to add movie: %v", err)
		}

		if result.ID == 0 {
			t.Fatal("Expected non-zero ID for created movie")
		}

		fixtures.AddMediaID("bigbuckbunny", result.ID)

		if cfg.VerboseOutput {
			t.Logf("Added movie with ID: %d", result.ID)
		}
	})

	t.Run("List movies", func(t *testing.T) {
		result, err := client.GetMedia("movie")
		if err != nil {
			t.Fatalf("Failed to list movies: %v", err)
		}

		if len(result.Movies) == 0 {
			t.Fatal("Expected at least one movie in library")
		}

		found := false
		for _, m := range result.Movies {
			if m.Title == cfg.PublicVideo.Name {
				found = true
				if cfg.VerboseOutput {
					t.Logf("Found movie in library: %s (ID: %d, Status: %d)", m.Title, m.ID, m.Status)
				}
			}
		}

		if !found {
			t.Fatalf("Expected to find %s in movie list", cfg.PublicVideo.Name)
		}
	})

	t.Run("Add TV show to library", func(t *testing.T) {
		req := AddMediaRequest{
			Type:    "tv",
			Title:   "Test TV Show",
			Year:    2024,
			Quality: "720p",
		}

		result, err := client.AddMedia(req)
		if err != nil {
			t.Fatalf("Failed to add TV show: %v", err)
		}

		fixtures.AddMediaID("testtvshow", result.ID)
	})
}

func cleanupTestMedia(t *testing.T, client *APIClient, fixtures *Fixtures) {
	for key, id := range fixtures.MediaIDs {
		if err := client.DeleteMedia(getMediaType(key), id); err != nil {
			t.Logf("Warning: Failed to cleanup media %s (ID: %d): %v", key, id, err)
		}
	}
}

func getMediaType(key string) string {
	switch {
	case strings.Contains(key, "tvshow"):
		return "tv"
	case strings.Contains(key, "music"):
		return "music"
	case strings.Contains(key, "book"):
		return "book"
	default:
		return "movie"
	}
}
