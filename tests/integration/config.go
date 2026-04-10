package integration

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"
)

type TestConfig struct {
	ServerURL     string
	AdminUser     string
	AdminPass     string
	TestUser      TestUser
	DownloadDir   string
	LibraryDir    string
	HardlinkDir   string
	Indexers      []IndexerConfig
	Trackers      []TrackerConfig
	TMDBAPIKey    string
	PublicVideo   PublicVideoConfig
	RunSlowTests  bool
	CleanupFiles  bool
	ParallelTests bool
	VerboseOutput bool
}

type TestUser struct {
	Username string
	Password string
	Email    string
	Token    string
}

type IndexerConfig struct {
	URL    string
	APIKey string
	Name   string
}

type TrackerConfig struct {
	Enabled  bool
	URL      string
	Username string
	Password string
	APIKey   string
	PassKey  string
}

type PublicVideoConfig struct {
	Name   string
	Year   int
	TMDBID int
}

func LoadTestConfig(t *testing.T) *TestConfig {
	t.Helper()

	config := &TestConfig{}

	getEnv := func(key, defaultVal string) string {
		if val := os.Getenv(key); val != "" {
			return val
		}
		return defaultVal
	}

	getEnvBool := func(key string, defaultVal bool) bool {
		if val := os.Getenv(key); val != "" {
			b, err := strconv.ParseBool(val)
			if err == nil {
				return b
			}
		}
		return defaultVal
	}

	getEnvInt := func(key string, defaultVal int) int {
		if val := os.Getenv(key); val != "" {
			i, err := strconv.Atoi(val)
			if err == nil {
				return i
			}
		}
		return defaultVal
	}

	config.ServerURL = getEnv("TEST_SERVER_URL", "http://localhost:8080")
	config.AdminUser = getEnv("TEST_ADMIN_USER", "admin")
	config.AdminPass = getEnv("TEST_ADMIN_PASS", "changeme123")

	config.TestUser = TestUser{
		Username: getEnv("TEST_USER_USERNAME", "testuser"),
		Password: getEnv("TEST_USER_PASSWORD", "TestPass123!"),
		Email:    getEnv("TEST_USER_EMAIL", "testuser@example.com"),
	}

	config.DownloadDir = getEnv("TEST_DOWNLOAD_DIR", "/tmp/mediarr-tests/downloads")
	config.LibraryDir = getEnv("TEST_LIBRARY_DIR", "/tmp/mediarr-tests/library")
	config.HardlinkDir = getEnv("TEST_HARDLINK_DIR", "/tmp/mediarr-tests/hardlinks")

	if url := getEnv("TEST_INDEXER_1_URL", ""); url != "" {
		config.Indexers = append(config.Indexers, IndexerConfig{
			URL:    url,
			APIKey: getEnv("TEST_INDEXER_1_API_KEY", ""),
			Name:   getEnv("TEST_INDEXER_1_NAME", "Indexer 1"),
		})
	}
	if url := getEnv("TEST_INDEXER_2_URL", ""); url != "" {
		config.Indexers = append(config.Indexers, IndexerConfig{
			URL:    url,
			APIKey: getEnv("TEST_INDEXER_2_API_KEY", ""),
			Name:   getEnv("TEST_INDEXER_2_NAME", "Indexer 2"),
		})
	}

	if getEnvBool("TEST_TRACKER_RED_ENABLED", false) {
		config.Trackers = append(config.Trackers, TrackerConfig{
			Enabled:  true,
			URL:      getEnv("TEST_TRACKER_RED_URL", "https://redacted.ch"),
			Username: getEnv("TEST_TRACKER_RED_USERNAME", ""),
			Password: getEnv("TEST_TRACKER_RED_PASSWORD", ""),
			PassKey:  getEnv("TEST_TRACKER_RED_PASSKEY", ""),
		})
	}

	config.TMDBAPIKey = getEnv("TEST_TMDB_API_KEY", "")

	config.PublicVideo = PublicVideoConfig{
		Name:   getEnv("TEST_PUBLIC_VIDEO_NAME", "Big Buck Bunny"),
		Year:   getEnvInt("TEST_PUBLIC_VIDEO_YEAR", 2008),
		TMDBID: getEnvInt("TEST_PUBLIC_VIDEO_TMDB_ID", 251),
	}

	config.RunSlowTests = getEnvBool("RUN_SLOW_TESTS", false)
	config.CleanupFiles = getEnvBool("CLEANUP_TEST_FILES", true)
	config.ParallelTests = getEnvBool("PARALLEL_TESTS", false)
	config.VerboseOutput = getEnvBool("VERBOSE_OUTPUT", true)

	if config.VerboseOutput {
		t.Logf("Test configuration loaded:")
		t.Logf("  Server URL: %s", config.ServerURL)
		t.Logf("  Admin: %s", config.AdminUser)
		t.Logf("  Test user: %s", config.TestUser.Username)
		t.Logf("  Download dir: %s", config.DownloadDir)
		t.Logf("  Library dir: %s", config.LibraryDir)
		t.Logf("  Indexers configured: %d", len(config.Indexers))
		t.Logf("  Trackers configured: %d", len(config.Trackers))
	}

	return config
}

func (c *TestConfig) SkipIfNoServer(t *testing.T) {
	t.Helper()
	if !c.isServerReachable() {
		t.Skip("Server not reachable at " + c.ServerURL)
	}
}

func (c *TestConfig) SkipIfSlow(t *testing.T) {
	t.Helper()
	if !c.RunSlowTests {
		t.Skip("Slow tests disabled (set RUN_SLOW_TESTS=true to enable)")
	}
}

func (c *TestConfig) SkipIfNoIndexers(t *testing.T) {
	t.Helper()
	if len(c.Indexers) == 0 {
		t.Skip("No indexers configured (set TEST_INDEXER_1_URL and TEST_INDEXER_1_API_KEY)")
	}
}

func (c *TestConfig) SkipIfNoTrackers(t *testing.T) {
	t.Helper()
	if len(c.Trackers) == 0 {
		t.Skip("No trackers configured")
	}
}

func (c *TestConfig) SetupTestDirs(t *testing.T) {
	t.Helper()

	dirs := []string{c.DownloadDir, c.LibraryDir, c.HardlinkDir}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create test directory %s: %v", dir, err)
		}
	}

	if c.VerboseOutput {
		t.Logf("Created test directories")
	}
}

func (c *TestConfig) Cleanup(t *testing.T) {
	t.Helper()

	if !c.CleanupFiles {
		return
	}

	dirs := []string{c.DownloadDir, c.LibraryDir, c.HardlinkDir}
	for _, dir := range dirs {
		if err := os.RemoveAll(dir); err != nil {
			t.Logf("Warning: Failed to cleanup %s: %v", dir, err)
		}
	}

	if c.VerboseOutput {
		t.Logf("Cleaned up test directories")
	}
}

func (c *TestConfig) isServerReachable() bool {
	resp, err := http.Get(c.ServerURL + "/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (c *TestConfig) GetAPIURL(path string) string {
	return fmt.Sprintf("%s%s", c.ServerURL, path)
}

func (c *TestConfig) GetAuthHeaders(token string) map[string]string {
	headers := make(map[string]string)
	headers["Content-Type"] = "application/json"
	if token != "" {
		headers["Authorization"] = "Bearer " + token
	}
	return headers
}

type Fixtures struct {
	AdminToken  string
	UserToken   string
	MediaIDs    map[string]uint32
	DownloadIDs []uint32
}

func NewFixtures() *Fixtures {
	return &Fixtures{
		MediaIDs:    make(map[string]uint32),
		DownloadIDs: make([]uint32, 0),
	}
}

func (f *Fixtures) AddMediaID(key string, id uint32) {
	f.MediaIDs[key] = id
}

func (f *Fixtures) GetMediaID(key string) (uint32, bool) {
	id, ok := f.MediaIDs[key]
	return id, ok
}

func (f *Fixtures) AddDownloadID(id uint32) {
	f.DownloadIDs = append(f.DownloadIDs, id)
}

func NewHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
	}
}

func WaitForCondition(t *testing.T, condition func() bool, timeout time.Duration, name string) bool {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}

func AssertStatusCode(t *testing.T, resp *http.Response, expected int) {
	t.Helper()
	if resp.StatusCode != expected {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Expected status %d, got %d. Body: %s", expected, resp.StatusCode, string(body))
	}
}
